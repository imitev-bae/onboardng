package main

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"net/smtp"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"database/sql"
	"html/template"

	_ "modernc.org/sqlite"

	"github.com/fsnotify/fsnotify"
	"github.com/hesusruiz/onboardng/common"
	"github.com/hesusruiz/onboardng/credissuance"
	"gopkg.in/yaml.v3"
)

// APIResponse is the reply to the API calls
type APIResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type RateLimitEntry struct {
	Count     int
	StartTime time.Time
}

var (
	emailRateLimiter  = make(map[string]*RateLimitEntry)
	verificationCodes = make(map[string]string) // email -> code
	rateLimiterMu     sync.RWMutex
	codesMu           sync.RWMutex
)

var (
	issuer  *credissuance.LEARIssuance
	db      *sql.DB
	smtpCfg credissuance.SMTPConfig
)

func main() {
	watchFlag := flag.Bool("watch", false, "watch for changes and start server")
	envFlag := flag.String("env", "dev", "environment to serve (dev, pre or pro)")
	port := flag.String("port", "3000", "port for the server")
	flag.Parse()

	// Load configuration
	configData, err := os.ReadFile("config.yaml")
	if err != nil {
		slog.Error("❌ Error reading config.yaml", "error", err)
		os.Exit(1)
	}
	var cfg Config
	if err := yaml.Unmarshal(configData, &cfg); err != nil {
		slog.Error("❌ Error parsing config.yaml", "error", err)
		os.Exit(1)
	}

	// Initial generation
	generate(cfg)

	// Initialize database
	db, err = initDB()
	if err != nil {
		slog.Error("❌ Error initializing database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// Get the environment config
	envCfg := cfg.Environments[*envFlag]

	// Setup issuer
	issuerCfg := credissuance.Config{
		PrivateKeyFile:        envCfg.PrivateKeyFile,
		MachineCredentialFile: envCfg.MachineCredentialFile,
		MyDidkey:              envCfg.MyDidkey,
		Verifier: credissuance.VerifierConfig{
			URL:           envCfg.Verifier.URL,
			TokenEndpoint: envCfg.Verifier.TokenEndpoint,
		},
		Issuer: credissuance.IssuerConfig{
			CredentialIssuancePath: envCfg.Issuer.CredentialIssuancePath,
		},
	}
	issuer, err = credissuance.NewLEARIssuance(issuerCfg)
	if err != nil {
		slog.Error("❌ Error creating issuer", "error", err)
		os.Exit(1)
	}

	// Setup SMTP config
	smtpCfg = envCfg.SMTP

	// Setup mux for API and Static Files
	mux := http.NewServeMux()

	// Static file serving from the generated directory
	targetDir := filepath.Join(cfg.DestDir, *envFlag)
	fileServer := http.FileServer(http.Dir(targetDir))
	mux.Handle("/", fileServer)

	// API Handlers
	mux.HandleFunc("/api/validate-email", enableCORS(handleValidateEmail))
	mux.HandleFunc("/api/verify-code", enableCORS(handleVerifyCode))
	mux.HandleFunc("/api/register", enableCORS(handleRegister))

	// Start Watcher if requested
	if *watchFlag {
		go startWatcher(cfg)
	}

	// Start Server
	slog.Info("🚀 Server running", "env", *envFlag, "dir", targetDir, "url", "http://localhost:"+*port)
	if err := http.ListenAndServe(":"+*port, mux); err != nil {
		slog.Error("Server failed", "error", err)
		os.Exit(1)
	}
}

func startWatcher(cfg Config) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Error("❌ Watcher Error", "error", err)
		return
	}
	defer watcher.Close()

	// Watch relevant directories and files recursively
	watchPaths := []string{
		cfg.SrcDir,
		"config.yaml",
	}

	for _, path := range watchPaths {
		err = filepath.Walk(path, func(walkPath string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return watcher.Add(walkPath)
			}
			return nil
		})
		if err != nil {
			slog.Warn("⚠️ Error walking path", "path", path, "error", err)
		}
	}
	watcher.Add("config.yaml")

	slog.Info("👀 Watching for changes...")

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			// Only regenerate on write, create, or remove events in source directories
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) != 0 {
				// Avoid infinite loop if we touch something in the dest_dir (though we only watch src_dir)
				slog.Info("📝 File updated. Regenerating...", "file", event.Name)
				generate(cfg)
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			slog.Error("❌ Watcher error", "error", err)
		}
	}
}

// sendJSON utility helper
func sendJSON(w http.ResponseWriter, status int, success bool, message string, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(APIResponse{
		Success: success,
		Message: message,
		Data:    data,
	})
}

// enableCORS middleware to allow all origins
func enableCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Requested-With, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next(w, r)
	}
}

// validateCSRF checks the X-Requested-With header
func validateCSRF(r *http.Request) bool {
	return r.Header.Get("X-Requested-With") != ""
}

func generateCode() string {
	n, _ := rand.Int(rand.Reader, big.NewInt(1000000))
	return fmt.Sprintf("%06d", n)
}

// isValidEmail checks if the email address provided has a valid format
func isValidEmail(email string) bool {
	// A simple but effective email regex
	re := regexp.MustCompile(`^[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}$`)
	return re.MatchString(strings.ToLower(email))
}

// generateRegistrationID creates a human-readable but unguessable ID in the format YYYYMMDD-{8-digit}
func generateRegistrationID() string {
	dateStr := time.Now().Format("20060102")
	n, _ := rand.Int(rand.Reader, big.NewInt(100000000)) // 8 digits
	return fmt.Sprintf("%s-%08d", dateStr, n)
}

// Registration represents a user registration record in the database
type Registration struct {
	RegistrationID  string    `json:"registration_id"`
	Email           string    `json:"email"`
	FirstName       string    `json:"first_name"`
	LastName        string    `json:"last_name"`
	CompanyName     string    `json:"company_name"`
	Country         string    `json:"country"`
	VatID           string    `json:"vat_id"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	IssuanceAt      time.Time `json:"issuance_at,omitempty"`
	IssuanceError   string    `json:"issuance_error,omitempty"`
	NotifEmailAt    time.Time `json:"notif_email_at,omitempty"`
	NotifEmailError string    `json:"notif_email_error,omitempty"`
}

func initDB() (*sql.DB, error) {
	db, err := sql.Open("sqlite", "onboarding.db")
	if err != nil {
		return nil, err
	}

	// Create table if not exists
	query := `
	CREATE TABLE IF NOT EXISTS registrations (
		registration_id TEXT UNIQUE,
		email TEXT UNIQUE,
		first_name TEXT,
		last_name TEXT,
		company_name TEXT,
		country TEXT,
		vat_id TEXT UNIQUE,
		created_at DATETIME,
		updated_at DATETIME,
		issuance_at DATETIME,
		issuance_error TEXT,
		notif_email_at DATETIME,
		notif_email_error TEXT
	);`
	if _, err := db.Exec(query); err != nil {
		return nil, err
	}

	return db, nil
}

func saveRegistration(db *sql.DB, reg *Registration) error {
	now := time.Now()
	if reg.CreatedAt.IsZero() {
		reg.CreatedAt = now
	}
	reg.UpdatedAt = now

	query := `
	INSERT INTO registrations (
		registration_id, email, first_name, last_name, company_name, country, vat_id,
		created_at, updated_at, issuance_at, issuance_error, notif_email_at, notif_email_error
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := db.Exec(query,
		reg.RegistrationID, reg.Email, reg.FirstName, reg.LastName, reg.CompanyName, reg.Country, reg.VatID,
		reg.CreatedAt, reg.UpdatedAt, reg.IssuanceAt, reg.IssuanceError, reg.NotifEmailAt, reg.NotifEmailError,
	)
	return err
}

func updateRegistrationStatus(db *sql.DB, reg *Registration) error {
	reg.UpdatedAt = time.Now()
	query := `
	UPDATE registrations SET
		updated_at = ?,
		issuance_at = ?,
		issuance_error = ?,
		notif_email_at = ?,
		notif_email_error = ?
	WHERE registration_id = ? AND email = ?`
	_, err := db.Exec(query,
		reg.UpdatedAt, reg.IssuanceAt, reg.IssuanceError, reg.NotifEmailAt, reg.NotifEmailError,
		reg.RegistrationID, reg.Email,
	)
	return err
}

func amendRegistration(db *sql.DB, reg *Registration) error {
	reg.UpdatedAt = time.Now()
	query := `
	UPDATE registrations SET
		first_name = ?,
		last_name = ?,
		company_name = ?,
		country = ?,
		updated_at = ?
	WHERE registration_id = ? AND email = ?`
	_, err := db.Exec(query,
		reg.FirstName, reg.LastName, reg.CompanyName, reg.Country,
		reg.UpdatedAt,
		reg.RegistrationID, reg.Email,
	)
	return err
}

func getRegistrations(db *sql.DB, limit, offset int) ([]Registration, error) {
	query := `
	SELECT 
		registration_id, email, first_name, last_name, company_name, country, vat_id,
		created_at, updated_at, issuance_at, issuance_error, notif_email_at, notif_email_error
	FROM registrations
	ORDER BY created_at DESC
	LIMIT ? OFFSET ?`

	rows, err := db.Query(query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var regs []Registration
	for rows.Next() {
		var reg Registration
		err := rows.Scan(
			&reg.RegistrationID, &reg.Email, &reg.FirstName, &reg.LastName, &reg.CompanyName, &reg.Country, &reg.VatID,
			&reg.CreatedAt, &reg.UpdatedAt, &reg.IssuanceAt, &reg.IssuanceError, &reg.NotifEmailAt, &reg.NotifEmailError,
		)
		if err != nil {
			return nil, err
		}
		regs = append(regs, reg)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return regs, nil
}

// handleValidateEmail initiates the email verification process by generating a code.
//
// HTTP Request:
//
//	Method: POST
//	Path: /api/validate-email
//	Headers:
//	  Content-Type: application/json
//	  X-Requested-With: [any value] (Required for CSRF protection)
//
// Request Body:
//
//	{
//	  "email": "string"
//	}
//
// Success Response (200 OK):
//
//	{
//	  "success": true,
//	  "message": "Validation code sent to your email",
//	  "data": { "code": "123456" }
//	}
//
// Error Responses:
//
//	400 Bad Request: Invalid JSON or missing email
//	403 Forbidden: Missing CSRF header
//	405 Method Not Allowed: If not a POST request
//	429 Too Many Requests: If rate limit exceeded (3 requests per 3 minutes)
func handleValidateEmail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !validateCSRF(r) {
		sendJSON(w, http.StatusForbidden, false, "Security check failed: missing CSRF header", nil)
		return
	}

	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendJSON(w, http.StatusBadRequest, false, "Invalid request body", nil)
		return
	}

	if req.Email == "" || !isValidEmail(req.Email) {
		sendJSON(w, http.StatusBadRequest, false, "A valid email is required", nil)
		return
	}

	// Rate limiting: 3 requests allowed in a window of 3 minutes
	rateLimiterMu.Lock()
	entry, exists := emailRateLimiter[req.Email]

	if !exists || time.Since(entry.StartTime) > 3*time.Minute {
		// New window or window expired
		emailRateLimiter[req.Email] = &RateLimitEntry{
			Count:     1,
			StartTime: time.Now(),
		}
	} else {
		// Within window
		if entry.Count >= 3 {
			rateLimiterMu.Unlock()
			sendJSON(w, http.StatusTooManyRequests, false, "Too many requests. Please wait a few minutes.", nil)
			return
		}
		entry.Count++
	}
	rateLimiterMu.Unlock()

	// Generate and store code
	code := generateCode()
	codesMu.Lock()
	verificationCodes[req.Email] = code
	codesMu.Unlock()

	// Return code for testing/simulation
	sendJSON(w, http.StatusOK, true, "Validation code sent to your email", map[string]string{"code": code})
}

// handleVerifyCode verifies the code sent to the user's email.
//
// HTTP Request:
//
//	Method: POST
//	Path: /api/verify-code
//	Headers:
//	  Content-Type: application/json
//	  X-Requested-With: [any value] (Required for CSRF protection)
//
// Request Body:
//
//	{
//	  "email": "string",
//	  "code": "string"
//	}
//
// Success Response (200 OK):
//
//	{
//	  "success": true,
//	  "message": "Email verified successfully"
//	}
//
// Error Responses:
//
//	400 Bad Request: Invalid JSON or incorrect code
//	403 Forbidden: Missing CSRF header
//	405 Method Not Allowed: If not a POST request
func handleVerifyCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !validateCSRF(r) {
		sendJSON(w, http.StatusForbidden, false, "Security check failed: missing CSRF header", nil)
		return
	}

	var req struct {
		Email string `json:"email"`
		Code  string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendJSON(w, http.StatusBadRequest, false, "Invalid request body", nil)
		return
	}

	codesMu.RLock()
	expectedCode, exists := verificationCodes[req.Email]
	codesMu.RUnlock()

	if !exists || expectedCode != req.Code {
		sendJSON(w, http.StatusBadRequest, false, "Invalid verification code", nil)
		return
	}

	// Verification successful
	// TODO: We could remove the code here or keep it for a short duration to prevent replay if needed.
	codesMu.Lock()
	delete(verificationCodes, req.Email)
	codesMu.Unlock()

	sendJSON(w, http.StatusOK, true, "Email verified successfully", nil)
}

type registrationRequest struct {
	FirstName   string `json:"firstName"`
	LastName    string `json:"lastName"`
	CompanyName string `json:"companyName"`
	Country     string `json:"country"`
	VatId       string `json:"vatId"`
	Email       string `json:"email"`
	Website     string `json:"website"`
}

// Validate checks if the registration request is valid
func (r *registrationRequest) Validate() error {
	if r.FirstName == "" {
		return fmt.Errorf("first name is required")
	}
	if r.LastName == "" {
		return fmt.Errorf("last name is required")
	}
	if r.CompanyName == "" {
		return fmt.Errorf("company name is required")
	}
	if r.Country == "" {
		return fmt.Errorf("country is required")
	}
	if !common.IsValidCountry(r.Country) {
		return fmt.Errorf("invalid country code")
	}
	if r.VatId == "" {
		return fmt.Errorf("VAT ID is required")
	}
	if r.Email == "" {
		return fmt.Errorf("email is required")
	}
	if !isValidEmail(r.Email) {
		return fmt.Errorf("invalid email address format")
	}
	return nil
}

// handleRegister processes the final registration form.
//
// HTTP Request:
//
//	Method: POST
//	Path: /api/register
//	Headers:
//	  Content-Type: application/json
//	  X-Requested-With: [any value] (Required for CSRF protection)
//
// Request Body:
//
//	{
//	  "firstName": "string",
//	  "lastName": "string",
//	  "companyName": "string",
//	  "country": "string",
//	  "vatId": "string",
//	  "email": "string",
//	  "website": "string" (Honeypot field, should be empty)
//	}
//
// Success Response (200 OK):
//
//	{
//	  "success": true,
//	  "message": "Registration successful"
//	}
//
// Error Responses:
//
//	400 Bad Request: Invalid JSON or validation error (e.g., country code)
//	403 Forbidden: Missing CSRF header
//	405 Method Not Allowed: If not a POST request
//	500 Internal Server Error: Failed to issue credential
func handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !validateCSRF(r) {
		sendJSON(w, http.StatusForbidden, false, "Security check failed: missing CSRF header", nil)
		return
	}

	var requestData registrationRequest
	if err := json.NewDecoder(r.Body).Decode(&requestData); err != nil {
		sendJSON(w, http.StatusBadRequest, false, "Invalid request body", nil)
		return
	}

	// Honeypot check: assume 'website' is the hidden field
	if requestData.Website != "" {
		slog.Info("🤖 Bot detected via honeypot field")
		// Fake success for bots
		sendJSON(w, http.StatusOK, true, "Registration successful", nil)
		return
	}

	// Validate request data
	if err := requestData.Validate(); err != nil {
		sendJSON(w, http.StatusBadRequest, false, err.Error(), nil)
		return
	}

	// Fill the credential with the data from the form
	cred := &credissuance.LEARIssuanceRequestBody{
		Schema:        "LEARCredentialEmployee",
		OperationMode: "S",
		Format:        "jwt_vc_json",
		Payload: credissuance.Payload{
			Mandator: credissuance.Mandator{
				OrganizationIdentifier: requestData.Country + "-" + requestData.VatId,
				Organization:           requestData.CompanyName,
				Country:                requestData.Country,
				CommonName:             requestData.FirstName + " " + requestData.LastName,
				EmailAddress:           requestData.Email,
			},
			Mandatee: credissuance.Mandatee{
				FirstName:   requestData.FirstName,
				LastName:    requestData.LastName,
				Nationality: requestData.Country,
				Email:       requestData.Email,
			},
			Power: []credissuance.Power{
				{
					Type:     "domain",
					Domain:   "DOME",
					Function: "Onboarding",
					Action:   credissuance.Strings{"execute", "verify"},
				},
			},
		},
	}

	prettyJSON, _ := json.MarshalIndent(cred, "", "  ")
	fmt.Println(string(prettyJSON))

	// Create Registration object
	regID := generateRegistrationID()
	reg := &Registration{
		RegistrationID: regID,
		Email:          requestData.Email,
		FirstName:      requestData.FirstName,
		LastName:       requestData.LastName,
		CompanyName:    requestData.CompanyName,
		Country:        requestData.Country,
		VatID:          requestData.VatId,
	}

	// Initial save before issuance
	if err := saveRegistration(db, reg); err != nil {
		slog.Error("❌ Error saving initial registration", "error", err)
		// Return with an error response, there is nothing we can do here
		sendJSON(w, http.StatusInternalServerError, false, "Failed to save registration", nil)
		return
	}

	// Perform the issuance request
	reg.IssuanceAt = time.Now()
	_, err := issuer.LEARIssuanceRequest(cred)
	if err != nil {
		slog.Error("❌ Error calling issuance service", "error", err)
		reg.IssuanceError = err.Error()
		if updateErr := updateRegistrationStatus(db, reg); updateErr != nil {
			slog.Error("❌ Error updating registration status with issuance error", "error", updateErr)
		}
		sendJSON(w, http.StatusInternalServerError, false, "Failed to issue credential", nil)
		return
	}

	// Update registration with success
	reg.IssuanceError = ""
	if err := updateRegistrationStatus(db, reg); err != nil {
		slog.Error("❌ Error updating registration status with issuance success", "error", err)
	}

	// Send welcome email
	go func() {
		err := sendWelcomeEmail(reg, smtpCfg)
		if err != nil {
			slog.Error("❌ Error sending welcome email", "error", err)
			reg.NotifEmailError = err.Error()
		} else {
			slog.Info("📧 Welcome email sent", "email", reg.Email)
			reg.NotifEmailAt = time.Now()
			reg.NotifEmailError = ""
		}
		if updateErr := updateRegistrationStatus(db, reg); updateErr != nil {
			slog.Error("❌ Error updating registration status with email result", "error", updateErr)
		}
	}()

	sendJSON(w, http.StatusOK, true, "Registration successful", nil)
}

// sendWelcomeEmail sends a welcome email to the user who has registered.
func sendWelcomeEmail(reg *Registration, cfg credissuance.SMTPConfig) error {
	if !cfg.Enabled {
		return nil
	}

	// Read SMTP password
	passwordBytes, err := os.ReadFile(cfg.PasswordFile)
	if err != nil {
		return fmt.Errorf("failed to read SMTP password file: %w", err)
	}
	password := strings.TrimSpace(string(passwordBytes))

	// Data for the template
	data := map[string]any{
		"FirstName":      reg.FirstName,
		"CompanyName":    reg.CompanyName,
		"RegistrationID": reg.RegistrationID,
	}

	// Parse and execute template
	tmpl, err := template.ParseFiles("src/pages/email_welcome.html")
	if err != nil {
		return fmt.Errorf("failed to parse email template: %w", err)
	}

	var body bytes.Buffer
	if err := tmpl.ExecuteTemplate(&body, "content", data); err != nil {
		return fmt.Errorf("failed to execute email template: %w", err)
	}

	// Prepare email message
	from := cfg.Username
	to := []string{reg.Email}
	subject := "Welcome to Onboarding!"
	mime := "MIME-version: 1.0;\nContent-Type: text/html; charset=\"UTF-8\";\n\n"
	msg := []byte("From: " + from + "\n" +
		"To: " + reg.Email + "\n" +
		"Subject: " + subject + "\n" +
		mime + body.String())

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	auth := smtp.PlainAuth("", cfg.Username, password, cfg.Host)

	// Send email with support for TLS (port 465)
	if cfg.TLS && cfg.Port == 465 {
		tlsConfig := &tls.Config{
			InsecureSkipVerify: false,
			ServerName:         cfg.Host,
		}

		conn, err := tls.Dial("tcp", addr, tlsConfig)
		if err != nil {
			return fmt.Errorf("failed to dial TLS: %w", err)
		}
		defer conn.Close()

		c, err := smtp.NewClient(conn, cfg.Host)
		if err != nil {
			return fmt.Errorf("failed to create SMTP client: %w", err)
		}
		defer c.Quit()

		if err = c.Auth(auth); err != nil {
			return fmt.Errorf("failed to authenticate: %w", err)
		}

		if err = c.Mail(from); err != nil {
			return fmt.Errorf("failed to set sender: %w", err)
		}

		for _, addr := range to {
			if err = c.Rcpt(addr); err != nil {
				return fmt.Errorf("failed to add recipient: %w", err)
			}
		}

		w, err := c.Data()
		if err != nil {
			return fmt.Errorf("failed to open data writer: %w", err)
		}

		_, err = w.Write(msg)
		if err != nil {
			return fmt.Errorf("failed to write message: %w", err)
		}

		err = w.Close()
		if err != nil {
			return fmt.Errorf("failed to close data writer: %w", err)
		}

		return nil
	}

	// Normal SMTP (typically port 587 with STARTTLS)
	return smtp.SendMail(addr, auth, from, to, msg)
}

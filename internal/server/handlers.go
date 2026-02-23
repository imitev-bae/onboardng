package server

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/hesusruiz/onboardng/common"
	"github.com/hesusruiz/onboardng/credissuance"
	"github.com/hesusruiz/onboardng/internal/db"
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

type RegistrationRequest struct {
	FirstName   string `json:"firstName"`
	LastName    string `json:"lastName"`
	CompanyName string `json:"companyName"`
	Country     string `json:"country"`
	VatId       string `json:"vatId"`
	Email       string `json:"email"`
	Website     string `json:"website"`
}

// SendJSON utility helper
func (s *Server) SendJSON(w http.ResponseWriter, status int, success bool, message string, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(APIResponse{
		Success: success,
		Message: message,
		Data:    data,
	})
}

// EnableCORS middleware to allow all origins
func (s *Server) EnableCORS(next http.HandlerFunc) http.HandlerFunc {
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
	re := regexp.MustCompile(`^[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}$`)
	return re.MatchString(strings.ToLower(email))
}

// generateRegistrationID creates a human-readable but unguessable ID in the format YYYYMMDD-{8-digit}
func generateRegistrationID() string {
	dateStr := time.Now().Format("20060102")
	n, _ := rand.Int(rand.Reader, big.NewInt(100000000)) // 8 digits
	return fmt.Sprintf("%s-%08d", dateStr, n)
}

func (s *Server) HandleValidateEmail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !validateCSRF(r) {
		s.SendJSON(w, http.StatusForbidden, false, "Security check failed: missing CSRF header", nil)
		return
	}

	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.SendJSON(w, http.StatusBadRequest, false, "Invalid request body", nil)
		return
	}

	if req.Email == "" || !isValidEmail(req.Email) {
		s.SendJSON(w, http.StatusBadRequest, false, "A valid email is required", nil)
		return
	}

	// Rate limiting
	s.RateLimiterMu.Lock()
	entry, exists := s.EmailRateLimiter[req.Email]

	if !exists || time.Since(entry.StartTime) > 3*time.Minute {
		s.EmailRateLimiter[req.Email] = &RateLimitEntry{
			Count:     1,
			StartTime: time.Now(),
		}
	} else {
		if entry.Count >= 3 {
			s.RateLimiterMu.Unlock()
			s.SendJSON(w, http.StatusTooManyRequests, false, "Too many requests. Please wait a few minutes.", nil)
			return
		}
		entry.Count++
	}
	s.RateLimiterMu.Unlock()

	// Generate and store code
	code := generateCode()
	s.CodesMu.Lock()
	s.VerificationCodes[req.Email] = code
	s.CodesMu.Unlock()

	s.SendJSON(w, http.StatusOK, true, "Validation code sent to your email", map[string]string{"code": code})
}

func (s *Server) HandleVerifyCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !validateCSRF(r) {
		s.SendJSON(w, http.StatusForbidden, false, "Security check failed: missing CSRF header", nil)
		return
	}

	var req struct {
		Email string `json:"email"`
		Code  string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.SendJSON(w, http.StatusBadRequest, false, "Invalid request body", nil)
		return
	}

	s.CodesMu.RLock()
	expectedCode, exists := s.VerificationCodes[req.Email]
	s.CodesMu.RUnlock()

	if !exists || expectedCode != req.Code {
		s.SendJSON(w, http.StatusBadRequest, false, "Invalid verification code", nil)
		return
	}

	s.CodesMu.Lock()
	delete(s.VerificationCodes, req.Email)
	s.CodesMu.Unlock()

	s.SendJSON(w, http.StatusOK, true, "Email verified successfully", nil)
}

func (s *RegistrationRequest) Validate() error {
	if s.FirstName == "" {
		return fmt.Errorf("first name is required")
	}
	if s.LastName == "" {
		return fmt.Errorf("last name is required")
	}
	if s.CompanyName == "" {
		return fmt.Errorf("company name is required")
	}
	if s.Country == "" {
		return fmt.Errorf("country is required")
	}
	if !common.IsValidCountry(s.Country) {
		return fmt.Errorf("invalid country code")
	}
	if s.VatId == "" {
		return fmt.Errorf("VAT ID is required")
	}
	if s.Email == "" {
		return fmt.Errorf("email is required")
	}
	if !isValidEmail(s.Email) {
		return fmt.Errorf("invalid email address format")
	}
	return nil
}

// HandleRegister handles the registration process
// It validates the request data, generates a registration ID, and sends an email to the user
func (s *Server) HandleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !validateCSRF(r) {
		s.SendJSON(w, http.StatusForbidden, false, "Security check failed: missing CSRF header", nil)
		return
	}

	var requestData RegistrationRequest
	if err := json.NewDecoder(r.Body).Decode(&requestData); err != nil {
		s.SendJSON(w, http.StatusBadRequest, false, "Invalid request body", nil)
		return
	}

	if requestData.Website != "" {
		slog.Info("🤖 Bot detected via honeypot field")
		s.SendJSON(w, http.StatusOK, true, "Registration successful", nil)
		return
	}

	if err := requestData.Validate(); err != nil {
		s.SendJSON(w, http.StatusBadRequest, false, err.Error(), nil)
		return
	}

	slog.Info("Attempting to issue credential for registration", "email", requestData.Email, "vatID", requestData.VatId)

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

	regID := generateRegistrationID()
	reg := &db.Registration{
		RegistrationID: regID,
		Email:          requestData.Email,
		FirstName:      requestData.FirstName,
		LastName:       requestData.LastName,
		CompanyName:    requestData.CompanyName,
		Country:        requestData.Country,
		VatID:          requestData.VatId,
	}

	// Create an initial registration in the database, updated with error and status later
	if err := s.DB.SaveRegistration(reg); err != nil {
		slog.Error("❌ Error saving initial registration", "error", err)
		s.SendJSON(w, http.StatusInternalServerError, false, "Failed to save registration", err.Error())
		return
	}

	reg.IssuanceAt = time.Now()
	_, issError := s.Issuer.LEARIssuanceRequest(cred)
	if issError != nil {
		// There was an error, update the register and send an email informing of the error

		slog.Error("❌ Error calling issuance service", "error", issError)
		reg.IssuanceError = issError.Error()
		if updateErr := s.DB.UpdateRegistrationStatus(reg); updateErr != nil {
			slog.Error("❌ Error updating registration status with issuance error", "error", updateErr)
		}

		// Format the information we sent to the Issuer
		buf, err := json.MarshalIndent(cred, "", "  ")
		if err != nil {
			slog.Error("❌ Error marshalling credential data", "error", err)
		}

		// Send an email informing of the error, including the information that we wanted to issue
		err = s.Mail.SendIssuerError(reg, string(buf), reg.IssuanceError)
		if err != nil {
			slog.Error("❌ Error sending issuer error email", "error", err)
		}

		// Send a welcome email to the user, as if no error happened
		err = s.Mail.SendWelcomeEmail(reg)
		if err != nil {
			slog.Error("❌ Error sending welcome email", "error", err)
			reg.NotifEmailError = err.Error()
		} else {
			slog.Info("📧 Welcome email sent", "email", reg.Email)
			reg.NotifEmailAt = time.Now()
			reg.NotifEmailError = ""
		}
		if updateErr := s.DB.UpdateRegistrationStatus(reg); updateErr != nil {
			slog.Error("❌ Error updating registration status with email result", "error", updateErr)
		}

		s.SendJSON(w, http.StatusOK, true, "Registration successful", nil)
		return
	}

	// Issuance correct, update the register and send an email informing of the success
	reg.IssuanceError = ""
	if err := s.DB.UpdateRegistrationStatus(reg); err != nil {
		slog.Error("❌ Error updating registration status with issuance success", "error", err)
	}

	err := s.Mail.SendWelcomeEmail(reg)
	if err != nil {
		slog.Error("❌ Error sending welcome email", "error", err)
		reg.NotifEmailError = err.Error()
	} else {
		slog.Info("📧 Welcome email sent", "email", reg.Email)
		reg.NotifEmailAt = time.Now()
		reg.NotifEmailError = ""
	}
	if updateErr := s.DB.UpdateRegistrationStatus(reg); updateErr != nil {
		slog.Error("❌ Error updating registration status with email result", "error", updateErr)
	}

	s.SendJSON(w, http.StatusOK, true, "Registration successful", nil)
}

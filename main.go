package main

import (
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
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

	if req.Email == "" {
		sendJSON(w, http.StatusBadRequest, false, "Email is required", nil)
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

func handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !validateCSRF(r) {
		sendJSON(w, http.StatusForbidden, false, "Security check failed: missing CSRF header", nil)
		return
	}

	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendJSON(w, http.StatusBadRequest, false, "Invalid request body", nil)
		return
	}

	// Honeypot check: assume 'website' is the hidden field
	if hp, ok := req["website"].(string); ok && hp != "" {
		slog.Info("🤖 Bot detected via honeypot field")
		// Fake success for bots
		sendJSON(w, http.StatusOK, true, "Registration successful", nil)
		return
	}

	// TODO: Validate form, write to SQLite, call external API, send welcome email

	sendJSON(w, http.StatusOK, true, "Registration successful", nil)
}

package server

import (
	"log/slog"
	"net"
	"net/http"
	"sync"

	"golang.org/x/time/rate"

	"github.com/hesusruiz/onboardng/credissuance"
	"github.com/hesusruiz/onboardng/internal/configuration"
	"github.com/hesusruiz/onboardng/internal/db"
)

type DBServiceProvider interface {
	SaveRegistration(reg *db.RegistrationRecord) error
	UpdateRegistrationStatus(reg *db.RegistrationRecord) error
	SaveRegistrationError(regErr *db.RegistrationError) error
	GetRegistrationByVatID(vatID string) (*db.RegistrationRecord, error)
	GetRegistrationByEmail(email string) (*db.RegistrationRecord, error)
}

type MailServiceProvider interface {
	SendVerificationCode(email string, code string) error
	SendWelcomeEmail(reg *db.RegistrationRecord) error
	SendIssuerError(reg *db.RegistrationRecord, payload string, errorMsg string) error
}

type IssuanceServiceProvider interface {
	GetAccessToken() (string, error)
	TMFGetOrganizationByELSI(accessToken string, elsi string) ([]credissuance.Organization, error)
	TMFDeleteOrganization(accessToken string, id string) error
	LEARIssuanceRequest(accessToken string, learCredData *credissuance.LEARIssuanceRequestBody) ([]byte, error)
	TMFCreateOrganization(accessToken string, org *credissuance.Organization_Create) (*credissuance.Organization, error)
}

type Server struct {
	Runtime           configuration.RuntimeEnv
	DB                DBServiceProvider
	Issuer            IssuanceServiceProvider
	Mail              MailServiceProvider
	EmailRateLimiter  map[string]*RateLimitEntry
	VerificationCodes map[string]*VerificationCodeEntry
	RateLimiterMu     sync.RWMutex
	CodesMu           sync.RWMutex
	IPLimiters        map[string]*rate.Limiter
	IPLimitersMu      sync.Mutex
	Handler           http.Handler
}

func NewServer(runtime configuration.RuntimeEnv, dbService DBServiceProvider, issuer IssuanceServiceProvider, mailService MailServiceProvider, staticFilesDir string) *Server {
	s := &Server{
		Runtime:           runtime,
		DB:                dbService,
		Issuer:            issuer,
		Mail:              mailService,
		EmailRateLimiter:  make(map[string]*RateLimitEntry),
		VerificationCodes: make(map[string]*VerificationCodeEntry),
		IPLimiters:        make(map[string]*rate.Limiter),
	}

	mux := http.NewServeMux()

	// Static file serving
	// fileServer := http.FileServer(http.Dir(staticFilesDir))
	fileServer := http.FileServer(http.Dir("dist/browser"))
	mux.Handle("/", fileServer)

	// API Routes
	mux.HandleFunc("/api/validate-email", s.LogRequest(s.EnableCORS(s.RateLimitIP(s.HandleValidateEmail))))
	mux.HandleFunc("/api/verify-code", s.LogRequest(s.EnableCORS(s.HandleVerifyCode)))
	mux.HandleFunc("/api/register", s.LogRequest(s.EnableCORS(s.HandleRegister)))
	mux.HandleFunc("/health", s.HandleHealth)

	// Serve Angular SPA routes
	serveIndex := func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "dist/browser/index.html")
	}
	mux.HandleFunc("/register-customer", serveIndex)
	mux.HandleFunc("/register-provider", serveIndex)
	mux.HandleFunc("/onboard-provider", serveIndex)

	s.Handler = mux
	return s
}

func (s *Server) LogRequest(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slog.Info("Entry", "method", r.Method, "url", r.URL.Path)
		next(w, r)
	}
}

func (s *Server) getIPLimiter(ip string) *rate.Limiter {
	s.IPLimitersMu.Lock()
	defer s.IPLimitersMu.Unlock()

	limiter, exists := s.IPLimiters[ip]
	if !exists {
		// Allow 1 request per second with a burst of 5
		limiter = rate.NewLimiter(1, 5)
		s.IPLimiters[ip] = limiter
	}

	return limiter
}

func (s *Server) RateLimitIP(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}

		limiter := s.getIPLimiter(ip)
		if !limiter.Allow() {
			s.SendJSON(w, r, http.StatusTooManyRequests, false, "Too many requests", nil)
			return
		}

		next(w, r)
	}
}

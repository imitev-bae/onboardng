package server

import (
	"net"
	"net/http"
	"sync"

	"golang.org/x/time/rate"

	"github.com/hesusruiz/onboardng/credissuance"
	"github.com/hesusruiz/onboardng/internal/db"
	"github.com/hesusruiz/onboardng/internal/mail"
)

type Server struct {
	DB                *db.Service
	Issuer            *credissuance.LEARIssuance
	Mail              *mail.Service
	EmailRateLimiter  map[string]*RateLimitEntry
	VerificationCodes map[string]*VerificationCodeEntry
	RateLimiterMu     sync.RWMutex
	CodesMu           sync.RWMutex
	IPLimiters        map[string]*rate.Limiter
	IPLimitersMu      sync.Mutex
	Handler           http.Handler
}

func NewServer(dbService *db.Service, issuer *credissuance.LEARIssuance, mailService *mail.Service, staticFilesDir string) *Server {
	s := &Server{
		DB:                dbService,
		Issuer:            issuer,
		Mail:              mailService,
		EmailRateLimiter:  make(map[string]*RateLimitEntry),
		VerificationCodes: make(map[string]*VerificationCodeEntry),
		IPLimiters:        make(map[string]*rate.Limiter),
	}

	mux := http.NewServeMux()

	// Static file serving
	fileServer := http.FileServer(http.Dir(staticFilesDir))
	mux.Handle("/", fileServer)

	// API Routes
	mux.HandleFunc("/api/validate-email", s.EnableCORS(s.RateLimitIP(s.HandleValidateEmail)))
	mux.HandleFunc("/api/verify-code", s.EnableCORS(s.HandleVerifyCode))
	mux.HandleFunc("/api/register", s.EnableCORS(s.HandleRegister))
	mux.HandleFunc("/health", s.HandleHealth)

	s.Handler = mux
	return s
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
			s.SendJSON(w, http.StatusTooManyRequests, false, "Too many requests", nil)
			return
		}

		next(w, r)
	}
}

package server

import (
	"net/http"
	"sync"

	"github.com/hesusruiz/onboardng/credissuance"
	"github.com/hesusruiz/onboardng/internal/db"
	"github.com/hesusruiz/onboardng/internal/mail"
)

type Server struct {
	DB                *db.Service
	Issuer            *credissuance.LEARIssuance
	Mail              *mail.Service
	EmailRateLimiter  map[string]*RateLimitEntry
	VerificationCodes map[string]string
	RateLimiterMu     sync.RWMutex
	CodesMu           sync.RWMutex
}

func NewServer(dbService *db.Service, issuer *credissuance.LEARIssuance, mailService *mail.Service) *Server {
	return &Server{
		DB:                dbService,
		Issuer:            issuer,
		Mail:              mailService,
		EmailRateLimiter:  make(map[string]*RateLimitEntry),
		VerificationCodes: make(map[string]string),
	}
}

func (s *Server) RegisterRoutes() http.Handler {
	mux := http.NewServeMux()

	// API Handlers
	// Note: These are mounted under /api/ in main.go
	mux.HandleFunc("POST /api/validate-email", s.EnableCORS(s.HandleValidateEmail))
	mux.HandleFunc("POST /api/verify-code", s.EnableCORS(s.HandleVerifyCode))
	mux.HandleFunc("POST /api/register", s.EnableCORS(s.HandleRegister))

	return mux
}

package db

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/hesusruiz/onboardng/internal/configuration"
	"github.com/hesusruiz/utils/errl"
	_ "modernc.org/sqlite"
)

// RegistrationRecord represents a user registration record in the database
type RegistrationRecord struct {
	RegistrationID string    `json:"registration_id"`
	Email          string    `json:"email"`
	FirstName      string    `json:"first_name"`
	LastName       string    `json:"last_name"`
	CompanyName    string    `json:"company_name"`
	Country        string    `json:"country"`
	VatID          string    `json:"vat_id"`
	StreetAddress  string    `json:"street_address"`
	PostalCode     string    `json:"postal_code"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	Notified       bool      `json:"notified"`
	Issued         bool      `json:"issued"`
	TMFRegistered  bool      `json:"tmf_registered"`
}

// RegistrationError represents an error during the registration process
type RegistrationError struct {
	RegistrationID string    `json:"registration_id"`
	Email          string    `json:"email"`
	VatID          string    `json:"vat_id"`
	Error          string    `json:"error"`
	CreatedAt      time.Time `json:"created_at"`
}

// Service provides database operations for registrations
type Service struct {
	dbPath  string
	conn    *sql.DB
	runtime configuration.RuntimeEnv
}

func NewService(runtime configuration.RuntimeEnv, path string) (*Service, error) {

	// Create the directory if it does not exist
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, errl.Errorf("failed to create data directory: %w", err)
	}

	// Open the database
	dbConn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, errl.Errorf("failed to open database: %w", err)
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
		street_address TEXT,
		postal_code TEXT,
		notified BOOLEAN,
		issued BOOLEAN,
		tmf_registered BOOLEAN,
		created_at DATETIME,
		updated_at DATETIME
	);
	CREATE TABLE IF NOT EXISTS registration_errors (
		registration_id TEXT,
		email TEXT,
		vat_id TEXT,
		error TEXT,
		created_at DATETIME
	);`
	if _, err := dbConn.Exec(query); err != nil {
		dbConn.Close()
		return nil, errl.Errorf("failed to create tables: %w", err)
	}

	return &Service{dbPath: path, conn: dbConn, runtime: runtime}, nil
}

func (s *Service) Close() error {
	return s.conn.Close()
}

func (s *Service) SaveRegistration(reg *RegistrationRecord) error {
	insertQuery := `
	INSERT INTO registrations (
		registration_id, email, first_name, last_name, company_name, country, vat_id,
		street_address, postal_code,
		created_at, updated_at, notified, issued, tmf_registered
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	now := time.Now()
	reg.CreatedAt = now
	reg.UpdatedAt = now
	reg.Notified = false
	reg.Issued = false
	reg.TMFRegistered = false

	// Check if a registration with the same VAT ID already exists
	oldRegVat, err := s.GetRegistrationByVatID(reg.VatID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return errl.Errorf("failed to check VAT ID: %w", err)
	}

	// Check if a registration with the same email already exists
	oldRegEmail, err := s.GetRegistrationByEmail(reg.Email)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return errl.Errorf("failed to check email: %w", err)
	}

	switch s.runtime {
	case configuration.Development, configuration.Preproduction:
		// In Dev/Pre, if BOTH email and VAT ID match an existing record, we overwrite it (amend)
		if oldRegVat != nil && oldRegEmail != nil && oldRegVat.RegistrationID == oldRegEmail.RegistrationID {
			slog.Info("Registration exists with same email and VAT ID, amending", "vat_id", reg.VatID, "email", reg.Email)
			// Reuse the existing registration ID
			reg.RegistrationID = oldRegVat.RegistrationID
			return s.AmendRegistration(reg)
		}

		// Otherwise, if either exists, it's a conflict since they don't belong to the same record
		if oldRegVat != nil {
			return errl.Errorf("company with VAT ID %s already registered with a different email", reg.VatID)
		}
		if oldRegEmail != nil {
			return errl.Errorf("user with email %s already registered with a different VAT ID", reg.Email)
		}

		// No conflicts, insert as new
		slog.Info("Saving new registration in development or preproduction", "vat_id", reg.VatID, "email", reg.Email)
		_, err := s.conn.Exec(insertQuery,
			reg.RegistrationID, reg.Email, reg.FirstName, reg.LastName, reg.CompanyName, reg.Country, reg.VatID,
			reg.StreetAddress, reg.PostalCode,
			reg.CreatedAt, reg.UpdatedAt, reg.Notified, reg.Issued, reg.TMFRegistered,
		)
		if err != nil {
			return errl.Errorf("failed to insert registration: %w", err)
		}
		return nil

	case configuration.Production:
		slog.Info("Saving registration in production", "vat_id", reg.VatID, "email", reg.Email)

		// In production, we reject if either the company (vat_id) or the individual (email) already exists.
		if oldRegVat != nil {
			return errl.Errorf("company with VAT ID %s already registered", reg.VatID)
		}
		if oldRegEmail != nil {
			return errl.Errorf("email %s already registered", reg.Email)
		}

		_, err = s.conn.Exec(insertQuery,
			reg.RegistrationID, reg.Email, reg.FirstName, reg.LastName, reg.CompanyName, reg.Country, reg.VatID,
			reg.StreetAddress, reg.PostalCode,
			reg.CreatedAt, reg.UpdatedAt, reg.Notified, reg.Issued, reg.TMFRegistered,
		)
		if err != nil {
			return errl.Errorf("failed to insert registration: %w", err)
		}
		return nil
	}

	// Should never happen, return an error
	return fmt.Errorf("unknown runtime environment: %s", s.runtime)
}

func (s *Service) UpdateRegistrationStatus(reg *RegistrationRecord) error {
	reg.UpdatedAt = time.Now()
	query := `
	UPDATE registrations SET
		updated_at = ?,
		notified = ?,
		issued = ?,
		tmf_registered = ?
	WHERE registration_id = ? AND email = ?`
	_, err := s.conn.Exec(query,
		reg.UpdatedAt, reg.Notified, reg.Issued, reg.TMFRegistered,
		reg.RegistrationID, reg.Email,
	)
	if err != nil {
		return errl.Errorf("failed to update registration: %w", err)
	}
	return nil
}

func (s *Service) AmendRegistration(reg *RegistrationRecord) error {
	reg.UpdatedAt = time.Now()
	query := `
	UPDATE registrations SET
		registration_id = ?,
		first_name = ?,
		last_name = ?,
		company_name = ?,
		country = ?,
		street_address = ?,
		postal_code = ?,
		updated_at = ?,
		notified = ?,
		issued = ?,
		tmf_registered = ?,
		email = ?
	WHERE vat_id = ?`
	_, err := s.conn.Exec(query,
		reg.RegistrationID,
		reg.FirstName, reg.LastName, reg.CompanyName, reg.Country,
		reg.StreetAddress, reg.PostalCode,
		reg.UpdatedAt,
		reg.Notified, reg.Issued, reg.TMFRegistered,
		reg.Email,
		reg.VatID,
	)
	if err != nil {
		return errl.Errorf("failed to amend registration: %w", err)
	}
	return nil
}

func (s *Service) GetRegistrations(limit, offset int) ([]RegistrationRecord, error) {
	query := `
	SELECT 
		registration_id, email, first_name, last_name, company_name, country, vat_id,
		street_address, postal_code,
		created_at, updated_at, notified, issued, tmf_registered
	FROM registrations
	ORDER BY created_at DESC
	LIMIT ? OFFSET ?`

	rows, err := s.conn.Query(query, limit, offset)
	if err != nil {
		return nil, errl.Errorf("failed to get registrations: %w", err)
	}
	defer rows.Close()

	var regs []RegistrationRecord
	for rows.Next() {
		var reg RegistrationRecord
		err := rows.Scan(
			&reg.RegistrationID, &reg.Email, &reg.FirstName, &reg.LastName, &reg.CompanyName, &reg.Country, &reg.VatID,
			&reg.StreetAddress, &reg.PostalCode,
			&reg.CreatedAt, &reg.UpdatedAt, &reg.Notified, &reg.Issued, &reg.TMFRegistered,
		)
		if err != nil {
			return nil, errl.Errorf("failed to scan registration row: %w", err)
		}
		regs = append(regs, reg)
	}

	if err = rows.Err(); err != nil {
		return nil, errl.Errorf("failed to iterate over registration rows: %w", err)
	}

	return regs, nil
}

func (s *Service) GetRegistration(vatID string, email string) (*RegistrationRecord, error) {
	query := `
	SELECT 
		registration_id, email, first_name, last_name, company_name, country, vat_id,
		street_address, postal_code,
		created_at, updated_at, notified, issued, tmf_registered
	FROM registrations
	WHERE vat_id = ? AND email = ?`

	var reg RegistrationRecord
	err := s.conn.QueryRow(query, vatID, email).Scan(
		&reg.RegistrationID, &reg.Email, &reg.FirstName, &reg.LastName, &reg.CompanyName, &reg.Country, &reg.VatID,
		&reg.StreetAddress, &reg.PostalCode,
		&reg.CreatedAt, &reg.UpdatedAt, &reg.Notified, &reg.Issued, &reg.TMFRegistered,
	)
	if err != nil {
		return nil, errl.Errorf("failed to get registration by VAT ID and email: %w", err)
	}
	return &reg, nil
}

func (s *Service) GetRegistrationByVatID(vatID string) (*RegistrationRecord, error) {
	query := `
	SELECT 
		registration_id, email, first_name, last_name, company_name, country, vat_id,
		street_address, postal_code,
		created_at, updated_at, notified, issued, tmf_registered
	FROM registrations
	WHERE vat_id = ?`

	var reg RegistrationRecord
	err := s.conn.QueryRow(query, vatID).Scan(
		&reg.RegistrationID, &reg.Email, &reg.FirstName, &reg.LastName, &reg.CompanyName, &reg.Country, &reg.VatID,
		&reg.StreetAddress, &reg.PostalCode,
		&reg.CreatedAt, &reg.UpdatedAt, &reg.Notified, &reg.Issued, &reg.TMFRegistered,
	)
	if err != nil {
		return nil, errl.Errorf("failed to get registration by VAT ID: %w", err)
	}
	return &reg, nil
}

func (s *Service) GetRegistrationByEmail(email string) (*RegistrationRecord, error) {
	query := `
	SELECT 
		registration_id, email, first_name, last_name, company_name, country, vat_id,
		street_address, postal_code,
		created_at, updated_at, notified, issued, tmf_registered
	FROM registrations
	WHERE email = ?`

	var reg RegistrationRecord
	err := s.conn.QueryRow(query, email).Scan(
		&reg.RegistrationID, &reg.Email, &reg.FirstName, &reg.LastName, &reg.CompanyName, &reg.Country, &reg.VatID,
		&reg.StreetAddress, &reg.PostalCode,
		&reg.CreatedAt, &reg.UpdatedAt, &reg.Notified, &reg.Issued, &reg.TMFRegistered,
	)
	if err != nil {
		return nil, errl.Errorf("failed to get registration by email: %w", err)
	}
	return &reg, nil
}

func (s *Service) SaveRegistrationError(regErr *RegistrationError) error {
	query := `
	INSERT INTO registration_errors (
		registration_id, email, vat_id, error, created_at
	) VALUES (?, ?, ?, ?, ?)`

	regErr.CreatedAt = time.Now()

	_, err := s.conn.Exec(query,
		regErr.RegistrationID, regErr.Email, regErr.VatID, regErr.Error, regErr.CreatedAt,
	)
	if err != nil {
		return errl.Errorf("failed to insert registration error: %w", err)
	}
	return nil
}

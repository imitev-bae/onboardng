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

// RegistrationFile represents a file uploaded by a user during registration
type RegistrationFile struct {
	FileID         string    `json:"file_id"`
	RegistrationID string    `json:"registration_id"`
	VatID          string    `json:"vat_id"`
	Name           string    `json:"name"`
	MimeType       string    `json:"mime_type"`
	Size           int64     `json:"size"`
	Status         string    `json:"status"`
	Content        []byte    `json:"content"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
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
	);
	CREATE TABLE IF NOT EXISTS registration_files (
		file_id TEXT UNIQUE,
		registration_id TEXT,
		vat_id TEXT,
		name TEXT,
		mime_type TEXT,
		size INTEGER,
		status TEXT,
		content BLOB,
		created_at DATETIME,
		updated_at DATETIME
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

var ErrorAlreadyExists = errors.New("already exists")

func (s *Service) SaveRegistration(reg *RegistrationRecord) error {
	insertQuery := `
	INSERT INTO registrations (
		registration_id, email, first_name, last_name, company_name, country, vat_id,
		street_address, postal_code,
		created_at, updated_at, notified, issued, tmf_registered
	) VALUES (
		:registration_id, :email, :first_name, :last_name, :company_name, :country, :vat_id,
		:street_address, :postal_code,
		:created_at, :updated_at, :notified, :issued, :tmf_registered
	)`

	now := time.Now()
	reg.CreatedAt = now
	reg.UpdatedAt = now
	reg.Notified = false
	reg.Issued = false
	reg.TMFRegistered = false

	// Check if a registration with the same VAT ID already exists
	oldRegVat, err := s.GetRegistrationByVatID(reg.VatID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return errl.Errorf("failed to search for VAT ID: %w", err)
	}

	// Check if a registration with the same email already exists
	oldRegEmail, err := s.GetRegistrationByEmail(reg.Email)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return errl.Errorf("failed to search for email: %w", err)
	}

	switch s.runtime {

	case configuration.Production:

		// In production, we reject if either the company (vat_id) or the individual (email) already exists.
		if oldRegVat != nil {
			return errl.Errorf("%w: company with VAT ID %s already registered", ErrorAlreadyExists, reg.VatID)
		}
		if oldRegEmail != nil {
			return errl.Errorf("%w: email %s already registered", ErrorAlreadyExists, reg.Email)
		}

		// No conflicts, insert as new
		_, err = s.conn.Exec(insertQuery,
			sql.Named("registration_id", reg.RegistrationID),
			sql.Named("email", reg.Email),
			sql.Named("first_name", reg.FirstName),
			sql.Named("last_name", reg.LastName),
			sql.Named("company_name", reg.CompanyName),
			sql.Named("country", reg.Country),
			sql.Named("vat_id", reg.VatID),
			sql.Named("street_address", reg.StreetAddress),
			sql.Named("postal_code", reg.PostalCode),
			sql.Named("created_at", reg.CreatedAt),
			sql.Named("updated_at", reg.UpdatedAt),
			sql.Named("notified", reg.Notified),
			sql.Named("issued", reg.Issued),
			sql.Named("tmf_registered", reg.TMFRegistered),
		)
		if err != nil {
			return errl.Errorf("failed to insert registration: %w", err)
		}
		return nil

	case configuration.Development, configuration.Preproduction:
		// In Dev/Pre, if BOTH email and VAT ID match an existing record, we overwrite it (amend)
		if oldRegVat != nil && oldRegEmail != nil && oldRegVat.RegistrationID == oldRegEmail.RegistrationID {
			slog.Info("Registration exists with same email and VAT ID, amending", "environment", s.runtime, "vat_id", reg.VatID, "email", reg.Email)
			// Reuse the existing registration ID
			reg.RegistrationID = oldRegVat.RegistrationID
			return s.AmendRegistration(reg)
		}

		// Otherwise, if either exists, it's a conflict since they don't belong to the same record
		if oldRegVat != nil {
			return errl.Errorf("%w: company with VAT ID %s already registered with a different email", ErrorAlreadyExists, reg.VatID)
		}
		if oldRegEmail != nil {
			return errl.Errorf("%w: user with email %s already registered with a different VAT ID", ErrorAlreadyExists, reg.Email)
		}

		// No conflicts, insert as new
		_, err := s.conn.Exec(insertQuery,
			sql.Named("registration_id", reg.RegistrationID),
			sql.Named("email", reg.Email),
			sql.Named("first_name", reg.FirstName),
			sql.Named("last_name", reg.LastName),
			sql.Named("company_name", reg.CompanyName),
			sql.Named("country", reg.Country),
			sql.Named("vat_id", reg.VatID),
			sql.Named("street_address", reg.StreetAddress),
			sql.Named("postal_code", reg.PostalCode),
			sql.Named("created_at", reg.CreatedAt),
			sql.Named("updated_at", reg.UpdatedAt),
			sql.Named("notified", reg.Notified),
			sql.Named("issued", reg.Issued),
			sql.Named("tmf_registered", reg.TMFRegistered),
		)
		if err != nil {
			return errl.Errorf("failed to insert registration: %w", err)
		}
		return nil

	}

	// Should never happen, return an error
	return fmt.Errorf("unknown runtime environment: %s", s.runtime)
}

// UpdateRegistrationStatus updates the status flags of a registration.
// Other fields are not updated, except the timestamp of the update.
func (s *Service) UpdateRegistrationStatus(reg *RegistrationRecord) error {
	reg.UpdatedAt = time.Now()
	query := `
	UPDATE registrations SET
		updated_at = :updated_at,
		notified = :notified,
		issued = :issued,
		tmf_registered = :tmf_registered
	WHERE registration_id = :registration_id AND email = :email`
	_, err := s.conn.Exec(query,
		sql.Named("updated_at", reg.UpdatedAt),
		sql.Named("notified", reg.Notified),
		sql.Named("issued", reg.Issued),
		sql.Named("tmf_registered", reg.TMFRegistered),
		sql.Named("registration_id", reg.RegistrationID),
		sql.Named("email", reg.Email),
	)
	if err != nil {
		return errl.Errorf("failed to update registration: %w", err)
	}
	return nil
}

// AmendRegistration updates all fields of an existing registration, except the vat_id, email and registration_id.
func (s *Service) AmendRegistration(reg *RegistrationRecord) error {
	reg.UpdatedAt = time.Now()
	query := `
	UPDATE registrations SET
		first_name = :first_name,
		last_name = :last_name,
		company_name = :company_name,
		country = :country,
		street_address = :street_address,
		postal_code = :postal_code,
		updated_at = :updated_at,
		notified = :notified,
		issued = :issued,
		tmf_registered = :tmf_registered
	WHERE vat_id = :vat_id`
	_, err := s.conn.Exec(query,
		sql.Named("first_name", reg.FirstName),
		sql.Named("last_name", reg.LastName),
		sql.Named("company_name", reg.CompanyName),
		sql.Named("country", reg.Country),
		sql.Named("street_address", reg.StreetAddress),
		sql.Named("postal_code", reg.PostalCode),
		sql.Named("updated_at", reg.UpdatedAt),
		sql.Named("notified", reg.Notified),
		sql.Named("issued", reg.Issued),
		sql.Named("tmf_registered", reg.TMFRegistered),
		sql.Named("vat_id", reg.VatID),
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
	LIMIT :limit OFFSET :offset`

	rows, err := s.conn.Query(query,
		sql.Named("limit", limit),
		sql.Named("offset", offset),
	)
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
	WHERE vat_id = :vat_id AND email = :email`

	var reg RegistrationRecord
	err := s.conn.QueryRow(query,
		sql.Named("vat_id", vatID),
		sql.Named("email", email),
	).Scan(
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
	WHERE vat_id = :vat_id`

	var reg RegistrationRecord
	err := s.conn.QueryRow(query, sql.Named("vat_id", vatID)).Scan(
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
	WHERE email = :email`

	var reg RegistrationRecord
	err := s.conn.QueryRow(query, sql.Named("email", email)).Scan(
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
	) VALUES (:registration_id, :email, :vat_id, :error, :created_at)`

	regErr.CreatedAt = time.Now()

	_, err := s.conn.Exec(query,
		sql.Named("registration_id", regErr.RegistrationID),
		sql.Named("email", regErr.Email),
		sql.Named("vat_id", regErr.VatID),
		sql.Named("error", regErr.Error),
		sql.Named("created_at", regErr.CreatedAt),
	)
	if err != nil {
		return errl.Errorf("failed to insert registration error: %w", err)
	}
	return nil
}

func (s *Service) SaveRegistrationFile(f *RegistrationFile) error {
	query := `
	INSERT INTO registration_files (
		file_id, registration_id, vat_id, name, mime_type, size, status, content, created_at, updated_at
	) VALUES (
		:file_id, :registration_id, :vat_id, :name, :mime_type, :size, :status, :content, :created_at, :updated_at
	)`

	now := time.Now()
	f.CreatedAt = now
	f.UpdatedAt = now
	if f.Status == "" {
		f.Status = "uploaded" // Default status
	}

	_, err := s.conn.Exec(query,
		sql.Named("file_id", f.FileID),
		sql.Named("registration_id", f.RegistrationID),
		sql.Named("vat_id", f.VatID),
		sql.Named("name", f.Name),
		sql.Named("mime_type", f.MimeType),
		sql.Named("size", f.Size),
		sql.Named("status", f.Status),
		sql.Named("content", f.Content),
		sql.Named("created_at", f.CreatedAt),
		sql.Named("updated_at", f.UpdatedAt),
	)
	if err != nil {
		return errl.Errorf("failed to insert registration file: %w", err)
	}
	return nil
}

func (s *Service) GetRegistrationFilesByVatID(vatID string) ([]RegistrationFile, error) {
	query := `
	SELECT 
		file_id, registration_id, vat_id, name, mime_type, size, status, content, created_at, updated_at
	FROM registration_files
	WHERE vat_id = :vat_id
	ORDER BY created_at ASC`

	rows, err := s.conn.Query(query, sql.Named("vat_id", vatID))
	if err != nil {
		return nil, errl.Errorf("failed to get registration files: %w", err)
	}
	defer rows.Close()

	var files []RegistrationFile
	for rows.Next() {
		var f RegistrationFile
		err := rows.Scan(
			&f.FileID, &f.RegistrationID, &f.VatID, &f.Name, &f.MimeType, &f.Size, &f.Status, &f.Content, &f.CreatedAt, &f.UpdatedAt,
		)
		if err != nil {
			return nil, errl.Errorf("failed to scan registration file row: %w", err)
		}
		files = append(files, f)
	}

	if err = rows.Err(); err != nil {
		return nil, errl.Errorf("failed to iterate over registration file rows: %w", err)
	}

	return files, nil
}

func (s *Service) UpdateRegistrationFileStatus(fileID string, status string) error {
	query := `
	UPDATE registration_files SET
		status = :status,
		updated_at = :updated_at
	WHERE file_id = :file_id`

	_, err := s.conn.Exec(query,
		sql.Named("status", status),
		sql.Named("updated_at", time.Now()),
		sql.Named("file_id", fileID),
	)
	if err != nil {
		return errl.Errorf("failed to update registration file status: %w", err)
	}
	return nil
}

func (s *Service) DeleteRegistrationFile(fileID string) error {
	query := `DELETE FROM registration_files WHERE file_id = :file_id`
	_, err := s.conn.Exec(query, sql.Named("file_id", fileID))
	if err != nil {
		return errl.Errorf("failed to delete registration file: %w", err)
	}
	return nil
}

func (s *Service) GetRegistrationFile(fileID string) (*RegistrationFile, error) {
	query := `
	SELECT 
		file_id, registration_id, vat_id, name, mime_type, size, status, content, created_at, updated_at
	FROM registration_files
	WHERE file_id = :file_id`

	var f RegistrationFile
	err := s.conn.QueryRow(query, sql.Named("file_id", fileID)).Scan(
		&f.FileID, &f.RegistrationID, &f.VatID, &f.Name, &f.MimeType, &f.Size, &f.Status, &f.Content, &f.CreatedAt, &f.UpdatedAt,
	)
	if err != nil {
		return nil, errl.Errorf("failed to get registration file: %w", err)
	}
	return &f, nil
}

func (s *Service) UpdateRegistrationFileContent(fileID string, content []byte) error {
	query := `
	UPDATE registration_files SET
		content = :content,
		updated_at = :updated_at
	WHERE file_id = :file_id`

	_, err := s.conn.Exec(query,
		sql.Named("content", content),
		sql.Named("updated_at", time.Now()),
		sql.Named("file_id", fileID),
	)
	if err != nil {
		return errl.Errorf("failed to update registration file content: %w", err)
	}
	return nil
}

func (s *Service) GetRegistrationErrors(limit, offset int) ([]RegistrationError, error) {
	query := `
	SELECT 
		registration_id, email, vat_id, error, created_at
	FROM registration_errors
	ORDER BY created_at DESC
	LIMIT :limit OFFSET :offset`

	rows, err := s.conn.Query(query,
		sql.Named("limit", limit),
		sql.Named("offset", offset),
	)
	if err != nil {
		return nil, errl.Errorf("failed to get registration errors: %w", err)
	}
	defer rows.Close()

	var errorsList []RegistrationError
	for rows.Next() {
		var e RegistrationError
		err := rows.Scan(
			&e.RegistrationID, &e.Email, &e.VatID, &e.Error, &e.CreatedAt,
		)
		if err != nil {
			return nil, errl.Errorf("failed to scan registration error row: %w", err)
		}
		errorsList = append(errorsList, e)
	}

	if err = rows.Err(); err != nil {
		return nil, errl.Errorf("failed to iterate over registration error rows: %w", err)
	}

	return errorsList, nil
}

func (s *Service) GetRegistrationFiles(limit, offset int) ([]RegistrationFile, error) {
	query := `
	SELECT 
		file_id, registration_id, vat_id, name, mime_type, size, status, content, created_at, updated_at
	FROM registration_files
	ORDER BY created_at DESC
	LIMIT :limit OFFSET :offset`

	rows, err := s.conn.Query(query,
		sql.Named("limit", limit),
		sql.Named("offset", offset),
	)
	if err != nil {
		return nil, errl.Errorf("failed to get registration files: %w", err)
	}
	defer rows.Close()

	var files []RegistrationFile
	for rows.Next() {
		var f RegistrationFile
		err := rows.Scan(
			&f.FileID, &f.RegistrationID, &f.VatID, &f.Name, &f.MimeType, &f.Size, &f.Status, &f.Content, &f.CreatedAt, &f.UpdatedAt,
		)
		if err != nil {
			return nil, errl.Errorf("failed to scan registration file row: %w", err)
		}
		files = append(files, f)
	}

	if err = rows.Err(); err != nil {
		return nil, errl.Errorf("failed to iterate over registration file rows: %w", err)
	}

	return files, nil
}

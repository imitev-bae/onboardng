package db

import (
	"os"
	"testing"

	"github.com/hesusruiz/onboardng/internal/configuration"
)

func TestRegistrationConstraints(t *testing.T) {
	dbPath := "data/onboarding_test.db"
	os.Remove(dbPath) // Start fresh
	defer os.Remove(dbPath)

	// --- PRODUCTION ENVIRONMENT TESTS ---
	t.Log("Testing Production strict constraints")
	svcPro, err := NewService(configuration.Production, dbPath)
	if err != nil {
		t.Fatalf("failed to create pro service: %v", err)
	}

	reg1 := &RegistrationRecord{
		RegistrationID: "reg-001",
		Email:          "user@example.com",
		VatID:          "VAT123",
		CompanyName:    "Company A",
	}

	// 1. First registration should succeed
	if err := svcPro.SaveRegistration(reg1); err != nil {
		t.Fatalf("first registration failed: %v", err)
	}

	// 2. Registering same company (VAT) with different email should FAIL in production
	reg2 := &RegistrationRecord{
		RegistrationID: "reg-002",
		Email:          "other@example.com",
		VatID:          "VAT123",
		CompanyName:    "Company A Again",
	}
	if err := svcPro.SaveRegistration(reg2); err == nil {
		t.Errorf("expected error for duplicate VAT ID in production, got nil")
	}

	// 3. Registering DIFFERENT company (VAT) with SAME email should FAIL in production
	reg3 := &RegistrationRecord{
		RegistrationID: "reg-003",
		Email:          "user@example.com",
		VatID:          "VAT456",
		CompanyName:    "Company B",
	}
	if err := svcPro.SaveRegistration(reg3); err == nil {
		t.Errorf("expected error for duplicate email in production, got nil")
	}
	svcPro.Close()

	// --- DEVELOPMENT ENVIRONMENT TESTS ---
	t.Log("Testing Development overwrite and conflict logic")
	svcDev, err := NewService(configuration.Development, dbPath)
	if err != nil {
		t.Fatalf("failed to create dev service: %v", err)
	}
	defer svcDev.Close()

	// 4. Update Company A (VAT123) with SAME email in Dev mode -> Should OVERWRITE
	reg4 := &RegistrationRecord{
		RegistrationID: "reg-004",
		Email:          "user@example.com",
		VatID:          "VAT123",
		CompanyName:    "Company A Updated",
	}
	if err := svcDev.SaveRegistration(reg4); err != nil {
		t.Fatalf("failed to update company in dev mode: %v", err)
	}

	// Verify the update (same RegID should be preserved or reused)
	updatedReg, err := svcDev.GetRegistrationByVatID("VAT123")
	if err != nil {
		t.Fatalf("failed to get updated registration: %v", err)
	}
	if updatedReg.CompanyName != "Company A Updated" {
		t.Errorf("expected CompanyName to be updated to 'Company A Updated', got %s", updatedReg.CompanyName)
	}
	if updatedReg.RegistrationID != "reg-001" {
		t.Errorf("expected RegistrationID to be reused (reg-001), got %s", updatedReg.RegistrationID)
	}

	// 5. Conflicting registration (SAME email, DIFFERENT vat_id) in Dev mode -> Should FAIL
	reg5 := &RegistrationRecord{
		RegistrationID: "reg-005",
		Email:          "user@example.com",
		VatID:          "VAT999",
		CompanyName:    "Email Conflict Company",
	}
	if err := svcDev.SaveRegistration(reg5); err == nil {
		t.Errorf("expected error for conflicting email in dev mode, got nil")
	}

	// 6. Conflicting registration (DIFFERENT email, SAME vat_id) in Dev mode -> Should FAIL
	reg6 := &RegistrationRecord{
		RegistrationID: "reg-006",
		Email:          "new-user@example.com",
		VatID:          "VAT123",
		CompanyName:    "VAT Conflict Company",
	}
	if err := svcDev.SaveRegistration(reg6); err == nil {
		t.Errorf("expected error for conflicting VAT ID in dev mode, got nil")
	}
}

func TestRegistrationErrors(t *testing.T) {
	dbPath := "data/onboarding_errors_test.db"
	os.Remove(dbPath) // Start fresh
	defer os.Remove(dbPath)

	svc, err := NewService(configuration.Development, dbPath)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	defer svc.Close()

	errEntry := &RegistrationError{
		RegistrationID: "reg-err-001",
		Email:          "error@example.com",
		VatID:          "VAT_ERR_1",
		Error:          "Something went wrong",
	}

	if err := svc.SaveRegistrationError(errEntry); err != nil {
		t.Fatalf("failed to save registration error: %v", err)
	}

	// Verify that the error was saved by manually querying the (private) connection
	// Since we don't have a GetRegistrationErrors method yet, we use a raw query
	var count int
	err = svc.conn.QueryRow("SELECT COUNT(*) FROM registration_errors WHERE registration_id = ?", "reg-err-001").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query registration errors: %v", err)
	}

	if count != 1 {
		t.Errorf("expected 1 registration error, got %d", count)
	}
}

func TestRegistrationStatusFields(t *testing.T) {
	dbPath := "data/onboarding_status_test.db"
	os.Remove(dbPath) // Start fresh
	defer os.Remove(dbPath)

	svc, err := NewService(configuration.Development, dbPath)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	defer svc.Close()

	reg := &RegistrationRecord{
		RegistrationID: "reg-status-001",
		Email:          "status@example.com",
		VatID:          "VAT_STATUS_1",
		CompanyName:    "Status Co",
	}

	// 1. Initial save should have all status as false
	if err := svc.SaveRegistration(reg); err != nil {
		t.Fatalf("failed to save registration: %v", err)
	}

	savedReg, err := svc.GetRegistrationByVatID(reg.VatID)
	if err != nil {
		t.Fatalf("failed to get registration: %v", err)
	}

	if savedReg.Notified || savedReg.Issued || savedReg.TMFRegistered {
		t.Errorf("expected all status fields to be false, got Notified=%v, Issued=%v, TMFRegistered=%v",
			savedReg.Notified, savedReg.Issued, savedReg.TMFRegistered)
	}

	// 2. Update status
	savedReg.Notified = true
	savedReg.Issued = true
	if err := svc.UpdateRegistrationStatus(savedReg); err != nil {
		t.Fatalf("failed to update status: %v", err)
	}

	updatedReg, err := svc.GetRegistrationByVatID(reg.VatID)
	if err != nil {
		t.Fatalf("failed to get registration: %v", err)
	}

	if !updatedReg.Notified || !updatedReg.Issued || updatedReg.TMFRegistered {
		t.Errorf("expected updated status fields, got Notified=%v, Issued=%v, TMFRegistered=%v",
			updatedReg.Notified, updatedReg.Issued, updatedReg.TMFRegistered)
	}
}

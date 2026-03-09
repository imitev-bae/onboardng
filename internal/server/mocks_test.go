package server

import (
	"github.com/hesusruiz/onboardng/credissuance"
	"github.com/hesusruiz/onboardng/internal/db"
)

type MockDB struct {
	SaveRegistrationFunc         func(reg *db.RegistrationRecord) error
	UpdateRegistrationStatusFunc func(reg *db.RegistrationRecord) error
	SaveRegistrationErrorFunc    func(regErr *db.RegistrationError) error
	GetRegistrationByVatIDFunc   func(vatID string) (*db.RegistrationRecord, error)
	GetRegistrationByEmailFunc   func(email string) (*db.RegistrationRecord, error)
}

func (m *MockDB) SaveRegistration(reg *db.RegistrationRecord) error {
	return m.SaveRegistrationFunc(reg)
}
func (m *MockDB) UpdateRegistrationStatus(reg *db.RegistrationRecord) error {
	return m.UpdateRegistrationStatusFunc(reg)
}
func (m *MockDB) SaveRegistrationError(regErr *db.RegistrationError) error {
	return m.SaveRegistrationErrorFunc(regErr)
}
func (m *MockDB) GetRegistrationByVatID(vatID string) (*db.RegistrationRecord, error) {
	return m.GetRegistrationByVatIDFunc(vatID)
}
func (m *MockDB) GetRegistrationByEmail(email string) (*db.RegistrationRecord, error) {
	return m.GetRegistrationByEmailFunc(email)
}

type MockMail struct {
	SendVerificationCodeFunc func(email string, code string) error
	SendWelcomeEmailFunc     func(reg *db.RegistrationRecord) error
	SendIssuerErrorFunc      func(reg *db.RegistrationRecord, payload string, errorMsg string) error
}

func (m *MockMail) SendVerificationCode(email string, code string) error {
	return m.SendVerificationCodeFunc(email, code)
}
func (m *MockMail) SendWelcomeEmail(reg *db.RegistrationRecord) error {
	return m.SendWelcomeEmailFunc(reg)
}
func (m *MockMail) SendIssuerError(reg *db.RegistrationRecord, payload string, errorMsg string) error {
	return m.SendIssuerErrorFunc(reg, payload, errorMsg)
}

type MockIssuance struct {
	GetAccessTokenFunc           func() (string, error)
	TMFGetOrganizationByELSIFunc func(accessToken string, elsi string) ([]credissuance.Organization, error)
	TMFDeleteOrganizationFunc    func(accessToken string, id string) error
	LEARIssuanceRequestFunc      func(accessToken string, learCredData *credissuance.LEARIssuanceRequestBody) ([]byte, error)
	TMFCreateOrganizationFunc    func(accessToken string, org *credissuance.Organization_Create) (*credissuance.Organization, error)
}

func (m *MockIssuance) GetAccessToken() (string, error) {
	return m.GetAccessTokenFunc()
}
func (m *MockIssuance) TMFGetOrganizationByELSI(accessToken string, elsi string) ([]credissuance.Organization, error) {
	return m.TMFGetOrganizationByELSIFunc(accessToken, elsi)
}
func (m *MockIssuance) TMFDeleteOrganization(accessToken string, id string) error {
	return m.TMFDeleteOrganizationFunc(accessToken, id)
}
func (m *MockIssuance) LEARIssuanceRequest(accessToken string, learCredData *credissuance.LEARIssuanceRequestBody) ([]byte, error) {
	return m.LEARIssuanceRequestFunc(accessToken, learCredData)
}
func (m *MockIssuance) TMFCreateOrganization(accessToken string, org *credissuance.Organization_Create) (*credissuance.Organization, error) {
	return m.TMFCreateOrganizationFunc(accessToken, org)
}

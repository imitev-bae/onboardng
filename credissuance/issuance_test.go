package credissuance

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"

	"github.com/hesusruiz/onboardng/internal/configuration"
	"gopkg.in/yaml.v3"
)

type MockRoundTripper struct {
	OriginalTransport http.RoundTripper
	Responses         map[string]string
}

func (m *MockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	url := req.URL.String()
	if respBody, ok := m.Responses[url]; ok {
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(bytes.NewBufferString(respBody)),
			Header:     make(http.Header),
		}, nil
	}
	if m.OriginalTransport != nil {
		return m.OriginalTransport.RoundTrip(req)
	}
	return nil, fmt.Errorf("no mock response for %s", url)
}

type TestConfig struct {
	DestDir      string                             `yaml:"dest_dir"`
	SrcDir       string                             `yaml:"src_dir"`
	AppName      string                             `yaml:"app_name"`
	Environments map[string]configuration.EnvConfig `yaml:"environments"`
}

func TestLEARIssuanceRequest(t *testing.T) {

	// Load testconfiguration
	configData, err := os.ReadFile("testdata/config.yaml")
	if err != nil {
		t.Fatalf("failed to read config.yaml: %v", err)
	}
	var cfg TestConfig
	if err := yaml.Unmarshal(configData, &cfg); err != nil {
		t.Fatalf("failed to parse config.yaml: %v", err)
	}

	// Get the environment config
	envCfg := cfg.Environments["dev"]

	// Setup issuer
	issuerCfg := configuration.EnvConfig{
		PrivateKeyFile:        envCfg.PrivateKeyFile,
		MachineCredentialFile: envCfg.MachineCredentialFile,
		MyDidkey:              envCfg.MyDidkey,
		Verifier: configuration.VerifierConfig{
			URL:           envCfg.Verifier.URL,
			TokenEndpoint: envCfg.Verifier.TokenEndpoint,
		},
		Issuer: configuration.IssuerConfig{
			CredentialIssuancePath: envCfg.Issuer.CredentialIssuancePath,
		},
	}
	issuer, err := NewLEARIssuance(issuerCfg)
	if err != nil {
		t.Fatalf("NewLEARIssuance failed: %v", err)
	}

	// Use the first credential from sample_credentials.go
	cred := Cred1()

	// Perform the issuance request
	resp, err := issuer.LEARIssuanceRequest(cred)
	if err != nil {
		t.Fatalf("LEARIssuanceRequest failed: %v", err)
	}

	// Verify the response
	expectedResponse := `{"credential": "mock_credential"}`
	if string(resp) != expectedResponse {
		t.Errorf("expected response %s, got %s", expectedResponse, string(resp))
	}
}

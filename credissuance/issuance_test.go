package credissuance

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
)

func TestNewLEARIssuance(t *testing.T) {
	// Helper function to create a temporary private key file
	createTempPrivateKey := func(t *testing.T) string {
		t.Helper()
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatalf("failed to generate private key: %v", err)
		}

		der, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			t.Fatalf("failed to marshal private key: %v", err)
		}

		block := &pem.Block{
			Type:  "PRIVATE KEY",
			Bytes: der,
		}

		tmpFile, err := os.CreateTemp("", "private_key_*.pem")
		if err != nil {
			t.Fatalf("failed to create temp file: %v", err)
		}
		defer tmpFile.Close()

		if err := pem.Encode(tmpFile, block); err != nil {
			t.Fatalf("failed to encode private key to pem: %v", err)
		}
		return tmpFile.Name()
	}

	// Helper function to create a temporary dummy file
	createTempFile := func(t *testing.T, content string) string {
		t.Helper()
		tmpFile, err := os.CreateTemp("", "dummy_*.txt")
		if err != nil {
			t.Fatalf("failed to create temp file: %v", err)
		}
		defer tmpFile.Close()

		if _, err := tmpFile.WriteString(content); err != nil {
			t.Fatalf("failed to write to temp file: %v", err)
		}
		return tmpFile.Name()
	}

	t.Run("Valid configuration", func(t *testing.T) {
		privateKeyPath := createTempPrivateKey(t)
		defer os.Remove(privateKeyPath)

		machineCredPath := createTempFile(t, "dummy_machine_credential")
		defer os.Remove(machineCredPath)

		config := Config{
			PrivateKeyFilePEM:     privateKeyPath,
			MachineCredentialFile: machineCredPath,
			Verifier: VerifierConfig{
				TokenEndpoint: "https://verifier.example.com/token",
				URL:           "https://verifier.example.com",
			},
			MyDidkey: "did:key:example",
			Issuer: IssuerConfig{
				CredentialIssuancePath: "https://issuer.example.com/issue",
			},
		}

		issuer, err := NewLEARIssuance(config)
		if err != nil {
			t.Fatalf("NewLEARIssuance failed: %v", err)
		}

		if issuer == nil {
			t.Fatal("expected non-nil issuer")
		}

		if issuer.machineCredential != "dummy_machine_credential" {
			t.Errorf("expected machine credential 'dummy_machine_credential', got '%s'", issuer.machineCredential)
		}

		if issuer.verifierTokenEndpoint != "https://verifier.example.com/token" {
			t.Errorf("expected verifierTokenEndpoint 'https://verifier.example.com/token', got '%s'", issuer.verifierTokenEndpoint)
		}

		if issuer.verifierURL != "https://verifier.example.com" {
			t.Errorf("expected verifierURL 'https://verifier.example.com', got '%s'", issuer.verifierURL)
		}

		if issuer.myDidkey != "did:key:example" {
			t.Errorf("expected myDidkey 'did:key:example', got '%s'", issuer.myDidkey)
		}

		if issuer.credentialIssuancePath != "https://issuer.example.com/issue" {
			t.Errorf("expected credentialIssuancePath 'https://issuer.example.com/issue', got '%s'", issuer.credentialIssuancePath)
		}

		// Helper access to private fields for verification if needed,
		// but typically we test behavior. Since struct fields are private,
		// we can't easily access them without reflection or just trusting
		// the function logic which is simple assignment.
		// However, we can check basic properties if exposed or just rely on no error.
	})

	t.Run("Missing private key file", func(t *testing.T) {
		config := Config{
			PrivateKeyFilePEM:     "non_existent_file.pem",
			MachineCredentialFile: "dummy.txt",
		}
		_, err := NewLEARIssuance(config)
		if err == nil {
			t.Fatal("expected error for missing private key file, got nil")
		}
	})

	t.Run("Missing machine credential file", func(t *testing.T) {
		privateKeyPath := createTempPrivateKey(t)
		defer os.Remove(privateKeyPath)

		config := Config{
			PrivateKeyFilePEM:     privateKeyPath,
			MachineCredentialFile: "non_existent_file.txt",
		}

		_, err := NewLEARIssuance(config)
		if err == nil {
			t.Fatal("expected error for missing machine credential file, got nil")
		}
	})
}

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

func TestLEARIssuanceRequest(t *testing.T) {
	// Save the original transport
	originalTransport := http.DefaultClient.Transport
	defer func() { http.DefaultClient.Transport = originalTransport }()

	// Define the mock responses
	responses := map[string]string{
		"https://verifier.dome-marketplace-sbx.org/oidc/token":                         `{"access_token": "mock_access_token"}`,
		"https://issuer.dome-marketplace-sbx.org/issuer-api/vci/v1/issuances/external": `{"credential": "mock_credential"}`,
	}

	// Set the mock transport
	http.DefaultClient.Transport = &MockRoundTripper{
		OriginalTransport: originalTransport,
		Responses:         responses,
	}

	// Define the configuration with adjusted paths relative to the test file
	config := Config{
		ApiUrl:                "http://localhost:8080",
		Debug:                 true,
		PrivateKeyFilePEM:     "../config/credentials/development/sbx_didkey_priv.pem",
		MachineCredentialFile: "../config/credentials/development/sbx_lear_credential_machine.txt",
		MyDidkey:              "did:key:zDnaeb3th2TDPrQTnM19Mdy543xFetwCpqjmcGBFgisqYKuL4",
		Verifier: VerifierConfig{
			URL:           "https://verifier.dome-marketplace-sbx.org",
			TokenEndpoint: "https://verifier.dome-marketplace-sbx.org/oidc/token",
		},
		Issuer: IssuerConfig{
			CredentialIssuancePath: "https://issuer.dome-marketplace-sbx.org/issuer-api/vci/v1/issuances/external",
		},
	}

	// Initialize the LEARIssuance struct
	issuer, err := NewLEARIssuance(config)
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

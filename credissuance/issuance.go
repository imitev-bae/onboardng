package credissuance

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
)

type LEARIssuanceRequestBody struct {
	Schema        string  `json:"schema,omitempty"`
	OperationMode string  `json:"operation_mode,omitempty"`
	Format        string  `json:"format,omitempty"`
	ResponseUri   string  `json:"response_uri,omitempty"`
	Payload       Payload `json:"payload,omitempty"`
}

func ParseLEARIssuanceRequestBody(body []byte) (*LEARIssuanceRequestBody, error) {
	var req LEARIssuanceRequestBody
	err := json.Unmarshal(body, &req)
	if err != nil {
		return nil, err
	}
	return &req, nil
}

type Payload struct {
	Mandator Mandator `json:"mandator"`
	Mandatee Mandatee `json:"mandatee"`
	Power    []Power  `json:"power,omitempty"`
}

type Mandator struct {
	OrganizationIdentifier string `json:"organizationIdentifier,omitempty"`
	Organization           string `json:"organization,omitempty"`
	Country                string `json:"country,omitempty"`
	CommonName             string `json:"commonName,omitempty"`
	EmailAddress           string `json:"emailAddress,omitempty"`
	SerialNumber           string `json:"serialNumber,omitempty"`
}

type Mandatee struct {
	FirstName   string `json:"firstName,omitempty"`
	LastName    string `json:"lastName,omitempty"`
	Nationality string `json:"nationality,omitempty"`
	Email       string `json:"email,omitempty"`
}

type Power struct {
	Type     string  `json:"type,omitempty"`
	Domain   string  `json:"domain,omitempty"`
	Function string  `json:"function,omitempty"`
	Action   Strings `json:"action,omitempty"`
}

// The "action" claim can either be a single string or an array.
// We need to serialize the claim as a single string if the array has only one element
type Strings []string

func (s Strings) MarshalJSON() (b []byte, err error) {
	if len(s) == 1 {
		return json.Marshal(s[0])
	}

	return json.Marshal([]string(s))
}

type Config struct {
	ApiUrl string `yaml:"api_url"`
	Debug  bool   `yaml:"debug"`

	PrivateKeyFilePEM     string `yaml:"privateKeyFilePEM,omitempty"`
	MachineCredentialFile string `yaml:"machineCredentialFile,omitempty"`

	MyDidkey string         `yaml:"mydidkey,omitempty"`
	Verifier VerifierConfig `yaml:"verifier"`
	Issuer   IssuerConfig   `yaml:"issuer"`
}

type VerifierConfig struct {
	URL           string `yaml:"url,omitempty"`
	TokenEndpoint string `yaml:"token_endpoint,omitempty"`
}

type IssuerConfig struct {
	CredentialIssuancePath string `yaml:"credentialIssuancePath,omitempty"`
}

type LEARIssuance struct {
	privateKey        *ecdsa.PrivateKey
	machineCredential string

	verifierTokenEndpoint  string
	verifierURL            string
	myDidkey               string
	credentialIssuancePath string
}

func NewLEARIssuance(config Config) (*LEARIssuance, error) {

	// Read the private key
	pemBytesRaw, err := os.ReadFile(config.PrivateKeyFilePEM)
	if err != nil {
		return nil, err
	}

	// Decode from the PEM format
	pemBlock, _ := pem.Decode(pemBytesRaw)
	privKeyAny, err := x509.ParsePKCS8PrivateKey(pemBlock.Bytes)
	if err != nil {
		return nil, err
	}
	privateKey := privKeyAny.(*ecdsa.PrivateKey)

	// Read the LEARCredentialMachine
	buf, err := os.ReadFile(config.MachineCredentialFile)
	if err != nil {
		return nil, err
	}
	machineCredential := string(buf)

	l := &LEARIssuance{
		privateKey:        privateKey,
		machineCredential: machineCredential,
	}

	l.verifierTokenEndpoint = config.Verifier.TokenEndpoint
	l.verifierURL = config.Verifier.URL
	l.myDidkey = config.MyDidkey
	l.credentialIssuancePath = config.Issuer.CredentialIssuancePath

	return l, nil

}

func (l *LEARIssuance) LEARIssuanceRequest(learCredData *LEARIssuanceRequestBody) ([]byte, error) {

	// Get an access token from the Verifier
	access_token, err := TokenRequest(
		l.verifierTokenEndpoint,
		l.machineCredential,
		l.myDidkey,
		l.verifierURL,
		l.privateKey,
	)
	if err != nil {
		return nil, err
	}

	fmt.Printf("Access Token: %v\n", access_token)
	fmt.Printf("Issuance Endpoint: %v\n", l.credentialIssuancePath)

	// The request buffer
	buf, err := json.Marshal(learCredData)
	if err != nil {
		return nil, err
	}
	requestBody := bytes.NewBuffer(buf)

	// The request to send
	req, _ := http.NewRequest("POST", l.credentialIssuancePath, requestBody)
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", "Bearer "+access_token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 399 {
		fmt.Println("Error calling LEAR Issuance Endpoint:", resp.Status)
		return nil, fmt.Errorf("error calling LEAR Issuance Endpoint: %v", resp.Status)
	}

	ResponseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return ResponseBody, nil
}

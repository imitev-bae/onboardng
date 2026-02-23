package credissuance

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/hesusruiz/onboardng/internal/configuration"
	"github.com/mr-tron/base58/base58"
)

type LEARIssuanceRequestBody struct {
	Schema        string  `json:"schema,omitempty"`
	OperationMode string  `json:"operation_mode,omitempty"`
	Format        string  `json:"format,omitempty"`
	ResponseUri   string  `json:"response_uri,omitempty"`
	Payload       Payload `json:"payload"`
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

type LEARIssuance struct {
	privateKey        *ecdsa.PrivateKey
	machineCredential string

	verifierTokenEndpoint  string
	verifierURL            string
	myDidkey               string
	credentialIssuancePath string
}

func NewLEARIssuance(config configuration.EnvConfig) (*LEARIssuance, error) {

	// Read the private key
	pemBytesRaw, err := os.ReadFile(config.PrivateKeyFile)
	if err != nil {
		return nil, err
	}

	// Strip any '0x' or '0X' prefix from the key and decode it
	hexKey := strings.TrimPrefix(string(pemBytesRaw), "0x")
	hexKey = strings.TrimPrefix(hexKey, "0X")
	dBytes, _ := hex.DecodeString(hexKey)

	// Create ECDSA Private Key from the raw private key
	curve := elliptic.P256()
	privateKey, err := ecdsa.ParseRawPrivateKey(curve, dBytes)
	if err != nil {
		return nil, err
	}

	// For safety, we are going to derive the associated did:key and compare to the one in the config
	// We have to represent the public key as a compressed array of bytes,
	// and then apply the encoding for did:key.

	// This is the uncompressed public key
	uncompressed, err := privateKey.PublicKey.Bytes()
	if err != nil {
		return nil, err
	}

	// Extract X and Y from the slice
	// X is bytes [1:33], Y is bytes [33:65]
	xBytes := uncompressed[1:33]
	yLastByte := uncompressed[64]

	// Determine the compressedPrefix (0x02 if Y is even, 0x03 if Y is odd)
	var compressedPrefix byte = 0x02
	if yLastByte%2 != 0 {
		compressedPrefix = 0x03
	}

	// Construct the 33-byte compressed key
	compressedBytes := append([]byte{compressedPrefix}, xBytes...)

	// Compress the public key for the DID
	varintPrefix := []byte{0x80, 0x24} // Varint for P-256
	didKey := "did:key:z" + base58.Encode(append(varintPrefix, compressedBytes...))

	if didKey != config.MyDidkey {
		return nil, fmt.Errorf("the private key does not correspond to the did:key in the configuration")
	}

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

package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"os"
	"strings"

	"github.com/mr-tron/base58/base58"
)

func main() {
	hexKey := "0x0826f120c769d4de55ad686b31c60242d87615a2bb25f53c07b580d9e7a074af"
	// Strip any '0x' or '0X' prefix from the key and decode it
	hexKey = strings.TrimPrefix(hexKey, "0x")
	hexKey = strings.TrimPrefix(hexKey, "0X")

	// Get the private key raw key bytes
	dBytes, _ := hex.DecodeString(hexKey)

	curve := elliptic.P256()

	// Import the key with the new mechanism
	privECDSA, err := ecdsa.ParseRawPrivateKey(curve, dBytes)
	if err != nil {
		panic(err)
	}

	// PEM (PKCS#8)
	derBytes, _ := x509.MarshalPKCS8PrivateKey(privECDSA)
	fmt.Println("--- PEM PRIVATE KEY ---")
	pem.Encode(os.Stdout, &pem.Block{Type: "PRIVATE KEY", Bytes: derBytes})

	// did:key derivation

	// Derive public coordinates
	privECDSA.PublicKey.X, privECDSA.PublicKey.Y = curve.ScalarBaseMult(dBytes)

	// Compress the public key for the DID
	pubBytes := elliptic.MarshalCompressed(curve, privECDSA.PublicKey.X, privECDSA.PublicKey.Y)
	prefix := []byte{0x80, 0x24} // Varint for P-256
	didKey := "did:key:z" + base58.Encode(append(prefix, pubBytes...))

	fmt.Println("\n--- DERIVED DID:KEY ---")
	fmt.Println(didKey)

	// Use privECDSA here to sign your JWT with a library like golang-jwt/jwt
	fmt.Println("\nSuccess: Use 'privECDSA' variable for JWT signing.")
}

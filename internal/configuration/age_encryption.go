package configuration

import (
	"fmt"
	"os"

	"filippo.io/age"
)

func GenerateNewIdentity() error {
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		return err
	}
	fmt.Printf("Public key: %s\n", identity.Recipient().String())
	fmt.Printf("Private key (KEEP SECRET): %s\n", identity.String())
	return nil
}

func EncryptConfigFile(inputPath, outputPath, publicKey string) error {
	recipient, err := age.ParseX25519Recipient(publicKey)
	if err != nil {
		return err
	}

	out, _ := os.Create(outputPath)
	defer out.Close()

	w, err := age.Encrypt(out, recipient)
	if err != nil {
		return err
	}

	content, _ := os.ReadFile(inputPath)
	w.Write(content)
	return w.Close()
}

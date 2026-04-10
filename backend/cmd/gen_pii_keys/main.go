//go:build ignore

// gen_pii_keys generates a pair of cryptographically random keys for the
// PII encryption service and prints them as environment variable exports.
//
// Usage:
//   go run ./cmd/gen_pii_keys/main.go
//
// Copy the output into your Cloud Run environment variables or Secret Manager.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
)

func main() {
	aesKey := make([]byte, 32)
	if _, err := rand.Read(aesKey); err != nil {
		log.Fatal(err)
	}
	hmacKey := make([]byte, 32)
	if _, err := rand.Read(hmacKey); err != nil {
		log.Fatal(err)
	}
	fmt.Println("# Add these to Cloud Run environment variables:")
	fmt.Println()
	fmt.Printf("PII_AES_KEY=%s\n", hex.EncodeToString(aesKey))
	fmt.Printf("PII_HMAC_KEY=%s\n", hex.EncodeToString(hmacKey))
	fmt.Println()
	fmt.Println("# IMPORTANT: Store these keys securely. If lost, encrypted PII cannot be recovered.")
	fmt.Println("# Both keys must remain constant — changing them will make existing orders undecryptable.")
}

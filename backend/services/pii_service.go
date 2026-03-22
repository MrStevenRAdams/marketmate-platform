package services

// ============================================================================
// PII ENCRYPTION SERVICE
//
// Provides field-level AES-256-GCM encryption and HMAC-SHA256 search tokens
// for PII fields in orders (name, email, phone, address).
//
// KEY MANAGEMENT
// ──────────────
// Keys are loaded from environment variables at startup:
//   PII_AES_KEY   — 32-byte hex-encoded AES-256 key  (64 hex chars)
//   PII_HMAC_KEY  — 32-byte hex-encoded HMAC key     (64 hex chars)
//
// In production these are injected via Cloud Run secrets / Secret Manager.
// If the keys are absent, the service runs in PASSTHROUGH mode — PII is stored
// plaintext with a warning logged. This keeps the app functional in dev without
// secrets configured.
//
// FIRESTORE LAYOUT
// ────────────────
// Each order document gains the following fields:
//
//   customer_enc        string   AES-GCM ciphertext (base64) of Customer JSON
//   shipping_enc        string   AES-GCM ciphertext (base64) of ShippingAddress JSON
//   billing_enc         string   AES-GCM ciphertext (base64) of BillingAddress JSON (if present)
//
//   pii_email_token     string   HMAC of normalised email
//   pii_name_token      string   HMAC of normalised buyer name
//   pii_postcode_token  string   HMAC of normalised postcode
//   pii_phone_token     string   HMAC of normalised phone
//
//   pii_encrypted       bool     true once fields have been encrypted
//
// The plaintext Customer and ShippingAddress structs are set to zero values
// before Firestore write, so they never appear in stored documents.
//
// SEARCH
// ──────
// To search by email: compute HMAC(normalise(email)) and query pii_email_token.
// Exact match only — partial/wildcard search is not supported.
//
// DECRYPTION
// ──────────
// Call PIIService.DecryptOrder(order, encryptedFields) to re-populate the
// plaintext Customer and ShippingAddress fields for display / dispatch.
// ============================================================================

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"module-a/models"
)

// PIIService handles encryption, decryption and search token generation.
type PIIService struct {
	aesKey  []byte // 32 bytes for AES-256
	hmacKey []byte // 32 bytes for HMAC-SHA256
	enabled bool   // false = passthrough mode (dev/no keys)
}

// EncryptedOrderFields holds the ciphertext fields that are stored in Firestore
// alongside (or in place of) the plaintext PII fields.
type EncryptedOrderFields struct {
	CustomerEnc      string `json:"customer_enc" firestore:"customer_enc"`
	ShippingEnc      string `json:"shipping_enc" firestore:"shipping_enc"`
	BillingEnc       string `json:"billing_enc,omitempty" firestore:"billing_enc,omitempty"`
	EmailToken       string `json:"pii_email_token" firestore:"pii_email_token"`
	NameToken        string `json:"pii_name_token" firestore:"pii_name_token"`
	PostcodeToken    string `json:"pii_postcode_token" firestore:"pii_postcode_token"`
	PhoneToken       string `json:"pii_phone_token" firestore:"pii_phone_token"`
	PIIEncrypted     bool   `json:"pii_encrypted" firestore:"pii_encrypted"`
}

// NewPIIService initialises the service from environment variables.
// Call once at application startup.
func NewPIIService() *PIIService {
	aesHex := os.Getenv("PII_AES_KEY")
	hmacHex := os.Getenv("PII_HMAC_KEY")

	if aesHex == "" || hmacHex == "" {
		log.Println("⚠️  PIIService: PII_AES_KEY / PII_HMAC_KEY not set — running in PASSTHROUGH mode (PII stored plaintext)")
		return &PIIService{enabled: false}
	}

	aesKey, err := hex.DecodeString(aesHex)
	if err != nil || len(aesKey) != 32 {
		log.Printf("⚠️  PIIService: invalid PII_AES_KEY (must be 64 hex chars) — passthrough mode: %v", err)
		return &PIIService{enabled: false}
	}

	hmacKey, err := hex.DecodeString(hmacHex)
	if err != nil || len(hmacKey) != 32 {
		log.Printf("⚠️  PIIService: invalid PII_HMAC_KEY (must be 64 hex chars) — passthrough mode: %v", err)
		return &PIIService{enabled: false}
	}

	log.Println("✅ PIIService: AES-256-GCM encryption enabled")
	return &PIIService{aesKey: aesKey, hmacKey: hmacKey, enabled: true}
}

// Enabled returns true if encryption is active.
func (p *PIIService) Enabled() bool { return p.enabled }

// ─── EncryptOrder ─────────────────────────────────────────────────────────────
//
// Takes an order (with plaintext PII in Customer / ShippingAddress / BillingAddress),
// returns:
//   - EncryptedOrderFields to be merged into the Firestore document
//   - A sanitised copy of the order with PII fields zeroed out
//
// In passthrough mode the order is returned unchanged and EncryptedOrderFields
// has PIIEncrypted=false.

func (p *PIIService) EncryptOrder(order models.Order) (models.Order, EncryptedOrderFields, error) {
	ef := EncryptedOrderFields{}

	if !p.enabled {
		ef.PIIEncrypted = false
		// Still generate tokens so search works even in passthrough mode
		// (tokens are non-reversible so safe to store even without encryption)
		ef.EmailToken    = p.token(order.Customer.Email)
		ef.NameToken     = p.token(order.Customer.Name)
		ef.PostcodeToken = p.token(order.ShippingAddress.PostalCode)
		ef.PhoneToken    = p.token(order.Customer.Phone)
		return order, ef, nil
	}

	// Encrypt Customer
	customerJSON, _ := json.Marshal(order.Customer)
	customerEnc, err := p.encrypt(customerJSON)
	if err != nil {
		return order, ef, fmt.Errorf("encrypt customer: %w", err)
	}
	ef.CustomerEnc = customerEnc

	// Encrypt ShippingAddress
	shippingJSON, _ := json.Marshal(order.ShippingAddress)
	shippingEnc, err := p.encrypt(shippingJSON)
	if err != nil {
		return order, ef, fmt.Errorf("encrypt shipping: %w", err)
	}
	ef.ShippingEnc = shippingEnc

	// Encrypt BillingAddress (if present)
	if order.BillingAddress != nil {
		billingJSON, _ := json.Marshal(order.BillingAddress)
		billingEnc, err := p.encrypt(billingJSON)
		if err != nil {
			return order, ef, fmt.Errorf("encrypt billing: %w", err)
		}
		ef.BillingEnc = billingEnc
	}

	// Search tokens
	ef.EmailToken    = p.token(order.Customer.Email)
	ef.NameToken     = p.token(order.Customer.Name)
	ef.PostcodeToken = p.token(order.ShippingAddress.PostalCode)
	ef.PhoneToken    = p.token(order.Customer.Phone)
	ef.PIIEncrypted  = true

	// Zero out plaintext PII from the order before storage
	sanitised := order
	sanitised.Customer = models.Customer{}
	sanitised.ShippingAddress = models.Address{}
	sanitised.BillingAddress = nil

	return sanitised, ef, nil
}

// ─── DecryptOrder ─────────────────────────────────────────────────────────────
//
// Restores plaintext PII into an order from its EncryptedOrderFields.
// Safe to call even if encryption is disabled — returns the order unchanged.

func (p *PIIService) DecryptOrder(order models.Order, ef EncryptedOrderFields) (models.Order, error) {
	if !ef.PIIEncrypted || !p.enabled {
		return order, nil
	}

	// Decrypt Customer
	if ef.CustomerEnc != "" {
		plaintext, err := p.decrypt(ef.CustomerEnc)
		if err != nil {
			return order, fmt.Errorf("decrypt customer: %w", err)
		}
		var customer models.Customer
		if err := json.Unmarshal(plaintext, &customer); err != nil {
			return order, fmt.Errorf("unmarshal customer: %w", err)
		}
		order.Customer = customer
	}

	// Decrypt ShippingAddress
	if ef.ShippingEnc != "" {
		plaintext, err := p.decrypt(ef.ShippingEnc)
		if err != nil {
			return order, fmt.Errorf("decrypt shipping: %w", err)
		}
		var addr models.Address
		if err := json.Unmarshal(plaintext, &addr); err != nil {
			return order, fmt.Errorf("unmarshal shipping: %w", err)
		}
		order.ShippingAddress = addr
	}

	// Decrypt BillingAddress
	if ef.BillingEnc != "" {
		plaintext, err := p.decrypt(ef.BillingEnc)
		if err != nil {
			return order, fmt.Errorf("decrypt billing: %w", err)
		}
		var addr models.Address
		if err := json.Unmarshal(plaintext, &addr); err != nil {
			return order, fmt.Errorf("unmarshal billing: %w", err)
		}
		order.BillingAddress = &addr
	}

	return order, nil
}

// ─── SearchToken ──────────────────────────────────────────────────────────────
//
// Returns the HMAC token for a given search value.
// Use this in query handlers to convert a search term into a token for Firestore query.

func (p *PIIService) SearchToken(value string) string {
	return p.token(value)
}

// ─── Private helpers ──────────────────────────────────────────────────────────

// token computes HMAC-SHA256 of the normalised value, hex-encoded.
func (p *PIIService) token(value string) string {
	if value == "" {
		return ""
	}
	normalised := strings.ToLower(strings.TrimSpace(value))
	// Use HMAC with the hmacKey if available, otherwise use a fixed zero key
	// (tokens in passthrough mode are still non-reversible one-way hashes)
	key := p.hmacKey
	if key == nil {
		key = make([]byte, 32) // zero key for passthrough — tokens still work for exact search
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(normalised))
	return hex.EncodeToString(mac.Sum(nil))
}

// encrypt performs AES-256-GCM encryption and returns base64(nonce + ciphertext).
func (p *PIIService) encrypt(plaintext []byte) (string, error) {
	block, err := aes.NewCipher(p.aesKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// decrypt reverses encrypt.
func (p *PIIService) decrypt(encoded string) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("base64 decode: %w", err)
	}
	block, err := aes.NewCipher(p.aesKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}

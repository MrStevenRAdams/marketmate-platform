package handlers

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"

	"module-a/models"
	"module-a/services"
)

// ============================================================================
// SUPPLIER HANDLER
// ============================================================================
// Dedicated handler for /api/v1/suppliers.
// Sensitive fields (FTP password, webhook secret, bank account number) are
// encrypted with AES-256-GCM using CREDENTIAL_ENCRYPTION_KEY before storage.
// ============================================================================

type SupplierHandler struct {
	client        *firestore.Client
	encryptionKey []byte
}

func NewSupplierHandler(client *firestore.Client) *SupplierHandler {
	key := os.Getenv("CREDENTIAL_ENCRYPTION_KEY")
	if key == "" {
		key = "default-32-char-key-change-me!!"
	}
	if len(key) > 32 {
		key = key[:32]
	}
	return &SupplierHandler{
		client:        client,
		encryptionKey: []byte(key),
	}
}

func (h *SupplierHandler) tenantID(c *gin.Context) string {
	if tid := c.GetString("tenant_id"); tid != "" {
		return tid
	}
	return c.GetHeader("X-Tenant-Id")
}

func (h *SupplierHandler) col(tenantID string) *firestore.CollectionRef {
	return h.client.Collection("tenants").Doc(tenantID).Collection("suppliers")
}

// ── AES-256-GCM helpers ──────────────────────────────────────────────────────

func (h *SupplierHandler) encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	block, err := aes.NewCipher(h.encryptionKey)
	if err != nil {
		return "", err
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := aesGCM.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ct), nil
}

func (h *SupplierHandler) decrypt(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(h.encryptionKey)
	if err != nil {
		return "", err
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonceSize := aesGCM.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}
	pt, err := aesGCM.Open(nil, data[:nonceSize], data[nonceSize:], nil)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

// ── Encryption helpers on structs ─────────────────────────────────────────────

// encryptSensitiveFields encrypts all sensitive sub-struct fields before save.
// The incoming request body carries plaintext in *_plain sentinel fields that
// the frontend sends; they are encrypted here and stored as *_enc fields.
func (h *SupplierHandler) encryptSensitiveFields(sup *models.Supplier) error {
	if sup.FTPConfig != nil && sup.FTPConfig.PasswordEnc != "" {
		// If the value doesn't look like base64 ciphertext, treat it as plaintext
		if !isEncrypted(sup.FTPConfig.PasswordEnc) {
			enc, err := h.encrypt(sup.FTPConfig.PasswordEnc)
			if err != nil {
				return fmt.Errorf("encrypting FTP password: %w", err)
			}
			sup.FTPConfig.PasswordEnc = enc
		}
	}
	if sup.WebhookConfig != nil && sup.WebhookConfig.SecretEnc != "" {
		if !isEncrypted(sup.WebhookConfig.SecretEnc) {
			enc, err := h.encrypt(sup.WebhookConfig.SecretEnc)
			if err != nil {
				return fmt.Errorf("encrypting webhook secret: %w", err)
			}
			sup.WebhookConfig.SecretEnc = enc
		}
	}
	if sup.BankDetails != nil && sup.BankDetails.AccountNumberEnc != "" {
		if !isEncrypted(sup.BankDetails.AccountNumberEnc) {
			enc, err := h.encrypt(sup.BankDetails.AccountNumberEnc)
			if err != nil {
				return fmt.Errorf("encrypting bank account number: %w", err)
			}
			sup.BankDetails.AccountNumberEnc = enc
		}
	}
	return nil
}

// redactSensitiveFields replaces encrypted blobs with a placeholder so
// we never return raw ciphertext to the frontend.
func redactSensitiveFields(sup *models.Supplier) {
	if sup.FTPConfig != nil && sup.FTPConfig.PasswordEnc != "" {
		sup.FTPConfig.PasswordEnc = "••••••••"
	}
	if sup.WebhookConfig != nil && sup.WebhookConfig.SecretEnc != "" {
		sup.WebhookConfig.SecretEnc = "••••••••"
	}
	if sup.BankDetails != nil && sup.BankDetails.AccountNumberEnc != "" {
		sup.BankDetails.AccountNumberEnc = "••••••••"
	}
}

// isEncrypted is a heuristic: if the value is valid base64 and longer than 32
// chars it's already encrypted. This prevents double-encryption on update.
func isEncrypted(s string) bool {
	if len(s) < 32 {
		return false
	}
	_, err := base64.StdEncoding.DecodeString(s)
	return err == nil
}

// ── CRUD ─────────────────────────────────────────────────────────────────────

// ListSuppliers GET /api/v1/suppliers
func (h *SupplierHandler) ListSuppliers(c *gin.Context) {
	tenantID := h.tenantID(c)
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}

	q := h.col(tenantID).Query
	if c.Query("active") == "true" {
		q = q.Where("active", "==", true)
	}

	iter := q.Documents(c.Request.Context())
	defer iter.Stop()

	var suppliers []models.Supplier
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch suppliers"})
			return
		}
		var sup models.Supplier
		if err := doc.DataTo(&sup); err != nil {
			log.Printf("[Supplier] failed to parse %s: %v", doc.Ref.ID, err)
			continue
		}
		redactSensitiveFields(&sup)
		suppliers = append(suppliers, sup)
	}
	if suppliers == nil {
		suppliers = []models.Supplier{}
	}
	c.JSON(http.StatusOK, gin.H{"suppliers": suppliers, "count": len(suppliers)})
}

// GetSupplier GET /api/v1/suppliers/:id
func (h *SupplierHandler) GetSupplier(c *gin.Context) {
	tenantID := h.tenantID(c)
	supplierID := c.Param("id")

	doc, err := h.col(tenantID).Doc(supplierID).Get(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "supplier not found"})
		return
	}
	var sup models.Supplier
	if err := doc.DataTo(&sup); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse supplier"})
		return
	}
	redactSensitiveFields(&sup)
	c.JSON(http.StatusOK, sup)
}

// CreateSupplier POST /api/v1/suppliers
func (h *SupplierHandler) CreateSupplier(c *gin.Context) {
	tenantID := h.tenantID(c)
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}

	var sup models.Supplier
	if err := c.ShouldBindJSON(&sup); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid supplier: " + err.Error()})
		return
	}
	if sup.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "supplier name is required"})
		return
	}

	if err := h.encryptSensitiveFields(&sup); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	sup.SupplierID = "sup_" + uuid.New().String()
	sup.TenantID = tenantID
	sup.Active = true
	sup.CreatedAt = time.Now()
	sup.UpdatedAt = time.Now()
	if sup.Currency == "" {
		sup.Currency = "GBP"
	}

	_, err := h.col(tenantID).Doc(sup.SupplierID).Set(c.Request.Context(), sup)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save supplier"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"supplier_id": sup.SupplierID,
		"name":        sup.Name,
		"message":     "Supplier created successfully",
	})
}

// UpdateSupplier PUT /api/v1/suppliers/:id
func (h *SupplierHandler) UpdateSupplier(c *gin.Context) {
	tenantID := h.tenantID(c)
	supplierID := c.Param("id")

	ref := h.col(tenantID).Doc(supplierID)
	if _, err := ref.Get(c.Request.Context()); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "supplier not found"})
		return
	}

	var sup models.Supplier
	if err := c.ShouldBindJSON(&sup); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid update body"})
		return
	}

	// Preserve existing encrypted values when the frontend sends the placeholder
	if sup.FTPConfig != nil && sup.FTPConfig.PasswordEnc == "••••••••" {
		existing, _ := h.col(tenantID).Doc(supplierID).Get(c.Request.Context())
		var existingSup models.Supplier
		if existing != nil {
			existing.DataTo(&existingSup)
		}
		if existingSup.FTPConfig != nil {
			sup.FTPConfig.PasswordEnc = existingSup.FTPConfig.PasswordEnc
		} else {
			sup.FTPConfig.PasswordEnc = ""
		}
	}
	if sup.WebhookConfig != nil && sup.WebhookConfig.SecretEnc == "••••••••" {
		existing, _ := h.col(tenantID).Doc(supplierID).Get(c.Request.Context())
		var existingSup models.Supplier
		if existing != nil {
			existing.DataTo(&existingSup)
		}
		if existingSup.WebhookConfig != nil {
			sup.WebhookConfig.SecretEnc = existingSup.WebhookConfig.SecretEnc
		} else {
			sup.WebhookConfig.SecretEnc = ""
		}
	}
	if sup.BankDetails != nil && sup.BankDetails.AccountNumberEnc == "••••••••" {
		existing, _ := h.col(tenantID).Doc(supplierID).Get(c.Request.Context())
		var existingSup models.Supplier
		if existing != nil {
			existing.DataTo(&existingSup)
		}
		if existingSup.BankDetails != nil {
			sup.BankDetails.AccountNumberEnc = existingSup.BankDetails.AccountNumberEnc
		} else {
			sup.BankDetails.AccountNumberEnc = ""
		}
	}

	if err := h.encryptSensitiveFields(&sup); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	sup.UpdatedAt = time.Now()
	sup.SupplierID = supplierID
	sup.TenantID = tenantID

	_, err := ref.Set(c.Request.Context(), sup)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update supplier"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Supplier updated", "supplier_id": supplierID})
}

// DeleteSupplier DELETE /api/v1/suppliers/:id — soft delete
func (h *SupplierHandler) DeleteSupplier(c *gin.Context) {
	tenantID := h.tenantID(c)
	supplierID := c.Param("id")

	_, err := h.col(tenantID).Doc(supplierID).Update(c.Request.Context(), []firestore.Update{
		{Path: "active", Value: false},
		{Path: "updated_at", Value: time.Now()},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to deactivate supplier"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Supplier deactivated", "supplier_id": supplierID})
}

// ── Test Connection ───────────────────────────────────────────────────────────

// TestConnection POST /api/v1/suppliers/:id/test-connection
func (h *SupplierHandler) TestConnection(c *gin.Context) {
	tenantID := h.tenantID(c)
	supplierID := c.Param("id")

	doc, err := h.col(tenantID).Doc(supplierID).Get(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "supplier not found"})
		return
	}
	var sup models.Supplier
	if err := doc.DataTo(&sup); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse supplier"})
		return
	}

	switch sup.OrderMethod {
	case "webhook":
		h.testWebhook(c, sup)
	case "ftp", "sftp":
		h.testFTP(c, sup)
	case "email":
		h.testEmail(c, sup)
	default:
		c.JSON(http.StatusOK, gin.H{
			"status":  "ok",
			"method":  sup.OrderMethod,
			"message": "Manual orders require no connection test",
		})
	}
}

func (h *SupplierHandler) testWebhook(c *gin.Context, sup models.Supplier) {
	if sup.WebhookConfig == nil || sup.WebhookConfig.URL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "webhook URL not configured"})
		return
	}
	cfg := sup.WebhookConfig
	payload, _ := json.Marshal(map[string]interface{}{
		"test":        true,
		"supplier_id": sup.SupplierID,
		"timestamp":   time.Now().UTC(),
	})

	req, err := http.NewRequest(cfg.Method, cfg.URL, bytes.NewReader(payload))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid webhook URL: " + err.Error()})
		return
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}

	// Apply auth
	if cfg.AuthType == "bearer" || cfg.AuthType == "api_key" {
		secret, _ := h.decrypt(cfg.SecretEnc)
		if cfg.AuthType == "bearer" {
			req.Header.Set("Authorization", "Bearer "+secret)
		} else if cfg.AuthHeader != "" {
			req.Header.Set(cfg.AuthHeader, secret)
		}
	} else if cfg.AuthType == "basic" {
		secret, _ := h.decrypt(cfg.SecretEnc)
		req.SetBasicAuth(cfg.AuthHeader, secret) // AuthHeader holds username for basic
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status":  "error",
			"message": "Connection failed: " + err.Error(),
		})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		c.JSON(http.StatusOK, gin.H{
			"status":      "ok",
			"http_status": resp.StatusCode,
			"message":     fmt.Sprintf("Webhook responded with %d", resp.StatusCode),
		})
	} else {
		c.JSON(http.StatusOK, gin.H{
			"status":      "error",
			"http_status": resp.StatusCode,
			"message":     fmt.Sprintf("Webhook returned %d: %s", resp.StatusCode, string(body)),
		})
	}
}

func (h *SupplierHandler) testFTP(c *gin.Context, sup models.Supplier) {
	if sup.FTPConfig == nil || sup.FTPConfig.Host == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "FTP host not configured"})
		return
	}
	cfg := sup.FTPConfig
	port := cfg.Port
	if port == 0 {
		if cfg.Protocol == "sftp" {
			port = 22
		} else {
			port = 21
		}
	}
	addr := fmt.Sprintf("%s:%d", cfg.Host, port)
	conn, err := net.DialTimeout("tcp", addr, 8*time.Second)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status":  "error",
			"message": fmt.Sprintf("Cannot reach %s: %s", addr, err.Error()),
		})
		return
	}
	conn.Close()
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"message": fmt.Sprintf("TCP connection to %s successful (authentication not tested)", addr),
	})
}

func (h *SupplierHandler) testEmail(c *gin.Context, sup models.Supplier) {
	toAddr := sup.Email
	if sup.EmailConfig != nil && len(sup.EmailConfig.ToAddresses) > 0 {
		toAddr = sup.EmailConfig.ToAddresses[0]
	}
	if toAddr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no email address configured on supplier"})
		return
	}
	subject := "MarketMate connection test"
	body := fmt.Sprintf("<p>This is an automated connection test from MarketMate for supplier <strong>%s</strong> (%s).</p><p>If you received this email, the email connection is working correctly.</p>", sup.Name, sup.Code)
	if err := services.SendRawEmail(toAddr, subject, body); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status":  "error",
			"message": "Failed to send test email: " + err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"message": fmt.Sprintf("Test email sent to %s", toAddr),
	})
}

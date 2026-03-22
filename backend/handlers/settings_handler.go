package handlers

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/smtp"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"
)

// ============================================================================
// SETTINGS HANDLER
//
// Routes:
//   GET    /api/v1/settings/email           Get SMTP config
//   PUT    /api/v1/settings/email           Save SMTP config
//   POST   /api/v1/settings/email/test      Send test email
//   GET    /api/v1/settings/notifications   Get notification prefs
//   PUT    /api/v1/settings/notifications   Save notification prefs
//   GET    /api/v1/settings/currency        List currency rates
//   POST   /api/v1/settings/currency        Add/update a currency rate
//   DELETE /api/v1/settings/currency/:id    Delete a currency rate
// ============================================================================

type SettingsHandler struct {
	client *firestore.Client
}

func NewSettingsHandler(client *firestore.Client) *SettingsHandler {
	return &SettingsHandler{client: client}
}

// ─── Firestore helpers ────────────────────────────────────────────────────────

func (h *SettingsHandler) configDoc(tenantID string) *firestore.DocumentRef {
	return h.client.Collection("tenants").Doc(tenantID).
		Collection("config").Doc("settings")
}

func (h *SettingsHandler) currencyCol(tenantID string) *firestore.CollectionRef {
	return h.client.Collection("tenants").Doc(tenantID).
		Collection("currency_rates")
}

// ============================================================================
// EMAIL SETTINGS
// ============================================================================

// GetEmailSettings  GET /api/v1/settings/email
func (h *SettingsHandler) GetEmailSettings(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	doc, err := h.configDoc(tenantID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"smtp_config": map[string]interface{}{}})
		return
	}
	data := doc.Data()
	smtpConfig, _ := data["smtp_config"].(map[string]interface{})
	if smtpConfig == nil {
		smtpConfig = map[string]interface{}{}
	}
	// Mask stored password — never return plaintext
	if _, has := smtpConfig["password"]; has {
		smtpConfig["password"] = "••••••••"
	}
	c.JSON(http.StatusOK, gin.H{"smtp_config": smtpConfig})
}

// UpdateEmailSettings  PUT /api/v1/settings/email
func (h *SettingsHandler) UpdateEmailSettings(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	var req struct {
		SMTPConfig map[string]interface{} `json:"smtp_config"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.SMTPConfig == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "smtp_config is required"})
		return
	}

	// Don't overwrite password when client echoes back the mask
	existing, err := h.configDoc(tenantID).Get(ctx)
	if err == nil {
		existingData := existing.Data()
		if existingSmtp, ok := existingData["smtp_config"].(map[string]interface{}); ok {
			if pw, _ := req.SMTPConfig["password"].(string); pw == "••••••••" {
				req.SMTPConfig["password"] = existingSmtp["password"]
			}
		}
	}

	if _, err := h.configDoc(tenantID).Set(ctx, map[string]interface{}{
		"smtp_config": req.SMTPConfig,
		"updated_at":  time.Now(),
	}, firestore.MergeAll); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// TestEmailSettings  POST /api/v1/settings/email/test
func (h *SettingsHandler) TestEmailSettings(c *gin.Context) {
	var req struct {
		SMTPConfig map[string]interface{} `json:"smtp_config"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	host, _ := req.SMTPConfig["host"].(string)
	if host == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "SMTP host is required"})
		return
	}

	port, _ := req.SMTPConfig["port"].(string)
	if port == "" {
		port = "587"
	}
	username, _ := req.SMTPConfig["username"].(string)
	password, _ := req.SMTPConfig["password"].(string)
	fromAddr, _ := req.SMTPConfig["from_address"].(string)
	fromName, _ := req.SMTPConfig["from_name"].(string)
	tlsEnabled, _ := req.SMTPConfig["tls"].(bool)

	if fromAddr == "" {
		fromAddr = username
	}
	if fromName == "" {
		fromName = "MarketMate"
	}
	toAddr := username
	if toAddr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Username is required (used as recipient for test)"})
		return
	}

	addr := fmt.Sprintf("%s:%s", host, port)
	subject := "MarketMate — SMTP Test"
	body := "Your MarketMate email configuration is working correctly.\r\n\r\nThis is an automated test message."
	msg := fmt.Sprintf("From: %s <%s>\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s\r\n",
		fromName, fromAddr, toAddr, subject, body)

	if err := dialAndSend(addr, host, username, password, fromAddr, toAddr, []byte(msg), tlsEnabled); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("SMTP error: %v", err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "sent_to": toAddr})
}

// dialAndSend dials an SMTP server and sends a message.
// Supports both STARTTLS (port 587) and implicit TLS (port 465).
func dialAndSend(addr, host, username, password, from, to string, msg []byte, useTLS bool) error {
	auth := smtp.PlainAuth("", username, password, host)

	// Port 465: implicit TLS
	_, port, _ := net.SplitHostPort(addr)
	if port == "465" {
		tlsConfig := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS13}
		conn, err := tls.Dial("tcp", addr, tlsConfig)
		if err != nil {
			return fmt.Errorf("TLS dial: %w", err)
		}
		client, err := smtp.NewClient(conn, host)
		if err != nil {
			return fmt.Errorf("SMTP client: %w", err)
		}
		defer client.Close()
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("SMTP auth: %w", err)
		}
		if err := client.Mail(from); err != nil {
			return err
		}
		if err := client.Rcpt(to); err != nil {
			return err
		}
		w, err := client.Data()
		if err != nil {
			return err
		}
		if _, err := w.Write(msg); err != nil {
			return err
		}
		return w.Close()
	}

	// Default: STARTTLS or plain
	if useTLS {
		return smtp.SendMail(addr, auth, from, []string{to}, msg)
	}
	// Plain (no TLS) — rarely needed but supported
	client, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("SMTP dial: %w", err)
	}
	defer client.Close()
	if username != "" {
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("SMTP auth: %w", err)
		}
	}
	if err := client.Mail(from); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	return w.Close()
}

// ============================================================================
// NOTIFICATION PREFERENCES
// ============================================================================

// GetNotificationSettings  GET /api/v1/settings/notifications
func (h *SettingsHandler) GetNotificationSettings(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	doc, err := h.configDoc(tenantID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"notifications": map[string]interface{}{}})
		return
	}
	data := doc.Data()
	notifs, _ := data["notifications"].(map[string]interface{})
	if notifs == nil {
		notifs = map[string]interface{}{}
	}
	c.JSON(http.StatusOK, gin.H{"notifications": notifs})
}

// UpdateNotificationSettings  PUT /api/v1/settings/notifications
func (h *SettingsHandler) UpdateNotificationSettings(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	var req struct {
		Notifications map[string]interface{} `json:"notifications"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if _, err := h.configDoc(tenantID).Set(ctx, map[string]interface{}{
		"notifications": req.Notifications,
		"updated_at":    time.Now(),
	}, firestore.MergeAll); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ============================================================================
// CURRENCY RATES
// ============================================================================

type CurrencyRate struct {
	ID        string    `json:"id" firestore:"id"`
	From      string    `json:"from" firestore:"from"`
	To        string    `json:"to" firestore:"to"`
	Rate      float64   `json:"rate" firestore:"rate"`
	Mode      string    `json:"mode" firestore:"mode"` // "manual" | "auto"
	UpdatedAt time.Time `json:"updated_at" firestore:"updated_at"`
}

// GetCurrencyRates  GET /api/v1/settings/currency
func (h *SettingsHandler) GetCurrencyRates(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	iter := h.currencyCol(tenantID).OrderBy("from", firestore.Asc).Documents(ctx)
	var rates []CurrencyRate
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		var r CurrencyRate
		if err := doc.DataTo(&r); err == nil {
			rates = append(rates, r)
		}
	}
	if rates == nil {
		rates = []CurrencyRate{}
	}
	c.JSON(http.StatusOK, gin.H{"rates": rates})
}

// AddCurrencyRate  POST /api/v1/settings/currency
func (h *SettingsHandler) AddCurrencyRate(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	var req struct {
		From string  `json:"from" binding:"required"`
		To   string  `json:"to" binding:"required"`
		Rate float64 `json:"rate" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Deterministic ID allows upsert of existing pair
	id := fmt.Sprintf("%s_%s", req.From, req.To)
	rate := CurrencyRate{
		ID:        id,
		From:      req.From,
		To:        req.To,
		Rate:      req.Rate,
		Mode:      "manual",
		UpdatedAt: time.Now(),
	}
	if _, err := h.currencyCol(tenantID).Doc(id).Set(ctx, rate); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"rate": rate})
}

// DeleteCurrencyRate  DELETE /api/v1/settings/currency/:id
func (h *SettingsHandler) DeleteCurrencyRate(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	id := c.Param("id")
	ctx := c.Request.Context()

	if _, err := h.currencyCol(tenantID).Doc(id).Delete(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}


// ============================================================================
// TASK 14: TAX / FINANCIAL SETTINGS — VAT Registration Number
// Routes:
//   GET  /api/v1/settings/tax     — Get tax/financial settings
//   PUT  /api/v1/settings/tax     — Save tax/financial settings
// ============================================================================

type TaxSettings struct {
	VATNumber       string `json:"vat_number" firestore:"vat_number"`
	TaxRegion       string `json:"tax_region" firestore:"tax_region"`     // e.g. "GB", "EU"
	DefaultTaxRate  float64 `json:"default_tax_rate" firestore:"default_tax_rate"` // e.g. 0.20
	TaxIncluded     bool   `json:"tax_included" firestore:"tax_included"` // prices include tax
	UpdatedAt       string `json:"updated_at,omitempty" firestore:"updated_at,omitempty"`
}

// GetTaxSettings  GET /api/v1/settings/tax
func (h *SettingsHandler) GetTaxSettings(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	doc, err := h.configDoc(tenantID).Get(ctx)
	if err != nil {
		c.JSON(200, gin.H{"tax": TaxSettings{}})
		return
	}
	data := doc.Data()
	taxData, _ := data["tax"].(map[string]interface{})
	if taxData == nil {
		c.JSON(200, gin.H{"tax": TaxSettings{}})
		return
	}
	var ts TaxSettings
	// Manual map since we stored as interface{}
	ts.VATNumber, _ = taxData["vat_number"].(string)
	ts.TaxRegion, _ = taxData["tax_region"].(string)
	if rate, ok := taxData["default_tax_rate"].(float64); ok {
		ts.DefaultTaxRate = rate
	}
	ts.TaxIncluded, _ = taxData["tax_included"].(bool)
	c.JSON(200, gin.H{"tax": ts})
}

// UpdateTaxSettings  PUT /api/v1/settings/tax
func (h *SettingsHandler) UpdateTaxSettings(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	var req TaxSettings
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	req.UpdatedAt = time.Now().Format(time.RFC3339)

	if _, err := h.configDoc(tenantID).Set(ctx, map[string]interface{}{
		"tax":        req,
		"updated_at": time.Now(),
	}, firestore.MergeAll); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// ============================================================================
// ORDER SETTINGS (Session 3)
// ============================================================================

type StatusMapping struct {
	InternalStatus string `json:"internal_status" firestore:"internal_status"`
	DisplayName    string `json:"display_name"    firestore:"display_name"`
}

type OrderSettings struct {
	// Merge & Split
	AutoMergeEnabled      bool   `json:"auto_merge_enabled" firestore:"auto_merge_enabled"`
	MergeSameAddress      bool   `json:"merge_same_address" firestore:"merge_same_address"`
	SplitThreshold        int    `json:"split_threshold" firestore:"split_threshold"`
	BlockMergeFlagDefault bool   `json:"block_merge_flag_default" firestore:"block_merge_flag_default"`

	// Pre-Processing
	CheckWeight    bool `json:"check_weight" firestore:"check_weight"`
	CheckItems     bool `json:"check_items" firestore:"check_items"`
	CheckPackaging bool `json:"check_packaging" firestore:"check_packaging"`

	// Display
	DateFormatDisplay    string `json:"date_format_display" firestore:"date_format_display"`
	DefaultPaymentMethod string `json:"default_payment_method" firestore:"default_payment_method"`

	// Status Mappings
	StatusMappings []StatusMapping `json:"status_mappings" firestore:"status_mappings"`

	// Despatch
	DespatchButtonAction string `json:"despatch_button_action" firestore:"despatch_button_action"` // complete_and_process|complete_only|process_only

	UpdatedAt string `json:"updated_at,omitempty" firestore:"updated_at,omitempty"`
}

func (h *SettingsHandler) orderSettingsDoc(tenantID string) *firestore.DocumentRef {
	return h.client.Collection("tenants").Doc(tenantID).Collection("settings").Doc("order_settings")
}

func (h *SettingsHandler) GetOrderSettings(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	doc, err := h.orderSettingsDoc(tenantID).Get(ctx)
	if err != nil {
		c.JSON(200, gin.H{"settings": OrderSettings{
			DespatchButtonAction: "complete_and_process",
			DateFormatDisplay:    "DD/MM/YYYY",
			StatusMappings:       []StatusMapping{},
		}})
		return
	}
	var s OrderSettings
	doc.DataTo(&s)
	if s.StatusMappings == nil {
		s.StatusMappings = []StatusMapping{}
	}
	c.JSON(200, gin.H{"settings": s})
}

func (h *SettingsHandler) UpdateOrderSettings(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	var req OrderSettings
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	req.UpdatedAt = time.Now().Format(time.RFC3339)
	if req.StatusMappings == nil {
		req.StatusMappings = []StatusMapping{}
	}

	if _, err := h.orderSettingsDoc(tenantID).Set(ctx, req); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// ============================================================================
// PRINT SETTINGS (Session 3)
// ============================================================================

type PrintSettings struct {
	// Invoice
	InvoiceAutoPrint      bool `json:"invoice_auto_print" firestore:"invoice_auto_print"`
	InvoicePrintOnDespatch bool `json:"invoice_print_on_despatch" firestore:"invoice_print_on_despatch"`

	// Stock Labels
	StockLabelFormat    string `json:"stock_label_format" firestore:"stock_label_format"`   // A4|A5|label_4x6
	StockLabelAutoPrint bool   `json:"stock_label_auto_print" firestore:"stock_label_auto_print"`

	// Shipping Labels
	ShippingLabelSort string `json:"shipping_label_sort" firestore:"shipping_label_sort"` // order_date|order_number|channel|destination_country

	UpdatedAt string `json:"updated_at,omitempty" firestore:"updated_at,omitempty"`
}

func (h *SettingsHandler) printSettingsDoc(tenantID string) *firestore.DocumentRef {
	return h.client.Collection("tenants").Doc(tenantID).Collection("settings").Doc("print_settings")
}

func (h *SettingsHandler) GetPrintSettings(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	doc, err := h.printSettingsDoc(tenantID).Get(ctx)
	if err != nil {
		c.JSON(200, gin.H{"settings": PrintSettings{
			StockLabelFormat:  "A4",
			ShippingLabelSort: "order_date",
		}})
		return
	}
	var s PrintSettings
	doc.DataTo(&s)
	c.JSON(200, gin.H{"settings": s})
}

func (h *SettingsHandler) UpdatePrintSettings(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	var req PrintSettings
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	req.UpdatedAt = time.Now().Format(time.RFC3339)

	if _, err := h.printSettingsDoc(tenantID).Set(ctx, req); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// ============================================================================
// WMS SETTINGS (Session 4)
// ============================================================================

type WMSSettings struct {
	AutoAllocationEnabled     bool     `json:"auto_allocation_enabled" firestore:"auto_allocation_enabled"`
	AssignableTypes           []string `json:"assignable_types" firestore:"assignable_types"` // order, purchase_order
	FIFOEnabled               bool     `json:"fifo_enabled" firestore:"fifo_enabled"`
	BinrackSuggestionsEnabled bool     `json:"binrack_suggestions_enabled" firestore:"binrack_suggestions_enabled"`
	BinrackSuggestionUseBatch bool     `json:"binrack_suggestion_use_batch" firestore:"binrack_suggestion_use_batch"`
	UpdatedAt                 string   `json:"updated_at,omitempty" firestore:"updated_at,omitempty"`
}

func (h *SettingsHandler) wmsSettingsDoc(tenantID string) *firestore.DocumentRef {
	return h.client.Collection("tenants").Doc(tenantID).Collection("settings").Doc("wms_settings")
}

func (h *SettingsHandler) GetWMSSettings(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	doc, err := h.wmsSettingsDoc(tenantID).Get(ctx)
	if err != nil {
		c.JSON(200, gin.H{"settings": WMSSettings{AssignableTypes: []string{"order"}}})
		return
	}
	var s WMSSettings
	doc.DataTo(&s)
	if s.AssignableTypes == nil {
		s.AssignableTypes = []string{}
	}
	c.JSON(200, gin.H{"settings": s})
}

func (h *SettingsHandler) UpdateWMSSettings(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	var req WMSSettings
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	req.UpdatedAt = time.Now().Format(time.RFC3339)
	if req.AssignableTypes == nil {
		req.AssignableTypes = []string{}
	}

	if _, err := h.wmsSettingsDoc(tenantID).Set(ctx, req); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// ============================================================================
// COUNTRIES & TAX RATES (Session 2)
// ============================================================================

type TaxRegion struct {
	ID            string  `json:"id" firestore:"id"`
	Name          string  `json:"name" firestore:"name"`
	TaxRate       float64 `json:"tax_rate" firestore:"tax_rate"`
	EffectiveFrom string  `json:"effective_from" firestore:"effective_from"`
}

type TaxRateHistory struct {
	Rate          float64 `json:"rate" firestore:"rate"`
	EffectiveFrom string  `json:"effective_from" firestore:"effective_from"`
	ChangedBy     string  `json:"changed_by" firestore:"changed_by"`
}

type Country struct {
	ID             string           `json:"id" firestore:"id"`
	TenantID       string           `json:"tenant_id" firestore:"tenant_id"`
	Name           string           `json:"name" firestore:"name"`
	ISOCode        string           `json:"iso_code" firestore:"iso_code"`
	DefaultTaxRate float64          `json:"default_tax_rate" firestore:"default_tax_rate"`
	Regions        []TaxRegion      `json:"regions" firestore:"regions"`
	TaxRateHistory []TaxRateHistory `json:"tax_rate_history" firestore:"tax_rate_history"`
	CreatedAt      string           `json:"created_at" firestore:"created_at"`
	UpdatedAt      string           `json:"updated_at" firestore:"updated_at"`
}

func (h *SettingsHandler) countriesCol(tenantID string) *firestore.CollectionRef {
	return h.client.Collection("tenants").Doc(tenantID).Collection("countries")
}

// GET /api/v1/settings/countries
func (h *SettingsHandler) ListCountries(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	var list []Country
	iter := h.countriesCol(tenantID).OrderBy("name", firestore.Asc).Documents(ctx)
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done { break }
		if err != nil {
			c.JSON(500, gin.H{"error": "failed to list countries"})
			return
		}
		var co Country
		doc.DataTo(&co)
		if co.Regions == nil { co.Regions = []TaxRegion{} }
		if co.TaxRateHistory == nil { co.TaxRateHistory = []TaxRateHistory{} }
		list = append(list, co)
	}
	if list == nil { list = []Country{} }
	c.JSON(200, gin.H{"countries": list})
}

// POST /api/v1/settings/countries
func (h *SettingsHandler) CreateCountry(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	var req struct {
		Name           string  `json:"name" binding:"required"`
		ISOCode        string  `json:"iso_code" binding:"required"`
		DefaultTaxRate float64 `json:"default_tax_rate"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	now := time.Now().Format(time.RFC3339)
	co := Country{
		ID:             "country_" + uuid.New().String(),
		TenantID:       tenantID,
		Name:           req.Name,
		ISOCode:        req.ISOCode,
		DefaultTaxRate: req.DefaultTaxRate,
		Regions:        []TaxRegion{},
		TaxRateHistory: []TaxRateHistory{},
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if _, err := h.countriesCol(tenantID).Doc(co.ID).Set(ctx, co); err != nil {
		c.JSON(500, gin.H{"error": "failed to create country"})
		return
	}
	c.JSON(201, gin.H{"country": co})
}

// PUT /api/v1/settings/countries/:id
func (h *SettingsHandler) UpdateCountry(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	id := c.Param("id")
	ctx := c.Request.Context()

	doc, err := h.countriesCol(tenantID).Doc(id).Get(ctx)
	if err != nil {
		c.JSON(404, gin.H{"error": "country not found"})
		return
	}
	var co Country
	doc.DataTo(&co)

	var req struct {
		Name           *string          `json:"name"`
		ISOCode        *string          `json:"iso_code"`
		DefaultTaxRate *float64         `json:"default_tax_rate"`
		Regions        []TaxRegion      `json:"regions"`
		TaxRateHistory []TaxRateHistory `json:"tax_rate_history"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	if req.Name != nil { co.Name = *req.Name }
	if req.ISOCode != nil { co.ISOCode = *req.ISOCode }
	if req.DefaultTaxRate != nil { co.DefaultTaxRate = *req.DefaultTaxRate }
	if req.Regions != nil { co.Regions = req.Regions }
	if req.TaxRateHistory != nil { co.TaxRateHistory = req.TaxRateHistory }
	co.UpdatedAt = time.Now().Format(time.RFC3339)

	if _, err := h.countriesCol(tenantID).Doc(id).Set(ctx, co); err != nil {
		c.JSON(500, gin.H{"error": "failed to update country"})
		return
	}
	c.JSON(200, gin.H{"country": co})
}

// DELETE /api/v1/settings/countries/:id
func (h *SettingsHandler) DeleteCountry(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	id := c.Param("id")
	ctx := c.Request.Context()

	if _, err := h.countriesCol(tenantID).Doc(id).Delete(ctx); err != nil {
		c.JSON(500, gin.H{"error": "failed to delete country"})
		return
	}
	c.JSON(200, gin.H{"deleted": true})
}

// ============================================================================
// FEATURE MODULES — configurable feature toggles per tenant
// ============================================================================

type EnabledModules struct {
	WMS               bool   `json:"wms" firestore:"wms"`
	AdvancedDispatch  bool   `json:"advanced_dispatch" firestore:"advanced_dispatch"`
	PurchaseOrders    bool   `json:"purchase_orders" firestore:"purchase_orders"`
	Automation        bool   `json:"automation" firestore:"automation"`
	RMA               bool   `json:"rma" firestore:"rma"`
	AdvancedAnalytics bool   `json:"advanced_analytics" firestore:"advanced_analytics"`
	EmailSystem       bool   `json:"email_system" firestore:"email_system"`
	UpdatedAt         string `json:"updated_at,omitempty" firestore:"updated_at,omitempty"`
}

func (h *SettingsHandler) modulesDoc(tenantID string) *firestore.DocumentRef {
	return h.client.Collection("tenants").Doc(tenantID).Collection("settings").Doc("enabled_modules")
}

// GET /api/v1/settings/modules
func (h *SettingsHandler) GetModules(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	doc, err := h.modulesDoc(tenantID).Get(ctx)
	if err != nil {
		// Default: all modules enabled (existing users get full functionality)
		c.JSON(200, gin.H{"modules": EnabledModules{
			WMS: true, AdvancedDispatch: true, PurchaseOrders: true,
			Automation: true, RMA: true, AdvancedAnalytics: true, EmailSystem: true,
		}})
		return
	}
	var m EnabledModules
	doc.DataTo(&m)
	c.JSON(200, gin.H{"modules": m})
}

// PUT /api/v1/settings/modules
func (h *SettingsHandler) UpdateModules(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	var req EnabledModules
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	req.UpdatedAt = time.Now().Format(time.RFC3339)

	if _, err := h.modulesDoc(tenantID).Set(ctx, req); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "modules": req})
}

// ============================================================================
// SETUP WIZARD — marks tenant onboarding as complete
// ============================================================================

// GET /api/v1/settings/setup-status
func (h *SettingsHandler) GetSetupStatus(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	doc, err := h.client.Collection("tenants").Doc(tenantID).Get(ctx)
	if err != nil {
		c.JSON(200, gin.H{"setup_complete": false})
		return
	}
	data := doc.Data()
	complete, _ := data["setup_complete"].(bool)
	referralSource, _ := data["referral_source"].(string)
	temuWizardStage, _ := data["temu_wizard_stage"].(string)
	sourceChannel, _ := data["source_channel"].(string)
	c.JSON(200, gin.H{
		"setup_complete":    complete,
		"referral_source":   referralSource,
		"temu_wizard_stage": temuWizardStage,
		"source_channel":    sourceChannel,
	})
}

// POST /api/v1/settings/setup-complete
func (h *SettingsHandler) CompleteSetup(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	var req struct {
		BusinessSize   string `json:"business_size"`   // small, medium, large
		ProductCount   string `json:"product_count"`   // 1-50, 51-500, 500+
		OrdersPerDay   string `json:"orders_per_day"`  // 1-10, 11-50, 50+
		HasWarehouse   bool   `json:"has_warehouse"`
		Modules        EnabledModules `json:"modules"`
		SelectedChannels []string `json:"selected_channels"` // e.g. ["amazon", "ebay", "temu"]
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// Save modules
	req.Modules.UpdatedAt = time.Now().Format(time.RFC3339)
	if _, err := h.modulesDoc(tenantID).Set(ctx, req.Modules); err != nil {
		c.JSON(500, gin.H{"error": "failed to save modules: " + err.Error()})
		return
	}

	// Save business profile
	selectedChannels := req.SelectedChannels
	if selectedChannels == nil { selectedChannels = []string{} }
	profileDoc := h.client.Collection("tenants").Doc(tenantID).Collection("settings").Doc("business_profile")
	if _, err := profileDoc.Set(ctx, map[string]interface{}{
		"business_size": req.BusinessSize,
		"product_count": req.ProductCount,
		"orders_per_day": req.OrdersPerDay,
		"has_warehouse": req.HasWarehouse,
		"selected_channels": selectedChannels,
		"updated_at": time.Now().Format(time.RFC3339),
	}); err != nil {
		c.JSON(500, gin.H{"error": "failed to save profile: " + err.Error()})
		return
	}

	// Mark tenant setup as complete
	if _, err := h.client.Collection("tenants").Doc(tenantID).Update(ctx, []firestore.Update{
		{Path: "setup_complete", Value: true},
		{Path: "updated_at", Value: time.Now().UTC()},
	}); err != nil {
		c.JSON(500, gin.H{"error": "failed to mark setup complete: " + err.Error()})
		return
	}

	c.JSON(200, gin.H{"ok": true})
}

// GET /api/v1/settings/selected-channels
func (h *SettingsHandler) GetSelectedChannels(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	doc, err := h.client.Collection("tenants").Doc(tenantID).Collection("settings").Doc("business_profile").Get(ctx)
	if err != nil {
		c.JSON(200, gin.H{"channels": []string{}})
		return
	}
	data := doc.Data()
	channels, _ := data["selected_channels"].([]interface{})
	result := make([]string, 0, len(channels))
	for _, ch := range channels {
		if s, ok := ch.(string); ok {
			result = append(result, s)
		}
	}
	c.JSON(200, gin.H{"channels": result})
}

// ============================================================================
// AI CREDITS — tracking and purchased credit packs
// ============================================================================

// CreditPack represents a purchased credit pack with expiry
type CreditPack struct {
	PackID      string    `json:"pack_id" firestore:"pack_id"`
	TenantID    string    `json:"tenant_id" firestore:"tenant_id"`
	Credits     int       `json:"credits" firestore:"credits"`
	Used        int       `json:"used" firestore:"used"`
	Remaining   int       `json:"remaining" firestore:"remaining"`
	PricePaid   float64   `json:"price_paid" firestore:"price_paid"`
	Currency    string    `json:"currency" firestore:"currency"`
	PurchasedAt time.Time `json:"purchased_at" firestore:"purchased_at"`
	ExpiresAt   time.Time `json:"expires_at" firestore:"expires_at"`
}

func (h *SettingsHandler) creditPacksCol(tenantID string) *firestore.CollectionRef {
	return h.client.Collection("tenants").Doc(tenantID).Collection("credit_packs")
}

// GET /api/v1/ai/credits
func (h *SettingsHandler) GetAICredits(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	// Get tenant for free credits
	tenantDoc, err := h.client.Collection("tenants").Doc(tenantID).Get(ctx)
	if err != nil {
		c.JSON(200, gin.H{"free_used": 0, "free_limit": 100, "monthly_used": 0, "monthly_limit": 0, "purchased_remaining": 0})
		return
	}
	data := tenantDoc.Data()
	freeUsed, _ := data["free_credits_used"].(int64)
	freeLimit, _ := data["free_credits_limit"].(int64)
	if freeLimit == 0 { freeLimit = 100 }

	// Count remaining purchased credits (non-expired)
	now := time.Now()
	purchasedRemaining := 0
	iter := h.creditPacksCol(tenantID).
		Where("expires_at", ">", now).
		Documents(ctx)
	defer iter.Stop()
	for {
		doc, docErr := iter.Next()
		if docErr != nil { break }
		var pack CreditPack
		doc.DataTo(&pack)
		purchasedRemaining += pack.Remaining
	}

	// Plan-to-credits mapping (order-based tiers — ShipStation model)
	plan, _ := data["subscription_plan"].(string)
	monthlyCreditsUsed, _ := data["monthly_credits_used"].(int64)
	estimatedOrders, _ := data["estimated_monthly_orders"].(int64)

	monthlyLimit := planToMonthlyCredits(plan)

	// Auto-reset monthly credits if past reset date
	if resetAt, ok := data["monthly_credits_reset_at"].(time.Time); ok {
		if now.After(resetAt) {
			monthlyCreditsUsed = 0
			nextReset := resetAt.AddDate(0, 1, 0)
			h.client.Collection("tenants").Doc(tenantID).Update(ctx, []firestore.Update{
				{Path: "monthly_credits_used", Value: 0},
				{Path: "monthly_credits_reset_at", Value: nextReset},
			})
		}
	}

	// Recommend plan based on estimated monthly orders
	recommendedPlan := recommendPlan(int(estimatedOrders))

	c.JSON(200, gin.H{
		"free_used":            int(freeUsed),
		"free_limit":           int(freeLimit),
		"free_remaining":       max(0, int(freeLimit)-int(freeUsed)),
		"monthly_used":         int(monthlyCreditsUsed),
		"monthly_limit":        monthlyLimit,
		"monthly_remaining":    max(0, monthlyLimit-int(monthlyCreditsUsed)),
		"purchased_remaining":  purchasedRemaining,
		"total_available":      max(0, int(freeLimit)-int(freeUsed)) + max(0, monthlyLimit-int(monthlyCreditsUsed)) + purchasedRemaining,
		"subscription_plan":    plan,
		"recommended_plan":     recommendedPlan,
		"estimated_monthly_orders": int(estimatedOrders),
	})
}

// POST /api/v1/ai/credits/consume
// Called internally when AI generates a listing. Consumes credits in order:
// purchased (oldest first) → monthly → free
func (h *SettingsHandler) ConsumeCredits(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	var req struct {
		Count int `json:"count" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Count <= 0 {
		c.JSON(400, gin.H{"error": "count must be > 0"})
		return
	}

	remaining := req.Count
	now := time.Now()

	// 1. Consume from purchased packs (oldest expiry first)
	packIter := h.creditPacksCol(tenantID).
		Where("remaining", ">", 0).
		Where("expires_at", ">", now).
		OrderBy("expires_at", firestore.Asc).
		Documents(ctx)

	for remaining > 0 {
		doc, docErr := packIter.Next()
		if docErr != nil { break }
		var pack CreditPack
		doc.DataTo(&pack)
		consume := min(remaining, pack.Remaining)
		pack.Used += consume
		pack.Remaining -= consume
		remaining -= consume
		h.creditPacksCol(tenantID).Doc(pack.PackID).Update(ctx, []firestore.Update{
			{Path: "used", Value: pack.Used},
			{Path: "remaining", Value: pack.Remaining},
		})
	}
	packIter.Stop()

	// 2. Consume from free credits
	if remaining > 0 {
		tenantDoc, _ := h.client.Collection("tenants").Doc(tenantID).Get(ctx)
		if tenantDoc != nil {
			data := tenantDoc.Data()
			freeUsed, _ := data["free_credits_used"].(int64)
			freeLimit, _ := data["free_credits_limit"].(int64)
			if freeLimit == 0 { freeLimit = 100 }
			freeAvailable := int(freeLimit) - int(freeUsed)
			consume := min(remaining, freeAvailable)
			if consume > 0 {
				h.client.Collection("tenants").Doc(tenantID).Update(ctx, []firestore.Update{
					{Path: "free_credits_used", Value: int(freeUsed) + consume},
				})
				remaining -= consume
			}
		}
	}

	consumed := req.Count - remaining
	c.JSON(200, gin.H{"ok": true, "consumed": consumed, "shortfall": remaining})
}

// POST /api/v1/ai/credits/purchase
func (h *SettingsHandler) PurchaseCreditPack(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	var req struct {
		PackSize string `json:"pack_size" binding:"required"` // small, medium, large, bulk
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	packs := map[string]struct{ credits int; price float64 }{
		"small":  {50, 9.99},
		"medium": {150, 24.99},
		"large":  {500, 69.99},
		"bulk":   {1000, 119.99},
	}

	packDef, ok := packs[req.PackSize]
	if !ok {
		c.JSON(400, gin.H{"error": "invalid pack size — use small, medium, large, or bulk"})
		return
	}

	now := time.Now()
	pack := CreditPack{
		PackID:      "cp_" + uuid.New().String(),
		TenantID:    tenantID,
		Credits:     packDef.credits,
		Used:        0,
		Remaining:   packDef.credits,
		PricePaid:   packDef.price,
		Currency:    "GBP",
		PurchasedAt: now,
		ExpiresAt:   now.AddDate(0, 0, 30), // 30-day expiry
	}

	if _, err := h.creditPacksCol(tenantID).Doc(pack.PackID).Set(ctx, pack); err != nil {
		c.JSON(500, gin.H{"error": "failed to save credit pack: " + err.Error()})
		return
	}

	c.JSON(200, gin.H{"ok": true, "pack": pack})
}

// GET /api/v1/ai/credits/packs
func (h *SettingsHandler) ListCreditPacks(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	var packs []CreditPack
	iter := h.creditPacksCol(tenantID).
		OrderBy("purchased_at", firestore.Desc).
		Limit(20).
		Documents(ctx)
	defer iter.Stop()
	for {
		doc, docErr := iter.Next()
		if docErr != nil { break }
		var pack CreditPack
		doc.DataTo(&pack)
		packs = append(packs, pack)
	}
	if packs == nil { packs = []CreditPack{} }
	c.JSON(200, gin.H{"packs": packs})
}

// ─── Plan-to-credits mapping (order-based tiers) ────────────────────────────

// planToMonthlyCredits returns the monthly AI credit allocation for a plan tier.
func planToMonthlyCredits(plan string) int {
	switch plan {
	case "starter_s":
		return 50
	case "starter_m":
		return 200
	case "starter_l":
		return 500
	case "premium":
		return 1000
	case "enterprise":
		return 999999 // effectively unlimited
	default:
		return 0 // no plan = no monthly credits (free tier only)
	}
}

// recommendPlan suggests the right plan tier based on estimated monthly orders.
func recommendPlan(estimatedOrders int) string {
	switch {
	case estimatedOrders <= 100:
		return "starter_s"
	case estimatedOrders <= 500:
		return "starter_m"
	case estimatedOrders <= 1500:
		return "starter_l"
	case estimatedOrders <= 5000:
		return "premium"
	default:
		return "enterprise"
	}
}

// Plan tier display metadata for frontend
type PlanTierInfo struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	OrdersMin      int    `json:"orders_min"`
	OrdersMax      int    `json:"orders_max"` // 0 = unlimited
	MonthlyCredits int    `json:"monthly_credits"`
}

var PlanTiers = []PlanTierInfo{
	{ID: "starter_s", Name: "Starter S", OrdersMin: 0, OrdersMax: 100, MonthlyCredits: 50},
	{ID: "starter_m", Name: "Starter M", OrdersMin: 101, OrdersMax: 500, MonthlyCredits: 200},
	{ID: "starter_l", Name: "Starter L", OrdersMin: 501, OrdersMax: 1500, MonthlyCredits: 500},
	{ID: "premium", Name: "Premium", OrdersMin: 1501, OrdersMax: 5000, MonthlyCredits: 1000},
	{ID: "enterprise", Name: "Enterprise", OrdersMin: 5001, OrdersMax: 0, MonthlyCredits: 999999},
}

func max(a, b int) int { if a > b { return a }; return b }
func min(a, b int) int { if a < b { return a }; return b }

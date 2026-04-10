package services

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/smtp"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"module-a/models"
)

// ============================================================================
// TEMPLATE SERVICE — Module L (Pagebuilder)
// ============================================================================
// Handles:
//   - Template CRUD (Firestore)
//   - Seller profile CRUD (Firestore)
//   - Merge tag resolution (order → template render data)
//   - HTML rendering with real data
//   - Email sending via SMTP
// ============================================================================

type TemplateService struct {
	client *firestore.Client
}

func NewTemplateService(client *firestore.Client) *TemplateService {
	return &TemplateService{client: client}
}

// ── Firestore paths ────────────────────────────────────────────────────────

func (s *TemplateService) templatesCol(tenantID string) *firestore.CollectionRef {
	return s.client.Collection("tenants").Doc(tenantID).Collection("templates")
}

func (s *TemplateService) sellerDoc(tenantID string) *firestore.DocumentRef {
	return s.client.Collection("tenants").Doc(tenantID).Collection("settings").Doc("seller_profile")
}

// ============================================================================
// TEMPLATE CRUD
// ============================================================================

func (s *TemplateService) CreateTemplate(ctx context.Context, tenantID string, tpl *models.Template) error {
	tpl.TenantID = tenantID
	tpl.CreatedAt = time.Now()
	tpl.UpdatedAt = time.Now()
	if tpl.Version == 0 {
		tpl.Version = 1
	}
	_, err := s.templatesCol(tenantID).Doc(tpl.TemplateID).Set(ctx, tpl)
	return err
}

func (s *TemplateService) UpdateTemplate(ctx context.Context, tenantID, templateID string, tpl *models.Template) error {
	tpl.TenantID = tenantID
	tpl.TemplateID = templateID
	tpl.UpdatedAt = time.Now()
	_, err := s.templatesCol(tenantID).Doc(templateID).Set(ctx, tpl)
	return err
}

func (s *TemplateService) GetTemplate(ctx context.Context, tenantID, templateID string) (*models.Template, error) {
	doc, err := s.templatesCol(tenantID).Doc(templateID).Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("template not found: %w", err)
	}
	var tpl models.Template
	if err := doc.DataTo(&tpl); err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}
	return &tpl, nil
}

func (s *TemplateService) ListTemplates(ctx context.Context, tenantID string, templateType string) ([]*models.Template, error) {
	q := s.templatesCol(tenantID).OrderBy("updated_at", firestore.Desc)
	if templateType != "" {
		q = s.templatesCol(tenantID).Where("type", "==", templateType).OrderBy("updated_at", firestore.Desc)
	}

	iter := q.Documents(ctx)
	var templates []*models.Template
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		var tpl models.Template
		if err := doc.DataTo(&tpl); err != nil {
			continue
		}
		templates = append(templates, &tpl)
	}
	if templates == nil {
		templates = []*models.Template{}
	}
	return templates, nil
}

func (s *TemplateService) DeleteTemplate(ctx context.Context, tenantID, templateID string) error {
	_, err := s.templatesCol(tenantID).Doc(templateID).Delete(ctx)
	return err
}

// ToggleTemplate flips the Enabled flag on a template.
func (s *TemplateService) ToggleTemplate(ctx context.Context, tenantID, templateID string, enabled bool) error {
	_, err := s.templatesCol(tenantID).Doc(templateID).Update(ctx, []firestore.Update{
		{Path: "enabled", Value: enabled},
		{Path: "updated_at", Value: time.Now()},
	})
	return err
}

// GetDefaultTemplate returns the default template for a given type
func (s *TemplateService) GetDefaultTemplate(ctx context.Context, tenantID string, templateType models.TemplateType) (*models.Template, error) {
	iter := s.templatesCol(tenantID).
		Where("type", "==", string(templateType)).
		Where("is_default", "==", true).
		Limit(1).
		Documents(ctx)

	doc, err := iter.Next()
	if err != nil {
		// Fall back to first template of this type
		iter2 := s.templatesCol(tenantID).
			Where("type", "==", string(templateType)).
			OrderBy("updated_at", firestore.Desc).
			Limit(1).
			Documents(ctx)
		doc, err = iter2.Next()
		if err != nil {
			return nil, fmt.Errorf("no template of type %s found", templateType)
		}
	}

	var tpl models.Template
	if err := doc.DataTo(&tpl); err != nil {
		return nil, err
	}
	return &tpl, nil
}

// SetDefaultTemplate marks a template as default for its type and clears others
func (s *TemplateService) SetDefaultTemplate(ctx context.Context, tenantID, templateID string) error {
	tpl, err := s.GetTemplate(ctx, tenantID, templateID)
	if err != nil {
		return err
	}

	// Clear existing default for this type
	iter := s.templatesCol(tenantID).
		Where("type", "==", string(tpl.Type)).
		Where("is_default", "==", true).
		Documents(ctx)

	batch := s.client.Batch()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		batch.Update(doc.Ref, []firestore.Update{{Path: "is_default", Value: false}})
	}

	// Set new default
	batch.Update(s.templatesCol(tenantID).Doc(templateID), []firestore.Update{{Path: "is_default", Value: true}})
	_, err = batch.Commit(ctx)
	return err
}

// ============================================================================
// SELLER PROFILE
// ============================================================================

func (s *TemplateService) GetSellerProfile(ctx context.Context, tenantID string) (*models.SellerProfile, error) {
	doc, err := s.sellerDoc(tenantID).Get(ctx)
	if err != nil {
		// Return empty profile rather than error — tenant may not have set it yet
		return &models.SellerProfile{TenantID: tenantID}, nil
	}
	var profile models.SellerProfile
	if err := doc.DataTo(&profile); err != nil {
		return &models.SellerProfile{TenantID: tenantID}, nil
	}
	return &profile, nil
}

func (s *TemplateService) UpdateSellerProfile(ctx context.Context, tenantID string, profile *models.SellerProfile) error {
	profile.TenantID = tenantID
	profile.UpdatedAt = time.Now()
	_, err := s.sellerDoc(tenantID).Set(ctx, profile)
	return err
}

// ============================================================================
// ORDER → RENDER DATA MAPPING
// ============================================================================

// BuildRenderData converts a real order + order lines + seller profile into
// the flat data structure the merge tag resolver uses.
func (s *TemplateService) BuildRenderData(
	ctx context.Context,
	tenantID string,
	order *models.Order,
	lines []*models.OrderLine,
	shippingMethod string,
) (*models.TemplateRenderData, error) {

	seller, _ := s.GetSellerProfile(ctx, tenantID)

	currency := "£"
	if order.Totals.GrandTotal.Currency == "USD" {
		currency = "$"
	} else if order.Totals.GrandTotal.Currency == "EUR" {
		currency = "€"
	}

	formatMoney := func(m models.Money) string {
		if m.Currency == "GBP" {
			return fmt.Sprintf("£%.2f", m.Amount)
		}
		return fmt.Sprintf("%s%.2f", currency, m.Amount)
	}

	data := &models.TemplateRenderData{
		Order: models.OrderRenderData{
			ID:                order.ExternalOrderID,
			Date:              order.OrderDate,
			Status:            order.Status,
			Total:             formatMoney(order.Totals.GrandTotal),
			Subtotal:          formatMoney(order.Totals.Subtotal),
			Tax:               formatMoney(order.Totals.Tax),
			ShippingCost:      formatMoney(order.Totals.Shipping),
			Notes:             order.InternalNotes,
			NumericID:         order.OrderID,
			ExternalReference: order.ExternalOrderID,
			ProcessedDate:     order.OrderDate,
			DispatchByDate:    order.DespatchByDate,
			TrackingNumber:    order.TrackingNumber,
			Vendor:            order.Carrier,
			Currency:          order.Totals.GrandTotal.Currency,
			PaymentMethod:     order.PaymentMethod,
		},
		Customer: models.CustomerRenderData{
			Name:  order.Customer.Name,
			Email: order.Customer.Email,
			Phone: order.Customer.Phone,
		},
		Shipping: models.ShippingRenderData{
			Name:         order.ShippingAddress.Name,
			AddressLine1: order.ShippingAddress.AddressLine1,
			AddressLine2: order.ShippingAddress.AddressLine2,
			AddressLine3: "",
			City:         order.ShippingAddress.City,
			State:        order.ShippingAddress.State,
			PostalCode:   order.ShippingAddress.PostalCode,
			Country:      order.ShippingAddress.Country,
			Method:       shippingMethod,
		},
		Custom: map[string]string{},
	}

	if seller != nil {
		data.Seller = models.SellerRenderData{
			Name:      seller.Name,
			Address:   seller.Address,
			Phone:     seller.Phone,
			Email:     seller.Email,
			LogoURL:   seller.LogoURL,
			VATNumber: seller.VATNumber,
		}
	}

	for _, line := range lines {
		data.Lines = append(data.Lines, models.LineRenderData{
			SKU:         line.SKU,
			Title:       line.Title,
			Quantity:    line.Quantity,
			UnitPrice:   formatMoney(line.UnitPrice),
			LineTotal:   formatMoney(line.LineTotal),
			BatchNumber: "",
			BinRack:     "",
			Weight:      "",
		})
	}

	return data, nil
}

// ============================================================================
// MERGE TAG RESOLUTION
// ============================================================================

var mergeTagRe = regexp.MustCompile(`\{\{([a-zA-Z0-9_.]+)\}\}`)

// ResolveMergeTags replaces {{path}} tokens in an HTML string with real values.
// Unresolved tags are left as-is so the designer can spot them.
func ResolveMergeTags(html string, data *models.TemplateRenderData) string {
	return mergeTagRe.ReplaceAllStringFunc(html, func(match string) string {
		path := mergeTagRe.FindStringSubmatch(match)[1]
		val := resolveMergeTagField(path, data)
		if val == "" {
			return match // leave unresolved tags visible
		}
		return val
	})
}

func resolveMergeTagField(path string, data *models.TemplateRenderData) string {
	parts := strings.SplitN(path, ".", 2)
	if len(parts) != 2 {
		return ""
	}
	ns, key := parts[0], parts[1]

	switch ns {
	case "order":
		switch key {
		case "id":                 return data.Order.ID
		case "date":               return data.Order.Date
		case "status":             return data.Order.Status
		case "total":              return data.Order.Total
		case "subtotal":           return data.Order.Subtotal
		case "tax":                return data.Order.Tax
		case "shipping_cost":      return data.Order.ShippingCost
		case "notes":              return data.Order.Notes
		case "numeric_id":         return data.Order.NumericID
		case "external_reference": return data.Order.ExternalReference
		case "processed_date":     return data.Order.ProcessedDate
		case "dispatch_by_date":   return data.Order.DispatchByDate
		case "tracking_number":    return data.Order.TrackingNumber
		case "vendor":             return data.Order.Vendor
		case "currency":           return data.Order.Currency
		case "payment_method":     return data.Order.PaymentMethod
		}
	case "customer":
		switch key {
		case "name":  return data.Customer.Name
		case "email": return data.Customer.Email
		case "phone": return data.Customer.Phone
		}
	case "shipping":
		switch key {
		case "name":          return data.Shipping.Name
		case "address_line1": return data.Shipping.AddressLine1
		case "address_line2": return data.Shipping.AddressLine2
		case "address_line3": return data.Shipping.AddressLine3
		case "city":          return data.Shipping.City
		case "state":         return data.Shipping.State
		case "postal_code":   return data.Shipping.PostalCode
		case "country":       return data.Shipping.Country
		case "method":        return data.Shipping.Method
		}
	case "seller":
		switch key {
		case "name":       return data.Seller.Name
		case "address":    return data.Seller.Address
		case "phone":      return data.Seller.Phone
		case "email":      return data.Seller.Email
		case "logo_url":   return data.Seller.LogoURL
		case "vat_number": return data.Seller.VATNumber
		}
	case "line":
		if len(data.Lines) > 0 {
			switch key {
			case "sku":          return data.Lines[0].SKU
			case "title":        return data.Lines[0].Title
			case "quantity":     return strconv.Itoa(data.Lines[0].Quantity)
			case "unit_price":   return data.Lines[0].UnitPrice
			case "line_total":   return data.Lines[0].LineTotal
			case "batch_number": return data.Lines[0].BatchNumber
			case "bin_rack":     return data.Lines[0].BinRack
			case "weight":       return data.Lines[0].Weight
			}
		}
	case "custom":
		if data.Custom != nil {
			return data.Custom[key]
		}
	}
	return ""
}

// ============================================================================
// EMAIL SENDING (SMTP)
// ============================================================================

// MailConfig holds SMTP settings for template email sending.
// Named MailConfig (not SMTPConfig) to avoid collision with rule_actions.go.
type MailConfig struct {
	Host     string
	Port     string
	Username string
	Password string
	From     string
	FromName string
	ReplyTo  string
	TLS      bool
}

func smtpConfigFromEnv() MailConfig {
	port := os.Getenv("SMTP_PORT")
	// Default TLS based on port: 465 = implicit TLS, 587/25 = STARTTLS
	// Only use implicit TLS if SMTP_TLS is explicitly "true" or port is 465
	tlsDefault := port == "465"
	tlsStr := os.Getenv("SMTP_TLS")
	tls := tlsDefault
	if tlsStr == "true" {
		tls = true
	} else if tlsStr == "false" {
		tls = false
	}
	return MailConfig{
		Host:     os.Getenv("SMTP_HOST"),
		Port:     port,
		Username: os.Getenv("SMTP_USERNAME"),
		Password: os.Getenv("SMTP_PASSWORD"),
		From:     os.Getenv("SMTP_FROM"),
		FromName: os.Getenv("SMTP_FROM_NAME"),
		ReplyTo:  os.Getenv("SMTP_REPLY_TO"),
		TLS:      tls,
	}
}

// ── Sent Mail Log ─────────────────────────────────────────────────────────────

// SentMailEntry is the document written to tenants/{tid}/sent_mail_log after every send attempt.
type SentMailEntry struct {
	ID           string    `firestore:"id"            json:"id"`
	OrderID      string    `firestore:"order_id"      json:"order_id"`
	TemplateID   string    `firestore:"template_id"   json:"template_id"`
	TemplateName string    `firestore:"template_name" json:"template_name"`
	Recipient    string    `firestore:"recipient"     json:"recipient"`
	Subject      string    `firestore:"subject"       json:"subject"`
	Status       string    `firestore:"status"        json:"status"` // sent | failed | pending
	ErrorMessage string    `firestore:"error_message" json:"error_message"`
	SentAt       time.Time `firestore:"sent_at"       json:"sent_at"`
}

func (s *TemplateService) sentMailCol(tenantID string) *firestore.CollectionRef {
	return s.client.Collection("tenants").Doc(tenantID).Collection("sent_mail_log")
}

// WriteSentMailLog writes a log entry to sent_mail_log after a send attempt.
func (s *TemplateService) WriteSentMailLog(ctx context.Context, tenantID string, entry SentMailEntry) {
	if entry.ID == "" {
		entry.ID = fmt.Sprintf("mail_%d", time.Now().UnixNano())
	}
	if entry.SentAt.IsZero() {
		entry.SentAt = time.Now()
	}
	s.sentMailCol(tenantID).Doc(entry.ID).Set(ctx, entry) //nolint
}

// SendTemplateEmail renders a template with order data and sends via SMTP.
func (s *TemplateService) SendTemplateEmail(
	ctx context.Context,
	tenantID string,
	template *models.Template,
	renderData *models.TemplateRenderData,
	toEmail string,
	subject string,
) error {
	cfg := smtpConfigFromEnv()
	if cfg.Host == "" {
		return fmt.Errorf("SMTP not configured — set SMTP_HOST, SMTP_PORT, SMTP_USERNAME, SMTP_PASSWORD")
	}

	// Build the HTML body from template blocks
	// The frontend serialiser handles this client-side for preview,
	// but for server-side sending we use the stored HTML if provided,
	// or signal that the caller should pass pre-rendered HTML.
	// For now we send the blocks as JSON in a wrapper — a future improvement
	// would run a Go port of the HTML serialiser here.
	// The recommended flow: frontend renders HTML → POST to /send with html_body.
	return fmt.Errorf("use /templates/:id/send with a pre-rendered html_body field")
}

// smtpConfigFromFirestore loads SMTP settings from Firestore for a tenant,
// falling back to env vars for any fields not set in Firestore.
func (s *TemplateService) smtpConfigFromFirestore(ctx context.Context, tenantID string) MailConfig {
	cfg := smtpConfigFromEnv() // start with env var defaults
	doc, err := s.client.Collection("tenants").Doc(tenantID).
		Collection("config").Doc("settings").Get(ctx)
	if err != nil {
		return cfg
	}
	data := doc.Data()
	smtp, _ := data["smtp_config"].(map[string]interface{})
	if smtp == nil {
		return cfg
	}
	if v, _ := smtp["host"].(string); v != "" {
		cfg.Host = v
	}
	if v, _ := smtp["port"].(string); v != "" {
		cfg.Port = v
	}
	if v, _ := smtp["username"].(string); v != "" {
		cfg.Username = v
	}
	if v, _ := smtp["password"].(string); v != "" && v != "••••••••" {
		cfg.Password = v
	}
	if v, _ := smtp["from_address"].(string); v != "" {
		cfg.From = v
	}
	if v, _ := smtp["from_name"].(string); v != "" {
		cfg.FromName = v
	}
	if v, _ := smtp["reply_to"].(string); v != "" {
		cfg.ReplyTo = v
	}
	if v, ok := smtp["tls"].(bool); ok {
		cfg.TLS = v
	}
	return cfg
}

// SendRawEmailForTenant sends a pre-rendered HTML email via SMTP, loading config from
// Firestore (with env var fallback) so reply_to and other tenant settings are honoured.
func (s *TemplateService) SendRawEmailForTenant(ctx context.Context, tenantID, toEmail, subject, htmlBody string) error {
	cfg := s.smtpConfigFromFirestore(ctx, tenantID)
	return sendWithConfig(cfg, toEmail, subject, htmlBody)
}

// SendRawEmail sends a pre-rendered HTML email via SMTP using env var config.
// Kept for backward compatibility with callers outside TemplateService that don't have a tenant context.
func SendRawEmail(toEmail, subject, htmlBody string) error {
	return sendWithConfig(smtpConfigFromEnv(), toEmail, subject, htmlBody)
}

// sendWithConfig is the shared SMTP send implementation used by both SendRawEmail and SendRawEmailForTenant.
func sendWithConfig(cfg MailConfig, toEmail, subject, htmlBody string) error {
	if cfg.Host == "" {
		return fmt.Errorf("SMTP not configured — set SMTP_HOST in .env")
	}

	fromHeader := cfg.From
	if cfg.FromName != "" {
		fromHeader = fmt.Sprintf("%s <%s>", cfg.FromName, cfg.From)
	}

	headers := []string{
		"MIME-Version: 1.0",
		"Content-Type: text/html; charset=UTF-8",
		fmt.Sprintf("From: %s", fromHeader),
		fmt.Sprintf("To: %s", toEmail),
		fmt.Sprintf("Subject: %s", subject),
	}
	if cfg.ReplyTo != "" {
		headers = append(headers, fmt.Sprintf("Reply-To: %s", cfg.ReplyTo))
	}
	headers = append(headers, "", htmlBody)

	msg := strings.Join(headers, "\r\n")

	addr := cfg.Host + ":" + cfg.Port
	auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)

	if cfg.TLS {
		tlsCfg := &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS13}
		conn, err := tls.Dial("tcp", addr, tlsCfg)
		if err != nil {
			return fmt.Errorf("SMTP TLS dial: %w", err)
		}
		client, err := smtp.NewClient(conn, cfg.Host)
		if err != nil {
			return fmt.Errorf("SMTP client: %w", err)
		}
		defer client.Close()

		if err = client.Auth(auth); err != nil {
			return fmt.Errorf("SMTP auth: %w", err)
		}
		if err = client.Mail(cfg.From); err != nil {
			return fmt.Errorf("SMTP MAIL FROM: %w", err)
		}
		if err = client.Rcpt(toEmail); err != nil {
			return fmt.Errorf("SMTP RCPT TO: %w", err)
		}
		wc, err := client.Data()
		if err != nil {
			return fmt.Errorf("SMTP DATA: %w", err)
		}
		defer wc.Close()
		_, err = fmt.Fprint(wc, msg)
		return err
	}

	// STARTTLS — connect plain, upgrade to TLS, then auth
	tlsCfg := &tls.Config{ServerName: cfg.Host}
	client, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("SMTP dial: %w", err)
	}
	defer client.Close()
	if err := client.StartTLS(tlsCfg); err != nil {
		return fmt.Errorf("STARTTLS: %w", err)
	}
	if cfg.Username != "" {
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("SMTP auth: %w", err)
		}
	}
	if err := client.Mail(cfg.From); err != nil {
		return fmt.Errorf("SMTP MAIL FROM: %w", err)
	}
	if err := client.Rcpt(toEmail); err != nil {
		return fmt.Errorf("SMTP RCPT TO: %w", err)
	}
	wc, err := client.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA: %w", err)
	}
	defer wc.Close()
	_, err = fmt.Fprint(wc, msg)
	return err
}

// ============================================================================
// AUTOMATED EVENT EMAIL SENDING
// ============================================================================

// SendEventEmail looks up all enabled, automated email templates matching eventType,
// renders and sends each one using the order's customer email, and writes to the
// Sent Mail Log and Order Audit Trail.
func (s *TemplateService) SendEventEmail(
	ctx context.Context,
	tenantID string,
	eventType string,
	order *models.Order,
) {
	if order == nil {
		return
	}
	toEmail := order.Customer.Email
	if toEmail == "" {
		return
	}

	// Find all enabled, automated email templates matching this trigger event
	iter := s.templatesCol(tenantID).
		Where("type", "==", string(models.TemplateTypeEmail)).
		Where("trigger_type", "==", models.TriggerTypeAutomated).
		Where("trigger_event", "==", eventType).
		Where("enabled", "==", true).
		Documents(ctx)

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var tpl models.Template
		if err := doc.DataTo(&tpl); err != nil {
			continue
		}
		if !tpl.Enabled {
			continue
		}

		// Build render data — load lines
		var linesPtrs []*models.OrderLine
		linesIter := s.client.Collection("tenants").Doc(tenantID).
			Collection("orders").Doc(order.OrderID).
			Collection("lines").Documents(ctx)
		for {
			ld, lerr := linesIter.Next()
			if lerr == iterator.Done {
				break
			}
			if lerr != nil {
				break
			}
			var line models.OrderLine
			if ld.DataTo(&line) == nil {
				linesPtrs = append(linesPtrs, &line)
			}
		}

		renderData, err := s.BuildRenderData(ctx, tenantID, order, linesPtrs, "")
		if err != nil {
			continue
		}

		// The template blocks are stored as opaque JSON; for automated sends we
		// use the template name as the subject and send a minimal HTML placeholder
		// if no pre-rendered HTML is available. The canonical flow is:
		//   frontend renders HTML → POST /templates/:id/send with html_body
		// For server-side automated sends we build a simple text-only fallback body.
		subject := tpl.Name
		htmlBody := fmt.Sprintf(
			`<!DOCTYPE html><html><body style="font-family:sans-serif;font-size:14px;color:#1e293b;padding:24px;">
<p>Dear %s,</p>
<p>This is an automated notification for order %s.</p>
<p>Thank you for your order.</p>
</body></html>`,
			renderData.Customer.Name,
			renderData.Order.ID,
		)
		// Resolve any merge tags if we can embed the blocks as text
		htmlBody = ResolveMergeTags(htmlBody, renderData)

		logEntry := SentMailEntry{
			OrderID:      order.OrderID,
			TemplateID:   tpl.TemplateID,
			TemplateName: tpl.Name,
			Recipient:    toEmail,
			Subject:      subject,
		}

		sendErr := s.SendRawEmailForTenant(ctx, tenantID, toEmail, subject, htmlBody)
		if sendErr != nil {
			logEntry.Status = "failed"
			logEntry.ErrorMessage = sendErr.Error()
			s.WriteSentMailLog(ctx, tenantID, logEntry)
			// Write audit trail via package-level helper imported from handlers package.
			// We call the Firestore directly to avoid a circular import.
			s.writeAuditEntry(ctx, tenantID, order.OrderID, "email_failed", "system",
				fmt.Sprintf("Automated email failed (event=%s, template=%s): %s", eventType, tpl.Name, sendErr.Error()))
		} else {
			logEntry.Status = "sent"
			s.WriteSentMailLog(ctx, tenantID, logEntry)
			s.writeAuditEntry(ctx, tenantID, order.OrderID, "email_sent", "system",
				fmt.Sprintf("Automated email sent (event=%s, template=%s, to=%s)", eventType, tpl.Name, toEmail))
		}
	}
}

// writeAuditEntry writes directly to the order audit trail from the service layer.
func (s *TemplateService) writeAuditEntry(ctx context.Context, tenantID, orderID, action, performedBy, notes string) {
	if tenantID == "" || orderID == "" {
		return
	}
	entry := map[string]interface{}{
		"audit_id":     fmt.Sprintf("aud_%d", time.Now().UnixNano()),
		"action":       action,
		"performed_by": performedBy,
		"notes":        notes,
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	s.client.Collection("tenants").Doc(tenantID).
		Collection("orders").Doc(orderID).
		Collection("audit_trail").Doc(entry["audit_id"].(string)).
		Set(ctx, entry) //nolint
}

// ============================================================================
// AI TEXT GENERATION (proxy — keeps API key server-side)
// ============================================================================

// GenerateTemplateText proxies the AI content request through the existing
// AIService so the Anthropic/Gemini key is never exposed to the browser.
func (s *TemplateService) GenerateTemplateText(
	ctx context.Context,
	aiService *AIService,
	templateType, tone, length, prompt, currentContent, mergeTagList string,
) (string, error) {
	if !aiService.IsAvailable() {
		return "", fmt.Errorf("AI service not configured")
	}

	fullPrompt := strings.Join([]string{
		fmt.Sprintf("You are helping write content for a %s template.", templateType),
		fmt.Sprintf("Tone: %s.", tone),
		fmt.Sprintf("Length: %s.", length),
		fmt.Sprintf("Available merge tags: %s", mergeTagList),
		func() string {
			if currentContent != "" {
				return fmt.Sprintf("Current content in the block: %q", currentContent)
			}
			return ""
		}(),
		fmt.Sprintf("User request: %s", prompt),
		"Rules:",
		"- Return ONLY the text content, no markdown formatting, no code fences.",
		"- Use merge tags like {{customer.name}} for dynamic data where appropriate.",
		"- Match the requested tone precisely.",
		"- Match the requested length precisely.",
		"- Keep it appropriate for the template type.",
	}, "\n")

	// Remove empty lines
	var lines []string
	for _, l := range strings.Split(fullPrompt, "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}

	return aiService.GenerateText(ctx, strings.Join(lines, "\n"))
}

// ============================================================================
// SESSION 3 — VARIABLE FORMULA ENGINE
// ============================================================================
// Evaluates formula expressions for Variable blocks at render time.
// Supports:
//   - String concatenation:  {customer.name} + " " + {shipping.city}
//   - Basic arithmetic:      {line.unit_price} * {line.quantity}
//   - Conditional (ternary): IF({order.tax} > 0, "Inc. VAT", "Ex. VAT")
//   - String functions:      UPPER(...), LOWER(...), TRIM(...)
// ============================================================================

var formulaFieldRe = regexp.MustCompile(`\{([a-zA-Z0-9_.]+)\}`)

// EvaluateFormula evaluates a formula expression against the render data context.
// Field references use single braces: {order.id}, {customer.name}, etc.
func EvaluateFormula(formula string, data *models.TemplateRenderData) string {
	if formula == "" {
		return ""
	}

	// 1. Check for IF(condition, true_val, false_val) — top-level function call
	if upper := strings.ToUpper(strings.TrimSpace(formula)); strings.HasPrefix(upper, "IF(") {
		return evalIF(formula, data)
	}

	// 2. Check for string functions: UPPER(...), LOWER(...), TRIM(...)
	trimmed := strings.TrimSpace(formula)
	if up := strings.ToUpper(trimmed); strings.HasPrefix(up, "UPPER(") && strings.HasSuffix(trimmed, ")") {
		inner := trimmed[6 : len(trimmed)-1]
		return strings.ToUpper(EvaluateFormula(inner, data))
	}
	if up := strings.ToUpper(trimmed); strings.HasPrefix(up, "LOWER(") && strings.HasSuffix(trimmed, ")") {
		inner := trimmed[6 : len(trimmed)-1]
		return strings.ToLower(EvaluateFormula(inner, data))
	}
	if up := strings.ToUpper(trimmed); strings.HasPrefix(up, "TRIM(") && strings.HasSuffix(trimmed, ")") {
		inner := trimmed[5 : len(trimmed)-1]
		return strings.TrimSpace(EvaluateFormula(inner, data))
	}

	// 3. Tokenise by top-level operators + and *
	//    A simple left-to-right pass: split on + (concat) and * (multiply)
	//    We only support binary expressions for arithmetic; chaining works naturally.
	result, ok := evalArithmetic(trimmed, data)
	if ok {
		return result
	}

	// 4. Fallback: treat as a single field reference or literal string
	return evalAtom(trimmed, data)
}

// evalIF handles IF(condition, trueVal, falseVal)
func evalIF(formula string, data *models.TemplateRenderData) string {
	// Strip outer IF(...)
	inner := strings.TrimSpace(formula)
	// Find matching open paren
	start := strings.Index(strings.ToUpper(inner), "IF(")
	if start < 0 {
		return formula
	}
	inner = inner[start+3 : len(inner)-1] // contents between IF( and final )

	// Split into 3 parts at the top-level commas
	parts := splitTopLevel(inner, ',')
	if len(parts) < 3 {
		return formula
	}

	condStr := strings.TrimSpace(parts[0])
	trueVal := strings.TrimSpace(parts[1])
	falseVal := strings.TrimSpace(parts[2])

	if evalConditionExpr(condStr, data) {
		return evalAtom(trueVal, data)
	}
	return evalAtom(falseVal, data)
}

// evalConditionExpr evaluates a simple boolean condition like "{order.tax} > 0"
func evalConditionExpr(expr string, data *models.TemplateRenderData) bool {
	ops := []string{">=", "<=", "!=", ">", "<", "==", "="}
	for _, op := range ops {
		idx := strings.Index(expr, op)
		if idx < 0 {
			continue
		}
		lhs := strings.TrimSpace(expr[:idx])
		rhs := strings.TrimSpace(expr[idx+len(op):])
		lval := evalAtom(lhs, data)
		rval := evalAtom(rhs, data)
		// Try numeric
		lnum, lerr := strconv.ParseFloat(lval, 64)
		rnum, rerr := strconv.ParseFloat(rval, 64)
		if lerr == nil && rerr == nil {
			switch op {
			case ">":        return lnum > rnum
			case "<":        return lnum < rnum
			case ">=":       return lnum >= rnum
			case "<=":       return lnum <= rnum
			case "==", "=":  return lnum == rnum
			case "!=":       return lnum != rnum
			}
		}
		// String comparison
		switch op {
		case "==", "=":  return lval == rval
		case "!=":       return lval != rval
		}
	}
	// Fallback: non-empty string = true
	v := evalAtom(strings.TrimSpace(expr), data)
	return v != "" && v != "0" && v != "false"
}

// evalArithmetic handles expressions with + (concat/add) and * (multiply)
// Returns (result, true) if any operator was found; (_, false) otherwise.
func evalArithmetic(expr string, data *models.TemplateRenderData) (string, bool) {
	// Split by + at top level first (lowest precedence)
	plusParts := splitTopLevel(expr, '+')
	if len(plusParts) > 1 {
		var sb strings.Builder
		for i, part := range plusParts {
			part = strings.TrimSpace(part)
			// Recurse for * within each part
			if res, ok := evalArithmetic(part, data); ok {
				sb.WriteString(res)
			} else {
				sb.WriteString(evalAtom(part, data))
			}
			_ = i
		}
		return sb.String(), true
	}

	// Split by * at top level
	mulParts := splitTopLevel(expr, '*')
	if len(mulParts) > 1 {
		result := 1.0
		for _, part := range mulParts {
			part = strings.TrimSpace(part)
			val := evalAtom(part, data)
			// Strip currency symbols
			val = strings.TrimFunc(val, func(r rune) bool {
				return r == '£' || r == '$' || r == '€' || r == ','
			})
			num, err := strconv.ParseFloat(val, 64)
			if err != nil {
				return "0", true
			}
			result *= num
		}
		return fmt.Sprintf("%.2f", result), true
	}

	return "", false
}

// evalAtom resolves a single token: a {field.path}, a quoted "string literal", or a bare number.
func evalAtom(token string, data *models.TemplateRenderData) string {
	token = strings.TrimSpace(token)

	// Quoted string literal: "..." or '...'
	if (strings.HasPrefix(token, `"`) && strings.HasSuffix(token, `"`)) ||
		(strings.HasPrefix(token, `'`) && strings.HasSuffix(token, `'`)) {
		return token[1 : len(token)-1]
	}

	// Field reference: {field.path}
	if strings.HasPrefix(token, "{") && strings.HasSuffix(token, "}") {
		path := token[1 : len(token)-1]
		return resolveMergeTagField(path, data)
	}

	// Substitute any {field} references within the token
	resolved := formulaFieldRe.ReplaceAllStringFunc(token, func(m string) string {
		path := m[1 : len(m)-1]
		return resolveMergeTagField(path, data)
	})

	return resolved
}

// splitTopLevel splits s on the given separator rune, but only at depth 0
// (i.e. ignoring occurrences inside parentheses or quotes).
func splitTopLevel(s string, sep rune) []string {
	var parts []string
	depth := 0
	inQuote := rune(0)
	start := 0
	for i, ch := range s {
		switch {
		case inQuote != 0:
			if ch == inQuote {
				inQuote = 0
			}
		case ch == '"' || ch == '\'':
			inQuote = ch
		case ch == '(':
			depth++
		case ch == ')':
			depth--
		case ch == sep && depth == 0:
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// ============================================================================
// SESSION 3 — CONDITIONAL STYLE EVALUATION (backend)
// ============================================================================
// ConditionalStyle represents one style override entry on a block.
// The conditions array and style map mirror the frontend schema.
// Used when rendering HTML server-side to apply dynamic style overrides.
// ============================================================================

// EvalConditionalStyles evaluates a slice of conditional style entries
// and returns the merged CSS string for any conditions that pass.
// entries is a []interface{} decoded from the block's conditionalStyles JSON.
// data is the render context.
func EvalConditionalStyles(entries []interface{}, data *models.TemplateRenderData) string {
	if len(entries) == 0 {
		return ""
	}

	var styleOverrides []string

	for _, entry := range entries {
		em, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}

		// Evaluate the conditions array
		conditions, _ := em["conditions"].([]interface{})
		logic, _ := em["logic"].(string)
		if logic == "" {
			logic = "and"
		}

		if !evalConditionGroup(conditions, logic, data) {
			continue
		}

		// Conditions passed — collect style overrides
		styles, _ := em["styles"].(map[string]interface{})
		for prop, val := range styles {
			styleOverrides = append(styleOverrides, fmt.Sprintf("%s:%v", prop, val))
		}
	}

	return strings.Join(styleOverrides, ";")
}

// evalConditionGroup evaluates a group of conditions with AND/OR logic.
func evalConditionGroup(conditions []interface{}, logic string, data *models.TemplateRenderData) bool {
	if len(conditions) == 0 {
		return true
	}

	results := make([]bool, 0, len(conditions))
	for _, cond := range conditions {
		cm, ok := cond.(map[string]interface{})
		if !ok {
			continue
		}
		field, _ := cm["field"].(string)
		operator, _ := cm["operator"].(string)
		value, _ := cm["value"].(string)
		if field == "" {
			continue
		}
		fieldVal := resolveMergeTagField(field, data)
		results = append(results, evalSingleCondition(fieldVal, operator, value))
	}

	if len(results) == 0 {
		return true
	}

	if logic == "or" {
		for _, r := range results {
			if r {
				return true
			}
		}
		return false
	}
	// AND
	for _, r := range results {
		if !r {
			return false
		}
	}
	return true
}

// evalSingleCondition evaluates one condition row (mirrors the frontend conditions.jsx logic).
func evalSingleCondition(fieldVal, operator, value string) bool {
	stripped := strings.TrimFunc(fieldVal, func(r rune) bool {
		return r == '£' || r == '$' || r == '€' || r == ','
	})
	numField, _ := strconv.ParseFloat(stripped, 64)
	numValue, _ := strconv.ParseFloat(value, 64)
	lower := strings.ToLower(fieldVal)
	lowerVal := strings.ToLower(value)

	switch operator {
	case "eq":           return fieldVal == value
	case "neq":          return fieldVal != value
	case "gt":           return numField > numValue
	case "lt":           return numField < numValue
	case "gte":          return numField >= numValue
	case "lte":          return numField <= numValue
	case "contains":     return strings.Contains(lower, lowerVal)
	case "not_contains": return !strings.Contains(lower, lowerVal)
	case "empty":        return fieldVal == ""
	case "not_empty":    return fieldVal != ""
	case "starts_with":  return strings.HasPrefix(lower, lowerVal)
	case "ends_with":    return strings.HasSuffix(lower, lowerVal)
	case "not":          return !strings.Contains(lower, lowerVal)
	case "regex":
		matched, _ := regexp.MatchString(value, fieldVal)
		return matched
	case "like_any":     return strings.Contains(lower, lowerVal)
	case "like_single":
		if len(value) != 1 || len(fieldVal) == 0 {
			return false
		}
		for _, ch := range fieldVal {
			if strings.ContainsRune(value, ch) {
				return true
			}
		}
		return false
	}
	return true
}

// ResolveMergeTagsWithFormulas is like ResolveMergeTags but also processes
// Formula-mode variable blocks. formulaMap is a map of blockID -> formula string;
// the caller extracts these from the block tree before calling.
// For standard merge tags the existing ResolveMergeTags function is used.
func ResolveMergeTagsWithFormulas(html string, data *models.TemplateRenderData, formulaMap map[string]string) string {
	// First resolve standard merge tags
	resolved := ResolveMergeTags(html, data)

	// Then resolve formula placeholders: <!-- formula:blockID:...formula... -->
	formulaRe := regexp.MustCompile(`<!--formula:([^:]+):([^>]+)-->`)
	resolved = formulaRe.ReplaceAllStringFunc(resolved, func(match string) string {
		subs := formulaRe.FindStringSubmatch(match)
		if len(subs) < 3 {
			return match
		}
		formula := subs[2]
		return EvaluateFormula(formula, data)
	})

	return resolved
}

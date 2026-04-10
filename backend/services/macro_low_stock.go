package services

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"html/template"
	"log"
	"net/smtp"
	"strconv"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
)

// ============================================================================
// LOW STOCK NOTIFICATION MACRO — Session 8
// ============================================================================
// Configurable macro that queries inventory for items below their reorder
// point and sends an HTML email listing the low-stock items via SMTP.
// ============================================================================

// LowStockMacroConfig holds the configurable parameters for this macro.
type LowStockMacroConfig struct {
	AllLocations  bool   // Check all warehouse locations
	LocationName  string // If AllLocations is false, filter to this location
	EmailHost     string
	EmailUser     string
	EmailPassword string
	EmailPort     int
	EmailTo       string
}

type LowStockMacroService struct {
	client *firestore.Client
}

func NewLowStockMacroService(client *firestore.Client) *LowStockMacroService {
	return &LowStockMacroService{client: client}
}

type lowStockItem struct {
	SKU          string
	ProductName  string
	Available    int
	ReorderPoint int
	Location     string
}

// Run executes the low stock notification macro for a single tenant.
func (s *LowStockMacroService) Run(ctx context.Context, tenantID string, cfg LowStockMacroConfig) error {
	if cfg.EmailTo == "" || cfg.EmailHost == "" {
		return fmt.Errorf("email_to and email_host are required parameters")
	}

	items, err := s.fetchLowStockItems(ctx, tenantID, cfg)
	if err != nil {
		return fmt.Errorf("fetch low stock items: %w", err)
	}

	if len(items) == 0 {
		log.Printf("[LowStockMacro] tenant=%s: no low-stock items found", tenantID)
		return nil
	}

	html, err := buildLowStockEmailHTML(items, tenantID)
	if err != nil {
		return fmt.Errorf("build email: %w", err)
	}

	subject := fmt.Sprintf("Low Stock Alert — %d item(s) need attention (%s)",
		len(items), time.Now().Format("02 Jan 2006"))

	if err := s.sendEmail(cfg, cfg.EmailTo, subject, html); err != nil {
		return fmt.Errorf("send email: %w", err)
	}

	log.Printf("[LowStockMacro] tenant=%s: sent alert for %d items to %s", tenantID, len(items), cfg.EmailTo)
	return nil
}

func (s *LowStockMacroService) fetchLowStockItems(ctx context.Context, tenantID string, cfg LowStockMacroConfig) ([]lowStockItem, error) {
	inventoryCol := s.client.Collection("tenants").Doc(tenantID).Collection("inventory")

	q := inventoryCol.Where("reorder_point", ">", 0)
	if !cfg.AllLocations && cfg.LocationName != "" {
		q = inventoryCol.Where("location", "==", cfg.LocationName).Where("reorder_point", ">", 0)
	}

	iter := q.Documents(ctx)
	defer iter.Stop()

	var items []lowStockItem
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		data := doc.Data()
		available, _ := data["total_available"].(int64)
		reorderPoint, _ := data["reorder_point"].(int64)

		if available > reorderPoint {
			continue
		}

		sku, _ := data["sku"].(string)
		productName, _ := data["product_name"].(string)
		location, _ := data["location"].(string)

		items = append(items, lowStockItem{
			SKU:          sku,
			ProductName:  productName,
			Available:    int(available),
			ReorderPoint: int(reorderPoint),
			Location:     location,
		})
	}

	return items, nil
}

const lowStockEmailTemplate = `<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"><title>Low Stock Alert</title></head>
<body style="font-family:Arial,sans-serif;font-size:14px;color:#1e293b;background:#f8fafc;padding:0;margin:0;">
  <div style="max-width:680px;margin:32px auto;background:#fff;border-radius:8px;overflow:hidden;box-shadow:0 2px 8px rgba(0,0,0,0.08);">
    <div style="background:#ef4444;padding:24px 32px;">
      <h1 style="color:#fff;margin:0;font-size:20px;">⚠ Low Stock Alert</h1>
      <p style="color:#fecaca;margin:4px 0 0;font-size:13px;">{{.Date}} — {{.Count}} item(s) below reorder point</p>
    </div>
    <div style="padding:24px 32px;">
      <table width="100%" cellpadding="0" cellspacing="0" style="border-collapse:collapse;">
        <thead>
          <tr style="background:#f1f5f9;">
            <th style="padding:10px 12px;text-align:left;font-size:12px;color:#64748b;font-weight:600;border-bottom:1px solid #e2e8f0;">SKU</th>
            <th style="padding:10px 12px;text-align:left;font-size:12px;color:#64748b;font-weight:600;border-bottom:1px solid #e2e8f0;">Product Name</th>
            <th style="padding:10px 12px;text-align:center;font-size:12px;color:#64748b;font-weight:600;border-bottom:1px solid #e2e8f0;">In Stock</th>
            <th style="padding:10px 12px;text-align:center;font-size:12px;color:#64748b;font-weight:600;border-bottom:1px solid #e2e8f0;">Reorder Point</th>
            <th style="padding:10px 12px;text-align:left;font-size:12px;color:#64748b;font-weight:600;border-bottom:1px solid #e2e8f0;">Location</th>
          </tr>
        </thead>
        <tbody>
          {{range .Items}}
          <tr style="border-bottom:1px solid #f1f5f9;">
            <td style="padding:10px 12px;font-family:monospace;font-size:13px;color:#3b82f6;">{{.SKU}}</td>
            <td style="padding:10px 12px;font-size:13px;color:#1e293b;">{{.ProductName}}</td>
            <td style="padding:10px 12px;text-align:center;font-size:13px;font-weight:700;color:{{if eq .Available 0}}#ef4444{{else}}#f59e0b{{end}};">{{.Available}}</td>
            <td style="padding:10px 12px;text-align:center;font-size:13px;color:#64748b;">{{.ReorderPoint}}</td>
            <td style="padding:10px 12px;font-size:13px;color:#64748b;">{{if .Location}}{{.Location}}{{else}}—{{end}}</td>
          </tr>
          {{end}}
        </tbody>
      </table>
      <p style="margin:24px 0 0;font-size:13px;color:#64748b;">This alert was sent automatically by Marketmate. Please replenish stock at your earliest convenience.</p>
    </div>
  </div>
</body>
</html>`

func buildLowStockEmailHTML(items []lowStockItem, tenantID string) (string, error) {
	tmpl, err := template.New("low_stock").Parse(lowStockEmailTemplate)
	if err != nil {
		return "", err
	}
	data := struct {
		Date  string
		Count int
		Items []lowStockItem
	}{
		Date:  time.Now().Format("02 Jan 2006"),
		Count: len(items),
		Items: items,
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (s *LowStockMacroService) sendEmail(cfg LowStockMacroConfig, to, subject, html string) error {
	port := cfg.EmailPort
	if port == 0 {
		port = 587
	}
	addr := cfg.EmailHost + ":" + strconv.Itoa(port)

	msg := buildMIMEMessage(cfg.EmailUser, to, subject, html)

	var auth smtp.Auth
	if cfg.EmailUser != "" && cfg.EmailPassword != "" {
		auth = smtp.PlainAuth("", cfg.EmailUser, cfg.EmailPassword, cfg.EmailHost)
	}

	if port == 465 {
		// SSL
		tlsConfig := &tls.Config{ServerName: cfg.EmailHost, MinVersion: tls.VersionTLS13}
		conn, err := tls.Dial("tcp", addr, tlsConfig)
		if err != nil {
			return fmt.Errorf("TLS dial: %w", err)
		}
		defer conn.Close()
		client, err := smtp.NewClient(conn, cfg.EmailHost)
		if err != nil {
			return fmt.Errorf("SMTP client: %w", err)
		}
		defer client.Close()
		if auth != nil {
			if err := client.Auth(auth); err != nil {
				return fmt.Errorf("SMTP auth: %w", err)
			}
		}
		if err := client.Mail(cfg.EmailUser); err != nil {
			return err
		}
		if err := client.Rcpt(to); err != nil {
			return err
		}
		w, err := client.Data()
		if err != nil {
			return err
		}
		defer w.Close()
		_, err = w.Write([]byte(msg))
		return err
	}

	// STARTTLS / plain
	return smtp.SendMail(addr, auth, cfg.EmailUser, []string{to}, []byte(msg))
}

func buildMIMEMessage(from, to, subject, html string) string {
	return fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-version: 1.0;\r\nContent-Type: text/html; charset=\"UTF-8\";\r\n\r\n%s",
		from, to, subject, html,
	)
}

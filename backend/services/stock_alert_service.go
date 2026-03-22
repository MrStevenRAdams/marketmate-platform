package services

import (
	"context"
	"fmt"
	"log"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
)

// ============================================================================
// STOCK ALERT SERVICE (A-007)
// ============================================================================
// Checks all tenants' inventory for items at or below reorder point and
// fires in-app notifications. Tracks sent alerts to avoid spam (24h cooldown).
//
// Usage:
//   svc := NewStockAlertService(firestoreClient)
//   // Call after every stock-reducing operation, or from a scheduled job:
//   svc.CheckAllTenants(ctx)
// ============================================================================

type StockAlertService struct {
	client *firestore.Client
}

func NewStockAlertService(client *firestore.Client) *StockAlertService {
	return &StockAlertService{client: client}
}

type stockAlertRecord struct {
	AlertID     string    `firestore:"alert_id"`
	TenantID    string    `firestore:"tenant_id"`
	SKU         string    `firestore:"sku"`
	ProductID   string    `firestore:"product_id"`
	ProductName string    `firestore:"product_name"`
	Available   int       `firestore:"available"`
	ReorderPoint int      `firestore:"reorder_point"`
	SentAt      time.Time `firestore:"sent_at"`
}

// ── CheckLowStockForTenant ─────────────────────────────────────────────────
// Checks a single tenant's inventory and fires notifications for low-stock items.

func (s *StockAlertService) CheckLowStockForTenant(ctx context.Context, tenantID string) error {
	inventoryCol := s.client.Collection("tenants").Doc(tenantID).Collection("inventory")

	// Find items where total_available <= reorder_point (and reorder_point > 0)
	iter := inventoryCol.
		Where("reorder_point", ">", 0).
		Documents(ctx)
	defer iter.Stop()

	alerted := 0
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return fmt.Errorf("iterate inventory: %w", err)
		}

		data := doc.Data()
		available, _ := data["total_available"].(int64)
		reorderPoint, _ := data["reorder_point"].(int64)
		sku, _ := data["sku"].(string)
		productName, _ := data["product_name"].(string)
		productID, _ := data["product_id"].(string)

		if available > reorderPoint {
			continue
		}

		// Check cooldown — skip if we already alerted in the last 24 hours
		alreadySent, err := s.alertSentRecently(ctx, tenantID, sku)
		if err != nil || alreadySent {
			continue
		}

		// Create in-app notification
		if err := s.createNotification(ctx, tenantID, stockAlertRecord{
			AlertID:      fmt.Sprintf("alert_%s_%d", sku, time.Now().Unix()),
			TenantID:     tenantID,
			SKU:          sku,
			ProductID:    productID,
			ProductName:  productName,
			Available:    int(available),
			ReorderPoint: int(reorderPoint),
			SentAt:       time.Now(),
		}); err != nil {
			log.Printf("[StockAlert] failed to create notification for %s/%s: %v", tenantID, sku, err)
			continue
		}

		// Record the alert to prevent re-sending within 24h
		if err := s.recordAlert(ctx, tenantID, sku, productID, productName, int(available), int(reorderPoint)); err != nil {
			log.Printf("[StockAlert] failed to record alert for %s/%s: %v", tenantID, sku, err)
		}

		alerted++
	}

	if alerted > 0 {
		log.Printf("[StockAlert] sent %d low-stock alerts for tenant %s", alerted, tenantID)
	}

	return nil
}

// ── CheckAllTenants ────────────────────────────────────────────────────────
// Iterates all tenants and checks each one.

func (s *StockAlertService) CheckAllTenants(ctx context.Context) {
	iter := s.client.Collection("tenants").Documents(ctx)
	defer iter.Stop()

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Printf("[StockAlert] error iterating tenants: %v", err)
			return
		}
		tenantID := doc.Ref.ID
		if err := s.CheckLowStockForTenant(ctx, tenantID); err != nil {
			log.Printf("[StockAlert] error checking tenant %s: %v", tenantID, err)
		}
	}
}

// ── alertSentRecently ──────────────────────────────────────────────────────

func (s *StockAlertService) alertSentRecently(ctx context.Context, tenantID, sku string) (bool, error) {
	cutoff := time.Now().Add(-24 * time.Hour)
	iter := s.client.Collection("tenants").Doc(tenantID).Collection("stock_alerts").
		Where("sku", "==", sku).
		Where("sent_at", ">=", cutoff).
		Limit(1).
		Documents(ctx)
	defer iter.Stop()

	_, err := iter.Next()
	if err == iterator.Done {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// ── recordAlert ────────────────────────────────────────────────────────────

func (s *StockAlertService) recordAlert(ctx context.Context, tenantID, sku, productID, productName string, available, reorderPoint int) error {
	alertID := fmt.Sprintf("alert_%s_%d", sku, time.Now().UnixNano())
	_, err := s.client.Collection("tenants").Doc(tenantID).Collection("stock_alerts").Doc(alertID).Set(ctx, map[string]interface{}{
		"alert_id":      alertID,
		"sku":           sku,
		"product_id":    productID,
		"product_name":  productName,
		"available":     available,
		"reorder_point": reorderPoint,
		"sent_at":       time.Now(),
	})
	return err
}

// ── createNotification ─────────────────────────────────────────────────────
// Writes a notification document that the frontend notification panel reads.

func (s *StockAlertService) createNotification(ctx context.Context, tenantID string, alert stockAlertRecord) error {
	deepLink := fmt.Sprintf("/inventory?sku=%s", alert.SKU)
	body := fmt.Sprintf(
		"SKU %s (%s) has only %d units available (reorder point: %d). Replenishment may be required.",
		alert.SKU, alert.ProductName, alert.Available, alert.ReorderPoint,
	)

	var severity string
	if alert.Available == 0 {
		severity = "critical"
	} else {
		severity = "warning"
	}

	notifID := fmt.Sprintf("notif_low_stock_%s_%d", alert.SKU, time.Now().UnixNano())
	_, err := s.client.Collection("tenants").Doc(tenantID).Collection("notifications").Doc(notifID).Set(ctx, map[string]interface{}{
		"notification_id": notifID,
		"type":            "low_stock",
		"severity":        severity,
		"title":           fmt.Sprintf("Low stock: %s", alert.SKU),
		"body":            body,
		"sku":             alert.SKU,
		"product_id":      alert.ProductID,
		"deep_link":       deepLink,
		"read":            false,
		"acknowledged":    false,
		"created_at":      time.Now(),
	})
	return err
}

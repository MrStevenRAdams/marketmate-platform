package services

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"module-a/models"
)

// ============================================================================
// TRACKING MACRO SERVICE — Session 8
// ============================================================================
// Fetches tracking updates from connected carriers and auto-processes orders.
// Uses an interface so carriers can be added without changing core logic.
// ============================================================================

// CarrierTrackingResult holds tracking info returned by a carrier.
type CarrierTrackingResult struct {
	OrderID        string
	TrackingNumber string
	IsDelivered    bool
	IsDispatched   bool
	StatusMessage  string
}

// CarrierAdapter is the interface for carrier-specific tracking lookups.
type CarrierAdapter interface {
	Name() string
	FetchTracking(ctx context.Context, orders []map[string]interface{}) ([]CarrierTrackingResult, error)
}

// stubCarrierAdapter is a no-op adapter used when no real carrier is configured.
type stubCarrierAdapter struct{ name string }

func (a *stubCarrierAdapter) Name() string { return a.name }
func (a *stubCarrierAdapter) FetchTracking(_ context.Context, orders []map[string]interface{}) ([]CarrierTrackingResult, error) {
	log.Printf("[TrackingMacro] stub carrier %s: no real API configured, returning empty results", a.name)
	return nil, nil
}

func carrierAdapterForName(carrier string) CarrierAdapter {
	// Future: return real implementations for Royal Mail, DHL, FedEx, etc.
	return &stubCarrierAdapter{name: carrier}
}

// TrackingMacroService runs the import-tracking macro.
type TrackingMacroService struct {
	client *firestore.Client
}

func NewTrackingMacroService(client *firestore.Client) *TrackingMacroService {
	return &TrackingMacroService{client: client}
}

// Run fetches tracking updates and optionally auto-processes orders.
func (s *TrackingMacroService) Run(ctx context.Context, tenantID, carrier, location string, autoProcess bool) error {
	orders, err := s.fetchDispatchedOrders(ctx, tenantID, location)
	if err != nil {
		return fmt.Errorf("fetch dispatched orders: %w", err)
	}
	if len(orders) == 0 {
		log.Printf("[TrackingMacro] tenant=%s: no dispatched orders needing tracking", tenantID)
		return nil
	}

	adapter := carrierAdapterForName(carrier)
	results, err := adapter.FetchTracking(ctx, orders)
	if err != nil {
		return fmt.Errorf("fetch tracking from %s: %w", carrier, err)
	}

	for _, r := range results {
		if err := s.applyTrackingResult(ctx, tenantID, r, autoProcess); err != nil {
			log.Printf("[TrackingMacro] failed to apply result for order %s: %v", r.OrderID, err)
		}
	}

	log.Printf("[TrackingMacro] tenant=%s carrier=%s: processed %d tracking results", tenantID, carrier, len(results))
	return nil
}

func (s *TrackingMacroService) fetchDispatchedOrders(ctx context.Context, tenantID, location string) ([]map[string]interface{}, error) {
	q := s.client.Collection("tenants").Doc(tenantID).Collection("orders").
		Where("status", "==", "dispatched").
		Limit(100)
	if location != "" {
		q = s.client.Collection("tenants").Doc(tenantID).Collection("orders").
			Where("status", "==", "dispatched").
			Where("warehouse_location", "==", location).
			Limit(100)
	}

	iter := q.Documents(ctx)
	defer iter.Stop()

	var orders []map[string]interface{}
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		data := doc.Data()
		data["order_id"] = doc.Ref.ID
		orders = append(orders, data)
	}
	return orders, nil
}

func (s *TrackingMacroService) applyTrackingResult(ctx context.Context, tenantID string, r CarrierTrackingResult, autoProcess bool) error {
	updates := []firestore.Update{
		{Path: "updated_at", Value: time.Now().Format(time.RFC3339)},
	}
	if r.TrackingNumber != "" {
		updates = append(updates, firestore.Update{Path: "tracking_number", Value: r.TrackingNumber})
	}
	if autoProcess && r.IsDispatched {
		updates = append(updates, firestore.Update{Path: "status", Value: "dispatched"})
	}
	_, err := s.client.Collection("tenants").Doc(tenantID).Collection("orders").Doc(r.OrderID).Update(ctx, updates)
	return err
}

// ============================================================================
// ADDITIONAL RULE ACTIONS — Session 8
// ============================================================================
// replace_diacritics, format_postcode, default_phone_number,
// shipping_cost_to_service
//
// These are wired into ActionExecutor.ExecuteAction in rule_actions.go.
// This file provides the implementation functions called by the action switch.
// ============================================================================

// ExecReplaceDiacritics strips diacritics from address fields on an order.
func ExecReplaceDiacritics(ctx context.Context, client *firestore.Client, tenantID string, orderCtx *models.OrderContext) error {
	if orderCtx.Order == nil {
		return fmt.Errorf("replace_diacritics: no order in context")
	}
	order := orderCtx.Order
	orderID := order.OrderID

	updates := []firestore.Update{
		{Path: "updated_at", Value: time.Now().Format(time.RFC3339)},
	}

	if order.Customer.Name != "" {
		updates = append(updates, firestore.Update{
			Path:  "customer.name",
			Value: removeDiacritics(order.Customer.Name),
		})
	}
	if order.ShippingAddress.AddressLine1 != "" {
		updates = append(updates, firestore.Update{
			Path:  "shipping_address.address_line1",
			Value: removeDiacritics(order.ShippingAddress.AddressLine1),
		})
	}
	if order.ShippingAddress.AddressLine2 != "" {
		updates = append(updates, firestore.Update{
			Path:  "shipping_address.address_line2",
			Value: removeDiacritics(order.ShippingAddress.AddressLine2),
		})
	}
	if order.ShippingAddress.City != "" {
		updates = append(updates, firestore.Update{
			Path:  "shipping_address.city",
			Value: removeDiacritics(order.ShippingAddress.City),
		})
	}

	_, err := client.Collection("tenants").Doc(tenantID).Collection("orders").Doc(orderID).Update(ctx, updates)
	return err
}

// removeDiacritics replaces accented characters with their ASCII equivalents.
func removeDiacritics(s string) string {
	// Decompose Unicode combining characters (NFD) and keep only ASCII.
	// For a production implementation this should use golang.org/x/text/unicode/norm.
	// Here we use an explicit lookup table covering the most common European diacritics.
	replacer := strings.NewReplacer(
		"à", "a", "á", "a", "â", "a", "ã", "a", "ä", "a", "å", "a",
		"À", "A", "Á", "A", "Â", "A", "Ã", "A", "Ä", "A", "Å", "A",
		"æ", "ae", "Æ", "AE",
		"ç", "c", "Ç", "C",
		"è", "e", "é", "e", "ê", "e", "ë", "e",
		"È", "E", "É", "E", "Ê", "E", "Ë", "E",
		"ì", "i", "í", "i", "î", "i", "ï", "i",
		"Ì", "I", "Í", "I", "Î", "I", "Ï", "I",
		"ñ", "n", "Ñ", "N",
		"ò", "o", "ó", "o", "ô", "o", "õ", "o", "ö", "o", "ø", "o",
		"Ò", "O", "Ó", "O", "Ô", "O", "Õ", "O", "Ö", "O", "Ø", "O",
		"ß", "ss",
		"ù", "u", "ú", "u", "û", "u", "ü", "u",
		"Ù", "U", "Ú", "U", "Û", "U", "Ü", "U",
		"ý", "y", "ÿ", "y", "Ý", "Y",
		"ž", "z", "Ž", "Z",
		"š", "s", "Š", "S",
		"ř", "r", "Ř", "R",
		"č", "c", "Č", "C",
		"ď", "d", "Ď", "D",
		"ě", "e", "Ě", "E",
		"ğ", "g", "Ğ", "G",
		"ı", "i",
		"ł", "l", "Ł", "L",
	)
	return replacer.Replace(s)
}

// ExecFormatPostcode applies UK postcode spacing to shipping.postal_code.
// e.g. "SW1A1AA" → "SW1A 1AA"
func ExecFormatPostcode(ctx context.Context, client *firestore.Client, tenantID string, orderCtx *models.OrderContext) error {
	if orderCtx.Order == nil {
		return fmt.Errorf("format_postcode: no order in context")
	}
	raw := strings.TrimSpace(strings.ToUpper(orderCtx.ShippingPostcode))
	if raw == "" {
		return nil
	}

	formatted := formatUKPostcode(raw)
	if formatted == raw {
		return nil // no change needed
	}

	_, err := client.Collection("tenants").Doc(tenantID).Collection("orders").Doc(orderCtx.Order.OrderID).Update(ctx, []firestore.Update{
		{Path: "shipping_address.postal_code", Value: formatted},
		{Path: "updated_at", Value: time.Now().Format(time.RFC3339)},
	})
	return err
}

// formatUKPostcode ensures a single space before the inward code (last 3 chars).
// Input: "SW1A1AA" → "SW1A 1AA"
// Input: "SW1A 1AA" → "SW1A 1AA" (no-op)
func formatUKPostcode(s string) string {
	// Strip all spaces first, then re-insert before last 3 characters
	stripped := strings.ReplaceAll(s, " ", "")
	if len(stripped) < 5 || len(stripped) > 8 {
		return s // not a standard UK postcode length
	}
	// The inward code is always the last 3 characters: digit, letter, letter
	inward := stripped[len(stripped)-3:]
	outward := stripped[:len(stripped)-3]
	// Validate inward code pattern: digit followed by two letters
	if len(inward) == 3 && unicode.IsDigit(rune(inward[0])) &&
		unicode.IsLetter(rune(inward[1])) && unicode.IsLetter(rune(inward[2])) {
		return outward + " " + inward
	}
	return s
}

// ExecDefaultPhoneNumber sets customer phone if empty.
func ExecDefaultPhoneNumber(ctx context.Context, client *firestore.Client, tenantID string, orderCtx *models.OrderContext, defaultNumber string) error {
	if orderCtx.Order == nil {
		return fmt.Errorf("default_phone_number: no order in context")
	}
	if orderCtx.Order.Customer.Phone != "" {
		return nil // already set
	}
	_, err := client.Collection("tenants").Doc(tenantID).Collection("orders").Doc(orderCtx.Order.OrderID).Update(ctx, []firestore.Update{
		{Path: "customer.phone", Value: defaultNumber},
		{Path: "updated_at", Value: time.Now().Format(time.RFC3339)},
	})
	return err
}

// CostServiceMapping defines a single range-to-service mapping entry.
type CostServiceMapping struct {
	MinCost     float64 `json:"min_cost"`
	MaxCost     float64 `json:"max_cost"`
	ServiceName string  `json:"service_name"`
}

// ExecShippingCostToService maps shipping_cost to a service name via cost ranges.
func ExecShippingCostToService(ctx context.Context, client *firestore.Client, tenantID string, orderCtx *models.OrderContext, mappings []CostServiceMapping) error {
	if orderCtx.Order == nil {
		return fmt.Errorf("shipping_cost_to_service: no order in context")
	}
	cost := orderCtx.ShippingCostGBP // Shipping line value from Totals.Shipping.Amount

	var matched string
	for _, m := range mappings {
		if cost >= m.MinCost && cost <= m.MaxCost {
			matched = m.ServiceName
			break
		}
	}
	if matched == "" {
		return nil // no range matched
	}

	_, err := client.Collection("tenants").Doc(tenantID).Collection("orders").Doc(orderCtx.Order.OrderID).Update(ctx, []firestore.Update{
		{Path: "shipping_service", Value: matched},
		{Path: "updated_at", Value: time.Now().Format(time.RFC3339)},
	})
	return err
}

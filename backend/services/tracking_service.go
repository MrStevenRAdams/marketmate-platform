package services

// ============================================================================
// TRACKING SERVICE — Submit tracking numbers back to marketplaces
// ============================================================================
// After a shipment is created and a tracking number obtained, this service
// pushes the confirmation back to the originating marketplace channel.
//
// Amazon: confirmShipment (Orders API v0)
// eBay:   postOrderFulfillment (Fulfillment API)
// Temu:   shipOrder (Temu Open API)
//
// Called by: dispatch_handlers.go → ConfirmShipment endpoint
// Also triggered by: a scheduled task that sweeps for pending reports
//
// Firestore update pattern:
//   After each attempt, the MarketplaceReporting entry on the Shipment
//   document is updated with: status, attempts, reported_at, error.
// ============================================================================

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"module-a/models"
	"module-a/repository"
)

// TrackingService submits tracking numbers to marketplace APIs.
type TrackingService struct {
	repo           *repository.FirestoreRepository
	marketplaceSvc *MarketplaceService
}

// NewTrackingService constructs a TrackingService.
func NewTrackingService(repo *repository.FirestoreRepository, marketplaceSvc *MarketplaceService) *TrackingService {
	return &TrackingService{
		repo:           repo,
		marketplaceSvc: marketplaceSvc,
	}
}

// ============================================================================
// PUBLIC API
// ============================================================================

// ReportShipmentTracking submits tracking for all pending marketplace reports
// on a given shipment. Called immediately after label generation succeeds.
//
// Non-blocking on individual channel failures — it attempts all channels and
// records each result separately in Firestore.
func (s *TrackingService) ReportShipmentTracking(ctx context.Context, tenantID, shipmentID string) error {
	client := s.repo.GetClient()

	// Load shipment
	doc, err := client.Collection("tenants").Doc(tenantID).
		Collection("shipments").Doc(shipmentID).Get(ctx)
	if err != nil {
		return fmt.Errorf("load shipment: %w", err)
	}

	var shipment models.Shipment
	if err := doc.DataTo(&shipment); err != nil {
		return fmt.Errorf("unmarshal shipment: %w", err)
	}

	if shipment.TrackingNumber == "" {
		return fmt.Errorf("shipment %s has no tracking number", shipmentID)
	}

	// Process each marketplace report entry
	for i := range shipment.MarketplaceReporting {
		report := &shipment.MarketplaceReporting[i]
		if report.Status == models.TrackingReportConfirmed {
			log.Printf("[tracking] shipment %s order %s already confirmed, skipping", shipmentID, report.OrderID)
			continue
		}

		var reportErr error
		switch strings.ToLower(report.Channel) {
		case "amazon":
			reportErr = s.reportToAmazon(ctx, tenantID, shipment, report)
		case "ebay":
			reportErr = s.reportToEbay(ctx, tenantID, shipment, report)
		case "temu":
			reportErr = s.reportToTemu(ctx, tenantID, shipment, report)
		default:
			log.Printf("[tracking] unknown channel %q for shipment %s, skipping", report.Channel, shipmentID)
			continue
		}

		// Update status regardless of success/failure
		report.Attempts++
		now := time.Now()
		report.ReportedAt = &now
		if reportErr != nil {
			report.Status = models.TrackingReportFailed
			report.Error = reportErr.Error()
			log.Printf("[tracking] failed to report to %s for order %s: %v",
				report.Channel, report.OrderID, reportErr)
		} else {
			report.Status = models.TrackingReportConfirmed
			report.Error = ""
			log.Printf("[tracking] confirmed tracking to %s for order %s (tracking: %s)",
				report.Channel, report.OrderID, shipment.TrackingNumber)
		}
	}

	// Persist updated reporting array
	_, err = client.Collection("tenants").Doc(tenantID).
		Collection("shipments").Doc(shipmentID).
		Update(ctx, []firestore.Update{
			{Path: "marketplace_reporting", Value: shipment.MarketplaceReporting},
			{Path: "updated_at", Value: time.Now()},
		})
	return err
}

// SweepPendingReports finds all shipments with pending tracking reports and
// retries them. Designed to be called by a Cloud Scheduler job or Cloud Tasks.
// Returns counts of (attempted, succeeded, failed).
func (s *TrackingService) SweepPendingReports(ctx context.Context, tenantID string) (attempted, succeeded, failed int) {
	client := s.repo.GetClient()

	iter := client.Collection("tenants").Doc(tenantID).
		Collection("shipments").
		Where("status", "==", "label_generated").
		Limit(100). // Process in batches
		Documents(ctx)
	defer iter.Stop()

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Printf("[tracking] sweep query error: %v", err)
			break
		}

		var shipment models.Shipment
		if err := doc.DataTo(&shipment); err != nil {
			continue
		}

		// Check if any reports are pending and under max retry
		hasPending := false
		for _, r := range shipment.MarketplaceReporting {
			if r.Status == models.TrackingReportPending ||
				(r.Status == models.TrackingReportFailed && r.Attempts < 5) {
				hasPending = true
				break
			}
		}
		if !hasPending {
			continue
		}

		attempted++
		if err := s.ReportShipmentTracking(ctx, tenantID, shipment.ShipmentID); err != nil {
			failed++
		} else {
			succeeded++
		}
	}

	return attempted, succeeded, failed
}

// ============================================================================
// AMAZON — confirmShipment
// ============================================================================

type amazonConfirmShipmentRequest struct {
	MarketplaceID     string                  `json:"marketplaceId"`
	PackageDetail     amazonPackageDetail      `json:"packageDetail"`
}

type amazonPackageDetail struct {
	PackageReferenceID     string                `json:"packageReferenceId"`
	CarrierCode            string                `json:"carrierCode"`
	CarrierName            string                `json:"carrierName,omitempty"`
	ShippingMethod         string                `json:"shippingMethod,omitempty"`
	TrackingNumber         string                `json:"trackingNumber"`
	ShipDate               string                `json:"shipDate"`
	ShipFromSupplySourceID string                `json:"shipFromSupplySourceId,omitempty"`
	OrderItems             []amazonConfirmItem   `json:"orderItems"`
}

type amazonConfirmItem struct {
	OrderItemID string `json:"orderItemId"`
	Quantity    int    `json:"quantity"`
}

func (s *TrackingService) reportToAmazon(ctx context.Context, tenantID string, shipment models.Shipment, report *models.MarketplaceTrackingReport) error {
	// Load Amazon credential for this channel account
	cred, err := s.marketplaceSvc.GetCredentialByAccountID(ctx, tenantID, report.ChannelAccountID)
	if err != nil {
		return fmt.Errorf("load amazon credential: %w", err)
	}

	// Load the order to get marketplace region and line items
	client := s.repo.GetClient()
	orderDoc, err := client.Collection("tenants").Doc(tenantID).
		Collection("orders").Doc(report.OrderID).Get(ctx)
	if err != nil {
		return fmt.Errorf("load order: %w", err)
	}
	var order models.Order
	if err := orderDoc.DataTo(&order); err != nil {
		return fmt.Errorf("unmarshal order: %w", err)
	}

	// Load order lines to build confirmShipment items
	linesIter := client.Collection("tenants").Doc(tenantID).
		Collection("orders").Doc(report.OrderID).
		Collection("lines").Documents(ctx)
	defer linesIter.Stop()

	var items []amazonConfirmItem
	for {
		lineDoc, err := linesIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var line models.OrderLine
		if lineDoc.DataTo(&line) == nil && line.LineID != "" {
			items = append(items, amazonConfirmItem{
				OrderItemID: line.LineID,
				Quantity:    line.Quantity,
			})
		}
	}

	if len(items) == 0 {
		// No lines found — use a placeholder (Amazon will accept if items match)
		items = []amazonConfirmItem{{
			OrderItemID: "unknown",
			Quantity:    1,
		}}
	}

	// Determine marketplace ID from region
	marketplaceID := amazonMarketplaceID(order.MarketplaceRegion)

	// Map our carrier ID to Amazon's carrier code
	carrierCode, carrierName := amazonCarrierCode(shipment.CarrierID)

	reqBody := amazonConfirmShipmentRequest{
		MarketplaceID: marketplaceID,
		PackageDetail: amazonPackageDetail{
			PackageReferenceID: fmt.Sprintf("pkg-%s", shipment.ShipmentID[:8]),
			CarrierCode:        carrierCode,
			CarrierName:        carrierName,
			ShippingMethod:     shipment.ServiceName,
			TrackingNumber:     shipment.TrackingNumber,
			ShipDate:           time.Now().UTC().Format(time.RFC3339),
			OrderItems:         items,
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	// Determine SP-API endpoint
	endpoint := amazonEndpointForRegion(order.MarketplaceRegion)
	url := fmt.Sprintf("%s/orders/v0/orders/%s/shipmentConfirmation",
		endpoint, report.ExternalOrderID)

	// Get OAuth token
	token, err := s.getAmazonToken(ctx, cred)
	if err != nil {
		return fmt.Errorf("get amazon token: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-amz-access-token", token)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 300 {
		return fmt.Errorf("amazon API returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// ============================================================================
// EBAY — postOrderFulfillment
// ============================================================================

type ebayFulfillmentRequest struct {
	LineItems          []ebayFulfillmentLine `json:"lineItems"`
	ShippedDate        string                `json:"shippedDate"`
	ShippingCarrierCode string               `json:"shippingCarrierCode"`
	TrackingNumber     string                `json:"trackingNumber"`
}

type ebayFulfillmentLine struct {
	LineItemID string `json:"lineItemId"`
	Quantity   int    `json:"quantity"`
}

func (s *TrackingService) reportToEbay(ctx context.Context, tenantID string, shipment models.Shipment, report *models.MarketplaceTrackingReport) error {
	cred, err := s.marketplaceSvc.GetCredentialByAccountID(ctx, tenantID, report.ChannelAccountID)
	if err != nil {
		return fmt.Errorf("load ebay credential: %w", err)
	}

	client := s.repo.GetClient()
	linesIter := client.Collection("tenants").Doc(tenantID).
		Collection("orders").Doc(report.OrderID).
		Collection("lines").Documents(ctx)
	defer linesIter.Stop()

	var lines []ebayFulfillmentLine
	for {
		lineDoc, err := linesIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var line models.OrderLine
		if lineDoc.DataTo(&line) == nil {
			lines = append(lines, ebayFulfillmentLine{
				LineItemID: line.LineID,
				Quantity:   line.Quantity,
			})
		}
	}

	if len(lines) == 0 {
		lines = []ebayFulfillmentLine{{LineItemID: "1", Quantity: 1}}
	}

	carrierCode := ebayCarrierCode(shipment.CarrierID)

	reqBody := ebayFulfillmentRequest{
		LineItems:           lines,
		ShippedDate:         time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
		ShippingCarrierCode: carrierCode,
		TrackingNumber:      shipment.TrackingNumber,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	// eBay Fulfillment API endpoint
	url := fmt.Sprintf("https://api.ebay.com/sell/fulfillment/v1/order/%s/shipping_fulfillment",
		report.ExternalOrderID)

	token, err := s.getEbayToken(ctx, cred)
	if err != nil {
		return fmt.Errorf("get ebay token: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	// eBay returns 201 Created on success
	if resp.StatusCode >= 300 {
		return fmt.Errorf("ebay API returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// ============================================================================
// TEMU — shipOrder
// ============================================================================

type temuShipOrderRequest struct {
	OrderSN       string `json:"order_sn"`
	ShippingMethod string `json:"shipping_method"`
	TrackingNumber string `json:"tracking_number"`
	LogisticsID    int    `json:"logistics_id,omitempty"`
}

func (s *TrackingService) reportToTemu(ctx context.Context, tenantID string, shipment models.Shipment, report *models.MarketplaceTrackingReport) error {
	cred, err := s.marketplaceSvc.GetCredentialByAccountID(ctx, tenantID, report.ChannelAccountID)
	if err != nil {
		return fmt.Errorf("load temu credential: %w", err)
	}

	apiKey := cred.DecryptedAPIKey
	if apiKey == "" {
		return fmt.Errorf("no Temu API key configured")
	}

	reqBody := temuShipOrderRequest{
		OrderSN:        report.ExternalOrderID,
		ShippingMethod: shipment.ServiceName,
		TrackingNumber: shipment.TrackingNumber,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	// Temu uses a query-param API key pattern
	url := fmt.Sprintf("https://openapi.temu.com/api/ship-order?access_token=%s", apiKey)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 300 {
		return fmt.Errorf("temu API returned %d: %s", resp.StatusCode, string(respBody))
	}

	// Temu wraps errors in a success 200 with error codes
	var temuResp struct {
		ErrorCode int    `json:"error_code"`
		ErrorMsg  string `json:"error_msg"`
	}
	if json.Unmarshal(respBody, &temuResp) == nil && temuResp.ErrorCode != 0 {
		return fmt.Errorf("temu error %d: %s", temuResp.ErrorCode, temuResp.ErrorMsg)
	}

	return nil
}

// ============================================================================
// TOKEN HELPERS — thin wrappers around marketplace credentials
// ============================================================================

// DecryptedCredential is a temporary struct populated when we decrypt a
// marketplace credential to make an API call.
type DecryptedCredential struct {
	AccessToken    string
	RefreshToken   string
	DecryptedAPIKey string
	ClientID       string
	ClientSecret   string
	LWAClientID    string
	LWAClientSecret string
	SandboxMode    bool
}

func (s *TrackingService) getAmazonToken(ctx context.Context, cred *DecryptedCredential) (string, error) {
	if cred.AccessToken != "" {
		return cred.AccessToken, nil
	}
	// Refresh using LWA
	if cred.RefreshToken == "" || cred.LWAClientID == "" {
		return "", fmt.Errorf("no refresh token or LWA client ID available")
	}

	type lwaResp struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		RefreshToken string `json:"refresh_token"`
	}

	body := fmt.Sprintf("grant_type=refresh_token&refresh_token=%s&client_id=%s&client_secret=%s",
		cred.RefreshToken, cred.LWAClientID, cred.LWAClientSecret)

	resp, err := http.PostForm("https://api.amazon.com/auth/o2/token",
		urlValues(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var r lwaResp
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", err
	}
	if r.AccessToken == "" {
		return "", fmt.Errorf("empty access token from LWA")
	}
	return r.AccessToken, nil
}

func (s *TrackingService) getEbayToken(ctx context.Context, cred *DecryptedCredential) (string, error) {
	if cred.AccessToken != "" {
		return cred.AccessToken, nil
	}
	return "", fmt.Errorf("no eBay access token available — token refresh not yet implemented in tracking service")
}

// ============================================================================
// CARRIER CODE MAPPINGS
// ============================================================================

func amazonCarrierCode(carrierID string) (code, name string) {
	switch strings.ToLower(carrierID) {
	case "royal_mail", "royalmail":
		return "Royal Mail", "Royal Mail"
	case "dpd":
		return "DPD", "DPD"
	case "evri", "hermes":
		return "Evri", "Evri"
	case "ups":
		return "UPS", "UPS"
	case "fedex":
		return "FedEx", "FedEx"
	case "dhl":
		return "DHL", "DHL"
	default:
		return "Other", carrierID
	}
}

func ebayCarrierCode(carrierID string) string {
	switch strings.ToLower(carrierID) {
	case "royal_mail", "royalmail":
		return "Royal Mail"
	case "dpd":
		return "DPD"
	case "evri", "hermes":
		return "Evri"
	case "ups":
		return "UPS"
	case "fedex":
		return "FedEx"
	case "dhl":
		return "DHL"
	default:
		return "Other"
	}
}

func amazonEndpointForRegion(region string) string {
	switch strings.ToLower(region) {
	case "eu", "uk", "de", "fr", "it", "es", "nl", "pl", "se", "be", "tr":
		return "https://sellingpartnerapi-eu.amazon.com"
	case "fe", "jp", "au", "sg":
		return "https://sellingpartnerapi-fe.amazon.com"
	default:
		return "https://sellingpartnerapi-na.amazon.com"
	}
}

func amazonMarketplaceID(region string) string {
	switch strings.ToLower(region) {
	case "uk":
		return "A1F83G8C2ARO7P"
	case "de":
		return "A1PA6795UKMFR9"
	case "fr":
		return "A13V1IB3VIYZZH"
	case "it":
		return "APJ6JRA9NG5V4"
	case "es":
		return "A1RKKUPIHCS9HS"
	case "us":
		return "ATVPDKIKX0DER"
	case "ca":
		return "A2EUQ1WTGCTBG2"
	default:
		return "A1F83G8C2ARO7P" // Default to UK
	}
}

// ============================================================================
// HELPERS
// ============================================================================

// GetCredentialByAccountID loads and decrypts a marketplace credential
// matching the given channel_account_id (which is stored as CredentialID).
// Delegates to MarketplaceService.ListCredentials and DecryptCredential.
func (s *MarketplaceService) GetCredentialByAccountID(ctx context.Context, tenantID, channelAccountID string) (*DecryptedCredential, error) {
	creds, err := s.ListCredentials(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list credentials: %w", err)
	}

	for _, c := range creds {
		if c.CredentialID == channelAccountID {
			decrypted, err := s.DecryptCredential(ctx, &c)
			if err != nil {
				return nil, fmt.Errorf("decrypt credential: %w", err)
			}
			return decrypted, nil
		}
	}
	return nil, fmt.Errorf("no credential found for channel account %s", channelAccountID)
}

// urlValues is a minimal helper to avoid importing net/url in the token refresh.
func urlValues(encoded string) map[string][]string {
	result := map[string][]string{}
	for _, pair := range strings.Split(encoded, "&") {
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) == 2 {
			result[kv[0]] = []string{kv[1]}
		}
	}
	return result
}

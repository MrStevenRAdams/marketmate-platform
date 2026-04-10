package carriers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Self-registration
// ---------------------------------------------------------------------------

func init() {
	Register(&RoyalMailClickDropAdapter{
		httpClient: &http.Client{Timeout: 60 * time.Second},
	})
}

// ---------------------------------------------------------------------------
// Adapter struct
// ---------------------------------------------------------------------------

// RoyalMailClickDropAdapter implements CarrierAdapter for the Royal Mail
// Click & Drop REST API (https://api.parcel.royalmail.com/api/v1).
//
// Credentials stored in Firestore extra map:
//
//	"api_key"  — Bearer token obtained from Click & Drop Settings → Integrations
//	"is_oba"   — "true" if the account is an Online Business Account (enables
//	             label retrieval and returns directly from the API)
type RoyalMailClickDropAdapter struct {
	httpClient *http.Client
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	rmcdBaseURL    = "https://api.parcel.royalmail.com/api/v1"
	rmcdCarrierID  = "royalmail_clickanddrop"
	rmcdCarrierKey = "Royal Mail OBA" // carrierName used in manifest requests
)

// ---------------------------------------------------------------------------
// GetMetadata
// ---------------------------------------------------------------------------

func (a *RoyalMailClickDropAdapter) GetMetadata() CarrierMetadata {
	return CarrierMetadata{
		ID:          rmcdCarrierID,
		Name:        "royalmail_clickanddrop",
		DisplayName: "Royal Mail (Click & Drop)",
		Country:     "GB",
		IsActive:    true,
		// Feature flags — drives all conditional UI logic.
		// tracking  : status is polled from the Click & Drop order status endpoint.
		//             Full event-level tracking would require the separate Royal Mail
		//             Tracking API V2 (different auth / contract) — not wired here.
		// international : supported with customs data
		// manifest      : POST /manifests closes the day for OBA accounts
		// signature     : Tracked 24/48 Signature + Special Delivery services
		// customs       : CN22/CN23 included with label fetch for international
		// void          : DELETE /orders/{ids} cancels labels
		Features: []string{
			"tracking",
			"international",
			"manifest",
			"signature",
			"customs",
			"void",
		},
	}
}

// ---------------------------------------------------------------------------
// SupportsFeature
// ---------------------------------------------------------------------------

func (a *RoyalMailClickDropAdapter) SupportsFeature(feature CarrierFeature) bool {
	for _, f := range a.GetMetadata().Features {
		if f == string(feature) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// ValidateCredentials
//
// We call GET /version — it is always present regardless of account type or
// order history, requires a valid Bearer token, and returns 200 with version
// info.  A 401 means the key is wrong; any other non-200 is surfaced as an
// error with the status code so it is easy to diagnose.
// ---------------------------------------------------------------------------

func (a *RoyalMailClickDropAdapter) ValidateCredentials(ctx context.Context, creds CarrierCredentials) error {
	apiKey, err := extractAPIKey(creds)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		rmcdBaseURL+"/version", nil)
	if err != nil {
		return fmt.Errorf("royalmail_clickanddrop: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := a.do(req)
	if err != nil {
		return fmt.Errorf("royalmail_clickanddrop: connection error: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized:
		return fmt.Errorf("royalmail_clickanddrop: invalid API key — check Click & Drop Settings > Integrations > Click & Drop API > copy Auth Key")
	case http.StatusForbidden:
		return fmt.Errorf("royalmail_clickanddrop: API key lacks required permissions")
	default:
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("royalmail_clickanddrop: unexpected status %d: %s", resp.StatusCode, string(body))
	}
}

// ---------------------------------------------------------------------------
// GetServices
//
// Click & Drop does not expose a services endpoint — the list is fixed by
// Royal Mail and well-documented.  We return the standard set here.
// OBA accounts may have additional negotiated services; those can be added
// via the "Custom Service Codes" mechanism if needed.
// ---------------------------------------------------------------------------

func (a *RoyalMailClickDropAdapter) GetServices(ctx context.Context, creds CarrierCredentials) ([]ShippingService, error) {
	return []ShippingService{
		// ── Domestic ────────────────────────────────────────────────────────
		{
			Code:          "TRM",
			Name:          "Tracked 24",
			Domestic:      true,
			International: false,
			EstimatedDays: 1,
			MaxWeight:     20.0,
			Features:      []string{"tracking"},
		},
		{
			Code:          "TRN",
			Name:          "Tracked 24 with Signature",
			Domestic:      true,
			International: false,
			EstimatedDays: 1,
			MaxWeight:     20.0,
			Features:      []string{"tracking", "signature"},
		},
		{
			Code:          "TRS",
			Name:          "Tracked 48",
			Domestic:      true,
			International: false,
			EstimatedDays: 2,
			MaxWeight:     20.0,
			Features:      []string{"tracking"},
		},
		{
			Code:          "TRK",
			Name:          "Tracked 48 with Signature",
			Domestic:      true,
			International: false,
			EstimatedDays: 2,
			MaxWeight:     20.0,
			Features:      []string{"tracking", "signature"},
		},
		{
			Code:          "SD1",
			Name:          "Special Delivery Guaranteed by 1pm",
			Domestic:      true,
			International: false,
			EstimatedDays: 1,
			MaxWeight:     10.0,
			Features:      []string{"tracking", "signature"},
		},
		{
			Code:          "SD9",
			Name:          "Special Delivery Guaranteed by 9am",
			Domestic:      true,
			International: false,
			EstimatedDays: 1,
			MaxWeight:     10.0,
			Features:      []string{"tracking", "signature"},
		},
		{
			Code:          "CRL",
			Name:          "1st Class",
			Domestic:      true,
			International: false,
			EstimatedDays: 1,
			MaxWeight:     2.0,
			Features:      []string{},
		},
		{
			Code:          "CRL48",
			Name:          "2nd Class",
			Domestic:      true,
			International: false,
			EstimatedDays: 3,
			MaxWeight:     2.0,
			Features:      []string{},
		},
		{
			Code:          "STL1",
			Name:          "Signed For 1st Class",
			Domestic:      true,
			International: false,
			EstimatedDays: 1,
			MaxWeight:     2.0,
			Features:      []string{"signature"},
		},
		{
			Code:          "STL48",
			Name:          "Signed For 2nd Class",
			Domestic:      true,
			International: false,
			EstimatedDays: 3,
			MaxWeight:     2.0,
			Features:      []string{"signature"},
		},
		// ── International ───────────────────────────────────────────────────
		{
			Code:          "OLA",
			Name:          "International Tracked & Signed",
			Domestic:      false,
			International: true,
			EstimatedDays: 7,
			MaxWeight:     2.0,
			Features:      []string{"tracking", "signature", "customs"},
		},
		{
			Code:          "OLS",
			Name:          "International Tracked",
			Domestic:      false,
			International: true,
			EstimatedDays: 7,
			MaxWeight:     2.0,
			Features:      []string{"tracking", "customs"},
		},
		{
			Code:          "OSA",
			Name:          "International Signed",
			Domestic:      false,
			International: true,
			EstimatedDays: 7,
			MaxWeight:     2.0,
			Features:      []string{"signature", "customs"},
		},
		{
			Code:          "OMP",
			Name:          "International Economy",
			Domestic:      false,
			International: true,
			EstimatedDays: 14,
			MaxWeight:     2.0,
			Features:      []string{"customs"},
		},
	}, nil
}

// ---------------------------------------------------------------------------
// GetRates
//
// The Click & Drop API has no rate-quote endpoint.  We return an empty
// RateResponse so the UI rate-quote flow degrades gracefully.  The
// SupportsFeature("rate_quotes") flag is not set, so the UI will not show
// a "Get Rates" button for this carrier.
// ---------------------------------------------------------------------------

func (a *RoyalMailClickDropAdapter) GetRates(ctx context.Context, creds CarrierCredentials, req RateRequest) (*RateResponse, error) {
	return &RateResponse{Rates: []Rate{}}, nil
}

// ---------------------------------------------------------------------------
// CreateShipment
//
// Flow for Click & Drop:
//  1. POST /orders  — creates one order in the Click & Drop account and gets
//                     back an orderIdentifier (integer).
//  2. GET  /orders/{id}/label — fetches the label PDF (OBA accounts only).
//     For non-OBA accounts we return without LabelData; the user must log
//     into Click & Drop to print manually.
//
// The tracking number is the Royal Mail barcode, returned by the Click & Drop
// order status after the label is generated.  For OBA accounts this is
// available immediately after step 2.
// ---------------------------------------------------------------------------

func (a *RoyalMailClickDropAdapter) CreateShipment(ctx context.Context, creds CarrierCredentials, req ShipmentRequest) (*ShipmentResponse, error) {
	apiKey, err := extractAPIKey(creds)
	if err != nil {
		return nil, err
	}
	isOBA := extractIsOBA(creds)

	// ── Step 1: Create order ────────────────────────────────────────────────
	orderRef := req.Reference
	if orderRef == "" {
		orderRef = fmt.Sprintf("MM-%d", time.Now().UnixMilli())
	}

	weightGrams := 0.0
	if len(req.Parcels) > 0 {
		for _, p := range req.Parcels {
			weightGrams += p.Weight * 1000
		}
	}
	if weightGrams == 0 {
		weightGrams = 500 // default 500 g
	}

	orderPayload := rmcdCreateOrdersRequest{
		Orders: []rmcdCreateOrderRequest{
			{
				OrderReference:        orderRef,
				OrderDate:             time.Now().UTC().Format(time.RFC3339),
				SubTotal:              0,
				ShippingCostCharged:   0,
				Total:                 0,
				LabelGenerationRequest: &rmcdLabelGenerationRequest{
					ServiceCode:    req.ServiceCode,
					PackageSizeCode: resolvePackageSizeCode(weightGrams),
					WeightInGrams:   int(weightGrams),
				},
				Recipient: rmcdRecipient{
					Name:        recipientName(req.ToAddress),
					AddressLine1: req.ToAddress.AddressLine1,
					AddressLine2: req.ToAddress.AddressLine2,
					City:         req.ToAddress.City,
					County:       req.ToAddress.State,
					Postcode:     req.ToAddress.PostalCode,
					CountryCode:  isoToRMCountryCode(req.ToAddress.Country),
					PhoneNumber:  req.ToAddress.Phone,
					EmailAddress: req.ToAddress.Email,
				},
			},
		},
	}

	// Attach customs lines for international shipments
	if req.Options.Customs != nil && req.ToAddress.Country != "GB" {
		orderPayload.Orders[0].CustomsInfo = buildCustomsInfo(req)
	}

	orderBodyBytes, err := json.Marshal(orderPayload)
	if err != nil {
		return nil, fmt.Errorf("royalmail_clickanddrop: marshal order: %w", err)
	}

	createReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		rmcdBaseURL+"/orders", bytes.NewReader(orderBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("royalmail_clickanddrop: build create order request: %w", err)
	}
	createReq.Header.Set("Authorization", "Bearer "+apiKey)
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Accept", "application/json")

	createResp, err := a.do(createReq)
	if err != nil {
		return nil, fmt.Errorf("royalmail_clickanddrop: create order: %w", err)
	}
	defer createResp.Body.Close()

	if createResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(createResp.Body)
		return nil, fmt.Errorf("royalmail_clickanddrop: create order status %d: %s", createResp.StatusCode, string(body))
	}

	var createResult rmcdCreateOrdersResponse
	if err := json.NewDecoder(createResp.Body).Decode(&createResult); err != nil {
		return nil, fmt.Errorf("royalmail_clickanddrop: decode create order response: %w", err)
	}

	if len(createResult.CreatedOrders) == 0 {
		if len(createResult.Errors) > 0 {
			return nil, fmt.Errorf("royalmail_clickanddrop: order rejected: %s", createResult.Errors[0].Message)
		}
		return nil, fmt.Errorf("royalmail_clickanddrop: no order created and no error returned")
	}

	orderID := createResult.CreatedOrders[0].OrderIdentifier
	orderRef = createResult.CreatedOrders[0].OrderReference

	// Build a basic response — we have the order ID but no tracking number yet.
	shipResp := &ShipmentResponse{
		TrackingNumber: fmt.Sprintf("RMCD-%d", orderID), // placeholder until label is generated
		TrackingURL:    "https://www.royalmail.com/track-your-item",
		LabelFormat:    "PDF",
		CarrierRef:     fmt.Sprintf("%d", orderID),
		Cost:           Money{Amount: 0, Currency: "GBP"},
		Metadata:       map[string]string{},
	}

	// ── Step 2: Fetch label (OBA only) ──────────────────────────────────────
	if !isOBA {
		// Non-OBA: order is in Click & Drop; user must print from the website.
		// We return successfully — the UI should surface a note to the user.
		shipResp.Metadata["notes"] = "Label must be printed from your Click & Drop account (non-OBA)"
		return shipResp, nil
	}

	includeReturnLabel := getBoolExtra(req.Options.Extra, "include_return_label")
	labelURL := fmt.Sprintf("%s/orders/%d/label?documentType=postageLabel&includeReturnsLabel=%t",
		rmcdBaseURL, orderID, includeReturnLabel)

	labelReq, err := http.NewRequestWithContext(ctx, http.MethodGet, labelURL, nil)
	if err != nil {
		return nil, fmt.Errorf("royalmail_clickanddrop: build label request: %w", err)
	}
	labelReq.Header.Set("Authorization", "Bearer "+apiKey)
	labelReq.Header.Set("Accept", "application/pdf")

	// Labels are generated asynchronously inside Click & Drop.  We retry a few
	// times with a short back-off before giving up and returning without label data.
	var labelBytes []byte
	for attempt := 1; attempt <= 5; attempt++ {
		labelResp, err := a.do(labelReq)
		if err != nil {
			return nil, fmt.Errorf("royalmail_clickanddrop: fetch label: %w", err)
		}

		switch labelResp.StatusCode {
		case http.StatusOK:
			labelBytes, err = io.ReadAll(labelResp.Body)
			labelResp.Body.Close()
			if err != nil {
				return nil, fmt.Errorf("royalmail_clickanddrop: read label bytes: %w", err)
			}
			goto labelDone
		case http.StatusNotFound:
			// Label not yet generated — wait and retry
			labelResp.Body.Close()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt*2) * time.Second):
			}
		case http.StatusForbidden:
			labelResp.Body.Close()
			// Shouldn't happen because we checked isOBA, but handle gracefully
			shipResp.Metadata["notes"] = "Label retrieval forbidden — confirm this is an OBA account"
			return shipResp, nil
		default:
			body, _ := io.ReadAll(labelResp.Body)
			labelResp.Body.Close()
			return nil, fmt.Errorf("royalmail_clickanddrop: label fetch status %d: %s", labelResp.StatusCode, string(body))
		}
	}

labelDone:
	if len(labelBytes) > 0 {
		shipResp.LabelData = labelBytes
		shipResp.LabelFormat = "PDF"
	} else {
		shipResp.Metadata["notes"] = "Label generation is still in progress — check Click & Drop shortly"
	}

	// ── Step 3: Read back the order to get the real tracking barcode ────────
	trackingNumber, err := a.fetchTrackingNumber(ctx, apiKey, orderID)
	if err == nil && trackingNumber != "" {
		shipResp.TrackingNumber = trackingNumber
		shipResp.TrackingURL = "https://www.royalmail.com/track-your-item#" + trackingNumber
	}

	return shipResp, nil
}

// fetchTrackingNumber retrieves the tracking barcode for a Click & Drop order.
func (a *RoyalMailClickDropAdapter) fetchTrackingNumber(ctx context.Context, apiKey string, orderID int64) (string, error) {
	url := fmt.Sprintf("%s/orders/%d", rmcdBaseURL, orderID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := a.do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil
	}

	// The API returns an array when called with identifiers in the path
	var orders []rmcdGetOrderInfoResource
	if err := json.NewDecoder(resp.Body).Decode(&orders); err != nil {
		return "", nil
	}
	if len(orders) > 0 && orders[0].TrackingNumber != "" {
		return orders[0].TrackingNumber, nil
	}
	return "", nil
}

// ---------------------------------------------------------------------------
// VoidShipment
//
// DELETE /orders/{orderIdentifier} cancels the order and invalidates any label.
// ---------------------------------------------------------------------------

func (a *RoyalMailClickDropAdapter) VoidShipment(ctx context.Context, creds CarrierCredentials, trackingNumber string) error {
	apiKey, err := extractAPIKey(creds)
	if err != nil {
		return err
	}

	// trackingNumber here is CarrierOrderID (the integer ID) stored at shipment
	// creation time.  The dispatch handler stores this in the CarrierOrderID field
	// and passes it through as the void identifier.
	orderID := trackingNumber // dispatcher passes CarrierOrderID into this field

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		fmt.Sprintf("%s/orders/%s", rmcdBaseURL, orderID), nil)
	if err != nil {
		return fmt.Errorf("royalmail_clickanddrop: build void request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := a.do(req)
	if err != nil {
		return fmt.Errorf("royalmail_clickanddrop: void request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("royalmail_clickanddrop: void status %d: %s", resp.StatusCode, string(body))
}

// ---------------------------------------------------------------------------
// GetTracking
//
// Click & Drop has no tracking events endpoint.  We poll the order status
// and map it onto the standard MarketMate tracking statuses.  Full event-level
// tracking would require a separate subscription to the Royal Mail Tracking API
// V2 (different authentication / contract).
// ---------------------------------------------------------------------------

func (a *RoyalMailClickDropAdapter) GetTracking(ctx context.Context, creds CarrierCredentials, trackingNumber string) (*TrackingInfo, error) {
	apiKey, err := extractAPIKey(creds)
	if err != nil {
		return nil, err
	}

	// We use the order identifier stored as CarrierOrderID.
	orderID := trackingNumber

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/orders/%s", rmcdBaseURL, orderID), nil)
	if err != nil {
		return nil, fmt.Errorf("royalmail_clickanddrop: build tracking request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := a.do(req)
	if err != nil {
		return nil, fmt.Errorf("royalmail_clickanddrop: tracking request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("royalmail_clickanddrop: tracking status %d", resp.StatusCode)
	}

	var orders []rmcdGetOrderInfoResource
	if err := json.NewDecoder(resp.Body).Decode(&orders); err != nil {
		return nil, fmt.Errorf("royalmail_clickanddrop: decode tracking response: %w", err)
	}
	if len(orders) == 0 {
		return nil, fmt.Errorf("royalmail_clickanddrop: order not found")
	}

	o := orders[0]
	status := mapRMCDOrderStatus(o.OrderStatus)

	return &TrackingInfo{
		TrackingNumber: o.TrackingNumber,
		Status:         TrackingStatus(status),
		StatusDetail:   o.OrderStatus,
		Location:       "https://www.royalmail.com/track-your-item#" + o.TrackingNumber,
		Events:         []TrackingEvent{}, // no event-level data from Click & Drop API
	}, nil
}

// mapRMCDOrderStatus maps Click & Drop order status strings to the
// canonical MarketMate tracking status set.
func mapRMCDOrderStatus(rmStatus string) string {
	switch strings.ToLower(rmStatus) {
	case "new", "readytoprint":
		return "pre_transit"
	case "labelgenerated":
		return "pre_transit"
	case "despatched":
		return "in_transit"
	case "despatchedbycourier", "despatchedbyothercourier":
		return "in_transit"
	case "cancelled", "deleted":
		return "cancelled"
	default:
		return "pre_transit"
	}
}

// ---------------------------------------------------------------------------
// GenerateManifest
//
// POST /manifests closes the day for OBA accounts.  Royal Mail requires a
// daily manifest to be submitted before collection.
// ---------------------------------------------------------------------------

func (a *RoyalMailClickDropAdapter) GenerateManifest(ctx context.Context, creds CarrierCredentials, shipments []ManifestShipment) (*ManifestResult, error) {
	apiKey, err := extractAPIKey(creds)
	if err != nil {
		return nil, err
	}

	payload := rmcdManifestEligibleOrdersRequest{
		CarrierName: rmcdCarrierKey,
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("royalmail_clickanddrop: marshal manifest request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		rmcdBaseURL+"/manifests", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("royalmail_clickanddrop: build manifest request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := a.do(req)
	if err != nil {
		return nil, fmt.Errorf("royalmail_clickanddrop: manifest request: %w", err)
	}
	defer resp.Body.Close()

	// 201 = manifest created with PDF immediately available
	// 202 = manifest created but PDF not yet ready (async generation)
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("royalmail_clickanddrop: manifest status %d: %s", resp.StatusCode, string(body))
	}

	var manifestResp rmcdManifestOrdersResponse
	if err := json.NewDecoder(resp.Body).Decode(&manifestResp); err != nil {
		return nil, fmt.Errorf("royalmail_clickanddrop: decode manifest response: %w", err)
	}

	result := &ManifestResult{
		CarrierID:     "royalmail_clickanddrop",
		ShipmentCount: len(shipments),
		CreatedAt:     time.Now(),
	}

	// Decode PDF if available immediately (201 response)
	if manifestResp.DocumentPDF != "" {
		pdfBytes, err := base64.StdEncoding.DecodeString(manifestResp.DocumentPDF)
		if err == nil {
			result.Data   = pdfBytes
			result.Format = "pdf"
		}
	}

	return result, nil
}

// ---------------------------------------------------------------------------
// HTTP helpers
// ---------------------------------------------------------------------------

// do executes an HTTP request, routing through the egress proxy if configured.
func (a *RoyalMailClickDropAdapter) do(req *http.Request) (*http.Response, error) {
	proxyURL := os.Getenv("EGRESS_PROXY_URL")
	if proxyURL != "" {
		targetURL := req.URL.String()
		parsedProxy, err := url.Parse(proxyURL)
		if err == nil {
			req.URL = parsedProxy
			req.Host = ""
			req.Header.Set("X-Target-URL", targetURL)
			req.Header.Set("X-Proxy-Secret", os.Getenv("EGRESS_PROXY_SECRET"))
		}
	}
	return a.httpClient.Do(req)
}

// ---------------------------------------------------------------------------
// Credential helpers
// ---------------------------------------------------------------------------

func extractAPIKey(creds CarrierCredentials) (string, error) {
	if creds.Extra != nil {
		if v, ok := creds.Extra["api_key"]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s, nil
			}
		}
	}
	// Fallback: some older credential records may store in Password field
	if creds.Password != "" {
		return creds.Password, nil
	}
	return "", fmt.Errorf("royalmail_clickanddrop: api_key missing from carrier credentials")
}

func extractIsOBA(creds CarrierCredentials) bool {
	if creds.Extra == nil {
		return false
	}
	v, ok := creds.Extra["is_oba"]
	if !ok {
		return false
	}
	if b, ok := v.(bool); ok {
		return b
	}
	if s, ok := v.(string); ok {
		return strings.EqualFold(s, "true") || s == "1"
	}
	return false
}

// ---------------------------------------------------------------------------
// Mapping / utility helpers
// ---------------------------------------------------------------------------

// resolvePackageSizeCode returns the Click & Drop package size code based on
// weight.  For a more accurate mapping, dimensions from the ShipmentRequest
// could be used; weight alone is a safe default.
func resolvePackageSizeCode(weightGrams float64) string {
	switch {
	case weightGrams <= 100:
		return "Letter"
	case weightGrams <= 750:
		return "LargeLetter"
	case weightGrams <= 2000:
		return "SmallParcel"
	default:
		return "MediumParcel"
	}
}

// recipientName returns the address Name field directly.
func recipientName(addr Address) string {
	return addr.Name
}

// isoToRMCountryCode converts a 2-letter ISO country code to the 2-letter
// format expected by Click & Drop (they are the same, but this makes the
// mapping explicit and easy to extend).
func isoToRMCountryCode(iso2 string) string {
	return strings.ToUpper(iso2)
}

// buildCustomsInfo builds the customs line items from the ShipmentRequest.
func buildCustomsInfo(req ShipmentRequest) *rmcdCustomsInfo {
	if req.Options.Customs == nil {
		return nil
	}
	c := req.Options.Customs
	items := make([]rmcdCustomsItem, 0, len(c.Items))
	for _, item := range c.Items {
		items = append(items, rmcdCustomsItem{
			Description: item.Description,
			Quantity:    item.Quantity,
			Value:       item.Value,
			// WeightGrams, HSTariffCode, CountryOfOrigin are not on the base
			// CustomsItem type — they are enriched via CustomsProfile at dispatch time.
		})
	}
	return &rmcdCustomsInfo{
		ReasonForExport: c.ContentsType,
		Items:           items,
	}
}



// getBoolExtra safely reads a bool value from an Options.Extra map.
// Returns false if the key is absent or the value is not a bool.
func getBoolExtra(extra map[string]interface{}, key string) bool {
	if extra == nil {
		return false
	}
	if v, ok := extra[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Click & Drop API request / response types
// ---------------------------------------------------------------------------

type rmcdCreateOrdersRequest struct {
	Orders []rmcdCreateOrderRequest `json:"orders"`
}

type rmcdCreateOrderRequest struct {
	OrderReference         string                      `json:"orderReference,omitempty"`
	OrderDate              string                      `json:"orderDate"`
	SubTotal               float64                     `json:"subtotal"`
	ShippingCostCharged    float64                     `json:"shippingCostCharged"`
	Total                  float64                     `json:"total"`
	Recipient              rmcdRecipient               `json:"recipient"`
	LabelGenerationRequest *rmcdLabelGenerationRequest `json:"label,omitempty"`
	CustomsInfo            *rmcdCustomsInfo            `json:"customsInfo,omitempty"`
}

type rmcdRecipient struct {
	Name         string `json:"name"`
	AddressLine1 string `json:"addressLine1"`
	AddressLine2 string `json:"addressLine2,omitempty"`
	City         string `json:"city"`
	County       string `json:"county,omitempty"`
	Postcode     string `json:"postcode"`
	CountryCode  string `json:"countryCode"`
	PhoneNumber  string `json:"phoneNumber,omitempty"`
	EmailAddress string `json:"emailAddress,omitempty"`
}

type rmcdLabelGenerationRequest struct {
	ServiceCode     string `json:"serviceCode"`
	PackageSizeCode string `json:"packageSizeCode"`
	WeightInGrams   int    `json:"weightInGrams"`
}

type rmcdCustomsInfo struct {
	ReasonForExport string             `json:"reasonForExport,omitempty"`
	Items           []rmcdCustomsItem  `json:"items,omitempty"`
}

type rmcdCustomsItem struct {
	Description     string  `json:"description"`
	Quantity        int     `json:"quantity"`
	Value           float64 `json:"value"`
	Weight          float64 `json:"weightInGrams"`
	HSCode          string  `json:"hsCode,omitempty"`
	CountryOfOrigin string  `json:"countryOfOrigin,omitempty"`
}

type rmcdCreateOrdersResponse struct {
	CreatedOrders []rmcdCreatedOrder        `json:"createdOrders"`
	Errors        []rmcdCreateOrderError    `json:"failedOrders"`
}

type rmcdCreatedOrder struct {
	OrderIdentifier int64  `json:"orderIdentifier"`
	OrderReference  string `json:"orderReference"`
}

type rmcdCreateOrderError struct {
	OrderReference string `json:"orderReference"`
	Message        string `json:"message"`
}

type rmcdGetOrderInfoResource struct {
	OrderIdentifier int64  `json:"orderIdentifier"`
	OrderReference  string `json:"orderReference"`
	OrderStatus     string `json:"orderStatus"`
	TrackingNumber  string `json:"trackingNumber"`
}

type rmcdManifestEligibleOrdersRequest struct {
	CarrierName string `json:"carrierName,omitempty"`
}

type rmcdManifestOrdersResponse struct {
	ManifestNumber int64  `json:"manifestNumber"`
	DocumentPDF    string `json:"documentPdf,omitempty"` // base64 PDF
}

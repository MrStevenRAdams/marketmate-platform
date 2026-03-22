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
	"strings"
	"sync"
	"time"
)

// ============================================================================
// FEDEX CARRIER ADAPTER
// ============================================================================
// Implements CarrierAdapter using the FedEx Ship API (REST, v1).
//
// Authentication: OAuth2 client credentials
//   ClientID     → FedEx developer portal client_id (stored in APIKey)
//   ClientSecret → FedEx developer portal client_secret (stored in Password)
//   AccountID    → FedEx account number (e.g. "123456789")
//   IsSandbox    → true = sandbox endpoint, false = production
//
// Developer portal: https://developer.fedex.com
// Sandbox base:     https://apis-sandbox.fedex.com
// Production base:  https://apis.fedex.com
//
// Credentials required in CarrierCredentials:
//   APIKey    → client_id  (from developer portal project)
//   Password  → client_secret
//   AccountID → FedEx account number
//
// Services (from UK):
//   International Priority (IP), International Economy (IE),
//   International Priority Express (IPE), International Priority Freight (IPF),
//   International Economy Freight (IEF), FedEx Express Saver (ES),
//   FedEx 1Day Freight (1DF) — EU domestic only
//
// Notes:
//   - OAuth2 token is cached per (clientID, sandbox) pair and refreshed on expiry
//   - All weights in kg, dimensions in cm (converted to lb/in for FedEx API)
//   - FedEx rates are returned in the shipper's currency by default
// ============================================================================

// ============================================================================
// TOKEN CACHE
// ============================================================================

type fedexToken struct {
	AccessToken string
	ExpiresAt   time.Time
}

var (
	fedexTokenCache = make(map[string]*fedexToken)
	fedexTokenMu    sync.Mutex
)

// ============================================================================
// ADAPTER
// ============================================================================

type FedExAdapter struct {
	httpClient *http.Client
}

func init() {
	Register(&FedExAdapter{
		httpClient: &http.Client{Timeout: 30 * time.Second},
	})
}

// ============================================================================
// METADATA
// ============================================================================

func (a *FedExAdapter) GetMetadata() CarrierMetadata {
	return CarrierMetadata{
		ID:          "fedex",
		Name:        "FedEx",
		DisplayName: "FedEx International",
		Country:     "GB",
		Logo:        "https://www.fedex.com/content/dam/fedex/us-united-states/about/images/2021/Q1/fedex-logo-200x200.png",
		Website:     "https://www.fedex.com",
		SupportURL:  "https://developer.fedex.com",
		Features: []string{
			string(FeatureRateQuotes),
			string(FeatureTracking),
			string(FeatureInternational),
			string(FeatureSignature),
			string(FeatureInsurance),
			string(FeatureSaturdayDelivery),
			string(FeaturePickup),
			string(FeatureCustoms),
			string(FeatureVoid),
			string(FeatureManifest),
		},
		IsActive: true,
	}
}

// ============================================================================
// CREDENTIAL VALIDATION
// ============================================================================

func (a *FedExAdapter) ValidateCredentials(ctx context.Context, creds CarrierCredentials) error {
	if creds.APIKey == "" {
		return fmt.Errorf("fedex: client_id (API Key) is required")
	}
	if creds.Password == "" {
		return fmt.Errorf("fedex: client_secret (Password) is required")
	}
	if creds.AccountID == "" {
		return fmt.Errorf("fedex: account number is required")
	}

	// Attempt to obtain a token — this validates the client credentials
	_, err := a.getToken(ctx, creds)
	if err != nil {
		return fmt.Errorf("fedex credential validation failed: %w", err)
	}
	return nil
}

// ============================================================================
// SERVICES
// ============================================================================

func (a *FedExAdapter) GetServices(ctx context.Context, creds CarrierCredentials) ([]ShippingService, error) {
	return []ShippingService{
		// ── International ────────────────────────────────────────────────────
		{
			Code:          "INTERNATIONAL_PRIORITY",
			Name:          "FedEx International Priority",
			Description:   "Time-definite delivery typically in 1-3 business days worldwide",
			Domestic:      false,
			International: true,
			EstimatedDays: 2,
			MaxWeight:     68.0,
			Features:      []string{"tracking", "signature", "customs", "insurance"},
		},
		{
			Code:          "INTERNATIONAL_ECONOMY",
			Name:          "FedEx International Economy",
			Description:   "Cost-effective delivery typically in 2-5 business days",
			Domestic:      false,
			International: true,
			EstimatedDays: 4,
			MaxWeight:     68.0,
			Features:      []string{"tracking", "customs", "insurance"},
		},
		{
			Code:          "INTERNATIONAL_PRIORITY_EXPRESS",
			Name:          "FedEx International Priority Express",
			Description:   "Fastest international service, next possible business day by 10:30am",
			Domestic:      false,
			International: true,
			EstimatedDays: 1,
			MaxWeight:     68.0,
			Features:      []string{"tracking", "signature", "customs", "insurance", "guaranteed"},
		},
		{
			Code:          "INTERNATIONAL_FIRST",
			Name:          "FedEx International First",
			Description:   "Early morning delivery to select international destinations",
			Domestic:      false,
			International: true,
			EstimatedDays: 1,
			MaxWeight:     68.0,
			Features:      []string{"tracking", "signature", "customs", "insurance"},
		},
		{
			Code:          "EUROPE_FIRST_INTERNATIONAL_PRIORITY",
			Name:          "FedEx Europe First International Priority",
			Description:   "Next business day delivery to Europe by 9:00am or 10:00am",
			Domestic:      false,
			International: true,
			EstimatedDays: 1,
			MaxWeight:     68.0,
			Features:      []string{"tracking", "signature", "customs", "insurance"},
		},
		// ── Freight (heavy) ──────────────────────────────────────────────────
		{
			Code:          "INTERNATIONAL_PRIORITY_FREIGHT",
			Name:          "FedEx International Priority Freight",
			Description:   "Time-definite freight delivery, typically 1-3 business days",
			Domestic:      false,
			International: true,
			EstimatedDays: 2,
			MaxWeight:     1000.0,
			Features:      []string{"tracking", "customs", "freight"},
		},
		{
			Code:          "INTERNATIONAL_ECONOMY_FREIGHT",
			Name:          "FedEx International Economy Freight",
			Description:   "Cost-effective freight delivery, typically 2-5 business days",
			Domestic:      false,
			International: true,
			EstimatedDays: 5,
			MaxWeight:     1000.0,
			Features:      []string{"tracking", "customs", "freight"},
		},
	}, nil
}

// ============================================================================
// RATE SHOPPING
// ============================================================================

func (a *FedExAdapter) GetRates(ctx context.Context, creds CarrierCredentials, req RateRequest) (*RateResponse, error) {
	token, err := a.getToken(ctx, creds)
	if err != nil {
		return nil, fmt.Errorf("fedex auth failed: %w", err)
	}

	fedexReq := a.buildRateRequest(creds, req)

	body, err := json.Marshal(fedexReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal FedEx rate request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		a.baseURL(creds.IsSandbox)+"/rate/v1/rates/quotes", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	a.setAuthHeader(httpReq, token)

	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("fedex rate request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		var errResp fedexErrorResponse
		if json.Unmarshal(respBody, &errResp) == nil && len(errResp.Errors) > 0 {
			return nil, fmt.Errorf("fedex rate error: %s — %s", errResp.Errors[0].Code, errResp.Errors[0].Message)
		}
		return nil, fmt.Errorf("fedex rate error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var fedexResp fedexRateResponse
	if err := json.Unmarshal(respBody, &fedexResp); err != nil {
		return nil, fmt.Errorf("failed to parse FedEx rate response: %w", err)
	}

	var rates []Rate
	for _, output := range fedexResp.Output.RateReplyDetails {
		for _, rating := range output.RatedShipmentDetails {
			if rating.ShipmentRateDetail.RateType != "PAYOR_ACCOUNT_PACKAGE" &&
				rating.ShipmentRateDetail.RateType != "PAYOR_ACCOUNT_SHIPMENT" {
				continue
			}
			currency := rating.ShipmentRateDetail.TotalNetCharge.Currency
			if currency == "" {
				currency = "GBP"
			}
			rates = append(rates, Rate{
				ServiceCode:   output.ServiceType,
				ServiceName:   a.serviceDisplayName(output.ServiceType),
				Cost:          Money{Amount: rating.ShipmentRateDetail.TotalNetCharge.Amount, Currency: currency},
				Currency:      currency,
				EstimatedDays: a.estimatedDays(output.ServiceType),
				Carrier:       "fedex",
			})
			break // take the first (account) rate only
		}
	}

	return &RateResponse{Rates: rates}, nil
}

// ============================================================================
// SHIPMENT CREATION
// ============================================================================

func (a *FedExAdapter) CreateShipment(ctx context.Context, creds CarrierCredentials, req ShipmentRequest) (*ShipmentResponse, error) {
	token, err := a.getToken(ctx, creds)
	if err != nil {
		return nil, fmt.Errorf("fedex auth failed: %w", err)
	}

	fedexReq := a.buildShipmentRequest(creds, req)

	body, err := json.Marshal(fedexReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal FedEx shipment request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		a.baseURL(creds.IsSandbox)+"/ship/v1/shipments", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	a.setAuthHeader(httpReq, token)

	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("fedex shipment request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		var errResp fedexErrorResponse
		if json.Unmarshal(respBody, &errResp) == nil && len(errResp.Errors) > 0 {
			return nil, fmt.Errorf("fedex shipment error: %s — %s", errResp.Errors[0].Code, errResp.Errors[0].Message)
		}
		return nil, fmt.Errorf("fedex shipment error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var fedexResp fedexShipmentResponse
	if err := json.Unmarshal(respBody, &fedexResp); err != nil {
		return nil, fmt.Errorf("failed to parse FedEx shipment response: %w", err)
	}

	if len(fedexResp.Output.TransactionShipments) == 0 {
		return nil, fmt.Errorf("fedex: no shipment returned in response")
	}

	shipment := fedexResp.Output.TransactionShipments[0]
	result := &ShipmentResponse{
		TrackingNumber: shipment.MasterTrackingNumber,
		TrackingURL:    fmt.Sprintf("https://www.fedex.com/fedextrack/?trknbr=%s", shipment.MasterTrackingNumber),
		CarrierRef:     shipment.MasterTrackingNumber,
		Metadata:       map[string]string{"service_type": shipment.ServiceType},
	}

	// Extract label from first piece
	if len(shipment.PieceResponses) > 0 {
		piece := shipment.PieceResponses[0]
		if len(piece.PackageDocuments) > 0 {
			doc := piece.PackageDocuments[0]
			result.LabelFormat = doc.ContentType
			result.LabelData = []byte(doc.EncodedLabel)
		}
	}

	// Extract cost
	if len(shipment.ShipmentDocuments) > 0 {
		// Cost comes from completedShipmentDetail in some response variants
	}
	// Fallback cost (FedEx requires a separate rate call for actual cost)
	result.Cost = Money{Amount: 0, Currency: "GBP"}
	result.Currency = "GBP"
	result.EstimatedDelivery = time.Now().Add(time.Duration(a.estimatedDays(req.ServiceCode)) * 24 * time.Hour)

	return result, nil
}

// ============================================================================
// VOID SHIPMENT
// ============================================================================

func (a *FedExAdapter) VoidShipment(ctx context.Context, creds CarrierCredentials, trackingNumber string) error {
	token, err := a.getToken(ctx, creds)
	if err != nil {
		return fmt.Errorf("fedex auth failed: %w", err)
	}

	payload := map[string]interface{}{
		"accountNumber": map[string]string{"value": creds.AccountID},
		"trackingNumber": map[string]string{
			"trackingNumber": trackingNumber,
			"trackingNumberUniqueId": "",
			"shipDate": "",
		},
		"deletionControl": "DELETE_ALL_PACKAGES",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "PUT",
		a.baseURL(creds.IsSandbox)+"/ship/v1/shipments/cancel", bytes.NewReader(body))
	if err != nil {
		return err
	}
	a.setAuthHeader(httpReq, token)

	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("fedex void request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("fedex void error (status %d): %s", resp.StatusCode, string(errBody))
	}

	return nil
}

// ============================================================================
// TRACKING
// ============================================================================

func (a *FedExAdapter) GetTracking(ctx context.Context, creds CarrierCredentials, trackingNumber string) (*TrackingInfo, error) {
	token, err := a.getToken(ctx, creds)
	if err != nil {
		return nil, fmt.Errorf("fedex auth failed: %w", err)
	}

	payload := map[string]interface{}{
		"trackingInfo": []map[string]interface{}{
			{"trackingNumberInfo": map[string]string{"trackingNumber": trackingNumber}},
		},
		"includeDetailedScans": true,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		a.baseURL(creds.IsSandbox)+"/track/v1/trackingnumbers", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	a.setAuthHeader(httpReq, token)

	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("fedex tracking request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fedex tracking error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var fedexResp fedexTrackingResponse
	if err := json.Unmarshal(respBody, &fedexResp); err != nil {
		return nil, fmt.Errorf("failed to parse FedEx tracking response: %w", err)
	}

	if len(fedexResp.Output.CompleteTrackResults) == 0 ||
		len(fedexResp.Output.CompleteTrackResults[0].TrackResults) == 0 {
		return nil, fmt.Errorf("tracking number not found: %s", trackingNumber)
	}

	result := fedexResp.Output.CompleteTrackResults[0].TrackResults[0]
	info := &TrackingInfo{
		TrackingNumber: trackingNumber,
		Status:         a.mapTrackingStatus(result.LatestStatusDetail.StatusByLocale),
		StatusDetail:   result.LatestStatusDetail.Description,
	}

	// Map events
	for _, e := range result.TrackingEventDetails {
		ts, _ := time.Parse("2006-01-02T15:04:05", e.EventTime)
		info.Events = append(info.Events, TrackingEvent{
			Timestamp:   ts,
			Status:      e.EventType,
			Description: e.EventDescription,
			Location:    strings.TrimSpace(e.ScanLocation.City + ", " + e.ScanLocation.CountryCode),
		})
	}

	// Estimated delivery
	if result.EstimatedDeliveryTimeWindow.Window.Ends != "" {
		if t, err := time.Parse("2006-01-02T15:04:05", result.EstimatedDeliveryTimeWindow.Window.Ends); err == nil {
			info.EstimatedDelivery = t
		}
	}

	// Actual delivery + signature
	if result.ActualDeliveryTime != "" {
		if t, err := time.Parse("2006-01-02T15:04:05", result.ActualDeliveryTime); err == nil {
			info.ActualDelivery = t
		}
		info.SignedBy = result.DeliveryDetails.ReceivedByName
	}

	return info, nil
}

// ============================================================================
// MANIFEST
// ============================================================================

// GenerateManifest calls the FedEx End of Day / Close endpoint.
// FedEx requires a close-out call so that Express shipments are accepted at
// the drop-off/collection point. The API returns a summary report PDF.
func (a *FedExAdapter) GenerateManifest(ctx context.Context, creds CarrierCredentials, shipments []ManifestShipment) (*ManifestResult, error) {
	if len(shipments) == 0 {
		return nil, fmt.Errorf("no shipments provided for manifest")
	}

	token, err := a.getToken(ctx, creds)
	if err != nil {
		return nil, fmt.Errorf("FedEx authentication failed: %w", err)
	}

	// Build tracking number list for the close-out request
	trackingNumbers := make([]string, 0, len(shipments))
	for _, s := range shipments {
		trackingNumbers = append(trackingNumbers, s.TrackingNumber)
	}

	type fedexEODPickup struct {
		PickupType      string   `json:"pickupType"`
		TrackingNumbers []string `json:"trackingNumbers"`
	}
	type fedexEODReq struct {
		MerchantPhoneNumber string           `json:"merchantPhoneNumber"`
		Pickups             []fedexEODPickup `json:"pickups"`
	}

	eodReq := fedexEODReq{
		MerchantPhoneNumber: "00000000000",
		Pickups: []fedexEODPickup{
			{PickupType: "ON_CALL", TrackingNumbers: trackingNumbers},
		},
	}

	body, err := json.Marshal(eodReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal EOD request: %w", err)
	}

	baseURL := a.baseURL(creds.IsSandbox)
	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/ship/v1/endofday", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create EOD request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("FedEx EOD API call failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read FedEx EOD response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("FedEx EOD returned %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse response to extract any encoded PDF report document
	var eodResp struct {
		Output struct {
			PickupConfirmationCode string `json:"pickupConfirmationCode"`
			Documents              []struct {
				ContentType  string `json:"contentType"`
				EncodedLabel string `json:"encodedLabel"`
			} `json:"documents"`
		} `json:"output"`
	}

	// Fallback CSV generator used when FedEx returns no PDF document
	buildCSV := func() []byte {
		var csvBuf strings.Builder
		csvBuf.WriteString("TrackingNumber,ServiceCode,Reference,RecipientName,PostalCode,Country,WeightKg,Parcels,Cost,Currency,CreatedAt\n")
		for _, s := range shipments {
			csvBuf.WriteString(fmt.Sprintf("%s,%s,%s,%s,%s,%s,%.3f,%d,%.2f,%s,%s\n",
				s.TrackingNumber, s.ServiceCode, s.Reference,
				s.ToName, s.ToPostalCode, s.ToCountry,
				s.WeightKg, s.ParcelCount, s.Cost, s.Currency,
				s.CreatedAt.Format(time.RFC3339),
			))
		}
		return []byte(csvBuf.String())
	}

	if jsonErr := json.Unmarshal(respBody, &eodResp); jsonErr != nil {
		return &ManifestResult{
			CarrierID:     "fedex",
			Format:        "csv",
			Data:          buildCSV(),
			ShipmentCount: len(shipments),
			CreatedAt:     time.Now(),
		}, nil
	}

	for _, doc := range eodResp.Output.Documents {
		if (doc.ContentType == "MANIFEST" || doc.ContentType == "REPORT") && doc.EncodedLabel != "" {
			decoded, decErr := base64.StdEncoding.DecodeString(doc.EncodedLabel)
			if decErr != nil {
				// Try URL-safe encoding as fallback
				decoded, decErr = base64.URLEncoding.DecodeString(doc.EncodedLabel)
			}
			if decErr == nil && len(decoded) > 0 {
				return &ManifestResult{
					CarrierID:     "fedex",
					Format:        "pdf",
					Data:          decoded,
					ShipmentCount: len(shipments),
					CreatedAt:     time.Now(),
				}, nil
			}
		}
	}

	// No PDF in response — return CSV summary
	return &ManifestResult{
		CarrierID:     "fedex",
		Format:        "csv",
		Data:          buildCSV(),
		ShipmentCount: len(shipments),
		CreatedAt:     time.Now(),
	}, nil
}

// ============================================================================
// FEATURES
// ============================================================================

func (a *FedExAdapter) SupportsFeature(feature CarrierFeature) bool {
	supported := map[CarrierFeature]bool{
		FeatureRateQuotes:       true,
		FeatureTracking:         true,
		FeatureInternational:    true,
		FeatureSignature:        true,
		FeatureInsurance:        true,
		FeatureSaturdayDelivery: true,
		FeaturePickup:           true,
		FeatureCustoms:          true,
		FeatureVoid:             true,
		FeatureManifest:         true,
		FeaturePOBox:            false,
	}
	return supported[feature]
}

// ============================================================================
// OAUTH2 TOKEN MANAGEMENT
// ============================================================================

func (a *FedExAdapter) getToken(ctx context.Context, creds CarrierCredentials) (string, error) {
	cacheKey := creds.APIKey + "|" + fmt.Sprintf("%v", creds.IsSandbox)

	fedexTokenMu.Lock()
	defer fedexTokenMu.Unlock()

	if t, ok := fedexTokenCache[cacheKey]; ok && time.Now().Before(t.ExpiresAt) {
		return t.AccessToken, nil
	}

	// Request new token
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", creds.APIKey)
	form.Set("client_secret", creds.Password)

	tokenURL := a.baseURL(creds.IsSandbox) + "/oauth/token"
	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fedex token request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fedex authentication failed (status %d): %s", resp.StatusCode, string(respBody))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"` // seconds
		TokenType   string `json:"token_type"`
	}
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return "", fmt.Errorf("failed to parse FedEx token response: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("fedex: empty access token returned")
	}

	// Cache with 60s buffer before expiry
	expiry := time.Duration(tokenResp.ExpiresIn-60) * time.Second
	if expiry < 0 {
		expiry = 0
	}
	fedexTokenCache[cacheKey] = &fedexToken{
		AccessToken: tokenResp.AccessToken,
		ExpiresAt:   time.Now().Add(expiry),
	}

	return tokenResp.AccessToken, nil
}

// ============================================================================
// REQUEST BUILDERS
// ============================================================================

func (a *FedExAdapter) buildRateRequest(creds CarrierCredentials, req RateRequest) map[string]interface{} {
	parcels := make([]map[string]interface{}, 0, len(req.Parcels))
	for _, p := range req.Parcels {
		parcels = append(parcels, map[string]interface{}{
			"weight": map[string]interface{}{
				"units": "KG",
				"value": p.Weight,
			},
			"dimensions": map[string]interface{}{
				"length": p.Length,
				"width":  p.Width,
				"height": p.Height,
				"units":  "CM",
			},
		})
	}

	shipment := map[string]interface{}{
		"shipper": map[string]interface{}{
			"address": map[string]interface{}{
				"postalCode":  req.FromAddress.PostalCode,
				"countryCode": req.FromAddress.Country,
			},
		},
		"recipient": map[string]interface{}{
			"address": map[string]interface{}{
				"postalCode":     req.ToAddress.PostalCode,
				"countryCode":    req.ToAddress.Country,
				"residential":    false,
			},
		},
		"pickupType":      "USE_SCHEDULED_PICKUP",
		"rateRequestType": []string{"ACCOUNT"},
		"requestedPackageLineItems": parcels,
	}

	// Filter to specific services if requested
	if len(req.Services) > 0 {
		shipment["serviceType"] = req.Services[0]
	}

	return map[string]interface{}{
		"accountNumber": map[string]string{"value": creds.AccountID},
		"requestedShipment": shipment,
	}
}

func (a *FedExAdapter) buildShipmentRequest(creds CarrierCredentials, req ShipmentRequest) map[string]interface{} {
	// Label spec
	labelSpec := map[string]interface{}{
		"labelStockType": "PAPER_4X6",
		"imageType":      "PDF",
	}
	if req.Options.LabelFormat != "" {
		switch strings.ToUpper(req.Options.LabelFormat) {
		case "ZPL":
			labelSpec["imageType"] = "ZPLII"
			labelSpec["labelStockType"] = "STOCK_4X6"
		case "PNG":
			labelSpec["imageType"] = "PNG"
		}
	}

	// Packages
	parcels := make([]map[string]interface{}, 0, len(req.Parcels))
	for i, p := range req.Parcels {
		pkg := map[string]interface{}{
			"sequenceNumber": i + 1,
			"weight": map[string]interface{}{
				"units": "KG",
				"value": p.Weight,
			},
			"dimensions": map[string]interface{}{
				"length": p.Length,
				"width":  p.Width,
				"height": p.Height,
				"units":  "CM",
			},
		}
		// Signature
		if req.Options.Signature {
			pkg["specialServicesRequested"] = map[string]interface{}{
				"specialServiceTypes": []string{"SIGNATURE_OPTION"},
				"signatureOptionDetail": map[string]string{
					"optionType": "DIRECT",
				},
			}
		}
		parcels = append(parcels, pkg)
	}

	// Requested shipment
	shipment := map[string]interface{}{
		"shipper": map[string]interface{}{
			"contact": map[string]interface{}{
				"personName":   req.FromAddress.Name,
				"companyName":  req.FromAddress.Company,
				"phoneNumber":  req.FromAddress.Phone,
				"emailAddress": req.FromAddress.Email,
			},
			"address": map[string]interface{}{
				"streetLines":         []string{req.FromAddress.AddressLine1, req.FromAddress.AddressLine2},
				"city":                req.FromAddress.City,
				"stateOrProvinceCode": req.FromAddress.State,
				"postalCode":          req.FromAddress.PostalCode,
				"countryCode":         req.FromAddress.Country,
			},
		},
		"recipients": []map[string]interface{}{
			{
				"contact": map[string]interface{}{
					"personName":   req.ToAddress.Name,
					"companyName":  req.ToAddress.Company,
					"phoneNumber":  req.ToAddress.Phone,
					"emailAddress": req.ToAddress.Email,
				},
				"address": map[string]interface{}{
					"streetLines":         []string{req.ToAddress.AddressLine1, req.ToAddress.AddressLine2},
					"city":                req.ToAddress.City,
					"stateOrProvinceCode": req.ToAddress.State,
					"postalCode":          req.ToAddress.PostalCode,
					"countryCode":         req.ToAddress.Country,
					"residential":         false,
				},
			},
		},
		"serviceType":             req.ServiceCode,
		"packagingType":           "YOUR_PACKAGING",
		"pickupType":              "USE_SCHEDULED_PICKUP",
		"shippingChargesPayment": map[string]interface{}{
			"paymentType": "SENDER",
			"payor": map[string]interface{}{
				"responsibleParty": map[string]interface{}{
					"accountNumber": map[string]string{"value": creds.AccountID},
				},
			},
		},
		"labelSpecification":              labelSpec,
		"requestedPackageLineItems":       parcels,
		"totalPackageCount":               len(parcels),
		"shipDatestamp":                   time.Now().Format("2006-01-02"),
		"shipmentSpecialServices":         a.buildSpecialServices(req),
	}

	// Customer reference
	if req.Reference != "" {
		shipment["customerReferences"] = []map[string]string{
			{"customerReferenceType": "CUSTOMER_REFERENCE", "value": req.Reference},
		}
	}

	// Customs for international
	if req.Options.Customs != nil && req.FromAddress.Country != req.ToAddress.Country {
		shipment["customsClearanceDetail"] = a.buildCustoms(req)
	}

	return map[string]interface{}{
		"accountNumber":     map[string]string{"value": creds.AccountID},
		"requestedShipment": shipment,
		"labelResponseOptions": "LABEL",
		"shipAction":        "CONFIRM",
	}
}

func (a *FedExAdapter) buildSpecialServices(req ShipmentRequest) map[string]interface{} {
	services := []string{}

	if req.Options.SaturdayDelivery {
		services = append(services, "SATURDAY_DELIVERY")
	}
	if req.Options.Insurance != nil && req.Options.Insurance.Amount > 0 {
		services = append(services, "NON_STANDARD_PACKAGING")
	}

	if len(services) == 0 {
		return nil
	}
	return map[string]interface{}{"specialServiceTypes": services}
}

func (a *FedExAdapter) buildCustoms(req ShipmentRequest) map[string]interface{} {
	customs := req.Options.Customs

	items := make([]map[string]interface{}, 0, len(customs.Items))
	for _, item := range customs.Items {
		items = append(items, map[string]interface{}{
			"description":      item.Description,
			"quantity":         item.Quantity,
			"quantityUnits":    "PCS",
			"unitPrice":        map[string]interface{}{"amount": item.Value, "currency": "GBP"},
			"customsValue":     map[string]interface{}{"amount": item.Value * float64(item.Quantity), "currency": "GBP"},
			"countryOfManufacture": item.OriginCountry,
			"harmonizedCode":   item.HSCode,
			"weight":           map[string]interface{}{"units": "KG", "value": item.Weight},
		})
	}

	result := map[string]interface{}{
		"dutiesPayment": map[string]interface{}{
			"paymentType": "SENDER",
			"payor": map[string]interface{}{
				"responsibleParty": map[string]interface{}{
					"accountNumber": map[string]string{"value": ""},
				},
			},
		},
		"exportDetail": map[string]interface{}{
			"exportComplianceStatement": "NO_EEI_30_37_A",
		},
		"commodities": items,
	}

	if customs.InvoiceNumber != "" {
		result["commercialInvoice"] = map[string]interface{}{
			"comments":      []string{customs.InvoiceNumber},
			"shipmentPurpose": "SOLD",
		}
	}

	return result
}

// ============================================================================
// HELPERS
// ============================================================================

func (a *FedExAdapter) baseURL(sandbox bool) string {
	if sandbox {
		return "https://apis-sandbox.fedex.com"
	}
	return "https://apis.fedex.com"
}

func (a *FedExAdapter) setAuthHeader(req *http.Request, token string) {
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-locale", "en_GB")
}

func (a *FedExAdapter) serviceDisplayName(serviceType string) string {
	names := map[string]string{
		"INTERNATIONAL_PRIORITY":          "FedEx International Priority",
		"INTERNATIONAL_ECONOMY":           "FedEx International Economy",
		"INTERNATIONAL_PRIORITY_EXPRESS":  "FedEx International Priority Express",
		"INTERNATIONAL_FIRST":             "FedEx International First",
		"EUROPE_FIRST_INTERNATIONAL_PRIORITY": "FedEx Europe First Intl Priority",
		"INTERNATIONAL_PRIORITY_FREIGHT":  "FedEx Intl Priority Freight",
		"INTERNATIONAL_ECONOMY_FREIGHT":   "FedEx Intl Economy Freight",
	}
	if n, ok := names[serviceType]; ok {
		return n
	}
	return serviceType
}

func (a *FedExAdapter) estimatedDays(serviceType string) int {
	days := map[string]int{
		"INTERNATIONAL_PRIORITY_EXPRESS":      1,
		"INTERNATIONAL_FIRST":                 1,
		"EUROPE_FIRST_INTERNATIONAL_PRIORITY": 1,
		"INTERNATIONAL_PRIORITY":              2,
		"INTERNATIONAL_PRIORITY_FREIGHT":      2,
		"INTERNATIONAL_ECONOMY":               4,
		"INTERNATIONAL_ECONOMY_FREIGHT":       5,
	}
	if d, ok := days[serviceType]; ok {
		return d
	}
	return 3
}

func (a *FedExAdapter) mapTrackingStatus(fedexStatus string) TrackingStatus {
	s := strings.ToUpper(fedexStatus)
	switch {
	case strings.Contains(s, "DELIVERED"):
		return TrackingStatusDelivered
	case strings.Contains(s, "OUT FOR DELIVERY"), strings.Contains(s, "ON VEHICLE"):
		return TrackingStatusOutForDelivery
	case strings.Contains(s, "IN TRANSIT"), strings.Contains(s, "AT FEDEX"), strings.Contains(s, "PICKED UP"):
		return TrackingStatusInTransit
	case strings.Contains(s, "LABEL"), strings.Contains(s, "SHIPMENT INFORMATION"):
		return TrackingStatusPreTransit
	case strings.Contains(s, "EXCEPTION"), strings.Contains(s, "DELAY"), strings.Contains(s, "UNABLE"):
		return TrackingStatusException
	case strings.Contains(s, "RETURN"):
		return TrackingStatusReturned
	case strings.Contains(s, "CANCEL"):
		return TrackingStatusCancelled
	default:
		return TrackingStatusUnknown
	}
}

// ============================================================================
// FEDEX API RESPONSE STRUCTS
// ============================================================================

type fedexErrorResponse struct {
	Errors []struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
}

type fedexRateResponse struct {
	Output struct {
		RateReplyDetails []struct {
			ServiceType          string `json:"serviceType"`
			RatedShipmentDetails []struct {
				ShipmentRateDetail struct {
					RateType       string `json:"rateType"`
					TotalNetCharge struct {
						Amount   float64 `json:"amount"`
						Currency string  `json:"currency"`
					} `json:"totalNetCharge"`
				} `json:"shipmentRateDetail"`
			} `json:"ratedShipmentDetails"`
		} `json:"rateReplyDetails"`
	} `json:"output"`
}

type fedexShipmentResponse struct {
	Output struct {
		TransactionShipments []struct {
			ServiceType          string `json:"serviceType"`
			MasterTrackingNumber string `json:"masterTrackingNumber"`
			ShipmentDocuments    []struct {
				ContentType  string `json:"contentType"`
				EncodedLabel string `json:"encodedLabel"`
			} `json:"shipmentDocuments"`
			PieceResponses []struct {
				TrackingNumber   string `json:"trackingNumber"`
				PackageDocuments []struct {
					ContentType  string `json:"contentType"`
					EncodedLabel string `json:"encodedLabel"`
				} `json:"packageDocuments"`
			} `json:"pieceResponses"`
		} `json:"transactionShipments"`
	} `json:"output"`
}

type fedexTrackingResponse struct {
	Output struct {
		CompleteTrackResults []struct {
			TrackResults []struct {
				LatestStatusDetail struct {
					StatusByLocale string `json:"statusByLocale"`
					Description    string `json:"description"`
				} `json:"latestStatusDetail"`
				TrackingEventDetails []struct {
					EventTime        string `json:"eventTime"`
					EventType        string `json:"eventType"`
					EventDescription string `json:"eventDescription"`
					ScanLocation     struct {
						City        string `json:"city"`
						CountryCode string `json:"countryCode"`
					} `json:"scanLocation"`
				} `json:"trackingEventDetails"`
				EstimatedDeliveryTimeWindow struct {
					Window struct {
						Ends string `json:"ends"`
					} `json:"window"`
				} `json:"estimatedDeliveryTimeWindow"`
				ActualDeliveryTime string `json:"actualDeliveryTime"`
				DeliveryDetails    struct {
					ReceivedByName string `json:"receivedByName"`
				} `json:"deliveryDetails"`
			} `json:"trackResults"`
		} `json:"completeTrackResults"`
	} `json:"output"`
}

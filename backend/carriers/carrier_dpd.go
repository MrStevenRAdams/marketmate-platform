package carriers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ============================================================================
// DPD UK ADAPTER
// ============================================================================
// Implements the CarrierAdapter interface for DPD UK API
// API Docs: https://www.dpd.co.uk/content/products_services/uk_api.jsp
// ============================================================================

type DPDAdapter struct {
	httpClient *http.Client
}

func init() {
	// Auto-register DPD adapter
	Register(&DPDAdapter{
		httpClient: &http.Client{Timeout: 30 * time.Second},
	})
}

func (a *DPDAdapter) GetMetadata() CarrierMetadata {
	return CarrierMetadata{
		ID:          "dpd-uk",
		Name:        "DPD",
		DisplayName: "DPD UK",
		Country:     "GB",
		Logo:        "https://www.dpd.co.uk/logo.svg",
		Website:     "https://www.dpd.co.uk",
		SupportURL:  "https://www.dpd.co.uk/content/products_services/uk_api.jsp",
		Features: []string{
			string(FeatureTracking),
			string(FeatureSignature),
			string(FeatureSaturdayDelivery),
			string(FeaturePickup),
			string(FeatureInternational),
		},
		IsActive: true,
	}
}

func (a *DPDAdapter) ValidateCredentials(ctx context.Context, creds CarrierCredentials) error {
	// DPD uses username/password authentication
	url := a.getBaseURL(creds.IsSandbox) + "/user/?action=login"
	
	reqBody := map[string]string{
		"username": creds.Username,
		"password": creds.Password,
	}
	
	body, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	
	req.Header.Set("Content-Type", "application/json")
	
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("DPD connection failed: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != 200 {
		return fmt.Errorf("invalid DPD credentials")
	}
	
	return nil
}

func (a *DPDAdapter) GetServices(ctx context.Context, creds CarrierCredentials) ([]ShippingService, error) {
	return []ShippingService{
		{
			Code:          "DPD_NEXT_DAY",
			Name:          "DPD Next Day",
			Description:   "Next working day delivery by end of day",
			Domestic:      true,
			International: false,
			EstimatedDays: 1,
			MaxWeight:     30.0,
			Features:      []string{"tracking", "signature"},
		},
		{
			Code:          "DPD_12",
			Name:          "DPD 12:00",
			Description:   "Next working day delivery by 12pm",
			Domestic:      true,
			International: false,
			EstimatedDays: 1,
			MaxWeight:     30.0,
			Features:      []string{"tracking", "signature"},
		},
		{
			Code:          "DPD_10",
			Name:          "DPD 10:30",
			Description:   "Next working day delivery by 10:30am",
			Domestic:      true,
			International: false,
			EstimatedDays: 1,
			MaxWeight:     30.0,
			Features:      []string{"tracking", "signature"},
		},
		{
			Code:          "DPD_SAT",
			Name:          "DPD Saturday",
			Description:   "Saturday delivery",
			Domestic:      true,
			International: false,
			EstimatedDays: 1,
			MaxWeight:     30.0,
			Features:      []string{"tracking", "signature", "saturday_delivery"},
		},
		{
			Code:          "DPD_CLASSIC",
			Name:          "DPD Classic",
			Description:   "International delivery 2-5 days",
			Domestic:      false,
			International: true,
			EstimatedDays: 3,
			MaxWeight:     30.0,
			Features:      []string{"tracking", "customs"},
		},
	}, nil
}

func (a *DPDAdapter) GetRates(ctx context.Context, creds CarrierCredentials, req RateRequest) (*RateResponse, error) {
	// DPD rates based on service and weight
	totalWeight := 0.0
	for _, parcel := range req.Parcels {
		totalWeight += parcel.Weight
	}
	
	isInternational := req.ToAddress.Country != "GB"
	var rates []Rate
	
	if !isInternational {
		rates = append(rates,
			Rate{
				ServiceCode:   "DPD_NEXT_DAY",
				ServiceName:   "DPD Next Day",
				Cost:          Money{Amount: 6.50 + (totalWeight * 0.5), Currency: "GBP"},
				Currency:      "GBP",
				EstimatedDays: 1,
				Carrier:       "dpd-uk",
			},
			Rate{
				ServiceCode:   "DPD_12",
				ServiceName:   "DPD 12:00",
				Cost:          Money{Amount: 8.50 + (totalWeight * 0.5), Currency: "GBP"},
				Currency:      "GBP",
				EstimatedDays: 1,
				Carrier:       "dpd-uk",
			},
			Rate{
				ServiceCode:   "DPD_10",
				ServiceName:   "DPD 10:30",
				Cost:          Money{Amount: 12.50 + (totalWeight * 0.5), Currency: "GBP"},
				Currency:      "GBP",
				EstimatedDays: 1,
				Carrier:       "dpd-uk",
			},
		)
	} else {
		rates = append(rates, Rate{
			ServiceCode:   "DPD_CLASSIC",
			ServiceName:   "DPD Classic International",
			Cost:          Money{Amount: 15.00 + (totalWeight * 2.0), Currency: "GBP"},
			Currency:      "GBP",
			EstimatedDays: 3,
			Carrier:       "dpd-uk",
		})
	}
	
	return &RateResponse{Rates: rates}, nil
}

func (a *DPDAdapter) CreateShipment(ctx context.Context, creds CarrierCredentials, req ShipmentRequest) (*ShipmentResponse, error) {
	// First, authenticate to get session token
	token, err := a.authenticate(ctx, creds)
	if err != nil {
		return nil, err
	}
	
	url := a.getBaseURL(creds.IsSandbox) + "/shipping/shipment"
	
	// Build DPD shipment request
	dpdReq := map[string]interface{}{
		"accountNumber": creds.AccountID,
		"serviceCode":   req.ServiceCode,
		"collectionDetails": map[string]interface{}{
			"contactName": req.FromAddress.Name,
			"address": map[string]interface{}{
				"organisation": req.FromAddress.Company,
				"street":       req.FromAddress.AddressLine1,
				"locality":     req.FromAddress.City,
				"postcode":     req.FromAddress.PostalCode,
				"countryCode":  req.FromAddress.Country,
			},
		},
		"deliveryDetails": map[string]interface{}{
			"contactName": req.ToAddress.Name,
			"address": map[string]interface{}{
				"organisation": req.ToAddress.Company,
				"street":       req.ToAddress.AddressLine1,
				"locality":     req.ToAddress.City,
				"postcode":     req.ToAddress.PostalCode,
				"countryCode":  req.ToAddress.Country,
			},
			"notificationDetails": map[string]interface{}{
				"email": req.ToAddress.Email,
				"mobile": req.ToAddress.Phone,
			},
		},
		"parcels": []map[string]interface{}{},
		"networkCode": "2",
		"numberOfParcels": len(req.Parcels),
	}
	
	// Add parcels
	for _, parcel := range req.Parcels {
		dpdReq["parcels"] = append(dpdReq["parcels"].([]map[string]interface{}), map[string]interface{}{
			"weight": parcel.Weight,
		})
	}
	
	body, _ := json.Marshal(dpdReq)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")
	
	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("DPD shipment request failed: %w", err)
	}
	defer resp.Body.Close()
	
	respBody, _ := io.ReadAll(resp.Body)
	
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return nil, fmt.Errorf("DPD API error (status %d): %s", resp.StatusCode, string(respBody))
	}
	
	var dpdResp struct {
		Data struct {
			ShipmentID     string `json:"shipmentID"`
			ConsignmentNumber string `json:"consignmentNumber"`
			ParcelNumbers  []string `json:"parcelNumbers"`
			LabelData      string   `json:"labelData"` // Base64 PDF
		} `json:"data"`
	}
	
	if err := json.Unmarshal(respBody, &dpdResp); err != nil {
		return nil, fmt.Errorf("failed to parse DPD response: %w", err)
	}
	
	return &ShipmentResponse{
		TrackingNumber: dpdResp.Data.ParcelNumbers[0],
		LabelFormat:    "PDF",
		LabelData:      []byte(dpdResp.Data.LabelData),
		TrackingURL:    fmt.Sprintf("https://www.dpd.co.uk/tracking/trackingSearch.do?search.searchType=0&search.parcelNumber=%s", dpdResp.Data.ParcelNumbers[0]),
		Cost:           Money{Amount: 0, Currency: "GBP"}, // DPD doesn't return cost in response
		Currency:       "GBP",
		CarrierRef:     dpdResp.Data.ConsignmentNumber,
	}, nil
}

func (a *DPDAdapter) VoidShipment(ctx context.Context, creds CarrierCredentials, trackingNumber string) error {
	// DPD doesn't support voiding via API - must be done through portal
	return fmt.Errorf("DPD does not support label voiding via API")
}

func (a *DPDAdapter) GetTracking(ctx context.Context, creds CarrierCredentials, trackingNumber string) (*TrackingInfo, error) {
	url := fmt.Sprintf("%s/tracking/%s", a.getBaseURL(creds.IsSandbox), trackingNumber)
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	body, _ := io.ReadAll(resp.Body)
	
	var trackingData struct {
		TrackingNumber string `json:"parcelNumber"`
		Status         string `json:"status"`
		Events         []struct {
			Date        string `json:"date"`
			Time        string `json:"time"`
			Description string `json:"description"`
			Location    string `json:"location"`
		} `json:"events"`
	}
	
	if err := json.Unmarshal(body, &trackingData); err != nil {
		return nil, err
	}
	
	events := []TrackingEvent{}
	for _, e := range trackingData.Events {
		timestamp, _ := time.Parse("2006-01-02 15:04", e.Date+" "+e.Time)
		events = append(events, TrackingEvent{
			Timestamp:   timestamp,
			Description: e.Description,
			Location:    e.Location,
		})
	}
	
	return &TrackingInfo{
		TrackingNumber: trackingNumber,
		Status:         a.mapStatus(trackingData.Status),
		Events:         events,
	}, nil
}

func (a *DPDAdapter) SupportsFeature(feature CarrierFeature) bool {
	supported := map[CarrierFeature]bool{
		FeatureTracking:         true,
		FeatureSignature:        true,
		FeatureSaturdayDelivery: true,
		FeaturePickup:           true,
		FeatureInternational:    true,
		FeatureRateQuotes:       true,
		FeatureManifest:         true,
	}
	return supported[feature]
}

// Helper methods

func (a *DPDAdapter) getBaseURL(sandbox bool) string {
	if sandbox {
		return "https://api-sandbox.dpd.co.uk"
	}
	return "https://api.dpd.co.uk"
}

func (a *DPDAdapter) authenticate(ctx context.Context, creds CarrierCredentials) (string, error) {
	url := a.getBaseURL(creds.IsSandbox) + "/user/?action=login"
	
	reqBody := map[string]string{
		"username": creds.Username,
		"password": creds.Password,
	}
	
	body, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	
	req.Header.Set("Content-Type", "application/json")
	
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	
	var authResp struct {
		Data struct {
			GeoSession string `json:"geoSession"`
		} `json:"data"`
	}
	
	respBody, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(respBody, &authResp); err != nil {
		return "", err
	}
	
	return authResp.Data.GeoSession, nil
}

// GenerateManifest calls the DPD end-of-day close-out endpoint.
// DPD requires a collection manifest so the driver can collect parcels; this
// POSTs the collection request and returns a PDF manifest.
func (a *DPDAdapter) GenerateManifest(ctx context.Context, creds CarrierCredentials, shipments []ManifestShipment) (*ManifestResult, error) {
	if len(shipments) == 0 {
		return nil, fmt.Errorf("no shipments provided for manifest")
	}

	token, err := a.authenticate(ctx, creds)
	if err != nil {
		return nil, fmt.Errorf("DPD authentication failed: %w", err)
	}

	// DPD collection close-out: POST /shipping/network/close-out
	// Instructs DPD that the collection job is complete and requests the manifest PDF.
	type dpdCloseOut struct {
		CollectionDate string `json:"collectionDate"` // YYYY-MM-DD
		PrintFormat    string `json:"printFormat"`    // "PDF"
	}

	reqBody := dpdCloseOut{
		CollectionDate: time.Now().Format("2006-01-02"),
		PrintFormat:    "PDF",
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal close-out request: %w", err)
	}

	url := a.getBaseURL(creds.IsSandbox) + "/shipping/network/close-out"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create close-out request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("GeoSession", token)
	req.Header.Set("Accept", "application/pdf")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("DPD close-out API failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("DPD close-out returned %d: %s", resp.StatusCode, string(respBody))
	}

	pdfData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read DPD manifest PDF: %w", err)
	}

	return &ManifestResult{
		CarrierID:     "dpd-uk",
		Format:        "pdf",
		Data:          pdfData,
		ShipmentCount: len(shipments),
		CreatedAt:     time.Now(),
	}, nil
}

func (a *DPDAdapter) mapStatus(dpdStatus string) TrackingStatus {
	statusMap := map[string]TrackingStatus{
		"Information Received":  TrackingStatusPreTransit,
		"In Transit":           TrackingStatusInTransit,
		"Out for Delivery":     TrackingStatusOutForDelivery,
		"Delivered":            TrackingStatusDelivered,
		"Exception":            TrackingStatusException,
	}
	
	if status, ok := statusMap[dpdStatus]; ok {
		return status
	}
	return TrackingStatusUnknown
}

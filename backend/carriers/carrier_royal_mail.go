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
// ROYAL MAIL ADAPTER
// ============================================================================
// Implements the CarrierAdapter interface for Royal Mail Click & Drop API
// API Docs: https://developer.royalmail.net/api/click-and-drop
// ============================================================================

type RoyalMailAdapter struct {
	httpClient *http.Client
}

func init() {
	// Auto-register this adapter on package import
	Register(&RoyalMailAdapter{
		httpClient: &http.Client{Timeout: 30 * time.Second},
	})
}

// GetMetadata returns Royal Mail carrier information
func (a *RoyalMailAdapter) GetMetadata() CarrierMetadata {
	return CarrierMetadata{
		ID:          "royal-mail",
		Name:        "Royal Mail",
		DisplayName: "Royal Mail (UK)",
		Country:     "GB",
		Logo:        "https://www.royalmail.com/logo.svg",
		Website:     "https://www.royalmail.com",
		SupportURL:  "https://developer.royalmail.net",
		Features: []string{
			string(FeatureTracking),
			string(FeatureSignature),
			string(FeatureInternational),
			string(FeatureSaturdayDelivery),
			string(FeaturePOBox),
			string(FeatureCustoms),
		},
		IsActive: true,
	}
}

// ValidateCredentials checks if Royal Mail credentials are valid
func (a *RoyalMailAdapter) ValidateCredentials(ctx context.Context, creds CarrierCredentials) error {
	// Test with a simple API call
	url := a.getBaseURL(creds.IsSandbox) + "/v1/account"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+creds.APIKey)
	req.Header.Set("Accept", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to Royal Mail API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("invalid credentials (status %d)", resp.StatusCode)
	}

	return nil
}

// GetServices returns available Royal Mail shipping services
func (a *RoyalMailAdapter) GetServices(ctx context.Context, creds CarrierCredentials) ([]ShippingService, error) {
	// Royal Mail standard services
	services := []ShippingService{
		{
			Code:          "1ST",
			Name:          "1st Class",
			Description:   "Next working day delivery",
			Domestic:      true,
			International: false,
			EstimatedDays: 1,
			MaxWeight:     2.0, // kg
			Features:      []string{"tracking", "signature"},
		},
		{
			Code:          "2ND",
			Name:          "2nd Class",
			Description:   "2-3 working days delivery",
			Domestic:      true,
			International: false,
			EstimatedDays: 2,
			MaxWeight:     2.0,
			Features:      []string{"tracking"},
		},
		{
			Code:          "SD1",
			Name:          "Special Delivery Guaranteed by 1pm",
			Description:   "Next working day by 1pm",
			Domestic:      true,
			International: false,
			EstimatedDays: 1,
			MaxWeight:     20.0,
			Features:      []string{"tracking", "signature", "insurance"},
		},
		{
			Code:          "INTL_STD",
			Name:          "International Standard",
			Description:   "International delivery 3-7 days",
			Domestic:      false,
			International: true,
			EstimatedDays: 5,
			MaxWeight:     2.0,
			Features:      []string{"tracking", "customs"},
		},
		{
			Code:          "INTL_TRK",
			Name:          "International Tracked",
			Description:   "International delivery with tracking",
			Domestic:      false,
			International: true,
			EstimatedDays: 5,
			MaxWeight:     2.0,
			Features:      []string{"tracking", "signature", "customs"},
		},
	}

	return services, nil
}

// GetRates retrieves shipping rates from Royal Mail
func (a *RoyalMailAdapter) GetRates(ctx context.Context, creds CarrierCredentials, req RateRequest) (*RateResponse, error) {
	// Royal Mail Click & Drop uses fixed pricing based on service and weight
	// For accurate rates, you'd integrate with their pricing API
	// This is a simplified implementation

	isInternational := req.ToAddress.Country != "GB"
	totalWeight := 0.0
	for _, parcel := range req.Parcels {
		totalWeight += parcel.Weight
	}

	var rates []Rate

	if !isInternational {
		// Domestic rates (simplified)
		if totalWeight <= 2.0 {
			rates = append(rates, Rate{
				ServiceCode:   "1ST",
				ServiceName:   "1st Class",
				Cost:          Money{Amount: 3.50, Currency: "GBP"},
				Currency:      "GBP",
				EstimatedDays: 1,
				Carrier:       "royal-mail",
			})
			rates = append(rates, Rate{
				ServiceCode:   "2ND",
				ServiceName:   "2nd Class",
				Cost:          Money{Amount: 2.85, Currency: "GBP"},
				Currency:      "GBP",
				EstimatedDays: 2,
				Carrier:       "royal-mail",
			})
		}
		if totalWeight <= 20.0 {
			rates = append(rates, Rate{
				ServiceCode:   "SD1",
				ServiceName:   "Special Delivery by 1pm",
				Cost:          Money{Amount: 8.50 + (totalWeight * 1.5), Currency: "GBP"},
				Currency:      "GBP",
				EstimatedDays: 1,
				Carrier:       "royal-mail",
			})
		}
	} else {
		// International rates
		if totalWeight <= 2.0 {
			rates = append(rates, Rate{
				ServiceCode:   "INTL_STD",
				ServiceName:   "International Standard",
				Cost:          Money{Amount: 6.50, Currency: "GBP"},
				Currency:      "GBP",
				EstimatedDays: 5,
				Carrier:       "royal-mail",
			})
			rates = append(rates, Rate{
				ServiceCode:   "INTL_TRK",
				ServiceName:   "International Tracked",
				Cost:          Money{Amount: 9.50, Currency: "GBP"},
				Currency:      "GBP",
				EstimatedDays: 5,
				Carrier:       "royal-mail",
			})
		}
	}

	return &RateResponse{Rates: rates}, nil
}

// CreateShipment generates a Royal Mail shipping label
func (a *RoyalMailAdapter) CreateShipment(ctx context.Context, creds CarrierCredentials, req ShipmentRequest) (*ShipmentResponse, error) {
	url := a.getBaseURL(creds.IsSandbox) + "/v1/shipments"

	// Build Royal Mail API request
	rmReq := map[string]interface{}{
		"serviceCode": req.ServiceCode,
		"shipper": map[string]interface{}{
			"name":        req.FromAddress.Name,
			"addressLine1": req.FromAddress.AddressLine1,
			"addressLine2": req.FromAddress.AddressLine2,
			"city":        req.FromAddress.City,
			"postcode":    req.FromAddress.PostalCode,
			"country":     req.FromAddress.Country,
		},
		"recipient": map[string]interface{}{
			"name":        req.ToAddress.Name,
			"company":     req.ToAddress.Company,
			"addressLine1": req.ToAddress.AddressLine1,
			"addressLine2": req.ToAddress.AddressLine2,
			"city":        req.ToAddress.City,
			"postcode":    req.ToAddress.PostalCode,
			"country":     req.ToAddress.Country,
			"phone":       req.ToAddress.Phone,
			"email":       req.ToAddress.Email,
		},
		"parcels": []map[string]interface{}{},
		"reference": req.Reference,
	}

	// Add parcels
	for _, parcel := range req.Parcels {
		rmReq["parcels"] = append(rmReq["parcels"].([]map[string]interface{}), map[string]interface{}{
			"weight":      parcel.Weight,
			"length":      parcel.Length,
			"width":       parcel.Width,
			"height":      parcel.Height,
			"description": parcel.Description,
		})
	}

	// Add options
	if req.Options.Signature {
		rmReq["signatureRequired"] = true
	}
	if req.Options.SaturdayDelivery {
		rmReq["saturdayDelivery"] = true
	}

	// Add customs for international shipments
	if req.ToAddress.Country != "GB" && req.Options.Customs != nil {
		customsItems := []map[string]interface{}{}
		for _, item := range req.Options.Customs.Items {
			customsItems = append(customsItems, map[string]interface{}{
				"description":   item.Description,
				"quantity":      item.Quantity,
				"value":         item.Value,
				"weight":        item.Weight,
				"hsCode":        item.HSCode,
				"originCountry": item.OriginCountry,
			})
		}
		rmReq["customs"] = map[string]interface{}{
			"contentsType": req.Options.Customs.ContentsType,
			"items":        customsItems,
		}
	}

	// Make API request
	body, _ := json.Marshal(rmReq)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Authorization", "Bearer "+creds.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("Royal Mail API request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return nil, fmt.Errorf("Royal Mail API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	// Parse response
	var rmResp struct {
		TrackingNumber string  `json:"trackingNumber"`
		LabelURL       string  `json:"labelUrl"`
		Cost           float64 `json:"cost"`
		CarrierRef     string  `json:"carrierReference"`
	}

	if err := json.Unmarshal(respBody, &rmResp); err != nil {
		return nil, fmt.Errorf("failed to parse Royal Mail response: %w", err)
	}

	return &ShipmentResponse{
		TrackingNumber: rmResp.TrackingNumber,
		LabelURL:       rmResp.LabelURL,
		LabelFormat:    "PDF",
		TrackingURL:    fmt.Sprintf("https://www.royalmail.com/track-your-item#/tracking-results/%s", rmResp.TrackingNumber),
		Cost:           Money{Amount: rmResp.Cost, Currency: "GBP"},
		Currency:       "GBP",
		CarrierRef:     rmResp.CarrierRef,
	}, nil
}

// VoidShipment cancels a Royal Mail shipment
func (a *RoyalMailAdapter) VoidShipment(ctx context.Context, creds CarrierCredentials, trackingNumber string) error {
	url := fmt.Sprintf("%s/v1/shipments/%s", a.getBaseURL(creds.IsSandbox), trackingNumber)

	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+creds.APIKey)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to void Royal Mail shipment: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 204 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Royal Mail void failed (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// GetTracking retrieves tracking information
func (a *RoyalMailAdapter) GetTracking(ctx context.Context, creds CarrierCredentials, trackingNumber string) (*TrackingInfo, error) {
	url := fmt.Sprintf("%s/v1/tracking/%s", a.getBaseURL(creds.IsSandbox), trackingNumber)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+creds.APIKey)
	req.Header.Set("Accept", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Royal Mail tracking request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Royal Mail tracking error (status %d): %s", resp.StatusCode, string(body))
	}

	var rmTracking struct {
		TrackingNumber string `json:"trackingNumber"`
		Status         string `json:"status"`
		StatusDetail   string `json:"statusDetail"`
		Events         []struct {
			Timestamp   time.Time `json:"timestamp"`
			Status      string    `json:"status"`
			Description string    `json:"description"`
			Location    string    `json:"location"`
		} `json:"events"`
		EstimatedDelivery time.Time `json:"estimatedDelivery"`
		ActualDelivery    time.Time `json:"actualDelivery"`
		SignedBy          string    `json:"signedBy"`
	}

	if err := json.Unmarshal(body, &rmTracking); err != nil {
		return nil, fmt.Errorf("failed to parse Royal Mail tracking: %w", err)
	}

	// Convert to standard tracking format
	events := []TrackingEvent{}
	for _, e := range rmTracking.Events {
		events = append(events, TrackingEvent{
			Timestamp:   e.Timestamp,
			Status:      e.Status,
			Description: e.Description,
			Location:    e.Location,
		})
	}

	return &TrackingInfo{
		TrackingNumber:    rmTracking.TrackingNumber,
		Status:            a.mapStatus(rmTracking.Status),
		StatusDetail:      rmTracking.StatusDetail,
		Events:            events,
		EstimatedDelivery: rmTracking.EstimatedDelivery,
		ActualDelivery:    rmTracking.ActualDelivery,
		SignedBy:          rmTracking.SignedBy,
	}, nil
}

// GenerateManifest calls the Royal Mail Click & Drop manifest endpoint.
// Royal Mail requires a manifest call before the end-of-day collection; without it
// tracked items may not be accepted. The API returns a PDF manifest.
func (a *RoyalMailAdapter) GenerateManifest(ctx context.Context, creds CarrierCredentials, shipments []ManifestShipment) (*ManifestResult, error) {
	if len(shipments) == 0 {
		return nil, fmt.Errorf("no shipments provided for manifest")
	}

	// Royal Mail Click & Drop: POST /v1/manifests creates an end-of-day manifest
	// for all un-manifested orders associated with the account.
	// We pass the tracking numbers we want to include.
	type rmManifestOrder struct {
		OrderIdentifier string `json:"orderIdentifier"`
	}
	type rmManifestReq struct {
		Orders []rmManifestOrder `json:"orders"`
	}

	orders := make([]rmManifestOrder, 0, len(shipments))
	for _, s := range shipments {
		orders = append(orders, rmManifestOrder{OrderIdentifier: s.TrackingNumber})
	}

	body, err := json.Marshal(rmManifestReq{Orders: orders})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal manifest request: %w", err)
	}

	url := a.getBaseURL(creds.IsSandbox) + "/v1/manifests"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create manifest request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+creds.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/pdf")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("manifest API call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("manifest API returned %d: %s", resp.StatusCode, string(respBody))
	}

	pdfData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest PDF: %w", err)
	}

	return &ManifestResult{
		CarrierID:     "royal-mail",
		Format:        "pdf",
		Data:          pdfData,
		ShipmentCount: len(shipments),
		CreatedAt:     time.Now(),
	}, nil
}

// SupportsFeature checks if Royal Mail supports a feature
func (a *RoyalMailAdapter) SupportsFeature(feature CarrierFeature) bool {
	supported := map[CarrierFeature]bool{
		FeatureTracking:         true,
		FeatureSignature:        true,
		FeatureInternational:    true,
		FeatureSaturdayDelivery: true,
		FeaturePOBox:            true,
		FeatureCustoms:          true,
		FeatureVoid:             true,
		FeatureRateQuotes:       true,
		FeatureInsurance:        true,
		FeatureManifest:         true,
	}
	return supported[feature]
}

// Helper methods

func (a *RoyalMailAdapter) getBaseURL(sandbox bool) string {
	if sandbox {
		return "https://api-sandbox.royalmail.net"
	}
	return "https://api.royalmail.net"
}

func (a *RoyalMailAdapter) mapStatus(rmStatus string) TrackingStatus {
	statusMap := map[string]TrackingStatus{
		"pre_transit":       TrackingStatusPreTransit,
		"in_transit":        TrackingStatusInTransit,
		"out_for_delivery":  TrackingStatusOutForDelivery,
		"delivered":         TrackingStatusDelivered,
		"exception":         TrackingStatusException,
		"returned":          TrackingStatusReturned,
		"cancelled":         TrackingStatusCancelled,
	}

	if status, ok := statusMap[rmStatus]; ok {
		return status
	}
	return TrackingStatusUnknown
}

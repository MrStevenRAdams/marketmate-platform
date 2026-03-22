package carriers

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ============================================================================
// TEMPLATE CARRIER ADAPTER
// ============================================================================
// Copy this file and replace "Template" with your carrier name
// Fill in the methods with your carrier's API calls
// Register in init() and your carrier will automatically appear in the system
// ============================================================================

type TemplateCarrierAdapter struct {
	httpClient *http.Client
}

// init() is called when the package is imported
// This automatically registers your carrier in the system
func init() {
	// Uncomment this line after implementing your adapter
	// Register(&TemplateCarrierAdapter{
	// 	httpClient: &http.Client{Timeout: 30 * time.Second},
	// })
}

// GetMetadata returns information about your carrier for the UI
func (a *TemplateCarrierAdapter) GetMetadata() CarrierMetadata {
	return CarrierMetadata{
		ID:          "my-carrier", // Unique ID (lowercase, hyphen-separated)
		Name:        "MyCarrier",  // Short name
		DisplayName: "My Awesome Carrier Ltd", // Full display name
		Country:     "GB",         // Primary country code
		Logo:        "https://mycarrier.com/logo.svg", // Logo URL
		Website:     "https://mycarrier.com",
		SupportURL:  "https://developer.mycarrier.com", // API docs
		Features: []string{
			string(FeatureTracking),
			string(FeatureRateQuotes),
			string(FeatureSignature),
			// Add features your carrier supports
		},
		IsActive: true, // Set to true when ready
	}
}

// ValidateCredentials tests if the provided API credentials are valid
// This is called when a user adds your carrier in Settings
func (a *TemplateCarrierAdapter) ValidateCredentials(ctx context.Context, creds CarrierCredentials) error {
	// Example: Make a simple API call to test credentials
	// url := "https://api.mycarrier.com/v1/test"
	// req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	// req.Header.Set("Authorization", "Bearer " + creds.APIKey)
	// resp, err := a.httpClient.Do(req)
	// if err != nil {
	//     return fmt.Errorf("connection failed: %w", err)
	// }
	// if resp.StatusCode != 200 {
	//     return fmt.Errorf("invalid credentials")
	// }
	
	return fmt.Errorf("not implemented")
}

// GetServices returns the shipping services your carrier offers
func (a *TemplateCarrierAdapter) GetServices(ctx context.Context, creds CarrierCredentials) ([]ShippingService, error) {
	// Return your carrier's services
	// These will appear in the UI for users to select
	
	services := []ShippingService{
		{
			Code:          "EXPRESS",
			Name:          "Express Next Day",
			Description:   "Next working day delivery",
			Domestic:      true,
			International: false,
			EstimatedDays: 1,
			MaxWeight:     30.0, // kg
			Features:      []string{"tracking", "signature"},
		},
		{
			Code:          "STANDARD",
			Name:          "Standard 2-3 Day",
			Description:   "Economy delivery 2-3 working days",
			Domestic:      true,
			International: false,
			EstimatedDays: 2,
			MaxWeight:     30.0,
			Features:      []string{"tracking"},
		},
		// Add more services...
	}
	
	return services, nil
}

// GetRates retrieves shipping costs for the given shipment
// Called when users request rate quotes before creating a label
func (a *TemplateCarrierAdapter) GetRates(ctx context.Context, creds CarrierCredentials, req RateRequest) (*RateResponse, error) {
	// Calculate total weight
	totalWeight := 0.0
	for _, parcel := range req.Parcels {
		totalWeight += parcel.Weight
	}
	
	// Check if domestic or international
	isDomestic := req.ToAddress.Country == "GB" // or your carrier's country
	
	// Example: Call your carrier's rating API
	// url := "https://api.mycarrier.com/v1/rates"
	// Build request with from/to addresses and parcel details
	// Parse response and return rates
	
	var rates []Rate
	
	if isDomestic {
		rates = append(rates, Rate{
			ServiceCode:   "EXPRESS",
			ServiceName:   "Express Next Day",
			Cost:          Money{Amount: 8.50, Currency: "GBP"},
			Currency:      "GBP",
			EstimatedDays: 1,
			Carrier:       "my-carrier",
		})
		
		rates = append(rates, Rate{
			ServiceCode:   "STANDARD",
			ServiceName:   "Standard 2-3 Day",
			Cost:          Money{Amount: 5.50, Currency: "GBP"},
			Currency:      "GBP",
			EstimatedDays: 2,
			Carrier:       "my-carrier",
		})
	}
	
	return &RateResponse{Rates: rates}, nil
}

// CreateShipment generates a shipping label and returns tracking info
// This is the main method - called when a user ships an order
func (a *TemplateCarrierAdapter) CreateShipment(ctx context.Context, creds CarrierCredentials, req ShipmentRequest) (*ShipmentResponse, error) {
	// Step 1: Build API request to your carrier
	// Include:
	// - Service code (req.ServiceCode)
	// - From address (req.FromAddress)
	// - To address (req.ToAddress)
	// - Parcel details (req.Parcels)
	// - Options (req.Options - signature, insurance, etc.)
	// - Reference number (req.Reference)
	
	// Step 2: Make API call to create shipment
	// url := "https://api.mycarrier.com/v1/shipments"
	// Make POST request with shipment details
	
	// Step 3: Parse response
	// Extract:
	// - Tracking number
	// - Label URL or label data (PDF/PNG/ZPL)
	// - Cost
	// - Estimated delivery date
	
	// Step 4: Return standardized response
	return &ShipmentResponse{
		TrackingNumber: "TRACK123456789", // From carrier response
		LabelURL:       "https://api.mycarrier.com/labels/abc123.pdf", // Label download URL
		LabelFormat:    "PDF", // or PNG, ZPL
		LabelData:      nil,   // Or base64 encoded label bytes
		TrackingURL:    "https://mycarrier.com/track/TRACK123456789", // Public tracking page
		Cost:           Money{Amount: 8.50, Currency: "GBP"},
		Currency:       "GBP",
		CarrierRef:     "CARRIER_REF_123", // Carrier's internal reference
		EstimatedDelivery: time.Now().Add(24 * time.Hour),
	}, nil
}

// VoidShipment cancels a shipment and voids the label
// Called when a user cancels an order before shipping
func (a *TemplateCarrierAdapter) VoidShipment(ctx context.Context, creds CarrierCredentials, trackingNumber string) error {
	// Call your carrier's void/cancel API
	// url := fmt.Sprintf("https://api.mycarrier.com/v1/shipments/%s/void", trackingNumber)
	// Make DELETE or POST request
	
	// Some carriers don't support voiding - return error if not supported
	return fmt.Errorf("not implemented")
}

// GetTracking retrieves current tracking status
// Called when users check tracking or when updating order status
func (a *TemplateCarrierAdapter) GetTracking(ctx context.Context, creds CarrierCredentials, trackingNumber string) (*TrackingInfo, error) {
	// Call your carrier's tracking API
	// url := fmt.Sprintf("https://api.mycarrier.com/v1/tracking/%s", trackingNumber)
	
	// Parse response and map to standard tracking events
	
	return &TrackingInfo{
		TrackingNumber: trackingNumber,
		Status:         TrackingStatusInTransit, // Map carrier status to standard status
		StatusDetail:   "Package is in transit",
		Events: []TrackingEvent{
			{
				Timestamp:   time.Now().Add(-24 * time.Hour),
				Status:      "Picked Up",
				Description: "Package picked up from sender",
				Location:    "London, UK",
			},
			{
				Timestamp:   time.Now(),
				Status:      "In Transit",
				Description: "Package is on the way",
				Location:    "Birmingham, UK",
			},
		},
		EstimatedDelivery: time.Now().Add(24 * time.Hour),
	}, nil
}

// SupportsFeature reports which features your carrier supports
func (a *TemplateCarrierAdapter) SupportsFeature(feature CarrierFeature) bool {
	// Return true for features your carrier supports
	supported := map[CarrierFeature]bool{
		FeatureTracking:    true,  // Has tracking
		FeatureRateQuotes:  true,  // Can provide rate quotes
		FeatureSignature:   true,  // Signature on delivery
		FeatureVoid:        false, // Cannot void labels (example)
		FeatureInsurance:   true,  // Can add insurance
		FeatureInternational: false, // Only domestic (example)
		FeatureManifest:    false, // Override to true if your carrier requires a manifest call
	}

	return supported[feature]
}

// GenerateManifest produces an end-of-day manifest. If your carrier does not
// support server-side manifests, return a client-generated CSV summary so the
// operator always has a collection document.
func (a *TemplateCarrierAdapter) GenerateManifest(ctx context.Context, creds CarrierCredentials, shipments []ManifestShipment) (*ManifestResult, error) {
	if len(shipments) == 0 {
		return nil, fmt.Errorf("no shipments provided for manifest")
	}

	// TODO: call your carrier's manifest / close-out endpoint if one exists.
	// If not, generate a CSV summary (like the pattern below) and return it.

	var buf strings.Builder
	buf.WriteString("TrackingNumber,ServiceCode,Reference,RecipientName,PostalCode,Country,WeightKg,Parcels,Cost,Currency,CreatedAt\n")
	for _, s := range shipments {
		buf.WriteString(fmt.Sprintf("%s,%s,%s,%s,%s,%s,%.3f,%d,%.2f,%s,%s\n",
			s.TrackingNumber, s.ServiceCode, s.Reference,
			s.ToName, s.ToPostalCode, s.ToCountry,
			s.WeightKg, s.ParcelCount, s.Cost, s.Currency,
			s.CreatedAt.Format(time.RFC3339),
		))
	}

	return &ManifestResult{
		CarrierID:     "template-carrier", // replace with your carrier ID
		Format:        "csv",
		Data:          []byte(buf.String()),
		ShipmentCount: len(shipments),
		CreatedAt:     time.Now(),
	}, nil
}

// ============================================================================
// HELPER METHODS (optional)
// ============================================================================

func (a *TemplateCarrierAdapter) getBaseURL(sandbox bool) string {
	if sandbox {
		return "https://api-sandbox.mycarrier.com"
	}
	return "https://api.mycarrier.com"
}

// Map your carrier's status codes to standard TrackingStatus
func (a *TemplateCarrierAdapter) mapStatus(carrierStatus string) TrackingStatus {
	statusMap := map[string]TrackingStatus{
		"LABEL_CREATED":     TrackingStatusPreTransit,
		"PICKED_UP":         TrackingStatusInTransit,
		"IN_TRANSIT":        TrackingStatusInTransit,
		"OUT_FOR_DELIVERY":  TrackingStatusOutForDelivery,
		"DELIVERED":         TrackingStatusDelivered,
		"EXCEPTION":         TrackingStatusException,
		"RETURNED":          TrackingStatusReturned,
	}
	
	if status, ok := statusMap[carrierStatus]; ok {
		return status
	}
	return TrackingStatusUnknown
}

// ============================================================================
// USAGE INSTRUCTIONS FOR DEVELOPERS
// ============================================================================

/*
To add your carrier to Marketmate:

1. Copy this file to: backend/carriers/carrier_yourname.go

2. Replace "Template" with your carrier name throughout the file

3. Fill in GetMetadata() with your carrier's details

4. Implement the API methods:
   - ValidateCredentials: Test API credentials
   - GetServices: Return your shipping services
   - GetRates: Call your rating API
   - CreateShipment: Generate labels (MOST IMPORTANT)
   - GetTracking: Fetch tracking updates
   - VoidShipment: Cancel shipments (if supported)

5. Uncomment the Register() call in init()

6. Import your package in main.go:
   import _ "module-a/carriers" // This will auto-register all carriers

7. Your carrier will now appear in:
   - Settings > Carriers
   - Dispatch > Carrier Selection
   - Rate shopping

API DESIGN TIPS:
- Use context for cancellation support
- Return clear error messages
- Handle rate limits gracefully
- Log API requests for debugging
- Support both sandbox and production modes
- Parse all carrier-specific errors

TESTING:
- Test with sandbox/test credentials first
- Verify label downloads work
- Test tracking updates
- Test international shipments (if supported)
- Test edge cases (invalid addresses, oversized parcels, etc.)

Need help? Check existing adapters (Royal Mail, DPD) for examples.
*/

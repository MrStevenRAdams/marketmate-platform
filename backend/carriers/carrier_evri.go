package carriers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"time"
)

// ============================================================================
// EVRI CARRIER ADAPTER
// ============================================================================
// Implements CarrierAdapter for Evri (formerly Hermes UK).
//
// API: Evri Routing Web Service v4 (XML/REST)
//   Spec: Evri Client Integration – API – Domestic and International Routing
//         and Labels v1.6.14
//
// Authentication: HTTP Basic auth (username:password)
//
// Credentials stored in CarrierCredentials:
//   AccountID              → Evri clientId  (e.g. "9866") — mandatory
//   Username               → Evri username  (e.g. "247CommerceLtd-sit")
//   Password               → Evri password
//   Extra["client_name"]   → Evri clientName (e.g. "247 Commerce Ltd") — mandatory
//   IsSandbox              → true = SIT endpoint, false = production
//
// SIT REST endpoint:  https://sit.hermes-europe.co.uk/routing/service/rest/v4
// Prod REST endpoint: https://www.hermes-europe.co.uk/routing/service/rest/v4
//
// Egress proxy:
//   If EGRESS_PROXY_URL is set, all Evri API calls are routed through it
//   (same pattern as the Temu client / carrier_evri proxy support).
// ============================================================================

const (
	evriSITBase  = "https://sit.hermes-europe.co.uk/routing/service/rest/v4"
	evriProdBase = "https://www.hermes-europe.co.uk/routing/service/rest/v4"
)

// Evri service codes — passed as ServiceCode in ShipmentRequest.
const (
	EvriServiceParcel        = "PARCEL"
	EvriServiceNextDay       = "PARCEL_NEXT_DAY"
	EvriServiceLargeParcel   = "LARGE_PARCEL"
	EvriServiceSignature     = "PARCEL_SIGNATURE"
	EvriServiceInternational = "INT_PARCEL"
)

// ============================================================================
// ADAPTER STRUCT
// ============================================================================

type EvriAdapter struct {
	httpClient *http.Client
}

var evriEgressProxyURL    = os.Getenv("EGRESS_PROXY_URL")
var evriEgressProxySecret = os.Getenv("EGRESS_PROXY_SECRET")

func init() {
	Register(&EvriAdapter{
		httpClient: &http.Client{Timeout: 30 * time.Second},
	})
}

// ============================================================================
// METADATA
// ============================================================================

func (a *EvriAdapter) GetMetadata() CarrierMetadata {
	return CarrierMetadata{
		ID:          "evri",
		Name:        "Evri",
		DisplayName: "Evri (formerly Hermes)",
		Country:     "GB",
		Logo:        "https://www.evri.com/assets/images/evri-logo.svg",
		Website:     "https://www.evri.com",
		SupportURL:  "https://www.evri.com/business",
		Features: []string{
			string(FeatureTracking),
			string(FeatureInternational),
			string(FeatureManifest),
			string(FeatureRateQuotes),
			string(FeatureSignature),
		},
		IsActive: true,
	}
}

// ============================================================================
// CREDENTIALS VALIDATION
// ============================================================================
// Sends a determineDeliveryRouting request with a dummy address to verify
// credentials. Any HTTP 200 (even with routing errors) means auth was accepted.

func (a *EvriAdapter) ValidateCredentials(ctx context.Context, creds CarrierCredentials) error {
	if creds.AccountID == "" {
		return fmt.Errorf("evri: clientId (account_id) is required")
	}
	if a.clientName(creds) == "" {
		return fmt.Errorf("evri: clientName (extra.client_name) is required")
	}

	entry := evriRoutingEntry{
		LastName:        "Test",
		StreetName:      "1 Test Street",
		City:            "London",
		PostCode:        "EC1A 1BB",
		CountryCode:     "GB",
		WeightGrams:     500,
		LengthCM:        20,
		WidthCM:         15,
		DepthCM:         10,
		ValuePence:      100,
		Reference1:      "CREDTEST",
		DespatchDate:    time.Now().Format("2006-01-02"),
		CountryOfOrigin: "GB",
		Currency:        "GBP",
		Description:     "Test",
	}

	payload := a.buildXMLPayload(creds, entry, "")

	resp, err := a.postXML(ctx, creds, "determineDeliveryRouting", payload)
	if err != nil {
		return fmt.Errorf("evri connection failed: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return fmt.Errorf("evri: invalid credentials (401)")
	case http.StatusForbidden:
		return fmt.Errorf("evri: account not authorised (403)")
	}
	return nil
}

// ============================================================================
// SERVICES
// ============================================================================

func (a *EvriAdapter) GetServices(ctx context.Context, creds CarrierCredentials) ([]ShippingService, error) {
	return []ShippingService{
		{
			Code:          EvriServiceParcel,
			Name:          "Evri Parcel",
			Description:   "Standard parcel delivery (2–3 working days)",
			Domestic:      true,
			International: false,
			EstimatedDays: 3,
			MaxWeight:     15.0,
			Features:      []string{"tracking"},
		},
		{
			Code:          EvriServiceNextDay,
			Name:          "Evri Parcel – Next Day",
			Description:   "Next working day delivery",
			Domestic:      true,
			International: false,
			EstimatedDays: 1,
			MaxWeight:     15.0,
			Features:      []string{"tracking", "next_day"},
		},
		{
			Code:          EvriServiceLargeParcel,
			Name:          "Evri Large Parcel",
			Description:   "Large item parcel delivery (2–4 working days)",
			Domestic:      true,
			International: false,
			EstimatedDays: 4,
			MaxWeight:     15.0,
			Features:      []string{"tracking"},
		},
		{
			Code:          EvriServiceSignature,
			Name:          "Evri Parcel – Signature",
			Description:   "Standard parcel with signature on delivery",
			Domestic:      true,
			International: false,
			EstimatedDays: 3,
			MaxWeight:     15.0,
			Features:      []string{"tracking", "signature"},
		},
		{
			Code:          EvriServiceInternational,
			Name:          "Evri International Parcel",
			Description:   "International delivery to 190+ countries via GECO",
			Domestic:      false,
			International: true,
			EstimatedDays: 10,
			MaxWeight:     2.0,
			Features:      []string{"tracking", "customs"},
		},
	}, nil
}

// ============================================================================
// RATES
// ============================================================================
// Evri does not provide a pricing API for standard routing accounts — rates are
// contractual. We return all applicable services with a zero cost placeholder.

func (a *EvriAdapter) GetRates(ctx context.Context, creds CarrierCredentials, req RateRequest) (*RateResponse, error) {
	services, _ := a.GetServices(ctx, creds)
	isIntl := req.ToAddress.Country != "" && req.ToAddress.Country != "GB"

	var rates []Rate
	for _, svc := range services {
		if isIntl && !svc.International {
			continue
		}
		if !isIntl && !svc.Domestic {
			continue
		}
		rates = append(rates, Rate{
			ServiceCode:   svc.Code,
			ServiceName:   svc.Name,
			Cost:          Money{Amount: 0, Currency: "GBP"},
			Currency:      "GBP",
			EstimatedDays: svc.EstimatedDays,
			Carrier:       "evri",
		})
	}
	return &RateResponse{Rates: rates}, nil
}

// ============================================================================
// CREATE SHIPMENT
// ============================================================================
// Calls routeDeliveryCreatePreadviceAndLabel, returning a tracking barcode
// and base64-encoded PDF label.

func (a *EvriAdapter) CreateShipment(ctx context.Context, creds CarrierCredentials, req ShipmentRequest) (*ShipmentResponse, error) {
	entry, err := a.mapShipmentRequest(req)
	if err != nil {
		return nil, fmt.Errorf("evri: %w", err)
	}

	labelFormat := ""
	if strings.ToUpper(req.Options.LabelFormat) == "ZPL" {
		labelFormat = "ZPL_799_1199"
	}

	payload := a.buildXMLPayload(creds, entry, labelFormat)

	resp, err := a.postXML(ctx, creds, "routeDeliveryCreatePreadviceAndLabel", payload)
	if err != nil {
		return nil, fmt.Errorf("evri shipment request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("evri: read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("evri shipment error (HTTP %d): %.300s", resp.StatusCode, string(body))
	}

	return a.parseShipmentResponse(body, req)
}

// ============================================================================
// VOID SHIPMENT
// ============================================================================
// Not available via the Evri routing API.

func (a *EvriAdapter) VoidShipment(ctx context.Context, creds CarrierCredentials, trackingNumber string) error {
	return fmt.Errorf("evri: label voiding is not available via the routing API — contact your Evri account manager to cancel parcel %s", trackingNumber)
}

// ============================================================================
// TRACKING
// ============================================================================
// A separate Evri Tracking API contract is required for event-level data.
// We return the public tracking URL so operators can click through.

func (a *EvriAdapter) GetTracking(ctx context.Context, creds CarrierCredentials, trackingNumber string) (*TrackingInfo, error) {
	return &TrackingInfo{
		TrackingNumber: trackingNumber,
		Status:         TrackingStatusPreTransit,
		StatusDetail:   "Tracking available at evri.com",
		Location:       "https://www.evri.com/track/" + trackingNumber,
	}, nil
}

// ============================================================================
// MANIFEST
// ============================================================================

func (a *EvriAdapter) GenerateManifest(ctx context.Context, creds CarrierCredentials, shipments []ManifestShipment) (*ManifestResult, error) {
	if len(shipments) == 0 {
		return nil, fmt.Errorf("no shipments provided for manifest")
	}

	var buf strings.Builder
	buf.WriteString("TrackingNumber,ServiceCode,Reference,RecipientName,PostalCode,Country,WeightKg,Parcels,CreatedAt\n")
	for _, s := range shipments {
		buf.WriteString(fmt.Sprintf("%s,%s,%s,%s,%s,%s,%.3f,%d,%s\n",
			s.TrackingNumber,
			s.ServiceCode,
			s.Reference,
			s.ToName,
			s.ToPostalCode,
			s.ToCountry,
			s.WeightKg,
			s.ParcelCount,
			s.CreatedAt.Format(time.RFC3339),
		))
	}

	return &ManifestResult{
		CarrierID:     "evri",
		Format:        "csv",
		Data:          []byte(buf.String()),
		ShipmentCount: len(shipments),
		CreatedAt:     time.Now(),
	}, nil
}

// ============================================================================
// FEATURES
// ============================================================================

func (a *EvriAdapter) SupportsFeature(feature CarrierFeature) bool {
	supported := map[CarrierFeature]bool{
		FeatureTracking:         true,
		FeatureVoid:             false,
		FeatureInternational:    true,
		FeatureManifest:         true,
		FeatureRateQuotes:       true,
		FeatureSignature:        true,
		FeatureInsurance:        false,
		FeatureCustoms:          true,
		FeaturePOBox:            false,
		FeatureSaturdayDelivery: false,
		FeaturePickup:           false,
	}
	return supported[feature]
}

// ============================================================================
// XML STRUCTURES — REQUEST
// ============================================================================

// evriRoutingEntry holds the per-shipment data used to build the XML payload.
type evriRoutingEntry struct {
	// Recipient
	FirstName    string
	LastName     string // mandatory per spec
	HouseNo      string
	HouseName    string
	StreetName   string // mandatory
	AddressLine1 string
	City         string // mandatory
	Region       string
	PostCode     string // mandatory for GB
	CountryCode  string // mandatory, ISO 2-letter uppercase
	Phone        string
	Email        string
	Reference1   string // customerReference1 — mandatory
	Reference2   string

	// Parcel (all mandatory per spec)
	WeightGrams   int
	LengthCM      int
	WidthCM       int
	DepthCM       int
	ValuePence    int
	NumberOfItems int
	Description   string
	Currency      string

	// Service flags
	NextDay   bool
	Signature bool

	// Sender address
	SenderName  string
	SenderLine1 string
	SenderLine2 string
	SenderLine3 string

	// Required dates/fields
	DespatchDate    string // YYYY-MM-DD
	CountryOfOrigin string // ISO 2-letter

	// International
	ExportReason string
	EORINumber   string
	VATNumber    string
	IOSSNumber   string
}

type xmlDeliveryRoutingRequest struct {
	XMLName                       xml.Name                          `xml:"deliveryRoutingRequest"`
	ClientID                      string                            `xml:"clientId"`
	ClientName                    string                            `xml:"clientName"`
	SourceOfRequest               string                            `xml:"sourceOfRequest"`
	CreationDate                  string                            `xml:"creationDate"`
	LabelFormat                   string                            `xml:"labelFormat,omitempty"`
	EORINumber                    string                            `xml:"eoriNumber,omitempty"`
	VATNumber                     string                            `xml:"vatNumber,omitempty"`
	IOSSNumber                    string                            `xml:"iossNumber,omitempty"`
	DeliveryRoutingRequestEntries xmlDeliveryRoutingRequestEntries  `xml:"deliveryRoutingRequestEntries"`
}

type xmlDeliveryRoutingRequestEntries struct {
	Entries []xmlDeliveryRoutingRequestEntry `xml:"deliveryRoutingRequestEntry"`
}

type xmlDeliveryRoutingRequestEntry struct {
	AddressValidationRequired bool               `xml:"addressValidationRequired"`
	Customer                  xmlCustomer        `xml:"customer"`
	Parcel                    xmlParcel          `xml:"parcel"`
	Services                  *xmlServices       `xml:"services,omitempty"`
	SenderAddress             *xmlSenderAddress  `xml:"senderAddress,omitempty"`
	ExpectedDespatchDate      string             `xml:"expectedDespatchDate"`
	CountryOfOrigin           string             `xml:"countryOfOrigin"`
}

type xmlCustomer struct {
	Address            xmlAddress `xml:"address"`
	MobilePhoneNo      string     `xml:"mobilePhoneNo,omitempty"`
	Email              string     `xml:"email,omitempty"`
	CustomerReference1 string     `xml:"customerReference1"`
	CustomerReference2 string     `xml:"customerReference2,omitempty"`
}

type xmlAddress struct {
	FirstName    string `xml:"firstName,omitempty"`
	LastName     string `xml:"lastName"`
	HouseNo      string `xml:"houseNo,omitempty"`
	HouseName    string `xml:"houseName,omitempty"`
	StreetName   string `xml:"streetName"`
	AddressLine1 string `xml:"addressLine1,omitempty"`
	City         string `xml:"city"`
	Region       string `xml:"region,omitempty"`
	PostCode     string `xml:"postCode"`
	CountryCode  string `xml:"countryCode"`
}

type xmlParcel struct {
	Weight            int    `xml:"weight"`
	Length            int    `xml:"length"`
	Width             int    `xml:"width"`
	Depth             int    `xml:"depth"`
	Girth             int    `xml:"girth"`
	CombinedDimension int    `xml:"combinedDimension"`
	Volume            int    `xml:"volume"`
	Currency          string `xml:"currency,omitempty"`
	Value             int    `xml:"value"`
	NumberOfItems     int    `xml:"numberOfItems,omitempty"`
	Description       string `xml:"description,omitempty"`
	OriginOfParcel    string `xml:"originOfParcel,omitempty"`
	ExportReason      string `xml:"exportReason,omitempty"`
}

type xmlServices struct {
	NextDay   string `xml:"nextDay,omitempty"`   // "true"
	Signature string `xml:"signature,omitempty"` // "true"
}

type xmlSenderAddress struct {
	AddressLine1 string `xml:"addressLine1,omitempty"`
	AddressLine2 string `xml:"addressLine2,omitempty"`
	AddressLine3 string `xml:"addressLine3,omitempty"`
	AddressLine4 string `xml:"addressLine4,omitempty"`
}

// buildXMLPayload constructs the full XML request body.
func (a *EvriAdapter) buildXMLPayload(creds CarrierCredentials, e evriRoutingEntry, labelFormat string) []byte {
	girth := (2 * e.WidthCM) + (2 * e.DepthCM)
	combined := e.LengthCM + girth
	volume := e.LengthCM * e.WidthCM * e.DepthCM

	currency := e.Currency
	if currency == "" {
		currency = "GBP"
	}

	var services *xmlServices
	if e.NextDay || e.Signature {
		services = &xmlServices{}
		if e.NextDay {
			services.NextDay = "true"
		}
		if e.Signature {
			services.Signature = "true"
		}
	}

	var sender *xmlSenderAddress
	if e.SenderName != "" || e.SenderLine1 != "" {
		sender = &xmlSenderAddress{
			AddressLine1: e.SenderName,
			AddressLine2: e.SenderLine1,
			AddressLine3: e.SenderLine2,
			AddressLine4: e.SenderLine3,
		}
	}

	root := xmlDeliveryRoutingRequest{
		ClientID:        creds.AccountID,
		ClientName:      a.clientName(creds),
		SourceOfRequest: "CLIENTWS",
		CreationDate:    time.Now().UTC().Format("2006-01-02T15:04:05"),
		LabelFormat:     labelFormat,
		EORINumber:      e.EORINumber,
		VATNumber:       e.VATNumber,
		IOSSNumber:      e.IOSSNumber,
		DeliveryRoutingRequestEntries: xmlDeliveryRoutingRequestEntries{
			Entries: []xmlDeliveryRoutingRequestEntry{
				{
					AddressValidationRequired: false,
					Customer: xmlCustomer{
						Address: xmlAddress{
							FirstName:    e.FirstName,
							LastName:     e.LastName,
							HouseNo:      e.HouseNo,
							HouseName:    e.HouseName,
							StreetName:   e.StreetName,
							AddressLine1: e.AddressLine1,
							City:         e.City,
							Region:       e.Region,
							PostCode:     strings.ToUpper(e.PostCode),
							CountryCode:  strings.ToUpper(e.CountryCode),
						},
						MobilePhoneNo:      e.Phone,
						Email:              e.Email,
						CustomerReference1: e.Reference1,
						CustomerReference2: e.Reference2,
					},
					Parcel: xmlParcel{
						Weight:            e.WeightGrams,
						Length:            e.LengthCM,
						Width:             e.WidthCM,
						Depth:             e.DepthCM,
						Girth:             girth,
						CombinedDimension: combined,
						Volume:            volume,
						Currency:          currency,
						Value:             e.ValuePence,
						NumberOfItems:     e.NumberOfItems,
						Description:       e.Description,
						OriginOfParcel:    strings.ToUpper(e.CountryOfOrigin),
						ExportReason:      e.ExportReason,
					},
					Services:             services,
					SenderAddress:        sender,
					ExpectedDespatchDate: e.DespatchDate,
					CountryOfOrigin:      strings.ToUpper(e.CountryOfOrigin),
				},
			},
		},
	}

	xmlBytes, _ := xml.MarshalIndent(root, "", "  ")
	return append([]byte(xml.Header), xmlBytes...)
}

// ============================================================================
// XML STRUCTURES — RESPONSE
// ============================================================================

// xmlRoutingWrapper handles the method-response wrapper element.
// REST responses look like:
//   <routeDeliveryCreatePreadviceAndLabelResponse>
//     <routingResponse>...</routingResponse>
//   </routeDeliveryCreatePreadviceAndLabelResponse>
type xmlRoutingWrapper struct {
	XMLName  xml.Name           `xml:""`
	Response xmlRoutingResponse `xml:"routingResponse"`
}

type xmlRoutingResponse struct {
	XMLName                xml.Name               `xml:"routingResponse"`
	RoutingResponseEntries xmlRoutingRespEntries  `xml:"routingResponseEntries"`
}

type xmlRoutingRespEntries struct {
	Entries []xmlRoutingRespEntry `xml:"routingResponseEntry"`
}

type xmlRoutingRespEntry struct {
	OutboundCarriers xmlCarriers    `xml:"outboundCarriers"`
	ErrorMessages    []xmlMessage   `xml:"errorMessages>message"`
	WarningMessages  []xmlMessage   `xml:"warningMessages>message"`
}

type xmlCarriers struct {
	Carrier1   xmlCarrier `xml:"carrier1"`
	LabelImage string     `xml:"labelImage"` // base64 PDF or ZPL
}

type xmlCarrier struct {
	CarrierID          string     `xml:"carrierId"`
	DeliveryMethodCode string     `xml:"deliveryMethodCode"`
	Barcode1           xmlBarcode `xml:"barcode1"`
}

type xmlBarcode struct {
	BarcodeNumber  string `xml:"barcodeNumber"`
	BarcodeDisplay string `xml:"barcodeDisplay"`
}

type xmlMessage struct {
	ErrorCode        string `xml:"errorCode"`
	ErrorDescription string `xml:"errorDescription"`
}

func (a *EvriAdapter) parseShipmentResponse(body []byte, req ShipmentRequest) (*ShipmentResponse, error) {
	// Try wrapped form first
	var wrapper xmlRoutingWrapper
	_ = xml.Unmarshal(body, &wrapper)

	// If wrapper has no entries, try direct routingResponse
	entries := wrapper.Response.RoutingResponseEntries.Entries
	if len(entries) == 0 {
		var direct xmlRoutingResponse
		if err := xml.Unmarshal(body, &direct); err != nil {
			return nil, fmt.Errorf("evri: failed to parse response XML: %w (body: %.300s)", err, string(body))
		}
		entries = direct.RoutingResponseEntries.Entries
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("evri: no response entries in XML (body: %.300s)", string(body))
	}

	entry := entries[0]

	// Surface any API errors
	if len(entry.ErrorMessages) > 0 {
		msgs := make([]string, 0, len(entry.ErrorMessages))
		for _, m := range entry.ErrorMessages {
			msgs = append(msgs, fmt.Sprintf("[%s] %s", m.ErrorCode, m.ErrorDescription))
		}
		return nil, fmt.Errorf("evri routing error: %s", strings.Join(msgs, "; "))
	}

	trackingNumber := entry.OutboundCarriers.Carrier1.Barcode1.BarcodeNumber
	if trackingNumber == "" {
		return nil, fmt.Errorf("evri: no tracking barcode in response (body: %.300s)", string(body))
	}

	result := &ShipmentResponse{
		TrackingNumber:    trackingNumber,
		TrackingURL:       "https://www.evri.com/track/" + trackingNumber,
		CarrierRef:        trackingNumber,
		EstimatedDelivery: a.estimateDelivery(req.ServiceCode),
		Metadata: map[string]string{
			"barcode_display":      entry.OutboundCarriers.Carrier1.Barcode1.BarcodeDisplay,
			"delivery_method_code": entry.OutboundCarriers.Carrier1.DeliveryMethodCode,
		},
	}

	// Decode label image (base64 PDF/ZPL per spec)
	if img := entry.OutboundCarriers.LabelImage; img != "" {
		decoded, err := base64.StdEncoding.DecodeString(img)
		if err != nil {
			// Some responses may use base64.URLEncoding or already be raw bytes
			decoded = []byte(img)
		}
		result.LabelData = decoded
		result.LabelFormat = "PDF"
		if strings.ToUpper(req.Options.LabelFormat) == "ZPL" {
			result.LabelFormat = "ZPL"
		}
	}

	return result, nil
}

// ============================================================================
// HTTP TRANSPORT
// ============================================================================

// postXML sends an XML body to the given Evri routing method endpoint,
// routing through the egress proxy if EGRESS_PROXY_URL is set.
func (a *EvriAdapter) postXML(ctx context.Context, creds CarrierCredentials, method string, payload []byte) (*http.Response, error) {
	targetURL := a.baseURL(creds.IsSandbox) + "/" + method

	if evriEgressProxyURL != "" {
		return a.postViaProxy(ctx, targetURL, creds.Username, creds.Password, payload)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", targetURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "text/xml")
	req.Header.Set("Accept", "text/xml,application/xml")
	req.SetBasicAuth(creds.Username, creds.Password)
	return a.httpClient.Do(req)
}

// postViaProxy wraps the request in the egress proxy JSON envelope (same as Temu).
func (a *EvriAdapter) postViaProxy(ctx context.Context, targetURL, username, password string, bodyBytes []byte) (*http.Response, error) {
	// Build the Authorization header manually so the proxy can forward it
	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))

	headers := map[string]string{
		"Content-Type":  "text/xml",
		"Accept":        "text/xml,application/xml",
		"Authorization": authHeader,
	}

	proxyPayload := map[string]interface{}{
		"url":     targetURL,
		"method":  "POST",
		"headers": headers,
		"body":    string(bodyBytes),
	}
	proxyBody, err := json.Marshal(proxyPayload)
	if err != nil {
		return nil, fmt.Errorf("evri proxy: marshal: %w", err)
	}

	proxyReq, err := http.NewRequestWithContext(ctx, "POST",
		evriEgressProxyURL+"/forward", bytes.NewReader(proxyBody))
	if err != nil {
		return nil, fmt.Errorf("evri proxy: build request: %w", err)
	}
	proxyReq.Header.Set("Content-Type", "application/json")
	if evriEgressProxySecret != "" {
		proxyReq.Header.Set("X-Proxy-Secret", evriEgressProxySecret)
	}

	return a.httpClient.Do(proxyReq)
}

// ============================================================================
// HELPERS
// ============================================================================

func (a *EvriAdapter) baseURL(sandbox bool) string {
	if sandbox {
		return evriSITBase
	}
	return evriProdBase
}

// clientName retrieves the Evri clientName from Extra, falling back to username.
func (a *EvriAdapter) clientName(creds CarrierCredentials) string {
	if creds.Extra != nil {
		if v, ok := creds.Extra["client_name"]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return strings.TrimSuffix(creds.Username, "-sit")
}

// mapShipmentRequest converts the generic ShipmentRequest into an evriRoutingEntry.
func (a *EvriAdapter) mapShipmentRequest(req ShipmentRequest) (evriRoutingEntry, error) {
	to := req.ToAddress
	if to.Country == "" {
		return evriRoutingEntry{}, fmt.Errorf("destination country code is required")
	}
	if to.PostalCode == "" && to.Country == "GB" {
		return evriRoutingEntry{}, fmt.Errorf("postcode is required for GB shipments")
	}

	firstName, lastName := evriSplitName(to.Name)

	// Weight: kg → grams
	totalWeightGrams := 0
	for _, p := range req.Parcels {
		totalWeightGrams += int(math.Round(p.Weight * 1000))
	}
	if totalWeightGrams == 0 {
		totalWeightGrams = 500
	}

	// Dimensions from first parcel
	lengthCM, widthCM, depthCM := 1, 1, 1
	if len(req.Parcels) > 0 {
		p := req.Parcels[0]
		if p.Length > 0 {
			lengthCM = int(math.Round(p.Length))
		}
		if p.Width > 0 {
			widthCM = int(math.Round(p.Width))
		}
		if p.Height > 0 {
			depthCM = int(math.Round(p.Height))
		}
	}

	nextDay := req.ServiceCode == EvriServiceNextDay
	signature := req.ServiceCode == EvriServiceSignature || req.Options.Signature

	// Declared value in pence
	valuePence := 0
	if req.Options.Customs != nil {
		for _, item := range req.Options.Customs.Items {
			valuePence += int(math.Round(item.Value * 100))
		}
	}

	exportReason := ""
	if req.Options.Customs != nil {
		exportReason = evriMapExportReason(req.Options.Customs.ContentsType)
	}

	from := req.FromAddress

	return evriRoutingEntry{
		FirstName:       firstName,
		LastName:        lastName,
		HouseNo:         to.AddressLine1,
		StreetName:      evriStreetName(to),
		AddressLine1:    to.AddressLine2,
		City:            to.City,
		Region:          to.State,
		PostCode:        to.PostalCode,
		CountryCode:     to.Country,
		Phone:           to.Phone,
		Email:           to.Email,
		Reference1:      req.Reference,
		WeightGrams:     totalWeightGrams,
		LengthCM:        lengthCM,
		WidthCM:         widthCM,
		DepthCM:         depthCM,
		ValuePence:      valuePence,
		NumberOfItems:   len(req.Parcels),
		Description:     req.Description,
		NextDay:         nextDay,
		Signature:       signature,
		SenderName:      from.Company,
		SenderLine1:     from.AddressLine1,
		SenderLine2:     from.City + " " + from.PostalCode,
		DespatchDate:    time.Now().Format("2006-01-02"),
		CountryOfOrigin: from.Country,
		Currency:        "GBP",
		ExportReason:    exportReason,
	}, nil
}

func (a *EvriAdapter) estimateDelivery(serviceCode string) time.Time {
	switch serviceCode {
	case EvriServiceNextDay:
		return time.Now().Add(24 * time.Hour)
	case EvriServiceLargeParcel:
		return time.Now().Add(4 * 24 * time.Hour)
	case EvriServiceInternational:
		return time.Now().Add(10 * 24 * time.Hour)
	default:
		return time.Now().Add(3 * 24 * time.Hour)
	}
}

// evriSplitName splits "First Last" → (first, last). lastName is mandatory.
func evriSplitName(full string) (first, last string) {
	parts := strings.Fields(full)
	switch len(parts) {
	case 0:
		return "", "Customer"
	case 1:
		return "", parts[0]
	default:
		return strings.Join(parts[:len(parts)-1], " "), parts[len(parts)-1]
	}
}

// evriStreetName picks the most appropriate address line for Evri's streetName field.
// Evri wants the street name here (not the house number, which goes in houseNo).
func evriStreetName(addr Address) string {
	if addr.AddressLine2 != "" {
		return addr.AddressLine2
	}
	if addr.AddressLine1 != "" {
		return addr.AddressLine1
	}
	return "Unknown Street"
}

// evriMapExportReason maps generic customs content types to Evri's allowed values.
func evriMapExportReason(contentsType string) string {
	switch strings.ToLower(contentsType) {
	case "merchandise", "sale", "commercial_goods":
		return "commercial_goods"
	case "gift", "gifts":
		return "gifts"
	case "document", "documents":
		return "documents"
	case "sample", "commercial_sample":
		return "commercial_sample"
	case "return", "returned_goods":
		return "returned_goods"
	default:
		return "commercial_goods"
	}
}

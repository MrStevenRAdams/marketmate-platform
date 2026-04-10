// platform/backend/marketplace/clients/evri/evri_client.go
package evri

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// ============================================================================
// CONSTANTS & CONFIG
// ============================================================================

const (
	EvriSITBase       = "https://sit.hermes-europe.co.uk/routing/service/rest/v4"
	EvriProdBase      = "https://www.hermes-europe.co.uk/routing/service/rest/v4"
	EvriTrackingBase  = "https://api.hermesworld.co.uk"
	EvriOAuthEndpoint = "https://api.hermesworld.co.uk/oauth/token"
)

// ============================================================================
// CLIENT STRUCT
// ============================================================================

type Client struct {
	httpClient  *http.Client
	proxyURL    string
	proxySecret string

	// OAuth token cache for tracking API
	tokenMu        sync.Mutex
	cachedToken    string
	tokenExpiresAt time.Time
}

func NewClient() *Client {
	return &Client{
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		proxyURL:    os.Getenv("EGRESS_PROXY_URL"),
		proxySecret: os.Getenv("EGRESS_PROXY_SECRET"),
	}
}

// ============================================================================
// COVERAGE WARNING TYPES
// ============================================================================

type CoverageWarning struct {
	Code        string `json:"code"`
	Severity    string `json:"severity"` // "info", "warn", "error"
	Title       string `json:"title"`
	Description string `json:"description"`
	Suggestion  string `json:"suggestion,omitempty"`
}

// GetCoverageWarnings returns pre-flight coverage warnings for a destination.
func GetCoverageWarnings(destCountry, postcode, service string) []CoverageWarning {
	var warnings []CoverageWarning
	pc := strings.ToUpper(strings.TrimSpace(postcode))
	prefix2 := ""
	if len(pc) >= 2 {
		prefix2 = pc[:2]
	}
	prefix3 := ""
	if len(pc) >= 3 {
		prefix3 = pc[:3]
	}
	prefix4 := ""
	if len(pc) >= 4 {
		prefix4 = pc[:4]
	}

	if destCountry == "GB" || destCountry == "" {
		// BFPO — Evri does not serve these
		if strings.HasPrefix(pc, "BFPO") {
			warnings = append(warnings, CoverageWarning{
				Code:        "BFPO_UNSUPPORTED",
				Severity:    "error",
				Title:       "BFPO address not supported by Evri",
				Description: "Evri does not deliver to BFPO addresses.",
				Suggestion:  "Use Royal Mail for BFPO shipments.",
			})
			return warnings
		}

		// Channel Islands — outside UK customs
		if prefix2 == "GY" || prefix2 == "JE" {
			warnings = append(warnings, CoverageWarning{
				Code:        "CHANNEL_ISLANDS_CUSTOMS",
				Severity:    "warn",
				Title:       "Channel Islands — outside UK customs territory",
				Description: "Channel Islands (Jersey & Guernsey) are outside the UK customs territory. CN22/CN23 customs forms are required.",
				Suggestion:  "Ensure a customs profile is attached to this shipment.",
			})
		}

		// Isle of Man
		if prefix2 == "IM" {
			warnings = append(warnings, CoverageWarning{
				Code:        "ISLE_OF_MAN_CUSTOMS",
				Severity:    "warn",
				Title:       "Isle of Man — outside UK VAT territory",
				Description: "The Isle of Man has its own VAT regime separate from the UK mainland.",
				Suggestion:  "Check customs requirements before dispatching.",
			})
		}

		// Shetland / Orkney — restricted
		if prefix2 == "ZE" || prefix3 == "KW1" {
			if prefix3 == "KW1" {
				d := ""
				if len(pc) >= 4 {
					d = pc[3:4]
				}
				if d == "5" || d == "6" || d == "7" {
					warnings = append(warnings, CoverageWarning{
						Code:        "SHETLAND_ORKNEY_RESTRICTED",
						Severity:    "warn",
						Title:       "Shetland/Orkney — restricted delivery zone",
						Description: "KW15–KW17 (Orkney) and ZE (Shetland) postcodes are in a restricted delivery zone for Evri. Service availability may be limited.",
						Suggestion:  "Consider Royal Mail for these remote island destinations.",
					})
				}
			} else {
				warnings = append(warnings, CoverageWarning{
					Code:        "SHETLAND_RESTRICTED",
					Severity:    "warn",
					Title:       "Shetland — restricted delivery zone",
					Description: "ZE (Shetland) postcodes are in a restricted delivery zone for Evri.",
					Suggestion:  "Consider Royal Mail for Shetland destinations.",
				})
			}
		}

		// Scottish Highlands surcharge zone
		highlands := isScottishHighlands(prefix2, prefix3, prefix4)
		if highlands {
			warnings = append(warnings, CoverageWarning{
				Code:        "SCOTTISH_HIGHLANDS_SURCHARGE",
				Severity:    "info",
				Title:       "Scottish Highlands — surcharge zone",
				Description: "This postcode is in the Scottish Highlands surcharge zone. An additional carrier surcharge of approximately £5–10 typically applies.",
				Suggestion:  "Confirm surcharge with Evri account manager.",
			})
		}
	} else {
		// International
		if destCountry != "IE" { // Ireland is straightforward
			warnings = append(warnings, CoverageWarning{
				Code:        "INTERNATIONAL_CUSTOMS",
				Severity:    "info",
				Title:       "International shipment — customs documentation required",
				Description: fmt.Sprintf("Shipping to %s requires customs documentation. IOSS number may apply for EU shipments under €150.", destCountry),
				Suggestion:  "Attach a customs profile with HS code, country of manufacture, and duty terms.",
			})
		}

		// EU IOSS
		euCountries := map[string]bool{
			"AT": true, "BE": true, "BG": true, "HR": true, "CY": true, "CZ": true,
			"DK": true, "EE": true, "FI": true, "FR": true, "DE": true, "GR": true,
			"HU": true, "IE": true, "IT": true, "LV": true, "LT": true, "LU": true,
			"MT": true, "NL": true, "PL": true, "PT": true, "RO": true, "SK": true,
			"SI": true, "ES": true, "SE": true,
		}
		if euCountries[destCountry] {
			warnings = append(warnings, CoverageWarning{
				Code:        "EU_IOSS_REQUIRED",
				Severity:    "warn",
				Title:       "EU shipment — IOSS number required for orders under €150",
				Description: "Since 1 July 2021, EU imports under €150 require an IOSS (Import One Stop Shop) number. Without it, the recipient pays import VAT on delivery.",
				Suggestion:  "Add your IOSS number to the customs profile for EU shipments.",
			})
		}
	}

	return warnings
}

func isScottishHighlands(prefix2, prefix3, prefix4 string) bool {
	// KW (excluding KW15-17 already flagged as Orkney — but include them here too as Highlands)
	if prefix2 == "KW" || prefix2 == "IV" || prefix2 == "HS" {
		return true
	}
	// PA20-49, PA60-78
	if prefix2 == "PA" {
		paNum := 0
		fmt.Sscanf(prefix3[2:], "%d", &paNum)
		if paNum == 0 && len(prefix4) >= 4 {
			fmt.Sscanf(prefix4[2:], "%d", &paNum)
		}
		if (paNum >= 20 && paNum <= 49) || (paNum >= 60 && paNum <= 78) {
			return true
		}
	}
	// PH19-26
	if prefix2 == "PH" {
		phNum := 0
		fmt.Sscanf(prefix3[2:], "%d", &phNum)
		if phNum == 0 {
			fmt.Sscanf(prefix4[2:], "%d", &phNum)
		}
		if phNum >= 19 && phNum <= 26 {
			return true
		}
	}
	// KA27-28
	if prefix2 == "KA" {
		kaNum := 0
		fmt.Sscanf(prefix3[2:], "%d", &kaNum)
		if kaNum == 0 {
			fmt.Sscanf(prefix4[2:], "%d", &kaNum)
		}
		if kaNum >= 27 && kaNum <= 28 {
			return true
		}
	}
	return false
}

// ============================================================================
// EVRI COVERAGE ERROR
// ============================================================================

type EvriCoverageError struct {
	ErrorCode   int    `json:"error_code"`
	Description string `json:"description"`
}

func (e *EvriCoverageError) Error() string {
	return fmt.Sprintf("evri coverage error %d: %s", e.ErrorCode, e.Description)
}

var coverageErrorCodes = map[int]bool{
	10159: true,
	10160: true,
	10089: true,
	10090: true,
}

// ============================================================================
// REQUEST / RESPONSE TYPES — ROUTING WEB SERVICE
// ============================================================================

type DeliveryRequest struct {
	ClientID   string
	ClientName string
	IsSandbox  bool

	// Parcel
	WeightKg float64
	LengthCM float64
	WidthCM  float64
	DepthCM  float64
	Value    float64

	// Customer
	FirstName    string
	LastName     string
	AddressLine1 string
	AddressLine2 string
	AddressLine3 string
	City         string
	Postcode     string
	Country      string // ISO 2-letter

	// References
	CustomerRef1 string
	CustomerRef2 string

	// Notifications
	Mobile string
	Email  string

	// Services
	NextDay           bool
	Signature         bool
	HouseholdSig      bool
	ParcelShopID      string
	ParcelShopAddress string

	// Optional return label
	IncludeReturnLabel bool

	// Despatch
	DespatchDate    string // YYYY-MM-DD
	CountryOfOrigin string // ISO 2-letter

	// International
	Contents []ParcelContent
	IOSSNumber string
	EOIRNumber string
	VATNumber  string
	CPCCode    string
	DutyPaid   string // "DDP" or "DDU"
}

type CollectionRequest struct {
	ClientID   string
	ClientName string
	IsSandbox  bool

	WeightKg float64
	LengthCM float64
	WidthCM  float64
	DepthCM  float64

	CollectionDate string
	CollectionRef  string

	AddressLine1 string
	AddressLine2 string
	City         string
	Postcode     string
	Country      string

	NextDay bool
}

type ReturnRequest struct {
	ClientID        string
	ClientName      string
	IsSandbox       bool
	OriginalBarcode string
	CustomerRef     string
	FirstName       string
	LastName        string
	AddressLine1    string
	AddressLine2    string
	City            string
	Postcode        string
	Country         string
	WeightKg        float64
	LengthCM        float64
	WidthCM         float64
	DepthCM         float64
}

type ParcelContent struct {
	SKU                 string
	Description         string
	HSCode              string
	Quantity            int
	WeightKg            float64
	UnitValueGBP        float64
	CountryOfManufacture string
}

type RoutingResponse struct {
	Barcode             string
	OutboundLabelBase64 string
	ReturnBarcode       string
	ReturnLabelBase64   string
	SortLevel1          string
	SortLevel2          string
	SortLevel3          string
	SortLevel4          string
	SortLevel5          string
	ServiceDescription  string
	ErrorCode           int
	ErrorDescription    string
}

// ============================================================================
// XML REQUEST STRUCTURES
// ============================================================================

type evriDeliveryRoutingRequest struct {
	XMLName xml.Name                    `xml:"deliveryRoutingRequest"`
	Entries []evriDeliveryRoutingEntry  `xml:"entries>entry"`
}

type evriDeliveryRoutingEntry struct {
	ClientID             string              `xml:"clientId"`
	ClientName           string              `xml:"clientName"`
	Parcel               evriParcel          `xml:"parcel"`
	Customer             evriCustomer        `xml:"customer"`
	Services             evriServices        `xml:"services"`
	DespatchDate         string              `xml:"expectedDespatchDate"`
	CountryOfOrigin      string              `xml:"countryOfOrigin"`
	CustomerReference1   string              `xml:"customerReference1,omitempty"`
	CustomerReference2   string              `xml:"customerReference2,omitempty"`
	InternationalDetails *evriIntlDetails    `xml:"internationalDetails,omitempty"`
}

type evriParcel struct {
	WeightKg          float64 `xml:"weight"`
	LengthCM          float64 `xml:"length"`
	WidthCM           float64 `xml:"width"`
	DepthCM           float64 `xml:"depth"`
	Girth             float64 `xml:"girth"`
	CombinedDimension float64 `xml:"combinedDimension"`
	Volume            float64 `xml:"volume"`
}

type evriCustomer struct {
	FirstName    string `xml:"firstName"`
	LastName     string `xml:"lastName"`
	AddressLine1 string `xml:"address>address1"`
	AddressLine2 string `xml:"address>address2,omitempty"`
	AddressLine3 string `xml:"address>address3,omitempty"`
	City         string `xml:"address>city"`
	Postcode     string `xml:"address>postCode"`
	Country      string `xml:"address>countryCode"`
	Email        string `xml:"email,omitempty"`
	Mobile       string `xml:"mobilePhone,omitempty"`
	AlertType    int    `xml:"customerAlertType,omitempty"`
	AlertGroup   string `xml:"customerAlertGroup,omitempty"`
}

type evriServices struct {
	NextDay          *evriServiceFlag       `xml:"nextDay,omitempty"`
	Signature        *evriServiceFlag       `xml:"signature,omitempty"`
	HouseholdSig     *evriServiceFlag       `xml:"householdSignature,omitempty"`
	ParcelShop       *evriParcelShopService `xml:"parcelShopService,omitempty"`
}

type evriServiceFlag struct {
	Active bool `xml:"active"`
}

type evriParcelShopService struct {
	ParcelShopID      string `xml:"parcelShopId"`
	AddressLine1      string `xml:"address>address1,omitempty"`
}

type evriIntlDetails struct {
	Contents   []evriContent `xml:"contents>content,omitempty"`
	IOSSNumber string        `xml:"iossNumber,omitempty"`
	EOIRNumber string        `xml:"eoirNumber,omitempty"`
	VATNumber  string        `xml:"vatNumber,omitempty"`
	CPCCode    string        `xml:"cpcCode,omitempty"`
	DutyPaid   string        `xml:"dutyPaid,omitempty"` // DDP or DDU
}

type evriContent struct {
	SKU                  string  `xml:"sku,omitempty"`
	Description          string  `xml:"description"`
	HSCode               string  `xml:"hsCode,omitempty"`
	Quantity             int     `xml:"quantity"`
	WeightKg             float64 `xml:"weight"`
	UnitValue            float64 `xml:"unitValue"`
	CountryOfManufacture string  `xml:"countryOfManufacture"`
}

// ============================================================================
// XML RESPONSE STRUCTURES
// ============================================================================

type evriRoutingResponseXML struct {
	XMLName xml.Name              `xml:"deliveryRoutingResponse"`
	Entries []evriResponseEntryXML `xml:"entries>entry"`
}

type evriResponseEntryXML struct {
	Carrier         *evriCarrierXML      `xml:"carriers>carrier"`
	ServiceDesc     *evriServiceDescXML  `xml:"serviceDescriptions>serviceDescription"`
	Titles          []string             `xml:"titles>title"`
	ErrorMessage    *evriErrorXML        `xml:"errorMessage"`
	ReturnLabel     *evriReturnLabelXML  `xml:"returnLabel"`
}

type evriCarrierXML struct {
	Barcode    string `xml:"barcode"`
	LabelImage string `xml:"labelImage"`
	SortCode1  string `xml:"sortCode1"`
	SortCode2  string `xml:"sortCode2"`
	SortCode3  string `xml:"sortCode3"`
	SortCode4  string `xml:"sortCode4"`
	SortCode5  string `xml:"sortCode5"`
}

type evriServiceDescXML struct {
	Description string `xml:"description"`
}

type evriErrorXML struct {
	Code    int    `xml:"code"`
	Message string `xml:"message"`
}

type evriReturnLabelXML struct {
	Barcode    string `xml:"barcode"`
	LabelImage string `xml:"labelImage"`
}

// ============================================================================
// ROUTING WEB SERVICE METHODS
// ============================================================================

// routeDeliveryCreatePreadviceAndLabel — standard dispatch + outbound label
func (c *Client) RouteDeliveryCreatePreadviceAndLabel(ctx context.Context, req DeliveryRequest) (*RoutingResponse, error) {
	return c.callRoutingService(ctx, "routeDeliveryCreatePreadviceAndLabel", req, false)
}

// RouteDeliveryCreatePreadviceReturnBarcodeAndLabel — dispatch + outbound + return label
func (c *Client) RouteDeliveryCreatePreadviceReturnBarcodeAndLabel(ctx context.Context, req DeliveryRequest) (*RoutingResponse, error) {
	return c.callRoutingService(ctx, "routeDeliveryCreatePreadviceReturnBarcodeAndLabel", req, true)
}

// DetermineDeliveryRouting — address validation/routing check only, no commit
func (c *Client) DetermineDeliveryRouting(ctx context.Context, req DeliveryRequest) (*RoutingResponse, error) {
	return c.callRoutingService(ctx, "determineDeliveryRouting", req, false)
}

// RouteCollectionCreatePreadvice — schedule warehouse collection
func (c *Client) RouteCollectionCreatePreadvice(ctx context.Context, req CollectionRequest) (*RoutingResponse, error) {
	return c.callCollectionService(ctx, "routeCollectionCreatePreadvice", req)
}

// RouteCollectionCreatePreadviceAndLabel — collection + label
func (c *Client) RouteCollectionCreatePreadviceAndLabel(ctx context.Context, req CollectionRequest) (*RoutingResponse, error) {
	return c.callCollectionService(ctx, "routeCollectionCreatePreadviceAndLabel", req)
}

// CreateReturnBarcodeAndLabel — standalone return label for post-shipment returns
func (c *Client) CreateReturnBarcodeAndLabel(ctx context.Context, req ReturnRequest) (*RoutingResponse, error) {
	return c.callReturnService(ctx, req)
}

// ============================================================================
// INTERNAL ROUTING CALLERS
// ============================================================================

func (c *Client) callRoutingService(ctx context.Context, operation string, req DeliveryRequest, returnLabel bool) (*RoutingResponse, error) {
	girth := 2*req.WidthCM + 2*req.DepthCM
	combined := req.LengthCM + girth
	volume := req.LengthCM * req.WidthCM * req.DepthCM

	entry := evriDeliveryRoutingEntry{
		ClientID:   req.ClientID,
		ClientName: req.ClientName,
		Parcel: evriParcel{
			WeightKg:          req.WeightKg,
			LengthCM:          req.LengthCM,
			WidthCM:           req.WidthCM,
			DepthCM:           req.DepthCM,
			Girth:             math.Round(girth*100) / 100,
			CombinedDimension: math.Round(combined*100) / 100,
			Volume:            math.Round(volume*100) / 100,
		},
		Customer: evriCustomer{
			FirstName:    req.FirstName,
			LastName:     req.LastName,
			AddressLine1: req.AddressLine1,
			AddressLine2: req.AddressLine2,
			AddressLine3: req.AddressLine3,
			City:         req.City,
			Postcode:     req.Postcode,
			Country:      req.Country,
			Email:        req.Email,
			Mobile:       req.Mobile,
		},
		Services:           evriServices{},
		DespatchDate:       req.DespatchDate,
		CountryOfOrigin:    req.CountryOfOrigin,
		CustomerReference1: req.CustomerRef1,
		CustomerReference2: req.CustomerRef2,
	}

	// Notifications
	if req.Mobile != "" {
		entry.Customer.AlertType = 2
		entry.Customer.AlertGroup = "SMS"
	}

	// Services
	if req.NextDay {
		entry.Services.NextDay = &evriServiceFlag{Active: true}
	}
	if req.Signature {
		entry.Services.Signature = &evriServiceFlag{Active: true}
	}
	if req.HouseholdSig {
		entry.Services.HouseholdSig = &evriServiceFlag{Active: true}
	}
	if req.ParcelShopID != "" {
		entry.Services.ParcelShop = &evriParcelShopService{
			ParcelShopID: req.ParcelShopID,
			AddressLine1: req.ParcelShopAddress,
		}
	}

	// International
	if req.Country != "GB" && len(req.Contents) > 0 {
		intl := &evriIntlDetails{
			IOSSNumber: req.IOSSNumber,
			EOIRNumber: req.EOIRNumber,
			VATNumber:  req.VATNumber,
			CPCCode:    req.CPCCode,
			DutyPaid:   req.DutyPaid,
		}
		for _, item := range req.Contents {
			intl.Contents = append(intl.Contents, evriContent{
				SKU:                  item.SKU,
				Description:          item.Description,
				HSCode:               item.HSCode,
				Quantity:             item.Quantity,
				WeightKg:             item.WeightKg,
				UnitValue:            item.UnitValueGBP,
				CountryOfManufacture: item.CountryOfManufacture,
			})
		}
		entry.InternationalDetails = intl
	}

	xmlBody, err := xml.Marshal(evriDeliveryRoutingRequest{Entries: []evriDeliveryRoutingEntry{entry}})
	if err != nil {
		return nil, fmt.Errorf("evri: failed to marshal request: %w", err)
	}

	baseURL := EvriProdBase
	if req.IsSandbox {
		baseURL = EvriSITBase
	}

	httpResp, err := c.postXML(ctx, baseURL, operation, req.ClientID, "", xmlBody)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("evri: failed to read response body: %w", err)
	}

	return parseRoutingResponse(body, returnLabel)
}

func (c *Client) callCollectionService(ctx context.Context, operation string, req CollectionRequest) (*RoutingResponse, error) {
	// Build a minimal delivery-style request for collection
	delReq := DeliveryRequest{
		ClientID:     req.ClientID,
		ClientName:   req.ClientName,
		IsSandbox:    req.IsSandbox,
		WeightKg:     req.WeightKg,
		LengthCM:     req.LengthCM,
		WidthCM:      req.WidthCM,
		DepthCM:      req.DepthCM,
		AddressLine1: req.AddressLine1,
		AddressLine2: req.AddressLine2,
		City:         req.City,
		Postcode:     req.Postcode,
		Country:      req.Country,
		DespatchDate: req.CollectionDate,
		CustomerRef1: req.CollectionRef,
		NextDay:      req.NextDay,
	}
	return c.callRoutingService(ctx, operation, delReq, false)
}

func (c *Client) callReturnService(ctx context.Context, req ReturnRequest) (*RoutingResponse, error) {
	delReq := DeliveryRequest{
		ClientID:     req.ClientID,
		ClientName:   req.ClientName,
		IsSandbox:    req.IsSandbox,
		WeightKg:     req.WeightKg,
		LengthCM:     req.LengthCM,
		WidthCM:      req.WidthCM,
		DepthCM:      req.DepthCM,
		FirstName:    req.FirstName,
		LastName:     req.LastName,
		AddressLine1: req.AddressLine1,
		AddressLine2: req.AddressLine2,
		City:         req.City,
		Postcode:     req.Postcode,
		Country:      req.Country,
		CustomerRef1: req.CustomerRef,
		DespatchDate: time.Now().Format("2006-01-02"),
	}
	return c.callRoutingService(ctx, "createReturnBarcodeAndLabel", delReq, true)
}

func parseRoutingResponse(body []byte, wantReturn bool) (*RoutingResponse, error) {
	var xmlResp evriRoutingResponseXML
	if err := xml.Unmarshal(body, &xmlResp); err != nil {
		return nil, fmt.Errorf("evri: failed to parse XML response: %w", err)
	}

	if len(xmlResp.Entries) == 0 {
		return nil, fmt.Errorf("evri: empty response entries")
	}

	entry := xmlResp.Entries[0]
	resp := &RoutingResponse{}

	if entry.ErrorMessage != nil && entry.ErrorMessage.Code != 0 {
		resp.ErrorCode = entry.ErrorMessage.Code
		resp.ErrorDescription = entry.ErrorMessage.Message
		if coverageErrorCodes[resp.ErrorCode] {
			return nil, &EvriCoverageError{ErrorCode: resp.ErrorCode, Description: resp.ErrorDescription}
		}
		return nil, fmt.Errorf("evri error %d: %s", resp.ErrorCode, resp.ErrorDescription)
	}

	if entry.Carrier != nil {
		resp.Barcode = entry.Carrier.Barcode
		resp.OutboundLabelBase64 = entry.Carrier.LabelImage
		resp.SortLevel1 = entry.Carrier.SortCode1
		resp.SortLevel2 = entry.Carrier.SortCode2
		resp.SortLevel3 = entry.Carrier.SortCode3
		resp.SortLevel4 = entry.Carrier.SortCode4
		resp.SortLevel5 = entry.Carrier.SortCode5
	}

	if entry.ServiceDesc != nil {
		resp.ServiceDescription = entry.ServiceDesc.Description
	}

	if wantReturn && entry.ReturnLabel != nil {
		resp.ReturnBarcode = entry.ReturnLabel.Barcode
		resp.ReturnLabelBase64 = entry.ReturnLabel.LabelImage
	}

	return resp, nil
}

// ============================================================================
// HTTP HELPERS — ROUTING WEB SERVICE (Basic Auth via proxy)
// ============================================================================

func (c *Client) postXML(ctx context.Context, baseURL, operation, clientID, password string, body []byte) (*http.Response, error) {
	targetURL := fmt.Sprintf("%s/%s", strings.TrimRight(baseURL, "/"), operation)

	var req *http.Request
	var err error

	if c.proxyURL != "" {
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, c.proxyURL, strings.NewReader(string(body)))
		if err != nil {
			return nil, err
		}
		req.Header.Set("X-Target-URL", targetURL)
		if c.proxySecret != "" {
			req.Header.Set("X-Proxy-Secret", c.proxySecret)
		}
	} else {
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, targetURL, strings.NewReader(string(body)))
		if err != nil {
			return nil, err
		}
	}

	req.Header.Set("Content-Type", "application/xml")
	req.Header.Set("Accept", "application/xml")

	// Basic auth from env
	evriClientID := os.Getenv("EVRI_CLIENT_ID")
	evriSecret := os.Getenv("EVRI_CLIENT_SECRET")
	if evriClientID != "" {
		req.SetBasicAuth(evriClientID, evriSecret)
	}

	return c.httpClient.Do(req)
}

// ============================================================================
// TRACKING API — OAuth2
// ============================================================================

type TrackingEvent struct {
	DateTime     time.Time       `json:"dateTime"`
	Location     *GeoLocation    `json:"location"`
	TrackingPoint TrackingPoint  `json:"trackingPoint"`
	Links        []TrackingLink  `json:"links"`
}

type GeoLocation struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type TrackingPoint struct {
	Description    string `json:"description"`
	TrackingPointID string `json:"trackingPointId"`
}

type TrackingLink struct {
	Rel  string `json:"rel"`
	Href string `json:"href"`
}

type ETAResponse struct {
	Barcode       string    `json:"barcode"`
	DisplayString string    `json:"displayString"`
	FromDateTime  time.Time `json:"fromDateTime"`
	ToDateTime    time.Time `json:"toDateTime"`
	Type          string    `json:"type"` // e.g. "WINDOW", "BEFORE_TIME"
}

type SignatureResponse struct {
	Barcode      string `json:"barcode"`
	ImageBase64  string `json:"imageBase64"`
	ImageFormat  string `json:"imageFormat"`
	PrintedName  string `json:"printedName"`
	SignedAt     time.Time `json:"signedAt"`
}

type SafePlacePhotoResponse struct {
	Barcode     string `json:"barcode"`
	ImageBase64 string `json:"imageBase64"`
	ImageFormat string `json:"imageFormat"`
	TakenAt     time.Time `json:"takenAt"`
}

type oauthTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

func (c *Client) getTrackingToken(ctx context.Context) (string, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	// Refresh 60 seconds before expiry
	if c.cachedToken != "" && time.Now().Before(c.tokenExpiresAt.Add(-60*time.Second)) {
		return c.cachedToken, nil
	}

	clientID := os.Getenv("EVRI_TRACKING_CLIENT_ID")
	clientSecret := os.Getenv("EVRI_TRACKING_CLIENT_SECRET")

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)

	var req *http.Request
	var err error

	tokenURL := EvriOAuthEndpoint
	if c.proxyURL != "" {
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, c.proxyURL, strings.NewReader(form.Encode()))
		if err != nil {
			return "", err
		}
		req.Header.Set("X-Target-URL", tokenURL)
		if c.proxySecret != "" {
			req.Header.Set("X-Proxy-Secret", c.proxySecret)
		}
	} else {
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
		if err != nil {
			return "", err
		}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("evri tracking oauth: %w", err)
	}
	defer resp.Body.Close()

	var tok oauthTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return "", fmt.Errorf("evri tracking oauth decode: %w", err)
	}

	c.cachedToken = tok.AccessToken
	c.tokenExpiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	return c.cachedToken, nil
}

func (c *Client) trackingGET(ctx context.Context, path string) (*http.Response, error) {
	token, err := c.getTrackingToken(ctx)
	if err != nil {
		return nil, err
	}

	targetURL := EvriTrackingBase + path

	var req *http.Request
	if c.proxyURL != "" {
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, c.proxyURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("X-Target-URL", targetURL)
		if c.proxySecret != "" {
			req.Header.Set("X-Proxy-Secret", c.proxySecret)
		}
	} else {
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
		if err != nil {
			return nil, err
		}
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	return c.httpClient.Do(req)
}

// GetTrackingEvents fetches all tracking events for a barcode
func (c *Client) GetTrackingEvents(ctx context.Context, barcode string) ([]TrackingEvent, error) {
	path := fmt.Sprintf("/client-trackingapi/v1/events?barcode=%s&descriptionType=CLIENT", url.QueryEscape(barcode))
	resp, err := c.trackingGET(ctx, path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Events []TrackingEvent `json:"events"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("evri tracking events decode: %w", err)
	}
	return result.Events, nil
}

// GetETA fetches estimated delivery time for a barcode (requires ETA product)
func (c *Client) GetETA(ctx context.Context, barcode string) (*ETAResponse, error) {
	path := fmt.Sprintf("/client-trackingapi/v1/etas?barcode=%s", url.QueryEscape(barcode))
	resp, err := c.trackingGET(ctx, path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}

	var eta ETAResponse
	if err := json.NewDecoder(resp.Body).Decode(&eta); err != nil {
		return nil, fmt.Errorf("evri eta decode: %w", err)
	}
	return &eta, nil
}

// GetSignature fetches proof of delivery signature
func (c *Client) GetSignature(ctx context.Context, barcode string) (*SignatureResponse, error) {
	path := fmt.Sprintf("/client-trackingapi/v1/signatures?barcode=%s", url.QueryEscape(barcode))
	resp, err := c.trackingGET(ctx, path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}

	var sig SignatureResponse
	if err := json.NewDecoder(resp.Body).Decode(&sig); err != nil {
		return nil, fmt.Errorf("evri signature decode: %w", err)
	}
	return &sig, nil
}

// GetSafePlacePhoto fetches safe place delivery photo.
// IMPORTANT: Callers must perform an entitlement check before exposing this image to end users.
// Safe place photos must only be returned to authenticated tenant users, never to unauthenticated callers.
func (c *Client) GetSafePlacePhoto(ctx context.Context, barcode string) (*SafePlacePhotoResponse, error) {
	path := fmt.Sprintf("/client-trackingapi/v1/safe-place-photos?barcode=%s", url.QueryEscape(barcode))
	resp, err := c.trackingGET(ctx, path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}

	var photo SafePlacePhotoResponse
	if err := json.NewDecoder(resp.Body).Decode(&photo); err != nil {
		return nil, fmt.Errorf("evri safe place photo decode: %w", err)
	}
	return &photo, nil
}

// IsCoverageError returns true if the error is an Evri coverage/service availability error
func IsCoverageError(err error) (*EvriCoverageError, bool) {
	if ce, ok := err.(*EvriCoverageError); ok {
		return ce, true
	}
	return nil, false
}

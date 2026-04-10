package amazon

// ============================================================================
// SP-API EXTENSIONS: FBA Inbound Shipments + Amazon Vendor Central
// ============================================================================
// File: platform/backend/marketplace/clients/amazon/sp_api_fba_vendor.go
//
// Adds two groups of SP-API calls to SPAPIClient:
//
//  1. FBA Inbound Shipments (FBA Inbound v0 API)
//     - CreateInboundShipmentPlan  → /fba/inbound/v0/plans
//     - CreateInboundShipment      → /fba/inbound/v0/shipments/{id}
//     - UpdateInboundShipment      → PUT /fba/inbound/v0/shipments/{id}
//
//  2. Amazon Vendor Central (Vendor Orders API)
//     - GetVendorOrders            → /vendor/orders/v1/purchaseOrders
//     - AcknowledgeVendorOrder     → /vendor/orders/v1/acknowledgements
//
// All calls go through the existing makeRequest / ensureValidToken machinery
// so token refresh, retries, and rate limiting are handled automatically.
// ============================================================================

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"
)

// ─── FBA Inbound Types ────────────────────────────────────────────────────────

// FBAInboundShipmentItem represents a single SKU line in a shipment plan request.
type FBAInboundShipmentItem struct {
	SellerSKU       string `json:"SellerSKU"`
	ASIN            string `json:"ASIN,omitempty"`
	Condition       string `json:"Condition"` // e.g. "NewItem"
	QuantityShipped int    `json:"QuantityShipped"`
}

// FBAInboundPlanRequest is the body sent to CreateInboundShipmentPlan.
type FBAInboundPlanRequest struct {
	ShipFromAddress    FBAAddress               `json:"ShipFromAddress"`
	ShipToCountryCode  string                   `json:"ShipToCountryCode"` // e.g. "GB"
	LabelPrepPreference string                  `json:"LabelPrepPreference"` // "SELLER_LABEL" or "AMAZON_LABEL_ONLY"
	InboundShipmentPlanRequestItems []FBAInboundShipmentItem `json:"InboundShipmentPlanRequestItems"`
}

// FBAAddress is a postal address used in FBA shipment requests.
type FBAAddress struct {
	Name            string `json:"Name"`
	AddressLine1    string `json:"AddressLine1"`
	AddressLine2    string `json:"AddressLine2,omitempty"`
	City            string `json:"City"`
	CountryCode     string `json:"CountryCode"`
	PostalCode      string `json:"PostalCode"`
	StateOrProvince string `json:"StateOrProvince,omitempty"`
}

// FBAInboundShipmentPlan is one element of the Amazon plan response.
// Amazon may split a single request into multiple plans (one per FC).
type FBAInboundShipmentPlan struct {
	ShipmentID              string                   `json:"ShipmentId"`
	DestinationFulfillmentCenterId string            `json:"DestinationFulfillmentCenterId"`
	ShipToAddress           FBAAddress               `json:"ShipToAddress"`
	LabelPrepType           string                   `json:"LabelPrepType"`
	Items                   []FBAInboundShipmentItem `json:"Items"`
	EstimatedBoxContentsFee *struct {
		TotalUnits int `json:"TotalUnits"`
	} `json:"EstimatedBoxContentsFee,omitempty"`
}

// FBAInboundPlanResponse is the full response from CreateInboundShipmentPlan.
type FBAInboundPlanResponse struct {
	InboundShipmentPlans []FBAInboundShipmentPlan `json:"InboundShipmentPlans"`
}

// ─── Vendor Central Types ─────────────────────────────────────────────────────

// VendorOrderItem is one line on a Vendor Central purchase order.
type VendorOrderItem struct {
	ItemSequenceNumber string `json:"itemSequenceNumber"`
	BuyerProductIdentifier struct {
		ASIN string `json:"asin"`
	} `json:"buyerProductIdentifier"`
	VendorProductIdentifier struct {
		VendorSKU string `json:"vendorSKU"`
	} `json:"vendorProductIdentifier,omitempty"`
	OrderedQuantity struct {
		Amount       int    `json:"amount"`
		UnitOfMeasure string `json:"unitOfMeasure"`
	} `json:"orderedQuantity"`
	NetCost *struct {
		CurrencyCode string `json:"currencyCode"`
		Amount       string `json:"amount"`
	} `json:"netCost,omitempty"`
}

// VendorOrder represents one Amazon Vendor Central purchase order.
type VendorOrder struct {
	PurchaseOrderNumber string          `json:"purchaseOrderNumber"`
	PurchaseOrderState  string          `json:"purchaseOrderState"` // "New", "Acknowledged", "Closed"
	OrderDetails        struct {
		PurchaseOrderDate    string           `json:"purchaseOrderDate"`
		PurchaseOrderExpiryDate string        `json:"purchaseOrderExpiryDate,omitempty"`
		DeliveryWindow       struct {
			StartDateTime string `json:"startDateTime"`
			EndDateTime   string `json:"endDateTime"`
		} `json:"deliveryWindow"`
		ShipToParty struct {
			PartyId string `json:"partyId"`
			Address struct {
				Name         string `json:"name"`
				AddressLine1 string `json:"addressLine1"`
				City         string `json:"city"`
				CountryCode  string `json:"countryCode"`
				PostalCode   string `json:"postalCode"`
			} `json:"address"`
		} `json:"shipToParty"`
		Items []VendorOrderItem `json:"items"`
	} `json:"orderDetails"`
}

// VendorOrdersResponse is the paginated response from the Vendor Orders API.
type VendorOrdersResponse struct {
	Orders    []VendorOrder `json:"orders"`
	NextToken string        `json:"nextToken,omitempty"`
}

// VendorAcknowledgementLine is one line in an acknowledgement request.
type VendorAcknowledgementLine struct {
	ItemSequenceNumber   string `json:"itemSequenceNumber"`
	AcknowledgedQuantity struct {
		Amount        int    `json:"amount"`
		UnitOfMeasure string `json:"unitOfMeasure"`
	} `json:"acknowledgedQuantity"`
	ScheduledShipDate    string `json:"scheduledShipDate,omitempty"`
	ScheduledDeliveryDate string `json:"scheduledDeliveryDate,omitempty"`
	AcknowledgementCode  string `json:"acknowledgementCode"` // "Accepted" or "Rejected"
	RejectionReason      string `json:"rejectionReason,omitempty"`
}

// VendorAcknowledgementRequest is the body sent to acknowledge/decline a vendor order.
type VendorAcknowledgementRequest struct {
	Acknowledgements []struct {
		PurchaseOrderNumber string                      `json:"purchaseOrderNumber"`
		SellerId            string                      `json:"sellerId"`
		Items               []VendorAcknowledgementLine `json:"items"`
	} `json:"acknowledgements"`
}

// ─── FBA Inbound Methods ──────────────────────────────────────────────────────

// CreateInboundShipmentPlan calls the Amazon SP-API to create an FBA inbound
// shipment plan. Amazon may split the items across multiple fulfilment centres
// and returns one plan per FC. The caller should persist each plan's ShipmentId
// and DestinationFulfillmentCenterId.
//
// SP-API endpoint: POST /fba/inbound/v0/plans
func (c *SPAPIClient) CreateInboundShipmentPlan(ctx context.Context, req FBAInboundPlanRequest) (*FBAInboundPlanResponse, error) {
	resp, err := c.makeRequest(ctx, "POST", "/fba/inbound/v0/plans", nil, req, c.inventoryLimiter)
	if err != nil {
		return nil, fmt.Errorf("CreateInboundShipmentPlan: %w", err)
	}
	defer resp.Body.Close()

	var envelope struct {
		Payload FBAInboundPlanResponse `json:"payload"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode CreateInboundShipmentPlan response: %w", err)
	}
	return &envelope.Payload, nil
}

// ConfirmInboundShipment calls the SP-API to confirm a specific FBA shipment plan,
// turning it into an active inbound shipment with a trackable status.
//
// SP-API endpoint: POST /fba/inbound/v0/shipments/{shipmentId}/confirm
func (c *SPAPIClient) ConfirmInboundShipment(ctx context.Context, amazonShipmentID string) error {
	path := fmt.Sprintf("/fba/inbound/v0/shipments/%s/confirm", amazonShipmentID)
	resp, err := c.makeRequest(ctx, "POST", path, nil, struct{}{}, c.inventoryLimiter)
	if err != nil {
		return fmt.Errorf("ConfirmInboundShipment: %w", err)
	}
	resp.Body.Close()
	return nil
}

// ─── Vendor Central Methods ───────────────────────────────────────────────────

// GetVendorOrders fetches purchase orders from Amazon Vendor Central.
// createdAfter limits results to orders created on or after that time.
// Pass an empty string for nextToken on the first call; pass the returned
// NextToken on subsequent calls to page through results.
//
// SP-API endpoint: GET /vendor/orders/v1/purchaseOrders
func (c *SPAPIClient) GetVendorOrders(ctx context.Context, createdAfter time.Time, nextToken string) (*VendorOrdersResponse, error) {
	params := url.Values{}
	params.Set("createdAfter", createdAfter.UTC().Format(time.RFC3339))
	params.Set("limit", "50")
	if nextToken != "" {
		params.Set("nextToken", nextToken)
	}
	// Filter to only "New" orders so we don't repeatedly re-import acknowledged ones
	params.Set("purchaseOrderState", "New")

	resp, err := c.makeRequest(ctx, "GET", "/vendor/orders/v1/purchaseOrders", params, nil, c.inventoryLimiter)
	if err != nil {
		return nil, fmt.Errorf("GetVendorOrders: %w", err)
	}
	defer resp.Body.Close()

	var envelope struct {
		Payload VendorOrdersResponse `json:"payload"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode GetVendorOrders response: %w", err)
	}
	return &envelope.Payload, nil
}

// AcknowledgeVendorOrder sends an accept or decline acknowledgement to Amazon
// Vendor Central for a specific purchase order.
// code should be "Accepted" or "Rejected".
//
// SP-API endpoint: POST /vendor/orders/v1/acknowledgements
func (c *SPAPIClient) AcknowledgeVendorOrder(ctx context.Context, poNumber string, items []VendorOrderItem, code string, reason string) error {
	lines := make([]VendorAcknowledgementLine, 0, len(items))
	for _, item := range items {
		line := VendorAcknowledgementLine{
			ItemSequenceNumber:  item.ItemSequenceNumber,
			AcknowledgementCode: code,
			RejectionReason:     reason,
		}
		line.AcknowledgedQuantity.Amount = item.OrderedQuantity.Amount
		line.AcknowledgedQuantity.UnitOfMeasure = item.OrderedQuantity.UnitOfMeasure
		if line.AcknowledgedQuantity.UnitOfMeasure == "" {
			line.AcknowledgedQuantity.UnitOfMeasure = "Each"
		}
		lines = append(lines, line)
	}

	body := VendorAcknowledgementRequest{
		Acknowledgements: []struct {
			PurchaseOrderNumber string                      `json:"purchaseOrderNumber"`
			SellerId            string                      `json:"sellerId"`
			Items               []VendorAcknowledgementLine `json:"items"`
		}{
			{
				PurchaseOrderNumber: poNumber,
				SellerId:            c.config.SellerID,
				Items:               lines,
			},
		},
	}

	resp, err := c.makeRequest(ctx, "POST", "/vendor/orders/v1/acknowledgements", nil, body, c.inventoryLimiter)
	if err != nil {
		return fmt.Errorf("AcknowledgeVendorOrder: %w", err)
	}
	resp.Body.Close()
	return nil
}

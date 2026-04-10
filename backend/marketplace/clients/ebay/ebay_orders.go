package ebay

import (
	"encoding/json"
	"fmt"
	"time"
)

// ============================================================================
// EBAY FULFILLMENT API - ORDERS
// ============================================================================
// These types are specific to the Fulfillment API and differ from Inventory API

// Order represents an eBay order from the Fulfillment API
type Order struct {
	OrderID                      string                       `json:"orderId"`
	OrderFulfillmentStatus       string                       `json:"orderFulfillmentStatus"`
	OrderPaymentStatus           string                       `json:"orderPaymentStatus"`
	CreationDate                 string                       `json:"creationDate"`
	LastModifiedDate             string                       `json:"lastModifiedDate"`
	PricingSummary               *OrderPricingSummary         `json:"pricingSummary,omitempty"`
	Buyer                        *OrderBuyer                  `json:"buyer,omitempty"`
	FulfillmentStartInstructions []FulfillmentInstruction     `json:"fulfillmentStartInstructions,omitempty"`
	LineItems                    []LineItem                   `json:"lineItems,omitempty"`
}

// OrderPricingSummary for orders (different from inventory PricingSummary)
type OrderPricingSummary struct {
	Total          *MoneyAmount `json:"total,omitempty"`
	Subtotal       *MoneyAmount `json:"subtotal,omitempty"`
	DeliveryCost   *MoneyAmount `json:"deliveryCost,omitempty"`
	Tax            *MoneyAmount `json:"tax,omitempty"`
}

// MoneyAmount represents currency values in orders
type MoneyAmount struct {
	Value                 string `json:"value"`
	Currency              string `json:"currency"`
	ConvertedFromValue    string `json:"convertedFromValue,omitempty"`
	ConvertedFromCurrency string `json:"convertedFromCurrency,omitempty"`
}

// OrderBuyer contains buyer information
type OrderBuyer struct {
	Username                 string              `json:"username"`
	BuyerRegistrationAddress *BuyerAddress       `json:"buyerRegistrationAddress,omitempty"`
}

// BuyerAddress contains buyer's registration address
type BuyerAddress struct {
	FullName       string          `json:"fullName,omitempty"`
	ContactAddress *ContactAddress `json:"contactAddress,omitempty"`
	PrimaryPhone   *Phone          `json:"primaryPhone,omitempty"`
}

type FulfillmentInstruction struct {
	ShippingStep *ShippingStep `json:"shippingStep,omitempty"`
}

type ShippingStep struct {
	ShipTo            *ShipToAddress `json:"shipTo,omitempty"`
	ShipToReferenceID string         `json:"shipToReferenceId,omitempty"`
}

// ShipToAddress contains shipping destination
type ShipToAddress struct {
	FullName       string          `json:"fullName,omitempty"`
	ContactAddress *ContactAddress `json:"contactAddress,omitempty"`
	PrimaryPhone   *Phone          `json:"primaryPhone,omitempty"`
}

type ContactAddress struct {
	AddressLine1    string `json:"addressLine1"`
	AddressLine2    string `json:"addressLine2,omitempty"`
	City            string `json:"city"`
	StateOrProvince string `json:"stateOrProvince"`
	PostalCode      string `json:"postalCode"`
	CountryCode     string `json:"countryCode"`
}

type Phone struct {
	PhoneNumber string `json:"phoneNumber"`
}

type LineItem struct {
	LineItemID                string       `json:"lineItemId"`
	Title                     string       `json:"title"`
	Quantity                  int          `json:"quantity"`
	LineItemCost              *MoneyAmount `json:"lineItemCost,omitempty"`
	Total                     *MoneyAmount `json:"total,omitempty"`
	DeliveryCost              *MoneyAmount `json:"deliveryCost,omitempty"`
	Tax                       *MoneyAmount `json:"tax,omitempty"`
	SKU                       string       `json:"sku,omitempty"`
	LineItemFulfillmentStatus string       `json:"lineItemFulfillmentStatus"`
}

type OrdersResponse struct {
	Orders     []Order `json:"orders"`
	Total      int     `json:"total"`
	NextCursor string  `json:"next,omitempty"`
}

// GetOrders fetches orders from eBay Fulfillment API
// filter can be: creationdate, lastmodifieddate
// Valid statuses: NOT_STARTED, IN_PROGRESS, FULFILLED, etc.
func (c *Client) GetOrders(filter, filterValue string, limit int) (*OrdersResponse, error) {
	if limit <= 0 {
		limit = 50
	}
	
	path := fmt.Sprintf("/sell/fulfillment/v1/order?filter=%s:[%s]&limit=%d", filter, filterValue, limit)
	
	respBody, err := c.get(path)
	if err != nil {
		return nil, err
	}
	
	var result OrdersResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}
	
	return &result, nil
}

// GetOrdersByCreationDate is a convenience method to fetch orders created after a date
func (c *Client) GetOrdersByCreationDate(createdAfter time.Time, limit int) (*OrdersResponse, error) {
	// eBay format: 2016-02-21T08:25:43.511Z
	filterValue := createdAfter.UTC().Format("2006-01-02T15:04:05.000Z") + ".."
	return c.GetOrders("creationdate", filterValue, limit)
}

// GetOrder fetches a specific order by ID
func (c *Client) GetOrder(orderID string) (*Order, error) {
	path := fmt.Sprintf("/sell/fulfillment/v1/order/%s", orderID)
	
	respBody, err := c.get(path)
	if err != nil {
		return nil, err
	}
	
	var order Order
	if err := json.Unmarshal(respBody, &order); err != nil {
		return nil, err
	}
	
	return &order, nil
}

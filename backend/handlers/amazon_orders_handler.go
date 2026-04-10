package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"module-a/marketplace/clients/amazon"
	"module-a/models"
	"module-a/services"
)

type AmazonOrdersHandler struct {
	orderService       *services.OrderService
	marketplaceService *services.MarketplaceService
	usage              *UsageInstrumentor
}

func NewAmazonOrdersHandler(orderService *services.OrderService, marketplaceService *services.MarketplaceService) *AmazonOrdersHandler {
	return &AmazonOrdersHandler{
		orderService:       orderService,
		marketplaceService: marketplaceService,
	}
}

// Amazon SP-API Order structures
type AmazonOrder struct {
	AmazonOrderID            string          `json:"AmazonOrderId"`
	PurchaseDate             string          `json:"PurchaseDate"`
	LastUpdateDate           string          `json:"LastUpdateDate"`
	OrderStatus              string          `json:"OrderStatus"`
	FulfillmentChannel       string          `json:"FulfillmentChannel"`
	SalesChannel             string          `json:"SalesChannel"`
	OrderChannel             string          `json:"OrderChannel"`
	ShipServiceLevel         string          `json:"ShipServiceLevel"`
	OrderTotal               AmazonMoney     `json:"OrderTotal"`
	NumberOfItemsShipped     int             `json:"NumberOfItemsShipped"`
	NumberOfItemsUnshipped   int             `json:"NumberOfItemsUnshipped"`
	PaymentMethod            string          `json:"PaymentMethod"`
	PaymentMethodDetails     []string        `json:"PaymentMethodDetails"`
	MarketplaceID            string          `json:"MarketplaceId"`
	ShipmentServiceLevelCategory string      `json:"ShipmentServiceLevelCategory"`
	OrderType                string          `json:"OrderType"`
	EarliestShipDate         string          `json:"EarliestShipDate"`
	LatestShipDate           string          `json:"LatestShipDate"`
	EarliestDeliveryDate     string          `json:"EarliestDeliveryDate"`
	LatestDeliveryDate       string          `json:"LatestDeliveryDate"`
	IsBusinessOrder          bool            `json:"IsBusinessOrder"`
	IsPrime                  bool            `json:"IsPrime"`
	IsPremiumOrder           bool            `json:"IsPremiumOrder"`
	IsGlobalExpressEnabled   bool            `json:"IsGlobalExpressEnabled"`
	ShippingAddress          *AmazonAddress  `json:"ShippingAddress,omitempty"`
	BuyerInfo                *AmazonBuyerInfo `json:"BuyerInfo,omitempty"`
}

type AmazonMoney struct {
	CurrencyCode string `json:"CurrencyCode"`
	Amount       string `json:"Amount"`
}

type AmazonAddress struct {
	Name          string `json:"Name"`
	AddressLine1  string `json:"AddressLine1"`
	AddressLine2  string `json:"AddressLine2"`
	AddressLine3  string `json:"AddressLine3"`
	City          string `json:"City"`
	County        string `json:"County"`
	District      string `json:"District"`
	StateOrRegion string `json:"StateOrRegion"`
	Municipality  string `json:"Municipality"`
	PostalCode    string `json:"PostalCode"`
	CountryCode   string `json:"CountryCode"`
	Phone         string `json:"Phone"`
	AddressType   string `json:"AddressType"`
}

type AmazonBuyerInfo struct {
	BuyerEmail  string `json:"BuyerEmail"`
	BuyerName   string `json:"BuyerName"`
	BuyerCounty string `json:"BuyerCounty"`
}

type AmazonOrderItem struct {
	ASIN                     string       `json:"ASIN"`
	SellerSKU                string       `json:"SellerSKU"`
	OrderItemID              string       `json:"OrderItemId"`
	Title                    string       `json:"Title"`
	QuantityOrdered          int          `json:"QuantityOrdered"`
	QuantityShipped          int          `json:"QuantityShipped"`
	ItemPrice                *AmazonMoney `json:"ItemPrice,omitempty"`
	ShippingPrice            *AmazonMoney `json:"ShippingPrice,omitempty"`
	ItemTax                  *AmazonMoney `json:"ItemTax,omitempty"`
	ShippingTax              *AmazonMoney `json:"ShippingTax,omitempty"`
	ShippingDiscount         *AmazonMoney `json:"ShippingDiscount,omitempty"`
	PromotionDiscount        *AmazonMoney `json:"PromotionDiscount,omitempty"`
	PromotionIDs             []string     `json:"PromotionIds"`
	IsGift                   bool         `json:"IsGift"`
	ConditionNote            string       `json:"ConditionNote"`
	ConditionID              string       `json:"ConditionId"`
	SerialNumberRequired     bool         `json:"SerialNumberRequired"`
	IsTransparency           bool         `json:"IsTransparency"`
}

// SP-API Response structures
type OrdersResponse struct {
	Payload struct {
		Orders          []AmazonOrder `json:"Orders"`
		NextToken       string        `json:"NextToken"`
		CreatedBefore   string        `json:"CreatedBefore"`
	} `json:"payload"`
	Errors []APIError `json:"errors,omitempty"`
}

type OrderItemsResponse struct {
	Payload struct {
		OrderItems []AmazonOrderItem `json:"OrderItems"`
		NextToken  string            `json:"NextToken"`
	} `json:"payload"`
	Errors []APIError `json:"errors,omitempty"`
}

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details string `json:"details"`
}

// ImportAmazonOrders imports orders from Amazon SP-API
func (h *AmazonOrdersHandler) ImportAmazonOrders(ctx context.Context, tenantID, credentialID string, createdAfter, createdBefore time.Time) (int, error) {
	// Get marketplace credentials
	cred, err := h.marketplaceService.GetCredential(ctx, tenantID, credentialID)
	if err != nil {
		return 0, fmt.Errorf("failed to get credentials: %w", err)
	}

	// Get merged credentials (global + tenant-specific)
	mergedCreds, err := h.marketplaceService.GetFullCredentials(ctx, cred)
	if err != nil {
		return 0, fmt.Errorf("failed to merge credentials: %w", err)
	}

	// Create SP-API client config
	config := &amazon.SPAPIConfig{
		LWAClientID:        mergedCreds["lwa_client_id"],
		LWAClientSecret:    mergedCreds["lwa_client_secret"],
		RefreshToken:       mergedCreds["refresh_token"],
		AWSAccessKeyID:     mergedCreds["aws_access_key_id"],
		AWSSecretAccessKey: mergedCreds["aws_secret_access_key"],
		MarketplaceID:      mergedCreds["marketplace_id"],
		Region:             mergedCreds["region"],
		SellerID:           mergedCreds["seller_id"],
		IsSandbox:          false,
	}

	// Create SP-API client
	client, err := amazon.NewSPAPIClient(ctx, config)
	if err != nil {
		return 0, fmt.Errorf("failed to create SP-API client: %w", err)
	}

	log.Printf("Fetching Amazon orders from %s to %s", createdAfter.Format(time.RFC3339), createdBefore.Format(time.RFC3339))

	// Fetch orders using SP-API client
	// GetOrdersWithPII uses Amazon's Restricted Data Token to fetch full buyer
	// name, email and shipping address. Falls back to standard call if token fails.
	ordersResp, err := client.GetOrdersWithPII(ctx, createdAfter)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch orders from Amazon: %w", err)
	}

	log.Printf("Fetched %d orders from Amazon", len(ordersResp.Orders))

	// Convert and save orders
	imported := 0
	for _, spOrder := range ordersResp.Orders {
		// Fetch order items for this order
		itemsResp, err := client.GetOrderItems(ctx, spOrder.AmazonOrderID)
		if err != nil {
			log.Printf("Failed to fetch items for order %s: %v", spOrder.AmazonOrderID, err)
			continue
		}

		// Convert to internal order format
		order := h.convertSPAPIOrderToInternal(spOrder, credentialID)

		orderID, isNew, err := h.orderService.CreateOrder(ctx, tenantID, order)
		if err != nil {
			log.Printf("Failed to save order %s: %v", spOrder.AmazonOrderID, err)
			continue
		}

		if !isNew {
			log.Printf("Skipping duplicate Amazon order %s (already exists as %s)", spOrder.AmazonOrderID, orderID)
			continue
		}

		// Save order line items
		for _, item := range itemsResp.OrderItems {
			line := h.convertSPAPIItemToOrderLine(item)
			if err := h.orderService.CreateOrderLine(ctx, tenantID, order.OrderID, line); err != nil {
				log.Printf("Failed to save order line %s: %v", item.OrderItemID, err)
			}
		}

		imported++
	}

	// Record usage — non-blocking
	if h.usage != nil && imported > 0 {
		h.usage.RecordOrderImport(context.Background(), tenantID, "amazon", imported, 0)
	}

	return imported, nil
}

// convertSPAPIOrderToInternal converts SP-API order to internal format
func (h *AmazonOrdersHandler) convertSPAPIOrderToInternal(spOrder amazon.Order, credentialID string) *models.Order {
	order := &models.Order{
		OrderID:          fmt.Sprintf("amz_%s", spOrder.AmazonOrderID),
		Channel:          "amazon",
		ChannelAccountID: credentialID,
		ExternalOrderID:  spOrder.AmazonOrderID,
		OrderDate:        spOrder.PurchaseDate,
		Status:           mapAmazonStatus(spOrder.OrderStatus),
		PaymentStatus:    "captured",
		SLAAtRisk:        false,
	}

	// Map customer
	if spOrder.BuyerInfo != nil {
		order.Customer = models.Customer{
			Name:  spOrder.BuyerInfo.BuyerName,
			Email: spOrder.BuyerInfo.BuyerEmail,
		}
	}

	// Map shipping address
	if spOrder.ShippingAddress != nil {
		order.ShippingAddress = models.Address{
			Name:         spOrder.ShippingAddress.Name,
			AddressLine1: spOrder.ShippingAddress.AddressLine1,
			City:         spOrder.ShippingAddress.City,
			State:        spOrder.ShippingAddress.StateOrRegion,
			PostalCode:   spOrder.ShippingAddress.PostalCode,
			Country:      spOrder.ShippingAddress.CountryCode,
		}
	}

	// Map totals
	var amount float64
	fmt.Sscanf(spOrder.OrderTotal.Amount, "%f", &amount)
	order.Totals = models.OrderTotals{
		GrandTotal: models.Money{
			Amount:   amount,
			Currency: spOrder.OrderTotal.CurrencyCode,
		},
	}

	return order
}

// convertSPAPIItemToOrderLine converts SP-API order item to internal format
func (h *AmazonOrdersHandler) convertSPAPIItemToOrderLine(item amazon.OrderItem) *models.OrderLine {
	line := &models.OrderLine{
		LineID:            item.OrderItemID,
		SKU:               item.SellerSKU,
		Title:             item.Title,
		Quantity:          item.QuantityOrdered,
		FulfilledQuantity: item.QuantityShipped,
		Status:            "pending",
		FulfilmentType:    "stock",
	}

	// Map pricing
	if item.ItemPrice != nil {
		var amount float64
		fmt.Sscanf(item.ItemPrice.Amount, "%f", &amount)
		line.UnitPrice = models.Money{
			Amount:   amount,
			Currency: item.ItemPrice.CurrencyCode,
		}

		// Calculate line total
		lineTotal := amount * float64(item.QuantityOrdered)
		
		if item.ItemTax != nil {
			var taxAmount float64
			fmt.Sscanf(item.ItemTax.Amount, "%f", &taxAmount)
			line.Tax = models.Money{
				Amount:   taxAmount,
				Currency: item.ItemTax.CurrencyCode,
			}
			lineTotal += taxAmount
		}

		line.LineTotal = models.Money{
			Amount:   lineTotal,
			Currency: item.ItemPrice.CurrencyCode,
		}
	}

	return line
}

// fetchOrdersFromAmazon - keep existing implementation as fallback
func (h *AmazonOrdersHandler) fetchOrdersFromAmazon(ctx context.Context, creds map[string]string, createdAfter, createdBefore time.Time) ([]AmazonOrder, error) {
	// Get access token
	accessToken, err := h.getAccessToken(creds)
	if err != nil {
		return nil, fmt.Errorf("failed to get access token: %w", err)
	}

	log.Printf("Got access token: %s...", accessToken[:20])

	// Determine endpoint based on region
	endpoint := h.getEndpointForRegion(creds["region"])
	log.Printf("Using endpoint: %s", endpoint)
	
	// Build URL
	apiURL := fmt.Sprintf("%s/orders/v0/orders", endpoint)
	
	// Build query parameters
	params := url.Values{}
	params.Set("MarketplaceIds", creds["marketplace_id"])
	params.Set("CreatedAfter", createdAfter.Format(time.RFC3339))
	if !createdBefore.IsZero() {
		params.Set("CreatedBefore", createdBefore.Format(time.RFC3339))
	}
	
	fullURL := fmt.Sprintf("%s?%s", apiURL, params.Encode())
	log.Printf("Request URL: %s", fullURL)
	log.Printf("MarketplaceIds: %s", creds["marketplace_id"])
	log.Printf("Date range: %s to %s", createdAfter.Format(time.RFC3339), createdBefore.Format(time.RFC3339))

	// Make request
	req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("x-amz-access-token", accessToken)
	req.Header.Set("Content-Type", "application/json")
	
	log.Printf("Request headers: x-amz-access-token=%s..., Content-Type=application/json", accessToken[:20])

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	log.Printf("Amazon API response status: %d", resp.StatusCode)
	log.Printf("Amazon API response body: %s", string(body))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Amazon API error: %d - %s", resp.StatusCode, string(body))
	}

	var ordersResp OrdersResponse
	if err := json.Unmarshal(body, &ordersResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(ordersResp.Errors) > 0 {
		return nil, fmt.Errorf("API error: %s - %s", ordersResp.Errors[0].Code, ordersResp.Errors[0].Message)
	}

	orders := ordersResp.Payload.Orders

	// Handle pagination if NextToken is present
	nextToken := ordersResp.Payload.NextToken
	for nextToken != "" {
		moreOrders, token, err := h.fetchOrdersWithToken(ctx, creds, accessToken, endpoint, nextToken)
		if err != nil {
			log.Printf("Error fetching next page: %v", err)
			break
		}
		orders = append(orders, moreOrders...)
		nextToken = token
	}

	return orders, nil
}

// fetchOrdersWithToken fetches next page of orders using NextToken
func (h *AmazonOrdersHandler) fetchOrdersWithToken(ctx context.Context, creds map[string]string, accessToken, endpoint, nextToken string) ([]AmazonOrder, string, error) {
	apiURL := fmt.Sprintf("%s/orders/v0/orders", endpoint)
	params := url.Values{}
	params.Set("NextToken", nextToken)
	
	fullURL := fmt.Sprintf("%s?%s", apiURL, params.Encode())

	req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
	if err != nil {
		return nil, "", err
	}

	req.Header.Set("x-amz-access-token", accessToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("Amazon API error: %d - %s", resp.StatusCode, string(body))
	}

	var ordersResp OrdersResponse
	if err := json.Unmarshal(body, &ordersResp); err != nil {
		return nil, "", err
	}

	return ordersResp.Payload.Orders, ordersResp.Payload.NextToken, nil
}

// fetchOrderItemsFromAmazon gets line items for an order
func (h *AmazonOrdersHandler) fetchOrderItemsFromAmazon(ctx context.Context, creds map[string]string, amazonOrderID string) ([]AmazonOrderItem, error) {
	// Get access token
	accessToken, err := h.getAccessToken(creds)
	if err != nil {
		return nil, fmt.Errorf("failed to get access token: %w", err)
	}

	// Determine endpoint
	endpoint := h.getEndpointForRegion(creds["region"])
	
	// Build URL
	apiURL := fmt.Sprintf("%s/orders/v0/orders/%s/orderItems", endpoint, amazonOrderID)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("x-amz-access-token", accessToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Amazon API error: %d - %s", resp.StatusCode, string(body))
	}

	var itemsResp OrderItemsResponse
	if err := json.Unmarshal(body, &itemsResp); err != nil {
		return nil, fmt.Errorf("failed to parse items response: %w", err)
	}

	if len(itemsResp.Errors) > 0 {
		return nil, fmt.Errorf("API error: %s - %s", itemsResp.Errors[0].Code, itemsResp.Errors[0].Message)
	}

	items := itemsResp.Payload.OrderItems

	// Handle pagination
	nextToken := itemsResp.Payload.NextToken
	for nextToken != "" {
		moreItems, token, err := h.fetchOrderItemsWithToken(ctx, accessToken, endpoint, amazonOrderID, nextToken)
		if err != nil {
			log.Printf("Error fetching next page of items: %v", err)
			break
		}
		items = append(items, moreItems...)
		nextToken = token
	}

	return items, nil
}

// fetchOrderItemsWithToken fetches next page of order items
func (h *AmazonOrdersHandler) fetchOrderItemsWithToken(ctx context.Context, accessToken, endpoint, amazonOrderID, nextToken string) ([]AmazonOrderItem, string, error) {
	apiURL := fmt.Sprintf("%s/orders/v0/orders/%s/orderItems", endpoint, amazonOrderID)
	params := url.Values{}
	params.Set("NextToken", nextToken)
	
	fullURL := fmt.Sprintf("%s?%s", apiURL, params.Encode())

	req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
	if err != nil {
		return nil, "", err
	}

	req.Header.Set("x-amz-access-token", accessToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("Amazon API error: %d - %s", resp.StatusCode, string(body))
	}

	var itemsResp OrderItemsResponse
	if err := json.Unmarshal(body, &itemsResp); err != nil {
		return nil, "", err
	}

	return itemsResp.Payload.OrderItems, itemsResp.Payload.NextToken, nil
}

// getAccessToken retrieves LWA access token
func (h *AmazonOrdersHandler) getAccessToken(creds map[string]string) (string, error) {
	tokenURL := "https://api.amazon.com/auth/o2/token"
	
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", creds["refresh_token"])
	data.Set("client_id", creds["lwa_client_id"])
	data.Set("client_secret", creds["lwa_client_secret"])

	req, err := http.NewRequest("POST", tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token request failed: %d - %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}

	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", err
	}

	return tokenResp.AccessToken, nil
}

// getEndpointForRegion returns the correct SP-API endpoint
func (h *AmazonOrdersHandler) getEndpointForRegion(region string) string {
	endpoints := map[string]string{
		"NA": "https://sellingpartnerapi-na.amazon.com",
		"EU": "https://sellingpartnerapi-eu.amazon.com",
		"FE": "https://sellingpartnerapi-fe.amazon.com",
		"US": "https://sellingpartnerapi-na.amazon.com",
		"UK": "https://sellingpartnerapi-eu.amazon.com",
		"DE": "https://sellingpartnerapi-eu.amazon.com",
		"FR": "https://sellingpartnerapi-eu.amazon.com",
		"IT": "https://sellingpartnerapi-eu.amazon.com",
		"ES": "https://sellingpartnerapi-eu.amazon.com",
		"JP": "https://sellingpartnerapi-fe.amazon.com",
	}

	if endpoint, ok := endpoints[region]; ok {
		return endpoint
	}
	return endpoints["US"] // Default to US
}

// convertAmazonOrderToInternal converts Amazon order to internal format
func (h *AmazonOrdersHandler) convertAmazonOrderToInternal(amazonOrder AmazonOrder, items []AmazonOrderItem, credentialID string) *models.Order {
	order := &models.Order{
		OrderID:          fmt.Sprintf("amz_%s", amazonOrder.AmazonOrderID),
		Channel:          "amazon",
		ChannelAccountID: credentialID,
		ExternalOrderID:  amazonOrder.AmazonOrderID,
		OrderDate:        amazonOrder.PurchaseDate,
		Status:           mapAmazonStatus(amazonOrder.OrderStatus),
		PaymentStatus:    "captured", // Amazon orders are pre-paid
		PaymentMethod:    amazonOrder.PaymentMethod,
		SLAAtRisk:        false,
		MarketplaceRegion: amazonOrder.MarketplaceID,
	}

	// Map customer
	if amazonOrder.BuyerInfo != nil {
		order.Customer = models.Customer{
			Name:  amazonOrder.BuyerInfo.BuyerName,
			Email: amazonOrder.BuyerInfo.BuyerEmail,
		}
	}

	// Map shipping address
	if amazonOrder.ShippingAddress != nil {
		order.ShippingAddress = models.Address{
			Name:         amazonOrder.ShippingAddress.Name,
			AddressLine1: amazonOrder.ShippingAddress.AddressLine1,
			AddressLine2: amazonOrder.ShippingAddress.AddressLine2,
			City:         amazonOrder.ShippingAddress.City,
			State:        amazonOrder.ShippingAddress.StateOrRegion,
			PostalCode:   amazonOrder.ShippingAddress.PostalCode,
			Country:      amazonOrder.ShippingAddress.CountryCode,
		}
	}

	// Calculate totals
	order.Totals = h.calculateAmazonTotals(amazonOrder, items)

	// Map promised ship date
	if amazonOrder.LatestShipDate != "" {
		order.PromisedShipBy = amazonOrder.LatestShipDate
	}

	return order
}

// convertAmazonItemToOrderLine converts Amazon order item to order line
func (h *AmazonOrdersHandler) convertAmazonItemToOrderLine(item AmazonOrderItem) *models.OrderLine {
	line := &models.OrderLine{
		LineID:            item.OrderItemID,
		SKU:               item.SellerSKU,
		Title:             item.Title,
		Quantity:          item.QuantityOrdered,
		FulfilledQuantity: item.QuantityShipped,
		Status:            "pending",
		FulfilmentType:    "stock",
	}

	// Map pricing
	if item.ItemPrice != nil {
		line.UnitPrice = models.Money{
			Amount:   parseAmazonMoney(item.ItemPrice.Amount),
			Currency: item.ItemPrice.CurrencyCode,
		}
		
		// Calculate line total
		lineTotal := parseAmazonMoney(item.ItemPrice.Amount) * float64(item.QuantityOrdered)
		
		if item.ItemTax != nil {
			line.Tax = models.Money{
				Amount:   parseAmazonMoney(item.ItemTax.Amount),
				Currency: item.ItemTax.CurrencyCode,
			}
			lineTotal += parseAmazonMoney(item.ItemTax.Amount)
		}

		line.LineTotal = models.Money{
			Amount:   lineTotal,
			Currency: item.ItemPrice.CurrencyCode,
		}
	}

	return line
}

// Helper functions
func mapAmazonStatus(amazonStatus string) string {
	statusMap := map[string]string{
		"Pending":          "imported",
		"Unshipped":        "processing",
		"PartiallyShipped": "processing",
		"Shipped":          "fulfilled",
		"Canceled":         "cancelled",
		"Unfulfillable":    "on_hold",
	}
	
	if status, ok := statusMap[amazonStatus]; ok {
		return status
	}
	return "imported"
}

func (h *AmazonOrdersHandler) calculateAmazonTotals(order AmazonOrder, items []AmazonOrderItem) models.OrderTotals {
	totals := models.OrderTotals{
		Subtotal: models.Money{
			Amount:   parseAmazonMoney(order.OrderTotal.Amount),
			Currency: order.OrderTotal.CurrencyCode,
		},
		GrandTotal: models.Money{
			Amount:   parseAmazonMoney(order.OrderTotal.Amount),
			Currency: order.OrderTotal.CurrencyCode,
		},
	}

	// Calculate from items for more accuracy
	var subtotal, tax, shipping, discount float64
	currency := order.OrderTotal.CurrencyCode
	
	for _, item := range items {
		if item.ItemPrice != nil {
			subtotal += parseAmazonMoney(item.ItemPrice.Amount) * float64(item.QuantityOrdered)
		}
		if item.ItemTax != nil {
			tax += parseAmazonMoney(item.ItemTax.Amount)
		}
		if item.ShippingPrice != nil {
			shipping += parseAmazonMoney(item.ShippingPrice.Amount)
		}
		if item.PromotionDiscount != nil {
			discount += parseAmazonMoney(item.PromotionDiscount.Amount)
		}
	}

	totals.Subtotal.Amount = subtotal
	totals.Subtotal.Currency = currency
	totals.Tax.Amount = tax
	totals.Tax.Currency = currency
	totals.Shipping.Amount = shipping
	totals.Shipping.Currency = currency
	totals.Discount.Amount = discount
	totals.Discount.Currency = currency

	return totals
}

func parseAmazonMoney(amount string) float64 {
	if amount == "" {
		return 0.0
	}
	var value float64
	fmt.Sscanf(amount, "%f", &value)
	return value
}

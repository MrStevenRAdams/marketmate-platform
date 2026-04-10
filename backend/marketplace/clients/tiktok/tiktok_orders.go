package tiktok

// ============================================================================
// TIKTOK SHOP ORDERS CLIENT
// ============================================================================
// Handles order fetching, acceptance, shipment confirmation, and cancellation.
// ============================================================================

import (
	"encoding/json"
	"fmt"
)

// ── Order Types ───────────────────────────────────────────────────────────────

// OrderStatus values used by TikTok Shop API
const (
	OrderStatusUnpaid       = "UNPAID"
	OrderStatusOnHold       = "ON_HOLD"
	OrderStatusAwaitingShip = "AWAITING_SHIPMENT"
	OrderStatusAwaitingColl = "AWAITING_COLLECTION"
	OrderStatusInTransit    = "IN_TRANSIT"
	OrderStatusDelivered    = "DELIVERED"
	OrderStatusCompleted    = "COMPLETED"
	OrderStatusCancelled    = "CANCELLED"
)

type OrderAddress struct {
	FullName       string `json:"full_name"`
	PhoneNumber    string `json:"phone_number"`
	AddressLine1   string `json:"address_line1"`
	AddressLine2   string `json:"address_line2"`
	City           string `json:"city"`
	State          string `json:"state"`
	PostalCode     string `json:"postal_code"`
	CountryCode    string `json:"region_code"`
	DistrictInfo   []struct {
		AddressLevel string `json:"address_level"`
		AddressName  string `json:"address_name"`
	} `json:"district_info,omitempty"`
}

type OrderLineItem struct {
	ID              string `json:"id"`
	ProductName     string `json:"product_name"`
	SKU             string `json:"seller_sku"`
	SkuID           string `json:"sku_id"`
	ProductID       string `json:"product_id"`
	Quantity        int    `json:"quantity"`
	OriginalPrice   string `json:"original_price"`
	SalePrice       string `json:"sale_price"`
	Currency        string `json:"currency"`
	SkuImage        string `json:"sku_image,omitempty"`
	PackageID       string `json:"package_id,omitempty"`
	TrackingNumber  string `json:"tracking_number,omitempty"`
	CancelReason    string `json:"cancel_reason,omitempty"`
	SkuType         string `json:"sku_type,omitempty"`
}

type Order struct {
	ID              string         `json:"id"`
	Status          string         `json:"status"`
	SubStatus       string         `json:"sub_status,omitempty"`
	CreateTime      int64          `json:"create_time"`
	UpdateTime      int64          `json:"update_time"`
	Currency        string         `json:"currency"`
	TotalAmount     string         `json:"payment_info,omitempty"`
	BuyerMessage    string         `json:"buyer_message,omitempty"`
	ShippingAddress OrderAddress   `json:"recipient_address"`
	LineItems       []OrderLineItem `json:"line_items"`
	PackageList     []struct {
		PackageID       string   `json:"id"`
		TrackingNumber  string   `json:"tracking_number,omitempty"`
		ShippingProvider string  `json:"shipping_provider_name,omitempty"`
		LineItemIDs     []string `json:"sku_list,omitempty"`
	} `json:"packages,omitempty"`
	PaymentInfo struct {
		Currency            string `json:"currency"`
		TotalAmount         string `json:"total_amount"`
		SubTotal            string `json:"sub_total,omitempty"`
		ShippingFee         string `json:"shipping_fee,omitempty"`
		PlatformDiscount    string `json:"platform_discount,omitempty"`
		SellerDiscount      string `json:"seller_discount,omitempty"`
	} `json:"payment_info,omitempty"`
	BuyerUID        string `json:"buyer_uid,omitempty"`
	ShopID          string `json:"shop_id,omitempty"`
	Region          string `json:"region,omitempty"`
	DeliveryOption  string `json:"delivery_option,omitempty"`
	ShippingType    string `json:"shipping_type,omitempty"`
}

// ── Fetch Orders ──────────────────────────────────────────────────────────────

// OrdersFilter defines the search criteria for fetching orders.
type OrdersFilter struct {
	UpdateTimeFrom int64    // Unix timestamp — search by update time range
	UpdateTimeTo   int64
	Status         string   // filter by order status (optional)
	PageToken      string   // for pagination
	PageSize       int      // max 100
}

type OrdersPage struct {
	Orders        []Order `json:"orders"`
	NextPageToken string  `json:"next_page_token"`
	Total         int     `json:"total_count"`
}

// GetOrders fetches orders with optional status filter and pagination.
func (c *Client) GetOrders(filter OrdersFilter) (*OrdersPage, error) {
	if filter.PageSize <= 0 || filter.PageSize > 100 {
		filter.PageSize = 100
	}

	body := map[string]interface{}{
		"update_time_from": filter.UpdateTimeFrom,
		"update_time_to":   filter.UpdateTimeTo,
		"page_size":        filter.PageSize,
	}
	if filter.Status != "" {
		body["order_status"] = filter.Status
	}

	params := map[string]string{}
	if filter.PageToken != "" {
		params["page_token"] = filter.PageToken
	}

	data, err := c.post("/api/v2/order/search", params, body)
	if err != nil {
		return nil, err
	}

	var page OrdersPage
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, fmt.Errorf("unmarshal orders page: %w", err)
	}
	return &page, nil
}

// GetOrderDetail fetches full details for specific order IDs (max 50 per call).
func (c *Client) GetOrderDetail(orderIDs []string) ([]Order, error) {
	if len(orderIDs) == 0 {
		return nil, nil
	}
	if len(orderIDs) > 50 {
		orderIDs = orderIDs[:50]
	}

	body := map[string]interface{}{
		"ids": orderIDs,
	}

	data, err := c.post("/api/v2/order/detail/query", nil, body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Orders []Order `json:"orders"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("unmarshal order detail: %w", err)
	}
	return result.Orders, nil
}

// FetchNewOrders fetches all orders updated in the given time window,
// auto-paginating until all pages are retrieved.
func (c *Client) FetchNewOrders(updateTimeFrom, updateTimeTo int64, status string) ([]Order, error) {
	var allOrders []Order
	pageToken := ""

	for {
		page, err := c.GetOrders(OrdersFilter{
			UpdateTimeFrom: updateTimeFrom,
			UpdateTimeTo:   updateTimeTo,
			Status:         status,
			PageToken:      pageToken,
			PageSize:       100,
		})
		if err != nil {
			return allOrders, fmt.Errorf("fetch orders page: %w", err)
		}

		allOrders = append(allOrders, page.Orders...)

		if page.NextPageToken == "" || len(page.Orders) == 0 {
			break
		}
		pageToken = page.NextPageToken
	}

	// Fetch detailed info for all orders (includes line items, addresses)
	if len(allOrders) > 0 {
		var ids []string
		for _, o := range allOrders {
			ids = append(ids, o.ID)
		}
		// Batch in groups of 50
		var detailed []Order
		for i := 0; i < len(ids); i += 50 {
			end := i + 50
			if end > len(ids) {
				end = len(ids)
			}
			batch, err := c.GetOrderDetail(ids[i:end])
			if err != nil {
				// Non-fatal — fall back to summary data
				detailed = append(detailed, allOrders[i:end]...)
				continue
			}
			detailed = append(detailed, batch...)
		}
		return detailed, nil
	}

	return allOrders, nil
}

// ── Shipment ──────────────────────────────────────────────────────────────────

// ShipPackageRequest is the payload for confirming shipment.
type ShipPackageRequest struct {
	OrderID        string `json:"order_id"`
	PackageID      string `json:"package_id,omitempty"`
	ShippingProvider string `json:"shipping_provider_id,omitempty"`
	ShippingProviderName string `json:"shipping_provider_name,omitempty"`
	TrackingNumber string `json:"tracking_number"`
	PickupType     int    `json:"pickup_type"` // 1=drop-off, 2=pickup
}

// ConfirmShipment marks an order package as shipped with a tracking number.
// For custom/3PL carriers, use TrackingNumber + ShippingProviderName.
func (c *Client) ConfirmShipment(req *ShipPackageRequest) error {
	body := map[string]interface{}{
		"order_id":        req.OrderID,
		"tracking_number": req.TrackingNumber,
		"pickup_type":     req.PickupType,
	}
	if req.PackageID != "" {
		body["package_id"] = req.PackageID
	}
	if req.ShippingProvider != "" {
		body["shipping_provider_id"] = req.ShippingProvider
	}
	if req.ShippingProviderName != "" {
		body["shipping_provider_name"] = req.ShippingProviderName
	}

	_, err := c.post("/api/v2/fulfillment/shipment/ship", nil, body)
	return err
}

// PushTracking is the high-level method used by the orders handler.
// It sets the tracking number and confirms shipment in one call.
func (c *Client) PushTracking(orderID, packageID, trackingNumber, carrier string) error {
	return c.ConfirmShipment(&ShipPackageRequest{
		OrderID:              orderID,
		PackageID:            packageID,
		TrackingNumber:       trackingNumber,
		ShippingProviderName: carrier,
		PickupType:           1, // drop-off
	})
}

// ── Cancellation ──────────────────────────────────────────────────────────────

// CancelOrder submits a cancellation request for an order.
func (c *Client) CancelOrder(orderID, cancelReason string, cancelReasonKey string) error {
	body := map[string]interface{}{
		"order_id":         orderID,
		"cancel_reason":    cancelReason,
		"cancel_reason_key": cancelReasonKey,
	}
	_, err := c.post("/api/v2/order/cancellation/create", nil, body)
	return err
}

// GetCancelReasons returns the available cancellation reason codes.
func (c *Client) GetCancelReasons() ([]map[string]interface{}, error) {
	data, err := c.get("/api/v2/order/cancellation/reasons", nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Reasons []map[string]interface{} `json:"reasons"`
	}
	json.Unmarshal(data, &result)
	return result.Reasons, nil
}

// ── Shipping Providers ────────────────────────────────────────────────────────

type ShippingProvider struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	SupportCOD  bool   `json:"support_cod"`
}

// GetShippingProviders returns available shipping providers for the region.
func (c *Client) GetShippingProviders() ([]ShippingProvider, error) {
	data, err := c.get("/api/v2/logistics/ship/providers", nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		Providers []ShippingProvider `json:"shipping_providers"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("unmarshal shipping providers: %w", err)
	}
	return result.Providers, nil
}

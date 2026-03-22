package temu

import (
	"encoding/json"
	"fmt"
	"log"
)

// ============================================================================
// TEMU ORDERS API (V2)
// ============================================================================

// Order represents a Temu parent order
type Order struct {
	ParentOrderSn               string        `json:"parentOrderSn"`
	ParentOrderStatus           int           `json:"parentOrderStatus"`
	ParentOrderTime             int64         `json:"parentOrderTime"`
	ParentConfirmTime           int64         `json:"parentConfirmTime"`
	ParentShippingTime          int64         `json:"parentShippingTime"`
	UpdateTime                  int64         `json:"updateTime"`
	ExpectShipLatestTime        int64         `json:"expectShipLatestTime"`
	LatestDeliveryTime          int64         `json:"latestDeliveryTime"`
	RegionID                    int           `json:"regionId"`
	SiteID                      int           `json:"siteId"`
	ShippingMethod              int           `json:"shippingMethod"`
	OrderPaymentType            string        `json:"orderPaymentType"`
	HasShippingFee              bool          `json:"hasShippingFee"`
	ParentOrderLabel            []OrderLabel  `json:"parentOrderLabel,omitempty"`
	FulfillmentWarning          []string      `json:"fulfillmentWarning,omitempty"`
	OrderList                   []OrderLine   `json:"orderList,omitempty"`
	// Shipping address fields (from detail API)
	RegionName1                 string        `json:"regionName1,omitempty"`
	RegionName2                 string        `json:"regionName2,omitempty"`
	RegionName3                 string        `json:"regionName3,omitempty"`
}

type OrderLabel struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

type OrderLine struct {
	OrderSN                        string       `json:"orderSn"`
	GoodsID                        int64        `json:"goodsId"`
	SkuID                          int64        `json:"skuId"`
	GoodsName                      string       `json:"goodsName"`
	OriginalGoodsName              string       `json:"originalGoodsName,omitempty"`
	ThumbURL                       string       `json:"thumbUrl"`
	Spec                           string       `json:"spec,omitempty"`
	Quantity                       int          `json:"quantity"`
	CanceledQuantityBeforeShipment int          `json:"canceledQuantityBeforeShipment,omitempty"`
	OrderStatus                    int          `json:"orderStatus"`
	OrderCreateTime                int64        `json:"orderCreateTime"`
	FulfillmentType                string       `json:"fulfillmentType"`
	OrderPaymentType               string       `json:"orderPaymentType"`
	IsCancelledDuringPending       bool         `json:"isCancelledDuringPending,omitempty"`
	PackageAbnormalTypeList        []string     `json:"packageAbnormalTypeList,omitempty"`
	OrderLabel                     []OrderLabel `json:"orderLabel,omitempty"`
	InventoryDeductionWarehouseID  string       `json:"inventoryDeductionWarehouseId,omitempty"`
}

type OrdersRequest struct {
	PageNumber                   int      `json:"pageNumber"`
	PageSize                     int      `json:"pageSize"`
	CreateAfter                  int64    `json:"createAfter,omitempty"`      // Unix timestamp
	CreateBefore                 int64    `json:"createBefore,omitempty"`     // Unix timestamp
	UpdateAtStart                int64    `json:"updateAtStart,omitempty"`
	UpdateAtEnd                  int64    `json:"updateAtEnd,omitempty"`
	ParentOrderStatus            int      `json:"parentOrderStatus,omitempty"`  // 0=all
	RegionID                     int      `json:"regionId,omitempty"`
	ParentOrderSnList            []string `json:"parentOrderSnList,omitempty"`
	SkuID                        int64    `json:"skuId,omitempty"`
	HasPreSaleOrder              bool     `json:"hasPreSaleOrder,omitempty"`
	HasQualificationRequiredOrder bool    `json:"hasQualificationRequiredOrder,omitempty"`
	Sortby                       string   `json:"sortby,omitempty"`
}

type OrdersResponse struct {
	TotalItemNum int         `json:"totalItemNum"`
	PageItems    []PageItem  `json:"pageItems"`
}

type PageItem struct {
	ParentOrderMap Order       `json:"parentOrderMap"`
	OrderList      []OrderLine `json:"orderList"`
}

type ShippingInfo struct {
	ReceiptName           string       `json:"receiptName"`
	ReceiptAdditionalName string       `json:"receiptAdditionalName,omitempty"`
	Mobile                string       `json:"mobile"`
	BackupMobile          string       `json:"backupMobile,omitempty"`
	Mail                  string       `json:"mail,omitempty"`
	RegionName1           string       `json:"regionName1"`  // Country
	RegionName2           string       `json:"regionName2"`  // State/Province
	RegionName3           string       `json:"regionName3"`  // City
	RegionName4           string       `json:"regionName4,omitempty"`
	AddressLine1          string       `json:"addressLine1"`
	AddressLine2          string       `json:"addressLine2,omitempty"`
	AddressLine3          string       `json:"addressLine3,omitempty"`
	AddressLineAll        string       `json:"addressLineAll,omitempty"`
	PostCode              string       `json:"postCode"`
	NationalAddress       string       `json:"nationalAddress,omitempty"`
	AddressExtra          *AddressExtra `json:"addressExtra,omitempty"`
}

type AddressExtra struct {
	FirstName            string `json:"firstName"`
	LastName             string `json:"lastName"`
	AdditionalFirstName  string `json:"additionalFirstName,omitempty"`
	AdditionalLastName   string `json:"additionalLastName,omitempty"`
}

// GetOrders fetches orders from Temu using V2 API
// Temu API type: "bg.order.list.v2.get"
func (c *Client) GetOrders(req OrdersRequest) (*OrdersResponse, error) {
	if req.PageSize <= 0 {
		req.PageSize = 50
	}
	if req.PageNumber <= 0 {
		req.PageNumber = 1
	}

	params := map[string]interface{}{
		"type":       "bg.order.list.v2.get",
		"pageNumber": req.PageNumber,
		"pageSize":   req.PageSize,
	}

	// Add optional filters
	if req.CreateAfter > 0 {
		params["createAfter"] = req.CreateAfter
	}
	if req.CreateBefore > 0 {
		params["createBefore"] = req.CreateBefore
	}
	if req.UpdateAtStart > 0 {
		params["updateAtStart"] = req.UpdateAtStart
	}
	if req.UpdateAtEnd > 0 {
		params["updateAtEnd"] = req.UpdateAtEnd
	}
	if req.ParentOrderStatus > 0 {
		params["parentOrderStatus"] = req.ParentOrderStatus
	}
	if req.RegionID > 0 {
		params["regionId"] = req.RegionID
	}
	if len(req.ParentOrderSnList) > 0 {
		params["parentOrderSnList"] = req.ParentOrderSnList
	}
	if req.SkuID > 0 {
		params["skuId"] = req.SkuID
	}
	if req.Sortby != "" {
		params["sortby"] = req.Sortby
	}

	// Log the full request for debugging with Temu support
	log.Printf("=== TEMU API REQUEST DEBUG ===")
	log.Printf("Request params: %+v", params)
	
	resp, err := c.Post(params)
	if err != nil {
		return nil, fmt.Errorf("API call failed: %w", err)
	}

	// Log the complete response including request ID
	log.Printf("=== TEMU API RESPONSE DEBUG ===")
	log.Printf("Success: %v", resp.Success)
	log.Printf("RequestID: %s", resp.RequestID)
	log.Printf("ErrorCode: %d", resp.ErrorCode)
	log.Printf("ErrorMsg: %s", resp.ErrorMsg)
	log.Printf("Result JSON: %s", string(resp.Result))
	log.Printf("=== END TEMU DEBUG ===")

	if !resp.Success {
		return nil, fmt.Errorf("Temu API error %d: %s (RequestID: %s)", resp.ErrorCode, resp.ErrorMsg, resp.RequestID)
	}
	
	var result OrdersResponse
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		log.Printf("[Temu GetOrders] Failed to unmarshal. Raw result: %s", string(resp.Result))
		return nil, fmt.Errorf("unmarshal orders response: %w", err)
	}
	
	log.Printf("[Temu GetOrders] Successfully parsed: %d total items, %d page items", result.TotalItemNum, len(result.PageItems))

	return &result, nil
}

// GetOrderDetail fetches detailed information for a single order using V2 API
// Temu API type: "bg.order.detail.v2.get"
func (c *Client) GetOrderDetail(parentOrderSN string) (*Order, error) {
	params := map[string]interface{}{
		"type":           "bg.order.detail.v2.get",
		"parentOrderSn":  parentOrderSN,
	}

	resp, err := c.Post(params)
	if err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("Temu API error %d: %s", resp.ErrorCode, resp.ErrorMsg)
	}

	var result struct {
		ParentOrderMap Order       `json:"parentOrderMap"`
		OrderList      []OrderLine `json:"orderList"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("unmarshal order detail: %w", err)
	}

	// Combine parent order with line items
	result.ParentOrderMap.OrderList = result.OrderList
	return &result.ParentOrderMap, nil
}

// GetShippingInfo fetches shipping address for an order
// Temu API type: "bg.order.shippinginfo.v2.get"
func (c *Client) GetShippingInfo(parentOrderSN string) (*ShippingInfo, error) {
	params := map[string]interface{}{
		"type":          "bg.order.shippinginfo.v2.get",
		"parentOrderSn": parentOrderSN,
	}

	resp, err := c.Post(params)
	if err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("Temu API error %d: %s", resp.ErrorCode, resp.ErrorMsg)
	}

	var shippingInfo ShippingInfo
	if err := json.Unmarshal(resp.Result, &shippingInfo); err != nil {
		return nil, fmt.Errorf("unmarshal shipping info: %w", err)
	}

	return &shippingInfo, nil
}

package tesco

import (
	"fmt"
	"time"
)

// ============================================================================
// TESCO ORDERS CLIENT
// ============================================================================

// Order represents a Tesco marketplace order
type Order struct {
	OrderID          string      `json:"orderId"`
	Status           string      `json:"status"`
	CreatedAt        string      `json:"createdAt"`
	CustomerName     string      `json:"customerName"`
	ShippingAddress  Address     `json:"shippingAddress"`
	Lines            []OrderLine `json:"lines"`
	TotalAmount      float64     `json:"totalAmount"`
	Currency         string      `json:"currency"`
}

// Address is a Tesco shipping address
type Address struct {
	Line1      string `json:"addressLine1"`
	Line2      string `json:"addressLine2"`
	City       string `json:"city"`
	PostalCode string `json:"postalCode"`
	Country    string `json:"country"`
}

// OrderLine is a line item in a Tesco order
type OrderLine struct {
	LineID      string  `json:"lineId"`
	GTIN        string  `json:"gtin"`
	Title       string  `json:"title"`
	Quantity    int     `json:"quantity"`
	UnitPrice   float64 `json:"unitPrice"`
	LineTotal   float64 `json:"lineTotal"`
	Currency    string  `json:"currency"`
}

// FetchNewOrders retrieves orders created between from and to
func (c *Client) FetchNewOrders(from, to time.Time) ([]Order, error) {
	var allOrders []Order
	page := 1
	for {
		resp, err := c.GetOrders(from, to, page)
		if err != nil {
			return nil, fmt.Errorf("fetch orders page %d: %w", page, err)
		}

		orders, ok := resp["orders"]
		if !ok {
			break
		}

		ordersSlice, ok := orders.([]interface{})
		if !ok || len(ordersSlice) == 0 {
			break
		}

		for _, raw := range ordersSlice {
			if m, ok := raw.(map[string]interface{}); ok {
				o := mapToOrder(m)
				allOrders = append(allOrders, o)
			}
		}

		// Check for more pages
		if totalPages, ok := resp["totalPages"].(float64); ok {
			if float64(page) >= totalPages {
				break
			}
		} else {
			break
		}
		page++
	}

	return allOrders, nil
}

// FetchOrderDetail retrieves a single order by ID
func (c *Client) FetchOrderDetail(orderID string) (*Order, error) {
	resp, err := c.do("GET", fmt.Sprintf("v1/orders/%s", orderID), nil)
	if err != nil {
		return nil, err
	}
	o := mapToOrder(resp)
	return &o, nil
}

// PushTracking sends tracking info to Tesco for an order
func (c *Client) PushTracking(orderID, trackingNumber, carrier string) error {
	_, err := c.UpdateShipment(orderID, map[string]interface{}{
		"trackingNumber": trackingNumber,
		"carrier":        carrier,
		"shippedAt":      time.Now().Format(time.RFC3339),
	})
	return err
}

// mapToOrder converts a raw API response map to an Order struct
func mapToOrder(m map[string]interface{}) Order {
	o := Order{}
	if v, ok := m["orderId"].(string); ok {
		o.OrderID = v
	}
	if v, ok := m["status"].(string); ok {
		o.Status = v
	}
	if v, ok := m["createdAt"].(string); ok {
		o.CreatedAt = v
	}
	if v, ok := m["customerName"].(string); ok {
		o.CustomerName = v
	}
	if v, ok := m["totalAmount"].(float64); ok {
		o.TotalAmount = v
	}
	if v, ok := m["currency"].(string); ok {
		o.Currency = v
	}
	if addr, ok := m["shippingAddress"].(map[string]interface{}); ok {
		o.ShippingAddress = Address{
			Line1:      strField(addr, "addressLine1"),
			Line2:      strField(addr, "addressLine2"),
			City:       strField(addr, "city"),
			PostalCode: strField(addr, "postalCode"),
			Country:    strField(addr, "country"),
		}
	}
	if lines, ok := m["lines"].([]interface{}); ok {
		for _, raw := range lines {
			if l, ok := raw.(map[string]interface{}); ok {
				line := OrderLine{
					LineID:   strField(l, "lineId"),
					GTIN:     strField(l, "gtin"),
					Title:    strField(l, "title"),
					Currency: strField(l, "currency"),
				}
				if v, ok := l["quantity"].(float64); ok {
					line.Quantity = int(v)
				}
				if v, ok := l["unitPrice"].(float64); ok {
					line.UnitPrice = v
				}
				if v, ok := l["lineTotal"].(float64); ok {
					line.LineTotal = v
				}
				o.Lines = append(o.Lines, line)
			}
		}
	}
	return o
}

func strField(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

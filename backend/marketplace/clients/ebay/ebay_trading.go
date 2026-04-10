package ebay

import (
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

// ============================================================================
// EBAY TRADING API CLIENT
// ============================================================================
// The Trading API is eBay's legacy XML-based API but it is the ONLY way to
// retrieve ALL seller listings including:
//   - Items with 0 stock
//   - Ended/unsold items
//   - Items created via Seller Hub, mobile app, or any other tool
//
// Uses OAuth tokens via the X-EBAY-API-IAF-TOKEN header (no separate
// Trading API auth needed).
//
// Key calls:
//   - GetUser: Get authenticated seller's username
//   - GetMyeBaySelling: Get all seller's listings (active, unsold, sold)
//   - GetItem: Get full details for a single item
// ============================================================================

// tradingAPIURL returns the correct Trading API endpoint
func (c *Client) tradingAPIURL() string {
	if strings.Contains(c.APIRoot, "sandbox") {
		return "https://api.sandbox.ebay.com/ws/api.dll"
	}
	return "https://api.ebay.com/ws/api.dll"
}

// tradingCall makes an authenticated Trading API XML call
func (c *Client) tradingCall(callName, xmlBody string) ([]byte, error) {
	if err := c.ensureValidToken(); err != nil {
		return nil, fmt.Errorf("token refresh: %w", err)
	}

	req, err := http.NewRequest("POST", c.tradingAPIURL(), strings.NewReader(xmlBody))
	if err != nil {
		return nil, err
	}

	c.mu.RLock()
	token := c.AccessToken
	c.mu.RUnlock()

	req.Header.Set("Content-Type", "text/xml")
	req.Header.Set("X-EBAY-API-IAF-TOKEN", token)
	req.Header.Set("X-EBAY-API-CALL-NAME", callName)
	req.Header.Set("X-EBAY-API-SITEID", "3") // UK
	req.Header.Set("X-EBAY-API-COMPATIBILITY-LEVEL", "1225")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s request failed: %w", callName, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s response: %w", callName, err)
	}

	return body, nil
}

// ============================================================================
// GetUser — returns the authenticated seller's eBay username
// ============================================================================

func (c *Client) TradingGetUser() (string, error) {
	xmlBody := `<?xml version="1.0" encoding="utf-8"?>
<GetUserRequest xmlns="urn:ebay:apis:eBLBaseComponents">
  <ErrorLanguage>en_US</ErrorLanguage>
  <WarningLevel>Low</WarningLevel>
</GetUserRequest>`

	body, err := c.tradingCall("GetUser", xmlBody)
	if err != nil {
		return "", err
	}

	var resp struct {
		XMLName xml.Name `xml:"GetUserResponse"`
		Ack     string   `xml:"Ack"`
		User    struct {
			UserID string `xml:"UserID"`
		} `xml:"User"`
		Errors struct {
			ShortMessage string `xml:"ShortMessage"`
			LongMessage  string `xml:"LongMessage"`
		} `xml:"Errors"`
	}

	if err := xml.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("parse GetUser: %w (body: %s)", err, truncate(string(body), 300))
	}

	if resp.Ack == "Failure" {
		return "", fmt.Errorf("GetUser failed: %s", resp.Errors.LongMessage)
	}

	log.Printf("[eBay Trading] GetUser: username=%s", resp.User.UserID)
	return resp.User.UserID, nil
}

// ============================================================================
// GetMyeBaySelling — returns ALL seller listings
// ============================================================================

// TradingSellingItem represents a single listing from GetMyeBaySelling
type TradingSellingItem struct {
	ItemID            string
	Title             string
	SKU               string
	CurrentPrice      float64
	Currency          string
	Quantity          int
	QuantityAvailable int
	QuantitySold      int
	ListingStatus     string // Active, Completed, Ended
	ListingType       string // FixedPriceItem, Chinese (auction)
	PrimaryCategory   TradingCategory
	PictureURL        []string
	ViewItemURL       string
	ConditionID       string
	ConditionName     string
	StartTime         string
	EndTime           string
	WatchCount        int
	HitCount          int
}

type TradingCategory struct {
	CategoryID   string
	CategoryName string
}

// TradingSellingPage is the paginated result from GetMyeBaySelling
type TradingSellingPage struct {
	Items      []TradingSellingItem
	TotalItems int
	PageNumber int
	TotalPages int
}

// GetMyeBaySelling fetches ALL seller listings (active, unsold, ended).
// This returns items regardless of stock level — crucial for inventory management.
//
// listType: "ActiveList", "UnsoldList", "SoldList", or "DeletedFromSoldList"
// For a full import, call with "ActiveList" first, then "UnsoldList" if needed.
func (c *Client) GetMyeBaySelling(listType string, pageNumber, entriesPerPage int) (*TradingSellingPage, error) {
	if entriesPerPage < 1 || entriesPerPage > 200 {
		entriesPerPage = 100
	}
	if pageNumber < 1 {
		pageNumber = 1
	}

	// Sort value depends on list type
	sortValue := "TimeLeft" // Valid for ActiveList
	switch listType {
	case "UnsoldList":
		sortValue = "EndTime"
	case "SoldList":
		sortValue = "EndTime"
	case "ScheduledList":
		sortValue = "StartTime"
	}

	// Build the XML request with the correct list type and pagination
	xmlBody := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<GetMyeBaySellingRequest xmlns="urn:ebay:apis:eBLBaseComponents">
  <ErrorLanguage>en_US</ErrorLanguage>
  <WarningLevel>Low</WarningLevel>
  <%s>
    <Sort>%s</Sort>
    <Pagination>
      <EntriesPerPage>%d</EntriesPerPage>
      <PageNumber>%d</PageNumber>
    </Pagination>
  </%s>
</GetMyeBaySellingRequest>`, listType, sortValue, entriesPerPage, pageNumber, listType)

	log.Printf("[eBay Trading] GetMyeBaySelling: listType=%s, page=%d, perPage=%d", listType, pageNumber, entriesPerPage)

	body, err := c.tradingCall("GetMyeBaySelling", xmlBody)
	if err != nil {
		return nil, err
	}

	// Log raw response (truncated)
	rawStr := string(body)
	if len(rawStr) > 1000 {
		rawStr = rawStr[:1000] + "..."
	}
	log.Printf("[eBay Trading] GetMyeBaySelling response (%d bytes): %s", len(body), rawStr)

	// Parse the response — the item list node name matches the listType
	return parseMyeBaySellingResponse(body, listType)
}

// parseMyeBaySellingResponse handles the XML parsing for GetMyeBaySelling
func parseMyeBaySellingResponse(body []byte, listType string) (*TradingSellingPage, error) {
	// Generic response structure
	var resp struct {
		XMLName    xml.Name `xml:"GetMyeBaySellingResponse"`
		Ack        string   `xml:"Ack"`
		ActiveList struct {
			ItemArray struct {
				Items []tradingXMLItem `xml:"Item"`
			} `xml:"ItemArray"`
			PaginationResult struct {
				TotalNumberOfEntries int `xml:"TotalNumberOfEntries"`
				TotalNumberOfPages   int `xml:"TotalNumberOfPages"`
			} `xml:"PaginationResult"`
		} `xml:"ActiveList"`
		UnsoldList struct {
			ItemArray struct {
				Items []tradingXMLItem `xml:"Item"`
			} `xml:"ItemArray"`
			PaginationResult struct {
				TotalNumberOfEntries int `xml:"TotalNumberOfEntries"`
				TotalNumberOfPages   int `xml:"TotalNumberOfPages"`
			} `xml:"PaginationResult"`
		} `xml:"UnsoldList"`
		SoldList struct {
			OrderTransactionArray struct {
				Items []struct {
					Transaction struct {
						Item tradingXMLItem `xml:"Item"`
					} `xml:"Transaction"`
				} `xml:"OrderTransaction"`
			} `xml:"OrderTransactionArray"`
			PaginationResult struct {
				TotalNumberOfEntries int `xml:"TotalNumberOfEntries"`
				TotalNumberOfPages   int `xml:"TotalNumberOfPages"`
			} `xml:"PaginationResult"`
		} `xml:"SoldList"`
		Errors struct {
			ShortMessage string `xml:"ShortMessage"`
			LongMessage  string `xml:"LongMessage"`
			ErrorCode    string `xml:"ErrorCode"`
		} `xml:"Errors"`
	}

	if err := xml.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse GetMyeBaySelling: %w (body: %s)", err, truncate(string(body), 500))
	}

	if resp.Ack == "Failure" {
		return nil, fmt.Errorf("GetMyeBaySelling failed [%s]: %s", resp.Errors.ErrorCode, resp.Errors.LongMessage)
	}

	page := &TradingSellingPage{PageNumber: 1}

	switch listType {
	case "ActiveList":
		page.TotalItems = resp.ActiveList.PaginationResult.TotalNumberOfEntries
		page.TotalPages = resp.ActiveList.PaginationResult.TotalNumberOfPages
		for _, xmlItem := range resp.ActiveList.ItemArray.Items {
			page.Items = append(page.Items, convertXMLItem(xmlItem, "Active"))
		}
	case "UnsoldList":
		page.TotalItems = resp.UnsoldList.PaginationResult.TotalNumberOfEntries
		page.TotalPages = resp.UnsoldList.PaginationResult.TotalNumberOfPages
		for _, xmlItem := range resp.UnsoldList.ItemArray.Items {
			page.Items = append(page.Items, convertXMLItem(xmlItem, "Ended"))
		}
	case "SoldList":
		page.TotalItems = resp.SoldList.PaginationResult.TotalNumberOfEntries
		page.TotalPages = resp.SoldList.PaginationResult.TotalNumberOfPages
		for _, ot := range resp.SoldList.OrderTransactionArray.Items {
			page.Items = append(page.Items, convertXMLItem(ot.Transaction.Item, "Completed"))
		}
	}

	log.Printf("[eBay Trading] GetMyeBaySelling: %s returned %d items (total=%d, pages=%d)",
		listType, len(page.Items), page.TotalItems, page.TotalPages)

	return page, nil
}

// tradingXMLItem is the raw XML structure for an eBay item
type tradingXMLItem struct {
	ItemID    string `xml:"ItemID"`
	Title     string `xml:"Title"`
	SKU       string `xml:"SKU"`
	ViewItemURL string `xml:"ViewItemURL"`
	WatchCount  int    `xml:"WatchCount"`
	HitCount    int    `xml:"HitCount"`
	Quantity    int    `xml:"Quantity"`
	QuantityAvailable int `xml:"QuantityAvailable"`
	SellingStatus struct {
		CurrentPrice struct {
			Value    string `xml:",chardata"`
			Currency string `xml:"currencyID,attr"`
		} `xml:"CurrentPrice"`
		QuantitySold int    `xml:"QuantitySold"`
		ListingStatus string `xml:"ListingStatus"`
	} `xml:"SellingStatus"`
	ListingType string `xml:"ListingType"`
	PrimaryCategory struct {
		CategoryID   string `xml:"CategoryID"`
		CategoryName string `xml:"CategoryName"`
	} `xml:"PrimaryCategory"`
	PictureDetails struct {
		PictureURL []string `xml:"PictureURL"`
	} `xml:"PictureDetails"`
	ConditionID          string `xml:"ConditionID"`
	ConditionDisplayName string `xml:"ConditionDisplayName"`
	ListingDetails struct {
		StartTime string `xml:"StartTime"`
		EndTime   string `xml:"EndTime"`
	} `xml:"ListingDetails"`
}

func convertXMLItem(x tradingXMLItem, defaultStatus string) TradingSellingItem {
	var price float64
	fmt.Sscanf(x.SellingStatus.CurrentPrice.Value, "%f", &price)

	status := x.SellingStatus.ListingStatus
	if status == "" {
		status = defaultStatus
	}

	return TradingSellingItem{
		ItemID:            x.ItemID,
		Title:             x.Title,
		SKU:               x.SKU,
		CurrentPrice:      price,
		Currency:          x.SellingStatus.CurrentPrice.Currency,
		Quantity:          x.Quantity,
		QuantityAvailable: x.QuantityAvailable,
		QuantitySold:      x.SellingStatus.QuantitySold,
		ListingStatus:     status,
		ListingType:       x.ListingType,
		PrimaryCategory: TradingCategory{
			CategoryID:   x.PrimaryCategory.CategoryID,
			CategoryName: x.PrimaryCategory.CategoryName,
		},
		PictureURL:    x.PictureDetails.PictureURL,
		ViewItemURL:   x.ViewItemURL,
		ConditionID:   x.ConditionID,
		ConditionName: x.ConditionDisplayName,
		StartTime:     x.ListingDetails.StartTime,
		EndTime:       x.ListingDetails.EndTime,
		WatchCount:    x.WatchCount,
		HitCount:      x.HitCount,
	}
}

// ============================================================================
// GetItem — full details for a single listing (description, specifics, etc.)
// ============================================================================

// TradingItemDetail holds full item details from GetItem
type TradingItemDetail struct {
	ItemID            string
	Title             string
	Description       string
	SKU               string
	CurrentPrice      float64
	Currency          string
	Quantity          int
	QuantityAvailable int
	QuantitySold      int
	ListingStatus     string
	ListingType       string
	CategoryID        string
	CategoryName      string
	CategoryPath      string
	PictureURL        []string
	ViewItemURL       string
	ConditionID       string
	ConditionName     string
	StartTime         string
	EndTime           string
	Brand             string
	MPN               string
	EAN               string
	UPC               string
	ISBN              string
	ItemSpecifics     map[string][]string // name → values
	Variations        []TradingVariation  // multi-variation listing data
}

// TradingVariation represents a single variation within a multi-variation listing
type TradingVariation struct {
	SKU               string
	Price             float64
	Currency          string
	Quantity          int
	QuantitySold      int
	QuantityAvailable int
	VariationSpecifics map[string]string // e.g. "Color": "Red", "Size": "Large"
	PictureURL        []string          // variation-specific images
}

// GetItem fetches full details for a single item via Trading API
func (c *Client) TradingGetItem(itemID string) (*TradingItemDetail, error) {
	xmlBody := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<GetItemRequest xmlns="urn:ebay:apis:eBLBaseComponents">
  <ErrorLanguage>en_US</ErrorLanguage>
  <WarningLevel>Low</WarningLevel>
  <ItemID>%s</ItemID>
  <DetailLevel>ReturnAll</DetailLevel>
  <IncludeItemSpecifics>true</IncludeItemSpecifics>
</GetItemRequest>`, itemID)

	body, err := c.tradingCall("GetItem", xmlBody)
	if err != nil {
		return nil, err
	}

	var resp struct {
		XMLName xml.Name `xml:"GetItemResponse"`
		Ack     string   `xml:"Ack"`
		Item    struct {
			ItemID    string `xml:"ItemID"`
			Title     string `xml:"Title"`
			Description string `xml:"Description"`
			SKU       string `xml:"SKU"`
			Quantity  int    `xml:"Quantity"`
			ListingType string `xml:"ListingType"`
			ViewItemURL string `xml:"ListingDetails>ViewItemURL"`
			StartTime   string `xml:"ListingDetails>StartTime"`
			EndTime     string `xml:"ListingDetails>EndTime"`
			SellingStatus struct {
				CurrentPrice struct {
					Value    string `xml:",chardata"`
					Currency string `xml:"currencyID,attr"`
				} `xml:"CurrentPrice"`
				QuantitySold  int    `xml:"QuantitySold"`
				ListingStatus string `xml:"ListingStatus"`
			} `xml:"SellingStatus"`
			PrimaryCategory struct {
				CategoryID   string `xml:"CategoryID"`
				CategoryName string `xml:"CategoryName"`
			} `xml:"PrimaryCategory"`
			PictureDetails struct {
				PictureURL []string `xml:"PictureURL"`
			} `xml:"PictureDetails"`
			ConditionID          string `xml:"ConditionID"`
			ConditionDisplayName string `xml:"ConditionDisplayName"`
			ItemSpecifics struct {
				NameValueList []struct {
					Name  string   `xml:"Name"`
					Value []string `xml:"Value"`
				} `xml:"NameValueList"`
			} `xml:"ItemSpecifics"`
			ProductListingDetails struct {
				BrandMPN struct {
					Brand string `xml:"Brand"`
					MPN   string `xml:"MPN"`
				} `xml:"BrandMPN"`
				EAN  string `xml:"EAN"`
				UPC  string `xml:"UPC"`
				ISBN string `xml:"ISBN"`
			} `xml:"ProductListingDetails"`
			Variations struct {
				Variation []struct {
					SKU           string `xml:"SKU"`
					StartPrice    struct {
						Value    string `xml:",chardata"`
						Currency string `xml:"currencyID,attr"`
					} `xml:"StartPrice"`
					Quantity      int `xml:"Quantity"`
					SellingStatus struct {
						QuantitySold int `xml:"QuantitySold"`
					} `xml:"SellingStatus"`
					VariationSpecifics struct {
						NameValueList []struct {
							Name  string `xml:"Name"`
							Value string `xml:"Value"`
						} `xml:"NameValueList"`
					} `xml:"VariationSpecifics"`
				} `xml:"Variation"`
				Pictures struct {
					VariationSpecificName string `xml:"VariationSpecificName"`
					VariationSpecificPictureSet []struct {
						VariationSpecificValue string   `xml:"VariationSpecificValue"`
						PictureURL             []string `xml:"PictureURL"`
					} `xml:"VariationSpecificPictureSet"`
				} `xml:"Pictures"`
			} `xml:"Variations"`
		} `xml:"Item"`
		Errors struct {
			ShortMessage string `xml:"ShortMessage"`
			LongMessage  string `xml:"LongMessage"`
		} `xml:"Errors"`
	}

	if err := xml.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse GetItem: %w", err)
	}

	if resp.Ack == "Failure" {
		return nil, fmt.Errorf("GetItem failed: %s", resp.Errors.LongMessage)
	}

	var price float64
	fmt.Sscanf(resp.Item.SellingStatus.CurrentPrice.Value, "%f", &price)

	item := &TradingItemDetail{
		ItemID:            resp.Item.ItemID,
		Title:             resp.Item.Title,
		Description:       resp.Item.Description,
		SKU:               resp.Item.SKU,
		CurrentPrice:      price,
		Currency:          resp.Item.SellingStatus.CurrentPrice.Currency,
		Quantity:          resp.Item.Quantity,
		QuantityAvailable: resp.Item.Quantity - resp.Item.SellingStatus.QuantitySold,
		QuantitySold:      resp.Item.SellingStatus.QuantitySold,
		ListingStatus:     resp.Item.SellingStatus.ListingStatus,
		ListingType:       resp.Item.ListingType,
		CategoryID:        resp.Item.PrimaryCategory.CategoryID,
		CategoryName:      resp.Item.PrimaryCategory.CategoryName,
		PictureURL:        resp.Item.PictureDetails.PictureURL,
		ViewItemURL:       resp.Item.ViewItemURL,
		ConditionID:       resp.Item.ConditionID,
		ConditionName:     resp.Item.ConditionDisplayName,
		StartTime:         resp.Item.StartTime,
		EndTime:           resp.Item.EndTime,
		Brand:             resp.Item.ProductListingDetails.BrandMPN.Brand,
		MPN:               resp.Item.ProductListingDetails.BrandMPN.MPN,
		EAN:               resp.Item.ProductListingDetails.EAN,
		UPC:               resp.Item.ProductListingDetails.UPC,
		ISBN:              resp.Item.ProductListingDetails.ISBN,
		ItemSpecifics:     make(map[string][]string),
		Variations:        []TradingVariation{},
	}

	for _, nv := range resp.Item.ItemSpecifics.NameValueList {
		item.ItemSpecifics[nv.Name] = nv.Value
	}

	// Parse variations
	// Build a map of variation-specific images: value → []URL
	variationImages := make(map[string][]string)
	if resp.Item.Variations.Pictures.VariationSpecificName != "" {
		for _, picSet := range resp.Item.Variations.Pictures.VariationSpecificPictureSet {
			variationImages[picSet.VariationSpecificValue] = picSet.PictureURL
		}
	}
	for _, v := range resp.Item.Variations.Variation {
		var vPrice float64
		fmt.Sscanf(v.StartPrice.Value, "%f", &vPrice)
		specs := make(map[string]string)
		var imageMatchValue string
		for _, nv := range v.VariationSpecifics.NameValueList {
			specs[nv.Name] = nv.Value
			// Match variation images by the VariationSpecificName
			if nv.Name == resp.Item.Variations.Pictures.VariationSpecificName {
				imageMatchValue = nv.Value
			}
		}
		tv := TradingVariation{
			SKU:                v.SKU,
			Price:              vPrice,
			Currency:           v.StartPrice.Currency,
			Quantity:           v.Quantity,
			QuantitySold:       v.SellingStatus.QuantitySold,
			QuantityAvailable:  v.Quantity - v.SellingStatus.QuantitySold,
			VariationSpecifics: specs,
			PictureURL:         variationImages[imageMatchValue],
		}
		item.Variations = append(item.Variations, tv)
	}

	// Extract brand from item specifics if not in ProductListingDetails
	if item.Brand == "" {
		if vals, ok := item.ItemSpecifics["Brand"]; ok && len(vals) > 0 {
			item.Brand = vals[0]
		}
	}
	if item.MPN == "" {
		if vals, ok := item.ItemSpecifics["MPN"]; ok && len(vals) > 0 {
			item.MPN = vals[0]
		}
	}

	return item, nil
}

// ============================================================================
// GetSellerList — bulk fetch of seller's listings with FULL item details
// ============================================================================
// Returns up to 200 items per page with DetailLevel=ReturnAll, including:
//   - Title, Description, Item Specifics, Variations, Images, Identifiers
// Rate limit: 300 calls per 15 seconds PER SELLER (user-level, not app-level)
// This replaces the GetMyeBaySelling + per-item GetItem pattern.

// SellerListPage holds one page of results from GetSellerList
type SellerListPage struct {
	Items      []*TradingItemDetail
	TotalItems int
	TotalPages int
	PageNumber int
}

// GetSellerList fetches a page of the seller's listings with full details.
// listFilter: "ActiveList", "EndedList", or "" for all.
func (c *Client) GetSellerList(pageNumber, entriesPerPage int, endTimeFrom, endTimeTo string) (*SellerListPage, error) {
	if entriesPerPage > 200 {
		entriesPerPage = 200 // max with DetailLevel=ReturnAll
	}

	xmlBody := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<GetSellerListRequest xmlns="urn:ebay:apis:eBLBaseComponents">
  <ErrorLanguage>en_US</ErrorLanguage>
  <WarningLevel>Low</WarningLevel>
  <DetailLevel>ReturnAll</DetailLevel>
  <IncludeVariations>true</IncludeVariations>
  <EndTimeFrom>%s</EndTimeFrom>
  <EndTimeTo>%s</EndTimeTo>
  <Pagination>
    <EntriesPerPage>%d</EntriesPerPage>
    <PageNumber>%d</PageNumber>
  </Pagination>
  <IncludeItemSpecifics>true</IncludeItemSpecifics>
</GetSellerListRequest>`, endTimeFrom, endTimeTo, entriesPerPage, pageNumber)

	log.Printf("[eBay Trading] GetSellerList: page=%d, perPage=%d, endTimeFrom=%s, endTimeTo=%s",
		pageNumber, entriesPerPage, endTimeFrom, endTimeTo)

	body, err := c.tradingCall("GetSellerList", xmlBody)
	if err != nil {
		return nil, err
	}

	return parseSellerListResponse(body)
}

func parseSellerListResponse(body []byte) (*SellerListPage, error) {
	var resp struct {
		XMLName         xml.Name `xml:"GetSellerListResponse"`
		Ack             string   `xml:"Ack"`
		PaginationResult struct {
			TotalNumberOfEntries int `xml:"TotalNumberOfEntries"`
			TotalNumberOfPages   int `xml:"TotalNumberOfPages"`
		} `xml:"PaginationResult"`
		PageNumber int `xml:"PageNumber"`
		ItemArray  struct {
			Items []sellerListXMLItem `xml:"Item"`
		} `xml:"ItemArray"`
		Errors struct {
			ShortMessage string `xml:"ShortMessage"`
			LongMessage  string `xml:"LongMessage"`
			ErrorCode    string `xml:"ErrorCode"`
		} `xml:"Errors"`
	}

	if err := xml.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse GetSellerList: %w (body: %s)", err, truncate(string(body), 500))
	}

	if resp.Ack == "Failure" {
		return nil, fmt.Errorf("GetSellerList failed [%s]: %s", resp.Errors.ErrorCode, resp.Errors.LongMessage)
	}

	page := &SellerListPage{
		TotalItems: resp.PaginationResult.TotalNumberOfEntries,
		TotalPages: resp.PaginationResult.TotalNumberOfPages,
		PageNumber: resp.PageNumber,
	}

	for _, xmlItem := range resp.ItemArray.Items {
		item := convertSellerListItem(xmlItem)
		page.Items = append(page.Items, item)
	}

	log.Printf("[eBay Trading] GetSellerList: page %d returned %d items (total=%d, pages=%d)",
		page.PageNumber, len(page.Items), page.TotalItems, page.TotalPages)

	return page, nil
}

// sellerListXMLItem mirrors the full Item XML from GetSellerList (same structure as GetItem)
type sellerListXMLItem struct {
	ItemID    string `xml:"ItemID"`
	Title     string `xml:"Title"`
	Description string `xml:"Description"`
	SKU       string `xml:"SKU"`
	Quantity  int    `xml:"Quantity"`
	ListingType string `xml:"ListingType"`
	ViewItemURL string `xml:"ListingDetails>ViewItemURL"`
	StartTime   string `xml:"ListingDetails>StartTime"`
	EndTime     string `xml:"ListingDetails>EndTime"`
	SellingStatus struct {
		CurrentPrice struct {
			Value    string `xml:",chardata"`
			Currency string `xml:"currencyID,attr"`
		} `xml:"CurrentPrice"`
		QuantitySold  int    `xml:"QuantitySold"`
		ListingStatus string `xml:"ListingStatus"`
	} `xml:"SellingStatus"`
	PrimaryCategory struct {
		CategoryID   string `xml:"CategoryID"`
		CategoryName string `xml:"CategoryName"`
	} `xml:"PrimaryCategory"`
	PictureDetails struct {
		PictureURL []string `xml:"PictureURL"`
	} `xml:"PictureDetails"`
	ConditionID          string `xml:"ConditionID"`
	ConditionDisplayName string `xml:"ConditionDisplayName"`
	ItemSpecifics struct {
		NameValueList []struct {
			Name  string   `xml:"Name"`
			Value []string `xml:"Value"`
		} `xml:"NameValueList"`
	} `xml:"ItemSpecifics"`
	ProductListingDetails struct {
		BrandMPN struct {
			Brand string `xml:"Brand"`
			MPN   string `xml:"MPN"`
		} `xml:"BrandMPN"`
		EAN  string `xml:"EAN"`
		UPC  string `xml:"UPC"`
		ISBN string `xml:"ISBN"`
	} `xml:"ProductListingDetails"`
	Variations struct {
		Variation []struct {
			SKU        string `xml:"SKU"`
			StartPrice struct {
				Value    string `xml:",chardata"`
				Currency string `xml:"currencyID,attr"`
			} `xml:"StartPrice"`
			Quantity      int `xml:"Quantity"`
			SellingStatus struct {
				QuantitySold int `xml:"QuantitySold"`
			} `xml:"SellingStatus"`
			VariationSpecifics struct {
				NameValueList []struct {
					Name  string `xml:"Name"`
					Value string `xml:"Value"`
				} `xml:"NameValueList"`
			} `xml:"VariationSpecifics"`
		} `xml:"Variation"`
		Pictures struct {
			VariationSpecificName       string `xml:"VariationSpecificName"`
			VariationSpecificPictureSet []struct {
				VariationSpecificValue string   `xml:"VariationSpecificValue"`
				PictureURL             []string `xml:"PictureURL"`
			} `xml:"VariationSpecificPictureSet"`
		} `xml:"Pictures"`
	} `xml:"Variations"`
}

func convertSellerListItem(x sellerListXMLItem) *TradingItemDetail {
	var price float64
	fmt.Sscanf(x.SellingStatus.CurrentPrice.Value, "%f", &price)

	item := &TradingItemDetail{
		ItemID:            x.ItemID,
		Title:             x.Title,
		Description:       x.Description,
		SKU:               x.SKU,
		CurrentPrice:      price,
		Currency:          x.SellingStatus.CurrentPrice.Currency,
		Quantity:          x.Quantity,
		QuantityAvailable: x.Quantity - x.SellingStatus.QuantitySold,
		QuantitySold:      x.SellingStatus.QuantitySold,
		ListingStatus:     x.SellingStatus.ListingStatus,
		ListingType:       x.ListingType,
		CategoryID:        x.PrimaryCategory.CategoryID,
		CategoryName:      x.PrimaryCategory.CategoryName,
		PictureURL:        x.PictureDetails.PictureURL,
		ViewItemURL:       x.ViewItemURL,
		ConditionID:       x.ConditionID,
		ConditionName:     x.ConditionDisplayName,
		StartTime:         x.StartTime,
		EndTime:           x.EndTime,
		Brand:             x.ProductListingDetails.BrandMPN.Brand,
		MPN:               x.ProductListingDetails.BrandMPN.MPN,
		EAN:               x.ProductListingDetails.EAN,
		UPC:               x.ProductListingDetails.UPC,
		ISBN:              x.ProductListingDetails.ISBN,
		ItemSpecifics:     make(map[string][]string),
		Variations:        []TradingVariation{},
	}

	// Item specifics
	for _, nv := range x.ItemSpecifics.NameValueList {
		item.ItemSpecifics[nv.Name] = nv.Value
	}

	// Variations + variation images
	variationImages := make(map[string][]string)
	if x.Variations.Pictures.VariationSpecificName != "" {
		for _, picSet := range x.Variations.Pictures.VariationSpecificPictureSet {
			variationImages[picSet.VariationSpecificValue] = picSet.PictureURL
		}
	}
	for _, v := range x.Variations.Variation {
		var vPrice float64
		fmt.Sscanf(v.StartPrice.Value, "%f", &vPrice)
		specs := make(map[string]string)
		var imageMatchValue string
		for _, nv := range v.VariationSpecifics.NameValueList {
			specs[nv.Name] = nv.Value
			if nv.Name == x.Variations.Pictures.VariationSpecificName {
				imageMatchValue = nv.Value
			}
		}
		item.Variations = append(item.Variations, TradingVariation{
			SKU:                v.SKU,
			Price:              vPrice,
			Currency:           v.StartPrice.Currency,
			Quantity:           v.Quantity,
			QuantitySold:       v.SellingStatus.QuantitySold,
			QuantityAvailable:  v.Quantity - v.SellingStatus.QuantitySold,
			VariationSpecifics: specs,
			PictureURL:         variationImages[imageMatchValue],
		})
	}

	// Extract brand/MPN from item specifics if not in ProductListingDetails
	if item.Brand == "" {
		if vals, ok := item.ItemSpecifics["Brand"]; ok && len(vals) > 0 {
			item.Brand = vals[0]
		}
	}
	if item.MPN == "" {
		if vals, ok := item.ItemSpecifics["MPN"]; ok && len(vals) > 0 {
			item.MPN = vals[0]
		}
	}

	return item
}

// ============================================================================
// SetNotificationPreferences — subscribe seller to all relevant eBay topics
// ============================================================================
// Subscribes the seller to cancellation, return, feedback, and message topics
// with JSON encoding so our webhook handler can parse them without XML/SOAP.
// Called automatically after OAuth connection.
//
// Topics subscribed:
//   CANCELLATION_CREATED — buyer requests cancellation
//   RETURN_CREATED       — buyer opens return
//   RETURN_CLOSED        — return case closed
//   AskSellerQuestion    — buyer sends message
//   FeedbackLeft         — buyer leaves feedback
//   FixedPriceTransaction — order placed
func (c *Client) SetNotificationPreferences(webhookURL string) error {
	xml := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<SetNotificationPreferencesRequest xmlns="urn:ebay:apis:eBLBaseComponents">
  <ApplicationDeliveryPreferences>
    <ApplicationEnable>Enable</ApplicationEnable>
    <ApplicationURL>%s</ApplicationURL>
    <DeviceType>Platform</DeviceType>
    <PayloadVersion>1173</PayloadVersion>
  </ApplicationDeliveryPreferences>
  <UserDeliveryPreferenceArray>
    <NotificationEnable>
      <EventType>CANCELLATION_CREATED</EventType>
      <EventEnable>Enable</EventEnable>
    </NotificationEnable>
    <NotificationEnable>
      <EventType>RETURN_CREATED</EventType>
      <EventEnable>Enable</EventEnable>
    </NotificationEnable>
    <NotificationEnable>
      <EventType>RETURN_CLOSED</EventType>
      <EventEnable>Enable</EventEnable>
    </NotificationEnable>
    <NotificationEnable>
      <EventType>AskSellerQuestion</EventType>
      <EventEnable>Enable</EventEnable>
    </NotificationEnable>
    <NotificationEnable>
      <EventType>FeedbackLeft</EventType>
      <EventEnable>Enable</EventEnable>
    </NotificationEnable>
    <NotificationEnable>
      <EventType>FixedPriceTransaction</EventType>
      <EventEnable>Enable</EventEnable>
    </NotificationEnable>
  </UserDeliveryPreferenceArray>
</SetNotificationPreferencesRequest>`, webhookURL)

	resp, err := c.tradingCall("SetNotificationPreferences", xml)
	if err != nil {
		return fmt.Errorf("SetNotificationPreferences: %w", err)
	}

	// Check for errors in response
	respStr := string(resp)
	if strings.Contains(respStr, "<Ack>Failure</Ack>") {
		// Extract error message
		start := strings.Index(respStr, "<ShortMessage>")
		end := strings.Index(respStr, "</ShortMessage>")
		if start >= 0 && end > start {
			return fmt.Errorf("SetNotificationPreferences failed: %s", respStr[start+14:end])
		}
		return fmt.Errorf("SetNotificationPreferences failed")
	}

	return nil
}

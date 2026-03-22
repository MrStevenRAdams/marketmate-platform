package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"module-a/marketplace/clients/ebay"
	"module-a/models"
)

// ============================================================================
// EBAY HANDLER — LISTING EXTENSIONS
// ============================================================================
// Additional endpoints for eBay listing creation:
//   POST /ebay/prepare              → Auto-map PIM product to eBay draft
//   POST /ebay/submit               → Create inventory item + offer + publish
//   GET  /ebay/categories/aspects   → Item specifics for a category
// ============================================================================

// ── Request / Response types ──

type EbayPrepareRequest struct {
	ProductID     string `json:"product_id" binding:"required"`
	CredentialID  string `json:"credential_id"`
	MarketplaceID string `json:"marketplace_id"` // EBAY_GB, EBAY_US, etc.
}

type EbayDraft struct {
	// Core
	Title                string              `json:"title"`
	Subtitle             string              `json:"subtitle"`
	Description          string              `json:"description"`
	Condition            string              `json:"condition"`
	ConditionDescription string              `json:"conditionDescription"`
	Brand                string              `json:"brand"`
	MPN                  string              `json:"mpn"`

	// Category
	CategoryID             string `json:"categoryId"`
	CategoryName           string `json:"categoryName"`
	SecondaryCategoryID    string `json:"secondaryCategoryId"`
	SecondaryCategoryName  string `json:"secondaryCategoryName"`

	// Aspects (item specifics)
	Aspects map[string][]string `json:"aspects"`

	// Pricing & Format
	ListingFormat              string `json:"listingFormat"`
	Price                      string `json:"price"`
	Currency                   string `json:"currency"`
	ReservePrice               string `json:"reservePrice"`
	BestOfferEnabled           bool   `json:"bestOfferEnabled"`
	BestOfferAutoAcceptPrice   string `json:"bestOfferAutoAcceptPrice"`
	BestOfferAutoDeclinePrice  string `json:"bestOfferAutoDeclinePrice"`
	VATPercentage              string `json:"vatPercentage"`

	// Inventory
	SKU      string `json:"sku"`
	Quantity string `json:"quantity"`
	LotSize  string `json:"lotSize"`

	// Images
	Images    []string `json:"images"`
	ImageAlts []string `json:"imageAlts"` // FLD-16: per-image alt text / captions

	// Policies
	FulfillmentPolicyID  string `json:"fulfillmentPolicyId"`
	PaymentPolicyID      string `json:"paymentPolicyId"`
	ReturnPolicyID       string `json:"returnPolicyId"`
	MerchantLocationKey  string `json:"merchantLocationKey"`

	// Package dimensions & weight
	PackageLength      string `json:"packageLength"`
	PackageWidth       string `json:"packageWidth"`
	PackageHeight      string `json:"packageHeight"`
	PackageWeightValue string `json:"packageWeightValue"`
	DimensionUnit      string `json:"dimensionUnit"`
	WeightUnit         string `json:"weightUnit"`
	PackageType        string `json:"packageType"`

	// Identifiers
	EAN  string `json:"ean"`
	UPC  string `json:"upc"`
	ISBN string `json:"isbn"`

	// Listing enhancements
	ListingDuration              string `json:"listingDuration"`
	PrivateListing               bool   `json:"privateListing"`
	ScheduledStartTime           string `json:"scheduledStartTime"`
	IncludeCatalogProductDetails bool   `json:"includeCatalogProductDetails"`

	// Marketplace
	MarketplaceID string `json:"marketplaceId"`

	// eBay Catalog (FLD-09)
	EPID string `json:"epid"`

	// GPSR — EU General Product Safety Regulation (FLD-07)
	GPSRManufacturerName        string `json:"gpsrManufacturerName"`
	GPSRManufacturerAddress     string `json:"gpsrManufacturerAddress"`
	GPSRResponsiblePersonName   string `json:"gpsrResponsiblePersonName"`
	GPSRResponsiblePersonContact string `json:"gpsrResponsiblePersonContact"`
	GPSRSafetyAttestation       bool   `json:"gpsrSafetyAttestation"`
	GPSRDocumentURLs            string `json:"gpsrDocumentUrls"`

	// Volume / Quantity pricing tiers (FLD-10)
	PricingTiers []EbayPricingTier `json:"pricingTiers"`

	// Promoted Listings (PRC-04)
	// Optional ad rate percentage (1–20) for COST_PER_SALE promoted listings.
	// If empty, promoted listing is not added to the offer.
	PromotedListingRate string `json:"promotedListingRate"`

	// FLD-01 — Payment methods annotation (stored in overrides; not sent to eBay API).
	// e.g. ["PayPal", "Credit/Debit Card", "Klarna"]
	PaymentMethods []string `json:"paymentMethods"`

	// FLD-02 — Bullet points (up to 5).
	// On submit these are prepended to the listing description as a <ul> HTML block.
	BulletPoints     []string `json:"bulletPoints"`
	ShortDescription string   `json:"shortDescription"` // Mobile-first summary (stored in overrides only)

	// Update context
	IsUpdate          bool   `json:"isUpdate"`
	ExistingOfferID   string `json:"existingOfferId"`
	ExistingListingID string `json:"existingListingId"`

	// VAR-01 — Variation listings (Session H).
	// When len(Variants) > 0 the submit handler creates an InventoryItemGroup
	// with one child inventory item per active variant. One group-level offer is
	// then published. When empty, the existing single-SKU flow is used.
	Variants []ChannelVariantDraft `json:"variants,omitempty"`
}

// EbayPricingTier defines a quantity-based price break (FLD-10)
type EbayPricingTier struct {
	MinQty       int    `json:"minQty"`
	PricePerUnit string `json:"pricePerUnit"`
}

type EbayPrepareResponse struct {
	OK                   bool                         `json:"ok"`
	Error                string                       `json:"error,omitempty"`
	Product              map[string]interface{}        `json:"product,omitempty"`
	Draft                *EbayDraft                   `json:"draft,omitempty"`
	CategorySuggestions  []ebay.CategorySuggestion    `json:"categorySuggestions,omitempty"`
	ItemAspects          []ebay.ItemAspect            `json:"itemAspects,omitempty"`
	FulfillmentPolicies  []ebay.FulfillmentPolicy     `json:"fulfillmentPolicies,omitempty"`
	PaymentPolicies      []ebay.PaymentPolicy         `json:"paymentPolicies,omitempty"`
	ReturnPolicies       []ebay.ReturnPolicy          `json:"returnPolicies,omitempty"`
	Locations            []ebay.InventoryLocation     `json:"locations,omitempty"`
	DebugErrors          []string                     `json:"debugErrors,omitempty"`
}

type EbaySubmitRequest struct {
	ProductID    string    `json:"product_id"`
	CredentialID string   `json:"credential_id"`
	Draft        EbayDraft `json:"draft"`
	Publish      bool      `json:"publish"` // if true, publish immediately after creating offer
}

type EbaySubmitResponse struct {
	OK                  bool     `json:"ok"`
	Error               string   `json:"error,omitempty"`
	OfferID             string   `json:"offerId,omitempty"`
	ListingID           string   `json:"listingId,omitempty"`
	Warnings            []string `json:"warnings,omitempty"`
	InventoryItemResult string   `json:"inventoryItemResult,omitempty"`
	OfferResult         string   `json:"offerResult,omitempty"`
	PublishResult       string   `json:"publishResult,omitempty"`
}

// ============================================================================
// POST /api/v1/ebay/prepare
// ============================================================================
// Loads PIM product, maps to eBay fields, fetches category suggestions,
// fetches item aspects, fetches business policies — returns pre-filled draft.

func (h *EbayHandler) PrepareEbayListing(c *gin.Context) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[eBay Prepare] PANIC RECOVERED: %v", r)
			c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("Internal panic: %v", r)})
		}
	}()

	tenantID := c.GetString("tenant_id")
	log.Printf("[eBay Prepare] START — tenant=%s", tenantID)

	var req EbayPrepareRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	marketplaceID := req.MarketplaceID
	if marketplaceID == "" {
		marketplaceID = "EBAY_GB"
	}

	var debugErrors []string

	// ── Step 1: Get eBay client ──
	if req.CredentialID != "" {
		c.Request.URL.RawQuery += "&credential_id=" + req.CredentialID
	}
	client, err := h.getEbayClient(c)
	if err != nil {
		log.Printf("[eBay Prepare] Client failed: %v", err)
		debugErrors = append(debugErrors, fmt.Sprintf("eBay client: %v", err))
	}

	// ── Step 2: Load PIM product ──
	log.Printf("[eBay Prepare] Loading PIM product %s...", req.ProductID)
	productModel, err := h.productRepo.GetProduct(c.Request.Context(), tenantID, req.ProductID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("product not found: %v", err)})
		return
	}
	productBytes, _ := json.Marshal(productModel)
	var product map[string]interface{}
	json.Unmarshal(productBytes, &product)
	log.Printf("[eBay Prepare] Product loaded: %s", extractString(product, "title"))

	// ── Step 3: Load extended data (from eBay import) ──
	var ebayRawData map[string]interface{}
	extData, err := h.repo.GetExtendedDataByProductID(c.Request.Context(), tenantID, req.ProductID)
	if err == nil && extData != nil {
		if dataField, ok := extData["data"].(map[string]interface{}); ok {
			ebayRawData = dataField
			log.Printf("[eBay Prepare] Extended data found with %d fields", len(ebayRawData))
		}
	}

	// ── Step 4: Build draft ──
	draft := buildEbayDraft(product, ebayRawData, marketplaceID)
	log.Printf("[eBay Prepare] Draft built: SKU=%s, title=%s", draft.SKU, draft.Title)

	// ── Step 5: Category suggestions ──
	var categorySuggestions []ebay.CategorySuggestion
	if client != nil && draft.Title != "" {
		log.Printf("[eBay Prepare] Fetching category suggestions for: %s", draft.Title)
		suggestions, err := client.GetCategorySuggestions(marketplaceID, draft.Title)
		if err != nil {
			debugErrors = append(debugErrors, fmt.Sprintf("category suggestions: %v", err))
		} else {
			categorySuggestions = suggestions
			if len(suggestions) > 0 && draft.CategoryID == "" {
				draft.CategoryID = suggestions[0].Category.CategoryID
				draft.CategoryName = suggestions[0].Category.CategoryName
				log.Printf("[eBay Prepare] Auto-selected category: %s (%s)", draft.CategoryName, draft.CategoryID)
			}
		}
	}

	// ── Step 6: Item aspects for selected category ──
	var itemAspects []ebay.ItemAspect
	if client != nil && draft.CategoryID != "" {
		log.Printf("[eBay Prepare] Fetching item aspects for category %s...", draft.CategoryID)
		aspects, err := client.GetItemAspectsForCategory(marketplaceID, draft.CategoryID)
		if err != nil {
			debugErrors = append(debugErrors, fmt.Sprintf("item aspects: %v", err))
		} else {
			itemAspects = aspects
			log.Printf("[eBay Prepare] Got %d item aspects", len(aspects))

			// Pre-fill aspects from PIM product attributes
			prefillAspects(draft, aspects, product)
		}
	}

	// ── Step 7: Business policies ──
	var fulfillmentPolicies []ebay.FulfillmentPolicy
	var paymentPolicies []ebay.PaymentPolicy
	var returnPolicies []ebay.ReturnPolicy
	if client != nil {
		log.Printf("[eBay Prepare] Fetching business policies for %s...", marketplaceID)
		fp, fpErr := client.GetFulfillmentPolicies(marketplaceID)
		pp, ppErr := client.GetPaymentPolicies(marketplaceID)
		rp, rpErr := client.GetReturnPolicies(marketplaceID)
		if fpErr != nil {
			debugErrors = append(debugErrors, fmt.Sprintf("fulfillment policies: %v", fpErr))
		} else {
			fulfillmentPolicies = fp
		}
		if ppErr != nil {
			debugErrors = append(debugErrors, fmt.Sprintf("payment policies: %v", ppErr))
		} else {
			paymentPolicies = pp
		}
		if rpErr != nil {
			debugErrors = append(debugErrors, fmt.Sprintf("return policies: %v", rpErr))
		} else {
			returnPolicies = rp
		}

		// Auto-select first policy if only one exists
		if len(fulfillmentPolicies) == 1 {
			draft.FulfillmentPolicyID = fulfillmentPolicies[0].FulfillmentPolicyID
		}
		if len(paymentPolicies) == 1 {
			draft.PaymentPolicyID = paymentPolicies[0].PaymentPolicyID
		}
		if len(returnPolicies) == 1 {
			draft.ReturnPolicyID = returnPolicies[0].ReturnPolicyID
		}
	}

	// ── Step 8: Inventory locations ──
	var locations []ebay.InventoryLocation
	if client != nil {
		locPage, err := client.GetInventoryLocations()
		if err != nil {
			debugErrors = append(debugErrors, fmt.Sprintf("locations: %v", err))
		} else if locPage != nil {
			locations = locPage.Locations
			if len(locations) == 1 {
				draft.MerchantLocationKey = locations[0].MerchantLocationKey
			}
		}
	}

	// ── Step 9: Check existing eBay listing for this product ──
	if client != nil && draft.SKU != "" {
		existingItem, err := client.GetInventoryItem(draft.SKU)
		if err == nil && existingItem != nil {
			draft.IsUpdate = true
			log.Printf("[eBay Prepare] Found existing inventory item for SKU=%s", draft.SKU)

			// Check for existing offers
			offerPage, err := client.GetOffers(draft.SKU)
			if err == nil && len(offerPage.Offers) > 0 {
				offer := offerPage.Offers[0]
				draft.ExistingOfferID = offer.OfferID
				if offer.Listing != nil && offer.Listing.ListingID != "" {
					draft.ExistingListingID = offer.Listing.ListingID
				}
				log.Printf("[eBay Prepare] Found existing offer %s", draft.ExistingOfferID)
			}
		}
	}

	// ── Step 10: Load PIM variants (VAR-01) ──
	fallbackImage := ""
	if len(draft.Images) > 0 {
		fallbackImage = draft.Images[0]
	}
	draft.Variants = loadChannelVariants(c.Request.Context(), h.productRepo, tenantID, req.ProductID, draft.Price, fallbackImage)
	log.Printf("[eBay Prepare] Loaded %d variants", len(draft.Variants))

	log.Printf("[eBay Prepare] DONE — debugErrors=%d", len(debugErrors))

	c.JSON(http.StatusOK, EbayPrepareResponse{
		OK:                  true,
		Product:             product,
		Draft:               draft,
		CategorySuggestions: categorySuggestions,
		ItemAspects:         itemAspects,
		FulfillmentPolicies: fulfillmentPolicies,
		PaymentPolicies:     paymentPolicies,
		ReturnPolicies:      returnPolicies,
		Locations:           locations,
		DebugErrors:         debugErrors,
	})
}

// ============================================================================
// POST /api/v1/ebay/submit
// ============================================================================
// Creates/updates inventory item → creates/updates offer → optionally publishes.
// Saves listing record to Firestore.

func (h *EbayHandler) SubmitEbayListing(c *gin.Context) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[eBay Submit] PANIC RECOVERED: %v", r)
			c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("Internal panic: %v", r)})
		}
	}()

	tenantID := c.GetString("tenant_id")

	var req EbaySubmitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	draft := req.Draft
	if draft.SKU == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "SKU is required"})
		return
	}

	// Get client
	if req.CredentialID != "" {
		c.Request.URL.RawQuery += "&credential_id=" + req.CredentialID
	}
	client, err := h.getEbayClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	var warnings []string
	resp := EbaySubmitResponse{OK: true}

	marketplaceID := draft.MarketplaceID
	if marketplaceID == "" {
		marketplaceID = "EBAY_GB"
	}
	currency := ebay.MarketplaceCurrency(marketplaceID)
	if draft.Currency != "" {
		currency = draft.Currency
	}

	// ── Check for variation listing (VAR-01) ──
	// Count active variants. If ≥2 are active, use the InventoryItemGroup flow.
	activeVariants := make([]ChannelVariantDraft, 0)
	for _, v := range draft.Variants {
		if v.Active {
			activeVariants = append(activeVariants, v)
		}
	}

	if len(activeVariants) >= 2 {
		// ── Variation Flow: N child inventory items + 1 group + 1 group offer ──
		groupKey := draft.SKU + "-group"
		log.Printf("[eBay Submit] Variation flow: %d active variants, groupKey=%s", len(activeVariants), groupKey)

		// Step 1: Create/replace each child inventory item
		childSKUs := make([]string, 0, len(activeVariants))
		for _, v := range activeVariants {
			childTitle := draft.Title
			if v.Title != "" {
				childTitle = v.Title
			}
			childDesc := draft.Description
			if v.Description != "" {
				childDesc = v.Description
			}
			childCondition := draft.Condition
			if v.Condition != "" {
				childCondition = v.Condition
			}
			childBrand := draft.Brand
			if v.Brand != "" {
				childBrand = v.Brand
			}

			childItem := &ebay.InventoryItem{
				Product: &ebay.Product{
					Title:       childTitle,
					Description: childDesc,
					Brand:       childBrand,
					MPN:         draft.MPN,
					Aspects:     buildVariantAspects(draft.Aspects, v.Combination),
				},
				Condition:            childCondition,
				ConditionDescription: draft.ConditionDescription,
				Availability: &ebay.Availability{
					ShipToLocationAvailability: &ebay.ShipToLocation{
						Quantity: parseIntOrDefault(v.Stock, 0),
					},
				},
				InventoryItemGroupKeys: []string{groupKey},
			}

			// Per-variant images: use v.Images if set, fall back to v.Image, then parent
			var childImages []string
			if len(v.Images) > 0 {
				childImages = v.Images
			} else if v.Image != "" {
				childImages = []string{v.Image}
			}
			if len(childImages) > 0 {
				childItem.Product.ImageURLs = childImages
			}

			if v.EAN != "" {
				childItem.Product.EAN = []string{v.EAN}
			}
			if draft.PackageWeightValue != "" {
				wv, _ := strconv.ParseFloat(draft.PackageWeightValue, 64)
				childItem.PackageWeightAndSize = &ebay.PackageWeightAndSize{
					Weight: &ebay.Weight{Value: wv, Unit: draft.WeightUnit},
				}
			}
			_, err := client.CreateOrReplaceInventoryItemFull(v.SKU, childItem)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("variant SKU %s: inventory item error: %v", v.SKU, err))
				continue
			}
			childSKUs = append(childSKUs, v.SKU)
			log.Printf("[eBay Submit] Variant inventory item created: SKU=%s", v.SKU)
		}
		if len(childSKUs) == 0 {
			c.JSON(http.StatusOK, EbaySubmitResponse{
				OK:    false,
				Error: "All variant inventory items failed to create",
			})
			return
		}

		// Step 2: Build and upsert the InventoryItemGroup
		group := buildInventoryItemGroup(groupKey, &draft, childSKUs)
		if err := client.CreateOrReplaceInventoryItemGroup(groupKey, group); err != nil {
			c.JSON(http.StatusOK, EbaySubmitResponse{
				OK:       false,
				Error:    fmt.Sprintf("Failed to create inventory item group: %v", err),
				Warnings: warnings,
			})
			return
		}
		resp.InventoryItemResult = fmt.Sprintf("group created with %d child SKUs", len(childSKUs))
		log.Printf("[eBay Submit] InventoryItemGroup created: %s", groupKey)

		// Step 3: Create group-level offer
		minPrice := findMinPrice(activeVariants, draft.Price)
		groupOffer := &ebay.GroupOffer{
			MarketplaceID:         marketplaceID,
			Format:                "FIXED_PRICE",
			ListingDescription:    draft.Description,
			InventoryItemGroupKey: groupKey,
			MerchantLocationKey:   draft.MerchantLocationKey,
			PricingSummary: &ebay.PricingSummary{
				Price: &ebay.Amount{Currency: currency, Value: minPrice},
			},
		}
		if draft.CategoryID != "" {
			groupOffer.CategoryID = draft.CategoryID
		}
		if draft.FulfillmentPolicyID != "" || draft.PaymentPolicyID != "" || draft.ReturnPolicyID != "" {
			groupOffer.ListingPolicies = &ebay.ListingPolicies{
				FulfillmentPolicyID: draft.FulfillmentPolicyID,
				PaymentPolicyID:     draft.PaymentPolicyID,
				ReturnPolicyID:      draft.ReturnPolicyID,
			}
		}
		offerID, err := client.CreateGroupOffer(groupOffer)
		if err != nil {
			c.JSON(http.StatusOK, EbaySubmitResponse{
				OK:       false,
				Error:    fmt.Sprintf("Failed to create group offer: %v", err),
				Warnings: warnings,
			})
			return
		}
		resp.OfferID = offerID
		resp.OfferResult = "group offer created"
		log.Printf("[eBay Submit] Group offer created: %s", offerID)

		// Step 4: Publish group offer if requested
		if req.Publish && offerID != "" {
			listingID, err := client.PublishOffer(offerID)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("Group offer created but publish failed: %v", err))
				resp.PublishResult = fmt.Sprintf("failed: %v", err)
			} else {
				resp.ListingID = listingID
				resp.PublishResult = "published"
				log.Printf("[eBay Submit] Group offer published, listingID=%s", listingID)
			}
		} else {
			resp.PublishResult = "skipped (draft only)"
		}

		resp.Warnings = warnings
		saveEbayListingToFirestore(c, h, tenantID, req, &draft, &resp)
		c.JSON(http.StatusOK, resp)
		return
	}

	// ── Single-SKU Flow (unchanged) ──

	// ── Step 1: Create/Update Inventory Item ──
	log.Printf("[eBay Submit] Step 1: Creating/updating inventory item SKU=%s", draft.SKU)
	inventoryItem := buildInventoryItem(&draft, currency)
	resultBody, err := client.CreateOrReplaceInventoryItemFull(draft.SKU, inventoryItem)
	if err != nil {
		c.JSON(http.StatusOK, EbaySubmitResponse{
			OK:                  false,
			Error:               fmt.Sprintf("Failed to create inventory item: %v", err),
			InventoryItemResult: resultBody,
		})
		return
	}
	resp.InventoryItemResult = "OK"
	log.Printf("[eBay Submit] Step 1 OK: inventory item created/updated")

	// ── Step 2: Create or Update Offer ──
	offer := buildExtendedOffer(&draft, marketplaceID, currency)

	if draft.ExistingOfferID != "" {
		// Update existing offer
		log.Printf("[eBay Submit] Step 2: Updating existing offer %s", draft.ExistingOfferID)
		err = client.UpdateExtendedOffer(draft.ExistingOfferID, offer)
		if err != nil {
			c.JSON(http.StatusOK, EbaySubmitResponse{
				OK:    false,
				Error: fmt.Sprintf("Failed to update offer: %v", err),
			})
			return
		}
		resp.OfferID = draft.ExistingOfferID
		resp.OfferResult = "updated"
		log.Printf("[eBay Submit] Step 2 OK: offer updated")
	} else {
		// Create new offer
		log.Printf("[eBay Submit] Step 2: Creating new offer for SKU=%s", draft.SKU)
		offerID, err := client.CreateExtendedOffer(offer)
		if err != nil {
			c.JSON(http.StatusOK, EbaySubmitResponse{
				OK:    false,
				Error: fmt.Sprintf("Failed to create offer: %v", err),
			})
			return
		}
		resp.OfferID = offerID
		resp.OfferResult = "created"
		log.Printf("[eBay Submit] Step 2 OK: offer created, ID=%s", offerID)
	}

	// ── Step 3: Publish (if requested) ──
	if req.Publish && resp.OfferID != "" {
		log.Printf("[eBay Submit] Step 3: Publishing offer %s", resp.OfferID)
		listingID, err := client.PublishOffer(resp.OfferID)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("Offer created but publish failed: %v", err))
			resp.PublishResult = fmt.Sprintf("failed: %v", err)
		} else {
			resp.ListingID = listingID
			resp.PublishResult = "published"
			log.Printf("[eBay Submit] Step 3 OK: published, listingID=%s", listingID)
		}
	} else {
		resp.PublishResult = "skipped (draft only)"
	}

	resp.Warnings = warnings

	// ── Step 4: Save listing to Firestore ──
	saveEbayListingToFirestore(c, h, tenantID, req, &draft, &resp)
	c.JSON(http.StatusOK, resp)
}

// ── VAR-01 helper: saveEbayListingToFirestore ────────────────────────────────
// Shared between the single-SKU and InventoryItemGroup submit paths.
func saveEbayListingToFirestore(c *gin.Context, h *EbayHandler, tenantID string, req EbaySubmitRequest, draft *EbayDraft, resp *EbaySubmitResponse) {
	now := time.Now()
	state := "draft"
	if resp.ListingID != "" {
		state = "published"
	} else if resp.OfferID != "" {
		state = "ready"
	}
	marketplaceID := draft.MarketplaceID
	if marketplaceID == "" {
		marketplaceID = "EBAY_GB"
	}
	credentialID := req.CredentialID
	if credentialID == "" {
		creds, _ := h.repo.ListCredentials(c.Request.Context(), tenantID)
		for _, cred := range creds {
			if cred.Channel == "ebay" && cred.Active {
				credentialID = cred.CredentialID
				break
			}
		}
	}
	existingListing, _ := h.repo.FindListingByProductAndAccount(c.Request.Context(), tenantID, req.ProductID, credentialID)
	if existingListing != nil {
		existingListing.State = state
		existingListing.ChannelIdentifiers = &models.ChannelIdentifiers{
			SKU:       draft.SKU,
			ListingID: resp.ListingID,
		}
		existingListing.MarketplaceID = marketplaceID
		existingListing.UpdatedAt = now
		if state == "published" {
			existingListing.LastPublishedAt = &now
		}
		if err := h.repo.UpdateListing(c.Request.Context(), existingListing); err != nil {
			log.Printf("[eBay Submit] WARNING: Firestore update failed: %v", err)
		}
	} else {
		listing := &models.Listing{
			ListingID:        fmt.Sprintf("ebay-%s-%d", draft.SKU, now.Unix()),
			TenantID:         tenantID,
			ProductID:        req.ProductID,
			Channel:          "ebay",
			ChannelAccountID: credentialID,
			MarketplaceID:    marketplaceID,
			State:            state,
			ChannelIdentifiers: &models.ChannelIdentifiers{
				SKU:       draft.SKU,
				ListingID: resp.ListingID,
			},
			Overrides: &models.ListingOverrides{
				Title:           draft.Title,
				CategoryMapping: draft.CategoryID,
				Attributes: func() map[string]interface{} {
					m := map[string]interface{}{}
					if draft.ShortDescription != "" {
						m["short_description"] = draft.ShortDescription
					}
					if len(draft.PaymentMethods) > 0 {
						m["payment_methods"] = draft.PaymentMethods
					}
					if len(m) == 0 {
						return nil
					}
					return m
				}(),
			},
			CreatedAt: now,
			UpdatedAt: now,
		}
		if state == "published" {
			listing.LastPublishedAt = &now
		}
		if err := h.repo.CreateListing(c.Request.Context(), listing); err != nil {
			log.Printf("[eBay Submit] WARNING: Firestore create failed: %v", err)
		}
	}
}

// buildInventoryItemGroup constructs an eBay InventoryItemGroup from the draft.
func buildInventoryItemGroup(groupKey string, draft *EbayDraft, childSKUs []string) *ebay.InventoryItemGroup {
	group := &ebay.InventoryItemGroup{
		InventoryItemGroupKey: groupKey,
		Title:                 draft.Title,
		Description:           draft.Description,
		Aspects:               draft.Aspects,
		ImageUrls:             draft.Images,
	}
	// Build VariesBy from the combination keys of the first active variant
	// (all variants should share the same combination keys)
	if len(draft.Variants) > 0 {
		specs := []ebay.VariationSpec{}
		// Collect all unique values per attribute key
		keyValues := map[string][]string{}
		for _, v := range draft.Variants {
			if !v.Active {
				continue
			}
			for k, val := range v.Combination {
				keyValues[k] = appendUnique(keyValues[k], val)
			}
		}
		for k, vals := range keyValues {
			specs = append(specs, ebay.VariationSpec{Name: k, Values: vals})
		}
		if len(specs) > 0 {
			group.VariesBy = &ebay.VariesBy{Specifications: specs}
		}
	}
	_ = childSKUs // childSKUs are referenced by the inventory items via InventoryItemGroupKeys
	return group
}

// buildVariantAspects merges the product-level aspects with this variant's combination values.
func buildVariantAspects(productAspects map[string][]string, combination map[string]string) map[string][]string {
	result := map[string][]string{}
	for k, v := range productAspects {
		result[k] = v
	}
	for k, v := range combination {
		result[k] = []string{v}
	}
	return result
}

// findMinPrice returns the lowest price string among active variants (used as the group offer price).
func findMinPrice(variants []ChannelVariantDraft, fallback string) string {
	min := -1.0
	for _, v := range variants {
		if !v.Active {
			continue
		}
		p, err := strconv.ParseFloat(v.Price, 64)
		if err != nil || p <= 0 {
			continue
		}
		if min < 0 || p < min {
			min = p
		}
	}
	if min > 0 {
		return fmt.Sprintf("%.2f", min)
	}
	return fallback
}

// appendUnique appends val to slice only if not already present.
func appendUnique(slice []string, val string) []string {
	for _, s := range slice {
		if s == val {
			return slice
		}
	}
	return append(slice, val)
}

// parseIntOrDefault parses a string to int, returning def on error.
func parseIntOrDefault(s string, def int) int {
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return v
}

// ============================================================================
// GET /api/v1/ebay/categories/aspects
// ============================================================================
// Returns item specifics (aspects) for a category from the Taxonomy API.

func (h *EbayHandler) GetItemAspects(c *gin.Context) {
	client, err := h.getEbayClient(c)
	if err != nil {
		c.JSON(400, gin.H{"ok": false, "error": err.Error()})
		return
	}

	categoryID := c.Query("category_id")
	marketplace := c.DefaultQuery("marketplace", "EBAY_GB")

	if categoryID == "" {
		c.JSON(400, gin.H{"ok": false, "error": "category_id is required"})
		return
	}

	aspects, err := client.GetItemAspectsForCategory(marketplace, categoryID)
	if err != nil {
		c.JSON(500, gin.H{"ok": false, "error": err.Error()})
		return
	}

	treeID := ebay.GetTreeIDPublic(marketplace)

	c.JSON(200, gin.H{
		"ok":             true,
		"aspects":        aspects,
		"categoryId":     categoryID,
		"categoryTreeId": treeID,
	})
}

// ============================================================================
// DRAFT BUILDER
// ============================================================================

func buildEbayDraft(product map[string]interface{}, ebayData map[string]interface{}, marketplaceID string) *EbayDraft {
	currency := ebay.MarketplaceCurrency(marketplaceID)

	draft := &EbayDraft{
		Title:         extractString(product, "title"),
		Description:   extractString(product, "description"),
		Brand:         extractString(product, "brand"),
		SKU:           extractString(product, "sku"),
		Condition:     "NEW",
		ListingFormat: "FIXED_PRICE",
		Currency:      currency,
		MarketplaceID: marketplaceID,
		DimensionUnit: "CENTIMETER",
		WeightUnit:    "KILOGRAM",
		PackageType:   "PACKAGE_THICK_ENVELOPE",
		ListingDuration: "GTC",
		Quantity:      "1",
		Images:         []string{},
		Aspects:        map[string][]string{},
		BulletPoints:   []string{},
		PaymentMethods: []string{},
		IncludeCatalogProductDetails: true,
	}

	// Subtitle from PIM
	if sub := extractString(product, "subtitle"); sub != "" {
		draft.Subtitle = sub
	}

	// Price from PIM
	if price, ok := product["price"].(float64); ok && price > 0 {
		draft.Price = fmt.Sprintf("%.2f", price)
	}

	// Images from PIM assets
	if assets, ok := product["assets"].([]interface{}); ok {
		for _, a := range assets {
			if m, ok := a.(map[string]interface{}); ok {
				if u, ok := m["url"].(string); ok && u != "" {
					draft.Images = append(draft.Images, u)
				}
			}
			if s, ok := a.(string); ok && s != "" {
				draft.Images = append(draft.Images, s)
			}
		}
	}
	if imgs, ok := product["images"].([]interface{}); ok && len(draft.Images) == 0 {
		for _, img := range imgs {
			if s, ok := img.(string); ok && s != "" {
				draft.Images = append(draft.Images, s)
			}
		}
	}

	// Identifiers from PIM
	if identifiers, ok := product["identifiers"].(map[string]interface{}); ok {
		if ean := extractString(identifiers, "ean"); ean != "" {
			draft.EAN = ean
		}
		if upc := extractString(identifiers, "upc"); upc != "" {
			draft.UPC = upc
		}
		if isbn := extractString(identifiers, "isbn"); isbn != "" {
			draft.ISBN = isbn
		}
		if mpn := extractString(identifiers, "mpn"); mpn != "" {
			draft.MPN = mpn
		}
	}
	// Also try top-level ean/barcode
	if draft.EAN == "" {
		if ean := extractString(product, "ean"); ean != "" {
			draft.EAN = ean
		}
	}
	if draft.EAN == "" {
		if barcode := extractString(product, "barcode"); barcode != "" {
			draft.EAN = barcode
		}
	}

	// Dimensions from PIM
	if dims, ok := product["dimensions"].(map[string]interface{}); ok {
		if l, ok := dims["length"].(float64); ok {
			draft.PackageLength = fmt.Sprintf("%.1f", l)
		}
		if w, ok := dims["width"].(float64); ok {
			draft.PackageWidth = fmt.Sprintf("%.1f", w)
		}
		if h, ok := dims["height"].(float64); ok {
			draft.PackageHeight = fmt.Sprintf("%.1f", h)
		}
	}
	if w, ok := product["weight"].(map[string]interface{}); ok {
		if val, ok := w["value"].(float64); ok {
			draft.PackageWeightValue = fmt.Sprintf("%.2f", val)
		}
	}

	// Brand → aspects
	if draft.Brand != "" {
		draft.Aspects["Brand"] = []string{draft.Brand}
	}
	if draft.MPN != "" {
		draft.Aspects["MPN"] = []string{draft.MPN}
	}

	// PIM attributes → aspects
	if attrs, ok := product["attributes"].(map[string]interface{}); ok {
		for key, val := range attrs {
			aspectName := toAspectName(key)
			switch v := val.(type) {
			case string:
				if v != "" {
					draft.Aspects[aspectName] = []string{v}
				}
			case []interface{}:
				var vals []string
				for _, item := range v {
					if s, ok := item.(string); ok && s != "" {
						vals = append(vals, s)
					}
				}
				if len(vals) > 0 {
					draft.Aspects[aspectName] = vals
				}
			}
		}
	}

	// Enrich from eBay extended data (from previous import)
	if ebayData != nil {
		enrichFromEbayData(draft, ebayData)
	}

	return draft
}

// enrichFromEbayData overlays data from a previous eBay import
func enrichFromEbayData(draft *EbayDraft, data map[string]interface{}) {
	if product, ok := data["product"].(map[string]interface{}); ok {
		if title, ok := product["title"].(string); ok && title != "" {
			draft.Title = title
		}
		if desc, ok := product["description"].(string); ok && desc != "" {
			draft.Description = desc
		}
		if brand, ok := product["brand"].(string); ok && brand != "" {
			draft.Brand = brand
			draft.Aspects["Brand"] = []string{brand}
		}
		if mpn, ok := product["mpn"].(string); ok && mpn != "" {
			draft.MPN = mpn
			draft.Aspects["MPN"] = []string{mpn}
		}
		if aspects, ok := product["aspects"].(map[string]interface{}); ok {
			for key, val := range aspects {
				if vals, ok := val.([]interface{}); ok {
					var strVals []string
					for _, v := range vals {
						if s, ok := v.(string); ok && s != "" {
							strVals = append(strVals, s)
						}
					}
					if len(strVals) > 0 {
						draft.Aspects[key] = strVals
					}
				}
			}
		}
		// Images from extended data
		if imgs, ok := product["imageUrls"].([]interface{}); ok && len(imgs) > 0 && len(draft.Images) == 0 {
			for _, img := range imgs {
				if s, ok := img.(string); ok && s != "" {
					draft.Images = append(draft.Images, s)
				}
			}
		}
		// EAN from extended data
		if eans, ok := product["ean"].([]interface{}); ok && len(eans) > 0 && draft.EAN == "" {
			if s, ok := eans[0].(string); ok {
				draft.EAN = s
			}
		}
		if upcs, ok := product["upc"].([]interface{}); ok && len(upcs) > 0 && draft.UPC == "" {
			if s, ok := upcs[0].(string); ok {
				draft.UPC = s
			}
		}
	}

	// Condition from extended data
	if cond, ok := data["condition"].(string); ok && cond != "" {
		draft.Condition = strings.ToUpper(cond)
	}
}

// prefillAspects pre-fills required aspects from PIM product data
func prefillAspects(draft *EbayDraft, aspects []ebay.ItemAspect, product map[string]interface{}) {
	attrs, _ := product["attributes"].(map[string]interface{})
	if attrs == nil {
		return
	}

	for _, aspect := range aspects {
		name := aspect.LocalizedAspectName
		if _, exists := draft.Aspects[name]; exists {
			continue // already set
		}

		// Try to match PIM attribute keys to aspect names
		normalizedAspectName := strings.ToLower(strings.ReplaceAll(name, " ", "_"))
		for key, val := range attrs {
			normalizedKey := strings.ToLower(strings.ReplaceAll(key, " ", "_"))
			if normalizedKey == normalizedAspectName {
				switch v := val.(type) {
				case string:
					if v != "" {
						draft.Aspects[name] = []string{v}
					}
				case []interface{}:
					var vals []string
					for _, item := range v {
						if s, ok := item.(string); ok && s != "" {
							vals = append(vals, s)
						}
					}
					if len(vals) > 0 {
						draft.Aspects[name] = vals
					}
				}
				break
			}
		}
	}
}

// toAspectName converts a snake_case PIM key to Title Case for eBay aspects
func toAspectName(key string) string {
	words := strings.Split(strings.ReplaceAll(key, "-", "_"), "_")
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + strings.ToLower(w[1:])
		}
	}
	return strings.Join(words, " ")
}

// ============================================================================
// INVENTORY ITEM BUILDER
// ============================================================================

func buildInventoryItem(draft *EbayDraft, currency string) *ebay.InventoryItem {
	item := &ebay.InventoryItem{
		Condition: draft.Condition,
		Product: &ebay.Product{
			Title:       draft.Title,
			Description: draft.Description,
			ImageURLs:   draft.Images,
			Aspects:     draft.Aspects,
		},
		Availability: &ebay.Availability{
			ShipToLocationAvailability: &ebay.ShipToLocation{
				Quantity: parseIntSafe(draft.Quantity, 0),
			},
		},
	}

	// Condition description (for used items)
	if draft.ConditionDescription != "" {
		item.ConditionDescription = draft.ConditionDescription
	}

	// Brand, MPN, identifiers
	if draft.Brand != "" {
		item.Product.Brand = draft.Brand
	}
	if draft.MPN != "" {
		item.Product.MPN = draft.MPN
	}
	if draft.EAN != "" {
		item.Product.EAN = []string{draft.EAN}
	}
	if draft.UPC != "" {
		item.Product.UPC = []string{draft.UPC}
	}
	if draft.ISBN != "" {
		item.Product.ISBN = []string{draft.ISBN}
	}

	// eBay Catalog EPID (FLD-09)
	if draft.EPID != "" {
		item.Product.EPID = draft.EPID
	}

	// GPSR — inject EU Product Safety fields as item aspects (FLD-07)
	// These map to eBay item specifics enforced by EU safety regulations.
	// Using aspect injection is the supported mechanism for GPSR compliance data.
	if draft.GPSRManufacturerName != "" || draft.GPSRResponsiblePersonName != "" || draft.GPSRSafetyAttestation {
		if item.Product.Aspects == nil {
			item.Product.Aspects = make(map[string][]string)
		}
		if draft.GPSRManufacturerName != "" {
			item.Product.Aspects["Manufacturer Name"] = []string{draft.GPSRManufacturerName}
		}
		if draft.GPSRManufacturerAddress != "" {
			item.Product.Aspects["Manufacturer Address"] = []string{draft.GPSRManufacturerAddress}
		}
		if draft.GPSRResponsiblePersonName != "" {
			item.Product.Aspects["EU Responsible Person"] = []string{draft.GPSRResponsiblePersonName}
		}
		if draft.GPSRResponsiblePersonContact != "" {
			item.Product.Aspects["EU Responsible Person Contact"] = []string{draft.GPSRResponsiblePersonContact}
		}
		if draft.GPSRSafetyAttestation {
			item.Product.Aspects["Safety Attestation"] = []string{"Completed"}
		}
		if draft.GPSRDocumentURLs != "" {
			item.Product.Aspects["Regulatory Documentation"] = []string{draft.GPSRDocumentURLs}
		}
	}
	if draft.PackageLength != "" || draft.PackageWidth != "" || draft.PackageHeight != "" || draft.PackageWeightValue != "" {
		pws := &ebay.PackageWeightAndSize{
			PackageType: draft.PackageType,
		}
		if draft.PackageLength != "" || draft.PackageWidth != "" || draft.PackageHeight != "" {
			pws.Dimensions = &ebay.Dimensions{
				Length: parseFloatSafe(draft.PackageLength, 0),
				Width:  parseFloatSafe(draft.PackageWidth, 0),
				Height: parseFloatSafe(draft.PackageHeight, 0),
				Unit:   draft.DimensionUnit,
			}
		}
		if draft.PackageWeightValue != "" {
			pws.Weight = &ebay.Weight{
				Value: parseFloatSafe(draft.PackageWeightValue, 0),
				Unit:  draft.WeightUnit,
			}
		}
		item.PackageWeightAndSize = pws
	}

	return item
}

// ============================================================================
// OFFER BUILDER
// ============================================================================

func buildExtendedOffer(draft *EbayDraft, marketplaceID, currency string) *ebay.ExtendedOffer {
	offer := &ebay.ExtendedOffer{
		SKU:           draft.SKU,
		MarketplaceID: marketplaceID,
		Format:        draft.ListingFormat,
		CategoryID:    draft.CategoryID,
		ListingDuration: draft.ListingDuration,
		AvailableQuantity: parseIntSafe(draft.Quantity, 0),
		IncludeCatalogProductDetails: draft.IncludeCatalogProductDetails,
		HideBuyerDetails: draft.PrivateListing,
	}

	// Secondary category
	if draft.SecondaryCategoryID != "" {
		offer.SecondaryCategoryID = draft.SecondaryCategoryID
	}

	// Subtitle
	if draft.Subtitle != "" {
		offer.Subtitle = draft.Subtitle
	}

	// Listing description (offer-level, overrides inventory item description).
	// FLD-02: if bullet points are provided, prepend them as a <ul> block.
	if draft.Description != "" || len(draft.BulletPoints) > 0 {
		desc := draft.Description
		if len(draft.BulletPoints) > 0 {
			var sb strings.Builder
			sb.WriteString("<ul>")
			for _, bp := range draft.BulletPoints {
				if bp != "" {
					sb.WriteString("<li>")
					sb.WriteString(bp)
					sb.WriteString("</li>")
				}
			}
			sb.WriteString("</ul>")
			if desc != "" {
				sb.WriteString(desc)
			}
			desc = sb.String()
		}
		offer.ListingDescription = desc
	}

	// Scheduled start
	if draft.ScheduledStartTime != "" {
		offer.ListingStartDate = draft.ScheduledStartTime
	}

	// Lot size
	if draft.LotSize != "" {
		offer.LotSize = parseIntSafe(draft.LotSize, 0)
	}

	// Pricing
	if draft.Price != "" {
		offer.PricingSummary = &ebay.PricingSummary{
			Price: &ebay.Amount{
				Value:    draft.Price,
				Currency: currency,
			},
		}
		// Reserve price for auctions
		if draft.ListingFormat == "AUCTION" && draft.ReservePrice != "" {
			offer.PricingSummary.MinimumAdvertisedPrice = &ebay.Amount{
				Value:    draft.ReservePrice,
				Currency: currency,
			}
		}
		// Quantity-based pricing tiers (FLD-10)
		if len(draft.PricingTiers) > 0 {
			tiers := make([]ebay.PricingTier, 0, len(draft.PricingTiers))
			for _, t := range draft.PricingTiers {
				if t.MinQty > 0 && t.PricePerUnit != "" {
					tiers = append(tiers, ebay.PricingTier{
						MinQuantity: t.MinQty,
						Price: &ebay.Amount{
							Value:    t.PricePerUnit,
							Currency: currency,
						},
					})
				}
			}
			if len(tiers) > 0 {
				offer.PricingSummary.PricingTiers = tiers
			}
		}
	}

	// Business policies
	if draft.FulfillmentPolicyID != "" || draft.PaymentPolicyID != "" || draft.ReturnPolicyID != "" {
		offer.ListingPolicies = &ebay.ListingPolicies{
			FulfillmentPolicyID: draft.FulfillmentPolicyID,
			PaymentPolicyID:     draft.PaymentPolicyID,
			ReturnPolicyID:      draft.ReturnPolicyID,
		}
	}

	// Merchant location
	if draft.MerchantLocationKey != "" {
		offer.MerchantLocationKey = draft.MerchantLocationKey
	}

	// Best offer
	if draft.BestOfferEnabled {
		offer.BestOfferTerms = &ebay.BestOfferTerms{
			BestOfferEnabled: true,
		}
		if draft.BestOfferAutoAcceptPrice != "" {
			offer.BestOfferTerms.AutoAcceptPrice = &ebay.Amount{
				Value:    draft.BestOfferAutoAcceptPrice,
				Currency: currency,
			}
		}
		if draft.BestOfferAutoDeclinePrice != "" {
			offer.BestOfferTerms.AutoDeclinePrice = &ebay.Amount{
				Value:    draft.BestOfferAutoDeclinePrice,
				Currency: currency,
			}
		}
	}

	// VAT
	if draft.VATPercentage != "" {
		vatPct := parseFloatSafe(draft.VATPercentage, 0)
		if vatPct > 0 {
			offer.Tax = &ebay.TaxInfo{
				VatPercentage: vatPct,
				ApplyTax:      true,
			}
		}
	}

	// Promoted Listings (PRC-04)
	// Only valid for FIXED_PRICE; add if rate is provided and in range.
	if draft.ListingFormat != "AUCTION" && draft.PromotedListingRate != "" {
		rate := parseFloatSafe(draft.PromotedListingRate, 0)
		if rate >= 1 && rate <= 20 {
			offer.PromotedListings = &ebay.PromotedListingSettings{
				PromotedListingType: "COST_PER_SALE",
				BidPercentage:       fmt.Sprintf("%.1f", rate),
			}
		}
	}

	return offer
}

// ============================================================================
// CATALOG SEARCH (FLD-09)
// ============================================================================

// GET /api/v1/ebay/catalog/search?q=xxx&gtin=xxx&marketplace=EBAY_GB
// Searches the eBay product catalogue by keyword or GTIN.
// Returns at most 10 CatalogProduct results.
// If the seller's credentials do not include catalog scope, returns empty results.
func (h *EbayHandler) CatalogSearch(c *gin.Context) {
	query := c.Query("q")
	gtin := c.Query("gtin")
	marketplace := c.Query("marketplace")
	if marketplace == "" {
		marketplace = "EBAY_GB"
	}

	if query == "" && gtin == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "q or gtin query parameter required"})
		return
	}

	client, err := h.getEbayClient(c)
	if err != nil {
		// If no client (no credentials), return empty results gracefully
		c.JSON(http.StatusOK, gin.H{"ok": true, "products": []interface{}{}})
		return
	}

	products, err := client.CatalogSearch(query, gtin, marketplace)
	if err != nil {
		log.Printf("[eBay CatalogSearch] error: %v", err)
		c.JSON(http.StatusOK, gin.H{"ok": true, "products": []interface{}{}})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "products": products})
}

// ============================================================================
// HELPERS
// ============================================================================

func parseIntSafe(s string, def int) int {
	var v int
	if _, err := fmt.Sscanf(s, "%d", &v); err == nil {
		return v
	}
	// Try parsing as float first (e.g. "1.0" -> 1)
	var f float64
	if _, err := fmt.Sscanf(s, "%f", &f); err == nil {
		return int(f)
	}
	return def
}

func parseFloatSafe(s string, def float64) float64 {
	var v float64
	if _, err := fmt.Sscanf(s, "%f", &v); err == nil {
		return v
	}
	return def
}

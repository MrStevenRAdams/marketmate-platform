package services

// ============================================================================
// EBAY BROWSE ENRICHMENT SERVICE
// ============================================================================
//
// Implements a two-phase enrichment pipeline for eBay-imported products:
//
// PHASE 1 — Per-listing enrichment (run after import)
//   For each product that was imported from eBay:
//   - Fetch full BrowseItem using the eBay item ID
//   - Extract epid, full GTIN array, shortDescription, categoryPath,
//     localizedAspects, additionalImages, estimatedSoldQuantity
//   - Store as extended_data source key: "ebay_item_{itemID}"
//
// PHASE 2 — Cross-listing EAN enrichment
//   For each product that has an EAN:
//   - Search Browse API for ALL listings with that EAN across all sellers
//   - For each result, store a branch: "ebay_ean_{EAN}_{sellerUsername}"
//     This gives one branch per unique seller listing for that EAN
//   - If an epid is found in any result, fetch the canonical eBay product
//     record and store it as "ebay_epid_{epid}"
//
// FIRESTORE LAYOUT (extended_data collection, keyed by source_key)
// ────────────────────────────────────────────────────────────────
//   ebay_item_{itemID}          Full BrowseItem for your own listing
//   ebay_ean_{EAN}_{sellerID}   Each other seller's listing for same EAN
//   ebay_epid_{epid}            Canonical eBay product catalogue entry
//
// AI CONSOLIDATION (separate step, not in this service)
//   An AI consolidation pass reads all branches for a product,
//   verifies EAN consistency, and writes a single "consolidated" record.
//
// USAGE
// ─────
//   service.EnrichProduct(ctx, tenantID, productID, ebayItemID, ean, credentialID)
//   service.EnrichAllUnenrichedEbay(ctx, tenantID, credentialID)
// ============================================================================

import (
	"context"
	"html"
	"regexp"
	"fmt"
	"log"
	"strings"
	"time"

	"module-a/marketplace/clients/ebay"
	"module-a/models"
	"module-a/repository"
)

// EbayEnrichmentService handles Browse API enrichment for eBay products.
type EbayEnrichmentService struct {
	repo *repository.MarketplaceRepository
}

func NewEbayEnrichmentService(repo *repository.MarketplaceRepository) *EbayEnrichmentService {
	return &EbayEnrichmentService{repo: repo}
}

// ─── EbayEnrichmentResult ─────────────────────────────────────────────────────

type EbayEnrichmentResult struct {
	ProductID        string
	EbayItemID       string
	BranchesWritten  []string // source_keys written
	EpidFound        string
	EANsFound        []string
	CrossListings    int // how many other-seller listings found for this EAN
	Error            string
}

// ─── EnrichProduct ────────────────────────────────────────────────────────────
//
// Full enrichment for a single product. Runs both phases.
// credentialID is the eBay credential to use for API calls.

func (s *EbayEnrichmentService) EnrichProduct(
	ctx context.Context,
	tenantID string,
	productID string,
	ebayItemID string,
	ean string,
	credentialID string,
	ebayClient *ebay.Client,
) (*EbayEnrichmentResult, error) {

	result := &EbayEnrichmentResult{
		ProductID:  productID,
		EbayItemID: ebayItemID,
	}

	// ── Phase 1: Fetch full BrowseItem for this listing ───────────────────────
	if ebayItemID != "" {
		branch, epid, eans, err := s.enrichFromOwnListing(ctx, tenantID, productID, ebayItemID, ebayClient)
		if err != nil {
			log.Printf("[EbayEnrich] Phase1 failed for item %s: %v", ebayItemID, err)
			result.Error = err.Error()
		} else {
			result.BranchesWritten = append(result.BranchesWritten, branch)
			result.EpidFound = epid
			result.EANsFound = eans

			// Use EAN from Phase 1 if caller didn't provide one
			if ean == "" && len(eans) > 0 {
				ean = eans[0]
			}
		}
	}

	// ── Phase 2: Search by EAN across all sellers ─────────────────────────────
	if ean != "" && ean != "Does not apply" {
		branches, crossCount, epid, err := s.enrichFromEANSearch(ctx, tenantID, productID, ean, ebayClient)
		if err != nil {
			log.Printf("[EbayEnrich] Phase2 EAN search failed for %s: %v", ean, err)
		} else {
			result.BranchesWritten = append(result.BranchesWritten, branches...)
			result.CrossListings = crossCount
			if result.EpidFound == "" {
				result.EpidFound = epid
			}
		}
	}

	// ── Phase 3: Fetch canonical EPID record ──────────────────────────────────
	if result.EpidFound != "" {
		branch, err := s.enrichFromEPID(ctx, tenantID, productID, result.EpidFound, ebayClient)
		if err != nil {
			log.Printf("[EbayEnrich] Phase3 EPID fetch failed for %s: %v", result.EpidFound, err)
		} else if branch != "" {
			result.BranchesWritten = append(result.BranchesWritten, branch)
		}
	}

	log.Printf("[EbayEnrich] Product %s: %d branches written, epid=%s, ean=%s, crossListings=%d",
		productID, len(result.BranchesWritten), result.EpidFound, ean, result.CrossListings)

	return result, nil
}

// ─── Phase 1: Own listing ─────────────────────────────────────────────────────

func (s *EbayEnrichmentService) enrichFromOwnListing(
	ctx context.Context,
	tenantID, productID, ebayItemID string,
	client *ebay.Client,
) (sourceKey string, epid string, eans []string, err error) {

	item, err := client.BrowseGetItemByLegacyID(ebayItemID)
	if err != nil {
		return "", "", nil, fmt.Errorf("BrowseGetItemByLegacyID(%s): %w", ebayItemID, err)
	}

	data := browseItemToMap(item)
	data["enrichment_phase"] = "own_listing"
	data["enriched_at"] = time.Now().Format(time.RFC3339)

	sourceKey = fmt.Sprintf("ebay_item_%s", ebayItemID)
	ext := &models.ExtendedProductData{
		SourceKey:  sourceKey,
		ProductID:  productID,
		TenantID:   tenantID,
		Source:     "ebay_browse",
		SourceID:   ebayItemID,
		Data:       data,
		FetchedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	if err := s.repo.SaveExtendedData(ctx, tenantID, ext); err != nil {
		return "", "", nil, fmt.Errorf("save own listing enrichment: %w", err)
	}

	return sourceKey, item.EpID, item.EAN, nil
}

// ─── Phase 2: EAN search across all sellers ───────────────────────────────────

func (s *EbayEnrichmentService) enrichFromEANSearch(
	ctx context.Context,
	tenantID, productID, ean string,
	client *ebay.Client,
) (branches []string, crossCount int, epid string, err error) {

	summaries, err := client.BrowseSearchByGTIN(ean, "EBAY_GB", 50)
	if err != nil {
		return nil, 0, "", err
	}
	if len(summaries) == 0 {
		return nil, 0, "", nil
	}

	for _, summary := range summaries {
		sellerID := "unknown"
		if summary.Seller != nil {
			sellerID = summary.Seller.Username
		}

		// Sanitise sellerID for use as a Firestore doc key
		sellerID = sanitiseDocKey(sellerID)

		sourceKey := fmt.Sprintf("ebay_ean_%s_%s", sanitiseDocKey(ean), sellerID)

		data := map[string]interface{}{
			"enrichment_phase":  "ean_cross_listing",
			"ean_searched":      ean,
			"ebay_item_id":      summary.ItemID,
			"legacy_item_id":    summary.LegacyItemID,
			"title":             summary.Title,
			"category_path":     summary.CategoryPath,
			"epid":              summary.EpID,
			"gtin":              summary.GTIN,
			"condition":         summary.Condition,
			"condition_id":      summary.ConditionID,
			"item_web_url":      summary.ItemWebURL,
			"quantity_sold":     summary.QuantitySold,
			"enriched_at":       time.Now().Format(time.RFC3339),
		}

		if summary.Price != nil {
			data["price_value"] = summary.Price.Value
			data["price_currency"] = summary.Price.Currency
		}
		if summary.Image != nil {
			data["image_url"] = summary.Image.ImageURL
		}
		if summary.Seller != nil {
			data["seller_username"] = summary.Seller.Username
			data["seller_feedback_score"] = summary.Seller.FeedbackScore
			data["seller_feedback_pct"] = summary.Seller.FeedbackPercentage
		}
		if len(summary.Categories) > 0 {
			cats := make([]string, len(summary.Categories))
			for i, c := range summary.Categories {
				cats[i] = c.CategoryName
			}
			data["categories"] = cats
			data["category_id"] = summary.Categories[0].CategoryID
		}

		ext := &models.ExtendedProductData{
			SourceKey: sourceKey,
			ProductID: productID,
			TenantID:  tenantID,
			Source:    "ebay_browse_ean",
			SourceID:  summary.ItemID,
			Data:      data,
			FetchedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		if saveErr := s.repo.SaveExtendedData(ctx, tenantID, ext); saveErr != nil {
			log.Printf("[EbayEnrich] Failed to save EAN branch %s: %v", sourceKey, saveErr)
			continue
		}

		branches = append(branches, sourceKey)
		crossCount++

		// Capture the first epid we find
		if epid == "" && summary.EpID != "" {
			epid = summary.EpID
		}
	}

	return branches, crossCount, epid, nil
}

// ─── Phase 3: Canonical EPID record ──────────────────────────────────────────

func (s *EbayEnrichmentService) enrichFromEPID(
	ctx context.Context,
	tenantID, productID, epid string,
	client *ebay.Client,
) (sourceKey string, err error) {

	item, err := client.BrowseGetItemsByEPID(epid, "EBAY_GB")
	if err != nil {
		return "", fmt.Errorf("BrowseGetItemsByEPID(%s): %w", epid, err)
	}

	data := browseItemToMap(item)
	data["enrichment_phase"] = "epid_canonical"
	data["epid"] = epid
	data["enriched_at"] = time.Now().Format(time.RFC3339)

	sourceKey = fmt.Sprintf("ebay_epid_%s", sanitiseDocKey(epid))
	ext := &models.ExtendedProductData{
		SourceKey: sourceKey,
		ProductID: productID,
		TenantID:  tenantID,
		Source:    "ebay_browse_epid",
		SourceID:  epid,
		Data:      data,
		FetchedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := s.repo.SaveExtendedData(ctx, tenantID, ext); err != nil {
		return "", fmt.Errorf("save EPID enrichment: %w", err)
	}

	return sourceKey, nil
}

// ─── Bulk enrichment ──────────────────────────────────────────────────────────

// EbayEnrichmentRequest is the payload for a single enrichment task
type EbayEnrichmentRequest struct {
	TenantID     string `json:"tenant_id"`
	ProductID    string `json:"product_id"`
	EbayItemID   string `json:"ebay_item_id"`
	EAN          string `json:"ean"`
	CredentialID string `json:"credential_id"`
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// browseItemToMap converts a BrowseItem to a flat map for extended_data storage
func browseItemToMap(item *ebay.BrowseItem) map[string]interface{} {
	if item == nil {
		return map[string]interface{}{}
	}

	data := map[string]interface{}{
		"ebay_item_id":      item.ItemID,
		"legacy_item_id":    item.LegacyItemID,
		"title":             item.Title,
		"short_description": stripHTMLEnrich(item.ShortDescription),
		"description":       stripHTMLEnrich(item.Description),
		"condition":         item.Condition,
		"condition_id":      item.ConditionID,
		"condition_description": item.ConditionDescription,
		"item_web_url":      item.ItemWebURL,
		"category_path":     item.CategoryPath,
		"category_id":       item.CategoryID,
		"brand":             item.Brand,
		"mpn":               item.MPN,
		"epid":              item.EpID,
		"gtin":              item.GTIN,
		"sku":               item.SKU,
		"quantity_sold":     item.QuantitySold,
	}

	// Identifiers
	if len(item.EAN) > 0 {
		data["eans"] = item.EAN
		data["ean"] = item.EAN[0]
	}
	if len(item.UPC) > 0 {
		data["upcs"] = item.UPC
		data["upc"] = item.UPC[0]
	}
	if len(item.ISBN) > 0 {
		data["isbns"] = item.ISBN
		data["isbn"] = item.ISBN[0]
	}

	// Price
	if item.Price != nil {
		data["price_value"] = item.Price.Value
		data["price_currency"] = item.Price.Currency
	}

	// Images
	if item.Image != nil {
		data["image_url"] = item.Image.ImageURL
	}
	var additionalImages []string
	for _, img := range item.AdditionalImages {
		additionalImages = append(additionalImages, img.ImageURL)
	}
	if len(additionalImages) > 0 {
		data["additional_images"] = additionalImages
	}

	// Seller
	if item.Seller != nil {
		data["seller_username"] = item.Seller.Username
		data["seller_feedback_score"] = item.Seller.FeedbackScore
		data["seller_feedback_pct"] = item.Seller.FeedbackPercentage
	}

	// Location
	if item.ItemLocation != nil {
		data["item_location_country"] = item.ItemLocation.Country
		data["item_location_city"] = item.ItemLocation.City
		data["item_location_postcode"] = item.ItemLocation.PostalCode
	}

	// Buying options
	if len(item.BuyingOptions) > 0 {
		data["buying_options"] = item.BuyingOptions
	}

	// Localized aspects (structured item specifics)
	if len(item.LocalizedAspects) > 0 {
		aspects := make(map[string]string)
		for _, a := range item.LocalizedAspects {
			aspects[a.Name] = a.Value
		}
		data["localized_aspects"] = aspects
	}

	// Availability
	if len(item.EstimatedAvailabilities) > 0 {
		av := item.EstimatedAvailabilities[0]
		data["availability_status"] = av.EstimatedAvailabilityStatus
		data["estimated_sold_quantity"] = av.EstimatedSoldQuantity
	}

	// Categories
	if len(item.Categories) > 0 {
		cats := make([]map[string]string, len(item.Categories))
		for i, c := range item.Categories {
			cats[i] = map[string]string{"id": c.CategoryID, "name": c.CategoryName}
		}
		data["categories"] = cats
	}

	return data
}

// sanitiseDocKey makes a string safe for use as a Firestore document key
func sanitiseDocKey(s string) string {
	s = strings.ToLower(s)
	var result strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			result.WriteRune(r)
		} else {
			result.WriteRune('_')
		}
	}
	return strings.Trim(result.String(), "_")
}

// stripHTMLEnrich removes HTML tags and decodes entities for clean plain-text descriptions.
var reHTMLTagEnrich = regexp.MustCompile(`<[^>]+>`)

func stripHTMLEnrich(s string) string {
	s = reHTMLTagEnrich.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimSpace(s)
}

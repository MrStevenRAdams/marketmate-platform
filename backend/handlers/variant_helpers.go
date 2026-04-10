package handlers

// ============================================================================
// VARIANT HELPERS
// ============================================================================
// Shared types and functions for loading PIM product variants into a generic
// ChannelVariantDraft shape that every channel handler can consume.

import (
	"context"
	"fmt"
	"log"

	"module-a/repository"
)

// ChannelVariantDraft is the canonical variant shape returned by every channel's
// prepare endpoint and sent back in every channel's submit payload.
type ChannelVariantDraft struct {
	ID          string            `json:"id"`
	SKU         string            `json:"sku"`
	Combination map[string]string `json:"combination"` // e.g. {"Color":"Red","Size":"M"}
	Price       string            `json:"price"`
	Stock       string            `json:"stock"`
	Image       string            `json:"image"`       // primary image URL
	Images      []string          `json:"images"`      // all images for this variant
	EAN         string            `json:"ean"`
	UPC         string            `json:"upc"`
	Title       string            `json:"title,omitempty"`
	Description string            `json:"description,omitempty"`
	Brand       string            `json:"brand,omitempty"`
	Condition   string            `json:"condition,omitempty"`
	Weight      string            `json:"weight,omitempty"`
	WeightUnit  string            `json:"weightUnit,omitempty"`
	Length      string            `json:"length,omitempty"`
	Width       string            `json:"width,omitempty"`
	Height      string            `json:"height,omitempty"`
	LengthUnit  string            `json:"lengthUnit,omitempty"`
	Active      bool              `json:"active"`
}

var axisKeys = map[string]bool{
	"color": true, "colour": true, "size": true, "style": true,
	"flavour": true, "scent": true, "material": true, "pattern": true,
}

// loadChannelVariants fetches child variation products linked by parent_id.
// Falls back to the legacy variants subcollection if no children are found.
func loadChannelVariants(
	ctx context.Context,
	repo *repository.FirestoreRepository,
	tenantID, productID, fallbackPrice string,
	fallbackImage string,
) []ChannelVariantDraft {

	// ── Strategy 1: child products with parent_id = productID ──
	children, _, err := repo.ListProducts(ctx, tenantID, map[string]interface{}{"parent_id": productID}, 100, 0)
	if err != nil {
		log.Printf("[loadChannelVariants] ListProducts(parent_id) error for %s: %v", productID, err)
	}

	if len(children) > 0 {
		result := make([]ChannelVariantDraft, 0, len(children))
		for _, child := range children {
			// Combination: first try known axis keys, then fall back to ALL
			// string attributes that aren't internal/system fields.
			skipKeys := map[string]bool{
				"source_sku": true, "source_price": true, "source_quantity": true,
				"description": true, "title": true, "brand": true,
			}
			combination := map[string]string{}
			for k, val := range child.Attributes {
				if axisKeys[k] {
					if s := fmt.Sprintf("%v", val); s != "" && s != "<nil>" {
						combination[k] = s
					}
				}
			}
			// If no known axis keys matched, use all short string attributes as axes
			if len(combination) == 0 {
				for k, val := range child.Attributes {
					if skipKeys[k] { continue }
					s := fmt.Sprintf("%v", val)
					if s != "" && s != "<nil>" && len(s) < 100 {
						combination[k] = s
					}
				}
			}
			if len(combination) == 0 {
				// Last resort: use child SKU as the differentiator
				if child.SKU != "" {
					combination["variant"] = child.SKU
				} else {
					continue
				}
			}

			// SKU: top-level field first, then attributes.source_sku
			sku := child.SKU
			if sku == "" {
				if s, ok := child.Attributes["source_sku"].(string); ok {
					sku = s
				}
			}

			// Price from attributes.source_price
			price := fallbackPrice
			switch p := child.Attributes["source_price"].(type) {
			case float64:
				if p > 0 {
					price = fmt.Sprintf("%.2f", p)
				}
			}

			// Stock from attributes.source_quantity
			stock := "0"
			switch q := child.Attributes["source_quantity"].(type) {
			case float64:
				stock = fmt.Sprintf("%.0f", q)
			case int64:
				stock = fmt.Sprintf("%d", q)
			}

			// EAN / UPC from identifiers
			ean, upc := "", ""
			if child.Identifiers != nil {
				if child.Identifiers.EAN != nil {
					ean = *child.Identifiers.EAN
				}
				if child.Identifiers.UPC != nil {
					upc = *child.Identifiers.UPC
				}
			}

			// Images from assets
			var images []string
			primaryImage := fallbackImage
			for _, a := range child.Assets {
				if a.URL != "" {
					images = append(images, a.URL)
					if primaryImage == fallbackImage {
						primaryImage = a.URL
					}
				}
			}
			if images == nil {
				images = []string{}
			}

			result = append(result, ChannelVariantDraft{
				ID:          child.ProductID,
				SKU:         sku,
				Combination: combination,
				Price:       price,
				Stock:       stock,
				Image:       primaryImage,
				Images:      images,
				EAN:         ean,
				UPC:         upc,
				Active:      child.Status != "inactive",
			})
		}
		if len(result) > 0 {
			log.Printf("[loadChannelVariants] Loaded %d child-product variants for %s", len(result), productID)
			return result
		}
	}

	// ── Strategy 2: legacy variants subcollection ──
	variants, _, verr := repo.ListVariants(ctx, tenantID, map[string]interface{}{"product_id": productID}, 100, 0)
	if verr != nil {
		log.Printf("[loadChannelVariants] ListVariants error for product %s: %v", productID, verr)
		return nil
	}
	if len(variants) == 0 {
		return nil
	}

	result := make([]ChannelVariantDraft, 0, len(variants))
	for _, v := range variants {
		combination := map[string]string{}
		if v.Attributes != nil {
			for k, val := range v.Attributes {
				if axisKeys[k] {
					combination[k] = fmt.Sprintf("%v", val)
				}
			}
		}
		price := fallbackPrice
		if v.Pricing != nil && v.Pricing.ListPrice != nil && v.Pricing.ListPrice.Amount > 0 {
			price = fmt.Sprintf("%.2f", v.Pricing.ListPrice.Amount)
		}
		ean := ""
		if v.Identifiers != nil && v.Identifiers.EAN != nil && *v.Identifiers.EAN != "" {
			ean = *v.Identifiers.EAN
		}
		if ean == "" && v.Barcode != nil && *v.Barcode != "" {
			ean = *v.Barcode
		}
		result = append(result, ChannelVariantDraft{
			ID:          v.VariantID,
			SKU:         v.SKU,
			Combination: combination,
			Price:       price,
			Stock:       "0",
			Image:       fallbackImage,
			Images:      []string{},
			EAN:         ean,
			Active:      v.Status != "inactive",
		})
	}
	return result
}

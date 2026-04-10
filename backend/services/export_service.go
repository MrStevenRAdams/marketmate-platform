package services

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"sort"
	"strings"
	"time"

	"module-a/models"
	"module-a/repository"
)

// ============================================================================
// CSV/XLSX EXPORT SERVICE
// ============================================================================
// Column layout (MarketMate import/export format v2):
//
//   FIXED COLUMNS (always present — see FixedColumns slice below):
//     product_id, product_type, parent_sku, sku, title, subtitle, description,
//     brand, status, ean, upc, asin, isbn, mpn, gtin,
//     categories, tags, key_features, attribute_set_id,
//     list_price, currency, rrp, cost_price, sale_price, sale_start, sale_end,
//     quantity,
//     weight_value, weight_unit, length, width, height, dimension_unit,
//     shipping_weight_value, shipping_weight_unit,
//     shipping_length, shipping_width, shipping_height, shipping_dimension_unit,
//     use_serial_numbers, end_of_life, storage_group_id,
//     alias, barcode,
//     supplier_sku, supplier_name, supplier_cost, supplier_currency, supplier_lead_time_days
//
//   VARIANT ATTRIBUTE COLUMNS (one column per key, named):
//     variant_attr_colour, variant_attr_size, variant_attr_{key} …
//
//   IMAGE COLUMNS:
//     image_1 … image_5
//
//   BUNDLE COLUMN:
//     bundle_component_skus
//
//   FREEFORM ATTRIBUTE COLUMNS (one column per key, named):
//     attribute_colour, attribute_recommended_age, attribute_{key} …
//
// Naming convention: the UI strips the "attribute_" or "variant_attr_" prefix
// and title-cases the remainder to produce a display label.
// Example: "attribute_recommended_age" → "Recommended Age"
//
// BACKWARD COMPATIBILITY: the old "attribute_N_name / attribute_N_value" pair
// format is still accepted on import but never produced on export.
//
// This format is symmetrical: every exported file can be re-imported without
// modification.
// ============================================================================

const (
	MaxImageCols = 5 // fixed number of image URL columns per row
)

type ExportService struct {
	repo *repository.FirestoreRepository
}

func NewExportService(repo *repository.FirestoreRepository) *ExportService {
	return &ExportService{repo: repo}
}

// FixedColumns are the non-dynamic columns that always appear in the export.
// These map 1:1 to Product / Variant model fields. Every field that exists in
// the PIM must be represented here so the file is a complete round-trip.
//
// Column naming rules:
//   - Fixed model fields use snake_case matching the JSON tag (e.g. "end_of_life")
//   - Freeform attribute columns use the "attribute_{key}" prefix (e.g. "attribute_colour")
//   - Variant attribute columns use the "variant_attr_{key}" prefix
//   - Supplier fields use the "supplier_{n}_{field}" prefix
var FixedColumns = []string{
	// Identity & type
	"product_id", "product_type", "parent_sku", "sku",

	// Core content
	"title", "subtitle", "description", "brand", "status",

	// Identifiers
	"ean", "upc", "asin", "isbn", "mpn", "gtin",

	// Classification
	"categories", "tags", "key_features", "attribute_set_id",

	// Pricing  (products: read from Attributes map; variants: read from Pricing struct)
	"list_price", "currency", "rrp", "cost_price",
	"sale_price", "sale_start", "sale_end",

	// Stock
	"quantity",

	// Product dimensions & weight (physical product)
	"weight_value", "weight_unit",
	"length", "width", "height", "dimension_unit",

	// Shipping dimensions & weight (packaged)
	"shipping_weight_value", "shipping_weight_unit",
	"shipping_length", "shipping_width", "shipping_height", "shipping_dimension_unit",

	// Lifecycle flags
	"use_serial_numbers", "end_of_life",

	// WMS
	"storage_group_id",

	// Variant-specific (blank for parent/simple/bundle rows)
	"alias", "barcode",

	// Supplier 1 (primary supplier — most products have at most one)
	"supplier_sku", "supplier_name", "supplier_cost", "supplier_currency",
	"supplier_lead_time_days",
}

// buildHeaders constructs the full column header slice for a given export.
//
// Attribute columns changed from the old "attribute_N_name / attribute_N_value"
// pair format to direct named columns: "attribute_{key}" and "variant_attr_{key}".
// This makes the file self-describing and allows users to add new attributes simply
// by adding a column named "attribute_recommended_age" etc. The UI can render the
// column name as "Recommended Age" by stripping the prefix and title-casing.
//
// attrKeys and variantAttrKeys are the sorted union of all attribute keys seen
// across all products/variants in the export — collected before calling this
// function in a single pre-scan.
func buildHeaders(attrKeys, variantAttrKeys []string, numImageCols int) []string {
	h := make([]string, 0, len(FixedColumns)+len(attrKeys)+len(variantAttrKeys)+numImageCols+1)
	h = append(h, FixedColumns...)

	// Named variant attribute columns: variant_attr_colour, variant_attr_size …
	for _, k := range variantAttrKeys {
		h = append(h, "variant_attr_"+k)
	}

	// Image columns
	for i := 1; i <= numImageCols; i++ {
		h = append(h, fmt.Sprintf("image_%d", i))
	}

	// Bundle components
	h = append(h, "bundle_component_skus")

	// Named freeform attribute columns: attribute_colour, attribute_recommended_age …
	for _, k := range attrKeys {
		h = append(h, "attribute_"+k)
	}

	return h
}

// attrColumnLabel converts a column name like "attribute_recommended_age"
// to a display label "Recommended Age". Used by the UI — not needed in the
// export service itself, documented here for reference.
//
// Implementation in TypeScript (frontend):
//   const label = col.replace(/^(attribute_|variant_attr_)/, '').replace(/_/g, ' ')
//                    .replace(/\w/g, c => c.toUpperCase())

type ExportResult struct {
	Headers    []string   `json:"headers"`
	Rows       [][]string `json:"rows"`
	Total      int        `json:"total"`
	Filename   string     `json:"filename"`
	ExportedAt string     `json:"exported_at"`
}

func (s *ExportService) ExportProductsCSV(ctx context.Context, tenantID string, filters map[string]interface{}) ([]byte, string, error) {
	result, err := s.ExportProducts(ctx, tenantID, filters)
	if err != nil {
		return nil, "", err
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	w.Write(result.Headers)
	for _, row := range result.Rows {
		w.Write(row)
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, "", fmt.Errorf("csv write: %w", err)
	}
	return buf.Bytes(), result.Filename, nil
}

// internalAttrs are product attribute keys that are stored internally and
// should not be exported as freeform attribute columns.
var internalAttrs = map[string]bool{
	"source_sku": true, "source_price": true, "source_currency": true,
	"source_marketplace": true, "bullet_points": true, "sku": true,
	"fulfillment_channel": true,
}

// collectAttrKeys performs a single pre-scan over products and variants to
// discover the union of all freeform attribute keys actually used. This is
// necessary for the new named-column format so we know which columns to emit.
// Internal/system keys are excluded. Keys are returned sorted for determinism.
func collectAttrKeys(products []models.Product, variants []models.Variant) (attrKeys, variantAttrKeys []string) {
	attrSet := map[string]bool{}
	variantAttrSet := map[string]bool{}
	for _, p := range products {
		for k := range p.Attributes {
			if !internalAttrs[k] {
				attrSet[k] = true
			}
		}
	}
	for _, v := range variants {
		for k := range v.Attributes {
			variantAttrSet[k] = true
		}
	}
	for k := range attrSet {
		attrKeys = append(attrKeys, k)
	}
	for k := range variantAttrSet {
		variantAttrKeys = append(variantAttrKeys, k)
	}
	sort.Strings(attrKeys)
	sort.Strings(variantAttrKeys)
	return
}

// ExportProducts builds a complete in-memory ExportResult for the given tenant.
// For background export jobs, prefer ExportProductsToWriter which streams rows
// directly to an io.Writer and avoids buffering the entire CSV in memory.
func (s *ExportService) ExportProducts(ctx context.Context, tenantID string, filters map[string]interface{}) (*ExportResult, error) {
	products, _, err := s.repo.ListProducts(ctx, tenantID, filters, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("list products: %w", err)
	}
	variants, _, err := s.repo.ListVariants(ctx, tenantID, map[string]interface{}{}, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("list variants: %w", err)
	}

	variantsByProduct := map[string][]models.Variant{}
	for _, v := range variants {
		variantsByProduct[v.ProductID] = append(variantsByProduct[v.ProductID], v)
	}
	productSKU := map[string]string{}
	for _, p := range products {
		productSKU[p.ProductID] = resolveProductSKU(&p, variantsByProduct[p.ProductID])
	}

	attrKeys, variantAttrKeys := collectAttrKeys(products, variants)
	allHeaders := buildHeaders(attrKeys, variantAttrKeys, MaxImageCols)
	colIdx := map[string]int{}
	for i, h := range allHeaders {
		colIdx[h] = i
	}

	var rows [][]string
	for _, p := range products {
		if p.ProductType == "variant" {
			continue
		}
		rows = append(rows, productToRow(&p, productSKU, colIdx, allHeaders, attrKeys))
		if p.ProductType == "parent" {
			psku := productSKU[p.ProductID]
			for _, v := range variantsByProduct[p.ProductID] {
				rows = append(rows, variantToRow(&v, psku, colIdx, allHeaders, variantAttrKeys))
			}
		}
	}

	log.Printf("[Export] %d rows, %d cols for tenant %s", len(rows), len(allHeaders), tenantID)
	return &ExportResult{
		Headers:    allHeaders,
		Rows:       rows,
		Total:      len(rows),
		Filename:   FormatExportFilename("products", "csv"),
		ExportedAt: time.Now().Format(time.RFC3339),
	}, nil
}

// ExportProductsToWriter streams a products CSV export directly into w without
// buffering the entire file in memory. It returns the number of data rows written
// (excluding the header). progressFn is called after every 1,000 rows with the
// current count so callers can write progress updates; pass nil to skip.
func (s *ExportService) ExportProductsToWriter(ctx context.Context, tenantID string, w io.Writer, progressFn func(rowsWritten int)) (rowCount int, err error) {
	products, _, err := s.repo.ListProducts(ctx, tenantID, map[string]interface{}{}, 0, 0)
	if err != nil {
		return 0, fmt.Errorf("list products: %w", err)
	}
	variants, _, err := s.repo.ListVariants(ctx, tenantID, map[string]interface{}{}, 0, 0)
	if err != nil {
		return 0, fmt.Errorf("list variants: %w", err)
	}

	variantsByProduct := map[string][]models.Variant{}
	for _, v := range variants {
		variantsByProduct[v.ProductID] = append(variantsByProduct[v.ProductID], v)
	}
	productSKU := map[string]string{}
	for _, p := range products {
		productSKU[p.ProductID] = resolveProductSKU(&p, variantsByProduct[p.ProductID])
	}

	attrKeys, variantAttrKeys := collectAttrKeys(products, variants)
	allHeaders := buildHeaders(attrKeys, variantAttrKeys, MaxImageCols)
	colIdx := map[string]int{}
	for i, h := range allHeaders {
		colIdx[h] = i
	}

	cw := csv.NewWriter(w)
	if err := cw.Write(allHeaders); err != nil {
		return 0, fmt.Errorf("write header: %w", err)
	}

	for i, p := range products {
		if p.ProductType == "variant" {
			continue
		}
		if err := cw.Write(productToRow(&p, productSKU, colIdx, allHeaders, attrKeys)); err != nil {
			return rowCount, fmt.Errorf("write row %d: %w", i, err)
		}
		rowCount++
		if p.ProductType == "parent" {
			psku := productSKU[p.ProductID]
			for _, v := range variantsByProduct[p.ProductID] {
				if err := cw.Write(variantToRow(&v, psku, colIdx, allHeaders, variantAttrKeys)); err != nil {
					return rowCount, fmt.Errorf("write variant row: %w", err)
				}
				rowCount++
			}
		}
		if rowCount%1000 == 0 {
			cw.Flush()
			if err := cw.Error(); err != nil {
				return rowCount, fmt.Errorf("csv flush at row %d: %w", rowCount, err)
			}
			if progressFn != nil {
				progressFn(rowCount)
			}
		}
	}

	cw.Flush()
	return rowCount, cw.Error()
}

// ============================================================================
// ROW BUILDERS
// ============================================================================

func productToRow(p *models.Product, productSKU map[string]string, colIdx map[string]int, headers []string, attrKeys []string) []string {
	row := make([]string, len(headers))
	set := func(col, val string) {
		if idx, ok := colIdx[col]; ok && val != "" {
			row[idx] = val
		}
	}

	// Identity
	set("product_id", p.ProductID)
	set("product_type", p.ProductType)
	set("sku", productSKU[p.ProductID])
	set("title", p.Title)
	set("status", p.Status)

	// Core content
	if p.Subtitle != nil    { set("subtitle", *p.Subtitle) }
	if p.Description != nil { set("description", *p.Description) }
	if p.Brand != nil       { set("brand", *p.Brand) }

	// Identifiers
	if p.Identifiers != nil {
		set("ean",  derefStr(p.Identifiers.EAN))
		set("upc",  derefStr(p.Identifiers.UPC))
		set("asin", derefStr(p.Identifiers.ASIN))
		set("isbn", derefStr(p.Identifiers.ISBN))
		set("mpn",  derefStr(p.Identifiers.MPN))
		set("gtin", derefStr(p.Identifiers.GTIN))
	}

	// Classification
	if len(p.CategoryIDs) > 0  { set("categories",     strings.Join(p.CategoryIDs, "|")) }
	if len(p.Tags) > 0         { set("tags",            strings.Join(p.Tags, "|")) }
	if len(p.KeyFeatures) > 0  { set("key_features",   strings.Join(p.KeyFeatures, "|")) }
	if p.AttributeSetID != nil { set("attribute_set_id", *p.AttributeSetID) }

	// Pricing — for products, price lives in Attributes["source_price"]
	if p.Attributes != nil {
		if price, ok := p.Attributes["source_price"].(float64); ok && price > 0 {
			set("list_price", fmt.Sprintf("%.2f", price))
		}
		if curr, ok := p.Attributes["source_currency"].(string); ok {
			set("currency", curr)
		}
	}

	// Physical dimensions & weight
	if p.Dimensions != nil {
		if p.Dimensions.Length != nil { set("length", fmt.Sprintf("%.2f", *p.Dimensions.Length)) }
		if p.Dimensions.Width  != nil { set("width",  fmt.Sprintf("%.2f", *p.Dimensions.Width))  }
		if p.Dimensions.Height != nil { set("height", fmt.Sprintf("%.2f", *p.Dimensions.Height)) }
		if p.Dimensions.Unit   != ""  { set("dimension_unit", p.Dimensions.Unit) }
	}
	if p.Weight != nil {
		if p.Weight.Value != nil { set("weight_value", fmt.Sprintf("%.3f", *p.Weight.Value)) }
		if p.Weight.Unit  != ""  { set("weight_unit", p.Weight.Unit) }
	}

	// Shipping dimensions & weight
	if p.ShippingDimensions != nil {
		if p.ShippingDimensions.Length != nil { set("shipping_length", fmt.Sprintf("%.2f", *p.ShippingDimensions.Length)) }
		if p.ShippingDimensions.Width  != nil { set("shipping_width",  fmt.Sprintf("%.2f", *p.ShippingDimensions.Width))  }
		if p.ShippingDimensions.Height != nil { set("shipping_height", fmt.Sprintf("%.2f", *p.ShippingDimensions.Height)) }
		if p.ShippingDimensions.Unit   != ""  { set("shipping_dimension_unit", p.ShippingDimensions.Unit) }
	}
	if p.ShippingWeight != nil {
		if p.ShippingWeight.Value != nil { set("shipping_weight_value", fmt.Sprintf("%.3f", *p.ShippingWeight.Value)) }
		if p.ShippingWeight.Unit  != ""  { set("shipping_weight_unit", p.ShippingWeight.Unit) }
	}

	// Lifecycle flags
	if p.UseSerialNumbers { set("use_serial_numbers", "true") }
	if p.EndOfLife        { set("end_of_life", "true") }

	// WMS
	if p.StorageGroupID != "" { set("storage_group_id", p.StorageGroupID) }

	// Images
	for i, url := range getImageURLs(p.Assets) {
		if i >= MaxImageCols { break }
		set(fmt.Sprintf("image_%d", i+1), url)
	}

	// Bundle components
	if p.ProductType == "bundle" && len(p.BundleComponents) > 0 {
		var parts []string
		for _, bc := range p.BundleComponents {
			csku := productSKU[bc.ProductID]
			if csku == "" { csku = bc.ProductID }
			parts = append(parts, fmt.Sprintf("%s:%d", csku, bc.Quantity))
		}
		set("bundle_component_skus", strings.Join(parts, "|"))
	}

	// Primary supplier (supplier at priority=1 or IsDefault, else first entry)
	if len(p.Suppliers) > 0 {
		sup := p.Suppliers[0]
		for _, s := range p.Suppliers {
			if s.IsDefault || s.Priority == 1 {
				sup = s
				break
			}
		}
		set("supplier_sku",            sup.SupplierSKU)
		set("supplier_name",           sup.SupplierName)
		set("supplier_cost",           fmt.Sprintf("%.2f", sup.UnitCost))
		set("supplier_currency",       sup.Currency)
		if sup.LeadTimeDays > 0 {
			set("supplier_lead_time_days", fmt.Sprintf("%d", sup.LeadTimeDays))
		}
	}

	// Freeform attributes — one column per key: "attribute_{key}"
	// attrKeys is the sorted union across the whole export so columns align.
	if p.Attributes != nil {
		for _, key := range attrKeys {
			if val, ok := p.Attributes[key]; ok {
				if s := attrToString(val); s != "" {
					set("attribute_"+key, s)
				}
			}
		}
	}

	return row
}

func variantToRow(v *models.Variant, parentSKU string, colIdx map[string]int, headers []string, variantAttrKeys []string) []string {
	row := make([]string, len(headers))
	set := func(col, val string) {
		if idx, ok := colIdx[col]; ok && val != "" {
			row[idx] = val
		}
	}

	// Identity
	set("product_id",   v.ProductID)
	set("product_type", "variant")
	set("parent_sku",   parentSKU)
	set("sku",          v.SKU)
	set("status",       v.Status)

	// Variant-specific fields
	if v.Alias   != nil { set("alias",   *v.Alias) }
	if v.Barcode != nil { set("barcode", *v.Barcode) }
	if v.Title   != nil { set("title",   *v.Title) }

	// Identifiers
	if v.Identifiers != nil {
		set("ean",  derefStr(v.Identifiers.EAN))
		set("upc",  derefStr(v.Identifiers.UPC))
		set("asin", derefStr(v.Identifiers.ASIN))
		set("isbn", derefStr(v.Identifiers.ISBN))
		set("mpn",  derefStr(v.Identifiers.MPN))
		set("gtin", derefStr(v.Identifiers.GTIN))
	}

	// Pricing
	if v.Pricing != nil {
		curr := "GBP"
		if v.Pricing.ListPrice != nil {
			curr = v.Pricing.ListPrice.Currency
			set("list_price", fmt.Sprintf("%.2f", v.Pricing.ListPrice.Amount))
			set("currency", curr)
		}
		if v.Pricing.RRP  != nil { set("rrp",        fmt.Sprintf("%.2f", v.Pricing.RRP.Amount)) }
		if v.Pricing.Cost != nil { set("cost_price",  fmt.Sprintf("%.2f", v.Pricing.Cost.Amount)) }
		if v.Pricing.Sale != nil {
			set("sale_price", fmt.Sprintf("%.2f", v.Pricing.Sale.SalePrice.Amount))
			if v.Pricing.Sale.From != nil { set("sale_start", v.Pricing.Sale.From.Format("2006-01-02")) }
			if v.Pricing.Sale.To   != nil { set("sale_end",   v.Pricing.Sale.To.Format("2006-01-02")) }
		}
		_ = curr
	}

	// Dimensions & weight
	if v.Dimensions != nil {
		if v.Dimensions.Length != nil { set("length", fmt.Sprintf("%.2f", *v.Dimensions.Length)) }
		if v.Dimensions.Width  != nil { set("width",  fmt.Sprintf("%.2f", *v.Dimensions.Width))  }
		if v.Dimensions.Height != nil { set("height", fmt.Sprintf("%.2f", *v.Dimensions.Height)) }
		if v.Dimensions.Unit   != ""  { set("dimension_unit", v.Dimensions.Unit) }
	}
	if v.Weight != nil {
		if v.Weight.Value != nil { set("weight_value", fmt.Sprintf("%.3f", *v.Weight.Value)) }
		if v.Weight.Unit  != ""  { set("weight_unit", v.Weight.Unit) }
	}

	// Variant attributes — one column per key: "variant_attr_{key}"
	for _, key := range variantAttrKeys {
		if val, ok := v.Attributes[key]; ok {
			if s := attrToString(val); s != "" {
				set("variant_attr_"+key, s)
			}
		}
	}

	return row
}

func (s *ExportService) ExportPrices(ctx context.Context, tenantID string) (*ExportResult, error) {
	full, err := s.ExportProducts(ctx, tenantID, map[string]interface{}{})
	if err != nil {
		return nil, err
	}
	headers := []string{"sku", "product_type", "list_price", "currency", "rrp", "cost_price", "sale_price", "sale_start", "sale_end"}
	fi := map[string]int{}
	for i, h := range full.Headers {
		fi[h] = i
	}
	var rows [][]string
	for _, r := range full.Rows {
		sku := sg(r, fi, "sku")
		if sku == "" {
			continue
		}
		rows = append(rows, []string{
			sku, sg(r, fi, "product_type"), sg(r, fi, "list_price"), sg(r, fi, "currency"),
			sg(r, fi, "rrp"), sg(r, fi, "cost_price"), sg(r, fi, "sale_price"),
			sg(r, fi, "sale_start"), sg(r, fi, "sale_end"),
		})
	}
	return &ExportResult{
		Headers:    headers,
		Rows:       rows,
		Total:      len(rows),
		Filename:   FormatExportFilename("prices", "csv"),
		ExportedAt: time.Now().Format(time.RFC3339),
	}, nil
}

func (s *ExportService) ExportStock(ctx context.Context, tenantID string) (*ExportResult, error) {
	full, err := s.ExportProducts(ctx, tenantID, map[string]interface{}{})
	if err != nil {
		return nil, err
	}
	headers := []string{"sku", "product_type", "quantity"}
	fi := map[string]int{}
	for i, h := range full.Headers {
		fi[h] = i
	}
	var rows [][]string
	for _, r := range full.Rows {
		sku := sg(r, fi, "sku")
		if sku == "" {
			continue
		}
		rows = append(rows, []string{sku, sg(r, fi, "product_type"), sg(r, fi, "quantity")})
	}
	return &ExportResult{
		Headers:    headers,
		Rows:       rows,
		Total:      len(rows),
		Filename:   FormatExportFilename("stock", "csv"),
		ExportedAt: time.Now().Format(time.RFC3339),
	}, nil
}

// ============================================================================
// HELPERS
// ============================================================================

func resolveProductSKU(p *models.Product, variants []models.Variant) string {
	if p.Attributes != nil {
		if sku, ok := p.Attributes["source_sku"].(string); ok && sku != "" {
			return sku
		}
		if sku, ok := p.Attributes["sku"].(string); ok && sku != "" {
			return sku
		}
	}
	if p.Identifiers != nil && p.Identifiers.ASIN != nil && *p.Identifiers.ASIN != "" {
		return *p.Identifiers.ASIN
	}
	if len(variants) > 0 {
		base := variants[0].SKU
		parts := strings.Split(base, "-")
		if len(parts) > 1 {
			return strings.Join(parts[:len(parts)-1], "-")
		}
		return base + "-PARENT"
	}
	return p.ProductID
}

func getImageURLs(assets []models.ProductAsset) []string {
	var urls []string
	var primary string
	for _, a := range assets {
		if a.Role == "primary_image" {
			primary = a.URL
		} else {
			urls = append(urls, a.URL)
		}
	}
	if primary != "" {
		urls = append([]string{primary}, urls...)
	}
	return urls
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func attrToString(val interface{}) string {
	if val == nil {
		return ""
	}
	switch v := val.(type) {
	case string:
		return v
	case float64:
		if v == float64(int(v)) {
			return fmt.Sprintf("%d", int(v))
		}
		return fmt.Sprintf("%.2f", v)
	case bool:
		if v {
			return "true"
		}
		return "false"
	case []interface{}:
		var p []string
		for _, i := range v {
			p = append(p, fmt.Sprintf("%v", i))
		}
		return strings.Join(p, "|")
	case []string:
		return strings.Join(v, "|")
	default:
		s := fmt.Sprintf("%v", v)
		if s == "map[]" || s == "[]" || s == "<nil>" {
			return ""
		}
		return s
	}
}

func sortedKeys(m map[string]bool) []string {
	k := make([]string, 0, len(m))
	for key := range m {
		k = append(k, key)
	}
	sort.Strings(k)
	return k
}

func sg(row []string, idx map[string]int, col string) string {
	if i, ok := idx[col]; ok && i < len(row) {
		return row[i]
	}
	return ""
}

func FormatExportFilename(prefix, format string) string {
	return fmt.Sprintf("%s_%s.%s", prefix, time.Now().Format("2006-01-02_150405"), format)
}

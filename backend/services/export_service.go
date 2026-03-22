package services

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
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
// Column layout (matches MarketMate import/export format):
//
//   FIXED COLUMNS (always present):
//     product_id, product_type, parent_sku, sku, title, subtitle, description,
//     brand, status, ean, upc, asin, isbn, mpn, gtin, categories, tags,
//     list_price, currency, rrp, cost_price, sale_price, sale_start, sale_end,
//     quantity, weight_value, weight_unit, length, width, height, dimension_unit,
//
//   VARIANT ATTRIBUTE COLUMNS (name/value pairs, up to MaxVariantAttrCols):
//     variant_attr_1_name, variant_attr_1_value, ..., variant_attr_N_name, variant_attr_N_value
//
//   IMAGE COLUMNS (fixed MaxImageCols slots):
//     image_1 … image_5
//
//   BUNDLE COLUMN:
//     bundle_component_skus
//
//   ATTRIBUTE COLUMNS (one named column per unique attribute key across all products):
//     e.g. amazon_product_type, amazon_status, brand, color, manufacturer, model_number,
//          part_number, size, source_quantity, style, ...
//     Columns are sorted alphabetically. Each row only populates the columns
//     relevant to its product type — other cells are empty (sparse).
//     This format supports full round-trip: export → edit → re-import.
//
//   BACKWARDS COMPAT: attribute_N_name / attribute_N_value pairs are still
//   accepted on import for files exported before this change.
// ============================================================================

const (
	MaxVariantAttrCols = 10 // max variant attribute name/value pairs
	MaxImageCols       = 5  // fixed number of image slots
	MaxAttrCols        = 25 // legacy: kept for import backwards compatibility only
)

type ExportService struct {
	repo *repository.FirestoreRepository
}

func NewExportService(repo *repository.FirestoreRepository) *ExportService {
	return &ExportService{repo: repo}
}

// FixedColumns are the non-dynamic columns that always appear in the export.
var FixedColumns = []string{
	"product_id", "product_type", "parent_sku", "sku",
	"title", "subtitle", "description", "brand", "status",
	"ean", "upc", "asin", "isbn", "mpn", "gtin",
	"categories", "tags",
	"list_price", "currency", "rrp", "cost_price",
	"sale_price", "sale_start", "sale_end",
	"quantity",
	"weight_value", "weight_unit",
	"length", "width", "height", "dimension_unit",
}

// buildHeaders constructs the full ordered header slice.
// attrKeys is the sorted list of all unique attribute keys across all products —
// each becomes its own named column (e.g. "color", "manufacturer", "size").
func buildHeaders(numVariantAttrCols, numImageCols int, attrKeys []string) []string {
	h := make([]string, 0, len(FixedColumns)+numVariantAttrCols*2+numImageCols+1+len(attrKeys))
	h = append(h, FixedColumns...)

	// variant attribute name/value pairs (kept as name/value for variants)
	for i := 1; i <= numVariantAttrCols; i++ {
		h = append(h, fmt.Sprintf("variant_attr_%d_name", i))
		h = append(h, fmt.Sprintf("variant_attr_%d_value", i))
	}

	// images
	for i := 1; i <= numImageCols; i++ {
		h = append(h, fmt.Sprintf("image_%d", i))
	}

	// bundle
	h = append(h, "bundle_component_skus")

	// one named column per unique attribute key (sparse — most cells empty per row)
	h = append(h, attrKeys...)

	return h
}

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

	// ── PASS 1: collect all unique attribute keys and variant attr counts ──
	// Attributes stored internally (not exported as named columns)
	internal := map[string]bool{
		"source_sku": true, "source_price": true, "source_currency": true,
		"source_marketplace": true, "bullet_points": true, "sku": true,
		"fulfillment_channel": true,
	}

	// Collect every unique freeform attribute key across all products.
	// Each unique key becomes its own named column in the export.
	attrKeySet := map[string]bool{}
	maxVariantAttrs := 0

	for _, p := range products {
		if p.Attributes != nil {
			for key := range p.Attributes {
				if !internal[key] && key != "bullet_points" {
					attrKeySet[key] = true
				}
			}
		}
	}
	for _, v := range variants {
		count := len(v.Attributes)
		if count > maxVariantAttrs {
			maxVariantAttrs = count
		}
	}

	// Sort attribute keys for deterministic column ordering
	attrKeys := sortedKeys(attrKeySet)

	// Cap variant attr cols
	numVariantAttrCols := maxVariantAttrs
	if numVariantAttrCols > MaxVariantAttrCols {
		numVariantAttrCols = MaxVariantAttrCols
	}
	if numVariantAttrCols < 1 {
		numVariantAttrCols = 1
	}

	allHeaders := buildHeaders(numVariantAttrCols, MaxImageCols, attrKeys)
	colIdx := map[string]int{}
	for i, h := range allHeaders {
		colIdx[h] = i
	}

	// ── PASS 2: build rows ──
	var rows [][]string
	for _, p := range products {
		if p.ProductType == "variant" {
			continue
		}
		rows = append(rows, productToRow(&p, productSKU, colIdx, allHeaders, numVariantAttrCols, attrKeys, internal))
		if p.ProductType == "parent" {
			psku := productSKU[p.ProductID]
			for _, v := range variantsByProduct[p.ProductID] {
				rows = append(rows, variantToRow(&v, psku, colIdx, allHeaders, numVariantAttrCols))
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

// ============================================================================
// ROW BUILDERS
// ============================================================================

func productToRow(p *models.Product, productSKU map[string]string, colIdx map[string]int, headers []string, numVariantAttrCols int, attrKeys []string, internal map[string]bool) []string {
	row := make([]string, len(headers))
	set := func(col, val string) {
		if idx, ok := colIdx[col]; ok && val != "" {
			row[idx] = val
		}
	}

	set("product_id", p.ProductID)
	set("product_type", p.ProductType)
	set("sku", productSKU[p.ProductID])
	set("title", p.Title)
	set("status", p.Status)
	if p.Subtitle != nil {
		set("subtitle", *p.Subtitle)
	}
	if p.Description != nil {
		set("description", *p.Description)
	}
	if p.Brand != nil {
		set("brand", *p.Brand)
	}

	if p.Identifiers != nil {
		set("ean", derefStr(p.Identifiers.EAN))
		set("upc", derefStr(p.Identifiers.UPC))
		set("asin", derefStr(p.Identifiers.ASIN))
		set("isbn", derefStr(p.Identifiers.ISBN))
		set("mpn", derefStr(p.Identifiers.MPN))
		set("gtin", derefStr(p.Identifiers.GTIN))
	}
	if len(p.CategoryIDs) > 0 {
		set("categories", strings.Join(p.CategoryIDs, "|"))
	}
	if len(p.Tags) > 0 {
		set("tags", strings.Join(p.Tags, "|"))
	}
	if p.Attributes != nil {
		if price, ok := p.Attributes["source_price"].(float64); ok && price > 0 {
			set("list_price", fmt.Sprintf("%.2f", price))
		}
		if curr, ok := p.Attributes["source_currency"].(string); ok {
			set("currency", curr)
		}
	}

	// Images
	for i, url := range getImageURLs(p.Assets) {
		if i >= MaxImageCols {
			break
		}
		set(fmt.Sprintf("image_%d", i+1), url)
	}

	// Bundle components
	if p.ProductType == "bundle" && len(p.BundleComponents) > 0 {
		var parts []string
		for _, bc := range p.BundleComponents {
			csku := productSKU[bc.ProductID]
			if csku == "" {
				csku = bc.ProductID
			}
			parts = append(parts, fmt.Sprintf("%s:%d", csku, bc.Quantity))
		}
		set("bundle_component_skus", strings.Join(parts, "|"))
	}

	// Freeform attributes — write each value into its named column
	if p.Attributes != nil {
		for _, key := range attrKeys {
			if !internal[key] && key != "bullet_points" {
				if s := attrToString(p.Attributes[key]); s != "" {
					set(key, s)
				}
			}
		}
	}

	return row
}

func variantToRow(v *models.Variant, parentSKU string, colIdx map[string]int, headers []string, numVariantAttrCols int) []string {
	row := make([]string, len(headers))
	set := func(col, val string) {
		if idx, ok := colIdx[col]; ok && val != "" {
			row[idx] = val
		}
	}

	set("product_id", v.ProductID)
	set("product_type", "variant")
	set("parent_sku", parentSKU)
	set("sku", v.SKU)
	set("status", v.Status)
	if v.Title != nil {
		set("title", *v.Title)
	}

	if v.Identifiers != nil {
		set("ean", derefStr(v.Identifiers.EAN))
		set("upc", derefStr(v.Identifiers.UPC))
		set("asin", derefStr(v.Identifiers.ASIN))
		set("isbn", derefStr(v.Identifiers.ISBN))
		set("mpn", derefStr(v.Identifiers.MPN))
		set("gtin", derefStr(v.Identifiers.GTIN))
	}
	if v.Pricing != nil {
		if v.Pricing.ListPrice != nil {
			set("list_price", fmt.Sprintf("%.2f", v.Pricing.ListPrice.Amount))
			set("currency", v.Pricing.ListPrice.Currency)
		}
		if v.Pricing.RRP != nil {
			set("rrp", fmt.Sprintf("%.2f", v.Pricing.RRP.Amount))
		}
		if v.Pricing.Cost != nil {
			set("cost_price", fmt.Sprintf("%.2f", v.Pricing.Cost.Amount))
		}
		if v.Pricing.Sale != nil {
			set("sale_price", fmt.Sprintf("%.2f", v.Pricing.Sale.SalePrice.Amount))
			if v.Pricing.Sale.From != nil {
				set("sale_start", v.Pricing.Sale.From.Format("2006-01-02"))
			}
			if v.Pricing.Sale.To != nil {
				set("sale_end", v.Pricing.Sale.To.Format("2006-01-02"))
			}
		}
	}
	if v.Dimensions != nil {
		if v.Dimensions.Length != nil {
			set("length", fmt.Sprintf("%.1f", *v.Dimensions.Length))
		}
		if v.Dimensions.Width != nil {
			set("width", fmt.Sprintf("%.1f", *v.Dimensions.Width))
		}
		if v.Dimensions.Height != nil {
			set("height", fmt.Sprintf("%.1f", *v.Dimensions.Height))
		}
		set("dimension_unit", v.Dimensions.Unit)
	}
	if v.Weight != nil {
		if v.Weight.Value != nil {
			set("weight_value", fmt.Sprintf("%.1f", *v.Weight.Value))
		}
		set("weight_unit", v.Weight.Unit)
	}

	// Variant attributes as name/value pairs
	var attrKeys []string
	for key := range v.Attributes {
		attrKeys = append(attrKeys, key)
	}
	sort.Strings(attrKeys)
	for i, key := range attrKeys {
		slot := i + 1
		if slot > numVariantAttrCols {
			break
		}
		if s := attrToString(v.Attributes[key]); s != "" {
			set(fmt.Sprintf("variant_attr_%d_name", slot), key)
			set(fmt.Sprintf("variant_attr_%d_value", slot), s)
		}
	}

	return row
}

// ============================================================================
// PRICE & STOCK EXPORTS
// ============================================================================

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

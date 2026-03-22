package amazon

// ============================================================================
// SP-API LISTING EXTENSIONS v2
// ============================================================================
// Additional methods for the Amazon listing creation flow:
//   - Product Type Definitions API (JSON Schema for product attributes)
//   - Search Product Types (find matching product types)
//   - Enhanced PutListingItem / PatchListingItem (return raw request + response)
//   - GetListingsItemRaw (return raw JSON for pre-population)
//   - GetListingsRestrictions (check if seller can list an ASIN/category)
//   - ValidateListingPreview (dry-run validation without persisting)
//   - Schema parsing for conditional rules (allOf/if/then)
//
// These methods extend the SPAPIClient defined in sp_api.go.
// ============================================================================

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// ============================================================================
// PRODUCT TYPE DEFINITIONS API
// ============================================================================

// ProductTypeSearchResult represents a product type search result.
// Amazon API may return the type identifier as either "productType" or "name".
type ProductTypeSearchResult struct {
	ProductType    string   `json:"productType"`
	Name           string   `json:"name"`                    // fallback — some API versions use "name"
	DisplayName    string   `json:"displayName"`
	MarketplaceIDs []string `json:"marketplaceIds,omitempty"`
}

// GetProductType returns the product type identifier, preferring productType over name.
func (r ProductTypeSearchResult) GetProductType() string {
	if r.ProductType != "" {
		return r.ProductType
	}
	return r.Name
}

type ProductTypeSearchResponse struct {
	ProductTypes []ProductTypeSearchResult `json:"productTypes"`
}

type ProductTypeDefResponse struct {
	PropertyGroups map[string]PropertyGroup `json:"propertyGroups,omitempty"`
	Locale         string                   `json:"locale,omitempty"`
	MarketplaceIDs []string                 `json:"marketplaceIds,omitempty"`
	ProductType    string                   `json:"productType,omitempty"`
	DisplayName    string                   `json:"displayName,omitempty"`
	Schema         *SchemaLink              `json:"schema,omitempty"`
}

type SchemaLink struct {
	Link     SchemaLinkInfo `json:"link"`
	Checksum string         `json:"checksum"`
}
type SchemaLinkInfo struct {
	Resource string `json:"resource"`
	Verb     string `json:"verb"`
}
type PropertyGroup struct {
	Title         string   `json:"title"`
	Description   string   `json:"description"`
	PropertyNames []string `json:"propertyNames"`
}

// SearchProductTypes searches for product types matching keywords.
// GET /definitions/2020-09-01/productTypes
func (c *SPAPIClient) SearchProductTypes(ctx context.Context, keywords string, itemName string) (*ProductTypeSearchResponse, error) {
	path := "/definitions/2020-09-01/productTypes"
	queryParams := url.Values{}
	queryParams.Set("marketplaceIds", c.config.MarketplaceID)
	if keywords != "" {
		queryParams.Set("keywords", keywords)
	}
	if itemName != "" {
		queryParams.Set("itemName", itemName)
	}

	resp, err := c.makeRequest(ctx, "GET", path, queryParams, nil, c.catalogLimiter)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result ProductTypeSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetProductTypeDefinition fetches ONLY the definition metadata (property groups, display name, schema link).
// Does NOT download the actual JSON Schema — use FetchAndParseSchema for that.
// This keeps memory usage low (~2KB response).
func (c *SPAPIClient) GetProductTypeDefinition(ctx context.Context, productType string, locale string) (*ProductTypeDefResponse, error) {
	path := fmt.Sprintf("/definitions/2020-09-01/productTypes/%s", url.PathEscape(productType))
	queryParams := url.Values{}
	queryParams.Set("marketplaceIds", c.config.MarketplaceID)
	queryParams.Set("sellerId", c.config.SellerID)
	queryParams.Set("requirements", "LISTING")
	queryParams.Set("requirementsEnforced", "ENFORCED")
	if locale == "" {
		locale = "en_GB"
	}
	queryParams.Set("locale", locale)

	resp, err := c.makeRequest(ctx, "GET", path, queryParams, nil, c.catalogLimiter)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var definition ProductTypeDefResponse
	if err := json.Unmarshal(body, &definition); err != nil {
		return nil, err
	}

	return &definition, nil
}

// FetchAndParseSchema downloads the JSON Schema from the schema link,
// parses it into a frontend-friendly ParsedSchemaResult, and then discards
// the raw schema to avoid holding 10-20MB in memory.
// Returns the parsed result (~50-100KB) instead of the raw schema (~5-20MB).
func (c *SPAPIClient) FetchAndParseSchema(ctx context.Context, definition *ProductTypeDefResponse) (*ParsedSchemaResult, error) {
	if definition == nil || definition.Schema == nil || definition.Schema.Link.Resource == "" {
		return nil, fmt.Errorf("no schema link in definition")
	}

	schemaResp, err := c.makeRawRequest(ctx, definition.Schema.Link.Resource)
	if err != nil {
		return nil, fmt.Errorf("fetch schema: %w", err)
	}
	if schemaResp == nil {
		return nil, fmt.Errorf("nil response from schema link")
	}
	defer schemaResp.Body.Close()

	schemaBody, err := io.ReadAll(schemaResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read schema body: %w", err)
	}

	// Parse into map — this is the memory-heavy part, but we discard it after parsing
	var schema map[string]interface{}
	if err := json.Unmarshal(schemaBody, &schema); err != nil {
		return nil, fmt.Errorf("unmarshal schema: %w", err)
	}

	// Parse into frontend-friendly structure (~50-100KB)
	parsed := ParseProductTypeSchema(schema, definition.PropertyGroups)

	// schema and schemaBody are now eligible for GC
	return parsed, nil
}

// ============================================================================
// LISTINGS RESTRICTIONS API
// ============================================================================

// RestrictionReason represents a reason why a listing is restricted.
type RestrictionReason struct {
	ReasonCode string           `json:"reasonCode"` // APPROVAL_REQUIRED, etc.
	Message    string           `json:"message"`
	Links      []RestrictionLink `json:"links,omitempty"`
}

type RestrictionLink struct {
	Resource string `json:"resource"` // URL for approval
	Verb     string `json:"verb"`     // GET
	Title    string `json:"title"`
	Type     string `json:"type"` // text/html
}

// Restriction represents a marketplace restriction for a listing.
type Restriction struct {
	MarketplaceID string              `json:"marketplaceId"`
	ConditionType string              `json:"conditionType,omitempty"`
	Reasons       []RestrictionReason `json:"reasons"`
}

// RestrictionsResponse is the response from the Restrictions API.
type RestrictionsResponse struct {
	Restrictions []Restriction `json:"restrictions"`
}

// GetListingsRestrictions checks whether restrictions exist for an ASIN.
// GET /listings/2021-08-01/restrictions
func (c *SPAPIClient) GetListingsRestrictions(ctx context.Context, asin string, conditionType string) (*RestrictionsResponse, error) {
	path := "/listings/2021-08-01/restrictions"
	queryParams := url.Values{}
	queryParams.Set("asin", asin)
	queryParams.Set("sellerId", c.config.SellerID)
	queryParams.Set("marketplaceIds", c.config.MarketplaceID)
	if conditionType != "" {
		queryParams.Set("conditionType", conditionType)
	}

	resp, err := c.makeRequest(ctx, "GET", path, queryParams, nil, c.listingsLimiter)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result RestrictionsResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ============================================================================
// VALIDATION PREVIEW (dry-run without persisting)
// ============================================================================

// ValidateListingPreview performs a dry-run validation using mode=VALIDATION_PREVIEW.
// Same as PutListingItem but with ?mode=VALIDATION_PREVIEW — returns issues without persisting.
func (c *SPAPIClient) ValidateListingPreview(ctx context.Context, sku string, productType string, attributes map[string]interface{}) (*ListingSubmitResult, error) {
	path := fmt.Sprintf("/listings/2021-08-01/items/%s/%s", c.config.SellerID, url.PathEscape(sku))
	queryParams := url.Values{}
	queryParams.Set("marketplaceIds", c.config.MarketplaceID)
	queryParams.Set("mode", "VALIDATION_PREVIEW")

	reqBody := map[string]interface{}{
		"productType":  productType,
		"requirements": "LISTING",
		"attributes":   attributes,
	}

	return c.submitListingRequest(ctx, "PUT", path, queryParams, reqBody)
}

// ============================================================================
// ENHANCED LISTINGS API — with raw request/response for debug panel
// ============================================================================

// ListingSubmitResult contains both the request sent and the raw response received.
type ListingSubmitResult struct {
	Success      bool                   `json:"success"`
	Status       string                 `json:"status,omitempty"`
	SubmissionID string                 `json:"submissionId,omitempty"`
	Issues       []Issue                `json:"issues,omitempty"`
	SKU          string                 `json:"sku,omitempty"`
	Request      map[string]interface{} `json:"request"`
	Response     map[string]interface{} `json:"response"`
}

// PutListingItem creates or fully replaces a listing using PUT.
// Returns the raw request + response for debug panel.
func (c *SPAPIClient) PutListingItem(ctx context.Context, sku string, productType string, attributes map[string]interface{}, requirements string) (*ListingSubmitResult, error) {
	path := fmt.Sprintf("/listings/2021-08-01/items/%s/%s", c.config.SellerID, url.PathEscape(sku))
	queryParams := url.Values{}
	queryParams.Set("marketplaceIds", c.config.MarketplaceID)

	reqBody := map[string]interface{}{
		"productType":  productType,
		"requirements": requirements,
		"attributes":   attributes,
	}

	return c.submitListingRequest(ctx, "PUT", path, queryParams, reqBody)
}

// PatchOperation for PATCH requests
type PatchOperation struct {
	Op    string      `json:"op"`    // "add", "replace", "delete"
	Path  string      `json:"path"`  // JSON Pointer to the attribute
	Value interface{} `json:"value,omitempty"`
}

// PatchListingItem applies a partial update to an existing listing using PATCH.
func (c *SPAPIClient) PatchListingItem(ctx context.Context, sku string, productType string, patches []PatchOperation) (*ListingSubmitResult, error) {
	path := fmt.Sprintf("/listings/2021-08-01/items/%s/%s", c.config.SellerID, url.PathEscape(sku))
	queryParams := url.Values{}
	queryParams.Set("marketplaceIds", c.config.MarketplaceID)

	reqBody := map[string]interface{}{
		"productType": productType,
		"patches":     patches,
	}

	return c.submitListingRequest(ctx, "PATCH", path, queryParams, reqBody)
}

// DeleteListingItem deletes a listing.
func (c *SPAPIClient) DeleteListingItem(ctx context.Context, sku string) (*ListingSubmitResult, error) {
	path := fmt.Sprintf("/listings/2021-08-01/items/%s/%s", c.config.SellerID, url.PathEscape(sku))
	queryParams := url.Values{}
	queryParams.Set("marketplaceIds", c.config.MarketplaceID)

	return c.submitListingRequest(ctx, "DELETE", path, queryParams, nil)
}

// GetListingsItemRaw returns the raw JSON response for a listing item.
func (c *SPAPIClient) GetListingsItemRaw(ctx context.Context, sku string) (map[string]interface{}, error) {
	path := fmt.Sprintf("/listings/2021-08-01/items/%s/%s", c.config.SellerID, url.PathEscape(sku))
	queryParams := url.Values{}
	queryParams.Set("marketplaceIds", c.config.MarketplaceID)
	queryParams.Set("includedData", "summaries,attributes,issues,fulfillmentAvailability,offers")

	resp, err := c.makeRequest(ctx, "GET", path, queryParams, nil, c.listingsLimiter)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// submitListingRequest handles PUT/PATCH/DELETE and captures both request + response for the debug panel.
func (c *SPAPIClient) submitListingRequest(ctx context.Context, method, path string, queryParams url.Values, body interface{}) (*ListingSubmitResult, error) {
	result := &ListingSubmitResult{
		Request: map[string]interface{}{
			"method": method,
			"path":   path,
		},
	}

	if body != nil {
		result.Request["body"] = body
	}

	// Ensure valid token
	if err := c.ensureValidToken(ctx); err != nil {
		return nil, fmt.Errorf("token refresh failed: %w", err)
	}

	c.listingsLimiter.wait()

	endpoint := c.getEndpoint()
	fullURL := endpoint + path
	if queryParams != nil && len(queryParams) > 0 {
		fullURL += "?" + queryParams.Encode()
	}

	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-amz-access-token", c.token.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var rawResponse map[string]interface{}
	json.Unmarshal(respBody, &rawResponse)
	result.Response = rawResponse

	// Parse standard fields from response
	if status, ok := rawResponse["status"].(string); ok {
		result.Status = status
		result.Success = status == "ACCEPTED" || status == "VALID"
	}
	if subID, ok := rawResponse["submissionId"].(string); ok {
		result.SubmissionID = subID
	}
	if issues, ok := rawResponse["issues"].([]interface{}); ok {
		for _, issue := range issues {
			if m, ok := issue.(map[string]interface{}); ok {
				iss := Issue{
					Code:     fmt.Sprintf("%v", m["code"]),
					Message:  fmt.Sprintf("%v", m["message"]),
					Severity: fmt.Sprintf("%v", m["severity"]),
				}
				if attrNames, ok := m["attributeNames"].([]interface{}); ok {
					for _, an := range attrNames {
						iss.AttributeNames = append(iss.AttributeNames, fmt.Sprintf("%v", an))
					}
				}
				result.Issues = append(result.Issues, iss)
			}
		}
	}

	// If HTTP error status, mark as failure
	if resp.StatusCode >= 400 {
		result.Success = false
		if result.Status == "" {
			result.Status = fmt.Sprintf("HTTP_%d", resp.StatusCode)
		}
	}

	return result, nil
}

// ============================================================================
// SCHEMA PARSER — Extract structured attribute metadata from Amazon JSON Schema
// ============================================================================
// Amazon's JSON Schema is complex: it uses allOf→if/then for conditional rules,
// custom keywords (editable, enumNames, hidden, selectors), nested value objects,
// and property groups. This parser converts it into a flat, frontend-friendly format.

// ParsedAttribute is a frontend-friendly representation of a schema property.
type ParsedAttribute struct {
	Name        string   `json:"name"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Type        string   `json:"type"`            // string, integer, number, boolean, object, array
	Required    bool     `json:"required"`
	Editable    bool     `json:"editable"`         // false = locked after creation
	Hidden      bool     `json:"hidden"`           // Amazon suggests hiding in UI
	Group       string   `json:"group"`            // property group name
	GroupTitle  string   `json:"groupTitle"`        // display title of group

	// Value constraints
	EnumValues  []string `json:"enumValues,omitempty"`
	EnumNames   []string `json:"enumNames,omitempty"` // display labels for enum values
	MaxLength   int      `json:"maxLength,omitempty"`
	MinLength   int      `json:"minLength,omitempty"`
	Minimum     *float64 `json:"minimum,omitempty"`
	Maximum     *float64 `json:"maximum,omitempty"`
	Pattern     string   `json:"pattern,omitempty"`
	Examples    []string `json:"examples,omitempty"`
	MaxItems    int      `json:"maxItems,omitempty"`
	MinItems    int      `json:"minItems,omitempty"`

	// Conditional rules — populated from allOf/if/then
	Conditions []ConditionalRule `json:"conditions,omitempty"`
}

// ConditionalRule represents "if field X equals Y, then this attribute becomes required/gains constraints"
type ConditionalRule struct {
	// The trigger condition
	IfField string `json:"ifField"`    // attribute name that triggers the rule
	IfValue string `json:"ifValue"`    // value that triggers (or "any" for presence check)

	// The effect when condition is met
	ThenRequired    bool     `json:"thenRequired,omitempty"`    // becomes required
	ThenEnumValues  []string `json:"thenEnumValues,omitempty"`  // restricted enum
	ThenMaxLength   int      `json:"thenMaxLength,omitempty"`
	ThenMinLength   int      `json:"thenMinLength,omitempty"`
	ThenDescription string   `json:"thenDescription,omitempty"` // extra description
}

// ParsedSchemaResult is the complete parsed output sent to the frontend.
type ParsedSchemaResult struct {
	Attributes       []ParsedAttribute            `json:"attributes"`
	ConditionalRules []ConditionalRuleFlat         `json:"conditionalRules"` // cross-attribute rules
	GPSRAttributes   []ParsedAttribute            `json:"gpsrAttributes"`   // GPSR-specific fields
	GroupOrder       []string                      `json:"groupOrder"`       // ordered group names
	Groups           map[string]PropertyGroup      `json:"groups"`           // group metadata
}

// ConditionalRuleFlat is a top-level cross-attribute conditional rule.
// "If the user sets <IfField>=<IfValue>, then <ThenFields> become required."
type ConditionalRuleFlat struct {
	IfField    string   `json:"ifField"`
	IfValue    string   `json:"ifValue"`
	ThenFields []string `json:"thenFields"` // attribute names that become required
}

// ParseProductTypeSchema parses an Amazon Product Type Definition JSON Schema
// into a frontend-friendly structure with conditional rules extracted.
func ParseProductTypeSchema(schema map[string]interface{}, propertyGroups map[string]PropertyGroup) *ParsedSchemaResult {
	result := &ParsedSchemaResult{
		Groups: propertyGroups,
	}

	if schema == nil {
		return result
	}

	// Get top-level required fields
	topRequired := extractStringArray(schema, "required")
	topRequiredSet := toSet(topRequired)

	// Parse properties
	properties, _ := schema["properties"].(map[string]interface{})
	if properties == nil {
		return result
	}

	// Core fields handled separately by the form — skip them in dynamic attributes
	coreFields := toSet([]string{
		"item_name", "brand", "bullet_point", "product_description",
		"condition_type", "purchasable_offer", "fulfillment_availability",
		"main_product_image_locator", "other_product_image_locator",
		"externally_assigned_product_identifier", "merchant_suggested_asin",
		"child_parent_sku_relationship", "variation_theme",
	})

	// GPSR attribute names
	gpsrFieldNames := toSet([]string{
		"gpsr_manufacturer_reference", "dsa_responsible_party_address",
		"gpsr_safety_attestation", "compliance_media",
	})

	// Parse allOf conditional blocks
	conditionalRules := parseAllOfConditionals(schema)

	// Build conditionally-required lookup: attribute → which conditions make it required
	condReqMap := map[string][]ConditionalRuleFlat{}
	for _, rule := range conditionalRules {
		for _, f := range rule.ThenFields {
			condReqMap[f] = append(condReqMap[f], rule)
		}
	}
	result.ConditionalRules = conditionalRules

	// Group ordering
	groupOrderMap := map[string]int{}
	groupIdx := 0

	for propName, propDef := range properties {
		def, ok := propDef.(map[string]interface{})
		if !ok {
			continue
		}

		attr := ParsedAttribute{
			Name:     propName,
			Title:    getStringField(def, "title", propName),
			Required: topRequiredSet[propName],
			Editable: true,
			Hidden:   false,
		}

		// Description
		attr.Description = getStringField(def, "description", "")

		// Custom Amazon keywords
		if editable, ok := def["editable"].(bool); ok {
			attr.Editable = editable
		}
		if hidden, ok := def["hidden"].(bool); ok {
			attr.Hidden = hidden
		}

		// Determine type and constraints from the nested structure
		// Amazon attributes are arrays of objects: { items: { properties: { value: { type, enum, ... } } } }
		innerType, innerDef := resolveInnerType(def)
		attr.Type = innerType

		if innerDef != nil {
			attr.EnumValues = extractStringArray(innerDef, "enum")
			attr.EnumNames = extractStringArray(innerDef, "enumNames")
			attr.Examples = extractStringArray(innerDef, "examples")

			if ml, ok := innerDef["maxLength"].(float64); ok {
				attr.MaxLength = int(ml)
			}
			if ml, ok := innerDef["minLength"].(float64); ok {
				attr.MinLength = int(ml)
			}
			if min, ok := innerDef["minimum"].(float64); ok {
				attr.Minimum = &min
			}
			if max, ok := innerDef["maximum"].(float64); ok {
				attr.Maximum = &max
			}
			if p, ok := innerDef["pattern"].(string); ok {
				attr.Pattern = p
			}
		}

		// MaxItems / MinItems (on the array itself)
		if mi, ok := def["maxItems"].(float64); ok {
			attr.MaxItems = int(mi)
		}
		if mi, ok := def["minItems"].(float64); ok {
			attr.MinItems = int(mi)
		}

		// Attach conditional rules for this attribute
		if rules, ok := condReqMap[propName]; ok {
			for _, r := range rules {
				attr.Conditions = append(attr.Conditions, ConditionalRule{
					IfField:      r.IfField,
					IfValue:      r.IfValue,
					ThenRequired: true,
				})
			}
		}

		// Find group
		attr.Group = "other"
		attr.GroupTitle = "Other Attributes"
		for gName, gDef := range propertyGroups {
			for _, pn := range gDef.PropertyNames {
				if pn == propName {
					attr.Group = gName
					attr.GroupTitle = gDef.Title
					if _, exists := groupOrderMap[gName]; !exists {
						groupOrderMap[gName] = groupIdx
						groupIdx++
					}
					break
				}
			}
		}
		if attr.Group == "other" {
			if _, exists := groupOrderMap["other"]; !exists {
				groupOrderMap["other"] = 999
			}
		}

		// Route to the right bucket
		if gpsrFieldNames[propName] {
			result.GPSRAttributes = append(result.GPSRAttributes, attr)
		} else if !coreFields[propName] {
			result.Attributes = append(result.Attributes, attr)
		}
	}

	// Sort attributes: required first, then by group, then alphabetical
	sort.SliceStable(result.Attributes, func(i, j int) bool {
		a, b := result.Attributes[i], result.Attributes[j]
		if a.Required != b.Required {
			return a.Required
		}
		if a.Group != b.Group {
			return groupOrderMap[a.Group] < groupOrderMap[b.Group]
		}
		return a.Title < b.Title
	})

	// Build group order
	type groupSort struct {
		name string
		idx  int
	}
	var gs []groupSort
	for k, v := range groupOrderMap {
		gs = append(gs, groupSort{k, v})
	}
	sort.Slice(gs, func(i, j int) bool { return gs[i].idx < gs[j].idx })
	for _, g := range gs {
		result.GroupOrder = append(result.GroupOrder, g.name)
	}

	return result
}

// parseAllOfConditionals extracts if/then conditional rules from the schema's allOf block.
// Each allOf entry may contain { "if": { "properties": { "X": ... } }, "then": { "required": [...] } }
func parseAllOfConditionals(schema map[string]interface{}) []ConditionalRuleFlat {
	allOf, ok := schema["allOf"].([]interface{})
	if !ok {
		return nil
	}

	var rules []ConditionalRuleFlat
	for _, entry := range allOf {
		entryMap, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}

		ifBlock, hasIf := entryMap["if"].(map[string]interface{})
		thenBlock, hasThen := entryMap["then"].(map[string]interface{})
		if !hasIf || !hasThen {
			continue
		}

		// Parse the "if" condition
		ifProps, ok := ifBlock["properties"].(map[string]interface{})
		if !ok {
			continue
		}

		// We handle the most common pattern: single field with const or enum value
		for fieldName, fieldDef := range ifProps {
			fieldMap, ok := fieldDef.(map[string]interface{})
			if !ok {
				continue
			}

			ifValue := extractIfValue(fieldMap)
			if ifValue == "" {
				continue
			}

			// Parse the "then" block for required fields and properties
			thenRequired := extractStringArray(thenBlock, "required")

			// Also check if then.properties adds new constraints
			if thenProps, ok := thenBlock["properties"].(map[string]interface{}); ok {
				for propName := range thenProps {
					// If it appears in then.properties but not in then.required, it's still relevant
					found := false
					for _, r := range thenRequired {
						if r == propName {
							found = true
							break
						}
					}
					if !found {
						// Add as conditionally relevant (not strictly required, but enabled)
						thenRequired = append(thenRequired, propName)
					}
				}
			}

			if len(thenRequired) > 0 {
				rules = append(rules, ConditionalRuleFlat{
					IfField:    fieldName,
					IfValue:    ifValue,
					ThenFields: thenRequired,
				})
			}
		}
	}

	return rules
}

// extractIfValue extracts the trigger value from an if property definition.
// Handles patterns like:
//   - { "items": { "properties": { "value": { "const": "true" } } } }
//   - { "const": "some_value" }
//   - { "enum": ["val1", "val2"] }
//   - { "items": { "properties": { "value": { "enum": [...] } } } }
func extractIfValue(fieldDef map[string]interface{}) string {
	// Direct const
	if c, ok := fieldDef["const"].(string); ok {
		return c
	}
	if c, ok := fieldDef["const"].(bool); ok {
		if c {
			return "true"
		}
		return "false"
	}

	// Direct enum → join with |
	if enumArr := extractStringArray(fieldDef, "enum"); len(enumArr) > 0 {
		return strings.Join(enumArr, "|")
	}

	// Nested Amazon pattern: items.properties.value.const
	items, ok := fieldDef["items"].(map[string]interface{})
	if !ok {
		return ""
	}
	props, ok := items["properties"].(map[string]interface{})
	if !ok {
		return ""
	}
	valueDef, ok := props["value"].(map[string]interface{})
	if !ok {
		return ""
	}

	// Nested const
	if c, ok := valueDef["const"].(string); ok {
		return c
	}
	if c, ok := valueDef["const"].(bool); ok {
		if c {
			return "true"
		}
		return "false"
	}

	// Nested enum
	if enumArr := extractStringArray(valueDef, "enum"); len(enumArr) > 0 {
		return strings.Join(enumArr, "|")
	}

	return ""
}

// resolveInnerType navigates the nested Amazon attribute structure to find the actual value type.
// Amazon attributes are typically: { type: "array", items: { properties: { value: { type: "string", ... } } } }
func resolveInnerType(propDef map[string]interface{}) (string, map[string]interface{}) {
	// Direct type at top level
	if t, ok := propDef["type"].(string); ok && t != "array" {
		return t, propDef
	}

	// Navigate array → items → properties → value
	items, ok := propDef["items"].(map[string]interface{})
	if !ok {
		if t, ok := propDef["type"].(string); ok {
			return t, propDef
		}
		return "string", nil
	}

	props, ok := items["properties"].(map[string]interface{})
	if !ok {
		// Simple array items with direct type
		if t, ok := items["type"].(string); ok {
			return t, items
		}
		return "string", nil
	}

	valueDef, ok := props["value"].(map[string]interface{})
	if !ok {
		// Object-type attribute (has properties but no "value" sub-property)
		return "object", items
	}

	innerType := "string"
	if t, ok := valueDef["type"].(string); ok {
		innerType = t
	}

	return innerType, valueDef
}

// ── Helpers ──

func extractStringArray(m map[string]interface{}, key string) []string {
	arr, ok := m[key].([]interface{})
	if !ok {
		return nil
	}
	var result []string
	for _, v := range arr {
		if s, ok := v.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

func getStringField(m map[string]interface{}, key, fallback string) string {
	if v, ok := m[key].(string); ok && v != "" {
		return v
	}
	return fallback
}

func toSet(arr []string) map[string]bool {
	m := make(map[string]bool, len(arr))
	for _, v := range arr {
		m[v] = true
	}
	return m
}

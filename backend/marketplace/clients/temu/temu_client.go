package temu

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

// ============================================================================
// TEMU API CLIENT
// ============================================================================
// Handles signing, HTTP POST, and all Temu Open Platform API calls.
// All Temu API interactions use POST with JSON body.
// The specific API is identified by the "type" parameter.
//
// Egress proxy:
//   If EGRESS_PROXY_URL is set, all outbound calls are routed through it
//   so they originate from the whitelisted static IP in us-central1.
// ============================================================================

// Regional API base URLs
const (
	TemuBaseURLUS = "https://openapi-b.temu.com/openapi/router"
	TemuBaseURLEU = "https://openapi-b-eu.temu.com/openapi/router"
)

// detailDiagOnce ensures the goods detail diagnostic log fires exactly once per process.
var detailDiagOnce atomic.Bool

var (
	egressProxyURL    = os.Getenv("EGRESS_PROXY_URL")
	egressProxySecret = os.Getenv("EGRESS_PROXY_SECRET")
)

// Client is the low-level Temu API client
type Client struct {
	BaseURL     string
	AppKey      string
	AppSecret   string
	AccessToken string
	HTTPClient  *http.Client
}

// NewClient creates a new Temu API client
func NewClient(baseURL, appKey, appSecret, accessToken string) *Client {
	return &Client{
		BaseURL:     baseURL,
		AppKey:      appKey,
		AppSecret:   appSecret,
		AccessToken: accessToken,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// APIResponse is the standard Temu response wrapper
type APIResponse struct {
	Success   bool                   `json:"success"`
	ErrorCode int                    `json:"errorCode,omitempty"`
	ErrorMsg  string                 `json:"errorMsg,omitempty"`
	Result    json.RawMessage        `json:"result,omitempty"`
	RequestID string                 `json:"requestId,omitempty"`
	Raw       map[string]interface{} `json:"-"` // Full raw response
}

// Post sends a signed request to the Temu API
// doPost sends a POST request to targetURL with the given JSON body,
// routing through the egress proxy if EGRESS_PROXY_URL is set.
func (c *Client) doPost(targetURL, bodyStr string) (*http.Response, error) {
	if egressProxyURL == "" {
		// Direct call
		req, err := http.NewRequest("POST", targetURL, strings.NewReader(bodyStr))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		return c.HTTPClient.Do(req)
	}

	// Route through egress proxy
	proxyPayload, _ := json.Marshal(map[string]interface{}{
		"url":    targetURL,
		"method": "POST",
		"headers": map[string]string{
			"Content-Type": "application/json",
		},
		"body": bodyStr,
	})
	proxyReq, err := http.NewRequest("POST", egressProxyURL+"/forward", bytes.NewReader(proxyPayload))
	if err != nil {
		return nil, err
	}
	proxyReq.Header.Set("Content-Type", "application/json")
	if egressProxySecret != "" {
		proxyReq.Header.Set("X-Proxy-Secret", egressProxySecret)
	}
	return c.HTTPClient.Do(proxyReq)
}

func (c *Client) doPostCtx(ctx context.Context, targetURL, bodyStr string) (*http.Response, error) {
	if egressProxyURL == "" {
		// Direct call
		req, err := http.NewRequestWithContext(ctx, "POST", targetURL, strings.NewReader(bodyStr))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		return c.HTTPClient.Do(req)
	}

	// Route through egress proxy
	proxyPayload, _ := json.Marshal(map[string]interface{}{
		"url":    targetURL,
		"method": "POST",
		"headers": map[string]string{
			"Content-Type": "application/json",
		},
		"body": bodyStr,
	})
	proxyReq, err := http.NewRequestWithContext(ctx, "POST", egressProxyURL+"/forward", bytes.NewReader(proxyPayload))
	if err != nil {
		return nil, err
	}
	proxyReq.Header.Set("Content-Type", "application/json")
	if egressProxySecret != "" {
		proxyReq.Header.Set("X-Proxy-Secret", egressProxySecret)
	}
	return c.HTTPClient.Do(proxyReq)
}

func (c *Client) Post(params map[string]interface{}) (*APIResponse, error) {
	// Add common parameters
	params["app_key"] = c.AppKey
	params["access_token"] = c.AccessToken
	params["data_type"] = "JSON"
	params["timestamp"] = time.Now().Unix()

	// Sign the request
	params["sign"] = c.sign(params)

	// Marshal to JSON
	body, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	apiType := ""
	if t, ok := params["type"].(string); ok {
		apiType = t
	}

	// DEBUG: Log full request only when TEMU_DEBUG=true env var is set.
	// Do NOT enable permanently — 579 products × 2 log lines × 2KB = log flood.
	if os.Getenv("TEMU_DEBUG") == "true" {
		debugParams := make(map[string]interface{})
		for k, v := range params {
			if k == "access_token" || k == "sign" || k == "app_key" {
				s := fmt.Sprintf("%v", v)
				if len(s) > 8 {
					debugParams[k] = s[:4] + "****" + s[len(s)-4:]
				} else {
					debugParams[k] = "****"
				}
			} else {
				debugParams[k] = v
			}
		}
		debugJSON, _ := json.MarshalIndent(debugParams, "", "  ")
		log.Printf("[Temu DEBUG] >>> REQUEST %s\nURL: %s\nBody: %s", apiType, c.BaseURL, string(debugJSON))
	}

	// POST request — route through egress proxy if configured
	resp, err := c.doPost(c.BaseURL, string(body))
	if err != nil {
		return nil, fmt.Errorf("HTTP POST %s: %w", apiType, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	// DEBUG: Log full response only when TEMU_DEBUG=true
	if os.Getenv("TEMU_DEBUG") == "true" {
		respStr := string(respBody)
		if len(respStr) > 2000 {
			respStr = respStr[:2000] + "... [TRUNCATED]"
		}
		log.Printf("[Temu DEBUG] <<< RESPONSE %s\nHTTP Status: %d\nBody: %s", apiType, resp.StatusCode, respStr)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d from Temu API %s: %s", resp.StatusCode, apiType, string(respBody[:minInt(len(respBody), 500)]))
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w (body: %s)", err, string(respBody[:minInt(len(respBody), 500)]))
	}

	// Also store raw response
	var raw map[string]interface{}
	json.Unmarshal(respBody, &raw)
	apiResp.Raw = raw

	if !apiResp.Success {
		log.Printf("[Temu] API %s failed: code=%d msg=%s", apiType, apiResp.ErrorCode, apiResp.ErrorMsg)
	}

	return &apiResp, nil
}

// PostCtx is identical to Post but attaches ctx to the HTTP request so the
// call can be cancelled via context deadline. Used by GetCategoriesCtx.
func (c *Client) PostCtx(ctx context.Context, params map[string]interface{}) (*APIResponse, error) {
	params["app_key"] = c.AppKey
	params["access_token"] = c.AccessToken
	params["data_type"] = "JSON"
	params["timestamp"] = time.Now().Unix()
	params["sign"] = c.sign(params)

	body, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	resp, err := c.doPostCtx(ctx, c.BaseURL, string(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d from Temu API: %s", resp.StatusCode, string(respBody[:minInt(len(respBody), 200)]))
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	return &apiResp, nil
}

// GetCategoriesCtx fetches child categories with a context deadline so the
// call cannot hang indefinitely regardless of proxy or upstream behaviour.
func (c *Client) GetCategoriesCtx(ctx context.Context, parentID *int) ([]TemuCategory, error) {
	params := map[string]interface{}{
		"type": "bg.local.goods.cats.get",
	}
	if parentID != nil {
		params["parentCatId"] = *parentID
	}

	resp, err := c.PostCtx(ctx, params)
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		// Try alternative parameter name
		if parentID != nil {
			params2 := map[string]interface{}{
				"type":     "bg.local.goods.cats.get",
				"parentId": *parentID,
			}
			resp2, err := c.PostCtx(ctx, params2)
			if err != nil {
				return nil, err
			}
			if !resp2.Success {
				return nil, fmt.Errorf("get categories: %s", resp2.ErrorMsg)
			}
			resp = resp2
		} else {
			return nil, fmt.Errorf("get categories: %s", resp.ErrorMsg)
		}
	}

	var result struct {
		GoodsCatsList []TemuCategory `json:"goodsCatsList"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		var cats []TemuCategory
		if err2 := json.Unmarshal(resp.Result, &cats); err2 != nil {
			return nil, fmt.Errorf("parse categories: %w", err)
		}
		return cats, nil
	}
	return result.GoodsCatsList, nil
}


// sign generates the MD5 signature required by Temu API.
//
// Algorithm: UPPER(MD5(secret + sorted(key + value_string) + secret))
//
// For nested objects and arrays, values are serialised to JSON without HTML
// escaping. Go's json.Marshal escapes &, <, > as \u0026, \u003c, \u003e by
// default, but Temu's reference implementation (Python) uses
// json.dumps(ensure_ascii=False) which keeps these characters literal.
// Using SetEscapeHTML(false) matches Python's behaviour exactly.
func (c *Client) sign(params map[string]interface{}) string {
	flat := make(map[string]string)
	for k, v := range params {
		if k == "sign" || v == nil {
			continue
		}
		rv := reflect.ValueOf(v)
		kind := rv.Kind()
		if kind == reflect.Map || kind == reflect.Slice || kind == reflect.Array {
			// Use SetEscapeHTML(false) to match Python's json.dumps behaviour:
			// & stays as &, < stays as <, > stays as >
			var buf bytes.Buffer
			enc := json.NewEncoder(&buf)
			enc.SetEscapeHTML(false)
			if err := enc.Encode(v); err != nil {
				continue
			}
			// Encode adds a trailing newline — trim it
			flat[k] = strings.TrimRight(buf.String(), "\n")
		} else {
			flat[k] = fmt.Sprintf("%v", v)
		}
	}

	keys := make([]string, 0, len(flat))
	for k := range flat {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	sb.WriteString(c.AppSecret)
	for _, k := range keys {
		sb.WriteString(k)
		sb.WriteString(flat[k])
	}
	sb.WriteString(c.AppSecret)

	hash := md5.Sum([]byte(sb.String()))
	return strings.ToUpper(fmt.Sprintf("%x", hash))
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ============================================================================
// CATEGORY APIs
// ============================================================================

// TemuCategory represents a Temu product category
type TemuCategory struct {
	CatID    int      `json:"catId"`
	CatName  string   `json:"catName"`
	ParentID int      `json:"parentId"`
	Leaf     bool     `json:"leaf"`
	Level    int      `json:"level,omitempty"`
	CatPath  []string `json:"catPath,omitempty"`
}

// RecommendCategory asks Temu to recommend categories based on product name.
//
// The bg.local.goods.category.recommend API returns a flat list of integer
// catIds in result.catIdList — NOT full category objects. Each returned catId
// is typically a mid-level (non-leaf) category. To get leaf categories we must
// call bg.local.goods.cats.get(parentCatId=catId) for each returned id and
// recurse until we hit leaf nodes (leaf=true).
//
// This matches the Temu API documentation:
//   "To obtain leaf categories: recursively call bg.local.goods.cats.get,
//    entering parentCatId with the catId from recommend's results, until
//    the leaf categories are obtained."
func (c *Client) RecommendCategory(goodsName string) ([]TemuCategory, error) {
	params := map[string]interface{}{
		"type":      "bg.local.goods.category.recommend",
		"goodsName": goodsName,
	}

	resp, err := c.Post(params)
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("recommend category: %s", resp.ErrorMsg)
	}

	// The API returns { "catIdList": [int, int, ...] }
	var result struct {
		CatIdList []int `json:"catIdList"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil || len(result.CatIdList) == 0 {
		log.Printf("[Temu RecommendCategory] catIdList empty or unparseable. Raw result: %s", string(resp.Result))
		return nil, nil
	}

	log.Printf("[Temu RecommendCategory] Got %d catId(s) from recommend: %v", len(result.CatIdList), result.CatIdList)

	// For each returned catId, resolve to leaf categories via cats.get.
	// Deduplicate so we don't make redundant API calls.
	seen := make(map[int]bool)
	var leafCats []TemuCategory

	for _, catId := range result.CatIdList {
		if seen[catId] {
			continue
		}
		seen[catId] = true

		leaves, err := c.resolveToLeaves(catId, 0, seen)
		if err != nil {
			log.Printf("[Temu RecommendCategory] resolveToLeaves(%d) error: %v", catId, err)
			continue
		}
		leafCats = append(leafCats, leaves...)
	}

	log.Printf("[Temu RecommendCategory] Resolved to %d leaf categor(ies)", len(leafCats))
	return leafCats, nil
}

// resolveToLeaves calls bg.local.goods.cats.get with the given catId as
// parentCatId. If the children are leaf nodes they are returned directly.
// If they are not leaves, each child is recursed (up to maxDepth levels).
// ancestorPath carries the names of all ancestors above this catId so that
// every returned leaf has a fully populated CatPath and CatName.
func (c *Client) resolveToLeaves(catId int, depth int, seen map[int]bool) ([]TemuCategory, error) {
	return c.resolveToLeavesWithPath(catId, depth, seen, []string{})
}

func (c *Client) resolveToLeavesWithPath(catId int, depth int, seen map[int]bool, ancestorPath []string) ([]TemuCategory, error) {
	const maxDepth = 5
	if depth >= maxDepth {
		log.Printf("[Temu resolveToLeaves] max depth reached at catId=%d", catId)
		return nil, nil
	}

	children, err := c.GetCategories(&catId)
	if err != nil {
		return nil, fmt.Errorf("cats.get(parentCatId=%d): %w", catId, err)
	}

	log.Printf("[Temu resolveToLeaves] catId=%d depth=%d returned %d children", catId, depth, len(children))

	// If cats.get returns no children, this catId IS itself a leaf.
	// We don't have its name here (recommend only gave us the int), so
	// return it with whatever path we have — the handler will enrich it.
	if len(children) == 0 {
		return []TemuCategory{{CatID: catId, Leaf: true, CatPath: ancestorPath}}, nil
	}

	var leaves []TemuCategory
	for _, child := range children {
		// Build this child's full path = ancestors + child's own name
		childPath := make([]string, len(ancestorPath), len(ancestorPath)+1)
		copy(childPath, ancestorPath)
		if child.CatName != "" {
			childPath = append(childPath, child.CatName)
		}

		if child.Leaf {
			child.CatPath = childPath
			leaves = append(leaves, child)
		} else {
			if seen[child.CatID] {
				continue
			}
			seen[child.CatID] = true
			nested, err := c.resolveToLeavesWithPath(child.CatID, depth+1, seen, childPath)
			if err != nil {
				log.Printf("[Temu resolveToLeaves] error resolving child catId=%d: %v", child.CatID, err)
				continue
			}
			leaves = append(leaves, nested...)
		}
	}
	return leaves, nil
}

// GetCategories fetches Temu category tree. Pass parentID=0 or -1 for root categories.
func (c *Client) GetCategories(parentID *int) ([]TemuCategory, error) {
	params := map[string]interface{}{
		"type": "bg.local.goods.cats.get",
	}
	if parentID != nil {
		params["parentCatId"] = *parentID
	}

	resp, err := c.Post(params)
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		// Try alternative parameter name
		if parentID != nil {
			params["parentId"] = *parentID
			delete(params, "parentCatId")
			resp, err = c.Post(params)
			if err != nil {
				return nil, err
			}
		}
		if !resp.Success {
			return nil, fmt.Errorf("get categories: %s", resp.ErrorMsg)
		}
	}

	// Parse result — Temu nests under result.goodsCatsList
	var result struct {
		GoodsCatsList []TemuCategory `json:"goodsCatsList"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		// Try direct array
		var cats []TemuCategory
		if err2 := json.Unmarshal(resp.Result, &cats); err2 != nil {
			return nil, fmt.Errorf("parse categories: %w", err)
		}
		return cats, nil
	}
	return result.GoodsCatsList, nil
}

// GetCategoryPath resolves the full category path from leaf to root.
// Returns path like ["Toys", "Playsets", "Animal Playsets"]
// Strategy: Given catId, we walk upward. Temu categories have parentId.
// We fetch siblings at each level to find the current cat's name and parentId.
func (c *Client) GetCategoryPath(leafCatID int) ([]string, error) {
	var path []string
	currentID := leafCatID
	seen := make(map[int]bool)

	for i := 0; i < 10; i++ { // Max 10 levels deep
		if currentID == 0 || seen[currentID] {
			break
		}
		seen[currentID] = true

		// Fetch all cats and search — Temu's cats.get with no parent returns roots
		// We need to find currentID somewhere. Try fetching the parent's children.
		// Problem: we don't know the parent yet for the first iteration.
		// Workaround: use a separate detail call or just return catId-only path

		// Actually the simplest reliable approach:
		// The recommend API and template fetch already give us category info.
		// For the path, we'll build it during prepare by walking the tree.
		// For now, just return a single-element path.
		break
	}

	if len(path) == 0 {
		path = []string{fmt.Sprintf("Category %d", leafCatID)}
	}
	return path, nil
}

// BuildCategoryPath builds path by walking up from leaf using parentId.
// Requires a pre-built lookup map of catId -> TemuCategory.
func BuildCategoryPath(leafCatID int, lookup map[int]TemuCategory) []string {
	var path []string
	cur := leafCatID
	seen := make(map[int]bool)

	for i := 0; i < 10; i++ {
		if cur == 0 || seen[cur] {
			break
		}
		seen[cur] = true
		cat, ok := lookup[cur]
		if !ok {
			break
		}
		path = append([]string{cat.CatName}, path...)
		cur = cat.ParentID
	}
	return path
}

// GetComplianceRules fetches compliance requirements for a category
func (c *Client) GetComplianceRules(catID int) (map[string]interface{}, error) {
	params := map[string]interface{}{
		"type":  "bg.local.goods.compliance.rules.get",
		"catId": catID,
	}

	resp, err := c.Post(params)
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("get compliance rules: %s", resp.ErrorMsg)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("parse compliance rules: %w", err)
	}
	return result, nil
}

// ============================================================================
// TEMPLATE APIs
// ============================================================================

// GetTemplate fetches the attribute template for a leaf category
func (c *Client) GetTemplate(catID int) (map[string]interface{}, error) {
	params := map[string]interface{}{
		"type":  "bg.local.goods.template.get",
		"catId": catID,
	}

	resp, err := c.Post(params)
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("get template: %s", resp.ErrorMsg)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}
	return result, nil
}

// ============================================================================
// IMAGE UPLOAD
// ============================================================================

// UploadImage uploads an image URL to Temu CDN and returns the Temu-hosted URL
func (c *Client) UploadImage(imageURL string) (string, error) {
	// Skip if already on Temu CDN
	lower := strings.ToLower(imageURL)
	if strings.Contains(lower, "kwcdn.com") || strings.Contains(lower, "temuimg") ||
		strings.Contains(lower, "temucdn") || strings.Contains(lower, "pddpic") {
		return imageURL, nil
	}

	params := map[string]interface{}{
		"type":                 "bg.local.goods.image.upload",
		"fileUrl":              imageURL,
		"scalingType":          1,
		"compressionType":      1,
		"formatConversionType": 1,
	}

	resp, err := c.Post(params)
	if err != nil {
		return "", fmt.Errorf("upload image: %w", err)
	}
	if !resp.Success {
		return "", fmt.Errorf("image upload failed: %s", resp.ErrorMsg)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return "", fmt.Errorf("parse image result: %w", err)
	}

	// Extract URL from result — try multiple field names
	for _, key := range []string{"imageUrl", "url", "fileUrl"} {
		if url, ok := result[key].(string); ok && url != "" {
			return url, nil
		}
	}

	return "", fmt.Errorf("image upload response missing URL")
}

// UploadImages uploads multiple images and returns Temu-hosted URLs
func (c *Client) UploadImages(imageURLs []string) ([]string, error) {
	var temuURLs []string
	for _, url := range imageURLs {
		temuURL, err := c.UploadImage(url)
		if err != nil {
			log.Printf("[Temu] WARNING: failed to upload image %s: %v", url, err)
			continue
		}
		temuURLs = append(temuURLs, temuURL)
	}
	return temuURLs, nil
}

// ============================================================================
// SPEC ID RESOLUTION
// ============================================================================

// GetSpecIDs resolves spec IDs for SKU variants
func (c *Client) GetSpecIDs(catID int, parentSpecID int, childSpecName string) ([]int, error) {
	params := map[string]interface{}{
		"type":           "bg.local.goods.spec.id.get",
		"catId":          catID,
		"parentSpecId":   parentSpecID,
		"childSpecName":  childSpecName,
	}

	resp, err := c.Post(params)
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("get spec IDs: %s", resp.ErrorMsg)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, err
	}

	log.Printf("[Temu Spec] spec.id.get response for catId=%d parentSpecId=%d label=%q: %v", catID, parentSpecID, childSpecName, result)

	// Try multiple possible response fields (Temu is inconsistent)
	for _, key := range []string{"specIdList", "idList", "skuSpecIdList"} {
		if list, ok := result[key].([]interface{}); ok && len(list) > 0 {
			ids := []int{}
			for _, v := range list {
				if id, ok := v.(float64); ok {
					ids = append(ids, int(id))
				}
			}
			if len(ids) > 0 {
				return ids, nil
			}
		}
	}

	// Also check nested data.specIdList
	if data, ok := result["data"].(map[string]interface{}); ok {
		if list, ok := data["specIdList"].([]interface{}); ok && len(list) > 0 {
			ids := []int{}
			for _, v := range list {
				if id, ok := v.(float64); ok {
					ids = append(ids, int(id))
				}
			}
			if len(ids) > 0 {
				return ids, nil
			}
		}
	}

	// Single specId field
	if specID, ok := result["specId"]; ok {
		if id, ok := specID.(float64); ok {
			return []int{int(id)}, nil
		}
	}

	return nil, fmt.Errorf("no spec ID returned for catId=%d parentSpecId=%d label=%q", catID, parentSpecID, childSpecName)
}

// ============================================================================
// SHIPPING TEMPLATES
// ============================================================================

// ShippingTemplate represents a Temu freight/shipping template
type ShippingTemplate struct {
	TemplateID   string `json:"templateId"`
	TemplateName string `json:"templateName"`
}

// GetShippingTemplates fetches available shipping/freight templates
func (c *Client) GetShippingTemplates() ([]ShippingTemplate, string, error) {
	params := map[string]interface{}{
		"type":     "bg.freight.template.list.query",
		"pageNo":   1,
		"pageSize": 200,
	}

	resp, err := c.Post(params)
	if err != nil {
		return nil, "", err
	}
	if !resp.Success {
		return nil, "", fmt.Errorf("get shipping templates: %s", resp.ErrorMsg)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, "", err
	}

	defaultID := ""
	if d, ok := result["defaultTemplateId"]; ok {
		defaultID = fmt.Sprintf("%v", d)
	}

	var templates []ShippingTemplate
	if list, ok := result["templateList"].([]interface{}); ok {
		for _, item := range list {
			if m, ok := item.(map[string]interface{}); ok {
				t := ShippingTemplate{}
				if id, ok := m["templateId"]; ok {
					t.TemplateID = fmt.Sprintf("%v", id)
				} else if id, ok := m["id"]; ok {
					t.TemplateID = fmt.Sprintf("%v", id)
				}
				if name, ok := m["templateName"]; ok {
					t.TemplateName = fmt.Sprintf("%v", name)
				} else if name, ok := m["name"]; ok {
					t.TemplateName = fmt.Sprintf("%v", name)
				}
				if t.TemplateID != "" {
					templates = append(templates, t)
				}
			}
		}
	}

	return templates, defaultID, nil
}

// ============================================================================
// BRAND / TRADEMARK
// ============================================================================

// BrandTrademark represents a Temu brand authorization
type BrandTrademark struct {
	BrandID     int    `json:"brandId"`
	BrandName   string `json:"brandName,omitempty"`
	TrademarkID int    `json:"trademarkId,omitempty"`
}

// LookupBrandTrademark searches for brand authorization in the seller's shop
func (c *Client) LookupBrandTrademark(brandID *int, brandName *string) (*BrandTrademark, error) {
	params := map[string]interface{}{
		"type":    "temu.local.goods.brand.trademark.V2.get",
		"version": "V2",
		"page":    1,
		"size":    20,
	}

	reqObj := make(map[string]interface{})
	if brandID != nil {
		reqObj["brandId"] = *brandID
	}
	if brandName != nil && *brandName != "" {
		reqObj["brandName"] = *brandName
	}
	if len(reqObj) > 0 {
		params["request"] = reqObj
	}

	resp, err := c.Post(params)
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("brand lookup: %s", resp.ErrorMsg)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, err
	}

	// Find trademark list
	var rows []interface{}
	for _, key := range []string{"trademarkList", "list", "records", "items"} {
		if list, ok := result[key].([]interface{}); ok {
			rows = list
			break
		}
	}

	if len(rows) == 0 {
		return nil, nil // No brand found
	}

	// Find best match
	for _, row := range rows {
		m, ok := row.(map[string]interface{})
		if !ok {
			continue
		}
		bt := &BrandTrademark{}
		if bid, ok := m["brandId"].(float64); ok {
			bt.BrandID = int(bid)
		}
		if bn, ok := m["brandName"].(string); ok {
			bt.BrandName = bn
		}
		if tid, ok := m["trademarkId"].(float64); ok {
			bt.TrademarkID = int(tid)
		}

		// Check for match
		if brandID != nil && bt.BrandID == *brandID {
			return bt, nil
		}
		if brandName != nil && strings.EqualFold(bt.BrandName, *brandName) {
			return bt, nil
		}
	}

	// Return first result as fallback
	m := rows[0].(map[string]interface{})
	bt := &BrandTrademark{}
	if bid, ok := m["brandId"].(float64); ok {
		bt.BrandID = int(bid)
	}
	if tid, ok := m["trademarkId"].(float64); ok {
		bt.TrademarkID = int(tid)
	}
	return bt, nil
}

// BrandTrademarkFull includes all three IDs needed for submission
type BrandTrademarkFull struct {
	BrandID        int64  `json:"brandId"`
	BrandName      string `json:"brandName"`
	TrademarkID    int64  `json:"trademarkId"`
	TrademarkBizID int64  `json:"trademarkBizId"`
}

// ListAllBrands returns all authorized brands for the seller (no filter).
// Paginated — fetches up to 200 brands across multiple pages.
func (c *Client) ListAllBrands() ([]BrandTrademarkFull, error) {
	var allBrands []BrandTrademarkFull
	page := 1
	pageSize := 50

	for {
		// Try bg.local.goods.brands.get first (original working endpoint)
		params := map[string]interface{}{
			"type":     "bg.local.goods.brands.get",
			"page":     page,
			"pageSize": pageSize,
		}

		resp, err := c.Post(params)
		if err != nil {
			return nil, err
		}
		if !resp.Success {
			// Fallback to trademark endpoint
			params["type"] = "bg.local.goods.brand.trademark.get"
			delete(params, "version")
			resp, err = c.Post(params)
			if err != nil {
				return nil, err
			}
			if !resp.Success {
				// Final fallback: V2
				params["type"] = "temu.local.goods.brand.trademark.V2.get"
				params["version"] = "V2"
				params["size"] = pageSize
				resp, err = c.Post(params)
				if err != nil {
					return nil, err
				}
				if !resp.Success {
					return nil, fmt.Errorf("list brands: %s", resp.ErrorMsg)
				}
			}
		}

		var result map[string]interface{}
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			return nil, err
		}

		// Find the list in various possible keys
		var rows []interface{}
		for _, key := range []string{"brandList", "brandInfos", "trademarkList", "list", "records", "items", "result", "data"} {
			if list, ok := result[key].([]interface{}); ok {
				rows = list
				break
			}
		}

		if len(rows) == 0 {
			// Log raw response keys so we can see what the API actually returned
			keys := make([]string, 0, len(result))
			for k := range result {
				keys = append(keys, k)
			}
			fmt.Printf("[Temu ListAllBrands] Got 0 rows on page %d. Result keys: %v. Raw: %s\n", page, keys, string(resp.Result))
			break
		}

		for _, row := range rows {
			m, ok := row.(map[string]interface{})
			if !ok {
				continue
			}
			bt := BrandTrademarkFull{}
			if v, ok := m["brandId"].(float64); ok {
				bt.BrandID = int64(v)
			}
			if v, ok := m["brandName"].(string); ok {
				bt.BrandName = v
			}
			if v, ok := m["trademarkId"].(float64); ok {
				bt.TrademarkID = int64(v)
			}
			if v, ok := m["trademarkBizId"].(float64); ok {
				bt.TrademarkBizID = int64(v)
			}
			if bt.BrandID > 0 {
				allBrands = append(allBrands, bt)
			}
		}

		// Check if there are more pages
		total := 0
		if t, ok := result["total"].(float64); ok {
			total = int(t)
		} else if t, ok := result["totalCount"].(float64); ok {
			total = int(t)
		}

		if total > 0 && page*pageSize >= total {
			break
		}
		if len(rows) < pageSize {
			break
		}

		page++
		if page > 4 { // Safety limit
			break
		}
	}

	return allBrands, nil
}

// ============================================================================
// PRODUCT IMPORT (Query existing products from Temu)
// ============================================================================

// TemuGoods represents a product from bg.local.goods.list.query
type TemuGoods struct {
	GoodsID       int64                    `json:"goodsId"`
	GoodsName     string                   `json:"goodsName"`
	GoodsSn       string                   `json:"goodsSn"`       // Temu product code
	OutGoodsSn    string                   `json:"outGoodsSn"`    // Seller's external code
	CatID         int                      `json:"catId"`
	Status        int                      `json:"status"`        // 0=editing, 1=reviewing, 2=on sale, 3=off sale, 4=rejected
	OnSale        int                      `json:"onSale"`        // 0=off, 1=on
	CreatedAt     int64                    `json:"createdAt"`     // Unix millis
	UpdatedAt     int64                    `json:"updatedAt"`     // Unix millis
	MainImageUrl  string                   `json:"mainImageUrl"`
}

// TemuGoodsDetail is the full product detail from bg.local.goods.detail.query
type TemuGoodsDetail struct {
	GoodsID       int64                    `json:"goodsId"`
	GoodsName     string                   `json:"goodsName"`
	GoodsSn       string                   `json:"goodsSn"`
	OutGoodsSn    string                   `json:"outGoodsSn"`
	CatID         int                      `json:"catId"`
	CatName       string                   `json:"catName"`
	Status        int                      `json:"status"`
	OnSale        int                      `json:"onSale"`
	GoodsDesc     string                   `json:"goodsDesc"`
	BulletPoints  []string                 `json:"bulletPoints"`
	MainImageUrl  string                   `json:"mainImageUrl"`
	CarouselImage []string                 `json:"carouselImage"`
	DetailImage   []string                 `json:"detailImage"`
	SkuList       []TemuSKU                `json:"skuList"`
	GoodsProperty []map[string]interface{} `json:"goodsProperty"`
	BrandInfo     map[string]interface{}   `json:"brandInfo"`
	Raw           map[string]interface{}   `json:"-"` // Full raw response
}

// TemuSKU represents a single SKU from product details or sku.list.query
type TemuSKU struct {
	SkuID       int64                  `json:"skuId"`
	OutSkuSn    string                 `json:"outSkuSn"`    // Seller SKU
	Price       map[string]interface{} `json:"price"`
	Stock       int                    `json:"stock"`
	SpecList    []map[string]interface{} `json:"specList"`
	ImageUrl    string                 `json:"imageUrl"`
	Weight      interface{}            `json:"weight"`
	Length      interface{}            `json:"length"`
	Width       interface{}            `json:"width"`
	Height      interface{}            `json:"height"`
}

// GoodsListPage is the result of bg.local.goods.list.query
type GoodsListPage struct {
	Total      int         `json:"total"`
	PageNumber int         `json:"pageNumber"`
	PageSize   int         `json:"pageSize"`
	GoodsList  []TemuGoods `json:"goodsList"`
}

// ListGoods fetches a page of products from the Temu shop
func (c *Client) ListGoods(pageNumber, pageSize int, goodsStatusFilterType ...int) (*GoodsListPage, error) {
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	if pageNumber < 1 {
		pageNumber = 1
	}

	statusFilter := 0
	if len(goodsStatusFilterType) > 0 {
		statusFilter = goodsStatusFilterType[0]
	}

	log.Printf("[Temu] ListGoods: baseURL=%s page=%d pageSize=%d statusFilter=%d", c.BaseURL, pageNumber, pageSize, statusFilter)

	params := map[string]interface{}{
		"type":     "bg.local.goods.list.query",
		"pageNo":   pageNumber,
		"pageSize": pageSize,
	}
	if statusFilter > 0 {
		params["goodsStatusFilterType"] = statusFilter
	}

	resp, err := c.Post(params)
	if err != nil {
		log.Printf("[Temu] ListGoods API error: %v", err)
		return nil, err
	}
	if !resp.Success {
		log.Printf("[Temu] ListGoods not successful: code=%d msg=%s", resp.ErrorCode, resp.ErrorMsg)
		return nil, fmt.Errorf("list goods: %s", resp.ErrorMsg)
	}

	// Log raw result only in debug mode
	if os.Getenv("TEMU_DEBUG") == "true" {
		log.Printf("[Temu] ListGoods raw result: %s", string(resp.Result[:minInt(len(resp.Result), 500)]))
	}

	var page GoodsListPage
	var raw map[string]interface{}
	_ = json.Unmarshal(resp.Result, &raw) // always parse raw for total extraction

	if err := json.Unmarshal(resp.Result, &page); err != nil {
		// Try alternate nesting for goodsList
		if raw != nil {
			for _, key := range []string{"goodsList", "list", "data"} {
				if list, ok := raw[key]; ok {
					b, _ := json.Marshal(list)
					var goods []TemuGoods
					if json.Unmarshal(b, &goods) == nil {
						page.GoodsList = goods
					}
					break
				}
			}
		}
		if len(page.GoodsList) == 0 {
			return nil, fmt.Errorf("parse goods list: %w", err)
		}
	}

	// Always try to extract Total from raw — it may be nested even when unmarshal succeeds
	if page.Total == 0 && raw != nil {
		for _, key := range []string{"total", "totalCount", "total_count"} {
			if t, ok := raw[key].(float64); ok && t > 0 {
				page.Total = int(t)
				break
			}
		}
		// Also check one level of nesting (e.g. raw["data"]["total"])
		if page.Total == 0 {
			for _, wrapper := range []string{"data", "result", "pageInfo"} {
				if nested, ok := raw[wrapper].(map[string]interface{}); ok {
					for _, key := range []string{"total", "totalCount", "total_count"} {
						if t, ok := nested[key].(float64); ok && t > 0 {
							page.Total = int(t)
							break
						}
					}
					if page.Total > 0 {
						break
					}
				}
			}
		}
	}

	page.PageNumber = pageNumber
	page.PageSize = pageSize

	return &page, nil
}

// GetGoodsDetail fetches full product details including SKUs and properties
func (c *Client) GetGoodsDetail(goodsID int64) (*TemuGoodsDetail, error) {
	params := map[string]interface{}{
		"type":    "bg.local.goods.detail.query",
		"goodsId": goodsID,
	}

	resp, err := c.Post(params)
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("get goods detail (goodsId=%d): %s", goodsID, resp.ErrorMsg)
	}

	// Store raw for extended data
	var raw map[string]interface{}
	json.Unmarshal(resp.Result, &raw)

	var detail TemuGoodsDetail
	if err := json.Unmarshal(resp.Result, &detail); err != nil {
		return nil, fmt.Errorf("parse goods detail: %w", err)
	}

	detail.Raw = raw

	// DIAGNOSTIC: always log the first goods detail call so we can see
	// exactly what field names Temu returns. Remove once confirmed.
	if detailDiagOnce.CompareAndSwap(false, true) {
		rawKeys := make([]string, 0, len(raw))
		for k := range raw {
			rawKeys = append(rawKeys, k)
		}
		rawStr := string(resp.Result)
		if len(rawStr) > 800 {
			rawStr = rawStr[:800]
		}
		log.Printf("[Temu GoodsDetail DIAGNOSTIC] goodsId=%d raw_keys=%v", goodsID, rawKeys)
		log.Printf("[Temu GoodsDetail DIAGNOSTIC] parsed: name=%q mainImage=%q carousel=%d skus=%d bulletPoints=%d",
			detail.GoodsName, detail.MainImageUrl, len(detail.CarouselImage), len(detail.SkuList), len(detail.BulletPoints))
		log.Printf("[Temu GoodsDetail RAW(800chars): %s", rawStr)
	}

	// Try to extract images — Temu uses inconsistent field names across API versions.
	// Check all known variants at top level and inside goodsGallery wrapper.
	if raw != nil {
		// Helper to extract string slice from raw
		extractStrings := func(val interface{}) []string {
			var out []string
			if arr, ok := val.([]interface{}); ok {
				for _, item := range arr {
					if s, ok := item.(string); ok && s != "" {
						out = append(out, s)
					} else if m, ok := item.(map[string]interface{}); ok {
						// Some versions nest: [{url: "..."}]
						for _, urlKey := range []string{"url", "imageUrl", "image_url"} {
							if s, ok := m[urlKey].(string); ok && s != "" {
								out = append(out, s)
								break
							}
						}
					}
				}
			}
			return out
		}

		// Main image — try all known field names
		if detail.MainImageUrl == "" {
			for _, key := range []string{"mainImageUrl", "mainImage", "main_image_url", "main_image"} {
				if s, ok := raw[key].(string); ok && s != "" {
					detail.MainImageUrl = s
					break
				}
			}
		}

		// Carousel images — try top level then goodsGallery wrapper
		if len(detail.CarouselImage) == 0 {
			for _, key := range []string{"carouselImage", "carousel_image", "images"} {
				if imgs := extractStrings(raw[key]); len(imgs) > 0 {
					detail.CarouselImage = imgs
					break
				}
			}
		}

		// goodsGallery nested wrapper (common in newer API versions)
		for _, galleryKey := range []string{"goodsGallery", "gallery"} {
			if gallery, ok := raw[galleryKey].(map[string]interface{}); ok {
				if detail.MainImageUrl == "" {
					for _, key := range []string{"mainImage", "mainImageUrl", "main_image"} {
						if s, ok := gallery[key].(string); ok && s != "" {
							detail.MainImageUrl = s
							break
						}
					}
				}
				if len(detail.CarouselImage) == 0 {
					for _, key := range []string{"carouselImage", "carousel_image", "images"} {
						if imgs := extractStrings(gallery[key]); len(imgs) > 0 {
							detail.CarouselImage = imgs
							break
						}
					}
				}
				if len(detail.DetailImage) == 0 {
					for _, key := range []string{"detailImage", "detail_image", "detailImages"} {
						if imgs := extractStrings(gallery[key]); len(imgs) > 0 {
							detail.DetailImage = imgs
							break
						}
					}
				}
			}
		}

		// detailImage at top level
		if len(detail.DetailImage) == 0 {
			for _, key := range []string{"detailImage", "detail_image", "detailImages"} {
				if imgs := extractStrings(raw[key]); len(imgs) > 0 {
					detail.DetailImage = imgs
					break
				}
			}
		}
	}

	// Try to extract SKUs from nested structure
	if len(detail.SkuList) == 0 && raw != nil {
		for _, key := range []string{"skuList", "skuInfoList", "sku_list"} {
			if list, ok := raw[key].([]interface{}); ok {
				b, _ := json.Marshal(list)
				json.Unmarshal(b, &detail.SkuList)
				break
			}
		}
	}

	// Extract bullet points from nested
	if len(detail.BulletPoints) == 0 && raw != nil {
		if bps, ok := raw["bulletPoints"].([]interface{}); ok {
			for _, bp := range bps {
				if s, ok := bp.(string); ok {
					detail.BulletPoints = append(detail.BulletPoints, s)
				}
			}
		}
	}

	return &detail, nil
}

// ListSKUs fetches SKU details for a product
func (c *Client) ListSKUs(goodsID int64) ([]TemuSKU, error) {
	params := map[string]interface{}{
		"type":    "bg.local.goods.sku.list.query",
		"goodsId": goodsID,
	}

	resp, err := c.Post(params)
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("list SKUs: %s", resp.ErrorMsg)
	}

	var result struct {
		SkuList []TemuSKU `json:"skuList"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		// Try direct array
		var skus []TemuSKU
		if err2 := json.Unmarshal(resp.Result, &skus); err2 == nil {
			return skus, nil
		}
		return nil, fmt.Errorf("parse SKU list: %w", err)
	}
	return result.SkuList, nil
}

// MallInfo represents shop/mall information
type MallInfo struct {
	MallID   int64  `json:"mallId"`
	MallName string `json:"mallName"`
	Region   string `json:"region"`
	Raw      map[string]interface{} `json:"-"`
}

// GetMallInfo fetches the shop information.
// Falls back to a category list ping if the mall info endpoint is IP-restricted.
func (c *Client) GetMallInfo() (*MallInfo, error) {
	params := map[string]interface{}{
		"type": "bg.local.mall.info.get",
	}

	resp, err := c.Post(params)
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		// bg.local.mall.info.get may be IP-restricted on some accounts.
		// Fall back to a lightweight category ping which is always accessible.
		pingParams := map[string]interface{}{
			"type":        "bg.local.goods.cats.get",
			"parentCatId": 0,
			"pageSize":    1,
		}
		pingResp, pingErr := c.Post(pingParams)
		if pingErr != nil {
			// Return original error if ping also fails
			return nil, fmt.Errorf("get mall info: %s", resp.ErrorMsg)
		}
		if !pingResp.Success {
			return nil, fmt.Errorf("get mall info: %s", resp.ErrorMsg)
		}
		// Ping succeeded — connection is valid, return a minimal MallInfo
		return &MallInfo{MallName: "Connected"}, nil
	}

	var raw map[string]interface{}
	json.Unmarshal(resp.Result, &raw)

	var info MallInfo
	json.Unmarshal(resp.Result, &info)
	info.Raw = raw

	return &info, nil
}

// ============================================================================
// PRODUCT SUBMISSION
// ============================================================================

// AddProductResult is the response from bg.local.goods.add
type AddProductResult struct {
	GoodsID     int64                    `json:"goodsId"`
	SkuInfoList []map[string]interface{} `json:"skuInfoList"`
}

// SubmitProduct sends a goods.add or goods.update to Temu.
// Returns the parsed result, the raw Temu response, and any error.
// The raw response is returned even on failure for debugging.
func (c *Client) SubmitProduct(request map[string]interface{}, isUpdate bool) (*AddProductResult, map[string]interface{}, error) {
	apiType := "bg.local.goods.add"
	if isUpdate {
		apiType = "bg.local.goods.update"
	}

	params := map[string]interface{}{
		"type": apiType,
	}
	for k, v := range request {
		params[k] = v
	}

	resp, err := c.Post(params)
	if err != nil {
		return nil, nil, err
	}

	rawResp := resp.Raw // always capture the raw response

	if !resp.Success {
		return nil, rawResp, fmt.Errorf("%s failed (code=%d): %s", apiType, resp.ErrorCode, resp.ErrorMsg)
	}

	var result AddProductResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, rawResp, fmt.Errorf("parse result: %w", err)
	}

	return &result, rawResp, nil
}

// ============================================================================
// LISTING STATUS
// ============================================================================

// SetSaleStatus lists (onsale=1) or delists (onsale=0) a product
func (c *Client) SetSaleStatus(goodsID int64, onSale bool) error {
	sale := 0
	if onSale {
		sale = 1
	}
	params := map[string]interface{}{
		"type":    "bg.local.goods.sale.status.set",
		"goodsId": goodsID,
		"onsale":  sale,
	}

	resp, err := c.Post(params)
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("set sale status: %s", resp.ErrorMsg)
	}
	return nil
}

// GetPublishStatus checks the review/publish status of a product
func (c *Client) GetPublishStatus(goodsID int64) (map[string]interface{}, error) {
	params := map[string]interface{}{
		"type":    "bg.local.goods.publish.status.get",
		"goodsId": goodsID,
	}

	resp, err := c.Post(params)
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("get publish status: %s", resp.ErrorMsg)
	}

	var result map[string]interface{}
	json.Unmarshal(resp.Result, &result)
	return result, nil
}

// ============================================================================
// STOCK & PRICE SYNC
// ============================================================================
// Temu Open Platform API types:
//   bg.local.goods.sku.stock.update  — update stock per SKU
//   bg.local.goods.sku.price.edit   — update price per SKU

// SKUStockUpdate is a single entry in the stock update list.
type SKUStockUpdate struct {
	SkuID    int64 `json:"skuId"`
	StockNum int   `json:"stockNum"`
}

// SKUPriceUpdate is a single entry in the price update list.
type SKUPriceUpdate struct {
	SkuID int64              `json:"skuId"`
	Price SKUPriceValue      `json:"price"`
}

// SKUPriceValue holds the currency and amount for a Temu price update.
type SKUPriceValue struct {
	CurrencyType string `json:"currencyType"` // e.g. "USD", "GBP", "EUR"
	Price        string `json:"price"`         // decimal string e.g. "19.99"
}

// UpdateSKUStock pushes new stock quantities to Temu for every SKU of a product.
// goodsID is the Temu goods ID (the external_id stored in our listings collection).
// updates contains one entry per SKU variant; use ListSKUs to get skuId values first.
func (c *Client) UpdateSKUStock(goodsID int64, updates []SKUStockUpdate) error {
	if len(updates) == 0 {
		return nil
	}
	params := map[string]interface{}{
		"type":         "bg.local.goods.sku.stock.update",
		"goodsId":      goodsID,
		"skuStockList": updates,
	}
	resp, err := c.Post(params)
	if err != nil {
		return fmt.Errorf("temu sku stock update: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("temu sku stock update (code=%d): %s", resp.ErrorCode, resp.ErrorMsg)
	}
	return nil
}

// UpdateSKUPrice pushes a new price to Temu for every SKU of a product.
// currencyType should match the currency configured on the Temu seller account (e.g. "USD").
// price is the retail price as a decimal string.
func (c *Client) UpdateSKUPrice(goodsID int64, updates []SKUPriceUpdate) error {
	if len(updates) == 0 {
		return nil
	}
	params := map[string]interface{}{
		"type":         "bg.local.goods.sku.price.edit",
		"goodsId":      goodsID,
		"skuPriceList": updates,
	}
	resp, err := c.Post(params)
	if err != nil {
		return fmt.Errorf("temu sku price edit: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("temu sku price edit (code=%d): %s", resp.ErrorCode, resp.ErrorMsg)
	}
	return nil
}

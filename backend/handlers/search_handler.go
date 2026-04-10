package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"module-a/repository"
	"module-a/services"

	"github.com/gin-gonic/gin"
)

type SearchHandler struct {
	searchService   *services.SearchService
	firestoreRepo   *repository.FirestoreRepository
	marketplaceRepo *repository.MarketplaceRepository
	listingService  *services.ListingService
}

func NewSearchHandler(
	searchService *services.SearchService,
	firestoreRepo *repository.FirestoreRepository,
	marketplaceRepo *repository.MarketplaceRepository,
) *SearchHandler {
	return &SearchHandler{
		searchService:   searchService,
		firestoreRepo:   firestoreRepo,
		marketplaceRepo: marketplaceRepo,
	}
}

// SetListingService wires in the ListingService after construction.
// Must be called in main.go immediately after NewSearchHandler.
func (h *SearchHandler) SetListingService(svc *services.ListingService) {
	h.listingService = svc
}

// GET /api/v1/search/products?q=rocky&status=active&page=1&per_page=20
func (h *SearchHandler) SearchProducts(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	query := c.Query("q")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))

	filters := map[string]string{}
	if v := c.Query("status"); v != "" {
		filters["status"] = v
	}
	if v := c.Query("brand"); v != "" {
		filters["brand"] = v
	}
	if v := c.Query("product_type"); v != "" {
		filters["product_type"] = v
	}
	if v := c.Query("parent_id"); v != "" {
		filters["parent_id"] = v
	}
	if v := c.Query("parent_asin"); v != "" {
		filters["parent_asin"] = v
	}

	result, err := h.searchService.SearchProducts(c.Request.Context(), tenantID, query, filters, page, perPage)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":         result.Hits,
		"found":        result.Found,
		"page":         result.Page,
		"out_of":       result.OutOf,
		"facet_counts": result.FacetCounts,
	})
}

// GET /api/v1/search/listings
//
// Query params:
//
//	q                  — free-text search (title, SKU, brand)
//	channel            — e.g. "ebay"
//	state              — e.g. "error" or "error,blocked" (comma-separated OR)
//	brand              — product brand facet value
//	category           — product category facet value
//	account_name       — credential account name facet value
//	channel_account_id — exact credential ID filter
//	page, per_page
func (h *SearchHandler) SearchListings(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	query := c.Query("q")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "50"))

	filters := map[string]string{}
	if v := c.Query("channel"); v != "" {
		filters["channel"] = v
	}
	if v := c.Query("state"); v != "" {
		filters["state"] = v
	}
	if v := c.Query("brand"); v != "" {
		filters["brand"] = v
	}
	if v := c.Query("category"); v != "" {
		filters["category"] = v
	}
	if v := c.Query("account_name"); v != "" {
		filters["account_name"] = v
	}
	if v := c.Query("channel_account_id"); v != "" {
		filters["channel_account_id"] = v
	}

	result, err := h.searchService.SearchListings(c.Request.Context(), tenantID, query, filters, page, perPage)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":         result.Hits,
		"found":        result.Found,
		"page":         result.Page,
		"out_of":       result.OutOf,
		"facet_counts": result.FacetCounts,
	})
}

// POST /api/v1/search/reindex-listings
//
// Drops and recreates the listings Typesense collection with the current schema,
// then re-indexes all listings for the calling tenant with full product and
// credential data joined in.
//
// Call this after deploying a schema change, or whenever listing search data
// looks stale. Safe to call at any time — it's a full replace, not incremental.
func (h *SearchHandler) ReindexListings(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	if h.listingService == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "listing service not wired — add searchHandler.SetListingService(listingService) to main.go",
		})
		return
	}

	// Drop + recreate the collection with the current schema
	if err := h.searchService.ResetListingsCollection(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "reset collection: " + err.Error()})
		return
	}

	// Load all listings joined with product + credential data (limit=0 means all)
	listings, _, err := h.listingService.ListListingsWithProductsPaginated(c.Request.Context(), tenantID, "", 0, 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "load listings: " + err.Error()})
		return
	}

	count, err := h.searchService.SyncAllListingsWithProducts(c.Request.Context(), tenantID, listings)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "sync listings: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"indexed": count,
		"message": fmt.Sprintf("Reindexed %d listings for tenant %s", count, tenantID),
	})
}

// POST /api/v1/search/sync — Full reindex for a tenant
func (h *SearchHandler) SyncAll(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req struct {
		Collection string `json:"collection"` // "products", "listings", or "" for both
	}
	c.ShouldBindJSON(&req)

	results := gin.H{}

	if req.Collection == "" || req.Collection == "products" {
		count, err := h.searchService.SyncAllProducts(c.Request.Context(), tenantID)
		if err != nil {
			results["products"] = gin.H{"error": err.Error()}
		} else {
			results["products"] = gin.H{"indexed": count}
		}
	}

	if req.Collection == "" || req.Collection == "listings" {
		if h.listingService != nil {
			// Use enriched sync when listing service is wired
			listings, _, err := h.listingService.ListListingsWithProductsPaginated(c.Request.Context(), tenantID, "", 0, 0)
			if err != nil {
				results["listings"] = gin.H{"error": err.Error()}
			} else {
				count, err := h.searchService.SyncAllListingsWithProducts(c.Request.Context(), tenantID, listings)
				if err != nil {
					results["listings"] = gin.H{"error": err.Error()}
				} else {
					results["listings"] = gin.H{"indexed": count}
				}
			}
		} else {
			// Fallback: basic sync without product data
			rawListings, _, err := h.marketplaceRepo.ListListingsPaginated(c.Request.Context(), tenantID, "", 0, 0)
			if err != nil {
				results["listings"] = gin.H{"error": err.Error()}
			} else {
				count, err := h.searchService.SyncAllListings(c.Request.Context(), tenantID, rawListings)
				if err != nil {
					results["listings"] = gin.H{"error": err.Error()}
				} else {
					results["listings"] = gin.H{"indexed": count}
				}
			}
		}
	}

	c.JSON(http.StatusOK, results)
}

// GET /api/v1/search/health
func (h *SearchHandler) Health(c *gin.Context) {
	healthy, errMsg := h.searchService.HealthyWithError()
	if healthy {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	} else {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable", "error": errMsg})
	}
}

// POST /api/v1/search/reconnect
// Accepts { "typesense_url": "http://10.x.x.x:8108" } and updates the
// search service connection. Persists the URL in the TYPESENSE_URL env var
// for the lifetime of this process.
func (h *SearchHandler) Reconnect(c *gin.Context) {
	var req struct {
		TypesenseURL string `json:"typesense_url"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.TypesenseURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "typesense_url is required"})
		return
	}

	os.Setenv("TYPESENSE_URL", req.TypesenseURL)
	h.searchService.UpdateHost(req.TypesenseURL)

	if !h.searchService.Healthy() {
		c.JSON(http.StatusOK, gin.H{
			"ok":      false,
			"message": fmt.Sprintf("Updated URL to %s but Typesense is still unreachable. Check the IP address and that the VM is running.", req.TypesenseURL),
			"url":     req.TypesenseURL,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": fmt.Sprintf("Connected successfully to Typesense at %s", req.TypesenseURL),
		"url":     req.TypesenseURL,
	})
}

// POST /api/v1/admin/search/restart-vm
//
// Resets the Typesense GCE instance via the Compute Engine API.
// Required env vars: GCP_PROJECT_ID
// Optional: TYPESENSE_GCE_INSTANCE (default: "typesense-server"), TYPESENSE_GCE_ZONE (default: "us-central1-a")
func (h *SearchHandler) RestartTypesenseVM(c *gin.Context) {
	project := os.Getenv("GCP_PROJECT_ID")
	if project == "" {
		c.JSON(http.StatusInternalServerError, gin.H{
			"ok":    false,
			"error": "GCP_PROJECT_ID env var not set",
		})
		return
	}

	instance := os.Getenv("TYPESENSE_GCE_INSTANCE")
	if instance == "" {
		instance = "typesense-server"
	}
	zone := os.Getenv("TYPESENSE_GCE_ZONE")
	if zone == "" {
		zone = "us-central1-a"
	}

	token, err := getComputeAccessToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"ok":    false,
			"error": "failed to get GCP access token: " + err.Error(),
		})
		return
	}

	url := fmt.Sprintf(
		"https://compute.googleapis.com/compute/v1/projects/%s/zones/%s/instances/%s/reset",
		project, zone, instance,
	)

	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, url, strings.NewReader("{}"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": err.Error()})
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"ok":    false,
			"error": "Compute Engine API call failed: " + err.Error(),
		})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		var gcpErr struct {
			Error struct {
				Message string `json:"message"`
				Code    int    `json:"code"`
			} `json:"error"`
		}
		msg := string(body)
		if json.Unmarshal(body, &gcpErr) == nil && gcpErr.Error.Message != "" {
			msg = gcpErr.Error.Message
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"ok":    false,
			"error": fmt.Sprintf("GCP reset failed (HTTP %d): %s", resp.StatusCode, msg),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":       true,
		"message":  fmt.Sprintf("VM reset initiated for %s (%s). Typesense will be back in ~30–60 seconds.", instance, zone),
		"instance": instance,
		"zone":     zone,
	})
}

// getComputeAccessToken fetches an OAuth2 token from the GCE metadata server.
func getComputeAccessToken() (string, error) {
	req, err := http.NewRequest(http.MethodGet,
		"http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token",
		nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Metadata-Flavor", "Google")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("metadata server unreachable (are you running on GCP?): %w", err)
	}
	defer resp.Body.Close()

	var t struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&t); err != nil {
		return "", err
	}
	if t.AccessToken == "" {
		return "", fmt.Errorf("empty access token from metadata server")
	}
	return t.AccessToken, nil
}

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

// GET /api/v1/search/listings?q=rocky&channel=amazon&page=1
func (h *SearchHandler) SearchListings(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	query := c.Query("q")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))

	filters := map[string]string{}
	if v := c.Query("channel"); v != "" {
		filters["channel"] = v
	}
	if v := c.Query("state"); v != "" {
		filters["state"] = v
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
		listings, _, err := h.marketplaceRepo.ListListingsPaginated(c.Request.Context(), tenantID, "", 0, 0)
		if err != nil {
			results["listings"] = gin.H{"error": err.Error()}
		} else {
			count, err := h.searchService.SyncAllListings(c.Request.Context(), tenantID, listings)
			if err != nil {
				results["listings"] = gin.H{"error": err.Error()}
			} else {
				results["listings"] = gin.H{"indexed": count}
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
// for the lifetime of this process. For permanent fix, update Cloud Run env vars.
func (h *SearchHandler) Reconnect(c *gin.Context) {
	var req struct {
		TypesenseURL string `json:"typesense_url"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.TypesenseURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "typesense_url is required"})
		return
	}

	// Update the env var so NewSearchService picks it up if recreated
	os.Setenv("TYPESENSE_URL", req.TypesenseURL)

	// Update the live search service connection
	h.searchService.UpdateHost(req.TypesenseURL)

	// Test the new connection
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
// "Reset" is a hard power-cycle: the VM reboots and Docker restarts the
// typesense container automatically (restart=always policy).
//
// Required env vars (already set in Cloud Run):
//   GCP_PROJECT_ID   — e.g. "marketmate-486116"
//
// Optional overrides (defaults match the production setup):
//   TYPESENSE_GCE_INSTANCE — instance name  (default: "typesense-server")
//   TYPESENSE_GCE_ZONE     — zone           (default: "us-central1-a")
//
// The Cloud Run service account needs the "compute.instances.reset" IAM
// permission on the typesense-server instance (roles/compute.instanceAdmin.v1
// on the instance, or project-level).
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

	// Get an OAuth2 access token from the GCE metadata server.
	// This works automatically on Cloud Run using the service account.
	token, err := getComputeAccessToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"ok":    false,
			"error": "failed to get GCP access token: " + err.Error(),
		})
		return
	}

	// POST to Compute Engine instances.reset
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
		// Parse GCP error message if present
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
// This is the same pattern used elsewhere in the codebase.
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

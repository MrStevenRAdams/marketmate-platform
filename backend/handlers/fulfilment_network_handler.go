package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sort"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"

	"module-a/models"
)

// FulfilmentNetworkHandler manages fulfilment network CRUD and resolution logic.
// Routes (all under /api/v1/):
//   GET    /fulfilment-networks              — list all networks
//   POST   /fulfilment-networks              — create network
//   PUT    /fulfilment-networks/:id          — update network
//   DELETE /fulfilment-networks/:id          — delete network
//   POST   /fulfilment-networks/:id/resolve  — dry-run: which source wins for a given order?
//   POST   /orders/assign-network            — assign orders to a network (resolves + commits)
type FulfilmentNetworkHandler struct {
	client *firestore.Client
}

func NewFulfilmentNetworkHandler(client *firestore.Client) *FulfilmentNetworkHandler {
	return &FulfilmentNetworkHandler{client: client}
}

func (h *FulfilmentNetworkHandler) getTenantID(c *gin.Context) (string, bool) {
	tid := c.GetHeader("X-Tenant-Id")
	if tid == "" {
		tid = c.GetString("tenant_id")
	}
	if tid == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "X-Tenant-Id header required"})
		return "", false
	}
	return tid, true
}

func (h *FulfilmentNetworkHandler) networksCol(tenantID string) *firestore.CollectionRef {
	return h.client.Collection(fmt.Sprintf("tenants/%s/fulfilment_networks", tenantID))
}

// ListNetworks  GET /api/v1/fulfilment-networks
func (h *FulfilmentNetworkHandler) ListNetworks(c *gin.Context) {
	tenantID, ok := h.getTenantID(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	iter := h.networksCol(tenantID).OrderBy("name", firestore.Asc).Documents(ctx)
	defer iter.Stop()

	var networks []models.FulfilmentNetwork
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		var n models.FulfilmentNetwork
		if doc.DataTo(&n) == nil {
			networks = append(networks, n)
		}
	}
	if networks == nil {
		networks = []models.FulfilmentNetwork{}
	}
	c.JSON(http.StatusOK, gin.H{"networks": networks})
}

// CreateNetwork  POST /api/v1/fulfilment-networks
func (h *FulfilmentNetworkHandler) CreateNetwork(c *gin.Context) {
	tenantID, ok := h.getTenantID(c)
	if !ok {
		return
	}
	var req struct {
		Name        string                      `json:"name" binding:"required"`
		Description string                      `json:"description"`
		Sources     []models.NetworkSourceEntry `json:"sources"`
		Active      bool                        `json:"active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Sources == nil {
		req.Sources = []models.NetworkSourceEntry{}
	}

	id := fmt.Sprintf("net-%s", uuid.New().String()[:8])
	now := time.Now().Format(time.RFC3339)
	network := models.FulfilmentNetwork{
		NetworkID:   id,
		TenantID:    tenantID,
		Name:        req.Name,
		Description: req.Description,
		Sources:     req.Sources,
		Active:      req.Active,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	ctx := c.Request.Context()
	if _, err := h.networksCol(tenantID).Doc(id).Set(ctx, network); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"network": network})
}

// UpdateNetwork  PUT /api/v1/fulfilment-networks/:id
func (h *FulfilmentNetworkHandler) UpdateNetwork(c *gin.Context) {
	tenantID, ok := h.getTenantID(c)
	if !ok {
		return
	}
	networkID := c.Param("id")
	var req struct {
		Name        string                      `json:"name"`
		Description string                      `json:"description"`
		Sources     []models.NetworkSourceEntry `json:"sources"`
		Active      *bool                       `json:"active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updates := []firestore.Update{
		{Path: "updated_at", Value: time.Now().Format(time.RFC3339)},
	}
	if req.Name != "" {
		updates = append(updates, firestore.Update{Path: "name", Value: req.Name})
	}
	if req.Description != "" {
		updates = append(updates, firestore.Update{Path: "description", Value: req.Description})
	}
	if req.Sources != nil {
		updates = append(updates, firestore.Update{Path: "sources", Value: req.Sources})
	}
	if req.Active != nil {
		updates = append(updates, firestore.Update{Path: "active", Value: *req.Active})
	}

	ctx := c.Request.Context()
	if _, err := h.networksCol(tenantID).Doc(networkID).Update(ctx, updates); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// DeleteNetwork  DELETE /api/v1/fulfilment-networks/:id
func (h *FulfilmentNetworkHandler) DeleteNetwork(c *gin.Context) {
	tenantID, ok := h.getTenantID(c)
	if !ok {
		return
	}
	networkID := c.Param("id")
	ctx := c.Request.Context()
	if _, err := h.networksCol(tenantID).Doc(networkID).Delete(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ResolveNetwork  POST /api/v1/fulfilment-networks/:id/resolve
// Body: { order_id: "..." }
// Dry-run: returns which source would be selected for the given order and why.
func (h *FulfilmentNetworkHandler) ResolveNetwork(c *gin.Context) {
	tenantID, ok := h.getTenantID(c)
	if !ok {
		return
	}
	networkID := c.Param("id")
	var req struct {
		OrderID string `json:"order_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	networkDoc, err := h.networksCol(tenantID).Doc(networkID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Network not found"})
		return
	}
	var network models.FulfilmentNetwork
	if err := networkDoc.DataTo(&network); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse network"})
		return
	}

	result, err := h.resolveForOrder(ctx, tenantID, network, req.OrderID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"result": result})
}

// AssignNetwork  POST /api/v1/orders/assign-network
// Body: { order_ids: [...], network_id: "..." }
// Resolves each order against the network and writes fulfilment_center_id to the order document.
func (h *FulfilmentNetworkHandler) AssignNetwork(c *gin.Context) {
	tenantID, ok := h.getTenantID(c)
	if !ok {
		return
	}
	var req struct {
		OrderIDs  []string `json:"order_ids" binding:"required"`
		NetworkID string   `json:"network_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	networkDoc, err := h.networksCol(tenantID).Doc(req.NetworkID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Network not found"})
		return
	}
	var network models.FulfilmentNetwork
	if err := networkDoc.DataTo(&network); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse network"})
		return
	}

	type assignResult struct {
		OrderID        string `json:"order_id"`
		AssignedSource string `json:"assigned_source,omitempty"`
		Error          string `json:"error,omitempty"`
	}
	var results []assignResult

	for _, orderID := range req.OrderIDs {
		result, resolveErr := h.resolveForOrder(ctx, tenantID, network, orderID)
		if resolveErr != nil {
			results = append(results, assignResult{OrderID: orderID, Error: resolveErr.Error()})
			continue
		}
		if result.SelectedSource == nil {
			results = append(results, assignResult{OrderID: orderID, Error: "No suitable source found in network"})
			continue
		}

		_, updateErr := h.client.Collection(fmt.Sprintf("tenants/%s/orders", tenantID)).Doc(orderID).
			Update(ctx, []firestore.Update{
				{Path: "fulfilment_center_id", Value: result.SelectedSource.SourceID},
				{Path: "fulfilment_center_name", Value: result.SelectedSource.Name},
				{Path: "updated_at", Value: time.Now().Format(time.RFC3339)},
			})
		if updateErr != nil {
			log.Printf("AssignNetwork: failed to update order %s: %v", orderID, updateErr)
			results = append(results, assignResult{OrderID: orderID, Error: updateErr.Error()})
			continue
		}
		results = append(results, assignResult{OrderID: orderID, AssignedSource: result.SelectedSource.Name})
	}

	c.JSON(http.StatusOK, gin.H{"results": results, "total": len(results)})
}

// resolveForOrder walks a network's source list in priority order and returns
// the first active source that meets the minimum-stock threshold.
func (h *FulfilmentNetworkHandler) resolveForOrder(
	ctx context.Context,
	tenantID string,
	network models.FulfilmentNetwork,
	orderID string,
) (*models.NetworkResolveResult, error) {
	result := &models.NetworkResolveResult{
		NetworkID: network.NetworkID,
		OrderID:   orderID,
	}

	sources := make([]models.NetworkSourceEntry, len(network.Sources))
	copy(sources, network.Sources)
	sort.Slice(sources, func(i, j int) bool {
		return sources[i].Priority < sources[j].Priority
	})

	for _, entry := range sources {
		srcDoc, err := h.client.Collection(fmt.Sprintf("tenants/%s/fulfilment_sources", tenantID)).
			Doc(entry.SourceID).Get(ctx)
		if err != nil {
			result.SkippedSources = append(result.SkippedSources, models.ResolveSkipReason{
				SourceID: entry.SourceID,
				Reason:   "Source not found",
			})
			continue
		}
		var src models.FulfilmentSource
		if dataErr := srcDoc.DataTo(&src); dataErr != nil {
			result.SkippedSources = append(result.SkippedSources, models.ResolveSkipReason{
				SourceID: entry.SourceID,
				Reason:   "Failed to parse source: " + dataErr.Error(),
			})
			continue
		}
		if !src.Active {
			result.SkippedSources = append(result.SkippedSources, models.ResolveSkipReason{
				SourceID:   entry.SourceID,
				SourceName: src.Name,
				Reason:     "Source is inactive",
			})
			continue
		}

		// Min-stock check
		if entry.MinStock > 0 {
			available := h.getAggregateStock(ctx, tenantID, entry.SourceID)
			if available < entry.MinStock {
				result.SkippedSources = append(result.SkippedSources, models.ResolveSkipReason{
					SourceID:   entry.SourceID,
					SourceName: src.Name,
					Reason:     fmt.Sprintf("Insufficient stock: have %d, need %d", available, entry.MinStock),
				})
				continue
			}
		}

		result.SelectedSource = &src
		result.Reason = fmt.Sprintf("Selected %q (priority %d)", src.Name, entry.Priority)
		return result, nil
	}

	result.Reason = "No suitable source found in network"
	return result, nil
}

// getAggregateStock returns total available stock units at a given fulfilment source.
func (h *FulfilmentNetworkHandler) getAggregateStock(ctx context.Context, tenantID, sourceID string) int {
	iter := h.client.Collection(fmt.Sprintf("tenants/%s/inventory", tenantID)).
		Where("source_id", "==", sourceID).
		Where("quantity_available", ">", 0).
		Limit(200).
		Documents(ctx)
	defer iter.Stop()

	total := 0
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		data := doc.Data()
		if qty, ok := data["quantity_available"].(int64); ok {
			total += int(qty)
		} else if qty, ok := data["quantity_available"].(float64); ok {
			total += int(qty)
		}
	}
	return total
}

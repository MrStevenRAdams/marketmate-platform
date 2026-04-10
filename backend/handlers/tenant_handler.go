package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
)

// ============================================================================
// TENANT HANDLER — Lightweight tenant management
// ============================================================================
// Provides a simple way to create and list "accounts" so testers
// each get their own isolated data.  No auth — just a name/label.
// This will be replaced by Module M (Users & Roles) later.
// ============================================================================

type TenantHandler struct {
	client *firestore.Client
}

func NewTenantHandler(client *firestore.Client) *TenantHandler {
	return &TenantHandler{client: client}
}

type TenantAccount struct {
	TenantID  string `json:"tenant_id" firestore:"tenant_id"`
	Name      string `json:"name" firestore:"name"`
	Initials  string `json:"initials" firestore:"initials"`
	Color     string `json:"color" firestore:"color"`
	CreatedAt time.Time `json:"created_at" firestore:"created_at"`
}

// ListTenants returns all tenant accounts from both the real "tenants" collection
// (production customers) and the legacy "tenant_accounts" collection (dev/test).
func (h *TenantHandler) ListTenants(c *gin.Context) {
	ctx := c.Request.Context()
	seen := make(map[string]bool)
	var tenants []TenantAccount

	// ── Real customers from "tenants" collection ──────────────────────────
	realIter := h.client.Collection("tenants").
		OrderBy("created_at", firestore.Asc).
		Documents(ctx)
	for {
		doc, err := realIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break // non-fatal — continue to tenant_accounts
		}
		data := doc.Data()
		tid, _ := data["tenant_id"].(string)
		if tid == "" {
			tid = doc.Ref.ID
		}
		name, _ := data["name"].(string)
		if name == "" {
			name = tid
		}
		if seen[tid] {
			continue
		}
		seen[tid] = true
		initials, _ := data["initials"].(string)
		color, _ := data["color"].(string)
		var createdAt time.Time
		if ts, ok := data["created_at"].(time.Time); ok {
			createdAt = ts
		}
		tenants = append(tenants, TenantAccount{
			TenantID:  tid,
			Name:      name,
			Initials:  initials,
			Color:     color,
			CreatedAt: createdAt,
		})
	}

	// ── Dev/test accounts from "tenant_accounts" (legacy switcher) ────────
	devIter := h.client.Collection("tenant_accounts").
		OrderBy("created_at", firestore.Asc).
		Documents(ctx)
	for {
		doc, err := devIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var t TenantAccount
		if err := doc.DataTo(&t); err != nil {
			continue
		}
		if seen[t.TenantID] {
			continue // already included from real tenants
		}
		seen[t.TenantID] = true
		tenants = append(tenants, t)
	}

	if tenants == nil {
		tenants = []TenantAccount{}
	}

	c.JSON(http.StatusOK, gin.H{"data": tenants})
}

// CreateTenant registers a new tenant account
func (h *TenantHandler) CreateTenant(c *gin.Context) {
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name cannot be empty"})
		return
	}

	// Generate a URL-safe tenant ID from the name
	slug := strings.ToLower(strings.ReplaceAll(name, " ", "-"))
	slug = "tenant-" + slug + "-" + uuid.New().String()[:6]

	// Generate initials (up to 2 chars)
	words := strings.Fields(name)
	initials := ""
	for i, w := range words {
		if i >= 2 {
			break
		}
		initials += strings.ToUpper(w[:1])
	}
	if initials == "" {
		initials = "?"
	}

	// Pick a color based on hash
	colors := []string{"#3b82f6", "#10b981", "#f59e0b", "#ef4444", "#8b5cf6", "#06b6d4", "#f97316", "#14b8a6"}
	colorIndex := 0
	for _, ch := range slug {
		colorIndex += int(ch)
	}
	color := colors[colorIndex%len(colors)]

	tenant := TenantAccount{
		TenantID:  slug,
		Name:      name,
		Initials:  initials,
		Color:     color,
		CreatedAt: time.Now(),
	}

	if _, err := h.client.Collection("tenant_accounts").Doc(slug).Set(c.Request.Context(), tenant); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": tenant})
}

// DeleteTenant removes a tenant account (does NOT delete their data)
func (h *TenantHandler) DeleteTenant(c *gin.Context) {
	tenantID := c.Param("id")

	if _, err := h.client.Collection("tenant_accounts").Doc(tenantID).Delete(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

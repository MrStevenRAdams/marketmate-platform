package handlers

import (
	"context"
	"net/http"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"google.golang.org/api/iterator"

	"module-a/models"
)

// ============================================================================
// APP STORE HANDLER — Session 7
// ============================================================================
// GET    /api/v1/apps                   — list all apps in the store
// GET    /api/v1/apps/installed         — list installed apps for tenant
// POST   /api/v1/apps/:id/install       — install an app for tenant
// DELETE /api/v1/apps/:id/uninstall     — uninstall an app for tenant
// POST   /api/v1/apps/seed              — seed built-in apps (admin/dev only)
// ============================================================================

type AppStoreHandler struct {
	client *firestore.Client
}

func NewAppStoreHandler(client *firestore.Client) *AppStoreHandler {
	return &AppStoreHandler{client: client}
}

// ListApps GET /api/v1/apps
func (h *AppStoreHandler) ListApps(c *gin.Context) {
	ctx := c.Request.Context()
	tenantID := c.GetString("tenant_id")

	iter := h.client.Collection("apps").OrderBy("name", firestore.Asc).Documents(ctx)
	defer iter.Stop()

	var apps []models.App
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list apps"})
			return
		}
		var app models.App
		if err := doc.DataTo(&app); err != nil {
			continue
		}
		apps = append(apps, app)
	}

	// Fall back to built-in list if store not seeded
	if len(apps) == 0 {
		apps = make([]models.App, len(models.BuiltInApps))
		copy(apps, models.BuiltInApps)
	}

	installed := h.loadInstalledSet(ctx, tenantID)

	type AppWithInstalled struct {
		models.App
		IsInstalled bool       `json:"is_installed"`
		InstalledAt *time.Time `json:"installed_at,omitempty"`
	}

	result := make([]AppWithInstalled, 0, len(apps))
	for _, app := range apps {
		item := AppWithInstalled{App: app}
		if ia, ok := installed[app.AppID]; ok {
			item.IsInstalled = true
			t := ia.InstalledAt
			item.InstalledAt = &t
		}
		result = append(result, item)
	}

	c.JSON(http.StatusOK, gin.H{"apps": result, "count": len(result)})
}

// ListInstalledApps GET /api/v1/apps/installed
func (h *AppStoreHandler) ListInstalledApps(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	installed := h.loadInstalledSlice(ctx, tenantID)
	c.JSON(http.StatusOK, gin.H{"installed_apps": installed, "count": len(installed)})
}

// InstallApp POST /api/v1/apps/:id/install
func (h *AppStoreHandler) InstallApp(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	callerRole := models.Role(c.GetString("role"))
	if !callerRole.Can("manage_users") {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin or owner required"})
		return
	}

	appID := c.Param("id")
	ctx := c.Request.Context()

	app, found := h.findApp(ctx, appID)
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "app not found"})
		return
	}

	ia := models.InstalledApp{
		AppID:       appID,
		TenantID:    tenantID,
		InstalledAt: time.Now().UTC(),
		InstalledBy: c.GetString("user_id"),
		Enabled:     true,
	}

	docRef := h.client.Collection("tenants").Doc(tenantID).Collection("installed_apps").Doc(appID)
	if _, err := docRef.Set(ctx, ia); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to install app"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"installed_app": ia, "app": app})
}

// UninstallApp DELETE /api/v1/apps/:id/uninstall
func (h *AppStoreHandler) UninstallApp(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	callerRole := models.Role(c.GetString("role"))
	if !callerRole.Can("manage_users") {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin or owner required"})
		return
	}

	appID := c.Param("id")
	ctx := c.Request.Context()

	if _, err := h.client.Collection("tenants").Doc(tenantID).Collection("installed_apps").Doc(appID).Delete(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to uninstall app"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "App uninstalled", "app_id": appID})
}

// SeedApps POST /api/v1/apps/seed
func (h *AppStoreHandler) SeedApps(c *gin.Context) {
	ctx := c.Request.Context()
	seeded := 0
	for _, app := range models.BuiltInApps {
		app.CreatedAt = time.Now().UTC()
		if _, err := h.client.Collection("apps").Doc(app.AppID).Set(ctx, app); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "seed failed: " + err.Error()})
			return
		}
		seeded++
	}
	c.JSON(http.StatusOK, gin.H{"seeded": seeded})
}

// ── helpers ──────────────────────────────────────────────────────────────────

func (h *AppStoreHandler) findApp(ctx context.Context, appID string) (models.App, bool) {
	// Check Firestore
	doc, err := h.client.Collection("apps").Doc(appID).Get(ctx)
	if err == nil {
		var app models.App
		if err := doc.DataTo(&app); err == nil {
			return app, true
		}
	}
	// Fall back to built-in list
	for _, a := range models.BuiltInApps {
		if a.AppID == appID {
			return a, true
		}
	}
	return models.App{}, false
}

func (h *AppStoreHandler) loadInstalledSet(ctx context.Context, tenantID string) map[string]models.InstalledApp {
	result := map[string]models.InstalledApp{}
	iter := h.client.Collection("tenants").Doc(tenantID).Collection("installed_apps").Documents(ctx)
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var ia models.InstalledApp
		if err := doc.DataTo(&ia); err == nil {
			result[ia.AppID] = ia
		}
	}
	return result
}

func (h *AppStoreHandler) loadInstalledSlice(ctx context.Context, tenantID string) []models.InstalledApp {
	var result []models.InstalledApp
	iter := h.client.Collection("tenants").Doc(tenantID).Collection("installed_apps").Documents(ctx)
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var ia models.InstalledApp
		if err := doc.DataTo(&ia); err == nil {
			result = append(result, ia)
		}
	}
	if result == nil {
		result = []models.InstalledApp{}
	}
	return result
}

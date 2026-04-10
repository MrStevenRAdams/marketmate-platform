// platform/backend/handlers/customs_profile_handler.go
package handlers

import (
	"fmt"
	"net/http"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"
)

// ============================================================================
// CUSTOMS PROFILE HANDLER
// ============================================================================

type CustomsProfileHandler struct {
	client *firestore.Client
}

func NewCustomsProfileHandler(client *firestore.Client) *CustomsProfileHandler {
	return &CustomsProfileHandler{client: client}
}

type CustomsProfile struct {
	ID                   string    `json:"id" firestore:"id"`
	TenantID             string    `json:"tenant_id" firestore:"tenant_id"`
	Name                 string    `json:"name" firestore:"name"`
	CommodityCode        string    `json:"commodity_code" firestore:"commodity_code"` // HS code 6-10 digits
	CountryOfManufacture string    `json:"country_of_manufacture" firestore:"country_of_manufacture"`
	Description          string    `json:"description" firestore:"description"`
	DutyPaid             string    `json:"duty_paid" firestore:"duty_paid"` // "DDP" or "DDU"
	TypicalValueGBP      float64   `json:"typical_value_gbp" firestore:"typical_value_gbp"`
	TypicalWeightKG      float64   `json:"typical_weight_kg" firestore:"typical_weight_kg"`
	IOSSApplicable       bool      `json:"ioss_applicable" firestore:"ioss_applicable"`
	RequiresEORI         bool      `json:"requires_eori" firestore:"requires_eori"`
	RequiresVATNumber    bool      `json:"requires_vat_number" firestore:"requires_vat_number"`
	RequiresCPCCode      bool      `json:"requires_cpc_code" firestore:"requires_cpc_code"`
	CPCCode              string    `json:"cpc_code,omitempty" firestore:"cpc_code,omitempty"`
	CreatedAt            time.Time `json:"created_at" firestore:"created_at"`
	UpdatedAt            time.Time `json:"updated_at" firestore:"updated_at"`
}

func (h *CustomsProfileHandler) collection(tenantID string) *firestore.CollectionRef {
	return h.client.Collection("customs_profiles").Doc(tenantID).Collection("profiles")
}

// GET /api/v1/dispatch/customs-profiles
func (h *CustomsProfileHandler) ListProfiles(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}

	iter := h.collection(tenantID).Documents(c.Request.Context())
	defer iter.Stop()

	var profiles []CustomsProfile
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		var p CustomsProfile
		doc.DataTo(&p)
		p.ID = doc.Ref.ID
		profiles = append(profiles, p)
	}

	if profiles == nil {
		profiles = []CustomsProfile{}
	}
	c.JSON(http.StatusOK, gin.H{"profiles": profiles, "count": len(profiles)})
}

// POST /api/v1/dispatch/customs-profiles
func (h *CustomsProfileHandler) CreateProfile(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}

	var p CustomsProfile
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := validateCustomsProfile(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	p.ID = uuid.New().String()
	p.TenantID = tenantID
	p.CreatedAt = time.Now()
	p.UpdatedAt = time.Now()

	_, err := h.collection(tenantID).Doc(p.ID).Set(c.Request.Context(), p)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, p)
}

// GET /api/v1/dispatch/customs-profiles/:id
func (h *CustomsProfileHandler) GetProfile(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	profileID := c.Param("id")

	doc, err := h.collection(tenantID).Doc(profileID).Get(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "customs profile not found"})
		return
	}

	var p CustomsProfile
	doc.DataTo(&p)
	p.ID = doc.Ref.ID
	c.JSON(http.StatusOK, p)
}

// PUT /api/v1/dispatch/customs-profiles/:id
func (h *CustomsProfileHandler) UpdateProfile(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	profileID := c.Param("id")

	_, err := h.collection(tenantID).Doc(profileID).Get(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "customs profile not found"})
		return
	}

	var p CustomsProfile
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := validateCustomsProfile(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	p.ID = profileID
	p.TenantID = tenantID
	p.UpdatedAt = time.Now()

	_, err = h.collection(tenantID).Doc(profileID).Set(c.Request.Context(), p)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, p)
}

// DELETE /api/v1/dispatch/customs-profiles/:id
func (h *CustomsProfileHandler) DeleteProfile(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	profileID := c.Param("id")

	_, err := h.collection(tenantID).Doc(profileID).Delete(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

func validateCustomsProfile(p *CustomsProfile) error {
	if p.Name == "" {
		return fmt.Errorf("name is required")
	}
	if p.DutyPaid != "DDP" && p.DutyPaid != "DDU" {
		return fmt.Errorf("duty_paid must be DDP or DDU")
	}
	if len(p.CommodityCode) < 6 || len(p.CommodityCode) > 10 {
		return fmt.Errorf("commodity_code (HS code) must be 6–10 digits")
	}
	if p.CountryOfManufacture == "" || len(p.CountryOfManufacture) != 2 {
		return fmt.Errorf("country_of_manufacture must be a 2-letter ISO code")
	}
	return nil
}

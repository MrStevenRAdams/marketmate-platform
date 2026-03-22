package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"module-a/models"
)

// ============================================================================
// AUTH MIDDLEWARE
// ============================================================================

const (
	CtxUserID   = "user_id"
	CtxTenantID = "tenant_id"
	CtxRole     = "role"
	CtxEmail    = "email"
)

// ── Firebase token cache ──────────────────────────────────────────────────────

type firebaseTokenInfo struct {
	UID   string
	Email string
}

type tokenCacheEntry struct {
	info      firebaseTokenInfo
	expiresAt time.Time
}

type tokenCache struct {
	mu    sync.RWMutex
	store map[string]tokenCacheEntry
}

var globalTokenCache = &tokenCache{store: make(map[string]tokenCacheEntry)}

func (tc *tokenCache) get(token string) (firebaseTokenInfo, bool) {
	tc.mu.RLock()
	defer tc.mu.RUnlock()
	e, ok := tc.store[token]
	if !ok || time.Now().After(e.expiresAt) {
		return firebaseTokenInfo{}, false
	}
	return e.info, true
}

func (tc *tokenCache) set(token string, info firebaseTokenInfo, ttl time.Duration) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.store[token] = tokenCacheEntry{info: info, expiresAt: time.Now().Add(ttl)}
	if len(tc.store) > 1000 {
		for k, v := range tc.store {
			if time.Now().After(v.expiresAt) {
				delete(tc.store, k)
			}
		}
	}
}

func verifyFirebaseToken(ctx context.Context, idToken string) (firebaseTokenInfo, error) {
	if info, ok := globalTokenCache.get(idToken); ok {
		return info, nil
	}

	webAPIKey := os.Getenv("FIREBASE_WEB_API_KEY")
	if webAPIKey == "" {
		return firebaseTokenInfo{}, fmt.Errorf("FIREBASE_WEB_API_KEY not configured")
	}

	url := "https://identitytoolkit.googleapis.com/v1/accounts:lookup?key=" + webAPIKey
	body := strings.NewReader(`{"idToken":"` + idToken + `"}`)

	req, err := http.NewRequestWithContext(ctx, "POST", url, body)
	if err != nil {
		return firebaseTokenInfo{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return firebaseTokenInfo{}, fmt.Errorf("firebase verification failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return firebaseTokenInfo{}, fmt.Errorf("firebase token invalid (status %d)", resp.StatusCode)
	}

	var result struct {
		Users []struct {
			LocalID string `json:"localId"`
			Email   string `json:"email"`
		} `json:"users"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil || len(result.Users) == 0 {
		return firebaseTokenInfo{}, fmt.Errorf("firebase: no user in response")
	}

	info := firebaseTokenInfo{UID: result.Users[0].LocalID, Email: result.Users[0].Email}
	globalTokenCache.set(idToken, info, 5*time.Minute)
	return info, nil
}

// ── Membership lookup ─────────────────────────────────────────────────────────

func findMembership(ctx context.Context, client *firestore.Client, firebaseUID, tenantID string) (*models.UserMembership, *models.GlobalUser, error) {
	iter := client.Collection("global_users").
		Where("firebase_uid", "==", firebaseUID).
		Limit(1).Documents(ctx)
	defer iter.Stop()

	snap, err := iter.Next()
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil, fmt.Errorf("user not found")
		}
		return nil, nil, fmt.Errorf("user lookup: %w", err)
	}

	var user models.GlobalUser
	if err := snap.DataTo(&user); err != nil {
		return nil, nil, err
	}

	mIter := client.Collection("user_memberships").
		Where("user_id", "==", user.UserID).
		Where("tenant_id", "==", tenantID).
		Where("status", "==", string(models.MembershipActive)).
		Limit(1).Documents(ctx)
	defer mIter.Stop()

	mSnap, err := mIter.Next()
	if err != nil {
		return nil, &user, fmt.Errorf("not a member of tenant %s", tenantID)
	}

	var m models.UserMembership
	if err := mSnap.DataTo(&m); err != nil {
		return nil, &user, err
	}
	return &m, &user, nil
}

// ── Middleware functions ──────────────────────────────────────────────────────

// AuthMiddleware verifies Firebase token + tenant membership.
func AuthMiddleware(firestoreClient *firestore.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "missing Authorization header",
				"code":  "auth_required",
			})
			return
		}
		idToken := strings.TrimPrefix(authHeader, "Bearer ")

		tenantID := c.GetHeader("X-Tenant-Id")
		if tenantID == "" {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"error": "X-Tenant-Id header required",
				"code":  "tenant_required",
			})
			return
		}

		tokenInfo, err := verifyFirebaseToken(c.Request.Context(), idToken)
		if err != nil {
			log.Printf("[auth] token verify failed: %v", err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid or expired token",
				"code":  "token_invalid",
			})
			return
		}

		membership, user, err := findMembership(c.Request.Context(), firestoreClient, tokenInfo.UID, tenantID)
		if err != nil {
			log.Printf("[auth] membership check failed uid=%s tenant=%s: %v", tokenInfo.UID, tenantID, err)
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "not authorised for this tenant",
				"code":  "forbidden",
			})
			return
		}

		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			firestoreClient.Collection("global_users").Doc(user.UserID).Update(ctx, []firestore.Update{
				{Path: "last_login_at", Value: time.Now().UTC()},
			})
		}()

		c.Set(CtxUserID, membership.UserID)
		c.Set(CtxTenantID, tenantID)
		c.Set(CtxRole, string(membership.Role))
		c.Set(CtxEmail, user.Email)
		// Store explicit permission overrides so RequirePermission can read them.
		if membership.Permissions != nil {
			c.Set("permissions", membership.Permissions)
		} else {
			c.Set("permissions", map[string]bool{})
		}
		c.Next()
	}
}

// RequireRole aborts if the caller's role cannot perform the given action.
func RequireRole(action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		role := models.Role(c.GetString(CtxRole))
		if !role.Can(action) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error":     "insufficient permissions",
				"code":      "forbidden",
				"required":  action,
				"your_role": string(role),
			})
			return
		}
		c.Next()
	}
}

// RequireMinRole aborts if the caller is below the minimum role level.
func RequireMinRole(minRole models.Role) gin.HandlerFunc {
	return func(c *gin.Context) {
		hierarchy := map[models.Role]int{
			models.RoleViewer:  1,
			models.RoleManager: 2,
			models.RoleAdmin:   3,
			models.RoleOwner:   4,
		}
		role := models.Role(c.GetString(CtxRole))
		if hierarchy[role] < hierarchy[minRole] {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error":    "insufficient role",
				"required": string(minRole),
				"yours":    string(role),
			})
			return
		}
		c.Next()
	}
}

// TenantMiddleware is the legacy stub kept for dev/test environments.
// Set DISABLE_AUTH=true in .env to use this instead of AuthMiddleware.
func TenantMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := c.GetHeader("X-Tenant-Id")
		if tenantID == "" {
			tenantID = "tenant-demo"
		}
		c.Set("tenant_id", tenantID)
		c.Next()
	}
}

// RateLimitMiddleware adds rate limit headers.
func RateLimitMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-RateLimit-Limit", "10000")
		c.Header("X-RateLimit-Remaining", "9847")
		c.Header("X-RateLimit-Reset", "1643385600")
		c.Next()
	}
}

// CORSMiddleware handles CORS.
func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, Authorization, accept, origin, Cache-Control, X-Requested-With, X-Tenant-Id, X-Correlation-Id, Idempotency-Key")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, PATCH, DELETE")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// CorrelationIDMiddleware adds correlation ID tracking.
func CorrelationIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		correlationID := c.GetHeader("X-Correlation-Id")
		if correlationID == "" {
			correlationID = time.Now().Format("20060102150405")
		}
		c.Set("correlation_id", correlationID)
		c.Header("X-Correlation-Id", correlationID)
		c.Next()
	}
}

// RequirePermission checks whether the authenticated user has the given
// granular permission. It first checks the explicit Permissions map stored in
// the context (populated by AuthMiddleware from the UserMembership document).
// If no explicit value is found it falls back to the role-based default via
// models.RoleDefaultPermissions. Owner role always passes.
func RequirePermission(perm string) gin.HandlerFunc {
	return func(c *gin.Context) {
		role := models.Role(c.GetString(CtxRole))
		if role == models.RoleOwner {
			c.Next()
			return
		}

		// Check explicit override map first
		if raw, exists := c.Get("permissions"); exists {
			if perms, ok := raw.(map[string]bool); ok {
				if val, found := perms[perm]; found {
					if !val {
						c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
							"error":      "permission denied",
							"code":       "forbidden",
							"permission": perm,
						})
						return
					}
					c.Next()
					return
				}
			}
		}

		// Fall back to role default
		defaults := models.RoleDefaultPermissions(role)
		if !defaults[perm] {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error":      "permission denied",
				"code":       "forbidden",
				"permission": perm,
			})
			return
		}
		c.Next()
	}
}

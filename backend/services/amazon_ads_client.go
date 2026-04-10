package services

// ============================================================================
// AMAZON ADS API CLIENT
// ============================================================================
//
// Wraps the Amazon Advertising API's Sponsored Products keyword recommendations
// endpoint. Used by KeywordIntelligenceService.RefreshFromAmazonAdsAPI.
//
// Authentication: OAuth2 via https://api.amazon.com/auth/o2/token
//   - Client credentials (client_id, client_secret) from Secret Manager
//   - Refresh token from tenants/{tid}/marketplace_credentials where
//     channel == "amazon_ads"
//
// Absent credentials are NOT an error — return nil, nil and the caller
// skips the enrichment step silently.
// ============================================================================

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	secretmanagerpb "cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
)

const (
	amazonAdsTokenEndpoint      = "https://api.amazon.com/auth/o2/token"
	amazonAdsKWRecommendURL     = "https://advertising.amazon.eu/sp/targets/keywords/recommendations"
	amazonAdsClientIDSecret     = "marketmate-amazon-ads-client-id"
	amazonAdsClientSecretSecret = "marketmate-amazon-ads-client-secret"
	amazonAdsProjectID          = "marketmate-486116"
	amazonAdsMaxRecommendations = 100
)

// AmazonAdsClient wraps the Amazon Advertising API keyword recommendations call.
type AmazonAdsClient struct {
	httpClient   *http.Client
	clientID     string
	clientSecret string
}

// AmazonAdsCreds holds the per-tenant OAuth credentials needed for the Ads API.
type AmazonAdsCreds struct {
	RefreshToken string
	ProfileID    string // optional — used as Amazon-Advertising-API-Scope header if set
}

// AdsKeywordRecommendation is a single keyword recommendation from the Ads API.
type AdsKeywordRecommendation struct {
	Keyword      string
	BidLow       float64
	BidHigh      float64
	BidSuggested float64
}

// ── Constructor ───────────────────────────────────────────────────────────────

// NewAmazonAdsClient creates an AmazonAdsClient by loading client credentials
// from Secret Manager. Returns nil, nil if either secret is absent —
// callers must handle a nil client gracefully.
func NewAmazonAdsClient(ctx context.Context) (*AmazonAdsClient, error) {
	clientID, err := loadSecret(ctx, amazonAdsProjectID, amazonAdsClientIDSecret)
	if err != nil {
		// Secret absent — Ads integration not configured yet
		return nil, nil
	}
	clientSecret, err := loadSecret(ctx, amazonAdsProjectID, amazonAdsClientSecretSecret)
	if err != nil {
		return nil, nil
	}
	if strings.TrimSpace(clientID) == "" || strings.TrimSpace(clientSecret) == "" {
		return nil, nil
	}
	return &AmazonAdsClient{
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		clientID:     strings.TrimSpace(clientID),
		clientSecret: strings.TrimSpace(clientSecret),
	}, nil
}

// ── Public methods ────────────────────────────────────────────────────────────

// GetCredentialsForTenant fetches the amazon_ads credential for the given
// tenant from Firestore. Returns nil, nil if no credential exists — callers
// must skip enrichment gracefully.
func GetAmazonAdsCreds(ctx context.Context, fsClient *firestore.Client, tenantID string) (*AmazonAdsCreds, error) {
	iter := fsClient.
		Collection("tenants").Doc(tenantID).
		Collection("marketplace_credentials").
		Where("channel", "==", "amazon_ads").
		Where("active", "==", true).
		Limit(1).
		Documents(ctx)
	defer iter.Stop()

	doc, err := iter.Next()
	if err == iterator.Done {
		// No credential found — not an error
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query amazon_ads credential: %w", err)
	}

	data := doc.Data()
	refreshToken, _ := data["refresh_token"].(string)
	if refreshToken == "" {
		// Check credential_data map as fallback (Option A manual entry stores here)
		if cd, ok := data["credential_data"].(map[string]interface{}); ok {
			refreshToken, _ = cd["refresh_token"].(string)
		}
	}
	if refreshToken == "" {
		return nil, nil
	}

	var profileID string
	if cd, ok := data["credential_data"].(map[string]interface{}); ok {
		profileID, _ = cd["profile_id"].(string)
	}

	return &AmazonAdsCreds{
		RefreshToken: refreshToken,
		ProfileID:    profileID,
	}, nil
}

// GetKeywordRecommendations calls the Amazon Advertising API keyword
// recommendations endpoint for the given ASIN. Returns nil, nil if the
// client is nil (credentials not configured).
func (c *AmazonAdsClient) GetKeywordRecommendations(
	ctx context.Context,
	asin string,
	creds *AmazonAdsCreds,
) ([]AdsKeywordRecommendation, error) {
	if c == nil || creds == nil {
		return nil, nil
	}

	// Exchange refresh token for access token
	accessToken, err := c.fetchAccessToken(ctx, creds.RefreshToken)
	if err != nil {
		return nil, fmt.Errorf("amazon_ads: token exchange: %w", err)
	}

	// Build request payload
	payload := map[string]interface{}{
		"asins":              []string{asin},
		"maxRecommendations": amazonAdsMaxRecommendations,
		"locale":             "en_GB",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("amazon_ads: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, amazonAdsKWRecommendURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("amazon_ads: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Amazon-Advertising-API-ClientId", c.clientID)
	if creds.ProfileID != "" {
		req.Header.Set("Amazon-Advertising-API-Scope", creds.ProfileID)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("amazon_ads: http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("amazon_ads: unexpected HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return parseKWRecommendations(respBody)
}

// ── Internal helpers ──────────────────────────────────────────────────────────

func (c *AmazonAdsClient) fetchAccessToken(ctx context.Context, refreshToken string) (string, error) {
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refreshToken)
	data.Set("client_id", c.clientID)
	data.Set("client_secret", c.clientSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, amazonAdsTokenEndpoint,
		strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint HTTP %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}
	if tokenResp.Error != "" {
		return "", fmt.Errorf("token error: %s", tokenResp.Error)
	}
	return tokenResp.AccessToken, nil
}

// parseKWRecommendations parses the recommendedKeywords array from the Ads API response.
// The response shape is: {"recommendedKeywords": [{"keyword": "...", "bid": {"suggested": 1.23, "rangeStart": 0.80, "rangeEnd": 1.50}}]}
func parseKWRecommendations(body []byte) ([]AdsKeywordRecommendation, error) {
	var envelope struct {
		RecommendedKeywords []struct {
			Keyword string `json:"keyword"`
			Bid     struct {
				Suggested  float64 `json:"suggested"`
				RangeStart float64 `json:"rangeStart"`
				RangeEnd   float64 `json:"rangeEnd"`
			} `json:"bid"`
		} `json:"recommendedKeywords"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("amazon_ads: parse response: %w", err)
	}

	results := make([]AdsKeywordRecommendation, 0, len(envelope.RecommendedKeywords))
	for _, kw := range envelope.RecommendedKeywords {
		if kw.Keyword == "" {
			continue
		}
		results = append(results, AdsKeywordRecommendation{
			Keyword:      kw.Keyword,
			BidSuggested: kw.Bid.Suggested,
			BidLow:       kw.Bid.RangeStart,
			BidHigh:      kw.Bid.RangeEnd,
		})
	}
	return results, nil
}

// loadSecret fetches a single secret value from GCP Secret Manager.
// Returns ("", nil) when the secret does not exist so callers can
// treat absent secrets as disabled integrations rather than errors.
func loadSecret(ctx context.Context, projectID, secretName string) (string, error) {
	client, err := secretmanager.NewClient(ctx)
	if err != nil {
		return "", fmt.Errorf("secret manager client: %w", err)
	}
	defer client.Close()

	name := fmt.Sprintf("projects/%s/secrets/%s/versions/latest", projectID, secretName)
	result, err := client.AccessSecretVersion(ctx, &secretmanagerpb.AccessSecretVersionRequest{
		Name: name,
	})
	if err != nil {
		// Treat not-found as absent (not an error) — integration simply not configured
		if strings.Contains(err.Error(), "NotFound") || strings.Contains(err.Error(), "not found") {
			return "", fmt.Errorf("secret not found: %s", secretName)
		}
		return "", fmt.Errorf("access secret %s: %w", secretName, err)
	}
	return string(result.Payload.Data), nil
}

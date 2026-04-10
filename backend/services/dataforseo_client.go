package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	secretmanagerpb "cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
)

const (
	dataForSEOEndpoint  = "https://api.dataforseo.com/v3/dataforseo_labs/amazon/ranked_keywords/live"
	dataForSEOSecretKey = "marketmate-dataforseo-credentials"
	dataForSEOProjectID = "marketmate-486116"
)

// DataForSEOClient wraps the DataForSEO Amazon ranked keywords endpoint.
// Authentication is HTTP Basic using credentials from GCP Secret Manager.
type DataForSEOClient struct {
	httpClient *http.Client
	username   string
	password   string
}

// dataForSEORequest is the payload sent to the ranked keywords endpoint.
type dataForSEORequest struct {
	ASIN         string `json:"asin"`
	LanguageName string `json:"language_name"`
	LocationCode int    `json:"location_code"`
	Limit        int    `json:"limit"`
}

// dataForSEOResponse is the top-level API response envelope.
type dataForSEOResponse struct {
	StatusCode int                     `json:"status_code"`
	StatusMessage string              `json:"status_message"`
	Tasks      []dataForSEOTask        `json:"tasks"`
}

type dataForSEOTask struct {
	StatusCode int                     `json:"status_code"`
	Result     []dataForSEOTaskResult  `json:"result"`
}

type dataForSEOTaskResult struct {
	Items []dataForSEOItem `json:"items"`
}

type dataForSEOItem struct {
	Keyword     string              `json:"keyword"`
	KeywordData dataForSEOKWData    `json:"keyword_data"`
	RankedSERP  dataForSEORanked    `json:"ranked_serp_element"`
}

type dataForSEOKWData struct {
	KeywordInfo dataForSEOKWInfo `json:"keyword_info"`
}

type dataForSEOKWInfo struct {
	SearchVolume int `json:"search_volume"`
}

type dataForSEORanked struct {
	SerpItem dataForSEOSerpItem `json:"serp_item"`
}

type dataForSEOSerpItem struct {
	RankAbsolute int `json:"rank_absolute"`
}

// RankedKeyword is the normalised result returned by GetRankedKeywords.
type RankedKeyword struct {
	Keyword      string
	SearchVolume int
	OrganicRank  int
}

// NewDataForSEOClient creates a DataForSEOClient, loading credentials from
// GCP Secret Manager. The secret must be stored as "username:password".
func NewDataForSEOClient(ctx context.Context) (*DataForSEOClient, error) {
	creds, err := loadDataForSEOCredentials(ctx)
	if err != nil {
		return nil, fmt.Errorf("dataforseo: failed to load credentials: %w", err)
	}

	parts := strings.SplitN(creds, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("dataforseo: credentials must be in 'username:password' format")
	}

	return &DataForSEOClient{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		username:   parts[0],
		password:   parts[1],
	}, nil
}

// GetRankedKeywords calls the DataForSEO Amazon ranked keywords endpoint for
// the given ASIN and returns up to 100 ranked keywords for the UK market
// (location_code 2826, language English).
func (c *DataForSEOClient) GetRankedKeywords(ctx context.Context, asin string) ([]RankedKeyword, error) {
	payload := []dataForSEORequest{
		{
			ASIN:         asin,
			LanguageName: "English",
			LocationCode: 2826, // United Kingdom
			Limit:        100,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("dataforseo: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dataForSEOEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("dataforseo: build request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(c.username+":"+c.password)))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("dataforseo: http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("dataforseo: unexpected HTTP %d: %s", resp.StatusCode, string(b))
	}

	var dfsResp dataForSEOResponse
	if err := json.NewDecoder(resp.Body).Decode(&dfsResp); err != nil {
		return nil, fmt.Errorf("dataforseo: decode response: %w", err)
	}

	if dfsResp.StatusCode != 20000 {
		return nil, fmt.Errorf("dataforseo: API error %d: %s", dfsResp.StatusCode, dfsResp.StatusMessage)
	}

	var keywords []RankedKeyword
	for _, task := range dfsResp.Tasks {
		if task.StatusCode != 20000 {
			continue
		}
		for _, result := range task.Result {
			for _, item := range result.Items {
				if item.Keyword == "" {
					continue
				}
				keywords = append(keywords, RankedKeyword{
					Keyword:      item.Keyword,
					SearchVolume: item.KeywordData.KeywordInfo.SearchVolume,
					OrganicRank:  item.RankedSERP.SerpItem.RankAbsolute,
				})
			}
		}
	}

	return keywords, nil
}

// loadDataForSEOCredentials fetches the DataForSEO credentials string from
// GCP Secret Manager. The latest version of the secret is always used.
func loadDataForSEOCredentials(ctx context.Context) (string, error) {
	client, err := secretmanager.NewClient(ctx)
	if err != nil {
		return "", fmt.Errorf("create secret manager client: %w", err)
	}
	defer client.Close()

	name := fmt.Sprintf("projects/%s/secrets/%s/versions/latest", dataForSEOProjectID, dataForSEOSecretKey)
	result, err := client.AccessSecretVersion(ctx, &secretmanagerpb.AccessSecretVersionRequest{
		Name: name,
	})
	if err != nil {
		return "", fmt.Errorf("access secret %s: %w", name, err)
	}

	return string(result.Payload.Data), nil
}

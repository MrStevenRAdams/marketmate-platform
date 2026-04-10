package services

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	cloudtasks "cloud.google.com/go/cloudtasks/apiv2"
	taskspb "cloud.google.com/go/cloudtasks/apiv2/cloudtaskspb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"module-a/marketplace"
	ebayClient "module-a/marketplace/clients/ebay"
	temu "module-a/marketplace/clients/temu"
	"module-a/models"
	"module-a/repository"

	"cloud.google.com/go/firestore"
	"github.com/google/uuid"
)

// ============================================================================
// MARKETPLACE SERVICE - Credential & Connection Management
// ============================================================================

type MarketplaceService struct {
	repo             *repository.MarketplaceRepository
	globalConfigRepo *repository.GlobalConfigRepository
	encryptionKey    []byte
}

func NewMarketplaceService(
	repo *repository.MarketplaceRepository,
	globalConfigRepo *repository.GlobalConfigRepository,
	encryptionKey string,
) *MarketplaceService {
	return &MarketplaceService{
		repo:             repo,
		globalConfigRepo: globalConfigRepo,
		encryptionKey:    []byte(encryptionKey),
	}
}

func (s *MarketplaceService) CreateCredential(ctx context.Context, tenantID string, req models.ConnectMarketplaceRequest) (*models.MarketplaceCredential, error) {
	encryptedData := make(map[string]string)
	encryptedFields := []string{}
	for key, value := range req.Credentials {
		if s.IsSensitiveField(key) {
			encrypted, err := s.Encrypt(value)
			if err != nil {
				return nil, fmt.Errorf("encryption failed: %w", err)
			}
			encryptedData[key] = encrypted
			encryptedFields = append(encryptedFields, key)
		} else {
			encryptedData[key] = value
		}
	}
	credential := &models.MarketplaceCredential{
		CredentialID:    uuid.New().String(),
		TenantID:        tenantID,
		Channel:         req.Channel,
		AccountName:     req.AccountName,
		MarketplaceID:   req.MarketplaceID,
		Environment:     req.Environment,
		CredentialData:  encryptedData,
		EncryptedFields: encryptedFields,
		Active:          true,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	testErr := s.TestConnection(ctx, credential)
	now := time.Now()
	if testErr != nil {
		credential.Active = false
		credential.LastTestStatus = "failed"
		credential.LastErrorMessage = testErr.Error()
	} else {
		credential.LastTestStatus = "success"
		credential.LastTestedAt = &now
	}
	if err := s.repo.SaveCredential(ctx, credential); err != nil {
		return nil, err
	}
	return credential, testErr
}

// SaveCredential persists an updated credential document to Firestore.
// Used by token refresh flows (e.g. Lazada) to write new access_token /
// refresh_token / TokenExpiresAt back after a successful refresh.
func (s *MarketplaceService) SaveCredential(ctx context.Context, cred *models.MarketplaceCredential) error {
	cred.UpdatedAt = time.Now()
	return s.repo.SaveCredential(ctx, cred)
}

func (s *MarketplaceService) buildFullCredentials(ctx context.Context, credential *models.MarketplaceCredential) (map[string]string, error) {
	globalKeys, err := s.globalConfigRepo.GetMarketplaceKeys(ctx, credential.Channel)
	if err != nil {
		globalKeys = map[string]string{}
	}
	tenantKeys, err := s.decryptCredentials(credential)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt tenant credentials: %w", err)
	}
	merged := make(map[string]string)
	for k, v := range globalKeys {
		merged[k] = v
	}
	for k, v := range tenantKeys {
		merged[k] = v
	}
	return merged, nil
}

func (s *MarketplaceService) TestConnection(ctx context.Context, credential *models.MarketplaceCredential) error {
	mergedCreds, err := s.buildFullCredentials(ctx, credential)
	if err != nil {
		return err
	}
	adapter, err := marketplace.GetAdapter(ctx, credential.Channel, marketplace.Credentials{
		MarketplaceID:   credential.Channel,
		Environment:     credential.Environment,
		MarketplaceType: credential.Channel,
		Data:            mergedCreds,
	})
	if err != nil {
		return err
	}
	return adapter.TestConnection(ctx)
}

func (s *MarketplaceService) GetFullCredentials(ctx context.Context, credential *models.MarketplaceCredential) (map[string]string, error) {
	return s.buildFullCredentials(ctx, credential)
}

// GetTemuMallInfo fetches the mall/shop info for a Temu credential.
// Returns the MallInfo (including MallID and MallName) on success, or nil if unavailable.
// Used during TestConnection to store mall_id for webhook routing.
func (s *MarketplaceService) GetTemuMallInfo(ctx context.Context, credential *models.MarketplaceCredential) (*temu.MallInfo, error) {
	mergedCreds, err := s.buildFullCredentials(ctx, credential)
	if err != nil {
		return nil, err
	}
	baseURL := mergedCreds["base_url"]
	if baseURL == "" {
		baseURL = "https://openapi.temu.com"
	}
	appKey := mergedCreds["app_key"]
	appSecret := mergedCreds["app_secret"]
	accessToken := mergedCreds["access_token"]
	if appKey == "" || appSecret == "" || accessToken == "" {
		return nil, fmt.Errorf("incomplete Temu credentials for GetMallInfo")
	}
	client := temu.NewClient(baseURL, appKey, appSecret, accessToken)
	return client.GetMallInfo()
}

// DecryptCredential decrypts a MarketplaceCredential and returns a DecryptedCredential
// struct containing the individual fields the tracking service needs to make API calls.
// The DecryptedCredential type is defined in tracking_service.go (same package).
func (s *MarketplaceService) DecryptCredential(ctx context.Context, cred *models.MarketplaceCredential) (*DecryptedCredential, error) {
	keys, err := s.buildFullCredentials(ctx, cred)
	if err != nil {
		return nil, fmt.Errorf("decrypt credential: %w", err)
	}
	return &DecryptedCredential{
		AccessToken:     cred.AccessToken,
		RefreshToken:    cred.RefreshToken,
		DecryptedAPIKey: keys["api_key"],
		ClientID:        keys["client_id"],
		ClientSecret:    keys["client_secret"],
		LWAClientID:     keys["lwa_client_id"],
		LWAClientSecret: keys["lwa_client_secret"],
		SandboxMode:     cred.Environment == "sandbox",
	}, nil
}

func (s *MarketplaceService) ListCredentials(ctx context.Context, tenantID string) ([]models.MarketplaceCredential, error) {
	return s.repo.ListCredentials(ctx, tenantID)
}

func (s *MarketplaceService) GetCredential(ctx context.Context, tenantID, credentialID string) (*models.MarketplaceCredential, error) {
	return s.repo.GetCredential(ctx, tenantID, credentialID)
}

func (s *MarketplaceService) DeleteCredential(ctx context.Context, tenantID, credentialID string) error {
	return s.repo.DeleteCredential(ctx, tenantID, credentialID)
}

// ============================================================================
// ENCRYPTION HELPERS
// ============================================================================

func (s *MarketplaceService) IsSensitiveField(key string) bool {
	sensitive := []string{"api_key", "api_secret", "secret", "password", "token", "refresh_token", "access_token", "client_secret", "lwa_client_secret", "aws_secret_access_key"}
	for _, field := range sensitive {
		if key == field {
			return true
		}
	}
	return false
}

func (s *MarketplaceService) Encrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(s.encryptionKey)
	if err != nil {
		return "", err
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := aesGCM.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func (s *MarketplaceService) decrypt(ciphertext string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(s.encryptionKey)
	if err != nil {
		return "", err
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonceSize := aesGCM.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertextData := data[:nonceSize], data[nonceSize:]
	plaintext, err := aesGCM.Open(nil, nonce, ciphertextData, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func (s *MarketplaceService) decryptCredentials(credential *models.MarketplaceCredential) (map[string]string, error) {
	decrypted := make(map[string]string)
	for key, value := range credential.CredentialData {
		isEncrypted := false
		for _, field := range credential.EncryptedFields {
			if field == key {
				isEncrypted = true
				break
			}
		}
		if isEncrypted {
			plaintext, err := s.decrypt(value)
			if err != nil {
				return nil, fmt.Errorf("failed to decrypt %s: %w", key, err)
			}
			decrypted[key] = plaintext
		} else {
			decrypted[key] = value
		}
	}
	return decrypted, nil
}

// ============================================================================
// IMPORT SERVICE
// ============================================================================

// MatchAnalyzer is a narrow interface so ImportService can trigger match analysis
// without importing the handlers package (which would be a circular import).
type MatchAnalyzer interface {
	RunAnalysis(ctx context.Context, tenantID, jobID, channel, credentialID string)
}

type ImportService struct {
	repo               *repository.MarketplaceRepository
	productService     *ProductService
	marketplaceService *MarketplaceService
	searchService      *SearchService
	taskService        *TaskService              // optional — nil when Cloud Tasks not configured
	enrichService      *EbayEnrichmentService    // optional — nil disables inline enrichment
	matchAnalyzer      MatchAnalyzer             // optional — triggers review analysis after pending_review jobs
}

func NewImportService(repo *repository.MarketplaceRepository, productService *ProductService, marketplaceService *MarketplaceService) *ImportService {
	return &ImportService{repo: repo, productService: productService, marketplaceService: marketplaceService}
}

// SetSearchService wires the search service after construction (avoids circular deps)
func (s *ImportService) SetSearchService(ss *SearchService) {
	s.searchService = ss
}

// SetTaskService wires the Cloud Tasks service after construction (avoids circular deps)
func (s *ImportService) SetTaskService(ts *TaskService) {
	s.taskService = ts
}

// SetEnrichService wires the eBay enrichment service so that import can
// run enrichment inline (synchronously) instead of via Cloud Tasks.
func (s *ImportService) SetEnrichService(es *EbayEnrichmentService) {
	s.enrichService = es
}

// SetMatchAnalyzer wires the match analysis trigger so pending_review jobs
// automatically kick off duplicate detection when they complete.
func (s *ImportService) SetMatchAnalyzer(ma MatchAnalyzer) {
	s.matchAnalyzer = ma
}

// buildEbayClient constructs an authenticated eBay Browse API client from
// stored credentials. Used for inline enrichment during import.
func (s *ImportService) buildEbayClient(ctx context.Context, tenantID, credentialID string) (*ebayClient.Client, error) {
	cred, err := s.repo.GetCredential(ctx, tenantID, credentialID)
	if err != nil {
		return nil, fmt.Errorf("credential not found: %w", err)
	}
	if cred.Channel != "ebay" {
		return nil, fmt.Errorf("credential %s is not an eBay credential", credentialID)
	}

	clientID := cred.CredentialData["client_id"]
	clientSecret := cred.CredentialData["client_secret"]
	devID := cred.CredentialData["dev_id"]
	production := cred.Environment == "production"

	client := ebayClient.NewClient(clientID, clientSecret, devID, production)

	if refresh := cred.CredentialData["refresh_token"]; refresh != "" {
		client.SetTokens("", refresh)
	} else if token := cred.CredentialData["oauth_token"]; token != "" {
		client.SetTokens(token, "")
	} else {
		return nil, fmt.Errorf("no OAuth token found in credential")
	}

	if username := cred.CredentialData["seller_username"]; username != "" {
		client.SellerUsername = username
	}

	return client, nil
}

func (s *ImportService) StartImportJob(ctx context.Context, tenantID string, req models.ImportProductsRequest) (*models.ImportJob, error) {
	if req.FulfillmentFilter == "" {
		req.FulfillmentFilter = "all"
	}
	if req.StockFilter == "" {
		req.StockFilter = "all"
	}
	// Look up the credential's friendly account name
	accountName := req.ChannelAccountID
	if cred, err := s.repo.GetCredential(ctx, tenantID, req.ChannelAccountID); err == nil && cred != nil {
		accountName = cred.AccountName
	}

	job := &models.ImportJob{
		JobID:             uuid.New().String(),
		TenantID:          tenantID,
		JobType:           req.JobType,
		Channel:           req.Channel,
		ChannelAccountID:  req.ChannelAccountID,
		AccountName:       accountName,
		Status:            "pending",
		ExternalIDs:       req.ExternalIDs,
		FulfillmentFilter: req.FulfillmentFilter,
		StockFilter:       req.StockFilter,
		SyncStock:         req.SyncStock,
		AIOptimize:        req.AIOptimize,
		EnrichData:        req.EnrichData,
		PendingReview:     req.PendingReview,
		TemuStatusFilters: req.TemuStatusFilters,
		EbayListTypes:     req.EbayListTypes,
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}
	if err := s.repo.CreateImportJob(ctx, job); err != nil {
		return nil, err
	}

	// Amazon uses Cloud Functions orchestrator
	// which handles the long-running SP-API report flow outside Cloud Run constraints.
	// All other channels use the local worker (direct API calls).
	if job.Channel == "amazon" {
		go s.triggerOrchestrator(job)
	} else {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[Import] PANIC in local worker for job %s: %v", job.JobID, r)
					// Try to mark job as failed
					failCtx := context.Background()
					failJob, _ := s.repo.GetImportJob(failCtx, tenantID, job.JobID)
					if failJob != nil {
						failJob.Status = "failed"
						failJob.StatusMessage = fmt.Sprintf("Internal error: %v", r)
						now := time.Now()
						failJob.CompletedAt = &now
						s.repo.UpdateImportJob(failCtx, failJob)
					}
				}
			}()
			log.Printf("[Import] Starting local worker for %s import job %s (tenant: %s)", job.Channel, job.JobID, tenantID)
			if err := s.ProcessImportJob(context.Background(), tenantID, job.JobID); err != nil {
				log.Printf("[Import] Local worker error for job %s: %v", job.JobID, err)
			}
			log.Printf("[Import] Local worker finished for job %s", job.JobID)
		}()
	}

	return job, nil
}

func (s *ImportService) triggerOrchestrator(job *models.ImportJob) {
	ctx := context.Background()

	failJob := func(msg string) {
		log.Printf("[Import] triggerOrchestrator FAILED for job %s: %s", job.JobID, msg)
		jobRef := s.repo.GetImportJobRef(ctx, job.TenantID, job.JobID)
		now := time.Now()
		jobRef.Update(ctx, []firestore.Update{
			{Path: "status", Value: "failed"},
			{Path: "status_message", Value: msg},
			{Path: "completed_at", Value: now},
			{Path: "updated_at", Value: now},
		})
	}

	orchestratorURL := os.Getenv("ORCHESTRATOR_FUNCTION_URL")
	if orchestratorURL == "" {
		failJob("Server misconfiguration: ORCHESTRATOR_FUNCTION_URL not set. Contact support.")
		return
	}

	log.Printf("[Import] triggerOrchestrator: job=%s tenant=%s channel=%s credential=%s url=%s",
		job.JobID, job.TenantID, job.Channel, job.ChannelAccountID, orchestratorURL)

	payload := map[string]interface{}{
		"tenant_id":          job.TenantID,
		"job_id":             job.JobID,
		"credential_id":      job.ChannelAccountID,
		"channel":            job.Channel,
		"job_type":           job.JobType,
		"external_ids":       job.ExternalIDs,
		"fulfillment_filter": job.FulfillmentFilter,
		"stock_filter":       job.StockFilter,
		"enrich_data":        job.EnrichData,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		failJob(fmt.Sprintf("Failed to build request: %v", err))
		return
	}

	// Get ID token for Cloud Functions authentication
	idToken, err := getIDToken(orchestratorURL)
	if err != nil {
		log.Printf("[Import] WARN: get ID token failed: %v — trying access token", err)
		idToken, err = getAccessToken()
		if err != nil {
			failJob(fmt.Sprintf("Authentication failed: %v", err))
			return
		}
	}

	req, err := http.NewRequest("POST", orchestratorURL, strings.NewReader(string(body)))
	if err != nil {
		failJob(fmt.Sprintf("Failed to create request: %v", err))
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+idToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		failJob(fmt.Sprintf("Orchestrator unreachable: %v", err))
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		log.Printf("[Import] Orchestrator triggered OK for job %s (HTTP %d)", job.JobID, resp.StatusCode)
	} else {
		failJob(fmt.Sprintf("Orchestrator returned HTTP %d: %s", resp.StatusCode, string(respBody)))
	}
}

func getIDToken(audience string) (string, error) {
	// Use Google metadata server for ID token (works on Cloud Run)
	url := fmt.Sprintf("http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/identity?audience=%s", audience)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Metadata-Flavor", "Google")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(body)), nil
}

func (s *ImportService) ProcessImportJob(ctx context.Context, tenantID, jobID string) error {
	log.Printf("[Import] ProcessImportJob starting: tenant=%s job=%s", tenantID, jobID)
	job, err := s.repo.GetImportJob(ctx, tenantID, jobID)
	if err != nil {
		log.Printf("[Import] Failed to get job: %v", err)
		return err
	}
	log.Printf("[Import] Job loaded: channel=%s, account=%s", job.Channel, job.ChannelAccountID)

	// Check if already cancelled before we do anything
	if job.Status == "cancelled" {
		log.Printf("[Import] Job %s already cancelled — aborting before start", jobID)
		return nil
	}

	job.Status = "running"
	job.StatusMessage = "Connecting to marketplace..."
	now := time.Now()
	job.StartedAt = &now
	if err := s.repo.UpdateImportJob(ctx, job); err != nil {
		log.Printf("[Import] Failed to update job status: %v", err)
		return err
	}

	log.Printf("[Import] Getting credential: %s", job.ChannelAccountID)
	credential, err := s.marketplaceService.GetCredential(ctx, job.TenantID, job.ChannelAccountID)
	if err != nil {
		log.Printf("[Import] Failed to get credential: %v", err)
		return s.failJob(ctx, job, err)
	}
	log.Printf("[Import] Got credential: channel=%s env=%s", credential.Channel, credential.Environment)
	mergedCreds, err := s.marketplaceService.GetFullCredentials(ctx, credential)
	if err != nil {
		log.Printf("[Import] Failed to merge credentials: %v", err)
		return s.failJob(ctx, job, err)
	}
	log.Printf("[Import] Merged credentials: %d keys", len(mergedCreds))
	adapter, err := marketplace.GetAdapter(ctx, job.Channel, marketplace.Credentials{
		MarketplaceID:   job.Channel,
		Environment:     credential.Environment,
		MarketplaceType: job.Channel,
		Data:            mergedCreds,
	})
	if err != nil {
		log.Printf("[Import] Failed to get adapter: %v", err)
		return s.failJob(ctx, job, err)
	}
	log.Printf("[Import] Adapter created for %s, starting fetch...", job.Channel)

	// Update status: fetching data from marketplace
	if len(job.ExternalIDs) > 0 {
		job.StatusMessage = fmt.Sprintf("Fetching %d products from %s...", len(job.ExternalIDs), job.Channel)
	} else {
		job.StatusMessage = fmt.Sprintf("Fetching all products from %s — this may take several minutes...", job.Channel)
	}
	s.repo.UpdateImportJob(ctx, job)

	// seenGoodsIDs tracks every external ID seen during this import run.
	// Used after fetching to reconcile stale Firestore listings (goodsIds that
	// no longer exist on Temu or have been deleted).
	seenGoodsIDs := make(map[string]bool)
	deletedGoodsIDs := make(map[string]bool) // subset: explicitly marked deleted on Temu

	// saveProduct is called for each product — either immediately via ProductCallback
	// or after all fetching is done (fallback for adapters that don't support streaming).
	var cancelled bool // in-memory flag to prevent periodic writes from overwriting cancellation
	saveProduct := func(mpProduct marketplace.MarketplaceProduct) bool {
		if cancelled {
			return false
		}

		// Track this goodsId regardless of status
		if mpProduct.ExternalID != "" {
			seenGoodsIDs[mpProduct.ExternalID] = true
			if mpProduct.MarketplaceStatus == "deleted" {
				deletedGoodsIDs[mpProduct.ExternalID] = true
			}
		}

		// Deleted products: don't create/update PIM entries — just record the ID
		// so reconciliation can clean up the Firestore listing below.
		if mpProduct.MarketplaceStatus == "deleted" {
			job.ProcessedItems++
			return true
		}
		// Check for cancellation every 50 items
		if job.ProcessedItems > 0 && job.ProcessedItems%50 == 0 {
			freshJob, err := s.repo.GetImportJob(ctx, job.TenantID, job.JobID)
			if err == nil && freshJob.Status == "cancelled" {
				log.Printf("[Import] Job %s: cancelled by user at %d processed", job.JobID, job.ProcessedItems)
				cancelled = true
				// Don't overwrite — the cancel endpoint already wrote the correct status.
				// Just stop processing.
				return false
			}
		}

		// Apply filters — skip products that don't match
		if !s.matchesFilters(mpProduct, job.FulfillmentFilter, job.StockFilter) {
			job.SkippedItems++
			job.ProcessedItems++
			return true
		}

		// Check whether the adapter signalled an enrichment failure via RawData.
		// If so, record a structured ImportError with the full SP-API diagnostic
		// fields before deciding what to do with the product.
		if failed, _ := mpProduct.RawData["_enrich_failed"].(bool); failed {
			errMsg, _ := mpProduct.RawData["_enrich_error"].(string)
			reqURL, _ := mpProduct.RawData["_request_url"].(string)
			respBody, _ := mpProduct.RawData["_response_body"].(string)
			statusCode := 0
			if sc, ok := mpProduct.RawData["_status_code"].(int); ok {
				statusCode = sc
			}
			job.ErrorLog = append(job.ErrorLog, models.ImportError{
				ExternalID:   mpProduct.ExternalID,
				ErrorCode:    "ENRICH_FAILED",
				Message:      errMsg,
				Timestamp:    time.Now(),
				RequestURL:   reqURL,
				StatusCode:   statusCode,
				ResponseBody: respBody,
			})
			job.FailedItems++
			job.ProcessedItems++
			return true
		}

		externalID := mpProduct.ExternalID
		if externalID == "" {
			externalID = mpProduct.SKU
		}
		if externalID == "" {
			job.SkippedItems++
			job.ProcessedItems++
			return true
		}

		// Check if already imported
		existingMapping, _ := s.repo.GetMappingByExternalID(ctx, job.TenantID, job.Channel, externalID)
		if existingMapping != nil {
			updates := s.buildProductUpdates(mpProduct)
			if err := s.productService.UpdateProduct(ctx, job.TenantID, existingMapping.ProductID, updates); err != nil {
				log.Printf("[Import] Warning: failed to update product %s: %v", existingMapping.ProductID, err)
			}
			existingMapping.UpdatedAt = time.Now()
			s.repo.UpdateMapping(ctx, existingMapping)
			// Only sync quantity if the job has SyncStock enabled
			if !job.SyncStock {
				mpProduct.Quantity = 0
			}
			s.saveExtendedData(ctx, job, existingMapping.ProductID, externalID, mpProduct)
			s.ensureListingRecord(ctx, job, credential, existingMapping.ProductID, externalID, mpProduct)
			job.UpdatedItems++
		} else {
			s.processImportedProduct(ctx, job, credential, mpProduct)
		}
		job.ProcessedItems++

		job.StatusMessage = fmt.Sprintf("Importing... %d saved (%d new, %d updated)",
			job.SuccessfulItems+job.UpdatedItems, job.SuccessfulItems, job.UpdatedItems)
		if job.ProcessedItems%10 == 0 {
			// Re-check cancellation before writing to avoid overwriting a cancel
			freshJob, err := s.repo.GetImportJob(ctx, job.TenantID, job.JobID)
			if err == nil && freshJob.Status == "cancelled" {
				log.Printf("[Import] Job %s: cancelled detected at periodic write (%d processed)", job.JobID, job.ProcessedItems)
				cancelled = true
				return false
			}
			s.repo.UpdateImportJob(ctx, job)
		}
		return true
	}

	// cancelledDuringFetch is set by the ProgressCallback if it detects cancellation mid-fetch
	var cancelledDuringFetch bool

	// Build the set of external IDs that already have an import mapping in the database.
	// Adapters that do per-product enrichment (Amazon) use this to skip the catalog API
	// call for already-known products, so ProcessedItems ticks instantly for them and
	// only increments after enrichment completes for genuinely new products.
	alreadyMappedIDs := map[string]bool{}
	if allMappings, listErr := s.repo.ListMappings(ctx, job.TenantID); listErr == nil {
		for _, m := range allMappings {
			if m.Channel == job.Channel {
				alreadyMappedIDs[m.ExternalID] = true
			}
		}
	}
	log.Printf("[Import] Job %s: %d ASINs/IDs already mapped for channel %s — will skip enrichment for those", job.JobID, len(alreadyMappedIDs), job.Channel)

	filters := marketplace.ImportFilters{
		ExternalIDs:       job.ExternalIDs,
		PageSize:          50,
		FulfillmentFilter: job.FulfillmentFilter,
		StockFilter:       job.StockFilter,
		TemuStatusFilters: job.TemuStatusFilters,
		EbayListTypes:     job.EbayListTypes,
		AlreadyMappedIDs:  alreadyMappedIDs,
		ProgressCallback: func(message string) bool {
			if cancelled {
				return false
			}
			freshJob, err := s.repo.GetImportJob(ctx, job.TenantID, job.JobID)
			if err == nil && freshJob.Status == "cancelled" {
				log.Printf("[Import] Job %s: cancellation detected during fetch — stopping", job.JobID)
				cancelledDuringFetch = true
				cancelled = true
				return false
			}
			// Adapters can signal the total item count via __total__:N
			// For multi-list-type imports (e.g. eBay Active+Unsold), each list type
			// sends its own __total__ on page 1 — we accumulate across list types.
			// Single-channel adapters (Temu) only send this once so += is correct.
			if strings.HasPrefix(message, "__total__:") {
				var total int
				if _, err := fmt.Sscanf(message, "__total__:%d", &total); err == nil && total > 0 {
					job.TotalItems += total
					s.repo.UpdateImportJob(ctx, job)
				}
				return true
			}
			job.StatusMessage = message
			s.repo.UpdateImportJob(ctx, job)
			return true
		},
		ProductCallback: func(mpProduct marketplace.MarketplaceProduct) bool {
			return saveProduct(mpProduct)
		},
	}
	products, err := adapter.FetchListings(ctx, filters)
	if cancelledDuringFetch || cancelled {
		log.Printf("[Import] Job %s: fetch aborted due to cancellation", job.JobID)
		return nil
	}
	if err != nil {
		return s.failJob(ctx, job, err)
	}

	// For adapters that don't use ProductCallback, process the returned slice now
	for _, mpProduct := range products {
		if !saveProduct(mpProduct) {
			break // cancelled
		}
	}

	// Re-check cancellation — if cancelled during post-fetch processing, don't overwrite
	if cancelled {
		log.Printf("[Import] Job %s: cancelled during processing — not overwriting status", job.JobID)
		return nil
	}

	// ── Temu listing reconciliation ───────────────────────────────────────────
	// After fetching all pages, reconcile Firestore listings against what Temu
	// actually returned. Uses goodsId (channel_identifiers.listing_id) as the
	// stable reference — NOT the SKU, which can be reused after delete/recreate.
	//
	// Rule: if a Firestore listing's goodsId was not seen in this import run
	// (or was explicitly returned as status=6 Deleted), mark the listing as
	// "ended" and delete the import_mapping so a future import can re-create it.
	//
	// Important: we only do this for the specific channel account that was imported,
	// and only when we have a reasonably complete picture (processed > 0 items).
	if job.Channel == "temu" && len(seenGoodsIDs) > 0 && !cancelled {
		job.StatusMessage = "Reconciling listings..."
		s.repo.UpdateImportJob(ctx, job)

		allListings, listErr := s.repo.ListListingsByAccount(ctx, job.TenantID, job.ChannelAccountID)
		if listErr != nil {
			log.Printf("[Import] Temu reconciliation: failed to list listings: %v", listErr)
		} else {
			endedCount := 0
			for _, listing := range allListings {
				if listing.Channel != "temu" {
					continue
				}
				if listing.State == "ended" {
					continue // already reconciled
				}
				goodsID := ""
				if listing.ChannelIdentifiers != nil {
					goodsID = listing.ChannelIdentifiers.ListingID
				}
				if goodsID == "" {
					continue // no goodsId to reconcile against
				}

				shouldEnd := deletedGoodsIDs[goodsID] || !seenGoodsIDs[goodsID]
				if !shouldEnd {
					continue
				}

				reason := "not returned by Temu API"
				if deletedGoodsIDs[goodsID] {
					reason = "deleted on Temu (status=6)"
				}
				log.Printf("[Import] Temu reconciliation: ending listing %s (goodsId=%s) — %s",
					listing.ListingID, goodsID, reason)

				// Mark listing as ended
				listing.State = "ended"
				listing.UpdatedAt = time.Now()
				if err := s.repo.UpdateListing(ctx, &listing); err != nil {
					log.Printf("[Import] Temu reconciliation: failed to update listing %s: %v", listing.ListingID, err)
					continue
				}

				// Delete the import mapping so the product can be re-matched
				// if the seller recreates the listing on Temu with a new goodsId.
				if listing.ProductID != "" {
					mapping, mapErr := s.repo.GetMappingByExternalID(ctx, job.TenantID, job.Channel, goodsID)
					if mapErr == nil && mapping != nil {
						if err := s.repo.DeleteMapping(ctx, job.TenantID, mapping.MappingID); err != nil {
							log.Printf("[Import] Temu reconciliation: failed to delete mapping for goodsId=%s: %v", goodsID, err)
						}
					}
				}

				endedCount++
			}
			if endedCount > 0 {
				log.Printf("[Import] Temu reconciliation: ended %d stale listings for account %s", endedCount, job.ChannelAccountID)
			} else {
				log.Printf("[Import] Temu reconciliation: all %d listings are current", len(allListings))
			}
		}
	}

	log.Printf("[Import] Job %s: completed — %d new, %d updated, %d skipped, %d failed",
		job.JobID, job.SuccessfulItems, job.UpdatedItems, job.SkippedItems, job.FailedItems)

	job.Status = "completed"
	job.StatusMessage = fmt.Sprintf("Imported %d new, updated %d, skipped %d", job.SuccessfulItems, job.UpdatedItems, job.SkippedItems)
	// Correct TotalItems if more items were actually processed than the adapter
	// originally signalled (e.g. eBay Trading API reports per-list-type totals
	// that can be smaller than the combined count across all list types).
	if job.ProcessedItems > job.TotalItems {
		job.TotalItems = job.ProcessedItems
	}
	completedTime := time.Now()
	job.CompletedAt = &completedTime

	// For auto-connect pending_review jobs, kick off match analysis immediately
	// after the job completes so the user can start reviewing without waiting.
	if job.PendingReview && s.matchAnalyzer != nil {
		go s.matchAnalyzer.RunAnalysis(context.Background(), job.TenantID, job.JobID, job.Channel, job.ChannelAccountID)
	}

	return s.repo.UpdateImportJob(ctx, job)
}

func (s *ImportService) processImportedProduct(ctx context.Context, job *models.ImportJob, credential *models.MarketplaceCredential, mpProduct marketplace.MarketplaceProduct) {
	externalID := mpProduct.ExternalID
	if externalID == "" {
		externalID = mpProduct.SKU
	}

	existingMapping, _ := s.repo.GetMappingByExternalID(ctx, job.TenantID, job.Channel, externalID)
	var productID string
	isNew := existingMapping == nil

	// Skip if an active listing already exists for this channel+SKU —
	// the product was previously confirmed and is already live.
	if isNew && job.PendingReview && mpProduct.SKU != "" {
		if existingListing, _ := s.repo.FindListingByChannelAndSKU(ctx, job.TenantID, job.Channel, mpProduct.SKU); existingListing != nil {
			log.Printf("[Import] Skipping pending_review for %s/%s — listing already exists", job.Channel, mpProduct.SKU)
			job.SkippedItems++
			return
		}
	}

	if isNew {
		// Zero out quantity unless this job is configured to sync stock
		if !job.SyncStock {
			mpProduct.Quantity = 0
		}
		pimProduct := s.convertToPIMProduct(mpProduct, job.TenantID)
		pimProduct.ProductID = uuid.New().String()
		productID = pimProduct.ProductID

		if job.PendingReview {
			// Write to pending_imports — product does NOT enter the PIM yet.
			// It will be moved to products/ when the user confirms in Review Mappings.
			productBytes, _ := json.Marshal(pimProduct)
			var pendingDoc map[string]interface{}
			json.Unmarshal(productBytes, &pendingDoc)
			// Add pending_imports metadata
			pendingDoc["import_job_id"] = job.JobID
			pendingDoc["channel"] = job.Channel
			pendingDoc["external_id"] = externalID
			pendingDoc["channel_account_id"] = job.ChannelAccountID
			pendingDoc["enrichment_status"] = "pending"
			pendingDoc["created_at"] = time.Now()
			pendingDoc["updated_at"] = time.Now()

			if err := s.repo.CreatePendingImport(ctx, job.TenantID, pendingDoc); err != nil {
				job.FailedItems++
				job.ErrorLog = append(job.ErrorLog, models.ImportError{
					ExternalID: externalID, ErrorCode: "CREATE_FAILED", Message: err.Error(), Timestamp: time.Now(),
				})
				return
			}

			// Mapping points to pending_imports — source_collection tells confirm action where to find it
			mapping := &models.ImportMapping{
				MappingID: uuid.New().String(), TenantID: job.TenantID, Channel: job.Channel,
				ChannelAccountID: job.ChannelAccountID, ExternalID: externalID, ProductID: productID,
				SourceCollection: "pending_imports",
				SyncEnabled: true, CreatedAt: time.Now(), UpdatedAt: time.Now(),
			}
			s.repo.CreateMapping(ctx, mapping)
			job.SuccessfulItems++

			// Kick off enrichment against pending_imports (non-blocking)
			go s.enrichPendingImport(context.Background(), job.TenantID, productID, externalID, job.ChannelAccountID, job.Channel, mpProduct)
			return
		}

		// Non-pending: create directly in products collection
		err := s.productService.CreateProduct(ctx, pimProduct)
		if err != nil {
			job.FailedItems++
			job.ErrorLog = append(job.ErrorLog, models.ImportError{
				ExternalID: externalID, ErrorCode: "CREATE_FAILED", Message: err.Error(), Timestamp: time.Now(),
			})
			return
		}
		if s.searchService != nil {
			if err := s.searchService.IndexProduct(pimProduct); err != nil {
				log.Printf("[Import] Warning: failed to index product %s in search: %v", productID, err)
			}
		}

		mapping := &models.ImportMapping{
			MappingID: uuid.New().String(), TenantID: job.TenantID, Channel: job.Channel,
			ChannelAccountID: job.ChannelAccountID, ExternalID: externalID, ProductID: productID,
			SourceCollection: "products",
			SyncEnabled: true, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}
		s.repo.CreateMapping(ctx, mapping)
		job.SuccessfulItems++
	} else {
		productID = existingMapping.ProductID
		updates := s.buildProductUpdates(mpProduct)
		if err := s.productService.UpdateProduct(ctx, job.TenantID, productID, updates); err != nil {
			log.Printf("Warning: failed to update product %s: %v", productID, err)
		}
		existingMapping.UpdatedAt = time.Now()
		s.repo.UpdateMapping(ctx, existingMapping)
		job.UpdatedItems++
	}

	// Handle variations
	for _, variation := range mpProduct.Variations {
		s.processVariation(ctx, job, productID, variation)
	}

	// Store extended data
	s.saveExtendedData(ctx, job, productID, externalID, mpProduct)

	// For newly-created eBay products: run Browse enrichment inline so the
	// product is fully enriched before the import counter increments.
	// Previously this was queued via Cloud Tasks; now it runs synchronously.
	if isNew && job.Channel == "ebay" && s.enrichService != nil {
		ean := mpProduct.Identifiers.EAN
		if ean == "" {
			if e, ok := mpProduct.RawData["ean"].(string); ok {
				ean = e
			}
		}

		ebayClient, clientErr := s.buildEbayClient(ctx, job.TenantID, job.ChannelAccountID)
		if clientErr != nil {
			log.Printf("[Import] warning: failed to build eBay client for inline enrichment of product %s: %v", productID, clientErr)
		} else {
			_, enrichErr := s.enrichService.EnrichProduct(ctx, job.TenantID, productID, externalID, ean, job.ChannelAccountID, ebayClient)
			if enrichErr != nil {
				// Non-fatal — bulk enrichment will catch it later
				log.Printf("[Import] warning: inline enrichment failed for product %s: %v", productID, enrichErr)
			} else {
				job.EnrichedItems++
			}
		}
	}

	// Ensure listing record for this connection.
	// For pending_review products we do NOT create a listing yet — the listing
	// is created when the user confirms the product in Review Mappings.
	// Creating it here would make the product appear as "listed" before review.
	if !job.PendingReview || !isNew {
		s.ensureListingRecord(ctx, job, credential, productID, externalID, mpProduct)
	}
}

func (s *ImportService) processVariation(ctx context.Context, job *models.ImportJob, parentProductID string, variation marketplace.Variation) {
	existingMapping, _ := s.repo.GetMappingByExternalID(ctx, job.TenantID, job.Channel, variation.ExternalID)
	if existingMapping != nil {
		return
	}
	variant := &models.Variant{
		VariantID: uuid.New().String(), TenantID: job.TenantID, ProductID: parentProductID,
		SKU: variation.SKU, Attributes: variation.Attributes, Status: "active",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if variation.Price > 0 {
		variant.Pricing = &models.VariantPricing{
			ListPrice: &models.Money{Amount: variation.Price, Currency: "GBP"},
		}
	}
	if err := s.productService.CreateVariant(ctx, variant); err != nil {
		log.Printf("Warning: failed to create variant %s: %v", variation.ExternalID, err)
		return
	}
	mapping := &models.ImportMapping{
		MappingID: uuid.New().String(), TenantID: job.TenantID, Channel: job.Channel,
		ChannelAccountID: job.ChannelAccountID, ExternalID: variation.ExternalID,
		ProductID: parentProductID, VariantID: variant.VariantID,
		SyncEnabled: true, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	s.repo.CreateMapping(ctx, mapping)
}

func (s *ImportService) saveExtendedData(ctx context.Context, job *models.ImportJob, productID, externalID string, mpProduct marketplace.MarketplaceProduct) {
	s.saveExtendedDataToCollection(ctx, job.TenantID, job.Channel, externalID, job.ChannelAccountID, "products", productID, mpProduct)
}

func (s *ImportService) saveExtendedDataToCollection(ctx context.Context, tenantID, channel, externalID, channelAccountID, collection, productID string, mpProduct marketplace.MarketplaceProduct) {
	extData := make(map[string]interface{})
	for k, v := range mpProduct.RawData {
		extData[k] = v
	}
	for k, v := range mpProduct.Attributes {
		extData[k] = v
	}
	if mpProduct.FulfillmentChannel != "" {
		extData["fulfillment_channel"] = mpProduct.FulfillmentChannel
	}
	if mpProduct.Condition != "" {
		extData["condition"] = mpProduct.Condition
	}
	if mpProduct.ListingURL != "" {
		extData["listing_url"] = mpProduct.ListingURL
	}
	if mpProduct.Currency != "" {
		extData["currency"] = mpProduct.Currency
	}
	if mpProduct.Quantity > 0 {
		extData["quantity"] = mpProduct.Quantity
	}
	if len(extData) == 0 {
		return
	}
	sourceKey := fmt.Sprintf("%s_%s", channel, externalID)
	extended := &models.ExtendedProductData{
		SourceKey: sourceKey, ProductID: productID, TenantID: tenantID,
		Source: channel, SourceID: externalID, ChannelAccountID: channelAccountID,
		Data: extData, FetchedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := s.repo.SaveExtendedDataToCollection(ctx, tenantID, collection, extended); err != nil {
		log.Printf("Warning: failed to save extended data for %s: %v", externalID, err)
	}
}

// enrichPendingImport runs eBay enrichment against pending_imports in the background.
// It also saves raw Temu/channel extended_data to the pending_imports subcollection.
// If the product is confirmed before enrichment completes, the mapping's source_collection
// will have been updated to "products" and the next write goes to the right place.
func (s *ImportService) enrichPendingImport(ctx context.Context, tenantID, pendingID, externalID, channelAccountID, channel string, mpProduct marketplace.MarketplaceProduct) {
	// Save raw channel extended_data to pending_imports subcollection
	s.saveExtendedDataToCollection(ctx, tenantID, channel, externalID, channelAccountID, "pending_imports", pendingID, mpProduct)

	// Update enrichment status to running
	s.repo.UpdatePendingImport(ctx, tenantID, pendingID, map[string]interface{}{
		"enrichment_status": "running",
		"updated_at":        time.Now(),
	})

	// Run eBay EAN enrichment if available
	if s.enrichService == nil {
		s.repo.UpdatePendingImport(ctx, tenantID, pendingID, map[string]interface{}{
			"enrichment_status": "complete",
			"updated_at":        time.Now(),
		})
		return
	}

	ean := mpProduct.Identifiers.EAN
	if ean == "" {
		if e, ok := mpProduct.RawData["ean"].(string); ok {
			ean = e
		}
	}

	// Check if the product has been confirmed (mapping moved to products) before we start
	mapping, _ := s.repo.GetMappingByExternalID(ctx, tenantID, channel, externalID)
	if mapping == nil {
		// Mapping deleted — product was rejected during enrichment
		return
	}

	// Determine which collection to write enrichment results to
	targetCollection := mapping.SourceCollection
	if targetCollection == "" {
		targetCollection = "products"
	}
	targetProductID := mapping.ProductID

	ebayClient, err := s.buildEbayClient(ctx, tenantID, channelAccountID)
	if err != nil {
		log.Printf("[enrichPending] no eBay client for %s/%s: %v", tenantID, pendingID, err)
		s.repo.UpdatePendingImport(ctx, tenantID, pendingID, map[string]interface{}{
			"enrichment_status": "complete", // no eBay = not a failure
			"updated_at":        time.Now(),
		})
		return
	}

	_, enrichErr := s.enrichService.EnrichProduct(ctx, tenantID, targetProductID, externalID, ean, channelAccountID, ebayClient)
	status := "complete"
	if enrichErr != nil {
		log.Printf("[enrichPending] enrichment failed for %s: %v", pendingID, enrichErr)
		status = "failed"
	}

	// Only update pending_imports status if the product is still there
	if targetCollection == "pending_imports" {
		s.repo.UpdatePendingImport(ctx, tenantID, pendingID, map[string]interface{}{
			"enrichment_status": status,
			"updated_at":        time.Now(),
		})
	}
}



func (s *ImportService) ensureListingRecord(ctx context.Context, job *models.ImportJob, credential *models.MarketplaceCredential, productID, externalID string, mpProduct marketplace.MarketplaceProduct) {
	existing, _ := s.repo.FindListingByProductAndAccount(ctx, job.TenantID, productID, job.ChannelAccountID)
	if existing != nil {
		existing.UpdatedAt = time.Now()
		existing.ChannelIdentifiers = &models.ChannelIdentifiers{
			ListingID: externalID, SKU: mpProduct.SKU, URL: mpProduct.ListingURL,
		}
		existing.State = "published"
		// Update price & quantity from marketplace
		if mpProduct.Price > 0 || mpProduct.Quantity > 0 {
			if existing.Overrides == nil {
				existing.Overrides = &models.ListingOverrides{}
			}
			if mpProduct.Price > 0 {
				price := mpProduct.Price
				existing.Overrides.Price = &price
			}
			qty := mpProduct.Quantity
			existing.Overrides.Quantity = &qty
		}
		s.repo.UpdateListing(ctx, existing)
		return
	}

	listing := &models.Listing{
		ListingID: uuid.New().String(), TenantID: job.TenantID, ProductID: productID,
		Channel: job.Channel, ChannelAccountID: job.ChannelAccountID,
		MarketplaceID: credential.MarketplaceID, State: "published",
		ChannelIdentifiers: &models.ChannelIdentifiers{
			ListingID: externalID, SKU: mpProduct.SKU, URL: mpProduct.ListingURL,
		},
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	// Populate overrides from marketplace data so edit mode can pre-fill the form
	listing.Overrides = &models.ListingOverrides{
		Title:       mpProduct.Title,
		Description: mpProduct.Description,
	}
	// Category — Temu sends as first entry in Categories slice (the catId string)
	if len(mpProduct.Categories) > 0 {
		listing.Overrides.CategoryMapping = mpProduct.Categories[0]
	}
	if len(mpProduct.Images) > 0 {
		imageURLs := make([]string, 0, len(mpProduct.Images))
		for _, img := range mpProduct.Images {
			if img.URL != "" {
				imageURLs = append(imageURLs, img.URL)
			}
		}
		listing.Overrides.Images = imageURLs
	}
	if mpProduct.Price > 0 {
		price := mpProduct.Price
		listing.Overrides.Price = &price
	}
	qty := mpProduct.Quantity
	listing.Overrides.Quantity = &qty
	if err := s.repo.CreateListing(ctx, listing); err != nil {
		log.Printf("Warning: failed to create listing for %s: %v", externalID, err)
	}
}


func (s *ImportService) matchesFilters(p marketplace.MarketplaceProduct, fulfillmentFilter, stockFilter string) bool {
	if fulfillmentFilter == "fba" && !strings.EqualFold(p.FulfillmentChannel, "AFN") && !strings.EqualFold(p.FulfillmentChannel, "FBA") {
		return false
	}
	if fulfillmentFilter == "merchant" && !strings.EqualFold(p.FulfillmentChannel, "MFN") && !strings.EqualFold(p.FulfillmentChannel, "DEFAULT") && p.FulfillmentChannel != "" {
		return false
	}
	if stockFilter == "in_stock" && !p.IsInStock {
		return false
	}
	return true
}

func (s *ImportService) applyFilters(products []marketplace.MarketplaceProduct, fulfillmentFilter, stockFilter string) []marketplace.MarketplaceProduct {
	if fulfillmentFilter == "all" && stockFilter == "all" {
		return products
	}
	var filtered []marketplace.MarketplaceProduct
	for _, p := range products {
		if fulfillmentFilter == "fba" && !strings.EqualFold(p.FulfillmentChannel, "AFN") && !strings.EqualFold(p.FulfillmentChannel, "FBA") {
			continue
		}
		if fulfillmentFilter == "merchant" && !strings.EqualFold(p.FulfillmentChannel, "MFN") && !strings.EqualFold(p.FulfillmentChannel, "DEFAULT") && p.FulfillmentChannel != "" {
			continue
		}
		if stockFilter == "in_stock" && !p.IsInStock {
			continue
		}
		filtered = append(filtered, p)
	}
	return filtered
}

func (s *ImportService) buildProductUpdates(mp marketplace.MarketplaceProduct) map[string]interface{} {
	updates := map[string]interface{}{"updated_at": time.Now()}
	if mp.Title != "" {
		updates["title"] = mp.Title
	}
	if mp.Description != "" {
		desc := mp.Description
		updates["description"] = &desc
	}
	if mp.Brand != "" {
		brand := mp.Brand
		updates["brand"] = &brand
	}
	if mp.SKU != "" {
		updates["sku"] = mp.SKU
	}
	// Update source price/currency in attributes
	updates["attributes.source_sku"] = mp.SKU
	if mp.Price > 0 {
		updates["attributes.source_price"] = mp.Price
	}
	if mp.Currency != "" {
		updates["attributes.source_currency"] = mp.Currency
	}
	return updates
}

func (s *ImportService) convertToPIMProduct(mp marketplace.MarketplaceProduct, tenantID string) *models.Product {
	assets := []models.ProductAsset{}
	for i, img := range mp.Images {
		role := "gallery"
		if i == 0 {
			role = "primary_image"
		}
		assets = append(assets, models.ProductAsset{
			AssetID: uuid.New().String(), URL: img.URL, Role: role, SortOrder: i,
		})
	}
	var identifiers *models.ProductIdentifiers
	asin := mp.Identifiers.ASIN
	ean := mp.Identifiers.EAN
	upc := mp.Identifiers.UPC
	if asin != "" || ean != "" || upc != "" {
		identifiers = &models.ProductIdentifiers{}
		if asin != "" {
			identifiers.ASIN = &asin
		}
		if ean != "" {
			identifiers.EAN = &ean
		}
		if upc != "" {
			identifiers.UPC = &upc
		}
	}
	desc := mp.Description
	brand := mp.Brand
	productType := "simple"
	if len(mp.Variations) > 0 {
		productType = "parent"
	}
	return &models.Product{
		TenantID: tenantID, Title: mp.Title, Description: &desc, Brand: &brand,
		Status: "active", ProductType: productType, SKU: mp.SKU, Assets: assets, Identifiers: identifiers,
		Attributes: map[string]interface{}{
			"source_sku": mp.SKU, "source_price": mp.Price, "source_currency": mp.Currency,
		},
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
}

func (s *ImportService) failJob(ctx context.Context, job *models.ImportJob, err error) error {
	job.Status = "failed"
	job.StatusMessage = err.Error()
	now := time.Now()
	job.CompletedAt = &now
	job.ErrorLog = append(job.ErrorLog, models.ImportError{
		ErrorCode: "JOB_FAILED", Message: err.Error(), Timestamp: time.Now(),
	})
	s.repo.UpdateImportJob(ctx, job)
	return err
}

func (s *ImportService) ListImportJobs(ctx context.Context, tenantID string) ([]models.ImportJob, error) {
	return s.repo.ListImportJobs(ctx, tenantID)
}

func (s *ImportService) DeleteImportJob(ctx context.Context, tenantID, jobID string) error {
	jobRef := s.repo.GetImportJobRef(ctx, tenantID, jobID)
	// Verify it exists first
	if _, err := jobRef.Get(ctx); err != nil {
		return fmt.Errorf("job not found: %w", err)
	}
	_, err := jobRef.Delete(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete job: %w", err)
	}
	log.Printf("[Import] Job %s deleted by user", jobID)
	return nil
}

func (s *ImportService) GetImportJob(ctx context.Context, tenantID, jobID string) (*models.ImportJob, error) {
	return s.repo.GetImportJob(ctx, tenantID, jobID)
}

func (s *ImportService) CancelImportJob(ctx context.Context, tenantID, jobID string) error {
	// Use targeted field updates instead of full doc write to avoid
	// INDEX_ENTRIES_COUNT_LIMIT_EXCEEDED on bloated job docs
	jobRef := s.repo.GetImportJobRef(ctx, tenantID, jobID)

	// First check current status
	doc, err := jobRef.Get(ctx)
	if err != nil {
		return fmt.Errorf("job not found: %w", err)
	}
	status, _ := doc.Data()["status"].(string)
	if status != "pending" && status != "running" {
		return fmt.Errorf("cannot cancel job with status %q", status)
	}

	now := time.Now()
	_, err = jobRef.Update(ctx, []firestore.Update{
		{Path: "status", Value: "cancelled"},
		{Path: "status_message", Value: "Cancelled by user"},
		{Path: "completed_at", Value: now},
		{Path: "updated_at", Value: now},
	})
	if err != nil {
		return fmt.Errorf("failed to cancel job: %w", err)
	}

	log.Printf("[Import] Job %s: cancel requested by user", jobID)

	// Delete only the tasks belonging to this job — never purge the whole queue
	// as that would affect other tenants' concurrent imports.
	go cancelJobTasks(jobID)

	return nil
}

// cancelJobTasks deletes all Cloud Tasks for a specific job by their deterministic names.
// Tasks are named: job-{jobID}-batch-{0..N} and job-{jobID}-enrich-{0..N}
// We attempt deletion for indices 0..199 (well above any realistic batch count).
func cancelJobTasks(jobID string) {
	projectID := os.Getenv("GCP_PROJECT_ID")
	region := os.Getenv("GCP_REGION")
	if projectID == "" || region == "" {
		log.Printf("[Import] Cannot cancel tasks for job %s: GCP_PROJECT_ID or GCP_REGION not set", jobID)
		return
	}

	token, err := getAccessToken()
	if err != nil {
		log.Printf("[Import] Cannot get access token for task deletion: %v", err)
		return
	}

	type queueTask struct {
		queue string
		name  string
	}

	var tasks []queueTask
	batchQueue := fmt.Sprintf("projects/%s/locations/%s/queues/import-batches", projectID, region)
	enrichQueue := fmt.Sprintf("projects/%s/locations/%s/queues/enrich-products", projectID, region)

	// Generate all possible task names (0..199 covers any realistic import)
	for i := 0; i < 200; i++ {
		tasks = append(tasks,
			queueTask{batchQueue, fmt.Sprintf("%s/tasks/job-%s-batch-%d", batchQueue, jobID, i)},
		)
		// Enrich tasks are named job-{jobID}-enrich-{batchIndex}-{taskIndex}
		// Each batch can produce up to 10 enrich tasks (50 items/batch ÷ 50 per enrich task = 1, but up to 10 for safety)
		for j := 0; j < 10; j++ {
			tasks = append(tasks,
				queueTask{enrichQueue, fmt.Sprintf("%s/tasks/job-%s-enrich-%d-%d", enrichQueue, jobID, i, j)},
			)
		}
	}

	deleted := 0
	for _, t := range tasks {
		url := fmt.Sprintf("https://cloudtasks.googleapis.com/v2/%s", t.name)
		req, err := http.NewRequest("DELETE", url, nil)
		if err != nil {
			continue
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == 200 {
			deleted++
		}
		// 404 = task already executed or never existed — expected and fine
	}

	log.Printf("[Import] Job %s: deleted %d pending tasks", jobID, deleted)
}

func getAccessToken() (string, error) {
	// Use Google metadata server for access token (works on Cloud Run)
	req, err := http.NewRequest("GET",
		"http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token",
		nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Metadata-Flavor", "Google")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var tokenResp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", err
	}
	return tokenResp.AccessToken, nil
}

// ============================================================================
// LISTING SERVICE
// ============================================================================

type ListingService struct {
	repo               *repository.MarketplaceRepository
	marketplaceService *MarketplaceService
	productService     *ProductService
}

func NewListingService(repo *repository.MarketplaceRepository, marketplaceService *MarketplaceService, productService *ProductService) *ListingService {
	return &ListingService{repo: repo, marketplaceService: marketplaceService, productService: productService}
}

func (s *ListingService) CreateListing(ctx context.Context, tenantID string, req models.CreateListingRequest) (*models.Listing, error) {
	listing := &models.Listing{
		ListingID: uuid.New().String(), TenantID: tenantID, ProductID: req.ProductID,
		VariantID: req.VariantID, Channel: req.Channel, ChannelAccountID: req.ChannelAccountID,
		State: "draft", Overrides: req.Overrides, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	validation, err := s.ValidateListing(ctx, listing)
	if err != nil {
		return nil, err
	}
	listing.ValidationState = &models.ValidationState{
		Status: validation.Status, Blockers: validation.Blockers,
		Warnings: validation.Warnings, ValidatedAt: time.Now(),
	}
	if validation.Status == "ok" || validation.Status == "warning" {
		listing.State = "ready"
	} else {
		listing.State = "blocked"
	}
	if err := s.repo.CreateListing(ctx, listing); err != nil {
		return nil, err
	}
	if req.AutoPublish && listing.State == "ready" {
		s.PublishListing(ctx, tenantID, listing.ListingID)
	}
	return listing, nil
}

func (s *ListingService) PublishListing(ctx context.Context, tenantID, listingID string) error {
	listing, err := s.repo.GetListing(ctx, tenantID, listingID)
	if err != nil {
		return err
	}
	if listing.State != "ready" {
		return fmt.Errorf("listing is not ready to publish (state: %s)", listing.State)
	}
	product, err := s.productService.GetProduct(ctx, tenantID, listing.ProductID)
	if err != nil {
		return err
	}
	credential, err := s.marketplaceService.GetCredential(ctx, tenantID, listing.ChannelAccountID)
	if err != nil {
		return err
	}
	mergedCreds, err := s.marketplaceService.GetFullCredentials(ctx, credential)
	if err != nil {
		return err
	}
	adapter, err := marketplace.GetAdapter(ctx, listing.Channel, marketplace.Credentials{
		MarketplaceID: listing.Channel, Environment: credential.Environment,
		MarketplaceType: listing.Channel, Data: mergedCreds,
	})
	if err != nil {
		return err
	}
	listingData := s.prepareListingData(product, listing)
	result, err := adapter.CreateListing(ctx, listingData)
	if err != nil {
		listing.State = "error"
		now := time.Now()
		listing.Health = &models.ListingHealth{
			Status: "needs_attention", LastErrorMessage: err.Error(), LastErrorAt: &now,
		}
		s.repo.UpdateListing(ctx, listing)
		return err
	}
	listing.State = "published"
	listing.ChannelIdentifiers = &models.ChannelIdentifiers{
		ListingID: result.ExternalID, SKU: result.SKU, URL: result.URL,
	}
	now := time.Now()
	listing.LastPublishedAt = &now
	return s.repo.UpdateListing(ctx, listing)
}

func (s *ListingService) ValidateListing(ctx context.Context, listing *models.Listing) (*models.ValidationState, error) {
	product, err := s.productService.GetProduct(ctx, listing.TenantID, listing.ProductID)
	if err != nil {
		return nil, err
	}
	validation := &models.ValidationState{
		Status: "ok", Blockers: []models.ValidationIssue{}, Warnings: []models.ValidationIssue{},
		ValidatedAt: time.Now(),
	}
	if product.Title == "" {
		validation.Status = "blocked"
		validation.Blockers = append(validation.Blockers, models.ValidationIssue{
			Code: "MISSING_TITLE", Message: "Product title is required", Severity: "error",
		})
	}
	if len(product.Assets) == 0 {
		validation.Status = "blocked"
		validation.Blockers = append(validation.Blockers, models.ValidationIssue{
			Code: "MISSING_IMAGES", Message: "At least one product image is required",
			Severity: "error", Remediation: "Upload product images",
		})
	}
	return validation, nil
}

func (s *ListingService) prepareListingData(product *models.Product, listing *models.Listing) marketplace.ListingData {
	desc := ""
	if product.Description != nil {
		desc = *product.Description
	}
	imageURLs := []string{}
	for _, asset := range product.Assets {
		if asset.URL != "" {
			imageURLs = append(imageURLs, asset.URL)
		}
	}
	sku := ""
	if product.Identifiers != nil && product.Identifiers.ASIN != nil {
		sku = *product.Identifiers.ASIN
	}
	price := 0.0
	if p, ok := product.Attributes["source_price"]; ok {
		if pf, ok := p.(float64); ok {
			price = pf
		}
	}
	return marketplace.ListingData{
		ProductID: listing.ProductID, VariantID: listing.VariantID,
		Title: product.Title, Description: desc, Price: price, Images: imageURLs,
		CustomFields: map[string]interface{}{"sku": sku},
	}
}

func (s *ListingService) ListListings(ctx context.Context, tenantID string, channel string) ([]models.Listing, error) {
	return s.repo.ListListings(ctx, tenantID, channel)
}

// ListListingsWithProducts returns listings joined with their product data
func (s *ListingService) ListListingsWithProducts(ctx context.Context, tenantID string, channel string) ([]models.ListingWithProduct, error) {
	result, _, err := s.ListListingsWithProductsPaginated(ctx, tenantID, channel, 0, 0)
	return result, err
}

// ListListingsWithProductsPaginated returns paginated listings joined with product data
func (s *ListingService) ListListingsWithProductsPaginated(ctx context.Context, tenantID string, channel string, limit, offset int) ([]models.ListingWithProduct, int, error) {
	listings, total, err := s.repo.ListListingsPaginated(ctx, tenantID, channel, limit, offset)
	if err != nil {
		return nil, 0, err
	}

	if len(listings) == 0 {
		return []models.ListingWithProduct{}, total, nil
	}

	// Collect unique product IDs
	productIDs := make([]string, 0, len(listings))
	for _, l := range listings {
		if l.ProductID != "" {
			productIDs = append(productIDs, l.ProductID)
		}
	}

	// Batch fetch products
	products, err := s.productService.GetProductsByIDs(ctx, tenantID, productIDs)
	if err != nil {
		log.Printf("[ListListings] WARN: failed to fetch products: %v", err)
		products = map[string]*models.Product{}
	}

	// Fetch all credentials to build account name map
	credentials, err := s.marketplaceService.ListCredentials(ctx, tenantID)
	credMap := make(map[string]*models.MarketplaceCredential, len(credentials))
	if err == nil {
		for i := range credentials {
			c := &credentials[i]
			credMap[c.CredentialID] = c
		}
	} else {
		log.Printf("[ListListings] WARN: failed to fetch credentials: %v", err)
	}

	// Join listing + product + credential data
	result := make([]models.ListingWithProduct, 0, len(listings))
	for _, l := range listings {
		lwp := models.ListingWithProduct{Listing: l}

		// Join product fields
		if p, ok := products[l.ProductID]; ok && p != nil {
			lwp.ProductTitle = p.Title
			if p.Brand != nil {
				lwp.ProductBrand = *p.Brand
			}
			lwp.ProductCategory = p.ProductType
			if len(p.Assets) > 0 {
				lwp.ProductImage = p.Assets[0].URL
			}
			if attrs := p.Attributes; attrs != nil {
				if price, ok := attrs["source_price"].(float64); ok {
					lwp.ProductPrice = price
				}
				if qty, ok := attrs["source_quantity"]; ok {
					switch v := qty.(type) {
					case float64:
						lwp.ProductQty = int(v)
					case int64:
						lwp.ProductQty = int(v)
					}
				}
				if sku, ok := attrs["source_sku"].(string); ok {
					lwp.ProductSKU = sku
				}
			}
			// Fall back to the product's top-level SKU field when source_sku
			// attribute is not set — this covers manually-created products
			// that were never imported from a marketplace.
			if lwp.ProductSKU == "" && p.SKU != "" {
				lwp.ProductSKU = p.SKU
			}
		}

		// Join credential / account name
		if cred, ok := credMap[l.ChannelAccountID]; ok && cred != nil {
			lwp.AccountName = cred.AccountName
		}

		// Surface error message from health or validation blockers
		if l.Health != nil && l.Health.LastErrorMessage != "" {
			lwp.ErrorMessage = l.Health.LastErrorMessage
		} else if l.ValidationState != nil && len(l.ValidationState.Blockers) > 0 {
			lwp.ErrorMessage = l.ValidationState.Blockers[0].Message
		}

		result = append(result, lwp)
	}

	return result, total, nil
}

func (s *ListingService) GetUnlistedProducts(ctx context.Context, tenantID string, channel string, limit, offset int) ([]models.Product, int64, error) {
	// Get all listings for this channel
	listings, err := s.repo.ListListings(ctx, tenantID, channel)
	if err != nil {
		return nil, 0, err
	}

	// Build set of product IDs that already have listings
	listedProducts := make(map[string]bool, len(listings))
	for _, l := range listings {
		listedProducts[l.ProductID] = true
	}

	// Get all products
	allProducts, total, err := s.productService.ListProducts(ctx, tenantID, map[string]interface{}{}, 0, 0)
	if err != nil {
		return nil, 0, err
	}

	// Filter to unlisted
	var unlisted []models.Product
	for _, p := range allProducts {
		if !listedProducts[p.ProductID] {
			unlisted = append(unlisted, p)
		}
	}

	// Apply pagination manually
	unlistedTotal := int64(len(unlisted))
	if offset > 0 && offset < len(unlisted) {
		unlisted = unlisted[offset:]
	} else if offset >= len(unlisted) {
		unlisted = nil
	}
	if limit > 0 && limit < len(unlisted) {
		unlisted = unlisted[:limit]
	}

	_ = total // not used; we compute unlistedTotal ourselves
	return unlisted, unlistedTotal, nil
}

func (s *ListingService) GetListing(ctx context.Context, tenantID, listingID string) (*models.Listing, error) {
	return s.repo.GetListing(ctx, tenantID, listingID)
}

// GetLinkedProduct fetches the product linked to a listing
func (s *ListingService) GetLinkedProduct(ctx context.Context, tenantID, productID string) (*models.Product, error) {
	return s.productService.GetProduct(ctx, tenantID, productID)
}

// GetExtendedDataForProduct finds extended data docs linked to a product
func (s *ListingService) GetExtendedDataForProduct(ctx context.Context, tenantID, productID string) (map[string]interface{}, error) {
	return s.repo.GetExtendedDataByProductID(ctx, tenantID, productID)
}

func (s *ListingService) UpdateListing(ctx context.Context, listing *models.Listing) error {
	listing.UpdatedAt = time.Now()
	return s.repo.UpdateListing(ctx, listing)
}

func (s *ListingService) DeleteListing(ctx context.Context, tenantID, listingID string) error {
	return s.repo.DeleteListing(ctx, tenantID, listingID)
}

// ============================================================================
// ENRICHMENT QUEUEING
// ============================================================================

type enrichItem struct {
	ProductID string `json:"product_id"`
	ASIN      string `json:"asin"`
}

type enrichPayload struct {
	TenantID     string       `json:"tenant_id"`
	JobID        string       `json:"job_id"`
	CredentialID string       `json:"credential_id"`
	Items        []enrichItem `json:"items"`
}

// EnrichSelected queues enrichment for specific listings
func (s *ListingService) EnrichSelected(ctx context.Context, tenantID string, listingIDs []string) (int, error) {
	// Look up listings to get product IDs
	var items []enrichItem
	for _, lid := range listingIDs {
		listing, err := s.repo.GetListing(ctx, tenantID, lid)
		if err != nil {
			continue
		}
		if listing.ProductID == "" {
			continue
		}
		product, err := s.productService.GetProduct(ctx, tenantID, listing.ProductID)
		if err != nil {
			continue
		}
		asin := ""
		if product.Identifiers != nil && product.Identifiers.ASIN != nil {
			asin = *product.Identifiers.ASIN
		}
		if asin == "" {
			continue
		}
		items = append(items, enrichItem{ProductID: product.ProductID, ASIN: asin})
	}

	if len(items) == 0 {
		return 0, nil
	}

	return s.queueEnrichmentTasks(ctx, tenantID, items)
}

// EnrichAllUnenriched queues enrichment for all products without enriched_at
func (s *ListingService) EnrichAllUnenriched(ctx context.Context, tenantID string) (int, error) {
	// Find all products without enriched_at
	unenriched, err := s.repo.GetUnenrichedProductASINs(ctx, tenantID)
	if err != nil {
		return 0, err
	}

	if len(unenriched) == 0 {
		return 0, nil
	}

	items := make([]enrichItem, len(unenriched))
	for i, u := range unenriched {
		items[i] = enrichItem{ProductID: u.ProductID, ASIN: u.ASIN}
	}

	return s.queueEnrichmentTasks(ctx, tenantID, items)
}

// queueEnrichmentTasks sends enrichment items to the Cloud Tasks queue
func (s *ListingService) queueEnrichmentTasks(ctx context.Context, tenantID string, items []enrichItem) (int, error) {
	enrichFnURL := os.Getenv("ENRICH_FUNCTION_URL")
	if enrichFnURL == "" {
		// Fallback — try to construct from project
		enrichFnURL = fmt.Sprintf("https://import-enrich-%s-uc.a.run.app", os.Getenv("GCP_PROJECT_ID"))
	}

	projectID := os.Getenv("GCP_PROJECT_ID")
	region := os.Getenv("GCP_REGION")
	if region == "" {
		region = "us-central1"
	}

	// Get first active Amazon credential for enrichment
	credID, err := s.repo.GetFirstAmazonCredentialID(ctx, tenantID)
	if err != nil || credID == "" {
		return 0, fmt.Errorf("no active Amazon credential found for enrichment")
	}

	queuePath := fmt.Sprintf("projects/%s/locations/%s/queues/enrich-products", projectID, region)

	// Create Cloud Tasks client
	tasksClient, err := cloudtasks.NewClient(ctx)
	if err != nil {
		return 0, fmt.Errorf("cloud tasks client: %w", err)
	}
	defer tasksClient.Close()

	// Get compute SA email for OIDC
	projectNumber := os.Getenv("GCP_PROJECT_NUMBER")
	if projectNumber == "" {
		projectNumber = "487246736287" // fallback
	}
	saEmail := fmt.Sprintf("%s-compute@developer.gserviceaccount.com", projectNumber)

	// Batch items into groups of 50
	batchSize := 50
	taskCount := 0
	perTaskDuration := 30 * time.Second

	for i := 0; i < len(items); i += batchSize {
		end := i + batchSize
		if end > len(items) {
			end = len(items)
		}

		payload := enrichPayload{
			TenantID:     tenantID,
			JobID:        "manual-enrich",
			CredentialID: credID,
			Items:        items[i:end],
		}

		body, _ := json.Marshal(payload)

		scheduleDelay := time.Duration(taskCount) * perTaskDuration

		task := &taskspb.Task{
			MessageType: &taskspb.Task_HttpRequest{
				HttpRequest: &taskspb.HttpRequest{
					HttpMethod: taskspb.HttpMethod_POST,
					Url:        enrichFnURL,
					Headers:    map[string]string{"Content-Type": "application/json"},
					Body:       body,
					AuthorizationHeader: &taskspb.HttpRequest_OidcToken{
						OidcToken: &taskspb.OidcToken{
							ServiceAccountEmail: saEmail,
						},
					},
				},
			},
			ScheduleTime: timestamppb.New(time.Now().Add(scheduleDelay)),
		}

		if _, err := tasksClient.CreateTask(ctx, &taskspb.CreateTaskRequest{
			Parent: queuePath,
			Task:   task,
		}); err != nil {
			log.Printf("[Enrich] ERROR: queue task: %v", err)
		} else {
			taskCount++
		}
	}

	log.Printf("[Enrich] Queued %d items in %d tasks for tenant %s", len(items), taskCount, tenantID)
	return len(items), nil
}

// ============================================================================
// BULK REVISE BY FIELD
// ============================================================================

// allowedReviseFields is the authoritative set of override fields that
// BulkReviseListings is permitted to write.
var allowedReviseFields = map[string]bool{
	"title":       true,
	"description": true,
	"price":       true,
	"attributes":  true,
	"images":      true,
}

// BulkReviseListings writes explicit field values into the overrides of each
// supplied listing. Only the fields listed in the fields slice are touched;
// all other override and listing fields are left intact via firestore.MergeAll.
// Per-listing errors are collected and returned rather than aborting the whole
// operation, matching the behaviour of BulkPublishListings.
func (s *ListingService) BulkReviseListings(
	ctx context.Context,
	tenantID string,
	listingIDs []string,
	fields []string,
	values models.BulkReviseFieldValues,
) (*models.BulkReviseResult, error) {
	// Validate field names server-side
	for _, f := range fields {
		if !allowedReviseFields[f] {
			return nil, fmt.Errorf(
				"invalid field %q: allowed fields are title, description, price, attributes, images", f,
			)
		}
	}

	fieldSet := make(map[string]bool, len(fields))
	for _, f := range fields {
		fieldSet[f] = true
	}

	// Obtain Firestore client directly — we use MergeAll to touch only the
	// override paths we care about, which is not possible through UpdateListing
	// (which replaces the full document).
	fsClient := s.repo.GetFirestoreClient()
	listingsCol := fsClient.Collection("tenants").Doc(tenantID).Collection("listings")

	result := &models.BulkReviseResult{}

	for _, listingID := range listingIDs {
		// Fetch existing listing to read current overrides so we can merge.
		existing, err := s.repo.GetListing(ctx, tenantID, listingID)
		if err != nil {
			result.Failed++
			result.Errors = append(result.Errors, models.BulkReviseError{
				ListingID: listingID,
				Error:     fmt.Sprintf("listing not found: %v", err),
			})
			continue
		}

		// Start from a copy of existing overrides to preserve untouched fields.
		newOverrides := map[string]interface{}{}
		if existing.Overrides != nil {
			if existing.Overrides.Title != "" {
				newOverrides["title"] = existing.Overrides.Title
			}
			if existing.Overrides.Description != "" {
				newOverrides["description"] = existing.Overrides.Description
			}
			if existing.Overrides.CategoryMapping != "" {
				newOverrides["category_mapping"] = existing.Overrides.CategoryMapping
			}
			if existing.Overrides.Price != nil {
				newOverrides["price"] = *existing.Overrides.Price
			}
			if existing.Overrides.Quantity != nil {
				newOverrides["quantity"] = *existing.Overrides.Quantity
			}
			if len(existing.Overrides.Attributes) > 0 {
				newOverrides["attributes"] = existing.Overrides.Attributes
			}
			if len(existing.Overrides.Images) > 0 {
				newOverrides["images"] = existing.Overrides.Images
			}
		}

		// Write the requested fields, overwriting existing values.
		if fieldSet["title"] {
			newOverrides["title"] = values.Title
		}
		if fieldSet["description"] {
			newOverrides["description"] = values.Description
		}
		if fieldSet["price"] && values.Price != nil {
			newOverrides["price"] = *values.Price
		}
		if fieldSet["attributes"] {
			if values.Attributes != nil {
				newOverrides["attributes"] = values.Attributes
			} else {
				newOverrides["attributes"] = map[string]interface{}{}
			}
		}
		if fieldSet["images"] {
			if values.Images != nil {
				newOverrides["images"] = values.Images
			} else {
				newOverrides["images"] = []string{}
			}
		}

		// Write back using MergeAll — only our explicit keys are touched.
		updates := map[string]interface{}{
			"overrides":  newOverrides,
			"updated_at": time.Now(),
		}

		if _, err := listingsCol.Doc(listingID).Set(ctx, updates, firestore.MergeAll); err != nil {
			result.Failed++
			result.Errors = append(result.Errors, models.BulkReviseError{
				ListingID: listingID,
				Error:     fmt.Sprintf("write failed: %v", err),
			})
			continue
		}

		result.Succeeded++
	}

	return result, nil
}

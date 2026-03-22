package repository

import (
	"context"
	"time"
	"fmt"
	"log"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"module-a/models"
)

// ============================================================================
// MARKETPLACE REPOSITORY - Firestore Operations for Module B
// ============================================================================

type MarketplaceRepository struct {
	client *firestore.Client
}

func NewMarketplaceRepository(client *firestore.Client) *MarketplaceRepository {
	return &MarketplaceRepository{
		client: client,
	}
}

// GetFirestoreClient returns the underlying Firestore client for direct queries
func (r *MarketplaceRepository) GetFirestoreClient() *firestore.Client {
	return r.client
}

// ============================================================================
// MARKETPLACE CREDENTIALS
// ============================================================================

func (r *MarketplaceRepository) SaveCredential(ctx context.Context, credential *models.MarketplaceCredential) error {
	docRef := r.client.Collection("tenants").Doc(credential.TenantID).
		Collection("marketplace_credentials").Doc(credential.CredentialID)

	_, err := docRef.Set(ctx, credential)
	return err
}

func (r *MarketplaceRepository) GetCredential(ctx context.Context, tenantID, credentialID string) (*models.MarketplaceCredential, error) {
	docRef := r.client.Collection("tenants").Doc(tenantID).
		Collection("marketplace_credentials").Doc(credentialID)

	doc, err := docRef.Get(ctx)
	if err != nil {
		return nil, err
	}

	var credential models.MarketplaceCredential
	if err := doc.DataTo(&credential); err != nil {
		return nil, err
	}

	return &credential, nil
}

func (r *MarketplaceRepository) ListCredentials(ctx context.Context, tenantID string) ([]models.MarketplaceCredential, error) {
	iter := r.client.Collection("tenants").Doc(tenantID).
		Collection("marketplace_credentials").
		Where("active", "==", true).
		Documents(ctx)

	var credentials []models.MarketplaceCredential
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}

		var credential models.MarketplaceCredential
		if err := doc.DataTo(&credential); err != nil {
			continue
		}
		credentials = append(credentials, credential)
	}

	return credentials, nil
}

func (r *MarketplaceRepository) DeleteCredential(ctx context.Context, tenantID, credentialID string) error {
	docRef := r.client.Collection("tenants").Doc(tenantID).
		Collection("marketplace_credentials").Doc(credentialID)

	_, err := docRef.Delete(ctx)
	return err
}


// ============================================================================
// CREDENTIAL CONFIG
// ============================================================================

func (r *MarketplaceRepository) GetCredentialConfig(ctx context.Context, tenantID, credentialID string) (*models.ChannelConfig, error) {
    docRef := r.client.Collection("tenants").Doc(tenantID).
        Collection("marketplace_credentials").Doc(credentialID)

    doc, err := docRef.Get(ctx)
    if err != nil {
        return nil, err
    }

    var cred models.MarketplaceCredential
    if err := doc.DataTo(&cred); err != nil {
        return nil, err
    }

    return &cred.Config, nil
}

func (r *MarketplaceRepository) UpdateCredentialConfig(ctx context.Context, tenantID, credentialID string, config models.ChannelConfig) error {
    docRef := r.client.Collection("tenants").Doc(tenantID).
        Collection("marketplace_credentials").Doc(credentialID)

    _, err := docRef.Update(ctx, []firestore.Update{
        {Path: "config", Value: config},
        {Path: "updated_at", Value: time.Now()},
    })
    return err
}

func (r *MarketplaceRepository) UpdateCredentialLastSync(ctx context.Context, tenantID, credentialID, status, errMsg string, count int) error {
    docRef := r.client.Collection("tenants").Doc(tenantID).
        Collection("marketplace_credentials").Doc(credentialID)

    _, err := docRef.Update(ctx, []firestore.Update{
        {Path: "config.orders.last_sync", Value: time.Now().UTC().Format(time.RFC3339)},
        {Path: "config.orders.last_sync_status", Value: status},
        {Path: "config.orders.last_sync_count", Value: count},
        {Path: "config.orders.last_sync_error", Value: errMsg},
        {Path: "updated_at", Value: time.Now()},
    })
    return err
}

// ListAllActiveCredentials returns all active credentials across all tenants
// Used by the orchestrator job to find credentials due for order sync
func (r *MarketplaceRepository) ListAllActiveCredentials(ctx context.Context) ([]models.MarketplaceCredential, error) {
    // Firestore doesn't support collection group queries on sub-collections easily
    // so we first list all tenants then query each
    tenantsIter := r.client.Collection("tenants").Documents(ctx)
    var allCreds []models.MarketplaceCredential

    for {
        tenantDoc, err := tenantsIter.Next()
        if err == iterator.Done {
            break
        }
        if err != nil {
            continue
        }

        credsIter := tenantDoc.Ref.Collection("marketplace_credentials").
            Where("active", "==", true).
            Documents(ctx)

        for {
            credDoc, err := credsIter.Next()
            if err == iterator.Done {
                break
            }
            if err != nil {
                continue
            }

            var cred models.MarketplaceCredential
            if err := credDoc.DataTo(&cred); err != nil {
                continue
            }
            allCreds = append(allCreds, cred)
        }
    }

    return allCreds, nil
}

// ============================================================================
// LISTINGS
// ============================================================================

func (r *MarketplaceRepository) CreateListing(ctx context.Context, listing *models.Listing) error {
	docRef := r.client.Collection("tenants").Doc(listing.TenantID).
		Collection("listings").Doc(listing.ListingID)

	_, err := docRef.Set(ctx, listing)
	return err
}

func (r *MarketplaceRepository) GetListing(ctx context.Context, tenantID, listingID string) (*models.Listing, error) {
	docRef := r.client.Collection("tenants").Doc(tenantID).
		Collection("listings").Doc(listingID)

	doc, err := docRef.Get(ctx)
	if err != nil {
		return nil, err
	}

	var listing models.Listing
	if err := doc.DataTo(&listing); err != nil {
		return nil, err
	}

	return &listing, nil
}

func (r *MarketplaceRepository) UpdateListing(ctx context.Context, listing *models.Listing) error {
	docRef := r.client.Collection("tenants").Doc(listing.TenantID).
		Collection("listings").Doc(listing.ListingID)

	_, err := docRef.Set(ctx, listing)
	return err
}

// GetExtendedDataByProductID returns the first extended_data document for a product.
// Reads from the product subcollection: products/{productID}/extended_data
// Falls back to listing the subcollection and returning the first doc found.
func (r *MarketplaceRepository) GetExtendedDataByProductID(ctx context.Context, tenantID, productID string) (map[string]interface{}, error) {
	iter := r.client.Collection("tenants").Doc(tenantID).
		Collection("products").Doc(productID).
		Collection("extended_data").
		Documents(ctx)
	defer iter.Stop()

	doc, err := iter.Next()
	if err != nil {
		return nil, err
	}
	return doc.Data(), nil
}

func (r *MarketplaceRepository) ListListings(ctx context.Context, tenantID string, channel string) ([]models.Listing, error) {
	listings, _, err := r.ListListingsPaginated(ctx, tenantID, channel, 0, 0)
	return listings, err
}

// ListListingsPaginated returns listings with server-side pagination
// limit=0 means return all. Returns (listings, total, error)
// total is estimated as offset + len(results) + 1 if there are more pages
func (r *MarketplaceRepository) ListListingsPaginated(ctx context.Context, tenantID string, channel string, limit, offset int) ([]models.Listing, int, error) {
	col := r.client.Collection("tenants").Doc(tenantID).Collection("listings")

	// Build base query
	var baseQuery firestore.Query
	if channel != "" {
		baseQuery = col.Where("channel", "==", channel).OrderBy("created_at", firestore.Desc)
	} else {
		baseQuery = col.OrderBy("created_at", firestore.Desc)
	}

	// Apply pagination — fetch limit+1 to know if there are more
	query := baseQuery
	fetchLimit := 0
	if limit > 0 {
		fetchLimit = limit + 1
		if offset > 0 {
			query = query.Offset(offset)
		}
		query = query.Limit(fetchLimit)
	}

	iter := query.Documents(ctx)
	var listings []models.Listing
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, 0, err
		}

		var listing models.Listing
		if err := doc.DataTo(&listing); err != nil {
			continue
		}
		listings = append(listings, listing)
	}

	// Determine total estimate
	hasMore := false
	if fetchLimit > 0 && len(listings) > limit {
		hasMore = true
		listings = listings[:limit] // trim the extra one
	}

	total := offset + len(listings)
	if hasMore {
		total = total + 1 // signal there are more
	}

	return listings, total, nil
}

// ListListingsByAccount returns listings for a specific channel account (connection)
func (r *MarketplaceRepository) ListListingsByProductID(ctx context.Context, tenantID, productID string) ([]models.Listing, error) {
	col := r.client.Collection("tenants").Doc(tenantID).Collection("listings")
	iter := col.Where("product_id", "==", productID).Documents(ctx)
	var listings []models.Listing
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		var l models.Listing
		if err := doc.DataTo(&l); err != nil {
			continue
		}
		listings = append(listings, l)
	}
	return listings, nil
}

func (r *MarketplaceRepository) ListListingsByAccount(ctx context.Context, tenantID, channelAccountID string) ([]models.Listing, error) {
	iter := r.client.Collection("tenants").Doc(tenantID).
		Collection("listings").
		Where("channel_account_id", "==", channelAccountID).
		Documents(ctx)

	var listings []models.Listing
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}

		var listing models.Listing
		if err := doc.DataTo(&listing); err != nil {
			continue
		}
		listings = append(listings, listing)
	}

	return listings, nil
}

// FindListingByProductAndAccount checks if a listing already exists for a product+account combo
func (r *MarketplaceRepository) FindListingByProductAndAccount(ctx context.Context, tenantID, productID, channelAccountID string) (*models.Listing, error) {
	iter := r.client.Collection("tenants").Doc(tenantID).
		Collection("listings").
		Where("product_id", "==", productID).
		Where("channel_account_id", "==", channelAccountID).
		Limit(1).
		Documents(ctx)

	doc, err := iter.Next()
	if err != nil {
		return nil, err // iterator.Done or real error
	}

	var listing models.Listing
	if err := doc.DataTo(&listing); err != nil {
		return nil, err
	}

	return &listing, nil
}

func (r *MarketplaceRepository) DeleteListing(ctx context.Context, tenantID, listingID string) error {
	docRef := r.client.Collection("tenants").Doc(tenantID).
		Collection("listings").Doc(listingID)

	_, err := docRef.Delete(ctx)
	return err
}

// ============================================================================
// IMPORT JOBS
// ============================================================================

func (r *MarketplaceRepository) CreateImportJob(ctx context.Context, job *models.ImportJob) error {
	docRef := r.client.Collection("tenants").Doc(job.TenantID).
		Collection("import_jobs").Doc(job.JobID)

	_, err := docRef.Set(ctx, job)
	return err
}

func (r *MarketplaceRepository) GetImportJob(ctx context.Context, tenantID, jobID string) (*models.ImportJob, error) {
	docRef := r.client.Collection("tenants").Doc(tenantID).
		Collection("import_jobs").Doc(jobID)

	doc, err := docRef.Get(ctx)
	if err != nil {
		return nil, err
	}

	var job models.ImportJob
	if err := doc.DataTo(&job); err != nil {
		// DataTo can fail on bloated legacy docs (e.g. large imported_products arrays).
		// Fall back to safe manual extraction of scalar fields only.
		log.Printf("[Repo] GetImportJob DataTo failed for %s/%s, using raw fallback: %v", tenantID, jobID, err)
		job = extractImportJob(jobID, tenantID, doc.Data())
	}

	return &job, nil
}

// GetImportJobRef returns a raw Firestore document reference for targeted updates
func (r *MarketplaceRepository) GetImportJobRef(ctx context.Context, tenantID, jobID string) *firestore.DocumentRef {
	return r.client.Collection("tenants").Doc(tenantID).
		Collection("import_jobs").Doc(jobID)
}

func (r *MarketplaceRepository) UpdateImportJob(ctx context.Context, job *models.ImportJob) error {
	docRef := r.client.Collection("tenants").Doc(job.TenantID).
		Collection("import_jobs").Doc(job.JobID)

	_, err := docRef.Set(ctx, job)
	return err
}

func (r *MarketplaceRepository) ListImportJobs(ctx context.Context, tenantID string) ([]models.ImportJob, error) {
	iter := r.client.Collection("tenants").Doc(tenantID).
		Collection("import_jobs").
		OrderBy("created_at", firestore.Desc).
		Documents(ctx)

	var jobs []models.ImportJob
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}

		job := extractImportJob(doc.Ref.ID, tenantID, doc.Data())
		jobs = append(jobs, job)
	}

	return jobs, nil
}


// extractImportJob builds an ImportJob from a raw Firestore data map.
// Used as a fallback when DataTo fails on bloated documents, and as the
// primary path in ListImportJobs to avoid DataTo failures entirely.
func extractImportJob(jobID, tenantID string, data map[string]interface{}) models.ImportJob {
	var job models.ImportJob
	job.JobID    = jobID
	job.TenantID = tenantID
	toInt := func(v interface{}) int {
		if i, ok := v.(int64); ok { return int(i) }
		if i, ok := v.(int); ok { return i }
		return 0
	}
	if v, ok := data["status"].(string);             ok { job.Status = v }
	if v, ok := data["channel"].(string);             ok { job.Channel = v }
	if v, ok := data["job_type"].(string);            ok { job.JobType = v }
	if v, ok := data["channel_account_id"].(string);  ok { job.ChannelAccountID = v }
	if v, ok := data["status_message"].(string);      ok { job.StatusMessage = v }
	if v, ok := data["enrich_data"].(bool);           ok { job.EnrichData = v }
	job.TotalItems         = toInt(data["total_items"])
	job.ProcessedItems     = toInt(data["processed_items"])
	job.SuccessfulItems    = toInt(data["successful_items"])
	job.FailedItems        = toInt(data["failed_items"])
	job.SkippedItems       = toInt(data["skipped_items"])
	job.UpdatedItems       = toInt(data["updated_items"])
	job.EnrichedItems      = toInt(data["enriched_items"])
	job.EnrichFailedItems  = toInt(data["enrich_failed_items"])
	job.EnrichSkippedItems = toInt(data["enrich_skipped_items"])
	job.EnrichTotalItems   = toInt(data["enrich_total_items"])
	if v, ok := data["created_at"].(time.Time);    ok { job.CreatedAt = v }
	if v, ok := data["updated_at"].(time.Time);    ok { job.UpdatedAt = v }
	if v, ok := data["started_at"].(*time.Time);   ok { job.StartedAt = v }
	if v, ok := data["completed_at"].(*time.Time); ok { job.CompletedAt = v }
	return job
}

func (r *MarketplaceRepository) GetPendingImportJobs(ctx context.Context) ([]models.ImportJob, error) {
	// Collect tenant IDs from multiple sources to handle phantom parents
	tenantIDs := make(map[string]bool)

	// 1. Scan actual tenant documents
	tenantsIter := r.client.Collection("tenants").Documents(ctx)
	for {
		tenantDoc, err := tenantsIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Printf("[PendingJobs] Error scanning tenants: %v", err)
			break
		}
		log.Printf("[PendingJobs] Found tenant doc: %s", tenantDoc.Ref.ID)
		tenantIDs[tenantDoc.Ref.ID] = true
	}

	// 2. Also check known tenant IDs from credentials (handles phantom parent docs)
	knownTenants := []string{"tenant-demo"}
	for _, t := range knownTenants {
		tenantIDs[t] = true
	}

	log.Printf("[PendingJobs] Checking %d tenants: %v", len(tenantIDs), tenantIDs)

	var allJobs []models.ImportJob
	for tenantID := range tenantIDs {
		jobsIter := r.client.Collection("tenants").Doc(tenantID).
			Collection("import_jobs").
			Where("status", "==", "pending").
			Documents(ctx)

		for {
			doc, err := jobsIter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				log.Printf("[PendingJobs] Error querying jobs for tenant %s: %v", tenantID, err)
				break
			}

			var job models.ImportJob
			if err := doc.DataTo(&job); err != nil {
				log.Printf("[PendingJobs] Error parsing job doc: %v", err)
				continue
			}
			log.Printf("[PendingJobs] Found pending job: %s (tenant: %s)", job.JobID, job.TenantID)
			allJobs = append(allJobs, job)
		}
	}

	return allJobs, nil
}

// ============================================================================
// IMPORT MAPPINGS
// ============================================================================

func (r *MarketplaceRepository) CreateMapping(ctx context.Context, mapping *models.ImportMapping) error {
	docRef := r.client.Collection("tenants").Doc(mapping.TenantID).
		Collection("import_mappings").Doc(mapping.MappingID)

	_, err := docRef.Set(ctx, mapping)
	return err
}

func (r *MarketplaceRepository) GetMappingByExternalID(ctx context.Context, tenantID, channel, externalID string) (*models.ImportMapping, error) {
	iter := r.client.Collection("tenants").Doc(tenantID).
		Collection("import_mappings").
		Where("channel", "==", channel).
		Where("external_id", "==", externalID).
		Limit(1).
		Documents(ctx)

	doc, err := iter.Next()
	if err != nil {
		return nil, err
	}

	var mapping models.ImportMapping
	if err := doc.DataTo(&mapping); err != nil {
		return nil, err
	}

	return &mapping, nil
}

func (r *MarketplaceRepository) ListMappings(ctx context.Context, tenantID string) ([]models.ImportMapping, error) {
	iter := r.client.Collection("tenants").Doc(tenantID).
		Collection("import_mappings").
		Documents(ctx)

	var mappings []models.ImportMapping
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}

		var mapping models.ImportMapping
		if err := doc.DataTo(&mapping); err != nil {
			continue
		}
		mappings = append(mappings, mapping)
	}

	return mappings, nil
}

func (r *MarketplaceRepository) UpdateMapping(ctx context.Context, mapping *models.ImportMapping) error {
	docRef := r.client.Collection("tenants").Doc(mapping.TenantID).
		Collection("import_mappings").Doc(mapping.MappingID)

	_, err := docRef.Set(ctx, mapping)
	return err
}

func (r *MarketplaceRepository) DeleteMapping(ctx context.Context, tenantID, mappingID string) error {
	docRef := r.client.Collection("tenants").Doc(tenantID).
		Collection("import_mappings").Doc(mappingID)

	_, err := docRef.Delete(ctx)
	return err
}

// ============================================================================
// EXTENDED PRODUCT DATA
// ============================================================================
// Stored as: tenants/{tenant}/products/{product_id}/extended_data/{source_key}
// Subcollection of the product document. source_key is the document ID.
// ============================================================================

// SaveExtendedData creates or overwrites extended product data for a given source.
// Path: tenants/{tenantID}/products/{productID}/extended_data/{source_key}
func (r *MarketplaceRepository) SaveExtendedData(ctx context.Context, tenantID string, data *models.ExtendedProductData) error {
	docRef := r.client.Collection("tenants").Doc(tenantID).
		Collection("products").Doc(data.ProductID).
		Collection("extended_data").Doc(data.SourceKey)

	_, err := docRef.Set(ctx, data)
	if err != nil {
		return fmt.Errorf("failed to save extended data: %w", err)
	}
	return nil
}

// GetExtendedData retrieves extended product data for a specific source.
// Path: tenants/{tenantID}/products/{productID}/extended_data/{source_key}
func (r *MarketplaceRepository) GetExtendedData(ctx context.Context, tenantID, productID, sourceKey string) (*models.ExtendedProductData, error) {
	docRef := r.client.Collection("tenants").Doc(tenantID).
		Collection("products").Doc(productID).
		Collection("extended_data").Doc(sourceKey)

	doc, err := docRef.Get(ctx)
	if err != nil {
		return nil, err
	}

	var data models.ExtendedProductData
	if err := doc.DataTo(&data); err != nil {
		return nil, err
	}

	return &data, nil
}

// ListExtendedData returns all extended data sources for a product.
// Path: tenants/{tenantID}/products/{productID}/extended_data/*
func (r *MarketplaceRepository) ListExtendedData(ctx context.Context, tenantID, productID string) ([]models.ExtendedProductData, error) {
	iter := r.client.Collection("tenants").Doc(tenantID).
		Collection("products").Doc(productID).
		Collection("extended_data").
		Documents(ctx)
	defer iter.Stop()

	var results []models.ExtendedProductData
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}

		var data models.ExtendedProductData
		if err := doc.DataTo(&data); err != nil {
			continue
		}
		results = append(results, data)
	}

	return results, nil
}

// DeleteExtendedData removes extended data for a specific source.
// Path: tenants/{tenantID}/products/{productID}/extended_data/{source_key}
func (r *MarketplaceRepository) DeleteExtendedData(ctx context.Context, tenantID, productID, sourceKey string) error {
	docRef := r.client.Collection("tenants").Doc(tenantID).
		Collection("products").Doc(productID).
		Collection("extended_data").Doc(sourceKey)

	_, err := docRef.Delete(ctx)
	return err
}

// GetUnenrichedProductASINs returns product IDs and ASINs for products that have no enriched_at timestamp
func (r *MarketplaceRepository) GetUnenrichedProductASINs(ctx context.Context, tenantID string) ([]struct {
	ProductID string
	ASIN      string
}, error) {
	// Query products where enriched_at does not exist
	// Firestore doesn't support "field does not exist" queries directly,
	// so we scan all products and filter client-side
	iter := r.client.Collection("tenants").Doc(tenantID).
		Collection("products").
		Select("identifiers", "enriched_at").
		Documents(ctx)

	type item struct {
		ProductID string
		ASIN      string
	}
	var items []item

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}

		data := doc.Data()

		// Skip if already enriched
		if _, hasEnriched := data["enriched_at"]; hasEnriched {
			continue
		}

		// Get ASIN from identifiers
		asin := ""
		if ids, ok := data["identifiers"].(map[string]interface{}); ok {
			if a, ok := ids["asin"].(string); ok {
				asin = a
			}
		}
		if asin == "" {
			continue
		}

		items = append(items, item{ProductID: doc.Ref.ID, ASIN: asin})
	}

	// Convert to the expected return type
	result := make([]struct {
		ProductID string
		ASIN      string
	}, len(items))
	for i, it := range items {
		result[i].ProductID = it.ProductID
		result[i].ASIN = it.ASIN
	}

	return result, nil
}

// GetFirstAmazonCredentialID returns the ID of the first active Amazon credential for a tenant
func (r *MarketplaceRepository) GetFirstAmazonCredentialID(ctx context.Context, tenantID string) (string, error) {
	iter := r.client.Collection("tenants").Doc(tenantID).
		Collection("marketplace_credentials").
		Where("channel", "==", "amazon").
		Where("active", "==", true).
		Limit(1).
		Documents(ctx)

	doc, err := iter.Next()
	if err != nil {
		return "", fmt.Errorf("no active Amazon credential found")
	}
	return doc.Ref.ID, nil
}

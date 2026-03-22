package repository

import (
	"context"

	"google.golang.org/api/iterator"
	"module-a/models"
)

// ============================================================================
// AI GENERATION JOB REPOSITORY METHODS
// ============================================================================

func (r *MarketplaceRepository) SaveAIGenerationJob(ctx context.Context, job *models.AIGenerationJob) error {
	docRef := r.client.Collection("tenants").Doc(job.TenantID).
		Collection("ai_generation_jobs").Doc(job.JobID)

	_, err := docRef.Set(ctx, job)
	return err
}

func (r *MarketplaceRepository) GetAIGenerationJob(ctx context.Context, tenantID, jobID string) (*models.AIGenerationJob, error) {
	docRef := r.client.Collection("tenants").Doc(tenantID).
		Collection("ai_generation_jobs").Doc(jobID)

	doc, err := docRef.Get(ctx)
	if err != nil {
		return nil, err
	}

	var job models.AIGenerationJob
	if err := doc.DataTo(&job); err != nil {
		return nil, err
	}

	return &job, nil
}

func (r *MarketplaceRepository) ListAIGenerationJobs(ctx context.Context, tenantID string) ([]models.AIGenerationJob, error) {
	iter := r.client.Collection("tenants").Doc(tenantID).
		Collection("ai_generation_jobs").
		OrderBy("created_at", 1). // desc
		Limit(50).
		Documents(ctx)
	defer iter.Stop()

	var jobs []models.AIGenerationJob
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}

		var job models.AIGenerationJob
		if err := doc.DataTo(&job); err != nil {
			continue
		}
		jobs = append(jobs, job)
	}

	return jobs, nil
}

// SaveListing is an alias for CreateListing for consistency with the AI handler
func (r *MarketplaceRepository) SaveListing(ctx context.Context, listing *models.Listing) error {
	return r.CreateListing(ctx, listing)
}

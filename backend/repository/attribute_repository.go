package repository

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/api/iterator"
	"module-a/models"
)

// ============================================================================
// ATTRIBUTE REPOSITORY
// All documents are scoped under tenants/{tenantID}/attributes/{id}
// and tenants/{tenantID}/attribute_sets/{id}
// ============================================================================

// attrCol returns the tenant-scoped attributes collection path.
func (r *FirestoreRepository) attrCol(tenantID string) string {
	return fmt.Sprintf("tenants/%s/attributes", tenantID)
}

// attrSetCol returns the tenant-scoped attribute_sets collection path.
func (r *FirestoreRepository) attrSetCol(tenantID string) string {
	return fmt.Sprintf("tenants/%s/attribute_sets", tenantID)
}

// ─── Attribute CRUD ───────────────────────────────────────────────────────────

func (r *FirestoreRepository) CreateAttribute(ctx context.Context, tenantID string, attr *models.SimpleAttribute) error {
	attr.TenantID = tenantID
	attr.CreatedAt = time.Now()
	attr.UpdatedAt = time.Now()

	_, err := r.client.Collection(r.attrCol(tenantID)).Doc(attr.ID).Set(ctx, attr)
	if err != nil {
		return fmt.Errorf("failed to create attribute: %w", err)
	}
	return nil
}

func (r *FirestoreRepository) GetAttribute(ctx context.Context, tenantID, id string) (*models.SimpleAttribute, error) {
	doc, err := r.client.Collection(r.attrCol(tenantID)).Doc(id).Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("attribute not found: %w", err)
	}

	var attr models.SimpleAttribute
	if err := doc.DataTo(&attr); err != nil {
		return nil, fmt.Errorf("failed to parse attribute: %w", err)
	}
	return &attr, nil
}

func (r *FirestoreRepository) UpdateAttribute(ctx context.Context, tenantID string, attr *models.SimpleAttribute) error {
	existing, err := r.GetAttribute(ctx, tenantID, attr.ID)
	if err != nil {
		return err
	}

	attr.TenantID = existing.TenantID
	attr.CreatedAt = existing.CreatedAt
	attr.UpdatedAt = time.Now()

	_, err = r.client.Collection(r.attrCol(tenantID)).Doc(attr.ID).Set(ctx, attr)
	if err != nil {
		return fmt.Errorf("failed to update attribute: %w", err)
	}
	return nil
}

func (r *FirestoreRepository) DeleteAttribute(ctx context.Context, tenantID, id string) error {
	if _, err := r.GetAttribute(ctx, tenantID, id); err != nil {
		return err
	}
	_, err := r.client.Collection(r.attrCol(tenantID)).Doc(id).Delete(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete attribute: %w", err)
	}
	return nil
}

func (r *FirestoreRepository) ListAttributes(ctx context.Context, tenantID string) ([]*models.SimpleAttribute, error) {
	iter := r.client.Collection(r.attrCol(tenantID)).Documents(ctx)

	var attributes []*models.SimpleAttribute
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to iterate attributes: %w", err)
		}

		var attr models.SimpleAttribute
		if err := doc.DataTo(&attr); err != nil {
			continue
		}
		attributes = append(attributes, &attr)
	}
	return attributes, nil
}

// ─── Attribute Set CRUD ───────────────────────────────────────────────────────

func (r *FirestoreRepository) CreateAttributeSet(ctx context.Context, tenantID string, set *models.SimpleAttributeSet) error {
	set.TenantID = tenantID
	set.CreatedAt = time.Now()
	set.UpdatedAt = time.Now()

	_, err := r.client.Collection(r.attrSetCol(tenantID)).Doc(set.ID).Set(ctx, set)
	if err != nil {
		return fmt.Errorf("failed to create attribute set: %w", err)
	}
	return nil
}

func (r *FirestoreRepository) GetAttributeSet(ctx context.Context, tenantID, id string) (*models.SimpleAttributeSet, error) {
	doc, err := r.client.Collection(r.attrSetCol(tenantID)).Doc(id).Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("attribute set not found: %w", err)
	}

	var set models.SimpleAttributeSet
	if err := doc.DataTo(&set); err != nil {
		return nil, fmt.Errorf("failed to parse attribute set: %w", err)
	}
	return &set, nil
}

func (r *FirestoreRepository) UpdateAttributeSet(ctx context.Context, tenantID string, set *models.SimpleAttributeSet) error {
	existing, err := r.GetAttributeSet(ctx, tenantID, set.ID)
	if err != nil {
		return err
	}

	set.TenantID = existing.TenantID
	set.CreatedAt = existing.CreatedAt
	set.UpdatedAt = time.Now()

	_, err = r.client.Collection(r.attrSetCol(tenantID)).Doc(set.ID).Set(ctx, set)
	if err != nil {
		return fmt.Errorf("failed to update attribute set: %w", err)
	}
	return nil
}

func (r *FirestoreRepository) DeleteAttributeSet(ctx context.Context, tenantID, id string) error {
	if _, err := r.GetAttributeSet(ctx, tenantID, id); err != nil {
		return err
	}
	_, err := r.client.Collection(r.attrSetCol(tenantID)).Doc(id).Delete(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete attribute set: %w", err)
	}
	return nil
}

func (r *FirestoreRepository) ListAttributeSets(ctx context.Context, tenantID string) ([]*models.SimpleAttributeSet, error) {
	iter := r.client.Collection(r.attrSetCol(tenantID)).Documents(ctx)

	var sets []*models.SimpleAttributeSet
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to iterate attribute sets: %w", err)
		}

		var set models.SimpleAttributeSet
		if err := doc.DataTo(&set); err != nil {
			continue
		}
		sets = append(sets, &set)
	}
	return sets, nil
}

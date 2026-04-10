package repository

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"module-a/models"
)

// FirestoreRepository handles all database operations
type FirestoreRepository struct {
	client    *firestore.Client
	projectID string
}

// NewFirestoreRepository creates a new Firestore repository
func NewFirestoreRepository(ctx context.Context, projectID string) (*FirestoreRepository, error) {
	client, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to create firestore client: %v", err)
	}

	return &FirestoreRepository{
		client:    client,
		projectID: projectID,
	}, nil
}

// Close closes the Firestore client
func (r *FirestoreRepository) Close() error {
	return r.client.Close()
}

// GetClient returns the underlying Firestore client for use by other repositories
func (r *FirestoreRepository) GetClient() *firestore.Client {
	return r.client
}

// ==================== PRODUCT METHODS ====================

// CreateProduct creates a new product in Firestore
func (r *FirestoreRepository) CreateProduct(ctx context.Context, product *models.Product) error {
	ref := r.client.Collection("tenants").Doc(product.TenantID).
		Collection("products").Doc(product.ProductID)
	
	_, err := ref.Set(ctx, product)
	if err != nil {
		return fmt.Errorf("failed to create product: %v", err)
	}
	
	return nil
}

// GetProduct retrieves a product by ID
func (r *FirestoreRepository) GetProduct(ctx context.Context, tenantID, productID string) (*models.Product, error) {
	ref := r.client.Collection("tenants").Doc(tenantID).
		Collection("products").Doc(productID)
	
	doc, err := ref.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("product not found: %v", err)
	}
	
	var product models.Product
	if err := doc.DataTo(&product); err != nil {
		return nil, fmt.Errorf("failed to parse product: %v", err)
	}
	
	return &product, nil
}

// UpdateProduct updates an existing product
func (r *FirestoreRepository) UpdateProduct(ctx context.Context, tenantID, productID string, updates map[string]interface{}) error {
	ref := r.client.Collection("tenants").Doc(tenantID).
		Collection("products").Doc(productID)
	
	updates["updated_at"] = firestore.ServerTimestamp
	
	_, err := ref.Update(ctx, mapToUpdates(updates))
	if err != nil {
		return fmt.Errorf("failed to update product: %v", err)
	}
	
	return nil
}

// DeleteProduct deletes a product
func (r *FirestoreRepository) DeleteProduct(ctx context.Context, tenantID, productID string) error {
	ref := r.client.Collection("tenants").Doc(tenantID).
		Collection("products").Doc(productID)

	// Firestore does not cascade-delete subcollections when a document is deleted.
	// Explicitly delete all known subcollection documents first.
	for _, sub := range []string{"extended_data"} {
		iter := ref.Collection(sub).Documents(ctx)
		for {
			doc, err := iter.Next()
			if err != nil {
				break
			}
			if _, delErr := doc.Ref.Delete(ctx); delErr != nil {
				iter.Stop()
				return fmt.Errorf("failed to delete %s/%s: %v", sub, doc.Ref.ID, delErr)
			}
		}
		iter.Stop()
	}

	_, err := ref.Delete(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete product: %v", err)
	}

	return nil
}

// ListProducts retrieves products with optional filters
func (r *FirestoreRepository) ListProducts(ctx context.Context, tenantID string, filters map[string]interface{}, limit, offset int) ([]models.Product, int64, error) {
	baseQuery := r.client.Collection("tenants").Doc(tenantID).Collection("products")

	// Determine if a filter is applied
	hasFilter := false
	var query firestore.Query
	if parentID, ok := filters["parent_id"].(string); ok && parentID != "" {
		query = baseQuery.Where("parent_id", "==", parentID)
		hasFilter = true
	} else if parentASIN, ok := filters["parent_asin"].(string); ok && parentASIN != "" {
		query = baseQuery.Where("attributes.parent_asin", "==", parentASIN)
		hasFilter = true
	} else if status, ok := filters["status"].(string); ok && status != "" {
		query = baseQuery.Where("status", "==", status)
		hasFilter = true
	}
	_ = hasFilter
	
	// Count using baseQuery (works on CollectionRef, not zero-value Query)
	var total int64
	countResult, err := baseQuery.NewAggregationQuery().WithCount("count").Get(ctx)
	if err == nil {
		if countVal, ok := countResult["count"]; ok {
			switch v := countVal.(type) {
			case int64:
				total = v
			case float64:
				total = int64(v)
			}
		}
	} else {
		log.Printf("[Repo] ListProducts count aggregation failed, using 0: %v", err)
	}

	// Build iterator — for no-filter case use baseQuery (CollectionRef) directly.
	// Using the embedded firestore.Query from CollectionRef causes pagination to
	// stop after ~1000 docs. Using CollectionRef.Documents() iterates all pages.
	var iter *firestore.DocumentIterator
	if !hasFilter {
		cr := r.client.Collection("tenants").Doc(tenantID).Collection("products")
		switch {
		case limit > 0 && offset > 0:
			iter = cr.Offset(offset).Limit(limit).Documents(ctx)
		case limit > 0:
			iter = cr.Limit(limit).Documents(ctx)
		case offset > 0:
			iter = cr.Offset(offset).Documents(ctx)
		default:
			iter = cr.Documents(ctx)
		}
	} else {
		if offset > 0 {
			query = query.Offset(offset)
		}
		if limit > 0 {
			query = query.Limit(limit)
		}
		iter = query.Documents(ctx)
	}
	
	var products []models.Product
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, 0, fmt.Errorf("failed to iterate products: %v", err)
		}
		
		var product models.Product
		if err := doc.DataTo(&product); err != nil {
			continue
		}
		
		products = append(products, product)
	}

	// When filtering by status we skipped OrderBy to avoid composite index,
	// so sort in memory by created_at descending.
	if hasFilter {
		sort.Slice(products, func(i, j int) bool {
			return products[i].CreatedAt.After(products[j].CreatedAt)
		})
	}

	return products, total, nil
}

// GetProductsByIDs retrieves multiple products by their IDs in a single pass
func (r *FirestoreRepository) GetProductsByIDs(ctx context.Context, tenantID string, productIDs []string) (map[string]*models.Product, error) {
	result := make(map[string]*models.Product, len(productIDs))
	if len(productIDs) == 0 {
		return result, nil
	}

	// Deduplicate IDs
	seen := make(map[string]bool, len(productIDs))
	unique := make([]string, 0, len(productIDs))
	for _, id := range productIDs {
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}

	// Firestore GetAll supports up to 500 refs per call
	col := r.client.Collection("tenants").Doc(tenantID).Collection("products")
	for i := 0; i < len(unique); i += 500 {
		end := i + 500
		if end > len(unique) {
			end = len(unique)
		}
		batch := unique[i:end]
		refs := make([]*firestore.DocumentRef, len(batch))
		for j, id := range batch {
			refs[j] = col.Doc(id)
		}
		docs, err := r.client.GetAll(ctx, refs)
		if err != nil {
			return nil, fmt.Errorf("batch get products: %v", err)
		}
		for _, doc := range docs {
			if !doc.Exists() {
				continue
			}
			var p models.Product
			if err := doc.DataTo(&p); err != nil {
				continue
			}
			result[p.ProductID] = &p
		}
	}

	return result, nil
}

// ==================== VARIANT METHODS ====================

// CreateVariant creates a new variant
func (r *FirestoreRepository) CreateVariant(ctx context.Context, variant *models.Variant) error {
	ref := r.client.Collection("tenants").Doc(variant.TenantID).
		Collection("variants").Doc(variant.VariantID)
	
	_, err := ref.Set(ctx, variant)
	if err != nil {
		return fmt.Errorf("failed to create variant: %v", err)
	}
	
	return nil
}

// GetVariant retrieves a variant by ID
func (r *FirestoreRepository) GetVariant(ctx context.Context, tenantID, variantID string) (*models.Variant, error) {
	ref := r.client.Collection("tenants").Doc(tenantID).
		Collection("variants").Doc(variantID)
	
	doc, err := ref.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("variant not found: %v", err)
	}
	
	var variant models.Variant
	if err := doc.DataTo(&variant); err != nil {
		return nil, fmt.Errorf("failed to parse variant: %v", err)
	}
	
	return &variant, nil
}

// UpdateVariant updates an existing variant
func (r *FirestoreRepository) UpdateVariant(ctx context.Context, tenantID, variantID string, updates map[string]interface{}) error {
	ref := r.client.Collection("tenants").Doc(tenantID).
		Collection("variants").Doc(variantID)
	
	updates["updated_at"] = firestore.ServerTimestamp
	
	_, err := ref.Update(ctx, mapToUpdates(updates))
	if err != nil {
		return fmt.Errorf("failed to update variant: %v", err)
	}
	
	return nil
}

// DeleteVariant deletes a variant
func (r *FirestoreRepository) DeleteVariant(ctx context.Context, tenantID, variantID string) error {
	ref := r.client.Collection("tenants").Doc(tenantID).
		Collection("variants").Doc(variantID)
	
	_, err := ref.Delete(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete variant: %v", err)
	}
	
	return nil
}

// ListVariants retrieves variants with optional filters and pagination.
//
// Performance note: when limit == 0 (full export / no pagination), the count
// pass is skipped entirely — the caller does not need it and a full collection
// scan just to count is wasteful (it would double the Firestore reads).
func (r *FirestoreRepository) ListVariants(ctx context.Context, tenantID string, filters map[string]interface{}, limit, offset int) ([]models.Variant, int64, error) {
	collRef := r.client.Collection("tenants").Doc(tenantID).Collection("variants")

	var query firestore.Query

	// When filtering by product_id, skip OrderBy to avoid composite index requirement
	if productID, ok := filters["product_id"].(string); ok && productID != "" {
		query = collRef.Where("product_id", "==", productID)
	} else {
		query = collRef.OrderBy("created_at", firestore.Desc)
	}

	// Only count when pagination is actually in use. For full exports (limit=0)
	// the count is never surfaced to the caller, so skip the extra scan entirely.
	var total int64
	if limit > 0 {
		countResult, err := collRef.NewAggregationQuery().WithCount("count").Get(ctx)
		if err == nil {
			if countVal, ok := countResult["count"]; ok {
				switch v := countVal.(type) {
				case int64:
					total = v
				case float64:
					total = int64(v)
				}
			}
		} else {
			log.Printf("[ListVariants] count aggregation failed, using 0: %v", err)
		}
	}

	// Apply pagination
	if offset > 0 {
		query = query.Offset(offset)
	}
	if limit > 0 {
		query = query.Limit(limit)
	}

	iter := query.Documents(ctx)
	defer iter.Stop()

	var variants []models.Variant
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Printf("[ListVariants] iterate error: %v", err)
			return nil, 0, fmt.Errorf("failed to iterate variants: %v", err)
		}

		var variant models.Variant
		if err := doc.DataTo(&variant); err != nil {
			log.Printf("[ListVariants] DataTo error for doc %s: %v", doc.Ref.ID, err)
			continue
		}

		variants = append(variants, variant)
	}

	return variants, total, nil
}

// ==================== CATEGORY METHODS ====================

// CreateCategory creates a new category in Firestore
func (r *FirestoreRepository) CreateCategory(ctx context.Context, category *models.Category) error {
	ref := r.client.Collection("tenants").Doc(category.TenantID).
		Collection("categories").Doc(category.CategoryID)
	
	_, err := ref.Set(ctx, category)
	if err != nil {
		return fmt.Errorf("failed to create category: %v", err)
	}
	
	return nil
}

// UpdateCategory updates an existing category in Firestore
func (r *FirestoreRepository) UpdateCategory(ctx context.Context, tenantID, categoryID string, updates map[string]interface{}) error {
	ref := r.client.Collection("tenants").Doc(tenantID).
		Collection("categories").Doc(categoryID)
	
	updates["updated_at"] = firestore.ServerTimestamp
	
	_, err := ref.Update(ctx, mapToUpdates(updates))
	if err != nil {
		return fmt.Errorf("failed to update category: %v", err)
	}
	
	return nil
}

// GetCategory retrieves a category by ID
func (r *FirestoreRepository) GetCategory(ctx context.Context, tenantID, categoryID string) (*models.Category, error) {
	ref := r.client.Collection("tenants").Doc(tenantID).
		Collection("categories").Doc(categoryID)
	
	doc, err := ref.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("category not found: %v", err)
	}
	
	var category models.Category
	if err := doc.DataTo(&category); err != nil {
		return nil, fmt.Errorf("failed to parse category: %v", err)
	}
	
	return &category, nil
}

// ListCategories retrieves all categories for a tenant
func (r *FirestoreRepository) ListCategories(ctx context.Context, tenantID string) ([]models.Category, error) {
	// Simplified query - sort in memory instead of requiring composite index
	iter := r.client.Collection("tenants").Doc(tenantID).
		Collection("categories").
		Documents(ctx)
	
	var categories []models.Category
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to iterate categories: %v", err)
		}
		
		var category models.Category
		if err := doc.DataTo(&category); err != nil {
			continue
		}
		
		categories = append(categories, category)
	}
	
	// Sort in memory (simpler than composite index)
	// Sort by sort_order first, then by name
	sort.Slice(categories, func(i, j int) bool {
		if categories[i].SortOrder != categories[j].SortOrder {
			return categories[i].SortOrder < categories[j].SortOrder
		}
		return categories[i].Name < categories[j].Name
	})
	
	return categories, nil
}

// CategoryTree represents a category with its children for tree view
type CategoryTree struct {
	models.Category
	Children []CategoryTree `json:"children"`
}

// GetCategoryTree retrieves all categories and builds a hierarchical tree
func (r *FirestoreRepository) GetCategoryTree(ctx context.Context, tenantID string) ([]CategoryTree, error) {
	// Get all categories
	categories, err := r.ListCategories(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	
	// Build tree structure
	return buildCategoryTree(categories), nil
}

// buildCategoryTree converts flat category list to hierarchical tree
func buildCategoryTree(categories []models.Category) []CategoryTree {
	// Create a map for quick lookup
	catMap := make(map[string]*CategoryTree)
	var rootCategories []CategoryTree
	
	log.Printf("🌳 Building category tree from %d categories", len(categories))
	
	// First pass: Create tree nodes
	for _, cat := range categories {
		log.Printf("   Category: %s (ID: %s, ParentID: %v)", cat.Name, cat.CategoryID, cat.ParentID)
		catMap[cat.CategoryID] = &CategoryTree{
			Category: cat,
			Children: []CategoryTree{},
		}
	}
	
	// Second pass: Build tree structure
	for _, cat := range categories {
		node := catMap[cat.CategoryID]
		
		if cat.ParentID == nil || *cat.ParentID == "" {
			// Root category
			log.Printf("   ✅ %s is ROOT category", cat.Name)
			rootCategories = append(rootCategories, *node)
		} else {
			// Child category - add to parent
			if parent, exists := catMap[*cat.ParentID]; exists {
				log.Printf("   ✅ %s is CHILD of %s", cat.Name, parent.Name)
				parent.Children = append(parent.Children, *node)
			} else {
				// Parent not found, treat as root
				log.Printf("   ⚠️  %s parent ID %s NOT FOUND, treating as root", cat.Name, *cat.ParentID)
				rootCategories = append(rootCategories, *node)
			}
		}
	}
	
	log.Printf("🌳 Tree built: %d root categories", len(rootCategories))
	for _, root := range rootCategories {
		log.Printf("   ROOT: %s (%d children)", root.Name, len(root.Children))
	}
	
	return rootCategories
}

// DeleteCategory deletes a category from Firestore
func (r *FirestoreRepository) DeleteCategory(ctx context.Context, tenantID, categoryID string) error {
	ref := r.client.Collection("tenants").Doc(tenantID).
		Collection("categories").Doc(categoryID)
	
	_, err := ref.Delete(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete category: %v", err)
	}
	
	return nil
}

// ==================== JOB METHODS ====================

// CreateJob creates a new job
func (r *FirestoreRepository) CreateJob(ctx context.Context, job *models.Job) error {
	ref := r.client.Collection("tenants").Doc(job.TenantID).
		Collection("jobs").Doc(job.JobID)
	
	_, err := ref.Set(ctx, job)
	if err != nil {
		return fmt.Errorf("failed to create job: %v", err)
	}
	
	return nil
}

// GetJob retrieves a job by ID
func (r *FirestoreRepository) GetJob(ctx context.Context, tenantID, jobID string) (*models.Job, error) {
	ref := r.client.Collection("tenants").Doc(tenantID).
		Collection("jobs").Doc(jobID)
	
	doc, err := ref.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("job not found: %v", err)
	}
	
	var job models.Job
	if err := doc.DataTo(&job); err != nil {
		return nil, fmt.Errorf("failed to parse job: %v", err)
	}
	
	return &job, nil
}

// ListJobs retrieves jobs for a tenant
func (r *FirestoreRepository) ListJobs(ctx context.Context, tenantID string, limit, offset int) ([]models.Job, int64, error) {
	query := r.client.Collection("tenants").Doc(tenantID).
		Collection("jobs").
		OrderBy("created_at", firestore.Desc)
	
	// Get total count
	var total int64
	countIter := query.Documents(ctx)
	for {
		_, err := countIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, 0, fmt.Errorf("failed to count jobs: %v", err)
		}
		total++
	}
	
	// Apply pagination
	if offset > 0 {
		query = query.Offset(offset)
	}
	if limit > 0 {
		query = query.Limit(limit)
	}
	
	iter := query.Documents(ctx)
	
	var jobs []models.Job
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, 0, fmt.Errorf("failed to iterate jobs: %v", err)
		}
		
		var job models.Job
		if err := doc.DataTo(&job); err != nil {
			continue
		}
		
		jobs = append(jobs, job)
	}
	
	return jobs, total, nil
}

// ==================== HELPER FUNCTIONS ====================

// mapToUpdates converts a map[string]interface{} to firestore.Update slice
func mapToUpdates(m map[string]interface{}) []firestore.Update {
	updates := make([]firestore.Update, 0, len(m))
	for key, value := range m {
		updates = append(updates, firestore.Update{
			Path:  key,
			Value: value,
		})
	}
	return updates
}

// createSlug creates a URL-friendly slug from a string
func createSlug(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	// Remove special characters (basic implementation)
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return -1
	}, s)
	return s
}

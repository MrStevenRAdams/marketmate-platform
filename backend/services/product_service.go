package services

import (
	"context"

	"module-a/models"
	"module-a/repository"
)

type ProductService struct {
	repo *repository.FirestoreRepository
}

func NewProductService(repo *repository.FirestoreRepository) *ProductService {
	return &ProductService{
		repo: repo,
	}
}

// CreateProduct creates a new product
func (s *ProductService) CreateProduct(ctx context.Context, product *models.Product) error {
	return s.repo.CreateProduct(ctx, product)
}

// GetProduct retrieves a product by ID
func (s *ProductService) GetProduct(ctx context.Context, tenantID, productID string) (*models.Product, error) {
	return s.repo.GetProduct(ctx, tenantID, productID)
}

// UpdateProduct updates a product
func (s *ProductService) UpdateProduct(ctx context.Context, tenantID, productID string, updates map[string]interface{}) error {
	return s.repo.UpdateProduct(ctx, tenantID, productID, updates)
}

// DeleteProduct deletes a product
func (s *ProductService) DeleteProduct(ctx context.Context, tenantID, productID string) error {
	return s.repo.DeleteProduct(ctx, tenantID, productID)
}

// ListProducts lists products with pagination
func (s *ProductService) ListProducts(ctx context.Context, tenantID string, filters map[string]interface{}, limit, offset int) ([]models.Product, int64, error) {
	return s.repo.ListProducts(ctx, tenantID, filters, limit, offset)
}

// CreateVariant creates a new variant
func (s *ProductService) CreateVariant(ctx context.Context, variant *models.Variant) error {
	return s.repo.CreateVariant(ctx, variant)
}

// GetVariant retrieves a variant by ID
func (s *ProductService) GetVariant(ctx context.Context, tenantID, variantID string) (*models.Variant, error) {
	return s.repo.GetVariant(ctx, tenantID, variantID)
}

// UpdateVariant updates a variant
func (s *ProductService) UpdateVariant(ctx context.Context, tenantID, variantID string, updates map[string]interface{}) error {
	return s.repo.UpdateVariant(ctx, tenantID, variantID, updates)
}

// DeleteVariant deletes a variant
func (s *ProductService) DeleteVariant(ctx context.Context, tenantID, variantID string) error {
	return s.repo.DeleteVariant(ctx, tenantID, variantID)
}

// ListVariants lists variants with pagination
func (s *ProductService) ListVariants(ctx context.Context, tenantID string, filters map[string]interface{}, limit, offset int) ([]models.Variant, int64, error) {
	return s.repo.ListVariants(ctx, tenantID, filters, limit, offset)
}

// CreateCategory creates a new category
func (s *ProductService) CreateCategory(ctx context.Context, category *models.Category) error {
	return s.repo.CreateCategory(ctx, category)
}

// GetCategory retrieves a category by ID
func (s *ProductService) GetCategory(ctx context.Context, tenantID, categoryID string) (*models.Category, error) {
	return s.repo.GetCategory(ctx, tenantID, categoryID)
}

// UpdateCategory updates a category
func (s *ProductService) UpdateCategory(ctx context.Context, tenantID, categoryID string, updates map[string]interface{}) error {
	return s.repo.UpdateCategory(ctx, tenantID, categoryID, updates)
}

// DeleteCategory deletes a category
func (s *ProductService) DeleteCategory(ctx context.Context, tenantID, categoryID string) error {
	return s.repo.DeleteCategory(ctx, tenantID, categoryID)
}

// ListCategories lists all categories
func (s *ProductService) ListCategories(ctx context.Context, tenantID string) ([]models.Category, error) {
	return s.repo.ListCategories(ctx, tenantID)
}

// GetCategoryTree returns categories in hierarchical tree structure
func (s *ProductService) GetCategoryTree(ctx context.Context, tenantID string) ([]repository.CategoryTree, error) {
	return s.repo.GetCategoryTree(ctx, tenantID)
}

// CreateJob creates a new job
func (s *ProductService) CreateJob(ctx context.Context, job *models.Job) error {
	return s.repo.CreateJob(ctx, job)
}

// GetJob retrieves a job by ID
func (s *ProductService) GetJob(ctx context.Context, tenantID, jobID string) (*models.Job, error) {
	return s.repo.GetJob(ctx, tenantID, jobID)
}

// ListJobs lists jobs with pagination
func (s *ProductService) ListJobs(ctx context.Context, tenantID string, limit, offset int) ([]models.Job, int64, error) {
	return s.repo.ListJobs(ctx, tenantID, limit, offset)
}

// GetProductsByIDs retrieves multiple products by their IDs in a single batch
func (s *ProductService) GetProductsByIDs(ctx context.Context, tenantID string, productIDs []string) (map[string]*models.Product, error) {
	return s.repo.GetProductsByIDs(ctx, tenantID, productIDs)
}

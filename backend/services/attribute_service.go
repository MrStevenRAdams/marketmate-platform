package services

import (
	"context"

	"module-a/models"
	"module-a/repository"
)

type AttributeService struct {
	repo *repository.FirestoreRepository
}

func NewAttributeService(repo *repository.FirestoreRepository) *AttributeService {
	return &AttributeService{repo: repo}
}

// Attribute methods

func (s *AttributeService) CreateAttribute(ctx context.Context, tenantID string, attr *models.SimpleAttribute) (*models.SimpleAttribute, error) {
	if err := s.repo.CreateAttribute(ctx, tenantID, attr); err != nil {
		return nil, err
	}
	return attr, nil
}

func (s *AttributeService) GetAttribute(ctx context.Context, tenantID, id string) (*models.SimpleAttribute, error) {
	return s.repo.GetAttribute(ctx, tenantID, id)
}

func (s *AttributeService) UpdateAttribute(ctx context.Context, tenantID string, attr *models.SimpleAttribute) (*models.SimpleAttribute, error) {
	if err := s.repo.UpdateAttribute(ctx, tenantID, attr); err != nil {
		return nil, err
	}
	return attr, nil
}

func (s *AttributeService) DeleteAttribute(ctx context.Context, tenantID, id string) error {
	return s.repo.DeleteAttribute(ctx, tenantID, id)
}

func (s *AttributeService) ListAttributes(ctx context.Context, tenantID string) ([]*models.SimpleAttribute, error) {
	return s.repo.ListAttributes(ctx, tenantID)
}

// Attribute Set methods

func (s *AttributeService) CreateAttributeSet(ctx context.Context, tenantID string, set *models.SimpleAttributeSet) (*models.SimpleAttributeSet, error) {
	if err := s.repo.CreateAttributeSet(ctx, tenantID, set); err != nil {
		return nil, err
	}
	return set, nil
}

func (s *AttributeService) GetAttributeSet(ctx context.Context, tenantID, id string) (*models.SimpleAttributeSet, error) {
	return s.repo.GetAttributeSet(ctx, tenantID, id)
}

func (s *AttributeService) UpdateAttributeSet(ctx context.Context, tenantID string, set *models.SimpleAttributeSet) (*models.SimpleAttributeSet, error) {
	if err := s.repo.UpdateAttributeSet(ctx, tenantID, set); err != nil {
		return nil, err
	}
	return set, nil
}

func (s *AttributeService) DeleteAttributeSet(ctx context.Context, tenantID, id string) error {
	return s.repo.DeleteAttributeSet(ctx, tenantID, id)
}

func (s *AttributeService) ListAttributeSets(ctx context.Context, tenantID string) ([]*models.SimpleAttributeSet, error) {
	return s.repo.ListAttributeSets(ctx, tenantID)
}

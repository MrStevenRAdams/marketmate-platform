package services

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

type StorageService struct {
	client     *storage.Client
	bucketName string
}

func NewStorageService(credentialsFile, bucketName string) (*StorageService, error) {
	ctx := context.Background()

	var client *storage.Client
	var err error

	if credentialsFile != "" {
		// Explicit credentials file provided (local dev with serviceAccountKey.json)
		client, err = storage.NewClient(ctx, option.WithCredentialsFile(credentialsFile))
	} else {
		// No file specified — use Application Default Credentials.
		// On Cloud Run this is the attached service account; locally it uses
		// the credentials from `gcloud auth application-default login`.
		client, err = storage.NewClient(ctx)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create storage client: %w", err)
	}

	return &StorageService{
		client:     client,
		bucketName: bucketName,
	}, nil
}

// BuildPath creates the GCS path following the tenant structure
// Examples:
//   - {tenantid}/products/{sku}/images/main.jpg
//   - {tenantid}/products/{sku}/files/spec.pdf
//   - {tenantid}/categories/{categoryid}/images/banner.jpg
func (s *StorageService) BuildPath(tenantID, entityType, entityID, subFolder, filename string) string {
	// tenantID/entityType/entityID/subFolder/filename
	// e.g., tenant-123/products/SKU-001/images/main.jpg
	return filepath.Join(tenantID, entityType, entityID, subFolder, filename)
}

// Upload uploads a file to GCS
func (s *StorageService) Upload(ctx context.Context, path string, file io.Reader, contentType string) (string, error) {
	wc := s.client.Bucket(s.bucketName).Object(path).NewWriter(ctx)
	wc.ContentType = contentType
	wc.CacheControl = "public, max-age=86400" // Cache for 1 day

	if _, err := io.Copy(wc, file); err != nil {
		wc.Close()
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	if err := wc.Close(); err != nil {
		return "", fmt.Errorf("failed to close writer: %w", err)
	}

	// Return public URL
	publicURL := fmt.Sprintf("https://storage.googleapis.com/%s/%s", s.bucketName, path)
	return publicURL, nil
}

// UploadWithPath is a convenience method that builds the path and uploads
func (s *StorageService) UploadWithPath(ctx context.Context, tenantID, entityType, entityID, subFolder, filename string, file io.Reader, contentType string) (string, string, error) {
	path := s.BuildPath(tenantID, entityType, entityID, subFolder, filename)
	url, err := s.Upload(ctx, path, file, contentType)
	if err != nil {
		return "", "", err
	}
	return url, path, nil
}

// Delete deletes a file from GCS
func (s *StorageService) Delete(ctx context.Context, path string) error {
	obj := s.client.Bucket(s.bucketName).Object(path)
	if err := obj.Delete(ctx); err != nil {
		return fmt.Errorf("failed to delete file: %w", err)
	}
	return nil
}

// DeleteMultiple deletes multiple files
func (s *StorageService) DeleteMultiple(ctx context.Context, paths []string) error {
	for _, path := range paths {
		if err := s.Delete(ctx, path); err != nil {
			// Log error but continue with other deletions
			fmt.Printf("Failed to delete %s: %v\n", path, err)
		}
	}
	return nil
}

// List lists all files in a folder
func (s *StorageService) List(ctx context.Context, prefix string) ([]string, error) {
	var files []string

	it := s.client.Bucket(s.bucketName).Objects(ctx, &storage.Query{
		Prefix: prefix,
	})

	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to list files: %w", err)
		}
		files = append(files, attrs.Name)
	}

	return files, nil
}

// GetSignedURL generates a temporary signed URL for private access
func (s *StorageService) GetSignedURL(ctx context.Context, path string, expiryMinutes int) (string, error) {
	opts := &storage.SignedURLOptions{
		Method:  "GET",
		Expires: time.Now().Add(time.Duration(expiryMinutes) * time.Minute),
	}

	url, err := s.client.Bucket(s.bucketName).SignedURL(path, opts)
	if err != nil {
		return "", fmt.Errorf("failed to generate signed URL: %w", err)
	}

	return url, nil
}

// ValidateTenantAccess ensures the path belongs to the tenant
func (s *StorageService) ValidateTenantAccess(tenantID, path string) bool {
	// Path must start with tenantID/
	return strings.HasPrefix(path, tenantID+"/")
}

// GetContentType determines content type from filename
func GetContentType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	contentTypes := map[string]string{
		".jpg":  "image/jpeg",
		".jpeg": "image/jpeg",
		".png":  "image/png",
		".gif":  "image/gif",
		".webp": "image/webp",
		".pdf":  "application/pdf",
		".csv":  "text/csv",
		".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		".zip":  "application/zip",
	}

	if ct, ok := contentTypes[ext]; ok {
		return ct
	}
	return "application/octet-stream"
}

// SanitizeFilename removes dangerous characters from filename
func SanitizeFilename(filename string) string {
	// Remove path separators and dangerous characters
	filename = filepath.Base(filename)
	filename = strings.ReplaceAll(filename, " ", "-")
	filename = strings.ToLower(filename)
	return filename
}

// Close closes the storage client
func (s *StorageService) Close() error {
	return s.client.Close()
}

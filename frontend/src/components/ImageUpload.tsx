import React, { useState } from 'react';
import { fileService } from '../services/api';

interface Props {
  entityType: string;      // 'products', 'categories', etc.
  entityID: string;        // SKU, category ID, etc.
  subFolder?: string;      // 'images', 'files' (default: 'images')
  currentImageUrl?: string;
  onUploadSuccess: (url: string, path: string) => void;
  onUploadError?: (error: Error) => void;
  onDelete?: () => void;
  acceptedFormats?: string; // e.g., "image/*" or ".jpg,.png"
  maxSizeMB?: number;
}

export default function ImageUpload({
  entityType,
  entityID,
  subFolder = 'images',
  currentImageUrl,
  onUploadSuccess,
  onUploadError,
  onDelete,
  acceptedFormats = 'image/*',
  maxSizeMB = 5
}: Props) {
  const [uploading, setUploading] = useState(false);
  const [progress, setProgress] = useState(0);
  const [preview, setPreview] = useState<string | null>(currentImageUrl || null);
  const [error, setError] = useState<string | null>(null);

  const handleFileSelect = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;

    // Validate file size
    if (file.size > maxSizeMB * 1024 * 1024) {
      setError(`File size must be less than ${maxSizeMB}MB`);
      return;
    }

    // Show preview for images
    if (file.type.startsWith('image/')) {
      const reader = new FileReader();
      reader.onload = (e) => setPreview(e.target?.result as string);
      reader.readAsDataURL(file);
    }

    // Upload
    try {
      setUploading(true);
      setError(null);
      setProgress(0);

      // Simulate progress (actual upload doesn't report progress in this simple version)
      const progressInterval = setInterval(() => {
        setProgress((prev) => Math.min(prev + 10, 90));
      }, 200);

      const result = await fileService.upload(file, entityType, entityID, subFolder);

      clearInterval(progressInterval);
      setProgress(100);
      
      onUploadSuccess(result.url, result.path);
      setPreview(result.url);
    } catch (err) {
      const error = err as Error;
      setError(error.message || 'Upload failed');
      onUploadError?.(error);
      setPreview(currentImageUrl || null);
    } finally {
      setUploading(false);
      setProgress(0);
    }
  };

  const handleDelete = () => {
    setPreview(null);
    onDelete?.();
  };

  return (
    <div style={{ width: '100%' }}>
      {/* Current Image Preview */}
      {preview && (
        <div style={{ marginBottom: '16px', position: 'relative' }}>
          <img
            src={preview}
            alt="Preview"
            style={{
              width: '100%',
              maxWidth: '300px',
              height: 'auto',
              borderRadius: '8px',
              border: '1px solid var(--border)'
            }}
          />
          {onDelete && !uploading && (
            <button
              type="button"
              onClick={handleDelete}
              style={{
                position: 'absolute',
                top: '8px',
                right: '8px',
                padding: '6px 12px',
                backgroundColor: 'var(--danger)',
                color: 'white',
                border: 'none',
                borderRadius: '6px',
                cursor: 'pointer',
                fontSize: '12px',
                fontWeight: '500'
              }}
            >
              <i className="ri-delete-bin-line" style={{ marginRight: '4px' }}></i>
              Remove
            </button>
          )}
        </div>
      )}

      {/* Upload Area */}
      <div
        style={{
          border: '2px dashed var(--border)',
          borderRadius: '8px',
          padding: '32px',
          textAlign: 'center',
          backgroundColor: uploading ? 'var(--bg-tertiary)' : 'var(--bg-secondary)',
          cursor: uploading ? 'not-allowed' : 'pointer',
          transition: 'all 0.2s',
          position: 'relative',
          overflow: 'hidden'
        }}
        onClick={() => !uploading && document.getElementById('file-input')?.click()}
        onMouseEnter={(e) => {
          if (!uploading) {
            e.currentTarget.style.borderColor = 'var(--primary)';
            e.currentTarget.style.backgroundColor = 'var(--primary-glow)';
          }
        }}
        onMouseLeave={(e) => {
          if (!uploading) {
            e.currentTarget.style.borderColor = 'var(--border)';
            e.currentTarget.style.backgroundColor = 'var(--bg-secondary)';
          }
        }}
      >
        {/* Progress Bar */}
        {uploading && progress > 0 && (
          <div
            style={{
              position: 'absolute',
              top: 0,
              left: 0,
              height: '4px',
              width: `${progress}%`,
              backgroundColor: 'var(--primary)',
              transition: 'width 0.3s'
            }}
          />
        )}

        <input
          id="file-input"
          type="file"
          accept={acceptedFormats}
          onChange={handleFileSelect}
          disabled={uploading}
          style={{ display: 'none' }}
        />

        {uploading ? (
          <>
            <div style={{ fontSize: '36px', marginBottom: '12px' }}>⏳</div>
            <div style={{ fontSize: '14px', color: 'var(--text-primary)', fontWeight: '500' }}>
              Uploading... {progress}%
            </div>
          </>
        ) : (
          <>
            <i
              className="ri-upload-cloud-line"
              style={{
                fontSize: '48px',
                color: 'var(--text-muted)',
                display: 'block',
                marginBottom: '12px'
              }}
            ></i>
            <div style={{ fontSize: '14px', color: 'var(--text-primary)', marginBottom: '8px' }}>
              <strong>Click to upload</strong> or drag and drop
            </div>
            <div style={{ fontSize: '12px', color: 'var(--text-muted)' }}>
              {acceptedFormats === 'image/*' ? 'JPG, PNG, GIF, WebP' : 'Supported formats'} (Max {maxSizeMB}MB)
            </div>
          </>
        )}
      </div>

      {/* Error Message */}
      {error && (
        <div
          style={{
            marginTop: '12px',
            padding: '12px',
            backgroundColor: 'var(--danger-glow)',
            border: '1px solid var(--danger)',
            borderRadius: '6px',
            color: 'var(--danger)',
            fontSize: '13px'
          }}
        >
          <i className="ri-error-warning-line" style={{ marginRight: '6px' }}></i>
          {error}
        </div>
      )}

      {/* Upload Path Info */}
      {preview && !uploading && (
        <div
          style={{
            marginTop: '8px',
            fontSize: '11px',
            color: 'var(--text-muted)',
            fontFamily: 'monospace'
          }}
        >
          Path: {entityType}/{entityID}/{subFolder}/
        </div>
      )}
    </div>
  );
}

/*
USAGE EXAMPLES:

1. Category Image Upload:
<ImageUpload
  entityType="categories"
  entityID={categoryId}
  subFolder="images"
  currentImageUrl={category.image_url}
  onUploadSuccess={(url, path) => {
    setCategory({ ...category, image_url: url, image_path: path });
  }}
  onDelete={() => {
    setCategory({ ...category, image_url: '', image_path: '' });
  }}
/>

2. Product Main Image:
<ImageUpload
  entityType="products"
  entityID={productSKU}
  subFolder="images"
  currentImageUrl={product.main_image}
  onUploadSuccess={(url, path) => {
    setProduct({ ...product, main_image: url });
  }}
/>

3. Product Files (PDF, etc.):
<ImageUpload
  entityType="products"
  entityID={productSKU}
  subFolder="files"
  acceptedFormats=".pdf,.doc,.docx"
  maxSizeMB={10}
  onUploadSuccess={(url, path) => {
    setProduct({ ...product, spec_sheet_url: url });
  }}
/>
*/

// ============================================================================
// CSVUploader — drag-and-drop CSV upload for keyword intelligence ingest
// ============================================================================

import React, { useState, useRef, useCallback } from 'react';
import { getActiveTenantId } from '../../contexts/TenantContext';

const API_BASE = (import.meta as any).env?.VITE_API_URL || 'http://localhost:8080/api/v1';

export interface CSVUploaderProps {
  label: string;
  sourceType: string;
  productId: string;
  onSuccess: () => void;
  onError: (message: string) => void;
  formatGuideUrl?: string;
}

type UploadState = 'idle' | 'uploading' | 'success' | 'error';

export function CSVUploader({ label, sourceType, productId, onSuccess, onError, formatGuideUrl }: CSVUploaderProps) {
  const [uploadState, setUploadState] = useState<UploadState>('idle');
  const [progress, setProgress] = useState(0);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);
  const [isDragOver, setIsDragOver] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const successTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const upload = useCallback(async (file: File) => {
    if (!file.name.toLowerCase().endsWith('.csv')) {
      setErrorMsg('Please upload a .csv file');
      setUploadState('error');
      onError('Invalid file type — .csv required');
      return;
    }

    setUploadState('uploading');
    setProgress(0);
    setErrorMsg(null);

    const formData = new FormData();
    formData.append('file', file);
    formData.append('source_type', sourceType);

    return new Promise<void>((resolve) => {
      const xhr = new XMLHttpRequest();

      xhr.upload.addEventListener('progress', (e) => {
        if (e.lengthComputable) {
          setProgress(Math.round((e.loaded / e.total) * 100));
        }
      });

      xhr.addEventListener('load', () => {
        if (xhr.status >= 200 && xhr.status < 300) {
          setUploadState('success');
          setProgress(100);
          onSuccess();
          if (successTimerRef.current) clearTimeout(successTimerRef.current);
          successTimerRef.current = setTimeout(() => {
            setUploadState('idle');
            setProgress(0);
          }, 3000);
        } else {
          let msg = 'Upload failed';
          try { msg = JSON.parse(xhr.responseText)?.error ?? msg; } catch { /* ignore */ }
          setUploadState('error');
          setErrorMsg(msg);
          onError(msg);
        }
        resolve();
      });

      xhr.addEventListener('error', () => {
        const msg = 'Network error — upload failed';
        setUploadState('error');
        setErrorMsg(msg);
        onError(msg);
        resolve();
      });

      xhr.open('POST', `${API_BASE}/products/${productId}/keyword-intelligence/ingest`);
      xhr.setRequestHeader('X-Tenant-Id', getActiveTenantId());
      xhr.send(formData);
    });
  }, [productId, sourceType, onSuccess, onError]);

  const handleDrop = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    setIsDragOver(false);
    const file = e.dataTransfer.files[0];
    if (file) upload(file);
  }, [upload]);

  const handleFileChange = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (file) upload(file);
    // reset so same file can be re-uploaded
    if (fileInputRef.current) fileInputRef.current.value = '';
  }, [upload]);

  const isUploading = uploadState === 'uploading';

  return (
    <div style={{ marginBottom: 16 }}>
      {/* Header row */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 8 }}>
        <span style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)' }}>{label}</span>
        {formatGuideUrl && (
          <a
            href={formatGuideUrl}
            target="_blank"
            rel="noopener noreferrer"
            style={{ fontSize: 11, color: 'var(--primary)', textDecoration: 'none' }}
          >
            Format guide ↗
          </a>
        )}
      </div>

      {/* Drop zone */}
      <div
        onDragOver={(e) => { e.preventDefault(); setIsDragOver(true); }}
        onDragLeave={() => setIsDragOver(false)}
        onDrop={handleDrop}
        onClick={() => !isUploading && fileInputRef.current?.click()}
        style={{
          border: `2px dashed ${isDragOver ? 'var(--primary)' : uploadState === 'success' ? 'var(--success)' : uploadState === 'error' ? 'var(--danger)' : 'var(--border-bright)'}`,
          borderRadius: 8,
          padding: '16px 14px',
          textAlign: 'center',
          cursor: isUploading ? 'not-allowed' : 'pointer',
          background: isDragOver ? 'var(--primary-glow, rgba(99,102,241,0.06))' : 'var(--bg-primary)',
          transition: 'border-color 0.15s, background 0.15s',
          position: 'relative',
          overflow: 'hidden',
        }}
      >
        {/* Progress bar underlay */}
        {isUploading && (
          <div style={{
            position: 'absolute', inset: 0, left: 0, top: 0,
            width: `${progress}%`, background: 'var(--primary-glow, rgba(99,102,241,0.1))',
            transition: 'width 0.2s',
          }} />
        )}

        <input
          ref={fileInputRef}
          type="file"
          accept=".csv"
          style={{ display: 'none' }}
          onChange={handleFileChange}
          disabled={isUploading}
        />

        {uploadState === 'idle' && (
          <>
            <div style={{ fontSize: 20, marginBottom: 4 }}>📄</div>
            <div style={{ fontSize: 12, color: 'var(--text-secondary)' }}>
              Drop CSV here or <span style={{ color: 'var(--primary)', fontWeight: 600 }}>browse</span>
            </div>
          </>
        )}

        {uploadState === 'uploading' && (
          <div style={{ position: 'relative', zIndex: 1 }}>
            <div style={{ fontSize: 12, color: 'var(--text-secondary)', marginBottom: 4 }}>
              Uploading… {progress}%
            </div>
            <div style={{ height: 3, background: 'var(--bg-tertiary)', borderRadius: 2, overflow: 'hidden', margin: '0 20px' }}>
              <div style={{ height: '100%', width: `${progress}%`, background: 'var(--primary)', transition: 'width 0.2s' }} />
            </div>
          </div>
        )}

        {uploadState === 'success' && (
          <div style={{ fontSize: 12, color: 'var(--success)', fontWeight: 600 }}>
            ✓ Keywords uploaded successfully
          </div>
        )}

        {uploadState === 'error' && (
          <div>
            <div style={{ fontSize: 12, color: 'var(--danger)', marginBottom: 4 }}>
              {errorMsg ?? 'Upload failed'}
            </div>
            <div style={{ fontSize: 11, color: 'var(--text-muted)' }}>Click to try again</div>
          </div>
        )}
      </div>
    </div>
  );
}

export default CSVUploader;

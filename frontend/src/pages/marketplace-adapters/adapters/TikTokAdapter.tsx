import React from 'react';
import { MarketplaceFormProps } from '../types';

export default function TikTokAdapter({ marketplaceData, onChange, onSync }: MarketplaceFormProps) {
  const updateField = (field: string, value: any) => {
    onChange({ ...marketplaceData, [field]: value });
  };

  return (
    <div className="space-y-4">
      <button
        onClick={onSync}
        className="w-full btn btn-secondary flex items-center justify-center gap-2"
      >
        <i className="ri-refresh-line"></i>
        Sync from Core Product Data
      </button>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
          Title <span className="text-red-500">*</span>
        </label>
        <input
          type="text"
          value={marketplaceData.title || ''}
          onChange={(e) => updateField('title', e.target.value)}
          maxLength={255}
          placeholder="Product title (max 255 chars)"
          className="input w-full"
        />
        <p className="text-xs text-[var(--text-muted)] mt-1">
          {(marketplaceData.title || '').length}/255 characters
        </p>
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
          Price (GBP) <span className="text-red-500">*</span>
        </label>
        <input
          type="number"
          step="0.01"
          min="0.01"
          value={marketplaceData.price || ''}
          onChange={(e) => updateField('price', parseFloat(e.target.value) || 0)}
          className="input w-full"
          placeholder="0.00"
        />
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
          Stock Quantity <span className="text-red-500">*</span>
        </label>
        <input
          type="number"
          min="0"
          value={marketplaceData.quantity || 0}
          onChange={(e) => updateField('quantity', parseInt(e.target.value) || 0)}
          className="input w-full"
        />
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
          Category ID <span className="text-red-500">*</span>
        </label>
        <input
          type="number"
          value={marketplaceData.category_id || ''}
          onChange={(e) => updateField('category_id', parseInt(e.target.value) || 0)}
          className="input w-full"
          placeholder="TikTok category ID (leaf)"
        />
        <p className="text-xs text-[var(--text-muted)] mt-1">
          Use the full listing page to browse categories
        </p>
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">Brand</label>
        <input
          type="text"
          value={marketplaceData.brand || ''}
          onChange={(e) => updateField('brand', e.target.value)}
          className="input w-full"
          placeholder="Brand name (optional)"
        />
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
          Description
        </label>
        <textarea
          rows={4}
          value={marketplaceData.description || ''}
          onChange={(e) => updateField('description', e.target.value)}
          className="input w-full resize-none"
          placeholder="Product description"
        />
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
          Video URL
        </label>
        <input
          type="url"
          value={marketplaceData.video_url || ''}
          onChange={(e) => updateField('video_url', e.target.value)}
          className="input w-full"
          placeholder="TikTok product video URL (optional)"
        />
      </div>

      <div className="bg-[var(--bg-secondary)] rounded-lg p-3 border border-[var(--border)]">
        <p className="text-xs text-[var(--text-muted)] flex items-start gap-2">
          <i className="ri-information-line mt-0.5 flex-shrink-0 text-[#00f2ea]"></i>
          <span>
            For full listing control (category browsing, attribute mapping, multi-variant SKUs, image
            uploads) use the{' '}
            <strong>Create TikTok Listing</strong> page from the Marketplace menu.
          </span>
        </p>
      </div>
    </div>
  );
}

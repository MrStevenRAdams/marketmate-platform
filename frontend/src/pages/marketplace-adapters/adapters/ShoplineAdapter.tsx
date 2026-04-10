import React from 'react';
import { MarketplaceFormProps } from '../types';

export default function ShoplineAdapter({ coreData, marketplaceData, onChange, onSync }: MarketplaceFormProps) {
  const updateField = (field: string, value: any) => {
    onChange({ ...marketplaceData, [field]: value });
  };

  return (
    <div className="space-y-4">
      <button onClick={onSync} className="w-full btn btn-secondary flex items-center justify-center gap-2">
        <i className="ri-refresh-line"></i>
        Sync from Core Product Data
      </button>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-2">
          Title *
        </label>
        <input
          type="text"
          value={marketplaceData.title || ''}
          onChange={(e) => updateField('title', e.target.value)}
          className="input w-full"
        />
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-2">
          Product Type
        </label>
        <input
          type="text"
          value={marketplaceData.productType || ''}
          onChange={(e) => updateField('productType', e.target.value)}
          className="input w-full"
          placeholder="e.g. T-Shirts, Electronics"
        />
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-2">
          Vendor (Brand)
        </label>
        <input
          type="text"
          value={marketplaceData.vendor || ''}
          onChange={(e) => updateField('vendor', e.target.value)}
          className="input w-full"
        />
      </div>

      <div className="grid grid-cols-2 gap-3">
        <div>
          <label className="block text-sm font-medium text-[var(--text-primary)] mb-2">
            Price *
          </label>
          <input
            type="number"
            step="0.01"
            value={marketplaceData.price || ''}
            onChange={(e) => updateField('price', parseFloat(e.target.value) || 0)}
            className="input w-full"
          />
        </div>
        <div>
          <label className="block text-sm font-medium text-[var(--text-primary)] mb-2">
            Compare at Price
          </label>
          <input
            type="number"
            step="0.01"
            value={marketplaceData.compareAtPrice || ''}
            onChange={(e) => updateField('compareAtPrice', parseFloat(e.target.value) || 0)}
            className="input w-full"
          />
        </div>
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-2">
          SKU *
        </label>
        <input
          type="text"
          value={marketplaceData.sku || ''}
          onChange={(e) => updateField('sku', e.target.value)}
          className="input w-full"
        />
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-2">
          Inventory Quantity
        </label>
        <input
          type="number"
          value={marketplaceData.quantity || ''}
          onChange={(e) => updateField('quantity', parseInt(e.target.value) || 0)}
          className="input w-full"
        />
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-2">
          Tags <span className="text-[var(--text-muted)] font-normal">(comma-separated)</span>
        </label>
        <input
          type="text"
          value={marketplaceData.tags || ''}
          onChange={(e) => updateField('tags', e.target.value)}
          className="input w-full"
          placeholder="tag1, tag2, tag3"
        />
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-2">
          Status
        </label>
        <select
          value={marketplaceData.status || 'draft'}
          onChange={(e) => updateField('status', e.target.value)}
          className="input w-full"
        >
          <option value="active">Active (published)</option>
          <option value="draft">Draft</option>
          <option value="archived">Archived</option>
        </select>
      </div>

      <div className="grid grid-cols-2 gap-3">
        <div>
          <label className="block text-sm font-medium text-[var(--text-primary)] mb-2">
            Weight Value
          </label>
          <input
            type="number"
            step="0.001"
            value={marketplaceData.weightValue || ''}
            onChange={(e) => updateField('weightValue', e.target.value)}
            className="input w-full"
            placeholder="0.000"
          />
        </div>
        <div>
          <label className="block text-sm font-medium text-[var(--text-primary)] mb-2">
            Weight Unit
          </label>
          <select
            value={marketplaceData.weightUnit || 'kg'}
            onChange={(e) => updateField('weightUnit', e.target.value)}
            className="input w-full"
          >
            <option value="kg">kg</option>
            <option value="g">g</option>
            <option value="lb">lb</option>
            <option value="oz">oz</option>
          </select>
        </div>
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-2">
          SEO Title
        </label>
        <input
          type="text"
          value={marketplaceData.seoTitle || ''}
          onChange={(e) => updateField('seoTitle', e.target.value)}
          className="input w-full"
          placeholder="Defaults to product title"
          maxLength={70}
        />
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-2">
          SEO Description
        </label>
        <textarea
          value={marketplaceData.seoDescription || ''}
          onChange={(e) => updateField('seoDescription', e.target.value)}
          className="input w-full min-h-[80px]"
          placeholder="Meta description for search engines"
          maxLength={320}
        />
      </div>
    </div>
  );
}

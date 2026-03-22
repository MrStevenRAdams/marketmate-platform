import React from 'react';
import { MarketplaceFormProps } from '../types';

export default function EbayAdapter({ coreData, marketplaceData, onChange, onSync }: MarketplaceFormProps) {
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
          eBay Title * <span className="text-xs text-[var(--text-muted)]">(Max 80 chars)</span>
        </label>
        <input
          type="text"
          value={marketplaceData.title || ''}
          onChange={(e) => updateField('title', e.target.value)}
          className="input w-full"
          maxLength={80}
        />
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-2">
          Subtitle <span className="text-xs text-[var(--text-muted)]">(Max 55 chars)</span>
        </label>
        <input
          type="text"
          value={marketplaceData.subtitle || ''}
          onChange={(e) => updateField('subtitle', e.target.value)}
          className="input w-full"
          maxLength={55}
        />
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-2">
          Category ID *
        </label>
        <input
          type="text"
          value={marketplaceData.categoryId || ''}
          onChange={(e) => updateField('categoryId', e.target.value)}
          className="input w-full"
          placeholder="eBay category ID"
        />
      </div>

      <div className="grid grid-cols-2 gap-3">
        <div>
          <label className="block text-sm font-medium text-[var(--text-primary)] mb-2">
            Format
          </label>
          <select
            value={marketplaceData.format || 'FixedPrice'}
            onChange={(e) => updateField('format', e.target.value)}
            className="select w-full"
          >
            <option value="FixedPrice">Fixed Price</option>
            <option value="Auction">Auction</option>
          </select>
        </div>
        <div>
          <label className="block text-sm font-medium text-[var(--text-primary)] mb-2">
            Price * ($)
          </label>
          <input
            type="number"
            step="0.01"
            value={marketplaceData.price || ''}
            onChange={(e) => updateField('price', parseFloat(e.target.value) || 0)}
            className="input w-full"
          />
        </div>
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-2">
          Quantity *
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
          Condition
        </label>
        <select
          value={marketplaceData.condition || 'New'}
          onChange={(e) => updateField('condition', e.target.value)}
          className="select w-full"
        >
          <option value="New">New</option>
          <option value="New with tags">New with tags</option>
          <option value="New without tags">New without tags</option>
          <option value="Used">Used</option>
          <option value="Refurbished">Refurbished</option>
        </select>
      </div>
    </div>
  );
}

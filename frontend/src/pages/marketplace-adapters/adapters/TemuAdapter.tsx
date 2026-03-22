import React from 'react';
import { MarketplaceFormProps } from '../types';

export default function TemuAdapter({ marketplaceData, onChange, onSync }: MarketplaceFormProps) {
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
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-2">Title *</label>
        <input
          type="text"
          value={marketplaceData.title || ''}
          onChange={(e) => updateField('title', e.target.value)}
          className="input w-full"
        />
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-2">Price *</label>
        <input
          type="number"
          step="0.01"
          value={marketplaceData.price || ''}
          onChange={(e) => updateField('price', parseFloat(e.target.value) || 0)}
          className="input w-full"
        />
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-2">Category</label>
        <input
          type="text"
          value={marketplaceData.category || ''}
          onChange={(e) => updateField('category', e.target.value)}
          className="input w-full"
        />
      </div>
    </div>
  );
}

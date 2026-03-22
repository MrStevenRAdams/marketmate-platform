import React from 'react';
import { MarketplaceFormProps } from '../types';

export default function TescoAdapter({ marketplaceData, onChange, onSync }: MarketplaceFormProps) {
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
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-2">GTIN *</label>
        <input
          type="text"
          value={marketplaceData.gtin || ''}
          onChange={(e) => updateField('gtin', e.target.value)}
          className="input w-full"
          placeholder="Global Trade Item Number"
        />
      </div>

      <div className="grid grid-cols-2 gap-3">
        <div>
          <label className="block text-sm font-medium text-[var(--text-primary)] mb-2">Price (£) *</label>
          <input
            type="number"
            step="0.01"
            value={marketplaceData.price || ''}
            onChange={(e) => updateField('price', parseFloat(e.target.value) || 0)}
            className="input w-full"
          />
        </div>
        <div>
          <label className="block text-sm font-medium text-[var(--text-primary)] mb-2">VAT (%)</label>
          <input
            type="number"
            value={marketplaceData.vat || 20}
            onChange={(e) => updateField('vat', parseFloat(e.target.value) || 20)}
            className="input w-full"
          />
        </div>
      </div>
    </div>
  );
}

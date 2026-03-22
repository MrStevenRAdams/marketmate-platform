import React from 'react';
import { MarketplaceFormProps } from '../types';

export default function ShopifyAdapter({ coreData, marketplaceData, onChange, onSync }: MarketplaceFormProps) {
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
          placeholder="e.g., T-Shirts, Electronics"
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
          SKU
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
          value={marketplaceData.inventory || ''}
          onChange={(e) => updateField('inventory', parseInt(e.target.value) || 0)}
          className="input w-full"
        />
      </div>
    </div>
  );
}

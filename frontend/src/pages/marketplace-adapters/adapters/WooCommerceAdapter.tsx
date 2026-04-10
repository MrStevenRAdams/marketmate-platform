import { MarketplaceFormProps } from '../types';

export default function WooCommerceAdapter({ marketplaceData, onChange, onSync }: MarketplaceFormProps) {
  const updateField = (field: string, value: unknown) => {
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
          Product Name <span className="text-red-500">*</span>
        </label>
        <input
          type="text"
          value={marketplaceData.name || ''}
          onChange={(e) => updateField('name', e.target.value)}
          placeholder="WooCommerce product name"
          className="input w-full"
        />
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
          Regular Price <span className="text-red-500">*</span>
        </label>
        <div className="relative">
          <span className="absolute left-3 top-1/2 -translate-y-1/2 text-[var(--text-muted)]">£</span>
          <input
            type="number"
            step="0.01"
            min="0"
            value={marketplaceData.regular_price || ''}
            onChange={(e) => updateField('regular_price', e.target.value)}
            className="input w-full pl-7"
            placeholder="0.00"
          />
        </div>
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
          Sale Price
        </label>
        <div className="relative">
          <span className="absolute left-3 top-1/2 -translate-y-1/2 text-[var(--text-muted)]">£</span>
          <input
            type="number"
            step="0.01"
            min="0"
            value={marketplaceData.sale_price || ''}
            onChange={(e) => updateField('sale_price', e.target.value)}
            className="input w-full pl-7"
            placeholder="Optional"
          />
        </div>
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
          Stock Quantity
        </label>
        <input
          type="number"
          min="0"
          value={marketplaceData.stock_quantity ?? 0}
          onChange={(e) => updateField('stock_quantity', parseInt(e.target.value) || 0)}
          className="input w-full"
        />
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
          SKU
        </label>
        <input
          type="text"
          value={marketplaceData.sku || ''}
          onChange={(e) => updateField('sku', e.target.value)}
          placeholder="Stock keeping unit"
          className="input w-full"
        />
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
          Short Description
        </label>
        <textarea
          value={marketplaceData.short_description || ''}
          onChange={(e) => updateField('short_description', e.target.value)}
          rows={3}
          className="input w-full resize-none text-sm"
          placeholder="Short excerpt for product listings…"
        />
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
          Status
        </label>
        <select
          className="input w-full"
          value={marketplaceData.status || 'publish'}
          onChange={(e) => updateField('status', e.target.value)}
        >
          <option value="publish">Published</option>
          <option value="draft">Draft</option>
          <option value="private">Private</option>
        </select>
      </div>
    </div>
  );
}

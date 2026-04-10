import { MarketplaceFormProps } from '../types';

export default function WalmartAdapter({ marketplaceData, onChange, onSync }: MarketplaceFormProps) {
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
          value={marketplaceData.productName || marketplaceData.product_name || ''}
          onChange={(e) => {
            updateField('productName', e.target.value);
            updateField('product_name', e.target.value);
          }}
          placeholder="Walmart listing title"
          className="input w-full"
          maxLength={200}
        />
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
          Price (USD) <span className="text-red-500">*</span>
        </label>
        <div className="relative">
          <span className="absolute left-3 top-1/2 -translate-y-1/2 text-[var(--text-muted)]">$</span>
          <input
            type="number"
            step="0.01"
            min="0.01"
            value={marketplaceData.price || ''}
            onChange={(e) => updateField('price', e.target.value)}
            className="input w-full pl-7"
            placeholder="0.00"
          />
        </div>
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
          Quantity
        </label>
        <input
          type="number"
          min="0"
          value={marketplaceData.quantity ?? 0}
          onChange={(e) => updateField('quantity', parseInt(e.target.value) || 0)}
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
          placeholder="Your unique SKU"
          className="input w-full"
        />
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
          UPC / GTIN
        </label>
        <input
          type="text"
          value={marketplaceData.upc || ''}
          onChange={(e) => updateField('upc', e.target.value)}
          placeholder="12-digit UPC barcode"
          className="input w-full"
        />
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
          Brand
        </label>
        <input
          type="text"
          value={marketplaceData.brand || ''}
          onChange={(e) => updateField('brand', e.target.value)}
          placeholder="Brand name"
          className="input w-full"
        />
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
          Short Description
        </label>
        <textarea
          value={marketplaceData.shortDescription || marketplaceData.short_description || ''}
          onChange={(e) => {
            updateField('shortDescription', e.target.value);
            updateField('short_description', e.target.value);
          }}
          rows={3}
          className="input w-full resize-none text-sm"
          placeholder="Brief product summary…"
        />
      </div>

      <div className="text-xs text-[var(--text-muted)] p-2 rounded" style={{ background: 'var(--bg-secondary)' }}>
        <i className="ri-information-line mr-1" />
        Walmart listings are submitted via feeds. Use the full Create Listing page for initial submission.
        Changes here queue a feed update when saved.
      </div>
    </div>
  );
}

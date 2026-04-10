import { MarketplaceFormProps } from '../types';

export default function OnBuyAdapter({ marketplaceData, onChange, onSync }: MarketplaceFormProps) {
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
          OPC (OnBuy Product Code) <span className="text-red-500">*</span>
        </label>
        <input
          type="text"
          value={marketplaceData.opc || ''}
          onChange={(e) => updateField('opc', e.target.value)}
          placeholder="e.g. ABCD-1234"
          className="input w-full"
        />
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">SKU</label>
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
          Price <span className="text-red-500">*</span>
        </label>
        <div className="relative">
          <span className="absolute left-3 top-1/2 -translate-y-1/2 text-[var(--text-muted)]">£</span>
          <input
            type="number"
            step="0.01"
            min="0"
            value={marketplaceData.price || ''}
            onChange={(e) => updateField('price', parseFloat(e.target.value) || 0)}
            className="input w-full pl-7"
            placeholder="0.00"
          />
        </div>
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">Stock</label>
        <input
          type="number"
          min="0"
          value={marketplaceData.stock ?? 0}
          onChange={(e) => updateField('stock', parseInt(e.target.value) || 0)}
          className="input w-full"
        />
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">Condition</label>
        <select
          className="input w-full"
          value={marketplaceData.condition_id || 'new'}
          onChange={(e) => updateField('condition_id', e.target.value)}
        >
          <option value="new">New</option>
          <option value="used_like_new">Used – Like New</option>
          <option value="used_very_good">Used – Very Good</option>
          <option value="used_good">Used – Good</option>
          <option value="used_acceptable">Used – Acceptable</option>
          <option value="refurbished">Refurbished</option>
        </select>
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
          Delivery Template ID
        </label>
        <input
          type="number"
          min="0"
          value={marketplaceData.delivery_template_id || ''}
          onChange={(e) => updateField('delivery_template_id', parseInt(e.target.value) || 0)}
          className="input w-full"
          placeholder="0"
        />
      </div>
    </div>
  );
}

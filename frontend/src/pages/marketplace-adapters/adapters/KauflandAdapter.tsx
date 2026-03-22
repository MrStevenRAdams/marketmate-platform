import { MarketplaceFormProps } from '../types';

export default function KauflandAdapter({ marketplaceData, onChange, onSync }: MarketplaceFormProps) {
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
          EAN / GTIN <span className="text-red-500">*</span>
        </label>
        <input
          type="text"
          value={marketplaceData.ean || ''}
          onChange={(e) => updateField('ean', e.target.value)}
          placeholder="e.g. 4006381333931"
          className="input w-full font-mono"
          maxLength={14}
        />
        <p className="text-xs text-[var(--text-muted)] mt-1">
          Required — Kaufland identifies products by EAN barcode
        </p>
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
          Listing Price (€) <span className="text-red-500">*</span>
        </label>
        <div className="relative">
          <span className="absolute left-3 top-1/2 -translate-y-1/2 text-[var(--text-muted)]">€</span>
          <input
            type="number"
            step="0.01"
            min="0.01"
            value={marketplaceData.listing_price || ''}
            onChange={(e) => updateField('listing_price', e.target.value)}
            className="input w-full pl-7"
            placeholder="0.00"
          />
        </div>
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
          Minimum Price (€)
        </label>
        <div className="relative">
          <span className="absolute left-3 top-1/2 -translate-y-1/2 text-[var(--text-muted)]">€</span>
          <input
            type="number"
            step="0.01"
            min="0"
            value={marketplaceData.minimum_price || ''}
            onChange={(e) => updateField('minimum_price', e.target.value)}
            className="input w-full pl-7"
            placeholder="Optional"
          />
        </div>
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
          Stock Amount
        </label>
        <input
          type="number"
          min="0"
          value={marketplaceData.amount ?? 0}
          onChange={(e) => updateField('amount', parseInt(e.target.value) || 0)}
          className="input w-full"
        />
        <p className="text-xs text-[var(--text-muted)] mt-1">
          Set to 0 to deactivate this unit
        </p>
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
          Condition
        </label>
        <select
          value={marketplaceData.condition || 1}
          onChange={(e) => updateField('condition', parseInt(e.target.value))}
          className="input w-full"
        >
          <option value={1}>New</option>
          <option value={2}>Like New</option>
          <option value={3}>Very Good</option>
          <option value={4}>Good</option>
          <option value={5}>Acceptable</option>
        </select>
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
          Seller Note
        </label>
        <textarea
          value={marketplaceData.note || ''}
          onChange={(e) => updateField('note', e.target.value)}
          rows={3}
          className="input w-full resize-none text-sm"
          placeholder="Optional condition details visible to buyers…"
          maxLength={500}
        />
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
          Handling Time (days)
        </label>
        <input
          type="number"
          min="1"
          max="30"
          value={marketplaceData.handling_time_in_days ?? 1}
          onChange={(e) => updateField('handling_time_in_days', parseInt(e.target.value) || 1)}
          className="input w-full"
        />
      </div>
    </div>
  );
}

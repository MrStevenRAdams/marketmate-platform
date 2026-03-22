import { MarketplaceFormProps } from '../types';

export default function BigCommerceAdapter({ marketplaceData, onChange, onSync }: MarketplaceFormProps) {
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
          placeholder="BigCommerce product name"
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
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
          Inventory Level
        </label>
        <input
          type="number"
          min="0"
          value={marketplaceData.inventory_level ?? 0}
          onChange={(e) => updateField('inventory_level', parseInt(e.target.value) || 0)}
          className="input w-full"
        />
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
          Weight (kg)
        </label>
        <input
          type="number"
          step="0.001"
          min="0"
          value={marketplaceData.weight || ''}
          onChange={(e) => updateField('weight', parseFloat(e.target.value) || 0)}
          className="input w-full"
          placeholder="0.000"
        />
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
          Availability
        </label>
        <select
          className="input w-full"
          value={marketplaceData.availability || 'available'}
          onChange={(e) => updateField('availability', e.target.value)}
        >
          <option value="available">Available</option>
          <option value="disabled">Disabled</option>
          <option value="preorder">Pre-order</option>
        </select>
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
          Visibility
        </label>
        <select
          className="input w-full"
          value={marketplaceData.is_visible === false ? 'false' : 'true'}
          onChange={(e) => updateField('is_visible', e.target.value === 'true')}
        >
          <option value="true">Visible</option>
          <option value="false">Hidden</option>
        </select>
      </div>
    </div>
  );
}

import React, { useState } from 'react';
import { Variant, VariantField } from './types';

interface Props {
  variants: Variant[];
  parentTitle: string;
  parentWeight: number;
  parentDimensions: { width: number; height: number; length: number };
  onChange: (variants: Variant[]) => void;
}

export default function VariantTableSection({ 
  variants, 
  parentTitle,
  parentWeight,
  parentDimensions,
  onChange 
}: Props) {
  
  const [expandedVariantId, setExpandedVariantId] = useState<string | null>(null);

  const toggleExpand = (variantId: string) => {
    setExpandedVariantId(expandedVariantId === variantId ? null : variantId);
  };

  const updateVariant = (index: number, updates: Partial<Variant>) => {
    const newVariants = [...variants];
    newVariants[index] = { ...newVariants[index], ...updates };
    onChange(newVariants);
  };

  const updateVariantField = <T,>(
    index: number,
    field: keyof Variant,
    value: T,
    inheritFromParent: boolean
  ) => {
    const newVariants = [...variants];
    (newVariants[index][field] as VariantField<T>) = {
      value,
      inheritFromParent
    };
    onChange(newVariants);
  };

  const getVariantName = (variant: Variant) => {
    return Object.entries(variant.optionCombination)
      .map(([key, value]) => `${key}: ${value}`)
      .join(' / ');
  };

  const toggleStatus = (index: number) => {
    updateVariant(index, {
      status: variants[index].status === 'active' ? 'inactive' : 'active'
    });
  };

  if (variants.length === 0) {
    return (
      <div className="card">
        <div className="card-header">
          <h2 style={{ fontSize: '18px', fontWeight: 600 }}>Variants</h2>
        </div>
        <div style={{ padding: 'var(--spacing-xl)', textAlign: 'center' }}>
          <i className="ri-stack-line text-4xl text-[var(--text-muted)] mb-3"></i>
          <p className="text-[var(--text-muted)]">
            No variants generated yet. Add product options and click "Generate Variants".
          </p>
        </div>
      </div>
    );
  }

  return (
    <div className="card">
      <div className="card-header">
        <div>
          <h2 style={{ fontSize: '18px', fontWeight: 600 }}>Variants ({variants.length})</h2>
          <p style={{ fontSize: '13px', color: 'var(--text-muted)', marginTop: '4px' }}>
            Each variant can inherit or customize values
          </p>
        </div>
      </div>
      <div style={{ padding: 'var(--spacing-xl)' }}>
        <div className="space-y-2">
          {variants.map((variant, index) => (
            <div 
              key={variant.id}
              className="border border-[var(--border)] rounded-lg overflow-hidden bg-[var(--bg-tertiary)]"
            >
              {/* Variant Row */}
              <div className="p-3 flex items-center gap-3">
                <button
                  type="button"
                  onClick={() => toggleExpand(variant.id)}
                  className="text-[var(--text-muted)] hover:text-[var(--text-primary)] transition-colors cursor-pointer"
                >
                  <i className={`ri-arrow-${expandedVariantId === variant.id ? 'down' : 'right'}-s-line text-lg`}></i>
                </button>

                <div className="flex-1 grid grid-cols-6 gap-3 items-center">
                  <div className="col-span-2">
                    <div className="font-medium text-sm text-[var(--text-primary)]">
                      {getVariantName(variant)}
                    </div>
                    <div className="text-xs text-[var(--text-muted)] mt-0.5">
                      {variant.sku.value}
                    </div>
                  </div>

                  <div className="text-sm text-[var(--text-primary)]">
                    £{variant.price.inheritFromParent ? '0.00' : variant.price.value.toFixed(2)}
                    {variant.price.inheritFromParent && (
                      <span className="text-xs text-[var(--text-muted)] ml-1">(inherited)</span>
                    )}
                  </div>

                  <div className="text-sm text-[var(--text-primary)]">
                    {variant.stock.inheritFromParent ? 'N/A' : variant.stock.value}
                    {variant.stock.inheritFromParent && (
                      <span className="text-xs text-[var(--text-muted)] ml-1">(inherited)</span>
                    )}
                  </div>

                  <div className="text-sm text-[var(--text-primary)]">
                    {variant.weight.inheritFromParent 
                      ? parentWeight || 'N/A'
                      : variant.weight.value} lbs
                  </div>

                  <div className="flex items-center gap-2 justify-end">
                    <button
                      type="button"
                      onClick={() => toggleStatus(index)}
                      className={`px-3 py-1 rounded-full text-xs font-medium cursor-pointer ${
                        variant.status === 'active'
                          ? 'bg-[var(--success)]/20 text-[var(--success)]'
                          : 'bg-[var(--text-muted)]/20 text-[var(--text-muted)]'
                      }`}
                    >
                      {variant.status === 'active' ? 'Active' : 'Inactive'}
                    </button>
                  </div>
                </div>
              </div>

              {/* Expanded Detail Panel */}
              {expandedVariantId === variant.id && (
                <div className="border-t border-[var(--border)] p-4 bg-[var(--bg-secondary)]">
                  <div className="grid grid-cols-2 gap-4">
                    {/* SKU - Always unique */}
                    <div>
                      <label className="block text-xs font-medium text-[var(--text-primary)] mb-2">
                        SKU <span className="text-[var(--danger)]">*</span>
                      </label>
                      <input
                        type="text"
                        value={variant.sku.value}
                        onChange={(e) => updateVariantField(index, 'sku', e.target.value, false)}
                        className="input w-full text-sm"
                        placeholder="Enter unique SKU"
                      />
                    </div>

                    {/* Title with inheritance */}
                    <div>
                      <label className="flex items-center justify-between text-xs font-medium text-[var(--text-primary)] mb-2">
                        <span>Title</span>
                        <label className="flex items-center gap-1.5 cursor-pointer text-[var(--text-muted)] font-normal">
                          <input
                            type="checkbox"
                            checked={variant.title.inheritFromParent}
                            onChange={(e) => updateVariantField(
                              index, 
                              'title', 
                              e.target.checked ? parentTitle : variant.title.value,
                              e.target.checked
                            )}
                            className="cursor-pointer"
                          />
                          <span className="text-xs">Same as parent</span>
                        </label>
                      </label>
                      <input
                        type="text"
                        value={variant.title.inheritFromParent ? parentTitle : variant.title.value}
                        onChange={(e) => updateVariantField(index, 'title', e.target.value, false)}
                        disabled={variant.title.inheritFromParent}
                        className="input w-full text-sm"
                        placeholder="Variant title"
                      />
                    </div>

                    {/* Price */}
                    <div>
                      <label className="flex items-center justify-between text-xs font-medium text-[var(--text-primary)] mb-2">
                        <span>Price (£)</span>
                        <label className="flex items-center gap-1.5 cursor-pointer text-[var(--text-muted)] font-normal">
                          <input
                            type="checkbox"
                            checked={variant.price.inheritFromParent}
                            onChange={(e) => updateVariantField(
                              index,
                              'price',
                              e.target.checked ? 0 : variant.price.value,
                              e.target.checked
                            )}
                            className="cursor-pointer"
                          />
                          <span className="text-xs">Same as parent</span>
                        </label>
                      </label>
                      <input
                        type="number"
                        step="0.01"
                        value={variant.price.value}
                        onChange={(e) => updateVariantField(index, 'price', parseFloat(e.target.value) || 0, false)}
                        disabled={variant.price.inheritFromParent}
                        className="input w-full text-sm"
                        placeholder="0.00"
                      />
                    </div>

                    {/* Stock */}
                    <div>
                      <label className="flex items-center justify-between text-xs font-medium text-[var(--text-primary)] mb-2">
                        <span>Stock Quantity</span>
                        <label className="flex items-center gap-1.5 cursor-pointer text-[var(--text-muted)] font-normal">
                          <input
                            type="checkbox"
                            checked={variant.stock.inheritFromParent}
                            onChange={(e) => updateVariantField(
                              index,
                              'stock',
                              e.target.checked ? 0 : variant.stock.value,
                              e.target.checked
                            )}
                            className="cursor-pointer"
                          />
                          <span className="text-xs">Same as parent</span>
                        </label>
                      </label>
                      <input
                        type="number"
                        value={variant.stock.value}
                        onChange={(e) => updateVariantField(index, 'stock', parseInt(e.target.value) || 0, false)}
                        disabled={variant.stock.inheritFromParent}
                        className="input w-full text-sm"
                        placeholder="0"
                      />
                    </div>

                    {/* Weight */}
                    <div>
                      <label className="flex items-center justify-between text-xs font-medium text-[var(--text-primary)] mb-2">
                        <span>Weight (lbs)</span>
                        <label className="flex items-center gap-1.5 cursor-pointer text-[var(--text-muted)] font-normal">
                          <input
                            type="checkbox"
                            checked={variant.weight.inheritFromParent}
                            onChange={(e) => updateVariantField(
                              index,
                              'weight',
                              e.target.checked ? parentWeight : variant.weight.value,
                              e.target.checked
                            )}
                            className="cursor-pointer"
                          />
                          <span className="text-xs">Same as parent</span>
                        </label>
                      </label>
                      <input
                        type="number"
                        step="0.01"
                        value={variant.weight.inheritFromParent ? parentWeight : variant.weight.value}
                        onChange={(e) => updateVariantField(index, 'weight', parseFloat(e.target.value) || 0, false)}
                        disabled={variant.weight.inheritFromParent}
                        className="input w-full text-sm"
                        placeholder="0.00"
                      />
                    </div>

                    {/* Dimensions */}
                    <div>
                      <label className="flex items-center justify-between text-xs font-medium text-[var(--text-primary)] mb-2">
                        <span>Dimensions (W × H × L)</span>
                        <label className="flex items-center gap-1.5 cursor-pointer text-[var(--text-muted)] font-normal">
                          <input
                            type="checkbox"
                            checked={variant.dimensions.inheritFromParent}
                            onChange={(e) => updateVariantField(
                              index,
                              'dimensions',
                              e.target.checked ? parentDimensions : variant.dimensions.value,
                              e.target.checked
                            )}
                            className="cursor-pointer"
                          />
                          <span className="text-xs">Same as parent</span>
                        </label>
                      </label>
                      <div className="grid grid-cols-3 gap-1">
                        <input
                          type="number"
                          step="0.01"
                          value={variant.dimensions.inheritFromParent ? parentDimensions.width : variant.dimensions.value.width}
                          onChange={(e) => updateVariantField(
                            index,
                            'dimensions',
                            { ...variant.dimensions.value, width: parseFloat(e.target.value) || 0 },
                            false
                          )}
                          disabled={variant.dimensions.inheritFromParent}
                          className="input w-full text-sm"
                          placeholder="W"
                        />
                        <input
                          type="number"
                          step="0.01"
                          value={variant.dimensions.inheritFromParent ? parentDimensions.height : variant.dimensions.value.height}
                          onChange={(e) => updateVariantField(
                            index,
                            'dimensions',
                            { ...variant.dimensions.value, height: parseFloat(e.target.value) || 0 },
                            false
                          )}
                          disabled={variant.dimensions.inheritFromParent}
                          className="input w-full text-sm"
                          placeholder="H"
                        />
                        <input
                          type="number"
                          step="0.01"
                          value={variant.dimensions.inheritFromParent ? parentDimensions.length : variant.dimensions.value.length}
                          onChange={(e) => updateVariantField(
                            index,
                            'dimensions',
                            { ...variant.dimensions.value, length: parseFloat(e.target.value) || 0 },
                            false
                          )}
                          disabled={variant.dimensions.inheritFromParent}
                          className="input w-full text-sm"
                          placeholder="L"
                        />
                      </div>
                    </div>
                  </div>

                  <div className="mt-3 pt-3 border-t border-[var(--border)] flex items-center justify-between">
                    <div className="text-xs text-[var(--text-muted)]">
                      <i className="ri-information-line mr-1"></i>
                      Inherited fields will use the parent product's value
                    </div>
                    <button
                      type="button"
                      onClick={() => toggleStatus(index)}
                      className="btn btn-secondary btn-sm"
                    >
                      {variant.status === 'active' ? 'Deactivate' : 'Activate'}
                    </button>
                  </div>
                </div>
              )}
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

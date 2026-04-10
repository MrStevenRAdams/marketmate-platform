import React from 'react';
import { MarketplaceFormProps } from '../types';

export default function EtsyAdapter({ marketplaceData, onChange, onSync }: MarketplaceFormProps) {
  const updateField = (field: string, value: any) => {
    onChange({ ...marketplaceData, [field]: value });
  };

  const tags: string[] = marketplaceData.tags || [];
  const [tagInput, setTagInput] = React.useState('');

  function addTag() {
    const t = tagInput.trim().toLowerCase();
    if (!t || tags.includes(t) || tags.length >= 13) return;
    updateField('tags', [...tags, t]);
    setTagInput('');
  }

  function removeTag(tag: string) {
    updateField('tags', tags.filter((t: string) => t !== tag));
  }

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
          Title <span className="text-red-500">*</span>
        </label>
        <input
          type="text"
          value={marketplaceData.title || ''}
          onChange={(e) => updateField('title', e.target.value)}
          maxLength={140}
          placeholder="Etsy listing title (max 140 chars)"
          className="input w-full"
        />
        <p className="text-xs text-[var(--text-muted)] mt-1">
          {(marketplaceData.title || '').length}/140 characters
        </p>
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
            onChange={(e) => updateField('price', parseFloat(e.target.value) || 0)}
            className="input w-full pl-7"
            placeholder="0.00"
          />
        </div>
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
          Quantity <span className="text-red-500">*</span>
        </label>
        <input
          type="number"
          min="1"
          value={marketplaceData.quantity || 1}
          onChange={(e) => updateField('quantity', parseInt(e.target.value) || 1)}
          className="input w-full"
        />
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
          Tags <span className="text-xs text-[var(--text-muted)]">({tags.length}/13)</span>
        </label>
        <div className="flex gap-1.5 flex-wrap mb-2">
          {tags.map((tag: string) => (
            <span
              key={tag}
              className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs"
              style={{ background: 'var(--bg-secondary)', color: 'var(--text-primary)' }}
            >
              {tag}
              <button onClick={() => removeTag(tag)} className="opacity-50 hover:opacity-100">
                ×
              </button>
            </span>
          ))}
        </div>
        {tags.length < 13 && (
          <div className="flex gap-2">
            <input
              className="input flex-1 text-sm"
              value={tagInput}
              onChange={(e) => setTagInput(e.target.value)}
              onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); addTag(); } }}
              placeholder="Add tag…"
            />
            <button className="btn btn-secondary btn-sm" onClick={addTag}>+</button>
          </div>
        )}
      </div>

      <div>
        <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
          Description
        </label>
        <textarea
          value={marketplaceData.description || ''}
          onChange={(e) => updateField('description', e.target.value)}
          rows={4}
          className="input w-full resize-none text-sm"
          placeholder="Listing description for Etsy…"
        />
      </div>
    </div>
  );
}

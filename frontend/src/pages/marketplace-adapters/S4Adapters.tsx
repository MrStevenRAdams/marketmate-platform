import React from 'react';
import { MarketplaceFormProps } from '../types';

// ============================================================================
// SESSION 4 — MARKETPLACE ADAPTER FORMS
// Back Market · Zalando · Bol.com · Lazada
// Each exports a default named component used in MarketplaceRegistry.ts
// ============================================================================

// ── shared mini helpers ────────────────────────────────────────────────────────

const Field = ({ label, note, children }: { label: string; note?: string; children: React.ReactNode }) => (
  <div>
    <label style={{ display: 'block', fontSize: 13, fontWeight: 600, color: 'var(--text-primary)', marginBottom: 6 }}>
      {label}
      {note && <span style={{ fontWeight: 400, color: 'var(--text-muted)', fontSize: 12, marginLeft: 6 }}>{note}</span>}
    </label>
    {children}
  </div>
);

const inputCls = 'input w-full';
const selectCls = 'select w-full';

// ── BACK MARKET ────────────────────────────────────────────────────────────────

export function BackMarketAdapter({ coreData, marketplaceData, onChange, onSync }: MarketplaceFormProps) {
  const u = (field: string, value: any) => onChange({ ...marketplaceData, [field]: value });

  const gradeOptions = [
    { value: 'excellent', label: '⭐ Excellent — like new' },
    { value: 'good',      label: '✅ Good — minor cosmetic marks' },
    { value: 'fair',      label: '🔧 Fair — visible marks, fully functional' },
  ];

  return (
    <div className="space-y-4">
      <button onClick={onSync} className="w-full btn btn-secondary flex items-center justify-center gap-2">
        <i className="ri-refresh-line" /> Sync from Core Product Data
      </button>

      {/* Product title */}
      <Field label="Listing Title *" note="max 80 chars">
        <input
          type="text" maxLength={80} className={inputCls}
          value={marketplaceData.title || coreData?.title || ''}
          onChange={e => u('title', e.target.value)}
          placeholder="Refurbished iPhone 14 Pro 128GB Space Black"
        />
        <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 4, textAlign: 'right' }}>
          {(marketplaceData.title || '').length}/80
        </div>
      </Field>

      {/* Grade */}
      <Field label="Condition Grade *" note="required by Back Market">
        <select className={selectCls} value={marketplaceData.grade || 'good'} onChange={e => u('grade', e.target.value)}>
          {gradeOptions.map(g => <option key={g.value} value={g.value}>{g.label}</option>)}
        </select>
      </Field>

      {/* Condition description */}
      <Field label="Condition Description *" note="visible to buyers">
        <textarea
          rows={3} className={inputCls}
          value={marketplaceData.description || ''}
          onChange={e => u('description', e.target.value)}
          placeholder="Describe the specific cosmetic condition, what's included (charger, box), and any notable marks…"
          style={{ resize: 'vertical' }}
        />
      </Field>

      {/* Back Market product ID */}
      <Field label="Back Market Product ID *" note="numeric ID from Back Market catalogue">
        <input
          type="number" className={inputCls}
          value={marketplaceData.product_id || ''}
          onChange={e => u('product_id', parseInt(e.target.value) || '')}
          placeholder="e.g. 12345"
        />
        <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 4 }}>
          Find this in your Back Market Seller Portal → Catalogue.
        </div>
      </Field>

      {/* Price */}
      <div className="grid grid-cols-2 gap-3">
        <Field label="Price *">
          <input
            type="number" step="0.01" className={inputCls}
            value={marketplaceData.price || ''}
            onChange={e => u('price', parseFloat(e.target.value) || 0)}
            placeholder="0.00"
          />
        </Field>
        <Field label="Currency">
          <select className={selectCls} value={marketplaceData.currency || 'GBP'} onChange={e => u('currency', e.target.value)}>
            {['GBP', 'EUR', 'USD'].map(c => <option key={c}>{c}</option>)}
          </select>
        </Field>
      </div>

      {/* Stock */}
      <Field label="Stock Quantity *">
        <input
          type="number" min={0} className={inputCls}
          value={marketplaceData.quantity || ''}
          onChange={e => u('quantity', parseInt(e.target.value) || 0)}
        />
      </Field>

      {/* Warranty */}
      <Field label="Warranty (months)" note="optional">
        <input
          type="number" min={0} className={inputCls}
          value={marketplaceData.warranty_months || ''}
          onChange={e => u('warranty_months', parseInt(e.target.value) || 0)}
          placeholder="12"
        />
      </Field>

      {/* Info panel */}
      <div style={{ padding: '12px 14px', background: 'rgba(20,184,166,0.06)', borderRadius: 8, border: '1px solid rgba(20,184,166,0.2)', fontSize: 12, color: 'var(--text-muted)', lineHeight: 1.6 }}>
        <strong style={{ color: 'var(--text-primary)' }}>ℹ️ Back Market Notes</strong><br />
        Listings must have a valid Back Market Product ID from their catalogue. Grade and condition description are mandatory and visible to buyers. Back Market reviews all new listings before activation (typically 24–48h).
      </div>
    </div>
  );
}

// ── ZALANDO ────────────────────────────────────────────────────────────────────

export function ZalandoAdapter({ coreData, marketplaceData, onChange, onSync }: MarketplaceFormProps) {
  const u = (field: string, value: any) => onChange({ ...marketplaceData, [field]: value });

  const sizeCharts = ['EU', 'UK', 'US', 'IT', 'FR', 'DE', 'ONE SIZE'];
  const seasons = ['SS24', 'AW24', 'SS25', 'AW25', 'EVERGREEN'];

  return (
    <div className="space-y-4">
      <button onClick={onSync} className="w-full btn btn-secondary flex items-center justify-center gap-2">
        <i className="ri-refresh-line" /> Sync from Core Product Data
      </button>

      {/* Article name */}
      <Field label="Article Name *">
        <input
          type="text" className={inputCls}
          value={marketplaceData.name || coreData?.title || ''}
          onChange={e => u('name', e.target.value)}
          placeholder="Nike Air Max 270 Men's Trainers"
        />
      </Field>

      {/* EAN */}
      <Field label="EAN / Barcode *" note="required by Zalando for product matching">
        <input
          type="text" className={inputCls}
          value={marketplaceData.ean || coreData?.identifiers?.ean || ''}
          onChange={e => u('ean', e.target.value)}
          placeholder="5901234123457"
        />
      </Field>

      {/* Price & currency */}
      <div className="grid grid-cols-2 gap-3">
        <Field label="Price *">
          <input
            type="number" step="0.01" className={inputCls}
            value={marketplaceData.price || ''}
            onChange={e => u('price', parseFloat(e.target.value) || 0)}
            placeholder="0.00"
          />
        </Field>
        <Field label="Currency">
          <select className={selectCls} value={marketplaceData.currency || 'EUR'} onChange={e => u('currency', e.target.value)}>
            {['EUR', 'GBP', 'PLN', 'SEK', 'DKK', 'NOK'].map(c => <option key={c}>{c}</option>)}
          </select>
        </Field>
      </div>

      {/* Stock */}
      <Field label="Stock Quantity *">
        <input
          type="number" min={0} className={inputCls}
          value={marketplaceData.stock || ''}
          onChange={e => u('stock', parseInt(e.target.value) || 0)}
        />
      </Field>

      {/* Size chart */}
      <div className="grid grid-cols-2 gap-3">
        <Field label="Size Chart">
          <select className={selectCls} value={marketplaceData.size_chart || 'EU'} onChange={e => u('size_chart', e.target.value)}>
            {sizeCharts.map(s => <option key={s}>{s}</option>)}
          </select>
        </Field>
        <Field label="Size Value" note="e.g. 42, M, 10UK">
          <input
            type="text" className={inputCls}
            value={marketplaceData.size || ''}
            onChange={e => u('size', e.target.value)}
            placeholder="42"
          />
        </Field>
      </div>

      {/* Colour */}
      <Field label="Colour" note="as shown on Zalando">
        <input
          type="text" className={inputCls}
          value={marketplaceData.colour || ''}
          onChange={e => u('colour', e.target.value)}
          placeholder="Black / White"
        />
      </Field>

      {/* Season */}
      <Field label="Season / Collection">
        <select className={selectCls} value={marketplaceData.season || 'EVERGREEN'} onChange={e => u('season', e.target.value)}>
          {seasons.map(s => <option key={s}>{s}</option>)}
        </select>
      </Field>

      {/* Gender */}
      <Field label="Target Gender">
        <select className={selectCls} value={marketplaceData.gender || ''} onChange={e => u('gender', e.target.value)}>
          <option value="">— Select —</option>
          {['men', 'women', 'unisex', 'kids'].map(g => <option key={g} value={g}>{g.charAt(0).toUpperCase() + g.slice(1)}</option>)}
        </select>
      </Field>

      <div style={{ padding: '12px 14px', background: 'rgba(255,102,0,0.05)', borderRadius: 8, border: '1px solid rgba(255,102,0,0.15)', fontSize: 12, color: 'var(--text-muted)', lineHeight: 1.6 }}>
        <strong style={{ color: 'var(--text-primary)' }}>ℹ️ Zalando Notes</strong><br />
        Article creation must be done in the Zalando Partner Portal. MarketMate syncs <strong>price, stock, and tracking</strong> via the ZDirect API. EAN is mandatory for product matching.
      </div>
    </div>
  );
}

// ── BOL.COM ────────────────────────────────────────────────────────────────────

export function BolAdapter({ coreData, marketplaceData, onChange, onSync }: MarketplaceFormProps) {
  const u = (field: string, value: any) => onChange({ ...marketplaceData, [field]: value });

  const fulfilmentOptions = [
    { value: 'FBR', label: 'FBR — Fulfilled by Retailer (you ship)' },
    { value: 'FBB', label: 'FBB — Fulfilled by Bol (bol.com ships)' },
  ];

  const deliveryTimes = [
    { value: '1-2d', label: '1–2 business days' },
    { value: '2-3d', label: '2–3 business days' },
    { value: '3-5d', label: '3–5 business days' },
    { value: '1-2w', label: '1–2 weeks' },
  ];

  return (
    <div className="space-y-4">
      <button onClick={onSync} className="w-full btn btn-secondary flex items-center justify-center gap-2">
        <i className="ri-refresh-line" /> Sync from Core Product Data
      </button>

      {/* Title */}
      <Field label="Offer Title *">
        <input
          type="text" className={inputCls}
          value={marketplaceData.title || coreData?.title || ''}
          onChange={e => u('title', e.target.value)}
          placeholder="Product title as shown on bol.com"
        />
      </Field>

      {/* EAN */}
      <Field label="EAN *" note="product must exist in bol.com catalogue">
        <input
          type="text" className={inputCls}
          value={marketplaceData.ean || coreData?.identifiers?.ean || ''}
          onChange={e => u('ean', e.target.value)}
          placeholder="8712345678901"
        />
        <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 4 }}>
          The product must already exist in the bol.com catalogue. If not, submit it via the bol.com Seller Portal first.
        </div>
      </Field>

      {/* Price */}
      <div className="grid grid-cols-2 gap-3">
        <Field label="Price (EUR) *">
          <input
            type="number" step="0.01" className={inputCls}
            value={marketplaceData.price || ''}
            onChange={e => u('price', parseFloat(e.target.value) || 0)}
            placeholder="0.00"
          />
        </Field>
        <Field label="Stock Quantity *">
          <input
            type="number" min={0} className={inputCls}
            value={marketplaceData.stock || ''}
            onChange={e => u('stock', parseInt(e.target.value) || 0)}
          />
        </Field>
      </div>

      {/* Fulfilment method */}
      <Field label="Fulfilment Method *">
        <select className={selectCls} value={marketplaceData.fulfilment || 'FBR'} onChange={e => u('fulfilment', e.target.value)}>
          {fulfilmentOptions.map(o => <option key={o.value} value={o.value}>{o.label}</option>)}
        </select>
      </Field>

      {/* Delivery time */}
      <Field label="Delivery Time Promise">
        <select className={selectCls} value={marketplaceData.delivery_time || '2-3d'} onChange={e => u('delivery_time', e.target.value)}>
          {deliveryTimes.map(d => <option key={d.value} value={d.value}>{d.label}</option>)}
        </select>
      </Field>

      {/* Condition */}
      <Field label="Condition">
        <select className={selectCls} value={marketplaceData.condition || 'NEW'} onChange={e => u('condition', e.target.value)}>
          <option value="NEW">New</option>
          <option value="GOOD">As Good As New</option>
          <option value="REASONABLE">Reasonable</option>
          <option value="MODERATE">Moderate</option>
        </select>
      </Field>

      {/* Reference (your SKU) */}
      <Field label="Your Reference / SKU" note="stored as your internal reference">
        <input
          type="text" className={inputCls}
          value={marketplaceData.reference || coreData?.sku || ''}
          onChange={e => u('reference', e.target.value)}
          placeholder="Your internal SKU"
        />
      </Field>

      <div style={{ padding: '12px 14px', background: 'rgba(14,66,153,0.05)', borderRadius: 8, border: '1px solid rgba(14,66,153,0.15)', fontSize: 12, color: 'var(--text-muted)', lineHeight: 1.6 }}>
        <strong style={{ color: 'var(--text-primary)' }}>ℹ️ Bol.com Notes</strong><br />
        Bol.com is the dominant marketplace in the Netherlands and Belgium. EAN is required — the product must be in their catalogue. MarketMate syncs <strong>stock, price, and shipment confirmations</strong> via the Retailer API v10.
      </div>
    </div>
  );
}

// ── LAZADA ────────────────────────────────────────────────────────────────────

export function LazadaAdapter({ coreData, marketplaceData, onChange, onSync }: MarketplaceFormProps) {
  const u = (field: string, value: any) => onChange({ ...marketplaceData, [field]: value });

  const regions = [
    { value: 'MY', label: '🇲🇾 Malaysia (Lazada.com.my)', url: 'https://api.lazada.com.my/rest' },
    { value: 'SG', label: '🇸🇬 Singapore (Lazada.sg)', url: 'https://api.lazada.sg/rest' },
    { value: 'TH', label: '🇹🇭 Thailand (Lazada.co.th)', url: 'https://api.lazada.co.th/rest' },
    { value: 'ID', label: '🇮🇩 Indonesia (Lazada.co.id)', url: 'https://api.lazada.co.id/rest' },
    { value: 'PH', label: '🇵🇭 Philippines (Lazada.com.ph)', url: 'https://api.lazada.com.ph/rest' },
    { value: 'VN', label: '🇻🇳 Vietnam (Lazada.vn)', url: 'https://api.lazada.vn/rest' },
  ];

  const handleRegionChange = (regionCode: string) => {
    const region = regions.find(r => r.value === regionCode);
    u('region', regionCode);
    if (region) u('base_url', region.url);
  };

  return (
    <div className="space-y-4">
      <button onClick={onSync} className="w-full btn btn-secondary flex items-center justify-center gap-2">
        <i className="ri-refresh-line" /> Sync from Core Product Data
      </button>

      {/* Product name */}
      <Field label="Product Name *">
        <input
          type="text" className={inputCls}
          value={marketplaceData.name || coreData?.title || ''}
          onChange={e => u('name', e.target.value)}
          placeholder="Samsung Galaxy S24 Ultra 256GB Phantom Black"
        />
      </Field>

      {/* Region */}
      <Field label="Lazada Region *" note="determines which API endpoint is used">
        <select className={selectCls} value={marketplaceData.region || 'MY'} onChange={e => handleRegionChange(e.target.value)}>
          {regions.map(r => <option key={r.value} value={r.value}>{r.label}</option>)}
        </select>
      </Field>

      {/* Price & currency */}
      <div className="grid grid-cols-2 gap-3">
        <Field label="Price *">
          <input
            type="number" step="0.01" className={inputCls}
            value={marketplaceData.price || ''}
            onChange={e => u('price', parseFloat(e.target.value) || 0)}
            placeholder="0.00"
          />
        </Field>
        <Field label="Sale Price" note="optional discount price">
          <input
            type="number" step="0.01" className={inputCls}
            value={marketplaceData.sale_price || ''}
            onChange={e => u('sale_price', parseFloat(e.target.value) || 0)}
            placeholder="0.00"
          />
        </Field>
      </div>

      {/* Stock */}
      <Field label="Stock Quantity *">
        <input
          type="number" min={0} className={inputCls}
          value={marketplaceData.quantity || ''}
          onChange={e => u('quantity', parseInt(e.target.value) || 0)}
        />
      </Field>

      {/* Category */}
      <Field label="Lazada Category ID" note="from Lazada Seller Center category tree">
        <input
          type="text" className={inputCls}
          value={marketplaceData.category_id || ''}
          onChange={e => u('category_id', e.target.value)}
          placeholder="e.g. 10000168"
        />
      </Field>

      {/* Brand */}
      <Field label="Brand">
        <input
          type="text" className={inputCls}
          value={marketplaceData.brand || coreData?.brand || ''}
          onChange={e => u('brand', e.target.value)}
          placeholder="Samsung"
        />
      </Field>

      {/* Description */}
      <Field label="Product Description" note="shown on product page">
        <textarea
          rows={4} className={inputCls}
          value={marketplaceData.description || coreData?.description || ''}
          onChange={e => u('description', e.target.value)}
          placeholder="Describe the product features, compatibility, and what's included…"
          style={{ resize: 'vertical' }}
        />
      </Field>

      {/* Shipping provider */}
      <Field label="Default Shipping Provider" note="used when pushing tracking">
        <input
          type="text" className={inputCls}
          value={marketplaceData.shipping_provider || ''}
          onChange={e => u('shipping_provider', e.target.value)}
          placeholder="e.g. Pos Laju, J&T Express, DHL"
        />
      </Field>

      <div style={{ padding: '12px 14px', background: 'rgba(245,114,36,0.05)', borderRadius: 8, border: '1px solid rgba(245,114,36,0.2)', fontSize: 12, color: 'var(--text-muted)', lineHeight: 1.6 }}>
        <strong style={{ color: 'var(--text-primary)' }}>ℹ️ Lazada Notes</strong><br />
        Listing creation must be done in Lazada Seller Center. MarketMate syncs <strong>stock, price, and tracking</strong> via the Lazada Open Platform API. Access token must be refreshed every 30 days — you'll be prompted when it expires.
      </div>
    </div>
  );
}

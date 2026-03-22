// ============================================================================
// KAUFLAND LISTING CREATE PAGE
// ============================================================================
// Kaufland uses EAN-based listing units. Each unit has a price, stock amount,
// condition, and optional shipping group / handling time.
// Arrives with ?product_id=xxx from the product edit page.
//
// Fields: EAN (required), listing price (required), stock amount, condition,
// minimum price, seller note, shipping group, handling time, categories.

import { useState, useEffect } from 'react';
import { useSearchParams, useNavigate } from 'react-router-dom';
import kauflandApi, { KauflandCategory, ChannelVariantDraft } from '../../services/kaufland-api';
import ConfiguratorSelector from './ConfiguratorSelector';
import { ConfiguratorDetail } from '../../services/configurator-api';

// Kaufland condition IDs
const KAUFLAND_CONDITIONS = [
  { id: 1,  label: 'New' },
  { id: 2,  label: 'Like New' },
  { id: 3,  label: 'Very Good' },
  { id: 4,  label: 'Good' },
  { id: 5,  label: 'Acceptable' },
];

// Common German carrier codes for Kaufland fulfilment
const CARRIER_CODES = [
  'DHL', 'DPD', 'Hermes', 'GLS', 'UPS', 'FedEx', 'TNT', 'Postnord', 'OTHER',
];

export default function KauflandListingCreate() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const productId    = searchParams.get('product_id');
  const credentialId = searchParams.get('credential_id') || '';

  // ── Form state ────────────────────────────────────────────────────────────
  const [loading, setLoading]           = useState(true);
  const [aiGenerating, setAiGenerating] = useState(false);
  const [aiApplied, setAiApplied] = useState(false);
  const [aiError, setAiError] = useState('');
  const [error, setError]               = useState('');

  const [ean, setEan]                   = useState('');
  const [listingPrice, setListingPrice] = useState('');
  const [minimumPrice, setMinimumPrice] = useState('');
  const [amount, setAmount]             = useState<number>(0);
  const [condition, setCondition]       = useState<number>(1);
  const [note, setNote]                 = useState('');
  const [shippingGroup, setShippingGroup] = useState('');
  const [handlingTime, setHandlingTime] = useState<number>(1);

  // Category browsing (optional — Kaufland is EAN-driven)
  const [categories, setCategories]     = useState<KauflandCategory[]>([]);
  const [catLoading, setCatLoading]     = useState(false);

  // ── Submit state ──────────────────────────────────────────────────────────
  const [submitting, setSubmitting]     = useState(false);
  const [submitError, setSubmitError]   = useState('');
  const [submitResult, setSubmitResult] = useState<{ unitId: string; status: string } | null>(null);

  // ── Configurator (CFG-07) ──
  const [_selectedConfigurator, setSelectedConfigurator] = useState<ConfiguratorDetail | null>(null);

  // VAR-01 — Variation listings
  const [variants, setVariants] = useState<ChannelVariantDraft[]>([]);
  const [isVariantProduct, setIsVariantProduct] = useState(false);
  const [variantSubmitResults, setVariantSubmitResults] = useState<{ sku: string; ean: string; unitId?: string; error?: string }[]>([]);
  const updateVariant = (id: string, field: keyof ChannelVariantDraft, value: any) => {
    setVariants(vs => vs.map(v => v.id === id ? { ...v, [field]: value } : v));
  };

  const handleConfiguratorSelect = (cfg: ConfiguratorDetail | null) => {
    setSelectedConfigurator(cfg);
    if (!cfg) return;
    // Pre-populate shipping group
    if (cfg.shipping_defaults?.shipping_group) setShippingGroup(cfg.shipping_defaults.shipping_group);
    // Pre-populate attribute defaults
    if (cfg.attribute_defaults) {
      for (const attr of cfg.attribute_defaults) {
        if (attr.source === 'default_value' && attr.default_value) {
          if (attr.attribute_name === 'note') setNote(attr.default_value);
        }
      }
    }
  };

  // ── Init ──────────────────────────────────────────────────────────────────
  useEffect(() => {
    if (!productId) {
      setError('No product_id provided. Please open this page from a product.');
      setLoading(false);
      return;
    }
    init();
  }, [productId]);

  async function init() {
    setLoading(true);
    setError('');
    try {
      const res = await kauflandApi.prepareDraft(productId!, credentialId || undefined);
      if (res.data?.ok) {
        const d = res.data.draft;
        setEan(d.ean || '');
        setListingPrice(d.listing_price > 0 ? String(d.listing_price) : '');
        setMinimumPrice(d.minimum_price > 0 ? String(d.minimum_price) : '');
        setAmount(d.amount || 0);
        setCondition(d.condition || 1);
        setNote(d.note || '');
        setShippingGroup(d.shipping_group || '');
        setHandlingTime(d.handling_time_in_days || 1);
        // VAR-01: load variants
        if (d.variants && d.variants.length > 0) {
          setVariants(d.variants);
          setIsVariantProduct(true);
        }
      } else {
        setError('Could not load product data. Make sure a Kaufland credential is connected.');
      }


      const aiFlag = searchParams.get('ai');
      if (aiFlag === 'pending' && productId) {
        setAiGenerating(true);
        try {
          const { aiService: aiApi } = await import('../../services/ai-api');
          const schemaFields = [
            { name: 'title', display_name: 'Title', data_type: 'string', required: true, allowed_values: [], max_length: 0 },
            { name: 'description', display_name: 'Description', data_type: 'string', required: true, allowed_values: [], max_length: 0 },
            { name: 'ean', display_name: 'EAN', data_type: 'string', required: false, allowed_values: [], max_length: 0 }
          ];
          const aiRes = await aiApi.generateWithSchema({
            product_id: productId,
            channel: 'kaufland',
            category_id: '',
            category_name: '',
            fields: schemaFields,
          });
          const aiListing = aiRes.data.data?.listings?.[0];
          if (aiListing) {
            if (aiListing.title) { setTitle(aiListing.title); }
            if (aiListing.description) { setDescription(aiListing.description); }
            setAiApplied(true);
          }
        } catch (aiErr: any) {
          setAiError(aiErr.response?.data?.error || aiErr.message || 'AI generation failed');
        }
        setAiGenerating(false);
      }
    } catch (err: any) {
      setError(err.message || 'Failed to load product data');
    } finally {
      setLoading(false);
    }

    // Load categories in background (non-blocking)
    loadCategories();
  }

  async function loadCategories() {
    setCatLoading(true);
    try {
      const res = await kauflandApi.getCategories(credentialId || undefined);
      if (res.data?.ok) {
        setCategories(res.data.categories || []);
      }
    } catch {
      // silently ignore — categories are optional context
    } finally {
      setCatLoading(false);
    }
  }

  // ── Submit ────────────────────────────────────────────────────────────────
  async function handleSubmit() {
    // VAR-01: EAN enforcement — Kaufland requires EAN for every submitted variant.
    // Block submission if any active variant is missing an EAN.
    if (isVariantProduct) {
      const activeVariants = variants.filter(v => v.active);
      const missingEAN = activeVariants.filter(v => !v.ean.trim());
      if (missingEAN.length > 0) {
        setSubmitError(
          `${missingEAN.length} active variant${missingEAN.length > 1 ? 's are' : ' is'} missing an EAN. ` +
          `Kaufland requires a unique EAN for each variant. ` +
          `Please fill in all EAN fields or deactivate variants without EANs.`
        );
        return;
      }
    }

    setSubmitting(true);
    setSubmitError('');
    setSubmitResult(null);
    setVariantSubmitResults([]);

    // VAR-01: multi-variant path — one unit per active variant
    const activeVariants = variants.filter(v => v.active);
    if (isVariantProduct && activeVariants.length >= 2) {
      const results: { sku: string; ean: string; unitId?: string; error?: string }[] = [];
      for (const v of activeVariants) {
        if (!v.ean) {
          results.push({ sku: v.sku, ean: '', error: 'Skipped — no EAN' });
          continue;
        }
        const price = parseFloat(v.price) > 0 ? parseFloat(v.price) : parseFloat(listingPrice);
        const stock = parseInt(v.stock) >= 0 ? parseInt(v.stock) : amount;
        try {
          const payload: Record<string, unknown> = {
            ean: v.ean.trim(),
            condition,
            listing_price: price,
            amount: stock,
            handling_time_in_days: handlingTime,
            credential_id: credentialId || undefined,
            variants: [], // empty — tells backend to use single-unit path for this call
          };
          if (minimumPrice && parseFloat(minimumPrice) > 0) payload.minimum_price = parseFloat(minimumPrice);
          if (note.trim()) payload.note = note.trim();
          if (shippingGroup.trim()) payload.shipping_group = shippingGroup.trim();
          const res = await kauflandApi.submitUnit(payload as any);
          if (res.data?.ok) {
            results.push({ sku: v.sku, ean: v.ean, unitId: res.data.unit_id });
          } else {
            results.push({ sku: v.sku, ean: v.ean, error: res.data?.error || 'Failed' });
          }
        } catch (err: any) {
          results.push({ sku: v.sku, ean: v.ean, error: err?.response?.data?.error || err.message });
        }
      }
      setVariantSubmitResults(results);
      setSubmitting(false);
      return;
    }

    // Single-unit path (original)
    if (!ean.trim()) {
      setSubmitError('EAN is required. Kaufland identifies products by their EAN barcode.');
      setSubmitting(false);
      return;
    }
    if (!listingPrice || parseFloat(listingPrice) <= 0) {
      setSubmitError('Listing price must be greater than €0.00');
      setSubmitting(false);
      return;
    }

    try {
      const payload: Record<string, unknown> = {
        ean: ean.trim(),
        condition,
        listing_price: parseFloat(listingPrice),
        amount,
        handling_time_in_days: handlingTime,
        credential_id: credentialId || undefined,
      };
      if (minimumPrice && parseFloat(minimumPrice) > 0) payload.minimum_price = parseFloat(minimumPrice);
      if (note.trim()) payload.note = note.trim();
      if (shippingGroup.trim()) payload.shipping_group = shippingGroup.trim();

      const res = await kauflandApi.submitUnit(payload as any);
      if (!res.data?.ok) {
        setSubmitError(res.data?.error || 'Failed to create unit on Kaufland');
        return;
      }
      setSubmitResult({
        unitId: res.data.unit_id || '',
        status: res.data.status || 'created',
      });
    } catch (err: any) {
      setSubmitError(err?.response?.data?.error || err.message || 'Submission failed');
    } finally {
      setSubmitting(false);
    }
  }

  // ── Validation summary ─────────────────────────────────────────────────────
  const validationItems = [
    { field: 'EAN', ok: ean.trim().length >= 8 },
    { field: 'Listing Price (€)', ok: !!listingPrice && parseFloat(listingPrice) > 0 },
    { field: 'Stock Amount', ok: amount >= 0, warn: true },
    { field: 'Condition', ok: condition > 0 && condition <= 5 },
    { field: 'Handling Time', ok: handlingTime >= 1, warn: true },
  ];

  // ── Render ────────────────────────────────────────────────────────────────
  if (loading) {
    return (
      <div className="page-wrapper flex items-center justify-center min-h-[300px]">
        <div className="flex items-center gap-2 text-[var(--text-muted)]">
          <i className="ri-loader-4-line animate-spin text-xl" />
          Loading product data…
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="page-wrapper">
        <div className="alert alert-error">
          <i className="ri-error-warning-line" /> {error}
        </div>

        <button className="btn btn-secondary mt-4" onClick={() => navigate(-1)}>
          <i className="ri-arrow-left-line" /> Back
        </button>
      </div>
    );
  }

  return (
    <div className="page-wrapper">
      {aiGenerating && (
        <div style={{ padding: '12px 14px', background: 'linear-gradient(90deg, rgba(139,92,246,0.15), rgba(59,130,246,0.15))', borderRadius: 8, fontSize: 13, color: 'var(--accent-purple)', marginBottom: 16, display: 'flex', gap: 10, alignItems: 'center', border: '1px solid rgba(139,92,246,0.3)' }}>
          <div className="spinner" style={{ width: 16, height: 16, border: '2px solid var(--accent-purple)', borderTopColor: 'transparent', borderRadius: '50%', animation: 'spin 1s linear infinite' }} />
          <span style={{ fontWeight: 600 }}>🤖 AI is generating optimised content for Kaufland...</span>
        </div>
      )}
      {aiApplied && (
        <div style={{ padding: '10px 14px', background: 'linear-gradient(90deg, rgba(139,92,246,0.15), rgba(59,130,246,0.15))', borderRadius: 8, fontSize: 12, color: 'var(--accent-purple)', marginBottom: 16, display: 'flex', gap: 8, alignItems: 'center', border: '1px solid rgba(139,92,246,0.3)' }}>
          <span style={{ fontSize: 16 }}>🤖</span>
          <span style={{ fontWeight: 600 }}>AI-generated content applied</span>
          <span style={{ color: 'var(--text-muted)' }}>— title and description have been filled. Review before submitting.</span>
        </div>
      )}
      {aiError && (
        <div style={{ padding: '10px 14px', background: 'var(--warning-glow)', borderRadius: 8, fontSize: 12, color: 'var(--warning)', marginBottom: 16, border: '1px solid var(--warning)' }}>
          ⚠️ AI generation failed: {aiError} — fill in fields manually.
        </div>
      )}
      {/* Header */}
      <div className="page-header mb-6">
        <div className="flex items-center gap-3">
          <button className="btn btn-ghost btn-sm" onClick={() => navigate(-1)}>
            <i className="ri-arrow-left-line" />
          </button>
          <div>
            <h1 className="page-title flex items-center gap-2">
              <span>🛒</span> Create Kaufland Listing
            </h1>
            <p className="page-subtitle">
              Kaufland listings are EAN-driven. Provide the EAN barcode, price and stock to go live.
            </p>
          </div>
        </div>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* ── Main form ─────────────────────────────────────────────────── */}
        <div className="lg:col-span-2 space-y-4">

          {/* ── Configurator (CFG-07) ── */}
          <ConfiguratorSelector channel="kaufland" credentialId={credentialId} onSelect={handleConfiguratorSelect} />

          {/* Success banner */}
          {submitResult && (
            <div className="card" style={{ borderLeft: '4px solid #22c55e' }}>
              <div className="card-body">
                <div className="flex items-start gap-3">
                  <i className="ri-checkbox-circle-fill text-green-400 text-xl mt-0.5" />
                  <div>
                    <p className="font-semibold text-[var(--text-primary)]">Unit created on Kaufland</p>
                    {submitResult.unitId && (
                      <p className="text-xs text-[var(--text-muted)] font-mono mt-0.5">
                        Unit ID: {submitResult.unitId}
                      </p>
                    )}
                    <p className="text-sm text-[var(--text-muted)] mt-1">
                      Your listing is now live on Kaufland. It may take a few minutes to appear in search results.
                    </p>
                  </div>
                </div>
              </div>
            </div>
          )}

          {/* EAN & Product Identity */}
          <div className="card">
            <div className="card-header">
              <h3 className="card-title">Product Identity</h3>
              <p className="card-subtitle">
                Kaufland uses the EAN to match your listing to the product catalogue
              </p>
            </div>
            <div className="card-body space-y-4">
              <div>
                <label className="form-label">
                  EAN / GTIN <span className="text-red-500">*</span>
                </label>
                <input
                  type="text"
                  value={ean}
                  onChange={(e) => setEan(e.target.value)}
                  placeholder="e.g. 4006381333931"
                  className="input w-full font-mono"
                  maxLength={14}
                />
                <p className="form-hint">
                  8, 12, or 13-digit EAN/GTIN barcode. Kaufland uses this to link your unit to the product page.
                </p>
              </div>

              <div>
                <label className="form-label">Condition</label>
                <select
                  value={condition}
                  onChange={(e) => setCondition(parseInt(e.target.value))}
                  className="input w-full"
                >
                  {KAUFLAND_CONDITIONS.map((c) => (
                    <option key={c.id} value={c.id}>{c.label}</option>
                  ))}
                </select>
              </div>

              <div>
                <label className="form-label">Seller Note</label>
                <textarea
                  value={note}
                  onChange={(e) => setNote(e.target.value)}
                  rows={3}
                  className="input w-full resize-none"
                  placeholder="Optional condition details visible to buyers…"
                  maxLength={500}
                />
                <p className="form-hint">{note.length}/500 characters</p>
              </div>
            </div>
          </div>

          {/* Pricing */}
          <div className="card">
            <div className="card-header">
              <h3 className="card-title">Pricing</h3>
            </div>
            <div className="card-body space-y-4">
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="form-label">
                    Listing Price (€) <span className="text-red-500">*</span>
                  </label>
                  <div className="relative">
                    <span className="absolute left-3 top-1/2 -translate-y-1/2 text-[var(--text-muted)]">€</span>
                    <input
                      type="number"
                      step="0.01"
                      min="0.01"
                      value={listingPrice}
                      onChange={(e) => setListingPrice(e.target.value)}
                      className="input w-full pl-7"
                      placeholder="0.00"
                    />
                  </div>
                </div>
                <div>
                  <label className="form-label">Minimum Price (€)</label>
                  <div className="relative">
                    <span className="absolute left-3 top-1/2 -translate-y-1/2 text-[var(--text-muted)]">€</span>
                    <input
                      type="number"
                      step="0.01"
                      min="0"
                      value={minimumPrice}
                      onChange={(e) => setMinimumPrice(e.target.value)}
                      className="input w-full pl-7"
                      placeholder="Optional floor price"
                    />
                  </div>
                  <p className="form-hint">Used by Kaufland's price automation</p>
                </div>
              </div>
            </div>
          </div>

          {/* Stock & Fulfilment */}
          <div className="card">
            <div className="card-header">
              <h3 className="card-title">Stock & Fulfilment</h3>
            </div>
            <div className="card-body space-y-4">
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="form-label">Stock Amount</label>
                  <input
                    type="number"
                    min="0"
                    value={amount}
                    onChange={(e) => setAmount(parseInt(e.target.value) || 0)}
                    className="input w-full"
                  />
                  <p className="form-hint">Setting to 0 deactivates the unit</p>
                </div>
                <div>
                  <label className="form-label">Handling Time (days)</label>
                  <input
                    type="number"
                    min="1"
                    max="30"
                    value={handlingTime}
                    onChange={(e) => setHandlingTime(parseInt(e.target.value) || 1)}
                    className="input w-full"
                  />
                </div>
              </div>

              <div>
                <label className="form-label">Shipping Group</label>
                <input
                  type="text"
                  value={shippingGroup}
                  onChange={(e) => setShippingGroup(e.target.value)}
                  placeholder="e.g. Standard"
                  className="input w-full"
                />
                <p className="form-hint">
                  Leave blank to use your default shipping group from Kaufland Seller Centre.
                </p>
              </div>
            </div>
          </div>

          {/* Category Browser (informational) */}
          {categories.length > 0 && (
            <div className="card">
              <div className="card-header">
                <h3 className="card-title">Browse Categories</h3>
                <p className="card-subtitle">
                  Kaufland assigns the category automatically from your EAN.
                  This list is shown for reference only.
                </p>
              </div>
              <div className="card-body">
                {catLoading ? (
                  <p className="text-sm text-[var(--text-muted)] flex items-center gap-2">
                    <i className="ri-loader-4-line animate-spin" /> Loading categories…
                  </p>
                ) : (
                  <div className="grid grid-cols-2 gap-2 max-h-64 overflow-y-auto pr-1">
                    {categories.slice(0, 40).map((cat) => (
                      <div
                        key={cat.category_id}
                        className="text-xs px-2 py-1.5 rounded text-[var(--text-muted)]"
                        style={{ background: 'var(--bg-secondary)' }}
                      >
                        {cat.title}
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </div>
          )}
        </div>

        {/* ── Sidebar ───────────────────────────────────────────────────── */}
        <div className="space-y-4">

          {/* VAR-01 — Variant Grid (mimicked variations) */}
          {isVariantProduct && (
            <div className="card">
              <div className="card-header">
                <h3 className="card-title" style={{ color: '#d946ef' }}>Variants ({variants.length})</h3>
              </div>
              <div className="card-body space-y-3">
                <div className="p-3 rounded-lg text-sm" style={{ background: 'rgba(245,158,11,0.1)', border: '1px solid rgba(245,158,11,0.3)' }}>
                  <p className="font-medium mb-1" style={{ color: '#f59e0b' }}>
                    <i className="ri-git-branch-line mr-1" />Mimicked variations
                  </p>
                  <p style={{ color: 'var(--text-muted)' }}>
                    Kaufland does not support native variation groups. Each active variant will be submitted as a separate unit using its own EAN. Variants without an EAN will be skipped.
                  </p>
                </div>
                <div style={{ overflowX: 'auto' }}>
                  <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12 }}>
                    <thead>
                      <tr style={{ borderBottom: '2px solid var(--border)' }}>
                        <th style={{ padding: '6px 8px', textAlign: 'left', fontWeight: 600, color: 'var(--text-secondary)' }}>Active</th>
                        {variants.length > 0 && Object.keys(variants[0].combination).map(k => (
                          <th key={k} style={{ padding: '6px 8px', textAlign: 'left', fontWeight: 600, color: 'var(--text-secondary)' }}>{k}</th>
                        ))}
                        <th style={{ padding: '6px 8px', textAlign: 'left', fontWeight: 600, color: 'var(--text-secondary)' }}>SKU</th>
                        <th style={{ padding: '6px 8px', textAlign: 'left', fontWeight: 600, color: 'var(--text-secondary)' }}>Price (€)</th>
                        <th style={{ padding: '6px 8px', textAlign: 'left', fontWeight: 600, color: 'var(--text-secondary)' }}>Stock</th>
                        <th style={{ padding: '6px 8px', textAlign: 'left', fontWeight: 600, color: '#ef4444' }}>EAN *</th>
                      </tr>
                    </thead>
                    <tbody>
                      {variants.map(v => (
                        <tr key={v.id} style={{ borderBottom: '1px solid var(--border)', opacity: v.active ? 1 : 0.45 }}>
                          <td style={{ padding: '5px 8px', verticalAlign: 'middle' }}>
                            <input type="checkbox" checked={v.active} onChange={e => updateVariant(v.id, 'active', e.target.checked)} />
                          </td>
                          {Object.values(v.combination).map((val, i) => (
                            <td key={i} style={{ padding: '5px 8px', verticalAlign: 'middle' }}>{val}</td>
                          ))}
                          <td style={{ padding: '5px 8px', verticalAlign: 'middle' }}>
                            <input value={v.sku} onChange={e => updateVariant(v.id, 'sku', e.target.value)}
                              style={{ padding: '3px 6px', fontSize: 11, width: 100, borderRadius: 5, border: '1px solid var(--border)', background: 'var(--bg-primary)', color: 'var(--text-primary)' }} />
                          </td>
                          <td style={{ padding: '5px 8px', verticalAlign: 'middle' }}>
                            <input value={v.price} onChange={e => updateVariant(v.id, 'price', e.target.value)}
                              style={{ padding: '3px 6px', fontSize: 11, width: 70, borderRadius: 5, border: '1px solid var(--border)', background: 'var(--bg-primary)', color: 'var(--text-primary)' }}
                              type="number" step="0.01" />
                          </td>
                          <td style={{ padding: '5px 8px', verticalAlign: 'middle' }}>
                            <input value={v.stock} onChange={e => updateVariant(v.id, 'stock', e.target.value)}
                              style={{ padding: '3px 6px', fontSize: 11, width: 50, borderRadius: 5, border: '1px solid var(--border)', background: 'var(--bg-primary)', color: 'var(--text-primary)' }}
                              type="number" />
                          </td>
                          <td style={{ padding: '5px 8px', verticalAlign: 'middle' }}>
                            <input value={v.ean} onChange={e => updateVariant(v.id, 'ean', e.target.value)}
                              style={{ padding: '3px 6px', fontSize: 11, width: 110, borderRadius: 5, border: `1px solid ${v.ean ? 'var(--border)' : '#ef4444'}`, background: 'var(--bg-primary)', color: 'var(--text-primary)' }}
                              placeholder="Required" />
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>

                {/* Per-variant submission results */}
                {variantSubmitResults.length > 0 && (
                  <div className="space-y-1 mt-2">
                    {variantSubmitResults.map((r, i) => (
                      <div key={i} className="flex items-center gap-2 text-xs p-2 rounded" style={{ background: r.unitId ? 'rgba(34,197,94,0.08)' : 'rgba(239,68,68,0.08)' }}>
                        <i className={r.unitId ? 'ri-checkbox-circle-line text-green-400' : 'ri-close-circle-line text-red-400'} />
                        <span style={{ color: 'var(--text-secondary)' }}>{r.sku}</span>
                        <span style={{ color: 'var(--text-muted)' }}>EAN: {r.ean || '—'}</span>
                        {r.unitId && <span className="ml-auto" style={{ color: 'var(--text-muted)' }}>Unit: {r.unitId}</span>}
                        {r.error && <span className="ml-auto text-red-400">{r.error}</span>}
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </div>
          )}

          {/* Submit */}
          <div className="card">
            <div className="card-header">
              <h3 className="card-title">Submit to Kaufland</h3>
            </div>
            <div className="card-body space-y-3">
              <div className="p-3 rounded-lg text-sm" style={{ background: 'var(--bg-secondary)' }}>
                <p className="font-medium text-[var(--text-primary)] mb-1">
                  <i className="ri-information-line mr-1" />EAN-based listing
                </p>
                <p className="text-[var(--text-muted)]">
                  Kaufland matches your unit to the product catalogue via EAN.
                  Your price, stock and condition are what differentiate your offer.
                </p>
              </div>

              {submitError && (
                <div className="alert alert-error text-sm">
                  <i className="ri-error-warning-line" /> {submitError}
                </div>
              )}

              <button
                className="btn btn-primary w-full"
                onClick={handleSubmit}
                disabled={submitting || !!(isVariantProduct ? variantSubmitResults.length > 0 : submitResult) || (isVariantProduct && variants.filter(v => v.active && !v.ean.trim()).length > 0)}
              >
                {submitting ? (
                  <><i className="ri-loader-4-line animate-spin" /> Creating unit{isVariantProduct ? 's' : ''}…</>
                ) : (variantSubmitResults.length > 0 || submitResult) ? (
                  <><i className="ri-checkbox-circle-line" /> {isVariantProduct ? 'Units Created' : 'Unit Created'}</>
                ) : (
                  <><i className="ri-add-circle-line" /> {isVariantProduct ? `Create ${variants.filter(v => v.active).length} Units on Kaufland` : 'Create Unit on Kaufland'}</>
                )}
              </button>

              {submitResult && (
                <button
                  className="btn btn-secondary w-full"
                  onClick={() => navigate(-1)}
                >
                  <i className="ri-check-line" /> Done
                </button>
              )}
            </div>
          </div>

          {/* Requirements checklist */}
          <div className="card">
            <div className="card-header">
              <h3 className="card-title">Requirements</h3>
            </div>
            <div className="card-body text-sm space-y-2">
              {validationItems.map(({ field, ok, warn }) => (
                <div key={field} className="flex items-center gap-2">
                  <i
                    className={
                      ok
                        ? 'ri-checkbox-circle-line text-green-400'
                        : (warn ?? false)
                          ? 'ri-alert-line text-yellow-400'
                          : 'ri-close-circle-line text-red-400'
                    }
                  />
                  <span className={ok ? 'text-[var(--text-primary)]' : 'text-[var(--text-muted)]'}>
                    {field}
                    {!ok && (warn ?? false) ? ' (recommended)' : !ok ? ' (required)' : ''}
                  </span>
                </div>
              ))}
            </div>
          </div>

          {/* Kaufland info */}
          <div className="card">
            <div className="card-header">
              <h3 className="card-title">About Kaufland</h3>
            </div>
            <div className="card-body text-sm text-[var(--text-muted)] space-y-2">
              <p>
                <i className="ri-global-line mr-1" />
                Operates in DE, SK, CZ, PL, HR, RO, and BG.
              </p>
              <p>
                <i className="ri-barcode-line mr-1" />
                All listings require an EAN/GTIN. Kaufland uses this to populate product title, images and description automatically.
              </p>
              <p>
                <i className="ri-truck-line mr-1" />
                Orders are fulfilled per order unit. Each line item gets a separate tracking number push.
              </p>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

// ============================================================================
// BLUEPARK LISTING CREATE PAGE
// ============================================================================
// Location: frontend/src/pages/marketplace/BlueparkListingCreate.tsx
//
// Arrives with ?product_id=xxx&credential_id=yyy from the product edit page.
// Pattern: useSearchParams → prepare → draft state → updateDraft → handleSubmit.
// Follows the same structure as OnBuyListingCreate.tsx.
// ============================================================================

import { useState, useEffect, useMemo } from 'react';
import { useSearchParams, useNavigate } from 'react-router-dom';
import { blueparkApi, BlueparkSubmitPayload } from '../../services/bluepark-api';
import { ChannelVariantDraft } from '../../services/ebay-api';
import ConfiguratorSelector from './ConfiguratorSelector';
import { ConfiguratorDetail } from '../../services/configurator-api';

// Bluepark brand colour
const BP_BLUE = '#003087';

// ── Condition options ─────────────────────────────────────────────────────────

const CONDITION_OPTIONS = [
  { value: 'new', label: 'New' },
  { value: 'used', label: 'Used' },
  { value: 'refurbished', label: 'Refurbished' },
];

const STATUS_OPTIONS = [
  { value: 'active', label: 'Active' },
  { value: 'inactive', label: 'Inactive' },
];

// ── Draft state type ──────────────────────────────────────────────────────────

interface BlueparkDraftState {
  name: string;
  sku: string;
  description: string;
  price: string;
  quantity: number;
  weight: string;
  barcode: string;
  brand: string;
  images: string[];
  status: string;
  condition: string;
}

const EMPTY_DRAFT: BlueparkDraftState = {
  name: '',
  sku: '',
  description: '',
  price: '',
  quantity: 0,
  weight: '',
  barcode: '',
  brand: '',
  images: [],
  status: 'active',
  condition: 'new',
};

// ── Component ─────────────────────────────────────────────────────────────────

export default function BlueparkListingCreate() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const productId = searchParams.get('product_id');
  const credentialId = searchParams.get('credential_id') || '';

  // ── State ──────────────────────────────────────────────────────────────────

  const [loading, setLoading] = useState(true);
  const [aiGenerating, setAiGenerating] = useState(false);
  const [aiApplied, setAiApplied] = useState(false);
  const [aiError, setAiError] = useState('');
  const [error, setError] = useState('');
  const [draft, setDraft] = useState<BlueparkDraftState>(EMPTY_DRAFT);
  const [submitting, setSubmitting] = useState(false);
  const [submitResult, setSubmitResult] = useState<{
    ok: boolean;
    product_id?: string;
    message?: string;
    error?: string;
  } | null>(null);

  // ── Configurator (CFG-07) ──────────────────────────────────────────────────

  const [_selectedConfigurator, setSelectedConfigurator] = useState<ConfiguratorDetail | null>(null);

  // VAR-01 — Variation listings (mimicked: one Bluepark product per variant)
  const [variants, setVariants] = useState<ChannelVariantDraft[]>([]);
  const [isVariantProduct, setIsVariantProduct] = useState(false);
  const [variantSubmitResults, setVariantSubmitResults] = useState<{ sku: string; productId?: string; error?: string }[]>([]);
  const updateVariant = (id: string, field: keyof ChannelVariantDraft, value: any) => {
    setVariants(vs => vs.map(v => v.id === id ? { ...v, [field]: value } : v));
  };

  function handleConfiguratorSelect(cfg: ConfiguratorDetail | null) {
    setSelectedConfigurator(cfg);
    if (!cfg) return;
    if (cfg.shipping_defaults?.condition) {
      updateDraft('condition', cfg.shipping_defaults.condition);
    }
  }

  // ── updateDraft helper ─────────────────────────────────────────────────────

  function updateDraft<K extends keyof BlueparkDraftState>(key: K, value: BlueparkDraftState[K]) {
    setDraft((prev) => ({ ...prev, [key]: value }));
  }

  // ── Load draft on mount ────────────────────────────────────────────────────

  useEffect(() => {
    if (!productId) {
      setError('No product_id provided. Please open this page from a product.');
      setLoading(false);
      return;
    }
    loadDraft();
  }, [productId]);

  async function loadDraft() {
    setLoading(true);
    setError('');
    try {
      const res = await blueparkApi.prepareDraft(productId!, credentialId || undefined);
      if (res.data?.ok && res.data.draft) {
        const d = res.data.draft;
        setDraft({
          name: d.name || '',
          sku: d.sku || '',
          description: d.description || '',
          price: d.price != null ? String(d.price) : '',
          quantity: d.quantity ?? 0,
          weight: d.weight != null ? String(d.weight) : '',
          barcode: d.barcode || '',
          brand: d.brand || '',
          images: d.images || [],
          status: d.status || 'active',
          condition: d.condition || 'new',
        });
        // VAR-01: load variants
        if (d.variants && d.variants.length > 0) {
          setVariants(d.variants);
          setIsVariantProduct(true);
        }
      } else {
        setError(res.data?.error || 'Could not load product data. Make sure a Bluepark credential is connected.');
      }


      const aiFlag = searchParams.get('ai');
      if (aiFlag === 'pending' && productId) {
        setAiGenerating(true);
        try {
          const { aiService: aiApi } = await import('../../services/ai-api');
          const schemaFields = [
            { name: 'title', display_name: 'Title', data_type: 'string', required: true, allowed_values: [], max_length: 0 },
            { name: 'description', display_name: 'Description', data_type: 'string', required: true, allowed_values: [], max_length: 0 }
          ];
          const aiRes = await aiApi.generateWithSchema({
            product_id: productId,
            channel: 'bluepark',
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
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Failed to load Bluepark listing data';
      setError(msg);
    } finally {
      setLoading(false);
    }
  }

  // ── canSubmit ──────────────────────────────────────────────────────────────

  const canSubmit = useMemo(() => {
    return (
      draft.name.trim().length > 0 &&
      draft.price !== '' &&
      !isNaN(parseFloat(draft.price)) &&
      parseFloat(draft.price) >= 0
    );
  }, [draft]);

  // ── handleSubmit ───────────────────────────────────────────────────────────

  async function handleSubmit() {
    if (!canSubmit) return;

    setSubmitting(true);
    setSubmitResult(null);
    setError('');
    setVariantSubmitResults([]);

    // VAR-01: mimicked variations — one Bluepark product per active variant
    const activeVariants = variants.filter(v => v.active);
    if (isVariantProduct && activeVariants.length >= 2) {
      const results: { sku: string; productId?: string; error?: string }[] = [];
      for (const v of activeVariants) {
        const price = parseFloat(v.price) > 0 ? parseFloat(v.price) : parseFloat(draft.price);
        const qty = parseInt(v.stock) >= 0 ? parseInt(v.stock) : draft.quantity;
        const label = Object.values(v.combination).join(' / ');
        try {
          const payload: BlueparkSubmitPayload = {
            name: draft.name.trim() + (label ? ` - ${label}` : ''),
            sku: v.sku.trim() || undefined,
            description: draft.description.trim() || undefined,
            price,
            quantity: qty,
            weight: draft.weight ? parseFloat(draft.weight) : undefined,
            barcode: v.ean.trim() || draft.barcode.trim() || undefined,
            brand: draft.brand.trim() || undefined,
            images: draft.images.length > 0 ? draft.images : undefined,
            status: draft.status,
            condition: draft.condition,
            credential_id: credentialId || undefined,
          };
          const res = await blueparkApi.submit(payload);
          if (res.data?.ok) {
            results.push({ sku: v.sku, productId: res.data.product_id });
          } else {
            results.push({ sku: v.sku, error: res.data?.error || 'Failed' });
          }
        } catch (err: any) {
          results.push({ sku: v.sku, error: err?.response?.data?.error || err.message || 'Submission failed' });
        }
      }
      setVariantSubmitResults(results);
      setSubmitting(false);
      return;
    }

    try {
      const payload: BlueparkSubmitPayload = {
        name: draft.name.trim(),
        sku: draft.sku.trim() || undefined,
        description: draft.description.trim() || undefined,
        price: parseFloat(draft.price),
        quantity: draft.quantity,
        weight: draft.weight ? parseFloat(draft.weight) : undefined,
        barcode: draft.barcode.trim() || undefined,
        brand: draft.brand.trim() || undefined,
        images: draft.images.length > 0 ? draft.images : undefined,
        status: draft.status,
        condition: draft.condition,
        credential_id: credentialId || undefined,
      };

      const res = await blueparkApi.submit(payload);
      if (res.data?.ok) {
        setSubmitResult({ ok: true, product_id: res.data.product_id, message: res.data.message });
      } else {
        setSubmitResult({ ok: false, error: res.data?.error || 'Submission failed' });
      }
    } catch (err: unknown) {
      const axiosErr = err as { response?: { data?: { error?: string } }; message?: string };
      setSubmitResult({
        ok: false,
        error: axiosErr.response?.data?.error || axiosErr.message || 'Submission failed',
      });
    } finally {
      setSubmitting(false);
    }
  }

  // ── Requirements checklist ─────────────────────────────────────────────────

  const requirements = [
    { label: 'Product Name', met: draft.name.trim().length > 0 },
    { label: 'Price set', met: draft.price !== '' && !isNaN(parseFloat(draft.price)) && parseFloat(draft.price) >= 0 },
    { label: 'Quantity set', met: draft.quantity >= 0 },
    { label: 'SKU (recommended)', met: draft.sku.trim().length > 0 },
  ];

  // ── Render: loading ────────────────────────────────────────────────────────

  if (loading) {
    return (
      <div className="page-container">
        <div className="flex items-center justify-center min-h-[300px]">
          <div className="flex items-center gap-3 text-[var(--text-muted)]">
            <i className="ri-loader-4-line text-2xl animate-spin" />
            <span>Loading product data…</span>
          </div>
        </div>
      </div>
    );
  }

  // ── Render: success ────────────────────────────────────────────────────────

  if (submitResult?.ok) {
    return (
      <div className="page-container">
        <div className="flex flex-col items-center justify-center min-h-[400px] gap-4 text-center">
          <div style={{ fontSize: 56 }}>🛒</div>
          <h2 className="text-2xl font-semibold text-[var(--text-primary)]">
            Bluepark Product Created!
          </h2>
          {submitResult.product_id && (
            <p className="text-[var(--text-muted)]">
              Product ID: <span className="font-mono">{submitResult.product_id}</span>
            </p>
          )}
          {submitResult.message && (
            <p className="text-[var(--text-muted)] text-sm">{submitResult.message}</p>
          )}
          <div className="flex gap-3">
            <button className="btn btn-secondary" onClick={() => navigate(-1)}>
              ← Back to Product
            </button>
            <button className="btn btn-primary" onClick={() => navigate('/marketplace/listings')}>
              View All Listings
            </button>
          </div>
        </div>
      </div>
    );
  }

  // ── Render: main form ──────────────────────────────────────────────────────

  return (
    <div className="page-container">
      {/* Header */}
      <div className="page-header mb-6">
        <button onClick={() => navigate(-1)} className="btn btn-ghost flex items-center gap-2 mb-4">
          <i className="ri-arrow-left-line" />
          Back
        </button>
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <div
              className="w-10 h-10 rounded-lg flex items-center justify-center text-white font-bold text-sm"
              style={{ backgroundColor: BP_BLUE }}
            >
              BP
            </div>
            <div>
              <h1 className="text-2xl font-semibold text-[var(--text-primary)]">

                Create Bluepark Listing
              </h1>
              {productId && (
                <p className="text-sm text-[var(--text-muted)]">Product ID: {productId}</p>
              )}
            </div>
          </div>
          <button
            onClick={handleSubmit}
            disabled={submitting || !canSubmit}
            className="btn btn-primary flex items-center gap-2"
            style={{ background: BP_BLUE, borderColor: BP_BLUE, opacity: canSubmit ? 1 : 0.5 }}
          >
            {submitting ? (
              <>
                <i className="ri-loader-4-line animate-spin" />
                Creating…
              </>
            ) : (
              <>
                <i className="ri-add-line" />
                Create Product
              </>
            )}
          </button>
        </div>
      </div>

      {/* Error banners */}
      {error && (
        <div className="bg-red-500/10 border border-red-500/30 rounded-lg p-4 mb-6 flex items-start gap-3">
          <i className="ri-error-warning-line text-red-400 text-xl mt-0.5" />
          <p className="text-red-400">{error}</p>
        </div>
      )}
      {aiGenerating && (
        <div style={{ padding: '12px 14px', background: 'linear-gradient(90deg, rgba(139,92,246,0.15), rgba(59,130,246,0.15))', borderRadius: 8, fontSize: 13, color: 'var(--accent-purple)', marginBottom: 16, display: 'flex', gap: 10, alignItems: 'center', border: '1px solid rgba(139,92,246,0.3)' }}>
          <div className="spinner" style={{ width: 16, height: 16, border: '2px solid var(--accent-purple)', borderTopColor: 'transparent', borderRadius: '50%', animation: 'spin 1s linear infinite' }} />
          <span style={{ fontWeight: 600 }}>🤖 AI is generating optimised content for Bluepark...</span>
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
      {submitResult && !submitResult.ok && (
        <div className="bg-red-500/10 border border-red-500/30 rounded-lg p-4 mb-6 flex items-start gap-3">
          <i className="ri-error-warning-line text-red-400 text-xl mt-0.5" />
          <p className="text-red-400">{submitResult.error || 'Submission failed'}</p>
        </div>
      )}

      {/* Configurator (CFG-07) */}
      <ConfiguratorSelector channel="bluepark" credentialId={credentialId} onSelect={handleConfiguratorSelect} />

      {/* VAR-01 — Variant Grid (mimicked) */}
      {isVariantProduct && (
        <div className="card mb-4" style={{ borderLeft: '3px solid #d946ef' }}>
          <div className="card-header">
            <h3 className="card-title" style={{ color: '#d946ef' }}>Variants ({variants.length})</h3>
          </div>
          <div className="card-body">
            {/* Amber warning — mimicked channel */}
            <div style={{ background: 'rgba(245,158,11,0.1)', border: '1px solid rgba(245,158,11,0.3)', borderRadius: 8, padding: '10px 14px', marginBottom: 12, display: 'flex', gap: 8, alignItems: 'flex-start' }}>
              <i className="ri-git-branch-line" style={{ color: '#f59e0b', marginTop: 2 }} />
              <p style={{ fontSize: 12, color: '#f59e0b', margin: 0 }}>
                <strong>Mimicked variations</strong> — Bluepark does not support native grouped variants. Each active variant will be submitted as a separate Bluepark product with the combination label appended to the name. If a variant has an EAN it will be used as the product barcode.
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
                    <th style={{ padding: '6px 8px', textAlign: 'left', fontWeight: 600, color: 'var(--text-secondary)' }}>Price (£)</th>
                    <th style={{ padding: '6px 8px', textAlign: 'left', fontWeight: 600, color: 'var(--text-secondary)' }}>Stock</th>
                    <th style={{ padding: '6px 8px', textAlign: 'left', fontWeight: 600, color: 'var(--text-secondary)' }}>EAN (Barcode)</th>
                  </tr>
                </thead>
                <tbody>
                  {variants.map(v => (
                    <tr key={v.id} style={{ borderBottom: '1px solid var(--border)', opacity: v.active ? 1 : 0.45 }}>
                      <td style={{ padding: '5px 8px' }}>
                        <input type="checkbox" checked={v.active} onChange={e => updateVariant(v.id, 'active', e.target.checked)} />
                      </td>
                      {Object.values(v.combination).map((val, i) => (
                        <td key={i} style={{ padding: '5px 8px' }}>{val}</td>
                      ))}
                      <td style={{ padding: '5px 8px' }}>
                        <input value={v.sku} onChange={e => updateVariant(v.id, 'sku', e.target.value)}
                          style={{ padding: '3px 6px', fontSize: 11, width: 100, borderRadius: 5, border: '1px solid var(--border)', background: 'var(--bg-primary)', color: 'var(--text-primary)' }} />
                      </td>
                      <td style={{ padding: '5px 8px' }}>
                        <input value={v.price} onChange={e => updateVariant(v.id, 'price', e.target.value)} type="number" step="0.01"
                          style={{ padding: '3px 6px', fontSize: 11, width: 70, borderRadius: 5, border: '1px solid var(--border)', background: 'var(--bg-primary)', color: 'var(--text-primary)' }} />
                      </td>
                      <td style={{ padding: '5px 8px' }}>
                        <input value={v.stock} onChange={e => updateVariant(v.id, 'stock', e.target.value)} type="number"
                          style={{ padding: '3px 6px', fontSize: 11, width: 50, borderRadius: 5, border: '1px solid var(--border)', background: 'var(--bg-primary)', color: 'var(--text-primary)' }} />
                      </td>
                      <td style={{ padding: '5px 8px' }}>
                        <input value={v.ean} onChange={e => updateVariant(v.id, 'ean', e.target.value)}
                          style={{ padding: '3px 6px', fontSize: 11, width: 110, borderRadius: 5, border: '1px solid var(--border)', background: 'var(--bg-primary)', color: 'var(--text-primary)' }} />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {/* Per-variant submission results */}
            {variantSubmitResults.length > 0 && (
              <div style={{ marginTop: 12 }}>
                {variantSubmitResults.map((r, i) => (
                  <div key={i} style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 12, marginBottom: 4 }}>
                    {r.error
                      ? <><i className="ri-close-circle-line" style={{ color: 'var(--danger)' }} /><span style={{ color: 'var(--danger)' }}>{r.sku}: {r.error}</span></>
                      : <><i className="ri-checkbox-circle-line" style={{ color: 'var(--success)' }} /><span style={{ color: 'var(--success)' }}>{r.sku}{r.productId ? ` — ID: ${r.productId}` : ''}</span></>
                    }
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      )}

      {/* Platform notice */}
      <div
        className="rounded-lg p-4 mb-6 text-sm"
        style={{ background: `${BP_BLUE}15`, borderLeft: `3px solid ${BP_BLUE}` }}
      >
        <p className="font-medium text-[var(--text-primary)] mb-1">
          <i className="ri-information-line mr-1" />
          Bluepark — UK E-Commerce Platform
        </p>
        <p className="text-[var(--text-muted)]">
          Products are listed directly into your Bluepark store via REST API. Ensure your API key has
          product write permissions in your Bluepark admin panel.
        </p>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* ── Main column ──────────────────────────────────────────────────── */}
        <div className="lg:col-span-2 space-y-6">
          {/* Identity */}
          <div className="card p-6">
            <h2 className="text-lg font-semibold text-[var(--text-primary)] mb-4">
              Product Identity
            </h2>
            <div className="space-y-4">
              <div>
                <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
                  Product Name <span className="text-red-400">*</span>
                </label>
                <input
                  type="text"
                  value={draft.name}
                  onChange={(e) => updateDraft('name', e.target.value)}
                  placeholder="Enter product name"
                  className="input w-full"
                  maxLength={255}
                />
                <p className="text-xs text-[var(--text-muted)] mt-1">{draft.name.length}/255</p>
              </div>

              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
                    SKU
                  </label>
                  <input
                    type="text"
                    value={draft.sku}
                    onChange={(e) => updateDraft('sku', e.target.value)}
                    placeholder="Stock keeping unit"
                    className="input w-full"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
                    Barcode / EAN
                  </label>
                  <input
                    type="text"
                    value={draft.barcode}
                    onChange={(e) => updateDraft('barcode', e.target.value)}
                    placeholder="EAN, UPC, or ISBN"
                    className="input w-full"
                  />
                </div>
              </div>

              <div>
                <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
                  Brand
                </label>
                <input
                  type="text"
                  value={draft.brand}
                  onChange={(e) => updateDraft('brand', e.target.value)}
                  placeholder="Brand or manufacturer name"
                  className="input w-full"
                />
              </div>
            </div>
          </div>

          {/* Description */}
          <div className="card p-6">
            <h2 className="text-lg font-semibold text-[var(--text-primary)] mb-4">Description</h2>
            <textarea
              rows={8}
              value={draft.description}
              onChange={(e) => updateDraft('description', e.target.value)}
              className="input w-full resize-y"
              placeholder="Product description (HTML supported)…"
              style={{ fontFamily: 'var(--font-mono, monospace)', fontSize: 13 }}
            />
          </div>

          {/* Pricing & Inventory */}
          <div className="card p-6">
            <h2 className="text-lg font-semibold text-[var(--text-primary)] mb-4">
              Pricing &amp; Inventory
            </h2>
            <div className="grid grid-cols-2 gap-4">
              <div>
                <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
                  Price <span className="text-red-400">*</span>
                </label>
                <div className="relative">
                  <span className="absolute left-3 top-1/2 -translate-y-1/2 text-[var(--text-muted)]">
                    £
                  </span>
                  <input
                    type="number"
                    step="0.01"
                    min="0"
                    value={draft.price}
                    onChange={(e) => updateDraft('price', e.target.value)}
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
                  value={draft.quantity}
                  onChange={(e) => updateDraft('quantity', parseInt(e.target.value) || 0)}
                  className="input w-full"
                />
              </div>
            </div>
          </div>

          {/* Shipping */}
          <div className="card p-6">
            <h2 className="text-lg font-semibold text-[var(--text-primary)] mb-4">Shipping</h2>
            <div style={{ maxWidth: 240 }}>
              <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
                Weight (kg)
              </label>
              <input
                type="number"
                step="0.001"
                min="0"
                value={draft.weight}
                onChange={(e) => updateDraft('weight', e.target.value)}
                className="input w-full"
                placeholder="0.000"
              />
              <p className="text-xs text-[var(--text-muted)] mt-1">
                Used to calculate shipping rates in Bluepark.
              </p>
            </div>
          </div>

          {/* Images */}
          <div className="card p-6">
            <h2 className="text-lg font-semibold text-[var(--text-primary)] mb-4">Images</h2>
            {draft.images.length === 0 ? (
              <p className="text-sm text-[var(--text-muted)]">
                No images loaded from MarketMate product.
              </p>
            ) : (
              <div className="image-grid mb-3">
                {draft.images.map((src, i) => (
                  <div key={i} className="image-thumb-wrap">
                    <img src={src} alt="" className="image-thumb" />
                    {i === 0 && <span className="image-badge">Main</span>}
                    <button
                      className="image-remove-btn"
                      onClick={() => updateDraft('images', draft.images.filter((_, idx) => idx !== i))}
                      title="Remove"
                    >
                      <i className="ri-close-line" />
                    </button>
                  </div>
                ))}
              </div>
            )}
            <p className="text-xs text-[var(--text-muted)]">
              Images are sent as URLs. Additional images can be managed in your Bluepark admin panel.
            </p>
          </div>
        </div>

        {/* ── Sidebar ──────────────────────────────────────────────────────── */}
        <div className="space-y-6">
          {/* Requirements checklist */}
          <div className="card p-5">
            <h3 className="font-semibold text-[var(--text-primary)] mb-3">Requirements</h3>
            <ul className="space-y-2">
              {requirements.map((r, i) => (
                <li key={i} className="flex items-center gap-2 text-sm">
                  <i
                    className={`text-base ${
                      r.met
                        ? 'ri-checkbox-circle-fill text-green-400'
                        : 'ri-checkbox-blank-circle-line text-[var(--text-muted)]'
                    }`}
                  />
                  <span className={r.met ? 'text-[var(--text-primary)]' : 'text-[var(--text-muted)]'}>
                    {r.label}
                  </span>
                </li>
              ))}
            </ul>
          </div>

          {/* Product configuration */}
          <div className="card p-5">
            <h3 className="font-semibold text-[var(--text-primary)] mb-3">Configuration</h3>
            <div className="space-y-4">
              <div>
                <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
                  Status
                </label>
                <select
                  className="input w-full"
                  value={draft.status}
                  onChange={(e) => updateDraft('status', e.target.value)}
                >
                  {STATUS_OPTIONS.map((opt) => (
                    <option key={opt.value} value={opt.value}>
                      {opt.label}
                    </option>
                  ))}
                </select>
              </div>
              <div>
                <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
                  Condition
                </label>
                <select
                  className="input w-full"
                  value={draft.condition}
                  onChange={(e) => updateDraft('condition', e.target.value)}
                >
                  {CONDITION_OPTIONS.map((opt) => (
                    <option key={opt.value} value={opt.value}>
                      {opt.label}
                    </option>
                  ))}
                </select>
              </div>
            </div>
          </div>

          {/* Bluepark notes */}
          <div className="card p-5">
            <h3 className="font-semibold text-[var(--text-primary)] mb-3">Bluepark Notes</h3>
            <ul className="space-y-2 text-sm text-[var(--text-muted)]">
              <li className="flex items-start gap-2">
                <i className="ri-information-line text-[var(--primary)] mt-0.5" />
                Bluepark is a UK-based e-commerce platform. Orders are fulfilled from your Bluepark store.
              </li>
              <li className="flex items-start gap-2">
                <i className="ri-information-line text-[var(--primary)] mt-0.5" />
                Your API key must have product write permissions. Check your Bluepark admin → API settings.
              </li>
              <li className="flex items-start gap-2">
                <i className="ri-information-line text-[var(--primary)] mt-0.5" />
                Import orders via the Orders page → Import → Bluepark.
              </li>
            </ul>
          </div>

          {/* Submit button */}
          <button
            onClick={handleSubmit}
            disabled={submitting || !canSubmit || (isVariantProduct && variantSubmitResults.length > 0)}
            className="btn btn-primary w-full flex items-center justify-center gap-2 py-3"
            style={{ background: BP_BLUE, borderColor: BP_BLUE, opacity: canSubmit ? 1 : 0.5 }}
          >
            {submitting ? (
              <>
                <i className="ri-loader-4-line animate-spin" />
                {isVariantProduct ? `Creating ${variants.filter(v => v.active).length} Products…` : 'Creating Product…'}
              </>
            ) : variantSubmitResults.length > 0 ? (
              <>
                <i className="ri-checkbox-circle-line" />
                Products Created
              </>
            ) : (
              <>
                <i className="ri-add-line" />
                {isVariantProduct ? `Create ${variants.filter(v => v.active).length} Bluepark Products` : 'Create Bluepark Product'}
              </>
            )}
          </button>
        </div>
      </div>
    </div>
  );
}

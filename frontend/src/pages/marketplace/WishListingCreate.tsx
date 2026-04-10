// ============================================================================
// WISH LISTING CREATE PAGE
// ============================================================================
// Location: frontend/src/pages/marketplace/WishListingCreate.tsx
//
// Arrives with ?product_id=xxx&credential_id=yyy from the product edit page.
// Pattern: useSearchParams → prepare → draft state → updateDraft → handleSubmit.
// Follows the same structure as OnBuyListingCreate.tsx / BlueparkListingCreate.tsx.
//
// Note: Wish is a global marketplace in decline but remains a required integration.
// Wish products require at least one variant. The default variant is pre-filled
// from PIM data and can be edited before submission.
// ============================================================================

import { useState, useEffect, useMemo } from 'react';
import { useSearchParams, useNavigate } from 'react-router-dom';
import { wishApi, WishVariant, WishSubmitPayload } from '../../services/wish-api';
import ConfiguratorSelector from './ConfiguratorSelector';
import { ConfiguratorDetail } from '../../services/configurator-api';

// Wish brand colour
const WISH_BLUE = '#2D6BE4';

// ── Draft state type ──────────────────────────────────────────────────────────

interface WishDraftState {
  name: string;
  description: string;
  sku: string;
  price: string;
  shipping: string;
  inventory: number;
  weight: string;       // grams
  brand: string;
  mainImage: string;
  extraImages: string[];
  tags: string;
  enabled: boolean;
  // Default variant mirrors top-level fields
  variantSku: string;
  variantPrice: string;
  variantShipping: string;
  variantInventory: number;
  variantWeight: string;
  variantLandedCost: string;
}

const EMPTY_DRAFT: WishDraftState = {
  name: '',
  description: '',
  sku: '',
  price: '',
  shipping: '0',
  inventory: 0,
  weight: '',
  brand: '',
  mainImage: '',
  extraImages: [],
  tags: '',
  enabled: true,
  variantSku: '',
  variantPrice: '',
  variantShipping: '0',
  variantInventory: 0,
  variantWeight: '',
  variantLandedCost: '',
};

// ── Component ─────────────────────────────────────────────────────────────────

export default function WishListingCreate() {
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
  const [draft, setDraft] = useState<WishDraftState>(EMPTY_DRAFT);
  const [draftNote, setDraftNote] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [submitResult, setSubmitResult] = useState<{
    ok: boolean;
    product_id?: string;
    message?: string;
    error?: string;
  } | null>(null);

  // ── Configurator (CFG-07) ──────────────────────────────────────────────────

  const [_selectedConfigurator, setSelectedConfigurator] = useState<ConfiguratorDetail | null>(null);

  function handleConfiguratorSelect(cfg: ConfiguratorDetail | null) {
    setSelectedConfigurator(cfg);
    if (!cfg) return;
    if (cfg.shipping_defaults?.brand) {
      updateDraft('brand', cfg.shipping_defaults.brand);
    }
  }

  // ── updateDraft helper ─────────────────────────────────────────────────────

  function updateDraft<K extends keyof WishDraftState>(key: K, value: WishDraftState[K]) {
    setDraft((prev) => ({ ...prev, [key]: value }));
  }

  // ── Sync variant fields from top-level when they're empty ─────────────────

  function syncVariant() {
    setDraft((prev) => ({
      ...prev,
      variantSku: prev.variantSku || prev.sku,
      variantPrice: prev.variantPrice || prev.price,
      variantShipping: prev.variantShipping || prev.shipping,
      variantInventory: prev.variantInventory || prev.inventory,
      variantWeight: prev.variantWeight || prev.weight,
      variantLandedCost: prev.variantLandedCost || prev.price,
    }));
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
      const res = await wishApi.prepareDraft(productId!, credentialId || undefined);
      if (res.data?.ok && res.data.draft) {
        const d = res.data.draft;
        const price = d.price != null ? String(d.price) : '';
        const shipping = d.shipping != null ? String(d.shipping) : '0';
        const weight = d.weight != null ? String(d.weight) : '';
        const mainImg = d.main_image || (d.extra_images?.[0] ?? '');
        const extraImgs = d.extra_images?.filter((img) => img !== mainImg) ?? [];

        setDraft({
          name: d.name || '',
          description: d.description || '',
          sku: d.sku || '',
          price,
          shipping,
          inventory: d.inventory ?? 0,
          weight,
          brand: d.brand || '',
          mainImage: mainImg,
          extraImages: extraImgs,
          tags: d.tags || '',
          enabled: d.enabled ?? true,
          // Variant mirrors top-level
          variantSku: d.variants?.[0]?.sku || d.sku || '',
          variantPrice: d.variants?.[0]?.price != null ? String(d.variants[0].price) : price,
          variantShipping: d.variants?.[0]?.shipping != null ? String(d.variants[0].shipping) : shipping,
          variantInventory: d.variants?.[0]?.inventory ?? d.inventory ?? 0,
          variantWeight: d.variants?.[0]?.weight != null ? String(d.variants[0].weight) : weight,
          variantLandedCost: d.variants?.[0]?.landed_cost != null ? String(d.variants[0].landed_cost) : price,
        });
        if (res.data.note) {
          setDraftNote(res.data.note);
        }
      } else {
        setError(res.data?.error || 'Could not load product data. Make sure a Wish credential is connected.');
      }


      const aiFlag = searchParams.get('ai');
      if (aiFlag === 'pending' && productId) {
        setAiGenerating(true);
        try {
          const { aiService: aiApi } = await import('../../services/ai-api');
          const schemaFields = [
            { name: 'title', display_name: 'Title', data_type: 'string', required: true, allowed_values: [], max_length: 100 },
            { name: 'description', display_name: 'Description', data_type: 'string', required: true, allowed_values: [], max_length: 0 },
            { name: 'tags', display_name: 'Tags', data_type: 'string', required: false, allowed_values: [], max_length: 0 }
          ];
          const aiRes = await aiApi.generateWithSchema({
            product_id: productId,
            channel: 'wish',
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
      const msg = err instanceof Error ? err.message : 'Failed to load Wish listing data';
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
      parseFloat(draft.price) > 0 &&
      draft.mainImage.trim().length > 0
    );
  }, [draft]);

  // ── handleSubmit ───────────────────────────────────────────────────────────

  async function handleSubmit() {
    if (!canSubmit) return;

    setSubmitting(true);
    setSubmitResult(null);
    setError('');

    const variant: WishVariant = {
      sku: draft.variantSku.trim() || draft.sku.trim(),
      price: parseFloat(draft.variantPrice || draft.price),
      shipping: parseFloat(draft.variantShipping || draft.shipping || '0'),
      inventory: draft.variantInventory,
      weight: parseFloat(draft.variantWeight || draft.weight || '0'),
      landed_cost: parseFloat(draft.variantLandedCost || draft.variantPrice || draft.price),
      main_image: draft.mainImage.trim(),
      enabled: draft.enabled,
    };

    try {
      const allImages = [
        ...(draft.mainImage ? [draft.mainImage] : []),
        ...draft.extraImages.filter(Boolean),
      ];

      const payload: WishSubmitPayload = {
        name: draft.name.trim(),
        description: draft.description.trim() || undefined,
        sku: draft.sku.trim() || undefined,
        price: parseFloat(draft.price),
        shipping: parseFloat(draft.shipping || '0'),
        inventory: draft.inventory,
        weight: draft.weight ? parseFloat(draft.weight) : undefined,
        brand: draft.brand.trim() || undefined,
        main_image: draft.mainImage.trim(),
        extra_images: allImages.length > 1 ? allImages.slice(1) : undefined,
        tags: draft.tags.trim() || undefined,
        enabled: draft.enabled,
        variants: [variant],
        credential_id: credentialId || undefined,
      };

      const res = await wishApi.submit(payload);
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
    { label: 'Price > 0', met: draft.price !== '' && parseFloat(draft.price) > 0 },
    { label: 'Main image URL', met: draft.mainImage.trim().length > 0 },
    { label: 'Inventory set', met: draft.inventory >= 0 },
    { label: 'Variant configured', met: draft.variantPrice !== '' && parseFloat(draft.variantPrice || draft.price) > 0 },
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
          <div style={{ fontSize: 56 }}>🛍️</div>
          <h2 className="text-2xl font-semibold text-[var(--text-primary)]">
            Wish Product Created!
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
              style={{ backgroundColor: WISH_BLUE }}
            >
              W
            </div>
            <div>
              <h1 className="text-2xl font-semibold text-[var(--text-primary)]">

                Create Wish Listing
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
            style={{ background: WISH_BLUE, borderColor: WISH_BLUE, opacity: canSubmit ? 1 : 0.5 }}
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
          <span style={{ fontWeight: 600 }}>🤖 AI is generating optimised content for Wish...</span>
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
      <ConfiguratorSelector channel="wish" credentialId={credentialId} onSelect={handleConfiguratorSelect} />

      {/* Platform notice */}
      {draftNote && (
        <div
          className="rounded-lg p-4 mb-6 text-sm"
          style={{ background: `${WISH_BLUE}15`, borderLeft: `3px solid ${WISH_BLUE}` }}
        >
          <p className="text-[var(--text-muted)]">
            <i className="ri-information-line mr-1" />
            {draftNote}
          </p>
        </div>
      )}

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
                    Brand
                  </label>
                  <input
                    type="text"
                    value={draft.brand}
                    onChange={(e) => updateDraft('brand', e.target.value)}
                    placeholder="Brand or manufacturer"
                    className="input w-full"
                  />
                </div>
              </div>

              <div>
                <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
                  Tags
                </label>
                <input
                  type="text"
                  value={draft.tags}
                  onChange={(e) => updateDraft('tags', e.target.value)}
                  placeholder="Comma-separated tags (e.g. electronics, gadgets)"
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
              placeholder="Product description…"
            />
          </div>

          {/* Pricing & Inventory */}
          <div className="card p-6">
            <h2 className="text-lg font-semibold text-[var(--text-primary)] mb-4">
              Pricing &amp; Inventory
            </h2>
            <div className="grid grid-cols-3 gap-4">
              <div>
                <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
                  Price <span className="text-red-400">*</span>
                </label>
                <div className="relative">
                  <span className="absolute left-3 top-1/2 -translate-y-1/2 text-[var(--text-muted)]">
                    $
                  </span>
                  <input
                    type="number"
                    step="0.01"
                    min="0.01"
                    value={draft.price}
                    onChange={(e) => updateDraft('price', e.target.value)}
                    className="input w-full pl-6"
                    placeholder="0.00"
                  />
                </div>
              </div>
              <div>
                <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
                  Shipping
                </label>
                <div className="relative">
                  <span className="absolute left-3 top-1/2 -translate-y-1/2 text-[var(--text-muted)]">
                    $
                  </span>
                  <input
                    type="number"
                    step="0.01"
                    min="0"
                    value={draft.shipping}
                    onChange={(e) => updateDraft('shipping', e.target.value)}
                    className="input w-full pl-6"
                    placeholder="0.00"
                  />
                </div>
              </div>
              <div>
                <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
                  Inventory
                </label>
                <input
                  type="number"
                  min="0"
                  value={draft.inventory}
                  onChange={(e) => updateDraft('inventory', parseInt(e.target.value) || 0)}
                  className="input w-full"
                />
              </div>
            </div>
            <div className="mt-4" style={{ maxWidth: 200 }}>
              <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
                Weight (grams)
              </label>
              <input
                type="number"
                step="1"
                min="0"
                value={draft.weight}
                onChange={(e) => updateDraft('weight', e.target.value)}
                className="input w-full"
                placeholder="0"
              />
              <p className="text-xs text-[var(--text-muted)] mt-1">
                Weight in grams (Wish requirement).
              </p>
            </div>
          </div>

          {/* Images */}
          <div className="card p-6">
            <h2 className="text-lg font-semibold text-[var(--text-primary)] mb-4">
              Images <span className="text-red-400 text-base">*</span>
            </h2>
            <div>
              <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
                Main Image URL <span className="text-red-400">*</span>
              </label>
              <input
                type="url"
                value={draft.mainImage}
                onChange={(e) => updateDraft('mainImage', e.target.value)}
                placeholder="https://…"
                className="input w-full"
              />
              {draft.mainImage && (
                <img
                  src={draft.mainImage}
                  alt="Main"
                  className="mt-2 rounded object-cover"
                  style={{ width: 80, height: 80 }}
                  onError={(e) => { (e.target as HTMLImageElement).style.display = 'none'; }}
                />
              )}
            </div>
            {draft.extraImages.length > 0 && (
              <div className="mt-4">
                <p className="text-sm font-medium text-[var(--text-primary)] mb-2">Extra Images</p>
                <div className="image-grid">
                  {draft.extraImages.map((src, i) => (
                    <div key={i} className="image-thumb-wrap">
                      <img src={src} alt="" className="image-thumb" />
                      <button
                        className="image-remove-btn"
                        onClick={() =>
                          updateDraft('extraImages', draft.extraImages.filter((_, idx) => idx !== i))
                        }
                        title="Remove"
                      >
                        <i className="ri-close-line" />
                      </button>
                    </div>
                  ))}
                </div>
              </div>
            )}
            <p className="text-xs text-[var(--text-muted)] mt-3">
              Wish requires a main image URL. Up to 10 images total.
            </p>
          </div>

          {/* Default Variant */}
          <div className="card p-6">
            <h2 className="text-lg font-semibold text-[var(--text-primary)] mb-1">
              Default Variant
            </h2>
            <p className="text-sm text-[var(--text-muted)] mb-4">
              Wish requires at least one variant. This mirrors the product fields above — only change these
              if the variant differs from the parent product.
            </p>
            <div className="grid grid-cols-2 gap-4">
              <div>
                <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
                  Variant SKU
                </label>
                <input
                  type="text"
                  value={draft.variantSku}
                  onChange={(e) => updateDraft('variantSku', e.target.value)}
                  placeholder={draft.sku || 'Variant SKU'}
                  className="input w-full"
                  onFocus={syncVariant}
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
                  Variant Price ($)
                </label>
                <input
                  type="number"
                  step="0.01"
                  min="0.01"
                  value={draft.variantPrice}
                  onChange={(e) => updateDraft('variantPrice', e.target.value)}
                  placeholder={draft.price || '0.00'}
                  className="input w-full"
                  onFocus={syncVariant}
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
                  Variant Shipping ($)
                </label>
                <input
                  type="number"
                  step="0.01"
                  min="0"
                  value={draft.variantShipping}
                  onChange={(e) => updateDraft('variantShipping', e.target.value)}
                  placeholder="0.00"
                  className="input w-full"
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
                  Variant Inventory
                </label>
                <input
                  type="number"
                  min="0"
                  value={draft.variantInventory}
                  onChange={(e) => updateDraft('variantInventory', parseInt(e.target.value) || 0)}
                  className="input w-full"
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
                  Variant Weight (g)
                </label>
                <input
                  type="number"
                  step="1"
                  min="0"
                  value={draft.variantWeight}
                  onChange={(e) => updateDraft('variantWeight', e.target.value)}
                  placeholder={draft.weight || '0'}
                  className="input w-full"
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
                  Landed Cost ($)
                </label>
                <input
                  type="number"
                  step="0.01"
                  min="0"
                  value={draft.variantLandedCost}
                  onChange={(e) => updateDraft('variantLandedCost', e.target.value)}
                  placeholder={draft.price || '0.00'}
                  className="input w-full"
                />
                <p className="text-xs text-[var(--text-muted)] mt-1">
                  Price + shipping, inclusive of all costs to buyer.
                </p>
              </div>
            </div>
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

          {/* Listing status */}
          <div className="card p-5">
            <h3 className="font-semibold text-[var(--text-primary)] mb-3">Listing Status</h3>
            <label className="flex items-center gap-3 cursor-pointer">
              <div
                onClick={() => updateDraft('enabled', !draft.enabled)}
                style={{
                  width: 40,
                  height: 22,
                  borderRadius: 11,
                  background: draft.enabled ? WISH_BLUE : 'var(--border)',
                  position: 'relative',
                  cursor: 'pointer',
                  transition: 'background 0.2s',
                  flexShrink: 0,
                }}
              >
                <div
                  style={{
                    position: 'absolute',
                    top: 3,
                    left: draft.enabled ? 21 : 3,
                    width: 16,
                    height: 16,
                    borderRadius: '50%',
                    background: '#fff',
                    transition: 'left 0.2s',
                    boxShadow: '0 1px 3px rgba(0,0,0,0.3)',
                  }}
                />
              </div>
              <span className="text-sm text-[var(--text-primary)]">
                {draft.enabled ? 'Enabled (live)' : 'Disabled'}
              </span>
            </label>
          </div>

          {/* Wish notes */}
          <div className="card p-5">
            <h3 className="font-semibold text-[var(--text-primary)] mb-3">Wish Notes</h3>
            <ul className="space-y-2 text-sm text-[var(--text-muted)]">
              <li className="flex items-start gap-2">
                <i className="ri-information-line text-[var(--primary)] mt-0.5" />
                Wish is a global marketplace. Prices are in USD by default.
              </li>
              <li className="flex items-start gap-2">
                <i className="ri-information-line text-[var(--primary)] mt-0.5" />
                Every Wish product must have at least one variant. The default variant has been pre-filled from your PIM data.
              </li>
              <li className="flex items-start gap-2">
                <i className="ri-information-line text-[var(--primary)] mt-0.5" />
                A <strong>main image URL</strong> is mandatory — Wish rejects products without one.
              </li>
              <li className="flex items-start gap-2">
                <i className="ri-warning-line text-yellow-400 mt-0.5" />
                Weight must be in <strong>grams</strong> (not kg). MarketMate has converted automatically.
              </li>
            </ul>
          </div>

          {/* Submit button */}
          <button
            onClick={handleSubmit}
            disabled={submitting || !canSubmit}
            className="btn btn-primary w-full flex items-center justify-center gap-2 py-3"
            style={{ background: WISH_BLUE, borderColor: WISH_BLUE, opacity: canSubmit ? 1 : 0.5 }}
          >
            {submitting ? (
              <>
                <i className="ri-loader-4-line animate-spin" />
                Creating Product…
              </>
            ) : (
              <>
                <i className="ri-add-line" />
                Create Wish Product
              </>
            )}
          </button>
        </div>
      </div>
    </div>
  );
}

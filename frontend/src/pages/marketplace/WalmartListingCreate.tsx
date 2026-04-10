// ============================================================================
// WALMART LISTING CREATE PAGE
// ============================================================================
// Walmart uses feed-based item submission (MP_ITEM feed via JSON).
// The UI collects product data, submits a feed, then polls for feed status.
// Arrives with ?product_id=xxx from product edit page.
//
// Fields: product name, UPC/GTIN, category (Walmart taxonomy), price,
// quantity, short description, key features (bullet points), brand,
// model number, shipping weight, images.

import { useState, useEffect, useRef } from 'react';
import { useSearchParams, useNavigate } from 'react-router-dom';
import walmartApi, { WalmartFeedStatus } from '../../services/walmart-api';
import ConfiguratorSelector from './ConfiguratorSelector';
import { ConfiguratorDetail } from '../../services/configurator-api';
// Walmart top-level category options (mirrors backend GetCategories)
const WALMART_CATEGORIES = [
  { id: '3944',    name: 'Electronics' },
  { id: '3951',    name: 'Clothing, Shoes & Accessories' },
  { id: '5438',    name: 'Home & Garden' },
  { id: '976759',  name: 'Toys' },
  { id: '4044',    name: 'Sports & Outdoors' },
  { id: '1085666', name: 'Baby' },
  { id: '5429',    name: 'Health & Beauty' },
  { id: '5427',    name: 'Automotive' },
  { id: '4104',    name: 'Books' },
  { id: '3961',    name: 'Food' },
];

export default function WalmartListingCreate() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const productId   = searchParams.get('product_id');
  const credentialId = searchParams.get('credential_id') || '';

  // ── Form state ────────────────────────────────────────────────────────────
  const [loading, setLoading]             = useState(true);
  const [aiGenerating, setAiGenerating] = useState(false);
  const [aiApplied, setAiApplied] = useState(false);
  const [aiError, setAiError] = useState('');
  const [error, setError]                 = useState('');

  const [productName, setProductName]     = useState('');
  const [shortDescription, setShortDescription] = useState('');
  const [sku, setSku]                     = useState('');
  const [upc, setUpc]                     = useState('');
  const [brand, setBrand]                 = useState('');
  const [modelNumber, setModelNumber]     = useState('');
  const [price, setPrice]                 = useState('');
  const [quantity, setQuantity]           = useState<number>(0);
  const [category, setCategory]           = useState('');
  const [shippingWeight, setShippingWeight] = useState('');
  const [keyFeatures, setKeyFeatures]     = useState<string[]>(['', '', '', '', '']);
  const [images, setImages]               = useState<string[]>([]);

  // ── Feed submission state ─────────────────────────────────────────────────
  const [submitting, setSubmitting]       = useState(false);
  const [feedId, setFeedId]               = useState<string | null>(null);
  const [feedStatus, setFeedStatus]       = useState<WalmartFeedStatus | null>(null);
  const [submitError, setSubmitError]     = useState('');
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  // ── Configurator (CFG-07) ──
  // Note: Walmart uses feed-based submission and does not create a MarketMate
  // listing record immediately — configurator pre-populates fields only.
  const [_selectedConfigurator, setSelectedConfigurator] = useState<ConfiguratorDetail | null>(null);

  const handleConfiguratorSelect = (cfg: ConfiguratorDetail | null) => {
    setSelectedConfigurator(cfg);
    if (!cfg) return;
    // Pre-populate category
    if (cfg.category_path) setCategory(cfg.category_path);
    // Pre-populate attribute defaults (brand, model number etc)
    if (cfg.attribute_defaults) {
      for (const attr of cfg.attribute_defaults) {
        if (attr.source === 'default_value' && attr.default_value) {
          if (attr.attribute_name === 'brand') setBrand(attr.default_value);
          if (attr.attribute_name === 'model_number') setModelNumber(attr.default_value);
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
    return () => { if (pollRef.current) clearInterval(pollRef.current); };
  }, [productId]);

  async function init() {
    setLoading(true);
    setError('');
    try {
      const res = await walmartApi.prepareDraft(productId!, credentialId || undefined);
      if (res.data?.ok) {
        const d = res.data.draft;
        setProductName(d.product_name || '');
        setShortDescription(d.short_description || '');
        setSku(String(d.sku || ''));
        setPrice(String(d.price || ''));
        setQuantity(Number(d.quantity) || 0);
        setBrand(String(d.brand || ''));
        setShippingWeight(String(d.shipping_weight || ''));
        setImages(d.images || []);
      } else {
        setError('Could not load product data. Make sure a Walmart credential is connected.');
      }


      const aiFlag = searchParams.get('ai');
      if (aiFlag === 'pending' && productId) {
        setAiGenerating(true);
        try {
          const { aiService: aiApi } = await import('../../services/ai-api');
          const schemaFields = [
            { name: 'title', display_name: 'Title', data_type: 'string', required: true, allowed_values: [], max_length: 0 },
            { name: 'description', display_name: 'Description', data_type: 'string', required: true, allowed_values: [], max_length: 0 },
            { name: 'key_features', display_name: 'Key Features', data_type: 'string', required: false, allowed_values: [], max_length: 0 }
          ];
          const aiRes = await aiApi.generateWithSchema({
            product_id: productId,
            channel: 'walmart',
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
  }

  // ── Key features helpers ──────────────────────────────────────────────────
  function updateFeature(index: number, value: string) {
    const updated = [...keyFeatures];
    updated[index] = value;
    setKeyFeatures(updated);
  }

  function addFeature() {
    setKeyFeatures([...keyFeatures, '']);
  }

  function removeFeature(index: number) {
    setKeyFeatures(keyFeatures.filter((_, i) => i !== index));
  }

  // ── Feed polling ──────────────────────────────────────────────────────────
  function startPolling(id: string) {
    if (pollRef.current) clearInterval(pollRef.current);
    pollRef.current = setInterval(async () => {
      try {
        const res = await walmartApi.getFeedStatus(id, credentialId || undefined);
        if (res.data?.ok && res.data.status) {
          setFeedStatus(res.data.status);
          const s = res.data.status.feedStatus;
          if (s === 'PROCESSED' || s === 'ERROR') {
            if (pollRef.current) clearInterval(pollRef.current);
          }
        }
      } catch {
        // ignore poll errors
      }
    }, 5000);
  }

  // ── Submit ────────────────────────────────────────────────────────────────
  async function handleSubmit() {
    if (!productName.trim()) { setSubmitError('Product name is required.'); return; }
    if (!price || parseFloat(price) <= 0) { setSubmitError('A valid price is required.'); return; }

    setSubmitting(true);
    setSubmitError('');
    setFeedId(null);
    setFeedStatus(null);

    const features = keyFeatures.filter(f => f.trim() !== '');
    const mainImg  = images[0] || undefined;
    const addlImgs = images.slice(1);

    const itemPayload: Record<string, unknown> = {
      sku: sku || productName.toLowerCase().replace(/\s+/g, '-').slice(0, 50),
      productName: productName,
      price: {
        currency: 'USD',
        amount: parseFloat(price),
      },
    };
    if (shortDescription) itemPayload['shortDescription'] = shortDescription;
    if (upc)              itemPayload['upc'] = upc;
    if (brand)            itemPayload['brand'] = brand;
    if (modelNumber)      itemPayload['modelNumber'] = modelNumber;
    if (category)         itemPayload['category'] = category;
    if (shippingWeight)   itemPayload['ShippingWeight'] = { measure: shippingWeight, unit: 'LB' };
    if (features.length)  itemPayload['keyFeatures'] = features;
    if (mainImg)          itemPayload['mainImageUrl'] = mainImg;
    if (addlImgs.length)  itemPayload['additionalImageUrls'] = addlImgs;

    const feedPayload = {
      MPItemFeed: {
        MPItemFeedHeader: { version: '4.7' },
        MPItem: [itemPayload],
      },
    };

    try {
      const res = await walmartApi.submitItemFeed(feedPayload, credentialId || undefined);
      if (!res.data?.ok) {
        setSubmitError(res.data?.error || 'Feed submission failed');
        return;
      }
      const id = res.data.feed_id!;
      setFeedId(id);
      startPolling(id);
    } catch (err: any) {
      setSubmitError(err?.response?.data?.error || err.message || 'Feed submission failed');
    } finally {
      setSubmitting(false);
    }
  }

  // ── Feed status display ───────────────────────────────────────────────────
  function renderFeedStatus() {
    if (!feedId) return null;

    const statusColor: Record<string, string> = {
      RECEIVED:   '#3b82f6',
      INPROGRESS: '#f59e0b',
      PROCESSED:  '#22c55e',
      ERROR:      '#ef4444',
    };
    const statusLabel: Record<string, string> = {
      RECEIVED:   'Received by Walmart',
      INPROGRESS: 'Processing…',
      PROCESSED:  'Processed',
      ERROR:      'Error',
    };

    const status = feedStatus?.feedStatus;
    const color  = status ? statusColor[status] : '#6b7280';
    const label  = status ? statusLabel[status] : 'Checking…';

    return (
      <div className="card mb-4" style={{ borderLeft: `4px solid ${color}` }}>
        <div className="card-body">
          <div className="flex items-center justify-between mb-3">
            <div>
              <p className="font-semibold text-[var(--text-primary)]">Feed Submitted</p>
              <p className="text-xs text-[var(--text-muted)] font-mono mt-0.5">{feedId}</p>
            </div>
            <span
              className="text-sm font-medium px-3 py-1 rounded-full"
              style={{ background: color + '22', color }}
            >
              {label}
            </span>
          </div>

          {feedStatus && (
            <div className="grid grid-cols-3 gap-3 text-center">
              {[
                { label: 'Received',   value: feedStatus.itemsReceived },
                { label: 'Succeeded',  value: feedStatus.itemsSucceeded },
                { label: 'Failed',     value: feedStatus.itemsFailed },
              ].map(({ label, value }) => (
                <div key={label} className="rounded-lg p-2" style={{ background: 'var(--bg-secondary)' }}>
                  <p className="text-lg font-bold text-[var(--text-primary)]">{value}</p>
                  <p className="text-xs text-[var(--text-muted)]">{label}</p>
                </div>
              ))}
            </div>
          )}

          {!feedStatus && (
            <div className="flex items-center gap-2 text-sm text-[var(--text-muted)]">
              <i className="ri-loader-4-line animate-spin" />
              Polling for status every 5 seconds…
            </div>
          )}

          {feedStatus?.feedStatus === 'PROCESSED' && feedStatus.itemsSucceeded > 0 && (
            <div className="mt-3 p-3 rounded-lg bg-green-500/10 text-green-400 text-sm">
              <i className="ri-checkbox-circle-line mr-1" />
              Item submitted successfully. It may take up to 24 hours to appear on Walmart.com.
            </div>
          )}

          {feedStatus?.feedStatus === 'ERROR' && (
            <div className="mt-3 p-3 rounded-lg bg-red-500/10 text-red-400 text-sm">
              <i className="ri-error-warning-line mr-1" />
              Feed processing failed. Check Walmart Seller Center for details.
            </div>
          )}
        </div>
      </div>
    );
  }

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
          <span style={{ fontWeight: 600 }}>🤖 AI is generating optimised content for Walmart...</span>
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
              <span>🛒</span> Create Walmart Listing
            </h1>
            <p className="page-subtitle">
              Items are submitted via Walmart's feed system. Status is polled automatically after submission.
            </p>
          </div>
        </div>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* ── Main form ───────────────────────────────────────────────────── */}
        <div className="lg:col-span-2 space-y-4">

          {/* ── Configurator (CFG-07) ── */}
          <ConfiguratorSelector channel="walmart" credentialId={credentialId} onSelect={handleConfiguratorSelect} />

          {/* Feed status (shown after submit) */}
          {renderFeedStatus()}

          {/* Product Details */}
          <div className="card">
            <div className="card-header">
              <h3 className="card-title">Product Details</h3>
            </div>
            <div className="card-body space-y-4">
              <div>
                <label className="form-label">
                  Product Name <span className="text-red-500">*</span>
                </label>
                <input
                  type="text"
                  value={productName}
                  onChange={(e) => setProductName(e.target.value)}
                  placeholder="e.g. Acme Widget Pro 3000"
                  className="input w-full"
                  maxLength={200}
                />
                <p className="form-hint">{productName.length}/200 characters</p>
              </div>

              <div>
                <label className="form-label">Short Description</label>
                <textarea
                  value={shortDescription}
                  onChange={(e) => setShortDescription(e.target.value)}
                  rows={3}
                  className="input w-full resize-none"
                  placeholder="Brief product summary shown in search results…"
                  maxLength={4000}
                />
              </div>

              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="form-label">Brand</label>
                  <input
                    type="text"
                    value={brand}
                    onChange={(e) => setBrand(e.target.value)}
                    placeholder="Brand name"
                    className="input w-full"
                  />
                </div>
                <div>
                  <label className="form-label">Model Number</label>
                  <input
                    type="text"
                    value={modelNumber}
                    onChange={(e) => setModelNumber(e.target.value)}
                    placeholder="Manufacturer model number"
                    className="input w-full"
                  />
                </div>
              </div>

              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="form-label">SKU</label>
                  <input
                    type="text"
                    value={sku}
                    onChange={(e) => setSku(e.target.value)}
                    placeholder="Your unique SKU"
                    className="input w-full"
                  />
                </div>
                <div>
                  <label className="form-label">UPC / GTIN</label>
                  <input
                    type="text"
                    value={upc}
                    onChange={(e) => setUpc(e.target.value)}
                    placeholder="12-digit UPC barcode"
                    className="input w-full"
                  />
                  <p className="form-hint">Strongly recommended by Walmart</p>
                </div>
              </div>
            </div>
          </div>

          {/* Pricing & Inventory */}
          <div className="card">
            <div className="card-header">
              <h3 className="card-title">Pricing & Inventory</h3>
            </div>
            <div className="card-body">
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="form-label">
                    Price (USD) <span className="text-red-500">*</span>
                  </label>
                  <div className="relative">
                    <span className="absolute left-3 top-1/2 -translate-y-1/2 text-[var(--text-muted)]">$</span>
                    <input
                      type="number"
                      step="0.01"
                      min="0.01"
                      value={price}
                      onChange={(e) => setPrice(e.target.value)}
                      className="input w-full pl-7"
                      placeholder="0.00"
                    />
                  </div>
                </div>
                <div>
                  <label className="form-label">Quantity</label>
                  <input
                    type="number"
                    min="0"
                    value={quantity}
                    onChange={(e) => setQuantity(parseInt(e.target.value) || 0)}
                    className="input w-full"
                  />
                </div>
              </div>
            </div>
          </div>

          {/* Category & Shipping */}
          <div className="card">
            <div className="card-header">
              <h3 className="card-title">Category & Shipping</h3>
            </div>
            <div className="card-body space-y-4">
              <div>
                <label className="form-label">Category</label>
                <select
                  value={category}
                  onChange={(e) => setCategory(e.target.value)}
                  className="input w-full"
                >
                  <option value="">— Select a category —</option>
                  {WALMART_CATEGORIES.map((cat) => (
                    <option key={cat.id} value={cat.id}>{cat.name}</option>
                  ))}
                </select>
              </div>
              <div>
                <label className="form-label">Shipping Weight (lb)</label>
                <input
                  type="number"
                  step="0.01"
                  min="0"
                  value={shippingWeight}
                  onChange={(e) => setShippingWeight(e.target.value)}
                  className="input w-full"
                  placeholder="e.g. 1.5"
                />
              </div>
            </div>
          </div>

          {/* Key Features */}
          <div className="card">
            <div className="card-header">
              <h3 className="card-title">Key Features</h3>
              <p className="card-subtitle">Bullet points shown on the Walmart product page</p>
            </div>
            <div className="card-body space-y-2">
              {keyFeatures.map((feature, i) => (
                <div key={i} className="flex gap-2 items-center">
                  <span className="text-[var(--text-muted)] text-sm w-5 text-right shrink-0">{i + 1}.</span>
                  <input
                    type="text"
                    value={feature}
                    onChange={(e) => updateFeature(i, e.target.value)}
                    placeholder={`Feature ${i + 1}`}
                    className="input flex-1"
                  />
                  {keyFeatures.length > 1 && (
                    <button
                      type="button"
                      className="btn btn-ghost btn-sm text-[var(--text-muted)]"
                      onClick={() => removeFeature(i)}
                      title="Remove"
                    >
                      <i className="ri-close-line" />
                    </button>
                  )}
                </div>
              ))}
              {keyFeatures.length < 10 && (
                <button
                  type="button"
                  className="btn btn-ghost btn-sm mt-1"
                  onClick={addFeature}
                >
                  <i className="ri-add-line" /> Add feature
                </button>
              )}
            </div>
          </div>

          {/* Images */}
          <div className="card">
            <div className="card-header">
              <h3 className="card-title">Images</h3>
            </div>
            <div className="card-body">
              {images.length === 0 ? (
                <p className="text-sm text-[var(--text-muted)]">
                  No images loaded from MarketMate product. Add image URLs below.
                </p>
              ) : (
                <div className="image-grid mb-3">
                  {images.map((src, i) => (
                    <div key={i} className="image-thumb-wrap">
                      <img src={src} alt="" className="image-thumb" />
                      {i === 0 && <span className="image-badge">Main</span>}
                      <button
                        className="image-remove-btn"
                        onClick={() => setImages(images.filter((_, idx) => idx !== i))}
                        title="Remove"
                      >
                        <i className="ri-close-line" />
                      </button>
                    </div>
                  ))}
                </div>
              )}
              <p className="form-hint">
                Images are submitted as URLs. Walmart will download and host them.
                The first image is used as the main product image.
              </p>
            </div>
          </div>
        </div>

        {/* ── Sidebar ─────────────────────────────────────────────────────── */}
        <div className="space-y-4">
          {/* Submit Feed */}
          <div className="card">
            <div className="card-header">
              <h3 className="card-title">Submit to Walmart</h3>
            </div>
            <div className="card-body space-y-3">
              <div className="p-3 rounded-lg text-sm" style={{ background: 'var(--bg-secondary)' }}>
                <p className="font-medium text-[var(--text-primary)] mb-1">
                  <i className="ri-information-line mr-1" />Feed-based submission
                </p>
                <p className="text-[var(--text-muted)]">
                  Walmart processes listings asynchronously via feeds. After submitting, this page
                  polls for status every 5 seconds. Items may take up to 24 hours to go live.
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
                disabled={submitting || (feedStatus?.feedStatus === 'RECEIVED' || feedStatus?.feedStatus === 'INPROGRESS')}
              >
                {submitting ? (
                  <><i className="ri-loader-4-line animate-spin" /> Submitting…</>
                ) : feedId ? (
                  <><i className="ri-refresh-line" /> Resubmit Feed</>
                ) : (
                  <><i className="ri-send-plane-line" /> Submit as Feed</>
                )}
              </button>

              {feedId && (
                <button
                  className="btn btn-secondary w-full"
                  onClick={() => navigate(-1)}
                >
                  <i className="ri-check-line" /> Done
                </button>
              )}
            </div>
          </div>

          {/* Walmart requirements */}
          <div className="card">
            <div className="card-header">
              <h3 className="card-title">Requirements</h3>
            </div>
            <div className="card-body text-sm space-y-2">
              {[
                { field: 'Product Name', ok: productName.trim().length > 0 },
                { field: 'Price (USD)', ok: !!price && parseFloat(price) > 0 },
                { field: 'UPC / GTIN', ok: upc.trim().length > 0, warn: true },
                { field: 'Brand', ok: brand.trim().length > 0, warn: true },
                { field: 'Category', ok: category !== '', warn: true },
                { field: 'Main Image', ok: images.length > 0, warn: true },
              ].map(({ field, ok, warn }) => (
                <div key={field} className="flex items-center gap-2">
                  <i
                    className={ok
                      ? 'ri-checkbox-circle-line text-green-400'
                      : warn
                        ? 'ri-alert-line text-yellow-400'
                        : 'ri-close-circle-line text-red-400'}
                  />
                  <span className={ok ? 'text-[var(--text-primary)]' : 'text-[var(--text-muted)]'}>
                    {field}
                    {!ok && warn ? ' (recommended)' : !ok ? ' (required)' : ''}
                  </span>
                </div>
              ))}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

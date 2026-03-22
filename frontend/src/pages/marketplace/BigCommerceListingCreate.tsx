// ============================================================================
// BIGCOMMERCE LISTING CREATE PAGE
// ============================================================================
// Arrives with ?product_id=xxx from product edit page.
// Covers: product name, SKU, description, price, inventory, weight,
// product type, availability, condition, category selector, visibility.

import { useState, useEffect } from 'react';
import { useSearchParams, useNavigate } from 'react-router-dom';
import { bigcommerceApi, BigCommerceCategory, BigCommerceProduct, ChannelVariantDraft } from '../../services/bigcommerce-api';
import ConfiguratorSelector from './ConfiguratorSelector';
import { ConfiguratorDetail } from '../../services/configurator-api';

// ── Category helpers ──────────────────────────────────────────────────────────

function buildCategoryTree(cats: BigCommerceCategory[]): Map<number, BigCommerceCategory[]> {
  const tree = new Map<number, BigCommerceCategory[]>();
  for (const cat of cats) {
    const siblings = tree.get(cat.parent_id) || [];
    siblings.push(cat);
    tree.set(cat.parent_id, siblings);
  }
  return tree;
}

function getCategoryPath(categoryId: number, cats: BigCommerceCategory[]): string {
  const catMap = new Map(cats.map((c) => [c.id, c]));
  const parts: string[] = [];
  let current: BigCommerceCategory | undefined = catMap.get(categoryId);
  while (current) {
    parts.unshift(current.name);
    current = current.parent_id ? catMap.get(current.parent_id) : undefined;
  }
  return parts.join(' › ');
}

// ── Option constants ──────────────────────────────────────────────────────────

const TYPE_OPTIONS = [
  { value: 'physical', label: 'Physical' },
  { value: 'digital', label: 'Digital' },
];

const AVAILABILITY_OPTIONS = [
  { value: 'available', label: 'Available' },
  { value: 'disabled', label: 'Disabled' },
  { value: 'preorder', label: 'Pre-order' },
];

const CONDITION_OPTIONS = [
  { value: 'New', label: 'New' },
  { value: 'Used', label: 'Used' },
  { value: 'Refurbished', label: 'Refurbished' },
];

// ── Main Component ────────────────────────────────────────────────────────────

export default function BigCommerceListingCreate() {
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

  // Core fields
  const [name, setName] = useState('');
  const [sku, setSku] = useState('');
  const [description, setDescription] = useState('');
  const [price, setPrice] = useState('');
  const [inventoryLevel, setInventoryLevel] = useState<number>(0);
  const [weight, setWeight] = useState('');
  const [images, setImages] = useState<string[]>([]);

  // Product config
  const [type, setType] = useState('physical');
  const [isVisible, setIsVisible] = useState(true);
  const [availability, setAvailability] = useState('available');
  const [condition, setCondition] = useState('New');
  const [pageTitle, setPageTitle] = useState('');
  const [metaDescription, setMetaDescription] = useState('');

  // Category
  const [allCategories, setAllCategories] = useState<BigCommerceCategory[]>([]);
  const [catTree, setCatTree] = useState<Map<number, BigCommerceCategory[]>>(new Map());
  const [selectedCategoryId, setSelectedCategoryId] = useState<number | null>(null);
  const [selectedCategoryName, setSelectedCategoryName] = useState('');
  const [catBrowsing, setCatBrowsing] = useState(false);
  const [browseLevels, setBrowseLevels] = useState<BigCommerceCategory[][]>([]);
  const [browseSel, setBrowseSel] = useState<(BigCommerceCategory | null)[]>([]);

  // Submission
  const [submitting, setSubmitting] = useState(false);
  const [submitResult, setSubmitResult] = useState<{
    ok: boolean;
    id?: number;
    sku?: string;
    error?: string;
  } | null>(null);

  // ── Configurator (CFG-07) ──
  const [_selectedConfigurator, setSelectedConfigurator] = useState<ConfiguratorDetail | null>(null);

  // VAR-01 — Variation listings
  const [variants, setVariants] = useState<ChannelVariantDraft[]>([]);
  const [isVariantProduct, setIsVariantProduct] = useState(false);
  const updateVariant = (id: string, field: keyof ChannelVariantDraft, value: any) => {
    setVariants(vs => vs.map(v => v.id === id ? { ...v, [field]: value } : v));
  };

  const handleConfiguratorSelect = (cfg: ConfiguratorDetail | null) => {
    setSelectedConfigurator(cfg);
    if (!cfg) return;
    // Pre-populate category
    if (cfg.category_id) {
      const catIdNum = parseInt(cfg.category_id, 10);
      if (!isNaN(catIdNum)) {
        setSelectedCategoryId(catIdNum);
        setSelectedCategoryName(cfg.category_path || cfg.category_id);
      }
    }
    // Pre-populate product type / condition
    if (cfg.shipping_defaults?.product_type) setType(cfg.shipping_defaults.product_type);
    if (cfg.shipping_defaults?.condition) setCondition(cfg.shipping_defaults.condition);
  };

  // ── Init ───────────────────────────────────────────────────────────────────

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
      const [prepRes, catRes] = await Promise.allSettled([
        bigcommerceApi.prepareDraft(productId!, credentialId || undefined),
        bigcommerceApi.getCategories(credentialId || undefined),
      ]);

      if (prepRes.status === 'fulfilled' && prepRes.value.data?.ok) {
        const d = prepRes.value.data.draft;
        setName(d.name || '');
        setSku(d.sku || '');
        setDescription(d.description || '');
        setPrice(String(d.price || ''));
        setInventoryLevel(d.inventory_level ?? 0);
        setWeight(String(d.weight || ''));
        setImages(d.images || []);
        if (d.type) setType(d.type);
        if (d.availability) setAvailability(d.availability);
        if (d.condition) setCondition(d.condition);
        setIsVisible(d.is_visible ?? true);
        // VAR-01: load variants
        if (d.variants && d.variants.length > 0) {
          setVariants(d.variants);
          setIsVariantProduct(true);
        }
      } else {
        setError('Could not load product data. Make sure a BigCommerce credential is connected.');
      }

      if (catRes.status === 'fulfilled' && catRes.value.data?.ok) {
        const cats = (catRes.value.data.categories || []).filter((c) => c.is_visible);
        setAllCategories(cats);
        setCatTree(buildCategoryTree(cats));
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
            channel: 'bigcommerce',
            category_id: '',
            category_name: '',
            fields: schemaFields,
          });
          const aiListing = aiRes.data.data?.listings?.[0];
          if (aiListing) {
            if (aiListing.title) { setName(aiListing.title); }
            if (aiListing.description) { setDescription(aiListing.description); }
            setAiApplied(true);
          }
        } catch (aiErr: any) {
          setAiError(aiErr.response?.data?.error || aiErr.message || 'AI generation failed');
        }
        setAiGenerating(false);
      }
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Failed to load BigCommerce listing data';
      setError(msg);
    } finally {
      setLoading(false);
    }
  }

  // ── Category browser ───────────────────────────────────────────────────────

  // ENH-03: refresh categories on demand
  const [refreshingCats, setRefreshingCats] = useState(false);
  async function refreshCategories() {
    setRefreshingCats(true);
    try {
      const catRes = await bigcommerceApi.getCategories(credentialId || undefined);
      if (catRes.data?.ok) {
        const cats = (catRes.data.categories || []).filter((c: any) => c.is_visible);
        setAllCategories(cats);
        setCatTree(buildCategoryTree(cats));
      }
    } catch { /* ignore */ }
    finally { setRefreshingCats(false); }
  }

  function openCategoryBrowser() {
    // BigCommerce root categories have parent_id=0
    const roots = catTree.get(0) || [];
    setBrowseLevels([roots]);
    setBrowseSel([null]);
    setCatBrowsing(true);
  }

  function selectBrowseNode(level: number, cat: BigCommerceCategory) {
    const newSel = browseSel.slice(0, level + 1);
    newSel[level] = cat;
    const newLevels = browseLevels.slice(0, level + 1);

    const children = (catTree.get(cat.id) || []).filter((c) => c.is_visible);
    if (children.length > 0) {
      newLevels.push(children);
      newSel.push(null);
    }

    setBrowseLevels(newLevels);
    setBrowseSel(newSel);
  }

  function confirmCategorySelection() {
    const lastSelected = [...browseSel].reverse().find(Boolean);
    if (lastSelected) {
      setSelectedCategoryId(lastSelected.id);
      setSelectedCategoryName(getCategoryPath(lastSelected.id, allCategories));
    }
    setCatBrowsing(false);
  }

  // ── Submit ─────────────────────────────────────────────────────────────────

  async function handleSubmit() {
    if (!name.trim()) {
      alert('Product name is required');
      return;
    }
    const priceNum = parseFloat(price);
    if (isNaN(priceNum) || priceNum < 0) {
      alert('Price must be 0 or greater');
      return;
    }

    setSubmitting(true);
    setSubmitResult(null);

    try {
      const payload: BigCommerceProduct & { credential_id?: string; variants?: ChannelVariantDraft[] } = {
        name: name.trim(),
        type,
        sku: sku.trim() || undefined,
        description: description.trim() || undefined,
        price: priceNum,
        weight: weight ? parseFloat(weight) : 0,
        inventory_level: inventoryLevel,
        inventory_tracking: isVariantProduct && variants.filter(v => v.active).length >= 2 ? 'variant' : 'product',
        is_visible: isVisible,
        availability,
        condition,
        page_title: pageTitle.trim() || undefined,
        meta_description: metaDescription.trim() || undefined,
        categories: selectedCategoryId ? [selectedCategoryId] : undefined,
        credential_id: credentialId || undefined,
        variants: isVariantProduct ? variants : undefined, // VAR-01
      };

      const res = await bigcommerceApi.submit(payload);
      setSubmitResult(res.data);
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

  // ── Render ─────────────────────────────────────────────────────────────────

  const BC_BLUE = '#1C4EBF';

  if (loading) {
    return (
      <div className="page">
        <div className="loading-state">
          <div className="spinner" />
          <p>Loading BigCommerce listing data…</p>
        </div>
      </div>
    );
  }

  if (error && !name) {
    return (
      <div className="page">
        <div className="empty-state">
          <i className="ri-error-warning-line text-red-500 text-4xl mb-3" />
          <p className="text-red-500">{error}</p>
          <button className="btn btn-secondary mt-4" onClick={() => navigate(-1)}>
            ← Back
          </button>
        </div>
      </div>
    );
  }

  if (submitResult?.ok) {
    return (
      <div className="page">
        <div className="empty-state">
          <div style={{ fontSize: 56, marginBottom: 16 }}>🛒</div>
          <h2 className="text-xl font-semibold mb-2">BigCommerce Product Created!</h2>
          {submitResult.id && (
            <p className="text-[var(--text-muted)] mb-1">
              Product ID: <code>{submitResult.id}</code>
            </p>
          )}
          {submitResult.sku && (
            <p className="text-[var(--text-muted)] mb-4">
              SKU: <code>{submitResult.sku}</code>
            </p>
          )}
          <div className="flex gap-3 justify-center">
            <button className="btn btn-secondary" onClick={() => navigate(-1)}>
              ← Back to Product
            </button>
            <button
              className="btn btn-primary"
              onClick={() => navigate('/marketplace/listings')}
            >
              View All Listings
            </button>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="page">
      {/* Header */}
      <div className="page-header">
        <button className="btn btn-ghost btn-sm" onClick={() => navigate(-1)}>
          <i className="ri-arrow-left-line" />
        </button>
        <div>
          <h1 className="page-title flex items-center gap-2">
            <i className="ri-store-3-line" style={{ color: BC_BLUE }} />
            Create BigCommerce Listing
          </h1>
          {productId && (
            <p className="text-sm text-[var(--text-muted)]">Product ID: {productId}</p>
          )}
        </div>
        <div className="ml-auto flex gap-2 items-center">
          <label className="flex items-center gap-2 cursor-pointer">
            <span className="text-sm text-[var(--text-muted)]">Visible</span>
            <div
              className={`toggle ${isVisible ? 'toggle-active' : ''}`}
              onClick={() => setIsVisible(!isVisible)}
              style={{
                width: 40,
                height: 22,
                borderRadius: 11,
                background: isVisible ? BC_BLUE : 'var(--border)',
                position: 'relative',
                cursor: 'pointer',
                transition: 'background 0.2s',
              }}
            >
              <div
                style={{
                  position: 'absolute',
                  top: 3,
                  left: isVisible ? 21 : 3,
                  width: 16,
                  height: 16,
                  borderRadius: '50%',
                  background: '#fff',
                  transition: 'left 0.2s',
                  boxShadow: '0 1px 3px rgba(0,0,0,0.3)',
                }}
              />
            </div>
          </label>
          <button
            className="btn btn-primary"
            onClick={handleSubmit}
            disabled={submitting}
            style={{ background: BC_BLUE, borderColor: BC_BLUE }}
          >
            {submitting ? 'Creating…' : 'Create Product'}
          </button>
        </div>
      </div>

      {/* Error banners */}
      {error && (
        <div className="alert alert-error mb-4">
          <i className="ri-error-warning-line" /> {error}
        </div>

      )}
      {aiGenerating && (
        <div style={{ padding: '12px 14px', background: 'linear-gradient(90deg, rgba(139,92,246,0.15), rgba(59,130,246,0.15))', borderRadius: 8, fontSize: 13, color: 'var(--accent-purple)', marginBottom: 16, display: 'flex', gap: 10, alignItems: 'center', border: '1px solid rgba(139,92,246,0.3)' }}>
          <div className="spinner" style={{ width: 16, height: 16, border: '2px solid var(--accent-purple)', borderTopColor: 'transparent', borderRadius: '50%', animation: 'spin 1s linear infinite' }} />
          <span style={{ fontWeight: 600 }}>🤖 AI is generating optimised content for BigCommerce...</span>
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
        <div className="alert alert-error mb-4">
          <i className="ri-error-warning-line" /> {submitResult.error || 'Submission failed'}
        </div>
      )}

      {/* ── Configurator (CFG-07) ── */}
      <ConfiguratorSelector channel="bigcommerce" credentialId={credentialId} onSelect={handleConfiguratorSelect} />

      {/* VAR-01 — Variant Grid */}
      {isVariantProduct && (
        <div className="card mb-4" style={{ borderLeft: '3px solid #d946ef' }}>
          <div className="card-header">
            <h3 className="card-title" style={{ color: '#d946ef' }}>Variants ({variants.length})</h3>
          </div>
          <div className="card-body">
            <p style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 12 }}>
              BigCommerce supports native variant listings with per-variant inventory tracking. Each active variant will be submitted with its own SKU, price and stock.
            </p>
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
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      )}

      {/* Requirements checklist */}
      <div
        className="card mb-4"
        style={{ borderLeft: `3px solid ${BC_BLUE}`, background: 'var(--bg-secondary)' }}
      >
        <div className="card-body py-3">
          <p className="text-sm font-medium mb-2">
            <i className="ri-information-line mr-1" />
            BigCommerce Requirements
          </p>
          <div className="flex flex-wrap gap-3 text-sm">
            <span className={`flex items-center gap-1 ${name ? 'text-green-400' : 'text-[var(--text-muted)]'}`}>
              <i className={name ? 'ri-checkbox-circle-fill' : 'ri-checkbox-blank-circle-line'} />
              Product Name
            </span>
            <span
              className={`flex items-center gap-1 ${parseFloat(price) >= 0 && price !== '' ? 'text-green-400' : 'text-[var(--text-muted)]'}`}
            >
              <i
                className={
                  parseFloat(price) >= 0 && price !== ''
                    ? 'ri-checkbox-circle-fill'
                    : 'ri-checkbox-blank-circle-line'
                }
              />
              Price
            </span>
            <span className={`flex items-center gap-1 ${parseFloat(weight) > 0 ? 'text-green-400' : 'text-[var(--text-muted)]'}`}>
              <i className={parseFloat(weight) > 0 ? 'ri-checkbox-circle-fill' : 'ri-checkbox-blank-circle-line'} />
              Weight (physical products)
            </span>
            <span className="flex items-center gap-1 text-[var(--text-muted)]">
              <i className="ri-checkbox-blank-circle-line" />
              Category (optional)
            </span>
          </div>
        </div>
      </div>

      <div className="listing-create-layout">
        {/* ── Left column: Core fields ──────────────────────────────────────── */}
        <div className="listing-create-main">
          {/* Identity */}
          <div className="card mb-4">
            <div className="card-header">
              <h3 className="card-title">Product Identity</h3>
            </div>
            <div className="card-body">
              <div className="form-row">
                <div className="form-group">
                  <label className="form-label">
                    Product Name <span className="text-red-500">*</span>
                  </label>
                  <input
                    className="input"
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    placeholder="Enter product name"
                    maxLength={255}
                  />
                  <p className="form-hint">{name.length}/255</p>
                </div>
                <div className="form-group">
                  <label className="form-label">SKU</label>
                  <input
                    className="input"
                    value={sku}
                    onChange={(e) => setSku(e.target.value)}
                    placeholder="Stock keeping unit (optional)"
                  />
                  <p className="form-hint">Optional but recommended. Must be unique in your store.</p>
                </div>
              </div>
            </div>
          </div>

          {/* Description */}
          <div className="card mb-4">
            <div className="card-header">
              <h3 className="card-title">Description</h3>
            </div>
            <div className="card-body">
              <label className="form-label">Description (HTML supported)</label>
              <textarea
                className="input"
                rows={8}
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="Detailed product description…"
                style={{ fontFamily: 'var(--font-mono, monospace)', fontSize: 13 }}
              />
            </div>
          </div>

          {/* Pricing & Inventory */}
          <div className="card mb-4">
            <div className="card-header">
              <h3 className="card-title">Pricing &amp; Inventory</h3>
            </div>
            <div className="card-body">
              <div className="form-row">
                <div className="form-group">
                  <label className="form-label">
                    Price <span className="text-red-500">*</span>
                  </label>
                  <div className="input-group">
                    <span className="input-group-text">£</span>
                    <input
                      type="number"
                      className="input"
                      value={price}
                      onChange={(e) => setPrice(e.target.value)}
                      placeholder="0.00"
                      min="0"
                      step="0.01"
                    />
                  </div>
                </div>
                <div className="form-group">
                  <label className="form-label">Inventory Level</label>
                  <input
                    type="number"
                    className="input"
                    value={inventoryLevel}
                    onChange={(e) => setInventoryLevel(parseInt(e.target.value) || 0)}
                    min="0"
                  />
                  <p className="form-hint">Stock tracked via BigCommerce inventory management.</p>
                </div>
              </div>
            </div>
          </div>

          {/* Shipping */}
          <div className="card mb-4">
            <div className="card-header">
              <h3 className="card-title">Shipping</h3>
            </div>
            <div className="card-body">
              <div className="form-group" style={{ maxWidth: 240 }}>
                <label className="form-label">
                  Weight (kg){type === 'physical' && <span className="text-red-500"> *</span>}
                </label>
                <input
                  className="input"
                  value={weight}
                  onChange={(e) => setWeight(e.target.value)}
                  placeholder="0.00"
                  type="number"
                  min="0"
                  step="0.001"
                />
                <p className="form-hint">Required for physical products to calculate shipping rates.</p>
              </div>
            </div>
          </div>

          {/* SEO */}
          <div className="card mb-4">
            <div className="card-header">
              <h3 className="card-title">SEO</h3>
            </div>
            <div className="card-body">
              <div className="form-group">
                <label className="form-label">Page Title</label>
                <input
                  className="input"
                  value={pageTitle}
                  onChange={(e) => setPageTitle(e.target.value)}
                  placeholder="Overrides product name for browser tab and search engines"
                  maxLength={255}
                />
              </div>
              <div className="form-group">
                <label className="form-label">Meta Description</label>
                <textarea
                  className="input"
                  rows={3}
                  value={metaDescription}
                  onChange={(e) => setMetaDescription(e.target.value)}
                  placeholder="Short description for search engine results…"
                  maxLength={255}
                />
                <p className="form-hint">{metaDescription.length}/255</p>
              </div>
            </div>
          </div>
        </div>

        {/* ── Right column ──────────────────────────────────────────────────── */}
        <div className="listing-create-sidebar">
          {/* Product Config */}
          <div className="card mb-4">
            <div className="card-header">
              <h3 className="card-title">Product Configuration</h3>
            </div>
            <div className="card-body">
              <label className="form-label">Product Type</label>
              <div className="flex gap-2 flex-wrap mb-4">
                {TYPE_OPTIONS.map((opt) => (
                  <button
                    key={opt.value}
                    className={`btn btn-sm ${type === opt.value ? 'btn-primary' : 'btn-secondary'}`}
                    onClick={() => setType(opt.value)}
                    style={type === opt.value ? { background: BC_BLUE, borderColor: BC_BLUE } : {}}
                  >
                    {opt.label}
                  </button>
                ))}
              </div>

              <label className="form-label">Availability</label>
              <select
                className="input w-full mb-4"
                value={availability}
                onChange={(e) => setAvailability(e.target.value)}
              >
                {AVAILABILITY_OPTIONS.map((opt) => (
                  <option key={opt.value} value={opt.value}>
                    {opt.label}
                  </option>
                ))}
              </select>

              <label className="form-label">Condition</label>
              <select
                className="input w-full"
                value={condition}
                onChange={(e) => setCondition(e.target.value)}
              >
                {CONDITION_OPTIONS.map((opt) => (
                  <option key={opt.value} value={opt.value}>
                    {opt.label}
                  </option>
                ))}
              </select>
            </div>
          </div>

          {/* Category */}
          <div className="card mb-4">
            <div className="card-header" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <h3 className="card-title">Category</h3>
              <button
                type="button"
                onClick={refreshCategories}
                disabled={refreshingCats}
                style={{ padding: '4px 10px', fontSize: 12, borderRadius: 6, border: '1px solid var(--border)', background: 'transparent', color: 'var(--text-muted)', cursor: 'pointer', opacity: refreshingCats ? 0.6 : 1 }}
                title="Re-fetch categories from BigCommerce"
              >{refreshingCats ? '⏳ Refreshing…' : '🔄 Refresh'}</button>
            </div>
            <div className="card-body">
              <button
                type="button"
                className="btn btn-secondary w-full text-left"
                onClick={openCategoryBrowser}
              >
                {selectedCategoryName || (
                  <span className="text-[var(--text-muted)]">
                    <i className="ri-folder-line mr-1" />
                    Select category…
                  </span>
                )}
              </button>
              {selectedCategoryId && (
                <button
                  type="button"
                  className="btn btn-ghost btn-sm mt-1 text-[var(--text-muted)]"
                  onClick={() => {
                    setSelectedCategoryId(null);
                    setSelectedCategoryName('');
                  }}
                >
                  <i className="ri-close-line" /> Clear
                </button>
              )}
              <p className="form-hint mt-2">
                Assigns the product to a BigCommerce category. Products can appear in multiple
                categories — additional assignments can be made in BigCommerce Admin.
              </p>
            </div>
          </div>

          {/* Images */}
          <div className="card mb-4">
            <div className="card-header">
              <h3 className="card-title">Images</h3>
            </div>
            <div className="card-body">
              {images.length === 0 ? (
                <p className="text-sm text-[var(--text-muted)]">
                  No images loaded from MarketMate product.
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
                Images are referenced by URL. Upload additional images directly in your
                BigCommerce Admin under Products → Images.
              </p>
            </div>
          </div>
        </div>
      </div>

      {/* ── Category Browser Modal ────────────────────────────────────────── */}
      {catBrowsing && (
        <div className="modal-overlay" onClick={() => setCatBrowsing(false)}>
          <div className="modal modal-lg" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <h3 className="modal-title">Select Category</h3>
              <button
                className="btn btn-ghost btn-sm"
                onClick={() => setCatBrowsing(false)}
              >
                <i className="ri-close-line" />
              </button>
            </div>
            <div className="modal-body">
              {browseLevels.length === 0 || browseLevels[0].length === 0 ? (
                <p className="text-[var(--text-muted)]">
                  No categories found on this BigCommerce store.
                </p>
              ) : (
                <div className="category-browser">
                  {browseLevels.map((cats, level) => (
                    <div key={level} className="category-browser-col">
                      {cats.map((cat) => {
                        const hasChildren =
                          (catTree.get(cat.id) || []).filter((c) => c.is_visible).length > 0;
                        const isSelected = browseSel[level]?.id === cat.id;
                        return (
                          <button
                            key={cat.id}
                            className={`category-item ${isSelected ? 'selected' : ''}`}
                            onClick={() => selectBrowseNode(level, cat)}
                          >
                            <span className="category-name">{cat.name}</span>
                            {hasChildren && <i className="ri-arrow-right-s-line" />}
                          </button>
                        );
                      })}
                    </div>
                  ))}
                </div>
              )}
            </div>
            <div className="modal-footer">
              <span className="text-sm text-[var(--text-muted)]">
                {browseSel.filter(Boolean).length > 0
                  ? browseSel
                      .filter(Boolean)
                      .map((c) => c!.name)
                      .join(' › ')
                  : 'No category selected'}
              </span>
              <div className="flex gap-2">
                <button className="btn btn-secondary" onClick={() => setCatBrowsing(false)}>
                  Cancel
                </button>
                <button
                  className="btn btn-primary"
                  onClick={confirmCategorySelection}
                  disabled={!browseSel.some(Boolean)}
                  style={{ background: BC_BLUE, borderColor: BC_BLUE }}
                >
                  Confirm
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

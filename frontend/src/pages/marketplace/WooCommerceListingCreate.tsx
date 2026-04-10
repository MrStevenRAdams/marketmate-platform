// ============================================================================
// WOOCOMMERCE LISTING CREATE PAGE
// ============================================================================
// Arrives with ?product_id=xxx from product edit page.
// Covers: product name, description (HTML), regular price, sale price, SKU,
// stock quantity, weight, dimensions, category selector, product type,
// downloadable/virtual toggles, images.

import { useState, useEffect } from 'react';
import { useSearchParams, useNavigate } from 'react-router-dom';
import { wooApi, WooCategory, WooProduct, ChannelVariantDraft } from '../../services/woocommerce-api';
import ConfiguratorSelector from './ConfiguratorSelector';
import { ConfiguratorDetail } from '../../services/configurator-api';

// ── Category tree helpers ─────────────────────────────────────────────────────

function buildCategoryTree(cats: WooCategory[]): Map<number, WooCategory[]> {
  const tree = new Map<number, WooCategory[]>();
  for (const cat of cats) {
    const siblings = tree.get(cat.parent) || [];
    siblings.push(cat);
    tree.set(cat.parent, siblings);
  }
  return tree;
}

function getCategoryPath(categoryId: number, cats: WooCategory[]): string {
  const catMap = new Map(cats.map((c) => [c.id, c]));
  const parts: string[] = [];
  let current: WooCategory | undefined = catMap.get(categoryId);
  while (current) {
    parts.unshift(current.name);
    current = current.parent ? catMap.get(current.parent) : undefined;
  }
  return parts.join(' › ');
}

// ── Main Component ────────────────────────────────────────────────────────────

export default function WooCommerceListingCreate() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const productId = searchParams.get('product_id');
  const credentialId = searchParams.get('credential_id') || '';

  // ── State ────────────────────────────────────────────────────────────────

  const [loading, setLoading] = useState(true);
  const [aiGenerating, setAiGenerating] = useState(false);
  const [aiApplied, setAiApplied] = useState(false);
  const [aiError, setAiError] = useState('');
  const [error, setError] = useState('');

  // Form fields
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [shortDescription, setShortDescription] = useState('');
  const [regularPrice, setRegularPrice] = useState('');
  const [salePrice, setSalePrice] = useState('');
  const [sku, setSku] = useState('');
  const [stockQuantity, setStockQuantity] = useState<number>(0);
  const [manageStock, setManageStock] = useState(true);
  const [weight, setWeight] = useState('');
  const [length, setLength] = useState('');
  const [width, setWidth] = useState('');
  const [height, setHeight] = useState('');
  const [productType, setProductType] = useState<'simple' | 'variable'>('simple');
  const [downloadable, setDownloadable] = useState(false);
  const [virtual, setVirtual] = useState(false);
  const [status, setStatus] = useState<'publish' | 'draft'>('publish');
  const [images, setImages] = useState<string[]>([]);

  // Category
  const [allCategories, setAllCategories] = useState<WooCategory[]>([]);
  const [catTree, setCatTree] = useState<Map<number, WooCategory[]>>(new Map());
  const [selectedCategoryId, setSelectedCategoryId] = useState<number | null>(null);
  const [selectedCategoryName, setSelectedCategoryName] = useState('');
  const [catBrowsing, setCatBrowsing] = useState(false);
  const [browseLevels, setBrowseLevels] = useState<WooCategory[][]>([]);
  const [browseSel, setBrowseSel] = useState<(WooCategory | null)[]>([]);

  // Submission
  const [submitting, setSubmitting] = useState(false);
  const [submitResult, setSubmitResult] = useState<{
    ok: boolean;
    product_id?: number;
    permalink?: string;
    error?: string;
  } | null>(null);

  // ── Configurator (CFG-07) ──
  // Note: WooCommerce submit does not create a MarketMate listing record,
  // so configurator join is not persisted — pre-population only.
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
    // Pre-populate shipping defaults (e.g. product type)
    if (cfg.shipping_defaults?.product_type) {
      setProductType(cfg.shipping_defaults.product_type as 'simple' | 'variable');
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
      const [prepRes, catRes] = await Promise.allSettled([
        wooApi.prepareDraft(productId!, credentialId || undefined),
        wooApi.getCategories(credentialId || undefined),
      ]);

      if (prepRes.status === 'fulfilled' && prepRes.value.data?.ok) {
        const d = prepRes.value.data.draft;
        setName(d.name || '');
        setDescription(d.description || '');
        setSku(String(d.sku || ''));
        setRegularPrice(String(d.regular_price || ''));
        setStockQuantity(d.stock_quantity ?? 0);
        setWeight(String(d.weight || ''));
        setLength(String(d.dimensions?.length || ''));
        setWidth(String(d.dimensions?.width || ''));
        setHeight(String(d.dimensions?.height || ''));
        setImages(d.images || []);
        // VAR-01: load variants; auto-switch type to 'variable' when present
        if (d.variants && d.variants.length > 0) {
          setVariants(d.variants);
          setIsVariantProduct(true);
          setProductType('variable');
        }
      } else {
        setError('Could not load product data. Make sure a WooCommerce credential is connected.');
      }

      if (catRes.status === 'fulfilled' && catRes.value.data?.ok) {
        const cats = catRes.value.data.categories || [];
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
            channel: 'woocommerce',
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
    } catch (err: any) {
      setError(err.message || 'Failed to load WooCommerce listing data');
    } finally {
      setLoading(false);
    }
  }

  // ── Category browser ──────────────────────────────────────────────────────

  function openCategoryBrowser() {
    const roots = catTree.get(0) || [];
    setBrowseLevels([roots]);
    setBrowseSel([null]);
    setCatBrowsing(true);
  }

  function selectBrowseNode(level: number, cat: WooCategory) {
    const newSel = browseSel.slice(0, level + 1);
    newSel[level] = cat;
    const newLevels = browseLevels.slice(0, level + 1);

    const children = catTree.get(cat.id) || [];
    if (children.length > 0) {
      newLevels.push(children);
      newSel.push(null);
    }

    setBrowseLevels(newLevels);
    setBrowseSel(newSel);
  }

  function confirmCategorySelection() {
    // Use the deepest selected node
    const lastSelected = [...browseSel].reverse().find(Boolean);
    if (lastSelected) {
      setSelectedCategoryId(lastSelected.id);
      setSelectedCategoryName(getCategoryPath(lastSelected.id, allCategories));
    }
    setCatBrowsing(false);
  }

  // ── Submit ────────────────────────────────────────────────────────────────

  async function handleSubmit() {
    if (!name.trim()) { alert('Product name is required'); return; }
    if (!regularPrice || parseFloat(regularPrice) < 0) { alert('Price must be 0 or greater'); return; }

    setSubmitting(true);
    setSubmitResult(null);

    try {
      const qty = stockQuantity;
      const payload: WooProduct & { credential_id?: string; variants?: ChannelVariantDraft[] } = {
        name: name.trim(),
        description: description.trim(),
        short_description: shortDescription.trim() || undefined,
        sku: sku.trim() || undefined,
        regular_price: regularPrice,
        sale_price: salePrice || undefined,
        manage_stock: manageStock,
        stock_quantity: manageStock ? qty : null,
        weight: weight || undefined,
        dimensions: (length || width || height) ? { length, width, height } : undefined,
        type: isVariantProduct ? 'variable' : productType, // VAR-01: force variable when variants present
        status,
        downloadable,
        virtual,
        images: images.map((src) => ({ src })),
        categories: selectedCategoryId ? [{ id: selectedCategoryId }] : undefined,
        credential_id: credentialId || undefined,
        variants: isVariantProduct ? variants : undefined, // VAR-01
      };

      const res = await wooApi.submit(payload);
      setSubmitResult(res.data);
    } catch (err: any) {
      setSubmitResult({
        ok: false,
        error: err.response?.data?.error || err.message || 'Submission failed',
      });
    } finally {
      setSubmitting(false);
    }
  }

  // ── Render ────────────────────────────────────────────────────────────────

  if (loading) {
    return (
      <div className="page">
        <div className="loading-state">
          <div className="spinner" />
          <p>Loading WooCommerce listing data…</p>
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
          <button className="btn btn-secondary mt-4" onClick={() => navigate(-1)}>← Back</button>
        </div>
      </div>
    );
  }

  if (submitResult?.ok) {
    return (
      <div className="page">
        <div className="empty-state">
          <div style={{ fontSize: 56, marginBottom: 16 }}>🛒</div>
          <h2 className="text-xl font-semibold mb-2">WooCommerce Product Created!</h2>
          {submitResult.product_id && (
            <p className="text-[var(--text-muted)] mb-1">
              Product ID: <code>{submitResult.product_id}</code>
            </p>
          )}
          {submitResult.permalink && (
            <p className="text-[var(--text-muted)] mb-4">
              <a href={submitResult.permalink} target="_blank" rel="noopener noreferrer"
                className="text-[var(--primary)] underline">
                View on store ↗
              </a>
            </p>
          )}
          <div className="flex gap-3 justify-center">
            <button className="btn btn-secondary" onClick={() => navigate(-1)}>← Back to Product</button>
            <button className="btn btn-primary" onClick={() => navigate('/marketplace/listings')}>
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
            <i className="ri-store-3-fill" style={{ color: '#7c3aed' }} />
            Create WooCommerce Listing
          </h1>
          {productId && (
            <p className="text-sm text-[var(--text-muted)]">Product ID: {productId}</p>
          )}
        </div>
        <div className="ml-auto flex gap-2">
          <select
            className="input input-sm"
            value={status}
            onChange={(e) => setStatus(e.target.value as 'publish' | 'draft')}
          >
            <option value="publish">Publish</option>
            <option value="draft">Save as Draft</option>
          </select>
          <button
            className="btn btn-primary"
            onClick={handleSubmit}
            disabled={submitting}
          >
            {submitting ? 'Publishing…' : status === 'publish' ? 'Publish Product' : 'Save Draft'}
          </button>
        </div>
      </div>

      {/* Error banner */}
      {error && (
        <div className="alert alert-error mb-4">
          <i className="ri-error-warning-line" /> {error}
        </div>

      )}
      {aiGenerating && (
        <div style={{ padding: '12px 14px', background: 'linear-gradient(90deg, rgba(139,92,246,0.15), rgba(59,130,246,0.15))', borderRadius: 8, fontSize: 13, color: 'var(--accent-purple)', marginBottom: 16, display: 'flex', gap: 10, alignItems: 'center', border: '1px solid rgba(139,92,246,0.3)' }}>
          <div className="spinner" style={{ width: 16, height: 16, border: '2px solid var(--accent-purple)', borderTopColor: 'transparent', borderRadius: '50%', animation: 'spin 1s linear infinite' }} />
          <span style={{ fontWeight: 600 }}>🤖 AI is generating optimised content for WooCommerce...</span>
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

      {/* Submit error */}
      {submitResult && !submitResult.ok && (
        <div className="alert alert-error mb-4">
          <i className="ri-error-warning-line" /> {submitResult.error || 'Submission failed'}
        </div>
      )}

      <div className="listing-create-layout">
        {/* ── Left column: Core fields ────────────────────────────────────── */}
        <div className="listing-create-main">

          {/* ── Configurator (CFG-07) ── */}
          <ConfiguratorSelector channel="woocommerce" credentialId={credentialId} onSelect={handleConfiguratorSelect} />

          {/* VAR-01 — Variant Grid */}
          {isVariantProduct && (
            <div className="card mb-4" style={{ borderLeft: '3px solid #d946ef' }}>
              <div className="card-header">
                <h3 className="card-title" style={{ color: '#d946ef' }}>Variants ({variants.length})</h3>
              </div>
              <div className="card-body">
                <p style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 12 }}>
                  WooCommerce supports native variable products. This product will be created as type <strong>variable</strong> with one variation per active variant below.
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

          {/* Product Name */}
          <div className="card mb-4">
            <div className="card-body">
              <label className="form-label">Product Name <span className="text-red-500">*</span></label>
              <input
                className="input"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="Enter product name"
                maxLength={200}
              />
              <p className="form-hint">{name.length}/200</p>
            </div>
          </div>

          {/* Description */}
          <div className="card mb-4">
            <div className="card-header"><h3 className="card-title">Description</h3></div>
            <div className="card-body">
              <label className="form-label">Full Description (HTML supported)</label>
              <textarea
                className="input"
                rows={8}
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="Detailed product description…"
                style={{ fontFamily: 'var(--font-mono, monospace)', fontSize: 13 }}
              />
              <label className="form-label mt-3">Short Description</label>
              <textarea
                className="input"
                rows={3}
                value={shortDescription}
                onChange={(e) => setShortDescription(e.target.value)}
                placeholder="Brief excerpt shown in product listings…"
              />
            </div>
          </div>

          {/* Pricing */}
          <div className="card mb-4">
            <div className="card-header"><h3 className="card-title">Pricing</h3></div>
            <div className="card-body">
              <div className="form-row">
                <div className="form-group">
                  <label className="form-label">Regular Price <span className="text-red-500">*</span></label>
                  <div className="input-group">
                    <span className="input-group-text">£</span>
                    <input
                      type="number"
                      className="input"
                      value={regularPrice}
                      onChange={(e) => setRegularPrice(e.target.value)}
                      placeholder="0.00"
                      min="0"
                      step="0.01"
                    />
                  </div>
                </div>
                <div className="form-group">
                  <label className="form-label">Sale Price</label>
                  <div className="input-group">
                    <span className="input-group-text">£</span>
                    <input
                      type="number"
                      className="input"
                      value={salePrice}
                      onChange={(e) => setSalePrice(e.target.value)}
                      placeholder="Optional"
                      min="0"
                      step="0.01"
                    />
                  </div>
                </div>
              </div>
            </div>
          </div>

          {/* Inventory */}
          <div className="card mb-4">
            <div className="card-header"><h3 className="card-title">Inventory</h3></div>
            <div className="card-body">
              <div className="form-row">
                <div className="form-group">
                  <label className="form-label">SKU</label>
                  <input
                    className="input"
                    value={sku}
                    onChange={(e) => setSku(e.target.value)}
                    placeholder="Stock keeping unit"
                  />
                </div>
                <div className="form-group">
                  <label className="form-label">Stock Quantity</label>
                  <input
                    type="number"
                    className="input"
                    value={stockQuantity}
                    onChange={(e) => setStockQuantity(parseInt(e.target.value) || 0)}
                    min="0"
                    disabled={!manageStock}
                  />
                </div>
              </div>
              <div className="form-check mt-2">
                <input
                  type="checkbox"
                  id="manageStock"
                  checked={manageStock}
                  onChange={(e) => setManageStock(e.target.checked)}
                  className="form-check-input"
                />
                <label htmlFor="manageStock" className="form-check-label">
                  Manage stock quantity
                </label>
              </div>
            </div>
          </div>

          {/* Shipping */}
          <div className="card mb-4">
            <div className="card-header"><h3 className="card-title">Shipping</h3></div>
            <div className="card-body">
              <div className="form-row">
                <div className="form-group">
                  <label className="form-label">Weight (kg)</label>
                  <input
                    className="input"
                    value={weight}
                    onChange={(e) => setWeight(e.target.value)}
                    placeholder="0.00"
                    type="number" min="0" step="0.01"
                  />
                </div>
              </div>
              <label className="form-label mt-2">Dimensions (cm)</label>
              <div className="form-row">
                <div className="form-group">
                  <input className="input" value={length} onChange={(e) => setLength(e.target.value)}
                    placeholder="Length" type="number" min="0" />
                </div>
                <div className="form-group">
                  <input className="input" value={width} onChange={(e) => setWidth(e.target.value)}
                    placeholder="Width" type="number" min="0" />
                </div>
                <div className="form-group">
                  <input className="input" value={height} onChange={(e) => setHeight(e.target.value)}
                    placeholder="Height" type="number" min="0" />
                </div>
              </div>
            </div>
          </div>

        </div>

        {/* ── Right column: Product type, category, images ──────────────── */}
        <div className="listing-create-sidebar">

          {/* Product Type */}
          <div className="card mb-4">
            <div className="card-header"><h3 className="card-title">Product Type</h3></div>
            <div className="card-body">
              <div className="flex gap-2 mb-3">
                {(['simple', 'variable'] as const).map((t) => (
                  <button
                    key={t}
                    className={`btn btn-sm ${productType === t ? 'btn-primary' : 'btn-secondary'}`}
                    onClick={() => setProductType(t)}
                  >
                    {t.charAt(0).toUpperCase() + t.slice(1)}
                  </button>
                ))}
              </div>
              <div className="flex flex-col gap-2">
                <label className="flex items-center gap-2 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={downloadable}
                    onChange={(e) => setDownloadable(e.target.checked)}
                    className="form-check-input"
                  />
                  <span className="text-sm">Downloadable</span>
                </label>
                <label className="flex items-center gap-2 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={virtual}
                    onChange={(e) => setVirtual(e.target.checked)}
                    className="form-check-input"
                  />
                  <span className="text-sm">Virtual (no shipping required)</span>
                </label>
              </div>
            </div>
          </div>

          {/* Category */}
          <div className="card mb-4">
            <div className="card-header"><h3 className="card-title">Category</h3></div>
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
                  onClick={() => { setSelectedCategoryId(null); setSelectedCategoryName(''); }}
                >
                  <i className="ri-close-line" /> Clear
                </button>
              )}
            </div>
          </div>

          {/* Images */}
          <div className="card mb-4">
            <div className="card-header"><h3 className="card-title">Images</h3></div>
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
                      {i === 0 && (
                        <span className="image-badge">Main</span>
                      )}
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
              <p className="form-hint mb-2">
                Images are sent directly as URLs to WooCommerce.
                WooCommerce will download them automatically.
              </p>
            </div>
          </div>

        </div>
      </div>

      {/* ── Category Browser Modal ─────────────────────────────────────── */}
      {catBrowsing && (
        <div className="modal-overlay" onClick={() => setCatBrowsing(false)}>
          <div className="modal modal-lg" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <h3 className="modal-title">Select Category</h3>
              <button className="btn btn-ghost btn-sm" onClick={() => setCatBrowsing(false)}>
                <i className="ri-close-line" />
              </button>
            </div>
            <div className="modal-body">
              {browseLevels.length === 0 ? (
                <p className="text-[var(--text-muted)]">No categories found on this store.</p>
              ) : (
                <div className="category-browser">
                  {browseLevels.map((cats, level) => (
                    <div key={level} className="category-browser-col">
                      {cats.map((cat) => {
                        const hasChildren = (catTree.get(cat.id) || []).length > 0;
                        const isSelected = browseSel[level]?.id === cat.id;
                        return (
                          <button
                            key={cat.id}
                            className={`category-item ${isSelected ? 'selected' : ''}`}
                            onClick={() => selectBrowseNode(level, cat)}
                          >
                            <span className="category-name">{cat.name}</span>
                            {cat.count > 0 && (
                              <span className="category-count">{cat.count}</span>
                            )}
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
                  ? browseSel.filter(Boolean).map((c) => c!.name).join(' › ')
                  : 'No category selected'}
              </span>
              <div className="flex gap-2">
                <button className="btn btn-secondary" onClick={() => setCatBrowsing(false)}>Cancel</button>
                <button
                  className="btn btn-primary"
                  onClick={confirmCategorySelection}
                  disabled={!browseSel.some(Boolean)}
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

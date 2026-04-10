// ============================================================================
// MAGENTO 2 LISTING CREATE PAGE
// ============================================================================
// Arrives with ?product_id=xxx from product edit page.
// Covers: product name, SKU (required), description, short description,
// price, stock quantity, weight, category selector, status/visibility,
// product type, attribute set ID, images.

import { useState, useEffect } from 'react';
import { useSearchParams, useNavigate } from 'react-router-dom';
import { magentoApi, MagentoCategory, MagentoProduct, ChannelVariantDraft } from '../../services/magento-api';
import ConfiguratorSelector from './ConfiguratorSelector';
import { ConfiguratorDetail } from '../../services/configurator-api';
// ── Category tree helpers ─────────────────────────────────────────────────────

function buildCategoryTree(cats: MagentoCategory[]): Map<number, MagentoCategory[]> {
  const tree = new Map<number, MagentoCategory[]>();
  for (const cat of cats) {
    const siblings = tree.get(cat.parent_id) || [];
    siblings.push(cat);
    tree.set(cat.parent_id, siblings);
  }
  return tree;
}

function getCategoryPath(categoryId: number, cats: MagentoCategory[]): string {
  const catMap = new Map(cats.map((c) => [c.id, c]));
  const parts: string[] = [];
  let current: MagentoCategory | undefined = catMap.get(categoryId);
  while (current) {
    parts.unshift(current.name);
    current = current.parent_id ? catMap.get(current.parent_id) : undefined;
  }
  return parts.join(' › ');
}

// ── Magento status/visibility labels ─────────────────────────────────────────

const STATUS_OPTIONS = [
  { value: 1, label: 'Enabled' },
  { value: 2, label: 'Disabled' },
];

const VISIBILITY_OPTIONS = [
  { value: 4, label: 'Catalog & Search' },
  { value: 2, label: 'Catalog Only' },
  { value: 3, label: 'Search Only' },
  { value: 1, label: 'Not Visible' },
];

const TYPE_OPTIONS = [
  { value: 'simple', label: 'Simple' },
  { value: 'virtual', label: 'Virtual' },
  { value: 'downloadable', label: 'Downloadable' },
];

// ── Main Component ────────────────────────────────────────────────────────────

export default function MagentoListingCreate() {
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

  // Core fields
  const [sku, setSku] = useState('');
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [shortDescription, setShortDescription] = useState('');
  const [price, setPrice] = useState('');
  const [stockQuantity, setStockQuantity] = useState<number>(0);
  const [weight, setWeight] = useState('');

  // Product config
  const [status, setStatus] = useState(1);
  const [visibility, setVisibility] = useState(4);
  const [typeId, setTypeId] = useState('simple');
  const [attributeSetId, setAttributeSetId] = useState(4);
  const [images, setImages] = useState<string[]>([]);

  // Category
  const [allCategories, setAllCategories] = useState<MagentoCategory[]>([]);
  const [catTree, setCatTree] = useState<Map<number, MagentoCategory[]>>(new Map());
  const [selectedCategoryId, setSelectedCategoryId] = useState<number | null>(null);
  const [selectedCategoryName, setSelectedCategoryName] = useState('');
  const [catBrowsing, setCatBrowsing] = useState(false);
  const [browseLevels, setBrowseLevels] = useState<MagentoCategory[][]>([]);
  const [browseSel, setBrowseSel] = useState<(MagentoCategory | null)[]>([]);

  // Submission
  const [submitting, setSubmitting] = useState(false);
  const [submitResult, setSubmitResult] = useState<{
    ok: boolean;
    sku?: string;
    id?: number;
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
    // Pre-populate attribute set ID
    if (cfg.shipping_defaults?.attribute_set_id) {
      const asId = parseInt(cfg.shipping_defaults.attribute_set_id, 10);
      if (!isNaN(asId)) setAttributeSetId(asId);
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
        magentoApi.prepareDraft(productId!, credentialId || undefined),
        magentoApi.getCategories(credentialId || undefined),
      ]);

      if (prepRes.status === 'fulfilled' && prepRes.value.data?.ok) {
        const d = prepRes.value.data.draft;
        setSku(d.sku || '');
        setName(d.name || '');
        setDescription(d.description || '');
        setPrice(String(d.price || ''));
        setStockQuantity(d.stock_quantity ?? 0);
        setWeight(String(d.weight || ''));
        setImages(d.images || []);
        if (d.status) setStatus(d.status);
        if (d.visibility) setVisibility(d.visibility);
        if (d.type_id) setTypeId(d.type_id);
        if (d.attribute_set_id) setAttributeSetId(d.attribute_set_id);
        // VAR-01: load variants; auto-switch type_id to configurable
        if (d.variants && d.variants.length > 0) {
          setVariants(d.variants);
          setIsVariantProduct(true);
          setTypeId('configurable');
          setVisibility(4); // Catalog, Search
        }
      } else {
        setError(
          'Could not load product data. Make sure a Magento credential is connected.',
        );
      }

      if (catRes.status === 'fulfilled' && catRes.value.data?.ok) {
        const cats = (catRes.value.data.categories || []).filter((c) => c.is_active);
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
            channel: 'magento',
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
      const msg = err instanceof Error ? err.message : 'Failed to load Magento listing data';
      setError(msg);
    } finally {
      setLoading(false);
    }
  }

  // ── Category browser ──────────────────────────────────────────────────────

  // ENH-03: refresh categories on demand
  const [refreshingCats, setRefreshingCats] = useState(false);
  async function refreshCategories() {
    setRefreshingCats(true);
    try {
      const catRes = await magentoApi.getCategories(credentialId || undefined);
      if (catRes.data?.ok) {
        const cats = (catRes.data.categories || []).filter((c: any) => c.is_active);
        setAllCategories(cats);
        setCatTree(buildCategoryTree(cats));
      }
    } catch { /* ignore */ }
    finally { setRefreshingCats(false); }
  }

  function openCategoryBrowser() {
    // Root categories in Magento have parent_id=1 (Default Category) or parent_id=0 (true root)
    const roots =
      catTree.get(1) ||
      catTree.get(2) ||
      catTree.get(0) ||
      [];
    setBrowseLevels([roots]);
    setBrowseSel([null]);
    setCatBrowsing(true);
  }

  function selectBrowseNode(level: number, cat: MagentoCategory) {
    const newSel = browseSel.slice(0, level + 1);
    newSel[level] = cat;
    const newLevels = browseLevels.slice(0, level + 1);

    const children = (catTree.get(cat.id) || []).filter((c) => c.is_active);
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

  // ── Submit ────────────────────────────────────────────────────────────────

  async function handleSubmit() {
    if (!sku.trim()) {
      alert('SKU is required for Magento products');
      return;
    }
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
      const customAttributes: Array<{ attribute_code: string; value: unknown }> = [];

      if (description.trim()) {
        customAttributes.push({ attribute_code: 'description', value: description.trim() });
      }
      if (shortDescription.trim()) {
        customAttributes.push({ attribute_code: 'short_description', value: shortDescription.trim() });
      }
      if (selectedCategoryId) {
        customAttributes.push({ attribute_code: 'category_ids', value: [String(selectedCategoryId)] });
      }

      // URL key from name
      const urlKey = name
        .trim()
        .toLowerCase()
        .replace(/[^a-z0-9\s-]/g, '')
        .replace(/\s+/g, '-')
        .replace(/-+/g, '-');
      if (urlKey) {
        customAttributes.push({ attribute_code: 'url_key', value: urlKey });
      }

      const payload: MagentoProduct & { credential_id?: string; variants?: ChannelVariantDraft[] } = {
        sku: sku.trim(),
        name: name.trim(),
        price: priceNum,
        status,
        visibility,
        type_id: isVariantProduct ? 'configurable' : typeId, // VAR-01: force configurable
        attribute_set_id: attributeSetId,
        weight: weight ? parseFloat(weight) : undefined,
        extension_attributes: {
          stock_item: {
            qty: stockQuantity,
            is_in_stock: stockQuantity > 0,
            manage_stock: true,
          },
        },
        custom_attributes: customAttributes.length > 0 ? customAttributes : undefined,
        credential_id: credentialId || undefined,
        variants: isVariantProduct ? variants : undefined, // VAR-01
      };

      const res = await magentoApi.submit(payload);
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

  // ── Render ────────────────────────────────────────────────────────────────

  if (loading) {
    return (
      <div className="page">
        <div className="loading-state">
          <div className="spinner" />
          <p>Loading Magento listing data…</p>
        </div>
      </div>
    );
  }

  if (error && !name && !sku) {
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
          <div style={{ fontSize: 56, marginBottom: 16 }}>🏪</div>
          <h2 className="text-xl font-semibold mb-2">Magento Product Created!</h2>
          {submitResult.sku && (
            <p className="text-[var(--text-muted)] mb-1">
              SKU: <code>{submitResult.sku}</code>
            </p>
          )}
          {submitResult.id && (
            <p className="text-[var(--text-muted)] mb-4">
              Product ID: <code>{submitResult.id}</code>
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
            <i className="ri-store-line" style={{ color: '#f97316' }} />
            Create Magento 2 Listing
          </h1>
          {productId && (
            <p className="text-sm text-[var(--text-muted)]">Product ID: {productId}</p>
          )}
        </div>
        <div className="ml-auto flex gap-2 items-center">
          <select
            className="input input-sm"
            value={status}
            onChange={(e) => setStatus(Number(e.target.value))}
          >
            {STATUS_OPTIONS.map((opt) => (
              <option key={opt.value} value={opt.value}>
                {opt.label}
              </option>
            ))}
          </select>
          <button
            className="btn btn-primary"
            onClick={handleSubmit}
            disabled={submitting}
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
          <span style={{ fontWeight: 600 }}>🤖 AI is generating optimised content for Magento...</span>
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
      <ConfiguratorSelector channel="magento" credentialId={credentialId} onSelect={handleConfiguratorSelect} />

      {/* VAR-01 — Variant Grid */}
      {isVariantProduct && (
        <div className="card mb-4" style={{ borderLeft: '3px solid #d946ef' }}>
          <div className="card-header">
            <h3 className="card-title" style={{ color: '#d946ef' }}>Variants ({variants.length})</h3>
          </div>
          <div className="card-body">
            <p style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 12 }}>
              Magento supports configurable products. Each active variant will be created as a <strong>simple</strong> child product (visibility=Not Visible), then linked to a <strong>configurable</strong> parent product.
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
        style={{ borderLeft: '3px solid var(--primary)', background: 'var(--bg-secondary)' }}
      >
        <div className="card-body py-3">
          <p className="text-sm font-medium mb-2">
            <i className="ri-information-line mr-1" />
            Magento 2 Requirements
          </p>
          <div className="flex flex-wrap gap-3 text-sm">
            <span className={`flex items-center gap-1 ${sku ? 'text-green-400' : 'text-[var(--text-muted)]'}`}>
              <i className={sku ? 'ri-checkbox-circle-fill' : 'ri-checkbox-blank-circle-line'} />
              SKU (required)
            </span>
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
            <span className="flex items-center gap-1 text-[var(--text-muted)]">
              <i className="ri-checkbox-blank-circle-line" />
              Category (optional)
            </span>
          </div>
        </div>
      </div>

      <div className="listing-create-layout">
        {/* ── Left column: Core fields ─────────────────────────────────────── */}
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
                    SKU <span className="text-red-500">*</span>
                  </label>
                  <input
                    className="input"
                    value={sku}
                    onChange={(e) => setSku(e.target.value)}
                    placeholder="Unique stock keeping unit"
                  />
                  <p className="form-hint">
                    Must be unique across your Magento store. Used as the primary identifier.
                  </p>
                </div>
                <div className="form-group">
                  <label className="form-label">
                    Attribute Set ID
                  </label>
                  <input
                    type="number"
                    className="input"
                    value={attributeSetId}
                    onChange={(e) => setAttributeSetId(parseInt(e.target.value) || 4)}
                    min={1}
                  />
                  <p className="form-hint">Default is 4 (Default set). Find in Magento → Stores → Attributes → Attribute Set.</p>
                </div>
              </div>
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
            </div>
          </div>

          {/* Description */}
          <div className="card mb-4">
            <div className="card-header">
              <h3 className="card-title">Description</h3>
            </div>
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
            <div className="card-header">
              <h3 className="card-title">Pricing</h3>
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
                  <label className="form-label">Stock Quantity</label>
                  <input
                    type="number"
                    className="input"
                    value={stockQuantity}
                    onChange={(e) => setStockQuantity(parseInt(e.target.value) || 0)}
                    min="0"
                  />
                  <p className="form-hint">
                    Stock is managed via Magento's stock items API.
                  </p>
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
                <label className="form-label">Weight (kg)</label>
                <input
                  className="input"
                  value={weight}
                  onChange={(e) => setWeight(e.target.value)}
                  placeholder="0.00"
                  type="number"
                  min="0"
                  step="0.01"
                />
              </div>
            </div>
          </div>
        </div>

        {/* ── Right column ─────────────────────────────────────────────────── */}
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
                    className={`btn btn-sm ${typeId === opt.value ? 'btn-primary' : 'btn-secondary'}`}
                    onClick={() => setTypeId(opt.value)}
                  >
                    {opt.label}
                  </button>
                ))}
              </div>

              <label className="form-label">Visibility</label>
              <select
                className="input w-full"
                value={visibility}
                onChange={(e) => setVisibility(Number(e.target.value))}
              >
                {VISIBILITY_OPTIONS.map((opt) => (
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
                title="Re-fetch categories from Magento"
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
                Assigns the product to a Magento category tree node.
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
                Note: Magento image uploads require base64 encoding. Images from MarketMate
                URLs are stored as references. Upload directly in your Magento admin for
                full image management.
              </p>
            </div>
          </div>
        </div>
      </div>

      {/* ── Category Browser Modal ─────────────────────────────────────────── */}
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
              {browseLevels.length === 0 ? (
                <p className="text-[var(--text-muted)]">
                  No categories found on this Magento store.
                </p>
              ) : (
                <div className="category-browser">
                  {browseLevels.map((cats, level) => (
                    <div key={level} className="category-browser-col">
                      {cats.map((cat) => {
                        const hasChildren =
                          (catTree.get(cat.id) || []).filter((c) => c.is_active).length > 0;
                        const isSelected = browseSel[level]?.id === cat.id;
                        return (
                          <button
                            key={cat.id}
                            className={`category-item ${isSelected ? 'selected' : ''}`}
                            onClick={() => selectBrowseNode(level, cat)}
                          >
                            <span className="category-name">{cat.name}</span>
                            {cat.product_count != null && cat.product_count > 0 && (
                              <span className="category-count">{cat.product_count}</span>
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
                  ? browseSel
                      .filter(Boolean)
                      .map((c) => c!.name)
                      .join(' › ')
                  : 'No category selected'}
              </span>
              <div className="flex gap-2">
                <button
                  className="btn btn-secondary"
                  onClick={() => setCatBrowsing(false)}
                >
                  Cancel
                </button>
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

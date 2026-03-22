// ============================================================================
// TIKTOK SHOP LISTING CREATE PAGE
// ============================================================================
// Arrives with ?product_id=xxx from product edit page.
// Covers: category selection, attribute mapping, image upload to TikTok CDN,
// SKU/price/inventory, shipping template, and final submission.

import { useState, useEffect, useCallback } from 'react';
import { useSearchParams, useNavigate } from 'react-router-dom';
import {
  tiktokApi,
  TikTokCategory,
  TikTokAttribute,
  TikTokBrand,
  TikTokShippingTemplate,
  TikTokWarehouse,
  TikTokDraft,
  TikTokSubmitPayload,
  ChannelVariantDraft,
} from '../../services/tiktok-api';
import ConfiguratorSelector from './ConfiguratorSelector';
import { ConfiguratorDetail } from '../../services/configurator-api';

// ── Helpers ───────────────────────────────────────────────────────────────────

function buildCategoryTree(categories: TikTokCategory[]): Map<number, TikTokCategory[]> {
  const tree = new Map<number, TikTokCategory[]>();
  for (const cat of categories) {
    const children = tree.get(cat.parent_id) || [];
    children.push(cat);
    tree.set(cat.parent_id, children);
  }
  return tree;
}

// ── Main Component ────────────────────────────────────────────────────────────

export default function TikTokListingCreate() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const productId = searchParams.get('product_id');
  const credentialId = searchParams.get('credential_id') || '';

  // ── State ─────────────────────────────────────────────────────────────────

  const [loading, setLoading] = useState(true);
  const [aiGenerating, setAiGenerating] = useState(false);
  const [aiApplied, setAiApplied] = useState(false);
  const [aiError, setAiError] = useState('');
  const [error, setError] = useState('');
  const [draft, setDraft] = useState<TikTokDraft | null>(null);

  // Form fields
  const [title, setTitle] = useState('');
  const [description, setDescription] = useState('');
  const [price, setPrice] = useState('');
  const [quantity, setQuantity] = useState(0);
  const [sellerSku, setSellerSku] = useState('');
  const [brandId, setBrandId] = useState('');
  const [shippingTemplateId, setShippingTemplateId] = useState('');
  const [warehouseId, setWarehouseId] = useState('');
  const [weightKg, setWeightKg] = useState('');
  const [lengthCm, setLengthCm] = useState('');
  const [widthCm, setWidthCm] = useState('');
  const [heightCm, setHeightCm] = useState('');

  // Category
  const [allCategories, setAllCategories] = useState<TikTokCategory[]>([]);
  const [catTree, setCatTree] = useState<Map<number, TikTokCategory[]>>(new Map());
  const [catPath, setCatPath] = useState<TikTokCategory[]>([]); // breadcrumb path
  const [selectedCatId, setSelectedCatId] = useState<number>(0);
  const [catBrowsing, setCatBrowsing] = useState(false);
  const [browseLevel, setBrowseLevel] = useState<TikTokCategory[][]>([]); // columns
  const [browseSel, setBrowseSel] = useState<(TikTokCategory | null)[]>([]);

  // Attributes (leaf category)
  const [attributes, setAttributes] = useState<TikTokAttribute[]>([]);
  const [attrValues, setAttrValues] = useState<Record<number, string | number>>({});

  // Brands, shipping, warehouses
  const [brands, setBrands] = useState<TikTokBrand[]>([]);
  const [shippingTemplates, setShippingTemplates] = useState<TikTokShippingTemplate[]>([]);
  const [warehouses, setWarehouses] = useState<TikTokWarehouse[]>([]);

  // Images
  const [images, setImages] = useState<string[]>([]);
  const [uploadedUris, setUploadedUris] = useState<{ uri: string; original: string }[]>([]);
  const [uploadingImages, setUploadingImages] = useState(false);

  // Submission
  const [submitting, setSubmitting] = useState(false);
  const [submitResult, setSubmitResult] = useState<{
    ok: boolean;
    product_id?: string;
    error?: string;
  } | null>(null);

  // ── Configurator (CFG-07) ──
  const [selectedConfigurator, setSelectedConfigurator] = useState<ConfiguratorDetail | null>(null);

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
      if (!isNaN(catIdNum)) setSelectedCatId(catIdNum);
    }
    // Pre-populate attribute defaults
    if (cfg.attribute_defaults && cfg.attribute_defaults.length > 0) {
      const extras: Record<number, string> = {};
      for (const attr of cfg.attribute_defaults) {
        const numId = parseInt(attr.attribute_name, 10);
        if (!isNaN(numId) && attr.source === 'default_value' && attr.default_value) {
          extras[numId] = attr.default_value;
        }
      }
      if (Object.keys(extras).length > 0) {
        setAttrValues(prev => ({ ...prev, ...extras }));
      }
    }
    // Pre-populate shipping template
    if (cfg.shipping_defaults?.shipping_template_id) {
      setShippingTemplateId(cfg.shipping_defaults.shipping_template_id);
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
      const [prepRes, catRes, brandRes, shipRes, wareRes] = await Promise.allSettled([
        tiktokApi.prepare({ product_id: productId!, credential_id: credentialId || undefined }),
        tiktokApi.getCategories(credentialId || undefined),
        tiktokApi.getBrands(credentialId || undefined),
        tiktokApi.getShippingTemplates(credentialId || undefined),
        tiktokApi.getWarehouses(credentialId || undefined),
      ]);

      // Product draft
      if (prepRes.status === 'fulfilled' && prepRes.value.data?.ok) {
        const d = prepRes.value.data.draft;
        setDraft(d);
        setTitle(d.title || '');
        setDescription(d.description || '');
        setSellerSku(d.sku || '');
        setPrice(String(d.price || ''));
        setQuantity(d.quantity || 0);
        setImages(d.images || []);
        setWeightKg(d.weight_kg || '');
        setLengthCm(d.length_cm || '');
        setWidthCm(d.width_cm || '');
        setHeightCm(d.height_cm || '');
        if (d.category_id) {
          setSelectedCatId(d.category_id);
        }
        // VAR-01: load variants
        if (d.variants && d.variants.length > 0) {
          setVariants(d.variants);
          setIsVariantProduct(true);
        }
      } else {
        setError('Could not load product data. Make sure a TikTok credential is connected.');
      }

      // Categories
      if (catRes.status === 'fulfilled' && catRes.value.data?.ok) {
        const cats = catRes.value.data.categories;
        setAllCategories(cats);
        setCatTree(buildCategoryTree(cats));
      }

      // Brands
      if (brandRes.status === 'fulfilled' && brandRes.value.data?.ok) {
        setBrands(brandRes.value.data.brands);
      }

      // Shipping templates
      if (shipRes.status === 'fulfilled' && shipRes.value.data?.ok) {
        const tmplts = shipRes.value.data.templates;
        setShippingTemplates(tmplts);
        if (tmplts.length > 0) setShippingTemplateId(tmplts[0].template_id);
      }

      // Warehouses
      if (wareRes.status === 'fulfilled' && wareRes.value.data?.ok) {
        const wares = wareRes.value.data.warehouses;
        setWarehouses(wares);
        if (wares.length > 0) setWarehouseId(wares[0].id);
      }


      const aiFlag = searchParams.get('ai');
      if (aiFlag === 'pending' && productId) {
        setAiGenerating(true);
        try {
          const { aiService: aiApi } = await import('../../services/ai-api');
          const schemaFields = [
            { name: 'title', display_name: 'Title', data_type: 'string', required: true, allowed_values: [], max_length: 255 },
            { name: 'description', display_name: 'Description', data_type: 'string', required: true, allowed_values: [], max_length: 0 }
          ];
          const aiRes = await aiApi.generateWithSchema({
            product_id: productId,
            channel: 'tiktok',
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
      setError(err.message || 'Failed to load TikTok listing data');
    } finally {
      setLoading(false);
    }
  }

  // ── Category fetch attributes ─────────────────────────────────────────────

  const loadAttributes = useCallback(async (catId: number) => {
    if (!catId) return;
    try {
      const res = await tiktokApi.getCategoryAttributes(catId, credentialId || undefined);
      if (res.data?.ok) {
        setAttributes(res.data.attributes || []);
        setAttrValues({});
      }
    } catch {
      // non-fatal
    }
  }, [credentialId]);

  useEffect(() => {
    if (selectedCatId) loadAttributes(selectedCatId);
  }, [selectedCatId]);

  // ── Category browser ──────────────────────────────────────────────────────

  function openCategoryBrowser() {
    const roots = catTree.get(0) || [];
    setBrowseLevel([roots]);
    setBrowseSel([null]);
    setCatBrowsing(true);
  }

  function selectBrowseCategory(level: number, cat: TikTokCategory) {
    const newSel = browseSel.slice(0, level + 1);
    newSel[level] = cat;
    const newLevels = browseLevel.slice(0, level + 1);

    if (!cat.is_leaf) {
      const children = catTree.get(cat.id) || [];
      if (children.length > 0) {
        newLevels.push(children);
        newSel.push(null);
      }
    }

    setBrowseLevel(newLevels);
    setBrowseSel(newSel);

    if (cat.is_leaf) {
      // Confirm selection
      setSelectedCatId(cat.id);
      const path: TikTokCategory[] = [];
      for (let i = 0; i <= level; i++) {
        if (newSel[i]) path.push(newSel[i]!);
      }
      setCatPath(path);
      setCatBrowsing(false);
    }
  }

  // ── Image upload ──────────────────────────────────────────────────────────

  async function uploadImagesToTikTok() {
    if (images.length === 0) return;
    setUploadingImages(true);
    const uploaded: { uri: string; original: string }[] = [];
    for (const imgUrl of images) {
      try {
        const res = await tiktokApi.uploadImage(imgUrl);
        if (res.data?.ok && res.data.image?.uri) {
          uploaded.push({ uri: res.data.image.uri, original: imgUrl });
        }
      } catch {
        // skip failed images
      }
    }
    setUploadedUris(uploaded);
    setUploadingImages(false);
    return uploaded;
  }

  // ── Submit ────────────────────────────────────────────────────────────────

  async function handleSubmit() {
    if (!title.trim()) { alert('Title is required'); return; }
    if (!selectedCatId) { alert('Please select a category'); return; }
    if (!price || parseFloat(price) <= 0) { alert('Price must be greater than 0'); return; }
    if (images.length === 0) { alert('At least one product image is required'); return; }

    setSubmitting(true);
    setSubmitResult(null);

    try {
      // Upload images first
      const uris = uploadedUris.length > 0 ? uploadedUris : (await uploadImagesToTikTok() || []);

      if (uris.length === 0) {
        setSubmitResult({ ok: false, error: 'Failed to upload images to TikTok. Please check your connection.' });
        setSubmitting(false);
        return;
      }

      // Build attributes array
      const productAttributes = attributes
        .filter((a) => attrValues[a.id] !== undefined && attrValues[a.id] !== '')
        .map((a) => ({
          id: a.id,
          values: [{ name: String(attrValues[a.id]) }],
        }));

      // Build SKUs — if multi-variant, send variants and let backend build skus[];
      // otherwise build a single SKU from the form fields as before.
      const activeVariants = variants.filter(v => v.active);
      const skus: TikTokSubmitPayload['skus'] = isVariantProduct && activeVariants.length >= 2
        ? [] // backend will build from variants[]
        : [
            {
              seller_sku: sellerSku,
              price: { currency: 'GBP', original_price: parseFloat(price).toFixed(2) },
              inventory: warehouseId
                ? [{ quantity, warehouse_id: warehouseId }]
                : [],
            },
          ];

      const payload: TikTokSubmitPayload = {
        title: title.trim(),
        description: description.trim(),
        category_id: selectedCatId,
        brand_id: brandId || undefined,
        main_images: uris.map((u) => ({ uri: u.uri })),
        skus,
        shipping_template_id: shippingTemplateId || undefined,
        product_attributes: productAttributes.length > 0 ? productAttributes : undefined,
        variants: isVariantProduct ? variants : undefined, // VAR-01
      };

      if (weightKg) {
        payload.package_weight = { unit: 'KILOGRAM', value: weightKg };
      }
      if (lengthCm && widthCm && heightCm) {
        payload.package_dimensions = {
          length: lengthCm,
          width: widthCm,
          height: heightCm,
          unit: 'CENTIMETER',
        };
      }

      const res = await tiktokApi.submit(payload);
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
          <p>Loading TikTok listing data…</p>
        </div>
      </div>
    );
  }

  if (error && !draft) {
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
          <div style={{ fontSize: 56, marginBottom: 16 }}>✅</div>
          <h2 className="text-xl font-semibold mb-2">TikTok Listing Created!</h2>
          <p className="text-[var(--text-muted)] mb-1">Product ID: <code>{submitResult.product_id}</code></p>
          <p className="text-[var(--text-muted)] mb-6">Your product is now live on TikTok Shop.</p>
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
            <i className="ri-tiktok-fill" style={{ color: '#00f2ea' }} />
            Create TikTok Shop Listing
          </h1>
          {productId && (
            <p className="text-sm text-[var(--text-muted)]">Product ID: {productId}</p>
          )}
        </div>
        <div className="ml-auto">
          <button
            className="btn btn-primary"
            onClick={handleSubmit}
            disabled={submitting}
          >
            {submitting ? (
              <><span className="spinner-sm mr-2" />Publishing…</>
            ) : (
              <><i className="ri-send-plane-line mr-2" />Publish to TikTok Shop</>
            )}
          </button>
        </div>
      </div>

      {submitResult && !submitResult.ok && (
        <div className="alert alert-error mb-4">
          <i className="ri-error-warning-line" />
          {submitResult.error}
        </div>

      )}
      {aiGenerating && (
        <div style={{ padding: '12px 14px', background: 'linear-gradient(90deg, rgba(139,92,246,0.15), rgba(59,130,246,0.15))', borderRadius: 8, fontSize: 13, color: 'var(--accent-purple)', marginBottom: 16, display: 'flex', gap: 10, alignItems: 'center', border: '1px solid rgba(139,92,246,0.3)' }}>
          <div className="spinner" style={{ width: 16, height: 16, border: '2px solid var(--accent-purple)', borderTopColor: 'transparent', borderRadius: '50%', animation: 'spin 1s linear infinite' }} />
          <span style={{ fontWeight: 600 }}>🤖 AI is generating optimised content for TikTok...</span>
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

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* Left column — main info */}
        <div className="lg:col-span-2 space-y-6">

          {/* ── Configurator (CFG-07) ── */}
          <ConfiguratorSelector channel="tiktok" credentialId={credentialId} onSelect={handleConfiguratorSelect} />

          {/* VAR-01 — Variant Grid */}
          {isVariantProduct && (
            <div className="card" style={{ borderLeft: '3px solid #d946ef', marginBottom: 16 }}>
              <div className="card-header">
                <h3 className="card-title" style={{ color: '#d946ef' }}>Variants ({variants.length})</h3>
              </div>
              <div className="card-body">
                <p style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 12 }}>
                  TikTok Shop supports native multi-SKU products. Each active variant will be submitted as a separate SKU with its own price and inventory in a single product listing.
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

          {/* Core fields */}
          <div className="card">
            <div className="card-header">
              <h3 className="card-title">Product Details</h3>
            </div>
            <div className="card-body space-y-4">
              <div>
                <label className="label">Title <span className="text-red-500">*</span></label>
                <input
                  className="input w-full"
                  value={title}
                  onChange={(e) => setTitle(e.target.value)}
                  maxLength={255}
                  placeholder="Product title"
                />
                <p className="text-xs text-[var(--text-muted)] mt-1">{title.length}/255</p>
              </div>

              <div>
                <label className="label">Description</label>
                <textarea
                  className="input w-full resize-none"
                  rows={6}
                  value={description}
                  onChange={(e) => setDescription(e.target.value)}
                  placeholder="Describe the product — include key features, materials, sizing info, etc."
                />
              </div>
            </div>
          </div>

          {/* Category */}
          <div className="card">
            <div className="card-header">
              <h3 className="card-title">Category <span className="text-red-500">*</span></h3>
            </div>
            <div className="card-body">
              {selectedCatId ? (
                <div className="flex items-center justify-between">
                  <div>
                    <div className="flex items-center gap-2 flex-wrap text-sm text-[var(--text-muted)]">
                      {catPath.map((c, i) => (
                        <span key={c.id} className="flex items-center gap-1">
                          {i > 0 && <i className="ri-arrow-right-s-line" />}
                          <span className={i === catPath.length - 1 ? 'text-[var(--text-primary)] font-medium' : ''}>
                            {c.local_name}
                          </span>
                        </span>
                      ))}
                      {catPath.length === 0 && (
                        <span className="text-[var(--text-primary)] font-medium">
                          Category ID: {selectedCatId}
                        </span>
                      )}
                    </div>
                  </div>
                  <button className="btn btn-secondary btn-sm" onClick={openCategoryBrowser}>
                    Change
                  </button>
                </div>
              ) : (
                <button className="btn btn-secondary w-full" onClick={openCategoryBrowser}>
                  <i className="ri-folder-open-line mr-2" />
                  Browse Categories
                </button>
              )}

              {catBrowsing && (
                <div className="mt-4 border border-[var(--border)] rounded-lg overflow-auto">
                  <div className="flex divide-x divide-[var(--border)]" style={{ minHeight: 240 }}>
                    {browseLevel.map((levelCats, li) => (
                      <div key={li} className="min-w-[200px] overflow-y-auto max-h-64">
                        {levelCats.map((cat) => (
                          <button
                            key={cat.id}
                            onClick={() => selectBrowseCategory(li, cat)}
                            className={`w-full text-left px-3 py-2 text-sm hover:bg-[var(--bg-secondary)] flex items-center justify-between ${
                              browseSel[li]?.id === cat.id ? 'bg-[var(--bg-secondary)] text-[var(--accent)]' : ''
                            }`}
                          >
                            <span>{cat.local_name}</span>
                            {!cat.is_leaf && <i className="ri-arrow-right-s-line text-xs opacity-50" />}
                            {cat.is_leaf && <span className="text-xs text-green-500">✓</span>}
                          </button>
                        ))}
                      </div>
                    ))}
                  </div>
                  <div className="p-2 border-t border-[var(--border)]">
                    <button className="btn btn-ghost btn-sm" onClick={() => setCatBrowsing(false)}>
                      Cancel
                    </button>
                  </div>
                </div>
              )}
            </div>
          </div>

          {/* Category Attributes */}
          {attributes.length > 0 && (
            <div className="card">
              <div className="card-header">
                <h3 className="card-title">Category Attributes</h3>
                <p className="card-subtitle text-xs text-[var(--text-muted)]">
                  {attributes.filter((a) => a.is_mandatory).length} required
                </p>
              </div>
              <div className="card-body space-y-3">
                {attributes.map((attr) => (
                  <div key={attr.id}>
                    <label className="label text-sm">
                      {attr.name}
                      {attr.is_mandatory && <span className="text-red-500 ml-1">*</span>}
                    </label>
                    {attr.values.length > 0 ? (
                      <select
                        className="input w-full"
                        value={attrValues[attr.id] || ''}
                        onChange={(e) => setAttrValues((prev) => ({ ...prev, [attr.id]: e.target.value }))}
                      >
                        <option value="">— Select —</option>
                        {attr.values.map((v) => (
                          <option key={v.id} value={v.name}>{v.name}</option>
                        ))}
                      </select>
                    ) : (
                      <input
                        type="text"
                        className="input w-full"
                        value={String(attrValues[attr.id] || '')}
                        onChange={(e) => setAttrValues((prev) => ({ ...prev, [attr.id]: e.target.value }))}
                        placeholder={`Enter ${attr.name.toLowerCase()}`}
                      />
                    )}
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Images */}
          <div className="card">
            <div className="card-header">
              <h3 className="card-title">Product Images <span className="text-red-500">*</span></h3>
              <p className="card-subtitle text-xs text-[var(--text-muted)]">
                Min 1 image required. 9:16 ratio recommended for best TikTok performance.
              </p>
            </div>
            <div className="card-body">
              {images.length === 0 ? (
                <div className="empty-state py-6">
                  <i className="ri-image-line text-3xl opacity-30 mb-2" />
                  <p className="text-sm text-[var(--text-muted)]">
                    No images found on the product. Please add images to the product first.
                  </p>
                </div>
              ) : (
                <>
                  <div className="grid grid-cols-4 gap-3 mb-4">
                    {images.map((img, i) => (
                      <div key={i} className="relative aspect-square rounded-lg overflow-hidden border border-[var(--border)]">
                        <img src={img} alt={`Product ${i + 1}`} className="w-full h-full object-cover" />
                        {uploadedUris.find((u) => u.original === img) && (
                          <div className="absolute top-1 right-1 bg-green-500 rounded-full w-5 h-5 flex items-center justify-center">
                            <i className="ri-check-line text-white text-xs" />
                          </div>
                        )}
                      </div>
                    ))}
                  </div>
                  <button
                    className="btn btn-secondary w-full"
                    onClick={uploadImagesToTikTok}
                    disabled={uploadingImages}
                  >
                    {uploadingImages ? (
                      <><span className="spinner-sm mr-2" />Uploading to TikTok CDN…</>
                    ) : uploadedUris.length > 0 ? (
                      <><i className="ri-check-line mr-2 text-green-500" />{uploadedUris.length}/{images.length} images uploaded</>
                    ) : (
                      <><i className="ri-upload-cloud-line mr-2" />Upload Images to TikTok CDN</>
                    )}
                  </button>
                  <p className="text-xs text-[var(--text-muted)] mt-2">
                    Images are automatically uploaded when you publish.
                  </p>
                </>
              )}
            </div>
          </div>
        </div>

        {/* Right column — pricing, inventory, logistics */}
        <div className="space-y-6">

          {/* Pricing & Inventory */}
          <div className="card">
            <div className="card-header">
              <h3 className="card-title">Pricing & Stock</h3>
            </div>
            <div className="card-body space-y-4">
              <div>
                <label className="label">Price (GBP) <span className="text-red-500">*</span></label>
                <div className="relative">
                  <span className="absolute left-3 top-1/2 -translate-y-1/2 text-[var(--text-muted)]">£</span>
                  <input
                    type="number"
                    step="0.01"
                    min="0.01"
                    className="input w-full pl-7"
                    value={price}
                    onChange={(e) => setPrice(e.target.value)}
                    placeholder="0.00"
                  />
                </div>
              </div>

              <div>
                <label className="label">Stock Quantity <span className="text-red-500">*</span></label>
                <input
                  type="number"
                  min="0"
                  className="input w-full"
                  value={quantity}
                  onChange={(e) => setQuantity(parseInt(e.target.value) || 0)}
                />
              </div>

              <div>
                <label className="label">Seller SKU</label>
                <input
                  className="input w-full font-mono text-sm"
                  value={sellerSku}
                  onChange={(e) => setSellerSku(e.target.value)}
                  placeholder="Your internal SKU"
                />
              </div>

              {warehouses.length > 1 && (
                <div>
                  <label className="label">Fulfilment Warehouse</label>
                  <select
                    className="input w-full"
                    value={warehouseId}
                    onChange={(e) => setWarehouseId(e.target.value)}
                  >
                    {warehouses.map((w) => (
                      <option key={w.id} value={w.id}>{w.name}</option>
                    ))}
                  </select>
                </div>
              )}
            </div>
          </div>

          {/* Brand */}
          <div className="card">
            <div className="card-header">
              <h3 className="card-title">Brand</h3>
            </div>
            <div className="card-body">
              <select
                className="input w-full"
                value={brandId}
                onChange={(e) => setBrandId(e.target.value)}
              >
                <option value="">No brand / Generic</option>
                {brands.map((b) => (
                  <option key={b.id} value={b.id}>{b.brand_name}</option>
                ))}
              </select>
            </div>
          </div>

          {/* Shipping */}
          <div className="card">
            <div className="card-header">
              <h3 className="card-title">Shipping Template</h3>
            </div>
            <div className="card-body">
              {shippingTemplates.length === 0 ? (
                <p className="text-sm text-[var(--text-muted)]">
                  No shipping templates found. Please set up shipping templates in TikTok Seller Center.
                </p>
              ) : (
                <select
                  className="input w-full"
                  value={shippingTemplateId}
                  onChange={(e) => setShippingTemplateId(e.target.value)}
                >
                  {shippingTemplates.map((t) => (
                    <option key={t.template_id} value={t.template_id}>{t.name}</option>
                  ))}
                </select>
              )}
            </div>
          </div>

          {/* Package Dimensions */}
          <div className="card">
            <div className="card-header">
              <h3 className="card-title">Package Dimensions</h3>
              <p className="card-subtitle text-xs text-[var(--text-muted)]">Optional but recommended</p>
            </div>
            <div className="card-body space-y-3">
              <div>
                <label className="label">Weight (kg)</label>
                <input
                  type="number"
                  step="0.001"
                  min="0"
                  className="input w-full"
                  value={weightKg}
                  onChange={(e) => setWeightKg(e.target.value)}
                  placeholder="0.000"
                />
              </div>
              <div className="grid grid-cols-3 gap-2">
                <div>
                  <label className="label text-xs">L (cm)</label>
                  <input
                    type="number"
                    className="input w-full"
                    value={lengthCm}
                    onChange={(e) => setLengthCm(e.target.value)}
                    placeholder="0"
                  />
                </div>
                <div>
                  <label className="label text-xs">W (cm)</label>
                  <input
                    type="number"
                    className="input w-full"
                    value={widthCm}
                    onChange={(e) => setWidthCm(e.target.value)}
                    placeholder="0"
                  />
                </div>
                <div>
                  <label className="label text-xs">H (cm)</label>
                  <input
                    type="number"
                    className="input w-full"
                    value={heightCm}
                    onChange={(e) => setHeightCm(e.target.value)}
                    placeholder="0"
                  />
                </div>
              </div>
            </div>
          </div>

          {/* Submit button (also in header, this is a convenience repeat) */}
          <button
            className="btn btn-primary w-full"
            onClick={handleSubmit}
            disabled={submitting}
          >
            {submitting ? (
              <><span className="spinner-sm mr-2" />Publishing…</>
            ) : (
              <><i className="ri-tiktok-fill mr-2" />Publish to TikTok Shop</>
            )}
          </button>
        </div>
      </div>
    </div>
  );
}

// ============================================================================
// ONBUY LISTING CREATE PAGE
// ============================================================================
// Arrives with ?product_id=xxx from product edit page.
// Covers: OPC, SKU, description, price, stock, condition, delivery template,
// category browser, site_id.

import { useState, useEffect } from 'react';
import { useSearchParams, useNavigate } from 'react-router-dom';
import { onbuyApi, OnBuyCategory, OnBuyCondition, OnBuyListing, ChannelVariantDraft } from '../../services/onbuy-api';
import ConfiguratorSelector from './ConfiguratorSelector';
import { ConfiguratorDetail } from '../../services/configurator-api';

// ── Category helpers ──────────────────────────────────────────────────────────

function buildCategoryTree(cats: OnBuyCategory[]): Map<number, OnBuyCategory[]> {
  const tree = new Map<number, OnBuyCategory[]>();
  for (const cat of cats) {
    const siblings = tree.get(cat.parent_id) || [];
    siblings.push(cat);
    tree.set(cat.parent_id, siblings);
  }
  return tree;
}

function getCategoryPath(categoryId: number, cats: OnBuyCategory[]): string {
  const catMap = new Map(cats.map((c) => [c.category_id, c]));
  const parts: string[] = [];
  let current: OnBuyCategory | undefined = catMap.get(categoryId);
  while (current) {
    parts.unshift(current.name);
    current = current.parent_id ? catMap.get(current.parent_id) : undefined;
  }
  return parts.join(' › ');
}

// ── Main Component ────────────────────────────────────────────────────────────

export default function OnBuyListingCreate() {
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
  const [opc, setOpc] = useState('');
  const [sku, setSku] = useState('');
  const [description, setDescription] = useState('');
  const [price, setPrice] = useState('');
  const [stock, setStock] = useState<number>(0);
  const [conditionId, setConditionId] = useState('new');
  const [deliveryTemplateId, setDeliveryTemplateId] = useState('');
  const [siteId, setSiteId] = useState(2000);

  // Metadata
  const [conditions, setConditions] = useState<OnBuyCondition[]>([]);
  const [allCategories, setAllCategories] = useState<OnBuyCategory[]>([]);
  const [catTree, setCatTree] = useState<Map<number, OnBuyCategory[]>>(new Map());
  const [selectedCategoryId, setSelectedCategoryId] = useState<number | null>(null);
  const [selectedCategoryName, setSelectedCategoryName] = useState('');
  const [catBrowsing, setCatBrowsing] = useState(false);
  const [browseLevels, setBrowseLevels] = useState<OnBuyCategory[][]>([]);
  const [browseSel, setBrowseSel] = useState<(OnBuyCategory | null)[]>([]);

  // Submission
  const [submitting, setSubmitting] = useState(false);
  const [submitResult, setSubmitResult] = useState<{
    ok: boolean;
    listing_id?: string;
    error?: string;
  } | null>(null);

  // ── Configurator (CFG-07) ──
  const [_selectedConfigurator, setSelectedConfigurator] = useState<ConfiguratorDetail | null>(null);

  // VAR-01 — Variation listings
  const [variants, setVariants] = useState<ChannelVariantDraft[]>([]);
  const [isVariantProduct, setIsVariantProduct] = useState(false);
  const [variantSubmitResults, setVariantSubmitResults] = useState<{ sku: string; listingId?: string; error?: string }[]>([]);
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
    // Pre-populate delivery template
    if (cfg.shipping_defaults?.delivery_template_id) {
      setDeliveryTemplateId(cfg.shipping_defaults.delivery_template_id);
    }
  };

  // ── Load draft ─────────────────────────────────────────────────────────────

  useEffect(() => {
    if (!productId) {
      setError('No product_id provided');
      setLoading(false);
      return;
    }

    async function loadData() {
      try {
        const [draftRes, conditionsRes, categoriesRes] = await Promise.all([
          onbuyApi.prepareDraft(productId!, credentialId || undefined),
          onbuyApi.getConditions(credentialId || undefined),
          onbuyApi.getCategories(credentialId || undefined),
        ]);

        if (draftRes.data?.ok && draftRes.data.draft) {
          const d = draftRes.data.draft;
          setOpc(d.opc || '');
          setSku(d.sku || '');
          setDescription(d.description || '');
          setPrice(d.price ? String(d.price) : '');
          setStock(d.stock || 0);
          setConditionId(d.condition_id || 'new');
          setDeliveryTemplateId(d.delivery_template_id ? String(d.delivery_template_id) : '');
          setSiteId(d.site_id || 2000);
          // VAR-01: load variants
          if (d.variants && d.variants.length > 0) {
            setVariants(d.variants);
            setIsVariantProduct(true);
          }
        }

        if (conditionsRes.data?.conditions) {
          setConditions(conditionsRes.data.conditions);
        }

        if (categoriesRes.data?.categories) {
          const cats = categoriesRes.data.categories;
          setAllCategories(cats);
          const tree = buildCategoryTree(cats);
          setCatTree(tree);
          const roots = tree.get(0) || [];
          setBrowseLevels([roots]);
          setBrowseSel([null]);
        }
      } catch (err: any) {
        setError(err.message || 'Failed to load product data');
      } finally {
        setLoading(false);
      }
    }

    loadData();
  }, [productId, credentialId]);

  // ── Category browser ───────────────────────────────────────────────────────

  function handleCatSelect(levelIdx: number, cat: OnBuyCategory) {
    const newSel = [...browseSel.slice(0, levelIdx + 1)];
    newSel[levelIdx] = cat;
    const newLevels = [...browseLevels.slice(0, levelIdx + 1)];

    if (cat.has_children) {
      const children = catTree.get(cat.category_id) || [];
      if (children.length > 0) {
        newLevels.push(children);
        newSel.push(null);
      }
    }

    setBrowseLevels(newLevels);
    setBrowseSel(newSel);
    setSelectedCategoryId(cat.category_id);
    setSelectedCategoryName(getCategoryPath(cat.category_id, allCategories));
  }

  function applyCategory() {
    setCatBrowsing(false);
  }

  // ── Submit ─────────────────────────────────────────────────────────────────

  async function handleSubmit() {
    setSubmitting(true);
    setError('');
    setSubmitResult(null);
    setVariantSubmitResults([]);

    // VAR-01: multi-variant path — one listing per active variant
    const activeVariants = variants.filter(v => v.active);
    if (isVariantProduct && activeVariants.length >= 2) {
      const results: { sku: string; listingId?: string; error?: string }[] = [];
      for (const v of activeVariants) {
        const varPrice = parseFloat(v.price) > 0 ? parseFloat(v.price) : parseFloat(price);
        const varStock = parseInt(v.stock) >= 0 ? parseInt(v.stock) : stock;
        try {
          const payload: OnBuyListing & { credential_id?: string } = {
            opc: opc.trim(),
            sku: v.sku.trim(),
            description: description.trim(),
            price: varPrice,
            stock: varStock,
            condition_id: conditionId,
            delivery_template_id: deliveryTemplateId ? parseInt(deliveryTemplateId, 10) : undefined,
            site_id: siteId,
            credential_id: credentialId || undefined,
          };
          const res = await onbuyApi.submit(payload);
          if (res.data?.ok) {
            results.push({ sku: v.sku, listingId: res.data.listing_id });
          } else {
            results.push({ sku: v.sku, error: res.data?.error || 'Failed' });
          }
        } catch (err: any) {
          results.push({ sku: v.sku, error: err.response?.data?.error || err.message || 'Failed' });
        }
      }
      setVariantSubmitResults(results);
      setSubmitting(false);
      return;
    }

    // Single listing path (original)
    if (!opc.trim()) {
      setError('OnBuy Product Code (OPC) is required');
      setSubmitting(false);
      return;
    }
    if (!price || parseFloat(price) <= 0) {
      setError('Price must be greater than 0');
      setSubmitting(false);
      return;
    }

    try {
      const payload: OnBuyListing & { credential_id?: string } = {
        opc: opc.trim(),
        sku: sku.trim(),
        description: description.trim(),
        price: parseFloat(price),
        stock,
        condition_id: conditionId,
        delivery_template_id: deliveryTemplateId ? parseInt(deliveryTemplateId, 10) : undefined,
        site_id: siteId,
        credential_id: credentialId || undefined,
      };

      const res = await onbuyApi.submit(payload);
      if (res.data?.ok) {
        setSubmitResult({ ok: true, listing_id: res.data.listing_id });
      } else {
        setSubmitResult({ ok: false, error: res.data?.error || 'Submission failed' });
      }


      const aiFlag = searchParams.get('ai');
      if (aiFlag === 'pending' && productId) {
        setAiGenerating(true);
        try {
          const { aiService: aiApi } = await import('../../services/ai-api');
          const schemaFields = [
            { name: 'title', display_name: 'Title', data_type: 'string', required: true, allowed_values: [], max_length: 0 },
            { name: 'description', display_name: 'Description', data_type: 'string', required: true, allowed_values: [], max_length: 0 },
            { name: 'condition', display_name: 'Condition', data_type: 'enum', required: true, allowed_values: ['New','Used - Like New','Used - Very Good','Used - Good','Used - Acceptable'], max_length: 0 }
          ];
          const aiRes = await aiApi.generateWithSchema({
            product_id: productId,
            channel: 'onbuy',
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
      setSubmitResult({ ok: false, error: err.response?.data?.error || err.message || 'Submission failed' });
    } finally {
      setSubmitting(false);
    }
  }

  // ── Requirements checklist ─────────────────────────────────────────────────

  const requirements = [
    { label: 'OnBuy Product Code (OPC)', met: opc.trim().length > 0 },
    { label: 'Price > 0', met: parseFloat(price) > 0 },
    { label: 'Stock quantity', met: stock >= 0 },
    { label: 'Condition selected', met: conditionId.length > 0 },
  ];

  // ── Render ─────────────────────────────────────────────────────────────────

  if (loading) {
    return (
      <div className="page-container">
        <div className="flex items-center justify-center min-h-[300px]">
          <div className="flex items-center gap-3 text-[var(--text-muted)]">
            <i className="ri-loader-4-line text-2xl animate-spin"></i>
            <span>Loading product data…</span>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="page-container">
      {/* Header */}
      <div className="page-header">
        <button onClick={() => navigate(-1)} className="btn btn-ghost flex items-center gap-2 mb-4">
          <i className="ri-arrow-left-line"></i>
          Back
        </button>
        <div className="flex items-center gap-3">
          <div
            className="w-10 h-10 rounded-lg flex items-center justify-center text-white font-bold text-lg"
            style={{ backgroundColor: '#E76119' }}
          >
            O
          </div>
          <div>
            <h1 className="text-2xl font-semibold text-[var(--text-primary)]">Create OnBuy Listing</h1>

            <p className="text-sm text-[var(--text-muted)]">
              {productId ? `Product ID: ${productId}` : 'New listing'}
            </p>
          </div>
        </div>
      </div>

      {/* Success banner */}
      {submitResult?.ok && (
        <div className="bg-green-500/10 border border-green-500/30 rounded-lg p-4 mb-6 flex items-start gap-3">
          <i className="ri-checkbox-circle-line text-green-400 text-xl mt-0.5"></i>
          <div>
            <p className="text-green-400 font-medium">Listing created successfully on OnBuy!</p>
            {submitResult.listing_id && (
              <p className="text-[var(--text-muted)] text-sm mt-1">
                Listing ID: <span className="font-mono">{submitResult.listing_id}</span>
              </p>
            )}
          </div>
        </div>
      )}

      {/* Error banner */}
      {(error || submitResult?.error) && (
        <div className="bg-red-500/10 border border-red-500/30 rounded-lg p-4 mb-6 flex items-start gap-3">
          <i className="ri-error-warning-line text-red-400 text-xl mt-0.5"></i>
          <p className="text-red-400">{error || submitResult?.error}</p>
        </div>
      )}
      {aiGenerating && (
        <div style={{ padding: '12px 14px', background: 'linear-gradient(90deg, rgba(139,92,246,0.15), rgba(59,130,246,0.15))', borderRadius: 8, fontSize: 13, color: 'var(--accent-purple)', marginBottom: 16, display: 'flex', gap: 10, alignItems: 'center', border: '1px solid rgba(139,92,246,0.3)' }}>
          <div className="spinner" style={{ width: 16, height: 16, border: '2px solid var(--accent-purple)', borderTopColor: 'transparent', borderRadius: '50%', animation: 'spin 1s linear infinite' }} />
          <span style={{ fontWeight: 600 }}>🤖 AI is generating optimised content for OnBuy...</span>
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
        {/* Main form */}
        <div className="lg:col-span-2 space-y-6">

          {/* ── Configurator (CFG-07) ── */}
          <ConfiguratorSelector channel="onbuy" credentialId={credentialId} onSelect={handleConfiguratorSelect} />

          {/* OnBuy Product Code */}
          <div className="card p-6">
            <h2 className="text-lg font-semibold text-[var(--text-primary)] mb-4">
              OnBuy Product Code <span className="text-red-400">*</span>
            </h2>
            <div>
              <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
                OPC (OnBuy Product Code) <span className="text-red-400">*</span>
              </label>
              <input
                type="text"
                value={opc}
                onChange={(e) => setOpc(e.target.value)}
                placeholder="e.g. ABCD-1234"
                className="input w-full"
              />
              <p className="text-xs text-[var(--text-muted)] mt-1">
                The OPC links your listing to an existing product in the OnBuy catalogue. Find it via the OnBuy Seller Centre.
              </p>
            </div>
            <div className="mt-4">
              <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">SKU</label>
              <input
                type="text"
                value={sku}
                onChange={(e) => setSku(e.target.value)}
                placeholder="Your stock keeping unit"
                className="input w-full"
              />
            </div>
          </div>

          {/* Pricing & Stock */}
          <div className="card p-6">
            <h2 className="text-lg font-semibold text-[var(--text-primary)] mb-4">Pricing & Stock</h2>
            <div className="grid grid-cols-2 gap-4">
              <div>
                <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
                  Price <span className="text-red-400">*</span>
                </label>
                <div className="relative">
                  <span className="absolute left-3 top-1/2 -translate-y-1/2 text-[var(--text-muted)]">£</span>
                  <input
                    type="number"
                    step="0.01"
                    min="0"
                    value={price}
                    onChange={(e) => setPrice(e.target.value)}
                    className="input w-full pl-7"
                    placeholder="0.00"
                  />
                </div>
              </div>
              <div>
                <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">Stock</label>
                <input
                  type="number"
                  min="0"
                  value={stock}
                  onChange={(e) => setStock(parseInt(e.target.value) || 0)}
                  className="input w-full"
                />
              </div>
            </div>
          </div>

          {/* Condition & Delivery */}
          <div className="card p-6">
            <h2 className="text-lg font-semibold text-[var(--text-primary)] mb-4">Condition & Delivery</h2>
            <div className="grid grid-cols-2 gap-4">
              <div>
                <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
                  Condition <span className="text-red-400">*</span>
                </label>
                {conditions.length > 0 ? (
                  <select
                    className="input w-full"
                    value={conditionId}
                    onChange={(e) => setConditionId(e.target.value)}
                  >
                    {conditions.map((cond) => (
                      <option key={cond.condition_id} value={cond.condition_id}>
                        {cond.name}
                      </option>
                    ))}
                  </select>
                ) : (
                  <select
                    className="input w-full"
                    value={conditionId}
                    onChange={(e) => setConditionId(e.target.value)}
                  >
                    <option value="new">New</option>
                    <option value="used_like_new">Used – Like New</option>
                    <option value="used_very_good">Used – Very Good</option>
                    <option value="used_good">Used – Good</option>
                    <option value="used_acceptable">Used – Acceptable</option>
                    <option value="refurbished">Refurbished</option>
                  </select>
                )}
              </div>
              <div>
                <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">
                  Delivery Template ID
                </label>
                <input
                  type="number"
                  min="0"
                  value={deliveryTemplateId}
                  onChange={(e) => setDeliveryTemplateId(e.target.value)}
                  className="input w-full"
                  placeholder="0 (uses default)"
                />
                <p className="text-xs text-[var(--text-muted)] mt-1">
                  From OnBuy Seller Centre → Delivery Templates
                </p>
              </div>
            </div>
            <div className="mt-4">
              <label className="block text-sm font-medium text-[var(--text-primary)] mb-1">Site ID</label>
              <input
                type="number"
                value={siteId}
                onChange={(e) => setSiteId(parseInt(e.target.value) || 2000)}
                className="input w-full"
                placeholder="2000"
              />
              <p className="text-xs text-[var(--text-muted)] mt-1">2000 = OnBuy UK (default)</p>
            </div>
          </div>

          {/* Description */}
          <div className="card p-6">
            <h2 className="text-lg font-semibold text-[var(--text-primary)] mb-4">Description</h2>
            <textarea
              rows={6}
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              className="input w-full resize-y"
              placeholder="Product description…"
            />
          </div>

          {/* Category browser */}
          {allCategories.length > 0 && (
            <div className="card p-6">
              <h2 className="text-lg font-semibold text-[var(--text-primary)] mb-4">Category</h2>
              {selectedCategoryId && !catBrowsing && (
                <div className="flex items-center justify-between mb-3">
                  <span className="text-sm text-[var(--text-primary)]">{selectedCategoryName}</span>
                  <button
                    onClick={() => setCatBrowsing(true)}
                    className="btn btn-ghost text-xs"
                  >
                    Change
                  </button>
                </div>
              )}
              {(!selectedCategoryId || catBrowsing) && (
                <div>
                  <div className="flex gap-2 overflow-x-auto pb-2">
                    {browseLevels.map((level, levelIdx) => (
                      <div key={levelIdx} className="min-w-[180px] max-h-48 overflow-y-auto border border-[var(--border-color)] rounded-lg">
                        {level.map((cat) => (
                          <button
                            key={cat.category_id}
                            onClick={() => handleCatSelect(levelIdx, cat)}
                            className={`w-full text-left px-3 py-2 text-sm hover:bg-[var(--bg-secondary)] transition-colors flex items-center justify-between ${
                              browseSel[levelIdx]?.category_id === cat.category_id
                                ? 'bg-[var(--primary)]/10 text-[var(--primary)]'
                                : 'text-[var(--text-primary)]'
                            }`}
                          >
                            <span>{cat.name}</span>
                            {cat.has_children && <i className="ri-arrow-right-s-line text-xs opacity-50"></i>}
                          </button>
                        ))}
                      </div>
                    ))}
                  </div>
                  {selectedCategoryId && (
                    <button onClick={applyCategory} className="btn btn-primary mt-3 text-sm">
                      Use: {selectedCategoryName}
                    </button>
                  )}
                </div>
              )}
            </div>
          )}
        </div>

        {/* Sidebar */}
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
                  ></i>
                  <span className={r.met ? 'text-[var(--text-primary)]' : 'text-[var(--text-muted)]'}>
                    {r.label}
                  </span>
                </li>
              ))}
            </ul>
          </div>

          {/* OnBuy info */}
          <div className="card p-5">
            <h3 className="font-semibold text-[var(--text-primary)] mb-3">OnBuy Notes</h3>
            <ul className="space-y-2 text-sm text-[var(--text-muted)]">
              <li className="flex items-start gap-2">
                <i className="ri-information-line text-[var(--primary)] mt-0.5"></i>
                OnBuy listings are linked to an OPC (OnBuy Product Code) in their catalogue.
              </li>
              <li className="flex items-start gap-2">
                <i className="ri-information-line text-[var(--primary)] mt-0.5"></i>
                Orders arrive as <span className="font-mono text-xs">awaiting_dispatch</span>. Import will auto-acknowledge them.
              </li>
              <li className="flex items-start gap-2">
                <i className="ri-information-line text-[var(--primary)] mt-0.5"></i>
                Site ID 2000 = OnBuy UK.
              </li>
            </ul>
          </div>

          {/* VAR-01 — Variant Grid (mimicked variations) */}
          {isVariantProduct && (
            <div style={{ marginBottom: 16, padding: 16, background: 'var(--bg-secondary)', borderRadius: 10, border: '1px solid var(--border)' }}>
              <h3 style={{ margin: '0 0 4px', fontSize: 14, fontWeight: 700, color: '#d946ef' }}>Variants ({variants.length})</h3>
              <div className="p-3 rounded-lg text-sm mb-3" style={{ background: 'rgba(245,158,11,0.1)', border: '1px solid rgba(245,158,11,0.3)' }}>
                <p className="font-medium mb-1" style={{ color: '#f59e0b' }}>
                  <i className="ri-git-branch-line mr-1" />Mimicked variations
                </p>
                <p style={{ color: 'var(--text-muted)', fontSize: 12 }}>
                  OnBuy does not support native variation groups. Each active variant will be submitted as a separate listing using the same OPC but its own SKU and price.
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

              {/* Per-variant submission results */}
              {variantSubmitResults.length > 0 && (
                <div className="space-y-1 mt-3">
                  {variantSubmitResults.map((r, i) => (
                    <div key={i} className="flex items-center gap-2 text-xs p-2 rounded" style={{ background: r.listingId ? 'rgba(34,197,94,0.08)' : 'rgba(239,68,68,0.08)' }}>
                      <i className={r.listingId ? 'ri-checkbox-circle-line text-green-400' : 'ri-close-circle-line text-red-400'} />
                      <span style={{ color: 'var(--text-secondary)' }}>{r.sku}</span>
                      {r.listingId && <span className="ml-auto" style={{ color: 'var(--text-muted)' }}>ID: {r.listingId}</span>}
                      {r.error && <span className="ml-auto text-red-400">{r.error}</span>}
                    </div>
                  ))}
                </div>
              )}
            </div>
          )}

          {/* Submit */}
          <button
            onClick={handleSubmit}
            disabled={submitting || !!(isVariantProduct ? variantSubmitResults.length > 0 : submitResult?.ok)}
            className="btn btn-primary w-full flex items-center justify-center gap-2 py-3"
          >
            {submitting ? (
              <>
                <i className="ri-loader-4-line animate-spin"></i>
                Creating {isVariantProduct ? 'listings' : 'Listing'}…
              </>
            ) : (isVariantProduct ? variantSubmitResults.length > 0 : submitResult?.ok) ? (
              <>
                <i className="ri-checkbox-circle-fill"></i>
                {isVariantProduct ? `${variantSubmitResults.filter(r => r.listingId).length} Listing(s) Created` : 'Listing Created'}
              </>
            ) : (
              <>
                <i className="ri-add-line"></i>
                {isVariantProduct ? `Create ${variants.filter(v => v.active).length} OnBuy Listings` : 'Create OnBuy Listing'}
              </>
            )}
          </button>
        </div>
      </div>
    </div>
  );
}

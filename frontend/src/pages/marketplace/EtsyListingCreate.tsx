// ============================================================================
// ETSY LISTING CREATE PAGE
// ============================================================================
// Arrives with ?product_id=xxx from product edit page.
// Covers: taxonomy (category) selection, listing details, tags, materials,
// who_made / when_made / is_supply, shipping profile selector, images.

import { useState, useEffect } from 'react';
import { useSearchParams, useNavigate } from 'react-router-dom';
import {
  etsyApi,
  EtsyTaxonomyNode,
  EtsyShippingProfile,
  EtsyDraft,
  EtsySubmitPayload,
  ChannelVariantDraft,
} from '../../services/etsy-api';
import ConfiguratorSelector from './ConfiguratorSelector';
import { configuratorService, ConfiguratorDetail } from '../../services/configurator-api';
import { listingService } from '../../services/marketplace-api';

// ── Helpers ───────────────────────────────────────────────────────────────────

function buildTaxonomyTree(nodes: EtsyTaxonomyNode[]): Map<number, EtsyTaxonomyNode[]> {
  const tree = new Map<number, EtsyTaxonomyNode[]>();
  for (const node of nodes) {
    const children = tree.get(node.parent_id) || [];
    children.push(node);
    tree.set(node.parent_id, children);
  }
  return tree;
}

function isLeafNode(node: EtsyTaxonomyNode, tree: Map<number, EtsyTaxonomyNode[]>): boolean {
  const children = tree.get(node.id);
  return !children || children.length === 0;
}

const WHO_MADE_OPTIONS = [
  { value: 'i_did', label: 'I did' },
  { value: 'collective', label: 'A member of my shop (collective)' },
  { value: 'someone_else', label: 'Someone else (manufactured)' },
];

const WHEN_MADE_OPTIONS = [
  { value: 'made_to_order', label: 'Made to order' },
  { value: '2020_2025', label: '2020 – 2025' },
  { value: '2010_2019', label: '2010 – 2019' },
  { value: '2000_2009', label: '2000 – 2009' },
  { value: 'before_2000', label: 'Before 2000' },
  { value: '1990s', label: '1990s' },
  { value: '1980s', label: '1980s' },
  { value: '1970s', label: '1970s' },
  { value: '1960s', label: '1960s' },
  { value: '1950s', label: '1950s' },
  { value: '1940s', label: '1940s' },
  { value: '1930s', label: '1930s' },
  { value: '1920s', label: '1920s' },
  { value: 'before_1920', label: 'Before 1920' },
];

// ── Main Component ────────────────────────────────────────────────────────────

export default function EtsyListingCreate() {
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
  const [draft, setDraft] = useState<EtsyDraft | null>(null);

  // VAR-01 — Variation listings
  const [variants, setVariants] = useState<ChannelVariantDraft[]>([]);
  const [isVariantProduct, setIsVariantProduct] = useState(false);
  const updateVariant = (id: string, field: keyof ChannelVariantDraft, value: any) => {
    setVariants(vs => vs.map(v => v.id === id ? { ...v, [field]: value } : v));
  };

  // Form fields
  const [title, setTitle] = useState('');
  const [description, setDescription] = useState('');
  const [price, setPrice] = useState('');
  const [quantity, setQuantity] = useState(1);
  const [whoMade, setWhoMade] = useState('i_did');
  const [whenMade, setWhenMade] = useState('made_to_order');
  const [isSupply, setIsSupply] = useState(false);
  const [shippingProfileId, setShippingProfileId] = useState<number | ''>('');
  const [tagInput, setTagInput] = useState('');
  const [tags, setTags] = useState<string[]>([]);
  const [materialInput, setMaterialInput] = useState('');
  const [materials, setMaterials] = useState<string[]>([]);

  // Taxonomy
  const [allNodes, setAllNodes] = useState<EtsyTaxonomyNode[]>([]);
  const [taxTree, setTaxTree] = useState<Map<number, EtsyTaxonomyNode[]>>(new Map());
  const [selectedTaxonomyId, setSelectedTaxonomyId] = useState<number>(0);
  const [selectedTaxonomyName, setSelectedTaxonomyName] = useState('');
  const [catBrowsing, setCatBrowsing] = useState(false);
  const [browseLevel, setBrowseLevel] = useState<EtsyTaxonomyNode[][]>([]);
  const [browseSel, setBrowseSel] = useState<(EtsyTaxonomyNode | null)[]>([]);

  // Shipping profiles
  const [shippingProfiles, setShippingProfiles] = useState<EtsyShippingProfile[]>([]);

  // Images
  const [images, setImages] = useState<string[]>([]);

  // Submission
  const [submitting, setSubmitting] = useState(false);
  const [submitResult, setSubmitResult] = useState<{
    ok: boolean;
    listing_id?: number;
    error?: string;
  } | null>(null);

  // ── Configurator (CFG-07) ──
  const [selectedConfigurator, setSelectedConfigurator] = useState<ConfiguratorDetail | null>(null);

  const handleConfiguratorSelect = (cfg: ConfiguratorDetail | null) => {
    setSelectedConfigurator(cfg);
    if (!cfg) return;
    // Pre-populate taxonomy / category
    if (cfg.category_id) {
      const catIdNum = parseInt(cfg.category_id, 10);
      if (!isNaN(catIdNum)) {
        setSelectedTaxonomyId(catIdNum);
        setSelectedTaxonomyName(cfg.category_path || cfg.category_id);
      }
    }
    // Pre-populate shipping profile
    if (cfg.shipping_defaults?.shipping_profile_id) {
      const spId = parseInt(cfg.shipping_defaults.shipping_profile_id, 10);
      if (!isNaN(spId)) setShippingProfileId(spId);
    }
    // Pre-populate tags from attribute defaults with source = default_value
    if (cfg.attribute_defaults && cfg.attribute_defaults.length > 0) {
      const newTags: string[] = [];
      for (const attr of cfg.attribute_defaults) {
        if (attr.attribute_name === 'tags' && attr.default_value) {
          newTags.push(...attr.default_value.split(',').map((t: string) => t.trim()).filter(Boolean));
        }
      }
      if (newTags.length > 0) setTags(prev => [...new Set([...prev, ...newTags])]);
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
      const [prepRes, taxRes, shipRes] = await Promise.allSettled([
        etsyApi.prepare({ product_id: productId!, credential_id: credentialId || undefined }),
        etsyApi.getTaxonomy(credentialId || undefined),
        etsyApi.getShippingProfiles(credentialId || undefined),
      ]);

      // Product draft
      if (prepRes.status === 'fulfilled' && prepRes.value.data?.ok) {
        const d = prepRes.value.data.draft;
        setDraft(d);
        setTitle(d.title || '');
        setDescription(d.description || '');
        setPrice(String(d.price || ''));
        setQuantity(d.quantity || 1);
        setImages(d.images || []);
        if (d.tags?.length) setTags(d.tags);
        if (d.materials?.length) setMaterials(d.materials);
        if (d.who_made) setWhoMade(d.who_made);
        if (d.when_made) setWhenMade(d.when_made);
        if (d.is_supply !== undefined) setIsSupply(d.is_supply);
        if (d.taxonomy_id) setSelectedTaxonomyId(d.taxonomy_id);
        // VAR-01: load variants
        if (d.variants && d.variants.length > 0) {
          setVariants(d.variants);
          setIsVariantProduct(true);
        }
      } else {
        setError('Could not load product data. Make sure an Etsy credential is connected.');
      }

      // Taxonomy
      if (taxRes.status === 'fulfilled' && taxRes.value.data?.ok) {
        const nodes = taxRes.value.data.nodes || [];
        setAllNodes(nodes);
        setTaxTree(buildTaxonomyTree(nodes));
      }

      // Shipping profiles
      if (shipRes.status === 'fulfilled' && shipRes.value.data?.ok) {
        const profiles = shipRes.value.data.profiles || [];
        setShippingProfiles(profiles);
        if (profiles.length > 0) setShippingProfileId(profiles[0].shipping_profile_id);
      }


      const aiFlag = searchParams.get('ai');
      if (aiFlag === 'pending' && productId) {
        setAiGenerating(true);
        try {
          const { aiService: aiApi } = await import('../../services/ai-api');
          const schemaFields = [
            { name: 'title', display_name: 'Title', data_type: 'string', required: true, allowed_values: [], max_length: 140 },
            { name: 'description', display_name: 'Description', data_type: 'string', required: true, allowed_values: [], max_length: 0 },
            { name: 'tags', display_name: 'Tags', data_type: 'string', required: false, allowed_values: [], max_length: 0 },
            { name: 'materials', display_name: 'Materials', data_type: 'string', required: false, allowed_values: [], max_length: 0 }
          ];
          const aiRes = await aiApi.generateWithSchema({
            product_id: productId,
            channel: 'etsy',
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
      setError(err.message || 'Failed to load Etsy listing data');
    } finally {
      setLoading(false);
    }
  }

  // ── Category browser ──────────────────────────────────────────────────────

  function openCategoryBrowser() {
    const roots = taxTree.get(0) || [];
    setBrowseLevel([roots]);
    setBrowseSel([null]);
    setCatBrowsing(true);
  }

  function selectBrowseNode(level: number, node: EtsyTaxonomyNode) {
    const newSel = browseSel.slice(0, level + 1);
    newSel[level] = node;
    const newLevels = browseLevel.slice(0, level + 1);

    const leaf = isLeafNode(node, taxTree);
    if (!leaf) {
      const children = taxTree.get(node.id) || [];
      if (children.length > 0) {
        newLevels.push(children);
        newSel.push(null);
      }
    }

    setBrowseLevel(newLevels);
    setBrowseSel(newSel);

    if (leaf) {
      setSelectedTaxonomyId(node.id);
      // Build breadcrumb name from selected path
      const pathNames = newSel.filter(Boolean).map((n) => n!.name).join(' › ');
      setSelectedTaxonomyName(pathNames);
      setCatBrowsing(false);
    }
  }

  // ── Tags ──────────────────────────────────────────────────────────────────

  function addTag() {
    const t = tagInput.trim().toLowerCase().replace(/[,]/g, '');
    if (!t || tags.includes(t) || tags.length >= 13) return;
    setTags([...tags, t]);
    setTagInput('');
  }

  function removeTag(tag: string) {
    setTags(tags.filter((t) => t !== tag));
  }

  function handleTagKeyDown(e: React.KeyboardEvent) {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault();
      addTag();
    }
  }

  // ── Materials ─────────────────────────────────────────────────────────────

  function addMaterial() {
    const m = materialInput.trim();
    if (!m || materials.includes(m) || materials.length >= 13) return;
    setMaterials([...materials, m]);
    setMaterialInput('');
  }

  function removeMaterial(material: string) {
    setMaterials(materials.filter((m) => m !== material));
  }

  function handleMaterialKeyDown(e: React.KeyboardEvent) {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault();
      addMaterial();
    }
  }

  // ── Submit ────────────────────────────────────────────────────────────────

  async function handleSubmit() {
    if (!title.trim()) { alert('Title is required'); return; }
    if (!selectedTaxonomyId) { alert('Please select a category'); return; }
    if (!price || parseFloat(price) <= 0) { alert('Price must be greater than 0'); return; }
    if (quantity < 1) { alert('Quantity must be at least 1'); return; }

    setSubmitting(true);
    setSubmitResult(null);

    try {
      const payload: EtsySubmitPayload = {
        title: title.trim().slice(0, 140),
        description: description.trim(),
        price: parseFloat(price),
        quantity,
        taxonomy_id: selectedTaxonomyId,
        who_made: whoMade,
        when_made: whenMade,
        is_supply: isSupply,
        shipping_profile_id: shippingProfileId !== '' ? Number(shippingProfileId) : undefined,
        tags: tags.slice(0, 13),
        materials: materials.slice(0, 13),
        images: images.slice(0, 10),
        variants: variants.filter(v => v.active), // VAR-01
      };

      const res = await etsyApi.submit(payload);
      setSubmitResult(res.data);
      // ── Configurator join (CFG-07) ──
      if (res.data?.ok && selectedConfigurator) {
        try {
          const listRes = await listingService.list({ product_id: productId!, channel: 'etsy', limit: 10 });
          const listings: any[] = listRes.data?.listings || listRes.data?.data || [];
          if (listings.length > 0) {
            const newest = listings[listings.length - 1];
            await configuratorService.assignListings(selectedConfigurator.configurator_id, [newest.listing_id]);
          }
        } catch { /* non-fatal */ }
      }
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
          <p>Loading Etsy listing data…</p>
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
          <div style={{ fontSize: 56, marginBottom: 16 }}>🛍️</div>
          <h2 className="text-xl font-semibold mb-2">Etsy Listing Created!</h2>
          <p className="text-[var(--text-muted)] mb-1">
            Listing ID: <code>{submitResult.listing_id}</code>
          </p>
          <p className="text-[var(--text-muted)] mb-6">
            Your listing has been created on Etsy and is ready for review.
          </p>
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
            <i className="ri-store-2-fill" style={{ color: '#F1641E' }} />
            Create Etsy Listing
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
              <><i className="ri-store-2-fill mr-2" />Publish to Etsy</>
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
          <span style={{ fontWeight: 600 }}>🤖 AI is generating optimised content for Etsy...</span>
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
          <ConfiguratorSelector channel="etsy" credentialId={credentialId} onSelect={handleConfiguratorSelect} />

          {/* Core fields */}
          <div className="card">
            <div className="card-header">
              <h3 className="card-title">Listing Details</h3>
            </div>
            <div className="card-body space-y-4">
              <div>
                <label className="label">
                  Title <span className="text-red-500">*</span>
                </label>
                <input
                  className="input w-full"
                  value={title}
                  onChange={(e) => setTitle(e.target.value)}
                  maxLength={140}
                  placeholder="Describe the item in a few words (max 140 chars)"
                />
                <p className="text-xs text-[var(--text-muted)] mt-1">
                  {title.length}/140 characters
                </p>
              </div>

              <div>
                <label className="label">Description</label>
                <textarea
                  className="input w-full resize-none"
                  rows={8}
                  value={description}
                  onChange={(e) => setDescription(e.target.value)}
                  placeholder="Describe the item — include dimensions, materials, care instructions, etc."
                />
              </div>

              {/* Tags */}
              <div>
                <label className="label">
                  Tags <span className="text-xs text-[var(--text-muted)]">({tags.length}/13)</span>
                </label>
                <div className="flex gap-2 mb-2 flex-wrap">
                  {tags.map((tag) => (
                    <span
                      key={tag}
                      className="inline-flex items-center gap-1 px-2 py-1 rounded-full text-xs"
                      style={{ background: 'var(--bg-secondary)', color: 'var(--text-primary)' }}
                    >
                      {tag}
                      <button onClick={() => removeTag(tag)} className="opacity-60 hover:opacity-100">
                        <i className="ri-close-line text-xs" />
                      </button>
                    </span>
                  ))}
                </div>
                {tags.length < 13 && (
                  <div className="flex gap-2">
                    <input
                      className="input flex-1"
                      value={tagInput}
                      onChange={(e) => setTagInput(e.target.value)}
                      onKeyDown={handleTagKeyDown}
                      placeholder="Add a tag and press Enter or comma"
                    />
                    <button className="btn btn-secondary" onClick={addTag}>Add</button>
                  </div>
                )}
                <p className="text-xs text-[var(--text-muted)] mt-1">
                  Tags help buyers find your listing. Max 13 tags, 1–20 chars each.
                </p>
              </div>

              {/* Materials */}
              <div>
                <label className="label">
                  Materials <span className="text-xs text-[var(--text-muted)]">({materials.length}/13)</span>
                </label>
                <div className="flex gap-2 mb-2 flex-wrap">
                  {materials.map((mat) => (
                    <span
                      key={mat}
                      className="inline-flex items-center gap-1 px-2 py-1 rounded-full text-xs"
                      style={{ background: 'var(--bg-secondary)', color: 'var(--text-primary)' }}
                    >
                      {mat}
                      <button onClick={() => removeMaterial(mat)} className="opacity-60 hover:opacity-100">
                        <i className="ri-close-line text-xs" />
                      </button>
                    </span>
                  ))}
                </div>
                {materials.length < 13 && (
                  <div className="flex gap-2">
                    <input
                      className="input flex-1"
                      value={materialInput}
                      onChange={(e) => setMaterialInput(e.target.value)}
                      onKeyDown={handleMaterialKeyDown}
                      placeholder="Add a material and press Enter or comma"
                    />
                    <button className="btn btn-secondary" onClick={addMaterial}>Add</button>
                  </div>
                )}
              </div>
            </div>
          </div>

          {/* Category / Taxonomy */}
          <div className="card">
            <div className="card-header">
              <h3 className="card-title">Category <span className="text-red-500">*</span></h3>
            </div>
            <div className="card-body">
              {selectedTaxonomyId ? (
                <div className="flex items-center justify-between">
                  <div>
                    <p className="text-sm font-medium text-[var(--text-primary)]">
                      {selectedTaxonomyName || `Category ID: ${selectedTaxonomyId}`}
                    </p>
                    <p className="text-xs text-[var(--text-muted)]">ID: {selectedTaxonomyId}</p>
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
                    {browseLevel.map((levelNodes, li) => (
                      <div key={li} className="min-w-[200px] overflow-y-auto max-h-64">
                        {levelNodes.map((node) => {
                          const leaf = isLeafNode(node, taxTree);
                          return (
                            <button
                              key={node.id}
                              onClick={() => selectBrowseNode(li, node)}
                              className={`w-full text-left px-3 py-2 text-sm hover:bg-[var(--bg-secondary)] flex items-center justify-between ${
                                browseSel[li]?.id === node.id
                                  ? 'bg-[var(--bg-secondary)] text-[var(--accent)]'
                                  : ''
                              }`}
                            >
                              <span>{node.name}</span>
                              {!leaf && <i className="ri-arrow-right-s-line text-xs opacity-50" />}
                              {leaf && <span className="text-xs text-green-500">✓</span>}
                            </button>
                          );
                        })}
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

          {/* Images */}
          <div className="card">
            <div className="card-header">
              <h3 className="card-title">
                Product Images
                <span className="text-xs text-[var(--text-muted)] ml-2 font-normal">
                  ({images.length}/10)
                </span>
              </h3>
              <p className="card-subtitle text-xs text-[var(--text-muted)]">
                Minimum 1 image. Images are uploaded to Etsy when you publish.
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
                <div className="grid grid-cols-4 gap-3">
                  {images.map((img, i) => (
                    <div
                      key={i}
                      className="relative aspect-square rounded-lg overflow-hidden border border-[var(--border)]"
                    >
                      <img
                        src={img}
                        alt={`Product ${i + 1}`}
                        className="w-full h-full object-cover"
                      />
                      {i === 0 && (
                        <div
                          className="absolute bottom-0 left-0 right-0 text-center text-xs py-0.5"
                          style={{ background: 'rgba(0,0,0,0.5)', color: '#fff' }}
                        >
                          Main
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              )}
              <p className="text-xs text-[var(--text-muted)] mt-2">
                Images are sent to Etsy during submission. First image is used as the main image.
              </p>
            </div>
          </div>
        </div>

        {/* Right column — pricing, provenance, shipping */}
        <div className="space-y-6">

          {/* Pricing & Inventory */}
          <div className="card">
            <div className="card-header">
              <h3 className="card-title">Pricing & Stock</h3>
            </div>
            <div className="card-body space-y-4">
              <div>
                <label className="label">
                  Price (USD) <span className="text-red-500">*</span>
                </label>
                <div className="relative">
                  <span className="absolute left-3 top-1/2 -translate-y-1/2 text-[var(--text-muted)]">$</span>
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
                <p className="text-xs text-[var(--text-muted)] mt-1">Etsy charges in USD by default.</p>
              </div>

              <div>
                <label className="label">
                  Quantity <span className="text-red-500">*</span>
                </label>
                <input
                  type="number"
                  min="1"
                  className="input w-full"
                  value={quantity}
                  onChange={(e) => setQuantity(parseInt(e.target.value) || 1)}
                />
              </div>
            </div>
          </div>

          {/* Provenance */}
          <div className="card">
            <div className="card-header">
              <h3 className="card-title">Provenance</h3>
              <p className="card-subtitle text-xs text-[var(--text-muted)]">
                Required by Etsy for all listings
              </p>
            </div>
            <div className="card-body space-y-4">
              <div>
                <label className="label">
                  Who made this? <span className="text-red-500">*</span>
                </label>
                <select
                  className="input w-full"
                  value={whoMade}
                  onChange={(e) => setWhoMade(e.target.value)}
                >
                  {WHO_MADE_OPTIONS.map((o) => (
                    <option key={o.value} value={o.value}>{o.label}</option>
                  ))}
                </select>
              </div>

              <div>
                <label className="label">
                  When was it made? <span className="text-red-500">*</span>
                </label>
                <select
                  className="input w-full"
                  value={whenMade}
                  onChange={(e) => setWhenMade(e.target.value)}
                >
                  {WHEN_MADE_OPTIONS.map((o) => (
                    <option key={o.value} value={o.value}>{o.label}</option>
                  ))}
                </select>
              </div>

              <div className="flex items-center gap-3">
                <input
                  type="checkbox"
                  id="is_supply"
                  checked={isSupply}
                  onChange={(e) => setIsSupply(e.target.checked)}
                  className="w-4 h-4"
                />
                <label htmlFor="is_supply" className="text-sm text-[var(--text-primary)] cursor-pointer">
                  This is a craft supply or tool
                </label>
              </div>
            </div>
          </div>

          {/* Shipping profile */}
          <div className="card">
            <div className="card-header">
              <h3 className="card-title">Shipping Profile</h3>
            </div>
            <div className="card-body">
              {shippingProfiles.length === 0 ? (
                <p className="text-sm text-[var(--text-muted)]">
                  No shipping profiles found. Please create a shipping profile in your Etsy seller account.
                </p>
              ) : (
                <select
                  className="input w-full"
                  value={shippingProfileId}
                  onChange={(e) => setShippingProfileId(Number(e.target.value))}
                >
                  <option value="">— No shipping profile —</option>
                  {shippingProfiles.map((p) => (
                    <option key={p.shipping_profile_id} value={p.shipping_profile_id}>
                      {p.title}
                    </option>
                  ))}
                </select>
              )}
            </div>
          </div>

          {/* VAR-01 — Variant Grid */}
          {isVariantProduct && (
            <div style={{ marginTop: 24, padding: 20, background: 'var(--bg-secondary)', borderRadius: 12, border: '1px solid var(--border)' }}>
              <h3 style={{ margin: '0 0 4px', fontSize: 15, fontWeight: 700, color: '#d946ef' }}>Variants ({variants.length})</h3>
              <p style={{ fontSize: 12, color: 'var(--text-muted)', margin: '0 0 12px' }}>
                Etsy supports variation listings via the inventory API. The listing will be created first, then variant offerings will be applied automatically.
              </p>
              <div style={{ overflowX: 'auto' }}>
                <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
                  <thead>
                    <tr style={{ borderBottom: '2px solid var(--border)' }}>
                      <th style={{ padding: '8px 10px', textAlign: 'left', fontWeight: 600, fontSize: 12, color: 'var(--text-secondary)' }}>Active</th>
                      {variants.length > 0 && Object.keys(variants[0].combination).map(k => (
                        <th key={k} style={{ padding: '8px 10px', textAlign: 'left', fontWeight: 600, fontSize: 12, color: 'var(--text-secondary)' }}>{k}</th>
                      ))}
                      <th style={{ padding: '8px 10px', textAlign: 'left', fontWeight: 600, fontSize: 12, color: 'var(--text-secondary)' }}>SKU</th>
                      <th style={{ padding: '8px 10px', textAlign: 'left', fontWeight: 600, fontSize: 12, color: 'var(--text-secondary)' }}>Price (£)</th>
                      <th style={{ padding: '8px 10px', textAlign: 'left', fontWeight: 600, fontSize: 12, color: 'var(--text-secondary)' }}>Stock</th>
                    </tr>
                  </thead>
                  <tbody>
                    {variants.map(v => (
                      <tr key={v.id} style={{ borderBottom: '1px solid var(--border)', opacity: v.active ? 1 : 0.45 }}>
                        <td style={{ padding: '6px 10px', verticalAlign: 'middle' }}>
                          <input type="checkbox" checked={v.active} onChange={e => updateVariant(v.id, 'active', e.target.checked)} />
                        </td>
                        {Object.values(v.combination).map((val, i) => (
                          <td key={i} style={{ padding: '6px 10px', verticalAlign: 'middle', fontSize: 12 }}>{val}</td>
                        ))}
                        <td style={{ padding: '6px 10px', verticalAlign: 'middle' }}>
                          <input value={v.sku} onChange={e => updateVariant(v.id, 'sku', e.target.value)}
                            style={{ padding: '4px 8px', fontSize: 12, width: 120, borderRadius: 6, border: '1px solid var(--border)', background: 'var(--bg-primary)', color: 'var(--text-primary)' }} />
                        </td>
                        <td style={{ padding: '6px 10px', verticalAlign: 'middle' }}>
                          <input value={v.price} onChange={e => updateVariant(v.id, 'price', e.target.value)}
                            style={{ padding: '4px 8px', fontSize: 12, width: 80, borderRadius: 6, border: '1px solid var(--border)', background: 'var(--bg-primary)', color: 'var(--text-primary)' }}
                            type="number" step="0.01" />
                        </td>
                        <td style={{ padding: '6px 10px', verticalAlign: 'middle' }}>
                          <input value={v.stock} onChange={e => updateVariant(v.id, 'stock', e.target.value)}
                            style={{ padding: '4px 8px', fontSize: 12, width: 60, borderRadius: 6, border: '1px solid var(--border)', background: 'var(--bg-primary)', color: 'var(--text-primary)' }}
                            type="number" />
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {/* Publish button (repeat) */}
          <button
            className="btn btn-primary w-full"
            onClick={handleSubmit}
            disabled={submitting}
          >
            {submitting ? (
              <><span className="spinner-sm mr-2" />Publishing…</>
            ) : (
              <><i className="ri-store-2-fill mr-2" />Publish to Etsy</>
            )}
          </button>
        </div>
      </div>
    </div>
  );
}

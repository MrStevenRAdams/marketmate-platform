// ============================================================================
// SHOPLINE LISTING CREATE PAGE
// ============================================================================
import { useState, useEffect, useRef } from 'react';
import { useSearchParams, useNavigate } from 'react-router-dom';
import { shoplineApi, ShoplineDraft, ShoplinePricingTier } from '../../services/shopline-api';
import type { ChannelVariantDraft } from '../../services/channel-types';

// Shopline brand colour — teal/turquoise
const SHOPLINE_TEAL = '#00b8d4';
const SHOPLINE_DARK = '#006d7f';

const labelStyle: React.CSSProperties = {
  display: 'block', fontSize: 11, fontWeight: 600,
  color: 'var(--text-muted)', textTransform: 'uppercase',
  letterSpacing: '0.06em', marginBottom: 4,
};
const inputStyle: React.CSSProperties = {
  width: '100%', padding: '8px 12px', background: 'var(--bg-elevated)',
  border: '1px solid var(--border)', borderRadius: 6,
  color: 'var(--text-primary)', fontSize: 14, boxSizing: 'border-box',
};
const textareaStyle: React.CSSProperties = {
  ...inputStyle, minHeight: 120, resize: 'vertical', fontFamily: 'inherit',
};

function Section({ title, subtitle, accent = SHOPLINE_TEAL, children }: {
  title: string; subtitle?: string; accent?: string; children: React.ReactNode;
}) {
  return (
    <div style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 10, marginBottom: 16, overflow: 'hidden' }}>
      <div style={{ padding: '12px 20px', borderBottom: '1px solid var(--border)', borderLeft: `4px solid ${accent}`, display: 'flex', alignItems: 'baseline', gap: 10 }}>
        <span style={{ fontWeight: 700, fontSize: 14, color: 'var(--text-primary)' }}>{title}</span>
        {subtitle && <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>{subtitle}</span>}
      </div>
      <div style={{ padding: '16px 20px', display: 'flex', flexDirection: 'column', gap: 12 }}>{children}</div>
    </div>
  );
}

function Row({ children }: { children: React.ReactNode }) {
  return <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap' }}>{children}</div>;
}

// ── Tag picker ────────────────────────────────────────────────────────────────
function TagPicker({ existingTags, selectedTags, onChange }: {
  existingTags: string[]; selectedTags: string[]; onChange: (tags: string[]) => void;
}) {
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState('');
  const [newTag, setNewTag] = useState('');
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const handler = (e: MouseEvent) => { if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false); };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, []);

  const filtered = existingTags.filter(t => t.toLowerCase().includes(search.toLowerCase()) && !selectedTags.includes(t));
  const toggle = (tag: string) => onChange(selectedTags.includes(tag) ? selectedTags.filter(t => t !== tag) : [...selectedTags, tag]);
  const addNew = () => { if (newTag.trim() && !selectedTags.includes(newTag.trim())) { onChange([...selectedTags, newTag.trim()]); setNewTag(''); } };

  return (
    <div ref={ref} style={{ position: 'relative' }}>
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, padding: '8px 12px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6, cursor: 'text', minHeight: 40 }}
        onClick={() => setOpen(true)}>
        {selectedTags.map(tag => (
          <span key={tag} style={{ display: 'inline-flex', alignItems: 'center', gap: 4, padding: '2px 8px', background: `${SHOPLINE_TEAL}22`, border: `1px solid ${SHOPLINE_TEAL}55`, borderRadius: 99, fontSize: 12, color: SHOPLINE_TEAL }}>
            {tag}
            <button onClick={e => { e.stopPropagation(); toggle(tag); }} style={{ background: 'none', border: 'none', cursor: 'pointer', color: SHOPLINE_TEAL, padding: 0, fontSize: 12 }}>✕</button>
          </span>
        ))}
        {selectedTags.length === 0 && <span style={{ color: 'var(--text-muted)', fontSize: 13 }}>Click to add tags…</span>}
      </div>
      {open && (
        <div style={{ position: 'absolute', top: '100%', left: 0, right: 0, zIndex: 100, background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 8, boxShadow: '0 8px 24px rgba(0,0,0,0.3)', marginTop: 4, maxHeight: 300, overflow: 'hidden', display: 'flex', flexDirection: 'column' }}>
          <div style={{ padding: 10, borderBottom: '1px solid var(--border)' }}>
            <input autoFocus value={search} onChange={e => setSearch(e.target.value)} placeholder="Search existing tags…" style={{ ...inputStyle, fontSize: 13 }} />
          </div>
          <div style={{ overflowY: 'auto', flex: 1 }}>
            {filtered.length === 0 && <div style={{ padding: '10px 14px', fontSize: 13, color: 'var(--text-muted)' }}>No more tags available</div>}
            {filtered.map(tag => (
              <div key={tag} onClick={() => toggle(tag)} style={{ padding: '8px 14px', cursor: 'pointer', fontSize: 13, color: 'var(--text-primary)' }}
                onMouseEnter={e => (e.currentTarget.style.background = 'var(--bg-elevated)')}
                onMouseLeave={e => (e.currentTarget.style.background = 'transparent')}>
                {tag}
              </div>
            ))}
          </div>
          <div style={{ padding: 10, borderTop: '1px solid var(--border)', display: 'flex', gap: 8 }}>
            <input value={newTag} onChange={e => setNewTag(e.target.value)} onKeyDown={e => e.key === 'Enter' && addNew()} placeholder="New tag…" style={{ ...inputStyle, fontSize: 13, flex: 1 }} />
            <button onClick={addNew} style={{ padding: '6px 12px', background: SHOPLINE_TEAL, border: 'none', borderRadius: 6, color: '#fff', cursor: 'pointer', fontSize: 13, fontWeight: 600 }}>Add</button>
          </div>
        </div>
      )}
    </div>
  );
}

// ── Category picker ───────────────────────────────────────────────────────────
function CategoryPicker({ categories, value, onChange }: {
  categories: Array<{ id: string; full_name: string }>;
  value: string;
  onChange: (id: string, name: string) => void;
}) {
  const [search, setSearch] = useState('');
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const handler = (e: MouseEvent) => { if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false); };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, []);

  const selectedCat = categories.find(c => c.id === value);
  const filtered = categories.filter(c => c.full_name.toLowerCase().includes(search.toLowerCase())).slice(0, 30);

  return (
    <div ref={ref} style={{ position: 'relative' }}>
      <div onClick={() => setOpen(o => !o)} style={{ ...inputStyle, cursor: 'pointer', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <span style={{ color: selectedCat ? 'var(--text-primary)' : 'var(--text-muted)', fontSize: 14 }}>
          {selectedCat ? selectedCat.full_name : 'Select category…'}
        </span>
        <span style={{ color: 'var(--text-muted)', fontSize: 11 }}>▾</span>
      </div>
      {open && (
        <div style={{ position: 'absolute', top: '100%', left: 0, right: 0, zIndex: 100, background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 8, boxShadow: '0 8px 24px rgba(0,0,0,0.3)', marginTop: 4, maxHeight: 320, overflow: 'hidden', display: 'flex', flexDirection: 'column' }}>
          <div style={{ padding: 10, borderBottom: '1px solid var(--border)' }}>
            <input autoFocus value={search} onChange={e => setSearch(e.target.value)} placeholder="Search categories…" style={{ ...inputStyle, fontSize: 13 }} />
          </div>
          <div style={{ overflowY: 'auto', flex: 1 }}>
            <div onClick={() => { onChange('', ''); setOpen(false); }} style={{ padding: '8px 14px', cursor: 'pointer', fontSize: 13, color: 'var(--text-muted)', borderBottom: '1px solid var(--border)' }}>— No category —</div>
            {filtered.map(cat => (
              <div key={cat.id} onClick={() => { onChange(cat.id, cat.full_name); setOpen(false); }}
                style={{ padding: '8px 14px', cursor: 'pointer', fontSize: 13, color: 'var(--text-primary)', background: cat.id === value ? `${SHOPLINE_TEAL}18` : 'transparent' }}
                onMouseEnter={e => (e.currentTarget.style.background = 'var(--bg-elevated)')}
                onMouseLeave={e => (e.currentTarget.style.background = cat.id === value ? `${SHOPLINE_TEAL}18` : 'transparent')}>
                {cat.full_name}
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

// ── Variant Card ──────────────────────────────────────────────────────────────
function VariantCard({ variant, optionKeys, onChange }: {
  variant: ChannelVariantDraft;
  optionKeys: string[];
  onChange: (field: keyof ChannelVariantDraft, value: any) => void;
}) {
  const [expanded, setExpanded] = useState(false);
  const label = Object.values(variant.combination).join(' / ') || variant.sku || 'Variant';

  return (
    <div style={{ border: '1px solid var(--border)', borderRadius: 8, overflow: 'hidden', opacity: variant.active ? 1 : 0.5 }}>
      <div style={{ padding: '8px 14px', display: 'flex', alignItems: 'center', gap: 10, background: 'var(--bg-elevated)', cursor: 'pointer' }} onClick={() => setExpanded(e => !e)}>
        <input type="checkbox" checked={variant.active} onChange={e => { e.stopPropagation(); onChange('active', e.target.checked); }}
          style={{ width: 16, height: 16 }} onClick={e => e.stopPropagation()} />
        <span style={{ fontWeight: 600, fontSize: 13, flex: 1 }}>{label}</span>
        {variant.sku && <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>{variant.sku}</span>}
        {variant.price && <span style={{ fontSize: 13, color: SHOPLINE_TEAL, fontWeight: 600 }}>£{variant.price}</span>}
        {variant.stock && <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>{variant.stock} units</span>}
        <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>{expanded ? '▲' : '▼'}</span>
      </div>
      {expanded && variant.active && (
        <div style={{ padding: '12px 14px', display: 'flex', flexDirection: 'column', gap: 10 }}>
          <Row>
            <div style={{ flex: 1, minWidth: 120 }}>
              <label style={labelStyle}>SKU</label>
              <input value={variant.sku} onChange={e => onChange('sku', e.target.value)} style={inputStyle} placeholder="SKU-001" />
            </div>
            <div style={{ flex: 1, minWidth: 100 }}>
              <label style={labelStyle}>Price</label>
              <input value={variant.price} onChange={e => onChange('price', e.target.value)} style={inputStyle} type="number" step="0.01" min="0" placeholder="0.00" />
            </div>
            <div style={{ flex: 1, minWidth: 100 }}>
              <label style={labelStyle}>Stock</label>
              <input value={variant.stock} onChange={e => onChange('stock', e.target.value)} style={inputStyle} type="number" min="0" placeholder="0" />
            </div>
            <div style={{ flex: 1, minWidth: 120 }}>
              <label style={labelStyle}>Barcode / EAN</label>
              <input value={variant.ean || ''} onChange={e => onChange('ean', e.target.value)} style={inputStyle} placeholder="5012345678900" />
            </div>
          </Row>
          <div style={{ paddingTop: 4, borderTop: '1px solid var(--border)' }}>
            <label style={{ ...labelStyle, fontSize: 10 }}>Variation Options <span style={{ fontWeight: 400, textTransform: 'none', color: 'var(--text-muted)' }}>(from PIM — edit in product record)</span></label>
            <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', marginTop: 4 }}>
              {Object.entries(variant.combination).map(([k, v]) => (
                <span key={k} style={{ padding: '3px 10px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 99, fontSize: 12, color: 'var(--text-secondary)' }}>
                  <span style={{ color: 'var(--text-muted)' }}>{k}:</span> {v}
                </span>
              ))}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

// ── Extended draft type ───────────────────────────────────────────────────────
interface ExtendedDraft extends ShoplineDraft {
  taxable: boolean;
  requiresShipping: boolean;
  costPerItem: string;
  inventoryLocationId: string;
  inventoryManaged: boolean;
  countryOfOrigin: string;
  hsCode: string;
  categoryId: string;
  categoryName: string;
  channelIds: string[];
  collectionIds: string[];
  seoTitle: string;
  seoDescription: string;
  seoHandle: string;
  tagsList: string[];
}

// ── Country list ──────────────────────────────────────────────────────────────
const COUNTRIES = [
  { code: '', name: 'Select country…' },
  { code: 'GB', name: 'United Kingdom' }, { code: 'US', name: 'United States' },
  { code: 'DE', name: 'Germany' }, { code: 'FR', name: 'France' },
  { code: 'IT', name: 'Italy' }, { code: 'ES', name: 'Spain' },
  { code: 'NL', name: 'Netherlands' }, { code: 'BE', name: 'Belgium' },
  { code: 'PL', name: 'Poland' }, { code: 'SE', name: 'Sweden' },
  { code: 'CN', name: 'China' }, { code: 'IN', name: 'India' },
  { code: 'JP', name: 'Japan' }, { code: 'KR', name: 'South Korea' },
  { code: 'TW', name: 'Taiwan' }, { code: 'VN', name: 'Vietnam' },
  { code: 'MY', name: 'Malaysia' }, { code: 'SG', name: 'Singapore' },
  { code: 'TH', name: 'Thailand' }, { code: 'PH', name: 'Philippines' },
  { code: 'ID', name: 'Indonesia' }, { code: 'BD', name: 'Bangladesh' },
  { code: 'TR', name: 'Turkey' }, { code: 'CA', name: 'Canada' },
  { code: 'AU', name: 'Australia' }, { code: 'MX', name: 'Mexico' },
  { code: 'BR', name: 'Brazil' }, { code: 'AE', name: 'UAE' },
];

// ============================================================================
// MAIN COMPONENT
// ============================================================================
export default function ShoplineListingCreate() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const productId    = searchParams.get('product_id') || '';
  const credentialId = searchParams.get('credential_id') || '';

  const [loading,        setLoading]        = useState(true);
  const [error,          setError]          = useState('');
  const [draft,          setDraft]          = useState<ExtendedDraft | null>(null);
  const [submitting,     setSubmitting]     = useState(false);
  const [publishOnSubmit, setPublishOnSubmit] = useState(true);
  const [submitResult,   setSubmitResult]   = useState<any>(null);

  // VAR-01 — Variants
  const [variants,         setVariants]         = useState<ChannelVariantDraft[]>([]);
  const [isVariantProduct, setIsVariantProduct] = useState(false);
  const [variantSplitMode, setVariantSplitMode] = useState(false);

  // Store data fetched from Shopline
  const [locations,         setLocations]         = useState<Array<{ id: string; name: string; active: boolean }>>([]);
  const [channels,          setChannels]          = useState<Array<{ id: string; name: string }>>([]);
  const [existingTags,      setExistingTags]      = useState<string[]>([]);
  const [categories,        setCategories]        = useState<Array<{ id: string; full_name: string }>>([]);
  const [productTypes,      setProductTypes]      = useState<string[]>([]);
  const [collections,       setCollections]       = useState<Array<{ id: string; title: string; handle: string }>>([]);
  const [storeDataLoading,  setStoreDataLoading]  = useState(false);

  const [skuDuplicateChannels, setSkuDuplicateChannels] = useState<string[]>([]);

  const API_BASE = (import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1');

  // ── Load ──────────────────────────────────────────────────────────────────
  useEffect(() => {
    if (!productId) { setError('No product_id provided.'); setLoading(false); return; }
    prepareListing(productId);
  }, [productId]);

  useEffect(() => {
    if (credentialId) loadStoreData();
  }, [credentialId]);

  async function prepareListing(pid: string) {
    setLoading(true); setError('');
    try {
      const res = await shoplineApi.prepare({ product_id: pid, credential_id: credentialId || undefined });
      const data = res.data;
      if (!data.ok) { setError(data.error || 'Failed to prepare listing'); setLoading(false); return; }
      if (data.draft) {
        const tagsList = (data.draft.tags || '').split(',').map((t: string) => t.trim()).filter(Boolean);
        setDraft({
          ...data.draft,
          bulletPoints: data.draft.bulletPoints || [],
          customAttributes: data.draft.customAttributes || [],
          taxable: true,
          requiresShipping: true,
          costPerItem: '',
          inventoryLocationId: '',
          inventoryManaged: true,
          countryOfOrigin: '',
          hsCode: '',
          categoryId: '',
          categoryName: '',
          channelIds: [],
          collectionIds: [],
          seoTitle: '',
          seoDescription: '',
          seoHandle: '',
          tagsList,
        } as ExtendedDraft);
        if (data.draft.variants?.length > 0) { setVariants(data.draft.variants); setIsVariantProduct(true); }
      }
    } catch (e: any) { setError(e?.response?.data?.error || e?.message || 'Network error'); }
    finally { setLoading(false); }
  }

  async function loadStoreData() {
    setStoreDataLoading(true);
    const headers = { 'X-Tenant-Id': (await import('../../contexts/TenantContext')).getActiveTenantId() };
    const credParam = credentialId ? `?credential_id=${credentialId}` : '';
    try {
      const [locRes, chanRes, tagRes, typeRes, colRes, catRes] = await Promise.allSettled([
        fetch(`${API_BASE}/shopline/locations${credParam}`, { headers }),
        fetch(`${API_BASE}/shopline/channels${credParam}`, { headers }),
        fetch(`${API_BASE}/shopline/tags${credParam}`, { headers }),
        fetch(`${API_BASE}/shopline/types${credParam}`, { headers }),
        fetch(`${API_BASE}/shopline/collections${credParam}`, { headers }),
        fetch(`${API_BASE}/shopline/categories${credParam}`, { headers }),
      ]);
      if (locRes.status === 'fulfilled' && locRes.value.ok) {
        const d = await locRes.value.json();
        setLocations((d.locations || []).filter((l: any) => l.active !== false));
      }
      if (chanRes.status === 'fulfilled' && chanRes.value.ok) {
        const d = await chanRes.value.json();
        setChannels(d.channels || []);
      }
      if (tagRes.status === 'fulfilled' && tagRes.value.ok) {
        const d = await tagRes.value.json();
        const tagArr = Array.isArray(d.tags) ? d.tags : (d.tags || '').split(',').map((t: string) => t.trim()).filter(Boolean);
        setExistingTags(tagArr);
      }
      if (typeRes.status === 'fulfilled' && typeRes.value.ok) {
        const d = await typeRes.value.json();
        setProductTypes(d.types || []);
      }
      if (colRes.status === 'fulfilled' && colRes.value.ok) {
        const d = await colRes.value.json();
        setCollections(d.collections || []);
      }
      if (catRes.status === 'fulfilled' && catRes.value.ok) {
        const d = await catRes.value.json();
        setCategories(d.categories || []);
      }
    } catch { /* non-fatal */ }
    finally { setStoreDataLoading(false); }
  }

  // ── Draft helpers ─────────────────────────────────────────────────────────
  const updateDraft = (field: keyof ExtendedDraft, value: unknown) =>
    setDraft(d => d ? { ...d, [field]: value } : d);

  const updateVariant = (id: string, field: keyof ChannelVariantDraft, value: any) =>
    setVariants(vs => vs.map(v => v.id === id ? { ...v, [field]: value } : v));

  // Pricing tiers
  const tiers: ShoplinePricingTier[] = draft?.pricingTiers || [];
  const setTiers = (t: ShoplinePricingTier[]) => updateDraft('pricingTiers', t);
  const addTier = () => setTiers([...tiers, { minQty: 2, pricePerUnit: '' }]);
  const removeTier = (i: number) => setTiers(tiers.filter((_, idx) => idx !== i));
  const updateTier = (i: number, field: keyof ShoplinePricingTier, value: string | number) =>
    setTiers(tiers.map((t, idx) => idx === i ? { ...t, [field]: value } : t));

  // Profit / margin
  const price  = parseFloat(draft?.price || '0') || 0;
  const cost   = parseFloat(draft?.costPerItem || '0') || 0;
  const profit = price - cost;
  const margin = price > 0 ? ((profit / price) * 100) : 0;

  // Variant analysis
  const activeVariants = variants.filter(v => v.active);
  const optionKeys = activeVariants.length > 0 ? Object.keys(activeVariants[0].combination) : [];
  const tooManyOptions  = optionKeys.length > 3;
  const tooManyVariants = activeVariants.length > 100;
  const variantChunks   = tooManyVariants
    ? Array.from({ length: Math.ceil(activeVariants.length / 100) }, (_, i) => activeVariants.slice(i * 100, (i + 1) * 100))
    : [];

  // SKU duplicate check
  const checkSKUDuplicate = async (sku: string) => {
    if (!sku.trim()) { setSkuDuplicateChannels([]); return; }
    try {
      const { getActiveTenantId } = await import('../../contexts/TenantContext');
      const res = await fetch(`${API_BASE}/listings/check-sku?sku=${encodeURIComponent(sku)}`, { headers: { 'X-Tenant-Id': getActiveTenantId() || '' } });
      if (res.ok) { const d = await res.json(); setSkuDuplicateChannels(d.isDuplicate ? (d.existingChannels || []) : []); }
    } catch { /* non-blocking */ }
  };

  // ── Submit ────────────────────────────────────────────────────────────────
  async function handleSubmit(variantsOverride?: ChannelVariantDraft[]) {
    if (!draft) return;
    if (!draft.title.trim()) { alert('Title is required.'); return; }
    if (!draft.sku.trim() && !isVariantProduct) { alert('SKU is required.'); return; }
    if (!draft.price.trim() && !isVariantProduct) { alert('Price is required.'); return; }
    setSubmitting(true); setSubmitResult(null);
    try {
      const submitDraft = {
        ...draft,
        tags: (draft.tagsList || []).join(', '),
        variants: variantsOverride || (isVariantProduct ? variants : []),
      };
      const res = await shoplineApi.submit({
        product_id: productId,
        credential_id: credentialId || undefined,
        draft: submitDraft as any,
        publish: publishOnSubmit,
      });
      setSubmitResult({
        ok: res.data.ok,
        shoplineProductId: res.data.shoplineProductId,
        url: res.data.url,
        pricingRulesCreated: res.data.pricingRulesCreated,
        warnings: res.data.warnings,
        error: res.data.error,
      });
    } catch (e: any) {
      setSubmitResult({ ok: false, error: e?.response?.data?.error || e?.message || 'Network error' });
    } finally { setSubmitting(false); }
  }

  const removeImage = (idx: number) => updateDraft('images', draft?.images.filter((_, i) => i !== idx) || []);
  const addImageUrl = () => { const url = window.prompt('Image URL:'); if (url?.trim()) updateDraft('images', [...(draft?.images || []), url.trim()]); };

  if (loading) return (
    <div style={{ padding: 40, textAlign: 'center', color: 'var(--text-muted)' }}>
      <div style={{ fontSize: 28, marginBottom: 12 }}>⏳</div>
      Preparing Shopline listing…
    </div>
  );
  if (error) return (
    <div style={{ padding: 40, textAlign: 'center' }}>
      <div style={{ fontSize: 28, marginBottom: 12 }}>❌</div>
      <div style={{ color: 'var(--danger)', marginBottom: 16 }}>{error}</div>
      <button onClick={() => navigate(-1)} style={{ padding: '8px 18px', borderRadius: 6, border: '1px solid var(--border)', background: 'transparent', color: 'var(--text-primary)', cursor: 'pointer' }}>← Back</button>
    </div>
  );
  if (!draft) return null;

  return (
    <div style={{ maxWidth: 860, margin: '0 auto', padding: '20px 16px 80px' }}>

      {/* Header */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 20 }}>
        <button onClick={() => navigate(-1)} style={{ background: 'transparent', border: 'none', cursor: 'pointer', color: 'var(--text-muted)', fontSize: 18 }}>←</button>
        <div style={{ width: 36, height: 36, borderRadius: 8, background: `${SHOPLINE_TEAL}22`, display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 18 }}>🛍️</div>
        <div>
          <h1 style={{ margin: 0, fontSize: 18, fontWeight: 700 }}>
            {draft.isUpdate ? 'Update Shopline Listing' : 'Create Shopline Listing'}
          </h1>
          {storeDataLoading && <div style={{ fontSize: 11, color: 'var(--text-muted)' }}>⏳ Loading store data…</div>}
        </div>
        {draft.isUpdate && draft.existingProductId && (
          <span style={{ marginLeft: 'auto', padding: '4px 10px', background: `${SHOPLINE_TEAL}18`, border: `1px solid ${SHOPLINE_TEAL}44`, borderRadius: 6, fontSize: 12, color: SHOPLINE_TEAL }}>
            Updating product {draft.existingProductId}
          </span>
        )}
      </div>

      {/* ── Product Details ── */}
      <Section title="Product Details" accent={SHOPLINE_TEAL}>
        <div>
          <label style={labelStyle}>Title *</label>
          <input value={draft.title} onChange={e => updateDraft('title', e.target.value)} style={inputStyle} placeholder="Product title" />
        </div>
        <div>
          <label style={labelStyle}>Description (HTML)</label>
          <textarea value={draft.description} onChange={e => updateDraft('description', e.target.value)} style={textareaStyle} placeholder="Product description — HTML supported" />
        </div>

        {/* Bullet points */}
        <div>
          <label style={{ ...labelStyle, marginBottom: 0 }}>
            Bullet Points <span style={{ fontWeight: 400, color: 'var(--text-muted)', textTransform: 'none' }}>(up to 8 — prepended to description)</span>
          </label>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 6, marginTop: 8 }}>
            {(draft.bulletPoints || []).map((bp, i) => (
              <div key={i} style={{ display: 'flex', gap: 8 }}>
                <input value={bp} onChange={e => { const bps = [...(draft.bulletPoints || [])]; bps[i] = e.target.value; updateDraft('bulletPoints', bps); }} style={{ ...inputStyle, flex: 1 }} placeholder={`Bullet point ${i + 1}`} />
                <button onClick={() => updateDraft('bulletPoints', (draft.bulletPoints || []).filter((_, j) => j !== i))} style={{ padding: '6px 10px', background: 'transparent', border: '1px solid var(--danger)', borderRadius: 6, color: 'var(--danger)', cursor: 'pointer' }}>✕</button>
              </div>
            ))}
            {(draft.bulletPoints || []).length < 8 && (
              <button onClick={() => updateDraft('bulletPoints', [...(draft.bulletPoints || []), ''])} style={{ alignSelf: 'flex-start', padding: '6px 14px', background: 'transparent', border: '1px dashed var(--border)', borderRadius: 6, color: 'var(--text-muted)', cursor: 'pointer', fontSize: 13 }}>+ Add bullet point</button>
            )}
          </div>
        </div>

        <Row>
          <div style={{ flex: 1, minWidth: 180 }}>
            <label style={labelStyle}>Vendor / Brand</label>
            <input value={draft.vendor} onChange={e => updateDraft('vendor', e.target.value)} style={inputStyle} placeholder="Brand name" />
          </div>
          <div style={{ flex: 1, minWidth: 180 }}>
            <label style={labelStyle}>Product Type</label>
            <input value={draft.productType} onChange={e => updateDraft('productType', e.target.value)}
              list="shopline-product-types" style={inputStyle} placeholder="e.g. Electronics" autoComplete="off" />
            <datalist id="shopline-product-types">
              {productTypes.map(t => <option key={t} value={t} />)}
            </datalist>
          </div>
        </Row>

        {/* Tags */}
        <div>
          <label style={labelStyle}>Tags</label>
          <TagPicker existingTags={existingTags} selectedTags={draft.tagsList || []} onChange={tags => updateDraft('tagsList', tags)} />
          <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 4 }}>
            {existingTags.length > 0 ? `${existingTags.length} existing tags loaded from your Shopline store` : 'No existing tags found — type new tags below'}
          </div>
        </div>

        {/* Category */}
        <div>
          <label style={labelStyle}>
            Product Category <span style={{ fontWeight: 400, textTransform: 'none', color: 'var(--text-muted)' }}>(Shopline taxonomy)</span>
          </label>
          {categories.length > 0
            ? <CategoryPicker categories={categories} value={draft.categoryId} onChange={(id, name) => { updateDraft('categoryId', id); updateDraft('categoryName', name); }} />
            : <div style={{ padding: '8px 12px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6, fontSize: 13, color: 'var(--text-muted)' }}>
                {storeDataLoading ? '⏳ Loading categories…' : 'Categories not available — you can set product_type above as a fallback'}
              </div>
          }
        </div>
      </Section>

      {/* ── Pricing & Inventory ── */}
      <Section title="Pricing & Inventory" accent="#22c55e">
        {!isVariantProduct && (
          <Row>
            <div style={{ flex: 1, minWidth: 120 }}>
              <label style={labelStyle}>SKU *</label>
              <input value={draft.sku} onChange={e => updateDraft('sku', e.target.value)} onBlur={e => checkSKUDuplicate(e.target.value)}
                style={{ ...inputStyle, borderColor: skuDuplicateChannels.length > 0 ? 'var(--warning)' : undefined }} placeholder="PROD-001" />
              {skuDuplicateChannels.length > 0 && <div style={{ fontSize: 11, color: '#fbbf24', marginTop: 3 }}>⚠ SKU already listed on: {skuDuplicateChannels.join(', ')}</div>}
            </div>
            <div style={{ flex: 1, minWidth: 120 }}>
              <label style={labelStyle}>Barcode / EAN</label>
              <input value={draft.barcode} onChange={e => updateDraft('barcode', e.target.value)} style={inputStyle} placeholder="5012345678900" />
            </div>
          </Row>
        )}

        <Row>
          <div style={{ flex: 1, minWidth: 100 }}>
            <label style={labelStyle}>Price *</label>
            <input value={draft.price} onChange={e => updateDraft('price', e.target.value)} style={inputStyle} type="number" step="0.01" min="0" placeholder="0.00" />
          </div>
          <div style={{ flex: 1, minWidth: 100 }}>
            <label style={labelStyle}>Compare-at Price <span style={{ fontWeight: 400, textTransform: 'none' }}>RRP/was</span></label>
            <input value={draft.compareAtPrice} onChange={e => updateDraft('compareAtPrice', e.target.value)} style={inputStyle} type="number" step="0.01" min="0" placeholder="0.00" />
          </div>
          <div style={{ flex: 1, minWidth: 100 }}>
            <label style={labelStyle}>Cost Per Item</label>
            <input value={draft.costPerItem} onChange={e => updateDraft('costPerItem', e.target.value)} style={inputStyle} type="number" step="0.01" min="0" placeholder="0.00" />
          </div>
        </Row>

        {/* Profit / margin */}
        {cost > 0 && price > 0 && (
          <div style={{ display: 'flex', gap: 16, padding: '10px 14px', background: profit >= 0 ? 'rgba(34,197,94,0.08)' : 'rgba(239,68,68,0.08)', borderRadius: 8, border: `1px solid ${profit >= 0 ? 'rgba(34,197,94,0.2)' : 'rgba(239,68,68,0.2)'}`, fontSize: 13 }}>
            <span>Profit: <strong style={{ color: profit >= 0 ? '#4ade80' : '#f87171' }}>£{profit.toFixed(2)}</strong></span>
            <span>Margin: <strong style={{ color: profit >= 0 ? '#4ade80' : '#f87171' }}>{margin.toFixed(1)}%</strong></span>
          </div>
        )}

        {/* Quantity pricing tiers */}
        <div>
          <label style={labelStyle}>
            Quantity Pricing Tiers <span style={{ fontWeight: 400, textTransform: 'none', color: 'var(--text-muted)' }}>(stored as product metadata)</span>
          </label>
          {tiers.map((tier, idx) => (
            <div key={idx} style={{ display: 'flex', gap: 8, marginBottom: 8, alignItems: 'center' }}>
              <span style={{ fontSize: 13, color: 'var(--text-muted)', whiteSpace: 'nowrap' }}>Buy</span>
              <input value={tier.minQty} onChange={e => updateTier(idx, 'minQty', parseInt(e.target.value) || 0)} style={{ ...inputStyle, width: 60 }} type="number" min="2" />
              <span style={{ fontSize: 13, color: 'var(--text-muted)', whiteSpace: 'nowrap' }}>or more →</span>
              <input value={tier.pricePerUnit} onChange={e => updateTier(idx, 'pricePerUnit', e.target.value)} style={{ ...inputStyle, width: 80 }} type="number" step="0.01" placeholder="each" />
              <button onClick={() => removeTier(idx)} style={{ padding: '6px 10px', background: 'transparent', border: '1px solid var(--danger)', borderRadius: 6, color: 'var(--danger)', cursor: 'pointer', fontSize: 12 }}>✕</button>
            </div>
          ))}
          <button onClick={addTier} style={{ padding: '6px 14px', background: 'transparent', border: '1px dashed var(--border)', borderRadius: 6, color: 'var(--text-muted)', cursor: 'pointer', fontSize: 13 }}>+ Add tier</button>
        </div>

        <Row>
          <div style={{ flex: 1, minWidth: 120 }}>
            <label style={labelStyle}>Quantity</label>
            <input value={draft.quantity} onChange={e => updateDraft('quantity', e.target.value)} style={inputStyle} type="number" min="0" placeholder="0" />
          </div>
          <div style={{ flex: 2, minWidth: 200 }}>
            <label style={labelStyle}>Inventory Location</label>
            {locations.length > 0
              ? <select value={draft.inventoryLocationId} onChange={e => updateDraft('inventoryLocationId', e.target.value)} style={inputStyle}>
                  <option value="">— Don't set inventory location —</option>
                  {locations.map(l => <option key={l.id} value={String(l.id)}>{l.name}</option>)}
                </select>
              : <div style={{ ...inputStyle, color: 'var(--text-muted)', fontSize: 13 }}>{storeDataLoading ? '⏳ Loading…' : 'No locations found'}</div>
            }
          </div>
        </Row>

        <Row>
          <label style={{ display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer', fontSize: 14 }}>
            <input type="checkbox" checked={draft.taxable} onChange={e => updateDraft('taxable', e.target.checked)} style={{ width: 16, height: 16 }} />
            Charge tax on this product
          </label>
          <label style={{ display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer', fontSize: 14 }}>
            <input type="checkbox" checked={draft.inventoryManaged} onChange={e => updateDraft('inventoryManaged', e.target.checked)} style={{ width: 16, height: 16 }} />
            Track inventory quantity
          </label>
        </Row>
      </Section>

      {/* ── Shipping & Customs ── */}
      <Section title="Shipping, Weight & Customs" accent="#f59e0b">
        <Row>
          <div style={{ flex: 1, minWidth: 140 }}>
            <label style={labelStyle}>Weight Value</label>
            <input value={draft.weightValue} onChange={e => updateDraft('weightValue', e.target.value)} style={inputStyle} type="number" step="0.001" min="0" placeholder="0.000" />
          </div>
          <div style={{ minWidth: 100 }}>
            <label style={labelStyle}>Unit</label>
            <select value={draft.weightUnit} onChange={e => updateDraft('weightUnit', e.target.value)} style={inputStyle}>
              <option value="kg">kg</option><option value="g">g</option><option value="lb">lb</option><option value="oz">oz</option>
            </select>
          </div>
          <div style={{ flex: 1, minWidth: 180 }}>
            <label style={labelStyle}>Country of Origin</label>
            <select value={draft.countryOfOrigin} onChange={e => updateDraft('countryOfOrigin', e.target.value)} style={inputStyle}>
              {COUNTRIES.map(c => <option key={c.code} value={c.code}>{c.name}</option>)}
            </select>
          </div>
          <div style={{ flex: 1, minWidth: 140 }}>
            <label style={labelStyle}>HS Code <span style={{ fontWeight: 400, textTransform: 'none' }}>Customs</span></label>
            <input value={draft.hsCode} onChange={e => updateDraft('hsCode', e.target.value)} style={inputStyle} placeholder="e.g. 8471.30" />
          </div>
        </Row>
        <label style={{ display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer', fontSize: 14 }}>
          <input type="checkbox" checked={draft.requiresShipping} onChange={e => updateDraft('requiresShipping', e.target.checked)} style={{ width: 16, height: 16 }} />
          This product requires shipping
        </label>
      </Section>

      {/* ── Sales Channels & Collections ── */}
      <Section title="Sales Channels & Collections" accent="#6366f1">
        <div>
          <label style={labelStyle}>
            Sales Channels <span style={{ fontWeight: 400, textTransform: 'none', color: 'var(--text-muted)' }}>(where this product is published)</span>
          </label>
          {channels.length > 0
            ? <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8, marginTop: 4 }}>
                {channels.map((ch: any) => {
                  const selected = (draft.channelIds || []).includes(String(ch.id));
                  return (
                    <button key={ch.id} onClick={() => {
                      const ids = draft.channelIds || [];
                      updateDraft('channelIds', selected ? ids.filter(id => id !== String(ch.id)) : [...ids, String(ch.id)]);
                    }} style={{ padding: '6px 14px', borderRadius: 20, fontSize: 13, cursor: 'pointer', fontWeight: 600, border: selected ? `1px solid ${SHOPLINE_TEAL}` : '1px solid var(--border)', background: selected ? `${SHOPLINE_TEAL}18` : 'transparent', color: selected ? SHOPLINE_TEAL : 'var(--text-muted)' }}>
                      {ch.name}
                    </button>
                  );
                })}
              </div>
            : <div style={{ fontSize: 13, color: 'var(--text-muted)', marginTop: 4 }}>{storeDataLoading ? '⏳ Loading…' : 'No sales channels found — product will use store default'}</div>
          }
        </div>
        <div>
          <label style={labelStyle}>Collections</label>
          {collections.length > 0
            ? <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8, marginTop: 4 }}>
                {collections.map((col: any) => {
                  const colId = String(col.id);
                  const selected = (draft.collectionIds || []).includes(colId);
                  return (
                    <button key={colId} onClick={() => {
                      const ids: string[] = draft.collectionIds || [];
                      updateDraft('collectionIds', selected ? ids.filter(id => id !== colId) : [...ids, colId]);
                    }} style={{ padding: '6px 14px', borderRadius: 20, fontSize: 13, cursor: 'pointer', fontWeight: 600, border: selected ? '1px solid #8b5cf6' : '1px solid var(--border)', background: selected ? '#8b5cf618' : 'transparent', color: selected ? '#8b5cf6' : 'var(--text-muted)' }}>
                      {col.title}
                    </button>
                  );
                })}
              </div>
            : <div style={{ fontSize: 13, color: 'var(--text-muted)', marginTop: 4 }}>{storeDataLoading ? '⏳ Loading…' : 'No collections found in your store'}</div>
          }
        </div>
      </Section>

      {/* ── Images ── */}
      <Section title={`Images (${draft.images.length})`} subtitle="First image = main · Double-click to set as primary" accent="#8b5cf6">
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8, marginBottom: 8 }}>
          {draft.images.map((img, idx) => (
            <div key={idx} style={{ display: 'flex', flexDirection: 'column', gap: 4, width: 110 }}>
              <div onDoubleClick={() => { if (idx > 0) { const imgs = [...draft.images]; [imgs[0], imgs[idx]] = [imgs[idx], imgs[0]]; updateDraft('images', imgs); } }}
                title={idx === 0 ? 'Primary image' : 'Double-click to set as primary'}
                style={{ position: 'relative', width: 110, height: 80, borderRadius: 6, border: idx === 0 ? `2px solid ${SHOPLINE_TEAL}` : '1px solid var(--border)', overflow: 'hidden', cursor: idx > 0 ? 'pointer' : 'default' }}>
                <img src={img} alt={(draft.imageAlts || [])[idx] || ''} style={{ width: '100%', height: '100%', objectFit: 'cover' }} onError={e => { (e.target as HTMLImageElement).alt = '⚠️'; }} />
                <button onClick={() => removeImage(idx)} style={{ position: 'absolute', top: 2, right: 2, background: 'rgba(239,68,68,0.9)', border: 'none', borderRadius: 3, color: '#fff', cursor: 'pointer', fontSize: 11, padding: '1px 4px' }}>✕</button>
                {idx === 0 && <div style={{ position: 'absolute', bottom: 0, left: 0, right: 0, background: SHOPLINE_TEAL, color: '#fff', fontSize: 9, textAlign: 'center', padding: '1px 0' }}>MAIN</div>}
              </div>
              <input value={(draft.imageAlts || [])[idx] || ''} onChange={e => { const alts = [...(draft.imageAlts || draft.images.map(() => ''))]; alts[idx] = e.target.value; updateDraft('imageAlts', alts); }} placeholder="Alt text" style={{ fontSize: 10, padding: '3px 5px', borderRadius: 4, border: '1px solid var(--border)', background: 'var(--bg-primary)', color: 'var(--text-primary)', width: '100%' }} />
            </div>
          ))}
          <button onClick={addImageUrl} style={{ width: 80, height: 80, borderRadius: 6, border: '2px dashed var(--border)', background: 'transparent', color: 'var(--text-muted)', cursor: 'pointer', fontSize: 24, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>+</button>
        </div>
      </Section>

      {/* ── SEO ── */}
      <Section title="SEO" accent="#06b6d4">
        <Row>
          <div style={{ flex: 2, minWidth: 200 }}>
            <label style={labelStyle}>Page Title <span style={{ fontWeight: 400, textTransform: 'none', color: 'var(--text-muted)' }}>(defaults to product title)</span></label>
            <input value={draft.seoTitle} onChange={e => updateDraft('seoTitle', e.target.value)} style={inputStyle} placeholder={draft.title} maxLength={70} />
            <div style={{ fontSize: 11, color: draft.seoTitle.length > 60 ? '#fbbf24' : 'var(--text-muted)', marginTop: 3 }}>{draft.seoTitle.length}/70 chars</div>
          </div>
          <div style={{ flex: 1, minWidth: 160 }}>
            <label style={labelStyle}>URL Handle / Slug</label>
            <input value={draft.seoHandle} onChange={e => updateDraft('seoHandle', e.target.value.toLowerCase().replace(/\s+/g, '-').replace(/[^a-z0-9-]/g, ''))} style={inputStyle} placeholder="my-product-name" />
            <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 3 }}>Letters, numbers and hyphens only</div>
          </div>
        </Row>
        <div>
          <label style={labelStyle}>Meta Description</label>
          <textarea value={draft.seoDescription} onChange={e => updateDraft('seoDescription', e.target.value)} style={{ ...textareaStyle, minHeight: 80 }} placeholder="Brief description for search engines…" maxLength={320} />
          <div style={{ fontSize: 11, color: draft.seoDescription.length > 160 ? '#fbbf24' : 'var(--text-muted)', marginTop: 3 }}>{draft.seoDescription.length}/320 chars</div>
        </div>
      </Section>

      {/* ── Custom Attributes (metafields equivalent) ── */}
      <Section title={`Custom Attributes (${(draft.customAttributes || []).length})`} subtitle="Shopline product metadata" accent="#f59e0b">
        {(draft.customAttributes || []).map((attr, idx) => (
          <div key={idx} style={{ display: 'flex', gap: 8, alignItems: 'flex-start', marginBottom: 8, flexWrap: 'wrap' }}>
            <input placeholder="key" value={attr.key} onChange={e => { const attrs = [...(draft.customAttributes || [])]; attrs[idx] = { ...attrs[idx], key: e.target.value }; updateDraft('customAttributes', attrs); }}
              style={{ flex: '1 1 120px', padding: '6px 8px', borderRadius: 6, border: '1px solid var(--border)', background: 'var(--bg-primary)', color: 'var(--text-primary)', fontSize: 13 }} />
            <select value={attr.type} onChange={e => { const attrs = [...(draft.customAttributes || [])]; attrs[idx] = { ...attrs[idx], type: e.target.value }; updateDraft('customAttributes', attrs); }}
              style={{ flex: '0 0 120px', padding: '6px 8px', borderRadius: 6, border: '1px solid var(--border)', background: 'var(--bg-primary)', color: 'var(--text-primary)', fontSize: 13 }}>
              <option value="text">Text</option>
              <option value="number">Number</option>
              <option value="boolean">Boolean</option>
              <option value="json">JSON</option>
            </select>
            <input placeholder="value" value={attr.value} onChange={e => { const attrs = [...(draft.customAttributes || [])]; attrs[idx] = { ...attrs[idx], value: e.target.value }; updateDraft('customAttributes', attrs); }}
              style={{ flex: '2 1 180px', padding: '6px 8px', borderRadius: 6, border: '1px solid var(--border)', background: 'var(--bg-primary)', color: 'var(--text-primary)', fontSize: 13 }} />
            <button onClick={() => updateDraft('customAttributes', (draft.customAttributes || []).filter((_, i) => i !== idx))} style={{ padding: '6px 10px', borderRadius: 6, border: '1px solid var(--danger)', background: 'transparent', color: 'var(--danger)', cursor: 'pointer', fontSize: 12, flexShrink: 0 }}>✕</button>
          </div>
        ))}
        <button onClick={() => updateDraft('customAttributes', [...(draft.customAttributes || []), { key: '', value: '', type: 'text' }])} style={{ marginTop: 4, padding: '6px 14px', borderRadius: 6, border: '1px dashed var(--border)', background: 'transparent', color: 'var(--text-muted)', cursor: 'pointer', fontSize: 13 }}>+ Add Attribute</button>
      </Section>

      {/* ── Publish Settings ── */}
      <Section title="Publish Settings" accent="#6366f1">
        <label style={{ display: 'flex', alignItems: 'center', gap: 10, cursor: 'pointer', fontSize: 14 }}>
          <input type="checkbox" checked={publishOnSubmit} onChange={e => setPublishOnSubmit(e.target.checked)} style={{ width: 16, height: 16 }} />
          Publish immediately (status: <strong>active</strong>)
        </label>
        {!publishOnSubmit && <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>Product will be saved as a <strong>draft</strong>.</div>}
      </Section>

      {/* ── Variants ── */}
      {isVariantProduct && (
        <div style={{ marginBottom: 16, padding: 20, background: 'var(--bg-secondary)', borderRadius: 12, border: '1px solid var(--border)' }}>
          <h3 style={{ margin: '0 0 4px', fontSize: 15, fontWeight: 700, color: '#d946ef' }}>Variants ({activeVariants.length})</h3>

          {tooManyOptions && (
            <div style={{ padding: '12px 16px', background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 8, marginBottom: 12 }}>
              <strong style={{ color: '#f87171' }}>⚠ Too many variation options</strong>
              <p style={{ fontSize: 13, color: 'var(--text-secondary)', margin: '6px 0 0' }}>
                Shopline supports a maximum of <strong>3 option types</strong> per product (e.g. Size, Colour, Material).
                This product has <strong>{optionKeys.length} options</strong> ({optionKeys.join(', ')}).
                Please reduce the options in your PIM, or create separate listings per option group.
              </p>
            </div>
          )}

          {tooManyVariants && !tooManyOptions && (
            <div style={{ padding: '12px 16px', background: 'rgba(251,191,36,0.1)', border: '1px solid rgba(251,191,36,0.3)', borderRadius: 8, marginBottom: 12 }}>
              <strong style={{ color: '#fbbf24' }}>⚠ Too many variants ({activeVariants.length})</strong>
              <p style={{ fontSize: 13, color: 'var(--text-secondary)', margin: '6px 0 8px' }}>
                Shopline limits products to <strong>100 variants</strong>. You can split into {variantChunks.length} separate listings:
              </p>
              <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
                {variantChunks.map((chunk, i) => (
                  <button key={i} onClick={() => { setVariantSplitMode(true); handleSubmit(chunk); }} disabled={submitting}
                    style={{ padding: '6px 14px', background: '#fbbf2422', border: '1px solid #fbbf24', borderRadius: 6, color: '#fbbf24', cursor: 'pointer', fontSize: 13, fontWeight: 600 }}>
                    Submit Part {i + 1} ({chunk.length} variants)
                  </button>
                ))}
              </div>
            </div>
          )}

          {!tooManyOptions && !tooManyVariants && (
            <>
              <p style={{ fontSize: 12, color: 'var(--text-muted)', margin: '0 0 12px' }}>
                {optionKeys.length} option{optionKeys.length !== 1 ? 's' : ''}: <strong>{optionKeys.join(', ')}</strong> · {activeVariants.length} active variants
              </p>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                {variants.map(v => (
                  <VariantCard key={v.id} variant={v} optionKeys={optionKeys} onChange={(field, value) => updateVariant(v.id, field, value)} />
                ))}
              </div>
            </>
          )}
        </div>
      )}

      {/* ── Submit result ── */}
      {submitResult && (
        <div style={{ background: submitResult.ok ? 'rgba(34,197,94,0.08)' : 'rgba(239,68,68,0.1)', border: `1px solid ${submitResult.ok ? 'rgba(34,197,94,0.25)' : 'rgba(239,68,68,0.3)'}`, borderRadius: 10, padding: '16px 20px', marginBottom: 16 }}>
          {submitResult.ok ? (
            <>
              <div style={{ fontWeight: 700, color: '#4ade80', marginBottom: 8, fontSize: 15 }}>
                ✅ {draft.isUpdate ? 'Listing updated' : 'Listing created'} on Shopline!
              </div>
              {submitResult.url && <div style={{ fontSize: 13, marginBottom: 4 }}><a href={submitResult.url} target="_blank" rel="noopener noreferrer" style={{ color: SHOPLINE_TEAL }}>🔗 View on Shopline ↗</a></div>}
              {(submitResult.pricingRulesCreated ?? 0) > 0 && <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>🏷️ {submitResult.pricingRulesCreated} pricing tier{submitResult.pricingRulesCreated > 1 ? 's' : ''} saved</div>}
              {submitResult.warnings?.length > 0 && (
                <div style={{ marginTop: 8, fontSize: 12, color: '#fbbf24' }}>
                  <strong>⚠️ Warnings:</strong>
                  <ul style={{ margin: '4px 0 0', paddingLeft: 20 }}>{submitResult.warnings.map((w: string, i: number) => <li key={i}>{w}</li>)}</ul>
                </div>
              )}
              <div style={{ display: 'flex', gap: 10, marginTop: 12 }}>
                <button onClick={() => navigate('/marketplace/listings')} style={{ padding: '8px 18px', background: SHOPLINE_TEAL, border: 'none', borderRadius: 6, color: '#fff', cursor: 'pointer', fontWeight: 600, fontSize: 13 }}>View All Listings</button>
                <button onClick={() => navigate(`/products/${productId}`)} style={{ padding: '8px 18px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', cursor: 'pointer', fontSize: 13 }}>Back to Product</button>
              </div>
            </>
          ) : (
            <div style={{ color: '#f87171' }}><strong>❌ Submission failed:</strong> {submitResult.error}</div>
          )}
        </div>
      )}

      {/* ── Action bar ── */}
      {!submitResult?.ok && (
        <div style={{ position: 'sticky', bottom: 0, background: 'var(--bg-secondary)', borderTop: '1px solid var(--border)', padding: '12px 20px', display: 'flex', alignItems: 'center', gap: 12, borderRadius: '0 0 10px 10px', zIndex: 10 }}>
          <button onClick={() => handleSubmit()} disabled={submitting || tooManyOptions || (tooManyVariants && !variantSplitMode)}
            style={{ padding: '10px 28px', background: (submitting || tooManyOptions) ? 'var(--bg-elevated)' : SHOPLINE_TEAL, border: 'none', borderRadius: 7, color: (submitting || tooManyOptions) ? 'var(--text-muted)' : '#fff', fontWeight: 700, fontSize: 14, cursor: (submitting || tooManyOptions) ? 'not-allowed' : 'pointer' }}>
            {submitting ? '⏳ Submitting…' : draft.isUpdate ? '🔄 Update Listing' : publishOnSubmit ? '🚀 Create & Publish' : '💾 Create as Draft'}
          </button>
          <button onClick={() => navigate(-1)} disabled={submitting} style={{ padding: '10px 18px', background: 'transparent', border: '1px solid var(--border)', borderRadius: 7, color: 'var(--text-muted)', cursor: 'pointer', fontSize: 13 }}>Cancel</button>
          {tooManyOptions && <span style={{ fontSize: 12, color: '#f87171' }}>⚠ Reduce to max 3 option types before submitting</span>}
          {tooManyVariants && !tooManyOptions && <span style={{ fontSize: 12, color: '#fbbf24' }}>⚠ Use split buttons above to submit in parts</span>}
        </div>
      )}
    </div>
  );
}

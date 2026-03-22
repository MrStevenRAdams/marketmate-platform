// ============================================================================
// SHOPIFY LISTING CREATE PAGE
// ============================================================================
import { useState, useEffect, useRef } from 'react';
import { useSearchParams, useNavigate } from 'react-router-dom';
import { shopifyApi, ShopifyDraft, ShopifyPricingTier } from '../../services/shopify-api';
import type { ChannelVariantDraft } from '../../services/channel-types';

const SHOPIFY_GREEN = '#96bf48';

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

function Section({ title, subtitle, accent = SHOPIFY_GREEN, children }: {
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

// ── Tag picker popup ──────────────────────────────────────────────────────────
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
          <span key={tag} style={{ display: 'inline-flex', alignItems: 'center', gap: 4, padding: '2px 8px', background: `${SHOPIFY_GREEN}22`, border: `1px solid ${SHOPIFY_GREEN}55`, borderRadius: 99, fontSize: 12, color: SHOPIFY_GREEN }}>
            {tag}
            <button onClick={e => { e.stopPropagation(); toggle(tag); }} style={{ background: 'none', border: 'none', cursor: 'pointer', color: SHOPIFY_GREEN, padding: 0, fontSize: 12, lineHeight: 1 }}>✕</button>
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
            {filtered.length === 0 && search === '' && <div style={{ padding: '10px 14px', fontSize: 13, color: 'var(--text-muted)' }}>No more tags available</div>}
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
            <button onClick={addNew} style={{ padding: '6px 12px', background: SHOPIFY_GREEN, border: 'none', borderRadius: 6, color: '#fff', cursor: 'pointer', fontSize: 13, fontWeight: 600 }}>Add</button>
          </div>
        </div>
      )}
    </div>
  );
}

// ── Category picker ───────────────────────────────────────────────────────────
function CategoryPicker({ categories, value, onChange }: {
  categories: Array<{ id: string; full_name: string }>; value: string; onChange: (id: string, name: string) => void;
}) {
  const [search, setSearch] = useState('');
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const selected = categories.find(c => c.id === value);

  useEffect(() => {
    const handler = (e: MouseEvent) => { if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false); };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, []);

  const filtered = categories.filter(c => c.full_name?.toLowerCase().includes(search.toLowerCase())).slice(0, 50);

  return (
    <div ref={ref} style={{ position: 'relative' }}>
      <div style={{ display: 'flex', gap: 8 }}>
        <input readOnly value={selected?.full_name || ''} placeholder="Click to select category…" onClick={() => setOpen(o => !o)}
          style={{ ...inputStyle, cursor: 'pointer', flex: 1 }} />
        {value && <button onClick={() => onChange('', '')} style={{ padding: '8px 12px', background: 'transparent', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-muted)', cursor: 'pointer', fontSize: 12 }}>Clear</button>}
      </div>
      {open && (
        <div style={{ position: 'absolute', top: '100%', left: 0, right: 0, zIndex: 100, background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 8, boxShadow: '0 8px 24px rgba(0,0,0,0.3)', marginTop: 4, maxHeight: 320, overflow: 'hidden', display: 'flex', flexDirection: 'column' }}>
          <div style={{ padding: 10, borderBottom: '1px solid var(--border)' }}>
            <input autoFocus value={search} onChange={e => setSearch(e.target.value)} placeholder="Search categories…" style={{ ...inputStyle, fontSize: 13 }} />
          </div>
          <div style={{ overflowY: 'auto', flex: 1 }}>
            {filtered.length === 0 && <div style={{ padding: '12px 14px', fontSize: 13, color: 'var(--text-muted)' }}>No categories found</div>}
            {filtered.map(cat => (
              <div key={cat.id} onClick={() => { onChange(cat.id, cat.full_name); setOpen(false); setSearch(''); }}
                style={{ padding: '8px 14px', cursor: 'pointer', fontSize: 13, color: 'var(--text-primary)' }}
                onMouseEnter={e => (e.currentTarget.style.background = 'var(--bg-elevated)')}
                onMouseLeave={e => (e.currentTarget.style.background = 'transparent')}>
                {cat.full_name}
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

// ── Variant Card ─────────────────────────────────────────────────────────────
// Expanded per-variant editor with all editable fields including image and weight.
function VariantCard({
  variant, optionKeys, onChange,
}: {
  variant: ChannelVariantDraft;
  optionKeys: string[];
  onChange: (field: keyof ChannelVariantDraft, value: any) => void;
}) {
  const [expanded, setExpanded] = useState(false);
  const combinationLabel = optionKeys.map(k => variant.combination[k] || '—').join(' / ');

  return (
    <div style={{
      border: `1px solid ${variant.active ? 'var(--border)' : 'var(--border)'}`,
      borderRadius: 8,
      background: variant.active ? 'var(--bg-elevated)' : 'var(--bg-secondary)',
      opacity: variant.active ? 1 : 0.6,
      overflow: 'hidden',
    }}>
      {/* ── Header row ── */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '10px 14px', cursor: 'pointer' }}
        onClick={() => setExpanded(e => !e)}>
        <input type="checkbox" checked={variant.active} onClick={e => e.stopPropagation()}
          onChange={e => onChange('active', e.target.checked)}
          style={{ width: 15, height: 15, flexShrink: 0 }} />
        {variant.image && (
          <img src={variant.image} alt="" style={{ width: 36, height: 36, objectFit: 'cover', borderRadius: 4, flexShrink: 0 }} onError={e => { (e.target as HTMLImageElement).style.display = 'none'; }} />
        )}
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ fontWeight: 600, fontSize: 13, color: 'var(--text-primary)' }}>{combinationLabel}</div>
          <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 1 }}>
            SKU: {variant.sku || '—'} · £{variant.price || '—'} · Stock: {variant.stock || '0'}
          </div>
        </div>
        <div style={{ fontSize: 18, color: 'var(--text-muted)', userSelect: 'none' }}>{expanded ? '▲' : '▼'}</div>
      </div>

      {/* ── Expanded fields ── */}
      {expanded && (
        <div style={{ padding: '12px 14px', borderTop: '1px solid var(--border)', display: 'flex', flexDirection: 'column', gap: 10 }}>

          {/* Row 1: SKU + EAN */}
          <div style={{ display: 'flex', gap: 10, flexWrap: 'wrap' }}>
            <div style={{ flex: '1 1 140px' }}>
              <label style={{ ...labelStyle, fontSize: 10 }}>SKU *</label>
              <input value={variant.sku} onChange={e => onChange('sku', e.target.value)}
                style={{ ...inputStyle, fontSize: 13 }} placeholder="PROD-001-VAR" />
            </div>
            <div style={{ flex: '1 1 140px' }}>
              <label style={{ ...labelStyle, fontSize: 10 }}>EAN / Barcode</label>
              <input value={variant.ean} onChange={e => onChange('ean', e.target.value)}
                style={{ ...inputStyle, fontSize: 13 }} placeholder="5012345678900" />
            </div>
          </div>

          {/* Row 2: Price + Compare-at + Stock */}
          <div style={{ display: 'flex', gap: 10, flexWrap: 'wrap' }}>
            <div style={{ flex: '1 1 90px' }}>
              <label style={{ ...labelStyle, fontSize: 10 }}>Price *</label>
              <input value={variant.price} onChange={e => onChange('price', e.target.value)}
                style={{ ...inputStyle, fontSize: 13 }} type="number" step="0.01" min="0" placeholder="0.00" />
            </div>
            <div style={{ flex: '1 1 90px' }}>
              <label style={{ ...labelStyle, fontSize: 10 }}>Stock</label>
              <input value={variant.stock} onChange={e => onChange('stock', e.target.value)}
                style={{ ...inputStyle, fontSize: 13 }} type="number" min="0" placeholder="0" />
            </div>
          </div>

          {/* Row 3: Weight */}
          <div style={{ display: 'flex', gap: 10, flexWrap: 'wrap' }}>
            <div style={{ flex: '1 1 120px' }}>
              <label style={{ ...labelStyle, fontSize: 10 }}>Weight</label>
              <input value={variant.weight || ''} onChange={e => onChange('weight', e.target.value)}
                style={{ ...inputStyle, fontSize: 13 }} type="number" step="0.001" min="0" placeholder="0.000" />
            </div>
            <div style={{ minWidth: 90 }}>
              <label style={{ ...labelStyle, fontSize: 10 }}>Unit</label>
              <select value={variant.weightUnit || 'kg'} onChange={e => onChange('weightUnit', e.target.value)} style={{ ...inputStyle, fontSize: 13 }}>
                <option value="kg">kg</option>
                <option value="g">g</option>
                <option value="lb">lb</option>
                <option value="oz">oz</option>
              </select>
            </div>
          </div>

          {/* Row 4: Title override */}
          <div>
            <label style={{ ...labelStyle, fontSize: 10 }}>Title Override <span style={{ fontWeight: 400, textTransform: 'none', color: 'var(--text-muted)' }}>(optional — defaults to product title)</span></label>
            <input value={variant.title || ''} onChange={e => onChange('title', e.target.value)}
              style={{ ...inputStyle, fontSize: 13 }} placeholder="Leave blank to use product title" />
          </div>

          {/* Row 5: Image URL override */}
          <div>
            <label style={{ ...labelStyle, fontSize: 10 }}>Primary Image URL <span style={{ fontWeight: 400, textTransform: 'none', color: 'var(--text-muted)' }}>(override)</span></label>
            <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
              <input value={variant.image || ''} onChange={e => onChange('image', e.target.value)}
                style={{ ...inputStyle, fontSize: 13, flex: 1 }} placeholder="https://…" />
              {variant.image && (
                <img src={variant.image} alt="" style={{ width: 40, height: 40, objectFit: 'cover', borderRadius: 4, border: '1px solid var(--border)', flexShrink: 0 }}
                  onError={e => { (e.target as HTMLImageElement).style.display = 'none'; }} />
              )}
            </div>
          </div>

          {/* Row 6: Additional images */}
          <div>
            <label style={{ ...labelStyle, fontSize: 10 }}>Additional Images</label>
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
              {(variant.images || []).map((img, i) => (
                <div key={i} style={{ position: 'relative' }}>
                  <img src={img} alt="" style={{ width: 48, height: 48, objectFit: 'cover', borderRadius: 4, border: '1px solid var(--border)' }}
                    onError={e => { (e.target as HTMLImageElement).alt = '⚠'; }} />
                  <button onClick={() => onChange('images', (variant.images || []).filter((_, j) => j !== i))}
                    style={{ position: 'absolute', top: -4, right: -4, width: 16, height: 16, background: 'var(--danger)', border: 'none', borderRadius: '50%', color: '#fff', cursor: 'pointer', fontSize: 9, display: 'flex', alignItems: 'center', justifyContent: 'center', padding: 0 }}>✕</button>
                </div>
              ))}
              <button onClick={() => {
                const url = window.prompt('Image URL:');
                if (url?.trim()) onChange('images', [...(variant.images || []), url.trim()]);
              }} style={{ width: 48, height: 48, borderRadius: 4, border: '2px dashed var(--border)', background: 'transparent', color: 'var(--text-muted)', cursor: 'pointer', fontSize: 20 }}>+</button>
            </div>
          </div>

          {/* Combination values (read-only display) */}
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
interface ExtendedDraft extends ShopifyDraft {
  taxable: boolean;
  requiresShipping: boolean;
  unitPriceMeasure: string;
  unitPriceMeasurementUnit: string;
  unitPriceQuantityUnit: string;
  costPerItem: string;
  inventoryLocationId: string;
  inventoryManaged: boolean;
  countryOfOrigin: string;
  hsCode: string;
  categoryId: string;
  categoryName: string;
  publicationIds: string[];
  seoTitle: string;
  seoDescription: string;
  tagsList: string[];  // parsed from draft.tags
}

// ── Country list ─────────────────────────────────────────────────────────────
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
  { code: 'BD', name: 'Bangladesh' }, { code: 'TR', name: 'Turkey' },
  { code: 'CA', name: 'Canada' }, { code: 'AU', name: 'Australia' },
  { code: 'MX', name: 'Mexico' }, { code: 'BR', name: 'Brazil' },
];

// ============================================================================
// MAIN COMPONENT
// ============================================================================
export default function ShopifyListingCreate() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const productId    = searchParams.get('product_id') || '';
  const credentialId = searchParams.get('credential_id') || '';

  const [loading,   setLoading]   = useState(true);
  const [error,     setError]     = useState('');
  const [draft,     setDraft]     = useState<ExtendedDraft | null>(null);
  const [submitting,    setSubmitting]    = useState(false);
  const [publishOnSubmit, setPublishOnSubmit] = useState(true);
  const [submitResult,  setSubmitResult]  = useState<any>(null);

  // VAR-01 — Variants
  const [variants, setVariants] = useState<ChannelVariantDraft[]>([]);
  const [isVariantProduct, setIsVariantProduct] = useState(false);
  const [variantSplitMode, setVariantSplitMode] = useState(false);
  const [variantSplitChunk, setVariantSplitChunk] = useState(0);

  // Store data fetched from Shopify
  const [locations, setLocations]         = useState<Array<{ id: number; name: string; active: boolean }>>([]);
  const [publications, setPublications]   = useState<Array<{ id: number; name: string }>>([]);
  const [existingTags, setExistingTags]   = useState<string[]>([]);
  const [categories, setCategories]       = useState<Array<{ id: string; full_name: string }>>([]);
  const [productTypes, setProductTypes]   = useState<string[]>([]);
  const [collections, setCollections]     = useState<Array<{ id: number; title: string; handle: string; type: string }>>([]);
  const [storeDataLoading, setStoreDataLoading] = useState(false);

  // Pricing
  const [costPerItem, setCostPerItem]   = useState('');
  const [skuDuplicateChannels, setSkuDuplicateChannels] = useState<string[]>([]);

  const API_BASE = (import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1');

  // ── Load ────────────────────────────────────────────────────────────────
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
      const res = await shopifyApi.prepare({ product_id: pid, credential_id: credentialId || undefined });
      const data = res.data;
      if (!data.ok) { setError(data.error || 'Failed to prepare listing'); setLoading(false); return; }
      if (data.draft) {
        const tagsList = (data.draft.tags || '').split(',').map((t: string) => t.trim()).filter(Boolean);
        setDraft({
          ...data.draft,
          metafields: data.draft.metafields || [],
          bulletPoints: data.draft.bulletPoints || [],
          paymentMethods: data.draft.paymentMethods || [],
          taxable: true,
          requiresShipping: true,
          unitPriceMeasure: '',
          unitPriceMeasurementUnit: 'ml',
          unitPriceQuantityUnit: 'cl',
          costPerItem: '',
          inventoryLocationId: '',
          inventoryManaged: true,
          countryOfOrigin: '',
          hsCode: '',
          categoryId: '',
          categoryName: '',
          publicationIds: [],
          seoTitle: '',
          seoDescription: '',
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
      const [locRes, pubRes, tagRes, typeRes, colRes, catRes] = await Promise.allSettled([
        fetch(`${API_BASE}/shopify/locations${credParam}`, { headers }),
        fetch(`${API_BASE}/shopify/publications${credParam}`, { headers }),
        fetch(`${API_BASE}/shopify/tags${credParam}`, { headers }),
        fetch(`${API_BASE}/shopify/types${credParam}`, { headers }),
        fetch(`${API_BASE}/shopify/collections${credParam}`, { headers }),
        fetch(`${API_BASE}/shopify/categories${credParam}`, { headers }),
      ]);
      if (locRes.status === 'fulfilled' && locRes.value.ok) {
        const d = await locRes.value.json();
        setLocations((d.locations || []).filter((l: any) => l.active));
      }
      if (pubRes.status === 'fulfilled' && pubRes.value.ok) {
        const d = await pubRes.value.json();
        setPublications(d.publications || []);
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

  // ── Draft helpers ──────────────────────────────────────────────────────
  const updateDraft = (field: keyof ExtendedDraft, value: unknown) =>
    setDraft(d => d ? { ...d, [field]: value } : d);

  const updateVariant = (id: string, field: keyof ChannelVariantDraft, value: any) =>
    setVariants(vs => vs.map(v => v.id === id ? { ...v, [field]: value } : v));

  // Pricing tiers
  const tiers: ShopifyPricingTier[] = draft?.pricingTiers || [];
  const setTiers = (t: ShopifyPricingTier[]) => updateDraft('pricingTiers', t);
  const addTier = () => setTiers([...tiers, { minQty: 2, pricePerUnit: '' }]);
  const removeTier = (i: number) => setTiers(tiers.filter((_, idx) => idx !== i));
  const updateTier = (i: number, field: keyof ShopifyPricingTier, value: string | number) =>
    setTiers(tiers.map((t, idx) => idx === i ? { ...t, [field]: value } : t));

  // Profit / margin
  const price = parseFloat(draft?.price || '0') || 0;
  const cost  = parseFloat(draft?.costPerItem || '0') || 0;
  const profit = price - cost;
  const margin = price > 0 ? ((profit / price) * 100) : 0;

  // Variant analysis
  const activeVariants = variants.filter(v => v.active);
  const optionKeys = activeVariants.length > 0 ? Object.keys(activeVariants[0].combination) : [];
  const tooManyOptions = optionKeys.length > 3;
  const tooManyVariants = activeVariants.length > 100;
  const variantChunks = tooManyVariants
    ? Array.from({ length: Math.ceil(activeVariants.length / 100) }, (_, i) => activeVariants.slice(i * 100, (i + 1) * 100))
    : [];

  // SKU check
  const checkSKUDuplicate = async (sku: string) => {
    if (!sku.trim()) { setSkuDuplicateChannels([]); return; }
    try {
      const { getActiveTenantId } = await import('../../contexts/TenantContext');
      const res = await fetch(`${API_BASE}/listings/check-sku?sku=${encodeURIComponent(sku)}`, { headers: { 'X-Tenant-Id': getActiveTenantId() || '' } });
      if (res.ok) { const d = await res.json(); setSkuDuplicateChannels(d.isDuplicate ? (d.existingChannels || []) : []); }
    } catch { /* non-blocking */ }
  };

  // ── Submit ─────────────────────────────────────────────────────────────
  async function handleSubmit(variantsOverride?: ChannelVariantDraft[]) {
    if (!draft) return;
    if (!draft.title.trim()) { alert('Title is required.'); return; }
    if (!draft.sku.trim() && !isVariantProduct) { alert('SKU is required.'); return; }
    if (!draft.price.trim() && !isVariantProduct) { alert('Price is required.'); return; }
    setSubmitting(true); setSubmitResult(null);
    try {
      // Merge tagsList back to tags string
      const submitDraft = {
        ...draft,
        tags: (draft.tagsList || []).join(', '),
        costPerItem: draft.costPerItem,
        variants: variantsOverride || (isVariantProduct ? variants : []),
      };
      const res = await shopifyApi.submit({ product_id: productId, credential_id: credentialId || undefined, draft: submitDraft as any, publish: publishOnSubmit });
      setSubmitResult({ ok: res.data.ok, shopifyProductId: res.data.shopifyProductId, url: res.data.url, priceRulesCreated: res.data.priceRulesCreated, warnings: res.data.warnings, error: res.data.error });
    } catch (e: any) { setSubmitResult({ ok: false, error: e?.response?.data?.error || e?.message || 'Network error' }); }
    finally { setSubmitting(false); }
  }

  // Image helpers
  const removeImage = (idx: number) => updateDraft('images', draft?.images.filter((_, i) => i !== idx) || []);
  const addImageUrl = () => { const url = window.prompt('Image URL:'); if (url?.trim()) updateDraft('images', [...(draft?.images || []), url.trim()]); };

  if (loading) return <div style={{ padding: 40, textAlign: 'center', color: 'var(--text-muted)' }}><div style={{ fontSize: 28, marginBottom: 12 }}>⏳</div>Preparing Shopify listing…</div>;
  if (error) return <div style={{ padding: 40, textAlign: 'center' }}><div style={{ fontSize: 28, marginBottom: 12 }}>❌</div><div style={{ color: 'var(--danger)', marginBottom: 16 }}>{error}</div><button onClick={() => navigate(-1)} style={{ padding: '8px 18px', borderRadius: 6, border: '1px solid var(--border)', background: 'transparent', color: 'var(--text-primary)', cursor: 'pointer' }}>← Back</button></div>;
  if (!draft) return null;

  return (
    <div style={{ maxWidth: 860, margin: '0 auto', padding: '20px 16px 80px' }}>
      {/* Header */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 20 }}>
        <button onClick={() => navigate(-1)} style={{ background: 'transparent', border: 'none', cursor: 'pointer', color: 'var(--text-muted)', fontSize: 18 }}>←</button>
        <div style={{ width: 36, height: 36, borderRadius: 8, background: `${SHOPIFY_GREEN}22`, display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 18 }}>🛒</div>
        <div>
          <h1 style={{ margin: 0, fontSize: 18, fontWeight: 700 }}>{draft.isUpdate ? 'Update Shopify Listing' : 'Create Shopify Listing'}</h1>
          {storeDataLoading && <div style={{ fontSize: 11, color: 'var(--text-muted)' }}>⏳ Loading store data…</div>}
        </div>
      </div>

      {/* ── Core Details ── */}
      <Section title="Product Details" accent={SHOPIFY_GREEN}>
        <div>
          <label style={labelStyle}>Title *</label>
          <input value={draft.title} onChange={e => updateDraft('title', e.target.value)} style={inputStyle} placeholder="Product title" />
        </div>
        <div>
          <label style={labelStyle}>Description (HTML)</label>
          <textarea value={draft.description} onChange={e => updateDraft('description', e.target.value)} style={textareaStyle} placeholder="Product description — HTML supported" />
        </div>
        <div>
          <label style={{ ...labelStyle, marginBottom: 0 }}>Bullet Points <span style={{ fontWeight: 400, color: 'var(--text-muted)', textTransform: 'none' }}>(up to 8 — prepended to description)</span></label>
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
              list="shopify-product-types" style={inputStyle} placeholder="e.g. Electronics" autoComplete="off" />
            <datalist id="shopify-product-types">
              {productTypes.map(t => <option key={t} value={t} />)}
            </datalist>
            {productTypes.length > 0 && <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 3 }}>{productTypes.length} types used in your store</div>}
          </div>
        </Row>

        {/* Tags */}
        <div>
          <label style={labelStyle}>Tags</label>
          <TagPicker existingTags={existingTags} selectedTags={draft.tagsList || []} onChange={tags => updateDraft('tagsList', tags)} />
          <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 4 }}>
            {existingTags.length > 0 ? `${existingTags.length} existing tags loaded from your Shopify store` : 'No existing tags found — type new tags in the input below'}
          </div>
        </div>

        {/* Category */}
        <div>
          <label style={labelStyle}>Product Category <span style={{ fontWeight: 400, textTransform: 'none', color: 'var(--text-muted)' }}>(Shopify standard taxonomy)</span></label>
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
              <input value={draft.sku} onChange={e => updateDraft('sku', e.target.value)} onBlur={e => checkSKUDuplicate(e.target.value)} style={{ ...inputStyle, borderColor: skuDuplicateChannels.length > 0 ? 'var(--warning)' : undefined }} placeholder="PROD-001" />
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

        {/* Profit / margin display */}
        {cost > 0 && price > 0 && (
          <div style={{ display: 'flex', gap: 16, padding: '10px 14px', background: profit >= 0 ? 'rgba(34,197,94,0.08)' : 'rgba(239,68,68,0.08)', borderRadius: 8, border: `1px solid ${profit >= 0 ? 'rgba(34,197,94,0.2)' : 'rgba(239,68,68,0.2)'}`, fontSize: 13 }}>
            <span>Profit: <strong style={{ color: profit >= 0 ? '#4ade80' : '#f87171' }}>£{profit.toFixed(2)}</strong></span>
            <span>Margin: <strong style={{ color: profit >= 0 ? '#4ade80' : '#f87171' }}>{margin.toFixed(1)}%</strong></span>
          </div>
        )}

        {/* Unit pricing */}
        <div>
          <label style={labelStyle}>Unit Pricing <span style={{ fontWeight: 400, textTransform: 'none', color: 'var(--text-muted)' }}>(EU/UK requirement — e.g. £1.20 per 100ml)</span></label>
          <Row>
            <div style={{ flex: 1, minWidth: 100 }}>
              <label style={{ ...labelStyle, fontSize: 10 }}>Reference Amount</label>
              <input value={draft.unitPriceMeasure} onChange={e => updateDraft('unitPriceMeasure', e.target.value)} style={inputStyle} placeholder="100" type="number" />
            </div>
            <div style={{ minWidth: 100 }}>
              <label style={{ ...labelStyle, fontSize: 10 }}>Reference Unit</label>
              <select value={draft.unitPriceMeasurementUnit} onChange={e => updateDraft('unitPriceMeasurementUnit', e.target.value)} style={inputStyle}>
                <option value="ml">ml</option><option value="cl">cl</option><option value="l">l</option>
                <option value="mg">mg</option><option value="g">g</option><option value="kg">kg</option>
                <option value="oz">oz</option><option value="lb">lb</option>
                <option value="cm">cm</option><option value="m">m</option><option value="ft">ft</option>
                <option value="in">in</option><option value="unit">unit</option>
              </select>
            </div>
            <div style={{ minWidth: 100 }}>
              <label style={{ ...labelStyle, fontSize: 10 }}>Sale Unit</label>
              <select value={draft.unitPriceQuantityUnit} onChange={e => updateDraft('unitPriceQuantityUnit', e.target.value)} style={inputStyle}>
                <option value="cl">cl</option><option value="ml">ml</option><option value="l">l</option>
                <option value="g">g</option><option value="kg">kg</option><option value="oz">oz</option>
                <option value="lb">lb</option><option value="unit">unit</option>
              </select>
            </div>
            {draft.unitPriceMeasure && price > 0 && (
              <div style={{ display: 'flex', alignItems: 'flex-end', paddingBottom: 2 }}>
                <span style={{ fontSize: 13, color: SHOPIFY_GREEN, fontWeight: 600 }}>
                  = £{(price / parseFloat(draft.unitPriceMeasure || '1')).toFixed(4)} per {draft.unitPriceMeasurementUnit}
                </span>
              </div>
            )}
          </Row>
        </div>

        {/* Quantity tiers */}
        <div>
          <label style={labelStyle}>Quantity Pricing Tiers <span style={{ fontWeight: 400, textTransform: 'none', color: 'var(--text-muted)' }}>(Shopify Price Rules)</span></label>
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

        {/* Tax + inventory tracking */}
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

      {/* ── Shipping & Weight ── */}
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
          <label style={labelStyle}>Sales Channels <span style={{ fontWeight: 400, textTransform: 'none', color: 'var(--text-muted)' }}>(where this product is published)</span></label>
          {publications.length > 0
            ? <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8, marginTop: 4 }}>
                {publications.map((pub: any) => {
                  const selected = (draft.publicationIds || []).includes(String(pub.id));
                  return (
                    <button key={pub.id} onClick={() => {
                      const ids = draft.publicationIds || [];
                      updateDraft('publicationIds', selected ? ids.filter((id: string) => id !== String(pub.id)) : [...ids, String(pub.id)]);
                    }} style={{ padding: '6px 14px', borderRadius: 20, fontSize: 13, cursor: 'pointer', fontWeight: 600, border: selected ? `1px solid ${SHOPIFY_GREEN}` : '1px solid var(--border)', background: selected ? `${SHOPIFY_GREEN}18` : 'transparent', color: selected ? SHOPIFY_GREEN : 'var(--text-muted)' }}>
                      {pub.name}
                    </button>
                  );
                })}
              </div>
            : <div style={{ fontSize: 13, color: 'var(--text-muted)', marginTop: 4 }}>{storeDataLoading ? '⏳ Loading…' : 'No sales channels found — product will use store default'}</div>
          }
        </div>
        <div>
          <label style={labelStyle}>Collections <span style={{ fontWeight: 400, textTransform: 'none', color: 'var(--text-muted)' }}>(add to manual or smart collections)</span></label>
          {collections.length > 0
            ? <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8, marginTop: 4 }}>
                {collections.map((col: any) => {
                  const isSmart = col.type === 'smart';
                  // Smart collections: prefix ID so backend knows to skip /collects.json
                  const colId = isSmart ? `smart:${col.id}` : String(col.id);
                  const selected = (draft.collectionIds || []).includes(colId);
                  return (
                    <button key={colId}
                      onClick={() => {
                        if (isSmart) return; // smart collections are read-only
                        const ids: string[] = (draft as any).collectionIds || [];
                        updateDraft('collectionIds' as any, selected ? ids.filter((id: string) => id !== colId) : [...ids, colId]);
                      }}
                      title={isSmart ? 'Smart collection — membership is automatic based on rules, cannot be manually assigned' : col.title}
                      style={{ padding: '6px 14px', borderRadius: 20, fontSize: 13,
                        cursor: isSmart ? 'not-allowed' : 'pointer', fontWeight: 600,
                        border: isSmart ? '1px dashed var(--border)' : selected ? '1px solid #8b5cf6' : '1px solid var(--border)',
                        background: isSmart ? 'transparent' : selected ? '#8b5cf618' : 'transparent',
                        color: isSmart ? 'var(--text-muted)' : selected ? '#8b5cf6' : 'var(--text-muted)',
                        opacity: isSmart ? 0.5 : 1 }}>
                      {col.title}
                      <span style={{ fontSize: 10, marginLeft: 4, opacity: 0.6 }}>{isSmart ? '⚡ auto' : 'manual'}</span>
                    </button>
                  );
                })}
              </div>
            : <div style={{ fontSize: 13, color: 'var(--text-muted)', marginTop: 4 }}>{storeDataLoading ? '⏳ Loading…' : 'No collections found in your store'}</div>
          }
        </div>
      </Section>

      {/* ── Images ── */}
      <Section title={`Images (${draft.images.length}/250)`} subtitle="First image = gallery image · Double-click to set as primary" accent="#8b5cf6">
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8, marginBottom: 8 }}>
          {draft.images.map((img, idx) => (
            <div key={idx} style={{ display: 'flex', flexDirection: 'column', gap: 4, width: 110 }}>
              <div onDoubleClick={() => { if (idx > 0) { const imgs = [...draft.images]; const alts = [...(draft.imageAlts || draft.images.map(() => ''))]; [imgs[0], imgs[idx]] = [imgs[idx], imgs[0]]; [alts[0], alts[idx]] = [alts[idx], alts[0]]; updateDraft('images', imgs); updateDraft('imageAlts', alts); } }}
                title={idx === 0 ? 'Primary image' : 'Double-click to set as primary'}
                style={{ position: 'relative', width: 110, height: 80, borderRadius: 6, border: idx === 0 ? `2px solid ${SHOPIFY_GREEN}` : '1px solid var(--border)', overflow: 'hidden', cursor: idx > 0 ? 'pointer' : 'default' }}>
                <img src={img} alt={(draft.imageAlts || [])[idx] || ''} style={{ width: '100%', height: '100%', objectFit: 'cover' }} onError={e => { (e.target as HTMLImageElement).alt = '⚠️'; }} />
                <button onClick={() => removeImage(idx)} style={{ position: 'absolute', top: 2, right: 2, background: 'rgba(239,68,68,0.9)', border: 'none', borderRadius: 3, color: '#fff', cursor: 'pointer', fontSize: 11, padding: '1px 4px' }}>✕</button>
                {idx === 0 && <div style={{ position: 'absolute', bottom: 0, left: 0, right: 0, background: SHOPIFY_GREEN, color: '#fff', fontSize: 9, textAlign: 'center', padding: '1px 0' }}>MAIN</div>}
              </div>
              <input value={(draft.imageAlts || [])[idx] || ''} onChange={e => { const alts = [...(draft.imageAlts || draft.images.map(() => ''))]; alts[idx] = e.target.value; updateDraft('imageAlts', alts); }} placeholder="Alt text" style={{ fontSize: 10, padding: '3px 5px', borderRadius: 4, border: '1px solid var(--border)', background: 'var(--bg-primary)', color: 'var(--text-primary)', width: '100%' }} />
            </div>
          ))}
          {draft.images.length < 250 && <button onClick={addImageUrl} style={{ width: 80, height: 80, borderRadius: 6, border: '2px dashed var(--border)', background: 'transparent', color: 'var(--text-muted)', cursor: 'pointer', fontSize: 24, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>+</button>}
        </div>
      </Section>

      {/* ── SEO ── */}
      <Section title="SEO" accent="#06b6d4">
        <div>
          <label style={labelStyle}>Page Title <span style={{ fontWeight: 400, textTransform: 'none', color: 'var(--text-muted)' }}>(defaults to product title)</span></label>
          <input value={draft.seoTitle} onChange={e => updateDraft('seoTitle', e.target.value)} style={inputStyle} placeholder={draft.title} maxLength={70} />
          <div style={{ fontSize: 11, color: draft.seoTitle.length > 60 ? '#fbbf24' : 'var(--text-muted)', marginTop: 3 }}>{draft.seoTitle.length}/70 chars</div>
        </div>
        <div>
          <label style={labelStyle}>Meta Description</label>
          <textarea value={draft.seoDescription} onChange={e => updateDraft('seoDescription', e.target.value)} style={{ ...textareaStyle, minHeight: 80 }} placeholder="Brief description for search engines…" maxLength={320} />
          <div style={{ fontSize: 11, color: draft.seoDescription.length > 160 ? '#fbbf24' : 'var(--text-muted)', marginTop: 3 }}>{draft.seoDescription.length}/320 chars</div>
        </div>
      </Section>

      {/* ── Metafields ── */}
      <Section title={`Metafields (${(draft.metafields || []).length})`} subtitle="Mapped from store metafield definitions" accent="#f59e0b">
        {(draft.metafields || []).map((mf, idx) => (
          <div key={idx} style={{ display: 'flex', gap: 8, alignItems: 'flex-start', marginBottom: 8, flexWrap: 'wrap' }}>
            <input placeholder="namespace" value={mf.namespace} onChange={e => { const mfs = [...(draft.metafields || [])]; mfs[idx] = { ...mfs[idx], namespace: e.target.value }; updateDraft('metafields', mfs); }} style={{ flex: '1 1 110px', padding: '6px 8px', borderRadius: 6, border: '1px solid var(--border)', background: 'var(--bg-primary)', color: 'var(--text-primary)', fontSize: 13 }} />
            <input placeholder="key" value={mf.key} onChange={e => { const mfs = [...(draft.metafields || [])]; mfs[idx] = { ...mfs[idx], key: e.target.value }; updateDraft('metafields', mfs); }} style={{ flex: '1 1 110px', padding: '6px 8px', borderRadius: 6, border: '1px solid var(--border)', background: 'var(--bg-primary)', color: 'var(--text-primary)', fontSize: 13 }} />
            <select value={mf.type} onChange={e => { const mfs = [...(draft.metafields || [])]; mfs[idx] = { ...mfs[idx], type: e.target.value }; updateDraft('metafields', mfs); }} style={{ flex: '0 0 160px', padding: '6px 8px', borderRadius: 6, border: '1px solid var(--border)', background: 'var(--bg-primary)', color: 'var(--text-primary)', fontSize: 13 }}>
              <option value="single_line_text_field">Text (single)</option><option value="multi_line_text_field">Text (multi)</option>
              <option value="number_integer">Number (int)</option><option value="number_decimal">Number (decimal)</option>
              <option value="json">JSON</option><option value="boolean">Boolean</option>
              <option value="url">URL</option><option value="color">Color</option>
              <option value="date">Date</option><option value="date_time">Date & Time</option>
            </select>
            <input placeholder="value" value={mf.value} onChange={e => { const mfs = [...(draft.metafields || [])]; mfs[idx] = { ...mfs[idx], value: e.target.value }; updateDraft('metafields', mfs); }} style={{ flex: '2 1 160px', padding: '6px 8px', borderRadius: 6, border: '1px solid var(--border)', background: 'var(--bg-primary)', color: 'var(--text-primary)', fontSize: 13 }} />
            <button onClick={() => updateDraft('metafields', (draft.metafields || []).filter((_: any, i: number) => i !== idx))} style={{ padding: '6px 10px', borderRadius: 6, border: '1px solid var(--danger)', background: 'transparent', color: 'var(--danger)', cursor: 'pointer', fontSize: 12, flexShrink: 0 }}>✕</button>
          </div>
        ))}
        <button onClick={() => updateDraft('metafields', [...(draft.metafields || []), { namespace: 'custom', key: '', value: '', type: 'single_line_text_field' }])} style={{ marginTop: 4, padding: '6px 14px', borderRadius: 6, border: '1px dashed var(--border)', background: 'transparent', color: 'var(--text-muted)', cursor: 'pointer', fontSize: 13 }}>+ Add Metafield</button>
        <p style={{ marginTop: 4, fontSize: 11, color: 'var(--text-muted)' }}>Metafield mapping is configured in Shopify Settings → Metafield Mappings.</p>
      </Section>

      {/* ── Publish Settings ── */}
      <Section title="Publish Settings" accent="#6366f1">
        <label style={{ display: 'flex', alignItems: 'center', gap: 10, cursor: 'pointer', fontSize: 14 }}>
          <input type="checkbox" id="publish-toggle" checked={publishOnSubmit} onChange={e => setPublishOnSubmit(e.target.checked)} style={{ width: 16, height: 16 }} />
          Publish immediately (status: <strong>active</strong>)
        </label>
        {!publishOnSubmit && <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>Product will be saved as a <strong>draft</strong>.</div>}
      </Section>

      {/* ── Variants ── */}
      {isVariantProduct && (
        <div style={{ marginBottom: 16, padding: 20, background: 'var(--bg-secondary)', borderRadius: 12, border: '1px solid var(--border)' }}>
          <h3 style={{ margin: '0 0 4px', fontSize: 15, fontWeight: 700, color: '#d946ef' }}>Variants ({activeVariants.length})</h3>

          {/* >3 options error */}
          {tooManyOptions && (
            <div style={{ padding: '12px 16px', background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 8, marginBottom: 12 }}>
              <strong style={{ color: '#f87171' }}>⚠ Too many variation options</strong>
              <p style={{ fontSize: 13, color: 'var(--text-secondary)', margin: '6px 0 0' }}>
                Shopify supports a maximum of <strong>3 option types</strong> per product (e.g. Size, Colour, Material).
                This product has <strong>{optionKeys.length} options</strong> ({optionKeys.join(', ')}).
                Please reduce the options in your PIM before listing on Shopify, or create separate listings per option group.
              </p>
            </div>
          )}

          {/* >100 variants warning + split */}
          {tooManyVariants && !tooManyOptions && (
            <div style={{ padding: '12px 16px', background: 'rgba(251,191,36,0.1)', border: '1px solid rgba(251,191,36,0.3)', borderRadius: 8, marginBottom: 12 }}>
              <strong style={{ color: '#fbbf24' }}>⚠ Too many variants ({activeVariants.length})</strong>
              <p style={{ fontSize: 13, color: 'var(--text-secondary)', margin: '6px 0 8px' }}>
                Shopify limits products to <strong>100 variants</strong>. Your product has {activeVariants.length} active variants.
                You can split these into {variantChunks.length} separate Shopify listings, each with up to 100 variants.
              </p>
              <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
                {variantChunks.map((chunk, i) => (
                  <button key={i} onClick={() => { setVariantSplitMode(true); setVariantSplitChunk(i); handleSubmit(chunk); }}
                    disabled={submitting}
                    style={{ padding: '6px 14px', background: '#fbbf2422', border: '1px solid #fbbf24', borderRadius: 6, color: '#fbbf24', cursor: 'pointer', fontSize: 13, fontWeight: 600 }}>
                    Submit Part {i + 1} ({chunk.length} variants: {Object.values(chunk[0].combination).join('/')} → {Object.values(chunk[chunk.length-1].combination).join('/')})
                  </button>
                ))}
              </div>
            </div>
          )}

          {/* Variant cards (when within limits) */}
          {!tooManyOptions && !tooManyVariants && (
            <>
              <p style={{ fontSize: 12, color: 'var(--text-muted)', margin: '0 0 12px' }}>
                {optionKeys.length} option{optionKeys.length !== 1 ? 's' : ''}: <strong>{optionKeys.join(', ')}</strong> · {activeVariants.length} active variants
              </p>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                {variants.map(v => (
                  <VariantCard
                    key={v.id}
                    variant={v}
                    optionKeys={optionKeys}
                    onChange={(field, value) => updateVariant(v.id, field, value)}
                  />
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
              <div style={{ fontWeight: 700, color: '#4ade80', marginBottom: 8, fontSize: 15 }}>✅ {draft.isUpdate ? 'Listing updated' : 'Listing created'} successfully!</div>
              {submitResult.url && <div style={{ fontSize: 13, marginBottom: 4 }}><a href={submitResult.url} target="_blank" rel="noopener noreferrer" style={{ color: SHOPIFY_GREEN }}>🔗 View on Shopify ↗</a></div>}
              {(submitResult.priceRulesCreated ?? 0) > 0 && <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>🏷️ {submitResult.priceRulesCreated} pricing tier{submitResult.priceRulesCreated > 1 ? 's' : ''} created</div>}
              {submitResult.warnings?.length > 0 && <div style={{ marginTop: 8, fontSize: 12, color: '#fbbf24' }}><strong>⚠️ Warnings:</strong><ul style={{ margin: '4px 0 0', paddingLeft: 20 }}>{submitResult.warnings.map((w: string, i: number) => <li key={i}>{w}</li>)}</ul></div>}
              <div style={{ display: 'flex', gap: 10, marginTop: 12 }}>
                <button onClick={() => navigate('/marketplace/listings')} style={{ padding: '8px 18px', background: SHOPIFY_GREEN, border: 'none', borderRadius: 6, color: '#fff', cursor: 'pointer', fontWeight: 600, fontSize: 13 }}>View All Listings</button>
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
            style={{ padding: '10px 28px', background: (submitting || tooManyOptions) ? 'var(--bg-elevated)' : SHOPIFY_GREEN, border: 'none', borderRadius: 7, color: (submitting || tooManyOptions) ? 'var(--text-muted)' : '#fff', fontWeight: 700, fontSize: 14, cursor: (submitting || tooManyOptions) ? 'not-allowed' : 'pointer' }}>
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

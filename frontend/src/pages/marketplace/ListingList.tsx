// ============================================================================
// LISTING LIST PAGE — SESSION A: Inventory Grid UX
// ============================================================================
// Location: frontend/src/pages/marketplace/ListingList.tsx
//
// Session A changes:
//   GRD-01  Per-channel status badges column (grouped by product_id)
//   GRD-02  Variation parent expand / collapse (variant_id grouping)
//   GRD-03  Right-click context menu (View, Edit, Publish, Duplicate, Delete)
//   GRD-04  Column chooser (gear icon, localStorage persistence)
//   GRD-05  Resizable columns (drag + double-click autosize)

import { useState, useEffect, useCallback, useRef, useMemo } from 'react';
import { useNavigate } from 'react-router-dom';
import { listingService } from '../../services/marketplace-api';
import BulkReviseDialog from './BulkReviseDialog';

// ─── Constants ────────────────────────────────────────────────────────────────

const adapterEmoji: Record<string, string> = {
  amazon: '📦', temu: '🛍️', ebay: '🏷️', shopify: '🛒', tesco: '🏪',
  backmarket: '♻️', fruugo: '🌍', walmart: '🏬', etsy: '🎨',
};

const stateColors: Record<string, { bg: string; fg: string }> = {
  published: { bg: 'var(--success-glow)', fg: 'var(--success)' },
  ready:     { bg: 'var(--info-glow)',    fg: 'var(--info)' },
  imported:  { bg: 'var(--warning-glow)',fg: 'var(--warning)' },
  draft:     { bg: 'var(--bg-tertiary)', fg: 'var(--text-secondary)' },
  error:     { bg: 'var(--danger-glow)', fg: 'var(--danger)' },
  blocked:   { bg: 'var(--danger-glow)', fg: 'var(--danger)' },
  paused:    { bg: 'var(--warning-glow)',fg: 'var(--warning)' },
};

const PAGE_SIZE = 50;

// ─── Column definitions ───────────────────────────────────────────────────────

type ColKey = 'product' | 'sku' | 'channels' | 'state' | 'price' | 'qty' | 'updated' | 'created' | 'product_id';

interface ColDef {
  key: ColKey;
  label: string;
  defaultVisible: boolean;
  alwaysVisible: boolean;
  defaultWidth: number;
  align?: 'left' | 'right';
}

const COLUMN_DEFS: ColDef[] = [
  { key: 'product',    label: 'Product',     defaultVisible: true,  alwaysVisible: true,  defaultWidth: 280, align: 'left' },
  { key: 'sku',        label: 'SKU',         defaultVisible: true,  alwaysVisible: true,  defaultWidth: 140, align: 'left' },
  { key: 'channels',   label: 'Channels',    defaultVisible: true,  alwaysVisible: true,  defaultWidth: 220, align: 'left' },
  { key: 'state',      label: 'State',       defaultVisible: true,  alwaysVisible: true,  defaultWidth: 110, align: 'left' },
  { key: 'price',      label: 'Price',       defaultVisible: true,  alwaysVisible: false, defaultWidth: 90,  align: 'right' },
  { key: 'qty',        label: 'Qty',         defaultVisible: true,  alwaysVisible: false, defaultWidth: 70,  align: 'right' },
  { key: 'updated',    label: 'Last Updated',defaultVisible: false, alwaysVisible: false, defaultWidth: 140, align: 'left' },
  { key: 'created',    label: 'Created',     defaultVisible: false, alwaysVisible: false, defaultWidth: 140, align: 'left' },
  { key: 'product_id', label: 'Product ID',  defaultVisible: false, alwaysVisible: false, defaultWidth: 180, align: 'left' },
];

const COL_PREFS_KEY = 'mm_listing_col_visibility';
const COL_WIDTHS_KEY = 'mm_listing_col_widths';
const MIN_COL_WIDTH = 80;

function loadColVisibility(): Record<ColKey, boolean> {
  try {
    const raw = localStorage.getItem(COL_PREFS_KEY);
    if (raw) {
      const stored = JSON.parse(raw) as Record<ColKey, boolean>;
      // Always-visible cols are always true
      const result = { ...stored };
      COLUMN_DEFS.filter(c => c.alwaysVisible).forEach(c => { result[c.key] = true; });
      return result;
    }
  } catch { /* ignore */ }
  const defaults: Partial<Record<ColKey, boolean>> = {};
  COLUMN_DEFS.forEach(c => { defaults[c.key] = c.defaultVisible; });
  return defaults as Record<ColKey, boolean>;
}

function loadColWidths(): Record<ColKey, number> {
  try {
    const raw = localStorage.getItem(COL_WIDTHS_KEY);
    if (raw) return JSON.parse(raw);
  } catch { /* ignore */ }
  const defaults: Partial<Record<ColKey, number>> = {};
  COLUMN_DEFS.forEach(c => { defaults[c.key] = c.defaultWidth; });
  return defaults as Record<ColKey, number>;
}

// ─── Types ────────────────────────────────────────────────────────────────────

interface Listing {
  listing_id: string;
  product_id: string;
  variant_id?: string;
  channel: string;
  state: string;
  product_title?: string;
  product_sku?: string;
  product_brand?: string;
  product_price?: number;
  product_qty?: number;
  product_image?: string;
  overrides?: { title?: string; price?: number };
  channel_identifiers?: Record<string, string>;
  updated_at?: string;
  created_at?: string;
}

interface ProductGroup {
  product_id: string;
  title: string;
  sku: string;
  image?: string;
  brand?: string;
  /** All listings for this product (across channels & variants) */
  listings: Listing[];
  /** True if this product has multiple variants */
  hasVariants: boolean;
}

interface ContextMenu {
  x: number;
  y: number;
  listing: Listing;
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

function groupByProduct(listings: Listing[]): ProductGroup[] {
  const map = new Map<string, ProductGroup>();
  for (const l of listings) {
    if (!map.has(l.product_id)) {
      map.set(l.product_id, {
        product_id: l.product_id,
        title: l.product_title || l.overrides?.title || '(Untitled)',
        sku: l.product_sku || '—',
        image: l.product_image,
        brand: l.product_brand,
        listings: [],
        hasVariants: false,
      });
    }
    const g = map.get(l.product_id)!;
    g.listings.push(l);
    if (l.variant_id) g.hasVariants = true;
  }
  return Array.from(map.values());
}

function formatDate(iso?: string) {
  if (!iso) return '—';
  try {
    return new Date(iso).toLocaleDateString('en-GB', { day: '2-digit', month: 'short', year: 'numeric' });
  } catch { return '—'; }
}

// ─── Component ────────────────────────────────────────────────────────────────

export default function ListingList() {
  const navigate = useNavigate();

  // Data
  const [listings, setListings] = useState<Listing[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState('');

  // Pagination
  const [offset, setOffset] = useState(0);
  const [total, setTotal] = useState(0);
  const [hasMore, setHasMore] = useState(false);

  // Filters
  const [stateFilter, setStateFilter] = useState('all');
  const [channelFilter, setChannelFilter] = useState('all');
  const [search, setSearch] = useState('');
  const [viewMode, setViewMode] = useState<'listings' | 'unlisted'>('listings');
  const [unlistedProducts, setUnlistedProducts] = useState<any[]>([]);

  // Selection
  const [selected, setSelected] = useState(new Set<string>());
  const [bulkMenuOpen, setBulkMenuOpen] = useState(false);
  const [bulkActionLoading, setBulkActionLoading] = useState('');
  const [bulkReviseOpen, setBulkReviseOpen] = useState(false);

  // GRD-02: expanded parent products
  const [expandedProducts, setExpandedProducts] = useState(new Set<string>());

  // GRD-03: context menu
  const [contextMenu, setContextMenu] = useState<ContextMenu | null>(null);

  // GRD-04: column chooser
  const [colVisibility, setColVisibility] = useState<Record<ColKey, boolean>>(loadColVisibility);
  const [chooserOpen, setChooserOpen] = useState(false);
  const chooserRef = useRef<HTMLDivElement>(null);

  // GRD-05: column widths + resize state
  const [colWidths, setColWidths] = useState<Record<ColKey, number>>(loadColWidths);
  const resizeRef = useRef<{ col: ColKey; startX: number; startW: number } | null>(null);
  const tableRef = useRef<HTMLTableElement>(null);

  // ─── Data loading ──────────────────────────────────────────────────────────

  const loadListings = useCallback(async (newOffset: number) => {
    if (newOffset === 0) setLoading(true); else setLoadingMore(true);
    setError('');
    try {
      const params: any = { limit: PAGE_SIZE, offset: newOffset };
      if (channelFilter !== 'all') params.channel = channelFilter;
      const res = await listingService.list(params);
      const data: Listing[] = res.data?.data || [];
      const serverTotal = res.data?.total || 0;
      setListings(data);
      setTotal(serverTotal);
      setOffset(newOffset);
      setHasMore(newOffset + data.length < serverTotal);
    } catch (err: any) {
      setError(err.message || 'Failed to load listings');
    } finally {
      setLoading(false);
      setLoadingMore(false);
    }
  }, [channelFilter]);

  useEffect(() => { if (viewMode === 'listings') loadListings(0); }, [viewMode, channelFilter, loadListings]);
  useEffect(() => { if (viewMode === 'unlisted' && channelFilter !== 'all') loadUnlisted(); }, [viewMode, channelFilter]);

  async function loadUnlisted() {
    if (channelFilter === 'all') return;
    setLoading(true);
    try {
      const res = await listingService.listUnlisted(channelFilter);
      setUnlistedProducts(res.data?.data || []);
    } catch { /* ignore */ }
    finally { setLoading(false); }
  }

  // ─── Filtering & grouping ──────────────────────────────────────────────────

  const filtered = useMemo(() => listings.filter(l => {
    if (stateFilter !== 'all' && l.state !== stateFilter) return false;
    if (search) {
      const q = search.toLowerCase();
      const title = l.product_title || l.overrides?.title || '';
      const sku = l.product_sku || '';
      if (
        !title.toLowerCase().includes(q) &&
        !sku.toLowerCase().includes(q) &&
        !l.product_id.toLowerCase().includes(q)
      ) return false;
    }
    return true;
  }), [listings, stateFilter, search]);

  // GRD-02: group listings by product_id
  const productGroups = useMemo(() => groupByProduct(filtered), [filtered]);

  const filteredUnlisted = useMemo(() => unlistedProducts.filter(p => {
    if (!search) return true;
    const q = search.toLowerCase();
    return (p.title || '').toLowerCase().includes(q) || p.product_id.toLowerCase().includes(q);
  }), [unlistedProducts, search]);

  // ─── Selection helpers ─────────────────────────────────────────────────────

  function toggleSelect(id: string) {
    setSelected(prev => { const n = new Set(prev); n.has(id) ? n.delete(id) : n.add(id); return n; });
  }
  function toggleSelectAll() {
    setSelected(prev =>
      prev.size === filtered.length
        ? new Set()
        : new Set(filtered.map(l => l.listing_id))
    );
  }

  // ─── Single-row actions ────────────────────────────────────────────────────

  async function handlePublishSingle(id: string) {
    try { await listingService.publish(id); loadListings(offset); }
    catch (e: any) { alert(e.response?.data?.error || 'Publish failed'); }
  }

  async function handleDelete(id: string) {
    if (!confirm('Delete this listing?')) return;
    try {
      await listingService.delete(id);
      setListings(prev => prev.filter(l => l.listing_id !== id));
    } catch { alert('Delete failed'); }
  }

  async function handleDuplicate(listing: Listing) {
    // Navigate to create form pre-filled with the same product_id.
    // The create form reads product_id from URL params and pre-populates data.
    navigate(`/marketplace/listings/create?product_id=${listing.product_id}&channel=${listing.channel}&duplicate_from=${listing.listing_id}`);
  }

  // ─── Bulk actions ──────────────────────────────────────────────────────────

  useEffect(() => {
    if (!bulkMenuOpen) return;
    const close = () => setBulkMenuOpen(false);
    setTimeout(() => document.addEventListener('click', close), 0);
    return () => document.removeEventListener('click', close);
  }, [bulkMenuOpen]);

  async function handleBulkAction(action: string) {
    if (selected.size === 0) return;
    const ids = Array.from(selected);
    setBulkMenuOpen(false);
    setBulkActionLoading(action);
    try {
      switch (action) {
        case 'publish':
          await listingService.bulkPublish(ids);
          setSelected(new Set());
          loadListings(0);
          break;
        case 'enrich': {
          const enrichRes = await listingService.bulkEnrich(ids);
          alert(`✨ Queued ${enrichRes.data?.queued || 0} products for enrichment. This will take a few minutes.`);
          setSelected(new Set());
          break;
        }
        case 'revise':
          setBulkReviseOpen(true);
          break;
        case 'delete':
          if (!confirm(`Delete ${ids.length} listings? This cannot be undone.`)) break;
          await listingService.bulkDelete(ids);
          setSelected(new Set());
          loadListings(0);
          break;
        case 'enrich_all':
          if (!confirm('Enrich ALL unenriched products? This may take a while for large catalogs.')) break;
          const allRes = await listingService.enrichAll();
          alert(`✨ Queued ${allRes.data?.queued || 0} products for enrichment.`);
          break;
      }
    } catch (e: any) {
      alert(e.response?.data?.error || `${action} failed`);
    } finally {
      setBulkActionLoading('');
    }
  }

  // ─── GRD-02: expand / collapse product groups ──────────────────────────────

  function toggleExpand(productId: string) {
    setExpandedProducts(prev => {
      const n = new Set(prev);
      n.has(productId) ? n.delete(productId) : n.add(productId);
      return n;
    });
  }

  // ─── GRD-03: context menu ─────────────────────────────────────────────────

  function handleContextMenu(e: React.MouseEvent, listing: Listing) {
    e.preventDefault();
    setContextMenu({ x: e.clientX, y: e.clientY, listing });
  }

  useEffect(() => {
    if (!contextMenu) return;
    function close() { setContextMenu(null); }
    function handleKey(e: KeyboardEvent) { if (e.key === 'Escape') close(); }
    setTimeout(() => document.addEventListener('click', close), 0);
    document.addEventListener('keydown', handleKey);
    return () => {
      document.removeEventListener('click', close);
      document.removeEventListener('keydown', handleKey);
    };
  }, [contextMenu]);

  // ─── GRD-04: column chooser ────────────────────────────────────────────────

  useEffect(() => {
    localStorage.setItem(COL_PREFS_KEY, JSON.stringify(colVisibility));
  }, [colVisibility]);

  function toggleCol(key: ColKey) {
    const def = COLUMN_DEFS.find(c => c.key === key)!;
    if (def.alwaysVisible) return;
    setColVisibility(prev => ({ ...prev, [key]: !prev[key] }));
  }

  useEffect(() => {
    if (!chooserOpen) return;
    function close(e: MouseEvent) {
      if (chooserRef.current && !chooserRef.current.contains(e.target as Node)) {
        setChooserOpen(false);
      }
    }
    setTimeout(() => document.addEventListener('click', close), 0);
    return () => document.removeEventListener('click', close);
  }, [chooserOpen]);

  const visibleCols = COLUMN_DEFS.filter(c => colVisibility[c.key]);

  // ─── GRD-05: resize columns ────────────────────────────────────────────────

  function handleResizeMouseDown(e: React.MouseEvent, col: ColKey) {
    e.preventDefault();
    e.stopPropagation();
    resizeRef.current = { col, startX: e.clientX, startW: colWidths[col] };

    function onMouseMove(ev: MouseEvent) {
      if (!resizeRef.current) return;
      const delta = ev.clientX - resizeRef.current.startX;
      const newW = Math.max(MIN_COL_WIDTH, resizeRef.current.startW + delta);
      setColWidths(prev => {
        const updated = { ...prev, [resizeRef.current!.col]: newW };
        localStorage.setItem(COL_WIDTHS_KEY, JSON.stringify(updated));
        return updated;
      });
    }
    function onMouseUp() {
      resizeRef.current = null;
      window.removeEventListener('mousemove', onMouseMove);
      window.removeEventListener('mouseup', onMouseUp);
    }
    window.addEventListener('mousemove', onMouseMove);
    window.addEventListener('mouseup', onMouseUp);
  }

  function autosizeCol(col: ColKey) {
    if (!tableRef.current) return;
    const colIdx = visibleCols.findIndex(c => c.key === col);
    if (colIdx === -1) return;
    // +1 because first col is checkbox col at index 0
    const cells = tableRef.current.querySelectorAll(`tr td:nth-child(${colIdx + 2}), tr th:nth-child(${colIdx + 2})`);
    let max = MIN_COL_WIDTH;
    cells.forEach(cell => {
      const w = (cell as HTMLElement).scrollWidth;
      if (w > max) max = w;
    });
    setColWidths(prev => {
      const updated = { ...prev, [col]: max + 16 };
      localStorage.setItem(COL_WIDTHS_KEY, JSON.stringify(updated));
      return updated;
    });
  }

  function autosizeAll() {
    visibleCols.forEach(c => autosizeCol(c.key));
  }

  // ─── Render helpers ────────────────────────────────────────────────────────

  /** GRD-01: per-channel status badges */
  function renderChannelBadges(group: ProductGroup) {
    // Collect unique channel+state combos
    const channelMap = new Map<string, { state: string; listingId: string }>();
    for (const l of group.listings) {
      // Show worst state per channel (error > paused > others)
      const existing = channelMap.get(l.channel);
      if (!existing || statePriority(l.state) > statePriority(existing.state)) {
        channelMap.set(l.channel, { state: l.state, listingId: l.listing_id });
      }
    }
    return (
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }}>
        {Array.from(channelMap.entries()).map(([ch, { state, listingId }]) => {
          const sc = stateColors[state] || stateColors.draft;
          return (
            <span
              key={ch}
              title={`${ch}: ${state}`}
              onClick={(e) => { e.stopPropagation(); navigate(`/marketplace/listings/${listingId}`); }}
              style={{
                display: 'inline-flex', alignItems: 'center', gap: 3,
                padding: '2px 7px', borderRadius: 12, fontSize: 11, fontWeight: 600,
                background: sc.bg, color: sc.fg, border: `1px solid ${sc.fg}40`,
                cursor: 'pointer', userSelect: 'none',
                transition: 'opacity 0.15s',
              }}
              onMouseEnter={e => (e.currentTarget.style.opacity = '0.75')}
              onMouseLeave={e => (e.currentTarget.style.opacity = '1')}
            >
              {adapterEmoji[ch] || '🌐'} {ch}
            </span>
          );
        })}
      </div>
    );
  }

  function statePriority(state: string) {
    return { error: 4, blocked: 3, paused: 2, published: 1 }[state] ?? 0;
  }

  /** Representative listing for a group (used for state/price/qty columns) */
  function representativeListing(group: ProductGroup): Listing {
    // Prefer published, else first
    return group.listings.find(l => l.state === 'published') ?? group.listings[0];
  }

  function renderCell(col: ColKey, group: ProductGroup, variant?: Listing): React.ReactNode {
    const rep = variant ?? representativeListing(group);
    const sc = stateColors[rep.state] || stateColors.draft;
    switch (col) {
      case 'product':
        return (
          <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            {variant ? (
              <div style={{ width: 36, height: 36, borderRadius: 6, background: 'var(--bg-tertiary)', display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 12, color: 'var(--text-muted)' }}>
                ↳
              </div>
            ) : group.image ? (
              <img src={group.image} alt="" style={{ width: 36, height: 36, borderRadius: 6, objectFit: 'cover', background: '#fff', flexShrink: 0 }} />
            ) : (
              <div style={{ width: 36, height: 36, borderRadius: 6, background: 'var(--bg-tertiary)', display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 16, flexShrink: 0 }}>📦</div>
            )}
            <div style={{ minWidth: 0 }}>
              <div style={{ fontSize: 13, fontWeight: variant ? 500 : 600, maxWidth: '100%', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', color: variant ? 'var(--text-secondary)' : 'var(--text-primary)' }}>
                {variant ? (rep.product_title || rep.overrides?.title || '(Untitled)') : group.title}
              </div>
              {!variant && group.brand && <div style={{ fontSize: 11, color: 'var(--text-muted)' }}>{group.brand}</div>}
              {variant && <div style={{ fontSize: 11, color: 'var(--text-muted)', fontFamily: 'monospace' }}>{rep.listing_id}</div>}
            </div>
          </div>
        );
      case 'sku':
        return <span style={{ fontSize: 12, color: 'var(--text-secondary)', fontFamily: 'monospace' }}>{variant ? (rep.product_sku || '—') : group.sku}</span>;
      case 'channels':
        return variant ? (
          <span style={{ display: 'inline-flex', alignItems: 'center', gap: 3, padding: '2px 7px', borderRadius: 12, fontSize: 11, fontWeight: 600, background: sc.bg, color: sc.fg, border: `1px solid ${sc.fg}40` }}>
            {adapterEmoji[rep.channel] || '🌐'} {rep.channel}
          </span>
        ) : renderChannelBadges(group);
      case 'state':
        return <span style={{ padding: '2px 8px', borderRadius: 4, fontSize: 11, fontWeight: 700, background: sc.bg, color: sc.fg, border: `1px solid ${sc.fg}30` }}>{rep.state?.toUpperCase()}</span>;
      case 'price':
        return <span style={{ fontSize: 13 }}>{rep.product_price != null ? `£${rep.product_price.toFixed(2)}` : rep.overrides?.price != null ? `£${rep.overrides.price.toFixed(2)}` : '—'}</span>;
      case 'qty':
        return <span style={{ fontSize: 13 }}>{rep.product_qty ?? '—'}</span>;
      case 'updated':
        return <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>{formatDate(rep.updated_at)}</span>;
      case 'created':
        return <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>{formatDate(rep.created_at)}</span>;
      case 'product_id':
        return <span style={{ fontSize: 11, fontFamily: 'monospace', color: 'var(--text-muted)' }}>{group.product_id}</span>;
      default:
        return null;
    }
  }

  // ─── Pagination ────────────────────────────────────────────────────────────

  const currentPage = Math.floor(offset / PAGE_SIZE) + 1;

  // ─── Loading state ─────────────────────────────────────────────────────────

  if (loading && listings.length === 0) return (
    <div className="page"><div className="loading-state"><div className="spinner"></div><p>Loading listings...</p></div></div>
  );

  // ─── Render ────────────────────────────────────────────────────────────────

  return (
    <div className="page">
      {/* Header */}
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 24 }}>
        <div>
          <h1 style={{ fontSize: 24, fontWeight: 700, color: 'var(--text-primary)' }}>Listings</h1>
          <p style={{ color: 'var(--text-muted)', fontSize: 13, marginTop: 4 }}>
            {productGroups.length.toLocaleString()} product{productGroups.length !== 1 ? 's' : ''} · {total.toLocaleString()} listings total
          </p>
        </div>
        <div style={{ display: 'flex', gap: 10, alignItems: 'center' }}>
          <button
            style={{
              padding: '8px 14px', borderRadius: 6, fontSize: 13, fontWeight: 600,
              border: '1px solid var(--border)', background: 'var(--bg-secondary)',
              color: 'var(--text-primary)', cursor: 'pointer', display: 'flex', alignItems: 'center', gap: 6,
            }}
            onClick={() => navigate('/marketplace/configurators')}
          >
            <span>⚙️</span> Configurators
          </button>
          <button className="btn btn-primary" onClick={() => navigate('/marketplace/listings/create')}>+ Create Listing</button>
        </div>
      </div>

      {/* Filters + toolbar */}
      <div className="card" style={{ padding: 16, marginBottom: 20, display: 'flex', gap: 12, alignItems: 'center', flexWrap: 'wrap' }}>
        <input className="input" style={{ flex: 1, minWidth: 200 }}
          placeholder="Search by SKU, title, or product ID..."
          value={search} onChange={e => setSearch(e.target.value)} />
        <select className="select" style={{ width: 150 }} value={channelFilter}
          onChange={e => { setChannelFilter(e.target.value); setOffset(0); }}>
          <option value="all">All Channels</option>
          <option value="amazon">Amazon</option>
          <option value="ebay">eBay</option>
          <option value="temu">Temu</option>
          <option value="shopify">Shopify</option>
          <option value="tesco">Tesco</option>
          <option value="backmarket">Back Market</option>
          <option value="fruugo">Fruugo</option>
        </select>
        <select className="select" style={{ width: 150 }}
          value={viewMode === 'unlisted' ? 'unlisted' : stateFilter}
          onChange={e => {
            const v = e.target.value;
            if (v === 'unlisted') { setViewMode('unlisted'); setStateFilter('all'); }
            else { setViewMode('listings'); setStateFilter(v); }
          }}>
          <option value="all">All States</option>
          <option value="imported">Imported</option>
          <option value="draft">Draft</option>
          <option value="ready">Ready</option>
          <option value="published">Published</option>
          <option value="paused">Paused</option>
          <option value="error">Error</option>
          <option value="unlisted">Unlisted Products</option>
        </select>

        {/* GRD-05: Autosize all */}
        <button className="btn btn-secondary" style={{ fontSize: 12 }} title="Autosize all columns" onClick={autosizeAll}>
          ⇔ Autosize
        </button>

        {/* GRD-04: Column chooser */}
        <div style={{ position: 'relative' }} ref={chooserRef}>
          <button
            className="btn btn-secondary"
            style={{ fontSize: 12, padding: '6px 10px' }}
            title="Choose columns"
            onClick={() => setChooserOpen(o => !o)}
          >
            ⚙ Columns
          </button>
          {chooserOpen && (
            <div style={{
              position: 'absolute', top: '100%', right: 0, marginTop: 4, zIndex: 60,
              background: 'var(--bg-secondary)', border: '1px solid var(--border)',
              borderRadius: 8, boxShadow: '0 8px 24px rgba(0,0,0,0.3)',
              padding: '8px 0', minWidth: 180,
            }}>
              <div style={{ padding: '4px 14px 8px', fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.06em' }}>Columns</div>
              {COLUMN_DEFS.map(col => (
                <label
                  key={col.key}
                  style={{
                    display: 'flex', alignItems: 'center', gap: 8,
                    padding: '6px 14px', cursor: col.alwaysVisible ? 'default' : 'pointer',
                    opacity: col.alwaysVisible ? 0.5 : 1,
                  }}
                >
                  <input
                    type="checkbox"
                    checked={!!colVisibility[col.key]}
                    disabled={col.alwaysVisible}
                    onChange={() => toggleCol(col.key)}
                    style={{ cursor: col.alwaysVisible ? 'default' : 'pointer' }}
                  />
                  <span style={{ fontSize: 13, color: 'var(--text-primary)' }}>{col.label}</span>
                  {col.alwaysVisible && <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>(required)</span>}
                </label>
              ))}
            </div>
          )}
        </div>

        {/* Bulk actions */}
        {selected.size > 0 && (
          <div style={{ position: 'relative' }}>
            <button className="btn btn-primary" style={{ fontSize: 12 }}
              onClick={() => setBulkMenuOpen(!bulkMenuOpen)}
              disabled={!!bulkActionLoading}>
              {bulkActionLoading ? '⏳ Processing...' : `⚡ Bulk Actions (${selected.size})`}
            </button>
            {bulkMenuOpen && (
              <div style={{
                position: 'absolute', top: '100%', right: 0, marginTop: 4, zIndex: 50,
                background: 'var(--bg-secondary)', border: '1px solid var(--border)',
                borderRadius: 8, boxShadow: '0 8px 24px rgba(0,0,0,0.3)', minWidth: 220, overflow: 'hidden',
              }}>
                {[
                  { action: 'enrich', icon: '✨', label: 'Enrich Data' },
                  { action: 'publish', icon: '🚀', label: 'Publish' },
                  { action: 'revise', icon: '📝', label: 'Revise Fields' },
                ].map(({ action, icon, label }) => (
                  <button key={action}
                    style={{ width: '100%', padding: '10px 16px', textAlign: 'left', background: 'none', border: 'none', color: 'var(--text-primary)', cursor: 'pointer', fontSize: 13, display: 'flex', alignItems: 'center', gap: 8 }}
                    onMouseEnter={e => (e.currentTarget.style.background = 'var(--bg-tertiary)')}
                    onMouseLeave={e => (e.currentTarget.style.background = 'none')}
                    onClick={() => handleBulkAction(action)}>
                    <span>{icon}</span> {label}
                  </button>
                ))}
                <div style={{ height: 1, background: 'var(--border)' }} />
                <button
                  style={{ width: '100%', padding: '10px 16px', textAlign: 'left', background: 'none', border: 'none', color: 'var(--danger)', cursor: 'pointer', fontSize: 13, display: 'flex', alignItems: 'center', gap: 8 }}
                  onMouseEnter={e => (e.currentTarget.style.background = 'var(--bg-tertiary)')}
                  onMouseLeave={e => (e.currentTarget.style.background = 'none')}
                  onClick={() => handleBulkAction('delete')}>
                  <span>🗑️</span> Delete Listings
                </button>
              </div>
            )}
          </div>
        )}

        <button className="btn btn-secondary" style={{ fontSize: 12 }}
          onClick={() => handleBulkAction('enrich_all')}
          disabled={!!bulkActionLoading}>
          ✨ Enrich All
        </button>
      </div>

      {error && (
        <div style={{ padding: 12, marginBottom: 16, borderRadius: 8, background: 'var(--danger-glow)', border: '1px solid var(--danger)', color: 'var(--danger)', fontSize: 13 }}>{error}</div>
      )}

      {/* ─── Unlisted products view ─────────────────────────────────────────── */}
      {viewMode === 'unlisted' ? (
        <div className="card" style={{ padding: 0, overflow: 'hidden' }}>
          {channelFilter === 'all' ? (
            <div style={{ padding: 40, textAlign: 'center', color: 'var(--text-muted)' }}>Select a channel to view unlisted products</div>
          ) : filteredUnlisted.length === 0 ? (
            <div style={{ padding: 40, textAlign: 'center', color: 'var(--text-muted)' }}>All products have listings on this channel</div>
          ) : (
            <table style={{ width: '100%', borderCollapse: 'collapse' }}>
              <thead>
                <tr style={{ borderBottom: '1px solid var(--border)' }}>
                  <th style={{ textAlign: 'left', padding: '12px 16px', fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase' }}>Product</th>
                  <th style={{ textAlign: 'right', padding: '12px 16px', fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase' }}>Action</th>
                </tr>
              </thead>
              <tbody>
                {filteredUnlisted.map(product => (
                  <tr key={product.product_id} style={{ borderBottom: '1px solid var(--border)' }}>
                    <td style={{ padding: '10px 16px', fontSize: 14 }}>{product.title || '(Untitled)'}</td>
                    <td style={{ padding: '10px 16px', textAlign: 'right' }}>
                      <button className="btn btn-primary" style={{ fontSize: 12, padding: '4px 12px' }}
                        onClick={() => navigate(`/marketplace/listings/create?product_id=${product.product_id}&channel=${channelFilter}`)}>
                        Create Listing
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>

      ) : (
        /* ─── Main listings grid ────────────────────────────────────────────── */
        <div className="card" style={{ padding: 0, overflow: 'hidden' }}>
          {productGroups.length === 0 && !loading ? (
            <div style={{ padding: 40, textAlign: 'center' }}>
              <div style={{ fontSize: 40, marginBottom: 12 }}>📋</div>
              <h3 style={{ fontSize: 16, fontWeight: 600, marginBottom: 8 }}>No listings found</h3>
              <p style={{ color: 'var(--text-muted)', marginBottom: 16 }}>
                {search ? 'Try a different search term' : 'Import products to create listings'}
              </p>
            </div>
          ) : (
            <>
              <div style={{ overflowX: 'auto' }}>
                <table ref={tableRef} style={{ width: '100%', borderCollapse: 'collapse', tableLayout: 'fixed' }}>
                  <colgroup>
                    {/* Checkbox col */}
                    <col style={{ width: 40 }} />
                    {visibleCols.map(col => (
                      <col key={col.key} style={{ width: colWidths[col.key] }} />
                    ))}
                    {/* Actions col */}
                    <col style={{ width: 100 }} />
                  </colgroup>

                  <thead>
                    <tr style={{ borderBottom: '2px solid var(--border)', background: 'var(--bg-secondary)' }}>
                      {/* Checkbox */}
                      <th style={{ width: 40, padding: '10px 12px' }}>
                        <input type="checkbox"
                          onChange={toggleSelectAll}
                          checked={selected.size > 0 && selected.size === filtered.length}
                          ref={el => { if (el) el.indeterminate = selected.size > 0 && selected.size < filtered.length; }}
                        />
                      </th>

                      {/* Dynamic columns */}
                      {visibleCols.map(col => (
                        <th key={col.key}
                          style={{
                            textAlign: col.align || 'left', padding: '10px 12px',
                            fontSize: 11, fontWeight: 700, color: 'var(--text-muted)',
                            textTransform: 'uppercase', letterSpacing: '0.05em',
                            position: 'relative', userSelect: 'none', whiteSpace: 'nowrap',
                          }}>
                          {col.label}
                          {/* GRD-05: resize handle */}
                          <span
                            style={{
                              position: 'absolute', right: 0, top: 0, bottom: 0, width: 6,
                              cursor: 'col-resize', display: 'flex', alignItems: 'center', justifyContent: 'center',
                            }}
                            onMouseDown={e => handleResizeMouseDown(e, col.key)}
                            onDoubleClick={() => autosizeCol(col.key)}
                            title="Drag to resize · Double-click to autosize"
                          >
                            <span style={{ width: 2, height: '60%', background: 'var(--border)', borderRadius: 1 }} />
                          </span>
                        </th>
                      ))}

                      {/* Actions */}
                      <th style={{ textAlign: 'right', padding: '10px 12px', fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase' }}>Actions</th>
                    </tr>
                  </thead>

                  <tbody>
                    {productGroups.map(group => {
                      const isExpanded = expandedProducts.has(group.product_id);
                      const rep = representativeListing(group);

                      return (
                        <>
                          {/* ── Parent row ── */}
                          <tr
                            key={`parent-${group.product_id}`}
                            style={{ borderBottom: '1px solid var(--border)', cursor: 'pointer', background: 'transparent' }}
                            onClick={() => navigate(`/marketplace/listings/${rep.listing_id}`)}
                            onContextMenu={e => handleContextMenu(e, rep)}
                            onMouseEnter={e => (e.currentTarget.style.background = 'var(--bg-tertiary)')}
                            onMouseLeave={e => (e.currentTarget.style.background = 'transparent')}
                          >
                            {/* Checkbox */}
                            <td style={{ padding: '8px 12px' }} onClick={e => e.stopPropagation()}>
                              <input type="checkbox"
                                checked={group.listings.some(l => selected.has(l.listing_id))}
                                onChange={() => {
                                  const allSelected = group.listings.every(l => selected.has(l.listing_id));
                                  setSelected(prev => {
                                    const n = new Set(prev);
                                    group.listings.forEach(l => allSelected ? n.delete(l.listing_id) : n.add(l.listing_id));
                                    return n;
                                  });
                                }}
                                ref={el => {
                                  if (el) el.indeterminate = group.listings.some(l => selected.has(l.listing_id)) && !group.listings.every(l => selected.has(l.listing_id));
                                }}
                              />
                            </td>

                            {/* Dynamic cells */}
                            {visibleCols.map(col => (
                              <td key={col.key} style={{ padding: '8px 12px', textAlign: col.align || 'left', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                                {col.key === 'product' ? (
                                  <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                                    {/* GRD-02: expand/collapse toggle */}
                                    {group.hasVariants ? (
                                      <button
                                        onClick={e => { e.stopPropagation(); toggleExpand(group.product_id); }}
                                        style={{ background: 'none', border: 'none', cursor: 'pointer', padding: '0 2px', fontSize: 12, color: 'var(--text-muted)', flexShrink: 0 }}
                                        title={isExpanded ? 'Collapse variants' : 'Expand variants'}
                                      >
                                        {isExpanded ? '▼' : '▶'}
                                      </button>
                                    ) : (
                                      <span style={{ width: 18, flexShrink: 0 }} />
                                    )}
                                    <div style={{ flex: 1, minWidth: 0 }}>
                                      {renderCell(col.key, group)}
                                    </div>
                                  </div>
                                ) : (
                                  renderCell(col.key, group)
                                )}
                              </td>
                            ))}

                            {/* Row actions */}
                            <td style={{ padding: '8px 12px', textAlign: 'right' }} onClick={e => e.stopPropagation()}>
                              <div style={{ display: 'flex', gap: 4, justifyContent: 'flex-end' }}>
                                {(rep.state === 'imported' || rep.state === 'ready') && (
                                  <button className="btn-icon" title="Publish" onClick={() => handlePublishSingle(rep.listing_id)}>🚀</button>
                                )}
                                <button className="btn-icon" title="Delete" onClick={() => handleDelete(rep.listing_id)}>🗑️</button>
                              </div>
                            </td>
                          </tr>

                          {/* GRD-02: variant child rows */}
                          {group.hasVariants && isExpanded && group.listings.map(variant => (
                            <tr
                              key={`variant-${variant.listing_id}`}
                              style={{ borderBottom: '1px solid var(--border)', cursor: 'pointer', background: 'var(--bg-secondary)' }}
                              onClick={() => navigate(`/marketplace/listings/${variant.listing_id}`)}
                              onContextMenu={e => handleContextMenu(e, variant)}
                              onMouseEnter={e => (e.currentTarget.style.background = 'var(--bg-tertiary)')}
                              onMouseLeave={e => (e.currentTarget.style.background = 'var(--bg-secondary)')}
                            >
                              {/* Checkbox */}
                              <td style={{ padding: '6px 12px' }} onClick={e => e.stopPropagation()}>
                                <input type="checkbox"
                                  checked={selected.has(variant.listing_id)}
                                  onChange={() => toggleSelect(variant.listing_id)}
                                />
                              </td>

                              {/* Dynamic cells, with indent on first col */}
                              {visibleCols.map((col, i) => (
                                <td key={col.key} style={{ padding: '6px 12px', paddingLeft: i === 0 ? 36 : 12, textAlign: col.align || 'left', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                                  {renderCell(col.key, group, variant)}
                                </td>
                              ))}

                              {/* Row actions */}
                              <td style={{ padding: '6px 12px', textAlign: 'right' }} onClick={e => e.stopPropagation()}>
                                <div style={{ display: 'flex', gap: 4, justifyContent: 'flex-end' }}>
                                  {(variant.state === 'imported' || variant.state === 'ready') && (
                                    <button className="btn-icon" title="Publish" onClick={() => handlePublishSingle(variant.listing_id)}>🚀</button>
                                  )}
                                  <button className="btn-icon" title="Delete" onClick={() => handleDelete(variant.listing_id)}>🗑️</button>
                                </div>
                              </td>
                            </tr>
                          ))}
                        </>
                      );
                    })}
                  </tbody>
                </table>
              </div>

              {/* Pagination */}
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '12px 16px', borderTop: '1px solid var(--border)' }}>
                <div style={{ fontSize: 13, color: 'var(--text-muted)' }}>
                  Showing {offset + 1}–{offset + filtered.length} of {total > offset + PAGE_SIZE ? `${total.toLocaleString()}+` : total.toLocaleString()}
                </div>
                <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                  <button className="btn btn-secondary" style={{ padding: '6px 12px', fontSize: 12 }}
                    disabled={offset === 0 || loadingMore}
                    onClick={() => loadListings(Math.max(0, offset - PAGE_SIZE))}>
                    ← Prev
                  </button>
                  <span style={{ fontSize: 13, color: 'var(--text-secondary)' }}>Page {currentPage}</span>
                  <button className="btn btn-secondary" style={{ padding: '6px 12px', fontSize: 12 }}
                    disabled={!hasMore || loadingMore}
                    onClick={() => loadListings(offset + PAGE_SIZE)}>
                    {loadingMore ? '⏳' : 'Next →'}
                  </button>
                </div>
              </div>
            </>
          )}
        </div>
      )}

      {/* ─── GRD-03: Right-click context menu ─────────────────────────────────── */}
      {contextMenu && (
        <div
          style={{
            position: 'fixed',
            top: Math.min(contextMenu.y, window.innerHeight - 220),
            left: Math.min(contextMenu.x, window.innerWidth - 200),
            zIndex: 9999,
            background: 'var(--bg-secondary)',
            border: '1px solid var(--border)',
            borderRadius: 8,
            boxShadow: '0 8px 32px rgba(0,0,0,0.4)',
            minWidth: 190,
            overflow: 'hidden',
          }}
          onClick={e => e.stopPropagation()}
        >
          {/* Header: show listing channel */}
          <div style={{ padding: '8px 14px 6px', fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.06em', borderBottom: '1px solid var(--border)' }}>
            {adapterEmoji[contextMenu.listing.channel] || '🌐'} {contextMenu.listing.channel} listing
          </div>

          {[
            { label: '👁 View Listing',   action: () => { navigate(`/marketplace/listings/${contextMenu.listing.listing_id}`); setContextMenu(null); } },
            { label: '✏️ Edit Listing',   action: () => { navigate(`/marketplace/listings/${contextMenu.listing.listing_id}/edit`); setContextMenu(null); } },
            { label: '🚀 Publish',        action: () => { handlePublishSingle(contextMenu.listing.listing_id); setContextMenu(null); }, disabled: !['imported','ready'].includes(contextMenu.listing.state) },
            { label: '📋 Duplicate',      action: () => { handleDuplicate(contextMenu.listing); setContextMenu(null); } },
          ].map(item => (
            <button
              key={item.label}
              disabled={item.disabled}
              onClick={item.action}
              style={{
                width: '100%', padding: '9px 14px', textAlign: 'left',
                background: 'none', border: 'none',
                color: item.disabled ? 'var(--text-muted)' : 'var(--text-primary)',
                cursor: item.disabled ? 'default' : 'pointer', fontSize: 13,
                display: 'flex', alignItems: 'center', gap: 8,
                opacity: item.disabled ? 0.45 : 1,
              }}
              onMouseEnter={e => { if (!item.disabled) e.currentTarget.style.background = 'var(--bg-tertiary)'; }}
              onMouseLeave={e => (e.currentTarget.style.background = 'none')}
            >
              {item.label}
            </button>
          ))}

          <div style={{ height: 1, background: 'var(--border)' }} />

          <button
            onClick={() => { handleDelete(contextMenu.listing.listing_id); setContextMenu(null); }}
            style={{
              width: '100%', padding: '9px 14px', textAlign: 'left',
              background: 'none', border: 'none', color: 'var(--danger)',
              cursor: 'pointer', fontSize: 13, display: 'flex', alignItems: 'center', gap: 8,
            }}
            onMouseEnter={e => (e.currentTarget.style.background = 'var(--bg-tertiary)')}
            onMouseLeave={e => (e.currentTarget.style.background = 'none')}
          >
            🗑️ Delete
          </button>
        </div>
      )}

      {/* ─── Bulk Revise Dialog ───────────────────────────────────────────────── */}
      {bulkReviseOpen && (
        <BulkReviseDialog
          listingIds={Array.from(selected)}
          onClose={() => setBulkReviseOpen(false)}
          onComplete={() => { setSelected(new Set()); loadListings(0); }}
        />
      )}
    </div>
  );
}

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
import { getActiveTenantId } from '../../contexts/TenantContext';
import BulkReviseDialog from './BulkReviseDialog';
import { SEOScoreBadge } from '../../components/seo/SEOScoreBadge';
import { BulkOptimiseModal } from '../../components/seo/BulkOptimiseModal';

interface Credential {
  credential_id: string;
  account_name: string;
  channel: string;
  active: boolean;
}

// ─── Constants ────────────────────────────────────────────────────────────────

const adapterEmoji: Record<string, string> = {
  amazon: '📦', temu: '🛍️', ebay: '🏷️', shopify: '🛒', tesco: '🏪',
  backmarket: '♻️', fruugo: '🌍', walmart: '🏬', etsy: '🎨',
};

const stateColors: Record<string, { bg: string; fg: string }> = {
  published: { bg: 'var(--success-glow)', fg: 'var(--success)' },
  ready:     { bg: 'var(--info-glow)',    fg: 'var(--info)' },
  imported:  { bg: 'var(--bg-tertiary)', fg: 'var(--text-secondary)' },
  draft:     { bg: 'var(--bg-tertiary)', fg: 'var(--text-secondary)' },
  error:     { bg: 'var(--danger-glow)', fg: 'var(--danger)' },
  blocked:   { bg: 'var(--danger-glow)', fg: 'var(--danger)' },
  paused:    { bg: 'var(--warning-glow)',fg: 'var(--warning)' },
};

// Maps the raw DB state value to a human-readable display label.
// 'imported' is the DB state for products pulled from a channel that haven't
// been published back — they show as "Unlisted" to the user.
function stateLabel(state: string): string {
  if (state === 'imported') return 'UNLISTED';
  return state?.toUpperCase() ?? '—';
}

const PAGE_SIZE = 200;

// ─── Column definitions ───────────────────────────────────────────────────────

type ColKey = 'product' | 'sku' | 'channels' | 'state' | 'updated' | 'created' | 'product_id' | 'seo_score';

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

  { key: 'seo_score',  label: 'SEO Score',   defaultVisible: true,  alwaysVisible: false, defaultWidth: 110, align: 'left' },
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
  channel_account_id?: string;
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

  // Filters — multi-select sets; empty set = show all
  const [stateFilters, setStateFilters] = useState<Set<string>>(new Set());
  const [credentialFilters, setCredentialFilters] = useState<Set<string>>(new Set());
  const [search, setSearch] = useState('');
  const [viewMode, setViewMode] = useState<'listings' | 'unlisted'>('listings');
  const [channelDropOpen, setChannelDropOpen] = useState(false);
  const [stateDropOpen, setStateDropOpen] = useState(false);
  const channelDropRef = useRef<HTMLDivElement>(null);
  const stateDropRef = useRef<HTMLDivElement>(null);
  const [credentials, setCredentials] = useState<Credential[]>([]);
  const [unlistedProducts, setUnlistedProducts] = useState<any[]>([]);

  // Derive channelFilter from credentialFilters: if exactly one credential is selected,
  // use that credential's channel; otherwise 'all'.
  const channelFilter = useMemo(() => {
    if (credentialFilters.size === 1) {
      const credId = [...credentialFilters][0];
      const cred = credentials.find((c: any) => c.credential_id === credId);
      return cred?.channel || 'all';
    }
    return 'all';
  }, [credentialFilters, credentials]);

  // Selection
  const [selected, setSelected] = useState(new Set<string>());
  const [bulkMenuOpen, setBulkMenuOpen] = useState(false);
  const [bulkActionLoading, setBulkActionLoading] = useState('');
  const [bulkReviseOpen, setBulkReviseOpen] = useState(false);
  const [bulkOptimiseOpen, setBulkOptimiseOpen] = useState(false);

  // GRD-02: expanded parent products
  const [expandedProducts, setExpandedProducts] = useState(new Set<string>());

  // GRD-03: context menu
  const [contextMenu, setContextMenu] = useState<ContextMenu | null>(null);

  // GRD-04: column chooser
  const [colVisibility, setColVisibility] = useState<Record<ColKey, boolean>>(loadColVisibility);

  // GRD-05: column widths + resize state
  const [colWidths, setColWidths] = useState<Record<ColKey, number>>(loadColWidths);

  // SEO scores — keyed by listing_id, fetched once on mount via seo-summary
  const [seoScores, setSeoScores] = useState<Record<string, number | null>>({});
  // SEO score sort: null = unsorted, 'asc' = worst first, 'desc' = best first
  const [seoSort, setSeoSort] = useState<'asc' | 'desc' | null>(null);

  // ─── Data loading ──────────────────────────────────────────────────────────

  const loadListings = useCallback(async (newOffset: number) => {
    if (newOffset === 0) setLoading(true); else setLoadingMore(true);
    setError('');
    try {
      const params: any = { limit: PAGE_SIZE, offset: newOffset };
      // Load all — credential filtering is done client-side for multi-select
      const res = await listingService.list(params);
      const data: Listing[] = res.data?.data || [];
      const serverTotal = res.data?.total || 0;
      if (newOffset === 0) {
        setListings(data);
      } else {
        setListings(prev => [...prev, ...data]);
      }
      setTotal(serverTotal);
      setOffset(newOffset);
      const loadedSoFar = newOffset + data.length;
      setHasMore(loadedSoFar < serverTotal);
      // Automatically fetch remaining pages so all channels appear in the grid
      if (loadedSoFar < serverTotal && newOffset === 0) {
        // Kick off background fetches for the rest — up to 10,000 total listings
        const maxAutoLoad = 10000;
        let nextOffset = loadedSoFar;
        while (nextOffset < Math.min(serverTotal, maxAutoLoad)) {
          const batchParams: any = { limit: PAGE_SIZE, offset: nextOffset };
          // no channel param — fetching all
          const batchRes = await listingService.list(batchParams);
          const batchData: Listing[] = batchRes.data?.data || [];
          if (batchData.length === 0) break;
          setListings(prev => [...prev, ...batchData]);
          nextOffset += batchData.length;
          setOffset(nextOffset);
          setHasMore(nextOffset < serverTotal);
          if (batchData.length < PAGE_SIZE) break;
        }
      }
    } catch (err: any) {
      setError(err.message || 'Failed to load listings');
    } finally {
      setLoading(false);
      setLoadingMore(false);
    }
  }, []);

  useEffect(() => { if (viewMode === 'listings') loadListings(0); }, [viewMode, loadListings]);
  useEffect(() => {
    // Fetch all active credential accounts so we can render one column per account.
    const API_BASE = (import.meta as any).env?.VITE_API_URL || 'http://localhost:8080/api/v1';
    const tenantId = getActiveTenantId();
    fetch(`${API_BASE}/marketplace/credentials`, { headers: { 'X-Tenant-Id': tenantId } })
      .then(r => r.json())
      .then(d => {
        const all: Credential[] = d.credentials || d.data || [];
        setCredentials(all.filter((c: Credential) => c.active !== false));
      })
      .catch(() => {/* non-fatal */});
  }, []);

  // Fetch SEO summary once — single request for all listing scores.
  useEffect(() => {
    const API_BASE = (import.meta as any).env?.VITE_API_URL || 'http://localhost:8080/api/v1';
    const tenantId = getActiveTenantId();
    fetch(`${API_BASE}/listings/seo-summary`, { headers: { 'X-Tenant-Id': tenantId } })
      .then(r => r.ok ? r.json() : null)
      .then(d => {
        if (!d?.listings) return;
        const scores: Record<string, number | null> = {};
        for (const entry of d.listings) {
          scores[entry.listing_id] = entry.seo_score ?? null;
        }
        setSeoScores(scores);
      })
      .catch(() => {/* non-fatal — column shows null badge */});
  }, []);
  // Unlisted view removed — covered by credential columns showing UNLISTED

  useEffect(() => { if (viewMode === 'unlisted') loadUnlisted(); }, [viewMode, channelFilter]);

  async function loadUnlisted() {
    setLoading(true);
    try {
      if (channelFilter === 'all') {
        // No channel selected — fetch all PIM products and show those with no listings
        const API_BASE = (import.meta as any).env?.VITE_API_URL || 'http://localhost:8080/api/v1';
        const tenantId = getActiveTenantId();
        const res = await fetch(`${API_BASE}/products?limit=500`, {
          headers: { 'X-Tenant-Id': tenantId },
        });
        const data = await res.json();
        const allProducts: any[] = data.products || data.data || [];
        // Filter to products that have no listings in the current listings array
        const listedProductIds = new Set(listings.map((l: any) => l.product_id));
        setUnlistedProducts(allProducts.filter(p => !listedProductIds.has(p.product_id)));
      } else {
        const res = await listingService.listUnlisted(channelFilter);
        setUnlistedProducts(res.data?.data || []);
      }
    } catch { /* ignore */ }
    finally { setLoading(false); }
  }

  // ─── Filtering & grouping ──────────────────────────────────────────────────

  const filtered = useMemo(() => {
    // Build set of product_ids that match credential filter (if any active)
    const credMatch = credentialFilters.size === 0
      ? null
      : new Set(listings.filter(l => credentialFilters.has(l.channel_account_id || '')).map(l => l.product_id));

    return listings.filter(l => {
      if (stateFilters.size > 0 && !stateFilters.has(l.state)) return false;
      if (credMatch !== null && !credMatch.has(l.product_id)) return false;
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
    });
  }, [listings, stateFilters, credentialFilters, search]);

  // GRD-02: group listings by product_id, optionally sorted by SEO score
  const productGroups = useMemo(() => {
    const groups = groupByProduct(filtered);
    if (!seoSort) return groups;
    return [...groups].sort((a, b) => {
      // Representative listing score for the group
      const repA = a.listings[0]?.listing_id;
      const repB = b.listings[0]?.listing_id;
      const scoreA = repA !== undefined ? (seoScores[repA] ?? -1) : -1;
      const scoreB = repB !== undefined ? (seoScores[repB] ?? -1) : -1;
      return seoSort === 'asc' ? scoreA - scoreB : scoreB - scoreA;
    });
  }, [filtered, seoSort, seoScores]);

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
        case 'optimise_seo':
          setBulkOptimiseOpen(true);
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

  // ─── Filter dropdown click-outside close ──────────────────────────────────
  useEffect(() => {
    function handleOutside(e: MouseEvent) {
      if (channelDropRef.current && !channelDropRef.current.contains(e.target as Node)) setChannelDropOpen(false);
      if (stateDropRef.current && !stateDropRef.current.contains(e.target as Node)) setStateDropOpen(false);
    }
    document.addEventListener('mousedown', handleOutside);
    return () => document.removeEventListener('mousedown', handleOutside);
  }, []);

  // ─── GRD-04: column chooser ────────────────────────────────────────────────







  const visibleCols = COLUMN_DEFS.filter(c => colVisibility[c.key]);

  // ─── Per-credential columns ─────────────────────────────────────────────────
  // One column per active credential account, always shown regardless of whether
  // any listing for the current product appears on that account.
  // Column label = account_name the user gave when connecting (e.g. "Temu Fasteners").
  const credentialCols = useMemo(() => {
    return credentials.map(cr => ({
      credentialId: cr.credential_id,
      label: cr.account_name,
      channel: cr.channel,
    }));
  }, [credentials]);

  // ─── GRD-05: resize columns ────────────────────────────────────────────────


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
              title={`${ch}: ${stateLabel(state)}`}
              onClick={(e) => { e.stopPropagation(); navigate(`/marketplace/listings/${listingId}/edit`); }}
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
              <div
                onClick={e => { e.stopPropagation(); navigate(`/products/${group.product_id}?tab=listings`); }}
                style={{ fontSize: 13, fontWeight: variant ? 500 : 600, maxWidth: '100%', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', color: 'var(--primary)', cursor: 'pointer', textDecoration: 'underline' }}>
                {variant ? (rep.product_title || rep.overrides?.title || '(Untitled)') : group.title}
              </div>
              {!variant && group.brand && <div style={{ fontSize: 11, color: 'var(--text-muted)' }}>{group.brand}</div>}
              {variant && <div style={{ fontSize: 11, color: 'var(--text-muted)', fontFamily: 'monospace' }}>{rep.listing_id}</div>}
            </div>
          </div>
        );
      case 'sku':
        return <span onClick={e => { e.stopPropagation(); navigate(`/products/${group.product_id}?tab=listings`); }} style={{ fontSize: 12, color: 'var(--primary)', fontFamily: 'monospace', cursor: 'pointer', textDecoration: 'underline' }}>{variant ? (rep.product_sku || '—') : group.sku}</span>;
      case 'channels':
        return variant ? (
          <span style={{ display: 'inline-flex', alignItems: 'center', gap: 3, padding: '2px 7px', borderRadius: 12, fontSize: 11, fontWeight: 600, background: sc.bg, color: sc.fg, border: `1px solid ${sc.fg}40` }}>
            {adapterEmoji[rep.channel] || '🌐'} {rep.channel}
          </span>
        ) : renderChannelBadges(group);
      case 'state':
        return <span style={{ padding: '2px 8px', borderRadius: 4, fontSize: 11, fontWeight: 700, background: sc.bg, color: sc.fg, border: `1px solid ${sc.fg}30` }}>{stateLabel(rep.state)}</span>;

      case 'updated':
        return <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>{formatDate(rep.updated_at)}</span>;
      case 'created':
        return <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>{formatDate(rep.created_at)}</span>;
      case 'product_id':
        return <span style={{ fontSize: 11, fontFamily: 'monospace', color: 'var(--text-muted)' }}>{group.product_id}</span>;
      case 'seo_score': {
        const score = seoScores[rep.listing_id] ?? null;
        return (
          <SEOScoreBadge
            score={score}
            size="sm"
            onClick={() => navigate(`/marketplace/listings/${rep.listing_id}?tab=seo`)}
          />
        );
      }
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
            {productGroups.length.toLocaleString()} product{productGroups.length !== 1 ? 's' : ''} · {total.toLocaleString()} listings · {credentialCols.length} channel{credentialCols.length !== 1 ? 's' : ''}
            {loadingMore && <span style={{ marginLeft: 8, fontSize: 12, color: 'var(--primary)' }}>· loading more…</span>}
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
          <button className="btn btn-primary" onClick={() => navigate('/marketplace/listings/create')}>+ Create Listings</button>
        </div>
      </div>

      {/* Filters */}
      <div className="card" style={{ padding: 16, marginBottom: 20, display: 'flex', gap: 12, alignItems: 'center', flexWrap: 'wrap' }}>
        <input className="input" style={{ flex: 1, minWidth: 200 }}
          placeholder="Search by SKU, title, or product ID..."
          value={search} onChange={e => setSearch(e.target.value)} />

        {/* Channel multi-select dropdown */}
        <div style={{ position: 'relative' }} ref={channelDropRef}>
          <button
            onClick={() => { setChannelDropOpen(o => !o); setStateDropOpen(false); }}
            style={{
              padding: '7px 12px', borderRadius: 6, border: '1px solid var(--border)',
              background: credentialFilters.size > 0 ? 'var(--primary-glow)' : 'var(--bg-secondary)',
              color: credentialFilters.size > 0 ? 'var(--primary)' : 'var(--text-primary)',
              cursor: 'pointer', fontSize: 13, display: 'flex', alignItems: 'center', gap: 6, whiteSpace: 'nowrap',
            }}>
            Channel {credentialFilters.size > 0 ? `(${credentialFilters.size})` : ''} ▾
          </button>
          {channelDropOpen && (
            <div style={{
              position: 'absolute', top: '100%', left: 0, marginTop: 4, zIndex: 60,
              background: 'var(--bg-secondary)', border: '1px solid var(--border)',
              borderRadius: 8, boxShadow: '0 8px 24px rgba(0,0,0,0.3)', minWidth: 200, padding: '6px 0',
            }}>
              {credentialFilters.size > 0 && (
                <button onClick={() => setCredentialFilters(new Set())}
                  style={{ width: '100%', padding: '6px 14px', textAlign: 'left', background: 'none', border: 'none', color: 'var(--primary)', cursor: 'pointer', fontSize: 12 }}>
                  Clear all
                </button>
              )}
              {credentials.map(cr => (
                <label key={cr.credential_id} style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '7px 14px', cursor: 'pointer' }}>
                  <input type="checkbox"
                    checked={credentialFilters.has(cr.credential_id)}
                    onChange={() => {
                      setCredentialFilters(prev => {
                        const n = new Set(prev);
                        n.has(cr.credential_id) ? n.delete(cr.credential_id) : n.add(cr.credential_id);
                        return n;
                      });
                    }} />
                  <span style={{ fontSize: 13 }}>{adapterEmoji[cr.channel] || '🌐'} {cr.account_name}</span>
                </label>
              ))}
            </div>
          )}
        </div>

        {/* State multi-select dropdown */}
        <div style={{ position: 'relative' }} ref={stateDropRef}>
          <button
            onClick={() => { setStateDropOpen(o => !o); setChannelDropOpen(false); }}
            style={{
              padding: '7px 12px', borderRadius: 6, border: '1px solid var(--border)',
              background: stateFilters.size > 0 ? 'var(--primary-glow)' : 'var(--bg-secondary)',
              color: stateFilters.size > 0 ? 'var(--primary)' : 'var(--text-primary)',
              cursor: 'pointer', fontSize: 13, display: 'flex', alignItems: 'center', gap: 6, whiteSpace: 'nowrap',
            }}>
            State {stateFilters.size > 0 ? `(${stateFilters.size})` : ''} ▾
          </button>
          {stateDropOpen && (
            <div style={{
              position: 'absolute', top: '100%', left: 0, marginTop: 4, zIndex: 60,
              background: 'var(--bg-secondary)', border: '1px solid var(--border)',
              borderRadius: 8, boxShadow: '0 8px 24px rgba(0,0,0,0.3)', minWidth: 180, padding: '6px 0',
            }}>
              {stateFilters.size > 0 && (
                <button onClick={() => setStateFilters(new Set())}
                  style={{ width: '100%', padding: '6px 14px', textAlign: 'left', background: 'none', border: 'none', color: 'var(--primary)', cursor: 'pointer', fontSize: 12 }}>
                  Clear all
                </button>
              )}
              {(['imported','draft','ready','published','paused','error'] as const).map(s => (
                <label key={s} style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '7px 14px', cursor: 'pointer' }}>
                  <input type="checkbox"
                    checked={stateFilters.has(s)}
                    onChange={() => {
                      setStateFilters(prev => {
                        const n = new Set(prev);
                        n.has(s) ? n.delete(s) : n.add(s);
                        return n;
                      });
                    }} />
                  <span style={{ fontSize: 13 }}>{stateLabel(s)}</span>
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
                borderRadius: 8, boxShadow: '0 8px 24px rgba(0,0,0,0.3)', minWidth: 200, overflow: 'hidden',
              }}>
                {[
                  { action: 'enrich', icon: '✨', label: 'Enrich Data' },
                  { action: 'publish', icon: '🚀', label: 'Publish' },
                  { action: 'revise', icon: '📝', label: 'Revise Fields' },
                  { action: 'optimise_seo', icon: '🎯', label: 'Optimise SEO' },
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
      </div>

      {error && (
        <div style={{ padding: 12, marginBottom: 16, borderRadius: 8, background: 'var(--danger-glow)', border: '1px solid var(--danger)', color: 'var(--danger)', fontSize: 13 }}>{error}</div>
      )}

      {/* ─── Unlisted products view ─────────────────────────────────────────── */}
      {viewMode === 'unlisted' ? (
        <div className="card" style={{ padding: 0, overflow: 'hidden' }}>
          {loading ? (
            <div style={{ padding: 40, textAlign: 'center', color: 'var(--text-muted)' }}>Loading products…</div>
          ) : filteredUnlisted.length === 0 ? (
            <div style={{ padding: 40, textAlign: 'center', color: 'var(--text-muted)' }}>
              {channelFilter === 'all' ? 'All products have listings on at least one channel.' : 'All products have listings on this channel.'}
            </div>
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
                <table style={{ width: '100%', borderCollapse: 'collapse', tableLayout: 'fixed' }}>
                  <colgroup>
                    {/* Checkbox col */}
                    <col style={{ width: 40 }} />
                    {/* Product + SKU always-visible cols */}
                    {visibleCols.filter(c => c.key !== 'channels' && c.key !== 'state').map(col => (
                      <col key={col.key} style={{ width: colWidths[col.key] }} />
                    ))}
                    {/* One col per credential account */}
                    {credentialCols.map(cr => (
                      <col key={cr.credentialId} style={{ width: 160 }} />
                    ))}

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

                      {/* Product + SKU (and any other non-channel visible cols) */}
                      {visibleCols.filter(c => c.key !== 'channels' && c.key !== 'state').map(col => (
                        <th key={col.key}
                          style={{
                            textAlign: col.align || 'left', padding: '10px 12px',
                            fontSize: 11, fontWeight: 700, color: 'var(--text-muted)',
                            textTransform: 'uppercase', letterSpacing: '0.05em',
                            userSelect: 'none', whiteSpace: 'nowrap',
                            cursor: col.key === 'seo_score' ? 'pointer' : 'default',
                          }}
                          onClick={col.key === 'seo_score' ? () => setSeoSort(prev =>
                            prev === 'asc' ? 'desc' : 'asc'
                          ) : undefined}
                          title={col.key === 'seo_score' ? 'Click to sort by SEO score' : undefined}
                        >
                          {col.label}
                          {col.key === 'seo_score' && seoSort && (
                            <span style={{ marginLeft: 4, opacity: 0.7 }}>
                              {seoSort === 'asc' ? '↑' : '↓'}
                            </span>
                          )}

                        </th>
                      ))}

                      {/* Per-credential channel columns */}
                      {credentialCols.map(cr => (
                        <th key={cr.credentialId}
                          style={{
                            textAlign: 'center', padding: '10px 12px',
                            fontSize: 11, fontWeight: 700, color: 'var(--text-muted)',
                            textTransform: 'uppercase', letterSpacing: '0.05em',
                            whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis',
                            maxWidth: 160,
                          }}
                          title={cr.label}
                        >
                          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 5 }}>
                            <span>{adapterEmoji[cr.channel] || '🌐'}</span>
                            <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', maxWidth: 120 }}>{cr.label}</span>
                          </div>
                        </th>
                      ))}


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
                            onClick={() => navigate(`/products/${group.product_id}?tab=listings`)}
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

                            {/* Non-channel visible cols (Product, SKU, etc.) */}
                            {visibleCols.filter(c => c.key !== 'channels' && c.key !== 'state').map(col => (
                              <td key={col.key} style={{ padding: '8px 12px', textAlign: col.align || 'left', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                                {col.key === 'product' ? (
                                  <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
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
                            {/* Per-credential status cells */}
                            {credentialCols.map(cr => {
                              const listing = group.listings.find(l => l.channel_account_id === cr.credentialId);
                              if (!listing) {
                                return (
                                  <td key={cr.credentialId} style={{ padding: '8px 12px', textAlign: 'center' }}>
                                    <span style={{
                                      display: 'inline-block', padding: '2px 8px', borderRadius: 4,
                                      fontSize: 11, fontWeight: 700,
                                      background: 'var(--bg-tertiary)', color: 'var(--text-muted)',
                                      border: '1px solid var(--border)',
                                    }}>UNLISTED</span>
                                  </td>
                                );
                              }
                              const sc = stateColors[listing.state] || stateColors.draft;
                              return (
                                <td key={cr.credentialId} style={{ padding: '8px 12px', textAlign: 'center' }}
                                  onClick={e => { e.stopPropagation(); navigate(`/marketplace/listings/${listing.listing_id}/edit`); }}>
                                  <span style={{
                                    display: 'inline-block', padding: '2px 8px', borderRadius: 4,
                                    fontSize: 11, fontWeight: 700, cursor: 'pointer',
                                    background: sc.bg, color: sc.fg, border: `1px solid ${sc.fg}30`,
                                  }}>
                                    {stateLabel(listing.state)}
                                  </span>
                                </td>
                              );
                            })}


                          </tr>

                          {/* GRD-02: variant child rows */}
                          {group.hasVariants && isExpanded && group.listings.map(variant => (
                            <tr
                              key={`variant-${variant.listing_id}`}
                              style={{ borderBottom: '1px solid var(--border)', cursor: 'pointer', background: 'var(--bg-secondary)' }}
                              onClick={() => navigate(`/marketplace/listings/${variant.listing_id}/edit`)}
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

                              {/* Non-channel cols, with indent on first */}
                              {visibleCols.filter(c => c.key !== 'channels' && c.key !== 'state').map((col, i) => (
                                <td key={col.key} style={{ padding: '6px 12px', paddingLeft: i === 0 ? 36 : 12, textAlign: col.align || 'left', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                                  {renderCell(col.key, group, variant)}
                                </td>
                              ))}
                              {/* Per-credential status for this variant */}
                              {credentialCols.map(cr => {
                                if (variant.channel_account_id !== cr.credentialId) {
                                  return (
                                    <td key={cr.credentialId} style={{ padding: '6px 12px', textAlign: 'center' }}>
                                      <span style={{ display: 'inline-block', padding: '2px 8px', borderRadius: 4, fontSize: 11, fontWeight: 700, background: 'var(--bg-tertiary)', color: 'var(--text-muted)', border: '1px solid var(--border)' }}>UNLISTED</span>
                                    </td>
                                  );
                                }
                                const sc = stateColors[variant.state] || stateColors.draft;
                                return (
                                  <td key={cr.credentialId} style={{ padding: '6px 12px', textAlign: 'center' }}>
                                    <span style={{ display: 'inline-block', padding: '2px 8px', borderRadius: 4, fontSize: 11, fontWeight: 700, background: sc.bg, color: sc.fg, border: `1px solid ${sc.fg}30` }}>
                                      {stateLabel(variant.state)}
                                    </span>
                                  </td>
                                );
                              })}


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

      {/* ─── Bulk Optimise SEO Modal ──────────────────────────────────────────── */}
      <BulkOptimiseModal
        listingIds={Array.from(selected)}
        isOpen={bulkOptimiseOpen}
        onClose={() => setBulkOptimiseOpen(false)}
        onComplete={() => { setSelected(new Set()); setBulkOptimiseOpen(false); }}
      />
    </div>
  );
}

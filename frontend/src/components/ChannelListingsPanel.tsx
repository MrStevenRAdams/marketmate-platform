// ============================================================================
// CHANNEL LISTINGS PANEL
// ============================================================================
import { useState, useEffect, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { api } from '../services/api';   // uses axios with Firebase auth + tenant headers

// ─── Style constants (must be declared before use to avoid TDZ errors) ───────
const card: React.CSSProperties = { background: 'var(--bg-elevated,#1a1e28)', border: '1px solid var(--border,rgba(255,255,255,0.07))', borderRadius: 10, padding: '16px 18px' };
const widgetTitle: React.CSSProperties = { fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.07em' };
const base: React.CSSProperties = { display: 'inline-flex', alignItems: 'center', gap: 5, padding: '6px 12px', borderRadius: 6, fontSize: 12, fontWeight: 500, cursor: 'pointer', whiteSpace: 'nowrap', border: 'none', fontFamily: 'inherit' };
const btnPri: React.CSSProperties = { ...base, background: 'var(--primary,#06b6d4)', color: '#000', fontWeight: 600 };
const btnSec: React.CSSProperties = { ...base, background: 'var(--bg-secondary,#13161e)', color: 'var(--text-muted)', border: '1px solid rgba(255,255,255,0.12)' };
const btnPub: React.CSSProperties = { ...base, background: 'rgba(34,197,94,0.15)', color: '#22c55e', border: '1px solid rgba(34,197,94,0.3)' };
const btnErr: React.CSSProperties = { ...base, background: 'rgba(239,68,68,0.12)', color: '#ef4444', border: '1px solid rgba(239,68,68,0.3)' };

// ─── Local types ─────────────────────────────────────────────────────────────

interface Credential {
  credential_id: string;
  channel: string;
  account_name: string;
  marketplace_id?: string;
  active: boolean;
}

interface Listing {
  listing_id: string;
  channel: string;
  channel_account_id: string;
  fulfillment_channel?: string;  // "AFN" = FBA, "MFN"/"MERCHANT" = FBM
  state: string;
  channel_identifiers?: { external_listing_id?: string; listing_url?: string; sku?: string };
  validation_state?: {
    status: string;
    blockers?: Array<{ code: string; message: string }>;
    warnings?: Array<{ code: string; message: string }>;
  };
  last_published_at?: string;
  last_synced_at?: string;
  overrides?: { price?: number };
}

interface DisplayRow {
  cred: Credential;
  listing?: Listing;
  fulfillmentBadge?: string;
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

function toFulfillmentBadge(fc: string | undefined): string | undefined {
  if (!fc) return undefined;
  const u = fc.toUpperCase();
  if (u === 'AFN') return 'FBA';
  if (u === 'MFN' || u === 'MERCHANT') return 'FBM';
  return fc;
}

function stateInfo(state?: string) {
  switch (state) {
    case 'published': return { label: 'Listed',     color: '#22c55e', bg: 'rgba(34,197,94,0.12)',   border: 'rgba(34,197,94,0.3)',   icon: '●' };
    case 'ready':     return { label: 'Ready',      color: '#06b6d4', bg: 'rgba(6,182,212,0.12)',   border: 'rgba(6,182,212,0.3)',   icon: '○' };
    case 'draft':     return { label: 'Draft',      color: '#94a3b8', bg: 'rgba(148,163,184,0.1)',  border: 'rgba(148,163,184,0.2)', icon: '◌' };
    case 'paused':    return { label: 'Paused',     color: '#f59e0b', bg: 'rgba(245,158,11,0.12)',  border: 'rgba(245,158,11,0.3)',  icon: '⏸' };
    case 'ended':     return { label: 'Ended',      color: '#64748b', bg: 'rgba(100,116,139,0.1)',  border: 'rgba(100,116,139,0.2)', icon: '◼' };
    case 'error':     return { label: 'Error',      color: '#ef4444', bg: 'rgba(239,68,68,0.12)',   border: 'rgba(239,68,68,0.3)',   icon: '✕' };
    case 'imported': return { label: 'Imported',   color: '#a78bfa', bg: 'rgba(167,139,250,0.12)', border: 'rgba(167,139,250,0.3)', icon: '↓' };
    default:          return { label: 'Not Listed', color: '#64748b', bg: 'rgba(100,116,139,0.08)', border: 'rgba(100,116,139,0.15)', icon: '–' };
  }
}

const EMOJI: Record<string, string> = {
  amazon: '📦', ebay: '🏷️', shopify: '🛒', temu: '🛍️', tesco: '🏪',
  tiktok: '🎵', etsy: '🛍️', woocommerce: '🛒', magento: '🏪',
  bigcommerce: '🛒', onbuy: '🏷️', walmart: '🛒', kaufland: '🛒',
  backmarket: '🔁', zalando: '👟', bol: '📚', lazada: '🛍️',
  bluepark: '🔵', wish: '⭐',
};

function createUrl(channel: string, credentialId: string, productId: string) {
  // FIX (Issue 7): backmarket, zalando, bol, lazada removed from dedicated list.
  // Their listing-create pages exist but have no routes in App.tsx yet.
  // They now fall back to the generic /marketplace/listings/create?channel=X form.
  // Re-add them here once their routes are registered in App.tsx.
  const dedicated = ['amazon','ebay','shopify','temu','tiktok','etsy','woocommerce','walmart',
    'kaufland','magento','bigcommerce','onbuy','bluepark','wish'];
  const ch = channel.toLowerCase();
  // amazonnew uses the same listing form as amazon
  const routeCh = ch === 'amazonnew' ? 'amazon' : ch;
  return dedicated.includes(ch) || ch === 'amazonnew'
    ? `/marketplace/${routeCh}/listings/create?product_id=${productId}&credential_id=${credentialId}`
    : `/marketplace/listings/create?product_id=${productId}&credential_id=${credentialId}&channel=${ch}`;
}

// Build display rows — one per listing (handles FBA+FBM on same credential)
function buildRows(credentials: Credential[], listings: Listing[]): DisplayRow[] {
  const rows: DisplayRow[] = [];
  for (const cred of credentials) {
    const credListings = listings.filter(l => l.channel_account_id === cred.credential_id);
    if (credListings.length === 0) {
      rows.push({ cred });
    } else if (credListings.length === 1) {
      const l = credListings[0];
      rows.push({ cred, listing: l, fulfillmentBadge: toFulfillmentBadge(l.fulfillment_channel) });
    } else {
      for (const l of credListings) {
        rows.push({
          cred, listing: l,
          fulfillmentBadge: toFulfillmentBadge(l.fulfillment_channel) ?? `#${credListings.indexOf(l) + 1}`,
        });
      }
    }
  }
  return rows;
}

// ─── Data hook ────────────────────────────────────────────────────────────────

function useListingsData(productId: string) {
  const [credentials, setCredentials] = useState<Credential[]>([]);
  const [listings, setListings] = useState<Listing[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    if (!productId) return;
    setLoading(true); setError(null);
    try {
      const [cr, lr] = await Promise.all([
        api.get('/marketplace/credentials'),
        api.get(`/marketplace/listings?product_id=${productId}`),
      ]);
      const creds: Credential[] = cr.data?.credentials ?? cr.data?.data ?? cr.data ?? [];
      setCredentials(creds.filter(c => c.active));
      const lsts: Listing[] = lr.data?.listings ?? lr.data?.data ?? lr.data ?? [];
      setListings(Array.isArray(lsts) ? lsts : []);
    } catch (e: any) {
      setError(e?.response?.data?.error ?? e?.message ?? 'Failed to load');
    } finally { setLoading(false); }
  }, [productId]);

  useEffect(() => { load(); }, [load]);
  return { credentials, listings, loading, error, reload: load };
}

// ─── Summary widget (right panel) ────────────────────────────────────────────

export function ChannelListingsSummary({ productId, onGoToListings }: { productId: string; onGoToListings: () => void }) {
  const { credentials, listings, loading } = useListingsData(productId);

  if (loading) return (
    <div style={card}><div style={widgetTitle}>Channel Listings</div><div style={{ fontSize: 12, color: 'var(--text-muted)', padding: '8px 0' }}>Loading…</div></div>
  );

  if (!credentials.length) return (
    <div style={card}><div style={widgetTitle}>Channel Listings</div><div style={{ fontSize: 12, color: 'var(--text-muted)', lineHeight: 1.5 }}>No channels connected.</div></div>
  );

  const rows = buildRows(credentials, listings);
  const listed = rows.filter(r => r.listing && ['published','ready'].includes(r.listing.state)).length;
  const problems = rows.some(r => !r.listing || ['error','blocked','ended','paused'].includes(r.listing?.state ?? ''));
  const allGood = listed === rows.length && !problems;
  const sc = allGood ? '#22c55e' : problems ? '#ef4444' : '#f59e0b';
  const sb = allGood ? 'rgba(34,197,94,0.12)' : problems ? 'rgba(239,68,68,0.1)' : 'rgba(245,158,11,0.1)';
  const sbd = allGood ? 'rgba(34,197,94,0.3)' : problems ? 'rgba(239,68,68,0.3)' : 'rgba(245,158,11,0.3)';

  return (
    <div style={card}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 10 }}>
        <div style={widgetTitle}>Channel Listings</div>
        <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>{listed}/{rows.length}</span>
      </div>
      <button onClick={onGoToListings} style={{ display: 'flex', alignItems: 'center', gap: 8, width: '100%', padding: '10px 12px', background: sb, border: `1px solid ${sbd}`, borderRadius: 8, cursor: 'pointer', textAlign: 'left' }}>
        <span style={{ fontSize: 14, color: sc }}>{allGood ? '✓' : problems ? '⚠' : '○'}</span>
        <div style={{ flex: 1 }}>
          <div style={{ fontSize: 12, fontWeight: 600, color: sc }}>{allGood ? 'Listed on all channels' : `${listed} of ${rows.length} active`}</div>
          {!allGood && <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 2 }}>Click to manage →</div>}
        </div>
      </button>
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, marginTop: 10 }}>
        {rows.map((row, i) => {
          const si = stateInfo(row.listing?.state);
          return (
            <button key={i} onClick={onGoToListings} title={`${row.cred.account_name}${row.fulfillmentBadge ? ` (${row.fulfillmentBadge})` : ''}: ${si.label}`}
              style={{ display: 'inline-flex', alignItems: 'center', gap: 4, padding: '3px 7px', background: si.bg, border: `1px solid ${si.border}`, borderRadius: 99, fontSize: 11, color: si.color, cursor: 'pointer', fontWeight: 500 }}>
              <span>{EMOJI[row.cred.channel.toLowerCase()] ?? '🔗'}</span>
              <span style={{ maxWidth: 80, overflow: 'hidden', textOverflow: 'ellipsis' }}>{row.cred.account_name}</span>
              {row.fulfillmentBadge && <span style={{ opacity: 0.75 }}>· {row.fulfillmentBadge}</span>}
            </button>
          );
        })}
      </div>
    </div>
  );
}

// ─── Full listings tab ────────────────────────────────────────────────────────

export function ChannelListingsTab({ productId }: { productId: string }) {
  const navigate = useNavigate();
  const { credentials, listings, loading, error, reload } = useListingsData(productId);
  const [publishing, setPublishing] = useState<string | null>(null);

  if (loading) return (
    <div style={{ textAlign: 'center', padding: 48, color: 'var(--text-muted)', fontSize: 13 }}>
      <div style={{ margin: '0 auto 12px', width: 28, height: 28, border: '3px solid var(--border)', borderTopColor: 'var(--accent-cyan)', borderRadius: '50%', animation: 'spin 0.8s linear infinite' }} />
      Loading channel listings…
    </div>
  );

  if (error) return (
    <div style={{ padding: 16, background: 'rgba(239,68,68,0.08)', border: '1px solid rgba(239,68,68,0.2)', borderRadius: 8, color: '#ef4444', fontSize: 13 }}>
      ⚠ {error} <button onClick={reload} style={{ marginLeft: 12, padding: '2px 10px', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 4, background: 'none', color: '#ef4444', cursor: 'pointer', fontSize: 12 }}>Retry</button>
    </div>
  );

  if (!credentials.length) return (
    <div style={{ textAlign: 'center', padding: 64, color: 'var(--text-muted)' }}>
      <div style={{ fontSize: 40, marginBottom: 12 }}>🔗</div>
      <h3 style={{ fontSize: 15, fontWeight: 600, marginBottom: 6, color: 'var(--text-secondary)' }}>No Channels Connected</h3>
      <p style={{ fontSize: 13, maxWidth: 360, margin: '0 auto 20px' }}>Connect a marketplace in Settings to start listing products.</p>
      <button onClick={() => navigate('/marketplace/connections')} style={btnSec}>Go to Marketplace Connections</button>
    </div>
  );

  const handlePublish = async (listingId: string) => {
    setPublishing(listingId);
    try {
      await api.post(`/marketplace/listings/${listingId}/publish`);
      await reload();
    } catch (e: any) {
      alert(e?.response?.data?.error ?? 'Failed to publish listing');
    } finally { setPublishing(null); }
  };

  const rows = buildRows(credentials, listings);
  const published = rows.filter(r => r.listing?.state === 'published').length;
  const problems  = rows.filter(r => r.listing && ['error','blocked'].includes(r.listing.state)).length;
  const unlisted  = rows.filter(r => !r.listing).length;

  const COLS = '200px 70px 110px 160px 100px 1fr auto';

  return (
    <div>
      {/* Summary bar */}
      <div style={{ display: 'flex', gap: 12, marginBottom: 20, flexWrap: 'wrap' }}>
        <Stat v={published} label="Published" c="#22c55e" />
        <Stat v={rows.filter(r => r.listing?.state === 'ready').length} label="Ready" c="#06b6d4" />
        <Stat v={rows.filter(r => r.listing && ['draft','paused','ended'].includes(r.listing.state)).length} label="Draft/Paused" c="#94a3b8" />
        <Stat v={problems} label="Errors" c="#ef4444" />
        <Stat v={unlisted} label="Not Listed" c="#64748b" />
      </div>

      {/* Grid */}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
        {/* Header */}
        <div style={{ display: 'grid', gridTemplateColumns: COLS, gap: 12, padding: '6px 16px' }}>
          {['Channel / Account','Fulfil.','Status','External ID / SKU','Last Synced','Issues',''].map((h, i) => (
            <div key={i} style={{ fontSize: 10, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.06em', textAlign: i === 6 ? 'right' : 'left' }}>{h}</div>
          ))}
        </div>

        {rows.map((row, i) => {
          const si = stateInfo(row.listing?.state);
          const badge = row.fulfillmentBadge;
          const badgeColor = badge === 'FBA'
            ? { color: '#f59e0b', bg: 'rgba(245,158,11,0.12)', border: 'rgba(245,158,11,0.3)' }
            : badge === 'FBM'
            ? { color: '#06b6d4', bg: 'rgba(6,182,212,0.12)', border: 'rgba(6,182,212,0.3)' }
            : { color: 'var(--text-muted)', bg: 'transparent', border: 'var(--border)' };

          const ch          = row.cred.channel.toLowerCase();
          const chLabel     = row.cred.channel.charAt(0).toUpperCase() + row.cred.channel.slice(1);
          const hasListing  = Boolean(row.listing);
          const isImported  = row.listing?.state === 'imported';
          const isPublished = row.listing?.state === 'published';
          const isError     = ['error','blocked'].includes(row.listing?.state ?? '');
          const isReady     = row.listing?.state === 'ready';
          const extUrl      = row.listing?.channel_identifiers?.listing_url;
          const cUrl        = createUrl(row.cred.channel, row.cred.credential_id, productId);
          // For channel-specific imported listings, route to the channel's create/edit form
          // (pre-populated via listing_id) rather than the generic ListingDetail page
          const eUrl = row.listing
            ? (ch === 'amazon' || ch === 'amazonnew')
              ? `/marketplace/amazon/listings/create?product_id=${productId}&credential_id=${row.cred.credential_id}&listing_id=${row.listing.listing_id}`
              : `/marketplace/listings/${row.listing.listing_id}`
            : cUrl;
          const blockers    = row.listing?.validation_state?.blockers ?? [];
          const warnings    = row.listing?.validation_state?.warnings ?? [];

          return (
            <div key={i} style={{ display: 'grid', gridTemplateColumns: COLS, alignItems: 'center', gap: 12, padding: '14px 16px', background: 'var(--bg-elevated,#1a1e28)', border: '1px solid var(--border,rgba(255,255,255,0.07))', borderRadius: 8, transition: 'border-color 0.15s' }}
              onMouseEnter={e => (e.currentTarget.style.borderColor = 'rgba(255,255,255,0.14)')}
              onMouseLeave={e => (e.currentTarget.style.borderColor = 'rgba(255,255,255,0.07)')}>

              {/* Channel */}
              <div style={{ display: 'flex', alignItems: 'center', gap: 10, minWidth: 0 }}>
                <span style={{ width: 32, height: 32, borderRadius: 8, background: 'var(--bg-card,#22273a)', display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 16, flexShrink: 0, border: '1px solid var(--border)' }}>
                  {EMOJI[ch] ?? '🔗'}
                </span>
                <div style={{ minWidth: 0 }}>
                  <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--text-primary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{chLabel}</div>
                  <div style={{ fontSize: 11, color: 'var(--text-muted)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{row.cred.account_name}</div>
                </div>
              </div>

              {/* Fulfillment badge */}
              <div>
                {badge
                  ? <span style={{ display: 'inline-flex', padding: '3px 8px', borderRadius: 99, fontSize: 11, fontWeight: 700, color: badgeColor.color, background: badgeColor.bg, border: `1px solid ${badgeColor.border}` }}>{badge}</span>
                  : <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>—</span>
                }
              </div>

              {/* State pill */}
              <div>
                <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5, padding: '3px 10px', borderRadius: 99, fontSize: 11, fontWeight: 600, color: si.color, background: si.bg, border: `1px solid ${si.border}` }}>
                  <span style={{ fontSize: 9 }}>{si.icon}</span>{si.label}
                </span>
              </div>

              {/* External ID */}
              <div style={{ fontFamily: 'monospace', fontSize: 11, color: 'var(--text-muted)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                {row.listing?.channel_identifiers?.external_listing_id || row.listing?.channel_identifiers?.sku || <span style={{ fontStyle: 'italic', fontFamily: 'inherit' }}>—</span>}
              </div>

              {/* Last synced */}
              <div style={{ fontSize: 11, color: 'var(--text-muted)' }}>
                {row.listing?.last_synced_at
                  ? new Date(row.listing.last_synced_at).toLocaleDateString('en-GB', { day: '2-digit', month: 'short', year: '2-digit' })
                  : <span style={{ fontStyle: 'italic' }}>Never</span>}
              </div>

              {/* Issues */}
              <div style={{ minWidth: 0 }}>
                {blockers.length > 0
                  ? <>{blockers.slice(0,2).map((b,j) => <div key={j} style={{ fontSize: 11, color: '#ef4444', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>✕ {b.message}</div>)}{blockers.length > 2 && <div style={{ fontSize: 11, color: 'var(--text-muted)' }}>+{blockers.length-2} more</div>}</>
                  : warnings.length > 0
                  ? <div style={{ fontSize: 11, color: '#f59e0b', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>⚠ {warnings[0].message}{warnings.length > 1 ? ` (+${warnings.length-1})` : ''}</div>
                  : isPublished ? <span style={{ fontSize: 11, color: '#22c55e' }}>No issues</span>
                  : isImported ? <span style={{ fontSize: 11, color: '#a78bfa' }}>Imported — review listing</span>
                  : !hasListing ? <span style={{ fontSize: 11, color: 'var(--text-muted)', fontStyle: 'italic' }}>Not created</span>
                  : <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>—</span>
                }
              </div>

              {/* Actions */}
              <div style={{ display: 'flex', gap: 6, justifyContent: 'flex-end', flexShrink: 0 }}>
                {isPublished && extUrl && <a href={extUrl} target="_blank" rel="noopener noreferrer" style={{ ...btnSec, textDecoration: 'none', fontSize: 12, padding: '5px 10px' }}>View Live ↗</a>}
                {isReady && <button disabled={publishing === row.listing?.listing_id} onClick={() => row.listing && handlePublish(row.listing.listing_id)} style={{ ...btnPub, opacity: publishing === row.listing?.listing_id ? 0.6 : 1 }}>{publishing === row.listing?.listing_id ? 'Publishing…' : 'Publish Now'}</button>}
                {!hasListing
                  ? <button onClick={() => navigate(cUrl)} style={btnPri}>List to Channel</button>
                  : <button onClick={() => navigate(eUrl)} style={isError ? btnErr : btnSec}>{isError ? '⚠ Fix Listing' : 'Edit Listing'}</button>
                }
              </div>
            </div>
          );
        })}
      </div>

      <p style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 20, lineHeight: 1.6 }}>
        Click <strong>List to Channel</strong> to create a new listing, or <strong>Edit Listing</strong> to update.
        Amazon accounts may show separate <strong>FBA</strong> and <strong>FBM</strong> rows.
      </p>

      {/* Temu category debug panel — only shown when at least one Temu credential exists */}
      {(() => {
        const temuCreds = credentials.filter(c => c.channel === 'temu' || c.channel === 'temu_sandbox');
        if (temuCreds.length === 0) return null;
        return <TemuCategoryDebugPanel productId={productId} temuCredentials={temuCreds} />;
      })()}
    </div>
  );
}

// ─── Temu Category Debug Panel ───────────────────────────────────────────────

function TemuCategoryDebugPanel({ productId, temuCredentials }: {
  productId: string;
  temuCredentials: Credential[];
}) {
  const [selectedCredId, setSelectedCredId] = useState(temuCredentials[0]?.credential_id ?? '');
  const [titleInput, setTitleInput] = useState('');
  const [titleLoaded, setTitleLoaded] = useState(false);
  const [running, setRunning] = useState(false);
  const [result, setResult] = useState<{
    request: Record<string, unknown>;
    response: unknown;
    error?: string;
    durationMs: number;
  } | null>(null);

  // Load product title once on mount
  useEffect(() => {
    if (titleLoaded || !productId) return;
    api.get(`/products/${productId}`)
      .then(r => {
        const p = r.data?.data ?? r.data;
        setTitleInput(p?.title ?? '');
        setTitleLoaded(true);
      })
      .catch(() => setTitleLoaded(true));
  }, [productId, titleLoaded]);

  const runLookup = async () => {
    if (!titleInput.trim() || running) return;
    setRunning(true);
    setResult(null);

    const request = { goodsName: titleInput.trim() };
    const t0 = Date.now();
    try {
      const res = await api.post(
        `/temu/categories/recommend${selectedCredId ? `?credential_id=${selectedCredId}` : ''}`,
        request,
      );
      setResult({ request, response: res.data, durationMs: Date.now() - t0 });
    } catch (err: any) {
      const errData = err?.response?.data ?? { message: err?.message ?? 'Network error' };
      setResult({ request, response: errData, error: err?.message ?? 'Request failed', durationMs: Date.now() - t0 });
    } finally {
      setRunning(false);
    }
  };

  const responseOk = result && !result.error && (result.response as any)?.ok === true;
  const items: any[] = responseOk ? ((result!.response as any)?.items ?? []) : [];
  const leafItems = items.filter((c: any) => c.leaf);

  const panelStyle: React.CSSProperties = {
    marginTop: 24, border: '1px solid var(--border)', borderRadius: 10,
    overflow: 'hidden', background: 'var(--bg-secondary)',
  };
  const headerStyle: React.CSSProperties = {
    display: 'flex', alignItems: 'center', gap: 10, padding: '12px 16px',
    background: 'var(--bg-tertiary)', borderBottom: '1px solid var(--border)',
  };
  const preStyle: React.CSSProperties = {
    background: '#1e1e2e', color: '#cdd6f4',
    fontFamily: '"Fira Code","JetBrains Mono","Consolas",monospace',
    fontSize: 11, lineHeight: 1.6, padding: '14px 16px', margin: 0,
    overflowX: 'auto', whiteSpace: 'pre-wrap' as const, wordBreak: 'break-word' as const,
    maxHeight: 400, overflowY: 'auto',
  };
  const labelS: React.CSSProperties = {
    fontSize: 10, fontWeight: 700, textTransform: 'uppercase' as const,
    letterSpacing: '0.06em', color: 'var(--text-muted)', marginBottom: 4, display: 'block',
  };

  return (
    <div style={panelStyle}>
      {/* Header */}
      <div style={headerStyle}>
        <span style={{ fontSize: 14 }}>🔍</span>
        <span style={{ fontWeight: 700, fontSize: 13, color: 'var(--text-primary)' }}>
          Temu Category Lookup — Debug
        </span>
        <span style={{ fontSize: 11, color: 'var(--text-muted)', marginLeft: 'auto' }}>
          Calls <code style={{ fontSize: 10, background: 'var(--bg-primary)', padding: '1px 5px', borderRadius: 3 }}>POST /temu/categories/recommend</code> — stays on this page
        </span>
      </div>

      <div style={{ padding: '14px 16px', display: 'flex', flexDirection: 'column', gap: 12 }}>

        {/* Credential picker — only shown if >1 Temu credential */}
        {temuCredentials.length > 1 && (
          <div>
            <span style={labelS}>Credential</span>
            <select
              value={selectedCredId}
              onChange={e => setSelectedCredId(e.target.value)}
              style={{ padding: '6px 10px', borderRadius: 6, border: '1px solid var(--border)', background: 'var(--bg-primary)', color: 'var(--text-primary)', fontSize: 13, cursor: 'pointer' }}
            >
              {temuCredentials.map(c => (
                <option key={c.credential_id} value={c.credential_id}>
                  {c.account_name} ({c.channel})
                </option>
              ))}
            </select>
          </div>
        )}

        {/* Title input */}
        <div>
          <span style={labelS}>Product title sent to Temu — edit to test different inputs</span>
          <div style={{ display: 'flex', gap: 8 }}>
            <input
              value={titleInput}
              onChange={e => setTitleInput(e.target.value)}
              onKeyDown={e => e.key === 'Enter' && runLookup()}
              placeholder={titleLoaded ? '(empty title)' : 'Loading product title…'}
              style={{ flex: 1, padding: '8px 12px', borderRadius: 6, border: '1px solid var(--border)', background: 'var(--bg-primary)', color: 'var(--text-primary)', fontSize: 13, outline: 'none' }}
            />
            <button
              onClick={runLookup}
              disabled={running || !titleInput.trim()}
              style={{ padding: '8px 20px', borderRadius: 6, border: 'none', background: running ? 'var(--border)' : 'var(--accent)', color: running ? 'var(--text-muted)' : '#fff', fontWeight: 700, fontSize: 13, cursor: running || !titleInput.trim() ? 'not-allowed' : 'pointer', whiteSpace: 'nowrap', opacity: titleInput.trim() ? 1 : 0.5 }}
            >
              {running ? '⏳ Running…' : '▶ Test Lookup'}
            </button>
          </div>
        </div>

        {/* Results */}
        {result && (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>

            {/* Summary bar */}
            <div style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '8px 12px', borderRadius: 6, background: responseOk ? 'rgba(34,197,94,0.08)' : 'rgba(239,68,68,0.08)', border: `1px solid ${responseOk ? 'rgba(34,197,94,0.25)' : 'rgba(239,68,68,0.25)'}` }}>
              <span style={{ fontSize: 15 }}>{responseOk ? '✅' : '❌'}</span>
              <span style={{ fontWeight: 700, fontSize: 13, color: responseOk ? '#22c55e' : '#ef4444' }}>
                {responseOk
                  ? `${items.length} categor${items.length === 1 ? 'y' : 'ies'} returned — ${leafItems.length} leaf`
                  : result.error ?? 'Request failed'}
              </span>
              <span style={{ fontSize: 11, color: 'var(--text-muted)', marginLeft: 'auto' }}>{result.durationMs}ms</span>
            </div>

            {/* Leaf results — the key diagnostic */}
            {responseOk && (
              <div>
                <span style={labelS}>Leaf categories (these would be auto-selected in the listing form)</span>
                {leafItems.length === 0 ? (
                  <div style={{ padding: '10px 14px', borderRadius: 6, background: 'rgba(245,158,11,0.08)', border: '1px solid rgba(245,158,11,0.3)', fontSize: 13, color: '#f59e0b', fontWeight: 600 }}>
                    ⚠ No leaf categories returned — this is why the manual picker opens automatically
                  </div>
                ) : (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
                    {leafItems.map((cat: any, i: number) => (
                      <div key={i} style={{ display: 'flex', alignItems: 'flex-start', gap: 10, padding: '8px 12px', borderRadius: 6, background: 'rgba(34,197,94,0.06)', border: '1px solid rgba(34,197,94,0.2)' }}>
                        <span style={{ fontSize: 11, fontWeight: 700, color: '#22c55e', flexShrink: 0, marginTop: 2 }}>✓ leaf</span>
                        <div style={{ flex: 1, minWidth: 0 }}>
                          {/* Full path breadcrumb */}
                          {cat.catPath && cat.catPath.length > 0 && (
                            <div style={{ fontSize: 11, color: 'var(--text-muted)', marginBottom: 2, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                              {cat.catPath.join(' › ')}
                            </div>
                          )}
                          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                            <span style={{ fontSize: 13, color: 'var(--text-primary)', fontWeight: 600 }}>
                              {cat.catName || '(no name — run schema sync)'}
                            </span>
                            <span style={{ fontFamily: 'monospace', fontSize: 11, color: 'var(--text-muted)', flexShrink: 0 }}>catId: {cat.catId}</span>
                          </div>
                        </div>
                        {cat.level != null && <span style={{ fontSize: 11, color: 'var(--text-muted)', flexShrink: 0, marginTop: 2 }}>level {cat.level}</span>}
                      </div>
                    ))}
                  </div>
                )}
              </div>
            )}

            {/* Non-leaf categories */}
            {responseOk && items.length > leafItems.length && (
              <div>
                <span style={labelS}>Non-leaf categories also returned ({items.filter((c: any) => !c.leaf).length})</span>
                <div style={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
                  {items.filter((c: any) => !c.leaf).map((cat: any, i: number) => (
                    <div key={i} style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '5px 12px', borderRadius: 6, background: 'var(--bg-tertiary)', border: '1px solid var(--border)' }}>
                      <span style={{ fontSize: 11, color: 'var(--text-muted)', flexShrink: 0 }}>○ branch</span>
                      <span style={{ fontFamily: 'monospace', fontSize: 12, color: 'var(--text-muted)', flexShrink: 0 }}>catId: {cat.catId}</span>
                      <span style={{ fontSize: 13, color: 'var(--text-secondary)' }}>{cat.catName}</span>
                    </div>
                  ))}
                </div>
              </div>
            )}

            {/* Raw request */}
            <div>
              <span style={labelS}>Request →</span>
              <pre style={{ ...preStyle, border: '1px solid #313244' }}>
                {JSON.stringify(result.request, null, 2)}
              </pre>
            </div>

            {/* Raw response */}
            <div>
              <span style={labelS}>← Response</span>
              <pre style={{ ...preStyle, color: responseOk ? '#a6e3a1' : '#f38ba8', border: `1px solid ${responseOk ? '#313244' : '#45293a'}` }}>
                {JSON.stringify(result.response, null, 2)}
              </pre>
            </div>

          </div>
        )}
      </div>
    </div>
  );
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

function Stat({ v, label, c }: { v: number; label: string; c: string }) {
  if (!v) return null;
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '6px 12px', background: 'var(--bg-elevated,#1a1e28)', border: '1px solid var(--border,rgba(255,255,255,0.07))', borderRadius: 8, fontSize: 12 }}>
      <span style={{ width: 8, height: 8, borderRadius: '50%', background: c, flexShrink: 0 }} />
      <span style={{ fontWeight: 700, color: c }}>{v}</span>
      <span style={{ color: 'var(--text-muted)' }}>{label}</span>
    </div>
  );
}


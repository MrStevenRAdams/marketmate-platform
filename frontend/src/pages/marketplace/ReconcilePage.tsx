// ============================================================================
// SKU RECONCILIATION PAGE  (Session 7 — History tab added)
// ============================================================================
// Route: /marketplace/channels/:id/reconcile
// Tabs:
//   "Current Run"  — existing matching / confirm flow
//   "History"      — past runs audit trail (S7 new)
// ============================================================================

import { useState, useEffect, useCallback } from 'react';
import { useParams, useNavigate, useSearchParams } from 'react-router-dom';
import { getActiveTenantId } from '../../contexts/TenantContext';

const API = (path: string) => `/api/v1${path}`;
const headers = () => ({
  'Content-Type': 'application/json',
  'X-Tenant-Id': getActiveTenantId() || '',
});

// ── S4 channel display config ─────────────────────────────────────────────────

const CHANNEL_META: Record<string, { label: string; color: string; isS4?: boolean; enrichSupported?: boolean }> = {
  backmarket: { label: 'Back Market', color: '#14B8A6', isS4: true, enrichSupported: true },
  zalando:    { label: 'Zalando',     color: '#FF6600', isS4: true, enrichSupported: false },
  bol:        { label: 'Bol.com',     color: '#0E4299', isS4: true, enrichSupported: false },
  lazada:     { label: 'Lazada',      color: '#F57224', isS4: true, enrichSupported: false },
  amazon:     { label: 'Amazon',      color: '#FF9900', enrichSupported: true },
  ebay:       { label: 'eBay',        color: '#E53238', enrichSupported: true },
  shopify:    { label: 'Shopify',     color: '#96BF48' },
  temu:       { label: 'Temu',        color: '#EA6A35' },
  etsy:       { label: 'Etsy',        color: '#F1641E' },
  woocommerce:{ label: 'WooCommerce', color: '#7F54B3' },
  walmart:    { label: 'Walmart',     color: '#0071CE' },
  kaufland:   { label: 'Kaufland',    color: '#D40000' },
  magento:    { label: 'Magento',     color: '#EE672F' },
  onbuy:      { label: 'OnBuy',       color: '#00A650' },
};

function chLabel(ch: string): string {
  return CHANNEL_META[ch]?.label ?? (ch.charAt(0).toUpperCase() + ch.slice(1));
}
function chColor(ch: string): string {
  return CHANNEL_META[ch]?.color ?? 'var(--accent, #6366f1)';
}
function chIsS4(ch: string): boolean {
  return CHANNEL_META[ch]?.isS4 === true;
}
function chEnrichSupported(ch: string): boolean {
  return CHANNEL_META[ch]?.enrichSupported === true;
}

// ── types ─────────────────────────────────────────────────────────────────────

type MatchType = 'full' | 'partial' | 'none';
type Decision = 'accepted' | 'new' | 'skipped' | '';

interface ReconcileRow {
  channel_sku: string;
  channel_title: string;
  channel_image: string;
  channel_price: number;
  channel_stock: number;
  external_id: string;
  match_type: MatchType;
  match_score: number;
  match_reason: string;
  internal_product_id: string;
  internal_sku: string;
  internal_title: string;
  decision: Decision;
  ai_enrich: boolean;
  push_status?: string;
  external_listing_id?: string;
  push_error?: string;
}

interface ReconcileJob {
  job_id: string;
  credential_id: string;
  channel: string;
  status: 'running' | 'complete' | 'confirming' | 'confirmed' | 'failed';
  total_rows: number;
  full_matches: number;
  partial: number;
  unmatched: number;
  confirmed: number;
  push_total?: number;
  push_succeeded?: number;
  push_failed?: number;
  push_manual_required?: number;
  rows: ReconcileRow[];
}

interface RunSummary {
  job_id: string;
  channel: string;
  status: string;
  created_at: string;
  completed_at?: string;
  total_rows: number;
  full_matches: number;
  partial: number;
  unmatched: number;
  confirmed: number;
  push_total?: number;
  push_succeeded?: number;
  push_failed?: number;
  push_manual_required?: number;
}

interface ProductSearchResult {
  product_id: string;
  sku: string;
  title: string;
}

// ── product search ────────────────────────────────────────────────────────────

function ProductSearch({ value, onSelect }: { value: string; onSelect: (id: string, sku: string, title: string) => void }) {
  const [query, setQuery] = useState(value);
  const [results, setResults] = useState<ProductSearchResult[]>([]);
  const [open, setOpen] = useState(false);

  const search = useCallback(async (q: string) => {
    if (!q || q.length < 2) { setResults([]); return; }
    try {
      const res = await fetch(API(`/search/products?q=${encodeURIComponent(q)}&limit=8`), { headers: headers() });
      const data = await res.json();
      setResults(data.results || data.data || []);
    } catch { setResults([]); }
  }, []);

  useEffect(() => {
    const t = setTimeout(() => search(query), 300);
    return () => clearTimeout(t);
  }, [query, search]);

  return (
    <div style={{ position: 'relative' }}>
      <input
        value={query}
        onChange={e => { setQuery(e.target.value); setOpen(true); }}
        onFocus={() => setOpen(true)}
        onBlur={() => setTimeout(() => setOpen(false), 200)}
        placeholder="Search products…"
        style={{
          width: '100%', padding: '6px 10px', borderRadius: 6, border: '1px solid var(--border)',
          background: 'var(--surface)', color: 'var(--text)', fontSize: 12, boxSizing: 'border-box',
        }}
      />
      {open && results.length > 0 && (
        <div style={{
          position: 'absolute', top: '100%', left: 0, right: 0, zIndex: 50,
          background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 8,
          boxShadow: '0 4px 20px rgba(0,0,0,0.12)', overflow: 'hidden', marginTop: 2,
        }}>
          {results.map(r => (
            <div
              key={r.product_id}
              onClick={() => { onSelect(r.product_id, r.sku, r.title); setQuery(r.sku + ' — ' + r.title); setOpen(false); }}
              style={{ padding: '8px 12px', cursor: 'pointer', borderBottom: '1px solid var(--border)' }}
              onMouseEnter={e => (e.currentTarget.style.background = 'var(--hover, rgba(0,0,0,0.04))')}
              onMouseLeave={e => (e.currentTarget.style.background = 'transparent')}
            >
              <div style={{ fontWeight: 600, fontSize: 12 }}>{r.sku}</div>
              <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{r.title}</div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

// ── row card ──────────────────────────────────────────────────────────────────

const MATCH_COLORS: Record<MatchType, string> = {
  full: '#22c55e',
  partial: '#f59e0b',
  none: '#94a3b8',
};

const MATCH_LABELS: Record<MatchType, string> = {
  full: '✓ Full match',
  partial: '~ Partial match',
  none: '✕ Unmatched',
};

const PUSH_BADGES: Record<string, { label: string; color: string }> = {
  live: { label: '🟢 Live', color: '#22c55e' },
  push_failed: { label: '🔴 Push failed', color: '#ef4444' },
  requires_manual_publish: { label: '⚠️ Manual required', color: '#f59e0b' },
};

function RowCard({
  row,
  channel,
  onChange,
}: {
  row: ReconcileRow;
  channel: string;
  onChange: (sku: string, updates: Partial<ReconcileRow>) => void;
}) {
  const isAmazon = channel === 'amazon';
  const isS4 = chIsS4(channel);
  const showEnrich = chEnrichSupported(channel);
  const channelColor = chColor(channel);

  const setDecision = (d: Decision) => onChange(row.channel_sku, { decision: d });
  const setAIEnrich = (v: boolean) => onChange(row.channel_sku, { ai_enrich: v });
  const setMatch = (id: string, sku: string, title: string) =>
    onChange(row.channel_sku, { internal_product_id: id, internal_sku: sku, internal_title: title });

  const decided = row.decision !== '';
  const badgeColor = MATCH_COLORS[row.match_type];
  const pushBadge = row.push_status ? PUSH_BADGES[row.push_status] : null;

  return (
    <div style={{
      display: 'grid',
      gridTemplateColumns: '1fr auto 1fr auto',
      gap: 12,
      alignItems: 'start',
      padding: '14px 16px',
      borderRadius: 10,
      border: `1px solid ${decided ? (row.decision === 'skipped' ? 'var(--border)' : (isS4 ? channelColor + '33' : 'var(--accent, #6366f1)22')) : (isS4 ? channelColor + '20' : 'var(--border)')}`,
      background: decided
        ? row.decision === 'accepted' ? 'rgba(34,197,94,0.03)'
          : row.decision === 'new' ? (isS4 ? `${channelColor}06` : 'rgba(99,102,241,0.03)')
          : 'var(--surface-2)'
        : 'var(--surface)',
      transition: 'all 0.15s',
      marginBottom: 8,
      opacity: row.decision === 'skipped' ? 0.5 : 1,
    }}>
      {/* Channel side */}
      <div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 6 }}>
          {isS4 && (
            <span style={{ width: 8, height: 8, borderRadius: '50%', background: channelColor, display: 'inline-block', flexShrink: 0 }} />
          )}
          <span style={{ fontSize: 11, fontWeight: 700, color: isS4 ? channelColor : 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.06em' }}>
            {chLabel(channel)}
          </span>
          {isS4 && (
            <span style={{ fontSize: 9, fontWeight: 700, color: channelColor, padding: '1px 5px', border: `1px solid ${channelColor}40`, borderRadius: 10 }}>NEW</span>
          )}
        </div>
        <div style={{ display: 'flex', gap: 10, alignItems: 'flex-start' }}>
          {row.channel_image ? (
            <img src={row.channel_image} alt="" style={{ width: 48, height: 48, objectFit: 'cover', borderRadius: 6, flexShrink: 0, border: '1px solid var(--border)' }} />
          ) : (
            <div style={{ width: 48, height: 48, background: 'var(--surface-2)', borderRadius: 6, flexShrink: 0, display: 'flex', alignItems: 'center', justifyContent: 'center', color: 'var(--text-muted)', fontSize: 18 }}>📦</div>
          )}
          <div style={{ minWidth: 0 }}>
            <div style={{ fontWeight: 700, fontSize: 13, fontFamily: 'monospace', color: 'var(--accent, #6366f1)' }}>{row.channel_sku}</div>
            <div style={{ fontSize: 12, color: 'var(--text)', marginTop: 2, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', maxWidth: 260 }}>{row.channel_title}</div>
            <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 3 }}>
              {row.channel_price > 0 && <span style={{ marginRight: 8 }}>£{row.channel_price.toFixed(2)}</span>}
              {row.channel_stock > 0 && <span>{row.channel_stock} in stock</span>}
            </div>
            {pushBadge && (
              <div style={{ marginTop: 4, fontSize: 11, fontWeight: 600, color: pushBadge.color }}>
                {pushBadge.label}
                {row.external_listing_id && <span style={{ fontWeight: 400, marginLeft: 4, color: 'var(--text-muted)' }}>#{row.external_listing_id}</span>}
              </div>
            )}
          </div>
        </div>
      </div>

      {/* Match badge + arrow */}
      <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', gap: 4, padding: '8px 0' }}>
        <span style={{ fontSize: 11, fontWeight: 600, color: badgeColor, background: badgeColor + '18', borderRadius: 999, padding: '2px 8px', whiteSpace: 'nowrap' }}>
          {MATCH_LABELS[row.match_type]}
        </span>
        {row.match_reason && (
          <span style={{ fontSize: 10, color: 'var(--text-muted)' }}>via {row.match_reason}</span>
        )}
        {row.match_score > 0 && row.match_type === 'partial' && (
          <span style={{ fontSize: 10, color: 'var(--text-muted)' }}>{Math.round(row.match_score * 100)}%</span>
        )}
        <span style={{ fontSize: 16, color: 'var(--text-muted)', marginTop: 2 }}>→</span>
      </div>

      {/* Internal side */}
      <div>
        <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.06em', marginBottom: 6 }}>MarketMate Product</div>
        {row.decision === 'new' ? (
          <div style={{ padding: '10px 12px', background: 'rgba(99,102,241,0.06)', borderRadius: 8, border: '1px dashed var(--accent, #6366f1)' }}>
            <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--accent, #6366f1)' }}>🆕 Will be created as draft</div>
            <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 2 }}>SKU: {row.channel_sku} · Status: draft</div>
          </div>
        ) : (
          <>
            {row.internal_product_id ? (
              <div style={{ marginBottom: 6, padding: '8px 10px', background: 'var(--surface-2)', borderRadius: 6 }}>
                <div style={{ fontWeight: 700, fontSize: 13, fontFamily: 'monospace', color: 'var(--text)' }}>{row.internal_sku}</div>
                <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{row.internal_title}</div>
              </div>
            ) : (
              <div style={{ marginBottom: 6, padding: '8px 10px', background: 'var(--surface-2)', borderRadius: 6, color: 'var(--text-muted)', fontSize: 12 }}>
                No match found
              </div>
            )}
            <ProductSearch
              value={row.internal_sku || ''}
              onSelect={(id, sku, title) => setMatch(id, sku, title)}
            />
          </>
        )}

        {/* Enrichment toggle */}
        {showEnrich && row.decision !== 'skipped' && (
          <div style={{ marginTop: 8, display: 'flex', alignItems: 'center', gap: 6 }}>
            {isAmazon ? (
              <span style={{ fontSize: 11, color: '#22c55e', fontWeight: 500 }}>🤖 Amazon enrichment: automatic</span>
            ) : isS4 ? (
              <label style={{ display: 'flex', alignItems: 'center', gap: 6, cursor: 'pointer', fontSize: 12 }}>
                <input
                  type="checkbox"
                  checked={row.ai_enrich}
                  onChange={e => setAIEnrich(e.target.checked)}
                  style={{ width: 14, height: 14 }}
                />
                <span>🤖 AI Enrich <span style={{ color: 'var(--text-muted)' }}>(refurb grade + condition)</span></span>
              </label>
            ) : (
              <label style={{ display: 'flex', alignItems: 'center', gap: 6, cursor: 'pointer', fontSize: 12 }}>
                <input
                  type="checkbox"
                  checked={row.ai_enrich}
                  onChange={e => setAIEnrich(e.target.checked)}
                  style={{ width: 14, height: 14 }}
                />
                <span>🤖 AI Enrich <span style={{ color: 'var(--text-muted)' }}>(uses AI credits)</span></span>
              </label>
            )}
          </div>
        )}
        {/* S4 channels that require manual publish after confirm */}
        {isS4 && !showEnrich && row.decision === 'new' && (channel === 'zalando' || channel === 'lazada') && (
          <div style={{ marginTop: 8, fontSize: 11, color: '#f59e0b', display: 'flex', alignItems: 'center', gap: 4 }}>
            ⚠️ {chLabel(channel)} requires manual activation in Seller Centre after confirm
          </div>
        )}
      </div>

      {/* Decision buttons */}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 5, minWidth: 110 }}>
        {row.match_type !== 'none' && (
          <button
            onClick={() => setDecision(row.decision === 'accepted' ? '' : 'accepted')}
            style={{
              padding: '6px 10px', borderRadius: 6, border: 'none', cursor: 'pointer', fontSize: 12, fontWeight: 600,
              background: row.decision === 'accepted' ? '#22c55e' : 'rgba(34,197,94,0.1)',
              color: row.decision === 'accepted' ? '#fff' : '#22c55e',
              transition: 'all 0.15s',
            }}
          >
            ✓ Accept
          </button>
        )}
        <button
          onClick={() => setDecision(row.decision === 'new' ? '' : 'new')}
          style={{
            padding: '6px 10px', borderRadius: 6, border: 'none', cursor: 'pointer', fontSize: 12, fontWeight: 600,
            background: row.decision === 'new' ? 'var(--accent, #6366f1)' : 'rgba(99,102,241,0.1)',
            color: row.decision === 'new' ? '#fff' : 'var(--accent, #6366f1)',
            transition: 'all 0.15s',
          }}
        >
          🆕 Import New
        </button>
        <button
          onClick={() => setDecision(row.decision === 'skipped' ? '' : 'skipped')}
          style={{
            padding: '6px 10px', borderRadius: 6, border: '1px solid var(--border)', cursor: 'pointer', fontSize: 12,
            background: 'var(--surface)', color: 'var(--text-muted)',
          }}
        >
          Skip
        </button>
      </div>
    </div>
  );
}

// ── History tab ───────────────────────────────────────────────────────────────

function HistoryTab({ credentialID }: { credentialID: string }) {
  const [runs, setRuns] = useState<RunSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [expanded, setExpanded] = useState<string | null>(null);

  useEffect(() => {
    fetch(API(`/marketplace/credentials/${credentialID}/reconcile/history`), { headers: headers() })
      .then(r => r.ok ? r.json() : { data: [] })
      .then(d => setRuns(d.data || []))
      .catch(() => setRuns([]))
      .finally(() => setLoading(false));
  }, [credentialID]);

  const fmt = (iso?: string) => {
    if (!iso) return '—';
    const d = new Date(iso);
    return d.toLocaleDateString('en-GB', { day: '2-digit', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit' });
  };

  if (loading) return <div style={{ textAlign: 'center', padding: 40, color: 'var(--text-muted)' }}>Loading history…</div>;
  if (runs.length === 0) return (
    <div style={{ textAlign: 'center', padding: 60, background: 'var(--surface)', borderRadius: 12, border: '1px solid var(--border)' }}>
      <div style={{ fontSize: 36, marginBottom: 12 }}>📋</div>
      <h3 style={{ margin: '0 0 8px' }}>No history yet</h3>
      <p style={{ color: 'var(--text-muted)', margin: 0 }}>Run reconciliation and confirm decisions to build up an audit trail.</p>
    </div>
  );

  return (
    <div>
      <p style={{ color: 'var(--text-muted)', fontSize: 13, marginBottom: 16 }}>
        Showing last {runs.length} reconciliation run{runs.length !== 1 ? 's' : ''}. Click a row to expand details.
      </p>
      <div style={{ background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 10, overflow: 'hidden' }}>
        {/* Table header */}
        <div style={{
          display: 'grid',
          gridTemplateColumns: '2fr 1fr 80px 80px 80px 80px 80px 100px',
          gap: 8,
          padding: '10px 16px',
          background: 'var(--surface-2)',
          borderBottom: '1px solid var(--border)',
          fontSize: 11,
          fontWeight: 700,
          color: 'var(--text-muted)',
          textTransform: 'uppercase',
          letterSpacing: '0.06em',
        }}>
          <div>Date</div>
          <div>Status</div>
          <div style={{ textAlign: 'right' }}>Total</div>
          <div style={{ textAlign: 'right' }}>Matched</div>
          <div style={{ textAlign: 'right' }}>Confirmed</div>
          <div style={{ textAlign: 'right' }}>Pushed</div>
          <div style={{ textAlign: 'right' }}>Failed</div>
          <div style={{ textAlign: 'right' }}>Manual</div>
        </div>

        {runs.map((run, idx) => (
          <div key={run.job_id}>
            <div
              onClick={() => setExpanded(expanded === run.job_id ? null : run.job_id)}
              style={{
                display: 'grid',
                gridTemplateColumns: '2fr 1fr 80px 80px 80px 80px 80px 100px',
                gap: 8,
                padding: '12px 16px',
                cursor: 'pointer',
                borderBottom: idx < runs.length - 1 ? '1px solid var(--border)' : 'none',
                background: expanded === run.job_id ? 'rgba(99,102,241,0.04)' : 'transparent',
                transition: 'background 0.15s',
              }}
              onMouseEnter={e => { if (expanded !== run.job_id) e.currentTarget.style.background = 'var(--hover, rgba(0,0,0,0.03))'; }}
              onMouseLeave={e => { if (expanded !== run.job_id) e.currentTarget.style.background = 'transparent'; }}
            >
              <div>
                <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--text)' }}>{fmt(run.created_at)}</div>
                {run.completed_at && (
                  <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 1 }}>Completed {fmt(run.completed_at)}</div>
                )}
              </div>
              <div>
                <span style={{
                  fontSize: 11, fontWeight: 600, borderRadius: 999, padding: '2px 8px',
                  background: run.status === 'confirmed' ? '#22c55e18' : run.status === 'failed' ? '#ef444418' : 'var(--surface-2)',
                  color: run.status === 'confirmed' ? '#22c55e' : run.status === 'failed' ? '#ef4444' : 'var(--text-muted)',
                }}>
                  {run.status === 'confirmed' ? '✅ Confirmed' : run.status === 'failed' ? '❌ Failed' : run.status}
                </span>
              </div>
              <div style={{ textAlign: 'right', fontSize: 13 }}>{run.total_rows}</div>
              <div style={{ textAlign: 'right', fontSize: 13, color: '#22c55e' }}>{run.full_matches}</div>
              <div style={{ textAlign: 'right', fontSize: 13, color: 'var(--accent, #6366f1)' }}>{run.confirmed}</div>
              <div style={{ textAlign: 'right', fontSize: 13, color: '#22c55e' }}>{run.push_succeeded ?? '—'}</div>
              <div style={{ textAlign: 'right', fontSize: 13, color: run.push_failed ? '#ef4444' : 'var(--text-muted)' }}>{run.push_failed ?? '—'}</div>
              <div style={{ textAlign: 'right', fontSize: 13, color: run.push_manual_required ? '#f59e0b' : 'var(--text-muted)' }}>{run.push_manual_required ?? '—'}</div>
            </div>

            {/* Expanded detail row */}
            {expanded === run.job_id && (
              <div style={{
                padding: '14px 20px',
                background: 'rgba(99,102,241,0.03)',
                borderBottom: idx < runs.length - 1 ? '1px solid var(--border)' : 'none',
              }}>
                <div style={{ display: 'flex', gap: 24, flexWrap: 'wrap' }}>
                  {[
                    { label: 'Job ID', value: run.job_id, mono: true },
                    { label: 'Channel', value: run.channel, isChannel: true },
                    { label: 'Total listings fetched', value: String(run.total_rows) },
                    { label: 'Full matches', value: String(run.full_matches) },
                    { label: 'Partial matches', value: String(run.partial) },
                    { label: 'Unmatched', value: String(run.unmatched) },
                    { label: 'Confirmed mappings', value: String(run.confirmed) },
                    { label: 'Listings pushed live', value: String(run.push_succeeded ?? '—') },
                    { label: 'Push failures', value: String(run.push_failed ?? '—') },
                    { label: 'Require manual publish', value: String(run.push_manual_required ?? '—') },
                  ].map(item => (
                    <div key={item.label}>
                      <div style={{ fontSize: 10, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', marginBottom: 2 }}>{item.label}</div>
                      {item.isChannel ? (
                        <div style={{ fontSize: 13, display: 'flex', alignItems: 'center', gap: 6 }}>
                          <span style={{ width: 8, height: 8, borderRadius: '50%', background: chColor(item.value), display: 'inline-block', flexShrink: 0 }} />
                          <span style={{ color: chIsS4(item.value) ? chColor(item.value) : 'var(--text)', fontWeight: chIsS4(item.value) ? 600 : 400 }}>
                            {chLabel(item.value)}
                          </span>
                          {chIsS4(item.value) && (
                            <span style={{ fontSize: 9, fontWeight: 700, color: chColor(item.value), padding: '1px 5px', border: `1px solid ${chColor(item.value)}40`, borderRadius: 10 }}>NEW</span>
                          )}
                        </div>
                      ) : (
                        <div style={{ fontSize: 13, fontFamily: item.mono ? 'monospace' : undefined, color: 'var(--text)' }}>{item.value}</div>
                      )}
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}

// ── main page ─────────────────────────────────────────────────────────────────

type PageTab = 'current' | 'history';

export default function ReconcilePage() {
  const { id: credentialID } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const tenantID = getActiveTenantId();

  const initialTab = (searchParams.get('tab') as PageTab) || 'current';
  const [activeTab, setActiveTab] = useState<PageTab>(initialTab);

  const [job, setJob] = useState<ReconcileJob | null>(null);
  const [rows, setRows] = useState<ReconcileRow[]>([]);
  const [loading, setLoading] = useState(true);
  const [running, setRunning] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [exporting, setExporting] = useState(false);
  const [filter, setFilter] = useState<'all' | MatchType>('all');
  const [pollTimer, setPollTimer] = useState<ReturnType<typeof setInterval> | null>(null);
  const [toast, setToast] = useState<{ type: 'ok' | 'err'; msg: string } | null>(null);
  const [importUploading, setImportUploading] = useState(false);

  const switchTab = (tab: PageTab) => {
    setActiveTab(tab);
    setSearchParams(tab === 'current' ? {} : { tab });
  };

  const showToast = (type: 'ok' | 'err', msg: string) => {
    setToast({ type, msg });
    setTimeout(() => setToast(null), 4000);
  };

  useEffect(() => {
    if (!credentialID || !tenantID) return;
    fetch(API(`/marketplace/credentials/${credentialID}/reconcile`), { headers: headers() })
      .then(r => r.ok ? r.json() : null)
      .then(d => {
        if (d?.data) {
          setJob(d.data);
          setRows(d.data.rows || []);
          if (d.data.status === 'running') startPolling();
        }
      })
      .catch(() => {})
      .finally(() => setLoading(false));
  }, [credentialID, tenantID]);

  const startPolling = () => {
    const t = setInterval(async () => {
      const r = await fetch(API(`/marketplace/credentials/${credentialID}/reconcile`), { headers: headers() });
      if (!r.ok) return;
      const d = await r.json();
      if (d.data) {
        setJob(d.data);
        setRows(d.data.rows || []);
        if (d.data.status !== 'running') {
          clearInterval(t);
          setPollTimer(null);
          setRunning(false);
        }
      }
    }, 2000);
    setPollTimer(t);
  };

  useEffect(() => () => { if (pollTimer) clearInterval(pollTimer); }, [pollTimer]);

  const handleRunAutoLink = async () => {
    if (!credentialID || !tenantID) return;
    setRunning(true);
    setLoading(true);
    try {
      const r = await fetch(API(`/marketplace/credentials/${credentialID}/auto-link`), {
        method: 'POST', headers: headers(),
      });
      if (!r.ok) throw new Error('Failed to start auto-link');
      startPolling();
    } catch (e: any) {
      showToast('err', e.message);
      setRunning(false);
    } finally {
      setLoading(false);
    }
  };

  const updateRow = (sku: string, updates: Partial<ReconcileRow>) => {
    setRows(prev => prev.map(r => r.channel_sku === sku ? { ...r, ...updates } : r));
  };

  const handleConfirm = async () => {
    if (!credentialID || !tenantID) return;
    const decided = rows.filter(r => r.decision !== '');
    if (decided.length === 0) {
      showToast('err', 'No decisions made yet — accept, import, or skip rows first');
      return;
    }
    setConfirming(true);
    try {
      const r = await fetch(API(`/marketplace/credentials/${credentialID}/reconcile/confirm`), {
        method: 'POST',
        headers: headers(),
        body: JSON.stringify({
          rows: decided.map(r => ({
            channel_sku: r.channel_sku,
            decision: r.decision,
            internal_product_id: r.internal_product_id,
            ai_enrich: r.ai_enrich,
          })),
        }),
      });
      if (!r.ok) throw new Error('Confirm failed');
      const d = await r.json();
      const pushMsg = d.push_summary?.pushed > 0
        ? ` · ${d.push_summary.pushed} listing${d.push_summary.pushed !== 1 ? 's' : ''} pushed live`
        : '';
      showToast('ok', `✅ ${d.confirmed} mapping${d.confirmed !== 1 ? 's' : ''} saved${pushMsg}`);
      setJob(prev => prev ? { ...prev, confirmed: d.confirmed, status: 'confirmed', ...d.push_summary } : prev);
    } catch (e: any) {
      showToast('err', e.message);
    } finally {
      setConfirming(false);
    }
  };

  const handleExport = async () => {
    if (!credentialID || !tenantID) return;
    setExporting(true);
    try {
      const r = await fetch(API(`/marketplace/credentials/${credentialID}/reconcile/export`), { headers: headers() });
      if (!r.ok) throw new Error('Export failed');
      const blob = await r.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `unmatched_skus_${credentialID}.xlsx`;
      a.click();
      URL.revokeObjectURL(url);
    } catch (e: any) {
      showToast('err', e.message);
    } finally {
      setExporting(false);
    }
  };

  const handleImportResolutions = async (file: File) => {
    if (!credentialID || !tenantID) return;
    setImportUploading(true);
    try {
      const fd = new FormData();
      fd.append('file', file);
      const r = await fetch(API(`/marketplace/credentials/${credentialID}/reconcile/import`), {
        method: 'POST',
        headers: { 'X-Tenant-Id': tenantID },
        body: fd,
      });
      if (!r.ok) throw new Error('Import failed');
      const d = await r.json();
      showToast('ok', `✅ ${d.confirmed} mapping${d.confirmed !== 1 ? 's' : ''} applied from file`);
      const reloaded = await fetch(API(`/marketplace/credentials/${credentialID}/reconcile`), { headers: headers() });
      if (reloaded.ok) {
        const rd = await reloaded.json();
        setJob(rd.data);
        setRows(rd.data.rows || []);
      }
    } catch (e: any) {
      showToast('err', e.message);
    } finally {
      setImportUploading(false);
    }
  };

  const visibleRows = filter === 'all' ? rows : rows.filter(r => r.match_type === filter);
  const decidedCount = rows.filter(r => r.decision !== '').length;
  const undecidedCount = rows.filter(r => r.decision === '' && r.match_type !== 'full').length;

  return (
    <div style={{ padding: '24px 28px', fontFamily: "'DM Sans', system-ui, sans-serif", maxWidth: 1200, margin: '0 auto' }}>
      {/* Toast */}
      {toast && (
        <div style={{
          position: 'fixed', top: 20, right: 20, zIndex: 9999,
          padding: '12px 20px', borderRadius: 10, fontWeight: 600, fontSize: 14,
          background: toast.type === 'ok' ? '#22c55e' : '#ef4444', color: '#fff',
          boxShadow: '0 4px 20px rgba(0,0,0,0.2)',
        }}>
          {toast.msg}
        </div>
      )}

      {/* Header */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 6 }}>
        <button onClick={() => navigate(-1)} style={{ background: 'none', border: 'none', color: 'var(--text-muted)', cursor: 'pointer', fontSize: 20 }}>←</button>
        <h1 style={{ margin: 0, fontSize: 22, fontWeight: 700 }}>SKU Reconciliation</h1>
        {job && (
          <>
            {/* Channel badge */}
            <span style={{
              fontSize: 12, borderRadius: 999, padding: '2px 10px', fontWeight: 600,
              background: chIsS4(job.channel) ? chColor(job.channel) : 'var(--surface-2)',
              color: chIsS4(job.channel) ? '#fff' : 'var(--text-muted)',
              display: 'flex', alignItems: 'center', gap: 4,
            }}>
              {chLabel(job.channel)}
              {chIsS4(job.channel) && <span style={{ fontSize: 9, opacity: 0.85 }}>NEW</span>}
            </span>
            {/* Status badge */}
            <span style={{ fontSize: 12, background: job.status === 'confirmed' ? '#22c55e' : job.status === 'running' ? '#f59e0b' : 'var(--surface-2)', color: job.status === 'confirmed' ? '#fff' : job.status === 'running' ? '#fff' : 'var(--text-muted)', borderRadius: 999, padding: '2px 10px', fontWeight: 600 }}>
              {job.status === 'running' ? '⏳ Matching…' : job.status === 'confirmed' ? '✅ Confirmed' : job.status === 'failed' ? '❌ Failed' : '✓ Ready'}
            </span>
          </>
        )}
        {/* View History shortcut */}
        {activeTab === 'current' && (
          <button
            onClick={() => switchTab('history')}
            style={{ marginLeft: 'auto', padding: '5px 12px', borderRadius: 6, border: '1px solid var(--border)', background: 'var(--surface)', color: 'var(--text-muted)', fontSize: 12, cursor: 'pointer' }}
          >
            📋 View History
          </button>
        )}
      </div>
      <p style={{ color: 'var(--text-muted)', margin: '0 0 16px 36px', fontSize: 14 }}>
        Match channel listings to your internal product catalogue. Exact SKU matches are accepted automatically.
      </p>

      {/* S4 channel info banner — shown when current run is for an S4 channel */}
      {job && chIsS4(job.channel) && (
        <div style={{
          margin: '0 0 16px 36px',
          padding: '10px 16px',
          borderRadius: 8,
          background: `${chColor(job.channel)}10`,
          border: `1px solid ${chColor(job.channel)}30`,
          fontSize: 13,
          color: 'var(--text-secondary)',
          display: 'flex',
          gap: 10,
          alignItems: 'flex-start',
        }}>
          <span style={{ fontSize: 16, flexShrink: 0 }}>ℹ️</span>
          <div>
            <strong style={{ color: chColor(job.channel) }}>{chLabel(job.channel)}</strong>
            {job.channel === 'backmarket' && (
              <span> — Back Market will automatically activate accepted listings. AI enrichment available for refurb grade and condition data.</span>
            )}
            {job.channel === 'bol' && (
              <span> — Bol.com listings accepted here will be activated automatically via the Offers API.</span>
            )}
            {(job.channel === 'zalando' || job.channel === 'lazada') && (
              <span> — {chLabel(job.channel)} accepted listings will be marked <strong>requires manual publish</strong> — activate them in your Seller Centre after confirming.</span>
            )}
          </div>
        </div>
      )}

      {/* Tabs */}
      <div style={{ display: 'flex', gap: 4, marginBottom: 20, borderBottom: '1px solid var(--border)', paddingBottom: 0 }}>
        {(['current', 'history'] as PageTab[]).map(tab => (
          <button
            key={tab}
            onClick={() => switchTab(tab)}
            style={{
              padding: '8px 18px',
              border: 'none',
              borderBottom: activeTab === tab ? '2px solid var(--accent, #6366f1)' : '2px solid transparent',
              background: 'none',
              color: activeTab === tab ? 'var(--accent, #6366f1)' : 'var(--text-muted)',
              fontWeight: activeTab === tab ? 700 : 400,
              fontSize: 14,
              cursor: 'pointer',
              marginBottom: -1,
              transition: 'all 0.15s',
            }}
          >
            {tab === 'current' ? '🔄 Current Run' : '📋 History'}
          </button>
        ))}
      </div>

      {/* History tab */}
      {activeTab === 'history' && credentialID && (
        <HistoryTab credentialID={credentialID} />
      )}

      {/* Current run tab */}
      {activeTab === 'current' && (
        <>
          {/* Stats bar */}
          {job && !loading && (
            <div style={{ display: 'flex', gap: 12, marginBottom: 20, flexWrap: 'wrap' }}>
              {[
                { label: 'Total', value: job.total_rows, color: 'var(--text)' },
                { label: 'Full match', value: job.full_matches, color: '#22c55e' },
                { label: 'Partial', value: job.partial, color: '#f59e0b' },
                { label: 'Unmatched', value: job.unmatched, color: '#94a3b8' },
                { label: 'Decided', value: decidedCount, color: 'var(--accent, #6366f1)' },
              ].map(s => (
                <div key={s.label} style={{ padding: '10px 16px', background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 8, textAlign: 'center', minWidth: 80 }}>
                  <div style={{ fontSize: 22, fontWeight: 800, color: s.color }}>{s.value}</div>
                  <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 1 }}>{s.label}</div>
                </div>
              ))}
              {/* Push summary if available */}
              {job.status === 'confirmed' && (job.push_total ?? 0) > 0 && (
                <>
                  <div style={{ width: 1, background: 'var(--border)', margin: '0 4px' }} />
                  {[
                    { label: 'Pushed live', value: job.push_succeeded ?? 0, color: '#22c55e' },
                    { label: 'Push failed', value: job.push_failed ?? 0, color: '#ef4444' },
                    { label: 'Manual req.', value: job.push_manual_required ?? 0, color: '#f59e0b' },
                  ].map(s => (
                    <div key={s.label} style={{ padding: '10px 16px', background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 8, textAlign: 'center', minWidth: 80 }}>
                      <div style={{ fontSize: 22, fontWeight: 800, color: s.color }}>{s.value}</div>
                      <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 1 }}>{s.label}</div>
                    </div>
                  ))}
                </>
              )}
            </div>
          )}

          {/* Action bar */}
          <div style={{ display: 'flex', gap: 10, marginBottom: 20, alignItems: 'center', flexWrap: 'wrap' }}>
            <button
              onClick={handleRunAutoLink}
              disabled={running}
              style={{ padding: '8px 16px', borderRadius: 8, border: 'none', background: 'var(--accent, #6366f1)', color: '#fff', fontWeight: 600, fontSize: 13, cursor: running ? 'not-allowed' : 'pointer', opacity: running ? 0.7 : 1 }}
            >
              {running ? '⏳ Matching…' : job ? '🔄 Re-run Auto-link' : '🔗 Run Auto-link'}
            </button>

            {job && job.status === 'complete' && (
              <>
                <button
                  onClick={handleConfirm}
                  disabled={confirming || decidedCount === 0}
                  style={{ padding: '8px 16px', borderRadius: 8, border: 'none', background: decidedCount > 0 ? '#22c55e' : 'var(--surface-2)', color: decidedCount > 0 ? '#fff' : 'var(--text-muted)', fontWeight: 600, fontSize: 13, cursor: decidedCount > 0 ? 'pointer' : 'not-allowed', opacity: confirming ? 0.7 : 1 }}
                >
                  {confirming ? '⏳ Saving…' : `✅ Confirm ${decidedCount} decision${decidedCount !== 1 ? 's' : ''}`}
                </button>

                <button
                  onClick={handleExport}
                  disabled={exporting}
                  style={{ padding: '8px 16px', borderRadius: 8, border: '1px solid var(--border)', background: 'var(--surface)', color: 'var(--text)', fontSize: 13, cursor: 'pointer' }}
                >
                  {exporting ? '⏳…' : '⬇ Export unmatched (.xlsx)'}
                </button>

                <label style={{ padding: '8px 16px', borderRadius: 8, border: '1px solid var(--border)', background: 'var(--surface)', color: 'var(--text)', fontSize: 13, cursor: 'pointer' }}>
                  {importUploading ? '⏳ Importing…' : '⬆ Re-upload resolutions'}
                  <input type="file" accept=".xlsx" style={{ display: 'none' }} onChange={e => e.target.files?.[0] && handleImportResolutions(e.target.files[0])} />
                </label>
              </>
            )}

            {/* Filter tabs */}
            <div style={{ marginLeft: 'auto', display: 'flex', gap: 4 }}>
              {(['all', 'full', 'partial', 'none'] as const).map(f => (
                <button key={f} onClick={() => setFilter(f)} style={{
                  padding: '6px 12px', borderRadius: 6, border: '1px solid var(--border)', fontSize: 12, fontWeight: 500, cursor: 'pointer',
                  background: filter === f ? 'var(--accent, #6366f1)' : 'var(--surface)',
                  color: filter === f ? '#fff' : 'var(--text)',
                }}>
                  {f === 'all' ? `All (${rows.length})` : f === 'full' ? `✓ Full (${job?.full_matches ?? 0})` : f === 'partial' ? `~ Partial (${job?.partial ?? 0})` : `✕ Unmatched (${job?.unmatched ?? 0})`}
                </button>
              ))}
            </div>
          </div>

          {/* Content */}
          {loading ? (
            <div style={{ textAlign: 'center', padding: 60, color: 'var(--text-muted)' }}>
              {running ? '⏳ Fetching channel listings and matching SKUs…' : 'Loading…'}
            </div>
          ) : !job ? (
            <div style={{ textAlign: 'center', padding: 60, background: 'var(--surface)', borderRadius: 12, border: '1px solid var(--border)' }}>
              <div style={{ fontSize: 40, marginBottom: 12 }}>🔗</div>
              <h3 style={{ margin: '0 0 8px' }}>No reconciliation run yet</h3>
              <p style={{ color: 'var(--text-muted)', margin: '0 0 20px' }}>Click "Run Auto-link" to fetch your channel listings and match them against your internal catalogue.</p>
              <button onClick={handleRunAutoLink} style={{ padding: '10px 22px', borderRadius: 8, border: 'none', background: 'var(--accent, #6366f1)', color: '#fff', fontWeight: 700, fontSize: 14, cursor: 'pointer' }}>
                🔗 Run Auto-link
              </button>
            </div>
          ) : job.status === 'running' ? (
            <div style={{ textAlign: 'center', padding: 60, color: 'var(--text-muted)' }}>
              <div style={{ fontSize: 40, marginBottom: 12 }}>⏳</div>
              <p>Fetching listings and matching SKUs… this may take a moment.</p>
            </div>
          ) : visibleRows.length === 0 ? (
            <div style={{ textAlign: 'center', padding: 40, color: 'var(--text-muted)', background: 'var(--surface)', borderRadius: 12, border: '1px solid var(--border)' }}>
              No rows match the current filter.
            </div>
          ) : (
            <div>
              {undecidedCount > 0 && filter === 'all' && (
                <div style={{ marginBottom: 12, padding: '8px 14px', background: '#f59e0b18', borderRadius: 8, fontSize: 13, color: '#92400e', fontWeight: 500, display: 'flex', alignItems: 'center', gap: 8 }}>
                  ⚠️ {undecidedCount} row{undecidedCount !== 1 ? 's' : ''} still need a decision (partial or unmatched).
                </div>
              )}
              {visibleRows.map(row => (
                <RowCard
                  key={row.channel_sku}
                  row={row}
                  channel={job.channel}
                  onChange={updateRow}
                />
              ))}
            </div>
          )}
        </>
      )}
    </div>
  );
}

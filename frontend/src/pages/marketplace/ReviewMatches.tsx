// ============================================================================
// REVIEW MATCHES PAGE — Second Import Matching Flow
// ============================================================================
// Route: /marketplace/import/:jobId/review-matches
//
// Three tabs:
//   "Exact Matches"   — ASIN/SKU matched to existing product; user confirms or rejects
//   "Possible Matches"— Typesense fuzzy title match; user reviews with confidence score
//   "Unmatched"       — No match found; user decides to import as new or skip
//
// API endpoints (all under /marketplace/import/jobs/:id/...):
//   POST  analyze-matches     → triggers background analysis
//   GET   matches             → { match_status, results: { exact, fuzzy, unmatched } }
//   POST  matches/accept      → { row_ids, accept_all, match_type }
//   POST  matches/reject      → { row_ids }
//   POST  unmatched/import-new→ { row_ids, all }
// ============================================================================

import React, { useState, useEffect, useCallback, useRef } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { apiFetch } from '../../services/apiFetch';

// ── Types ──────────────────────────────────────────────────────────────────────

interface MatchRow {
  row_id: string;
  external_id: string;
  sku: string;
  title: string;
  image_url: string;
  price: string;
  match_type: 'exact' | 'fuzzy' | 'none';
  match_score: number;
  match_reason: string;
  matched_product_id: string;
  matched_product_title: string;
  matched_product_sku: string;
  matched_product_image: string;
  matched_product_asin: string;
  decision: '' | 'accepted' | 'rejected' | 'import_as_new';
}

interface MatchResults {
  exact: MatchRow[];
  fuzzy: MatchRow[];
  unmatched: MatchRow[];
}

type Tab = 'exact' | 'fuzzy' | 'unmatched';

// ── Helpers ────────────────────────────────────────────────────────────────────

function scoreColor(score: number): string {
  if (score >= 0.85) return '#10b981';
  if (score >= 0.65) return '#f59e0b';
  return '#ef4444';
}

function scoreLabel(score: number): string {
  if (score >= 0.85) return 'High';
  if (score >= 0.65) return 'Medium';
  return 'Low';
}

function decisionBadge(decision: string) {
  if (decision === 'accepted') return (
    <span style={{ background: '#d1fae5', color: '#065f46', padding: '2px 8px', borderRadius: 99, fontSize: 11, fontWeight: 700 }}>
      ✓ Accepted
    </span>
  );
  if (decision === 'import_as_new') return (
    <span style={{ background: '#dbeafe', color: '#1e40af', padding: '2px 8px', borderRadius: 99, fontSize: 11, fontWeight: 700 }}>
      + New Product
    </span>
  );
  if (decision === 'rejected') return (
    <span style={{ background: '#fee2e2', color: '#991b1b', padding: '2px 8px', borderRadius: 99, fontSize: 11, fontWeight: 700 }}>
      ✕ Rejected
    </span>
  );
  return null;
}

function ProductCard({ imageUrl, title, sku, asin, label, accent }: {
  imageUrl: string; title: string; sku?: string; asin?: string; label: string; accent: string;
}) {
  return (
    <div style={{ flex: 1, minWidth: 0 }}>
      <div style={{ fontSize: 10, fontWeight: 700, color: accent, textTransform: 'uppercase', letterSpacing: 1, marginBottom: 6 }}>
        {label}
      </div>
      <div style={{ display: 'flex', gap: 10, alignItems: 'center' }}>
        {imageUrl ? (
          <img src={imageUrl} alt="" style={{ width: 48, height: 48, objectFit: 'contain', borderRadius: 6, border: '1px solid var(--border)', background: '#fff', flexShrink: 0 }} />
        ) : (
          <div style={{ width: 48, height: 48, borderRadius: 6, border: '1px solid var(--border)', background: 'var(--surface-alt)', flexShrink: 0, display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 18, color: 'var(--text-muted)' }}>
            □
          </div>
        )}
        <div style={{ minWidth: 0 }}>
          <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--text)', lineHeight: 1.3, overflow: 'hidden', display: '-webkit-box', WebkitLineClamp: 2, WebkitBoxOrient: 'vertical' }}>
            {title || '—'}
          </div>
          {sku && <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 2 }}>SKU: {sku}</div>}
          {asin && <div style={{ fontSize: 11, color: 'var(--text-muted)' }}>ASIN: {asin}</div>}
        </div>
      </div>
    </div>
  );
}

// ── Row component ──────────────────────────────────────────────────────────────

function MatchRowCard({ row, selected, onToggle, onAccept, onReject, onImportNew, busy }: {
  row: MatchRow;
  selected: boolean;
  onToggle: () => void;
  onAccept: () => void;
  onReject: () => void;
  onImportNew: () => void;
  busy: boolean;
}) {
  const decided = row.decision !== '';

  return (
    <div style={{
      background: 'var(--surface)',
      border: `1px solid ${selected ? 'var(--primary, #6366f1)' : 'var(--border)'}`,
      borderRadius: 10,
      padding: '14px 16px',
      opacity: busy ? 0.6 : 1,
      transition: 'border-color 0.15s, box-shadow 0.15s',
      boxShadow: selected ? '0 0 0 3px rgba(99,102,241,0.12)' : 'none',
    }}>
      <div style={{ display: 'flex', gap: 12, alignItems: 'flex-start' }}>
        {/* Checkbox */}
        {!decided && (
          <div
            onClick={onToggle}
            style={{
              width: 18, height: 18, borderRadius: 4, border: `2px solid ${selected ? 'var(--primary, #6366f1)' : 'var(--border)'}`,
              background: selected ? 'var(--primary, #6366f1)' : 'transparent',
              cursor: 'pointer', flexShrink: 0, marginTop: 2,
              display: 'flex', alignItems: 'center', justifyContent: 'center',
            }}
          >
            {selected && <span style={{ color: '#fff', fontSize: 11, lineHeight: 1 }}>✓</span>}
          </div>
        )}
        {decided && <div style={{ width: 18, flexShrink: 0 }} />}

        {/* Content */}
        <div style={{ flex: 1, minWidth: 0 }}>
          {/* Two-column product comparison */}
          <div style={{ display: 'flex', gap: 16, alignItems: 'flex-start', flexWrap: 'wrap' }}>
            <ProductCard
              imageUrl={row.image_url}
              title={row.title}
              sku={row.sku}
              asin={row.external_id}
              label="Incoming"
              accent="#6366f1"
            />

            {(row.match_type === 'exact' || row.match_type === 'fuzzy') && (
              <>
                {/* Arrow + score */}
                <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', padding: '8px 0', flexShrink: 0 }}>
                  <div style={{ fontSize: 20, color: scoreColor(row.match_score) }}>→</div>
                  {row.match_type === 'fuzzy' && (
                    <div style={{
                      fontSize: 10, fontWeight: 700, color: scoreColor(row.match_score),
                      background: `${scoreColor(row.match_score)}18`, borderRadius: 99,
                      padding: '2px 6px', marginTop: 4, whiteSpace: 'nowrap',
                    }}>
                      {scoreLabel(row.match_score)} {Math.round(row.match_score * 100)}%
                    </div>
                  )}
                  {row.match_type === 'exact' && (
                    <div style={{ fontSize: 10, fontWeight: 700, color: '#10b981', background: '#d1fae5', borderRadius: 99, padding: '2px 6px', marginTop: 4 }}>
                      Exact
                    </div>
                  )}
                </div>

                <ProductCard
                  imageUrl={row.matched_product_image}
                  title={row.matched_product_title}
                  sku={row.matched_product_sku}
                  asin={row.matched_product_asin}
                  label="Existing Product"
                  accent="#10b981"
                />
              </>
            )}
          </div>

          {/* Match reason & decision badge */}
          <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginTop: 10, flexWrap: 'wrap' }}>
            {row.match_reason && (
              <span style={{ fontSize: 11, color: 'var(--text-muted)', background: 'var(--surface-alt)', padding: '2px 8px', borderRadius: 6 }}>
                {row.match_reason}
              </span>
            )}
            {decided && decisionBadge(row.decision)}
          </div>
        </div>

        {/* Action buttons */}
        {!decided && !busy && (
          <div style={{ display: 'flex', gap: 6, flexShrink: 0, flexDirection: 'column' }}>
            {row.match_type !== 'none' && (
              <button onClick={onAccept} style={btnStyle('#10b981')}>
                ✓ Use Existing
              </button>
            )}
            {row.match_type !== 'none' && (
              <button onClick={onReject} style={btnStyle('#6366f1')}>
                + Import New
              </button>
            )}
            {row.match_type === 'none' && (
              <button onClick={onImportNew} style={btnStyle('#6366f1')}>
                + Import as New
              </button>
            )}
          </div>
        )}
        {busy && (
          <div style={{ width: 80, display: 'flex', alignItems: 'center', justifyContent: 'center', color: 'var(--text-muted)', fontSize: 11 }}>
            saving…
          </div>
        )}
      </div>
    </div>
  );
}

function btnStyle(bg: string): React.CSSProperties {
  return {
    padding: '6px 12px', borderRadius: 6, border: 'none', background: bg, color: '#fff',
    fontSize: 12, fontWeight: 600, cursor: 'pointer', whiteSpace: 'nowrap', minWidth: 110,
  };
}

// ── Main page ──────────────────────────────────────────────────────────────────

export default function ReviewMatches() {
  const { jobId } = useParams<{ jobId: string }>();
  const navigate = useNavigate();

  const [tab, setTab] = useState<Tab>('exact');
  const [matchStatus, setMatchStatus] = useState<string>('');
  const [results, setResults] = useState<MatchResults>({ exact: [], fuzzy: [], unmatched: [] });
  const [loading, setLoading] = useState(true);
  const [analyzing, setAnalyzing] = useState(false);
  const [error, setError] = useState('');
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [busyRows, setBusyRows] = useState<Set<string>>(new Set());
  const [bulkBusy, setBulkBusy] = useState(false);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const load = useCallback(async () => {
    if (!jobId) return;
    try {
      const res = await apiFetch(`/marketplace/import/jobs/${jobId}/matches`);
      if (!res.ok) { setError('Failed to load match results'); return; }
      const data = await res.json();
      setMatchStatus(data.match_status || '');
      const r = data.results || {};
      setResults({
        exact:     Array.isArray(r.exact)     ? r.exact     : [],
        fuzzy:     Array.isArray(r.fuzzy)     ? r.fuzzy     : [],
        unmatched: Array.isArray(r.unmatched) ? r.unmatched : [],
      });
      if (data.match_status !== 'analyzing') {
        if (pollRef.current) { clearInterval(pollRef.current); pollRef.current = null; }
        setAnalyzing(false);
      }
    } catch (e) {
      setError('Network error loading matches');
    } finally {
      setLoading(false);
    }
  }, [jobId]);

  // On mount: trigger analysis then start polling
  useEffect(() => {
    if (!jobId) return;
    (async () => {
      setLoading(true);
      // Trigger analysis (idempotent — backend returns early if already done)
      try {
        const res = await apiFetch(`/marketplace/import/jobs/${jobId}/analyze-matches`, { method: 'POST' });
        if (res.ok) {
          const d = await res.json();
          if (d.match_status === 'analyzing') {
            setAnalyzing(true);
          }
        }
      } catch { /* ignore — we'll poll */ }

      await load();

      // Poll while analyzing
      pollRef.current = setInterval(load, 3000);
    })();

    return () => { if (pollRef.current) clearInterval(pollRef.current); };
  }, [jobId, load]);

  // ── Selection helpers ────────────────────────────────────────────────────────

  const currentRows: MatchRow[] = results[tab] || [];
  const undecidedRows = currentRows.filter(r => r.decision === '');
  const allUndecidedSelected = undecidedRows.length > 0 && undecidedRows.every(r => selected.has(r.row_id));

  function toggleRow(rowId: string) {
    setSelected(prev => {
      const next = new Set(prev);
      next.has(rowId) ? next.delete(rowId) : next.add(rowId);
      return next;
    });
  }

  function toggleAll() {
    if (allUndecidedSelected) {
      setSelected(prev => {
        const next = new Set(prev);
        undecidedRows.forEach(r => next.delete(r.row_id));
        return next;
      });
    } else {
      setSelected(prev => {
        const next = new Set(prev);
        undecidedRows.forEach(r => next.add(r.row_id));
        return next;
      });
    }
  }

  // ── Single-row actions ───────────────────────────────────────────────────────

  async function acceptRow(row: MatchRow) {
    setBusyRows(prev => new Set([...prev, row.row_id]));
    try {
      await apiFetch(`/marketplace/import/jobs/${jobId}/matches/accept`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ row_ids: [row.row_id] }),
      });
      await load();
      setSelected(prev => { const n = new Set(prev); n.delete(row.row_id); return n; });
    } finally {
      setBusyRows(prev => { const n = new Set(prev); n.delete(row.row_id); return n; });
    }
  }

  async function rejectRow(row: MatchRow) {
    setBusyRows(prev => new Set([...prev, row.row_id]));
    try {
      await apiFetch(`/marketplace/import/jobs/${jobId}/matches/reject`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ row_ids: [row.row_id] }),
      });
      await load();
      setSelected(prev => { const n = new Set(prev); n.delete(row.row_id); return n; });
    } finally {
      setBusyRows(prev => { const n = new Set(prev); n.delete(row.row_id); return n; });
    }
  }

  async function importNewRow(row: MatchRow) {
    setBusyRows(prev => new Set([...prev, row.row_id]));
    try {
      await apiFetch(`/marketplace/import/jobs/${jobId}/unmatched/import-new`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ row_ids: [row.row_id] }),
      });
      await load();
      setSelected(prev => { const n = new Set(prev); n.delete(row.row_id); return n; });
    } finally {
      setBusyRows(prev => { const n = new Set(prev); n.delete(row.row_id); return n; });
    }
  }

  // ── Bulk actions ─────────────────────────────────────────────────────────────

  async function bulkAccept() {
    if (selected.size === 0) return;
    setBulkBusy(true);
    try {
      await apiFetch(`/marketplace/import/jobs/${jobId}/matches/accept`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ row_ids: [...selected] }),
      });
      await load();
      setSelected(new Set());
    } finally { setBulkBusy(false); }
  }

  async function bulkReject() {
    if (selected.size === 0) return;
    setBulkBusy(true);
    try {
      await apiFetch(`/marketplace/import/jobs/${jobId}/matches/reject`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ row_ids: [...selected] }),
      });
      await load();
      setSelected(new Set());
    } finally { setBulkBusy(false); }
  }

  async function acceptAllExact() {
    setBulkBusy(true);
    try {
      await apiFetch(`/marketplace/import/jobs/${jobId}/matches/accept`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ accept_all: true, match_type: 'exact' }),
      });
      await load();
    } finally { setBulkBusy(false); }
  }

  async function importAllUnmatched() {
    setBulkBusy(true);
    try {
      await apiFetch(`/marketplace/import/jobs/${jobId}/unmatched/import-new`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ all: true }),
      });
      await load();
    } finally { setBulkBusy(false); }
  }

  // ── Summary counts ───────────────────────────────────────────────────────────

  const exactTotal = results.exact.length;
  const fuzzyTotal = results.fuzzy.length;
  const unmatchedTotal = results.unmatched.length;
  const total = exactTotal + fuzzyTotal + unmatchedTotal;

  const decided = [
    ...results.exact, ...results.fuzzy, ...results.unmatched
  ].filter(r => r.decision !== '').length;

  const allDone = total > 0 && decided >= total;

  // ── Render ───────────────────────────────────────────────────────────────────

  if (loading && matchStatus === '') {
    return (
      <div style={{ padding: 40, textAlign: 'center', color: 'var(--text-muted)' }}>
        <div style={{ fontSize: 32, marginBottom: 12 }}>⏳</div>
        <div style={{ fontSize: 16, fontWeight: 600 }}>Analyzing import for matches…</div>
        <div style={{ fontSize: 13, marginTop: 6 }}>This may take a moment for large imports.</div>
      </div>
    );
  }

  if (error) {
    return (
      <div style={{ padding: 40, textAlign: 'center' }}>
        <div style={{ fontSize: 32, marginBottom: 12 }}>⚠️</div>
        <div style={{ fontSize: 16, fontWeight: 600, color: '#ef4444' }}>{error}</div>
        <button onClick={() => { setError(''); load(); }} style={{ marginTop: 16, ...btnStyle('#6366f1') }}>
          Retry
        </button>
      </div>
    );
  }

  return (
    <div style={{ padding: '24px 28px', maxWidth: 1100, margin: '0 auto' }}>
      {/* Header */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 24 }}>
        <button
          onClick={() => navigate('/marketplace/import')}
          style={{ background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-muted)', fontSize: 20, padding: 0, display: 'flex', alignItems: 'center' }}
        >
          ←
        </button>
        <div>
          <h1 style={{ margin: 0, fontSize: 22, fontWeight: 700, color: 'var(--text)' }}>
            Review Import Matches
          </h1>
          <div style={{ fontSize: 13, color: 'var(--text-muted)', marginTop: 2 }}>
            Job ID: <code style={{ fontSize: 12 }}>{jobId}</code>
          </div>
        </div>
        <div style={{ marginLeft: 'auto', display: 'flex', alignItems: 'center', gap: 10 }}>
          {analyzing && (
            <span style={{ fontSize: 12, color: '#f59e0b', background: '#fef3c7', padding: '4px 10px', borderRadius: 99, fontWeight: 600 }}>
              ⏳ Analyzing…
            </span>
          )}
          {allDone && (
            <span style={{ fontSize: 12, color: '#10b981', background: '#d1fae5', padding: '4px 10px', borderRadius: 99, fontWeight: 600 }}>
              ✓ All reviewed
            </span>
          )}
          {total > 0 && !allDone && !analyzing && (
            <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
              {decided} / {total} reviewed
            </span>
          )}
        </div>
      </div>

      {/* No matches needed */}
      {!analyzing && total === 0 && matchStatus === 'no_review_needed' && (
        <div style={{ textAlign: 'center', padding: 60, background: 'var(--surface)', borderRadius: 12, border: '1px solid var(--border)' }}>
          <div style={{ fontSize: 40, marginBottom: 12 }}>✅</div>
          <div style={{ fontSize: 18, fontWeight: 700, color: 'var(--text)' }}>No duplicates found</div>
          <div style={{ fontSize: 13, color: 'var(--text-muted)', marginTop: 6 }}>
            All imported products appear to be new. No review needed.
          </div>
          <button onClick={() => navigate('/marketplace/import')} style={{ marginTop: 20, ...btnStyle('#6366f1') }}>
            Back to Import Dashboard
          </button>
        </div>
      )}

      {total > 0 && (
        <>
          {/* Tab bar */}
          <div style={{ display: 'flex', gap: 0, borderBottom: '1px solid var(--border)', marginBottom: 20 }}>
            {([
              { key: 'exact', label: 'Exact Matches', count: exactTotal, dot: '#10b981' },
              { key: 'fuzzy', label: 'Possible Matches', count: fuzzyTotal, dot: '#f59e0b' },
              { key: 'unmatched', label: 'Unmatched', count: unmatchedTotal, dot: '#6366f1' },
            ] as const).map(t => (
              <button
                key={t.key}
                onClick={() => { setTab(t.key); setSelected(new Set()); }}
                style={{
                  padding: '10px 20px',
                  border: 'none',
                  borderBottom: tab === t.key ? '2px solid var(--primary, #6366f1)' : '2px solid transparent',
                  background: 'none',
                  cursor: 'pointer',
                  color: tab === t.key ? 'var(--primary, #6366f1)' : 'var(--text-muted)',
                  fontWeight: tab === t.key ? 700 : 500,
                  fontSize: 14,
                  display: 'flex',
                  alignItems: 'center',
                  gap: 8,
                }}
              >
                {t.label}
                <span style={{
                  background: t.count === 0 ? 'var(--surface-alt)' : t.dot + '22',
                  color: t.count === 0 ? 'var(--text-muted)' : t.dot,
                  borderRadius: 99,
                  padding: '1px 7px',
                  fontSize: 12,
                  fontWeight: 700,
                }}>
                  {t.count}
                </span>
              </button>
            ))}
          </div>

          {/* Tab description */}
          <div style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 16 }}>
            {tab === 'exact' && 'These products matched by ASIN or SKU to an existing product. Confirm to link, or reject to import as a new product.'}
            {tab === 'fuzzy' && 'These products were matched by title similarity. Review the confidence score and decide whether to link or import as new.'}
            {tab === 'unmatched' && 'No existing product was found for these items. Choose to import them as new products, or skip.'}
          </div>

          {/* Bulk actions bar */}
          {undecidedRows.length > 0 && (
            <div style={{
              display: 'flex', alignItems: 'center', gap: 10, padding: '10px 14px',
              background: 'var(--surface)', border: '1px solid var(--border)',
              borderRadius: 8, marginBottom: 14, flexWrap: 'wrap',
            }}>
              <label style={{ display: 'flex', alignItems: 'center', gap: 7, cursor: 'pointer', fontSize: 13, fontWeight: 600, color: 'var(--text)' }}>
                <input
                  type="checkbox"
                  checked={allUndecidedSelected}
                  onChange={toggleAll}
                  style={{ width: 15, height: 15, cursor: 'pointer' }}
                />
                {allUndecidedSelected ? 'Deselect all' : `Select all (${undecidedRows.length})`}
              </label>

              {selected.size > 0 && (
                <span style={{ fontSize: 12, color: 'var(--text-muted)', marginLeft: 4 }}>
                  {selected.size} selected
                </span>
              )}

              <div style={{ marginLeft: 'auto', display: 'flex', gap: 8 }}>
                {tab !== 'unmatched' && selected.size > 0 && (
                  <>
                    <button
                      onClick={bulkAccept}
                      disabled={bulkBusy}
                      style={{ ...btnStyle('#10b981'), opacity: bulkBusy ? 0.5 : 1 }}
                    >
                      ✓ Use Existing ({selected.size})
                    </button>
                    <button
                      onClick={bulkReject}
                      disabled={bulkBusy}
                      style={{ ...btnStyle('#6366f1'), opacity: bulkBusy ? 0.5 : 1 }}
                    >
                      + Import New ({selected.size})
                    </button>
                  </>
                )}
                {tab === 'exact' && selected.size === 0 && undecidedRows.length > 0 && (
                  <button
                    onClick={acceptAllExact}
                    disabled={bulkBusy}
                    style={{ ...btnStyle('#10b981'), opacity: bulkBusy ? 0.5 : 1 }}
                  >
                    ✓ Accept All Exact
                  </button>
                )}
                {tab === 'unmatched' && selected.size === 0 && undecidedRows.length > 0 && (
                  <button
                    onClick={importAllUnmatched}
                    disabled={bulkBusy}
                    style={{ ...btnStyle('#6366f1'), opacity: bulkBusy ? 0.5 : 1 }}
                  >
                    + Import All as New
                  </button>
                )}
                {tab === 'unmatched' && selected.size > 0 && (
                  <button
                    onClick={bulkReject}
                    disabled={bulkBusy}
                    style={{ ...btnStyle('#6366f1'), opacity: bulkBusy ? 0.5 : 1 }}
                  >
                    + Import Selected as New ({selected.size})
                  </button>
                )}
              </div>
            </div>
          )}

          {/* Rows */}
          {currentRows.length === 0 ? (
            <div style={{ textAlign: 'center', padding: 48, color: 'var(--text-muted)', fontSize: 14 }}>
              {analyzing ? 'Analyzing…' : 'No items in this category.'}
            </div>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
              {currentRows.map(row => (
                <MatchRowCard
                  key={row.row_id}
                  row={row}
                  selected={selected.has(row.row_id)}
                  onToggle={() => toggleRow(row.row_id)}
                  onAccept={() => acceptRow(row)}
                  onReject={() => rejectRow(row)}
                  onImportNew={() => importNewRow(row)}
                  busy={busyRows.has(row.row_id) || bulkBusy}
                />
              ))}
            </div>
          )}

          {/* Footer summary */}
          {allDone && (
            <div style={{ marginTop: 24, padding: '16px 20px', background: '#d1fae5', borderRadius: 10, display: 'flex', alignItems: 'center', gap: 14 }}>
              <span style={{ fontSize: 24 }}>✅</span>
              <div>
                <div style={{ fontWeight: 700, color: '#065f46', fontSize: 15 }}>All matches reviewed!</div>
                <div style={{ fontSize: 13, color: '#047857', marginTop: 2 }}>
                  {[...results.exact, ...results.fuzzy, ...results.unmatched].filter(r => r.decision === 'accepted').length} linked to existing products,{' '}
                  {[...results.exact, ...results.fuzzy, ...results.unmatched].filter(r => r.decision === 'import_as_new').length} imported as new.
                </div>
              </div>
              <button
                onClick={() => navigate('/marketplace/import')}
                style={{ marginLeft: 'auto', ...btnStyle('#10b981') }}
              >
                Done
              </button>
            </div>
          )}
        </>
      )}
    </div>
  );
}

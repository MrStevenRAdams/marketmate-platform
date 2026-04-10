// ============================================================================
// UsageDashboard — SEO & keyword credit usage panel
// ============================================================================
// Session 11. Rendered inside BillingSettings.tsx as the "SEO & Keywords" tab.
// Fetches from the two new Session 11 endpoints:
//   GET /api/v1/billing/usage-summary    → period totals + credit_breakdown
//   GET /api/v1/billing/audit-log        → paginated usage_events (DESC)
//
// These endpoints read tenants/{id}/usage_events, populated by
// instrumentation.LogUsageEvent throughout Sessions 1–10. They are distinct
// from the old /billing/usage and /billing/audit which read credit_ledger
// and audit_log respectively.

import React, { useState, useEffect, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { CreditBreakdownChart, BreakdownEntry } from './CreditBreakdownChart';

const API = (import.meta as any).env?.VITE_API_URL || 'http://localhost:8080/api/v1';

// ── Types ─────────────────────────────────────────────────────────────────────

interface UsageSummary {
  period_start: string;
  period_end: string;
  credits_used: number;
  credits_remaining?: number | null;
  credit_breakdown: Record<string, BreakdownEntry>;
}

interface AuditEvent {
  id: string;
  timestamp: string;
  event_type: string;
  credit_cost: number;
  product_id?: string;
  listing_id?: string;
  data_source?: string;
  metadata?: Record<string, string>;
}

interface AuditLog {
  events: AuditEvent[];
  next_cursor?: string;
}

type Period = 'current_month' | 'last_month' | 'last_30' | 'last_90';

const PERIOD_LABELS: Record<Period, string> = {
  current_month: 'This month',
  last_month:    'Last month',
  last_30:       'Last 30 days',
  last_90:       'Last 90 days',
};

// ── Human-readable labels (shared with CreditBreakdownChart) ──────────────────
const LABEL_MAP: Record<string, string> = {
  ai_listing_optimise:             'AI listing optimisation',
  ai_keyword_reanalysis:           'AI keyword re-analysis',
  dataforseo_asin_refresh:         'Market data refresh',
  dataforseo_competitor_lookup:    'Competitor analysis',
  dataforseo_asin_lookup:          'Market data (platform)',
  amazon_catalog_extract:          'Catalog enrichment (platform)',
  amazon_ads_kw_recommendations:   'Ads keyword recommendations (platform)',
  brand_analytics_pull:            'Brand Analytics sync (platform)',
  seo_score_calculation:           'SEO score calculation (platform)',
};

function humanLabel(eventType: string): string {
  return LABEL_MAP[eventType] ?? eventType.replace(/_/g, ' ');
}

function fmt(iso: string): string {
  const d = new Date(iso);
  return d.toLocaleDateString('en-GB', { day: 'numeric', month: 'short' }) +
    ' ' + d.toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit' });
}

// ── Skeleton tiles ────────────────────────────────────────────────────────────
function TileSkeleton() {
  return (
    <div style={{
      flex: 1, padding: '18px 24px', borderRadius: 10,
      background: 'var(--bg-secondary)', border: '1px solid var(--border-color)',
    }}>
      <div style={{ height: 13, width: 100, background: 'var(--bg-tertiary)', borderRadius: 4, marginBottom: 10 }} />
      <div style={{ height: 32, width: 70, background: 'var(--bg-tertiary)', borderRadius: 6 }} />
    </div>
  );
}

// ── Error banner ──────────────────────────────────────────────────────────────
function ErrorBanner({ msg }: { msg: string }) {
  return (
    <div style={{
      padding: '14px 18px', borderRadius: 8,
      background: 'var(--danger-glow, #fee2e2)',
      color: 'var(--danger, #dc2626)',
      fontSize: 13, border: '1px solid var(--danger, #dc2626)30',
    }}>
      ⚠️ {msg}
    </div>
  );
}

// ── Main component ────────────────────────────────────────────────────────────
export function UsageDashboard() {
  const navigate = useNavigate();
  const [period, setPeriod] = useState<Period>('current_month');

  const [summary, setSummary] = useState<UsageSummary | null>(null);
  const [summaryLoading, setSummaryLoading] = useState(true);
  const [summaryError, setSummaryError] = useState<string | null>(null);

  const [auditEvents, setAuditEvents] = useState<AuditEvent[]>([]);
  const [auditLoading, setAuditLoading] = useState(true);
  const [auditError, setAuditError] = useState<string | null>(null);
  const [nextCursor, setNextCursor] = useState<string | undefined>(undefined);
  const [loadingMore, setLoadingMore] = useState(false);

  const tenantId = localStorage.getItem('marketmate_active_tenant') ?? '';
  const headers = { 'Content-Type': 'application/json', 'X-Tenant-Id': tenantId };

  // ── Fetch summary ──────────────────────────────────────────────────────────
  const fetchSummary = useCallback(async () => {
    setSummaryLoading(true);
    setSummaryError(null);
    try {
      const res = await fetch(`${API}/billing/usage-summary?period=${period}`, { headers });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data: UsageSummary = await res.json();
      setSummary(data);
    } catch (e: any) {
      setSummaryError('Failed to load usage summary — ' + (e?.message ?? 'network error'));
    } finally {
      setSummaryLoading(false);
    }
  }, [period, tenantId]); // eslint-disable-line react-hooks/exhaustive-deps

  // ── Fetch audit log (first page) ──────────────────────────────────────────
  const fetchAudit = useCallback(async () => {
    setAuditLoading(true);
    setAuditError(null);
    setAuditEvents([]);
    setNextCursor(undefined);
    try {
      const res = await fetch(`${API}/billing/audit-log?limit=50`, { headers });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data: AuditLog = await res.json();
      setAuditEvents(data.events ?? []);
      setNextCursor(data.next_cursor);
    } catch (e: any) {
      setAuditError('Failed to load event history — ' + (e?.message ?? 'network error'));
    } finally {
      setAuditLoading(false);
    }
  }, [tenantId]); // eslint-disable-line react-hooks/exhaustive-deps

  // ── Load more (cursor pagination) ─────────────────────────────────────────
  async function loadMore() {
    if (!nextCursor || loadingMore) return;
    setLoadingMore(true);
    try {
      const url = `${API}/billing/audit-log?limit=50&before=${encodeURIComponent(nextCursor)}`;
      const res = await fetch(url, { headers });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data: AuditLog = await res.json();
      setAuditEvents(prev => [...prev, ...(data.events ?? [])]);
      setNextCursor(data.next_cursor);
    } catch {
      // non-fatal — button stays visible for retry
    } finally {
      setLoadingMore(false);
    }
  }

  useEffect(() => { fetchSummary(); }, [fetchSummary]);
  useEffect(() => { fetchAudit(); }, [fetchAudit]);

  // ── Credit balance tile values ─────────────────────────────────────────────
  const creditsUsed = summary?.credits_used ?? 0;
  const creditsRemaining = summary?.credits_remaining ?? null;
  const PLAN_ALLOCATION = creditsRemaining != null ? creditsUsed + creditsRemaining : null;
  const usedPct = PLAN_ALLOCATION ? (creditsUsed / PLAN_ALLOCATION) * 100 : 0;

  const usedColour = usedPct > 80 ? '#854F0B' : 'var(--text-primary)';
  const remainColour = creditsRemaining != null && creditsRemaining < 10 ? '#993C1D' : 'var(--text-primary)';

  // ── Platform absorbed count ────────────────────────────────────────────────
  const platformAbsorbed = summary?.credit_breakdown?.['platform_absorbed'];

  // ── Reference table rows ───────────────────────────────────────────────────
  const tableRows = summary
    ? Object.entries(summary.credit_breakdown)
        .filter(([key]) => key !== 'platform_absorbed')
        .sort(([, a], [, b]) => b.credits - a.credits)
    : [];

  // ─────────────────────────────────────────────────────────────────────────
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 24 }}>

      {/* ── Section 1 — Credit balance summary ─────────────────────────── */}
      <div style={{
        background: 'var(--bg-secondary)',
        border: '1px solid var(--border-color)',
        borderRadius: 12,
        padding: '20px 24px',
      }}>
        <div style={{
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          marginBottom: 16,
          flexWrap: 'wrap',
          gap: 12,
        }}>
          <div style={{ fontSize: 15, fontWeight: 700, color: 'var(--text-primary)' }}>
            SEO &amp; keyword credit usage
          </div>

          {/* Period selector */}
          <select
            value={period}
            onChange={e => setPeriod(e.target.value as Period)}
            style={{
              fontSize: 13,
              padding: '5px 10px',
              borderRadius: 6,
              border: '1px solid var(--border-color)',
              background: 'var(--bg-primary)',
              color: 'var(--text-primary)',
              cursor: 'pointer',
            }}
          >
            {(Object.keys(PERIOD_LABELS) as Period[]).map(p => (
              <option key={p} value={p}>{PERIOD_LABELS[p]}</option>
            ))}
          </select>
        </div>

        {summaryError && <ErrorBanner msg={summaryError} />}

        {/* Metric tiles */}
        <div style={{ display: 'flex', gap: 16, flexWrap: 'wrap' }}>
          {summaryLoading ? (
            <><TileSkeleton /><TileSkeleton /></>
          ) : (
            <>
              <div style={{
                flex: 1, minWidth: 140,
                padding: '16px 20px',
                borderRadius: 10,
                background: 'var(--bg-primary)',
                border: '1px solid var(--border-color)',
              }}>
                <div style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 6 }}>
                  Credits used this period
                </div>
                <div style={{ fontSize: 28, fontWeight: 800, color: usedColour, lineHeight: 1 }}>
                  {creditsUsed.toFixed(1)}
                </div>
                {PLAN_ALLOCATION && (
                  <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 4 }}>
                    {Math.round(usedPct)}% of {PLAN_ALLOCATION.toFixed(0)} allocated
                  </div>
                )}
              </div>

              {creditsRemaining != null && (
                <div style={{
                  flex: 1, minWidth: 140,
                  padding: '16px 20px',
                  borderRadius: 10,
                  background: 'var(--bg-primary)',
                  border: '1px solid var(--border-color)',
                }}>
                  <div style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 6 }}>
                    Credits remaining
                  </div>
                  <div style={{ fontSize: 28, fontWeight: 800, color: remainColour, lineHeight: 1 }}>
                    {creditsRemaining.toFixed(1)}
                  </div>
                  {creditsRemaining < 10 && (
                    <div style={{ fontSize: 11, color: '#993C1D', marginTop: 4, fontWeight: 600 }}>
                      Running low —{' '}
                      <a href="/settings/billing" style={{ color: 'inherit', textDecoration: 'underline' }}>
                        top up →
                      </a>
                    </div>
                  )}
                </div>
              )}
            </>
          )}
        </div>
      </div>

      {/* ── Section 2 — Usage breakdown chart ──────────────────────────── */}
      <div style={{
        background: 'var(--bg-secondary)',
        border: '1px solid var(--border-color)',
        borderRadius: 12,
        padding: '20px 24px',
      }}>
        <div style={{ fontSize: 15, fontWeight: 700, color: 'var(--text-primary)', marginBottom: 16 }}>
          Credit spend by operation
        </div>

        {summaryError ? (
          <ErrorBanner msg={summaryError} />
        ) : (
          <CreditBreakdownChart
            breakdown={summary?.credit_breakdown ?? {}}
            isLoading={summaryLoading}
          />
        )}

        {/* Reference table */}
        {!summaryLoading && !summaryError && (tableRows.length > 0 || platformAbsorbed) && (
          <div style={{ marginTop: 20 }}>
            <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12 }}>
              <thead>
                <tr style={{ borderBottom: '1px solid var(--border-color)' }}>
                  {['Operation', 'Count', 'Credits'].map(h => (
                    <th key={h} style={{
                      textAlign: h === 'Operation' ? 'left' : 'right',
                      padding: '6px 8px',
                      color: 'var(--text-muted)',
                      fontWeight: 600,
                    }}>{h}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {tableRows.map(([et, { count, credits }]) => (
                  <tr key={et} style={{ borderBottom: '1px solid var(--border-color)20' }}>
                    <td style={{ padding: '7px 8px', color: 'var(--text-secondary)' }}>
                      {humanLabel(et)}
                    </td>
                    <td style={{ padding: '7px 8px', textAlign: 'right', color: 'var(--text-secondary)' }}>
                      {count}
                    </td>
                    <td style={{ padding: '7px 8px', textAlign: 'right', fontWeight: 700, color: 'var(--text-primary)' }}>
                      {credits.toFixed(2)}
                    </td>
                  </tr>
                ))}
                {platformAbsorbed && (
                  <tr style={{ borderBottom: '1px solid var(--border-color)20' }}>
                    <td style={{ padding: '7px 8px', color: 'var(--text-muted)', fontStyle: 'italic' }}>
                      Platform operations (free — Marketmate absorbs cost)
                    </td>
                    <td style={{ padding: '7px 8px', textAlign: 'right', color: 'var(--text-muted)' }}>
                      {platformAbsorbed.count}
                    </td>
                    <td style={{ padding: '7px 8px', textAlign: 'right' }}>
                      <span style={{
                        display: 'inline-block',
                        padding: '2px 7px',
                        borderRadius: 10,
                        background: 'var(--bg-tertiary)',
                        color: 'var(--text-muted)',
                        fontSize: 11,
                        fontWeight: 600,
                      }}>Free</span>
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {/* ── Section 3 — Event history table ────────────────────────────── */}
      <div style={{
        background: 'var(--bg-secondary)',
        border: '1px solid var(--border-color)',
        borderRadius: 12,
        padding: '20px 24px',
      }}>
        <div style={{ fontSize: 15, fontWeight: 700, color: 'var(--text-primary)', marginBottom: 16 }}>
          Event history
        </div>

        {auditError && <ErrorBanner msg={auditError} />}

        {auditLoading ? (
          <div style={{ color: 'var(--text-muted)', fontSize: 13, padding: '16px 0' }}>
            Loading events…
          </div>
        ) : auditEvents.length === 0 && !auditError ? (
          <div style={{ color: 'var(--text-muted)', fontSize: 13, padding: '16px 0', textAlign: 'center' }}>
            No SEO / keyword events recorded yet.
          </div>
        ) : (
          <>
            {/* Table header */}
            <div style={{
              display: 'grid',
              gridTemplateColumns: '160px 1fr 1fr 80px',
              gap: 8,
              padding: '0 4px 8px',
              borderBottom: '1px solid var(--border-color)',
              fontSize: 11,
              fontWeight: 700,
              color: 'var(--text-muted)',
              textTransform: 'uppercase',
              letterSpacing: '0.04em',
            }}>
              <div>Date / Time</div>
              <div>Operation</div>
              <div>Listing</div>
              <div style={{ textAlign: 'right' }}>Credits</div>
            </div>

            {/* Rows */}
            {auditEvents.map(ev => (
              <div
                key={ev.id}
                style={{
                  display: 'grid',
                  gridTemplateColumns: '160px 1fr 1fr 80px',
                  gap: 8,
                  padding: '9px 4px',
                  borderBottom: '1px solid var(--border-color)20',
                  fontSize: 12,
                  alignItems: 'center',
                }}
              >
                <div style={{ color: 'var(--text-muted)', fontVariantNumeric: 'tabular-nums' }}>
                  {fmt(ev.timestamp)}
                </div>
                <div style={{ color: 'var(--text-secondary)' }}>
                  {humanLabel(ev.event_type)}
                </div>
                <div>
                  {ev.listing_id ? (
                    <span
                      onClick={() => navigate(`/marketplace/listings/${ev.listing_id}?tab=seo`)}
                      style={{
                        fontSize: 11,
                        fontFamily: 'monospace',
                        color: 'var(--primary)',
                        cursor: 'pointer',
                        textDecoration: 'underline',
                      }}
                    >
                      {ev.listing_id.slice(0, 12)}…
                    </span>
                  ) : ev.product_id ? (
                    <span style={{ fontSize: 11, fontFamily: 'monospace', color: 'var(--text-muted)' }}>
                      {ev.product_id.slice(0, 12)}…
                    </span>
                  ) : (
                    <span style={{ color: 'var(--text-muted)' }}>—</span>
                  )}
                </div>
                <div style={{ textAlign: 'right' }}>
                  {ev.credit_cost === 0 ? (
                    <span style={{
                      display: 'inline-block',
                      padding: '2px 7px',
                      borderRadius: 10,
                      background: 'var(--bg-tertiary)',
                      color: 'var(--text-muted)',
                      fontSize: 11,
                      fontWeight: 600,
                    }}>Free</span>
                  ) : (
                    <span style={{ fontWeight: 700, color: 'var(--text-primary)' }}>
                      {ev.credit_cost.toFixed(2)}
                    </span>
                  )}
                </div>
              </div>
            ))}

            {/* Load more */}
            {nextCursor && (
              <div style={{ textAlign: 'center', paddingTop: 16 }}>
                <button
                  onClick={loadMore}
                  disabled={loadingMore}
                  style={{
                    padding: '8px 20px',
                    fontSize: 13,
                    borderRadius: 6,
                    border: '1px solid var(--border-color)',
                    background: 'var(--bg-primary)',
                    color: 'var(--text-secondary)',
                    cursor: loadingMore ? 'not-allowed' : 'pointer',
                    opacity: loadingMore ? 0.6 : 1,
                  }}
                >
                  {loadingMore ? 'Loading…' : 'Load more'}
                </button>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}

export default UsageDashboard;

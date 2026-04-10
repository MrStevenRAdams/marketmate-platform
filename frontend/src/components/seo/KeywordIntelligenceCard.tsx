// ============================================================================
// KeywordIntelligenceCard — keyword data status + ingestion UI for product page
// ============================================================================
//
// Session 9 changes:
//  • Layer 4 amber banner: shown when source_layer === 'ai' AND product has an
//    ASIN. Prompts seller to do a competitor ASIN lookup to get real data.
//  • 402 error messages now include a "Buy credits →" link to /settings/billing
//    instead of plain text, matching the BulkOptimiseModal pattern.
//  • handleAIRefresh no longer passes ?asin= when the product has no ASIN
//    (forces the backend into the Layer 4 RefreshFromAI branch at 0.5 credits).
//    When the product has no ASIN, it passes ?title= and ?category= instead so
//    the backend can fetch them for the AI prompt.

import React, { useState, useEffect, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { getActiveTenantId } from '../../contexts/TenantContext';
import { LayerStatusBadge, DataLayer } from './LayerStatusBadge';
import { CSVUploader } from './CSVUploader';

const API_BASE = (import.meta as any).env?.VITE_API_URL || 'http://localhost:8080/api/v1';

export interface KeywordIntelligenceCardProps {
  productId: string;
  asin: string | null;
  title?: string;
  category?: string;
}

interface KeywordEntry {
  keyword: string;
  search_volume?: number;
  score?: number;
  source_layer?: string;
}

interface KeywordIntelligenceData {
  cache_key: string;
  keywords: KeywordEntry[];
  source_layer: string;
  last_refreshed: string | null;
  category?: string;
}

type TabKey = 'upload' | 'competitor' | 'ai' | 'refresh';

const TABS: { key: TabKey; label: string }[] = [
  { key: 'upload', label: 'Upload data' },
  { key: 'competitor', label: 'Competitor analysis' },
  { key: 'ai', label: 'AI analysis' },
  { key: 'refresh', label: 'Data refresh' },
];

function apiHeaders() {
  return { 'X-Tenant-Id': getActiveTenantId(), 'Content-Type': 'application/json' };
}

function formatDate(d: string | null): string {
  if (!d) return '—';
  try {
    return new Date(d).toLocaleDateString('en-GB', {
      day: '2-digit', month: 'short', year: 'numeric',
    });
  } catch { return d; }
}

function isOlderThan30Days(d: string | null): boolean {
  if (!d) return false;
  try {
    return Date.now() - new Date(d).getTime() > 30 * 24 * 60 * 60 * 1000;
  } catch { return false; }
}

// ── Layer upgrade banners ─────────────────────────────────────────────────────

function InfoBanner({
  color,
  message,
  action,
  onAction,
}: {
  color: 'teal' | 'grey' | 'amber';
  message: string;
  action?: string;
  onAction?: () => void;
}) {
  const palette = {
    teal:  { bg: 'rgba(13,148,136,0.1)',  border: 'rgba(13,148,136,0.3)',  text: '#0d9488' },
    grey:  { bg: 'var(--bg-tertiary)',     border: 'var(--border)',         text: 'var(--text-secondary)' },
    amber: { bg: 'rgba(245,158,11,0.1)',   border: 'rgba(245,158,11,0.3)',  text: '#d97706' },
  }[color];

  return (
    <div style={{
      display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12,
      padding: '10px 14px', borderRadius: 8, marginBottom: 12,
      background: palette.bg, border: `1px solid ${palette.border}`,
    }}>
      <span style={{ fontSize: 12, color: palette.text, flex: 1 }}>{message}</span>
      {action && onAction && (
        <button
          onClick={onAction}
          style={{
            padding: '4px 12px', borderRadius: 6, border: `1px solid ${palette.text}`,
            background: 'transparent', color: palette.text, fontSize: 11, fontWeight: 600,
            cursor: 'pointer', flexShrink: 0,
          }}
        >
          {action}
        </button>
      )}
    </div>
  );
}

// ── Keyword chip ──────────────────────────────────────────────────────────────

function KeywordChip({ keyword, volume }: { keyword: string; volume: number | null }) {
  return (
    <span style={{
      display: 'inline-flex', alignItems: 'center', gap: 6,
      padding: '3px 10px', borderRadius: 20,
      background: 'var(--bg-tertiary)', border: '1px solid var(--border)',
      fontSize: 11, color: 'var(--text-secondary)', whiteSpace: 'nowrap',
    }}>
      {keyword}
      <span style={{ color: 'var(--text-muted)', fontSize: 10 }}>
        {volume != null ? volume.toLocaleString() : '—'}
      </span>
    </span>
  );
}

// ── Inline 402 error with billing link ───────────────────────────────────────

function InsufficientCreditsMsg({ balance }: { balance?: number }) {
  const balText = balance != null ? ` (balance: ${balance} credits)` : '';
  return (
    <span>
      Insufficient credits{balText} —{' '}
      <a href="/settings/billing" style={{ color: 'inherit', fontWeight: 700, textDecoration: 'underline' }}>
        Buy credits →
      </a>
    </span>
  );
}

// ── Main component ────────────────────────────────────────────────────────────

export function KeywordIntelligenceCard({ productId, asin, title, category }: KeywordIntelligenceCardProps) {
  const navigate = useNavigate();
  const [data, setData] = useState<KeywordIntelligenceData | null>(null);
  const [loading, setLoading] = useState(true);
  const [activeTab, setActiveTab] = useState<TabKey>('upload');
  const [previewOpen, setPreviewOpen] = useState(false);

  // Competitor analysis state
  const [competitorAsin, setCompetitorAsin] = useState('');
  const [competitorLoading, setCompetitorLoading] = useState(false);
  const [competitorMsg, setCompetitorMsg] = useState<{ type: 'success' | 'error'; text: React.ReactNode } | null>(null);

  // AI re-analyse state
  const [aiRefreshLoading, setAiRefreshLoading] = useState(false);
  const [aiRefreshMsg, setAiRefreshMsg] = useState<{ type: 'success' | 'error'; text: React.ReactNode } | null>(null);

  // Manual refresh state
  const [refreshLoading, setRefreshLoading] = useState(false);
  const [refreshMsg, setRefreshMsg] = useState<{ type: 'success' | 'error'; text: React.ReactNode } | null>(null);

  // Upload error state
  const [uploadError, setUploadError] = useState<string | null>(null);

  const fetchData = useCallback(async () => {
    setLoading(true);
    try {
      const params = new URLSearchParams();
      if (asin) params.set('asin', asin);
      const res = await fetch(
        `${API_BASE}/products/${productId}/keyword-intelligence?${params}`,
        { headers: apiHeaders() }
      );
      if (res.ok) {
        const json = await res.json();
        // Handler wraps in { product_id, keyword_set } — unwrap
        setData(json.keyword_set ?? json);
      }
    } catch { /* non-fatal */ } finally {
      setLoading(false);
    }
  }, [productId, asin]);

  useEffect(() => { fetchData(); }, [fetchData]);

  const sourceLayer = (data?.source_layer ?? 'ai') as DataLayer;
  const keywords = data?.keywords ?? [];
  const lastRefreshed = data?.last_refreshed ?? null;
  const hasNoData = !lastRefreshed || keywords.length === 0;

  // --- Upgrade banner visibility ---

  // Layer 2: show when using catalog/AI layer AND has an ASIN (Amazon Ads
  // enrichment is possible but not yet connected).
  const showAdsUpgradeBanner =
    (sourceLayer === 'amazon_catalog' || sourceLayer === 'ai') && !!asin;

  const showBrandAnalyticsBanner =
    sourceLayer === 'amazon_ads' || sourceLayer === 'amazon_catalog' || sourceLayer === 'ai';

  const showStaleBanner =
    sourceLayer === 'dataforseo' && isOlderThan30Days(lastRefreshed);

  // Layer 4 active state: AI layer but product HAS an ASIN — real data is
  // available but hasn't been fetched. Show amber prompt to do competitor lookup.
  const showLayer4UpgradeBanner = sourceLayer === 'ai' && !!asin;

  // ── Action handlers ───────────────────────────────────────────────────────

  async function handleCompetitorAnalysis() {
    if (!competitorAsin.trim()) return;
    setCompetitorLoading(true);
    setCompetitorMsg(null);
    try {
      const res = await fetch(`${API_BASE}/products/${productId}/keyword-intelligence/ingest`, {
        method: 'POST',
        headers: apiHeaders(),
        body: JSON.stringify({ source_type: 'competitor_asin', asin: competitorAsin.trim() }),
      });
      if (res.ok) {
        setCompetitorMsg({ type: 'success', text: 'Competitor analysis complete. Keywords updated.' });
        setCompetitorAsin('');
        fetchData();
      } else if (res.status === 402) {
        const body = await res.json();
        setCompetitorMsg({ type: 'error', text: <InsufficientCreditsMsg balance={body.balance} /> });
      } else {
        const body = await res.json().catch(() => ({}));
        setCompetitorMsg({ type: 'error', text: body.error ?? 'Analysis failed' });
      }
    } catch {
      setCompetitorMsg({ type: 'error', text: 'Network error — please try again' });
    } finally {
      setCompetitorLoading(false);
    }
  }

  async function handleAIRefresh() {
    setAiRefreshLoading(true);
    setAiRefreshMsg(null);
    try {
      // Session 9: when the product has no ASIN, do NOT pass ?asin= — this
      // forces the backend into the Layer 4 RefreshFromAI branch (0.5 credits).
      // Pass title and category instead so the AI has context.
      const params = new URLSearchParams();
      if (asin) {
        params.set('asin', asin);
      } else {
        if (title) params.set('title', title);
        if (category) params.set('category', category);
      }
      const res = await fetch(
        `${API_BASE}/products/${productId}/keyword-intelligence/refresh?${params}`,
        { method: 'POST', headers: apiHeaders() }
      );
      if (res.ok) {
        setAiRefreshMsg({ type: 'success', text: 'Re-analysis complete. Keywords updated.' });
        fetchData();
      } else if (res.status === 402) {
        const body = await res.json();
        setAiRefreshMsg({ type: 'error', text: <InsufficientCreditsMsg balance={body.balance} /> });
      } else {
        const body = await res.json().catch(() => ({}));
        setAiRefreshMsg({ type: 'error', text: body.error ?? 'Re-analysis failed' });
      }
    } catch {
      setAiRefreshMsg({ type: 'error', text: 'Network error — please try again' });
    } finally {
      setAiRefreshLoading(false);
    }
  }

  async function handleForceRefresh() {
    setRefreshLoading(true);
    setRefreshMsg(null);
    try {
      const params = asin ? `?asin=${encodeURIComponent(asin)}` : '';
      const res = await fetch(`${API_BASE}/products/${productId}/keyword-intelligence/refresh${params}`, {
        method: 'POST',
        headers: apiHeaders(),
      });
      if (res.ok) {
        setRefreshMsg({ type: 'success', text: 'Refresh complete. Keyword data updated.' });
        fetchData();
      } else if (res.status === 402) {
        const body = await res.json();
        setRefreshMsg({ type: 'error', text: <InsufficientCreditsMsg balance={body.balance} /> });
      } else {
        const body = await res.json().catch(() => ({}));
        setRefreshMsg({ type: 'error', text: body.error ?? 'Refresh failed' });
      }
    } catch {
      setRefreshMsg({ type: 'error', text: 'Network error — please try again' });
    } finally {
      setRefreshLoading(false);
    }
  }

  // ── Styles ────────────────────────────────────────────────────────────────

  const card: React.CSSProperties = {
    background: 'var(--bg-secondary)',
    border: '1px solid var(--border)',
    borderRadius: 12,
    padding: 24,
    marginTop: 24,
  };

  const tabBar: React.CSSProperties = {
    display: 'flex', gap: 2,
    borderBottom: '1px solid var(--border)',
    marginBottom: 20,
  };

  const tabBtn = (active: boolean): React.CSSProperties => ({
    padding: '8px 14px', fontSize: 12, fontWeight: active ? 700 : 500,
    color: active ? 'var(--primary)' : 'var(--text-secondary)',
    background: 'none', border: 'none', cursor: 'pointer',
    borderBottom: active ? '2px solid var(--primary)' : '2px solid transparent',
    marginBottom: -1, transition: 'color 0.15s',
  });

  const msgStyle = (type: 'success' | 'error'): React.CSSProperties => ({
    fontSize: 12, padding: '8px 12px', borderRadius: 6, marginTop: 10,
    background: type === 'success' ? 'rgba(34,197,94,0.1)' : 'rgba(239,68,68,0.1)',
    color: type === 'success' ? 'var(--success, #22c55e)' : 'var(--danger, #ef4444)',
    border: `1px solid ${type === 'success' ? 'rgba(34,197,94,0.2)' : 'rgba(239,68,68,0.2)'}`,
  });

  // ── Render ────────────────────────────────────────────────────────────────

  return (
    <div style={card}>
      {/* Section title */}
      <h3 style={{ fontSize: 15, fontWeight: 700, color: 'var(--text-primary)', marginBottom: 16, paddingBottom: 10, borderBottom: '1px solid var(--border)' }}>
        Keyword Intelligence
      </h3>

      {/* Section 1 — Status header */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 16 }}>
        <LayerStatusBadge layer={sourceLayer} />
        <div style={{ fontSize: 12, color: 'var(--text-muted)', textAlign: 'right' }}>
          {hasNoData ? (
            <span>No keyword data yet</span>
          ) : (
            <>
              <div>Last updated: {formatDate(lastRefreshed)}</div>
              <div>{keywords.length} keywords indexed</div>
            </>
          )}
        </div>
      </div>

      {/* Section 2 — Upgrade banners */}

      {/* Layer 4 active state: AI layer but ASIN exists — real data available */}
      {showLayer4UpgradeBanner && (
        <InfoBanner
          color="amber"
          message="Upgrade to real market data — competitor ASIN lookup from 0.5 credits. Your product has an ASIN; use it to get live search volume and competitor keyword data."
          action="Analyse"
          onAction={() => setActiveTab('competitor')}
        />
      )}

      {showAdsUpgradeBanner && (
        <InfoBanner
          color="teal"
          message="Connect Amazon Advertising to enhance keyword priority scores for free. No cost, just connect your Ads account."
          action="Connect"
          onAction={() => navigate('/marketplace/connections')}
        />
      )}
      {showBrandAnalyticsBanner && (
        <InfoBanner
          color="grey"
          message="Upload Amazon Brand Analytics data to unlock real purchase-based keyword rankings."
          action="Upload"
          onAction={() => setActiveTab('upload')}
        />
      )}
      {showStaleBanner && (
        <InfoBanner
          color="amber"
          message="Keyword data is over 30 days old."
          action="Refresh (0.25 credits)"
          onAction={() => setActiveTab('refresh')}
        />
      )}

      {/* Section 3 — Tabs */}
      <div style={tabBar}>
        {TABS.map(t => (
          <button key={t.key} style={tabBtn(activeTab === t.key)} onClick={() => setActiveTab(t.key)}>
            {t.label}
          </button>
        ))}
      </div>

      {/* Tab: Upload data */}
      {activeTab === 'upload' && (
        <div>
          {uploadError && (
            <div style={msgStyle('error')}>{uploadError}</div>
          )}
          <CSVUploader
            label="Amazon Brand Analytics CSV"
            sourceType="brand_analytics"
            productId={productId}
            onSuccess={() => { setUploadError(null); fetchData(); }}
            onError={setUploadError}
            formatGuideUrl="https://sellercentral.amazon.com/help/hub/reference/G202173140"
          />
          <CSVUploader
            label="eBay Terapeak CSV"
            sourceType="terapeak"
            productId={productId}
            onSuccess={() => { setUploadError(null); fetchData(); }}
            onError={setUploadError}
          />
          <CSVUploader
            label="Generic keyword list (one per line, optional volume)"
            sourceType="generic"
            productId={productId}
            onSuccess={() => { setUploadError(null); fetchData(); }}
            onError={setUploadError}
          />
        </div>
      )}

      {/* Tab: Competitor analysis */}
      {activeTab === 'competitor' && (
        <div>
          <label style={{ display: 'block', fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 6 }}>
            Enter a competitor ASIN
          </label>
          <div style={{ display: 'flex', gap: 10 }}>
            <input
              type="text"
              value={competitorAsin}
              onChange={e => setCompetitorAsin(e.target.value)}
              placeholder="e.g. B09WD2VRXF"
              disabled={competitorLoading}
              style={{
                flex: 1, padding: '9px 12px', borderRadius: 8,
                background: 'var(--bg-primary)', border: '1px solid var(--border-bright)',
                color: 'var(--text-primary)', fontSize: 13, outline: 'none',
              }}
              onKeyDown={e => e.key === 'Enter' && !competitorLoading && handleCompetitorAnalysis()}
            />
            <button
              onClick={handleCompetitorAnalysis}
              disabled={competitorLoading || !competitorAsin.trim()}
              style={{
                padding: '9px 16px', borderRadius: 8, fontSize: 12, fontWeight: 600,
                background: competitorLoading || !competitorAsin.trim() ? 'var(--bg-tertiary)' : 'var(--primary)',
                color: competitorLoading || !competitorAsin.trim() ? 'var(--text-muted)' : 'white',
                border: 'none', cursor: competitorLoading || !competitorAsin.trim() ? 'not-allowed' : 'pointer',
                display: 'flex', alignItems: 'center', gap: 6, whiteSpace: 'nowrap',
              }}
            >
              {competitorLoading ? (
                <>
                  <span style={{ width: 12, height: 12, border: '2px solid rgba(255,255,255,0.3)', borderTopColor: 'white', borderRadius: '50%', animation: 'spin 1s linear infinite', display: 'inline-block' }} />
                  Analysing…
                </>
              ) : 'Analyse (0.5 credits)'}
            </button>
          </div>
          {competitorMsg && <div style={msgStyle(competitorMsg.type)}>{competitorMsg.text}</div>}
          <p style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 10 }}>
            Queries live Amazon data to extract high-converting keywords from a competing product.
          </p>
        </div>
      )}

      {/* Tab: AI analysis */}
      {activeTab === 'ai' && (
        <div>
          {sourceLayer === 'ai' ? (
            <>
              <p style={{ fontSize: 12, color: 'var(--text-secondary)', marginBottom: 12 }}>
                AI-generated keyword reasoning for this product. Top 10 keywords from current analysis:
              </p>
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, marginBottom: 16 }}>
                {keywords.slice(0, 10).map(k => (
                  <KeywordChip
                    key={k.keyword}
                    keyword={k.keyword}
                    volume={k.search_volume ?? null}
                  />
                ))}
                {keywords.length === 0 && (
                  <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>No keywords yet</span>
                )}
              </div>
            </>
          ) : (
            <p style={{ fontSize: 12, color: 'var(--text-secondary)', marginBottom: 16 }}>
              AI analysis is your baseline — your current data layer (<strong>{sourceLayer}</strong>) provides better signals.
            </p>
          )}
          <button
            onClick={handleAIRefresh}
            disabled={aiRefreshLoading}
            style={{
              padding: '8px 16px', borderRadius: 8, fontSize: 12, fontWeight: 600,
              background: aiRefreshLoading ? 'var(--bg-tertiary)' : 'var(--primary)',
              color: aiRefreshLoading ? 'var(--text-muted)' : 'white',
              border: 'none', cursor: aiRefreshLoading ? 'not-allowed' : 'pointer',
              display: 'inline-flex', alignItems: 'center', gap: 6,
            }}
          >
            {aiRefreshLoading ? (
              <><span style={{ width: 12, height: 12, border: '2px solid rgba(255,255,255,0.3)', borderTopColor: 'white', borderRadius: '50%', animation: 'spin 1s linear infinite', display: 'inline-block' }} />Re-analysing…</>
            ) : 'Re-analyse (0.5 credits)'}
          </button>
          {aiRefreshMsg && <div style={msgStyle(aiRefreshMsg.type)}>{aiRefreshMsg.text}</div>}
        </div>
      )}

      {/* Tab: Data refresh */}
      {activeTab === 'refresh' && (
        <div>
          <div style={{ fontSize: 12, color: 'var(--text-secondary)', marginBottom: 12 }}>
            Last refreshed: <strong>{formatDate(lastRefreshed)}</strong>
          </div>
          <p style={{ fontSize: 11, color: 'var(--text-muted)', marginBottom: 14 }}>
            This queries live search data via DataForSEO and counts towards your usage (0.25 credits, ~$0.012 platform cost).
          </p>
          <button
            onClick={handleForceRefresh}
            disabled={refreshLoading}
            style={{
              padding: '8px 16px', borderRadius: 8, fontSize: 12, fontWeight: 600,
              background: refreshLoading ? 'var(--bg-tertiary)' : 'var(--primary)',
              color: refreshLoading ? 'var(--text-muted)' : 'white',
              border: 'none', cursor: refreshLoading ? 'not-allowed' : 'pointer',
              display: 'inline-flex', alignItems: 'center', gap: 6,
            }}
          >
            {refreshLoading ? (
              <><span style={{ width: 12, height: 12, border: '2px solid rgba(255,255,255,0.3)', borderTopColor: 'white', borderRadius: '50%', animation: 'spin 1s linear infinite', display: 'inline-block' }} />Refreshing…</>
            ) : 'Force refresh from market data (0.25 credits)'}
          </button>
          {refreshMsg && <div style={msgStyle(refreshMsg.type)}>{refreshMsg.text}</div>}
        </div>
      )}

      {/* Section 4 — Keyword preview (collapsed) */}
      {keywords.length > 0 && (
        <details
          open={previewOpen}
          onToggle={e => setPreviewOpen((e.target as HTMLDetailsElement).open)}
          style={{ marginTop: 20, borderTop: '1px solid var(--border)', paddingTop: 16 }}
        >
          <summary style={{
            fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)',
            cursor: 'pointer', listStyle: 'none', display: 'flex', alignItems: 'center', gap: 6,
          }}>
            <span style={{ transition: 'transform 0.2s', transform: previewOpen ? 'rotate(90deg)' : 'none', display: 'inline-block' }}>▶</span>
            Top keywords preview
          </summary>
          <div style={{ marginTop: 12 }}>
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, marginBottom: 12 }}>
              {keywords.slice(0, 10).map(k => (
                <KeywordChip
                  key={k.keyword}
                  keyword={k.keyword}
                  volume={k.search_volume ?? null}
                />
              ))}
            </div>
            <span
              onClick={() => navigate(`/products/${productId}?tab=listings`)}
              style={{ fontSize: 12, color: 'var(--primary)', textDecoration: 'none', cursor: 'pointer' }}
            >
              View full analysis →
            </span>
          </div>
        </details>
      )}
    </div>
  );
}

export default KeywordIntelligenceCard;

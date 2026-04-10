// ============================================================================
// SEOScorePanel — full SEO panel with score ring, evidence table, content panel
// ============================================================================
// Owns all data fetching. Placed in the 'seo' tab of ListingDetail.

import React, { useState, useEffect, useCallback } from 'react';
import { getActiveTenantId } from '../../contexts/TenantContext';
import { SEOScoreBadge } from './SEOScoreBadge';
import { KeywordEvidenceTable, KeywordEvidenceRow } from './KeywordEvidenceTable';
import { OptimisedContentPanel, ListingFieldContent } from './OptimisedContentPanel';
import { DataLayer } from './LayerStatusBadge';

const API_BASE = (import.meta as any).env?.VITE_API_URL || 'http://localhost:8080/api/v1';

// ── API response shapes ───────────────────────────────────────────────────────

interface ListingScoreResponse {
  total: number;
  title_coverage: number;
  title_placement: number;
  bullet_coverage: number;
  description_depth: number;
  field_completeness: number;
  layer_confidence: number;
  gap_keywords: any[];
  keyword_cache_key: string;
}

interface KeywordEntry {
  keyword: string;
  score: number;
  tier: string;
  volume: number;
  rank: number;
  conversion_weight: number;
  in_title?: boolean;
  in_bullets?: boolean;
  in_description?: boolean;
}

interface KeywordIntelligenceResponse {
  cache_key: string;
  keywords: KeywordEntry[];
  source_layer: string;
  last_refreshed: string;
  category: string;
}

// ── Score ring ────────────────────────────────────────────────────────────────

function ScoreRing({ score, size = 96 }: { score: number | null; size?: number }) {
  const radius = (size - 10) / 2;
  const circumference = 2 * Math.PI * radius;
  const pct = score === null ? 0 : Math.min(100, Math.max(0, score));
  const dash = (pct / 100) * circumference;

  let color = '#6b7280'; // grey for null
  if (score !== null) {
    if (score >= 90) color = '#0d9488';
    else if (score >= 70) color = '#22c55e';
    else if (score >= 40) color = '#f59e0b';
    else color = '#ef4444';
  }

  return (
    <div style={{ position: 'relative', width: size, height: size, flexShrink: 0 }}>
      <svg width={size} height={size} style={{ transform: 'rotate(-90deg)' }}>
        <circle cx={size / 2} cy={size / 2} r={radius} fill="none" stroke="var(--bg-tertiary)" strokeWidth={8} />
        <circle
          cx={size / 2} cy={size / 2} r={radius}
          fill="none"
          stroke={color}
          strokeWidth={8}
          strokeLinecap="round"
          strokeDasharray={circumference}
          strokeDashoffset={circumference - dash}
          style={{ transition: 'stroke-dashoffset 0.6s ease' }}
        />
      </svg>
      <div style={{
        position: 'absolute', inset: 0,
        display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center',
      }}>
        <span style={{ fontSize: score === null ? 14 : 22, fontWeight: 800, color, lineHeight: 1 }}>
          {score === null ? '—' : score}
        </span>
        {score !== null && <span style={{ fontSize: 9, color: 'var(--text-muted)', marginTop: 1 }}>/100</span>}
      </div>
    </div>
  );
}

// ── Breakdown bar ─────────────────────────────────────────────────────────────

function BreakdownBar({ label, value, max }: { label: string; value: number; max: number }) {
  const pct = Math.round((value / max) * 100);
  const barColor = pct >= 80 ? '#22c55e' : pct >= 40 ? '#f59e0b' : '#ef4444';
  return (
    <div style={{ marginBottom: 8 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 3 }}>
        <span style={{ fontSize: 11, color: 'var(--text-secondary)' }}>{label}</span>
        <span style={{ fontSize: 11, fontWeight: 600, color: barColor }}>{value}/{max}</span>
      </div>
      <div style={{ height: 4, background: 'var(--bg-tertiary)', borderRadius: 2, overflow: 'hidden' }}>
        <div style={{ height: '100%', width: `${pct}%`, background: barColor, borderRadius: 2, transition: 'width 0.5s ease' }} />
      </div>
    </div>
  );
}

// ── Main component ────────────────────────────────────────────────────────────

export interface SEOScorePanelProps {
  listingId: string;
  productAsin: string | null;
  productId: string;
  listingChannel?: string;                   // e.g. "amazon", "ebay" — used for generation
  currentContent?: ListingFieldContent;      // optional — passed from ListingDetail if available
}

function apiHeaders() {
  return { 'X-Tenant-Id': getActiveTenantId(), 'Content-Type': 'application/json' };
}

export function SEOScorePanel({ listingId, productAsin, productId, listingChannel = 'amazon', currentContent: currentContentProp }: SEOScorePanelProps) {
  const [scoreData, setScoreData] = useState<ListingScoreResponse | null>(null);
  const [kwData, setKwData] = useState<KeywordIntelligenceResponse | null>(null);
  const [scoreLoading, setScoreLoading] = useState(true);
  const [kwLoading, setKwLoading] = useState(true);
  const [optimisedContent, setOptimisedContent] = useState<ListingFieldContent | null>(null);
  const [isGenerating, setIsGenerating] = useState(false);

  const fetchScore = useCallback(async () => {
    setScoreLoading(true);
    try {
      const res = await fetch(`${API_BASE}/listings/${listingId}/seo-score`, { headers: apiHeaders() });
      if (res.ok) setScoreData(await res.json());
    } catch { /* non-fatal */ } finally { setScoreLoading(false); }
  }, [listingId]);

  const fetchKeywords = useCallback(async () => {
    if (!productId) return;
    setKwLoading(true);
    try {
      const res = await fetch(`${API_BASE}/products/${productId}/keyword-intelligence`, { headers: apiHeaders() });
      if (res.ok) setKwData(await res.json());
    } catch { /* non-fatal */ } finally { setKwLoading(false); }
  }, [productId]);

  useEffect(() => { fetchScore(); fetchKeywords(); }, [fetchScore, fetchKeywords]);

  // ── Map keyword intelligence response to table rows ───────────────────────

  function buildEvidenceRows(): KeywordEvidenceRow[] {
    if (!kwData?.keywords) return [];

    // Index gap_keywords from score response for inListing flags
    const gapSet = new Set<string>(
      (scoreData?.gap_keywords ?? []).map((g: any) => (g.keyword ?? g.Keyword ?? '').toLowerCase())
    );

    return kwData.keywords.map(kw => {
      const kwLower = kw.keyword.toLowerCase();
      const isGap = gapSet.has(kwLower);

      // Determine priority from score/conversion_weight
      let priority: 'HIGH' | 'MED' | 'LOW' = 'LOW';
      if (kw.conversion_weight > 0.6 || kw.score > 0.7) priority = 'HIGH';
      else if (kw.conversion_weight > 0.3 || kw.score > 0.4) priority = 'MED';

      return {
        keyword: kw.keyword,
        searchVolume: kw.volume > 0 ? kw.volume : null,
        clicks: null,      // only available with DataForSEO ads data
        purchases: kw.conversion_weight > 0 ? Math.round(kw.conversion_weight * 100) : null,
        priority,
        inTitle: !isGap && (kw.in_title ?? false),
        inBullets: !isGap && (kw.in_bullets ?? false),
        inDescription: !isGap && (kw.in_description ?? false),
      };
    });
  }

  // ── Handle generation ─────────────────────────────────────────────────────

  async function handleGenerate(fields: string[]) {
    setIsGenerating(true);
    try {
      // POST /api/v1/ai/generate — accepts { product_id, channels, mode }
      // fields param is informational; generation is per-product not per-field
      const res = await fetch(`${API_BASE}/ai/generate`, {
        method: 'POST',
        headers: apiHeaders(),
        body: JSON.stringify({ product_id: productId, channels: [listingChannel], mode: 'hybrid' }),
      });
      if (res.ok) {
        const data = await res.json();
        // Response shape: { data: { listings: [{ channel, title, description, bullet_points }] } }
        const listing = data?.data?.listings?.[0] ?? data?.data ?? data;
        setOptimisedContent({
          title: listing.title ?? '',
          bullets: listing.bullet_points ?? listing.bullets ?? [],
          description: listing.description ?? '',
        });
        fetchScore();
      }
    } catch { /* non-fatal */ } finally { setIsGenerating(false); }
  }

  // Fetch current overrides so we can merge individual field updates
  async function fetchCurrentOverrides(): Promise<Record<string, any>> {
    try {
      const res = await fetch(`${API_BASE}/marketplace/listings/${listingId}`, { headers: apiHeaders() });
      if (res.ok) {
        const data = await res.json();
        // GET /marketplace/listings/:id returns { data: { listing: { overrides: {...} } } }
        return data?.data?.listing?.overrides ?? data?.data?.overrides ?? {};
      }
    } catch { /* non-fatal */ }
    return {};
  }

  async function handleSaveField(field: string, value: string) {
    try {
      // PATCH /api/v1/marketplace/listings/:id — replaces overrides object entirely.
      // Must fetch current overrides first to avoid wiping other fields.
      // OptimisedContentPanel passes:
      //   field="title"       value=string
      //   field="description" value=string
      //   field="bullets"     value=newline-joined string  → must split to string[]
      // ListingOverrides JSON keys: title, description, bullet_points
      const current = await fetchCurrentOverrides();
      let merged: Record<string, any>;
      if (field === 'bullets') {
        // Split newline-joined string back to array, filter empty lines
        const bulletArray = value.split('\n').map(s => s.trim()).filter(Boolean);
        merged = { ...current, bullet_points: bulletArray };
      } else {
        merged = { ...current, [field]: value };
      }
      await fetch(`${API_BASE}/marketplace/listings/${listingId}`, {
        method: 'PATCH',
        headers: apiHeaders(),
        body: JSON.stringify(merged),
      });
      fetchScore();
    } catch { /* non-fatal */ }
  }

  function handleDiscardField(_field: string) {
    // Just clear optimised content for that field — for simplicity clear all
    setOptimisedContent(null);
  }

  // ── Build current content — use prop if provided by ListingDetail ────────

  const currentContent: ListingFieldContent = currentContentProp ?? {
    title: '',
    bullets: [],
    description: '',
  };

  const dataLayer: DataLayer = kwData?.source_layer as DataLayer ?? null;
  const evidenceRows = buildEvidenceRows();
  const score = scoreData ? scoreData.total : null;

  const scoreComponents = scoreData ? [
    { label: 'Title coverage',   value: scoreData.title_coverage,    max: 20 },
    { label: 'Title placement',  value: scoreData.title_placement,   max: 15 },
    { label: 'Bullet coverage',  value: scoreData.bullet_coverage,   max: 25 },
    { label: 'Description depth',value: scoreData.description_depth, max: 20 },
    { label: 'Field completeness',value: scoreData.field_completeness, max: 10 },
    { label: 'Data confidence',  value: scoreData.layer_confidence,  max: 10 },
  ] : [];

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      {/* Score header */}
      <div style={{
        background: 'var(--bg-elevated)',
        border: '1px solid var(--border)',
        borderRadius: 10,
        padding: '16px 20px',
        display: 'flex',
        gap: 24,
        alignItems: 'flex-start',
      }}>
        {/* Ring */}
        <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 6 }}>
          <ScoreRing score={scoreLoading ? null : score} />
          <SEOScoreBadge score={scoreLoading ? null : score} size="sm" showLabel />
        </div>

        {/* Breakdown bars */}
        <div style={{ flex: 1 }}>
          <div style={{ fontSize: 13, fontWeight: 700, marginBottom: 12, color: 'var(--text-primary)' }}>
            Score breakdown
          </div>
          {scoreLoading ? (
            <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>Loading score…</div>
          ) : scoreComponents.length === 0 ? (
            <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>Score not yet available. Generate your first listing to get a score.</div>
          ) : (
            scoreComponents.map(c => <BreakdownBar key={c.label} {...c} />)
          )}
        </div>
      </div>

      {/* Two-column panel: keywords left, content right */}
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16, alignItems: 'start' }}>
        {/* Left: keyword evidence table */}
        <div style={{
          background: 'var(--bg-elevated)',
          border: '1px solid var(--border)',
          borderRadius: 10,
          padding: '14px 16px',
          minHeight: 400,
          display: 'flex',
          flexDirection: 'column',
        }}>
          <div style={{ fontSize: 13, fontWeight: 700, marginBottom: 12 }}>Keyword evidence</div>
          <div style={{ flex: 1, overflow: 'hidden' }}>
            <KeywordEvidenceTable
              keywords={evidenceRows}
              isLoading={kwLoading}
              dataLayer={dataLayer}
            />
          </div>
        </div>

        {/* Right: optimised content panel */}
        <div style={{
          background: 'var(--bg-elevated)',
          border: '1px solid var(--border)',
          borderRadius: 10,
          padding: '14px 16px',
        }}>
          <div style={{ fontSize: 13, fontWeight: 700, marginBottom: 12 }}>Optimised content</div>
          <OptimisedContentPanel
            listingId={listingId}
            currentContent={currentContent}
            optimisedContent={optimisedContent}
            keywordsCovered={kwData?.keywords.slice(0, 8).map(k => k.keyword) ?? []}
            rationale=""
            isGenerating={isGenerating}
            onGenerate={handleGenerate}
            onSaveField={handleSaveField}
            onDiscardField={handleDiscardField}
            creditCost={1}
            hasEnoughCredits={true}
          />
        </div>
      </div>
    </div>
  );
}

export default SEOScorePanel;

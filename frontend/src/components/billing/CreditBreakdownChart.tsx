// ============================================================================
// CreditBreakdownChart — horizontal bar chart for SEO/keyword credit spend
// ============================================================================
// Session 11. Reusable component; receives breakdown data already fetched by
// the parent (UsageDashboard). Pure SVG — no Chart.js or external dep needed.
//
// Chart.js is NOT in package.json so we build with SVG instead. The output
// is visually equivalent: horizontal bars, one per event_type with credits > 0,
// colour-coded by operation type, with percentage labels.
//
// platform_absorbed is excluded from the chart (always 0 credits) but the
// parent UsageDashboard shows it in the reference table below.

import React from 'react';

export interface BreakdownEntry {
  count: number;
  credits: number;
}

export interface CreditBreakdownChartProps {
  breakdown: Record<string, BreakdownEntry>;
  isLoading: boolean;
}

// ── Colour mapping (matching spec hex values) ─────────────────────────────────
const COLOUR_MAP: Record<string, string> = {
  ai_listing_optimise:          '#0F6E56', // teal
  ai_keyword_reanalysis:        '#3C3489', // purple
  dataforseo_asin_refresh:      '#854F0B', // amber
  dataforseo_competitor_lookup: '#993C1D', // coral
};
const FALLBACK_COLOUR = '#888780'; // grey for any other type

function getColour(eventType: string): string {
  return COLOUR_MAP[eventType] ?? FALLBACK_COLOUR;
}

// ── Human-readable labels ─────────────────────────────────────────────────────
const LABEL_MAP: Record<string, string> = {
  ai_listing_optimise:             'AI listing optimisation',
  ai_keyword_reanalysis:           'AI keyword re-analysis',
  dataforseo_asin_refresh:         'Market data refresh',
  dataforseo_competitor_lookup:    'Competitor analysis',
  dataforseo_asin_lookup:          'Market data (platform)',
  amazon_catalog_extract:          'Catalog enrichment (platform)',
  amazon_ads_kw_recommendations:   'Ads keyword recommendations (platform)',
  brand_analytics_pull:            'Brand Analytics sync (platform)',
};

function getLabel(eventType: string): string {
  return LABEL_MAP[eventType] ?? eventType.replace(/_/g, ' ');
}

// ── Loading skeleton ──────────────────────────────────────────────────────────
function BarSkeleton() {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      {[60, 40, 25, 15].map((w, i) => (
        <div key={i} style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          <div style={{
            width: 160, height: 13, borderRadius: 4,
            background: 'var(--bg-tertiary)',
            animation: 'pulse 1.5s ease-in-out infinite',
            flexShrink: 0,
          }} />
          <div style={{
            flex: 1, height: 22, borderRadius: 4,
            background: 'var(--bg-tertiary)',
            animation: 'pulse 1.5s ease-in-out infinite',
            maxWidth: `${w}%`,
          }} />
          <div style={{
            width: 40, height: 13, borderRadius: 4,
            background: 'var(--bg-tertiary)',
            animation: 'pulse 1.5s ease-in-out infinite',
            flexShrink: 0,
          }} />
        </div>
      ))}
    </div>
  );
}

// ── Main component ────────────────────────────────────────────────────────────
export function CreditBreakdownChart({ breakdown, isLoading }: CreditBreakdownChartProps) {
  if (isLoading) return <BarSkeleton />;

  // Filter to credit-consuming entries only (exclude platform_absorbed which is always 0)
  const entries = Object.entries(breakdown)
    .filter(([key, val]) => key !== 'platform_absorbed' && val.credits > 0)
    .sort(([, a], [, b]) => b.credits - a.credits);

  if (entries.length === 0) {
    return (
      <div style={{
        textAlign: 'center',
        padding: '32px 0',
        color: 'var(--text-muted)',
        fontSize: 14,
      }}>
        No credit-consuming events in this period.
      </div>
    );
  }

  const maxCredits = Math.max(...entries.map(([, v]) => v.credits));

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
      {entries.map(([eventType, { credits, count }]) => {
        const barPct = maxCredits > 0 ? (credits / maxCredits) * 100 : 0;
        const colour = getColour(eventType);
        const label = getLabel(eventType);

        return (
          <div key={eventType} style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            {/* Operation label */}
            <div style={{
              width: 200,
              flexShrink: 0,
              fontSize: 12,
              color: 'var(--text-secondary)',
              textAlign: 'right',
              lineHeight: 1.3,
            }}>
              {label}
            </div>

            {/* Bar track */}
            <div style={{
              flex: 1,
              height: 22,
              background: 'var(--bg-tertiary)',
              borderRadius: 4,
              overflow: 'hidden',
              position: 'relative',
            }}>
              <div style={{
                width: `${barPct}%`,
                height: '100%',
                background: colour,
                borderRadius: 4,
                transition: 'width 0.4s ease',
                minWidth: barPct > 0 ? 4 : 0,
              }} />
            </div>

            {/* Credits value */}
            <div style={{
              width: 52,
              flexShrink: 0,
              fontSize: 12,
              fontWeight: 700,
              color: 'var(--text-primary)',
              textAlign: 'right',
            }}>
              {credits.toFixed(1)} cr
            </div>

            {/* Op count */}
            <div style={{
              width: 48,
              flexShrink: 0,
              fontSize: 11,
              color: 'var(--text-muted)',
              textAlign: 'right',
            }}>
              ×{count}
            </div>
          </div>
        );
      })}
    </div>
  );
}

export default CreditBreakdownChart;

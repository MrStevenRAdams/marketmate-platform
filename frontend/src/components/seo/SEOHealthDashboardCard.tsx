// ============================================================================
// SEOHealthDashboardCard — Session 7
// Fetches GET /api/v1/listings/seo-summary and renders:
//   • Average score badge
//   • Score distribution horizontal bars
//   • Worst 5 listings with Fix buttons
//   • "Optimise all poor listings" button (only when poor listings exist)
// ============================================================================

import { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { apiFetch } from '../../services/apiFetch';
import { SEOScoreBadge } from './SEOScoreBadge';
import { BulkOptimiseModal } from './BulkOptimiseModal';

export interface SEOHealthDashboardCardProps {
  onViewAll?: () => void;
  onOptimiseAll?: (poorIds: string[]) => void;
}

interface ListingEntry {
  listing_id: string;
  seo_score: number | null;
}

interface ScoreDistribution {
  excellent: number;
  good: number;
  needs_improvement: number;
  poor: number;
}

interface SEOSummaryResponse {
  average_score: number;
  listings: ListingEntry[];
  score_distribution: ScoreDistribution;
}

const BAND_CONFIG = [
  { key: 'excellent' as const,        label: 'Excellent',        color: '#0d9488' },
  { key: 'good' as const,             label: 'Good',             color: '#22c55e' },
  { key: 'needs_improvement' as const, label: 'Needs improvement', color: '#f59e0b' },
  { key: 'poor' as const,             label: 'Poor',             color: '#ef4444' },
];

// ── Sub-components ────────────────────────────────────────────────────────────

function SkeletonBar({ w = '100%' }: { w?: string }) {
  return (
    <div style={{
      height: 20, width: w, borderRadius: 4,
      background: 'linear-gradient(90deg, var(--bg-elevated) 25%, var(--bg-tertiary, #1e2536) 50%, var(--bg-elevated) 75%)',
      backgroundSize: '200% 100%',
      animation: 'shimmer 1.5s infinite',
    }} />
  );
}

// ── Main component ────────────────────────────────────────────────────────────

export function SEOHealthDashboardCard({ onViewAll, onOptimiseAll }: SEOHealthDashboardCardProps) {
  const navigate = useNavigate();

  const [summary, setSummary] = useState<SEOSummaryResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [bulkModalOpen, setBulkModalOpen] = useState(false);
  const [poorListingIds, setPoorListingIds] = useState<string[]>([]);

  useEffect(() => {
    setLoading(true);
    apiFetch('/listings/seo-summary?limit=500')
      .then(r => r.ok ? r.json() : null)
      .then((d: SEOSummaryResponse | null) => {
        if (!d) return;
        setSummary(d);
        // Collect poor listing IDs (score < 40 or null scored as 0)
        const poor = (d.listings || [])
          .filter(l => l.seo_score !== null && l.seo_score < 40)
          .map(l => l.listing_id);
        setPoorListingIds(poor);
      })
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  // Worst 5 scored listings (only those with a score, sorted ASC)
  const worstFive = summary
    ? [...summary.listings]
        .filter(l => l.seo_score !== null)
        .sort((a, b) => (a.seo_score ?? 0) - (b.seo_score ?? 0))
        .slice(0, 5)
    : [];

  const totalInBands = summary
    ? Object.values(summary.score_distribution ?? {}).reduce((a, b) => a + b, 0)
    : 0;

  function handleOptimiseAllPoor() {
    if (onOptimiseAll) {
      onOptimiseAll(poorListingIds);
    } else {
      setBulkModalOpen(true);
    }
  }

  return (
    <>
      <div style={{
        background: 'var(--bg-secondary)',
        border: '1px solid var(--border)',
        borderRadius: 12,
        overflow: 'hidden',
        display: 'flex',
        flexDirection: 'column',
      }}>
        {/* Header */}
        <div style={{
          padding: '16px 20px',
          borderBottom: '1px solid var(--border)',
          display: 'flex', alignItems: 'center', justifyContent: 'space-between',
        }}>
          <div style={{ fontWeight: 600, color: 'var(--text-primary)', fontSize: 14 }}>
            Listing SEO Health
          </div>
          <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
            {!loading && summary && (
              <SEOScoreBadge score={Math.round(summary.average_score)} size="md" />
            )}
            {onViewAll && (
              <button
                onClick={onViewAll}
                style={{
                  fontSize: 12, padding: '4px 10px', borderRadius: 6, cursor: 'pointer',
                  background: 'var(--bg-elevated)', border: '1px solid var(--border)',
                  color: 'var(--text-muted)', fontFamily: 'inherit',
                }}
              >
                View all →
              </button>
            )}
          </div>
        </div>

        <div style={{ flex: 1, padding: '16px 20px', display: 'flex', flexDirection: 'column', gap: 20 }}>

          {/* Score distribution bars */}
          <div>
            <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: 10 }}>
              Score distribution
            </div>
            {loading ? (
              <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                {[80, 60, 45, 30].map((w, i) => <SkeletonBar key={i} w={`${w}%`} />)}
              </div>
            ) : summary ? (
              <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                {BAND_CONFIG.map(({ key, label, color }) => {
                  const count = summary.score_distribution?.[key] ?? 0;
                  const pct = totalInBands > 0 ? (count / totalInBands) * 100 : 0;
                  return (
                    <div key={key} style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                      <div style={{ width: 110, fontSize: 12, color: 'var(--text-secondary)', flexShrink: 0 }}>
                        <span style={{ fontWeight: 600, color, marginRight: 4 }}>{count}</span>
                        {label}
                      </div>
                      <div style={{ flex: 1, height: 8, background: 'var(--bg-elevated)', borderRadius: 4, overflow: 'hidden' }}>
                        <div style={{
                          height: '100%',
                          width: `${pct}%`,
                          background: color,
                          borderRadius: 4,
                          opacity: 0.85,
                          transition: 'width 0.4s ease',
                        }} />
                      </div>
                    </div>
                  );
                })}
              </div>
            ) : (
              <div style={{ fontSize: 13, color: 'var(--text-muted)' }}>No data available.</div>
            )}
          </div>

          {/* Worst 5 listings */}
          <div>
            <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: 10 }}>
              Lowest scoring listings
            </div>
            {loading ? (
              <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                {[...Array(5)].map((_, i) => <SkeletonBar key={i} />)}
              </div>
            ) : worstFive.length === 0 ? (
              <div style={{ fontSize: 13, color: 'var(--text-muted)' }}>No scored listings yet.</div>
            ) : (
              <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                {worstFive.map(listing => (
                  <div key={listing.listing_id} style={{
                    display: 'flex', alignItems: 'center', gap: 10,
                    padding: '6px 0',
                    borderBottom: '1px solid var(--border)',
                  }}>
                    <div style={{
                      flex: 1, fontSize: 12, fontFamily: 'monospace',
                      color: 'var(--text-secondary)', overflow: 'hidden',
                      textOverflow: 'ellipsis', whiteSpace: 'nowrap',
                    }}>
                      {listing.listing_id.length > 20
                        ? `…${listing.listing_id.slice(-18)}`
                        : listing.listing_id}
                    </div>
                    <SEOScoreBadge score={listing.seo_score} size="sm" />
                    <button
                      onClick={() => navigate(`/marketplace/listings/${listing.listing_id}?tab=seo`)}
                      style={{
                        fontSize: 11, padding: '3px 10px', borderRadius: 5, cursor: 'pointer',
                        background: 'var(--primary)', border: 'none',
                        color: '#fff', fontWeight: 600, fontFamily: 'inherit',
                        flexShrink: 0,
                      }}
                    >
                      Fix
                    </button>
                  </div>
                ))}
              </div>
            )}
          </div>

          {/* Optimise all poor listings button */}
          {!loading && poorListingIds.length > 0 && (
            <button
              onClick={handleOptimiseAllPoor}
              style={{
                width: '100%', padding: '10px 16px', borderRadius: 8, cursor: 'pointer',
                background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)',
                color: '#ef4444', fontWeight: 600, fontSize: 13, fontFamily: 'inherit',
                textAlign: 'center',
              }}
            >
              🔧 Optimise {poorListingIds.length} poor listing{poorListingIds.length !== 1 ? 's' : ''}
            </button>
          )}
        </div>
      </div>

      {/* Internal bulk modal (used when onOptimiseAll prop is not provided) */}
      {!onOptimiseAll && (
        <BulkOptimiseModal
          listingIds={poorListingIds}
          isOpen={bulkModalOpen}
          onClose={() => setBulkModalOpen(false)}
          onComplete={() => {
            setBulkModalOpen(false);
            // Refresh the card by re-fetching
            setLoading(true);
            apiFetch('/listings/seo-summary?limit=500')
              .then(r => r.ok ? r.json() : null)
              .then((d: SEOSummaryResponse | null) => {
                if (d) setSummary(d);
                const poor = (d?.listings || [])
                  .filter(l => l.seo_score !== null && (l.seo_score ?? 0) < 40)
                  .map(l => l.listing_id);
                setPoorListingIds(poor);
              })
              .catch(() => {})
              .finally(() => setLoading(false));
          }}
        />
      )}

      <style>{`@keyframes shimmer { 0%{background-position:200% 0} 100%{background-position:-200% 0} }`}</style>
    </>
  );
}

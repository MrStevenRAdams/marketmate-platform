// ============================================================================
// KeywordEvidenceTable — left panel keyword evidence grid
// ============================================================================
// Red rows = converting keywords not yet in the listing (critical gap).
// Amber rows = high-priority keywords with no purchase data, not in listing.

import React from 'react';
import { LayerStatusBadge, DataLayer } from './LayerStatusBadge';

export interface KeywordEvidenceRow {
  keyword: string;
  searchVolume: number | null;
  clicks: number | null;
  purchases: number | null;
  priority: 'HIGH' | 'MED' | 'LOW';
  inTitle: boolean;
  inBullets: boolean;
  inDescription: boolean;
}

export interface KeywordEvidenceTableProps {
  keywords: KeywordEvidenceRow[];
  isLoading: boolean;
  dataLayer: DataLayer;
}

const priorityColors: Record<string, { color: string; bg: string }> = {
  HIGH: { color: '#ef4444', bg: '#ef444415' },
  MED:  { color: '#f59e0b', bg: '#f59e0b15' },
  LOW:  { color: 'var(--text-muted)', bg: 'var(--bg-tertiary)' },
};

// Layers that don't have volume data yet
const VOLUME_HIDDEN_LAYERS: DataLayer[] = ['amazon_catalog', 'ai'];

function fmt(n: number | null, layer: DataLayer): string {
  if (VOLUME_HIDDEN_LAYERS.includes(layer)) return '—';
  if (n === null) return '—';
  if (n >= 1000) return `${(n / 1000).toFixed(1)}k`;
  return String(n);
}

function InListingDot({ inListing }: { inListing: boolean }) {
  return (
    <span
      title={inListing ? 'Keyword found in listing' : 'Keyword not in listing'}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        justifyContent: 'center',
        width: 20,
        height: 20,
        borderRadius: '50%',
        background: inListing ? '#22c55e20' : '#ef444420',
        color: inListing ? '#22c55e' : '#ef4444',
        fontSize: 12,
        fontWeight: 700,
      }}
    >
      {inListing ? '✓' : '✗'}
    </span>
  );
}

export function KeywordEvidenceTable({ keywords, isLoading, dataLayer }: KeywordEvidenceTableProps) {
  const hideVolume = VOLUME_HIDDEN_LAYERS.includes(dataLayer);

  const thStyle: React.CSSProperties = {
    padding: '8px 10px',
    fontSize: 10,
    fontWeight: 700,
    color: 'var(--text-muted)',
    textTransform: 'uppercase',
    textAlign: 'left',
    borderBottom: '2px solid var(--border)',
    whiteSpace: 'nowrap',
    position: 'sticky',
    top: 0,
    background: 'var(--bg-elevated)',
    zIndex: 1,
  };

  const numTh: React.CSSProperties = { ...thStyle, textAlign: 'right' };

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
      {/* Layer badge + hint */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 10, flexShrink: 0 }}>
        <LayerStatusBadge layer={dataLayer} />
        {hideVolume && (
          <span
            title="Volume data available after first listing generation."
            style={{ fontSize: 10, color: 'var(--text-muted)', cursor: 'help' }}
          >
            ℹ️ Vol/Clicks/Purch unavailable
          </span>
        )}
      </div>

      {/* Legend */}
      <div style={{ display: 'flex', gap: 12, marginBottom: 8, flexShrink: 0 }}>
        <span style={{ fontSize: 10, color: '#ef4444' }}>■ Converting keyword not in listing</span>
        <span style={{ fontSize: 10, color: '#f59e0b' }}>■ High-priority gap</span>
      </div>

      {/* Table */}
      <div style={{ flex: 1, overflowY: 'auto', border: '1px solid var(--border)', borderRadius: 8 }}>
        {isLoading ? (
          <div style={{ padding: 32, textAlign: 'center', color: 'var(--text-muted)', fontSize: 13 }}>
            Loading keyword data…
          </div>
        ) : keywords.length === 0 ? (
          <div style={{ padding: 32, textAlign: 'center', color: 'var(--text-muted)', fontSize: 13 }}>
            No keyword data available yet.
          </div>
        ) : (
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12 }}>
            <thead>
              <tr>
                <th style={thStyle}>Keyword</th>
                <th style={{ ...numTh, width: 60 }}>Vol</th>
                <th style={{ ...numTh, width: 60 }}>Clicks</th>
                <th style={{ ...numTh, width: 60 }}>Purch.</th>
                <th style={{ ...thStyle, width: 68 }}>Priority</th>
                <th style={{ ...thStyle, width: 60, textAlign: 'center' }}>In listing</th>
              </tr>
            </thead>
            <tbody>
              {keywords.map((row, i) => {
                const inListing = row.inTitle || row.inBullets || row.inDescription;
                const isConvertingGap = (row.purchases !== null && row.purchases > 0) && !inListing;
                const isHighPriorityGap = !inListing && row.priority === 'HIGH' && row.purchases === null;

                let rowBg = 'transparent';
                if (isConvertingGap)   rowBg = '#ef444412';
                else if (isHighPriorityGap) rowBg = '#f59e0b10';

                const pri = priorityColors[row.priority];

                return (
                  <tr
                    key={i}
                    style={{
                      background: rowBg,
                      borderBottom: '1px solid var(--border)',
                    }}
                  >
                    <td style={{ padding: '7px 10px', fontWeight: isConvertingGap ? 600 : 400 }}>
                      {isConvertingGap && (
                        <span title="Converting keyword not in listing" style={{ marginRight: 4, color: '#ef4444' }}>●</span>
                      )}
                      {row.keyword}
                    </td>
                    <td style={{ padding: '7px 10px', textAlign: 'right', color: 'var(--text-muted)', fontFamily: 'monospace' }}>
                      <span title={hideVolume ? 'Volume data available after first listing generation.' : undefined}>
                        {fmt(row.searchVolume, dataLayer)}
                      </span>
                    </td>
                    <td style={{ padding: '7px 10px', textAlign: 'right', color: 'var(--text-muted)', fontFamily: 'monospace' }}>
                      <span title={hideVolume ? 'Volume data available after first listing generation.' : undefined}>
                        {fmt(row.clicks, dataLayer)}
                      </span>
                    </td>
                    <td style={{ padding: '7px 10px', textAlign: 'right', color: isConvertingGap ? '#ef4444' : 'var(--text-muted)', fontFamily: 'monospace', fontWeight: isConvertingGap ? 700 : 400 }}>
                      <span title={hideVolume ? 'Volume data available after first listing generation.' : undefined}>
                        {fmt(row.purchases, dataLayer)}
                      </span>
                    </td>
                    <td style={{ padding: '7px 10px' }}>
                      <span style={{
                        display: 'inline-block',
                        padding: '1px 6px',
                        borderRadius: 10,
                        fontSize: 10,
                        fontWeight: 700,
                        background: pri.bg,
                        color: pri.color,
                        border: `1px solid ${pri.color}30`,
                      }}>
                        {row.priority}
                      </span>
                    </td>
                    <td style={{ padding: '7px 10px', textAlign: 'center' }}>
                      <InListingDot inListing={inListing} />
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}

export default KeywordEvidenceTable;

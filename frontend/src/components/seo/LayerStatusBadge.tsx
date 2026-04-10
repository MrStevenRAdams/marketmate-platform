// ============================================================================
// LayerStatusBadge — shows the active keyword data layer
// ============================================================================

import React from 'react';

export type DataLayer = 'amazon_catalog' | 'amazon_ads' | 'dataforseo' | 'ai' | null;

export interface LayerStatusBadgeProps {
  layer: DataLayer;
}

interface LayerMeta {
  label: string;
  color: string;
  bg: string;
  icon: string;
}

function getLayerMeta(layer: DataLayer): LayerMeta {
  switch (layer) {
    case 'dataforseo':  return { label: 'Full market data',      color: '#0d9488', bg: '#0d948820', icon: '📊' };
    case 'amazon_ads':  return { label: 'Catalog + Ads enhanced', color: '#6366f1', bg: '#6366f120', icon: '🎯' };
    case 'amazon_catalog': return { label: 'Catalog data',       color: '#3b82f6', bg: '#3b82f620', icon: '📦' };
    case 'ai':          return { label: 'AI estimated',           color: '#8b5cf6', bg: '#8b5cf620', icon: '✨' };
    default:            return { label: 'No data yet',            color: 'var(--text-muted)', bg: 'var(--bg-tertiary)', icon: '○' };
  }
}

export function LayerStatusBadge({ layer }: LayerStatusBadgeProps) {
  const meta = getLayerMeta(layer);
  return (
    <span
      title={`Keyword data source: ${meta.label}`}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 5,
        padding: '2px 8px',
        borderRadius: 12,
        fontSize: 11,
        fontWeight: 600,
        background: meta.bg,
        color: meta.color,
        border: `1px solid ${meta.color}30`,
        whiteSpace: 'nowrap',
      }}
    >
      <span>{meta.icon}</span>
      {meta.label}
    </span>
  );
}

export default LayerStatusBadge;

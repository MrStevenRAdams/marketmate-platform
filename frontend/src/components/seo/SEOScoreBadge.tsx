// ============================================================================
// SEOScoreBadge — compact coloured pill for SEO score display
// ============================================================================
// Used in ListingList rows and wherever a quick score indicator is needed.
//
// Session 9: added showUpsellTooltip prop. When true and score < 70 (red or
// amber), hovering the badge reveals a small tooltip: "Optimise this listing
// for 1 credit →". The tooltip is a link that navigates to the listing SEO
// tab. Implemented with pure CSS :hover via an injected <style> block — no JS
// event listeners added. Tooltip only shows for red/amber (< 70), never for
// green (≥ 70) or teal (≥ 90).

import React from 'react';
import { useNavigate } from 'react-router-dom';

export interface SEOScoreBadgeProps {
  score: number | null;   // null = not yet calculated
  size?: 'sm' | 'md';
  onClick?: () => void;
  showLabel?: boolean;
  // Session 9 — upsell tooltip
  showUpsellTooltip?: boolean;
  listingId?: string;      // required when showUpsellTooltip is true
}

interface ScoreMeta {
  bg: string;
  color: string;
  label: string;
}

function getScoreMeta(score: number | null): ScoreMeta {
  if (score === null) return { bg: 'var(--bg-tertiary)', color: 'var(--text-muted)', label: 'No score' };
  if (score >= 90)   return { bg: '#0d948820', color: '#0d9488', label: 'Excellent' };
  if (score >= 70)   return { bg: '#22c55e20', color: '#22c55e', label: 'Good' };
  if (score >= 40)   return { bg: '#f59e0b20', color: '#f59e0b', label: 'Could be better' };
  return              { bg: '#ef444420', color: '#ef4444', label: 'Needs work' };
}

// CSS injected once into the document so :hover works on the tooltip wrapper.
// The class name is namespaced to avoid collisions.
const TOOLTIP_STYLE_ID = 'mm-seo-badge-upsell-style';
function ensureTooltipStyles() {
  if (typeof document === 'undefined') return;
  if (document.getElementById(TOOLTIP_STYLE_ID)) return;
  const style = document.createElement('style');
  style.id = TOOLTIP_STYLE_ID;
  style.textContent = `
    .mm-seo-badge-wrap {
      position: relative;
      display: inline-flex;
    }
    .mm-seo-badge-tip {
      display: none;
      position: absolute;
      bottom: calc(100% + 6px);
      left: 50%;
      transform: translateX(-50%);
      white-space: nowrap;
      background: #1e293b;
      color: #fff;
      font-size: 11px;
      font-weight: 500;
      padding: 5px 10px;
      border-radius: 6px;
      box-shadow: 0 2px 8px rgba(0,0,0,0.25);
      pointer-events: auto;
      z-index: 100;
      line-height: 1.4;
    }
    .mm-seo-badge-tip::after {
      content: '';
      position: absolute;
      top: 100%;
      left: 50%;
      transform: translateX(-50%);
      border: 5px solid transparent;
      border-top-color: #1e293b;
    }
    .mm-seo-badge-wrap:hover .mm-seo-badge-tip {
      display: block;
    }
    .mm-seo-badge-tip a {
      color: #7dd3fc;
      text-decoration: none;
      font-weight: 600;
    }
    .mm-seo-badge-tip a:hover {
      text-decoration: underline;
    }
  `;
  document.head.appendChild(style);
}

export function SEOScoreBadge({
  score,
  size = 'md',
  onClick,
  showLabel = false,
  showUpsellTooltip = false,
  listingId,
}: SEOScoreBadgeProps) {
  const meta = getScoreMeta(score);
  const isSmall = size === 'sm';
  const navigate = useNavigate();

  // Inject tooltip CSS once on first render that needs it.
  const shouldShowTooltip = showUpsellTooltip && score !== null && score < 70 && !!listingId;
  if (shouldShowTooltip) {
    ensureTooltipStyles();
  }

  const badgeEl = (
    <span
      onClick={onClick}
      title={score === null ? 'SEO score not yet calculated' : `SEO Score: ${score}/100 — ${meta.label}`}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: isSmall ? 3 : 5,
        padding: isSmall ? '1px 6px' : '3px 10px',
        borderRadius: 20,
        fontSize: isSmall ? 10 : 12,
        fontWeight: 700,
        background: meta.bg,
        color: meta.color,
        border: `1px solid ${meta.color}40`,
        cursor: onClick ? 'pointer' : 'default',
        whiteSpace: 'nowrap',
        userSelect: 'none',
        transition: 'opacity 0.15s',
      }}
      onMouseEnter={e => { if (onClick) (e.currentTarget as HTMLElement).style.opacity = '0.8'; }}
      onMouseLeave={e => { if (onClick) (e.currentTarget as HTMLElement).style.opacity = '1'; }}
    >
      {score === null ? '—' : score}
      {showLabel && score !== null && (
        <span style={{ fontSize: isSmall ? 9 : 11, fontWeight: 500, opacity: 0.85 }}>
          {meta.label}
        </span>
      )}
      {onClick && (
        <span style={{ fontSize: isSmall ? 8 : 10, opacity: 0.6, marginLeft: 1 }}>›</span>
      )}
    </span>
  );

  // Wrap in tooltip container for red/amber scores when upsell is requested.
  if (shouldShowTooltip) {
    return (
      <span className="mm-seo-badge-wrap">
        {badgeEl}
        <span className="mm-seo-badge-tip">
          <a
            href={`/marketplace/listings/${listingId}?tab=seo`}
            onClick={e => {
              e.preventDefault();
              navigate(`/marketplace/listings/${listingId}?tab=seo`);
            }}
          >
            Optimise this listing for 1 credit →
          </a>
        </span>
      </span>
    );
  }

  return badgeEl;
}

export default SEOScoreBadge;

// src/utils/keywordUtils.ts
// Shared keyword manipulation utilities used by all five listing-create pages.
// Single source of truth — no duplicated fetch logic across components.

import { getActiveTenantId } from '../contexts/TenantContext';
import { KeywordIntelligenceResponse } from '../types/seo';

// ---------------------------------------------------------------------------
// Etsy tag truncation
// Truncates a keyword to Etsy's 20-char tag limit.
// Strategy: last-space split before char 20 → first word → hard truncate.
// ---------------------------------------------------------------------------
export function truncateToEtsyTag(keyword: string): string {
  const k = keyword.trim().toLowerCase();
  if (k.length <= 20) return k;

  // Last space before char 20
  const sub = k.slice(0, 20);
  const lastSpace = sub.lastIndexOf(' ');
  if (lastSpace > 0) return sub.slice(0, lastSpace);

  // Fall back to first word
  const firstSpace = k.indexOf(' ');
  if (firstSpace > 0) return k.slice(0, firstSpace).slice(0, 20);

  // Hard truncate
  return k.slice(0, 20);
}

// ---------------------------------------------------------------------------
// Temu title assembly
// Assembles a Temu title from keywords following the template:
// [Brand] · [Product details] · [Application range] · [Product type] · [Main features]
// ---------------------------------------------------------------------------
export function assembleTemuTitle(keywords: string[]): string {
  if (keywords.length === 0) return '';
  const kws = keywords.map(k => k.trim()).filter(Boolean);

  const brand    = kws[0] ?? '';
  const details  = kws[1] ?? '';
  const appRange = [kws[2], kws[3]].filter(Boolean).join(', ');
  const type     = kws[4] ?? '';
  const features = kws.slice(5, 8).filter(Boolean).join(', ');

  return [brand, details, appRange, type, features]
    .filter(Boolean)
    .join(' · ')
    .slice(0, 500);
}

// ---------------------------------------------------------------------------
// Shopify meta description assembly
// Assembles a ≤155-char meta description from keywords and product title.
// Format: "{kw1} — {kw2} and {kw3}. {truncated_title}."
// ---------------------------------------------------------------------------
export function assembleMetaDescription(keywords: string[], productTitle: string): string {
  const kw1 = keywords[0] ?? '';
  const kw2 = keywords[1] ?? '';
  const kw3 = keywords[2] ?? '';

  const kwPart = kw3
    ? `${kw1} — ${kw2} and ${kw3}.`
    : kw2
    ? `${kw1} — ${kw2}.`
    : `${kw1}.`;

  // How much room is left for the title?
  const space = 155 - kwPart.length - 2; // " " + "."
  const truncTitle = space > 0 ? productTitle.slice(0, space) : '';
  const full = truncTitle ? `${kwPart} ${truncTitle}.` : kwPart;

  return full.slice(0, 155);
}

// ---------------------------------------------------------------------------
// Shared fetch utility
// Single fetch used by all listing-create pages — returns null on any error.
// apiBase should be import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1'
// ---------------------------------------------------------------------------
export async function fetchKeywordIntelligence(
  productId: string,
  apiBase: string,
): Promise<KeywordIntelligenceResponse | null> {
  try {
    const tenantId = getActiveTenantId();
    const base = apiBase || (import.meta.env.VITE_API_URL as string) || 'http://localhost:8080/api/v1';
    const res = await fetch(
      `${base}/products/${encodeURIComponent(productId)}/keyword-intelligence`,
      {
        headers: {
          'X-Tenant-Id': tenantId,
          'Content-Type': 'application/json',
        },
      },
    );
    if (!res.ok) return null;
    const data = await res.json();
    // Handle both bare response and { ok, data: {...} } envelope
    return (data?.data ?? data) as KeywordIntelligenceResponse;
  } catch {
    return null;
  }
}

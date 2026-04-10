// src/types/seo.ts
// Shared types for the keyword intelligence API response.
// Used by all five listing-create pages and SEO UI components.

export interface KeywordEntry {
  keyword: string;
  score: number;
  search_volume: number | null;
  organic_rank: number;
  bid_estimate_low: number;
  bid_estimate_high: number;
  source_layer: string;
}

export interface KeywordIntelligenceResponse {
  cache_key: string;
  keywords: KeywordEntry[];
  source_layer: string;
  last_refreshed: string;
  category: string;
}

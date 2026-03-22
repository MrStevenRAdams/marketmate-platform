// ============================================================================
// ETSY API SERVICE
// ============================================================================
// Calls the Go backend's /api/v1/etsy/* endpoints.

import axios from 'axios';
import { getActiveTenantId } from '../contexts/TenantContext';
import { auth } from '../contexts/AuthContext';

const API_BASE_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';

const api = axios.create({
  baseURL: `${API_BASE_URL}/etsy`,
  headers: { 'Content-Type': 'application/json' },
});

api.interceptors.request.use(async (config) => {
  config.headers['X-Tenant-Id'] = getActiveTenantId();
  try {
    let user = auth.currentUser;
    if (!user) {
      user = await new Promise<typeof auth.currentUser>((resolve) => {
        const unsub = auth.onAuthStateChanged((u) => { unsub(); resolve(u); });
        setTimeout(() => resolve(null), 5000);
      });
    }
    if (user) {
      const token = await user.getIdToken();
      config.headers['Authorization'] = `Bearer ${token}`;
    }
  } catch { /* non-fatal */ }
  return config;
});

// ── Types ─────────────────────────────────────────────────────────────────────

export interface EtsyShop {
  shop_id: number;
  shop_name: string;
  url: string;
  title: string;
}

export interface EtsyTaxonomyNode {
  id: number;
  level: number;
  name: string;
  parent_id: number;
  children?: EtsyTaxonomyNode[];
}

export interface EtsyTaxonomyProperty {
  property_id: number;
  name: string;
  display_name: string;
  is_required: boolean;
  is_multivalued: boolean;
  possible_values?: { value_id: number; name: string }[];
}

export interface EtsyShippingProfile {
  shipping_profile_id: number;
  title: string;
  user_id: number;
  min_processing_days?: number;
  max_processing_days?: number;
}

export interface EtsyMoney {
  amount: number;
  divisor: number;
  currency_code: string;
}

export interface EtsyListingImage {
  listing_image_id: number;
  url_570xN: string;
  url_fullxfull: string;
  rank: number;
}

export interface EtsyListing {
  listing_id: number;
  title: string;
  description: string;
  price: EtsyMoney;
  quantity: number;
  state: string;
  tags: string[];
  materials: string[];
  taxonomy_id: number;
  who_made: string;
  when_made: string;
  is_supply: boolean;
  shipping_profile_id?: number;
  images?: EtsyListingImage[];
  url?: string;
}

export interface EtsySubmitPayload {
  title: string;
  description: string;
  price: number;
  quantity: number;
  taxonomy_id: number;
  who_made: string;
  when_made: string;
  is_supply: boolean;
  shipping_profile_id?: number;
  tags?: string[];
  materials?: string[];
  images?: string[];
  variants?: ChannelVariantDraft[]; // VAR-01
}

import type { ChannelVariantDraft } from './channel-types';
export type { ChannelVariantDraft };

export interface EtsyDraft {
  title: string;
  description: string;
  price: string | number;
  quantity: number;
  sku: string;
  images: string[];
  tags: string[];
  materials: string[];
  who_made: string;
  when_made: string;
  is_supply: boolean;
  taxonomy_id: number;

  // VAR-01 — Variation listings (Session H)
  variants?: ChannelVariantDraft[];
}

// ── API Methods ───────────────────────────────────────────────────────────────

export const etsyApi = {
  /** GET /etsy/shop — connected shop info */
  getShop: (credentialId?: string) =>
    api.get<{ ok: boolean; shop: EtsyShop }>('/shop', {
      params: credentialId ? { credential_id: credentialId } : {},
    }),

  /** GET /etsy/taxonomy — full taxonomy tree */
  getTaxonomy: (credentialId?: string) =>
    api.get<{ ok: boolean; nodes: EtsyTaxonomyNode[] }>('/taxonomy', {
      params: credentialId ? { credential_id: credentialId } : {},
    }),

  /** GET /etsy/taxonomy/:id/properties */
  getTaxonomyProperties: (taxonomyId: number, credentialId?: string) =>
    api.get<{ ok: boolean; properties: EtsyTaxonomyProperty[] }>(
      `/taxonomy/${taxonomyId}/properties`,
      { params: credentialId ? { credential_id: credentialId } : {} }
    ),

  /** GET /etsy/shipping-profiles */
  getShippingProfiles: (credentialId?: string) =>
    api.get<{ ok: boolean; profiles: EtsyShippingProfile[] }>('/shipping-profiles', {
      params: credentialId ? { credential_id: credentialId } : {},
    }),

  /** POST /etsy/images/upload — fetch an image URL and upload to Etsy */
  uploadImage: (url: string, listingId?: number, rank?: number) =>
    api.post<{ ok: boolean; image?: EtsyListingImage; base64?: string; error?: string }>(
      '/images/upload',
      { url, listing_id: listingId, rank }
    ),

  /** POST /etsy/prepare — build draft from MarketMate product */
  prepare: (data: { product_id: string; credential_id?: string }) =>
    api.post<{ ok: boolean; product_id: string; draft: EtsyDraft; error?: string }>('/prepare', data),

  /** POST /etsy/submit — create listing on Etsy */
  submit: (payload: EtsySubmitPayload) =>
    api.post<{ ok: boolean; listing_id: number; state: string; error?: string }>('/submit', payload),

  /** PUT /etsy/listings/:id — update listing */
  updateListing: (listingId: number, payload: Partial<EtsySubmitPayload & { state: string }>) =>
    api.put<{ ok: boolean; listing_id: number; state: string; error?: string }>(
      `/listings/${listingId}`,
      payload
    ),

  /** DELETE /etsy/listings/:id */
  deleteListing: (listingId: number) =>
    api.delete<{ ok: boolean; deleted: number }>(`/listings/${listingId}`),

  /** GET /etsy/listings */
  getListings: (offset?: number, limit?: number) =>
    api.get<{ ok: boolean; listings: EtsyListing[]; count: number; offset: number; limit: number }>(
      '/listings',
      { params: { offset, limit } }
    ),

  /** POST /etsy/orders/import */
  importOrders: (data: { credential_id?: string; hours_back?: number }) =>
    api.post('/orders/import', data),

  /** POST /etsy/orders/:id/ship */
  pushTracking: (receiptId: string, data: { tracking_number: string; carrier_name?: string; credential_id?: string }) =>
    api.post(`/orders/${receiptId}/ship`, data),

  /** GET /etsy/oauth/login */
  getOAuthURL: (accountName: string) =>
    fetch(`${API_BASE_URL}/etsy/oauth/login?account_name=${encodeURIComponent(accountName)}`, {
      headers: { 'X-Tenant-Id': getActiveTenantId() },
    }).then((r) => r.json() as Promise<{ ok: boolean; consent_url: string; error?: string }>),
};

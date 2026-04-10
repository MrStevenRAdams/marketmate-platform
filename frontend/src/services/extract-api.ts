// ============================================================================
// EXTRACT API SERVICE
// ============================================================================
// Calls the Go backend's /api/v1/extract/* endpoints.
// Implements IMP-01 / CLM-01 (Extract Inventory) and CLM-02 (Inventory Mapping).

import axios from 'axios';
import { getActiveTenantId } from '../contexts/TenantContext';
import { auth } from '../contexts/AuthContext';

const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';

const api = axios.create({
  baseURL: `${API_BASE}/extract`,
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

export interface ExtractChannel {
  credential_id: string;
  channel: string;
  account_name: string;
  active: boolean;
}

export interface ExtractListing {
  external_id: string;
  title: string;
  sku: string;
  price?: number;
  quantity?: number;
  status: string;
  image_url?: string;
  asin?: string;
  raw?: Record<string, unknown>;
}

export interface BrowseListingsResponse {
  ok: boolean;
  listings: ExtractListing[];
  total: number;
  next_cursor?: string;
  note?: string;
  error?: string;
}

export interface ExtractResult {
  extracted: number;
  skipped: number;
  listing_ids: string[];
  errors?: string[];
}

export interface LinkListingRequest {
  product_id: string;
  product_sku?: string;
}

// ── API Methods ───────────────────────────────────────────────────────────────

export const extractApi = {
  /** List channels that have active credentials and support extraction */
  listChannels: () =>
    api.get<{ ok: boolean; channels: ExtractChannel[] }>('/channels'),

  /** Browse live listings from a channel */
  browseListings: (
    channel: string,
    params: {
      credential_id: string;
      limit?: number;
      offset?: number;
      cursor?: string;
      search?: string;
    },
  ) => api.get<BrowseListingsResponse>(`/${channel}/listings`, { params }),

  /** Extract selected listings into MarketMate as imported drafts */
  extractListings: (
    channel: string,
    payload: {
      credential_id: string;
      external_ids: string[];
      product_id?: string;
    },
  ) =>
    api.post<{ ok: boolean; result: ExtractResult }>(
      `/${channel}/listings/extract`,
      payload,
    ),

  /** CLM-02: Link an existing imported listing to an internal product */
  linkListing: (listingId: string, req: LinkListingRequest) =>
    api.post<{ ok: boolean; listing_id: string; product_id: string; message: string }>(
      `/listings/${listingId}/link`,
      req,
    ),
};

export default extractApi;

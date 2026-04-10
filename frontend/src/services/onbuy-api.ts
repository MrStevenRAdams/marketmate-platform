// ============================================================================
// ONBUY API SERVICE
// ============================================================================
// Calls the Go backend's /api/v1/onbuy/* endpoints.

import axios from 'axios';
import { getActiveTenantId } from '../contexts/TenantContext';
import { auth } from '../contexts/AuthContext';

const API_BASE_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';

const api = axios.create({
  baseURL: `${API_BASE_URL}/onbuy`,
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

export interface OnBuyCategory {
  category_id: number;
  name: string;
  parent_id: number;
  has_children: boolean;
  site_id: number;
}

export interface OnBuyCondition {
  condition_id: string;
  name: string;
}

export interface OnBuyListing {
  listing_id?: string;
  opc: string;
  site_id: number;
  condition_id: string;
  price: number;
  stock: number;
  delivery_template_id?: number;
  sku?: string;
  description?: string;
  featured_price?: number;
  group_id?: string;
}

import type { ChannelVariantDraft } from './channel-types';
export type { ChannelVariantDraft };

export interface OnBuyDraft {
  opc: string;
  sku: string;
  description?: string;
  price: number;
  stock: number;
  condition_id: string;
  delivery_template_id: number;
  site_id: number;

  // VAR-01 — Variation listings (Session H)
  variants?: ChannelVariantDraft[];
}

export interface OnBuyConnectRequest {
  account_name?: string;
  consumer_key: string;
  consumer_secret: string;
  site_id?: string;
}

export interface OnBuyTestRequest {
  consumer_key: string;
  consumer_secret: string;
  site_id?: string;
}

export interface OnBuyOrderAddress {
  name: string;
  line_1: string;
  line_2?: string;
  town: string;
  county?: string;
  postcode: string;
  country_code: string;
}

export interface OnBuyOrderLine {
  order_line_id: string;
  opc: string;
  sku: string;
  product_name: string;
  quantity: number;
  unit_price: number;
  line_total: number;
  condition_id: string;
}

export interface OnBuyOrder {
  order_id: string;
  status: string;
  date: string;
  buyer_name: string;
  buyer_email?: string;
  delivery_address: OnBuyOrderAddress;
  lines: OnBuyOrderLine[];
  order_total: number;
  delivery_cost: number;
  currency_code: string;
}

// ── API Methods ───────────────────────────────────────────────────────────────

export const onbuyApi = {
  /** Test connection with provided credentials (before saving) */
  testConnection: (req: OnBuyTestRequest) =>
    api.post<{ ok: boolean; error?: string; message?: string }>('/test', req),

  /** Save OnBuy credentials (tests connection first) */
  connect: (req: OnBuyConnectRequest) =>
    api.post<{ ok: boolean; credential_id?: string; error?: string }>('/connect', req),

  /** Get OnBuy category list */
  getCategories: (credentialId?: string) =>
    api.get<{ ok: boolean; categories: OnBuyCategory[]; error?: string }>('/categories', {
      params: credentialId ? { credential_id: credentialId } : {},
    }),

  /** Get OnBuy conditions list */
  getConditions: (credentialId?: string) =>
    api.get<{ ok: boolean; conditions: OnBuyCondition[]; error?: string }>('/conditions', {
      params: credentialId ? { credential_id: credentialId } : {},
    }),

  /** Build a listing draft from a MarketMate product */
  prepareDraft: (productId: string, credentialId?: string) =>
    api.post<{ ok: boolean; product_id: string; title: string; draft: OnBuyDraft; error?: string }>('/prepare', {
      product_id: productId,
      credential_id: credentialId,
    }),

  /** Create a listing on OnBuy */
  submit: (payload: OnBuyListing & { credential_id?: string }) =>
    api.post<{ ok: boolean; listing_id?: string; error?: string; message?: string }>('/submit', payload),

  /** Update an existing listing by listing_id */
  updateListing: (listingId: string, payload: Record<string, unknown>) =>
    api.put<{ ok: boolean; listing_id?: string; error?: string }>(`/listings/${listingId}`, payload),

  /** Delete a listing by listing_id */
  deleteListing: (listingId: string, credentialId?: string) =>
    api.delete<{ ok: boolean; deleted_id?: string; error?: string }>(
      `/listings/${listingId}`,
      { params: credentialId ? { credential_id: credentialId } : {} },
    ),

  /** List listings from OnBuy (paginated) */
  getListings: (params?: { page?: number; credential_id?: string }) =>
    api.get<{ ok: boolean; listings: OnBuyListing[]; page?: number; error?: string }>('/listings', { params }),

  // Orders

  /** Trigger order import from OnBuy */
  importOrders: (payload: { credential_id?: string; hours_back?: number }) =>
    api.post<{ status: string; hours_back: number; from: string; credential_id: string }>(
      '/orders/import',
      payload,
    ),

  /** List raw OnBuy orders (debug/preview) */
  listOrders: (params?: { credential_id?: string; status?: string; page?: number }) =>
    api.get<{ orders: OnBuyOrder[]; count: number; total: number; error?: string }>('/orders', { params }),

  /** Push tracking to an OnBuy order (dispatch) */
  pushTracking: (
    orderId: string,
    payload: { credential_id?: string; tracking_number: string; carrier?: string },
  ) =>
    api.post<{ ok: boolean; order_id?: string; error?: string }>(`/orders/${orderId}/ship`, payload),

  /** Acknowledge an OnBuy order */
  acknowledgeOrder: (orderId: string, credentialId?: string) =>
    api.post<{ ok: boolean; order_id?: string; message?: string; error?: string }>(
      `/orders/${orderId}/ack`,
      {},
      { params: credentialId ? { credential_id: credentialId } : {} },
    ),
};

export default onbuyApi;

// ============================================================================
// KAUFLAND API SERVICE
// ============================================================================
// Calls the Go backend's /api/v1/kaufland/* endpoints.

import axios from 'axios';
import { getActiveTenantId } from '../contexts/TenantContext';
import { auth } from '../contexts/AuthContext';

const API_BASE_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';

const api = axios.create({
  baseURL: `${API_BASE_URL}/kaufland`,
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

export interface KauflandConnectRequest {
  account_name?: string;
  client_key: string;
  secret_key: string;
}

export interface KauflandTestRequest {
  client_key: string;
  secret_key: string;
}

export interface KauflandCategory {
  category_id: number;
  title: string;
  url: string;
  parent_id: number | null;
}

export interface KauflandCategoryAttribute {
  name: string;
  type: string;
  is_mandatory: boolean;
  values: unknown;
}

export interface KauflandUnit {
  id_offer: string;
  ean: string;
  condition: number;
  listing_price: number;
  minimum_price: number;
  note: string;
  amount: number;
  status: string;
  shipping_group: string;
  handling_time_in_days: number;
  warehouse_code: string;
}

export interface KauflandCreateUnitRequest {
  ean: string;
  condition: number;
  listing_price: number;
  minimum_price?: number;
  note?: string;
  amount: number;
  shipping_group?: string;
  handling_time_in_days?: number;
  credential_id?: string;
}

export interface KauflandUpdateUnitRequest {
  listing_price?: number;
  minimum_price?: number;
  amount?: number;
  note?: string;
  shipping_group?: string;
  handling_time_in_days?: number;
  credential_id?: string;
}

import type { ChannelVariantDraft } from './channel-types';
export type { ChannelVariantDraft };

export interface KauflandPreparedDraft {
  ean: string;
  listing_price: number;
  minimum_price: number;
  amount: number;
  condition: number;
  note: string;
  shipping_group: string;
  handling_time_in_days: number;

  // VAR-01 — Variation listings (Session H)
  variants?: ChannelVariantDraft[];
}

// ── API Methods ───────────────────────────────────────────────────────────────

export const kauflandApi = {
  /** Test connection with provided credentials (before saving) */
  testConnection: (req: KauflandTestRequest) =>
    api.post<{ ok: boolean; error?: string; message?: string }>('/test', req),

  /** Save Kaufland credentials (tests connection first) */
  connect: (req: KauflandConnectRequest) =>
    api.post<{ ok: boolean; credential_id?: string; error?: string }>('/connect', req),

  /** Get all Kaufland categories */
  getCategories: (credentialId?: string) =>
    api.get<{ ok: boolean; categories: KauflandCategory[]; count: number; error?: string }>('/categories', {
      params: credentialId ? { credential_id: credentialId } : {},
    }),

  /** Build a listing draft from a MarketMate product */
  prepareDraft: (productId: string, credentialId?: string) =>
    api.post<{ ok: boolean; product_id: string; draft: KauflandPreparedDraft; error?: string }>('/prepare', {
      product_id: productId,
      credential_id: credentialId,
    }),

  /** Create a unit (listing) on Kaufland */
  submitUnit: (payload: KauflandCreateUnitRequest) =>
    api.post<{ ok: boolean; unit_id?: string; ean?: string; status?: string; message?: string; error?: string }>('/submit', payload),

  /** Update an existing unit */
  updateUnit: (unitId: string, payload: KauflandUpdateUnitRequest) =>
    api.patch<{ ok: boolean; unit_id?: string; message?: string; error?: string }>(`/units/${encodeURIComponent(unitId)}`, payload),

  /** Delete a unit */
  deleteUnit: (unitId: string, credentialId?: string) =>
    api.delete<{ ok: boolean; deleted_unit_id?: string; error?: string }>(`/units/${encodeURIComponent(unitId)}`, {
      params: credentialId ? { credential_id: credentialId } : {},
    }),

  /** List seller units */
  getUnits: (params?: { offset?: number; limit?: number; credential_id?: string }) =>
    api.get<{ ok: boolean; units: KauflandUnit[]; total: number; offset: number; limit: number; error?: string }>('/units', { params }),

  // Orders
  /** Trigger order import from Kaufland */
  importOrders: (payload: { credential_id?: string; hours_back?: number }) =>
    api.post<{ status: string; hours_back: number; from: string; credential_id: string }>('/orders/import', payload),

  /** List raw Kaufland orders (debug) */
  listOrders: (params?: { credential_id?: string; status?: string }) =>
    api.get<{ orders: unknown[]; count: number; error?: string }>('/orders', { params }),

  /** Push tracking number to a Kaufland order unit */
  pushTracking: (orderUnitId: string, payload: {
    credential_id?: string;
    tracking_number: string;
    carrier_code?: string;
  }) => api.post<{ ok: boolean; error?: string }>(`/orders/${encodeURIComponent(orderUnitId)}/ship`, payload),

  /** Update order status (informational) */
  updateOrderStatus: (orderId: string, status: string, credentialId?: string) =>
    api.post<{ ok: boolean; note?: string; error?: string }>(`/orders/${encodeURIComponent(orderId)}/status`, {
      status,
      credential_id: credentialId,
    }),
};

export default kauflandApi;

// ============================================================================
// WALMART API SERVICE
// ============================================================================
// Calls the Go backend's /api/v1/walmart/* endpoints.

import axios from 'axios';
import { getActiveTenantId } from '../contexts/TenantContext';
import { auth } from '../contexts/AuthContext';

const API_BASE_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';

const api = axios.create({
  baseURL: `${API_BASE_URL}/walmart`,
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

export interface WalmartConnectRequest {
  account_name?: string;
  client_id: string;
  client_secret: string;
}

export interface WalmartTestRequest {
  client_id: string;
  client_secret: string;
}

export interface WalmartFeedStatus {
  feedId: string;
  feedStatus: 'RECEIVED' | 'INPROGRESS' | 'PROCESSED' | 'ERROR';
  itemsReceived: number;
  itemsSucceeded: number;
  itemsFailed: number;
  itemsProcessing: number;
}

export interface WalmartItemPrice {
  currency: string;
  amount: number;
}

export interface WalmartItem {
  mart: string;
  sku: string;
  offerId: string;
  itemId: number;
  productName: string;
  price?: WalmartItemPrice;
  shippingWeight?: number;
  publishStatus: string;
  lifecycleStatus: string;
  availabilityStatus: string;
}

export interface WalmartItemsResponse {
  ok: boolean;
  items: WalmartItem[];
  total_items: number;
  next_cursor: string;
  error?: string;
}

export interface WalmartItemPayload {
  sku: string;
  product_name: string;
  price: number;
  quantity: number;
  short_description?: string;
  upc?: string;
  brand?: string;
  model_number?: string;
  category?: string;
  key_features?: string[];
  shipping_weight?: number;
  main_image_url?: string;
  additional_image_urls?: string[];
  credential_id?: string;
}

export interface WalmartPreparedDraft {
  product_name: string;
  short_description: string;
  sku: string | undefined;
  price: number | string | undefined;
  quantity: number | undefined;
  brand: string | undefined;
  images: string[];
  upc: string;
  model_number: string;
  category: string;
  key_features: string[];
  shipping_weight: number | string | undefined;
}

// ── API Methods ───────────────────────────────────────────────────────────────

export const walmartApi = {
  /** Test connection with provided credentials (before saving) */
  testConnection: (req: WalmartTestRequest) =>
    api.post<{ ok: boolean; error?: string; message?: string }>('/test', req),

  /** Save Walmart credentials (tests connection first) */
  connect: (req: WalmartConnectRequest) =>
    api.post<{ ok: boolean; credential_id?: string; error?: string }>('/connect', req),

  /** Build a listing draft from a MarketMate product */
  prepareDraft: (productId: string, credentialId?: string) =>
    api.post<{ ok: boolean; product_id: string; draft: WalmartPreparedDraft; error?: string }>('/prepare', {
      product_id: productId,
      credential_id: credentialId,
    }),

  /** Submit an MP_ITEM feed to Walmart */
  submitItemFeed: (payload: Record<string, unknown>, credentialId?: string) =>
    api.post<{ ok: boolean; feed_id?: string; message?: string; error?: string }>(
      '/submit',
      { ...payload, credential_id: credentialId }
    ),

  /** Poll feed status */
  getFeedStatus: (feedId: string, credentialId?: string) =>
    api.get<{ ok: boolean; status?: WalmartFeedStatus; error?: string }>(`/feeds/${feedId}`, {
      params: credentialId ? { credential_id: credentialId } : {},
    }),

  /** Update inventory for a SKU */
  updateInventory: (sku: string, quantity: number, credentialId?: string) =>
    api.put<{ ok: boolean; sku?: string; quantity?: number; error?: string }>(
      `/items/${encodeURIComponent(sku)}/inventory`,
      { quantity, credential_id: credentialId }
    ),

  /** Update price for a SKU */
  updatePrice: (sku: string, price: number, credentialId?: string) =>
    api.put<{ ok: boolean; sku?: string; price?: number; error?: string }>(
      `/items/${encodeURIComponent(sku)}/price`,
      { price, credential_id: credentialId }
    ),

  /** Retire (remove) an item from Walmart */
  retireItem: (sku: string, credentialId?: string) =>
    api.delete<{ ok: boolean; retired_sku?: string; error?: string }>(
      `/items/${encodeURIComponent(sku)}`,
      { params: credentialId ? { credential_id: credentialId } : {} }
    ),

  /** List seller items */
  getItems: (cursor?: string, credentialId?: string) =>
    api.get<WalmartItemsResponse>('/items', {
      params: {
        ...(cursor ? { cursor } : {}),
        ...(credentialId ? { credential_id: credentialId } : {}),
      },
    }),

  // Orders
  /** Trigger order import from Walmart */
  importOrders: (payload: { credential_id?: string; hours_back?: number }) =>
    api.post<{ status: string; hours_back: number; from: string; credential_id: string }>('/orders/import', payload),

  /** Push tracking number to a Walmart order */
  pushTracking: (purchaseOrderId: string, payload: {
    credential_id?: string;
    tracking_number: string;
    carrier?: string;
    tracking_url?: string;
  }) => api.post<{ ok: boolean; error?: string }>(`/orders/${purchaseOrderId}/ship`, payload),

  /** Acknowledge a Walmart order */
  acknowledgeOrder: (purchaseOrderId: string, credentialId?: string) =>
    api.post<{ ok: boolean; error?: string }>(
      `/orders/${purchaseOrderId}/acknowledge`,
      {},
      { params: credentialId ? { credential_id: credentialId } : {} }
    ),
};

export default walmartApi;

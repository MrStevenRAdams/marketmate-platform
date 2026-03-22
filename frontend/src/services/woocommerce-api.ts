// ============================================================================
// WOOCOMMERCE API SERVICE
// ============================================================================
// Calls the Go backend's /api/v1/woocommerce/* endpoints.

import axios from 'axios';
import { getActiveTenantId } from '../contexts/TenantContext';
import { auth } from '../contexts/AuthContext';

const API_BASE_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';

const api = axios.create({
  baseURL: `${API_BASE_URL}/woocommerce`,
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

export interface WooCategory {
  id: number;
  name: string;
  slug: string;
  parent: number;
  count: number;
}

export interface WooAttribute {
  id: number;
  name: string;
  slug: string;
  type: string;
  order_by: string;
}

export interface WooProductImage {
  id?: number;
  src: string;
  alt?: string;
}

export interface WooProductCategory {
  id: number;
  name?: string;
  slug?: string;
}

export interface WooProductDimensions {
  length?: string;
  width?: string;
  height?: string;
}

export interface WooProduct {
  id?: number;
  name: string;
  slug?: string;
  status?: string;
  type?: string;
  description?: string;
  short_description?: string;
  sku?: string;
  price?: string;
  regular_price?: string;
  sale_price?: string;
  manage_stock?: boolean;
  stock_quantity?: number | null;
  stock_status?: string;
  weight?: string;
  dimensions?: WooProductDimensions;
  categories?: WooProductCategory[];
  images?: WooProductImage[];
  downloadable?: boolean;
  virtual?: boolean;
  permalink?: string;
}

import type { ChannelVariantDraft } from './channel-types';
export type { ChannelVariantDraft };

export interface WooDraft {
  name: string;
  description: string;
  sku: string | undefined;
  regular_price: string | undefined;
  stock_quantity: number | undefined;
  weight: string | undefined;
  dimensions: WooProductDimensions;
  images: string[];
  type: string;
  status: string;
  manage_stock: boolean;

  // VAR-01 — Variation listings (Session H)
  variants?: ChannelVariantDraft[];
}

export interface WooConnectRequest {
  account_name?: string;
  store_url: string;
  consumer_key: string;
  consumer_secret: string;
}

export interface WooTestRequest {
  store_url: string;
  consumer_key: string;
  consumer_secret: string;
}

export interface WooSubmitPayload extends WooProduct {
  credential_id?: string;
}

// ── API Methods ───────────────────────────────────────────────────────────────

export const wooApi = {
  /** Test connection with provided credentials (before saving) */
  testConnection: (req: WooTestRequest) =>
    api.post<{ ok: boolean; error?: string; message?: string; status?: unknown }>('/test', req),

  /** Save WooCommerce credentials (tests connection first) */
  connect: (req: WooConnectRequest) =>
    api.post<{ ok: boolean; credential_id?: string; error?: string }>('/connect', req),

  /** Get all product categories */
  getCategories: (credentialId?: string) =>
    api.get<{ ok: boolean; categories: WooCategory[]; error?: string }>('/categories', {
      params: credentialId ? { credential_id: credentialId } : {},
    }),

  /** Get all product attributes */
  getAttributes: (credentialId?: string) =>
    api.get<{ ok: boolean; attributes: WooAttribute[]; error?: string }>('/attributes', {
      params: credentialId ? { credential_id: credentialId } : {},
    }),

  /** Build a listing draft from a MarketMate product */
  prepareDraft: (productId: string, credentialId?: string) =>
    api.post<{ ok: boolean; product_id: string; draft: WooDraft; error?: string }>('/prepare', {
      product_id: productId,
      credential_id: credentialId,
    }),

  /** Create a product on WooCommerce */
  submit: (payload: WooSubmitPayload) =>
    api.post<{ ok: boolean; product_id?: number; permalink?: string; status?: string; error?: string }>('/submit', payload),

  /** Update an existing product */
  updateProduct: (productId: number, payload: Partial<WooProduct>) =>
    api.put<{ ok: boolean; product_id?: number; status?: string; error?: string }>(`/products/${productId}`, payload),

  /** Delete a product */
  deleteProduct: (productId: number, credentialId?: string) =>
    api.delete<{ ok: boolean; deleted?: number; error?: string }>(`/products/${productId}`, {
      params: credentialId ? { credential_id: credentialId } : {},
    }),

  /** List products from WooCommerce */
  getProducts: (params?: { page?: number; per_page?: number; status?: string; credential_id?: string }) =>
    api.get<{ ok: boolean; products: WooProduct[]; page?: number; per_page?: number; error?: string }>('/products', { params }),

  // Orders
  /** Trigger order import from WooCommerce */
  importOrders: (payload: { credential_id?: string; hours_back?: number }) =>
    api.post<{ status: string; hours_back: number; from: string; credential_id: string }>('/orders/import', payload),

  /** Push tracking to a WooCommerce order */
  pushTracking: (orderId: string | number, payload: {
    credential_id?: string;
    tracking_number: string;
    carrier?: string;
    tracking_url?: string;
  }) => api.post<{ ok: boolean; error?: string }>(`/orders/${orderId}/ship`, payload),

  /** Update WooCommerce order status */
  updateOrderStatus: (orderId: string | number, status: string, credentialId?: string) =>
    api.post<{ ok: boolean; error?: string }>(`/orders/${orderId}/status`, {
      status,
      credential_id: credentialId,
    }),
};

export default wooApi;

// ============================================================================
// BIGCOMMERCE API SERVICE
// ============================================================================
// Calls the Go backend's /api/v1/bigcommerce/* endpoints.

import axios from 'axios';
import { getActiveTenantId } from '../contexts/TenantContext';
import { auth } from '../contexts/AuthContext';

const API_BASE_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';

const api = axios.create({
  baseURL: `${API_BASE_URL}/bigcommerce`,
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

export interface BigCommerceCategory {
  id: number;
  parent_id: number;
  name: string;
  description?: string;
  is_visible: boolean;
  sort_order?: number;
  url?: string;
}

export interface BigCommerceProductImage {
  id?: number;
  product_id?: number;
  is_thumbnail?: boolean;
  sort_order?: number;
  description?: string;
  image_url?: string;
  url_standard?: string;
  url_thumbnail?: string;
}

export interface BigCommerceProductVariant {
  id?: number;
  product_id?: number;
  sku?: string;
  price?: number;
  inventory_level?: number;
  is_visible?: boolean;
}

export interface BigCommerceCustomField {
  id?: number;
  name: string;
  value: string;
}

export interface BigCommerceProduct {
  id?: number;
  name: string;
  type: string;               // "physical" | "digital"
  sku?: string;
  description?: string;
  weight: number;
  price: number;
  sale_price?: number;
  inventory_level?: number;
  inventory_tracking?: string; // "none" | "product" | "variant"
  is_visible: boolean;
  is_featured?: boolean;
  categories?: number[];
  brand_id?: number;
  availability?: string;       // "available" | "disabled" | "preorder"
  condition?: string;          // "New" | "Used" | "Refurbished"
  page_title?: string;
  meta_description?: string;
  search_keywords?: string;
  images?: BigCommerceProductImage[];
  variants?: BigCommerceProductVariant[];
  custom_fields?: BigCommerceCustomField[];
  date_created?: string;
  date_modified?: string;
}

import type { ChannelVariantDraft } from './channel-types';
export type { ChannelVariantDraft };

export interface BigCommerceDraft {
  name: string;
  sku: string;
  description?: string;
  price: number;
  inventory_level: number;
  weight: number;
  type: string;
  is_visible: boolean;
  availability: string;
  condition: string;
  images: string[];

  // VAR-01 — Variation listings (Session H)
  variants?: ChannelVariantDraft[];
}

export interface BigCommerceConnectRequest {
  account_name?: string;
  store_hash: string;
  client_id?: string;
  access_token: string;
}

export interface BigCommerceTestRequest {
  store_hash: string;
  client_id?: string;
  access_token: string;
}

export interface BigCommerceSubmitPayload extends BigCommerceProduct {
  credential_id?: string;
}

export interface BigCommerceOrder {
  id: number;
  date_created: string;
  date_modified?: string;
  status_id: number;
  status: string;
  customer_id: number;
  total_inc_tax: string;
  total_ex_tax: string;
  shipping_cost_inc_tax: string;
  currency_code: string;
  customer_message?: string;
  payment_method?: string;
  payment_status?: string;
  billing_address: {
    first_name: string;
    last_name: string;
    street_1: string;
    city: string;
    state: string;
    zip: string;
    country_iso2: string;
    email?: string;
  };
}

// ── API Methods ───────────────────────────────────────────────────────────────

export const bigcommerceApi = {
  /** Test connection with provided credentials (before saving) */
  testConnection: (req: BigCommerceTestRequest) =>
    api.post<{ ok: boolean; error?: string; message?: string }>('/test', req),

  /** Save BigCommerce credentials (tests connection first) */
  connect: (req: BigCommerceConnectRequest) =>
    api.post<{ ok: boolean; credential_id?: string; error?: string }>('/connect', req),

  /** Get all product categories (flat list) */
  getCategories: (credentialId?: string) =>
    api.get<{ ok: boolean; categories: BigCommerceCategory[]; error?: string }>('/categories', {
      params: credentialId ? { credential_id: credentialId } : {},
    }),

  /** Build a listing draft from a MarketMate product */
  prepareDraft: (productId: string, credentialId?: string) =>
    api.post<{ ok: boolean; product_id: string; draft: BigCommerceDraft; error?: string }>('/prepare', {
      product_id: productId,
      credential_id: credentialId,
    }),

  /** Create a product on BigCommerce */
  submit: (payload: BigCommerceSubmitPayload) =>
    api.post<{ ok: boolean; id?: number; sku?: string; is_visible?: boolean; error?: string }>('/submit', payload),

  /** Update an existing product by numeric ID */
  updateProduct: (id: number, payload: Partial<BigCommerceProduct>) =>
    api.put<{ ok: boolean; id?: number; sku?: string; error?: string }>(
      `/products/${id}`,
      payload,
    ),

  /** Delete a product by numeric ID */
  deleteProduct: (id: number, credentialId?: string) =>
    api.delete<{ ok: boolean; deleted_id?: number; error?: string }>(
      `/products/${id}`,
      { params: credentialId ? { credential_id: credentialId } : {} },
    ),

  /** List products from BigCommerce (paginated) */
  getProducts: (params?: { page?: number; limit?: number; credential_id?: string }) =>
    api.get<{
      ok: boolean;
      products: BigCommerceProduct[];
      total_count?: number;
      page?: number;
      limit?: number;
      total_pages?: number;
      error?: string;
    }>('/products', { params }),

  // Orders

  /** Trigger order import from BigCommerce */
  importOrders: (payload: { credential_id?: string; hours_back?: number }) =>
    api.post<{ status: string; hours_back: number; from: string; credential_id: string }>(
      '/orders/import',
      payload,
    ),

  /** List raw BigCommerce orders (debug/preview) */
  listOrders: (params?: { credential_id?: string; hours_back?: number }) =>
    api.get<{ orders: BigCommerceOrder[]; count: number; error?: string }>('/orders', { params }),

  /** Push tracking to a BigCommerce order (creates shipment) */
  pushTracking: (
    orderId: string | number,
    payload: {
      credential_id?: string;
      tracking_number: string;
      carrier?: string;
      shipping_provider?: string;
      comments?: string;
    },
  ) => api.post<{ ok: boolean; shipment_id?: number; error?: string }>(`/orders/${orderId}/ship`, payload),

  /** Update BigCommerce order status (informational) */
  updateOrderStatus: (orderId: string | number, status: string, credentialId?: string) =>
    api.post<{ ok: boolean; message?: string; error?: string }>(`/orders/${orderId}/status`, {
      status,
      credential_id: credentialId,
    }),
};

export default bigcommerceApi;

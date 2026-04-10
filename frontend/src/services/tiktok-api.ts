// ============================================================================
// TIKTOK SHOP API SERVICE
// ============================================================================
// Calls the Go backend's /api/v1/tiktok/* endpoints.

import axios from 'axios';
import { getActiveTenantId } from '../contexts/TenantContext';
import { auth } from '../contexts/AuthContext';

const API_BASE_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';

const api = axios.create({
  baseURL: `${API_BASE_URL}/tiktok`,
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

export interface TikTokShop {
  id: string;
  name: string;
  region: string;
  type: number;
  cipher: string;
}

export interface TikTokCategory {
  id: number;
  parent_id: number;
  local_name: string;
  is_leaf: boolean;
}

export interface TikTokAttribute {
  id: number;
  name: string;
  type: number; // 1=single, 2=multi, 3=text
  is_mandatory: boolean;
  is_sku: boolean;
  values: { id: number; name: string }[];
  input_type: string;
}

export interface TikTokBrand {
  id: string;
  brand_name: string;
  status: string;
  is_t1_brand: boolean;
}

export interface TikTokShippingTemplate {
  template_id: string;
  name: string;
}

export interface TikTokWarehouse {
  id: string;
  name: string;
  type: number;
}

export interface TikTokSKU {
  seller_sku: string;
  price: { currency: string; original_price: string };
  inventory: { quantity: number; warehouse_id: string }[];
  sales_attributes?: {
    attribute_id: number;
    attribute_value: { id: number; name: string };
    sku_img?: { uri: string };
  }[];
}

import type { ChannelVariantDraft } from './channel-types';
export type { ChannelVariantDraft };

export interface TikTokSubmitPayload {
  title: string;
  description: string;
  category_id: number;
  brand_id?: string;
  main_images: { uri: string }[];
  skus: TikTokSKU[];
  shipping_template_id?: string;
  package_weight?: { unit: string; value: string };
  package_dimensions?: { length: string; width: string; height: string; unit: string };
  product_attributes?: { id: number; values: { id?: number; name: string }[] }[];
  variants?: ChannelVariantDraft[]; // VAR-01
}

export interface TikTokDraft {
  title: string;
  description: string;
  brand: string | null;
  sku: string;
  price: string | number;
  quantity: number;
  images: string[];
  category_id: number;
  attributes: TikTokAttribute[];
  weight_kg: string;
  length_cm: string;
  width_cm: string;
  height_cm: string;

  // VAR-01 — Variation listings (Session H)
  variants?: ChannelVariantDraft[];
}

// ── API Methods ───────────────────────────────────────────────────────────────

export const tiktokApi = {
  /** GET /tiktok/shops — list authorized shops */
  getShops: (credentialId?: string) =>
    api.get<{ ok: boolean; shops: TikTokShop[] }>('/shops', {
      params: credentialId ? { credential_id: credentialId } : {},
    }),

  /** GET /tiktok/categories — full category tree */
  getCategories: (credentialId?: string) =>
    api.get<{ ok: boolean; categories: TikTokCategory[] }>('/categories', {
      params: credentialId ? { credential_id: credentialId } : {},
    }),

  /** GET /tiktok/categories/:id/attributes */
  getCategoryAttributes: (categoryId: number, credentialId?: string) =>
    api.get<{ ok: boolean; attributes: TikTokAttribute[] }>(
      `/categories/${categoryId}/attributes`,
      { params: credentialId ? { credential_id: credentialId } : {} }
    ),

  /** GET /tiktok/brands */
  getBrands: (credentialId?: string) =>
    api.get<{ ok: boolean; brands: TikTokBrand[] }>('/brands', {
      params: credentialId ? { credential_id: credentialId } : {},
    }),

  /** GET /tiktok/shipping-templates */
  getShippingTemplates: (credentialId?: string) =>
    api.get<{ ok: boolean; templates: TikTokShippingTemplate[] }>('/shipping-templates', {
      params: credentialId ? { credential_id: credentialId } : {},
    }),

  /** GET /tiktok/warehouses */
  getWarehouses: (credentialId?: string) =>
    api.get<{ ok: boolean; warehouses: TikTokWarehouse[] }>('/warehouses', {
      params: credentialId ? { credential_id: credentialId } : {},
    }),

  /** POST /tiktok/images/upload — upload image from URL to TikTok CDN */
  uploadImage: (url: string) =>
    api.post<{ ok: boolean; image: { uri: string; width: number; height: number }; error?: string }>(
      '/images/upload',
      { url }
    ),

  /** POST /tiktok/prepare — load product + build draft */
  prepare: (data: { product_id: string; category_id?: number; credential_id?: string }) =>
    api.post<{ ok: boolean; product_id: string; draft: TikTokDraft; error?: string }>('/prepare', data),

  /** POST /tiktok/submit — create product on TikTok Shop */
  submit: (payload: TikTokSubmitPayload) =>
    api.post<{ ok: boolean; product_id: string; sku_list: any[]; error?: string }>('/submit', payload),

  /** PUT /tiktok/products/:id — update existing product */
  updateProduct: (productId: string, payload: TikTokSubmitPayload) =>
    api.put<{ ok: boolean; product_id: string; error?: string }>(`/products/${productId}`, payload),

  /** DELETE /tiktok/products/:id */
  deleteProduct: (productId: string) =>
    api.delete<{ ok: boolean; deleted: string }>(`/products/${productId}`),

  /** GET /tiktok/products */
  getProducts: (pageToken?: string, pageSize?: number) =>
    api.get<{ ok: boolean; products: any[]; next_page_token: string; total: number }>('/products', {
      params: { page_token: pageToken, page_size: pageSize },
    }),

  /** POST /tiktok/orders/import */
  importOrders: (data: { credential_id?: string; hours_back?: number }) =>
    api.post('/orders/import', data),

  /** POST /tiktok/orders/:id/ship */
  pushTracking: (orderId: string, data: { tracking_number: string; carrier?: string; package_id?: string }) =>
    api.post(`/orders/${orderId}/ship`, data),

  /** POST /tiktok/orders/:id/cancel */
  cancelOrder: (orderId: string, data: { cancel_reason?: string; cancel_reason_key?: string }) =>
    api.post(`/orders/${orderId}/cancel`, data),

  /** GET /tiktok/oauth/login */
  getOAuthURL: (accountName: string) =>
    fetch(`${API_BASE_URL}/tiktok/oauth/login?account_name=${encodeURIComponent(accountName)}`, {
      headers: { 'X-Tenant-Id': getActiveTenantId() },
    }).then((r) => r.json() as Promise<{ ok: boolean; consent_url: string; error?: string }>),
};

// ============================================================================
// MAGENTO 2 API SERVICE
// ============================================================================
// Calls the Go backend's /api/v1/magento/* endpoints.

import axios from 'axios';
import { getActiveTenantId } from '../contexts/TenantContext';
import { auth } from '../contexts/AuthContext';

const API_BASE_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';

const api = axios.create({
  baseURL: `${API_BASE_URL}/magento`,
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

export interface MagentoCategory {
  id: number;
  parent_id: number;
  name: string;
  is_active: boolean;
  position?: number;
  level: number;
  product_count?: number;
  children_data?: MagentoCategory[];
}

export interface MagentoStockItem {
  item_id?: number;
  product_id?: number;
  qty: number;
  is_in_stock: boolean;
  manage_stock: boolean;
}

export interface MagentoProductExtensionAttributes {
  stock_item?: MagentoStockItem;
}

export interface MagentoCustomAttribute {
  attribute_code: string;
  value: unknown;
}

export interface MagentoMediaEntry {
  id?: number;
  media_type: string;
  label?: string;
  position: number;
  disabled: boolean;
  types?: string[];
  file?: string;
}

export interface MagentoProduct {
  id?: number;
  sku: string;
  name: string;
  attribute_set_id?: number;
  price: number;
  status?: number;       // 1=enabled, 2=disabled
  visibility?: number;   // 1=Not Visible, 2=Catalog, 3=Search, 4=Both
  type_id?: string;      // simple, configurable, virtual, etc.
  created_at?: string;
  updated_at?: string;
  weight?: number;
  extension_attributes?: MagentoProductExtensionAttributes;
  custom_attributes?: MagentoCustomAttribute[];
  media_gallery_entries?: MagentoMediaEntry[];
}

import type { ChannelVariantDraft } from './channel-types';
export type { ChannelVariantDraft };

export interface MagentoDraft {
  sku: string;
  name: string;
  description?: string;
  price: number;
  stock_quantity: number;
  weight?: number;
  images: string[];
  status: number;
  visibility: number;
  type_id: string;
  attribute_set_id: number;

  // VAR-01 — Variation listings (Session H)
  variants?: ChannelVariantDraft[];
}

export interface MagentoConnectRequest {
  account_name?: string;
  store_url: string;
  integration_token: string;
}

export interface MagentoTestRequest {
  store_url: string;
  integration_token: string;
}

export interface MagentoSubmitPayload extends MagentoProduct {
  credential_id?: string;
}

export interface MagentoOrderFull {
  entity_id?: number;
  increment_id: string;
  status: string;
  state?: string;
  created_at?: string;
  customer_email?: string;
  customer_firstname?: string;
  customer_lastname?: string;
  grand_total: number;
  shipping_amount?: number;
  order_currency_code?: string;
  items?: Array<{
    item_id?: number;
    sku: string;
    name: string;
    qty_ordered: number;
    price: number;
    row_total?: number;
  }>;
}

// ── API Methods ───────────────────────────────────────────────────────────────

export const magentoApi = {
  /** Test connection with provided credentials (before saving) */
  testConnection: (req: MagentoTestRequest) =>
    api.post<{ ok: boolean; error?: string; message?: string }>('/test', req),

  /** Save Magento credentials (tests connection first) */
  connect: (req: MagentoConnectRequest) =>
    api.post<{ ok: boolean; credential_id?: string; error?: string }>('/connect', req),

  /** Get all product categories (flat list) */
  getCategories: (credentialId?: string) =>
    api.get<{ ok: boolean; categories: MagentoCategory[]; error?: string }>('/categories', {
      params: credentialId ? { credential_id: credentialId } : {},
    }),

  /** Build a listing draft from a MarketMate product */
  prepareDraft: (productId: string, credentialId?: string) =>
    api.post<{ ok: boolean; product_id: string; draft: MagentoDraft; error?: string }>('/prepare', {
      product_id: productId,
      credential_id: credentialId,
    }),

  /** Create a product on Magento */
  submit: (payload: MagentoSubmitPayload) =>
    api.post<{ ok: boolean; sku?: string; id?: number; status?: number; error?: string }>('/submit', payload),

  /** Update an existing product */
  updateProduct: (sku: string, payload: Partial<MagentoProduct>) =>
    api.put<{ ok: boolean; sku?: string; id?: number; status?: number; error?: string }>(
      `/products/${encodeURIComponent(sku)}`,
      payload,
    ),

  /** Delete a product */
  deleteProduct: (sku: string, credentialId?: string) =>
    api.delete<{ ok: boolean; deleted_sku?: string; error?: string }>(
      `/products/${encodeURIComponent(sku)}`,
      { params: credentialId ? { credential_id: credentialId } : {} },
    ),

  /** List products from Magento (paginated) */
  getProducts: (params?: { page?: number; page_size?: number; credential_id?: string }) =>
    api.get<{
      ok: boolean;
      products: MagentoProduct[];
      total_count?: number;
      page?: number;
      page_size?: number;
      error?: string;
    }>('/products', { params }),

  // Orders
  /** Trigger order import from Magento */
  importOrders: (payload: { credential_id?: string; hours_back?: number }) =>
    api.post<{ status: string; hours_back: number; from: string; credential_id: string }>(
      '/orders/import',
      payload,
    ),

  /** List raw Magento orders (debug) */
  listOrders: (params?: { credential_id?: string; hours_back?: number; status?: string }) =>
    api.get<{ orders: MagentoOrderFull[]; count: number; error?: string }>('/orders', { params }),

  /** Push tracking to a Magento order (creates shipment) */
  pushTracking: (
    orderId: string | number,
    payload: {
      credential_id?: string;
      tracking_number: string;
      carrier?: string;
      carrier_title?: string;
    },
  ) => api.post<{ ok: boolean; error?: string }>(`/orders/${orderId}/ship`, payload),

  /** Update Magento order status */
  updateOrderStatus: (orderId: string | number, status: string, credentialId?: string) =>
    api.post<{ ok: boolean; message?: string; error?: string }>(`/orders/${orderId}/status`, {
      status,
      credential_id: credentialId,
    }),
};

export default magentoApi;

// ============================================================================
// SHOPIFY API SERVICE
// ============================================================================

import axios from 'axios';
import { getActiveTenantId } from '../contexts/TenantContext';
import { auth } from '../contexts/AuthContext';
import type { ChannelVariantDraft } from './channel-types';

export type { ChannelVariantDraft };

const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';

const api = axios.create({
  baseURL: `${API_BASE}/shopify`,
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

// ── Types ──────────────────────────────────────────────────────────────────

export interface ShopifyPricingTier {
  minQty: number;
  pricePerUnit: string;
}

export interface ShopifyDraft {
  title: string;
  description: string;
  vendor: string;
  productType: string;
  tags: string;
  sku: string;
  barcode: string;
  price: string;
  compareAtPrice: string;
  quantity: string;
  weightValue: string;
  weightUnit: string;
  images: string[];
  imageAlts: string[];
  status: string;
  pricingTiers: ShopifyPricingTier[];
  metafields: Array<{
    namespace: string;
    key: string;
    value: string;
    type: string;
  }>;
  paymentMethods: string[];
  bulletPoints: string[];
  isUpdate: boolean;
  existingProductId: string;
  existingVariantId: string;
  variants?: ChannelVariantDraft[];

  // New fields
  taxable?: boolean;
  requiresShipping?: boolean;
  unitPriceMeasure?: string;
  unitPriceMeasurementUnit?: string;
  unitPriceQuantityUnit?: string;
  costPerItem?: string;
  inventoryLocationId?: string;
  inventoryManaged?: boolean;
  countryOfOrigin?: string;
  hsCode?: string;
  categoryId?: string;
  categoryName?: string;
  publicationIds?: string[];
  seoTitle?: string;
  seoDescription?: string;
  tagsList?: string[];
  [key: string]: any; // allow ExtendedDraft passthrough
}

export interface ShopifyPrepareResponse {
  ok: boolean;
  error?: string;
  product?: Record<string, unknown>;
  draft?: ShopifyDraft;
  debugErrors?: string[];
}

export interface ShopifySubmitResponse {
  ok: boolean;
  error?: string;
  shopifyProductId?: string;
  shopifyVariantId?: string;
  url?: string;
  priceRulesCreated: number;
  warnings?: string[];
}

export interface ShopifyOAuthLoginResponse {
  ok: boolean;
  consent_url?: string;
  error?: string;
}

export interface ShopifyOrdersResponse {
  ok: boolean;
  orders?: unknown[];
  count?: number;
  error?: string;
}

export interface ShopifyImportResponse {
  ok: boolean;
  imported: number;
  total: number;
  errors?: string[];
}

// ── API Methods ────────────────────────────────────────────────────────────

export const shopifyApi = {
  // OAuth
  getOAuthLoginUrl(shop: string, accountName?: string) {
    return axios.get<ShopifyOAuthLoginResponse>(`${API_BASE}/shopify/oauth/login`, {
      params: { shop, account_name: accountName },
      headers: { 'X-Tenant-Id': getActiveTenantId() },
    });
  },

  // Connection test
  testConnection(credentialId?: string) {
    return api.get('/test', { params: { credential_id: credentialId } });
  },

  // Listing
  prepare(payload: { product_id: string; credential_id?: string }) {
    return api.post<ShopifyPrepareResponse>('/prepare', payload);
  },

  submit(payload: {
    product_id: string;
    credential_id?: string;
    draft: ShopifyDraft;
    publish: boolean;
  }) {
    return api.post<ShopifySubmitResponse>('/submit', payload);
  },

  // Orders
  importOrders(payload: {
    credential_id?: string;
    status?: string;
    limit?: number;
    created_at_min?: string;
  }) {
    return api.post<ShopifyImportResponse>('/orders/import', payload);
  },

  getOrders(credentialId?: string, limit = 50) {
    return api.get<ShopifyOrdersResponse>('/orders', {
      params: { credential_id: credentialId, limit },
    });
  },

  markShipped(orderId: string, payload: {
    credential_id?: string;
    tracking_number: string;
    tracking_company?: string;
    notify_customer?: boolean;
  }) {
    return api.post(`/orders/${orderId}/ship`, payload);
  },

  // Stock
  updateStock(payload: {
    credential_id?: string;
    inventory_item_id?: number;
    sku?: string;
    quantity: number;
    location_id?: number;
  }) {
    return api.post('/stock', payload);
  },

  // Webhooks
  registerWebhooks(credentialId?: string) {
    return api.post('/webhooks/register', { credential_id: credentialId });
  },
};

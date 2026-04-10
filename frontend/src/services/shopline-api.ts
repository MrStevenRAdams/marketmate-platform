// ============================================================================
// SHOPLINE API SERVICE
// ============================================================================

import axios from 'axios';
import { getActiveTenantId } from '../contexts/TenantContext';
import { auth } from '../contexts/AuthContext';
import type { ChannelVariantDraft } from './channel-types';

export type { ChannelVariantDraft };

const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';

const api = axios.create({
  baseURL: `${API_BASE}/shopline`,
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

export interface ShoplinePricingTier {
  minQty: number;
  pricePerUnit: string;
}

export interface ShoplineDraft {
  title: string;
  description: string;
  vendor: string;
  productType: string;
  tags: string;
  tagsList: string[];
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
  pricingTiers: ShoplinePricingTier[];
  bulletPoints: string[];
  isUpdate: boolean;
  existingProductId: string;
  existingVariantId: string;
  variants?: ChannelVariantDraft[];

  // Shopline-specific
  customAttributes?: Array<{
    key: string;
    value: string;
    type: string;
  }>;

  // Inventory
  taxable?: boolean;
  requiresShipping?: boolean;
  inventoryManaged?: boolean;
  inventoryLocationId?: string;

  // Shipping / compliance
  countryOfOrigin?: string;
  hsCode?: string;
  costPerItem?: string;

  // SEO
  seoTitle?: string;
  seoDescription?: string;
  seoHandle?: string;

  // Category / organisation
  categoryId?: string;
  categoryName?: string;
  collectionIds?: string[];
  channelIds?: string[];

  [key: string]: any;
}

export interface ShoplinePrepareResponse {
  ok: boolean;
  error?: string;
  product?: Record<string, unknown>;
  draft?: ShoplineDraft;
  debugErrors?: string[];
}

export interface ShoplineSubmitResponse {
  ok: boolean;
  error?: string;
  shoplineProductId?: string;
  shoplineVariantId?: string;
  url?: string;
  pricingRulesCreated: number;
  warnings?: string[];
}

export interface ShoplineOAuthLoginResponse {
  ok: boolean;
  consent_url?: string;
  error?: string;
}

export interface ShoplineOrdersResponse {
  ok: boolean;
  orders?: unknown[];
  count?: number;
  error?: string;
}

export interface ShoplineImportResponse {
  ok: boolean;
  imported: number;
  total: number;
  errors?: string[];
}

export interface ShoplineLocation {
  id: string;
  name: string;
  address1?: string;
  city?: string;
  country?: string;
  active: boolean;
}

export interface ShoplineCollection {
  id: string;
  title: string;
  handle: string;
  type?: 'manual' | 'smart';
}

export interface ShoplineChannel {
  id: string;
  name: string;
  type?: string;
}

export interface ShoplineCategory {
  id: string;
  full_name: string;
}

// ── API Methods ────────────────────────────────────────────────────────────

export const shoplineApi = {
  // OAuth
  getOAuthLoginUrl(shop: string, accountName?: string) {
    return axios.get<ShoplineOAuthLoginResponse>(`${API_BASE}/shopline/oauth/login`, {
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
    return api.post<ShoplinePrepareResponse>('/prepare', payload);
  },

  submit(payload: {
    product_id: string;
    credential_id?: string;
    draft: ShoplineDraft;
    publish: boolean;
  }) {
    return api.post<ShoplineSubmitResponse>('/submit', payload);
  },

  // Orders
  importOrders(payload: {
    credential_id?: string;
    status?: string;
    limit?: number;
    created_at_min?: string;
  }) {
    return api.post<ShoplineImportResponse>('/orders/import', payload);
  },

  getOrders(credentialId?: string, limit = 50) {
    return api.get<ShoplineOrdersResponse>('/orders', {
      params: { credential_id: credentialId, limit },
    });
  },

  shipOrder(orderId: string, payload: {
    credential_id?: string;
    tracking_number: string;
    tracking_company: string;
    notify_customer?: boolean;
  }) {
    return api.post(`/orders/${orderId}/ship`, payload);
  },

  // Stock
  updateStock(payload: {
    credential_id?: string;
    variant_id?: string;
    sku?: string;
    quantity: number;
    location_id?: string;
  }) {
    return api.post('/stock', payload);
  },

  // Webhooks
  registerWebhooks(credentialId?: string) {
    return api.post('/webhooks/register', { credential_id: credentialId });
  },

  // Store data
  getLocations(credentialId?: string) {
    return api.get<{ ok: boolean; locations: ShoplineLocation[] }>('/locations', {
      params: { credential_id: credentialId },
    });
  },

  getChannels(credentialId?: string) {
    return api.get<{ ok: boolean; channels: ShoplineChannel[] }>('/channels', {
      params: { credential_id: credentialId },
    });
  },

  getTags(credentialId?: string) {
    return api.get<{ ok: boolean; tags: string[] }>('/tags', {
      params: { credential_id: credentialId },
    });
  },

  getTypes(credentialId?: string) {
    return api.get<{ ok: boolean; types: string[] }>('/types', {
      params: { credential_id: credentialId },
    });
  },

  getCollections(credentialId?: string) {
    return api.get<{ ok: boolean; collections: ShoplineCollection[] }>('/collections', {
      params: { credential_id: credentialId },
    });
  },

  getCategories(credentialId?: string, search?: string) {
    return api.get<{ ok: boolean; categories: ShoplineCategory[] }>('/categories', {
      params: { credential_id: credentialId, search },
    });
  },
};

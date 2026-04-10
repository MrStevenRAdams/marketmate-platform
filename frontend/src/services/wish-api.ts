// ============================================================================
// WISH API SERVICE
// ============================================================================
// Calls the Go backend's /api/v1/wish/* endpoints.
// Auth: Access token — stored as credential field "access_token".
// Pattern mirrors bigcommerce-api.ts exactly.

import axios from 'axios';
import { getActiveTenantId } from '../contexts/TenantContext';
import { auth } from '../contexts/AuthContext';

const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';

const api = axios.create({
  baseURL: `${API_BASE}/wish`,
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

export interface WishVariant {
  sku: string;
  price: number;
  shipping: number;
  inventory: number;
  weight: number;       // grams
  landed_cost: number;
  main_image: string;
  enabled: boolean;
  [key: string]: unknown;
}

export interface WishDraft {
  name: string;
  description: string;
  sku: string;
  price: number;
  shipping: number;
  inventory: number;
  weight: number;       // grams
  brand: string;
  main_image: string;
  extra_images: string[];
  tags: string;
  is_shipping_only: boolean;
  enabled: boolean;
  variants: WishVariant[];
  note?: string;
}

export interface WishSubmitPayload {
  name: string;
  description?: string;
  sku?: string;
  price: number;
  shipping?: number;
  inventory: number;
  weight?: number;
  brand?: string;
  main_image?: string;
  extra_images?: string[];
  tags?: string;
  enabled?: boolean;
  variants?: WishVariant[];
  product_id?: string;  // set to update an existing product
  credential_id?: string;
  [key: string]: unknown;
}

export interface WishConnectRequest {
  account_name?: string;
  access_token: string;
}

export interface WishSubmitResult {
  ok: boolean;
  product_id?: string;
  message?: string;
  error?: string;
  result?: Record<string, unknown>;
}

export interface WishOrderImportResult {
  ok: boolean;
  status?: string;
  hours_back?: number;
  credential_id?: string;
  message?: string;
  error?: string;
}

// ── API Methods ───────────────────────────────────────────────────────────────

export const wishApi = {
  /** Test + save Wish access token credentials */
  connect: (req: WishConnectRequest) =>
    api.post<{ ok: boolean; credential_id?: string; error?: string }>('/connect', req),

  /** Build a listing draft from a MarketMate product */
  prepareDraft: (productId: string, credentialId?: string) =>
    api.post<{ ok: boolean; product_id: string; draft: WishDraft; note?: string; error?: string }>(
      '/listings/prepare',
      { product_id: productId, credential_id: credentialId },
    ),

  /** Create or update a Wish product */
  submit: (payload: WishSubmitPayload) =>
    api.post<WishSubmitResult>('/listings/submit', payload),

  /** Trigger a Wish order import */
  importOrders: (params?: { credential_id?: string; hours_back?: number }) =>
    api.get<WishOrderImportResult>('/orders/import', { params }),
};

export default wishApi;

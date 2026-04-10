// ============================================================================
// BLUEPARK API SERVICE
// ============================================================================
// Calls the Go backend's /api/v1/bluepark/* endpoints.
// Auth: API Key — stored as credential field "api_key".
// Pattern mirrors bigcommerce-api.ts exactly.

import axios from 'axios';
import { getActiveTenantId } from '../contexts/TenantContext';
import { auth } from '../contexts/AuthContext';
import type { ChannelVariantDraft } from './channel-types';
export type { ChannelVariantDraft };

const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';

const api = axios.create({
  baseURL: `${API_BASE}/bluepark`,
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

export interface BlueparkDraft {
  name: string;
  sku: string;
  description: string;
  price: number;
  quantity: number;
  weight: number;       // kg
  barcode: string;
  brand: string;
  images: string[];
  status: string;       // "active" | "inactive"
  condition: string;    // "new" | "used" | "refurbished"
  variants?: ChannelVariantDraft[];  // VAR-01
}

export interface BlueparkSubmitPayload {
  name: string;
  sku?: string;
  description?: string;
  price: number;
  quantity: number;
  weight?: number;
  barcode?: string;
  brand?: string;
  images?: string[];
  status?: string;
  condition?: string;
  credential_id?: string;
  product_id?: string;  // set to update an existing product
  [key: string]: unknown;
}

export interface BlueparkConnectRequest {
  account_name?: string;
  api_key: string;
}

export interface BlueparkSubmitResult {
  ok: boolean;
  product_id?: string;
  message?: string;
  error?: string;
  result?: Record<string, unknown>;
}

export interface BlueparkOrderImportResult {
  ok: boolean;
  status?: string;
  hours_back?: number;
  credential_id?: string;
  message?: string;
  error?: string;
}

// ── API Methods ───────────────────────────────────────────────────────────────

export const blueparkApi = {
  /** Test + save Bluepark API key credentials */
  connect: (req: BlueparkConnectRequest) =>
    api.post<{ ok: boolean; credential_id?: string; error?: string }>('/connect', req),

  /** Build a listing draft from a MarketMate product */
  prepareDraft: (productId: string, credentialId?: string) =>
    api.post<{ ok: boolean; product_id: string; draft: BlueparkDraft; error?: string }>(
      '/listings/prepare',
      { product_id: productId, credential_id: credentialId },
    ),

  /** Create or update a Bluepark product */
  submit: (payload: BlueparkSubmitPayload) =>
    api.post<BlueparkSubmitResult>('/listings/submit', payload),

  /** Trigger a Bluepark order import */
  importOrders: (params?: { credential_id?: string; hours_back?: number }) =>
    api.get<BlueparkOrderImportResult>('/orders/import', { params }),
};

export default blueparkApi;

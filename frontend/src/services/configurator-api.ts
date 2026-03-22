// ============================================================================
// CONFIGURATOR API SERVICE — SESSION 1 (CFG-01, CFG-02, CFG-03)
// ============================================================================
// All types and request payloads match the Go backend exactly:
//   - models/configurator.go   (struct definitions + JSON tags)
//   - handlers/configurator_handler.go (endpoint paths)
//   - main.go                  (route registration under /api/v1)
// ============================================================================

import axios from 'axios';
import { getActiveTenantId } from '../contexts/TenantContext';
import { auth } from '../contexts/AuthContext';

const API_BASE_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';

const apiClient = axios.create({
  baseURL: API_BASE_URL,
  headers: { 'Content-Type': 'application/json' },
});

apiClient.interceptors.request.use(
  async (config) => {
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
},
  (error) => Promise.reject(error),
);

// ============================================================================
// TYPES — match Go models/configurator.go exactly
// ============================================================================

export interface AttributeDefault {
  attribute_name: string;
  source: 'extended_property' | 'default_value';
  ep_key?: string;
  default_value?: string;
}

export interface Configurator {
  configurator_id: string;
  tenant_id: string;
  name: string;
  channel: string;
  channel_credential_id?: string;
  category_id?: string;
  category_path?: string;
  shipping_defaults?: Record<string, string>;
  attribute_defaults?: AttributeDefault[];
  variation_schema?: string[];
  created_at: string;
  updated_at: string;
}

export interface ConfiguratorWithStats extends Configurator {
  listing_count: number;
  error_count: number;
  in_process_count: number;
}

export interface ConfiguratorDetail extends Configurator {
  linked_listings: LinkedListing[];
}

export interface LinkedListing {
  listing_id: string;
  product_sku?: string;
  product_title?: string;
  channel?: string;
  state?: string;
  [key: string]: any;
}

export interface ReviseJob {
  job_id: string;
  configurator_id: string;
  fields: string[];
  status: string;
  total: number;
  succeeded: number;
  failed: number;
  errors?: string[];
  created_at: string;
}

export type ReviseField =
  | 'title'
  | 'description'
  | 'price'
  | 'attributes'
  | 'images'
  | 'category'
  | 'shipping';

// ============================================================================
// SERVICE
// ============================================================================

export const configuratorService = {
  /** GET /configurators — list all with stats, optional channel filter */
  list: (params?: { channel?: string }) =>
    apiClient.get<{ configurators: ConfiguratorWithStats[]; total: number }>(
      '/configurators',
      { params },
    ),

  /** GET /configurators/:id — full detail + linked listings */
  get: (id: string) =>
    apiClient.get<{ configurator: ConfiguratorDetail }>(`/configurators/${id}`),

  /** POST /configurators — create new */
  create: (data: Partial<Configurator>) =>
    apiClient.post<{ configurator: Configurator }>('/configurators', data),

  /** PUT /configurators/:id — full replace */
  update: (id: string, data: Partial<Configurator>) =>
    apiClient.put<{ configurator: Configurator }>(`/configurators/${id}`, data),

  /** DELETE /configurators/:id */
  delete: (id: string, force = false) =>
    apiClient.delete(`/configurators/${id}`, { params: force ? { force: 'true' } : {} }),

  /** POST /configurators/:id/duplicate */
  duplicate: (id: string) =>
    apiClient.post<{ configurator: Configurator }>(`/configurators/${id}/duplicate`),

  /** POST /configurators/:id/revise — bulk push fields to linked listings */
  revise: (id: string, fields: ReviseField[]) =>
    apiClient.post<{ job: ReviseJob }>(`/configurators/${id}/revise`, { fields }),

  /** POST /configurators/:id/assign-listings */
  assignListings: (id: string, listingIds: string[]) =>
    apiClient.post(`/configurators/${id}/assign-listings`, { listing_ids: listingIds }),

  /** POST /configurators/:id/remove-listings */
  removeListings: (id: string, listingIds: string[]) =>
    apiClient.post(`/configurators/${id}/remove-listings`, { listing_ids: listingIds }),

  /** POST /configurators/auto-select */
  autoSelect: (channel: string, categoryId?: string) =>
    apiClient.post<{ configurator_id: string | null }>('/configurators/auto-select', {
      channel,
      category_id: categoryId || '',
    }),

  /** POST /configurators/ai-setup — USP-01
   * Sends channel + product description to AI, returns suggested configurator settings. */
  aiSetup: (channel: string, productDescription: string, credentialId?: string) =>
    apiClient.post<{
      ok: boolean;
      suggestion?: {
        category_id: string;
        category_path: string;
        attribute_defaults: Array<{
          attribute_name: string;
          source: 'default_value' | 'extended_property';
          ep_key?: string;
          default_value?: string;
        }>;
        shipping_defaults: Record<string, string>;
        reasoning: string;
      };
      error?: string;
      raw_response?: string;
    }>('/configurators/ai-setup', {
      channel,
      product_description: productDescription,
      credential_id: credentialId || '',
    }),
};

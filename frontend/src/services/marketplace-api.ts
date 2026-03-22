// ============================================================================
// MARKETPLACE API SERVICE - Module B (FIXED)
// ============================================================================
// Location: frontend/src/services/marketplace-api.ts
//
// All types and request payloads match the Go backend exactly:
//   - models/marketplace.go  (struct definitions + JSON tags)
//   - handlers/marketplace_handler.go  (endpoint paths)
//   - main.go  (route registration)

import axios from 'axios';
import { getActiveTenantId } from '../contexts/TenantContext';
import { auth } from '../contexts/AuthContext';

const API_BASE_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';

const apiClient = axios.create({
  baseURL: API_BASE_URL,
  headers: {
    'Content-Type': 'application/json',
  },
});

// Inject X-Tenant-Id and Authorization on every request
apiClient.interceptors.request.use(
  async (config) => {
    config.headers['X-Tenant-Id'] = getActiveTenantId();

    // Attach Firebase auth token
    try {
      if (auth.currentUser) {
        const token = await auth.currentUser.getIdToken();
        config.headers['Authorization'] = `Bearer ${token}`;
      }
    } catch (e) {
      console.error('[marketplace-api] failed to get auth token:', e);
    }

    return config;
  },
  (error) => Promise.reject(error),
);

// ============================================================================
// TYPES - Match Go structs in models/marketplace.go
// ============================================================================

/** Matches: models.MarketplaceCredential */
export interface MarketplaceCredential {
  credential_id: string;
  tenant_id: string;
  channel: string;                // "amazon", "ebay", "temu", etc.
  account_name: string;
  marketplace_id?: string;
  environment: string;            // "production" | "sandbox"
  credential_data?: Record<string, string>;
  active: boolean;                // boolean, not a status string
  last_tested_at?: string;
  last_test_status?: string;      // "success" | "failed"
  last_error_message?: string;
  created_at: string;
  updated_at: string;
}

/** Matches: models.ConnectMarketplaceRequest */
export interface ConnectMarketplaceRequest {
  channel: string;                // REQUIRED
  account_name: string;           // REQUIRED
  marketplace_id?: string;
  environment: string;            // REQUIRED - "production" or "sandbox"
  credentials: Record<string, string>;  // REQUIRED
}

/** Matches: models.ImportJob */
export interface ImportJob {
  job_id: string;
  tenant_id: string;
  job_type: string;               // "full_import", "selective", "scheduled"
  channel: string;
  channel_account_id: string;
  external_ids?: string[];
  status: string;                 // "pending", "running", "completed", "failed", "cancelled"
  status_message?: string;        // Human-readable progress description
  report_id?: string;             // Amazon report ID for tracking
  total_items: number;
  processed_items: number;
  successful_items: number;
  failed_items: number;
  skipped_items: number;
  enrich_data?: boolean;
  enrich_total_items?: number;
  enriched_items?: number;
  enrich_failed_items?: number;
  enrich_skipped_items?: number;
  imported_products?: string[];
  error_log?: ImportError[];
  started_at?: string;
  completed_at?: string;
  created_at: string;
  updated_at: string;
  // Second-import matching flow
  match_status?: 'analyzing' | 'review_required' | 'no_review_needed' | 'reviewed';
  match_result_count?: number;
}

export interface ImportError {
  external_id: string;
  error_code: string;
  message?: string;
  error_message?: string;
  timestamp?: string;
}

/** Matches: models.ImportProductsRequest */
export interface StartImportRequest {
  channel: string;                // REQUIRED
  channel_account_id: string;     // REQUIRED - the credential_id
  job_type: string;               // REQUIRED - "full_import" or "selective"
  external_ids?: string[];        // For selective import (ASINs, etc.)
  fulfillment_filter?: string;    // "all", "fba", "merchant"
  stock_filter?: string;          // "all", "in_stock"
  ai_optimize?: boolean;          // Enable AI title/description optimization
  enrich_data?: boolean;          // Fetch extended data (images, bullets, dimensions)
  auto_map?: boolean;
  temu_status_filters?: number[]; // Temu goodsStatusFilterType: 1=Active/Inactive, 4=Incomplete, 5=Draft, 6=Deleted
  ebay_list_types?: string[];    // eBay Trading API list types: ActiveList, UnsoldList, SoldList
  sync_stock?: boolean;          // Import channel quantity to default warehouse (default: false)
}

/** Matches: models.Listing */
export interface Listing {
  listing_id: string;
  tenant_id: string;
  product_id: string;
  variant_id?: string;
  channel: string;
  channel_account_id: string;
  marketplace_id?: string;
  state: string;                  // "draft", "ready", "published", "paused", "ended", "error", "blocked"
  channel_identifiers?: {
    external_listing_id?: string;
    listing_url?: string;
    sku?: string;
  };
  overrides?: ListingOverrides;
  validation_state?: {
    status: string;
    blockers?: Array<{ code: string; message: string }>;
    warnings?: Array<{ code: string; message: string }>;
    validated_at?: string;
  };
  version?: number;
  last_published_at?: string;
  last_synced_at?: string;
  created_at: string;
  updated_at: string;
}

export interface ListingOverrides {
  title?: string;
  description?: string;
  category_mapping?: string;
  attributes?: Record<string, any>;
  images?: string[];
  price?: number;
  quantity?: number;
}

/** Matches: models.CreateListingRequest */
export interface CreateListingRequest {
  product_id: string;             // REQUIRED
  variant_id?: string;
  channel: string;                // REQUIRED
  channel_account_id: string;     // REQUIRED - the credential_id
  overrides?: ListingOverrides;
  auto_publish?: boolean;
}

export interface AdapterInfo {
  id: string;
  name: string;
  display_name: string;
  icon: string;
  color: string;
  requires_oauth: boolean;
  supported_regions?: string[];
  features: string[];
  is_active: boolean;
  credential_fields?: Array<{ key: string; label: string; type: string; required: boolean }>;
}

// ============================================================================
// CREDENTIAL MANAGEMENT
// ============================================================================

export const credentialService = {
  /** POST /marketplace/credentials */
  create: (data: ConnectMarketplaceRequest) =>
    apiClient.post('/marketplace/credentials', data),

  /** GET /marketplace/credentials */
  list: () =>
    apiClient.get('/marketplace/credentials'),

  /** GET /marketplace/credentials/:id */
  get: (id: string) =>
    apiClient.get(`/marketplace/credentials/${id}`),

  /** DELETE /marketplace/credentials/:id */
  delete: (id: string) =>
    apiClient.delete(`/marketplace/credentials/${id}`),

  /** POST /marketplace/credentials/:id/test */
  test: (id: string) =>
    apiClient.post(`/marketplace/credentials/${id}/test`),
};

// ============================================================================
// IMPORT MANAGEMENT
// ============================================================================

export const importService = {
  /** POST /marketplace/import */
  start: (data: StartImportRequest) =>
    apiClient.post('/marketplace/import', data),

  /** GET /marketplace/import/jobs */
  listJobs: (params?: { status?: string; channel?: string; page?: number; page_size?: number }) =>
    apiClient.get('/marketplace/import/jobs', { params }),

  /** GET /marketplace/import/jobs/:id */
  getJob: (jobId: string) =>
    apiClient.get(`/marketplace/import/jobs/${jobId}`),

  /** POST /marketplace/import/jobs/:id/cancel */
  cancel: (jobId: string) =>
    apiClient.post(`/marketplace/import/jobs/${jobId}/cancel`),
};

// ============================================================================
// LISTING MANAGEMENT
// ============================================================================

export const listingService = {
  /** POST /marketplace/listings */
  create: (data: CreateListingRequest) =>
    apiClient.post('/marketplace/listings', data),

  /** GET /marketplace/listings */
  list: (params?: { state?: string; channel?: string; product_id?: string; limit?: number; offset?: number }) =>
    apiClient.get('/marketplace/listings', { params }),

  /** GET /marketplace/listings/:id */
  get: (id: string) =>
    apiClient.get(`/marketplace/listings/${id}`),

  /** PATCH /marketplace/listings/:id */
  update: (id: string, data: Partial<CreateListingRequest>) =>
    apiClient.patch(`/marketplace/listings/${id}`, data),

  /** DELETE /marketplace/listings/:id */
  delete: (id: string) =>
    apiClient.delete(`/marketplace/listings/${id}`),

  /** POST /marketplace/listings/:id/publish */
  publish: (id: string) =>
    apiClient.post(`/marketplace/listings/${id}/publish`),

  /** POST /marketplace/listings/:id/validate */
  validate: (id: string) =>
    apiClient.post(`/marketplace/listings/${id}/validate`),

  /** POST /marketplace/listings/bulk/publish */
  bulkPublish: (listingIds: string[]) =>
    apiClient.post('/marketplace/listings/bulk/publish', { listing_ids: listingIds }),

  /** POST /marketplace/listings/bulk/enrich */
  bulkEnrich: (listingIds: string[]) =>
    apiClient.post('/marketplace/listings/bulk/enrich', { listing_ids: listingIds }),

  /** POST /marketplace/listings/bulk/enrich — all unenriched */
  enrichAll: () =>
    apiClient.post('/marketplace/listings/bulk/enrich', { mode: 'all_unenriched' }),

  /** POST /marketplace/listings/bulk/delete */
  bulkDelete: (listingIds: string[]) =>
    apiClient.post('/marketplace/listings/bulk/delete', { listing_ids: listingIds }),

  /** POST /marketplace/listings/bulk/revise — write explicit field values to overrides */
  bulkRevise: (
    listingIds: string[],
    fields: string[],
    fieldValues: {
      title?: string;
      description?: string;
      price?: number;
      attributes?: Record<string, string>;
      images?: string[];
    },
  ) =>
    apiClient.post('/marketplace/listings/bulk/revise', {
      listing_ids: listingIds,
      fields,
      field_values: fieldValues,
    }),

  /** POST /marketplace/listings/bulk/revise/preview — USP-03
   * Read-only diff of current vs proposed values — no writes performed. */
  bulkRevisePreview: (
    listingIds: string[],
    fields: string[],
    fieldValues: {
      title?: string;
      description?: string;
      price?: number;
      attributes?: Record<string, string>;
      images?: string[];
    },
  ) =>
    apiClient.post<{
      ok: boolean;
      count: number;
      previews: Array<{
        listing_id: string;
        title: string;
        channel: string;
        current: Record<string, any>;
        proposed: Record<string, any>;
      }>;
    }>('/marketplace/listings/bulk/revise/preview', {
      listing_ids: listingIds,
      fields,
      field_values: fieldValues,
    }),

  /** GET /marketplace/listings/:id/analytics?days=N — USP-02 */
  getListingAnalytics: (listingId: string, days = 30) =>
    apiClient.get<{
      ok: boolean;
      supported: boolean;
      channel?: string;
      listing_id?: string;
      period_days?: number;
      metrics?: {
        revenue?: number;
        units_sold?: number;
        sessions?: number;
        page_views?: number;
        impressions?: number;
        clicks?: number;
        conversion_rate?: number;
        currency?: string;
      };
      message?: string;
      error?: string;
    }>(`/marketplace/listings/${listingId}/analytics`, { params: { days } }),

  /** GET /marketplace/listings/unlisted?channel=xxx */
  listUnlisted: (channel: string) =>
    apiClient.get('/marketplace/listings/unlisted', { params: { channel } }),

  /** GET /marketplace/listings/:id — returns listing + product + extended data */
  getDetail: (id: string) =>
    apiClient.get(`/marketplace/listings/${id}`),
};

// ============================================================================
// ADAPTER INFO
// ============================================================================

export const adapterService = {
  /** GET /marketplace/adapters */
  list: () =>
    apiClient.get('/marketplace/adapters'),

  /** GET /marketplace/adapters/:id/fields */
  getFields: (adapterId: string) =>
    apiClient.get(`/marketplace/adapters/${adapterId}/fields`),
};

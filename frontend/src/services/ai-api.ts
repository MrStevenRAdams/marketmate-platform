// ============================================================================
// AI LISTING GENERATION API SERVICE
// ============================================================================
// Location: frontend/src/services/ai-api.ts

import axios from 'axios';
import { getActiveTenantId } from '../contexts/TenantContext';
import { auth } from '../contexts/AuthContext';

const API_BASE_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';

const apiClient = axios.create({
  baseURL: API_BASE_URL,
  headers: { 'Content-Type': 'application/json' },
});

apiClient.interceptors.request.use(async (config) => {
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

// ============================================================================
// TYPES
// ============================================================================

export interface AIStatus {
  available: boolean;
  has_gemini: boolean;
  has_claude: boolean;
  mode: 'hybrid' | 'claude_only' | 'gemini_only' | 'unavailable';
}

export interface AIGeneratedListing {
  channel: string;
  title: string;
  description: string;
  bullet_points?: string[];
  category_id?: string;
  category_name?: string;
  attributes?: Record<string, any>;
  search_terms?: string[];
  price?: number;
  confidence: number;
  warnings?: string[];
  applied?: boolean;
  listing_id?: string;
}

export interface AIGenerationResult {
  product_id: string;
  listings: AIGeneratedListing[];
  error?: string;
  duration_ms: number;
}

export interface AIGenerationJob {
  job_id: string;
  tenant_id: string;
  product_ids: string[];
  channels: string[];
  channel_account_id: string;
  mode: string;
  auto_apply: boolean;
  status: string;
  status_message?: string;
  total_products: number;
  processed_count: number;
  success_count: number;
  failed_count: number;
  results?: AIGenerationJobResult[];
  created_at: string;
  updated_at: string;
  started_at?: string;
  completed_at?: string;
}

export interface AIGenerationJobResult {
  product_id: string;
  product_title?: string;
  status: string;
  error?: string;
  listings?: AIGeneratedListing[];
  duration_ms?: number;
}

export interface GenerateRequest {
  product_id: string;
  channels: string[];
  mode?: 'hybrid' | 'fast' | 'quality';
}

export interface BulkGenerateRequest {
  product_ids: string[];
  channels: string[];
  channel_account_id: string;
  mode?: 'hybrid' | 'fast' | 'quality';
  auto_apply?: boolean;
}

export interface ApplyItem {
  product_id: string;
  channel: string;
  channel_account_id: string;
  title: string;
  description: string;
  bullet_points?: string[];
  attributes?: Record<string, any>;
  price?: number;
  quantity?: number;
}

// Schema-driven generation — used by channel listing create pages with ?ai=pending.
export interface SchemaField {
  name: string;
  display_name: string;
  data_type: string;
  required: boolean;
  allowed_values: string[];
  max_length: number;
}

export interface GenerateWithSchemaRequest {
  product_id: string;
  channel: string;
  category_id: string;
  category_name: string;
  fields: SchemaField[];
}

// ============================================================================
// API METHODS
// ============================================================================

export const aiService = {
  /** GET /ai/status */
  status: () =>
    apiClient.get<{ available: boolean; has_gemini: boolean; has_claude: boolean; mode: string }>('/ai/status'),

  /** POST /ai/generate — single product, synchronous */
  generate: (data: GenerateRequest) =>
    apiClient.post<{ data: AIGenerationResult; message: string }>('/ai/generate', data),

  /** POST /ai/generate/bulk — multiple products, async */
  generateBulk: (data: BulkGenerateRequest) =>
    apiClient.post<{ data: AIGenerationJob; message: string }>('/ai/generate/bulk', data),

  /** GET /ai/generate/jobs */
  listJobs: () =>
    apiClient.get<{ data: AIGenerationJob[] }>('/ai/generate/jobs'),

  /** GET /ai/generate/jobs/:id */
  getJob: (jobId: string) =>
    apiClient.get<{ data: AIGenerationJob }>(`/ai/generate/jobs/${jobId}`),

  /** POST /ai/generate/apply */
  apply: (items: ApplyItem[]) =>
    apiClient.post('/ai/generate/apply', { items }),

  /**
   * POST /ai/generate-with-schema
   * Channel-specific generation driven by a schema field list.
   * Used by EbayListingCreate, AmazonListingCreate, and S4 listing pages
   * when arriving with ?ai=pending.
   */
  generateWithSchema: (data: GenerateWithSchemaRequest) =>
    apiClient.post<{ data: AIGenerationResult }>('/ai/generate-with-schema', data),
};

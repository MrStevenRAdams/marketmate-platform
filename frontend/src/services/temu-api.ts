// ============================================================================
// TEMU API SERVICE
// ============================================================================
// Location: frontend/src/services/temu-api.ts
//
// Calls the Go backend's /api/v1/temu/* endpoints.

import axios from 'axios';
import { getActiveTenantId } from '../contexts/TenantContext';
import { auth } from '../contexts/AuthContext';

const API_BASE_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';

const api = axios.create({
  baseURL: `${API_BASE_URL}/temu`,
  headers: {
    'Content-Type': 'application/json',
  },
});

// Inject X-Tenant-Id on every request from the active tenant
api.interceptors.request.use(
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

// ── Types ──

export interface TemuCategory {
  catId: number;
  catName: string;
  parentId: number;
  leaf: boolean;
  level?: number;
}

export interface TemuShippingTemplate {
  templateId: string;
  templateName: string;
}

export interface TemuBrand {
  brandId: number;
  brandName: string;
  trademarkId: number;
  trademarkBizId: number;
}

export interface TemuPropertyValue {
  vid: number;
  value: string;
  imgUrl?: string;
}

export interface TemuProperty {
  pid: number;
  name: string;
  required: boolean;
  isSale: boolean;
  templatePid?: number;
  refPid?: number;
  values: TemuPropertyValue[];
  inputType?: string; // 'select', 'text', 'number'
}

export interface TemuTemplateData {
  saleProperties: TemuProperty[];
  nonSaleProperties: TemuProperty[];
  raw: Record<string, any>;
}

export interface TemuDraft {
  title: string;
  description: string;
  bulletPoints: string[];
  catId: number;
  catName: string;
  sku: string;
  price: { baseAmount: string; listAmount?: string; currency: string } | null;
  images: string[];
  dimensions: { lengthCm: string; widthCm: string; heightCm: string } | null;
  weight: { weightG: string } | null;
  quantity: number;
  goodsProperties: Record<string, any>[];
  shippingTemplate: string;
  brand: Record<string, any> | null;
  compliance: Record<string, any> | null;
  // Extended data from previous Temu import
  temuExtendedData?: Record<string, any> | null;
  variants?: import('./channel-types').ChannelVariantDraft[];
}

export interface PrepareResponse {
  ok: boolean;
  error?: string;
  product?: Record<string, any>;
  category?: TemuCategory;
  template?: Record<string, any>;
  draft?: TemuDraft;
  brands?: TemuBrand[];
}

export interface SubmitResponse {
  ok: boolean;
  error?: string;
  listingId?: string;
  goodsId?: number;
  skuInfo?: any[];
}

// ── API calls ──

export const temuApi = {
  /** POST /temu/categories/recommend — auto-categorize from product title */
  recommendCategory: (goodsName: string) =>
    api.post<{ ok: boolean; items: TemuCategory[]; error?: string }>(
      '/categories/recommend',
      { goodsName }
    ),

  /** GET /temu/categories?parentId=123 — manual drill-down */
  getCategories: (parentId?: number, credentialId?: string) =>
    api.get<{ ok: boolean; items: TemuCategory[]; error?: string }>(
      '/categories',
      { params: { ...(parentId != null ? { parentId } : {}), ...(credentialId ? { credential_id: credentialId } : {}) } }
    ),

  /** GET /temu/category/path?catId=123 — full ancestor path for a leaf category */
  getCategoryPath: (catId: number) =>
    api.get<{ ok: boolean; path: string[]; catId: number }>(
      '/category/path',
      { params: { catId } }
    ),

  /** GET /temu/template?catId=123 — attribute template for a leaf category */
  getTemplate: (catId: number) =>
    api.get<{ ok: boolean; template: Record<string, any> }>(
      '/template',
      { params: { catId } }
    ),

  /** GET /temu/shipping-templates — freight templates */
  getShippingTemplates: () =>
    api.get<{ ok: boolean; templates: TemuShippingTemplate[]; defaultId: string }>(
      '/shipping-templates'
    ),

  /** GET /temu/brands — all authorized brands for the seller */
  listBrands: (credentialId?: string) =>
    api.get<{ ok: boolean; brands: TemuBrand[]; error?: string }>(
      '/brands',
      { params: credentialId ? { credential_id: credentialId } : {} }
    ),

  /** POST /temu/brand/trademark — brand authorization lookup */
  lookupBrand: (data: { brandId?: number; brandName?: string }) =>
    api.post('/brand/trademark', data),

  /** POST /temu/prepare — prepare listing draft from product data */
  prepare: (data: { product_id: string; catId?: number; credential_id?: string }) =>
    api.post<PrepareResponse>('/prepare', data),

  /** POST /temu/submit — submit reviewed draft to Temu */
  submit: (data: Record<string, any>) =>
    api.post<SubmitResponse>('/submit', data),

  /** GET /temu/compliance?catId=123 — compliance rules for a category */
  getCompliance: (catId: number) =>
    api.get<{ ok: boolean; rules: Record<string, any>; error?: string }>(
      '/compliance',
      { params: { catId } }
    ),
};

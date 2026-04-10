// ============================================================================
// AMAZON API SERVICE
// ============================================================================
// Frontend API client for Amazon SP-API listing endpoints.
// Location: frontend/src/services/amazon-api.ts

import axios from 'axios';
import { getActiveTenantId } from '../contexts/TenantContext';
import { auth } from '../contexts/AuthContext';

const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';

const api = axios.create({
  baseURL: `${API_BASE}/amazon`,
  headers: { 'Content-Type': 'application/json' },
});

// Inject tenant header + Firebase auth token
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

// ============================================================================
// TYPES
// ============================================================================

export interface ProductTypeResult {
  productType: string;
  name?: string;          // Amazon sometimes returns the type as 'name' instead of 'productType'
  displayName: string;
  marketplaceIds?: string[];
}

// Helper to get the actual product type identifier
export function getProductType(pt: ProductTypeResult): string {
  return pt.productType || pt.name || '';
}

// ── Parsed schema types (from backend schema parser) ──

export interface ConditionalRule {
  ifField: string;
  ifValue: string;
  thenRequired?: boolean;
  thenEnumValues?: string[];
  thenMaxLength?: number;
  thenMinLength?: number;
  thenDescription?: string;
}

export interface ParsedAttribute {
  name: string;
  title: string;
  description?: string;
  type: string;
  required: boolean;
  editable: boolean;
  hidden: boolean;
  group: string;
  groupTitle: string;

  enumValues?: string[];
  enumNames?: string[];
  maxLength?: number;
  minLength?: number;
  minimum?: number;
  maximum?: number;
  pattern?: string;
  examples?: string[];
  maxItems?: number;
  minItems?: number;

  conditions?: ConditionalRule[];
}

export interface ConditionalRuleFlat {
  ifField: string;
  ifValue: string;
  thenFields: string[];
}

export interface ParsedSchemaResult {
  attributes: ParsedAttribute[];
  conditionalRules: ConditionalRuleFlat[];
  gpsrAttributes: ParsedAttribute[];
  groupOrder: string[];
  groups: Record<string, { title: string; description: string; propertyNames: string[] }>;
}

// ── Restrictions ──

export interface RestrictionReason {
  reasonCode: string;
  message: string;
  links?: { resource: string; verb: string; title: string; type: string }[];
}

export interface Restriction {
  marketplaceId: string;
  conditionType?: string;
  reasons: RestrictionReason[];
}

// ── Draft ──

export interface AmazonVariantDraft {
  id: string;
  sku: string;
  combination: Record<string, string>;
  price: string;
  stock: string;
  image: string;
  active: boolean;
  ean: string;
  // Per-variant detail overrides
  upc?: string;
  asin?: string;
  weight?: string;
  weightUnit?: string;
  length?: string;
  width?: string;
  height?: string;
  lengthUnit?: string;
  condition?: string;
  title?: string;
  brand?: string;
  manufacturer?: string;
}

export interface AmazonDraft {
  title: string;
  description: string;
  bulletPoints: string[];
  brand: string;
  sku: string;
  price: string;
  currency: string;
  condition: string;
  images: string[];
  productType: string;
  productTypeName: string;
  marketplaceId: string;
  attributes: Record<string, any>;
  fulfillmentChannel: string;
  length: string;
  width: string;
  height: string;
  weight: string;
  lengthUnit: string;
  weightUnit: string;
  ean: string;
  upc: string;
  asin: string;
  isUpdate: boolean;
  existingListing?: any;
  variants?: AmazonVariantDraft[];
  variationTheme?: string;

  // Phase 1 — Pricing & Offer
  listPrice: string;          // RRP / MSRP (strikethrough price)
  salePrice: string;          // Discounted/sale price
  salePriceStart: string;     // ISO date — sale start
  salePriceEnd: string;       // ISO date — sale end
  b2bPrice: string;           // Amazon Business price
  b2bTier1Qty: string;        // B2B quantity discount tier 1 min qty
  b2bTier1Price: string;      // B2B quantity discount tier 1 price
  b2bTier2Qty: string;
  b2bTier2Price: string;
  b2bTier3Qty: string;
  b2bTier3Price: string;

  // Phase 1 — Fulfillment
  quantity: string;           // MFN stock quantity
  handlingTime: string;       // lead_time_to_ship_max_days
  restockDate: string;        // ISO date — when back in stock

  // Package dimensions
  pkgLength: string;
  pkgWidth: string;
  pkgHeight: string;
  pkgWeight: string;
  pkgLengthUnit: string;
  pkgWeightUnit: string;

  // Additional identifiers
  isbn: string;

  // Browse nodes
  browseNode2: string;

  // FLD-05 — "Use main item images only" toggle
  // When true, child listings do not receive per-variant image attributes;
  // all variants inherit the parent product images.
  useMainImagesOnly?: boolean;        // Secondary browse node ID

  // Condition
  conditionNote: string;      // Required for used/refurbished items

  // Phase 2 — Shipping, Tax, Limits
  shippingTemplate: string;   // merchant_shipping_group
  productTaxCode: string;     // product_tax_code (A_GEN_TAX, etc)
  maxOrderQty: string;        // max_order_quantity
  releaseDate: string;        // merchant_release_date (ISO date)
  minPrice: string;           // minimum_seller_allowed_price
  maxPrice: string;           // maximum_seller_allowed_price
}

// ── Responses ──

export interface AmazonPrepareResponse {
  ok: boolean;
  error?: string;
  product?: any;
  draft?: AmazonDraft;
  productTypes?: ProductTypeResult[];
  definition?: any;
  propertyGroups?: any;
  parsedSchema?: ParsedSchemaResult;
  restrictions?: Restriction[];
  debugListing?: any;
  debugExtendedData?: any;
  debugErrors?: string[];
}

export interface AmazonIssue {
  code: string;
  message: string;
  severity: string;
  attributeNames?: string[];
}

export interface AmazonSubmitResponse {
  ok: boolean;
  error?: string;
  status?: string;
  submissionId?: string;
  issues?: AmazonIssue[];
  request?: any;
  response?: any;
  childResults?: any[];
}

export interface AmazonValidateResponse {
  ok: boolean;
  error?: string;
  status?: string;
  issues?: AmazonIssue[];
  errorCount?: number;
  warningCount?: number;
  request?: any;
  response?: any;
}

export interface CatalogItem {
  asin: string;
  title: string;
  brand: string;
  image: string;
}

// ============================================================================
// API METHODS
// ============================================================================

export const amazonApi = {
  searchProductTypes: (params: { keywords?: string; itemName?: string; credential_id?: string }) =>
    api.get('/product-types/search', { params }),

  getProductTypeDefinition: (params: { product_type: string; credential_id?: string }) =>
    api.get('/product-types/definition', { params }),

  searchCatalog: (params: { keywords?: string; credential_id?: string }) =>
    api.get('/catalog/search', { params }),

  prepare: (data: { product_id: string; credential_id?: string; product_type?: string }) =>
    api.post<AmazonPrepareResponse>('/prepare', data),

  submit: (data: any) =>
    api.post<AmazonSubmitResponse>('/submit', data),

  validate: (data: any) =>
    api.post<AmazonValidateResponse>('/validate', data),

  checkRestrictions: (params: { asin: string; condition_type?: string; credential_id?: string }) =>
    api.get('/restrictions', { params }),

  getListing: (params: { sku: string; credential_id?: string }) =>
    api.get('/listing', { params }),
};

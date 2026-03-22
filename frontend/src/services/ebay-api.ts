// ============================================================================
// EBAY API SERVICE
// ============================================================================
// Frontend API client for eBay listing endpoints.
// Location: frontend/src/services/ebay-api.ts
//
// Calls the Go backend's /api/v1/ebay/* endpoints.
// Pattern follows amazon-api.ts and temu-api.ts exactly.

import axios from 'axios';

// ChannelVariantDraft is defined in channel-types.ts and re-exported here
// for backwards compatibility with any existing imports from ebay-api.
export type { ChannelVariantDraft } from './channel-types';

import { getActiveTenantId } from '../contexts/TenantContext';
import { auth } from '../contexts/AuthContext';

const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';

const api = axios.create({
  baseURL: `${API_BASE}/ebay`,
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

// ── Category / Taxonomy ──

export interface CategorySuggestion {
  category: {
    categoryId: string;
    categoryName: string;
  };
  categoryTreeNodeLevel: number;
  relevancy: string;
}

export interface ItemAspect {
  localizedAspectName: string;
  aspectConstraint: {
    aspectRequired: boolean;
    aspectMode: string; // "SELECTION_ONLY" | "FREE_TEXT"
    aspectUsage: string; // "RECOMMENDED" | "OPTIONAL"
    expectedRequiredByDate?: string;
    itemToAspectCardinality: string; // "SINGLE" | "MULTI"
  };
  aspectValues?: { localizedValue: string }[];
}

// ── Business Policies ──

export interface FulfillmentPolicy {
  fulfillmentPolicyId: string;
  name: string;
  marketplaceId: string;
}

export interface PaymentPolicy {
  paymentPolicyId: string;
  name: string;
  marketplaceId: string;
}

export interface ReturnPolicy {
  returnPolicyId: string;
  name: string;
  marketplaceId: string;
}

export interface InventoryLocation {
  merchantLocationKey: string;
  // FLD-12: Click & Collect / in-store pickup
  clickAndCollectEnabled: boolean;
  pickupLeadTimeDays: number;    // 0-30 days (0 = same day)
  pickupDropOffEnabled: boolean; // allow customer drop-off (IN_STORE_PICKUP type)
  name: string;
  merchantLocationStatus: string;
  location?: {
    address?: {
      addressLine1?: string;
      city?: string;
      stateOrProvince?: string;
      postalCode?: string;
      country?: string;
    };
  };
  locationTypes?: string[];
}

// ── Draft ──

export interface EbayDraft {
  // Core
  title: string;
  subtitle: string;
  description: string;
  condition: string;
  conditionDescription: string;
  brand: string;
  mpn: string;

  // Category
  categoryId: string;
  categoryName: string;
  secondaryCategoryId: string;
  secondaryCategoryName: string;

  // Item specifics (aspects)
  aspects: Record<string, string[]>;

  // Pricing & Format
  listingFormat: string; // "FIXED_PRICE" | "AUCTION"
  price: string;
  currency: string;
  reservePrice: string;
  bestOfferEnabled: boolean;
  bestOfferAutoAcceptPrice: string;
  bestOfferAutoDeclinePrice: string;
  vatPercentage: string;

  // Inventory
  sku: string;
  quantity: string;
  lotSize: string;

  // Images
  images: string[];
  imageAlts: string[]; // FLD-16: per-image alt text / captions

  // Policies (IDs from business policies)
  fulfillmentPolicyId: string;
  paymentPolicyId: string;
  returnPolicyId: string;
  merchantLocationKey: string;
  // FLD-12: Click & Collect / in-store pickup
  clickAndCollectEnabled: boolean;
  pickupLeadTimeDays: number;    // 0-30 days (0 = same day)
  pickupDropOffEnabled: boolean; // allow customer drop-off (IN_STORE_PICKUP type)

  // Package dimensions & weight
  packageLength: string;
  packageWidth: string;
  packageHeight: string;
  packageWeightValue: string;
  dimensionUnit: string; // "CENTIMETER" | "INCH"
  weightUnit: string;    // "KILOGRAM" | "POUND" | "GRAM" | "OUNCE"
  packageType: string;   // "LETTER", "LARGE_ENVELOPE", "PACKAGE", etc.

  // Identifiers
  ean: string;
  upc: string;
  isbn: string;

  // Listing enhancements
  listingDuration: string; // "GTC", "DAYS_3", "DAYS_5", "DAYS_7", "DAYS_10", "DAYS_30"
  privateListing: boolean;
  scheduledStartTime: string;
  includeCatalogProductDetails: boolean;

  // eBay Catalog (FLD-09)
  epid: string;

  // GPSR — EU General Product Safety Regulation (FLD-07)
  gpsrManufacturerName: string;
  gpsrManufacturerAddress: string;
  gpsrResponsiblePersonName: string;
  gpsrResponsiblePersonContact: string;
  gpsrSafetyAttestation: boolean;
  gpsrDocumentUrls: string;

  // Volume / Quantity pricing tiers (FLD-10)
  pricingTiers: { minQty: number; pricePerUnit: string }[];

  // Promoted Listings (PRC-04)
  promotedListingRate: string; // Optional ad rate % (1–20) for COST_PER_SALE promoted listings

  // FLD-01 — Payment methods annotation (stored in overrides; not sent to eBay API)
  // e.g. ["PayPal", "Credit/Debit Card", "Klarna"]
  paymentMethods: string[];

  // FLD-02 — Bullet points (up to 5)
  // Prepended to the listing description as <ul><li>...</li></ul> on submit
  bulletPoints: string[];
  shortDescription: string; // Mobile-first summary (stored in overrides only)

  // Marketplace
  marketplaceId: string; // "EBAY_GB", "EBAY_US", etc.

  // Update context
  isUpdate: boolean;
  existingOfferId: string;
  existingListingId: string;

  // VAR-01 — Variation listings (Session H)
  variants?: ChannelVariantDraft[];
}

// ── Responses ──

export interface EbayPrepareResponse {
  ok: boolean;
  error?: string;
  product?: Record<string, any>;
  draft?: EbayDraft;
  categorySuggestions?: CategorySuggestion[];
  itemAspects?: ItemAspect[];
  fulfillmentPolicies?: FulfillmentPolicy[];
  paymentPolicies?: PaymentPolicy[];
  returnPolicies?: ReturnPolicy[];
  locations?: InventoryLocation[];
  debugErrors?: string[];
}

export interface EbaySubmitResponse {
  ok: boolean;
  error?: string;
  offerId?: string;
  listingId?: string;
  warnings?: string[];
  inventoryItemResult?: string;
  offerResult?: string;
  publishResult?: string;
}

export interface EbayPoliciesResponse {
  ok: boolean;
  errors?: string[];
  fulfillment_policies?: FulfillmentPolicy[];
  payment_policies?: PaymentPolicy[];
  return_policies?: ReturnPolicy[];
}

export interface EbayLocationsResponse {
  ok: boolean;
  total: number;
  locations: InventoryLocation[];
}

export interface EbayCategorySuggestionsResponse {
  ok: boolean;
  suggestions: CategorySuggestion[];
}

export interface EbayItemAspectsResponse {
  ok: boolean;
  aspects: ItemAspect[];
  categoryId: string;
  categoryTreeId: string;
}

export interface EbayCatalogSearchResponse {
  ok: boolean;
  products: CatalogProduct[];
}

export interface CatalogProduct {
  epid: string;
  title: string;
  imageUrl?: string;
  gtins?: string[];
  brand?: string;
  categoryName?: string;
}

// ============================================================================
// API METHODS
// ============================================================================

export const ebayApi = {
  /** POST /ebay/prepare — prepare listing draft from PIM product */
  prepare: (data: { product_id: string; credential_id?: string; marketplace_id?: string }) =>
    api.post<EbayPrepareResponse>('/prepare', data),

  /** POST /ebay/submit — submit reviewed draft to eBay */
  submit: (data: Record<string, any>) =>
    api.post<EbaySubmitResponse>('/submit', data),

  /** GET /ebay/categories/suggest?q=xxx&marketplace=EBAY_GB */
  suggestCategories: (q: string, marketplace?: string) =>
    api.get<EbayCategorySuggestionsResponse>('/categories/suggest', {
      params: { q, marketplace: marketplace || 'EBAY_GB' },
    }),

  /** GET /ebay/categories/aspects?category_id=xxx&marketplace=EBAY_GB */
  getItemAspects: (categoryId: string, marketplace?: string) =>
    api.get<EbayItemAspectsResponse>('/categories/aspects', {
      params: { category_id: categoryId, marketplace: marketplace || 'EBAY_GB' },
    }),

  /** GET /ebay/catalog/search?q=xxx&gtin=xxx&marketplace=EBAY_GB (FLD-09) */
  catalogSearch: (params: { q?: string; gtin?: string; marketplace?: string }) =>
    api.get<EbayCatalogSearchResponse>('/catalog/search', { params }),

  /** GET /ebay/policies?marketplace=EBAY_GB */
  getPolicies: (marketplace?: string) =>
    api.get<EbayPoliciesResponse>('/policies', {
      params: { marketplace: marketplace || 'EBAY_GB' },
    }),

  /** GET /ebay/locations */
  getLocations: () =>
    api.get<EbayLocationsResponse>('/locations'),

  /** GET /ebay/inventory/:sku */
  getInventoryItem: (sku: string) =>
    api.get(`/inventory/${encodeURIComponent(sku)}`),

  /** GET /ebay/offers/:sku */
  getOffers: (sku: string) =>
    api.get(`/offers/${encodeURIComponent(sku)}`),

  /** POST /ebay/test — test connection */
  testConnection: () =>
    api.post('/test'),
};

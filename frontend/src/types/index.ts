// Product Types
export interface Product {
  product_id: string;
  tenant_id: string;
  status: 'draft' | 'active' | 'archived';
  title: string;
  subtitle?: string;
  description?: string;
  brand?: string;
  product_type: 'simple' | 'parent' | 'variant' | 'bundle';
  parent_id?: string;
  identifiers?: ProductIdentifiers;
  category_ids?: string[];
  tags?: string[];
  attribute_set_id?: string;
  attributes?: Record<string, any>;
  assets?: ProductAsset[];
  bundle_components?: BundleComponent[];
  compliance?: ComplianceStatus;
  readiness_flags?: ReadinessFlags;
  /** When true, every unit must be tracked with a unique serial number on receipt */
  use_serial_numbers?: boolean;
  /** When true, the product is discontinued — new POs blocked, listings warned */
  end_of_life?: boolean;
  created_at: string;
  updated_at: string;
  created_by?: string;
  updated_by?: string;
}

export interface ProductIdentifiers {
  ean?: string;
  upc?: string;
  asin?: string;
  isbn?: string;
  mpn?: string;
  gtin?: string;
}

export interface ProductAsset {
  asset_id: string;
  url: string;
  path: string;
  role: 'primary_image' | 'gallery' | 'manual' | 'compliance';
  sort_order: number;
}

export interface BundleComponent {
  component_id: string;
  product_id: string;
  quantity: number;
  is_required: boolean;
  sort_order: number;
  title?: string;
}

export interface ComplianceStatus {
  status: 'compliant' | 'non_compliant' | 'pending' | 'unknown';
  blocking_issues?: string[];
  expiring_within_days?: number;
  missing_required_types?: string[];
  last_reviewed_at?: string;
}

export interface ReadinessFlags {
  ready_for_listing: boolean;
  ready_for_shipping: boolean;
  has_compliance_issues: boolean;
}

// Variant Types
export interface Variant {
  variant_id: string;
  tenant_id: string;
  product_id: string;
  sku: string;
  barcode?: string;
  identifiers?: ProductIdentifiers;
  title?: string;
  attributes?: Record<string, any>;
  status: 'active' | 'inactive' | 'archived';
  pricing?: VariantPricing;
  dimensions?: Dimensions;
  weight?: Weight;
  created_at: string;
  updated_at: string;
  created_by?: string;
  updated_by?: string;
}

export interface VariantPricing {
  list_price?: Money;
  rrp?: Money;
  cost?: Money;
  sale?: SalePrice;
}

export interface Money {
  amount: number;
  currency: string;
  fx_rate_used?: number;
  fx_rate_timestamp?: string;
}

export interface SalePrice {
  sale_price: Money;
  from?: string;
  to?: string;
}

export interface Dimensions {
  length?: number;
  width?: number;
  height?: number;
  unit: 'mm' | 'cm' | 'm' | 'in';
}

export interface Weight {
  value?: number;
  unit: 'g' | 'kg' | 'lb' | 'oz';
}

// Category Types
export interface CategoryImage {
  url: string;
  path: string;
  sort_order: number;
}

export interface Category {
  category_id: string;
  tenant_id: string;
  name: string;
  slug: string;
  parent_id?: string | null;
  description?: string;
  attribute_set_id?: string;
  images?: CategoryImage[];
  sort_order: number;
  level: number;
  path: string;
  active: boolean;
  created_at: string;
  updated_at: string;
  children?: Category[];
}

// Job Types
export interface Job {
  job_id: string;
  tenant_id: string;
  type: 'import' | 'export' | 'ai_enrichment' | 'bulk_publish' | 'generate_variants';
  status: 'queued' | 'running' | 'succeeded' | 'partial' | 'failed' | 'cancelled';
  progress?: number;
  summary?: JobSummary;
  errors?: JobError[];
  result_url?: string;
  created_at: string;
  started_at?: string;
  finished_at?: string;
  expires_at: string;
}

export interface JobSummary {
  total: number;
  succeeded: number;
  failed: number;
  skipped: number;
}

export interface JobError {
  item_id?: string;
  error_code: string;
  error_message: string;
}

// API Response Types
export interface PaginationMeta {
  total: number;
  page: number;
  page_size: number;
  total_pages: number;
  next_cursor?: string;
}

export interface ListProductsResponse {
  data: Product[];
  pagination: PaginationMeta;
}

export interface ListVariantsResponse {
  data: Variant[];
  pagination: PaginationMeta;
}

export interface ListCategoriesResponse {
  data: Category[];
}

export interface ListJobsResponse {
  data: Job[];
  pagination: PaginationMeta;
}

// Request Types
export interface CreateProductRequest {
  title: string;
  subtitle?: string;
  description?: string;
  brand?: string;
  product_type: 'simple' | 'parent' | 'variant' | 'bundle';
  parent_id?: string;
  identifiers?: ProductIdentifiers;
  category_ids?: string[];
  tags?: string[];
  attribute_set_id?: string;
  attributes?: Record<string, any>;
  bundle_components?: BundleComponent[];
  assets?: ProductAsset[];
}

export interface UpdateProductRequest {
  title?: string;
  subtitle?: string;
  description?: string;
  brand?: string;
  status?: 'draft' | 'active' | 'archived';
  category_ids?: string[];
  tags?: string[];
  attributes?: Record<string, any>;
  assets?: ProductAsset[];
  use_serial_numbers?: boolean;
  end_of_life?: boolean;
}

export interface CreateVariantRequest {
  sku: string;
  barcode?: string;
  title?: string;
  identifiers?: ProductIdentifiers;
  attributes?: Record<string, any>;
  pricing?: VariantPricing;
  dimensions?: Dimensions;
  weight?: Weight;
}

export interface GenerateVariantsRequest {
  attributes: Record<string, string[]>;
  sku_pattern?: string;
}

export interface CreateCategoryRequest {
  name: string;
  slug?: string;
  parent_id?: string | null;
  description?: string;
  images?: CategoryImage[];
  sort_order?: number;
}

export interface UpdateCategoryRequest {
  name?: string;
  slug?: string;
  parent_id?: string | null;
  description?: string;
  images?: CategoryImage[];
  sort_order?: number;
  active?: boolean;
}

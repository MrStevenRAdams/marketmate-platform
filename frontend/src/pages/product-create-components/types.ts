// ============================================================================
// VARIANT & BUNDLE TYPES
// ============================================================================

export interface VariantField<T> {
  value: T;
  inheritFromParent: boolean;
}

export interface ProductOption {
  name: string;
  values: string[];
}

export interface Variant {
  id: string;
  optionCombination: Record<string, string>; // { Color: 'Red', Size: 'Large' }
  sku: VariantField<string>;
  title: VariantField<string>;
  weight: VariantField<number>;
  dimensions: VariantField<{ width: number; height: number; length: number }>;
  price: VariantField<number>;
  stock: VariantField<number>;
  status: 'active' | 'inactive';
  expanded?: boolean; // For UI expansion state
}

export interface BundleItem {
  product_id: string;
  title: string;
  sku: string;
  brand?: string;
  quantity: number;
  image?: string;
}

export interface Product {
  product_id: string;
  title: string;
  subtitle?: string;
  sku?: string;
  brand?: string;
  status: string;
  assets?: Array<{ url: string }>;
}

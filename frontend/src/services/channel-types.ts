// ============================================================================
// SHARED CHANNEL TYPES
// ============================================================================
// Canonical types shared across all channel API services.

export interface ChannelVariantDraft {
  id: string;
  sku: string;
  combination: Record<string, string>;   // { Color: 'Red', Size: 'M' }
  price: string;
  stock: string;
  image: string;       // primary image URL override
  images?: string[];   // all images for this variant
  ean: string;
  upc?: string;
  title?: string;        // variant-specific title override
  description?: string;  // variant-specific description override
  brand?: string;
  condition?: string;
  weight?: string;
  weightUnit?: string;
  length?: string;
  width?: string;
  height?: string;
  lengthUnit?: string;
  active: boolean;
}

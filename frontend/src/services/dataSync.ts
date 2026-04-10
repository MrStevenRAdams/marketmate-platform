import { ProductFormData } from '../types';

// ============================================================================
// AI-POWERED DATA TRANSFORMATION UTILITIES
// ============================================================================

/**
 * Amazon Data Sync
 * Optimizes product data for Amazon marketplace
 */
export const amazonDataSync = (coreData: ProductFormData) => ({
  // Amazon title (max 200 chars, but 60-80 recommended for mobile)
  title: truncateTitle(coreData.title, 80),
  
  // Bullet points (max 5, each max 255 chars)
  bulletPoints: coreData.keyFeatures.slice(0, 5).map(f => truncate(f, 255)),
  
  // Description (HTML allowed)
  description: coreData.description,
  
  // Search terms (comma-separated keywords)
  searchTerms: extractKeywords(coreData.title + ' ' + coreData.tags),
  
  // Product ID type
  productIdType: 'UPC',
  productId: '',
  
  // Category
  browseNode: '',
  
  // Pricing
  price: 0,
  salePrice: 0,
  
  // Fulfillment
  fulfillmentChannel: 'FBA',
  
  // Condition
  condition: 'New',
  conditionNote: '',
  
  // Images (max 9)
  images: coreData.images.slice(0, 9),
});

/**
 * eBay Data Sync
 * Optimizes product data for eBay marketplace
 */
export const ebayDataSync = (coreData: ProductFormData) => ({
  // eBay title (max 80 chars)
  title: truncateTitle(coreData.title, 80),
  
  // Subtitle (max 55 chars, optional)
  subtitle: '',
  
  // Description (HTML required)
  description: convertToHTML(coreData.description),
  
  // Category
  categoryId: '',
  
  // Condition
  condition: 'New',
  conditionDescription: '',
  
  // Pricing
  format: 'FixedPrice', // or 'Auction'
  price: 0,
  quantity: 0,
  
  // Shipping
  shippingService: 'StandardShipping',
  shippingCost: 0,
  
  // Item specifics (attributes)
  itemSpecifics: Object.entries(coreData.attributes || {}).map(([name, value]) => ({
    name,
    value
  })),
  
  // Images (max 12)
  images: coreData.images.slice(0, 12),
});

/**
 * Shopify Data Sync
 * Optimizes product data for Shopify
 */
export const shopifyDataSync = (coreData: ProductFormData) => ({
  // Title
  title: coreData.title,
  
  // Description (HTML allowed)
  bodyHtml: convertToHTML(coreData.description),
  
  // Vendor (brand)
  vendor: coreData.brand,
  
  // Product type
  productType: '',
  
  // Tags (comma-separated)
  tags: coreData.tags,
  
  // Collections
  collections: [],
  
  // Pricing
  price: 0,
  compareAtPrice: 0,
  
  // SKU & inventory
  sku: '',
  inventory: 0,
  
  // Images
  images: coreData.images.map(url => ({ src: url })),
  
  // SEO
  seoTitle: coreData.title,
  seoDescription: extractFirstSentence(coreData.description),
});

/**
 * Temu Data Sync
 * Optimizes product data for Temu marketplace
 */
export const temuDataSync = (coreData: ProductFormData) => ({
  title: truncateTitle(coreData.title, 100),
  description: coreData.description,
  category: '',
  attributes: coreData.attributes || {},
  price: 0,
  images: coreData.images.slice(0, 10),
  brand: coreData.brand,
});

/**
 * Tesco Data Sync
 * Optimizes product data for Tesco marketplace
 */
export const tescoDataSync = (coreData: ProductFormData) => ({
  title: truncateTitle(coreData.title, 120),
  description: coreData.description,
  category: '',
  gtin: '',
  brand: coreData.brand,
  price: 0,
  vat: 20, // UK standard VAT
  images: coreData.images.slice(0, 8),
  nutritionalInfo: {}, // For food products
});

// ============================================================================
// UTILITY FUNCTIONS
// ============================================================================

const truncateTitle = (title: string, maxLength: number): string => {
  if (title.length <= maxLength) return title;
  return title.substring(0, maxLength - 3) + '...';
};

const truncate = (text: string, maxLength: number): string => {
  if (text.length <= maxLength) return text;
  return text.substring(0, maxLength - 3) + '...';
};

const extractKeywords = (text: string): string => {
  // Simple keyword extraction (in production, use NLP/AI)
  const words = text.toLowerCase()
    .replace(/[^a-z0-9\s]/g, '')
    .split(/\s+/)
    .filter(w => w.length > 3);
  
  // Remove duplicates and return top 20
  return [...new Set(words)].slice(0, 20).join(', ');
};

const convertToHTML = (text: string): string => {
  // Convert plain text/rich text to HTML
  if (text.includes('<')) return text; // Already HTML
  
  return text
    .split('\n')
    .map(line => `<p>${line}</p>`)
    .join('');
};

const extractFirstSentence = (text: string): string => {
  // Extract first sentence for meta description
  const stripped = text.replace(/<[^>]*>/g, ''); // Remove HTML
  const match = stripped.match(/^[^.!?]+[.!?]/);
  return match ? match[0] : stripped.substring(0, 160);
};

/**
 * Validate Amazon Data
 */
export const validateAmazonData = (data: any): { isValid: boolean; errors: Array<{ field: string; message: string }> } => {
  const errors: Array<{ field: string; message: string }> = [];
  
  if (!data.title || data.title.length > 200) {
    errors.push({ field: 'title', message: 'Title must be between 1-200 characters' });
  }
  
  if (!data.productId) {
    errors.push({ field: 'productId', message: 'Product ID (UPC/EAN/ASIN) is required' });
  }
  
  if (!data.price || data.price <= 0) {
    errors.push({ field: 'price', message: 'Price must be greater than 0' });
  }
  
  if (data.bulletPoints && data.bulletPoints.length > 5) {
    errors.push({ field: 'bulletPoints', message: 'Maximum 5 bullet points allowed' });
  }
  
  return {
    isValid: errors.length === 0,
    errors
  };
};

/**
 * Validate eBay Data
 */
export const validateEbayData = (data: any): { isValid: boolean; errors: Array<{ field: string; message: string }> } => {
  const errors: Array<{ field: string; message: string }> = [];
  
  if (!data.title || data.title.length > 80) {
    errors.push({ field: 'title', message: 'Title must be between 1-80 characters' });
  }
  
  if (!data.categoryId) {
    errors.push({ field: 'categoryId', message: 'eBay category is required' });
  }
  
  if (!data.price || data.price <= 0) {
    errors.push({ field: 'price', message: 'Price must be greater than 0' });
  }
  
  if (!data.quantity || data.quantity < 1) {
    errors.push({ field: 'quantity', message: 'Quantity must be at least 1' });
  }
  
  return {
    isValid: errors.length === 0,
    errors
  };
};

/**
 * Validate Shopify Data
 */
export const validateShopifyData = (data: any): { isValid: boolean; errors: Array<{ field: string; message: string }> } => {
  const errors: Array<{ field: string; message: string }> = [];
  
  if (!data.title) {
    errors.push({ field: 'title', message: 'Title is required' });
  }
  
  if (!data.price || data.price <= 0) {
    errors.push({ field: 'price', message: 'Price must be greater than 0' });
  }
  
  return {
    isValid: errors.length === 0,
    errors
  };
};

export const shoplineDataSync = (coreData: ProductFormData) => ({
  // Title
  title: coreData.title,

  // Description (HTML allowed)
  description: convertToHTML(coreData.description),

  // Vendor (brand)
  vendor: coreData.brand,

  // Product type
  productType: '',

  // Tags (comma-separated)
  tags: coreData.tags,

  // Pricing
  price: 0,
  compareAtPrice: 0,

  // SKU & inventory
  sku: '',
  quantity: 0,

  // Images
  images: coreData.images,

  // SEO
  seoTitle: coreData.title,
  seoDescription: extractFirstSentence(coreData.description),
});

export const validateShoplineData = (data: any): { isValid: boolean; errors: Array<{ field: string; message: string }> } => {
  const errors: Array<{ field: string; message: string }> = [];

  if (!data.title) {
    errors.push({ field: 'title', message: 'Title is required' });
  }

  if (!data.price || data.price <= 0) {
    errors.push({ field: 'price', message: 'Price must be greater than 0' });
  }

  if (!data.sku) {
    errors.push({ field: 'sku', message: 'SKU is required' });
  }

  return {
    isValid: errors.length === 0,
    errors,
  };
};

/**
 * Generic validation for Temu/Tesco
 */
export const validateGenericData = (data: any): { isValid: boolean; errors: Array<{ field: string; message: string }> } => {
  const errors: Array<{ field: string; message: string }> = [];
  
  if (!data.title) {
    errors.push({ field: 'title', message: 'Title is required' });
  }
  
  if (!data.price || data.price <= 0) {
    errors.push({ field: 'price', message: 'Price must be greater than 0' });
  }
  
  return {
    isValid: errors.length === 0,
    errors
  };
};

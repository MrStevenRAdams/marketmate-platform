// ============================================================================
// API SERVICE — Module A (PIM)
// ============================================================================
// Updated to use dynamic tenant ID from TenantContext.
// The tenant header is injected via a request interceptor so
// every call automatically uses the currently selected account.
// ============================================================================

import axios from 'axios';
import { getActiveTenantId } from '../contexts/TenantContext';
import { auth } from '../contexts/AuthContext';

const API_BASE_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';

// Create axios instance — tenant header added dynamically via interceptor
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
    try {
      // Wait for Firebase auth to resolve — prevents 401s on first load
      // when auth.currentUser is momentarily null
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
    } catch {
      // Non-fatal
    }
    return config;
  },
  (error) => Promise.reject(error),
);

// Response interceptor for error handling
apiClient.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response?.status === 401) {
      console.error('Unauthorized - redirect to login');
    }
    return Promise.reject(error);
  },
);

// Export the main API object
export const api = {
  // Generic methods
  get: (url: string, config?: any) => apiClient.get(url, config),
  post: (url: string, data?: any, config?: any) => apiClient.post(url, data, config),
  patch: (url: string, data?: any, config?: any) => apiClient.patch(url, data, config),
  put: (url: string, data?: any, config?: any) => apiClient.put(url, data, config),
  delete: (url: string, config?: any) => apiClient.delete(url, config),

  // File upload method
  upload: async (formData: FormData) => {
    return apiClient.post('/upload', formData, {
      headers: { 'Content-Type': 'multipart/form-data' },
    });
  },
};

// Product Service
export const productService = {
  // List products with optional filters and pagination
  list: (params?: any) => apiClient.get('/products', { params }),
  
  // Get a single product by ID
  get: (id: string) => apiClient.get(`/products/${id}`),
  
  // Create a new product
  create: (data: any) => apiClient.post('/products', data),
  
  // Update a product
  update: (id: string, data: any) => apiClient.patch(`/products/${id}`, data),
  
  // Delete a product
  delete: (id: string) => apiClient.delete(`/products/${id}`),
  
  // Bulk operations
  bulkCreate: (products: any[]) => apiClient.post('/products/bulk', { products }),
  bulkUpdate: (updates: any[]) => apiClient.patch('/products/bulk', { updates }),

  // AI-powered product lookup by identifier (EAN/ASIN/UPC/ISBN)
  aiLookup: (data: { sku: string; identifierType: string; identifierValue: string; credentialId?: string }) =>
    apiClient.post('/products/ai-lookup', data),
};

// Variant Service
export const variantService = {
  // List all variants or variants for a specific product
  list: (productId?: string, params?: any) =>
    productId
      ? apiClient.get(`/products/${productId}/variants`, { params })
      : apiClient.get('/variants', { params }),
  
  // Get a single variant by ID
  get: (id: string) => apiClient.get(`/variants/${id}`),
  
  // Create a new variant for a product
  create: (productId: string, data: any) =>
    apiClient.post(`/products/${productId}/variants`, data),
  
  // Update a variant
  update: (id: string, data: any) => apiClient.patch(`/variants/${id}`, data),
  
  // Delete a variant
  delete: (id: string) => apiClient.delete(`/variants/${id}`),

  // Generate variants from attribute combinations
  generate: (productId: string, data: any) =>
    apiClient.post(`/products/${productId}/generate-variants`, data),
};

// Category Service
export const categoryService = {
  // List categories (flat)
  list: () => apiClient.get('/categories'),
  
  // Get category tree (hierarchical)
  tree: () => apiClient.get('/categories/tree'),
  
  // Get a single category by ID
  get: (id: string) => apiClient.get(`/categories/${id}`),
  
  // Create a new category
  create: (data: any) => apiClient.post('/categories', data),
  
  // Update a category
  update: (id: string, data: any) => apiClient.patch(`/categories/${id}`, data),
  
  // Delete a category
  delete: (id: string) => apiClient.delete(`/categories/${id}`),
};

// File Service
export const fileService = {
  // Upload a file
  upload: (formData: FormData) =>
    apiClient.post('/upload', formData, {
      headers: { 'Content-Type': 'multipart/form-data' },
    }),
  
  // Delete a single file
  delete: (url: string) => apiClient.delete('/files', { data: { url } }),
  
  // Delete multiple files
  deleteBatch: (urls: string[]) => apiClient.post('/files/delete-batch', { urls }),
  
  // List files in a folder
  list: (folder: string) => apiClient.get('/files/list', { params: { folder } }),
};

// Job Service
export const jobService = {
  // Get a job by ID
  get: (id: string) => apiClient.get(`/jobs/${id}`),
  
  // List all jobs
  list: (params?: any) => apiClient.get('/jobs', { params }),
};

// Attribute Service
export const attributeService = {
  // List all attributes
  list: () => apiClient.get('/attributes'),
  
  // Get a single attribute by ID
  get: (id: string) => apiClient.get(`/attributes/${id}`),
  
  // Create a new attribute
  create: (data: any) => apiClient.post('/attributes', data),
  
  // Update an attribute
  update: (id: string, data: any) => apiClient.patch(`/attributes/${id}`, data),
  
  // Delete an attribute
  delete: (id: string) => apiClient.delete(`/attributes/${id}`),
};

// Attribute Set Service
export const attributeSetService = {
  // List all attribute sets
  list: () => apiClient.get('/attribute-sets'),
  
  // Get a single attribute set by ID
  get: (id: string) => apiClient.get(`/attribute-sets/${id}`),
  
  // Create a new attribute set
  create: (data: any) => apiClient.post('/attribute-sets', data),
  
  // Update an attribute set
  update: (id: string, data: any) => apiClient.patch(`/attribute-sets/${id}`, data),
  
  // Delete an attribute set
  delete: (id: string) => apiClient.delete(`/attribute-sets/${id}`),
};

// Default export for backward compatibility
export default api;

// Search Service (Typesense)
export const searchService = {
  products: (params?: { q?: string; status?: string; brand?: string; product_type?: string; page?: number; per_page?: number }) =>
    apiClient.get('/search/products', { params }),
  
  listings: (params?: { q?: string; channel?: string; state?: string; page?: number; per_page?: number }) =>
    apiClient.get('/search/listings', { params }),
  
  sync: (collection?: string) =>
    apiClient.post('/search/sync', { collection }),
  
  health: () => apiClient.get('/search/health'),
};

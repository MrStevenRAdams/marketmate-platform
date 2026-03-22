import React, { useState, useEffect } from 'react';
import { productService, searchService } from '../../services/api';
import { Product, BundleItem } from './types';

interface Props {
  show: boolean;
  onClose: () => void;
  onAddItems: (items: BundleItem[]) => void;
  excludeIds: string[];
  title?: string;
  confirmLabel?: string;
  singleSelect?: boolean;
}

export default function ProductSearchModal({ show, onClose, onAddItems, excludeIds, title, confirmLabel, singleSelect }: Props) {
  const [search, setSearch] = useState('');
  const [products, setProducts] = useState<Product[]>([]);
  const [loading, setLoading] = useState(false);
  const [selectedProducts, setSelectedProducts] = useState<Map<string, BundleItem>>(new Map());

  useEffect(() => {
    if (show) {
      loadProducts();
    } else {
      // Reset when modal closes
      setSearch('');
      setSelectedProducts(new Map());
    }
  }, [show]);

  useEffect(() => {
    if (show) {
      const timer = setTimeout(() => {
        loadProducts();
      }, 300);
      return () => clearTimeout(timer);
    }
  }, [search]);

  const loadProducts = async () => {
    try {
      setLoading(true);
      let productArray: Product[] = [];

      try {
        // Primary: use Typesense full-text search (handles pagination + search server-side)
        const searchRes = await searchService.products({
          q: search.trim() || '*',
          per_page: 100,
        });
        const searchData = searchRes.data;
        const hits = Array.isArray(searchData?.data) ? searchData.data : [];
        // Typesense returns flat docs with product_id, title, sku, brand, image_url, status, etc.
        productArray = hits.map((h: any) => ({
          product_id: h.product_id || h.id,
          title: h.title || '',
          sku: h.sku || '',
          brand: h.brand || '',
          status: h.status || '',
          product_type: h.product_type || '',
          assets: h.image_url ? [{ url: h.image_url, role: 'primary_image' }] : [],
          attributes: {},
          ...h,
        }));
      } catch {
        // Fallback: Firestore endpoint (no server-side search, limited to page_size)
        const response = await productService.list({ page_size: 100 });
        const responseData = response.data;
        if (responseData?.data && Array.isArray(responseData.data)) {
          productArray = responseData.data;
        } else if (Array.isArray(responseData)) {
          productArray = responseData;
        }

        // Client-side search fallback
        if (search.trim()) {
          const q = search.toLowerCase();
          productArray = productArray.filter((p: any) => {
            const sku = p.sku || p.attributes?.source_sku || '';
            return (
              (p.title || '').toLowerCase().includes(q) ||
              sku.toLowerCase().includes(q) ||
              (p.brand || '').toLowerCase().includes(q)
            );
          });
        }
      }

      const filtered = productArray.filter(
        (p: Product) => !excludeIds.includes(p.product_id)
      );
      setProducts(filtered);
    } catch (error) {
      console.error('Failed to load products:', error);
      setProducts([]);
    } finally {
      setLoading(false);
    }
  };

  const handleAddProduct = (product: Product) => {
    const newSelected = new Map(selectedProducts);
    
    if (newSelected.has(product.product_id)) {
      newSelected.delete(product.product_id);
    } else {
      // In single-select mode, clear previous selection first
      if (singleSelect) {
        newSelected.clear();
      }
      // Resolve SKU from multiple possible sources
      const resolvedSku = product.sku || (product as any).attributes?.source_sku || '';
      newSelected.set(product.product_id, {
        product_id: product.product_id,
        title: product.title,
        sku: resolvedSku,
        brand: product.brand,
        quantity: 1,
        image: product.assets?.[0]?.url
      });
    }
    
    setSelectedProducts(newSelected);
  };

  const handleConfirm = () => {
    const items = Array.from(selectedProducts.values());
    onAddItems(items);
    onClose();
  };

  if (!show) return null;

  return (
    <div
      style={{
        position: 'fixed',
        top: 0,
        left: 0,
        right: 0,
        bottom: 0,
        backgroundColor: 'rgba(0, 0, 0, 0.75)',
        zIndex: 2000,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        padding: '20px'
      }}
      onClick={onClose}
    >
      <div
        style={{
          backgroundColor: 'white',
          borderRadius: '12px',
          width: '100%',
          maxWidth: '600px',
          maxHeight: '80vh',
          display: 'flex',
          flexDirection: 'column',
          boxShadow: '0 20px 60px rgba(0, 0, 0, 0.3)'
        }}
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div
          style={{
            padding: '20px 24px',
            borderBottom: '1px solid #e5e7eb',
            display: 'flex',
            justifyContent: 'space-between',
            alignItems: 'center'
          }}
        >
          <h2 style={{ fontSize: '18px', fontWeight: '600', color: '#111827', margin: 0 }}>
            {title || 'Add Product to Bundle'}
          </h2>
          <button
            onClick={onClose}
            style={{
              background: 'none',
              border: 'none',
              fontSize: '24px',
              color: '#6b7280',
              cursor: 'pointer',
              padding: '4px',
              lineHeight: '1'
            }}
          >
            ×
          </button>
        </div>

        {/* Search */}
        <div style={{ padding: '16px 24px', borderBottom: '1px solid #e5e7eb' }}>
          <div style={{ position: 'relative' }}>
            <i
              className="ri-search-line"
              style={{
                position: 'absolute',
                left: '12px',
                top: '50%',
                transform: 'translateY(-50%)',
                color: '#9ca3af',
                fontSize: '18px'
              }}
            ></i>
            <input
              type="text"
              placeholder="Search by product name or SKU..."
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              style={{
                width: '100%',
                padding: '10px 12px 10px 40px',
                border: '1px solid #d1d5db',
                borderRadius: '8px',
                fontSize: '14px',
                outline: 'none'
              }}
              onFocus={(e) => {
                e.target.style.borderColor = '#3b82f6';
                e.target.style.boxShadow = '0 0 0 3px rgba(59, 130, 246, 0.1)';
              }}
              onBlur={(e) => {
                e.target.style.borderColor = '#d1d5db';
                e.target.style.boxShadow = 'none';
              }}
            />
          </div>
        </div>

        {/* Product List */}
        <div
          style={{
            flex: 1,
            overflowY: 'auto',
            padding: '16px 24px'
          }}
        >
          {loading ? (
            <div style={{ textAlign: 'center', padding: '40px', color: '#6b7280' }}>
              <div style={{ fontSize: '14px' }}>Loading products...</div>
            </div>
          ) : products.length === 0 ? (
            <div style={{ textAlign: 'center', padding: '40px', color: '#6b7280' }}>
              <i className="ri-inbox-line" style={{ fontSize: '48px', display: 'block', marginBottom: '12px', opacity: 0.3 }}></i>
              <div style={{ fontSize: '14px' }}>
                {search ? 'No products found' : 'No products available'}
              </div>
            </div>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
              {products.map((product) => {
                const isSelected = selectedProducts.has(product.product_id);
                
                return (
                  <div
                    key={product.product_id}
                    style={{
                      display: 'flex',
                      justifyContent: 'space-between',
                      alignItems: 'center',
                      padding: '12px',
                      border: '1px solid #e5e7eb',
                      borderRadius: '8px',
                      backgroundColor: isSelected ? '#eff6ff' : 'white',
                      transition: 'all 0.2s',
                      cursor: 'pointer'
                    }}
                    onMouseEnter={(e) => {
                      if (!isSelected) {
                        e.currentTarget.style.backgroundColor = '#f9fafb';
                      }
                    }}
                    onMouseLeave={(e) => {
                      if (!isSelected) {
                        e.currentTarget.style.backgroundColor = 'white';
                      }
                    }}
                  >
                    <div style={{ flex: 1, minWidth: 0 }}>
                      <div
                        style={{
                          fontSize: '14px',
                          fontWeight: '500',
                          color: '#111827',
                          marginBottom: '4px',
                          overflow: 'hidden',
                          textOverflow: 'ellipsis',
                          whiteSpace: 'nowrap'
                        }}
                      >
                        {product.title}
                      </div>
                      <div
                        style={{
                          fontSize: '12px',
                          color: '#6b7280',
                          fontFamily: 'monospace'
                        }}
                      >
                        SKU: {product.sku || (product as any).attributes?.source_sku || 'N/A'}
                      </div>
                    </div>

                    <div
                      style={{
                        display: 'flex',
                        alignItems: 'center',
                        gap: '12px',
                        marginLeft: '16px'
                      }}
                    >
                      <div
                        style={{
                          fontSize: '14px',
                          fontWeight: '600',
                          color: '#111827',
                          minWidth: '60px',
                          textAlign: 'right'
                        }}
                      >
                        £{(Math.random() * 50 + 10).toFixed(2)}
                      </div>

                      <button
                        onClick={() => handleAddProduct(product)}
                        style={{
                          padding: '6px 14px',
                          backgroundColor: isSelected ? '#dc2626' : '#10b981',
                          color: 'white',
                          border: 'none',
                          borderRadius: '6px',
                          fontSize: '12px',
                          fontWeight: '500',
                          cursor: 'pointer',
                          display: 'flex',
                          alignItems: 'center',
                          gap: '4px',
                          transition: 'background-color 0.2s',
                          whiteSpace: 'nowrap'
                        }}
                        onMouseEnter={(e) => {
                          e.currentTarget.style.backgroundColor = isSelected ? '#b91c1c' : '#059669';
                        }}
                        onMouseLeave={(e) => {
                          e.currentTarget.style.backgroundColor = isSelected ? '#dc2626' : '#10b981';
                        }}
                      >
                        <i className={isSelected ? 'ri-check-line' : 'ri-add-line'}></i>
                        {isSelected ? 'Added' : 'Add'}
                      </button>
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </div>

        {/* Footer */}
        <div
          style={{
            padding: '16px 24px',
            borderTop: '1px solid #e5e7eb',
            display: 'flex',
            justifyContent: 'space-between',
            alignItems: 'center',
            backgroundColor: '#f9fafb'
          }}
        >
          <div style={{ fontSize: '14px', color: '#6b7280' }}>
            {singleSelect
              ? (selectedProducts.size === 1
                ? `Selected: ${Array.from(selectedProducts.values())[0]?.sku || Array.from(selectedProducts.values())[0]?.title}`
                : 'No product selected')
              : `${selectedProducts.size} product${selectedProducts.size !== 1 ? 's' : ''} selected`}
          </div>

          <div style={{ display: 'flex', gap: '12px' }}>
            <button
              onClick={onClose}
              style={{
                padding: '8px 16px',
                backgroundColor: 'white',
                border: '1px solid #d1d5db',
                borderRadius: '6px',
                fontSize: '14px',
                fontWeight: '500',
                color: '#374151',
                cursor: 'pointer',
                transition: 'all 0.2s'
              }}
              onMouseEnter={(e) => {
                e.currentTarget.style.backgroundColor = '#f3f4f6';
              }}
              onMouseLeave={(e) => {
                e.currentTarget.style.backgroundColor = 'white';
              }}
            >
              Cancel
            </button>

            <button
              onClick={handleConfirm}
              disabled={selectedProducts.size === 0}
              style={{
                padding: '8px 20px',
                backgroundColor: selectedProducts.size === 0 ? '#9ca3af' : '#3b82f6',
                color: 'white',
                border: 'none',
                borderRadius: '6px',
                fontSize: '14px',
                fontWeight: '500',
                cursor: selectedProducts.size === 0 ? 'not-allowed' : 'pointer',
                transition: 'background-color 0.2s'
              }}
              onMouseEnter={(e) => {
                if (selectedProducts.size > 0) {
                  e.currentTarget.style.backgroundColor = '#2563eb';
                }
              }}
              onMouseLeave={(e) => {
                if (selectedProducts.size > 0) {
                  e.currentTarget.style.backgroundColor = '#3b82f6';
                }
              }}
            >
              {confirmLabel || 'Add to Bundle'}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

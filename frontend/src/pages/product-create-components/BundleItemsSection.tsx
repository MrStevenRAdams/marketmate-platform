import React, { useState } from 'react';
import { BundleItem } from './types';
import ProductSearchModal from './ProductSearchModal';

interface Props {
  items: BundleItem[];
  onChange: (items: BundleItem[]) => void;
}

export default function BundleItemsSection({ items, onChange }: Props) {
  const [showSearchModal, setShowSearchModal] = useState(false);
  
  // Input row state
  const [inputSku, setInputSku] = useState('');
  const [inputQuantity, setInputQuantity] = useState('1');
  const [selectedProduct, setSelectedProduct] = useState<{
    product_id: string;
    title: string;
    sku: string;
    brand?: string;
    image?: string;
  } | null>(null);

  // Handle product selection from modal
  const handleProductSelect = (products: BundleItem[]) => {
    if (products.length > 0) {
      const product = products[0]; // Take first selected product
      setSelectedProduct({
        product_id: product.product_id,
        title: product.title,
        sku: product.sku,
        brand: product.brand,
        image: product.image
      });
      setInputSku(product.sku); // Fill SKU field
    }
  };

  // Add product to bundle
  const handleAddProduct = () => {
    if (!selectedProduct || !inputQuantity || parseInt(inputQuantity) < 1) {
      return;
    }

    const newItem: BundleItem = {
      product_id: selectedProduct.product_id,
      title: selectedProduct.title,
      sku: selectedProduct.sku,
      brand: selectedProduct.brand,
      quantity: parseInt(inputQuantity),
      image: selectedProduct.image
    };

    onChange([...items, newItem]);

    // Clear input row
    setInputSku('');
    setInputQuantity('1');
    setSelectedProduct(null);
  };

  // Update quantity of existing item
  const updateQuantity = (productId: string, quantity: number) => {
    if (quantity >= 1) {
      onChange(
        items.map(item =>
          item.product_id === productId ? { ...item, quantity } : item
        )
      );
    }
  };

  // Remove item from bundle
  const removeItem = (productId: string) => {
    onChange(items.filter(item => item.product_id !== productId));
  };

  return (
    <>
      <div className="card" style={{ marginBottom: 'var(--spacing-lg)' }}>
        <div className="card-header">
          <h2 style={{ fontSize: '18px', fontWeight: 600 }}>Bundle Items</h2>
          <p style={{ fontSize: '13px', color: 'var(--text-muted)', marginTop: '4px' }}>
            Select products to include in this bundle
          </p>
        </div>
        
        <div style={{ padding: 'var(--spacing-xl)' }}>
          {/* Input Row */}
          <div style={{
            display: 'grid',
            gridTemplateColumns: '200px 80px 120px 1fr 100px',
            gap: 'var(--spacing-md)',
            alignItems: 'center',
            padding: 'var(--spacing-md)',
            backgroundColor: 'var(--bg-tertiary)',
            borderRadius: '8px',
            border: '1px solid var(--border)',
            marginBottom: 'var(--spacing-lg)'
          }}>
            {/* SKU Input */}
            <input
              type="text"
              placeholder="SKU"
              value={inputSku}
              onChange={(e) => setInputSku(e.target.value)}
              className="input"
              style={{
                fontFamily: 'var(--font-mono)',
                fontSize: '14px'
              }}
            />

            {/* Quantity Input */}
            <input
              type="number"
              min="1"
              placeholder="Qty"
              value={inputQuantity}
              onChange={(e) => setInputQuantity(e.target.value)}
              className="input"
              style={{ textAlign: 'center' }}
            />

            {/* Select Button */}
            <button
              type="button"
              onClick={() => setShowSearchModal(true)}
              className="btn btn-secondary"
              style={{ fontSize: '13px' }}
            >
              <i className="ri-search-line" style={{ marginRight: '6px' }}></i>
              Select
            </button>

            {/* Product Name Display */}
            <div style={{
              fontSize: '14px',
              color: selectedProduct ? 'var(--text-primary)' : 'var(--text-muted)',
              fontStyle: selectedProduct ? 'normal' : 'italic',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap'
            }}>
              {selectedProduct ? selectedProduct.title : 'No product selected'}
            </div>

            {/* Add Button */}
            <button
              type="button"
              onClick={handleAddProduct}
              disabled={!selectedProduct || !inputQuantity}
              className="btn btn-primary"
              style={{
                fontSize: '13px',
                opacity: (!selectedProduct || !inputQuantity) ? 0.5 : 1
              }}
            >
              <i className="ri-add-line" style={{ marginRight: '6px' }}></i>
              Add
            </button>
          </div>

          {/* Bundle Contents List */}
          {items.length === 0 ? (
            <div style={{
              textAlign: 'center',
              padding: 'var(--spacing-4xl)',
              color: 'var(--text-muted)',
              fontSize: '14px'
            }}>
              <i className="ri-gift-line" style={{ fontSize: '48px', display: 'block', marginBottom: '12px', opacity: 0.3 }}></i>
              No products in bundle yet. Use the form above to add products.
            </div>
          ) : (
            <>
              <div style={{
                fontSize: '14px',
                fontWeight: '600',
                color: 'var(--text-primary)',
                marginBottom: 'var(--spacing-md)'
              }}>
                Bundle Contents ({items.length} {items.length === 1 ? 'product' : 'products'}):
              </div>

              <div style={{
                border: '1px solid var(--border)',
                borderRadius: '8px',
                overflow: 'hidden'
              }}>
                {items.map((item, index) => (
                  <div
                    key={item.product_id}
                    style={{
                      display: 'grid',
                      gridTemplateColumns: '1fr 150px 80px',
                      gap: 'var(--spacing-md)',
                      alignItems: 'center',
                      padding: 'var(--spacing-md)',
                      borderBottom: index < items.length - 1 ? '1px solid var(--border)' : 'none',
                      backgroundColor: 'var(--bg-secondary)',
                      transition: 'background-color 0.2s'
                    }}
                    onMouseEnter={(e) => {
                      e.currentTarget.style.backgroundColor = 'var(--bg-tertiary)';
                    }}
                    onMouseLeave={(e) => {
                      e.currentTarget.style.backgroundColor = 'var(--bg-secondary)';
                    }}
                  >
                    {/* Product Info */}
                    <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
                      {item.image && (
                        <img
                          src={item.image}
                          alt={item.title}
                          style={{
                            width: '40px',
                            height: '40px',
                            objectFit: 'cover',
                            borderRadius: '6px',
                            border: '1px solid var(--border)'
                          }}
                        />
                      )}
                      <div>
                        <div style={{
                          fontSize: '14px',
                          fontWeight: '500',
                          color: 'var(--text-primary)',
                          marginBottom: '2px'
                        }}>
                          {item.title}
                        </div>
                        <div style={{
                          fontSize: '12px',
                          fontFamily: 'var(--font-mono)',
                          color: 'var(--text-muted)'
                        }}>
                          SKU: {item.sku}
                        </div>
                      </div>
                    </div>

                    {/* Quantity Input (Editable) */}
                    <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                      <label style={{ fontSize: '12px', color: 'var(--text-muted)', fontWeight: '500' }}>
                        Qty:
                      </label>
                      <input
                        type="number"
                        min="1"
                        value={item.quantity}
                        onChange={(e) => updateQuantity(item.product_id, parseInt(e.target.value) || 1)}
                        className="input"
                        style={{
                          width: '80px',
                          textAlign: 'center',
                          fontSize: '14px'
                        }}
                      />
                    </div>

                    {/* Delete Button */}
                    <button
                      type="button"
                      onClick={() => removeItem(item.product_id)}
                      style={{
                        padding: '8px 16px',
                        backgroundColor: 'transparent',
                        border: '1px solid var(--danger)',
                        borderRadius: '6px',
                        color: 'var(--danger)',
                        fontSize: '13px',
                        cursor: 'pointer',
                        transition: 'all 0.2s',
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'center',
                        gap: '6px'
                      }}
                      onMouseEnter={(e) => {
                        e.currentTarget.style.backgroundColor = 'var(--danger)';
                        e.currentTarget.style.color = 'white';
                      }}
                      onMouseLeave={(e) => {
                        e.currentTarget.style.backgroundColor = 'transparent';
                        e.currentTarget.style.color = 'var(--danger)';
                      }}
                    >
                      <i className="ri-delete-bin-line"></i>
                      Delete
                    </button>
                  </div>
                ))}
              </div>

              {/* Bundle Summary */}
              <div style={{
                marginTop: 'var(--spacing-md)',
                padding: 'var(--spacing-md)',
                backgroundColor: 'var(--bg-tertiary)',
                borderRadius: '8px',
                border: '1px solid var(--border)',
                display: 'flex',
                justifyContent: 'space-between',
                alignItems: 'center'
              }}>
                <div style={{ fontSize: '14px', color: 'var(--text-muted)' }}>
                  Total Items in Bundle:
                </div>
                <div style={{ fontSize: '18px', fontWeight: '600', color: 'var(--text-primary)' }}>
                  {items.reduce((sum, item) => sum + item.quantity, 0)} items
                </div>
              </div>

              {/* Warning if < 2 products */}
              {items.length < 2 && (
                <div style={{
                  marginTop: 'var(--spacing-md)',
                  padding: 'var(--spacing-md)',
                  backgroundColor: 'var(--warning-glow)',
                  border: '1px solid var(--warning)',
                  borderRadius: '8px',
                  display: 'flex',
                  alignItems: 'start',
                  gap: '12px'
                }}>
                  <i className="ri-alert-line" style={{ color: 'var(--warning)', fontSize: '20px' }}></i>
                  <div style={{ fontSize: '13px', color: 'var(--text-primary)' }}>
                    <strong>Bundle Requirement:</strong> A bundle must contain at least 2 different products.
                  </div>
                </div>
              )}
            </>
          )}
        </div>
      </div>

      {/* Product Search Modal */}
      <ProductSearchModal
        show={showSearchModal}
        onClose={() => setShowSearchModal(false)}
        onAddItems={handleProductSelect}
        excludeIds={items.map(item => item.product_id)}
      />
    </>
  );
}

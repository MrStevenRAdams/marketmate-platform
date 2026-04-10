import React from 'react';

interface ProductTypeModalProps {
  isOpen: boolean;
  onClose: () => void;
  onSelect: (type: 'simple' | 'variant' | 'bundle') => void;
}

export default function ProductTypeModal({ isOpen, onClose, onSelect }: ProductTypeModalProps) {
  if (!isOpen) return null;

  const handleSelect = (type: 'simple' | 'variant' | 'bundle') => {
    onSelect(type);
    onClose();
  };

  return (
    <>
      {/* Overlay */}
      <div
        style={{
          position: 'fixed',
          top: 0,
          left: 0,
          right: 0,
          bottom: 0,
          backgroundColor: 'rgba(0, 0, 0, 0.7)',
          zIndex: 1000,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
        }}
        onClick={onClose}
      >
        {/* Modal */}
        <div
          style={{
            backgroundColor: 'white',
            borderRadius: '12px',
            width: '90%',
            maxWidth: '540px',
            padding: '32px',
            boxShadow: '0 20px 25px -5px rgba(0, 0, 0, 0.3)',
            position: 'relative',
          }}
          onClick={(e) => e.stopPropagation()}
        >
          {/* Header */}
          <div style={{ marginBottom: '24px' }}>
            <h2
              style={{
                fontSize: '24px',
                fontWeight: '600',
                color: '#1a1a1a',
                marginBottom: '8px',
              }}
            >
              Select Product Type
            </h2>
            <p style={{ fontSize: '14px', color: '#666' }}>
              Choose the type of product you want to create
            </p>
          </div>

          {/* Options */}
          <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
            {/* Simple Product */}
            <button
              onClick={() => handleSelect('simple')}
              style={{
                display: 'flex',
                alignItems: 'center',
                padding: '16px',
                border: '1px solid #e0e0e0',
                borderRadius: '8px',
                backgroundColor: 'white',
                cursor: 'pointer',
                transition: 'all 0.2s',
                textAlign: 'left',
                width: '100%',
              }}
              onMouseEnter={(e) => {
                e.currentTarget.style.backgroundColor = '#f5f9ff';
                e.currentTarget.style.borderColor = '#3b82f6';
              }}
              onMouseLeave={(e) => {
                e.currentTarget.style.backgroundColor = 'white';
                e.currentTarget.style.borderColor = '#e0e0e0';
              }}
            >
              <div
                style={{
                  width: '48px',
                  height: '48px',
                  borderRadius: '8px',
                  backgroundColor: '#e3f2fd',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  marginRight: '16px',
                  flexShrink: 0,
                }}
              >
                <svg
                  width="24"
                  height="24"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="#3b82f6"
                  strokeWidth="2"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                >
                  <circle cx="12" cy="12" r="10" />
                  <circle cx="12" cy="12" r="3" />
                </svg>
              </div>
              <div style={{ flex: 1 }}>
                <div
                  style={{
                    fontSize: '16px',
                    fontWeight: '600',
                    color: '#1a1a1a',
                    marginBottom: '4px',
                  }}
                >
                  Simple Product
                </div>
                <div style={{ fontSize: '13px', color: '#666' }}>
                  A single product with no variations (e.g., a book, a single item)
                </div>
              </div>
              <svg
                width="20"
                height="20"
                viewBox="0 0 24 24"
                fill="none"
                stroke="#999"
                strokeWidth="2"
                strokeLinecap="round"
                strokeLinejoin="round"
              >
                <polyline points="9 18 15 12 9 6" />
              </svg>
            </button>

            {/* Variation Product */}
            <button
              onClick={() => handleSelect('variant')}
              style={{
                display: 'flex',
                alignItems: 'center',
                padding: '16px',
                border: '1px solid #e0e0e0',
                borderRadius: '8px',
                backgroundColor: 'white',
                cursor: 'pointer',
                transition: 'all 0.2s',
                textAlign: 'left',
                width: '100%',
              }}
              onMouseEnter={(e) => {
                e.currentTarget.style.backgroundColor = '#f5f9ff';
                e.currentTarget.style.borderColor = '#3b82f6';
              }}
              onMouseLeave={(e) => {
                e.currentTarget.style.backgroundColor = 'white';
                e.currentTarget.style.borderColor = '#e0e0e0';
              }}
            >
              <div
                style={{
                  width: '48px',
                  height: '48px',
                  borderRadius: '8px',
                  backgroundColor: '#e3f2fd',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  marginRight: '16px',
                  flexShrink: 0,
                }}
              >
                <svg
                  width="24"
                  height="24"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="#3b82f6"
                  strokeWidth="2"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                >
                  <rect x="3" y="3" width="7" height="7" />
                  <rect x="14" y="3" width="7" height="7" />
                  <rect x="14" y="14" width="7" height="7" />
                  <rect x="3" y="14" width="7" height="7" />
                </svg>
              </div>
              <div style={{ flex: 1 }}>
                <div
                  style={{
                    fontSize: '16px',
                    fontWeight: '600',
                    color: '#1a1a1a',
                    marginBottom: '4px',
                  }}
                >
                  Variation Product
                </div>
                <div style={{ fontSize: '13px', color: '#666' }}>
                  A product with multiple variations (e.g., different sizes, colors)
                </div>
              </div>
              <svg
                width="20"
                height="20"
                viewBox="0 0 24 24"
                fill="none"
                stroke="#999"
                strokeWidth="2"
                strokeLinecap="round"
                strokeLinejoin="round"
              >
                <polyline points="9 18 15 12 9 6" />
              </svg>
            </button>

            {/* Bundled Product */}
            <button
              onClick={() => handleSelect('bundle')}
              style={{
                display: 'flex',
                alignItems: 'center',
                padding: '16px',
                border: '1px solid #e0e0e0',
                borderRadius: '8px',
                backgroundColor: 'white',
                cursor: 'pointer',
                transition: 'all 0.2s',
                textAlign: 'left',
                width: '100%',
              }}
              onMouseEnter={(e) => {
                e.currentTarget.style.backgroundColor = '#f5f9ff';
                e.currentTarget.style.borderColor = '#3b82f6';
              }}
              onMouseLeave={(e) => {
                e.currentTarget.style.backgroundColor = 'white';
                e.currentTarget.style.borderColor = '#e0e0e0';
              }}
            >
              <div
                style={{
                  width: '48px',
                  height: '48px',
                  borderRadius: '8px',
                  backgroundColor: '#e3f2fd',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  marginRight: '16px',
                  flexShrink: 0,
                }}
              >
                <svg
                  width="24"
                  height="24"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="#3b82f6"
                  strokeWidth="2"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                >
                  <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z" />
                  <polyline points="3.27 6.96 12 12.01 20.73 6.96" />
                  <line x1="12" y1="22.08" x2="12" y2="12" />
                </svg>
              </div>
              <div style={{ flex: 1 }}>
                <div
                  style={{
                    fontSize: '16px',
                    fontWeight: '600',
                    color: '#1a1a1a',
                    marginBottom: '4px',
                  }}
                >
                  Bundled Product
                </div>
                <div style={{ fontSize: '13px', color: '#666' }}>
                  A collection of multiple products sold together as one
                </div>
              </div>
              <svg
                width="20"
                height="20"
                viewBox="0 0 24 24"
                fill="none"
                stroke="#999"
                strokeWidth="2"
                strokeLinecap="round"
                strokeLinejoin="round"
              >
                <polyline points="9 18 15 12 9 6" />
              </svg>
            </button>
          </div>

          {/* Cancel Button */}
          <div style={{ marginTop: '24px', display: 'flex', justifyContent: 'flex-end' }}>
            <button
              onClick={onClose}
              style={{
                padding: '10px 24px',
                backgroundColor: '#475569',
                color: 'white',
                border: 'none',
                borderRadius: '6px',
                fontSize: '14px',
                fontWeight: '500',
                cursor: 'pointer',
                transition: 'background-color 0.2s',
              }}
              onMouseEnter={(e) => {
                e.currentTarget.style.backgroundColor = '#334155';
              }}
              onMouseLeave={(e) => {
                e.currentTarget.style.backgroundColor = '#475569';
              }}
            >
              Cancel
            </button>
          </div>
        </div>
      </div>
    </>
  );
}

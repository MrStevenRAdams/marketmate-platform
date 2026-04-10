import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { variantService } from '../services/api';
import type { Variant } from '../types';

export default function VariantList() {
  const [variants, setVariants] = useState<Variant[]>([]);
  const [loading, setLoading] = useState(true);
  const navigate = useNavigate();

  useEffect(() => {
    loadVariants();
  }, []);

  async function loadVariants() {
    try {
      setLoading(true);
      const response = await variantService.list();
      setVariants(response.data || []);
    } catch (error) {
      console.error('Failed to load variants:', error);
      setVariants([]);
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="page">
      <div className="page-header">
        <div>
          <h1 className="page-title">Variants</h1>
          <p className="page-subtitle">Manage product variants and SKUs</p>
        </div>
        <div className="page-actions">
          <button className="btn btn-primary" onClick={() => navigate('/products/create')}>
            <span>➕</span> Add Variant
          </button>
        </div>
      </div>

      <div className="card">
        <div className="table-container">
          {loading ? (
            <div className="loading-state">
              <div className="spinner"></div>
              <p>Loading variants...</p>
            </div>
          ) : variants.length === 0 ? (
            <div className="empty-state">
              <div className="empty-icon">🏷️</div>
              <h3>No variants found</h3>
              <p>Start by adding variants to your products</p>
            </div>
          ) : (
            <table className="table">
              <thead>
                <tr>
                  <th>SKU</th>
                  <th>Product</th>
                  <th>Status</th>
                  <th>Pricing</th>
                  <th>Created</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody>
                {variants.map((variant) => (
                  <tr key={variant.variant_id}>
                    <td>
                      <code style={{ fontFamily: 'monospace', color: 'var(--accent-cyan)', fontWeight: 600 }}>
                        {variant.sku}
                      </code>
                    </td>
                    <td>{variant.title || variant.product_id}</td>
                    <td>
                      <span className={`badge badge-${variant.status === 'active' ? 'success' : 'warning'}`}>
                        {variant.status}
                      </span>
                    </td>
                    <td>
                      {variant.pricing?.list_price ? (
                        <span style={{ color: 'var(--accent-cyan)', fontWeight: 600 }}>
                          {variant.pricing.list_price.currency} {variant.pricing.list_price.amount.toFixed(2)}
                        </span>
                      ) : (
                        <span className="text-muted">—</span>
                      )}
                    </td>
                    <td className="text-muted">
                      {new Date(variant.created_at).toLocaleDateString()}
                    </td>
                    <td>
                      <div className="action-buttons">
                        <button
                          className="btn-icon"
                          title="Edit product"
                          onClick={() => navigate(`/products/${variant.product_id}/edit`)}
                        >
                          ✏️
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </div>
    </div>
  );
}

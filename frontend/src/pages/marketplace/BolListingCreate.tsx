// ============================================================================
// BOL.COM LISTING CREATE PAGE
// ============================================================================
// Bol.com Retailer API v10. Listings require a valid EAN (mandatory).
// AI generation produces title + description.
// Category is looked up from Bol's category tree.
// ============================================================================

import { useState, useEffect } from 'react';
import { useSearchParams, useNavigate } from 'react-router-dom';
import { getActiveTenantId } from '../../contexts/TenantContext';
import axios from 'axios';

const BOL_BLUE = '#0077CC';
const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';

const BOL_SCHEMA_FIELDS = [
  { name: 'title', display_name: 'Product Title', data_type: 'string', required: true, allowed_values: [], max_length: 600 },
  { name: 'description', display_name: 'Product Description', data_type: 'text', required: true, allowed_values: [], max_length: 4000 },
];

interface BolDraft {
  title: string;
  description: string;
  ean: string;
  price: string;
  quantity: string;
  categoryId: string;
  categorySearch: string;
  conditionName: string;
  fulfillmentMethod: 'FBB' | 'FBR';
  productId: string;
  productTitle: string;
}

const emptyDraft = (): BolDraft => ({
  title: '', description: '', ean: '', price: '', quantity: '1',
  categoryId: '', categorySearch: '', conditionName: 'NEW',
  fulfillmentMethod: 'FBR', productId: '', productTitle: '',
});

const CONDITION_OPTIONS = [
  { value: 'NEW', label: 'New' },
  { value: 'AS_NEW', label: 'As New' },
  { value: 'GOOD', label: 'Good' },
  { value: 'REASONABLE', label: 'Reasonable' },
  { value: 'MODERATE', label: 'Moderate' },
];

export default function BolListingCreate() {
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const productId = searchParams.get('product_id') || '';
  const credentialId = searchParams.get('credential_id') || '';
  const aiFlag = searchParams.get('ai');

  const [draft, setDraft] = useState<BolDraft>(emptyDraft());
  const [loading, setLoading] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState('');
  const [success, setSuccess] = useState('');
  const [aiGenerating, setAiGenerating] = useState(false);
  const [aiApplied, setAiApplied] = useState(false);
  const [aiError, setAiError] = useState('');

  useEffect(() => {
    if (!productId) { setLoading(false); return; }
    (async () => {
      try {
        const tenantId = getActiveTenantId();
        const res = await axios.get(`${API_BASE}/products/${productId}`, {
          headers: { 'X-Tenant-Id': tenantId },
        });
        const p = res.data?.data || res.data;
        setDraft(prev => ({
          ...prev,
          productId: p.product_id || productId,
          productTitle: p.title || '',
          title: p.title || '',
          ean: p.identifiers?.ean || p.identifiers?.gtin || '',
          price: p.price ? String(p.price) : '',
          quantity: String(p.stock_quantity || 1),
        }));

        if (aiFlag === 'pending') {
          setAiGenerating(true);
          try {
            const { aiService } = await import('../../services/ai-api');
            const aiRes = await aiService.generateWithSchema({
              product_id: productId,
              channel: 'bol',
              category_id: '',
              category_name: '',
              fields: BOL_SCHEMA_FIELDS,
            });
            const listing = aiRes.data.data?.listings?.[0];
            if (listing) {
              setDraft(prev => ({
                ...prev,
                title: listing.title || prev.title,
                description: listing.description || prev.description,
              }));
              setAiApplied(true);
            }
          } catch (e: any) {
            setAiError(e.response?.data?.error || 'AI generation failed');
          }
          setAiGenerating(false);
        }
      } catch {
        // non-fatal
      } finally {
        setLoading(false);
      }
    })();
  }, [productId, aiFlag]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!draft.ean) { setError('EAN is mandatory for Bol.com listings.'); return; }
    if (!draft.title || !draft.price) { setError('Title and price are required.'); return; }
    setSubmitting(true); setError('');
    try {
      const tenantId = getActiveTenantId();
      await axios.post(`${API_BASE}/bol/offers`, {
        credential_id: credentialId,
        product_id: productId,
        ean: draft.ean,
        title: draft.title,
        description: draft.description,
        category_id: draft.categoryId,
        price: parseFloat(draft.price),
        quantity: parseInt(draft.quantity, 10) || 1,
        condition: draft.conditionName,
        fulfillment_method: draft.fulfillmentMethod,
      }, { headers: { 'X-Tenant-Id': tenantId } });
      setSuccess('Offer submitted to Bol.com successfully!');
      setTimeout(() => navigate('/marketplace/listings'), 2000);
    } catch (e: any) {
      setError(e.response?.data?.error || 'Failed to submit offer to Bol.com');
    } finally {
      setSubmitting(false);
    }
  };

  const label = (text: string, required = false) => (
    <label style={{ display: 'block', marginBottom: 4, fontSize: 12, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.05em' }}>
      {text}{required && <span style={{ color: '#ef4444', marginLeft: 2 }}>*</span>}
    </label>
  );

  const inputStyle: React.CSSProperties = {
    width: '100%', padding: '10px 12px', borderRadius: 8, fontSize: 14,
    background: 'var(--bg-primary)', color: 'var(--text-primary)',
    border: '1px solid var(--border-color)', outline: 'none', boxSizing: 'border-box',
  };

  if (loading) return (
    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '60vh' }}>
      <div style={{ width: 40, height: 40, border: `3px solid ${BOL_BLUE}`, borderTopColor: 'transparent', borderRadius: '50%', animation: 'spin 0.8s linear infinite' }} />
      <style>{`@keyframes spin { to { transform: rotate(360deg); } }`}</style>
    </div>
  );

  return (
    <div style={{ maxWidth: 760, margin: '0 auto', padding: '32px 24px' }}>
      {/* Header */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 16, marginBottom: 32 }}>
        <div style={{ width: 48, height: 48, borderRadius: 12, background: `${BOL_BLUE}20`, border: `2px solid ${BOL_BLUE}40`, display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 24 }}>📚</div>
        <div>
          <h1 style={{ fontSize: 22, fontWeight: 700, margin: 0 }}>Bol.com Listing</h1>
          {draft.productTitle && <p style={{ margin: '2px 0 0', color: 'var(--text-muted)', fontSize: 13 }}>{draft.productTitle}</p>}
        </div>
        {aiApplied && (
          <div style={{ marginLeft: 'auto', padding: '6px 14px', borderRadius: 20, background: '#10b98120', border: '1px solid #10b98140', color: '#10b981', fontSize: 12, fontWeight: 600 }}>
            ✨ AI Draft Applied
          </div>
        )}
      </div>

      {/* EAN warning */}
      <div style={{ padding: '14px 16px', borderRadius: 10, background: `${BOL_BLUE}08`, border: `1px solid ${BOL_BLUE}30`, marginBottom: 24, fontSize: 13 }}>
        <strong>⚠️ EAN required:</strong> Bol.com cannot create a listing without a valid EAN/GTIN barcode. If your product doesn't have one, you cannot list on Bol.com.
      </div>

      {aiGenerating && (
        <div style={{ padding: '14px 16px', borderRadius: 10, background: `${BOL_BLUE}10`, border: `1px solid ${BOL_BLUE}30`, marginBottom: 20, display: 'flex', alignItems: 'center', gap: 10 }}>
          <div style={{ width: 16, height: 16, border: `2px solid ${BOL_BLUE}`, borderTopColor: 'transparent', borderRadius: '50%', animation: 'spin 0.8s linear infinite', flexShrink: 0 }} />
          <span style={{ fontSize: 13, color: BOL_BLUE }}>Generating AI draft for Bol.com…</span>
          <style>{`@keyframes spin { to { transform: rotate(360deg); } }`}</style>
        </div>
      )}

      {aiError && <div style={{ padding: '12px 16px', borderRadius: 8, background: '#ef444410', border: '1px solid #ef444430', color: '#ef4444', fontSize: 13, marginBottom: 20 }}>⚠️ AI: {aiError}</div>}
      {error && <div style={{ padding: '12px 16px', borderRadius: 8, background: '#ef444410', border: '1px solid #ef444430', color: '#ef4444', fontSize: 13, marginBottom: 20 }}>{error}</div>}
      {success && <div style={{ padding: '12px 16px', borderRadius: 8, background: '#10b98110', border: '1px solid #10b98130', color: '#10b981', fontSize: 13, marginBottom: 20 }}>{success}</div>}

      <form onSubmit={handleSubmit} style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
        {/* EAN — mandatory, first */}
        <div>
          {label('EAN / GTIN (mandatory)', true)}
          <input style={{ ...inputStyle, borderColor: !draft.ean ? '#ef4444' : 'var(--border-color)' }}
            value={draft.ean} onChange={e => setDraft(d => ({ ...d, ean: e.target.value }))}
            placeholder="13-digit EAN barcode" />
          {!draft.ean && <p style={{ fontSize: 11, color: '#ef4444', marginTop: 4 }}>EAN is mandatory for Bol.com</p>}
        </div>

        {/* Title */}
        <div>
          {label('Product Title', true)}
          <input style={inputStyle} value={draft.title} onChange={e => setDraft(d => ({ ...d, title: e.target.value }))} placeholder="Product title as shown on Bol.com" />
        </div>

        {/* Description */}
        <div>
          {label('Product Description')}
          <textarea style={{ ...inputStyle, minHeight: 120, resize: 'vertical' }}
            value={draft.description} onChange={e => setDraft(d => ({ ...d, description: e.target.value }))}
            placeholder="Detailed product description…" />
        </div>

        {/* Category */}
        <div>
          {label('Category')}
          <input style={inputStyle} value={draft.categorySearch}
            onChange={e => setDraft(d => ({ ...d, categorySearch: e.target.value, categoryId: e.target.value }))}
            placeholder="Enter category name or ID (e.g. 8299 — Books)" />
          <p style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 4 }}>
            Find category IDs at <a href="https://api.bol.com/retailer/public/redoc" target="_blank" rel="noreferrer" style={{ color: BOL_BLUE }}>Bol.com API docs</a>
          </p>
        </div>

        {/* Price, Qty, Condition, Fulfillment */}
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16 }}>
          <div>
            {label('Price (€)', true)}
            <input style={inputStyle} type="number" step="0.01" min="0" value={draft.price}
              onChange={e => setDraft(d => ({ ...d, price: e.target.value }))} placeholder="0.00" />
          </div>
          <div>
            {label('Quantity', true)}
            <input style={inputStyle} type="number" min="1" value={draft.quantity}
              onChange={e => setDraft(d => ({ ...d, quantity: e.target.value }))} />
          </div>
          <div>
            {label('Condition')}
            <select style={inputStyle} value={draft.conditionName} onChange={e => setDraft(d => ({ ...d, conditionName: e.target.value }))}>
              {CONDITION_OPTIONS.map(o => <option key={o.value} value={o.value}>{o.label}</option>)}
            </select>
          </div>
          <div>
            {label('Fulfilment')}
            <select style={inputStyle} value={draft.fulfillmentMethod} onChange={e => setDraft(d => ({ ...d, fulfillmentMethod: e.target.value as 'FBB' | 'FBR' }))}>
              <option value="FBR">FBR — Fulfilled by Retailer</option>
              <option value="FBB">FBB — Fulfilled by Bol</option>
            </select>
          </div>
        </div>

        {/* Actions */}
        <div style={{ display: 'flex', gap: 12, paddingTop: 8 }}>
          <button type="button" onClick={() => navigate(-1)}
            style={{ padding: '11px 22px', borderRadius: 8, border: '1px solid var(--border-color)', background: 'transparent', color: 'var(--text-primary)', cursor: 'pointer', fontSize: 14 }}>
            Cancel
          </button>
          <button type="submit" disabled={submitting || !draft.ean}
            style={{ flex: 1, padding: '11px 0', borderRadius: 8, border: 'none', background: BOL_BLUE, color: '#fff', cursor: (submitting || !draft.ean) ? 'not-allowed' : 'pointer', fontSize: 14, fontWeight: 600, opacity: (submitting || !draft.ean) ? 0.7 : 1 }}>
            {submitting ? 'Submitting…' : '📚 Submit to Bol.com'}
          </button>
        </div>
      </form>
    </div>
  );
}

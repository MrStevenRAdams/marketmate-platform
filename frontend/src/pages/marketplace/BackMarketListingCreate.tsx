// ============================================================================
// BACK MARKET LISTING CREATE PAGE
// ============================================================================
// Back Market = refurbished goods marketplace.
// Fixed schema (no dynamic category lookup).
// Supports ?ai=pending flow: generateWithSchema → overlay results onto form.
// ============================================================================

import { useState, useEffect } from 'react';
import { useSearchParams, useNavigate } from 'react-router-dom';
import { listingService } from '../../services/marketplace-api';
import { getActiveTenantId } from '../../contexts/TenantContext';
import axios from 'axios';

const BM_BLUE = '#3D7EBF';
const BM_GRADES = ['Excellent', 'Good', 'Fair'] as const;
type BMGrade = typeof BM_GRADES[number];

const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';

interface BMDraft {
  title: string;
  description: string;
  grade: BMGrade;
  ean: string;
  price: string;
  quantity: string;
  internalSku: string;
  productId: string;
  productTitle: string;
}

const emptyDraft = (): BMDraft => ({
  title: '', description: '', grade: 'Good', ean: '',
  price: '', quantity: '1', internalSku: '', productId: '', productTitle: '',
});

// Back Market schema fields for AI generation
const BM_SCHEMA_FIELDS = [
  { name: 'title', display_name: 'Listing Title', data_type: 'string', required: true, allowed_values: [], max_length: 80 },
  { name: 'description', display_name: 'Product Description', data_type: 'text', required: true, allowed_values: [], max_length: 2000 },
  { name: 'grade', display_name: 'Condition Grade', data_type: 'enum', required: true, allowed_values: ['Excellent', 'Good', 'Fair'], max_length: 0 },
];

export default function BackMarketListingCreate() {
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const productId = searchParams.get('product_id') || '';
  const credentialId = searchParams.get('credential_id') || '';
  const aiFlag = searchParams.get('ai');

  const [draft, setDraft] = useState<BMDraft>(emptyDraft());
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
          title: (p.title || '').slice(0, 80),
          internalSku: p.sku || '',
          ean: p.identifiers?.ean || p.identifiers?.gtin || '',
          price: p.price ? String(p.price) : '',
          quantity: String(p.stock_quantity || 1),
        }));

        // AI generation flow
        if (aiFlag === 'pending') {
          setAiGenerating(true);
          try {
            const { aiService } = await import('../../services/ai-api');
            const aiRes = await aiService.generateWithSchema({
              product_id: productId,
              channel: 'backmarket',
              category_id: '',
              category_name: 'Refurbished Electronics',
              fields: BM_SCHEMA_FIELDS,
            });
            const listing = aiRes.data.data?.listings?.[0];
            if (listing) {
              setDraft(prev => ({
                ...prev,
                title: (listing.title || prev.title).slice(0, 80),
                description: listing.description || prev.description,
                grade: (BM_GRADES.includes(listing.attributes?.grade as BMGrade)
                  ? listing.attributes!.grade as BMGrade
                  : prev.grade),
              }));
              setAiApplied(true);
            }
          } catch (e: any) {
            setAiError(e.response?.data?.error || e.message || 'AI generation failed');
          }
          setAiGenerating(false);
        }
      } catch (e: any) {
        setError(e.response?.data?.error || 'Failed to load product');
      } finally {
        setLoading(false);
      }
    })();
  }, [productId, aiFlag]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!draft.title || !draft.description || !draft.grade || !draft.price) {
      setError('Title, description, grade and price are required.');
      return;
    }
    setSubmitting(true); setError('');
    try {
      const tenantId = getActiveTenantId();
      await axios.post(`${API_BASE}/backmarket/listings`, {
        credential_id: credentialId,
        product_id: productId,
        title: draft.title,
        description: draft.description,
        grade: draft.grade,
        ean: draft.ean,
        price: parseFloat(draft.price),
        quantity: parseInt(draft.quantity, 10) || 1,
        sku: draft.internalSku,
      }, { headers: { 'X-Tenant-Id': tenantId } });

      // Also save as internal listing record
      await listingService.createListing({
        product_id: productId,
        channel: 'backmarket',
        channel_account_id: credentialId,
        title: draft.title,
        description: draft.description,
        status: 'active',
        price: parseFloat(draft.price),
      } as any);

      setSuccess('Listing submitted to Back Market successfully!');
      setTimeout(() => navigate('/marketplace/listings'), 2000);
    } catch (e: any) {
      setError(e.response?.data?.error || 'Failed to submit listing');
    } finally {
      setSubmitting(false);
    }
  };

  const field = (label: string, required = false) => (
    <label style={{ display: 'block', marginBottom: 4, fontSize: 12, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.05em' }}>
      {label}{required && <span style={{ color: '#ef4444', marginLeft: 2 }}>*</span>}
    </label>
  );

  const inputStyle: React.CSSProperties = {
    width: '100%', padding: '10px 12px', borderRadius: 8, fontSize: 14,
    background: 'var(--bg-primary)', color: 'var(--text-primary)',
    border: '1px solid var(--border-color)', outline: 'none', boxSizing: 'border-box',
  };

  if (loading) return (
    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '60vh', flexDirection: 'column', gap: 16 }}>
      <div style={{ width: 40, height: 40, border: `3px solid ${BM_BLUE}`, borderTopColor: 'transparent', borderRadius: '50%', animation: 'spin 0.8s linear infinite' }} />
      {aiGenerating && <p style={{ color: 'var(--text-muted)', fontSize: 14 }}>✨ Generating AI draft…</p>}
      <style>{`@keyframes spin { to { transform: rotate(360deg); } }`}</style>
    </div>
  );

  return (
    <div style={{ maxWidth: 760, margin: '0 auto', padding: '32px 24px' }}>
      {/* Header */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 16, marginBottom: 32 }}>
        <div style={{ width: 48, height: 48, borderRadius: 12, background: `${BM_BLUE}20`, border: `2px solid ${BM_BLUE}40`, display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 24 }}>🔁</div>
        <div>
          <h1 style={{ fontSize: 22, fontWeight: 700, margin: 0 }}>Back Market Listing</h1>
          {draft.productTitle && <p style={{ margin: '2px 0 0', color: 'var(--text-muted)', fontSize: 13 }}>{draft.productTitle}</p>}
        </div>
        {aiApplied && (
          <div style={{ marginLeft: 'auto', padding: '6px 14px', borderRadius: 20, background: '#10b98120', border: '1px solid #10b98140', color: '#10b981', fontSize: 12, fontWeight: 600 }}>
            ✨ AI Draft Applied
          </div>
        )}
      </div>

      {aiGenerating && (
        <div style={{ padding: '14px 16px', borderRadius: 10, background: `${BM_BLUE}10`, border: `1px solid ${BM_BLUE}30`, marginBottom: 20, display: 'flex', alignItems: 'center', gap: 10 }}>
          <div style={{ width: 16, height: 16, border: `2px solid ${BM_BLUE}`, borderTopColor: 'transparent', borderRadius: '50%', animation: 'spin 0.8s linear infinite', flexShrink: 0 }} />
          <span style={{ fontSize: 13, color: BM_BLUE }}>Generating AI draft for Back Market…</span>
        </div>
      )}

      {aiError && (
        <div style={{ padding: '12px 16px', borderRadius: 8, background: '#ef444410', border: '1px solid #ef444430', color: '#ef4444', fontSize: 13, marginBottom: 20 }}>
          ⚠️ AI generation: {aiError} — you can still fill in the form manually.
        </div>
      )}

      {error && <div style={{ padding: '12px 16px', borderRadius: 8, background: '#ef444410', border: '1px solid #ef444430', color: '#ef4444', fontSize: 13, marginBottom: 20 }}>{error}</div>}
      {success && <div style={{ padding: '12px 16px', borderRadius: 8, background: '#10b98110', border: '1px solid #10b98130', color: '#10b981', fontSize: 13, marginBottom: 20 }}>{success}</div>}

      <form onSubmit={handleSubmit} style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
        {/* Title */}
        <div>
          {field('Listing Title', true)}
          <input style={inputStyle} value={draft.title} maxLength={80}
            onChange={e => setDraft(d => ({ ...d, title: e.target.value }))}
            placeholder="Max 80 characters" />
          <p style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 4 }}>{draft.title.length}/80 characters</p>
        </div>

        {/* Description */}
        <div>
          {field('Description', true)}
          <textarea style={{ ...inputStyle, minHeight: 140, resize: 'vertical' }}
            value={draft.description}
            onChange={e => setDraft(d => ({ ...d, description: e.target.value }))}
            placeholder="Describe the product condition, included accessories, and any defects…" />
        </div>

        {/* Grade */}
        <div>
          {field('Back Market Grade', true)}
          <div style={{ display: 'flex', gap: 10 }}>
            {BM_GRADES.map(g => (
              <button key={g} type="button"
                onClick={() => setDraft(d => ({ ...d, grade: g }))}
                style={{
                  flex: 1, padding: '10px 0', borderRadius: 8, fontSize: 13, fontWeight: 600, cursor: 'pointer',
                  border: `2px solid ${draft.grade === g ? BM_BLUE : 'var(--border-color)'}`,
                  background: draft.grade === g ? `${BM_BLUE}15` : 'var(--bg-primary)',
                  color: draft.grade === g ? BM_BLUE : 'var(--text-muted)',
                }}>
                {g === 'Excellent' ? '⭐ Excellent' : g === 'Good' ? '👍 Good' : '✔️ Fair'}
              </button>
            ))}
          </div>
          <p style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 6 }}>
            Excellent = like new · Good = minor wear · Fair = visible wear but fully functional
          </p>
        </div>

        {/* EAN + Price + Qty */}
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: 16 }}>
          <div>
            {field('EAN / GTIN')}
            <input style={inputStyle} value={draft.ean} onChange={e => setDraft(d => ({ ...d, ean: e.target.value }))} placeholder="e.g. 0194252339219" />
          </div>
          <div>
            {field('Price (£)', true)}
            <input style={inputStyle} type="number" step="0.01" min="0" value={draft.price}
              onChange={e => setDraft(d => ({ ...d, price: e.target.value }))} placeholder="0.00" />
          </div>
          <div>
            {field('Quantity', true)}
            <input style={inputStyle} type="number" min="1" value={draft.quantity}
              onChange={e => setDraft(d => ({ ...d, quantity: e.target.value }))} />
          </div>
        </div>

        {/* Actions */}
        <div style={{ display: 'flex', gap: 12, paddingTop: 8 }}>
          <button type="button" onClick={() => navigate(-1)}
            style={{ padding: '11px 22px', borderRadius: 8, border: '1px solid var(--border-color)', background: 'transparent', color: 'var(--text-primary)', cursor: 'pointer', fontSize: 14 }}>
            Cancel
          </button>
          <button type="submit" disabled={submitting}
            style={{ flex: 1, padding: '11px 0', borderRadius: 8, border: 'none', background: BM_BLUE, color: '#fff', cursor: submitting ? 'not-allowed' : 'pointer', fontSize: 14, fontWeight: 600, opacity: submitting ? 0.7 : 1 }}>
            {submitting ? 'Submitting…' : '🔁 Submit to Back Market'}
          </button>
        </div>
      </form>
    </div>
  );
}

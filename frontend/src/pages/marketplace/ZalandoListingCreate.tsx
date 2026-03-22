// ============================================================================
// ZALANDO LISTING CREATE PAGE
// ============================================================================
// Zalando's ZDirect API does NOT support programmatic listing creation.
// Sellers must use the Zalando Partner Portal (partner.zalando.com).
// This page:
//   1. Clearly explains the limitation
//   2. Generates an AI draft if ?ai=pending
//   3. Lets the seller copy-paste into Partner Portal
// ============================================================================

import { useState, useEffect } from 'react';
import { useSearchParams } from 'react-router-dom';
import { getActiveTenantId } from '../../contexts/TenantContext';
import axios from 'axios';

const ZO_ORANGE = '#F3712D';
const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';

const ZALANDO_SCHEMA_FIELDS = [
  { name: 'title', display_name: 'Product Name', data_type: 'string', required: true, allowed_values: [], max_length: 250 },
  { name: 'description', display_name: 'Product Description', data_type: 'text', required: true, allowed_values: [], max_length: 5000 },
  { name: 'materials', display_name: 'Materials & Composition', data_type: 'string', required: false, allowed_values: [], max_length: 500 },
  { name: 'care_instructions', display_name: 'Care Instructions', data_type: 'string', required: false, allowed_values: [], max_length: 500 },
];

interface AIDraft {
  title: string;
  description: string;
  attributes: Record<string, string>;
}

export default function ZalandoListingCreate() {
  const [searchParams] = useSearchParams();
  const productId = searchParams.get('product_id') || '';
  const aiFlag = searchParams.get('ai');

  const [productTitle, setProductTitle] = useState('');
  const [loading, setLoading] = useState(true);
  const [aiGenerating, setAiGenerating] = useState(false);
  const [aiDraft, setAiDraft] = useState<AIDraft | null>(null);
  const [aiError, setAiError] = useState('');
  const [copied, setCopied] = useState<string | null>(null);

  useEffect(() => {
    if (!productId) { setLoading(false); return; }
    (async () => {
      try {
        const tenantId = getActiveTenantId();
        const res = await axios.get(`${API_BASE}/products/${productId}`, {
          headers: { 'X-Tenant-Id': tenantId },
        });
        const p = res.data?.data || res.data;
        setProductTitle(p.title || '');

        if (aiFlag === 'pending') {
          setAiGenerating(true);
          try {
            const { aiService } = await import('../../services/ai-api');
            const aiRes = await aiService.generateWithSchema({
              product_id: productId,
              channel: 'zalando',
              category_id: '',
              category_name: 'Fashion',
              fields: ZALANDO_SCHEMA_FIELDS,
            });
            const listing = aiRes.data.data?.listings?.[0];
            if (listing) {
              setAiDraft({
                title: listing.title || '',
                description: listing.description || '',
                attributes: listing.attributes
                  ? Object.fromEntries(Object.entries(listing.attributes).map(([k, v]) => [k, String(v)]))
                  : {},
              });
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

  const copyToClipboard = (text: string, key: string) => {
    navigator.clipboard.writeText(text).then(() => {
      setCopied(key);
      setTimeout(() => setCopied(null), 2000);
    });
  };

  const CopyField = ({ label, value, fieldKey }: { label: string; value: string; fieldKey: string }) => (
    <div style={{ marginBottom: 20 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 6 }}>
        <label style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.05em' }}>{label}</label>
        <button onClick={() => copyToClipboard(value, fieldKey)}
          style={{ padding: '4px 10px', borderRadius: 6, border: `1px solid ${ZO_ORANGE}40`, background: copied === fieldKey ? '#10b98115' : `${ZO_ORANGE}10`, color: copied === fieldKey ? '#10b981' : ZO_ORANGE, cursor: 'pointer', fontSize: 11, fontWeight: 600 }}>
          {copied === fieldKey ? '✓ Copied' : 'Copy'}
        </button>
      </div>
      <div style={{ padding: '12px 14px', borderRadius: 8, background: 'var(--bg-primary)', border: '1px solid var(--border-color)', fontSize: 13, color: 'var(--text-primary)', whiteSpace: 'pre-wrap', wordBreak: 'break-word', minHeight: 48 }}>
        {value || <span style={{ color: 'var(--text-muted)', fontStyle: 'italic' }}>No content generated</span>}
      </div>
    </div>
  );

  return (
    <div style={{ maxWidth: 760, margin: '0 auto', padding: '32px 24px' }}>
      {/* Header */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 16, marginBottom: 28 }}>
        <div style={{ width: 48, height: 48, borderRadius: 12, background: `${ZO_ORANGE}20`, border: `2px solid ${ZO_ORANGE}40`, display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 24 }}>👟</div>
        <div>
          <h1 style={{ fontSize: 22, fontWeight: 700, margin: 0 }}>Zalando Listing</h1>
          {productTitle && <p style={{ margin: '2px 0 0', color: 'var(--text-muted)', fontSize: 13 }}>{productTitle}</p>}
        </div>
      </div>

      {/* API Limitation Banner */}
      <div style={{ padding: '18px 20px', borderRadius: 12, background: `${ZO_ORANGE}10`, border: `1px solid ${ZO_ORANGE}40`, marginBottom: 28 }}>
        <div style={{ display: 'flex', gap: 12, alignItems: 'flex-start' }}>
          <span style={{ fontSize: 20, flexShrink: 0 }}>ℹ️</span>
          <div>
            <p style={{ margin: '0 0 8px', fontWeight: 700, fontSize: 15 }}>Zalando doesn't support API listing creation</p>
            <p style={{ margin: '0 0 10px', fontSize: 13, color: 'var(--text-muted)', lineHeight: 1.6 }}>
              The Zalando ZDirect API manages price, stock, orders, and tracking — but new product listings must be created manually through the <strong>Zalando Partner Portal</strong>. Once your article is live, MarketMate handles everything else automatically.
            </p>
            <a href="https://partner.zalando.com" target="_blank" rel="noreferrer"
              style={{ display: 'inline-flex', alignItems: 'center', gap: 6, padding: '8px 16px', borderRadius: 8, background: ZO_ORANGE, color: '#fff', fontSize: 13, fontWeight: 600, textDecoration: 'none' }}>
              Open Zalando Partner Portal →
            </a>
          </div>
        </div>
      </div>

      {aiGenerating && (
        <div style={{ padding: '14px 16px', borderRadius: 10, background: `${ZO_ORANGE}10`, border: `1px solid ${ZO_ORANGE}30`, marginBottom: 20, display: 'flex', alignItems: 'center', gap: 10 }}>
          <div style={{ width: 16, height: 16, border: `2px solid ${ZO_ORANGE}`, borderTopColor: 'transparent', borderRadius: '50%', animation: 'spin 0.8s linear infinite', flexShrink: 0 }} />
          <span style={{ fontSize: 13, color: ZO_ORANGE }}>Generating AI draft content for Zalando…</span>
          <style>{`@keyframes spin { to { transform: rotate(360deg); } }`}</style>
        </div>
      )}

      {aiError && (
        <div style={{ padding: '12px 16px', borderRadius: 8, background: '#ef444410', border: '1px solid #ef444430', color: '#ef4444', fontSize: 13, marginBottom: 20 }}>
          ⚠️ AI generation failed: {aiError}
        </div>
      )}

      {(aiDraft || loading) && (
        <div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 20 }}>
            <span style={{ width: 6, height: 6, borderRadius: '50%', background: '#10b981' }} />
            <h2 style={{ fontSize: 16, fontWeight: 700, margin: 0 }}>
              {loading ? 'Loading…' : aiDraft ? '✨ AI-Generated Draft — Copy into Partner Portal' : 'AI Draft'}
            </h2>
          </div>

          {aiDraft && (
            <>
              <CopyField label="Product Name" value={aiDraft.title} fieldKey="title" />
              <CopyField label="Product Description" value={aiDraft.description} fieldKey="description" />
              {aiDraft.attributes.materials && <CopyField label="Materials" value={aiDraft.attributes.materials} fieldKey="materials" />}
              {aiDraft.attributes.care_instructions && <CopyField label="Care Instructions" value={aiDraft.attributes.care_instructions} fieldKey="care" />}

              <button onClick={() => {
                const full = `PRODUCT NAME:\n${aiDraft.title}\n\nDESCRIPTION:\n${aiDraft.description}` +
                  (aiDraft.attributes.materials ? `\n\nMATERIALS:\n${aiDraft.attributes.materials}` : '') +
                  (aiDraft.attributes.care_instructions ? `\n\nCARE INSTRUCTIONS:\n${aiDraft.attributes.care_instructions}` : '');
                copyToClipboard(full, 'all');
              }}
                style={{ width: '100%', padding: '12px 0', borderRadius: 8, border: `1px solid ${ZO_ORANGE}`, background: 'transparent', color: ZO_ORANGE, cursor: 'pointer', fontSize: 14, fontWeight: 600 }}>
                {copied === 'all' ? '✓ All content copied!' : 'Copy All Content'}
              </button>
            </>
          )}
        </div>
      )}

      {!loading && !aiDraft && !aiGenerating && (
        <div style={{ padding: '32px', textAlign: 'center', color: 'var(--text-muted)', fontSize: 14 }}>
          <p>No AI draft available. Navigate here with <code>?ai=pending</code> to generate content, or create the listing directly in the Zalando Partner Portal.</p>
        </div>
      )}
    </div>
  );
}

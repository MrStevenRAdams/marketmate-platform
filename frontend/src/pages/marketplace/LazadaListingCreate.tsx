// ============================================================================
// LAZADA LISTING CREATE PAGE
// ============================================================================
// Lazada's API does NOT support programmatic product listing creation.
// Sellers must use Lazada Seller Center (sellercenter.lazada.com).
// The API only manages price/stock/orders/tracking once a product is live.
// This page explains the limitation and generates an AI draft for copy-paste.
// ============================================================================

import { useState, useEffect } from 'react';
import { useSearchParams } from 'react-router-dom';
import { getActiveTenantId } from '../../contexts/TenantContext';
import axios from 'axios';

const LZ_ORANGE = '#F57226';
const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';

const LAZADA_SCHEMA_FIELDS = [
  { name: 'name', display_name: 'Product Name', data_type: 'string', required: true, allowed_values: [], max_length: 255 },
  { name: 'description', display_name: 'Product Description', data_type: 'text', required: true, allowed_values: [], max_length: 25000 },
  { name: 'short_description', display_name: 'Short Description', data_type: 'string', required: false, allowed_values: [], max_length: 255 },
  { name: 'highlights', display_name: 'Product Highlights', data_type: 'text', required: false, allowed_values: [], max_length: 1000 },
];

interface AIDraft {
  title: string;
  description: string;
  short_description: string;
  highlights: string;
}

export default function LazadaListingCreate() {
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
              channel: 'lazada',
              category_id: '',
              category_name: '',
              fields: LAZADA_SCHEMA_FIELDS,
            });
            const listing = aiRes.data.data?.listings?.[0];
            if (listing) {
              setAiDraft({
                title: listing.title || '',
                description: listing.description || '',
                short_description: listing.attributes?.short_description ? String(listing.attributes.short_description) : '',
                highlights: listing.attributes?.highlights ? String(listing.attributes.highlights) : listing.bullet_points?.join('\n') || '',
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

  const CopyField = ({ label, value, fieldKey, multiline = false }: { label: string; value: string; fieldKey: string; multiline?: boolean }) => (
    <div style={{ marginBottom: 20 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 6 }}>
        <label style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.05em' }}>{label}</label>
        <button onClick={() => copyToClipboard(value, fieldKey)}
          style={{ padding: '4px 10px', borderRadius: 6, border: `1px solid ${LZ_ORANGE}40`, background: copied === fieldKey ? '#10b98115' : `${LZ_ORANGE}10`, color: copied === fieldKey ? '#10b981' : LZ_ORANGE, cursor: 'pointer', fontSize: 11, fontWeight: 600 }}>
          {copied === fieldKey ? '✓ Copied' : 'Copy'}
        </button>
      </div>
      <div style={{ padding: '12px 14px', borderRadius: 8, background: 'var(--bg-primary)', border: '1px solid var(--border-color)', fontSize: 13, color: 'var(--text-primary)', whiteSpace: multiline ? 'pre-wrap' : 'normal', wordBreak: 'break-word', minHeight: 48 }}>
        {value || <span style={{ color: 'var(--text-muted)', fontStyle: 'italic' }}>No content generated</span>}
      </div>
    </div>
  );

  return (
    <div style={{ maxWidth: 760, margin: '0 auto', padding: '32px 24px' }}>
      {/* Header */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 16, marginBottom: 28 }}>
        <div style={{ width: 48, height: 48, borderRadius: 12, background: `${LZ_ORANGE}20`, border: `2px solid ${LZ_ORANGE}40`, display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 24 }}>🛍️</div>
        <div>
          <h1 style={{ fontSize: 22, fontWeight: 700, margin: 0 }}>Lazada Listing</h1>
          {productTitle && <p style={{ margin: '2px 0 0', color: 'var(--text-muted)', fontSize: 13 }}>{productTitle}</p>}
        </div>
      </div>

      {/* API Limitation Banner */}
      <div style={{ padding: '18px 20px', borderRadius: 12, background: `${LZ_ORANGE}10`, border: `1px solid ${LZ_ORANGE}40`, marginBottom: 28 }}>
        <div style={{ display: 'flex', gap: 12, alignItems: 'flex-start' }}>
          <span style={{ fontSize: 20, flexShrink: 0 }}>ℹ️</span>
          <div>
            <p style={{ margin: '0 0 8px', fontWeight: 700, fontSize: 15 }}>Lazada doesn't support API listing creation</p>
            <p style={{ margin: '0 0 12px', fontSize: 13, color: 'var(--text-muted)', lineHeight: 1.6 }}>
              The Lazada Seller API manages pricing, stock, orders and fulfilment — but new product listings must be created through <strong>Lazada Seller Center</strong>. Once your SKU is live, MarketMate automatically syncs orders and stock.
            </p>
            <div style={{ display: 'flex', gap: 10, flexWrap: 'wrap' }}>
              <a href="https://sellercenter.lazada.com" target="_blank" rel="noreferrer"
                style={{ display: 'inline-flex', alignItems: 'center', gap: 6, padding: '8px 16px', borderRadius: 8, background: LZ_ORANGE, color: '#fff', fontSize: 13, fontWeight: 600, textDecoration: 'none' }}>
                Open Lazada Seller Center →
              </a>
              <a href="https://sellercenter.lazada.com/apps/seller/catalog/add-product" target="_blank" rel="noreferrer"
                style={{ display: 'inline-flex', alignItems: 'center', gap: 6, padding: '8px 16px', borderRadius: 8, border: `1px solid ${LZ_ORANGE}`, color: LZ_ORANGE, fontSize: 13, fontWeight: 600, textDecoration: 'none', background: 'transparent' }}>
                Add Product Page →
              </a>
            </div>
          </div>
        </div>
      </div>

      {aiGenerating && (
        <div style={{ padding: '14px 16px', borderRadius: 10, background: `${LZ_ORANGE}10`, border: `1px solid ${LZ_ORANGE}30`, marginBottom: 20, display: 'flex', alignItems: 'center', gap: 10 }}>
          <div style={{ width: 16, height: 16, border: `2px solid ${LZ_ORANGE}`, borderTopColor: 'transparent', borderRadius: '50%', animation: 'spin 0.8s linear infinite', flexShrink: 0 }} />
          <span style={{ fontSize: 13, color: LZ_ORANGE }}>Generating AI draft for Lazada…</span>
          <style>{`@keyframes spin { to { transform: rotate(360deg); } }`}</style>
        </div>
      )}

      {aiError && (
        <div style={{ padding: '12px 16px', borderRadius: 8, background: '#ef444410', border: '1px solid #ef444430', color: '#ef4444', fontSize: 13, marginBottom: 20 }}>
          ⚠️ AI generation failed: {aiError}
        </div>
      )}

      {aiDraft && (
        <div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 20 }}>
            <span style={{ fontSize: 16 }}>✨</span>
            <h2 style={{ fontSize: 16, fontWeight: 700, margin: 0 }}>AI-Generated Draft — Copy into Seller Center</h2>
          </div>

          <CopyField label="Product Name" value={aiDraft.title} fieldKey="title" />
          {aiDraft.short_description && <CopyField label="Short Description" value={aiDraft.short_description} fieldKey="short_desc" />}
          <CopyField label="Full Description" value={aiDraft.description} fieldKey="description" multiline />
          {aiDraft.highlights && <CopyField label="Product Highlights" value={aiDraft.highlights} fieldKey="highlights" multiline />}

          <button onClick={() => {
            const full = `PRODUCT NAME:\n${aiDraft.title}` +
              (aiDraft.short_description ? `\n\nSHORT DESCRIPTION:\n${aiDraft.short_description}` : '') +
              `\n\nFULL DESCRIPTION:\n${aiDraft.description}` +
              (aiDraft.highlights ? `\n\nHIGHLIGHTS:\n${aiDraft.highlights}` : '');
            copyToClipboard(full, 'all');
          }}
            style={{ width: '100%', padding: '12px 0', borderRadius: 8, border: `1px solid ${LZ_ORANGE}`, background: 'transparent', color: LZ_ORANGE, cursor: 'pointer', fontSize: 14, fontWeight: 600 }}>
            {copied === 'all' ? '✓ All content copied!' : 'Copy All Content'}
          </button>
        </div>
      )}

      {!loading && !aiDraft && !aiGenerating && (
        <div style={{ padding: '32px', textAlign: 'center', color: 'var(--text-muted)', fontSize: 14, background: 'var(--bg-secondary)', borderRadius: 12, border: '1px solid var(--border-color)' }}>
          <p style={{ margin: 0 }}>Navigate here with <code style={{ padding: '2px 6px', borderRadius: 4, background: 'var(--bg-primary)', fontSize: 12 }}>?ai=pending</code> to generate draft content for copy-pasting into Lazada Seller Center.</p>
        </div>
      )}
    </div>
  );
}

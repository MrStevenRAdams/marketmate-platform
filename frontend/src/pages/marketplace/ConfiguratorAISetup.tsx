// ============================================================================
// CONFIGURATOR AI SETUP — SESSION F (USP-01)
// ============================================================================
// Location: frontend/src/pages/marketplace/ConfiguratorAISetup.tsx
// Route:    /marketplace/configurators/ai-setup
//
// 3-step wizard:
//   Step 1 — Channel + credential + product description
//   Step 2 — AI suggestion review (category, attributes, shipping)
//   Step 3 — Apply → create configurator → redirect to ConfiguratorDetail
// ============================================================================

import { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { configuratorService, type AttributeDefault } from '../../services/configurator-api';
import { credentialService } from '../../services/marketplace-api';

// ── Shared styles ─────────────────────────────────────────────────────────────

const cardStyle: React.CSSProperties = {
  background: 'var(--bg-secondary)',
  border: '1px solid var(--border)',
  borderRadius: 12,
  padding: 24,
  marginBottom: 20,
};

const labelStyle: React.CSSProperties = {
  display: 'block', fontSize: 11, fontWeight: 700, color: 'var(--text-muted)',
  textTransform: 'uppercase', letterSpacing: '0.5px', marginBottom: 6,
};

const inputStyle: React.CSSProperties = {
  width: '100%', padding: '10px 14px', borderRadius: 8,
  background: 'var(--bg-primary)', border: '1px solid var(--border)',
  color: 'var(--text-primary)', fontSize: 14, outline: 'none',
  boxSizing: 'border-box',
};

const sectionTitle: React.CSSProperties = {
  fontSize: 15, fontWeight: 700, color: 'var(--text-primary)',
  marginBottom: 14, paddingBottom: 10, borderBottom: '1px solid var(--border)',
};

// ── Channel definitions ───────────────────────────────────────────────────────

const ALL_CHANNELS = [
  { id: 'amazon',      label: 'Amazon',      emoji: '📦' },
  { id: 'ebay',        label: 'eBay',         emoji: '🏷️' },
  { id: 'shopify',     label: 'Shopify',      emoji: '🛒' },
  { id: 'woocommerce', label: 'WooCommerce',  emoji: '🟣' },
  { id: 'etsy',        label: 'Etsy',         emoji: '🧶' },
  { id: 'kaufland',    label: 'Kaufland',     emoji: '🟠' },
  { id: 'walmart',     label: 'Walmart',      emoji: '🟡' },
  { id: 'tiktok',      label: 'TikTok Shop',  emoji: '🎵' },
  { id: 'temu',        label: 'Temu',         emoji: '🛍️' },
  { id: 'bigcommerce', label: 'BigCommerce',  emoji: '🛍️' },
  { id: 'magento',     label: 'Magento',      emoji: '🔶' },
  { id: 'onbuy',       label: 'OnBuy',        emoji: '🔵' },
];

// ── Step type ─────────────────────────────────────────────────────────────────

type Step = 'describe' | 'review' | 'applying';

// ── AI suggestion shape ───────────────────────────────────────────────────────

interface AISuggestion {
  category_id: string;
  category_path: string;
  attribute_defaults: AttributeDefault[];
  shipping_defaults: Record<string, string>;
  reasoning: string;
}

// ============================================================================
// COMPONENT
// ============================================================================

export default function ConfiguratorAISetup() {
  const navigate = useNavigate();

  // Step state
  const [step, setStep] = useState<Step>('describe');

  // Step 1 form
  const [channel, setChannel] = useState('amazon');
  const [credentialId, setCredentialId] = useState('');
  const [productDescription, setProductDescription] = useState('');
  const [credentials, setCredentials] = useState<any[]>([]);
  const [credsLoading, setCredsLoading] = useState(false);

  // AI loading / result
  const [aiLoading, setAiLoading] = useState(false);
  const [aiError, setAiError] = useState('');
  const [suggestion, setSuggestion] = useState<AISuggestion | null>(null);

  // Editable suggestion fields (user can tweak before applying)
  const [editedName, setEditedName] = useState('');
  const [editedCategoryId, setEditedCategoryId] = useState('');
  const [editedCategoryPath, setEditedCategoryPath] = useState('');
  const [editedAttrs, setEditedAttrs] = useState<AttributeDefault[]>([]);
  const [editedShipping, setEditedShipping] = useState<{ key: string; value: string }[]>([]);

  // Apply state
  const [applyError, setApplyError] = useState('');

  // ── Load credentials when channel changes ────────────────────────────────

  useEffect(() => {
    setCredsLoading(true);
    setCredentialId('');
    credentialService
      .list()
      .then(res => {
        const all = (res.data as any)?.credentials || [];
        const filtered = all.filter((c: any) => c.channel === channel && c.active !== false);
        setCredentials(filtered);
        if (filtered.length > 0) setCredentialId(filtered[0].credential_id);
      })
      .catch(() => setCredentials([]))
      .finally(() => setCredsLoading(false));
  }, [channel]);

  // ── Step 1 → Step 2: call AI ──────────────────────────────────────────────

  async function handleGenerate() {
    if (!productDescription.trim()) {
      setAiError('Please describe your product type before generating.');
      return;
    }
    setAiError('');
    setAiLoading(true);
    try {
      const res = await configuratorService.aiSetup(channel, productDescription.trim(), credentialId);
      const data = res.data;
      if (!data.ok || !data.suggestion) {
        setAiError(data.error || 'AI generation failed. Please try again.');
        return;
      }
      const s = data.suggestion;
      setSuggestion(s);
      // Pre-populate editable fields
      setEditedName(`${ALL_CHANNELS.find(c => c.id === channel)?.label || channel} — AI Setup`);
      setEditedCategoryId(s.category_id);
      setEditedCategoryPath(s.category_path);
      setEditedAttrs(s.attribute_defaults || []);
      setEditedShipping(
        Object.entries(s.shipping_defaults || {}).map(([key, value]) => ({ key, value }))
      );
      setStep('review');
    } catch (e: any) {
      setAiError(e.response?.data?.error || 'AI generation failed. Please try again.');
    } finally {
      setAiLoading(false);
    }
  }

  // ── Step 2 → create configurator ─────────────────────────────────────────

  async function handleApply() {
    setApplyError('');
    setStep('applying');

    const shippingMap: Record<string, string> = {};
    for (const row of editedShipping) {
      if (row.key.trim()) shippingMap[row.key.trim()] = row.value;
    }

    try {
      const res = await configuratorService.create({
        name: editedName || `${channel} AI Setup`,
        channel,
        channel_credential_id: credentialId || undefined,
        category_id: editedCategoryId,
        category_path: editedCategoryPath,
        attribute_defaults: editedAttrs.filter(a => a.attribute_name.trim()),
        shipping_defaults: shippingMap,
      });
      const newId = res.data.configurator.configurator_id;
      navigate(`/marketplace/configurators/${newId}`);
    } catch (e: any) {
      setApplyError(e.response?.data?.error || 'Failed to create configurator. Please try again.');
      setStep('review');
    }
  }

  // ── Attribute row helpers ─────────────────────────────────────────────────

  function updateAttr(idx: number, field: keyof AttributeDefault, value: string) {
    setEditedAttrs(prev => prev.map((a, i) => i === idx ? { ...a, [field]: value } : a));
  }

  function removeAttr(idx: number) {
    setEditedAttrs(prev => prev.filter((_, i) => i !== idx));
  }

  function addAttr() {
    setEditedAttrs(prev => [...prev, { attribute_name: '', source: 'default_value', default_value: '' }]);
  }

  function updateShipping(idx: number, field: 'key' | 'value', value: string) {
    setEditedShipping(prev => prev.map((r, i) => i === idx ? { ...r, [field]: value } : r));
  }

  function removeShipping(idx: number) {
    setEditedShipping(prev => prev.filter((_, i) => i !== idx));
  }

  // ── Step indicator ────────────────────────────────────────────────────────

  function StepIndicator() {
    const steps = [
      { num: 1, label: 'Describe Product' },
      { num: 2, label: 'Review Suggestion' },
      { num: 3, label: 'Apply' },
    ];
    const currentNum = step === 'describe' ? 1 : step === 'review' ? 2 : 3;

    return (
      <div style={{ display: 'flex', alignItems: 'center', marginBottom: 28 }}>
        {steps.map((s, i) => (
          <div key={s.num} style={{ display: 'flex', alignItems: 'center', flex: i < steps.length - 1 ? 1 : undefined }}>
            <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 4 }}>
              <div style={{
                width: 32, height: 32, borderRadius: '50%', display: 'flex', alignItems: 'center', justifyContent: 'center',
                fontWeight: 700, fontSize: 13,
                background: s.num <= currentNum ? 'var(--primary)' : 'var(--bg-tertiary)',
                color: s.num <= currentNum ? '#fff' : 'var(--text-muted)',
                border: s.num === currentNum ? '2px solid var(--primary)' : '2px solid transparent',
              }}>
                {s.num < currentNum ? '✓' : s.num}
              </div>
              <span style={{ fontSize: 11, color: s.num === currentNum ? 'var(--primary)' : 'var(--text-muted)', fontWeight: 600, whiteSpace: 'nowrap' }}>
                {s.label}
              </span>
            </div>
            {i < steps.length - 1 && (
              <div style={{ flex: 1, height: 2, background: s.num < currentNum ? 'var(--primary)' : 'var(--border)', margin: '0 8px', marginBottom: 18 }} />
            )}
          </div>
        ))}
      </div>
    );
  }

  // ── Render ────────────────────────────────────────────────────────────────

  return (
    <div style={{ maxWidth: 760, margin: '0 auto', padding: '32px 24px' }}>
      {/* Page header */}
      <div style={{ marginBottom: 28 }}>
        <button
          onClick={() => step === 'review' ? setStep('describe') : navigate('/marketplace/configurators')}
          style={{ background: 'none', border: 'none', color: 'var(--text-muted)', cursor: 'pointer', fontSize: 13, padding: 0, marginBottom: 12 }}
        >
          ← Back
        </button>
        <h1 style={{ fontSize: 24, fontWeight: 800, color: 'var(--text-primary)', margin: 0 }}>
          🤖 AI Configurator Setup
        </h1>
        <p style={{ fontSize: 14, color: 'var(--text-muted)', marginTop: 6 }}>
          Describe your product and we'll suggest the best category, attributes and shipping defaults for your chosen channel.
        </p>
      </div>

      <StepIndicator />

      {/* ── STEP 1: Describe ─────────────────────────────────────────────── */}
      {step === 'describe' && (
        <>
          <div style={cardStyle}>
            <div style={sectionTitle}>1. Choose Channel</div>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(130px, 1fr))', gap: 8 }}>
              {ALL_CHANNELS.map(ch => (
                <button
                  key={ch.id}
                  onClick={() => setChannel(ch.id)}
                  style={{
                    padding: '10px 12px', borderRadius: 8, cursor: 'pointer',
                    border: channel === ch.id ? '2px solid var(--primary)' : '1px solid var(--border)',
                    background: channel === ch.id ? 'var(--primary-glow, rgba(99,102,241,0.08))' : 'var(--bg-primary)',
                    color: channel === ch.id ? 'var(--primary)' : 'var(--text-primary)',
                    fontWeight: channel === ch.id ? 700 : 500, fontSize: 13, textAlign: 'center',
                    transition: 'all 0.15s',
                  }}
                >
                  {ch.emoji} {ch.label}
                </button>
              ))}
            </div>
          </div>

          <div style={cardStyle}>
            <div style={sectionTitle}>2. Select Account (optional)</div>
            {credsLoading ? (
              <p style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading credentials…</p>
            ) : credentials.length === 0 ? (
              <p style={{ color: 'var(--text-muted)', fontSize: 13 }}>No {channel} credentials connected. AI will still suggest settings based on channel knowledge.</p>
            ) : (
              <select
                value={credentialId}
                onChange={e => setCredentialId(e.target.value)}
                style={{ ...inputStyle, width: 'auto', maxWidth: 320 }}
              >
                {credentials.map((c: any) => (
                  <option key={c.credential_id} value={c.credential_id}>
                    {c.account_name || c.credential_id}
                  </option>
                ))}
              </select>
            )}
          </div>

          <div style={cardStyle}>
            <div style={sectionTitle}>3. Describe Your Product</div>
            <p style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 12 }}>
              Be specific — include the product type, key features, and any relevant details (e.g. "wireless Bluetooth headphones for gaming, over-ear, noise-cancelling, USB-C charging").
            </p>
            <label style={labelStyle}>Product Description</label>
            <textarea
              value={productDescription}
              onChange={e => setProductDescription(e.target.value)}
              placeholder="Describe your product type and key features…"
              rows={5}
              style={{ ...inputStyle, resize: 'vertical', lineHeight: 1.6 }}
            />
            {aiError && (
              <p style={{ color: 'var(--danger)', fontSize: 13, marginTop: 8 }}>{aiError}</p>
            )}
          </div>

          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 12 }}>
            <button
              onClick={() => navigate('/marketplace/configurators')}
              style={{ padding: '10px 20px', background: 'var(--bg-tertiary)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-primary)', cursor: 'pointer', fontSize: 14, fontWeight: 600 }}
            >
              Cancel
            </button>
            <button
              onClick={handleGenerate}
              disabled={aiLoading || !productDescription.trim()}
              style={{ padding: '10px 24px', background: aiLoading ? 'var(--bg-tertiary)' : 'var(--primary)', border: 'none', borderRadius: 8, color: aiLoading ? 'var(--text-muted)' : '#fff', cursor: aiLoading || !productDescription.trim() ? 'not-allowed' : 'pointer', fontSize: 14, fontWeight: 700, opacity: !productDescription.trim() ? 0.5 : 1 }}
            >
              {aiLoading ? '🤖 Generating…' : '🤖 Generate Suggestion'}
            </button>
          </div>
        </>
      )}

      {/* ── STEP 2: Review ───────────────────────────────────────────────── */}
      {step === 'review' && suggestion && (
        <>
          {/* AI Reasoning banner */}
          <div style={{ background: 'var(--primary-glow, rgba(99,102,241,0.08))', border: '1px solid var(--primary)', borderRadius: 10, padding: '14px 18px', marginBottom: 20, display: 'flex', gap: 12 }}>
            <span style={{ fontSize: 20 }}>💡</span>
            <div>
              <div style={{ fontSize: 13, fontWeight: 700, color: 'var(--primary)', marginBottom: 4 }}>AI Reasoning</div>
              <div style={{ fontSize: 13, color: 'var(--text-primary)' }}>{suggestion.reasoning}</div>
            </div>
          </div>

          {/* Name */}
          <div style={cardStyle}>
            <div style={sectionTitle}>Configurator Name</div>
            <label style={labelStyle}>Name</label>
            <input
              type="text"
              value={editedName}
              onChange={e => setEditedName(e.target.value)}
              style={inputStyle}
            />
          </div>

          {/* Category */}
          <div style={cardStyle}>
            <div style={sectionTitle}>Category</div>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16 }}>
              <div>
                <label style={labelStyle}>Category ID</label>
                <input
                  type="text"
                  value={editedCategoryId}
                  onChange={e => setEditedCategoryId(e.target.value)}
                  style={inputStyle}
                />
              </div>
              <div>
                <label style={labelStyle}>Category Path</label>
                <input
                  type="text"
                  value={editedCategoryPath}
                  onChange={e => setEditedCategoryPath(e.target.value)}
                  style={inputStyle}
                />
              </div>
            </div>
          </div>

          {/* Attribute defaults */}
          <div style={cardStyle}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 14, paddingBottom: 10, borderBottom: '1px solid var(--border)' }}>
              <span style={{ fontSize: 15, fontWeight: 700, color: 'var(--text-primary)' }}>Attribute Defaults ({editedAttrs.length})</span>
              <button
                onClick={addAttr}
                style={{ padding: '6px 14px', background: 'var(--bg-primary)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', cursor: 'pointer', fontSize: 13, fontWeight: 600 }}
              >
                + Add
              </button>
            </div>
            {editedAttrs.length === 0 ? (
              <p style={{ color: 'var(--text-muted)', fontSize: 13 }}>No attribute defaults suggested.</p>
            ) : (
              <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                {editedAttrs.map((attr, idx) => (
                  <div key={idx} style={{ display: 'grid', gridTemplateColumns: '1fr 1fr auto', gap: 10, alignItems: 'flex-end' }}>
                    <div>
                      {idx === 0 && <label style={labelStyle}>Attribute Name</label>}
                      <input
                        type="text"
                        value={attr.attribute_name}
                        onChange={e => updateAttr(idx, 'attribute_name', e.target.value)}
                        placeholder="e.g. brand"
                        style={inputStyle}
                      />
                    </div>
                    <div>
                      {idx === 0 && <label style={labelStyle}>Default Value</label>}
                      <input
                        type="text"
                        value={attr.default_value || ''}
                        onChange={e => updateAttr(idx, 'default_value', e.target.value)}
                        placeholder="e.g. Sony"
                        style={inputStyle}
                      />
                    </div>
                    <button
                      onClick={() => removeAttr(idx)}
                      style={{ padding: '10px 12px', background: 'none', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--danger)', cursor: 'pointer', fontSize: 16, lineHeight: 1 }}
                    >
                      ×
                    </button>
                  </div>
                ))}
              </div>
            )}
          </div>

          {/* Shipping defaults */}
          <div style={cardStyle}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 14, paddingBottom: 10, borderBottom: '1px solid var(--border)' }}>
              <span style={{ fontSize: 15, fontWeight: 700, color: 'var(--text-primary)' }}>Shipping Defaults ({editedShipping.length})</span>
              <button
                onClick={() => setEditedShipping(prev => [...prev, { key: '', value: '' }])}
                style={{ padding: '6px 14px', background: 'var(--bg-primary)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', cursor: 'pointer', fontSize: 13, fontWeight: 600 }}
              >
                + Add
              </button>
            </div>
            {editedShipping.length === 0 ? (
              <p style={{ color: 'var(--text-muted)', fontSize: 13 }}>No shipping defaults suggested.</p>
            ) : (
              <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                {editedShipping.map((row, idx) => (
                  <div key={idx} style={{ display: 'grid', gridTemplateColumns: '1fr 1fr auto', gap: 10, alignItems: 'flex-end' }}>
                    <div>
                      {idx === 0 && <label style={labelStyle}>Key</label>}
                      <input type="text" value={row.key} onChange={e => updateShipping(idx, 'key', e.target.value)} placeholder="e.g. dispatch_time" style={inputStyle} />
                    </div>
                    <div>
                      {idx === 0 && <label style={labelStyle}>Value</label>}
                      <input type="text" value={row.value} onChange={e => updateShipping(idx, 'value', e.target.value)} placeholder="e.g. 1-2 days" style={inputStyle} />
                    </div>
                    <button onClick={() => removeShipping(idx)} style={{ padding: '10px 12px', background: 'none', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--danger)', cursor: 'pointer', fontSize: 16, lineHeight: 1 }}>×</button>
                  </div>
                ))}
              </div>
            )}
          </div>

          {applyError && (
            <div style={{ background: 'var(--danger-glow)', border: '1px solid var(--danger)', borderRadius: 8, padding: '12px 16px', marginBottom: 16, color: 'var(--danger)', fontSize: 13 }}>
              {applyError}
            </div>
          )}

          <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12 }}>
            <button
              onClick={() => setStep('describe')}
              style={{ padding: '10px 20px', background: 'var(--bg-tertiary)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-primary)', cursor: 'pointer', fontSize: 14, fontWeight: 600 }}
            >
              ← Back
            </button>
            <button
              onClick={handleApply}
              style={{ padding: '10px 28px', background: 'var(--primary)', border: 'none', borderRadius: 8, color: '#fff', cursor: 'pointer', fontSize: 14, fontWeight: 700 }}
            >
              ✅ Create Configurator
            </button>
          </div>
        </>
      )}

      {/* ── STEP 3: Applying ────────────────────────────────────────────── */}
      {step === 'applying' && (
        <div style={{ ...cardStyle, textAlign: 'center', padding: 48 }}>
          <div style={{ fontSize: 48, marginBottom: 16 }}>⚙️</div>
          <h2 style={{ fontSize: 20, fontWeight: 700, color: 'var(--text-primary)', marginBottom: 8 }}>Creating Configurator…</h2>
          <p style={{ fontSize: 14, color: 'var(--text-muted)' }}>Saving your new configurator to Firestore and redirecting you to the detail page.</p>
        </div>
      )}
    </div>
  );
}

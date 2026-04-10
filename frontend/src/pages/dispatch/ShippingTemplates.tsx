// platform/frontend/src/pages/dispatch/ShippingTemplates.tsx
import { useState, useEffect, useCallback } from 'react';
import { getActiveTenantId } from '../../contexts/TenantContext';

const API_BASE = import.meta.env.VITE_API_URL ?? 'http://localhost:8080/api/v1';

function api(path: string, init?: RequestInit) {
  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      'X-Tenant-Id': getActiveTenantId(),
      ...init?.headers,
    },
  });
}

// ─── Types ────────────────────────────────────────────────────────────────────

interface ShippingTemplate {
  id: string;
  name: string;
  layout: 'a4_single' | 'a4_dual' | 'a4_packing_slip' | 'thermal_6x4' | 'custom';
  include_outbound_label: boolean;
  include_return_label: boolean;
  include_packing_slip: boolean;
  include_logo: boolean;
  include_qr_code: boolean;
  custom_html?: string;
  created_at?: string;
  updated_at?: string;
}

const LAYOUT_OPTIONS: { value: ShippingTemplate['layout']; label: string; icon: string; description: string }[] = [
  { value: 'a4_single',       label: 'A4 Single',        icon: '📄', description: 'Outbound label centred on A4. Standard printer-friendly format.' },
  { value: 'a4_dual',         label: 'A4 Dual',           icon: '📋', description: 'Top half outbound, bottom half return label. Fold and tear.' },
  { value: 'a4_packing_slip', label: 'A4 + Packing Slip', icon: '🧾', description: 'Small label top-right, packing slip with order lines filling the page.' },
  { value: 'thermal_6x4',     label: 'Thermal 6×4″',     icon: '🏷️', description: '152×101 mm thermal label only. Optimised for Zebra printers.' },
  { value: 'custom',          label: 'Custom HTML',        icon: '⚙️', description: 'Full HTML control. Advanced users only.' },
];

function emptyTemplate(): Partial<ShippingTemplate> {
  return {
    name: '',
    layout: 'a4_single',
    include_outbound_label: true,
    include_return_label: false,
    include_packing_slip: false,
    include_logo: true,
    include_qr_code: false,
    custom_html: '',
  };
}

// ─── Main Component ───────────────────────────────────────────────────────────

export default function ShippingTemplates() {
  const [templates, setTemplates] = useState<ShippingTemplate[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [success, setSuccess] = useState('');
  const [showModal, setShowModal] = useState(false);
  const [editing, setEditing] = useState<ShippingTemplate | null>(null);
  const [form, setForm] = useState<Partial<ShippingTemplate>>(emptyTemplate());

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const res = await api('/dispatch/shipping-templates');
      if (res.ok) {
        const data = await res.json();
        setTemplates(data.templates || []);
      }
    } catch { /* no templates yet */ }
    finally { setLoading(false); }
  }, []);

  useEffect(() => { load(); }, [load]);

  const openCreate = () => {
    setEditing(null);
    setForm(emptyTemplate());
    setError('');
    setShowModal(true);
  };

  const openEdit = (t: ShippingTemplate) => {
    setEditing(t);
    setForm({ ...t });
    setError('');
    setShowModal(true);
  };

  const openDuplicate = async (t: ShippingTemplate) => {
    const dup = { ...t, id: undefined, name: `${t.name} (copy)` };
    try {
      const res = await api('/dispatch/shipping-templates', { method: 'POST', body: JSON.stringify(dup) });
      if (!res.ok) throw new Error('Duplicate failed');
      await load();
      setSuccess('Template duplicated');
      setTimeout(() => setSuccess(''), 3000);
    } catch (e: any) { setError(e.message); }
  };

  const saveTemplate = async () => {
    if (!form.name?.trim()) { setError('Template name is required'); return; }
    if (!form.layout) { setError('Please select a layout'); return; }
    setSaving(true);
    setError('');
    try {
      const id = editing?.id;
      const res = id
        ? await api(`/dispatch/shipping-templates/${id}`, { method: 'PUT', body: JSON.stringify(form) })
        : await api('/dispatch/shipping-templates', { method: 'POST', body: JSON.stringify(form) });
      if (!res.ok) {
        const e = await res.json().catch(() => ({}));
        throw new Error(e.error || `HTTP ${res.status}`);
      }
      setSuccess(id ? 'Template updated' : 'Template created');
      setShowModal(false);
      await load();
      setTimeout(() => setSuccess(''), 3000);
    } catch (e: any) {
      setError(e.message || 'Save failed');
    } finally {
      setSaving(false);
    }
  };

  const deleteTemplate = async (t: ShippingTemplate) => {
    if (!window.confirm(`Delete template "${t.name}"?`)) return;
    try {
      const res = await api(`/dispatch/shipping-templates/${t.id}`, { method: 'DELETE' });
      if (!res.ok) throw new Error('Delete failed');
      await load();
    } catch (e: any) { setError(e.message); }
  };

  const layoutInfo = (layout: string) => LAYOUT_OPTIONS.find(l => l.value === layout);

  return (
    <div style={{ padding: '28px 32px', maxWidth: 1100, margin: '0 auto' }}>

      {/* Header */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 24 }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 24, fontWeight: 700, color: 'var(--text-primary)' }}>🏷️ Shipping Templates</h1>
          <p style={{ margin: '4px 0 0', fontSize: 14, color: 'var(--text-muted)' }}>
            Define reusable label layouts for dispatch — A4, thermal, packing slips, and custom HTML.
          </p>
        </div>
        <button onClick={openCreate} style={btnPrimary}>+ New Template</button>
      </div>

      {success && <div style={alertSuccess}>{success}</div>}
      {error && !showModal && <div style={alertError}>{error}</div>}

      {/* Template list */}
      {loading ? (
        <div style={{ textAlign: 'center', padding: '60px 0', color: 'var(--text-muted)', fontSize: 14 }}>Loading templates…</div>
      ) : templates.length === 0 ? (
        <div style={{ textAlign: 'center', padding: '60px 0', color: 'var(--text-muted)', fontSize: 14 }}>
          <div style={{ fontSize: 40, marginBottom: 12 }}>🏷️</div>
          <div>No shipping templates yet. <strong>Create one</strong> to customise your label layouts.</div>
          <button onClick={openCreate} style={{ ...btnPrimary, marginTop: 16 }}>Create your first template</button>
        </div>
      ) : (
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(300px, 1fr))', gap: 14 }}>
          {templates.map(t => {
            const info = layoutInfo(t.layout);
            return (
              <div key={t.id} style={{
                background: 'var(--bg-secondary)',
                border: '1px solid var(--border)',
                borderRadius: 10,
                padding: '16px 18px',
                display: 'flex',
                flexDirection: 'column',
                gap: 10,
              }}>
                {/* Card header */}
                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                    <span style={{ fontSize: 20 }}>{info?.icon}</span>
                    <span style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.04em' }}>
                      {info?.label}
                    </span>
                  </div>
                  <div style={{ display: 'flex', gap: 4 }}>
                    <button onClick={() => openEdit(t)} style={btnIconSm} title="Edit">✏️</button>
                    <button onClick={() => openDuplicate(t)} style={btnIconSm} title="Duplicate">⧉</button>
                    <button onClick={() => deleteTemplate(t)} style={{ ...btnIconSm, color: '#ef4444' }} title="Delete">🗑</button>
                  </div>
                </div>

                {/* Name */}
                <div style={{ fontSize: 14, fontWeight: 600, color: 'var(--text-primary)' }}>{t.name}</div>

                {/* Feature badges */}
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: 5 }}>
                  <span style={badge}>Outbound label</span>
                  {t.include_return_label  && <span style={{ ...badge, ...badgeGreen  }}>Return label</span>}
                  {t.include_packing_slip  && <span style={{ ...badge, ...badgeBlue   }}>Packing slip</span>}
                  {t.include_logo          && <span style={{ ...badge, ...badgePurple }}>Logo</span>}
                  {t.include_qr_code       && <span style={{ ...badge, ...badgeAmber  }}>QR code</span>}
                </div>
              </div>
            );
          })}
        </div>
      )}

      {/* ── Modal ── */}
      {showModal && (
        <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.65)', zIndex: 1000, display: 'flex', alignItems: 'center', justifyContent: 'center', padding: 24 }}>
          <div style={{ background: 'var(--bg-primary)', border: '1px solid var(--border)', borderRadius: 14, padding: 28, width: '100%', maxWidth: 620, maxHeight: '90vh', overflowY: 'auto' }}>

            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 20 }}>
              <h2 style={{ margin: 0, fontSize: 18, fontWeight: 700, color: 'var(--text-primary)' }}>
                {editing ? 'Edit Template' : 'New Shipping Template'}
              </h2>
              <button onClick={() => { setShowModal(false); setError(''); }} style={btnClose}>×</button>
            </div>

            {error && <div style={{ ...alertError, marginBottom: 14 }}>{error}</div>}

            {/* Name */}
            <label style={labelStyle}>Template Name</label>
            <input
              value={form.name || ''}
              onChange={e => setForm(f => ({ ...f, name: e.target.value }))}
              placeholder="e.g. Standard A4 Label"
              style={{ ...inputStyle, width: '100%', marginBottom: 16, boxSizing: 'border-box' }}
            />

            {/* Layout selector */}
            <label style={labelStyle}>Layout</label>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8, marginBottom: 16 }}>
              {LAYOUT_OPTIONS.map(opt => (
                <button
                  key={opt.value}
                  onClick={() => setForm(f => ({ ...f, layout: opt.value }))}
                  style={{
                    textAlign: 'left',
                    padding: '10px 12px',
                    borderRadius: 8,
                    border: `2px solid ${form.layout === opt.value ? 'var(--primary)' : 'var(--border)'}`,
                    background: form.layout === opt.value ? 'rgba(99,102,241,0.08)' : 'var(--bg-elevated)',
                    cursor: 'pointer',
                    transition: 'border-color 0.15s',
                  }}
                >
                  <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 4 }}>
                    <span>{opt.icon}</span>
                    <span style={{ fontSize: 13, fontWeight: 600, color: 'var(--text-primary)' }}>{opt.label}</span>
                  </div>
                  <div style={{ fontSize: 11, color: 'var(--text-muted)', lineHeight: 1.4 }}>{opt.description}</div>
                </button>
              ))}
            </div>

            {/* Options */}
            <label style={labelStyle}>Options</label>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 8, marginBottom: 16 }}>
              {[
                { key: 'include_return_label',  label: 'Include return label',              disabled: !['a4_dual', 'a4_single'].includes(form.layout || '') },
                { key: 'include_packing_slip',  label: 'Include packing slip',              disabled: form.layout !== 'a4_packing_slip' },
                { key: 'include_logo',          label: 'Include merchant logo',             disabled: false },
                { key: 'include_qr_code',       label: 'Include QR code (tracking page)',   disabled: false },
              ].map(opt => (
                <label key={opt.key} style={{
                  display: 'flex', alignItems: 'center', gap: 8, cursor: opt.disabled ? 'default' : 'pointer',
                  opacity: opt.disabled ? 0.4 : 1, fontSize: 13, color: 'var(--text-secondary)',
                }}>
                  <input
                    type="checkbox"
                    disabled={opt.disabled}
                    checked={(form as any)[opt.key] || false}
                    onChange={e => setForm(f => ({ ...f, [opt.key]: e.target.checked }))}
                  />
                  {opt.label}
                </label>
              ))}
            </div>

            {/* Custom HTML */}
            {form.layout === 'custom' && (
              <>
                <label style={labelStyle}>Custom HTML</label>
                <textarea
                  value={form.custom_html || ''}
                  onChange={e => setForm(f => ({ ...f, custom_html: e.target.value }))}
                  placeholder="<!DOCTYPE html><html>…"
                  spellCheck={false}
                  rows={8}
                  style={{ ...inputStyle, width: '100%', boxSizing: 'border-box', fontFamily: 'monospace', fontSize: 12, resize: 'vertical', marginBottom: 16 }}
                />
                <p style={{ fontSize: 11, color: 'var(--text-muted)', marginBottom: 16, marginTop: -12 }}>
                  Script tags and event handlers are stripped for security.
                </p>
              </>
            )}

            <div style={{ display: 'flex', gap: 10, justifyContent: 'flex-end' }}>
              <button onClick={() => { setShowModal(false); setError(''); }} style={btnGhost}>Cancel</button>
              <button onClick={saveTemplate} disabled={saving} style={{ ...btnPrimary, opacity: saving ? 0.6 : 1 }}>
                {saving ? 'Saving…' : editing ? 'Update Template' : 'Create Template'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

// ─── Styles ───────────────────────────────────────────────────────────────────

const inputStyle: React.CSSProperties = {
  padding: '8px 12px',
  background: 'var(--bg-elevated)',
  border: '1px solid var(--border)',
  borderRadius: 6,
  color: 'var(--text-primary)',
  fontSize: 13,
  outline: 'none',
};

const labelStyle: React.CSSProperties = {
  display: 'block',
  fontSize: 11,
  fontWeight: 700,
  color: 'var(--text-muted)',
  marginBottom: 6,
  textTransform: 'uppercase',
  letterSpacing: '0.05em',
};

const btnPrimary: React.CSSProperties = {
  padding: '8px 18px',
  background: 'var(--primary)',
  color: 'white',
  border: 'none',
  borderRadius: 6,
  cursor: 'pointer',
  fontSize: 13,
  fontWeight: 600,
};

const btnGhost: React.CSSProperties = {
  padding: '8px 16px',
  background: 'transparent',
  color: 'var(--text-secondary)',
  border: '1px solid var(--border)',
  borderRadius: 6,
  cursor: 'pointer',
  fontSize: 13,
};

const btnIconSm: React.CSSProperties = {
  padding: '4px 6px',
  background: 'transparent',
  border: '1px solid var(--border)',
  borderRadius: 4,
  cursor: 'pointer',
  fontSize: 13,
  color: 'var(--text-muted)',
};

const btnClose: React.CSSProperties = {
  background: 'transparent',
  border: 'none',
  fontSize: 20,
  color: 'var(--text-muted)',
  cursor: 'pointer',
  padding: '0 4px',
  lineHeight: 1,
};

const alertSuccess: React.CSSProperties = {
  marginBottom: 14,
  padding: '10px 14px',
  background: 'rgba(34,197,94,0.1)',
  border: '1px solid rgba(34,197,94,0.3)',
  borderRadius: 6,
  color: '#22c55e',
  fontSize: 13,
};

const alertError: React.CSSProperties = {
  padding: '10px 14px',
  background: 'rgba(239,68,68,0.1)',
  border: '1px solid rgba(239,68,68,0.3)',
  borderRadius: 6,
  color: '#ef4444',
  fontSize: 13,
};

const badge: React.CSSProperties = {
  fontSize: 11,
  padding: '2px 7px',
  borderRadius: 4,
  background: 'var(--bg-elevated)',
  border: '1px solid var(--border)',
  color: 'var(--text-muted)',
  fontWeight: 500,
};

const badgeGreen:  React.CSSProperties = { background: 'rgba(34,197,94,0.1)',  border: '1px solid rgba(34,197,94,0.3)',  color: '#22c55e' };
const badgeBlue:   React.CSSProperties = { background: 'rgba(59,130,246,0.1)', border: '1px solid rgba(59,130,246,0.3)', color: '#60a5fa' };
const badgePurple: React.CSSProperties = { background: 'rgba(139,92,246,0.1)', border: '1px solid rgba(139,92,246,0.3)', color: '#a78bfa' };
const badgeAmber:  React.CSSProperties = { background: 'rgba(245,158,11,0.1)', border: '1px solid rgba(245,158,11,0.3)', color: '#fbbf24' };

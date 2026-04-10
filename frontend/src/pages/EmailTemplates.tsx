import { useState, useEffect } from 'react';
import { getActiveTenantId } from '../contexts/TenantContext';

const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';
function api(path: string, init?: RequestInit) {
  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId(), ...init?.headers },
  });
}

interface EmailTemplate {
  id: string;
  tenant_id: string;
  name: string;
  type: string;
  subject: string;
  body: string;
  variables: string[];
  active: boolean;
  created_at: string;
  updated_at: string;
}

const TEMPLATE_TYPES = [
  { value: 'order_confirmation',    label: 'Order Confirmation' },
  { value: 'despatch_notification', label: 'Despatch Notification' },
  { value: 'rma_update',            label: 'RMA Update' },
  { value: 'low_stock_alert',       label: 'Low Stock Alert' },
];

const DEFAULT_VARIABLES: Record<string, string[]> = {
  order_confirmation:    ['{{order_id}}', '{{customer_name}}', '{{order_date}}', '{{order_total}}', '{{items}}'],
  despatch_notification: ['{{order_id}}', '{{tracking_number}}', '{{carrier}}', '{{customer_name}}', '{{estimated_delivery}}'],
  rma_update:            ['{{order_id}}', '{{rma_id}}', '{{rma_status}}', '{{customer_name}}', '{{refund_amount}}'],
  low_stock_alert:       ['{{sku}}', '{{product_title}}', '{{current_stock}}', '{{reorder_point}}'],
};

type ModalMode = 'create' | 'edit';

interface ModalState {
  mode: ModalMode;
  template?: EmailTemplate;
  name: string;
  type: string;
  subject: string;
  body: string;
  active: boolean;
}

export default function EmailTemplates() {
  const [templates, setTemplates] = useState<EmailTemplate[]>([]);
  const [loading, setLoading] = useState(true);
  const [modal, setModal] = useState<ModalState | null>(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => { load(); }, []);

  async function load() {
    setLoading(true);
    try {
      const res = await api('/email-templates');
      if (res.ok) {
        const data = await res.json();
        setTemplates(data.templates || []);
      }
    } finally {
      setLoading(false);
    }
  }

  function openCreate() {
    setModal({ mode: 'create', name: '', type: 'order_confirmation', subject: '', body: '', active: true });
    setError(null);
  }

  function openEdit(t: EmailTemplate) {
    setModal({ mode: 'edit', template: t, name: t.name, type: t.type, subject: t.subject, body: t.body, active: t.active });
    setError(null);
  }

  async function saveTemplate() {
    if (!modal) return;
    setSaving(true);
    setError(null);
    try {
      const body = { name: modal.name, type: modal.type, subject: modal.subject, body: modal.body, active: modal.active };
      const res = modal.mode === 'create'
        ? await api('/email-templates', { method: 'POST', body: JSON.stringify(body) })
        : await api(`/email-templates/${modal.template!.id}`, { method: 'PUT', body: JSON.stringify(body) });

      if (!res.ok) {
        const data = await res.json();
        setError(data.error || 'Failed to save template');
        return;
      }
      setModal(null);
      await load();
    } finally {
      setSaving(false);
    }
  }

  async function deleteTemplate(id: string) {
    if (!confirm('Delete this email template? This cannot be undone.')) return;
    await api(`/email-templates/${id}`, { method: 'DELETE' });
    await load();
  }

  const typeLabel = (type: string) => TEMPLATE_TYPES.find(t => t.value === type)?.label ?? type;
  const variables = modal ? DEFAULT_VARIABLES[modal.type] || [] : [];

  return (
    <div style={{ padding: '32px 40px', maxWidth: 1100, margin: '0 auto' }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 28 }}>
        <div>
          <h1 style={{ margin: 0, color: 'var(--text-primary)', fontSize: 22, fontWeight: 700 }}>✉️ Email Templates</h1>
          <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 14 }}>Manage transactional email templates with variable placeholders</p>
        </div>
        <button
          onClick={openCreate}
          style={{ padding: '10px 20px', background: 'var(--primary)', border: 'none', borderRadius: 8, color: '#fff', fontWeight: 600, fontSize: 14, cursor: 'pointer' }}
        >
          + New Template
        </button>
      </div>

      {/* Template cards */}
      {loading ? (
        <div style={{ textAlign: 'center', padding: 60, color: 'var(--text-muted)' }}>Loading…</div>
      ) : templates.length === 0 ? (
        <div style={{ textAlign: 'center', padding: 80, color: 'var(--text-muted)' }}>
          <div style={{ fontSize: 40, marginBottom: 14 }}>✉️</div>
          <div style={{ fontWeight: 600, fontSize: 16, marginBottom: 6 }}>No email templates yet</div>
          <div style={{ fontSize: 14 }}>Create your first template to start customising transactional emails.</div>
        </div>
      ) : (
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(320px, 1fr))', gap: 16 }}>
          {templates.map(t => (
            <div key={t.id} style={{ background: 'var(--bg-secondary)', borderRadius: 12, border: '1px solid var(--border)', padding: 20 }}>
              <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', marginBottom: 10 }}>
                <div>
                  <div style={{ fontWeight: 600, color: 'var(--text-primary)', fontSize: 15, marginBottom: 4 }}>{t.name}</div>
                  <span style={{
                    background: 'rgba(99,102,241,0.12)', color: '#818cf8',
                    fontSize: 11, fontWeight: 600, padding: '2px 8px', borderRadius: 4,
                  }}>{typeLabel(t.type)}</span>
                </div>
                <span style={{
                  background: t.active ? 'rgba(34,197,94,0.12)' : 'rgba(107,114,128,0.12)',
                  color: t.active ? '#4ade80' : '#9ca3af',
                  fontSize: 11, fontWeight: 600, padding: '2px 8px', borderRadius: 4,
                }}>
                  {t.active ? 'Active' : 'Inactive'}
                </span>
              </div>
              <div style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 6 }}>
                <strong style={{ color: 'var(--text-secondary)' }}>Subject:</strong> {t.subject || '—'}
              </div>
              <div style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 14, lineHeight: 1.5 }}>
                {t.body ? t.body.replace(/<[^>]*>/g, '').slice(0, 100) + (t.body.length > 100 ? '…' : '') : '—'}
              </div>
              <div style={{ display: 'flex', gap: 8 }}>
                <button
                  onClick={() => openEdit(t)}
                  style={{ flex: 1, padding: '7px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-secondary)', cursor: 'pointer', fontSize: 13 }}
                >
                  Edit
                </button>
                <button
                  onClick={() => deleteTemplate(t.id)}
                  style={{ padding: '7px 12px', background: 'rgba(239,68,68,0.08)', border: '1px solid rgba(239,68,68,0.2)', borderRadius: 6, color: '#f87171', cursor: 'pointer', fontSize: 13 }}
                >
                  Delete
                </button>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Create/Edit Modal */}
      {modal && (
        <>
          <div onClick={() => setModal(null)} style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.5)', zIndex: 999 }} />
          <div style={{
            position: 'fixed', top: '50%', left: '50%', transform: 'translate(-50%,-50%)',
            background: 'var(--bg-secondary)', borderRadius: 16, border: '1px solid var(--border)',
            width: 680, maxHeight: '90vh', overflowY: 'auto', padding: 28, zIndex: 1000,
            boxShadow: '0 24px 64px rgba(0,0,0,0.5)',
          }}>
            <h2 style={{ margin: '0 0 20px', color: 'var(--text-primary)', fontSize: 18, fontWeight: 700 }}>
              {modal.mode === 'create' ? 'Create Template' : 'Edit Template'}
            </h2>

            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16, marginBottom: 16 }}>
              <div>
                <label style={{ display: 'block', fontSize: 13, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 6 }}>Template Name</label>
                <input
                  type="text"
                  value={modal.name}
                  onChange={e => setModal({ ...modal, name: e.target.value })}
                  placeholder="e.g. Order Confirmation"
                  style={{ width: '100%', padding: '9px 12px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-primary)', fontSize: 14, boxSizing: 'border-box' }}
                />
              </div>
              <div>
                <label style={{ display: 'block', fontSize: 13, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 6 }}>Type</label>
                <select
                  value={modal.type}
                  onChange={e => setModal({ ...modal, type: e.target.value })}
                  style={{ width: '100%', padding: '9px 12px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-primary)', fontSize: 14, boxSizing: 'border-box' }}
                >
                  {TEMPLATE_TYPES.map(t => <option key={t.value} value={t.value}>{t.label}</option>)}
                </select>
              </div>
            </div>

            {/* Available variables */}
            {variables.length > 0 && (
              <div style={{ marginBottom: 16, padding: 12, background: 'var(--bg-elevated)', borderRadius: 8, border: '1px solid var(--border)' }}>
                <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-muted)', marginBottom: 6 }}>AVAILABLE VARIABLES</div>
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
                  {variables.map(v => (
                    <span key={v} style={{ background: 'rgba(99,102,241,0.12)', color: '#818cf8', padding: '2px 8px', borderRadius: 4, fontSize: 12, fontFamily: 'monospace' }}>{v}</span>
                  ))}
                </div>
              </div>
            )}

            <div style={{ marginBottom: 16 }}>
              <label style={{ display: 'block', fontSize: 13, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 6 }}>Subject Line</label>
              <input
                type="text"
                value={modal.subject}
                onChange={e => setModal({ ...modal, subject: e.target.value })}
                placeholder="e.g. Your order {{order_id}} has been confirmed"
                style={{ width: '100%', padding: '9px 12px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-primary)', fontSize: 14, boxSizing: 'border-box' }}
              />
            </div>

            <div style={{ marginBottom: 16 }}>
              <label style={{ display: 'block', fontSize: 13, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 6 }}>
                Body <span style={{ fontWeight: 400, color: 'var(--text-muted)' }}>(HTML supported)</span>
              </label>
              <textarea
                value={modal.body}
                onChange={e => setModal({ ...modal, body: e.target.value })}
                rows={10}
                placeholder="<p>Hello {{customer_name}},</p><p>Your order {{order_id}} has been confirmed…</p>"
                style={{
                  width: '100%', padding: '9px 12px', background: 'var(--bg-elevated)', border: '1px solid var(--border)',
                  borderRadius: 8, color: 'var(--text-primary)', fontSize: 13, boxSizing: 'border-box',
                  fontFamily: 'monospace', resize: 'vertical', lineHeight: 1.6,
                }}
              />
            </div>

            <div style={{ marginBottom: 20, display: 'flex', alignItems: 'center', gap: 10 }}>
              <input
                type="checkbox"
                id="active-toggle"
                checked={modal.active}
                onChange={e => setModal({ ...modal, active: e.target.checked })}
                style={{ width: 16, height: 16, cursor: 'pointer' }}
              />
              <label htmlFor="active-toggle" style={{ fontSize: 14, color: 'var(--text-secondary)', cursor: 'pointer' }}>
                Template is active
              </label>
            </div>

            {error && (
              <div style={{ background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 8, padding: '10px 14px', color: '#f87171', fontSize: 13, marginBottom: 16 }}>
                {error}
              </div>
            )}

            <div style={{ display: 'flex', gap: 10, justifyContent: 'flex-end' }}>
              <button onClick={() => setModal(null)} style={{ padding: '9px 18px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-secondary)', cursor: 'pointer', fontSize: 14 }}>
                Cancel
              </button>
              <button
                onClick={saveTemplate}
                disabled={saving}
                style={{ padding: '9px 20px', background: saving ? 'rgba(99,102,241,0.4)' : 'var(--primary)', border: 'none', borderRadius: 8, color: '#fff', fontWeight: 600, fontSize: 14, cursor: saving ? 'not-allowed' : 'pointer' }}
              >
                {saving ? 'Saving…' : modal.mode === 'create' ? 'Create Template' : 'Save Changes'}
              </button>
            </div>
          </div>
        </>
      )}
    </div>
  );
}

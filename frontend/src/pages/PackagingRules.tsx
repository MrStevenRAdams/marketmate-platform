import { useState, useEffect, useCallback } from 'react';
import { getActiveTenantId } from '../contexts/TenantContext';

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

// ─── Types ─────────────────────────────────────────────────────────────────────

interface PackagingRule {
  rule_id?: string;
  name: string;
  priority: number;
  condition_type: string;
  weight_min_kg: number;
  weight_max_kg: number;
  max_length_cm?: number;
  max_width_cm?: number;
  max_height_cm?: number;
  package_format: string;
  package_name: string;
  carrier_id?: string;
  service_code?: string;
  is_active: boolean;
}

const PACKAGE_FORMATS = [
  { value: 'letter', label: '✉️ Letter', description: 'Up to 5mm thick, ≤100g' },
  { value: 'large_letter', label: '📬 Large Letter', description: 'Up to 25mm thick, ≤750g' },
  { value: 'small_parcel', label: '📦 Small Parcel', description: 'Up to 45cm × 35cm × 16cm, ≤2kg' },
  { value: 'medium_parcel', label: '📦 Medium Parcel', description: 'Up to 61cm × 46cm × 46cm, ≤20kg' },
  { value: 'large_parcel', label: '📦 Large Parcel', description: 'Up to 120cm any side, ≤30kg' },
  { value: 'pallet', label: '🪵 Pallet', description: 'Oversized / heavy freight' },
];

const CARRIER_OPTIONS = [
  { id: 'royal-mail', name: 'Royal Mail' },
  { id: 'dpd', name: 'DPD' },
  { id: 'evri', name: 'Evri' },
  { id: 'fedex', name: 'FedEx' },
];

function emptyRule(): PackagingRule {
  return {
    name: '',
    priority: 10,
    condition_type: 'weight_range',
    weight_min_kg: 0,
    weight_max_kg: 0.1,
    package_format: 'letter',
    package_name: 'Standard Letter',
    is_active: true,
  };
}

// ─── Main Component ─────────────────────────────────────────────────────────────

export default function PackagingRules() {
  const [rules, setRules] = useState<PackagingRule[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [success, setSuccess] = useState('');
  const [showModal, setShowModal] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [form, setForm] = useState<PackagingRule>(emptyRule());

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const res = await api('/dispatch/packaging-rules');
      if (res.ok) {
        const data = await res.json();
        setRules(data.rules || []);
      }
    } catch {
      // no rules yet
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { load(); }, [load]);

  const openCreate = () => {
    setEditingId(null);
    setForm(emptyRule());
    setError('');
    setShowModal(true);
  };

  const openEdit = (rule: PackagingRule) => {
    setEditingId(rule.rule_id || null);
    setForm({ ...rule });
    setError('');
    setShowModal(true);
  };

  const save = async () => {
    if (!form.name.trim()) { setError('Rule name is required'); return; }
    setSaving(true);
    setError('');
    try {
      const res = editingId
        ? await api(`/dispatch/packaging-rules/${editingId}`, { method: 'PUT', body: JSON.stringify(form) })
        : await api('/dispatch/packaging-rules', { method: 'POST', body: JSON.stringify(form) });
      if (!res.ok) {
        const e = await res.json().catch(() => ({}));
        throw new Error(e.error || `HTTP ${res.status}`);
      }
      setSuccess(editingId ? 'Rule updated' : 'Rule created');
      setShowModal(false);
      await load();
      setTimeout(() => setSuccess(''), 3000);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Save failed');
    } finally {
      setSaving(false);
    }
  };

  const deleteRule = async (rule: PackagingRule) => {
    if (!rule.rule_id || !window.confirm(`Delete rule "${rule.name}"?`)) return;
    try {
      await api(`/dispatch/packaging-rules/${rule.rule_id}`, { method: 'DELETE' });
      await load();
    } catch { setError('Delete failed'); }
  };

  const formatLabel = (rule: PackagingRule) => {
    const fmt = PACKAGE_FORMATS.find(f => f.value === rule.package_format);
    return fmt ? fmt.label : rule.package_format;
  };

  const weightRange = (rule: PackagingRule) => {
    if (rule.weight_max_kg > 0) return `${rule.weight_min_kg}–${rule.weight_max_kg} kg`;
    return `≥ ${rule.weight_min_kg} kg`;
  };

  return (
    <div style={{ padding: '28px 32px', maxWidth: 1000, margin: '0 auto' }}>

      {/* Header */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 24 }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 24, fontWeight: 700, color: 'var(--text-primary)' }}>📦 Packaging Rules</h1>
          <p style={{ margin: '4px 0 0', fontSize: 14, color: 'var(--text-muted)' }}>
            Auto-assign a packaging format based on order weight or dimensions. Rules are evaluated in priority order.
          </p>
        </div>
        <button onClick={openCreate} style={btnPrimary}>+ Add Rule</button>
      </div>

      {success && (
        <div style={{ marginBottom: 14, padding: '10px 14px', background: 'rgba(34,197,94,0.1)', border: '1px solid rgba(34,197,94,0.3)', borderRadius: 6, color: '#22c55e', fontSize: 13 }}>
          ✓ {success}
        </div>
      )}
      {error && !showModal && (
        <div style={{ marginBottom: 14, padding: '10px 14px', background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 6, color: '#ef4444', fontSize: 13 }}>
          {error}
        </div>
      )}

      {/* Package format reference */}
      <div style={{ marginBottom: 24, padding: 16, background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 10 }}>
        <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: 10 }}>
          Royal Mail Format Reference
        </div>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 8 }}>
          {PACKAGE_FORMATS.map(fmt => (
            <div key={fmt.value} style={{ fontSize: 12, color: 'var(--text-secondary)' }}>
              <strong style={{ color: 'var(--text-primary)' }}>{fmt.label}</strong>
              <br />
              <span style={{ color: 'var(--text-muted)' }}>{fmt.description}</span>
            </div>
          ))}
        </div>
      </div>

      {/* Rules */}
      {loading ? (
        <div style={{ textAlign: 'center', padding: '50px 0', color: 'var(--text-muted)', fontSize: 14 }}>Loading rules…</div>
      ) : rules.length === 0 ? (
        <div style={{ textAlign: 'center', padding: '50px 0', color: 'var(--text-muted)', fontSize: 14 }}>
          <div style={{ fontSize: 32, marginBottom: 12 }}>📦</div>
          No packaging rules yet.
        </div>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
          {rules.sort((a, b) => a.priority - b.priority).map((rule, i) => (
            <div
              key={rule.rule_id || i}
              style={{
                display: 'grid',
                gridTemplateColumns: '40px 1fr 1fr auto auto auto',
                alignItems: 'center',
                gap: 14,
                padding: '14px 18px',
                background: 'var(--bg-secondary)',
                border: '1px solid var(--border)',
                borderRadius: 10,
                opacity: rule.is_active ? 1 : 0.5,
              }}
            >
              <div style={{ textAlign: 'center', fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6, padding: '4px 0' }}>
                #{rule.priority}
              </div>
              <div>
                <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--text-primary)' }}>{rule.name}</div>
                <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 2 }}>
                  Weight: {weightRange(rule)}
                </div>
              </div>
              <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                <span style={{ fontSize: 13 }}>→</span>
                <span style={{ fontSize: 12, fontWeight: 600, background: 'rgba(99,102,241,0.1)', border: '1px solid rgba(99,102,241,0.3)', color: 'var(--primary)', borderRadius: 4, padding: '3px 10px' }}>
                  {formatLabel(rule)}
                </span>
                <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>{rule.package_name}</span>
              </div>
              <span style={{ fontSize: 11, fontWeight: 600, color: rule.is_active ? '#22c55e' : 'var(--text-muted)' }}>
                {rule.is_active ? '● Active' : '○ Off'}
              </span>
              <button onClick={() => openEdit(rule)} style={btnSmall}>Edit</button>
              <button onClick={() => deleteRule(rule)} style={{ ...btnSmall, color: '#ef4444', borderColor: 'rgba(239,68,68,0.3)' }}>Delete</button>
            </div>
          ))}
        </div>
      )}

      {/* Modal */}
      {showModal && (
        <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.6)', zIndex: 1000, display: 'flex', alignItems: 'center', justifyContent: 'center', padding: 24 }}>
          <div style={{ background: 'var(--bg-primary)', border: '1px solid var(--border)', borderRadius: 14, padding: 28, width: '100%', maxWidth: 560, maxHeight: '90vh', overflowY: 'auto' }}>
            <h2 style={{ margin: '0 0 20px', fontSize: 18, fontWeight: 700, color: 'var(--text-primary)' }}>
              {editingId ? 'Edit Packaging Rule' : 'New Packaging Rule'}
            </h2>

            {error && (
              <div style={{ marginBottom: 12, padding: '8px 12px', background: 'rgba(239,68,68,0.1)', borderRadius: 6, color: '#ef4444', fontSize: 13 }}>
                {error}
              </div>
            )}

            <label style={labelStyle}>Rule Name</label>
            <input value={form.name} onChange={e => setForm(f => ({ ...f, name: e.target.value }))}
              placeholder="e.g. Light items → Large Letter" style={{ ...inputStyle, width: '100%', marginBottom: 14, boxSizing: 'border-box' }} />

            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12, marginBottom: 14 }}>
              <div>
                <label style={labelStyle}>Priority</label>
                <input type="number" value={form.priority} onChange={e => setForm(f => ({ ...f, priority: +e.target.value }))}
                  style={{ ...inputStyle, width: '100%', boxSizing: 'border-box' }} />
              </div>
              <div style={{ display: 'flex', alignItems: 'flex-end', paddingBottom: 2 }}>
                <label style={{ display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer', fontSize: 13, color: 'var(--text-secondary)' }}>
                  <input type="checkbox" checked={form.is_active} onChange={e => setForm(f => ({ ...f, is_active: e.target.checked }))} />
                  Active
                </label>
              </div>
            </div>

            <label style={labelStyle}>Weight Range (kg)</label>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12, marginBottom: 14 }}>
              <div>
                <div style={{ fontSize: 11, color: 'var(--text-muted)', marginBottom: 4 }}>Min (≥)</div>
                <input type="number" step="0.001" value={form.weight_min_kg}
                  onChange={e => setForm(f => ({ ...f, weight_min_kg: +e.target.value }))}
                  style={{ ...inputStyle, width: '100%', boxSizing: 'border-box' }} />
              </div>
              <div>
                <div style={{ fontSize: 11, color: 'var(--text-muted)', marginBottom: 4 }}>Max (&lt;) — 0 = no limit</div>
                <input type="number" step="0.001" value={form.weight_max_kg}
                  onChange={e => setForm(f => ({ ...f, weight_max_kg: +e.target.value }))}
                  style={{ ...inputStyle, width: '100%', boxSizing: 'border-box' }} />
              </div>
            </div>

            <label style={labelStyle}>Package Format</label>
            <select value={form.package_format}
              onChange={e => {
                const fmt = PACKAGE_FORMATS.find(f => f.value === e.target.value);
                setForm(f => ({ ...f, package_format: e.target.value, package_name: fmt?.label.replace(/^.+ /, '') || f.package_name }));
              }}
              style={{ ...inputStyle, width: '100%', marginBottom: 14, boxSizing: 'border-box' }}>
              {PACKAGE_FORMATS.map(fmt => (
                <option key={fmt.value} value={fmt.value}>{fmt.label} — {fmt.description}</option>
              ))}
            </select>

            <label style={labelStyle}>Package Name (display)</label>
            <input value={form.package_name} onChange={e => setForm(f => ({ ...f, package_name: e.target.value }))}
              placeholder="e.g. Large Letter" style={{ ...inputStyle, width: '100%', marginBottom: 14, boxSizing: 'border-box' }} />

            <label style={labelStyle}>Preferred Carrier (optional)</label>
            <select value={form.carrier_id || ''}
              onChange={e => setForm(f => ({ ...f, carrier_id: e.target.value || undefined }))}
              style={{ ...inputStyle, width: '100%', marginBottom: 20, boxSizing: 'border-box' }}>
              <option value="">— No preference —</option>
              {CARRIER_OPTIONS.map(c => <option key={c.id} value={c.id}>{c.name}</option>)}
            </select>

            <div style={{ display: 'flex', gap: 10, justifyContent: 'flex-end' }}>
              <button onClick={() => { setShowModal(false); setError(''); }} style={btnGhost}>Cancel</button>
              <button onClick={save} disabled={saving} style={{ ...btnPrimary, opacity: saving ? 0.6 : 1 }}>
                {saving ? 'Saving…' : editingId ? 'Update' : 'Create'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

// ─── Styles ───────────────────────────────────────────────────────────────────

const inputStyle: React.CSSProperties = { padding: '8px 12px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', fontSize: 13, outline: 'none' };
const labelStyle: React.CSSProperties = { display: 'block', fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', marginBottom: 6, textTransform: 'uppercase', letterSpacing: '0.05em' };
const btnPrimary: React.CSSProperties = { padding: '8px 18px', background: 'var(--primary)', color: 'white', border: 'none', borderRadius: 6, cursor: 'pointer', fontSize: 13, fontWeight: 600 };
const btnGhost: React.CSSProperties = { padding: '8px 16px', background: 'transparent', color: 'var(--text-secondary)', border: '1px solid var(--border)', borderRadius: 6, cursor: 'pointer', fontSize: 13 };
const btnSmall: React.CSSProperties = { padding: '5px 12px', background: 'transparent', color: 'var(--text-secondary)', border: '1px solid var(--border)', borderRadius: 5, cursor: 'pointer', fontSize: 12 };

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

interface ShippingRule {
  definition_id?: string;
  rule_id?: string;
  name: string;
  priority: number;
  is_active: boolean;
  conditions: RuleCondition[];
  actions: RuleAction;
  created_at?: string;
}

interface RuleCondition {
  field: string;    // channel, order_value_gte, order_value_lt, weight_gte_kg, weight_lt_kg, destination_country, sku_prefix
  operator: string; // eq, gte, lt, contains
  value: string;
}

interface RuleAction {
  carrier_id: string;
  service_code: string;
  service_label?: string;
}

interface CarrierService {
  carrier_id: string;
  carrier_name: string;
  service_code: string;
  service_name: string;
}

const CONDITION_FIELDS = [
  { value: 'channel', label: 'Channel' },
  { value: 'order_value_gte', label: 'Order value ≥ (£)' },
  { value: 'order_value_lt', label: 'Order value < (£)' },
  { value: 'weight_gte_kg', label: 'Weight ≥ (kg)' },
  { value: 'weight_lt_kg', label: 'Weight < (kg)' },
  { value: 'destination_country', label: 'Destination country (ISO)' },
  { value: 'sku_prefix', label: 'SKU prefix' },
];

const CHANNEL_OPTIONS = ['amazon', 'ebay', 'shopify', 'etsy', 'temu', 'tiktok', 'woocommerce', 'manual'];

const KNOWN_SERVICES: CarrierService[] = [
  { carrier_id: 'royal-mail', carrier_name: 'Royal Mail', service_code: 'RM48', service_name: '48 (2nd Class)' },
  { carrier_id: 'royal-mail', carrier_name: 'Royal Mail', service_code: 'RM24', service_name: '24 (1st Class)' },
  { carrier_id: 'royal-mail', carrier_name: 'Royal Mail', service_code: 'RM_TRACKED48', service_name: 'Tracked 48' },
  { carrier_id: 'royal-mail', carrier_name: 'Royal Mail', service_code: 'RM_TRACKED24', service_name: 'Tracked 24' },
  { carrier_id: 'dpd', carrier_name: 'DPD', service_code: 'DPD_NEXT_DAY', service_name: 'Next Day' },
  { carrier_id: 'dpd', carrier_name: 'DPD', service_code: 'DPD_TWO_DAY', service_name: '2-Day' },
  { carrier_id: 'evri', carrier_name: 'Evri', service_code: 'EVRI_STANDARD', service_name: 'Standard' },
  { carrier_id: 'evri', carrier_name: 'Evri', service_code: 'EVRI_NEXT_DAY', service_name: 'Next Day' },
  { carrier_id: 'fedex', carrier_name: 'FedEx', service_code: 'FEDEX_GROUND', service_name: 'Ground' },
  { carrier_id: 'fedex', carrier_name: 'FedEx', service_code: 'FEDEX_EXPRESS', service_name: 'Express' },
];

function emptyRule(): ShippingRule {
  return {
    name: '',
    priority: 10,
    is_active: true,
    conditions: [{ field: 'channel', operator: 'eq', value: '' }],
    actions: { carrier_id: 'royal-mail', service_code: 'RM_TRACKED24', service_label: 'Royal Mail Tracked 24' },
  };
}

// ─── Main Component ─────────────────────────────────────────────────────────────

export default function ShippingRules() {
  const [rules, setRules] = useState<ShippingRule[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [success, setSuccess] = useState('');
  const [showModal, setShowModal] = useState(false);
  const [editing, setEditing] = useState<ShippingRule | null>(null);
  const [form, setForm] = useState<ShippingRule>(emptyRule());

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const res = await api('/postage-definitions');
      if (res.ok) {
        const data = await res.json();
        setRules(data.rules || []);
      }
    } catch {
      // OK — no rules yet
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { load(); }, [load]);

  const openCreate = () => {
    setEditing(null);
    setForm(emptyRule());
    setShowModal(true);
  };

  const openEdit = (rule: ShippingRule) => {
    setEditing(rule);
    setForm({ ...rule });
    setShowModal(true);
  };

  const saveRule = async () => {
    if (!form.name.trim()) { setError('Rule name is required'); return; }
    setSaving(true);
    setError('');
    try {
      const id = editing?.definition_id || editing?.rule_id;
      const res = id
        ? await api(`/postage-definitions/${id}`, { method: 'PUT', body: JSON.stringify(form) })
        : await api('/postage-definitions', { method: 'POST', body: JSON.stringify(form) });

      if (!res.ok) {
        const e = await res.json().catch(() => ({}));
        throw new Error(e.error || `HTTP ${res.status}`);
      }
      setSuccess(id ? 'Rule updated' : 'Rule created');
      setShowModal(false);
      await load();
      setTimeout(() => setSuccess(''), 3000);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Save failed');
    } finally {
      setSaving(false);
    }
  };

  const deleteRule = async (rule: ShippingRule) => {
    const id = rule.definition_id || rule.rule_id;
    if (!id || !window.confirm(`Delete rule "${rule.name}"?`)) return;
    try {
      const res = await api(`/postage-definitions/${id}`, { method: 'DELETE' });
      if (!res.ok) throw new Error('Delete failed');
      await load();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Delete failed');
    }
  };

  const toggleActive = async (rule: ShippingRule) => {
    const id = rule.definition_id || rule.rule_id;
    if (!id) return;
    try {
      await api(`/postage-definitions/${id}`, {
        method: 'PUT',
        body: JSON.stringify({ ...rule, is_active: !rule.is_active }),
      });
      await load();
    } catch {}
  };

  const addCondition = () => {
    setForm(f => ({ ...f, conditions: [...f.conditions, { field: 'channel', operator: 'eq', value: '' }] }));
  };

  const removeCondition = (idx: number) => {
    setForm(f => ({ ...f, conditions: f.conditions.filter((_, i) => i !== idx) }));
  };

  const updateCondition = (idx: number, key: keyof RuleCondition, val: string) => {
    setForm(f => {
      const conds = [...f.conditions];
      conds[idx] = { ...conds[idx], [key]: val };
      return { ...f, conditions: conds };
    });
  };

  const conditionLabel = (c: RuleCondition) => {
    const fieldLabel = CONDITION_FIELDS.find(f => f.value === c.field)?.label || c.field;
    return `${fieldLabel} = ${c.value || '…'}`;
  };

  const actionLabel = (rule: ShippingRule) => {
    const svc = KNOWN_SERVICES.find(s => s.carrier_id === rule.actions.carrier_id && s.service_code === rule.actions.service_code);
    if (svc) return `${svc.carrier_name} — ${svc.service_name}`;
    return `${rule.actions.carrier_id} / ${rule.actions.service_code}`;
  };

  return (
    <div style={{ padding: '28px 32px', maxWidth: 1100, margin: '0 auto' }}>

      {/* Header */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 24 }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 24, fontWeight: 700, color: 'var(--text-primary)' }}>⚙️ Shipping Rules</h1>
          <p style={{ margin: '4px 0 0', fontSize: 14, color: 'var(--text-muted)' }}>
            Auto-select a carrier and service when an order matches conditions. Rules are evaluated in priority order.
          </p>
        </div>
        <button onClick={openCreate} style={btnPrimary}>+ Add Rule</button>
      </div>

      {success && (
        <div style={{ marginBottom: 14, padding: '10px 14px', background: 'rgba(34,197,94,0.1)', border: '1px solid rgba(34,197,94,0.3)', borderRadius: 6, color: '#22c55e', fontSize: 13 }}>
          {success}
        </div>
      )}
      {error && (
        <div style={{ marginBottom: 14, padding: '10px 14px', background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 6, color: '#ef4444', fontSize: 13 }}>
          {error}
        </div>
      )}

      {/* Rules List */}
      {loading ? (
        <div style={{ textAlign: 'center', padding: '60px 0', color: 'var(--text-muted)', fontSize: 14 }}>Loading rules…</div>
      ) : rules.length === 0 ? (
        <div style={{ textAlign: 'center', padding: '60px 0', color: 'var(--text-muted)', fontSize: 14 }}>
          <div style={{ fontSize: 32, marginBottom: 12 }}>📦</div>
          No shipping rules yet. Click <strong>Add Rule</strong> to create one.
          <p style={{ fontSize: 12, marginTop: 8, color: 'var(--text-muted)' }}>
            Example: if channel = Amazon AND weight &lt; 500g → Royal Mail Tracked 48
          </p>
        </div>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
          {rules.sort((a, b) => a.priority - b.priority).map((rule, i) => (
            <div
              key={rule.definition_id || rule.rule_id || i}
              style={{
                padding: '16px 20px',
                background: 'var(--bg-secondary)',
                border: `1px solid ${rule.is_active ? 'var(--border)' : 'var(--border)'}`,
                borderRadius: 10,
                opacity: rule.is_active ? 1 : 0.55,
                display: 'grid',
                gridTemplateColumns: '40px 1fr auto auto auto',
                alignItems: 'center',
                gap: 16,
              }}
            >
              {/* Priority badge */}
              <div style={{ textAlign: 'center', fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6, padding: '4px 0' }}>
                #{rule.priority}
              </div>

              {/* Content */}
              <div>
                <div style={{ fontSize: 14, fontWeight: 600, color: 'var(--text-primary)', marginBottom: 4 }}>{rule.name}</div>
                <div style={{ fontSize: 12, color: 'var(--text-muted)', display: 'flex', flexWrap: 'wrap', gap: 6 }}>
                  {rule.conditions.map((c, ci) => (
                    <span key={ci} style={{ background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 4, padding: '2px 8px' }}>
                      {conditionLabel(c)}
                    </span>
                  ))}
                  <span style={{ color: 'var(--text-muted)' }}>→</span>
                  <span style={{ background: 'rgba(99,102,241,0.1)', border: '1px solid rgba(99,102,241,0.3)', color: 'var(--primary)', borderRadius: 4, padding: '2px 8px', fontWeight: 600 }}>
                    {actionLabel(rule)}
                  </span>
                </div>
              </div>

              {/* Active toggle */}
              <button
                onClick={() => toggleActive(rule)}
                style={{ ...btnSmall, color: rule.is_active ? '#22c55e' : 'var(--text-muted)', borderColor: rule.is_active ? 'rgba(34,197,94,0.4)' : 'var(--border)' }}
              >
                {rule.is_active ? '● Active' : '○ Inactive'}
              </button>

              <button onClick={() => openEdit(rule)} style={btnSmall}>Edit</button>
              <button onClick={() => deleteRule(rule)} style={{ ...btnSmall, color: '#ef4444', borderColor: 'rgba(239,68,68,0.3)' }}>Delete</button>
            </div>
          ))}
        </div>
      )}

      {/* Modal */}
      {showModal && (
        <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.6)', zIndex: 1000, display: 'flex', alignItems: 'center', justifyContent: 'center', padding: 24 }}>
          <div style={{ background: 'var(--bg-primary)', border: '1px solid var(--border)', borderRadius: 14, padding: 28, width: '100%', maxWidth: 600, maxHeight: '90vh', overflowY: 'auto' }}>
            <h2 style={{ margin: '0 0 20px', fontSize: 18, fontWeight: 700, color: 'var(--text-primary)' }}>
              {editing ? 'Edit Rule' : 'New Shipping Rule'}
            </h2>

            {error && (
              <div style={{ marginBottom: 14, padding: '8px 12px', background: 'rgba(239,68,68,0.1)', borderRadius: 6, color: '#ef4444', fontSize: 13 }}>
                {error}
              </div>
            )}

            {/* Name */}
            <label style={labelStyle}>Rule Name</label>
            <input
              value={form.name}
              onChange={e => setForm(f => ({ ...f, name: e.target.value }))}
              placeholder="e.g. Amazon Light Items → Royal Mail"
              style={{ ...inputStyle, width: '100%', marginBottom: 14, boxSizing: 'border-box' }}
            />

            {/* Priority */}
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12, marginBottom: 14 }}>
              <div>
                <label style={labelStyle}>Priority (lower = first)</label>
                <input
                  type="number"
                  value={form.priority}
                  onChange={e => setForm(f => ({ ...f, priority: Number(e.target.value) }))}
                  style={{ ...inputStyle, width: '100%', boxSizing: 'border-box' }}
                />
              </div>
              <div style={{ display: 'flex', alignItems: 'flex-end', paddingBottom: 2 }}>
                <label style={{ display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer', fontSize: 13, color: 'var(--text-secondary)' }}>
                  <input
                    type="checkbox"
                    checked={form.is_active}
                    onChange={e => setForm(f => ({ ...f, is_active: e.target.checked }))}
                  />
                  Active
                </label>
              </div>
            </div>

            {/* Conditions */}
            <div style={{ marginBottom: 16 }}>
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 8 }}>
                <label style={{ ...labelStyle, marginBottom: 0 }}>Conditions (all must match)</label>
                <button onClick={addCondition} style={{ ...btnSmall, fontSize: 11 }}>+ Add condition</button>
              </div>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                {form.conditions.map((cond, ci) => (
                  <div key={ci} style={{ display: 'grid', gridTemplateColumns: '1fr auto', gap: 8, alignItems: 'center' }}>
                    <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 }}>
                      <select
                        value={cond.field}
                        onChange={e => updateCondition(ci, 'field', e.target.value)}
                        style={inputStyle}
                      >
                        {CONDITION_FIELDS.map(f => (
                          <option key={f.value} value={f.value}>{f.label}</option>
                        ))}
                      </select>
                      {cond.field === 'channel' ? (
                        <select
                          value={cond.value}
                          onChange={e => updateCondition(ci, 'value', e.target.value)}
                          style={inputStyle}
                        >
                          <option value="">Select channel…</option>
                          {CHANNEL_OPTIONS.map(ch => (
                            <option key={ch} value={ch}>{ch}</option>
                          ))}
                        </select>
                      ) : (
                        <input
                          value={cond.value}
                          onChange={e => updateCondition(ci, 'value', e.target.value)}
                          placeholder={cond.field.includes('value') ? 'e.g. 50' : cond.field.includes('kg') ? 'e.g. 0.5' : 'value'}
                          style={inputStyle}
                        />
                      )}
                    </div>
                    {form.conditions.length > 1 && (
                      <button
                        onClick={() => removeCondition(ci)}
                        style={{ ...btnSmall, color: '#ef4444', borderColor: 'rgba(239,68,68,0.3)', padding: '4px 8px' }}
                      >
                        ✕
                      </button>
                    )}
                  </div>
                ))}
              </div>
            </div>

            {/* Action */}
            <div style={{ marginBottom: 20 }}>
              <label style={labelStyle}>→ Then use</label>
              <select
                value={`${form.actions.carrier_id}::${form.actions.service_code}`}
                onChange={e => {
                  const [carrierId, serviceCode] = e.target.value.split('::');
                  const svc = KNOWN_SERVICES.find(s => s.carrier_id === carrierId && s.service_code === serviceCode);
                  setForm(f => ({
                    ...f,
                    actions: { carrier_id: carrierId, service_code: serviceCode, service_label: svc ? `${svc.carrier_name} — ${svc.service_name}` : '' },
                  }));
                }}
                style={{ ...inputStyle, width: '100%', boxSizing: 'border-box' }}
              >
                {KNOWN_SERVICES.map(s => (
                  <option key={`${s.carrier_id}::${s.service_code}`} value={`${s.carrier_id}::${s.service_code}`}>
                    {s.carrier_name} — {s.service_name}
                  </option>
                ))}
              </select>
            </div>

            <div style={{ display: 'flex', gap: 10, justifyContent: 'flex-end' }}>
              <button onClick={() => { setShowModal(false); setError(''); }} style={btnGhost}>Cancel</button>
              <button onClick={saveRule} disabled={saving} style={{ ...btnPrimary, opacity: saving ? 0.6 : 1 }}>
                {saving ? 'Saving…' : editing ? 'Update Rule' : 'Create Rule'}
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
  fontSize: 12,
  fontWeight: 600,
  color: 'var(--text-muted)',
  marginBottom: 6,
  textTransform: 'uppercase',
  letterSpacing: '0.04em',
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

const btnSmall: React.CSSProperties = {
  padding: '5px 12px',
  background: 'transparent',
  color: 'var(--text-secondary)',
  border: '1px solid var(--border)',
  borderRadius: 5,
  cursor: 'pointer',
  fontSize: 12,
  whiteSpace: 'nowrap',
};

import { useState, useEffect, useCallback } from 'react';
import { getActiveTenantId } from '../contexts/TenantContext';

const API_BASE = import.meta.env.VITE_API_URL ?? 'http://localhost:8080/api/v1';
function api(path: string, init?: RequestInit) {
  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId(), ...init?.headers },
  });
}

interface PriceSyncRule {
  rule_id: string;
  name: string;
  enabled: boolean;
  credential_id: string;
  channel: string;
  price_adj_type: string;
  price_adj_value: number;
  round_to: number;
  apply_to_all: boolean;
  last_run_at?: string;
}

interface PriceSyncLogEntry {
  log_id: string;
  rule_name: string;
  sku: string;
  old_price: number;
  new_price: number;
  channel: string;
  status: string;
  error_message?: string;
  created_at: string;
}

interface Credential {
  credential_id: string;
  name: string;
  channel: string;
}

export default function PriceSyncPage() {
  const [tab, setTab] = useState<'rules' | 'log'>('rules');
  const [rules, setRules] = useState<PriceSyncRule[]>([]);
  const [log, setLog] = useState<PriceSyncLogEntry[]>([]);
  const [credentials, setCredentials] = useState<Credential[]>([]);
  const [loading, setLoading] = useState(true);
  const [editModal, setEditModal] = useState<PriceSyncRule | null | 'new'>(null);
  const [triggering, setTriggering] = useState<string | null>(null);
  const [triggerResult, setTriggerResult] = useState<string | null>(null);

  // Form state
  const [fName, setFName] = useState('');
  const [fCredId, setFCredId] = useState('');
  const [fChannel, setFChannel] = useState('');
  const [fAdjType, setFAdjType] = useState('none');
  const [fAdjValue, setFAdjValue] = useState('0');
  const [fRoundTo, setFRoundTo] = useState('0');
  const [fApplyAll, setFApplyAll] = useState(true);
  const [fEnabled, setFEnabled] = useState(true);
  const [formError, setFormError] = useState('');
  const [saving, setSaving] = useState(false);

  const loadAll = useCallback(async () => {
    setLoading(true);
    try {
      const [rRes, lRes, cRes] = await Promise.all([
        api('/price-sync/rules'),
        api('/price-sync/log'),
        api('/marketplace/credentials'),
      ]);
      if (rRes.ok) setRules((await rRes.json()).rules || []);
      if (lRes.ok) setLog((await lRes.json()).entries || []);
      if (cRes.ok) {
        const d = await cRes.json();
        setCredentials(d.credentials || d.accounts || []);
      }
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { loadAll(); }, [loadAll]);

  const openEdit = (rule: PriceSyncRule | 'new') => {
    if (rule === 'new') {
      setFName(''); setFCredId(''); setFChannel(''); setFAdjType('none');
      setFAdjValue('0'); setFRoundTo('0'); setFApplyAll(true); setFEnabled(true);
    } else {
      setFName(rule.name); setFCredId(rule.credential_id); setFChannel(rule.channel);
      setFAdjType(rule.price_adj_type || 'none'); setFAdjValue(String(rule.price_adj_value || 0));
      setFRoundTo(String(rule.round_to || 0)); setFApplyAll(rule.apply_to_all); setFEnabled(rule.enabled);
    }
    setFormError('');
    setEditModal(rule);
  };

  const handleSave = async () => {
    if (!fName.trim()) { setFormError('Name is required'); return; }
    if (!fCredId) { setFormError('Select a channel account'); return; }
    setSaving(true); setFormError('');
    try {
      const isNew = editModal === 'new';
      const id = isNew ? '' : (editModal as PriceSyncRule).rule_id;
      const res = await api(isNew ? '/price-sync/rules' : `/price-sync/rules/${id}`, {
        method: isNew ? 'POST' : 'PUT',
        body: JSON.stringify({
          name: fName.trim(), credential_id: fCredId, channel: fChannel,
          price_adj_type: fAdjType, price_adj_value: parseFloat(fAdjValue) || 0,
          round_to: parseFloat(fRoundTo) || 0, apply_to_all: fApplyAll, enabled: fEnabled,
        }),
      });
      if (res.ok) { setEditModal(null); loadAll(); }
      else { const d = await res.json().catch(() => ({})); setFormError(d.error || 'Save failed'); }
    } catch { setFormError('Network error'); }
    finally { setSaving(false); }
  };

  const handleDelete = async (id: string) => {
    if (!confirm('Delete this rule?')) return;
    await api(`/price-sync/rules/${id}`, { method: 'DELETE' });
    loadAll();
  };

  const handleToggle = async (rule: PriceSyncRule) => {
    await api(`/price-sync/rules/${rule.rule_id}`, {
      method: 'PUT',
      body: JSON.stringify({ enabled: !rule.enabled }),
    });
    loadAll();
  };

  const handleTrigger = async (ruleId: string) => {
    setTriggering(ruleId); setTriggerResult(null);
    const res = await api('/price-sync/trigger', {
      method: 'POST',
      body: JSON.stringify({ rule_id: ruleId }),
    });
    if (res.ok) {
      const d = await res.json();
      setTriggerResult(`Synced ${d.synced} products`);
      loadAll();
    }
    setTriggering(null);
  };

  const adjLabel = (type: string, val: number) => {
    if (type === 'percent') return val >= 0 ? `+${val}%` : `${val}%`;
    if (type === 'fixed') return val >= 0 ? `+£${val.toFixed(2)}` : `−£${Math.abs(val).toFixed(2)}`;
    return 'No adjustment';
  };

  const statusColor = (s: string) => s === 'success' ? 'var(--success)' : s === 'error' ? 'var(--danger)' : 'var(--text-muted)';

  const CHANNEL_ICONS: Record<string, string> = { amazon: '📦', ebay: '🛒', shopify: '🛍️', woocommerce: '🌐', etsy: '🎨' };

  return (
    <div style={{ padding: '24px 32px', maxWidth: 1100, margin: '0 auto' }}>
      <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', marginBottom: 24 }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 24, fontWeight: 700, color: 'var(--text-primary)' }}>Price Sync</h1>
          <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 14 }}>
            Automatically push product prices to channel listings with optional markup/markdown rules.
          </p>
        </div>
        {tab === 'rules' && (
          <button style={btnPrimaryStyle} onClick={() => openEdit('new')}>+ New Rule</button>
        )}
      </div>

      {triggerResult && (
        <div style={{ padding: '10px 14px', background: 'rgba(16,185,129,0.1)', border: '1px solid rgba(16,185,129,0.3)', borderRadius: 6, color: 'var(--success)', fontSize: 13, marginBottom: 16, display: 'flex', justifyContent: 'space-between' }}>
          <span>✓ {triggerResult}</span>
          <button style={{ background: 'none', border: 'none', color: 'var(--success)', cursor: 'pointer' }} onClick={() => setTriggerResult(null)}>✕</button>
        </div>
      )}

      {/* Tabs */}
      <div style={{ display: 'flex', borderBottom: '1px solid var(--border)', marginBottom: 24 }}>
        {(['rules', 'log'] as const).map(t => (
          <button key={t} onClick={() => setTab(t)} style={{
            padding: '10px 20px', background: 'none', border: 'none',
            borderBottom: tab === t ? '2px solid var(--primary)' : '2px solid transparent',
            color: tab === t ? 'var(--primary)' : 'var(--text-muted)',
            cursor: 'pointer', fontSize: 14, fontWeight: tab === t ? 600 : 400, marginBottom: -1,
          }}>
            {t === 'rules' ? '⚙ Rules' : '📋 Sync Log'}
          </button>
        ))}
      </div>

      {loading ? (
        <div style={{ padding: 48, textAlign: 'center', color: 'var(--text-muted)' }}>Loading…</div>
      ) : tab === 'rules' ? (
        rules.length === 0 ? (
          <div style={{ padding: '64px 32px', textAlign: 'center', background: 'var(--bg-secondary)', borderRadius: 12, border: '1px solid var(--border)' }}>
            <div style={{ fontSize: 40, marginBottom: 12 }}>💱</div>
            <h3 style={{ margin: '0 0 8px', color: 'var(--text-primary)' }}>No price sync rules</h3>
            <p style={{ color: 'var(--text-muted)', margin: '0 0 16px' }}>Create a rule to automatically push prices to your channel listings.</p>
            <button style={btnPrimaryStyle} onClick={() => openEdit('new')}>+ Create First Rule</button>
          </div>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
            {rules.map(rule => (
              <div key={rule.rule_id} style={{
                background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 10,
                padding: '16px 20px', display: 'flex', alignItems: 'center', gap: 16,
                opacity: rule.enabled ? 1 : 0.6,
              }}>
                {/* Toggle */}
                <button
                  onClick={() => handleToggle(rule)}
                  style={{
                    width: 40, height: 22, borderRadius: 11, border: 'none', cursor: 'pointer',
                    background: rule.enabled ? 'var(--primary)' : 'var(--bg-elevated)',
                    position: 'relative', flexShrink: 0, transition: 'background 0.2s',
                  }}
                  title={rule.enabled ? 'Disable rule' : 'Enable rule'}
                >
                  <div style={{
                    position: 'absolute', top: 3, left: rule.enabled ? 21 : 3,
                    width: 16, height: 16, borderRadius: '50%', background: 'white', transition: 'left 0.2s',
                  }} />
                </button>

                {/* Icon + info */}
                <span style={{ fontSize: 20 }}>{CHANNEL_ICONS[rule.channel] || '🔗'}</span>
                <div style={{ flex: 1 }}>
                  <div style={{ fontWeight: 600, color: 'var(--text-primary)', marginBottom: 2 }}>{rule.name}</div>
                  <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                    {rule.channel} · {adjLabel(rule.price_adj_type, rule.price_adj_value)} · {rule.apply_to_all ? 'All SKUs' : 'Selected SKUs'}
                    {rule.last_run_at && ` · Last run: ${new Date(rule.last_run_at).toLocaleString()}`}
                  </div>
                </div>

                {/* Actions */}
                <div style={{ display: 'flex', gap: 6 }}>
                  <button
                    style={btnSmallStyle}
                    onClick={() => handleTrigger(rule.rule_id)}
                    disabled={triggering === rule.rule_id}
                  >
                    {triggering === rule.rule_id ? 'Running…' : '▶ Run Now'}
                  </button>
                  <button style={btnSmallStyle} onClick={() => openEdit(rule)}>Edit</button>
                  <button style={{ ...btnSmallStyle, color: 'var(--danger)' }} onClick={() => handleDelete(rule.rule_id)}>Delete</button>
                </div>
              </div>
            ))}
          </div>
        )
      ) : (
        /* Sync Log */
        <div style={{ background: 'var(--bg-secondary)', borderRadius: 12, border: '1px solid var(--border)', overflow: 'hidden' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
            <thead>
              <tr style={{ background: 'var(--bg-tertiary)' }}>
                {['Date', 'Rule', 'SKU', 'Channel', 'Old Price', 'New Price', 'Status'].map(h => (
                  <th key={h} style={thStyle}>{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {log.map(entry => (
                <tr key={entry.log_id} style={{ borderTop: '1px solid var(--border)' }}>
                  <td style={{ ...tdStyle, fontSize: 12, color: 'var(--text-muted)', whiteSpace: 'nowrap' }}>
                    {new Date(entry.created_at).toLocaleString()}
                  </td>
                  <td style={{ ...tdStyle, color: 'var(--text-secondary)' }}>{entry.rule_name}</td>
                  <td style={{ ...tdStyle, fontFamily: 'monospace', fontSize: 12 }}>{entry.sku}</td>
                  <td style={tdStyle}>{CHANNEL_ICONS[entry.channel] || ''} {entry.channel}</td>
                  <td style={{ ...tdStyle, color: 'var(--text-secondary)' }}>£{entry.old_price?.toFixed(2)}</td>
                  <td style={{ ...tdStyle, fontWeight: 600 }}>£{entry.new_price?.toFixed(2)}</td>
                  <td style={tdStyle}>
                    <span style={{ padding: '2px 8px', borderRadius: 12, fontSize: 11, fontWeight: 600, color: statusColor(entry.status), background: `${statusColor(entry.status)}20` }}>
                      {entry.status}
                    </span>
                    {entry.error_message && (
                      <div style={{ fontSize: 11, color: 'var(--danger)', marginTop: 2 }}>{entry.error_message}</div>
                    )}
                  </td>
                </tr>
              ))}
              {log.length === 0 && (
                <tr><td colSpan={7} style={{ padding: '32px', textAlign: 'center', color: 'var(--text-muted)' }}>
                  No sync log entries yet. Run a rule to see results here.
                </td></tr>
              )}
            </tbody>
          </table>
        </div>
      )}

      {/* Edit Modal */}
      {editModal !== null && (
        <div style={overlayStyle}>
          <div style={{ ...modalStyle, width: 500 }}>
            <div style={modalHeaderStyle}>
              <h3 style={modalTitleStyle}>{editModal === 'new' ? 'New Price Sync Rule' : 'Edit Rule'}</h3>
              <button style={closeBtnStyle} onClick={() => setEditModal(null)}>✕</button>
            </div>
            <div style={{ padding: '20px 24px', maxHeight: '70vh', overflowY: 'auto' }}>
              <div style={fieldStyle}>
                <label style={labelStyle}>Rule name <span style={{ color: 'var(--danger)' }}>*</span></label>
                <input style={inputStyle} placeholder="e.g. Amazon UK - 5% markup" value={fName} onChange={e => setFName(e.target.value)} autoFocus />
              </div>
              <div style={fieldStyle}>
                <label style={labelStyle}>Channel account <span style={{ color: 'var(--danger)' }}>*</span></label>
                <select style={inputStyle} value={fCredId} onChange={e => {
                  setFCredId(e.target.value);
                  const cred = credentials.find(c => c.credential_id === e.target.value);
                  if (cred) setFChannel(cred.channel);
                }}>
                  <option value="">Select account…</option>
                  {credentials.map(c => <option key={c.credential_id} value={c.credential_id}>{c.name}</option>)}
                </select>
              </div>
              <div style={fieldStyle}>
                <label style={labelStyle}>Price adjustment</label>
                <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 }}>
                  <select style={inputStyle} value={fAdjType} onChange={e => setFAdjType(e.target.value)}>
                    <option value="none">No adjustment</option>
                    <option value="percent">Percentage</option>
                    <option value="fixed">Fixed amount</option>
                  </select>
                  {fAdjType !== 'none' && (
                    <input type="number" style={inputStyle} placeholder="e.g. 5 for +5%" value={fAdjValue} onChange={e => setFAdjValue(e.target.value)} />
                  )}
                </div>
                {fAdjType !== 'none' && (
                  <p style={{ margin: '4px 0 0', fontSize: 12, color: 'var(--text-muted)' }}>
                    Positive = markup, negative = markdown. Example: 5% on £10 product → £10.50
                  </p>
                )}
              </div>
              <div style={fieldStyle}>
                <label style={labelStyle}>Price rounding (optional)</label>
                <select style={inputStyle} value={fRoundTo} onChange={e => setFRoundTo(e.target.value)}>
                  <option value="0">No rounding</option>
                  <option value="0.99">Charm pricing (£x.99)</option>
                  <option value="0.95">Round to .95</option>
                  <option value="0.00">Round to whole £</option>
                </select>
              </div>
              <div style={{ marginTop: 14, display: 'flex', alignItems: 'center', gap: 10 }}>
                <input type="checkbox" id="applyAll" checked={fApplyAll} onChange={e => setFApplyAll(e.target.checked)} />
                <label htmlFor="applyAll" style={{ fontSize: 13, color: 'var(--text-secondary)', cursor: 'pointer' }}>Apply to all products</label>
              </div>
              <div style={{ marginTop: 10, display: 'flex', alignItems: 'center', gap: 10 }}>
                <input type="checkbox" id="enabled" checked={fEnabled} onChange={e => setFEnabled(e.target.checked)} />
                <label htmlFor="enabled" style={{ fontSize: 13, color: 'var(--text-secondary)', cursor: 'pointer' }}>Rule enabled</label>
              </div>
              {formError && <p style={{ margin: '12px 0 0', fontSize: 13, color: 'var(--danger)' }}>{formError}</p>}
            </div>
            <div style={modalFooterStyle}>
              <button style={btnGhostStyle} onClick={() => setEditModal(null)}>Cancel</button>
              <button style={btnPrimaryStyle} onClick={handleSave} disabled={saving}>
                {saving ? 'Saving…' : (editModal === 'new' ? 'Create Rule' : 'Save Changes')}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

// ─── Styles ───────────────────────────────────────────────────────────────────
const overlayStyle: React.CSSProperties = { position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.6)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 };
const modalStyle: React.CSSProperties = { background: 'var(--bg-secondary)', borderRadius: 12, border: '1px solid var(--border)', maxWidth: '95vw' };
const modalHeaderStyle: React.CSSProperties = { display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '18px 24px', borderBottom: '1px solid var(--border)' };
const modalTitleStyle: React.CSSProperties = { margin: 0, fontSize: 17, fontWeight: 600, color: 'var(--text-primary)' };
const modalFooterStyle: React.CSSProperties = { display: 'flex', justifyContent: 'flex-end', gap: 8, padding: '14px 24px', borderTop: '1px solid var(--border)' };
const closeBtnStyle: React.CSSProperties = { background: 'none', border: 'none', color: 'var(--text-muted)', cursor: 'pointer', fontSize: 18 };
const fieldStyle: React.CSSProperties = { marginTop: 14 };
const labelStyle: React.CSSProperties = { display: 'block', marginBottom: 6, fontSize: 13, fontWeight: 500, color: 'var(--text-secondary)' };
const inputStyle: React.CSSProperties = { width: '100%', padding: '8px 12px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', fontSize: 14, outline: 'none', boxSizing: 'border-box' };
const btnPrimaryStyle: React.CSSProperties = { padding: '8px 16px', background: 'var(--primary)', color: 'white', border: 'none', borderRadius: 6, cursor: 'pointer', fontSize: 13, fontWeight: 600 };
const btnGhostStyle: React.CSSProperties = { padding: '8px 16px', background: 'transparent', color: 'var(--text-secondary)', border: '1px solid var(--border)', borderRadius: 6, cursor: 'pointer', fontSize: 13 };
const btnSmallStyle: React.CSSProperties = { padding: '4px 10px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 4, color: 'var(--text-secondary)', cursor: 'pointer', fontSize: 12 };
const thStyle: React.CSSProperties = { padding: '10px 16px', textAlign: 'left', fontSize: 11, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--text-muted)', borderBottom: '1px solid var(--border)' };
const tdStyle: React.CSSProperties = { padding: '10px 16px', color: 'var(--text-primary)' };

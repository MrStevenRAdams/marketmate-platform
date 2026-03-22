import { useState, useEffect } from 'react';
import { Link } from 'react-router-dom';
import { getActiveTenantId } from '../../contexts/TenantContext';
import '../../components/SettingsLayout.css';

const API = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';
function api(path: string, init?: RequestInit) {
  return fetch(`${API}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId(), ...(init?.headers || {}) },
  });
}

interface StatusMapping { internal_status: string; display_name: string; }
interface OrderSettings {
  auto_merge_enabled: boolean; merge_same_address: boolean; split_threshold: number;
  block_merge_flag_default: boolean; check_weight: boolean; check_items: boolean;
  check_packaging: boolean; date_format_display: string; default_payment_method: string;
  status_mappings: StatusMapping[]; despatch_button_action: string;
}
type Toast = { msg: string; type: 'success' | 'error' } | null;

const blank: OrderSettings = {
  auto_merge_enabled: false, merge_same_address: false, split_threshold: 0,
  block_merge_flag_default: false, check_weight: true, check_items: true,
  check_packaging: false, date_format_display: 'DD/MM/YYYY', default_payment_method: '',
  status_mappings: [], despatch_button_action: 'complete_and_process',
};

export default function OrderSettings() {
  const [s, setS] = useState<OrderSettings>(blank);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [toast, setToast] = useState<Toast>(null);
  const [newInternal, setNewInternal] = useState('');
  const [newDisplay, setNewDisplay] = useState('');

  const showToast = (msg: string, type: 'success' | 'error') => {
    setToast({ msg, type }); setTimeout(() => setToast(null), 3000);
  };

  useEffect(() => {
    api('/settings/orders').then(r => r.json()).then(d => setS({ ...blank, ...(d.settings || {}) }))
      .catch(() => {}).finally(() => setLoading(false));
  }, []);

  const set = <K extends keyof OrderSettings>(key: K, value: OrderSettings[K]) =>
    setS(prev => ({ ...prev, [key]: value }));

  const save = async () => {
    setSaving(true);
    try {
      const r = await api('/settings/orders', { method: 'PUT', body: JSON.stringify(s) });
      if (!r.ok) throw new Error(await r.text());
      showToast('Settings saved', 'success');
    } catch (e: any) { showToast(e.message || 'Save failed', 'error'); }
    finally { setSaving(false); }
  };

  const addMapping = () => {
    if (!newInternal.trim()) return;
    set('status_mappings', [...s.status_mappings, { internal_status: newInternal.trim(), display_name: newDisplay.trim() }]);
    setNewInternal(''); setNewDisplay('');
  };
  const removeMapping = (idx: number) => set('status_mappings', s.status_mappings.filter((_, i) => i !== idx));

  if (loading) return <div className="settings-page"><div style={{ color: 'var(--text-muted)', padding: 40 }}>Loading…</div></div>;

  const Toggle = ({ label, checked, onChange }: { label: string; checked: boolean; onChange: (v: boolean) => void }) => (
    <label style={{ display: 'flex', alignItems: 'center', gap: 10, fontSize: 13, cursor: 'pointer', marginBottom: 10 }}>
      <input type="checkbox" checked={checked} onChange={e => onChange(e.target.checked)} />
      {label}
    </label>
  );

  return (
    <div className="settings-page">
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 4 }}>
        <Link to="/settings" style={{ color: 'var(--text-muted)', fontSize: 13, textDecoration: 'none' }}>← Settings</Link>
      </div>
      <h1 className="settings-page-title">Order Settings</h1>
      <p className="settings-page-sub">Configure order merging, pre-processing checks and despatch behaviour.</p>

      {/* Merge & Split */}
      <div className="settings-section">
        <div className="settings-section-title">Merge &amp; Split Settings</div>
        <Toggle label="Enable automatic order merging" checked={s.auto_merge_enabled} onChange={v => set('auto_merge_enabled', v)} />
        <Toggle label="Merge orders with the same shipping address" checked={s.merge_same_address} onChange={v => set('merge_same_address', v)} />
        <Toggle label="Block merge by default for new orders" checked={s.block_merge_flag_default} onChange={v => set('block_merge_flag_default', v)} />
        <div className="settings-field" style={{ maxWidth: 300 }}>
          <label className="settings-label">Split Threshold (max items per order, 0 = no limit)</label>
          <input className="settings-input" type="number" min={0} value={s.split_threshold} onChange={e => set('split_threshold', parseInt(e.target.value) || 0)} />
        </div>
      </div>

      {/* Pre-Processing */}
      <div className="settings-section">
        <div className="settings-section-title">Pre-Processing Checks</div>
        <Toggle label="Verify item weight before despatch" checked={s.check_weight} onChange={v => set('check_weight', v)} />
        <Toggle label="Verify item count before despatch" checked={s.check_items} onChange={v => set('check_items', v)} />
        <Toggle label="Check packaging suitability before despatch" checked={s.check_packaging} onChange={v => set('check_packaging', v)} />
      </div>

      {/* Order Display */}
      <div className="settings-section">
        <div className="settings-section-title">Order Display</div>
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16, maxWidth: 600 }}>
          <div className="settings-field">
            <label className="settings-label">Date Format</label>
            <select className="settings-select" value={s.date_format_display} onChange={e => set('date_format_display', e.target.value)}>
              {['DD/MM/YYYY','MM/DD/YYYY','YYYY-MM-DD'].map(f => <option key={f} value={f}>{f}</option>)}
            </select>
          </div>
          <div className="settings-field">
            <label className="settings-label">Default Payment Method</label>
            <input className="settings-input" value={s.default_payment_method} onChange={e => set('default_payment_method', e.target.value)} placeholder="e.g. Credit Card" />
          </div>
        </div>
      </div>

      {/* Status Mappings */}
      <div className="settings-section">
        <div className="settings-section-title">Order Status Mappings</div>
        <p style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 16 }}>Map internal status labels to custom display names shown in the UI.</p>
        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13, marginBottom: 12 }}>
          <thead>
            <tr>{['Internal Status','Display Name',''].map(h => (
              <th key={h} style={{ textAlign: 'left', padding: '0 12px 8px', fontSize: 11, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.06em', color: 'var(--text-muted)', borderBottom: '1px solid var(--border)' }}>{h}</th>
            ))}</tr>
          </thead>
          <tbody>
            {s.status_mappings.map((m, i) => (
              <tr key={i}>
                <td style={{ padding: 10, borderBottom: '1px solid var(--border)' }}>{m.internal_status}</td>
                <td style={{ padding: 10, borderBottom: '1px solid var(--border)' }}>{m.display_name}</td>
                <td style={{ padding: 10, borderBottom: '1px solid var(--border)' }}>
                  <button onClick={() => removeMapping(i)} style={{ background: 'none', border: 'none', color: '#ef4444', cursor: 'pointer', fontSize: 16 }}>✕</button>
                </td>
              </tr>
            ))}
            <tr>
              <td style={{ padding: 8 }}><input className="settings-input" style={{ marginBottom: 0 }} value={newInternal} onChange={e => setNewInternal(e.target.value)} placeholder="e.g. awaiting_payment" /></td>
              <td style={{ padding: 8 }}><input className="settings-input" style={{ marginBottom: 0 }} value={newDisplay} onChange={e => setNewDisplay(e.target.value)} placeholder="e.g. Pending Payment" /></td>
              <td style={{ padding: 8 }}><button className="settings-btn-secondary" onClick={addMapping} style={{ padding: '6px 12px' }}>Add</button></td>
            </tr>
          </tbody>
        </table>
      </div>

      {/* Despatch Button */}
      <div className="settings-section">
        <div className="settings-section-title">Despatch Button Behaviour</div>
        <div className="settings-field" style={{ maxWidth: 400 }}>
          <label className="settings-label">When the Despatch button is clicked</label>
          <select className="settings-select" value={s.despatch_button_action} onChange={e => set('despatch_button_action', e.target.value)}>
            <option value="complete_and_process">Complete &amp; Process (default)</option>
            <option value="complete_only">Complete Only</option>
            <option value="process_only">Process Only</option>
          </select>
        </div>
      </div>

      <div style={{ marginTop: 8 }}>
        <button className="settings-btn-primary" onClick={save} disabled={saving}>{saving ? 'Saving…' : 'Save Changes'}</button>
      </div>

      {toast && <div className={`settings-toast ${toast.type}`}>{toast.type === 'success' ? '✓' : '✗'} {toast.msg}</div>}
    </div>
  );
}

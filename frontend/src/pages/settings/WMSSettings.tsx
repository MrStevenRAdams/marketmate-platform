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

interface WMSSettings {
  auto_allocation_enabled: boolean;
  assignable_types: string[];
  fifo_enabled: boolean;
  binrack_suggestions_enabled: boolean;
  binrack_suggestion_use_batch: boolean;
}
type Toast = { msg: string; type: 'success' | 'error' } | null;

const blank: WMSSettings = {
  auto_allocation_enabled: false,
  assignable_types: ['order'],
  fifo_enabled: false,
  binrack_suggestions_enabled: false,
  binrack_suggestion_use_batch: false,
};

export default function WMSSettings() {
  const [s, setS] = useState<WMSSettings>(blank);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [toast, setToast] = useState<Toast>(null);

  const showToast = (msg: string, type: 'success' | 'error') => {
    setToast({ msg, type }); setTimeout(() => setToast(null), 3000);
  };

  useEffect(() => {
    api('/settings/wms').then(r => r.json()).then(d => setS({ ...blank, ...(d.settings || {}) }))
      .catch(() => {}).finally(() => setLoading(false));
  }, []);

  const toggleAssignable = (type: string) => {
    setS(prev => ({
      ...prev,
      assignable_types: prev.assignable_types.includes(type)
        ? prev.assignable_types.filter(t => t !== type)
        : [...prev.assignable_types, type],
    }));
  };

  const save = async () => {
    setSaving(true);
    try {
      const r = await api('/settings/wms', { method: 'PUT', body: JSON.stringify(s) });
      if (!r.ok) throw new Error(await r.text());
      showToast('WMS settings saved', 'success');
    } catch (e: any) { showToast(e.message || 'Save failed', 'error'); }
    finally { setSaving(false); }
  };

  if (loading) return <div className="settings-page"><div style={{ color: 'var(--text-muted)', padding: 40 }}>Loading…</div></div>;

  const Toggle = ({ label, desc, checked, onChange }: { label: string; desc?: string; checked: boolean; onChange: (v: boolean) => void }) => (
    <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', padding: '14px 0', borderBottom: '1px solid var(--border)' }}>
      <div>
        <div style={{ fontSize: 14, fontWeight: 500, color: 'var(--text-primary)' }}>{label}</div>
        {desc && <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 3 }}>{desc}</div>}
      </div>
      <label style={{ position: 'relative', display: 'inline-flex', alignItems: 'center', cursor: 'pointer', flexShrink: 0, marginLeft: 16 }}>
        <input type="checkbox" checked={checked} onChange={e => onChange(e.target.checked)} style={{ position: 'absolute', opacity: 0, width: 0 }} />
        <div style={{
          width: 40, height: 22, borderRadius: 11, background: checked ? 'var(--primary, #7c3aed)' : 'var(--border)',
          transition: '0.2s', position: 'relative',
        }}>
          <div style={{
            position: 'absolute', top: 2, left: checked ? 20 : 2, width: 18, height: 18,
            borderRadius: '50%', background: 'white', transition: '0.2s', boxShadow: '0 1px 3px rgba(0,0,0,0.2)',
          }} />
        </div>
      </label>
    </div>
  );

  return (
    <div className="settings-page">
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 4 }}>
        <Link to="/settings" style={{ color: 'var(--text-muted)', fontSize: 13, textDecoration: 'none' }}>← Settings</Link>
      </div>
      <h1 className="settings-page-title">WMS Settings</h1>
      <p className="settings-page-sub">Configure warehouse management system behaviour, allocation and FIFO rules.</p>

      <div className="settings-section">
        <div className="settings-section-title">Auto Allocation</div>
        <Toggle
          label="Enable Auto-Allocation"
          desc="Automatically allocate stock to orders based on rules when an order arrives."
          checked={s.auto_allocation_enabled}
          onChange={v => setS(prev => ({ ...prev, auto_allocation_enabled: v }))}
        />
        <div style={{ marginTop: 16 }}>
          <label className="settings-label">Assignable Types</label>
          <div style={{ display: 'flex', gap: 16, marginTop: 8 }}>
            {['order','purchase_order'].map(type => (
              <label key={type} style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13, cursor: 'pointer' }}>
                <input type="checkbox" checked={s.assignable_types.includes(type)} onChange={() => toggleAssignable(type)} />
                {type === 'order' ? 'Sales Orders' : 'Purchase Orders'}
              </label>
            ))}
          </div>
        </div>
      </div>

      <div className="settings-section">
        <div className="settings-section-title">FIFO &amp; Stock Control</div>
        <Toggle
          label="Enable FIFO (First In, First Out)"
          desc="Pick the oldest stock first. Requires batch/lot tracking to be active."
          checked={s.fifo_enabled}
          onChange={v => setS(prev => ({ ...prev, fifo_enabled: v }))}
        />
      </div>

      <div className="settings-section">
        <div className="settings-section-title">Binrack Suggestions</div>
        <Toggle
          label="Enable Binrack Suggestions"
          desc="Suggest the best binrack for put-away and picking based on storage rules."
          checked={s.binrack_suggestions_enabled}
          onChange={v => setS(prev => ({ ...prev, binrack_suggestions_enabled: v }))}
        />
        {s.binrack_suggestions_enabled && (
          <Toggle
            label="Use Batch/Lot When Suggesting Binracks"
            desc="When enabled, suggestions prefer binracks already holding the same batch."
            checked={s.binrack_suggestion_use_batch}
            onChange={v => setS(prev => ({ ...prev, binrack_suggestion_use_batch: v }))}
          />
        )}
      </div>

      <div style={{ marginTop: 8 }}>
        <button className="settings-btn-primary" onClick={save} disabled={saving}>{saving ? 'Saving…' : 'Save Changes'}</button>
      </div>

      {toast && <div className={`settings-toast ${toast.type}`}>{toast.type === 'success' ? '✓' : '✗'} {toast.msg}</div>}
    </div>
  );
}

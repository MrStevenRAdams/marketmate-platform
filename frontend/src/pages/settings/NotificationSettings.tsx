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

type DeliveryMethod = 'in_app' | 'email' | 'both';

interface NotifPref {
  enabled: boolean;
  delivery: DeliveryMethod;
  threshold?: number;
}

interface NotifConfig {
  low_stock: NotifPref;
  failed_order: NotifPref;
  po_overdue: NotifPref;
  new_rma: NotifPref;
  import_complete: NotifPref;
}

const DEFAULT: NotifConfig = {
  low_stock:       { enabled: true,  delivery: 'both',   threshold: 5 },
  failed_order:    { enabled: true,  delivery: 'both' },
  po_overdue:      { enabled: true,  delivery: 'in_app' },
  new_rma:         { enabled: true,  delivery: 'in_app' },
  import_complete: { enabled: false, delivery: 'in_app' },
};

const LABELS: Record<keyof NotifConfig, { title: string; desc: string }> = {
  low_stock:       { title: 'Low Stock Alert',          desc: 'When any SKU drops below the threshold quantity' },
  failed_order:    { title: 'Failed Order Alert',       desc: 'When an order fails to import or process' },
  po_overdue:      { title: 'PO Delivery Overdue',      desc: 'When a purchase order is past its expected delivery date' },
  new_rma:         { title: 'New RMA Received',         desc: 'When a buyer submits a return or a marketplace return is synced' },
  import_complete: { title: 'Import Completed',         desc: 'When a bulk import job finishes (success or with errors)' },
};

type Toast = { msg: string; type: 'success' | 'error' } | null;

function DeliverySelect({ value, onChange }: { value: DeliveryMethod; onChange: (v: DeliveryMethod) => void }) {
  return (
    <select className="settings-select" style={{ width: 130 }} value={value} onChange={e => onChange(e.target.value as DeliveryMethod)}>
      <option value="in_app">In-app</option>
      <option value="email">Email</option>
      <option value="both">Both</option>
    </select>
  );
}

export default function NotificationSettings() {
  const [config, setConfig] = useState<NotifConfig>(DEFAULT);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [toast, setToast] = useState<Toast>(null);

  useEffect(() => {
    api('/settings/notifications').then(r => r.json()).then(d => {
      if (d.notifications) setConfig({ ...DEFAULT, ...d.notifications });
    }).catch(() => {}).finally(() => setLoading(false));
  }, []);

  const showToast = (msg: string, type: 'success' | 'error') => {
    setToast({ msg, type });
    setTimeout(() => setToast(null), 3000);
  };

  const save = async () => {
    setSaving(true);
    try {
      const r = await api('/settings/notifications', { method: 'PUT', body: JSON.stringify({ notifications: config }) });
      if (!r.ok) throw new Error(await r.text());
      showToast('Notification preferences saved', 'success');
    } catch (e: any) {
      showToast(e.message || 'Save failed', 'error');
    } finally { setSaving(false); }
  };

  const update = (key: keyof NotifConfig, patch: Partial<NotifPref>) =>
    setConfig(c => ({ ...c, [key]: { ...c[key], ...patch } }));

  if (loading) return <div className="settings-page"><div style={{ color: 'var(--text-muted)', padding: 40 }}>Loading…</div></div>;

  return (
    <div className="settings-page">
      <div className="settings-breadcrumb">
        <Link to="/settings">Settings</Link>
        <span className="settings-breadcrumb-sep">›</span>
        <span className="settings-breadcrumb-current">Notification Preferences</span>
      </div>

      <h1 className="settings-page-title">Notification Preferences</h1>
      <p className="settings-page-sub">Choose which events trigger notifications and how they are delivered.</p>

      <div className="settings-section">
        <div className="settings-section-title">Alert Configuration</div>

        {(Object.keys(LABELS) as Array<keyof NotifConfig>).map((key) => {
          const pref = config[key];
          const meta = LABELS[key];
          return (
            <div key={key} className="settings-toggle-row" style={{ alignItems: 'flex-start', paddingTop: 14, paddingBottom: 14 }}>
              <div className="settings-toggle-info" style={{ flex: 1 }}>
                <span className="settings-toggle-label">{meta.title}</span>
                <span className="settings-toggle-desc">{meta.desc}</span>
                {key === 'low_stock' && pref.enabled && (
                  <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginTop: 8 }}>
                    <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>Trigger when stock falls below</span>
                    <input
                      type="number"
                      min={1}
                      className="settings-input"
                      style={{ width: 72 }}
                      value={pref.threshold ?? 5}
                      onChange={e => update(key, { threshold: parseInt(e.target.value) || 1 })}
                    />
                    <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>units</span>
                  </div>
                )}
              </div>
              <div style={{ display: 'flex', alignItems: 'center', gap: 12, flexShrink: 0, marginTop: 2 }}>
                {pref.enabled && (
                  <DeliverySelect value={pref.delivery} onChange={(v) => update(key, { delivery: v })} />
                )}
                <label className="settings-toggle">
                  <input type="checkbox" checked={pref.enabled} onChange={e => update(key, { enabled: e.target.checked })} />
                  <span className="settings-toggle-slider" />
                </label>
              </div>
            </div>
          );
        })}
      </div>

      <div className="settings-btn-row">
        <button className="settings-btn-primary" onClick={save} disabled={saving}>
          {saving ? 'Saving…' : 'Save Preferences'}
        </button>
      </div>

      {toast && (
        <div className={`settings-toast ${toast.type}`}>
          {toast.type === 'success' ? '✓' : '✗'} {toast.msg}
        </div>
      )}
    </div>
  );
}

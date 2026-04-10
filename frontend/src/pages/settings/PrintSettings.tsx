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

interface PrintSettings {
  invoice_auto_print: boolean; invoice_print_on_despatch: boolean;
  stock_label_format: string; stock_label_auto_print: boolean;
  shipping_label_sort: string;
}
type Toast = { msg: string; type: 'success' | 'error' } | null;

const blank: PrintSettings = {
  invoice_auto_print: false, invoice_print_on_despatch: false,
  stock_label_format: 'A4', stock_label_auto_print: false,
  shipping_label_sort: 'order_date',
};

export default function PrintSettings() {
  const [s, setS] = useState<PrintSettings>(blank);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [toast, setToast] = useState<Toast>(null);

  const showToast = (msg: string, type: 'success' | 'error') => {
    setToast({ msg, type }); setTimeout(() => setToast(null), 3000);
  };

  useEffect(() => {
    api('/settings/print').then(r => r.json()).then(d => setS({ ...blank, ...(d.settings || {}) }))
      .catch(() => {}).finally(() => setLoading(false));
  }, []);

  const set = <K extends keyof PrintSettings>(key: K, value: PrintSettings[K]) =>
    setS(prev => ({ ...prev, [key]: value }));

  const save = async () => {
    setSaving(true);
    try {
      const r = await api('/settings/print', { method: 'PUT', body: JSON.stringify(s) });
      if (!r.ok) throw new Error(await r.text());
      showToast('Print settings saved', 'success');
    } catch (e: any) { showToast(e.message || 'Save failed', 'error'); }
    finally { setSaving(false); }
  };

  if (loading) return <div className="settings-page"><div style={{ color: 'var(--text-muted)', padding: 40 }}>Loading…</div></div>;

  const Toggle = ({ label, checked, onChange }: { label: string; checked: boolean; onChange: (v: boolean) => void }) => (
    <label style={{ display: 'flex', alignItems: 'center', gap: 10, fontSize: 13, cursor: 'pointer', marginBottom: 12 }}>
      <input type="checkbox" checked={checked} onChange={e => onChange(e.target.checked)} />
      {label}
    </label>
  );

  return (
    <div className="settings-page">
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 4 }}>
        <Link to="/settings" style={{ color: 'var(--text-muted)', fontSize: 13, textDecoration: 'none' }}>← Settings</Link>
      </div>
      <h1 className="settings-page-title">Print Settings</h1>
      <p className="settings-page-sub">Configure automatic printing and label preferences.</p>

      <div className="settings-section">
        <div className="settings-section-title">Invoice Print Settings</div>
        <Toggle label="Auto-print invoices when an order is created" checked={s.invoice_auto_print} onChange={v => set('invoice_auto_print', v)} />
        <Toggle label="Print invoice when order is despatched" checked={s.invoice_print_on_despatch} onChange={v => set('invoice_print_on_despatch', v)} />
      </div>

      <div className="settings-section">
        <div className="settings-section-title">Stock Item Labels</div>
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16, maxWidth: 500 }}>
          <div className="settings-field">
            <label className="settings-label">Label Format</label>
            <select className="settings-select" value={s.stock_label_format} onChange={e => set('stock_label_format', e.target.value)}>
              <option value="A4">A4</option>
              <option value="A5">A5</option>
              <option value="label_4x6">4×6 Label</option>
            </select>
          </div>
        </div>
        <Toggle label="Auto-print stock item labels when stock is booked in" checked={s.stock_label_auto_print} onChange={v => set('stock_label_auto_print', v)} />
      </div>

      <div className="settings-section">
        <div className="settings-section-title">Shipping Label Sort Order</div>
        <div className="settings-field" style={{ maxWidth: 350 }}>
          <label className="settings-label">Sort shipping labels by</label>
          <select className="settings-select" value={s.shipping_label_sort} onChange={e => set('shipping_label_sort', e.target.value)}>
            <option value="order_date">Order Date</option>
            <option value="order_number">Order Number</option>
            <option value="channel">Channel</option>
            <option value="destination_country">Destination Country</option>
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

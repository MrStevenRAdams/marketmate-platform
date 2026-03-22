import { useState, useEffect } from 'react';
import { Link } from 'react-router-dom';
import { getActiveTenantId } from '../../contexts/TenantContext';
import '../../components/SettingsLayout.css';

const API = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';

function api(path: string, init?: RequestInit) {
  return fetch(`${API}${path}`, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      'X-Tenant-Id': getActiveTenantId(),
      ...(init?.headers || {}),
    },
  });
}

interface SMTPConfig {
  host: string;
  port: string;
  username: string;
  password: string;
  from_address: string;
  from_name: string;
  reply_to: string;
  tls: boolean;
}

const DEFAULT: SMTPConfig = {
  host: '', port: '587', username: '', password: '',
  from_address: '', from_name: '', reply_to: '', tls: true,
};

type Toast = { msg: string; type: 'success' | 'error' } | null;

export default function EmailSettings() {
  const [form, setForm] = useState<SMTPConfig>(DEFAULT);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [toast, setToast] = useState<Toast>(null);

  useEffect(() => {
    api('/settings/email').then(r => r.json()).then(d => {
      if (d.smtp_config) setForm({ ...DEFAULT, ...d.smtp_config });
    }).catch(() => {}).finally(() => setLoading(false));
  }, []);

  const showToast = (msg: string, type: 'success' | 'error') => {
    setToast({ msg, type });
    setTimeout(() => setToast(null), 3500);
  };

  const save = async () => {
    setSaving(true);
    try {
      const r = await api('/settings/email', {
        method: 'PUT',
        body: JSON.stringify({ smtp_config: form }),
      });
      if (!r.ok) throw new Error(await r.text());
      showToast('Email settings saved', 'success');
    } catch (e: any) {
      showToast(e.message || 'Save failed', 'error');
    } finally { setSaving(false); }
  };

  const test = async () => {
    setTesting(true);
    try {
      const r = await api('/settings/email/test', { method: 'POST', body: JSON.stringify({ smtp_config: form }) });
      const d = await r.json();
      if (!r.ok) throw new Error(d.error || 'Test failed');
      showToast('Test email sent successfully', 'success');
    } catch (e: any) {
      showToast(e.message || 'Test failed', 'error');
    } finally { setTesting(false); }
  };

  const set = (k: keyof SMTPConfig, v: string | boolean) =>
    setForm(f => ({ ...f, [k]: v }));

  if (loading) return <div className="settings-page"><div style={{ color: 'var(--text-muted)', padding: 40 }}>Loading…</div></div>;

  return (
    <div className="settings-page">
      <div className="settings-breadcrumb">
        <Link to="/settings">Settings</Link>
        <span className="settings-breadcrumb-sep">›</span>
        <span className="settings-breadcrumb-current">Email Settings</span>
      </div>

      <h1 className="settings-page-title">Email Settings</h1>
      <p className="settings-page-sub">Configure your outbound SMTP server for sending order confirmations, invoices and notifications.</p>

      <div className="settings-section">
        <div className="settings-section-title">SMTP Configuration</div>

        <div className="settings-input-row">
          <div className="settings-field">
            <label className="settings-label">SMTP Host</label>
            <input className="settings-input" placeholder="smtp.example.com" value={form.host} onChange={e => set('host', e.target.value)} />
          </div>
          <div className="settings-field">
            <label className="settings-label">Port</label>
            <input className="settings-input" placeholder="587" value={form.port} onChange={e => set('port', e.target.value)} />
          </div>
        </div>

        <div className="settings-input-row">
          <div className="settings-field">
            <label className="settings-label">Username</label>
            <input className="settings-input" placeholder="user@example.com" value={form.username} onChange={e => set('username', e.target.value)} />
          </div>
          <div className="settings-field">
            <label className="settings-label">Password</label>
            <input className="settings-input" type="password" placeholder="••••••••" value={form.password} onChange={e => set('password', e.target.value)} />
          </div>
        </div>

        <div className="settings-toggle-row">
          <div className="settings-toggle-info">
            <span className="settings-toggle-label">Enable TLS / STARTTLS</span>
            <span className="settings-toggle-desc">Recommended for all production SMTP connections</span>
          </div>
          <label className="settings-toggle">
            <input type="checkbox" checked={form.tls} onChange={e => set('tls', e.target.checked)} />
            <span className="settings-toggle-slider" />
          </label>
        </div>
      </div>

      <div className="settings-section">
        <div className="settings-section-title">From Address</div>

        <div className="settings-input-row">
          <div className="settings-field">
            <label className="settings-label">From Name</label>
            <input className="settings-input" placeholder="MarketMate Orders" value={form.from_name} onChange={e => set('from_name', e.target.value)} />
          </div>
          <div className="settings-field">
            <label className="settings-label">From Email Address</label>
            <input className="settings-input" placeholder="orders@yourstore.com" value={form.from_address} onChange={e => set('from_address', e.target.value)} />
          </div>
        </div>

        <div className="settings-input-row">
          <div className="settings-field">
            <label className="settings-label">Reply-To Address (optional)</label>
            <input className="settings-input" placeholder="support@yourstore.com" value={form.reply_to} onChange={e => set('reply_to', e.target.value)} />
          </div>
        </div>
      </div>

      <div className="settings-btn-row">
        <button className="settings-btn-primary" onClick={save} disabled={saving}>
          {saving ? 'Saving…' : 'Save Settings'}
        </button>
        <button className="settings-btn-secondary" onClick={test} disabled={testing || !form.host}>
          {testing ? 'Sending…' : '📧 Send Test Email'}
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

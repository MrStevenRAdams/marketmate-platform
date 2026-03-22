import { useState, useEffect } from 'react';
import { getActiveTenantId } from '../../contexts/TenantContext';
import '../../components/SettingsLayout.css';

const API = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';

function api(path: string, init?: RequestInit) {
  return fetch(`${API}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId(), ...(init?.headers || {}) },
  });
}

interface TaxSettings {
  vat_number: string;
  tax_region: string;
  default_tax_rate: number;
  tax_included: boolean;
}

const TAX_REGIONS = [
  { value: 'GB', label: '🇬🇧 United Kingdom' },
  { value: 'EU', label: '🇪🇺 European Union' },
  { value: 'US', label: '🇺🇸 United States' },
  { value: 'AU', label: '🇦🇺 Australia' },
  { value: 'OTHER', label: '🌍 Other' },
];

type Toast = { msg: string; type: 'success' | 'error' } | null;

export default function TaxSettings() {
  const [settings, setSettings] = useState<TaxSettings>({
    vat_number: '',
    tax_region: 'GB',
    default_tax_rate: 20,
    tax_included: true,
  });
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [toast, setToast] = useState<Toast>(null);
  const [dirty, setDirty] = useState(false);

  const showToast = (msg: string, type: 'success' | 'error') => {
    setToast({ msg, type });
    setTimeout(() => setToast(null), 3500);
  };

  useEffect(() => {
    api('/settings/tax')
      .then(r => r.json())
      .then(d => {
        const t = d.tax || {};
        setSettings({
          vat_number: t.vat_number || '',
          tax_region: t.tax_region || 'GB',
          default_tax_rate: t.default_tax_rate != null ? t.default_tax_rate * 100 : 20,
          tax_included: t.tax_included ?? true,
        });
      })
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  const update = <K extends keyof TaxSettings>(key: K, value: TaxSettings[K]) => {
    setSettings(s => ({ ...s, [key]: value }));
    setDirty(true);
  };

  const save = async () => {
    setSaving(true);
    try {
      const payload = {
        ...settings,
        default_tax_rate: settings.default_tax_rate / 100, // convert % to decimal
      };
      const res = await api('/settings/tax', { method: 'PUT', body: JSON.stringify(payload) });
      if (!res.ok) throw new Error('Save failed');
      showToast('Tax settings saved', 'success');
      setDirty(false);
    } catch {
      showToast('Failed to save settings', 'error');
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return <div className="settings-page"><div style={{ padding: 40, textAlign: 'center', color: 'var(--text-muted)' }}>Loading…</div></div>;
  }

  return (
    <div className="settings-page">
      {toast && (
        <div style={{
          position: 'fixed', top: 20, right: 20, zIndex: 9999,
          background: toast.type === 'success' ? '#22c55e' : '#ef4444',
          color: '#fff', borderRadius: 8, padding: '10px 18px', fontWeight: 600, fontSize: 14,
          boxShadow: '0 4px 16px rgba(0,0,0,0.2)',
        }}>
          {toast.type === 'success' ? '✓' : '✕'} {toast.msg}
        </div>
      )}

      <div className="settings-page-header">
        <h1 className="settings-page-title">Tax &amp; VAT Settings</h1>
        <p className="settings-page-sub">Configure VAT registration, tax region and default rates for your orders.</p>
      </div>

      <div style={{ maxWidth: 560 }}>
        {/* VAT Registration Number */}
        <div className="settings-section">
          <div className="settings-section-title">VAT Registration</div>
          <div className="settings-field">
            <label className="settings-label">VAT Registration Number</label>
            <input
              className="settings-input"
              type="text"
              placeholder="e.g. GB123456789"
              value={settings.vat_number}
              onChange={e => update('vat_number', e.target.value)}
              style={{ fontFamily: 'monospace', letterSpacing: '0.04em' }}
            />
            <div className="settings-hint">
              Enter your VAT registration number. This will appear on invoices and tax documents.
            </div>
          </div>
        </div>

        {/* Tax Region */}
        <div className="settings-section">
          <div className="settings-section-title">Tax Region</div>
          <div className="settings-field">
            <label className="settings-label">Region</label>
            <select
              className="settings-input"
              value={settings.tax_region}
              onChange={e => update('tax_region', e.target.value)}
            >
              {TAX_REGIONS.map(r => (
                <option key={r.value} value={r.value}>{r.label}</option>
              ))}
            </select>
            <div className="settings-hint">
              Determines which tax rules and rates apply to your orders.
            </div>
          </div>
        </div>

        {/* Default Tax Rate */}
        <div className="settings-section">
          <div className="settings-section-title">Default Tax Rate</div>
          <div className="settings-field">
            <label className="settings-label">Rate (%)</label>
            <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
              <input
                className="settings-input"
                type="number"
                min={0}
                max={100}
                step={0.1}
                value={settings.default_tax_rate}
                onChange={e => update('default_tax_rate', parseFloat(e.target.value) || 0)}
                style={{ width: 120 }}
              />
              <span style={{ fontSize: 14, color: 'var(--text-muted)' }}>%</span>
            </div>
            <div className="settings-hint">
              Used when no specific tax rate is set on an order line. E.g. 20 for standard UK VAT.
            </div>
          </div>

          {/* Tax Included Toggle */}
          <div className="settings-field" style={{ marginTop: 16 }}>
            <label className="settings-label">Prices include tax</label>
            <div
              style={{ display: 'flex', alignItems: 'center', gap: 12, cursor: 'pointer' }}
              onClick={() => update('tax_included', !settings.tax_included)}
            >
              <div style={{
                width: 44, height: 24, borderRadius: 12, position: 'relative', transition: 'background 0.2s',
                background: settings.tax_included ? 'var(--primary, #6366f1)' : 'var(--border)',
              }}>
                <div style={{
                  width: 18, height: 18, borderRadius: '50%', background: '#fff',
                  position: 'absolute', top: 3,
                  left: settings.tax_included ? 23 : 3,
                  transition: 'left 0.2s',
                  boxShadow: '0 1px 4px rgba(0,0,0,0.2)',
                }} />
              </div>
              <span style={{ fontSize: 14, color: 'var(--text-secondary)' }}>
                {settings.tax_included ? 'Tax is included in listed prices' : 'Tax is added on top of listed prices'}
              </span>
            </div>
            <div className="settings-hint">
              When enabled, the listed price already includes VAT. Affects how tax is calculated on order totals.
            </div>
          </div>
        </div>

        {/* Preview */}
        <div className="settings-section" style={{ background: 'rgba(99,102,241,0.04)', border: '1px solid rgba(99,102,241,0.15)', borderRadius: 10, padding: '14px 18px' }}>
          <div className="settings-section-title" style={{ marginBottom: 8 }}>Preview</div>
          <div style={{ fontSize: 13, lineHeight: 1.8, color: 'var(--text-secondary)' }}>
            <div><strong>VAT Number:</strong> {settings.vat_number || <span style={{ color: 'var(--text-muted)' }}>Not set</span>}</div>
            <div><strong>Region:</strong> {TAX_REGIONS.find(r => r.value === settings.tax_region)?.label}</div>
            <div><strong>Standard rate:</strong> {settings.default_tax_rate}%</div>
            <div><strong>Pricing:</strong> {settings.tax_included ? 'Tax-inclusive' : 'Tax-exclusive (tax added at checkout)'}</div>
          </div>
        </div>

        {/* Save button */}
        <div style={{ marginTop: 24, display: 'flex', gap: 12, alignItems: 'center' }}>
          <button
            className="btn-pri"
            onClick={save}
            disabled={saving || !dirty}
            style={{ minWidth: 120, opacity: !dirty ? 0.6 : 1 }}
          >
            {saving ? 'Saving…' : 'Save Settings'}
          </button>
          {!dirty && !saving && (
            <span style={{ fontSize: 13, color: 'var(--text-muted)' }}>✓ Up to date</span>
          )}
        </div>
      </div>
    </div>
  );
}

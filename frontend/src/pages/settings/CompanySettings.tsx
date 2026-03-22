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

type Toast = { msg: string; type: 'success' | 'error' } | null;

interface SellerProfile {
  name: string;
  address: string;
  phone: string;
  email: string;
  website: string;
  vat_number: string;
  // Regional
  default_warehouse_country: string;
  my_country: string;
  default_currency: string;
  weight_unit: string;
  dimension_unit: string;
  timezone: string;
  dst_enabled: boolean;
  date_format: string;
  tax_for_direct_orders: string;
  localise_currencies: boolean;
}

const COMMON_CURRENCIES = ['GBP','USD','EUR','CAD','AUD','JPY','CNY','CHF','SEK','NOK','DKK','PLN','INR','BRL','MXN','ZAR','SGD','HKD','NZD','AED'];
const TIMEZONES = ['Europe/London','Europe/Paris','Europe/Berlin','Europe/Amsterdam','America/New_York','America/Chicago','America/Denver','America/Los_Angeles','Asia/Tokyo','Asia/Shanghai','Asia/Singapore','Asia/Dubai','Australia/Sydney','Pacific/Auckland','UTC'];
const DATE_FORMATS = ['DD/MM/YYYY','MM/DD/YYYY','YYYY-MM-DD'];
const WEIGHT_UNITS = ['g','kg','oz','lbs'];
const DIM_UNITS = ['cm','inches'];
const TAX_DIRECT = ['include','exclude','none'];
const COUNTRIES = [
  ['GB','United Kingdom'],['US','United States'],['DE','Germany'],['FR','France'],['ES','Spain'],
  ['IT','Italy'],['NL','Netherlands'],['AU','Australia'],['CA','Canada'],['JP','Japan'],
  ['CN','China'],['IN','India'],['BR','Brazil'],['MX','Mexico'],['SG','Singapore'],
  ['AE','UAE'],['ZA','South Africa'],['NZ','New Zealand'],['SE','Sweden'],['NO','Norway'],
];

const blank: SellerProfile = {
  name: '', address: '', phone: '', email: '', website: '', vat_number: '',
  default_warehouse_country: '', my_country: '', default_currency: 'GBP',
  weight_unit: 'kg', dimension_unit: 'cm', timezone: 'Europe/London',
  dst_enabled: true, date_format: 'DD/MM/YYYY', tax_for_direct_orders: 'exclude',
  localise_currencies: false,
};

export default function CompanySettings() {
  const [profile, setProfile] = useState<SellerProfile>(blank);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [toast, setToast] = useState<Toast>(null);

  const showToast = (msg: string, type: 'success' | 'error') => {
    setToast({ msg, type });
    setTimeout(() => setToast(null), 3000);
  };

  useEffect(() => {
    api('/settings/seller').then(r => r.json()).then(d => {
      setProfile({ ...blank, ...d });
    }).catch(() => {}).finally(() => setLoading(false));
  }, []);

  const set = (key: keyof SellerProfile, value: string | boolean) =>
    setProfile(p => ({ ...p, [key]: value }));

  const save = async () => {
    setSaving(true);
    try {
      const r = await api('/settings/seller', { method: 'PUT', body: JSON.stringify(profile) });
      if (!r.ok) throw new Error(await r.text());
      showToast('Settings saved', 'success');
    } catch (e: any) {
      showToast(e.message || 'Save failed', 'error');
    } finally { setSaving(false); }
  };

  if (loading) return <div className="settings-page"><div style={{ color: 'var(--text-muted)', padding: 40 }}>Loading…</div></div>;

  return (
    <div className="settings-page">
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 4 }}>
        <Link to="/settings" style={{ color: 'var(--text-muted)', fontSize: 13, textDecoration: 'none' }}>← Settings</Link>
      </div>
      <h1 className="settings-page-title">Company Settings</h1>
      <p className="settings-page-sub">Your business details used on invoices, labels and correspondence.</p>

      {/* ── Company Info ── */}
      <div className="settings-section">
        <div className="settings-section-title">Company Information</div>

        <div className="settings-field">
          <label className="settings-label">Company Name</label>
          <input className="settings-input" value={profile.name} onChange={e => set('name', e.target.value)} placeholder="Acme Ltd" />
        </div>
        <div className="settings-field">
          <label className="settings-label">Address</label>
          <textarea className="settings-input" style={{ height: 80, resize: 'vertical' }} value={profile.address} onChange={e => set('address', e.target.value)} placeholder="123 High Street, London, EC1A 1BB" />
        </div>
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16 }}>
          <div className="settings-field">
            <label className="settings-label">Phone</label>
            <input className="settings-input" value={profile.phone} onChange={e => set('phone', e.target.value)} placeholder="+44 20 7946 0123" />
          </div>
          <div className="settings-field">
            <label className="settings-label">Email</label>
            <input className="settings-input" type="email" value={profile.email} onChange={e => set('email', e.target.value)} placeholder="hello@acme.com" />
          </div>
          <div className="settings-field">
            <label className="settings-label">Website</label>
            <input className="settings-input" value={profile.website} onChange={e => set('website', e.target.value)} placeholder="https://acme.com" />
          </div>
          <div className="settings-field">
            <label className="settings-label">VAT Registration Number</label>
            <input className="settings-input" value={profile.vat_number} onChange={e => set('vat_number', e.target.value)} placeholder="GB123456789" />
          </div>
        </div>
      </div>

      {/* ── Regional & Display Preferences ── */}
      <div className="settings-section">
        <div className="settings-section-title">Regional &amp; Display Preferences</div>

        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16 }}>
          <div className="settings-field">
            <label className="settings-label">My Country</label>
            <select className="settings-select" value={profile.my_country} onChange={e => set('my_country', e.target.value)}>
              <option value="">— Select country —</option>
              {COUNTRIES.map(([code, name]) => <option key={code} value={code}>{name} ({code})</option>)}
            </select>
          </div>
          <div className="settings-field">
            <label className="settings-label">Default Warehouse Country</label>
            <select className="settings-select" value={profile.default_warehouse_country} onChange={e => set('default_warehouse_country', e.target.value)}>
              <option value="">— Select country —</option>
              {COUNTRIES.map(([code, name]) => <option key={code} value={code}>{name} ({code})</option>)}
            </select>
          </div>
          <div className="settings-field">
            <label className="settings-label">Default Currency</label>
            <select className="settings-select" value={profile.default_currency} onChange={e => set('default_currency', e.target.value)}>
              {COMMON_CURRENCIES.map(c => <option key={c} value={c}>{c}</option>)}
            </select>
          </div>
          <div className="settings-field">
            <label className="settings-label">Timezone</label>
            <select className="settings-select" value={profile.timezone} onChange={e => set('timezone', e.target.value)}>
              {TIMEZONES.map(tz => <option key={tz} value={tz}>{tz}</option>)}
            </select>
          </div>
          <div className="settings-field">
            <label className="settings-label">Weight Unit</label>
            <select className="settings-select" value={profile.weight_unit} onChange={e => set('weight_unit', e.target.value)}>
              {WEIGHT_UNITS.map(u => <option key={u} value={u}>{u}</option>)}
            </select>
          </div>
          <div className="settings-field">
            <label className="settings-label">Dimension Unit</label>
            <select className="settings-select" value={profile.dimension_unit} onChange={e => set('dimension_unit', e.target.value)}>
              {DIM_UNITS.map(u => <option key={u} value={u}>{u}</option>)}
            </select>
          </div>
          <div className="settings-field">
            <label className="settings-label">Date Format</label>
            <select className="settings-select" value={profile.date_format} onChange={e => set('date_format', e.target.value)}>
              {DATE_FORMATS.map(f => <option key={f} value={f}>{f}</option>)}
            </select>
          </div>
          <div className="settings-field">
            <label className="settings-label">Tax on Direct Orders</label>
            <select className="settings-select" value={profile.tax_for_direct_orders} onChange={e => set('tax_for_direct_orders', e.target.value)}>
              {TAX_DIRECT.map(v => <option key={v} value={v}>{v.charAt(0).toUpperCase() + v.slice(1)}</option>)}
            </select>
          </div>
        </div>

        <div style={{ display: 'flex', gap: 32, marginTop: 8 }}>
          <label style={{ display: 'flex', alignItems: 'center', gap: 10, fontSize: 13, cursor: 'pointer' }}>
            <input type="checkbox" checked={profile.dst_enabled} onChange={e => set('dst_enabled', e.target.checked)} />
            Daylight Saving Time (DST) enabled
          </label>
          <label style={{ display: 'flex', alignItems: 'center', gap: 10, fontSize: 13, cursor: 'pointer' }}>
            <input type="checkbox" checked={profile.localise_currencies} onChange={e => set('localise_currencies', e.target.checked)} />
            Localise currencies for each channel
          </label>
        </div>
      </div>

      <div style={{ marginTop: 8 }}>
        <button className="settings-btn-primary" onClick={save} disabled={saving}>
          {saving ? 'Saving…' : 'Save Changes'}
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

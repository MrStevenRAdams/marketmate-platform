import { useState, useEffect } from 'react';
import { Link } from 'react-router-dom';
import { getActiveTenantId } from '../../contexts/TenantContext';
import '../../components/SettingsLayout.css';
import './CurrencySettings.css';

const API = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';

function api(path: string, init?: RequestInit) {
  return fetch(`${API}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId(), ...(init?.headers || {}) },
  });
}

interface CurrencyRate {
  id: string;
  from: string;
  to: string;
  rate: number;
  mode: 'manual' | 'auto';
  updated_at: string;
}

const COMMON_CURRENCIES = ['GBP', 'USD', 'EUR', 'CAD', 'AUD', 'JPY', 'CNY', 'CHF', 'SEK', 'NOK', 'DKK', 'PLN', 'SGD', 'HKD', 'NZD', 'MXN', 'BRL', 'INR', 'ZAR', 'AED'];

type Toast = { msg: string; type: 'success' | 'error' } | null;

export default function CurrencySettings() {
  const [rates, setRates] = useState<CurrencyRate[]>([]);
  const [loading, setLoading] = useState(true);
  const [toast, setToast] = useState<Toast>(null);
  const [showAdd, setShowAdd] = useState(false);
  const [newFrom, setNewFrom] = useState('GBP');
  const [newTo, setNewTo] = useState('USD');
  const [newRate, setNewRate] = useState('');
  const [saving, setSaving] = useState(false);
  const [deleting, setDeleting] = useState<string | null>(null);
  const [baseCurrency, setBaseCurrency] = useState('GBP');
  const [savingBase, setSavingBase] = useState(false);

  const showToast = (msg: string, type: 'success' | 'error') => {
    setToast({ msg, type });
    setTimeout(() => setToast(null), 3000);
  };

  const load = () => {
    Promise.all([
      api('/settings/currency').then(r => r.json()),
      api('/settings/seller').then(r => r.json()),
    ]).then(([currData, sellerData]) => {
      setRates(currData.rates || []);
      if (sellerData.base_currency) setBaseCurrency(sellerData.base_currency);
    }).catch(() => {}).finally(() => setLoading(false));
  };

  const saveBaseCurrency = async () => {
    setSavingBase(true);
    try {
      const r = await api('/settings/seller', {
        method: 'PUT',
        body: JSON.stringify({ base_currency: baseCurrency }),
      });
      if (!r.ok) throw new Error(await r.text());
      showToast('Base currency saved', 'success');
    } catch (e: any) {
      showToast(e.message || 'Save failed', 'error');
    } finally { setSavingBase(false); }
  };

  useEffect(() => { load(); }, []);

  const addRate = async () => {
    if (!newRate || isNaN(parseFloat(newRate))) return;
    setSaving(true);
    try {
      const r = await api('/settings/currency', {
        method: 'POST',
        body: JSON.stringify({ from: newFrom, to: newTo, rate: parseFloat(newRate) }),
      });
      if (!r.ok) throw new Error(await r.text());
      showToast('Rate saved', 'success');
      setShowAdd(false);
      setNewRate('');
      load();
    } catch (e: any) {
      showToast(e.message || 'Save failed', 'error');
    } finally { setSaving(false); }
  };

  const deleteRate = async (id: string) => {
    setDeleting(id);
    try {
      await api(`/settings/currency/${id}`, { method: 'DELETE' });
      setRates(r => r.filter(x => x.id !== id));
    } finally { setDeleting(null); }
  };

  if (loading) return <div className="settings-page"><div style={{ color: 'var(--text-muted)', padding: 40 }}>Loading…</div></div>;

  return (
    <div className="settings-page">
      <div className="settings-breadcrumb">
        <Link to="/settings">Settings</Link>
        <span className="settings-breadcrumb-sep">›</span>
        <span className="settings-breadcrumb-current">Currency Exchange Rates</span>
      </div>

      <h1 className="settings-page-title">Currency Exchange Rates</h1>
      <p className="settings-page-sub">Configure your base currency and exchange rates used for multi-currency pricing and reporting.</p>

      {/* Base Currency */}
      <div className="settings-section" style={{ marginBottom: 24 }}>
        <div className="settings-section-title">Base Currency</div>
        <p style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 16, lineHeight: 1.6 }}>
          The primary currency for all pricing, reporting, and conversion calculations.
        </p>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          <select
            className="settings-select"
            value={baseCurrency}
            onChange={e => setBaseCurrency(e.target.value)}
            style={{ width: 200 }}
          >
            {COMMON_CURRENCIES.map(c => <option key={c} value={c}>{c}</option>)}
          </select>
          <button className="settings-btn-primary" onClick={saveBaseCurrency} disabled={savingBase}>
            {savingBase ? '…' : 'Save Base Currency'}
          </button>
        </div>
        <p style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 10 }}>
          Exchange rates below are shown relative to <strong>{baseCurrency}</strong>.
        </p>
      </div>

      <div className="settings-section">
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
          <div className="settings-section-title" style={{ marginBottom: 0 }}>Exchange Rates</div>
          <button className="settings-btn-primary" onClick={() => setShowAdd(s => !s)} style={{ padding: '7px 14px' }}>
            + Add Rate
          </button>
        </div>

        {showAdd && (
          <div className="currency-add-form">
            <select className="settings-select" value={newFrom} onChange={e => setNewFrom(e.target.value)}>
              {COMMON_CURRENCIES.map(c => <option key={c} value={c}>{c}</option>)}
            </select>
            <span className="currency-arrow">→</span>
            <select className="settings-select" value={newTo} onChange={e => setNewTo(e.target.value)}>
              {COMMON_CURRENCIES.map(c => <option key={c} value={c}>{c}</option>)}
            </select>
            <input
              className="settings-input"
              style={{ width: 120 }}
              type="number"
              step="0.0001"
              placeholder="1.2500"
              value={newRate}
              onChange={e => setNewRate(e.target.value)}
            />
            <button className="settings-btn-primary" onClick={addRate} disabled={saving || !newRate}>
              {saving ? '…' : 'Save'}
            </button>
            <button className="settings-btn-secondary" onClick={() => { setShowAdd(false); setNewRate(''); }}>
              Cancel
            </button>
          </div>
        )}

        {rates.length === 0 ? (
          <div className="currency-empty">No rates configured. Add a rate above.</div>
        ) : (
          <table className="currency-table">
            <thead>
              <tr>
                <th>Pair</th>
                <th>Rate</th>
                <th>Mode</th>
                <th>Last Updated</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {rates.map(rate => (
                <tr key={rate.id}>
                  <td><span className="currency-pair">{rate.from} → {rate.to}</span></td>
                  <td><span className="currency-rate">{rate.rate.toFixed(4)}</span></td>
                  <td>
                    <span className={`currency-badge ${rate.mode}`}>
                      {rate.mode === 'auto' ? '⚡ Auto' : '✏️ Manual'}
                    </span>
                  </td>
                  <td style={{ color: 'var(--text-muted)', fontSize: 12 }}>
                    {new Date(rate.updated_at).toLocaleDateString()}
                  </td>
                  <td>
                    <button
                      className="currency-delete-btn"
                      onClick={() => deleteRate(rate.id)}
                      disabled={deleting === rate.id}
                    >
                      {deleting === rate.id ? '…' : '✕'}
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      <div className="settings-section" style={{ opacity: 0.7 }}>
        <div className="settings-section-title">Automatic Rate Feeds</div>
        <p style={{ fontSize: 13, color: 'var(--text-muted)', lineHeight: 1.6 }}>
          Automatic daily exchange rate feeds from Open Exchange Rates are available as a subscription upgrade.
          Rates are updated every 24 hours and applied across all pricing calculations automatically.
        </p>
        <button className="settings-btn-secondary" style={{ marginTop: 12 }} disabled>
          Upgrade to unlock auto rates →
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

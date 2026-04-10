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

interface TaxRegion { id: string; name: string; tax_rate: number; effective_from: string; }
interface TaxRateHistory { rate: number; effective_from: string; changed_by: string; }
interface Country {
  id: string; name: string; iso_code: string; default_tax_rate: number;
  regions: TaxRegion[]; tax_rate_history: TaxRateHistory[];
}
type Toast = { msg: string; type: 'success' | 'error' } | null;

const blankCountry: Country = { id: '', name: '', iso_code: '', default_tax_rate: 0, regions: [], tax_rate_history: [] };
const blankRegion: TaxRegion = { id: '', name: '', tax_rate: 0, effective_from: '' };

export default function CountriesTax() {
  const [countries, setCountries] = useState<Country[]>([]);
  const [loading, setLoading] = useState(true);
  const [toast, setToast] = useState<Toast>(null);
  const [expanded, setExpanded] = useState<string | null>(null);
  const [showModal, setShowModal] = useState(false);
  const [editCountry, setEditCountry] = useState<Country | null>(null);
  const [showRegionModal, setShowRegionModal] = useState<string | null>(null);
  const [editRegion, setEditRegion] = useState<TaxRegion | null>(null);
  const [deleting, setDeleting] = useState<string | null>(null);

  const showToast = (msg: string, type: 'success' | 'error') => {
    setToast({ msg, type }); setTimeout(() => setToast(null), 3000);
  };

  const load = () => {
    api('/settings/countries').then(r => r.json()).then(d => setCountries(d.countries || []))
      .catch(() => {}).finally(() => setLoading(false));
  };
  useEffect(() => { load(); }, []);

  const openAdd = () => { setEditCountry({ ...blankCountry }); setShowModal(true); };
  const openEdit = (c: Country) => { setEditCountry({ ...c }); setShowModal(true); };

  const saveCountry = async () => {
    if (!editCountry) return;
    try {
      const isNew = !editCountry.id;
      const r = isNew
        ? await api('/settings/countries', { method: 'POST', body: JSON.stringify(editCountry) })
        : await api(`/settings/countries/${editCountry.id}`, { method: 'PUT', body: JSON.stringify(editCountry) });
      if (!r.ok) throw new Error(await r.text());
      showToast(isNew ? 'Country added' : 'Country updated', 'success');
      setShowModal(false); load();
    } catch (e: any) { showToast(e.message || 'Save failed', 'error'); }
  };

  const deleteCountry = async (id: string) => {
    setDeleting(id);
    try {
      await api(`/settings/countries/${id}`, { method: 'DELETE' });
      setCountries(cs => cs.filter(c => c.id !== id));
    } finally { setDeleting(null); }
  };

  const saveRegion = async () => {
    if (!editRegion || !showRegionModal) return;
    const country = countries.find(c => c.id === showRegionModal);
    if (!country) return;
    const existing = editRegion.id
      ? country.regions.map(r => r.id === editRegion.id ? editRegion : r)
      : [...country.regions, { ...editRegion, id: 'rg_' + Date.now().toString(36) }];
    try {
      const r = await api(`/settings/countries/${showRegionModal}`, {
        method: 'PUT', body: JSON.stringify({ ...country, regions: existing })
      });
      if (!r.ok) throw new Error(await r.text());
      showToast('Region saved', 'success');
      setShowRegionModal(null); load();
    } catch (e: any) { showToast(e.message || 'Save failed', 'error'); }
  };

  const deleteRegion = async (countryId: string, regionId: string) => {
    const country = countries.find(c => c.id === countryId);
    if (!country) return;
    const updated = country.regions.filter(r => r.id !== regionId);
    await api(`/settings/countries/${countryId}`, {
      method: 'PUT', body: JSON.stringify({ ...country, regions: updated })
    });
    load();
  };

  if (loading) return <div className="settings-page"><div style={{ color: 'var(--text-muted)', padding: 40 }}>Loading…</div></div>;

  return (
    <div className="settings-page">
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 4 }}>
        <Link to="/settings" style={{ color: 'var(--text-muted)', fontSize: 13, textDecoration: 'none' }}>← Settings</Link>
      </div>
      <h1 className="settings-page-title">Countries &amp; Tax Rates</h1>
      <p className="settings-page-sub">Configure country-level tax rates and regional tax rules.</p>

      <div className="settings-section">
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
          <div className="settings-section-title" style={{ marginBottom: 0 }}>Configured Countries</div>
          <button className="settings-btn-primary" onClick={openAdd} style={{ padding: '7px 14px' }}>+ Add Country</button>
        </div>

        {countries.length === 0 ? (
          <div style={{ color: 'var(--text-muted)', fontSize: 13, padding: '24px 0' }}>No countries configured yet.</div>
        ) : (
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
            <thead>
              <tr>
                {['Name','ISO Code','Default Tax Rate','Actions'].map(h => (
                  <th key={h} style={{ textAlign: 'left', padding: '0 12px 10px', fontSize: 11, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.06em', color: 'var(--text-muted)', borderBottom: '1px solid var(--border)' }}>{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {countries.map(c => (
                <>
                  <tr key={c.id} style={{ cursor: 'pointer' }} onClick={() => setExpanded(expanded === c.id ? null : c.id)}>
                    <td style={{ padding: 12, borderBottom: '1px solid var(--border)', fontWeight: 600 }}>
                      {expanded === c.id ? '▾ ' : '▸ '}{c.name}
                    </td>
                    <td style={{ padding: 12, borderBottom: '1px solid var(--border)' }}>
                      <span style={{ background: 'var(--bg-tertiary)', padding: '2px 8px', borderRadius: 4, fontFamily: 'monospace', fontSize: 12 }}>{c.iso_code}</span>
                    </td>
                    <td style={{ padding: 12, borderBottom: '1px solid var(--border)' }}>{c.default_tax_rate}%</td>
                    <td style={{ padding: 12, borderBottom: '1px solid var(--border)' }}>
                      <button className="settings-btn-secondary" style={{ padding: '4px 10px', marginRight: 6, fontSize: 12 }} onClick={e => { e.stopPropagation(); openEdit(c); }}>Edit</button>
                      <button style={{ padding: '4px 10px', fontSize: 12, background: 'rgba(239,68,68,0.1)', color: '#ef4444', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 6, cursor: 'pointer' }}
                        onClick={e => { e.stopPropagation(); deleteCountry(c.id); }} disabled={deleting === c.id}>
                        {deleting === c.id ? '…' : 'Delete'}
                      </button>
                    </td>
                  </tr>
                  {expanded === c.id && (
                    <tr key={c.id + '-exp'}>
                      <td colSpan={4} style={{ padding: '0 16px 16px', background: 'var(--bg-secondary)', borderBottom: '1px solid var(--border)' }}>
                        {/* Regions */}
                        <div style={{ marginTop: 16 }}>
                          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 10 }}>
                            <strong style={{ fontSize: 13 }}>Regions</strong>
                            <button className="settings-btn-secondary" style={{ fontSize: 12, padding: '4px 10px' }}
                              onClick={() => { setEditRegion({ ...blankRegion }); setShowRegionModal(c.id); }}>+ Add Region</button>
                          </div>
                          {c.regions.length === 0 ? (
                            <div style={{ color: 'var(--text-muted)', fontSize: 12 }}>No regions configured.</div>
                          ) : (
                            <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12 }}>
                              <thead>
                                <tr>{['Region','Tax Rate','Effective From',''].map(h => <th key={h} style={{ textAlign: 'left', padding: '4px 8px', color: 'var(--text-muted)', fontWeight: 600 }}>{h}</th>)}</tr>
                              </thead>
                              <tbody>
                                {c.regions.map(r => (
                                  <tr key={r.id}>
                                    <td style={{ padding: '6px 8px' }}>{r.name}</td>
                                    <td style={{ padding: '6px 8px' }}>{r.tax_rate}%</td>
                                    <td style={{ padding: '6px 8px', color: 'var(--text-muted)' }}>{r.effective_from || '—'}</td>
                                    <td style={{ padding: '6px 8px' }}>
                                      <button style={{ fontSize: 11, padding: '2px 8px', marginRight: 4, cursor: 'pointer', background: 'var(--bg-tertiary)', border: '1px solid var(--border)', borderRadius: 4, color: 'var(--text-secondary)' }}
                                        onClick={() => { setEditRegion(r); setShowRegionModal(c.id); }}>Edit</button>
                                      <button style={{ fontSize: 11, padding: '2px 8px', cursor: 'pointer', background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 4, color: '#ef4444' }}
                                        onClick={() => deleteRegion(c.id, r.id)}>Remove</button>
                                    </td>
                                  </tr>
                                ))}
                              </tbody>
                            </table>
                          )}
                        </div>
                        {/* History */}
                        {c.tax_rate_history.length > 0 && (
                          <div style={{ marginTop: 16 }}>
                            <strong style={{ fontSize: 13 }}>Rate History</strong>
                            <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12, marginTop: 8 }}>
                              <thead>
                                <tr>{['Rate','Effective From','Changed By'].map(h => <th key={h} style={{ textAlign: 'left', padding: '4px 8px', color: 'var(--text-muted)' }}>{h}</th>)}</tr>
                              </thead>
                              <tbody>
                                {c.tax_rate_history.map((h, i) => (
                                  <tr key={i}>
                                    <td style={{ padding: '6px 8px' }}>{h.rate}%</td>
                                    <td style={{ padding: '6px 8px', color: 'var(--text-muted)' }}>{h.effective_from}</td>
                                    <td style={{ padding: '6px 8px', color: 'var(--text-muted)' }}>{h.changed_by || '—'}</td>
                                  </tr>
                                ))}
                              </tbody>
                            </table>
                          </div>
                        )}
                      </td>
                    </tr>
                  )}
                </>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {/* Country Modal */}
      {showModal && editCountry && (
        <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.5)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 }}
          onClick={e => e.target === e.currentTarget && setShowModal(false)}>
          <div style={{ background: 'var(--bg-primary)', borderRadius: 12, padding: 28, width: 460, boxShadow: '0 20px 60px rgba(0,0,0,0.4)' }}>
            <h3 style={{ margin: '0 0 20px', fontSize: 16, fontWeight: 700 }}>{editCountry.id ? 'Edit Country' : 'Add Country'}</h3>
            <div className="settings-field">
              <label className="settings-label">Country Name</label>
              <input className="settings-input" value={editCountry.name} onChange={e => setEditCountry(c => ({ ...c!, name: e.target.value }))} placeholder="United Kingdom" />
            </div>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16 }}>
              <div className="settings-field">
                <label className="settings-label">ISO Code</label>
                <input className="settings-input" value={editCountry.iso_code} onChange={e => setEditCountry(c => ({ ...c!, iso_code: e.target.value.toUpperCase() }))} placeholder="GB" maxLength={2} />
              </div>
              <div className="settings-field">
                <label className="settings-label">Default Tax Rate (%)</label>
                <input className="settings-input" type="number" step="0.01" min="0" max="100" value={editCountry.default_tax_rate} onChange={e => setEditCountry(c => ({ ...c!, default_tax_rate: parseFloat(e.target.value) || 0 }))} />
              </div>
            </div>
            <div style={{ display: 'flex', gap: 10, justifyContent: 'flex-end', marginTop: 20 }}>
              <button className="settings-btn-secondary" onClick={() => setShowModal(false)}>Cancel</button>
              <button className="settings-btn-primary" onClick={saveCountry}>Save</button>
            </div>
          </div>
        </div>
      )}

      {/* Region Modal */}
      {showRegionModal && editRegion && (
        <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.5)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1001 }}
          onClick={e => e.target === e.currentTarget && setShowRegionModal(null)}>
          <div style={{ background: 'var(--bg-primary)', borderRadius: 12, padding: 28, width: 420, boxShadow: '0 20px 60px rgba(0,0,0,0.4)' }}>
            <h3 style={{ margin: '0 0 20px', fontSize: 16, fontWeight: 700 }}>{editRegion.id ? 'Edit Region' : 'Add Region'}</h3>
            <div className="settings-field">
              <label className="settings-label">Region Name</label>
              <input className="settings-input" value={editRegion.name} onChange={e => setEditRegion(r => ({ ...r!, name: e.target.value }))} placeholder="England" />
            </div>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16 }}>
              <div className="settings-field">
                <label className="settings-label">Tax Rate (%)</label>
                <input className="settings-input" type="number" step="0.01" value={editRegion.tax_rate} onChange={e => setEditRegion(r => ({ ...r!, tax_rate: parseFloat(e.target.value) || 0 }))} />
              </div>
              <div className="settings-field">
                <label className="settings-label">Effective From</label>
                <input className="settings-input" type="date" value={editRegion.effective_from} onChange={e => setEditRegion(r => ({ ...r!, effective_from: e.target.value }))} />
              </div>
            </div>
            <div style={{ display: 'flex', gap: 10, justifyContent: 'flex-end', marginTop: 20 }}>
              <button className="settings-btn-secondary" onClick={() => setShowRegionModal(null)}>Cancel</button>
              <button className="settings-btn-primary" onClick={saveRegion}>Save Region</button>
            </div>
          </div>
        </div>
      )}

      {toast && (
        <div className={`settings-toast ${toast.type}`}>{toast.type === 'success' ? '✓' : '✗'} {toast.msg}</div>
      )}
    </div>
  );
}

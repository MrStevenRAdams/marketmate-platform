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

interface BinType {
  id: string; name: string; standard_type: string; colour: string;
  volumetric_tracking: boolean; default_stock_availability: string; bound_location: string;
}
type Toast = { msg: string; type: 'success' | 'error' } | null;

const blank: BinType = {
  id: '', name: '', standard_type: 'Standard', colour: '#607D8B',
  volumetric_tracking: false, default_stock_availability: 'unchanged', bound_location: '',
};

const STANDARD_TYPES = ['Standard','Oversize','Refrigerated','Hazardous','Valuable'];
const AVAILABILITY = ['available','restricted','unchanged'];
const PRESET_COLOURS = ['#607D8B','#4CAF50','#2196F3','#FF9800','#F44336','#9C27B0','#00BCD4','#FF5722'];

export default function BinTypes() {
  const [types, setTypes] = useState<BinType[]>([]);
  const [loading, setLoading] = useState(true);
  const [toast, setToast] = useState<Toast>(null);
  const [showModal, setShowModal] = useState(false);
  const [editing, setEditing] = useState<BinType | null>(null);
  const [deleting, setDeleting] = useState<string | null>(null);

  const showToast = (msg: string, type: 'success' | 'error') => {
    setToast({ msg, type }); setTimeout(() => setToast(null), 3000);
  };

  const load = () => {
    api('/settings/bin-types').then(r => r.json()).then(d => setTypes(d.bin_types || []))
      .catch(() => {}).finally(() => setLoading(false));
  };
  useEffect(() => { load(); }, []);

  const openAdd = () => { setEditing({ ...blank }); setShowModal(true); };
  const openEdit = (bt: BinType) => { setEditing({ ...bt }); setShowModal(true); };

  const save = async () => {
    if (!editing) return;
    try {
      const isNew = !editing.id;
      const r = isNew
        ? await api('/settings/bin-types', { method: 'POST', body: JSON.stringify(editing) })
        : await api(`/settings/bin-types/${editing.id}`, { method: 'PUT', body: JSON.stringify(editing) });
      if (!r.ok) throw new Error(await r.text());
      showToast(isNew ? 'Bin type created' : 'Bin type updated', 'success');
      setShowModal(false); load();
    } catch (e: any) { showToast(e.message || 'Save failed', 'error'); }
  };

  const deleteBT = async (id: string) => {
    setDeleting(id);
    try {
      await api(`/settings/bin-types/${id}`, { method: 'DELETE' });
      setTypes(ts => ts.filter(t => t.id !== id));
    } finally { setDeleting(null); }
  };

  if (loading) return <div className="settings-page"><div style={{ color: 'var(--text-muted)', padding: 40 }}>Loading…</div></div>;

  return (
    <div className="settings-page">
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 4 }}>
        <Link to="/settings" style={{ color: 'var(--text-muted)', fontSize: 13, textDecoration: 'none' }}>← Settings</Link>
      </div>
      <h1 className="settings-page-title">Bin Types</h1>
      <p className="settings-page-sub">Define the types of storage locations in your warehouse.</p>

      <div className="settings-section">
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
          <div className="settings-section-title" style={{ marginBottom: 0 }}>Bin Types</div>
          <button className="settings-btn-primary" onClick={openAdd} style={{ padding: '7px 14px' }}>+ New Bin Type</button>
        </div>

        {types.length === 0 ? (
          <div style={{ color: 'var(--text-muted)', fontSize: 13, padding: '24px 0' }}>No bin types configured. Add one to get started.</div>
        ) : (
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
            <thead>
              <tr>{['','Name','Standard Type','Availability','Volume Tracking','Actions'].map(h => (
                <th key={h} style={{ textAlign: 'left', padding: '0 12px 10px', fontSize: 11, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.06em', color: 'var(--text-muted)', borderBottom: '1px solid var(--border)' }}>{h}</th>
              ))}</tr>
            </thead>
            <tbody>
              {types.map(bt => (
                <tr key={bt.id}>
                  <td style={{ padding: '10px 12px', borderBottom: '1px solid var(--border)' }}>
                    <span style={{ display: 'inline-block', width: 18, height: 18, borderRadius: 4, background: bt.colour }} />
                  </td>
                  <td style={{ padding: '10px 12px', borderBottom: '1px solid var(--border)', fontWeight: 600 }}>{bt.name}</td>
                  <td style={{ padding: '10px 12px', borderBottom: '1px solid var(--border)' }}>{bt.standard_type}</td>
                  <td style={{ padding: '10px 12px', borderBottom: '1px solid var(--border)', textTransform: 'capitalize' }}>{bt.default_stock_availability}</td>
                  <td style={{ padding: '10px 12px', borderBottom: '1px solid var(--border)' }}>{bt.volumetric_tracking ? '✓' : '—'}</td>
                  <td style={{ padding: '10px 12px', borderBottom: '1px solid var(--border)' }}>
                    <button className="settings-btn-secondary" style={{ padding: '4px 10px', marginRight: 6, fontSize: 12 }} onClick={() => openEdit(bt)}>Edit</button>
                    <button style={{ padding: '4px 10px', fontSize: 12, background: 'rgba(239,68,68,0.1)', color: '#ef4444', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 6, cursor: 'pointer' }}
                      onClick={() => deleteBT(bt.id)} disabled={deleting === bt.id}>{deleting === bt.id ? '…' : 'Delete'}</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {/* Modal */}
      {showModal && editing && (
        <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.5)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 }}
          onClick={e => e.target === e.currentTarget && setShowModal(false)}>
          <div style={{ background: 'var(--bg-primary)', borderRadius: 12, padding: 28, width: 500, boxShadow: '0 20px 60px rgba(0,0,0,0.4)' }}>
            <h3 style={{ margin: '0 0 20px', fontSize: 16, fontWeight: 700 }}>{editing.id ? 'Edit Bin Type' : 'New Bin Type'}</h3>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16 }}>
              <div className="settings-field" style={{ gridColumn: '1 / -1' }}>
                <label className="settings-label">Name</label>
                <input className="settings-input" value={editing.name} onChange={e => setEditing(b => ({ ...b!, name: e.target.value }))} placeholder="e.g. Cold Storage" />
              </div>
              <div className="settings-field">
                <label className="settings-label">Standard Type</label>
                <select className="settings-select" value={editing.standard_type} onChange={e => setEditing(b => ({ ...b!, standard_type: e.target.value }))}>
                  {STANDARD_TYPES.map(t => <option key={t} value={t}>{t}</option>)}
                </select>
              </div>
              <div className="settings-field">
                <label className="settings-label">Default Stock Availability</label>
                <select className="settings-select" value={editing.default_stock_availability} onChange={e => setEditing(b => ({ ...b!, default_stock_availability: e.target.value }))}>
                  {AVAILABILITY.map(a => <option key={a} value={a}>{a.charAt(0).toUpperCase() + a.slice(1)}</option>)}
                </select>
              </div>
              <div className="settings-field" style={{ gridColumn: '1 / -1' }}>
                <label className="settings-label">Badge Colour</label>
                <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', marginBottom: 8 }}>
                  {PRESET_COLOURS.map(c => (
                    <button key={c} onClick={() => setEditing(b => ({ ...b!, colour: c }))}
                      style={{ width: 28, height: 28, borderRadius: 6, background: c, border: editing.colour === c ? '2px solid white' : '2px solid transparent', cursor: 'pointer', outline: editing.colour === c ? '2px solid var(--primary)' : 'none' }} />
                  ))}
                  <input type="color" value={editing.colour} onChange={e => setEditing(b => ({ ...b!, colour: e.target.value }))} style={{ width: 28, height: 28, padding: 0, border: 'none', borderRadius: 6, cursor: 'pointer' }} />
                </div>
              </div>
              <div className="settings-field" style={{ gridColumn: '1 / -1' }}>
                <label className="settings-label">Bound Location (optional)</label>
                <input className="settings-input" value={editing.bound_location} onChange={e => setEditing(b => ({ ...b!, bound_location: e.target.value }))} placeholder="Location ID" />
              </div>
            </div>
            <label style={{ display: 'flex', alignItems: 'center', gap: 10, fontSize: 13, cursor: 'pointer', marginBottom: 20 }}>
              <input type="checkbox" checked={editing.volumetric_tracking} onChange={e => setEditing(b => ({ ...b!, volumetric_tracking: e.target.checked }))} />
              Enable volumetric tracking for this bin type
            </label>
            <div style={{ display: 'flex', gap: 10, justifyContent: 'flex-end' }}>
              <button className="settings-btn-secondary" onClick={() => setShowModal(false)}>Cancel</button>
              <button className="settings-btn-primary" onClick={save}>Save Bin Type</button>
            </div>
          </div>
        </div>
      )}

      {toast && <div className={`settings-toast ${toast.type}`}>{toast.type === 'success' ? '✓' : '✗'} {toast.msg}</div>}
    </div>
  );
}

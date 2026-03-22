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

interface Schedule {
  id: string; name: string; schedule_type: string; timezone: string;
  days_of_week: number[]; time_of_day: string; interval_minutes: number;
  run_at: string; enabled: boolean; action: string;
}
type Toast = { msg: string; type: 'success' | 'error' } | null;

const SCHEDULE_TYPES = ['daily','weekly','monthly','interval','one_time'];
const TIMEZONES = ['Europe/London','Europe/Paris','Europe/Berlin','America/New_York','America/Los_Angeles','UTC'];
const DAYS = ['Sun','Mon','Tue','Wed','Thu','Fri','Sat'];

const blank: Schedule = {
  id: '', name: '', schedule_type: 'daily', timezone: 'UTC',
  days_of_week: [], time_of_day: '09:00', interval_minutes: 60,
  run_at: '', enabled: true, action: '',
};

function nextRunLabel(s: Schedule): string {
  if (!s.enabled) return 'Disabled';
  if (s.schedule_type === 'one_time') return s.run_at || '—';
  if (s.schedule_type === 'interval') return `Every ${s.interval_minutes}m`;
  if (s.time_of_day) return `${s.time_of_day} ${s.timezone}`;
  return '—';
}

export default function Schedules() {
  const [schedules, setSchedules] = useState<Schedule[]>([]);
  const [loading, setLoading] = useState(true);
  const [toast, setToast] = useState<Toast>(null);
  const [showModal, setShowModal] = useState(false);
  const [editing, setEditing] = useState<Schedule | null>(null);
  const [deleting, setDeleting] = useState<string | null>(null);

  const showToast = (msg: string, type: 'success' | 'error') => {
    setToast({ msg, type }); setTimeout(() => setToast(null), 3000);
  };

  const load = () => {
    api('/schedules').then(r => r.json()).then(d => setSchedules(d.schedules || []))
      .catch(() => {}).finally(() => setLoading(false));
  };
  useEffect(() => { load(); }, []);

  const openAdd = () => { setEditing({ ...blank }); setShowModal(true); };
  const openEdit = (s: Schedule) => { setEditing({ ...s }); setShowModal(true); };

  const save = async () => {
    if (!editing) return;
    try {
      const isNew = !editing.id;
      const r = isNew
        ? await api('/schedules', { method: 'POST', body: JSON.stringify(editing) })
        : await api(`/schedules/${editing.id}`, { method: 'PUT', body: JSON.stringify(editing) });
      if (!r.ok) throw new Error(await r.text());
      showToast(isNew ? 'Schedule created' : 'Schedule updated', 'success');
      setShowModal(false); load();
    } catch (e: any) { showToast(e.message || 'Save failed', 'error'); }
  };

  const deleteSchedule = async (id: string) => {
    setDeleting(id);
    try {
      await api(`/schedules/${id}`, { method: 'DELETE' });
      setSchedules(ss => ss.filter(s => s.id !== id));
    } finally { setDeleting(null); }
  };

  const toggleDay = (day: number) => {
    if (!editing) return;
    const days = editing.days_of_week.includes(day)
      ? editing.days_of_week.filter(d => d !== day)
      : [...editing.days_of_week, day];
    setEditing(e => ({ ...e!, days_of_week: days }));
  };

  if (loading) return <div className="settings-page"><div style={{ color: 'var(--text-muted)', padding: 40 }}>Loading…</div></div>;

  const typeLabel = (t: string) => ({ daily: 'Daily', weekly: 'Weekly', monthly: 'Monthly', interval: 'Interval', one_time: 'One Time' }[t] || t);

  return (
    <div className="settings-page">
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 4 }}>
        <Link to="/settings" style={{ color: 'var(--text-muted)', fontSize: 13, textDecoration: 'none' }}>← Settings</Link>
      </div>
      <h1 className="settings-page-title">Schedules</h1>
      <p className="settings-page-sub">Configure automated scheduled tasks and recurring actions.</p>

      <div className="settings-section">
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
          <div className="settings-section-title" style={{ marginBottom: 0 }}>Configured Schedules</div>
          <button className="settings-btn-primary" onClick={openAdd} style={{ padding: '7px 14px' }}>+ New Schedule</button>
        </div>

        {schedules.length === 0 ? (
          <div style={{ color: 'var(--text-muted)', fontSize: 13, padding: '24px 0' }}>No schedules configured yet.</div>
        ) : (
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
            <thead>
              <tr>{['Name','Type','Next Run / Frequency','Status','Actions'].map(h => (
                <th key={h} style={{ textAlign: 'left', padding: '0 12px 10px', fontSize: 11, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.06em', color: 'var(--text-muted)', borderBottom: '1px solid var(--border)' }}>{h}</th>
              ))}</tr>
            </thead>
            <tbody>
              {schedules.map(s => (
                <tr key={s.id}>
                  <td style={{ padding: '12px', borderBottom: '1px solid var(--border)', fontWeight: 600 }}>{s.name}</td>
                  <td style={{ padding: '12px', borderBottom: '1px solid var(--border)' }}>
                    <span style={{ background: 'var(--bg-tertiary)', padding: '2px 8px', borderRadius: 4, fontSize: 11, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.04em' }}>{typeLabel(s.schedule_type)}</span>
                  </td>
                  <td style={{ padding: '12px', borderBottom: '1px solid var(--border)', color: 'var(--text-muted)' }}>{nextRunLabel(s)}</td>
                  <td style={{ padding: '12px', borderBottom: '1px solid var(--border)' }}>
                    <span style={{ color: s.enabled ? '#4CAF50' : '#9e9e9e', fontSize: 12, fontWeight: 600 }}>
                      {s.enabled ? '● Active' : '○ Disabled'}
                    </span>
                  </td>
                  <td style={{ padding: '12px', borderBottom: '1px solid var(--border)' }}>
                    <button className="settings-btn-secondary" style={{ padding: '4px 10px', marginRight: 6, fontSize: 12 }} onClick={() => openEdit(s)}>Edit</button>
                    <button style={{ padding: '4px 10px', fontSize: 12, background: 'rgba(239,68,68,0.1)', color: '#ef4444', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 6, cursor: 'pointer' }}
                      onClick={() => deleteSchedule(s.id)} disabled={deleting === s.id}>{deleting === s.id ? '…' : 'Delete'}</button>
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
          <div style={{ background: 'var(--bg-primary)', borderRadius: 12, padding: 28, width: 520, maxHeight: '90vh', overflowY: 'auto', boxShadow: '0 20px 60px rgba(0,0,0,0.4)' }}>
            <h3 style={{ margin: '0 0 20px', fontSize: 16, fontWeight: 700 }}>{editing.id ? 'Edit Schedule' : 'New Schedule'}</h3>

            <div className="settings-field">
              <label className="settings-label">Schedule Name</label>
              <input className="settings-input" value={editing.name} onChange={e => setEditing(s => ({ ...s!, name: e.target.value }))} placeholder="e.g. Daily Inventory Sync" />
            </div>

            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16 }}>
              <div className="settings-field">
                <label className="settings-label">Schedule Type</label>
                <select className="settings-select" value={editing.schedule_type} onChange={e => setEditing(s => ({ ...s!, schedule_type: e.target.value }))}>
                  {SCHEDULE_TYPES.map(t => <option key={t} value={t}>{typeLabel(t)}</option>)}
                </select>
              </div>
              <div className="settings-field">
                <label className="settings-label">Timezone</label>
                <select className="settings-select" value={editing.timezone} onChange={e => setEditing(s => ({ ...s!, timezone: e.target.value }))}>
                  {TIMEZONES.map(tz => <option key={tz} value={tz}>{tz}</option>)}
                </select>
              </div>
            </div>

            {/* Conditional fields based on type */}
            {['daily','weekly','monthly'].includes(editing.schedule_type) && (
              <div className="settings-field">
                <label className="settings-label">Time of Day (HH:MM)</label>
                <input className="settings-input" type="time" value={editing.time_of_day} onChange={e => setEditing(s => ({ ...s!, time_of_day: e.target.value }))} />
              </div>
            )}

            {editing.schedule_type === 'weekly' && (
              <div className="settings-field">
                <label className="settings-label">Days of Week</label>
                <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', marginTop: 6 }}>
                  {DAYS.map((day, i) => (
                    <button key={i} onClick={() => toggleDay(i)}
                      style={{ padding: '6px 12px', fontSize: 12, borderRadius: 6, cursor: 'pointer',
                        background: editing.days_of_week.includes(i) ? 'var(--primary, #7c3aed)' : 'var(--bg-tertiary)',
                        color: editing.days_of_week.includes(i) ? 'white' : 'var(--text-secondary)',
                        border: '1px solid var(--border)', fontWeight: 600 }}>
                      {day}
                    </button>
                  ))}
                </div>
              </div>
            )}

            {editing.schedule_type === 'interval' && (
              <div className="settings-field">
                <label className="settings-label">Interval (minutes)</label>
                <input className="settings-input" type="number" min={1} value={editing.interval_minutes} onChange={e => setEditing(s => ({ ...s!, interval_minutes: parseInt(e.target.value) || 60 }))} />
              </div>
            )}

            {editing.schedule_type === 'one_time' && (
              <div className="settings-field">
                <label className="settings-label">Run At (ISO 8601 datetime)</label>
                <input className="settings-input" type="datetime-local" value={editing.run_at} onChange={e => setEditing(s => ({ ...s!, run_at: e.target.value }))} />
              </div>
            )}

            <div className="settings-field">
              <label className="settings-label">Action</label>
              <input className="settings-input" value={editing.action} onChange={e => setEditing(s => ({ ...s!, action: e.target.value }))} placeholder="e.g. sync_inventory, send_report" />
            </div>

            <label style={{ display: 'flex', alignItems: 'center', gap: 10, fontSize: 13, cursor: 'pointer', marginBottom: 20 }}>
              <input type="checkbox" checked={editing.enabled} onChange={e => setEditing(s => ({ ...s!, enabled: e.target.checked }))} />
              Schedule is active
            </label>

            <div style={{ display: 'flex', gap: 10, justifyContent: 'flex-end' }}>
              <button className="settings-btn-secondary" onClick={() => setShowModal(false)}>Cancel</button>
              <button className="settings-btn-primary" onClick={save}>Save Schedule</button>
            </div>
          </div>
        </div>
      )}

      {toast && <div className={`settings-toast ${toast.type}`}>{toast.type === 'success' ? '✓' : '✗'} {toast.msg}</div>}
    </div>
  );
}

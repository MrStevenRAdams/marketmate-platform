import { useState, useEffect, useCallback } from 'react';
import { getActiveTenantId } from '../contexts/TenantContext';

const API_BASE = import.meta.env.VITE_API_URL ?? 'http://localhost:8080/api/v1';
function api(path: string, init?: RequestInit) {
  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId(), ...init?.headers },
  });
}

interface StorageGroup {
  group_id: string;
  name: string;
  description: string;
  created_at?: string;
}

export default function StorageGroups() {
  const [groups, setGroups] = useState<StorageGroup[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [editing, setEditing] = useState<StorageGroup | null>(null); // null = no modal, {} = new
  const [saving, setSaving] = useState(false);
  const [deleteConfirm, setDeleteConfirm] = useState<string | null>(null);

  const [formName, setFormName] = useState('');
  const [formDesc, setFormDesc] = useState('');
  const [formError, setFormError] = useState('');

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const res = await api('/storage-groups');
      if (res.ok) {
        const d = await res.json();
        setGroups(d.groups || []);
      } else {
        setError('Failed to load storage groups');
      }
    } catch {
      setError('Network error');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { load(); }, [load]);

  const openNew = () => {
    setFormName('');
    setFormDesc('');
    setFormError('');
    setEditing({ group_id: '', name: '', description: '' });
  };

  const openEdit = (g: StorageGroup) => {
    setFormName(g.name);
    setFormDesc(g.description);
    setFormError('');
    setEditing(g);
  };

  const handleSave = async () => {
    if (!formName.trim()) { setFormError('Name is required'); return; }
    setSaving(true);
    setFormError('');
    try {
      const isNew = !editing?.group_id;
      const res = await api(isNew ? '/storage-groups' : `/storage-groups/${editing!.group_id}`, {
        method: isNew ? 'POST' : 'PUT',
        body: JSON.stringify({ name: formName.trim(), description: formDesc.trim() }),
      });
      if (res.ok) {
        setEditing(null);
        load();
      } else {
        const d = await res.json().catch(() => ({}));
        setFormError(d.error || 'Failed to save');
      }
    } catch {
      setFormError('Network error');
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async (groupId: string) => {
    try {
      const res = await api(`/storage-groups/${groupId}`, { method: 'DELETE' });
      if (res.ok) {
        setDeleteConfirm(null);
        load();
      }
    } catch { /* ignore */ }
  };

  const STORAGE_ICONS: Record<string, string> = {
    Ambient: '🌡️',
    Chilled: '❄️',
    Frozen: '🧊',
    'High Value': '💎',
    'Short Life': '⏰',
    Hazardous: '⚠️',
  };

  return (
    <div style={{ padding: '24px 32px', maxWidth: 900, margin: '0 auto' }}>
      <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', marginBottom: 28 }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 24, fontWeight: 700, color: 'var(--text-primary)' }}>Storage Groups</h1>
          <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 14 }}>
            Define storage categories for warehouse products (e.g. Ambient, Chilled, Hazardous).
          </p>
        </div>
        <button style={btnPrimaryStyle} onClick={openNew}>+ New Group</button>
      </div>

      {error && (
        <div style={{ padding: '10px 14px', background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 6, color: 'var(--danger)', fontSize: 13, marginBottom: 16 }}>
          {error}
        </div>
      )}

      {loading ? (
        <div style={{ padding: 48, textAlign: 'center', color: 'var(--text-muted)' }}>Loading…</div>
      ) : (
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(260px, 1fr))', gap: 12 }}>
          {groups.map(g => (
            <div key={g.group_id} style={{
              background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 10,
              padding: '16px 20px', display: 'flex', flexDirection: 'column', gap: 6,
            }}>
              <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between' }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                  <span style={{ fontSize: 20 }}>{STORAGE_ICONS[g.name] || '📦'}</span>
                  <span style={{ fontWeight: 600, color: 'var(--text-primary)', fontSize: 15 }}>{g.name}</span>
                </div>
                <div style={{ display: 'flex', gap: 4 }}>
                  <button style={iconBtnStyle} onClick={() => openEdit(g)} title="Edit">✏️</button>
                  <button style={iconBtnStyle} onClick={() => setDeleteConfirm(g.group_id)} title="Delete">🗑️</button>
                </div>
              </div>
              {g.description && (
                <p style={{ margin: 0, fontSize: 13, color: 'var(--text-muted)', paddingLeft: 28 }}>{g.description}</p>
              )}
              <div style={{ fontSize: 11, color: 'var(--text-muted)', paddingLeft: 28, marginTop: 4 }}>
                ID: {g.group_id}
              </div>
            </div>
          ))}
          {groups.length === 0 && (
            <div style={{ gridColumn: '1 / -1', padding: '48px 32px', textAlign: 'center', color: 'var(--text-muted)', background: 'var(--bg-secondary)', borderRadius: 12, border: '1px solid var(--border)' }}>
              <div style={{ fontSize: 40, marginBottom: 12 }}>📦</div>
              <p>No storage groups yet. Click "+ New Group" to create one.</p>
              <p style={{ fontSize: 13 }}>Common examples: Ambient, Chilled, Frozen, High Value, Hazardous</p>
            </div>
          )}
        </div>
      )}

      {/* Create / Edit Modal */}
      {editing !== null && (
        <div style={overlayStyle}>
          <div style={{ ...modalStyle, width: 440 }}>
            <div style={modalHeaderStyle}>
              <h3 style={modalTitleStyle}>{editing.group_id ? 'Edit Storage Group' : 'New Storage Group'}</h3>
              <button style={closeBtnStyle} onClick={() => setEditing(null)}>✕</button>
            </div>
            <div style={{ padding: '20px 24px' }}>
              <div style={fieldStyle}>
                <label style={labelStyle}>Name <span style={{ color: 'var(--danger)' }}>*</span></label>
                <input
                  style={inputStyle}
                  placeholder="e.g. Chilled, High Value, Hazardous…"
                  value={formName}
                  onChange={e => setFormName(e.target.value)}
                  autoFocus
                />
              </div>
              <div style={fieldStyle}>
                <label style={labelStyle}>Description</label>
                <input
                  style={inputStyle}
                  placeholder="Optional description…"
                  value={formDesc}
                  onChange={e => setFormDesc(e.target.value)}
                />
              </div>
              {formError && (
                <p style={{ margin: '12px 0 0', fontSize: 13, color: 'var(--danger)' }}>{formError}</p>
              )}
            </div>
            <div style={modalFooterStyle}>
              <button style={btnGhostStyle} onClick={() => setEditing(null)}>Cancel</button>
              <button style={btnPrimaryStyle} onClick={handleSave} disabled={saving}>
                {saving ? 'Saving…' : (editing.group_id ? 'Save Changes' : 'Create Group')}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Delete confirm */}
      {deleteConfirm && (
        <div style={overlayStyle}>
          <div style={{ ...modalStyle, width: 380 }}>
            <div style={modalHeaderStyle}>
              <h3 style={modalTitleStyle}>Delete Storage Group?</h3>
              <button style={closeBtnStyle} onClick={() => setDeleteConfirm(null)}>✕</button>
            </div>
            <div style={{ padding: '16px 24px' }}>
              <p style={{ margin: 0, color: 'var(--text-secondary)', fontSize: 14 }}>
                This will delete the storage group. Products already assigned to this group will not be affected.
              </p>
            </div>
            <div style={modalFooterStyle}>
              <button style={btnGhostStyle} onClick={() => setDeleteConfirm(null)}>Cancel</button>
              <button style={{ ...btnPrimaryStyle, background: 'var(--danger)' }} onClick={() => handleDelete(deleteConfirm)}>
                Delete
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

// ─── Styles ───────────────────────────────────────────────────────────────────
const overlayStyle: React.CSSProperties = { position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.6)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 };
const modalStyle: React.CSSProperties = { background: 'var(--bg-secondary)', borderRadius: 12, border: '1px solid var(--border)', maxWidth: '95vw' };
const modalHeaderStyle: React.CSSProperties = { display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '18px 24px', borderBottom: '1px solid var(--border)' };
const modalTitleStyle: React.CSSProperties = { margin: 0, fontSize: 17, fontWeight: 600, color: 'var(--text-primary)' };
const modalFooterStyle: React.CSSProperties = { display: 'flex', justifyContent: 'flex-end', gap: 8, padding: '14px 24px', borderTop: '1px solid var(--border)' };
const closeBtnStyle: React.CSSProperties = { background: 'none', border: 'none', color: 'var(--text-muted)', cursor: 'pointer', fontSize: 18 };
const fieldStyle: React.CSSProperties = { marginTop: 16 };
const labelStyle: React.CSSProperties = { display: 'block', marginBottom: 6, fontSize: 13, fontWeight: 500, color: 'var(--text-secondary)' };
const inputStyle: React.CSSProperties = { width: '100%', padding: '8px 12px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', fontSize: 14, outline: 'none', boxSizing: 'border-box' };
const btnPrimaryStyle: React.CSSProperties = { padding: '8px 16px', background: 'var(--primary)', color: 'white', border: 'none', borderRadius: 6, cursor: 'pointer', fontSize: 13, fontWeight: 600 };
const btnGhostStyle: React.CSSProperties = { padding: '8px 16px', background: 'transparent', color: 'var(--text-secondary)', border: '1px solid var(--border)', borderRadius: 6, cursor: 'pointer', fontSize: 13 };
const iconBtnStyle: React.CSSProperties = { background: 'none', border: 'none', cursor: 'pointer', padding: '2px 4px', fontSize: 14, opacity: 0.7 };

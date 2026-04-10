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

interface TagDefinition {
  tag_id: string;
  name: string;
  color: string;
  shape: string;
  is_default?: boolean;
}

const SHAPES = ['square', 'circle', 'triangle', 'star', 'diamond', 'flag'] as const;
type Shape = typeof SHAPES[number];

const PRESET_COLORS = [
  '#3b82f6', '#22c55e', '#f59e0b', '#a855f7', '#ef4444', '#14b8a6',
  '#f97316', '#ec4899', '#64748b', '#0ea5e9', '#84cc16', '#dc2626',
];

function TagShape({ shape, color, size = 20 }: { shape: string; color: string; size?: number }) {
  const s = size;
  const c = color;
  switch (shape) {
    case 'circle':
      return <svg width={s} height={s}><circle cx={s/2} cy={s/2} r={s/2 - 1} fill={c} /></svg>;
    case 'triangle':
      return <svg width={s} height={s}><polygon points={`${s/2},1 ${s-1},${s-1} 1,${s-1}`} fill={c} /></svg>;
    case 'star': {
      const cx = s / 2, cy = s / 2, r1 = s / 2 - 1, r2 = r1 * 0.42;
      const pts = Array.from({ length: 10 }, (_, i) => {
        const angle = (Math.PI * i) / 5 - Math.PI / 2;
        const r = i % 2 === 0 ? r1 : r2;
        return `${cx + r * Math.cos(angle)},${cy + r * Math.sin(angle)}`;
      }).join(' ');
      return <svg width={s} height={s}><polygon points={pts} fill={c} /></svg>;
    }
    case 'diamond':
      return <svg width={s} height={s}><polygon points={`${s/2},1 ${s-1},${s/2} ${s/2},${s-1} 1,${s/2}`} fill={c} /></svg>;
    case 'flag':
      return <svg width={s} height={s}><rect x={1} y={1} width={4} height={s-2} fill={c} /><polygon points={`5,1 ${s-1},${s/3} 5,${s*2/3}`} fill={c} /></svg>;
    default: // square
      return <svg width={s} height={s}><rect x={1} y={1} width={s-2} height={s-2} rx={2} fill={c} /></svg>;
  }
}

type Toast = { msg: string; type: 'success' | 'error' } | null;

export default function OrderTagsSettings() {
  const [tags, setTags] = useState<TagDefinition[]>([]);
  const [loading, setLoading] = useState(true);
  const [toast, setToast] = useState<Toast>(null);
  const [editing, setEditing] = useState<string | null>(null); // tag_id being edited
  const [showCreate, setShowCreate] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null);

  // Form state for new/edit
  const [formName, setFormName] = useState('');
  const [formColor, setFormColor] = useState('#3b82f6');
  const [formShape, setFormShape] = useState<Shape>('square');
  const [formCustomColor, setFormCustomColor] = useState('');
  const [saving, setSaving] = useState(false);

  const showToast = (msg: string, type: 'success' | 'error') => {
    setToast({ msg, type });
    setTimeout(() => setToast(null), 3500);
  };

  const load = () => {
    api('/settings/order-tags')
      .then(r => r.json())
      .then(d => setTags(d.tags || []))
      .catch(() => {})
      .finally(() => setLoading(false));
  };

  useEffect(() => { load(); }, []);

  const startEdit = (tag: TagDefinition) => {
    setEditing(tag.tag_id);
    setFormName(tag.name);
    setFormColor(tag.color);
    setFormShape((tag.shape as Shape) || 'square');
    setFormCustomColor(PRESET_COLORS.includes(tag.color) ? '' : tag.color);
    setShowCreate(false);
  };

  const startCreate = () => {
    setEditing(null);
    setFormName('');
    setFormColor('#3b82f6');
    setFormShape('square');
    setFormCustomColor('');
    setShowCreate(true);
  };

  const cancelForm = () => {
    setEditing(null);
    setShowCreate(false);
  };

  const activeColor = formCustomColor || formColor;

  const saveCreate = async () => {
    if (!formName.trim()) return;
    setSaving(true);
    try {
      const res = await api('/settings/order-tags', {
        method: 'POST',
        body: JSON.stringify({ name: formName.trim(), color: activeColor, shape: formShape }),
      });
      if (!res.ok) throw new Error('Create failed');
      showToast('Tag created', 'success');
      setShowCreate(false);
      load();
    } catch {
      showToast('Failed to create tag', 'error');
    } finally { setSaving(false); }
  };

  const saveEdit = async (tagId: string) => {
    if (!formName.trim()) return;
    setSaving(true);
    try {
      const res = await api(`/settings/order-tags/${tagId}`, {
        method: 'PUT',
        body: JSON.stringify({ name: formName.trim(), color: activeColor, shape: formShape }),
      });
      if (!res.ok) throw new Error('Update failed');
      showToast('Tag updated', 'success');
      setEditing(null);
      load();
    } catch {
      showToast('Failed to update tag', 'error');
    } finally { setSaving(false); }
  };

  const deleteTag = async (tagId: string) => {
    try {
      const res = await api(`/settings/order-tags/${tagId}`, { method: 'DELETE' });
      if (!res.ok) throw new Error('Delete failed');
      showToast('Tag deleted', 'success');
      setConfirmDelete(null);
      load();
    } catch {
      showToast('Failed to delete tag', 'error');
    }
  };

  const FormPanel = ({ tagId }: { tagId?: string }) => (
    <div style={{ background: 'var(--bg-secondary)', borderRadius: 10, border: '1px solid var(--border)', padding: 18, marginTop: 8, marginBottom: 8 }}>
      <div style={{ display: 'flex', gap: 16, flexWrap: 'wrap', alignItems: 'flex-start' }}>
        {/* Preview */}
        <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 6, paddingTop: 4 }}>
          <TagShape shape={formShape} color={activeColor} size={36} />
          <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>Preview</span>
        </div>

        {/* Name */}
        <div style={{ flex: '1 1 160px' }}>
          <label style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', display: 'block', marginBottom: 4 }}>Tag Name</label>
          <input
            className="settings-input"
            type="text"
            placeholder="e.g. Priority, VIP…"
            value={formName}
            onChange={e => setFormName(e.target.value)}
            autoFocus
          />
        </div>

        {/* Shape */}
        <div>
          <label style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', display: 'block', marginBottom: 4 }}>Shape</label>
          <div style={{ display: 'flex', gap: 6 }}>
            {SHAPES.map(s => (
              <button
                key={s}
                title={s}
                onClick={() => setFormShape(s)}
                style={{
                  width: 34, height: 34, display: 'flex', alignItems: 'center', justifyContent: 'center',
                  border: formShape === s ? '2px solid var(--primary, #6366f1)' : '1px solid var(--border)',
                  borderRadius: 6, background: formShape === s ? 'rgba(99,102,241,0.1)' : 'var(--bg-tertiary)',
                  cursor: 'pointer',
                }}
              >
                <TagShape shape={s} color={formShape === s ? (activeColor || '#6366f1') : 'var(--text-muted)'} size={18} />
              </button>
            ))}
          </div>
        </div>

        {/* Color */}
        <div>
          <label style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', display: 'block', marginBottom: 4 }}>Colour</label>
          <div style={{ display: 'flex', gap: 5, flexWrap: 'wrap', maxWidth: 180 }}>
            {PRESET_COLORS.map(c => (
              <button
                key={c}
                onClick={() => { setFormColor(c); setFormCustomColor(''); }}
                style={{
                  width: 22, height: 22, borderRadius: '50%', background: c, border: 'none',
                  cursor: 'pointer',
                  outline: (formCustomColor ? false : formColor === c) ? `3px solid ${c}` : '2px solid transparent',
                  outlineOffset: 2,
                }}
              />
            ))}
          </div>
          <div style={{ marginTop: 8, display: 'flex', gap: 8, alignItems: 'center' }}>
            <input
              type="color"
              value={formCustomColor || formColor}
              onChange={e => setFormCustomColor(e.target.value)}
              style={{ width: 28, height: 28, border: 'none', background: 'none', cursor: 'pointer', padding: 0 }}
            />
            <input
              className="settings-input"
              type="text"
              placeholder="#hex"
              value={formCustomColor}
              onChange={e => setFormCustomColor(e.target.value)}
              style={{ width: 90, fontFamily: 'monospace', fontSize: 12 }}
            />
          </div>
        </div>
      </div>

      <div style={{ marginTop: 16, display: 'flex', gap: 10 }}>
        <button
          className="btn-pri"
          onClick={() => tagId ? saveEdit(tagId) : saveCreate()}
          disabled={saving || !formName.trim()}
          style={{ minWidth: 90 }}
        >
          {saving ? 'Saving…' : tagId ? 'Save Changes' : 'Create Tag'}
        </button>
        <button className="btn-sec" onClick={cancelForm}>Cancel</button>
      </div>
    </div>
  );

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
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
          <div>
            <h1 className="settings-page-title">Order Tags</h1>
            <p className="settings-page-sub">Create and manage coloured shape tags that can be assigned to orders for quick visual identification.</p>
          </div>
          {!showCreate && (
            <button className="btn-pri" onClick={startCreate} style={{ marginTop: 4 }}>+ New Tag</button>
          )}
        </div>
      </div>

      {showCreate && <FormPanel />}

      {loading ? (
        <div style={{ padding: 40, textAlign: 'center', color: 'var(--text-muted)' }}>Loading tags…</div>
      ) : tags.length === 0 ? (
        <div style={{ textAlign: 'center', padding: '40px 20px', color: 'var(--text-muted)' }}>
          <div style={{ fontSize: 40, marginBottom: 12 }}>🏷️</div>
          <div style={{ fontWeight: 600, marginBottom: 6 }}>No tags yet</div>
          <div style={{ fontSize: 13 }}>Create your first tag to start labelling orders.</div>
        </div>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 0 }}>
          {tags.map((tag, i) => (
            <div key={tag.tag_id}>
              {/* Confirm delete banner */}
              {confirmDelete === tag.tag_id && (
                <div style={{ background: 'rgba(239,68,68,0.08)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 8, padding: '12px 16px', marginBottom: 4, display: 'flex', alignItems: 'center', gap: 16 }}>
                  <span style={{ flex: 1, fontSize: 13, color: 'var(--text-secondary)' }}>
                    ⚠️ Delete <strong>{tag.name}</strong>? This will remove this tag from all orders.
                  </span>
                  <button onClick={() => deleteTag(tag.tag_id)} style={{ padding: '6px 14px', background: '#ef4444', color: '#fff', border: 'none', borderRadius: 6, cursor: 'pointer', fontWeight: 600, fontSize: 13 }}>Delete</button>
                  <button onClick={() => setConfirmDelete(null)} style={{ padding: '6px 14px', background: 'var(--bg-secondary)', color: 'var(--text-secondary)', border: '1px solid var(--border)', borderRadius: 6, cursor: 'pointer', fontSize: 13 }}>Cancel</button>
                </div>
              )}

              <div style={{
                display: 'flex', alignItems: 'center', gap: 14, padding: '14px 16px',
                background: editing === tag.tag_id ? 'rgba(99,102,241,0.04)' : (i % 2 === 0 ? 'var(--bg-secondary)' : 'transparent'),
                borderRadius: editing === tag.tag_id ? '8px 8px 0 0' : 8,
                border: editing === tag.tag_id ? '1px solid rgba(99,102,241,0.3)' : '1px solid transparent',
                borderBottom: editing === tag.tag_id ? 'none' : undefined,
              }}>
                <TagShape shape={tag.shape} color={tag.color} size={28} />
                <div style={{ flex: 1 }}>
                  <div style={{ fontWeight: 600, fontSize: 14 }}>{tag.name}</div>
                  <div style={{ fontSize: 12, color: 'var(--text-muted)', textTransform: 'capitalize' }}>
                    {tag.shape} · <span style={{ fontFamily: 'monospace' }}>{tag.color}</span>
                    {tag.is_default && <span style={{ marginLeft: 8, background: 'var(--bg-tertiary)', border: '1px solid var(--border)', borderRadius: 4, padding: '1px 6px', fontSize: 11 }}>default</span>}
                  </div>
                </div>
                <button
                  className="btn-sec"
                  style={{ fontSize: 12, padding: '5px 12px' }}
                  onClick={() => editing === tag.tag_id ? cancelForm() : startEdit(tag)}
                >
                  {editing === tag.tag_id ? 'Cancel' : '✏️ Edit'}
                </button>
                {!tag.is_default && (
                  <button
                    style={{ padding: '5px 10px', fontSize: 12, background: 'none', border: '1px solid rgba(239,68,68,0.4)', color: '#ef4444', borderRadius: 6, cursor: 'pointer' }}
                    onClick={() => setConfirmDelete(confirmDelete === tag.tag_id ? null : tag.tag_id)}
                  >
                    🗑
                  </button>
                )}
              </div>

              {editing === tag.tag_id && <FormPanel tagId={tag.tag_id} />}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

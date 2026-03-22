import { useState, useEffect } from 'react';
import { getActiveTenantId } from '../contexts/TenantContext';

const API = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';
function api(path: string, init?: RequestInit) {
  return fetch(`${API}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId(), ...(init?.headers || {}) },
  });
}

// ─── Types ────────────────────────────────────────────────────────────────────
interface PickwaveLine {
  id: string; order_id: string; sku: string; product_name: string;
  quantity: number; binrack_id: string; binrack_name: string;
  status: 'pending' | 'picked' | 'short'; picked_quantity: number;
}
interface Pickwave {
  id: string; name: string; status: string; type: string; grouping: string;
  assigned_user_id: string; max_orders: number; max_items: number;
  sort_by: string; show_next_only: boolean; order_ids: string[];
  order_count: number; item_count: number; lines?: PickwaveLine[];
  created_at: string; updated_at: string;
}

type Toast = { msg: string; type: 'success' | 'error' } | null;
type ViewMode = 'list' | 'kanban';

const STATUS_COLOURS: Record<string, string> = {
  draft: '#9e9e9e', in_progress: '#2196F3', complete: '#4CAF50',
  despatched: '#7c3aed', cancelled: '#ef4444',
  open: '#2196F3', picking: '#FF9800',
};
const KANBAN_COLS = ['draft','in_progress','complete','despatched','cancelled'];

// ─── Generate Dialog ──────────────────────────────────────────────────────────
function GeneratePickwaveDialog({ onClose, onCreated, preselectedOrders }: {
  onClose: () => void; onCreated: () => void; preselectedOrders?: string[];
}) {
  const [orders, setOrders] = useState<any[]>([]);
  const [selected, setSelected] = useState<string[]>(preselectedOrders || []);
  const [name, setName] = useState('');
  const [type, setType] = useState('multi_sku');
  const [grouping, setGrouping] = useState('single_order');
  const [sortBy, setSortBy] = useState('sku');
  const [maxOrders, setMaxOrders] = useState(0);
  const [maxItems, setMaxItems] = useState(0);
  const [showNextOnly, setShowNextOnly] = useState(false);
  const [assignedUser, setAssignedUser] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    api('/orders?status=open&limit=100').then(r => r.json()).then(d => {
      setOrders(d.orders || d.data || []);
    }).catch(() => {});
  }, []);

  const toggleOrder = (id: string) =>
    setSelected(s => s.includes(id) ? s.filter(x => x !== id) : [...s, id]);

  const submit = async () => {
    if (selected.length === 0) { setError('Select at least one order'); return; }
    setSaving(true); setError('');
    try {
      const r = await api('/pickwaves', {
        method: 'POST',
        body: JSON.stringify({
          order_ids: selected, name, type, grouping, sort_by: sortBy,
          max_orders: maxOrders, max_items: maxItems,
          show_next_only: showNextOnly, assigned_user_id: assignedUser,
        }),
      });
      if (!r.ok) throw new Error(await r.text());
      onCreated();
    } catch (e: any) { setError(e.message || 'Failed to create pickwave'); }
    finally { setSaving(false); }
  };

  const overlay: React.CSSProperties = { position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.5)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 };
  const modal: React.CSSProperties = { background: 'var(--bg-primary)', borderRadius: 12, padding: 28, width: 640, maxHeight: '90vh', overflowY: 'auto', boxShadow: '0 20px 60px rgba(0,0,0,0.4)' };
  const label: React.CSSProperties = { fontSize: 11, fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.06em', color: 'var(--text-muted)', display: 'block', marginBottom: 6 };
  const input: React.CSSProperties = { width: '100%', padding: '8px 12px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-primary)', fontSize: 13, marginBottom: 16 };

  return (
    <div style={overlay} onClick={e => e.target === e.currentTarget && onClose()}>
      <div style={modal}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 24 }}>
          <h3 style={{ margin: 0, fontSize: 18, fontWeight: 700 }}>Generate Pickwave</h3>
          <button onClick={onClose} style={{ background: 'none', border: 'none', fontSize: 20, cursor: 'pointer', color: 'var(--text-muted)' }}>×</button>
        </div>

        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16, marginBottom: 4 }}>
          <div>
            <label style={label}>Pickwave Name (optional)</label>
            <input style={input} value={name} onChange={e => setName(e.target.value)} placeholder="Auto-generated if blank" />
          </div>
          <div>
            <label style={label}>Assigned User</label>
            <input style={input} value={assignedUser} onChange={e => setAssignedUser(e.target.value)} placeholder="User ID or name" />
          </div>
          <div>
            <label style={label}>Pickwave Type</label>
            <select style={input} value={type} onChange={e => setType(e.target.value)}>
              <option value="single_sku">Single SKU</option>
              <option value="multi_sku">Multi-SKU</option>
              <option value="tote">Tote</option>
              <option value="tray">Tray</option>
            </select>
          </div>
          <div>
            <label style={label}>Grouping</label>
            <select style={input} value={grouping} onChange={e => setGrouping(e.target.value)}>
              <option value="single_order">Single Order</option>
              <option value="by_sku">By SKU</option>
              <option value="by_location">By Location</option>
              <option value="by_folder">By Folder</option>
            </select>
          </div>
          <div>
            <label style={label}>Sort By</label>
            <select style={input} value={sortBy} onChange={e => setSortBy(e.target.value)}>
              <option value="sku">SKU</option>
              <option value="binrack">Binrack</option>
              <option value="alphabetical">Alphabetical</option>
            </select>
          </div>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 }}>
            <div>
              <label style={label}>Max Orders (0=∞)</label>
              <input style={input} type="number" min={0} value={maxOrders} onChange={e => setMaxOrders(parseInt(e.target.value) || 0)} />
            </div>
            <div>
              <label style={label}>Max Items (0=∞)</label>
              <input style={input} type="number" min={0} value={maxItems} onChange={e => setMaxItems(parseInt(e.target.value) || 0)} />
            </div>
          </div>
        </div>

        <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13, cursor: 'pointer', marginBottom: 20 }}>
          <input type="checkbox" checked={showNextOnly} onChange={e => setShowNextOnly(e.target.checked)} />
          Show Next Only (hide already-picked lines)
        </label>

        <div>
          <label style={label}>Orders to Include ({selected.length} selected)</label>
          <div style={{ border: '1px solid var(--border)', borderRadius: 8, maxHeight: 200, overflowY: 'auto', background: 'var(--bg-secondary)' }}>
            {orders.length === 0 ? (
              <div style={{ padding: 16, color: 'var(--text-muted)', fontSize: 13, textAlign: 'center' }}>No open orders found</div>
            ) : orders.map((o: any) => (
              <label key={o.id || o.order_id} style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '10px 14px', borderBottom: '1px solid var(--border)', cursor: 'pointer', fontSize: 13 }}>
                <input type="checkbox"
                  checked={selected.includes(o.id || o.order_id)}
                  onChange={() => toggleOrder(o.id || o.order_id)} />
                <span style={{ fontWeight: 600 }}>{o.id || o.order_id}</span>
                <span style={{ color: 'var(--text-muted)' }}>{o.customer_name || o.channel || ''}</span>
              </label>
            ))}
          </div>
        </div>

        {error && <div style={{ marginTop: 12, padding: '10px 14px', background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 8, color: '#ef4444', fontSize: 13 }}>{error}</div>}

        <div style={{ display: 'flex', gap: 10, justifyContent: 'flex-end', marginTop: 20 }}>
          <button onClick={onClose} style={{ padding: '8px 18px', background: 'var(--bg-tertiary)', border: '1px solid var(--border)', borderRadius: 8, cursor: 'pointer', fontSize: 13 }}>Cancel</button>
          <button onClick={submit} disabled={saving} style={{ padding: '8px 18px', background: 'var(--primary, #7c3aed)', color: 'white', border: 'none', borderRadius: 8, cursor: 'pointer', fontSize: 13, fontWeight: 600 }}>
            {saving ? 'Generating…' : 'Generate Pickwave'}
          </button>
        </div>
      </div>
    </div>
  );
}

// ─── Detail Panel ─────────────────────────────────────────────────────────────
function PickwaveDetail({ wave, onClose, onUpdated }: { wave: Pickwave; onClose: () => void; onUpdated: () => void }) {
  const [detail, setDetail] = useState<Pickwave | null>(null);
  const [loading, setLoading] = useState(true);
  const [editPicked, setEditPicked] = useState<Record<string, number>>({});

  useEffect(() => {
    api(`/pickwaves/${wave.id}`).then(r => r.json()).then(d => setDetail(d.pickwave))
      .catch(() => {}).finally(() => setLoading(false));
  }, [wave.id]);

  const updateStatus = async (status: string) => {
    await api(`/pickwaves/${wave.id}`, { method: 'PUT', body: JSON.stringify({ status }) });
    onUpdated();
  };

  const savePicked = async (lineId: string) => {
    const qty = editPicked[lineId];
    if (qty === undefined) return;
    await api(`/pickwaves/${wave.id}/lines/${lineId}`, { method: 'PUT', body: JSON.stringify({ picked_quantity: qty }) });
    setDetail(d => d ? {
      ...d,
      lines: d.lines?.map(l => l.id === lineId ? { ...l, picked_quantity: qty, status: qty >= l.quantity ? 'picked' : qty > 0 ? 'short' : 'pending' } : l),
    } : d);
  };

  const panel: React.CSSProperties = { position: 'fixed', top: 0, right: 0, bottom: 0, width: 560, background: 'var(--bg-primary)', borderLeft: '1px solid var(--border)', boxShadow: '-4px 0 20px rgba(0,0,0,0.15)', display: 'flex', flexDirection: 'column', zIndex: 900 };

  return (
    <div style={panel}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '20px 24px', borderBottom: '1px solid var(--border)' }}>
        <div>
          <h3 style={{ margin: 0, fontSize: 16, fontWeight: 700 }}>{wave.name}</h3>
          <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 3 }}>
            <span style={{ background: STATUS_COLOURS[wave.status] || '#9e9e9e', color: 'white', padding: '2px 8px', borderRadius: 4, fontSize: 11, fontWeight: 700, textTransform: 'uppercase', marginRight: 8 }}>{wave.status}</span>
            {wave.type} · {wave.order_count} orders · {wave.item_count} items
          </div>
        </div>
        <button onClick={onClose} style={{ background: 'none', border: 'none', fontSize: 20, cursor: 'pointer', color: 'var(--text-muted)' }}>×</button>
      </div>

      <div style={{ padding: '12px 24px', borderBottom: '1px solid var(--border)', display: 'flex', gap: 8 }}>
        {wave.status === 'draft' && <button onClick={() => updateStatus('in_progress')} style={{ padding: '6px 14px', background: '#2196F3', color: 'white', border: 'none', borderRadius: 6, cursor: 'pointer', fontSize: 12, fontWeight: 600 }}>▶ Start Picking</button>}
        {wave.status === 'in_progress' && <button onClick={() => updateStatus('complete')} style={{ padding: '6px 14px', background: '#4CAF50', color: 'white', border: 'none', borderRadius: 6, cursor: 'pointer', fontSize: 12, fontWeight: 600 }}>✓ Complete</button>}
        {['draft','in_progress'].includes(wave.status) && <button onClick={() => updateStatus('cancelled')} style={{ padding: '6px 14px', background: 'rgba(239,68,68,0.1)', color: '#ef4444', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 6, cursor: 'pointer', fontSize: 12 }}>Cancel</button>}
      </div>

      <div style={{ flex: 1, overflowY: 'auto', padding: '0 24px 24px' }}>
        {loading ? <div style={{ padding: 40, textAlign: 'center', color: 'var(--text-muted)' }}>Loading…</div> : (
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13, marginTop: 16 }}>
            <thead>
              <tr>{['SKU','Product','Binrack','Req','Picked','Status'].map(h => (
                <th key={h} style={{ textAlign: 'left', padding: '0 8px 10px', fontSize: 11, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.06em', color: 'var(--text-muted)', borderBottom: '1px solid var(--border)' }}>{h}</th>
              ))}</tr>
            </thead>
            <tbody>
              {(detail?.lines || []).map(line => (
                <tr key={line.id}>
                  <td style={{ padding: '10px 8px', borderBottom: '1px solid var(--border)', fontFamily: 'monospace', fontSize: 12 }}>{line.sku}</td>
                  <td style={{ padding: '10px 8px', borderBottom: '1px solid var(--border)', maxWidth: 120, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{line.product_name}</td>
                  <td style={{ padding: '10px 8px', borderBottom: '1px solid var(--border)', color: 'var(--text-muted)', fontSize: 12 }}>{line.binrack_name || '—'}</td>
                  <td style={{ padding: '10px 8px', borderBottom: '1px solid var(--border)', fontWeight: 600 }}>{line.quantity}</td>
                  <td style={{ padding: '10px 8px', borderBottom: '1px solid var(--border)' }}>
                    <input type="number" min={0} max={line.quantity}
                      value={editPicked[line.id] ?? line.picked_quantity}
                      onChange={e => setEditPicked(p => ({ ...p, [line.id]: parseInt(e.target.value) || 0 }))}
                      onBlur={() => savePicked(line.id)}
                      style={{ width: 60, padding: '4px 6px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', fontSize: 13, textAlign: 'center' }} />
                  </td>
                  <td style={{ padding: '10px 8px', borderBottom: '1px solid var(--border)' }}>
                    <span style={{ padding: '2px 8px', borderRadius: 4, fontSize: 11, fontWeight: 700, textTransform: 'uppercase',
                      background: line.status === 'picked' ? 'rgba(76,175,80,0.15)' : line.status === 'short' ? 'rgba(255,152,0,0.15)' : 'rgba(158,158,158,0.15)',
                      color: line.status === 'picked' ? '#4CAF50' : line.status === 'short' ? '#FF9800' : '#9e9e9e' }}>
                      {line.status}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}

// ─── Main Page ────────────────────────────────────────────────────────────────
export default function Pickwaves() {
  const [waves, setWaves] = useState<Pickwave[]>([]);
  const [loading, setLoading] = useState(true);
  const [toast, setToast] = useState<Toast>(null);
  const [viewMode, setViewMode] = useState<ViewMode>('list');
  const [showGenerate, setShowGenerate] = useState(false);
  const [selectedWave, setSelectedWave] = useState<Pickwave | null>(null);
  const [statusFilter, setStatusFilter] = useState('');

  const showToast = (msg: string, type: 'success' | 'error') => {
    setToast({ msg, type }); setTimeout(() => setToast(null), 3000);
  };

  const load = () => {
    const qs = statusFilter ? `?status=${statusFilter}` : '';
    api(`/pickwaves${qs}`).then(r => r.json()).then(d => setWaves(d.pickwaves || []))
      .catch(() => showToast('Failed to load pickwaves', 'error'))
      .finally(() => setLoading(false));
  };
  useEffect(() => { load(); }, [statusFilter]);

  const deleteWave = async (id: string) => {
    await api(`/pickwaves/${id}`, { method: 'DELETE' });
    setWaves(ws => ws.filter(w => w.id !== id));
  };

  const page: React.CSSProperties = { padding: '28px 32px', maxWidth: 1200, margin: '0 auto' };
  const statusBadge = (status: string) => (
    <span style={{ display: 'inline-block', padding: '2px 10px', borderRadius: 4, fontSize: 11, fontWeight: 700, textTransform: 'uppercase', background: STATUS_COLOURS[status] || '#9e9e9e', color: 'white' }}>
      {status.replace('_', ' ')}
    </span>
  );

  return (
    <div style={page}>
      {/* Header */}
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', marginBottom: 28 }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 28, fontWeight: 800 }}>Pickwaves</h1>
          <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 14 }}>Manage batch picking waves for order fulfilment</p>
        </div>
        <button onClick={() => setShowGenerate(true)}
          style={{ padding: '10px 20px', background: 'var(--primary, #7c3aed)', color: 'white', border: 'none', borderRadius: 8, cursor: 'pointer', fontSize: 14, fontWeight: 700 }}>
          + Generate Pickwave
        </button>
      </div>

      {/* Filters & View Toggle */}
      <div style={{ display: 'flex', gap: 12, marginBottom: 20, alignItems: 'center' }}>
        <select value={statusFilter} onChange={e => setStatusFilter(e.target.value)}
          style={{ padding: '7px 12px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-primary)', fontSize: 13 }}>
          <option value="">All Statuses</option>
          {KANBAN_COLS.map(s => <option key={s} value={s}>{s.replace('_', ' ')}</option>)}
        </select>
        <div style={{ marginLeft: 'auto', display: 'flex', gap: 4 }}>
          {(['list','kanban'] as ViewMode[]).map(m => (
            <button key={m} onClick={() => setViewMode(m)}
              style={{ padding: '7px 14px', background: viewMode === m ? 'var(--primary, #7c3aed)' : 'var(--bg-secondary)', color: viewMode === m ? 'white' : 'var(--text-secondary)', border: '1px solid var(--border)', borderRadius: 8, cursor: 'pointer', fontSize: 13, textTransform: 'capitalize' }}>
              {m === 'list' ? '☰ List' : '⊞ Kanban'}
            </button>
          ))}
        </div>
      </div>

      {loading ? (
        <div style={{ padding: 60, textAlign: 'center', color: 'var(--text-muted)' }}>Loading pickwaves…</div>
      ) : viewMode === 'list' ? (
        /* LIST VIEW */
        <div style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 12, overflow: 'hidden' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
            <thead>
              <tr style={{ background: 'var(--bg-tertiary)' }}>
                {['Name / ID','Status','Type','Orders','Items','Assigned','Created',''].map(h => (
                  <th key={h} style={{ textAlign: 'left', padding: '10px 16px', fontSize: 11, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.06em', color: 'var(--text-muted)', borderBottom: '1px solid var(--border)' }}>{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {waves.length === 0 ? (
                <tr><td colSpan={8} style={{ padding: 40, textAlign: 'center', color: 'var(--text-muted)' }}>No pickwaves found. Generate one to get started.</td></tr>
              ) : waves.map(w => (
                <tr key={w.id} style={{ cursor: 'pointer', transition: 'background 0.15s' }}
                  onMouseEnter={e => (e.currentTarget.style.background = 'var(--bg-tertiary)')}
                  onMouseLeave={e => (e.currentTarget.style.background = '')}
                  onClick={() => setSelectedWave(w)}>
                  <td style={{ padding: '12px 16px', borderBottom: '1px solid var(--border)' }}>
                    <div style={{ fontWeight: 700 }}>{w.name}</div>
                    <div style={{ fontSize: 11, color: 'var(--text-muted)', fontFamily: 'monospace' }}>{w.id}</div>
                  </td>
                  <td style={{ padding: '12px 16px', borderBottom: '1px solid var(--border)' }}>{statusBadge(w.status)}</td>
                  <td style={{ padding: '12px 16px', borderBottom: '1px solid var(--border)', textTransform: 'capitalize', color: 'var(--text-muted)' }}>{w.type?.replace('_', ' ')}</td>
                  <td style={{ padding: '12px 16px', borderBottom: '1px solid var(--border)', fontWeight: 600 }}>{w.order_count}</td>
                  <td style={{ padding: '12px 16px', borderBottom: '1px solid var(--border)' }}>{w.item_count}</td>
                  <td style={{ padding: '12px 16px', borderBottom: '1px solid var(--border)', color: 'var(--text-muted)', fontSize: 12 }}>{w.assigned_user_id || '—'}</td>
                  <td style={{ padding: '12px 16px', borderBottom: '1px solid var(--border)', color: 'var(--text-muted)', fontSize: 12 }}>{w.created_at ? new Date(w.created_at).toLocaleDateString() : '—'}</td>
                  <td style={{ padding: '12px 16px', borderBottom: '1px solid var(--border)' }}>
                    <button onClick={e => { e.stopPropagation(); deleteWave(w.id); }}
                      style={{ background: 'rgba(239,68,68,0.1)', color: '#ef4444', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 6, padding: '4px 10px', cursor: 'pointer', fontSize: 12 }}>
                      Cancel
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        /* KANBAN VIEW */
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(5, 1fr)', gap: 16, alignItems: 'start' }}>
          {KANBAN_COLS.map(col => {
            const colWaves = waves.filter(w => w.status === col);
            return (
              <div key={col} style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 12, overflow: 'hidden' }}>
                <div style={{ padding: '10px 14px', borderBottom: '1px solid var(--border)', background: 'var(--bg-tertiary)', display: 'flex', alignItems: 'center', gap: 8 }}>
                  <span style={{ width: 8, height: 8, borderRadius: '50%', background: STATUS_COLOURS[col], display: 'inline-block' }} />
                  <span style={{ fontSize: 12, fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.06em' }}>{col.replace('_', ' ')}</span>
                  <span style={{ marginLeft: 'auto', background: 'var(--bg-primary)', borderRadius: '50%', width: 20, height: 20, display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 11, fontWeight: 700 }}>{colWaves.length}</span>
                </div>
                <div style={{ padding: 10, display: 'flex', flexDirection: 'column', gap: 8, minHeight: 60 }}>
                  {colWaves.map(w => (
                    <div key={w.id} onClick={() => setSelectedWave(w)} style={{ background: 'var(--bg-primary)', border: '1px solid var(--border)', borderRadius: 8, padding: '10px 12px', cursor: 'pointer' }}>
                      <div style={{ fontWeight: 700, fontSize: 13, marginBottom: 4 }}>{w.name}</div>
                      <div style={{ fontSize: 11, color: 'var(--text-muted)' }}>{w.order_count} orders · {w.item_count} items</div>
                      {w.assigned_user_id && <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 4 }}>👤 {w.assigned_user_id}</div>}
                    </div>
                  ))}
                  {colWaves.length === 0 && <div style={{ fontSize: 12, color: 'var(--text-muted)', padding: '8px 4px', textAlign: 'center' }}>Empty</div>}
                </div>
              </div>
            );
          })}
        </div>
      )}

      {showGenerate && (
        <GeneratePickwaveDialog
          onClose={() => setShowGenerate(false)}
          onCreated={() => { setShowGenerate(false); load(); showToast('Pickwave generated', 'success'); }}
        />
      )}

      {selectedWave && (
        <>
          <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.2)', zIndex: 899 }} onClick={() => setSelectedWave(null)} />
          <PickwaveDetail wave={selectedWave} onClose={() => setSelectedWave(null)} onUpdated={() => { load(); }} />
        </>
      )}

      {toast && (
        <div style={{ position: 'fixed', bottom: 24, right: 24, padding: '12px 20px', background: toast.type === 'success' ? '#4CAF50' : '#ef4444', color: 'white', borderRadius: 8, fontSize: 13, fontWeight: 600, zIndex: 9999 }}>
          {toast.type === 'success' ? '✓' : '✗'} {toast.msg}
        </div>
      )}
    </div>
  );
}

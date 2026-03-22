import { useState, useEffect } from 'react';
import { getActiveTenantId } from '../contexts/TenantContext';

const API = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';
function api(path: string, init?: RequestInit) {
  return fetch(`${API}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId(), ...(init?.headers || {}) },
  });
}

interface GeneratePickwaveDialogProps {
  onClose: () => void;
  onCreated: () => void;
  preselectedOrders?: string[];
}

export default function GeneratePickwaveDialog({ onClose, onCreated, preselectedOrders }: GeneratePickwaveDialogProps) {
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

  const toggleAll = () => {
    const allIds = orders.map((o: any) => o.id || o.order_id);
    setSelected(s => s.length === allIds.length ? [] : allIds);
  };

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
  const labelStyle: React.CSSProperties = { fontSize: 11, fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.06em', color: 'var(--text-muted)', display: 'block', marginBottom: 6 };
  const inputStyle: React.CSSProperties = { width: '100%', padding: '8px 12px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-primary)', fontSize: 13, marginBottom: 16, boxSizing: 'border-box' };

  return (
    <div style={overlay} onClick={e => e.target === e.currentTarget && onClose()}>
      <div style={modal}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 24 }}>
          <h3 style={{ margin: 0, fontSize: 18, fontWeight: 700, color: 'var(--text-primary)' }}>Generate Pickwave</h3>
          <button onClick={onClose} style={{ background: 'none', border: 'none', fontSize: 20, cursor: 'pointer', color: 'var(--text-muted)' }}>×</button>
        </div>

        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16, marginBottom: 4 }}>
          <div>
            <label style={labelStyle}>Pickwave Name (optional)</label>
            <input style={inputStyle} value={name} onChange={e => setName(e.target.value)} placeholder="Auto-generated if blank" />
          </div>
          <div>
            <label style={labelStyle}>Assigned User</label>
            <input style={inputStyle} value={assignedUser} onChange={e => setAssignedUser(e.target.value)} placeholder="User ID or name" />
          </div>
          <div>
            <label style={labelStyle}>Pickwave Type</label>
            <select style={inputStyle} value={type} onChange={e => setType(e.target.value)}>
              <option value="single_sku">Single SKU</option>
              <option value="multi_sku">Multi-SKU</option>
              <option value="tote">Tote</option>
              <option value="tray">Tray</option>
            </select>
          </div>
          <div>
            <label style={labelStyle}>Grouping</label>
            <select style={inputStyle} value={grouping} onChange={e => setGrouping(e.target.value)}>
              <option value="single_order">Single Order</option>
              <option value="by_sku">By SKU</option>
              <option value="by_location">By Location</option>
              <option value="by_folder">By Folder</option>
            </select>
          </div>
          <div>
            <label style={labelStyle}>Sort By</label>
            <select style={inputStyle} value={sortBy} onChange={e => setSortBy(e.target.value)}>
              <option value="sku">SKU</option>
              <option value="binrack">Binrack</option>
              <option value="alphabetical">Alphabetical</option>
            </select>
          </div>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 }}>
            <div>
              <label style={labelStyle}>Max Orders (0=∞)</label>
              <input style={inputStyle} type="number" min={0} value={maxOrders} onChange={e => setMaxOrders(parseInt(e.target.value) || 0)} />
            </div>
            <div>
              <label style={labelStyle}>Max Items (0=∞)</label>
              <input style={inputStyle} type="number" min={0} value={maxItems} onChange={e => setMaxItems(parseInt(e.target.value) || 0)} />
            </div>
          </div>
        </div>

        <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13, cursor: 'pointer', marginBottom: 20, color: 'var(--text-secondary)' }}>
          <input type="checkbox" checked={showNextOnly} onChange={e => setShowNextOnly(e.target.checked)} />
          Show Next Only (hide already-picked lines)
        </label>

        <div>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 }}>
            <label style={labelStyle}>Orders to Include ({selected.length} selected)</label>
            <button onClick={toggleAll} style={{ background: 'none', border: 'none', fontSize: 12, color: 'var(--primary)', cursor: 'pointer', padding: 0 }}>
              {selected.length === orders.length ? 'Deselect All' : 'Select All'}
            </button>
          </div>
          <div style={{ border: '1px solid var(--border)', borderRadius: 8, maxHeight: 200, overflowY: 'auto', background: 'var(--bg-secondary)' }}>
            {orders.length === 0 ? (
              <div style={{ padding: 16, color: 'var(--text-muted)', fontSize: 13, textAlign: 'center' }}>No open orders found</div>
            ) : orders.map((o: any) => {
              const id = o.id || o.order_id;
              return (
                <label key={id} style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '10px 14px', borderBottom: '1px solid var(--border)', cursor: 'pointer', fontSize: 13 }}>
                  <input type="checkbox" checked={selected.includes(id)} onChange={() => toggleOrder(id)} />
                  <span style={{ fontWeight: 600, color: 'var(--text-primary)' }}>{id}</span>
                  <span style={{ color: 'var(--text-muted)' }}>{o.customer_name || o.channel || ''}</span>
                </label>
              );
            })}
          </div>
        </div>

        {error && <div style={{ marginTop: 12, padding: '10px 14px', background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 8, color: '#ef4444', fontSize: 13 }}>{error}</div>}

        <div style={{ display: 'flex', gap: 10, justifyContent: 'flex-end', marginTop: 20 }}>
          <button onClick={onClose} style={{ padding: '8px 18px', background: 'var(--bg-tertiary)', border: '1px solid var(--border)', borderRadius: 8, cursor: 'pointer', fontSize: 13, color: 'var(--text-primary)' }}>Cancel</button>
          <button onClick={submit} disabled={saving} style={{ padding: '8px 18px', background: 'var(--primary, #7c3aed)', color: 'white', border: 'none', borderRadius: 8, cursor: 'pointer', fontSize: 13, fontWeight: 600, opacity: saving ? 0.7 : 1 }}>
            {saving ? 'Generating…' : 'Generate Pickwave'}
          </button>
        </div>
      </div>
    </div>
  );
}

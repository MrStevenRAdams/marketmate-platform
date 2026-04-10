import { useState, useEffect, useRef } from 'react';
import { getActiveTenantId } from '../contexts/TenantContext';

const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';
function api(path: string, init?: RequestInit) {
  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId(), ...init?.headers },
  });
}

interface StockItem {
  product_id: string;
  sku: string;
  title: string;
  total_stock: number;
  available: number;
  reserved: number;
  in_transit: number;
  reorder_point: number;
  warehouse?: string;
  locations?: Array<{ warehouse_name: string; quantity: number }>;
}

export default function MyInventory() {
  const [items, setItems] = useState<StockItem[]>([]);
  const [filtered, setFiltered] = useState<StockItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [search, setSearch] = useState('');
  const [page, setPage] = useState(0);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editValue, setEditValue] = useState('');
  const [editField, setEditField] = useState<'stock' | 'reorder'>('stock');
  const [saving, setSaving] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [reorderModal, setReorderModal] = useState<string | null>(null);
  const [reorderValue, setReorderValue] = useState('');
  const inputRef = useRef<HTMLInputElement>(null);

  const PAGE_SIZE = 50;

  useEffect(() => { loadInventory(); }, []);

  useEffect(() => {
    const q = search.toLowerCase();
    const f = q
      ? items.filter(i => i.sku?.toLowerCase().includes(q) || i.title?.toLowerCase().includes(q))
      : items;
    setFiltered(f);
    setPage(0);
  }, [search, items]);

  async function loadInventory() {
    setLoading(true);
    try {
      const res = await api('/inventory?limit=500');
      if (res.ok) {
        const data = await res.json();
        const raw = data.items || data.inventory || data.locations || [];
        const mapped: StockItem[] = (Array.isArray(raw) ? raw : []).map((item: Record<string, unknown>) => ({
          product_id:   String(item.product_id || item.sku || ''),
          sku:          String(item.sku || ''),
          title:        String(item.title || item.product_title || item.name || ''),
          total_stock:  Number(item.total_stock || item.quantity || item.stock_level || 0),
          available:    Number(item.available || item.available_stock || item.quantity || 0),
          reserved:     Number(item.reserved || item.reserved_stock || 0),
          in_transit:   Number(item.in_transit || 0),
          reorder_point: Number(item.reorder_point || 0),
          warehouse:    String(item.warehouse || item.warehouse_name || ''),
          locations:    Array.isArray(item.locations) ? (item.locations as Array<{ warehouse_name: string; quantity: number }>) : [],
        }));
        setItems(mapped);
        setFiltered(mapped);
      }
    } finally {
      setLoading(false);
    }
  }

  function startEdit(item: StockItem, field: 'stock' | 'reorder') {
    setEditingId(item.product_id + ':' + field);
    setEditValue(String(field === 'stock' ? item.total_stock : item.reorder_point));
    setEditField(field);
    setTimeout(() => inputRef.current?.select(), 50);
  }

  async function commitEdit(item: StockItem) {
    if (!editingId) return;
    setSaving(true);
    try {
      const val = parseInt(editValue, 10);
      if (isNaN(val)) { setEditingId(null); return; }

      if (editField === 'stock') {
        await api('/inventory/adjust', {
          method: 'POST',
          body: JSON.stringify({ product_id: item.product_id, sku: item.sku, adjustment: val - item.total_stock, reason: 'manual_edit' }),
        });
      } else {
        await api(`/inventory/${item.product_id}`, {
          method: 'PATCH',
          body: JSON.stringify({ reorder_point: val }),
        });
      }

      setItems(prev => prev.map(i =>
        i.product_id === item.product_id
          ? editField === 'stock'
            ? { ...i, total_stock: val, available: val - i.reserved }
            : { ...i, reorder_point: val }
          : i
      ));
    } finally {
      setSaving(false);
      setEditingId(null);
    }
  }

  function exportCSV() {
    const header = 'SKU,Title,Total Stock,Available,Reserved,In Transit,Reorder Point,Warehouse';
    const rows = filtered.map(i =>
      `"${i.sku}","${i.title}",${i.total_stock},${i.available},${i.reserved},${i.in_transit},${i.reorder_point},"${i.warehouse}"`
    );
    const blob = new Blob([header + '\n' + rows.join('\n')], { type: 'text/csv' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url; a.download = `inventory-${new Date().toISOString().slice(0, 10)}.csv`;
    a.click(); URL.revokeObjectURL(url);
  }

  async function bulkSetReorder() {
    const val = parseInt(reorderValue, 10);
    if (isNaN(val) || selected.size === 0) return;
    setSaving(true);
    try {
      for (const pid of Array.from(selected)) {
        await api(`/inventory/${pid}`, { method: 'PATCH', body: JSON.stringify({ reorder_point: val }) });
      }
      setItems(prev => prev.map(i => selected.has(i.product_id) ? { ...i, reorder_point: val } : i));
      setReorderModal(null);
      setSelected(new Set());
      setReorderValue('');
    } finally {
      setSaving(false);
    }
  }

  const toggleSelect = (id: string) => setSelected(prev => {
    const n = new Set(prev); n.has(id) ? n.delete(id) : n.add(id); return n;
  });
  const toggleAll = () => setSelected(selected.size === paged.length ? new Set() : new Set(paged.map(i => i.product_id)));

  const paged = filtered.slice(page * PAGE_SIZE, (page + 1) * PAGE_SIZE);
  const totalPages = Math.ceil(filtered.length / PAGE_SIZE);

  const cellStyle: React.CSSProperties = { padding: '11px 14px', borderBottom: '1px solid var(--border)' };
  const numStyle: React.CSSProperties = { ...cellStyle, textAlign: 'right', fontVariantNumeric: 'tabular-nums' };

  function EditableCell({ item, field, value }: { item: StockItem; field: 'stock' | 'reorder'; value: number }) {
    const eid = item.product_id + ':' + field;
    const isEditing = editingId === eid;
    if (isEditing) {
      return (
        <td style={numStyle}>
          <input
            ref={inputRef}
            type="number"
            value={editValue}
            onChange={e => setEditValue(e.target.value)}
            onBlur={() => commitEdit(item)}
            onKeyDown={e => { if (e.key === 'Enter') commitEdit(item); if (e.key === 'Escape') setEditingId(null); }}
            style={{ width: 70, padding: '3px 6px', background: 'var(--bg-elevated)', border: '1px solid var(--primary)', borderRadius: 4, color: 'var(--text-primary)', fontSize: 13, textAlign: 'right' }}
          />
        </td>
      );
    }
    return (
      <td
        style={{ ...numStyle, cursor: 'pointer' }}
        onClick={() => startEdit(item, field)}
        title="Click to edit"
      >
        <span style={{ borderBottom: '1px dashed var(--text-muted)' }}>{value}</span>
      </td>
    );
  }

  return (
    <div style={{ padding: '32px 40px', maxWidth: 1400, margin: '0 auto' }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 24 }}>
        <div>
          <h1 style={{ margin: 0, color: 'var(--text-primary)', fontSize: 22, fontWeight: 700 }}>📊 My Inventory</h1>
          <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 14 }}>
            {loading ? 'Loading…' : `${filtered.length.toLocaleString()} items`}
            {selected.size > 0 && <span style={{ marginLeft: 8, color: 'var(--accent-cyan)' }}>· {selected.size} selected</span>}
          </p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          {selected.size > 0 && (
            <>
              <button
                onClick={() => setReorderModal('bulk')}
                style={{ padding: '8px 16px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-secondary)', cursor: 'pointer', fontSize: 13 }}
              >
                Set Reorder Point
              </button>
            </>
          )}
          <button
            onClick={exportCSV}
            style={{ padding: '8px 16px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-secondary)', cursor: 'pointer', fontSize: 13 }}
          >
            ⬇ Export CSV
          </button>
        </div>
      </div>

      {/* Search */}
      <div style={{ marginBottom: 16 }}>
        <input
          type="text"
          value={search}
          onChange={e => setSearch(e.target.value)}
          placeholder="🔍 Search by SKU or product title…"
          style={{
            width: '100%', padding: '10px 14px', background: 'var(--bg-secondary)',
            border: '1px solid var(--border)', borderRadius: 10, color: 'var(--text-primary)',
            fontSize: 14, boxSizing: 'border-box',
          }}
        />
      </div>

      {/* Grid */}
      <div style={{ background: 'var(--bg-secondary)', borderRadius: 12, border: '1px solid var(--border)', overflow: 'hidden' }}>
        <div style={{ overflowX: 'auto' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', minWidth: 900 }}>
            <thead>
              <tr style={{ borderBottom: '1px solid var(--border)', background: 'var(--bg-elevated)' }}>
                <th style={{ padding: '10px 14px', width: 36 }}>
                  <input type="checkbox" checked={paged.length > 0 && selected.size === paged.length} onChange={toggleAll} />
                </th>
                {['SKU', 'Product Title', 'Total Stock', 'Available', 'Reserved', 'In Transit', 'Reorder Point', 'Warehouse'].map(h => (
                  <th key={h} style={{ padding: '10px 14px', textAlign: h.startsWith('Total') || h === 'Available' || h === 'Reserved' || h === 'In Transit' || h === 'Reorder Point' ? 'right' : 'left', fontSize: 12, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.05em', whiteSpace: 'nowrap' }}>{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {loading ? (
                <tr><td colSpan={9} style={{ padding: 60, textAlign: 'center', color: 'var(--text-muted)' }}>Loading inventory…</td></tr>
              ) : paged.length === 0 ? (
                <tr>
                  <td colSpan={9} style={{ padding: 60, textAlign: 'center', color: 'var(--text-muted)' }}>
                    <div style={{ fontSize: 32, marginBottom: 12 }}>📦</div>
                    <div>{search ? 'No items match your search.' : 'No inventory items found.'}</div>
                  </td>
                </tr>
              ) : paged.map(item => (
                <tr key={item.product_id} style={{ borderBottom: '1px solid var(--border)' }}>
                  <td style={{ padding: '11px 14px', borderBottom: '1px solid var(--border)' }}>
                    <input type="checkbox" checked={selected.has(item.product_id)} onChange={() => toggleSelect(item.product_id)} />
                  </td>
                  <td style={{ ...cellStyle, fontFamily: 'monospace', fontSize: 12, color: 'var(--text-muted)' }}>{item.sku || '—'}</td>
                  <td style={{ ...cellStyle, color: 'var(--text-primary)', fontWeight: 500, maxWidth: 300, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }} title={item.title}>{item.title || '—'}</td>
                  <EditableCell item={item} field="stock" value={item.total_stock} />
                  <td style={{ ...numStyle, color: item.available < item.reorder_point && item.reorder_point > 0 ? '#f87171' : 'var(--text-secondary)' }}>{item.available}</td>
                  <td style={numStyle}>{item.reserved}</td>
                  <td style={numStyle}>{item.in_transit}</td>
                  <EditableCell item={item} field="reorder" value={item.reorder_point} />
                  <td style={{ ...cellStyle, color: 'var(--text-muted)', fontSize: 13 }}>{item.warehouse || '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>

      {/* Pagination */}
      {totalPages > 1 && (
        <div style={{ display: 'flex', justifyContent: 'center', gap: 8, marginTop: 20 }}>
          <button onClick={() => setPage(p => Math.max(0, p - 1))} disabled={page === 0}
            style={{ padding: '7px 14px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6, color: page === 0 ? 'var(--text-muted)' : 'var(--text-secondary)', cursor: page === 0 ? 'not-allowed' : 'pointer', fontSize: 13 }}>
            ← Prev
          </button>
          <span style={{ padding: '7px 14px', color: 'var(--text-muted)', fontSize: 13 }}>
            Page {page + 1} of {totalPages}
          </span>
          <button onClick={() => setPage(p => Math.min(totalPages - 1, p + 1))} disabled={page >= totalPages - 1}
            style={{ padding: '7px 14px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6, color: page >= totalPages - 1 ? 'var(--text-muted)' : 'var(--text-secondary)', cursor: page >= totalPages - 1 ? 'not-allowed' : 'pointer', fontSize: 13 }}>
            Next →
          </button>
        </div>
      )}

      {/* Bulk Set Reorder Modal */}
      {reorderModal && (
        <>
          <div onClick={() => setReorderModal(null)} style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.5)', zIndex: 999 }} />
          <div style={{
            position: 'fixed', top: '50%', left: '50%', transform: 'translate(-50%,-50%)',
            background: 'var(--bg-secondary)', borderRadius: 16, border: '1px solid var(--border)',
            width: 380, padding: 24, zIndex: 1000, boxShadow: '0 24px 64px rgba(0,0,0,0.5)',
          }}>
            <h3 style={{ margin: '0 0 16px', color: 'var(--text-primary)', fontSize: 16 }}>Set Reorder Point ({selected.size} items)</h3>
            <input
              type="number"
              value={reorderValue}
              onChange={e => setReorderValue(e.target.value)}
              placeholder="Enter reorder point quantity"
              autoFocus
              style={{ width: '100%', padding: '9px 12px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-primary)', fontSize: 14, boxSizing: 'border-box', marginBottom: 16 }}
            />
            <div style={{ display: 'flex', gap: 10, justifyContent: 'flex-end' }}>
              <button onClick={() => setReorderModal(null)} style={{ padding: '9px 16px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-secondary)', cursor: 'pointer', fontSize: 14 }}>Cancel</button>
              <button onClick={bulkSetReorder} disabled={saving || !reorderValue}
                style={{ padding: '9px 18px', background: (saving || !reorderValue) ? 'rgba(99,102,241,0.4)' : 'var(--primary)', border: 'none', borderRadius: 8, color: '#fff', fontWeight: 600, fontSize: 14, cursor: (saving || !reorderValue) ? 'not-allowed' : 'pointer' }}>
                {saving ? 'Saving…' : 'Apply'}
              </button>
            </div>
          </div>
        </>
      )}
    </div>
  );
}

// ============================================================================
// PRODUCT DETAIL NEW TABS
// ============================================================================
// File: frontend/src/components/ProductDetailTabs.tsx
// Contains all new tab components added for gap closure:
//   - BatchesTab
//   - ExtendedPropertiesTab
//   - IdentifiersTab
//   - ChannelSkuMappingTab
//   - StockHistoryTab
//   - ItemStatsTab
//   - KTypesTab
// ============================================================================

import { useState, useEffect, useCallback } from 'react';
import { getActiveTenantId } from '../contexts/TenantContext';

const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';
function api(path: string, init?: RequestInit) {
  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId(), ...init?.headers },
  });
}

// ── BATCHES TAB ───────────────────────────────────────────────────────────────

interface Batch {
  batch_id: string;
  batch_number: string;
  quantity: number;
  sell_by_date?: string;
  expire_on_date?: string;
  priority_sequence: number;
  status: string;
  location_id: string;
}

export function BatchesTab({ productId }: { productId: string }) {
  const [batches, setBatches] = useState<Batch[]>([]);
  const [showModal, setShowModal] = useState(false);
  const [editing, setEditing] = useState<Batch | null>(null);
  const [form, setForm] = useState({ batch_number: '', quantity: 1, sell_by_date: '', expire_on_date: '', priority_sequence: 0, location_id: '' });
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => { load(); }, [productId]);

  async function load() {
    const res = await api(`/products/${productId}/batches`);
    if (res.ok) setBatches((await res.json()).batches || []);
  }

  function openCreate() {
    setEditing(null);
    setForm({ batch_number: '', quantity: 1, sell_by_date: '', expire_on_date: '', priority_sequence: 0, location_id: '' });
    setShowModal(true); setError('');
  }

  function openEdit(b: Batch) {
    setEditing(b);
    setForm({
      batch_number: b.batch_number,
      quantity: b.quantity,
      sell_by_date: b.sell_by_date?.slice(0, 10) || '',
      expire_on_date: b.expire_on_date?.slice(0, 10) || '',
      priority_sequence: b.priority_sequence,
      location_id: b.location_id,
    });
    setShowModal(true); setError('');
  }

  async function save() {
    if (!form.batch_number) { setError('Batch number required'); return; }
    setLoading(true);
    try {
      const body = JSON.stringify({
        ...form,
        sell_by_date: form.sell_by_date ? new Date(form.sell_by_date).toISOString() : null,
        expire_on_date: form.expire_on_date ? new Date(form.expire_on_date).toISOString() : null,
      });
      const res = editing
        ? await api(`/products/${productId}/batches/${editing.batch_id}`, { method: 'PUT', body })
        : await api(`/products/${productId}/batches`, { method: 'POST', body });
      if (!res.ok) throw new Error(await res.text());
      setShowModal(false); load();
    } catch (e: any) { setError(e.message); }
    finally { setLoading(false); }
  }

  async function deleteBatch(id: string) {
    if (!confirm('Delete this batch?')) return;
    await api(`/products/${productId}/batches/${id}`, { method: 'DELETE' });
    load();
  }

  const statusColor = (s: string) => ({ active: '#22c55e', expired: '#ef4444', consumed: '#64748b' }[s] || '#64748b');

  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 16 }}>
        <p style={{ color: 'var(--text-muted)', fontSize: 13, margin: 0 }}>Track individual batches with expiry dates and FEFO stock deduction.</p>
        <button onClick={openCreate} style={{ padding: '7px 16px', background: 'var(--primary)', border: 'none', borderRadius: 6, color: 'white', fontSize: 12, fontWeight: 700, cursor: 'pointer' }}>+ Add Batch</button>
      </div>

      {batches.length === 0 ? (
        <div style={{ textAlign: 'center', padding: 40, color: 'var(--text-muted)', fontSize: 13 }}>No batches yet.</div>
      ) : (
        <table style={{ width: '100%', borderCollapse: 'collapse' }}>
          <thead>
            <tr style={{ borderBottom: '1px solid var(--border)' }}>
              {['Batch #', 'Qty', 'Sell By', 'Expire On', 'Priority', 'Location', 'Status', ''].map(h => (
                <th key={h} style={{ padding: '8px 12px', textAlign: 'left', color: 'var(--text-muted)', fontSize: 11, fontWeight: 600 }}>{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {batches.map(b => (
              <tr key={b.batch_id} style={{ borderBottom: '1px solid var(--border)' }}>
                <td style={{ padding: '10px 12px', fontWeight: 600, color: 'var(--text-primary)', fontFamily: 'monospace' }}>{b.batch_number}</td>
                <td style={{ padding: '10px 12px', color: 'var(--text-primary)' }}>{b.quantity}</td>
                <td style={{ padding: '10px 12px', color: 'var(--text-secondary)', fontSize: 12 }}>{b.sell_by_date ? new Date(b.sell_by_date).toLocaleDateString() : '—'}</td>
                <td style={{ padding: '10px 12px', color: 'var(--text-secondary)', fontSize: 12 }}>{b.expire_on_date ? new Date(b.expire_on_date).toLocaleDateString() : '—'}</td>
                <td style={{ padding: '10px 12px', color: 'var(--text-muted)', fontSize: 12 }}>{b.priority_sequence || 0}</td>
                <td style={{ padding: '10px 12px', color: 'var(--text-muted)', fontSize: 12 }}>{b.location_id || '—'}</td>
                <td style={{ padding: '10px 12px' }}>
                  <span style={{ padding: '2px 8px', borderRadius: 4, fontSize: 11, fontWeight: 600, background: `${statusColor(b.status)}20`, color: statusColor(b.status) }}>{b.status}</span>
                </td>
                <td style={{ padding: '10px 12px' }}>
                  <div style={{ display: 'flex', gap: 6 }}>
                    <button onClick={() => openEdit(b)} style={{ padding: '3px 8px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 4, color: 'var(--text-muted)', cursor: 'pointer', fontSize: 11 }}>Edit</button>
                    <button onClick={() => deleteBatch(b.batch_id)} style={{ padding: '3px 8px', background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 4, color: '#ef4444', cursor: 'pointer', fontSize: 11 }}>Del</button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {showModal && (
        <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.6)', zIndex: 300, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
          <div style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 12, padding: 24, width: 440 }}>
            <h3 style={{ color: 'var(--text-primary)', marginBottom: 16 }}>{editing ? 'Edit' : 'Add'} Batch</h3>
            {error && <div style={{ color: '#ef4444', fontSize: 12, marginBottom: 10 }}>{error}</div>}
            <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
              {[
                { label: 'Batch Number', key: 'batch_number', type: 'text' },
                { label: 'Quantity', key: 'quantity', type: 'number' },
                { label: 'Sell By Date', key: 'sell_by_date', type: 'date' },
                { label: 'Expire On Date', key: 'expire_on_date', type: 'date' },
                { label: 'Priority Sequence', key: 'priority_sequence', type: 'number' },
                { label: 'Location ID', key: 'location_id', type: 'text' },
              ].map(f => (
                <div key={f.key}>
                  <label style={{ display: 'block', marginBottom: 4, fontSize: 12, color: 'var(--text-secondary)' }}>{f.label}</label>
                  <input type={f.type} value={(form as any)[f.key]} onChange={e => setForm(prev => ({ ...prev, [f.key]: f.type === 'number' ? parseInt(e.target.value) || 0 : e.target.value }))}
                    style={{ width: '100%', padding: '8px 12px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)' }} />
                </div>
              ))}
            </div>
            <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 10, marginTop: 16 }}>
              <button onClick={() => setShowModal(false)} style={{ padding: '8px 16px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-secondary)', cursor: 'pointer' }}>Cancel</button>
              <button onClick={save} disabled={loading} style={{ padding: '8px 16px', background: 'var(--primary)', border: 'none', borderRadius: 6, color: 'white', fontWeight: 700, cursor: 'pointer' }}>{loading ? 'Saving…' : 'Save'}</button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

// ── EXTENDED PROPERTIES TAB ───────────────────────────────────────────────────

export function ExtendedPropertiesTab({ productId }: { productId: string }) {
  const [props, setProps] = useState<Record<string, string>>({});
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    api(`/products/${productId}/extended-properties`).then(r => r.ok ? r.json() : null).then(d => {
      if (d) setProps(d.extended_properties || {});
    });
  }, [productId]);

  function addRow() { setProps(prev => ({ ...prev, '': '' })); }

  function updateKey(oldKey: string, newKey: string) {
    const val = props[oldKey];
    const next: Record<string, string> = {};
    Object.keys(props).forEach(k => { next[k === oldKey ? newKey : k] = props[k]; });
    setProps(next);
  }

  function updateValue(key: string, val: string) { setProps(prev => ({ ...prev, [key]: val })); }

  function removeRow(key: string) {
    const next = { ...props };
    delete next[key];
    setProps(next);
  }

  async function save() {
    setSaving(true);
    const res = await api(`/products/${productId}/extended-properties`, { method: 'PUT', body: JSON.stringify({ properties: props }) });
    if (res.ok) { setSaved(true); setTimeout(() => setSaved(false), 2000); }
    setSaving(false);
  }

  const entries = Object.entries(props);

  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 16 }}>
        <p style={{ color: 'var(--text-muted)', fontSize: 13, margin: 0 }}>Custom key-value properties for this product.</p>
        <div style={{ display: 'flex', gap: 8 }}>
          <button onClick={addRow} style={{ padding: '6px 14px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-secondary)', fontSize: 12, cursor: 'pointer' }}>+ Add Row</button>
          <button onClick={save} disabled={saving} style={{ padding: '6px 14px', background: saved ? '#22c55e' : 'var(--primary)', border: 'none', borderRadius: 6, color: 'white', fontSize: 12, fontWeight: 700, cursor: 'pointer' }}>{saved ? '✅ Saved' : saving ? 'Saving…' : 'Save'}</button>
        </div>
      </div>
      {entries.length === 0 ? (
        <div style={{ textAlign: 'center', padding: 32, color: 'var(--text-muted)', fontSize: 13 }}>No extended properties yet. Click "+ Add Row" to add one.</div>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
          {entries.map(([key, val]) => (
            <div key={key} style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
              <input value={key} onChange={e => updateKey(key, e.target.value)} placeholder="Property name"
                style={{ flex: 1, padding: '8px 12px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', fontSize: 13, fontWeight: 600 }} />
              <input value={val} onChange={e => updateValue(key, e.target.value)} placeholder="Value"
                style={{ flex: 2, padding: '8px 12px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', fontSize: 13 }} />
              <button onClick={() => removeRow(key)} style={{ padding: '8px 10px', background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 6, color: '#ef4444', cursor: 'pointer' }}>×</button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

// ── IDENTIFIERS TAB ───────────────────────────────────────────────────────────

const IDENTIFIER_FIELDS = ['asin', 'ean', 'gtin', 'upc', 'mpn', 'isbn', 'hs_code'];

export function IdentifiersTab({ productId, initialIdentifiers }: { productId: string; initialIdentifiers?: any }) {
  const [ids, setIds] = useState<Record<string, string>>(initialIdentifiers || {});
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);

  async function save() {
    setSaving(true);
    const res = await api(`/products/${productId}/identifiers`, { method: 'PUT', body: JSON.stringify(ids) });
    if (res.ok) { setSaved(true); setTimeout(() => setSaved(false), 2000); }
    setSaving(false);
  }

  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 16 }}>
        <p style={{ color: 'var(--text-muted)', fontSize: 13, margin: 0 }}>Product identifiers used across marketplaces.</p>
        <button onClick={save} disabled={saving} style={{ padding: '6px 14px', background: saved ? '#22c55e' : 'var(--primary)', border: 'none', borderRadius: 6, color: 'white', fontSize: 12, fontWeight: 700, cursor: 'pointer' }}>{saved ? '✅ Saved' : saving ? 'Saving…' : 'Save Changes'}</button>
      </div>
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
        {IDENTIFIER_FIELDS.map(field => (
          <div key={field}>
            <label style={{ display: 'block', marginBottom: 4, fontSize: 12, color: 'var(--text-secondary)', textTransform: 'uppercase', fontWeight: 600 }}>{field.replace('_', ' ')}</label>
            <input value={ids[field] || ''} onChange={e => setIds(prev => ({ ...prev, [field]: e.target.value }))}
              placeholder={`Enter ${field.toUpperCase()}…`}
              style={{ width: '100%', padding: '9px 14px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-primary)' }} />
          </div>
        ))}
      </div>
    </div>
  );
}

// ── CHANNEL SKU MAPPING TAB ───────────────────────────────────────────────────

export function ChannelSkuMappingTab({ productId }: { productId: string }) {
  const [listings, setListings] = useState<any[]>([]);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editSku, setEditSku] = useState('');
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    api(`/marketplace/listings?product_id=${productId}`).then(r => r.ok ? r.json() : null).then(d => {
      if (!d) return;
      // Backend returns { data: [] } — support both { listings: [] } and { data: [] }
      const arr = Array.isArray(d.listings) ? d.listings
        : Array.isArray(d.data) ? d.data
        : Array.isArray(d) ? d
        : [];
      setListings(arr);
    });
  }, [productId]);

  async function saveSku(listingId: string) {
    setSaving(true);
    await api(`/marketplace/listings/${listingId}`, { method: 'PATCH', body: JSON.stringify({ channel_sku: editSku }) });
    setEditingId(null);
    setSaving(false);
    // Reload
    api(`/marketplace/listings?product_id=${productId}`).then(r => r.ok ? r.json() : null).then(d => {
      if (!d) return;
      const arr = Array.isArray(d.listings) ? d.listings
        : Array.isArray(d.data) ? d.data
        : Array.isArray(d) ? d
        : [];
      setListings(arr);
    });
  }

  return (
    <div>
      <p style={{ color: 'var(--text-muted)', fontSize: 13, marginBottom: 16 }}>Channel SKU mappings for this product across all connected marketplaces.</p>
      {listings.length === 0 ? (
        <div style={{ textAlign: 'center', padding: 32, color: 'var(--text-muted)', fontSize: 13 }}>No listings found for this product.</div>
      ) : (
        <table style={{ width: '100%', borderCollapse: 'collapse' }}>
          <thead>
            <tr style={{ borderBottom: '1px solid var(--border)' }}>
              {['Channel', 'Account', 'Channel SKU', 'Status', 'Last Synced', ''].map(h => (
                <th key={h} style={{ padding: '8px 12px', textAlign: 'left', color: 'var(--text-muted)', fontSize: 11, fontWeight: 600 }}>{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {listings.map((l: any) => (
              <tr key={l.listing_id || l.id} style={{ borderBottom: '1px solid var(--border)' }}>
                <td style={{ padding: '10px 12px' }}>
                  <span style={{ fontWeight: 700, color: 'var(--text-primary)', textTransform: 'capitalize' }}>{l.channel || l.marketplace}</span>
                </td>
                <td style={{ padding: '10px 12px', color: 'var(--text-muted)', fontSize: 12 }}>{l.account_name || l.credential_id || '—'}</td>
                <td style={{ padding: '10px 12px' }}>
                  {editingId === (l.listing_id || l.id) ? (
                    <input autoFocus value={editSku} onChange={e => setEditSku(e.target.value)}
                      onKeyDown={e => e.key === 'Enter' && saveSku(l.listing_id || l.id)}
                      style={{ width: 150, padding: '4px 8px', background: 'var(--bg-secondary)', border: '1px solid var(--primary)', borderRadius: 4, color: 'var(--text-primary)', fontSize: 12 }} />
                  ) : (
                    <span style={{ fontFamily: 'monospace', color: 'var(--text-primary)', fontSize: 12 }}>{l.channel_sku || l.external_sku || '—'}</span>
                  )}
                </td>
                <td style={{ padding: '10px 12px' }}>
                  <span style={{ padding: '2px 8px', borderRadius: 4, fontSize: 11, fontWeight: 600, background: l.status === 'active' ? 'rgba(34,197,94,0.1)' : 'rgba(100,116,139,0.1)', color: l.status === 'active' ? '#22c55e' : '#64748b' }}>
                    {l.status || 'unknown'}
                  </span>
                </td>
                <td style={{ padding: '10px 12px', color: 'var(--text-muted)', fontSize: 11 }}>
                  {l.last_synced_at ? new Date(l.last_synced_at).toLocaleDateString() : '—'}
                </td>
                <td style={{ padding: '10px 12px' }}>
                  {editingId === (l.listing_id || l.id) ? (
                    <div style={{ display: 'flex', gap: 4 }}>
                      <button onClick={() => saveSku(l.listing_id || l.id)} disabled={saving} style={{ padding: '3px 8px', background: 'var(--primary)', border: 'none', borderRadius: 4, color: 'white', fontSize: 11, cursor: 'pointer' }}>Save</button>
                      <button onClick={() => setEditingId(null)} style={{ padding: '3px 8px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 4, color: 'var(--text-muted)', fontSize: 11, cursor: 'pointer' }}>✕</button>
                    </div>
                  ) : (
                    <button onClick={() => { setEditingId(l.listing_id || l.id); setEditSku(l.channel_sku || l.external_sku || ''); }}
                      style={{ padding: '3px 8px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 4, color: 'var(--text-muted)', cursor: 'pointer', fontSize: 11 }}>Edit SKU</button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}

// ── STOCK HISTORY TAB ─────────────────────────────────────────────────────────

export function StockHistoryTab({ productId }: { productId: string }) {
  const [adjustments, setAdjustments] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    api(`/products/${productId}/stock-history`).then(r => r.ok ? r.json() : null).then(d => {
      if (d) setAdjustments(d.adjustments || []);
      setLoading(false);
    });
  }, [productId]);

  const typeIcon = (type: string) => ({ sale: '🛒', adjustment: '⚙️', receipt: '📥', stock_in: '📥', scrap: '🗑️', transfer: '↔️', return: '↩️', supplier_return: '↩️', count: '📝' })[type] || '📋';

  return (
    <div>
      <p style={{ color: 'var(--text-muted)', fontSize: 13, marginBottom: 16 }}>Full audit trail of all stock movements for this product.</p>
      {loading ? (
        <div style={{ textAlign: 'center', padding: 32, color: 'var(--text-muted)' }}>Loading…</div>
      ) : adjustments.length === 0 ? (
        <div style={{ textAlign: 'center', padding: 32, color: 'var(--text-muted)', fontSize: 13 }}>No stock history yet.</div>
      ) : (
        <table style={{ width: '100%', borderCollapse: 'collapse' }}>
          <thead>
            <tr style={{ borderBottom: '1px solid var(--border)' }}>
              {['Date', 'Type', 'Change', 'Before', 'After', 'Location', 'Reason', 'Reference'].map(h => (
                <th key={h} style={{ padding: '8px 12px', textAlign: 'left', color: 'var(--text-muted)', fontSize: 11, fontWeight: 600 }}>{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {adjustments.map((a: any, i: number) => (
              <tr key={i} style={{ borderBottom: '1px solid var(--border)' }}>
                <td style={{ padding: '9px 12px', color: 'var(--text-muted)', fontSize: 11 }}>{a.created_at ? new Date(a.created_at).toLocaleString() : '—'}</td>
                <td style={{ padding: '9px 12px' }}>
                  <span style={{ fontSize: 12 }}>{typeIcon(a.type)} {a.type}</span>
                </td>
                <td style={{ padding: '9px 12px', fontWeight: 700, color: (a.delta || 0) >= 0 ? '#22c55e' : '#ef4444', fontSize: 13 }}>
                  {(a.delta || 0) >= 0 ? '+' : ''}{a.delta || 0}
                </td>
                <td style={{ padding: '9px 12px', color: 'var(--text-secondary)', fontSize: 12 }}>{a.quantity_before ?? '—'}</td>
                <td style={{ padding: '9px 12px', color: 'var(--text-secondary)', fontSize: 12 }}>{a.quantity_after ?? '—'}</td>
                <td style={{ padding: '9px 12px', color: 'var(--text-muted)', fontSize: 11 }}>{a.location_path || a.location_id || '—'}</td>
                <td style={{ padding: '9px 12px', color: 'var(--text-secondary)', fontSize: 12 }}>{a.reason || '—'}</td>
                <td style={{ padding: '9px 12px', color: 'var(--text-muted)', fontSize: 11, fontFamily: 'monospace' }}>{a.reference || a.order_id || a.po_id || '—'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}

// ── ITEM STATS TAB ────────────────────────────────────────────────────────────

export function ItemStatsTab({ productId }: { productId: string }) {
  const [stats, setStats] = useState<any>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    api(`/products/${productId}/stats`).then(r => r.ok ? r.json() : null).then(d => {
      if (d) setStats(d.stats);
      setLoading(false);
    });
  }, [productId]);

  if (loading) return <div style={{ textAlign: 'center', padding: 32, color: 'var(--text-muted)' }}>Loading stats…</div>;
  if (!stats) return <div style={{ textAlign: 'center', padding: 32, color: 'var(--text-muted)' }}>Could not load stats.</div>;

  const cards = [
    { label: 'Units Sold (30d)', value: stats.sold_30d, color: 'var(--primary)' },
    { label: 'Units Sold (90d)', value: stats.sold_90d, color: 'var(--primary)' },
    { label: 'Units Sold (365d)', value: stats.sold_365d, color: 'var(--primary)' },
    { label: 'Revenue (30d)', value: `£${(stats.revenue_30d || 0).toFixed(2)}`, color: '#22c55e' },
    { label: 'Revenue (90d)', value: `£${(stats.revenue_90d || 0).toFixed(2)}`, color: '#22c55e' },
    { label: 'Revenue (365d)', value: `£${(stats.revenue_365d || 0).toFixed(2)}`, color: '#22c55e' },
    { label: 'Returns', value: stats.return_count, color: '#ef4444' },
    { label: 'Avg Sale Price', value: `£${(stats.avg_sale_price || 0).toFixed(2)}`, color: '#fbbf24' },
  ];

  return (
    <div>
      <p style={{ color: 'var(--text-muted)', fontSize: 13, marginBottom: 20 }}>Historical sales performance for this product.</p>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, 1fr)', gap: 12 }}>
        {cards.map(card => (
          <div key={card.label} style={{ background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 10, padding: 16 }}>
            <div style={{ fontSize: 11, color: 'var(--text-muted)', marginBottom: 8, fontWeight: 600 }}>{card.label}</div>
            <div style={{ fontSize: 22, fontWeight: 700, color: card.color }}>{card.value}</div>
          </div>
        ))}
      </div>
    </div>
  );
}

// ── KTYPES TAB ────────────────────────────────────────────────────────────────

export function KTypesTab({ productId }: { productId: string }) {
  const [ktypes, setKtypes] = useState<any[]>([]);
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    api(`/products/${productId}/ktypes`).then(r => r.ok ? r.json() : null).then(d => {
      if (d) setKtypes(d.ktypes || []);
    });
  }, [productId]);

  function addRow() { setKtypes(prev => [...prev, { ktype_id: '', culture: 'en_GB', note: '', include_years: '', exclude_years: '' }]); }
  function removeRow(idx: number) { setKtypes(prev => prev.filter((_, i) => i !== idx)); }
  function updateRow(idx: number, field: string, value: string) { setKtypes(prev => prev.map((r, i) => i === idx ? { ...r, [field]: value } : r)); }

  async function save() {
    setSaving(true);
    const res = await api(`/products/${productId}/ktypes`, { method: 'PUT', body: JSON.stringify({ ktypes }) });
    if (res.ok) { setSaved(true); setTimeout(() => setSaved(false), 2000); }
    setSaving(false);
  }

  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 16 }}>
        <p style={{ color: 'var(--text-muted)', fontSize: 13, margin: 0 }}>Vehicle fitment data for eBay motor part listings.</p>
        <div style={{ display: 'flex', gap: 8 }}>
          <button onClick={addRow} style={{ padding: '6px 14px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-secondary)', fontSize: 12, cursor: 'pointer' }}>+ Add kType</button>
          <button onClick={save} disabled={saving} style={{ padding: '6px 14px', background: saved ? '#22c55e' : 'var(--primary)', border: 'none', borderRadius: 6, color: 'white', fontSize: 12, fontWeight: 700, cursor: 'pointer' }}>{saved ? '✅ Saved' : saving ? 'Saving…' : 'Save'}</button>
        </div>
      </div>
      {ktypes.length === 0 ? (
        <div style={{ textAlign: 'center', padding: 32, color: 'var(--text-muted)', fontSize: 13 }}>No kTypes added. Click "+ Add kType" to add vehicle fitment data.</div>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
          {ktypes.map((kt, idx) => (
            <div key={idx} style={{ display: 'flex', gap: 8, alignItems: 'center', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, padding: '10px 14px' }}>
              {[
                { key: 'ktype_id', placeholder: 'kType ID', width: 100 },
                { key: 'culture', placeholder: 'Culture (e.g. en_GB)', width: 120 },
                { key: 'note', placeholder: 'Note', width: 160 },
                { key: 'include_years', placeholder: 'Include Years', width: 120 },
                { key: 'exclude_years', placeholder: 'Exclude Years', width: 120 },
              ].map(f => (
                <input key={f.key} value={kt[f.key] || ''} onChange={e => updateRow(idx, f.key, e.target.value)} placeholder={f.placeholder}
                  style={{ width: f.width, padding: '6px 10px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', fontSize: 12 }} />
              ))}
              <button onClick={() => removeRow(idx)} style={{ padding: '6px 10px', background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 6, color: '#ef4444', cursor: 'pointer' }}>×</button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

// ── LISTING DESCRIPTIONS TAB — P0.1 ──────────────────────────────────────────

interface ListingDescription {
  description_id: string;
  credential_id: string;
  channel: string;
  account_name: string;
  title: string;
  description: string;
  price: number;
  sync_status: string;
  updated_at: string;
}

interface Credential {
  id: string;
  channel: string;
  account_name: string;
}

const CHANNEL_EMOJI: Record<string, string> = {
  amazon: '📦',
  ebay: '🏷️',
  temu: '🛍️',
  tiktok: '🎵',
  etsy: '🛍️',
  woocommerce: '🛒',
  magento: '🏪',
  bigcommerce: '🛒',
  onbuy: '🏷️',
  walmart: '🛒',
  kaufland: '🛒',
};

const SYNC_STATUS_ICON: Record<string, { icon: string; color: string; label: string }> = {
  save_required: { icon: '🟡', color: '#f59e0b', label: 'Save required' },
  pending:       { icon: '↻', color: '#3b82f6', label: 'Syncing' },
  success:       { icon: '✓', color: '#22c55e', label: 'Synced' },
  error:         { icon: '⚠', color: '#ef4444', label: 'Sync error' },
  no_change:     { icon: '⊘', color: '#6b7280', label: 'No change' },
};

export function ListingDescriptionsTab({ productId }: { productId: string }) {
  const [descriptions, setDescriptions] = useState<ListingDescription[]>([]);
  const [credentials, setCredentials] = useState<Credential[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState<string | null>(null);
  const [saved, setSaved] = useState<string | null>(null);
  const [forms, setForms] = useState<Record<string, { title: string; description: string; price: string }>>({});

  useEffect(() => { loadAll(); }, [productId]);

  async function loadAll() {
    setLoading(true);
    try {
      const [descRes, credRes] = await Promise.all([
        api(`/products/${productId}/listing-descriptions`),
        api('/marketplace/credentials'),
      ]);
      if (descRes.ok) {
        const d = await descRes.json();
        const list: ListingDescription[] = d.listing_descriptions || [];
        setDescriptions(list);
        // Pre-populate forms from saved data
        const initial: Record<string, { title: string; description: string; price: string }> = {};
        list.forEach(l => {
          initial[l.credential_id || l.description_id] = {
            title: l.title || '',
            description: l.description || '',
            price: l.price != null ? String(l.price) : '',
          };
        });
        setForms(initial);
      }
      if (credRes.ok) {
        const c = await credRes.json();
        setCredentials(Array.isArray(c.credentials) ? c.credentials : []);
      }
    } finally {
      setLoading(false);
    }
  }

  function getDescForCred(credId: string): ListingDescription | undefined {
    return descriptions.find(d => d.credential_id === credId);
  }

  function getForm(credId: string) {
    return forms[credId] || { title: '', description: '', price: '' };
  }

  function setFormField(credId: string, field: string, value: string) {
    setForms(prev => ({ ...prev, [credId]: { ...getForm(credId), [field]: value } }));
  }

  async function saveRow(cred: Credential) {
    const f = getForm(cred.id);
    const existing = getDescForCred(cred.id);
    const descId = existing?.description_id || 'new';
    setSaving(cred.id);
    try {
      const res = await api(`/products/${productId}/listing-descriptions/${descId}`, {
        method: 'PUT',
        body: JSON.stringify({
          credential_id: cred.id,
          channel: cred.channel,
          account_name: cred.account_name,
          title: f.title,
          description: f.description,
          price: parseFloat(f.price) || 0,
        }),
      });
      if (res.ok) {
        setSaved(cred.id);
        setTimeout(() => setSaved(null), 2000);
        loadAll();
      }
    } finally {
      setSaving(null);
    }
  }

  if (loading) return <div style={{ padding: 32, textAlign: 'center', color: 'var(--text-muted)' }}>Loading…</div>;

  if (credentials.length === 0) {
    return (
      <div style={{ padding: 32, textAlign: 'center' }}>
        <div style={{ fontSize: 40, marginBottom: 12 }}>🔗</div>
        <h3 style={{ color: 'var(--text-primary)' }}>No channels connected</h3>
        <p style={{ color: 'var(--text-muted)', fontSize: 14 }}>
          Connect marketplace channels in <strong>Marketplace → Connections</strong> to set per-channel prices, titles and descriptions.
        </p>
      </div>
    );
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <p style={{ color: 'var(--text-muted)', fontSize: 13, margin: 0 }}>
        Set per-channel prices, titles and HTML descriptions. Changes are saved to the product record and marked for sync.
      </p>
      {credentials.map(cred => {
        const existing = getDescForCred(cred.id);
        const f = getForm(cred.id);
        const syncCfg = existing ? (SYNC_STATUS_ICON[existing.sync_status] || SYNC_STATUS_ICON['no_change']) : null;
        const isSaving = saving === cred.id;
        const isSaved = saved === cred.id;

        return (
          <div key={cred.id} style={{
            background: 'var(--bg-elevated)', border: '1px solid var(--border)',
            borderRadius: 10, padding: 16, display: 'flex', flexDirection: 'column', gap: 12,
          }}>
            {/* Row header */}
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                <span style={{ fontSize: 20 }}>{CHANNEL_EMOJI[cred.channel] || '🌐'}</span>
                <div>
                  <div style={{ fontWeight: 600, color: 'var(--text-primary)', fontSize: 14 }}>{cred.account_name || cred.channel}</div>
                  <div style={{ fontSize: 11, color: 'var(--text-muted)', textTransform: 'capitalize' }}>{cred.channel}</div>
                </div>
              </div>
              <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                {syncCfg && (
                  <span style={{ fontSize: 12, color: syncCfg.color, display: 'flex', alignItems: 'center', gap: 4 }}>
                    {syncCfg.icon} {syncCfg.label}
                  </span>
                )}
                <button
                  onClick={() => saveRow(cred)}
                  disabled={isSaving}
                  style={{
                    padding: '6px 14px', borderRadius: 6, border: 'none', cursor: 'pointer',
                    fontWeight: 600, fontSize: 12,
                    background: isSaved ? '#22c55e' : 'var(--primary)', color: 'white',
                  }}
                >
                  {isSaved ? '✅ Saved' : isSaving ? 'Saving…' : 'Save'}
                </button>
              </div>
            </div>

            {/* Fields */}
            <div style={{ display: 'grid', gridTemplateColumns: '1fr auto', gap: 12, alignItems: 'start' }}>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                <input
                  type="text"
                  placeholder={`Title for ${cred.account_name || cred.channel}`}
                  value={f.title}
                  onChange={e => setFormField(cred.id, 'title', e.target.value)}
                  style={{
                    background: 'var(--bg-secondary)', border: '1px solid var(--border)',
                    borderRadius: 6, padding: '8px 12px', color: 'var(--text-primary)', fontSize: 13, width: '100%', boxSizing: 'border-box',
                  }}
                />
                <textarea
                  placeholder="HTML description (optional)"
                  value={f.description}
                  rows={3}
                  onChange={e => setFormField(cred.id, 'description', e.target.value)}
                  style={{
                    background: 'var(--bg-secondary)', border: '1px solid var(--border)',
                    borderRadius: 6, padding: '8px 12px', color: 'var(--text-primary)', fontSize: 13,
                    width: '100%', boxSizing: 'border-box', resize: 'vertical', fontFamily: 'monospace',
                  }}
                />
              </div>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 4, minWidth: 120 }}>
                <label style={{ fontSize: 11, color: 'var(--text-muted)', fontWeight: 600, textTransform: 'uppercase' }}>Price (£)</label>
                <input
                  type="number"
                  step="0.01"
                  placeholder="0.00"
                  value={f.price}
                  onChange={e => setFormField(cred.id, 'price', e.target.value)}
                  style={{
                    background: 'var(--bg-secondary)', border: '1px solid var(--border)',
                    borderRadius: 6, padding: '8px 12px', color: 'var(--text-primary)', fontSize: 13, width: '100%', boxSizing: 'border-box',
                  }}
                />
              </div>
            </div>
          </div>
        );
      })}
    </div>
  );
}

// ── WMS TAB — P0.5 ───────────────────────────────────────────────────────────

interface StorageGroupItem {
  group_id: string;
  name: string;
  description: string;
}

interface AssignedBinrack {
  binrack_id: string;
  name: string;
  location_id: string;
  binrack_type: string;
  current_fill: number;
  capacity: number;
  status: string;
}

const BINRACK_TYPES = ['pick', 'replenishment', 'long_term', 'bulk'];
const BINRACK_TYPE_LABELS: Record<string, string> = {
  pick: '🪣 Pick',
  replenishment: '📦 Replenishment',
  long_term: '🏭 Long Term',
  bulk: '📫 Bulk',
};

export function WMSTab({ productId }: { productId: string }) {
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [savedMsg, setSavedMsg] = useState('');

  const [storageGroups, setStorageGroups] = useState<StorageGroupItem[]>([]);
  const [assignedBinracks, setAssignedBinracks] = useState<AssignedBinrack[]>([]);

  const [storageGroup, setStorageGroup] = useState('');
  const [binrackRestrictions, setBinrackRestrictions] = useState<string[]>([]);
  const [allowedBinrackTypes, setAllowedBinrackTypes] = useState<string[]>([]);

  useEffect(() => { loadAll(); }, [productId]);

  async function loadAll() {
    setLoading(true);
    try {
      const [wmsRes, sgRes] = await Promise.all([
        api(`/products/${productId}/wms-config`),
        api('/storage-groups'),
      ]);
      if (wmsRes.ok) {
        const d = await wmsRes.json();
        setStorageGroup(d.storage_group || '');
        setBinrackRestrictions(d.binrack_restrictions || []);
        setAllowedBinrackTypes(d.allowed_binrack_types || []);
        setAssignedBinracks(d.assigned_binracks || []);
      }
      if (sgRes.ok) {
        const d = await sgRes.json();
        setStorageGroups(d.storage_groups || []);
      }
    } finally {
      setLoading(false);
    }
  }

  async function save() {
    setSaving(true);
    try {
      const res = await api(`/products/${productId}/wms-config`, {
        method: 'PUT',
        body: JSON.stringify({ storage_group: storageGroup, binrack_restrictions: binrackRestrictions, allowed_binrack_types: allowedBinrackTypes }),
      });
      if (res.ok) {
        setSavedMsg('✅ Saved');
        setTimeout(() => setSavedMsg(''), 2000);
      }
    } finally {
      setSaving(false);
    }
  }

  function toggleType(type: string) {
    setAllowedBinrackTypes(prev =>
      prev.includes(type) ? prev.filter(t => t !== type) : [...prev, type]
    );
  }

  if (loading) return <div style={{ padding: 32, textAlign: 'center', color: 'var(--text-muted)' }}>Loading…</div>;

  const cardStyle: React.CSSProperties = {
    background: 'var(--bg-elevated)', border: '1px solid var(--border)',
    borderRadius: 10, padding: 16, marginBottom: 16,
  };
  const sectionTitleStyle: React.CSSProperties = {
    fontSize: 13, fontWeight: 700, color: 'var(--text-primary)', marginBottom: 12,
  };

  return (
    <div>
      {/* Storage Group */}
      <div style={cardStyle}>
        <div style={sectionTitleStyle}>🗄️ Storage Group</div>
        <p style={{ color: 'var(--text-muted)', fontSize: 12, marginBottom: 10 }}>
          Assign this product to a storage group to restrict it to compatible binracks.
        </p>
        <select
          className="select"
          value={storageGroup}
          onChange={e => setStorageGroup(e.target.value)}
          style={{ minWidth: 220 }}
        >
          <option value="">— No group assigned —</option>
          {storageGroups.map(g => (
            <option key={g.group_id} value={g.name}>{g.name}{g.description ? ` — ${g.description}` : ''}</option>
          ))}
        </select>
      </div>

      {/* Allowed Binrack Types */}
      <div style={cardStyle}>
        <div style={sectionTitleStyle}>📍 Allowed Binrack Types</div>
        <p style={{ color: 'var(--text-muted)', fontSize: 12, marginBottom: 10 }}>
          Restrict this product to specific binrack types. Leave all unchecked to allow any type.
        </p>
        <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap' }}>
          {BINRACK_TYPES.map(type => (
            <label key={type} style={{ display: 'flex', alignItems: 'center', gap: 6, cursor: 'pointer', fontSize: 13, color: 'var(--text-primary)' }}>
              <input
                type="checkbox"
                checked={allowedBinrackTypes.includes(type)}
                onChange={() => toggleType(type)}
                style={{ width: 16, height: 16, cursor: 'pointer' }}
              />
              {BINRACK_TYPE_LABELS[type]}
            </label>
          ))}
        </div>
      </div>

      {/* Assigned Binracks */}
      <div style={cardStyle}>
        <div style={sectionTitleStyle}>📌 Currently Assigned Binracks</div>
        {assignedBinracks.length === 0 ? (
          <p style={{ color: 'var(--text-muted)', fontSize: 13, margin: 0 }}>
            This product is not currently assigned to any specific binracks. Stock is tracked at the location level.
          </p>
        ) : (
          <table className="table" style={{ marginTop: 0 }}>
            <thead>
              <tr>
                <th>Binrack</th>
                <th>Type</th>
                <th>Fill</th>
                <th>Status</th>
              </tr>
            </thead>
            <tbody>
              {assignedBinracks.map(br => (
                <tr key={br.binrack_id}>
                  <td style={{ fontWeight: 500 }}>{br.name}</td>
                  <td><span style={{ fontSize: 12, color: 'var(--text-muted)' }}>{BINRACK_TYPE_LABELS[br.binrack_type] || br.binrack_type}</span></td>
                  <td>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                      <div style={{ width: 80, height: 6, background: 'var(--bg-secondary)', borderRadius: 3 }}>
                        <div style={{
                          width: `${br.capacity ? Math.min(100, (br.current_fill / br.capacity) * 100) : 0}%`,
                          height: '100%', background: 'var(--primary)', borderRadius: 3,
                        }} />
                      </div>
                      <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>{br.current_fill}/{br.capacity}</span>
                    </div>
                  </td>
                  <td>
                    <span style={{
                      fontSize: 11, padding: '2px 6px', borderRadius: 4,
                      background: br.status === 'active' ? 'rgba(34,197,94,0.15)' : 'rgba(107,114,128,0.15)',
                      color: br.status === 'active' ? '#22c55e' : '#6b7280',
                    }}>
                      {br.status}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {/* Save button */}
      <div style={{ display: 'flex', gap: 10, alignItems: 'center' }}>
        <button
          onClick={save}
          disabled={saving}
          style={{
            padding: '8px 20px', background: 'var(--primary)', border: 'none',
            borderRadius: 6, color: 'white', fontWeight: 600, fontSize: 13, cursor: 'pointer',
          }}
        >
          {saving ? 'Saving…' : 'Save WMS Settings'}
        </button>
        {savedMsg && <span style={{ color: '#22c55e', fontSize: 13, fontWeight: 600 }}>{savedMsg}</span>}
      </div>
    </div>
  );
}

// ─── P1.7  Audit Trail Tab ────────────────────────────────────────────────────

interface AuditEntry {
  audit_id: string;
  event_type: string;
  location_path: string;
  delta: number;
  quantity_after: number;
  reason: string;
  reference: string;
  created_by: string;
  created_at: string;
}

const AUDIT_TYPE_ICONS: Record<string, string> = {
  sale: '🛒', receipt: '📥', count: '📝', scrap: '🗑️',
  transfer: '↔️', return: '↩️', adjustment: '✏️',
};

export function AuditTrailTab({ productId }: { productId: string }) {
  const [entries, setEntries] = useState<AuditEntry[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [typeFilter, setTypeFilter] = useState('');
  const [loading, setLoading] = useState(true);

  const load = useCallback(async (p = 1) => {
    setLoading(true);
    try {
      let url = `/products/${productId}/audit?page=${p}&page_size=50`;
      if (typeFilter) url += `&type=${typeFilter}`;
      const res = await api(url);
      if (res.ok) {
        const d = await res.json();
        setEntries(d.entries || []);
        setTotal(d.total || 0);
        setPage(p);
      }
    } finally {
      setLoading(false);
    }
  }, [productId, typeFilter]);

  useEffect(() => { load(1); }, [load]);

  const totalPages = Math.ceil(total / 50);

  return (
    <div style={{ padding: '20px 24px' }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 16 }}>
        <div>
          <h3 style={{ margin: 0, fontSize: 15, fontWeight: 600, color: 'var(--text-primary)' }}>Stock Audit Trail</h3>
          <p style={{ margin: '2px 0 0', fontSize: 12, color: 'var(--text-muted)' }}>{total} total events</p>
        </div>
        <select
          value={typeFilter}
          onChange={e => { setTypeFilter(e.target.value); load(1); }}
          style={{ padding: '6px 10px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-secondary)', fontSize: 13, outline: 'none' }}
        >
          <option value="">All event types</option>
          <option value="sale">Sales</option>
          <option value="receipt">Receipts (PO)</option>
          <option value="adjustment">Adjustments</option>
          <option value="count">Stock Counts</option>
          <option value="transfer">Transfers</option>
          <option value="return">Returns</option>
          <option value="scrap">Scrap</option>
        </select>
      </div>

      {loading ? (
        <div style={{ padding: 32, textAlign: 'center', color: 'var(--text-muted)', fontSize: 13 }}>Loading audit trail…</div>
      ) : entries.length === 0 ? (
        <div style={{ padding: '32px', textAlign: 'center', color: 'var(--text-muted)', fontSize: 13 }}>
          No stock movements recorded{typeFilter ? ` for type "${typeFilter}"` : ''}.
        </div>
      ) : (
        <>
          <div style={{ border: '1px solid var(--border)', borderRadius: 8, overflow: 'hidden' }}>
            <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
              <thead>
                <tr style={{ background: 'var(--bg-elevated)' }}>
                  {['Date', 'Type', 'Location', 'Change', 'After', 'Reference', 'By'].map(h => (
                    <th key={h} style={{ padding: '8px 14px', textAlign: 'left', fontSize: 11, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--text-muted)', borderBottom: '1px solid var(--border)' }}>{h}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {entries.map(e => (
                  <tr key={e.audit_id} style={{ borderTop: '1px solid var(--border)' }}>
                    <td style={{ padding: '8px 14px', fontSize: 12, color: 'var(--text-muted)', whiteSpace: 'nowrap' }}>
                      {new Date(e.created_at).toLocaleDateString()} {new Date(e.created_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
                    </td>
                    <td style={{ padding: '8px 14px' }}>
                      <span title={e.event_type} style={{ fontSize: 14 }}>{AUDIT_TYPE_ICONS[e.event_type] || '•'}</span>
                      <span style={{ marginLeft: 6, fontSize: 12, color: 'var(--text-secondary)', textTransform: 'capitalize' }}>{e.event_type}</span>
                    </td>
                    <td style={{ padding: '8px 14px', fontSize: 12, color: 'var(--text-muted)', maxWidth: 140, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                      {e.location_path || '—'}
                    </td>
                    <td style={{ padding: '8px 14px', fontWeight: 600, color: e.delta > 0 ? 'var(--success)' : 'var(--danger)' }}>
                      {e.delta > 0 ? `+${e.delta}` : e.delta}
                    </td>
                    <td style={{ padding: '8px 14px', color: 'var(--text-secondary)' }}>{e.quantity_after}</td>
                    <td style={{ padding: '8px 14px', fontSize: 12, color: 'var(--text-muted)', maxWidth: 120, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                      {e.reference || e.reason || '—'}
                    </td>
                    <td style={{ padding: '8px 14px', fontSize: 12, color: 'var(--text-muted)' }}>{e.created_by || '—'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {totalPages > 1 && (
            <div style={{ display: 'flex', justifyContent: 'center', gap: 8, marginTop: 16 }}>
              <button
                disabled={page <= 1}
                onClick={() => load(page - 1)}
                style={{ padding: '6px 12px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 4, color: 'var(--text-secondary)', cursor: page > 1 ? 'pointer' : 'not-allowed', fontSize: 12 }}
              >← Prev</button>
              <span style={{ alignSelf: 'center', fontSize: 12, color: 'var(--text-muted)' }}>Page {page} of {totalPages}</span>
              <button
                disabled={page >= totalPages}
                onClick={() => load(page + 1)}
                style={{ padding: '6px 12px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 4, color: 'var(--text-secondary)', cursor: page < totalPages ? 'pointer' : 'not-allowed', fontSize: 12 }}
              >Next →</button>
            </div>
          )}
        </>
      )}
    </div>
  );
}

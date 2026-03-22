import { useState, useEffect, useCallback, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import { Search, RefreshCw, ChevronLeft, ChevronRight, Filter, X } from 'lucide-react';
import { getActiveTenantId } from '../contexts/TenantContext';
import './Orders.css';

interface RawOrder {
  order_id?: string; id?: string; external_order_id?: string; order_number?: string;
  channel?: string; status?: string; order_date?: string; created_at?: string;
  customer?: { name?: string; email?: string };
  shipping_address?: { name?: string; address_line1?: string; city?: string; country?: string; postal_code?: string };
  totals?: { grand_total?: { amount: number; currency: string } };
  tracking_number?: string; label_url?: string; shipping_service?: string;
}

function getOrderId(o: RawOrder) { return o.order_id || o.id || o.external_order_id || ''; }
function getDisplayRef(o: RawOrder) { return o.external_order_id || o.order_number || o.order_id || o.id || '—'; }
function fmtDate(s?: string) {
  if (!s) return '—';
  const d = new Date(s);
  return isNaN(d.getTime()) ? '—' : d.toLocaleDateString('en-GB', { day: 'numeric', month: 'short', year: 'numeric' });
}
function fmtMoney(amount: number, currency: string) {
  return new Intl.NumberFormat('en-GB', { style: 'currency', currency: currency || 'GBP', minimumFractionDigits: 2 }).format(amount);
}
const StatusBadge = ({ status }: { status?: string }) => {
  const map: Record<string, { label: string; cls: string }> = {
    dispatched: { label: 'Dispatched', cls: 'sb-fulfilled' },
    completed:  { label: 'Completed',  cls: 'sb-fulfilled' },
    cancelled:  { label: 'Cancelled',  cls: 'sb-cancelled' },
  };
  const s = map[status || ''] || { label: status || 'Unknown', cls: 'sb-default' };
  return <span className={`status-badge ${s.cls}`}>{s.label}</span>;
};

function today() { return new Date().toISOString().split('T')[0]; }
function daysAgo(n: number) {
  const d = new Date(); d.setDate(d.getDate() - n);
  return d.toISOString().split('T')[0];
}

const ProcessedOrders = () => {
  const navigate = useNavigate();
  const [orders, setOrders] = useState<RawOrder[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [search, setSearch] = useState('');
  const [statusFilter, setStatusFilter] = useState('dispatched,completed');
  const [page, setPage] = useState(1);
  const [pageSize] = useState(50);
  const [showFilters, setShowFilters] = useState(false);
  const [shippingServiceFilter, setShippingServiceFilter] = useState('');
  const [dateFrom, setDateFrom] = useState('');
  const [dateTo, setDateTo] = useState('');
  const [channelFilter, setChannelFilter] = useState('');
  const [reprintingId, setReprintingId] = useState<string | null>(null);
  const [reprocessingId, setReprocessingId] = useState<string | null>(null);
  const searchTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const API_BASE = (import.meta as any).env?.VITE_API_URL || 'https://marketmate-api-487246736287.us-central1.run.app/api/v1';
  const tenantId = getActiveTenantId() || localStorage.getItem('tenantId') || '';

  const loadOrders = useCallback(async () => {
    setLoading(true);
    try {
      const params = new URLSearchParams();
      params.set('status', statusFilter);
      params.set('limit', String(pageSize));
      params.set('offset', String((page - 1) * pageSize));
      params.set('sort_by', 'created_at');
      params.set('sort_order', 'desc');
      if (search) { params.set('search', search); params.set('search_field', 'pii_email_token'); }
      if (shippingServiceFilter) params.set('shipping_service', shippingServiceFilter);
      if (channelFilter) params.set('channel', channelFilter);
      if (dateFrom) params.set('received_from', dateFrom);
      if (dateTo) params.set('received_to', dateTo);
      const res = await fetch(`${API_BASE}/orders?${params.toString()}`, { headers: { 'X-Tenant-Id': tenantId } });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = await res.json();
      setOrders(data.orders || []);
      setTotal(data.total || 0);
    } catch (err) { console.error('Failed to load processed orders:', err); setOrders([]); }
    finally { setLoading(false); }
  }, [tenantId, search, statusFilter, page, pageSize, API_BASE, shippingServiceFilter, channelFilter, dateFrom, dateTo]);

  useEffect(() => { loadOrders(); }, [loadOrders]);
  useEffect(() => {
    if (searchTimer.current) clearTimeout(searchTimer.current);
    searchTimer.current = setTimeout(() => setPage(1), 400);
    return () => { if (searchTimer.current) clearTimeout(searchTimer.current); };
  }, [search]);

  const totalPages = Math.max(1, Math.ceil(total / pageSize));

  const reprintLabel = async (order: RawOrder) => {
    if (order.label_url) { window.open(order.label_url, '_blank'); return; }
    const id = getOrderId(order);
    setReprintingId(id);
    try {
      const res = await fetch(`${API_BASE}/dispatch/shipments?order_id=${id}`, { headers: { 'X-Tenant-Id': tenantId } });
      if (res.ok) {
        const data = await res.json();
        const latest = (data.shipments || data.data || [])[0];
        if (latest?.label_url) window.open(latest.label_url, '_blank');
        else alert('No label found for this order.');
      } else { alert('Could not fetch label. Please try from the shipping dashboard.'); }
    } catch { alert('Reprint failed. Please try again.'); }
    finally { setReprintingId(null); }
  };

  const reprocessOrder = async (order: RawOrder) => {
    const id = getOrderId(order);
    if (!window.confirm(`Move order ${getDisplayRef(order)} back to "processing"? It will reappear in open orders.`)) return;
    setReprocessingId(id);
    try {
      const res = await fetch(`${API_BASE}/orders/${id}/status`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': tenantId },
        body: JSON.stringify({ status: 'processing' }),
      });
      if (!res.ok) { const e = await res.json().catch(() => ({})); throw new Error(e.error || `HTTP ${res.status}`); }
      await loadOrders();
    } catch (err: any) { alert(`Re-process failed: ${err.message}`); }
    finally { setReprocessingId(null); }
  };

  const clearFilters = () => { setSearch(''); setStatusFilter('dispatched,completed'); setShippingServiceFilter(''); setChannelFilter(''); setDateFrom(''); setDateTo(''); setPage(1); };
  const activeFilterCount = [shippingServiceFilter, channelFilter, dateFrom, dateTo].filter(Boolean).length;

  return (
    <div className="page orders-page">
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 16 }}>
        <div>
          <h1 style={{ fontSize: 22, fontWeight: 700, color: 'var(--text-primary)', margin: 0 }}>Processed Orders</h1>
          <p style={{ fontSize: 13, color: 'var(--text-muted)', margin: '4px 0 0' }}>Dispatched and completed orders</p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <button className="btn-sec" onClick={() => navigate('/orders')} style={{ fontSize: 13 }}>← Open Orders</button>
          <button className="btn-sec" onClick={loadOrders}><RefreshCw size={14} /> Refresh</button>
        </div>
      </div>

      <div className="toolbar">
        <div className="search-box" style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
          <Search size={15} color="var(--text-muted)" />
          <input placeholder="Search by email, reference…" value={search} onChange={e => setSearch(e.target.value)} style={{ flex: 1 }} />
          {search && <button onClick={() => setSearch('')} style={{ background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-muted)', padding: 0 }}><X size={14} /></button>}
        </div>
        <div className="toolbar-actions">
          <select value={statusFilter} onChange={e => { setStatusFilter(e.target.value); setPage(1); }}
            style={{ fontSize: 12, padding: '6px 10px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)' }}>
            <option value="dispatched,completed">Dispatched &amp; Completed</option>
            <option value="dispatched">Dispatched only</option>
            <option value="completed">Completed only</option>
            <option value="cancelled">Cancelled</option>
          </select>
          <button className="btn-sec" onClick={() => setShowFilters(v => !v)} style={{ position: 'relative' }}>
            <Filter size={14} /> Filters
            {activeFilterCount > 0 && (
              <span style={{ position: 'absolute', top: -6, right: -6, background: 'var(--primary)', color: '#fff', borderRadius: '50%', width: 16, height: 16, fontSize: 10, fontWeight: 700, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>{activeFilterCount}</span>
            )}
          </button>
        </div>
      </div>

      {showFilters && (
        <div style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 8, padding: '16px 20px', marginBottom: 8, display: 'flex', gap: 20, flexWrap: 'wrap', alignItems: 'flex-end' }}>
          {[
            { label: 'Date From', val: dateFrom, setter: setDateFrom },
            { label: 'Date To', val: dateTo, setter: setDateTo },
          ].map(({ label, val, setter }) => (
            <div key={label} style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
              <label style={{ fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: 0.5 }}>{label}</label>
              <input type="date" value={val} onChange={e => { setter(e.target.value); setPage(1); }}
                style={{ padding: '6px 10px', background: 'var(--bg-tertiary)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', fontSize: 13 }} />
            </div>
          ))}
          <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
            <label style={{ fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: 0.5 }}>Quick Range</label>
            <div style={{ display: 'flex', gap: 6 }}>
              {[{ label: 'Today', from: today(), to: today() }, { label: '7d', from: daysAgo(7), to: today() }, { label: '30d', from: daysAgo(30), to: today() }].map(p => (
                <button key={p.label} className="btn-sec" style={{ fontSize: 11, padding: '5px 10px' }} onClick={() => { setDateFrom(p.from); setDateTo(p.to); setPage(1); }}>{p.label}</button>
              ))}
            </div>
          </div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
            <label style={{ fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: 0.5 }}>Shipping Service</label>
            <input placeholder="e.g. Royal Mail 2nd Class" value={shippingServiceFilter} onChange={e => { setShippingServiceFilter(e.target.value); setPage(1); }}
              style={{ padding: '6px 10px', background: 'var(--bg-tertiary)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', fontSize: 13, width: 200 }} />
          </div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
            <label style={{ fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: 0.5 }}>Channel</label>
            <select value={channelFilter} onChange={e => { setChannelFilter(e.target.value); setPage(1); }}
              style={{ padding: '6px 10px', background: 'var(--bg-tertiary)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', fontSize: 13 }}>
              <option value="">All channels</option>
              <option value="amazon">Amazon</option><option value="ebay">eBay</option>
              <option value="shopify">Shopify</option><option value="temu">Temu</option>
              <option value="etsy">Etsy</option><option value="manual">Manual</option><option value="csv_import">CSV Import</option>
            </select>
          </div>
          <div style={{ alignSelf: 'flex-end' }}>
            <button className="btn-sec" onClick={clearFilters} style={{ fontSize: 12 }}><X size={12} /> Clear all</button>
          </div>
        </div>
      )}

      <div style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '8px 0', fontSize: 13, color: 'var(--text-muted)' }}>
        <span>{total} orders</span>
        <div style={{ marginLeft: 'auto', display: 'flex', gap: 4 }}>
          <button className="btn-sm" disabled={page <= 1} onClick={() => setPage(p => p - 1)}><ChevronLeft size={14} /></button>
          <span style={{ padding: '0 8px', lineHeight: '28px' }}>Page {page} / {totalPages}</span>
          <button className="btn-sm" disabled={page >= totalPages} onClick={() => setPage(p => p + 1)}><ChevronRight size={14} /></button>
        </div>
      </div>

      <div className="table-outer">
        <div className="table-scroll-x">
          <table className="orders-table">
            <thead>
              <tr>
                <th>Channel &amp; Reference</th><th>Customer</th><th>Date</th>
                <th>Value</th><th>Shipping</th><th>Tracking</th><th>Status</th><th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {loading ? (
                <tr><td colSpan={8} className="state-cell"><span className="spinner" /> Loading…</td></tr>
              ) : orders.length === 0 ? (
                <tr><td colSpan={8} className="state-cell">No processed orders found</td></tr>
              ) : orders.map(order => {
                const id = getOrderId(order);
                const custName = order.shipping_address?.name?.trim() || order.customer?.name?.trim() || '—';
                const location = [order.shipping_address?.city, order.shipping_address?.postal_code].filter(Boolean).join(', ');
                const gt = order.totals?.grand_total;
                return (
                  <tr key={id} className="order-row">
                    <td className="col-channel">
                      <div className="ch-badge">{(order.channel || '').toUpperCase() || '—'}</div>
                      <div className="order-ref">{getDisplayRef(order)}</div>
                    </td>
                    <td className="col-customer">
                      <div className="cust-name">{custName}</div>
                      {location && <div className="cust-loc">{location}</div>}
                    </td>
                    <td style={{ fontSize: 13, color: 'var(--text-secondary)' }}>{fmtDate(order.order_date || order.created_at)}</td>
                    <td style={{ fontSize: 13, fontWeight: 600 }}>{gt ? fmtMoney(gt.amount, gt.currency) : '—'}</td>
                    <td style={{ fontSize: 12, color: 'var(--text-muted)', maxWidth: 140 }}>{order.shipping_service || '—'}</td>
                    <td style={{ fontSize: 12 }}>
                      {order.tracking_number ? <span style={{ color: 'var(--primary)', fontWeight: 500 }}>{order.tracking_number}</span> : '—'}
                    </td>
                    <td><StatusBadge status={order.status} /></td>
                    <td>
                      <div style={{ display: 'flex', gap: 5, flexWrap: 'wrap' }}>
                        <button className="btn-sm btn-sec" style={{ fontSize: 11 }} onClick={() => navigate(`/rmas?order_id=${id}`)}>Return</button>
                        <button className="btn-sm btn-sec" style={{ fontSize: 11 }} onClick={() => reprintLabel(order)} disabled={reprintingId === id} title="Reprint shipping label">
                          {reprintingId === id ? '…' : '🖨 Label'}
                        </button>
                        <button className="btn-sm btn-sec" style={{ fontSize: 11, color: '#f59e0b' }} onClick={() => reprocessOrder(order)} disabled={reprocessingId === id} title="Move back to processing">
                          {reprocessingId === id ? '…' : '↩ Re-process'}
                        </button>
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </div>

      <div style={{ display: 'flex', justifyContent: 'center', gap: 4, padding: '12px 0' }}>
        <button className="btn-sm" disabled={page <= 1} onClick={() => { setPage(p => p - 1); window.scrollTo({ top: 0, behavior: 'smooth' }); }}><ChevronLeft size={14} /></button>
        <span style={{ padding: '0 12px', lineHeight: '28px', fontSize: 13, color: 'var(--text-muted)' }}>Page {page} / {totalPages}</span>
        <button className="btn-sm" disabled={page >= totalPages} onClick={() => { setPage(p => p + 1); window.scrollTo({ top: 0, behavior: 'smooth' }); }}><ChevronRight size={14} /></button>
      </div>
    </div>
  );
};

export default ProcessedOrders;

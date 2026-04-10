import { useState, useEffect, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { getActiveTenantId } from '../contexts/TenantContext';
import './RMAs.css';

const API_BASE = import.meta.env.VITE_API_URL ?? 'http://localhost:8080/api/v1';

function api(path: string, init?: RequestInit) {
  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      'X-Tenant-Id': getActiveTenantId(),
      ...(init?.headers || {}),
    },
  });
}

// ─── Types ────────────────────────────────────────────────────────────────────

interface RMACustomer { name: string; email?: string; address?: string; }

interface RefundDownload {
  refund_download_id: string;
  channel: string;
  credential_id: string;
  external_order_id: string;
  external_refund_id: string;
  order_id?: string;
  rma_id?: string;
  refund_date: string;
  refund_amount: number;
  currency: string;
  reason?: string;
  status: string; // unmatched | matched | rejected
  downloaded_at: string;
}

interface RMALine {
  line_id: string;
  product_id?: string;
  product_name: string;
  sku: string;
  qty_requested: number;
  qty_received: number;
  reason_code?: string;
  reason_detail?: string;
  condition?: string;
  disposition?: string;
  restock_location_id?: string;
  restock_qty?: number;
  pending_restock_qty?: number;
  restocked?: boolean;
}

interface RMA {
  rma_id: string;
  rma_number: string;
  order_id: string;
  order_number: string;
  channel: string;
  status: string;
  customer: RMACustomer;
  lines: RMALine[];
  refund_action?: string;
  refund_amount?: number;
  refund_currency?: string;
  tracking_number?: string;
  notes?: string;
  created_at: string;
  updated_at: string;
  resolved_at?: string;
}

// ─── Constants ────────────────────────────────────────────────────────────────

const STATUS_CONFIG: Record<string, { label: string; color: string; bg: string }> = {
  requested:       { label: 'Requested',       color: '#fbbf24', bg: 'rgba(251,191,36,0.12)' },
  authorised:      { label: 'Authorised',      color: '#60a5fa', bg: 'rgba(96,165,250,0.12)' },
  awaiting_return: { label: 'Awaiting Return', color: '#c084fc', bg: 'rgba(192,132,252,0.12)' },
  received:        { label: 'Received',        color: '#fb923c', bg: 'rgba(251,146,60,0.12)' },
  inspected:       { label: 'Inspected',       color: '#2dd4bf', bg: 'rgba(45,212,191,0.12)' },
  resolved:        { label: 'Resolved',        color: '#4ade80', bg: 'rgba(74,222,128,0.12)' },
};

const CHANNEL_ICONS: Record<string, string> = {
  amazon: '📦', ebay: '🛒', temu: '🏷️', manual: '📝',
};

const REASON_LABELS: Record<string, string> = {
  not_as_described: 'Not as described',
  damaged:          'Damaged',
  changed_mind:     'Changed mind',
  wrong_item:       'Wrong item',
  defective:        'Defective',
  other:            'Other',
};

const ALL_STATUSES = Object.keys(STATUS_CONFIG);

// ─── Create RMA Modal ─────────────────────────────────────────────────────────

interface CreateModalProps { onClose: () => void; onCreated: (rma: RMA) => void; }

function CreateRMAModal({ onClose, onCreated }: CreateModalProps) {
  const [orderSearch, setOrderSearch] = useState('');
  const [orders, setOrders] = useState<any[]>([]);
  const [selectedOrder, setSelectedOrder] = useState<any>(null);
  const [lines, setLines] = useState<any[]>([]);
  const [notes, setNotes] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  // Task 16: RMA type
  const [rmaType, setRmaType] = useState<'return' | 'exchange' | 'resend' | 'refund'>('return');
  // Task 16: Resend address (only shown for resend type)
  const [resendAddress, setResendAddress] = useState({ name: '', address_line1: '', city: '', postal_code: '', country: 'GB' });

  const searchOrders = useCallback(async (q: string) => {
    if (!q || q.length < 2) { setOrders([]); return; }
    try {
      const res = await api(`/orders?search=${encodeURIComponent(q)}&limit=10`);
      const data = await res.json();
      setOrders(data.orders || data.data?.orders || []);
    } catch { setOrders([]); }
  }, []);

  useEffect(() => {
    const t = setTimeout(() => searchOrders(orderSearch), 300);
    return () => clearTimeout(t);
  }, [orderSearch, searchOrders]);

  const selectOrder = (order: any) => {
    setSelectedOrder(order);
    setOrders([]);
    // Pre-fill lines from order items
    const items = order.items || order.line_items || [];
    setLines(items.map((item: any, i: number) => ({
      line_id: `line-${i}`,
      product_name: item.title || item.product_name || '',
      sku: item.sku || '',
      qty_requested: item.quantity || 1,
      reason_code: 'other',
      reason_detail: '',
      refund_amount: '', // Task 16: per-line refund amount
      refund_currency: 'GBP',
    })));
    // Pre-fill resend address from order shipping address
    if (order.shipping_address) {
      setResendAddress({
        name: order.shipping_address.name || '',
        address_line1: order.shipping_address.address_line1 || '',
        city: order.shipping_address.city || '',
        postal_code: order.shipping_address.postal_code || '',
        country: order.shipping_address.country || 'GB',
      });
    }
  };

  const updateLine = (idx: number, field: string, value: any) => {
    setLines(prev => prev.map((l, i) => i === idx ? { ...l, [field]: value } : l));
  };

  const addLine = () => {
    setLines(prev => [...prev, { line_id: `line-${Date.now()}`, product_name: '', sku: '', qty_requested: 1, reason_code: 'other', reason_detail: '', refund_amount: '', refund_currency: 'GBP' }]);
  };

  const submit = async () => {
    if (!selectedOrder && !lines.length) { setError('Select an order or add lines manually'); return; }
    setSaving(true); setError('');
    try {
      const payload: any = {
        order_id: selectedOrder?.order_id || '',
        order_number: selectedOrder?.order_number || selectedOrder?.external_order_id || '',
        channel: selectedOrder?.channel || 'manual',
        rma_type: rmaType, // Task 16
        customer: {
          name: selectedOrder?.customer?.name || '',
          email: selectedOrder?.customer?.email || '',
          address: [
            selectedOrder?.shipping_address?.address_line1,
            selectedOrder?.shipping_address?.city,
            selectedOrder?.shipping_address?.postal_code,
          ].filter(Boolean).join(', '),
        },
        lines: lines.map(l => ({
          ...l,
          // Task 16: include refund amount if set
          refund_amount: l.refund_amount ? parseFloat(l.refund_amount) : undefined,
          refund_currency: l.refund_currency || 'GBP',
        })),
        notes,
      };
      // Task 16: include resend address for resend type
      if (rmaType === 'resend') {
        payload.resend_shipping_name = resendAddress.name;
        payload.resend_address_line1 = resendAddress.address_line1;
        payload.resend_city = resendAddress.city;
        payload.resend_postal_code = resendAddress.postal_code;
        payload.resend_country = resendAddress.country;
      }
      const res = await api('/rmas', { method: 'POST', body: JSON.stringify(payload) });
      if (!res.ok) { const d = await res.json(); throw new Error(d.error || 'Create failed'); }
      const data = await res.json();
      onCreated(data.rma);
    } catch (e: any) {
      setError(e.message);
    } finally { setSaving(false); }
  };

  return (
    <div className="rma-modal-overlay" onClick={onClose}>
      <div className="rma-modal" onClick={e => e.stopPropagation()}>
        <div className="rma-modal-header">
          <h2>Create RMA</h2>
          <button className="rma-modal-close" onClick={onClose}>✕</button>
        </div>

        <div className="rma-modal-body">
          {/* Order search */}
          <div className="rma-field">
            <label>Search order</label>
            <input
              className="rma-input"
              placeholder="Order number or customer name…"
              value={selectedOrder ? (selectedOrder.order_number || selectedOrder.external_order_id || selectedOrder.order_id) : orderSearch}
              onChange={e => { if (selectedOrder) { setSelectedOrder(null); setLines([]); } setOrderSearch(e.target.value); }}
            />
            {orders.length > 0 && (
              <div className="rma-order-dropdown">
                {orders.map(o => (
                  <button key={o.order_id} className="rma-order-option" onClick={() => selectOrder(o)}>
                    <span className="rma-order-num">#{o.order_number || o.external_order_id || o.order_id}</span>
                    <span className="rma-order-cust">{o.customer?.name}</span>
                    <span className="rma-order-ch">{o.channel}</span>
                  </button>
                ))}
              </div>
            )}
          </div>

          {/* Task 16: RMA Type */}
          <div className="rma-field">
            <label>RMA Type</label>
            <div style={{ display: 'flex', gap: 8 }}>
              {([['return', '↩ Return'], ['exchange', '🔄 Exchange'], ['resend', '📦 Resend'], ['refund', '💳 Refund']] as const).map(([val, label]) => (
                <button key={val} onClick={() => setRmaType(val)}
                  style={{ flex: 1, padding: '8px 4px', fontSize: 12, fontWeight: rmaType === val ? 700 : 400, borderRadius: 8, cursor: 'pointer', border: rmaType === val ? '2px solid var(--primary)' : '1px solid var(--border)', background: rmaType === val ? 'rgba(var(--primary-rgb, 99,102,241),0.1)' : 'var(--bg-secondary)', color: rmaType === val ? 'var(--primary)' : 'var(--text-muted)' }}>
                  {label}
                </button>
              ))}
            </div>
          </div>

          {/* Task 16: Resend address (only for resend type) */}
          {rmaType === 'resend' && (
            <div className="rma-field" style={{ background: 'var(--bg-secondary)', borderRadius: 8, padding: 12, border: '1px solid var(--border)' }}>
              <label style={{ marginBottom: 8, display: 'block', fontWeight: 600 }}>📦 Resend Shipping Address</label>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                <input className="rma-input" placeholder="Name" value={resendAddress.name} onChange={e => setResendAddress(a => ({ ...a, name: e.target.value }))} />
                <input className="rma-input" placeholder="Address Line 1" value={resendAddress.address_line1} onChange={e => setResendAddress(a => ({ ...a, address_line1: e.target.value }))} />
                <div style={{ display: 'flex', gap: 8 }}>
                  <input className="rma-input" placeholder="City" style={{ flex: 1 }} value={resendAddress.city} onChange={e => setResendAddress(a => ({ ...a, city: e.target.value }))} />
                  <input className="rma-input" placeholder="Postcode" style={{ width: 100 }} value={resendAddress.postal_code} onChange={e => setResendAddress(a => ({ ...a, postal_code: e.target.value }))} />
                  <input className="rma-input" placeholder="Country" style={{ width: 60 }} value={resendAddress.country} onChange={e => setResendAddress(a => ({ ...a, country: e.target.value }))} />
                </div>
              </div>
            </div>
          )}
          <div className="rma-field">
            <label>Return lines</label>
            {lines.map((line, i) => (
              <div key={line.line_id} className="rma-create-line" style={{ flexWrap: 'wrap', gap: 4 }}>
                <input className="rma-input rma-line-sku" placeholder="SKU" value={line.sku} onChange={e => updateLine(i, 'sku', e.target.value)} />
                <input className="rma-input rma-line-name" placeholder="Product name" value={line.product_name} onChange={e => updateLine(i, 'product_name', e.target.value)} />
                <input className="rma-input rma-line-qty" type="number" min={1} value={line.qty_requested} onChange={e => updateLine(i, 'qty_requested', parseInt(e.target.value) || 1)} />
                <select className="rma-select" value={line.reason_code} onChange={e => updateLine(i, 'reason_code', e.target.value)}>
                  {Object.entries(REASON_LABELS).map(([v, l]) => <option key={v} value={v}>{l}</option>)}
                </select>
                {/* Task 16: per-line refund amount */}
                {rmaType === 'refund' && (
                  <input className="rma-input" type="number" min={0} step="0.01" placeholder="Refund £"
                    style={{ width: 80 }} value={line.refund_amount || ''}
                    onChange={e => updateLine(i, 'refund_amount', e.target.value)} />
                )}
                <button className="rma-line-remove" onClick={() => setLines(prev => prev.filter((_, j) => j !== i))}>✕</button>
              </div>
            ))}
            <button className="rma-btn-ghost rma-add-line" onClick={addLine}>+ Add line</button>
          </div>

          <div className="rma-field">
            <label>Notes</label>
            <textarea className="rma-textarea" rows={3} placeholder="Internal notes…" value={notes} onChange={e => setNotes(e.target.value)} />
          </div>

          {error && <div className="rma-error">{error}</div>}
        </div>

        <div className="rma-modal-footer">
          <button className="rma-btn-ghost" onClick={onClose}>Cancel</button>
          <button className="rma-btn-primary" onClick={submit} disabled={saving}>
            {saving ? 'Creating…' : 'Create RMA'}
          </button>
        </div>
      </div>
    </div>
  );
}

// ─── Main List Page ───────────────────────────────────────────────────────────

export default function RMAs() {
  const navigate = useNavigate();
  const [rmas, setRMAs] = useState<RMA[]>([]);
  const [loading, setLoading] = useState(true);
  const [statusFilter, setStatusFilter] = useState('');
  const [channelFilter, setChannelFilter] = useState('');
  const [showCreate, setShowCreate] = useState(false);
  const [syncing, setSyncing] = useState(false);
  const [actionable, setActionable] = useState(0);
  // Task 16: RMA type sub-tab (return | exchange | resend | refund | all)
  const [rmaTypeTab, setRmaTypeTab] = useState<'all' | 'return' | 'exchange' | 'resend' | 'refund'>('all');
  // S2-Task3: RMA date range filter
  const [dateFrom, setDateFrom] = useState('');
  const [dateTo, setDateTo] = useState('');
  const [showFilters, setShowFilters] = useState(false);
  const [pageTab, setPageTab] = useState<'rmas' | 'refund_downloads'>('rmas');

  // Refund downloads state
  const [refundDownloads, setRefundDownloads] = useState<RefundDownload[]>([]);
  const [refundLoading, setRefundLoading] = useState(false);
  const [refundChannelFilter, setRefundChannelFilter] = useState('');
  const [refundStatusFilter, setRefundStatusFilter] = useState('');
  const [matchingId, setMatchingId] = useState<string | null>(null);
  const [matchRmaId, setMatchRmaId] = useState('');
  const [matchingTarget, setMatchingTarget] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      let url = '/rmas?';
      if (statusFilter)  url += `status=${statusFilter}&`;
      if (channelFilter) url += `channel=${channelFilter}&`;
      // Task 16: rma_type filter
      if (rmaTypeTab !== 'all') url += `rma_type=${rmaTypeTab}&`;
      // S2-Task3: date range filter
      if (dateFrom) url += `date_from=${dateFrom}&`;
      if (dateTo)   url += `date_to=${dateTo}&`;
      const res = await api(url);
      if (!res.ok) throw new Error('Failed to load');
      const data = await res.json();
      setRMAs(data.rmas || []);
      setActionable(data.actionable || 0);
    } catch { setRMAs([]); } finally { setLoading(false); }
  }, [statusFilter, channelFilter, rmaTypeTab, dateFrom, dateTo]);

  useEffect(() => { load(); }, [load]);
  useEffect(() => { load(); }, [rmaTypeTab]); // re-load when type tab changes

  const sync = async () => {
    setSyncing(true);
    try { await api('/rmas/sync', { method: 'POST' }); await load(); } finally { setSyncing(false); }
  };

  const loadRefundDownloads = useCallback(async () => {
    setRefundLoading(true);
    try {
      let url = '/refund-downloads?';
      if (refundChannelFilter) url += `channel=${refundChannelFilter}&`;
      if (refundStatusFilter)  url += `status=${refundStatusFilter}&`;
      const res = await api(url);
      if (!res.ok) throw new Error('Failed to load');
      const data = await res.json();
      setRefundDownloads(data.data || []);
    } catch { setRefundDownloads([]); } finally { setRefundLoading(false); }
  }, [refundChannelFilter, refundStatusFilter]);

  useEffect(() => {
    if (pageTab === 'refund_downloads') loadRefundDownloads();
  }, [pageTab, loadRefundDownloads]);

  const handleMatchToRMA = async (downloadId: string) => {
    if (!matchRmaId.trim()) return;
    setMatchingTarget(downloadId);
    try {
      await api(`/refund-downloads/${downloadId}/match-rma`, {
        method: 'POST',
        body: JSON.stringify({ rma_id: matchRmaId.trim() }),
      });
      setMatchingId(null);
      setMatchRmaId('');
      loadRefundDownloads();
    } catch { alert('Failed to match RMA'); }
    finally { setMatchingTarget(null); }
  };

  const statusCounts = ALL_STATUSES.reduce((acc, s) => {
    acc[s] = rmas.filter(r => r.status === s).length;
    return acc;
  }, {} as Record<string, number>);

  return (
    <div className="rma-page">
      {/* Header */}
      <div className="rma-header">
        <div className="rma-header-left">
          <h1>Returns <span className="rma-count-badge">{rmas.length}</span></h1>
          {actionable > 0 && <span className="rma-actionable-badge">⚡ {actionable} need action</span>}
        </div>
        <div className="rma-header-right">
          <button className="rma-btn-ghost" onClick={sync} disabled={syncing}>
            {syncing ? '⏳ Syncing…' : '🔄 Sync from marketplaces'}
          </button>
          <button className="rma-btn-primary" onClick={() => setShowCreate(true)}>+ Create RMA</button>
        </div>
      </div>

      {/* Page tabs */}
      <div style={{ display: 'flex', gap: 4, marginBottom: 20, borderBottom: '1px solid var(--border)', paddingBottom: 0 }}>
        {([
          { id: 'rmas', label: '📋 RMAs' },
          { id: 'refund_downloads', label: '⬇️ Refund Downloads' },
        ] as const).map(t => (
          <button key={t.id} onClick={() => setPageTab(t.id)} style={{
            padding: '10px 18px', background: 'none', border: 'none', cursor: 'pointer',
            borderBottom: pageTab === t.id ? '2px solid var(--primary)' : '2px solid transparent',
            color: pageTab === t.id ? 'var(--primary)' : 'var(--text-muted)',
            fontWeight: pageTab === t.id ? 700 : 400, fontSize: 14,
          }}>{t.label}</button>
        ))}
      </div>

      {/* ── RMAs Tab ── */}
      {pageTab === 'rmas' && (<>

      {/* Task 16: RMA type sub-tabs */}
      <div style={{ display: 'flex', gap: 4, marginBottom: 16, borderBottom: '1px solid var(--border)' }}>
        {(['all', 'return', 'exchange', 'resend', 'refund'] as const).map(t => {
          const labels: Record<string, string> = { all: '📋 All', return: '↩ Returns', exchange: '🔄 Exchanges', resend: '📦 Resends', refund: '💳 Refunds' };
          const count = t === 'all' ? rmas.length : rmas.filter(r => (r as any).rma_type === t).length;
          return (
            <button key={t} onClick={() => setRmaTypeTab(t)} style={{
              padding: '8px 14px', background: 'none', border: 'none', cursor: 'pointer',
              borderBottom: rmaTypeTab === t ? '2px solid var(--primary)' : '2px solid transparent',
              color: rmaTypeTab === t ? 'var(--primary)' : 'var(--text-muted)',
              fontWeight: rmaTypeTab === t ? 700 : 400, fontSize: 13,
            }}>
              {labels[t]} {count > 0 && <span style={{ fontSize: 11, background: 'var(--bg-secondary)', borderRadius: 10, padding: '1px 6px' }}>{count}</span>}
            </button>
          );
        })}
      </div>
      {/* Status summary bar */}
      <div className="rma-status-bar">
        <button className={`rma-status-pill ${statusFilter === '' ? 'active' : ''}`} onClick={() => setStatusFilter('')}>
          All <span className="rma-pill-count">{rmas.length}</span>
        </button>
        {ALL_STATUSES.map(s => (
          <button
            key={s}
            className={`rma-status-pill ${statusFilter === s ? 'active' : ''}`}
            style={statusFilter === s ? { background: STATUS_CONFIG[s].bg, color: STATUS_CONFIG[s].color, borderColor: STATUS_CONFIG[s].color } : {}}
            onClick={() => setStatusFilter(statusFilter === s ? '' : s)}
          >
            {STATUS_CONFIG[s].label}
            {statusCounts[s] > 0 && <span className="rma-pill-count">{statusCounts[s]}</span>}
          </button>
        ))}

        <div className="rma-filter-spacer" />
        <button
          className="rma-btn-ghost"
          onClick={() => setShowFilters(f => !f)}
          style={{ fontSize: 13, padding: '5px 12px', background: (dateFrom || dateTo) ? 'rgba(99,102,241,0.1)' : undefined, borderColor: (dateFrom || dateTo) ? 'rgba(99,102,241,0.5)' : undefined }}
        >
          {showFilters ? '▲' : '▼'} Date {(dateFrom || dateTo) ? '●' : ''}
        </button>
        <select className="rma-select" value={channelFilter} onChange={e => setChannelFilter(e.target.value)}>
          <option value="">All channels</option>
          <option value="amazon">Amazon</option>
          <option value="ebay">eBay</option>
          <option value="temu">Temu</option>
          <option value="manual">Manual</option>
        </select>
      </div>

      {showFilters && (
        <div style={{ display: 'flex', gap: 12, alignItems: 'center', padding: '12px 16px', background: 'var(--bg-secondary)', borderRadius: 8, border: '1px solid var(--border)', marginBottom: 16 }}>
          <label style={{ fontSize: 13, color: 'var(--text-muted)', whiteSpace: 'nowrap' }}>Created:</label>
          <input type="date" className="rma-select" value={dateFrom} onChange={e => setDateFrom(e.target.value)} style={{ width: 150 }} />
          <span style={{ color: 'var(--text-muted)', fontSize: 13 }}>to</span>
          <input type="date" className="rma-select" value={dateTo} onChange={e => setDateTo(e.target.value)} style={{ width: 150 }} />
          {(dateFrom || dateTo) && (
            <button className="rma-btn-ghost" style={{ fontSize: 12 }} onClick={() => { setDateFrom(''); setDateTo(''); }}>✕ Clear</button>
          )}
        </div>
      )}

      {/* Table */}
      {loading ? (
        <div className="rma-loading">Loading returns…</div>
      ) : rmas.length === 0 ? (
        <div className="rma-empty">
          <div className="rma-empty-icon">📭</div>
          <div className="rma-empty-title">No returns found</div>
          <div className="rma-empty-sub">
            {statusFilter ? `No RMAs with status "${STATUS_CONFIG[statusFilter]?.label}"` : 'Create your first RMA or sync from your marketplaces'}
          </div>
          <button className="rma-btn-primary" onClick={() => setShowCreate(true)}>+ Create RMA</button>
        </div>
      ) : (
        <div className="rma-table-wrap">
          <table className="rma-table">
            <thead>
              <tr>
                <th>RMA #</th>
                <th>Order #</th>
                <th>Customer</th>
                <th>Channel</th>
                <th>Lines</th>
                <th>Status</th>
                <th>Created</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {rmas.map(rma => {
                const sc = STATUS_CONFIG[rma.status] || STATUS_CONFIG.requested;
                return (
                  <tr key={rma.rma_id} className="rma-row" onClick={() => navigate(`/rmas/${rma.rma_id}`)}>
                    <td className="rma-num">{rma.rma_number}</td>
                    <td className="rma-order-num">
                      {rma.order_number ? <span className="rma-order-link">#{rma.order_number}</span> : <span className="rma-muted">—</span>}
                    </td>
                    <td>
                      <div className="rma-customer-name">{rma.customer?.name || '—'}</div>
                      {rma.customer?.email && <div className="rma-customer-email">{rma.customer.email}</div>}
                    </td>
                    <td>
                      <span className="rma-channel">
                        {CHANNEL_ICONS[rma.channel] || '📦'} {rma.channel}
                      </span>
                    </td>
                    <td className="rma-lines-count">{rma.lines?.length || 0}</td>
                    <td>
                      <span className="rma-status-badge" style={{ color: sc.color, background: sc.bg }}>
                        {sc.label}
                      </span>
                    </td>
                    <td className="rma-date">{new Date(rma.created_at).toLocaleDateString()}</td>
                    <td><button className="rma-view-btn" onClick={e => { e.stopPropagation(); navigate(`/rmas/${rma.rma_id}`); }}>View →</button></td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
      </>)}

      {/* ── Refund Downloads Tab ── */}
      {pageTab === 'refund_downloads' && (
        <div>
          {/* Filters */}
          <div style={{ display: 'flex', gap: 12, marginBottom: 16, alignItems: 'center' }}>
            <select className="rma-select" value={refundChannelFilter} onChange={e => setRefundChannelFilter(e.target.value)}>
              <option value="">All channels</option>
              <option value="amazon">Amazon</option>
              <option value="ebay">eBay</option>
              <option value="shopify">Shopify</option>
            </select>
            <select className="rma-select" value={refundStatusFilter} onChange={e => setRefundStatusFilter(e.target.value)}>
              <option value="">All statuses</option>
              <option value="unmatched">Unmatched</option>
              <option value="matched">Matched</option>
              <option value="rejected">Rejected</option>
            </select>
            <button className="rma-btn-ghost" onClick={loadRefundDownloads} disabled={refundLoading}>
              {refundLoading ? '⏳ Loading…' : '🔄 Refresh'}
            </button>
            <span style={{ fontSize: 12, color: 'var(--text-muted)', marginLeft: 'auto' }}>
              {refundDownloads.length} refund{refundDownloads.length !== 1 ? 's' : ''} downloaded
            </span>
          </div>

          {/* Info banner */}
          <div style={{ padding: '12px 16px', marginBottom: 16, borderRadius: 8, background: 'rgba(59,130,246,0.06)', border: '1px solid rgba(59,130,246,0.2)', fontSize: 13, color: 'var(--text-secondary)', lineHeight: 1.6 }}>
            ℹ️ Refund downloads are pulled from channels via the order detail page or automatically when an RMA is approved. Match each refund to an RMA to keep your records in sync.
          </div>

          {refundLoading ? (
            <div className="rma-loading">Loading refund downloads…</div>
          ) : refundDownloads.length === 0 ? (
            <div className="rma-empty">
              <div className="rma-empty-icon">💸</div>
              <div className="rma-empty-title">No refund downloads yet</div>
              <div className="rma-empty-sub">Refunds are downloaded from Amazon, eBay, and Shopify when an order's refund button is triggered from the order detail page.</div>
            </div>
          ) : (
            <div className="rma-table-wrap">
              <table className="rma-table">
                <thead>
                  <tr>
                    <th>Channel</th>
                    <th>External Order</th>
                    <th>Refund ID</th>
                    <th>Amount</th>
                    <th>Reason</th>
                    <th>Date</th>
                    <th>Status</th>
                    <th>Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {refundDownloads.map(rd => {
                    const statusColors: Record<string, { color: string; bg: string }> = {
                      unmatched: { color: '#f59e0b', bg: 'rgba(245,158,11,0.12)' },
                      matched:   { color: '#22c55e', bg: 'rgba(34,197,94,0.12)' },
                      rejected:  { color: '#ef4444', bg: 'rgba(239,68,68,0.12)' },
                    };
                    const sc = statusColors[rd.status] || statusColors.unmatched;
                    const isMatching = matchingId === rd.refund_download_id;
                    return (
                      <tr key={rd.refund_download_id} className="rma-row">
                        <td>
                          <span className="rma-channel">{CHANNEL_ICONS[rd.channel] || '📦'} {rd.channel}</span>
                        </td>
                        <td style={{ fontFamily: 'monospace', fontSize: 12 }}>{rd.external_order_id}</td>
                        <td style={{ fontFamily: 'monospace', fontSize: 12, color: 'var(--text-muted)' }}>
                          {rd.external_refund_id?.slice(0, 16)}{rd.external_refund_id?.length > 16 ? '…' : ''}
                        </td>
                        <td style={{ fontWeight: 700 }}>
                          {rd.currency} {rd.refund_amount?.toFixed(2) || '0.00'}
                        </td>
                        <td style={{ color: 'var(--text-muted)', fontSize: 12, maxWidth: 160 }}>
                          {rd.reason?.slice(0, 40) || '—'}
                        </td>
                        <td style={{ color: 'var(--text-muted)', fontSize: 12 }}>
                          {rd.refund_date ? new Date(rd.refund_date).toLocaleDateString() : '—'}
                        </td>
                        <td>
                          <span className="rma-status-badge" style={{ color: sc.color, background: sc.bg }}>
                            {rd.status}
                          </span>
                          {rd.rma_id && (
                            <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 2 }}>→ {rd.rma_id.slice(0, 8)}…</div>
                          )}
                        </td>
                        <td>
                          {rd.status === 'unmatched' && (
                            isMatching ? (
                              <div style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
                                <input
                                  placeholder="RMA ID"
                                  value={matchRmaId}
                                  onChange={e => setMatchRmaId(e.target.value)}
                                  style={{ padding: '4px 8px', fontSize: 12, border: '1px solid var(--border)', borderRadius: 6, background: 'var(--bg-tertiary)', color: 'var(--text-primary)', width: 120 }}
                                  onKeyDown={e => e.key === 'Enter' && handleMatchToRMA(rd.refund_download_id)}
                                />
                                <button
                                  onClick={() => handleMatchToRMA(rd.refund_download_id)}
                                  disabled={matchingTarget === rd.refund_download_id}
                                  style={{ padding: '4px 10px', fontSize: 12, background: 'var(--primary)', color: '#fff', border: 'none', borderRadius: 6, cursor: 'pointer' }}
                                >
                                  {matchingTarget === rd.refund_download_id ? '…' : 'Match'}
                                </button>
                                <button
                                  onClick={() => { setMatchingId(null); setMatchRmaId(''); }}
                                  style={{ padding: '4px 8px', fontSize: 12, background: 'none', border: '1px solid var(--border)', borderRadius: 6, cursor: 'pointer', color: 'var(--text-muted)' }}
                                >✕</button>
                              </div>
                            ) : (
                              <button
                                onClick={() => { setMatchingId(rd.refund_download_id); setMatchRmaId(''); }}
                                style={{ padding: '5px 12px', fontSize: 12, background: 'none', border: '1px solid var(--border)', borderRadius: 6, cursor: 'pointer', color: 'var(--text-secondary)', fontWeight: 600 }}
                              >
                                🔗 Match to RMA
                              </button>
                            )
                          )}
                          {rd.status === 'matched' && rd.rma_id && (
                            <button
                              onClick={() => navigate(`/rmas/${rd.rma_id}`)}
                              style={{ padding: '5px 12px', fontSize: 12, background: 'none', border: '1px solid var(--border)', borderRadius: 6, cursor: 'pointer', color: 'var(--primary)', fontWeight: 600 }}
                            >View RMA →</button>
                          )}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}

      {showCreate && (
        <CreateRMAModal
          onClose={() => setShowCreate(false)}
          onCreated={rma => { setShowCreate(false); navigate(`/rmas/${rma.rma_id}`); }}
        />
      )}
    </div>
  );
}

import { useState, useEffect } from 'react';
import { getActiveTenantId } from '../contexts/TenantContext';

const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';
function api(path: string, init?: RequestInit) {
  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId(), ...init?.headers },
  });
}

interface VendorOrder {
  vendor_order_id: string;
  amazon_po_number: string;
  status: string;
  lines: any[];
  ship_to: string;
  total_amount: number;
  currency: string;
  created_at: string;
  notes: string;
}

const statusColors: Record<string, string> = {
  new: '#3b82f6',
  accepted: '#22c55e',
  declined: '#ef4444',
};

export default function VendorOrders() {
  const [orders, setOrders] = useState<VendorOrder[]>([]);
  const [syncing, setSyncing] = useState(false);
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const [declineReason, setDeclineReason] = useState('');
  const [declinePO, setDeclinePO] = useState<string | null>(null);

  useEffect(() => { load(); }, []);

  async function load() {
    const res = await api('/vendor-orders');
    if (res.ok) {
      const data = await res.json();
      setOrders(data.vendor_orders || []);
    }
  }

  async function sync() {
    setSyncing(true);
    await api('/vendor-orders/sync', { method: 'POST' });
    await load();
    setSyncing(false);
  }

  async function accept(id: string) {
    setActionLoading(id + '_accept');
    await api(`/vendor-orders/${id}/accept`, { method: 'POST' });
    await load();
    setActionLoading(null);
  }

  async function decline(id: string) {
    setActionLoading(id + '_decline');
    await api(`/vendor-orders/${id}/decline`, { method: 'POST', body: JSON.stringify({ reason: declineReason }) });
    setDeclinePO(null);
    setDeclineReason('');
    await load();
    setActionLoading(null);
  }

  const formatCurrency = (amount: number, currency: string) =>
    new Intl.NumberFormat('en-GB', { style: 'currency', currency: currency || 'GBP' }).format(amount);

  return (
    <div style={{ padding: 24, maxWidth: 1100, margin: '0 auto' }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 24 }}>
        <div>
          <h1 style={{ color: 'var(--text-primary)', marginBottom: 4 }}>🏪 Amazon Vendor Central POs</h1>
          <p style={{ color: 'var(--text-muted)' }}>Receive and manage Amazon Vendor Central purchase orders.</p>
        </div>
        <button onClick={sync} disabled={syncing}
          style={{ padding: '10px 20px', background: syncing ? 'var(--bg-elevated)' : 'var(--primary)', border: '1px solid var(--border)', borderRadius: 8, color: syncing ? 'var(--text-muted)' : 'white', fontWeight: 700, cursor: 'pointer' }}>
          {syncing ? '⏳ Syncing…' : '🔄 Sync from Amazon'}
        </button>
      </div>

      {orders.length === 0 ? (
        <div style={{ textAlign: 'center', padding: 60, color: 'var(--text-muted)' }}>
          <div style={{ fontSize: 40 }}>🏪</div>
          <div style={{ marginTop: 12 }}>No vendor orders found.</div>
          <p style={{ fontSize: 13, marginTop: 8 }}>Click "Sync from Amazon" to pull the latest Vendor Central POs.</p>
        </div>
      ) : (
        <table style={{ width: '100%', borderCollapse: 'collapse' }}>
          <thead>
            <tr style={{ borderBottom: '1px solid var(--border)' }}>
              {['Amazon PO #', 'Lines', 'Ship To', 'Total', 'Status', 'Actions'].map(h => (
                <th key={h} style={{ padding: '10px 14px', textAlign: 'left', color: 'var(--text-muted)', fontSize: 12, fontWeight: 600 }}>{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {orders.map(order => (
              <tr key={order.vendor_order_id} style={{ borderBottom: '1px solid var(--border)' }}>
                <td style={{ padding: '14px', fontWeight: 700, color: 'var(--text-primary)', fontFamily: 'monospace' }}>
                  {order.amazon_po_number}
                </td>
                <td style={{ padding: '14px', color: 'var(--text-secondary)' }}>{order.lines?.length || 0} lines</td>
                <td style={{ padding: '14px', color: 'var(--text-secondary)', fontSize: 13 }}>{order.ship_to || '—'}</td>
                <td style={{ padding: '14px', color: 'var(--text-primary)', fontWeight: 600 }}>
                  {formatCurrency(order.total_amount, order.currency)}
                </td>
                <td style={{ padding: '14px' }}>
                  <span style={{
                    padding: '3px 10px', borderRadius: 6, fontSize: 12, fontWeight: 600,
                    background: `${statusColors[order.status] || '#64748b'}20`,
                    color: statusColors[order.status] || '#64748b',
                  }}>
                    {order.status}
                  </span>
                </td>
                <td style={{ padding: '14px' }}>
                  {order.status === 'new' && (
                    <div style={{ display: 'flex', gap: 8 }}>
                      <button
                        onClick={() => accept(order.vendor_order_id)}
                        disabled={actionLoading === order.vendor_order_id + '_accept'}
                        style={{ padding: '5px 12px', background: 'rgba(34,197,94,0.1)', border: '1px solid rgba(34,197,94,0.3)', borderRadius: 6, color: '#22c55e', cursor: 'pointer', fontSize: 12, fontWeight: 600 }}>
                        ✓ Accept
                      </button>
                      <button
                        onClick={() => setDeclinePO(order.vendor_order_id)}
                        style={{ padding: '5px 12px', background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 6, color: '#ef4444', cursor: 'pointer', fontSize: 12, fontWeight: 600 }}>
                        ✗ Decline
                      </button>
                    </div>
                  )}
                  {order.status !== 'new' && (
                    <span style={{ color: 'var(--text-muted)', fontSize: 12 }}>
                      {order.status === 'accepted' ? '✓ Accepted' : '✗ Declined'}
                    </span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {/* Decline modal */}
      {declinePO && (
        <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.6)', zIndex: 300, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
          <div style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 12, padding: 24, width: 400 }}>
            <h3 style={{ color: 'var(--text-primary)', marginBottom: 16 }}>Decline Vendor Order</h3>
            <div style={{ marginBottom: 16 }}>
              <label style={{ display: 'block', marginBottom: 6, fontSize: 13, color: 'var(--text-secondary)' }}>Reason (optional)</label>
              <textarea value={declineReason} onChange={e => setDeclineReason(e.target.value)} rows={3}
                style={{ width: '100%', padding: '10px 14px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-primary)', resize: 'vertical' }} />
            </div>
            <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 12 }}>
              <button onClick={() => { setDeclinePO(null); setDeclineReason(''); }}
                style={{ padding: '8px 16px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-secondary)', cursor: 'pointer' }}>
                Cancel
              </button>
              <button onClick={() => decline(declinePO)} disabled={!!actionLoading}
                style={{ padding: '8px 16px', background: '#ef4444', border: 'none', borderRadius: 8, color: 'white', fontWeight: 700, cursor: 'pointer' }}>
                Decline Order
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

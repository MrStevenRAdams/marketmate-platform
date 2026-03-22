import { useState, useEffect, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { getActiveTenantId } from '../contexts/TenantContext';

const API_BASE = import.meta.env.VITE_API_URL ?? 'http://localhost:8080/api/v1';

function api(path: string, init?: RequestInit) {
  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      'X-Tenant-Id': getActiveTenantId(),
      ...init?.headers,
    },
  });
}

// ─── Types ─────────────────────────────────────────────────────────────────────

interface SLASummary {
  overdue: number;
  due_today: number;
  due_tomorrow: number;
  on_track: number;
  no_sla: number;
  total_pending: number;
}

interface Order {
  order_id: string;
  external_order_id?: string;
  channel: string;
  status: string;
  sub_status?: string;
  despatch_by_date?: string;
  promised_ship_by?: string;
  sla_at_risk?: boolean;
  created_at: string;
  customer?: { name?: string };
  totals?: { grand_total?: { amount: number; currency: string } };
}

type Band = 'overdue' | 'today' | 'tomorrow' | 'on_track' | 'no_sla';

// ─── Helpers ───────────────────────────────────────────────────────────────────

function getBand(order: Order): Band {
  const slaStr = order.despatch_by_date || order.promised_ship_by;
  if (!slaStr) return 'no_sla';

  const slaDate = new Date(slaStr);
  if (isNaN(slaDate.getTime())) return 'no_sla';

  const now = new Date();
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const tomorrow = new Date(today); tomorrow.setDate(tomorrow.getDate() + 1);
  const dayAfter = new Date(today); dayAfter.setDate(dayAfter.getDate() + 2);
  const slaDay = new Date(slaDate.getFullYear(), slaDate.getMonth(), slaDate.getDate());

  if (slaDay < today) return 'overdue';
  if (slaDay >= today && slaDay < tomorrow) return 'today';
  if (slaDay >= tomorrow && slaDay < dayAfter) return 'tomorrow';
  return 'on_track';
}

function fmtDate(s?: string) {
  if (!s) return '—';
  const d = new Date(s);
  return isNaN(d.getTime()) ? '—' : d.toLocaleDateString('en-GB', { day: 'numeric', month: 'short', year: 'numeric' });
}

function fmtMoney(amount?: number, currency?: string) {
  if (amount == null) return '—';
  return new Intl.NumberFormat('en-GB', {
    style: 'currency', currency: currency || 'GBP', minimumFractionDigits: 2,
  }).format(amount);
}

function slaLabel(s?: string) {
  if (!s) return '—';
  const d = new Date(s);
  if (isNaN(d.getTime())) return '—';
  const now = new Date();
  const diff = Math.floor((d.getTime() - now.getTime()) / (1000 * 60 * 60 * 24));
  const dateStr = d.toLocaleDateString('en-GB', { day: 'numeric', month: 'short' });
  if (diff < 0) return `${dateStr} (${Math.abs(diff + 1)}d overdue)`;
  if (diff === 0) return `${dateStr} (today)`;
  return dateStr;
}

// ─── Band Config ───────────────────────────────────────────────────────────────

const BAND_CONFIG: Record<Band, { label: string; colour: string; bg: string; icon: string; description: string }> = {
  overdue:  { label: 'Overdue',     colour: '#ef4444', bg: 'rgba(239,68,68,0.10)',   icon: '🔴', description: 'Despatch date has passed' },
  today:    { label: 'Due Today',   colour: '#f59e0b', bg: 'rgba(245,158,11,0.10)',  icon: '🟡', description: 'Must despatch today' },
  tomorrow: { label: 'Due Tomorrow',colour: '#3b82f6', bg: 'rgba(59,130,246,0.10)',  icon: '🔵', description: 'Must despatch tomorrow' },
  on_track: { label: 'On Track',    colour: '#22c55e', bg: 'rgba(34,197,94,0.10)',   icon: '🟢', description: 'More than 2 days remaining' },
  no_sla:   { label: 'No SLA',      colour: '#94a3b8', bg: 'rgba(148,163,184,0.10)', icon: '⚪', description: 'No despatch date set' },
};

// ─── Main Component ─────────────────────────────────────────────────────────────

export default function SLADashboard() {
  const navigate = useNavigate();
  const [summary, setSummary] = useState<SLASummary | null>(null);
  const [orders, setOrders] = useState<Order[]>([]);
  const [loading, setLoading] = useState(true);
  const [ordersLoading, setOrdersLoading] = useState(false);
  const [activeBand, setActiveBand] = useState<Band | null>(null);
  const [error, setError] = useState('');
  const [lastRefresh, setLastRefresh] = useState(new Date());

  const loadSummary = useCallback(async () => {
    try {
      const res = await api('/dispatch/sla-summary');
      if (res.ok) {
        const data = await res.json();
        setSummary(data);
      }
    } catch (e) {
      console.error('SLA summary error:', e);
    }
  }, []);

  const loadOrders = useCallback(async (band: Band) => {
    setOrdersLoading(true);
    setOrders([]);
    try {
      const now = new Date();
      const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
      const tomorrow = new Date(today); tomorrow.setDate(tomorrow.getDate() + 1);
      const dayAfter = new Date(today); dayAfter.setDate(dayAfter.getDate() + 2);

      let dateFrom = '';
      let dateTo = '';
      const statuses = 'imported,processing,on_hold,ready_to_fulfil,ready_to_dispatch';

      switch (band) {
        case 'overdue':
          dateTo = today.toISOString().split('T')[0];
          break;
        case 'today':
          dateFrom = today.toISOString().split('T')[0];
          dateTo = tomorrow.toISOString().split('T')[0];
          break;
        case 'tomorrow':
          dateFrom = tomorrow.toISOString().split('T')[0];
          dateTo = dayAfter.toISOString().split('T')[0];
          break;
        case 'on_track':
          dateFrom = dayAfter.toISOString().split('T')[0];
          break;
      }

      const params = new URLSearchParams({ status: statuses, limit: '200', sort_by: 'despatch_by_date', sort_order: 'asc' });
      if (dateFrom) params.set('despatch_from', dateFrom);
      if (dateTo) params.set('despatch_to', dateTo);

      const res = await api(`/orders?${params.toString()}`);
      if (res.ok) {
        const data = await res.json();
        const allOrders: Order[] = data.orders || [];
        // Filter by band on the client side for accuracy
        const filtered = band === 'no_sla'
          ? allOrders.filter(o => !o.despatch_by_date && !o.promised_ship_by)
          : allOrders.filter(o => getBand(o) === band);
        setOrders(filtered);
      }
    } catch (e) {
      setError('Failed to load orders');
    } finally {
      setOrdersLoading(false);
    }
  }, []);

  useEffect(() => {
    const run = async () => {
      setLoading(true);
      await loadSummary();
      setLoading(false);
    };
    run();
  }, [loadSummary]);

  // Auto-refresh every 2 minutes
  useEffect(() => {
    const timer = setInterval(() => {
      loadSummary();
      if (activeBand) loadOrders(activeBand);
      setLastRefresh(new Date());
    }, 120_000);
    return () => clearInterval(timer);
  }, [loadSummary, loadOrders, activeBand]);

  const handleBandClick = (band: Band) => {
    if (activeBand === band) {
      setActiveBand(null);
      setOrders([]);
    } else {
      setActiveBand(band);
      loadOrders(band);
    }
  };

  const refresh = async () => {
    setLoading(true);
    await loadSummary();
    if (activeBand) await loadOrders(activeBand);
    setLastRefresh(new Date());
    setLoading(false);
  };

  const bands: Band[] = ['overdue', 'today', 'tomorrow', 'on_track', 'no_sla'];

  return (
    <div style={{ padding: '28px 32px', maxWidth: 1400, margin: '0 auto' }}>

      {/* Header */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 28 }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 24, fontWeight: 700, color: 'var(--text-primary)' }}>
            📅 SLA Dashboard
          </h1>
          <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 14 }}>
            Despatch deadlines by urgency band. Last updated {lastRefresh.toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit' })}.
          </p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <button
            onClick={() => navigate('/dispatch')}
            style={{ ...btnGhost, fontSize: 13 }}
          >
            🚀 Go to Despatch Console
          </button>
          <button onClick={refresh} disabled={loading} style={{ ...btnGhost, fontSize: 13 }}>
            {loading ? 'Refreshing…' : '↻ Refresh'}
          </button>
        </div>
      </div>

      {error && (
        <div style={{ marginBottom: 16, padding: '10px 14px', background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 6, color: '#ef4444', fontSize: 13 }}>
          {error}
        </div>
      )}

      {/* Summary Cards */}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(5, 1fr)', gap: 14, marginBottom: 28 }}>
        {bands.map(band => {
          const cfg = BAND_CONFIG[band];
          const count = summary ? (
            band === 'overdue' ? summary.overdue :
            band === 'today' ? summary.due_today :
            band === 'tomorrow' ? summary.due_tomorrow :
            band === 'on_track' ? summary.on_track :
            summary.no_sla
          ) : 0;

          const isActive = activeBand === band;
          return (
            <div
              key={band}
              onClick={() => handleBandClick(band)}
              style={{
                padding: '20px 18px',
                borderRadius: 12,
                border: `2px solid ${isActive ? cfg.colour : 'var(--border)'}`,
                background: isActive ? cfg.bg : 'var(--bg-secondary)',
                cursor: 'pointer',
                transition: 'all 0.15s',
                userSelect: 'none',
              }}
            >
              <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 10 }}>
                <span style={{ fontSize: 20 }}>{cfg.icon}</span>
                <span style={{ fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.05em' }}>
                  {cfg.label}
                </span>
              </div>
              <div style={{ fontSize: 36, fontWeight: 800, color: loading ? 'var(--text-muted)' : cfg.colour, lineHeight: 1 }}>
                {loading ? '—' : count}
              </div>
              <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 6 }}>{cfg.description}</div>
            </div>
          );
        })}
      </div>

      {/* Progress bar */}
      {summary && summary.total_pending > 0 && (
        <div style={{ marginBottom: 24 }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 6 }}>
            <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>Order pipeline — {summary.total_pending} orders pending despatch</span>
          </div>
          <div style={{ height: 8, borderRadius: 99, background: 'var(--bg-elevated)', overflow: 'hidden', display: 'flex' }}>
            {[
              { count: summary.overdue, colour: '#ef4444' },
              { count: summary.due_today, colour: '#f59e0b' },
              { count: summary.due_tomorrow, colour: '#3b82f6' },
              { count: summary.on_track, colour: '#22c55e' },
              { count: summary.no_sla, colour: '#94a3b8' },
            ].map((seg, i) => (
              <div
                key={i}
                style={{
                  height: '100%',
                  width: `${(seg.count / summary.total_pending) * 100}%`,
                  background: seg.colour,
                  transition: 'width 0.3s',
                }}
              />
            ))}
          </div>
        </div>
      )}

      {/* Order List for selected band */}
      {activeBand && (
        <div style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 10, padding: 20 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 16 }}>
            <span style={{ fontSize: 16 }}>{BAND_CONFIG[activeBand].icon}</span>
            <h3 style={{ margin: 0, fontSize: 16, fontWeight: 700, color: 'var(--text-primary)' }}>
              {BAND_CONFIG[activeBand].label} Orders
            </h3>
            <span style={{
              fontSize: 12, fontWeight: 600, borderRadius: 20, padding: '2px 10px',
              background: BAND_CONFIG[activeBand].bg, color: BAND_CONFIG[activeBand].colour,
            }}>
              {ordersLoading ? '…' : `${orders.length} orders`}
            </span>
            {orders.length > 0 && (
              <button
                onClick={() => navigate('/dispatch')}
                style={{ ...btnPrimary, marginLeft: 'auto', fontSize: 12, padding: '6px 14px' }}
              >
                🚀 Despatch Console →
              </button>
            )}
          </div>

          {ordersLoading ? (
            <div style={{ textAlign: 'center', padding: '30px 0', color: 'var(--text-muted)', fontSize: 14 }}>
              Loading orders…
            </div>
          ) : orders.length === 0 ? (
            <div style={{ textAlign: 'center', padding: '30px 0', color: 'var(--text-muted)', fontSize: 14 }}>
              No orders in this band.
            </div>
          ) : (
            <div style={{ overflowX: 'auto' }}>
              <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                <thead>
                  <tr>
                    {['Order Ref', 'Channel', 'Customer', 'Status', 'SLA Date', 'Total'].map(h => (
                      <th key={h} style={thStyle}>{h}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {orders.map(order => {
                    const slaStr = order.despatch_by_date || order.promised_ship_by;
                    const slaColour = BAND_CONFIG[getBand(order)].colour;
                    return (
                      <tr
                        key={order.order_id}
                        onClick={() => navigate(`/orders?highlight=${order.order_id}`)}
                        style={{ cursor: 'pointer', borderBottom: '1px solid var(--border)', transition: 'background 0.1s' }}
                        onMouseEnter={e => (e.currentTarget.style.background = 'var(--bg-elevated)')}
                        onMouseLeave={e => (e.currentTarget.style.background = '')}
                      >
                        <td style={{ ...tdStyle, fontFamily: 'monospace', fontSize: 12, fontWeight: 600, color: 'var(--text-primary)' }}>
                          {order.external_order_id || order.order_id.slice(0, 12)}
                        </td>
                        <td style={tdStyle}>
                          <span style={{ fontSize: 11, fontWeight: 600, background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 4, padding: '2px 7px', textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--text-secondary)' }}>
                            {order.channel}
                          </span>
                        </td>
                        <td style={{ ...tdStyle, color: 'var(--text-primary)' }}>{order.customer?.name || '—'}</td>
                        <td style={tdStyle}>
                          <span style={{ fontSize: 11, borderRadius: 4, padding: '2px 8px', fontWeight: 600, background: 'var(--bg-elevated)', border: '1px solid var(--border)', color: 'var(--text-secondary)' }}>
                            {order.sub_status || order.status}
                          </span>
                        </td>
                        <td style={{ ...tdStyle, color: slaColour, fontWeight: 600, fontSize: 12 }}>
                          {slaLabel(slaStr)}
                        </td>
                        <td style={tdStyle}>
                          {fmtMoney(order.totals?.grand_total?.amount, order.totals?.grand_total?.currency)}
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

      {/* Info hint when nothing selected */}
      {!activeBand && !loading && summary && (
        <div style={{ textAlign: 'center', padding: '32px 0', color: 'var(--text-muted)', fontSize: 14 }}>
          Click a band above to drill into the orders within it.
        </div>
      )}
    </div>
  );
}

// ─── Styles ───────────────────────────────────────────────────────────────────

const thStyle: React.CSSProperties = {
  padding: '10px 12px',
  textAlign: 'left',
  fontSize: 11,
  fontWeight: 600,
  textTransform: 'uppercase',
  letterSpacing: '0.05em',
  color: 'var(--text-muted)',
  borderBottom: '1px solid var(--border)',
  whiteSpace: 'nowrap',
};

const tdStyle: React.CSSProperties = {
  padding: '11px 12px',
  color: 'var(--text-secondary)',
  fontSize: 13,
};

const btnGhost: React.CSSProperties = {
  padding: '8px 16px',
  background: 'transparent',
  color: 'var(--text-secondary)',
  border: '1px solid var(--border)',
  borderRadius: 6,
  cursor: 'pointer',
};

const btnPrimary: React.CSSProperties = {
  padding: '8px 18px',
  background: 'var(--primary)',
  color: 'white',
  border: 'none',
  borderRadius: 6,
  cursor: 'pointer',
  fontWeight: 600,
};

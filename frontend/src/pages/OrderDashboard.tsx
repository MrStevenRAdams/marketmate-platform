import { useState, useEffect, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';

const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';
function api(path: string, init?: RequestInit) {
  const tenantId = localStorage.getItem('active_tenant_id') || '';
  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': tenantId, ...init?.headers },
  });
}

// ── Types ─────────────────────────────────────────────────────────────────────

interface DayVolume { date: string; count: number; }

interface OldestOrder {
  order_id: string;
  channel: string;
  status: string;
  total: number;
  currency: string;
  order_date: string;
  created_at: string;
  sla_at_risk: boolean;
}

interface OrderDashboardData {
  by_status: Record<string, number>;
  daily_volume: DayVolume[];
  oldest_open: OldestOrder[];
}

// ── Styles / consts ──────────────────────────────────────────────────────────

const STATUS_CONFIG: Record<string, { label: string; colour: string; bg: string; icon: string }> = {
  imported:        { label: 'Imported',        colour: '#60a5fa', bg: 'rgba(59,130,246,0.12)',  icon: '📥' },
  processing:      { label: 'Processing',      colour: '#fbbf24', bg: 'rgba(251,191,36,0.12)',  icon: '⚙️' },
  on_hold:         { label: 'On Hold',         colour: '#f87171', bg: 'rgba(239,68,68,0.12)',   icon: '⏸️' },
  ready_to_fulfil: { label: 'Ready to Fulfil', colour: '#a78bfa', bg: 'rgba(139,92,246,0.12)',  icon: '✅' },
  parked:          { label: 'Parked',          colour: '#94a3b8', bg: 'rgba(148,163,184,0.12)', icon: '🅿️' },
};

function Skeleton({ w = '100%', h = 16, radius = 4 }: { w?: string | number; h?: number; radius?: number }) {
  return (
    <div style={{
      width: w, height: h, borderRadius: radius,
      background: 'linear-gradient(90deg, var(--bg-elevated) 0%, var(--bg-secondary) 50%, var(--bg-elevated) 100%)',
      backgroundSize: '200% 100%', animation: 'shimmer 1.5s infinite',
    }} />
  );
}

function timeAgo(iso: string) {
  if (!iso) return '—';
  try {
    const diff = Date.now() - new Date(iso).getTime();
    const h = Math.floor(diff / 3600000);
    if (h < 1) return `${Math.floor(diff / 60000)}m ago`;
    if (h < 24) return `${h}h ago`;
    return `${Math.floor(h / 24)}d ago`;
  } catch { return iso; }
}

function fmtDate(s: string) {
  if (!s) return '—';
  try { return new Date(s).toLocaleDateString('en-GB', { day: 'numeric', month: 'short' }); }
  catch { return s; }
}

// ── CSS Bar Chart ─────────────────────────────────────────────────────────────

function DailyVolumeChart({ data }: { data: DayVolume[] }) {
  const max = Math.max(...data.map(d => d.count), 1);
  const last30 = data.slice(-30);

  return (
    <div style={{ display: 'flex', alignItems: 'flex-end', gap: 3, height: 100, padding: '0 4px' }}>
      {last30.map((d, i) => {
        const pct = (d.count / max) * 100;
        return (
          <div
            key={i}
            title={`${fmtDate(d.date)}: ${d.count} orders`}
            style={{ flex: 1, display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 4, cursor: 'default' }}
          >
            <div style={{
              width: '100%', borderRadius: '3px 3px 0 0',
              background: `rgba(99,102,241,${0.3 + (pct / 100) * 0.7})`,
              height: `${Math.max(pct, 2)}%`,
              transition: 'background 0.2s ease',
            }}
              onMouseEnter={e => (e.currentTarget.style.background = '#6366f1')}
              onMouseLeave={e => (e.currentTarget.style.background = `rgba(99,102,241,${0.3 + (pct / 100) * 0.7})`)}
            />
          </div>
        );
      })}
    </div>
  );
}

// ── Status donut ─────────────────────────────────────────────────────────────

function StatusBreakdown({ byStatus }: { byStatus: Record<string, number> }) {
  const total = Object.values(byStatus).reduce((a, b) => a + b, 0);
  const entries = Object.entries(byStatus).filter(([, v]) => v > 0);

  if (total === 0) {
    return (
      <div style={{ textAlign: 'center', padding: 40, color: 'var(--text-muted)' }}>
        <div style={{ fontSize: 36, marginBottom: 8 }}>🎉</div>
        No open orders — all caught up!
      </div>
    );
  }

  // CSS stacked bar
  let cursor = 0;
  const segments = entries.map(([status, count]) => {
    const pct = (count / total) * 100;
    const cfg = STATUS_CONFIG[status] || { colour: '#6b7280', bg: 'rgba(107,114,128,0.12)', label: status, icon: '📋' };
    const seg = { status, count, pct, colour: cfg.colour, start: cursor };
    cursor += pct;
    return seg;
  });

  return (
    <div>
      {/* Segmented bar */}
      <div style={{ height: 12, borderRadius: 6, overflow: 'hidden', display: 'flex', marginBottom: 20 }}>
        {segments.map(seg => (
          <div
            key={seg.status}
            title={`${STATUS_CONFIG[seg.status]?.label ?? seg.status}: ${seg.count}`}
            style={{ width: `${seg.pct}%`, background: seg.colour, transition: 'opacity 0.2s' }}
            onMouseEnter={e => (e.currentTarget.style.opacity = '0.7')}
            onMouseLeave={e => (e.currentTarget.style.opacity = '1')}
          />
        ))}
      </div>

      {/* Legend */}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
        {entries.map(([status, count]) => {
          const cfg = STATUS_CONFIG[status] || { colour: '#6b7280', bg: 'rgba(107,114,128,0.12)', label: status, icon: '📋' };
          const pct = total > 0 ? Math.round((count / total) * 100) : 0;
          return (
            <div key={status} style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
              <div style={{ width: 10, height: 10, borderRadius: 3, background: cfg.colour, flexShrink: 0 }} />
              <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                <span style={{ fontSize: 13, color: 'var(--text-secondary)' }}>{cfg.icon} {cfg.label}</span>
                <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                  <span style={{ fontSize: 13, fontWeight: 700, color: cfg.colour }}>{count.toLocaleString()}</span>
                  <span style={{ fontSize: 11, color: 'var(--text-muted)', minWidth: 32, textAlign: 'right' }}>{pct}%</span>
                </div>
              </div>
            </div>
          );
        })}
        <div style={{ borderTop: '1px solid var(--border)', paddingTop: 10, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <span style={{ fontSize: 13, color: 'var(--text-muted)', fontWeight: 600 }}>Total Open</span>
          <span style={{ fontSize: 16, fontWeight: 700, color: 'var(--text-primary)' }}>{total.toLocaleString()}</span>
        </div>
      </div>
    </div>
  );
}

// ── Main component ────────────────────────────────────────────────────────────

export default function OrderDashboard() {
  const navigate = useNavigate();
  const [data, setData] = useState<OrderDashboardData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await api('/analytics/order-dashboard');
      if (!res.ok) throw new Error('Failed to load');
      setData(await res.json());
    } catch {
      setError('Failed to load order dashboard.');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { load(); }, [load]);

  return (
    <div style={{ padding: '24px 28px', maxWidth: 1300, margin: '0 auto' }}>
      <style>{`@keyframes shimmer { 0% { background-position: -200% 0 } 100% { background-position: 200% 0 } }`}</style>

      {/* Header */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 28 }}>
        <div>
          <h1 style={{ fontSize: 22, fontWeight: 700, color: 'var(--text-primary)', margin: 0 }}>Order Dashboard</h1>
          <p style={{ fontSize: 13, color: 'var(--text-muted)', margin: '4px 0 0' }}>Open orders by status with 30-day volume trend</p>
        </div>
        <div style={{ display: 'flex', gap: 10 }}>
          <button onClick={() => navigate('/orders')} style={{ padding: '8px 16px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-secondary)', fontSize: 13, cursor: 'pointer' }}>
            📋 View All Orders
          </button>
          <button onClick={load} style={{ padding: '8px 16px', background: 'var(--primary)', border: 'none', borderRadius: 8, color: '#fff', fontSize: 13, cursor: 'pointer', fontWeight: 600 }}>
            ↻ Refresh
          </button>
        </div>
      </div>

      {error && (
        <div style={{ padding: 16, background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 8, color: '#f87171', marginBottom: 20, fontSize: 13 }}>
          {error}
        </div>
      )}

      {/* Top panels: status breakdown + daily volume */}
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1.6fr', gap: 20, marginBottom: 24 }}>

        {/* Status breakdown panel */}
        <div style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 12, padding: 24 }}>
          <div style={{ fontSize: 14, fontWeight: 700, color: 'var(--text-primary)', marginBottom: 20 }}>Open Orders by Status</div>
          {loading ? (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
              {[1,2,3,4].map(i => <Skeleton key={i} h={36} radius={6} />)}
            </div>
          ) : (
            <StatusBreakdown byStatus={data?.by_status ?? {}} />
          )}
        </div>

        {/* Daily volume chart */}
        <div style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 12, padding: 24 }}>
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 20 }}>
            <div style={{ fontSize: 14, fontWeight: 700, color: 'var(--text-primary)' }}>Daily Order Volume</div>
            <span style={{ fontSize: 11, color: 'var(--text-muted)', background: 'var(--bg-elevated)', padding: '4px 10px', borderRadius: 6 }}>Last 30 days</span>
          </div>
          {loading ? (
            <div style={{ height: 100, display: 'flex', alignItems: 'flex-end', gap: 3 }}>
              {Array.from({ length: 30 }).map((_, i) => (
                <div key={i} style={{ flex: 1, background: 'var(--bg-elevated)', borderRadius: '3px 3px 0 0', height: `${20 + Math.random() * 60}%` }} />
              ))}
            </div>
          ) : (data?.daily_volume ?? []).length === 0 ? (
            <div style={{ height: 100, display: 'flex', alignItems: 'center', justifyContent: 'center', color: 'var(--text-muted)', fontSize: 13 }}>
              No order data for the last 30 days.
            </div>
          ) : (
            <>
              <DailyVolumeChart data={data!.daily_volume} />
              <div style={{ display: 'flex', justifyContent: 'space-between', marginTop: 8, fontSize: 11, color: 'var(--text-muted)' }}>
                <span>{fmtDate(data!.daily_volume[0]?.date)}</span>
                <span>{fmtDate(data!.daily_volume[data!.daily_volume.length - 1]?.date)}</span>
              </div>
            </>
          )}
        </div>
      </div>

      {/* Oldest open orders table */}
      <div style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 12, overflow: 'hidden' }}>
        <div style={{ padding: '16px 20px', borderBottom: '1px solid var(--border)', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <div>
            <div style={{ fontWeight: 700, color: 'var(--text-primary)', fontSize: 14 }}>Oldest Open Orders</div>
            <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>Orders awaiting action — oldest first</div>
          </div>
          <button
            onClick={() => navigate('/orders')}
            style={{ padding: '7px 14px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-secondary)', fontSize: 12, cursor: 'pointer' }}
          >
            View all →
          </button>
        </div>

        {loading ? (
          <div style={{ padding: 24 }}>
            {[1,2,3,4,5].map(i => <div key={i} style={{ display: 'flex', gap: 16, padding: '13px 0', borderBottom: '1px solid var(--border)' }}><Skeleton w={120} h={14} /><Skeleton w={80} h={14} /><Skeleton w={80} h={14} /><Skeleton w={60} h={14} /></div>)}
          </div>
        ) : (data?.oldest_open ?? []).length === 0 ? (
          <div style={{ textAlign: 'center', padding: 56, color: 'var(--text-muted)' }}>
            <div style={{ fontSize: 40, marginBottom: 12 }}>🎉</div>
            No open orders right now.
          </div>
        ) : (
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
            <thead>
              <tr style={{ borderBottom: '1px solid var(--border)' }}>
                {['Order ID', 'Channel', 'Status', 'Total', 'Order Date', 'Waiting', 'SLA'].map(h => (
                  <th key={h} style={{ padding: '11px 16px', textAlign: h === 'Order ID' || h === 'Channel' || h === 'Status' ? 'left' : 'right', color: 'var(--text-muted)', fontWeight: 600, fontSize: 11, textTransform: 'uppercase', letterSpacing: '0.05em', background: 'var(--bg-elevated)' }}>{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {(data?.oldest_open ?? []).map((order, i) => {
                const cfg = STATUS_CONFIG[order.status] || { colour: '#6b7280', bg: 'rgba(107,114,128,0.12)', label: order.status, icon: '📋' };
                return (
                  <tr
                    key={order.order_id}
                    onClick={() => navigate('/orders')}
                    style={{ borderBottom: '1px solid var(--border)', cursor: 'pointer', background: order.sla_at_risk ? 'rgba(239,68,68,0.04)' : i % 2 === 0 ? 'transparent' : 'rgba(255,255,255,0.01)' }}
                    onMouseEnter={e => (e.currentTarget.style.background = 'rgba(99,102,241,0.06)')}
                    onMouseLeave={e => (e.currentTarget.style.background = order.sla_at_risk ? 'rgba(239,68,68,0.04)' : i % 2 === 0 ? 'transparent' : 'rgba(255,255,255,0.01)')}
                  >
                    <td style={{ padding: '12px 16px', color: 'var(--primary)', fontWeight: 600, fontFamily: 'monospace', fontSize: 12 }}>
                      {order.order_id}
                    </td>
                    <td style={{ padding: '12px 16px' }}>
                      <span style={{ display: 'inline-block', padding: '3px 8px', borderRadius: 4, fontSize: 11, fontWeight: 600, background: 'var(--bg-elevated)', color: 'var(--text-secondary)', textTransform: 'capitalize' }}>
                        {order.channel || 'unknown'}
                      </span>
                    </td>
                    <td style={{ padding: '12px 16px' }}>
                      <span style={{ display: 'inline-block', padding: '3px 8px', borderRadius: 4, fontSize: 11, fontWeight: 600, background: cfg.bg, color: cfg.colour }}>
                        {cfg.icon} {cfg.label}
                      </span>
                    </td>
                    <td style={{ padding: '12px 16px', textAlign: 'right', fontWeight: 600, color: 'var(--text-primary)' }}>
                      {(order.total ?? 0).toLocaleString('en-GB', { style: 'currency', currency: order.currency || 'GBP' })}
                    </td>
                    <td style={{ padding: '12px 16px', textAlign: 'right', color: 'var(--text-muted)', fontSize: 12 }}>{fmtDate(order.order_date || order.created_at)}</td>
                    <td style={{ padding: '12px 16px', textAlign: 'right', color: 'var(--text-secondary)', fontSize: 12 }}>{timeAgo(order.created_at)}</td>
                    <td style={{ padding: '12px 16px', textAlign: 'right' }}>
                      {order.sla_at_risk ? (
                        <span style={{ display: 'inline-block', padding: '3px 8px', borderRadius: 4, fontSize: 11, fontWeight: 700, background: 'rgba(239,68,68,0.15)', color: '#f87171' }}>⚠️ At Risk</span>
                      ) : (
                        <span style={{ color: 'var(--text-muted)', fontSize: 11 }}>—</span>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}

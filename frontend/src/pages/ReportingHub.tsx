import { useState, useEffect, useCallback } from 'react';
import { getActiveTenantId } from '../contexts/TenantContext';

// ─── Constants ───────────────────────────────────────────────────────────────

const API_BASE = import.meta.env.VITE_API_URL ?? 'http://localhost:8080/api/v1';

function api(path: string) {
  return fetch(`${API_BASE}${path}`, {
    headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId() },
  });
}

type Period = 'today' | '7d' | '30d' | '90d';
type Tab = 'orders-channel' | 'orders-date' | 'orders-product' | 'despatch' | 'returns' | 'financial';

// ─── Types ───────────────────────────────────────────────────────────────────

interface ByChannelItem {
  channel: string; order_count: number; revenue: number; avg_order_value: number; currency: string;
}
interface ByChannelResp { date_from: string; date_to: string; currency: string; channels: ByChannelItem[]; }

interface ByDatePoint { period: string; orders: number; revenue: number; }
interface ByDateResp { granularity: string; currency: string; points: ByDatePoint[]; }

interface ProductItem { sku: string; title: string; units: number; revenue: number; order_count: number; }
interface ByProductResp { currency: string; top_sellers: ProductItem[]; slow_movers: ProductItem[]; }

interface DespatchResp {
  date_from: string; date_to: string;
  total_dispatched: number; on_time: number; on_time_percent: number;
  overdue: number; overdue_percent: number;
  due_today: number; due_tomorrow: number; on_track: number; no_sla: number;
}

interface ReturnsItem { key: string; count: number; }
interface HeatmapCell { channel: string; reason_code: string; count: number; }
interface ReturnsResp {
  date_from: string; date_to: string; total_rmas: number;
  total_refund_value: number; currency: string; avg_resolution_days: number;
  by_channel: ReturnsItem[]; by_product: ReturnsItem[]; by_reason_code: ReturnsItem[];
  reason_code_heatmap: HeatmapCell[];
}

interface VATBand { rate_label: string; tax_rate: number; order_count: number; net_revenue: number; output_vat: number; currency: string; }
interface FinancialResp {
  date_from: string; date_to: string; currency: string;
  total_revenue: number; total_cogs: number; gross_margin: number; gross_margin_pct: number;
  total_output_vat: number; total_shipping_cost: number; total_shipping_charged: number;
  vat_bands: VATBand[];
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

function fmtCurrency(amount: number, currency = 'GBP') {
  return new Intl.NumberFormat('en-GB', { style: 'currency', currency, maximumFractionDigits: 2 }).format(amount);
}
function fmtNum(n: number) { return new Intl.NumberFormat('en-GB').format(n); }
function fmtPct(n: number) { return `${n.toFixed(1)}%`; }

const CHANNEL_COLORS: Record<string, string> = {
  amazon: '#FF9900', ebay: '#E53238', shopify: '#96BF48', temu: '#EA6A35',
  tiktok: '#555', etsy: '#F1641E', woocommerce: '#7F54B3', backmarket: '#14B8A6',
  zalando: '#FF6600', bol: '#0E4299', lazada: '#F57224', unknown: '#999',
};
function chColor(ch: string) { return CHANNEL_COLORS[ch] ?? 'var(--primary)'; }
function chLabel(ch: string) { return ch.charAt(0).toUpperCase() + ch.slice(1); }

// ─── Sub-components ───────────────────────────────────────────────────────────

function PeriodSelector({ value, onChange }: { value: Period; onChange: (p: Period) => void }) {
  return (
    <div style={{ display: 'flex', gap: 6 }}>
      {(['today', '7d', '30d', '90d'] as Period[]).map(p => (
        <button
          key={p}
          onClick={() => onChange(p)}
          className={value === p ? 'btn-pri btn-sm' : 'btn-sec btn-sm'}
          style={{ minWidth: 48 }}
        >
          {p === 'today' ? 'Today' : p}
        </button>
      ))}
    </div>
  );
}

function KpiCard({ label, value, sub, colour }: { label: string; value: string; sub?: string; colour?: string }) {
  return (
    <div style={{ background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 10, padding: '16px 20px', flex: '1 1 160px' }}>
      {colour && <div style={{ width: 4, height: 32, background: colour, borderRadius: 2, marginBottom: 10 }} />}
      <div style={{ fontSize: 22, fontWeight: 700, color: colour ?? 'var(--text)' }}>{value}</div>
      <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 4 }}>{label}</div>
      {sub && <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 2 }}>{sub}</div>}
    </div>
  );
}

function SimpleBar({ items, valueKey, labelKey, colorFn, currency }: {
  items: Record<string, unknown>[];
  valueKey: string; labelKey: string;
  colorFn?: (item: Record<string, unknown>) => string;
  currency?: string;
}) {
  const max = Math.max(...items.map(i => Number(i[valueKey]) || 0), 1);
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
      {items.slice(0, 12).map((item, idx) => {
        const val = Number(item[valueKey]) || 0;
        const pct = (val / max) * 100;
        const label = String(item[labelKey] ?? '');
        const color = colorFn ? colorFn(item) : 'var(--primary)';
        return (
          <div key={idx} style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            <div style={{ width: 110, fontSize: 12, color: 'var(--text-muted)', textAlign: 'right', flexShrink: 0 }}>{chLabel(label)}</div>
            <div style={{ flex: 1, background: 'var(--surface-2)', borderRadius: 4, height: 22, position: 'relative' }}>
              <div style={{ width: `${pct}%`, height: '100%', background: color, borderRadius: 4, minWidth: 4, opacity: 0.85 }} />
            </div>
            <div style={{ width: 90, fontSize: 12, fontWeight: 600, textAlign: 'right', flexShrink: 0 }}>
              {currency ? fmtCurrency(val, currency) : fmtNum(val)}
            </div>
          </div>
        );
      })}
    </div>
  );
}

// ─── Main Component ───────────────────────────────────────────────────────────

export default function ReportingHub() {
  const [tab, setTab] = useState<Tab>('orders-channel');
  const [period, setPeriod] = useState<Period>('30d');
  const [granularity, setGranularity] = useState<'daily' | 'weekly' | 'monthly'>('daily');
  const [loading, setLoading] = useState(false);

  // Data states
  const [byChannel, setByChannel] = useState<ByChannelResp | null>(null);
  const [byDate, setByDate] = useState<ByDateResp | null>(null);
  const [byProduct, setByProduct] = useState<ByProductResp | null>(null);
  const [despatch, setDespatch] = useState<DespatchResp | null>(null);
  const [returns, setReturns] = useState<ReturnsResp | null>(null);
  const [financial, setFinancial] = useState<FinancialResp | null>(null);

  const fetchData = useCallback(async () => {
    setLoading(true);
    try {
      const q = `?period=${period}`;
      if (tab === 'orders-channel') {
        const r = await api(`/analytics/reports/orders-by-channel${q}`);
        setByChannel(await r.json());
      } else if (tab === 'orders-date') {
        const r = await api(`/analytics/reports/orders-by-date${q}&granularity=${granularity}`);
        setByDate(await r.json());
      } else if (tab === 'orders-product') {
        const r = await api(`/analytics/reports/orders-by-product${q}`);
        setByProduct(await r.json());
      } else if (tab === 'despatch') {
        const r = await api(`/analytics/reports/despatch-performance${q}`);
        setDespatch(await r.json());
      } else if (tab === 'returns') {
        const r = await api(`/analytics/reports/returns${q}`);
        setReturns(await r.json());
      } else if (tab === 'financial') {
        const r = await api(`/analytics/reports/financial${q}`);
        setFinancial(await r.json());
      }
    } catch (e) {
      console.error(e);
    } finally {
      setLoading(false);
    }
  }, [tab, period, granularity]);

  useEffect(() => { fetchData(); }, [fetchData]);

  const exportCSV = () => {
    const reportMap: Record<Tab, string> = {
      'orders-channel': 'orders-by-channel', 'orders-date': 'orders-by-date',
      'orders-product': 'orders-by-product', 'despatch': 'despatch-performance',
      'returns': 'returns', 'financial': 'financial',
    };
    const tenantId = getActiveTenantId();
    window.location.href = `${API_BASE}/analytics/reports/export?report=${reportMap[tab]}&period=${period}&X-Tenant-Id=${tenantId}`;
  };

  const tabs: { id: Tab; label: string }[] = [
    { id: 'orders-channel', label: 'By Channel' },
    { id: 'orders-date', label: 'By Date' },
    { id: 'orders-product', label: 'By Product' },
    { id: 'despatch', label: 'Despatch Performance' },
    { id: 'returns', label: 'Returns & RMA' },
    { id: 'financial', label: 'Financial' },
  ];

  return (
    <div style={{ padding: '24px 28px', maxWidth: 1200, margin: '0 auto' }}>
      {/* Header */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 24 }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 24, fontWeight: 700 }}>Reports</h1>
          <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 14 }}>
            Order, financial & returns analytics
          </p>
        </div>
        <div style={{ display: 'flex', gap: 10, alignItems: 'center' }}>
          <PeriodSelector value={period} onChange={p => { setPeriod(p); }} />
          <button className="btn-sec btn-sm" onClick={exportCSV} title="Export CSV">
            ↓ Export
          </button>
        </div>
      </div>

      {/* Tab bar */}
      <div style={{ display: 'flex', gap: 4, marginBottom: 24, borderBottom: '1px solid var(--border)', paddingBottom: 0 }}>
        {tabs.map(t => (
          <button
            key={t.id}
            onClick={() => setTab(t.id)}
            style={{
              background: 'none', border: 'none', cursor: 'pointer', padding: '8px 16px',
              fontSize: 14, fontWeight: tab === t.id ? 600 : 400,
              color: tab === t.id ? 'var(--primary)' : 'var(--text-muted)',
              borderBottom: tab === t.id ? '2px solid var(--primary)' : '2px solid transparent',
              marginBottom: -1, transition: 'all .15s',
            }}
          >
            {t.label}
          </button>
        ))}
      </div>

      {loading && (
        <div style={{ textAlign: 'center', padding: 60, color: 'var(--text-muted)' }}>Loading…</div>
      )}

      {/* ── By Channel ── */}
      {!loading && tab === 'orders-channel' && byChannel && (
        <div>
          <div style={{ display: 'flex', gap: 14, flexWrap: 'wrap', marginBottom: 28 }}>
            <KpiCard label="Total Revenue" value={fmtCurrency(byChannel.channels.reduce((s, c) => s + c.revenue, 0), byChannel.currency)} colour="var(--primary)" />
            <KpiCard label="Total Orders" value={fmtNum(byChannel.channels.reduce((s, c) => s + c.order_count, 0))} />
            <KpiCard label="Active Channels" value={String(byChannel.channels.length)} />
            <KpiCard
              label="Avg Order Value"
              value={fmtCurrency(
                byChannel.channels.reduce((s, c) => s + c.avg_order_value, 0) / Math.max(byChannel.channels.length, 1),
                byChannel.currency
              )}
            />
          </div>

          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 20 }}>
            <div style={{ background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 10, padding: 20 }}>
              <h3 style={{ margin: '0 0 18px', fontSize: 15 }}>Revenue by Channel</h3>
              <SimpleBar
                items={byChannel.channels as unknown as Record<string, unknown>[]}
                valueKey="revenue" labelKey="channel"
                colorFn={item => chColor(String(item.channel))}
                currency={byChannel.currency}
              />
            </div>
            <div style={{ background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 10, padding: 20 }}>
              <h3 style={{ margin: '0 0 18px', fontSize: 15 }}>Orders by Channel</h3>
              <SimpleBar
                items={byChannel.channels as unknown as Record<string, unknown>[]}
                valueKey="order_count" labelKey="channel"
                colorFn={item => chColor(String(item.channel))}
              />
            </div>
          </div>

          <div style={{ background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 10, padding: 20, marginTop: 20 }}>
            <h3 style={{ margin: '0 0 16px', fontSize: 15 }}>Channel Detail</h3>
            <div className="table-outer">
              <div className="table-scroll-x">
                <table className="orders-table">
                  <thead><tr>
                    <th>Channel</th><th>Orders</th><th>Revenue</th><th>Avg Order Value</th>
                  </tr></thead>
                  <tbody>
                    {byChannel.channels.map(ch => (
                      <tr key={ch.channel}>
                        <td>
                          <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8 }}>
                            <span style={{ width: 10, height: 10, borderRadius: '50%', background: chColor(ch.channel), display: 'inline-block' }} />
                            {chLabel(ch.channel)}
                          </span>
                        </td>
                        <td>{fmtNum(ch.order_count)}</td>
                        <td>{fmtCurrency(ch.revenue, ch.currency)}</td>
                        <td>{fmtCurrency(ch.avg_order_value, ch.currency)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* ── By Date ── */}
      {!loading && tab === 'orders-date' && byDate && (
        <div>
          <div style={{ display: 'flex', gap: 10, marginBottom: 20, alignItems: 'center' }}>
            <span style={{ fontSize: 13, color: 'var(--text-muted)' }}>Granularity:</span>
            {(['daily', 'weekly', 'monthly'] as const).map(g => (
              <button key={g} className={granularity === g ? 'btn-pri btn-sm' : 'btn-sec btn-sm'}
                onClick={() => setGranularity(g)}>
                {g.charAt(0).toUpperCase() + g.slice(1)}
              </button>
            ))}
          </div>

          <div style={{ display: 'flex', gap: 14, flexWrap: 'wrap', marginBottom: 28 }}>
            <KpiCard label="Total Orders" value={fmtNum(byDate.points.reduce((s, p) => s + p.orders, 0))} colour="var(--primary)" />
            <KpiCard label="Total Revenue" value={fmtCurrency(byDate.points.reduce((s, p) => s + p.revenue, 0), byDate.currency)} />
            <KpiCard label="Data Points" value={String(byDate.points.length)} sub={byDate.granularity} />
          </div>

          <div style={{ background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 10, padding: 20, marginBottom: 20 }}>
            <h3 style={{ margin: '0 0 16px', fontSize: 15 }}>Order Volume Over Time</h3>
            <div style={{ overflowX: 'auto' }}>
              <div style={{ display: 'flex', alignItems: 'flex-end', gap: 4, minWidth: byDate.points.length * 30, height: 160, paddingBottom: 24 }}>
                {byDate.points.map((pt, i) => {
                  const maxOrders = Math.max(...byDate.points.map(p => p.orders), 1);
                  const h = Math.max((pt.orders / maxOrders) * 130, 2);
                  return (
                    <div key={i} style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', flex: 1, minWidth: 24 }}>
                      <div title={`${pt.period}: ${pt.orders} orders`} style={{ width: '80%', height: h, background: 'var(--primary)', borderRadius: '3px 3px 0 0', opacity: 0.8, cursor: 'pointer' }} />
                      {byDate.points.length <= 14 && (
                        <div style={{ fontSize: 9, color: 'var(--text-muted)', marginTop: 4, transform: 'rotate(-45deg)', whiteSpace: 'nowrap' }}>
                          {pt.period.slice(-5)}
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            </div>
          </div>

          <div style={{ background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 10, padding: 20 }}>
            <h3 style={{ margin: '0 0 16px', fontSize: 15 }}>Revenue Over Time</h3>
            <div style={{ display: 'flex', alignItems: 'flex-end', gap: 4, overflowX: 'auto', height: 160 }}>
              {byDate.points.map((pt, i) => {
                const maxRev = Math.max(...byDate.points.map(p => p.revenue), 1);
                const h = Math.max((pt.revenue / maxRev) * 130, 2);
                return (
                  <div key={i} title={`${pt.period}: ${fmtCurrency(pt.revenue, byDate.currency)}`}
                    style={{ flex: 1, minWidth: 24, height: h, background: '#10b981', borderRadius: '3px 3px 0 0', opacity: 0.8, alignSelf: 'flex-end', cursor: 'pointer' }} />
                );
              })}
            </div>
          </div>

          <div style={{ background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 10, padding: 20, marginTop: 20 }}>
            <h3 style={{ margin: '0 0 16px', fontSize: 15 }}>Data Table</h3>
            <div className="table-outer"><div className="table-scroll-x">
              <table className="orders-table">
                <thead><tr><th>Period</th><th>Orders</th><th>Revenue</th></tr></thead>
                <tbody>
                  {[...byDate.points].reverse().slice(0, 30).map((pt, i) => (
                    <tr key={i}>
                      <td style={{ fontFamily: 'monospace' }}>{pt.period}</td>
                      <td>{fmtNum(pt.orders)}</td>
                      <td>{fmtCurrency(pt.revenue, byDate.currency)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div></div>
          </div>
        </div>
      )}

      {/* ── By Product ── */}
      {!loading && tab === 'orders-product' && byProduct && (
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 20 }}>
          {/* Top Sellers */}
          <div style={{ background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 10, padding: 20 }}>
            <h3 style={{ margin: '0 0 16px', fontSize: 15, color: '#10b981' }}>🏆 Top Sellers</h3>
            <table className="orders-table" style={{ fontSize: 13 }}>
              <thead><tr><th>SKU</th><th>Title</th><th>Units</th><th>Revenue</th></tr></thead>
              <tbody>
                {byProduct.top_sellers.map((p, i) => (
                  <tr key={i}>
                    <td style={{ fontFamily: 'monospace', fontSize: 12 }}>{p.sku}</td>
                    <td style={{ maxWidth: 160, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{p.title || p.sku}</td>
                    <td style={{ fontWeight: 600 }}>{fmtNum(p.units)}</td>
                    <td>{fmtCurrency(p.revenue, byProduct.currency)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          {/* Slow Movers */}
          <div style={{ background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 10, padding: 20 }}>
            <h3 style={{ margin: '0 0 16px', fontSize: 15, color: '#f59e0b' }}>🐢 Slow Movers</h3>
            <table className="orders-table" style={{ fontSize: 13 }}>
              <thead><tr><th>SKU</th><th>Title</th><th>Units</th><th>Revenue</th></tr></thead>
              <tbody>
                {byProduct.slow_movers.map((p, i) => (
                  <tr key={i}>
                    <td style={{ fontFamily: 'monospace', fontSize: 12 }}>{p.sku}</td>
                    <td style={{ maxWidth: 160, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{p.title || p.sku}</td>
                    <td>{fmtNum(p.units)}</td>
                    <td>{fmtCurrency(p.revenue, byProduct.currency)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* ── Despatch Performance ── */}
      {!loading && tab === 'despatch' && despatch && (
        <div>
          <div style={{ display: 'flex', gap: 14, flexWrap: 'wrap', marginBottom: 28 }}>
            <KpiCard label="Total Dispatched" value={fmtNum(despatch.total_dispatched)} colour="var(--primary)" />
            <KpiCard label="On Time" value={fmtNum(despatch.on_time)} sub={fmtPct(despatch.on_time_percent)} colour="#10b981" />
            <KpiCard label="Overdue" value={fmtNum(despatch.overdue)} sub={fmtPct(despatch.overdue_percent)} colour="#ef4444" />
            <KpiCard label="Due Today" value={fmtNum(despatch.due_today)} colour="#f59e0b" />
            <KpiCard label="Due Tomorrow" value={fmtNum(despatch.due_tomorrow)} colour="#8b5cf6" />
            <KpiCard label="On Track" value={fmtNum(despatch.on_track)} colour="#3b82f6" />
            <KpiCard label="No SLA" value={fmtNum(despatch.no_sla)} colour="#6b7280" />
          </div>

          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 20 }}>
            <div style={{ background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 10, padding: 20 }}>
              <h3 style={{ margin: '0 0 18px', fontSize: 15 }}>SLA Performance (Dispatched)</h3>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
                {[
                  { label: 'On Time', value: despatch.on_time_percent, color: '#10b981' },
                  { label: 'Overdue', value: despatch.overdue_percent, color: '#ef4444' },
                ].map(b => (
                  <div key={b.label}>
                    <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 13, marginBottom: 5 }}>
                      <span>{b.label}</span><span style={{ fontWeight: 600 }}>{fmtPct(b.value)}</span>
                    </div>
                    <div style={{ height: 10, background: 'var(--surface-2)', borderRadius: 5 }}>
                      <div style={{ width: `${Math.min(b.value, 100)}%`, height: '100%', background: b.color, borderRadius: 5 }} />
                    </div>
                  </div>
                ))}
              </div>
            </div>

            <div style={{ background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 10, padding: 20 }}>
              <h3 style={{ margin: '0 0 18px', fontSize: 15 }}>Open Order SLA Bands</h3>
              {[
                { label: 'Overdue', count: despatch.overdue, color: '#ef4444' },
                { label: 'Due Today', count: despatch.due_today, color: '#f59e0b' },
                { label: 'Due Tomorrow', count: despatch.due_tomorrow, color: '#8b5cf6' },
                { label: 'On Track', count: despatch.on_track, color: '#10b981' },
                { label: 'No SLA', count: despatch.no_sla, color: '#9ca3af' },
              ].map(b => {
                const total = despatch.overdue + despatch.due_today + despatch.due_tomorrow + despatch.on_track + despatch.no_sla || 1;
                const pct = (b.count / total) * 100;
                return (
                  <div key={b.label} style={{ marginBottom: 10 }}>
                    <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 13, marginBottom: 4 }}>
                      <span style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                        <span style={{ width: 10, height: 10, borderRadius: 2, background: b.color, display: 'inline-block' }} />
                        {b.label}
                      </span>
                      <span>{fmtNum(b.count)}</span>
                    </div>
                    <div style={{ height: 8, background: 'var(--surface-2)', borderRadius: 4 }}>
                      <div style={{ width: `${pct}%`, height: '100%', background: b.color, borderRadius: 4, opacity: 0.85 }} />
                    </div>
                  </div>
                );
              })}
            </div>
          </div>
        </div>
      )}

      {/* ── Returns & RMA ── */}
      {!loading && tab === 'returns' && returns && (
        <div>
          <div style={{ display: 'flex', gap: 14, flexWrap: 'wrap', marginBottom: 28 }}>
            <KpiCard label="Total RMAs" value={fmtNum(returns.total_rmas)} colour="var(--primary)" />
            <KpiCard label="Total Refund Value" value={fmtCurrency(returns.total_refund_value, returns.currency)} colour="#ef4444" />
            <KpiCard label="Avg Resolution Time" value={`${returns.avg_resolution_days.toFixed(1)} days`} colour="#f59e0b" />
          </div>

          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 20, marginBottom: 20 }}>
            <div style={{ background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 10, padding: 20 }}>
              <h3 style={{ margin: '0 0 16px', fontSize: 15 }}>Returns by Channel</h3>
              <SimpleBar
                items={returns.by_channel as unknown as Record<string, unknown>[]}
                valueKey="count" labelKey="key"
                colorFn={item => chColor(String(item.key))}
              />
            </div>
            <div style={{ background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 10, padding: 20 }}>
              <h3 style={{ margin: '0 0 16px', fontSize: 15 }}>Returns by Reason Code</h3>
              <SimpleBar
                items={returns.by_reason_code as unknown as Record<string, unknown>[]}
                valueKey="count" labelKey="key"
              />
            </div>
          </div>

          <div style={{ background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 10, padding: 20 }}>
            <h3 style={{ margin: '0 0 16px', fontSize: 15 }}>Reason Code Heatmap</h3>
            {returns.reason_code_heatmap.length === 0 ? (
              <p style={{ color: 'var(--text-muted)', fontSize: 13 }}>No return data for this period.</p>
            ) : (
              <div className="table-outer"><div className="table-scroll-x">
                <table className="orders-table" style={{ fontSize: 13 }}>
                  <thead><tr><th>Channel</th><th>Reason Code</th><th>Count</th></tr></thead>
                  <tbody>
                    {returns.reason_code_heatmap.slice(0, 20).map((cell, i) => (
                      <tr key={i}>
                        <td>
                          <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
                            <span style={{ width: 10, height: 10, borderRadius: '50%', background: chColor(cell.channel), display: 'inline-block' }} />
                            {chLabel(cell.channel)}
                          </span>
                        </td>
                        <td>{cell.reason_code}</td>
                        <td>
                          <span style={{
                            background: `rgba(239,68,68,${Math.min(cell.count / 10, 0.8)})`,
                            padding: '2px 8px', borderRadius: 4, fontWeight: 600, fontSize: 12,
                          }}>{cell.count}</span>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div></div>
            )}
          </div>
        </div>
      )}

      {/* ── Financial ── */}
      {!loading && tab === 'financial' && financial && (
        <div>
          <div style={{ display: 'flex', gap: 14, flexWrap: 'wrap', marginBottom: 28 }}>
            <KpiCard label="Total Revenue" value={fmtCurrency(financial.total_revenue, financial.currency)} colour="var(--primary)" />
            <KpiCard label="Total COGS" value={fmtCurrency(financial.total_cogs, financial.currency)} colour="#ef4444" />
            <KpiCard label="Gross Margin" value={fmtCurrency(financial.gross_margin, financial.currency)} sub={fmtPct(financial.gross_margin_pct)} colour="#10b981" />
            <KpiCard label="Output VAT" value={fmtCurrency(financial.total_output_vat, financial.currency)} colour="#f59e0b" />
            <KpiCard label="Carrier Cost" value={fmtCurrency(financial.total_shipping_cost, financial.currency)} sub="Paid to carriers" colour="#8b5cf6" />
            <KpiCard label="Shipping Charged" value={fmtCurrency(financial.total_shipping_charged, financial.currency)} sub="Charged to customers" colour="#3b82f6" />
          </div>

          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 20 }}>
            <div style={{ background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 10, padding: 20 }}>
              <h3 style={{ margin: '0 0 16px', fontSize: 15 }}>Margin Breakdown</h3>
              {[
                { label: 'Revenue', value: financial.total_revenue, color: 'var(--primary)' },
                { label: 'COGS', value: financial.total_cogs, color: '#ef4444' },
                { label: 'Gross Margin', value: financial.gross_margin, color: '#10b981' },
              ].map(row => {
                const pct = financial.total_revenue > 0 ? (row.value / financial.total_revenue) * 100 : 0;
                return (
                  <div key={row.label} style={{ marginBottom: 14 }}>
                    <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 13, marginBottom: 5 }}>
                      <span>{row.label}</span>
                      <span style={{ fontWeight: 600 }}>{fmtCurrency(row.value, financial.currency)}</span>
                    </div>
                    <div style={{ height: 10, background: 'var(--surface-2)', borderRadius: 5 }}>
                      <div style={{ width: `${Math.min(Math.abs(pct), 100)}%`, height: '100%', background: row.color, borderRadius: 5, opacity: 0.85 }} />
                    </div>
                  </div>
                );
              })}

              <div style={{ marginTop: 16, paddingTop: 14, borderTop: '1px solid var(--border)' }}>
                <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                  <span style={{ fontSize: 13, color: 'var(--text-muted)' }}>Shipping surplus/deficit</span>
                  <span style={{
                    fontSize: 13, fontWeight: 600,
                    color: financial.total_shipping_charged >= financial.total_shipping_cost ? '#10b981' : '#ef4444',
                  }}>
                    {fmtCurrency(financial.total_shipping_charged - financial.total_shipping_cost, financial.currency)}
                  </span>
                </div>
              </div>
            </div>

            <div style={{ background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 10, padding: 20 }}>
              <h3 style={{ margin: '0 0 16px', fontSize: 15 }}>VAT Return Helper</h3>
              {financial.vat_bands.length === 0 ? (
                <p style={{ color: 'var(--text-muted)', fontSize: 13 }}>No VAT data for this period.</p>
              ) : (
                <table className="orders-table" style={{ fontSize: 13 }}>
                  <thead><tr><th>Rate Band</th><th>Orders</th><th>Net Revenue</th><th>Output VAT</th></tr></thead>
                  <tbody>
                    {financial.vat_bands.map((b, i) => (
                      <tr key={i}>
                        <td>
                          <span className={`status-badge sb-${b.tax_rate >= 0.2 ? 'fulfilled' : b.tax_rate > 0 ? 'default' : 'cancelled'}`}>
                            {b.rate_label}
                          </span>
                        </td>
                        <td>{fmtNum(b.order_count)}</td>
                        <td>{fmtCurrency(b.net_revenue, b.currency)}</td>
                        <td style={{ fontWeight: 600 }}>{fmtCurrency(b.output_vat, b.currency)}</td>
                      </tr>
                    ))}
                    <tr style={{ fontWeight: 700, borderTop: '2px solid var(--border)' }}>
                      <td>Total</td>
                      <td>{fmtNum(financial.vat_bands.reduce((s, b) => s + b.order_count, 0))}</td>
                      <td>{fmtCurrency(financial.vat_bands.reduce((s, b) => s + b.net_revenue, 0), financial.currency)}</td>
                      <td>{fmtCurrency(financial.total_output_vat, financial.currency)}</td>
                    </tr>
                  </tbody>
                </table>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

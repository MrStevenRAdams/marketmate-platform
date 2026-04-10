import { useState, useEffect, useCallback } from 'react';
import React from 'react';
import { useNavigate } from 'react-router-dom';
import { getActiveTenantId } from '../contexts/TenantContext';

type AnalyticsTab = 'overview' | 'pnl' | 'listing-health' | 'channel-health';

const API_BASE = import.meta.env.VITE_API_URL ?? 'http://localhost:8080/api/v1';

function api(path: string) {
  return fetch(`${API_BASE}${path}`, {
    headers: {
      'Content-Type': 'application/json',
      'X-Tenant-Id': getActiveTenantId(),
    },
  });
}

// ─── Types ───────────────────────────────────────────────────────────────────

type Period = 'today' | '7d' | '30d' | '90d';

interface OverviewData {
  period: string;
  date_from: string;
  date_to: string;
  total_orders: number;
  total_revenue: number;
  currency: string;
  avg_order_value: number;
  units_dispatched: number;
  orders_by_status: Record<string, number>;
}

interface OrdersData {
  date_from: string;
  date_to: string;
  by_status: Record<string, number>;
  by_channel: Record<string, number>;
  by_day: Array<{ date: string; count: number }>;
}

interface RevenueData {
  date_from: string;
  date_to: string;
  currency: string;
  total_revenue: number;
  avg_order_value: number;
  by_channel: Record<string, number>;
  by_day: Array<{ date: string; revenue: number; orders: number }>;
}

interface TopProductsData {
  date_from: string;
  date_to: string;
  currency: string;
  products: Array<{ sku: string; title: string; units: number; revenue: number; orders: number }>;
}

interface InventoryHealth {
  total_skus: number;
  out_of_stock: number;
  low_stock: number;
  healthy: number;
  overstock: number;
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

function fmtCurrency(amount: number, currency = 'GBP') {
  return new Intl.NumberFormat('en-GB', { style: 'currency', currency, maximumFractionDigits: 0 }).format(amount);
}

function fmtNumber(n: number) {
  return new Intl.NumberFormat('en-GB').format(n);
}

// ─── Channel Display Config ───────────────────────────────────────────────────

const CHANNEL_DISPLAY: Record<string, { label: string; color: string; isS4?: boolean }> = {
  // S4 — new channels
  backmarket: { label: 'Back Market', color: '#14B8A6', isS4: true },
  zalando:    { label: 'Zalando',     color: '#FF6600', isS4: true },
  bol:        { label: 'Bol.com',     color: '#0E4299', isS4: true },
  lazada:     { label: 'Lazada',      color: '#F57224', isS4: true },
  // Established channels
  amazon:      { label: 'Amazon',      color: '#FF9900' },
  ebay:        { label: 'eBay',        color: '#E53238' },
  shopify:     { label: 'Shopify',     color: '#96BF48' },
  temu:        { label: 'Temu',        color: '#EA6A35' },
  tiktok:      { label: 'TikTok',      color: '#010101' },
  etsy:        { label: 'Etsy',        color: '#F1641E' },
  woocommerce: { label: 'WooCommerce', color: '#7F54B3' },
  walmart:     { label: 'Walmart',     color: '#0071CE' },
  kaufland:    { label: 'Kaufland',    color: '#D40000' },
  magento:     { label: 'Magento',     color: '#EE672F' },
  bigcommerce: { label: 'BigCommerce', color: '#121118' },
  onbuy:       { label: 'OnBuy',       color: '#00A650' },
  direct:      { label: 'Direct',      color: '#8B5CF6' },
};

function chLabel(ch: string): string {
  return CHANNEL_DISPLAY[ch]?.label ?? (ch.charAt(0).toUpperCase() + ch.slice(1));
}

function chColor(ch: string, fallback = 'var(--primary)'): string {
  return CHANNEL_DISPLAY[ch]?.color ?? fallback;
}

function chIsS4(ch: string): boolean {
  return CHANNEL_DISPLAY[ch]?.isS4 === true;
}

// ─── Stat Card ───────────────────────────────────────────────────────────────

function StatCard({
  label,
  value,
  sub,
  accent,
  icon,
}: {
  label: string;
  value: string;
  sub?: string | React.ReactNode;
  accent?: string;
  icon: string;
}) {
  return (
    <div style={cardStyle}>
      <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', marginBottom: 12 }}>
        <span style={{ fontSize: 22 }}>{icon}</span>
        <span style={{
          fontSize: 11, fontWeight: 600, padding: '2px 8px',
          background: 'var(--bg-elevated)', color: 'var(--text-muted)',
          borderRadius: 20, border: '1px solid var(--border)',
        }}>TOTAL</span>
      </div>
      <div style={{ fontSize: 28, fontWeight: 700, color: accent ?? 'var(--text-primary)', letterSpacing: '-0.5px', lineHeight: 1 }}>
        {value}
      </div>
      <div style={{ fontSize: 13, color: 'var(--text-muted)', marginTop: 6 }}>{label}</div>
      {sub && <div style={{ fontSize: 12, color: 'var(--text-secondary)', marginTop: 4 }}>{sub}</div>}
    </div>
  );
}

// ─── Pie Chart (SVG) ─────────────────────────────────────────────────────────

function PieChart({ data }: { data: Array<{ label: string; value: number }> }) {
  if (!data.length) return <EmptyChart />;
  const total = data.reduce((s, d) => s + d.value, 0);
  if (!total) return <EmptyChart />;
  const R = 80; const cx = 100; const cy = 100;
  let angle = -Math.PI / 2;
  const slices = data.map(d => {
    const sweep = (d.value / total) * 2 * Math.PI;
    const startAngle = angle;
    angle += sweep;
    return { ...d, startAngle, sweep, pct: Math.round((d.value / total) * 100) };
  });
  const arc = (cx: number, cy: number, r: number, start: number, sweep: number) => {
    const x1 = cx + r * Math.cos(start), y1 = cy + r * Math.sin(start);
    const x2 = cx + r * Math.cos(start + sweep), y2 = cy + r * Math.sin(start + sweep);
    return `M ${cx} ${cy} L ${x1} ${y1} A ${r} ${r} 0 ${sweep > Math.PI ? 1 : 0} 1 ${x2} ${y2} Z`;
  };
  return (
    <div style={{ display: 'flex', gap: 20, alignItems: 'center', flexWrap: 'wrap' }}>
      <svg viewBox="0 0 200 200" style={{ width: 160, height: 160, flexShrink: 0 }}>
        {slices.map((s, i) => (
          <path key={i} d={arc(cx, cy, R, s.startAngle, s.sweep)} fill={chColor(s.label, `hsl(${i * 37},65%,55%)`)} opacity={0.88} stroke="var(--bg-secondary)" strokeWidth={1.5} />
        ))}
      </svg>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
        {slices.filter(s => s.pct > 0).map((s, i) => (
          <div key={i} style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 12 }}>
            <span style={{ width: 10, height: 10, borderRadius: 2, background: chColor(s.label, `hsl(${i * 37},65%,55%)`), flexShrink: 0, display: 'inline-block' }} />
            <span style={{ color: 'var(--text-secondary)' }}>{chLabel(s.label)}</span>
            <span style={{ fontWeight: 600, color: 'var(--text-primary)', marginLeft: 'auto', minWidth: 32, textAlign: 'right' }}>{s.pct}%</span>
          </div>
        ))}
      </div>
    </div>
  );
}

// ─── Bar Chart (SVG) ─────────────────────────────────────────────────────────

function BarChart({
  data,
  valueLabel,
  color,
  channelColors = false,
}: {
  data: Array<{ label: string; value: number }>;
  valueLabel: string;
  color: string;
  channelColors?: boolean;
}) {
  if (!data.length) return <EmptyChart />;
  const max = Math.max(...data.map(d => d.value), 1);
  const W = 480;
  const H = 200;
  const barH = Math.min(32, Math.floor((H - data.length * 8) / data.length));
  const labelW = 110;
  const chartW = W - labelW - 60;

  return (
    <svg viewBox={`0 0 ${W} ${Math.max(H, data.length * (barH + 10))}`} style={{ width: '100%', maxWidth: W, display: 'block' }}>
      {data.map((d, i) => {
        const y = i * (barH + 10);
        const barWidth = Math.max(2, (d.value / max) * chartW);
        const barColor = channelColors ? chColor(d.label, color) : color;
        const displayLabel = channelColors ? chLabel(d.label) : d.label;
        const isS4 = channelColors && chIsS4(d.label);
        return (
          <g key={d.label}>
            {/* S4 indicator */}
            {isS4 && (
              <text x={0} y={y + barH / 2 - 6} fontSize={8} fill={barColor}
                style={{ fontFamily: 'inherit', fontWeight: 700 }}>NEW</text>
            )}
            {/* Label */}
            <text x={0} y={y + barH / 2 + 4} fontSize={11} fill="var(--text-secondary)"
              style={{ fontFamily: 'inherit' }} textAnchor="start">
              {displayLabel.length > 14 ? displayLabel.slice(0, 13) + '…' : displayLabel}
            </text>
            {/* Track */}
            <rect x={labelW} y={y} width={chartW} height={barH} rx={4} fill="var(--bg-elevated)" />
            {/* Bar */}
            <rect x={labelW} y={y} width={barWidth} height={barH} rx={4} fill={barColor} opacity={0.85} />
            {/* Value */}
            <text x={labelW + barWidth + 6} y={y + barH / 2 + 4} fontSize={11}
              fill="var(--text-secondary)" style={{ fontFamily: 'inherit' }}>
              {fmtNumber(d.value)}
            </text>
          </g>
        );
      })}
      <text x={W / 2} y={Math.max(H, data.length * (barH + 10))} fontSize={10}
        fill="var(--text-muted)" textAnchor="middle" style={{ fontFamily: 'inherit' }}>
        {valueLabel}
      </text>
    </svg>
  );
}

// ─── Line Chart (SVG) ────────────────────────────────────────────────────────

function LineChart({
  data,
  color,
  format,
}: {
  data: Array<{ date: string; value: number }>;
  color: string;
  format: (n: number) => string;
}) {
  if (!data.length) return <EmptyChart />;

  const W = 500;
  const H = 160;
  const padL = 56;
  const padR = 16;
  const padT = 12;
  const padB = 28;
  const chartW = W - padL - padR;
  const chartH = H - padT - padB;

  const values = data.map(d => d.value);
  const maxV = Math.max(...values, 1);
  const minV = Math.min(...values, 0);
  const range = maxV - minV || 1;

  const pts = data.map((d, i) => {
    const x = padL + (i / Math.max(data.length - 1, 1)) * chartW;
    const y = padT + chartH - ((d.value - minV) / range) * chartH;
    return { x, y, d };
  });

  const polyline = pts.map(p => `${p.x},${p.y}`).join(' ');
  const area = [
    `${pts[0].x},${padT + chartH}`,
    ...pts.map(p => `${p.x},${p.y}`),
    `${pts[pts.length - 1].x},${padT + chartH}`,
  ].join(' ');

  // Y-axis ticks
  const ticks = [minV, (minV + maxV) / 2, maxV];

  // X-axis labels — show first, middle, last
  const xLabels = [0, Math.floor((data.length - 1) / 2), data.length - 1].filter(
    (v, i, a) => a.indexOf(v) === i && v < data.length,
  );

  return (
    <svg viewBox={`0 0 ${W} ${H}`} style={{ width: '100%', maxWidth: W, display: 'block' }}>
      {/* Area fill */}
      <polygon points={area} fill={color} opacity={0.08} />
      {/* Grid lines */}
      {ticks.map((t, i) => {
        const y = padT + chartH - ((t - minV) / range) * chartH;
        return (
          <g key={i}>
            <line x1={padL} y1={y} x2={W - padR} y2={y} stroke="var(--border)" strokeWidth={0.5} />
            <text x={padL - 6} y={y + 4} fontSize={9} fill="var(--text-muted)"
              textAnchor="end" style={{ fontFamily: 'inherit' }}>
              {format(t)}
            </text>
          </g>
        );
      })}
      {/* Line */}
      <polyline points={polyline} fill="none" stroke={color} strokeWidth={2} strokeLinejoin="round" />
      {/* Dots for sparse data */}
      {data.length <= 14 && pts.map((p, i) => (
        <circle key={i} cx={p.x} cy={p.y} r={3} fill={color} />
      ))}
      {/* X labels */}
      {xLabels.map(i => (
        <text key={i} x={pts[i].x} y={H - 4} fontSize={9} fill="var(--text-muted)"
          textAnchor="middle" style={{ fontFamily: 'inherit' }}>
          {data[i].date.slice(5)} {/* MM-DD */}
        </text>
      ))}
    </svg>
  );
}

function EmptyChart() {
  return (
    <div style={{ height: 120, display: 'flex', alignItems: 'center', justifyContent: 'center',
      color: 'var(--text-muted)', fontSize: 13, border: '1px dashed var(--border)', borderRadius: 8 }}>
      No data for this period
    </div>
  );
}

// ─── Section Header ──────────────────────────────────────────────────────────

function SectionHeader({ title, sub }: { title: string; sub?: string }) {
  return (
    <div style={{ marginBottom: 16 }}>
      <h2 style={{ margin: 0, fontSize: 16, fontWeight: 600, color: 'var(--text-primary)' }}>{title}</h2>
      {sub && <p style={{ margin: '2px 0 0', fontSize: 13, color: 'var(--text-muted)' }}>{sub}</p>}
    </div>
  );
}

// ─── Main Component ───────────────────────────────────────────────────────────

export default function Analytics() {
  const navigate = useNavigate();
  const [activeTab, setActiveTab] = useState<AnalyticsTab>('overview');
  const [period, setPeriod] = useState<Period>('30d');
  const [customFrom, setCustomFrom] = useState('');
  const [customTo, setCustomTo] = useState('');
  const [useCustom, setUseCustom] = useState(false);

  const [overview, setOverview] = useState<OverviewData | null>(null);
  const [orders, setOrders] = useState<OrdersData | null>(null);
  const [revenue, setRevenue] = useState<RevenueData | null>(null);
  const [topProducts, setTopProducts] = useState<TopProductsData | null>(null);
  const [inventory, setInventory] = useState<InventoryHealth | null>(null);

  // Compare period
  const [compareEnabled, setCompareEnabled] = useState(false);
  const [prevOverview, setPrevOverview] = useState<OverviewData | null>(null);
  const [prevRevenue, setPrevRevenue] = useState<RevenueData | null>(null);

  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const periodParams = useCallback(() => {
    if (useCustom && customFrom && customTo) {
      return `date_from=${customFrom}&date_to=${customTo}`;
    }
    return `period=${period}`;
  }, [period, useCustom, customFrom, customTo]);

  const loadAll = useCallback(async () => {
    setLoading(true);
    setError('');
    const q = periodParams();
    try {
      const [ovRes, ordRes, revRes, tpRes, invRes] = await Promise.all([
        api(`/analytics/overview?${q}`),
        api(`/analytics/orders?${q}`),
        api(`/analytics/revenue?${q}`),
        api(`/analytics/top-products?${q}`),
        api('/analytics/inventory'),
      ]);
      if (ovRes.ok) setOverview(await ovRes.json());
      if (ordRes.ok) setOrders(await ordRes.json());
      if (revRes.ok) setRevenue(await revRes.json());
      if (tpRes.ok) setTopProducts(await tpRes.json());
      if (invRes.ok) setInventory(await invRes.json());

      // Load previous period for comparison
      if (compareEnabled) {
        let prevQ = '';
        if (useCustom && customFrom && customTo) {
          const days = Math.round((new Date(customTo).getTime() - new Date(customFrom).getTime()) / 86400000);
          const prevTo = new Date(new Date(customFrom).getTime() - 86400000).toISOString().slice(0, 10);
          const prevFrom = new Date(new Date(prevTo).getTime() - days * 86400000).toISOString().slice(0, 10);
          prevQ = `date_from=${prevFrom}&date_to=${prevTo}`;
        } else {
          prevQ = `period=${period}&prev=1`;
        }
        const [prevOvRes, prevRevRes] = await Promise.all([
          api(`/analytics/overview?${prevQ}`),
          api(`/analytics/revenue?${prevQ}`),
        ]);
        if (prevOvRes.ok) setPrevOverview(await prevOvRes.json());
        if (prevRevRes.ok) setPrevRevenue(await prevRevRes.json());
      } else {
        setPrevOverview(null);
        setPrevRevenue(null);
      }
    } catch (e: any) {
      setError(e.message || 'Failed to load analytics');
    } finally {
      setLoading(false);
    }
  }, [periodParams, compareEnabled]);

  useEffect(() => { loadAll(); }, [loadAll]);

  const currency = overview?.currency ?? revenue?.currency ?? 'GBP';

  const pctDelta = (cur: number, prev: number) => {
    if (!prev) return null;
    const d = ((cur - prev) / prev) * 100;
    return { d, positive: d >= 0 };
  };

  const DeltaBadge = ({ cur, prev }: { cur: number; prev: number }) => {
    const r = pctDelta(cur, prev);
    if (!r) return null;
    return (
      <span style={{ fontSize: 11, fontWeight: 600, color: r.positive ? '#10b981' : '#ef4444', marginLeft: 6 }}>
        {r.positive ? '▲' : '▼'} {Math.abs(r.d).toFixed(1)}%
      </span>
    );
  };

  const channelBarData = orders
    ? Object.entries(orders.by_channel).map(([label, value]) => ({ label, value })).sort((a, b) => b.value - a.value)
    : [];
  const revenueChannelBarData = revenue
    ? Object.entries(revenue.by_channel).map(([label, value]) => ({ label, value })).sort((a, b) => b.value - a.value)
    : [];
  const revenueTrendData = revenue?.by_day.map(d => ({ date: d.date, value: d.revenue })) ?? [];
  const ordersTrendData = orders?.by_day.map(d => ({ date: d.date, value: d.count })) ?? [];

  const statusColors: Record<string, string> = {
    fulfilled: 'var(--success)',
    imported: 'var(--primary)',
    processing: 'var(--accent-cyan)',
    on_hold: 'var(--warning)',
    cancelled: 'var(--danger)',
    ready_to_fulfil: 'var(--accent-teal)',
  };

  const tabs: { key: AnalyticsTab; label: string }[] = [
    { key: 'overview', label: '📊 Overview' },
    { key: 'pnl', label: '💰 P&L' },
    { key: 'listing-health', label: '🏥 Listing Health' },
    { key: 'channel-health', label: '📡 Channel Health' },
  ];

  return (
    <div style={{ padding: '24px 32px', maxWidth: 1280, margin: '0 auto' }}>

      {/* ── Page Header ── */}
      <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', marginBottom: 20 }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 24, fontWeight: 700, color: 'var(--text-primary)' }}>Analytics</h1>
          <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 14 }}>
            Revenue, order, and inventory performance at a glance.
          </p>
        </div>
        {activeTab === 'overview' && (
          <button style={btnGhostStyle} onClick={loadAll}>↻ Refresh</button>
        )}
      </div>

      {/* ── Tab Nav ── */}
      <div style={{ display: 'flex', gap: 2, marginBottom: 24, borderBottom: '1px solid var(--border)' }}>
        {tabs.map(t => (
          <button
            key={t.key}
            onClick={() => setActiveTab(t.key)}
            style={{
              padding: '8px 18px',
              background: 'transparent',
              border: 'none',
              borderBottom: activeTab === t.key ? '2px solid var(--primary)' : '2px solid transparent',
              color: activeTab === t.key ? 'var(--primary)' : 'var(--text-secondary)',
              fontWeight: activeTab === t.key ? 600 : 400,
              fontSize: 13,
              cursor: 'pointer',
              marginBottom: -1,
              transition: 'all 0.15s',
            }}
          >
            {t.label}
          </button>
        ))}
      </div>

      {/* ── Tab Content ── */}
      {activeTab === 'pnl' && <PnLTab period={period} setPeriod={setPeriod} />}
      {activeTab === 'listing-health' && <ListingHealthTab />}
      {activeTab === 'channel-health' && <ChannelHealthTab />}

      {activeTab === 'overview' && (
        <div>
          {/* ── Period Selector ── */}
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 28, flexWrap: 'wrap' }}>
            {(['today', '7d', '30d', '90d'] as Period[]).map(p => (
              <button
                key={p}
                onClick={() => { setUseCustom(false); setPeriod(p); }}
                style={{
                  ...pillStyle,
                  background: !useCustom && period === p ? 'var(--primary)' : 'var(--bg-elevated)',
                  color: !useCustom && period === p ? 'white' : 'var(--text-secondary)',
                  borderColor: !useCustom && period === p ? 'var(--primary)' : 'var(--border)',
                }}
              >
                {p === 'today' ? 'Today' : p === '7d' ? 'Last 7 days' : p === '30d' ? 'Last 30 days' : 'Last 90 days'}
              </button>
            ))}
            <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginLeft: 8 }}>
              <input
                type="date" value={customFrom} onChange={e => setCustomFrom(e.target.value)}
                style={{ ...inputStyle, width: 140 }}
              />
              <span style={{ color: 'var(--text-muted)', fontSize: 12 }}>to</span>
              <input
                type="date" value={customTo} onChange={e => setCustomTo(e.target.value)}
                style={{ ...inputStyle, width: 140 }}
              />
              <button
                onClick={() => { if (customFrom && customTo) { setUseCustom(true); loadAll(); } }}
                style={{ ...btnGhostStyle, fontSize: 12, padding: '6px 12px' }}
                disabled={!customFrom || !customTo}
              >
                Apply
              </button>
            </div>
            <label style={{ display: 'flex', alignItems: 'center', gap: 8, marginLeft: 'auto', cursor: 'pointer', fontSize: 13, color: 'var(--text-secondary)' }}>
              <div onClick={() => setCompareEnabled(v => !v)} style={{ width: 36, height: 20, borderRadius: 10, cursor: 'pointer', transition: 'all 0.2s', background: compareEnabled ? 'var(--primary)' : 'var(--bg-elevated)', border: `1px solid ${compareEnabled ? 'var(--primary)' : 'var(--border)'}`, position: 'relative', flexShrink: 0 }}>
                <div style={{ width: 14, height: 14, borderRadius: '50%', background: '#fff', position: 'absolute', top: 2, left: compareEnabled ? 18 : 2, transition: 'left 0.2s' }} />
              </div>
              Compare to previous period
            </label>
          </div>

          {error && <div style={errorStyle}>{error}</div>}

          {loading && (
            <div style={{ textAlign: 'center', padding: '60px 0', color: 'var(--text-muted)', fontSize: 14 }}>
              Loading analytics…
            </div>
          )}

          {!loading && (
            <div>
              {/* ── Overview Cards ── */}
              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(220px, 1fr))', gap: 16, marginBottom: 32 }}>
                <StatCard icon="🛒" label="Total Orders" value={fmtNumber(overview?.total_orders ?? 0)} sub={compareEnabled && prevOverview ? <DeltaBadge cur={overview?.total_orders ?? 0} prev={prevOverview.total_orders} /> : undefined} />
                <StatCard icon="💰" label="Total Revenue" value={fmtCurrency(overview?.total_revenue ?? 0, currency)} accent="var(--success)" sub={compareEnabled && prevOverview ? <DeltaBadge cur={overview?.total_revenue ?? 0} prev={prevOverview.total_revenue} /> : undefined} />
                <StatCard icon="📊" label="Avg Order Value" value={fmtCurrency(overview?.avg_order_value ?? 0, currency)} accent="var(--accent-cyan)" sub={compareEnabled && prevOverview ? <DeltaBadge cur={overview?.avg_order_value ?? 0} prev={prevOverview.avg_order_value} /> : undefined} />
                <StatCard icon="📦" label="Units Dispatched" value={fmtNumber(overview?.units_dispatched ?? 0)} accent="var(--accent-purple)" />
              </div>

              {/* ── Order Status Breakdown ── */}
              {overview?.orders_by_status && Object.keys(overview.orders_by_status).length > 0 && (
                <div style={{ ...sectionCard, marginBottom: 28 }}>
                  <SectionHeader title="Orders by Status" sub={`${overview.date_from} – ${overview.date_to}`} />
                  <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap' }}>
                    {Object.entries(overview.orders_by_status)
                      .filter(([, v]) => v > 0)
                      .sort((a, b) => b[1] - a[1])
                      .map(([status, count]) => (
                        <div key={status} style={{
                          padding: '8px 16px', borderRadius: 20,
                          background: 'var(--bg-elevated)',
                          border: `1px solid ${statusColors[status] ?? 'var(--border)'}`,
                          display: 'flex', alignItems: 'center', gap: 8,
                        }}>
                          <span style={{ width: 8, height: 8, borderRadius: '50%', background: statusColors[status] ?? 'var(--text-muted)', display: 'inline-block' }} />
                          <span style={{ fontSize: 13, color: 'var(--text-secondary)', textTransform: 'capitalize' }}>
                            {status.replace(/_/g, ' ')}
                          </span>
                          <span style={{ fontSize: 13, fontWeight: 600, color: 'var(--text-primary)' }}>
                            {fmtNumber(count)}
                          </span>
                        </div>
                      ))}
                  </div>
                </div>
              )}

              {/* ── S4 Channel Spotlight ── */}
              {(() => {
                const s4Channels = ['backmarket', 'zalando', 'bol', 'lazada'];
                const s4OrderData = s4Channels
                  .map(ch => ({
                    ch,
                    orders: orders?.by_channel[ch] ?? 0,
                    revenue: revenue?.by_channel[ch] ?? 0,
                  }))
                  .filter(d => d.orders > 0 || d.revenue > 0);

                if (s4OrderData.length === 0) return null;

                return (
                  <div style={{ ...sectionCard, marginBottom: 28 }}>
                    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 16 }}>
                      <SectionHeader title="New Channels — S4 Spotlight" sub="Back Market · Zalando · Bol.com · Lazada performance this period" />
                      <span style={{ fontSize: 11, color: 'var(--text-muted)', padding: '3px 10px', background: 'var(--bg-elevated)', borderRadius: 20, border: '1px solid var(--border)' }}>
                        NEW
                      </span>
                    </div>
                    <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(200px, 1fr))', gap: 12 }}>
                      {s4Channels.map(ch => {
                        const cfg = CHANNEL_DISPLAY[ch];
                        const ordersCount = orders?.by_channel[ch] ?? 0;
                        const revenueAmt = revenue?.by_channel[ch] ?? 0;
                        const hasData = ordersCount > 0 || revenueAmt > 0;
                        return (
                          <div key={ch} style={{
                            padding: '14px 16px',
                            background: hasData ? 'var(--bg-elevated)' : 'var(--bg-elevated)',
                            border: `1px solid ${hasData ? cfg.color + '40' : 'var(--border)'}`,
                            borderLeft: `4px solid ${cfg.color}`,
                            borderRadius: 8,
                            opacity: hasData ? 1 : 0.45,
                          }}>
                            <div style={{ fontSize: 13, fontWeight: 700, color: cfg.color, marginBottom: 8 }}>
                              {cfg.label}
                            </div>
                            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline' }}>
                              <div>
                                <div style={{ fontSize: 20, fontWeight: 700, color: 'var(--text-primary)', lineHeight: 1 }}>
                                  {fmtNumber(ordersCount)}
                                </div>
                                <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 2 }}>orders</div>
                              </div>
                              <div style={{ textAlign: 'right' }}>
                                <div style={{ fontSize: 14, fontWeight: 600, color: 'var(--success)' }}>
                                  {revenueAmt > 0 ? fmtCurrency(revenueAmt, currency) : '—'}
                                </div>
                                <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 2 }}>revenue</div>
                              </div>
                            </div>
                            {!hasData && (
                              <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 6 }}>No orders this period</div>
                            )}
                          </div>
                        );
                      })}
                    </div>
                  </div>
                );
              })()}

              {/* ── Two-column: Orders + Revenue by Channel ── */}
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 20, marginBottom: 28 }}>
                <div style={sectionCard}>
                  <SectionHeader title="Orders by Channel" />
                  <BarChart data={channelBarData} valueLabel="orders" color="var(--primary)" channelColors />
                </div>
                <div style={sectionCard}>
                  <SectionHeader title="Revenue by Channel" />
                  <BarChart data={revenueChannelBarData.map(d => ({ ...d, value: Math.round(d.value) }))} valueLabel={currency} color="var(--success)" channelColors />
                </div>
              </div>

              {/* ── Sales Distribution Pie Chart ── */}
              {revenueChannelBarData.length > 0 && (
                <div style={{ ...sectionCard, marginBottom: 28 }}>
                  <SectionHeader title="Sales Distribution by Channel" sub="Revenue share across all channels this period" />
                  <PieChart data={revenueChannelBarData} />
                </div>
              )}

              {/* ── Revenue Trend ── */}
              <div style={{ ...sectionCard, marginBottom: 28 }}>
                <SectionHeader title="Revenue Trend" sub="Daily gross revenue (non-cancelled orders)" />
                <LineChart data={revenueTrendData} color="var(--success)" format={n => fmtCurrency(n, currency)} />
              </div>

              {/* ── Order Volume Trend ── */}
              <div style={{ ...sectionCard, marginBottom: 28 }}>
                <SectionHeader title="Order Volume Trend" sub="Daily order count" />
                <LineChart data={ordersTrendData} color="var(--primary)" format={n => String(Math.round(n))} />
              </div>

              {/* ── Top Products ── */}
              <div style={{ ...sectionCard, marginBottom: 28 }}>
                <SectionHeader title="Top Products by Units Sold" sub="Top 20 SKUs in selected period" />
                {topProducts && topProducts.products.length > 0 ? (
                  <div style={{ overflowX: 'auto' }}>
                    <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                      <thead>
                        <tr>
                          {['#', 'SKU', 'Product', 'Units Sold', 'Revenue', 'Orders'].map(h => (
                            <th key={h} style={thStyle}>{h}</th>
                          ))}
                        </tr>
                      </thead>
                      <tbody>
                        {topProducts.products.map((p, i) => (
                          <tr key={p.sku} style={{ borderBottom: '1px solid var(--border)' }}>
                            <td style={{ ...tdStyle, color: 'var(--text-muted)', width: 36 }}>{i + 1}</td>
                            <td style={{ ...tdStyle, fontFamily: 'monospace', fontSize: 12, color: 'var(--accent-cyan)' }}>{p.sku}</td>
                            <td style={{ ...tdStyle, maxWidth: 260 }}>
                              <span style={{ display: 'block', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                                {p.title || <span style={{ color: 'var(--text-muted)' }}>—</span>}
                              </span>
                            </td>
                            <td style={{ ...tdStyle, fontWeight: 600, color: 'var(--text-primary)' }}>{fmtNumber(p.units)}</td>
                            <td style={{ ...tdStyle, color: 'var(--success)' }}>{fmtCurrency(p.revenue, topProducts.currency)}</td>
                            <td style={{ ...tdStyle, color: 'var(--text-secondary)' }}>{p.orders}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                ) : (
                  <div style={{ color: 'var(--text-muted)', fontSize: 13, padding: '20px 0' }}>
                    No order line data available for this period.
                  </div>
                )}
              </div>

              {/* ── Inventory Health ── */}
              <div style={sectionCard}>
                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 16 }}>
                  <SectionHeader title="Inventory Health" sub="Current snapshot across all SKUs" />
                  <button style={{ ...btnGhostStyle, fontSize: 12 }} onClick={() => navigate('/inventory')}>
                    View Inventory →
                  </button>
                </div>
                {inventory ? (
                  <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(160px, 1fr))', gap: 12 }}>
                    <HealthCard label="Total SKUs" value={fmtNumber(inventory.total_skus)} color="var(--text-secondary)" />
                    <HealthCard label="Healthy" value={fmtNumber(inventory.healthy)} color="var(--success)" />
                    <HealthCard label="Low Stock" value={fmtNumber(inventory.low_stock)} color="var(--warning)" />
                    <HealthCard label="Out of Stock" value={fmtNumber(inventory.out_of_stock)} color="var(--danger)" />
                    <HealthCard label="Overstock" value={fmtNumber(inventory.overstock)} color="var(--accent-cyan)" />
                  </div>
                ) : (
                  <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>No inventory data.</div>
                )}
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

// ============================================================================
// P&L TAB
// ============================================================================

interface PnLItem {
  channel: string;
  gross_revenue: number;
  fee_rate: number;
  est_fees: number;
  net_revenue: number;
  cogs: number;
  est_margin: number;
  margin_pct: number;
  order_count: number;
  currency: string;
}

interface PnLResponse {
  date_from: string;
  date_to: string;
  currency: string;
  channels: PnLItem[];
  totals: PnLItem;
}

function PnLTab({ period, setPeriod }: { period: Period; setPeriod: (p: Period) => void }) {
  const [data, setData] = useState<PnLResponse | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    setLoading(true);
    fetch(`${API_BASE}/analytics/channel-pnl?period=${period}`, {
      headers: { 'X-Tenant-Id': getActiveTenantId() },
    })
      .then(r => r.ok ? r.json() : null)
      .then(d => { setData(d); setLoading(false); })
      .catch(() => setLoading(false));
  }, [period]);

  const fmt = (n: number, currency = 'GBP') =>
    new Intl.NumberFormat('en-GB', { style: 'currency', currency, maximumFractionDigits: 0 }).format(n);

  const fmtPct = (n: number) => `${n.toFixed(1)}%`;

  const marginColor = (pct: number) => pct >= 20 ? '#22c55e' : pct >= 5 ? '#f59e0b' : '#ef4444';

  return (
    <div>
      {/* Period selector */}
      <div style={{ display: 'flex', gap: 8, marginBottom: 24 }}>
        {(['7d', '30d', '90d'] as Period[]).map(p => (
          <button key={p} onClick={() => setPeriod(p)} style={{
            ...pillStyle,
            background: period === p ? 'var(--primary)' : 'var(--bg-elevated)',
            color: period === p ? 'white' : 'var(--text-secondary)',
            borderColor: period === p ? 'var(--primary)' : 'var(--border)',
          }}>{p === '7d' ? 'Last 7 days' : p === '30d' ? 'Last 30 days' : 'Last 90 days'}</button>
        ))}
      </div>

      {loading ? (
        <div style={{ color: 'var(--text-muted)', fontSize: 14 }}>Loading P&L data…</div>
      ) : !data ? (
        <div style={{ color: 'var(--text-muted)', fontSize: 14 }}>No data available.</div>
      ) : (
        <>
          {/* Totals summary cards */}
          {data.totals && (
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(160px, 1fr))', gap: 16, marginBottom: 28 }}>
              {[
                { label: 'Gross Revenue', value: fmt(data.totals.gross_revenue, data.currency), color: 'var(--primary)' },
                { label: 'Est. Channel Fees', value: fmt(data.totals.est_fees, data.currency), color: '#f59e0b' },
                { label: 'Net Revenue', value: fmt(data.totals.net_revenue, data.currency), color: 'var(--accent-cyan)' },
                { label: 'Est. Margin', value: fmtPct(data.totals.margin_pct), color: marginColor(data.totals.margin_pct) },
              ].map(card => (
                <div key={card.label} style={{ padding: '16px 20px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 10 }}>
                  <div style={{ fontSize: 22, fontWeight: 700, color: card.color }}>{card.value}</div>
                  <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 4 }}>{card.label}</div>
                </div>
              ))}
            </div>
          )}

          {/* Channel table */}
          <div style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 10, overflow: 'hidden' }}>
            <div style={{ padding: '16px 20px', borderBottom: '1px solid var(--border)' }}>
              <h3 style={{ margin: 0, fontSize: 15, fontWeight: 600 }}>Channel P&L Breakdown</h3>
              <p style={{ margin: '4px 0 0', fontSize: 12, color: 'var(--text-muted)' }}>
                Fee rates are estimates. Override via tenant settings.
              </p>
            </div>
            <div style={{ overflowX: 'auto' }}>
              <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                <thead>
                  <tr>
                    {['Channel', 'Orders', 'Gross Revenue', 'Fee Rate', 'Est. Fees', 'Net Revenue', 'COGS', 'Est. Margin', 'Margin %'].map(h => (
                      <th key={h} style={thStyle}>{h}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {data.channels.map(ch => (
                    <tr key={ch.channel} style={{ borderBottom: '1px solid var(--border)' }}>
                      <td style={{ ...tdStyle, fontWeight: 600, color: 'var(--text-primary)' }}>
                        <span style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                          <span style={{ width: 10, height: 10, borderRadius: '50%', background: chColor(ch.channel), display: 'inline-block', flexShrink: 0 }} />
                          {chLabel(ch.channel)}
                          {chIsS4(ch.channel) && (
                            <span style={{ fontSize: 9, fontWeight: 700, color: chColor(ch.channel), padding: '1px 5px', border: `1px solid ${chColor(ch.channel)}40`, borderRadius: 10 }}>NEW</span>
                          )}
                        </span>
                      </td>
                      <td style={tdStyle}>{ch.order_count}</td>
                      <td style={tdStyle}>{fmt(ch.gross_revenue, ch.currency)}</td>
                      <td style={tdStyle}>{fmtPct(ch.fee_rate * 100)}</td>
                      <td style={{ ...tdStyle, color: '#f59e0b' }}>{fmt(ch.est_fees, ch.currency)}</td>
                      <td style={tdStyle}>{fmt(ch.net_revenue, ch.currency)}</td>
                      <td style={tdStyle}>{ch.cogs > 0 ? fmt(ch.cogs, ch.currency) : '—'}</td>
                      <td style={{ ...tdStyle, color: marginColor(ch.margin_pct) }}>{fmt(ch.est_margin, ch.currency)}</td>
                      <td style={{ ...tdStyle, fontWeight: 600, color: marginColor(ch.margin_pct) }}>{fmtPct(ch.margin_pct)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </>
      )}
    </div>
  );
}

// ============================================================================
// LISTING HEALTH TAB
// ============================================================================

interface HealthItem {
  product_id: string;
  sku: string;
  title: string;
  score: number;
  breakdown: Record<string, number>;
}

function ListingHealthTab() {
  const [data, setData] = useState<{ total: number; products: HealthItem[] } | null>(null);
  const [loading, setLoading] = useState(true);
  const [filter, setFilter] = useState<'all' | 'poor' | 'fair' | 'good'>('all');

  useEffect(() => {
    setLoading(true);
    fetch(`${API_BASE}/analytics/listing-health`, {
      headers: { 'X-Tenant-Id': getActiveTenantId() },
    })
      .then(r => r.ok ? r.json() : null)
      .then(d => { setData(d); setLoading(false); })
      .catch(() => setLoading(false));
  }, []);

  const scoreColor = (s: number) => s >= 80 ? '#22c55e' : s >= 50 ? '#f59e0b' : '#ef4444';
  const scoreLabel = (s: number) => s >= 80 ? 'Good' : s >= 50 ? 'Fair' : 'Poor';

  const filtered = data?.products.filter(p => {
    if (filter === 'poor') return p.score < 50;
    if (filter === 'fair') return p.score >= 50 && p.score < 80;
    if (filter === 'good') return p.score >= 80;
    return true;
  }) ?? [];

  const counts = data ? {
    poor: data.products.filter(p => p.score < 50).length,
    fair: data.products.filter(p => p.score >= 50 && p.score < 80).length,
    good: data.products.filter(p => p.score >= 80).length,
  } : { poor: 0, fair: 0, good: 0 };

  return (
    <div>
      {/* Filter pills */}
      <div style={{ display: 'flex', gap: 8, marginBottom: 20 }}>
        {([
          { key: 'all', label: `All (${data?.total ?? 0})` },
          { key: 'poor', label: `Poor — <50 (${counts.poor})`, color: '#ef4444' },
          { key: 'fair', label: `Fair — 50-79 (${counts.fair})`, color: '#f59e0b' },
          { key: 'good', label: `Good — 80+ (${counts.good})`, color: '#22c55e' },
        ] as { key: typeof filter; label: string; color?: string }[]).map(f => (
          <button key={f.key} onClick={() => setFilter(f.key)} style={{
            ...pillStyle,
            background: filter === f.key ? (f.color ?? 'var(--primary)') : 'var(--bg-elevated)',
            color: filter === f.key ? 'white' : 'var(--text-secondary)',
            borderColor: filter === f.key ? (f.color ?? 'var(--primary)') : 'var(--border)',
          }}>{f.label}</button>
        ))}
      </div>

      {loading ? (
        <div style={{ color: 'var(--text-muted)', fontSize: 14 }}>Calculating health scores…</div>
      ) : (
        <div style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 10, overflow: 'hidden' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse' }}>
            <thead>
              <tr>
                {['Score', 'SKU', 'Title', 'Title', 'Desc', 'Images', 'Price', 'Barcode'].map((h, i) => (
                  <th key={i} style={thStyle}>{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {filtered.length === 0 ? (
                <tr><td colSpan={8} style={{ ...tdStyle, textAlign: 'center', padding: 32, color: 'var(--text-muted)' }}>No products found.</td></tr>
              ) : filtered.map(p => {
                const color = scoreColor(p.score);
                return (
                  <tr key={p.product_id} style={{ borderBottom: '1px solid var(--border)' }}>
                    <td style={{ ...tdStyle, fontWeight: 700 }}>
                      <span style={{ color, display: 'flex', alignItems: 'center', gap: 6 }}>
                        <span style={{
                          display: 'inline-block', width: 36, height: 36, borderRadius: '50%',
                          background: color + '20', border: `2px solid ${color}`,
                          textAlign: 'center', lineHeight: '32px', fontSize: 12, fontWeight: 700,
                        }}>{p.score}</span>
                        <span style={{ fontSize: 11, color }}>{scoreLabel(p.score)}</span>
                      </span>
                    </td>
                    <td style={{ ...tdStyle, fontFamily: 'monospace', fontSize: 12 }}>{p.sku}</td>
                    <td style={{ ...tdStyle, maxWidth: 280, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{p.title || '—'}</td>
                    <td style={tdStyle}>{p.breakdown.title ?? 0}/25</td>
                    <td style={tdStyle}>{p.breakdown.description ?? 0}/25</td>
                    <td style={tdStyle}>{p.breakdown.images ?? 0}/25</td>
                    <td style={tdStyle}>{p.breakdown.price ?? 0}/15</td>
                    <td style={tdStyle}>{p.breakdown.barcode ?? 0}/10</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

// ============================================================================
// CHANNEL HEALTH TAB (Reconciliation Health)
// ============================================================================

interface ReconcileRunSummary {
  job_id: string;
  channel: string;
  created_at: string;
  total: number;
  matched: number;
  match_rate: number;
  push_total: number;
  push_succeeded: number;
}

function ChannelHealthTab() {
  const [data, setData] = useState<{ channels: Record<string, ReconcileRunSummary[]> } | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    setLoading(true);
    fetch(`${API_BASE}/analytics/reconciliation-health`, {
      headers: { 'X-Tenant-Id': getActiveTenantId() },
    })
      .then(r => r.ok ? r.json() : null)
      .then(d => { setData(d); setLoading(false); })
      .catch(() => setLoading(false));
  }, []);

  const rateColor = (r: number) => r >= 80 ? '#22c55e' : r >= 50 ? '#f59e0b' : '#ef4444';

  if (loading) return <div style={{ color: 'var(--text-muted)', fontSize: 14 }}>Loading channel health…</div>;

  const channels = data?.channels ?? {};
  const channelKeys = Object.keys(channels);

  if (channelKeys.length === 0) {
    return <div style={{ color: 'var(--text-muted)', fontSize: 14 }}>No reconciliation history found. Run a reconciliation to see channel health data.</div>;
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 24 }}>
      {channelKeys.map(ch => {
        const runs = channels[ch];
        const avgRate = runs.length > 0 ? runs.reduce((a, r) => a + r.match_rate, 0) / runs.length : 0;
        const color = rateColor(avgRate);

        return (
          <div key={ch} style={{ background: 'var(--bg-secondary)', border: `1px solid ${chIsS4(ch) ? chColor(ch) + '40' : 'var(--border)'}`, borderRadius: 10, overflow: 'hidden' }}>
            <div style={{ padding: '16px 20px', borderBottom: '1px solid var(--border)', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
              <div>
                <h3 style={{ margin: 0, fontSize: 15, fontWeight: 600, display: 'flex', alignItems: 'center', gap: 8 }}>
                  <span style={{ width: 10, height: 10, borderRadius: '50%', background: chColor(ch), display: 'inline-block' }} />
                  {chLabel(ch)}
                  {chIsS4(ch) && (
                    <span style={{ fontSize: 9, fontWeight: 700, color: chColor(ch), padding: '2px 6px', border: `1px solid ${chColor(ch)}40`, borderRadius: 10 }}>NEW</span>
                  )}
                </h3>
                <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>{runs.length} run{runs.length !== 1 ? 's' : ''} recorded</div>
              </div>
              <div style={{ textAlign: 'right' }}>
                <div style={{ fontSize: 22, fontWeight: 700, color }}>{avgRate.toFixed(1)}%</div>
                <div style={{ fontSize: 11, color: 'var(--text-muted)' }}>Avg match rate</div>
              </div>
            </div>

            {/* Sparkline */}
            <div style={{ padding: '12px 20px', borderBottom: '1px solid var(--border)' }}>
              <div style={{ display: 'flex', gap: 3, alignItems: 'flex-end', height: 40 }}>
                {[...runs].reverse().map((run, i) => (
                  <div key={i} title={`${run.created_at?.slice(0, 10)}: ${run.match_rate}% match`} style={{
                    flex: 1, maxWidth: 24,
                    height: `${Math.max(4, run.match_rate * 0.4)}px`,
                    background: rateColor(run.match_rate),
                    borderRadius: 2,
                    opacity: 0.8,
                    cursor: 'default',
                  }} />
                ))}
              </div>
              <div style={{ fontSize: 10, color: 'var(--text-muted)', marginTop: 4 }}>Last {runs.length} runs (oldest → newest)</div>
            </div>

            {/* Run table */}
            <table style={{ width: '100%', borderCollapse: 'collapse' }}>
              <thead>
                <tr>
                  {['Date', 'Total SKUs', 'Matched', 'Match Rate', 'Pushed', 'Push Success'].map(h => (
                    <th key={h} style={thStyle}>{h}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {runs.slice(0, 8).map(run => (
                  <tr key={run.job_id} style={{ borderBottom: '1px solid var(--border)' }}>
                    <td style={tdStyle}>{run.created_at?.slice(0, 10) || '—'}</td>
                    <td style={tdStyle}>{run.total}</td>
                    <td style={tdStyle}>{run.matched}</td>
                    <td style={{ ...tdStyle, fontWeight: 600, color: rateColor(run.match_rate) }}>{run.match_rate.toFixed(1)}%</td>
                    <td style={tdStyle}>{run.push_total || '—'}</td>
                    <td style={tdStyle}>{run.push_succeeded || '—'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        );
      })}
    </div>
  );
}

function HealthCard({ label, value, color }: { label: string; value: string; color: string }) {
  return (
    <div style={{
      padding: '16px', background: 'var(--bg-elevated)',
      border: `1px solid var(--border)`, borderRadius: 8,
      borderTop: `3px solid ${color}`,
    }}>
      <div style={{ fontSize: 22, fontWeight: 700, color }}>{value}</div>
      <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 4 }}>{label}</div>
    </div>
  );
}

// ─── Styles ───────────────────────────────────────────────────────────────────

const cardStyle: React.CSSProperties = {
  padding: 20,
  background: 'var(--bg-secondary)',
  border: '1px solid var(--border)',
  borderRadius: 10,
};

const sectionCard: React.CSSProperties = {
  padding: 24,
  background: 'var(--bg-secondary)',
  border: '1px solid var(--border)',
  borderRadius: 10,
};

const pillStyle: React.CSSProperties = {
  padding: '6px 14px', borderRadius: 20, border: '1px solid',
  cursor: 'pointer', fontSize: 13, fontWeight: 500, transition: 'all 0.15s',
};

const inputStyle: React.CSSProperties = {
  padding: '6px 10px', background: 'var(--bg-elevated)',
  border: '1px solid var(--border)', borderRadius: 6,
  color: 'var(--text-primary)', fontSize: 13, outline: 'none',
};

const btnGhostStyle: React.CSSProperties = {
  padding: '8px 16px', background: 'transparent', color: 'var(--text-secondary)',
  border: '1px solid var(--border)', borderRadius: 6, cursor: 'pointer', fontSize: 13,
};

const errorStyle: React.CSSProperties = {
  marginBottom: 20, padding: '10px 14px', background: 'rgba(239,68,68,0.1)',
  border: '1px solid rgba(239,68,68,0.3)', borderRadius: 6,
  color: 'var(--danger)', fontSize: 13,
};

const thStyle: React.CSSProperties = {
  padding: '10px 16px', textAlign: 'left', fontSize: 11, fontWeight: 600,
  textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--text-muted)',
  borderBottom: '1px solid var(--border)', whiteSpace: 'nowrap',
};

const tdStyle: React.CSSProperties = {
  padding: '11px 16px', color: 'var(--text-secondary)', fontSize: 13,
};

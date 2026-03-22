import { useState, useEffect, useCallback } from 'react';
import { useParams, useNavigate } from 'react-router-dom';

const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';
function api(path: string, init?: RequestInit) {
  const tenantId = localStorage.getItem('active_tenant_id') || '';
  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': tenantId, ...init?.headers },
  });
}

// ── Types ─────────────────────────────────────────────────────────────────────

interface StockAdjustment {
  adjustment_id: string;
  product_id: string;
  product_sku: string;
  product_name: string;
  location_id: string;
  location_path: string;
  type: string;
  delta: number;
  quantity_after?: number;
  reference?: string;
  note?: string;
  created_at: string;
  created_by?: string;
}

interface Product {
  product_id: string;
  sku: string;
  title: string;
  total_on_hand?: number;
  total_available?: number;
}

// ── Helpers ──────────────────────────────────────────────────────────────────

const TYPE_CONFIG: Record<string, { label: string; colour: string; bg: string; icon: string }> = {
  adjust:    { label: 'Manual Adjust', colour: '#a78bfa', bg: 'rgba(139,92,246,0.12)',  icon: '✏️' },
  receive:   { label: 'Received',      colour: '#4ade80', bg: 'rgba(34,197,94,0.12)',   icon: '📥' },
  despatch:  { label: 'Despatched',    colour: '#60a5fa', bg: 'rgba(59,130,246,0.12)',  icon: '📤' },
  count:     { label: 'Stock Count',   colour: '#fbbf24', bg: 'rgba(251,191,36,0.12)',  icon: '🔢' },
  transfer:  { label: 'Transfer',      colour: '#f97316', bg: 'rgba(249,115,22,0.12)',  icon: '🔀' },
  scrap:     { label: 'Scrapped',      colour: '#f87171', bg: 'rgba(239,68,68,0.12)',   icon: '🗑️' },
  return:    { label: 'Return',        colour: '#34d399', bg: 'rgba(52,211,153,0.12)',  icon: '↩️' },
  reserved:  { label: 'Reserved',      colour: '#94a3b8', bg: 'rgba(148,163,184,0.12)', icon: '🔒' },
};

function getTypeCfg(type: string) {
  return TYPE_CONFIG[type?.toLowerCase()] ?? { label: type ?? 'Unknown', colour: '#6b7280', bg: 'rgba(107,114,128,0.12)', icon: '📋' };
}

function fmtDate(iso: string) {
  if (!iso) return '—';
  try {
    return new Date(iso).toLocaleString('en-GB', { day: 'numeric', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit' });
  } catch { return iso; }
}

function Skeleton({ w = '100%', h = 16, radius = 4 }: { w?: string | number; h?: number; radius?: number }) {
  return (
    <div style={{
      width: w, height: h, borderRadius: radius,
      background: 'linear-gradient(90deg, var(--bg-elevated) 0%, var(--bg-secondary) 50%, var(--bg-elevated) 100%)',
      backgroundSize: '200% 100%', animation: 'shimmer 1.5s infinite',
    }} />
  );
}

// ── Stock Level Timeline Chart ────────────────────────────────────────────────

function TimelineChart({ adjustments }: { adjustments: StockAdjustment[] }) {
  if (adjustments.length === 0) return null;

  // Build running total from oldest to newest
  const sorted = [...adjustments].sort((a, b) => a.created_at.localeCompare(b.created_at));
  let running = 0;
  const points = sorted.map(adj => {
    running += adj.delta;
    return { date: adj.created_at, level: running, type: adj.type, delta: adj.delta };
  });

  const max = Math.max(...points.map(p => p.level), 1);
  const min = Math.min(...points.map(p => p.level), 0);
  const range = max - min || 1;

  const chartWidth = 100; // percentage units
  const chartHeight = 80; // px

  // Build SVG polyline points
  const svgPoints = points.map((p, i) => {
    const x = points.length === 1 ? 50 : (i / (points.length - 1)) * chartWidth;
    const y = chartHeight - ((p.level - min) / range) * (chartHeight - 8) - 4;
    return `${x},${y}`;
  }).join(' ');

  return (
    <div style={{ position: 'relative', height: chartHeight + 24, marginBottom: 4 }}>
      <svg
        viewBox={`0 0 ${chartWidth} ${chartHeight}`}
        preserveAspectRatio="none"
        style={{ width: '100%', height: chartHeight, display: 'block' }}
      >
        {/* Fill area */}
        <polyline
          points={`0,${chartHeight} ${svgPoints} ${chartWidth},${chartHeight}`}
          fill="rgba(99,102,241,0.1)"
          stroke="none"
        />
        {/* Line */}
        <polyline
          points={svgPoints}
          fill="none"
          stroke="#6366f1"
          strokeWidth="1.5"
          strokeLinejoin="round"
          strokeLinecap="round"
          vectorEffect="non-scaling-stroke"
        />
        {/* Zero line */}
        {min < 0 && (
          <line
            x1="0" y1={chartHeight - ((0 - min) / range) * (chartHeight - 8) - 4}
            x2={chartWidth} y2={chartHeight - ((0 - min) / range) * (chartHeight - 8) - 4}
            stroke="rgba(239,68,68,0.3)" strokeWidth="0.8" strokeDasharray="2,2"
            vectorEffect="non-scaling-stroke"
          />
        )}
      </svg>
      {/* Y axis labels */}
      <div style={{ position: 'absolute', right: 0, top: 0, display: 'flex', flexDirection: 'column', justifyContent: 'space-between', height: chartHeight, fontSize: 10, color: 'var(--text-muted)', textAlign: 'right', paddingRight: 0 }}>
        <span>{max}</span>
        <span>{min}</span>
      </div>
    </div>
  );
}

// ── Main Component ────────────────────────────────────────────────────────────

export default function StockItemHistory() {
  const { productId } = useParams<{ productId: string }>();
  const navigate = useNavigate();
  const [adjustments, setAdjustments] = useState<StockAdjustment[]>([]);
  const [product, setProduct] = useState<Product | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [filterType, setFilterType] = useState<string>('all');
  const [filterLocation, setFilterLocation] = useState<string>('all');

  const load = useCallback(async () => {
    if (!productId) return;
    setLoading(true);
    setError(null);
    try {
      const [histRes, prodRes] = await Promise.all([
        api(`/products/${productId}/stock-history?limit=200`),
        api(`/products/${productId}`),
      ]);
      if (!histRes.ok) throw new Error('Failed to load stock history');
      const histData = await histRes.json();
      setAdjustments(histData.adjustments || []);
      if (prodRes.ok) {
        const pd = await prodRes.json();
        setProduct(pd.product || pd);
      }
    } catch {
      setError('Failed to load stock history.');
    } finally {
      setLoading(false);
    }
  }, [productId]);

  useEffect(() => { load(); }, [load]);

  const allTypes = Array.from(new Set(adjustments.map(a => a.type).filter(Boolean)));
  const allLocations = Array.from(new Set(adjustments.map(a => a.location_path || a.location_id).filter(Boolean)));

  const filtered = adjustments
    .filter(a => filterType === 'all' || a.type === filterType)
    .filter(a => filterLocation === 'all' || (a.location_path || a.location_id) === filterLocation)
    .sort((a, b) => b.created_at.localeCompare(a.created_at));

  // Compute running totals (newest → oldest means we add delta in reverse)
  const withRunning = (() => {
    const sorted = [...filtered].sort((a, b) => a.created_at.localeCompare(b.created_at));
    let running = 0;
    const map = new Map<string, number>();
    sorted.forEach(a => {
      running += a.delta;
      map.set(a.adjustment_id, running);
    });
    return filtered.map(a => ({ ...a, running_total: map.get(a.adjustment_id) ?? 0 }));
  })();

  return (
    <div style={{ padding: '24px 28px', maxWidth: 1200, margin: '0 auto' }}>
      <style>{`@keyframes shimmer { 0% { background-position: -200% 0 } 100% { background-position: 200% 0 } }`}</style>

      {/* Breadcrumb */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 20, fontSize: 13, color: 'var(--text-muted)' }}>
        <button onClick={() => navigate('/products')} style={{ background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-muted)', padding: 0, fontSize: 13 }}>Products</button>
        <span>›</span>
        {product && <button onClick={() => navigate(`/products/${productId}`)} style={{ background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-muted)', padding: 0, fontSize: 13 }}>{product.sku}</button>}
        {product && <span>›</span>}
        <span style={{ color: 'var(--text-primary)' }}>Stock History</span>
      </div>

      {/* Product header */}
      <div style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 12, padding: 24, marginBottom: 24, display: 'flex', alignItems: 'center', gap: 20 }}>
        <div style={{ width: 52, height: 52, borderRadius: 12, background: 'rgba(99,102,241,0.12)', display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 26, flexShrink: 0 }}>📦</div>
        {loading && !product ? (
          <div style={{ flex: 1 }}>
            <Skeleton h={18} w={240} />
            <div style={{ marginTop: 8 }}><Skeleton h={13} w={120} /></div>
          </div>
        ) : (
          <div style={{ flex: 1 }}>
            <div style={{ fontSize: 18, fontWeight: 700, color: 'var(--text-primary)' }}>{product?.title || productId}</div>
            <div style={{ display: 'flex', gap: 20, marginTop: 6 }}>
              {product?.sku && <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>SKU: <strong style={{ color: 'var(--text-secondary)' }}>{product.sku}</strong></span>}
              {product?.total_on_hand != null && <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>On Hand: <strong style={{ color: '#60a5fa' }}>{product.total_on_hand}</strong></span>}
              {product?.total_available != null && <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>Available: <strong style={{ color: '#4ade80' }}>{product.total_available}</strong></span>}
            </div>
          </div>
        )}
        <button
          onClick={() => navigate(`/products/${productId}`)}
          style={{ padding: '8px 16px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-secondary)', fontSize: 13, cursor: 'pointer' }}
        >
          ← Back to Product
        </button>
      </div>

      {/* Timeline chart */}
      {!loading && adjustments.length > 0 && (
        <div style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 12, padding: 24, marginBottom: 24 }}>
          <div style={{ fontSize: 14, fontWeight: 700, color: 'var(--text-primary)', marginBottom: 16 }}>Stock Level Timeline</div>
          <TimelineChart adjustments={adjustments.filter(a => filterType === 'all' || a.type === filterType)} />
          <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 11, color: 'var(--text-muted)', marginTop: 8 }}>
            <span>Oldest</span>
            <span>Most recent</span>
          </div>
        </div>
      )}

      {/* Filters + table */}
      <div style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 12, overflow: 'hidden' }}>
        {/* Toolbar */}
        <div style={{ padding: '16px 20px', borderBottom: '1px solid var(--border)', display: 'flex', alignItems: 'center', gap: 12, flexWrap: 'wrap' }}>
          <div style={{ fontWeight: 700, color: 'var(--text-primary)', fontSize: 14, marginRight: 8 }}>Stock Change Events</div>

          {/* Type filter */}
          <select
            value={filterType}
            onChange={e => setFilterType(e.target.value)}
            style={{ padding: '7px 12px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-secondary)', fontSize: 12, cursor: 'pointer', outline: 'none' }}
          >
            <option value="all">All Types</option>
            {allTypes.map(t => (
              <option key={t} value={t}>{getTypeCfg(t).label}</option>
            ))}
          </select>

          {/* Location filter */}
          {allLocations.length > 1 && (
            <select
              value={filterLocation}
              onChange={e => setFilterLocation(e.target.value)}
              style={{ padding: '7px 12px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-secondary)', fontSize: 12, cursor: 'pointer', outline: 'none' }}
            >
              <option value="all">All Locations</option>
              {allLocations.map(l => (
                <option key={l} value={l}>{l}</option>
              ))}
            </select>
          )}

          <span style={{ marginLeft: 'auto', fontSize: 12, color: 'var(--text-muted)' }}>
            {filtered.length} event{filtered.length !== 1 ? 's' : ''}
          </span>
        </div>

        {error ? (
          <div style={{ padding: 48, textAlign: 'center', color: '#f87171', fontSize: 14 }}>{error}</div>
        ) : loading ? (
          <div style={{ padding: 24, display: 'flex', flexDirection: 'column', gap: 10 }}>
            {[1,2,3,4,5,6].map(i => <Skeleton key={i} h={46} radius={6} />)}
          </div>
        ) : filtered.length === 0 ? (
          <div style={{ textAlign: 'center', padding: 64, color: 'var(--text-muted)' }}>
            <div style={{ fontSize: 40, marginBottom: 12 }}>📋</div>
            No stock events found{filterType !== 'all' || filterLocation !== 'all' ? ' for the selected filters.' : '.'}
          </div>
        ) : (
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
            <thead>
              <tr style={{ borderBottom: '1px solid var(--border)' }}>
                {['Date & Time', 'Type', 'Change', 'Running Total', 'Location', 'Reference'].map(h => (
                  <th key={h} style={{ padding: '11px 16px', textAlign: h === 'Change' || h === 'Running Total' ? 'right' : 'left', color: 'var(--text-muted)', fontWeight: 600, fontSize: 11, textTransform: 'uppercase', letterSpacing: '0.05em', background: 'var(--bg-elevated)' }}>{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {withRunning.map((adj, i) => {
                const cfg = getTypeCfg(adj.type);
                const isPositive = adj.delta > 0;
                return (
                  <tr
                    key={adj.adjustment_id}
                    style={{ borderBottom: '1px solid var(--border)', background: i % 2 === 0 ? 'transparent' : 'rgba(255,255,255,0.01)' }}
                  >
                    <td style={{ padding: '12px 16px', color: 'var(--text-muted)', fontSize: 12, whiteSpace: 'nowrap' }}>{fmtDate(adj.created_at)}</td>
                    <td style={{ padding: '12px 16px' }}>
                      <span style={{ display: 'inline-block', padding: '3px 8px', borderRadius: 4, fontSize: 11, fontWeight: 600, background: cfg.bg, color: cfg.colour }}>
                        {cfg.icon} {cfg.label}
                      </span>
                    </td>
                    <td style={{ padding: '12px 16px', textAlign: 'right', fontWeight: 700, fontSize: 14, color: isPositive ? '#4ade80' : '#f87171' }}>
                      {isPositive ? '+' : ''}{adj.delta}
                    </td>
                    <td style={{ padding: '12px 16px', textAlign: 'right', fontWeight: 600, color: adj.running_total < 0 ? '#f87171' : 'var(--text-primary)' }}>
                      {adj.running_total}
                    </td>
                    <td style={{ padding: '12px 16px', color: 'var(--text-secondary)', maxWidth: 180, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                      {adj.location_path || adj.location_id || '—'}
                    </td>
                    <td style={{ padding: '12px 16px', color: 'var(--text-muted)', fontSize: 12, maxWidth: 160, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                      {adj.reference || adj.note || '—'}
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

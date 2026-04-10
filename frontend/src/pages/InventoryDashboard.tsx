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

// ── Types ────────────────────────────────────────────────────────────────────

interface InventoryLocation {
  location_id: string;
  name: string;
  sku_count: number;
  total_on_hand: number;
  total_reserved: number;
  total_available: number;
  total_value: number;
}

interface InventoryDashboardData {
  total_skus: number;
  total_value: number;
  out_of_stock_count: number;
  locations: InventoryLocation[];
}

interface LocationInventoryRecord {
  inventory_id: string;
  product_id: string;
  location_name: string;
  quantity: number;
  reserved_qty: number;
  available_qty: number;
  sku?: string;
}

// ── Helpers ──────────────────────────────────────────────────────────────────

function fmtCurrency(val: number) {
  return val.toLocaleString('en-GB', { style: 'currency', currency: 'GBP', maximumFractionDigits: 2 });
}

function fmtNum(val: number) {
  return val.toLocaleString('en-GB');
}

// ── Mini progress bar ────────────────────────────────────────────────────────

function MiniBar({ value, total, colour }: { value: number; total: number; colour: string }) {
  const pct = total > 0 ? Math.min(100, (value / total) * 100) : 0;
  return (
    <div style={{ height: 4, background: 'var(--bg-elevated)', borderRadius: 2, overflow: 'hidden', width: 80 }}>
      <div style={{ height: '100%', width: `${pct}%`, background: colour, borderRadius: 2, transition: 'width 0.4s ease' }} />
    </div>
  );
}

// ── Skeleton ─────────────────────────────────────────────────────────────────

function Skeleton({ w = '100%', h = 16, radius = 4 }: { w?: string | number; h?: number; radius?: number }) {
  return (
    <div style={{
      width: w, height: h, borderRadius: radius,
      background: 'linear-gradient(90deg, var(--bg-elevated) 0%, var(--bg-secondary) 50%, var(--bg-elevated) 100%)',
      backgroundSize: '200% 100%',
      animation: 'shimmer 1.5s infinite',
    }} />
  );
}

// ── Location Detail Drawer ───────────────────────────────────────────────────

function LocationDrawer({
  location,
  onClose,
}: {
  location: InventoryLocation | null;
  onClose: () => void;
}) {
  const [records, setRecords] = useState<LocationInventoryRecord[]>([]);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!location) return;
    setLoading(true);
    api(`/inventory?location_id=${encodeURIComponent(location.location_id)}`)
      .then(r => r.json())
      .then(d => setRecords(d.inventory || []))
      .catch(() => setRecords([]))
      .finally(() => setLoading(false));
  }, [location]);

  if (!location) return null;

  return (
    <>
      <div
        onClick={onClose}
        style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.45)', zIndex: 299 }}
      />
      <div style={{
        position: 'fixed', top: 0, right: 0, height: '100vh', width: 520,
        background: 'var(--bg-secondary)', borderLeft: '1px solid var(--border)',
        zIndex: 300, display: 'flex', flexDirection: 'column',
        boxShadow: '-6px 0 32px rgba(0,0,0,0.35)',
      }}>
        {/* Header */}
        <div style={{ padding: '20px 24px', borderBottom: '1px solid var(--border)', display: 'flex', alignItems: 'center', gap: 12 }}>
          <div style={{ width: 36, height: 36, borderRadius: 8, background: 'rgba(99,102,241,0.15)', display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 18 }}>🏭</div>
          <div style={{ flex: 1 }}>
            <div style={{ fontWeight: 700, color: 'var(--text-primary)', fontSize: 16 }}>{location.name || location.location_id}</div>
            <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>{location.sku_count} SKUs · {fmtNum(location.total_on_hand)} on hand</div>
          </div>
          <button onClick={onClose} style={{ background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-muted)', fontSize: 22, lineHeight: 1, padding: 4 }}>×</button>
        </div>

        {/* Stats row */}
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: 1, borderBottom: '1px solid var(--border)', background: 'var(--border)' }}>
          {[
            { label: 'On Hand', value: fmtNum(location.total_on_hand), colour: '#60a5fa' },
            { label: 'Reserved', value: fmtNum(location.total_reserved), colour: '#fbbf24' },
            { label: 'Available', value: fmtNum(location.total_available), colour: '#4ade80' },
          ].map(s => (
            <div key={s.label} style={{ padding: '14px 20px', background: 'var(--bg-secondary)', textAlign: 'center' }}>
              <div style={{ fontSize: 20, fontWeight: 700, color: s.colour }}>{s.value}</div>
              <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 2 }}>{s.label}</div>
            </div>
          ))}
        </div>

        {/* Inventory list */}
        <div style={{ flex: 1, overflowY: 'auto', padding: '8px 0' }}>
          {loading ? (
            <div style={{ padding: 24, display: 'flex', flexDirection: 'column', gap: 12 }}>
              {[1,2,3,4,5].map(i => <Skeleton key={i} h={44} radius={6} />)}
            </div>
          ) : records.length === 0 ? (
            <div style={{ textAlign: 'center', padding: 48, color: 'var(--text-muted)' }}>
              <div style={{ fontSize: 32, marginBottom: 8 }}>📭</div>
              No inventory records found for this location.
            </div>
          ) : (
            <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
              <thead>
                <tr style={{ borderBottom: '1px solid var(--border)' }}>
                  {['Product', 'On Hand', 'Reserved', 'Available'].map(h => (
                    <th key={h} style={{ padding: '10px 16px', textAlign: h === 'Product' ? 'left' : 'right', color: 'var(--text-muted)', fontWeight: 600, fontSize: 11, textTransform: 'uppercase', letterSpacing: '0.05em' }}>{h}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {records.map((rec, i) => (
                  <tr key={rec.inventory_id} style={{ borderBottom: '1px solid var(--border)', background: i % 2 === 0 ? 'transparent' : 'rgba(255,255,255,0.01)' }}>
                    <td style={{ padding: '10px 16px', color: 'var(--text-primary)', maxWidth: 200 }}>
                      <div style={{ fontWeight: 500, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                        {rec.sku || rec.product_id}
                      </div>
                    </td>
                    <td style={{ padding: '10px 16px', textAlign: 'right', color: '#60a5fa', fontWeight: 600 }}>{fmtNum(rec.quantity)}</td>
                    <td style={{ padding: '10px 16px', textAlign: 'right', color: '#fbbf24', fontWeight: 600 }}>{fmtNum(rec.reserved_qty)}</td>
                    <td style={{ padding: '10px 16px', textAlign: 'right', color: rec.available_qty > 0 ? '#4ade80' : '#f87171', fontWeight: 600 }}>{fmtNum(rec.available_qty)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </div>
    </>
  );
}

// ── Main Component ────────────────────────────────────────────────────────────

export default function InventoryDashboard() {
  const navigate = useNavigate();
  const [data, setData] = useState<InventoryDashboardData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [selected, setSelected] = useState<InventoryLocation | null>(null);
  const [search, setSearch] = useState('');
  const [sortField, setSortField] = useState<keyof InventoryLocation>('total_on_hand');
  const [sortAsc, setSortAsc] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await api('/analytics/inventory-dashboard');
      if (!res.ok) throw new Error('Failed to load');
      const d = await res.json();
      setData(d);
    } catch {
      setError('Failed to load inventory dashboard. Please try again.');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { load(); }, [load]);

  const toggleSort = (field: keyof InventoryLocation) => {
    if (sortField === field) setSortAsc(a => !a);
    else { setSortField(field); setSortAsc(false); }
  };

  const filteredLocations = (data?.locations ?? [])
    .filter(l => !search || (l.name || l.location_id).toLowerCase().includes(search.toLowerCase()))
    .sort((a, b) => {
      const av = a[sortField], bv = b[sortField];
      const diff = typeof av === 'number' && typeof bv === 'number' ? av - bv : String(av).localeCompare(String(bv));
      return sortAsc ? diff : -diff;
    });

  const maxOnHand = Math.max(...(data?.locations ?? []).map(l => l.total_on_hand), 1);

  const SortIcon = ({ field }: { field: keyof InventoryLocation }) =>
    sortField === field ? <span style={{ marginLeft: 4, opacity: 0.7 }}>{sortAsc ? '▲' : '▼'}</span> : null;

  return (
    <div style={{ padding: '24px 28px', maxWidth: 1300, margin: '0 auto' }}>
      <style>{`
        @keyframes shimmer { 0% { background-position: -200% 0 } 100% { background-position: 200% 0 } }
      `}</style>

      {/* Page header */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 28 }}>
        <div>
          <h1 style={{ fontSize: 22, fontWeight: 700, color: 'var(--text-primary)', margin: 0 }}>Inventory Dashboard</h1>
          <p style={{ fontSize: 13, color: 'var(--text-muted)', margin: '4px 0 0' }}>Stock levels and value across all warehouse locations</p>
        </div>
        <div style={{ display: 'flex', gap: 10 }}>
          <button
            onClick={() => navigate('/warehouse-locations')}
            style={{ padding: '8px 16px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-secondary)', fontSize: 13, cursor: 'pointer' }}
          >
            🏭 Manage Locations
          </button>
          <button
            onClick={load}
            style={{ padding: '8px 16px', background: 'var(--primary)', border: 'none', borderRadius: 8, color: '#fff', fontSize: 13, cursor: 'pointer', fontWeight: 600 }}
          >
            ↻ Refresh
          </button>
        </div>
      </div>

      {/* Summary cards */}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 16, marginBottom: 28 }}>
        {loading ? (
          [1,2,3].map(i => (
            <div key={i} style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 12, padding: 20 }}>
              <Skeleton h={14} w="40%" />
              <div style={{ marginTop: 10 }}><Skeleton h={28} w="60%" /></div>
            </div>
          ))
        ) : [
          { label: 'Total SKUs', value: fmtNum(data?.total_skus ?? 0), icon: '📦', colour: '#60a5fa', sub: 'Unique products in stock' },
          { label: 'Estimated Stock Value', value: fmtCurrency(data?.total_value ?? 0), icon: '💰', colour: '#4ade80', sub: 'Based on cost price' },
          { label: 'Out of Stock', value: fmtNum(data?.out_of_stock_count ?? 0), icon: '⚠️', colour: data?.out_of_stock_count ? '#f87171' : '#4ade80', sub: 'Records with zero availability' },
        ].map(card => (
          <div key={card.label} style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 12, padding: 20, display: 'flex', alignItems: 'flex-start', gap: 16 }}>
            <div style={{ width: 44, height: 44, borderRadius: 10, background: `${card.colour}1a`, display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 22, flexShrink: 0 }}>
              {card.icon}
            </div>
            <div>
              <div style={{ fontSize: 12, color: 'var(--text-muted)', fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.04em' }}>{card.label}</div>
              <div style={{ fontSize: 26, fontWeight: 700, color: card.colour, lineHeight: 1.2, marginTop: 4 }}>{card.value}</div>
              <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 4 }}>{card.sub}</div>
            </div>
          </div>
        ))}
      </div>

      {/* Location table */}
      <div style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 12, overflow: 'hidden' }}>
        {/* Table toolbar */}
        <div style={{ padding: '16px 20px', borderBottom: '1px solid var(--border)', display: 'flex', alignItems: 'center', gap: 12 }}>
          <input
            value={search}
            onChange={e => setSearch(e.target.value)}
            placeholder="Search locations…"
            style={{ padding: '8px 12px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-primary)', fontSize: 13, width: 240, outline: 'none' }}
          />
          <span style={{ marginLeft: 'auto', fontSize: 12, color: 'var(--text-muted)' }}>
            {filteredLocations.length} location{filteredLocations.length !== 1 ? 's' : ''}
          </span>
        </div>

        {error ? (
          <div style={{ padding: 48, textAlign: 'center', color: '#f87171' }}>{error}</div>
        ) : loading ? (
          <div style={{ padding: 24 }}>
            {[1,2,3,4].map(i => (
              <div key={i} style={{ display: 'flex', gap: 16, padding: '14px 0', borderBottom: '1px solid var(--border)' }}>
                <Skeleton w={200} h={14} /><Skeleton w={60} h={14} /><Skeleton w={60} h={14} /><Skeleton w={60} h={14} />
              </div>
            ))}
          </div>
        ) : filteredLocations.length === 0 ? (
          <div style={{ textAlign: 'center', padding: 64, color: 'var(--text-muted)' }}>
            <div style={{ fontSize: 40, marginBottom: 12 }}>🏭</div>
            {search ? 'No locations match your search.' : 'No warehouse locations found. Add locations to get started.'}
          </div>
        ) : (
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
            <thead>
              <tr style={{ borderBottom: '1px solid var(--border)' }}>
                {[
                  { label: 'Location', field: 'name' as keyof InventoryLocation, align: 'left' },
                  { label: 'SKUs', field: 'sku_count' as keyof InventoryLocation, align: 'right' },
                  { label: 'On Hand', field: 'total_on_hand' as keyof InventoryLocation, align: 'right' },
                  { label: 'Reserved', field: 'total_reserved' as keyof InventoryLocation, align: 'right' },
                  { label: 'Available', field: 'total_available' as keyof InventoryLocation, align: 'right' },
                  { label: 'Est. Value', field: 'total_value' as keyof InventoryLocation, align: 'right' },
                  { label: 'Utilisation', field: null as unknown as keyof InventoryLocation, align: 'right' },
                ].map(col => (
                  <th
                    key={col.label}
                    onClick={col.field ? () => toggleSort(col.field) : undefined}
                    style={{
                      padding: '12px 16px', textAlign: col.align as 'left' | 'right',
                      color: 'var(--text-muted)', fontWeight: 600, fontSize: 11,
                      textTransform: 'uppercase', letterSpacing: '0.05em',
                      cursor: col.field ? 'pointer' : 'default',
                      userSelect: 'none',
                      background: 'var(--bg-elevated)',
                    }}
                  >
                    {col.label}{col.field && <SortIcon field={col.field} />}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {filteredLocations.map((loc, i) => (
                <tr
                  key={loc.location_id}
                  onClick={() => setSelected(loc)}
                  style={{
                    borderBottom: '1px solid var(--border)',
                    cursor: 'pointer',
                    background: i % 2 === 0 ? 'transparent' : 'rgba(255,255,255,0.01)',
                    transition: 'background 0.15s ease',
                  }}
                  onMouseEnter={e => (e.currentTarget.style.background = 'rgba(99,102,241,0.06)')}
                  onMouseLeave={e => (e.currentTarget.style.background = i % 2 === 0 ? 'transparent' : 'rgba(255,255,255,0.01)')}
                >
                  <td style={{ padding: '13px 16px' }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                      <div style={{ width: 30, height: 30, borderRadius: 6, background: 'rgba(99,102,241,0.1)', display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 14, flexShrink: 0 }}>🏭</div>
                      <div>
                        <div style={{ fontWeight: 600, color: 'var(--text-primary)' }}>{loc.name || loc.location_id}</div>
                        {loc.name && loc.location_id !== loc.name && (
                          <div style={{ fontSize: 11, color: 'var(--text-muted)' }}>{loc.location_id}</div>
                        )}
                      </div>
                    </div>
                  </td>
                  <td style={{ padding: '13px 16px', textAlign: 'right', color: 'var(--text-secondary)', fontWeight: 500 }}>{fmtNum(loc.sku_count)}</td>
                  <td style={{ padding: '13px 16px', textAlign: 'right', color: '#60a5fa', fontWeight: 600 }}>{fmtNum(loc.total_on_hand)}</td>
                  <td style={{ padding: '13px 16px', textAlign: 'right', color: '#fbbf24', fontWeight: 600 }}>{fmtNum(loc.total_reserved)}</td>
                  <td style={{ padding: '13px 16px', textAlign: 'right', color: loc.total_available > 0 ? '#4ade80' : '#f87171', fontWeight: 600 }}>{fmtNum(loc.total_available)}</td>
                  <td style={{ padding: '13px 16px', textAlign: 'right', color: 'var(--text-secondary)', fontWeight: 500 }}>{fmtCurrency(loc.total_value)}</td>
                  <td style={{ padding: '13px 16px', textAlign: 'right' }}>
                    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'flex-end', gap: 8 }}>
                      <span style={{ fontSize: 11, color: 'var(--text-muted)', minWidth: 32, textAlign: 'right' }}>
                        {maxOnHand > 0 ? `${Math.round((loc.total_on_hand / maxOnHand) * 100)}%` : '—'}
                      </span>
                      <MiniBar value={loc.total_on_hand} total={maxOnHand} colour="#6366f1" />
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {/* Location detail drawer */}
      <LocationDrawer location={selected} onClose={() => setSelected(null)} />
    </div>
  );
}

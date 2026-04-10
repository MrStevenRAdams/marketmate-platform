import { useState, useEffect } from 'react';
import { getActiveTenantId } from '../contexts/TenantContext';

const API = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';
function api(path: string, init?: RequestInit) {
  return fetch(`${API}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId(), ...(init?.headers || {}) },
  });
}

interface StockMove {
  move_id: string;
  sku: string;
  product_name?: string;
  from_binrack_id: string;
  from_binrack_name: string;
  to_binrack_id: string;
  to_binrack_name: string;
  quantity: number;
  reason: string;
  moved_by: string;
  moved_at: string;
  warehouse_id?: string;
}

type DateFilter = 'all' | 'today' | 'week';

function formatDate(iso: string) {
  if (!iso) return '—';
  const d = new Date(iso);
  return d.toLocaleDateString() + ' ' + d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

function isToday(iso: string) {
  const d = new Date(iso);
  const now = new Date();
  return d.toDateString() === now.toDateString();
}

function isThisWeek(iso: string) {
  const d = new Date(iso);
  const now = new Date();
  const weekAgo = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000);
  return d >= weekAgo;
}

export default function StockMoves() {
  const [moves, setMoves] = useState<StockMove[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [dateFilter, setDateFilter] = useState<DateFilter>('all');
  const [skuSearch, setSkuSearch] = useState('');
  const [reasonFilter, setReasonFilter] = useState('');

  useEffect(() => {
    api('/stock/moves')
      .then(r => r.json())
      .then(d => setMoves(d.moves || []))
      .catch(() => setError('Failed to load stock moves'))
      .finally(() => setLoading(false));
  }, []);

  const filtered = moves.filter(m => {
    if (skuSearch && !m.sku?.toLowerCase().includes(skuSearch.toLowerCase())) return false;
    if (reasonFilter && m.reason !== reasonFilter) return false;
    if (dateFilter === 'today') return isToday(m.moved_at);
    if (dateFilter === 'week') return isThisWeek(m.moved_at);
    return true;
  });

  const reasons = Array.from(new Set(moves.map(m => m.reason).filter(Boolean)));

  const tabStyle = (active: boolean): React.CSSProperties => ({
    padding: '8px 18px',
    background: active ? 'var(--primary, #7c3aed)' : 'var(--bg-secondary)',
    color: active ? 'white' : 'var(--text-secondary)',
    border: '1px solid var(--border)',
    borderRadius: 8,
    cursor: 'pointer',
    fontSize: 13,
    fontWeight: active ? 700 : 400,
    transition: 'all 0.15s',
  });

  return (
    <div style={{ padding: '28px 32px', maxWidth: 1200, margin: '0 auto' }}>
      {/* Header */}
      <div style={{ marginBottom: 28 }}>
        <h1 style={{ margin: 0, fontSize: 28, fontWeight: 800 }}>Stock Moves</h1>
        <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 14 }}>
          History of all stock movements between bin rack locations
        </p>
      </div>

      {/* Filters */}
      <div style={{ display: 'flex', gap: 12, marginBottom: 20, alignItems: 'center', flexWrap: 'wrap' }}>
        {/* Date filter tabs */}
        <div style={{ display: 'flex', gap: 4 }}>
          {(['all', 'today', 'week'] as DateFilter[]).map(f => (
            <button key={f} style={tabStyle(dateFilter === f)} onClick={() => setDateFilter(f)}>
              {f === 'all' ? 'All' : f === 'today' ? 'Today' : 'This Week'}
            </button>
          ))}
        </div>

        {/* SKU search */}
        <input
          placeholder="Search SKU…"
          value={skuSearch}
          onChange={e => setSkuSearch(e.target.value)}
          style={{
            padding: '8px 12px',
            background: 'var(--bg-secondary)',
            border: '1px solid var(--border)',
            borderRadius: 8,
            color: 'var(--text-primary)',
            fontSize: 13,
            width: 180,
          }}
        />

        {/* Reason filter */}
        <select
          value={reasonFilter}
          onChange={e => setReasonFilter(e.target.value)}
          style={{
            padding: '8px 12px',
            background: 'var(--bg-secondary)',
            border: '1px solid var(--border)',
            borderRadius: 8,
            color: 'var(--text-primary)',
            fontSize: 13,
          }}
        >
          <option value="">All Reasons</option>
          {reasons.map(r => <option key={r} value={r}>{r}</option>)}
        </select>

        <div style={{ marginLeft: 'auto', fontSize: 13, color: 'var(--text-muted)' }}>
          {filtered.length} move{filtered.length !== 1 ? 's' : ''}
        </div>
      </div>

      {/* Table */}
      {loading ? (
        <div style={{ padding: 60, textAlign: 'center', color: 'var(--text-muted)' }}>Loading…</div>
      ) : error ? (
        <div style={{ padding: 40, textAlign: 'center', color: '#ef4444' }}>{error}</div>
      ) : (
        <div style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 12, overflow: 'hidden' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
            <thead>
              <tr style={{ background: 'var(--bg-tertiary)' }}>
                {['Move ID', 'SKU', 'From', 'To', 'Qty', 'Reason', 'Moved By', 'Date'].map(h => (
                  <th key={h} style={{
                    textAlign: 'left',
                    padding: '10px 16px',
                    fontSize: 11,
                    fontWeight: 600,
                    textTransform: 'uppercase',
                    letterSpacing: '0.06em',
                    color: 'var(--text-muted)',
                    borderBottom: '1px solid var(--border)',
                    whiteSpace: 'nowrap',
                  }}>{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {filtered.length === 0 ? (
                <tr>
                  <td colSpan={8} style={{ padding: 48, textAlign: 'center', color: 'var(--text-muted)' }}>
                    No stock moves found
                    {(skuSearch || reasonFilter || dateFilter !== 'all') && (
                      <span> — <button
                        onClick={() => { setSkuSearch(''); setReasonFilter(''); setDateFilter('all'); }}
                        style={{ background: 'none', border: 'none', color: 'var(--primary, #7c3aed)', cursor: 'pointer', fontSize: 13 }}>
                        clear filters
                      </button></span>
                    )}
                  </td>
                </tr>
              ) : filtered.map((m, i) => (
                <tr key={m.move_id || i}
                  style={{ transition: 'background 0.1s' }}
                  onMouseEnter={e => (e.currentTarget.style.background = 'var(--bg-tertiary)')}
                  onMouseLeave={e => (e.currentTarget.style.background = '')}>
                  <td style={{ padding: '12px 16px', borderBottom: '1px solid var(--border)', fontFamily: 'monospace', fontSize: 11, color: 'var(--text-muted)' }}>
                    {m.move_id ? m.move_id.slice(0, 12) + '…' : '—'}
                  </td>
                  <td style={{ padding: '12px 16px', borderBottom: '1px solid var(--border)' }}>
                    <span style={{ fontFamily: 'monospace', fontWeight: 700, fontSize: 13 }}>{m.sku}</span>
                    {m.product_name && <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 2 }}>{m.product_name}</div>}
                  </td>
                  <td style={{ padding: '12px 16px', borderBottom: '1px solid var(--border)' }}>
                    <span style={{
                      background: 'rgba(239,68,68,0.1)',
                      color: '#ef4444',
                      padding: '2px 8px',
                      borderRadius: 5,
                      fontSize: 12,
                      fontWeight: 600,
                    }}>
                      {m.from_binrack_name || m.from_binrack_id || '—'}
                    </span>
                  </td>
                  <td style={{ padding: '12px 16px', borderBottom: '1px solid var(--border)' }}>
                    <span style={{
                      background: 'rgba(76,175,80,0.1)',
                      color: '#4CAF50',
                      padding: '2px 8px',
                      borderRadius: 5,
                      fontSize: 12,
                      fontWeight: 600,
                    }}>
                      {m.to_binrack_name || m.to_binrack_id || '—'}
                    </span>
                  </td>
                  <td style={{ padding: '12px 16px', borderBottom: '1px solid var(--border)', fontWeight: 700 }}>
                    {m.quantity}
                  </td>
                  <td style={{ padding: '12px 16px', borderBottom: '1px solid var(--border)' }}>
                    {m.reason ? (
                      <span style={{
                        background: 'var(--bg-tertiary)',
                        border: '1px solid var(--border)',
                        borderRadius: 5,
                        padding: '2px 8px',
                        fontSize: 12,
                        textTransform: 'capitalize',
                      }}>
                        {m.reason.replace(/_/g, ' ')}
                      </span>
                    ) : '—'}
                  </td>
                  <td style={{ padding: '12px 16px', borderBottom: '1px solid var(--border)', color: 'var(--text-muted)', fontSize: 12 }}>
                    {m.moved_by || '—'}
                  </td>
                  <td style={{ padding: '12px 16px', borderBottom: '1px solid var(--border)', color: 'var(--text-muted)', fontSize: 12, whiteSpace: 'nowrap' }}>
                    {formatDate(m.moved_at)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

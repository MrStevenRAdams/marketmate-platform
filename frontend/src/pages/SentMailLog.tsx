import { useState, useEffect, useRef } from 'react';
import { Link } from 'react-router-dom';
import { getActiveTenantId } from '../contexts/TenantContext';

const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';
function api(path: string, init?: RequestInit) {
  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId(), ...init?.headers },
  });
}

interface SentMailEntry {
  id: string;
  order_id: string;
  template_id: string;
  template_name: string;
  recipient: string;
  subject: string;
  status: string; // sent | failed | pending
  error_message: string;
  sent_at: string;
}

export default function SentMailLog() {
  const [items, setItems] = useState<SentMailEntry[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [expandedError, setExpandedError] = useState<string | null>(null);

  const [recipient, setRecipient] = useState('');
  const [statusFilter, setStatusFilter] = useState('');
  const [dateFrom, setDateFrom] = useState('');
  const [dateTo, setDateTo] = useState('');
  const [offset, setOffset] = useState(0);
  const limit = 50;

  const searchTimer = useRef<any>(null);

  const load = async (off = offset) => {
    setLoading(true);
    try {
      const params = new URLSearchParams({ limit: String(limit), offset: String(off) });
      if (recipient) params.set('recipient', recipient);
      if (statusFilter) params.set('status', statusFilter);
      if (dateFrom) params.set('date_from', dateFrom);
      if (dateTo) params.set('date_to', dateTo);
      const res = await api(`/sent-mail?${params}`);
      if (res.ok) {
        const data = await res.json();
        setItems(data.items || []);
        setTotal(data.total || 0);
      }
    } catch {
      setItems([]);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    clearTimeout(searchTimer.current);
    searchTimer.current = setTimeout(() => { setOffset(0); load(0); }, 300);
    return () => clearTimeout(searchTimer.current);
  }, [recipient, statusFilter, dateFrom, dateTo]);

  useEffect(() => { load(); }, [offset]);

  const fmt = (iso: string) => {
    if (!iso) return '—';
    try {
      return new Date(iso).toLocaleString('en-GB', {
        day: '2-digit', month: 'short', year: 'numeric',
        hour: '2-digit', minute: '2-digit',
      });
    } catch { return iso; }
  };

  const statusBadge = (status: string) => {
    const cfg: Record<string, { bg: string; color: string; label: string }> = {
      sent:    { bg: 'rgba(34,197,94,0.12)',  color: '#4ade80', label: '✓ Sent' },
      failed:  { bg: 'rgba(239,68,68,0.12)',  color: '#f87171', label: '✗ Failed' },
      pending: { bg: 'rgba(156,163,175,0.15)', color: '#9ca3af', label: '⏳ Pending' },
    };
    const s = cfg[status] ?? { bg: 'rgba(156,163,175,0.15)', color: '#9ca3af', label: status };
    return (
      <span style={{ background: s.bg, color: s.color, padding: '3px 10px', borderRadius: 5, fontSize: 12, fontWeight: 600 }}>
        {s.label}
      </span>
    );
  };

  const totalPages = Math.ceil(total / limit);
  const currentPage = Math.floor(offset / limit) + 1;

  return (
    <div style={{ padding: '32px 40px', maxWidth: 1200, margin: '0 auto' }}>
      {/* Header */}
      <div style={{ marginBottom: 28 }}>
        <h1 style={{ margin: 0, color: 'var(--text-primary)', fontSize: 22, fontWeight: 700 }}>📤 Sent Mail Log</h1>
        <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 14 }}>
          Full history of all outbound emails — sent, failed, and pending.
        </p>
      </div>

      {/* Filters */}
      <div style={{
        display: 'flex', gap: 12, flexWrap: 'wrap', marginBottom: 20,
        background: 'var(--bg-secondary)', border: '1px solid var(--border)',
        borderRadius: 10, padding: '14px 16px',
      }}>
        <input
          value={recipient}
          onChange={e => setRecipient(e.target.value)}
          placeholder="Search recipient…"
          style={{
            flex: '1 1 220px', padding: '8px 12px', borderRadius: 7,
            border: '1px solid var(--border)', background: 'var(--bg-primary)',
            color: 'var(--text-primary)', fontSize: 13,
          }}
        />

        <select
          value={statusFilter}
          onChange={e => setStatusFilter(e.target.value)}
          style={{
            padding: '8px 12px', borderRadius: 7, border: '1px solid var(--border)',
            background: 'var(--bg-primary)', color: 'var(--text-primary)', fontSize: 13,
          }}
        >
          <option value="">All Statuses</option>
          <option value="sent">Sent</option>
          <option value="failed">Failed</option>
          <option value="pending">Pending</option>
        </select>

        <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
          <label style={{ fontSize: 12, color: 'var(--text-muted)', whiteSpace: 'nowrap' }}>From</label>
          <input
            type="date"
            value={dateFrom}
            onChange={e => setDateFrom(e.target.value)}
            style={{
              padding: '8px 10px', borderRadius: 7, border: '1px solid var(--border)',
              background: 'var(--bg-primary)', color: 'var(--text-primary)', fontSize: 13,
            }}
          />
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
          <label style={{ fontSize: 12, color: 'var(--text-muted)', whiteSpace: 'nowrap' }}>To</label>
          <input
            type="date"
            value={dateTo}
            onChange={e => setDateTo(e.target.value)}
            style={{
              padding: '8px 10px', borderRadius: 7, border: '1px solid var(--border)',
              background: 'var(--bg-primary)', color: 'var(--text-primary)', fontSize: 13,
            }}
          />
        </div>

        {(recipient || statusFilter || dateFrom || dateTo) && (
          <button
            onClick={() => { setRecipient(''); setStatusFilter(''); setDateFrom(''); setDateTo(''); }}
            style={{
              padding: '8px 14px', borderRadius: 7, border: '1px solid var(--border)',
              background: 'var(--bg-elevated)', color: 'var(--text-muted)', fontSize: 13, cursor: 'pointer',
            }}
          >
            Clear
          </button>
        )}
      </div>

      {/* Table */}
      <div style={{ background: 'var(--bg-secondary)', borderRadius: 12, border: '1px solid var(--border)', overflow: 'hidden' }}>
        <table style={{ width: '100%', borderCollapse: 'collapse' }}>
          <thead>
            <tr style={{ borderBottom: '1px solid var(--border)' }}>
              {['Date / Time', 'Recipient', 'Subject', 'Template', 'Order ID', 'Status'].map(h => (
                <th key={h} style={{
                  padding: '12px 16px', textAlign: 'left', fontSize: 12,
                  fontWeight: 600, color: 'var(--text-muted)',
                  letterSpacing: '0.05em', textTransform: 'uppercase',
                }}>{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {loading ? (
              <tr>
                <td colSpan={6} style={{ padding: 40, textAlign: 'center', color: 'var(--text-muted)' }}>Loading…</td>
              </tr>
            ) : items.length === 0 ? (
              <tr>
                <td colSpan={6} style={{ padding: 60, textAlign: 'center', color: 'var(--text-muted)' }}>
                  <div style={{ fontSize: 32, marginBottom: 12 }}>📭</div>
                  <div style={{ fontWeight: 600, marginBottom: 6 }}>No emails logged yet</div>
                  <div style={{ fontSize: 13 }}>Sent emails will appear here automatically.</div>
                </td>
              </tr>
            ) : items.map((item, i) => (
              <>
                <tr
                  key={item.id}
                  style={{
                    borderBottom: '1px solid var(--border)',
                    background: item.status === 'failed' && expandedError === item.id
                      ? 'rgba(239,68,68,0.04)' : 'transparent',
                    cursor: item.status === 'failed' ? 'pointer' : 'default',
                  }}
                  onClick={() => {
                    if (item.status === 'failed') {
                      setExpandedError(expandedError === item.id ? null : item.id);
                    }
                  }}
                  title={item.status === 'failed' ? 'Click to expand error' : undefined}
                >
                  <td style={{ padding: '12px 16px', color: 'var(--text-muted)', fontSize: 13, whiteSpace: 'nowrap' }}>
                    {fmt(item.sent_at)}
                  </td>
                  <td style={{ padding: '12px 16px', color: 'var(--text-primary)', fontSize: 13 }}>
                    {item.recipient}
                  </td>
                  <td style={{
                    padding: '12px 16px', color: 'var(--text-secondary)', fontSize: 13,
                    maxWidth: 260, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
                  }} title={item.subject}>
                    {item.subject}
                  </td>
                  <td style={{ padding: '12px 16px' }}>
                    {item.template_name ? (
                      <span style={{
                        background: 'rgba(99,102,241,0.12)', color: '#818cf8',
                        fontSize: 11, fontWeight: 600, padding: '2px 8px', borderRadius: 4,
                      }}>
                        {item.template_name}
                      </span>
                    ) : <span style={{ color: 'var(--text-muted)', fontSize: 13 }}>—</span>}
                  </td>
                  <td style={{ padding: '12px 16px', fontSize: 13 }}>
                    {item.order_id ? (
                      <Link
                        to={`/orders/${item.order_id}`}
                        style={{ color: 'var(--accent-cyan)', textDecoration: 'none' }}
                        onClick={e => e.stopPropagation()}
                      >
                        {item.order_id.slice(0, 10)}…
                      </Link>
                    ) : <span style={{ color: 'var(--text-muted)' }}>—</span>}
                  </td>
                  <td style={{ padding: '12px 16px' }}>
                    {statusBadge(item.status)}
                    {item.status === 'failed' && (
                      <span style={{ marginLeft: 6, fontSize: 11, color: 'var(--text-muted)' }}>
                        {expandedError === item.id ? '▲' : '▼'}
                      </span>
                    )}
                  </td>
                </tr>
                {/* Expandable error row */}
                {item.status === 'failed' && expandedError === item.id && (
                  <tr key={`${item.id}-err`} style={{ borderBottom: '1px solid var(--border)', background: 'rgba(239,68,68,0.06)' }}>
                    <td colSpan={6} style={{ padding: '8px 16px 14px 48px' }}>
                      <span style={{ fontSize: 12, color: '#f87171', fontFamily: 'monospace' }}>
                        {item.error_message || 'Unknown error'}
                      </span>
                    </td>
                  </tr>
                )}
              </>
            ))}
          </tbody>
        </table>
      </div>

      {/* Pagination */}
      {totalPages > 1 && (
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginTop: 16 }}>
          <span style={{ fontSize: 13, color: 'var(--text-muted)' }}>
            Showing {offset + 1}–{Math.min(offset + limit, total)} of {total}
          </span>
          <div style={{ display: 'flex', gap: 8 }}>
            <button
              onClick={() => setOffset(Math.max(0, offset - limit))}
              disabled={offset === 0}
              style={{
                padding: '7px 16px', borderRadius: 7, border: '1px solid var(--border)',
                background: 'var(--bg-elevated)', color: 'var(--text-secondary)',
                fontSize: 13, cursor: offset === 0 ? 'not-allowed' : 'pointer', opacity: offset === 0 ? 0.5 : 1,
              }}
            >
              ← Prev
            </button>
            <span style={{ padding: '7px 12px', fontSize: 13, color: 'var(--text-muted)' }}>
              Page {currentPage} of {totalPages}
            </span>
            <button
              onClick={() => setOffset(offset + limit)}
              disabled={offset + limit >= total}
              style={{
                padding: '7px 16px', borderRadius: 7, border: '1px solid var(--border)',
                background: 'var(--bg-elevated)', color: 'var(--text-secondary)',
                fontSize: 13, cursor: offset + limit >= total ? 'not-allowed' : 'pointer',
                opacity: offset + limit >= total ? 0.5 : 1,
              }}
            >
              Next →
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

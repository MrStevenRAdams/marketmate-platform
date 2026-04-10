import { useEffect, useState, useRef } from 'react';
import { getActiveTenantId } from '../contexts/TenantContext';

const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';

function api(path: string, init?: RequestInit) {
  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId(), ...init?.headers },
  });
}

interface AutomationLog {
  log_id: string;
  type: string;
  channel: string;
  source: string;
  status: string;
  started_at: string;
  completed_at?: string;
  duration_seconds: number;
  records_processed: number;
  error_message?: string;
  ack: boolean;
}

const TYPE_ICONS: Record<string, string> = {
  import: '⬇️',
  order_import: '🛒',
  automation: '⚙️',
  ai_generation: '🤖',
  email: '📧',
  export: '📤',
  channel_sync: '🔄',
  listing_updates: '📝',
};

const STATUS_CONFIG: Record<string, { color: string; label: string }> = {
  running:   { color: '#3b82f6', label: 'Running'   },
  pending:   { color: '#f59e0b', label: 'Pending'   },
  completed: { color: '#22c55e', label: 'Completed' },
  error:     { color: '#ef4444', label: 'Error'     },
  cancelled: { color: '#6b7280', label: 'Cancelled' },
};

function statusBadge(status: string) {
  const cfg = STATUS_CONFIG[status] || { color: '#6b7280', label: status };
  return (
    <span style={{
      display: 'inline-flex', alignItems: 'center', gap: 4,
      padding: '2px 8px', borderRadius: '999px', fontSize: '11px', fontWeight: 700,
      background: cfg.color + '22', color: cfg.color, border: `1px solid ${cfg.color}55`,
    }}>
      {status === 'running' && <span style={{ animation: 'spin 1s linear infinite', display: 'inline-block' }}>↻</span>}
      {cfg.label}
    </span>
  );
}

function fmtDuration(secs: number) {
  if (!secs || secs < 0) return '—';
  if (secs < 60) return `${Math.round(secs)}s`;
  return `${Math.floor(secs / 60)}m ${Math.round(secs % 60)}s`;
}

function fmtDate(iso: string) {
  if (!iso) return '—';
  return new Date(iso).toLocaleString();
}

export default function AutomationLogs() {
  const [logs, setLogs] = useState<AutomationLog[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [clearing, setClearing] = useState(false);

  const [period, setPeriod] = useState('7d');
  const [filterType, setFilterType] = useState('all');
  const [filterStatus, setFilterStatus] = useState('all');
  const [page, setPage] = useState(1);
  const pageSize = 50;

  const [expandedError, setExpandedError] = useState<string | null>(null);
  const [filterRule, setFilterRule] = useState('all');
  const [rules, setRules] = useState<{ rule_id: string; name: string }[]>([]);
  const [logDetail, setLogDetail] = useState<AutomationLog | null>(null);

  const autoRefresh = useRef<any>(null);

  // Fetch automation rules for the filter dropdown
  useEffect(() => {
    api('/automation-rules').then(r => r.ok ? r.json() : { rules: [] }).then(d => setRules(d.rules || []));
  }, []);

  const load = async (pg = page) => {
    try {
      setLoading(true);
      const params = new URLSearchParams({
        period, type: filterType, status: filterStatus,
        page: String(pg), page_size: String(pageSize),
      });
      if (filterRule !== 'all') params.set('rule_id', filterRule);
      const res = await api(`/automation-logs?${params}`);
      if (!res.ok) throw new Error('Failed');
      const data = await res.json();
      setLogs(data.logs || []);
      setTotal(data.total || 0);
    } catch {
      setLogs([]);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    setPage(1);
    load(1);
  }, [period, filterType, filterStatus, filterRule]);

  // Auto-refresh every 30s if any running tasks
  useEffect(() => {
    const hasRunning = logs.some(l => l.status === 'running');
    if (hasRunning) {
      autoRefresh.current = setInterval(() => load(page), 30000);
    }
    return () => clearInterval(autoRefresh.current);
  }, [logs, page]);

  const handleClear = async () => {
    setClearing(true);
    try {
      const res = await api('/automation-logs/clear', { method: 'POST' });
      if (res.ok) {
        load(page);
      }
    } finally {
      setClearing(false);
    }
  };

  const totalPages = Math.ceil(total / pageSize);
  const errorCount = logs.filter(l => l.status === 'error' && !l.ack).length;

  return (
    <div className="page">
      {/* Header */}
      <div className="page-header">
        <div>
          <h1 className="page-title">📋 Automation Logs</h1>
          <p className="page-subtitle">History of all background tasks, syncs, and automations</p>
        </div>
        <div className="page-actions">
          {errorCount > 0 && (
            <button
              className="btn btn-secondary"
              onClick={handleClear}
              disabled={clearing}
              style={{ color: 'var(--danger)', borderColor: 'rgba(239,68,68,0.3)' }}
            >
              {clearing ? 'Clearing…' : `🗑️ Clear ${errorCount} Error${errorCount !== 1 ? 's' : ''}`}
            </button>
          )}
          <button className="btn btn-secondary" onClick={() => load(page)}>
            🔄 Refresh
          </button>
        </div>
      </div>

      {/* Filters */}
      <div className="card" style={{ padding: '12px 16px', marginBottom: 16 }}>
        <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', alignItems: 'center' }}>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
            <label style={{ fontSize: '11px', color: 'var(--text-muted)', fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.05em' }}>Period</label>
            <select className="select" value={period} onChange={e => setPeriod(e.target.value)} style={{ minWidth: 140 }}>
              <option value="today">Today</option>
              <option value="7d">Last 7 days</option>
              <option value="30d">Last 30 days</option>
              <option value="90d">Last 90 days</option>
            </select>
          </div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
            <label style={{ fontSize: '11px', color: 'var(--text-muted)', fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.05em' }}>Type</label>
            <select className="select" value={filterType} onChange={e => setFilterType(e.target.value)} style={{ minWidth: 160 }}>
              <option value="all">All Types</option>
              <option value="import">Marketplace Import</option>
              <option value="order_import">Order Import</option>
              <option value="automation">Automation Rule</option>
              <option value="ai_generation">AI Generation</option>
              <option value="email">Email</option>
              <option value="export">Export</option>
              <option value="channel_sync">Channel Sync</option>
              <option value="listing_updates">Listing Updates</option>
            </select>
          </div>
          {rules.length > 0 && (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
              <label style={{ fontSize: '11px', color: 'var(--text-muted)', fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.05em' }}>Rule / Macro</label>
              <select className="select" value={filterRule} onChange={e => setFilterRule(e.target.value)} style={{ minWidth: 160 }}>
                <option value="all">All Rules</option>
                {rules.map(r => <option key={r.rule_id} value={r.rule_id}>{r.name}</option>)}
              </select>
            </div>
          )}
          <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
            <label style={{ fontSize: '11px', color: 'var(--text-muted)', fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.05em' }}>Status</label>
            <select className="select" value={filterStatus} onChange={e => setFilterStatus(e.target.value)} style={{ minWidth: 140 }}>
              <option value="all">All Statuses</option>
              <option value="running">Running</option>
              <option value="pending">Pending</option>
              <option value="completed">Completed</option>
              <option value="error">Error</option>
              <option value="cancelled">Cancelled</option>
            </select>
          </div>
          <div style={{ marginLeft: 'auto', fontSize: '13px', color: 'var(--text-muted)', alignSelf: 'flex-end', paddingBottom: 4 }}>
            {total > 0 ? `${total.toLocaleString()} records` : ''}
          </div>
        </div>
      </div>

      {/* Table */}
      <div className="card">
        <div className="table-container">
          {loading ? (
            <div className="loading-state"><div className="spinner" /><p>Loading logs…</p></div>
          ) : logs.length === 0 ? (
            <div className="empty-state">
              <div className="empty-icon">📋</div>
              <h3>No logs found</h3>
              <p>No tasks match the current filters for the selected period.</p>
            </div>
          ) : (
            <table className="table">
              <thead>
                <tr>
                  <th>Type</th>
                  <th>Source / Channel</th>
                  <th>Status</th>
                  <th>Started</th>
                  <th>Duration</th>
                  <th>Records</th>
                  <th>Error</th>
                </tr>
              </thead>
              <tbody>
                {logs.map(log => (
                  <tr key={log.log_id} style={{ cursor: 'pointer' }} onClick={() => setLogDetail(log)}>
                    <td>
                      <span style={{ fontSize: '16px' }}>{TYPE_ICONS[log.type] || '📄'}</span>{' '}
                      <span style={{ fontSize: '12px', color: 'var(--text-muted)' }}>
                        {log.type.replace(/_/g, ' ')}
                      </span>
                    </td>
                    <td style={{ fontWeight: 500, color: 'var(--text-primary)' }}>{log.source}</td>
                    <td>{statusBadge(log.status)}</td>
                    <td style={{ fontSize: '12px', color: 'var(--text-muted)' }}>{fmtDate(log.started_at)}</td>
                    <td style={{ fontSize: '12px', color: 'var(--text-muted)' }}>{fmtDuration(log.duration_seconds)}</td>
                    <td style={{ fontSize: '13px' }}>
                      {log.records_processed > 0 ? log.records_processed.toLocaleString() : '—'}
                    </td>
                    <td>
                      {log.error_message ? (
                        <div>
                          <span
                            style={{
                              fontSize: '12px', color: 'var(--danger)', cursor: 'pointer',
                              maxWidth: 220, display: 'inline-block',
                              overflow: expandedError === log.log_id ? 'visible' : 'hidden',
                              textOverflow: 'ellipsis', whiteSpace: expandedError === log.log_id ? 'normal' : 'nowrap',
                            }}
                            onClick={() => setExpandedError(expandedError === log.log_id ? null : log.log_id)}
                            title={log.error_message}
                          >
                            {log.error_message}
                          </span>
                        </div>
                      ) : '—'}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>

        {/* Pagination */}
        {totalPages > 1 && (
          <div className="card-footer">
            <div className="pagination-info">
              Page {page} of {totalPages} ({total.toLocaleString()} total)
            </div>
            <div className="pagination">
              <button className="btn-icon" disabled={page <= 1} onClick={() => { setPage(p => p - 1); load(page - 1); }}>◀</button>
              {Array.from({ length: Math.min(totalPages, 7) }, (_, i) => {
                const start = Math.max(1, Math.min(page - 3, totalPages - 6));
                const p = start + i;
                if (p > totalPages) return null;
                return (
                  <button key={p} className={`btn-icon ${p === page ? 'active' : ''}`} onClick={() => { setPage(p); load(p); }}>{p}</button>
                );
              })}
              <button className="btn-icon" disabled={page >= totalPages} onClick={() => { setPage(p => p + 1); load(page + 1); }}>▶</button>
            </div>
          </div>
        )}
      </div>

      <style>{`
        @keyframes spin { to { transform: rotate(360deg); } }
      `}</style>

      {/* Log Detail Modal */}
      {logDetail && (
        <div
          onClick={() => setLogDetail(null)}
          style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.6)', zIndex: 1000, display: 'flex', alignItems: 'center', justifyContent: 'center', padding: 24 }}
        >
          <div
            onClick={e => e.stopPropagation()}
            style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 12, padding: 28, maxWidth: 600, width: '100%', maxHeight: '80vh', overflowY: 'auto' }}
          >
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 20 }}>
              <div style={{ fontWeight: 700, fontSize: 16, color: 'var(--text-primary)', display: 'flex', alignItems: 'center', gap: 8 }}>
                <span style={{ fontSize: 20 }}>{TYPE_ICONS[logDetail.type] || '📄'}</span>
                {logDetail.source}
              </div>
              <button onClick={() => setLogDetail(null)} style={{ background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-muted)', fontSize: 20, lineHeight: 1 }}>×</button>
            </div>

            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12, marginBottom: 16 }}>
              {[
                ['Rule', logDetail.source],
                ['Type', logDetail.type.replace(/_/g, ' ')],
                ['Status', logDetail.status],
                ['Channel', logDetail.channel || '—'],
                ['Started', fmtDate(logDetail.started_at)],
                ['Completed', logDetail.completed_at ? fmtDate(logDetail.completed_at) : '—'],
                ['Duration', fmtDuration(logDetail.duration_seconds)],
                ['Records', logDetail.records_processed > 0 ? String(logDetail.records_processed) : '—'],
              ].map(([label, value]) => (
                <div key={label}>
                  <div style={{ fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.04em', marginBottom: 2 }}>{label}</div>
                  <div style={{ fontSize: 13, color: 'var(--text-primary)' }}>{value}</div>
                </div>
              ))}
            </div>

            {logDetail.error_message && (
              <div style={{ marginTop: 8 }}>
                <div style={{ fontSize: 12, fontWeight: 600, color: '#ef4444', marginBottom: 6 }}>Error</div>
                <pre style={{ background: 'rgba(239,68,68,0.08)', border: '1px solid rgba(239,68,68,0.2)', borderRadius: 6, padding: 12, fontSize: 12, color: '#ef4444', whiteSpace: 'pre-wrap', wordBreak: 'break-all', margin: 0 }}>
                  {logDetail.error_message}
                </pre>
              </div>
            )}

            <div style={{ marginTop: 16, display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
              {logDetail.status === 'error' && (
                <button
                  onClick={async () => {
                    await api(`/automation-logs/${logDetail.log_id}/retry`, { method: 'POST' });
                    setLogDetail(null);
                    load(page);
                  }}
                  style={{ padding: '7px 16px', background: 'var(--accent-cyan)', border: 'none', borderRadius: 6, color: '#0f172a', fontWeight: 700, fontSize: 13, cursor: 'pointer' }}
                >
                  ↻ Retry
                </button>
              )}
              <button onClick={() => setLogDetail(null)} style={{ padding: '7px 16px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-secondary)', fontSize: 13, cursor: 'pointer' }}>
                Close
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

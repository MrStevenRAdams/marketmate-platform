import { useState, useEffect } from 'react';
import { getActiveTenantId } from '../contexts/TenantContext';

const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';
function api(path: string, init?: RequestInit) {
  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId(), ...init?.headers },
  });
}

interface EmailLog {
  id: string;
  tenant_id: string;
  template_id: string;
  template_type: string;
  recipient: string;
  subject: string;
  status: string;
  error?: string;
  order_id?: string;
  sent_at: string;
}

const TEMPLATE_TYPE_LABELS: Record<string, string> = {
  order_confirmation:    'Order Confirmation',
  despatch_notification: 'Despatch Notification',
  rma_update:            'RMA Update',
  low_stock_alert:       'Low Stock Alert',
};

export default function EmailLogs() {
  const [logs, setLogs] = useState<EmailLog[]>([]);
  const [loading, setLoading] = useState(true);
  const [statusFilter, setStatusFilter] = useState<string>('all');

  useEffect(() => { loadLogs(); }, [statusFilter]);

  async function loadLogs() {
    setLoading(true);
    try {
      const params = statusFilter !== 'all' ? `?status=${statusFilter}` : '';
      const res = await api(`/email-logs${params}`);
      if (res.ok) {
        const data = await res.json();
        setLogs(data.logs || []);
      }
    } finally {
      setLoading(false);
    }
  }

  const fmt = (iso: string) => {
    try {
      return new Date(iso).toLocaleString('en-GB', { day: '2-digit', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit' });
    } catch { return iso; }
  };

  const statusStyle = (status: string) =>
    status === 'sent'
      ? { background: 'rgba(34,197,94,0.12)', color: '#4ade80' }
      : { background: 'rgba(239,68,68,0.12)', color: '#f87171' };

  const typeLabel = (t: string) => TEMPLATE_TYPE_LABELS[t] ?? t;

  return (
    <div style={{ padding: '32px 40px', maxWidth: 1100, margin: '0 auto' }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 28 }}>
        <div>
          <h1 style={{ margin: 0, color: 'var(--text-primary)', fontSize: 22, fontWeight: 700 }}>📬 Email Logs</h1>
          <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 14 }}>Read-only audit log of all sent transactional emails</p>
        </div>

        {/* Status filter */}
        <div style={{ display: 'flex', gap: 8 }}>
          {(['all', 'sent', 'failed'] as const).map(s => (
            <button
              key={s}
              onClick={() => setStatusFilter(s)}
              style={{
                padding: '7px 16px', borderRadius: 8, fontSize: 13, fontWeight: 600, cursor: 'pointer',
                border: statusFilter === s ? 'none' : '1px solid var(--border)',
                background: statusFilter === s ? 'var(--primary)' : 'var(--bg-elevated)',
                color: statusFilter === s ? '#fff' : 'var(--text-secondary)',
              }}
            >
              {s.charAt(0).toUpperCase() + s.slice(1)}
            </button>
          ))}
        </div>
      </div>

      <div style={{ background: 'var(--bg-secondary)', borderRadius: 12, border: '1px solid var(--border)', overflow: 'hidden' }}>
        <table style={{ width: '100%', borderCollapse: 'collapse' }}>
          <thead>
            <tr style={{ borderBottom: '1px solid var(--border)' }}>
              {['Sent At', 'Recipient', 'Template', 'Subject', 'Status', 'Order'].map(h => (
                <th key={h} style={{ padding: '12px 16px', textAlign: 'left', fontSize: 12, fontWeight: 600, color: 'var(--text-muted)', letterSpacing: '0.05em', textTransform: 'uppercase' }}>{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {loading ? (
              <tr><td colSpan={6} style={{ padding: 40, textAlign: 'center', color: 'var(--text-muted)' }}>Loading…</td></tr>
            ) : logs.length === 0 ? (
              <tr>
                <td colSpan={6} style={{ padding: 60, textAlign: 'center', color: 'var(--text-muted)' }}>
                  <div style={{ fontSize: 32, marginBottom: 12 }}>📭</div>
                  <div style={{ fontWeight: 600, marginBottom: 6 }}>No emails logged yet</div>
                  <div style={{ fontSize: 13 }}>Sent emails will appear here automatically.</div>
                </td>
              </tr>
            ) : logs.map((log, i) => {
              const st = statusStyle(log.status);
              return (
                <tr key={log.id} style={{ borderBottom: i < logs.length - 1 ? '1px solid var(--border)' : 'none' }}>
                  <td style={{ padding: '12px 16px', color: 'var(--text-muted)', fontSize: 13, whiteSpace: 'nowrap' }}>{fmt(log.sent_at)}</td>
                  <td style={{ padding: '12px 16px', color: 'var(--text-primary)', fontSize: 13 }}>{log.recipient}</td>
                  <td style={{ padding: '12px 16px' }}>
                    <span style={{ background: 'rgba(99,102,241,0.12)', color: '#818cf8', fontSize: 11, fontWeight: 600, padding: '2px 8px', borderRadius: 4 }}>
                      {typeLabel(log.template_type)}
                    </span>
                  </td>
                  <td style={{ padding: '12px 16px', color: 'var(--text-secondary)', fontSize: 13, maxWidth: 280, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
                    title={log.subject}>
                    {log.subject}
                  </td>
                  <td style={{ padding: '12px 16px' }}>
                    <span style={{ ...st, padding: '3px 10px', borderRadius: 5, fontSize: 12, fontWeight: 600 }} title={log.error}>
                      {log.status === 'sent' ? '✓ Sent' : '✗ Failed'}
                    </span>
                  </td>
                  <td style={{ padding: '12px 16px', color: 'var(--text-muted)', fontSize: 13 }}>
                    {log.order_id ? (
                      <a href={`/orders/${log.order_id}`} style={{ color: 'var(--accent-cyan)', textDecoration: 'none' }}>
                        {log.order_id.slice(0, 8)}…
                      </a>
                    ) : '—'}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}

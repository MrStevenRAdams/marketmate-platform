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

interface DeliveryException {
  exception_id: string;
  shipment_id: string;
  tracking_number?: string;
  carrier_id?: string;
  order_ids?: string[];
  exception_type: string;  // undeliverable | returned | lost | damaged
  description: string;
  acknowledged: boolean;
  acknowledged_at?: string;
  created_at: string;
  updated_at: string;
}

const EXCEPTION_CONFIG: Record<string, { label: string; icon: string; colour: string; bg: string }> = {
  undeliverable: { label: 'Undeliverable', icon: '🚫', colour: '#ef4444', bg: 'rgba(239,68,68,0.1)' },
  returned:      { label: 'Returned',      icon: '↩️', colour: '#f59e0b', bg: 'rgba(245,158,11,0.1)' },
  lost:          { label: 'Lost',           icon: '🔍', colour: '#8b5cf6', bg: 'rgba(139,92,246,0.1)' },
  damaged:       { label: 'Damaged',        icon: '⚠️', colour: '#f97316', bg: 'rgba(249,115,22,0.1)' },
};

function fmtDate(s?: string) {
  if (!s) return '—';
  const d = new Date(s);
  return isNaN(d.getTime()) ? '—' : d.toLocaleDateString('en-GB', { day: 'numeric', month: 'short', year: 'numeric' });
}

// ─── Main Component ─────────────────────────────────────────────────────────────

export default function DeliveryExceptions() {
  const navigate = useNavigate();
  const [exceptions, setExceptions] = useState<DeliveryException[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [showAcknowledged, setShowAcknowledged] = useState(false);
  const [acknowledging, setAcknowledging] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const params = new URLSearchParams();
      if (!showAcknowledged) params.set('unacknowledged', 'true');
      const res = await api(`/dispatch/exceptions?${params.toString()}`);
      if (res.ok) {
        const data = await res.json();
        setExceptions(data.exceptions || []);
      }
    } catch {
      setError('Failed to load exceptions');
    } finally {
      setLoading(false);
    }
  }, [showAcknowledged]);

  useEffect(() => { load(); }, [load]);

  const acknowledge = async (ex: DeliveryException) => {
    setAcknowledging(ex.exception_id);
    try {
      const res = await api(`/dispatch/exceptions/${ex.exception_id}/acknowledge`, { method: 'POST' });
      if (!res.ok) throw new Error('Failed');
      await load();
    } catch {
      setError('Failed to acknowledge exception');
    } finally {
      setAcknowledging(null);
    }
  };

  const unacknowledgedCount = exceptions.filter(e => !e.acknowledged).length;
  const typeGroups = ['undeliverable', 'returned', 'lost', 'damaged'].map(t => ({
    type: t,
    count: exceptions.filter(e => e.exception_type === t && !e.acknowledged).length,
  }));

  return (
    <div style={{ padding: '28px 32px', maxWidth: 1200, margin: '0 auto' }}>

      {/* Header */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 24 }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 24, fontWeight: 700, color: 'var(--text-primary)' }}>
            ⚠️ Delivery Exceptions
          </h1>
          <p style={{ margin: '4px 0 0', fontSize: 14, color: 'var(--text-muted)' }}>
            Undeliverable, returned, lost, and damaged parcels requiring attention.
          </p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13, color: 'var(--text-secondary)', cursor: 'pointer' }}>
            <input type="checkbox" checked={showAcknowledged} onChange={e => setShowAcknowledged(e.target.checked)} />
            Show acknowledged
          </label>
          <button onClick={load} style={btnGhost}>↻ Refresh</button>
        </div>
      </div>

      {/* Summary Cards */}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, 1fr)', gap: 14, marginBottom: 24 }}>
        {typeGroups.map(({ type, count }) => {
          const cfg = EXCEPTION_CONFIG[type] || { label: type, icon: '⚠️', colour: '#94a3b8', bg: 'rgba(148,163,184,0.1)' };
          return (
            <div key={type} style={{ padding: '16px 18px', background: 'var(--bg-secondary)', border: `1px solid ${count > 0 ? cfg.colour + '44' : 'var(--border)'}`, borderRadius: 10 }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 8 }}>
                <span style={{ fontSize: 18 }}>{cfg.icon}</span>
                <span style={{ fontSize: 11, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--text-muted)' }}>
                  {cfg.label}
                </span>
              </div>
              <div style={{ fontSize: 28, fontWeight: 800, color: count > 0 ? cfg.colour : 'var(--text-muted)' }}>{count}</div>
              <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 2 }}>open</div>
            </div>
          );
        })}
      </div>

      {error && (
        <div style={{ marginBottom: 14, padding: '10px 14px', background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 6, color: '#ef4444', fontSize: 13 }}>
          {error}
        </div>
      )}

      {loading ? (
        <div style={{ textAlign: 'center', padding: '50px 0', color: 'var(--text-muted)', fontSize: 14 }}>Loading…</div>
      ) : exceptions.length === 0 ? (
        <div style={{ textAlign: 'center', padding: '60px 0', color: 'var(--text-muted)', fontSize: 14 }}>
          <div style={{ fontSize: 40, marginBottom: 12 }}>✅</div>
          {showAcknowledged ? 'No delivery exceptions.' : 'No open exceptions — all clear!'}
        </div>
      ) : (
        <div style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 10, overflow: 'hidden' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse' }}>
            <thead>
              <tr>
                {['Type', 'Shipment / Tracking', 'Orders', 'Description', 'Flagged', 'Status', 'Action'].map(h => (
                  <th key={h} style={thStyle}>{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {exceptions.map(ex => {
                const cfg = EXCEPTION_CONFIG[ex.exception_type] || { label: ex.exception_type, icon: '⚠️', colour: '#94a3b8', bg: 'rgba(148,163,184,0.1)' };
                return (
                  <tr key={ex.exception_id} style={{ borderBottom: '1px solid var(--border)', opacity: ex.acknowledged ? 0.55 : 1 }}>
                    <td style={tdStyle}>
                      <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6, fontSize: 12, fontWeight: 600, background: cfg.bg, color: cfg.colour, border: `1px solid ${cfg.colour}44`, borderRadius: 6, padding: '4px 10px' }}>
                        {cfg.icon} {cfg.label}
                      </span>
                    </td>
                    <td style={tdStyle}>
                      <div style={{ fontSize: 12, fontFamily: 'monospace', color: 'var(--text-primary)' }}>{ex.shipment_id.slice(0, 14)}…</div>
                      {ex.tracking_number && (
                        <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 2 }}>{ex.tracking_number}</div>
                      )}
                    </td>
                    <td style={tdStyle}>
                      {(ex.order_ids || []).map(oid => (
                        <span key={oid} style={{ fontSize: 11, fontFamily: 'monospace', display: 'block', color: 'var(--primary)', cursor: 'pointer' }}
                          onClick={() => navigate(`/orders?highlight=${oid}`)}>
                          {oid.slice(0, 12)}
                        </span>
                      ))}
                    </td>
                    <td style={{ ...tdStyle, maxWidth: 260 }}>
                      <span style={{ fontSize: 12, color: 'var(--text-secondary)' }}>{ex.description}</span>
                    </td>
                    <td style={{ ...tdStyle, whiteSpace: 'nowrap' }}>{fmtDate(ex.created_at)}</td>
                    <td style={tdStyle}>
                      {ex.acknowledged ? (
                        <span style={{ fontSize: 11, color: '#22c55e', fontWeight: 600 }}>✓ Acknowledged</span>
                      ) : (
                        <span style={{ fontSize: 11, color: '#ef4444', fontWeight: 600 }}>● Open</span>
                      )}
                    </td>
                    <td style={tdStyle}>
                      {!ex.acknowledged && (
                        <button
                          onClick={() => acknowledge(ex)}
                          disabled={acknowledging === ex.exception_id}
                          style={{ ...btnSmall, fontSize: 11 }}
                        >
                          {acknowledging === ex.exception_id ? '…' : 'Acknowledge'}
                        </button>
                      )}
                    </td>
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

// ─── Styles ───────────────────────────────────────────────────────────────────

const thStyle: React.CSSProperties = { padding: '10px 14px', textAlign: 'left', fontSize: 11, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--text-muted)', borderBottom: '1px solid var(--border)', whiteSpace: 'nowrap', background: 'var(--bg-elevated)' };
const tdStyle: React.CSSProperties = { padding: '12px 14px', color: 'var(--text-secondary)', fontSize: 13 };
const btnGhost: React.CSSProperties = { padding: '8px 16px', background: 'transparent', color: 'var(--text-secondary)', border: '1px solid var(--border)', borderRadius: 6, cursor: 'pointer', fontSize: 13 };
const btnSmall: React.CSSProperties = { padding: '5px 12px', background: 'transparent', color: 'var(--text-secondary)', border: '1px solid var(--border)', borderRadius: 5, cursor: 'pointer', fontSize: 12, whiteSpace: 'nowrap' };

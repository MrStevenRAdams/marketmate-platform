import { useState, useEffect, useCallback } from 'react';
import { getActiveTenantId } from '../contexts/TenantContext';

const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';
function api(path: string, init?: RequestInit) {
  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId(), ...init?.headers },
  });
}

interface SyncTask {
  task_id: string;
  type: string;
  channel: string;
  source: string;
  status: string;
  progress: number;
  total: number;
  started_at: string;
  updated_at: string;
  error?: string;
  ack: boolean;
}

interface SyncStatusData {
  tasks: SyncTask[];
  processing: number;
  pending: number;
  errors: number;
}

interface SyncStatusPanelProps {
  isOpen: boolean;
  onClose: () => void;
  onErrorCountChange?: (count: number) => void;
}

export function SyncStatusPanel({ isOpen, onClose, onErrorCountChange }: SyncStatusPanelProps) {
  const [data, setData] = useState<SyncStatusData | null>(null);
  const [tab, setTab] = useState<'processing' | 'pending' | 'errors'>('processing');
  const [clearing, setClearing] = useState(false);

  const load = useCallback(async () => {
    try {
      const res = await api('/sync/status');
      if (res.ok) {
        const d = await res.json();
        setData(d);
        onErrorCountChange?.(d.errors || 0);
      }
    } catch {}
  }, [onErrorCountChange]);

  useEffect(() => {
    if (isOpen) {
      load();
      const interval = setInterval(load, 15000);
      return () => clearInterval(interval);
    }
  }, [isOpen, load]);

  async function clearErrors() {
    setClearing(true);
    try {
      await api('/sync/errors/clear', { method: 'POST' });
      load();
    } finally {
      setClearing(false);
    }
  }

  const tasks = data?.tasks || [];
  const filtered = tasks.filter(t => {
    if (tab === 'processing') return t.status === 'running';
    if (tab === 'pending') return t.status === 'pending';
    if (tab === 'errors') return t.status === 'error' && !t.ack;
    return false;
  });

  const typeIcon = (type: string) => {
    switch (type) {
      case 'import': return '⬇️';
      case 'order_import': return '🛒';
      case 'automation': return '⚙️';
      case 'ai_generation': return '🤖';
      default: return '🔄';
    }
  };

  const formatTime = (iso: string) => {
    if (!iso) return '—';
    try {
      return new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
    } catch { return '—'; }
  };

  return (
    <>
      {/* Overlay */}
      {isOpen && <div onClick={onClose} style={{ position: 'fixed', inset: 0, zIndex: 199 }} />}

      {/* Slide-out drawer */}
      <div style={{
        position: 'fixed', top: 0, right: 0, height: '100vh', width: 400,
        background: 'var(--bg-secondary)', borderLeft: '1px solid var(--border)',
        zIndex: 200, transform: isOpen ? 'translateX(0)' : 'translateX(100%)',
        transition: 'transform 0.25s ease', display: 'flex', flexDirection: 'column',
        boxShadow: '-4px 0 24px rgba(0,0,0,0.4)',
      }}>
        {/* Header */}
        <div style={{ padding: '16px 20px', borderBottom: '1px solid var(--border)', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <div>
            <div style={{ fontWeight: 700, color: 'var(--text-primary)', fontSize: 15 }}>🔄 Sync Status</div>
            <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>Last 24 hours</div>
          </div>
          <button onClick={onClose} style={{ background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-muted)', fontSize: 20, lineHeight: 1, padding: 4 }}>×</button>
        </div>

        {/* Summary pills */}
        <div style={{ display: 'flex', gap: 8, padding: '12px 20px', borderBottom: '1px solid var(--border)' }}>
          <div style={{ flex: 1, background: 'rgba(59,130,246,0.1)', border: '1px solid rgba(59,130,246,0.25)', borderRadius: 8, padding: '8px 12px', textAlign: 'center' }}>
            <div style={{ fontSize: 18, fontWeight: 700, color: '#3b82f6' }}>{data?.processing ?? '—'}</div>
            <div style={{ fontSize: 11, color: '#3b82f6', opacity: 0.8 }}>Processing</div>
          </div>
          <div style={{ flex: 1, background: 'rgba(251,191,36,0.1)', border: '1px solid rgba(251,191,36,0.25)', borderRadius: 8, padding: '8px 12px', textAlign: 'center' }}>
            <div style={{ fontSize: 18, fontWeight: 700, color: '#fbbf24' }}>{data?.pending ?? '—'}</div>
            <div style={{ fontSize: 11, color: '#fbbf24', opacity: 0.8 }}>Pending</div>
          </div>
          <div style={{ flex: 1, background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.25)', borderRadius: 8, padding: '8px 12px', textAlign: 'center' }}>
            <div style={{ fontSize: 18, fontWeight: 700, color: '#ef4444' }}>{data?.errors ?? '—'}</div>
            <div style={{ fontSize: 11, color: '#ef4444', opacity: 0.8 }}>Errors</div>
          </div>
        </div>

        {/* Tabs */}
        <div style={{ display: 'flex', borderBottom: '1px solid var(--border)', padding: '0 12px' }}>
          {(['processing', 'pending', 'errors'] as const).map(t => (
            <button key={t} onClick={() => setTab(t)} style={{
              padding: '10px 14px', background: 'none', border: 'none', cursor: 'pointer',
              borderBottom: tab === t ? '2px solid var(--accent-cyan)' : '2px solid transparent',
              color: tab === t ? 'var(--accent-cyan)' : 'var(--text-muted)',
              fontWeight: tab === t ? 600 : 400, fontSize: 13,
              textTransform: 'capitalize',
            }}>{t}</button>
          ))}
          {tab === 'errors' && (data?.errors ?? 0) > 0 && (
            <button onClick={clearErrors} disabled={clearing} style={{
              marginLeft: 'auto', padding: '6px 12px', marginTop: 6,
              background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)',
              borderRadius: 6, color: '#ef4444', fontSize: 11, cursor: 'pointer',
            }}>
              {clearing ? 'Clearing…' : 'Clear All'}
            </button>
          )}
        </div>

        {/* Task list */}
        <div style={{ flex: 1, overflowY: 'auto', padding: '8px 0' }}>
          {filtered.length === 0 ? (
            <div style={{ textAlign: 'center', padding: 40, color: 'var(--text-muted)', fontSize: 13 }}>
              {tab === 'processing' && 'No tasks currently processing.'}
              {tab === 'pending' && 'No tasks in queue.'}
              {tab === 'errors' && '✅ No unacknowledged errors.'}
            </div>
          ) : (
            filtered.map(task => (
              <div key={task.task_id} style={{
                padding: '12px 20px', borderBottom: '1px solid var(--border)',
                display: 'flex', alignItems: 'flex-start', gap: 12,
              }}>
                <div style={{ fontSize: 18, marginTop: 1 }}>{typeIcon(task.type)}</div>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div style={{ fontWeight: 600, color: 'var(--text-primary)', fontSize: 13 }}>{task.source}</div>
                  <div style={{ color: 'var(--text-muted)', fontSize: 11, marginTop: 2 }}>
                    {task.channel && <span style={{ marginRight: 8 }}>{task.channel}</span>}
                    Started {formatTime(task.started_at)}
                  </div>
                  {task.progress > 0 && task.total > 0 && (
                    <div style={{ marginTop: 8 }}>
                      <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 11, color: 'var(--text-muted)', marginBottom: 3 }}>
                        <span>Progress</span>
                        <span>{task.progress}/{task.total}</span>
                      </div>
                      <div style={{ height: 4, background: 'var(--bg-elevated)', borderRadius: 2, overflow: 'hidden' }}>
                        <div style={{ height: '100%', background: 'var(--primary)', width: `${Math.min(100, (task.progress / task.total) * 100)}%`, borderRadius: 2 }} />
                      </div>
                    </div>
                  )}
                  {task.error && (
                    <div style={{ marginTop: 6, fontSize: 11, color: '#ef4444', background: 'rgba(239,68,68,0.08)', borderRadius: 4, padding: '4px 8px' }}>
                      {task.error}
                    </div>
                  )}
                </div>
                <div style={{
                  flexShrink: 0, padding: '2px 8px', borderRadius: 4, fontSize: 11, fontWeight: 600,
                  background: task.status === 'running' ? 'rgba(59,130,246,0.15)' : task.status === 'error' ? 'rgba(239,68,68,0.15)' : 'rgba(251,191,36,0.15)',
                  color: task.status === 'running' ? '#3b82f6' : task.status === 'error' ? '#ef4444' : '#fbbf24',
                }}>
                  {task.status}
                </div>
              </div>
            ))
          )}
        </div>

        <div style={{ padding: '12px 20px', borderTop: '1px solid var(--border)' }}>
          <button onClick={load} style={{
            width: '100%', padding: '8px', background: 'var(--bg-elevated)', border: '1px solid var(--border)',
            borderRadius: 6, color: 'var(--text-secondary)', cursor: 'pointer', fontSize: 12,
          }}>
            🔄 Refresh
          </button>
        </div>
      </div>
    </>
  );
}

// ── Compact nav icon trigger ──────────────────────────────────────────────────

interface SyncStatusTriggerProps {
  onClick: () => void;
  errorCount: number;
}

export function SyncStatusTrigger({ onClick, errorCount }: SyncStatusTriggerProps) {
  return (
    <button onClick={onClick} style={{
      position: 'relative', background: 'none', border: 'none', cursor: 'pointer',
      padding: '6px 8px', borderRadius: 8, color: 'var(--text-muted)',
      display: 'flex', alignItems: 'center', justifyContent: 'center',
    }}
      title="Sync Status"
    >
      <span style={{ fontSize: 18 }}>🔄</span>
      {errorCount > 0 && (
        <span style={{
          position: 'absolute', top: 2, right: 2, background: '#ef4444', color: 'white',
          borderRadius: '50%', width: 16, height: 16, fontSize: 10, fontWeight: 700,
          display: 'flex', alignItems: 'center', justifyContent: 'center', lineHeight: 1,
        }}>
          {errorCount > 9 ? '9+' : errorCount}
        </span>
      )}
    </button>
  );
}

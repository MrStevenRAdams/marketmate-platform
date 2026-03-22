// ============================================================================
// DEV DB CLEANUP PAGE
// ============================================================================
// One-shot admin utilities for dropping stale Firestore collections that are
// too large to delete via the Firebase console (transaction size limit).
// Accessible via Dev Tools → DB Cleanup (owner role required).
// ============================================================================

import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import api from '../services/api';

interface CleanupTask {
  id: string;
  label: string;
  description: string;
  icon: string;
  endpoint: string;
  danger: boolean;
  confirm: string;
}

const TASKS: CleanupTask[] = [
  {
    id: 'extended_data',
    label: 'Purge legacy extended_data collection',
    description: 'Deletes the top-level extended_data collection that was migrated to product subcollections. Safe to run — the active data lives under products/{id}/extended_data.',
    icon: '🗑️',
    endpoint: '/admin/purge-extended-data',
    danger: false,
    confirm: 'purge extended_data',
  },
];

interface TaskState {
  status: 'idle' | 'confirming' | 'running' | 'done' | 'error';
  confirmInput: string;
  result: string | null;
  error: string | null;
}

export default function DevDbCleanup() {
  const navigate = useNavigate();
  const [tasks, setTasks] = useState<Record<string, TaskState>>(
    Object.fromEntries(TASKS.map(t => [t.id, { status: 'idle', confirmInput: '', result: null, error: null }]))
  );

  function setTask(id: string, patch: Partial<TaskState>) {
    setTasks(prev => ({ ...prev, [id]: { ...prev[id], ...patch } }));
  }

  async function run(task: CleanupTask) {
    const state = tasks[task.id];
    if (state.confirmInput.trim().toLowerCase() !== task.confirm.toLowerCase()) return;

    setTask(task.id, { status: 'running', error: null, result: null });
    try {
      const res = await api.post(task.endpoint);
      const data = res.data;
      setTask(task.id, {
        status: 'done',
        result: data.message || `Deleted ${data.deleted ?? '?'} documents`,
      });
    } catch (err: any) {
      setTask(task.id, {
        status: 'error',
        error: err?.response?.data?.error || err.message || 'Unknown error',
      });
    }
  }

  return (
    <div className="page">
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 28 }}>
        <button className="btn btn-secondary" onClick={() => navigate(-1)} style={{ padding: '8px 14px' }}>← Back</button>
        <div>
          <h1 className="page-title">DB Cleanup</h1>
          <p className="page-subtitle">One-shot utilities for dropping stale Firestore collections too large for the console</p>
        </div>
      </div>

      <div style={{ display: 'flex', flexDirection: 'column', gap: 16, maxWidth: 680 }}>
        {TASKS.map(task => {
          const state = tasks[task.id];
          const confirmMatch = state.confirmInput.trim().toLowerCase() === task.confirm.toLowerCase();

          return (
            <div key={task.id} className="card" style={{
              border: `1px solid ${task.danger ? 'rgba(239,68,68,0.3)' : 'var(--border-color)'}`,
            }}>
              <div style={{ padding: '20px 24px' }}>
                {/* Header */}
                <div style={{ display: 'flex', alignItems: 'flex-start', gap: 14, marginBottom: 12 }}>
                  <span style={{ fontSize: 24, flexShrink: 0 }}>{task.icon}</span>
                  <div style={{ flex: 1 }}>
                    <div style={{ fontWeight: 700, fontSize: 14, marginBottom: 4 }}>{task.label}</div>
                    <div style={{ fontSize: 12, color: 'var(--text-muted)', lineHeight: 1.6 }}>{task.description}</div>
                  </div>
                  {state.status === 'done' && (
                    <span style={{ fontSize: 20 }}>✅</span>
                  )}
                </div>

                {/* Result */}
                {state.status === 'done' && state.result && (
                  <div style={{
                    background: 'rgba(34,197,94,0.08)', border: '1px solid rgba(34,197,94,0.25)',
                    borderRadius: 8, padding: '10px 14px', fontSize: 13, color: 'var(--success)', marginBottom: 12,
                  }}>
                    ✓ {state.result}
                  </div>
                )}

                {state.status === 'error' && state.error && (
                  <div style={{
                    background: 'rgba(239,68,68,0.08)', border: '1px solid rgba(239,68,68,0.25)',
                    borderRadius: 8, padding: '10px 14px', fontSize: 13, color: 'var(--danger)', marginBottom: 12,
                  }}>
                    ✗ {state.error}
                  </div>
                )}

                {/* Confirm + run */}
                {state.status !== 'done' && (
                  <>
                    {state.status === 'idle' && (
                      <button
                        className="btn btn-secondary"
                        style={{ fontSize: 12 }}
                        onClick={() => setTask(task.id, { status: 'confirming' })}
                      >
                        Run
                      </button>
                    )}

                    {(state.status === 'confirming' || state.status === 'error') && (
                      <div style={{ marginTop: 4 }}>
                        <div style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 8 }}>
                          Type <code style={{ color: 'var(--danger)', background: 'var(--bg-tertiary)', padding: '1px 6px', borderRadius: 4 }}>{task.confirm}</code> to confirm
                        </div>
                        <div style={{ display: 'flex', gap: 8 }}>
                          <input
                            className="input"
                            style={{ flex: 1, fontSize: 13, fontFamily: 'monospace' }}
                            placeholder={task.confirm}
                            value={state.confirmInput}
                            onChange={e => setTask(task.id, { confirmInput: e.target.value })}
                            onKeyDown={e => e.key === 'Enter' && confirmMatch && run(task)}
                            autoFocus
                          />
                          <button
                            className="btn btn-primary"
                            style={{
                              fontSize: 12,
                              opacity: confirmMatch ? 1 : 0.4,
                              cursor: confirmMatch ? 'pointer' : 'not-allowed',
                              background: task.danger ? 'var(--danger)' : undefined,
                              borderColor: task.danger ? 'var(--danger)' : undefined,
                            }}
                            disabled={!confirmMatch}
                            onClick={() => run(task)}
                          >
                            Confirm &amp; Run
                          </button>
                          <button
                            className="btn btn-secondary"
                            style={{ fontSize: 12 }}
                            onClick={() => setTask(task.id, { status: 'idle', confirmInput: '', error: null })}
                          >
                            Cancel
                          </button>
                        </div>
                      </div>
                    )}

                    {state.status === 'running' && (
                      <div style={{ display: 'flex', alignItems: 'center', gap: 10, color: 'var(--text-muted)', fontSize: 13 }}>
                        <div className="spinner" style={{ width: 16, height: 16, borderWidth: 2 }} />
                        Deleting in batches…
                      </div>
                    )}
                  </>
                )}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

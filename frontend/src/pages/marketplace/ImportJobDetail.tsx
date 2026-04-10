// ============================================================================
// IMPORT JOB DETAIL PAGE
// ============================================================================
// Location: frontend/src/pages/marketplace/ImportJobDetail.tsx

import { useState, useEffect, useRef, useCallback } from 'react';
import { useParams, useNavigate, useLocation } from 'react-router-dom';
import { importService, ImportJob } from '../../services/marketplace-api';

const adapterEmoji: Record<string, string> = { amazon: '📦', temu: '🛍️', ebay: '🏷️', shopify: '🛒', tesco: '🏪' };

function formatDate(d?: string): string {
  if (!d) return '—';
  return new Date(d).toLocaleDateString('en-GB', { day: '2-digit', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit' });
}

function formatDuration(start?: string, end?: string): string {
  if (!start) return '—';
  const s = new Date(start).getTime();
  const e = end ? new Date(end).getTime() : Date.now();
  const diff = Math.floor((e - s) / 1000);
  if (diff < 60) return `${diff}s`;
  return `${Math.floor(diff / 60)}m ${diff % 60}s`;
}

// ── ErrorLog component ────────────────────────────────────────────────────────

interface ImportErrorEntry {
  external_id?: string;
  error_code?: string;
  message?: string;
  timestamp?: string;
  request_url?: string;
  status_code?: number;
  response_body?: string;
}

function ErrorLog({ errors }: { errors: ImportErrorEntry[] }) {
  const [expanded, setExpanded] = useState<Set<number>>(new Set());

  function toggle(i: number) {
    setExpanded(prev => {
      const next = new Set(prev);
      next.has(i) ? next.delete(i) : next.add(i);
      return next;
    });
  }

  const hasDetail = errors.some(e => e.request_url || e.status_code || e.response_body);

  return (
    <div className="card">
      <div className="card-header" style={{ background: 'rgba(239,68,68,0.08)', borderBottom: '1px solid rgba(239,68,68,0.2)' }}>
        <h3 style={{ fontSize: 15, fontWeight: 700, color: 'var(--danger)', display: 'flex', alignItems: 'center', gap: 8 }}>
          <span>⚠</span>
          <span>Errors</span>
          <span style={{ background: 'var(--danger)', color: '#fff', borderRadius: 10, padding: '1px 8px', fontSize: 12, fontWeight: 700 }}>
            {errors.length}
          </span>
        </h3>
        {hasDetail && (
          <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>Click a row to see full request/response</span>
        )}
      </div>
      <div style={{ padding: 0 }}>
        {errors.map((err, i) => {
          const isOpen = expanded.has(i);
          const hasDiag = !!(err.request_url || err.status_code || err.response_body);
          return (
            <div key={i} style={{ borderBottom: '1px solid var(--border-color)' }}>
              <div
                onClick={() => hasDiag && toggle(i)}
                style={{
                  display: 'grid',
                  gridTemplateColumns: '180px 140px 1fr auto',
                  gap: 16,
                  padding: '12px 20px',
                  alignItems: 'center',
                  cursor: hasDiag ? 'pointer' : 'default',
                  background: isOpen ? 'rgba(239,68,68,0.05)' : 'transparent',
                  transition: 'background 0.15s',
                }}
              >
                <code style={{ fontSize: 12, color: 'var(--accent-cyan)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                  {err.external_id || '—'}
                </code>
                <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                  <span className="badge badge-danger" style={{ fontSize: 11 }}>{err.error_code || 'ERROR'}</span>
                  {err.status_code ? (
                    <span style={{
                      fontSize: 11, fontWeight: 700, fontFamily: 'monospace',
                      color: err.status_code >= 500 ? 'var(--warning)' : 'var(--danger)',
                      background: 'var(--bg-tertiary)', padding: '1px 6px', borderRadius: 4,
                    }}>
                      {err.status_code}
                    </span>
                  ) : null}
                </div>
                <span style={{ fontSize: 13, color: 'var(--text-secondary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                  {err.message || '—'}
                </span>
                {hasDiag ? (
                  <span style={{ fontSize: 16, color: 'var(--text-muted)', transition: 'transform 0.2s', transform: isOpen ? 'rotate(90deg)' : 'rotate(0deg)', display: 'inline-block' }}>›</span>
                ) : <span />}
              </div>

              {isOpen && hasDiag && (
                <div style={{ padding: '0 20px 16px', background: 'rgba(239,68,68,0.03)', borderTop: '1px dashed rgba(239,68,68,0.15)' }}>
                  {err.request_url && (
                    <div style={{ marginTop: 12 }}>
                      <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.06em', marginBottom: 4 }}>
                        Request URL
                      </div>
                      <code style={{ display: 'block', fontSize: 12, color: 'var(--accent-cyan)', background: 'var(--bg-tertiary)', padding: '6px 10px', borderRadius: 6, wordBreak: 'break-all' }}>
                        {err.request_url}
                      </code>
                    </div>
                  )}
                  {err.response_body && (
                    <div style={{ marginTop: 12 }}>
                      <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.06em', marginBottom: 4 }}>
                        Response Body
                      </div>
                      <pre style={{
                        fontSize: 12, color: 'var(--danger)', background: 'var(--bg-tertiary)',
                        padding: '10px 12px', borderRadius: 6, overflowX: 'auto',
                        margin: 0, maxHeight: 300, overflowY: 'auto',
                        fontFamily: 'monospace', whiteSpace: 'pre-wrap', wordBreak: 'break-all',
                      }}>
                        {(() => { try { return JSON.stringify(JSON.parse(err.response_body!), null, 2); } catch { return err.response_body; } })()}
                      </pre>
                    </div>
                  )}
                </div>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}

// ── QueueConsole component ────────────────────────────────────────────────────
// Terminal-style live console. Polls every 2s via the parent and diffs
// counter changes into human-readable log lines.

interface ConsoleLine {
  id: number;
  ts: string;
  type: 'info' | 'ok' | 'warn' | 'error' | 'phase' | 'rate' | 'done';
  text: string;
}

const PHASE_LABELS: Record<string, string> = {
  pending: 'PENDING', running: 'RUNNING', completed: 'COMPLETED',
  failed: 'FAILED', cancelled: 'CANCELLED',
};

function nowTs(): string {
  return new Date().toTimeString().slice(0, 8);
}

function QueueConsole({ job, isRunning }: { job: ImportJob; isRunning: boolean }) {
  const [lines, setLines]   = useState<ConsoleLine[]>([]);
  const [paused, setPaused] = useState(false);
  const [filter, setFilter] = useState<'all' | 'warn' | 'error'>('all');
  const bottomRef           = useRef<HTMLDivElement>(null);
  const lineId              = useRef(0);
  const prevJob             = useRef<ImportJob | null>(null);
  const rateRef             = useRef<{ time: number; processed: number; enriched: number } | null>(null);
  const seeded              = useRef(false);

  const push = useCallback((type: ConsoleLine['type'], text: string) => {
    const line: ConsoleLine = { id: lineId.current++, ts: nowTs(), type, text };
    setLines(prev => {
      const next = [...prev, line];
      return next.length > 500 ? next.slice(next.length - 500) : next;
    });
  }, []);

  // Seed on mount
  useEffect(() => {
    if (seeded.current) return;
    seeded.current = true;
    push('phase', `── JOB ${job.job_id.slice(0, 8)} ·· ${(job.channel || '').toUpperCase()} ${job.job_type || ''} ──`);
    push('info',  `status=${job.status}  total=${job.total_items}  processed=${job.processed_items}  enriched=${job.enriched_items ?? 0}`);
    if (job.status_message) push('info', job.status_message);
    rateRef.current = { time: Date.now(), processed: job.processed_items, enriched: job.enriched_items ?? 0 };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Diff snapshots → lines
  useEffect(() => {
    const prev = prevJob.current;
    if (!prev) { prevJob.current = job; return; }

    if (prev.status !== job.status)
      push('phase', `── STATUS → ${PHASE_LABELS[job.status] ?? job.status.toUpperCase()} ──`);

    if (job.status_message && job.status_message !== prev.status_message)
      push('info', job.status_message);

    const dProcessed = job.processed_items - prev.processed_items;
    if (dProcessed > 0)
      push('ok', `batch  +${dProcessed} processed  → ${job.processed_items}/${job.total_items}  (✓${job.successful_items}  ✗${job.failed_items}  ⟳${job.skipped_items ?? 0})`);

    const enrichedNow  = job.enriched_items ?? 0;
    const enrichedPrev = prev.enriched_items ?? 0;
    const dEnriched    = enrichedNow - enrichedPrev;
    const enrichTotal  = (job as any).enrich_total_items ?? 0;
    if (dEnriched > 0)
      push('ok', `enrich +${dEnriched} enriched   → ${enrichedNow}${enrichTotal ? `/${enrichTotal}` : ''}  (✗${job.enrich_failed_items ?? 0})`);

    const dEnrichFail = (job.enrich_failed_items ?? 0) - (prev.enrich_failed_items ?? 0);
    if (dEnrichFail > 0 && dEnriched === 0)
      push('warn', `enrich +${dEnrichFail} failed     → total_fail=${job.enrich_failed_items}`);

    const dFailed = job.failed_items - prev.failed_items;
    if (dFailed > 0 && dProcessed === 0)
      push('error', `batch  +${dFailed} failed     → total_fail=${job.failed_items}`);

    const now = Date.now();
    const r = rateRef.current;
    if (r && now - r.time >= 10000) {
      const elapsed = (now - r.time) / 1000;
      const rateP = ((job.processed_items - r.processed) / elapsed).toFixed(1);
      const rateE = ((enrichedNow - r.enriched) / elapsed).toFixed(1);
      const totalElapsed = job.started_at ? Math.floor((now - new Date(job.started_at).getTime()) / 1000) : 0;
      push('rate', `throughput  batch=${rateP}/s  enrich=${rateE}/s  elapsed=${totalElapsed}s`);
      rateRef.current = { time: now, processed: job.processed_items, enriched: enrichedNow };
    }

    if (job.status === 'completed' && prev.status !== 'completed')
      push('done', `✓ COMPLETE — ${job.successful_items} imported  ${enrichedNow} enriched  ${job.failed_items + (job.enrich_failed_items ?? 0)} failed`);
    if (job.status === 'failed' && prev.status !== 'failed')
      push('error', `✗ FAILED — check error log below`);

    prevJob.current = job;
  }, [job, push]);

  // Auto-scroll
  useEffect(() => {
    if (!paused) bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [lines, paused]);

  const visible = filter === 'all' ? lines
    : lines.filter(l => filter === 'warn' ? (l.type === 'warn' || l.type === 'error') : l.type === 'error');

  const colour = (type: ConsoleLine['type']) => {
    const map: Record<string, string> = {
      ok: '#4ade80', warn: '#fbbf24', error: '#f87171',
      phase: '#60a5fa', rate: '#a78bfa', done: '#34d399', info: '#94a3b8',
    };
    return map[type] ?? '#94a3b8';
  };

  const prefix = (type: ConsoleLine['type']) => {
    const map: Record<string, string> = {
      ok: '  ✓ ', warn: '  ⚠ ', error: '  ✗ ', rate: '  ~ ', done: '  ★ ', phase: '', info: '    ',
    };
    return map[type] ?? '    ';
  };

  const btnStyle = (active: boolean, activeColor = '#3fb950'): React.CSSProperties => ({
    fontSize: 10, padding: '2px 8px', borderRadius: 4, border: 'none',
    cursor: 'pointer', fontFamily: 'inherit', letterSpacing: '0.06em', fontWeight: 700,
    background: active ? activeColor : '#21262d',
    color: active ? '#0d1117' : '#7d8590',
    transition: 'all 0.15s',
  });

  return (
    <div style={{ background: '#0d1117', borderRadius: 8, border: '1px solid #21262d', overflow: 'hidden', fontFamily: '"JetBrains Mono","Fira Code",Consolas,monospace' }}>
      <style>{`@keyframes cpulse{0%,100%{opacity:1}50%{opacity:.3}}.cline:hover{background:rgba(255,255,255,.03)!important}`}</style>

      {/* Title bar */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '10px 16px', background: '#161b22', borderBottom: '1px solid #21262d' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
          <div style={{ display: 'flex', gap: 6 }}>
            {['#ff5f57','#febc2e','#28c840'].map(c => <div key={c} style={{ width: 12, height: 12, borderRadius: '50%', background: c }} />)}
          </div>
          <span style={{ fontSize: 12, color: '#7d8590', letterSpacing: '0.05em' }}>
            queue-monitor — {job.job_id.slice(0, 12)}
          </span>
          {isRunning && (
            <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5, fontSize: 10, color: '#3fb950', fontWeight: 700, background: 'rgba(63,185,80,.1)', border: '1px solid rgba(63,185,80,.3)', borderRadius: 4, padding: '1px 7px', letterSpacing: '0.08em' }}>
              <span style={{ width: 6, height: 6, borderRadius: '50%', background: '#3fb950', animation: 'cpulse 1.2s ease-in-out infinite', display: 'inline-block' }} />
              LIVE
            </span>
          )}
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          {(['all','warn','error'] as const).map(f => (
            <button key={f} onClick={() => setFilter(f)} style={btnStyle(filter === f, f === 'error' ? '#f87171' : f === 'warn' ? '#fbbf24' : '#3fb950')}>
              {f.toUpperCase()}
            </button>
          ))}
          <div style={{ width: 1, height: 16, background: '#21262d' }} />
          <button onClick={() => setPaused(p => !p)} style={{ ...btnStyle(paused, 'rgba(251,191,36,.15)'), border: '1px solid #30363d', color: paused ? '#fbbf24' : '#7d8590' }}>
            {paused ? '▶ RESUME' : '⏸ PAUSE'}
          </button>
          <button onClick={() => setLines([])} style={{ ...btnStyle(false), border: '1px solid #30363d' }}>
            CLR
          </button>
        </div>
      </div>

      {/* Output */}
      <div style={{ height: 400, overflowY: 'auto', padding: '12px 0' }}>
        {visible.length === 0 && (
          <div style={{ padding: '40px 20px', textAlign: 'center', color: '#484f58', fontSize: 12 }}>
            {filter !== 'all' ? `No ${filter} messages` : 'Waiting for events…'}
          </div>
        )}
        {visible.map(line => (
          <div key={line.id} className="cline" style={{ display: 'flex', padding: line.type === 'phase' ? '10px 16px 6px' : '1px 16px', transition: 'background .1s' }}>
            {line.type === 'phase' ? (
              <span style={{ color: '#60a5fa', fontSize: 11, fontWeight: 700, letterSpacing: '0.08em' }}>{line.text}</span>
            ) : (
              <>
                <span style={{ color: '#484f58', fontSize: 11, minWidth: 68, userSelect: 'none', flexShrink: 0 }}>{line.ts}</span>
                <span style={{ color: colour(line.type), fontSize: 11, minWidth: 28, userSelect: 'none', flexShrink: 0, whiteSpace: 'pre' }}>{prefix(line.type)}</span>
                <span style={{ color: colour(line.type), fontSize: 11, opacity: line.type === 'info' ? 0.7 : 1 }}>{line.text}</span>
              </>
            )}
          </div>
        ))}
        <div ref={bottomRef} />
      </div>

      {/* Status bar */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '6px 16px', background: '#161b22', borderTop: '1px solid #21262d' }}>
        <span style={{ fontSize: 10, color: '#484f58' }}>{lines.length} events · {visible.length} visible</span>
        <div style={{ display: 'flex', gap: 16 }}>
          {[
            { label: 'PROCESSED', val: job.processed_items, total: job.total_items, col: '#4ade80' },
            { label: 'ENRICHED',  val: job.enriched_items ?? 0, total: (job as any).enrich_total_items ?? 0, col: '#a78bfa' },
            { label: 'FAILED',    val: job.failed_items + (job.enrich_failed_items ?? 0), total: 0, col: (job.failed_items + (job.enrich_failed_items ?? 0)) > 0 ? '#f87171' : '#484f58' },
          ].map(s => (
            <span key={s.label} style={{ fontSize: 10 }}>
              <span style={{ color: '#7d8590', letterSpacing: '0.06em' }}>{s.label} </span>
              <span style={{ color: s.col, fontWeight: 700 }}>{s.val}{s.total > 0 ? `/${s.total}` : ''}</span>
            </span>
          ))}
        </div>
      </div>
    </div>
  );
}

// ── Main page ─────────────────────────────────────────────────────────────────

export default function ImportJobDetail() {
  const { jobId } = useParams<{ jobId: string }>();
  const navigate  = useNavigate();
  const location  = useLocation();
  const [job, setJob]               = useState<ImportJob | null>(null);
  const [loading, setLoading]       = useState(true);
  const [error, setError]           = useState<string | null>(null);
  const [showConsole, setShowConsole] = useState(false);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  useEffect(() => {
    if (!jobId) return;
    loadJob();
    pollRef.current = setInterval(loadJob, 2000);
    return () => { if (pollRef.current) clearInterval(pollRef.current); };
  }, [jobId]);

  useEffect(() => {
    if (job && job.status !== 'running' && job.status !== 'pending')
      if (pollRef.current) clearInterval(pollRef.current);
  }, [job?.status]);

  useEffect(() => {
    if (location.hash === '#errors' && job?.error_log?.length) {
      setTimeout(() => document.getElementById('errors-section')?.scrollIntoView({ behavior: 'smooth', block: 'start' }), 100);
    }
  }, [job, location.hash]);

  async function loadJob() {
    try {
      const res = await importService.getJob(jobId!);
      setJob(res.data?.data || res.data);
      setError(null);
    } catch (err: any) {
      setError(err.message || 'Failed to load job');
    } finally {
      setLoading(false);
    }
  }

  if (loading) return <div className="page"><div className="loading-state"><div className="spinner" /><p>Loading job details...</p></div></div>;

  if (error || !job) {
    return (
      <div className="page">
        <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 24 }}>
          <button className="btn btn-secondary" onClick={() => navigate('/marketplace/import')}>← Back</button>
          <h1 className="page-title">Import Job</h1>
        </div>
        <div className="card"><div className="empty-state"><div className="empty-icon">⚠️</div><h3>{error || 'Job not found'}</h3><p>The import job could not be loaded.</p>
          <button className="btn btn-primary" onClick={() => navigate('/marketplace/import')}>Back to Dashboard</button></div></div>
      </div>
    );
  }

  const pct      = job.total_items > 0 ? Math.round((job.processed_items / job.total_items) * 100) : 0;
  const barColor = job.status === 'failed' ? 'var(--danger)' : job.status === 'completed' ? 'var(--success)' : 'var(--primary)';
  const isRunning = job.status === 'running' || job.status === 'pending';

  return (
    <div className="page">
      {/* Header */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 24 }}>
        <button className="btn btn-secondary" onClick={() => navigate('/marketplace/import')} style={{ padding: '8px 14px' }}>← Back</button>
        <div style={{ flex: 1 }}>
          <h1 className="page-title">Import Job <code style={{ color: 'var(--accent-cyan)', fontSize: '0.75em' }}>{job.job_id}</code></h1>
          <p className="page-subtitle">Source: {adapterEmoji[job.channel] || '🌐'} {job.channel} · Type: {job.job_type} · Started {formatDate(job.started_at || job.created_at)}</p>
        </div>
        <div style={{ display: 'flex', gap: 10, alignItems: 'center' }}>
          <button
            onClick={() => setShowConsole(s => !s)}
            style={{
              padding: '8px 16px', borderRadius: 6, border: '1px solid #30363d',
              cursor: 'pointer', fontSize: 12, fontWeight: 700,
              fontFamily: '"JetBrains Mono",Consolas,monospace',
              background: showConsole ? '#0d1117' : 'transparent',
              color: showConsole ? '#3fb950' : 'var(--text-secondary)',
              transition: 'all 0.2s', letterSpacing: '0.04em',
            }}
          >
            {showConsole ? '▼ CONSOLE' : '▶ CONSOLE'}
          </button>
          <span className={`badge ${job.status === 'completed' ? 'badge-success' : isRunning ? 'badge-info' : job.status === 'failed' ? 'badge-danger' : 'badge-warning'}`} style={{ fontSize: 13, padding: '6px 14px' }}>
            {job.status}
          </span>
        </div>
      </div>

      {/* Queue Console */}
      {showConsole && (
        <div style={{ marginBottom: 24 }}>
          <QueueConsole job={job} isRunning={isRunning} />
        </div>
      )}

      {/* Progress */}
      <div className="card" style={{ marginBottom: 24 }}>
        <div style={{ padding: 24 }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 12 }}>
            <span style={{ fontSize: 14, fontWeight: 600 }}>Progress</span>
            <span style={{ fontSize: 14, fontWeight: 700, color: 'var(--primary)' }}>{pct}%</span>
          </div>
          <div style={{ height: 8, background: 'var(--bg-tertiary)', borderRadius: 4, overflow: 'hidden', marginBottom: 24 }}>
            <div style={{ height: '100%', width: `${pct}%`, background: barColor, borderRadius: 4, transition: 'width 0.5s' }} />
          </div>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(5, 1fr)', gap: 20 }}>
            <div><div style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 4, textTransform: 'uppercase' }}>Total</div><div style={{ fontSize: 24, fontWeight: 700 }}>{job.total_items}</div></div>
            <div><div style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 4, textTransform: 'uppercase' }}>Processed</div><div style={{ fontSize: 24, fontWeight: 700 }}>{job.processed_items}</div></div>
            <div><div style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 4, textTransform: 'uppercase' }}>Succeeded</div><div style={{ fontSize: 24, fontWeight: 700, color: 'var(--success)' }}>{job.successful_items}</div></div>
            <div><div style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 4, textTransform: 'uppercase' }}>Failed</div><div style={{ fontSize: 24, fontWeight: 700, color: job.failed_items > 0 ? 'var(--danger)' : 'var(--text-muted)' }}>{job.failed_items}</div></div>
            <div><div style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 4, textTransform: 'uppercase' }}>Duration</div><div style={{ fontSize: 24, fontWeight: 700 }}>{formatDuration(job.started_at || job.created_at, job.completed_at || undefined)}</div></div>
          </div>
        </div>
      </div>

      {/* Timeline */}
      <div className="card" style={{ marginBottom: 24 }}>
        <div className="card-header"><h3 style={{ fontSize: 15, fontWeight: 700 }}>Timeline</h3></div>
        <div style={{ padding: 20, display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 16 }}>
          <div><div style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 4 }}>Created</div><div style={{ fontSize: 13, fontWeight: 600 }}>{formatDate(job.created_at)}</div></div>
          <div><div style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 4 }}>Started</div><div style={{ fontSize: 13, fontWeight: 600 }}>{formatDate(job.started_at)}</div></div>
          <div><div style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 4 }}>Completed</div><div style={{ fontSize: 13, fontWeight: 600 }}>{job.completed_at ? formatDate(job.completed_at) : isRunning ? '⏳ Running...' : '—'}</div></div>
        </div>
      </div>

      {/* Errors */}
      {job.error_log && job.error_log.length > 0 && (
        <div id="errors-section"><ErrorLog errors={job.error_log} /></div>
      )}
    </div>
  );
}

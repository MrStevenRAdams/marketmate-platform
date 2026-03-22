import { useState, useEffect, useCallback, useRef } from 'react';
import { getActiveTenantId } from '../contexts/TenantContext';

const API_BASE = (import.meta as any).env?.VITE_API_URL || 'https://marketmate-api-487246736287.us-central1.run.app/api/v1';

// ── Types ─────────────────────────────────────────────────────────────────────

interface OpsJob {
  job_id: string;
  tenant_id: string;
  tenant_name: string;
  collection: string;
  job_type: string;
  channel?: string;
  account_name?: string;
  description: string;
  status: string;
  status_message?: string;
  is_stuck: boolean;
  total: number;
  processed: number;
  succeeded: number;
  failed: number;
  skipped: number;
  created_at: string;
  started_at?: string;
  updated_at: string;
  completed_at?: string;
  elapsed_secs: number;
  can_cancel: boolean;
  can_retry: boolean;
  can_delete: boolean;
  raw?: Record<string, any>;
}

interface JobGroup {
  type: string;
  label: string;
  icon: string;
  jobs: OpsJob[];
}

interface Summary {
  total: number;
  running: number;
  pending: number;
  stuck: number;
  failed: number;
  done: number;
}

interface OpsData {
  tenants: Array<{ tenant_id: string; name: string }>;
  summary: Summary;
  job_groups: JobGroup[];
  fetched_at: string;
}

// ── Helpers ────────────────────────────────────────────────────────────────────

const STATUS_CONFIG: Record<string, { bg: string; fg: string; dot: string; label: string }> = {
  running:   { bg: '#0ea5e911', fg: '#38bdf8', dot: '#38bdf8', label: 'RUNNING' },
  pending:   { bg: '#f59e0b11', fg: '#fbbf24', dot: '#fbbf24', label: 'PENDING' },
  completed: { bg: '#10b98111', fg: '#34d399', dot: '#34d399', label: 'DONE' },
  failed:    { bg: '#ef444411', fg: '#f87171', dot: '#f87171', label: 'FAILED' },
  cancelled: { bg: '#6b728011', fg: '#9ca3af', dot: '#9ca3af', label: 'CANCELLED' },
};

const CHANNEL_COLOR: Record<string, string> = {
  amazon: '#FF9900', ebay: '#E53238', temu: '#FF6B35', shopify: '#96BF48',
};

function fmtTime(s?: string) {
  if (!s) return '—';
  return new Date(s).toLocaleString('en-GB', {
    day: '2-digit', month: 'short', hour: '2-digit', minute: '2-digit'
  });
}

function fmtElapsed(secs: number): string {
  if (!secs) return '';
  if (secs < 60) return `${Math.round(secs)}s`;
  if (secs < 3600) return `${Math.floor(secs / 60)}m ${Math.round(secs % 60)}s`;
  return `${Math.floor(secs / 3600)}h ${Math.floor((secs % 3600) / 60)}m`;
}

function timeAgo(ts?: string, now = Date.now()): string {
  if (!ts) return '—';
  const s = Math.floor((now - new Date(ts).getTime()) / 1000);
  if (s < 5)   return 'just now';
  if (s < 60)  return `${s}s ago`;
  if (s < 3600) return `${Math.floor(s / 60)}m ago`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`;
  return `${Math.floor(s / 86400)}d ago`;
}

function pct(job: OpsJob): number {
  if (job.total <= 0) return 0;
  const done = Math.max(job.processed, job.succeeded + job.failed + job.skipped);
  return Math.min(100, Math.round(done / job.total * 100));
}

// ── StatusPill ─────────────────────────────────────────────────────────────────

function StatusPill({ status, stuck }: { status: string; stuck?: boolean }) {
  const cfg = STATUS_CONFIG[status] || STATUS_CONFIG.pending;
  const live = status === 'running' || status === 'pending';
  return (
    <span style={{
      display: 'inline-flex', alignItems: 'center', gap: 5,
      padding: '3px 9px', borderRadius: 20, fontSize: 10, fontWeight: 800,
      letterSpacing: '0.5px', background: stuck ? '#f8717122' : cfg.bg,
      color: stuck ? '#f87171' : cfg.fg, whiteSpace: 'nowrap', fontFamily: 'monospace',
    }}>
      <span style={{
        width: 6, height: 6, borderRadius: '50%',
        background: stuck ? '#f87171' : cfg.dot,
        boxShadow: live && !stuck ? `0 0 5px ${cfg.dot}` : undefined,
        animation: live && !stuck ? 'pulse-dot 1.5s ease-in-out infinite' : undefined,
      }} />
      {stuck ? '⚠ STUCK' : cfg.label}
    </span>
  );
}

// ── Progress Bar ───────────────────────────────────────────────────────────────

function ProgressBar({ job, color }: { job: OpsJob; color: string }) {
  const p = pct(job);
  const active = job.status === 'running' || job.status === 'pending';
  const stuck = job.is_stuck;
  const barColor = stuck ? '#f87171' : job.status === 'completed' ? '#34d399' : color;

  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginTop: 8 }}>
      <div style={{ flex: 1, height: 4, background: 'var(--bg-tertiary,#1a1a2e)', borderRadius: 2, overflow: 'hidden' }}>
        {job.total === 0 && active ? (
          <div style={{
            height: '100%', width: '30%', background: barColor,
            animation: 'sweep 1.8s ease-in-out infinite alternate',
            opacity: 0.7,
          }} />
        ) : (
          <div style={{ height: '100%', width: `${p}%`, background: barColor, borderRadius: 2, transition: 'width 0.4s ease' }} />
        )}
      </div>
      <span style={{ fontSize: 11, color: 'var(--text-muted)', fontVariantNumeric: 'tabular-nums', minWidth: 80, textAlign: 'right' }}>
        {job.total > 0
          ? `${Math.max(job.processed, job.succeeded + job.failed).toLocaleString()} / ${job.total.toLocaleString()}`
          : active ? '—' : ''}
        {job.total > 0 && <span style={{ color: 'var(--text-muted)', marginLeft: 4 }}>{p}%</span>}
      </span>
    </div>
  );
}

// ── Job Detail Drawer ──────────────────────────────────────────────────────────

function JobDrawer({ job, onClose, onAction }: {
  job: OpsJob;
  onClose: () => void;
  onAction: (job: OpsJob, action: string) => Promise<void>;
}) {
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const [rawDetail, setRawDetail]         = useState<Record<string, any> | null>(null);
  const [loadingRaw, setLoadingRaw]       = useState(false);

  const cfg = STATUS_CONFIG[job.status] || STATUS_CONFIG.pending;

  const loadRaw = async () => {
    setLoadingRaw(true);
    try {
      const r = await fetch(
        `${API_BASE}/admin/ops/jobs/${job.tenant_id}/${job.collection}/${job.job_id}`,
      );
      if (r.ok) setRawDetail((await r.json()).data);
    } finally {
      setLoadingRaw(false);
    }
  };

  useEffect(() => { loadRaw(); }, []);

  const act = async (action: string) => {
    setActionLoading(action);
    try { await onAction(job, action); } finally { setActionLoading(null); }
  };

  return (
    <div style={{
      position: 'fixed', inset: 0, zIndex: 1000,
      display: 'flex', justifyContent: 'flex-end',
    }}>
      {/* Backdrop */}
      <div onClick={onClose} style={{ position: 'absolute', inset: 0, background: 'rgba(0,0,0,0.5)' }} />

      {/* Drawer */}
      <div style={{
        position: 'relative', width: 560, height: '100%', overflowY: 'auto',
        background: 'var(--bg-primary,#0f0f1a)', borderLeft: '1px solid var(--border,#2a2a3e)',
        padding: 24, display: 'flex', flexDirection: 'column', gap: 20,
        boxShadow: '-8px 0 32px rgba(0,0,0,0.4)',
      }}>
        {/* Header */}
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
          <div>
            <div style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 4, fontFamily: 'monospace' }}>
              {job.collection} / {job.job_id.slice(0, 24)}…
            </div>
            <div style={{ fontSize: 17, fontWeight: 700, color: 'var(--text-primary)' }}>{job.description}</div>
            <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 4 }}>
              👤 {job.tenant_name}
            </div>
          </div>
          <button onClick={onClose} style={{ background: 'none', border: 'none', color: 'var(--text-muted)', cursor: 'pointer', fontSize: 20, padding: 4 }}>✕</button>
        </div>

        {/* Status + timing */}
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
          <StatusPill status={job.status} stuck={job.is_stuck} />
          {job.elapsed_secs > 0 && (
            <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>⏱ {fmtElapsed(job.elapsed_secs)}</span>
          )}
          {job.status_message && (
            <span style={{ fontSize: 11, color: cfg.fg, fontStyle: 'italic' }}>{job.status_message}</span>
          )}
        </div>

        {/* Progress */}
        {job.total > 0 && (
          <div style={{ background: 'var(--bg-secondary,#13131f)', borderRadius: 10, padding: 14 }}>
            <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', marginBottom: 10, textTransform: 'uppercase', letterSpacing: '0.5px' }}>Progress</div>
            <ProgressBar job={job} color={CHANNEL_COLOR[job.channel||''] || 'var(--primary,#6366f1)'} />
            <div style={{ display: 'flex', gap: 16, marginTop: 12, flexWrap: 'wrap' }}>
              {[
                ['✅ Done', job.succeeded, '#34d399'],
                ['❌ Failed', job.failed, '#f87171'],
                ['⏭ Skipped', job.skipped, '#9ca3af'],
                ['📦 Total', job.total, 'var(--text-primary)'],
              ].map(([label, val, color]) => val > 0 && (
                <div key={label as string} style={{ textAlign: 'center' }}>
                  <div style={{ fontSize: 18, fontWeight: 800, color: color as string, fontVariantNumeric: 'tabular-nums' }}>{(val as number).toLocaleString()}</div>
                  <div style={{ fontSize: 10, color: 'var(--text-muted)', marginTop: 2 }}>{label}</div>
                </div>
              ))}
            </div>
          </div>
        )}

        {/* Timing */}
        <div style={{ background: 'var(--bg-secondary,#13131f)', borderRadius: 10, padding: 14 }}>
          <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', marginBottom: 10, textTransform: 'uppercase', letterSpacing: '0.5px' }}>Timeline</div>
          {[
            ['Created', job.created_at],
            ['Started', job.started_at],
            ['Last update', job.updated_at],
            ['Completed', job.completed_at],
          ].map(([label, val]) => val && (
            <div key={label} style={{ display: 'flex', justifyContent: 'space-between', fontSize: 12, padding: '4px 0', borderBottom: '1px solid var(--border,#2a2a3e)' }}>
              <span style={{ color: 'var(--text-muted)' }}>{label}</span>
              <span style={{ color: 'var(--text-primary)', fontVariantNumeric: 'tabular-nums' }}>{fmtTime(val as string)}</span>
            </div>
          ))}
        </div>

        {/* Actions */}
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
          {job.can_cancel && (
            <button onClick={() => act('cancel')} disabled={actionLoading === 'cancel'}
              style={{ padding: '8px 16px', borderRadius: 8, fontSize: 12, fontWeight: 700, cursor: 'pointer', border: '1px solid #f8717155', background: '#f8717111', color: '#f87171' }}>
              {actionLoading === 'cancel' ? '…' : '⏹ Cancel Job'}
            </button>
          )}
          {job.can_delete && (
            <button onClick={() => act('delete')} disabled={actionLoading === 'delete'}
              style={{ padding: '8px 16px', borderRadius: 8, fontSize: 12, fontWeight: 700, cursor: 'pointer', border: '1px solid var(--border,#2a2a3e)', background: 'transparent', color: 'var(--text-muted)' }}>
              {actionLoading === 'delete' ? '…' : '🗑 Delete Record'}
            </button>
          )}
        </div>

        {/* Raw JSON */}
        <div>
          <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', marginBottom: 8, textTransform: 'uppercase', letterSpacing: '0.5px' }}>Raw Document</div>
          {loadingRaw ? (
            <div style={{ color: 'var(--text-muted)', fontSize: 12 }}>Loading…</div>
          ) : (
            <pre style={{
              fontSize: 10, lineHeight: 1.6, background: 'var(--bg-secondary,#13131f)',
              borderRadius: 8, padding: 12, overflowX: 'auto', color: 'var(--text-secondary)',
              maxHeight: 400, overflowY: 'auto',
              border: '1px solid var(--border,#2a2a3e)',
            }}>
              {JSON.stringify(rawDetail || job.raw || {}, null, 2)}
            </pre>
          )}
        </div>
      </div>
    </div>
  );
}

// ── Job Card ───────────────────────────────────────────────────────────────────

function JobCard({ job, now, onSelect, onAction }: {
  job: OpsJob;
  now: number;
  onSelect: (j: OpsJob) => void;
  onAction: (j: OpsJob, action: string) => Promise<void>;
}) {
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const active   = job.status === 'running' || job.status === 'pending';
  const stuck    = job.is_stuck;
  const color    = CHANNEL_COLOR[job.channel||''] || '#6366f1';
  const barColor = stuck ? '#f87171' : job.status === 'completed' ? '#34d399' : color;

  const act = async (e: React.MouseEvent, action: string) => {
    e.stopPropagation();
    const labels: Record<string, string> = {
      cancel: `Cancel this job?\n\n${job.description} — ${job.tenant_name}`,
      delete: `Permanently delete this job record?\n\n${job.description}\nThis cannot be undone.`,
    };
    if (!confirm(labels[action] || `${action}?`)) return;
    setActionLoading(action);
    try { await onAction(job, action); } finally { setActionLoading(null); }
  };

  return (
    <div onClick={() => onSelect(job)} style={{
      padding: '12px 14px', borderRadius: 10,
      background: 'var(--bg-secondary,#13131f)',
      border: `1px solid ${stuck ? '#f8717144' : 'var(--border,#2a2a3e)'}`,
      borderLeft: `3px solid ${barColor}`,
      cursor: 'pointer', transition: 'border-color 0.15s',
    }}
    onMouseEnter={e => (e.currentTarget.style.borderColor = color)}
    onMouseLeave={e => (e.currentTarget.style.borderColor = stuck ? '#f8717144' : 'var(--border,#2a2a3e)')}
    >
      <div style={{ display: 'flex', gap: 10, alignItems: 'flex-start' }}>
        {/* Left: info */}
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ display: 'flex', gap: 8, alignItems: 'center', marginBottom: 4, flexWrap: 'wrap' }}>
            <StatusPill status={job.status} stuck={stuck} />
            <span style={{ fontSize: 13, fontWeight: 700, color: 'var(--text-primary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
              {job.description}
            </span>
          </div>
          <div style={{ fontSize: 11, color: 'var(--text-muted)', display: 'flex', gap: 10, flexWrap: 'wrap' }}>
            <span>👤 {job.tenant_name}</span>
            {job.channel && <span style={{ color: CHANNEL_COLOR[job.channel] || 'inherit' }}>● {job.channel}</span>}
            <span>{active ? `updated ${timeAgo(job.updated_at, now)}` : fmtTime(job.started_at || job.created_at)}</span>
            {job.elapsed_secs > 0 && <span>⏱ {fmtElapsed(job.elapsed_secs)}</span>}
          </div>
          {job.status_message && active && (
            <div style={{ fontSize: 11, color: stuck ? '#f87171' : '#38bdf8', marginTop: 4, fontStyle: 'italic',
              overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
              {job.status_message}
            </div>
          )}
          {job.total > 0 && <ProgressBar job={job} color={color} />}
        </div>

        {/* Right: actions */}
        <div style={{ display: 'flex', gap: 5, flexShrink: 0 }} onClick={e => e.stopPropagation()}>
          {job.can_cancel && (
            <button onClick={e => act(e, 'cancel')} disabled={actionLoading === 'cancel'}
              style={{ padding: '4px 10px', fontSize: 11, fontWeight: 700, borderRadius: 6, cursor: 'pointer', border: '1px solid #f8717155', background: '#f8717111', color: '#f87171' }}>
              {actionLoading === 'cancel' ? '…' : '⏹'}
            </button>
          )}
          {job.can_delete && (
            <button onClick={e => act(e, 'delete')} disabled={actionLoading === 'delete'}
              style={{ padding: '4px 10px', fontSize: 11, borderRadius: 6, cursor: 'pointer', border: '1px solid var(--border,#2a2a3e)', background: 'transparent', color: 'var(--text-muted)' }}>
              {actionLoading === 'delete' ? '…' : '🗑'}
            </button>
          )}
        </div>
      </div>
    </div>
  );
}

// ── Main OpsConsole ────────────────────────────────────────────────────────────

export default function OpsConsole() {
  const [data, setData]           = useState<OpsData | null>(null);
  const [loading, setLoading]     = useState(true);
  const [error, setError]         = useState('');
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [filterTenant, setFilterTenant] = useState('ALL');
  const [filterStatus, setFilterStatus] = useState('all');
  const [filterType, setFilterType]     = useState('all');
  const [selectedJob, setSelectedJob]   = useState<OpsJob | null>(null);
  const [now, setNow]             = useState(Date.now());
  const pollRef                   = useRef<any>(null);

  // Live clock for elapsed timers
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, []);

  const fetchData = useCallback(async () => {
    try {
      const params = new URLSearchParams();
      if (filterTenant !== 'ALL') params.set('tenant_id', filterTenant);
      if (filterStatus !== 'all') params.set('status', filterStatus);

      const r = await fetch(`${API_BASE}/admin/ops/jobs?${params}`);
      if (!r.ok) throw new Error(`HTTP ${r.status}`);
      const d: OpsData = await r.json();
      setData(d);
      setError('');
    } catch (e: any) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }, [filterTenant, filterStatus]);

  useEffect(() => {
    setLoading(true);
    fetchData();
  }, [fetchData]);

  useEffect(() => {
    clearInterval(pollRef.current);
    if (autoRefresh) {
      pollRef.current = setInterval(fetchData, 5000);
    }
    return () => clearInterval(pollRef.current);
  }, [autoRefresh, fetchData]);

  const handleAction = async (job: OpsJob, action: string) => {
    if (action === 'cancel') {
      await fetch(`${API_BASE}/admin/ops/jobs/${job.tenant_id}/${job.collection}/${job.job_id}/cancel`, { method: 'POST' });
    } else if (action === 'delete') {
      await fetch(`${API_BASE}/admin/ops/jobs/${job.tenant_id}/${job.collection}/${job.job_id}`, { method: 'DELETE' });
      if (selectedJob?.job_id === job.job_id) setSelectedJob(null);
    }
    await fetchData();
  };

  // Filter jobs client-side by type
  const groups = (data?.job_groups || [])
    .filter(g => filterType === 'all' || g.type === filterType)
    .filter(g => g.jobs.length > 0);

  const sum = data?.summary;
  const tenants = data?.tenants || [];
  const hasActive = (sum?.running || 0) + (sum?.pending || 0) > 0;

  return (
    <div style={{ padding: 24, maxWidth: 1300, margin: '0 auto', fontFamily: 'var(--font-body)' }}>
      <style>{`
        @keyframes pulse-dot { 0%,100%{opacity:1} 50%{opacity:0.3} }
        @keyframes sweep { 0%{margin-left:0;width:20%} 100%{margin-left:70%;width:30%} }
      `}</style>

      {/* ── Header ── */}
      <div style={{ marginBottom: 24 }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', flexWrap: 'wrap', gap: 12 }}>
          <div>
            <h1 style={{ fontSize: 22, fontWeight: 800, color: 'var(--text-primary)', margin: 0, letterSpacing: '-0.5px' }}>
              Operations Console
            </h1>
            <p style={{ fontSize: 12, color: 'var(--text-muted)', margin: '4px 0 0' }}>
              Every job, every tenant — live view
              {data?.fetched_at && ` · fetched ${timeAgo(data.fetched_at, now)}`}
            </p>
          </div>
          <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
            <button onClick={fetchData} style={{
              padding: '7px 14px', fontSize: 12, fontWeight: 600, borderRadius: 8,
              border: '1px solid var(--border)', background: 'var(--bg-secondary)',
              color: 'var(--text-secondary)', cursor: 'pointer',
            }}>
              {loading ? '…' : '↺ Refresh'}
            </button>
            <button onClick={() => setAutoRefresh(a => !a)} style={{
              padding: '7px 14px', fontSize: 12, fontWeight: 600, borderRadius: 8,
              border: '1px solid var(--border)', cursor: 'pointer',
              background: autoRefresh ? '#10b98122' : 'var(--bg-secondary)',
              color: autoRefresh ? '#34d399' : 'var(--text-muted)',
            }}>
              {autoRefresh ? '🟢 Live' : '⏸ Paused'}
            </button>
          </div>
        </div>
      </div>

      {/* ── Summary Bar ── */}
      {sum && (
        <div style={{ display: 'flex', gap: 10, marginBottom: 20, flexWrap: 'wrap' }}>
          {[
            { label: 'Running', val: sum.running, color: '#38bdf8', icon: '⚙️' },
            { label: 'Pending', val: sum.pending, color: '#fbbf24', icon: '⏳' },
            { label: 'Stuck',   val: sum.stuck,   color: '#f87171', icon: '⚠️' },
            { label: 'Failed',  val: sum.failed,  color: '#f87171', icon: '❌' },
            { label: 'Done',    val: sum.done,    color: '#34d399', icon: '✅' },
            { label: 'Total',   val: sum.total,   color: 'var(--text-muted)', icon: '📋' },
          ].map(({ label, val, color, icon }) => (
            <div key={label} style={{
              padding: '8px 16px', borderRadius: 10,
              background: 'var(--bg-secondary)', border: '1px solid var(--border)',
              minWidth: 90, textAlign: 'center',
              borderTop: val > 0 && label !== 'Done' && label !== 'Total' ? `2px solid ${color}` : '2px solid transparent',
            }}>
              <div style={{ fontSize: 20, fontWeight: 800, color: val > 0 ? color : 'var(--text-muted)', fontVariantNumeric: 'tabular-nums' }}>
                {val}
              </div>
              <div style={{ fontSize: 10, color: 'var(--text-muted)', marginTop: 2 }}>{icon} {label}</div>
            </div>
          ))}
          {hasActive && (
            <div style={{
              marginLeft: 'auto', padding: '8px 16px', borderRadius: 10, alignSelf: 'center',
              background: '#38bdf811', border: '1px solid #38bdf833', color: '#38bdf8',
              fontSize: 12, fontWeight: 700, display: 'flex', alignItems: 'center', gap: 6,
            }}>
              <span style={{ width: 8, height: 8, borderRadius: '50%', background: '#38bdf8', animation: 'pulse-dot 1s infinite', display: 'inline-block' }} />
              System active
            </div>
          )}
        </div>
      )}

      {error && (
        <div style={{ padding: 14, background: '#f8717111', border: '1px solid #f8717155', borderRadius: 10, color: '#f87171', fontSize: 13, marginBottom: 20 }}>
          ❌ Failed to load: {error}
        </div>
      )}

      {/* ── Filters ── */}
      <div style={{ display: 'flex', gap: 10, marginBottom: 20, flexWrap: 'wrap', alignItems: 'flex-end' }}>
        {/* Tenant filter */}
        <div>
          <div style={{ fontSize: 10, fontWeight: 700, color: 'var(--text-muted)', marginBottom: 4, textTransform: 'uppercase', letterSpacing: '0.5px' }}>Client</div>
          <select value={filterTenant} onChange={e => setFilterTenant(e.target.value)}
            style={{ padding: '6px 10px', borderRadius: 8, border: '1px solid var(--border)', background: 'var(--bg-secondary)', color: 'var(--text-primary)', fontSize: 12, minWidth: 180 }}>
            <option value="ALL">All Clients ({tenants.length})</option>
            {tenants.map(t => <option key={t.tenant_id} value={t.tenant_id}>{t.name || t.tenant_id}</option>)}
          </select>
        </div>

        {/* Status filter */}
        <div>
          <div style={{ fontSize: 10, fontWeight: 700, color: 'var(--text-muted)', marginBottom: 4, textTransform: 'uppercase', letterSpacing: '0.5px' }}>Status</div>
          <div style={{ display: 'flex', gap: 4 }}>
            {(['all', 'active', 'failed'] as const).map(s => (
              <button key={s} onClick={() => setFilterStatus(s)} style={{
                padding: '6px 12px', fontSize: 12, fontWeight: 600, borderRadius: 8, cursor: 'pointer',
                border: '1px solid var(--border)',
                background: filterStatus === s ? 'var(--primary,#6366f1)' : 'var(--bg-secondary)',
                color: filterStatus === s ? '#fff' : 'var(--text-muted)',
              }}>
                {s === 'all' ? 'All' : s === 'active' ? '⚙️ Active' : '❌ Failed'}
              </button>
            ))}
          </div>
        </div>

        {/* Type filter */}
        <div>
          <div style={{ fontSize: 10, fontWeight: 700, color: 'var(--text-muted)', marginBottom: 4, textTransform: 'uppercase', letterSpacing: '0.5px' }}>Job Type</div>
          <select value={filterType} onChange={e => setFilterType(e.target.value)}
            style={{ padding: '6px 10px', borderRadius: 8, border: '1px solid var(--border)', background: 'var(--bg-secondary)', color: 'var(--text-primary)', fontSize: 12 }}>
            <option value="all">All types</option>
            <option value="import">Product Imports</option>
            <option value="import_csv">CSV Imports</option>
            <option value="ebay_enrich">eBay Enrichment</option>
            <option value="ai_gen">AI Generation</option>
            <option value="schema">Schema Sync</option>
            <option value="background">Background Jobs</option>
          </select>
        </div>
      </div>

      {/* ── Loading ── */}
      {loading && !data && (
        <div style={{ padding: 60, textAlign: 'center', color: 'var(--text-muted)' }}>
          <div style={{ fontSize: 32, marginBottom: 12, animation: 'pulse-dot 1s infinite' }}>⚙️</div>
          <div style={{ fontSize: 14 }}>Loading all job data…</div>
        </div>
      )}

      {/* ── Job Groups ── */}
      {groups.length === 0 && !loading && (
        <div style={{ padding: 60, textAlign: 'center', background: 'var(--bg-secondary)', borderRadius: 12, color: 'var(--text-muted)' }}>
          <div style={{ fontSize: 36, marginBottom: 12 }}>📭</div>
          <div style={{ fontSize: 15, fontWeight: 600 }}>No jobs match the current filters</div>
        </div>
      )}

      <div style={{ display: 'flex', flexDirection: 'column', gap: 24 }}>
        {groups.map(group => (
          <div key={group.type}>
            {/* Group header */}
            <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 10 }}>
              <span style={{ fontSize: 16 }}>{group.icon}</span>
              <span style={{ fontSize: 14, fontWeight: 700, color: 'var(--text-primary)' }}>{group.label}</span>
              <span style={{ fontSize: 12, color: 'var(--text-muted)', background: 'var(--bg-secondary)', padding: '2px 8px', borderRadius: 10, border: '1px solid var(--border)' }}>
                {group.jobs.length}
              </span>
              <div style={{ flex: 1, height: 1, background: 'var(--border)' }} />
            </div>

            <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
              {group.jobs.map(job => (
                <JobCard key={`${job.tenant_id}-${job.job_id}`}
                  job={job} now={now}
                  onSelect={setSelectedJob}
                  onAction={handleAction}
                />
              ))}
            </div>
          </div>
        ))}
      </div>

      {/* ── Job Detail Drawer ── */}
      {selectedJob && (
        <JobDrawer
          job={selectedJob}
          onClose={() => setSelectedJob(null)}
          onAction={handleAction}
        />
      )}
    </div>
  );
}

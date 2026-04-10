import { useState, useEffect, useCallback, useRef } from 'react';
import { getActiveTenantId } from '../../contexts/TenantContext';
import { auth } from '../../contexts/AuthContext';

const API_BASE = (import.meta as any).env?.VITE_API_URL || 'http://localhost:8080/api/v1';

const CHANNEL_COLORS: Record<string, string> = {
  amazon: '#FF9900', ebay: '#E53238', temu: '#FF6B35', shopify: '#96BF48', tesco: '#E31B23',
};
const CHANNEL_EMOJI: Record<string, string> = {
  amazon: '📦', ebay: '🏷️', temu: '🛍️', shopify: '🛒', tesco: '🏪',
};
const JOB_TYPES = [
  { id: 'amazon-schema', name: 'Amazon Schemas', endpoint: 'amazon/schemas/jobs', color: '#FF9900' },
  { id: 'ebay-schema',   name: 'eBay Schemas',   endpoint: 'ebay/schemas/jobs',   color: '#E53238' },
  { id: 'temu-schema',   name: 'Temu Schemas',    endpoint: 'temu/schemas/jobs',   color: '#FF6B35' },
  { id: 'ai-generation', name: 'AI Generation',   endpoint: 'ai/generate/jobs',    color: '#8B5CF6' },
];

function fmtDate(d?: string) {
  if (!d) return '—';
  return new Date(d).toLocaleDateString('en-GB', { day: '2-digit', month: 'short', hour: '2-digit', minute: '2-digit' });
}

function elapsed(startedAt?: string, now = Date.now()): string {
  if (!startedAt) return '';
  const s = Math.floor((now - new Date(startedAt).getTime()) / 1000);
  if (s < 60) return `${s}s`;
  if (s < 3600) return `${Math.floor(s / 60)}m ${s % 60}s`;
  const h = Math.floor(s / 3600);
  return `${h}h ${Math.floor((s % 3600) / 60)}m`;
}

function timeAgo(ts?: string) {
  if (!ts) return '—';
  const s = Math.floor((Date.now() - new Date(ts).getTime()) / 1000);
  if (s < 60) return `${s}s ago`;
  if (s < 3600) return `${Math.floor(s / 60)}m ago`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`;
  return `${Math.floor(s / 86400)}d ago`;
}

function StatusPill({ status, stuck }: { status: string; stuck?: boolean }) {
  const colors: Record<string, [string, string]> = {
    running:   ['#1d4ed833', '#60a5fa'],
    pending:   ['#92400e33', '#fbbf24'],
    completed: ['#05703033', '#34d399'],
    failed:    ['#9f121233', '#f87171'],
    cancelled: ['#37415133', '#9ca3af'],
  };
  const [bg, fg] = colors[status] || colors.pending;
  const isLive = status === 'running' || status === 'pending';
  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5, padding: '3px 10px',
      borderRadius: 20, fontSize: 11, fontWeight: 700, background: bg, color: fg, whiteSpace: 'nowrap' }}>
      <span style={{ width: 6, height: 6, borderRadius: '50%', background: fg,
        boxShadow: isLive ? `0 0 6px ${fg}` : undefined }} />
      {stuck ? '⚠️ STUCK' : status.toUpperCase()}
    </span>
  );
}

// ============================================================================
// CUSTOMER JOBS TAB
// ============================================================================
function CustomerJobsTab() {
  const [tenants, setTenants]           = useState<any[]>([]);
  const [selectedTenant, setSelectedTenant] = useState<string>('ALL');
  const [allJobs, setAllJobs]           = useState<any[]>([]);
  const [loadingTenants, setLoadingTenants] = useState(true);
  const [loadingJobs, setLoadingJobs]   = useState(false);
  const [actionLoading, setActionLoading] = useState<Record<string, boolean>>({});
  const [filterStatus, setFilterStatus] = useState<string>('active');
  const [now, setNow]                   = useState(Date.now());
  const pollRef                         = useRef<any>(null);

  // 1-second tick so elapsed timers animate
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, []);

  // Load tenants once
  useEffect(() => {
    const base = API_BASE.replace('/api/v1', '');
    fetch(`${base}/api/v1/tenants`)
      .then(r => r.json())
      .then(d => setTenants(Array.isArray(d.data) ? d.data : []))
      .catch(() => setTenants([]))
      .finally(() => setLoadingTenants(false));
  }, []);

  const fetchJobs = useCallback(async () => {
    if (tenants.length === 0) return;
    setLoadingJobs(true);
    try {
      // Use the server-side aggregation endpoint which queries all tenants in one
      // Firestore call — avoids firing one request per tenant every 5s which was
      // saturating Cloud Run and causing 403 storms.
      let token = '';
      try { if (auth.currentUser) token = await auth.currentUser.getIdToken(); } catch { /* ignore */ }

      const tenantId = getActiveTenantId() || '';
      const url = selectedTenant === 'ALL'
        ? `${API_BASE}/admin/ops/jobs?limit=200&status=all`
        : `${API_BASE}/admin/ops/jobs?limit=200&status=all&tenant_id=${selectedTenant}`;

      const res = await fetch(url, {
        headers: {
          'X-Tenant-Id': tenantId,
          ...(token ? { Authorization: `Bearer ${token}` } : {}),
        },
      });

      if (!res.ok) {
        // Fallback: if admin endpoint unavailable, only fetch own tenant's jobs
        const fallback = await fetch(`${API_BASE}/marketplace/import/jobs?page_size=100`, {
          headers: {
            'X-Tenant-Id': tenantId,
            ...(token ? { Authorization: `Bearer ${token}` } : {}),
          },
        }).then(r => r.json()).catch(() => ({ data: [] }));
        const flat = (fallback.data || []).map((j: any) => ({
          ...j,
          _tenantId: tenantId,
          _tenantName: tenants.find(t => t.tenant_id === tenantId)?.name || tenantId,
        }));
        setAllJobs(flat);
        return;
      }

      const data = await res.json();
      // ops/jobs returns { job_groups: [{type, label, jobs: [...]}, ...], tenants: [...] }
      // Each job already has tenant_id and tenant_name set by the backend.
      const jobGroups: any[] = data.job_groups || [];
      const flat: any[] = [];
      for (const group of jobGroups) {
        for (const j of (group.jobs || [])) {
          flat.push({
            ...j,
            _tenantId: j.tenant_id || '',
            _tenantName: j.tenant_name || j.tenant_id || '',
          });
        }
      }

      flat.sort((a, b) => {
        const activeA = a.status === 'running' || a.status === 'pending';
        const activeB = b.status === 'running' || b.status === 'pending';
        if (activeA !== activeB) return activeA ? -1 : 1;
        return new Date(b.started_at || b.created_at).getTime() -
               new Date(a.started_at || a.created_at).getTime();
      });
      setAllJobs(flat);
    } finally {
      setLoadingJobs(false);
    }
  }, [selectedTenant, tenants]);

  useEffect(() => {
    if (tenants.length === 0) return;
    fetchJobs();
    clearInterval(pollRef.current);
    pollRef.current = setInterval(fetchJobs, 5000);
    return () => clearInterval(pollRef.current);
  }, [fetchJobs, tenants]);

  const doAction = async (job: any, action: 'cancel' | 'delete' | 'resume') => {
    const key = `${job.job_id}-${action}`;
    setActionLoading(l => ({ ...l, [key]: true }));
    try {
      const label = `${job._tenantName} · ${job.account_name || job.channel_account_id?.slice(-12) || job.channel}`;
      if (action === 'cancel') {
        if (!confirm(`Cancel this import?\n\n${label}`)) return;
        const res = await fetch(`${API_BASE}/marketplace/import/jobs/${job.job_id}/cancel`, {
          method: 'POST', headers: { 'X-Tenant-Id': job._tenantId },
        });
        if (!res.ok) { alert('Cancel failed: ' + (await res.text())); return; }
      } else if (action === 'delete') {
        if (!confirm(`Permanently delete this job record?\n\n${label}\nThis cannot be undone.`)) return;
        const res = await fetch(`${API_BASE}/marketplace/import/jobs/${job.job_id}`, {
          method: 'DELETE', headers: { 'X-Tenant-Id': job._tenantId },
        });
        if (!res.ok) { alert('Delete failed: ' + (await res.text())); return; }
      } else if (action === 'resume') {
        if (!confirm(`Resume enrichment for all unenriched products?\n\nTenant: ${job._tenantName}`)) return;
        const res = await fetch(`${API_BASE}/marketplace/listings/bulk/enrich`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': job._tenantId },
          body: JSON.stringify({ mode: 'all_unenriched' }),
        });
        const d = await res.json();
        alert(res.ok ? `Queued ${d.queued || 0} products for enrichment` : `Failed: ${d.error}`);
      }
      await fetchJobs();
    } finally {
      setActionLoading(l => ({ ...l, [key]: false }));
    }
  };

  const isActive = (s: string) => s === 'running' || s === 'pending';
  // A job is stuck if it's active and hasn't been updated in 30 minutes.
  // Exception: if enrich_total_items > 0 the enrichment queue was recently
  // seeded — Cloud Tasks workers update Firestore asynchronously, so a brief
  // gap between batch completion and the first enrichment counter increment is
  // normal. We give an extra 10-minute grace window in that case.
  const isStuck = (j: any) => {
    if (!isActive(j.status)) return false;
    const msSinceUpdate = now - new Date(j.updated_at || j.created_at).getTime();
    const hasEnrichQueue = (j.enrich_total_items || 0) > 0;
    const graceMs = hasEnrichQueue ? 40 * 60 * 1000 : 30 * 60 * 1000;
    return msSinceUpdate > graceMs;
  };

  const displayed = filterStatus === 'active'
    ? allJobs.filter(j => isActive(j.status) || isStuck(j))
    : allJobs;

  const summaryActive = allJobs.filter(j => isActive(j.status)).length;
  const summaryStuck  = allJobs.filter(j => isStuck(j)).length;
  const summaryFailed = allJobs.filter(j => j.status === 'failed').length;

  return (
    <div>
      {/* Controls */}
      <div style={{ display: 'flex', gap: 10, alignItems: 'flex-end', marginBottom: 20, flexWrap: 'wrap' }}>
        <div style={{ minWidth: 220 }}>
          <div style={{ fontSize: 10, fontWeight: 700, color: 'var(--text-muted)', marginBottom: 5, textTransform: 'uppercase', letterSpacing: '0.5px' }}>Client</div>
          {loadingTenants ? (
            <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>Loading clients…</div>
          ) : (
            <select className="select" value={selectedTenant} onChange={e => setSelectedTenant(e.target.value)} style={{ width: '100%' }}>
              <option value="ALL">All Clients ({tenants.length})</option>
              {tenants.map(t => <option key={t.tenant_id} value={t.tenant_id}>{t.name || t.tenant_id}</option>)}
            </select>
          )}
        </div>

        <div>
          <div style={{ fontSize: 10, fontWeight: 700, color: 'var(--text-muted)', marginBottom: 5, textTransform: 'uppercase', letterSpacing: '0.5px' }}>Show</div>
          <div style={{ display: 'flex', gap: 4 }}>
            {(['active', 'all'] as const).map(f => (
              <button key={f} onClick={() => setFilterStatus(f)} style={{
                padding: '5px 14px', fontSize: 12, fontWeight: 600, borderRadius: 6, cursor: 'pointer',
                border: '1px solid var(--border)',
                background: filterStatus === f ? 'var(--primary)' : 'var(--bg-secondary)',
                color: filterStatus === f ? '#fff' : 'var(--text-muted)',
              }}>
                {f === 'active' ? 'Active only' : 'All jobs'}
              </button>
            ))}
          </div>
        </div>

        <button onClick={fetchJobs} className="btn btn-secondary" style={{ fontSize: 12, alignSelf: 'flex-end' }}>
          {loadingJobs ? '…' : '🔄'} Refresh
        </button>

        <div style={{ marginLeft: 'auto', display: 'flex', gap: 8, alignItems: 'center' }}>
          {summaryActive > 0 && <span style={{ padding: '3px 10px', borderRadius: 12, fontSize: 11, fontWeight: 700, background: '#1d4ed833', color: '#60a5fa' }}>⚙️ {summaryActive} active</span>}
          {summaryStuck  > 0 && <span style={{ padding: '3px 10px', borderRadius: 12, fontSize: 11, fontWeight: 700, background: '#9f121233', color: '#f87171' }}>⚠️ {summaryStuck} stuck</span>}
          {summaryFailed > 0 && <span style={{ padding: '3px 10px', borderRadius: 12, fontSize: 11, fontWeight: 700, background: '#9f121233', color: '#f87171' }}>❌ {summaryFailed} failed</span>}
          <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>{allJobs.length} total jobs</span>
        </div>
      </div>

      {/* Empty state */}
      {!loadingJobs && displayed.length === 0 && (
        <div style={{ padding: 60, textAlign: 'center', background: 'var(--bg-secondary)', borderRadius: 12, color: 'var(--text-muted)' }}>
          <div style={{ fontSize: 36, marginBottom: 12 }}>📭</div>
          <div style={{ fontSize: 15, fontWeight: 600 }}>
            {filterStatus === 'active' ? 'No active jobs — all quiet!' : 'No import jobs found'}
          </div>
        </div>
      )}

      {/* Job cards */}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
        {displayed.map(job => {
          const active    = isActive(job.status);
          const stuck     = isStuck(job);
          const isAmazon  = job.channel === 'amazon' || job.channel === 'amazonnew';
          const enrichOn  = isAmazon && job.enrich_data !== false;
          const total     = job.total_items || 0;
          const enrichTotal = job.enrich_total_items || 0;
          const enriched  = (job.enriched_items || 0) + (job.enrich_skipped_items || 0);
          const processed = job.processed_items || 0;
          // In enrichment phase when enrich_total > 0
          const inEnrichPhase = enrichOn && enrichTotal > 0;
          const displayed_items = inEnrichPhase ? enriched : (enrichOn ? enriched : processed);
          const denominator = inEnrichPhase ? Math.max(enrichTotal, enriched) : Math.max(total, processed);
          const pct       = denominator > 0 ? Math.min(Math.round(displayed_items / denominator * 100), 100) : 0;
          const barColor  = stuck ? '#f87171'
                          : job.status === 'completed' ? '#34d399'
                          : job.status === 'cancelled' ? '#9ca3af'
                          : CHANNEL_COLORS[job.channel] || 'var(--primary)';

          const accountLabel = job.account_name || job.channel_account_id?.slice(-12) || job.channel;
          const elapsedStr   = elapsed(job.started_at || job.created_at, now);

          // Status message — always visible for active jobs, animates with elapsed
          let subLine = '';
          if (active) {
            if (job.status_message) {
              subLine = job.status_message;
            } else if (inEnrichPhase) {
              subLine = `Enriching products… ${enriched.toLocaleString()} / ${enrichTotal.toLocaleString()}`;
            } else {
              subLine = `Running for ${elapsedStr}`;
            }
          } else if (job.status_message) {
            subLine = job.status_message;
          }

          const isCancelling = actionLoading[`${job.job_id}-cancel`];
          const isDeleting   = actionLoading[`${job.job_id}-delete`];

          return (
            <div key={job.job_id} style={{
              padding: '14px 16px',
              background: 'var(--bg-secondary)',
              borderRadius: 10,
              border: `1px solid ${stuck ? '#f8717155' : 'var(--border)'}`,
              borderLeft: `4px solid ${barColor}`,
            }}>
              {/* Top row */}
              <div style={{ display: 'flex', alignItems: 'flex-start', gap: 12, marginBottom: 10 }}>
                <span style={{ fontSize: 22, lineHeight: 1, marginTop: 1 }}>{CHANNEL_EMOJI[job.channel] || '🌐'}</span>

                <div style={{ flex: 1, minWidth: 0 }}>
                  {/* PRIMARY LABEL: "Product Import — Steve's Amazon Account" */}
                  <div style={{ fontWeight: 700, fontSize: 14, color: 'var(--text-primary)', marginBottom: 2 }}>
                    {job.job_type === 'full_import' ? 'Product Import' : (job.job_type || 'Import').replace(/_/g, ' ')}
                    <span style={{ color: 'var(--text-muted)', fontWeight: 400, margin: '0 6px' }}>—</span>
                    <span style={{ color: CHANNEL_COLORS[job.channel] || 'var(--text-primary)' }}>
                      {accountLabel}
                    </span>
                  </div>

                  {/* SECONDARY: tenant · channel · enrichment flag */}
                  <div style={{ fontSize: 11, color: 'var(--text-muted)', marginBottom: active ? 4 : 0 }}>
                    👤 {job._tenantName}
                    {'  ·  '}
                    {job.channel.charAt(0).toUpperCase() + job.channel.slice(1)}
                    {enrichOn ? ' + Enrichment' : ''}
                    {'  ·  '}
                    {fmtDate(job.started_at || job.created_at)}
                  </div>

                  {/* STATUS MESSAGE LINE — live ticker for active jobs */}
                  {subLine && (
                    <div style={{
                      fontSize: 11,
                      color: stuck ? '#f87171' : active ? 'var(--text-secondary)' : 'var(--text-muted)',
                      fontStyle: active && !job.status_message ? 'italic' : 'normal',
                      display: 'flex', alignItems: 'center', gap: 5,
                    }}>
                      {active && (
                        <span style={{ fontVariantNumeric: 'tabular-nums', color: 'var(--text-muted)', flexShrink: 0 }}>
                          ⏱ {elapsedStr} ·
                        </span>
                      )}
                      <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
                        title={subLine}>{subLine}</span>
                    </div>
                  )}
                </div>

                {/* Status pill */}
                <StatusPill status={job.status} stuck={stuck} />

                {/* Actions */}
                <div style={{ display: 'flex', gap: 6, flexShrink: 0 }}>
                  {active && (
                    <button onClick={() => doAction(job, 'cancel')} disabled={isCancelling}
                      className="btn btn-secondary"
                      style={{ fontSize: 11, padding: '3px 10px', color: '#f87171', borderColor: '#f8717155' }}>
                      {isCancelling ? '…' : '⏹ Cancel'}
                    </button>
                  )}
                  {!active && enrichOn && enriched < total && total > 0 && (
                    <button onClick={() => doAction(job, 'resume')}
                      className="btn btn-secondary"
                      style={{ fontSize: 11, padding: '3px 10px', color: 'var(--accent-cyan)', borderColor: 'var(--accent-cyan)55' }}>
                      ✨ Resume
                    </button>
                  )}
                  <button onClick={() => doAction(job, 'delete')} disabled={isDeleting || active}
                    title={active ? 'Cancel first' : 'Delete record'}
                    className="btn btn-secondary"
                    style={{ fontSize: 11, padding: '3px 10px',
                      color: active ? 'var(--text-muted)' : '#f87171',
                      borderColor: active ? 'var(--border)' : '#f8717155',
                      opacity: active ? 0.3 : 1, cursor: active ? 'not-allowed' : 'pointer' }}>
                    {isDeleting ? '…' : '🗑'}
                  </button>
                </div>
              </div>

              {/* Progress row */}
              <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
                <div style={{ flex: 1, height: 6, background: 'var(--bg-tertiary)', borderRadius: 3, overflow: 'hidden' }}>
                  {denominator === 0 && active ? (
                    <div style={{ height: '100%', width: '35%', background: barColor, opacity: 0.7,
                      animation: 'pulse-bar 1.4s ease-in-out infinite alternate' }} />
                  ) : (
                    <div style={{ height: '100%', width: `${pct}%`, background: barColor,
                      borderRadius: 3, transition: 'width 0.5s ease' }} />
                  )}
                </div>

                {/* Items — most important number */}
                <div style={{ fontSize: 13, fontWeight: 700, color: 'var(--text-primary)',
                  fontVariantNumeric: 'tabular-nums', whiteSpace: 'nowrap', minWidth: 120, textAlign: 'right' }}>
                  {denominator > 0
                    ? `${displayed_items.toLocaleString()} / ${denominator.toLocaleString()}`
                    : active ? '—' : '0'}
                </div>

                {denominator > 0 && (
                  <div style={{ fontSize: 11, color: 'var(--text-muted)', width: 36, textAlign: 'right' }}>{pct}%</div>
                )}

                {/* Last Firestore update — shows if backend is alive */}
                <div style={{ fontSize: 11, color: stuck ? '#f87171' : 'var(--text-muted)', whiteSpace: 'nowrap' }}>
                  {active ? `last update: ${timeAgo(job.updated_at)}` : ''}
                </div>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

// ============================================================================
// SYSTEM JOBS TAB
// ============================================================================
function SystemJobsTab() {
  const [jobs, setJobs]               = useState<Record<string, any>>({});
  const [loading, setLoading]         = useState(true);
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [lastRefresh, setLastRefresh] = useState(new Date());
  const tenantId                      = getActiveTenantId();

  const fetchJobs = useCallback(async () => {
    const next: Record<string, any> = {};
    for (const jt of JOB_TYPES) {
      try {
        const res = await fetch(`${API_BASE}/${jt.endpoint}`, { headers: { 'X-Tenant-Id': tenantId } });
        next[jt.id] = res.ok
          ? { jobs: (await res.json()).jobs || [], error: null }
          : { jobs: [], error: `HTTP ${res.status}` };
      } catch (e: any) {
        next[jt.id] = { jobs: [], error: e.message };
      }
    }
    setJobs(next);
    setLastRefresh(new Date());
    setLoading(false);
  }, [tenantId]);

  useEffect(() => { fetchJobs(); }, [fetchJobs]);
  useEffect(() => {
    if (!autoRefresh) return;
    const t = setInterval(fetchJobs, 10000);
    return () => clearInterval(t);
  }, [autoRefresh, fetchJobs]);

  const cancelJob = async (jt: any, jobId: string) => {
    if (!confirm(`Cancel ${jt.name} job?`)) return;
    const endpoint = jt.endpoint.replace('/jobs', `/jobs/${jobId}/cancel`);
    const res = await fetch(`${API_BASE}/${endpoint}`, { method: 'POST', headers: { 'X-Tenant-Id': tenantId } });
    res.ok ? (alert('Cancelled'), fetchJobs()) : alert(`Failed: HTTP ${res.status}`);
  };

  const restartJob = async (jt: any, marketplaceId: string) => {
    const cfgs: Record<string, any> = {
      'amazon-schema': { url: 'amazon/schemas/download-all', body: { marketplaceId } },
      'ebay-schema':   { url: 'ebay/schemas/sync', body: { marketplaceId, fullSync: false } },
      'temu-schema':   { url: 'temu/schemas/sync', body: { fullSync: false } },
    };
    const cfg = cfgs[jt.id];
    if (!cfg || !confirm(`Start new ${jt.name} sync?`)) return;
    const res = await fetch(`${API_BASE}/${cfg.url}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': tenantId },
      body: JSON.stringify(cfg.body),
    });
    res.ok ? (alert('Started'), fetchJobs()) : alert(`Failed: ${await res.text()}`);
  };

  const isStuck = (job: any) =>
    job.status === 'running' &&
    (Date.now() - new Date(job.updatedAt).getTime()) > 30 * 60 * 1000;

  if (loading) return <div style={{ padding: 48, textAlign: 'center', color: 'var(--text-muted)' }}>Loading…</div>;

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 10, marginBottom: 20, alignItems: 'center' }}>
        <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>Updated {timeAgo(lastRefresh.toISOString())}</span>
        <button onClick={fetchJobs} className="btn btn-secondary" style={{ fontSize: 12 }}>🔄 Refresh</button>
        <button onClick={() => setAutoRefresh(a => !a)} style={{
          padding: '5px 14px', fontSize: 12, fontWeight: 600, borderRadius: 6, cursor: 'pointer',
          border: '1px solid var(--border)',
          background: autoRefresh ? 'var(--success)' : 'var(--bg-secondary)',
          color: autoRefresh ? '#fff' : 'var(--text-secondary)',
        }}>
          {autoRefresh ? '🟢 Auto ON' : '⚫ Auto OFF'}
        </button>
      </div>

      {JOB_TYPES.map(jt => {
        const d = jobs[jt.id] || { jobs: [], error: null };
        const running = d.jobs.filter((j: any) => j.status === 'running').length;
        const stuckN  = d.jobs.filter((j: any) => isStuck(j)).length;
        return (
          <div key={jt.id} style={{ marginBottom: 24 }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center',
              padding: '8px 14px', background: 'var(--bg-secondary)', borderRadius: 8,
              borderLeft: `4px solid ${jt.color}`, marginBottom: 10 }}>
              <div>
                <span style={{ fontWeight: 700, fontSize: 14, color: 'var(--text-primary)' }}>{jt.name}</span>
                <span style={{ fontSize: 12, color: 'var(--text-muted)', marginLeft: 10 }}>
                  {d.jobs.length} total{running > 0 ? ` · ${running} running` : ''}{stuckN > 0 ? ` · ⚠️ ${stuckN} stuck` : ''}
                </span>
              </div>
              {d.error && <span style={{ color: 'var(--danger)', fontSize: 12 }}>❌ {d.error}</span>}
            </div>
            {d.jobs.length === 0 && !d.error && (
              <div style={{ padding: 20, textAlign: 'center', color: 'var(--text-muted)', fontSize: 13,
                background: 'var(--bg-secondary)', borderRadius: 8 }}>No jobs</div>
            )}
            {d.jobs.slice(0, 10).map((job: any) => {
              const stuck = isStuck(job);
              const pct = job.total > 0
                ? Math.round(((job.downloaded || 0) + (job.skipped || 0)) / job.total * 100) : 0;
              return (
                <div key={job.jobId} style={{ marginBottom: 8, padding: '12px 14px',
                  background: 'var(--bg-secondary)', borderRadius: 8,
                  border: `1px solid ${stuck ? '#f8717155' : 'var(--border)'}`,
                  borderLeft: `4px solid ${stuck ? '#f87171' : jt.color}` }}>
                  <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 }}>
                    <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                      <StatusPill status={job.status} stuck={stuck} />
                      <span style={{ fontSize: 11, color: 'var(--text-muted)', fontFamily: 'monospace' }}>
                        {job.jobId?.substring(0, 20)}…
                      </span>
                    </div>
                    <div style={{ display: 'flex', gap: 6 }}>
                      {(job.status === 'running' || stuck) && (
                        <button onClick={() => cancelJob(jt, job.jobId)} className="btn btn-secondary"
                          style={{ fontSize: 11, padding: '3px 8px', color: '#f87171', borderColor: '#f8717155' }}>
                          ⏹ Cancel
                        </button>
                      )}
                      {(job.status === 'failed' || job.status === 'cancelled') && jt.id.includes('schema') && (
                        <button onClick={() => restartJob(jt, job.marketplaceId)} className="btn btn-secondary"
                          style={{ fontSize: 11, padding: '3px 8px', color: '#34d399', borderColor: '#34d39955' }}>
                          🔄 Restart
                        </button>
                      )}
                    </div>
                  </div>
                  {job.total > 0 && (
                    <div style={{ marginBottom: 8 }}>
                      <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 11, color: 'var(--text-muted)', marginBottom: 3 }}>
                        <span>{(job.downloaded || 0) + (job.skipped || 0)} / {job.total} schemas</span>
                        <span>{pct}%</span>
                      </div>
                      <div style={{ height: 5, background: 'var(--bg-tertiary)', borderRadius: 3, overflow: 'hidden' }}>
                        <div style={{ height: '100%', width: `${pct}%`, background: stuck ? '#f87171' : jt.color, transition: 'width 0.3s' }} />
                      </div>
                    </div>
                  )}
                  <div style={{ fontSize: 11, color: 'var(--text-muted)' }}>
                    Started {timeAgo(job.startedAt)} · Updated {timeAgo(job.updatedAt)}
                    {job.failed > 0 && <span style={{ color: '#f87171', marginLeft: 8 }}>· {job.failed} failed</span>}
                  </div>
                </div>
              );
            })}
          </div>
        );
      })}
    </div>
  );
}

// ============================================================================
// MAIN COMPONENT
// ============================================================================
export default function JobMonitor() {
  const [tab, setTab] = useState<'customer' | 'system'>('customer');

  const tabBtn = (t: typeof tab, label: string) => (
    <button key={t} onClick={() => setTab(t)} style={{
      padding: '8px 22px', fontSize: 13, fontWeight: 600, cursor: 'pointer',
      borderRadius: '6px 6px 0 0',
      border: '1px solid var(--border)',
      borderBottom: tab === t ? '1px solid var(--bg-primary)' : '1px solid var(--border)',
      background: tab === t ? 'var(--bg-primary)' : 'var(--bg-secondary)',
      color: tab === t ? 'var(--text-primary)' : 'var(--text-muted)',
      marginBottom: -1,
    }}>{label}</button>
  );

  return (
    <div style={{ padding: 24, maxWidth: 1400, margin: '0 auto' }}>
      <style>{`@keyframes pulse-bar { 0%{width:8%;opacity:0.3} 100%{width:80%;opacity:0.8} }`}</style>

      <div style={{ marginBottom: 24 }}>
        <h1 style={{ fontSize: 24, fontWeight: 700, color: 'var(--text-primary)', marginBottom: 4 }}>Job Monitor</h1>
        <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>
          Live view of all import and background jobs — auto-refreshes every 5s
        </p>
      </div>

      <div style={{ display: 'flex', gap: 4, borderBottom: '1px solid var(--border)', marginBottom: 0 }}>
        {tabBtn('customer', '👤 Customer Import Jobs')}
        {tabBtn('system',   '⚙️ System Jobs')}
      </div>

      <div style={{ paddingTop: 24 }}>
        {tab === 'customer' ? <CustomerJobsTab /> : <SystemJobsTab />}
      </div>
    </div>
  );
}

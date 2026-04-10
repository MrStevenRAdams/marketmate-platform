import { useState, useEffect, useCallback, useRef } from 'react';
import { getActiveTenantId } from '../../contexts/TenantContext';

// ============================================================================
// UNIFIED SCHEMA CACHE MANAGER
// ============================================================================
// Shows Amazon, eBay and Temu schema sync jobs running simultaneously.
// Each marketplace has its own panel with:
//   - Start / Cancel buttons
//   - Progress bar (downloaded / skipped / failed / total)
//   - currentActivity field — shows exactly what the job is doing right now
//   - Live log stream (last 200 entries from Firestore job document)
//   - Error list (expandable)
//   - Auto-refresh settings
//
// Server memory (MiB) is polled every 10 seconds from GET /api/v1/system/memory
// and shown in the header so operators can distinguish "job stuck" from "OOM".
//
// All three jobs poll independently at 3-second intervals.
// ============================================================================

// ── Types ─────────────────────────────────────────────────────────────────────

interface LogEntry {
  t: string;   // ISO timestamp
  msg: string;
  lvl: 'info' | 'warn' | 'error';
}

interface JobState {
  jobId: string;
  status: 'running' | 'completed' | 'cancelled' | 'failed';
  downloaded: number;
  skipped: number;
  failed: number;
  total: number;
  leafFound?: number;
  errors: string[];
  logs: LogEntry[];
  currentActivity: string;
  startedAt: any;
  updatedAt: any;
  completedAt?: any;
  treeWalkDone?: boolean;
  treeDownloaded?: boolean;
  error?: string;
  triggeredBy?: string;
}

interface MemoryStats {
  heapAllocMiB: number;
  heapSysMiB: number;
  heapInUseMiB: number;
  numGC: number;
  sampledAt: string;
}

interface MarketplacePanelState {
  selectedMarketplace: string;
  activeJob: JobState | null;
  recentJobs: JobState[];
  statsLabel: string;
  isStarting: boolean;
  error: string;
  showLogs: boolean;
  showErrors: boolean;
  showSettings: boolean;
  refreshSettings: {
    enabled: boolean;
    interval_days: number;
    marketplace_id?: string;
    last_run_at?: string;
  };
  settingsSaving: boolean;
  settingsSaved: boolean;
}

// ── Constants ─────────────────────────────────────────────────────────────────

const AMAZON_MARKETPLACES = [
  { id: 'A1F83G8C2ARO7P', name: 'UK', flag: '🇬🇧' },
  { id: 'ATVPDKIKX0DER',  name: 'US', flag: '🇺🇸' },
  { id: 'A1PA6795UKMFR9', name: 'DE', flag: '🇩🇪' },
  { id: 'A1RKKUPIHCS9HS', name: 'ES', flag: '🇪🇸' },
  { id: 'A13V1IB3VIYZZH', name: 'FR', flag: '🇫🇷' },
  { id: 'APJ6JRA9NG5V4',  name: 'IT', flag: '🇮🇹' },
  { id: 'A1805IZSGTT6HS', name: 'NL', flag: '🇳🇱' },
  { id: 'A2NODRKZP88ZB9', name: 'SE', flag: '🇸🇪' },
  { id: 'A2EUQ1WTGCTBG2', name: 'CA', flag: '🇨🇦' },
  { id: 'A39IBJ37TRP1C6', name: 'AU', flag: '🇦🇺' },
];

const EBAY_MARKETPLACES = [
  { id: 'EBAY_GB', name: 'UK',  flag: '🇬🇧' },
  { id: 'EBAY_US', name: 'US',  flag: '🇺🇸' },
  { id: 'EBAY_DE', name: 'DE',  flag: '🇩🇪' },
  { id: 'EBAY_AU', name: 'AU',  flag: '🇦🇺' },
  { id: 'EBAY_CA', name: 'CA',  flag: '🇨🇦' },
  { id: 'EBAY_FR', name: 'FR',  flag: '🇫🇷' },
  { id: 'EBAY_IT', name: 'IT',  flag: '🇮🇹' },
  { id: 'EBAY_ES', name: 'ES',  flag: '🇪🇸' },
];

const MARKETPLACE_CONFIG = {
  amazon: {
    label: 'Amazon',
    icon: '🛒',
    color: '#FF9900',
    syncPath: '/amazon/schemas/download-all',
    jobsPath: '/amazon/schemas/jobs',
    statsPath: (mpId: string) => `/amazon/schemas/list?marketplace_id=${mpId}`,
    settingsGetPath: '/amazon/schemas/refresh-settings',
    settingsPutPath: '/amazon/schemas/refresh-settings',
    buildBody: (mpId: string, fullSync: boolean) => ({ marketplaceId: mpId }),
    defaultMarketplace: 'A1F83G8C2ARO7P',
    marketplaces: AMAZON_MARKETPLACES,
    defaultSettings: { enabled: false, interval_days: 7, marketplace_id: 'A1F83G8C2ARO7P' },
  },
  ebay: {
    label: 'eBay',
    icon: '🏷️',
    color: '#E53238',
    syncPath: '/ebay/schemas/sync',
    jobsPath: '/ebay/schemas/jobs',
    statsPath: (mpId: string) => `/ebay/schemas/stats?marketplace_id=${mpId}`,
    settingsGetPath: '/ebay/schemas/refresh-settings',
    settingsPutPath: '/ebay/schemas/refresh-settings',
    buildBody: (mpId: string, fullSync: boolean) => ({ marketplaceId: mpId, fullSync }),
    defaultMarketplace: 'EBAY_GB',
    marketplaces: EBAY_MARKETPLACES,
    defaultSettings: { enabled: false, interval_days: 7, marketplace_id: 'EBAY_GB' },
  },
  temu: {
    label: 'Temu',
    icon: '🎁',
    color: '#FF6B35',
    syncPath: '/temu/schemas/sync',
    syncMissingRootsPath: '/temu/schemas/sync-missing-roots',
    jobsPath: '/temu/schemas/jobs',
    statsPath: (_mpId: string) => '/temu/schemas/stats',
    settingsGetPath: '/temu/schemas/refresh-settings',
    settingsPutPath: '/temu/schemas/refresh-settings',
    buildBody: (_mpId: string, fullSync: boolean) => ({ fullSync }),
    defaultMarketplace: 'global',
    marketplaces: [{ id: 'global', name: 'Global', flag: '🌍' }],
    defaultSettings: { enabled: false, interval_days: 7 },
  },
} as const;

type MarketplaceKey = keyof typeof MARKETPLACE_CONFIG;

// ── Helpers ───────────────────────────────────────────────────────────────────

const POLL_INTERVAL_MS = 3000;
const MEMORY_INTERVAL_MS = 10000;

function emptyPanel(key: MarketplaceKey): MarketplacePanelState {
  return {
    selectedMarketplace: MARKETPLACE_CONFIG[key].defaultMarketplace,
    activeJob: null,
    recentJobs: [],
    statsLabel: '—',
    isStarting: false,
    error: '',
    showLogs: false,
    showErrors: false,
    showSettings: false,
    refreshSettings: { ...MARKETPLACE_CONFIG[key].defaultSettings } as any,
    settingsSaving: false,
    settingsSaved: false,
  };
}

function progressPct(job: JobState): number {
  const total = job.total || job.leafFound || 0;
  if (total === 0) return 0;
  return Math.min(100, ((job.downloaded + job.skipped + job.failed) / total) * 100);
}

function jobStatusColor(status: string): string {
  switch (status) {
    case 'running':   return '#facc15';
    case 'completed': return '#4ade80';
    case 'cancelled': return '#94a3b8';
    case 'failed':    return '#f87171';
    default:          return '#94a3b8';
  }
}

function logLevelColor(lvl: string): string {
  switch (lvl) {
    case 'error': return '#fca5a5';
    case 'warn':  return '#fde68a';
    default:      return '#cbd5e1';
  }
}

function fmtTime(t: any): string {
  try {
    // Firestore Timestamp objects have a toDate() method or a seconds field.
    if (t && typeof t === 'object') {
      if (typeof t.toDate === 'function') return t.toDate().toLocaleTimeString();
      if (typeof t.seconds === 'number') return new Date(t.seconds * 1000).toLocaleTimeString();
    }
    // Plain ISO string or anything else Date can parse.
    const d = new Date(t);
    if (!isNaN(d.getTime())) return d.toLocaleTimeString();
    return String(t);
  } catch {
    return String(t);
  }
}

function elapsed(startedAt: any): string {
  if (!startedAt) return '';
  const d = typeof startedAt === 'string' ? new Date(startedAt) : startedAt?.toDate?.() ?? new Date(startedAt);
  const secs = Math.floor((Date.now() - d.getTime()) / 1000);
  if (secs < 60) return `${secs}s`;
  if (secs < 3600) return `${Math.floor(secs / 60)}m ${secs % 60}s`;
  return `${Math.floor(secs / 3600)}h ${Math.floor((secs % 3600) / 60)}m`;
}

// ── Main Component ────────────────────────────────────────────────────────────

export default function SchemaCacheManager() {
  const API_BASE = (import.meta as any).env?.VITE_API_URL || 'http://localhost:8080/api/v1';
  const tenantId = getActiveTenantId();

  const [panels, setPanels] = useState<Record<MarketplaceKey, MarketplacePanelState>>({
    amazon: emptyPanel('amazon'),
    ebay:   emptyPanel('ebay'),
    temu:   emptyPanel('temu'),
  });

  const [memory, setMemory] = useState<MemoryStats | null>(null);
  const [tick, setTick] = useState(0); // forces elapsed timer re-render

  // Refs for per-marketplace poll intervals (so they don't interfere with each other).
  const pollRefs = useRef<Record<MarketplaceKey, ReturnType<typeof setInterval> | null>>({
    amazon: null, ebay: null, temu: null,
  });

  // ── API fetch wrapper ──────────────────────────────────────────────────────

  const apiFetch = useCallback(async (path: string, options?: RequestInit) => {
    const res = await fetch(`${API_BASE}${path}`, {
      ...options,
      headers: {
        'Content-Type': 'application/json',
        'X-Tenant-Id': tenantId,
        ...(options?.headers || {}),
      },
    });
    if (!res.ok) {
      const text = await res.text();
      let msg = `HTTP ${res.status}`;
      try { msg = JSON.parse(text).error || msg; } catch { /* keep status msg */ }
      throw new Error(msg);
    }
    return res.json();
  }, [API_BASE, tenantId]);

  // ── Panel state updater ────────────────────────────────────────────────────

  const updatePanel = useCallback((key: MarketplaceKey, patch: Partial<MarketplacePanelState>) => {
    setPanels(prev => ({ ...prev, [key]: { ...prev[key], ...patch } }));
  }, []);

  // ── Stop polling for a marketplace ────────────────────────────────────────

  const stopPoll = useCallback((key: MarketplaceKey) => {
    if (pollRefs.current[key]) {
      clearInterval(pollRefs.current[key]!);
      pollRefs.current[key] = null;
    }
  }, []);

  // ── Fetch job status once ──────────────────────────────────────────────────

  const fetchJobStatus = useCallback(async (key: MarketplaceKey, jobId: string) => {
    try {
      const data = await apiFetch(`/${key}/schemas/jobs/${jobId}`) as JobState;
      updatePanel(key, { activeJob: data });
      if (data.status !== 'running') {
        stopPoll(key);
        // Reload stats
        fetchStats(key);
      }
    } catch (err: any) {
      // Don't kill polling on transient errors — log and continue.
      console.warn(`[${key}] poll error:`, err.message);
    }
  }, [apiFetch, updatePanel, stopPoll]);

  // ── Start polling ──────────────────────────────────────────────────────────

  const startPoll = useCallback((key: MarketplaceKey, jobId: string) => {
    stopPoll(key);
    pollRefs.current[key] = setInterval(() => {
      fetchJobStatus(key, jobId);
    }, POLL_INTERVAL_MS);
  }, [stopPoll, fetchJobStatus]);

  // ── Fetch stats ────────────────────────────────────────────────────────────

  const fetchStats = useCallback(async (key: MarketplaceKey) => {
    const cfg = MARKETPLACE_CONFIG[key];
    const mpId = panels[key].selectedMarketplace;
    try {
      const data = await apiFetch(cfg.statsPath(mpId));
      let label = '—';
      if (key === 'amazon') {
        label = `${data.count ?? 0} schemas cached`;
      } else if (key === 'ebay') {
        const pct = typeof data.cachePercentage === 'number' ? data.cachePercentage.toFixed(1) : '0';
        label = `${data.cachedAspects ?? 0} / ${data.leafCategories ?? 0} categories (${pct}%)`;
      } else {
        const pct = typeof data.cachePercentage === 'number' ? data.cachePercentage.toFixed(1) : '0';
        label = `${data.cachedTemplates ?? 0} / ${data.leafCategories ?? 0} templates (${pct}%)`;
      }
      updatePanel(key, { statsLabel: label });
    } catch { /* non-fatal */ }
  }, [apiFetch, updatePanel, panels]);

  // ── Fetch recent jobs ──────────────────────────────────────────────────────

  const fetchJobs = useCallback(async (key: MarketplaceKey) => {
    try {
      const data = await apiFetch(`/${key}/schemas/jobs`);
      const jobs: JobState[] = data.jobs || [];
      const running = jobs.find(j => j.status === 'running') || null;
      updatePanel(key, { recentJobs: jobs, activeJob: running });
      if (running) {
        startPoll(key, running.jobId);
      }
    } catch { /* non-fatal */ }
  }, [apiFetch, updatePanel, startPoll]);

  // ── Fetch refresh settings ─────────────────────────────────────────────────

  const fetchSettings = useCallback(async (key: MarketplaceKey) => {
    try {
      const data = await apiFetch(MARKETPLACE_CONFIG[key].settingsGetPath);
      if (data.settings) {
        updatePanel(key, { refreshSettings: data.settings });
      }
    } catch { /* non-fatal */ }
  }, [apiFetch, updatePanel]);

  // ── Start sync ─────────────────────────────────────────────────────────────

  const startSync = useCallback(async (key: MarketplaceKey, fullSync = false) => {
    const cfg = MARKETPLACE_CONFIG[key];
    const mpId = panels[key].selectedMarketplace;
    updatePanel(key, { isStarting: true, error: '' });
    try {
      const data = await apiFetch(cfg.syncPath, {
        method: 'POST',
        body: JSON.stringify(cfg.buildBody(mpId, fullSync)),
      });
      if (data.jobId) {
        const stub: JobState = {
          jobId: data.jobId,
          status: 'running',
          downloaded: 0, skipped: 0, failed: 0,
          total: data.total || 0,
          errors: [], logs: [],
          currentActivity: 'Starting...',
          startedAt: new Date().toISOString(),
          updatedAt: new Date().toISOString(),
        };
        updatePanel(key, { activeJob: stub });
        startPoll(key, data.jobId);
      }
    } catch (err: any) {
      updatePanel(key, { error: err.message });
    } finally {
      updatePanel(key, { isStarting: false });
    }
  }, [apiFetch, updatePanel, startPoll, panels]);

  // ── Cancel job ─────────────────────────────────────────────────────────────

  const cancelJob = useCallback(async (key: MarketplaceKey) => {
    const jobId = panels[key].activeJob?.jobId;
    if (!jobId) return;
    try {
      await apiFetch(`/${key}/schemas/jobs/${jobId}/cancel`, { method: 'POST' });
      updatePanel(key, { activeJob: { ...panels[key].activeJob!, status: 'cancelled' } });
      stopPoll(key);
    } catch (err: any) {
      updatePanel(key, { error: err.message });
    }
  }, [apiFetch, updatePanel, stopPoll, panels]);

  // ── Temu resume ────────────────────────────────────────────────────────────

  const resumeTemu = useCallback(async () => {
    updatePanel('temu', { isStarting: true, error: '' });
    try {
      const data = await apiFetch('/temu/schemas/resume', { method: 'POST' });
      if (data.jobId) {
        const stub: JobState = {
          jobId: data.jobId,
          status: 'running',
          downloaded: 0, skipped: 0, failed: 0,
          total: data.leafCount || 0, leafFound: data.leafCount || 0,
          errors: [], logs: [],
          currentActivity: `Resuming — ${data.leafCount} categories`,
          startedAt: new Date().toISOString(),
          updatedAt: new Date().toISOString(),
        };
        updatePanel('temu', { activeJob: stub });
        startPoll('temu', data.jobId);
      }
    } catch (err: any) {
      updatePanel('temu', { error: err.message });
    } finally {
      updatePanel('temu', { isStarting: false });
    }
  }, [apiFetch, updatePanel, startPoll]);

  // ── Temu sync missing roots ────────────────────────────────────────────────

  const syncMissingRootsTemu = useCallback(async () => {
    updatePanel('temu', { isStarting: true, error: '' });
    try {
      const data = await apiFetch('/temu/schemas/sync-missing-roots', { method: 'POST' });
      if (data.jobId) {
        const stub: JobState = {
          jobId: data.jobId,
          status: 'running',
          downloaded: 0, skipped: 0, failed: 0,
          total: 0, leafFound: 0,
          errors: [], logs: [],
          currentActivity: 'Loading existing categories from Firestore...',
          startedAt: new Date().toISOString(),
          updatedAt: new Date().toISOString(),
        };
        updatePanel('temu', { activeJob: stub });
        startPoll('temu', data.jobId);
      }
    } catch (err: any) {
      updatePanel('temu', { error: err.message });
    } finally {
      updatePanel('temu', { isStarting: false });
    }
  }, [apiFetch, updatePanel, startPoll]);

  // ── Save refresh settings ──────────────────────────────────────────────────

  const saveSettings = useCallback(async (key: MarketplaceKey) => {
    updatePanel(key, { settingsSaving: true, settingsSaved: false });
    try {
      await apiFetch(MARKETPLACE_CONFIG[key].settingsPutPath, {
        method: 'PUT',
        body: JSON.stringify(panels[key].refreshSettings),
      });
      updatePanel(key, { settingsSaved: true });
      setTimeout(() => updatePanel(key, { settingsSaved: false }), 3000);
    } catch (err: any) {
      updatePanel(key, { error: err.message });
    } finally {
      updatePanel(key, { settingsSaving: false });
    }
  }, [apiFetch, updatePanel, panels]);

  // ── Memory poll ────────────────────────────────────────────────────────────

  const fetchMemory = useCallback(async () => {
    try {
      const data = await apiFetch('/system/memory') as MemoryStats;
      setMemory(data);
    } catch { /* non-fatal */ }
  }, [apiFetch]);

  // ── Initial load ───────────────────────────────────────────────────────────

  useEffect(() => {
    if (!tenantId) return;
    (['amazon', 'ebay', 'temu'] as MarketplaceKey[]).forEach(key => {
      fetchStats(key);
      fetchJobs(key);
      fetchSettings(key);
    });
    fetchMemory();

    const memTimer = setInterval(fetchMemory, MEMORY_INTERVAL_MS);
    const tickTimer = setInterval(() => setTick(t => t + 1), 1000);

    return () => {
      clearInterval(memTimer);
      clearInterval(tickTimer);
      (['amazon', 'ebay', 'temu'] as MarketplaceKey[]).forEach(key => stopPoll(key));
    };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tenantId]);

  // ── Render ────────────────────────────────────────────────────────────────

  return (
    <div style={{ padding: '24px 20px', maxWidth: 1600, margin: '0 auto', color: 'var(--text-primary, #e2e8f0)' }}>

      {/* ── Header ── */}
      <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', marginBottom: 24, gap: 16, flexWrap: 'wrap' }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 22, fontWeight: 700, letterSpacing: '-0.3px' }}>
            Schema Cache Manager
          </h1>
          <p style={{ margin: '4px 0 0', fontSize: 13, color: 'var(--text-secondary, #94a3b8)' }}>
            Download and cache marketplace schemas for Amazon, eBay and Temu. All three can run simultaneously.
          </p>
        </div>

        {/* Memory widget */}
        <MemoryWidget memory={memory} />
      </div>

      {/* ── Three-column grid ── */}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 16, alignItems: 'start' }}>
        {(['amazon', 'ebay', 'temu'] as MarketplaceKey[]).map(key => (
          <MarketplacePanel
            key={key}
            mpKey={key}
            panel={panels[key]}
            tick={tick}
            onStartSync={(fullSync) => startSync(key, fullSync)}
            onCancel={() => cancelJob(key)}
            onResumeTemu={key === 'temu' ? resumeTemu : undefined}
            onSyncMissingRootsTemu={key === 'temu' ? syncMissingRootsTemu : undefined}
            onToggleLogs={() => updatePanel(key, { showLogs: !panels[key].showLogs })}
            onToggleErrors={() => updatePanel(key, { showErrors: !panels[key].showErrors })}
            onToggleSettings={() => updatePanel(key, { showSettings: !panels[key].showSettings })}
            onChangeMarketplace={(mpId) => {
              updatePanel(key, { selectedMarketplace: mpId });
              setTimeout(() => fetchStats(key), 100);
            }}
            onChangeSettings={(patch) => updatePanel(key, {
              refreshSettings: { ...panels[key].refreshSettings, ...patch },
            })}
            onSaveSettings={() => saveSettings(key)}
            onDismissError={() => updatePanel(key, { error: '' })}
          />
        ))}
      </div>
    </div>
  );
}

// ============================================================================
// MemoryWidget
// ============================================================================

function MemoryWidget({ memory }: { memory: MemoryStats | null }) {
  const heapMiB = memory?.heapAllocMiB ?? 0;
  const sysMiB = memory?.heapSysMiB ?? 0;
  const pct = sysMiB > 0 ? (heapMiB / sysMiB) * 100 : 0;
  const barColor = pct > 85 ? '#f87171' : pct > 65 ? '#fbbf24' : '#4ade80';

  return (
    <div style={{
      background: 'var(--bg-secondary, #1e293b)',
      border: '1px solid var(--border, #334155)',
      borderRadius: 10,
      padding: '10px 16px',
      minWidth: 220,
    }}>
      <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-secondary, #64748b)', letterSpacing: '0.08em', marginBottom: 6, textTransform: 'uppercase' }}>
        Server Memory
      </div>
      {memory ? (
        <>
          <div style={{ display: 'flex', alignItems: 'baseline', gap: 6, marginBottom: 6 }}>
            <span style={{ fontSize: 20, fontWeight: 700, fontVariantNumeric: 'tabular-nums' }}>
              {heapMiB.toFixed(1)}
            </span>
            <span style={{ fontSize: 12, color: 'var(--text-secondary, #64748b)' }}>MiB heap / {sysMiB.toFixed(1)} MiB sys</span>
          </div>
          <div style={{ height: 5, background: 'rgba(255,255,255,0.08)', borderRadius: 3, overflow: 'hidden', marginBottom: 4 }}>
            <div style={{ height: '100%', width: `${pct}%`, background: barColor, borderRadius: 3, transition: 'width 0.5s ease' }} />
          </div>
          <div style={{ fontSize: 10, color: 'var(--text-secondary, #64748b)', display: 'flex', justifyContent: 'space-between' }}>
            <span>GC runs: {memory.numGC}</span>
            <span>sampled {new Date(memory.sampledAt).toLocaleTimeString()}</span>
          </div>
        </>
      ) : (
        <div style={{ fontSize: 12, color: 'var(--text-secondary, #64748b)' }}>Polling every 10s…</div>
      )}
    </div>
  );
}

// ============================================================================
// MarketplacePanel
// ============================================================================

interface PanelProps {
  mpKey: MarketplaceKey;
  panel: MarketplacePanelState;
  tick: number;
  onStartSync: (fullSync: boolean) => void;
  onCancel: () => void;
  onResumeTemu?: () => void;
  onSyncMissingRootsTemu?: () => void;
  onToggleLogs: () => void;
  onToggleErrors: () => void;
  onToggleSettings: () => void;
  onChangeMarketplace: (mpId: string) => void;
  onChangeSettings: (patch: Partial<MarketplacePanelState['refreshSettings']>) => void;
  onSaveSettings: () => void;
  onDismissError: () => void;
}

function MarketplacePanel({
  mpKey, panel, tick,
  onStartSync, onCancel, onResumeTemu, onSyncMissingRootsTemu,
  onToggleLogs, onToggleErrors, onToggleSettings,
  onChangeMarketplace, onChangeSettings, onSaveSettings, onDismissError,
}: PanelProps) {
  const cfg = MARKETPLACE_CONFIG[mpKey];
  const { activeJob, isStarting, error, showLogs, showErrors, showSettings, refreshSettings, settingsSaving, settingsSaved } = panel;
  const isRunning = activeJob?.status === 'running';
  const logEndRef = useRef<HTMLDivElement>(null);

  // Auto-scroll logs to bottom when new entries arrive.
  useEffect(() => {
    if (showLogs && logEndRef.current) {
      logEndRef.current.scrollIntoView({ behavior: 'smooth' });
    }
  }, [activeJob?.logs?.length, showLogs]);

  const pct = activeJob ? progressPct(activeJob) : 0;
  const total = activeJob ? (activeJob.total || activeJob.leafFound || 0) : 0;
  const errCount = activeJob?.errors?.length ?? 0;
  const logCount = activeJob?.logs?.length ?? 0;

  return (
    <div style={{
      background: 'var(--bg-secondary, #1e293b)',
      border: `1px solid var(--border, #334155)`,
      borderRadius: 12,
      overflow: 'hidden',
      display: 'flex',
      flexDirection: 'column',
    }}>

      {/* ── Panel header ── */}
      <div style={{
        padding: '12px 16px',
        borderBottom: '1px solid var(--border, #334155)',
        background: 'rgba(255,255,255,0.02)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'space-between',
        gap: 8,
      }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <span style={{ fontSize: 20 }}>{cfg.icon}</span>
          <div>
            <div style={{ fontSize: 15, fontWeight: 700 }}>{cfg.label}</div>
            <div style={{ fontSize: 11, color: 'var(--text-secondary, #64748b)', marginTop: 1 }}>{panel.statsLabel}</div>
          </div>
        </div>

        {/* Marketplace selector */}
        {cfg.marketplaces.length > 1 && (
          <select
            value={panel.selectedMarketplace}
            onChange={e => onChangeMarketplace(e.target.value)}
            disabled={isRunning}
            style={{
              fontSize: 11, padding: '4px 6px', borderRadius: 6,
              border: '1px solid var(--border, #334155)',
              background: 'var(--bg-primary, #0f172a)',
              color: 'var(--text-primary, #e2e8f0)',
              cursor: isRunning ? 'not-allowed' : 'pointer',
            }}
          >
            {cfg.marketplaces.map(m => (
              <option key={m.id} value={m.id}>{m.flag} {m.name}</option>
            ))}
          </select>
        )}
      </div>

      {/* ── Panel body ── */}
      <div style={{ padding: '14px 16px', flex: 1, display: 'flex', flexDirection: 'column', gap: 12 }}>

        {/* Error banner */}
        {error && (
          <div style={{
            padding: '8px 12px', borderRadius: 8, fontSize: 12,
            background: 'rgba(248,113,113,0.12)', border: '1px solid #f87171',
            color: '#fca5a5', display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: 8,
          }}>
            <span style={{ flex: 1 }}>{error}</span>
            <button onClick={onDismissError} style={sBtnReset}>✕</button>
          </div>
        )}

        {/* Action buttons */}
        <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
          {!isRunning ? (
            <>
              <button
                onClick={() => onStartSync(false)}
                disabled={isStarting}
                style={{ ...sBtnPrimary, background: cfg.color, color: cfg.color === '#FF9900' ? '#000' : '#fff', opacity: isStarting ? 0.6 : 1 }}
              >
                {isStarting ? '⏳ Starting…' : '▶ Sync'}
              </button>
              <button
                onClick={() => onStartSync(true)}
                disabled={isStarting}
                style={{ ...sBtnSecondary, opacity: isStarting ? 0.6 : 1 }}
                title="Force re-download all, ignoring cache freshness"
              >
                ↻ Full
              </button>
              {mpKey === 'temu' && onResumeTemu && (
                <button onClick={onResumeTemu} disabled={isStarting} style={{ ...sBtnSecondary, opacity: isStarting ? 0.6 : 1 }} title="Download templates for all categories already in Firestore">
                  ⏩ Resume
                </button>
              )}
              {mpKey === 'temu' && onSyncMissingRootsTemu && (
                <button onClick={onSyncMissingRootsTemu} disabled={isStarting} style={{ ...sBtnSecondary, opacity: isStarting ? 0.6 : 1 }} title="Walk only root branches not yet saved in Firestore, then Resume to download templates">
                  🌿 Missing Roots
                </button>
              )}
            </>
          ) : (
            <button onClick={onCancel} style={{ ...sBtnPrimary, background: '#ef4444', color: '#fff' }}>
              ⏹ Cancel
            </button>
          )}
          <button onClick={onToggleSettings} style={{ ...sBtnSecondary, marginLeft: 'auto' }} title="Auto-refresh settings">⚙</button>
        </div>

        {/* Active job section */}
        {activeJob && (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>

            {/* Status row */}
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8 }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                <span style={{
                  display: 'inline-block', width: 8, height: 8, borderRadius: '50%',
                  background: jobStatusColor(activeJob.status),
                  boxShadow: isRunning ? `0 0 6px ${jobStatusColor(activeJob.status)}` : 'none',
                  animation: isRunning ? 'pulse 1.4s ease-in-out infinite' : 'none',
                }} />
                <span style={{ fontSize: 12, fontWeight: 600, color: jobStatusColor(activeJob.status), textTransform: 'capitalize' }}>
                  {activeJob.status}
                </span>
                {isRunning && activeJob.startedAt && (
                  <span style={{ fontSize: 11, color: 'var(--text-secondary, #64748b)' }}>
                    {elapsed(activeJob.startedAt)}
                  </span>
                )}
              </div>
              <div style={{ display: 'flex', gap: 10, fontSize: 11 }}>
                <span style={{ color: '#4ade80' }}>✓ {activeJob.downloaded}</span>
                <span style={{ color: '#94a3b8' }}>↷ {activeJob.skipped}</span>
                {activeJob.failed > 0 && <span style={{ color: '#f87171' }}>✗ {activeJob.failed}</span>}
                {total > 0 && <span style={{ color: 'var(--text-secondary, #64748b)' }}>/ {total}</span>}
              </div>
            </div>

            {/* Progress bar */}
            <div style={{ height: 5, background: 'rgba(255,255,255,0.07)', borderRadius: 3, overflow: 'hidden' }}>
              <div style={{
                height: '100%', borderRadius: 3, transition: 'width 0.4s ease',
                width: `${pct}%`,
                background: activeJob.status === 'completed' ? '#4ade80'
                  : activeJob.status === 'cancelled' ? '#94a3b8'
                  : activeJob.status === 'failed' ? '#f87171'
                  : cfg.color,
              }} />
            </div>

            {/* currentActivity — what's happening right now */}
            {activeJob.currentActivity && (
              <div style={{
                fontSize: 11, color: 'var(--text-secondary, #64748b)',
                fontFamily: 'monospace', padding: '5px 8px',
                background: 'rgba(0,0,0,0.25)', borderRadius: 6,
                whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis',
              }} title={activeJob.currentActivity}>
                {isRunning ? '⟳ ' : ''}{activeJob.currentActivity}
              </div>
            )}

            {/* Tree walk phase indicator (Temu / eBay) */}
            {mpKey === 'temu' && isRunning && (
              <div style={{ fontSize: 10, color: 'var(--text-secondary, #64748b)', display: 'flex', gap: 12 }}>
                <span style={{ color: activeJob.treeWalkDone ? '#4ade80' : cfg.color }}>
                  {activeJob.treeWalkDone ? '✓' : '⏳'} Phase 1: Tree walk ({activeJob.leafFound ?? 0} leaves)
                </span>
                <span style={{ color: activeJob.treeWalkDone ? cfg.color : 'var(--text-secondary, #64748b)' }}>
                  {activeJob.treeWalkDone ? '⏳' : '◦'} Phase 2: Templates
                </span>
              </div>
            )}

            {/* Toolbar: logs + errors toggles */}
            <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
              <button
                onClick={onToggleLogs}
                style={{ ...sBtnTiny, color: logCount > 0 ? cfg.color : 'var(--text-secondary, #64748b)' }}
              >
                {showLogs ? '▲' : '▼'} Logs ({logCount})
              </button>
              {errCount > 0 && (
                <button
                  onClick={onToggleErrors}
                  style={{ ...sBtnTiny, color: '#f87171' }}
                >
                  {showErrors ? '▲' : '▼'} Errors ({errCount})
                </button>
              )}
              {activeJob.error && (
                <span style={{ fontSize: 10, color: '#f87171', padding: '2px 6px', background: 'rgba(248,113,113,0.1)', borderRadius: 4 }}>
                  {activeJob.error}
                </span>
              )}
            </div>

            {/* Error list (collapsible) */}
            {showErrors && errCount > 0 && (
              <div style={{
                maxHeight: 120, overflowY: 'auto', padding: '6px 8px',
                background: 'rgba(248,113,113,0.06)', borderRadius: 6,
                border: '1px solid rgba(248,113,113,0.2)',
              }}>
                {activeJob.errors.slice(0, 50).map((e, i) => (
                  <div key={i} style={{ fontSize: 10, fontFamily: 'monospace', color: '#fca5a5', lineHeight: 1.6 }}>{e}</div>
                ))}
                {errCount > 50 && (
                  <div style={{ fontSize: 10, color: '#94a3b8', marginTop: 4 }}>…and {errCount - 50} more</div>
                )}
              </div>
            )}

            {/* Log stream (collapsible) */}
            {showLogs && (
              <div style={{
                maxHeight: 240, overflowY: 'auto', padding: '6px 8px',
                background: 'rgba(0,0,0,0.3)', borderRadius: 6,
                border: '1px solid var(--border, #334155)',
                fontFamily: 'monospace', fontSize: 10, lineHeight: 1.7,
              }}>
                {(activeJob.logs || []).map((entry, i) => (
                  <div key={i} style={{ display: 'flex', gap: 8, borderBottom: '1px solid rgba(255,255,255,0.03)', paddingBottom: 2 }}>
                    <span style={{ color: '#475569', flexShrink: 0 }}>{fmtTime(entry.t)}</span>
                    <span style={{ color: logLevelColor(entry.lvl), flexShrink: 0, width: 36 }}>{entry.lvl}</span>
                    <span style={{ color: logLevelColor(entry.lvl) }}>{entry.msg}</span>
                  </div>
                ))}
                <div ref={logEndRef} />
              </div>
            )}
          </div>
        )}

        {/* No job yet */}
        {!activeJob && !isStarting && (
          <div style={{ textAlign: 'center', padding: '20px 8px', color: 'var(--text-secondary, #64748b)', fontSize: 12 }}>
            No active job. Press <strong>Sync</strong> to start.
          </div>
        )}

        {/* Auto-refresh settings (collapsible) */}
        {showSettings && (
          <div style={{
            marginTop: 4, padding: '12px 14px',
            background: 'rgba(0,0,0,0.2)', borderRadius: 8,
            border: '1px solid var(--border, #334155)',
            display: 'flex', flexDirection: 'column', gap: 10,
          }}>
            <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-secondary, #64748b)', textTransform: 'uppercase', letterSpacing: '0.07em' }}>
              Auto-Refresh Settings
            </div>

            <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 12, cursor: 'pointer' }}>
              <input
                type="checkbox"
                checked={refreshSettings.enabled}
                onChange={e => onChangeSettings({ enabled: e.target.checked })}
              />
              Enable scheduled auto-refresh
            </label>

            <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <label style={{ fontSize: 12, color: 'var(--text-secondary, #64748b)', flexShrink: 0 }}>Every</label>
              <input
                type="number" min={1} max={90}
                value={refreshSettings.interval_days}
                onChange={e => onChangeSettings({ interval_days: parseInt(e.target.value) || 7 })}
                style={{ width: 52, ...sInput }}
              />
              <span style={{ fontSize: 12, color: 'var(--text-secondary, #64748b)' }}>days</span>
            </div>

            {refreshSettings.last_run_at && (
              <div style={{ fontSize: 10, color: 'var(--text-secondary, #64748b)' }}>
                Last run: {new Date(refreshSettings.last_run_at).toLocaleString()}
              </div>
            )}

            <button
              onClick={onSaveSettings}
              disabled={settingsSaving}
              style={{ ...sBtnPrimary, background: settingsSaved ? '#4ade80' : cfg.color, color: settingsSaved ? '#000' : cfg.color === '#FF9900' ? '#000' : '#fff', alignSelf: 'flex-start', opacity: settingsSaving ? 0.6 : 1 }}
            >
              {settingsSaving ? 'Saving…' : settingsSaved ? '✓ Saved' : 'Save Settings'}
            </button>
          </div>
        )}
      </div>

      {/* Pulse animation */}
      <style>{`
        @keyframes pulse {
          0%, 100% { opacity: 1; }
          50% { opacity: 0.4; }
        }
      `}</style>
    </div>
  );
}

// ── Shared styles ─────────────────────────────────────────────────────────────

const sBtnPrimary: React.CSSProperties = {
  padding: '7px 14px', borderRadius: 7, border: 'none',
  fontWeight: 700, fontSize: 12, cursor: 'pointer',
};

const sBtnSecondary: React.CSSProperties = {
  padding: '7px 12px', borderRadius: 7,
  border: '1px solid var(--border, #334155)',
  background: 'var(--bg-primary, #0f172a)',
  color: 'var(--text-secondary, #94a3b8)',
  fontWeight: 600, fontSize: 12, cursor: 'pointer',
};

const sBtnTiny: React.CSSProperties = {
  padding: '3px 8px', borderRadius: 5,
  border: '1px solid var(--border, #334155)',
  background: 'transparent',
  fontSize: 10, cursor: 'pointer', fontWeight: 600,
};

const sBtnReset: React.CSSProperties = {
  background: 'transparent', border: 'none',
  color: '#fca5a5', fontSize: 14, cursor: 'pointer', padding: '0 2px', flexShrink: 0,
};

const sInput: React.CSSProperties = {
  padding: '5px 8px', borderRadius: 6,
  border: '1px solid var(--border, #334155)',
  background: 'var(--bg-primary, #0f172a)',
  color: 'var(--text-primary, #e2e8f0)',
  fontSize: 12, outline: 'none',
};

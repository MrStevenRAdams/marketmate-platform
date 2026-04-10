import { useState, useEffect, useCallback, useRef } from 'react';
import { getActiveTenantId } from '../contexts/TenantContext';
import './ImportExport.css';

const API_BASE = import.meta.env.VITE_API_URL ?? 'http://localhost:8080/api/v1';

function api(path: string, init?: RequestInit) {
  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: { 'X-Tenant-Id': getActiveTenantId(), ...(init?.headers || {}) },
  });
}

// ─── Types ─────────────────────────────────────────────────────────────────────

// Import types — 'products' now routes to /pim/import/* endpoints
type ImportType = 'products' | 'listings' | 'prices' | 'inventory_basic' | 'inventory_delta' | 'inventory_advanced' | 'binrack_zone' | 'binrack_create_update' | 'binrack_item_restriction' | 'binrack_storage_group' | 'stock_migration' | 'orders';
type ExportType = 'products' | 'listings' | 'prices' | 'inventory_basic' | 'inventory_advanced' | 'rma' | 'purchase_orders' | 'shipments';
type FileFormat = 'csv' | 'xlsx';

interface RowError   { row: number; column: string; message: string; }
interface RowWarning { row: number; column: string; message: string; }

interface ValidationResult {
  total_rows: number; valid_rows: number;
  create_count: number; update_count: number;
  error_count: number; warning_count: number;
  errors: RowError[]; warnings: RowWarning[];
  unknown_locations?: string[];
}

interface ImportJob {
  job_id: string; import_type: string; filename: string;
  status: 'pending' | 'processing' | 'done' | 'failed';
  total_rows: number; processed_rows: number;
  created_count: number; updated_count: number; failed_count: number;
  created_at: string; updated_at: string;
  error_report?: RowError[];
}

// Marketplace product import job (from /marketplace/import/jobs)
interface MarketplaceImportJob {
  job_id: string;
  channel: string;
  channel_account_id: string;
  job_type: string;
  status: 'pending' | 'running' | 'completed' | 'failed' | 'cancelled';
  total_items: number;
  processed_items: number;
  successful_items: number;
  failed_items: number;
  enriched_items: number;
  enrich_total_items: number;
  enrich_skipped_items: number;
  enrich_failed_items: number;
  match_status?: string;
  status_message?: string;
  started_at?: string;
  created_at: string;
}

const adapterEmoji: Record<string, string> = {
  amazon: '📦', amazonnew: '📦', ebay: '🏷️', shopify: '🛒', temu: '🛍️',
  tiktok: '🎵', etsy: '🛍️', woocommerce: '🛒', magento: '🏪', bigcommerce: '🛒',
  onbuy: '🏷️', walmart: '🛒', kaufland: '🛒', backmarket: '🔁',
  zalando: '👟', bol: '📚', lazada: '🛍️', bluepark: '🔵', wish: '⭐', tesco: '🏪',
};

interface FileSettings {
  delimiter: string;
  encoding: string;
  hasHeaderRow: boolean;
  escapeChar: string;
}

interface PreviewResult {
  headers: string[];
  preview_rows: string[][];
  required_fields: string[];
  optional_fields: string[];
  auto_mapping: Record<string, string>;
}

type Step = 'upload' | 'file_settings' | 'column_mapping' | 'validate' | 'apply' | 'done';

// ─── Constants ─────────────────────────────────────────────────────────────────

// Whether an import type routes to the new /pim/import/* endpoints
// (products) vs the existing /import/* endpoints (everything else).
function isPIMImport(t: ImportType): boolean {
  return t === 'products';
}

// Build the preview/validate/apply URL prefix for the active import type
function importPrefix(t: ImportType): string {
  return isPIMImport(t) ? '/pim/import' : '/import';
}

// Template download URL
function templateUrl(t: ImportType): string {
  return isPIMImport(t) ? `/pim/template?format=csv` : `/import/templates/${t}`;
}

// Template filename
function templateFilename(t: ImportType): string {
  return isPIMImport(t) ? 'products_template.csv' : `${t}_template.csv`;
}

const IMPORT_TYPES: { value: ImportType; label: string; icon: string; desc: string; columns?: string }[] = [
  { value: 'products', label: 'Products', icon: '📦',
    desc: 'Create or update PIM catalogue — simple, variation and bundle products',
    columns: 'sku, title — plus delete, active, alias, asin, key_features, attribute_* columns' },
  { value: 'listings',                 label: 'Listings',               icon: '🏪', desc: 'Create or update marketplace listings' },
  { value: 'prices',                   label: 'Price File',             icon: '💷', desc: 'Update prices across channels' },
  { value: 'inventory_basic',          label: 'Basic Inventory',        icon: '📊', desc: 'Set stock levels by SKU (overwrites existing quantity)' },
  { value: 'inventory_delta',          label: 'Stock Delta',            icon: '🔄', desc: 'Add or subtract from existing stock levels (positive = add, negative = remove)' },
  { value: 'inventory_advanced',       label: 'Advanced Inventory',     icon: '🗃️',  desc: 'Set stock per SKU, warehouse & location' },
  { value: 'orders',                   label: 'Orders',                 icon: '📋', desc: 'Import orders from a CSV', columns: 'order_reference, sku, quantity' },
  { value: 'binrack_zone',             label: 'Binrack Zone',           icon: '🗂️',  desc: 'Assign zones to binracks by name', columns: 'binrack_name, zone_name' },
  { value: 'binrack_create_update',    label: 'Binrack Create/Update',  icon: '📍', desc: 'Create or update bin rack locations', columns: 'name, barcode, binrack_type, zone_name, aisle, section, level, bin_number, capacity' },
  { value: 'binrack_item_restriction', label: 'Binrack Restrictions',   icon: '🚫', desc: 'Restrict binracks to specific SKUs', columns: 'binrack_name, sku' },
  { value: 'binrack_storage_group',    label: 'Binrack Storage Group',  icon: '📁', desc: 'Assign storage groups to binracks', columns: 'binrack_name, storage_group_name' },
  { value: 'stock_migration',          label: 'Stock Migration',        icon: '⚠️', desc: 'Destructive stock overwrite — use with caution', columns: 'sku, warehouse_id, binrack_name, quantity' },
];

const EXPORT_TYPES: { value: ExportType; label: string; icon: string; desc: string; simple?: boolean }[] = [
  { value: 'products',           label: 'Products',           icon: '📦', desc: 'All products with variants and bundles (CSV or XLSX)' },
  { value: 'listings',           label: 'Listings',           icon: '🏪', desc: 'All marketplace listings' },
  { value: 'prices',             label: 'Price File',         icon: '💷', desc: 'Products with all channel prices' },
  { value: 'inventory_basic',    label: 'Basic Inventory',    icon: '📊', desc: 'SKU and total stock quantity' },
  { value: 'inventory_advanced', label: 'Advanced Inventory', icon: '🗃️', desc: 'Stock per location and warehouse' },
  { value: 'rma',                label: 'RMAs',               icon: '↩️', desc: 'All returns / RMA records', simple: true },
  { value: 'purchase_orders',    label: 'Purchase Orders',    icon: '🛒', desc: 'All purchase orders', simple: true },
  { value: 'shipments',          label: 'Shipments',          icon: '📫', desc: 'All shipment records', simple: true },
];

// attrLabel converts an attribute column name to a human-readable display label.
function attrLabel(col: string): string {
  return col
    .replace(/^attribute_/, '')
    .replace(/^variant_attr_/, '')
    .replace(/_/g, ' ')
    .replace(/\b\w/g, c => c.toUpperCase());
}

const DEFAULT_FILE_SETTINGS: FileSettings = {
  delimiter: ',', encoding: 'utf-8', hasHeaderRow: true, escapeChar: '',
};

// ─── Main Page ─────────────────────────────────────────────────────────────────

export default function ImportExport() {
  const [activeTab, setActiveTab] = useState<'imports' | 'export' | 'import'>('import');
  return (
    <div className="ie-page">
      <div className="ie-header">
        <div className="ie-header-left">
          <h1>Import <span className="ie-slash">/</span> Export</h1>
          <p className="ie-subtitle">Bulk data management for products, listings, pricing, and inventory</p>
        </div>
      </div>
      <div className="ie-tabs">
        <button className={`ie-tab ${activeTab === 'import' ? 'active' : ''}`} onClick={() => setActiveTab('import')}>
          <span className="ie-tab-icon">⬇️</span> Import
        </button>
        <button className={`ie-tab ${activeTab === 'export' ? 'active' : ''}`} onClick={() => setActiveTab('export')}>
          <span className="ie-tab-icon">⬆️</span> Export
        </button>
        <button className={`ie-tab ${activeTab === 'imports' ? 'active' : ''}`} onClick={() => setActiveTab('imports')}>
          <span className="ie-tab-icon">📥</span> Import Activity
        </button>
      </div>
      <div className="ie-body">
        {activeTab === 'imports' ? <ImportActivityPanel /> :
         activeTab === 'export'  ? <ExportPanel /> :
                                   <ImportPanel />}
      </div>
    </div>
  );
}

// ─── Export Panel ──────────────────────────────────────────────────────────────

function ExportPanel() {
  const [selectedType, setSelectedType] = useState<ExportType>('products');
  const [format, setFormat] = useState<FileFormat>('csv');
  const [exporting, setExporting] = useState(false);
  const [error, setError] = useState('');

  const selected = EXPORT_TYPES.find(t => t.value === selectedType);
  const isSimple = selected?.simple === true;
  // Products export goes directly to /pim/export (synchronous download, not queued)
  const isPIMProducts = selectedType === 'products';

  const [jobs, setJobs] = useState<any[]>([]);
  const [loadingJobs, setLoadingJobs] = useState(false);

  const loadJobs = async () => {
    setLoadingJobs(true);
    try {
      const res = await api('/export/jobs');
      const data = await res.json();
      setJobs(data.jobs || []);
    } catch {} finally {
      setLoadingJobs(false);
    }
  };

  useEffect(() => { loadJobs(); }, []);

  const doExport = async () => {
    setExporting(true);
    setError('');
    try {
      let res: Response;
      if (isPIMProducts) {
        // Products — direct synchronous download from PIM export handler
        // Supports CSV and XLSX. File is built inline (not queued).
        res = await api(`/pim/export?format=${format}`);
        if (!res.ok) {
          const j = await res.json().catch(() => ({}));
          throw new Error((j as any).error || `Export failed (${res.status})`);
        }
        const blob = await res.blob();
        const cd = res.headers.get('content-disposition') || '';
        const fnMatch = cd.match(/filename=([^;]+)/);
        triggerDownload(blob, fnMatch ? fnMatch[1].trim() : `products.${format}`);
      } else if (isSimple) {
        // Simple GET exports (RMA, PO, Shipments) — small files, direct download
        res = await api(`/export/${selectedType === 'purchase_orders' ? 'purchase-orders' : selectedType}`);
        if (!res.ok) {
          const j = await res.json().catch(() => ({}));
          throw new Error((j as any).error || `Export failed (${res.status})`);
        }
        const blob = await res.blob();
        const cd = res.headers.get('content-disposition') || '';
        const fnMatch = cd.match(/filename=([^;]+)/);
        triggerDownload(blob, fnMatch ? fnMatch[1].trim() : `export_${selectedType}.csv`);
      } else {
        // Large exports — queue as background job
        res = await api('/export/queue', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ type: selectedType, format }),
        });
        if (!res.ok) {
          const j = await res.json().catch(() => ({}));
          throw new Error((j as any).error || `Export failed (${res.status})`);
        }
        // Reload jobs list so new job appears immediately
        await loadJobs();
        // Poll every 5 seconds while any job is still building or queued.
        const poll = setInterval(async () => {
          const res = await api('/export/jobs');
          const data = await res.json();
          const jobs: any[] = data.jobs || [];
          setJobs(jobs);
          const stillActive = jobs.some(j => j.status === 'building' || j.status === 'queued');
          if (!stillActive) clearInterval(poll);
        }, 5000);
      }
    } catch (e: any) {
      setError(e.message);
    } finally {
      setExporting(false);
    }
  };

  return (
    <div className="ie-export-panel">
      <div className="ie-section-title">Choose Export Type</div>
      <div className="ie-type-grid">
        {EXPORT_TYPES.map(t => (
          <button key={t.value} className={`ie-type-card ${selectedType === t.value ? 'selected' : ''}`}
            onClick={() => setSelectedType(t.value)}>
            <span className="ie-type-icon">{t.icon}</span>
            <span className="ie-type-name">{t.label}</span>
            <span className="ie-type-desc">{t.desc}</span>
          </button>
        ))}
      </div>

      {/* Options + Export button in same block */}
      <div className="ie-export-options">
        {!isSimple && (
          <div className="ie-format-row">
            <span className="ie-label">File format</span>
            <div className="ie-format-btns">
              <button className={`ie-fmt-btn ${format === 'csv' ? 'active' : ''}`} onClick={() => setFormat('csv')}>CSV</button>
              <button className={`ie-fmt-btn ${format === 'xlsx' ? 'active' : ''}`} onClick={() => setFormat('xlsx')}>XLSX (Excel)</button>
            </div>
          </div>
        )}
        {error && <div className="ie-error-banner">⚠️ {error}</div>}
        <div className="ie-export-action">
          <button className="ie-btn-primary" onClick={doExport} disabled={exporting}>
            {exporting
              ? <><span className="ie-spinner" /> {isPIMProducts ? 'Preparing…' : 'Queuing…'}</>
              : <>{selected?.icon} {isPIMProducts ? `Download ${format.toUpperCase()}` : isSimple ? 'Export' : 'Queue Export'} {selected?.label}{!isSimple && !isPIMProducts ? ` as ${format.toUpperCase()}` : ''}</>}
          </button>
        </div>
      </div>

      {/* Export Jobs Queue — shown for non-simple, non-PIM exports */}
      {!isSimple && !isPIMProducts && (
        <div className="ie-jobs-section">
          <div className="ie-jobs-header">
            <span className="ie-jobs-title">📋 Export Queue</span>
            <button className="ie-btn-ghost ie-btn-sm" onClick={loadJobs} disabled={loadingJobs}>↻ Refresh</button>
          </div>
          {jobs.length === 0 ? (
            <div className="ie-jobs-empty">No exports yet. Click "Queue Export" above — the file is built in the background and will appear here when ready to download.</div>
          ) : (
            <table className="ie-jobs-table">
              <thead><tr><th>Type</th><th>Format</th><th>Rows</th><th>Status</th><th>Queued At</th><th>Download</th></tr></thead>
              <tbody>
                {jobs.map(job => (
                  <tr key={job.job_id}>
                    <td style={{textTransform:'capitalize'}}>{job.type?.replace(/_/g,' ')}</td>
                    <td>{(job.format || 'csv').toUpperCase()}</td>
                    <td>{job.row_count ? job.row_count.toLocaleString() : '—'}</td>
                    <td>
                      <span className={`ie-job-badge ie-job-${job.status}`}>
                        {job.status === 'ready'
                          ? '✅ Ready'
                          : job.status === 'building'
                          ? (job.progress_pct > 0 ? `⏳ Building… ${job.progress_pct}%` : '⏳ Building…')
                          : job.status === 'queued'
                          ? '🕐 Queued'
                          : job.status === 'failed'
                          ? '❌ Failed'
                          : job.status}
                      </span>
                      {job.error && <div className="ie-job-error">{job.error}</div>}
                    </td>
                    <td style={{fontSize:'11px',color:'#888'}}>{job.created_at ? new Date(job.created_at).toLocaleString() : ''}</td>
                    <td>{job.status === 'ready' && job.download_url
                      ? <a href={job.download_url} download className="ie-download-link">⬇ Download</a>
                      : <span style={{color:'#888'}}>—</span>}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      )}
    </div>
  );
}

// ─── Import Activity Panel ─────────────────────────────────────────────────────
// Shows all marketplace product import jobs (triggered automatically on channel
// connect, or manually from Dev Tools > Import Products).
// Shows live record counts during import — no Download button.

function ImportActivityPanel() {
  const [mpJobs, setMpJobs] = useState<MarketplaceImportJob[]>([]);
  const [credentials, setCredentials] = useState<{ credential_id: string; account_name: string; channel: string }[]>([]);
  const [loading, setLoading] = useState(true);
  const [cancellingId, setCancellingId] = useState<string | null>(null);
  const [tick, setTick] = useState(0);

  const loadAll = useCallback(async () => {
    try {
      const [jobsRes, credsRes] = await Promise.allSettled([
        api('/marketplace/import/jobs').then(r => r.json()),
        api('/marketplace/credentials').then(r => r.json()),
      ]);
      if (jobsRes.status === 'fulfilled') {
        setMpJobs(jobsRes.value.data || jobsRes.value.jobs || []);
      }
      if (credsRes.status === 'fulfilled') {
        setCredentials(credsRes.value.data || credsRes.value.credentials || []);
      }
    } catch {}
    setLoading(false);
  }, []);

  useEffect(() => { loadAll(); }, [loadAll]);

  // Tick every second so elapsed timers update without re-fetching
  useEffect(() => {
    const t = setInterval(() => setTick(n => n + 1), 1000);
    return () => clearInterval(t);
  }, []);

  // Poll every 5 s while any job is active
  useEffect(() => {
    const hasActive = mpJobs.some(j => j.status === 'running' || j.status === 'pending');
    if (!hasActive) return;
    const t = setInterval(loadAll, 5000);
    return () => clearInterval(t);
  }, [mpJobs, loadAll]);

  async function cancelJob(jobId: string) {
    if (!confirm('Cancel this import job? Products already imported will remain.')) return;
    setCancellingId(jobId);
    try {
      await api(`/marketplace/import/jobs/${jobId}/cancel`, { method: 'POST' });
      await loadAll();
    } catch {}
    setCancellingId(null);
  }

  function formatElapsed(startedAt?: string, createdAt?: string): string {
    const startMs = new Date(startedAt || createdAt || '').getTime();
    if (!startMs) return '';
    const sec = Math.floor((Date.now() - startMs) / 1000);
    const m = Math.floor(sec / 60), s = sec % 60;
    return m > 0 ? `${m}m ${s}s` : `${s}s`;
  }

  function formatDate(d?: string): string {
    if (!d) return '—';
    return new Date(d).toLocaleString('en-GB', { day: '2-digit', month: 'short', hour: '2-digit', minute: '2-digit' });
  }

  if (loading) {
    return <div style={{ padding: 40, textAlign: 'center', color: 'var(--text-muted)' }}>Loading import jobs…</div>;
  }

  return (
    <div className="ie-section">
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 16 }}>
        <div>
          <div className="ie-section-title" style={{ marginBottom: 2 }}>Import Activity</div>
          <p style={{ fontSize: 12, color: 'var(--text-muted)', margin: 0 }}>
            Product imports are triggered automatically when you connect a marketplace account.
            Progress updates live — no refresh needed while an import is running.
          </p>
        </div>
        <button className="ie-btn-ghost ie-btn-sm" onClick={loadAll}>↻ Refresh</button>
      </div>

      {mpJobs.length === 0 ? (
        <div className="ie-jobs-empty">
          No import jobs yet. Connect a marketplace account to start your first import automatically,
          or use Dev Tools › Import Products to start one manually.
        </div>
      ) : (
        <div className="ie-table-wrap">
          <table className="ie-table ie-imports-table">
            <thead>
              <tr>
                <th>Channel</th>
                <th>Status</th>
                <th>Progress</th>
                <th>Processed</th>
                <th>Imported</th>
                <th>Failed</th>
                <th>Started</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {mpJobs.map(job => {
                const isActive = job.status === 'running' || job.status === 'pending';
                const isAmazon = job.channel === 'amazon' || job.channel === 'amazonnew';
                const enrichOn = isAmazon;
                const enrichDone = (job.enriched_items || 0) + (job.enrich_skipped_items || 0) + (job.enrich_failed_items || 0);
                const inEnrichPhase = enrichOn && (job.enrich_total_items > 0 || enrichDone > 0);
                const enrichTotal = job.enrich_total_items || job.total_items;
                const displayDone = inEnrichPhase ? enrichDone : (enrichOn ? 0 : job.processed_items);
                const displayTotal = inEnrichPhase ? enrichTotal : Math.max(job.total_items, job.processed_items);
                const isCounting = isActive && job.total_items === 0;
                const pct = displayTotal > 0 ? Math.min(Math.round((displayDone / displayTotal) * 100), 100) : 0;

                const barColor = job.status === 'failed'    ? '#ef4444'
                               : job.status === 'completed' ? '#22c55e'
                               : job.status === 'cancelled' ? '#f59e0b'
                               : '#3b82f6';

                const processedText = isCounting
                  ? 'Starting…'
                  : displayTotal > 0
                  ? `${displayDone.toLocaleString()} / ${displayTotal.toLocaleString()}${inEnrichPhase && isActive ? ' (enriching)' : ''}`
                  : isActive ? '—' : `${(job.processed_items || 0).toLocaleString()}`;

                const cred = credentials.find(c => c.credential_id === job.channel_account_id);
                const accountName = cred?.account_name || job.channel;
                const elapsed = formatElapsed(job.started_at, job.created_at);

                const statusBadgeClass = job.status === 'completed' ? 'ie-badge-success'
                  : job.status === 'failed'    ? 'ie-badge-danger'
                  : job.status === 'cancelled' ? 'ie-badge-warn'
                  : isActive                   ? 'ie-badge-info'
                  : 'ie-badge-warn';

                let subMsg: string | null = null;
                if (job.status_message && !job.status_message.startsWith('Queued')) {
                  subMsg = job.status_message;
                } else if (inEnrichPhase && isActive) {
                  subMsg = `Enriching… ${(job.enriched_items || 0).toLocaleString()} / ${Math.max(job.enrich_total_items, job.enriched_items || 0).toLocaleString()}`;
                }

                return (
                  <tr key={job.job_id} style={{ cursor: 'pointer' }}
                      onClick={() => window.location.href = `/marketplace/import/${job.job_id}`}>
                    <td>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                        <span style={{ fontSize: 18 }}>{adapterEmoji[job.channel] || '🌐'}</span>
                        <div>
                          <div style={{ fontWeight: 600, fontSize: 13 }}>{accountName}</div>
                          <div style={{ fontSize: 11, color: 'var(--text-muted)', textTransform: 'capitalize' }}>
                            {job.channel} · {job.job_type === 'full_import' ? 'Full Import' : job.job_type}
                          </div>
                        </div>
                      </div>
                    </td>
                    <td>
                      <span className={`ie-job-badge ${statusBadgeClass}`}>
                        {isCounting ? 'starting' : job.status}
                      </span>
                      {isActive && elapsed && (
                        <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 3 }}>
                          {subMsg
                            ? <span title={subMsg} style={{ display: 'block', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', maxWidth: 140 }}>⏱ {elapsed} · {subMsg}</span>
                            : <span>⏱ {elapsed}</span>}
                        </div>
                      )}
                    </td>
                    <td style={{ width: 160 }}>
                      {isCounting ? (
                        <div>
                          <div style={{ height: 6, background: 'var(--bg-tertiary)', borderRadius: 3, overflow: 'hidden' }}>
                            <div style={{ height: '100%', width: '25%', background: barColor, borderRadius: 3,
                              animation: 'ie-pulse-bar 1.5s ease-in-out infinite alternate', opacity: 0.7 }} />
                          </div>
                          <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 3 }}>Starting import…</div>
                        </div>
                      ) : isActive || (!isActive && displayTotal > 0) ? (
                        <div>
                          <div style={{ height: 6, background: 'var(--bg-tertiary)', borderRadius: 3, overflow: 'hidden' }}>
                            <div style={{ height: '100%', width: `${pct}%`, background: barColor, borderRadius: 3,
                              transition: isActive ? 'width 0.4s ease' : 'none' }} />
                          </div>
                          <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 3 }}>{pct}%</div>
                        </div>
                      ) : null}
                    </td>
                    <td style={{ fontWeight: isActive ? 700 : 400, color: isActive ? 'var(--text-primary)' : 'var(--text-secondary)', fontVariantNumeric: 'tabular-nums', fontSize: 13 }}>
                      {processedText}
                    </td>
                    <td>
                      {(job.successful_items > 0 || job.status === 'completed') ? (
                        <span style={{ color: '#22c55e', fontWeight: 600 }}>
                          {job.successful_items.toLocaleString()}
                        </span>
                      ) : <span style={{ color: 'var(--text-muted)' }}>0</span>}
                    </td>
                    <td>
                      {job.failed_items > 0 ? (
                        <button
                          className="ie-link-btn ie-link-danger"
                          style={{ fontSize: 12, fontWeight: 700 }}
                          onClick={e => { e.stopPropagation(); window.location.href = `/marketplace/import/${job.job_id}#errors`; }}
                          title="View error details"
                        >
                          {job.failed_items} ⚠
                        </button>
                      ) : <span style={{ color: 'var(--text-muted)' }}>0</span>}
                    </td>
                    <td style={{ color: 'var(--text-muted)', fontSize: 12 }}>{formatDate(job.started_at || job.created_at)}</td>
                    <td style={{ textAlign: 'right' }}>
                      {isActive ? (
                        <button
                          className="ie-btn-ghost ie-btn-sm"
                          style={{ color: '#ef4444', borderColor: '#ef444440' }}
                          disabled={cancellingId === job.job_id}
                          onClick={e => { e.stopPropagation(); cancelJob(job.job_id); }}
                        >
                          {cancellingId === job.job_id ? '…' : '✕ Cancel'}
                        </button>
                      ) : job.status === 'completed' && job.match_status === 'review_required' ? (
                        <button
                          className="ie-btn-primary ie-btn-sm"
                          style={{ background: '#f59e0b', borderColor: '#f59e0b', fontSize: 11, whiteSpace: 'nowrap' }}
                          onClick={e => { e.stopPropagation(); window.location.href = `/marketplace/import/${job.job_id}/review-matches`; }}
                        >
                          ⚠ Review Matches
                        </button>
                      ) : (
                        <span style={{ color: 'var(--text-muted)', fontSize: 12 }}>—</span>
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

// ─── Import Panel ──────────────────────────────────────────────────────────────

function ImportPanel() {
  const [importType, setImportType] = useState<ImportType>('products');
  const [step, setStep] = useState<Step>('upload');
  const [file, setFile] = useState<File | null>(null);

  // File settings (Step 1b: collapsible)
  const [fileSettings, setFileSettings] = useState<FileSettings>(DEFAULT_FILE_SETTINGS);
  const [fileSettingsOpen, setFileSettingsOpen] = useState(false);

  // Column mapping (Step 2)
  const [preview, setPreview] = useState<PreviewResult | null>(null);
  const [columnMapping, setColumnMapping] = useState<Record<string, string>>({}); // targetField → fileHeader
  const [previewing, setPreviewing] = useState(false);

  // Validate (Step 3)
  const [validation, setValidation] = useState<ValidationResult | null>(null);
  const [validating, setValidating] = useState(false);
  const [confirmLocations, setConfirmLocations] = useState(false);

  // Apply (Step 4)
  const [applying, setApplying] = useState(false);
  const [jobId, setJobId] = useState('');
  const [jobStatus, setJobStatus] = useState<ImportJob | null>(null);

  const [error, setError] = useState('');
  const fileInputRef = useRef<HTMLInputElement>(null);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const resetFlow = () => {
    setStep('upload');
    setFile(null);
    setPreview(null);
    setColumnMapping({});
    setValidation(null);
    setConfirmLocations(false);
    setJobId('');
    setJobStatus(null);
    setError('');
    if (pollRef.current) clearInterval(pollRef.current);
  };

  // Reset flow when import type changes
  useEffect(() => { resetFlow(); }, [importType]);

  useEffect(() => {
    if (!jobId) return;
    const prefix = importPrefix(importType);
    const poll = async () => {
      try {
        const res = await api(`${prefix}/status/${jobId}`);
        if (!res.ok) return;
        const job: ImportJob = await res.json();
        setJobStatus(job);
        if (job.status === 'done' || job.status === 'failed') {
          if (pollRef.current) clearInterval(pollRef.current);
          setStep('done');
        }
      } catch {}
    };
    poll();
    pollRef.current = setInterval(poll, 1500);
    return () => { if (pollRef.current) clearInterval(pollRef.current); };
  }, [jobId]);

  const handleFileDrop = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    const f = e.dataTransfer.files[0];
    if (f) { setFile(f); setError(''); }
  }, []);

  const handleFileInput = (e: React.ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0];
    if (f) { setFile(f); setError(''); }
  };

  // Build FormData — PIM endpoint doesn't need 'type'; legacy endpoint does
  const buildFormData = (extraFields?: Record<string, string>) => {
    const fd = new FormData();
    if (file) fd.append('file', file);
    if (!isPIMImport(importType)) {
      // Legacy /import/* endpoints require a 'type' field
      fd.append('type', importType);
    }
    fd.append('delimiter', fileSettings.delimiter);
    fd.append('encoding', fileSettings.encoding);
    fd.append('has_header_row', fileSettings.hasHeaderRow ? 'true' : 'false');
    fd.append('escape_char', fileSettings.escapeChar);
    if (Object.keys(columnMapping).length > 0) {
      fd.append('column_mapping', JSON.stringify(columnMapping));
    }
    if (extraFields) {
      for (const [k, v] of Object.entries(extraFields)) fd.append(k, v);
    }
    return fd;
  };

  // Step 1 → 2: Preview & column mapping
  const doPreview = async () => {
    if (!file) return;
    setPreviewing(true);
    setError('');
    try {
      const fd = buildFormData();
      const prefix = importPrefix(importType);
      const res = await api(`${prefix}/preview`, { method: 'POST', body: fd });
      const data = await res.json() as any;
      if (!res.ok) throw new Error(data.error || 'Preview failed');
      setPreview(data as PreviewResult);
      setColumnMapping(data.auto_mapping || {});
      setStep('column_mapping');
    } catch (e: any) {
      setError(e.message || 'Failed to fetch — check the file format and try again');
    } finally {
      setPreviewing(false);
    }
  };

  // Step 2 → 3: Validate
  const doValidate = async () => {
    if (!file) return;
    setValidating(true);
    setError('');
    try {
      const fd = buildFormData();
      const prefix = importPrefix(importType);
      const res = await api(`${prefix}/validate`, { method: 'POST', body: fd });
      const data = await res.json() as any;
      if (!res.ok) throw new Error(data.error || 'Validation failed');
      setValidation(data as ValidationResult);
      setStep('validate');
    } catch (e: any) {
      setError(e.message || 'Validation request failed');
    } finally {
      setValidating(false);
    }
  };

  // Step 3 → 4: Apply
  const doApply = async () => {
    if (!file) return;
    setApplying(true);
    setError('');
    try {
      const fd = buildFormData(confirmLocations ? { confirm_unknown_locations: 'true' } : undefined);
      const prefix = importPrefix(importType);
      const res = await api(`${prefix}/apply`, { method: 'POST', body: fd });
      const data = await res.json() as any;
      if (!res.ok) throw new Error(data.error || 'Apply failed');
      setJobId(data.job_id);
      setStep('apply');
    } catch (e: any) {
      setError(e.message || 'Import request failed');
    } finally {
      setApplying(false);
    }
  };

  const downloadTemplate = async () => {
    try {
      const res = await api(templateUrl(importType));
      if (!res.ok) throw new Error('Failed to download template');
      const blob = await res.blob();
      triggerDownload(blob, templateFilename(importType));
    } catch (e: any) {
      setError(e.message);
    }
  };

  const hasBlockingErrors = validation && validation.error_count > 0;
  const hasUnknownLocations = validation && (validation.unknown_locations?.length || 0) > 0;
  const canProceed = validation && !hasBlockingErrors && (!hasUnknownLocations || confirmLocations);
  const selectedImport = IMPORT_TYPES.find(t => t.value === importType);

  // Step label map
  const stepLabels: Record<Step, string> = {
    upload: 'Upload',
    file_settings: 'File Settings',
    column_mapping: 'Column Mapping',
    validate: 'Validate',
    apply: 'Applying',
    done: 'Results',
  };
  const stepOrder: Step[] = ['upload', 'column_mapping', 'validate', 'apply', 'done'];

  return (
    <div className="ie-import-panel">
      {/* Type Selector */}
      <div className="ie-section-title">Choose Import Type</div>
      <div className="ie-type-grid">
        {IMPORT_TYPES.map(t => (
          <button key={t.value} className={`ie-type-card ${importType === t.value ? 'selected' : ''}`}
            onClick={() => setImportType(t.value)}>
            <span className="ie-type-icon">{t.icon}</span>
            <span className="ie-type-name">{t.label}</span>
            <span className="ie-type-desc">{t.desc}</span>
          </button>
        ))}
      </div>

      {/* Column format hint */}
      {selectedImport?.columns && (
        <div style={{ background: 'rgba(124,58,237,0.07)', border: '1px solid rgba(124,58,237,0.2)', borderRadius: 9, padding: '12px 18px', marginBottom: 16, fontSize: 13 }}>
          <span style={{ fontWeight: 700, color: '#7c3aed', marginRight: 8 }}>📋 Key columns:</span>
          <code style={{ fontFamily: 'monospace', color: 'var(--text-primary)', fontSize: 12 }}>{selectedImport.columns}</code>
          {selectedImport.value === 'stock_migration' && (
            <div style={{ marginTop: 8, color: '#ef4444', fontWeight: 600, fontSize: 12 }}>
              ⚠️ This import will <strong>destructively overwrite</strong> existing stock quantities. Ensure you have a backup.
            </div>
          )}
        </div>
      )}

      {/* Step indicator */}
      <div className="ie-steps">
        {stepOrder.map((s, i) => (
          <div key={s} className={`ie-step ${step === s ? 'current' : stepsAhead(step, s, stepOrder) ? '' : 'done'}`}>
            <span className="ie-step-num">{!stepsAhead(step, s, stepOrder) && step !== s ? '✓' : i + 1}</span>
            <span className="ie-step-label">{stepLabels[s]}</span>
          </div>
        ))}
      </div>

      {error && <div className="ie-error-banner">⚠️ {error}</div>}

      {/* ── Step 1: Upload ── */}
      {step === 'upload' && (
        <div className="ie-step-panel">
          <div className={`ie-dropzone ${file ? 'has-file' : ''}`}
            onDragOver={e => e.preventDefault()} onDrop={handleFileDrop}
            onClick={() => fileInputRef.current?.click()}>
            <input ref={fileInputRef} type="file" accept=".csv,.xlsx" style={{ display: 'none' }} onChange={handleFileInput} />
            {file ? (
              <div className="ie-file-selected">
                <span className="ie-file-icon">📄</span>
                <span className="ie-file-name">{file.name}</span>
                <span className="ie-file-size">{(file.size / 1024).toFixed(1)} KB</span>
              </div>
            ) : (
              <>
                <span className="ie-dz-icon">📁</span>
                <span className="ie-dz-text">Drop a CSV or XLSX file here, or <strong>click to browse</strong></span>
                <span className="ie-dz-hint">CSV or XLSX — configure parsing options below</span>
              </>
            )}
          </div>

          {/* ── File Settings (collapsible) ── */}
          <div className="ie-file-settings">
            <button className="ie-file-settings-toggle" onClick={() => setFileSettingsOpen(o => !o)}>
              ⚙️ File Settings {fileSettingsOpen ? '▲' : '▼'}
            </button>
            {fileSettingsOpen && (
              <div className="ie-file-settings-body">
                <div className="ie-fs-row">
                  <label className="ie-fs-label">Delimiter</label>
                  <select className="ie-fs-select" value={fileSettings.delimiter}
                    onChange={e => setFileSettings(s => ({ ...s, delimiter: e.target.value }))}>
                    <option value=",">, (comma)</option>
                    <option value="tab">⇥  (tab)</option>
                    <option value=";">; (semicolon)</option>
                    <option value="|">| (pipe)</option>
                  </select>
                </div>
                <div className="ie-fs-row">
                  <label className="ie-fs-label">Encoding</label>
                  <select className="ie-fs-select" value={fileSettings.encoding}
                    onChange={e => setFileSettings(s => ({ ...s, encoding: e.target.value }))}>
                    <option value="utf-8">UTF-8 (default)</option>
                    <option value="latin-1">Latin-1 / ISO-8859-1</option>
                  </select>
                </div>
                <div className="ie-fs-row">
                  <label className="ie-fs-label">Has Header Row</label>
                  <input type="checkbox" checked={fileSettings.hasHeaderRow}
                    onChange={e => setFileSettings(s => ({ ...s, hasHeaderRow: e.target.checked }))} />
                </div>
                <div className="ie-fs-row">
                  <label className="ie-fs-label">Escape Character</label>
                  <select className="ie-fs-select" value={fileSettings.escapeChar}
                    onChange={e => setFileSettings(s => ({ ...s, escapeChar: e.target.value }))}>
                    <option value="">Default (standard CSV quoting)</option>
                    <option value="\\">\ (backslash)</option>
                  </select>
                </div>
              </div>
            )}
          </div>

          <div className="ie-upload-footer">
            <button className="ie-btn-ghost" onClick={downloadTemplate}>
              ⬇ Download {selectedImport?.label} template
            </button>
            <button className="ie-btn-primary" onClick={doPreview} disabled={!file || previewing}>
              {previewing ? <><span className="ie-spinner" /> Loading…</> : 'Map Columns →'}
            </button>
          </div>
        </div>
      )}

      {/* ── Step 2: Column Mapping ── */}
      {step === 'column_mapping' && preview && (
        <div className="ie-step-panel">
          <div className="ie-section-title">Column Mapping</div>
          <p className="ie-mapping-hint">
            Map each required field to a column in your file. Optional fields can be left unmapped.
          </p>

          {/* Preview table */}
          {preview.preview_rows.length > 0 && (
            <div className="ie-table-wrap ie-preview-table-wrap">
              <table className="ie-table">
                <thead>
                  <tr>{preview.headers.map(h => (
                    <th key={h} title={h}>{attrLabel(h)}</th>
                  ))}</tr>
                </thead>
                <tbody>
                  {preview.preview_rows.map((row, ri) => (
                    <tr key={ri}>{preview.headers.map((_, ci) => <td key={ci}>{row[ci] ?? ''}</td>)}</tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          {/* Mapping rows */}
          <div className="ie-mapping-grid">
            <div className="ie-mapping-header-row">
              <span>Target Field</span>
              <span>Source Column in Your File</span>
            </div>
            {[
              ...preview.required_fields.map(f => ({ field: f, required: true })),
              ...preview.optional_fields.map(f => ({ field: f, required: false })),
            ].map(({ field, required }) => (
              <div key={field} className={`ie-mapping-row ${required ? 'required' : ''}`}>
                <span className="ie-mapping-field">
                  {attrLabel(field)}
                  {(field.startsWith('attribute_') || field.startsWith('variant_attr_')) && (
                    <code style={{ marginLeft: 6, fontSize: 10, opacity: 0.6, fontFamily: 'monospace' }}>
                      {field}
                    </code>
                  )}
                  {required && <span className="ie-required-badge">required</span>}
                </span>
                <select className="ie-mapping-select"
                  value={columnMapping[field] || ''}
                  onChange={e => setColumnMapping(m => ({ ...m, [field]: e.target.value }))}>
                  <option value="">— not mapped —</option>
                  {preview.headers.map(h => <option key={h} value={h}>{h}</option>)}
                </select>
              </div>
            ))}
          </div>

          <div className="ie-validate-footer">
            <button className="ie-btn-ghost" onClick={resetFlow}>← Re-upload</button>
            <button className="ie-btn-primary" onClick={doValidate} disabled={validating}>
              {validating ? <><span className="ie-spinner" /> Validating…</> : 'Validate →'}
            </button>
          </div>
        </div>
      )}

      {/* ── Step 3: Validate ── */}
      {step === 'validate' && validation && (
        <div className="ie-step-panel">
          <div className="ie-val-summary">
            <div className="ie-val-stat ie-stat-total"><span className="ie-stat-num">{validation.total_rows}</span><span className="ie-stat-lbl">Total rows</span></div>
            <div className="ie-val-stat ie-stat-ok"><span className="ie-stat-num">✅ {validation.valid_rows}</span><span className="ie-stat-lbl">Valid</span></div>
            {validation.create_count > 0 && <div className="ie-val-stat ie-stat-create"><span className="ie-stat-num">+ {validation.create_count}</span><span className="ie-stat-lbl">To create</span></div>}
            {validation.update_count > 0 && <div className="ie-val-stat ie-stat-update"><span className="ie-stat-num">↑ {validation.update_count}</span><span className="ie-stat-lbl">To update</span></div>}
            {validation.error_count > 0 && <div className="ie-val-stat ie-stat-err"><span className="ie-stat-num">❌ {validation.error_count}</span><span className="ie-stat-lbl">Errors</span></div>}
            {validation.warning_count > 0 && <div className="ie-val-stat ie-stat-warn"><span className="ie-stat-num">⚠️ {validation.warning_count}</span><span className="ie-stat-lbl">Warnings</span></div>}
          </div>

          {validation.errors.length > 0 && (
            <div className="ie-issues-section">
              <div className="ie-issues-title ie-err-title">❌ Errors — fix and re-upload before proceeding</div>
              <div className="ie-table-wrap">
                <table className="ie-table">
                  <thead><tr><th>Row</th><th>Column</th><th>Issue</th></tr></thead>
                  <tbody>
                    {validation.errors.slice(0, 50).map((e, i) => (
                      <tr key={i}>
                        <td>Row {e.row}</td>
                        <td><code title={e.column}>{attrLabel(e.column)}</code></td>
                        <td>{e.message}</td>
                      </tr>
                    ))}
                    {validation.errors.length > 50 && <tr><td colSpan={3} className="ie-more-rows">…and {validation.errors.length - 50} more errors</td></tr>}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {validation.warnings.length > 0 && (
            <div className="ie-issues-section">
              <div className="ie-issues-title ie-warn-title">⚠️ Warnings</div>
              <div className="ie-table-wrap">
                <table className="ie-table">
                  <thead><tr><th>Row</th><th>Column</th><th>Issue</th></tr></thead>
                  <tbody>
                    {validation.warnings.slice(0, 20).map((w, i) => (
                      <tr key={i} className="ie-warn-row">
                        <td>Row {w.row}</td>
                        <td><code title={w.column}>{attrLabel(w.column)}</code></td>
                        <td>{w.message}</td>
                      </tr>
                    ))}
                    {validation.warnings.length > 20 && <tr><td colSpan={3} className="ie-more-rows">…and {validation.warnings.length - 20} more warnings</td></tr>}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {hasUnknownLocations && (
            <div className="ie-location-confirm">
              <div className="ie-location-warn-header">⚠️ {validation.unknown_locations!.length} warehouse location{validation.unknown_locations!.length > 1 ? 's' : ''} not found and will be auto-created:</div>
              <ul className="ie-location-list">
                {validation.unknown_locations!.map(loc => <li key={loc}>• {loc}</li>)}
              </ul>
              <label className="ie-checkbox-row">
                <input type="checkbox" checked={confirmLocations} onChange={e => setConfirmLocations(e.target.checked)} />
                <span>I confirm — proceed and create these locations</span>
              </label>
            </div>
          )}

          <div className="ie-validate-footer">
            <button className="ie-btn-ghost" onClick={() => setStep('column_mapping')}>← Back to mapping</button>
            {hasBlockingErrors ? (
              <span className="ie-err-msg">Fix {validation.error_count} error{validation.error_count > 1 ? 's' : ''} before proceeding</span>
            ) : (
              <button className="ie-btn-primary" onClick={doApply} disabled={!canProceed || applying}>
                {applying ? <><span className="ie-spinner" /> Applying…</> : `Apply import (${validation.valid_rows} rows) →`}
              </button>
            )}
          </div>
        </div>
      )}

      {/* ── Step 4: Apply / Progress ── */}
      {step === 'apply' && (
        <div className="ie-step-panel ie-progress-panel">
          <div className="ie-progress-title">⏳ Processing import…</div>
          {jobStatus && (
            <>
              <div className="ie-progress-bar-wrap">
                <div className="ie-progress-bar"
                  style={{ width: jobStatus.total_rows > 0 ? `${Math.round((jobStatus.processed_rows / jobStatus.total_rows) * 100)}%` : '10%' }} />
              </div>
              <div className="ie-progress-text">{jobStatus.processed_rows} / {jobStatus.total_rows} rows processed</div>
            </>
          )}
        </div>
      )}

      {/* ── Step 5: Done ── */}
      {step === 'done' && jobStatus && (
        <div className="ie-step-panel ie-done-panel">
          <div className={`ie-done-icon ${jobStatus.failed_count === 0 ? 'success' : 'partial'}`}>
            {jobStatus.failed_count === 0 ? '✅' : '⚠️'}
          </div>
          <div className="ie-done-title">{jobStatus.status === 'done' ? 'Import complete' : 'Import failed'}</div>
          <div className="ie-done-stats">
            {jobStatus.created_count > 0 && <div className="ie-done-stat"><strong>{jobStatus.created_count}</strong> created</div>}
            {jobStatus.updated_count > 0 && <div className="ie-done-stat"><strong>{jobStatus.updated_count}</strong> updated</div>}
            {jobStatus.failed_count > 0 && <div className="ie-done-stat ie-stat-failed"><strong>{jobStatus.failed_count}</strong> failed</div>}
          </div>
          {jobStatus.error_report && jobStatus.error_report.length > 0 && (
            <div className="ie-done-errors">
              <div className="ie-issues-title ie-err-title">Failed rows</div>
              <div className="ie-table-wrap">
                <table className="ie-table">
                  <thead><tr><th>Row</th><th>Column</th><th>Error</th></tr></thead>
                  <tbody>
                    {jobStatus.error_report.slice(0, 20).map((e, i) => (
                      <tr key={i}><td>Row {e.row}</td><td><code>{e.column}</code></td><td>{e.message}</td></tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}
          <div className="ie-done-actions">
            <button className="ie-btn-primary" onClick={resetFlow}>Start another import</button>
          </div>
        </div>
      )}

      <ImportHistory importType={importType} />
    </div>
  );
}

// ─── Import History ────────────────────────────────────────────────────────────

function ImportHistory({ importType }: { importType: ImportType }) {
  const [jobs, setJobs] = useState<ImportJob[]>([]);
  const [loading, setLoading] = useState(true);
  const [deleting, setDeleting] = useState<string | null>(null);
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null);

  const prefix = importPrefix(importType);

  const fetchJobs = () => {
    api(`${prefix}/history`)
      .then(r => r.json())
      .then((d: any) => setJobs(d.jobs || []))
      .catch(() => {})
      .finally(() => setLoading(false));
  };

  useEffect(() => { fetchJobs(); }, [importType]);

  const handleDelete = async (jobId: string) => {
    setDeleting(jobId);
    setConfirmDelete(null);
    try {
      const res = await api(`${prefix}/jobs/${jobId}`, { method: 'DELETE' });
      if (res.ok) {
        setJobs(prev => prev.filter(j => j.job_id !== jobId));
      }
    } catch {}
    finally { setDeleting(null); }
  };

  if (loading || jobs.length === 0) return null;

  return (
    <div className="ie-history">
      <div className="ie-section-title">Import History</div>
      <div className="ie-table-wrap">
        <table className="ie-table ie-history-table">
          <thead>
            <tr>
              <th>Type</th><th>File</th><th>Date</th><th>Rows</th>
              <th>Created</th><th>Updated</th><th>Failed</th><th>Status</th><th></th>
            </tr>
          </thead>
          <tbody>
            {jobs.map(j => (
              <tr key={j.job_id}>
                <td><span className="ie-type-badge">{j.import_type}</span></td>
                <td className="ie-filename">{j.filename}</td>
                <td>{new Date(j.created_at).toLocaleString()}</td>
                <td>{j.total_rows}</td>
                <td>{j.created_count}</td>
                <td>{j.updated_count}</td>
                <td>{j.failed_count > 0 ? <span className="ie-failed-num">{j.failed_count}</span> : 0}</td>
                <td><span className={`ie-status-badge ie-status-${j.status}`}>{j.status}</span></td>
                <td>
                  {confirmDelete === j.job_id ? (
                    <span className="ie-confirm-delete">
                      Delete?{' '}
                      <button className="ie-link-btn ie-link-danger" onClick={() => handleDelete(j.job_id)} disabled={deleting === j.job_id}>
                        {deleting === j.job_id ? '…' : 'Yes'}
                      </button>
                      {' / '}
                      <button className="ie-link-btn" onClick={() => setConfirmDelete(null)}>No</button>
                    </span>
                  ) : (
                    <button className="ie-icon-btn ie-trash-btn" title="Delete job" onClick={() => setConfirmDelete(j.job_id)}>
                      🗑️
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

// ─── Utilities ─────────────────────────────────────────────────────────────────

function stepsAhead(current: Step, step: Step, order: Step[]): boolean {
  return order.indexOf(current) < order.indexOf(step);
}

function triggerDownload(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url; a.download = filename;
  document.body.appendChild(a); a.click(); a.remove();
  URL.revokeObjectURL(url);
}

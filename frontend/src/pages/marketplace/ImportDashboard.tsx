import { apiFetch } from '../../services/apiFetch';
// ============================================================================
// IMPORT DASHBOARD PAGE
// ============================================================================
// Location: frontend/src/pages/marketplace/ImportDashboard.tsx

import { useState, useEffect, useRef } from 'react';
import React from 'react';
import { useNavigate } from 'react-router-dom';
import {
  importService,
  credentialService,
  ImportJob,
  MarketplaceCredential,
  StartImportRequest,
} from '../../services/marketplace-api';
import { searchService } from '../../services/api';
import { getActiveTenantId } from '../../contexts/TenantContext';

const adapterEmoji: Record<string, string> = {
  amazon: '📦', temu: '🛍️', ebay: '🏷️', shopify: '🛒', tesco: '🏪',
};

function formatDate(d?: string): string {
  if (!d) return '—';
  return new Date(d).toLocaleDateString('en-GB', { day: '2-digit', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit' });
}

// ── WMS CSV Imports Component ─────────────────────────────────────────────────
const WMS_IMPORT_TYPES = [
  { value: 'binrack_zone',             label: 'Binrack Zone Assignment',    icon: '🗂️',  desc: 'Assign zones to binracks by name',           columns: 'binrack_name, zone_name' },
  { value: 'binrack_create_update',    label: 'Binrack Create / Update',    icon: '📍', desc: 'Create or update bin rack locations',         columns: 'name, barcode, binrack_type, zone_name, aisle, section, level, bin_number, capacity' },
  { value: 'binrack_item_restriction', label: 'Binrack Item Restrictions',  icon: '🚫', desc: 'Restrict binracks to specific SKUs',          columns: 'binrack_name, sku' },
  { value: 'binrack_storage_group',    label: 'Binrack Storage Group',      icon: '📁', desc: 'Assign storage groups to binracks',           columns: 'binrack_name, storage_group_name' },
  { value: 'stock_migration',          label: 'Stock Migration ⚠️',         icon: '🔄', desc: 'Destructive stock overwrite — use with care', columns: 'sku, warehouse_id, binrack_name, quantity' },
];

function WmsCsvImports() {
  const [wmsType, setWmsType] = useState('');
  const [wmsFile, setWmsFile] = useState<File | null>(null);
  const [wmsUploading, setWmsUploading] = useState(false);
  const [wmsResult, setWmsResult] = useState<string | null>(null);
  const [wmsError, setWmsError] = useState<string | null>(null);
  const [wmsConfirmMigration, setWmsConfirmMigration] = useState(false);
  const selectedWmsType = WMS_IMPORT_TYPES.find(t => t.value === wmsType);

  const handleWmsUpload = async () => {
    if (!wmsType || !wmsFile) return;
    if (wmsType === 'stock_migration' && !wmsConfirmMigration) {
      setWmsError('You must confirm the destructive migration before proceeding.');
      return;
    }
    setWmsUploading(true); setWmsError(null); setWmsResult(null);
    try {
      const formData = new FormData();
      formData.append('file', wmsFile);
      formData.append('type', wmsType);
      if (wmsType === 'stock_migration') formData.append('confirm_migration', 'true');
      const res = await fetch(`${import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1'}/import/wms`, {
        method: 'POST',
        headers: { 'X-Tenant-Id': getActiveTenantId() },
        body: formData,
      });
      const data = await res.json();
      if (!res.ok) throw new Error(data.error || 'Upload failed');
      setWmsResult(`✅ Import queued: ${data.rows_queued || data.message || 'Success'}`);
      setWmsFile(null); setWmsType('');
    } catch (e: any) { setWmsError(e.message); }
    finally { setWmsUploading(false); }
  };

  return (
    <div style={{ marginBottom: 28, padding: '20px 24px', background: 'var(--bg-secondary)', border: '1px solid var(--border-bright)', borderRadius: 12 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 16 }}>
        <span style={{ fontSize: 20 }}>🏭</span>
        <h3 style={{ margin: 0, fontSize: 15, fontWeight: 700, color: 'var(--text-primary)' }}>WMS CSV Imports</h3>
        <span style={{ fontSize: 11, color: 'var(--text-muted)', background: 'var(--bg-tertiary)', padding: '2px 8px', borderRadius: 10, border: '1px solid var(--border-bright)' }}>Warehouse Management</span>
      </div>
      <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', alignItems: 'flex-end' }}>
        <div style={{ flex: '0 0 240px' }}>
          <label style={{ display: 'block', fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: 6 }}>Import Type</label>
          <select value={wmsType} onChange={e => { setWmsType(e.target.value); setWmsError(null); setWmsResult(null); setWmsConfirmMigration(false); }}
            style={{ width: '100%', padding: '8px 10px', background: 'var(--bg-tertiary)', border: '1px solid var(--border-bright)', borderRadius: 6, color: 'var(--text-primary)', fontSize: 13 }}>
            <option value="">Select WMS import type…</option>
            {WMS_IMPORT_TYPES.map(t => <option key={t.value} value={t.value}>{t.icon} {t.label}</option>)}
          </select>
        </div>
        <div style={{ flex: '0 0 220px' }}>
          <label style={{ display: 'block', fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: 6 }}>CSV File</label>
          <input type="file" accept=".csv" onChange={e => setWmsFile(e.target.files?.[0] || null)}
            style={{ color: 'var(--text-secondary)', fontSize: 13 }} />
        </div>
        <button onClick={handleWmsUpload} disabled={!wmsType || !wmsFile || wmsUploading}
          style={{ padding: '8px 18px', background: 'var(--primary)', border: 'none', borderRadius: 6, color: 'white', fontWeight: 600, fontSize: 13, cursor: 'pointer', opacity: (!wmsType || !wmsFile) ? 0.5 : 1 }}>
          {wmsUploading ? '⏳ Uploading…' : '⬆ Upload'}
        </button>
      </div>
      {selectedWmsType && (
        <div style={{ marginTop: 12, padding: '10px 14px', background: 'rgba(124,58,237,0.08)', border: '1px solid rgba(124,58,237,0.2)', borderRadius: 8, fontSize: 12 }}>
          <div style={{ color: 'var(--text-secondary)', marginBottom: 4 }}>{selectedWmsType.icon} {selectedWmsType.desc}</div>
          <div style={{ color: 'var(--text-muted)' }}>📋 Required columns: <code style={{ fontFamily: 'monospace', color: 'var(--accent-cyan)' }}>{selectedWmsType.columns}</code></div>
          {wmsType === 'stock_migration' && (
            <>
              <div style={{ color: '#ef4444', fontWeight: 600, marginTop: 8 }}>⚠️ This import will destructively overwrite existing stock quantities. Ensure you have a backup.</div>
              <label style={{ display: 'flex', alignItems: 'center', gap: 8, marginTop: 8, cursor: 'pointer', fontSize: 12, color: 'var(--text-secondary)' }}>
                <input type="checkbox" checked={wmsConfirmMigration} onChange={e => setWmsConfirmMigration(e.target.checked)} />
                I understand this is destructive and have taken a backup
              </label>
            </>
          )}
        </div>
      )}
      {wmsResult && <div style={{ marginTop: 10, color: '#10b981', fontSize: 13, fontWeight: 600 }}>{wmsResult}</div>}
      {wmsError && <div style={{ marginTop: 10, color: '#ef4444', fontSize: 13 }}>{wmsError}</div>}
    </div>
  );
}

// ============================================================================
// AMAZON SP-API DEBUG PANEL (TEMPORARY — remove after troubleshooting)
// ============================================================================
// Renders below the jobs table. Allows selecting a running/recent job and
// firing a test SP-API call for a given ASIN so you can see the raw request
// and response Amazon returns. Useful for diagnosing why enrichment isn't
// collecting data (wrong creds, throttling, bad marketplace ID, etc.)
// ============================================================================

interface DebugEntry {
  id: string;
  ts: string;
  asin: string;
  credentialId: string;
  status: 'pending' | 'ok' | 'error';
  requestUrl?: string;
  requestHeaders?: Record<string, string>;
  responseStatus?: number;
  responseBody?: any;
  errorMessage?: string;
  durationMs?: number;
}

function AmazonDebugPanel({ jobs, credentials }: { jobs: ImportJob[]; credentials: any[] }) {
  const API_BASE = (import.meta.env.VITE_API_URL ?? 'http://localhost:8080/api/v1') as string;
  const [open, setOpen] = React.useState(false);
  const [testAsin, setTestAsin] = React.useState('');
  const [testCredId, setTestCredId] = React.useState('');
  const [testJobId, setTestJobId] = React.useState('');
  const [entries, setEntries] = React.useState<DebugEntry[]>([]);
  const [running, setRunning] = React.useState(false);
  const [selectedEntry, setSelectedEntry] = React.useState<DebugEntry | null>(null);

  // Auto-select the most recent amazon job credential
  React.useEffect(() => {
    const amazonJob = [...jobs].reverse().find(j => j.channel === 'amazon');
    if (amazonJob && !testCredId) {
      setTestCredId(amazonJob.channel_account_id || '');
      setTestJobId(amazonJob.job_id || '');
    }
  }, [jobs]);

  const runTest = async () => {
    if (!testAsin.trim() || !testCredId) return;
    const entryId = Math.random().toString(36).slice(2);
    const entry: DebugEntry = {
      id: entryId,
      ts: new Date().toISOString(),
      asin: testAsin.trim().toUpperCase(),
      credentialId: testCredId,
      status: 'pending',
    };
    setEntries(prev => [entry, ...prev]);
    setRunning(true);

    const t0 = Date.now();
    try {
      const res = await fetch(`${API_BASE}/marketplace/amazon/debug-enrich`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-Tenant-Id': (window as any).__activeTenantId || '',
        },
        body: JSON.stringify({
          asin: testAsin.trim().toUpperCase(),
          credential_id: testCredId,
          job_id: testJobId || undefined,
        }),
      });
      const durationMs = Date.now() - t0;
      const json = await res.json().catch(async () => ({ raw: await res.text() }));
      const updated: DebugEntry = {
        ...entry,
        status: res.ok ? 'ok' : 'error',
        responseStatus: res.status,
        responseBody: json,
        durationMs,
        requestUrl: json?.debug?.request_url,
        requestHeaders: json?.debug?.request_headers,
        errorMessage: !res.ok ? (json?.error || `HTTP ${res.status}`) : undefined,
      };
      setEntries(prev => prev.map(e => e.id === entryId ? updated : e));
      setSelectedEntry(updated);
    } catch (err: any) {
      const updated: DebugEntry = {
        ...entry,
        status: 'error',
        errorMessage: err.message,
        durationMs: Date.now() - t0,
      };
      setEntries(prev => prev.map(e => e.id === entryId ? updated : e));
      setSelectedEntry(updated);
    } finally {
      setRunning(false);
    }
  };

  const amazonCreds = credentials.filter(c => c.channel === 'amazon');
  const recentAmazonJobs = jobs.filter(j => j.channel === 'amazon').slice(0, 10);

  return (
    <div style={{
      marginTop: 32,
      border: '2px dashed #f59e0b',
      borderRadius: 12,
      background: 'rgba(245,158,11,0.04)',
      overflow: 'hidden',
    }}>
      {/* Header toggle */}
      <button
        onClick={() => setOpen(o => !o)}
        style={{
          width: '100%', display: 'flex', alignItems: 'center', gap: 10,
          padding: '14px 20px', background: 'none', border: 'none', cursor: 'pointer',
          textAlign: 'left',
        }}
      >
        <span style={{ fontSize: 18 }}>🔬</span>
        <span style={{ fontWeight: 700, fontSize: 14, color: '#f59e0b' }}>
          Amazon SP-API Debug Panel
        </span>
        <span style={{
          fontSize: 10, fontWeight: 600, background: '#f59e0b', color: '#000',
          padding: '2px 8px', borderRadius: 8, marginLeft: 4,
        }}>TEMPORARY</span>
        <span style={{ marginLeft: 'auto', color: '#f59e0b', fontSize: 12 }}>
          {open ? '▲ Hide' : '▼ Show'}
        </span>
      </button>

      {open && (
        <div style={{ padding: '0 20px 20px' }}>
          <p style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 0, marginBottom: 16 }}>
            Fire a test SP-API catalog lookup for a single ASIN to inspect the raw request &amp; response.
            Use this to confirm credentials are valid, check what Amazon returns, and diagnose enrichment failures.
          </p>

          {/* Controls */}
          <div style={{ display: 'flex', gap: 10, flexWrap: 'wrap', alignItems: 'flex-end', marginBottom: 16 }}>
            <div style={{ flex: '1 1 180px' }}>
              <label style={{ display: 'block', fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', marginBottom: 4, textTransform: 'uppercase', letterSpacing: '0.05em' }}>
                Credential
              </label>
              <select
                value={testCredId}
                onChange={e => setTestCredId(e.target.value)}
                style={{ width: '100%', padding: '7px 10px', background: 'var(--bg-secondary)', border: '1px solid var(--border-bright)', borderRadius: 6, color: 'var(--text-primary)', fontSize: 13 }}
              >
                <option value="">— select credential —</option>
                {amazonCreds.map(c => (
                  <option key={c.credential_id} value={c.credential_id}>
                    {c.account_name || c.credential_id}
                  </option>
                ))}
              </select>
            </div>

            <div style={{ flex: '1 1 160px' }}>
              <label style={{ display: 'block', fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', marginBottom: 4, textTransform: 'uppercase', letterSpacing: '0.05em' }}>
                ASIN to test
              </label>
              <input
                type="text"
                value={testAsin}
                onChange={e => setTestAsin(e.target.value)}
                onKeyDown={e => e.key === 'Enter' && runTest()}
                placeholder="e.g. B08N5WRWNW"
                style={{ width: '100%', padding: '7px 10px', background: 'var(--bg-secondary)', border: '1px solid var(--border-bright)', borderRadius: 6, color: 'var(--text-primary)', fontSize: 13, boxSizing: 'border-box' }}
              />
            </div>

            <div style={{ flex: '1 1 180px' }}>
              <label style={{ display: 'block', fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', marginBottom: 4, textTransform: 'uppercase', letterSpacing: '0.05em' }}>
                Associated job (optional)
              </label>
              <select
                value={testJobId}
                onChange={e => setTestJobId(e.target.value)}
                style={{ width: '100%', padding: '7px 10px', background: 'var(--bg-secondary)', border: '1px solid var(--border-bright)', borderRadius: 6, color: 'var(--text-primary)', fontSize: 13 }}
              >
                <option value="">— none —</option>
                {recentAmazonJobs.map(j => (
                  <option key={j.job_id} value={j.job_id}>
                    {j.job_id.slice(0, 12)}… ({j.status})
                  </option>
                ))}
              </select>
            </div>

            <button
              onClick={runTest}
              disabled={!testAsin.trim() || !testCredId || running}
              style={{
                padding: '8px 20px', background: '#f59e0b', border: 'none',
                borderRadius: 6, color: '#000', fontWeight: 700, fontSize: 13,
                cursor: (!testAsin.trim() || !testCredId || running) ? 'not-allowed' : 'pointer',
                opacity: (!testAsin.trim() || !testCredId) ? 0.5 : 1,
                whiteSpace: 'nowrap',
              }}
            >
              {running ? '⏳ Testing…' : '▶ Run Test'}
            </button>
          </div>

          {/* Entry list + detail pane */}
          {entries.length > 0 && (
            <div style={{ display: 'flex', gap: 12, minHeight: 260 }}>
              {/* Left: entry list */}
              <div style={{ flex: '0 0 260px', overflowY: 'auto', maxHeight: 480, borderRight: '1px solid var(--border-bright)', paddingRight: 12 }}>
                {entries.map(e => (
                  <button
                    key={e.id}
                    onClick={() => setSelectedEntry(e)}
                    style={{
                      display: 'block', width: '100%', textAlign: 'left',
                      padding: '8px 10px', marginBottom: 4, borderRadius: 6,
                      background: selectedEntry?.id === e.id ? 'rgba(245,158,11,0.12)' : 'var(--bg-secondary)',
                      border: selectedEntry?.id === e.id ? '1px solid #f59e0b' : '1px solid var(--border-bright)',
                      cursor: 'pointer',
                    }}
                  >
                    <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                      <span style={{ fontSize: 14 }}>
                        {e.status === 'pending' ? '⏳' : e.status === 'ok' ? '✅' : '❌'}
                      </span>
                      <span style={{ fontWeight: 700, fontSize: 12, fontFamily: 'monospace' }}>{e.asin}</span>
                      {e.durationMs && (
                        <span style={{ fontSize: 10, color: 'var(--text-muted)', marginLeft: 'auto' }}>{e.durationMs}ms</span>
                      )}
                    </div>
                    <div style={{ fontSize: 10, color: 'var(--text-muted)', marginTop: 2 }}>
                      {new Date(e.ts).toLocaleTimeString()} · HTTP {e.responseStatus ?? '…'}
                    </div>
                    {e.errorMessage && (
                      <div style={{ fontSize: 10, color: '#ef4444', marginTop: 2, wordBreak: 'break-all' }}>
                        {e.errorMessage.slice(0, 80)}
                      </div>
                    )}
                  </button>
                ))}
              </div>

              {/* Right: detail pane */}
              {selectedEntry && (
                <div style={{ flex: 1, overflowY: 'auto', maxHeight: 480 }}>
                  <div style={{ marginBottom: 12 }}>
                    <span style={{ fontWeight: 700, fontSize: 13 }}>{selectedEntry.asin}</span>
                    <span style={{ marginLeft: 10, fontSize: 12, color: selectedEntry.status === 'ok' ? '#10b981' : '#ef4444', fontWeight: 600 }}>
                      HTTP {selectedEntry.responseStatus ?? '—'}
                    </span>
                    {selectedEntry.durationMs && (
                      <span style={{ marginLeft: 10, fontSize: 11, color: 'var(--text-muted)' }}>{selectedEntry.durationMs}ms</span>
                    )}
                  </div>

                  {/* Request URL */}
                  {selectedEntry.requestUrl && (
                    <div style={{ marginBottom: 12 }}>
                      <div style={{ fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.06em', marginBottom: 4 }}>
                        Request URL
                      </div>
                      <code style={{ fontSize: 11, color: '#60a5fa', wordBreak: 'break-all', display: 'block', background: 'var(--bg-tertiary)', padding: '6px 10px', borderRadius: 6 }}>
                        {selectedEntry.requestUrl}
                      </code>
                    </div>
                  )}

                  {/* Request headers */}
                  {selectedEntry.requestHeaders && Object.keys(selectedEntry.requestHeaders).length > 0 && (
                    <div style={{ marginBottom: 12 }}>
                      <div style={{ fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.06em', marginBottom: 4 }}>
                        Request Headers
                      </div>
                      <div style={{ background: 'var(--bg-tertiary)', borderRadius: 6, padding: '8px 10px' }}>
                        {Object.entries(selectedEntry.requestHeaders).map(([k, v]) => (
                          <div key={k} style={{ fontSize: 11, display: 'flex', gap: 8, marginBottom: 2 }}>
                            <span style={{ color: '#a78bfa', fontFamily: 'monospace', flex: '0 0 200px' }}>{k}</span>
                            <span style={{ color: 'var(--text-secondary)', fontFamily: 'monospace', wordBreak: 'break-all' }}>
                              {k.toLowerCase().includes('token') || k.toLowerCase().includes('secret') || k.toLowerCase().includes('authorization')
                                ? v.slice(0, 20) + '…[redacted]'
                                : v}
                            </span>
                          </div>
                        ))}
                      </div>
                    </div>
                  )}

                  {/* Response body */}
                  <div>
                    <div style={{ fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.06em', marginBottom: 4 }}>
                      Response Body
                    </div>
                    <pre style={{
                      fontSize: 11, background: 'var(--bg-tertiary)', padding: '10px 12px',
                      borderRadius: 6, overflowX: 'auto', maxHeight: 300, overflowY: 'auto',
                      margin: 0, color: selectedEntry.status === 'error' ? '#fca5a5' : 'var(--text-primary)',
                      whiteSpace: 'pre-wrap', wordBreak: 'break-word',
                    }}>
                      {selectedEntry.responseBody
                        ? JSON.stringify(selectedEntry.responseBody, null, 2)
                        : selectedEntry.errorMessage || 'No response'}
                    </pre>
                  </div>
                </div>
              )}
            </div>
          )}

          {entries.length === 0 && (
            <div style={{ textAlign: 'center', padding: '32px 0', color: 'var(--text-muted)', fontSize: 13 }}>
              Enter an ASIN and click Run Test to inspect the SP-API response
            </div>
          )}
        </div>
      )}
    </div>
  );
}

export default function ImportDashboard() {
  const navigate = useNavigate();
  const [jobs, setJobs] = useState<ImportJob[]>([]);
  const [credentials, setCredentials] = useState<MarketplaceCredential[]>([]);
  const [loading, setLoading] = useState(true);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const [now, setNow] = useState(Date.now());
  const prevJobStatuses = useRef<Record<string, string>>({});

  // Tick every second so elapsed timers update live without polling the API
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, []);

  // ── Multi-step import modal state ────────────────────────────────────────
  const [modalOpen, setModalOpen] = useState(false);
  const [modalStep, setModalStep] = useState<'configure' | 'inventory' | 'listings' | 'confirm'>('configure');
  const [starting, setStarting] = useState(false);

  // Queue of imports the user has built up
  type QueuedImport = {
    id: string; // local key for react
    credentialId: string;
    channel: string;
    accountName: string;
    jobType: 'full_import';
    fulfillmentFilter: string;
    stockFilter: string;
    enrichData: boolean;
    syncStock: boolean;
    temuStatusFilters: number[];
    ebayListTypes: string[];
  };
  const [queue, setQueue] = useState<QueuedImport[]>([]);

  // ── Drag-and-drop hub state ───────────────────────────────────────────────
  // (removed — replaced by per-account import buttons)

  // Current import being configured (before adding to queue)
  const [cfgCredential, setCfgCredential] = useState('');
  const [cfgFulfillment, setCfgFulfillment] = useState('all');
  const [cfgStock, setCfgStock] = useState('all');
  const [cfgEnrich, setCfgEnrich] = useState(true);
  const [cfgSyncStock, setCfgSyncStock] = useState(false);
  const [cfgTemuStatus, setCfgTemuStatus] = useState<number[]>([1]);
  const [cfgEbayTypes, setCfgEbayTypes] = useState<string[]>(['ActiveList']);

  // Inventory sync step
  const [inventorySync, setInventorySync] = useState(false);
  const [inventorySource, setInventorySource] = useState('');

  // Listing generation step
  const [generateListings, setGenerateListings] = useState(false);
  const [listingTargets, setListingTargets] = useState<string[]>([]);
  const [listingAiEnrich, setListingAiEnrich] = useState(false);

  useEffect(() => {
    loadData();
    pollRef.current = setInterval(pollJobs, 3000);
    return () => { if (pollRef.current) clearInterval(pollRef.current); };
  }, []);

  async function loadData() {
    setLoading(true);
    try {
      const [jobsRes, credsRes] = await Promise.allSettled([
        importService.listJobs({ page_size: 50 }),
        credentialService.list(),
      ]);
      if (jobsRes.status === 'fulfilled') setJobs(jobsRes.value.data?.data || []);
      if (credsRes.status === 'fulfilled') setCredentials(credsRes.value.data?.data || []);
    } catch { /* ignore */ }
    finally { setLoading(false); }
  }

  async function pollJobs() {
    try {
      const res = await importService.listJobs({ page_size: 50 });
      const updatedJobs: any[] = res.data?.data || [];
      if (updatedJobs.length) {
        // Detect jobs that just transitioned to completed and trigger Typesense reindex
        updatedJobs.forEach(job => {
          const prev = prevJobStatuses.current[job.job_id];
          if (prev && prev !== 'completed' && job.status === 'completed') {
            // Fire and forget — don't block the poll
            searchService.sync().catch(() => {});
          }
          prevJobStatuses.current[job.job_id] = job.status;
        });
        setJobs(updatedJobs);
      }
    } catch { /* ignore */ }
  }

  async function cancelJob(jobId: string) {
    if (!confirm('Cancel this import job? Products already imported will remain.')) return;
    try {
      await importService.cancel(jobId);
      await pollJobs();
    } catch (err: any) {
      alert('Failed to cancel: ' + (err.response?.data?.error || err.message));
    }
  }

  const cfgChannel = credentials.find(c => c.credential_id === cfgCredential)?.channel || '';
  const activeCreds = credentials.filter(c => c.active);
  const queuedCredIds = queue.map(q => q.credentialId);
  const availableCreds = activeCreds.filter(c => !queuedCredIds.includes(c.credential_id));

  function resetCfg() {
    setCfgCredential('');
    setCfgFulfillment('all');
    setCfgStock('all');
    setCfgEnrich(true);
    setCfgSyncStock(false);
    setCfgTemuStatus([1]);
    setCfgEbayTypes(['ActiveList']);
  }

  function openModal() {
    setQueue([]);
    setModalStep('configure');
    setInventorySync(false);
    setInventorySource('');
    setGenerateListings(false);
    setListingTargets([]);
    setListingAiEnrich(false);
    resetCfg();
    setModalOpen(true);
  }

  function openModalForCred(credId: string) {
    setQueue([]);
    setModalStep('configure');
    setInventorySync(false);
    setInventorySource('');
    setGenerateListings(false);
    setListingTargets([]);
    setListingAiEnrich(false);
    resetCfg();
    setCfgCredential(credId);
    setModalOpen(true);
  }

  function addToQueue() {
    if (!cfgCredential) return;
    const cred = credentials.find(c => c.credential_id === cfgCredential);
    if (!cred) return;
    const item: QueuedImport = {
      id: crypto.randomUUID(),
      credentialId: cfgCredential,
      channel: cred.channel,
      accountName: cred.account_name,
      jobType: 'full_import',
      fulfillmentFilter: cfgChannel === 'amazon' ? cfgFulfillment : 'all',
      stockFilter: cfgChannel === 'amazon' ? cfgStock : 'all',
      enrichData: cfgChannel === 'amazon' ? true : cfgEnrich,
      syncStock: cfgSyncStock,
      temuStatusFilters: cfgChannel === 'temu' ? cfgTemuStatus : [1],
      ebayListTypes: cfgChannel === 'ebay' ? cfgEbayTypes : ['ActiveList'],
    };
    setQueue(prev => [...prev, item]);
    resetCfg();
  }

  async function handleStartAllImports() {
    if (queue.length === 0) return;
    setStarting(true);
    try {
      await Promise.all(queue.map(item => {
        const payload: StartImportRequest = {
          channel: item.channel,
          channel_account_id: item.credentialId,
          job_type: item.jobType,
          fulfillment_filter: item.channel === 'amazon' ? item.fulfillmentFilter : undefined,
          stock_filter: item.channel === 'amazon' ? item.stockFilter : undefined,
          enrich_data: item.enrichData,
          sync_stock: item.syncStock,
          temu_status_filters: item.channel === 'temu' ? item.temuStatusFilters : undefined,
          ebay_list_types: item.channel === 'ebay' ? item.ebayListTypes : undefined,
          // Store post-import preferences on each job
          inventory_sync: inventorySync,
          inventory_source: inventorySync ? inventorySource : undefined,
          generate_listings: generateListings,
          listing_targets: generateListings ? listingTargets : undefined,
          listing_ai_enrich: generateListings ? listingAiEnrich : undefined,
        };
        return importService.start(payload);
      }));
      setModalOpen(false);
      await pollJobs();
    } catch (err: any) {
      alert('Failed to start imports: ' + (err.response?.data?.error || err.response?.data?.details || err.message));
    } finally {
      setStarting(false);
    }
  }

  // ── Hub helpers removed — import is now triggered via per-account buttons ──

  const stats = {
    total: jobs.length,
    running: jobs.filter(j => j.status === 'running' || j.status === 'pending').length,
    completed: jobs.filter(j => j.status === 'completed').length,
    failed: jobs.filter(j => j.status === 'failed').length,
    totalImported: jobs.reduce((s, j) => s + (j.successful_items || 0), 0),
  };

  if (loading) {
    return <div className="page"><div className="loading-state"><div className="spinner"></div><p>Loading import dashboard...</p></div></div>;
  }

  return (
    <div className="page">
      <style>{`
        @keyframes pulse-bar {
          0%   { width: 15%; opacity: 0.4; }
          100% { width: 85%; opacity: 0.8; }
        }
      `}</style>
      <div className="page-header">
        <div>
          <h1 className="page-title">Import Dashboard</h1>
          <p className="page-subtitle">Import products from connected marketplaces</p>
        </div>
        <div className="page-actions">
          <button className="btn btn-primary" onClick={openModal} disabled={activeCreds.length === 0}>⬇ Start Import</button>
        </div>
      </div>

      {activeCreds.length === 0 && (
        <div style={{ padding: '12px 16px', marginBottom: 20, borderRadius: 8, background: 'var(--warning-glow)', border: '1px solid var(--warning)', color: 'var(--warning)', fontSize: 13, fontWeight: 600 }}>
          ⚠ No active marketplace connections.{' '}
          <span style={{ color: 'var(--primary)', cursor: 'pointer', textDecoration: 'underline' }} onClick={() => navigate('/marketplace/connections')}>Connect a marketplace first</span>
        </div>
      )}

      <div className="stats-grid">
        <div className="stat-card"><div className="stat-label">Total Jobs</div><div className="stat-value">{stats.total}</div></div>
        <div className="stat-card"><div className="stat-label">Running</div><div className="stat-value" style={{ color: stats.running > 0 ? 'var(--info)' : undefined }}>{stats.running}</div></div>
        <div className="stat-card"><div className="stat-label">Completed</div><div className="stat-value" style={{ color: 'var(--success)' }}>{stats.completed}</div></div>
        <div className="stat-card"><div className="stat-label">Products Imported</div><div className="stat-value">{stats.totalImported.toLocaleString()}</div></div>
      </div>

      {/* ── WMS CSV Imports ───────────────────────────────────────────────── */}
      <WmsCsvImports />
      <AmazonDebugPanel jobs={jobs} credentials={credentials} />

      {/* ── Marketplace Accounts ─────────────────────────────────────────── */}
      {activeCreds.length > 0 && (
        <div className="card" style={{ marginBottom: 24 }}>
          <div className="card-header">
            <h3 style={{ fontSize: 15, fontWeight: 700 }}>Marketplace Accounts</h3>
            <button className="btn btn-primary" onClick={openModal} disabled={activeCreds.length === 0}>⬇ Start Import</button>
          </div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 6, padding: '12px 16px' }}>
            {activeCreds.map(cred => (
              <div
                key={cred.credential_id}
                style={{ display: 'flex', alignItems: 'center', gap: 12, padding: '10px 14px', borderRadius: 10, background: 'var(--bg-tertiary)', border: '1px solid var(--border-bright)', transition: 'border-color 0.15s' }}
              >
                <span style={{ fontSize: 20 }}>{adapterEmoji[cred.channel] || '🌐'}</span>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div style={{ fontWeight: 600, fontSize: 13, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{cred.account_name}</div>
                  <div style={{ fontSize: 11, color: 'var(--text-muted)', textTransform: 'capitalize' }}>{cred.channel}</div>
                </div>
                <button
                  className="btn btn-secondary"
                  style={{ fontSize: 12, padding: '6px 12px', borderRadius: 6, whiteSpace: 'nowrap' }}
                  onClick={() => navigate(`/marketplace/channels/${cred.credential_id}/reconcile`)}
                  title="Review and manually match products from this channel"
                >
                  🔗 Review Matches
                </button>
                <button
                  className="btn btn-primary"
                  style={{ fontSize: 12, padding: '6px 14px', borderRadius: 6 }}
                  onClick={() => openModalForCred(cred.credential_id)}
                >
                  ⬇ Import
                </button>
              </div>
            ))}
          </div>
        </div>
      )}

      <div className="card">
        <div className="card-header">
          <h3 style={{ fontSize: 15, fontWeight: 700 }}>Import Jobs</h3>
          <button className="btn btn-secondary" onClick={loadData} style={{ fontSize: 12 }}>🔄 Refresh</button>
        </div>
        {jobs.length === 0 ? (
          <div className="empty-state">
            <div className="empty-icon">📥</div>
            <h3>No imports yet</h3>
            <p>Start your first import to bring products from your marketplace accounts</p>
            <button className="btn btn-primary" onClick={openModal} disabled={activeCreds.length === 0}>⬇ Start Import</button>
          </div>
        ) : (
          <div className="table-container">
            <table className="table">
              <thead>
                <tr>
                  <th>Marketplace</th>
                  <th>Status</th>
                  <th>Progress</th>
                  <th>Items</th>
                  <th>Imported</th>
                  <th>Failed</th>
                  <th>Skipped</th>
                  <th>Started</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {jobs.map(job => {
                  const isActive      = job.status === 'running' || job.status === 'pending';
                  // Progress logic — matches JobMonitor exactly:
                  // For Amazon with enrichment: use enriched_items as progress (enrichment is the slow phase)
                  // For everything else: use processed_items
                  const totalItems      = job.total_items        || 0;
                  const enrichTotalItems = job.enrich_total_items  || 0;
                  const processedItems  = job.processed_items      || 0;
                  const successfulItems = job.successful_items      || 0;
                  const enrichSkipped   = job.enrich_skipped_items  || 0;
                  const enrichFailed    = job.enrich_failed_items   || 0;
                  const trueEnriched    = job.enriched_items        || 0;
                  const isAmazon        = job.channel === 'amazon' || job.channel === 'amazonnew';
                  const enrichOn        = isAmazon && (job as any).enrich_data !== false;
                  // For Amazon jobs with enrichment, progress is purely enrichment.
                  // inEnrichPhase triggers as soon as ANY enrich activity is seen —
                  // either enrich_total_items is set OR enriched_items has started
                  // incrementing (whichever comes first).
                  const enrichDone      = trueEnriched + enrichSkipped + enrichFailed;
                  const inEnrichPhase   = enrichOn && (enrichTotalItems > 0 || enrichDone > 0);
                  const enrichTotal     = enrichTotalItems || totalItems;
                  const displayDone     = inEnrichPhase ? enrichDone : (enrichOn ? 0 : processedItems);
                  const displayTotal    = inEnrichPhase ? enrichTotal : (enrichOn ? (enrichTotal || 1) : Math.max(totalItems, processedItems));
                  const isCounting      = isActive && totalItems === 0;

                  const downloadPct = displayTotal > 0
                    ? Math.min(Math.round((displayDone / displayTotal) * 100), 100)
                    : 0;

                  const barColor = job.status === 'failed'    ? 'var(--danger)'
                                 : job.status === 'completed' ? 'var(--success)'
                                 : job.status === 'cancelled' ? 'var(--warning)'
                                 : 'var(--primary)';

                  const statusLabel = isCounting  ? 'starting'
                                    : job.status;

                  const phaseLabel  = inEnrichPhase && isActive ? ' (enriching)' : '';
                  const itemsDone   = inEnrichPhase ? enrichDone : (enrichOn ? 0 : processedItems);
                  const itemsTotal  = inEnrichPhase ? enrichTotal : Math.max(totalItems, processedItems);
                  const itemsText = isCounting
                    ? '0 / …'
                    : itemsTotal > 0
                    ? `${itemsDone.toLocaleString()} / ${itemsTotal.toLocaleString()}${phaseLabel}`
                    : isActive ? '—' : '0 / 0';

                  const cred = credentials.find(c => c.credential_id === job.channel_account_id);
                  const accountName = cred?.account_name || job.channel;

                  return (
                    <tr key={job.job_id} style={{ cursor: 'pointer' }} onClick={() => navigate(`/marketplace/import/${job.job_id}`)}>
                      <td>
                        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                          <span style={{ fontSize: 18 }}>{adapterEmoji[job.channel] || '🌐'}</span>
                          <div>
                            <div style={{ fontWeight: 600 }}>{accountName}</div>
                            <div style={{ fontSize: 11, color: 'var(--text-muted)', textTransform: 'capitalize' }}>
                              {job.channel} · {job.job_type === 'full_import' ? 'Full Import' : job.job_type}
                            </div>
                          </div>
                        </div>
                      </td>

                      <td>
                        <span className={`badge ${
                          job.status === 'completed' ? 'badge-success' :
                          job.status === 'cancelled' ? 'badge-warning' :
                          job.status === 'failed'    ? 'badge-danger'  :
                          isActive                   ? 'badge-info'    : 'badge-warning'
                        }`}>
                          {statusLabel}
                        </span>
                        {isActive && (() => {
                          const startMs = new Date(job.started_at || job.created_at).getTime();
                          const elapsedSec = Math.floor((now - startMs) / 1000);
                          const m = Math.floor(elapsedSec / 60);
                          const s = elapsedSec % 60;
                          const elapsed = m > 0 ? `${m}m ${s}s` : `${s}s`;
                          // Build a smart status message:
                          // 1. If backend has a specific status_message, use it (covers report download phase)
                          // 2. If in enrichment phase, show live enrichment count
                          // 3. Otherwise just show elapsed time
                          let subMsg: string | null = null;
                          if (job.status_message && !job.status_message.startsWith('Queued')) {
                            subMsg = job.status_message;
                          } else if (inEnrichPhase) {
                            subMsg = `Enriching products… ${trueEnriched.toLocaleString()} / ${Math.max(enrichTotalItems, trueEnriched).toLocaleString()}`;
                          } else if (job.status_message) {
                            subMsg = job.status_message;
                          }
                          return (
                            <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 3 }}>
                              {subMsg ? (
                                <span title={subMsg} style={{ display: 'block', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', maxWidth: '100%' }}>
                                  ⏱ {elapsed} · {subMsg}
                                </span>
                              ) : (
                                <span style={{ fontVariantNumeric: 'tabular-nums' }}>⏱ {elapsed}</span>
                              )}
                            </div>
                          );
                        })()}
                      </td>

                      <td style={{ width: 180 }}>
                        {/* Counting — total unknown */}
                        {isCounting && (
                          <div>
                            <div style={{ height: 6, background: 'var(--bg-tertiary)', borderRadius: 3, overflow: 'hidden' }}>
                              <div style={{ height: '100%', width: '30%', background: barColor, borderRadius: 3, animation: 'pulse-bar 1.5s ease-in-out infinite alternate', opacity: 0.7 }} />
                            </div>
                            <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 4 }}>Starting import…</div>
                          </div>
                        )}

                        {/* Active — single progress bar */}
                        {!isCounting && isActive && (
                          <div>
                            <div style={{ height: 6, background: 'var(--bg-tertiary)', borderRadius: 3, overflow: 'hidden' }}>
                              <div style={{ height: '100%', width: `${downloadPct}%`, background: barColor, borderRadius: 3, transition: 'width 0.4s ease' }} />
                            </div>
                            <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 4 }}>{downloadPct}%</div>
                          </div>
                        )}

                        {/* Done */}
                        {!isActive && displayTotal > 0 && (
                          <div>
                            <div style={{ height: 6, background: 'var(--bg-tertiary)', borderRadius: 3, overflow: 'hidden' }}>
                              <div style={{ height: '100%', width: `${downloadPct}%`, background: barColor, borderRadius: 3 }} />
                            </div>
                            <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 4 }}>{downloadPct}%</div>
                          </div>
                        )}
                      </td>

                      <td style={{ fontWeight: 600 }}>{itemsText}</td>
                      <td>
                        {trueEnriched > 0 ? (
                          <span style={{ color: 'var(--success)', fontWeight: 600 }}>
                            {trueEnriched.toLocaleString()}
                          </span>
                        ) : (
                          <span style={{ color: 'var(--text-muted)' }}>0</span>
                        )}
                      </td>
                      <td>
                        {(() => {
                          // For Amazon+enrich jobs, failures come from enrich_failed_items.
                          // For all other channels, use batch-phase failed_items.
                          const failCount = enrichOn ? enrichFailed : (job.failed_items || 0);
                          return failCount > 0 ? (
                            <button
                              className="btn"
                              style={{ fontSize: 12, padding: '2px 8px', color: 'var(--danger)', background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 5, fontWeight: 700, cursor: 'pointer' }}
                              onClick={e => { e.stopPropagation(); navigate(`/marketplace/import/${job.job_id}#errors`); }}
                              title="View error details"
                            >
                              {failCount} ⚠
                            </button>
                          ) : (
                            <span style={{ color: 'var(--text-muted)' }}>0</span>
                          );
                        })()}
                      </td>
                      <td>
                        {enrichSkipped > 0 ? (
                          <span style={{ color: 'var(--text-muted)', fontWeight: 600 }} title="Already enriched from a previous run">
                            {enrichSkipped.toLocaleString()}
                          </span>
                        ) : (
                          <span style={{ color: 'var(--text-muted)' }}>0</span>
                        )}
                      </td>
                      <td style={{ color: 'var(--text-muted)', fontSize: 13 }}>{formatDate(job.started_at || job.created_at)}</td>
                      <td style={{ textAlign: 'right' }}>
                        {isActive ? (
                          <button
                            className="btn btn-secondary"
                            style={{ fontSize: 11, padding: '4px 10px', color: 'var(--danger)', borderColor: 'var(--danger)' }}
                            onClick={e => { e.stopPropagation(); cancelJob(job.job_id); }}
                          >✕ Cancel</button>
                        ) : job.status === 'completed' ? (
                          <div style={{ display: 'flex', flexDirection: 'column', gap: 4, alignItems: 'flex-end' }}>
                            {job.match_status === 'review_required' && (
                              <button
                                className="btn btn-primary"
                                style={{ fontSize: 11, padding: '4px 10px', whiteSpace: 'nowrap', background: '#f59e0b', borderColor: '#f59e0b', color: '#fff' }}
                                onClick={e => {
                                  e.stopPropagation();
                                  navigate(`/marketplace/import/${job.job_id}/review-matches`);
                                }}
                                title="Review possible duplicate matches from this import"
                              >
                                ⚠ Review Matches
                              </button>
                            )}
                            {job.match_status === 'reviewed' && (
                              <span style={{ fontSize: 11, color: '#10b981', fontWeight: 600 }}>✓ Reviewed</span>
                            )}
                            {(!job.match_status || job.match_status === 'no_review_needed') && job.channel_account_id && (
                              <button
                                className="btn btn-secondary"
                                style={{ fontSize: 11, padding: '4px 10px', whiteSpace: 'nowrap' }}
                                onClick={e => {
                                  e.stopPropagation();
                                  navigate(`/marketplace/import/${job.job_id}/review-matches`);
                                }}
                                title="Analyze this import for potential duplicate products"
                              >
                                🔍 Analyze Matches
                              </button>
                            )}
                          </div>
                        ) : (
                          <span style={{ color: 'var(--text-muted)' }}>→</span>
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

      {/* ── Multi-step Import Modal ─────────────────────────────────────── */}
      {modalOpen && (() => {
        const STEPS = ['configure', 'inventory', 'listings', 'confirm'] as const;
        const stepIdx = STEPS.indexOf(modalStep);
        const stepLabels = ['Build Queue', 'Inventory', 'Listings', 'Confirm'];

        // Import config form — reused for initial + add-another
        const cfgValid = !!cfgCredential && (cfgChannel !== 'temu' || cfgTemuStatus.length > 0) && (cfgChannel !== 'ebay' || cfgEbayTypes.length > 0);

        const importConfigFormJsx = (
          <div>
            {/* Account selector — excludes already queued */}
            <div style={{ marginBottom: 16 }}>
              <label style={{ display: 'block', fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', marginBottom: 6, textTransform: 'uppercase', letterSpacing: '0.08em' }}>Marketplace Account</label>
              <select className="select" style={{ width: '100%' }} value={cfgCredential} onChange={e => setCfgCredential(e.target.value)}>
                <option value="">— Select account —</option>
                {availableCreds.map(c => (
                  <option key={c.credential_id} value={c.credential_id}>{adapterEmoji[c.channel] || '🌐'} {c.account_name} ({c.channel})</option>
                ))}
              </select>
            </div>

            {cfgCredential && (<>
              {/* Fulfillment filter — Amazon only */}
              {cfgChannel === 'amazon' && (
                <div style={{ marginBottom: 14 }}>
                  <label style={{ display: 'block', fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', marginBottom: 6, textTransform: 'uppercase', letterSpacing: '0.08em' }}>Fulfillment Channel</label>
                  <div style={{ display: 'flex', gap: 8 }}>
                    {([{ v: 'all', l: 'All' }, { v: 'fba', l: 'FBA Only' }, { v: 'merchant', l: 'Merchant Only' }]).map(opt => (
                      <button key={opt.v} onClick={() => setCfgFulfillment(opt.v)} style={{ flex: 1, padding: '7px 10px', borderRadius: 6, fontSize: 13, fontWeight: 600, cursor: 'pointer', transition: 'all 0.15s', background: cfgFulfillment === opt.v ? 'var(--primary)' : 'var(--bg-tertiary)', color: cfgFulfillment === opt.v ? '#fff' : 'var(--text-secondary)', border: `1px solid ${cfgFulfillment === opt.v ? 'var(--primary)' : 'var(--border-bright)'}` }}>{opt.l}</button>
                    ))}
                  </div>
                </div>
              )}

              {/* Stock filter — Amazon only */}
              {cfgChannel === 'amazon' && (
                <div style={{ marginBottom: 14 }}>
                  <label style={{ display: 'block', fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', marginBottom: 6, textTransform: 'uppercase', letterSpacing: '0.08em' }}>Stock Status</label>
                  <div style={{ display: 'flex', gap: 8 }}>
                    {([{ v: 'all', l: 'All Products' }, { v: 'in_stock', l: 'In Stock Only' }]).map(opt => (
                      <button key={opt.v} onClick={() => setCfgStock(opt.v)} style={{ flex: 1, padding: '7px 10px', borderRadius: 6, fontSize: 13, fontWeight: 600, cursor: 'pointer', transition: 'all 0.15s', background: cfgStock === opt.v ? 'var(--primary)' : 'var(--bg-tertiary)', color: cfgStock === opt.v ? '#fff' : 'var(--text-secondary)', border: `1px solid ${cfgStock === opt.v ? 'var(--primary)' : 'var(--border-bright)'}` }}>{opt.l}</button>
                    ))}
                  </div>
                </div>
              )}

              {/* Temu status filters */}
              {cfgChannel === 'temu' && (
                <div style={{ marginBottom: 14 }}>
                  <label style={{ display: 'block', fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', marginBottom: 6, textTransform: 'uppercase', letterSpacing: '0.08em' }}>Product Status to Import</label>
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                    {([{ value: 1, label: 'Active / Inactive', desc: 'On sale + off sale' }, { value: 4, label: 'Incomplete', desc: 'Not yet published' }, { value: 5, label: 'Draft', desc: 'Not submitted' }, { value: 6, label: 'Deleted', desc: 'Deleted or terminated' }] as const).map(opt => {
                      const checked = cfgTemuStatus.includes(opt.value);
                      return (
                        <label key={opt.value} style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '9px 12px', borderRadius: 7, cursor: 'pointer', background: checked ? 'var(--primary-glow)' : 'var(--bg-tertiary)', border: `1px solid ${checked ? 'var(--primary)' : 'var(--border-bright)'}` }}>
                          <input type="checkbox" checked={checked} onChange={() => setCfgTemuStatus(prev => prev.includes(opt.value) ? prev.filter(v => v !== opt.value) : [...prev, opt.value])} style={{ accentColor: 'var(--primary)' }} />
                          <div><div style={{ fontWeight: 600, fontSize: 13, color: checked ? 'var(--primary)' : 'var(--text-primary)' }}>{opt.label}</div><div style={{ fontSize: 11, color: 'var(--text-muted)' }}>{opt.desc}</div></div>
                        </label>
                      );
                    })}
                  </div>
                  {cfgTemuStatus.length === 0 && <div style={{ fontSize: 11, color: 'var(--danger)', marginTop: 4 }}>⚠ Select at least one status</div>}
                </div>
              )}

              {/* eBay listing types */}
              {cfgChannel === 'ebay' && (
                <div style={{ marginBottom: 14 }}>
                  <label style={{ display: 'block', fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', marginBottom: 6, textTransform: 'uppercase', letterSpacing: '0.08em' }}>Listing Types</label>
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                    {([{ value: 'ActiveList', label: 'Active Listings', desc: 'Currently live on eBay' }, { value: 'UnsoldList', label: 'Unsold / Ended', desc: 'Listings that ended without a sale' }, { value: 'SoldList', label: 'Sold Items', desc: 'Completed sales' }] as const).map(opt => {
                      const checked = cfgEbayTypes.includes(opt.value);
                      return (
                        <label key={opt.value} style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '9px 12px', borderRadius: 7, cursor: 'pointer', background: checked ? '#E532381A' : 'var(--bg-tertiary)', border: `1px solid ${checked ? '#E53238' : 'var(--border-bright)'}` }}>
                          <input type="checkbox" checked={checked} onChange={() => setCfgEbayTypes(prev => prev.includes(opt.value) ? prev.filter(v => v !== opt.value) : [...prev, opt.value])} style={{ accentColor: '#E53238' }} />
                          <div><div style={{ fontWeight: 600, fontSize: 13, color: checked ? '#E53238' : 'var(--text-primary)' }}>{opt.label}</div><div style={{ fontSize: 11, color: 'var(--text-muted)' }}>{opt.desc}</div></div>
                        </label>
                      );
                    })}
                  </div>
                  {cfgEbayTypes.length === 0 && <div style={{ fontSize: 11, color: 'var(--danger)', marginTop: 4 }}>⚠ Select at least one type</div>}
                </div>
              )}

              {/* Non-Amazon enrich toggle */}
              {cfgChannel !== 'amazon' && (
                <div style={{ marginBottom: 14, padding: 12, borderRadius: 8, background: 'var(--bg-tertiary)', border: '1px solid var(--border-bright)', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                  <div><div style={{ fontWeight: 600, fontSize: 13 }}>✨ Enrich Data</div><div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 2 }}>Fetch images, bullets & extended attributes</div></div>
                  <div onClick={() => setCfgEnrich(!cfgEnrich)} style={{ width: 40, height: 22, borderRadius: 11, cursor: 'pointer', transition: 'all 0.2s', background: cfgEnrich ? 'var(--accent-cyan)' : 'var(--bg-secondary)', border: `1px solid ${cfgEnrich ? 'var(--accent-cyan)' : 'var(--border-bright)'}`, position: 'relative', flexShrink: 0 }}>
                    <div style={{ width: 16, height: 16, borderRadius: '50%', background: '#fff', position: 'absolute', top: 2, left: cfgEnrich ? 20 : 2, transition: 'left 0.2s', boxShadow: '0 1px 3px rgba(0,0,0,0.3)' }} />
                  </div>
                </div>
              )}

              {/* Amazon enrich — always on, shown as info */}
              {cfgChannel === 'amazon' && (
                <div style={{ marginBottom: 14, padding: 12, borderRadius: 8, background: 'rgba(0,200,200,0.06)', border: '1px solid var(--accent-cyan)', display: 'flex', alignItems: 'center', gap: 10 }}>
                  <span style={{ fontSize: 18 }}>✨</span>
                  <div><div style={{ fontWeight: 600, fontSize: 13, color: 'var(--accent-cyan)' }}>Data enrichment enabled</div><div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 1 }}>Amazon imports always fetch images, bullets & extended attributes</div></div>
                </div>
              )}

              {/* Sync stock toggle */}
              <div style={{ padding: 12, borderRadius: 8, background: 'var(--bg-tertiary)', border: '1px solid var(--border-bright)', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                <div><div style={{ fontWeight: 600, fontSize: 13 }}>📦 Sync Stock Levels</div><div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 2 }}>Import quantity into your default warehouse</div></div>
                <div onClick={() => setCfgSyncStock(!cfgSyncStock)} style={{ width: 40, height: 22, borderRadius: 11, cursor: 'pointer', transition: 'all 0.2s', background: cfgSyncStock ? 'var(--accent-cyan)' : 'var(--bg-secondary)', border: `1px solid ${cfgSyncStock ? 'var(--accent-cyan)' : 'var(--border-bright)'}`, position: 'relative', flexShrink: 0 }}>
                  <div style={{ width: 16, height: 16, borderRadius: '50%', background: '#fff', position: 'absolute', top: 2, left: cfgSyncStock ? 20 : 2, transition: 'left 0.2s', boxShadow: '0 1px 3px rgba(0,0,0,0.3)' }} />
                </div>
              </div>
            </>)}
          </div>
        );

        return (
          <div style={{ position: 'fixed', inset: 0, zIndex: 1030, display: 'flex', alignItems: 'center', justifyContent: 'center' }} onClick={() => setModalOpen(false)}>
            <div style={{ position: 'absolute', inset: 0, background: 'rgba(0,0,0,0.75)', backdropFilter: 'blur(6px)' }} />
            <div onClick={e => e.stopPropagation()} style={{ position: 'relative', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 18, width: 560, maxWidth: '92vw', maxHeight: '88vh', overflow: 'hidden', display: 'flex', flexDirection: 'column' }}>

              {/* Header */}
              <div style={{ padding: '18px 24px', borderBottom: '1px solid var(--border)', display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexShrink: 0 }}>
                <div>
                  <h3 style={{ fontSize: 17, fontWeight: 700, margin: 0 }}>Start Import</h3>
                  <div style={{ display: 'flex', gap: 6, marginTop: 8 }}>
                    {stepLabels.map((label, i) => (
                      <div key={i} style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
                        <div style={{ width: 20, height: 20, borderRadius: '50%', fontSize: 11, fontWeight: 700, display: 'flex', alignItems: 'center', justifyContent: 'center', background: i < stepIdx ? 'var(--success)' : i === stepIdx ? 'var(--primary)' : 'var(--bg-tertiary)', color: i <= stepIdx ? '#fff' : 'var(--text-muted)', border: `1px solid ${i < stepIdx ? 'var(--success)' : i === stepIdx ? 'var(--primary)' : 'var(--border-bright)'}`, transition: 'all 0.2s' }}>{i < stepIdx ? '✓' : i + 1}</div>
                        <span style={{ fontSize: 11, color: i === stepIdx ? 'var(--text-primary)' : 'var(--text-muted)', fontWeight: i === stepIdx ? 600 : 400 }}>{label}</span>
                        {i < stepLabels.length - 1 && <span style={{ color: 'var(--border-bright)', fontSize: 11, marginLeft: 2 }}>›</span>}
                      </div>
                    ))}
                  </div>
                </div>
                <button onClick={() => setModalOpen(false)} style={{ background: 'none', border: 'none', color: 'var(--text-muted)', cursor: 'pointer', fontSize: 20, lineHeight: 1, padding: 4 }}>✕</button>
              </div>

              {/* Body */}
              <div style={{ flex: 1, overflowY: 'auto', padding: 24 }}>

                {/* ── STEP 1: BUILD QUEUE ── */}
                {modalStep === 'configure' && (
                  <div>
                    {/* Queued imports */}
                    {queue.length > 0 && (
                      <div style={{ marginBottom: 20 }}>
                        <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.08em', marginBottom: 8 }}>Queued Imports ({queue.length})</div>
                        <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                          {queue.map(item => (
                            <div key={item.id} style={{ display: 'flex', alignItems: 'center', gap: 12, padding: '10px 14px', borderRadius: 10, background: 'var(--bg-tertiary)', border: '1px solid var(--border-bright)' }}>
                              <span style={{ fontSize: 20 }}>{adapterEmoji[item.channel] || '🌐'}</span>
                              <div style={{ flex: 1 }}>
                                <div style={{ fontWeight: 600, fontSize: 14 }}>{item.accountName}</div>
                                <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 2 }}>
                                  {item.channel} · Full Import
                                  {item.channel === 'amazon' && item.fulfillmentFilter !== 'all' && ` · ${item.fulfillmentFilter.toUpperCase()}`}
                                  {item.enrichData && ' · ✨ Enriched'}
                                  {item.syncStock && ' · 📦 Stock sync'}
                                </div>
                              </div>
                              <button onClick={() => setQueue(prev => prev.filter(q => q.id !== item.id))} style={{ background: 'none', border: 'none', color: 'var(--text-muted)', cursor: 'pointer', fontSize: 16, padding: '2px 6px', borderRadius: 4 }} title="Remove">✕</button>
                            </div>
                          ))}
                        </div>
                        {availableCreds.length > 0 && <div style={{ height: 1, background: 'var(--border)', margin: '20px 0' }} />}
                      </div>
                    )}

                    {/* Add another import form */}
                    {availableCreds.length > 0 ? (
                      <div>
                        {queue.length > 0 && <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 14 }}>Add another import</div>}
                        {importConfigFormJsx}
                        {cfgCredential && (
                          <button onClick={addToQueue} disabled={!cfgValid} style={{ width: '100%', padding: '10px 0', marginTop: 16, borderRadius: 8, border: '1px dashed var(--primary)', background: 'var(--primary-glow)', color: 'var(--primary)', fontSize: 14, fontWeight: 600, cursor: cfgValid ? 'pointer' : 'not-allowed', opacity: cfgValid ? 1 : 0.5 }}>
                            + Add to Queue
                          </button>
                        )}
                      </div>
                    ) : (
                      queue.length === 0 && (
                        <div style={{ textAlign: 'center', padding: '32px 0', color: 'var(--text-muted)' }}>
                          <div style={{ fontSize: 32, marginBottom: 8 }}>🔌</div>
                          <div style={{ fontWeight: 600 }}>No marketplace accounts connected</div>
                        </div>
                      )
                    )}

                    {/* If no account selected yet but queue is empty, show the form without add button */}
                    {!cfgCredential && queue.length === 0 && availableCreds.length > 0 && importConfigFormJsx}
                  </div>
                )}

                {/* ── STEP 2: INVENTORY SYNC ── */}
                {modalStep === 'inventory' && (
                  <div>
                    <div style={{ marginBottom: 24, textAlign: 'center' }}>
                      <div style={{ fontSize: 36, marginBottom: 8 }}>📦</div>
                      <h4 style={{ fontSize: 16, fontWeight: 700, margin: '0 0 8px' }}>Update Inventory Levels?</h4>
                      <p style={{ fontSize: 13, color: 'var(--text-muted)', margin: 0 }}>Import stock quantities from one of your marketplace accounts into your product catalog</p>
                    </div>
                    <div style={{ display: 'flex', gap: 12, marginBottom: 20 }}>
                      {[{ v: true, l: 'Yes, sync inventory', icon: '✅' }, { v: false, l: 'No, skip', icon: '⏭' }].map(opt => (
                        <div key={String(opt.v)} onClick={() => setInventorySync(opt.v)} style={{ flex: 1, padding: 16, borderRadius: 12, cursor: 'pointer', textAlign: 'center', transition: 'all 0.2s', background: inventorySync === opt.v ? 'var(--primary-glow)' : 'var(--bg-tertiary)', border: `2px solid ${inventorySync === opt.v ? 'var(--primary)' : 'var(--border-bright)'}` }}>
                          <div style={{ fontSize: 24, marginBottom: 6 }}>{opt.icon}</div>
                          <div style={{ fontWeight: 600, fontSize: 14, color: inventorySync === opt.v ? 'var(--primary)' : 'var(--text-primary)' }}>{opt.l}</div>
                        </div>
                      ))}
                    </div>
                    {inventorySync && (
                      <div>
                        <label style={{ display: 'block', fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', marginBottom: 6, textTransform: 'uppercase', letterSpacing: '0.08em' }}>Source Marketplace</label>
                        <select className="select" style={{ width: '100%' }} value={inventorySource} onChange={e => setInventorySource(e.target.value)}>
                          <option value="">— Select source —</option>
                          {queue.map(item => (
                            <option key={item.credentialId} value={item.credentialId}>{adapterEmoji[item.channel] || '🌐'} {item.accountName}</option>
                          ))}
                        </select>
                      </div>
                    )}
                  </div>
                )}

                {/* ── STEP 3: LISTING GENERATION ── */}
                {modalStep === 'listings' && (
                  <div>
                    <div style={{ marginBottom: 24, textAlign: 'center' }}>
                      <div style={{ fontSize: 36, marginBottom: 8 }}>🤖</div>
                      <h4 style={{ fontSize: 16, fontWeight: 700, margin: '0 0 8px' }}>Auto-generate Missing Listings?</h4>
                      <p style={{ fontSize: 13, color: 'var(--text-muted)', margin: 0 }}>Once imports finish, automatically create draft listings on other marketplaces for products that don't have them yet</p>
                    </div>
                    <div style={{ display: 'flex', gap: 12, marginBottom: 20 }}>
                      {[{ v: true, l: 'Yes, generate drafts', icon: '✅' }, { v: false, l: 'No, skip', icon: '⏭' }].map(opt => (
                        <div key={String(opt.v)} onClick={() => setGenerateListings(opt.v)} style={{ flex: 1, padding: 16, borderRadius: 12, cursor: 'pointer', textAlign: 'center', transition: 'all 0.2s', background: generateListings === opt.v ? 'var(--primary-glow)' : 'var(--bg-tertiary)', border: `2px solid ${generateListings === opt.v ? 'var(--primary)' : 'var(--border-bright)'}` }}>
                          <div style={{ fontSize: 24, marginBottom: 6 }}>{opt.icon}</div>
                          <div style={{ fontWeight: 600, fontSize: 14, color: generateListings === opt.v ? 'var(--primary)' : 'var(--text-primary)' }}>{opt.l}</div>
                        </div>
                      ))}
                    </div>
                    {generateListings && (() => {
                      const importedChannels = queue.map(q => q.channel);
                      const targetCreds = activeCreds.filter(c => !importedChannels.includes(c.channel));
                      return (
                        <div>
                          {targetCreds.length > 0 ? (
                            <div style={{ marginBottom: 20 }}>
                              <label style={{ display: 'block', fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', marginBottom: 8, textTransform: 'uppercase', letterSpacing: '0.08em' }}>Target Marketplaces</label>
                              <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                                {targetCreds.map(c => {
                                  const checked = listingTargets.includes(c.credential_id);
                                  return (
                                    <label key={c.credential_id} style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '10px 12px', borderRadius: 8, cursor: 'pointer', background: checked ? 'var(--primary-glow)' : 'var(--bg-tertiary)', border: `1px solid ${checked ? 'var(--primary)' : 'var(--border-bright)'}` }}>
                                      <input type="checkbox" checked={checked} onChange={() => setListingTargets(prev => prev.includes(c.credential_id) ? prev.filter(id => id !== c.credential_id) : [...prev, c.credential_id])} style={{ accentColor: 'var(--primary)' }} />
                                      <span style={{ fontSize: 18 }}>{adapterEmoji[c.channel] || '🌐'}</span>
                                      <div style={{ fontWeight: 600, fontSize: 13, color: checked ? 'var(--primary)' : 'var(--text-primary)' }}>{c.account_name} <span style={{ fontWeight: 400, color: 'var(--text-muted)' }}>({c.channel})</span></div>
                                    </label>
                                  );
                                })}
                              </div>
                            </div>
                          ) : (
                            <div style={{ padding: 16, borderRadius: 8, background: 'var(--bg-tertiary)', color: 'var(--text-muted)', fontSize: 13, textAlign: 'center', marginBottom: 16 }}>
                              All connected accounts are already in your import queue
                            </div>
                          )}
                          <div style={{ padding: 14, borderRadius: 10, background: 'rgba(150,100,255,0.08)', border: '1px solid rgba(150,100,255,0.3)' }}>
                            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                              <div>
                                <div style={{ fontWeight: 600, fontSize: 13 }}>🧠 Use AI to enrich new listings?</div>
                                <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 2 }}>Generate optimised titles, descriptions & attributes. Uses AI credits.</div>
                              </div>
                              <div onClick={() => setListingAiEnrich(!listingAiEnrich)} style={{ width: 40, height: 22, borderRadius: 11, cursor: 'pointer', transition: 'all 0.2s', background: listingAiEnrich ? 'rgba(150,100,255,0.8)' : 'var(--bg-secondary)', border: `1px solid ${listingAiEnrich ? 'rgba(150,100,255,0.8)' : 'var(--border-bright)'}`, position: 'relative', flexShrink: 0 }}>
                                <div style={{ width: 16, height: 16, borderRadius: '50%', background: '#fff', position: 'absolute', top: 2, left: listingAiEnrich ? 20 : 2, transition: 'left 0.2s', boxShadow: '0 1px 3px rgba(0,0,0,0.3)' }} />
                              </div>
                            </div>
                          </div>
                        </div>
                      );
                    })()}
                  </div>
                )}

                {/* ── STEP 4: CONFIRM ── */}
                {modalStep === 'confirm' && (
                  <div>
                    <div style={{ marginBottom: 20, textAlign: 'center' }}>
                      <div style={{ fontSize: 36, marginBottom: 8 }}>🚀</div>
                      <h4 style={{ fontSize: 16, fontWeight: 700, margin: '0 0 6px' }}>Ready to Start</h4>
                      <p style={{ fontSize: 13, color: 'var(--text-muted)', margin: 0 }}>{queue.length} import{queue.length !== 1 ? 's' : ''} will start simultaneously</p>
                    </div>
                    <div style={{ display: 'flex', flexDirection: 'column', gap: 8, marginBottom: 16 }}>
                      {queue.map(item => (
                        <div key={item.id} style={{ display: 'flex', alignItems: 'center', gap: 12, padding: '12px 14px', borderRadius: 10, background: 'var(--bg-tertiary)', border: '1px solid var(--border-bright)' }}>
                          <span style={{ fontSize: 22 }}>{adapterEmoji[item.channel] || '🌐'}</span>
                          <div style={{ flex: 1 }}>
                            <div style={{ fontWeight: 600, fontSize: 14 }}>{item.accountName}</div>
                            <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 2 }}>
                              {[
                                'Full Import',
                                item.channel === 'amazon' && item.fulfillmentFilter !== 'all' && item.fulfillmentFilter.toUpperCase(),
                                item.enrichData && '✨ Enriched',
                                item.syncStock && '📦 Stock sync',
                              ].filter(Boolean).join(' · ')}
                            </div>
                          </div>
                          <span style={{ fontSize: 18, color: 'var(--success)' }}>✓</span>
                        </div>
                      ))}
                    </div>
                    {(inventorySync || generateListings) && (
                      <div style={{ padding: 14, borderRadius: 10, background: 'var(--bg-tertiary)', border: '1px solid var(--border-bright)', display: 'flex', flexDirection: 'column', gap: 8 }}>
                        {inventorySync && (
                          <div style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13 }}>
                            <span>📦</span>
                            <span>Inventory sync from <strong>{queue.find(q => q.credentialId === inventorySource)?.accountName || inventorySource}</strong></span>
                          </div>
                        )}
                        {generateListings && (
                          <div style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13 }}>
                            <span>🤖</span>
                            <span>Auto-generate listings on {listingTargets.length} marketplace{listingTargets.length !== 1 ? 's' : ''}{listingAiEnrich ? ' with AI enrichment' : ''}</span>
                          </div>
                        )}
                      </div>
                    )}
                  </div>
                )}
              </div>

              {/* Footer */}
              <div style={{ padding: '16px 24px', borderTop: '1px solid var(--border)', display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexShrink: 0, background: 'var(--bg-secondary)' }}>
                <button className="btn btn-secondary" onClick={() => {
                  if (modalStep === 'configure') setModalOpen(false);
                  else if (modalStep === 'inventory') setModalStep('configure');
                  else if (modalStep === 'listings') setModalStep('inventory');
                  else if (modalStep === 'confirm') setModalStep('listings');
                }}>{modalStep === 'configure' ? 'Cancel' : '← Back'}</button>

                {modalStep === 'configure' && (
                  <button className="btn btn-primary" disabled={queue.length === 0} onClick={() => setModalStep('inventory')}>
                    Continue → ({queue.length} queued)
                  </button>
                )}
                {modalStep === 'inventory' && (
                  <button className="btn btn-primary" disabled={inventorySync && !inventorySource} onClick={() => setModalStep('listings')}>
                    Continue →
                  </button>
                )}
                {modalStep === 'listings' && (
                  <button className="btn btn-primary" onClick={() => setModalStep('confirm')}>
                    Continue →
                  </button>
                )}
                {modalStep === 'confirm' && (
                  <button className="btn btn-primary" disabled={starting || queue.length === 0} onClick={handleStartAllImports}>
                    {starting ? '⏳ Starting...' : `🚀 Start ${queue.length} Import${queue.length !== 1 ? 's' : ''}`}
                  </button>
                )}
              </div>
            </div>
          </div>
        );
      })()}
    </div>
  );
}

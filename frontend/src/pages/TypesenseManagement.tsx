// ============================================================================
// TYPESENSE MANAGEMENT — Search Health Control Panel
// ============================================================================
// Location: frontend/src/pages/TypesenseManagement.tsx
//
// Self-service control panel for non-technical users to diagnose and fix
// Typesense search issues without command-line access.

import { useState, useEffect, useCallback } from 'react';

const API_BASE = (import.meta as any).env?.VITE_API_URL || 'http://localhost:8080/api/v1';

interface TenantInfo {
  tenant_id: string;
  name: string;
  initials?: string;
  color?: string;
}

interface TenantSyncState {
  tenant_id: string;
  name: string;
  syncing: boolean;
  lastResult: null | { products?: { indexed?: number; error?: string }; listings?: { indexed?: number; error?: string } };
}

type HealthStatus = 'checking' | 'healthy' | 'degraded' | 'down';

export default function TypesenseManagement() {
  // ── Health state ──
  const [health, setHealth] = useState<HealthStatus>('checking');
  const [lastChecked, setLastChecked] = useState<Date | null>(null);
  const [collectionsInfo, setCollectionsInfo] = useState<any>(null);

  // ── Tenant state ──
  const [tenants, setTenants] = useState<TenantSyncState[]>([]);
  const [loadingTenants, setLoadingTenants] = useState(true);

  // ── Global actions ──
  const [syncingAll, setSyncingAll] = useState(false);
  const [rebuildingSchema, setRebuildingSchema] = useState(false);
  const [actionLog, setActionLog] = useState<Array<{ time: Date; msg: string; type: 'info' | 'success' | 'error' }>>([]);

  // ── Connectivity fix ──
  const [typesenseIP, setTypesenseIP] = useState('');
  const [reconnecting, setReconnecting] = useState(false);
  const [reconnectResult, setReconnectResult] = useState<{ ok: boolean; message: string } | null>(null);

  // ── VM restart ──
  const [restarting, setRestarting] = useState(false);
  const [restartResult, setRestartResult] = useState<{ ok: boolean; message: string } | null>(null);

  const activeTenantId = localStorage.getItem('active_tenant_id') || 'tenant-demo';

  const log = useCallback((msg: string, type: 'info' | 'success' | 'error' = 'info') => {
    setActionLog(prev => [{ time: new Date(), msg, type }, ...prev].slice(0, 50));
  }, []);

  // ── API helper ──
  const apiFetch = useCallback(async (path: string, options?: RequestInit & { tenantId?: string }) => {
    const { tenantId, ...fetchOpts } = options || {};
    return fetch(`${API_BASE}${path}`, {
      ...fetchOpts,
      headers: {
        'Content-Type': 'application/json',
        'X-Tenant-Id': tenantId || activeTenantId,
        ...(fetchOpts?.headers || {}),
      },
    });
  }, [activeTenantId]);

  // ── Health check ──
  const checkHealth = useCallback(async () => {
    setHealth('checking');
    try {
      const res = await apiFetch('/search/health');
      if (res.ok) {
        const data = await res.json();
        setHealth(data.status === 'ok' ? 'healthy' : 'degraded');
      } else {
        setHealth('down');
      }
    } catch {
      setHealth('down');
    }
    setLastChecked(new Date());
  }, [apiFetch]);

  // ── Load tenants ──
  const loadTenants = useCallback(async () => {
    setLoadingTenants(true);
    try {
      const res = await apiFetch('/tenants');
      if (res.ok) {
        const data = await res.json();
        const list: TenantInfo[] = data.data || data || [];
        setTenants(list.map(t => ({
          tenant_id: t.tenant_id,
          name: t.name,
          syncing: false,
          lastResult: null,
        })));
      }
    } catch (e) {
      log('Failed to load tenants', 'error');
    } finally {
      setLoadingTenants(false);
    }
  }, [apiFetch, log]);

  // ── On mount ──
  useEffect(() => {
    checkHealth();
    loadTenants();
  }, []);

  // ── Sync single tenant ──
  const syncTenant = async (tenantId: string) => {
    setTenants(prev => prev.map(t =>
      t.tenant_id === tenantId ? { ...t, syncing: true, lastResult: null } : t
    ));
    log(`Syncing ${tenantId}...`);
    try {
      const res = await apiFetch('/search/sync', {
        method: 'POST',
        body: JSON.stringify({}),
        tenantId,
      });
      if (res.ok) {
        const data = await res.json();
        setTenants(prev => prev.map(t =>
          t.tenant_id === tenantId ? { ...t, syncing: false, lastResult: data } : t
        ));
        const pCount = data.products?.indexed ?? '?';
        const lCount = data.listings?.indexed ?? '?';
        log(`✓ ${tenantId}: ${pCount} products, ${lCount} listings indexed`, 'success');
      } else {
        const errText = await res.text();
        setTenants(prev => prev.map(t =>
          t.tenant_id === tenantId ? { ...t, syncing: false, lastResult: { products: { error: errText } } } : t
        ));
        log(`✗ ${tenantId}: ${errText}`, 'error');
      }
    } catch (e: any) {
      setTenants(prev => prev.map(t =>
        t.tenant_id === tenantId ? { ...t, syncing: false, lastResult: { products: { error: e.message } } } : t
      ));
      log(`✗ ${tenantId}: ${e.message}`, 'error');
    }
  };

  // ── Sync all tenants ──
  const syncAllTenants = async () => {
    setSyncingAll(true);
    log('Starting full sync across all tenants...');
    for (const tenant of tenants) {
      await syncTenant(tenant.tenant_id);
    }
    setSyncingAll(false);
    log('Full sync complete', 'success');
    checkHealth();
  };

  // ── Rebuild schema (re-trigger EnsureCollections + full sync) ──
  const rebuildSchema = async () => {
    if (!confirm('This will drop and recreate the search schema, then re-sync all data. Continue?')) return;
    setRebuildingSchema(true);
    log('Rebuilding search schema...');

    // There's no dedicated schema endpoint, so we sync which triggers EnsureCollections on the backend
    // on startup. For a manual rebuild, we sync all tenants.
    await syncAllTenants();

    setRebuildingSchema(false);
    log('Schema rebuild complete', 'success');
  };

  // ── Restart Typesense VM ──
  const restartVM = async () => {
    if (!confirm(
      'This will reboot the Typesense VM. The search engine will be unavailable for ~30–60 seconds while it restarts.\n\nOnly do this if the Typesense process has crashed and the Fix Connection button hasn\'t helped.\n\nContinue?'
    )) return;

    setRestarting(true);
    setRestartResult(null);
    log('Sending VM reset command to GCP...');
    try {
      const res = await fetch(`${API_BASE.replace('/api/v1', '')}/api/v1/admin/search/restart-vm`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
      });
      const data = await res.json();
      setRestartResult({ ok: data.ok, message: data.message ?? data.error });
      if (data.ok) {
        log(`✓ ${data.message}`, 'success');
        // Re-check health after 45s to show when it's back
        setTimeout(() => checkHealth(), 45000);
      } else {
        log(`✗ ${data.error ?? data.message}`, 'error');
      }
    } catch (e: any) {
      const msg = `VM restart request failed: ${e.message}`;
      setRestartResult({ ok: false, message: msg });
      log(`✗ ${msg}`, 'error');
    } finally {
      setRestarting(false);
    }
  };

  // ── Fix Typesense connectivity ──
  const fixConnectivity = async () => {
    const ip = typesenseIP.trim();
    if (!ip) {
      alert('Please enter the Typesense server IP address first.');
      return;
    }
    // Accept either a bare IP or a full URL
    const url = ip.startsWith('http') ? ip : `http://${ip}:8108`;
    setReconnecting(true);
    setReconnectResult(null);
    log(`Attempting to connect to Typesense at ${url}...`);
    try {
      const res = await apiFetch('/search/reconnect', {
        method: 'POST',
        body: JSON.stringify({ typesense_url: url }),
      });
      const data = await res.json();
      setReconnectResult({ ok: data.ok, message: data.message });
      if (data.ok) {
        log(`✓ ${data.message}`, 'success');
        checkHealth();
      } else {
        log(`✗ ${data.message}`, 'error');
      }
    } catch (e: any) {
      const msg = `Connection attempt failed: ${e.message}`;
      setReconnectResult({ ok: false, message: msg });
      log(`✗ ${msg}`, 'error');
    } finally {
      setReconnecting(false);
    }
  };

  // ── Status helpers ──
  const statusConfig: Record<HealthStatus, { label: string; color: string; glow: string; icon: string }> = {
    checking: { label: 'Checking...', color: 'var(--warning)', glow: 'var(--warning-glow)', icon: '⏳' },
    healthy: { label: 'Healthy', color: 'var(--success)', glow: 'var(--success-glow)', icon: '✓' },
    degraded: { label: 'Degraded', color: 'var(--warning)', glow: 'var(--warning-glow)', icon: '⚠' },
    down: { label: 'Down', color: 'var(--danger)', glow: 'var(--danger-glow)', icon: '✗' },
  };

  const sc = statusConfig[health];

  return (
    <div style={{ padding: 'var(--spacing-2xl)', maxWidth: 1200, margin: '0 auto' }}>
      {/* Page header */}
      <div style={{ marginBottom: 'var(--spacing-2xl)' }}>
        <h1 style={{ fontSize: 28, fontWeight: 700, marginBottom: 4 }}>Search Engine Management</h1>
        <p style={{ fontSize: 14, color: 'var(--text-secondary)' }}>
          Monitor and manage Typesense search indexing across all tenants
        </p>
      </div>

      {/* ── Status + global actions row ── */}
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 'var(--spacing-lg)', marginBottom: 'var(--spacing-lg)' }}>

        {/* Health card */}
        <div className="card">
          <div className="card-header">
            <h2 style={{ fontSize: 16, fontWeight: 600 }}>Engine Status</h2>
            <button className="btn btn-secondary" onClick={checkHealth} style={{ fontSize: 12, padding: '6px 12px' }}>
              ↻ Refresh
            </button>
          </div>
          <div style={{ padding: 'var(--spacing-xl)' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 16, marginBottom: 20 }}>
              {/* Status indicator */}
              <div style={{
                width: 56, height: 56, borderRadius: 12,
                background: sc.glow, border: `2px solid ${sc.color}`,
                display: 'flex', alignItems: 'center', justifyContent: 'center',
                fontSize: 24, fontWeight: 700, color: sc.color,
                boxShadow: `0 0 20px ${sc.glow}`,
                animation: health === 'checking' ? 'pulse 1.5s ease-in-out infinite' : undefined,
              }}>
                {sc.icon}
              </div>
              <div>
                <div style={{ fontSize: 20, fontWeight: 700, color: sc.color }}>{sc.label}</div>
                <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                  {lastChecked ? `Checked ${lastChecked.toLocaleTimeString()}` : 'Never checked'}
                </div>
              </div>
            </div>

            {health === 'down' && (
              <div style={{
                padding: 'var(--spacing-md)', borderRadius: 8,
                background: 'var(--danger-glow)', border: '1px solid var(--danger)',
                fontSize: 13, color: 'var(--text-primary)', lineHeight: 1.6,
              }}>
                <strong>Search engine is unreachable.</strong> This usually means:<br />
                • The Typesense server has crashed or the Raft state is corrupt<br />
                • The VM or container is stopped<br />
                • Network connectivity between the backend and Typesense is broken<br /><br />
                <strong>What to try:</strong> If this persists, the Typesense container may need a restart on the server. Contact your admin with this status.
              </div>
            )}

            {health === 'healthy' && (
              <div style={{
                padding: 'var(--spacing-md)', borderRadius: 8,
                background: 'var(--success-glow)', border: '1px solid var(--success)',
                fontSize: 13, color: 'var(--text-primary)',
              }}>
                Search engine is running and accepting requests. If search results seem stale or incomplete, use "Sync All Tenants" below to reindex.
              </div>
            )}
          </div>
        </div>

        {/* Global actions card */}
        <div className="card">
          <div className="card-header">
            <h2 style={{ fontSize: 16, fontWeight: 600 }}>Actions</h2>
          </div>
          <div style={{ padding: 'var(--spacing-xl)', display: 'flex', flexDirection: 'column', gap: 12 }}>
            {/* Sync all */}
            <button
              className="btn btn-primary"
              onClick={syncAllTenants}
              disabled={syncingAll || health === 'down'}
              style={{ width: '100%', justifyContent: 'center', opacity: (syncingAll || health === 'down') ? 0.5 : 1 }}
            >
              {syncingAll ? (
                <><span className="spinner" style={{ width: 16, height: 16, borderWidth: 2, margin: 0, marginRight: 8 }} /> Syncing...</>
              ) : (
                <>🔄 Sync All Tenants</>
              )}
            </button>
            <div style={{ fontSize: 12, color: 'var(--text-muted)', paddingLeft: 4 }}>
              Reindexes all products and listings for every tenant into the search engine.
            </div>

            {/* Rebuild schema */}
            <button
              className="btn btn-secondary"
              onClick={rebuildSchema}
              disabled={rebuildingSchema || syncingAll || health === 'down'}
              style={{
                width: '100%', justifyContent: 'center',
                opacity: (rebuildingSchema || syncingAll || health === 'down') ? 0.5 : 1,
                marginTop: 8,
              }}
            >
              {rebuildingSchema ? 'Rebuilding...' : '🔧 Rebuild Search Schema'}
            </button>
            <div style={{ fontSize: 12, color: 'var(--text-muted)', paddingLeft: 4 }}>
              Recreates search collections and reindexes everything. Use if search returns errors or unexpected results.
            </div>

            {/* Health recheck */}
            <button
              className="btn btn-secondary"
              onClick={checkHealth}
              style={{ width: '100%', justifyContent: 'center', marginTop: 8 }}
            >
              🏥 Check Health
            </button>
          </div>
        </div>
      </div>

      {/* ── Connectivity Fix Panel ── */}
      <div className="card" style={{ marginBottom: 'var(--spacing-lg)', borderColor: health === 'down' ? 'var(--danger)' : undefined }}>
        <div className="card-header">
          <h2 style={{ fontSize: 16, fontWeight: 600 }}>🔌 Fix Typesense Connection</h2>
          <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>Use when Status shows "Down"</span>
        </div>
        <div style={{ padding: 'var(--spacing-xl)' }}>
          <p style={{ fontSize: 13, color: 'var(--text-secondary)', marginBottom: 16, lineHeight: 1.6 }}>
            If the search engine is unreachable, the backend may have lost the Typesense server's IP address
            (this can happen after a Cloud Run redeploy). Enter the Typesense VM's internal IP below and click
            Fix Connection — no scripts or command line needed.
          </p>
          <p style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 16 }}>
            To find the IP: run <code style={{ background: 'var(--bg-tertiary)', padding: '2px 6px', borderRadius: 4 }}>
              gcloud compute instances describe typesense-server --zone=europe-west2-a --format="get(networkInterfaces[0].networkIP)"
            </code> in PowerShell.
          </p>
          <div style={{ display: 'flex', gap: 12, alignItems: 'flex-start', flexWrap: 'wrap' }}>
            <input
              type="text"
              value={typesenseIP}
              onChange={e => { setTypesenseIP(e.target.value); setReconnectResult(null); }}
              placeholder="e.g. 10.128.0.5"
              style={{
                flex: 1, minWidth: 200, maxWidth: 320,
                background: 'var(--bg-tertiary)',
                border: '1px solid var(--border)',
                borderRadius: 8, padding: '10px 14px',
                color: 'var(--text-primary)', fontSize: 14,
                outline: 'none',
              }}
              onKeyDown={e => e.key === 'Enter' && fixConnectivity()}
            />
            <button
              className="btn btn-primary"
              onClick={fixConnectivity}
              disabled={reconnecting || !typesenseIP.trim()}
              style={{ opacity: (reconnecting || !typesenseIP.trim()) ? 0.5 : 1, whiteSpace: 'nowrap' }}
            >
              {reconnecting ? (
                <><span className="spinner" style={{ width: 14, height: 14, borderWidth: 2, margin: 0, marginRight: 8 }} />Connecting...</>
              ) : '🔌 Fix Connection'}
            </button>
          </div>

          {reconnectResult && (
            <div style={{
              marginTop: 16, padding: 'var(--spacing-md)', borderRadius: 8,
              background: reconnectResult.ok ? 'var(--success-glow)' : 'var(--danger-glow)',
              border: `1px solid ${reconnectResult.ok ? 'var(--success)' : 'var(--danger)'}`,
              fontSize: 13, color: 'var(--text-primary)', lineHeight: 1.6,
            }}>
              {reconnectResult.ok ? '✅ ' : '❌ '}{reconnectResult.message}
              {reconnectResult.ok && (
                <div style={{ marginTop: 8, color: 'var(--text-secondary)', fontSize: 12 }}>
                  <strong>Note:</strong> This fix lasts until the next backend redeploy. To make it permanent, run your deploy command
                  with <code style={{ background: 'var(--bg-tertiary)', padding: '2px 6px', borderRadius: 4 }}>
                    --update-env-vars="TYPESENSE_URL=http://{typesenseIP.trim().startsWith('http') ? typesenseIP.trim().replace('http://', '').replace(':8108','') : typesenseIP.trim()}:8108"
                  </code> added to it.
                </div>
              )}
            </div>
          )}
        </div>
      </div>

      {/* ── Restart VM Panel ── */}
      <div className="card" style={{ marginBottom: 'var(--spacing-lg)', borderColor: 'var(--warning)' }}>
        <div className="card-header">
          <h2 style={{ fontSize: 16, fontWeight: 600 }}>🔁 Restart Typesense Server</h2>
          <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>Use when the process has crashed</span>
        </div>
        <div style={{ padding: 'var(--spacing-xl)' }}>
          <p style={{ fontSize: 13, color: 'var(--text-secondary)', marginBottom: 12, lineHeight: 1.6 }}>
            Reboots the <code style={{ background: 'var(--bg-tertiary)', padding: '2px 6px', borderRadius: 4 }}>typesense-server</code> GCE VM via the GCP Compute Engine API.
            Docker will automatically restart the Typesense container on boot (restart=always).
            The search engine will be unavailable for <strong>~30–60 seconds</strong>.
          </p>
          <p style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 16 }}>
            Equivalent to running: <code style={{ background: 'var(--bg-tertiary)', padding: '2px 6px', borderRadius: 4 }}>
              docker restart typesense
            </code> on the VM. Only needed if the container has crashed — if Status shows "Down" but the VM is running, try <strong>Fix Connection</strong> first.
          </p>

          <button
            className="btn btn-secondary"
            onClick={restartVM}
            disabled={restarting}
            style={{
              borderColor: 'var(--warning)',
              color: 'var(--warning)',
              opacity: restarting ? 0.6 : 1,
            }}
          >
            {restarting ? (
              <><span className="spinner" style={{ width: 14, height: 14, borderWidth: 2, margin: 0, marginRight: 8 }} />Restarting VM...</>
            ) : '🔁 Restart Typesense VM'}
          </button>

          {restartResult && (
            <div style={{
              marginTop: 16, padding: 'var(--spacing-md)', borderRadius: 8,
              background: restartResult.ok ? 'var(--success-glow)' : 'var(--danger-glow)',
              border: `1px solid ${restartResult.ok ? 'var(--success)' : 'var(--danger)'}`,
              fontSize: 13, color: 'var(--text-primary)', lineHeight: 1.6,
            }}>
              {restartResult.ok ? '✅ ' : '❌ '}{restartResult.message}
              {restartResult.ok && (
                <div style={{ marginTop: 8, color: 'var(--text-muted)', fontSize: 12 }}>
                  Health check will run automatically in ~45 seconds. You can also click <strong>Refresh</strong> on the Engine Status card manually.
                </div>
              )}
            </div>
          )}
        </div>
      </div>

      {/* ── Per-tenant sync table ── */}
      <div className="card" style={{ marginBottom: 'var(--spacing-lg)' }}>
        <div className="card-header">
          <h2 style={{ fontSize: 16, fontWeight: 600 }}>Tenant Index Status</h2>
          <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>{tenants.length} tenants</div>
        </div>
        <div style={{ overflowX: 'auto' }}>
          <table className="table">
            <thead>
              <tr>
                <th>Tenant</th>
                <th>Products Indexed</th>
                <th>Listings Indexed</th>
                <th>Status</th>
                <th style={{ textAlign: 'right' }}>Action</th>
              </tr>
            </thead>
            <tbody>
              {loadingTenants ? (
                <tr>
                  <td colSpan={5} style={{ textAlign: 'center', padding: 40, color: 'var(--text-muted)' }}>
                    Loading tenants...
                  </td>
                </tr>
              ) : tenants.length === 0 ? (
                <tr>
                  <td colSpan={5} style={{ textAlign: 'center', padding: 40, color: 'var(--text-muted)' }}>
                    No tenants found
                  </td>
                </tr>
              ) : tenants.map(t => {
                const pResult = t.lastResult?.products;
                const lResult = t.lastResult?.listings;
                const hasError = pResult?.error || lResult?.error;
                const hasSynced = t.lastResult !== null;

                return (
                  <tr key={t.tenant_id}>
                    <td>
                      <div style={{ fontWeight: 600, fontSize: 14 }}>{t.name}</div>
                      <div style={{ fontSize: 11, color: 'var(--text-muted)', fontFamily: 'monospace' }}>{t.tenant_id}</div>
                    </td>
                    <td>
                      {t.syncing ? (
                        <span style={{ color: 'var(--warning)', fontSize: 13 }}>syncing...</span>
                      ) : hasSynced ? (
                        pResult?.error ? (
                          <span style={{ color: 'var(--danger)', fontSize: 13 }}>Error</span>
                        ) : (
                          <span style={{ fontSize: 14, fontWeight: 600 }}>{pResult?.indexed ?? '—'}</span>
                        )
                      ) : (
                        <span style={{ color: 'var(--text-muted)', fontSize: 13 }}>—</span>
                      )}
                    </td>
                    <td>
                      {t.syncing ? (
                        <span style={{ color: 'var(--warning)', fontSize: 13 }}>syncing...</span>
                      ) : hasSynced ? (
                        lResult?.error ? (
                          <span style={{ color: 'var(--danger)', fontSize: 13 }}>Error</span>
                        ) : (
                          <span style={{ fontSize: 14, fontWeight: 600 }}>{lResult?.indexed ?? '—'}</span>
                        )
                      ) : (
                        <span style={{ color: 'var(--text-muted)', fontSize: 13 }}>—</span>
                      )}
                    </td>
                    <td>
                      {t.syncing ? (
                        <span className="badge badge-warning">Syncing</span>
                      ) : hasError ? (
                        <span className="badge badge-danger">Error</span>
                      ) : hasSynced ? (
                        <span className="badge badge-success">Synced</span>
                      ) : (
                        <span className="badge badge-info">Pending</span>
                      )}
                    </td>
                    <td style={{ textAlign: 'right' }}>
                      <button
                        className="btn btn-secondary"
                        onClick={() => syncTenant(t.tenant_id)}
                        disabled={t.syncing || health === 'down'}
                        style={{ fontSize: 12, padding: '6px 14px', opacity: (t.syncing || health === 'down') ? 0.5 : 1 }}
                      >
                        {t.syncing ? '⏳' : '🔄'} Sync
                      </button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </div>

      {/* ── Activity log ── */}
      <div className="card">
        <div className="card-header">
          <h2 style={{ fontSize: 16, fontWeight: 600 }}>Activity Log</h2>
          {actionLog.length > 0 && (
            <button
              className="btn btn-secondary"
              onClick={() => setActionLog([])}
              style={{ fontSize: 11, padding: '4px 10px' }}
            >
              Clear
            </button>
          )}
        </div>
        <div style={{ maxHeight: 280, overflowY: 'auto' }}>
          {actionLog.length === 0 ? (
            <div style={{ padding: 'var(--spacing-xl)', textAlign: 'center', color: 'var(--text-muted)', fontSize: 13 }}>
              No activity yet. Run a sync to see results here.
            </div>
          ) : (
            <div style={{ padding: 'var(--spacing-md)' }}>
              {actionLog.map((entry, i) => (
                <div key={i} style={{
                  display: 'flex', gap: 12, alignItems: 'flex-start',
                  padding: '6px 8px', borderRadius: 6, fontSize: 13,
                  background: i === 0 ? 'var(--bg-tertiary)' : 'transparent',
                  marginBottom: 2,
                }}>
                  <span style={{
                    fontSize: 11, fontFamily: 'monospace', color: 'var(--text-muted)',
                    whiteSpace: 'nowrap', paddingTop: 1,
                  }}>
                    {entry.time.toLocaleTimeString()}
                  </span>
                  <span style={{
                    color: entry.type === 'error' ? 'var(--danger)'
                      : entry.type === 'success' ? 'var(--success)'
                      : 'var(--text-secondary)',
                    wordBreak: 'break-word',
                  }}>
                    {entry.msg}
                  </span>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>

      {/* ── Troubleshooting guide ── */}
      <div className="card" style={{ marginTop: 'var(--spacing-lg)' }}>
        <div className="card-header">
          <h2 style={{ fontSize: 16, fontWeight: 600 }}>Troubleshooting Guide</h2>
        </div>
        <div style={{ padding: 'var(--spacing-xl)', fontSize: 13, color: 'var(--text-secondary)', lineHeight: 1.8 }}>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 'var(--spacing-xl)' }}>
            <div>
              <div style={{ fontWeight: 600, color: 'var(--text-primary)', marginBottom: 8, fontSize: 14 }}>
                Products not appearing in search
              </div>
              <p>Click "Sync" for the affected tenant. This reindexes all their products into the search engine. New tenants always need an initial sync.</p>
            </div>
            <div>
              <div style={{ fontWeight: 600, color: 'var(--text-primary)', marginBottom: 8, fontSize: 14 }}>
                Search returns stale/old data
              </div>
              <p>Run "Sync All Tenants" to refresh the entire index. Products are synced automatically on create/update but bulk imports may require a manual sync.</p>
            </div>
            <div>
              <div style={{ fontWeight: 600, color: 'var(--text-primary)', marginBottom: 8, fontSize: 14 }}>
                Status shows "Down"
              </div>
              <p>First try <strong>Fix Connection</strong> — enter the VM's internal IP (run the gcloud command shown to find it). If the container has actually crashed, use <strong>Restart Typesense VM</strong> to reboot it. Search will return in ~60 seconds.</p>
            </div>
            <div>
              <div style={{ fontWeight: 600, color: 'var(--text-primary)', marginBottom: 8, fontSize: 14 }}>
                Sync shows "Error" for a tenant
              </div>
              <p>Try "Rebuild Search Schema" which recreates the search collections from scratch. If the error persists, the search engine may be in an unhealthy state.</p>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

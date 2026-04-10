import { useState, useEffect } from 'react';
import { auth } from '../contexts/AuthContext';

const API_BASE = (import.meta as any).env?.VITE_API_URL || 'http://localhost:8080/api/v1';

interface Tenant {
  tenant_id: string;
  name: string;
}

interface CopyResult {
  sku: string;
  status: 'copied' | 'skipped' | 'error';
  message?: string;
}

interface CopyResponse {
  ok: boolean;
  copied: number;
  skipped: number;
  errors: number;
  results: CopyResult[];
}

async function authFetch(url: string, init?: RequestInit): Promise<Response> {
  let token = '';
  try {
    if (auth.currentUser) token = await auth.currentUser.getIdToken();
  } catch { /* ignore */ }
  return fetch(url, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...(init?.headers || {}),
    },
  });
}

export default function ProductCopyTool() {
  const [tenants, setTenants] = useState<Tenant[]>([]);
  const [sourceTenant, setSourceTenant] = useState('');
  const [destTenant, setDestTenant] = useState('');
  const [skuText, setSkuText] = useState('');
  const [running, setRunning] = useState(false);
  const [response, setResponse] = useState<CopyResponse | null>(null);
  const [error, setError] = useState('');

  useEffect(() => {
    authFetch(`${API_BASE}/tenants`)
      .then(r => r.json())
      .then(d => setTenants(d.tenants || d.data || []))
      .catch(() => setError('Failed to load tenants'));
  }, []);

  const skus = skuText
    .split('\n')
    .map(s => s.trim())
    .filter(s => s.length > 0);

  async function handleSubmit() {
    if (!sourceTenant || !destTenant) { setError('Select source and destination tenants'); return; }
    if (sourceTenant === destTenant) { setError('Source and destination must be different tenants'); return; }
    if (skus.length === 0) { setError('Enter at least one SKU'); return; }
    if (skus.length > 500) { setError('Maximum 500 SKUs per operation'); return; }

    setRunning(true);
    setError('');
    setResponse(null);

    try {
      const res = await authFetch(`${API_BASE}/admin/ops/copy-products`, {
        method: 'POST',
        body: JSON.stringify({ source_tenant: sourceTenant, dest_tenant: destTenant, skus }),
      });
      const data = await res.json();
      if (!res.ok) { setError(data.error || `HTTP ${res.status}`); return; }
      setResponse(data);
    } catch (e: any) {
      setError(e.message || 'Request failed');
    } finally {
      setRunning(false);
    }
  }

  const statusColor = (s: string) =>
    s === 'copied' ? '#10b981' : s === 'skipped' ? '#f59e0b' : '#ef4444';

  const statusIcon = (s: string) =>
    s === 'copied' ? '✅' : s === 'skipped' ? '⏭️' : '❌';

  return (
    <div style={{ maxWidth: 900, margin: '0 auto', padding: '32px 24px' }}>

      {/* Header */}
      <div style={{ marginBottom: 32 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 6 }}>
          <span style={{ fontSize: 28 }}>📋</span>
          <h1 style={{ fontSize: 24, fontWeight: 700, color: 'var(--text-primary)', margin: 0 }}>
            Product Copy Tool
          </h1>
          <span style={{ fontSize: 11, fontWeight: 700, background: 'rgba(239,68,68,0.15)', color: '#ef4444', padding: '2px 8px', borderRadius: 4 }}>
            DEV TOOL
          </span>
        </div>
        <p style={{ color: 'var(--text-secondary)', fontSize: 14, margin: 0 }}>
          Copy products and their extended data between tenants by SKU. Listings are not copied. Up to 500 SKUs per operation.
        </p>
      </div>

      {/* Main form card */}
      <div style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 12, padding: 28, marginBottom: 24 }}>

        {/* Tenant selectors */}
        <div style={{ display: 'grid', gridTemplateColumns: '1fr auto 1fr', gap: 16, alignItems: 'end', marginBottom: 28 }}>
          <div>
            <label style={{ display: 'block', fontSize: 12, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: 1, marginBottom: 8 }}>
              Source Tenant
            </label>
            <select
              value={sourceTenant}
              onChange={e => setSourceTenant(e.target.value)}
              style={{ width: '100%', padding: '10px 12px', borderRadius: 8, border: '1px solid var(--border)', background: 'var(--bg-tertiary)', color: 'var(--text-primary)', fontSize: 14 }}
            >
              <option value="">— Select source —</option>
              {tenants.map(t => (
                <option key={t.tenant_id} value={t.tenant_id}>
                  {t.name} ({t.tenant_id})
                </option>
              ))}
            </select>
          </div>

          <div style={{ textAlign: 'center', paddingBottom: 10 }}>
            <span style={{ fontSize: 22, color: 'var(--text-muted)' }}>→</span>
          </div>

          <div>
            <label style={{ display: 'block', fontSize: 12, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: 1, marginBottom: 8 }}>
              Destination Tenant
            </label>
            <select
              value={destTenant}
              onChange={e => setDestTenant(e.target.value)}
              style={{ width: '100%', padding: '10px 12px', borderRadius: 8, border: '1px solid var(--border)', background: 'var(--bg-tertiary)', color: 'var(--text-primary)', fontSize: 14 }}
            >
              <option value="">— Select destination —</option>
              {tenants.map(t => (
                <option key={t.tenant_id} value={t.tenant_id} disabled={t.tenant_id === sourceTenant}>
                  {t.name} ({t.tenant_id})
                </option>
              ))}
            </select>
          </div>
        </div>

        {/* SKU input */}
        <div style={{ marginBottom: 24 }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 }}>
            <label style={{ fontSize: 12, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: 1 }}>
              SKUs to Copy
            </label>
            <span style={{ fontSize: 12, color: skus.length > 500 ? '#ef4444' : 'var(--text-muted)' }}>
              {skus.length} / 500 SKUs
            </span>
          </div>
          <textarea
            value={skuText}
            onChange={e => setSkuText(e.target.value)}
            placeholder={'Enter one SKU per line:\nSKU-001\nSKU-002\nSKU-003'}
            rows={12}
            style={{
              width: '100%', padding: '12px 14px', borderRadius: 8,
              border: `1px solid ${skus.length > 500 ? '#ef4444' : 'var(--border)'}`,
              background: 'var(--bg-tertiary)', color: 'var(--text-primary)',
              fontSize: 13, fontFamily: 'monospace', resize: 'vertical',
              boxSizing: 'border-box',
            }}
          />
          <p style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 6 }}>
            One SKU per line. Blank lines are ignored. Products already existing in the destination are skipped.
          </p>
        </div>

        {/* Warning banner */}
        <div style={{ background: 'rgba(239,68,68,0.08)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 8, padding: '12px 16px', marginBottom: 24, fontSize: 13, color: 'var(--text-secondary)', display: 'flex', gap: 10 }}>
          <span>⚠️</span>
          <span>This operation writes directly to Firestore. It copies product documents and their <strong>extended_data</strong> subcollection. Listings, import mappings, and orders are <strong>not</strong> copied. Ensure the destination tenant is correct before proceeding.</span>
        </div>

        {/* Error */}
        {error && (
          <div style={{ background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.4)', borderRadius: 8, padding: '10px 14px', marginBottom: 16, fontSize: 13, color: '#ef4444' }}>
            {error}
          </div>
        )}

        {/* Submit */}
        <button
          onClick={handleSubmit}
          disabled={running || skus.length === 0 || skus.length > 500 || !sourceTenant || !destTenant || sourceTenant === destTenant}
          style={{
            padding: '12px 28px', borderRadius: 8, border: 'none',
            background: 'var(--primary, #6366f1)', color: '#fff',
            fontWeight: 700, fontSize: 15, cursor: running ? 'wait' : 'pointer',
            opacity: (running || skus.length === 0 || skus.length > 500 || !sourceTenant || !destTenant || sourceTenant === destTenant) ? 0.5 : 1,
            width: '100%',
          }}
        >
          {running ? '⏳ Copying products…' : `📋 Copy ${skus.length > 0 ? skus.length : ''} Product${skus.length !== 1 ? 's' : ''}`}
        </button>
      </div>

      {/* Results */}
      {response && (
        <div style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 12, padding: 28 }}>
          {/* Summary row */}
          <div style={{ display: 'flex', gap: 24, marginBottom: 24, flexWrap: 'wrap' }}>
            {[
              { label: 'Copied', value: response.copied, color: '#10b981', icon: '✅' },
              { label: 'Skipped', value: response.skipped, color: '#f59e0b', icon: '⏭️' },
              { label: 'Errors', value: response.errors, color: '#ef4444', icon: '❌' },
            ].map(s => (
              <div key={s.label} style={{ flex: 1, minWidth: 100, background: 'var(--bg-tertiary)', borderRadius: 10, padding: '16px 20px', textAlign: 'center', border: `1px solid ${s.color}30` }}>
                <div style={{ fontSize: 28, fontWeight: 800, color: s.color }}>{s.value}</div>
                <div style={{ fontSize: 13, color: 'var(--text-muted)', marginTop: 4 }}>{s.icon} {s.label}</div>
              </div>
            ))}
          </div>

          {/* Results table */}
          <div style={{ maxHeight: 400, overflowY: 'auto', borderRadius: 8, border: '1px solid var(--border)' }}>
            <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
              <thead>
                <tr style={{ background: 'var(--bg-tertiary)', borderBottom: '1px solid var(--border)' }}>
                  <th style={{ padding: '10px 14px', textAlign: 'left', fontWeight: 700, color: 'var(--text-muted)', fontSize: 11, textTransform: 'uppercase', letterSpacing: 0.5 }}>SKU</th>
                  <th style={{ padding: '10px 14px', textAlign: 'left', fontWeight: 700, color: 'var(--text-muted)', fontSize: 11, textTransform: 'uppercase', letterSpacing: 0.5 }}>Status</th>
                  <th style={{ padding: '10px 14px', textAlign: 'left', fontWeight: 700, color: 'var(--text-muted)', fontSize: 11, textTransform: 'uppercase', letterSpacing: 0.5 }}>Note</th>
                </tr>
              </thead>
              <tbody>
                {(response.results || []).map((r, i) => (
                  <tr key={r.sku + i} style={{ borderBottom: '1px solid var(--border)', background: i % 2 === 0 ? 'transparent' : 'var(--bg-tertiary)' }}>
                    <td style={{ padding: '8px 14px', fontFamily: 'monospace', color: 'var(--text-primary)' }}>{r.sku}</td>
                    <td style={{ padding: '8px 14px' }}>
                      <span style={{ color: statusColor(r.status), fontWeight: 600 }}>
                        {statusIcon(r.status)} {r.status}
                      </span>
                    </td>
                    <td style={{ padding: '8px 14px', color: 'var(--text-muted)', fontSize: 12 }}>{r.message || '—'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </div>
  );
}

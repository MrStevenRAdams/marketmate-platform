import { useState, useEffect } from 'react';
import { getActiveTenantId } from '../contexts/TenantContext';

const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';
function api(path: string, init?: RequestInit) {
  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId(), ...init?.headers },
  });
}

interface Manifest {
  manifest_id: string;
  tenant_id: string;
  carrier_id: string;
  manifest_date: string;
  shipment_count: number;
  status: string;
  download_url?: string;
  error_message?: string;
  created_at: string;
}

const CARRIERS = [
  { id: 'royal-mail', name: 'Royal Mail' },
  { id: 'dpd', name: 'DPD' },
  { id: 'evri', name: 'Evri' },
  { id: 'fedex', name: 'FedEx' },
];

const statusStyle = (status: string): { background: string; color: string } => {
  switch (status) {
    case 'generated': return { background: 'rgba(34,197,94,0.12)', color: '#4ade80' };
    case 'failed':    return { background: 'rgba(239,68,68,0.12)', color: '#f87171' };
    default:          return { background: 'rgba(251,191,36,0.12)', color: '#fbbf24' };
  }
};

export default function Manifests() {
  const [manifests, setManifests] = useState<Manifest[]>([]);
  const [loading, setLoading] = useState(true);
  const [generating, setGenerating] = useState(false);
  const [showModal, setShowModal] = useState(false);
  const [selectedCarrier, setSelectedCarrier] = useState('');
  const [manifestDate, setManifestDate] = useState(new Date().toISOString().slice(0, 10));
  const [error, setError] = useState<string | null>(null);

  useEffect(() => { loadManifests(); }, []);

  async function loadManifests() {
    setLoading(true);
    try {
      const res = await api('/dispatch/manifest/history');
      if (res.ok) {
        const data = await res.json();
        setManifests(data.manifests || []);
      }
    } finally {
      setLoading(false);
    }
  }

  async function generateManifest() {
    setGenerating(true);
    setError(null);
    try {
      const body: Record<string, string> = { manifest_date: manifestDate };
      if (selectedCarrier) body.carrier_id = selectedCarrier;

      const res = await api('/dispatch/manifest', {
        method: 'POST',
        body: JSON.stringify(body),
      });
      const data = await res.json();
      if (!res.ok) {
        setError(data.error || 'Failed to generate manifest');
        return;
      }
      setShowModal(false);
      setSelectedCarrier('');
      await loadManifests();
    } finally {
      setGenerating(false);
    }
  }

  const fmt = (iso: string) => {
    try { return new Date(iso).toLocaleString('en-GB', { day: '2-digit', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit' }); }
    catch { return iso; }
  };

  const carrierLabel = (id: string) => CARRIERS.find(c => c.id === id)?.name ?? id;

  return (
    <div style={{ padding: '32px 40px', maxWidth: 1100, margin: '0 auto' }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 28 }}>
        <div>
          <h1 style={{ margin: 0, color: 'var(--text-primary)', fontSize: 22, fontWeight: 700 }}>📦 Shipping Manifests</h1>
          <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 14 }}>End-of-day carrier manifests for booked shipments</p>
        </div>
        <button
          onClick={() => setShowModal(true)}
          style={{
            padding: '10px 20px', background: 'var(--primary)', border: 'none', borderRadius: 8,
            color: '#fff', fontWeight: 600, fontSize: 14, cursor: 'pointer',
          }}
        >
          + Generate Manifest
        </button>
      </div>

      {/* Manifests Table */}
      <div style={{ background: 'var(--bg-secondary)', borderRadius: 12, border: '1px solid var(--border)', overflow: 'hidden' }}>
        <table style={{ width: '100%', borderCollapse: 'collapse' }}>
          <thead>
            <tr style={{ borderBottom: '1px solid var(--border)' }}>
              {['Date', 'Carrier', 'Shipments', 'Status', 'Created', 'Actions'].map(h => (
                <th key={h} style={{ padding: '12px 16px', textAlign: 'left', fontSize: 12, fontWeight: 600, color: 'var(--text-muted)', letterSpacing: '0.05em', textTransform: 'uppercase' }}>{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {loading ? (
              <tr><td colSpan={6} style={{ padding: 40, textAlign: 'center', color: 'var(--text-muted)' }}>Loading…</td></tr>
            ) : manifests.length === 0 ? (
              <tr>
                <td colSpan={6} style={{ padding: 60, textAlign: 'center', color: 'var(--text-muted)' }}>
                  <div style={{ fontSize: 32, marginBottom: 12 }}>📄</div>
                  <div style={{ fontWeight: 600, marginBottom: 6 }}>No manifests yet</div>
                  <div style={{ fontSize: 13 }}>Generate your first end-of-day manifest above.</div>
                </td>
              </tr>
            ) : manifests.map((m, i) => {
              const st = statusStyle(m.status);
              return (
                <tr key={m.manifest_id} style={{ borderBottom: i < manifests.length - 1 ? '1px solid var(--border)' : 'none' }}>
                  <td style={{ padding: '14px 16px', color: 'var(--text-primary)', fontWeight: 500 }}>{m.manifest_date}</td>
                  <td style={{ padding: '14px 16px', color: 'var(--text-secondary)' }}>{carrierLabel(m.carrier_id)}</td>
                  <td style={{ padding: '14px 16px', color: 'var(--text-secondary)' }}>{m.shipment_count}</td>
                  <td style={{ padding: '14px 16px' }}>
                    <span style={{ ...st, padding: '3px 10px', borderRadius: 5, fontSize: 12, fontWeight: 600 }}>
                      {m.status.charAt(0).toUpperCase() + m.status.slice(1)}
                    </span>
                  </td>
                  <td style={{ padding: '14px 16px', color: 'var(--text-muted)', fontSize: 13 }}>{fmt(m.created_at)}</td>
                  <td style={{ padding: '14px 16px' }}>
                    {m.download_url ? (
                      <a
                        href={m.download_url}
                        target="_blank"
                        rel="noreferrer"
                        style={{ color: 'var(--accent-cyan)', fontSize: 13, textDecoration: 'none', fontWeight: 500 }}
                      >
                        ⬇ Download
                      </a>
                    ) : m.error_message ? (
                      <span style={{ color: '#f87171', fontSize: 12 }} title={m.error_message}>Error ⚠</span>
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

      {/* Generate Manifest Modal */}
      {showModal && (
        <>
          <div
            onClick={() => { setShowModal(false); setError(null); }}
            style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.5)', zIndex: 999 }}
          />
          <div style={{
            position: 'fixed', top: '50%', left: '50%', transform: 'translate(-50%,-50%)',
            background: 'var(--bg-secondary)', borderRadius: 16, border: '1px solid var(--border)',
            width: 440, padding: 28, zIndex: 1000, boxShadow: '0 24px 64px rgba(0,0,0,0.5)',
          }}>
            <h2 style={{ margin: '0 0 20px', color: 'var(--text-primary)', fontSize: 18, fontWeight: 700 }}>Generate End-of-Day Manifest</h2>

            <div style={{ marginBottom: 16 }}>
              <label style={{ display: 'block', fontSize: 13, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 6 }}>
                Manifest Date
              </label>
              <input
                type="date"
                value={manifestDate}
                onChange={e => setManifestDate(e.target.value)}
                style={{
                  width: '100%', padding: '9px 12px', background: 'var(--bg-elevated)',
                  border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-primary)',
                  fontSize: 14, boxSizing: 'border-box',
                }}
              />
            </div>

            <div style={{ marginBottom: 20 }}>
              <label style={{ display: 'block', fontSize: 13, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 6 }}>
                Carrier <span style={{ fontWeight: 400, color: 'var(--text-muted)' }}>(leave blank for all carriers)</span>
              </label>
              <select
                value={selectedCarrier}
                onChange={e => setSelectedCarrier(e.target.value)}
                style={{
                  width: '100%', padding: '9px 12px', background: 'var(--bg-elevated)',
                  border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-primary)',
                  fontSize: 14, boxSizing: 'border-box',
                }}
              >
                <option value="">All configured carriers</option>
                {CARRIERS.map(c => <option key={c.id} value={c.id}>{c.name}</option>)}
              </select>
            </div>

            {error && (
              <div style={{ background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 8, padding: '10px 14px', color: '#f87171', fontSize: 13, marginBottom: 16 }}>
                {error}
              </div>
            )}

            <div style={{ display: 'flex', gap: 10, justifyContent: 'flex-end' }}>
              <button
                onClick={() => { setShowModal(false); setError(null); }}
                style={{ padding: '9px 18px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-secondary)', cursor: 'pointer', fontSize: 14 }}
              >
                Cancel
              </button>
              <button
                onClick={generateManifest}
                disabled={generating}
                style={{
                  padding: '9px 20px', background: generating ? 'rgba(99,102,241,0.4)' : 'var(--primary)',
                  border: 'none', borderRadius: 8, color: '#fff', fontWeight: 600,
                  fontSize: 14, cursor: generating ? 'not-allowed' : 'pointer',
                }}
              >
                {generating ? 'Generating…' : 'Generate'}
              </button>
            </div>
          </div>
        </>
      )}
    </div>
  );
}

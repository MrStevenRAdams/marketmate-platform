import { useState, useEffect } from 'react';
import { getActiveTenantId } from '../contexts/TenantContext';

const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';
function api(path: string, init?: RequestInit) {
  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId(), ...init?.headers },
  });
}

interface QueuedShipment {
  shipment_id: string;
  order_ids: string[];
  carrier_id: string;
  service_name: string;
  tracking_number: string;
  label_url: string;
  label_format: string;
  status: string;
  created_at: string;
}

export default function LabelPrinting() {
  const [shipments, setShipments] = useState<QueuedShipment[]>([]);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [printing, setPrinting] = useState(false);
  const [result, setResult] = useState<{ urls: string[]; count: number } | null>(null);

  useEffect(() => { load(); }, []);

  async function load() {
    const res = await api('/shipments/print-queue');
    if (res.ok) {
      const data = await res.json();
      setShipments(data.shipments || []);
    }
  }

  function toggle(id: string) {
    setSelected(prev => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id); else next.add(id);
      return next;
    });
  }

  function toggleAll() {
    if (selected.size === shipments.length) {
      setSelected(new Set());
    } else {
      setSelected(new Set(shipments.map(s => s.shipment_id)));
    }
  }

  async function printSelected() {
    if (selected.size === 0) return;
    setPrinting(true);
    try {
      const res = await api('/shipments/print', {
        method: 'POST',
        body: JSON.stringify({ shipment_ids: Array.from(selected) }),
      });
      if (res.ok) {
        const data = await res.json();
        setResult({ urls: data.label_urls || [], count: data.count || 0 });
        // Open all label URLs in new tabs
        (data.label_urls || []).forEach((url: string) => window.open(url, '_blank'));
        setSelected(new Set());
        load();
      }
    } finally {
      setPrinting(false);
    }
  }

  return (
    <div style={{ padding: 24, maxWidth: 1000, margin: '0 auto' }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 24 }}>
        <div>
          <h1 style={{ color: 'var(--text-primary)', marginBottom: 4 }}>🏷️ Batch Label Printing</h1>
          <p style={{ color: 'var(--text-muted)' }}>Print shipping labels for multiple shipments at once.</p>
        </div>
        {selected.size > 0 && (
          <button onClick={printSelected} disabled={printing}
            style={{ padding: '10px 20px', background: 'var(--primary)', border: 'none', borderRadius: 8, color: 'white', fontWeight: 700, cursor: 'pointer' }}>
            {printing ? '🖨️ Printing…' : `🖨️ Print ${selected.size} Label${selected.size !== 1 ? 's' : ''}`}
          </button>
        )}
      </div>

      {result && (
        <div style={{ background: 'rgba(34,197,94,0.1)', border: '1px solid rgba(34,197,94,0.3)', borderRadius: 8, padding: '12px 16px', marginBottom: 16 }}>
          <span style={{ color: '#22c55e' }}>✅ {result.count} label{result.count !== 1 ? 's' : ''} sent to print. Labels opened in new tabs.</span>
          <button onClick={() => setResult(null)} style={{ marginLeft: 16, background: 'none', border: 'none', cursor: 'pointer', color: '#22c55e', opacity: 0.7 }}>×</button>
        </div>
      )}

      {shipments.length === 0 ? (
        <div style={{ textAlign: 'center', padding: 60, color: 'var(--text-muted)' }}>
          <div style={{ fontSize: 40 }}>✅</div>
          <div style={{ marginTop: 12 }}>No labels in the print queue.</div>
          <p style={{ fontSize: 13, marginTop: 8 }}>All shipment labels have been printed, or no labels have been generated yet.</p>
        </div>
      ) : (
        <>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 12, padding: '10px 16px', background: 'var(--bg-elevated)', borderRadius: 8, border: '1px solid var(--border)' }}>
            <input type="checkbox" checked={selected.size === shipments.length && shipments.length > 0} onChange={toggleAll} style={{ cursor: 'pointer' }} />
            <span style={{ color: 'var(--text-muted)', fontSize: 13 }}>
              {selected.size > 0 ? `${selected.size} of ${shipments.length} selected` : `${shipments.length} shipments in queue`}
            </span>
            {selected.size === 0 && (
              <button onClick={toggleAll} style={{ marginLeft: 8, background: 'none', border: 'none', cursor: 'pointer', color: 'var(--primary)', fontSize: 13 }}>Select All</button>
            )}
          </div>

          <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
            {shipments.map(s => (
              <div
                key={s.shipment_id}
                onClick={() => toggle(s.shipment_id)}
                style={{
                  display: 'flex', alignItems: 'center', gap: 16, padding: '16px 20px',
                  background: selected.has(s.shipment_id) ? 'rgba(99,102,241,0.08)' : 'var(--bg-elevated)',
                  border: `1px solid ${selected.has(s.shipment_id) ? 'var(--primary)' : 'var(--border)'}`,
                  borderRadius: 8, cursor: 'pointer', transition: 'border-color 0.15s',
                }}
              >
                <input type="checkbox" checked={selected.has(s.shipment_id)} onChange={() => toggle(s.shipment_id)} onClick={e => e.stopPropagation()} style={{ cursor: 'pointer' }} />
                <div style={{ flex: 1 }}>
                  <div style={{ fontWeight: 600, color: 'var(--text-primary)', fontSize: 13 }}>
                    {s.carrier_id} · {s.service_name || 'Standard'}
                  </div>
                  <div style={{ color: 'var(--text-muted)', fontSize: 12, marginTop: 2 }}>
                    Orders: {(s.order_ids || []).join(', ')}
                    {s.tracking_number && <span style={{ marginLeft: 12 }}>Tracking: {s.tracking_number}</span>}
                  </div>
                </div>
                <div style={{ textAlign: 'right' }}>
                  <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>{s.label_format || 'PDF'}</div>
                  {s.label_url && (
                    <a href={s.label_url} target="_blank" rel="noopener noreferrer"
                      onClick={e => e.stopPropagation()}
                      style={{ fontSize: 12, color: 'var(--primary)', textDecoration: 'none' }}>
                      Preview
                    </a>
                  )}
                </div>
              </div>
            ))}
          </div>
        </>
      )}
    </div>
  );
}

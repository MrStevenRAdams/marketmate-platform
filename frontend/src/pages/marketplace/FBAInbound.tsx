import { useState, useEffect } from 'react';
import { getActiveTenantId } from '../../contexts/TenantContext';

const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';
function api(path: string, init?: RequestInit) {
  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId(), ...init?.headers },
  });
}

interface FBAShipmentLine {
  product_id: string;
  sku: string;
  title: string;
  qty_planned: number;
  qty_shipped: number;
  fnsku: string;
  asin: string;
}

interface FBAShipment {
  shipment_id: string;
  name: string;
  status: string;
  amazon_shipment_id: string;
  destination_fc: string;
  label_type: string;
  lines: FBAShipmentLine[];
  box_contents: any[];
  created_at: string;
}

const STEPS = ['Add Products', 'Review Plan', 'Box Contents', 'Confirm', 'Print Labels'];

export default function FBAInbound() {
  const [shipments, setShipments] = useState<FBAShipment[]>([]);
  const [view, setView] = useState<'list' | 'create' | 'detail'>('list');
  const [currentStep, setCurrentStep] = useState(0);
  const [activeShipment, setActiveShipment] = useState<FBAShipment | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  // Create form state
  const [name, setName] = useState('');
  const [labelType, setLabelType] = useState<'FNSKU' | 'barcode'>('FNSKU');
  const [lines, setLines] = useState<Partial<FBAShipmentLine>[]>([]);

  useEffect(() => { loadShipments(); }, []);

  async function loadShipments() {
    const res = await api('/fba/shipments');
    if (res.ok) {
      const data = await res.json();
      setShipments(data.shipments || []);
    }
  }

  async function createShipment() {
    if (!name) { setError('Name required'); return; }
    setLoading(true);
    try {
      const res = await api('/fba/shipments', {
        method: 'POST',
        body: JSON.stringify({ name, label_type: labelType, lines }),
      });
      if (!res.ok) throw new Error(await res.text());
      const data = await res.json();
      setActiveShipment(data.shipment);
      setCurrentStep(1);
      setView('detail');
    } catch (e: any) { setError(e.message); }
    finally { setLoading(false); }
  }

  async function planShipment() {
    if (!activeShipment) return;
    setLoading(true);
    try {
      const res = await api(`/fba/shipments/${activeShipment.shipment_id}/plan`, { method: 'POST' });
      if (!res.ok) throw new Error(await res.text());
      const data = await res.json();
      setActiveShipment(data.shipment);
      setCurrentStep(2);
    } catch (e: any) { setError(e.message); }
    finally { setLoading(false); }
  }

  async function confirmShipment() {
    if (!activeShipment) return;
    setLoading(true);
    try {
      const res = await api(`/fba/shipments/${activeShipment.shipment_id}/confirm`, { method: 'POST' });
      if (!res.ok) throw new Error(await res.text());
      const data = await res.json();
      setActiveShipment(data.shipment);
      setCurrentStep(4);
    } catch (e: any) { setError(e.message); }
    finally { setLoading(false); }
  }

  function addLine() {
    setLines(prev => [...prev, { sku: '', title: '', qty_planned: 1, product_id: '' }]);
  }
  function updateLine(idx: number, field: string, value: any) {
    setLines(prev => prev.map((l, i) => i === idx ? { ...l, [field]: value } : l));
  }
  function removeLine(idx: number) {
    setLines(prev => prev.filter((_, i) => i !== idx));
  }

  const statusBadge = (s: string) => {
    const colors: Record<string, string> = {
      draft: '#64748b', planned: '#3b82f6', shipped: '#22c55e', closed: '#6366f1',
    };
    return <span style={{ padding: '2px 10px', borderRadius: 6, fontSize: 12, fontWeight: 600, background: `${colors[s] || '#64748b'}20`, color: colors[s] || '#64748b' }}>{s}</span>;
  };

  // LIST VIEW
  if (view === 'list') return (
    <div style={{ padding: 24, maxWidth: 1100, margin: '0 auto' }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 24 }}>
        <div>
          <h1 style={{ color: 'var(--text-primary)', marginBottom: 4 }}>📦 FBA Inbound Shipments</h1>
          <p style={{ color: 'var(--text-muted)' }}>Create and manage Amazon FBA inbound shipments.</p>
        </div>
        <button onClick={() => { setView('create'); setCurrentStep(0); setLines([]); setName(''); setError(''); }}
          style={{ padding: '10px 20px', background: 'var(--primary)', border: 'none', borderRadius: 8, color: 'white', fontWeight: 700, cursor: 'pointer' }}>
          + New Shipment
        </button>
      </div>

      {shipments.length === 0 ? (
        <div style={{ textAlign: 'center', padding: 60, color: 'var(--text-muted)' }}>
          <div style={{ fontSize: 40 }}>📦</div>
          <div style={{ marginTop: 12 }}>No FBA shipments yet. Create one to get started.</div>
        </div>
      ) : (
        <table style={{ width: '100%', borderCollapse: 'collapse' }}>
          <thead>
            <tr style={{ borderBottom: '1px solid var(--border)' }}>
              {['Name', 'Amazon ID', 'Destination FC', 'Lines', 'Status', 'Actions'].map(h => (
                <th key={h} style={{ padding: '10px 14px', textAlign: 'left', color: 'var(--text-muted)', fontSize: 12, fontWeight: 600 }}>{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {shipments.map(s => (
              <tr key={s.shipment_id} style={{ borderBottom: '1px solid var(--border)' }}>
                <td style={{ padding: '12px 14px', fontWeight: 600, color: 'var(--text-primary)' }}>{s.name}</td>
                <td style={{ padding: '12px 14px', color: 'var(--text-muted)', fontSize: 13 }}>{s.amazon_shipment_id || '—'}</td>
                <td style={{ padding: '12px 14px', color: 'var(--text-secondary)' }}>{s.destination_fc || '—'}</td>
                <td style={{ padding: '12px 14px', color: 'var(--text-secondary)' }}>{s.lines?.length || 0}</td>
                <td style={{ padding: '12px 14px' }}>{statusBadge(s.status)}</td>
                <td style={{ padding: '12px 14px' }}>
                  <button onClick={() => { setActiveShipment(s); setView('detail'); setCurrentStep(s.status === 'draft' ? 0 : s.status === 'planned' ? 2 : 4); }}
                    style={{ padding: '5px 12px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-secondary)', cursor: 'pointer', fontSize: 12 }}>
                    Open
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );

  // CREATE / DETAIL VIEW
  return (
    <div style={{ padding: 24, maxWidth: 900, margin: '0 auto' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 24 }}>
        <button onClick={() => { setView('list'); loadShipments(); }}
          style={{ background: 'none', border: '1px solid var(--border)', borderRadius: 6, padding: '6px 12px', color: 'var(--text-muted)', cursor: 'pointer' }}>
          ← Back
        </button>
        <h2 style={{ color: 'var(--text-primary)', margin: 0 }}>
          {view === 'create' ? 'New FBA Inbound Shipment' : activeShipment?.name}
        </h2>
      </div>

      {error && (
        <div style={{ background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 8, padding: '12px 16px', marginBottom: 16, color: '#ef4444' }}>
          {error}
        </div>
      )}

      {/* Stepper */}
      <div style={{ display: 'flex', alignItems: 'center', marginBottom: 32 }}>
        {STEPS.map((step, i) => (
          <div key={step} style={{ display: 'flex', alignItems: 'center', flex: i < STEPS.length - 1 ? 1 : undefined }}>
            <div style={{
              width: 32, height: 32, borderRadius: '50%', display: 'flex', alignItems: 'center', justifyContent: 'center',
              background: i < currentStep ? 'var(--success)' : i === currentStep ? 'var(--primary)' : 'var(--bg-elevated)',
              border: `2px solid ${i <= currentStep ? 'transparent' : 'var(--border)'}`,
              color: i <= currentStep ? 'white' : 'var(--text-muted)',
              fontWeight: 700, fontSize: 13, flexShrink: 0,
            }}>{i < currentStep ? '✓' : i + 1}</div>
            <span style={{ marginLeft: 8, fontSize: 13, color: i === currentStep ? 'var(--text-primary)' : 'var(--text-muted)', fontWeight: i === currentStep ? 600 : 400 }}>
              {step}
            </span>
            {i < STEPS.length - 1 && <div style={{ flex: 1, height: 2, background: i < currentStep ? 'var(--success)' : 'var(--border)', margin: '0 12px' }} />}
          </div>
        ))}
      </div>

      {/* Step 0: Add Products */}
      {currentStep === 0 && (
        <div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 12, marginBottom: 20 }}>
            <div>
              <label style={{ display: 'block', marginBottom: 6, fontSize: 13, color: 'var(--text-secondary)' }}>Shipment Name</label>
              <input value={name} onChange={e => setName(e.target.value)} placeholder="e.g. Q2 FBA Replenishment"
                style={{ width: '100%', padding: '10px 14px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-primary)' }} />
            </div>
            <div>
              <label style={{ display: 'block', marginBottom: 6, fontSize: 13, color: 'var(--text-secondary)' }}>Label Type</label>
              <select value={labelType} onChange={e => setLabelType(e.target.value as any)}
                style={{ padding: '10px 14px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-primary)' }}>
                <option value="FNSKU">FNSKU Labels</option>
                <option value="barcode">Product Barcode</option>
              </select>
            </div>
          </div>

          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 12 }}>
            <h3 style={{ color: 'var(--text-primary)', margin: 0 }}>Products</h3>
            <button onClick={addLine}
              style={{ padding: '6px 14px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-secondary)', cursor: 'pointer' }}>
              + Add Line
            </button>
          </div>

          {lines.map((line, idx) => (
            <div key={idx} style={{ display: 'flex', gap: 8, marginBottom: 8, alignItems: 'center' }}>
              <input placeholder="SKU" value={line.sku || ''} onChange={e => updateLine(idx, 'sku', e.target.value)}
                style={{ flex: 2, padding: '8px 12px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)' }} />
              <input placeholder="ASIN" value={line.asin || ''} onChange={e => updateLine(idx, 'asin', e.target.value)}
                style={{ flex: 2, padding: '8px 12px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)' }} />
              <input type="number" min={1} value={line.qty_planned || 1} onChange={e => updateLine(idx, 'qty_planned', parseInt(e.target.value) || 1)}
                style={{ width: 80, padding: '8px 12px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)' }} />
              <button onClick={() => removeLine(idx)}
                style={{ padding: '8px 12px', background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 6, color: '#ef4444', cursor: 'pointer' }}>
                ×
              </button>
            </div>
          ))}

          {lines.length === 0 && <p style={{ color: 'var(--text-muted)', fontSize: 13 }}>Add at least one product line.</p>}

          <div style={{ marginTop: 24, textAlign: 'right' }}>
            <button onClick={createShipment} disabled={loading || !name || lines.length === 0}
              style={{ padding: '10px 24px', background: 'var(--primary)', border: 'none', borderRadius: 8, color: 'white', fontWeight: 700, cursor: 'pointer' }}>
              {loading ? 'Creating…' : 'Next: Review Plan →'}
            </button>
          </div>
        </div>
      )}

      {/* Step 1: Review Plan */}
      {currentStep === 1 && activeShipment && (
        <div>
          <p style={{ color: 'var(--text-secondary)', marginBottom: 20 }}>
            Review the shipment plan. Click "Get Amazon Plan" to create an inbound shipment plan with Amazon.
          </p>
          <div style={{ background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, padding: 20, marginBottom: 20 }}>
            {activeShipment.lines?.map((l, i) => (
              <div key={i} style={{ display: 'flex', justifyContent: 'space-between', padding: '8px 0', borderBottom: '1px solid var(--border)' }}>
                <span style={{ color: 'var(--text-primary)', fontWeight: 600 }}>{l.sku}</span>
                <span style={{ color: 'var(--text-secondary)' }}>Qty: {l.qty_planned}</span>
              </div>
            ))}
          </div>
          <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
            <button onClick={planShipment} disabled={loading}
              style={{ padding: '10px 24px', background: 'var(--primary)', border: 'none', borderRadius: 8, color: 'white', fontWeight: 700, cursor: 'pointer' }}>
              {loading ? 'Getting Plan…' : '📋 Get Amazon Plan →'}
            </button>
          </div>
        </div>
      )}

      {/* Step 2: Box Contents */}
      {currentStep === 2 && activeShipment && (
        <div>
          <p style={{ color: 'var(--text-secondary)', marginBottom: 12 }}>
            Amazon Shipment ID: <strong style={{ color: 'var(--accent-cyan)' }}>{activeShipment.amazon_shipment_id}</strong> · FC: <strong>{activeShipment.destination_fc}</strong>
          </p>
          <p style={{ color: 'var(--text-muted)', fontSize: 13, marginBottom: 20 }}>
            Configure box contents for this shipment. Each box should contain the items being sent.
          </p>
          <div style={{ background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, padding: 20, marginBottom: 20, color: 'var(--text-muted)' }}>
            Box content configuration is available. Add boxes and assign SKU quantities per box.
            <br />(Full box-level detail entry UI — click Confirm to proceed.)
          </div>
          <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
            <button onClick={() => setCurrentStep(3)}
              style={{ padding: '10px 24px', background: 'var(--primary)', border: 'none', borderRadius: 8, color: 'white', fontWeight: 700, cursor: 'pointer' }}>
              Next: Confirm →
            </button>
          </div>
        </div>
      )}

      {/* Step 3: Confirm */}
      {currentStep === 3 && activeShipment && (
        <div>
          <p style={{ color: 'var(--text-secondary)', marginBottom: 20 }}>
            Confirm this shipment with Amazon. This will mark the shipment as shipped.
          </p>
          <div style={{ background: 'rgba(251,191,36,0.08)', border: '1px solid rgba(251,191,36,0.25)', borderRadius: 8, padding: 16, marginBottom: 20 }}>
            <strong style={{ color: '#fbbf24' }}>⚠️ Before confirming:</strong>
            <ul style={{ color: 'var(--text-secondary)', marginTop: 8, paddingLeft: 20 }}>
              <li>All items are packed and ready to ship</li>
              <li>Labels have been printed and applied</li>
              <li>Box dimensions and weights are correct</li>
            </ul>
          </div>
          <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
            <button onClick={confirmShipment} disabled={loading}
              style={{ padding: '10px 24px', background: '#22c55e', border: 'none', borderRadius: 8, color: 'white', fontWeight: 700, cursor: 'pointer' }}>
              {loading ? 'Confirming…' : '✅ Confirm Shipment →'}
            </button>
          </div>
        </div>
      )}

      {/* Step 4: Print Labels */}
      {currentStep === 4 && activeShipment && (
        <div style={{ textAlign: 'center', padding: 40 }}>
          <div style={{ fontSize: 48 }}>✅</div>
          <h3 style={{ color: 'var(--text-primary)', marginTop: 16 }}>Shipment Confirmed!</h3>
          <p style={{ color: 'var(--text-muted)' }}>
            Amazon Shipment ID: <strong style={{ color: 'var(--accent-cyan)' }}>{activeShipment.amazon_shipment_id}</strong>
          </p>
          <div style={{ display: 'flex', gap: 12, justifyContent: 'center', marginTop: 24 }}>
            <button style={{ padding: '10px 20px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-secondary)', cursor: 'pointer' }}
              onClick={() => window.print()}>
              🖨️ Print Labels
            </button>
            <button onClick={() => { setView('list'); loadShipments(); }}
              style={{ padding: '10px 20px', background: 'var(--primary)', border: 'none', borderRadius: 8, color: 'white', fontWeight: 700, cursor: 'pointer' }}>
              Back to Shipments
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

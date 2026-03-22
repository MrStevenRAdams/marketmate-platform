import { useState, useEffect, useRef } from 'react';
import { getActiveTenantId } from '../contexts/TenantContext';
import { SerialNumberInput, SerialRequiredBadge, useProductBySku } from '../components/SerialNumberInput';

const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';

function api(path: string, init?: RequestInit) {
  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId(), ...init?.headers },
  });
}

interface PO {
  po_id: string;
  po_number: string;
  supplier_name: string;
  status: string;
  lines: POLine[];
}

interface POLine {
  line_id: string;
  internal_sku: string;
  description: string;
  qty_ordered: number;
  qty_received: number;
}

interface Binrack {
  binrack_id: string;
  name: string;
  barcode?: string;
}

interface ExistingBatch {
  batch_id: string;
  batch_number: string;
  expiry_date?: string;
}

interface ReceiveLine {
  line_id: string;
  sku: string;
  description: string;
  qty_ordered: number;
  qty_received: number;
  qty_to_receive: number;
  binrack_id: string;
  batch_mode: 'new' | 'existing' | 'none';
  batch_number: string;
  batch_type: 'Standard' | 'Lot' | 'Serial';
  expiry_date: string;
  sell_by: string;
  existing_batch_id: string;
  // Serial tracking
  use_serial_numbers: boolean;   // copied from product flag at line creation
  serial_numbers: string[];      // one per unit received
}

// ── SerialFlagLoader ─────────────────────────────────────────────────────────
// Invisible component: once mounted it fetches the product by SKU and, if it
// is serial-tracked, updates the corresponding ReceiveLine in parent state.
function SerialFlagLoader({ sku, lineId, onFlagLoaded }: {
  sku: string;
  lineId: string;
  onFlagLoaded: (lineId: string, isSerial: boolean) => void;
}) {
  const { product, loading } = useProductBySku(sku);
  useEffect(() => {
    if (!loading && product !== null) {
      onFlagLoaded(lineId, !!product?.use_serial_numbers);
    }
  }, [product, loading, lineId, onFlagLoaded]);
  return null;
}

export default function StockIn() {
  const [mode, setMode] = useState<'po' | 'adhoc'>('po');
  const [pos, setPOs] = useState<PO[]>([]);
  const [selectedPO, setSelectedPO] = useState<PO | null>(null);
  const [lines, setLines] = useState<ReceiveLine[]>([]);
  const [adhocSku, setAdhocSku] = useState('');
  const [adhocQty, setAdhocQty] = useState(1);
  const [adhocBinrackId, setAdhocBinrackId] = useState('');
  const [printLabels, setPrintLabels] = useState(false);
  const [loading, setLoading] = useState(false);
  const [success, setSuccess] = useState('');
  const [error, setError] = useState('');
  const [binracks, setBinracks] = useState<Binrack[]>([]);
  const [existingBatches, setExistingBatches] = useState<Record<string, ExistingBatch[]>>({});
  const skuInputRef = useRef<HTMLInputElement>(null);

  // Called by SerialFlagLoader once it resolves the product flag for a line
  function handleSerialFlagLoaded(lineId: string, isSerial: boolean) {
    setLines(prev => prev.map(l => {
      if (l.line_id !== lineId || l.use_serial_numbers === isSerial) return l;
      return {
        ...l,
        use_serial_numbers: isSerial,
        // Auto-switch batch mode to 'new' and type to 'Serial' if serial-tracked
        batch_mode: isSerial ? 'new' : l.batch_mode,
        batch_type: isSerial ? 'Serial' : l.batch_type,
      };
    }));
  }

  useEffect(() => {
    if (mode === 'po') loadPOs();
    loadBinracks();
  }, [mode]);

  async function loadBinracks() {
    const res = await api('/warehouse/binracks');
    if (res.ok) {
      const data = await res.json();
      setBinracks(data.binracks || []);
    }
  }

  async function loadPOs() {
    const res = await api('/purchase-orders?status=sent,partially_received');
    if (res.ok) {
      const data = await res.json();
      setPOs(data.purchase_orders || data.pos || []);
    }
  }

  async function loadExistingBatches(sku: string) {
    if (existingBatches[sku]) return;
    const res = await api(`/products/${sku}/batches`);
    if (res.ok) {
      const data = await res.json();
      setExistingBatches(prev => ({ ...prev, [sku]: data.batches || [] }));
    }
  }

  function selectPO(po: PO) {
    setSelectedPO(po);
    setLines((po.lines || []).map(l => ({
      line_id: l.line_id,
      sku: l.internal_sku,
      description: l.description,
      qty_ordered: l.qty_ordered,
      qty_received: l.qty_received || 0,
      qty_to_receive: l.qty_ordered - (l.qty_received || 0),
      binrack_id: '',
      batch_mode: 'none' as const,
      batch_number: '',
      batch_type: 'Standard' as const,
      expiry_date: '',
      sell_by: '',
      existing_batch_id: '',
      use_serial_numbers: false,  // will be updated by SerialFlagLoader
      serial_numbers: [],
    })));
  }

  function updateLine(idx: number, field: keyof ReceiveLine, value: any) {
    setLines(prev => prev.map((l, i) => {
      if (i !== idx) return l;
      const updated = { ...l, [field]: value };
      if (field === 'batch_mode' && value === 'existing') {
        loadExistingBatches(l.sku);
      }
      return updated;
    }));
  }

  async function triggerLabelPrint(skus: Array<{ sku: string; qty: number }>) {
    try {
      await api('/labels/print', {
        method: 'POST',
        body: JSON.stringify({ items: skus }),
      });
    } catch {
      // Non-blocking — show a warning but don't fail
      setError(prev => prev + ' (Labels failed to print — please print manually)');
    }
  }

  async function confirmPO() {
    if (!selectedPO) return;
    // Serial validation — block if any serial-tracked line is incomplete
    const serialIncomplete = lines.filter(
      l => l.use_serial_numbers && l.serial_numbers.length < l.qty_to_receive
    );
    if (serialIncomplete.length > 0) {
      setError(
        `Serial numbers required: ${serialIncomplete.map(l => l.sku).join(', ')}. ` +
        `Enter one serial per unit before confirming.`
      );
      return;
    }
    setLoading(true);
    setError('');
    try {
      const res = await api(`/purchase-orders/${selectedPO.po_id}/receive`, {
        method: 'POST',
        body: JSON.stringify({
          lines: lines.map(l => ({
            line_id: l.line_id,
            qty_received: l.qty_to_receive,
            binrack_id: l.binrack_id || undefined,
            batch: l.batch_mode === 'new' ? {
              batch_number: l.batch_number,
              batch_type: l.use_serial_numbers ? 'Serial' : l.batch_type,
              expiry_date: l.expiry_date || undefined,
              sell_by: l.sell_by || undefined,
            } : l.batch_mode === 'existing' ? {
              existing_batch_id: l.existing_batch_id,
            } : undefined,
            serial_numbers: l.use_serial_numbers ? l.serial_numbers : undefined,
          })),
        }),
      });
      if (!res.ok) throw new Error(await res.text());

      if (printLabels) {
        await triggerLabelPrint(lines.map(l => ({ sku: l.sku, qty: l.qty_to_receive })));
      }

      setSuccess(`✅ Received goods for ${selectedPO.po_number}`);
      setSelectedPO(null);
      setLines([]);
      loadPOs();
    } catch (e: any) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }

  async function confirmAdhoc() {
    if (!adhocSku || adhocQty <= 0) return;
    setLoading(true);
    setError('');
    try {
      const res = await api('/inventory/adjust', {
        method: 'POST',
        body: JSON.stringify({
          product_sku: adhocSku,
          binrack_id: adhocBinrackId || undefined,
          delta: adhocQty,
          type: 'stock_in',
          reason: 'Ad-hoc stock in',
        }),
      });
      if (!res.ok) throw new Error(await res.text());

      if (printLabels) {
        await triggerLabelPrint([{ sku: adhocSku, qty: adhocQty }]);
      }

      setSuccess(`✅ Booked in ${adhocQty}x ${adhocSku}`);
      setAdhocSku('');
      setAdhocQty(1);
      setAdhocBinrackId('');
    } catch (e: any) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }

  const inputStyle: React.CSSProperties = { padding: '4px 8px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', fontSize: 12 };

  return (
    <div style={{ padding: 24, maxWidth: 1300, margin: '0 auto' }}>
      <h1 style={{ color: 'var(--text-primary)', marginBottom: 4 }}>📥 Stock In</h1>
      <p style={{ color: 'var(--text-muted)', marginBottom: 24 }}>Book stock in from a purchase order or ad-hoc.</p>

      {success && (
        <div style={{ background: 'rgba(34,197,94,0.1)', border: '1px solid rgba(34,197,94,0.3)', borderRadius: 8, padding: '12px 16px', marginBottom: 16, color: '#22c55e' }}>
          {success}
        </div>
      )}
      {error && (
        <div style={{ background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 8, padding: '12px 16px', marginBottom: 16, color: '#ef4444' }}>
          {error}
        </div>
      )}

      {/* Mode selector */}
      <div style={{ display: 'flex', gap: 8, marginBottom: 24 }}>
        {(['po', 'adhoc'] as const).map(m => (
          <button
            key={m}
            onClick={() => { setMode(m); setSelectedPO(null); setLines([]); setError(''); setSuccess(''); }}
            style={{
              padding: '10px 20px', borderRadius: 8, border: '1px solid',
              borderColor: mode === m ? 'var(--primary)' : 'var(--border)',
              background: mode === m ? 'var(--primary)' : 'var(--bg-elevated)',
              color: mode === m ? 'white' : 'var(--text-secondary)',
              cursor: 'pointer', fontWeight: 600,
            }}
          >
            {m === 'po' ? '📋 Against PO' : '🔍 Ad Hoc'}
          </button>
        ))}
        <div style={{ marginLeft: 'auto', display: 'flex', alignItems: 'center', gap: 8 }}>
          <label style={{ display: 'flex', alignItems: 'center', gap: 6, color: 'var(--text-secondary)', cursor: 'pointer' }}>
            <input type="checkbox" checked={printLabels} onChange={e => setPrintLabels(e.target.checked)} />
            Print labels on book-in
          </label>
        </div>
      </div>

      {/* PO mode - list */}
      {mode === 'po' && !selectedPO && (
        <div>
          <h3 style={{ color: 'var(--text-primary)', marginBottom: 12 }}>Select Purchase Order</h3>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
            {pos.length === 0 && <p style={{ color: 'var(--text-muted)' }}>No pending purchase orders found.</p>}
            {pos.map(po => (
              <div key={po.po_id}
                onClick={() => selectPO(po)}
                style={{
                  background: 'var(--bg-elevated)', border: '1px solid var(--border)',
                  borderRadius: 8, padding: '16px 20px', cursor: 'pointer',
                  display: 'flex', alignItems: 'center', justifyContent: 'space-between',
                }}
                onMouseEnter={e => (e.currentTarget.style.borderColor = 'var(--primary)')}
                onMouseLeave={e => (e.currentTarget.style.borderColor = 'var(--border)')}
              >
                <div>
                  <div style={{ fontWeight: 700, color: 'var(--text-primary)' }}>{po.po_number}</div>
                  <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>{po.supplier_name} · {po.lines?.length || 0} lines</div>
                </div>
                <span style={{ background: 'rgba(251,191,36,0.15)', color: '#fbbf24', padding: '3px 10px', borderRadius: 6, fontSize: 12, fontWeight: 600 }}>
                  {po.status}
                </span>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* PO mode - receive */}
      {mode === 'po' && selectedPO && (
        <div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 16 }}>
            <button onClick={() => { setSelectedPO(null); setLines([]); }}
              style={{ background: 'none', border: '1px solid var(--border)', borderRadius: 6, padding: '6px 12px', color: 'var(--text-muted)', cursor: 'pointer' }}>
              ← Back
            </button>
            <h3 style={{ color: 'var(--text-primary)', margin: 0 }}>Receiving: {selectedPO.po_number}</h3>
          </div>

          {/* Silently fetch serial flags for each SKU — updates lines state */}
          {lines.map(line => (
            <SerialFlagLoader
              key={`sfl-${line.line_id}`}
              sku={line.sku}
              lineId={line.line_id}
              onFlagLoaded={handleSerialFlagLoaded}
            />
          ))}

          <div style={{ overflowX: 'auto' }}>
            <table style={{ width: '100%', borderCollapse: 'collapse', marginBottom: 20, fontSize: 13 }}>
              <thead>
                <tr style={{ borderBottom: '1px solid var(--border)' }}>
                  {['SKU', 'Description', 'Ordered', 'Prev Rcvd', 'Qty to Rcv', 'Binrack', 'Batch Mode', 'Batch Details', 'Serial Numbers'].map(h => (
                    <th key={h} style={{ padding: '8px 10px', textAlign: 'left', color: 'var(--text-muted)', fontSize: 11, fontWeight: 600, whiteSpace: 'nowrap' }}>{h}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {lines.map((line, idx) => (
                  <tr key={line.line_id} style={{ borderBottom: '1px solid var(--border)', verticalAlign: 'top' }}>
                    <td style={{ padding: '8px 10px', color: 'var(--text-primary)', fontWeight: 600, fontFamily: 'monospace', fontSize: 12 }}>
                      {line.sku}
                      {line.use_serial_numbers && <> <SerialRequiredBadge /></>}
                    </td>
                    <td style={{ padding: '8px 10px', color: 'var(--text-secondary)', fontSize: 12, maxWidth: 160 }}>{line.description}</td>
                    <td style={{ padding: '8px 10px', color: 'var(--text-secondary)' }}>{line.qty_ordered}</td>
                    <td style={{ padding: '8px 10px', color: 'var(--text-muted)' }}>{line.qty_received}</td>
                    <td style={{ padding: '8px 10px' }}>
                      <input type="number" min={0} max={line.qty_ordered - line.qty_received}
                        value={line.qty_to_receive}
                        onChange={e => updateLine(idx, 'qty_to_receive', parseInt(e.target.value) || 0)}
                        style={{ ...inputStyle, width: 70 }} />
                    </td>
                    <td style={{ padding: '8px 10px' }}>
                      <select value={line.binrack_id} onChange={e => updateLine(idx, 'binrack_id', e.target.value)}
                        style={{ ...inputStyle, width: 130 }}>
                        <option value="">No binrack</option>
                        {binracks.map(b => <option key={b.binrack_id} value={b.binrack_id}>{b.name}</option>)}
                      </select>
                    </td>
                    <td style={{ padding: '8px 10px' }}>
                      <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
                        {(['none', 'new', 'existing'] as const).map(m => (
                          <label key={m} style={{ display: 'flex', alignItems: 'center', gap: 5, cursor: 'pointer', fontSize: 12 }}>
                            <input type="radio" name={`batch-${idx}`} value={m} checked={line.batch_mode === m}
                              onChange={() => updateLine(idx, 'batch_mode', m)} />
                            {m === 'none' ? 'None' : m === 'new' ? 'New Batch' : 'Existing'}
                          </label>
                        ))}
                      </div>
                    </td>
                    <td style={{ padding: '8px 10px', minWidth: 180 }}>
                      {line.use_serial_numbers && (
                        <SerialNumberInput
                          quantity={line.qty_to_receive}
                          value={line.serial_numbers}
                          onChange={sn => updateLine(idx, 'serial_numbers', sn)}
                          compact={true}
                        />
                      )}
                      {!line.use_serial_numbers && line.batch_mode === 'none' && (
                        <span style={{ color: 'var(--text-muted)', fontSize: 11, fontStyle: 'italic' }}>—</span>
                      )}
                    </td>
                    <td style={{ padding: '8px 10px', minWidth: 220 }}>
                      {line.batch_mode === 'new' && (
                        <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                          <input placeholder="Batch #" value={line.batch_number} onChange={e => updateLine(idx, 'batch_number', e.target.value)} style={{ ...inputStyle, width: '100%' }} />
                          <select value={line.batch_type} onChange={e => updateLine(idx, 'batch_type', e.target.value)} style={{ ...inputStyle, width: '100%' }}>
                            <option value="Standard">Standard</option>
                            <option value="Lot">Lot</option>
                            <option value="Serial">Serial</option>
                          </select>
                          <input type="date" placeholder="Expiry" value={line.expiry_date} onChange={e => updateLine(idx, 'expiry_date', e.target.value)} style={{ ...inputStyle, width: '100%' }} />
                          <input type="date" placeholder="Sell by" value={line.sell_by} onChange={e => updateLine(idx, 'sell_by', e.target.value)} style={{ ...inputStyle, width: '100%' }} />
                        </div>
                      )}
                      {line.batch_mode === 'existing' && (
                        <select value={line.existing_batch_id} onChange={e => updateLine(idx, 'existing_batch_id', e.target.value)} style={{ ...inputStyle, width: '100%' }}>
                          <option value="">Select batch…</option>
                          {(existingBatches[line.sku] || []).map(b => (
                            <option key={b.batch_id} value={b.batch_id}>{b.batch_number}{b.expiry_date ? ` (exp ${b.expiry_date})` : ''}</option>
                          ))}
                        </select>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          <div style={{ display: 'flex', gap: 12, justifyContent: 'flex-end' }}>
            <button onClick={() => { setSelectedPO(null); setLines([]); }}
              style={{ padding: '10px 20px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-secondary)', cursor: 'pointer' }}>
              Cancel
            </button>
            <button onClick={confirmPO} disabled={loading}
              style={{ padding: '10px 24px', background: 'var(--primary)', border: 'none', borderRadius: 8, color: 'white', fontWeight: 700, cursor: 'pointer' }}>
              {loading ? 'Booking In…' : `✅ Confirm Receipt${printLabels ? ' + Print Labels' : ''}`}
            </button>
          </div>
        </div>
      )}

      {/* Ad-hoc mode */}
      {mode === 'adhoc' && (
        <div style={{ maxWidth: 500 }}>
          <h3 style={{ color: 'var(--text-primary)', marginBottom: 16 }}>Ad Hoc Stock In</h3>
          <p style={{ color: 'var(--text-muted)', fontSize: 13, marginBottom: 20 }}>
            Scan a barcode or type a SKU to book stock in without a purchase order.
          </p>

          <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
            <div>
              <label style={{ display: 'block', marginBottom: 6, fontSize: 13, color: 'var(--text-secondary)' }}>SKU / Barcode</label>
              <input
                ref={skuInputRef}
                autoFocus
                type="text"
                placeholder="Scan or type SKU…"
                value={adhocSku}
                onChange={e => setAdhocSku(e.target.value)}
                style={{ width: '100%', padding: '10px 14px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-primary)', fontSize: 15 }}
              />
            </div>
            <div>
              <label style={{ display: 'block', marginBottom: 6, fontSize: 13, color: 'var(--text-secondary)' }}>Quantity</label>
              <input type="number" min={1} value={adhocQty} onChange={e => setAdhocQty(parseInt(e.target.value) || 1)}
                style={{ width: '100%', padding: '10px 14px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-primary)' }} />
            </div>
            <div>
              <label style={{ display: 'block', marginBottom: 6, fontSize: 13, color: 'var(--text-secondary)' }}>Binrack</label>
              <select value={adhocBinrackId} onChange={e => setAdhocBinrackId(e.target.value)}
                style={{ width: '100%', padding: '10px 14px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-primary)', fontSize: 14 }}>
                <option value="">No binrack (use default location)</option>
                {binracks.map(b => <option key={b.binrack_id} value={b.binrack_id}>{b.name}{b.barcode ? ` [${b.barcode}]` : ''}</option>)}
              </select>
            </div>
            <button onClick={confirmAdhoc} disabled={loading || !adhocSku}
              style={{ padding: '12px', background: 'var(--primary)', border: 'none', borderRadius: 8, color: 'white', fontWeight: 700, cursor: 'pointer', marginTop: 8 }}>
              {loading ? 'Booking In…' : `✅ Book In ${adhocQty}x ${adhocSku || '?'}${printLabels ? ' + Print Labels' : ''}`}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

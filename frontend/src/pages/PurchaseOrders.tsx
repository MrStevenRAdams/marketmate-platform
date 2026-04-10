import { useState, useEffect, useCallback } from 'react';
import { getActiveTenantId } from '../contexts/TenantContext';
import { getAuth } from 'firebase/auth';
import './PurchaseOrders.css';

const API_BASE = import.meta.env.VITE_API_URL ?? 'http://localhost:8080/api/v1';

async function getAuthToken(): Promise<string> {
  try {
    const auth = getAuth();
    const user = auth.currentUser;
    if (user) return await user.getIdToken();
  } catch { /* fall through */ }
  return localStorage.getItem('auth_token') || '';
}

async function api(path: string, init?: RequestInit) {
  const token = await getAuthToken();
  const tenantId = getActiveTenantId() || localStorage.getItem('tenantId') || localStorage.getItem('marketmate_tenant_id') || '';
  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      'X-Tenant-Id': tenantId,
      ...(token ? { 'Authorization': `Bearer ${token}` } : {}),
      ...init?.headers,
    },
  });
}

// ─── Types ─────────────────────────────────────────────────────────────────

interface POLine {
  line_id: string;
  product_id?: string;
  internal_sku?: string;
  sku?: string;
  supplier_sku?: string;
  description?: string;
  title?: string;
  qty_ordered: number;
  qty_received: number;
  unit_cost?: number;
  currency?: string;
  notes?: string;
}

interface ReceiptLine {
  line_id: string;
  qty_received: number;
  variance: number;
  notes?: string;
}

interface POReceipt {
  receipt_id: string;
  received_at: string;
  received_by: string;
  notes?: string;
  lines: ReceiptLine[];
}

interface PurchaseOrder {
  po_id: string;
  tenant_id: string;
  po_number: string;
  supplier_id: string;
  supplier_name?: string;
  type: string;
  order_method?: string;
  status: string;
  lines: POLine[];
  receipts?: POReceipt[];
  total_cost?: number;
  currency?: string;
  notes?: string;
  dropship_order_id?: string;
  tracking_number?: string;
  tracking_url?: string;
  carrier_name?: string;
  expected_at?: string;
  created_at: string;
  updated_at: string;
  sent_at?: string;
  order_ids?: string[];
}

interface Supplier {
  supplier_id: string;
  name: string;
  code: string;
  email?: string;
  currency?: string;
  order_method?: string;
  active: boolean;
}

interface DraftLine {
  internal_sku: string;
  supplier_sku: string;
  description: string;
  qty_ordered: number;
  unit_cost: number;
  currency: string;
}

interface ReorderSuggestion {
  suggestion_id: string;
  sku: string;
  product_id?: string;
  product_name: string;
  current_stock: number;
  reorder_point: number;
  suggested_qty: number;
  supplier_id: string;
  supplier_name: string;
  supplier_sku?: string;
  unit_cost?: number;
  currency?: string;
  status: string;
  approved_po_id?: string;
  approved_po_number?: string;
}

// ─── Config ─────────────────────────────────────────────────────────────────

const STATUS_CONFIG: Record<string, { label: string; cls: string }> = {
  draft:              { label: 'Draft',          cls: 'po-status-draft' },
  sent:               { label: 'Sent',           cls: 'po-status-sent' },
  acknowledged:       { label: 'Acknowledged',   cls: 'po-status-ack' },
  partially_received: { label: 'Part. Received', cls: 'po-status-partial' },
  received:           { label: 'Received',       cls: 'po-status-received' },
  shipped:            { label: 'Shipped',        cls: 'po-status-shipped' },
  cancelled:          { label: 'Cancelled',      cls: 'po-status-cancelled' },
};

const STATUS_ORDER = ['draft', 'sent', 'acknowledged', 'partially_received', 'received', 'shipped', 'cancelled'];

function fmt(date?: string) {
  if (!date) return '—';
  try { return new Date(date).toLocaleDateString('en-GB', { day: '2-digit', month: 'short', year: 'numeric' }); }
  catch { return date; }
}

function fmtMoney(amount?: number, currency?: string) {
  if (amount == null) return '—';
  try { return new Intl.NumberFormat('en-GB', { style: 'currency', currency: currency || 'GBP' }).format(amount); }
  catch { return `${(currency || '£')}${amount.toFixed(2)}`; }
}

function emptyLine(): DraftLine {
  return { internal_sku: '', supplier_sku: '', description: '', qty_ordered: 1, unit_cost: 0, currency: 'GBP' };
}

// ─── Create PO Modal ─────────────────────────────────────────────────────────

function CreatePOModal({ suppliers, onClose, onCreated }: {
  suppliers: Supplier[];
  onClose: () => void;
  onCreated: () => void;
}) {
  const [supplierID, setSupplierID] = useState('');
  const [poType, setPoType] = useState<'standard' | 'dropship'>('standard');
  const [orderMethod, setOrderMethod] = useState('');
  const [expectedAt, setExpectedAt] = useState('');
  const [notes, setNotes] = useState('');
  const [lines, setLines] = useState<DraftLine[]>([emptyLine()]);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  const selectedSupplier = suppliers.find(s => s.supplier_id === supplierID);

  useEffect(() => {
    if (selectedSupplier) {
      setOrderMethod(selectedSupplier.order_method || 'manual');
    }
  }, [supplierID]);

  const updateLine = (idx: number, field: keyof DraftLine, value: string | number) => {
    setLines(prev => prev.map((l, i) => i === idx ? { ...l, [field]: value } : l));
  };

  const addLine = () => setLines(prev => [...prev, emptyLine()]);
  const removeLine = (idx: number) => setLines(prev => prev.filter((_, i) => i !== idx));

  const save = async (sendNow: boolean) => {
    if (!supplierID) { setError('Please select a supplier'); return; }
    const validLines = lines.filter(l => l.internal_sku.trim() && l.qty_ordered > 0);
    if (validLines.length === 0) { setError('Add at least one line item with an SKU'); return; }

    setSaving(true);
    setError('');
    try {
      const body = {
        supplier_id: supplierID,
        type: poType,
        order_method: orderMethod || undefined,
        expected_at: expectedAt ? new Date(expectedAt).toISOString() : undefined,
        notes: notes || undefined,
        lines: validLines.map(l => ({
          internal_sku: l.internal_sku.trim(),
          supplier_sku: l.supplier_sku.trim() || undefined,
          description: l.description.trim() || l.internal_sku.trim(),
          qty_ordered: Number(l.qty_ordered),
          unit_cost: Number(l.unit_cost),
          currency: l.currency || 'GBP',
        })),
      };

      const res = await api('/purchase-orders', { method: 'POST', body: JSON.stringify(body) });
      if (!res.ok) {
        const d = await res.json();
        throw new Error(d.error || 'Failed to create purchase order');
      }

      const created = await res.json();
      const poID: string = created.purchase_order?.po_id;

      if (sendNow && poID) {
        const sendRes = await api(`/purchase-orders/${poID}/send`, { method: 'POST' });
        if (!sendRes.ok) {
          const d = await sendRes.json();
          setError(`PO created but could not send: ${d.error}`);
          setSaving(false);
          onCreated();
          return;
        }
      }

      onCreated();
    } catch (e: any) {
      setError(e.message);
      setSaving(false);
    }
  };

  return (
    <div className="po-overlay" onClick={onClose}>
      <div className="po-modal po-modal-lg" onClick={e => e.stopPropagation()}>
        <div className="po-modal-header">
          <h2>Create Purchase Order</h2>
          <button className="po-modal-close" onClick={onClose}>✕</button>
        </div>

        {error && <div className="po-modal-error">{error}</div>}

        <div className="po-modal-body">
          <div className="po-form-row">
            <div className="po-form-group po-flex-2">
              <label>Supplier <span className="po-required">*</span></label>
              <select value={supplierID} onChange={e => setSupplierID(e.target.value)} className="po-select">
                <option value="">— Select supplier —</option>
                {suppliers.filter(s => s.active).map(s => (
                  <option key={s.supplier_id} value={s.supplier_id}>{s.name} ({s.code})</option>
                ))}
              </select>
            </div>

            <div className="po-form-group">
              <label>Type</label>
              <div className="po-radio-group">
                {(['standard', 'dropship'] as const).map(t => (
                  <label key={t} className={`po-radio ${poType === t ? 'po-radio-active' : ''}`}>
                    <input type="radio" value={t} checked={poType === t} onChange={() => setPoType(t)} />
                    {t.charAt(0).toUpperCase() + t.slice(1)}
                  </label>
                ))}
              </div>
            </div>

            <div className="po-form-group">
              <label>Order Method</label>
              <select value={orderMethod} onChange={e => setOrderMethod(e.target.value)} className="po-select">
                <option value="">Supplier default</option>
                <option value="email">Email</option>
                <option value="webhook">Webhook</option>
                <option value="manual">Manual</option>
              </select>
            </div>
          </div>

          <div className="po-form-row">
            <div className="po-form-group">
              <label>Expected Delivery</label>
              <input type="date" value={expectedAt} onChange={e => setExpectedAt(e.target.value)} className="po-input" />
            </div>
            <div className="po-form-group po-flex-2">
              <label>Notes</label>
              <input type="text" value={notes} onChange={e => setNotes(e.target.value)}
                className="po-input" placeholder="Optional notes to supplier…" />
            </div>
          </div>

          <div className="po-lines-section">
            <div className="po-lines-header">
              <h3>Line Items</h3>
              <button type="button" className="btn btn-ghost btn-sm" onClick={addLine}>+ Add Line</button>
            </div>
            <div className="po-lines-edit-wrap">
              <table className="po-lines-edit-table">
                <thead>
                  <tr>
                    <th>Internal SKU *</th>
                    <th>Supplier SKU</th>
                    <th>Description</th>
                    <th>Qty *</th>
                    <th>Unit Cost</th>
                    <th>Currency</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {lines.map((line, idx) => (
                    <tr key={idx}>
                      <td>
                        <input className="po-input po-input-sm" value={line.internal_sku}
                          onChange={e => updateLine(idx, 'internal_sku', e.target.value)}
                          placeholder="SKU-001" />
                      </td>
                      <td>
                        <input className="po-input po-input-sm" value={line.supplier_sku}
                          onChange={e => updateLine(idx, 'supplier_sku', e.target.value)}
                          placeholder="Optional" />
                      </td>
                      <td>
                        <input className="po-input po-input-sm" value={line.description}
                          onChange={e => updateLine(idx, 'description', e.target.value)}
                          placeholder="Product name…" />
                      </td>
                      <td>
                        <input className="po-input po-input-sm po-input-num" type="number" min="1"
                          value={line.qty_ordered}
                          onChange={e => updateLine(idx, 'qty_ordered', parseInt(e.target.value) || 1)} />
                      </td>
                      <td>
                        <input className="po-input po-input-sm po-input-num" type="number" min="0" step="0.01"
                          value={line.unit_cost}
                          onChange={e => updateLine(idx, 'unit_cost', parseFloat(e.target.value) || 0)} />
                      </td>
                      <td>
                        <select className="po-select po-select-sm" value={line.currency}
                          onChange={e => updateLine(idx, 'currency', e.target.value)}>
                          {['GBP', 'USD', 'EUR', 'CAD', 'AUD'].map(c => <option key={c}>{c}</option>)}
                        </select>
                      </td>
                      <td>
                        {lines.length > 1 && (
                          <button type="button" className="po-remove-line" onClick={() => removeLine(idx)}>✕</button>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </div>

        <div className="po-modal-footer">
          <button className="btn btn-ghost" onClick={onClose} disabled={saving}>Cancel</button>
          <button className="btn btn-secondary" onClick={() => save(false)} disabled={saving}>
            {saving ? 'Saving…' : 'Save Draft'}
          </button>
          <button className="btn btn-primary" onClick={() => save(true)} disabled={saving}>
            {saving ? 'Saving…' : 'Save & Send'}
          </button>
        </div>
      </div>
    </div>
  );
}

// ─── Goods-In Modal ─────────────────────────────────────────────────────────

function GoodsInModal({ po, onClose, onReceived }: {
  po: PurchaseOrder;
  onClose: () => void;
  onReceived: () => void;
}) {
  const deliveryNumber = (po.receipts?.length || 0) + 1;
  const [receiptLines, setReceiptLines] = useState<Record<string, number>>(() => {
    const init: Record<string, number> = {};
    po.lines.forEach(l => { init[l.line_id] = 0; });
    return init;
  });
  const priorDeliveries = po.receipts?.length || 0;
  const [notes, setNotes] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState('');

  const variance = (line: POLine, qtyNow: number) =>
    qtyNow - (line.qty_ordered - line.qty_received);

  const varianceCls = (v: number) => v === 0 ? 'po-variance-ok' : v < 0 ? 'po-variance-short' : 'po-variance-over';
  const varianceLbl = (v: number) => v === 0 ? '✓ Exact' : v < 0 ? `${v} short` : `+${v} overage`;

  const submit = async () => {
    const lines = Object.entries(receiptLines)
      .filter(([, qty]) => qty > 0)
      .map(([lineID, qty]) => ({ line_id: lineID, qty_received: qty }));
    if (lines.length === 0) { setError('Enter a quantity for at least one line'); return; }

    setSubmitting(true);
    setError('');
    try {
      const res = await api(`/purchase-orders/${po.po_id}/receive`, {
        method: 'POST',
        body: JSON.stringify({ lines, notes: notes || undefined }),
      });
      if (!res.ok) {
        const d = await res.json();
        throw new Error(d.error || 'Failed to record receipt');
      }
      onReceived();
    } catch (e: any) {
      setError(e.message);
      setSubmitting(false);
    }
  };

  return (
    <div className="po-overlay" onClick={onClose}>
      <div className="po-modal po-modal-lg" onClick={e => e.stopPropagation()}>
        <div className="po-modal-header">
          <h2>📦 Delivery #{deliveryNumber} — {po.po_number}</h2>
          <button className="po-modal-close" onClick={onClose}>✕</button>
        </div>
        {error && <div className="po-modal-error">{error}</div>}
        {priorDeliveries > 0 && (
          <div style={{ margin:'8px 20px 0', padding:'8px 12px', borderRadius:7, background:'rgba(245,158,11,0.1)', border:'1px solid rgba(245,158,11,0.3)', fontSize:12, color:'var(--warning)', fontWeight:600 }}>
            ⚠ This PO already has {priorDeliveries} prior {priorDeliveries === 1 ? 'delivery' : 'deliveries'}. You can record additional quantities received, including overages.
          </div>
        )}
        <div className="po-modal-body">
          <table className="po-goods-in-table">
            <thead>
              <tr>
                <th>SKU</th>
                <th>Description</th>
                <th>Ordered</th>
                <th>Received</th>
                <th>Remaining</th>
                <th>Receive Now</th>
                <th>Variance</th>
              </tr>
            </thead>
            <tbody>
              {po.lines.map(line => {
                const remaining = line.qty_ordered - line.qty_received;
                const qtyNow = receiptLines[line.line_id] ?? 0;
                const v = variance(line, qtyNow);
                return (
                  <tr key={line.line_id}>
                    <td><code className="po-sku">{line.internal_sku || line.sku || '—'}</code></td>
                    <td className="po-gi-desc">{line.description || line.title || '—'}</td>
                    <td>{line.qty_ordered}</td>
                    <td>{line.qty_received}</td>
                    <td className={remaining <= 0 ? 'po-text-muted' : ''}>{Math.max(0, remaining)}</td>
                    <td>
                      <input type="number" min="0" max={line.qty_ordered * 2}
                        value={qtyNow}
                        onChange={e => setReceiptLines(p => ({ ...p, [line.line_id]: parseInt(e.target.value) || 0 }))}
                        className="po-input po-input-sm po-input-num"
                        disabled={false} />
                    </td>
                    <td>
                      {qtyNow > 0 && (
                        <span className={`po-variance ${varianceCls(v)}`}>{varianceLbl(v)}</span>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
          <div className="po-form-group po-mt-md">
            <label>Notes</label>
            <input className="po-input" value={notes} onChange={e => setNotes(e.target.value)}
              placeholder="Optional notes for this delivery…" />
          </div>
        </div>
        <div className="po-modal-footer">
          <button className="btn btn-ghost" onClick={onClose} disabled={submitting}>Cancel</button>
          <button className="btn btn-primary" onClick={submit} disabled={submitting}>
            {submitting ? 'Recording…' : 'Record Delivery'}
          </button>
        </div>
      </div>
    </div>
  );
}

// ─── Tracking Modal ──────────────────────────────────────────────────────────

function TrackingModal({ po, onClose, onSaved }: {
  po: PurchaseOrder;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [trackingNumber, setTrackingNumber] = useState(po.tracking_number || '');
  const [trackingURL, setTrackingURL] = useState(po.tracking_url || '');
  const [carrierName, setCarrierName] = useState(po.carrier_name || '');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  const save = async () => {
    if (!trackingNumber.trim()) { setError('Tracking number is required'); return; }
    setSaving(true);
    setError('');
    try {
      const res = await api(`/purchase-orders/${po.po_id}/tracking`, {
        method: 'POST',
        body: JSON.stringify({ tracking_number: trackingNumber, tracking_url: trackingURL, carrier_name: carrierName }),
      });
      if (!res.ok) { const d = await res.json(); throw new Error(d.error || 'Failed'); }
      onSaved();
    } catch (e: any) {
      setError(e.message);
      setSaving(false);
    }
  };

  return (
    <div className="po-overlay" onClick={onClose}>
      <div className="po-modal po-modal-sm" onClick={e => e.stopPropagation()}>
        <div className="po-modal-header">
          <h2>Tracking — {po.po_number}</h2>
          <button className="po-modal-close" onClick={onClose}>✕</button>
        </div>
        {error && <div className="po-modal-error">{error}</div>}
        <div className="po-modal-body">
          <div className="po-form-group">
            <label>Tracking Number <span className="po-required">*</span></label>
            <input className="po-input" value={trackingNumber} onChange={e => setTrackingNumber(e.target.value)} placeholder="e.g. JD123456789GB" />
          </div>
          <div className="po-form-group">
            <label>Carrier</label>
            <input className="po-input" value={carrierName} onChange={e => setCarrierName(e.target.value)} placeholder="e.g. Royal Mail" />
          </div>
          <div className="po-form-group">
            <label>Tracking URL</label>
            <input className="po-input" value={trackingURL} onChange={e => setTrackingURL(e.target.value)} placeholder="https://…" />
          </div>
        </div>
        <div className="po-modal-footer">
          <button className="btn btn-ghost" onClick={onClose} disabled={saving}>Cancel</button>
          <button className="btn btn-primary" onClick={save} disabled={saving}>{saving ? 'Saving…' : 'Save Tracking'}</button>
        </div>
      </div>
    </div>
  );
}

// ─── PO Detail ──────────────────────────────────────────────────────────────

function PODetail({ po, onAction }: { po: PurchaseOrder; onAction: () => void }) {
  const [showGoodsIn, setShowGoodsIn] = useState(false);
  const [showTracking, setShowTracking] = useState(false);
  const [actionLoading, setActionLoading] = useState('');

  const doAction = async (action: string) => {
    setActionLoading(action);
    try {
      const res = await api(`/purchase-orders/${po.po_id}/${action}`, { method: 'POST' });
      if (!res.ok) { const d = await res.json(); alert(d.error || `Failed: ${action}`); }
      else onAction();
    } catch { alert(`Failed: ${action}`); }
    setActionLoading('');
  };

  const canReceive = ['sent', 'acknowledged', 'partially_received', 'received'].includes(po.status);
  const canSend = po.status === 'draft';
  const canCancel = !['received', 'cancelled'].includes(po.status);
  const isDropship = po.type === 'dropship';

  return (
    <div className="po-detail">
      <div className="po-detail-actions">
        {canSend && (
          <button className="btn btn-primary btn-sm" disabled={actionLoading === 'send'} onClick={() => doAction('send')}>
            {actionLoading === 'send' ? 'Sending…' : '📤 Send PO'}
          </button>
        )}
        {canReceive && (
          <button className="btn btn-success btn-sm" onClick={() => setShowGoodsIn(true)}>
            📦 {po.receipts && po.receipts.length > 0 ? `Record Delivery #${po.receipts.length + 1}` : 'Receive Goods'}
          </button>
        )}
        {isDropship && po.status !== 'cancelled' && (
          <button className="btn btn-secondary btn-sm" onClick={() => setShowTracking(true)}>
            🚚 {po.tracking_number ? 'Update Tracking' : 'Mark as Shipped'}
          </button>
        )}
        {canCancel && (
          <button className="btn btn-danger btn-sm" disabled={actionLoading === 'cancel'}
            onClick={() => { if (confirm('Cancel this purchase order?')) doAction('cancel'); }}>
            {actionLoading === 'cancel' ? 'Cancelling…' : 'Cancel PO'}
          </button>
        )}
      </div>

      {isDropship && po.tracking_number && (
        <div className="po-tracking-bar">
          <span className="po-tracking-label">Tracking:</span>
          <code className="po-tracking-num">{po.tracking_number}</code>
          {po.carrier_name && <span className="po-tracking-carrier">via {po.carrier_name}</span>}
          {po.tracking_url && (
            <a href={po.tracking_url} target="_blank" rel="noopener noreferrer" className="btn btn-xs btn-ghost">Track →</a>
          )}
        </div>
      )}

      <div className="po-detail-section">
        <h4>Line Items</h4>
        <table className="po-lines-table">
          <thead>
            <tr>
              <th>Internal SKU</th>
              <th>Supplier SKU</th>
              <th>Description</th>
              <th>Ordered</th>
              <th>Received</th>
              <th>Unit Cost</th>
              <th>Line Total</th>
            </tr>
          </thead>
          <tbody>
            {(po.lines || []).map((line, i) => {
              const total = (line.unit_cost || 0) * (line.qty_ordered || 0);
              const pct = line.qty_ordered > 0 ? (line.qty_received / line.qty_ordered) * 100 : 0;
              return (
                <tr key={line.line_id || i}>
                  <td><code className="po-sku">{line.internal_sku || line.sku || '—'}</code></td>
                  <td><code className="po-sku po-sku-supplier">{line.supplier_sku || '—'}</code></td>
                  <td>{line.description || line.title || '—'}</td>
                  <td>{line.qty_ordered}</td>
                  <td>
                    <div className="po-recv-cell">
                      <span>{line.qty_received} / {line.qty_ordered}</span>
                      <div className="po-progress-bar">
                        <div className="po-progress-fill" style={{ width: `${Math.min(pct, 100)}%` }} />
                      </div>
                    </div>
                  </td>
                  <td>{fmtMoney(line.unit_cost, line.currency)}</td>
                  <td>{fmtMoney(total, line.currency)}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>

      {po.receipts && po.receipts.length > 0 && (
        <div className="po-detail-section">
          <div style={{ display:'flex', alignItems:'center', justifyContent:'space-between', marginBottom:10 }}>
            <h4 style={{ margin:0 }}>📦 Delivery History ({po.receipts.length} {po.receipts.length === 1 ? 'delivery' : 'deliveries'})</h4>
            {canReceive && (
              <button className="btn btn-success btn-sm" style={{ fontSize:11 }} onClick={() => setShowGoodsIn(true)}>
                + New Delivery
              </button>
            )}
          </div>
          <div className="po-receipts" style={{ display:'flex', flexDirection:'column', gap:8 }}>
            {po.receipts.map((receipt, idx) => {
              const totalReceived = receipt.lines.reduce((s: number, l: any) => s + (l.qty_received || 0), 0);
              return (
                <div key={receipt.receipt_id} className="po-receipt" style={{ borderRadius:10, background:'var(--bg-elevated)', border:'1px solid var(--border)', overflow:'hidden' }}>
                  <div className="po-receipt-header" style={{ display:'flex', alignItems:'center', gap:10, padding:'9px 14px', borderBottom: receipt.lines.length > 0 ? '1px solid var(--border)' : 'none', background:'var(--bg-secondary)' }}>
                    <span style={{ fontSize:12, fontWeight:700, color:'var(--text-muted)', background:'var(--bg-tertiary)', borderRadius:6, padding:'2px 7px' }}>#{idx + 1}</span>
                    <span style={{ fontWeight:600, fontSize:13 }}>{fmt(receipt.received_at)}</span>
                    <span style={{ fontSize:12, color:'var(--text-muted)' }}>·</span>
                    <span style={{ fontSize:12, color:'var(--success)', fontWeight:600 }}>{totalReceived} units</span>
                    {receipt.notes && <span className="po-receipt-notes" style={{ fontSize:11, color:'var(--text-muted)', fontStyle:'italic', marginLeft:'auto' }}>{receipt.notes}</span>}
                  </div>
                  {receipt.lines.length > 0 && (
                    <div style={{ padding:'8px 14px', display:'flex', flexDirection:'column', gap:4 }}>
                      {receipt.lines.map((rl: any) => {
                        const poLine = po.lines.find((l: any) => l.line_id === rl.line_id);
                        return (
                          <div key={rl.line_id} style={{ display:'flex', alignItems:'center', gap:8, fontSize:12 }}>
                            <code style={{ fontSize:11, color:'var(--text-muted)', background:'var(--bg-tertiary)', padding:'1px 5px', borderRadius:4 }}>{poLine?.internal_sku || poLine?.sku || rl.line_id}</code>
                            <span style={{ fontWeight:600, color:'var(--text-primary)' }}>{rl.qty_received} received</span>
                            {rl.variance !== 0 && (
                              <span style={{ fontSize:11, fontWeight:600, color: rl.variance < 0 ? 'var(--warning)' : 'var(--danger)', background: rl.variance < 0 ? 'rgba(245,158,11,0.1)' : 'rgba(239,68,68,0.1)', padding:'1px 6px', borderRadius:4 }}>
                                {rl.variance > 0 ? `+${rl.variance} over` : `${rl.variance} short`}
                              </span>
                            )}
                          </div>
                        );
                      })}
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        </div>
      )}

      {po.notes && (
        <div className="po-detail-section">
          <div className="po-notes">📝 {po.notes}</div>
        </div>
      )}

      {showGoodsIn && (
        <GoodsInModal po={po} onClose={() => setShowGoodsIn(false)}
          onReceived={() => { setShowGoodsIn(false); onAction(); }} />
      )}
      {showTracking && (
        <TrackingModal po={po} onClose={() => setShowTracking(false)}
          onSaved={() => { setShowTracking(false); onAction(); }} />
      )}
    </div>
  );
}

// ─── Main Component ──────────────────────────────────────────────────────────

export default function PurchaseOrders() {
  const [pos, setPOs] = useState<PurchaseOrder[]>([]);
  const [suppliers, setSuppliers] = useState<Supplier[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [statusFilter, setStatusFilter] = useState('');
  const [supplierFilter, setSupplierFilter] = useState('');
  const [expanded, setExpanded] = useState<string | null>(null);
  const [showCreate, setShowCreate] = useState(false);
  const [autoGenerating, setAutoGenerating] = useState(false);
  const [autoGenMsg, setAutoGenMsg] = useState('');
  const [autoGenMsgType, setAutoGenMsgType] = useState<'ok' | 'error'>('ok');

  // ── Task 4: Reorder Suggestions ───────────────────────────────────────────
  const [activeTab, setActiveTab] = useState<'orders' | 'suggestions'>('orders');
  const [suggestions, setSuggestions] = useState<ReorderSuggestion[]>([]);
  const [suggestionsLoading, setSuggestionsLoading] = useState(false);
  const [generatingSuggestions, setGeneratingSuggestions] = useState(false);
  const [approvingId, setApprovingId] = useState<string | null>(null);
  const [dismissingId, setDismissingId] = useState<string | null>(null);
  const [suggestionMsg, setSuggestionMsg] = useState('');
  const [editQty, setEditQty] = useState<Record<string, number>>({});

  const loadSuggestions = useCallback(async () => {
    setSuggestionsLoading(true);
    try {
      const res = await api('/purchase-orders/suggestions?status=pending');
      if (!res.ok) throw new Error('Failed to load suggestions');
      const data = await res.json();
      setSuggestions(data.suggestions || []);
    } catch (e: any) {
      setSuggestions([]);
    } finally {
      setSuggestionsLoading(false);
    }
  }, []);

  const handleGenerateSuggestions = async () => {
    setGeneratingSuggestions(true);
    setSuggestionMsg('');
    try {
      const res = await api('/purchase-orders/suggestions/generate', { method: 'POST', body: '{}' });
      const data = await res.json();
      if (!res.ok) throw new Error(data.error || 'Generate failed');
      setSuggestionMsg(data.message);
      await loadSuggestions();
    } catch (e: any) {
      setSuggestionMsg(`Error: ${e.message}`);
    } finally {
      setGeneratingSuggestions(false);
    }
  };

  const handleApproveSuggestion = async (s: ReorderSuggestion) => {
    setApprovingId(s.suggestion_id);
    setSuggestionMsg('');
    try {
      const qty = editQty[s.suggestion_id] ?? s.suggested_qty;
      const res = await api(`/purchase-orders/suggestions/${s.suggestion_id}/approve`, {
        method: 'POST',
        body: JSON.stringify({ qty }),
      });
      const data = await res.json();
      if (!res.ok) throw new Error(data.error || 'Approve failed');
      setSuggestionMsg(data.message);
      await loadSuggestions();
    } catch (e: any) {
      setSuggestionMsg(`Error: ${e.message}`);
    } finally {
      setApprovingId(null);
    }
  };

  const handleDismissSuggestion = async (suggestionId: string) => {
    setDismissingId(suggestionId);
    try {
      await api(`/purchase-orders/suggestions/${suggestionId}/dismiss`, { method: 'POST', body: '{}' });
      await loadSuggestions();
    } catch { /* silent */ } finally {
      setDismissingId(null);
    }
  };

  useEffect(() => {
    if (activeTab === 'suggestions') loadSuggestions();
  }, [activeTab, loadSuggestions]);

  // Load suggestion count on mount so banner can show on orders tab
  useEffect(() => {
    loadSuggestions();
  }, []); // eslint-disable-line

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const qs = new URLSearchParams();
      if (statusFilter) qs.set('status', statusFilter);
      if (supplierFilter) qs.set('supplier_id', supplierFilter);

      const [posRes, supRes] = await Promise.all([
        api(`/purchase-orders${qs.toString() ? '?' + qs : ''}`),
        api('/suppliers'),
      ]);

      if (!posRes.ok) throw new Error('Failed to load purchase orders');
      const posData = await posRes.json();
      setPOs(posData.purchase_orders || []);

      if (supRes.ok) {
        const supData = await supRes.json();
        setSuppliers(supData.suppliers || []);
      }
    } catch (e: any) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }, [statusFilter, supplierFilter]);

  useEffect(() => { load(); }, [load]);

  const handleAutoGenerate = async () => {
    setAutoGenerating(true);
    setAutoGenMsg('');
    try {
      const res = await api('/purchase-orders/auto-generate', { method: 'POST', body: JSON.stringify({}) });
      const data = await res.json();
      if (!res.ok) throw new Error(data.error || 'Auto-generate failed');
      setAutoGenMsg(data.message);
      setAutoGenMsgType('ok');
      load();
    } catch (e: any) {
      setAutoGenMsg(`Error: ${e.message}`);
      setAutoGenMsgType('error');
    } finally {
      setAutoGenerating(false);
    }
  };

  const countsByStatus = STATUS_ORDER.reduce((acc, s) => {
    acc[s] = pos.filter(p => p.status === s).length;
    return acc;
  }, {} as Record<string, number>);

  const supplierName = (id: string) => suppliers.find(s => s.supplier_id === id)?.name || id;

  return (
    <div className="po-page">
      <div className="po-header">
        <div>
          <h1 className="po-title">Purchase Orders</h1>
          <p className="po-subtitle">
            Create and manage purchase orders for stock replenishment and dropship fulfilment.
          </p>
        </div>
        <div className="po-header-actions">
          <button className="btn btn-ghost btn-sm" onClick={handleAutoGenerate} disabled={autoGenerating}>
            {autoGenerating ? '⟳ Generating…' : '⚡ Auto-generate'}
          </button>
          <button className="btn btn-ghost btn-sm" onClick={load}>↻ Refresh</button>
          <button className="btn btn-primary btn-sm" onClick={() => setShowCreate(true)}>
            + Create PO
          </button>
        </div>
      </div>

      {/* ── Tab bar ── */}
      <div style={{ display: 'flex', gap: 4, marginBottom: 16, borderBottom: '1px solid var(--border)', paddingBottom: 0 }}>
        <button
          onClick={() => setActiveTab('orders')}
          style={{
            background: 'none', border: 'none', padding: '8px 16px', cursor: 'pointer',
            fontSize: 13, fontWeight: 600,
            color: activeTab === 'orders' ? 'var(--primary)' : 'var(--text-muted)',
            borderBottom: activeTab === 'orders' ? '2px solid var(--primary)' : '2px solid transparent',
            marginBottom: -1,
          }}
        >
          📋 Purchase Orders
        </button>
        <button
          onClick={() => setActiveTab('suggestions')}
          style={{
            background: 'none', border: 'none', padding: '8px 16px', cursor: 'pointer',
            fontSize: 13, fontWeight: 600,
            color: activeTab === 'suggestions' ? 'var(--primary)' : 'var(--text-muted)',
            borderBottom: activeTab === 'suggestions' ? '2px solid var(--primary)' : '2px solid transparent',
            marginBottom: -1,
            display: 'flex', alignItems: 'center', gap: 6,
          }}
        >
          💡 Reorder Suggestions
          {suggestions.length > 0 && (
            <span style={{ background: '#ef4444', color: '#fff', fontSize: 10, fontWeight: 700, borderRadius: 10, padding: '1px 6px', lineHeight: 1.4 }}>
              {suggestions.length}
            </span>
          )}
        </button>
      </div>

      {autoGenMsg && (
        <div className={`po-auto-msg po-auto-msg--${autoGenMsgType}`}>
          {autoGenMsg}
          <button className="po-dismiss" onClick={() => setAutoGenMsg('')}>✕</button>
        </div>
      )}

      {/* ── Suggestions Tab ── */}
      {activeTab === 'suggestions' && (
        <div>
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 16 }}>
            <div>
              <div style={{ fontSize: 14, fontWeight: 600, color: 'var(--text-primary)', marginBottom: 4 }}>
                Pending Reorder Suggestions
              </div>
              <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                Automatically generated for items at or below their reorder point. Approve to create a draft PO line.
              </div>
            </div>
            <div style={{ display: 'flex', gap: 8 }}>
              <button className="btn btn-ghost btn-sm" onClick={loadSuggestions}>↻ Refresh</button>
              <button className="btn btn-ghost btn-sm" onClick={handleGenerateSuggestions} disabled={generatingSuggestions}>
                {generatingSuggestions ? '⟳ Scanning…' : '🔍 Scan Now'}
              </button>
            </div>
          </div>

          {suggestionMsg && (
            <div style={{ marginBottom: 12, padding: '10px 14px', background: 'rgba(34,197,94,0.1)', border: '1px solid rgba(34,197,94,0.3)', borderRadius: 8, fontSize: 13, color: 'var(--text-primary)', display: 'flex', justifyContent: 'space-between' }}>
              {suggestionMsg}
              <button onClick={() => setSuggestionMsg('')} style={{ background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-muted)', fontSize: 16, lineHeight: 1 }}>✕</button>
            </div>
          )}

          {suggestionsLoading ? (
            <div className="po-loading">Loading suggestions…</div>
          ) : suggestions.length === 0 ? (
            <div className="po-empty">
              <div className="po-empty-icon">✅</div>
              <h3>No pending suggestions</h3>
              <p>All stock levels are above their reorder points, or suggestions have already been actioned.</p>
              <button className="btn btn-ghost" onClick={handleGenerateSuggestions} disabled={generatingSuggestions}>
                {generatingSuggestions ? 'Scanning…' : '🔍 Scan Inventory Now'}
              </button>
            </div>
          ) : (
            <div className="po-table-wrap">
              <table className="po-table">
                <thead>
                  <tr>
                    <th>SKU</th>
                    <th>Product</th>
                    <th>Stock</th>
                    <th>Reorder Point</th>
                    <th>Supplier</th>
                    <th>Qty to Order</th>
                    <th>Est. Cost</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {suggestions.map(s => {
                    const qty = editQty[s.suggestion_id] ?? s.suggested_qty;
                    const estCost = qty * (s.unit_cost || 0);
                    return (
                      <tr key={s.suggestion_id} className="po-row">
                        <td><span className="po-number" style={{ fontSize: 12 }}>{s.sku}</span></td>
                        <td style={{ maxWidth: 200 }}>
                          <div style={{ fontSize: 13, color: 'var(--text-primary)', fontWeight: 500 }}>{s.product_name || '—'}</div>
                        </td>
                        <td>
                          <span style={{ color: '#ef4444', fontWeight: 700, fontSize: 13 }}>{s.current_stock}</span>
                        </td>
                        <td style={{ fontSize: 13, color: 'var(--text-muted)' }}>{s.reorder_point}</td>
                        <td style={{ fontSize: 13 }}>
                          {s.supplier_name
                            ? <span>{s.supplier_name}</span>
                            : <span style={{ color: '#f59e0b', fontSize: 12 }}>⚠ No supplier</span>}
                        </td>
                        <td>
                          <input
                            type="number"
                            min={1}
                            value={qty}
                            onChange={e => setEditQty(prev => ({ ...prev, [s.suggestion_id]: Math.max(1, parseInt(e.target.value) || 1) }))}
                            style={{ width: 72, padding: '4px 8px', fontSize: 13, textAlign: 'center', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)' }}
                          />
                        </td>
                        <td style={{ fontSize: 13, color: 'var(--text-muted)' }}>
                          {s.unit_cost ? fmtMoney(estCost, s.currency || 'GBP') : '—'}
                        </td>
                        <td style={{ whiteSpace: 'nowrap' }}>
                          <div style={{ display: 'flex', gap: 6 }}>
                            <button
                              className="btn btn-primary btn-sm"
                              disabled={!s.supplier_id || approvingId === s.suggestion_id}
                              onClick={() => handleApproveSuggestion(s)}
                              title={!s.supplier_id ? 'No supplier configured for this product' : 'Create draft PO line'}
                            >
                              {approvingId === s.suggestion_id ? '⟳' : '✓ Approve'}
                            </button>
                            <button
                              className="btn btn-ghost btn-sm"
                              disabled={dismissingId === s.suggestion_id}
                              onClick={() => handleDismissSuggestion(s.suggestion_id)}
                              style={{ color: 'var(--text-muted)' }}
                            >
                              {dismissingId === s.suggestion_id ? '⟳' : '✕'}
                            </button>
                          </div>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}

      {/* ── Purchase Orders Tab ── */}
      {activeTab === 'orders' && (<>

      <div className="po-filters">
        <div className="po-status-pills">
          <button className={`po-pill ${!statusFilter ? 'po-pill-active' : ''}`} onClick={() => setStatusFilter('')}>
            All <span>{pos.length}</span>
          </button>
          {STATUS_ORDER.filter(s => countsByStatus[s] > 0 || statusFilter === s).map(s => (
            <button
              key={s}
              className={`po-pill ${statusFilter === s ? 'po-pill-active' : ''}`}
              onClick={() => setStatusFilter(s === statusFilter ? '' : s)}
            >
              {STATUS_CONFIG[s]?.label || s}
              {countsByStatus[s] > 0 && <span>{countsByStatus[s]}</span>}
            </button>
          ))}
        </div>

        {suppliers.length > 0 && (
          <select className="po-select po-supplier-filter" value={supplierFilter}
            onChange={e => setSupplierFilter(e.target.value)}>
            <option value="">All Suppliers</option>
            {suppliers.map(s => (
              <option key={s.supplier_id} value={s.supplier_id}>{s.name}</option>
            ))}
          </select>
        )}
      </div>

      {error && <div className="po-error">{error}</div>}

      {/* Suggestions banner — shown on orders tab when pending suggestions exist */}
      {suggestions.length > 0 && (
        <div style={{ marginBottom: 14, padding: '12px 16px', background: 'rgba(251,191,36,0.1)', border: '1px solid rgba(251,191,36,0.4)', borderRadius: 10, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            <span style={{ fontSize: 20 }}>💡</span>
            <div>
              <span style={{ fontWeight: 700, fontSize: 13, color: 'var(--text-primary)' }}>
                {suggestions.length} reorder suggestion{suggestions.length !== 1 ? 's' : ''} pending
              </span>
              <span style={{ fontSize: 12, color: 'var(--text-muted)', marginLeft: 8 }}>
                Items have fallen below their reorder point
              </span>
            </div>
          </div>
          <button className="btn btn-ghost btn-sm" onClick={() => setActiveTab('suggestions')}>
            View Suggestions →
          </button>
        </div>
      )}

      {loading ? (
        <div className="po-loading">Loading purchase orders…</div>
      ) : pos.length === 0 ? (
        <div className="po-empty">
          <div className="po-empty-icon">📋</div>
          <h3>{statusFilter || supplierFilter ? 'No purchase orders match your filters' : 'No purchase orders yet'}</h3>
          <p>
            {statusFilter || supplierFilter
              ? 'Try adjusting your filters.'
              : 'Create your first PO manually, or use Auto-generate to create POs for low-stock products.'}
          </p>
          <div className="po-empty-actions">
            {(statusFilter || supplierFilter) && (
              <button className="btn btn-ghost" onClick={() => { setStatusFilter(''); setSupplierFilter(''); }}>
                Clear Filters
              </button>
            )}
            <button className="btn btn-primary" onClick={() => setShowCreate(true)}>+ Create PO</button>
          </div>
        </div>
      ) : (
        <div className="po-table-wrap">
          <table className="po-table">
            <thead>
              <tr>
                <th>PO Number</th>
                <th>Supplier</th>
                <th>Type</th>
                <th>Status</th>
                <th>Lines</th>
                <th>Total Value</th>
                <th>Expected</th>
                <th>Created</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {pos.map(po => (
                <>
                  <tr
                    key={po.po_id}
                    className={`po-row ${expanded === po.po_id ? 'po-row-expanded' : ''}`}
                    onClick={() => setExpanded(expanded === po.po_id ? null : po.po_id)}
                  >
                    <td><span className="po-number">{po.po_number}</span></td>
                    <td className="po-supplier">{po.supplier_name || supplierName(po.supplier_id)}</td>
                    <td>
                      <span className={`po-type-badge ${po.type === 'dropship' ? 'po-type-dropship' : 'po-type-standard'}`}>
                        {po.type || 'standard'}
                      </span>
                    </td>
                    <td>
                      <span className={`po-status ${STATUS_CONFIG[po.status]?.cls || ''}`}>
                        {STATUS_CONFIG[po.status]?.label || po.status}
                      </span>
                    </td>
                    <td className="po-meta">{po.lines?.length ?? 0} line{(po.lines?.length ?? 0) !== 1 ? 's' : ''}</td>
                    <td>{fmtMoney(po.total_cost, po.currency)}</td>
                    <td className="po-date">{fmt(po.expected_at)}</td>
                    <td className="po-date">{fmt(po.created_at)}</td>
                    <td className="po-chevron">{expanded === po.po_id ? '▲' : '▼'}</td>
                  </tr>

                  {expanded === po.po_id && (
                    <tr key={`${po.po_id}-detail`} className="po-detail-row">
                      <td colSpan={9}>
                        <PODetail po={po} onAction={() => load()} />
                      </td>
                    </tr>
                  )}
                </>
              ))}
            </tbody>
          </table>
        </div>
      )}

      </>) /* end activeTab === 'orders' */}

      {showCreate && (
        <CreatePOModal
          suppliers={suppliers}
          onClose={() => setShowCreate(false)}
          onCreated={() => { setShowCreate(false); load(); }}
        />
      )}
    </div>
  );
}

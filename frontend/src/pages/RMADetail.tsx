import { useState, useEffect, useCallback } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { getActiveTenantId } from '../contexts/TenantContext';
import './RMAs.css';

const API_BASE = import.meta.env.VITE_API_URL ?? 'http://localhost:8080/api/v1';

function api(path: string, init?: RequestInit) {
  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      'X-Tenant-Id': getActiveTenantId(),
      ...(init?.headers || {}),
    },
  });
}

// ─── Types ─────────────────────────────────────────────────────────────────────

interface RMALine {
  line_id: string;
  product_id?: string;
  product_name: string;
  sku: string;
  qty_requested: number;
  qty_received: number;
  reason_code?: string;
  reason_detail?: string;
  condition?: string;
  disposition?: string;
  restock_location_id?: string;
  restock_qty?: number;
  pending_restock_qty?: number;
  restocked?: boolean;
}

interface RMAEvent {
  event_id: string;
  status: string;
  note?: string;
  created_by: string;
  created_at: string;
}

interface RMA {
  rma_id: string;
  rma_number: string;
  order_id: string;
  order_number: string;
  channel: string;
  channel_account_id?: string;
  marketplace_rma_id?: string;
  status: string;
  customer: { name: string; email?: string; address?: string };
  lines: RMALine[];
  refund_action?: string;
  refund_amount?: number;
  refund_currency?: string;
  refund_reference?: string;
  refund_issued_at?: string;
  refund_push_status?: string;       // pushed | failed | undefined
  refund_push_reference?: string;
  refund_push_succeeded_at?: string;
  refund_push_error?: string;
  // S2-Task4: channel refund submission tracking
  channel_refund_submitted?: boolean;
  channel_refund_id?: string;
  tracking_number?: string;
  label_url?: string;
  notes?: string;
  timeline?: RMAEvent[];
  created_by: string;
  created_at: string;
  updated_at: string;
  resolved_at?: string;
}

// ─── Constants ────────────────────────────────────────────────────────────────

const STATUS_CONFIG: Record<string, { label: string; color: string; bg: string }> = {
  requested:       { label: 'Requested',       color: '#fbbf24', bg: 'rgba(251,191,36,0.12)' },
  authorised:      { label: 'Authorised',      color: '#60a5fa', bg: 'rgba(96,165,250,0.12)' },
  awaiting_return: { label: 'Awaiting Return', color: '#c084fc', bg: 'rgba(192,132,252,0.12)' },
  received:        { label: 'Received',        color: '#fb923c', bg: 'rgba(251,146,60,0.12)' },
  inspected:       { label: 'Inspected',       color: '#2dd4bf', bg: 'rgba(45,212,191,0.12)' },
  resolved:        { label: 'Resolved',        color: '#4ade80', bg: 'rgba(74,222,128,0.12)' },
};

const CHANNEL_ICONS: Record<string, string> = { amazon: '📦', ebay: '🛒', temu: '🏷️', manual: '📝' };

const CONDITIONS = ['resaleable', 'damaged', 'missing', 'wrong_item'];
const DISPOSITIONS = ['restock', 'quarantine', 'write_off', 'return_to_supplier'];
const REFUND_ACTIONS = ['full_refund', 'partial_refund', 'exchange', 'credit_note', 'none'];

const REASON_LABELS: Record<string, string> = {
  not_as_described: 'Not as described', damaged: 'Damaged', changed_mind: 'Changed mind',
  wrong_item: 'Wrong item', defective: 'Defective', other: 'Other',
};

// ─── Sub-components ───────────────────────────────────────────────────────────

function StatusBadge({ status }: { status: string }) {
  const sc = STATUS_CONFIG[status] || { label: status, color: '#94a3b8', bg: 'rgba(148,163,184,0.1)' };
  return <span className="rma-status-badge" style={{ color: sc.color, background: sc.bg }}>{sc.label}</span>;
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="rma-detail-section">
      <div className="rma-detail-section-title">{title}</div>
      {children}
    </div>
  );
}

// ─── Action panels ────────────────────────────────────────────────────────────

function AuthorisePanel({ rmaId, onDone }: { rmaId: string; onDone: () => void }) {
  const [saving, setSaving] = useState(false);
  const authorise = async () => {
    setSaving(true);
    await api(`/rmas/${rmaId}/authorise`, { method: 'POST', body: JSON.stringify({}) });
    setSaving(false); onDone();
  };
  return (
    <div className="rma-action-panel">
      <div className="rma-action-title">Authorise Return</div>
      <p className="rma-action-desc">Confirm this return request is accepted. The customer will be awaiting your return instructions.</p>
      <div className="rma-action-btns">
        <button className="rma-btn-primary" onClick={authorise} disabled={saving}>{saving ? 'Saving…' : '✓ Authorise Return'}</button>
      </div>
    </div>
  );
}

function ReceivePanel({ rma, onDone }: { rma: RMA; onDone: () => void }) {
  const [qtys, setQtys] = useState<Record<string, number>>(() =>
    Object.fromEntries(rma.lines.map(l => [l.line_id, l.qty_requested]))
  );
  const [saving, setSaving] = useState(false);

  const save = async () => {
    setSaving(true);
    await api(`/rmas/${rma.rma_id}/receive`, {
      method: 'POST',
      body: JSON.stringify({ lines: Object.entries(qtys).map(([line_id, qty_received]) => ({ line_id, qty_received })) }),
    });
    setSaving(false); onDone();
  };

  return (
    <div className="rma-action-panel">
      <div className="rma-action-title">Record Receipt</div>
      <p className="rma-action-desc">Enter the quantity actually received for each line.</p>
      <table className="rma-action-table">
        <thead><tr><th>SKU</th><th>Product</th><th>Expected</th><th>Received</th></tr></thead>
        <tbody>
          {rma.lines.map(line => (
            <tr key={line.line_id}>
              <td><code>{line.sku}</code></td>
              <td>{line.product_name}</td>
              <td>{line.qty_requested}</td>
              <td>
                <input
                  type="number" min={0} max={line.qty_requested}
                  className="rma-qty-input"
                  value={qtys[line.line_id] ?? line.qty_requested}
                  onChange={e => setQtys(p => ({ ...p, [line.line_id]: parseInt(e.target.value) || 0 }))}
                />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      <div className="rma-action-btns">
        <button className="rma-btn-primary" onClick={save} disabled={saving}>{saving ? 'Saving…' : '✓ Confirm Receipt'}</button>
      </div>
    </div>
  );
}

function InspectPanel({ rma, onDone }: { rma: RMA; onDone: () => void }) {
  const [lineData, setLineData] = useState<Record<string, { condition: string; disposition: string; restock_qty: number }>>(() =>
    Object.fromEntries(rma.lines.map(l => [l.line_id, { condition: 'resaleable', disposition: 'restock', restock_qty: l.qty_received || l.qty_requested }]))
  );
  const [saving, setSaving] = useState(false);

  const update = (lineId: string, field: string, value: any) =>
    setLineData(p => ({ ...p, [lineId]: { ...p[lineId], [field]: value } }));

  const save = async () => {
    setSaving(true);
    await api(`/rmas/${rma.rma_id}/inspect`, {
      method: 'POST',
      body: JSON.stringify({
        lines: rma.lines.map(l => ({
          line_id: l.line_id,
          ...(lineData[l.line_id] || {}),
        })),
      }),
    });
    setSaving(false); onDone();
  };

  return (
    <div className="rma-action-panel">
      <div className="rma-action-title">Inspect Items</div>
      <p className="rma-action-desc">Set the condition and disposition for each returned item.</p>
      <div className="rma-inspect-lines">
        {rma.lines.map(line => (
          <div key={line.line_id} className="rma-inspect-line">
            <div className="rma-inspect-line-header">
              <code>{line.sku}</code> — {line.product_name}
              <span className="rma-inspect-qty">Qty received: {line.qty_received}</span>
            </div>
            <div className="rma-inspect-fields">
              <div className="rma-inspect-field">
                <label>Condition</label>
                <select className="rma-select" value={lineData[line.line_id]?.condition} onChange={e => update(line.line_id, 'condition', e.target.value)}>
                  {CONDITIONS.map(c => <option key={c} value={c}>{c.replace(/_/g, ' ')}</option>)}
                </select>
              </div>
              <div className="rma-inspect-field">
                <label>Disposition</label>
                <select className="rma-select" value={lineData[line.line_id]?.disposition} onChange={e => update(line.line_id, 'disposition', e.target.value)}>
                  {DISPOSITIONS.map(d => <option key={d} value={d}>{d.replace(/_/g, ' ')}</option>)}
                </select>
              </div>
              <div className="rma-inspect-field">
                <label>Restock qty</label>
                <input type="number" min={0} className="rma-qty-input" value={lineData[line.line_id]?.restock_qty}
                  onChange={e => update(line.line_id, 'restock_qty', parseInt(e.target.value) || 0)} />
              </div>
            </div>
          </div>
        ))}
      </div>
      <div className="rma-action-btns">
        <button className="rma-btn-primary" onClick={save} disabled={saving}>{saving ? 'Saving…' : '✓ Save Inspection'}</button>
      </div>
    </div>
  );
}

// ─── Product search result type ───────────────────────────────────────────────
interface ProductSearchResult {
  product_id: string;
  title: string;
  sku: string;
}

function ResolvePanel({ rma, onDone }: { rma: RMA; onDone: () => void }) {
  const [refundAction, setRefundAction] = useState('full_refund');
  const [refundAmount, setRefundAmount] = useState('');
  const [refundRef, setRefundRef] = useState('');
  const [notes, setNotes] = useState('');
  const [saving, setSaving] = useState(false);
  const [exchangeResult, setExchangeResult] = useState<{ order_id: string; warning?: string } | null>(null);

  // Exchange-specific state
  const [productQuery, setProductQuery] = useState('');
  const [productResults, setProductResults] = useState<ProductSearchResult[]>([]);
  const [productSearching, setProductSearching] = useState(false);
  const [selectedProduct, setSelectedProduct] = useState<ProductSearchResult | null>(null);
  const [exchangeQty, setExchangeQty] = useState(1);
  const [shipName, setShipName] = useState(rma.customer?.name || '');
  const [shipAddress, setShipAddress] = useState(rma.customer?.address || '');

  // Debounced product search
  const searchProducts = async (q: string) => {
    if (!q.trim()) { setProductResults([]); return; }
    setProductSearching(true);
    try {
      const res = await api(`/products?q=${encodeURIComponent(q)}&limit=8`);
      if (res.ok) {
        const d = await res.json();
        setProductResults((d.products || []).map((p: { product_id: string; title: string; sku: string }) => ({
          product_id: p.product_id,
          title: p.title,
          sku: p.sku,
        })));
      }
    } finally {
      setProductSearching(false);
    }
  };

  const save = async () => {
    setSaving(true);
    setExchangeResult(null);
    try {
      if (refundAction === 'exchange') {
        if (!selectedProduct) { setSaving(false); return; }
        const res = await api(`/rmas/${rma.rma_id}/exchange`, {
          method: 'POST',
          body: JSON.stringify({
            replacement_product_id: selectedProduct.product_id,
            replacement_product_name: selectedProduct.title,
            replacement_sku: selectedProduct.sku,
            replacement_qty: exchangeQty,
            notes,
            shipping_name: shipName,
            shipping_address: shipAddress,
          }),
        });
        const d = await res.json();
        if (res.ok) {
          setExchangeResult({ order_id: d.exchange_order_id, warning: d.warning });
          onDone();
        }
      } else {
        await api(`/rmas/${rma.rma_id}/resolve`, {
          method: 'POST',
          body: JSON.stringify({
            refund_action: refundAction,
            refund_amount: parseFloat(refundAmount) || 0,
            refund_currency: 'GBP',
            refund_reference: refundRef,
            notes,
          }),
        });
        onDone();
      }
    } finally {
      setSaving(false);
    }
  };

  const isExchange = refundAction === 'exchange';

  return (
    <div className="rma-action-panel">
      <div className="rma-action-title">Resolve RMA</div>
      <div className="rma-resolve-fields">
        <div className="rma-field">
          <label>Resolution action</label>
          <select className="rma-select" value={refundAction} onChange={e => setRefundAction(e.target.value)}>
            {REFUND_ACTIONS.map(a => <option key={a} value={a}>{a.replace(/_/g, ' ')}</option>)}
          </select>
        </div>

        {/* Standard refund fields */}
        {!isExchange && refundAction !== 'none' && (
          <div className="rma-field">
            <label>Refund amount (GBP)</label>
            <input className="rma-input" type="number" min={0} step={0.01} placeholder="0.00" value={refundAmount} onChange={e => setRefundAmount(e.target.value)} />
          </div>
        )}
        {!isExchange && (
          <div className="rma-field">
            <label>Reference / transaction ID</label>
            <input className="rma-input" placeholder="Marketplace refund ID or internal ref…" value={refundRef} onChange={e => setRefundRef(e.target.value)} />
          </div>
        )}

        {/* Exchange-specific fields */}
        {isExchange && (
          <div className="rma-exchange-section">
            <div className="rma-exchange-heading">Replacement Item</div>
            <div className="rma-field">
              <label>Search products</label>
              <div className="rma-product-search-wrap">
                <input
                  className="rma-input"
                  placeholder="Type SKU or product name…"
                  value={productQuery}
                  onChange={e => { setProductQuery(e.target.value); searchProducts(e.target.value); }}
                  autoComplete="off"
                />
                {productSearching && <span className="rma-search-spinner">…</span>}
              </div>
              {productResults.length > 0 && !selectedProduct && (
                <div className="rma-product-results">
                  {productResults.map(p => (
                    <div
                      key={p.product_id}
                      className="rma-product-result-row"
                      onClick={() => { setSelectedProduct(p); setProductQuery(p.title); setProductResults([]); }}
                    >
                      <code className="rma-sku">{p.sku}</code>
                      <span className="rma-product-title">{p.title}</span>
                    </div>
                  ))}
                </div>
              )}
              {selectedProduct && (
                <div className="rma-selected-product">
                  <span className="rma-selected-product-label">
                    <code className="rma-sku">{selectedProduct.sku}</code> {selectedProduct.title}
                  </span>
                  <button className="rma-clear-product" onClick={() => { setSelectedProduct(null); setProductQuery(''); }}>✕</button>
                </div>
              )}
            </div>
            <div className="rma-field">
              <label>Quantity to send</label>
              <input className="rma-input rma-input--short" type="number" min={1} value={exchangeQty} onChange={e => setExchangeQty(parseInt(e.target.value) || 1)} />
            </div>
            <div className="rma-exchange-heading" style={{ marginTop: 12 }}>Delivery Details</div>
            <div className="rma-field">
              <label>Recipient name</label>
              <input className="rma-input" value={shipName} onChange={e => setShipName(e.target.value)} />
            </div>
            <div className="rma-field">
              <label>Delivery address</label>
              <textarea className="rma-textarea" rows={2} value={shipAddress} onChange={e => setShipAddress(e.target.value)} />
            </div>
          </div>
        )}

        <div className="rma-field">
          <label>Resolution notes</label>
          <textarea className="rma-textarea" rows={2} value={notes} onChange={e => setNotes(e.target.value)} />
        </div>
      </div>
      <div className="rma-action-btns">
        <button
          className="rma-btn-primary"
          onClick={save}
          disabled={saving || (isExchange && !selectedProduct)}
        >
          {saving ? 'Saving…' : isExchange ? '✓ Resolve & Create Exchange Order' : '✓ Resolve & Close RMA'}
        </button>
      </div>
      {exchangeResult?.warning && (
        <div className="rma-exchange-warning">⚠ {exchangeResult.warning}</div>
      )}
    </div>
  );
}

// ─── Push Refund Panel ────────────────────────────────────────────────────────

const PUSH_SUPPORTED_CHANNELS = ['amazon', 'ebay', 'shopify'];

function PushRefundPanel({ rma, onDone }: { rma: RMA; onDone: () => void }) {
  const [pushing, setPushing] = useState(false);
  const [result, setResult] = useState<{ ok: boolean; message: string; reference?: string } | null>(null);

  const alreadyPushed = rma.refund_push_status === 'pushed';
  const pushFailed = rma.refund_push_status === 'failed';

  const handlePush = async () => {
    if (!confirm(`Push refund of ${rma.refund_currency || 'GBP'} ${rma.refund_amount?.toFixed(2) || '0.00'} to ${rma.channel}?`)) return;
    setPushing(true);
    setResult(null);
    try {
      const res = await api(`/rmas/${rma.rma_id}/push-refund`, { method: 'POST' });
      const data = await res.json();
      if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`);
      setResult({ ok: true, message: data.message, reference: data.reference });
      onDone();
    } catch (e: any) {
      setResult({ ok: false, message: e.message || 'Push failed' });
    } finally {
      setPushing(false);
    }
  };

  return (
    <div style={{ marginTop: 20, padding: 16, borderRadius: 10, border: `1px solid ${alreadyPushed ? 'rgba(34,197,94,0.3)' : pushFailed ? 'rgba(239,68,68,0.3)' : 'var(--border)'}`, background: alreadyPushed ? 'rgba(34,197,94,0.05)' : pushFailed ? 'rgba(239,68,68,0.05)' : 'var(--bg-elevated)' }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 16 }}>
        <div>
          <div style={{ fontWeight: 700, fontSize: 14, marginBottom: 4 }}>
            {alreadyPushed ? '✅ Refund Pushed to Channel' : pushFailed ? '⚠️ Push Failed — Retry?' : `📤 Push Refund to ${rma.channel.charAt(0).toUpperCase() + rma.channel.slice(1)}`}
          </div>
          <div style={{ fontSize: 12, color: 'var(--text-muted)', lineHeight: 1.5 }}>
            {alreadyPushed
              ? `Successfully pushed ${rma.refund_push_succeeded_at ? new Date(rma.refund_push_succeeded_at).toLocaleString() : ''}. Ref: ${rma.refund_push_reference || '—'}`
              : pushFailed
              ? `Last error: ${rma.refund_push_error || 'Unknown error'}`
              : `Send the refund decision back to ${rma.channel} via their API. The customer will be notified automatically.`
            }
          </div>
        </div>
        {!alreadyPushed && (
          <button
            onClick={handlePush}
            disabled={pushing}
            style={{
              flexShrink: 0, padding: '9px 20px', borderRadius: 8, border: 'none', cursor: pushing ? 'not-allowed' : 'pointer',
              background: pushFailed ? '#ef4444' : 'var(--primary)', color: '#fff', fontWeight: 700, fontSize: 13,
              opacity: pushing ? 0.7 : 1, whiteSpace: 'nowrap',
            }}
          >
            {pushing ? '⏳ Pushing…' : pushFailed ? '🔄 Retry Push' : '📤 Push to Channel'}
          </button>
        )}
      </div>

      {result && (
        <div style={{ marginTop: 12, padding: '10px 14px', borderRadius: 8, background: result.ok ? 'rgba(34,197,94,0.1)' : 'rgba(239,68,68,0.1)', fontSize: 13, color: result.ok ? '#16a34a' : '#ef4444' }}>
          {result.ok ? `✅ ${result.message}${result.reference ? ` (Ref: ${result.reference})` : ''}` : `❌ ${result.message}`}
        </div>
      )}
    </div>
  );
}

// S2-Task4: Manual channel refund submission (for non-API channels)
function ManualChannelRefundPanel({ rma, onDone }: { rma: RMA; onDone: () => void }) {
  const [refId, setRefId] = useState(rma.channel_refund_id || '');
  const [saving, setSaving] = useState(false);

  if (rma.channel_refund_submitted) {
    return (
      <div style={{ marginTop: 16, padding: '12px 16px', borderRadius: 8, background: 'rgba(34,197,94,0.07)', border: '1px solid rgba(34,197,94,0.25)', fontSize: 13, display: 'flex', alignItems: 'center', gap: 12 }}>
        <span>✅ <strong>Refund submitted to channel</strong></span>
        {rma.channel_refund_id && <span style={{ color: 'var(--text-muted)' }}>Ref: <code>{rma.channel_refund_id}</code></span>}
      </div>
    );
  }

  const submit = async () => {
    setSaving(true);
    try {
      const res = await api(`/rmas/${rma.rma_id}`, {
        method: 'PUT',
        body: JSON.stringify({ channel_refund_submitted: true, channel_refund_id: refId.trim() || undefined }),
      });
      if (!res.ok) throw new Error('Update failed');
      onDone();
    } catch { alert('Failed to mark as submitted'); }
    finally { setSaving(false); }
  };

  return (
    <div style={{ marginTop: 16, padding: '14px 16px', borderRadius: 10, border: '1px solid var(--border)', background: 'var(--bg-elevated)' }}>
      <div style={{ fontWeight: 700, fontSize: 13, marginBottom: 10 }}>💳 Submit Refund to Channel</div>
      <div style={{ display: 'flex', gap: 10, alignItems: 'center' }}>
        <input
          style={{ flex: 1, padding: '8px 12px', borderRadius: 8, border: '1px solid var(--border)', background: 'var(--bg-tertiary)', color: 'var(--text-primary)', fontSize: 13 }}
          placeholder="Reference number (optional)"
          value={refId}
          onChange={e => setRefId(e.target.value)}
        />
        <button
          onClick={submit}
          disabled={saving}
          style={{ padding: '8px 18px', background: 'var(--primary)', color: '#fff', border: 'none', borderRadius: 8, cursor: 'pointer', fontWeight: 700, fontSize: 13, whiteSpace: 'nowrap', opacity: saving ? 0.7 : 1 }}
        >
          {saving ? 'Saving…' : '✓ Mark as Submitted'}
        </button>
      </div>
    </div>
  );
}

// ─── Main Detail Page ─────────────────────────────────────────────────────────

export default function RMADetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [rma, setRMA] = useState<RMA | null>(null);
  const [loading, setLoading] = useState(true);
  const [inventoryMode, setInventoryMode] = useState('simple');

  const load = useCallback(async () => {
    if (!id) return;
    setLoading(true);
    try {
      const [rmaRes, cfgRes] = await Promise.all([
        api(`/rmas/${id}`),
        api('/rmas/config'),
      ]);
      const rmaData = await rmaRes.json();
      const cfgData = await cfgRes.json();
      if (rmaRes.ok) setRMA(rmaData.rma);
      if (cfgRes.ok) setInventoryMode(cfgData.config?.inventory_mode || 'simple');
    } finally { setLoading(false); }
  }, [id]);

  useEffect(() => { load(); }, [load]);

  const restock = async (lineId: string) => {
    await api(`/rmas/${id}/restock/${lineId}`, { method: 'POST', body: JSON.stringify({}) });
    load();
  };

  if (loading) return <div className="rma-page rma-loading">Loading…</div>;
  if (!rma) return <div className="rma-page rma-loading">RMA not found</div>;

  const sc = STATUS_CONFIG[rma.status] || { label: rma.status, color: '#94a3b8', bg: 'rgba(148,163,184,0.1)' };

  return (
    <div className="rma-page rma-detail-page">
      {/* Breadcrumb */}
      <div className="rma-breadcrumb">
        <button className="rma-back" onClick={() => navigate('/rmas')}>← Returns</button>
        <span className="rma-breadcrumb-sep">/</span>
        <span>{rma.rma_number}</span>
      </div>

      {/* Header */}
      <div className="rma-detail-header">
        <div className="rma-detail-header-left">
          <h1>{rma.rma_number}</h1>
          <StatusBadge status={rma.status} />
          <span className="rma-channel-badge">{CHANNEL_ICONS[rma.channel]} {rma.channel}</span>
        </div>
        <div className="rma-detail-header-right">
          <span className="rma-created-date">Created {new Date(rma.created_at).toLocaleDateString('en-GB', { day: 'numeric', month: 'short', year: 'numeric' })}</span>
        </div>
      </div>

      <div className="rma-detail-body">
        <div className="rma-detail-main">

          {/* Customer */}
          <Section title="Customer">
            <div className="rma-customer-grid">
              <div><span className="rma-field-label">Name</span><span>{rma.customer?.name || '—'}</span></div>
              <div><span className="rma-field-label">Email</span><span>{rma.customer?.email || '—'}</span></div>
              {rma.order_number && <div><span className="rma-field-label">Order</span><span>#{rma.order_number}</span></div>}
              {rma.customer?.address && <div className="rma-full-row"><span className="rma-field-label">Return address</span><span>{rma.customer.address}</span></div>}
              {rma.tracking_number && <div><span className="rma-field-label">Tracking</span><span className="rma-mono">{rma.tracking_number}</span></div>}
            </div>
          </Section>

          {/* Lines */}
          <Section title="Return Lines">
            <table className="rma-lines-table">
              <thead>
                <tr>
                  <th>SKU</th>
                  <th>Product</th>
                  <th>Reason</th>
                  <th>Requested</th>
                  {['received','inspected','resolved'].includes(rma.status) && <th>Received</th>}
                  {['inspected','resolved'].includes(rma.status) && <><th>Condition</th><th>Disposition</th></>}
                  {inventoryMode === 'simple' && rma.status === 'inspected' && <th>Restock</th>}
                </tr>
              </thead>
              <tbody>
                {(rma.lines || []).map(line => (
                  <tr key={line.line_id}>
                    <td><code className="rma-sku">{line.sku}</code></td>
                    <td>{line.product_name}</td>
                    <td>
                      <span className="rma-reason-badge">{REASON_LABELS[line.reason_code || ''] || line.reason_code || '—'}</span>
                      {line.reason_detail && <div className="rma-reason-detail">"{line.reason_detail}"</div>}
                    </td>
                    <td className="rma-center">{line.qty_requested}</td>
                    {['received','inspected','resolved'].includes(rma.status) && (
                      <td className="rma-center">{line.qty_received}</td>
                    )}
                    {['inspected','resolved'].includes(rma.status) && (
                      <>
                        <td>
                          {line.condition ? (
                            <span className={`rma-condition-badge rma-condition-${line.condition}`}>
                              {line.condition.replace(/_/g, ' ')}
                            </span>
                          ) : '—'}
                        </td>
                        <td>
                          {line.disposition ? (
                            <span className={`rma-disposition-badge rma-disp-${line.disposition}`}>
                              {line.disposition.replace(/_/g, ' ')}
                            </span>
                          ) : '—'}
                        </td>
                      </>
                    )}
                    {inventoryMode === 'simple' && rma.status === 'inspected' && (
                      <td>
                        {line.restocked ? (
                          <span className="rma-restocked-tag">✓ Restocked</span>
                        ) : line.pending_restock_qty ? (
                          <button className="rma-btn-sm-primary" onClick={() => restock(line.line_id)}>
                            Restock {line.pending_restock_qty}
                          </button>
                        ) : '—'}
                      </td>
                    )}
                  </tr>
                ))}
              </tbody>
            </table>
          </Section>

          {/* Action panel — contextual by status */}
          {rma.status === 'requested' && (
            <AuthorisePanel rmaId={rma.rma_id} onDone={load} />
          )}
          {rma.status === 'authorised' && (
            <ReceivePanel rma={rma} onDone={load} />
          )}
          {rma.status === 'received' && (
            <InspectPanel rma={rma} onDone={load} />
          )}
          {rma.status === 'inspected' && (
            <ResolvePanel rma={rma} onDone={load} />
          )}

          {/* Resolution summary */}
          {rma.status === 'resolved' && (
            <Section title="Resolution">
              <div className="rma-resolution-grid">
                <div><span className="rma-field-label">Action</span><span>{rma.refund_action?.replace(/_/g, ' ') || '—'}</span></div>
                {(rma.refund_amount || 0) > 0 && (
                  <div><span className="rma-field-label">Amount</span><span className="rma-refund-amount">{rma.refund_currency || 'GBP'} {rma.refund_amount?.toFixed(2)}</span></div>
                )}
                {rma.refund_reference && (
                  <div>
                    <span className="rma-field-label">Reference</span>
                    <span className="rma-mono">{rma.refund_reference}</span>
                  </div>
                )}
                {rma.resolved_at && <div><span className="rma-field-label">Resolved</span><span>{new Date(rma.resolved_at).toLocaleString()}</span></div>}
              </div>

              {/* Push Refund to Channel */}
              {['amazon', 'ebay', 'shopify'].includes(rma.channel) && rma.refund_action && rma.refund_action !== 'none' && (
                <PushRefundPanel rma={rma} onDone={load} />
              )}
              {/* S2-Task4: Manual refund submission for non-API channels */}
              {!['amazon', 'ebay', 'shopify'].includes(rma.channel) && rma.refund_action && rma.refund_action !== 'none' && (
                <ManualChannelRefundPanel rma={rma} onDone={load} />
              )}
            </Section>
          )}

          {/* Notes */}
          {rma.notes && (
            <Section title="Notes">
              <p className="rma-notes-text">{rma.notes}</p>
            </Section>
          )}
        </div>

        {/* Sidebar: timeline */}
        <div className="rma-detail-sidebar">
          <Section title="Activity">
            <div className="rma-timeline">
              {(rma.timeline || []).slice().reverse().map(ev => {
                const sc = STATUS_CONFIG[ev.status] || { color: '#94a3b8' };
                return (
                  <div key={ev.event_id} className="rma-timeline-item">
                    <div className="rma-tl-dot" style={{ background: sc.color }} />
                    <div className="rma-tl-body">
                      <div className="rma-tl-status" style={{ color: sc.color }}>{STATUS_CONFIG[ev.status]?.label || ev.status}</div>
                      {ev.note && <div className="rma-tl-note">{ev.note}</div>}
                      <div className="rma-tl-meta">
                        {ev.created_by} · {new Date(ev.created_at).toLocaleString()}
                      </div>
                    </div>
                  </div>
                );
              })}
              {(!rma.timeline || rma.timeline.length === 0) && (
                <div className="rma-tl-empty">No activity yet</div>
              )}
            </div>
          </Section>
        </div>
      </div>
    </div>
  );
}

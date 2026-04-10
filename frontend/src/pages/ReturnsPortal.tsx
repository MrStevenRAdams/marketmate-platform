import { useState, useEffect } from 'react';
import { useParams } from 'react-router-dom';

// ─── Types ────────────────────────────────────────────────────────────────────

interface PortalConfig {
  tenant_id: string;
  company_name: string;
  enabled: boolean;
  policy_text?: string;
  window_days: number;
  require_reason: boolean;
  allow_exchange: boolean;
}

interface PortalOrderLine {
  line_id: string;
  product_name: string;
  sku: string;
  quantity: number;
}

interface PortalOrder {
  order_id: string;
  order_number: string;
  channel: string;
  order_date: string;
  lines: PortalOrderLine[];
}

interface ReturnLine {
  line_id: string;
  product_name: string;
  sku: string;
  qty_requested: number;
  reason_code: string;
  reason_detail: string;
  selected: boolean;
}

type Step = 'lookup' | 'select' | 'confirm' | 'done' | 'status';

const API_BASE =
  (import.meta as any).env?.VITE_API_URL ||
  'https://marketmate-api-487246736287.us-central1.run.app/api/v1';

const REASON_CODES = [
  { value: 'not_as_described', label: 'Not as described' },
  { value: 'damaged',          label: 'Arrived damaged' },
  { value: 'wrong_item',       label: 'Wrong item sent' },
  { value: 'defective',        label: 'Faulty / not working' },
  { value: 'changed_mind',     label: 'Changed my mind' },
  { value: 'other',            label: 'Other' },
];

// ─── Status step badge ────────────────────────────────────────────────────────

const STATUS_META: Record<string, { icon: string; color: string; label: string }> = {
  requested:       { icon: '📋', color: '#6366f1', label: 'Requested' },
  authorised:      { icon: '✅', color: '#10b981', label: 'Authorised' },
  awaiting_return: { icon: '📦', color: '#f59e0b', label: 'Awaiting return' },
  received:        { icon: '🔍', color: '#3b82f6', label: 'Received' },
  inspected:       { icon: '🔬', color: '#8b5cf6', label: 'Inspected' },
  resolved:        { icon: '🎉', color: '#10b981', label: 'Resolved' },
};

// ─── Component ────────────────────────────────────────────────────────────────

export default function ReturnsPortal() {
  const { tenantId } = useParams<{ tenantId: string }>();

  const [config, setConfig] = useState<PortalConfig | null>(null);
  const [configError, setConfigError] = useState('');
  const [step, setStep] = useState<Step>('lookup');

  // Lookup form
  const [orderNumber, setOrderNumber] = useState('');
  const [postcode, setPostcode] = useState('');
  const [customerName, setCustomerName] = useState('');
  const [customerEmail, setCustomerEmail] = useState('');
  const [lookupLoading, setLookupLoading] = useState(false);
  const [lookupError, setLookupError] = useState('');

  // Order & return lines
  const [order, setOrder] = useState<PortalOrder | null>(null);
  const [returnLines, setReturnLines] = useState<ReturnLine[]>([]);
  const [submitLoading, setSubmitLoading] = useState(false);
  const [submitError, setSubmitError] = useState('');

  // Done state
  const [rmaNumber, setRmaNumber] = useState('');
  const [rmaMessage, setRmaMessage] = useState('');

  // Status check
  const [statusRmaNumber, setStatusRmaNumber] = useState('');
  const [statusResult, setStatusResult] = useState<any>(null);
  const [statusLoading, setStatusLoading] = useState(false);
  const [statusError, setStatusError] = useState('');

  // ── Load config ─────────────────────────────────────────────────────────
  useEffect(() => {
    if (!tenantId) return;
    fetch(`${API_BASE}/public/returns/config/${tenantId}`)
      .then(r => r.json())
      .then(d => {
        if (d.error) setConfigError(d.error);
        else setConfig(d.config);
      })
      .catch(() => setConfigError('Unable to load the returns portal. Please try again later.'));
  }, [tenantId]);

  // ── Lookup order ─────────────────────────────────────────────────────────
  const handleLookup = async () => {
    setLookupError('');
    if (!orderNumber.trim() || !postcode.trim()) {
      setLookupError('Please enter both your order number and postcode.');
      return;
    }
    setLookupLoading(true);
    try {
      const res = await fetch(`${API_BASE}/public/returns/lookup`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ tenant_id: tenantId, order_number: orderNumber.trim(), postcode: postcode.trim() }),
      });
      const data = await res.json();
      if (!res.ok) { setLookupError(data.error || 'Order not found.'); return; }
      setOrder(data.order);
      setReturnLines(data.order.lines.map((l: PortalOrderLine) => ({
        ...l,
        qty_requested: 1,
        reason_code: '',
        reason_detail: '',
        selected: false,
      })));
      setStep('select');
    } catch {
      setLookupError('Something went wrong. Please try again.');
    } finally {
      setLookupLoading(false);
    }
  };

  // ── Submit return ────────────────────────────────────────────────────────
  const handleSubmit = async () => {
    const selectedLines = returnLines.filter(l => l.selected);
    if (selectedLines.length === 0) { setSubmitError('Please select at least one item to return.'); return; }
    if (config?.require_reason && selectedLines.some(l => !l.reason_code)) {
      setSubmitError('Please select a reason for each item you wish to return.'); return;
    }
    setSubmitError('');
    setSubmitLoading(true);
    try {
      const res = await fetch(`${API_BASE}/public/returns/submit`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          tenant_id: tenantId,
          order_id: order?.order_id,
          order_number: order?.order_number,
          customer_name: customerName,
          customer_email: customerEmail,
          lines: selectedLines.map(l => ({
            line_id: l.line_id,
            product_name: l.product_name,
            sku: l.sku,
            qty_requested: l.qty_requested,
            reason_code: l.reason_code,
            reason_detail: l.reason_detail,
          })),
        }),
      });
      const data = await res.json();
      if (!res.ok) { setSubmitError(data.error || 'Submission failed. Please try again.'); return; }
      setRmaNumber(data.rma_number);
      setRmaMessage(data.message);
      setStep('done');
    } catch {
      setSubmitError('Something went wrong. Please try again.');
    } finally {
      setSubmitLoading(false);
    }
  };

  // ── Check RMA status ─────────────────────────────────────────────────────
  const handleStatusCheck = async () => {
    if (!statusRmaNumber.trim()) return;
    setStatusLoading(true);
    setStatusError('');
    setStatusResult(null);
    try {
      const res = await fetch(`${API_BASE}/public/returns/rma/${statusRmaNumber.trim()}?tenant_id=${tenantId}`);
      const data = await res.json();
      if (!res.ok) { setStatusError(data.error || 'Return not found.'); return; }
      setStatusResult(data);
    } catch {
      setStatusError('Something went wrong. Please try again.');
    } finally {
      setStatusLoading(false);
    }
  };

  // ─── Render ──────────────────────────────────────────────────────────────

  return (
    <div style={{
      minHeight: '100vh',
      background: '#f8f7f4',
      fontFamily: "'DM Sans', 'Helvetica Neue', sans-serif",
      color: '#1a1a18',
    }}>
      <style>{`
        @import url('https://fonts.googleapis.com/css2?family=DM+Sans:wght@400;500;600&family=DM+Serif+Display&display=swap');

        .rp-card {
          background: #fff;
          border: 1px solid #e8e5e0;
          border-radius: 16px;
          padding: 40px;
          max-width: 560px;
          margin: 0 auto;
          box-shadow: 0 2px 20px rgba(0,0,0,0.06);
        }
        .rp-input {
          width: 100%;
          padding: 12px 16px;
          border: 1.5px solid #ddd8d0;
          border-radius: 10px;
          font-size: 15px;
          font-family: inherit;
          background: #fafaf8;
          color: #1a1a18;
          transition: border-color 0.15s;
          box-sizing: border-box;
        }
        .rp-input:focus {
          outline: none;
          border-color: #c8a96e;
          background: #fff;
        }
        .rp-btn {
          padding: 13px 28px;
          border-radius: 10px;
          border: none;
          font-size: 15px;
          font-family: inherit;
          font-weight: 600;
          cursor: pointer;
          transition: all 0.15s;
        }
        .rp-btn-primary {
          background: #1a1a18;
          color: #fff;
          width: 100%;
        }
        .rp-btn-primary:hover:not(:disabled) { background: #2d2d2a; transform: translateY(-1px); }
        .rp-btn-primary:disabled { opacity: 0.5; cursor: default; }
        .rp-btn-ghost {
          background: transparent;
          color: #888;
          border: 1.5px solid #ddd8d0;
          font-size: 13px;
          padding: 8px 16px;
        }
        .rp-btn-ghost:hover { border-color: #aaa; color: #555; }
        .rp-label {
          display: block;
          font-size: 13px;
          font-weight: 600;
          color: #666;
          margin-bottom: 6px;
          text-transform: uppercase;
          letter-spacing: 0.04em;
        }
        .rp-field { margin-bottom: 20px; }
        .rp-error {
          background: #fef2f2;
          border: 1px solid #fecaca;
          border-radius: 8px;
          padding: 12px 16px;
          font-size: 14px;
          color: #b91c1c;
          margin-bottom: 20px;
        }
        .rp-divider {
          border: none;
          border-top: 1px solid #e8e5e0;
          margin: 28px 0;
        }
        .rp-item-card {
          border: 1.5px solid #e8e5e0;
          border-radius: 12px;
          padding: 16px 18px;
          margin-bottom: 12px;
          transition: all 0.15s;
          cursor: pointer;
          background: #fafaf8;
        }
        .rp-item-card.selected {
          border-color: #c8a96e;
          background: #fffcf5;
        }
        .rp-item-card:hover { border-color: #bbb; }
        .rp-checkbox {
          width: 18px;
          height: 18px;
          accent-color: #c8a96e;
          cursor: pointer;
        }
        .rp-select {
          width: 100%;
          padding: 10px 14px;
          border: 1.5px solid #ddd8d0;
          border-radius: 8px;
          font-size: 14px;
          font-family: inherit;
          background: #fafaf8;
          color: #1a1a18;
          cursor: pointer;
          margin-top: 10px;
        }
        .rp-select:focus { outline: none; border-color: #c8a96e; }
        .rp-qty-row {
          display: flex;
          align-items: center;
          gap: 8px;
          margin-top: 10px;
        }
        .rp-qty-btn {
          width: 28px;
          height: 28px;
          border-radius: 6px;
          border: 1.5px solid #ddd8d0;
          background: #fff;
          font-size: 16px;
          cursor: pointer;
          display: flex;
          align-items: center;
          justify-content: center;
          font-family: inherit;
        }
        .rp-qty-btn:hover { border-color: #c8a96e; }
        .rp-status-pill {
          display: inline-flex;
          align-items: center;
          gap: 6px;
          padding: 6px 14px;
          border-radius: 100px;
          font-size: 13px;
          font-weight: 600;
          background: #f0f0ee;
        }
        .rp-step-tab {
          padding: 8px 18px;
          border: none;
          background: none;
          font-family: inherit;
          font-size: 13px;
          color: #999;
          cursor: pointer;
          border-bottom: 2px solid transparent;
          transition: all 0.15s;
        }
        .rp-step-tab.active {
          color: #1a1a18;
          border-bottom-color: #c8a96e;
          font-weight: 600;
        }
      `}</style>

      {/* Header */}
      <header style={{
        borderBottom: '1px solid #e8e5e0',
        background: '#fff',
        padding: '20px 40px',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'space-between',
      }}>
        <div>
          <div style={{ fontFamily: "'DM Serif Display', serif", fontSize: 22, color: '#1a1a18' }}>
            {config?.company_name || '…'}
          </div>
          <div style={{ fontSize: 12, color: '#999', marginTop: 2, letterSpacing: '0.08em', textTransform: 'uppercase' }}>
            Returns Portal
          </div>
        </div>
        <div style={{ fontSize: 12, color: '#bbb' }}>Powered by MarketMate</div>
      </header>

      {/* Nav tabs */}
      {!configError && (
        <div style={{ borderBottom: '1px solid #e8e5e0', background: '#fff', display: 'flex', paddingLeft: 40 }}>
          <button className={`rp-step-tab ${step !== 'status' ? 'active' : ''}`}
            onClick={() => { if (step === 'status') { setStep('lookup'); setStatusResult(null); } }}>
            Start a Return
          </button>
          <button className={`rp-step-tab ${step === 'status' ? 'active' : ''}`}
            onClick={() => setStep('status')}>
            Track a Return
          </button>
        </div>
      )}

      <main style={{ padding: '48px 24px', maxWidth: 640, margin: '0 auto' }}>

        {/* Config error */}
        {configError && (
          <div className="rp-card" style={{ textAlign: 'center' }}>
            <div style={{ fontSize: 40, marginBottom: 16 }}>🚫</div>
            <div style={{ fontSize: 16, fontWeight: 600, marginBottom: 8 }}>Returns unavailable</div>
            <div style={{ color: '#888', fontSize: 14 }}>{configError}</div>
          </div>
        )}

        {/* ── Status tab ── */}
        {!configError && step === 'status' && (
          <div className="rp-card">
            <h2 style={{ fontFamily: "'DM Serif Display', serif", fontSize: 26, fontWeight: 400, marginBottom: 6 }}>
              Track your return
            </h2>
            <p style={{ color: '#888', fontSize: 14, marginBottom: 28 }}>
              Enter your RMA reference number to see the current status.
            </p>
            <div className="rp-field">
              <label className="rp-label">RMA Reference Number</label>
              <input
                className="rp-input"
                placeholder="e.g. RMA-2026-0042"
                value={statusRmaNumber}
                onChange={e => setStatusRmaNumber(e.target.value)}
                onKeyDown={e => e.key === 'Enter' && handleStatusCheck()}
              />
            </div>
            {statusError && <div className="rp-error">{statusError}</div>}
            <button className="rp-btn rp-btn-primary" onClick={handleStatusCheck} disabled={statusLoading}>
              {statusLoading ? 'Looking up…' : 'Check Status'}
            </button>

            {statusResult && (
              <>
                <hr className="rp-divider" />
                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 16 }}>
                  <div style={{ fontWeight: 600, fontSize: 15 }}>{statusResult.rma_number}</div>
                  <span className="rp-status-pill" style={{ color: STATUS_META[statusResult.status]?.color || '#666' }}>
                    {STATUS_META[statusResult.status]?.icon} {STATUS_META[statusResult.status]?.label || statusResult.status}
                  </span>
                </div>
                <p style={{ fontSize: 14, color: '#666', marginBottom: 20 }}>{statusResult.status_label}</p>
                {statusResult.lines?.map((l: any, i: number) => (
                  <div key={i} style={{ fontSize: 13, color: '#888', padding: '8px 0', borderTop: '1px solid #f0f0ee' }}>
                    {l.product_name} — qty {l.qty_requested}
                  </div>
                ))}
              </>
            )}
          </div>
        )}

        {/* ── Step: Lookup ── */}
        {!configError && step === 'lookup' && (
          <div className="rp-card">
            <h2 style={{ fontFamily: "'DM Serif Display', serif", fontSize: 28, fontWeight: 400, marginBottom: 6 }}>
              Start a return
            </h2>
            <p style={{ color: '#888', fontSize: 14, marginBottom: 28 }}>
              Enter your order details to get started. Returns are accepted within {config?.window_days ?? 30} days of purchase.
            </p>

            <div className="rp-field">
              <label className="rp-label">Order Number</label>
              <input className="rp-input" placeholder="e.g. 123-4567890-1234567"
                value={orderNumber} onChange={e => setOrderNumber(e.target.value)} />
            </div>
            <div className="rp-field">
              <label className="rp-label">Delivery Postcode</label>
              <input className="rp-input" placeholder="e.g. SW1A 1AA"
                value={postcode} onChange={e => setPostcode(e.target.value)}
                onKeyDown={e => e.key === 'Enter' && handleLookup()} />
            </div>

            {lookupError && <div className="rp-error">{lookupError}</div>}

            <button className="rp-btn rp-btn-primary" onClick={handleLookup} disabled={lookupLoading}>
              {lookupLoading ? 'Finding your order…' : 'Find My Order'}
            </button>

            {config?.policy_text && (
              <>
                <hr className="rp-divider" />
                <p style={{ fontSize: 13, color: '#aaa', lineHeight: 1.6 }}>{config.policy_text}</p>
              </>
            )}
          </div>
        )}

        {/* ── Step: Select items ── */}
        {!configError && step === 'select' && order && (
          <div>
            <div style={{ marginBottom: 24, display: 'flex', alignItems: 'center', gap: 16 }}>
              <button className="rp-btn rp-btn-ghost" onClick={() => { setStep('lookup'); setOrder(null); }}>
                ← Back
              </button>
              <div>
                <div style={{ fontWeight: 600, fontSize: 15 }}>Order {order.order_number}</div>
                <div style={{ fontSize: 12, color: '#999' }}>Select the items you'd like to return</div>
              </div>
            </div>

            <div className="rp-card" style={{ marginBottom: 20 }}>
              <div style={{ fontWeight: 600, marginBottom: 4, fontSize: 14 }}>Your details</div>
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12, marginTop: 12 }}>
                <div className="rp-field" style={{ marginBottom: 0 }}>
                  <label className="rp-label">Your Name</label>
                  <input className="rp-input" placeholder="Full name"
                    value={customerName} onChange={e => setCustomerName(e.target.value)} />
                </div>
                <div className="rp-field" style={{ marginBottom: 0 }}>
                  <label className="rp-label">Email Address</label>
                  <input className="rp-input" type="email" placeholder="For updates"
                    value={customerEmail} onChange={e => setCustomerEmail(e.target.value)} />
                </div>
              </div>
            </div>

            <div className="rp-card">
              <div style={{ fontWeight: 600, marginBottom: 16, fontSize: 14 }}>
                Items in this order — select what you're returning
              </div>

              {returnLines.map((line, idx) => (
                <div key={line.line_id}
                  className={`rp-item-card ${line.selected ? 'selected' : ''}`}
                  onClick={() => setReturnLines(prev => prev.map((l, i) =>
                    i === idx ? { ...l, selected: !l.selected } : l))}>
                  <div style={{ display: 'flex', alignItems: 'flex-start', gap: 12 }}>
                    <input type="checkbox" className="rp-checkbox"
                      checked={line.selected}
                      onChange={() => {}}
                      onClick={e => e.stopPropagation()} />
                    <div style={{ flex: 1 }}>
                      <div style={{ fontWeight: 600, fontSize: 14, marginBottom: 2 }}>{line.product_name || '—'}</div>
                      <div style={{ fontSize: 12, color: '#999' }}>SKU: {line.sku || '—'} · Qty ordered: {line.quantity}</div>

                      {line.selected && (
                        <div onClick={e => e.stopPropagation()}>
                          <div className="rp-qty-row">
                            <span style={{ fontSize: 12, color: '#777', marginRight: 4 }}>Return qty:</span>
                            <button className="rp-qty-btn"
                              onClick={() => setReturnLines(prev => prev.map((l, i) =>
                                i === idx ? { ...l, qty_requested: Math.max(1, l.qty_requested - 1) } : l))}>−</button>
                            <span style={{ minWidth: 24, textAlign: 'center', fontWeight: 600, fontSize: 14 }}>
                              {line.qty_requested}
                            </span>
                            <button className="rp-qty-btn"
                              onClick={() => setReturnLines(prev => prev.map((l, i) =>
                                i === idx ? { ...l, qty_requested: Math.min(line.quantity, l.qty_requested + 1) } : l))}>+</button>
                          </div>

                          <select className="rp-select"
                            value={line.reason_code}
                            onChange={e => setReturnLines(prev => prev.map((l, i) =>
                              i === idx ? { ...l, reason_code: e.target.value } : l))}>
                            <option value="">Select reason for return…</option>
                            {REASON_CODES.map(r => (
                              <option key={r.value} value={r.value}>{r.label}</option>
                            ))}
                          </select>

                          {line.reason_code === 'other' && (
                            <input className="rp-input"
                              style={{ marginTop: 10 }}
                              placeholder="Please describe the issue…"
                              value={line.reason_detail}
                              onChange={e => setReturnLines(prev => prev.map((l, i) =>
                                i === idx ? { ...l, reason_detail: e.target.value } : l))} />
                          )}
                        </div>
                      )}
                    </div>
                  </div>
                </div>
              ))}

              {submitError && <div className="rp-error" style={{ marginTop: 8 }}>{submitError}</div>}

              <div style={{ marginTop: 24, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                <div style={{ fontSize: 13, color: '#999' }}>
                  {returnLines.filter(l => l.selected).length} item{returnLines.filter(l => l.selected).length !== 1 ? 's' : ''} selected
                </div>
                <button className="rp-btn rp-btn-primary"
                  style={{ width: 'auto' }}
                  onClick={handleSubmit}
                  disabled={submitLoading || returnLines.filter(l => l.selected).length === 0}>
                  {submitLoading ? 'Submitting…' : 'Submit Return Request →'}
                </button>
              </div>
            </div>
          </div>
        )}

        {/* ── Step: Done ── */}
        {!configError && step === 'done' && (
          <div className="rp-card" style={{ textAlign: 'center' }}>
            <div style={{ fontSize: 52, marginBottom: 20 }}>📬</div>
            <h2 style={{ fontFamily: "'DM Serif Display', serif", fontSize: 28, fontWeight: 400, marginBottom: 12 }}>
              Return requested
            </h2>
            <div style={{
              display: 'inline-block',
              background: '#f0f9f4',
              border: '1px solid #a7f3d0',
              borderRadius: 10,
              padding: '14px 28px',
              marginBottom: 20,
            }}>
              <div style={{ fontSize: 12, color: '#059669', fontWeight: 600, letterSpacing: '0.06em', textTransform: 'uppercase', marginBottom: 4 }}>
                Your RMA Reference
              </div>
              <div style={{ fontFamily: "'DM Serif Display', serif", fontSize: 28, color: '#047857' }}>
                {rmaNumber}
              </div>
            </div>
            <p style={{ color: '#666', fontSize: 14, lineHeight: 1.6, maxWidth: 380, margin: '0 auto 28px' }}>
              {rmaMessage || 'Your return request has been received. Please keep your reference number safe.'}
            </p>
            <div style={{ display: 'flex', gap: 12, justifyContent: 'center' }}>
              <button className="rp-btn rp-btn-ghost" onClick={() => {
                setStatusRmaNumber(rmaNumber);
                setStep('status');
              }}>
                Track this return →
              </button>
              <button className="rp-btn rp-btn-ghost" onClick={() => {
                setStep('lookup');
                setOrder(null);
                setOrderNumber('');
                setPostcode('');
                setCustomerName('');
                setCustomerEmail('');
                setRmaNumber('');
              }}>
                Start another return
              </button>
            </div>
          </div>
        )}
      </main>
    </div>
  );
}

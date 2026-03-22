import { useState, useEffect, useCallback } from 'react';
import { getActiveTenantId } from '../contexts/TenantContext';
import { SerialNumberInput, SerialRequiredBadge, useIsSerialProduct } from '../components/SerialNumberInput';

const API_BASE = import.meta.env.VITE_API_URL ?? 'http://localhost:8080/api/v1';
function api(path: string, init?: RequestInit) {
  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId(), ...init?.headers },
  });
}

// ─── Types ────────────────────────────────────────────────────────────────────

interface WarehouseLocation {
  location_id: string;
  name: string;
  path: string;
  source_id: string;
}

interface Product {
  product_id: string;
  title: string;
  sku: string;
}

interface InventoryAdjustment {
  adjustment_id: string;
  product_id: string;
  product_sku: string;
  product_name: string;
  location_id: string;
  location_path: string;
  type: string;
  delta: number;
  quantity_before: number;
  quantity_after: number;
  reason: string;
  reference: string;
  created_by: string;
  created_at: string;
}

// ─── Main Component ───────────────────────────────────────────────────────────

export default function WarehouseTransfers() {
  const [tab, setTab] = useState<'new' | 'open' | 'history'>('new');

  // New transfer form
  const [locations, setLocations] = useState<WarehouseLocation[]>([]);
  const [fromLocation, setFromLocation] = useState('');
  const [toLocation, setToLocation] = useState('');
  const [product, setProduct] = useState<Product | null>(null);
  const [productSearch, setProductSearch] = useState('');
  const [productResults, setProductResults] = useState<Product[]>([]);
  const [quantity, setQuantity] = useState('');
  const [reason, setReason] = useState('');
  const [transferring, setTransferring] = useState(false);
  const [transferError, setTransferError] = useState('');
  const [transferSuccess, setTransferSuccess] = useState<string | null>(null);
  const [currentStock, setCurrentStock] = useState<number | null>(null);

  // History
  const [history, setHistory] = useState<InventoryAdjustment[]>([]);
  const [historyLoading, setHistoryLoading] = useState(false);
  const [historyError, setHistoryError] = useState('');
  const [locationFilter, setLocationFilter] = useState('');

  // Serial number tracking
  const [serialNumbers, setSerialNumbers] = useState<string[]>([]);
  const { isSerial: productIsSerial } = useIsSerialProduct(product?.product_id ?? null);
  // Reset serials when product or quantity changes
  useEffect(() => { setSerialNumbers([]); }, [product?.product_id, quantity]);

  // Confirmation modal
  const [showConfirm, setShowConfirm] = useState(false);

  // Open moves
  const [openMoves, setOpenMoves] = useState<InventoryAdjustment[]>([]);
  const [openMovesLoading, setOpenMovesLoading] = useState(false);

  const loadLocations = useCallback(async () => {
    const res = await api('/locations');
    if (res.ok) {
      const d = await res.json();
      // Flatten tree to list for dropdown
      const flatten = (nodes: any[]): WarehouseLocation[] =>
        nodes.flatMap(n => [{ location_id: n.location_id, name: n.name, path: n.path, source_id: n.source_id }, ...flatten(n.children || [])]);
      setLocations(flatten(d.locations || []));
    }
  }, []);

  const loadHistory = useCallback(async () => {
    setHistoryLoading(true);
    setHistoryError('');
    try {
      let url = '/inventory/adjustments?type=transfer&limit=100';
      if (locationFilter) url += `&location_id=${locationFilter}`;
      const res = await api(url);
      if (res.ok) {
        const d = await res.json();
        setHistory(d.adjustments || []);
      } else {
        setHistoryError('Failed to load transfer history');
      }
    } catch {
      setHistoryError('Network error');
    } finally {
      setHistoryLoading(false);
    }
  }, [locationFilter]);

  const loadOpenMoves = useCallback(async () => {
    setOpenMovesLoading(true);
    try {
      const res = await api('/inventory/adjustments?type=transfer&status=pending,in_progress&limit=100');
      if (res.ok) {
        const d = await res.json();
        setOpenMoves(d.adjustments || []);
      }
    } finally { setOpenMovesLoading(false); }
  }, []);

  useEffect(() => { loadLocations(); }, [loadLocations]);
  useEffect(() => { if (tab === 'history') loadHistory(); }, [tab, loadHistory]);
  useEffect(() => { if (tab === 'open') loadOpenMoves(); }, [tab, loadOpenMoves]);

  // Product search
  useEffect(() => {
    if (!productSearch || productSearch.length < 2) { setProductResults([]); return; }
    const t = setTimeout(async () => {
      const res = await api(`/products?search=${encodeURIComponent(productSearch)}&limit=10`);
      if (res.ok) {
        const d = await res.json();
        setProductResults(d.products || []);
      }
    }, 300);
    return () => clearTimeout(t);
  }, [productSearch]);

  // Load current stock when product + from_location changes
  useEffect(() => {
    if (!product || !fromLocation) { setCurrentStock(null); return; }
    api(`/inventory/${product.product_id}?location_id=${fromLocation}`).then(async res => {
      if (res.ok) {
        const d = await res.json();
        const records: any[] = d.inventory || [];
        const record = records.find((r: any) => r.location_id === fromLocation);
        setCurrentStock(record?.quantity ?? 0);
      }
    });
  }, [product, fromLocation]);

  const handleTransfer = async () => {
    if (!product || !fromLocation || !toLocation || !quantity) {
      setTransferError('Please fill all required fields');
      return;
    }
    if (fromLocation === toLocation) {
      setTransferError('From and To locations must be different');
      return;
    }
    const qty = parseInt(quantity);
    if (!qty || qty < 1) { setTransferError('Enter a valid quantity'); return; }
    if (currentStock !== null && qty > currentStock) {
      setTransferError(`Cannot transfer ${qty} — only ${currentStock} available at source`);
      return;
    }
    setTransferError('');
    if (productIsSerial && serialNumbers.length < parseInt(quantity)) {
      setTransferError(
        `Serial numbers required: enter ${parseInt(quantity) - serialNumbers.length} more serial number(s) before transferring`
      );
      return;
    }
    setShowConfirm(true);
  };

  const executeTransfer = async () => {
    setShowConfirm(false);
    setTransferring(true);
    setTransferError('');
    const qty = parseInt(quantity);
    try {
      const res = await api('/inventory/transfer', {
        method: 'POST',
        body: JSON.stringify({
          product_id: product!.product_id,
          from_location_id: fromLocation,
          to_location_id: toLocation,
          quantity: qty,
          reason: reason || 'Manual transfer',
          serial_numbers: productIsSerial ? serialNumbers : undefined,
        }),
      });
      if (res.ok) {
        setTransferSuccess(`Successfully transferred ${qty}× ${product!.sku} from ${locationName(fromLocation)} to ${locationName(toLocation)}`);
        setProduct(null);
        setProductSearch('');
        setQuantity('');
        setReason('');
        setCurrentStock(null);
        setSerialNumbers([]);
      } else {
        const d = await res.json().catch(() => ({}));
        setTransferError(d.error || 'Transfer failed');
      }
    } catch {
      setTransferError('Network error');
    } finally {
      setTransferring(false);
    }
  };

  const markMoveStatus = async (adj: InventoryAdjustment, newStatus: string) => {
    await api(`/inventory/adjustments/${adj.adjustment_id}/status`, {
      method: 'PUT',
      body: JSON.stringify({ status: newStatus }),
    });
    loadOpenMoves();
  };

  const locationName = (id: string) => locations.find(l => l.location_id === id)?.path || id;

  const formatDelta = (delta: number) => (
    <span style={{ color: delta > 0 ? 'var(--success)' : 'var(--danger)', fontWeight: 600 }}>
      {delta > 0 ? `+${delta}` : delta}
    </span>
  );

  // ── Render ─────────────────────────────────────────────────────────────────

  return (
    <div style={{ padding: '24px 32px', maxWidth: 1100, margin: '0 auto' }}>
      <div style={{ marginBottom: 24 }}>
        <h1 style={{ margin: 0, fontSize: 24, fontWeight: 700, color: 'var(--text-primary)' }}>Warehouse Transfers</h1>
        <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 14 }}>
          Move stock between warehouse locations with a full audit trail.
        </p>
      </div>

      {/* Tabs */}
      <div style={{ display: 'flex', borderBottom: '1px solid var(--border)', marginBottom: 24 }}>
        {(['new', 'open', 'history'] as const).map(t => (
          <button key={t} onClick={() => setTab(t)} style={{
            padding: '10px 20px', background: 'none', border: 'none',
            borderBottom: tab === t ? '2px solid var(--primary)' : '2px solid transparent',
            color: tab === t ? 'var(--primary)' : 'var(--text-muted)',
            cursor: 'pointer', fontSize: 14, fontWeight: tab === t ? 600 : 400, marginBottom: -1,
          }}>
            {t === 'new' ? '↔ New Transfer' : t === 'open' ? '⏳ Open Moves' : '📋 Transfer History'}
          </button>
        ))}
      </div>

      {tab === 'new' && (
        <div style={{ maxWidth: 560 }}>
          {transferSuccess && (
            <div style={{ padding: '12px 16px', background: 'rgba(16,185,129,0.1)', border: '1px solid rgba(16,185,129,0.3)', borderRadius: 8, color: 'var(--success)', fontSize: 13, marginBottom: 20, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
              <span>✓ {transferSuccess}</span>
              <button style={{ background: 'none', border: 'none', color: 'var(--success)', cursor: 'pointer', fontSize: 16 }} onClick={() => setTransferSuccess(null)}>✕</button>
            </div>
          )}

          {/* Product search */}
          <div style={fieldStyle}>
            <label style={labelStyle}>Product / SKU <span style={{ color: 'var(--danger)' }}>*</span></label>
            {product ? (
              <div style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '8px 12px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6 }}>
                <div style={{ flex: 1 }}>
                  <span style={{ fontFamily: 'monospace', fontSize: 12, color: 'var(--text-secondary)' }}>{product.sku}</span>
                  <span style={{ marginLeft: 8, color: 'var(--text-primary)', fontSize: 13 }}>{product.title}</span>
                {productIsSerial && <SerialRequiredBadge />}
                </div>
                <button style={{ background: 'none', border: 'none', color: 'var(--text-muted)', cursor: 'pointer', fontSize: 14 }} onClick={() => { setProduct(null); setProductSearch(''); }}>✕</button>
              </div>
            ) : (
              <div style={{ position: 'relative' }}>
                <input
                  style={inputStyle}
                  placeholder="Type SKU or product name to search…"
                  value={productSearch}
                  onChange={e => setProductSearch(e.target.value)}
                />
                {productResults.length > 0 && (
                  <div style={{ position: 'absolute', top: '100%', left: 0, right: 0, background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6, zIndex: 10, maxHeight: 200, overflowY: 'auto' }}>
                    {productResults.map(p => (
                      <div key={p.product_id} style={{ padding: '8px 12px', cursor: 'pointer', fontSize: 13, borderBottom: '1px solid var(--border)' }}
                        onMouseDown={() => { setProduct(p); setProductSearch(''); setProductResults([]); }}>
                        <span style={{ fontFamily: 'monospace', fontSize: 11, color: 'var(--text-secondary)', marginRight: 8 }}>{p.sku}</span>
                        {p.title}
                      </div>
                    ))}
                  </div>
                )}
              </div>
            )}
          </div>

          {/* Locations */}
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12, marginTop: 14 }}>
            <div style={fieldStyle}>
              <label style={labelStyle}>From Location <span style={{ color: 'var(--danger)' }}>*</span></label>
              <select style={inputStyle} value={fromLocation} onChange={e => setFromLocation(e.target.value)}>
                <option value="">Select location…</option>
                {locations.map(l => <option key={l.location_id} value={l.location_id}>{l.path || l.name}</option>)}
              </select>
              {currentStock !== null && fromLocation && product && (
                <p style={{ margin: '4px 0 0', fontSize: 12, color: 'var(--text-muted)' }}>
                  Available: <strong style={{ color: currentStock > 0 ? 'var(--text-primary)' : 'var(--danger)' }}>{currentStock}</strong>
                </p>
              )}
            </div>
            <div style={fieldStyle}>
              <label style={labelStyle}>To Location <span style={{ color: 'var(--danger)' }}>*</span></label>
              <select style={inputStyle} value={toLocation} onChange={e => setToLocation(e.target.value)}>
                <option value="">Select location…</option>
                {locations.filter(l => l.location_id !== fromLocation).map(l => (
                  <option key={l.location_id} value={l.location_id}>{l.path || l.name}</option>
                ))}
              </select>
            </div>
          </div>

          <div style={fieldStyle}>
            <label style={labelStyle}>Quantity <span style={{ color: 'var(--danger)' }}>*</span></label>
            <input
              type="number" min={1}
              style={{ ...inputStyle, maxWidth: 160 }}
              placeholder="0"
              value={quantity}
              onChange={e => setQuantity(e.target.value)}
            />
          </div>

          <div style={fieldStyle}>
            <label style={labelStyle}>Reason</label>
            <input style={inputStyle} placeholder="e.g. Replenishment, Restock, Relocation…" value={reason} onChange={e => setReason(e.target.value)} />
          </div>

          {productIsSerial && product && quantity && parseInt(quantity) > 0 && (
            <div style={{ marginTop: 14 }}>
              <SerialNumberInput
                productId={product.product_id}
                quantity={parseInt(quantity)}
                value={serialNumbers}
                onChange={setSerialNumbers}
                label="Serial Numbers to Transfer"
              />
            </div>
          )}

          {transferError && (
            <div style={{ marginTop: 12, padding: '10px 14px', background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 6, color: 'var(--danger)', fontSize: 13 }}>
              {transferError}
            </div>
          )}

          <div style={{ marginTop: 20 }}>
            <button style={btnPrimaryStyle} onClick={handleTransfer} disabled={transferring}>
              {transferring ? 'Processing…' : '↔ Review Transfer'}
            </button>
          </div>
        </div>
      )}

      {/* Confirmation modal */}
      {showConfirm && product && (
        <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.6)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 }}>
          <div style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 12, padding: 32, maxWidth: 460, width: '100%', boxShadow: '0 20px 60px rgba(0,0,0,0.5)' }}>
            <h2 style={{ margin: '0 0 20px', fontSize: 18, fontWeight: 700, color: 'var(--text-primary)' }}>Confirm Transfer</h2>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 12, marginBottom: 24, padding: 16, background: 'var(--bg-elevated)', borderRadius: 8, border: '1px solid var(--border)' }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 14 }}>
                <span style={{ color: 'var(--text-muted)' }}>Product</span>
                <span style={{ fontWeight: 600, color: 'var(--text-primary)' }}>{product.sku} — {product.title}</span>
              </div>
              <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 14 }}>
                <span style={{ color: 'var(--text-muted)' }}>From</span>
                <span style={{ color: 'var(--text-primary)' }}>{locationName(fromLocation)}</span>
              </div>
              <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 14 }}>
                <span style={{ color: 'var(--text-muted)' }}>To</span>
                <span style={{ color: 'var(--text-primary)' }}>{locationName(toLocation)}</span>
              </div>
              <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 14 }}>
                <span style={{ color: 'var(--text-muted)' }}>Quantity</span>
                <span style={{ fontWeight: 700, color: 'var(--primary)', fontSize: 16 }}>{quantity}</span>
              </div>
              {reason && (
                <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 14 }}>
                  <span style={{ color: 'var(--text-muted)' }}>Reason</span>
                  <span style={{ color: 'var(--text-primary)' }}>{reason}</span>
                </div>
              )}
              {productIsSerial && serialNumbers.length > 0 && (
                <div style={{ fontSize: 14 }}>
                  <span style={{ color: 'var(--text-muted)', display: 'block', marginBottom: 4 }}>Serial Numbers</span>
                  <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }}>
                    {serialNumbers.map(sn => (
                      <span key={sn} style={{ padding: '2px 8px', background: 'rgba(34,197,94,0.1)', border: '1px solid rgba(34,197,94,0.25)', borderRadius: 99, fontSize: 11, color: '#22c55e', fontFamily: 'monospace' }}>{sn}</span>
                    ))}
                  </div>
                </div>
              )}
            </div>
            <div style={{ display: 'flex', gap: 12, justifyContent: 'flex-end' }}>
              <button style={btnGhostStyle} onClick={() => setShowConfirm(false)}>Cancel</button>
              <button style={btnPrimaryStyle} onClick={executeTransfer}>✓ Confirm Transfer</button>
            </div>
          </div>
        </div>
      )}

      {tab === 'open' && (
        <>
          <div style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: 16 }}>
            <button style={btnGhostStyle} onClick={loadOpenMoves}>↺ Refresh</button>
          </div>
          {openMovesLoading ? (
            <div style={{ padding: 48, textAlign: 'center', color: 'var(--text-muted)' }}>Loading…</div>
          ) : (
            <div style={{ background: 'var(--bg-secondary)', borderRadius: 12, border: '1px solid var(--border)', overflow: 'hidden' }}>
              <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
                <thead>
                  <tr style={{ background: 'var(--bg-tertiary)' }}>
                    {['Date', 'SKU / Product', 'From → To', 'Qty', 'Status', 'Actions'].map(h => (
                      <th key={h} style={thStyle}>{h}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {openMoves.map(adj => (
                    <tr key={adj.adjustment_id} style={{ borderTop: '1px solid var(--border)' }}>
                      <td style={{ ...tdStyle, fontSize: 12, color: 'var(--text-muted)', whiteSpace: 'nowrap' }}>
                        {new Date(adj.created_at).toLocaleDateString()}
                      </td>
                      <td style={tdStyle}>
                        <div style={{ fontFamily: 'monospace', fontSize: 11, color: 'var(--text-secondary)' }}>{adj.product_sku}</div>
                        <div>{adj.product_name}</div>
                      </td>
                      <td style={{ ...tdStyle, fontSize: 12, color: 'var(--text-muted)' }}>{adj.location_path}</td>
                      <td style={{ ...tdStyle, fontWeight: 600 }}>{Math.abs(adj.delta)}</td>
                      <td style={tdStyle}>
                        <span style={{ padding: '2px 8px', borderRadius: 10, fontSize: 11, fontWeight: 600,
                          background: adj.type === 'in_progress' ? 'rgba(59,130,246,0.15)' : 'rgba(245,158,11,0.15)',
                          color: adj.type === 'in_progress' ? '#3b82f6' : '#f59e0b' }}>
                          {adj.type || 'pending'}
                        </span>
                      </td>
                      <td style={tdStyle}>
                        <div style={{ display: 'flex', gap: 6 }}>
                          <button onClick={() => markMoveStatus(adj, 'in_progress')}
                            style={{ ...btnGhostStyle, fontSize: 11, padding: '3px 8px' }}>In Progress</button>
                          <button onClick={() => markMoveStatus(adj, 'complete')}
                            style={{ ...btnPrimaryStyle, fontSize: 11, padding: '3px 8px' }}>Complete</button>
                        </div>
                      </td>
                    </tr>
                  ))}
                  {openMoves.length === 0 && (
                    <tr><td colSpan={6} style={{ padding: '32px', textAlign: 'center', color: 'var(--text-muted)' }}>
                      No open moves found.
                    </td></tr>
                  )}
                </tbody>
              </table>
            </div>
          )}
        </>
      )}

      {tab === 'history' && (
        <>
          <div style={{ display: 'flex', gap: 10, marginBottom: 16 }}>
            <select style={{ ...inputStyle, maxWidth: 260 }} value={locationFilter} onChange={e => setLocationFilter(e.target.value)}>
              <option value="">All Locations</option>
              {locations.map(l => <option key={l.location_id} value={l.location_id}>{l.path || l.name}</option>)}
            </select>
            <button style={btnGhostStyle} onClick={loadHistory}>↺ Refresh</button>
          </div>

          {historyError && (
            <div style={{ padding: '10px 14px', background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 6, color: 'var(--danger)', fontSize: 13, marginBottom: 16 }}>
              {historyError}
            </div>
          )}

          {historyLoading ? (
            <div style={{ padding: 48, textAlign: 'center', color: 'var(--text-muted)' }}>Loading…</div>
          ) : (
            <div style={{ background: 'var(--bg-secondary)', borderRadius: 12, border: '1px solid var(--border)', overflow: 'hidden' }}>
              <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
                <thead>
                  <tr style={{ background: 'var(--bg-tertiary)' }}>
                    {['Date', 'SKU / Product', 'Location', 'Change', 'After', 'Reason', 'By'].map(h => (
                      <th key={h} style={thStyle}>{h}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {history.map(adj => (
                    <tr key={adj.adjustment_id} style={{ borderTop: '1px solid var(--border)' }}>
                      <td style={{ ...tdStyle, fontSize: 12, color: 'var(--text-muted)', whiteSpace: 'nowrap' }}>
                        {new Date(adj.created_at).toLocaleDateString()} {new Date(adj.created_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
                      </td>
                      <td style={tdStyle}>
                        <div style={{ fontFamily: 'monospace', fontSize: 11, color: 'var(--text-secondary)' }}>{adj.product_sku}</div>
                        <div>{adj.product_name}</div>
                      </td>
                      <td style={{ ...tdStyle, fontSize: 12, color: 'var(--text-muted)' }}>{adj.location_path}</td>
                      <td style={{ ...tdStyle, textAlign: 'center' }}>{formatDelta(adj.delta)}</td>
                      <td style={{ ...tdStyle, textAlign: 'center', color: 'var(--text-secondary)' }}>{adj.quantity_after}</td>
                      <td style={{ ...tdStyle, color: 'var(--text-secondary)' }}>{adj.reason || '—'}</td>
                      <td style={{ ...tdStyle, fontSize: 12, color: 'var(--text-muted)' }}>{adj.created_by || '—'}</td>
                    </tr>
                  ))}
                  {history.length === 0 && (
                    <tr><td colSpan={7} style={{ padding: '32px', textAlign: 'center', color: 'var(--text-muted)' }}>
                      No transfer history found.
                    </td></tr>
                  )}
                </tbody>
              </table>
            </div>
          )}
        </>
      )}
    </div>
  );
}

// ─── Styles ───────────────────────────────────────────────────────────────────
const fieldStyle: React.CSSProperties = { marginTop: 0 };
const labelStyle: React.CSSProperties = { display: 'block', marginBottom: 6, fontSize: 13, fontWeight: 500, color: 'var(--text-secondary)' };
const inputStyle: React.CSSProperties = { width: '100%', padding: '8px 12px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', fontSize: 14, outline: 'none', boxSizing: 'border-box' };
const btnPrimaryStyle: React.CSSProperties = { padding: '8px 16px', background: 'var(--primary)', color: 'white', border: 'none', borderRadius: 6, cursor: 'pointer', fontSize: 13, fontWeight: 600 };
const btnGhostStyle: React.CSSProperties = { padding: '8px 16px', background: 'transparent', color: 'var(--text-secondary)', border: '1px solid var(--border)', borderRadius: 6, cursor: 'pointer', fontSize: 13 };
const thStyle: React.CSSProperties = { padding: '10px 16px', textAlign: 'left', fontSize: 11, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--text-muted)', borderBottom: '1px solid var(--border)' };
const tdStyle: React.CSSProperties = { padding: '10px 16px', color: 'var(--text-primary)' };

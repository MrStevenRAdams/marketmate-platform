import { useState, useEffect, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { getActiveTenantId } from '../contexts/TenantContext';

const API_BASE = import.meta.env.VITE_API_URL ?? 'http://localhost:8080/api/v1';
function api(path: string, init?: RequestInit) {
  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId(), ...init?.headers },
  });
}

// ─── Types ────────────────────────────────────────────────────────────────────

interface ProductForecast {
  product_id: string;
  sku: string;
  product_name: string;
  calculated_adc: number;
  current_stock: number;
  days_of_stock: number;
  reorder_point: number;
  reorder_qty: number;
  forecast_status: 'ok' | 'low' | 'critical' | 'out_of_stock' | 'unconfigured';
}

interface Supplier {
  supplier_id: string;
  name: string;
  lead_time_days?: number;
}

interface ReplenishLine {
  product_id: string;
  sku: string;
  product_name: string;
  qty: number;
  current_stock: number;
  days_of_stock: number;
  forecast_status: string;
  supplier_id: string;
}

const STATUS_CONFIG = {
  low:          { label: 'Low',         color: 'var(--warning)',      bg: 'rgba(245,158,11,0.12)' },
  critical:     { label: 'Critical',    color: 'var(--accent-orange)', bg: 'rgba(249,115,22,0.12)' },
  out_of_stock: { label: 'Out of Stock', color: 'var(--danger)',       bg: 'rgba(239,68,68,0.12)' },
  ok:           { label: 'Healthy',     color: 'var(--success)',       bg: 'rgba(16,185,129,0.12)' },
  unconfigured: { label: 'Unconfigured', color: 'var(--text-muted)',   bg: 'rgba(100,116,139,0.12)' },
};

// ─── Main Component ───────────────────────────────────────────────────────────

export default function ForecastReplenishment() {
  const navigate = useNavigate();
  const [forecasts, setForecasts] = useState<ProductForecast[]>([]);
  const [suppliers, setSuppliers] = useState<Supplier[]>([]);
  const [loading, setLoading] = useState(true);
  const [statusFilter, setStatusFilter] = useState<string>('low,critical,out_of_stock');
  const [search, setSearch] = useState('');

  // Selected items for PO creation
  const [selected, setSelected] = useState<Map<string, ReplenishLine>>(new Map());
  const [defaultSupplierId, setDefaultSupplierId] = useState('');

  // Wizard state
  const [showWizard, setShowWizard] = useState(false);
  const [wizardStep, setWizardStep] = useState(1);
  const [groupedBySup, setGroupedBySup] = useState<Map<string, ReplenishLine[]>>(new Map());
  const [creating, setCreating] = useState(false);
  const [createdPOs, setCreatedPOs] = useState<{ po_id: string; po_number: string; supplier: string; lines: number }[]>([]);
  const [wizardError, setWizardError] = useState('');

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [fRes, sRes] = await Promise.all([
        api('/forecasting/products?limit=500'),
        api('/suppliers?limit=200'),
      ]);
      if (fRes.ok) {
        const d = await fRes.json();
        setForecasts(d.forecasts || []);
      }
      if (sRes.ok) {
        const d = await sRes.json();
        setSuppliers(d.suppliers || []);
      }
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { load(); }, [load]);

  const statuses = statusFilter ? statusFilter.split(',') : [];
  const filtered = forecasts.filter(f => {
    if (statuses.length > 0 && !statuses.includes(f.forecast_status)) return false;
    if (search) {
      const q = search.toLowerCase();
      if (!f.sku?.toLowerCase().includes(q) && !f.product_name?.toLowerCase().includes(q)) return false;
    }
    return true;
  });

  const toggleSelect = (f: ProductForecast) => {
    setSelected(prev => {
      const next = new Map(prev);
      if (next.has(f.product_id)) {
        next.delete(f.product_id);
      } else {
        next.set(f.product_id, {
          product_id: f.product_id,
          sku: f.sku,
          product_name: f.product_name,
          qty: f.reorder_qty || 1,
          current_stock: f.current_stock,
          days_of_stock: f.days_of_stock,
          forecast_status: f.forecast_status,
          supplier_id: defaultSupplierId,
        });
      }
      return next;
    });
  };

  const toggleAll = () => {
    if (selected.size === filtered.length) {
      setSelected(new Map());
    } else {
      const next = new Map<string, ReplenishLine>();
      filtered.forEach(f => {
        next.set(f.product_id, {
          product_id: f.product_id,
          sku: f.sku,
          product_name: f.product_name,
          qty: f.reorder_qty || 1,
          current_stock: f.current_stock,
          days_of_stock: f.days_of_stock,
          forecast_status: f.forecast_status,
          supplier_id: defaultSupplierId,
        });
      });
      setSelected(next);
    }
  };

  const updateLine = (productId: string, field: 'qty' | 'supplier_id', value: string | number) => {
    setSelected(prev => {
      const next = new Map(prev);
      const line = next.get(productId);
      if (line) next.set(productId, { ...line, [field]: value });
      return next;
    });
  };

  const openWizard = () => {
    // Group selected lines by supplier
    const grouped = new Map<string, ReplenishLine[]>();
    selected.forEach(line => {
      const sid = line.supplier_id || '__unassigned__';
      const arr = grouped.get(sid) || [];
      arr.push(line);
      grouped.set(sid, arr);
    });
    setGroupedBySup(grouped);
    setWizardStep(1);
    setCreatedPOs([]);
    setWizardError('');
    setShowWizard(true);
  };

  const createPOs = async () => {
    setCreating(true);
    setWizardError('');
    const results: typeof createdPOs = [];

    for (const [supplierId, lines] of groupedBySup.entries()) {
      if (supplierId === '__unassigned__') continue;
      const sup = suppliers.find(s => s.supplier_id === supplierId);
      try {
        const res = await api('/forecasting/create-po', {
          method: 'POST',
          body: JSON.stringify({
            supplier_id: supplierId,
            lines: lines.map(l => ({
              product_id: l.product_id,
              sku: l.sku,
              description: l.product_name,
              qty: l.qty,
            })),
            notes: `Auto-created from Replenishment screen`,
          }),
        });
        if (res.ok) {
          const d = await res.json();
          results.push({ po_id: d.po_id, po_number: d.po_number, supplier: sup?.name || supplierId, lines: d.lines });
        } else {
          setWizardError(`Failed to create PO for ${sup?.name || supplierId}`);
        }
      } catch {
        setWizardError(`Network error creating PO for ${sup?.name || supplierId}`);
      }
    }

    setCreatedPOs(results);
    setCreating(false);
    if (results.length > 0) setWizardStep(3);
  };

  const supplierName = (id: string) => suppliers.find(s => s.supplier_id === id)?.name || id;

  // ── Render ─────────────────────────────────────────────────────────────────

  return (
    <div style={{ padding: '24px 32px', maxWidth: 1300, margin: '0 auto' }}>
      {/* Header */}
      <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', marginBottom: 24 }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 24, fontWeight: 700, color: 'var(--text-primary)' }}>
            Replenishment
          </h1>
          <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 14 }}>
            Review items below reorder point and create purchase orders in bulk.
          </p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <button style={btnGhostStyle} onClick={() => navigate('/forecasting')}>← Forecasting</button>
          {selected.size > 0 && (
            <button style={btnPrimaryStyle} onClick={openWizard}>
              🛒 Create POs ({selected.size} items)
            </button>
          )}
        </div>
      </div>

      {/* Filters */}
      <div style={{ display: 'flex', gap: 10, marginBottom: 20, flexWrap: 'wrap' }}>
        <input
          style={{ ...inputStyle, width: 240, margin: 0 }}
          placeholder="Search SKU or product…"
          value={search}
          onChange={e => setSearch(e.target.value)}
        />
        <select style={{ ...inputStyle, width: 220, margin: 0 }} value={statusFilter} onChange={e => setStatusFilter(e.target.value)}>
          <option value="low,critical,out_of_stock">Needs Reorder (default)</option>
          <option value="">All Statuses</option>
          <option value="out_of_stock">Out of Stock only</option>
          <option value="critical">Critical only</option>
          <option value="low">Low only</option>
          <option value="ok">Healthy only</option>
        </select>
        <select
          style={{ ...inputStyle, width: 200, margin: 0 }}
          value={defaultSupplierId}
          onChange={e => setDefaultSupplierId(e.target.value)}
          title="Default supplier for newly selected items"
        >
          <option value="">— Default Supplier —</option>
          {suppliers.map(s => <option key={s.supplier_id} value={s.supplier_id}>{s.name}</option>)}
        </select>
        <span style={{ marginLeft: 'auto', fontSize: 12, color: 'var(--text-muted)', alignSelf: 'center' }}>
          {filtered.length} products · {selected.size} selected
        </span>
      </div>

      {/* Table */}
      {loading ? (
        <div style={{ padding: 48, textAlign: 'center', color: 'var(--text-muted)' }}>Loading forecasts…</div>
      ) : (
        <div style={{ background: 'var(--bg-secondary)', borderRadius: 12, border: '1px solid var(--border)', overflow: 'hidden' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
            <thead>
              <tr style={{ background: 'var(--bg-tertiary)' }}>
                <th style={{ ...thStyle, width: 36 }}>
                  <input type="checkbox" checked={selected.size === filtered.length && filtered.length > 0} onChange={toggleAll} />
                </th>
                {['SKU', 'Product', 'Status', 'In Stock', 'Days Left', 'ADC', 'Reorder Qty', 'Supplier'].map(h => (
                  <th key={h} style={thStyle}>{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {filtered.map(f => {
                const isSelected = selected.has(f.product_id);
                const line = selected.get(f.product_id);
                const sc = STATUS_CONFIG[f.forecast_status] || STATUS_CONFIG.unconfigured;
                const daysColor = f.days_of_stock <= 7 ? 'var(--danger)' : f.days_of_stock <= 14 ? 'var(--warning)' : 'var(--text-secondary)';
                return (
                  <tr key={f.product_id} style={{ borderTop: '1px solid var(--border)', background: isSelected ? 'rgba(var(--primary-rgb, 99,102,241),0.05)' : undefined }}>
                    <td style={{ ...tdStyle, textAlign: 'center' }}>
                      <input type="checkbox" checked={isSelected} onChange={() => toggleSelect(f)} />
                    </td>
                    <td style={{ ...tdStyle, fontFamily: 'monospace', fontSize: 12 }}>{f.sku}</td>
                    <td style={{ ...tdStyle, maxWidth: 220, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{f.product_name}</td>
                    <td style={tdStyle}>
                      <span style={{ padding: '2px 8px', borderRadius: 12, fontSize: 11, fontWeight: 600, background: sc.bg, color: sc.color }}>{sc.label}</span>
                    </td>
                    <td style={{ ...tdStyle, textAlign: 'center' }}>{f.current_stock}</td>
                    <td style={{ ...tdStyle, textAlign: 'center', fontWeight: 700, color: daysColor }}>
                      {f.days_of_stock === 999 ? '∞' : f.days_of_stock?.toFixed(0) ?? '—'}
                    </td>
                    <td style={{ ...tdStyle, textAlign: 'center', color: 'var(--text-secondary)' }}>{f.calculated_adc?.toFixed(2) || '—'}</td>
                    <td style={{ ...tdStyle, textAlign: 'center' }}>
                      {isSelected ? (
                        <input
                          type="number"
                          min={1}
                          value={line?.qty ?? f.reorder_qty}
                          onChange={e => updateLine(f.product_id, 'qty', parseInt(e.target.value) || 1)}
                          style={{ ...inputStyle, width: 70, margin: 0, padding: '3px 8px', textAlign: 'center' }}
                        />
                      ) : (
                        <span style={{ color: 'var(--text-muted)' }}>{f.reorder_qty || '—'}</span>
                      )}
                    </td>
                    <td style={tdStyle}>
                      {isSelected ? (
                        <select
                          value={line?.supplier_id || ''}
                          onChange={e => updateLine(f.product_id, 'supplier_id', e.target.value)}
                          style={{ ...inputStyle, width: 160, margin: 0, padding: '3px 8px' }}
                        >
                          <option value="">— Select —</option>
                          {suppliers.map(s => <option key={s.supplier_id} value={s.supplier_id}>{s.name}</option>)}
                        </select>
                      ) : (
                        <span style={{ color: 'var(--text-muted)' }}>—</span>
                      )}
                    </td>
                  </tr>
                );
              })}
              {filtered.length === 0 && (
                <tr>
                  <td colSpan={9} style={{ padding: '48px 32px', textAlign: 'center', color: 'var(--text-muted)' }}>
                    {search || statusFilter ? 'No products match your filters.' : 'No items need replenishment. Stock levels are healthy!'}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      )}

      {/* Sticky action bar when items selected */}
      {selected.size > 0 && (
        <div style={{
          position: 'sticky', bottom: 24, margin: '16px 0 0',
          background: 'var(--bg-elevated)', border: '1px solid var(--border)',
          borderRadius: 10, padding: '14px 20px',
          display: 'flex', alignItems: 'center', justifyContent: 'space-between',
          boxShadow: '0 4px 20px rgba(0,0,0,0.4)',
        }}>
          <span style={{ color: 'var(--text-secondary)', fontSize: 14 }}>
            <strong style={{ color: 'var(--text-primary)' }}>{selected.size}</strong> items selected ·{' '}
            Total reorder qty: <strong style={{ color: 'var(--text-primary)' }}>
              {Array.from(selected.values()).reduce((s, l) => s + l.qty, 0)}
            </strong>
          </span>
          <div style={{ display: 'flex', gap: 8 }}>
            <button style={btnGhostStyle} onClick={() => setSelected(new Map())}>Clear selection</button>
            <button style={btnPrimaryStyle} onClick={openWizard}>🛒 Create Purchase Orders</button>
          </div>
        </div>
      )}

      {/* ── Create PO Wizard ── */}
      {showWizard && (
        <div style={overlayStyle}>
          <div style={{ ...modalStyle, width: 620 }}>
            {/* Steps header */}
            <div style={{ padding: '20px 24px 0', borderBottom: '1px solid var(--border)' }}>
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                <h2 style={{ margin: 0, fontSize: 18, fontWeight: 600, color: 'var(--text-primary)' }}>
                  Create Purchase Orders
                </h2>
                <button style={closeBtnStyle} onClick={() => setShowWizard(false)}>✕</button>
              </div>
              <div style={{ display: 'flex', gap: 0, marginTop: 16 }}>
                {['Review', 'Confirm', 'Done'].map((label, i) => (
                  <div key={label} style={{ display: 'flex', alignItems: 'center' }}>
                    <div style={{
                      width: 24, height: 24, borderRadius: '50%', display: 'flex', alignItems: 'center', justifyContent: 'center',
                      background: wizardStep > i + 1 ? 'var(--success)' : wizardStep === i + 1 ? 'var(--primary)' : 'var(--bg-elevated)',
                      color: wizardStep >= i + 1 ? 'white' : 'var(--text-muted)',
                      fontSize: 11, fontWeight: 700, marginBottom: 16,
                    }}>{wizardStep > i + 1 ? '✓' : i + 1}</div>
                    <span style={{ fontSize: 12, color: wizardStep === i + 1 ? 'var(--text-primary)' : 'var(--text-muted)', margin: '0 12px 0 6px', marginBottom: 16 }}>{label}</span>
                    {i < 2 && <div style={{ width: 32, height: 1, background: 'var(--border)', marginBottom: 16, marginRight: 12 }} />}
                  </div>
                ))}
              </div>
            </div>

            <div style={{ padding: '20px 24px', maxHeight: '60vh', overflowY: 'auto' }}>
              {wizardStep === 1 && (
                <>
                  <p style={{ margin: '0 0 16px', color: 'var(--text-secondary)', fontSize: 14 }}>
                    {groupedBySup.size} purchase order(s) will be created, grouped by supplier:
                  </p>
                  {Array.from(groupedBySup.entries()).map(([sid, lines]) => {
                    const unassigned = sid === '__unassigned__';
                    return (
                      <div key={sid} style={{
                        marginBottom: 16, border: `1px solid ${unassigned ? 'var(--danger)' : 'var(--border)'}`,
                        borderRadius: 8, overflow: 'hidden',
                      }}>
                        <div style={{
                          padding: '10px 16px', background: 'var(--bg-elevated)',
                          display: 'flex', alignItems: 'center', justifyContent: 'space-between',
                        }}>
                          <span style={{ fontWeight: 600, color: unassigned ? 'var(--danger)' : 'var(--text-primary)' }}>
                            {unassigned ? '⚠ No supplier assigned' : supplierName(sid)}
                          </span>
                          <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>{lines.length} lines</span>
                        </div>
                        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
                          <tbody>
                            {lines.map(l => (
                              <tr key={l.product_id} style={{ borderTop: '1px solid var(--border)' }}>
                                <td style={{ padding: '8px 16px', fontFamily: 'monospace', fontSize: 12, color: 'var(--text-secondary)' }}>{l.sku}</td>
                                <td style={{ padding: '8px 16px', color: 'var(--text-primary)' }}>{l.product_name}</td>
                                <td style={{ padding: '8px 16px', textAlign: 'right', fontWeight: 600 }}>× {l.qty}</td>
                              </tr>
                            ))}
                          </tbody>
                        </table>
                      </div>
                    );
                  })}
                  {groupedBySup.has('__unassigned__') && (
                    <p style={{ margin: '12px 0 0', fontSize: 13, color: 'var(--danger)' }}>
                      Items without a supplier will be skipped. Go back and assign suppliers before continuing.
                    </p>
                  )}
                </>
              )}

              {wizardStep === 2 && (
                <div style={{ textAlign: 'center', padding: '24px 0' }}>
                  {creating ? (
                    <>
                      <div style={{ fontSize: 40, marginBottom: 16 }}>⏳</div>
                      <p style={{ color: 'var(--text-secondary)' }}>Creating purchase orders…</p>
                    </>
                  ) : (
                    <>
                      <p style={{ color: 'var(--text-secondary)', marginBottom: 20 }}>
                        Ready to create {groupedBySup.size - (groupedBySup.has('__unassigned__') ? 1 : 0)} purchase order(s). This cannot be undone.
                      </p>
                      {wizardError && (
                        <div style={{ padding: '10px 14px', background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 6, color: 'var(--danger)', fontSize: 13, marginBottom: 16 }}>
                          {wizardError}
                        </div>
                      )}
                    </>
                  )}
                </div>
              )}

              {wizardStep === 3 && (
                <>
                  <div style={{ textAlign: 'center', marginBottom: 20 }}>
                    <div style={{ fontSize: 40, marginBottom: 8 }}>✅</div>
                    <h3 style={{ margin: '0 0 4px', color: 'var(--success)' }}>{createdPOs.length} PO{createdPOs.length !== 1 ? 's' : ''} created</h3>
                  </div>
                  {createdPOs.map(po => (
                    <div key={po.po_id} style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '10px 16px', background: 'var(--bg-elevated)', borderRadius: 8, marginBottom: 8 }}>
                      <div>
                        <div style={{ fontWeight: 600, color: 'var(--text-primary)' }}>{po.po_number}</div>
                        <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>{po.supplier} · {po.lines} lines</div>
                      </div>
                      <button
                        style={{ ...btnGhostStyle, fontSize: 12 }}
                        onClick={() => { navigate('/purchase-orders'); setShowWizard(false); }}
                      >
                        View PO →
                      </button>
                    </div>
                  ))}
                </>
              )}
            </div>

            <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, padding: '16px 24px', borderTop: '1px solid var(--border)' }}>
              {wizardStep === 1 && (
                <>
                  <button style={btnGhostStyle} onClick={() => setShowWizard(false)}>Cancel</button>
                  <button style={btnPrimaryStyle} onClick={() => setWizardStep(2)}>
                    Next: Confirm →
                  </button>
                </>
              )}
              {wizardStep === 2 && !creating && createdPOs.length === 0 && (
                <>
                  <button style={btnGhostStyle} onClick={() => setWizardStep(1)}>← Back</button>
                  <button style={btnPrimaryStyle} onClick={createPOs}>Create Purchase Orders</button>
                </>
              )}
              {wizardStep === 3 && (
                <button style={btnPrimaryStyle} onClick={() => { setShowWizard(false); setSelected(new Map()); load(); }}>
                  Done
                </button>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

// ─── Styles ───────────────────────────────────────────────────────────────────
const overlayStyle: React.CSSProperties = { position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.6)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 };
const modalStyle: React.CSSProperties = { background: 'var(--bg-secondary)', borderRadius: 12, border: '1px solid var(--border)', maxWidth: '95vw', maxHeight: '90vh', overflowY: 'auto' };
const closeBtnStyle: React.CSSProperties = { background: 'none', border: 'none', color: 'var(--text-muted)', cursor: 'pointer', fontSize: 18 };
const inputStyle: React.CSSProperties = { width: '100%', padding: '8px 12px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', fontSize: 14, outline: 'none', boxSizing: 'border-box' };
const btnPrimaryStyle: React.CSSProperties = { padding: '8px 16px', background: 'var(--primary)', color: 'white', border: 'none', borderRadius: 6, cursor: 'pointer', fontSize: 13, fontWeight: 600 };
const btnGhostStyle: React.CSSProperties = { padding: '8px 16px', background: 'transparent', color: 'var(--text-secondary)', border: '1px solid var(--border)', borderRadius: 6, cursor: 'pointer', fontSize: 13 };
const thStyle: React.CSSProperties = { padding: '10px 16px', textAlign: 'left', fontSize: 11, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--text-muted)', borderBottom: '1px solid var(--border)' };
const tdStyle: React.CSSProperties = { padding: '10px 16px', color: 'var(--text-primary)' };

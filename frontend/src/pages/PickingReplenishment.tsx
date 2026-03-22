import { useState, useEffect, useCallback } from 'react';
import { getActiveTenantId } from '../contexts/TenantContext';

const API_BASE = import.meta.env.VITE_API_URL ?? 'http://localhost:8080/api/v1';
function api(path: string, init?: RequestInit) {
  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId(), ...init?.headers },
  });
}

interface ReplenishmentItem {
  binrack_id: string;
  binrack_name: string;
  location_id: string;
  location_path: string;
  zone_id: string;
  product_id: string;
  product_name: string;
  sku: string;
  current_fill: number;
  capacity: number;
  fill_percent: number;
  reorder_level: number;
  shortage: number; // how many units short
}

interface StockMoveRequest {
  from_binrack_id: string;
  to_binrack_id: string;
  product_id: string;
  quantity: number;
  notes: string;
}

export default function PickingReplenishment() {
  const [items, setItems] = useState<ReplenishmentItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [search, setSearch] = useState('');
  const [moveModal, setMoveModal] = useState<ReplenishmentItem | null>(null);

  // Move stock form
  const [moveQty, setMoveQty] = useState('');
  const [moveFromBinrack, setMoveFromBinrack] = useState('');
  const [moveNotes, setMoveNotes] = useState('');
  const [moving, setMoving] = useState(false);
  const [moveError, setMoveError] = useState('');
  const [moveSuccess, setMoveSuccess] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const res = await api('/warehouse/replenishment');
      if (res.ok) {
        const d = await res.json();
        setItems(d.items || []);
      } else {
        setError('Failed to load replenishment data');
      }
    } catch {
      setError('Network error');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { load(); }, [load]);

  const filtered = items.filter(item => {
    if (!search) return true;
    const q = search.toLowerCase();
    return item.sku?.toLowerCase().includes(q) ||
      item.product_name?.toLowerCase().includes(q) ||
      item.binrack_name?.toLowerCase().includes(q) ||
      item.location_path?.toLowerCase().includes(q);
  });

  const openMove = (item: ReplenishmentItem) => {
    setMoveModal(item);
    setMoveQty(String(item.shortage || 1));
    setMoveFromBinrack('');
    setMoveNotes('');
    setMoveError('');
    setMoveSuccess(false);
  };

  const handleMove = async () => {
    if (!moveModal) return;
    const qty = parseInt(moveQty);
    if (!qty || qty < 1) { setMoveError('Enter a valid quantity'); return; }

    setMoving(true);
    setMoveError('');
    try {
      const body: StockMoveRequest = {
        from_binrack_id: moveFromBinrack,
        to_binrack_id: moveModal.binrack_id,
        product_id: moveModal.product_id,
        quantity: qty,
        notes: moveNotes || `Replenishment move to pick bin ${moveModal.binrack_name}`,
      };
      const res = await api('/stock/move', { method: 'POST', body: JSON.stringify(body) });
      if (res.ok) {
        setMoveSuccess(true);
        load();
      } else {
        const d = await res.json().catch(() => ({}));
        setMoveError(d.error || 'Move failed');
      }
    } catch {
      setMoveError('Network error');
    } finally {
      setMoving(false);
    }
  };

  const fillColor = (pct: number) => {
    if (pct >= 80) return 'var(--danger)';
    if (pct >= 50) return 'var(--warning)';
    return 'var(--success)';
  };

  const urgencyLabel = (item: ReplenishmentItem) => {
    if (item.current_fill === 0) return { label: 'Empty', color: 'var(--danger)', bg: 'rgba(239,68,68,0.12)' };
    if (item.fill_percent <= 20) return { label: 'Critical', color: 'var(--accent-orange, #f97316)', bg: 'rgba(249,115,22,0.12)' };
    return { label: 'Low', color: 'var(--warning)', bg: 'rgba(245,158,11,0.12)' };
  };

  return (
    <div style={{ padding: '24px 32px', maxWidth: 1200, margin: '0 auto' }}>
      {/* Header */}
      <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', marginBottom: 24 }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 24, fontWeight: 700, color: 'var(--text-primary)' }}>
            Picking Replenishment
          </h1>
          <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 14 }}>
            Pick bins below reorder level. Move stock from replenishment or bulk storage to keep pick faces full.
          </p>
        </div>
        <button style={btnGhostStyle} onClick={load}>↺ Refresh</button>
      </div>

      {error && (
        <div style={{ padding: '10px 14px', background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 6, color: 'var(--danger)', fontSize: 13, marginBottom: 16 }}>
          {error}
        </div>
      )}

      {/* Summary bar */}
      {!loading && (
        <div style={{ display: 'flex', gap: 12, marginBottom: 20 }}>
          {[
            { label: 'Empty bins', count: items.filter(i => i.current_fill === 0).length, color: 'var(--danger)' },
            { label: 'Critical (≤20%)', count: items.filter(i => i.current_fill > 0 && i.fill_percent <= 20).length, color: 'var(--accent-orange, #f97316)' },
            { label: 'Low (≤50%)', count: items.filter(i => i.fill_percent > 20 && i.fill_percent <= 50).length, color: 'var(--warning)' },
          ].map(stat => (
            <div key={stat.label} style={{
              flex: 1, padding: '12px 16px', background: 'var(--bg-secondary)', border: '1px solid var(--border)',
              borderRadius: 8, borderLeft: `3px solid ${stat.color}`,
            }}>
              <div style={{ fontSize: 24, fontWeight: 700, color: stat.color }}>{stat.count}</div>
              <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>{stat.label}</div>
            </div>
          ))}
        </div>
      )}

      {/* Search */}
      <div style={{ marginBottom: 16 }}>
        <input
          style={{ ...inputStyle, maxWidth: 340 }}
          placeholder="Search SKU, product, or bin…"
          value={search}
          onChange={e => setSearch(e.target.value)}
        />
      </div>

      {loading ? (
        <div style={{ padding: 48, textAlign: 'center', color: 'var(--text-muted)' }}>Loading replenishment data…</div>
      ) : filtered.length === 0 ? (
        <div style={{ padding: '64px 32px', textAlign: 'center', background: 'var(--bg-secondary)', borderRadius: 12, border: '1px solid var(--border)' }}>
          <div style={{ fontSize: 48, marginBottom: 12 }}>✅</div>
          <h3 style={{ margin: '0 0 8px', color: 'var(--success)' }}>All pick bins are adequately stocked</h3>
          <p style={{ color: 'var(--text-muted)', margin: 0 }}>No pick bins are below their reorder level.</p>
        </div>
      ) : (
        <div style={{ background: 'var(--bg-secondary)', borderRadius: 12, border: '1px solid var(--border)', overflow: 'hidden' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
            <thead>
              <tr style={{ background: 'var(--bg-tertiary)' }}>
                {['Bin', 'Location', 'SKU / Product', 'Fill Level', 'In Bin', 'Capacity', 'Short by', 'Urgency', ''].map(h => (
                  <th key={h} style={thStyle}>{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {filtered.map(item => {
                const urgency = urgencyLabel(item);
                return (
                  <tr key={`${item.binrack_id}-${item.product_id}`} style={{ borderTop: '1px solid var(--border)' }}>
                    <td style={{ ...tdStyle, fontWeight: 600 }}>{item.binrack_name}</td>
                    <td style={{ ...tdStyle, fontSize: 12, color: 'var(--text-muted)', maxWidth: 160, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                      {item.location_path}
                    </td>
                    <td style={tdStyle}>
                      <div style={{ fontFamily: 'monospace', fontSize: 12, color: 'var(--text-secondary)' }}>{item.sku}</div>
                      <div style={{ fontSize: 13, color: 'var(--text-primary)' }}>{item.product_name}</div>
                    </td>
                    <td style={{ ...tdStyle, width: 120 }}>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                        <div style={{ flex: 1, height: 6, background: 'var(--bg-elevated)', borderRadius: 3, overflow: 'hidden' }}>
                          <div style={{
                            height: '100%', width: `${Math.min(item.fill_percent, 100)}%`,
                            background: fillColor(item.fill_percent), borderRadius: 3, transition: 'width 0.3s',
                          }} />
                        </div>
                        <span style={{ fontSize: 11, color: 'var(--text-muted)', whiteSpace: 'nowrap' }}>
                          {item.fill_percent?.toFixed(0)}%
                        </span>
                      </div>
                    </td>
                    <td style={{ ...tdStyle, textAlign: 'center' }}>{item.current_fill}</td>
                    <td style={{ ...tdStyle, textAlign: 'center', color: 'var(--text-muted)' }}>{item.capacity}</td>
                    <td style={{ ...tdStyle, textAlign: 'center', fontWeight: 600, color: 'var(--danger)' }}>
                      {item.shortage > 0 ? `−${item.shortage}` : '—'}
                    </td>
                    <td style={tdStyle}>
                      <span style={{ padding: '2px 8px', borderRadius: 12, fontSize: 11, fontWeight: 600, background: urgency.bg, color: urgency.color }}>
                        {urgency.label}
                      </span>
                    </td>
                    <td style={{ ...tdStyle, textAlign: 'right' }}>
                      <button style={btnSmallStyle} onClick={() => openMove(item)}>
                        Move Stock →
                      </button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      {/* Move Stock Modal */}
      {moveModal && (
        <div style={overlayStyle}>
          <div style={{ ...modalStyle, width: 460 }}>
            <div style={modalHeaderStyle}>
              <div>
                <h3 style={modalTitleStyle}>Move Stock to Pick Bin</h3>
                <p style={{ margin: '4px 0 0', fontSize: 13, color: 'var(--text-muted)' }}>
                  {moveModal.binrack_name} · {moveModal.sku}
                </p>
              </div>
              <button style={closeBtnStyle} onClick={() => setMoveModal(null)}>✕</button>
            </div>

            {moveSuccess ? (
              <div style={{ padding: '32px 24px', textAlign: 'center' }}>
                <div style={{ fontSize: 40, marginBottom: 12 }}>✅</div>
                <p style={{ color: 'var(--success)', fontWeight: 600, margin: '0 0 8px' }}>Stock moved successfully</p>
                <p style={{ color: 'var(--text-muted)', fontSize: 13, margin: 0 }}>The pick bin has been replenished.</p>
              </div>
            ) : (
              <div style={{ padding: '20px 24px' }}>
                <div style={{ padding: '12px 16px', background: 'var(--bg-elevated)', borderRadius: 8, marginBottom: 16, fontSize: 13 }}>
                  <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 6 }}>
                    <span style={{ color: 'var(--text-muted)' }}>Destination bin:</span>
                    <span style={{ fontWeight: 600 }}>{moveModal.binrack_name}</span>
                  </div>
                  <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 6 }}>
                    <span style={{ color: 'var(--text-muted)' }}>Current fill:</span>
                    <span>{moveModal.current_fill} / {moveModal.capacity}</span>
                  </div>
                  <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                    <span style={{ color: 'var(--text-muted)' }}>Units short:</span>
                    <span style={{ color: 'var(--danger)', fontWeight: 600 }}>{moveModal.shortage}</span>
                  </div>
                </div>

                <div style={fieldStyle}>
                  <label style={labelStyle}>Source bin ID (optional — leave blank for ad-hoc deduct)</label>
                  <input
                    style={inputStyle}
                    placeholder="e.g. bin_bulk_001 or leave blank"
                    value={moveFromBinrack}
                    onChange={e => setMoveFromBinrack(e.target.value)}
                  />
                </div>
                <div style={fieldStyle}>
                  <label style={labelStyle}>Quantity to move <span style={{ color: 'var(--danger)' }}>*</span></label>
                  <input
                    type="number"
                    min={1}
                    style={inputStyle}
                    value={moveQty}
                    onChange={e => setMoveQty(e.target.value)}
                  />
                </div>
                <div style={fieldStyle}>
                  <label style={labelStyle}>Notes</label>
                  <input
                    style={inputStyle}
                    placeholder="Optional notes…"
                    value={moveNotes}
                    onChange={e => setMoveNotes(e.target.value)}
                  />
                </div>
                {moveError && (
                  <p style={{ margin: '12px 0 0', fontSize: 13, color: 'var(--danger)' }}>{moveError}</p>
                )}
              </div>
            )}

            <div style={modalFooterStyle}>
              <button style={btnGhostStyle} onClick={() => setMoveModal(null)}>
                {moveSuccess ? 'Close' : 'Cancel'}
              </button>
              {!moveSuccess && (
                <button style={btnPrimaryStyle} onClick={handleMove} disabled={moving}>
                  {moving ? 'Moving…' : 'Move Stock'}
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
const modalStyle: React.CSSProperties = { background: 'var(--bg-secondary)', borderRadius: 12, border: '1px solid var(--border)', maxWidth: '95vw' };
const modalHeaderStyle: React.CSSProperties = { display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', padding: '18px 24px', borderBottom: '1px solid var(--border)' };
const modalTitleStyle: React.CSSProperties = { margin: 0, fontSize: 17, fontWeight: 600, color: 'var(--text-primary)' };
const modalFooterStyle: React.CSSProperties = { display: 'flex', justifyContent: 'flex-end', gap: 8, padding: '14px 24px', borderTop: '1px solid var(--border)' };
const closeBtnStyle: React.CSSProperties = { background: 'none', border: 'none', color: 'var(--text-muted)', cursor: 'pointer', fontSize: 18 };
const fieldStyle: React.CSSProperties = { marginTop: 14 };
const labelStyle: React.CSSProperties = { display: 'block', marginBottom: 6, fontSize: 13, fontWeight: 500, color: 'var(--text-secondary)' };
const inputStyle: React.CSSProperties = { width: '100%', padding: '8px 12px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', fontSize: 14, outline: 'none', boxSizing: 'border-box' };
const btnPrimaryStyle: React.CSSProperties = { padding: '8px 16px', background: 'var(--primary)', color: 'white', border: 'none', borderRadius: 6, cursor: 'pointer', fontSize: 13, fontWeight: 600 };
const btnGhostStyle: React.CSSProperties = { padding: '8px 16px', background: 'transparent', color: 'var(--text-secondary)', border: '1px solid var(--border)', borderRadius: 6, cursor: 'pointer', fontSize: 13 };
const btnSmallStyle: React.CSSProperties = { padding: '4px 10px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 4, color: 'var(--text-secondary)', cursor: 'pointer', fontSize: 12 };
const thStyle: React.CSSProperties = { padding: '10px 16px', textAlign: 'left', fontSize: 11, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--text-muted)', borderBottom: '1px solid var(--border)' };
const tdStyle: React.CSSProperties = { padding: '10px 16px', color: 'var(--text-primary)' };

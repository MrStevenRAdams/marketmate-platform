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

// ─── Types ───────────────────────────────────────────────────────────────────

interface ScrapRecord {
  scrap_id: string;
  product_id: string;
  sku: string;
  product_name: string;
  location_id: string;
  location_name: string;
  quantity: number;
  reason: string;
  notes?: string;
  scrap_value: number;
  currency: string;
  scrapped_by: string;
  created_at: string;
}

interface Location { location_id: string; name: string; path: string; is_leaf: boolean; }
interface Product { product_id: string; title: string; sku: string; }

const REASONS = [
  { value: 'damaged',      label: '⚠️  Damaged' },
  { value: 'expired',      label: '⏰ Expired' },
  { value: 'quality_fail', label: '❌ Quality Failure' },
  { value: 'lost',         label: '🔍 Lost / Missing' },
  { value: 'contaminated', label: '☢️  Contaminated' },
  { value: 'obsolete',     label: '📦 Obsolete' },
  { value: 'other',        label: '📝 Other' },
];

const REASON_COLORS: Record<string, string> = {
  damaged: 'var(--warning)',
  expired: 'var(--accent-orange)',
  quality_fail: 'var(--danger)',
  lost: 'var(--text-muted)',
  contaminated: 'var(--accent-purple)',
  obsolete: 'var(--text-muted)',
  other: 'var(--text-secondary)',
};

// ─── Scrap Modal ──────────────────────────────────────────────────────────────

function ScrapModal({
  locations,
  onClose,
  onScrapped,
}: {
  locations: Location[];
  onClose: () => void;
  onScrapped: () => void;
}) {
  const [search, setSearch] = useState('');
  const [products, setProducts] = useState<Product[]>([]);
  const [selectedProduct, setSelectedProduct] = useState<Product | null>(null);
  const [locationId, setLocationId] = useState('');
  const [quantity, setQuantity] = useState(1);
  const [reason, setReason] = useState('');
  const [notes, setNotes] = useState('');
  const [scrapValue, setScrapValue] = useState(0);
  const [currency, setCurrency] = useState('GBP');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [searching, setSearching] = useState(false);
  const [serialNumbers, setSerialNumbers] = useState<string[]>([]);
  const { isSerial: productIsSerial } = useIsSerialProduct(selectedProduct?.product_id ?? null);

  // When product changes, reset serial numbers
  useEffect(() => { setSerialNumbers([]); }, [selectedProduct?.product_id]);

  useEffect(() => {
    if (!search.trim() || search.length < 2) { setProducts([]); return; }
    const timer = setTimeout(async () => {
      setSearching(true);
      try {
        const res = await api(`/products?search=${encodeURIComponent(search)}&limit=20`);
        if (res.ok) {
          const d = await res.json();
          setProducts(d.data || []);
        }
      } finally { setSearching(false); }
    }, 300);
    return () => clearTimeout(timer);
  }, [search]);

  const save = async () => {
    if (!selectedProduct) { setError('Select a product'); return; }
    if (!locationId) { setError('Select a location'); return; }
    if (!reason) { setError('Select a reason'); return; }
    if (quantity < 1) { setError('Quantity must be at least 1'); return; }
    if (productIsSerial && serialNumbers.length < quantity) {
      setError(`Serial numbers required: enter ${quantity - serialNumbers.length} more serial number${quantity - serialNumbers.length > 1 ? 's' : ''}`);
      return;
    }
    setSaving(true); setError('');
    try {
      const res = await api('/stock-scraps', {
        method: 'POST',
        body: JSON.stringify({
          product_id: selectedProduct.product_id,
          location_id: locationId,
          quantity,
          reason,
          notes: notes || undefined,
          scrap_value: scrapValue || undefined,
          currency,
          serial_numbers: productIsSerial ? serialNumbers : undefined,
        }),
      });
      const d = await res.json();
      if (!res.ok) throw new Error(d.error || 'Failed to scrap');
      if (d.warning) alert(d.warning);
      onScrapped();
    } catch (e: any) {
      setError(e.message); setSaving(false);
    }
  };

  return (
    <div style={overlayStyle} onClick={onClose}>
      <div style={{ ...modalStyle, width: 520 }} onClick={e => e.stopPropagation()}>
        <div style={modalHeaderStyle}>
          <h2 style={{ margin: 0, fontSize: 18, fontWeight: 600, color: 'var(--text-primary)' }}>Scrap Stock</h2>
          <button style={closeButtonStyle} onClick={onClose}>✕</button>
        </div>
        {error && <div style={errorStyle}>{error}</div>}
        <div style={{ padding: '0 24px 8px' }}>
          {/* Product search */}
          <div style={fieldStyle}>
            <label style={labelStyle}>Product <span style={{ color: 'var(--danger)' }}>*</span></label>
            {selectedProduct ? (
              <div style={{
                display: 'flex', alignItems: 'center', justifyContent: 'space-between',
                padding: '10px 14px', background: 'var(--bg-elevated)', borderRadius: 6,
                border: '1px solid var(--primary)',
              }}>
                <div>
                  <div style={{ fontWeight: 600, fontSize: 13, display: 'flex', alignItems: 'center', gap: 6 }}>
                    {selectedProduct.title}
                    {productIsSerial && <SerialRequiredBadge />}
                  </div>
                  <div style={{ fontSize: 12, color: 'var(--accent-cyan)', fontFamily: 'monospace' }}>{selectedProduct.sku}</div>
                </div>
                <button
                  style={{ background: 'none', border: 'none', color: 'var(--text-muted)', cursor: 'pointer', fontSize: 16 }}
                  onClick={() => { setSelectedProduct(null); setSearch(''); }}
                >✕</button>
              </div>
            ) : (
              <>
                <input
                  style={inputStyle}
                  placeholder="Search by SKU or product name…"
                  value={search}
                  onChange={e => setSearch(e.target.value)}
                />
                {searching && <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 4 }}>Searching…</div>}
                {products.length > 0 && (
                  <div style={{
                    background: 'var(--bg-elevated)', border: '1px solid var(--border)',
                    borderRadius: 6, marginTop: 4, maxHeight: 180, overflowY: 'auto',
                  }}>
                    {products.map(p => (
                      <div
                        key={p.product_id}
                        onClick={() => { setSelectedProduct(p); setProducts([]); setSearch(''); }}
                        style={{
                          padding: '8px 12px', cursor: 'pointer', borderBottom: '1px solid var(--border)',
                          display: 'flex', alignItems: 'center', gap: 10,
                        }}
                        onMouseEnter={e => (e.currentTarget.style.background = 'var(--bg-tertiary)')}
                        onMouseLeave={e => (e.currentTarget.style.background = 'transparent')}
                      >
                        <code style={{ fontSize: 11, color: 'var(--accent-cyan)', minWidth: 80 }}>{p.sku}</code>
                        <span style={{ fontSize: 13, color: 'var(--text-primary)' }}>{p.title}</span>
                      </div>
                    ))}
                  </div>
                )}
              </>
            )}
          </div>

          {/* Location */}
          <div style={fieldStyle}>
            <label style={labelStyle}>Location <span style={{ color: 'var(--danger)' }}>*</span></label>
            <select style={inputStyle} value={locationId} onChange={e => setLocationId(e.target.value)}>
              <option value="">— Select location —</option>
              {locations.filter(l => l.is_leaf).map(l => (
                <option key={l.location_id} value={l.location_id}>{l.path || l.name}</option>
              ))}
            </select>
          </div>

          {/* Reason */}
          {productIsSerial && (
            <div style={fieldStyle}>
              <label style={labelStyle}>
                Serial Numbers <span style={{ color: 'var(--danger)' }}>*</span>
                <span style={{ marginLeft: 8, fontSize: 11, fontWeight: 400, color: 'var(--text-muted)' }}>
                  — {quantity} required
                </span>
              </label>
              <SerialNumberInput
                productId={selectedProduct?.product_id}
                quantity={quantity}
                value={serialNumbers}
                onChange={setSerialNumbers}
              />
            </div>
          )}

          <div style={fieldStyle}>
            <label style={labelStyle}>Reason <span style={{ color: 'var(--danger)' }}>*</span></label>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 }}>
              {REASONS.map(r => (
                <button
                  key={r.value}
                  type="button"
                  onClick={() => setReason(r.value)}
                  style={{
                    padding: '8px 12px', borderRadius: 6, border: '1px solid',
                    borderColor: reason === r.value ? 'var(--primary)' : 'var(--border)',
                    background: reason === r.value ? 'rgba(59,130,246,0.12)' : 'var(--bg-elevated)',
                    color: reason === r.value ? 'var(--primary)' : 'var(--text-secondary)',
                    cursor: 'pointer', fontSize: 12, textAlign: 'left', fontWeight: reason === r.value ? 600 : 400,
                  }}
                >
                  {r.label}
                </button>
              ))}
            </div>
          </div>

          {/* Qty + Value row */}
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 80px', gap: 12, marginTop: 16 }}>
            <div>
              <label style={labelStyle}>Quantity <span style={{ color: 'var(--danger)' }}>*</span></label>
              <input
                type="number" min="1" style={inputStyle} value={quantity}
                onChange={e => setQuantity(parseInt(e.target.value) || 1)}
              />
            </div>
            <div>
              <label style={labelStyle}>Scrap Value (per unit)</label>
              <input
                type="number" min="0" step="0.01" style={inputStyle} value={scrapValue}
                onChange={e => setScrapValue(parseFloat(e.target.value) || 0)}
              />
            </div>
            <div>
              <label style={labelStyle}>Currency</label>
              <select style={inputStyle} value={currency} onChange={e => setCurrency(e.target.value)}>
                {['GBP','USD','EUR'].map(c => <option key={c}>{c}</option>)}
              </select>
            </div>
          </div>

          {/* Notes */}
          <div style={fieldStyle}>
            <label style={labelStyle}>Notes</label>
            <textarea
              style={{ ...inputStyle, height: 64, resize: 'vertical', fontFamily: 'inherit' }}
              value={notes}
              onChange={e => setNotes(e.target.value)}
              placeholder="Optional additional details…"
            />
          </div>
        </div>

        <div style={modalFooterStyle}>
          <button style={btnGhostStyle} onClick={onClose} disabled={saving}>Cancel</button>
          <button
            style={{ ...btnPrimaryStyle, background: 'var(--danger)' }}
            onClick={save} disabled={saving}
          >
            {saving ? 'Scrapping…' : `Scrap ${quantity > 1 ? `${quantity} Units` : 'Item'}`}
          </button>
        </div>
      </div>
    </div>
  );
}

// ─── Main Page ────────────────────────────────────────────────────────────────

export default function StockScrap() {
  const [scraps, setScraps] = useState<ScrapRecord[]>([]);
  const [locations, setLocations] = useState<Location[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [showScrap, setShowScrap] = useState(false);
  const [stats, setStats] = useState<{ total_quantity_scrapped: number; total_scrap_value: number; by_reason: Record<string, number> } | null>(null);
  const [reasonFilter, setReasonFilter] = useState('');
  const [search, setSearch] = useState('');

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const qs = reasonFilter ? `?reason=${reasonFilter}` : '';
      const [scrapRes, locRes, statsRes] = await Promise.all([
        api(`/stock-scraps${qs}`),
        api('/locations'),
        api('/stock-scraps/stats'),
      ]);
      if (scrapRes.ok) { const d = await scrapRes.json(); setScraps(d.scraps || []); }
      if (locRes.ok) { const d = await locRes.json(); setLocations(d.locations || []); }
      if (statsRes.ok) { const d = await statsRes.json(); setStats(d); }
    } catch (e: any) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }, [reasonFilter]);

  useEffect(() => { load(); }, [load]);

  const filtered = scraps.filter(s => {
    if (!search) return true;
    return s.sku.toLowerCase().includes(search.toLowerCase()) ||
           s.product_name.toLowerCase().includes(search.toLowerCase());
  });

  const fmtMoney = (v: number, cur: string) => {
    try { return new Intl.NumberFormat('en-GB', { style: 'currency', currency: cur || 'GBP' }).format(v); }
    catch { return `${cur}${v.toFixed(2)}`; }
  };

  return (
    <div style={{ padding: '24px 32px', maxWidth: 1200, margin: '0 auto' }}>
      <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', marginBottom: 24 }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 24, fontWeight: 700, color: 'var(--text-primary)' }}>Scrap History</h1>
          <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 14 }}>
            Record and track scrapped or written-off inventory items.
          </p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <button style={btnGhostStyle} onClick={load}>↻ Refresh</button>
          <button style={{ ...btnPrimaryStyle, background: 'var(--danger)' }} onClick={() => setShowScrap(true)}>
            + Scrap Items
          </button>
        </div>
      </div>

      {/* Stats bar */}
      {stats && (
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 16, marginBottom: 24 }}>
          {[
            { label: 'Total Units Scrapped', value: stats.total_quantity_scrapped.toLocaleString(), icon: '📦' },
            { label: 'Total Scrap Value', value: fmtMoney(stats.total_scrap_value, 'GBP'), icon: '💰' },
            {
              label: 'Top Reason',
              value: stats.by_reason
                ? (Object.entries(stats.by_reason).sort((a, b) => b[1] - a[1])[0]?.[0] || '—')
                : '—',
              icon: '📊',
            },
          ].map(card => (
            <div key={card.label} style={{
              padding: '16px 20px', background: 'var(--bg-secondary)',
              border: '1px solid var(--border)', borderRadius: 10,
            }}>
              <div style={{ fontSize: 22, marginBottom: 4 }}>{card.icon}</div>
              <div style={{ fontSize: 20, fontWeight: 700, color: 'var(--text-primary)' }}>{card.value}</div>
              <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>{card.label}</div>
            </div>
          ))}
        </div>
      )}

      {/* Filters */}
      <div style={{ display: 'flex', gap: 12, marginBottom: 20, alignItems: 'center' }}>
        <input
          style={{ ...inputStyle, width: 240, margin: 0 }}
          placeholder="Search SKU or product…"
          value={search}
          onChange={e => setSearch(e.target.value)}
        />
        <select style={{ ...inputStyle, width: 180, margin: 0 }} value={reasonFilter} onChange={e => setReasonFilter(e.target.value)}>
          <option value="">All Reasons</option>
          {REASONS.map(r => <option key={r.value} value={r.value}>{r.label}</option>)}
        </select>
        {(search || reasonFilter) && (
          <button style={btnGhostStyle} onClick={() => { setSearch(''); setReasonFilter(''); }}>
            Clear
          </button>
        )}
      </div>

      {error && <div style={errorStyle}>{error}</div>}

      {loading ? (
        <div style={{ padding: '48px', textAlign: 'center', color: 'var(--text-muted)' }}>Loading…</div>
      ) : filtered.length === 0 ? (
        <div style={{
          padding: '64px 32px', textAlign: 'center',
          background: 'var(--bg-secondary)', borderRadius: 12, border: '1px solid var(--border)',
        }}>
          <div style={{ fontSize: 48, marginBottom: 16 }}>🗑️</div>
          <h3 style={{ margin: '0 0 8px', color: 'var(--text-primary)' }}>
            {search || reasonFilter ? 'No records match your filters' : 'No scrapped items yet'}
          </h3>
          <p style={{ color: 'var(--text-muted)', marginBottom: 24 }}>
            Use the "Scrap Items" button to record written-off or damaged stock.
          </p>
        </div>
      ) : (
        <div style={{ background: 'var(--bg-secondary)', borderRadius: 12, border: '1px solid var(--border)', overflow: 'hidden' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
            <thead>
              <tr style={{ background: 'var(--bg-tertiary)' }}>
                {['SKU', 'Product', 'Location', 'Qty', 'Reason', 'Scrap Value', 'Date', 'By'].map(h => (
                  <th key={h} style={thStyle}>{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {filtered.map(s => (
                <tr key={s.scrap_id} style={{ borderBottom: '1px solid var(--border)' }}>
                  <td style={{ ...tdStyle, fontFamily: 'monospace', color: 'var(--accent-cyan)' }}>{s.sku}</td>
                  <td style={{ ...tdStyle, color: 'var(--text-secondary)', maxWidth: 200 }}>
                    <div style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{s.product_name}</div>
                  </td>
                  <td style={{ ...tdStyle, color: 'var(--text-muted)', fontSize: 12 }}>{s.location_name}</td>
                  <td style={{ ...tdStyle, fontWeight: 700, color: 'var(--danger)', textAlign: 'center' }}>
                    -{s.quantity}
                  </td>
                  <td style={tdStyle}>
                    <span style={{
                      padding: '2px 8px', borderRadius: 12, fontSize: 11, fontWeight: 600,
                      background: `${REASON_COLORS[s.reason] || 'var(--text-muted)'}20`,
                      color: REASON_COLORS[s.reason] || 'var(--text-muted)',
                    }}>
                      {REASONS.find(r => r.value === s.reason)?.label.replace(/^[^\w]+/, '') || s.reason}
                    </span>
                  </td>
                  <td style={tdStyle}>
                    {s.scrap_value > 0 ? fmtMoney(s.scrap_value * s.quantity, s.currency) : '—'}
                  </td>
                  <td style={{ ...tdStyle, color: 'var(--text-muted)', fontSize: 12 }}>
                    {new Date(s.created_at).toLocaleDateString('en-GB', { day: '2-digit', month: 'short', year: 'numeric' })}
                  </td>
                  <td style={{ ...tdStyle, color: 'var(--text-muted)', fontSize: 12 }}>
                    {s.scrapped_by || '—'}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {showScrap && (
        <ScrapModal
          locations={locations}
          onClose={() => setShowScrap(false)}
          onScrapped={() => { setShowScrap(false); load(); }}
        />
      )}
    </div>
  );
}

// ─── Styles ─────────────────────────────────────────────────────────────────
const overlayStyle: React.CSSProperties = { position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.6)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 };
const modalStyle: React.CSSProperties = { background: 'var(--bg-secondary)', borderRadius: 12, border: '1px solid var(--border)', maxWidth: '95vw', maxHeight: '90vh', overflowY: 'auto' };
const modalHeaderStyle: React.CSSProperties = { display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '20px 24px 16px', borderBottom: '1px solid var(--border)' };
const modalFooterStyle: React.CSSProperties = { display: 'flex', justifyContent: 'flex-end', gap: 8, padding: '16px 24px', borderTop: '1px solid var(--border)' };
const closeButtonStyle: React.CSSProperties = { background: 'none', border: 'none', color: 'var(--text-muted)', cursor: 'pointer', fontSize: 18 };
const fieldStyle: React.CSSProperties = { marginTop: 16 };
const labelStyle: React.CSSProperties = { display: 'block', marginBottom: 6, fontSize: 13, fontWeight: 500, color: 'var(--text-secondary)' };
const inputStyle: React.CSSProperties = { width: '100%', padding: '8px 12px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', fontSize: 14, outline: 'none', boxSizing: 'border-box' };
const errorStyle: React.CSSProperties = { margin: '0 24px 16px', padding: '10px 14px', background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 6, color: 'var(--danger)', fontSize: 13 };
const btnPrimaryStyle: React.CSSProperties = { padding: '8px 16px', background: 'var(--primary)', color: 'white', border: 'none', borderRadius: 6, cursor: 'pointer', fontSize: 13, fontWeight: 600 };
const btnGhostStyle: React.CSSProperties = { padding: '8px 16px', background: 'transparent', color: 'var(--text-secondary)', border: '1px solid var(--border)', borderRadius: 6, cursor: 'pointer', fontSize: 13 };
const thStyle: React.CSSProperties = { padding: '10px 16px', textAlign: 'left', fontSize: 11, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--text-muted)', borderBottom: '1px solid var(--border)' };
const tdStyle: React.CSSProperties = { padding: '12px 16px', color: 'var(--text-primary)' };

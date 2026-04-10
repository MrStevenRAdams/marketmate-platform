// ============================================================================
// ORDER SEEDER — Dev Tools
// ============================================================================
// Generates realistic dummy orders from real PIM products for courier label
// testing. Accessible at /dev/orders/seed.
// ============================================================================

import { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { getActiveTenantId } from '../contexts/TenantContext';
import { auth } from '../contexts/AuthContext';

const API = (import.meta.env.VITE_API_URL ?? 'http://localhost:8080/api/v1');

async function apiHeaders() {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    'X-Tenant-Id': getActiveTenantId(),
  };
  try {
    const user = auth.currentUser;
    if (user) {
      const token = await user.getIdToken();
      headers['Authorization'] = `Bearer ${token}`;
    }
  } catch { /* non-fatal */ }
  return headers;
}

// ── Types ──────────────────────────────────────────────────────────────────

interface Product {
  product_id: string;
  sku: string;
  title: string;
  status: string;
  weight?: { value: number; unit: string };
  identifiers?: { ean?: string };
}

interface SeedResult {
  ok: boolean;
  created: number;
  order_ids: string[];
  errors?: string[];
}

// ── Address presets ────────────────────────────────────────────────────────

const POSTCODE_PRESETS = [
  // ── Domestic — standard zones ──
  { group: '🇬🇧 UK Standard', label: 'London Central (EC1V)', postcode: 'EC1V 7DU', country: 'GB', note: '' },
  { group: '🇬🇧 UK Standard', label: 'Leeds (LS1)', postcode: 'LS1 5AE', country: 'GB', note: '' },
  { group: '🇬🇧 UK Standard', label: 'Manchester (M1)', postcode: 'M1 4JX', country: 'GB', note: '' },
  { group: '🇬🇧 UK Standard', label: 'Birmingham (B3)', postcode: 'B3 2BB', country: 'GB', note: '' },
  { group: '🇬🇧 UK Standard', label: 'Edinburgh (EH2)', postcode: 'EH2 2AN', country: 'GB', note: '' },
  { group: '🇬🇧 UK Standard', label: 'Cardiff (CF10)', postcode: 'CF10 2EU', country: 'GB', note: '' },
  { group: '🇬🇧 UK Standard', label: 'Belfast (BT1)', postcode: 'BT1 5EA', country: 'GB', note: '' },
  // ── Domestic — surcharge / restricted zones ──
  { group: '⚠️ UK Surcharge Zones', label: 'Inverness / Highlands (IV1)', postcode: 'IV1 1HH', country: 'GB', note: 'Carriers often charge Highland surcharge' },
  { group: '⚠️ UK Surcharge Zones', label: 'Shetland (ZE1)', postcode: 'ZE1 0AA', country: 'GB', note: 'Shetland — many carriers refuse or surcharge' },
  { group: '⚠️ UK Surcharge Zones', label: 'Stornoway / Outer Hebrides (HS1)', postcode: 'HS1 2AA', country: 'GB', note: 'Outer Hebrides — island surcharge zone' },
  { group: '⚠️ UK Surcharge Zones', label: 'Orkney (KW15)', postcode: 'KW15 1AA', country: 'GB', note: 'Orkney islands — restricted for many carriers' },
  // ── Custom/overseas territories ──
  { group: '🔀 Customs Edge Cases', label: 'Jersey (JE2)', postcode: 'JE2 4WB', country: 'JE', note: 'Outside UK customs territory — needs customs declaration' },
  { group: '🔀 Customs Edge Cases', label: 'Isle of Man (IM1)', postcode: 'IM1 1EF', country: 'IM', note: 'Outside UK customs territory — needs customs declaration' },
  { group: '🔀 Customs Edge Cases', label: 'Guernsey (GY1)', postcode: 'GY1 1WH', country: 'GG', note: 'Outside UK customs territory — needs customs declaration' },
  { group: '🔀 Customs Edge Cases', label: 'BFPO (OX18)', postcode: 'OX18 3LX', country: 'GB', note: 'British Forces — some carriers refuse' },
  // ── EU ──
  { group: '🇪🇺 EU (IOSS threshold applies)', label: 'Germany — Hamburg', postcode: '20095', country: 'DE', note: 'IOSS applies for <€150; full customs otherwise' },
  { group: '🇪🇺 EU (IOSS threshold applies)', label: 'France — Paris', postcode: '75001', country: 'FR', note: '' },
  { group: '🇪🇺 EU (IOSS threshold applies)', label: 'Netherlands — Amsterdam', postcode: '1015 CS', country: 'NL', note: '' },
  { group: '🇪🇺 EU (IOSS threshold applies)', label: 'Italy — Milan', postcode: '20121', country: 'IT', note: '' },
  { group: '🇪🇺 EU (IOSS threshold applies)', label: 'Spain — Madrid', postcode: '28013', country: 'ES', note: '' },
  { group: '🇪🇺 EU (IOSS threshold applies)', label: 'Poland — Warsaw', postcode: '00-029', country: 'PL', note: '' },
  { group: '🇪🇺 EU (IOSS threshold applies)', label: 'Ireland — Dublin', postcode: 'D01 F5P2', country: 'IE', note: 'Republic of Ireland — EU customs rules' },
  // ── Rest of world ──
  { group: '🌍 Rest of World', label: 'USA — New York', postcode: '10118', country: 'US', note: 'Full customs + commercial invoice required' },
  { group: '🌍 Rest of World', label: 'Canada — Montreal', postcode: 'H2X 1Z3', country: 'CA', note: '' },
  { group: '🌍 Rest of World', label: 'Australia — Sydney', postcode: '2000', country: 'AU', note: '' },
  { group: '🌍 Rest of World', label: 'Japan — Tokyo', postcode: '160-0022', country: 'JP', note: '' },
  { group: '🌍 Rest of World', label: 'China — Shanghai', postcode: '200001', country: 'CN', note: 'DDP often not supported by all carriers' },
  { group: '🌍 Rest of World', label: 'India — Bangalore', postcode: '560001', country: 'IN', note: '' },
  { group: '🌍 Rest of World', label: 'Saudi Arabia — Riyadh', postcode: '12271', country: 'SA', note: '' },
  { group: '🌍 Rest of World', label: 'South Africa — Cape Town', postcode: '8001', country: 'ZA', note: '' },
  { group: '🌍 Rest of World', label: 'Brazil — São Paulo', postcode: '01310-100', country: 'BR', note: 'High import duties — good for testing DDP vs DDU' },
];

const CHANNELS = ['mixed', 'amazon', 'ebay', 'shopify', 'shopline', 'temu', 'etsy', 'woocommerce', 'tiktok'];
const STATUSES = ['processing', 'ready_to_fulfil', 'on_hold', 'imported'];

// Group presets for the dropdown
const presetGroups = Array.from(new Set(POSTCODE_PRESETS.map(p => p.group)));

// ── Styles ─────────────────────────────────────────────────────────────────

const TEAL = '#00b8d4';
const s = {
  label: { fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase' as const, letterSpacing: '0.06em', display: 'block', marginBottom: 5 },
  input: { width: '100%', padding: '8px 11px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', fontSize: 14, boxSizing: 'border-box' as const },
  card: { background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 10, padding: '18px 20px', marginBottom: 14 },
  row: { display: 'flex', gap: 12, flexWrap: 'wrap' as const },
  chip: (active: boolean, color = TEAL) => ({
    padding: '5px 12px', borderRadius: 20, fontSize: 13, cursor: 'pointer', fontWeight: 600, userSelect: 'none' as const, border: `1px solid ${active ? color : 'var(--border)'}`, background: active ? `${color}18` : 'transparent', color: active ? color : 'var(--text-muted)',
  }),
};

// ── Component ──────────────────────────────────────────────────────────────

export default function OrderSeeder() {
  const navigate = useNavigate();

  // Config state
  const [count,          setCount]          = useState(5);
  const [linesPerOrder,  setLinesPerOrder]  = useState(2);
  const [destination,    setDestination]    = useState<'domestic'|'international'|'mixed'|'custom'>('domestic');
  const [channel,        setChannel]        = useState('mixed');
  const [status,         setStatus]         = useState('processing');
  const [tag,            setTag]            = useState('LABEL-TEST');
  const [customPostcode, setCustomPostcode] = useState('');
  const [customCountry,  setCustomCountry]  = useState('GB');
  const [selectedPreset, setSelectedPreset] = useState('');
  const [selectedProductIDs, setSelectedProductIDs] = useState<string[]>([]);

  // Products from PIM
  const [products,        setProducts]        = useState<Product[]>([]);
  const [productsLoading, setProductsLoading] = useState(false);
  const [productSearch,   setProductSearch]   = useState('');

  // Execution
  const [running, setRunning] = useState(false);
  const [result,  setResult]  = useState<SeedResult | null>(null);
  const [error,   setError]   = useState('');

  // Load products on mount
  useEffect(() => {
    loadProducts();
  }, []);

  async function loadProducts() {
    setProductsLoading(true);
    try {
      const h = await apiHeaders();
      const res = await fetch(`${API}/products?page_size=100&status=active`, { headers: h });
      if (res.ok) {
        const d = await res.json();
        setProducts(d.data || []);
      }
    } catch { /* non-fatal */ }
    finally { setProductsLoading(false); }
  }

  // Apply preset
  function applyPreset(presetLabel: string) {
    const preset = POSTCODE_PRESETS.find(p => p.label === presetLabel);
    if (!preset) return;
    setSelectedPreset(presetLabel);
    setCustomPostcode(preset.postcode);
    setCustomCountry(preset.country);
    setDestination('custom');
  }

  // Toggle product selection
  function toggleProduct(id: string) {
    setSelectedProductIDs(prev =>
      prev.includes(id) ? prev.filter(x => x !== id) : [...prev, id]
    );
  }

  // Run seed
  async function runSeed() {
    setRunning(true);
    setResult(null);
    setError('');
    try {
      const h = await apiHeaders();
      const body: Record<string, unknown> = {
        count: count,
        lines_per_order: linesPerOrder,
        channel: channel === 'mixed' ? '' : channel,
        status,
        tag,
      };

      if (destination === 'custom') {
        body.postcode = customPostcode;
        body.country  = customCountry;
      } else {
        body.destination = destination;
      }

      if (selectedProductIDs.length > 0) {
        body.product_ids = selectedProductIDs;
      }

      const res = await fetch(`${API}/dev/orders/seed`, {
        method: 'POST',
        headers: h,
        body: JSON.stringify(body),
      });
      const data: SeedResult = await res.json();
      setResult(data);
    } catch (e: any) {
      setError(e.message || 'Network error');
    } finally {
      setRunning(false);
    }
  }

  const filteredProducts = products.filter(p =>
    productSearch === '' ||
    p.title.toLowerCase().includes(productSearch.toLowerCase()) ||
    p.sku.toLowerCase().includes(productSearch.toLowerCase())
  );

  const selectedPresetObj = POSTCODE_PRESETS.find(p => p.label === selectedPreset);

  return (
    <div className="page" style={{ maxWidth: 860 }}>

      {/* Header */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 24 }}>
        <button className="btn btn-secondary" onClick={() => navigate(-1)} style={{ padding: '8px 14px' }}>← Back</button>
        <div>
          <h1 className="page-title">Order Seeder</h1>
          <p className="page-subtitle">Generate realistic dummy orders from your PIM products for courier label testing</p>
        </div>
      </div>

      {/* ── Volume ── */}
      <div style={s.card}>
        <div style={{ fontWeight: 700, fontSize: 14, marginBottom: 14, display: 'flex', alignItems: 'center', gap: 8 }}>
          <span style={{ fontSize: 18 }}>📦</span> Volume
        </div>
        <div style={{ ...s.row, alignItems: 'flex-end' }}>
          <div style={{ flex: 1, minWidth: 140 }}>
            <label style={s.label}>Number of orders</label>
            <input type="number" min={1} max={50} value={count} onChange={e => setCount(Math.min(50, Math.max(1, +e.target.value)))} style={s.input} />
            <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 4 }}>Max 50 per run</div>
          </div>
          <div style={{ flex: 1, minWidth: 140 }}>
            <label style={s.label}>Line items per order</label>
            <input type="number" min={1} max={10} value={linesPerOrder} onChange={e => setLinesPerOrder(Math.min(10, Math.max(1, +e.target.value)))} style={s.input} />
            <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 4 }}>Products picked randomly from PIM</div>
          </div>
          <div style={{ flex: 1, minWidth: 140 }}>
            <label style={s.label}>Tag (applied to all)</label>
            <input type="text" value={tag} onChange={e => setTag(e.target.value)} style={s.input} placeholder="LABEL-TEST" />
          </div>
        </div>
      </div>

      {/* ── Destination ── */}
      <div style={s.card}>
        <div style={{ fontWeight: 700, fontSize: 14, marginBottom: 14, display: 'flex', alignItems: 'center', gap: 8 }}>
          <span style={{ fontSize: 18 }}>📍</span> Destination
        </div>

        {/* Mode selector */}
        <div style={{ ...s.row, marginBottom: 16 }}>
          {(['domestic', 'international', 'mixed', 'custom'] as const).map(d => (
            <button key={d} onClick={() => setDestination(d)} style={s.chip(destination === d)}>
              {d === 'domestic' ? '🇬🇧 Domestic' : d === 'international' ? '🌍 International' : d === 'mixed' ? '🔀 Mixed' : '🎯 Custom / Preset'}
            </button>
          ))}
        </div>

        {destination === 'domestic' && (
          <div style={{ padding: '10px 14px', background: 'rgba(0,184,212,0.06)', borderRadius: 8, fontSize: 13, color: 'var(--text-secondary)' }}>
            Uses a pool of 16 UK addresses including standard zones, Scottish Highlands, islands and BFPO. Good for testing surcharge detection and carrier eligibility.
          </div>
        )}

        {destination === 'international' && (
          <div style={{ padding: '10px 14px', background: 'rgba(0,184,212,0.06)', borderRadius: 8, fontSize: 13, color: 'var(--text-secondary)' }}>
            Uses a pool of 14 international addresses across EU (IOSS zone), USA, Canada, Japan, China, India, Australia and more. All orders will need customs declarations.
          </div>
        )}

        {destination === 'mixed' && (
          <div style={{ padding: '10px 14px', background: 'rgba(0,184,212,0.06)', borderRadius: 8, fontSize: 13, color: 'var(--text-secondary)' }}>
            50/50 split of domestic and international addresses. Good for testing label routing logic across a batch.
          </div>
        )}

        {destination === 'custom' && (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
            {/* Preset picker */}
            <div>
              <label style={s.label}>Preset addresses <span style={{ fontWeight: 400, textTransform: 'none' }}>— pick a known edge-case</span></label>
              <select
                value={selectedPreset}
                onChange={e => applyPreset(e.target.value)}
                style={s.input}
              >
                <option value="">— select a preset or enter manually below —</option>
                {presetGroups.map(group => (
                  <optgroup key={group} label={group}>
                    {POSTCODE_PRESETS.filter(p => p.group === group).map(p => (
                      <option key={p.label} value={p.label}>{p.label}</option>
                    ))}
                  </optgroup>
                ))}
              </select>
              {selectedPresetObj?.note && (
                <div style={{ marginTop: 6, padding: '6px 10px', background: 'rgba(251,191,36,0.1)', border: '1px solid rgba(251,191,36,0.3)', borderRadius: 6, fontSize: 12, color: '#fbbf24' }}>
                  ⚠ {selectedPresetObj.note}
                </div>
              )}
            </div>

            {/* Manual override */}
            <div style={s.row}>
              <div style={{ flex: 2, minWidth: 160 }}>
                <label style={s.label}>Postcode / ZIP</label>
                <input type="text" value={customPostcode} onChange={e => { setCustomPostcode(e.target.value); setSelectedPreset(''); }} style={s.input} placeholder="e.g. LS1 5AE or 10118" />
              </div>
              <div style={{ flex: 1, minWidth: 120 }}>
                <label style={s.label}>Country (ISO 2)</label>
                <input type="text" value={customCountry} onChange={e => { setCustomCountry(e.target.value.toUpperCase().slice(0, 2)); setSelectedPreset(''); }} style={s.input} placeholder="GB" maxLength={2} />
              </div>
            </div>
            <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>
              All orders in this batch will use this postcode/country with random customer names and street addresses.
            </div>
          </div>
        )}
      </div>

      {/* ── Channel & Status ── */}
      <div style={s.card}>
        <div style={{ fontWeight: 700, fontSize: 14, marginBottom: 14, display: 'flex', alignItems: 'center', gap: 8 }}>
          <span style={{ fontSize: 18 }}>🛒</span> Channel & Status
        </div>
        <div style={s.row}>
          <div style={{ flex: 1, minWidth: 180 }}>
            <label style={s.label}>Channel</label>
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, marginTop: 4 }}>
              {CHANNELS.map(ch => (
                <button key={ch} onClick={() => setChannel(ch)} style={s.chip(channel === ch, '#6366f1')}>
                  {ch === 'mixed' ? '🔀 Mixed' : ch}
                </button>
              ))}
            </div>
          </div>
          <div style={{ flex: 1, minWidth: 180 }}>
            <label style={s.label}>Order status</label>
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, marginTop: 4 }}>
              {STATUSES.map(st => (
                <button key={st} onClick={() => setStatus(st)} style={s.chip(status === st, '#22c55e')}>
                  {st.replace(/_/g, ' ')}
                </button>
              ))}
            </div>
            <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 6 }}>
              Use <strong>ready_to_fulfil</strong> to immediately test the dispatch / label flow
            </div>
          </div>
        </div>
      </div>

      {/* ── Product Selection ── */}
      <div style={s.card}>
        <div style={{ fontWeight: 700, fontSize: 14, marginBottom: 4, display: 'flex', alignItems: 'center', gap: 8, justifyContent: 'space-between' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <span style={{ fontSize: 18 }}>🏷️</span> Products
            {selectedProductIDs.length > 0 && (
              <span style={{ padding: '2px 8px', background: `${TEAL}22`, border: `1px solid ${TEAL}55`, borderRadius: 10, fontSize: 12, color: TEAL }}>
                {selectedProductIDs.length} pinned
              </span>
            )}
          </div>
          {selectedProductIDs.length > 0 && (
            <button onClick={() => setSelectedProductIDs([])} style={{ fontSize: 12, color: 'var(--text-muted)', background: 'none', border: 'none', cursor: 'pointer', padding: 0 }}>
              clear selection
            </button>
          )}
        </div>
        <p style={{ fontSize: 13, color: 'var(--text-muted)', margin: '0 0 12px' }}>
          Pin specific products to control which SKUs, HS codes and weights appear on labels. Leave empty to pick randomly from all active products.
        </p>

        <input
          type="text"
          value={productSearch}
          onChange={e => setProductSearch(e.target.value)}
          placeholder="Search by title or SKU…"
          style={{ ...s.input, marginBottom: 10 }}
        />

        {productsLoading ? (
          <div style={{ padding: '16px 0', textAlign: 'center', color: 'var(--text-muted)', fontSize: 13 }}>⏳ Loading products…</div>
        ) : filteredProducts.length === 0 ? (
          <div style={{ padding: '16px 0', textAlign: 'center', color: 'var(--text-muted)', fontSize: 13 }}>No active products found</div>
        ) : (
          <div style={{ maxHeight: 280, overflowY: 'auto', display: 'flex', flexDirection: 'column', gap: 4 }}>
            {filteredProducts.map(p => {
              const selected = selectedProductIDs.includes(p.product_id);
              return (
                <div
                  key={p.product_id}
                  onClick={() => toggleProduct(p.product_id)}
                  style={{
                    display: 'flex', alignItems: 'center', gap: 10,
                    padding: '8px 12px', borderRadius: 6, cursor: 'pointer',
                    background: selected ? `${TEAL}10` : 'var(--bg-elevated)',
                    border: `1px solid ${selected ? TEAL + '55' : 'var(--border)'}`,
                    transition: 'all 0.1s',
                  }}
                >
                  <input type="checkbox" checked={selected} readOnly style={{ width: 15, height: 15, accentColor: TEAL }} />
                  <div style={{ flex: 1, minWidth: 0 }}>
                    <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--text-primary)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{p.title}</div>
                    <div style={{ fontSize: 11, color: 'var(--text-muted)' }}>SKU: {p.sku}{p.weight ? ` · ${p.weight.value}${p.weight.unit}` : ''}{p.identifiers?.ean ? ` · EAN: ${p.identifiers.ean}` : ''}</div>
                  </div>
                  <span style={{ fontSize: 11, padding: '2px 6px', borderRadius: 4, background: p.status === 'active' ? 'rgba(34,197,94,0.12)' : 'rgba(156,163,175,0.15)', color: p.status === 'active' ? '#4ade80' : 'var(--text-muted)' }}>
                    {p.status}
                  </span>
                </div>
              );
            })}
          </div>
        )}
      </div>

      {/* ── What gets tested ── */}
      <div style={{ ...s.card, borderColor: 'rgba(0,184,212,0.3)', background: 'rgba(0,184,212,0.04)' }}>
        <div style={{ fontWeight: 700, fontSize: 14, marginBottom: 12, display: 'flex', alignItems: 'center', gap: 8 }}>
          <span style={{ fontSize: 18 }}>🧪</span> What this tests
        </div>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(220px, 1fr))', gap: 8, fontSize: 12, color: 'var(--text-secondary)' }}>
          {[
            { icon: '📬', text: 'Carrier postcode eligibility — Highlands / islands / BFPO zones' },
            { icon: '🏝️', text: 'Channel Islands & Isle of Man — outside UK customs territory' },
            { icon: '🛃', text: 'International customs declarations with real HS codes from PIM' },
            { icon: '🇪🇺', text: 'EU IOSS threshold — <€150 vs full customs invoice' },
            { icon: '📦', text: 'Weight-based service selection from actual product weights' },
            { icon: '🏷️', text: 'Real SKUs and product descriptions on customs labels' },
            { icon: '💰', text: 'DDP vs DDU routing — e.g. Brazil high-duty destinations' },
            { icon: '⚡', text: 'Multi-line orders — commodity grouping on customs forms' },
          ].map(item => (
            <div key={item.text} style={{ display: 'flex', gap: 8, padding: '8px 10px', background: 'var(--bg-elevated)', borderRadius: 6, border: '1px solid var(--border)' }}>
              <span style={{ fontSize: 16 }}>{item.icon}</span>
              <span>{item.text}</span>
            </div>
          ))}
        </div>
      </div>

      {/* ── Run button ── */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 14, marginBottom: 20 }}>
        <button
          onClick={runSeed}
          disabled={running}
          style={{
            padding: '12px 32px', background: running ? 'var(--bg-elevated)' : TEAL,
            border: 'none', borderRadius: 8, color: running ? 'var(--text-muted)' : '#fff',
            fontWeight: 700, fontSize: 15, cursor: running ? 'not-allowed' : 'pointer',
          }}
        >
          {running ? (
            <span style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <div className="spinner" style={{ width: 16, height: 16, borderWidth: 2 }} />
              Generating…
            </span>
          ) : `🚀 Generate ${count} order${count !== 1 ? 's' : ''}`}
        </button>
        <div style={{ fontSize: 13, color: 'var(--text-muted)' }}>
          {count} order{count !== 1 ? 's' : ''} × {linesPerOrder} line{linesPerOrder !== 1 ? 's' : ''} · {destination === 'custom' ? `${customCountry || 'GB'} ${customPostcode || '(no postcode)'}` : destination} · {channel === 'mixed' ? 'mixed channels' : channel} · status: {status.replace(/_/g, ' ')}
        </div>
      </div>

      {/* ── Error ── */}
      {error && (
        <div style={{ padding: '14px 18px', background: 'rgba(239,68,68,0.08)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 8, color: '#f87171', marginBottom: 16, fontSize: 14 }}>
          ❌ {error}
        </div>
      )}

      {/* ── Result ── */}
      {result && (
        <div style={{ padding: '18px 20px', background: result.ok ? 'rgba(34,197,94,0.06)' : 'rgba(239,68,68,0.06)', border: `1px solid ${result.ok ? 'rgba(34,197,94,0.25)' : 'rgba(239,68,68,0.3)'}`, borderRadius: 10, marginBottom: 16 }}>
          <div style={{ fontWeight: 700, fontSize: 15, marginBottom: 10, color: result.ok ? '#4ade80' : '#f87171' }}>
            {result.ok ? `✅ Created ${result.created} order${result.created !== 1 ? 's' : ''}` : '❌ Seed failed'}
          </div>

          {result.created > 0 && (
            <>
              <div style={{ fontSize: 13, color: 'var(--text-secondary)', marginBottom: 10 }}>
                Orders are now in your orders list. Head to Dispatch to generate labels.
              </div>
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, marginBottom: 12 }}>
                {result.order_ids.slice(0, 20).map(id => (
                  <a
                    key={id}
                    href={`/orders/${id}`}
                    style={{ padding: '3px 10px', background: 'rgba(0,184,212,0.1)', border: '1px solid rgba(0,184,212,0.3)', borderRadius: 6, fontSize: 11, color: TEAL, fontFamily: 'monospace', textDecoration: 'none' }}
                  >
                    {id.replace('seed-', '').slice(0, 24)}…
                  </a>
                ))}
                {result.order_ids.length > 20 && (
                  <span style={{ fontSize: 11, color: 'var(--text-muted)', alignSelf: 'center' }}>+{result.order_ids.length - 20} more</span>
                )}
              </div>
              <div style={{ display: 'flex', gap: 10 }}>
                <a href="/orders?tag=LABEL-TEST" style={{ padding: '8px 16px', background: TEAL, border: 'none', borderRadius: 6, color: '#fff', fontWeight: 600, fontSize: 13, textDecoration: 'none' }}>
                  View orders →
                </a>
                <a href="/dispatch" style={{ padding: '8px 16px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', fontSize: 13, textDecoration: 'none' }}>
                  Go to dispatch →
                </a>
                <button onClick={() => setResult(null)} style={{ padding: '8px 16px', background: 'transparent', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-muted)', fontSize: 13, cursor: 'pointer' }}>
                  Generate more
                </button>
              </div>
            </>
          )}

          {result.errors && result.errors.length > 0 && (
            <div style={{ marginTop: 12 }}>
              <div style={{ fontSize: 12, fontWeight: 600, color: '#fbbf24', marginBottom: 4 }}>⚠️ Partial errors ({result.errors.length}):</div>
              {result.errors.map((e, i) => (
                <div key={i} style={{ fontSize: 12, color: '#fbbf24', paddingLeft: 12 }}>• {e}</div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* ── Quick reference ── */}
      <div style={{ ...s.card, marginTop: 8 }}>
        <div style={{ fontWeight: 700, fontSize: 14, marginBottom: 12 }}>📋 Courier coverage quick reference</div>
        <div style={{ overflowX: 'auto' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12 }}>
            <thead>
              <tr style={{ borderBottom: '2px solid var(--border)' }}>
                {['Destination', 'Evri', 'Royal Mail', 'DPD', 'FedEx', 'Notes'].map(h => (
                  <th key={h} style={{ textAlign: 'left', padding: '6px 10px', color: 'var(--text-muted)', fontWeight: 600, fontSize: 11, textTransform: 'uppercase', letterSpacing: '0.04em' }}>{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {[
                { dest: 'UK Standard', evri: '✅', rm: '✅', dpd: '✅', fedex: '✅', note: '' },
                { dest: 'Scottish Highlands', evri: '✅ surcharge', rm: '✅', dpd: '✅ surcharge', fedex: '✅', note: 'Highland surcharge typically £5–10' },
                { dest: 'Shetland / Orkney', evri: '⚠️ restricted', rm: '✅', dpd: '⚠️ surcharge', fedex: '✅', note: 'Evri may refuse; Royal Mail safest' },
                { dest: 'Channel Islands', evri: '⚠️ limited', rm: '✅', dpd: '✅', fedex: '✅', note: 'Outside UK customs — CN22/CN23 needed' },
                { dest: 'Isle of Man', evri: '⚠️ limited', rm: '✅', dpd: '✅', fedex: '✅', note: 'Outside UK customs territory' },
                { dest: 'BFPO', evri: '❌', rm: '✅', dpd: '❌', fedex: '❌', note: 'Royal Mail BFPO service only' },
                { dest: 'EU (standard)', evri: '✅ GECO', rm: '✅', dpd: '✅', fedex: '✅', note: 'IOSS number needed for <€150' },
                { dest: 'USA', evri: '✅ GECO', rm: '✅', dpd: '✅', fedex: '✅', note: 'CN23 + commercial invoice required' },
                { dest: 'China', evri: '✅ GECO', rm: '✅', dpd: '⚠️', fedex: '✅', note: 'DDP rarely supported by all carriers' },
                { dest: 'Brazil', evri: '⚠️', rm: '✅', dpd: '⚠️', fedex: '✅', note: 'High import duties — test DDP vs DDU' },
              ].map(row => (
                <tr key={row.dest} style={{ borderBottom: '1px solid var(--border)' }}>
                  <td style={{ padding: '7px 10px', fontWeight: 600, color: 'var(--text-primary)' }}>{row.dest}</td>
                  <td style={{ padding: '7px 10px', color: row.evri.startsWith('✅') ? '#4ade80' : row.evri.startsWith('⚠️') ? '#fbbf24' : '#f87171' }}>{row.evri}</td>
                  <td style={{ padding: '7px 10px', color: row.rm.startsWith('✅') ? '#4ade80' : '#fbbf24' }}>{row.rm}</td>
                  <td style={{ padding: '7px 10px', color: row.dpd.startsWith('✅') ? '#4ade80' : row.dpd.startsWith('⚠️') ? '#fbbf24' : '#f87171' }}>{row.dpd}</td>
                  <td style={{ padding: '7px 10px', color: row.fedex.startsWith('✅') ? '#4ade80' : '#fbbf24' }}>{row.fedex}</td>
                  <td style={{ padding: '7px 10px', color: 'var(--text-muted)', fontSize: 11 }}>{row.note}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>

    </div>
  );
}

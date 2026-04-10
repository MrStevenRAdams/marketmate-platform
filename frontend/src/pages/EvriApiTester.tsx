// ============================================================================
// EVRI API TESTER — Dev Tools
// ============================================================================
// Tests all endpoints exposed by the rebuilt carrier_evri.go adapter:
//   • Credential validation  (POST /dispatch/carriers/evri/test)
//   • List services          (GET  /dispatch/carriers/evri/services)
//   • Get rates              (POST /dispatch/rates)
//   • Create shipment        (POST /dispatch/shipments)  — returns label PDF
//   • Get tracking           (GET  /dispatch/shipments/:id/tracking)
//   • Validate address       (POST /dispatch/address-validate)
// ============================================================================

import { useState, useRef } from 'react';
import { getActiveTenantId } from '../contexts/TenantContext';

const API = (import.meta.env.VITE_API_URL ?? 'http://localhost:8080/api/v1');

function apiHeaders() {
  return { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId() };
}

// ─── Preset addresses ────────────────────────────────────────────────────────
const ADDRESS_PRESETS = [
  {
    label: 'UK Standard (Leeds)',
    address: { name: 'John Smith', company: '', address_line1: '1', address_line2: 'Capitol Close', city: 'Morley', state: 'West Yorkshire', postal_code: 'LS27 0WH', country: 'GB', phone: '07700900000', email: 'john@example.com' },
  },
  {
    label: 'UK London (EC1)',
    address: { name: 'Jane Doe', company: 'Acme Ltd', address_line1: '10', address_line2: 'Goswell Road', city: 'London', state: '', postal_code: 'EC1V 7DU', country: 'GB', phone: '07700900001', email: 'jane@example.com' },
  },
  {
    label: 'Northern Ireland (Belfast)',
    address: { name: 'Aoife Murphy', company: '', address_line1: '5', address_line2: 'High Street', city: 'Belfast', state: '', postal_code: 'BT1 2AA', country: 'GB', phone: '07700900002', email: 'aoife@example.com' },
  },
  {
    label: 'Germany (Hamburg)',
    address: { name: 'Hans Müller', company: '', address_line1: '11', address_line2: 'Hauptstrasse', city: 'Hamburg', state: '', postal_code: '20095', country: 'DE', phone: '', email: 'hans@example.com' },
  },
  {
    label: 'France (Paris)',
    address: { name: 'Marie Dupont', company: '', address_line1: '12', address_line2: 'Rue de Rivoli', city: 'Paris', state: '', postal_code: '75001', country: 'FR', phone: '', email: 'marie@example.com' },
  },
  {
    label: 'USA (New York)',
    address: { name: 'Bob Johnson', company: '', address_line1: '350', address_line2: '5th Avenue', city: 'New York', state: 'NY', postal_code: '10118', country: 'US', phone: '', email: 'bob@example.com' },
  },
  {
    label: 'Invalid postcode (will error)',
    address: { name: 'Test Error', company: '', address_line1: '1', address_line2: 'Fake Street', city: 'Nowhere', state: '', postal_code: 'XX9 9ZZ', country: 'GB', phone: '', email: '' },
  },
];

const SERVICE_OPTIONS = [
  { code: 'PARCEL',           label: 'Standard (2–3 days)' },
  { code: 'PARCEL_NEXT_DAY',  label: 'Next Day' },
  { code: 'LARGE_PARCEL',     label: 'Large Parcel (2–4 days)' },
  { code: 'PARCEL_SIGNATURE', label: 'Signature' },
  { code: 'INT_PARCEL',       label: 'International (GECO)' },
];

const SENDER_DEFAULT = {
  name: '247 Commerce Ltd',
  company: '247 Commerce Ltd',
  address_line1: 'Unit 1',
  address_line2: 'Warehouse Road',
  city: 'Leeds',
  state: 'West Yorkshire',
  postal_code: 'LS27 0WH',
  country: 'GB',
  phone: '',
  email: '',
};

// ─── Types ───────────────────────────────────────────────────────────────────
interface Address {
  name: string; company: string;
  address_line1: string; address_line2: string;
  city: string; state: string; postal_code: string; country: string;
  phone: string; email: string;
}

interface Parcel { weight: number; length: number; width: number; height: number; description: string; }

type PanelId = 'creds' | 'services' | 'rates' | 'shipment' | 'tracking';

interface PanelState {
  loading: boolean;
  result: unknown;
  error: string | null;
}

function emptyPanel(): PanelState { return { loading: false, result: null, error: null }; }

// ─── Styles (inline — no CSS file needed for a dev tool) ─────────────────────
const s = {
  page:    { padding: '24px 32px', maxWidth: 1100, margin: '0 auto', fontFamily: 'var(--font-sans, monospace)' } as React.CSSProperties,
  header:  { display: 'flex', alignItems: 'center', gap: 12, marginBottom: 28 } as React.CSSProperties,
  badge:   { background: '#22c55e22', color: '#22c55e', border: '1px solid #22c55e44', borderRadius: 6, padding: '2px 10px', fontSize: 12, fontWeight: 600 } as React.CSSProperties,
  grid:    { display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 20, marginBottom: 20 } as React.CSSProperties,
  card:    { background: 'var(--bg-secondary, #1a1a2e)', border: '1px solid var(--border, #2a2a3e)', borderRadius: 12, overflow: 'hidden' } as React.CSSProperties,
  cardHdr: { padding: '14px 18px', borderBottom: '1px solid var(--border, #2a2a3e)', display: 'flex', alignItems: 'center', justifyContent: 'space-between' } as React.CSSProperties,
  cardBdy: { padding: 18 } as React.CSSProperties,
  label:   { display: 'block', fontSize: 11, fontWeight: 600, color: 'var(--text-muted, #888)', textTransform: 'uppercase' as const, letterSpacing: '0.05em', marginBottom: 4 },
  input:   { width: '100%', padding: '8px 10px', background: 'var(--bg-tertiary, #111)', border: '1px solid var(--border, #333)', borderRadius: 6, color: 'var(--text, #eee)', fontSize: 13, boxSizing: 'border-box' as const },
  select:  { width: '100%', padding: '8px 10px', background: 'var(--bg-tertiary, #111)', border: '1px solid var(--border, #333)', borderRadius: 6, color: 'var(--text, #eee)', fontSize: 13 },
  row:     { display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 10, marginBottom: 10 } as React.CSSProperties,
  row3:    { display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: 10, marginBottom: 10 } as React.CSSProperties,
  btn:     { padding: '9px 18px', borderRadius: 7, border: 'none', cursor: 'pointer', fontWeight: 600, fontSize: 13, display: 'inline-flex', alignItems: 'center', gap: 6 } as React.CSSProperties,
  btnGreen:{ background: '#22c55e', color: '#000' } as React.CSSProperties,
  btnGrey: { background: 'var(--bg-tertiary, #222)', color: 'var(--text-muted, #aaa)', border: '1px solid var(--border, #333)' } as React.CSSProperties,
  btnRed:  { background: '#ef4444', color: '#fff' } as React.CSSProperties,
  result:  { marginTop: 14, background: '#0a0a0a', border: '1px solid #2a2a2a', borderRadius: 8, padding: 14, fontSize: 12, fontFamily: 'monospace', maxHeight: 320, overflowY: 'auto' as const, whiteSpace: 'pre-wrap' as const, wordBreak: 'break-all' as const } as React.CSSProperties,
  ok:      { color: '#22c55e' } as React.CSSProperties,
  err:     { color: '#ef4444' } as React.CSSProperties,
  warn:    { color: '#f59e0b' } as React.CSSProperties,
  tag:     { fontSize: 11, padding: '1px 7px', borderRadius: 10, fontWeight: 600 } as React.CSSProperties,
  divider: { margin: '20px 0', borderColor: 'var(--border, #2a2a3e)', borderStyle: 'solid', borderWidth: '1px 0 0 0' } as React.CSSProperties,
  fullCard:{ gridColumn: '1 / -1' } as React.CSSProperties,
};

// ─── Main component ───────────────────────────────────────────────────────────
export default function EvriApiTester() {
  // Credentials (for test-creds panel — pre-filled with SIT creds from handover)
  const [credsAccountId, setCredsAccountId]   = useState('9866');
  const [credsClientName, setCredsClientName] = useState('247 Commerce Ltd');
  const [credsUsername, setCredsUsername]     = useState('247CommerceLtd-sit');
  const [credsPassword, setCredsPassword]     = useState('R4YTkjyumJzr');
  const [credsSandbox, setCredsSandbox]       = useState(true);

  // Recipient address
  const [toAddr, setToAddr]   = useState<Address>(ADDRESS_PRESETS[0].address);
  const [fromAddr, setFromAddr] = useState<Address>(SENDER_DEFAULT);

  // Parcel
  const [parcel, setParcel]   = useState<Parcel>({ weight: 1.0, length: 30, width: 20, height: 15, description: 'Clothing' });

  // Service
  const [serviceCode, setServiceCode] = useState('PARCEL');
  const [labelFormat, setLabelFormat] = useState('PDF');
  const [reference, setReference]     = useState('TEST-' + Date.now().toString().slice(-6));
  const [signature, setSignature]     = useState(false);

  // Tracking
  const [trackingNumber, setTrackingNumber] = useState('');

  // Panel states
  const [panels, setPanels] = useState<Record<PanelId, PanelState>>({
    creds: emptyPanel(), services: emptyPanel(), rates: emptyPanel(),
    shipment: emptyPanel(), tracking: emptyPanel(),
  });

  // Label display
  const [labelData, setLabelData] = useState<string | null>(null);
  const labelRef = useRef<HTMLIFrameElement>(null);

  function setPanel(id: PanelId, patch: Partial<PanelState>) {
    setPanels(prev => ({ ...prev, [id]: { ...prev[id], ...patch } }));
  }

  // ── Helpers ────────────────────────────────────────────────────────────────
  async function callApi(method: string, path: string, body?: unknown) {
    const res = await fetch(`${API}${path}`, {
      method,
      headers: apiHeaders(),
      ...(body !== undefined ? { body: JSON.stringify(body) } : {}),
    });
    const data = await res.json().catch(() => ({ _raw: `HTTP ${res.status}` }));
    if (!res.ok) throw Object.assign(new Error(`HTTP ${res.status}`), { data });
    return data;
  }

  function fmtResult(data: unknown): string {
    return JSON.stringify(data, null, 2);
  }

  // ── Save temp Evri creds to Firestore via API for testing ──────────────────
  async function saveTempCreds() {
    return callApi('POST', '/dispatch/carriers/evri/credentials', {
      carrier_id: 'evri',
      account_id: credsAccountId,
      username:   credsUsername,
      password:   credsPassword,
      is_sandbox: credsSandbox,
      extra: { client_name: credsClientName },
    });
  }

  // ── PANEL: Validate credentials ────────────────────────────────────────────
  async function testCreds() {
    setPanel('creds', { loading: true, error: null, result: null });
    try {
      await saveTempCreds();
      const data = await callApi('POST', '/dispatch/carriers/evri/test');
      setPanel('creds', { loading: false, result: data, error: null });
    } catch (e: unknown) {
      const err = e as { message: string; data?: unknown };
      setPanel('creds', { loading: false, error: err.message, result: err.data ?? null });
    }
  }

  // ── PANEL: List services ───────────────────────────────────────────────────
  async function listServices() {
    setPanel('services', { loading: true, error: null, result: null });
    try {
      const data = await callApi('GET', '/dispatch/carriers/evri/services');
      setPanel('services', { loading: false, result: data, error: null });
    } catch (e: unknown) {
      const err = e as { message: string; data?: unknown };
      setPanel('services', { loading: false, error: err.message, result: err.data ?? null });
    }
  }

  // ── PANEL: Get rates ───────────────────────────────────────────────────────
  async function getRates() {
    setPanel('rates', { loading: true, error: null, result: null });
    try {
      const body = {
        from_address: fromAddr,
        to_address:   toAddr,
        parcels: [{ weight: parcel.weight, length: parcel.length, width: parcel.width, height: parcel.height }],
        carrier_id: 'evri',
      };
      const data = await callApi('POST', '/dispatch/rates', body);
      setPanel('rates', { loading: false, result: data, error: null });
    } catch (e: unknown) {
      const err = e as { message: string; data?: unknown };
      setPanel('rates', { loading: false, error: err.message, result: err.data ?? null });
    }
  }

  // ── PANEL: Create shipment ─────────────────────────────────────────────────
  async function createShipment() {
    setPanel('shipment', { loading: true, error: null, result: null });
    setLabelData(null);
    try {
      const body = {
        carrier_id:   'evri',
        service_code: serviceCode,
        reference:    reference,
        description:  parcel.description,
        from_address: fromAddr,
        to_address:   toAddr,
        parcels: [{ weight: parcel.weight, length: parcel.length, width: parcel.width, height: parcel.height, description: parcel.description }],
        options: {
          label_format: labelFormat,
          signature:    signature,
        },
      };
      const data = await callApi('POST', '/dispatch/shipments', body) as Record<string, unknown>;
      setPanel('shipment', { loading: false, result: data, error: null });

      // Extract tracking number for the tracking panel
      if (data.tracking_number) setTrackingNumber(data.tracking_number as string);

      // If label data comes back, display it
      if (data.label_url) {
        setLabelData(data.label_url as string);
      } else if (data.label_data) {
        // base64 PDF
        setLabelData(`data:application/pdf;base64,${data.label_data}`);
      }
    } catch (e: unknown) {
      const err = e as { message: string; data?: unknown };
      setPanel('shipment', { loading: false, error: err.message, result: err.data ?? null });
    }
  }

  // ── PANEL: Get tracking ────────────────────────────────────────────────────
  async function getTracking() {
    if (!trackingNumber) return;
    setPanel('tracking', { loading: true, error: null, result: null });
    try {
      // Find shipment by tracking number first
      const shipments = await callApi('GET', `/dispatch/shipments?carrier_id=evri&limit=50`) as { shipments?: { shipment_id: string; tracking_number: string }[] };
      const match = shipments?.shipments?.find(s => s.tracking_number === trackingNumber);
      if (!match) throw new Error(`No shipment found with tracking number ${trackingNumber}`);
      const data = await callApi('GET', `/dispatch/shipments/${match.shipment_id}/tracking`);
      setPanel('tracking', { loading: false, result: data, error: null });
    } catch (e: unknown) {
      const err = e as { message: string; data?: unknown };
      setPanel('tracking', { loading: false, error: err.message, result: err.data ?? null });
    }
  }

  // ── Address preset loader ──────────────────────────────────────────────────
  function loadPreset(idx: number) {
    setToAddr({ ...ADDRESS_PRESETS[idx].address });
    // Auto-select international service if non-GB
    if (ADDRESS_PRESETS[idx].address.country !== 'GB') {
      setServiceCode('INT_PARCEL');
    } else {
      setServiceCode('PARCEL');
    }
  }

  // ── Render ─────────────────────────────────────────────────────────────────
  return (
    <div style={s.page}>
      {/* Header */}
      <div style={s.header}>
        <span style={{ fontSize: 28 }}>🟢</span>
        <div>
          <h1 style={{ margin: 0, fontSize: 22, fontWeight: 700 }}>Evri API Tester</h1>
          <p style={{ margin: 0, fontSize: 13, color: 'var(--text-muted, #888)' }}>
            Test all endpoints in the rebuilt carrier_evri.go adapter against the Evri Routing Web Service v4
          </p>
        </div>
        <div style={{ marginLeft: 'auto', display: 'flex', gap: 8, alignItems: 'center' }}>
          <span style={{ ...s.badge, background: '#f59e0b22', color: '#f59e0b', borderColor: '#f59e0b44' }}>
            XML/REST v4
          </span>
          <span style={s.badge}>hermes-europe.co.uk</span>
        </div>
      </div>

      {/* ── Section 1: Credentials ─────────────────────────────────────────── */}
      <div style={{ ...s.card, marginBottom: 20 }}>
        <div style={s.cardHdr}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <span>🔑</span>
            <strong>1. Credentials</strong>
            <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>— pre-filled with SIT test account from handover doc</span>
          </div>
          <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
            <label style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 13, cursor: 'pointer' }}>
              <input type="checkbox" checked={credsSandbox} onChange={e => setCredsSandbox(e.target.checked)} />
              Use SIT (test)
            </label>
            <button style={{ ...s.btn, ...s.btnGreen }} onClick={testCreds} disabled={panels.creds.loading}>
              {panels.creds.loading ? '⏳ Testing…' : '▶ Save & Test Credentials'}
            </button>
          </div>
        </div>
        <div style={s.cardBdy}>
          <div style={s.row}>
            <div>
              <label style={s.label}>Client ID (account_id)</label>
              <input style={s.input} value={credsAccountId} onChange={e => setCredsAccountId(e.target.value)} placeholder="e.g. 9866" />
            </div>
            <div>
              <label style={s.label}>Client Name (extra.client_name)</label>
              <input style={s.input} value={credsClientName} onChange={e => setCredsClientName(e.target.value)} placeholder="e.g. 247 Commerce Ltd" />
            </div>
          </div>
          <div style={s.row}>
            <div>
              <label style={s.label}>Username</label>
              <input style={s.input} value={credsUsername} onChange={e => setCredsUsername(e.target.value)} />
            </div>
            <div>
              <label style={s.label}>Password</label>
              <input style={s.input} type="password" value={credsPassword} onChange={e => setCredsPassword(e.target.value)} />
            </div>
          </div>
          {panels.creds.result !== null && (
            <div style={s.result}>
              <span style={panels.creds.error ? s.err : s.ok}>
                {panels.creds.error ? '✗ FAILED' : '✓ OK'}{'\n'}
              </span>
              {fmtResult(panels.creds.result)}
            </div>
          )}
          {panels.creds.error && !panels.creds.result && (
            <div style={{ ...s.result, ...s.err }}>{panels.creds.error}</div>
          )}
        </div>
      </div>

      {/* ── Section 2: Address + Parcel ────────────────────────────────────── */}
      <div style={{ ...s.card, marginBottom: 20 }}>
        <div style={s.cardHdr}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <span>📦</span>
            <strong>2. Shipment Details</strong>
          </div>
          <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' as const }}>
            {ADDRESS_PRESETS.map((p, i) => (
              <button
                key={i}
                style={{
                  ...s.btn,
                  ...(i === ADDRESS_PRESETS.length - 1 ? s.btnRed : s.btnGrey),
                  padding: '5px 10px',
                  fontSize: 11,
                }}
                onClick={() => loadPreset(i)}
              >
                {p.label}
              </button>
            ))}
          </div>
        </div>
        <div style={s.cardBdy}>
          <div style={s.grid}>
            {/* Recipient */}
            <div>
              <p style={{ margin: '0 0 10px', fontSize: 12, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.05em' }}>📬 Recipient Address</p>
              <div style={s.row}>
                <div><label style={s.label}>Name</label><input style={s.input} value={toAddr.name} onChange={e => setToAddr(a => ({ ...a, name: e.target.value }))} /></div>
                <div><label style={s.label}>Company</label><input style={s.input} value={toAddr.company} onChange={e => setToAddr(a => ({ ...a, company: e.target.value }))} /></div>
              </div>
              <div style={{ marginBottom: 10 }}>
                <label style={s.label}>Address Line 1 (house no.)</label>
                <input style={s.input} value={toAddr.address_line1} onChange={e => setToAddr(a => ({ ...a, address_line1: e.target.value }))} />
              </div>
              <div style={{ marginBottom: 10 }}>
                <label style={s.label}>Address Line 2 (street name) *mandatory*</label>
                <input style={s.input} value={toAddr.address_line2} onChange={e => setToAddr(a => ({ ...a, address_line2: e.target.value }))} />
              </div>
              <div style={s.row3}>
                <div><label style={s.label}>City *</label><input style={s.input} value={toAddr.city} onChange={e => setToAddr(a => ({ ...a, city: e.target.value }))} /></div>
                <div><label style={s.label}>Postcode *</label><input style={s.input} value={toAddr.postal_code} onChange={e => setToAddr(a => ({ ...a, postal_code: e.target.value }))} /></div>
                <div><label style={s.label}>Country (ISO) *</label><input style={s.input} value={toAddr.country} onChange={e => setToAddr(a => ({ ...a, country: e.target.value.toUpperCase() }))} maxLength={2} /></div>
              </div>
              <div style={s.row}>
                <div><label style={s.label}>Phone</label><input style={s.input} value={toAddr.phone} onChange={e => setToAddr(a => ({ ...a, phone: e.target.value }))} /></div>
                <div><label style={s.label}>Email</label><input style={s.input} value={toAddr.email} onChange={e => setToAddr(a => ({ ...a, email: e.target.value }))} /></div>
              </div>
            </div>

            {/* Sender */}
            <div>
              <p style={{ margin: '0 0 10px', fontSize: 12, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.05em' }}>🏭 Sender Address</p>
              <div style={s.row}>
                <div><label style={s.label}>Company</label><input style={s.input} value={fromAddr.company} onChange={e => setFromAddr(a => ({ ...a, company: e.target.value }))} /></div>
                <div><label style={s.label}>Address Line 1</label><input style={s.input} value={fromAddr.address_line1} onChange={e => setFromAddr(a => ({ ...a, address_line1: e.target.value }))} /></div>
              </div>
              <div style={{ marginBottom: 10 }}>
                <label style={s.label}>Address Line 2</label>
                <input style={s.input} value={fromAddr.address_line2} onChange={e => setFromAddr(a => ({ ...a, address_line2: e.target.value }))} />
              </div>
              <div style={s.row3}>
                <div><label style={s.label}>City</label><input style={s.input} value={fromAddr.city} onChange={e => setFromAddr(a => ({ ...a, city: e.target.value }))} /></div>
                <div><label style={s.label}>Postcode</label><input style={s.input} value={fromAddr.postal_code} onChange={e => setFromAddr(a => ({ ...a, postal_code: e.target.value }))} /></div>
                <div><label style={s.label}>Country</label><input style={s.input} value={fromAddr.country} onChange={e => setFromAddr(a => ({ ...a, country: e.target.value.toUpperCase() }))} maxLength={2} /></div>
              </div>

              <hr style={s.divider} />
              <p style={{ margin: '0 0 10px', fontSize: 12, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.05em' }}>📐 Parcel</p>
              <div style={s.row}>
                <div><label style={s.label}>Weight (kg)</label><input style={s.input} type="number" step="0.1" min="0.1" value={parcel.weight} onChange={e => setParcel(p => ({ ...p, weight: +e.target.value }))} /></div>
                <div><label style={s.label}>Description</label><input style={s.input} value={parcel.description} onChange={e => setParcel(p => ({ ...p, description: e.target.value }))} /></div>
              </div>
              <div style={s.row3}>
                <div><label style={s.label}>Length (cm)</label><input style={s.input} type="number" value={parcel.length} onChange={e => setParcel(p => ({ ...p, length: +e.target.value }))} /></div>
                <div><label style={s.label}>Width (cm)</label><input style={s.input} type="number" value={parcel.width} onChange={e => setParcel(p => ({ ...p, width: +e.target.value }))} /></div>
                <div><label style={s.label}>Height (cm)</label><input style={s.input} type="number" value={parcel.height} onChange={e => setParcel(p => ({ ...p, height: +e.target.value }))} /></div>
              </div>
            </div>
          </div>
        </div>
      </div>

      {/* ── Section 3: Action panels ────────────────────────────────────────── */}
      <div style={s.grid}>

        {/* Services */}
        <div style={s.card}>
          <div style={s.cardHdr}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <span>🚚</span><strong>List Services</strong>
            </div>
            <button style={{ ...s.btn, ...s.btnGreen }} onClick={listServices} disabled={panels.services.loading}>
              {panels.services.loading ? '⏳' : '▶ Run'}
            </button>
          </div>
          <div style={s.cardBdy}>
            <p style={{ margin: '0 0 10px', fontSize: 12, color: 'var(--text-muted)' }}>
              GET /dispatch/carriers/evri/services — returns all 5 service codes
            </p>
            {renderPanel(panels.services)}
          </div>
        </div>

        {/* Rates */}
        <div style={s.card}>
          <div style={s.cardHdr}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <span>💷</span><strong>Get Rates</strong>
            </div>
            <button style={{ ...s.btn, ...s.btnGreen }} onClick={getRates} disabled={panels.rates.loading}>
              {panels.rates.loading ? '⏳' : '▶ Run'}
            </button>
          </div>
          <div style={s.cardBdy}>
            <p style={{ margin: '0 0 10px', fontSize: 12, color: 'var(--text-muted)' }}>
              POST /dispatch/rates — indicative only (contractual pricing, costs will be £0)
            </p>
            {renderPanel(panels.rates)}
          </div>
        </div>

        {/* Create Shipment — full width */}
        <div style={{ ...s.card, ...s.fullCard }}>
          <div style={s.cardHdr}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <span>🏷️</span><strong>Create Shipment &amp; Generate Label</strong>
              <span style={{ ...s.tag, background: '#22c55e22', color: '#22c55e' }}>POST /dispatch/shipments</span>
            </div>
            <button style={{ ...s.btn, ...s.btnGreen }} onClick={createShipment} disabled={panels.shipment.loading}>
              {panels.shipment.loading ? '⏳ Calling Evri XML API…' : '▶ Create Shipment'}
            </button>
          </div>
          <div style={s.cardBdy}>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr 1fr auto', gap: 10, marginBottom: 14, alignItems: 'end' }}>
              <div>
                <label style={s.label}>Service</label>
                <select style={s.select} value={serviceCode} onChange={e => setServiceCode(e.target.value)}>
                  {SERVICE_OPTIONS.map(o => <option key={o.code} value={o.code}>{o.code} — {o.label}</option>)}
                </select>
              </div>
              <div>
                <label style={s.label}>Label Format</label>
                <select style={s.select} value={labelFormat} onChange={e => setLabelFormat(e.target.value)}>
                  <option value="PDF">PDF</option>
                  <option value="ZPL">ZPL (ZPL_799_1199)</option>
                </select>
              </div>
              <div>
                <label style={s.label}>Reference</label>
                <input style={s.input} value={reference} onChange={e => setReference(e.target.value)} />
              </div>
              <div>
                <label style={s.label}>Options</label>
                <label style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 13, cursor: 'pointer', marginTop: 4 }}>
                  <input type="checkbox" checked={signature} onChange={e => setSignature(e.target.checked)} />
                  Signature required
                </label>
              </div>
            </div>

            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16 }}>
              <div>
                {renderPanel(panels.shipment)}
              </div>
              <div>
                {labelData ? (
                  <div>
                    <p style={{ margin: '0 0 8px', fontSize: 12, fontWeight: 700, color: '#22c55e' }}>✓ Label received</p>
                    {labelData.startsWith('data:application/pdf') ? (
                      <iframe
                        ref={labelRef}
                        src={labelData}
                        style={{ width: '100%', height: 300, border: '1px solid var(--border)', borderRadius: 6 }}
                        title="Evri Label"
                      />
                    ) : (
                      <div style={{ ...s.result, height: 300, color: '#22c55e' }}>
                        {labelData.startsWith('http') ? (
                          <a href={labelData} target="_blank" rel="noreferrer" style={{ color: '#22c55e' }}>
                            📄 Open Label: {labelData}
                          </a>
                        ) : labelData}
                      </div>
                    )}
                    <div style={{ marginTop: 8, display: 'flex', gap: 8 }}>
                      {labelData.startsWith('http') && (
                        <a href={labelData} target="_blank" rel="noreferrer" style={{ ...s.btn, ...s.btnGreen, textDecoration: 'none' }}>
                          📥 Download Label
                        </a>
                      )}
                      {labelData.startsWith('data:') && (
                        <a
                          href={labelData}
                          download={`evri-label-${reference}.pdf`}
                          style={{ ...s.btn, ...s.btnGreen, textDecoration: 'none' }}
                        >
                          📥 Download PDF
                        </a>
                      )}
                    </div>
                  </div>
                ) : panels.shipment.result === null ? (
                  <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', height: 200, color: 'var(--text-muted)', border: '2px dashed var(--border)', borderRadius: 8 }}>
                    <span style={{ fontSize: 32, marginBottom: 8 }}>🏷️</span>
                    <span style={{ fontSize: 13 }}>Label will appear here</span>
                  </div>
                ) : (
                  <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', height: 200, color: '#f59e0b', border: '2px dashed #f59e0b44', borderRadius: 8 }}>
                    <span style={{ fontSize: 32, marginBottom: 8 }}>⚠️</span>
                    <span style={{ fontSize: 13 }}>No label data in response</span>
                    <span style={{ fontSize: 11, marginTop: 4, color: 'var(--text-muted)' }}>SIT may not return labels — check response for barcode</span>
                  </div>
                )}
              </div>
            </div>
          </div>
        </div>

        {/* Tracking */}
        <div style={{ ...s.card, ...s.fullCard }}>
          <div style={s.cardHdr}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <span>📍</span><strong>Get Tracking</strong>
              <span style={{ ...s.tag, background: '#6366f122', color: '#818cf8' }}>Note: full event data requires separate Evri Tracking API contract</span>
            </div>
            <button style={{ ...s.btn, ...s.btnGreen }} onClick={getTracking} disabled={panels.tracking.loading || !trackingNumber}>
              {panels.tracking.loading ? '⏳' : '▶ Run'}
            </button>
          </div>
          <div style={s.cardBdy}>
            <div style={{ display: 'flex', gap: 10, alignItems: 'flex-end', marginBottom: 14 }}>
              <div style={{ flex: 1 }}>
                <label style={s.label}>Tracking Number (auto-filled after Create Shipment)</label>
                <input style={s.input} value={trackingNumber} onChange={e => setTrackingNumber(e.target.value)} placeholder="e.g. H00IMA0014926664" />
              </div>
              {trackingNumber && (
                <a
                  href={`https://www.evri.com/track/${trackingNumber}`}
                  target="_blank"
                  rel="noreferrer"
                  style={{ ...s.btn, ...s.btnGrey, textDecoration: 'none' }}
                >
                  🌐 Track on Evri.com
                </a>
              )}
            </div>
            {renderPanel(panels.tracking)}
          </div>
        </div>

      </div>

      {/* ── Section 4: XML request preview ──────────────────────────────────── */}
      <div style={{ ...s.card, marginTop: 20 }}>
        <div style={s.cardHdr}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <span>🔍</span><strong>Expected XML Payload Preview</strong>
            <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>— what carrier_evri.go will send to hermes-europe.co.uk</span>
          </div>
        </div>
        <div style={s.cardBdy}>
          <XmlPreview toAddr={toAddr} fromAddr={fromAddr} parcel={parcel} serviceCode={serviceCode} reference={reference} labelFormat={labelFormat} signature={signature} credsAccountId={credsAccountId} credsClientName={credsClientName} credsSandbox={credsSandbox} />
        </div>
      </div>
    </div>
  );
}

// ─── Result panel renderer ─────────────────────────────────────────────────────
function renderPanel(p: PanelState) {
  if (p.loading) return <div style={{ padding: 20, textAlign: 'center', color: 'var(--text-muted)' }}>⏳ Calling API…</div>;
  if (!p.result && !p.error) return null;
  return (
    <div style={{
      marginTop: 0,
      background: '#0a0a0a',
      border: `1px solid ${p.error ? '#ef444444' : '#22c55e44'}`,
      borderRadius: 8, padding: 14, fontSize: 12,
      fontFamily: 'monospace',
      maxHeight: 280, overflowY: 'auto',
      whiteSpace: 'pre-wrap', wordBreak: 'break-all',
    }}>
      <span style={{ color: p.error ? '#ef4444' : '#22c55e' }}>
        {p.error ? '✗ ' + p.error + '\n\n' : '✓ Success\n\n'}
      </span>
      {p.result !== null && JSON.stringify(p.result, null, 2)}
    </div>
  );
}

// ─── XML Preview component ────────────────────────────────────────────────────
interface XmlPreviewProps {
  toAddr: Address; fromAddr: Address; parcel: Parcel;
  serviceCode: string; reference: string; labelFormat: string;
  signature: boolean; credsAccountId: string; credsClientName: string;
  credsSandbox: boolean;
}

function XmlPreview({ toAddr, fromAddr, parcel, serviceCode, reference, labelFormat, signature, credsAccountId, credsClientName, credsSandbox }: XmlPreviewProps) {
  const weightGrams = Math.round(parcel.weight * 1000);
  const girth = 2 * parcel.width + 2 * parcel.height;
  const combined = parcel.length + girth;
  const volume = Math.round(parcel.length * parcel.width * parcel.height);
  const endpoint = credsSandbox
    ? `https://sit.hermes-europe.co.uk/routing/service/rest/v4/routeDeliveryCreatePreadviceAndLabel`
    : `https://www.hermes-europe.co.uk/routing/service/rest/v4/routeDeliveryCreatePreadviceAndLabel`;

  const nextDay = serviceCode === 'PARCEL_NEXT_DAY';
  const sig = signature || serviceCode === 'PARCEL_SIGNATURE';

  const xml = `POST ${endpoint}
Authorization: Basic [base64(${credsAccountId ? 'username:password' : '?:?'})]
Content-Type: text/xml

<?xml version="1.0" encoding="UTF-8"?>
<deliveryRoutingRequest>
  <clientId>${credsAccountId || '?'}</clientId>
  <clientName>${credsClientName || '?'}</clientName>
  <sourceOfRequest>CLIENTWS</sourceOfRequest>
  <creationDate>${new Date().toISOString().slice(0, 19)}</creationDate>${labelFormat === 'ZPL' ? '\n  <labelFormat>ZPL_799_1199</labelFormat>' : ''}
  <deliveryRoutingRequestEntries>
    <deliveryRoutingRequestEntry>
      <addressValidationRequired>false</addressValidationRequired>
      <customer>
        <address>
          <firstName>${toAddr.name.split(' ').slice(0, -1).join(' ') || ''}</firstName>
          <lastName>${toAddr.name.split(' ').slice(-1)[0] || 'Customer'}</lastName>
          <houseNo>${toAddr.address_line1}</houseNo>
          <streetName>${toAddr.address_line2 || toAddr.address_line1}</streetName>
          <city>${toAddr.city}</city>
          <postCode>${toAddr.postal_code.toUpperCase()}</postCode>
          <countryCode>${toAddr.country.toUpperCase()}</countryCode>
        </address>${toAddr.phone ? `\n        <mobilePhoneNo>${toAddr.phone}</mobilePhoneNo>` : ''}${toAddr.email ? `\n        <email>${toAddr.email}</email>` : ''}
        <customerReference1>${reference}</customerReference1>
      </customer>
      <parcel>
        <weight>${weightGrams}</weight>
        <length>${parcel.length}</length>
        <width>${parcel.width}</width>
        <depth>${parcel.height}</depth>
        <girth>${girth}</girth>
        <combinedDimension>${combined}</combinedDimension>
        <volume>${volume}</volume>
        <currency>GBP</currency>
        <value>0</value>
        <numberOfItems>1</numberOfItems>
        <description>${parcel.description}</description>
        <originOfParcel>${fromAddr.country.toUpperCase()}</originOfParcel>
      </parcel>${(nextDay || sig) ? `
      <services>${nextDay ? '\n        <nextDay>true</nextDay>' : ''}${sig ? '\n        <signature>true</signature>' : ''}
      </services>` : ''}
      <senderAddress>
        <addressLine1>${fromAddr.company || fromAddr.name}</addressLine1>
        <addressLine2>${fromAddr.address_line1}</addressLine2>
        <addressLine3>${fromAddr.city} ${fromAddr.postal_code}</addressLine3>
      </senderAddress>
      <expectedDespatchDate>${new Date().toISOString().slice(0, 10)}</expectedDespatchDate>
      <countryOfOrigin>${fromAddr.country.toUpperCase()}</countryOfOrigin>
    </deliveryRoutingRequestEntry>
  </deliveryRoutingRequestEntries>
</deliveryRoutingRequest>`;

  return (
    <div style={{
      background: '#050508',
      border: '1px solid #1a1a2e',
      borderRadius: 8,
      padding: 14,
      fontSize: 12,
      fontFamily: 'monospace',
      overflowX: 'auto',
      whiteSpace: 'pre',
      color: '#a5f3fc',
      maxHeight: 400,
      overflowY: 'auto',
    }}>
      {xml}
    </div>
  );
}

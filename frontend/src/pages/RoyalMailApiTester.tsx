// ============================================================================
// ROYAL MAIL CLICK & DROP API TESTER — Dev Tools
// ============================================================================
// Tests all endpoints exposed by the rebuilt carrier_royal_mail.go adapter:
//   • Credential validation  (POST /dispatch/carriers/royal-mail/test)
//   • List services          (GET  /dispatch/carriers/royal-mail/services)
//   • Get rates              (POST /dispatch/rates)
//   • Create shipment        (POST /dispatch/shipments)  — label only for OBA
//   • Generate manifest      (POST /dispatch/manifests)
//
// OBA NOTE: Label PDF download requires an OBA-linked Click & Drop account.
// Non-OBA accounts will get a tracking number but no label — this is expected.
// ============================================================================

import { useState } from 'react';
import { getActiveTenantId } from '../contexts/TenantContext';

const API = (import.meta.env.VITE_API_URL ?? 'http://localhost:8080/api/v1');

function apiHeaders() {
  return { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId() };
}

// ─── Preset addresses ────────────────────────────────────────────────────────
const ADDRESS_PRESETS = [
  {
    label: 'UK Standard (Leeds)',
    address: { name: 'John Smith', company: '', address_line1: '1 Capitol Close', address_line2: 'Morley', city: 'Leeds', state: 'West Yorkshire', postal_code: 'LS27 0WH', country: 'GB', phone: '07700900000', email: 'john@example.com' },
  },
  {
    label: 'UK London (EC1)',
    address: { name: 'Jane Doe', company: 'Acme Ltd', address_line1: '10 Goswell Road', address_line2: '', city: 'London', state: '', postal_code: 'EC1V 7DU', country: 'GB', phone: '07700900001', email: 'jane@example.com' },
  },
  {
    label: 'Northern Ireland (Belfast)',
    address: { name: 'Aoife Murphy', company: '', address_line1: '5 High Street', address_line2: '', city: 'Belfast', state: '', postal_code: 'BT1 2AA', country: 'GB', phone: '07700900002', email: 'aoife@example.com' },
  },
  {
    label: 'Germany (Hamburg)',
    address: { name: 'Hans Müller', company: '', address_line1: '11 Hauptstrasse', address_line2: '', city: 'Hamburg', state: '', postal_code: '20095', country: 'DE', phone: '', email: 'hans@example.com' },
  },
  {
    label: 'France (Paris)',
    address: { name: 'Marie Dupont', company: '', address_line1: '12 Rue de Rivoli', address_line2: '', city: 'Paris', state: '', postal_code: '75001', country: 'FR', phone: '', email: 'marie@example.com' },
  },
  {
    label: 'USA (New York)',
    address: { name: 'Bob Johnson', company: '', address_line1: '350 5th Avenue', address_line2: '', city: 'New York', state: 'NY', postal_code: '10118', country: 'US', phone: '', email: 'bob@example.com' },
  },
  {
    label: 'Invalid postcode (will error)',
    address: { name: 'Test Error', company: '', address_line1: '1 Fake Street', address_line2: '', city: 'Nowhere', state: '', postal_code: 'XX9 9ZZ', country: 'GB', phone: '', email: '' },
  },
];

const SERVICE_OPTIONS = [
  { code: 'TRS',  label: 'Tracked 48 (2–3 days)' },
  { code: 'TRM',  label: 'Tracked 24 (next day)' },
  { code: 'TRS1', label: 'Tracked 48 + Signature' },
  { code: 'TRM1', label: 'Tracked 24 + Signature' },
  { code: 'SD1',  label: 'Special Delivery by 1pm' },
  { code: 'SD9',  label: 'Special Delivery by 9am' },
  { code: 'MT7',  label: 'International Tracked' },
  { code: 'MTE',  label: 'International Tracked & Signed' },
  { code: 'MP9',  label: 'International Signed' },
];

const SENDER_DEFAULT = {
  name: '247 Commerce Ltd',
  company: '247 Commerce Ltd',
  address_line1: 'Unit 1 Warehouse Road',
  address_line2: '',
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

type PanelId = 'creds' | 'services' | 'rates' | 'shipment' | 'manifest';

interface PanelState { loading: boolean; result: unknown; error: string | null; }

function emptyPanel(): PanelState { return { loading: false, result: null, error: null }; }

// ─── Styles ───────────────────────────────────────────────────────────────────
const s = {
  page:    { padding: '24px 32px', maxWidth: 1100, margin: '0 auto', fontFamily: 'var(--font-sans, monospace)' } as React.CSSProperties,
  header:  { display: 'flex', alignItems: 'center', gap: 12, marginBottom: 8 } as React.CSSProperties,
  badge:   { background: '#ef444422', color: '#ef4444', border: '1px solid #ef444444', borderRadius: 6, padding: '2px 10px', fontSize: 12, fontWeight: 600 } as React.CSSProperties,
  obaBadge:{ background: '#f9731622', color: '#f97316', border: '1px solid #f9731644', borderRadius: 6, padding: '2px 10px', fontSize: 12, fontWeight: 600 } as React.CSSProperties,
  grid:    { display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 20, marginBottom: 20 } as React.CSSProperties,
  card:    { background: 'var(--surface-2, #111)', border: '1px solid var(--border, #222)', borderRadius: 10, overflow: 'hidden' } as React.CSSProperties,
  cardHdr: { padding: '12px 16px', borderBottom: '1px solid var(--border, #222)', background: 'var(--surface-3, #161616)', display: 'flex', alignItems: 'center', justifyContent: 'space-between' } as React.CSSProperties,
  cardBdy: { padding: 16 } as React.CSSProperties,
  label:   { display: 'block', fontSize: 11, color: 'var(--text-muted, #888)', marginBottom: 4, textTransform: 'uppercase' as const, letterSpacing: '0.05em' },
  input:   { width: '100%', background: 'var(--input-bg, #0d0d0d)', border: '1px solid var(--border, #333)', borderRadius: 6, padding: '7px 10px', fontSize: 13, color: 'var(--text, #e5e7eb)', boxSizing: 'border-box' as const },
  select:  { width: '100%', background: 'var(--input-bg, #0d0d0d)', border: '1px solid var(--border, #333)', borderRadius: 6, padding: '7px 10px', fontSize: 13, color: 'var(--text, #e5e7eb)', boxSizing: 'border-box' as const },
  btn:     { padding: '8px 18px', borderRadius: 7, border: 'none', cursor: 'pointer', fontSize: 13, fontWeight: 600, transition: 'opacity .15s' } as React.CSSProperties,
  btnRed:  { background: '#ef4444', color: '#fff' } as React.CSSProperties,
  btnGrey: { background: 'var(--surface-3, #1f1f1f)', color: 'var(--text-muted, #aaa)', border: '1px solid var(--border, #333)' } as React.CSSProperties,
  btnBlue: { background: '#3b82f6', color: '#fff' } as React.CSSProperties,
  row2:    { display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 10, marginBottom: 10 } as React.CSSProperties,
  row3:    { display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: 10, marginBottom: 10 } as React.CSSProperties,
  row4:    { display: 'grid', gridTemplateColumns: '1fr 1fr 1fr 1fr', gap: 10, marginBottom: 10 } as React.CSSProperties,
};

// ─── OBA Info Banner ──────────────────────────────────────────────────────────
function OBABanner() {
  return (
    <div style={{
      background: '#f9731611',
      border: '1px solid #f9731644',
      borderRadius: 8,
      padding: '12px 16px',
      marginBottom: 20,
      fontSize: 13,
      lineHeight: 1.6,
    }}>
      <strong style={{ color: '#f97316' }}>⚠️ OBA Account Notice</strong>
      <div style={{ marginTop: 6, color: 'var(--text-muted, #aaa)' }}>
        <strong>Without OBA:</strong> Orders are created in Click &amp; Drop and a tracking number is returned,
        but <strong>label PDF download requires an OBA-linked account (403 otherwise)</strong>.
        Labels must be printed from the{' '}
        <a href="https://parcel.royalmail.com" target="_blank" rel="noreferrer" style={{ color: '#f97316' }}>Click &amp; Drop web UI</a>.
        <br />
        <strong>With OBA:</strong> Full API flow — create order + download label PDF in one call.
        To link OBA, go to Click &amp; Drop → My Account → Your Profile → OBA Account Details.
      </div>
    </div>
  );
}

// ─── Address form ─────────────────────────────────────────────────────────────
function AddressForm({ value, onChange, title }: { value: Address; onChange: (a: Address) => void; title: string }) {
  const set = (k: keyof Address) => (e: React.ChangeEvent<HTMLInputElement>) => onChange({ ...value, [k]: e.target.value });
  return (
    <div>
      <div style={{ marginBottom: 8, fontSize: 12, fontWeight: 600, color: 'var(--text-muted, #888)', textTransform: 'uppercase', letterSpacing: '0.05em' }}>{title}</div>
      <div style={s.row2}>
        <div><label style={s.label}>Name</label><input style={s.input} value={value.name} onChange={set('name')} /></div>
        <div><label style={s.label}>Company</label><input style={s.input} value={value.company} onChange={set('company')} /></div>
      </div>
      <div style={{ marginBottom: 10 }}><label style={s.label}>Address Line 1</label><input style={s.input} value={value.address_line1} onChange={set('address_line1')} /></div>
      <div style={{ marginBottom: 10 }}><label style={s.label}>Address Line 2</label><input style={s.input} value={value.address_line2} onChange={set('address_line2')} /></div>
      <div style={s.row3}>
        <div><label style={s.label}>City</label><input style={s.input} value={value.city} onChange={set('city')} /></div>
        <div><label style={s.label}>Postcode</label><input style={s.input} value={value.postal_code} onChange={set('postal_code')} /></div>
        <div><label style={s.label}>Country (ISO)</label><input style={s.input} value={value.country} onChange={set('country')} maxLength={2} /></div>
      </div>
      <div style={s.row2}>
        <div><label style={s.label}>Phone</label><input style={s.input} value={value.phone} onChange={set('phone')} /></div>
        <div><label style={s.label}>Email</label><input style={s.input} value={value.email} onChange={set('email')} /></div>
      </div>
    </div>
  );
}

// ─── Result panel ─────────────────────────────────────────────────────────────
function ResultPanel({ p }: { p: PanelState }) {
  if (p.loading) return <div style={{ padding: 20, textAlign: 'center', color: 'var(--text-muted)' }}>⏳ Calling API…</div>;
  if (!p.result && !p.error) return null;

  // If result contains a label_url with base64 PDF, show download button
  const result = p.result as Record<string, unknown> | null;
  const labelUrl = result?.label_url as string | undefined;
  const trackingNumber = result?.tracking_number as string | undefined;

  return (
    <div>
      {(trackingNumber || labelUrl) && (
        <div style={{ display: 'flex', gap: 10, marginBottom: 10, flexWrap: 'wrap' }}>
          {trackingNumber && (
            <a
              href={`https://www.royalmail.com/track-your-item#/tracking-results/${trackingNumber}`}
              target="_blank"
              rel="noreferrer"
              style={{ ...s.btn, ...s.btnGrey, textDecoration: 'none', fontSize: 12 }}
            >
              🔍 Track on Royal Mail
            </a>
          )}
          {labelUrl && labelUrl.startsWith('data:application/pdf') && (
            <a
              href={labelUrl}
              download={`label-${trackingNumber || 'rm'}.pdf`}
              style={{ ...s.btn, ...s.btnRed, textDecoration: 'none', fontSize: 12 }}
            >
              ⬇️ Download Label PDF
            </a>
          )}
          {!labelUrl && !p.error && (
            <div style={{ fontSize: 12, color: '#f97316', padding: '8px 0' }}>
              ⚠️ No label returned — OBA not linked. Print from{' '}
              <a href="https://parcel.royalmail.com" target="_blank" rel="noreferrer" style={{ color: '#f97316' }}>Click &amp; Drop</a>.
            </div>
          )}
        </div>
      )}
      <div style={{
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
    </div>
  );
}

// ─── JSON Preview ──────────────────────────────────────────────────────────────
function JsonPreview({ toAddr, parcel, serviceCode, reference, signature, saturdayDelivery }: {
  toAddr: Address; parcel: Parcel; serviceCode: string; reference: string;
  signature: boolean; saturdayDelivery: boolean;
}) {
  const weightGrams = Math.round(parcel.weight * 1000);
  const isIntl = toAddr.country !== 'GB';

  const payload = {
    "POST https://api.parcel.royalmail.com/api/v1/orders": null,
    "Authorization": "Bearer <your-api-key>",
    "body": {
      items: [{
        orderReference: reference || undefined,
        recipient: {
          name: toAddr.name || '?',
          companyName: toAddr.company || undefined,
          addressLine1: toAddr.address_line1 || '?',
          addressLine2: toAddr.address_line2 || undefined,
          city: toAddr.city || '?',
          postcode: toAddr.postal_code || '?',
          countryCode: toAddr.country || 'GB',
          phoneNumber: toAddr.phone || undefined,
          emailAddress: toAddr.email || undefined,
        },
        packages: [{
          weightInGrams: weightGrams || 500,
          packageFormatIdentifier: weightGrams <= 100 ? 'Letter' : weightGrams <= 750 ? 'LargeLetter' : weightGrams <= 2000 ? 'SmallParcel' : 'Parcel',
          ...(isIntl ? {
            contents: [{
              name: parcel.description || 'Goods',
              quantity: 1,
              unitValue: 10.00,
              customsDescription: parcel.description || 'Goods',
              originCountryCode: 'GBR',
            }]
          } : {}),
        }],
        postage: {
          serviceCode: serviceCode || '?',
          ...(signature ? { confirmation: 'signature' } : {}),
          ...(saturdayDelivery ? { saturdayGuaranteed: true } : {}),
        },
      }]
    }
  };

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
      {JSON.stringify(payload, null, 2)}
    </div>
  );
}

// ─── Main component ───────────────────────────────────────────────────────────
export default function RoyalMailApiTester() {
  // Credentials
  const [apiKey, setApiKey]             = useState('');
  const [isOba, setIsOba]               = useState(false);
  const [tradingName, setTradingName]   = useState('');

  // Addresses
  const [fromAddr, setFromAddr] = useState<Address>(SENDER_DEFAULT);
  const [toAddr, setToAddr]     = useState<Address>(ADDRESS_PRESETS[0].address);

  // Shipment options
  const [parcel, setParcel]           = useState<Parcel>({ weight: 0.5, length: 20, width: 15, height: 10, description: 'Test parcel' });
  const [serviceCode, setServiceCode] = useState('TRS');
  const [reference, setReference]     = useState('TEST-RM-001');
  const [signature, setSignature]     = useState(false);
  const [saturdayDelivery, setSaturdayDelivery] = useState(false);

  // Tracking
  const [trackingNumber, setTrackingNumber] = useState('');

  // Panel states
  const [panels, setPanels] = useState<Record<PanelId, PanelState>>({
    creds: emptyPanel(), services: emptyPanel(), rates: emptyPanel(),
    shipment: emptyPanel(), manifest: emptyPanel(),
  });

  function setPanel(id: PanelId, patch: Partial<PanelState>) {
    setPanels(p => ({ ...p, [id]: { ...p[id], ...patch } }));
  }

  function buildCreds() {
    return {
      carrier_id: 'royal-mail',
      api_key: apiKey,
      is_sandbox: false,
      extra: {
        is_oba_linked: isOba,
        trading_name: tradingName,
      },
    };
  }

  // ── Test credentials ────────────────────────────────────────────────────────
  async function testCreds() {
    setPanel('creds', { loading: true, result: null, error: null });
    try {
      const r = await fetch(`${API}/dispatch/carriers/royal-mail/test`, {
        method: 'POST',
        headers: apiHeaders(),
        body: JSON.stringify(buildCreds()),
      });
      const data = await r.json();
      if (!r.ok) setPanel('creds', { loading: false, result: null, error: data.error || r.statusText });
      else setPanel('creds', { loading: false, result: data, error: null });
    } catch (e: unknown) {
      setPanel('creds', { loading: false, result: null, error: String(e) });
    }
  }

  // ── List services ───────────────────────────────────────────────────────────
  async function listServices() {
    setPanel('services', { loading: true, result: null, error: null });
    try {
      const r = await fetch(`${API}/dispatch/carriers/royal-mail/services`, {
        headers: { ...apiHeaders(), 'X-Carrier-Api-Key': apiKey },
      });
      const data = await r.json();
      if (!r.ok) setPanel('services', { loading: false, result: null, error: data.error || r.statusText });
      else setPanel('services', { loading: false, result: data, error: null });
    } catch (e: unknown) {
      setPanel('services', { loading: false, result: null, error: String(e) });
    }
  }

  // ── Get rates ───────────────────────────────────────────────────────────────
  async function getRates() {
    setPanel('rates', { loading: true, result: null, error: null });
    try {
      const r = await fetch(`${API}/dispatch/rates`, {
        method: 'POST',
        headers: apiHeaders(),
        body: JSON.stringify({
          carrier_id: 'royal-mail',
          credentials: buildCreds(),
          from_address: fromAddr,
          to_address: toAddr,
          parcels: [parcel],
        }),
      });
      const data = await r.json();
      if (!r.ok) setPanel('rates', { loading: false, result: null, error: data.error || r.statusText });
      else setPanel('rates', { loading: false, result: data, error: null });
    } catch (e: unknown) {
      setPanel('rates', { loading: false, result: null, error: String(e) });
    }
  }

  // ── Create shipment ─────────────────────────────────────────────────────────
  async function createShipment() {
    setPanel('shipment', { loading: true, result: null, error: null });
    try {
      const r = await fetch(`${API}/dispatch/shipments`, {
        method: 'POST',
        headers: apiHeaders(),
        body: JSON.stringify({
          carrier_id: 'royal-mail',
          credentials: buildCreds(),
          service_code: serviceCode,
          from_address: fromAddr,
          to_address: toAddr,
          parcels: [parcel],
          reference,
          options: {
            signature,
            saturday_delivery: saturdayDelivery,
          },
        }),
      });
      const data = await r.json();
      if (!r.ok) setPanel('shipment', { loading: false, result: null, error: data.error || r.statusText });
      else {
        setPanel('shipment', { loading: false, result: data, error: null });
        if (data.tracking_number) setTrackingNumber(data.tracking_number);
      }
    } catch (e: unknown) {
      setPanel('shipment', { loading: false, result: null, error: String(e) });
    }
  }

  // ── Generate manifest ───────────────────────────────────────────────────────
  async function generateManifest() {
    setPanel('manifest', { loading: true, result: null, error: null });
    try {
      const r = await fetch(`${API}/dispatch/manifests`, {
        method: 'POST',
        headers: apiHeaders(),
        body: JSON.stringify({
          carrier_id: 'royal-mail',
          credentials: buildCreds(),
        }),
      });
      const data = await r.json();
      if (!r.ok) setPanel('manifest', { loading: false, result: null, error: data.error || r.statusText });
      else setPanel('manifest', { loading: false, result: data, error: null });
    } catch (e: unknown) {
      setPanel('manifest', { loading: false, result: null, error: String(e) });
    }
  }

  return (
    <div style={s.page}>

      {/* ── Header ──────────────────────────────────────────────────────────── */}
      <div style={s.header}>
        <span style={{ fontSize: 24 }}>📮</span>
        <div>
          <h2 style={{ margin: 0, fontSize: 20 }}>Royal Mail Click &amp; Drop API Tester</h2>
          <div style={{ fontSize: 12, color: 'var(--text-muted, #888)', marginTop: 2 }}>
            api.parcel.royalmail.com/api/v1 — Bearer token auth
          </div>
        </div>
        <span style={s.badge}>Dev Only</span>
        <span style={s.obaBadge}>OBA Required for Labels</span>
      </div>

      <OBABanner />

      {/* ── Section 1: Credentials + Services ───────────────────────────────── */}
      <div style={s.grid}>

        {/* Credentials card */}
        <div style={s.card}>
          <div style={s.cardHdr}>
            <strong>🔑 Credentials</strong>
            <button style={{ ...s.btn, ...s.btnRed }} onClick={testCreds} disabled={panels.creds.loading}>
              {panels.creds.loading ? '⏳ Testing…' : 'Test Connection'}
            </button>
          </div>
          <div style={s.cardBdy}>
            <div style={{ marginBottom: 10 }}>
              <label style={s.label}>API Key</label>
              <input style={s.input} type="password" value={apiKey} onChange={e => setApiKey(e.target.value)} placeholder="From Click & Drop → Settings → Integrations → Click & Drop API" />
            </div>
            <div style={{ marginBottom: 10 }}>
              <label style={s.label}>Trading Name (optional — overrides account default)</label>
              <input style={s.input} value={tradingName} onChange={e => setTradingName(e.target.value)} placeholder="e.g. My Shop Name" />
            </div>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 10 }}>
              <input type="checkbox" id="oba" checked={isOba} onChange={e => setIsOba(e.target.checked)} />
              <label htmlFor="oba" style={{ fontSize: 13, cursor: 'pointer' }}>
                OBA account linked <span style={{ color: 'var(--text-muted, #888)', fontSize: 12 }}>(enables label PDF download)</span>
              </label>
            </div>
            {!isOba && (
              <div style={{ fontSize: 12, color: '#f97316', background: '#f9731611', borderRadius: 6, padding: '8px 10px', marginBottom: 10 }}>
                Without OBA: orders will be created but labels must be printed from parcel.royalmail.com
              </div>
            )}
            <ResultPanel p={panels.creds} />
          </div>
        </div>

        {/* Services card */}
        <div style={s.card}>
          <div style={s.cardHdr}>
            <strong>📋 Available Services</strong>
            <button style={{ ...s.btn, ...s.btnGrey }} onClick={listServices} disabled={panels.services.loading}>
              {panels.services.loading ? '⏳…' : 'List Services'}
            </button>
          </div>
          <div style={s.cardBdy}>
            <div style={{ fontSize: 12, color: 'var(--text-muted, #888)', marginBottom: 12 }}>
              Returns the standard Click &amp; Drop service list. Note: available services depend on your OBA contract —
              the API does not expose a live services endpoint, so these are the standard options.
            </div>
            <ResultPanel p={panels.services} />
          </div>
        </div>

      </div>

      {/* ── Section 2: Addresses + Parcel ───────────────────────────────────── */}
      <div style={{ ...s.card, marginBottom: 20 }}>
        <div style={s.cardHdr}>
          <strong>📍 Addresses &amp; Parcel</strong>
          <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
            <label style={{ fontSize: 12, color: 'var(--text-muted, #888)' }}>Preset:</label>
            <select style={{ ...s.select, width: 220 }} onChange={e => {
              const preset = ADDRESS_PRESETS.find(p => p.label === e.target.value);
              if (preset) setToAddr(preset.address);
            }}>
              {ADDRESS_PRESETS.map(p => <option key={p.label}>{p.label}</option>)}
            </select>
          </div>
        </div>
        <div style={{ ...s.cardBdy, display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 24 }}>
          <AddressForm value={fromAddr} onChange={setFromAddr} title="From (Sender)" />
          <AddressForm value={toAddr} onChange={setToAddr} title="To (Recipient)" />
        </div>
        <div style={{ ...s.cardBdy, borderTop: '1px solid var(--border, #222)' }}>
          <div style={{ marginBottom: 8, fontSize: 12, fontWeight: 600, color: 'var(--text-muted, #888)', textTransform: 'uppercase', letterSpacing: '0.05em' }}>Parcel</div>
          <div style={s.row4}>
            <div><label style={s.label}>Weight (kg)</label><input style={s.input} type="number" step="0.1" value={parcel.weight} onChange={e => setParcel(p => ({ ...p, weight: +e.target.value }))} /></div>
            <div><label style={s.label}>Length (cm)</label><input style={s.input} type="number" value={parcel.length} onChange={e => setParcel(p => ({ ...p, length: +e.target.value }))} /></div>
            <div><label style={s.label}>Width (cm)</label><input style={s.input} type="number" value={parcel.width} onChange={e => setParcel(p => ({ ...p, width: +e.target.value }))} /></div>
            <div><label style={s.label}>Height (cm)</label><input style={s.input} type="number" value={parcel.height} onChange={e => setParcel(p => ({ ...p, height: +e.target.value }))} /></div>
          </div>
          <div><label style={s.label}>Description / Contents</label><input style={s.input} value={parcel.description} onChange={e => setParcel(p => ({ ...p, description: e.target.value }))} /></div>
        </div>
      </div>

      {/* ── Section 3: Rates + Shipment + Manifest ──────────────────────────── */}
      <div style={s.grid}>

        {/* Rates card */}
        <div style={s.card}>
          <div style={s.cardHdr}>
            <strong>💰 Get Rates</strong>
            <button style={{ ...s.btn, ...s.btnGrey }} onClick={getRates} disabled={panels.rates.loading}>
              {panels.rates.loading ? '⏳…' : 'Get Rates'}
            </button>
          </div>
          <div style={s.cardBdy}>
            <div style={{ fontSize: 12, color: 'var(--text-muted, #888)', marginBottom: 12 }}>
              Returns indicative rates. Click &amp; Drop has no live rating API — actual cost is determined by your OBA contract.
            </div>
            <ResultPanel p={panels.rates} />
          </div>
        </div>

        {/* Manifest card */}
        <div style={s.card}>
          <div style={s.cardHdr}>
            <strong>📦 Generate Manifest</strong>
            <button style={{ ...s.btn, ...s.btnGrey }} onClick={generateManifest} disabled={panels.manifest.loading}>
              {panels.manifest.loading ? '⏳…' : 'Manifest All Eligible'}
            </button>
          </div>
          <div style={s.cardBdy}>
            <div style={{ fontSize: 12, color: 'var(--text-muted, #888)', marginBottom: 12 }}>
              Manifests <strong>all eligible orders</strong> on your account (status: "Label Generated" or "Despatched").
              Run at end of day before collection. PDF manifest is returned if immediately available,
              otherwise poll <code>GET /manifests/&#123;id&#125;</code>.
            </div>
            <ResultPanel p={panels.manifest} />
          </div>
        </div>

      </div>

      {/* ── Section 4: Create Shipment ───────────────────────────────────────── */}
      <div style={{ ...s.card, marginBottom: 20 }}>
        <div style={s.cardHdr}>
          <strong>🚀 Create Shipment</strong>
          <button style={{ ...s.btn, ...s.btnRed }} onClick={createShipment} disabled={panels.shipment.loading}>
            {panels.shipment.loading ? '⏳ Creating…' : 'Create Shipment'}
          </button>
        </div>
        <div style={s.cardBdy}>
          <div style={s.row3}>
            <div>
              <label style={s.label}>Service</label>
              <select style={s.select} value={serviceCode} onChange={e => setServiceCode(e.target.value)}>
                {SERVICE_OPTIONS.map(o => <option key={o.code} value={o.code}>{o.label}</option>)}
              </select>
            </div>
            <div>
              <label style={s.label}>Reference</label>
              <input style={s.input} value={reference} onChange={e => setReference(e.target.value)} />
            </div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 8, justifyContent: 'flex-end', paddingBottom: 2 }}>
              <label style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 13, cursor: 'pointer' }}>
                <input type="checkbox" checked={signature} onChange={e => setSignature(e.target.checked)} />
                Signature required
              </label>
              <label style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 13, cursor: 'pointer' }}>
                <input type="checkbox" checked={saturdayDelivery} onChange={e => setSaturdayDelivery(e.target.checked)} />
                Saturday delivery
              </label>
            </div>
          </div>
          {!isOba && (
            <div style={{ fontSize: 12, color: '#f97316', background: '#f9731611', borderRadius: 6, padding: '8px 10px', marginBottom: 12 }}>
              ⚠️ Without OBA: order will be created and tracking number returned, but label PDF will not be available here.
              Print from <a href="https://parcel.royalmail.com" target="_blank" rel="noreferrer" style={{ color: '#f97316' }}>parcel.royalmail.com</a>.
            </div>
          )}
          {trackingNumber && (
            <div style={{ display: 'flex', gap: 10, marginBottom: 12 }}>
              <div style={{ flex: 1 }}>
                <label style={s.label}>Last Tracking Number</label>
                <input style={s.input} value={trackingNumber} onChange={e => setTrackingNumber(e.target.value)} />
              </div>
              <a
                href={`https://www.royalmail.com/track-your-item#/tracking-results/${trackingNumber}`}
                target="_blank"
                rel="noreferrer"
                style={{ ...s.btn, ...s.btnGrey, textDecoration: 'none', alignSelf: 'flex-end' }}
              >
                🔍 Track
              </a>
            </div>
          )}
          <ResultPanel p={panels.shipment} />
        </div>
      </div>

      {/* ── Section 5: JSON Payload Preview ─────────────────────────────────── */}
      <div style={s.card}>
        <div style={s.cardHdr}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <span>🔍</span><strong>Expected JSON Payload Preview</strong>
            <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>— what carrier_royal_mail.go will POST to api.parcel.royalmail.com</span>
          </div>
        </div>
        <div style={s.cardBdy}>
          <JsonPreview
            toAddr={toAddr}
            parcel={parcel}
            serviceCode={serviceCode}
            reference={reference}
            signature={signature}
            saturdayDelivery={saturdayDelivery}
          />
        </div>
      </div>

    </div>
  );
}

import { useState, useEffect, useCallback, useRef } from 'react';
// A-006: Page-builder serialiser for client-side invoice rendering
// eslint-disable-next-line @typescript-eslint/ban-ts-comment
// @ts-ignore – JS module, no types
import { generateFullHTML } from '../components/pagebuilder/serialisers/htmlSerialiser.jsx';
// eslint-disable-next-line @typescript-eslint/ban-ts-comment
// @ts-ignore
import { THEME_PRESETS } from '../components/pagebuilder/constants/index.jsx';
import {
  Download, RefreshCw, Printer, XCircle, FileText,
  AlertCircle, Lock, Unlock, Search, Filter,
  ChevronLeft, ChevronRight, Plus, GitMerge, Scissors,
  Edit2, X, Save, Columns, Mail, Send
} from 'lucide-react';
import { getActiveTenantId } from '../contexts/TenantContext';
import { getAuth } from 'firebase/auth';
import OrderActionsMenu from '../components/orders/OrderActionsMenu';
import './Orders.css';

// ─── Authenticated fetch helper (module-level) ────────────────────────────────
// Gets a fresh Firebase token + correct tenant ID on every call.
async function _getOrdersAuthHeaders(tenantId: string): Promise<Record<string, string>> {
  let token = '';
  try {
    const user = getAuth().currentUser;
    if (user) token = await user.getIdToken();
  } catch { /* ignore */ }
  return {
    'Content-Type': 'application/json',
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
  };
}

// ─── Types ────────────────────────────────────────────────────────────────────

interface LineItem {
  line_id?: string;
  title?: string;
  sku?: string;
  quantity?: number;
  line_total?: { amount: number; currency: string };
  unit_price?: { amount: number; currency: string };
  tax?: { amount: number; currency: string };
  tax_rate?: number; // Task 13: e.g. 0.20 for 20%
  status?: string;
}

interface RawOrder {
  order_id?: string;
  id?: string;
  external_order_id?: string;
  order_number?: string;
  channel?: string;
  channel_account_id?: string;
  status?: string;
  payment_status?: string;
  on_hold?: boolean;
  hold_reason?: string;
  sla_at_risk?: boolean;
  order_date?: string;
  created_at?: string;
  imported_at?: string;
  // Task 2: Despatch-By Date
  despatch_by_date?: string;
  // Task 3: Scheduled Delivery Date
  scheduled_delivery_date?: string;
  // Task 6: Tags
  tags?: string[];
  // Fix 1B: Folder assignment
  folder_id?: string;
  folder_name?: string;
  // Fix 1C: Identifier (free-text reference code)
  identifier?: string;
  // Fix 1D: Warehouse location and fulfilment centre overrides
  warehouse_location_id?: string;
  warehouse_location_name?: string;
  fulfilment_center_id?: string;
  fulfilment_center_name?: string;
  // Task 10: Invoice print status
  invoice_printed?: boolean;
  invoice_printed_at?: string;
  // Task 11: Shipping service
  shipping_service?: string;
  carrier?: string;
  customer?: { name?: string; email?: string };
  shipping_address?: {
    name?: string;
    address_line1?: string;
    city?: string;
    country?: string;
    postal_code?: string;
  };
  totals?: {
    grand_total?: { amount: number; currency: string };
    subtotal?: { amount: number; currency: string };
    shipping?: { amount: number; currency: string };
    discount?: { amount: number; currency: string };
    tax?: { amount: number; currency: string };
    postage_tax?: { amount: number; currency: string };
  };
  lines?: LineItem[];
  line_items?: LineItem[];
  items?: LineItem[];
  label_generated?: boolean;
  label_url?: string;
  tracking_number?: string;
  tenant_id?: string;
  internal_notes?: string;
}

interface LocalState {
  package_format: string;
  shipping_service: string;
  on_hold: boolean;
  hold_reason: string;
  label_generated: boolean;
  label_url?: string;
  tracking_number?: string;
  lines: LineItem[];
  lines_loaded: boolean;
}

const PACKAGE_OPTIONS = ['Parcel', 'Large Letter', 'Letter'];
const SHIPPING_OPTIONS = [
  'Royal Mail 1st Class',
  'Royal Mail 2nd Class',
  'Royal Mail Special Delivery',
  'DPD Next Day',
];
const HOLD_REASONS = [
  'Awaiting payment',
  'Stock issue',
  'Customer request',
  'Address query',
  'Damaged goods',
  'Other',
];

// ─── Helpers ──────────────────────────────────────────────────────────────────

function getOrderId(o: RawOrder): string {
  return o.order_id || o.id || o.external_order_id || '';
}

function getDisplayRef(o: RawOrder): string {
  return o.external_order_id || o.order_number || o.order_id || o.id || '—';
}

function getCustomerDisplay(o: RawOrder): { name: string; location: string } {
  const name = o.shipping_address?.name?.trim() || o.customer?.name?.trim() || '';
  const city = o.shipping_address?.city?.trim() || '';
  const postcode = o.shipping_address?.postal_code?.trim() || '';
  const location = [city, postcode].filter(Boolean).join(', ');
  return { name, location };
}

function getGrandTotal(o: RawOrder): number | null {
  const amt = o.totals?.grand_total?.amount;
  return amt != null ? amt : null;
}

function getCurrency(o: RawOrder): string {
  return o.totals?.grand_total?.currency || 'GBP';
}

function getLines(o: RawOrder, loc?: LocalState): LineItem[] {
  if (loc?.lines && loc.lines.length > 0) return loc.lines;
  return o.lines || o.line_items || o.items || [];
}

function fmtMoney(amount: number | null, currency: string): string {
  if (amount === null || amount === undefined) return '—';
  return new Intl.NumberFormat('en-GB', {
    style: 'currency',
    currency: currency || 'GBP',
    minimumFractionDigits: 2,
  }).format(amount);
}

function fmtDate(s?: string): { main: string; year: string; time: string } {
  if (!s) return { main: '—', year: '', time: '' };
  const d = new Date(s);
  if (isNaN(d.getTime())) return { main: '—', year: '', time: '' };
  return {
    main: d.toLocaleDateString('en-GB', { day: 'numeric', month: 'short' }),
    year: String(d.getFullYear()),
    time: d.toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit' }),
  };
}

function defaultLocal(o: RawOrder): LocalState {
  return {
    package_format: 'Parcel',
    shipping_service: 'Royal Mail 2nd Class',
    on_hold: o.on_hold ?? false,
    hold_reason: o.hold_reason ?? '',
    label_generated: o.label_generated ?? false,
    label_url: o.label_url,
    tracking_number: o.tracking_number,
    lines: o.lines || o.line_items || o.items || [],
    lines_loaded: (o.lines || o.line_items || o.items || []).length > 0,
  };
}

// ─── S4 Channel Config ────────────────────────────────────────────────────────

const S4_CHANNELS = [
  { id: 'backmarket', label: 'Back Market', icon: '♻️', color: '#14B8A6', importPath: '/backmarket/orders/import' },
  { id: 'zalando',    label: 'Zalando',     icon: '👗', color: '#FF6600', importPath: '/zalando/orders/import' },
  { id: 'bol',        label: 'Bol.com',     icon: '🏪', color: '#0E4299', importPath: '/bol/orders/import' },
  { id: 'lazada',     label: 'Lazada',      icon: '🛒', color: '#F57224', importPath: '/lazada/orders/import' },
];

// ─── S4ChannelTabs ────────────────────────────────────────────────────────────

const S4ChannelTabs = ({
  activeChannel, onSelect, tenantId, apiBase,
}: {
  activeChannel: string;
  onSelect: (ch: string) => void;
  tenantId: string;
  apiBase: string;
}) => {
  const [counts, setCounts] = useState<Record<string, number>>({});
  const [importing, setImporting] = useState<Record<string, boolean>>({});
  const [exporting, setExporting] = useState<Record<string, boolean>>({});
  const [credentials, setCredentials] = useState<Record<string, Array<{ credential_id: string; account_name: string }>>>({});

  // Authenticated fetch for this sub-component
  const s4AuthFetch = useCallback(async (url: string, init?: RequestInit) => {
    const headers = await _getOrdersAuthHeaders(tenantId);
    return fetch(url, { ...init, headers: { ...headers, ...(init?.headers || {}) } });
  }, [tenantId]);

  // Fetch order counts per S4 channel and their credentials
  useEffect(() => {
    async function load() {
      // Load credentials to know which channels are connected
      try {
        const res = await s4AuthFetch(`${apiBase}/marketplace/credentials`);
        if (res.ok) {
          const data = await res.json();
          const creds: Record<string, Array<{ credential_id: string; account_name: string }>> = {};
          for (const ch of S4_CHANNELS) {
            creds[ch.id] = (data.data || [])
              .filter((c: any) => c.channel === ch.id && c.active)
              .map((c: any) => ({ credential_id: c.credential_id, account_name: c.account_name || c.channel }));
          }
          setCredentials(creds);
        }
      } catch { /* silent */ }

      // Fetch per-channel order counts
      const newCounts: Record<string, number> = {};
      await Promise.all(
        S4_CHANNELS.map(async ch => {
          try {
            const res = await s4AuthFetch(`${apiBase}/orders?channel=${ch.id}&limit=1&offset=0`);
            if (res.ok) {
              const data = await res.json();
              newCounts[ch.id] = data.total ?? data.count ?? data.pagination?.total ?? 0;
            }
          } catch { /* silent */ }
        }),
      );
      setCounts(newCounts);
    }
    load();
  }, [apiBase, tenantId]);

  const triggerImport = async (ch: typeof S4_CHANNELS[0]) => {
    const creds = credentials[ch.id];
    if (!creds || creds.length === 0) {
      alert(`No active ${ch.label} credentials found. Connect a ${ch.label} account in Channel Connections first.`);
      return;
    }
    setImporting(prev => ({ ...prev, [ch.id]: true }));
    let imported = 0;
    let errors: string[] = [];
    await Promise.all(
      creds.map(async cred => {
        try {
          const res = await s4AuthFetch(`${apiBase}${ch.importPath}`, {
            method: 'POST',
            body: JSON.stringify({ credential_id: cred.credential_id }),
          });
          if (res.ok) {
            const data = await res.json();
            imported += data.imported ?? data.count ?? 0;
          } else {
            const e = await res.json().catch(() => ({}));
            errors.push(e.error || `Error ${res.status}`);
          }
        } catch (err: any) {
          errors.push(err.message || 'Unknown error');
        }
      }),
    );
    setImporting(prev => ({ ...prev, [ch.id]: false }));
    if (errors.length > 0) {
      alert(`Import completed with errors:\n${errors.join('\n')}`);
    } else {
      // Refresh count for this channel
      try {
        const res = await s4AuthFetch(`${apiBase}/orders?channel=${ch.id}&limit=1&offset=0`);
        if (res.ok) {
          const data = await res.json();
          setCounts(prev => ({ ...prev, [ch.id]: data.total ?? data.count ?? 0 }));
        }
      } catch { /* silent */ }
    }
  };

  const triggerExport = async (ch: typeof S4_CHANNELS[0]) => {
    if (exporting[ch.id]) return;
    setExporting(prev => ({ ...prev, [ch.id]: true }));
    try {
      const res = await fetch(
        `${apiBase}/${ch.id}/orders/bulk/export`,
      );
      if (!res.ok) {
        const e = await res.json().catch(() => ({}));
        alert(`Export failed: ${e.error || `HTTP ${res.status}`}`);
        return;
      }
      const data = await res.json();
      if (!data.headers || !data.rows) {
        alert('Export returned no data.');
        return;
      }
      // Build CSV client-side from headers + rows
      const lines = [data.headers, ...data.rows].map((row: string[]) =>
        row.map((cell: string) => `"${(cell ?? '').replace(/"/g, '""')}"`).join(','),
      );
      const csv = lines.join('\n');
      const blob = new Blob([csv], { type: 'text/csv;charset=utf-8;' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = (data.filename || `${ch.id}_orders.xlsx`).replace('.xlsx', '.csv');
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
    } catch (err: any) {
      alert(`Export error: ${err.message || 'Unknown error'}`);
    } finally {
      setExporting(prev => ({ ...prev, [ch.id]: false }));
    }
  };

  const isS4Active = S4_CHANNELS.some(ch => ch.id === activeChannel);

  return (
    <div style={{
      display: 'flex', alignItems: 'center', gap: 8, marginBottom: 12,
      padding: '10px 14px',
      background: 'var(--bg-secondary)',
      border: '1px solid var(--border)',
      borderRadius: 10,
      flexWrap: 'wrap',
    }}>
      <span style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.08em', marginRight: 4, flexShrink: 0 }}>
        New Channels
      </span>

      {/* "All" de-select pill */}
      {isS4Active && (
        <button
          onClick={() => onSelect('')}
          style={{
            fontSize: 12, padding: '4px 12px', borderRadius: 20, cursor: 'pointer',
            background: 'var(--bg-elevated)', border: '1px solid var(--border)',
            color: 'var(--text-muted)', fontWeight: 500,
          }}
        >
          ← All channels
        </button>
      )}

      {S4_CHANNELS.map(ch => {
        const isActive = activeChannel === ch.id;
        const count = counts[ch.id];
        const isImporting = importing[ch.id];
        const connected = (credentials[ch.id] || []).length > 0;

        return (
          <div key={ch.id} style={{ display: 'flex', alignItems: 'center', gap: 0, borderRadius: 20, overflow: 'hidden', border: `1px solid ${isActive ? ch.color : 'var(--border)'}` }}>
            {/* Channel filter button */}
            <button
              onClick={() => onSelect(isActive ? '' : ch.id)}
              title={connected ? `Filter to ${ch.label} orders` : `${ch.label} — no active credentials`}
              style={{
                display: 'flex', alignItems: 'center', gap: 6,
                padding: '5px 12px', cursor: 'pointer', border: 'none',
                background: isActive ? ch.color : 'var(--bg-elevated)',
                color: isActive ? '#fff' : connected ? 'var(--text-primary)' : 'var(--text-muted)',
                fontSize: 13, fontWeight: isActive ? 700 : 500, transition: 'all 0.15s',
              }}
            >
              <span>{ch.icon}</span>
              <span>{ch.label}</span>
              {count !== undefined && (
                <span style={{
                  background: isActive ? 'rgba(255,255,255,0.25)' : 'var(--bg-secondary)',
                  color: isActive ? '#fff' : 'var(--text-muted)',
                  borderRadius: 10, padding: '0 6px', fontSize: 11, fontWeight: 700,
                }}>
                  {count}
                </span>
              )}
              {!connected && <span title="Not connected" style={{ fontSize: 10, opacity: 0.6 }}>⚠️</span>}
            </button>

            {/* Import trigger button */}
            <button
              onClick={e => { e.stopPropagation(); triggerImport(ch); }}
              disabled={isImporting || !connected}
              title={connected ? `Import latest ${ch.label} orders now` : `Connect ${ch.label} in Channel Connections first`}
              style={{
                padding: '5px 8px', cursor: connected ? 'pointer' : 'not-allowed',
                border: 'none', borderLeft: `1px solid ${isActive ? 'rgba(255,255,255,0.3)' : 'var(--border)'}`,
                background: isActive ? ch.color : 'var(--bg-elevated)',
                color: isActive ? 'rgba(255,255,255,0.8)' : connected ? 'var(--text-muted)' : 'var(--text-muted)',
                fontSize: 13, opacity: isImporting ? 0.5 : connected ? 1 : 0.4,
                transition: 'all 0.15s',
              }}
            >
              {isImporting ? '⏳' : '⬇'}
            </button>

            {/* Export trigger button */}
            <button
              onClick={e => { e.stopPropagation(); triggerExport(ch); }}
              disabled={exporting[ch.id] || (counts[ch.id] ?? 0) === 0}
              title={(counts[ch.id] ?? 0) > 0 ? `Export ${ch.label} orders to CSV` : `No ${ch.label} orders to export`}
              style={{
                padding: '5px 8px', cursor: (counts[ch.id] ?? 0) > 0 ? 'pointer' : 'not-allowed',
                border: 'none', borderLeft: `1px solid ${isActive ? 'rgba(255,255,255,0.3)' : 'var(--border)'}`,
                background: isActive ? ch.color : 'var(--bg-elevated)',
                color: isActive ? 'rgba(255,255,255,0.8)' : (counts[ch.id] ?? 0) > 0 ? 'var(--text-muted)' : 'var(--text-muted)',
                fontSize: 13, opacity: exporting[ch.id] ? 0.5 : (counts[ch.id] ?? 0) > 0 ? 1 : 0.3,
                transition: 'all 0.15s',
              }}
            >
              {exporting[ch.id] ? '⏳' : '⬆'}
            </button>
          </div>
        );
      })}

      <span style={{ fontSize: 11, color: 'var(--text-muted)', marginLeft: 'auto', flexShrink: 0 }}>
        Click to filter · ⬇ import · ⬆ export CSV
      </span>
    </div>
  );
};

// ─── Component ────────────────────────────────────────────────────────────────

// ─── OrderDetailModal ────────────────────────────────────────────────────────
const OrderDetailModal = ({
  order, loc, onClose, onPrintLabel, onPrintPackingSlip,
}: {
  order: RawOrder;
  loc: LocalState;
  onClose: () => void;
  onPrintLabel: (o: RawOrder) => void;
  onPrintPackingSlip: (o: RawOrder) => void;
}) => {
  const dId = getOrderId(order);
  const lines = getLines(order, loc);
  const total = getGrandTotal(order);
  const currency = getCurrency(order);
  const { name: custName } = getCustomerDisplay(order);
  const addr = order.shipping_address;
  const df = fmtDate(order.order_date);
  // S2-Task7: tab state for the detail drawer
  const [activeTab, setActiveTab] = useState<'details' | 'lines' | 'activity' | 'notes'>('details');

  // Email customer state
  const [showEmailPanel, setShowEmailPanel] = useState(false);
  const [emailTemplates, setEmailTemplates] = useState<Array<{ id: string; name: string }>>([]);
  const [selectedTemplateId, setSelectedTemplateId] = useState('');
  const [emailTo, setEmailTo] = useState(order.customer?.email || '');
  const [emailSubject, setEmailSubject] = useState('');
  const [emailSending, setEmailSending] = useState(false);
  const [emailResult, setEmailResult] = useState<{ ok: boolean; msg: string } | null>(null);

  const emailApiBase =
    (import.meta as any).env?.VITE_API_URL ||
    'https://marketmate-api-487246736287.us-central1.run.app/api/v1';
  const emailTenantId = getActiveTenantId() || localStorage.getItem('tenantId') || '';
  const emailHeaders = () => {
    const token = localStorage.getItem('auth_token') || '';
    return {
      'Content-Type': 'application/json',
      'X-Tenant-Id': emailTenantId,
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    };
  };

  const loadEmailTemplates = async () => {
    try {
      const res = await fetch(`${emailApiBase}/templates?type=email`, { headers: emailHeaders() });
      if (!res.ok) return;
      const data = await res.json();
      const tpls = (data.templates || []).filter((t: any) => t.enabled !== false);
      setEmailTemplates(tpls);
      if (tpls.length > 0 && !selectedTemplateId) {
        setSelectedTemplateId(tpls[0].id);
        setEmailSubject(tpls[0].name);
      }
    } catch { /* ignore */ }
  };

  const handleOpenEmailPanel = () => {
    setShowEmailPanel(v => {
      if (!v) loadEmailTemplates();
      return !v;
    });
    setEmailResult(null);
  };

  const handleSendEmail = async () => {
    if (!selectedTemplateId || !emailTo.trim()) return;
    setEmailSending(true);
    setEmailResult(null);
    try {
      const res = await fetch(`${emailApiBase}/templates/${selectedTemplateId}/send`, {
        method: 'POST',
        headers: emailHeaders(),
        body: JSON.stringify({
          order_id: dId,
          to: emailTo.trim(),
          subject: emailSubject.trim(),
          html_body: `<p>Email from template ${selectedTemplateId}</p>`,
        }),
      });
      if (res.ok) {
        setEmailResult({ ok: true, msg: `Email sent to ${emailTo}` });
        setShowEmailPanel(false);
      } else {
        const err = await res.json().catch(() => ({ error: 'Unknown error' }));
        setEmailResult({ ok: false, msg: err.error || 'Failed to send email' });
      }
    } catch (e: any) {
      setEmailResult({ ok: false, msg: e.message || 'Network error' });
    } finally {
      setEmailSending(false);
    }
  };

  return (
    <div className="modal-detail" onClick={onClose}>
      <div className="md-card" onClick={e => e.stopPropagation()}>
        {/* Header */}
        <div className="md-header">
          <div className="md-title-block">
            <div className="md-order-num">Order {getDisplayRef(order)}</div>
            <div className="md-created">Created {df.main} {df.year} {df.time}</div>
          </div>
          <div className="md-status-track">
            <StatusBadge status={order.status} held={loc.on_hold} />
          </div>
          <button className="btn-x" onClick={onClose}>×</button>
        </div>

        {/* S2-Task7: Tab navigation */}
        <div style={{ display: 'flex', borderBottom: '1px solid var(--border)', padding: '0 20px' }}>
          {(['details', 'lines', 'activity', 'notes'] as const).map(tab => (
            <button
              key={tab}
              onClick={() => setActiveTab(tab)}
              style={{
                padding: '10px 16px', background: 'none', border: 'none', cursor: 'pointer', fontSize: 13, fontWeight: activeTab === tab ? 700 : 400,
                borderBottom: activeTab === tab ? '2px solid var(--primary)' : '2px solid transparent',
                color: activeTab === tab ? 'var(--primary)' : 'var(--text-muted)',
                textTransform: 'capitalize',
              }}
            >{tab}</button>
          ))}
        </div>

        {/* Body — two column layout */}
        <div className="md-body">

          {/* ── Details Tab ── */}
          {activeTab === 'details' && <>
          {/* LEFT column */}
          <div className="md-col-left">
            {/* Shipping address */}
            <div className="md-section">
              <div className="md-section-title">Customer shipping address</div>
              <div className="md-address">
                {custName && <div>{custName}</div>}
                {addr?.address_line1 && <div>{addr.address_line1}</div>}
                {addr?.city && <div>{addr.city}</div>}
                {addr?.postal_code && <div>{addr.postal_code}</div>}
                {addr?.country && <div>{addr.country}</div>}
                {!custName && !addr?.city && !addr?.postal_code && (
                  <div className="md-redacted">Address redacted by marketplace</div>
                )}
              </div>
            </div>

            {/* Line items table */}
            <div className="md-section">
              <table className="md-items-table">
                <thead>
                  <tr>
                    <th>SKU</th>
                    <th>Product name</th>
                    <th>Qty</th>
                    <th>Unit price</th>
                    <th>VAT</th>
                    <th>Total</th>
                  </tr>
                </thead>
                <tbody>
                  {lines.length > 0 ? lines.map((item, i) => (
                    <tr key={i}>
                      <td className="md-sku">{item.sku || '—'}</td>
                      <td>{item.title || '—'}</td>
                      <td className="md-center">{item.quantity ?? 1}</td>
                      <td className="md-right">{item.unit_price ? fmtMoney(item.unit_price.amount, item.unit_price.currency) : '—'}</td>
                      <td className="md-right" style={{ color: 'var(--text-muted)', fontSize: 12 }}>
                        {item.tax_rate != null && item.tax_rate > 0 ? `${(item.tax_rate * 100).toFixed(0)}%` : '—'}
                      </td>
                      <td className="md-right">{item.line_total ? fmtMoney(item.line_total.amount, item.line_total.currency) : '—'}</td>
                    </tr>
                  )) : (
                    <tr><td colSpan={6} className="md-empty">No items found</td></tr>
                  )}
                </tbody>
              </table>
            </div>

            {/* Fulfilment meta */}
            <div className="md-section md-meta-grid">
              <div className="md-meta-row">
                <span className="md-meta-label">Channel</span>
                <span className="md-meta-value"><span className="ch-badge" style={
                  order.channel === 'backmarket' ? { background: '#14B8A6', color: '#fff' } :
                  order.channel === 'zalando'    ? { background: '#FF6600', color: '#fff' } :
                  order.channel === 'bol'        ? { background: '#0E4299', color: '#fff' } :
                  order.channel === 'lazada'     ? { background: '#F57224', color: '#fff' } :
                  undefined
                }>{(order.channel || '').toUpperCase()}</span></span>
              </div>
              <div className="md-meta-row">
                <span className="md-meta-label">Channel reference</span>
                <span className="md-meta-value md-mono">{order.external_order_id || '—'}</span>
              </div>
              <div className="md-meta-row">
                <span className="md-meta-label">Package size</span>
                <span className="md-meta-value">{loc.package_format}</span>
              </div>
              <div className="md-meta-row">
                <span className="md-meta-label">Shipping service</span>
                <span className="md-meta-value">{loc.shipping_service}</span>
              </div>
              <div className="md-meta-row">
                <span className="md-meta-label">Payment status</span>
                <span className="md-meta-value">{order.payment_status || '—'}</span>
              </div>
              {order.sla_at_risk && (
                <div className="md-meta-row">
                  <span className="md-meta-label">SLA</span>
                  <span className="md-meta-value md-warn">At risk</span>
                </div>
              )}
            </div>
          </div>

          {/* RIGHT column */}
          <div className="md-col-right">
            {/* Totals */}
            <div className="md-section md-totals">
              <div className="md-section-title">Order totals</div>
              {[
                { label: 'Subtotal', val: order.totals?.subtotal },
                { label: 'Shipping', val: order.totals?.shipping },
                { label: 'Tax',      val: order.totals?.tax },
                { label: 'Discount', val: order.totals?.discount },
              ].map(({ label, val }) => val?.amount != null && val.amount !== 0 ? (
                <div key={label} className="md-total-row">
                  <span>{label}</span>
                  <span>{fmtMoney(val.amount, val.currency || currency)}</span>
                </div>
              ) : null)}
              <div className="md-total-row md-grand-total">
                <span>Total</span>
                <span>{total !== null ? fmtMoney(total, currency) : '—'}</span>
              </div>
            </div>

            {/* Actions */}
            <div className="md-section">
              <div className="md-section-title">Actions</div>
              {loc.label_generated ? (
                <div className="md-action-info">
                  <div className="md-tracking">
                    <div className="md-meta-label">Tracking number</div>
                    <div className="md-mono">{loc.tracking_number || '—'}</div>
                  </div>
                </div>
              ) : loc.on_hold ? (
                <div className="md-action-info md-redacted">On hold{loc.hold_reason ? `: ${loc.hold_reason}` : ''}</div>
              ) : (
                <button className="btn-pri md-print-btn" onClick={() => { onClose(); onPrintLabel(order); }}>
                  <Printer size={14} /> Generate Label
                </button>
              )}
              {/* Packing slip — always available regardless of label/hold state */}
              <button
                className="btn-sec md-print-btn"
                style={{ marginTop: 8, width: '100%' }}
                onClick={() => { onClose(); onPrintPackingSlip(order); }}
              >
                📦 Print Packing Slip
              </button>

              {/* Email Customer */}
              <button
                className="btn-sec md-print-btn"
                style={{ marginTop: 8, width: '100%', display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 6 }}
                onClick={handleOpenEmailPanel}
              >
                <Mail size={14} /> Email Customer
              </button>

              {emailResult && (
                <div style={{
                  marginTop: 8, padding: '8px 10px', borderRadius: 7, fontSize: 12,
                  background: emailResult.ok ? 'rgba(34,197,94,0.1)' : 'rgba(239,68,68,0.1)',
                  color: emailResult.ok ? '#15803d' : '#dc2626',
                  border: `1px solid ${emailResult.ok ? 'rgba(34,197,94,0.25)' : 'rgba(239,68,68,0.25)'}`,
                }}>
                  {emailResult.msg}
                </div>
              )}

              {showEmailPanel && (
                <div style={{ marginTop: 10, padding: 12, background: 'var(--bg-tertiary)', borderRadius: 8, border: '1px solid var(--border)' }}>
                  <div style={{ fontWeight: 700, fontSize: 13, marginBottom: 10, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                    <span>Send Email</span>
                    <button onClick={() => setShowEmailPanel(false)} style={{ background: 'none', border: 'none', cursor: 'pointer', padding: 2 }}><X size={14} /></button>
                  </div>

                  <label style={{ fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', display: 'block', marginBottom: 3 }}>Template</label>
                  {emailTemplates.length === 0 ? (
                    <div style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 8 }}>No email templates found. Create one in Settings → Page Builder.</div>
                  ) : (
                    <select
                      value={selectedTemplateId}
                      onChange={e => {
                        setSelectedTemplateId(e.target.value);
                        const tpl = emailTemplates.find(t => t.id === e.target.value);
                        if (tpl) setEmailSubject(tpl.name);
                      }}
                      style={{ width: '100%', padding: '6px 8px', borderRadius: 6, border: '1px solid var(--border)', background: 'var(--bg-primary)', fontSize: 12, marginBottom: 8 }}
                    >
                      {emailTemplates.map(t => <option key={t.id} value={t.id}>{t.name}</option>)}
                    </select>
                  )}

                  <label style={{ fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', display: 'block', marginBottom: 3 }}>To</label>
                  <input
                    value={emailTo}
                    onChange={e => setEmailTo(e.target.value)}
                    placeholder="customer@example.com"
                    style={{ width: '100%', padding: '6px 8px', borderRadius: 6, border: '1px solid var(--border)', background: 'var(--bg-primary)', fontSize: 12, marginBottom: 8 }}
                  />

                  <label style={{ fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', display: 'block', marginBottom: 3 }}>Subject</label>
                  <input
                    value={emailSubject}
                    onChange={e => setEmailSubject(e.target.value)}
                    placeholder="Email subject"
                    style={{ width: '100%', padding: '6px 8px', borderRadius: 6, border: '1px solid var(--border)', background: 'var(--bg-primary)', fontSize: 12, marginBottom: 10 }}
                  />

                  <button
                    onClick={handleSendEmail}
                    disabled={emailSending || !selectedTemplateId || !emailTo.trim()}
                    style={{
                      width: '100%', padding: '8px', display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 6,
                      background: 'var(--primary)', color: '#fff', border: 'none', borderRadius: 7, cursor: 'pointer', fontSize: 13, fontWeight: 600,
                      opacity: (emailSending || !selectedTemplateId || !emailTo.trim()) ? 0.6 : 1,
                    }}
                  >
                    <Send size={13} /> {emailSending ? 'Sending…' : 'Send'}
                  </button>
                </div>
              )}
            </div>
          </div>
          </>}

          {/* ── Lines Tab ── */}
          {activeTab === 'lines' && (
            <div style={{ flex: 1, padding: 20, overflowY: 'auto' }}>
              <table className="md-items-table" style={{ width: '100%' }}>
                <thead>
                  <tr>
                    <th>SKU</th>
                    <th>Product name</th>
                    <th>Qty</th>
                    <th>Unit price</th>
                    <th>VAT %</th>
                    <th>Total</th>
                  </tr>
                </thead>
                <tbody>
                  {lines.length > 0 ? lines.map((item, i) => (
                    <tr key={i}>
                      <td className="md-sku">{item.sku || '—'}</td>
                      <td>{item.title || '—'}</td>
                      <td className="md-center">{item.quantity ?? 1}</td>
                      <td className="md-right">{item.unit_price ? fmtMoney(item.unit_price.amount, item.unit_price.currency) : '—'}</td>
                      <td className="md-right" style={{ color: 'var(--text-muted)', fontSize: 12 }}>
                        {item.tax_rate != null && item.tax_rate > 0 ? `${(item.tax_rate * 100).toFixed(0)}%` : '—'}
                      </td>
                      <td className="md-right">{item.line_total ? fmtMoney(item.line_total.amount, item.line_total.currency) : '—'}</td>
                    </tr>
                  )) : (
                    <tr><td colSpan={6} className="md-empty">No line items found</td></tr>
                  )}
                </tbody>
              </table>
              <div className="md-section md-totals" style={{ marginTop: 20 }}>
                {[
                  { label: 'Subtotal', val: order.totals?.subtotal },
                  { label: 'Shipping', val: order.totals?.shipping },
                  { label: 'Tax',      val: order.totals?.tax },
                  { label: 'Discount', val: order.totals?.discount },
                ].map(({ label, val }) => val?.amount != null && val.amount !== 0 ? (
                  <div key={label} className="md-total-row">
                    <span>{label}</span>
                    <span>{fmtMoney(val.amount, val.currency || currency)}</span>
                  </div>
                ) : null)}
                <div className="md-total-row md-grand-total">
                  <span>Total</span>
                  <span>{total !== null ? fmtMoney(total, currency) : '—'}</span>
                </div>
              </div>
            </div>
          )}

          {/* ── Activity Tab ── */}
          {activeTab === 'activity' && (
            <div style={{ flex: 1, padding: 20, overflowY: 'auto' }}>
              {order.audit_trail && order.audit_trail.length > 0 ? (
                <div>
                  {[...(order.audit_trail || [])].reverse().map((entry: any, i: number) => (
                    <div key={i} style={{ display: 'flex', gap: 14, marginBottom: 16 }}>
                      <div style={{ width: 8, height: 8, borderRadius: '50%', background: 'var(--primary)', marginTop: 5, flexShrink: 0 }} />
                      <div>
                        <div style={{ fontSize: 13, fontWeight: 600 }}>{entry.action || entry.event || '—'}</div>
                        {entry.notes && <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>{entry.notes}</div>}
                        <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 3 }}>
                          {entry.performed_by || 'System'} · {entry.timestamp ? new Date(entry.timestamp).toLocaleString() : ''}
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              ) : (
                <div style={{ textAlign: 'center', padding: '30px 0', color: 'var(--text-muted)', fontSize: 13 }}>
                  No activity recorded for this order
                </div>
              )}
            </div>
          )}

          {/* ── Notes Tab ── */}
          {activeTab === 'notes' && (
            <div style={{ flex: 1, padding: 20 }}>
              {order.internal_notes || order.notes ? (
                <div>
                  {order.internal_notes && (
                    <div className="md-section">
                      <div className="md-section-title">Internal Notes</div>
                      <p style={{ fontSize: 13, color: 'var(--text-secondary)', lineHeight: 1.6, whiteSpace: 'pre-wrap' }}>{order.internal_notes}</p>
                    </div>
                  )}
                  {order.notes && (
                    <div className="md-section">
                      <div className="md-section-title">Order Notes</div>
                      <p style={{ fontSize: 13, color: 'var(--text-secondary)', lineHeight: 1.6, whiteSpace: 'pre-wrap' }}>{order.notes}</p>
                    </div>
                  )}
                </div>
              ) : (
                <div style={{ textAlign: 'center', padding: '30px 0', color: 'var(--text-muted)', fontSize: 13 }}>
                  No notes on this order
                </div>
              )}
            </div>
          )}

        </div>
      </div>
    </div>
  );
};

// ─── StatusBadge ─────────────────────────────────────────────────────────────
const STATUS_CONFIG: Record<string, { label: string; cls: string }> = {
  fulfilled:  { label: 'Fulfilled',  cls: 'sb-fulfilled' },
  imported:   { label: 'Imported',   cls: 'sb-imported'  },
  processing: { label: 'Processing', cls: 'sb-processing'},
  ready:      { label: 'Ready',      cls: 'sb-ready'     },
  cancelled:  { label: 'Cancelled',  cls: 'sb-cancelled' },
  pending:    { label: 'Pending',    cls: 'sb-pending'   },
  on_hold:    { label: 'On Hold',    cls: 'sb-hold'      },
};

const StatusBadge = ({ status, held }: { status?: string; held?: boolean }) => {
  if (held) return <span className="status-badge sb-hold">On Hold</span>;
  const key = (status || '').toLowerCase();
  const cfg = STATUS_CONFIG[key] || { label: status || '—', cls: 'sb-default' };
  return <span className={`status-badge ${cfg.cls}`}>{cfg.label}</span>;
};

// ─── PaginationBar ────────────────────────────────────────────────────────────
const PaginationBar = ({
  page, totalPages, totalOrders, ordersOnPage, pageSize, selectedCount,
  onPageChange, onPageSizeChange,
}: {
  page: number; totalPages: number; totalOrders: number;
  ordersOnPage: number; pageSize: number; selectedCount: number;
  onPageChange: (p: number) => void;
  onPageSizeChange: (n: number) => void;
}) => (
  <div className="pagination-bar">
    <div className="pg-info">
      Showing {ordersOnPage} of {totalOrders} orders
      {selectedCount > 0 && <span className="sel-count"> · {selectedCount} selected</span>}
    </div>
    <div className="pg-controls">
      <select value={pageSize} onChange={e => onPageSizeChange(Number(e.target.value))}>
        {[25, 50, 100, 200].map(n => <option key={n} value={n}>{n} per page</option>)}
      </select>
      <button className="btn-pg" onClick={() => onPageChange(Math.max(1, page - 1))} disabled={page === 1}>
        <ChevronLeft size={13} />
      </button>
      <div className="pg-nums">
        {page > 2 && <button className="btn-pn" onClick={() => onPageChange(1)}>1</button>}
        {page > 3 && <span className="pg-dot">…</span>}
        {page > 1 && <button className="btn-pn" onClick={() => onPageChange(page - 1)}>{page - 1}</button>}
        <button className="btn-pn active">{page}</button>
        {page < totalPages && <button className="btn-pn" onClick={() => onPageChange(page + 1)}>{page + 1}</button>}
        {page < totalPages - 2 && <span className="pg-dot">…</span>}
        {page < totalPages - 1 && <button className="btn-pn" onClick={() => onPageChange(totalPages)}>{totalPages}</button>}
      </div>
      <button className="btn-pg" onClick={() => onPageChange(Math.min(totalPages, page + 1))} disabled={page >= totalPages}>
        <ChevronRight size={13} />
      </button>
    </div>
  </div>
);

// ─── BulkShipModal ────────────────────────────────────────────────────────────
// Extracted from IIFE to fix TS1128 warning (was: showBulkShipModal && (() => {...})())
interface BulkShipModalProps {
  channel: string;
  credentialId: string;
  credentials: Array<{ credential_id: string; account_name: string }>;
  rows: Record<string, { tracking_number: string; carrier: string; external_order_id: string; display_ref: string }>;
  results: Array<{ order_id: string; ok: boolean; error?: string }> | null;
  submitting: boolean;
  onClose: () => void;
  onCredentialChange: (id: string) => void;
  onRowChange: (orderId: string, field: 'tracking_number' | 'carrier', value: string) => void;
  onSubmit: () => void;
  onDone: () => void;
}

const BulkShipModal = ({
  channel, credentialId, credentials, rows, results, submitting,
  onClose, onCredentialChange, onRowChange, onSubmit, onDone,
}: BulkShipModalProps) => {
  const chConfig = S4_CHANNELS.find(c => c.id === channel);
  const chLabel = chConfig?.label || channel;
  const chColor = chConfig?.color || 'var(--primary)';
  const ordersInModal = Object.entries(rows);
  const allFilled = ordersInModal.every(([, v]) => v.tracking_number.trim());

  return (
    <div className="modal-bg" onClick={() => { if (!submitting) onClose(); }}>
      <div className="modal" style={{ width: 640, maxWidth: '96vw', maxHeight: '88vh', display: 'flex', flexDirection: 'column' }} onClick={e => e.stopPropagation()}>
        <div className="modal-head" style={{ borderBottom: `3px solid ${chColor}` }}>
          <h2>🚚 Bulk Ship — {chLabel} ({ordersInModal.length} order{ordersInModal.length !== 1 ? 's' : ''})</h2>
          <button className="btn-x" onClick={onClose}>×</button>
        </div>

        <div className="modal-body" style={{ flex: 1, overflowY: 'auto' }}>
          <div className="field-grp" style={{ marginBottom: 16 }}>
            <label>{chLabel} Account</label>
            {credentials.length === 0 ? (
              <div style={{ fontSize: 13, color: '#ef4444', padding: '8px 0' }}>
                No active {chLabel} credentials found. Connect an account in Channel Connections first.
              </div>
            ) : credentials.length === 1 ? (
              <div style={{ fontSize: 13, color: 'var(--text-secondary)', padding: '6px 0' }}>
                Using: <strong>{credentials[0].account_name}</strong>
              </div>
            ) : (
              <select value={credentialId} onChange={e => onCredentialChange(e.target.value)} className="inline-sel wide">
                <option value="">Select account…</option>
                {credentials.map(c => (
                  <option key={c.credential_id} value={c.credential_id}>{c.account_name}</option>
                ))}
              </select>
            )}
          </div>

          {results ? (
            <div>
              <div style={{ fontWeight: 700, fontSize: 14, marginBottom: 12, color: 'var(--text-primary)' }}>
                Results: {results.filter(r => r.ok).length} succeeded · {results.filter(r => !r.ok).length} failed
              </div>
              {results.map(r => (
                <div key={r.order_id} style={{
                  display: 'flex', alignItems: 'center', gap: 10, padding: '7px 10px', marginBottom: 4,
                  background: r.ok ? 'rgba(34,197,94,0.08)' : 'rgba(239,68,68,0.08)',
                  border: `1px solid ${r.ok ? 'rgba(34,197,94,0.3)' : 'rgba(239,68,68,0.3)'}`,
                  borderRadius: 7, fontSize: 13,
                }}>
                  <span style={{ fontSize: 16 }}>{r.ok ? '✅' : '❌'}</span>
                  <span style={{ flex: 1, fontFamily: 'monospace', fontSize: 12 }}>{r.order_id}</span>
                  {r.error && <span style={{ color: '#ef4444', fontSize: 12 }}>{r.error}</span>}
                </div>
              ))}
              <div className="modal-actions" style={{ marginTop: 16 }}>
                <button className="btn-pri" onClick={onDone}>Done</button>
              </div>
            </div>
          ) : (
            <>
              <div style={{ marginBottom: 8 }}>
                <div style={{ display: 'grid', gridTemplateColumns: '1.5fr 2fr 1.5fr', gap: 8, marginBottom: 6 }}>
                  <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase' }}>Order Ref</div>
                  <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase' }}>Tracking Number *</div>
                  <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase' }}>Carrier</div>
                </div>
                {ordersInModal.map(([orderId, row]) => (
                  <div key={orderId} style={{ display: 'grid', gridTemplateColumns: '1.5fr 2fr 1.5fr', gap: 8, marginBottom: 6, alignItems: 'center' }}>
                    <div style={{ fontSize: 12, fontFamily: 'monospace', color: 'var(--text-secondary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }} title={row.display_ref}>
                      {row.display_ref}
                    </div>
                    <input
                      value={row.tracking_number}
                      onChange={e => onRowChange(orderId, 'tracking_number', e.target.value)}
                      placeholder="e.g. JD123456789GB"
                      style={{ fontSize: 13, padding: '5px 8px' }}
                    />
                    <input
                      value={row.carrier}
                      onChange={e => onRowChange(orderId, 'carrier', e.target.value)}
                      placeholder={channel === 'backmarket' ? 'e.g. DPD' : 'e.g. DHL'}
                      style={{ fontSize: 13, padding: '5px 8px' }}
                    />
                  </div>
                ))}
              </div>
              {!allFilled && (
                <div style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 12 }}>
                  Orders with no tracking number will be skipped.
                </div>
              )}
              <div className="modal-actions">
                <button className="btn-sec" onClick={onClose}>Cancel</button>
                <button
                  className="btn-pri"
                  onClick={onSubmit}
                  disabled={submitting || !credentialId || credentials.length === 0}
                  style={{ background: chColor, borderColor: chColor }}
                >
                  {submitting ? 'Shipping…' : `Ship ${ordersInModal.filter(([, v]) => v.tracking_number.trim()).length} Order${ordersInModal.filter(([, v]) => v.tracking_number.trim()).length !== 1 ? 's' : ''}`}
                </button>
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  );
};

const Orders = () => {
  const [orders, setOrders] = useState<RawOrder[]>([]);
  const [totalOrders, setTotalOrders] = useState(0);
  const [loading, setLoading] = useState(true);
  const [local, setLocalMap] = useState<Record<string, LocalState>>({});

  // Real per-status counts fetched from /orders/stats (BUG-014)
  const [orderStats, setOrderStats] = useState<{
    imported: number; processing: number; ready: number;
    fulfilled: number; on_hold: number; exceptions: number;
    unlinked_items_count: number; composite_items_count: number;
  }>({ imported: 0, processing: 0, ready: 0, fulfilled: 0, on_hold: 0, exceptions: 0, unlinked_items_count: 0, composite_items_count: 0 });

  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(50);
  const totalPages = Math.max(1, Math.ceil(totalOrders / pageSize));

  const [search, setSearch] = useState('');
  const searchRef = useRef(''); // always current, no stale closure
  const [searchField, setSearchField] = useState('pii_email_token');
  const searchFieldRef = useRef('pii_email_token');
  const [statusFilter, setStatusFilter] = useState('');
  const [channelFilter, setChannelFilter] = useState('');
  const [specialFilter, setSpecialFilter] = useState<'' | 'unlinked' | 'composite'>('');
  const [showFilters, setShowFilters] = useState(false);

  // Task 7: Date range filters
  const [receivedFrom, setReceivedFrom] = useState('');
  const [receivedTo, setReceivedTo] = useState('');
  const [despatchFrom, setDespatchFrom] = useState('');
  const [despatchTo, setDespatchTo] = useState('');
  const [deliveryFrom, setDeliveryFrom] = useState('');
  const [deliveryTo, setDeliveryTo] = useState('');

  // Task 8: Shipping / destination filters
  const [shippingServiceFilter, setShippingServiceFilter] = useState('');
  const [destinationCountryFilter, setDestinationCountryFilter] = useState('');
  const [carrierFilter, setCarrierFilter] = useState('');
  // Fix 1B: folder filter
  const [folderFilter, setFolderFilter] = useState('');
  const [allFolders, setAllFolders] = useState<Array<{folder_id: string; name: string; color: string}>>([]);
  // Fix 2A: due today / overdue quick filters
  const [dueTodayActive, setDueTodayActive] = useState(false);
  const [overdueActive, setOverdueActive] = useState(false);
  // Fix 1D: cached location + FC lists for modal dropdowns
  const [warehouseLocations, setWarehouseLocations] = useState<Array<{id: string; name: string; code?: string}>>([]);
  const [fulfilmentSources, setFulfilmentSources] = useState<Array<{source_id: string; name: string; type: string}>>([]);
  // Keep ref in sync so loadOrders always reads latest search
  useEffect(() => { searchRef.current = search; }, [search]);
  useEffect(() => { searchFieldRef.current = searchField; }, [searchField]);

  const [labelOrder, setLabelOrder] = useState<RawOrder | null>(null);
  const [detailOrder, setDetailOrder] = useState<RawOrder | null>(null);
  const [generatingLabel, setGeneratingLabel] = useState(false);
  const [showHoldModal, setShowHoldModal] = useState(false);
  const [holdAction, setHoldAction] = useState<'hold' | 'release'>('hold');
  const [holdReason, setHoldReason] = useState('');
  // A-003: Picklist
  const [generatingPicklist, setGeneratingPicklist] = useState(false);
  // A-005: CSV Export
  const [exportingCSV, setExportingCSV] = useState(false);
  // A-006: Print Invoice
  const [printingInvoice, setPrintingInvoice] = useState(false);

  // ── B-002: New Order Modal ─────────────────────────────────────────────────
  const [showNewOrderModal, setShowNewOrderModal] = useState(false);
  const [newOrderForm, setNewOrderForm] = useState({
    customer_name: '', customer_email: '', customer_phone: '',
    address_line1: '', address_line2: '', city: '', state: '', postal_code: '', country: 'GB',
    notes: '', shipping_method: '',
    line_items: [{ sku: '', title: '', quantity: 1, price: 0, currency: 'GBP' }],
  });
  const [savingNewOrder, setSavingNewOrder] = useState(false);

  // ── B-003: Edit Order Modal ────────────────────────────────────────────────
  const [editOrder, setEditOrder] = useState<RawOrder | null>(null);
  const [editForm, setEditForm] = useState<{
    customer_name: string; customer_email: string; customer_phone: string;
    address_line1: string; city: string; postal_code: string; country: string; notes: string;
    lines: Array<{ line_id: string; title: string; sku: string; quantity: number; unit_price: number; tax_rate: number; currency: string }>;
    shipping_cost: number; shipping_currency: string;
  } | null>(null);
  const [savingEdit, setSavingEdit] = useState(false);

  // ── B-004: Merge Modal ─────────────────────────────────────────────────────
  const [showMergeModal, setShowMergeModal] = useState(false);
  const [merging, setMerging] = useState(false);
  const [mergePrimaryId, setMergePrimaryId] = useState('');

  // ── B-005: Split Modal ─────────────────────────────────────────────────────
  const [splitOrder, setSplitOrder] = useState<RawOrder | null>(null);
  const [splitLines, setSplitLines] = useState<Array<{ line_id: string; title: string; sku: string; qty_total: number; qty_split: number }>>([]);
  const [splitting, setSplitting] = useState(false);

  // ── B-006: Cancel Modal ────────────────────────────────────────────────────
  const [cancelOrder, setCancelOrder] = useState<RawOrder | null>(null);
  const [cancelReason, setCancelReason] = useState('');
  const [cancelNotes, setCancelNotes] = useState('');
  const [cancelling, setCancelling] = useState(false);

  // ── Bulk Status Update ─────────────────────────────────────────────────────
  const [showBulkStatusModal, setShowBulkStatusModal] = useState(false);
  const [bulkStatus, setBulkStatus] = useState('');
  const [applyingBulkStatus, setApplyingBulkStatus] = useState(false);

  // ── S4 Bulk Ship ──────────────────────────────────────────────────────────
  const [showBulkShipModal, setShowBulkShipModal] = useState(false);
  const [bulkShipChannel, setBulkShipChannel] = useState<string>('');
  const [bulkShipCredentialId, setBulkShipCredentialId] = useState<string>('');
  const [bulkShipCredentials, setBulkShipCredentials] = useState<Array<{ credential_id: string; account_name: string }>>([]);
  // Per-order tracking inputs: key = order internal id, value = { tracking_number, carrier }
  const [bulkShipRows, setBulkShipRows] = useState<Record<string, { tracking_number: string; carrier: string; external_order_id: string; display_ref: string }>>({});
  const [bulkShipResults, setBulkShipResults] = useState<Array<{ order_id: string; ok: boolean; error?: string }> | null>(null);
  const [submittingBulkShip, setSubmittingBulkShip] = useState(false);

  // ── S4 Channel Export ─────────────────────────────────────────────────────
  const [exportingS4Channel, setExportingS4Channel] = useState(false);

  const exportS4Channel = async (channelId: string) => {
    if (exportingS4Channel) return;
    const ch = S4_CHANNELS.find(c => c.id === channelId);
    if (!ch) return;
    setExportingS4Channel(true);
    try {
      const res = await authFetch(`${API_BASE}/${ch.id}/orders/bulk/export`);
      if (!res.ok) {
        const e = await res.json().catch(() => ({}));
        throw new Error(e.error || `HTTP ${res.status}`);
      }
      const data = await res.json();
      if (!data.headers || !data.rows) throw new Error('Export returned no data');
      // Convert headers + rows to CSV client-side
      const lines: string[] = [data.headers, ...data.rows].map((row: string[]) =>
        row.map((cell: string) => `"${(cell ?? '').replace(/"/g, '""')}"`).join(','),
      );
      const blob = new Blob([lines.join('\n')], { type: 'text/csv;charset=utf-8;' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = (data.filename || `${ch.id}_orders.csv`).replace('.xlsx', '.csv');
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
    } catch (err: any) {
      alert(`Export failed: ${err.message || 'Unknown error'}`);
    } finally {
      setExportingS4Channel(false);
    }
  };

  const S4_CHANNEL_IDS = new Set(['backmarket', 'zalando', 'bol', 'lazada']);

  // Determine if all selected orders are from the same S4 channel
  const selectedOrders = orders.filter(o => selected.has(getOrderId(o)));
  const selectedChannels = Array.from(new Set(selectedOrders.map(o => o.channel || '')));
  const allSameS4Channel = selectedChannels.length === 1 && S4_CHANNEL_IDS.has(selectedChannels[0]);
  const s4SelectedChannel = allSameS4Channel ? selectedChannels[0] : '';

  const openBulkShip = async () => {
    if (!allSameS4Channel) return;
    const ch = s4SelectedChannel;
    // Fetch credentials for this channel
    try {
      const res = await authFetch(`${API_BASE}/marketplace/credentials`);
      const data = res.ok ? await res.json() : { data: [] };
      const creds = (data.data || [])
        .filter((c: any) => c.channel === ch && c.active)
        .map((c: any) => ({ credential_id: c.credential_id, account_name: c.account_name || c.channel }));
      setBulkShipCredentials(creds);
      setBulkShipCredentialId(creds.length === 1 ? creds[0].credential_id : '');
    } catch {
      setBulkShipCredentials([]);
      setBulkShipCredentialId('');
    }
    // Pre-populate rows for every selected order
    const rows: Record<string, { tracking_number: string; carrier: string; external_order_id: string; display_ref: string }> = {};
    selectedOrders.forEach(o => {
      const id = getOrderId(o);
      rows[id] = {
        tracking_number: '',
        carrier: '',
        external_order_id: o.external_order_id || '',
        display_ref: getDisplayRef(o),
      };
    });
    setBulkShipRows(rows);
    setBulkShipResults(null);
    setBulkShipChannel(ch);
    setShowBulkShipModal(true);
  };

  const submitBulkShip = async () => {
    if (!bulkShipCredentialId) { alert('Select a credential first.'); return; }
    const itemEntries = Object.entries(bulkShipRows).filter(([, v]) => v.tracking_number.trim());
    if (itemEntries.length === 0) { alert('Enter at least one tracking number.'); return; }

    const endpoint = `${API_BASE}/${bulkShipChannel}/orders/bulk/ship`;
    setSubmittingBulkShip(true);
    try {
      // Build channel-specific payload — each S4 channel has a different API shape
      let items: any[];
      if (bulkShipChannel === 'backmarket') {
        // Back Market: order_id must be a plain string (handler does Atoi), carrier required
        items = itemEntries.map(([, v]) => ({
          order_id: v.external_order_id,
          tracking_number: v.tracking_number.trim(),
          carrier: v.carrier.trim() || 'OTHER',
        }));
      } else if (bulkShipChannel === 'zalando') {
        // Zalando: also needs line_item_ids (array of line_id strings from the order)
        items = itemEntries.map(([orderId, v]) => {
          const order = orders.find(o => getOrderId(o) === orderId);
          const loc = getLoc(orderId);
          const lines = order ? getLines(order, loc) : [];
          return {
            order_id: v.external_order_id,
            tracking_number: v.tracking_number.trim(),
            carrier: v.carrier.trim() || 'DHL',
            line_item_ids: lines.map(l => l.line_id || '').filter(Boolean),
          };
        });
      } else if (bulkShipChannel === 'bol') {
        // Bol ships by order_item_id = line_id of the first line item
        items = itemEntries.map(([orderId, v]) => {
          const order = orders.find(o => getOrderId(o) === orderId);
          const loc = getLoc(orderId);
          const lines = order ? getLines(order, loc) : [];
          const firstLineId = lines[0]?.line_id || v.external_order_id;
          return {
            order_id: firstLineId,        // Bol.com: order_item_id
            tracking_number: v.tracking_number.trim(),
            carrier: v.carrier.trim() || 'DHL',
          };
        });
      } else if (bulkShipChannel === 'lazada') {
        // Lazada: needs order_item_ids as array of int64 (parsed from line_ids)
        items = itemEntries.map(([orderId, v]) => {
          const order = orders.find(o => getOrderId(o) === orderId);
          const loc = getLoc(orderId);
          const lines = order ? getLines(order, loc) : [];
          const orderItemIds = lines
            .map(l => parseInt(l.line_id || '', 10))
            .filter(n => !isNaN(n));
          return {
            order_id: v.external_order_id,
            order_item_ids: orderItemIds.length > 0 ? orderItemIds : [],
            tracking_number: v.tracking_number.trim(),
            carrier: v.carrier.trim() || 'MANUAL',
          };
        });
      } else {
        // Generic fallback for any future channels
        items = itemEntries.map(([, v]) => ({
          order_id: v.external_order_id,
          tracking_number: v.tracking_number.trim(),
          carrier: v.carrier.trim(),
        }));
      }

      const res = await fetch(endpoint, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ credential_id: bulkShipCredentialId, items }),
      });
      const data = await res.json();
      if (!res.ok) throw new Error(data.error || `Server error ${res.status}`);
      setBulkShipResults(data.results || []);
      // Update local state for successfully shipped orders
      (data.results || []).forEach((r: { order_id: string; ok: boolean }) => {
        if (r.ok) {
          const entry = itemEntries.find(([, v]) => v.external_order_id === r.order_id);
          if (entry) {
            const [orderId, v] = entry;
            patchLoc(orderId, { label_generated: true, tracking_number: v.tracking_number });
          }
        }
      });
      setTimeout(() => loadOrders(), 1500);
    } catch (err: any) {
      alert(`Bulk ship failed: ${err.message}`);
    } finally {
      setSubmittingBulkShip(false);
    }
  };

  const ORDER_STATUSES = [
    { value: 'imported',        label: 'Imported' },
    { value: 'processing',      label: 'Processing' },
    { value: 'ready',           label: 'Ready to Fulfil' },
    { value: 'fulfilled',       label: 'Fulfilled' },
    { value: 'on_hold',         label: 'On Hold' },
    { value: 'cancelled',       label: 'Cancelled' },
  ];

  const applyBulkStatus = async () => {
    if (!bulkStatus) { alert('Select a status first.'); return; }
    setApplyingBulkStatus(true);
    try {
      const res = await authFetch(`${API_BASE}/orders/bulk/status`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ order_ids: Array.from(selected), status: bulkStatus }),
      });
      if (!res.ok) {
        const e = await res.json().catch(() => ({}));
        throw new Error(e.error || `Server error ${res.status}`);
      }
      setShowBulkStatusModal(false);
      setBulkStatus('');
      setSelected(new Set());
      await loadOrders();
    } catch (err: any) {
      alert(`Bulk status update failed: ${err.message}`);
    } finally {
      setApplyingBulkStatus(false);
    }
  };

  // ── Task 6: Tag Definitions ─────────────────────────────────────────────────
  // Tag definitions loaded once on mount (see useEffect below near loadOrders)

  // Task 6: Tag shape SVG helper
  const TagShape = ({ shape, color, size = 10 }: { shape: string; color: string; size?: number }) => {
    const s = size;
    const style = { display: 'inline-block', verticalAlign: 'middle', flexShrink: 0 } as const;
    switch (shape) {
      case 'circle': return <svg width={s} height={s} style={style}><circle cx={s/2} cy={s/2} r={s/2} fill={color} /></svg>;
      case 'triangle': return <svg width={s} height={s} style={style}><polygon points={`${s/2},0 ${s},${s} 0,${s}`} fill={color} /></svg>;
      case 'star': return <svg width={s} height={s} viewBox="0 0 24 24" style={style}><polygon fill={color} points="12,2 15.09,8.26 22,9.27 17,14.14 18.18,21.02 12,17.77 5.82,21.02 7,14.14 2,9.27 8.91,8.26" /></svg>;
      case 'diamond': return <svg width={s} height={s} style={style}><polygon points={`${s/2},0 ${s},${s/2} ${s/2},${s} 0,${s/2}`} fill={color} /></svg>;
      case 'flag': return <svg width={s} height={s} viewBox="0 0 24 24" style={style}><path fill={color} d="M4 3 v18M4 3 h14 l-3 6 3 6 H4" /></svg>;
      default: return <svg width={s} height={s} style={style}><rect width={s} height={s} fill={color} /></svg>;
    }
  };

  // Task 6: Apply tag to selected/specific orders
  const applyTagToOrders = async (tagId: string, orderIds: string[]) => {
    try {
      await authFetch(`${API_BASE}/orders/tags`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ order_ids: orderIds, tag_id: tagId }),
      });
      setShowTagModal(false);
      await loadOrders();
    } catch (err: any) { alert(`Tag failed: ${err.message}`); }
  };

  // Task 6: Remove tag from orders
  const removeTagFromOrders = async (tagId: string, orderIds: string[]) => {
    try {
      await authFetch(`${API_BASE}/orders/tags`, {
        method: 'DELETE',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ order_ids: orderIds, tag_id: tagId }),
      });
      await loadOrders();
    } catch { /* ignore */ }
  };

  // Task 10: Mark invoice as printed
  const markInvoicePrinted = async (orderId: string) => {
    try {
      await authFetch(`${API_BASE}/orders/${orderId}/mark-invoice-printed`, {
        method: 'POST',
      });
    } catch { /* ignore — non-critical */ }
  };

  // ══════════════════════════════════════════════════════════════════════════
  // ACTIONS MENU HANDLERS
  // ══════════════════════════════════════════════════════════════════════════

  // ── Organise: Folders ──────────────────────────────────────────────────────
  const openFolderModal = async (orderIds: string[]) => {
    setFolderModalOrderIds(orderIds);
    try {
      const res = await authFetch(`${API_BASE}/orders/organise/folders`);
      if (res.ok) {
        const data = await res.json();
        setFolders(data.folders || []);
      }
    } catch { /* silent */ }
    setNewFolderName('');
    setShowFolderModal(true);
  };

  const assignFolder = async (folderId: string, folderName: string) => {
    try {
      const res = await authFetch(`${API_BASE}/orders/organise/folders`, {
        method: 'POST',
        body: JSON.stringify({ order_ids: folderModalOrderIds, folder_id: folderId, folder_name: folderName }),
      });
      if (!res.ok) throw new Error('Failed');
      setShowFolderModal(false);
      await loadOrders();
    } catch (err: any) { alert(`Assign folder failed: ${err.message}`); }
  };

  const createAndAssignFolder = async () => {
    if (!newFolderName.trim()) { alert('Enter a folder name.'); return; }
    setCreatingFolder(true);
    try {
      const res = await authFetch(`${API_BASE}/orders/organise/folders/create`, {
        method: 'POST',
        body: JSON.stringify({ name: newFolderName.trim() }),
      });
      if (!res.ok) throw new Error('Failed to create folder');
      const data = await res.json();
      await assignFolder(data.folder_id, data.name);
    } catch (err: any) { alert(`Create folder failed: ${err.message}`); }
    finally { setCreatingFolder(false); }
  };

  // ── Organise: Identifiers ──────────────────────────────────────────────────
  const openIdentifierModal = (orderIds: string[]) => {
    setIdentifierModalOrderIds(orderIds);
    setIdentifierValue('');
    setShowIdentifierModal(true);
  };

  const saveIdentifier = async () => {
    if (!identifierValue.trim()) { alert('Enter an identifier value.'); return; }
    try {
      const res = await authFetch(`${API_BASE}/orders/organise/identifiers`, {
        method: 'POST',
        body: JSON.stringify({ order_ids: identifierModalOrderIds, identifier: identifierValue.trim() }),
      });
      if (!res.ok) throw new Error('Failed');
      setShowIdentifierModal(false);
      await loadOrders();
    } catch (err: any) { alert(`Identifier failed: ${err.message}`); }
  };

  // ── Organise: Move to Location / Fulfilment Center ─────────────────────────
  const openLocationModal = (orderIds: string[], mode: 'location' | 'fulfilment') => {
    setLocationModalOrderIds(orderIds);
    setLocationModalMode(mode);
    setLocationValue('');
    setLocationName('');
    setShowLocationModal(true);
  };

  const saveLocation = async () => {
    if (!locationValue.trim()) { alert('Enter a location/centre ID.'); return; }
    const endpoint = locationModalMode === 'location'
      ? `${API_BASE}/orders/organise/location`
      : `${API_BASE}/orders/organise/fulfilment-center`;
    const body = locationModalMode === 'location'
      ? { order_ids: locationModalOrderIds, location_id: locationValue.trim(), location_name: locationName.trim() }
      : { order_ids: locationModalOrderIds, fulfilment_center_id: locationValue.trim(), fulfilment_center_name: locationName.trim() };
    try {
      const res = await authFetch(endpoint, { method: 'POST', body: JSON.stringify(body) });
      if (!res.ok) throw new Error('Failed');
      setShowLocationModal(false);
      await loadOrders();
    } catch (err: any) { alert(`Move failed: ${err.message}`); }
  };

  // ── Items: Batch Assignment ────────────────────────────────────────────────
  const openBatchAssignModal = (orderIds: string[]) => {
    setBatchModalOrderIds(orderIds);
    setBatchIdInput('');
    setBatchNumberInput('');
    setShowBatchModal(true);
  };

  const saveBatchAssignment = async () => {
    if (!batchIdInput.trim()) { alert('Enter a batch ID.'); return; }
    setProcessingBatchAction(true);
    try {
      const res = await authFetch(`${API_BASE}/orders/items/batch-assign`, {
        method: 'POST',
        body: JSON.stringify({ order_ids: batchModalOrderIds, batch_id: batchIdInput.trim(), batch_number: batchNumberInput.trim() }),
      });
      if (!res.ok) throw new Error('Failed');
      setShowBatchModal(false);
      await loadOrders();
    } catch (err: any) { alert(`Batch assignment failed: ${err.message}`); }
    finally { setProcessingBatchAction(false); }
  };

  const doAutoAssignBatches = async (orderIds: string[]) => {
    try {
      const res = await authFetch(`${API_BASE}/orders/items/auto-assign-batches`, {
        method: 'POST',
        body: JSON.stringify({ order_ids: orderIds }),
      });
      if (!res.ok) throw new Error('Failed');
      const data = await res.json();
      alert(data.message || `✅ Auto-assigned ${data.assigned} orders to batch ${data.batch_number || ''}`);
      await loadOrders();
    } catch (err: any) { alert(`Auto-assign failed: ${err.message}`); }
  };

  const doClearBatches = async (orderIds: string[]) => {
    if (!confirm(`Clear batch assignments from ${orderIds.length} order${orderIds.length !== 1 ? 's' : ''}?`)) return;
    try {
      const res = await authFetch(`${API_BASE}/orders/items/clear-batches`, {
        method: 'POST',
        body: JSON.stringify({ order_ids: orderIds }),
      });
      if (!res.ok) throw new Error('Failed');
      await loadOrders();
    } catch (err: any) { alert(`Clear batches failed: ${err.message}`); }
  };

  const doLinkUnlinkedItems = async (orderIds: string[]) => {
    try {
      const res = await authFetch(`${API_BASE}/orders/items/link-unlinked`, {
        method: 'POST',
        body: JSON.stringify({ order_ids: orderIds }),
      });
      if (!res.ok) throw new Error('Failed');
      const data = await res.json();
      alert(`✅ Linked ${data.lines_linked} item line${data.lines_linked !== 1 ? 's' : ''} to inventory.`);
      await loadOrders();
    } catch (err: any) { alert(`Link items failed: ${err.message}`); }
  };

  const doAddItemsToPO = async (orderIds: string[], mode: 'all' | 'out_of_stock') => {
    try {
      const res = await authFetch(`${API_BASE}/orders/items/add-to-po`, {
        method: 'POST',
        body: JSON.stringify({ order_ids: orderIds, mode }),
      });
      if (!res.ok) throw new Error('Failed');
      const data = await res.json();
      if (data.po_id) {
        alert(`✅ Purchase order created (${data.po_id})\n${data.lines_added} line${data.lines_added !== 1 ? 's' : ''} added.\n\nView in Purchasing → Purchase Orders.`);
      } else {
        alert(data.message || 'No items added to a PO.');
      }
    } catch (err: any) { alert(`Add to PO failed: ${err.message}`); }
  };

  // ── Shipping: Change Service ───────────────────────────────────────────────
  const openChangeServiceModal = (orderIds: string[]) => {
    setChangeServiceOrderIds(orderIds);
    setNewShippingService('');
    setShowChangeServiceModal(true);
  };

  const saveChangeService = async () => {
    if (!newShippingService.trim()) { alert('Select a shipping service.'); return; }
    setChangingService(true);
    try {
      const res = await authFetch(`${API_BASE}/orders/shipping/change-service`, {
        method: 'POST',
        body: JSON.stringify({ order_ids: changeServiceOrderIds, shipping_service: newShippingService.trim() }),
      });
      if (!res.ok) throw new Error('Failed');
      setShowChangeServiceModal(false);
      // Update local state immediately
      changeServiceOrderIds.forEach(id => patchLoc(id, { shipping_service: newShippingService }));
    } catch (err: any) { alert(`Change service failed: ${err.message}`); }
    finally { setChangingService(false); }
  };

  // ── Shipping: Get Quotes ───────────────────────────────────────────────────
  const openGetQuotes = async (orderIds: string[]) => {
    setQuotesOrderIds(orderIds);
    setQuotesResults([]);
    setShowQuotesModal(true);
    setLoadingQuotes(true);
    try {
      const res = await authFetch(`${API_BASE}/orders/shipping/get-quotes`, {
        method: 'POST',
        body: JSON.stringify({ order_ids: orderIds }),
      });
      if (!res.ok) throw new Error('Failed to fetch quotes');
      const data = await res.json();
      setQuotesResults(data.results || []);
    } catch (err: any) { alert(`Get quotes failed: ${err.message}`); setShowQuotesModal(false); }
    finally { setLoadingQuotes(false); }
  };

  // ── Shipping: Cancel Label ─────────────────────────────────────────────────
  const doCancelLabel = async (orderIds: string[]) => {
    if (!confirm(`Cancel shipping label${orderIds.length !== 1 ? 's' : ''} for ${orderIds.length} order${orderIds.length !== 1 ? 's' : ''}? This will void the labels.`)) return;
    try {
      const res = await authFetch(`${API_BASE}/orders/shipping/cancel-label`, {
        method: 'POST',
        body: JSON.stringify({ order_ids: orderIds }),
      });
      if (!res.ok) throw new Error('Failed');
      orderIds.forEach(id => patchLoc(id, { label_generated: false }));
    } catch (err: any) { alert(`Cancel label failed: ${err.message}`); }
  };

  // ── Shipping: Split Packaging ──────────────────────────────────────────────
  const openSplitPackagingModal = (orderId: string) => {
    setSplitPackagingOrderId(orderId);
    setShowSplitPackagingModal(true);
  };

  // ── Shipping: Change Dispatch Date ────────────────────────────────────────
  const openDispatchDateModal = (orderIds: string[]) => {
    setDispatchDateOrderIds(orderIds);
    setDispatchDateValue('');
    setShowDispatchDateModal(true);
  };

  const saveDispatchDate = async () => {
    if (!dispatchDateValue) { alert('Select a dispatch date.'); return; }
    try {
      const res = await authFetch(`${API_BASE}/orders/shipping/change-dispatch-date`, {
        method: 'POST',
        body: JSON.stringify({ order_ids: dispatchDateOrderIds, dispatch_date: dispatchDateValue }),
      });
      if (!res.ok) throw new Error('Failed');
      setShowDispatchDateModal(false);
      await loadOrders();
    } catch (err: any) { alert(`Change dispatch date failed: ${err.message}`); }
  };

  // ── Shipping: Change Delivery Dates ───────────────────────────────────────
  const openDeliveryDatesModal = (orderIds: string[]) => {
    setDeliveryDatesOrderIds(orderIds);
    setDeliveryDateFrom('');
    setDeliveryDateTo('');
    setShowDeliveryDatesModal(true);
  };

  const saveDeliveryDates = async () => {
    if (!deliveryDateFrom && !deliveryDateTo) { alert('Enter at least one delivery date.'); return; }
    try {
      const res = await authFetch(`${API_BASE}/orders/shipping/change-delivery-dates`, {
        method: 'POST',
        body: JSON.stringify({ order_ids: deliveryDatesOrderIds, delivery_from: deliveryDateFrom, delivery_to: deliveryDateTo }),
      });
      if (!res.ok) throw new Error('Failed');
      setShowDeliveryDatesModal(false);
      await loadOrders();
    } catch (err: any) { alert(`Change delivery dates failed: ${err.message}`); }
  };

  // ── Process Order ──────────────────────────────────────────────────────────
  const doProcessOrder = async (orderId: string) => {
    setProcessingOrder(true);
    try {
      const res = await authFetch(`${API_BASE}/orders/${orderId}/process`, { method: 'POST' });
      if (!res.ok) {
        const e = await res.json().catch(() => ({}));
        throw new Error(e.error || 'Failed');
      }
      await loadOrders();
    } catch (err: any) { alert(`Process order failed: ${err.message}`); }
    finally { setProcessingOrder(false); }
  };

  const doBatchProcess = async (orderIds: string[]) => {
    if (!confirm(`Batch process ${orderIds.length} order${orderIds.length !== 1 ? 's' : ''}? This marks them all as Processing immediately.`)) return;
    setBatchProcessing(true);
    try {
      const res = await authFetch(`${API_BASE}/orders/batch-process`, {
        method: 'POST',
        body: JSON.stringify({ order_ids: orderIds }),
      });
      if (!res.ok) throw new Error('Failed');
      await loadOrders();
    } catch (err: any) { alert(`Batch process failed: ${err.message}`); }
    finally { setBatchProcessing(false); }
  };

  // ── Other Actions: Notes ───────────────────────────────────────────────────
  const openNotesModal = async (orderId: string) => {
    setNotesModalOrderId(orderId);
    setNotesModalNotes([]);
    setNewNoteContent('');
    setNewNoteInternal(false);
    setLoadingNotes(true);
    setShowNotesModal(true);
    try {
      const res = await authFetch(`${API_BASE}/orders/${orderId}/notes`);
      if (res.ok) {
        const data = await res.json();
        setNotesModalNotes(data.notes || []);
      }
    } catch { /* silent */ }
    finally { setLoadingNotes(false); }
  };

  const addNote = async () => {
    if (!newNoteContent.trim()) return;
    setSavingNote(true);
    try {
      const res = await authFetch(`${API_BASE}/orders/${notesModalOrderId}/notes`, {
        method: 'POST',
        body: JSON.stringify({ content: newNoteContent.trim(), is_internal: newNoteInternal }),
      });
      if (!res.ok) throw new Error('Failed');
      const data = await res.json();
      setNotesModalNotes(prev => [data.note, ...prev]);
      setNewNoteContent('');
    } catch (err: any) { alert(`Add note failed: ${err.message}`); }
    finally { setSavingNote(false); }
  };

  const deleteNote = async (noteId: string) => {
    try {
      const res = await authFetch(`${API_BASE}/orders/${notesModalOrderId}/notes/${noteId}`, { method: 'DELETE' });
      if (!res.ok) throw new Error('Failed');
      setNotesModalNotes(prev => prev.filter(n => n.note_id !== noteId));
    } catch (err: any) { alert(`Delete note failed: ${err.message}`); }
  };

  // ── Other Actions: Order XML ───────────────────────────────────────────────
  const openXMLModal = async (orderId: string) => {
    setXmlModalOrderId(orderId);
    setXmlModalContent('');
    setLoadingXML(true);
    setShowXMLModal(true);
    try {
      const res = await authFetch(`${API_BASE}/orders/${orderId}/xml`);
      if (!res.ok) throw new Error('Failed to fetch order data');
      const data = await res.json();
      setXmlModalContent(data.raw || JSON.stringify(data, null, 2));
    } catch (err: any) { alert(`Load XML failed: ${err.message}`); setShowXMLModal(false); }
    finally { setLoadingXML(false); }
  };

  // ── Other Actions: Delete Order ────────────────────────────────────────────
  const openDeleteOrderModal = (orderId: string) => {
    setDeleteOrderId(orderId);
    setShowDeleteOrderModal(true);
  };

  const confirmDeleteOrder = async () => {
    setDeletingOrder(true);
    try {
      const res = await authFetch(`${API_BASE}/orders/${deleteOrderId}`, { method: 'DELETE' });
      if (!res.ok) {
        const e = await res.json().catch(() => ({}));
        throw new Error(e.error || 'Failed');
      }
      setShowDeleteOrderModal(false);
      setSelected(prev => { const n = new Set(prev); n.delete(deleteOrderId); return n; });
      await loadOrders();
    } catch (err: any) { alert(`Delete failed: ${err.message}`); }
    finally { setDeletingOrder(false); }
  };

  // ── Other Actions: Run Rules Engine ───────────────────────────────────────
  const doRunRulesEngine = async (orderIds: string[]) => {
    setRunningRules(true);
    try {
      const res = await authFetch(`${API_BASE}/orders/run-rules`, {
        method: 'POST',
        body: JSON.stringify({ order_ids: orderIds }),
      });
      if (!res.ok) throw new Error('Failed');
      const data = await res.json();
      alert(`✅ Rules engine run complete.\n${data.orders_processed} order${data.orders_processed !== 1 ? 's' : ''} processed, ${data.rules_applied} rule${data.rules_applied !== 1 ? 's' : ''} applied.`);
      await loadOrders();
    } catch (err: any) { alert(`Run rules failed: ${err.message}`); }
    finally { setRunningRules(false); }
  };

  // ── Print: Stock Item Label ────────────────────────────────────────────────
  const printStockItemLabel = async (orderIds: string[]) => {
    try {
      const res = await authFetch(`${API_BASE}/orders/stock-item-label`, {
        method: 'POST',
        body: JSON.stringify({ order_ids: orderIds }),
      });
      if (!res.ok) throw new Error('Not implemented yet');
      const data = await res.json();
      if (data.label_url) window.open(data.label_url, '_blank');
      else alert(data.message || 'Stock item label generated.');
    } catch (err: any) { alert(`Stock label: ${err.message}`); }
  };

  // ── B-007: Order Views ─────────────────────────────────────────────────────
  const [orderViews, setOrderViews] = useState<Array<{ view_id: string; name: string; filters: Record<string, string> }>>([]);
  const [activeViewId, setActiveViewId] = useState('');
  const [showSaveViewModal, setShowSaveViewModal] = useState(false);
  const [newViewName, setNewViewName] = useState('');
  const [savingView, setSavingView] = useState(false);

  // ── B-008: Column visibility ───────────────────────────────────────────────
  const ALL_COLUMNS = ['channel', 'value', 'tags', 'date', 'despatch_by', 'delivery_date', 'batch', 'customer', 'products', 'package', 'service', 'label', 'invoice', 'payment', 'status'] as const;
  type ColKey = typeof ALL_COLUMNS[number];
  const [visibleCols, setVisibleCols] = useState<Set<ColKey>>(new Set(ALL_COLUMNS));
  const [showColPicker, setShowColPicker] = useState(false);

  // ── Task 6: Tag Definitions ─────────────────────────────────────────────────
  const [tagDefinitions, setTagDefinitions] = useState<Array<{tag_id: string; name: string; color: string; shape: string}>>([]);
  const [showTagModal, setShowTagModal] = useState(false);
  const [tagModalOrderIds, setTagModalOrderIds] = useState<string[]>([]);

  // ── Actions Menu: Organise ─────────────────────────────────────────────────
  const [showFolderModal, setShowFolderModal] = useState(false);
  const [folderModalOrderIds, setFolderModalOrderIds] = useState<string[]>([]);
  const [folders, setFolders] = useState<Array<{folder_id: string; name: string; color: string}>>([]);
  const [newFolderName, setNewFolderName] = useState('');
  const [creatingFolder, setCreatingFolder] = useState(false);
  const [showIdentifierModal, setShowIdentifierModal] = useState(false);
  const [identifierModalOrderIds, setIdentifierModalOrderIds] = useState<string[]>([]);
  const [identifierValue, setIdentifierValue] = useState('');
  const [showLocationModal, setShowLocationModal] = useState(false);
  const [locationModalOrderIds, setLocationModalOrderIds] = useState<string[]>([]);
  const [locationModalMode, setLocationModalMode] = useState<'location' | 'fulfilment'>('location');
  const [locationValue, setLocationValue] = useState('');
  const [locationName, setLocationName] = useState('');

  // ── Actions Menu: Items ────────────────────────────────────────────────────
  const [showBatchModal, setShowBatchModal] = useState(false);
  const [batchModalOrderIds, setBatchModalOrderIds] = useState<string[]>([]);
  const [batchIdInput, setBatchIdInput] = useState('');
  const [batchNumberInput, setBatchNumberInput] = useState('');
  const [processingBatchAction, setProcessingBatchAction] = useState(false);

  // ── Actions Menu: Shipping ─────────────────────────────────────────────────
  const [showChangeServiceModal, setShowChangeServiceModal] = useState(false);
  const [changeServiceOrderIds, setChangeServiceOrderIds] = useState<string[]>([]);
  const [newShippingService, setNewShippingService] = useState('');
  const [changingService, setChangingService] = useState(false);
  const [showQuotesModal, setShowQuotesModal] = useState(false);
  const [quotesOrderIds, setQuotesOrderIds] = useState<string[]>([]);
  const [quotesResults, setQuotesResults] = useState<Array<{order_id: string; quotes: Array<{service: string; carrier: string; price: number; currency: string; transit_days: number}>; error?: string}>>([]);
  const [loadingQuotes, setLoadingQuotes] = useState(false);
  const [showSplitPackagingModal, setShowSplitPackagingModal] = useState(false);
  const [splitPackagingOrderId, setSplitPackagingOrderId] = useState('');
  const [showDispatchDateModal, setShowDispatchDateModal] = useState(false);
  const [dispatchDateOrderIds, setDispatchDateOrderIds] = useState<string[]>([]);
  const [dispatchDateValue, setDispatchDateValue] = useState('');
  const [showDeliveryDatesModal, setShowDeliveryDatesModal] = useState(false);
  const [deliveryDatesOrderIds, setDeliveryDatesOrderIds] = useState<string[]>([]);
  const [deliveryDateFrom, setDeliveryDateFrom] = useState('');
  const [deliveryDateTo, setDeliveryDateTo] = useState('');

  // ── Actions Menu: Process ──────────────────────────────────────────────────
  const [processingOrder, setProcessingOrder] = useState(false);
  const [batchProcessing, setBatchProcessing] = useState(false);

  // ── Actions Menu: Other Actions ────────────────────────────────────────────
  const [showNotesModal, setShowNotesModal] = useState(false);
  const [notesModalOrderId, setNotesModalOrderId] = useState('');
  const [notesModalNotes, setNotesModalNotes] = useState<Array<{note_id: string; content: string; created_by: string; created_at: string; is_internal: boolean}>>([]);
  const [newNoteContent, setNewNoteContent] = useState('');
  const [newNoteInternal, setNewNoteInternal] = useState(false);
  const [savingNote, setSavingNote] = useState(false);
  const [loadingNotes, setLoadingNotes] = useState(false);
  const [showXMLModal, setShowXMLModal] = useState(false);
  const [xmlModalOrderId, setXmlModalOrderId] = useState('');
  const [xmlModalContent, setXmlModalContent] = useState('');
  const [loadingXML, setLoadingXML] = useState(false);
  const [showDeleteOrderModal, setShowDeleteOrderModal] = useState(false);
  const [deleteOrderId, setDeleteOrderId] = useState('');
  const [deletingOrder, setDeletingOrder] = useState(false);
  const [runningRules, setRunningRules] = useState(false);

  // ── S2-Task9: Import job polling ────────────────────────────────────────────
  const [importJobId, setImportJobId] = useState<string | null>(null);
  const [importJobStatus, setImportJobStatus] = useState<{ status: string; created: number; applied: number; failed: number; total: number; errors?: string[] } | null>(null);
  const importPollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const startImportPolling = (jobId: string) => {
    setImportJobId(jobId);
    setImportJobStatus({ status: 'running', created: 0, applied: 0, failed: 0, total: 0 });
    if (importPollRef.current) clearInterval(importPollRef.current);
    importPollRef.current = setInterval(async () => {
      try {
        const res = await authFetch(`${API_BASE}/import/status/${jobId}`);
        if (!res.ok) return;
        const data = await res.json();
        const job = data.job || data;
        setImportJobStatus({
          status: job.status || 'running',
          created: job.created_count || job.created || 0,
          applied: job.applied_count || job.applied || 0,
          failed: job.failed_count || job.failed || 0,
          total: job.total_rows || job.total || 0,
          errors: job.errors || [],
        });
        if (job.status === 'complete' || job.status === 'done' || job.status === 'failed') {
          if (importPollRef.current) clearInterval(importPollRef.current);
          loadOrders();
        }
      } catch { /* ignore */ }
    }, 2000);
  };

  // ── Task 4: Multi-column sort ───────────────────────────────────────────────
  const [sortFields, setSortFields] = useState<Array<{field: string; direction: string}>>([]);

  // ── Task 5: Free-text search ────────────────────────────────────────────────
  const [searchMode, setSearchMode] = useState<'pii' | 'freetext'>('pii');

  const searchTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const loggedRef = useRef(false);

  // ── Staleness warning ──
  const [staleCredentials, setStaleCredentials] = useState<Array<{credential_id: string; account_name: string; channel: string; last_sync: string}>>([]);
  const [stalenessWarningDismissed, setStalenessWarningDismissed] = useState(false);
  const [downloadingFor, setDownloadingFor] = useState<string | null>(null);

  const API_BASE =
    (import.meta as any).env?.VITE_API_URL ||
    'https://marketmate-api-487246736287.us-central1.run.app/api/v1';

  // Always derive tenantId live — never fall back to a hardcoded tenant.
  // For new accounts getActiveTenantId() returns the correct tenant immediately.
  const tenantId =
    getActiveTenantId() ||
    localStorage.getItem('marketmate_tenant_id') ||
    localStorage.getItem('tenantId') ||
    '';

  // Convenience wrapper: authenticated fetch using current tenant + Firebase token
  const authFetch = useCallback(async (url: string, init?: RequestInit) => {
    const headers = await _getOrdersAuthHeaders(tenantId);
    return fetch(url, { ...init, headers: { ...headers, ...init?.headers } });
  }, [tenantId]);

  // ── Check for stale credentials on mount ──────────────────────────────
  useEffect(() => {
    async function checkStaleness() {
      try {
        const res = await authFetch(`${API_BASE}/marketplace/credentials`);
        if (!res.ok) return;
        const data = await res.json();
        const creds = data.data || [];
        const oneHourAgo = new Date(Date.now() - 60 * 60 * 1000).toISOString();
        const stale = creds.filter((c: any) => {
          if (!c.active || !c.config?.orders?.enabled) return false;
          const lastSync = c.config?.orders?.last_sync;
          if (!lastSync) return true; // never synced
          return lastSync < oneHourAgo;
        }).map((c: any) => ({
          credential_id: c.credential_id,
          account_name: c.account_name,
          channel: c.channel,
          last_sync: c.config?.orders?.last_sync || '',
        }));
        setStaleCredentials(stale);
      } catch { /* ignore */ }
    }
    checkStaleness();
  }, []);

  // Task 6: Load tag definitions on mount
  useEffect(() => {
    async function loadTagDefs() {
      try {
        const res = await authFetch(`${API_BASE}/settings/order-tags`);
        if (res.ok) {
          const data = await res.json();
          setTagDefinitions(data.tags || []);
        }
      } catch { /* ignore */ }
    }
    // Fix 1B: Load folder list for filter dropdown
    async function loadAllFolders() {
      try {
        const res = await authFetch(`${API_BASE}/orders/organise/folders`);
        if (res.ok) {
          const data = await res.json();
          setAllFolders(data.folders || []);
        }
      } catch { /* ignore */ }
    }
    // Fix 1D: Load warehouse locations for location modal dropdown
    async function loadWarehouseLocations() {
      try {
        const res = await authFetch(`${API_BASE}/locations`);
        if (res.ok) {
          const data = await res.json();
          const locs = (data.locations || data.items || []).map((l: any) => ({
            id: l.location_id || l.id,
            name: l.name,
            code: l.code,
          }));
          setWarehouseLocations(locs);
        }
      } catch { /* ignore */ }
    }
    // Fix 1D: Load fulfilment sources for FC modal dropdown
    async function loadFulfilmentSources() {
      try {
        const res = await authFetch(`${API_BASE}/fulfilment-sources`);
        if (res.ok) {
          const data = await res.json();
          setFulfilmentSources(data.sources || data.items || []);
        }
      } catch { /* ignore */ }
    }
    loadTagDefs();
    loadAllFolders();
    loadWarehouseLocations();
    loadFulfilmentSources();
  }, [API_BASE, tenantId]);

  async function downloadNow(credentialId: string, channel: string) {
    setDownloadingFor(credentialId);
    try {
      const res = await authFetch(`${API_BASE}/orders/import/now`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ credential_id: credentialId, channel, lookback_hours: 24 }),
      });
      if (!res.ok) {
        const body = await res.json().catch(() => ({}));
        throw new Error(body.error || `Server error ${res.status}`);
      }
      setStaleCredentials(prev => prev.filter(c => c.credential_id !== credentialId));
    } catch (err: any) {
      alert(`Download failed: ${err.message || 'Unknown error'}`);
    } finally {
      setDownloadingFor(null);
      setTimeout(() => loadOrders(), 2000);
    }
  }

  // ── Fetch lines subcollection for one order ─────────────────────────────
  // Confirmed working: GET /orders/{order_id}/lines → { lines: [...] }
  const fetchLines = useCallback(async (orderId: string) => {
    try {
      const url = `${API_BASE}/orders/${orderId}/lines?tenant_id=${tenantId}`;
      const res = await fetch(url);
      if (!res.ok) {
        console.warn(`[Lines] ${res.status} for ${orderId}`);
        setLocalMap(prev => ({
          ...prev,
          [orderId]: { ...prev[orderId], lines_loaded: true, lines: prev[orderId]?.lines || [] },
        }));
        return;
      }
      const data = await res.json();
      const lines: LineItem[] = data.lines || data.line_items || data.items || [];
      setLocalMap(prev => ({
        ...prev,
        [orderId]: { ...prev[orderId], lines, lines_loaded: true },
      }));
    } catch (err) {
      console.warn(`[Lines] fetch failed for ${orderId}:`, err);
      setLocalMap(prev => ({
        ...prev,
        [orderId]: { ...prev[orderId], lines_loaded: true, lines: prev[orderId]?.lines || [] },
      }));
    }
  }, [API_BASE, tenantId]);

  // ── Load orders ───────────────────────────────────────────────────────────
  const loadOrders = useCallback(async () => {
    setLoading(true);
    try {
      const params = new URLSearchParams({
        tenant_id: tenantId,
        limit: String(pageSize),
        offset: String((page - 1) * pageSize),
      });
      if (specialFilter) {
        params.set('special_filter', specialFilter);
      } else {
        if (statusFilter) params.set('status', statusFilter);
        if (channelFilter) params.set('channel', channelFilter);
        // Task 7: Date range filters
        if (receivedFrom) params.set('received_from', receivedFrom);
        if (receivedTo) params.set('received_to', receivedTo);
        if (despatchFrom) params.set('despatch_from', despatchFrom);
        if (despatchTo) params.set('despatch_to', despatchTo);
        if (deliveryFrom) params.set('delivery_from', deliveryFrom);
        if (deliveryTo) params.set('delivery_to', deliveryTo);
        // Task 8: Shipping filters
        if (shippingServiceFilter) params.set('shipping_service', shippingServiceFilter);
        if (destinationCountryFilter) params.set('destination_country', destinationCountryFilter);
        if (carrierFilter) params.set('carrier', carrierFilter);
        // Fix 1B: folder filter
        if (folderFilter) params.set('folder_id', folderFilter);
        // Fix 2A: due today / overdue quick filters
        if (dueTodayActive) {
          const today = new Date().toISOString().split('T')[0];
          params.set('despatch_to', today);
        } else if (overdueActive) {
          const yesterday = new Date(Date.now() - 86400000).toISOString().split('T')[0];
          params.set('despatch_to', yesterday);
        }
      }
      if (searchRef.current) {
        params.set('search', searchRef.current);
        // Task 5: choose correct search mode
        if (searchRef.current && !['pii_email_token','pii_name_token','pii_postcode_token','pii_phone_token'].includes(searchFieldRef.current)) {
          params.set('search_field', 'free_text');
        } else {
          params.set('search_field', searchFieldRef.current);
        }
      }
      // Task 4: multi-column sort
      if (sortFields.length > 0) {
        params.set('sort_fields', sortFields.map(sf => `${sf.field}:${sf.direction}`).join(','));
      }

      const res = await authFetch(`${API_BASE}/orders?${params}`);
      const data = await res.json();

      if (!loggedRef.current && data.orders?.length > 0) {
        console.log('[Orders] Raw order[0]:', JSON.stringify(data.orders[0], null, 2));
        loggedRef.current = true;
      }

      const rawOrders: RawOrder[] = data.orders || [];
      setOrders(rawOrders);
      const apiTotal = data.total ?? data.count ?? data.pagination?.total ?? rawOrders.length;
      console.log(`[Orders] page=${page} pageSize=${pageSize} offset=${(page-1)*pageSize} got=${rawOrders.length} apiTotal=${apiTotal}`);
      setTotalOrders(apiTotal);

      // Seed local state — do NOT call async functions inside setState
      setLocalMap(prev => {
        const next = { ...prev };
        rawOrders.forEach(o => {
          const id = getOrderId(o);
          if (!next[id]) {
            next[id] = defaultLocal(o);
          } else {
            next[id] = {
              ...next[id],
              lines: next[id].lines.length > 0 ? next[id].lines : (o.lines || []),
            };
          }
        });
        return next;
      });

      // Fetch lines for all orders AFTER state is set
      // Stagger requests to avoid hammering the API
      rawOrders.forEach((o, i) => {
        const id = getOrderId(o);
        const hasLines = (o.lines || o.line_items || o.items || []).length > 0;
        if (!hasLines) {
          setTimeout(() => fetchLines(id), i * 50);
        }
      });

      setSelected(new Set());
    } catch (err) {
      console.error('Orders load error:', err);
    } finally {
      setLoading(false);
    }
  }, [page, pageSize, statusFilter, channelFilter, specialFilter, receivedFrom, receivedTo, despatchFrom, despatchTo, deliveryFrom, deliveryTo, shippingServiceFilter, destinationCountryFilter, carrierFilter, folderFilter, dueTodayActive, overdueActive, sortFields, tenantId]); // searchRef is a ref, fetchLines is stable

  // ── Load order stats (real per-status counts) — BUG-014 fix ──────────────
  const loadOrderStats = useCallback(async () => {
    try {
      const res = await authFetch(`${API_BASE}/orders/stats`);
      if (!res.ok) return;
      const data = await res.json();
      setOrderStats({
        imported:               data.imported              ?? 0,
        processing:             data.processing            ?? 0,
        ready:                  data.ready                 ?? 0,
        fulfilled:              data.fulfilled             ?? 0,
        on_hold:                data.on_hold               ?? 0,
        exceptions:             data.exceptions            ?? 0,
        unlinked_items_count:   data.unlinked_items_count  ?? 0,
        composite_items_count:  data.composite_items_count ?? 0,
      });
    } catch (err) {
      console.warn('[Orders] Stats fetch failed:', err);
    }
  }, [API_BASE, tenantId]);

  // Reset selection when filters or page change
  useEffect(() => {
    setSelected(new Set());
  }, [statusFilter, channelFilter, specialFilter, page]);

  // Single effect — reacts to page/pageSize/filter changes immediately
  useEffect(() => {
    loggedRef.current = false;
    loadOrders();
    loadOrderStats();
  }, [page, pageSize, statusFilter, channelFilter, specialFilter, receivedFrom, receivedTo, despatchFrom, despatchTo, deliveryFrom, deliveryTo, shippingServiceFilter, destinationCountryFilter, carrierFilter, folderFilter, dueTodayActive, overdueActive, sortFields]); // eslint-disable-line

  // Debounced search: debounce input, then either reset page (triggers main effect) or force reload
  useEffect(() => {
    if (searchTimer.current) clearTimeout(searchTimer.current);
    searchTimer.current = setTimeout(() => {
      loggedRef.current = false;
      if (page !== 1) {
        setPage(1); // triggers main effect which calls loadOrders
      } else {
        loadOrders(); // already on page 1, just reload
      }
    }, 400);
    return () => { if (searchTimer.current) clearTimeout(searchTimer.current); };
  }, [search]); // eslint-disable-line

  // ── Local state helpers ───────────────────────────────────────────────────
  const getLoc = (id: string): LocalState =>
    local[id] ?? { package_format: 'Parcel', shipping_service: 'Royal Mail 2nd Class', on_hold: false, hold_reason: '', label_generated: false, lines: [], lines_loaded: false };

  const patchLoc = (id: string, patch: Partial<LocalState>) =>
    setLocalMap(prev => ({ ...prev, [id]: { ...getLoc(id), ...prev[id], ...patch } }));

  // ── Selection ──────────────────────────────────────────────────────────────
  const toggleOne = (e: React.ChangeEvent<HTMLInputElement>, id: string) => {
    e.stopPropagation();
    setSelected(prev => { const n = new Set(prev); n.has(id) ? n.delete(id) : n.add(id); return n; });
  };

  const toggleAll = (e: React.ChangeEvent<HTMLInputElement>) => {
    e.stopPropagation();
    const ids = orders.map(getOrderId);
    setSelected(selected.size === ids.length ? new Set() : new Set(ids));
  };

  // ── Hold ──────────────────────────────────────────────────────────────────
  const openHold = (action: 'hold' | 'release') => {
    if (selected.size === 0) return;
    setHoldAction(action); setHoldReason(''); setShowHoldModal(true);
  };

  const applyHold = async () => {
    const orderIds = Array.from(selected);
    const endpoint = holdAction === 'hold' ? '/orders/hold' : '/orders/hold/release';
    const body = holdAction === 'hold'
      ? { order_ids: orderIds, reason: holdReason }
      : { order_ids: orderIds };

    try {
      const res = await authFetch(`${API_BASE}${endpoint}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      });
      if (!res.ok) {
        const errBody = await res.json().catch(() => ({}));
        throw new Error(errBody.error || `Server error ${res.status}`);
      }
    } catch (err: any) {
      alert(`Hold action failed: ${err.message || 'Unknown error'}`);
      setShowHoldModal(false);
      return;
    }

    // Update local state to reflect the change immediately
    orderIds.forEach(id => patchLoc(id, {
      on_hold: holdAction === 'hold',
      hold_reason: holdAction === 'hold' ? holdReason : '',
    }));
    setShowHoldModal(false);
    setSelected(new Set());
  };

  // ── Label ──────────────────────────────────────────────────────────────────
    // ── A-003: Generate Picklist ─────────────────────────────────────────────
  const generatePicklist = async () => {
    if (selected.size === 0) return;
    setGeneratingPicklist(true);
    try {
      const res = await authFetch(`${API_BASE}/orders/picklist`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ order_ids: Array.from(selected) }),
      });
      if (!res.ok) {
        const errBody = await res.json().catch(() => ({}));
        throw new Error(errBody.error || `Server error ${res.status}`);
      }
      const data = await res.json();
      const items: Array<{ sku: string; product_name: string; location_path: string; binrack: string; qty_needed: number }> = data.picklist || [];
      const pickHtml = `<!DOCTYPE html><html><head><title>Pick List</title><style>
        body{font-family:sans-serif;padding:20px;color:#111}
        h2{margin-bottom:4px}
        .meta{color:#666;font-size:13px;margin-bottom:16px}
        table{width:100%;border-collapse:collapse}
        th,td{border:1px solid #ccc;padding:6px 10px;text-align:left;font-size:14px}
        th{background:#f0f0f0;font-weight:600}
      </style></head><body>
        <h2>Pick List</h2>
        <div class="meta">Orders: ${data.order_count} | Items: ${items.length} | ${new Date().toLocaleString()}</div>
        <table><thead><tr><th>SKU</th><th>Product</th><th>Location</th><th>Bin</th><th>Qty</th></tr></thead>
        <tbody>${items.map(i => `<tr><td>${i.sku}</td><td>${i.product_name}</td><td>${i.location_path || ''}</td><td>${i.binrack || ''}</td><td><strong>${i.qty_needed}</strong></td></tr>`).join('')}</tbody>
        </table><script>window.onload=()=>window.print()</script></body></html>`;
      const blob = new Blob([pickHtml], { type: 'text/html' });
      window.open(URL.createObjectURL(blob), '_blank');
    } catch (err: any) {
      alert(`Picklist failed: ${err.message || 'Unknown error'}`);
    } finally {
      setGeneratingPicklist(false);
    }
  };

  // ── A-005: Export Orders CSV ───────────────────────────────────────────────
  const exportOrdersCSV = async () => {
    setExportingCSV(true);
    try {
      const params = new URLSearchParams();
      if (statusFilter) params.set('status', statusFilter);
      if (channelFilter) params.set('channel', channelFilter);
      if (search) params.set('search', search);
      const res = await authFetch(`${API_BASE}/orders/export?${params.toString()}`);
      if (!res.ok) throw new Error(`Export returned ${res.status}`);
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `orders_${new Date().toISOString().slice(0, 10)}.csv`;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
    } catch (err: any) {
      alert(`Export failed: ${err.message || 'Unknown error'}`);
    } finally {
      setExportingCSV(false);
    }
  };

  // ── A-006: Print Invoice ───────────────────────────────────────────────────
  // Flow:
  //  1. GET /templates/default/invoice → fetch template (blocks, canvas, theme)
  //  2. POST /templates/:id/render { order_id } → fetch resolved merge-tag data
  //  3. Client-side: generateFullHTML(blocks, renderData, themeVars, canvas, name)
  //  4. Open in new window → browser print dialog
  const printInvoice = async (order: RawOrder) => {
    setPrintingInvoice(true);
    try {
      // Step 1: load the default invoice template
      const tmplRes = await authFetch(`${API_BASE}/templates/default/invoice`);
      if (!tmplRes.ok) throw new Error('No default invoice template found. Create one in Settings → Page Builder first.');
      const tmplData = await tmplRes.json();
      const tpl = tmplData.template || tmplData;
      const templateId: string = tpl.id || tpl.template_id || '';
      if (!templateId) throw new Error('No default invoice template configured');

      // Step 2: fetch render data (merge-tag context) from the backend
      const orderId = getOrderId(order);
      const renderRes = await authFetch(`${API_BASE}/templates/${templateId}/render`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ order_id: orderId }),
      });
      if (!renderRes.ok) {
        const e = await renderRes.json().catch(() => ({}));
        throw new Error(e.error || 'Render failed');
      }
      const renderPayload = await renderRes.json();
      const renderData = renderPayload.render_data || {};

      // Step 3: resolve the template's blocks to a full HTML document client-side
      const blocks = tpl.blocks || [];
      const canvas = tpl.canvas || { width: 794, height: 'auto', unit: 'px' };
      const themeName: string = tpl.theme || 'default';
      const themeVars = (THEME_PRESETS[themeName] || THEME_PRESETS['default'])?.vars || {};
      const invoiceHtml: string = generateFullHTML(blocks, renderData, themeVars, canvas, tpl.name || 'Invoice');
      if (!invoiceHtml) throw new Error('Template rendered no content');

      // Step 4: open in a new window and trigger print
      const blob = new Blob([invoiceHtml], { type: 'text/html' });
      const url = URL.createObjectURL(blob);
      const win = window.open(url, '_blank');
      if (win) {
        win.onload = () => {
          win.print();
          // Revoke blob URL after a delay to allow print to complete
          setTimeout(() => URL.revokeObjectURL(url), 60_000);
        };
      }
      // Task 10: Mark invoice as printed
      markInvoicePrinted(getOrderId(order));
    } catch (err: any) {
      alert(`Print invoice failed: ${err.message || 'Unknown error'}`);
    } finally {
      setPrintingInvoice(false);
    }
  };

  // ── Task 3: Print Packing Slip ────────────────────────────────────────────
  // Flow: POST /orders/packing-slip { order_id, shipping_method }
  //       → { html: "..." } → open in new window → browser print dialog
  const [printingPackingSlip, setPrintingPackingSlip] = useState(false);

  const printPackingSlip = async (order: RawOrder) => {
    const id = getOrderId(order);
    const loc = getLoc(id);
    setPrintingPackingSlip(true);
    try {
      const res = await authFetch(`${API_BASE}/orders/packing-slip`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          order_id: id,
          shipping_method: loc.shipping_service || '',
        }),
      });
      if (!res.ok) {
        const e = await res.json().catch(() => ({}));
        throw new Error(e.error || `Server error ${res.status}`);
      }
      const data = await res.json();
      const html: string = data.html || '';
      if (!html) throw new Error('No packing slip HTML returned');

      // Inject auto-print script if not already present, then open
      const printable = html.includes('window.print')
        ? html
        : html.replace('</body>', '<script>window.onload=()=>window.print()</script></body>');

      const blob = new Blob([printable], { type: 'text/html' });
      const url = URL.createObjectURL(blob);
      const win = window.open(url, '_blank');
      if (win) {
        // Revoke blob URL after print dialog has had time to open
        setTimeout(() => URL.revokeObjectURL(url), 60_000);
      }
    } catch (err: any) {
      alert(`Print packing slip failed: ${err.message || 'Unknown error'}`);
    } finally {
      setPrintingPackingSlip(false);
    }
  };

  const generateLabel = async () => {
    if (!labelOrder) return;
    const id = getOrderId(labelOrder);
    setGeneratingLabel(true);
    try {
      const res = await authFetch(`${API_BASE}/dispatch/shipments`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ order_id: id, carrier_id: 'royal-mail', service_code: '1ST' }),
      });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = await res.json();
      if (data.label_url) window.open(data.label_url, '_blank');
      patchLoc(id, { label_generated: true, label_url: data.label_url, tracking_number: data.tracking_number });
      setLabelOrder(null);
    } catch (err) {
      console.error('Label error:', err);
      alert('Failed to generate label. See console for details.');
    } finally {
      setGeneratingLabel(false);
    }
  };

  // ── B-002: Create Manual Order ─────────────────────────────────────────────
  const submitNewOrder = async () => {
    if (!newOrderForm.customer_name || newOrderForm.line_items.some(li => !li.sku || li.quantity < 1)) {
      alert('Please fill in customer name and at least one line item with SKU and quantity.');
      return;
    }
    setSavingNewOrder(true);
    try {
      const res = await authFetch(`${API_BASE}/orders/manual`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          customer_name: newOrderForm.customer_name,
          customer_email: newOrderForm.customer_email,
          customer_phone: newOrderForm.customer_phone,
          shipping_address: {
            name: newOrderForm.customer_name,
            address_line1: newOrderForm.address_line1,
            address_line2: newOrderForm.address_line2,
            city: newOrderForm.city,
            state: newOrderForm.state,
            postal_code: newOrderForm.postal_code,
            country: newOrderForm.country || 'GB',
          },
          line_items: newOrderForm.line_items.filter(li => li.sku),
          shipping_method: newOrderForm.shipping_method,
          notes: newOrderForm.notes,
          channel: 'direct',
        }),
      });
      if (!res.ok) {
        const e = await res.json().catch(() => ({}));
        throw new Error(e.error || `Server error ${res.status}`);
      }
      setShowNewOrderModal(false);
      setNewOrderForm({ customer_name: '', customer_email: '', customer_phone: '', address_line1: '', address_line2: '', city: '', state: '', postal_code: '', country: 'GB', notes: '', shipping_method: '', line_items: [{ sku: '', title: '', quantity: 1, price: 0, currency: 'GBP' }] });
      await loadOrders();
    } catch (err: any) {
      alert(`Failed to create order: ${err.message}`);
    } finally {
      setSavingNewOrder(false);
    }
  };

  // ── B-003: Edit Order ──────────────────────────────────────────────────────
  const openEditOrder = (order: RawOrder) => {
    const orderLines = order.lines || order.line_items || order.items || [];
    setEditOrder(order);
    setEditForm({
      customer_name: order.shipping_address?.name || order.customer?.name || '',
      customer_email: order.customer?.email || '',
      customer_phone: '',
      address_line1: order.shipping_address?.address_line1 || '',
      city: order.shipping_address?.city || '',
      postal_code: order.shipping_address?.postal_code || '',
      country: order.shipping_address?.country || 'GB',
      notes: order.internal_notes || '',
      lines: orderLines.map((l, i) => ({
        line_id: l.line_id || `line-${i}`,
        title: l.title || '',
        sku: l.sku || '',
        quantity: l.quantity || 1,
        unit_price: l.unit_price?.amount ?? l.line_total?.amount ?? 0,
        tax_rate: l.tax_rate ?? 0.20,
        currency: l.unit_price?.currency || l.line_total?.currency || 'GBP',
      })),
      shipping_cost: order.totals?.shipping?.amount ?? 0,
      shipping_currency: order.totals?.shipping?.currency || 'GBP',
    });
  };

  const saveEditOrder = async () => {
    if (!editOrder || !editForm) return;
    setSavingEdit(true);
    try {
      const res = await authFetch(`${API_BASE}/orders/${getOrderId(editOrder)}/edit`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          customer_name: editForm.customer_name,
          customer_email: editForm.customer_email,
          customer_phone: editForm.customer_phone,
          shipping_address: {
            name: editForm.customer_name,
            address_line1: editForm.address_line1,
            city: editForm.city,
            postal_code: editForm.postal_code,
            country: editForm.country,
          },
          notes: editForm.notes,
          line_items: editForm.lines.map(l => ({
            line_id: l.line_id,
            title: l.title,
            sku: l.sku,
            quantity: l.quantity,
            unit_price: { amount: l.unit_price, currency: l.currency },
            tax_rate: l.tax_rate,
          })),
          shipping: { amount: editForm.shipping_cost, currency: editForm.shipping_currency },
        }),
      });
      if (!res.ok) {
        const e = await res.json().catch(() => ({}));
        throw new Error(e.error || `Server error ${res.status}`);
      }
      setEditOrder(null);
      setEditForm(null);
      await loadOrders();
    } catch (err: any) {
      alert(`Failed to update order: ${err.message}`);
    } finally {
      setSavingEdit(false);
    }
  };

  // ── B-004: Merge Orders ────────────────────────────────────────────────────
  const openMerge = () => {
    if (selected.size < 2) { alert('Select at least 2 orders to merge.'); return; }
    const ids = Array.from(selected);
    setMergePrimaryId(ids[0]);
    setShowMergeModal(true);
  };

  const doMerge = async () => {
    const ids = Array.from(selected);
    const secondaryIds = ids.filter(id => id !== mergePrimaryId);
    setMerging(true);
    try {
      const res = await authFetch(`${API_BASE}/orders/merge`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ primary_order_id: mergePrimaryId, order_ids_to_merge: secondaryIds }),
      });
      if (!res.ok) {
        const e = await res.json().catch(() => ({}));
        throw new Error(e.error || `Server error ${res.status}`);
      }
      setShowMergeModal(false);
      setSelected(new Set());
      await loadOrders();
    } catch (err: any) {
      alert(`Merge failed: ${err.message}`);
    } finally {
      setMerging(false);
    }
  };

  // ── B-005: Split Order ─────────────────────────────────────────────────────
  const openSplit = async (order: RawOrder) => {
    const id = getOrderId(order);
    const loc = getLoc(id);
    const lines = getLines(order, loc);
    if (!loc.lines_loaded) {
      await fetchLines(id);
    }
    const freshLoc = getLoc(id);
    const freshLines = getLines(order, freshLoc);
    setSplitLines(freshLines.map(l => ({
      line_id: l.line_id || '',
      title: l.title || l.sku || '—',
      sku: l.sku || '',
      qty_total: l.quantity || 1,
      qty_split: 0,
    })));
    setSplitOrder(order);
    void lines; // suppress lint
  };

  const doSplit = async () => {
    if (!splitOrder) return;
    const splitItems = splitLines.filter(l => l.qty_split > 0 && l.line_id);
    if (splitItems.length === 0) { alert('Set a quantity > 0 for at least one line to split.'); return; }
    setSplitting(true);
    try {
      const res = await authFetch(`${API_BASE}/orders/${getOrderId(splitOrder)}/split`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ line_items_for_new_order: splitItems.map(l => ({ line_id: l.line_id, quantity: l.qty_split })) }),
      });
      if (!res.ok) {
        const e = await res.json().catch(() => ({}));
        throw new Error(e.error || `Server error ${res.status}`);
      }
      setSplitOrder(null);
      setSplitLines([]);
      await loadOrders();
    } catch (err: any) {
      alert(`Split failed: ${err.message}`);
    } finally {
      setSplitting(false);
    }
  };

  // ── B-006: Cancel Order ────────────────────────────────────────────────────
  const doCancel = async () => {
    if (!cancelOrder || !cancelReason) { alert('Please select a reason.'); return; }
    setCancelling(true);
    try {
      const res = await authFetch(`${API_BASE}/orders/${getOrderId(cancelOrder)}/cancel`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ reason: cancelReason, notes: cancelNotes }),
      });
      if (!res.ok) {
        const e = await res.json().catch(() => ({}));
        throw new Error(e.error || `Server error ${res.status}`);
      }
      setCancelOrder(null);
      setCancelReason('');
      setCancelNotes('');
      await loadOrders();
    } catch (err: any) {
      alert(`Cancel failed: ${err.message}`);
    } finally {
      setCancelling(false);
    }
  };

  // ── B-007: Order Views ─────────────────────────────────────────────────────
  const loadOrderViews = useCallback(async () => {
    try {
      const res = await authFetch(`${API_BASE}/order-views`);
      if (!res.ok) return;
      const data = await res.json();
      setOrderViews(data.views || []);
    } catch { /* silent */ }
  }, [API_BASE, tenantId]);

  useEffect(() => { loadOrderViews(); }, [loadOrderViews]);

  const applyView = (view: { view_id: string; name: string; filters: Record<string, string> }) => {
    setActiveViewId(view.view_id);
    if (view.filters?.status) setStatusFilter(view.filters.status);
    if (view.filters?.channel) setChannelFilter(view.filters.channel);
    setPage(1);
  };

  const saveCurrentView = async () => {
    if (!newViewName.trim()) { alert('Enter a name for this view.'); return; }
    setSavingView(true);
    try {
      const res = await authFetch(`${API_BASE}/order-views`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: newViewName.trim(),
          filters: { status: statusFilter, channel: channelFilter },
          columns: Array.from(visibleCols),
          sort_field: 'created_at',
          sort_dir: 'desc',
        }),
      });
      if (!res.ok) throw new Error();
      await loadOrderViews();
      setShowSaveViewModal(false);
      setNewViewName('');
    } catch {
      alert('Failed to save view.');
    } finally {
      setSavingView(false);
    }
  };

  const deleteOrderView = async (viewId: string) => {
    if (!confirm('Delete this view?')) return;
    await authFetch(`${API_BASE}/order-views/${viewId}`, { method: 'DELETE' });
    if (activeViewId === viewId) setActiveViewId('');
    await loadOrderViews();
  };

  // ── B-009: Keyboard shortcuts ──────────────────────────────────────────────
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      // Escape: deselect all / close modals
      if (e.key === 'Escape') {
        setSelected(new Set());
        setDetailOrder(null);
        return;
      }
      if (!e.shiftKey) return;
      // Don't fire shortcuts when typing in inputs
      if (['INPUT', 'TEXTAREA', 'SELECT'].includes((e.target as HTMLElement).tagName)) return;
      switch (e.key.toUpperCase()) {
        case 'A': { e.preventDefault(); toggleAll({ target: { checked: true } } as any); break; }
        case 'D': { e.preventDefault(); setSelected(new Set()); break; }
        case 'K': { e.preventDefault(); if (selected.size > 0) generatePicklist(); break; }
        case 'P': {
          e.preventDefault();
          if (selected.size === 1) {
            const o = orders.find(o => getOrderId(o) === Array.from(selected)[0]);
            if (o) printPackingSlip(o);
          }
          break;
        }
        case 'M': { e.preventDefault(); if (selected.size >= 2) openMerge(); break; }
        case 'H': { e.preventDefault(); if (selected.size > 0) openHold('hold'); break; }
        case 'I': {
          e.preventDefault();
          if (selected.size === 1) {
            const o = orders.find(o => getOrderId(o) === Array.from(selected)[0]);
            if (o) printInvoice(o);
          }
          break;
        }
        // S2-Task10: New shortcuts
        case 'T': {
          e.preventDefault();
          if (selected.size > 0) { setTagModalOrderIds(Array.from(selected)); setShowTagModal(true); }
          break;
        }
        case 'F': {
          e.preventDefault();
          setShowFilters(f => !f);
          break;
        }
      }
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selected, orders]);

  // ── Stats ─────────────────────────────────────────────────────────────────
  // Per-status counts come from /orders/stats (full dataset), total from pagination
  const stats = {
    total:                 totalOrders,
    imported:              orderStats.imported,
    processing:            orderStats.processing,
    ready:                 orderStats.ready,
    fulfilled:             orderStats.fulfilled,
    on_hold:               orderStats.on_hold,
    exceptions:            orderStats.exceptions,
    unlinked_items_count:  orderStats.unlinked_items_count,
    composite_items_count: orderStats.composite_items_count,
  };

  // ── Render ────────────────────────────────────────────────────────────────
  return (
    <div className="orders-page">

      {/* ── Page Header ── */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 12 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          <h1 style={{ fontSize: 22, fontWeight: 700, color: 'var(--text-primary)', margin: 0 }}>Orders</h1>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          {/* B-007: Saved views selector */}
          {orderViews.length > 0 && (
            <select
              value={activeViewId}
              onChange={e => {
                const v = orderViews.find(v => v.view_id === e.target.value);
                if (v) applyView(v); else setActiveViewId('');
              }}
              style={{ fontSize: 12, padding: '5px 8px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', cursor: 'pointer' }}
            >
              <option value="">All Orders (default)</option>
              {orderViews.map(v => (
                <option key={v.view_id} value={v.view_id}>{v.name}</option>
              ))}
            </select>
          )}
          <button className="btn-sec" style={{ fontSize: 12 }} onClick={() => setShowSaveViewModal(true)} title="Save current filters as a view">
            <Save size={13} /> Save View
          </button>
          {activeViewId && (
            <button className="btn-sec" style={{ fontSize: 12, color: '#ef4444' }} onClick={() => deleteOrderView(activeViewId)} title="Delete this view">
              <X size={13} />
            </button>
          )}
          {/* B-008: Column picker */}
          <div style={{ position: 'relative' }}>
            <button className="btn-sec" style={{ fontSize: 12 }} onClick={() => setShowColPicker(v => !v)} title="Show/hide columns (B-008)">
              <Columns size={13} /> Columns
            </button>
            {showColPicker && (
              <div style={{ position: 'absolute', right: 0, top: '100%', marginTop: 4, background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 8, padding: 12, zIndex: 100, minWidth: 180, boxShadow: '0 8px 24px rgba(0,0,0,0.4)' }}>
                {ALL_COLUMNS.map(col => (
                  <label key={col} style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 12, padding: '4px 0', cursor: 'pointer', color: 'var(--text-secondary)', textTransform: 'capitalize' }}>
                    <input type="checkbox" checked={visibleCols.has(col)} onChange={e => {
                      setVisibleCols(prev => {
                        const next = new Set(prev);
                        e.target.checked ? next.add(col) : next.delete(col);
                        return next;
                      });
                    }} />
                    {col.replace('_', ' ')}
                  </label>
                ))}
              </div>
            )}
          </div>
          {/* Task 1: CSV Order Import */}
          <button className="btn-sec" style={{ fontSize: 13 }} onClick={() => document.getElementById('order-csv-upload')?.click()} title="Import orders from CSV">
            📥 Import CSV
          </button>
          <input id="order-csv-upload" type="file" accept=".csv" style={{ display: 'none' }} onChange={async e => {
            const file = e.target.files?.[0];
            if (!file) return;
            const formData = new FormData();
            formData.append('file', file);
            formData.append('type', 'orders');
            try {
              const valRes = await authFetch(`${API_BASE}/import/validate`, { method: 'POST', body: formData });
              const val = await valRes.json();
              if (val.error_count > 0) {
                alert(`CSV has ${val.error_count} error(s). First error: ${val.errors?.[0]?.message || 'Unknown'}`);
                (e.target as HTMLInputElement).value = '';
                return;
              }
              if (window.confirm(`Import ${val.valid_rows} order(s) from CSV? Warnings: ${val.warning_count}`)) {
                const applyData = new FormData();
                applyData.append('file', file);
                applyData.append('type', 'orders');
                const applyRes = await authFetch(`${API_BASE}/import/apply`, { method: 'POST', body: applyData });
                const apply = await applyRes.json();
                alert(`Import queued (job ${apply.job_id}). ${apply.message}`);
                startImportPolling(apply.job_id);
              }
            } catch (err: any) { alert(`Import failed: ${err.message}`); }
            (e.target as HTMLInputElement).value = '';
          }} />
          {/* B-002: New Order button */}
          <button className="btn-pri" style={{ fontSize: 13 }} onClick={() => setShowNewOrderModal(true)}>
            <Plus size={14} /> New Order
          </button>
        </div>
      </div>

      {/* ── Staleness Warning Banner ── */}
      {staleCredentials.length > 0 && !stalenessWarningDismissed && (
        <div style={{ marginBottom: 16, padding: '14px 18px', background: 'rgba(255,153,0,0.1)', border: '1px solid rgba(255,153,0,0.4)', borderRadius: 10, display: 'flex', alignItems: 'flex-start', gap: 12 }}>
          <span style={{ fontSize: 20, flexShrink: 0 }}>⚠️</span>
          <div style={{ flex: 1 }}>
            <div style={{ fontWeight: 700, fontSize: 14, color: 'var(--text-primary)', marginBottom: 6 }}>
              Order sync overdue for {staleCredentials.length} account{staleCredentials.length > 1 ? 's' : ''}
            </div>
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
              {staleCredentials.map(cred => (
                <div key={cred.credential_id} style={{ display: 'flex', alignItems: 'center', gap: 8, background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 8, padding: '6px 10px', fontSize: 13 }}>
                  <span>{cred.account_name} ({cred.channel})</span>
                  {cred.last_sync && <span style={{ color: 'var(--text-muted)', fontSize: 11 }}>Last: {new Date(cred.last_sync).toLocaleTimeString()}</span>}
                  <button onClick={() => downloadNow(cred.credential_id, cred.channel)} disabled={downloadingFor === cred.credential_id}
                    style={{ background: 'var(--primary)', border: 'none', borderRadius: 6, color: '#fff', padding: '4px 10px', fontSize: 12, cursor: 'pointer', opacity: downloadingFor === cred.credential_id ? 0.6 : 1 }}>
                    {downloadingFor === cred.credential_id ? '⏳' : '⬇ Now'}
                  </button>
                </div>
              ))}
            </div>
          </div>
          <button onClick={() => setStalenessWarningDismissed(true)} style={{ background: 'none', border: 'none', color: 'var(--text-muted)', cursor: 'pointer', fontSize: 18, flexShrink: 0 }}>✕</button>
        </div>
      )}

      {/* Stats */}
      <div className="stats-grid">
        {[
          { label: 'TOTAL', val: stats.total, onClick: () => { setStatusFilter(''); setSpecialFilter(''); setPage(1); } },
          { label: 'IMPORTED', val: stats.imported, onClick: () => { setStatusFilter('imported'); setSpecialFilter(''); setPage(1); } },
          { label: 'PROCESSING', val: stats.processing, onClick: () => { setStatusFilter('processing'); setSpecialFilter(''); setPage(1); } },
          { label: 'READY', val: stats.ready, onClick: () => { setStatusFilter('ready'); setSpecialFilter(''); setPage(1); } },
          { label: 'FULFILLED', val: stats.fulfilled, onClick: () => { setStatusFilter('fulfilled'); setSpecialFilter(''); setPage(1); } },
          { label: 'ON HOLD', val: stats.on_hold, cls: 'stat-hold', onClick: () => {} },
          { label: 'EXCEPTIONS', val: stats.exceptions, cls: 'stat-err', onClick: () => {} },
        ].map(({ label, val, cls, onClick }) => (
          <div key={label} className={`stat ${cls || ''}`} onClick={onClick}>
            <div className="stat-label">{label}</div>
            <div className="stat-value">{val}</div>
          </div>
        ))}
        {/* J-003: Unlinked Items badge */}
        <div
          className={`stat${specialFilter === 'unlinked' ? ' stat-active-special' : ''}`}
          onClick={() => { setSpecialFilter(specialFilter === 'unlinked' ? '' : 'unlinked'); setStatusFilter(''); setPage(1); }}
          title="Orders containing items not linked to any MarketMate product"
          style={{ cursor: 'pointer' }}
        >
          <div className="stat-label" style={{ color: stats.unlinked_items_count > 0 ? '#f59e0b' : undefined }}>UNLINKED</div>
          <div className="stat-value" style={{ color: stats.unlinked_items_count > 0 ? '#f59e0b' : undefined }}>{stats.unlinked_items_count}</div>
        </div>
        {/* J-004: Composite/Bundle Items badge */}
        <div
          className={`stat${specialFilter === 'composite' ? ' stat-active-special' : ''}`}
          onClick={() => { setSpecialFilter(specialFilter === 'composite' ? '' : 'composite'); setStatusFilter(''); setPage(1); }}
          title="Orders containing bundle/composite items"
          style={{ cursor: 'pointer' }}
        >
          <div className="stat-label" style={{ color: stats.composite_items_count > 0 ? '#a78bfa' : undefined }}>COMPOSITE</div>
          <div className="stat-value" style={{ color: stats.composite_items_count > 0 ? '#a78bfa' : undefined }}>{stats.composite_items_count}</div>
        </div>
      </div>

      {/* Toolbar */}
      <div className="toolbar">
        <div className="search-box" style={{ display: 'flex', alignItems: 'center', gap: 0 }}>
          <Search size={15} color="#4b5f7c" style={{ flexShrink: 0 }} />
          <select
            value={searchField}
            onChange={e => setSearchField(e.target.value)}
            style={{
              background: 'none', border: 'none', color: 'var(--text-muted)',
              fontSize: 12, cursor: 'pointer', padding: '0 4px', outline: 'none',
              flexShrink: 0,
            }}
          >
            <option value="pii_email_token">Email</option>
            <option value="pii_name_token">Name</option>
            <option value="pii_postcode_token">Postcode</option>
            <option value="pii_phone_token">Phone</option>
            <option value="order_ref">Order Ref / SKU</option>
          </select>
          <input
            placeholder="Search by email, name, postcode or phone…"
            value={search}
            onChange={e => setSearch(e.target.value)}
            style={{ flex: 1 }}
          />
        </div>
        <div className="toolbar-actions">
          {/* ── Actions Menu ── */}
          <OrderActionsMenu
            variant="toolbar"
            selectedOrderIds={Array.from(selected)}
            singleOrder={selected.size === 1 ? (orders.find(o => getOrderId(o) === Array.from(selected)[0]) ?? null) : null}
            onViewOrder={id => { const o = orders.find(o => getOrderId(o) === id); if (o) setDetailOrder(o); }}
            onAssignFolder={openFolderModal}
            onAssignTag={ids => { setTagModalOrderIds(ids); setShowTagModal(true); }}
            onAssignIdentifier={openIdentifierModal}
            onMoveToLocation={ids => openLocationModal(ids, 'location')}
            onMoveToFulfilmentCenter={ids => openLocationModal(ids, 'fulfilment')}
            onBatchAssignment={openBatchAssignModal}
            onAutoAssignBatches={doAutoAssignBatches}
            onClearBatches={doClearBatches}
            onLinkUnlinkedItems={doLinkUnlinkedItems}
            onAddItemsToPO={doAddItemsToPO}
            onChangeService={openChangeServiceModal}
            onGetQuotes={openGetQuotes}
            onCancelLabel={doCancelLabel}
            onSplitPackaging={openSplitPackagingModal}
            onChangeDispatchDate={openDispatchDateModal}
            onChangeDeliveryDates={openDeliveryDatesModal}
            onPrintInvoice={ids => {
              if (ids.length === 1) { const o = orders.find(o => getOrderId(o) === ids[0]); if (o) printInvoice(o); }
              else alert('Select a single order to print invoice.');
            }}
            onPrintShippingLabel={id => { const o = orders.find(o => getOrderId(o) === id); if (o) setLabelOrder(o); }}
            onPrintPickList={() => generatePicklist()}
            onPrintPackList={id => { const o = orders.find(o => getOrderId(o) === id); if (o) printPackingSlip(o); }}
            onPrintStockItemLabel={printStockItemLabel}
            onProcessOrder={doProcessOrder}
            onBatchProcess={doBatchProcess}
            onChangeStatus={() => { setBulkStatus(''); setShowBulkStatusModal(true); }}
            onViewOrderNotes={openNotesModal}
            onViewOrderXML={openXMLModal}
            onSplitOrder={id => { const o = orders.find(o => getOrderId(o) === id); if (o) openSplit(o); }}
            onDeleteOrder={openDeleteOrderModal}
            onCancelOrder={id => { const o = orders.find(o => getOrderId(o) === id); if (o) { setCancelOrder(o); setCancelReason(''); setCancelNotes(''); } }}
            onRunRulesEngine={doRunRulesEngine}
          />

          {/* ── Always-visible toolbar buttons ── */}
          <button className="btn-sec" onClick={() => setShowFilters(v => !v)}>
            <Filter size={14} /> Filters
          </button>
          <button className="btn-sec" onClick={loadOrders}>
            <RefreshCw size={14} /> Refresh
          </button>
          <button className="btn-sec" onClick={exportOrdersCSV} disabled={exportingCSV} title="Export Orders CSV">
            <Download size={14} /> {exportingCSV ? 'Exporting…' : 'Export CSV'}
          </button>

          {/* ── Selection-dependent utilities ── */}
          {selected.size >= 2 && (
            <button className="btn-sec" onClick={openMerge} title="Merge selected orders (Shift+M)">
              <GitMerge size={14} /> Merge ({selected.size})
            </button>
          )}
          {selected.size > 0 && (
            <button
              className="btn-sec"
              onClick={async () => {
                const orderIds = Array.from(selected);
                const name = prompt(`Create pick wave for ${orderIds.length} order${orderIds.length !== 1 ? 's' : ''}?\n\nWave name (optional):`);
                if (name === null) return;
                try {
                  const res = await authFetch(`${API_BASE}/pickwaves`, {
                    method: 'POST',
                    body: JSON.stringify({ order_ids: orderIds, name: name.trim() || undefined }),
                  });
                  if (!res.ok) throw new Error('Create failed');
                  const data = await res.json();
                  alert(`✅ Pick wave created: ${data.pickwave?.name || 'Wave'}\nView it in Fulfilment → Pick Waves.`);
                } catch (e: any) {
                  alert(`Failed to create pick wave: ${e.message}`);
                }
              }}
              title="Create pick wave from selected orders"
            >
              📋 Pick Wave ({selected.size})
            </button>
          )}
          {selected.size > 0 && (
            <span style={{ fontSize: 11, color: 'var(--text-muted)', alignSelf: 'center', marginLeft: 4, whiteSpace: 'nowrap' }}>
              {selected.size} selected
            </span>
          )}
        </div>
      </div>

      {/* Filters */}
      {showFilters && (
        <div className="filters-panel">
          <div className="filter-grid">
            <div>
              <label>Channel</label>
              <select value={channelFilter} onChange={e => { setChannelFilter(e.target.value); setPage(1); }}>
                <option value="">All channels</option>
                <optgroup label="Established">
                  <option value="amazon">Amazon</option>
                  <option value="ebay">eBay</option>
                  <option value="shopify">Shopify</option>
                  <option value="temu">Temu</option>
                  <option value="tiktok">TikTok</option>
                  <option value="etsy">Etsy</option>
                  <option value="woocommerce">WooCommerce</option>
                  <option value="walmart">Walmart</option>
                  <option value="kaufland">Kaufland</option>
                  <option value="magento">Magento</option>
                  <option value="bigcommerce">BigCommerce</option>
                  <option value="onbuy">OnBuy</option>
                </optgroup>
                <optgroup label="Session 4 — New Channels">
                  <option value="backmarket">Back Market</option>
                  <option value="zalando">Zalando</option>
                  <option value="bol">Bol.com</option>
                  <option value="lazada">Lazada</option>
                </optgroup>
              </select>
            </div>
            <div>
              <label>Status</label>
              <select value={statusFilter} onChange={e => { setStatusFilter(e.target.value); setPage(1); }}>
                <option value="">All statuses</option>
                <option value="imported">Imported</option>
                <option value="processing">Processing</option>
                <option value="ready">Ready</option>
                <option value="fulfilled">Fulfilled</option>
                <option value="dispatched">Dispatched</option>
                <option value="completed">Completed</option>
                <option value="cancelled">Cancelled</option>
                <option value="on_hold">On Hold</option>
              </select>
            </div>

            {/* Task 7: Date Range Filters */}
            <div>
              <label>Received Date</label>
              <div style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
                <input type="date" value={receivedFrom} onChange={e => { setReceivedFrom(e.target.value); setPage(1); }}
                  style={{ fontSize: 12, padding: '5px 8px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)' }} />
                <span style={{ color: 'var(--text-muted)', fontSize: 12 }}>–</span>
                <input type="date" value={receivedTo} onChange={e => { setReceivedTo(e.target.value); setPage(1); }}
                  style={{ fontSize: 12, padding: '5px 8px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)' }} />
              </div>
            </div>
            <div>
              <label>Despatch By Date</label>
              <div style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
                <input type="date" value={despatchFrom} onChange={e => { setDespatchFrom(e.target.value); setPage(1); }}
                  style={{ fontSize: 12, padding: '5px 8px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)' }} />
                <span style={{ color: 'var(--text-muted)', fontSize: 12 }}>–</span>
                <input type="date" value={despatchTo} onChange={e => { setDespatchTo(e.target.value); setPage(1); }}
                  style={{ fontSize: 12, padding: '5px 8px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)' }} />
              </div>
            </div>
            <div>
              <label>Delivery Date</label>
              <div style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
                <input type="date" value={deliveryFrom} onChange={e => { setDeliveryFrom(e.target.value); setPage(1); }}
                  style={{ fontSize: 12, padding: '5px 8px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)' }} />
                <span style={{ color: 'var(--text-muted)', fontSize: 12 }}>–</span>
                <input type="date" value={deliveryTo} onChange={e => { setDeliveryTo(e.target.value); setPage(1); }}
                  style={{ fontSize: 12, padding: '5px 8px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)' }} />
              </div>
            </div>

            {/* Task 7: Date quick presets */}
            <div>
              <label>Quick Ranges</label>
              <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                {[
                  { label: 'Today', f: new Date().toISOString().split('T')[0], t: new Date().toISOString().split('T')[0] },
                  { label: '7 Days', f: (() => { const d = new Date(); d.setDate(d.getDate()-7); return d.toISOString().split('T')[0]; })(), t: new Date().toISOString().split('T')[0] },
                  { label: '30 Days', f: (() => { const d = new Date(); d.setDate(d.getDate()-30); return d.toISOString().split('T')[0]; })(), t: new Date().toISOString().split('T')[0] },
                ].map(p => (
                  <button key={p.label} className="btn-sec" style={{ fontSize: 11, padding: '4px 8px' }}
                    onClick={() => { setReceivedFrom(p.f); setReceivedTo(p.t); setPage(1); }}>
                    {p.label}
                  </button>
                ))}
              </div>
            </div>

            {/* Fix 2A: Due Today / Overdue despatch quick filters */}
            <div>
              <label>Despatch Quick Filters</label>
              <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                <button
                  className={dueTodayActive ? 'btn-pri' : 'btn-sec'}
                  style={{ fontSize: 11, padding: '4px 10px', fontWeight: dueTodayActive ? 700 : 400 }}
                  onClick={() => { setDueTodayActive(!dueTodayActive); setOverdueActive(false); setPage(1); }}
                  title="Show orders where despatch-by date is today or earlier">
                  📅 Due Today
                </button>
                <button
                  className={overdueActive ? 'btn-pri' : 'btn-sec'}
                  style={{ fontSize: 11, padding: '4px 10px', fontWeight: overdueActive ? 700 : 400, background: overdueActive ? '#ef4444' : undefined }}
                  onClick={() => { setOverdueActive(!overdueActive); setDueTodayActive(false); setPage(1); }}
                  title="Show orders where despatch-by date is yesterday or earlier (overdue)">
                  ⚠ Overdue
                </button>
              </div>
            </div>

            {/* Fix 1B: Folder filter */}
            <div>
              <label>Folder</label>
              <select value={folderFilter} onChange={e => { setFolderFilter(e.target.value); setPage(1); }}
                style={{ fontSize: 12, padding: '5px 8px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', minWidth: 150 }}>
                <option value="">All folders</option>
                {allFolders.map(f => (
                  <option key={f.folder_id} value={f.folder_id}>{f.name}</option>
                ))}
              </select>
            </div>

            {/* Task 8: Shipping filters */}
            <div>
              <label>Shipping Service</label>
              <input placeholder="e.g. Royal Mail 2nd" value={shippingServiceFilter}
                onChange={e => { setShippingServiceFilter(e.target.value); setPage(1); }}
                style={{ fontSize: 12, padding: '5px 8px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', width: 180 }} />
            </div>
            <div>
              <label>Destination Country</label>
              <input placeholder="e.g. GB, US, DE" value={destinationCountryFilter}
                onChange={e => { setDestinationCountryFilter(e.target.value); setPage(1); }}
                style={{ fontSize: 12, padding: '5px 8px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', width: 100 }} />
            </div>
            <div>
              <label>Carrier</label>
              <input placeholder="e.g. Royal Mail, DPD" value={carrierFilter}
                onChange={e => { setCarrierFilter(e.target.value); setPage(1); }}
                style={{ fontSize: 12, padding: '5px 8px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', width: 140 }} />
            </div>

            <div style={{ alignSelf: 'flex-end' }}>
              <button className="btn-sec" onClick={() => {
                setSearch(''); setStatusFilter(''); setChannelFilter(''); setSpecialFilter('');
                setReceivedFrom(''); setReceivedTo(''); setDespatchFrom(''); setDespatchTo('');
                setDeliveryFrom(''); setDeliveryTo(''); setShippingServiceFilter('');
                setDestinationCountryFilter(''); setCarrierFilter('');
                setFolderFilter(''); setDueTodayActive(false); setOverdueActive(false);
                setPage(1);
              }}>
                Clear all
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Pagination — top */}
      <PaginationBar
        page={page}
        totalPages={totalPages}
        totalOrders={totalOrders}
        ordersOnPage={orders.length}
        pageSize={pageSize}
        selectedCount={selected.size}
        onPageChange={p => { setPage(p); }}
        onPageSizeChange={n => { setPageSize(n); setPage(1); }}
      />

      {/* ── Floating Bulk Action Bar ── */}
      {selected.size > 0 && (
        <div style={{
          position: 'fixed', bottom: 24, left: '50%', transform: 'translateX(-50%)',
          background: 'var(--bg-elevated)', border: '1px solid var(--border)',
          borderRadius: 12, padding: '12px 20px', zIndex: 200,
          boxShadow: '0 8px 32px rgba(0,0,0,0.45)',
          display: 'flex', alignItems: 'center', gap: 12, minWidth: 480,
        }}>
          <span style={{ fontSize: 13, fontWeight: 600, color: 'var(--text-primary)', whiteSpace: 'nowrap' }}>
            {selected.size} order{selected.size !== 1 ? 's' : ''} selected
          </span>
          <div style={{ flex: 1, display: 'flex', alignItems: 'center', gap: 8 }}>
            <select
              value={bulkStatus}
              onChange={e => setBulkStatus(e.target.value)}
              style={{ flex: 1, fontSize: 13, padding: '6px 10px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)' }}
            >
              <option value="">Change status to…</option>
              {ORDER_STATUSES.map(s => (
                <option key={s.value} value={s.value}>{s.label}</option>
              ))}
            </select>
            <button
              onClick={applyBulkStatus}
              disabled={!bulkStatus || applyingBulkStatus}
              style={{
                background: bulkStatus ? 'var(--primary)' : 'var(--bg-secondary)',
                color: bulkStatus ? '#fff' : 'var(--text-muted)',
                border: '1px solid var(--border)', borderRadius: 6,
                padding: '6px 16px', fontSize: 13, fontWeight: 600,
                cursor: bulkStatus ? 'pointer' : 'default',
                opacity: applyingBulkStatus ? 0.6 : 1, whiteSpace: 'nowrap',
              }}
            >
              {applyingBulkStatus ? 'Applying…' : 'Apply'}
            </button>
          </div>
          {/* S4 Bulk Ship — only shown when all selected orders are same S4 channel */}
          {allSameS4Channel && (
            <button
              onClick={openBulkShip}
              style={{
                background: S4_CHANNELS.find(c => c.id === s4SelectedChannel)?.color || 'var(--primary)',
                color: '#fff', border: 'none', borderRadius: 6,
                padding: '6px 14px', fontSize: 13, fontWeight: 600,
                cursor: 'pointer', whiteSpace: 'nowrap', flexShrink: 0,
              }}
              title={`Bulk ship ${selected.size} ${s4SelectedChannel} order${selected.size !== 1 ? 's' : ''} with tracking numbers`}
            >
              🚚 Bulk Ship ({s4SelectedChannel})
            </button>
          )}
          {/* S4 Export — shown when a S4 channel filter is active */}
          {S4_CHANNEL_IDS.has(channelFilter) && (() => {
            const ch = S4_CHANNELS.find(c => c.id === channelFilter)!;
            return (
              <button
                onClick={() => exportS4Channel(channelFilter)}
                disabled={exportingS4Channel}
                style={{
                  background: ch.color, color: '#fff', border: 'none', borderRadius: 6,
                  padding: '6px 14px', fontSize: 13, fontWeight: 600,
                  cursor: 'pointer', whiteSpace: 'nowrap', flexShrink: 0, opacity: exportingS4Channel ? 0.6 : 1,
                }}
                title={`Export all ${ch.label} orders to CSV`}
              >
                {exportingS4Channel ? '⏳ Exporting…' : `⬆ Export ${ch.label}`}
              </button>
            );
          })()}
          <button
            onClick={() => setSelected(new Set())}
            style={{ background: 'none', border: 'none', color: 'var(--text-muted)', cursor: 'pointer', fontSize: 18, lineHeight: 1, padding: '0 4px' }}
            title="Clear selection"
          >×</button>
        </div>
      )}

      {/* ── SINGLE TABLE with sticky STATUS column on the right ── */}
      <div className="table-outer">
        <div className="table-scroll-x">
          <table className="orders-table">
            <thead>
              <tr>
                <th className="col-cb">
                  <input
                    type="checkbox"
                    checked={selected.size === orders.length && orders.length > 0}
                    onChange={toggleAll}
                    onClick={e => e.stopPropagation()}
                  />
                </th>
                <th className="col-channel">Channel &amp; Reference</th>
                <th className="col-value">Order Value</th>
                {visibleCols.has('tags') && <th className="col-tags">Tags</th>}
                {visibleCols.has('date') && (
                  <th className="col-date" style={{ cursor: 'pointer', userSelect: 'none' }}
                    onClick={() => { const cur = sortFields.find(s => s.field === 'order_date'); setSortFields([{ field: 'order_date', direction: cur?.direction === 'asc' ? 'desc' : 'asc' }]); setPage(1); }}>
                    Date {sortFields.find(s => s.field === 'order_date') ? (sortFields.find(s => s.field === 'order_date')?.direction === 'asc' ? '↑' : '↓') : ''}
                  </th>
                )}
                {/* Task 2: Despatch-By Date */}
                {visibleCols.has('despatch_by') && (
                  <th className="col-date" style={{ cursor: 'pointer', userSelect: 'none', whiteSpace: 'nowrap' }}
                    onClick={() => { const cur = sortFields.find(s => s.field === 'despatch_by_date'); setSortFields([{ field: 'despatch_by_date', direction: cur?.direction === 'asc' ? 'desc' : 'asc' }]); setPage(1); }}>
                    Despatch By {sortFields.find(s => s.field === 'despatch_by_date') ? (sortFields.find(s => s.field === 'despatch_by_date')?.direction === 'asc' ? '↑' : '↓') : ''}
                  </th>
                )}
                {/* Task 3: Scheduled Delivery Date */}
                {visibleCols.has('delivery_date') && (
                  <th className="col-date" style={{ cursor: 'pointer', userSelect: 'none', whiteSpace: 'nowrap' }}
                    onClick={() => { const cur = sortFields.find(s => s.field === 'scheduled_delivery_date'); setSortFields([{ field: 'scheduled_delivery_date', direction: cur?.direction === 'asc' ? 'desc' : 'asc' }]); setPage(1); }}>
                    Delivery Date {sortFields.find(s => s.field === 'scheduled_delivery_date') ? (sortFields.find(s => s.field === 'scheduled_delivery_date')?.direction === 'asc' ? '↑' : '↓') : ''}
                  </th>
                )}
                {visibleCols.has('batch') && <th className="col-batch">Batch</th>}
                {visibleCols.has('customer') && <th className="col-customer">Customer</th>}
                {visibleCols.has('products') && <th className="col-products">Product Details</th>}
                {visibleCols.has('package') && <th className="col-pkg">Package Format</th>}
                {visibleCols.has('service') && <th className="col-svc">Shipping Service</th>}
                {visibleCols.has('label') && <th className="col-action">Print Label</th>}
                {/* Task 10: Invoice print status */}
                {visibleCols.has('invoice') && <th className="col-invoice" style={{ whiteSpace: 'nowrap' }}>Invoice</th>}
                {/* Task 11: Payment status */}
                {visibleCols.has('payment') && <th style={{ whiteSpace: 'nowrap' }}>Payment</th>}
                <th className="col-status">Status</th>
                <th style={{ width: 100 }}>Actions</th>
              </tr>
            </thead>
            <tbody>
              {loading ? (
                <tr>
                  <td colSpan={13} className="state-cell">
                    <span className="spinner" /> Loading orders…
                  </td>
                </tr>
              ) : orders.length === 0 ? (
                <tr><td colSpan={13} className="state-cell">No orders found</td></tr>
              ) : orders.map(order => {
                const id = getOrderId(order);
                const loc = getLoc(id);
                const held = loc.on_hold;
                const sel = selected.has(id);
                const { name: custName, location: custLoc } = getCustomerDisplay(order);
                const total = getGrandTotal(order);
                const currency = getCurrency(order);
                const df = fmtDate(order.order_date);
                const lines = getLines(order, loc);

                return (
                  <tr
                    key={id}
                    className={['order-row', held ? 'row-hold' : '', sel ? 'row-sel' : ''].filter(Boolean).join(' ')}
                  >
                    <td className="col-cb" onClick={e => e.stopPropagation()}>
                      <input
                        type="checkbox"
                        checked={sel}
                        onChange={e => toggleOne(e, id)}
                        onClick={e => e.stopPropagation()}
                      />
                    </td>

                    <td className="col-channel">
                      <div className="ch-badge" style={
                        order.channel === 'backmarket' ? { background: '#14B8A6', color: '#fff' } :
                        order.channel === 'zalando'    ? { background: '#FF6600', color: '#fff' } :
                        order.channel === 'bol'        ? { background: '#0E4299', color: '#fff' } :
                        order.channel === 'lazada'     ? { background: '#F57224', color: '#fff' } :
                        undefined
                      }>{(order.channel || '').toUpperCase() || '—'}</div>
                      <div className="order-ref">{getDisplayRef(order)}</div>
                      {held && (
                        <div className="hold-pill">
                          <Lock size={9} /> HOLD{loc.hold_reason ? `: ${loc.hold_reason}` : ''}
                        </div>
                      )}
                    </td>

                    <td className="col-value">
                      <div className="cur-code">{currency}</div>
                      <div className="amt">{total !== null ? fmtMoney(total, currency) : '—'}</div>
                    </td>

                    <td className="col-tags">
                      {order.sla_at_risk && <span className="tag tag-sla"><AlertCircle size={9} /> SLA</span>}
                      {/* Task 6: Show tag shapes */}
                      {(order.tags || []).map(tagId => {
                        const def = tagDefinitions.find(t => t.tag_id === tagId);
                        if (!def) return <span key={tagId} className="tag" style={{ background: 'rgba(107,114,128,0.2)', color: '#94a3b8', fontSize: 10, borderRadius: 4, padding: '1px 6px', marginRight: 3 }}>{tagId}</span>;
                        return (
                          <span key={tagId} title={def.name} style={{ display: 'inline-flex', alignItems: 'center', gap: 3, marginRight: 3, cursor: 'pointer' }}
                            onClick={e => { e.stopPropagation(); removeTagFromOrders(tagId, [id]); }}>
                            <TagShape shape={def.shape} color={def.color} />
                          </span>
                        );
                      })}
                      {/* Fix 1B: Folder badge */}
                      {order.folder_name && (
                        <span title={`Folder: ${order.folder_name}`} style={{ display: 'inline-flex', alignItems: 'center', gap: 3, background: 'rgba(59,130,246,0.12)', color: '#3b82f6', border: '1px solid rgba(59,130,246,0.25)', borderRadius: 4, fontSize: 10, padding: '1px 6px', marginRight: 3, whiteSpace: 'nowrap' }}>
                          📁 {order.folder_name}
                        </span>
                      )}
                      {/* Fix 1C: Identifier badge */}
                      {order.identifier && (
                        <span title={`Identifier: ${order.identifier}`} style={{ display: 'inline-flex', alignItems: 'center', gap: 2, background: 'rgba(107,114,128,0.1)', color: '#6b7280', border: '1px solid rgba(107,114,128,0.2)', borderRadius: 4, fontSize: 10, padding: '1px 6px', marginRight: 3, fontFamily: 'monospace', whiteSpace: 'nowrap' }}>
                          #{order.identifier}
                        </span>
                      )}
                      {/* Fix 1D: Fulfilment centre badge */}
                      {order.fulfilment_center_name && (
                        <span title={`Fulfilment Centre: ${order.fulfilment_center_name}`} style={{ display: 'inline-flex', alignItems: 'center', gap: 2, background: 'rgba(168,85,247,0.1)', color: '#a855f7', border: '1px solid rgba(168,85,247,0.25)', borderRadius: 4, fontSize: 10, padding: '1px 6px', marginRight: 3, whiteSpace: 'nowrap' }}>
                          🏢 {order.fulfilment_center_name}
                        </span>
                      )}
                      {/* Task 11: Granular payment status */}
                      {order.payment_status === 'captured' && <span className="tag tag-paid">Paid</span>}
                      {order.payment_status === 'partial' && <span className="tag" style={{ background: 'rgba(245,158,11,0.15)', color: '#f59e0b', border: '1px solid rgba(245,158,11,0.3)' }}>Part Paid</span>}
                      {(order.payment_status === 'pending' || order.payment_status === 'unpaid') && <span className="tag" style={{ background: 'rgba(239,68,68,0.15)', color: '#ef4444', border: '1px solid rgba(239,68,68,0.3)' }}>Unpaid</span>}
                    </td>

                    <td className="col-date">
                      <div className="dt-main">{df.main}</div>
                      <div className="dt-sub">{df.year}</div>
                      <div className="dt-sub">{df.time}</div>
                    </td>

                    {/* Task 2: Despatch-By Date */}
                    {visibleCols.has('despatch_by') && (order.despatch_by_date ? (() => {
                      const dbdDate = new Date(order.despatch_by_date!);
                      const hoursUntil = (dbdDate.getTime() - Date.now()) / 3600000;
                      const color = hoursUntil < 0 ? '#ef4444' : hoursUntil < 24 ? '#f59e0b' : undefined;
                      return <td className="col-date" style={{ color }}><div className="dt-main" style={{ fontWeight: color ? 700 : 400 }}>{hoursUntil < 0 && '⚠ '}{dbdDate.toLocaleDateString('en-GB', { day: 'numeric', month: 'short' })}</div></td>;
                    })() : <td className="col-date muted">—</td>)}

                    {/* Task 3: Scheduled Delivery Date */}
                    {visibleCols.has('delivery_date') && (order.scheduled_delivery_date ? <td className="col-date"><div className="dt-main">{new Date(order.scheduled_delivery_date).toLocaleDateString('en-GB', { day: 'numeric', month: 'short' })}</div></td> : <td className="col-date muted">—</td>)}

                    <td className="col-batch muted">—</td>

                    <td className="col-customer">
                      {custName
                        ? <div className="cust-name">{custName}</div>
                        : <div className="muted">—</div>
                      }
                      {custLoc && <div className="cust-loc">{custLoc}</div>}
                    </td>

                    <td className="col-products">
                      {lines.length > 0 ? (
                        <>
                          {lines.slice(0, 3).map((item, i) => (
                            <div key={i} className="prod-row">
                              <div className="prod-info">
                                <div className="prod-line">
                                  <span className="qty">{item.quantity ?? 1}×</span>
                                  <span className="prod-name">{item.title || '—'}</span>
                                </div>
                                {item.sku && <div className="prod-sku"><span className="sku">{item.sku}</span></div>}
                              </div>
                            </div>
                          ))}
                          {lines.length > 3 && (
                            <button className="view-all-btn" onClick={e => { e.stopPropagation(); setDetailOrder(order); }}>
                              View all {lines.length} items →
                            </button>
                          )}
                        </>
                      ) : loc.lines_loaded ? (
                        <span className="muted">No items</span>
                      ) : (
                        <span className="muted">Loading…</span>
                      )}
                    </td>

                    <td className="col-pkg">
                      <select
                        className="inline-sel"
                        value={loc.package_format}
                        onChange={e => patchLoc(id, { package_format: e.target.value })}
                        onClick={e => e.stopPropagation()}
                      >
                        {PACKAGE_OPTIONS.map(o => <option key={o}>{o}</option>)}
                      </select>
                    </td>

                    <td className="col-svc">
                      <select
                        className="inline-sel"
                        value={loc.shipping_service}
                        onChange={e => patchLoc(id, { shipping_service: e.target.value })}
                        onClick={e => e.stopPropagation()}
                      >
                        {SHIPPING_OPTIONS.map(o => <option key={o}>{o}</option>)}
                      </select>
                    </td>

                    {/* Print Label column */}
                    {visibleCols.has('label') && (
                    <td className="col-action">
                      {loc.label_generated ? (
                        <div className="status-done">
                          <button className="btn-sm btn-dl" title="Download label"
                            onClick={() => window.open(loc.label_url, '_blank')}>
                            <Download size={13} />
                          </button>
                          <button className="btn-sm btn-void" title="Cancel shipment"
                            onClick={() => patchLoc(id, { label_generated: false })}>
                            <XCircle size={13} />
                          </button>
                        </div>
                      ) : held ? (
                        <button className="btn-status btn-held" disabled>
                          <Lock size={11} /> On Hold
                        </button>
                      ) : (
                        <button className="btn-status btn-print"
                          onClick={() => setLabelOrder(order)}>
                          <Printer size={11} /> Print Label
                        </button>
                      )}
                    </td>
                    )}

                    {/* Task 10: Invoice print status */}
                    {visibleCols.has('invoice') && (
                      <td style={{ textAlign: 'center', fontSize: 12 }}>
                        {order.invoice_printed
                          ? <span title={`Printed ${order.invoice_printed_at ? new Date(order.invoice_printed_at).toLocaleString() : ''}`} style={{ color: '#22c55e', fontWeight: 600 }}>✓</span>
                          : <span style={{ color: 'var(--text-muted)' }}>—</span>}
                      </td>
                    )}

                    {/* Task 11: Granular payment status column */}
                    {visibleCols.has('payment') && (
                      <td style={{ fontSize: 12 }}>
                        {order.payment_status === 'captured' || order.payment_status === 'paid'
                          ? <span style={{ color: '#22c55e', fontWeight: 600 }}>Paid</span>
                          : order.payment_status === 'partial'
                          ? <span style={{ color: '#f59e0b', fontWeight: 600 }}>Part Paid</span>
                          : order.payment_status === 'pending' || order.payment_status === 'unpaid'
                          ? <span style={{ color: '#ef4444', fontWeight: 600 }}>Unpaid</span>
                          : <span style={{ color: 'var(--text-muted)' }}>{order.payment_status || '—'}</span>}
                      </td>
                    )}

                    {/* Status badge column */}
                    <td className="col-status">
                      <StatusBadge status={order.status} held={held} />
                    </td>

                    {/* B-003/B-005/B-006: Row actions — now via Actions menu */}
                    <td style={{ whiteSpace: 'nowrap' }} onClick={e => e.stopPropagation()}>
                      <div style={{ display: 'flex', gap: 4, alignItems: 'center' }}>
                        <button
                          className="btn-sm"
                          title="Edit order (B-003)"
                          onClick={() => openEditOrder(order)}
                          style={{ fontSize: 11, padding: '3px 7px' }}
                        >
                          <Edit2 size={11} />
                        </button>
                        <OrderActionsMenu
                          variant="row"
                          selectedOrderIds={[]}
                          singleOrder={{ order_id: id, external_order_id: order.external_order_id, order_number: order.order_number, status: order.status }}
                          onViewOrder={oid => { const o = orders.find(o => getOrderId(o) === oid); if (o) setDetailOrder(o); }}
                          onAssignFolder={openFolderModal}
                          onAssignTag={ids => { setTagModalOrderIds(ids); setShowTagModal(true); }}
                          onAssignIdentifier={openIdentifierModal}
                          onMoveToLocation={ids => openLocationModal(ids, 'location')}
                          onMoveToFulfilmentCenter={ids => openLocationModal(ids, 'fulfilment')}
                          onBatchAssignment={openBatchAssignModal}
                          onAutoAssignBatches={doAutoAssignBatches}
                          onClearBatches={doClearBatches}
                          onLinkUnlinkedItems={doLinkUnlinkedItems}
                          onAddItemsToPO={doAddItemsToPO}
                          onChangeService={openChangeServiceModal}
                          onGetQuotes={openGetQuotes}
                          onCancelLabel={doCancelLabel}
                          onSplitPackaging={openSplitPackagingModal}
                          onChangeDispatchDate={openDispatchDateModal}
                          onChangeDeliveryDates={openDeliveryDatesModal}
                          onPrintInvoice={() => printInvoice(order)}
                          onPrintShippingLabel={() => setLabelOrder(order)}
                          onPrintPickList={() => generatePicklist()}
                          onPrintPackList={() => printPackingSlip(order)}
                          onPrintStockItemLabel={printStockItemLabel}
                          onProcessOrder={doProcessOrder}
                          onBatchProcess={doBatchProcess}
                          onChangeStatus={() => { setBulkStatus(''); setShowBulkStatusModal(true); }}
                          onViewOrderNotes={openNotesModal}
                          onViewOrderXML={openXMLModal}
                          onSplitOrder={() => openSplit(order)}
                          onDeleteOrder={openDeleteOrderModal}
                          onCancelOrder={() => { setCancelOrder(order); setCancelReason(''); setCancelNotes(''); }}
                          onRunRulesEngine={doRunRulesEngine}
                        />
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </div>

      {/* Pagination — bottom */}
      <PaginationBar
        page={page}
        totalPages={totalPages}
        totalOrders={totalOrders}
        ordersOnPage={orders.length}
        pageSize={pageSize}
        selectedCount={selected.size}
        onPageChange={p => { setPage(p); window.scrollTo({ top: 0, behavior: 'smooth' }); }}
        onPageSizeChange={n => { setPageSize(n); setPage(1); }}
      />

      {/* Order Detail Modal */}
      {detailOrder && (
        <OrderDetailModal
          order={detailOrder}
          loc={getLoc(getOrderId(detailOrder))}
          onClose={() => setDetailOrder(null)}
          onPrintLabel={o => setLabelOrder(o)}
          onPrintPackingSlip={o => { setDetailOrder(null); printPackingSlip(o); }}
        />
      )}

      {/* Label Modal */}      {/* Label Modal */}
      {labelOrder && (
        <div className="modal-bg" onClick={() => setLabelOrder(null)}>
          <div className="modal" onClick={e => e.stopPropagation()}>
            <div className="modal-head">
              <h2><Printer size={15} /> Generate Shipping Label</h2>
              <button className="btn-x" onClick={() => setLabelOrder(null)}>×</button>
            </div>
            <div className="modal-body">
              <div className="modal-ref">{getDisplayRef(labelOrder)}</div>
              <div className="modal-cust">
                {getCustomerDisplay(labelOrder).name || '—'}
                {getCustomerDisplay(labelOrder).location && <> · {getCustomerDisplay(labelOrder).location}</>}
              </div>
              {getLines(labelOrder, getLoc(getOrderId(labelOrder))).length > 0 && (
                <div className="modal-items">
                  {getLines(labelOrder, getLoc(getOrderId(labelOrder))).map((item, i) => (
                    <div key={i} className="modal-item">
                      <span className="qty">{item.quantity ?? 1}×</span>
                      <span>{item.title || '—'}</span>
                      {item.sku && <span className="sku">{item.sku}</span>}
                    </div>
                  ))}
                </div>
              )}
              <div className="modal-warn"><AlertCircle size={13} /> This will deduct stock from inventory</div>
              <div className="modal-actions">
                <button className="btn-sec" onClick={() => setLabelOrder(null)}>Cancel</button>
                <button className="btn-pri" onClick={generateLabel} disabled={generatingLabel}>
                  {generatingLabel ? 'Generating…' : 'Generate & Print'}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Hold Modal */}
      {showHoldModal && (
        <div className="modal-bg" onClick={() => setShowHoldModal(false)}>
          <div className="modal" onClick={e => e.stopPropagation()}>
            <div className="modal-head">
              <h2>
                {holdAction === 'hold'
                  ? <><Lock size={15} /> Hold Orders</>
                  : <><Unlock size={15} /> Release Orders</>}
              </h2>
              <button className="btn-x" onClick={() => setShowHoldModal(false)}>×</button>
            </div>
            <div className="modal-body">
              <p className="modal-count">{selected.size} order{selected.size !== 1 ? 's' : ''} selected</p>
              {holdAction === 'hold' && (
                <div className="field-grp">
                  <label>Reason</label>
                  <select className="inline-sel wide" value={holdReason} onChange={e => setHoldReason(e.target.value)}>
                    <option value="">Select a reason…</option>
                    {HOLD_REASONS.map(r => <option key={r}>{r}</option>)}
                  </select>
                </div>
              )}
              <div className="modal-actions">
                <button className="btn-sec" onClick={() => setShowHoldModal(false)}>Cancel</button>
                <button className="btn-pri" onClick={applyHold}>
                  {holdAction === 'hold' ? 'Apply Hold' : 'Release Orders'}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
      {/* ── B-002: New Order Modal ── */}
      {showNewOrderModal && (
        <div className="modal-bg" onClick={() => setShowNewOrderModal(false)}>
          <div className="modal" style={{ width: 600, maxWidth: '95vw', maxHeight: '85vh', overflowY: 'auto' }} onClick={e => e.stopPropagation()}>
            <div className="modal-head">
              <h2><Plus size={15} /> New Manual Order</h2>
              <button className="btn-x" onClick={() => setShowNewOrderModal(false)}>×</button>
            </div>
            <div className="modal-body">
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12, marginBottom: 12 }}>
                <div className="field-grp">
                  <label>Customer Name *</label>
                  <input value={newOrderForm.customer_name} onChange={e => setNewOrderForm(f => ({ ...f, customer_name: e.target.value }))} placeholder="Full name" />
                </div>
                <div className="field-grp">
                  <label>Email</label>
                  <input value={newOrderForm.customer_email} onChange={e => setNewOrderForm(f => ({ ...f, customer_email: e.target.value }))} placeholder="email@example.com" type="email" />
                </div>
                <div className="field-grp" style={{ gridColumn: '1 / -1' }}>
                  <label>Address Line 1</label>
                  <input value={newOrderForm.address_line1} onChange={e => setNewOrderForm(f => ({ ...f, address_line1: e.target.value }))} placeholder="123 Main Street" />
                </div>
                <div className="field-grp">
                  <label>City</label>
                  <input value={newOrderForm.city} onChange={e => setNewOrderForm(f => ({ ...f, city: e.target.value }))} placeholder="London" />
                </div>
                <div className="field-grp">
                  <label>Postcode</label>
                  <input value={newOrderForm.postal_code} onChange={e => setNewOrderForm(f => ({ ...f, postal_code: e.target.value }))} placeholder="SW1A 1AA" />
                </div>
              </div>
              <div style={{ fontWeight: 600, fontSize: 13, marginBottom: 8, color: 'var(--text-primary)' }}>Line Items</div>
              {newOrderForm.line_items.map((li, i) => (
                <div key={i} style={{ display: 'grid', gridTemplateColumns: '2fr 3fr 1fr 1.5fr auto', gap: 8, marginBottom: 8, alignItems: 'center' }}>
                  <input placeholder="SKU *" value={li.sku} onChange={e => setNewOrderForm(f => { const items = [...f.line_items]; items[i] = { ...items[i], sku: e.target.value }; return { ...f, line_items: items }; })} />
                  <input placeholder="Product name" value={li.title} onChange={e => setNewOrderForm(f => { const items = [...f.line_items]; items[i] = { ...items[i], title: e.target.value }; return { ...f, line_items: items }; })} />
                  <input type="number" placeholder="Qty" min={1} value={li.quantity} onChange={e => setNewOrderForm(f => { const items = [...f.line_items]; items[i] = { ...items[i], quantity: parseInt(e.target.value) || 1 }; return { ...f, line_items: items }; })} />
                  <input type="number" placeholder="Price" min={0} step={0.01} value={li.price} onChange={e => setNewOrderForm(f => { const items = [...f.line_items]; items[i] = { ...items[i], price: parseFloat(e.target.value) || 0 }; return { ...f, line_items: items }; })} />
                  <button className="btn-sm" style={{ color: '#ef4444', padding: '3px 6px' }} onClick={() => setNewOrderForm(f => ({ ...f, line_items: f.line_items.filter((_, idx) => idx !== i) }))} disabled={newOrderForm.line_items.length <= 1}>×</button>
                </div>
              ))}
              <button className="btn-sec" style={{ fontSize: 12, marginBottom: 12 }} onClick={() => setNewOrderForm(f => ({ ...f, line_items: [...f.line_items, { sku: '', title: '', quantity: 1, price: 0, currency: 'GBP' }] }))}>
                + Add Line
              </button>
              <div className="field-grp">
                <label>Notes</label>
                <textarea value={newOrderForm.notes} onChange={e => setNewOrderForm(f => ({ ...f, notes: e.target.value }))} rows={2} style={{ width: '100%', resize: 'vertical' }} />
              </div>
              <div className="modal-actions">
                <button className="btn-sec" onClick={() => setShowNewOrderModal(false)}>Cancel</button>
                <button className="btn-pri" onClick={submitNewOrder} disabled={savingNewOrder}>
                  {savingNewOrder ? 'Creating…' : 'Create Order'}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* ── B-003: Edit Order Modal ── */}
      {editOrder && editForm && (
        <div className="modal-bg" onClick={() => setEditOrder(null)}>
          <div className="modal" style={{ width: 700, maxHeight: '90vh', overflow: 'auto' }} onClick={e => e.stopPropagation()}>
            <div className="modal-head">
              <h2><Edit2 size={15} /> Edit Order — {getDisplayRef(editOrder)}</h2>
              <button className="btn-x" onClick={() => setEditOrder(null)}>×</button>
            </div>
            <div className="modal-body">
              {/* Customer & Address */}
              <div style={{ fontSize: 12, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.5px', marginBottom: 8 }}>Customer & Delivery Address</div>
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12, marginBottom: 20 }}>
                <div className="field-grp" style={{ gridColumn: '1 / -1' }}>
                  <label>Customer Name</label>
                  <input value={editForm.customer_name} onChange={e => setEditForm(f => f ? { ...f, customer_name: e.target.value } : f)} />
                </div>
                <div className="field-grp">
                  <label>Email</label>
                  <input value={editForm.customer_email} onChange={e => setEditForm(f => f ? { ...f, customer_email: e.target.value } : f)} type="email" />
                </div>
                <div className="field-grp">
                  <label>Phone</label>
                  <input value={editForm.customer_phone} onChange={e => setEditForm(f => f ? { ...f, customer_phone: e.target.value } : f)} />
                </div>
                <div className="field-grp" style={{ gridColumn: '1 / -1' }}>
                  <label>Address Line 1</label>
                  <input value={editForm.address_line1} onChange={e => setEditForm(f => f ? { ...f, address_line1: e.target.value } : f)} />
                </div>
                <div className="field-grp">
                  <label>City</label>
                  <input value={editForm.city} onChange={e => setEditForm(f => f ? { ...f, city: e.target.value } : f)} />
                </div>
                <div className="field-grp">
                  <label>Postcode</label>
                  <input value={editForm.postal_code} onChange={e => setEditForm(f => f ? { ...f, postal_code: e.target.value } : f)} />
                </div>
              </div>

              {/* Line Items */}
              <div style={{ fontSize: 12, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.5px', marginBottom: 8 }}>Line Items</div>
              <div style={{ border: '1px solid var(--border)', borderRadius: 8, overflow: 'hidden', marginBottom: 16 }}>
                <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
                  <thead>
                    <tr style={{ background: 'var(--bg-tertiary)' }}>
                      <th style={{ padding: '8px 10px', textAlign: 'left', fontWeight: 600, color: 'var(--text-muted)', fontSize: 11 }}>Item</th>
                      <th style={{ padding: '8px 10px', textAlign: 'left', fontWeight: 600, color: 'var(--text-muted)', fontSize: 11, width: 80 }}>SKU</th>
                      <th style={{ padding: '8px 10px', textAlign: 'center', fontWeight: 600, color: 'var(--text-muted)', fontSize: 11, width: 50 }}>Qty</th>
                      <th style={{ padding: '8px 10px', textAlign: 'right', fontWeight: 600, color: 'var(--text-muted)', fontSize: 11, width: 90 }}>Unit Price</th>
                      <th style={{ padding: '8px 10px', textAlign: 'right', fontWeight: 600, color: 'var(--text-muted)', fontSize: 11, width: 80 }}>VAT %</th>
                      <th style={{ padding: '8px 10px', textAlign: 'right', fontWeight: 600, color: 'var(--text-muted)', fontSize: 11, width: 80 }}>Line Total</th>
                      <th style={{ width: 36 }}></th>
                    </tr>
                  </thead>
                  <tbody>
                    {editForm.lines.map((line, idx) => {
                      const lineTotal = line.quantity * line.unit_price * (1 + line.tax_rate);
                      return (
                        <tr key={line.line_id} style={{ borderTop: '1px solid var(--border)' }}>
                          <td style={{ padding: '6px 10px' }}>
                            <input value={line.title} onChange={e => setEditForm(f => {
                              if (!f) return f;
                              const lines = [...f.lines]; lines[idx] = { ...lines[idx], title: e.target.value }; return { ...f, lines };
                            })} style={{ width: '100%', background: 'transparent', border: '1px solid var(--border)', borderRadius: 4, padding: '4px 6px', color: 'var(--text-primary)', fontSize: 12 }} />
                          </td>
                          <td style={{ padding: '6px 10px' }}>
                            <input value={line.sku} onChange={e => setEditForm(f => {
                              if (!f) return f;
                              const lines = [...f.lines]; lines[idx] = { ...lines[idx], sku: e.target.value }; return { ...f, lines };
                            })} style={{ width: '100%', background: 'transparent', border: '1px solid var(--border)', borderRadius: 4, padding: '4px 6px', color: 'var(--text-primary)', fontSize: 12 }} />
                          </td>
                          <td style={{ padding: '6px 10px', textAlign: 'center' }}>
                            <input type="number" min={1} value={line.quantity} onChange={e => setEditForm(f => {
                              if (!f) return f;
                              const lines = [...f.lines]; lines[idx] = { ...lines[idx], quantity: parseInt(e.target.value) || 1 }; return { ...f, lines };
                            })} style={{ width: 50, textAlign: 'center', background: 'transparent', border: '1px solid var(--border)', borderRadius: 4, padding: '4px 6px', color: 'var(--text-primary)', fontSize: 12 }} />
                          </td>
                          <td style={{ padding: '6px 10px', textAlign: 'right' }}>
                            <input type="number" step="0.01" value={line.unit_price} onChange={e => setEditForm(f => {
                              if (!f) return f;
                              const lines = [...f.lines]; lines[idx] = { ...lines[idx], unit_price: parseFloat(e.target.value) || 0 }; return { ...f, lines };
                            })} style={{ width: 80, textAlign: 'right', background: 'transparent', border: '1px solid var(--border)', borderRadius: 4, padding: '4px 6px', color: 'var(--text-primary)', fontSize: 12 }} />
                          </td>
                          <td style={{ padding: '6px 10px', textAlign: 'right' }}>
                            <select value={line.tax_rate} onChange={e => setEditForm(f => {
                              if (!f) return f;
                              const lines = [...f.lines]; lines[idx] = { ...lines[idx], tax_rate: parseFloat(e.target.value) }; return { ...f, lines };
                            })} style={{ width: 70, background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 4, padding: '4px 6px', color: 'var(--text-primary)', fontSize: 12 }}>
                              <option value={0}>0%</option>
                              <option value={0.05}>5%</option>
                              <option value={0.10}>10%</option>
                              <option value={0.125}>12.5%</option>
                              <option value={0.20}>20%</option>
                              <option value={0.23}>23%</option>
                            </select>
                          </td>
                          <td style={{ padding: '6px 10px', textAlign: 'right', fontWeight: 600, fontSize: 12, color: 'var(--text-primary)' }}>
                            {lineTotal.toFixed(2)}
                          </td>
                          <td style={{ padding: '6px 4px', textAlign: 'center' }}>
                            <button onClick={() => setEditForm(f => {
                              if (!f || f.lines.length <= 1) return f;
                              return { ...f, lines: f.lines.filter((_, i) => i !== idx) };
                            })} style={{ background: 'none', border: 'none', color: '#ef4444', cursor: 'pointer', fontSize: 14, padding: 2 }} title="Remove line">×</button>
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
                <div style={{ padding: '8px 10px', borderTop: '1px solid var(--border)', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                  <button onClick={() => setEditForm(f => {
                    if (!f) return f;
                    return { ...f, lines: [...f.lines, { line_id: `new-${Date.now()}`, title: '', sku: '', quantity: 1, unit_price: 0, tax_rate: 0.20, currency: f.lines[0]?.currency || 'GBP' }] };
                  })} style={{ fontSize: 12, color: 'var(--primary)', background: 'none', border: 'none', cursor: 'pointer', fontWeight: 600 }}>
                    + Add Line Item
                  </button>
                  <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                    Shipping: <input type="number" step="0.01" value={editForm.shipping_cost} onChange={e => setEditForm(f => f ? { ...f, shipping_cost: parseFloat(e.target.value) || 0 } : f)}
                      style={{ width: 70, textAlign: 'right', background: 'transparent', border: '1px solid var(--border)', borderRadius: 4, padding: '3px 6px', color: 'var(--text-primary)', fontSize: 12, marginLeft: 6 }} />
                  </div>
                </div>
              </div>

              {/* Notes */}
              <div className="field-grp">
                <label>Internal Notes</label>
                <textarea value={editForm.notes} onChange={e => setEditForm(f => f ? { ...f, notes: e.target.value } : f)} rows={2} style={{ width: '100%', resize: 'vertical' }} />
              </div>

              <div className="modal-actions">
                <button className="btn-sec" onClick={() => setEditOrder(null)}>Cancel</button>
                <button className="btn-pri" onClick={saveEditOrder} disabled={savingEdit}>
                  {savingEdit ? 'Saving…' : 'Save Changes'}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* ── B-004: Merge Modal ── */}
      {showMergeModal && (
        <div className="modal-bg" onClick={() => setShowMergeModal(false)}>
          <div className="modal" style={{ width: 480 }} onClick={e => e.stopPropagation()}>
            <div className="modal-head">
              <h2><GitMerge size={15} /> Merge Orders</h2>
              <button className="btn-x" onClick={() => setShowMergeModal(false)}>×</button>
            </div>
            <div className="modal-body">
              <p style={{ fontSize: 13, color: 'var(--text-secondary)', marginBottom: 16 }}>
                {selected.size} orders selected. All line items from secondary orders will be moved into the primary order. Secondary orders will be cancelled with reason "merged".
              </p>
              <div className="field-grp">
                <label>Primary Order (keep this one)</label>
                <select value={mergePrimaryId} onChange={e => setMergePrimaryId(e.target.value)} className="inline-sel wide">
                  {Array.from(selected).map(id => {
                    const o = orders.find(o => getOrderId(o) === id);
                    return <option key={id} value={id}>{getDisplayRef(o || {} as RawOrder)} — {o?.customer?.name || o?.shipping_address?.name || 'Unknown'}</option>;
                  })}
                </select>
              </div>
              <div className="modal-actions">
                <button className="btn-sec" onClick={() => setShowMergeModal(false)}>Cancel</button>
                <button className="btn-pri" onClick={doMerge} disabled={merging}>
                  {merging ? 'Merging…' : 'Confirm Merge'}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* ── B-005: Split Modal ── */}
      {splitOrder && (
        <div className="modal-bg" onClick={() => setSplitOrder(null)}>
          <div className="modal" style={{ width: 520 }} onClick={e => e.stopPropagation()}>
            <div className="modal-head">
              <h2><Scissors size={15} /> Split Order — {getDisplayRef(splitOrder)}</h2>
              <button className="btn-x" onClick={() => setSplitOrder(null)}>×</button>
            </div>
            <div className="modal-body">
              <p style={{ fontSize: 13, color: 'var(--text-secondary)', marginBottom: 12 }}>
                Set a quantity for each line item you want to move into a new separate order.
              </p>
              {splitLines.length === 0 ? (
                <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>Loading line items…</p>
              ) : splitLines.map((l, i) => (
                <div key={l.line_id || i} style={{ display: 'flex', alignItems: 'center', gap: 12, padding: '8px 0', borderBottom: '1px solid var(--border)' }}>
                  <div style={{ flex: 1 }}>
                    <div style={{ fontSize: 13, color: 'var(--text-primary)' }}>{l.title}</div>
                    {l.sku && <div style={{ fontSize: 11, color: 'var(--text-muted)' }}>{l.sku}</div>}
                  </div>
                  <span style={{ fontSize: 12, color: 'var(--text-muted)', minWidth: 60 }}>Total: {l.qty_total}</span>
                  <div>
                    <label style={{ fontSize: 11, color: 'var(--text-muted)', display: 'block', marginBottom: 2 }}>Split qty</label>
                    <input
                      type="number"
                      min={0}
                      max={l.qty_total}
                      value={l.qty_split}
                      onChange={e => setSplitLines(lines => lines.map((ln, idx) => idx === i ? { ...ln, qty_split: Math.min(l.qty_total, Math.max(0, parseInt(e.target.value) || 0)) } : ln))}
                      style={{ width: 70, textAlign: 'center' }}
                    />
                  </div>
                </div>
              ))}
              <div className="modal-actions" style={{ marginTop: 16 }}>
                <button className="btn-sec" onClick={() => setSplitOrder(null)}>Cancel</button>
                <button className="btn-pri" onClick={doSplit} disabled={splitting || splitLines.every(l => l.qty_split === 0)}>
                  {splitting ? 'Splitting…' : 'Split Order'}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* ── B-006: Cancel Modal ── */}
      {cancelOrder && (
        <div className="modal-bg" onClick={() => setCancelOrder(null)}>
          <div className="modal" style={{ width: 420 }} onClick={e => e.stopPropagation()}>
            <div className="modal-head">
              <h2><XCircle size={15} /> Cancel Order — {getDisplayRef(cancelOrder)}</h2>
              <button className="btn-x" onClick={() => setCancelOrder(null)}>×</button>
            </div>
            <div className="modal-body">
              <div className="field-grp">
                <label>Reason *</label>
                <select value={cancelReason} onChange={e => setCancelReason(e.target.value)} className="inline-sel wide">
                  <option value="">Select a reason…</option>
                  <option value="customer_request">Customer Request</option>
                  <option value="out_of_stock">Out of Stock</option>
                  <option value="fraud">Fraud</option>
                  <option value="duplicate">Duplicate Order</option>
                  <option value="other">Other</option>
                </select>
              </div>
              <div className="field-grp">
                <label>Notes (optional)</label>
                <textarea value={cancelNotes} onChange={e => setCancelNotes(e.target.value)} rows={2} style={{ width: '100%', resize: 'vertical' }} placeholder="Additional details…" />
              </div>
              <div className="modal-actions">
                <button className="btn-sec" onClick={() => setCancelOrder(null)}>Back</button>
                <button
                  className="btn-pri"
                  style={{ background: '#ef4444', borderColor: '#ef4444' }}
                  onClick={doCancel}
                  disabled={cancelling || !cancelReason}
                >
                  {cancelling ? 'Cancelling…' : 'Cancel Order'}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* ── B-007: Save View Modal ── */}
      {showSaveViewModal && (
        <div className="modal-bg" onClick={() => setShowSaveViewModal(false)}>
          <div className="modal" style={{ width: 380 }} onClick={e => e.stopPropagation()}>
            <div className="modal-head">
              <h2><Save size={15} /> Save Current View</h2>
              <button className="btn-x" onClick={() => setShowSaveViewModal(false)}>×</button>
            </div>
            <div className="modal-body">
              <p style={{ fontSize: 13, color: 'var(--text-secondary)', marginBottom: 12 }}>
                Saves current filters (status: {statusFilter || 'all'}, channel: {channelFilter || 'all'}) as a named view.
              </p>
              <div className="field-grp">
                <label>View Name *</label>
                <input value={newViewName} onChange={e => setNewViewName(e.target.value)} placeholder="e.g. Amazon Unshipped" autoFocus />
              </div>
              <div className="modal-actions">
                <button className="btn-sec" onClick={() => setShowSaveViewModal(false)}>Cancel</button>
                <button className="btn-pri" onClick={saveCurrentView} disabled={savingView || !newViewName.trim()}>
                  {savingView ? 'Saving…' : 'Save View'}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* ── Bulk Status Modal (from toolbar button) ── */}
      {showBulkStatusModal && (
        <div className="modal-bg" onClick={() => setShowBulkStatusModal(false)}>
          <div className="modal" style={{ width: 420 }} onClick={e => e.stopPropagation()}>
            <div className="modal-head">
              <h2>✏️ Change Status — {selected.size} order{selected.size !== 1 ? 's' : ''}</h2>
              <button className="btn-x" onClick={() => setShowBulkStatusModal(false)}>×</button>
            </div>
            <div className="modal-body">
              <p style={{ fontSize: 13, color: 'var(--text-secondary)', marginBottom: 16 }}>
                Apply a new status to all {selected.size} selected order{selected.size !== 1 ? 's' : ''} at once.
              </p>
              <div className="field-grp">
                <label>New Status *</label>
                <select value={bulkStatus} onChange={e => setBulkStatus(e.target.value)} className="inline-sel wide">
                  <option value="">Select a status…</option>
                  {ORDER_STATUSES.map(s => (
                    <option key={s.value} value={s.value}>{s.label}</option>
                  ))}
                </select>
              </div>
              <div className="modal-actions">
                <button className="btn-sec" onClick={() => setShowBulkStatusModal(false)}>Cancel</button>
                <button className="btn-pri" onClick={applyBulkStatus} disabled={applyingBulkStatus || !bulkStatus}>
                  {applyingBulkStatus ? 'Applying…' : 'Apply to All'}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* ── S4 Bulk Ship Modal ── */}
      {showBulkShipModal && (
        <BulkShipModal
          channel={bulkShipChannel}
          credentialId={bulkShipCredentialId}
          credentials={bulkShipCredentials}
          rows={bulkShipRows}
          results={bulkShipResults}
          submitting={submittingBulkShip}
          onClose={() => setShowBulkShipModal(false)}
          onCredentialChange={setBulkShipCredentialId}
          onRowChange={(orderId, field, value) => setBulkShipRows(prev => ({ ...prev, [orderId]: { ...prev[orderId], [field]: value } }))}
          onSubmit={submitBulkShip}
          onDone={() => { setShowBulkShipModal(false); setSelected(new Set()); }}
        />
      )}

      {/* ── S2-Task9: Import Job Progress Banner ── */}
      {importJobId && importJobStatus && (
        <div style={{
          position: 'fixed', bottom: 24, right: 24, zIndex: 9000, width: 340,
          background: 'var(--bg-elevated)', border: '1px solid var(--border)',
          borderRadius: 12, padding: '16px 20px', boxShadow: '0 8px 32px rgba(0,0,0,0.2)',
        }}>
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 10 }}>
            <div style={{ fontWeight: 700, fontSize: 14 }}>
              {importJobStatus.status === 'complete' || importJobStatus.status === 'done' ? '✅ Import Complete' :
               importJobStatus.status === 'failed' ? '❌ Import Failed' : '⏳ Importing Orders…'}
            </div>
            {(importJobStatus.status === 'complete' || importJobStatus.status === 'done' || importJobStatus.status === 'failed') && (
              <button onClick={() => { setImportJobId(null); setImportJobStatus(null); }}
                style={{ background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-muted)', fontSize: 16 }}>×</button>
            )}
          </div>
          {importJobStatus.total > 0 && (
            <div style={{ marginBottom: 8 }}>
              <div style={{ height: 6, borderRadius: 3, background: 'var(--border)', overflow: 'hidden' }}>
                <div style={{
                  height: '100%', borderRadius: 3, transition: 'width 0.4s',
                  background: importJobStatus.status === 'failed' ? '#ef4444' : '#22c55e',
                  width: `${Math.min(100, Math.round(((importJobStatus.created + importJobStatus.failed) / importJobStatus.total) * 100))}%`,
                }} />
              </div>
              <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 5, display: 'flex', gap: 12 }}>
                <span>✓ {importJobStatus.created} created</span>
                {importJobStatus.failed > 0 && <span style={{ color: '#ef4444' }}>✗ {importJobStatus.failed} failed</span>}
                <span style={{ marginLeft: 'auto' }}>{importJobStatus.total} total</span>
              </div>
            </div>
          )}
          {importJobStatus.errors && importJobStatus.errors.length > 0 && (
            <div style={{ fontSize: 12, color: '#ef4444', marginTop: 6, maxHeight: 80, overflowY: 'auto' }}>
              {importJobStatus.errors.slice(0, 3).map((e, i) => <div key={i}>{e}</div>)}
              {importJobStatus.errors.length > 3 && <div>…and {importJobStatus.errors.length - 3} more</div>}
            </div>
          )}
        </div>
      )}

      {/* ── B-009: Keyboard shortcut reference ── */}
      <div style={{ fontSize: 11, color: 'var(--text-muted)', padding: '4px 0', marginTop: 4, textAlign: 'right' }}>
        Shortcuts: <kbd style={{ background: 'var(--bg-elevated)', padding: '1px 5px', borderRadius: 3, border: '1px solid var(--border)', marginRight: 4 }}>Shift+A</kbd> Select all
        <kbd style={{ background: 'var(--bg-elevated)', padding: '1px 5px', borderRadius: 3, border: '1px solid var(--border)', margin: '0 4px' }}>Shift+D</kbd> Deselect
        <kbd style={{ background: 'var(--bg-elevated)', padding: '1px 5px', borderRadius: 3, border: '1px solid var(--border)', margin: '0 4px' }}>Shift+K</kbd> Picklist
        <kbd style={{ background: 'var(--bg-elevated)', padding: '1px 5px', borderRadius: 3, border: '1px solid var(--border)', margin: '0 4px' }}>Shift+P</kbd> Packing slip
        <kbd style={{ background: 'var(--bg-elevated)', padding: '1px 5px', borderRadius: 3, border: '1px solid var(--border)', margin: '0 4px' }}>Shift+M</kbd> Merge
        <kbd style={{ background: 'var(--bg-elevated)', padding: '1px 5px', borderRadius: 3, border: '1px solid var(--border)', margin: '0 4px' }}>Shift+H</kbd> Hold
        <kbd style={{ background: 'var(--bg-elevated)', padding: '1px 5px', borderRadius: 3, border: '1px solid var(--border)', margin: '0 4px' }}>Shift+T</kbd> Tag
        <kbd style={{ background: 'var(--bg-elevated)', padding: '1px 5px', borderRadius: 3, border: '1px solid var(--border)', margin: '0 4px' }}>Shift+I</kbd> Invoice
        <kbd style={{ background: 'var(--bg-elevated)', padding: '1px 5px', borderRadius: 3, border: '1px solid var(--border)', margin: '0 4px' }}>Shift+F</kbd> Filters
        <kbd style={{ background: 'var(--bg-elevated)', padding: '1px 5px', borderRadius: 3, border: '1px solid var(--border)', margin: '0 4px' }}>Esc</kbd> Deselect/Close
      </div>

      {/* ── Task 6: Tag Modal ── */}
      {showTagModal && (
        <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.7)', zIndex: 300, display: 'flex', alignItems: 'center', justifyContent: 'center' }}
          onClick={() => setShowTagModal(false)}>
          <div style={{ background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 12, padding: 24, minWidth: 320, maxWidth: 400 }}
            onClick={e => e.stopPropagation()}>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 16 }}>
              <h3 style={{ margin: 0, fontSize: 16, fontWeight: 700, color: 'var(--text-primary)' }}>
                Apply Tag to {tagModalOrderIds.length} Order{tagModalOrderIds.length !== 1 ? 's' : ''}
              </h3>
              <button onClick={() => setShowTagModal(false)} style={{ background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-muted)' }}><X size={18} /></button>
            </div>
            {tagDefinitions.length === 0 ? (
              <p style={{ color: 'var(--text-muted)', fontSize: 13 }}>No tags defined yet. Create tags in Settings → Order Tags.</p>
            ) : (
              <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                {tagDefinitions.map(tag => (
                  <button key={tag.tag_id}
                    onClick={() => applyTagToOrders(tag.tag_id, tagModalOrderIds)}
                    style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '10px 14px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 8, cursor: 'pointer', color: 'var(--text-primary)', fontSize: 13, textAlign: 'left', transition: 'background 0.12s' }}>
                    <TagShape shape={tag.shape} color={tag.color} size={14} />
                    <span style={{ fontWeight: 500 }}>{tag.name}</span>
                  </button>
                ))}
              </div>
            )}
            <div style={{ marginTop: 16, textAlign: 'right' }}>
              <a href="/settings/order-tags" style={{ fontSize: 12, color: 'var(--primary)', textDecoration: 'none' }}>Manage tags →</a>
            </div>
          </div>
        </div>
      )}

      {/* ══════════════════════════════════════════════════════════════════════
          ACTIONS MENU MODALS
          ══════════════════════════════════════════════════════════════════ */}

      {/* ── Folder Modal ── */}
      {showFolderModal && (
        <div className="modal-bg" onClick={() => setShowFolderModal(false)}>
          <div className="modal" style={{ width: 440 }} onClick={e => e.stopPropagation()}>
            <div className="modal-head">
              <h2>📁 Assign Folder — {folderModalOrderIds.length} order{folderModalOrderIds.length !== 1 ? 's' : ''}</h2>
              <button className="btn-x" onClick={() => setShowFolderModal(false)}>×</button>
            </div>
            <div className="modal-body">
              <p style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 0, marginBottom: 14, lineHeight: 1.5, background: 'rgba(59,130,246,0.06)', borderRadius: 6, padding: '8px 10px', border: '1px solid rgba(59,130,246,0.15)' }}>
                💡 Folders group orders for bulk processing or staff routing (e.g. "Urgent", "Pre-Christmas", "Gift Wrap"). After assigning, use the <strong>Folder filter</strong> at the top of the Orders screen to view only those orders.
              </p>
              {folders.length > 0 && (
                <div style={{ marginBottom: 16 }}>
                  <div style={{ fontSize: 11, fontWeight: 700, color: '#4b5f7c', textTransform: 'uppercase', letterSpacing: '0.6px', marginBottom: 8 }}>Existing Folders</div>
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                    {folders.map(f => (
                      <button key={f.folder_id}
                        onClick={() => assignFolder(f.folder_id, f.name)}
                        style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '9px 12px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 6, cursor: 'pointer', color: 'var(--text-primary)', fontSize: 13, textAlign: 'left' }}>
                        <span style={{ width: 10, height: 10, borderRadius: 2, background: f.color || '#3b82f6', flexShrink: 0 }} />
                        {f.name}
                      </button>
                    ))}
                  </div>
                </div>
              )}
              <div style={{ borderTop: folders.length > 0 ? '1px solid var(--border)' : 'none', paddingTop: folders.length > 0 ? 14 : 0 }}>
                <div style={{ fontSize: 11, fontWeight: 700, color: '#4b5f7c', textTransform: 'uppercase', letterSpacing: '0.6px', marginBottom: 8 }}>Create New Folder</div>
                <div style={{ display: 'flex', gap: 8 }}>
                  <input value={newFolderName} onChange={e => setNewFolderName(e.target.value)}
                    placeholder="Folder name…" style={{ flex: 1 }}
                    onKeyDown={e => { if (e.key === 'Enter') createAndAssignFolder(); }} />
                  <button className="btn-pri" onClick={createAndAssignFolder} disabled={creatingFolder || !newFolderName.trim()}>
                    {creatingFolder ? 'Creating…' : 'Create & Assign'}
                  </button>
                </div>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* ── Identifier Modal ── */}
      {showIdentifierModal && (
        <div className="modal-bg" onClick={() => setShowIdentifierModal(false)}>
          <div className="modal" style={{ width: 380 }} onClick={e => e.stopPropagation()}>
            <div className="modal-head">
              <h2># Assign Identifier — {identifierModalOrderIds.length} order{identifierModalOrderIds.length !== 1 ? 's' : ''}</h2>
              <button className="btn-x" onClick={() => setShowIdentifierModal(false)}>×</button>
            </div>
            <div className="modal-body">
              <p style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 0, marginBottom: 14, lineHeight: 1.5, background: 'rgba(107,114,128,0.06)', borderRadius: 6, padding: '8px 10px', border: '1px solid rgba(107,114,128,0.15)' }}>
                An identifier is a free-text reference code applied to this order (e.g. BATCH-001, PRIORITY, GIFT). Unlike tags, identifiers are not predefined — type any value. Use the search bar to find orders by identifier later.
              </p>
              <div className="field-grp">
                <label>Identifier Value *</label>
                <input value={identifierValue} onChange={e => setIdentifierValue(e.target.value)}
                  placeholder="e.g. BATCH-001, SPECIAL, PRIORITY"
                  onKeyDown={e => { if (e.key === 'Enter') saveIdentifier(); }} />
              </div>
              <div className="modal-actions">
                <button className="btn-sec" onClick={() => setShowIdentifierModal(false)}>Cancel</button>
                <button className="btn-pri" onClick={saveIdentifier} disabled={!identifierValue.trim()}>Apply</button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* ── Location / Fulfilment Center Modal ── */}
      {showLocationModal && (
        <div className="modal-bg" onClick={() => setShowLocationModal(false)}>
          <div className="modal" style={{ width: 420 }} onClick={e => e.stopPropagation()}>
            <div className="modal-head">
              <h2>{locationModalMode === 'location' ? '📍 Move to Location' : '🏢 Move to Fulfilment Centre'} — {locationModalOrderIds.length} order{locationModalOrderIds.length !== 1 ? 's' : ''}</h2>
              <button className="btn-x" onClick={() => setShowLocationModal(false)}>×</button>
            </div>
            <div className="modal-body">
              {locationModalMode === 'location' ? (
                <>
                  <p style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 0, marginBottom: 14, lineHeight: 1.5, background: 'rgba(59,130,246,0.06)', borderRadius: 6, padding: '8px 10px', border: '1px solid rgba(59,130,246,0.15)' }}>
                    Sets the specific warehouse bin or zone where this order's stock will be picked from. Overrides the default pick location — useful when stock has been pre-staged or relocated.
                  </p>
                  <div className="field-grp">
                    <label>Warehouse Location *</label>
                    {warehouseLocations.length > 0 ? (
                      <select value={locationValue} onChange={e => {
                        const loc = warehouseLocations.find(l => l.id === e.target.value);
                        setLocationValue(e.target.value);
                        setLocationName(loc?.name || '');
                      }} style={{ width: '100%' }}>
                        <option value="">Select a location…</option>
                        {warehouseLocations.map(l => (
                          <option key={l.id} value={l.id}>{l.name}{l.code ? ` (${l.code})` : ''}</option>
                        ))}
                      </select>
                    ) : (
                      <input value={locationValue} onChange={e => setLocationValue(e.target.value)}
                        placeholder="e.g. WH-A1, ZONE-B" />
                    )}
                  </div>
                  {warehouseLocations.length === 0 && (
                    <div className="field-grp">
                      <label>Name (optional)</label>
                      <input value={locationName} onChange={e => setLocationName(e.target.value)}
                        placeholder="e.g. Warehouse A, Aisle 1" />
                    </div>
                  )}
                </>
              ) : (
                <>
                  <p style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 0, marginBottom: 14, lineHeight: 1.5, background: 'rgba(168,85,247,0.06)', borderRadius: 6, padding: '8px 10px', border: '1px solid rgba(168,85,247,0.2)' }}>
                    Reassigns which fulfilment source (warehouse, 3PL, FBA) will process this order. This is the manual equivalent of what the Workflows engine does automatically — use when a specific order needs handling by a different centre.
                  </p>
                  <div className="field-grp">
                    <label>Fulfilment Centre *</label>
                    {fulfilmentSources.length > 0 ? (
                      <select value={locationValue} onChange={e => {
                        const src = fulfilmentSources.find(s => s.source_id === e.target.value);
                        setLocationValue(e.target.value);
                        setLocationName(src?.name || '');
                      }} style={{ width: '100%' }}>
                        <option value="">Select a fulfilment centre…</option>
                        {fulfilmentSources.map(s => (
                          <option key={s.source_id} value={s.source_id}>{s.name} — {s.type}</option>
                        ))}
                      </select>
                    ) : (
                      <input value={locationValue} onChange={e => setLocationValue(e.target.value)}
                        placeholder="e.g. FC-LONDON, FC-US-EAST" />
                    )}
                  </div>
                  {fulfilmentSources.length === 0 && (
                    <div className="field-grp">
                      <label>Name (optional)</label>
                      <input value={locationName} onChange={e => setLocationName(e.target.value)}
                        placeholder="e.g. London FC" />
                    </div>
                  )}
                </>
              )}
              <div className="modal-actions">
                <button className="btn-sec" onClick={() => setShowLocationModal(false)}>Cancel</button>
                <button className="btn-pri" onClick={saveLocation} disabled={!locationValue.trim()}>
                  {locationModalMode === 'location' ? 'Move' : 'Assign'}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* ── Batch Assignment Modal ── */}
      {showBatchModal && (
        <div className="modal-bg" onClick={() => setShowBatchModal(false)}>
          <div className="modal" style={{ width: 400 }} onClick={e => e.stopPropagation()}>
            <div className="modal-head">
              <h2>📦 Batch Assignment — {batchModalOrderIds.length} order{batchModalOrderIds.length !== 1 ? 's' : ''}</h2>
              <button className="btn-x" onClick={() => setShowBatchModal(false)}>×</button>
            </div>
            <div className="modal-body">
              <div className="field-grp">
                <label>Batch ID *</label>
                <input value={batchIdInput} onChange={e => setBatchIdInput(e.target.value)} placeholder="e.g. BATCH-2026-001" />
              </div>
              <div className="field-grp">
                <label>Batch Number (optional)</label>
                <input value={batchNumberInput} onChange={e => setBatchNumberInput(e.target.value)} placeholder="e.g. 001" />
              </div>
              <div className="modal-actions">
                <button className="btn-sec" onClick={() => setShowBatchModal(false)}>Cancel</button>
                <button className="btn-pri" onClick={saveBatchAssignment} disabled={processingBatchAction || !batchIdInput.trim()}>
                  {processingBatchAction ? 'Assigning…' : 'Assign Batch'}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* ── Change Shipping Service Modal ── */}
      {showChangeServiceModal && (
        <div className="modal-bg" onClick={() => setShowChangeServiceModal(false)}>
          <div className="modal" style={{ width: 420 }} onClick={e => e.stopPropagation()}>
            <div className="modal-head">
              <h2>🚚 Change Shipping Service — {changeServiceOrderIds.length} order{changeServiceOrderIds.length !== 1 ? 's' : ''}</h2>
              <button className="btn-x" onClick={() => setShowChangeServiceModal(false)}>×</button>
            </div>
            <div className="modal-body">
              <div className="field-grp">
                <label>Shipping Service *</label>
                <select value={newShippingService} onChange={e => setNewShippingService(e.target.value)} className="inline-sel wide">
                  <option value="">Select a service…</option>
                  {SHIPPING_OPTIONS.map(s => <option key={s} value={s}>{s}</option>)}
                  <option value="custom">Custom…</option>
                </select>
              </div>
              {newShippingService === 'custom' && (
                <div className="field-grp">
                  <label>Custom Service Name</label>
                  <input placeholder="e.g. FedEx Express" onChange={e => setNewShippingService(e.target.value)} autoFocus />
                </div>
              )}
              <div className="modal-actions">
                <button className="btn-sec" onClick={() => setShowChangeServiceModal(false)}>Cancel</button>
                <button className="btn-pri" onClick={saveChangeService} disabled={changingService || !newShippingService.trim()}>
                  {changingService ? 'Saving…' : 'Apply to All'}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* ── Shipping Quotes Modal ── */}
      {showQuotesModal && (
        <div className="modal-bg" onClick={() => setShowQuotesModal(false)}>
          <div className="modal" style={{ width: 560 }} onClick={e => e.stopPropagation()}>
            <div className="modal-head">
              <h2>💰 Shipping Quotes — {quotesOrderIds.length} order{quotesOrderIds.length !== 1 ? 's' : ''}</h2>
              <button className="btn-x" onClick={() => setShowQuotesModal(false)}>×</button>
            </div>
            <div className="modal-body">
              {loadingQuotes ? (
                <div style={{ textAlign: 'center', color: 'var(--text-muted)', padding: '20px 0' }}>
                  <span className="spinner" /> Fetching quotes…
                </div>
              ) : quotesResults.length === 0 ? (
                <p style={{ color: 'var(--text-muted)', fontSize: 13 }}>No quotes returned.</p>
              ) : (
                <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
                  {quotesResults.map(r => (
                    <div key={r.order_id}>
                      <div style={{ fontSize: 11, fontWeight: 700, color: '#4b5f7c', textTransform: 'uppercase', letterSpacing: '0.6px', marginBottom: 8 }}>
                        Order: {r.order_id}
                      </div>
                      {r.error ? (
                        <div style={{ color: '#ef4444', fontSize: 13 }}>{r.error}</div>
                      ) : (
                        <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                          {(r.quotes || []).map((q, qi) => (
                            <div key={qi} style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '8px 12px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 6 }}>
                              <span style={{ flex: 1, fontSize: 13, color: 'var(--text-primary)', fontWeight: 500 }}>{q.service}</span>
                              <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>{q.transit_days}d</span>
                              <span style={{ fontSize: 14, fontWeight: 700, color: '#22c55e' }}>
                                {new Intl.NumberFormat('en-GB', { style: 'currency', currency: q.currency || 'GBP' }).format(q.price)}
                              </span>
                              <button className="btn-pri" style={{ fontSize: 11, padding: '4px 10px' }}
                                onClick={() => {
                                  setShowQuotesModal(false);
                                  setChangeServiceOrderIds(quotesOrderIds);
                                  setNewShippingService(q.service);
                                  setShowChangeServiceModal(true);
                                }}>
                                Select
                              </button>
                            </div>
                          ))}
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              )}
              <div className="modal-actions">
                <button className="btn-sec" onClick={() => setShowQuotesModal(false)}>Close</button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* ── Split Packaging Modal ── */}
      {showSplitPackagingModal && (
        <div className="modal-bg" onClick={() => setShowSplitPackagingModal(false)}>
          <div className="modal" style={{ width: 460 }} onClick={e => e.stopPropagation()}>
            <div className="modal-head">
              <h2>📦 Split Packaging — {splitPackagingOrderId}</h2>
              <button className="btn-x" onClick={() => setShowSplitPackagingModal(false)}>×</button>
            </div>
            <div className="modal-body">
              <p style={{ color: 'var(--text-muted)', fontSize: 13, marginBottom: 16 }}>
                Use the main Despatch Console to define per-line package assignments. This will take you there.
              </p>
              <div style={{ display: 'flex', gap: 8, padding: '12px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 8, fontSize: 13, color: 'var(--text-secondary)' }}>
                <span>Order ID: <strong style={{ color: 'var(--text-primary)' }}>{splitPackagingOrderId}</strong></span>
              </div>
              <div className="modal-actions">
                <button className="btn-sec" onClick={() => setShowSplitPackagingModal(false)}>Cancel</button>
                <button className="btn-pri" onClick={() => {
                  setShowSplitPackagingModal(false);
                  window.location.href = `/despatch?order=${splitPackagingOrderId}`;
                }}>
                  Open Despatch Console →
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* ── Change Dispatch Date Modal ── */}
      {showDispatchDateModal && (
        <div className="modal-bg" onClick={() => setShowDispatchDateModal(false)}>
          <div className="modal" style={{ width: 380 }} onClick={e => e.stopPropagation()}>
            <div className="modal-head">
              <h2>📅 Change Dispatch Date — {dispatchDateOrderIds.length} order{dispatchDateOrderIds.length !== 1 ? 's' : ''}</h2>
              <button className="btn-x" onClick={() => setShowDispatchDateModal(false)}>×</button>
            </div>
            <div className="modal-body">
              <div className="field-grp">
                <label>Dispatch Date *</label>
                <input type="date" value={dispatchDateValue} onChange={e => setDispatchDateValue(e.target.value)}
                  style={{ fontSize: 13, padding: '7px 10px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', width: '100%' }} />
              </div>
              <div className="modal-actions">
                <button className="btn-sec" onClick={() => setShowDispatchDateModal(false)}>Cancel</button>
                <button className="btn-pri" onClick={saveDispatchDate} disabled={!dispatchDateValue}>Apply</button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* ── Change Delivery Dates Modal ── */}
      {showDeliveryDatesModal && (
        <div className="modal-bg" onClick={() => setShowDeliveryDatesModal(false)}>
          <div className="modal" style={{ width: 420 }} onClick={e => e.stopPropagation()}>
            <div className="modal-head">
              <h2>🗓 Change Delivery Dates — {deliveryDatesOrderIds.length} order{deliveryDatesOrderIds.length !== 1 ? 's' : ''}</h2>
              <button className="btn-x" onClick={() => setShowDeliveryDatesModal(false)}>×</button>
            </div>
            <div className="modal-body">
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
                <div className="field-grp">
                  <label>Delivery From</label>
                  <input type="date" value={deliveryDateFrom} onChange={e => setDeliveryDateFrom(e.target.value)}
                    style={{ fontSize: 13, padding: '7px 10px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', width: '100%' }} />
                </div>
                <div className="field-grp">
                  <label>Delivery To</label>
                  <input type="date" value={deliveryDateTo} onChange={e => setDeliveryDateTo(e.target.value)}
                    style={{ fontSize: 13, padding: '7px 10px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', width: '100%' }} />
                </div>
              </div>
              <div className="modal-actions">
                <button className="btn-sec" onClick={() => setShowDeliveryDatesModal(false)}>Cancel</button>
                <button className="btn-pri" onClick={saveDeliveryDates} disabled={!deliveryDateFrom && !deliveryDateTo}>Apply</button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* ── Order Notes Modal ── */}
      {showNotesModal && (
        <div className="modal-bg" onClick={() => setShowNotesModal(false)}>
          <div className="modal" style={{ width: 520, maxHeight: '80vh', display: 'flex', flexDirection: 'column' }} onClick={e => e.stopPropagation()}>
            <div className="modal-head">
              <h2>📝 Order Notes — {notesModalOrderId}</h2>
              <button className="btn-x" onClick={() => setShowNotesModal(false)}>×</button>
            </div>
            <div className="modal-body" style={{ flex: 1, overflowY: 'auto', display: 'flex', flexDirection: 'column', gap: 12 }}>
              {/* Add note */}
              <div style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 8, padding: 12 }}>
                <div style={{ fontSize: 11, fontWeight: 700, color: '#4b5f7c', textTransform: 'uppercase', letterSpacing: '0.6px', marginBottom: 8 }}>Add Note</div>
                <textarea
                  value={newNoteContent}
                  onChange={e => setNewNoteContent(e.target.value)}
                  placeholder="Write a note…"
                  rows={3}
                  style={{ width: '100%', resize: 'vertical', boxSizing: 'border-box' }}
                />
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginTop: 8 }}>
                  <label style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 12, color: 'var(--text-muted)', cursor: 'pointer' }}>
                    <input type="checkbox" checked={newNoteInternal} onChange={e => setNewNoteInternal(e.target.checked)} />
                    Internal note
                  </label>
                  <button className="btn-pri" style={{ fontSize: 12, padding: '5px 12px' }} onClick={addNote} disabled={savingNote || !newNoteContent.trim()}>
                    {savingNote ? 'Saving…' : 'Add Note'}
                  </button>
                </div>
              </div>
              {/* Notes list */}
              {loadingNotes ? (
                <div style={{ textAlign: 'center', color: 'var(--text-muted)', padding: '12px 0' }}><span className="spinner" /> Loading notes…</div>
              ) : notesModalNotes.length === 0 ? (
                <p style={{ color: 'var(--text-muted)', fontSize: 13, textAlign: 'center' }}>No notes yet.</p>
              ) : (
                <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                  {notesModalNotes.map(note => (
                    <div key={note.note_id} style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 6, padding: '10px 12px', position: 'relative' }}>
                      <div style={{ fontSize: 12, color: 'var(--text-secondary)', marginBottom: 6, display: 'flex', alignItems: 'center', gap: 8 }}>
                        <span style={{ fontWeight: 600 }}>{note.created_by}</span>
                        <span style={{ color: 'var(--text-muted)' }}>{new Date(note.created_at).toLocaleString('en-GB')}</span>
                        {note.is_internal && <span style={{ fontSize: 10, background: 'rgba(245,158,11,0.15)', color: '#f59e0b', border: '1px solid rgba(245,158,11,0.3)', borderRadius: 3, padding: '1px 5px' }}>Internal</span>}
                        <button onClick={() => deleteNote(note.note_id)}
                          style={{ marginLeft: 'auto', background: 'none', border: 'none', cursor: 'pointer', color: '#ef4444', fontSize: 11, opacity: 0.7 }}
                          title="Delete note">✕</button>
                      </div>
                      <div style={{ fontSize: 13, color: 'var(--text-primary)', whiteSpace: 'pre-wrap' }}>{note.content}</div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </div>
        </div>
      )}

      {/* ── Order XML / Raw Data Modal ── */}
      {showXMLModal && (
        <div className="modal-bg" onClick={() => setShowXMLModal(false)}>
          <div className="modal" style={{ width: 700, maxHeight: '82vh', display: 'flex', flexDirection: 'column' }} onClick={e => e.stopPropagation()}>
            <div className="modal-head">
              <h2>🔍 Order Raw Data — {xmlModalOrderId}</h2>
              <button className="btn-x" onClick={() => setShowXMLModal(false)}>×</button>
            </div>
            <div className="modal-body" style={{ flex: 1, overflow: 'hidden', display: 'flex', flexDirection: 'column' }}>
              {loadingXML ? (
                <div style={{ textAlign: 'center', color: 'var(--text-muted)', padding: '20px 0' }}><span className="spinner" /> Loading…</div>
              ) : (
                <>
                  <p style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 10 }}>
                    Raw channel data stored for order <code style={{ color: '#60a5fa' }}>{xmlModalOrderId}</code>. Read-only.
                  </p>
                  <pre style={{
                    flex: 1, overflowY: 'auto', background: '#070c18', border: '1px solid #182035',
                    borderRadius: 6, padding: 14, fontSize: 11, lineHeight: 1.6, color: '#94a3b8',
                    fontFamily: "'Courier New', monospace", whiteSpace: 'pre-wrap', wordBreak: 'break-all',
                  }}>
                    {xmlModalContent}
                  </pre>
                  <div className="modal-actions">
                    <button className="btn-sec" onClick={() => {
                      navigator.clipboard.writeText(xmlModalContent).then(() => alert('Copied to clipboard.'));
                    }}>Copy JSON</button>
                    <button className="btn-sec" onClick={() => setShowXMLModal(false)}>Close</button>
                  </div>
                </>
              )}
            </div>
          </div>
        </div>
      )}

      {/* ── Delete Order Confirmation Modal ── */}
      {showDeleteOrderModal && (
        <div className="modal-bg" onClick={() => setShowDeleteOrderModal(false)}>
          <div className="modal" style={{ width: 400 }} onClick={e => e.stopPropagation()}>
            <div className="modal-head">
              <h2 style={{ color: '#ef4444' }}>🗑 Delete Order</h2>
              <button className="btn-x" onClick={() => setShowDeleteOrderModal(false)}>×</button>
            </div>
            <div className="modal-body">
              <p style={{ fontSize: 14, color: 'var(--text-secondary)', marginBottom: 16, lineHeight: 1.6 }}>
                Are you sure you want to <strong style={{ color: '#ef4444' }}>permanently delete</strong> order{' '}
                <code style={{ color: '#60a5fa' }}>{deleteOrderId}</code>?
                <br /><br />
                This action cannot be undone. All order lines, notes, and related data will be removed.
              </p>
              <div className="modal-actions">
                <button className="btn-sec" onClick={() => setShowDeleteOrderModal(false)}>Cancel</button>
                <button
                  className="btn-pri"
                  style={{ background: '#ef4444', borderColor: '#ef4444' }}
                  onClick={confirmDeleteOrder}
                  disabled={deletingOrder}
                >
                  {deletingOrder ? 'Deleting…' : 'Delete Permanently'}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

    </div>
  );
};

export default Orders;

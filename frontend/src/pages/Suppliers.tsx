import { useState, useEffect, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { getActiveTenantId } from '../contexts/TenantContext';
import './Suppliers.css';

const API_BASE = import.meta.env.VITE_API_URL ?? 'http://localhost:8080/api/v1';

function api(path: string, init?: RequestInit) {
  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      'X-Tenant-Id': getActiveTenantId(),
      ...init?.headers,
    },
  });
}

// ─── Types ─────────────────────────────────────────────────────────────────

interface SourceAddress {
  company_name?: string;
  address_line1?: string;
  address_line2?: string;
  city?: string;
  county?: string;
  postal_code?: string;
  country?: string;
  phone?: string;
  email?: string;
}

interface SupplierEmailConfig {
  to_addresses: string[];
  cc_addresses?: string[];
  subject_template: string;
  attach_csv: boolean;
  attach_pdf: boolean;
  body_template?: string;
}

interface SupplierFTPConfig {
  host: string;
  port: number;
  username: string;
  password_enc?: string;
  path: string;
  protocol: 'ftp' | 'sftp';
}

interface SupplierWebhookConfig {
  url: string;
  method: 'POST' | 'PUT';
  auth_type: 'none' | 'api_key' | 'basic' | 'bearer';
  auth_header?: string;
  secret_enc?: string;
  headers?: Record<string, string>;
  format: 'json' | 'xml';
}

interface SupplierBankDetails {
  account_name: string;
  account_number_enc?: string;
  sort_code?: string;
  iban?: string;
  bic?: string;
  bank_name?: string;
}

interface SupplierCSVTemplate {
  column_map: Record<string, string>;
  delimiter: string;
  has_header: boolean;
  date_format: string;
}

interface Supplier {
  supplier_id: string;
  tenant_id: string;
  name: string;
  code: string;
  active: boolean;
  contact_name?: string;
  email?: string;
  phone?: string;
  website?: string;
  address?: SourceAddress;
  lead_time_days?: number;
  order_method?: string;
  email_config?: SupplierEmailConfig;
  ftp_config?: SupplierFTPConfig;
  webhook_config?: SupplierWebhookConfig;
  csv_template?: SupplierCSVTemplate;
  currency: string;
  payment_terms_days: number;
  payment_method?: string;
  bank_details?: SupplierBankDetails;
  vat_number?: string;
  company_reg_number?: string;
  credit_limit?: number;
  min_order_value?: number;
  notes?: string;
  tags?: string[];
  created_at: string;
  updated_at: string;
}

interface ProductSupplierRow {
  product_id: string;
  product_title: string;
  sku: string;
  supplier_sku: string;
  unit_cost: number;
  currency: string;
  lead_time_days: number;
  priority: number;
  is_default: boolean;
}

const CSV_FIELDS = [
  { key: 'supplier_sku', label: 'Supplier SKU' },
  { key: 'internal_sku', label: 'Internal SKU' },
  { key: 'qty_ordered',  label: 'Quantity' },
  { key: 'unit_cost',    label: 'Unit Cost' },
  { key: 'description',  label: 'Description' },
  { key: 'po_number',    label: 'PO Number' },
  { key: 'order_date',   label: 'Order Date' },
  { key: 'expected_date',label: 'Expected Date' },
];

const CURRENCIES = [
  'GBP','USD','EUR','AUD','CAD','JPY','CNY','CHF','SEK','NOK','DKK','NZD','SGD','HKD',
];

const PAYMENT_METHODS = [
  { value: 'bank_transfer', label: 'Bank Transfer' },
  { value: 'card',          label: 'Card' },
  { value: 'paypal',        label: 'PayPal' },
  { value: 'credit',        label: 'Credit Account' },
  { value: 'cheque',        label: 'Cheque' },
];

function blankSupplier(): Partial<Supplier> {
  return {
    name: '', code: '', active: true, currency: 'GBP', payment_terms_days: 30,
    order_method: 'email', lead_time_days: 3, tags: [],
    email_config: { to_addresses: [], subject_template: 'PO-{po_number} from MarketMate', attach_csv: true, attach_pdf: false },
    webhook_config: { url: '', method: 'POST', auth_type: 'none', format: 'json', headers: {} },
    ftp_config: { host: '', port: 21, username: '', path: '/', protocol: 'ftp' },
    csv_template: {
      column_map: Object.fromEntries(CSV_FIELDS.map(f => [f.key, f.label])),
      delimiter: ',', has_header: true, date_format: 'DD/MM/YYYY',
    },
    address: { address_line1: '', city: '', postal_code: '', country: 'GB' },
  };
}

function fmt(date?: string) {
  if (!date) return '—';
  try { return new Date(date).toLocaleDateString('en-GB', { day: '2-digit', month: 'short', year: 'numeric' }); }
  catch { return date; }
}

function methodBadge(method?: string) {
  switch (method) {
    case 'email':   return <span className="sup-badge sup-badge-email">✉ Email</span>;
    case 'webhook': return <span className="sup-badge sup-badge-webhook">⚡ Webhook</span>;
    case 'ftp':     return <span className="sup-badge sup-badge-ftp">📁 FTP</span>;
    case 'sftp':    return <span className="sup-badge sup-badge-ftp">📁 SFTP</span>;
    default:        return <span className="sup-badge sup-badge-manual">📋 Manual</span>;
  }
}

// ─── MaskedInput ─────────────────────────────────────────────────────────────

function MaskedInput({ value, onChange, placeholder }: { value: string; onChange: (v: string) => void; placeholder?: string }) {
  const [revealed, setRevealed] = useState(false);
  const isMasked = value === '••••••••';
  return (
    <div className="sup-masked-wrap">
      <input
        className="sup-input"
        type={revealed ? 'text' : 'password'}
        value={value}
        onChange={e => onChange(e.target.value)}
        placeholder={placeholder}
        readOnly={isMasked}
        onClick={() => { if (isMasked) onChange(''); }}
      />
      <button type="button" className="sup-reveal-btn" onClick={() => setRevealed(r => !r)}>
        {revealed ? '🙈' : '👁'}
      </button>
    </div>
  );
}

// ─── TagsInput ───────────────────────────────────────────────────────────────

function TagsInput({ tags, onChange }: { tags: string[]; onChange: (t: string[]) => void }) {
  const [input, setInput] = useState('');
  function add() {
    const v = input.trim();
    if (v && !tags.includes(v)) onChange([...tags, v]);
    setInput('');
  }
  return (
    <div className="sup-tags-wrap">
      {tags.map(t => (
        <span key={t} className="sup-tag">
          {t}<button type="button" onClick={() => onChange(tags.filter(x => x !== t))}>×</button>
        </span>
      ))}
      <input
        className="sup-tag-input" value={input} onChange={e => setInput(e.target.value)}
        onKeyDown={e => { if (e.key === 'Enter' || e.key === ',') { e.preventDefault(); add(); } }}
        placeholder="Add tag…"
      />
    </div>
  );
}

// ─── Main ────────────────────────────────────────────────────────────────────

export default function Suppliers() {
  const navigate = useNavigate();
  const [suppliers, setSuppliers] = useState<Supplier[]>([]);
  const [loading, setLoading] = useState(true);
  const [listError, setListError] = useState('');
  const [searchQ, setSearchQ] = useState('');
  const [showInactive, setShowInactive] = useState(false);

  const [mode, setMode] = useState<'list' | 'new' | 'edit'>('list');
  const [form, setForm] = useState<Partial<Supplier>>(blankSupplier());
  const [activeTab, setActiveTab] = useState<'general' | 'ordering' | 'financial' | 'products'>('general');
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState('');
  const [testResult, setTestResult] = useState<{ status: 'ok' | 'error' | 'testing'; message: string } | null>(null);
  const [testing, setTesting] = useState(false);
  const [products, setProducts] = useState<ProductSupplierRow[]>([]);
  const [productsLoading, setProductsLoading] = useState(false);
  const [headerRows, setHeaderRows] = useState<{ key: string; value: string }[]>([]);

  const load = useCallback(async () => {
    setLoading(true); setListError('');
    try {
      const res = await api('/suppliers');
      if (!res.ok) throw new Error('Failed to load suppliers');
      const data = await res.json();
      setSuppliers(data.suppliers || []);
    } catch (e: any) { setListError(e.message); }
    finally { setLoading(false); }
  }, []);

  useEffect(() => { load(); }, [load]);

  useEffect(() => {
    const h = form.webhook_config?.headers ?? {};
    setHeaderRows(Object.entries(h).map(([key, value]) => ({ key, value })));
  }, [form.webhook_config?.headers]);

  function openNew() {
    setForm(blankSupplier()); setActiveTab('general');
    setSaveError(''); setTestResult(null); setMode('new');
  }

  function openEdit(sup: Supplier) {
    setForm({ ...sup }); setActiveTab('general');
    setSaveError(''); setTestResult(null); setMode('edit');
    if (sup.supplier_id) loadProducts(sup.supplier_id);
  }

  async function loadProducts(supplierID: string) {
    setProductsLoading(true); setProducts([]);
    try {
      const res = await api(`/products?supplier_id=${supplierID}&limit=100`);
      if (!res.ok) return;
      const data = await res.json();
      const rows: ProductSupplierRow[] = [];
      for (const p of data.products || []) {
        for (const s of p.suppliers || []) {
          if (s.supplier_id === supplierID) {
            rows.push({ product_id: p.product_id, product_title: p.title, sku: p.sku,
              supplier_sku: s.supplier_sku, unit_cost: s.unit_cost,
              currency: s.currency || 'GBP', lead_time_days: s.lead_time_days,
              priority: s.priority, is_default: s.is_default });
          }
        }
      }
      setProducts(rows);
    } catch { /* silent */ }
    finally { setProductsLoading(false); }
  }

  function setF(patch: Partial<Supplier>) { setForm(f => ({ ...f, ...patch })); }

  function setEmailCfg(patch: Partial<SupplierEmailConfig>) {
    setForm(f => ({ ...f, email_config: { ...(f.email_config ?? { to_addresses: [], subject_template: '', attach_csv: false, attach_pdf: false }), ...patch } }));
  }
  function setFTPCfg(patch: Partial<SupplierFTPConfig>) {
    setForm(f => ({ ...f, ftp_config: { ...(f.ftp_config ?? { host: '', port: 21, username: '', path: '/', protocol: 'ftp' }), ...patch } }));
  }
  function setWebhookCfg(patch: Partial<SupplierWebhookConfig>) {
    setForm(f => ({ ...f, webhook_config: { ...(f.webhook_config ?? { url: '', method: 'POST', auth_type: 'none', format: 'json' }), ...patch } }));
  }
  function setBankDetails(patch: Partial<SupplierBankDetails>) {
    setForm(f => ({ ...f, bank_details: { ...(f.bank_details ?? { account_name: '' }), ...patch } }));
  }
  function setCSVCfg(patch: Partial<SupplierCSVTemplate>) {
    setForm(f => ({ ...f, csv_template: { ...(f.csv_template ?? { column_map: {}, delimiter: ',', has_header: true, date_format: 'DD/MM/YYYY' }), ...patch } }));
  }
  function setAddr(patch: Partial<SourceAddress>) {
    setForm(f => ({ ...f, address: { ...(f.address ?? {}), ...patch } }));
  }

  function syncHeaderRows(rows: { key: string; value: string }[]) {
    setHeaderRows(rows);
    const headers: Record<string, string> = {};
    for (const r of rows) { if (r.key.trim()) headers[r.key.trim()] = r.value; }
    setWebhookCfg({ headers });
  }

  async function handleSave() {
    if (!form.name?.trim()) { setSaveError('Supplier name is required'); return; }
    if (!form.code?.trim()) { setSaveError('Code is required'); return; }
    setSaving(true); setSaveError('');
    try {
      const isNew = mode === 'new';
      const url = isNew ? '/suppliers' : `/suppliers/${form.supplier_id}`;
      const method = isNew ? 'POST' : 'PUT';
      const res = await api(url, { method, body: JSON.stringify(form) });
      if (!res.ok) { const err = await res.json(); throw new Error(err.error || 'Save failed'); }
      await load(); setMode('list');
    } catch (e: any) { setSaveError(e.message); }
    finally { setSaving(false); }
  }

  async function handleDelete(id: string, name: string) {
    if (!confirm(`Deactivate "${name}"? Existing purchase orders will not be affected.`)) return;
    await api(`/suppliers/${id}`, { method: 'DELETE' });
    load();
  }

  async function handleTestConnection() {
    if (!form.supplier_id) return;
    setTesting(true); setTestResult({ status: 'testing', message: 'Testing connection…' });
    try {
      const res = await api(`/suppliers/${form.supplier_id}/test-connection`, { method: 'POST' });
      const data = await res.json();
      setTestResult({ status: data.status === 'ok' ? 'ok' : 'error', message: data.message });
    } catch (e: any) { setTestResult({ status: 'error', message: 'Request failed: ' + e.message }); }
    finally { setTesting(false); }
  }

  const filtered = suppliers.filter(s => {
    if (!showInactive && !s.active) return false;
    if (!searchQ) return true;
    const q = searchQ.toLowerCase();
    return s.name.toLowerCase().includes(q) || s.code.toLowerCase().includes(q) ||
      s.email?.toLowerCase().includes(q) || s.contact_name?.toLowerCase().includes(q);
  });

  // ── List view ──────────────────────────────────────────────────────────────

  if (mode === 'list') {
    return (
      <div className="sup-page">
        <div className="sup-header">
          <div>
            <h1 className="sup-title">Suppliers</h1>
            <p className="sup-subtitle">Manage dropship suppliers, order placement configuration, and financial details.</p>
          </div>
          <button className="btn btn-primary" onClick={openNew}>+ Add Supplier</button>
        </div>
        <div className="sup-toolbar">
          <input className="sup-search" placeholder="Search by name, code, email…"
            value={searchQ} onChange={e => setSearchQ(e.target.value)} />
          <label className="sup-toggle" style={{ marginLeft: 'auto' }}>
            <input type="checkbox" checked={showInactive} onChange={e => setShowInactive(e.target.checked)} />
            <span className="sup-toggle-label">Show inactive</span>
          </label>
          <span className="sup-count">{filtered.length} supplier{filtered.length !== 1 ? 's' : ''}</span>
        </div>
        {listError && <div className="sup-error">{listError}</div>}
        {loading ? (
          <div className="sup-loading">Loading suppliers…</div>
        ) : filtered.length === 0 ? (
          <div className="sup-empty">
            <div style={{ fontSize: 48, marginBottom: 16 }}>🤝</div>
            <h3>{searchQ ? 'No results' : 'No suppliers yet'}</h3>
            <p>{searchQ ? 'Try a different search term.' : 'Add your first dropship supplier to enable dropship workflows and purchase order automation.'}</p>
            {!searchQ && <button className="btn btn-primary" onClick={openNew}>Add First Supplier</button>}
          </div>
        ) : (
          <div className="sup-table-wrap">
            <table className="sup-table">
              <thead>
                <tr>
                  <th>Supplier</th><th>Code</th><th>Contact</th>
                  <th>Order Method</th><th>Currency</th><th>Lead Time</th>
                  <th>Status</th><th>Added</th><th></th>
                </tr>
              </thead>
              <tbody>
                {filtered.map(s => (
                  <tr key={s.supplier_id} className={!s.active ? 'sup-row-inactive' : ''} onClick={() => openEdit(s)}>
                    <td>
                      <div className="sup-name">{s.name}</div>
                      {s.notes && <div className="sup-note-preview">{s.notes.slice(0,60)}{s.notes.length > 60 ? '…' : ''}</div>}
                    </td>
                    <td><code className="sup-code">{s.code}</code></td>
                    <td>
                      {s.contact_name && <div className="sup-contact-name">{s.contact_name}</div>}
                      {s.email && <a href={`mailto:${s.email}`} className="sup-email" onClick={e => e.stopPropagation()}>{s.email}</a>}
                    </td>
                    <td>{methodBadge(s.order_method)}</td>
                    <td>{s.currency || '—'}</td>
                    <td>{s.lead_time_days != null ? `${s.lead_time_days}d` : '—'}</td>
                    <td><span className={`sup-badge ${s.active ? 'sup-badge-active' : 'sup-badge-inactive'}`}>{s.active ? '● Active' : '○ Inactive'}</span></td>
                    <td className="sup-date">{fmt(s.created_at)}</td>
                    <td onClick={e => e.stopPropagation()}>
                      <div className="sup-row-actions">
                        <button className="btn btn-xs btn-secondary" onClick={() => openEdit(s)}>Edit</button>
                        {s.active && <button className="btn btn-xs btn-danger" onClick={() => handleDelete(s.supplier_id, s.name)}>Deactivate</button>}
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    );
  }

  // ── Detail view ────────────────────────────────────────────────────────────

  const isNew = mode === 'new';
  return (
    <div className="sup-page" style={{ padding: 0, maxWidth: '100%' }}>
      <div className="sup-detail-header">
        <button className="btn btn-ghost btn-sm" onClick={() => setMode('list')}>← Back</button>
        <h1 className="sup-title">{isNew ? 'New Supplier' : form.name || 'Edit Supplier'}</h1>
        <div className="sup-detail-header-meta">
          {!isNew && form.order_method && methodBadge(form.order_method)}
          <span className={`sup-badge ${form.active ? 'sup-badge-active' : 'sup-badge-inactive'}`}>
            {form.active ? '● Active' : '○ Inactive'}
          </span>
        </div>
      </div>

      <div className="sup-tabs">
        {([
          { key: 'general',   label: '📋 General' },
          { key: 'ordering',  label: '⚙️ Order Placement' },
          { key: 'financial', label: '💰 Financial' },
          { key: 'products',  label: '📦 Products', disabled: isNew },
        ] as const).map(t => (
          <button
            key={t.key}
            className={`sup-tab ${activeTab === t.key ? 'active' : ''}`}
            onClick={() => { if (!('disabled' in t && t.disabled)) setActiveTab(t.key); }}
            style={'disabled' in t && t.disabled ? { opacity: 0.4, cursor: 'not-allowed' } : {}}
          >
            {t.label}
          </button>
        ))}
      </div>

      <div className="sup-tab-body" style={{ maxWidth: 860, margin: '0 auto', width: '100%' }}>

        {/* Tab: General */}
        {activeTab === 'general' && (
          <>
            <div className="sup-section">
              <div className="sup-section-header">
                <span className="sup-section-title">📋 Identity</span>
                <label className="sup-toggle">
                  <input type="checkbox" checked={form.active !== false} onChange={e => setF({ active: e.target.checked })} />
                  <span className="sup-toggle-label">{form.active !== false ? 'Active' : 'Inactive'}</span>
                </label>
              </div>
              <div className="sup-section-body">
                <div className="sup-grid">
                  <div className="sup-field sup-col-2">
                    <label className="sup-label">Supplier Name *</label>
                    <input className="sup-input" value={form.name || ''} onChange={e => setF({ name: e.target.value })} placeholder="e.g. Acme Wholesale Ltd" />
                  </div>
                  <div className="sup-field">
                    <label className="sup-label">Code *</label>
                    <input className="sup-input sup-input-mono" value={form.code || ''} onChange={e => setF({ code: e.target.value.toUpperCase() })} placeholder="ACME" />
                  </div>
                  <div className="sup-field">
                    <label className="sup-label">Lead Time (days)</label>
                    <input className="sup-input" type="number" min={0} value={form.lead_time_days ?? 3} onChange={e => setF({ lead_time_days: parseInt(e.target.value) || 0 })} />
                  </div>
                  <div className="sup-field sup-col-2">
                    <label className="sup-label">Notes</label>
                    <textarea className="sup-textarea" rows={3} value={form.notes || ''} onChange={e => setF({ notes: e.target.value })} placeholder="Internal notes about this supplier…" />
                  </div>
                  <div className="sup-field sup-col-2">
                    <label className="sup-label">Tags</label>
                    <TagsInput tags={form.tags || []} onChange={tags => setF({ tags })} />
                  </div>
                </div>
              </div>
            </div>

            <div className="sup-section">
              <div className="sup-section-header"><span className="sup-section-title">📞 Contact</span></div>
              <div className="sup-section-body">
                <div className="sup-grid">
                  <div className="sup-field">
                    <label className="sup-label">Contact Name</label>
                    <input className="sup-input" value={form.contact_name || ''} onChange={e => setF({ contact_name: e.target.value })} placeholder="Jane Smith" />
                  </div>
                  <div className="sup-field">
                    <label className="sup-label">Email</label>
                    <input className="sup-input" type="email" value={form.email || ''} onChange={e => setF({ email: e.target.value })} placeholder="orders@supplier.co.uk" />
                  </div>
                  <div className="sup-field">
                    <label className="sup-label">Phone</label>
                    <input className="sup-input" value={form.phone || ''} onChange={e => setF({ phone: e.target.value })} placeholder="+44 1234 567890" />
                  </div>
                  <div className="sup-field">
                    <label className="sup-label">Website</label>
                    <input className="sup-input" value={form.website || ''} onChange={e => setF({ website: e.target.value })} placeholder="https://acme.co.uk" />
                  </div>
                </div>
              </div>
            </div>

            <div className="sup-section">
              <div className="sup-section-header"><span className="sup-section-title">📍 Address</span></div>
              <div className="sup-section-body">
                <div className="sup-grid">
                  <div className="sup-field sup-col-2">
                    <label className="sup-label">Company Name</label>
                    <input className="sup-input" value={form.address?.company_name || ''} onChange={e => setAddr({ company_name: e.target.value })} />
                  </div>
                  <div className="sup-field sup-col-2">
                    <label className="sup-label">Address Line 1</label>
                    <input className="sup-input" value={form.address?.address_line1 || ''} onChange={e => setAddr({ address_line1: e.target.value })} />
                  </div>
                  <div className="sup-field sup-col-2">
                    <label className="sup-label">Address Line 2</label>
                    <input className="sup-input" value={form.address?.address_line2 || ''} onChange={e => setAddr({ address_line2: e.target.value })} />
                  </div>
                  <div className="sup-field">
                    <label className="sup-label">City</label>
                    <input className="sup-input" value={form.address?.city || ''} onChange={e => setAddr({ city: e.target.value })} />
                  </div>
                  <div className="sup-field">
                    <label className="sup-label">County / State</label>
                    <input className="sup-input" value={form.address?.county || ''} onChange={e => setAddr({ county: e.target.value })} />
                  </div>
                  <div className="sup-field">
                    <label className="sup-label">Postcode / ZIP</label>
                    <input className="sup-input" value={form.address?.postal_code || ''} onChange={e => setAddr({ postal_code: e.target.value })} />
                  </div>
                  <div className="sup-field">
                    <label className="sup-label">Country (ISO 2)</label>
                    <input className="sup-input" value={form.address?.country || ''} onChange={e => setAddr({ country: e.target.value.toUpperCase().slice(0,2) })} placeholder="GB" maxLength={2} />
                  </div>
                </div>
              </div>
            </div>
          </>
        )}

        {/* Tab: Order Placement */}
        {activeTab === 'ordering' && (
          <>
            <div className="sup-section">
              <div className="sup-section-header"><span className="sup-section-title">📬 Order Method</span></div>
              <div className="sup-section-body">
                <div className="sup-method-grid">
                  {[
                    { value: 'email',   icon: '✉️', label: 'Email',    desc: 'Send PO by email' },
                    { value: 'webhook', icon: '⚡',  label: 'Webhook',  desc: 'POST to REST endpoint' },
                    { value: 'ftp',     icon: '📁', label: 'FTP/SFTP', desc: 'Upload CSV to server' },
                    { value: 'manual',  icon: '📋', label: 'Manual',   desc: 'Download / print PO' },
                  ].map(m => (
                    <div key={m.value} className={`sup-method-card ${form.order_method === m.value ? 'selected' : ''}`} onClick={() => setF({ order_method: m.value })}>
                      <div className="sup-method-icon">{m.icon}</div>
                      <div className="sup-method-label">{m.label}</div>
                      <div className="sup-method-desc">{m.desc}</div>
                    </div>
                  ))}
                </div>
              </div>
            </div>

            {form.order_method === 'email' && (
              <div className="sup-section">
                <div className="sup-section-header">
                  <span className="sup-section-title">✉️ Email Configuration</span>
                  {!isNew && <button className="btn btn-sm btn-secondary" onClick={handleTestConnection} disabled={testing}>{testing ? '⏳ Testing…' : '🔌 Send Test Email'}</button>}
                </div>
                <div className="sup-section-body">
                  <div className="sup-grid">
                    <div className="sup-field sup-col-2">
                      <label className="sup-label">To Addresses (comma-separated)</label>
                      <input className="sup-input" value={(form.email_config?.to_addresses ?? []).join(', ')} onChange={e => setEmailCfg({ to_addresses: e.target.value.split(',').map(s => s.trim()).filter(Boolean) })} placeholder="orders@supplier.com" />
                    </div>
                    <div className="sup-field sup-col-2">
                      <label className="sup-label">CC Addresses</label>
                      <input className="sup-input" value={(form.email_config?.cc_addresses ?? []).join(', ')} onChange={e => setEmailCfg({ cc_addresses: e.target.value.split(',').map(s => s.trim()).filter(Boolean) })} placeholder="manager@supplier.com" />
                    </div>
                    <div className="sup-field sup-col-2">
                      <label className="sup-label">Subject Template</label>
                      <input className="sup-input" value={form.email_config?.subject_template || ''} onChange={e => setEmailCfg({ subject_template: e.target.value })} placeholder="PO-{po_number} from MarketMate" />
                      <span className="sup-info">Variables: {'{po_number}'}, {'{supplier_name}'}, {'{date}'}</span>
                    </div>
                    <div className="sup-field sup-col-2">
                      <label className="sup-label">Email Body (optional override)</label>
                      <textarea className="sup-textarea" rows={4} value={form.email_config?.body_template || ''} onChange={e => setEmailCfg({ body_template: e.target.value })} placeholder="Leave blank to use the default MarketMate PO email template." />
                    </div>
                    <div className="sup-field">
                      <label className="sup-toggle"><input type="checkbox" checked={form.email_config?.attach_csv ?? false} onChange={e => setEmailCfg({ attach_csv: e.target.checked })} /><span className="sup-toggle-label">Attach CSV</span></label>
                    </div>
                    <div className="sup-field">
                      <label className="sup-toggle"><input type="checkbox" checked={form.email_config?.attach_pdf ?? false} onChange={e => setEmailCfg({ attach_pdf: e.target.checked })} /><span className="sup-toggle-label">Attach PDF</span></label>
                    </div>
                  </div>
                  {testResult && <div className={`sup-test-result ${testResult.status}`}>{testResult.status === 'ok' ? '✅' : testResult.status === 'testing' ? '⏳' : '❌'} {testResult.message}</div>}
                </div>
              </div>
            )}

            {form.order_method === 'webhook' && (
              <div className="sup-section">
                <div className="sup-section-header">
                  <span className="sup-section-title">⚡ Webhook Configuration</span>
                  {!isNew && <button className="btn btn-sm btn-secondary" onClick={handleTestConnection} disabled={testing}>{testing ? '⏳ Testing…' : '🔌 Test Connection'}</button>}
                </div>
                <div className="sup-section-body">
                  <div className="sup-grid">
                    <div className="sup-field sup-col-2">
                      <label className="sup-label">Endpoint URL</label>
                      <input className="sup-input" value={form.webhook_config?.url || ''} onChange={e => setWebhookCfg({ url: e.target.value })} placeholder="https://api.supplier.com/orders" />
                    </div>
                    <div className="sup-field">
                      <label className="sup-label">HTTP Method</label>
                      <select className="sup-select" value={form.webhook_config?.method || 'POST'} onChange={e => setWebhookCfg({ method: e.target.value as 'POST' | 'PUT' })}>
                        <option value="POST">POST</option><option value="PUT">PUT</option>
                      </select>
                    </div>
                    <div className="sup-field">
                      <label className="sup-label">Payload Format</label>
                      <select className="sup-select" value={form.webhook_config?.format || 'json'} onChange={e => setWebhookCfg({ format: e.target.value as 'json' | 'xml' })}>
                        <option value="json">JSON</option><option value="xml">XML</option>
                      </select>
                    </div>
                    <div className="sup-field">
                      <label className="sup-label">Auth Type</label>
                      <select className="sup-select" value={form.webhook_config?.auth_type || 'none'} onChange={e => setWebhookCfg({ auth_type: e.target.value as SupplierWebhookConfig['auth_type'] })}>
                        <option value="none">None</option>
                        <option value="api_key">API Key Header</option>
                        <option value="bearer">Bearer Token</option>
                        <option value="basic">Basic Auth</option>
                      </select>
                    </div>
                    {form.webhook_config?.auth_type === 'api_key' && (
                      <div className="sup-field">
                        <label className="sup-label">API Key Header Name</label>
                        <input className="sup-input" value={form.webhook_config?.auth_header || ''} onChange={e => setWebhookCfg({ auth_header: e.target.value })} placeholder="X-API-Key" />
                      </div>
                    )}
                    {form.webhook_config?.auth_type === 'basic' && (
                      <div className="sup-field">
                        <label className="sup-label">Username</label>
                        <input className="sup-input" value={form.webhook_config?.auth_header || ''} onChange={e => setWebhookCfg({ auth_header: e.target.value })} placeholder="api_user" />
                      </div>
                    )}
                    {(form.webhook_config?.auth_type === 'api_key' || form.webhook_config?.auth_type === 'bearer' || form.webhook_config?.auth_type === 'basic') && (
                      <div className="sup-field">
                        <label className="sup-label">{form.webhook_config?.auth_type === 'basic' ? 'Password' : 'Secret / Token'}</label>
                        <MaskedInput value={form.webhook_config?.secret_enc || ''} onChange={v => setWebhookCfg({ secret_enc: v })} placeholder="Enter secret…" />
                        <span className="sup-info">Encrypted before storage using AES-256-GCM</span>
                      </div>
                    )}
                  </div>
                  <div className="sup-divider" />
                  <div style={{ marginBottom: 8, fontSize: 13, fontWeight: 600, color: 'var(--text-secondary)' }}>Custom Headers</div>
                  <table className="sup-headers-table">
                    <thead><tr><th>Header Name</th><th>Value</th><th style={{ width: 40 }}></th></tr></thead>
                    <tbody>
                      {headerRows.map((row, i) => (
                        <tr key={i}>
                          <td><input className="sup-input" value={row.key} onChange={e => { const r=[...headerRows]; r[i]={...r[i],key:e.target.value}; syncHeaderRows(r); }} placeholder="Header-Name" /></td>
                          <td><input className="sup-input" value={row.value} onChange={e => { const r=[...headerRows]; r[i]={...r[i],value:e.target.value}; syncHeaderRows(r); }} placeholder="value" /></td>
                          <td><button type="button" className="sup-del-btn" onClick={() => syncHeaderRows(headerRows.filter((_,j)=>j!==i))}>×</button></td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                  <button type="button" className="btn btn-sm btn-secondary" onClick={() => syncHeaderRows([...headerRows,{key:'',value:''}])}>+ Add Header</button>
                  {testResult && <div className={`sup-test-result ${testResult.status}`}>{testResult.status==='ok'?'✅':testResult.status==='testing'?'⏳':'❌'} {testResult.message}</div>}
                </div>
              </div>
            )}

            {form.order_method === 'ftp' && (
              <div className="sup-section">
                <div className="sup-section-header">
                  <span className="sup-section-title">📁 FTP / SFTP Configuration</span>
                  {!isNew && <button className="btn btn-sm btn-secondary" onClick={handleTestConnection} disabled={testing}>{testing ? '⏳ Testing…' : '🔌 Test Connection'}</button>}
                </div>
                <div className="sup-section-body">
                  <div className="sup-grid">
                    <div className="sup-field">
                      <label className="sup-label">Protocol</label>
                      <select className="sup-select" value={form.ftp_config?.protocol || 'ftp'} onChange={e => setFTPCfg({ protocol: e.target.value as 'ftp'|'sftp', port: e.target.value==='sftp'?22:21 })}>
                        <option value="ftp">FTP</option><option value="sftp">SFTP</option>
                      </select>
                    </div>
                    <div className="sup-field">
                      <label className="sup-label">Host</label>
                      <input className="sup-input" value={form.ftp_config?.host || ''} onChange={e => setFTPCfg({ host: e.target.value })} placeholder="ftp.supplier.com" />
                    </div>
                    <div className="sup-field">
                      <label className="sup-label">Port</label>
                      <input className="sup-input" type="number" value={form.ftp_config?.port || 21} onChange={e => setFTPCfg({ port: parseInt(e.target.value)||21 })} />
                    </div>
                    <div className="sup-field">
                      <label className="sup-label">Username</label>
                      <input className="sup-input" value={form.ftp_config?.username || ''} onChange={e => setFTPCfg({ username: e.target.value })} placeholder="marketmate_user" />
                    </div>
                    <div className="sup-field">
                      <label className="sup-label">Password</label>
                      <MaskedInput value={form.ftp_config?.password_enc || ''} onChange={v => setFTPCfg({ password_enc: v })} placeholder="Enter password…" />
                      <span className="sup-info">Encrypted before storage using AES-256-GCM</span>
                    </div>
                    <div className="sup-field sup-col-2">
                      <label className="sup-label">Remote Directory Path</label>
                      <input className="sup-input sup-input-mono" value={form.ftp_config?.path || '/'} onChange={e => setFTPCfg({ path: e.target.value })} placeholder="/orders/incoming/" />
                    </div>
                  </div>
                  {testResult && <div className={`sup-test-result ${testResult.status}`}>{testResult.status==='ok'?'✅':testResult.status==='testing'?'⏳':'❌'} {testResult.message}</div>}
                </div>
              </div>
            )}

            <div className="sup-section">
              <div className="sup-section-header"><span className="sup-section-title">📄 CSV Order Template</span></div>
              <div className="sup-section-body">
                <div className="sup-grid" style={{ marginBottom: 20 }}>
                  <div className="sup-field">
                    <label className="sup-label">Delimiter</label>
                    <select className="sup-select" value={form.csv_template?.delimiter || ','} onChange={e => setCSVCfg({ delimiter: e.target.value })}>
                      <option value=",">Comma (,)</option>
                      <option value={'\t'}>Tab (\t)</option>
                      <option value=";">Semicolon (;)</option>
                    </select>
                  </div>
                  <div className="sup-field">
                    <label className="sup-label">Date Format</label>
                    <input className="sup-input" value={form.csv_template?.date_format || 'DD/MM/YYYY'} onChange={e => setCSVCfg({ date_format: e.target.value })} placeholder="DD/MM/YYYY" />
                  </div>
                  <div className="sup-field">
                    <label className="sup-toggle"><input type="checkbox" checked={form.csv_template?.has_header ?? true} onChange={e => setCSVCfg({ has_header: e.target.checked })} /><span className="sup-toggle-label">Include header row</span></label>
                  </div>
                </div>
                <div style={{ marginBottom: 8, fontSize: 13, fontWeight: 600, color: 'var(--text-secondary)' }}>
                  Column Mapping <span className="sup-info" style={{ fontWeight: 400 }}>— Enter the column header your supplier expects</span>
                </div>
                <table className="sup-csv-table">
                  <thead><tr><th>Our Field</th><th>Supplier Column Header</th></tr></thead>
                  <tbody>
                    {CSV_FIELDS.map(f => (
                      <tr key={f.key}>
                        <td><span className="sup-csv-field-name">{f.key}</span></td>
                        <td><input className="sup-input" value={form.csv_template?.column_map?.[f.key] ?? f.label} onChange={e => setCSVCfg({ column_map: { ...(form.csv_template?.column_map ?? {}), [f.key]: e.target.value } })} placeholder={f.label} /></td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          </>
        )}

        {/* Tab: Financial */}
        {activeTab === 'financial' && (
          <>
            <div className="sup-section">
              <div className="sup-section-header"><span className="sup-section-title">💳 Payment Terms</span></div>
              <div className="sup-section-body">
                <div className="sup-grid">
                  <div className="sup-field">
                    <label className="sup-label">Currency</label>
                    <select className="sup-select" value={form.currency || 'GBP'} onChange={e => setF({ currency: e.target.value })}>
                      {CURRENCIES.map(c => <option key={c} value={c}>{c}</option>)}
                    </select>
                  </div>
                  <div className="sup-field">
                    <label className="sup-label">Payment Terms (days)</label>
                    <input className="sup-input" type="number" min={0} value={form.payment_terms_days ?? 30} onChange={e => setF({ payment_terms_days: parseInt(e.target.value)||0 })} />
                  </div>
                  <div className="sup-field">
                    <label className="sup-label">Payment Method</label>
                    <select className="sup-select" value={form.payment_method || ''} onChange={e => setF({ payment_method: e.target.value })}>
                      <option value="">— Select —</option>
                      {PAYMENT_METHODS.map(m => <option key={m.value} value={m.value}>{m.label}</option>)}
                    </select>
                  </div>
                  <div className="sup-field">
                    <label className="sup-label">Credit Limit ({form.currency || 'GBP'})</label>
                    <input className="sup-input" type="number" min={0} step={0.01} value={form.credit_limit ?? ''} onChange={e => setF({ credit_limit: parseFloat(e.target.value)||0 })} placeholder="0.00" />
                  </div>
                  <div className="sup-field">
                    <label className="sup-label">Min Order Value ({form.currency || 'GBP'})</label>
                    <input className="sup-input" type="number" min={0} step={0.01} value={form.min_order_value ?? ''} onChange={e => setF({ min_order_value: parseFloat(e.target.value)||0 })} placeholder="0.00" />
                  </div>
                </div>
              </div>
            </div>

            <div className="sup-section">
              <div className="sup-section-header"><span className="sup-section-title">🏦 Bank Details</span></div>
              <div className="sup-section-body">
                <div className="sup-grid">
                  <div className="sup-field sup-col-2">
                    <label className="sup-label">Account Name</label>
                    <input className="sup-input" value={form.bank_details?.account_name || ''} onChange={e => setBankDetails({ account_name: e.target.value })} placeholder="Acme Wholesale Ltd" />
                  </div>
                  <div className="sup-field">
                    <label className="sup-label">Account Number</label>
                    <MaskedInput value={form.bank_details?.account_number_enc || ''} onChange={v => setBankDetails({ account_number_enc: v })} placeholder="12345678" />
                    <span className="sup-info">Encrypted before storage using AES-256-GCM</span>
                  </div>
                  <div className="sup-field">
                    <label className="sup-label">Sort Code</label>
                    <input className="sup-input" value={form.bank_details?.sort_code || ''} onChange={e => setBankDetails({ sort_code: e.target.value })} placeholder="12-34-56" />
                  </div>
                  <div className="sup-field">
                    <label className="sup-label">IBAN</label>
                    <input className="sup-input sup-input-mono" value={form.bank_details?.iban || ''} onChange={e => setBankDetails({ iban: e.target.value.toUpperCase() })} placeholder="GB12SORT12345678" />
                  </div>
                  <div className="sup-field">
                    <label className="sup-label">BIC / SWIFT</label>
                    <input className="sup-input sup-input-mono" value={form.bank_details?.bic || ''} onChange={e => setBankDetails({ bic: e.target.value.toUpperCase() })} placeholder="AAAAGB2L" />
                  </div>
                  <div className="sup-field sup-col-2">
                    <label className="sup-label">Bank Name</label>
                    <input className="sup-input" value={form.bank_details?.bank_name || ''} onChange={e => setBankDetails({ bank_name: e.target.value })} placeholder="Barclays Bank PLC" />
                  </div>
                </div>
              </div>
            </div>

            <div className="sup-section">
              <div className="sup-section-header"><span className="sup-section-title">🏢 Company Registration</span></div>
              <div className="sup-section-body">
                <div className="sup-grid">
                  <div className="sup-field">
                    <label className="sup-label">VAT Number</label>
                    <input className="sup-input sup-input-mono" value={form.vat_number || ''} onChange={e => setF({ vat_number: e.target.value })} placeholder="GB123456789" />
                  </div>
                  <div className="sup-field">
                    <label className="sup-label">Company Reg Number</label>
                    <input className="sup-input sup-input-mono" value={form.company_reg_number || ''} onChange={e => setF({ company_reg_number: e.target.value })} placeholder="12345678" />
                  </div>
                </div>
              </div>
            </div>
          </>
        )}

        {/* Tab: Products */}
        {activeTab === 'products' && (
          <div className="sup-section">
            <div className="sup-section-header">
              <span className="sup-section-title">📦 Products Using This Supplier</span>
              <span className="sup-info" style={{ margin: 0 }}>Read-only — edit from the product page</span>
            </div>
            {productsLoading ? (
              <div className="sup-products-empty">Loading products…</div>
            ) : products.length === 0 ? (
              <div className="sup-products-empty">
                No products assigned to this supplier yet.<br />
                <span style={{ fontSize: 12, marginTop: 8, display: 'block' }}>Assign this supplier to products from the product edit page.</span>
              </div>
            ) : (
              <table className="sup-products-table">
                <thead>
                  <tr><th>SKU</th><th>Product</th><th>Supplier SKU</th><th>Unit Cost</th><th>Lead Time</th><th>Priority</th><th>Default</th><th></th></tr>
                </thead>
                <tbody>
                  {products.map(p => (
                    <tr key={p.product_id}>
                      <td><code className="sup-code">{p.sku}</code></td>
                      <td>{p.product_title}</td>
                      <td><code className="sup-code">{p.supplier_sku || '—'}</code></td>
                      <td>{p.unit_cost != null ? `${p.currency} ${p.unit_cost.toFixed(2)}` : '—'}</td>
                      <td>{p.lead_time_days != null ? `${p.lead_time_days}d` : '—'}</td>
                      <td>{p.priority || 1}</td>
                      <td>{p.is_default ? <span className="sup-badge sup-badge-active">Default</span> : <span style={{ color: 'var(--text-muted)', fontSize: 12 }}>—</span>}</td>
                      <td><a href={`/products/${p.product_id}/edit`} className="go-link" onClick={e => { e.preventDefault(); navigate(`/products/${p.product_id}/edit`); }}>Edit product →</a></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        )}
      </div>

      <div className="sup-save-bar">
        {saveError && <span className="sup-save-error">⚠ {saveError}</span>}
        <button className="btn btn-ghost" onClick={() => setMode('list')}>Cancel</button>
        <button className="btn btn-primary" onClick={handleSave} disabled={saving}>
          {saving ? 'Saving…' : isNew ? 'Create Supplier' : 'Save Changes'}
        </button>
      </div>
    </div>
  );
}

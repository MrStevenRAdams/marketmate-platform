import { useState, useEffect, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { getActiveTenantId } from '../contexts/TenantContext';
import './FulfilmentSources.css';

const API_BASE = import.meta.env.VITE_API_URL ?? 'http://localhost:8080/api/v1';

function api(path: string, init?: RequestInit) {
  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId(), ...init?.headers },
  });
}

// ─── Types ────────────────────────────────────────────────────────────────────

type SourceType = 'own_warehouse' | '3pl' | 'fba' | 'dropship' | 'virtual';
type IntegrationMethod = 'api' | 'sftp' | 'email' | 'edi' | '';

interface SourceAddress {
  company_name: string;
  address_line1: string;
  address_line2?: string;
  city: string;
  county?: string;
  postcode: string;
  country: string;
  phone?: string;
  email?: string;
}

interface FulfilmentSource {
  source_id: string;
  name: string;
  code: string;
  type: SourceType;
  active: boolean;
  default: boolean;
  address?: SourceAddress;
  inventory_tracked: boolean;
  inventory_mode: string;
  region?: string;
  tags?: string[];
  currency_override?: string;
  is_fulfilment_centre?: boolean;
  integration_method?: IntegrationMethod;
  integration_api_url?: string;
  integration_sftp_host?: string;
  integration_sftp_path?: string;
  integration_email?: string;
  integration_edi_format?: string;
  created_at: string;
  updated_at: string;
}

const SOURCE_TYPES: { value: SourceType; label: string; icon: string; desc: string }[] = [
  { value: 'own_warehouse', label: 'Own Warehouse', icon: '🏭', desc: 'Stock held and despatched by you' },
  { value: '3pl', label: '3PL', icon: '🏢', desc: 'Third-party logistics provider' },
  { value: 'fba', label: 'FBA', icon: '📦', desc: 'Fulfilled by Amazon — no label needed' },
  { value: 'dropship', label: 'Dropship', icon: '🚚', desc: 'Supplier ships directly to customer' },
  { value: 'virtual', label: 'Virtual', icon: '⚡', desc: 'Bundling / kitting — no physical location' },
];

const INVENTORY_MODES = [
  { value: 'real_time', label: 'Real-time' },
  { value: 'daily_sync', label: 'Daily sync' },
  { value: 'manual', label: 'Manual' },
];

function blank(): Partial<FulfilmentSource> {
  return {
    name: '', code: '', type: 'own_warehouse', active: true, default: false,
    inventory_tracked: true, inventory_mode: 'real_time', region: '',
    address: { company_name: '', address_line1: '', city: '', postcode: '', country: 'GB' },
  };
}

function typeIcon(t: SourceType) {
  return SOURCE_TYPES.find(s => s.value === t)?.icon || '📦';
}

function typeLabel(t: SourceType) {
  return SOURCE_TYPES.find(s => s.value === t)?.label || t;
}

// ─── Main Component ───────────────────────────────────────────────────────────

export default function FulfilmentSources() {
  const navigate = useNavigate();
  const [sources, setSources] = useState<FulfilmentSource[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [showForm, setShowForm] = useState(false);
  const [editing, setEditing] = useState<FulfilmentSource | null>(null);
  const [form, setForm] = useState<Partial<FulfilmentSource>>(blank());
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState('');
  const [confirmDefault, setConfirmDefault] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const res = await api('/fulfilment-sources');
      if (!res.ok) throw new Error('Failed to load');
      const data = await res.json();
      setSources(data.sources || []);
    } catch (e: any) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { load(); }, [load]);

  const handleNew = () => {
    setEditing(null);
    setForm(blank());
    setSaveError('');
    setShowForm(true);
  };

  const handleEdit = (s: FulfilmentSource) => {
    setEditing(s);
    setForm({ ...s });
    setSaveError('');
    setShowForm(true);
  };

  const handleDelete = async (id: string) => {
    if (!confirm('Delete this fulfilment source? Any workflows that reference it will need updating.')) return;
    await api(`/fulfilment-sources/${id}`, { method: 'DELETE' });
    load();
  };

  const handleSetDefault = async (id: string) => {
    await api(`/fulfilment-sources/${id}/set-default`, { method: 'POST' });
    setConfirmDefault(null);
    load();
  };

  const handleSave = async () => {
    if (!form.name?.trim()) { setSaveError('Name is required'); return; }
    if (!form.code?.trim()) { setSaveError('Code is required'); return; }

    setSaving(true);
    setSaveError('');
    try {
      const isNew = !editing;
      const url = isNew ? '/fulfilment-sources' : `/fulfilment-sources/${editing!.source_id}`;
      const method = isNew ? 'POST' : 'PATCH';
      const res = await api(url, { method, body: JSON.stringify(form) });
      if (!res.ok) {
        const err = await res.json();
        throw new Error(err.error || 'Save failed');
      }
      setShowForm(false);
      load();
    } catch (e: any) {
      setSaveError(e.message);
    } finally {
      setSaving(false);
    }
  };

  const updateAddr = (patch: Partial<SourceAddress>) => {
    setForm(f => ({ ...f, address: { ...f.address!, ...patch } }));
  };

  if (showForm) {
    return (
      <div className="fs-page">
        <div className="fs-editor-header">
          <button className="btn btn-ghost btn-sm" onClick={() => setShowForm(false)}>← Back</button>
          <h1 className="fs-title">{editing ? `Edit: ${editing.name}` : 'New Fulfilment Source'}</h1>
          <div className="fs-editor-actions">
            <button className="btn btn-ghost" onClick={() => setShowForm(false)}>Cancel</button>
            <button className="btn btn-primary" onClick={handleSave} disabled={saving}>
              {saving ? 'Saving…' : editing ? 'Save Changes' : 'Create Source'}
            </button>
          </div>
        </div>

        {saveError && <div className="fs-error">{saveError}</div>}

        <div className="fs-editor-body">
          {/* Identity */}
          <div className="fs-section">
            <h2 className="fs-section-title">📋 Identity</h2>
            <div className="fs-grid-3">
              <div className="fs-field fs-col-2">
                <label>Display Name *</label>
                <input className="fs-input" value={form.name || ''} onChange={e => setForm(f => ({ ...f, name: e.target.value }))} placeholder="e.g. London Warehouse" />
              </div>
              <div className="fs-field">
                <label>Short Code *</label>
                <input className="fs-input" value={form.code || ''} onChange={e => setForm(f => ({ ...f, code: e.target.value.toUpperCase() }))} placeholder="LON-01" />
              </div>
            </div>
          </div>

          {/* Type */}
          <div className="fs-section">
            <h2 className="fs-section-title">🏭 Source Type</h2>
            <div className="fs-type-grid">
              {SOURCE_TYPES.map(st => (
                <label key={st.value} className={`fs-type-card ${form.type === st.value ? 'selected' : ''}`}>
                  <input type="radio" name="source_type" value={st.value}
                    checked={form.type === st.value}
                    onChange={() => setForm(f => ({ ...f, type: st.value }))}
                  />
                  <span className="fs-type-icon">{st.icon}</span>
                  <span className="fs-type-name">{st.label}</span>
                  <span className="fs-type-desc">{st.desc}</span>
                </label>
              ))}
            </div>
          </div>

          {/* Address — not for FBA/virtual */}
          {(form.type === 'own_warehouse' || form.type === '3pl' || form.type === 'dropship') && (
            <div className="fs-section">
              <h2 className="fs-section-title">📍 Address</h2>
              <div className="fs-grid-2">
                <div className="fs-field fs-col-2">
                  <label>Company Name</label>
                  <input className="fs-input" value={form.address?.company_name || ''} onChange={e => updateAddr({ company_name: e.target.value })} />
                </div>
                <div className="fs-field fs-col-2">
                  <label>Address Line 1</label>
                  <input className="fs-input" value={form.address?.address_line1 || ''} onChange={e => updateAddr({ address_line1: e.target.value })} />
                </div>
                <div className="fs-field fs-col-2">
                  <label>Address Line 2</label>
                  <input className="fs-input" value={form.address?.address_line2 || ''} onChange={e => updateAddr({ address_line2: e.target.value })} />
                </div>
                <div className="fs-field">
                  <label>City</label>
                  <input className="fs-input" value={form.address?.city || ''} onChange={e => updateAddr({ city: e.target.value })} />
                </div>
                <div className="fs-field">
                  <label>Postcode</label>
                  <input className="fs-input" value={form.address?.postcode || ''} onChange={e => updateAddr({ postcode: e.target.value })} />
                </div>
                <div className="fs-field">
                  <label>Country</label>
                  <select className="fs-select" value={form.address?.country || 'GB'} onChange={e => updateAddr({ country: e.target.value })}>
                    {['GB', 'DE', 'FR', 'US', 'AU', 'CA', 'NL', 'BE', 'IT', 'ES'].map(c =>
                      <option key={c} value={c}>{c}</option>
                    )}
                  </select>
                </div>
                <div className="fs-field">
                  <label>Phone</label>
                  <input className="fs-input" value={form.address?.phone || ''} onChange={e => updateAddr({ phone: e.target.value })} />
                </div>
              </div>
            </div>
          )}

          {/* Inventory */}
          <div className="fs-section">
            <h2 className="fs-section-title">📊 Inventory Settings</h2>
            <div className="fs-grid-2">
              <div className="fs-field">
                <label>Track Inventory</label>
                <div className="fs-toggle-row">
                  <label className="fs-toggle">
                    <input type="checkbox" checked={form.inventory_tracked !== false}
                      onChange={e => setForm(f => ({ ...f, inventory_tracked: e.target.checked }))} />
                    <span>{form.inventory_tracked !== false ? 'Yes — stock levels tracked here' : 'No — not tracked (FBA/Dropship)'}</span>
                  </label>
                </div>
              </div>
              {form.inventory_tracked !== false && (
                <div className="fs-field">
                  <label>Inventory Mode</label>
                  <select className="fs-select" value={form.inventory_mode || 'real_time'} onChange={e => setForm(f => ({ ...f, inventory_mode: e.target.value }))}>
                    {INVENTORY_MODES.map(m => <option key={m.value} value={m.value}>{m.label}</option>)}
                  </select>
                </div>
              )}
            </div>
          </div>

          {/* Meta */}
          <div className="fs-section">
            <h2 className="fs-section-title">⚙️ Settings</h2>
            <div className="fs-grid-2">
              <div className="fs-field">
                <label>Region <span className="fs-hint">(used for nearest-source routing)</span></label>
                <input className="fs-input" value={form.region || ''} onChange={e => setForm(f => ({ ...f, region: e.target.value }))} placeholder="e.g. north, south, london" />
              </div>
              <div className="fs-field">
                <label>Active</label>
                <label className="fs-toggle">
                  <input type="checkbox" checked={form.active !== false} onChange={e => setForm(f => ({ ...f, active: e.target.checked }))} />
                  <span>{form.active !== false ? 'Active — available for workflow assignment' : 'Inactive — hidden from workflows'}</span>
                </label>
              </div>
              <div className="fs-field">
                <label>Currency Override <span className="fs-hint">(ISO 4217, overrides seller default)</span></label>
                <input className="fs-input" value={(form as any).currency_override || ''} onChange={e => setForm((f: any) => ({ ...f, currency_override: e.target.value.toUpperCase().slice(0,3) }))} placeholder="e.g. USD, EUR" maxLength={3} style={{ textTransform: 'uppercase', fontFamily: 'monospace' }} />
              </div>
              <div className="fs-field">
                <label>Fulfilment Centre</label>
                <label className="fs-toggle">
                  <input type="checkbox" checked={!!(form as any).is_fulfilment_centre} onChange={e => setForm((f: any) => ({ ...f, is_fulfilment_centre: e.target.checked }))} />
                  <span>{(form as any).is_fulfilment_centre ? 'Yes — this is a fulfilment centre (FC)' : 'No — standard source'}</span>
                </label>
              </div>
            </div>
          </div>

          {/* 3PL Integration */}
          {form.type === '3pl' && (
            <div className="fs-section">
              <h2 className="fs-section-title">🔌 3PL Integration</h2>
              <div className="fs-grid-2">
                <div className="fs-field fs-col-2">
                  <label>Integration Method</label>
                  <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
                    {([['api','API'],['sftp','SFTP'],['email','Email'],['edi','EDI'],['','None']] as [string, string][]).map(([val, lbl]) => (
                      <label key={val || 'none'} style={{ display: 'flex', alignItems: 'center', gap: 6, padding: '6px 12px', background: (form as any).integration_method === val ? 'rgba(124,58,237,0.15)' : 'var(--bg-tertiary)', border: `1px solid ${ (form as any).integration_method === val ? '#7c3aed' : 'var(--border)'}`, borderRadius: 7, cursor: 'pointer', fontSize: 13 }}>
                        <input type="radio" name="int_method" value={val} checked={(form as any).integration_method === val} onChange={() => setForm((f: any) => ({ ...f, integration_method: val }))} style={{ display: 'none' }} />
                        {lbl}
                      </label>
                    ))}
                  </div>
                </div>
                {(form as any).integration_method === 'api' && (
                  <div className="fs-field fs-col-2">
                    <label>API URL</label>
                    <input className="fs-input" value={(form as any).integration_api_url || ''} onChange={e => setForm((f: any) => ({ ...f, integration_api_url: e.target.value }))} placeholder="https://api.3pl-provider.com" />
                  </div>
                )}
                {(form as any).integration_method === 'sftp' && (<>
                  <div className="fs-field">
                    <label>SFTP Host</label>
                    <input className="fs-input" value={(form as any).integration_sftp_host || ''} onChange={e => setForm((f: any) => ({ ...f, integration_sftp_host: e.target.value }))} placeholder="sftp.example.com" />
                  </div>
                  <div className="fs-field">
                    <label>SFTP Path</label>
                    <input className="fs-input" value={(form as any).integration_sftp_path || ''} onChange={e => setForm((f: any) => ({ ...f, integration_sftp_path: e.target.value }))} placeholder="/orders/inbound" />
                  </div>
                </>)}
                {(form as any).integration_method === 'email' && (
                  <div className="fs-field">
                    <label>Integration Email</label>
                    <input className="fs-input" type="email" value={(form as any).integration_email || ''} onChange={e => setForm((f: any) => ({ ...f, integration_email: e.target.value }))} placeholder="orders@3pl-provider.com" />
                  </div>
                )}
                {(form as any).integration_method === 'edi' && (
                  <div className="fs-field">
                    <label>EDI Format</label>
                    <select className="fs-select" value={(form as any).integration_edi_format || ''} onChange={e => setForm((f: any) => ({ ...f, integration_edi_format: e.target.value }))}>
                      <option value="">Select format…</option>
                      <option value="X12">ANSI X12</option>
                      <option value="EDIFACT">UN/EDIFACT</option>
                      <option value="TRADACOMS">TRADACOMS</option>
                      <option value="custom">Custom</option>
                    </select>
                  </div>
                )}
              </div>
            </div>
          )}
        </div>
      </div>
    );
  }

  return (
    <div className="fs-page">
      <div className="fs-header">
        <div>
          <h1 className="fs-title">Fulfilment Sources</h1>
          <p className="fs-subtitle">Warehouses, 3PLs, dropship suppliers, and FBA nodes. The default source is used when no workflow matches an order.</p>
        </div>
        <button className="btn btn-primary" onClick={handleNew}>+ Add Source</button>
      </div>

      {error && <div className="fs-error">{error}</div>}

      {loading ? (
        <div className="fs-loading">Loading…</div>
      ) : sources.length === 0 ? (
        <div className="fs-empty">
          <div className="fs-empty-icon">🏭</div>
          <h3>No fulfilment sources yet</h3>
          <p>Add your first warehouse or 3PL to start routing orders.</p>
          <button className="btn btn-primary" onClick={handleNew}>Add First Source</button>
        </div>
      ) : (
        <div className="fs-grid">
          {sources.map(s => (
            <div key={s.source_id} className={`fs-card ${s.default ? 'fs-card-default' : ''} ${!s.active ? 'fs-card-inactive' : ''}`}>
              {s.default && <div className="fs-default-banner">⭐ Default</div>}
              <div className="fs-card-header">
                <span className="fs-type-chip">{typeIcon(s.type as SourceType)} {typeLabel(s.type as SourceType)}</span>
                {!s.active && <span className="wf-badge badge-archived">Inactive</span>}
              </div>
              <h3 className="fs-card-name">{s.name}</h3>
              <div className="fs-card-code">{s.code}</div>
              {s.address?.city && (
                <div className="fs-card-location">
                  📍 {s.address.city}{s.address.country && `, ${s.address.country}`}
                </div>
              )}
              {s.region && <div className="fs-card-region">Region: {s.region}</div>}
              <div className="fs-card-inventory">
                {s.inventory_tracked
                  ? `📊 Stock tracked (${s.inventory_mode})`
                  : '📊 Stock not tracked'}
              </div>
              <div className="fs-card-actions">
                <button className="btn btn-xs btn-secondary" onClick={() => handleEdit(s)}>✏️ Edit</button>
                {/* FIX (Issue 8): Locations button disabled — /fulfilment-sources/:id/locations has no page or route yet.
                    Re-enable once FulfilmentSourceLocations.tsx is built and route registered in App.tsx.
                    <button className="btn btn-xs btn-secondary" onClick={() => navigate(`/fulfilment-sources/${s.source_id}/locations`)}>🗂 Locations</button>
                */}
                {!s.default && (
                  <button className="btn btn-xs" onClick={() => setConfirmDefault(s.source_id)} title="Set as default">⭐ Set Default</button>
                )}
                <button className="btn btn-xs btn-danger" onClick={() => handleDelete(s.source_id)}>🗑 Delete</button>
              </div>
            </div>
          ))}
        </div>
      )}

      {confirmDefault && (
        <div className="fs-modal-overlay" onClick={() => setConfirmDefault(null)}>
          <div className="fs-modal" onClick={e => e.stopPropagation()}>
            <h3>Set as Default?</h3>
            <p>This source will be used for all orders that don't match any active workflow.</p>
            <div className="fs-modal-actions">
              <button className="btn btn-ghost" onClick={() => setConfirmDefault(null)}>Cancel</button>
              <button className="btn btn-primary" onClick={() => handleSetDefault(confirmDefault)}>Confirm</button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

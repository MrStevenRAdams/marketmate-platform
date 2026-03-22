import { useState, useEffect, useCallback, useRef } from 'react';
import { useParams, useNavigate, Link } from 'react-router-dom';
import { getActiveTenantId } from '../contexts/TenantContext';

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

// ─── Types ────────────────────────────────────────────────────────────────────

const ALL_CHANNELS = ['amazon', 'ebay', 'shopify', 'etsy', 'onbuy', 'temu', 'backmarket', 'tiktok', 'kaufland', 'zalando'];

interface AllocationRule {
  rule_id: string;
  name: string;
  warehouse_id: string;
  warehouse_name?: string;
  channels: string[];
  priority: number;
  min_stock: number;
  active: boolean;
}

// ─── Allocation Rule Modal ────────────────────────────────────────────────────

function RuleModal({
  rule,
  warehouses,
  onClose,
  onSaved,
}: {
  rule: AllocationRule | null;
  warehouses: FulfilmentSource[];
  onClose: () => void;
  onSaved: () => void;
}) {
  const [name, setName] = useState(rule?.name || '');
  const [warehouseId, setWarehouseId] = useState(rule?.warehouse_id || '');
  const [channels, setChannels] = useState<string[]>(rule?.channels || []);
  const [allChannels, setAllChannels] = useState(rule?.channels?.includes('*') || false);
  const [priority, setPriority] = useState(rule?.priority ?? 10);
  const [minStock, setMinStock] = useState(rule?.min_stock ?? 0);
  const [active, setActive] = useState(rule?.active ?? true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  const toggleChannel = (ch: string) => {
    setChannels(prev => prev.includes(ch) ? prev.filter(c => c !== ch) : [...prev, ch]);
  };

  const handleSubmit = async () => {
    if (!name.trim()) { setError('Name is required'); return; }
    if (!warehouseId) { setError('Select a warehouse'); return; }
    const chPayload = allChannels ? ['*'] : channels;
    if (chPayload.length === 0) { setError('Select at least one channel'); return; }
    setSaving(true); setError('');
    try {
      const body = { name: name.trim(), warehouse_id: warehouseId, channels: chPayload, priority, min_stock: minStock, active };
      const res = rule
        ? await api(`/warehouses/allocation-rules/${rule.rule_id}`, { method: 'PUT', body: JSON.stringify(body) })
        : await api('/warehouses/allocation-rules', { method: 'POST', body: JSON.stringify(body) });
      const data = await res.json();
      if (!res.ok) throw new Error(data.error || 'Failed to save rule');
      onSaved();
    } catch (e: any) { setError(e.message); } finally { setSaving(false); }
  };

  return (
    <div style={overlayStyle} onClick={e => e.target === e.currentTarget && onClose()}>
      <div style={{ ...modalStyle, maxWidth: '520px' }}>
        <div style={modalHeaderStyle}>
          <h2 style={{ margin: 0, fontSize: '18px', fontWeight: 600 }}>{rule ? 'Edit Rule' : 'Add Fulfilment Rule'}</h2>
          <button onClick={onClose} style={closeBtnStyle}>✕</button>
        </div>
        <div style={{ padding: '24px', display: 'flex', flexDirection: 'column', gap: '16px' }}>
          <div style={fieldStyle}>
            <label style={labelStyle}>Rule Name <span style={{ color: '#f87171' }}>*</span></label>
            <input value={name} onChange={e => setName(e.target.value)} placeholder="e.g. Amazon Priority UK" style={inputStyle} />
          </div>
          <div style={fieldStyle}>
            <label style={labelStyle}>Warehouse <span style={{ color: '#f87171' }}>*</span></label>
            <select value={warehouseId} onChange={e => setWarehouseId(e.target.value)} style={inputStyle}>
              <option value="">Select warehouse...</option>
              {warehouses.map(w => <option key={w.source_id} value={w.source_id}>{w.name}</option>)}
            </select>
          </div>
          <div style={fieldStyle}>
            <label style={labelStyle}>Channels</label>
            <label style={{ display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '8px', cursor: 'pointer' }}>
              <input type="checkbox" checked={allChannels} onChange={e => setAllChannels(e.target.checked)} />
              <span style={{ fontSize: '13px', color: 'var(--text-secondary)' }}>All channels (*)</span>
            </label>
            {!allChannels && (
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: '8px' }}>
                {ALL_CHANNELS.map(ch => (
                  <button
                    key={ch}
                    onClick={() => toggleChannel(ch)}
                    style={{
                      padding: '4px 12px', borderRadius: '16px', border: '1px solid',
                      borderColor: channels.includes(ch) ? 'var(--accent-cyan)' : 'var(--border-bright)',
                      background: channels.includes(ch) ? 'rgba(6,182,212,0.15)' : 'var(--bg-tertiary)',
                      color: channels.includes(ch) ? 'var(--accent-cyan)' : 'var(--text-muted)',
                      cursor: 'pointer', fontSize: '12px', textTransform: 'capitalize',
                    }}
                  >{ch}</button>
                ))}
              </div>
            )}
          </div>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '12px' }}>
            <div style={fieldStyle}>
              <label style={labelStyle}>Priority <span style={{ color: 'var(--text-muted)', fontWeight: 400 }}>(lower = higher)</span></label>
              <input type="number" min="1" value={priority} onChange={e => setPriority(parseInt(e.target.value) || 1)} style={inputStyle} />
            </div>
            <div style={fieldStyle}>
              <label style={labelStyle}>Min Stock Threshold</label>
              <input type="number" min="0" value={minStock} onChange={e => setMinStock(parseInt(e.target.value) || 0)} style={inputStyle} />
            </div>
          </div>
          <label style={{ display: 'flex', alignItems: 'center', gap: '8px', cursor: 'pointer' }}>
            <input type="checkbox" checked={active} onChange={e => setActive(e.target.checked)} />
            <span style={{ fontSize: '13px', color: 'var(--text-secondary)' }}>Rule active</span>
          </label>
          {error && <div style={errorStyle}>{error}</div>}
          <div style={{ display: 'flex', gap: '12px', justifyContent: 'flex-end' }}>
            <button onClick={onClose} style={btnSecStyle}>Cancel</button>
            <button onClick={handleSubmit} disabled={saving} style={btnPrimaryStyle}>
              {saving ? 'Saving...' : rule ? 'Update Rule' : 'Create Rule'}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

// ─── Allocation Rules Tab ─────────────────────────────────────────────────────

function AllocationRulesTab({ warehouses }: { warehouses: FulfilmentSource[] }) {
  const [rules, setRules] = useState<AllocationRule[]>([]);
  const [loading, setLoading] = useState(true);
  const [editRule, setEditRule] = useState<AllocationRule | null | 'new'>(null);
  const [error, setError] = useState('');

  const load = async () => {
    setLoading(true);
    try {
      const res = await api('/warehouses/allocation-rules');
      if (!res.ok) throw new Error('Failed to load rules');
      const d = await res.json();
      setRules(d.rules || []);
    } catch (e: any) { setError(e.message); } finally { setLoading(false); }
  };

  useEffect(() => { load(); }, []);

  const toggleActive = async (rule: AllocationRule) => {
    await api(`/warehouses/allocation-rules/${rule.rule_id}`, {
      method: 'PUT',
      body: JSON.stringify({ ...rule, active: !rule.active }),
    });
    load();
  };

  const deleteRule = async (ruleId: string) => {
    if (!window.confirm('Delete this rule?')) return;
    await api(`/warehouses/allocation-rules/${ruleId}`, { method: 'DELETE' });
    load();
  };

  const movePriority = async (rule: AllocationRule, dir: -1 | 1) => {
    await api(`/warehouses/allocation-rules/${rule.rule_id}`, {
      method: 'PUT',
      body: JSON.stringify({ ...rule, priority: rule.priority + dir }),
    });
    load();
  };

  const sorted = [...rules].sort((a, b) => a.priority - b.priority);

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: '16px' }}>
        <button style={btnPrimaryStyle} onClick={() => setEditRule('new')}>＋ Add Rule</button>
      </div>
      {error && <div style={errorStyle}>{error}</div>}
      {loading ? (
        <div style={{ padding: '32px', textAlign: 'center', color: 'var(--text-muted)' }}>Loading rules...</div>
      ) : sorted.length === 0 ? (
        <div style={{ padding: '48px', textAlign: 'center', color: 'var(--text-muted)', background: 'var(--bg-secondary)', borderRadius: '8px', border: '1px solid var(--border-bright)' }}>
          <div style={{ fontSize: '32px', marginBottom: '12px' }}>📋</div>
          <p>No fulfilment rules yet. Add a rule to control which warehouse fulfils which channel.</p>
        </div>
      ) : (
        <div style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border-bright)', borderRadius: '8px', overflow: 'hidden' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '13px' }}>
            <thead>
              <tr style={{ background: 'var(--bg-tertiary)' }}>
                {['Priority', 'Name', 'Warehouse', 'Channels', 'Min Stock', 'Active', 'Order', 'Actions'].map(h => (
                  <th key={h} style={{ padding: '10px 14px', textAlign: 'left', fontSize: '11px', fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.05em', borderBottom: '1px solid var(--border-bright)' }}>{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {sorted.map((rule, idx) => (
                <tr key={rule.rule_id} style={{ borderBottom: '1px solid var(--border-bright)' }}>
                  <td style={{ padding: '12px 14px', color: 'var(--text-muted)', fontFamily: 'monospace' }}>{rule.priority}</td>
                  <td style={{ padding: '12px 14px', fontWeight: 600, color: 'var(--text-primary)' }}>{rule.name}</td>
                  <td style={{ padding: '12px 14px', color: 'var(--text-secondary)' }}>{rule.warehouse_name || rule.warehouse_id}</td>
                  <td style={{ padding: '12px 14px' }}>
                    <div style={{ display: 'flex', flexWrap: 'wrap', gap: '4px' }}>
                      {rule.channels.includes('*')
                        ? <span style={{ padding: '2px 8px', background: 'rgba(6,182,212,0.15)', color: 'var(--accent-cyan)', borderRadius: '10px', fontSize: '11px' }}>All</span>
                        : rule.channels.map(ch => (
                          <span key={ch} style={{ padding: '2px 8px', background: 'var(--bg-tertiary)', color: 'var(--text-muted)', borderRadius: '10px', fontSize: '11px', textTransform: 'capitalize' }}>{ch}</span>
                        ))
                      }
                    </div>
                  </td>
                  <td style={{ padding: '12px 14px', color: 'var(--text-secondary)' }}>{rule.min_stock}</td>
                  <td style={{ padding: '12px 14px' }}>
                    <button
                      onClick={() => toggleActive(rule)}
                      style={{
                        padding: '3px 10px', borderRadius: '12px', border: 'none', fontSize: '12px', fontWeight: 600, cursor: 'pointer',
                        background: rule.active ? 'rgba(16,185,129,0.15)' : 'rgba(100,116,139,0.15)',
                        color: rule.active ? 'var(--success)' : 'var(--text-muted)',
                      }}
                    >{rule.active ? 'Active' : 'Inactive'}</button>
                  </td>
                  <td style={{ padding: '12px 14px' }}>
                    <div style={{ display: 'flex', gap: '4px' }}>
                      <button onClick={() => movePriority(rule, -1)} disabled={idx === 0} style={{ ...iconBtnStyle, opacity: idx === 0 ? 0.3 : 1 }}>▲</button>
                      <button onClick={() => movePriority(rule, 1)} disabled={idx === sorted.length - 1} style={{ ...iconBtnStyle, opacity: idx === sorted.length - 1 ? 0.3 : 1 }}>▼</button>
                    </div>
                  </td>
                  <td style={{ padding: '12px 14px' }}>
                    <div style={{ display: 'flex', gap: '4px' }}>
                      <button onClick={() => setEditRule(rule)} style={iconBtnStyle} title="Edit">✏️</button>
                      <button onClick={() => deleteRule(rule.rule_id)} style={{ ...iconBtnStyle, color: '#f87171' }} title="Delete">🗑</button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {editRule && (
        <RuleModal
          rule={editRule === 'new' ? null : editRule}
          warehouses={warehouses}
          onClose={() => setEditRule(null)}
          onSaved={() => { setEditRule(null); load(); }}
        />
      )}
    </div>
  );
}

interface WarehouseLocation {
  location_id: string;
  name: string;
  parent_id: string;
  source_id: string;
  path: string;
  depth: number;
  is_leaf: boolean;
  sort_order: number;
  barcode: string;
  active: boolean;
  children?: WarehouseLocation[];
  stock?: number;
}

interface FulfilmentSource {
  source_id: string;
  name: string;
}

// ─── Zone & Binrack Types ─────────────────────────────────────────────────────

interface Zone {
  zone_id: string;
  name: string;
  zone_type: string;
  colour: string;
  warehouse_id: string;
}

interface Binrack {
  binrack_id: string;
  name: string;
  barcode: string;
  binrack_type: string;
  zone_id: string;
  zone_name?: string;
  status: string;
  aisle: string;
  section: string;
  level: string;
  bin_number: string;
  length_cm: number;
  width_cm: number;
  height_cm: number;
  max_weight_kg: number;
  capacity: number;
  current_utilisation?: number;
  item_restrictions: string[];
  storage_group_id: string;
}

interface BinType {
  id: string;
  name: string;
  colour: string;
  standard_type: string;
}

interface StorageGroup {
  id: string;
  name: string;
}

interface BinrackItem {
  sku: string;
  product_name: string;
  quantity: number;
}

const ZONE_TYPES = ['Standard', 'Refrigerated', 'Hazardous', 'Valuable', 'High Shelf', 'Floor'];
const ZONE_TYPE_COLOURS: Record<string, string> = {
  Standard: '#6b7280', Refrigerated: '#0ea5e9', Hazardous: '#ef4444',
  Valuable: '#f59e0b', 'High Shelf': '#8b5cf6', Floor: '#10b981',
};

// ─── Zone Edit Modal ──────────────────────────────────────────────────────────

function ZoneEditModal({ zone, onClose, onSaved }: { zone: Zone | null; warehouseId: string; onClose: () => void; onSaved: () => void }) {
  const [name, setName] = useState(zone?.name || '');
  const [zoneType, setZoneType] = useState(zone?.zone_type || 'Standard');
  const [colour, setColour] = useState(zone?.colour || '#6b7280');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  const handleSubmit = async () => {
    if (!name.trim()) { setError('Name is required'); return; }
    setSaving(true);
    try {
      const body = { name: name.trim(), zone_type: zoneType, colour };
      const res = zone
        ? await api(`/warehouse/zones/${zone.zone_id}`, { method: 'PUT', body: JSON.stringify(body) })
        : await api('/warehouse/zones', { method: 'POST', body: JSON.stringify(body) });
      if (!res.ok) throw new Error(await res.text());
      onSaved();
    } catch (e: any) { setError(e.message); } finally { setSaving(false); }
  };

  return (
    <div style={overlayStyle} onClick={e => e.target === e.currentTarget && onClose()}>
      <div style={{ ...modalStyle, maxWidth: 420 }}>
        <div style={modalHeaderStyle}>
          <h2 style={{ margin: 0, fontSize: 18, fontWeight: 600 }}>{zone ? 'Edit Zone' : 'Add Zone'}</h2>
          <button onClick={onClose} style={closeBtnStyle}>✕</button>
        </div>
        <div style={{ padding: 24, display: 'flex', flexDirection: 'column', gap: 16 }}>
          <div style={fieldStyle}>
            <label style={labelStyle}>Zone Name *</label>
            <input value={name} onChange={e => setName(e.target.value)} placeholder="e.g. Ambient, Chilled" style={inputStyle} />
          </div>
          <div style={fieldStyle}>
            <label style={labelStyle}>Zone Type</label>
            <select value={zoneType} onChange={e => setZoneType(e.target.value)} style={inputStyle}>
              {ZONE_TYPES.map(t => <option key={t} value={t}>{t}</option>)}
            </select>
          </div>
          <div style={fieldStyle}>
            <label style={labelStyle}>Badge Colour</label>
            <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
              <input type="color" value={colour} onChange={e => setColour(e.target.value)}
                style={{ width: 44, height: 36, padding: 2, border: '1px solid var(--border-bright)', borderRadius: 6, cursor: 'pointer', background: 'var(--bg-tertiary)' }} />
              <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                {Object.entries(ZONE_TYPE_COLOURS).map(([type, c]) => (
                  <button key={type} title={type} onClick={() => setColour(c)}
                    style={{ width: 22, height: 22, borderRadius: '50%', background: c, border: colour === c ? '2px solid white' : '2px solid transparent', cursor: 'pointer' }} />
                ))}
              </div>
            </div>
          </div>
          {error && <div style={errorStyle}>{error}</div>}
          <div style={{ display: 'flex', gap: 12, justifyContent: 'flex-end' }}>
            <button onClick={onClose} style={btnSecStyle}>Cancel</button>
            <button onClick={handleSubmit} disabled={saving} style={btnPrimaryStyle}>{saving ? 'Saving…' : zone ? 'Update Zone' : 'Create Zone'}</button>
          </div>
        </div>
      </div>
    </div>
  );
}

// ─── Binrack Edit Modal ───────────────────────────────────────────────────────

function BinrackEditModal({ binrack, zones, binTypes, storageGroups, warehouseId, onClose, onSaved }: {
  binrack: Binrack | null; zones: Zone[]; binTypes: BinType[]; storageGroups: StorageGroup[];
  warehouseId: string; onClose: () => void; onSaved: () => void;
}) {
  const blank: Binrack = { binrack_id: '', name: '', barcode: '', binrack_type: '', zone_id: '', status: 'available', aisle: '', section: '', level: '', bin_number: '', length_cm: 0, width_cm: 0, height_cm: 0, max_weight_kg: 0, capacity: 0, item_restrictions: [], storage_group_id: '' };
  const init = binrack || blank;
  const [form, setForm] = useState({ ...init });
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [restrictionInput, setRestrictionInput] = useState('');

  const set = (k: keyof Binrack, v: any) => setForm(prev => ({ ...prev, [k]: v }));

  const handleSubmit = async () => {
    if (!form.name.trim()) { setError('Name is required'); return; }
    setSaving(true);
    try {
      const body = { ...form, warehouse_id: warehouseId };
      const res = binrack
        ? await api(`/warehouse/binracks/${binrack.binrack_id}`, { method: 'PUT', body: JSON.stringify(body) })
        : await api('/warehouse/binracks', { method: 'POST', body: JSON.stringify(body) });
      if (!res.ok) throw new Error(await res.text());
      onSaved();
    } catch (e: any) { setError(e.message); } finally { setSaving(false); }
  };

  const addRestriction = () => {
    const v = restrictionInput.trim().toUpperCase();
    if (v && !form.item_restrictions.includes(v)) {
      set('item_restrictions', [...form.item_restrictions, v]);
    }
    setRestrictionInput('');
  };

  const sectionTitle = (t: string) => (
    <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.06em', marginTop: 8, paddingBottom: 4, borderBottom: '1px solid var(--border-bright)' }}>{t}</div>
  );

  return (
    <div style={overlayStyle} onClick={e => e.target === e.currentTarget && onClose()}>
      <div style={{ ...modalStyle, maxWidth: 600, maxHeight: '90vh', overflowY: 'auto' }}>
        <div style={modalHeaderStyle}>
          <h2 style={{ margin: 0, fontSize: 18, fontWeight: 600 }}>{binrack ? 'Edit Binrack' : 'Add Binrack'}</h2>
          <button onClick={onClose} style={closeBtnStyle}>✕</button>
        </div>
        <div style={{ padding: 24, display: 'flex', flexDirection: 'column', gap: 14 }}>
          {/* Identity */}
          {sectionTitle('Identity')}
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
            <div style={fieldStyle}>
              <label style={labelStyle}>Name *</label>
              <input value={form.name} onChange={e => set('name', e.target.value)} placeholder="A-01-01" style={inputStyle} />
            </div>
            <div style={fieldStyle}>
              <label style={labelStyle}>Barcode</label>
              <input value={form.barcode} onChange={e => set('barcode', e.target.value)} placeholder="Scan barcode" style={inputStyle} />
            </div>
          </div>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: 12 }}>
            <div style={fieldStyle}>
              <label style={labelStyle}>Zone</label>
              <select value={form.zone_id} onChange={e => set('zone_id', e.target.value)} style={inputStyle}>
                <option value="">No zone</option>
                {zones.map(z => <option key={z.zone_id} value={z.zone_id}>{z.name}</option>)}
              </select>
            </div>
            <div style={fieldStyle}>
              <label style={labelStyle}>Bin Type</label>
              <select value={form.binrack_type} onChange={e => set('binrack_type', e.target.value)} style={inputStyle}>
                <option value="">None</option>
                {binTypes.map(t => <option key={t.id} value={t.id}>{t.name}</option>)}
              </select>
            </div>
            <div style={fieldStyle}>
              <label style={labelStyle}>Status</label>
              <select value={form.status} onChange={e => set('status', e.target.value)} style={inputStyle}>
                <option value="available">Available</option>
                <option value="occupied">Occupied</option>
                <option value="locked">Locked</option>
              </select>
            </div>
          </div>

          {/* Position */}
          {sectionTitle('Position')}
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr 1fr', gap: 12 }}>
            {(['aisle', 'section', 'level', 'bin_number'] as const).map(f => (
              <div key={f} style={fieldStyle}>
                <label style={labelStyle}>{f.replace('_', ' ')}</label>
                <input value={form[f]} onChange={e => set(f, e.target.value)} style={inputStyle} />
              </div>
            ))}
          </div>

          {/* Dimensions */}
          {sectionTitle('Dimensions & Capacity')}
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: 12 }}>
            <div style={fieldStyle}><label style={labelStyle}>Length (cm)</label><input type="number" value={form.length_cm || ''} onChange={e => set('length_cm', parseFloat(e.target.value) || 0)} style={inputStyle} /></div>
            <div style={fieldStyle}><label style={labelStyle}>Width (cm)</label><input type="number" value={form.width_cm || ''} onChange={e => set('width_cm', parseFloat(e.target.value) || 0)} style={inputStyle} /></div>
            <div style={fieldStyle}><label style={labelStyle}>Height (cm)</label><input type="number" value={form.height_cm || ''} onChange={e => set('height_cm', parseFloat(e.target.value) || 0)} style={inputStyle} /></div>
            <div style={fieldStyle}><label style={labelStyle}>Max Weight (kg)</label><input type="number" value={form.max_weight_kg || ''} onChange={e => set('max_weight_kg', parseFloat(e.target.value) || 0)} style={inputStyle} /></div>
            <div style={fieldStyle}><label style={labelStyle}>Capacity (units)</label><input type="number" value={form.capacity || ''} onChange={e => set('capacity', parseInt(e.target.value) || 0)} style={inputStyle} /></div>
          </div>

          {/* Storage Group */}
          {sectionTitle('Storage')}
          <div style={fieldStyle}>
            <label style={labelStyle}>Storage Group</label>
            <select value={form.storage_group_id} onChange={e => set('storage_group_id', e.target.value)} style={{ ...inputStyle, maxWidth: 260 }}>
              <option value="">No group</option>
              {storageGroups.map(g => <option key={g.id} value={g.id}>{g.name}</option>)}
            </select>
          </div>

          {/* Restrictions */}
          {sectionTitle('Item Restrictions (SKUs allowed here)')}
          <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
            <input value={restrictionInput} onChange={e => setRestrictionInput(e.target.value.toUpperCase())}
              onKeyDown={e => e.key === 'Enter' && addRestriction()} placeholder="Type SKU + Enter" style={{ ...inputStyle, maxWidth: 220 }} />
            <button onClick={addRestriction} style={btnSecStyle}>Add</button>
          </div>
          {form.item_restrictions.length > 0 && (
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
              {form.item_restrictions.map(s => (
                <span key={s} style={{ padding: '3px 10px', background: 'rgba(124,58,237,0.12)', color: '#7c3aed', borderRadius: 12, fontSize: 12, display: 'flex', alignItems: 'center', gap: 4 }}>
                  {s} <button onClick={() => set('item_restrictions', form.item_restrictions.filter(x => x !== s))}
                    style={{ background: 'none', border: 'none', cursor: 'pointer', color: '#7c3aed', fontSize: 14, lineHeight: 1, padding: 0 }}>×</button>
                </span>
              ))}
            </div>
          )}

          {error && <div style={errorStyle}>{error}</div>}
          <div style={{ display: 'flex', gap: 12, justifyContent: 'flex-end', marginTop: 4 }}>
            <button onClick={onClose} style={btnSecStyle}>Cancel</button>
            <button onClick={handleSubmit} disabled={saving} style={btnPrimaryStyle}>{saving ? 'Saving…' : binrack ? 'Update Binrack' : 'Create Binrack'}</button>
          </div>
        </div>
      </div>
    </div>
  );
}

// ─── Binracks Tab ─────────────────────────────────────────────────────────────

function BinracksTab({ warehouseId }: { warehouseId: string }) {
  const [zones, setZones] = useState<Zone[]>([]);
  const [binracks, setBinracks] = useState<Binrack[]>([]);
  const [binTypes, setBinTypes] = useState<BinType[]>([]);
  const [storageGroups, setStorageGroups] = useState<StorageGroup[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  // Filters
  const [filterZone, setFilterZone] = useState('');
  const [filterBinType, setFilterBinType] = useState('');
  const [filterStatus, setFilterStatus] = useState('');
  const [filterCapacity, setFilterCapacity] = useState('');

  // Edit modals
  const [editZone, setEditZone] = useState<Zone | 'new' | null>(null);
  const [editBinrack, setEditBinrack] = useState<Binrack | 'new' | null>(null);

  // Expanded binrack items
  const [expandedBinrack, setExpandedBinrack] = useState<string | null>(null);
  const [binrackItems, setBinrackItems] = useState<BinrackItem[]>([]);
  const [itemsLoading, setItemsLoading] = useState(false);

  const load = async () => {
    setLoading(true);
    try {
      const [zonesRes, binracksRes, btRes, sgRes] = await Promise.all([
        api(`/warehouse/zones?warehouse_id=${warehouseId}`),
        api(`/warehouse/binracks?warehouse_id=${warehouseId}`),
        api('/settings/bin-types'),
        api('/storage-groups'),
      ]);
      if (zonesRes.ok) setZones((await zonesRes.json()).zones || []);
      if (binracksRes.ok) setBinracks((await binracksRes.json()).binracks || []);
      if (btRes.ok) setBinTypes((await btRes.json()).bin_types || []);
      if (sgRes.ok) setStorageGroups((await sgRes.json()).storage_groups || []);
    } catch (e: any) { setError(e.message); }
    finally { setLoading(false); }
  };

  useEffect(() => { load(); }, [warehouseId]);

  const toggleExpand = async (id: string) => {
    if (expandedBinrack === id) { setExpandedBinrack(null); return; }
    setExpandedBinrack(id);
    setItemsLoading(true);
    try {
      const res = await api(`/warehouse/binracks/${id}/items`);
      if (res.ok) setBinrackItems((await res.json()).items || []);
    } catch { setBinrackItems([]); }
    finally { setItemsLoading(false); }
  };

  const deleteZone = async (zoneId: string) => {
    if (!window.confirm('Delete this zone?')) return;
    await api(`/warehouse/zones/${zoneId}`, { method: 'DELETE' });
    load();
  };

  const deleteBinrack = async (id: string) => {
    if (!window.confirm('Delete this binrack?')) return;
    await api(`/warehouse/binracks/${id}`, { method: 'DELETE' });
    load();
  };

  const capacityPct = (b: Binrack) => b.capacity > 0 ? Math.round(((b.current_utilisation ?? 0) / b.capacity) * 100) : 0;

  const filtered = binracks.filter(b => {
    if (filterZone && b.zone_id !== filterZone) return false;
    if (filterBinType && b.binrack_type !== filterBinType) return false;
    if (filterStatus && b.status !== filterStatus) return false;
    if (filterCapacity) {
      const pct = capacityPct(b);
      if (filterCapacity === 'under50' && pct >= 50) return false;
      if (filterCapacity === '50to80' && (pct < 50 || pct > 80)) return false;
      if (filterCapacity === 'over80' && pct <= 80) return false;
      if (filterCapacity === 'full' && pct < 100) return false;
    }
    return true;
  });

  const statusBadge = (s: string) => {
    const cfg: Record<string, [string, string]> = {
      available: ['rgba(16,185,129,0.15)', '#10b981'],
      occupied: ['rgba(245,158,11,0.15)', '#f59e0b'],
      locked: ['rgba(239,68,68,0.15)', '#ef4444'],
    };
    const [bg, color] = cfg[s] || ['var(--bg-elevated)', 'var(--text-muted)'];
    return <span style={{ padding: '2px 8px', borderRadius: 10, background: bg, color, fontSize: 11, fontWeight: 600 }}>{s}</span>;
  };

  if (loading) return <div style={{ padding: 32, textAlign: 'center', color: 'var(--text-muted)' }}>Loading…</div>;

  return (
    <div>
      {error && <div style={errorStyle}>{error}</div>}

      {/* ── Zones ── */}
      <div style={{ marginBottom: 28 }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12 }}>
          <h3 style={{ margin: 0, fontSize: 15, fontWeight: 600, color: 'var(--text-primary)' }}>Zones</h3>
          <button style={btnPrimaryStyle} onClick={() => setEditZone('new')}>＋ Add Zone</button>
        </div>
        {zones.length === 0 ? (
          <div style={{ padding: '24px', textAlign: 'center', color: 'var(--text-muted)', background: 'var(--bg-secondary)', border: '1px solid var(--border-bright)', borderRadius: 8 }}>
            No zones configured.
          </div>
        ) : (
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 10 }}>
            {zones.map(z => (
              <div key={z.zone_id} style={{ padding: '8px 14px', background: 'var(--bg-secondary)', border: `1px solid ${z.colour || '#6b7280'}40`, borderLeft: `4px solid ${z.colour || '#6b7280'}`, borderRadius: 8, display: 'flex', alignItems: 'center', gap: 10 }}>
                <span style={{ width: 10, height: 10, borderRadius: '50%', background: z.colour || '#6b7280', display: 'inline-block', flexShrink: 0 }} />
                <span style={{ fontWeight: 600, fontSize: 13, color: 'var(--text-primary)' }}>{z.name}</span>
                <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>{z.zone_type}</span>
                <button onClick={() => setEditZone(z)} style={iconBtnStyle} title="Edit">✏️</button>
                <button onClick={() => deleteZone(z.zone_id)} style={{ ...iconBtnStyle, color: '#f87171' }} title="Delete">🗑</button>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* ── Binracks ── */}
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12 }}>
        <h3 style={{ margin: 0, fontSize: 15, fontWeight: 600, color: 'var(--text-primary)' }}>Binracks</h3>
        <button style={btnPrimaryStyle} onClick={() => setEditBinrack('new')}>＋ Add Binrack</button>
      </div>

      {/* Filters */}
      <div style={{ display: 'flex', gap: 10, flexWrap: 'wrap', marginBottom: 16, padding: '12px 14px', background: 'var(--bg-secondary)', border: '1px solid var(--border-bright)', borderRadius: 8 }}>
        <select value={filterZone} onChange={e => setFilterZone(e.target.value)} style={{ ...inputStyle, width: 140 }}>
          <option value="">All Zones</option>
          {zones.map(z => <option key={z.zone_id} value={z.zone_id}>{z.name}</option>)}
        </select>
        <select value={filterBinType} onChange={e => setFilterBinType(e.target.value)} style={{ ...inputStyle, width: 150 }}>
          <option value="">All Bin Types</option>
          {binTypes.map(t => <option key={t.id} value={t.id}>{t.name}</option>)}
        </select>
        <select value={filterStatus} onChange={e => setFilterStatus(e.target.value)} style={{ ...inputStyle, width: 140 }}>
          <option value="">All Statuses</option>
          <option value="available">Available</option>
          <option value="occupied">Occupied</option>
          <option value="locked">Locked</option>
        </select>
        <select value={filterCapacity} onChange={e => setFilterCapacity(e.target.value)} style={{ ...inputStyle, width: 160 }}>
          <option value="">All Capacity</option>
          <option value="under50">Under 50%</option>
          <option value="50to80">50–80%</option>
          <option value="over80">Over 80%</option>
          <option value="full">Full (100%)</option>
        </select>
        {(filterZone || filterBinType || filterStatus || filterCapacity) && (
          <button onClick={() => { setFilterZone(''); setFilterBinType(''); setFilterStatus(''); setFilterCapacity(''); }} style={btnSecStyle}>Clear Filters</button>
        )}
        <span style={{ marginLeft: 'auto', fontSize: 12, color: 'var(--text-muted)', alignSelf: 'center' }}>{filtered.length} binrack{filtered.length !== 1 ? 's' : ''}</span>
      </div>

      {filtered.length === 0 ? (
        <div style={{ padding: '48px', textAlign: 'center', color: 'var(--text-muted)', background: 'var(--bg-secondary)', border: '1px solid var(--border-bright)', borderRadius: 8 }}>
          <div style={{ fontSize: 32, marginBottom: 12 }}>📍</div>
          <p>No binracks found. Add one or adjust your filters.</p>
        </div>
      ) : (
        <div style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border-bright)', borderRadius: 8, overflow: 'hidden' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
            <thead>
              <tr style={{ background: 'var(--bg-tertiary)' }}>
                {['', 'Name', 'Zone', 'Position', 'Status', 'Capacity', 'Actions'].map(h => (
                  <th key={h} style={{ padding: '10px 12px', textAlign: 'left', fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.05em', borderBottom: '1px solid var(--border-bright)' }}>{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {filtered.map(b => {
                const zone = zones.find(z => z.zone_id === b.zone_id);
                const pct = capacityPct(b);
                const isExpanded = expandedBinrack === b.binrack_id;
                return (
                  <>
                    <tr key={b.binrack_id} style={{ borderBottom: '1px solid var(--border-bright)', background: isExpanded ? 'rgba(124,58,237,0.04)' : 'transparent' }}>
                      <td style={{ padding: '10px 12px', width: 32 }}>
                        <button onClick={() => toggleExpand(b.binrack_id)} style={{ ...iconBtnStyle, fontSize: 12 }} title="Show items">
                          {isExpanded ? '▼' : '▶'}
                        </button>
                      </td>
                      <td style={{ padding: '10px 12px' }}>
                        <div style={{ fontWeight: 600, color: 'var(--text-primary)' }}>{b.name}</div>
                        {b.barcode && <div style={{ fontSize: 11, color: 'var(--text-muted)', fontFamily: 'monospace' }}>{b.barcode}</div>}
                      </td>
                      <td style={{ padding: '10px 12px' }}>
                        {zone ? (
                          <span style={{ padding: '2px 8px', borderRadius: 10, background: `${zone.colour || '#6b7280'}20`, color: zone.colour || '#6b7280', fontSize: 11, fontWeight: 600 }}>
                            {zone.name}
                          </span>
                        ) : <span style={{ color: 'var(--text-muted)', fontSize: 12 }}>—</span>}
                      </td>
                      <td style={{ padding: '10px 12px', color: 'var(--text-muted)', fontSize: 12 }}>
                        {[b.aisle, b.section, b.level, b.bin_number].filter(Boolean).join('-') || '—'}
                      </td>
                      <td style={{ padding: '10px 12px' }}>{statusBadge(b.status)}</td>
                      <td style={{ padding: '10px 12px' }}>
                        {b.capacity > 0 ? (
                          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                            <div style={{ width: 60, height: 6, background: 'var(--bg-elevated)', borderRadius: 3, overflow: 'hidden' }}>
                              <div style={{ width: `${Math.min(pct, 100)}%`, height: '100%', background: pct >= 90 ? '#ef4444' : pct >= 70 ? '#f59e0b' : '#10b981', borderRadius: 3 }} />
                            </div>
                            <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>{pct}%</span>
                          </div>
                        ) : <span style={{ color: 'var(--text-muted)', fontSize: 12 }}>—</span>}
                      </td>
                      <td style={{ padding: '10px 12px' }}>
                        <div style={{ display: 'flex', gap: 4 }}>
                          <button onClick={() => setEditBinrack(b)} style={iconBtnStyle} title="Edit">✏️</button>
                          <button onClick={() => deleteBinrack(b.binrack_id)} style={{ ...iconBtnStyle, color: '#f87171' }} title="Delete">🗑</button>
                        </div>
                      </td>
                    </tr>
                    {isExpanded && (
                      <tr key={`${b.binrack_id}-items`} style={{ background: 'rgba(124,58,237,0.03)', borderBottom: '1px solid var(--border-bright)' }}>
                        <td colSpan={7} style={{ padding: '12px 24px 16px' }}>
                          {itemsLoading ? (
                            <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading items…</div>
                          ) : binrackItems.length === 0 ? (
                            <div style={{ color: 'var(--text-muted)', fontSize: 13, fontStyle: 'italic' }}>No items currently stored in this binrack.</div>
                          ) : (
                            <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12 }}>
                              <thead>
                                <tr>
                                  <th style={{ textAlign: 'left', color: 'var(--text-muted)', fontWeight: 600, padding: '4px 8px', fontSize: 11 }}>SKU</th>
                                  <th style={{ textAlign: 'left', color: 'var(--text-muted)', fontWeight: 600, padding: '4px 8px', fontSize: 11 }}>Product</th>
                                  <th style={{ textAlign: 'right', color: 'var(--text-muted)', fontWeight: 600, padding: '4px 8px', fontSize: 11 }}>Qty</th>
                                </tr>
                              </thead>
                              <tbody>
                                {binrackItems.map(item => (
                                  <tr key={item.sku}>
                                    <td style={{ padding: '4px 8px', fontFamily: 'monospace', color: 'var(--accent-cyan)' }}>{item.sku}</td>
                                    <td style={{ padding: '4px 8px', color: 'var(--text-secondary)' }}>{item.product_name}</td>
                                    <td style={{ padding: '4px 8px', textAlign: 'right', fontWeight: 600, color: 'var(--text-primary)' }}>{item.quantity}</td>
                                  </tr>
                                ))}
                              </tbody>
                            </table>
                          )}
                        </td>
                      </tr>
                    )}
                  </>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      {/* Zone modal */}
      {editZone && (
        <ZoneEditModal
          zone={editZone === 'new' ? null : editZone}
          warehouseId={warehouseId}
          onClose={() => setEditZone(null)}
          onSaved={() => { setEditZone(null); load(); }}
        />
      )}

      {/* Binrack modal */}
      {editBinrack && (
        <BinrackEditModal
          binrack={editBinrack === 'new' ? null : editBinrack}
          zones={zones}
          binTypes={binTypes}
          storageGroups={storageGroups}
          warehouseId={warehouseId}
          onClose={() => setEditBinrack(null)}
          onSaved={() => { setEditBinrack(null); load(); }}
        />
      )}
    </div>
  );
}

interface Product {
  product_id: string;
  title: string;
  sku: string;
}

interface AdjustModalState {
  locationId: string;
  locationPath: string;
  locationName: string;
  prefilledProduct?: Product;
}

// ─── Adjustment Modal ─────────────────────────────────────────────────────────

function AdjustmentModal({
  state,
  onClose,
  onSuccess,
}: {
  state: AdjustModalState;
  onClose: () => void;
  onSuccess: () => void;
}) {
  const [product, setProduct] = useState<Product | null>(state.prefilledProduct || null);
  const [productSearch, setProductSearch] = useState('');
  const [productResults, setProductResults] = useState<Product[]>([]);
  const [direction, setDirection] = useState<'+' | '-'>('+');
  const [quantity, setQuantity] = useState('');
  const [reason, setReason] = useState('');
  const [reference, setReference] = useState('');
  const [currentStock, setCurrentStock] = useState<number | null>(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const searchTimeout = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    if (product) {
      fetchCurrentStock(product.product_id);
    }
  }, [product]);

  async function fetchCurrentStock(productId: string) {
    try {
      const res = await api(`/inventory/${productId}`);
      if (!res.ok) return;
      const data = await res.json();
      const records: { location_id: string; quantity: number }[] = data.inventory || [];
      const match = records.find(r => r.location_id === state.locationId);
      setCurrentStock(match ? match.quantity : 0);
    } catch {
      setCurrentStock(0);
    }
  };

  const searchProducts = async (q: string) => {
    if (!q.trim()) { setProductResults([]); return; }
    try {
      const res = await api(`/products?search=${encodeURIComponent(q)}&limit=10`);
      if (!res.ok) return;
      const data = await res.json();
      setProductResults(data.products || []);
    } catch {}
  };

  const handleProductInput = (v: string) => {
    setProductSearch(v);
    if (searchTimeout.current) clearTimeout(searchTimeout.current);
    searchTimeout.current = setTimeout(() => searchProducts(v), 300);
  };

  const delta = quantity ? (direction === '+' ? parseInt(quantity) : -parseInt(quantity)) : 0;
  const newStock = currentStock !== null ? currentStock + delta : null;

  const handleSubmit = async () => {
    if (!product) { setError('Select a product'); return; }
    if (!quantity || parseInt(quantity) <= 0) { setError('Enter a valid quantity'); return; }
    if (!reason.trim()) { setError('Reason is required'); return; }
    setSaving(true);
    setError('');
    try {
      const res = await api('/inventory/adjust', {
        method: 'POST',
        body: JSON.stringify({
          product_id: product.product_id,
          location_id: state.locationId,
          delta,
          reason,
          reference,
          type: 'adjustment',
        }),
      });
      const data = await res.json();
      if (!res.ok) throw new Error(data.error || 'Failed to adjust stock');
      onSuccess();
    } catch (e: any) {
      setError(e.message);
    } finally {
      setSaving(false);
    }
  };

  return (
    <div style={overlayStyle} onClick={e => e.target === e.currentTarget && onClose()}>
      <div style={modalStyle}>
        <div style={modalHeaderStyle}>
          <h2 style={{ margin: 0, fontSize: '18px', fontWeight: 600 }}>Adjust Stock</h2>
          <button onClick={onClose} style={closeBtnStyle}>✕</button>
        </div>

        <div style={{ padding: '24px', display: 'flex', flexDirection: 'column', gap: '16px' }}>
          <div style={fieldStyle}>
            <label style={labelStyle}>Location</label>
            <div style={{ fontSize: '14px', color: 'var(--text-muted)' }}>{state.locationPath}</div>
          </div>

          {/* Product selector */}
          <div style={fieldStyle}>
            <label style={labelStyle}>Product</label>
            {product ? (
              <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                <span style={{ fontSize: '14px', color: 'var(--text-primary)' }}>
                  {product.title} <span style={{ color: 'var(--text-muted)' }}>({product.sku})</span>
                </span>
                {!state.prefilledProduct && (
                  <button onClick={() => { setProduct(null); setCurrentStock(null); }} style={smallBtnStyle}>
                    ✕ Change
                  </button>
                )}
              </div>
            ) : (
              <div style={{ position: 'relative' }}>
                <input
                  value={productSearch}
                  onChange={e => handleProductInput(e.target.value)}
                  placeholder="Search by name or SKU..."
                  style={inputStyle}
                />
                {productResults.length > 0 && (
                  <div style={dropdownStyle}>
                    {productResults.map(p => (
                      <div
                        key={p.product_id}
                        onClick={() => { setProduct(p); setProductSearch(''); setProductResults([]); }}
                        style={dropdownItemStyle}
                      >
                        {p.title} <span style={{ color: 'var(--text-muted)', fontSize: '12px' }}>({p.sku})</span>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            )}
          </div>

          {/* Delta input */}
          <div style={fieldStyle}>
            <label style={labelStyle}>Quantity Change</label>
            <div style={{ display: 'flex', gap: '8px', alignItems: 'center' }}>
              <button
                onClick={() => setDirection('+')}
                style={{ ...dirBtnStyle, ...(direction === '+' ? dirBtnActiveStyle : {}) }}
              >＋ Add</button>
              <button
                onClick={() => setDirection('-')}
                style={{ ...dirBtnStyle, ...(direction === '-' ? dirBtnRemoveStyle : {}) }}
              >－ Remove</button>
              <input
                type="number"
                min="1"
                value={quantity}
                onChange={e => setQuantity(e.target.value.replace(/[^0-9]/g, ''))}
                placeholder="Qty"
                style={{ ...inputStyle, width: '100px' }}
              />
            </div>
            {currentStock !== null && quantity && (
              <div style={{ fontSize: '13px', color: 'var(--text-muted)', marginTop: '6px' }}>
                Current: <strong style={{ color: 'var(--text-primary)' }}>{currentStock}</strong>
                {' → '}
                <strong style={{ color: newStock! < 0 ? '#f87171' : 'var(--accent-cyan)' }}>
                  {newStock}
                </strong>
              </div>
            )}
          </div>

          <div style={fieldStyle}>
            <label style={labelStyle}>Reason <span style={{ color: '#f87171' }}>*</span></label>
            <input
              value={reason}
              onChange={e => setReason(e.target.value)}
              placeholder="e.g. Damaged in transit, Stock count correction"
              style={inputStyle}
            />
          </div>

          <div style={fieldStyle}>
            <label style={labelStyle}>Reference <span style={{ color: 'var(--text-muted)', fontWeight: 400 }}>(optional)</span></label>
            <input
              value={reference}
              onChange={e => setReference(e.target.value)}
              placeholder="PO number, order ID, etc."
              style={inputStyle}
            />
          </div>

          {error && <div style={errorStyle}>{error}</div>}

          <div style={{ display: 'flex', gap: '12px', justifyContent: 'flex-end' }}>
            <button onClick={onClose} style={btnSecStyle}>Cancel</button>
            <button onClick={handleSubmit} disabled={saving} style={btnPrimaryStyle}>
              {saving ? 'Saving...' : 'Apply Adjustment'}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

// ─── Location Tree Node ───────────────────────────────────────────────────────

function LocationNode({
  loc,
  onAdd,
  onDelete,
  onRename,
  onAdjust,
}: {
  loc: WarehouseLocation;
  onAdd: (parentId: string, parentDepth: number) => void;
  onDelete: (locationId: string) => void;
  onRename: (locationId: string, newName: string) => void;
  onAdjust: (loc: WarehouseLocation) => void;
}) {
  const [expanded, setExpanded] = useState(true);
  const [renaming, setRenaming] = useState(false);
  const [renameVal, setRenameVal] = useState(loc.name);
  const hasChildren = (loc.children?.length ?? 0) > 0;

  const indent = loc.depth * 20;

  const handleRenameSubmit = () => {
    if (renameVal.trim() && renameVal !== loc.name) {
      onRename(loc.location_id, renameVal.trim());
    }
    setRenaming(false);
  };

  return (
    <div>
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: '8px',
          padding: '8px 12px',
          borderBottom: '1px solid var(--border-bright)',
          background: loc.depth === 0 ? 'var(--bg-tertiary)' : 'transparent',
          borderLeft: loc.depth > 0 ? '2px solid var(--border-bright)' : 'none',
          marginLeft: loc.depth > 0 ? `${indent}px` : '0',
          paddingLeft: loc.depth > 0 ? '12px' : `${12 + indent}px`,
        }}
      >
        {/* Expand toggle */}
        <button
          onClick={() => setExpanded(e => !e)}
          style={{
            background: 'none',
            border: 'none',
            color: hasChildren ? 'var(--text-muted)' : 'transparent',
            cursor: hasChildren ? 'pointer' : 'default',
            width: '20px',
            fontSize: '12px',
            padding: 0,
            flexShrink: 0,
          }}
        >
          {hasChildren ? (expanded ? '▼' : '▶') : '·'}
        </button>

        {/* Name */}
        {renaming ? (
          <input
            autoFocus
            value={renameVal}
            onChange={e => setRenameVal(e.target.value)}
            onBlur={handleRenameSubmit}
            onKeyDown={e => { if (e.key === 'Enter') handleRenameSubmit(); if (e.key === 'Escape') setRenaming(false); }}
            style={{ ...inputStyle, fontSize: '14px', padding: '2px 8px', width: '200px' }}
          />
        ) : (
          <span
            onDoubleClick={() => setRenaming(true)}
            style={{
              fontSize: '14px',
              fontWeight: loc.depth === 0 ? 600 : 400,
              color: 'var(--text-primary)',
              flex: 1,
              cursor: 'text',
              userSelect: 'none',
            }}
            title="Double-click to rename"
          >
            {loc.name}
          </span>
        )}

        {/* Barcode */}
        {loc.barcode && (
          <span style={{ fontSize: '11px', color: 'var(--text-muted)', fontFamily: 'monospace' }}>
            [{loc.barcode}]
          </span>
        )}

        {/* Stock badge (leaf nodes only) */}
        {loc.is_leaf && (
          <span style={{
            fontSize: '12px',
            background: 'var(--bg-elevated)',
            color: (loc.stock ?? 0) > 0 ? 'var(--accent-cyan)' : 'var(--text-muted)',
            padding: '2px 8px',
            borderRadius: '12px',
            fontWeight: 600,
            minWidth: '40px',
            textAlign: 'center',
          }}>
            {loc.stock ?? 0}
          </span>
        )}

        {/* Actions */}
        <div style={{ display: 'flex', gap: '4px', marginLeft: 'auto' }}>
          {loc.is_leaf && (
            <button
              onClick={() => onAdjust(loc)}
              title="Adjust stock"
              style={iconBtnStyle}
            >⚙</button>
          )}
          <button
            onClick={() => onAdd(loc.location_id, loc.depth)}
            title="Add child location"
            style={iconBtnStyle}
          >＋</button>
          <button
            onClick={() => onDelete(loc.location_id)}
            disabled={hasChildren || (loc.stock ?? 0) > 0}
            title={hasChildren ? 'Has children' : (loc.stock ?? 0) > 0 ? 'Has stock' : 'Delete'}
            style={{
              ...iconBtnStyle,
              color: (hasChildren || (loc.stock ?? 0) > 0) ? 'var(--border-bright)' : '#f87171',
              cursor: (hasChildren || (loc.stock ?? 0) > 0) ? 'not-allowed' : 'pointer',
            }}
          >🗑</button>
        </div>
      </div>

      {expanded && hasChildren && (
        <div style={{ borderLeft: '2px solid var(--border-bright)', marginLeft: `${12 + indent + 10}px` }}>
          {loc.children!.map(child => (
            <LocationNode
              key={child.location_id}
              loc={child}
              onAdd={onAdd}
              onDelete={onDelete}
              onRename={onRename}
              onAdjust={onAdjust}
            />
          ))}
        </div>
      )}
    </div>
  );
}

// ─── Add Location Modal ───────────────────────────────────────────────────────

function AddLocationModal({
  sourceId,
  parentId,
  onClose,
  onSuccess,
}: {
  sourceId: string;
  parentId: string;
  onClose: () => void;
  onSuccess: () => void;
}) {
  const [name, setName] = useState('');
  const [barcode, setBarcode] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  const handleSubmit = async () => {
    if (!name.trim()) { setError('Name is required'); return; }
    setSaving(true);
    setError('');
    try {
      const res = await api('/locations', {
        method: 'POST',
        body: JSON.stringify({ name: name.trim(), source_id: sourceId, parent_id: parentId, barcode }),
      });
      const data = await res.json();
      if (!res.ok) throw new Error(data.error || 'Failed to create location');
      onSuccess();
    } catch (e: any) {
      setError(e.message);
    } finally {
      setSaving(false);
    }
  };

  return (
    <div style={overlayStyle} onClick={e => e.target === e.currentTarget && onClose()}>
      <div style={{ ...modalStyle, maxWidth: '400px' }}>
        <div style={modalHeaderStyle}>
          <h2 style={{ margin: 0, fontSize: '18px', fontWeight: 600 }}>Add Location</h2>
          <button onClick={onClose} style={closeBtnStyle}>✕</button>
        </div>
        <div style={{ padding: '24px', display: 'flex', flexDirection: 'column', gap: '16px' }}>
          <div style={fieldStyle}>
            <label style={labelStyle}>Name <span style={{ color: '#f87171' }}>*</span></label>
            <input
              autoFocus
              value={name}
              onChange={e => setName(e.target.value)}
              onKeyDown={e => e.key === 'Enter' && handleSubmit()}
              placeholder="e.g. Bay 1, Shelf A, Bin 01"
              style={inputStyle}
            />
          </div>
          <div style={fieldStyle}>
            <label style={labelStyle}>Barcode <span style={{ color: 'var(--text-muted)', fontWeight: 400 }}>(optional)</span></label>
            <input
              value={barcode}
              onChange={e => setBarcode(e.target.value)}
              placeholder="Scan or type barcode"
              style={inputStyle}
            />
          </div>
          {error && <div style={errorStyle}>{error}</div>}
          <div style={{ display: 'flex', gap: '12px', justifyContent: 'flex-end' }}>
            <button onClick={onClose} style={btnSecStyle}>Cancel</button>
            <button onClick={handleSubmit} disabled={saving} style={btnPrimaryStyle}>
              {saving ? 'Creating...' : 'Create Location'}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

// ─── Main Page ────────────────────────────────────────────────────────────────

export default function WarehouseLocations() {
  const { sourceId } = useParams<{ sourceId: string }>();
  const navigate = useNavigate();
  const [tab, setTab] = useState<'locations' | 'binracks' | 'rules'>('locations');
  const [source, setSource] = useState<FulfilmentSource | null>(null);
  const [allSources, setAllSources] = useState<FulfilmentSource[]>([]);
  const [tree, setTree] = useState<WarehouseLocation[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [addModal, setAddModal] = useState<{ parentId: string } | null>(null);
  const [adjustModal, setAdjustModal] = useState<AdjustModalState | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      if (sourceId) {
        const srcRes = await api(`/fulfilment-sources/${sourceId}`);
        if (srcRes.ok) {
          const d = await srcRes.json();
          setSource(d.source || d);
        }
      }
      const [locRes, srcListRes] = await Promise.all([
        api(`/locations${sourceId ? `?source_id=${sourceId}` : ''}`),
        api('/fulfilment-sources'),
      ]);
      if (!locRes.ok) throw new Error('Failed to load locations');
      const locData = await locRes.json();
      setTree(locData.locations || []);
      if (srcListRes.ok) {
        const srcData = await srcListRes.json();
        setAllSources(srcData.sources || []);
      }
    } catch (e: any) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }, [sourceId]);

  useEffect(() => { load(); }, [load]);

  const handleDelete = async (locationId: string) => {
    if (!window.confirm('Delete this location?')) return;
    try {
      const res = await api(`/locations/${locationId}`, { method: 'DELETE' });
      if (!res.ok) {
        const d = await res.json();
        alert(d.error || 'Failed to delete');
        return;
      }
      load();
    } catch (e: any) {
      alert(e.message);
    }
  };

  const handleRename = async (locationId: string, newName: string) => {
    try {
      await api(`/locations/${locationId}`, {
        method: 'PUT',
        body: JSON.stringify({ name: newName }),
      });
      load();
    } catch {}
  };

  if (loading) {
    return (
      <div className="page">
        <div className="loading-state"><div className="spinner" /><p>Loading locations...</p></div>
      </div>
    );
  }

  return (
    <div className="page">
      {/* Header */}
      <div className="page-header">
        <div>
          <div style={{ marginBottom: '12px' }}>
            <Link to="/fulfilment-sources" style={{ color: 'var(--text-muted)', fontSize: '14px' }}>
              ← Back to Fulfilment Sources
            </Link>
          </div>
          <h1 className="page-title">
            {source ? `${source.name} — Locations` : 'Warehouse Locations'}
          </h1>
          <p className="page-subtitle">Manage the location tree for stock assignment. Double-click any name to rename.</p>
        </div>
        {tab === 'locations' && (
          <div className="page-actions">
            <button
              className="btn btn-primary"
              onClick={() => setAddModal({ parentId: '' })}
            >
              ＋ Add Root Location
            </button>
          </div>
        )}
      </div>

      {/* Tab nav */}
      <div style={{ display: 'flex', gap: 0, marginBottom: '24px', borderBottom: '1px solid var(--border-bright)' }}>
        {([
          { key: 'locations', label: '📦 Locations' },
          { key: 'binracks', label: '📍 Binracks & Zones' },
          { key: 'rules', label: '📋 Fulfilment Rules' },
        ] as const).map(t => (
          <button key={t.key} onClick={() => setTab(t.key)} style={{
            padding: '10px 20px', background: 'none', border: 'none',
            borderBottom: tab === t.key ? '2px solid var(--primary)' : '2px solid transparent',
            color: tab === t.key ? 'var(--primary)' : 'var(--text-muted)',
            cursor: 'pointer', fontSize: '14px', fontWeight: tab === t.key ? 600 : 400, marginBottom: -1,
          }}>{t.label}</button>
        ))}
      </div>

      {error && <div style={errorStyle}>{error}</div>}

      {tab === 'rules' ? (
        <AllocationRulesTab warehouses={allSources} />
      ) : tab === 'binracks' ? (
        <BinracksTab warehouseId={sourceId || ''} />
      ) : (
        <>
          {/* Tree */}
          <div style={{
            background: 'var(--bg-secondary)',
            border: '1px solid var(--border-bright)',
            borderRadius: '8px',
            overflow: 'hidden',
          }}>
            {/* Legend row */}
            <div style={{
              display: 'flex',
              padding: '8px 12px',
              background: 'var(--bg-tertiary)',
              borderBottom: '1px solid var(--border-bright)',
              fontSize: '11px',
              color: 'var(--text-muted)',
              fontWeight: 600,
              textTransform: 'uppercase',
              letterSpacing: '0.05em',
              gap: '8px',
            }}>
              <span style={{ flex: 1 }}>Name</span>
              <span>Barcode</span>
              <span style={{ minWidth: '60px', textAlign: 'center' }}>Stock</span>
              <span style={{ width: '80px' }}>Actions</span>
            </div>

            {tree.length === 0 ? (
              <div style={{ padding: '48px', textAlign: 'center', color: 'var(--text-muted)' }}>
                <div style={{ fontSize: '32px', marginBottom: '12px' }}>📦</div>
                <p>No locations yet. Add a root location to get started.</p>
                <button
                  onClick={() => setAddModal({ parentId: '' })}
                  style={{ ...btnPrimaryStyle, marginTop: '12px' }}
                >
                  ＋ Add Root Location
                </button>
              </div>
            ) : (
              tree.map(loc => (
                <LocationNode
                  key={loc.location_id}
                  loc={loc}
                  onAdd={parentId => setAddModal({ parentId })}
                  onDelete={handleDelete}
                  onRename={handleRename}
                  onAdjust={loc => setAdjustModal({
                    locationId: loc.location_id,
                    locationPath: loc.path,
                    locationName: loc.name,
                  })}
                />
              ))
            )}
          </div>
        </>
      )}

      {/* Add Location Modal */}
      {addModal && sourceId && (
        <AddLocationModal
          sourceId={sourceId}
          parentId={addModal.parentId}
          onClose={() => setAddModal(null)}
          onSuccess={() => { setAddModal(null); load(); }}
        />
      )}

      {/* Adjustment Modal */}
      {adjustModal && (
        <AdjustmentModal
          state={adjustModal}
          onClose={() => setAdjustModal(null)}
          onSuccess={() => { setAdjustModal(null); load(); }}
        />
      )}
    </div>
  );
}

// ─── Shared Styles ────────────────────────────────────────────────────────────

const overlayStyle: React.CSSProperties = {
  position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.6)',
  display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000,
};
const modalStyle: React.CSSProperties = {
  background: 'var(--bg-secondary)', border: '1px solid var(--border-bright)',
  borderRadius: '12px', width: '100%', maxWidth: '540px', maxHeight: '90vh',
  overflow: 'auto', boxShadow: '0 20px 60px rgba(0,0,0,0.5)',
};
const modalHeaderStyle: React.CSSProperties = {
  display: 'flex', alignItems: 'center', justifyContent: 'space-between',
  padding: '20px 24px', borderBottom: '1px solid var(--border-bright)',
};
const closeBtnStyle: React.CSSProperties = {
  background: 'none', border: 'none', color: 'var(--text-muted)',
  fontSize: '18px', cursor: 'pointer', padding: '4px',
};
const fieldStyle: React.CSSProperties = { display: 'flex', flexDirection: 'column', gap: '6px' };
const labelStyle: React.CSSProperties = { fontSize: '12px', fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.05em' };
const inputStyle: React.CSSProperties = {
  background: 'var(--bg-tertiary)', border: '1px solid var(--border-bright)',
  borderRadius: '6px', padding: '8px 12px', color: 'var(--text-primary)', fontSize: '14px', width: '100%',
  boxSizing: 'border-box',
};
const errorStyle: React.CSSProperties = {
  background: 'rgba(248,113,113,0.1)', border: '1px solid rgba(248,113,113,0.3)',
  borderRadius: '6px', padding: '10px 14px', color: '#f87171', fontSize: '14px',
};
const btnPrimaryStyle: React.CSSProperties = {
  background: 'var(--primary)', border: 'none', borderRadius: '6px',
  padding: '8px 16px', color: '#fff', fontSize: '14px', cursor: 'pointer', fontWeight: 500,
};
const btnSecStyle: React.CSSProperties = {
  background: 'var(--bg-tertiary)', border: '1px solid var(--border-bright)',
  borderRadius: '6px', padding: '8px 16px', color: 'var(--text-primary)', fontSize: '14px', cursor: 'pointer',
};
const iconBtnStyle: React.CSSProperties = {
  background: 'none', border: 'none', color: 'var(--text-muted)', cursor: 'pointer',
  fontSize: '16px', padding: '2px 6px', borderRadius: '4px',
};
const smallBtnStyle: React.CSSProperties = {
  background: 'none', border: '1px solid var(--border-bright)', borderRadius: '4px',
  padding: '2px 8px', color: 'var(--text-muted)', fontSize: '12px', cursor: 'pointer',
};
const dropdownStyle: React.CSSProperties = {
  position: 'absolute', top: '100%', left: 0, right: 0, zIndex: 10,
  background: 'var(--bg-elevated)', border: '1px solid var(--border-bright)', borderRadius: '6px',
  boxShadow: '0 8px 24px rgba(0,0,0,0.4)', maxHeight: '200px', overflowY: 'auto',
};
const dropdownItemStyle: React.CSSProperties = {
  padding: '10px 14px', cursor: 'pointer', fontSize: '14px', color: 'var(--text-primary)',
  borderBottom: '1px solid var(--border-bright)',
};
const dirBtnStyle: React.CSSProperties = {
  padding: '6px 14px', borderRadius: '6px', border: '1px solid var(--border-bright)',
  background: 'var(--bg-tertiary)', color: 'var(--text-muted)', cursor: 'pointer', fontSize: '14px',
};
const dirBtnActiveStyle: React.CSSProperties = {
  background: 'rgba(6,182,212,0.15)', borderColor: 'var(--accent-cyan)', color: 'var(--accent-cyan)',
};
const dirBtnRemoveStyle: React.CSSProperties = {
  background: 'rgba(248,113,113,0.15)', borderColor: '#f87171', color: '#f87171',
};

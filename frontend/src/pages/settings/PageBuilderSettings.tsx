import { useState, useEffect, useCallback } from 'react';
import { Link } from 'react-router-dom';
import {
  Plus, Trash2, Edit2, ToggleLeft, ToggleRight, Mail,
  ChevronDown, ChevronUp, RefreshCw,
} from 'lucide-react';
import '../../components/SettingsLayout.css';
import PageBuilder from '../../components/pagebuilder/pagebuilder/components/PageBuilder';

// ─── Config ──────────────────────────────────────────────────────────────────

const API_BASE: string =
  (import.meta as any).env?.VITE_API_URL ||
  'https://marketmate-api-487246736287.us-central1.run.app/api/v1';

function getApiHeaders(tenantId: string): Record<string, string> {
  const token = localStorage.getItem('auth_token') || '';
  return {
    'Content-Type': 'application/json',
    'X-Tenant-Id': tenantId,
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
  };
}

function getActiveTenantId(): string {
  try {
    const raw = localStorage.getItem('activeTenant') || sessionStorage.getItem('activeTenant') || '';
    if (raw) {
      const parsed = JSON.parse(raw);
      return parsed?.tenantId || parsed?.id || '';
    }
  } catch { /* ignore */ }
  return localStorage.getItem('tenantId') || '';
}

// ─── Types ────────────────────────────────────────────────────────────────────

interface Template {
  id: string;
  name: string;
  type: string;
  output_format: string;
  version: number;
  is_default: boolean;
  enabled: boolean;
  trigger_type?: string;
  trigger_event?: string;
  updated_at: string;
  created_at: string;
}

const TEMPLATE_TYPES = [
  { value: 'invoice',       label: 'Invoice' },
  { value: 'packing_slip',  label: 'Packing Slip' },
  { value: 'postage_label', label: 'Postage Label' },
  { value: 'email',         label: 'Email' },
  { value: 'ebay_listing',  label: 'eBay Listing' },
  { value: 'custom',        label: 'Custom' },
];

const TRIGGER_EVENTS = [
  { value: 'order_confirmation',    label: 'Order Confirmation' },
  { value: 'order_despatch',        label: 'Order Despatch' },
  { value: 'return_confirmation',   label: 'Return Confirmation' },
  { value: 'refund_confirmation',   label: 'Refund Confirmation' },
  { value: 'exchange_confirmation', label: 'Exchange Confirmation' },
];

// ─── Component ────────────────────────────────────────────────────────────────

export default function PageBuilderSettings() {
  const [view, setView] = useState<'list' | 'builder'>('list');
  const [editTemplateId, setEditTemplateId] = useState<string | null>(null);
  const [templates, setTemplates] = useState<Template[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [typeFilter, setTypeFilter] = useState('');
  const [togglingId, setTogglingId] = useState<string | null>(null);
  const [deletingId, setDeletingId] = useState<string | null>(null);

  const [showNewForm, setShowNewForm] = useState(false);
  const [newName, setNewName] = useState('');
  const [newType, setNewType] = useState('email');
  const [newTriggerType, setNewTriggerType] = useState('manual');
  const [newTriggerEvent, setNewTriggerEvent] = useState('order_confirmation');
  const [creating, setCreating] = useState(false);

  const tenantId = getActiveTenantId();

  const fetchTemplates = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const url = typeFilter
        ? `${API_BASE}/templates?type=${typeFilter}`
        : `${API_BASE}/templates`;
      const res = await fetch(url, { headers: getApiHeaders(tenantId) });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = await res.json();
      setTemplates(data.templates || []);
    } catch (e: any) {
      setError('Failed to load templates: ' + e.message);
    } finally {
      setLoading(false);
    }
  }, [tenantId, typeFilter]);

  useEffect(() => {
    if (view === 'list') fetchTemplates();
  }, [view, fetchTemplates]);

  const handleToggle = async (tpl: Template, e: React.MouseEvent) => {
    e.stopPropagation();
    setTogglingId(tpl.id);
    try {
      await fetch(`${API_BASE}/templates/${tpl.id}/toggle`, {
        method: 'PATCH',
        headers: getApiHeaders(tenantId),
        body: JSON.stringify({ enabled: !tpl.enabled }),
      });
      setTemplates(prev => prev.map(t => t.id === tpl.id ? { ...t, enabled: !t.enabled } : t));
    } catch { /* ignore */ } finally {
      setTogglingId(null);
    }
  };

  const handleDelete = async (tpl: Template, e: React.MouseEvent) => {
    e.stopPropagation();
    if (!window.confirm(`Delete template "${tpl.name}"? This cannot be undone.`)) return;
    setDeletingId(tpl.id);
    try {
      await fetch(`${API_BASE}/templates/${tpl.id}`, {
        method: 'DELETE',
        headers: getApiHeaders(tenantId),
      });
      setTemplates(prev => prev.filter(t => t.id !== tpl.id));
    } catch { /* ignore */ } finally {
      setDeletingId(null);
    }
  };

  const handleCreate = async () => {
    if (!newName.trim()) return;
    setCreating(true);
    try {
      const payload: any = {
        name: newName.trim(),
        type: newType,
        output_format: newType === 'email' || newType === 'ebay_listing' ? 'HTML' : 'PDF',
        enabled: true,
        version: 1,
        blocks: [],
        canvas: { width: 595, height: 842, unit: 'px', backgroundColor: '#ffffff' },
        grid: { showRulers: false, showGrid: false, snapEnabled: false, gridSpacing: 10, gridStyle: 'lines' },
      };
      if (newType === 'email') {
        payload.trigger_type = newTriggerType;
        if (newTriggerType === 'automated') {
          payload.trigger_event = newTriggerEvent;
        }
      }
      const res = await fetch(`${API_BASE}/templates`, {
        method: 'POST',
        headers: getApiHeaders(tenantId),
        body: JSON.stringify(payload),
      });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = await res.json();
      const created = data.template;
      if (created) {
        setTemplates(prev => [created, ...prev]);
        setShowNewForm(false);
        setNewName('');
        setNewType('email');
        setNewTriggerType('manual');
        setNewTriggerEvent('order_confirmation');
      }
    } catch (e: any) {
      setError('Failed to create template: ' + e.message);
    } finally {
      setCreating(false);
    }
  };

  const openBuilder = (id: string | null = null) => {
    setEditTemplateId(id);
    setView('builder');
  };

  const triggerEventLabel = (event?: string) =>
    TRIGGER_EVENTS.find(e => e.value === event)?.label || event || '—';

  const typeLabel = (t: string) =>
    TEMPLATE_TYPES.find(x => x.value === t)?.label || t;

  // ── Builder view ──────────────────────────────────────────────────────────
  if (view === 'builder') {
    return (
      <div style={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
        <div style={{ padding: '12px 32px', borderBottom: '1px solid var(--border)', background: 'var(--bg-secondary)', flexShrink: 0 }}>
          <div className="settings-breadcrumb">
            <Link to="/settings">Settings</Link>
            <span className="settings-breadcrumb-sep">›</span>
            <button
              onClick={() => { setView('list'); setEditTemplateId(null); }}
              style={{ background: 'none', border: 'none', cursor: 'pointer', color: 'var(--primary)', fontSize: 13, padding: 0 }}
            >
              Page Builder
            </button>
            <span className="settings-breadcrumb-sep">›</span>
            <span className="settings-breadcrumb-current">{editTemplateId ? 'Edit Template' : 'New Template'}</span>
          </div>
        </div>
        <div style={{ flex: 1, overflow: 'hidden' }}>
          <PageBuilder />
        </div>
      </div>
    );
  }

  // ── List view ─────────────────────────────────────────────────────────────
  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', overflowY: 'auto' }}>
      <div style={{ padding: '20px 32px 0', borderBottom: '1px solid var(--border)', background: 'var(--bg-secondary)', flexShrink: 0 }}>
        <div className="settings-breadcrumb">
          <Link to="/settings">Settings</Link>
          <span className="settings-breadcrumb-sep">›</span>
          <span className="settings-breadcrumb-current">Page Builder</span>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '16px 0 12px' }}>
          <h2 style={{ margin: 0, fontSize: 20, fontWeight: 700 }}>Email &amp; Document Templates</h2>
          <div style={{ display: 'flex', gap: 8 }}>
            <button
              onClick={() => fetchTemplates()}
              style={{ display: 'flex', alignItems: 'center', gap: 6, padding: '8px 14px', background: 'var(--bg-tertiary)', border: '1px solid var(--border)', borderRadius: 8, cursor: 'pointer', fontSize: 13 }}
            >
              <RefreshCw size={14} /> Refresh
            </button>
            <button
              onClick={() => setShowNewForm(v => !v)}
              style={{ display: 'flex', alignItems: 'center', gap: 6, padding: '8px 14px', background: 'var(--primary)', color: '#fff', border: 'none', borderRadius: 8, cursor: 'pointer', fontSize: 13, fontWeight: 600 }}
            >
              <Plus size={14} /> New Template
              {showNewForm ? <ChevronUp size={13} /> : <ChevronDown size={13} />}
            </button>
          </div>
        </div>
      </div>

      <div style={{ padding: '24px 32px', maxWidth: 1100 }}>

        {showNewForm && (
          <div style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 10, padding: 20, marginBottom: 24 }}>
            <div style={{ fontWeight: 700, fontSize: 15, marginBottom: 14 }}>Create New Template</div>
            <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', alignItems: 'flex-end' }}>
              <div style={{ flex: '2 1 200px' }}>
                <label style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-muted)', display: 'block', marginBottom: 4 }}>Template Name *</label>
                <input
                  value={newName}
                  onChange={e => setNewName(e.target.value)}
                  placeholder="e.g. Order Confirmation Email"
                  style={{ width: '100%', padding: '8px 10px', borderRadius: 7, border: '1px solid var(--border)', background: 'var(--bg-primary)', fontSize: 13 }}
                />
              </div>
              <div style={{ flex: '1 1 140px' }}>
                <label style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-muted)', display: 'block', marginBottom: 4 }}>Type</label>
                <select
                  value={newType}
                  onChange={e => setNewType(e.target.value)}
                  style={{ width: '100%', padding: '8px 10px', borderRadius: 7, border: '1px solid var(--border)', background: 'var(--bg-primary)', fontSize: 13 }}
                >
                  {TEMPLATE_TYPES.map(t => <option key={t.value} value={t.value}>{t.label}</option>)}
                </select>
              </div>
              {newType === 'email' && (
                <>
                  <div style={{ flex: '1 1 130px' }}>
                    <label style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-muted)', display: 'block', marginBottom: 4 }}>Trigger Type</label>
                    <select
                      value={newTriggerType}
                      onChange={e => setNewTriggerType(e.target.value)}
                      style={{ width: '100%', padding: '8px 10px', borderRadius: 7, border: '1px solid var(--border)', background: 'var(--bg-primary)', fontSize: 13 }}
                    >
                      <option value="manual">Manual</option>
                      <option value="automated">Automated</option>
                    </select>
                  </div>
                  {newTriggerType === 'automated' && (
                    <div style={{ flex: '1 1 180px' }}>
                      <label style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-muted)', display: 'block', marginBottom: 4 }}>Trigger Event</label>
                      <select
                        value={newTriggerEvent}
                        onChange={e => setNewTriggerEvent(e.target.value)}
                        style={{ width: '100%', padding: '8px 10px', borderRadius: 7, border: '1px solid var(--border)', background: 'var(--bg-primary)', fontSize: 13 }}
                      >
                        {TRIGGER_EVENTS.map(ev => <option key={ev.value} value={ev.value}>{ev.label}</option>)}
                      </select>
                    </div>
                  )}
                </>
              )}
              <div style={{ display: 'flex', gap: 8 }}>
                <button
                  onClick={handleCreate}
                  disabled={creating || !newName.trim()}
                  style={{ padding: '8px 18px', background: 'var(--primary)', color: '#fff', border: 'none', borderRadius: 7, cursor: 'pointer', fontSize: 13, fontWeight: 600, opacity: (!newName.trim() || creating) ? 0.6 : 1 }}
                >
                  {creating ? 'Creating…' : 'Create'}
                </button>
                <button
                  onClick={() => setShowNewForm(false)}
                  style={{ padding: '8px 14px', background: 'var(--bg-tertiary)', border: '1px solid var(--border)', borderRadius: 7, cursor: 'pointer', fontSize: 13 }}
                >
                  Cancel
                </button>
              </div>
            </div>
          </div>
        )}

        {/* Filters */}
        <div style={{ display: 'flex', gap: 8, marginBottom: 16, alignItems: 'center', flexWrap: 'wrap' }}>
          <span style={{ fontSize: 13, color: 'var(--text-muted)', marginRight: 4 }}>Filter:</span>
          {[{ value: '', label: 'All' }, ...TEMPLATE_TYPES].map(t => (
            <button
              key={t.value}
              onClick={() => setTypeFilter(t.value)}
              style={{
                padding: '5px 12px', borderRadius: 20, fontSize: 12, fontWeight: 600, cursor: 'pointer',
                background: typeFilter === t.value ? 'var(--primary)' : 'var(--bg-secondary)',
                color: typeFilter === t.value ? '#fff' : 'var(--text-secondary)',
                border: `1px solid ${typeFilter === t.value ? 'var(--primary)' : 'var(--border)'}`,
              }}
            >
              {t.label}
            </button>
          ))}
        </div>

        {error && (
          <div style={{ background: 'rgba(239,68,68,0.08)', color: 'var(--danger,#ef4444)', border: '1px solid rgba(239,68,68,0.2)', borderRadius: 8, padding: '10px 14px', marginBottom: 16, fontSize: 13 }}>
            {error}
          </div>
        )}

        {loading ? (
          <div style={{ textAlign: 'center', padding: 40, color: 'var(--text-muted)' }}>Loading templates…</div>
        ) : templates.length === 0 ? (
          <div style={{ textAlign: 'center', padding: 48, color: 'var(--text-muted)', background: 'var(--bg-secondary)', borderRadius: 12, border: '1px dashed var(--border)' }}>
            <Mail size={32} style={{ marginBottom: 12, opacity: 0.4 }} />
            <div style={{ fontWeight: 600, marginBottom: 6 }}>No templates yet</div>
            <div style={{ fontSize: 13 }}>Create your first template to get started.</div>
          </div>
        ) : (
          <div style={{ border: '1px solid var(--border)', borderRadius: 10, overflow: 'hidden' }}>
            {/* Header */}
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 110px 110px 170px 80px 100px', padding: '10px 16px', background: 'var(--bg-secondary)', fontSize: 11, fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--text-muted)' }}>
              <span>Name</span>
              <span>Type</span>
              <span>Trigger</span>
              <span>Trigger Event</span>
              <span>Version</span>
              <span style={{ textAlign: 'right' }}>Actions</span>
            </div>

            {templates.map(tpl => (
              <div
                key={tpl.id}
                style={{
                  display: 'grid', gridTemplateColumns: '1fr 110px 110px 170px 80px 100px',
                  padding: '12px 16px', background: 'var(--bg-primary)',
                  borderTop: '1px solid var(--border)', alignItems: 'center',
                  opacity: tpl.enabled ? 1 : 0.65,
                }}
              >
                <div>
                  <span
                    style={{ fontWeight: 600, fontSize: 14, cursor: 'pointer', color: 'var(--text-primary)' }}
                    onClick={() => openBuilder(tpl.id)}
                  >
                    {tpl.name}
                  </span>
                  {tpl.is_default && (
                    <span style={{ marginLeft: 8, fontSize: 10, fontWeight: 700, background: 'var(--primary)', color: '#fff', padding: '1px 7px', borderRadius: 10 }}>DEFAULT</span>
                  )}
                  {!tpl.enabled && (
                    <span style={{ marginLeft: 8, fontSize: 10, fontWeight: 700, background: 'var(--bg-tertiary)', color: 'var(--text-muted)', padding: '1px 7px', borderRadius: 10, border: '1px solid var(--border)' }}>DISABLED</span>
                  )}
                </div>

                <span style={{ fontSize: 12, color: 'var(--text-secondary)' }}>{typeLabel(tpl.type)}</span>

                <span style={{ fontSize: 12, color: 'var(--text-secondary)', textTransform: 'capitalize' }}>
                  {tpl.type === 'email' ? (tpl.trigger_type || 'manual') : '—'}
                </span>

                <span style={{ fontSize: 12, color: 'var(--text-secondary)' }}>
                  {tpl.type === 'email' && tpl.trigger_type === 'automated'
                    ? triggerEventLabel(tpl.trigger_event)
                    : '—'}
                </span>

                <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>v{tpl.version}</span>

                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'flex-end', gap: 2 }}>
                  <button
                    onClick={e => handleToggle(tpl, e)}
                    disabled={togglingId === tpl.id}
                    title={tpl.enabled ? 'Disable template' : 'Enable template'}
                    style={{ background: 'none', border: 'none', cursor: 'pointer', padding: 4, display: 'flex', alignItems: 'center', color: tpl.enabled ? 'var(--primary)' : 'var(--text-muted)' }}
                  >
                    {tpl.enabled ? <ToggleRight size={20} /> : <ToggleLeft size={20} />}
                  </button>
                  <button
                    onClick={() => openBuilder(tpl.id)}
                    title="Edit in builder"
                    style={{ background: 'none', border: 'none', cursor: 'pointer', padding: 4, display: 'flex', alignItems: 'center', color: 'var(--text-secondary)' }}
                  >
                    <Edit2 size={15} />
                  </button>
                  <button
                    onClick={e => handleDelete(tpl, e)}
                    disabled={deletingId === tpl.id}
                    title="Delete template"
                    style={{ background: 'none', border: 'none', cursor: 'pointer', padding: 4, display: 'flex', alignItems: 'center', color: 'var(--danger,#ef4444)' }}
                  >
                    <Trash2 size={15} />
                  </button>
                </div>
              </div>
            ))}
          </div>
        )}

        <div style={{ marginTop: 24 }}>
          <button
            onClick={() => openBuilder(null)}
            style={{ display: 'inline-flex', alignItems: 'center', gap: 8, padding: '10px 20px', background: 'var(--primary)', color: '#fff', border: 'none', borderRadius: 8, cursor: 'pointer', fontSize: 14, fontWeight: 600 }}
          >
            <Plus size={16} /> Open Builder (New Template)
          </button>
        </div>
      </div>
    </div>
  );
}

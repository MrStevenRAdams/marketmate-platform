import { useState, useEffect, useCallback, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import { getActiveTenantId } from '../contexts/TenantContext';
import './Workflows.css';

// ─── Types ────────────────────────────────────────────────────────────────────

const API_BASE = import.meta.env.VITE_API_URL ?? 'http://localhost:8080/api/v1';

type WorkflowStatus = 'draft' | 'active' | 'paused' | 'archived';

interface Workflow {
  workflow_id: string;
  name: string;
  description?: string;
  priority: number;
  status: WorkflowStatus;
  trigger: { type: string; channels?: string[] };
  conditions: Condition[];
  actions: Action[];
  settings: { stop_on_error: boolean; test_mode: boolean; log_level: string };
  stats: { total_evaluated: number; total_matched: number; total_executed: number; total_failed: number };
  created_at: string;
  updated_at: string;
  last_executed_at?: string;
}

interface Condition {
  type: string;
  // geo
  geo_field?: string; geo_operator?: string; geo_value?: string; geo_values?: string[];
  // value
  value_field?: string; value_operator?: string; value_amount?: number; value_min?: number; value_max?: number; value_currency?: string;
  // weight
  weight_field?: string; weight_operator?: string; weight_value?: number; weight_min?: number; weight_max?: number;
  // item_count
  item_count_field?: string; item_count_operator?: string; item_count_value?: number; item_count_min?: number; item_count_max?: number;
  // sku
  sku_operator?: string; sku_value?: string; sku_values?: string[]; sku_scope?: string;
  // channel
  channel_operator?: string; channel_value?: string; channel_values?: string[];
  // tag
  tag_operator?: string; tag_value?: string; tag_values?: string[];
  // time
  time_field?: string; time_operator?: string; time_value?: string; time_days?: string[];
  // fulfilment_type
  fulfilment_type_operator?: string; fulfilment_type_value?: string; fulfilment_type_values?: string[];
}

interface Action {
  type: string;
  fulfilment_source_id?: string;
  carrier_id?: string;
  service_code?: string;
  signature_required?: boolean;
  declared_value_gbp?: number;
  tag?: string;
  status?: string;
  note?: string;
  priority?: number;
}

interface MarketplaceCredential {
  credential_id: string;
  account_name: string;
  channel: string;
  status: string;
}

type ViewMode = 'list' | 'edit' | 'executions';

const CONDITION_TYPES = [
  { value: 'geography', label: '🌍 Geography (country, postcode)' },
  { value: 'order_value', label: '💰 Order Value' },
  { value: 'weight', label: '⚖️ Weight' },
  { value: 'item_count', label: '📦 Item Count' },
  { value: 'sku', label: '🏷️ SKU / Product' },
  { value: 'channel', label: '🛒 Sales Channel' },
  { value: 'tag', label: '🔖 Order Tag' },
  { value: 'time', label: '🕐 Time / Date' },
  { value: 'fulfilment_type', label: '🏭 Fulfilment Type' },
];

const ACTION_TYPES = [
  { value: 'assign_fulfilment_source', label: '🏭 Assign Fulfilment Source' },
  { value: 'assign_fulfilment_network', label: '🌐 Assign Fulfilment Network' },
  { value: 'assign_carrier', label: '🚚 Assign Carrier & Service' },
  { value: 'assign_to_pickwave', label: '🌊 Assign to Pickwave' },
  { value: 'add_tag', label: '🔖 Add Tag to Order' },
  { value: 'set_priority', label: '⬆️ Set Processing Priority' },
  { value: 'add_note', label: '📝 Add Internal Note' },
  { value: 'set_status', label: '🔄 Set Order Status' },
];

const CHANNELS = ['amazon', 'ebay', 'temu', 'shopify', 'woocommerce', 'manual'];
const COUNTRIES_EU = ['GB', 'DE', 'FR', 'IT', 'ES', 'NL', 'BE', 'PL', 'SE', 'AT', 'DK', 'FI', 'IE', 'PT', 'US', 'CA', 'AU'];

function emptyCondition(): Condition { return { type: 'geography', geo_field: 'country', geo_operator: 'in', geo_values: [] }; }
function emptyAction(): Action { return { type: 'assign_fulfilment_source' }; }

function blankWorkflow(): Partial<Workflow> {
  return {
    name: '',
    description: '',
    priority: 100,
    status: 'draft',
    trigger: { type: 'order.imported', channels: [] },
    conditions: [],
    actions: [],
    settings: { stop_on_error: true, test_mode: false, log_level: 'normal' },
  };
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

function api(path: string, init?: RequestInit) {
  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId(), ...init?.headers },
  });
}

function statusBadge(status: WorkflowStatus) {
  const map: Record<WorkflowStatus, string> = {
    active: 'badge-active',
    draft: 'badge-draft',
    paused: 'badge-paused',
    archived: 'badge-archived',
  };
  return <span className={`wf-badge ${map[status] || ''}`}>{status}</span>;
}

function fmt(date?: string) {
  if (!date) return '—';
  try { return new Date(date).toLocaleDateString('en-GB', { day: '2-digit', month: 'short', year: 'numeric' }); }
  catch { return date; }
}

// ─── Main Component ────────────────────────────────────────────────────────────

export default function Workflows() {
  const navigate = useNavigate();
  const [view, setView] = useState<ViewMode>('list');
  const [workflows, setWorkflows] = useState<Workflow[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [selected, setSelected] = useState<Workflow | null>(null);
  const [editForm, setEditForm] = useState<Partial<Workflow>>(blankWorkflow());
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState('');
  const [fulfilmentSources, setFulfilmentSources] = useState<{ source_id: string; name: string; type: string }[]>([]);
  const [configuredCarriers, setConfiguredCarriers] = useState<{ id: string; display_name: string }[]>([]);
  const [executions, setExecutions] = useState<any[]>([]);
  const [execLoading, setExecLoading] = useState(false);
  const [bulkSelected, setBulkSelected] = useState<Set<string>>(new Set());
  const [credentials, setCredentials] = useState<MarketplaceCredential[]>([]);
  const [dragId, setDragId] = useState<string | null>(null);
  const [dragOverId, setDragOverId] = useState<string | null>(null);

  const loadWorkflows = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const res = await api('/workflows');
      if (!res.ok) throw new Error('Failed to load workflows');
      const data = await res.json();
      setWorkflows(data.workflows || []);
    } catch (e: any) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }, []);

  const loadCredentials = useCallback(async () => {
    try {
      const res = await api('/marketplace/credentials');
      if (res.ok) {
        const data = await res.json();
        const active = (data.credentials || []).filter((c: MarketplaceCredential) => c.status === 'active' || c.status === 'connected');
        setCredentials(active);
      }
    } catch {}
  }, []);

  const handleReorder = useCallback(async (orderedIds: string[]) => {
    // Optimistically update priority in local state
    setWorkflows(prev => {
      const map = new Map(prev.map(w => [w.workflow_id, w]));
      return orderedIds.map((id, i) => ({
        ...map.get(id)!,
        priority: orderedIds.length - i, // highest index = highest priority
      }));
    });
    try {
      await api('/workflows/reorder', {
        method: 'PATCH',
        body: JSON.stringify({ ordered_ids: orderedIds }),
      });
    } catch {
      loadWorkflows(); // revert on failure
    }
  }, [loadWorkflows]);

  const loadFulfilmentSources = useCallback(async () => {
    try {
      const res = await api('/fulfilment-sources');
      if (res.ok) {
        const data = await res.json();
        setFulfilmentSources(data.sources || []);
      }
    } catch {}
  }, []);

  const loadConfiguredCarriers = useCallback(async () => {
    try {
      const res = await api('/dispatch/carriers/configured');
      if (res.ok) {
        const data = await res.json();
        setConfiguredCarriers((data.carriers || []).filter((c: any) => c.is_active));
      }
    } catch {}
  }, []);

  useEffect(() => {
    loadWorkflows();
    loadFulfilmentSources();
    loadCredentials();
    loadConfiguredCarriers();
  }, [loadWorkflows, loadFulfilmentSources, loadCredentials, loadConfiguredCarriers]);

  // ── List actions ──────────────────────────────────────────────────────────

  const handleNew = () => {
    setSelected(null);
    setEditForm(blankWorkflow());
    setSaveError('');
    setView('edit');
  };

  const handleEdit = (wf: Workflow) => {
    setSelected(wf);
    setEditForm({ ...wf });
    setSaveError('');
    setView('edit');
  };

  const handleActivate = async (id: string) => {
    await api(`/workflows/${id}/activate`, { method: 'POST' });
    loadWorkflows();
  };

  const handlePause = async (id: string) => {
    await api(`/workflows/${id}/pause`, { method: 'POST' });
    loadWorkflows();
  };

  const handleDuplicate = async (id: string) => {
    await api(`/workflows/${id}/duplicate`, { method: 'POST' });
    loadWorkflows();
  };

  const handleDelete = async (id: string) => {
    if (!confirm('Delete this workflow? This cannot be undone.')) return;
    await api(`/workflows/${id}`, { method: 'DELETE' });
    loadWorkflows();
  };

  const handleViewExecutions = async (wf: Workflow) => {
    setSelected(wf);
    setExecLoading(true);
    setView('executions');
    try {
      const res = await api(`/workflows/${wf.workflow_id}/executions`);
      if (res.ok) {
        const data = await res.json();
        setExecutions(data.executions || []);
      }
    } catch {}
    setExecLoading(false);
  };

  const handleBulkActivate = async () => {
    await api('/workflows/bulk/activate', { method: 'POST', body: JSON.stringify({ workflow_ids: [...bulkSelected] }) });
    setBulkSelected(new Set());
    loadWorkflows();
  };

  const handleBulkPause = async () => {
    await api('/workflows/bulk/pause', { method: 'POST', body: JSON.stringify({ workflow_ids: [...bulkSelected] }) });
    setBulkSelected(new Set());
    loadWorkflows();
  };

  // ── Save ──────────────────────────────────────────────────────────────────

  const handleSave = async () => {
    if (!editForm.name?.trim()) { setSaveError('Workflow name is required'); return; }
    if (!editForm.actions?.length) { setSaveError('At least one action is required'); return; }

    setSaving(true);
    setSaveError('');
    try {
      const isNew = !selected;
      const url = isNew ? '/workflows' : `/workflows/${selected!.workflow_id}`;
      const method = isNew ? 'POST' : 'PATCH';
      const res = await api(url, { method, body: JSON.stringify(editForm) });
      if (!res.ok) {
        const err = await res.json();
        throw new Error(err.error || 'Save failed');
      }
      setView('list');
      loadWorkflows();
    } catch (e: any) {
      setSaveError(e.message);
    } finally {
      setSaving(false);
    }
  };

  // ── Condition helpers ─────────────────────────────────────────────────────

  const updateCondition = (idx: number, patch: Partial<Condition>) => {
    setEditForm(f => {
      const conds = [...(f.conditions || [])];
      conds[idx] = { ...conds[idx], ...patch };
      return { ...f, conditions: conds };
    });
  };

  const addCondition = () => {
    setEditForm(f => ({ ...f, conditions: [...(f.conditions || []), emptyCondition()] }));
  };

  const removeCondition = (idx: number) => {
    setEditForm(f => ({ ...f, conditions: (f.conditions || []).filter((_, i) => i !== idx) }));
  };

  const changeConditionType = (idx: number, type: string) => {
    const defaults: Record<string, Condition> = {
      geography: { type: 'geography', geo_field: 'country', geo_operator: 'in', geo_values: [] },
      order_value: { type: 'order_value', value_field: 'grand_total', value_operator: 'gte', value_amount: 0, value_currency: 'GBP' },
      weight: { type: 'weight', weight_field: 'total_weight', weight_operator: 'lte', weight_value: 2 },
      item_count: { type: 'item_count', item_count_field: 'total_quantity', item_count_operator: 'gte', item_count_value: 1 },
      sku: { type: 'sku', sku_operator: 'in', sku_values: [] },
      channel: { type: 'channel', channel_operator: 'in', channel_values: [] },
      tag: { type: 'tag', tag_operator: 'has_any', tag_values: [] },
      time: { type: 'time', time_field: 'order_date', time_operator: 'before', time_value: '17:00' },
      fulfilment_type: { type: 'fulfilment_type', fulfilment_type_operator: 'in', fulfilment_type_values: [] },
    };
    setEditForm(f => {
      const conds = [...(f.conditions || [])];
      conds[idx] = defaults[type] || { type };
      return { ...f, conditions: conds };
    });
  };

  // ── Action helpers ────────────────────────────────────────────────────────

  const updateAction = (idx: number, patch: Partial<Action>) => {
    setEditForm(f => {
      const acts = [...(f.actions || [])];
      acts[idx] = { ...acts[idx], ...patch };
      return { ...f, actions: acts };
    });
  };

  const addAction = () => {
    setEditForm(f => ({ ...f, actions: [...(f.actions || []), emptyAction()] }));
  };

  const removeAction = (idx: number) => {
    setEditForm(f => ({ ...f, actions: (f.actions || []).filter((_, i) => i !== idx) }));
  };

  // ─── Render ───────────────────────────────────────────────────────────────

  if (view === 'executions' && selected) {
    return <ExecutionsView wf={selected} executions={executions} loading={execLoading} onBack={() => setView('list')} />;
  }

  if (view === 'edit') {
    return (
      <WorkflowEditor
        form={editForm}
        setForm={setEditForm}
        isNew={!selected}
        saving={saving}
        saveError={saveError}
        fulfilmentSources={fulfilmentSources}
        configuredCarriers={configuredCarriers}
        credentials={credentials}
        onSave={handleSave}
        onCancel={() => setView('list')}
        onConditionTypeChange={changeConditionType}
        updateCondition={updateCondition}
        addCondition={addCondition}
        removeCondition={removeCondition}
        updateAction={updateAction}
        addAction={addAction}
        removeAction={removeAction}
      />
    );
  }

  return (
    <div className="wf-page">
      <div className="wf-header">
        <div>
          <h1 className="wf-title">Shipping Workflows</h1>
          <p className="wf-subtitle">
            Automatically route orders to the right fulfilment source and carrier based on rules.
            Rules are evaluated in priority order — first match wins.
          </p>
        </div>
        <div className="wf-header-actions">
          <button className="btn btn-secondary" onClick={() => navigate('/workflow-simulator')}>
            🧪 Simulator
          </button>
          <button className="btn btn-primary" onClick={handleNew}>
            + New Workflow
          </button>
        </div>
      </div>

      {bulkSelected.size > 0 && (
        <div className="wf-bulk-bar">
          <span>{bulkSelected.size} selected</span>
          <button className="btn btn-sm btn-success" onClick={handleBulkActivate}>Activate All</button>
          <button className="btn btn-sm btn-warning" onClick={handleBulkPause}>Pause All</button>
          <button className="btn btn-sm btn-ghost" onClick={() => setBulkSelected(new Set())}>Clear</button>
        </div>
      )}

      {error && <div className="wf-error">{error}</div>}

      {loading ? (
        <div className="wf-loading">Loading workflows…</div>
      ) : workflows.length === 0 ? (
        <div className="wf-empty">
          <div className="wf-empty-icon">⚙️</div>
          <h3>No workflows yet</h3>
          <p>Create your first workflow to automatically route orders to the right warehouse or carrier.</p>
          <button className="btn btn-primary" onClick={handleNew}>Create First Workflow</button>
        </div>
      ) : (
        <div className="wf-table-wrap">
          <table className="wf-table">
            <thead>
              <tr>
                <th style={{ width: 28 }} title="Drag to reorder"></th>
                <th style={{ width: 40 }}>
                  <input type="checkbox"
                    checked={bulkSelected.size === workflows.length}
                    onChange={e => setBulkSelected(e.target.checked ? new Set(workflows.map(w => w.workflow_id)) : new Set())}
                  />
                </th>
                <th style={{ width: 48 }}>#</th>
                <th>Workflow</th>
                <th>Trigger</th>
                <th>Rules</th>
                <th>Status</th>
                <th>Executions</th>
                <th>Last Run</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {[...workflows].sort((a, b) => b.priority - a.priority).map((wf, idx) => (
                <tr
                  key={wf.workflow_id}
                  className={[
                    wf.status === 'active' ? 'row-active' : '',
                    dragId === wf.workflow_id ? 'wf-row-dragging' : '',
                    dragOverId === wf.workflow_id && dragId !== wf.workflow_id ? 'wf-row-dragover' : '',
                  ].filter(Boolean).join(' ')}
                  draggable
                  onDragStart={() => setDragId(wf.workflow_id)}
                  onDragOver={e => { e.preventDefault(); setDragOverId(wf.workflow_id); }}
                  onDragLeave={() => setDragOverId(null)}
                  onDrop={e => {
                    e.preventDefault();
                    setDragOverId(null);
                    if (!dragId || dragId === wf.workflow_id) { setDragId(null); return; }
                    const sorted = [...workflows].sort((a, b) => b.priority - a.priority);
                    const fromIdx = sorted.findIndex(w => w.workflow_id === dragId);
                    const toIdx = sorted.findIndex(w => w.workflow_id === wf.workflow_id);
                    const reordered = [...sorted];
                    const [moved] = reordered.splice(fromIdx, 1);
                    reordered.splice(toIdx, 0, moved);
                    setDragId(null);
                    handleReorder(reordered.map(w => w.workflow_id));
                  }}
                  onDragEnd={() => { setDragId(null); setDragOverId(null); }}
                >
                  <td className="wf-drag-handle" title="Drag to reorder">⠿</td>
                  <td>
                    <input type="checkbox"
                      checked={bulkSelected.has(wf.workflow_id)}
                      onChange={e => {
                        const s = new Set(bulkSelected);
                        e.target.checked ? s.add(wf.workflow_id) : s.delete(wf.workflow_id);
                        setBulkSelected(s);
                      }}
                    />
                  </td>
                  <td><span className="wf-priority">{idx + 1}</span></td>
                  <td>
                    <div className="wf-name">{wf.name}</div>
                    {wf.description && <div className="wf-desc">{wf.description}</div>}
                    {wf.settings?.test_mode && <span className="wf-badge badge-test">TEST MODE</span>}
                  </td>
                  <td><span className="wf-trigger">{wf.trigger?.type || '—'}</span></td>
                  <td>
                    <span className="wf-rule-count">{wf.conditions?.length || 0} conditions</span>
                    <span className="wf-rule-sep">→</span>
                    <span className="wf-rule-count">{wf.actions?.length || 0} actions</span>
                  </td>
                  <td>{statusBadge(wf.status)}</td>
                  <td>
                    <div className="wf-stats">
                      <span title="Matched">{wf.stats?.total_matched ?? 0}✓</span>
                      {(wf.stats?.total_failed ?? 0) > 0 && (
                        <span className="wf-stat-fail" title="Failed">{wf.stats.total_failed}✗</span>
                      )}
                    </div>
                  </td>
                  <td className="wf-date">{fmt(wf.last_executed_at)}</td>
                  <td>
                    <div className="wf-row-actions">
                      <button className="btn btn-xs" onClick={() => handleEdit(wf)} title="Edit">✏️</button>
                      <button className="btn btn-xs" onClick={() => handleViewExecutions(wf)} title="Executions">📋</button>
                      <button className="btn btn-xs" onClick={() => handleDuplicate(wf.workflow_id)} title="Duplicate">📄</button>
                      {wf.status === 'active'
                        ? <button className="btn btn-xs btn-warning" onClick={() => handlePause(wf.workflow_id)} title="Pause">⏸</button>
                        : <button className="btn btn-xs btn-success" onClick={() => handleActivate(wf.workflow_id)} title="Activate">▶️</button>
                      }
                      <button className="btn btn-xs btn-danger" onClick={() => handleDelete(wf.workflow_id)} title="Delete">🗑</button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <div className="wf-info-box">
        <strong>How workflows work:</strong> When an order is imported, all <em>active</em> workflows are
        evaluated in priority order (highest first). The first workflow where ALL conditions match has its
        actions executed. If no workflow matches, the default fulfilment source is used.
      </div>
    </div>
  );
}

// ─── Workflow Editor ───────────────────────────────────────────────────────────

interface EditorProps {
  form: Partial<Workflow>;
  setForm: React.Dispatch<React.SetStateAction<Partial<Workflow>>>;
  isNew: boolean;
  saving: boolean;
  saveError: string;
  fulfilmentSources: { source_id: string; name: string; type: string }[];
  configuredCarriers: { id: string; display_name: string }[];
  credentials: MarketplaceCredential[];
  onSave: () => void;
  onCancel: () => void;
  onConditionTypeChange: (idx: number, type: string) => void;
  updateCondition: (idx: number, patch: Partial<Condition>) => void;
  addCondition: () => void;
  removeCondition: (idx: number) => void;
  updateAction: (idx: number, patch: Partial<Action>) => void;
  addAction: () => void;
  removeAction: (idx: number) => void;
}

// ─── Workflow Canvas ───────────────────────────────────────────────────────────
// SVG visual mode for WorkflowEditor — displays conditions and actions as
// an interactive flowchart. Reads/writes the same form.conditions & form.actions
// arrays so both modes stay in sync.

const WF_NODE_W  = 240;
const WF_NODE_H  = 52;
const WF_ROW_GAP = 80;
const WF_COL_GAP = 340;
const WF_PAD     = 44;

const WF_COLORS = {
  trigger:   { fill: '#080e1e', stroke: '#3b82f6', text: '#60a5fa', accent: '#3b82f6', icon: '⚡' },
  condition: { fill: '#080e1e', stroke: '#38bdf8', text: '#7dd3fc', accent: '#38bdf8', icon: '◈' },
  action:    { fill: '#080e1e', stroke: '#a78bfa', text: '#c4b5fd', accent: '#a78bfa', icon: '▶' },
};

interface WfCanvasNode {
  id: string;
  kind: 'trigger' | 'condition' | 'action';
  label: string;
  condIdx?: number;   // index into form.conditions[]
  actionIdx?: number; // index into form.actions[]
  x: number;
  y: number;
}

interface WfCanvasEdge { from: string; to: string; }

function buildWfGraph(conditions: Condition[], actions: Action[], trigger: Workflow['trigger']): { nodes: WfCanvasNode[]; edges: WfCanvasEdge[] } {
  const nodes: WfCanvasNode[] = [];
  const edges: WfCanvasEdge[] = [];

  // Trigger node
  const trigLabel = trigger?.type === 'order.imported' ? 'Order imported'
    : trigger?.type === 'order.status_changed' ? 'Status changed'
    : trigger?.type || 'Trigger';
  nodes.push({ id: 'wf-trigger', kind: 'trigger', label: trigLabel, x: WF_PAD, y: WF_PAD });

  // Condition column
  const condStartY = WF_PAD + WF_NODE_H + WF_ROW_GAP;
  conditions.forEach((cond, i) => {
    const id = `wf-cond-${i}`;
    const label = conditionLabel(cond);
    nodes.push({ id, kind: 'condition', label, condIdx: i, x: WF_PAD, y: condStartY + i * (WF_NODE_H + WF_ROW_GAP) });
  });

  // Action column
  const actionColX = WF_PAD + WF_NODE_W + WF_COL_GAP;
  const actionStartY = WF_PAD + WF_NODE_H + WF_ROW_GAP;
  actions.forEach((action, i) => {
    const id = `wf-action-${i}`;
    nodes.push({ id, kind: 'action', label: actionLabel(action), actionIdx: i, x: actionColX, y: actionStartY + i * (WF_NODE_H + WF_ROW_GAP) });
  });

  // Edges
  const condIds = conditions.map((_, i) => `wf-cond-${i}`);
  const actionIds = actions.map((_, i) => `wf-action-${i}`);

  if (condIds.length > 0) {
    edges.push({ from: 'wf-trigger', to: condIds[0] });
    for (let i = 1; i < condIds.length; i++) edges.push({ from: condIds[i - 1], to: condIds[i] });
    if (actionIds.length > 0) edges.push({ from: condIds[condIds.length - 1], to: actionIds[0] });
  } else if (actionIds.length > 0) {
    edges.push({ from: 'wf-trigger', to: actionIds[0] });
  }
  for (let i = 1; i < actionIds.length; i++) edges.push({ from: actionIds[i - 1], to: actionIds[i] });

  return { nodes, edges };
}

function conditionLabel(cond: Condition): string {
  switch (cond.type) {
    case 'geography':    return `${cond.geo_field || 'country'} ${cond.geo_operator || 'in'} [${(cond.geo_values || []).join(', ') || '…'}]`;
    case 'order_value':  return `total ${cond.value_operator || 'gte'} ${cond.value_amount ?? '?'} ${cond.value_currency || 'GBP'}`;
    case 'weight':       return `weight ${cond.weight_operator || 'lte'} ${cond.weight_value ?? '?'}kg`;
    case 'item_count':   return `items ${cond.item_count_operator || 'gte'} ${cond.item_count_value ?? '?'}`;
    case 'sku':          return `sku ${cond.sku_operator || 'in'} [${(cond.sku_values || []).slice(0, 2).join(', ') || '…'}]`;
    case 'channel':      return `channel ${cond.channel_operator || 'in'} [${(cond.channel_values || []).join(', ') || '…'}]`;
    case 'tag':          return `tag ${cond.tag_operator || 'has_any'} [${(cond.tag_values || []).join(', ') || '…'}]`;
    case 'time':         return `time ${cond.time_operator || ''} ${cond.time_value || ''}`;
    case 'fulfilment_type': return `fulfilment ${cond.fulfilment_type_operator || 'in'} [${(cond.fulfilment_type_values || []).join(', ') || '…'}]`;
    default:             return cond.type || 'condition';
  }
}

function actionLabel(action: Action): string {
  switch (action.type) {
    case 'assign_fulfilment_source': return `→ fulfilment: ${action.fulfilment_source_id || '?'}`;
    case 'assign_fulfilment_network': return `→ network: ${action.network_name || '?'}`;
    case 'assign_carrier':           return `→ carrier: ${action.carrier_id || '?'} ${action.service_code || ''}`;
    case 'add_tag':                  return `tag + "${action.tag || ''}"`;
    case 'add_note':                 return `note: "${(action.note || '').slice(0, 18)}…"`;
    case 'set_priority':             return `priority → ${action.priority ?? '?'}`;
    case 'set_status':               return `status → ${action.status || '?'}`;
    default:                         return action.type || 'action';
  }
}

function WorkflowCanvas({
  form, onAddCondition, onAddAction, onRemoveCondition, onRemoveAction,
}: {
  form: Partial<Workflow>;
  onAddCondition: () => void;
  onAddAction: () => void;
  onRemoveCondition: (idx: number) => void;
  onRemoveAction: (idx: number) => void;
}) {
  const svgRef = useRef<SVGSVGElement>(null);
  const [nodePositions, setNodePositions] = useState<Record<string, { x: number; y: number }>>({});
  const [dragging, setDragging] = useState<{ id: string; ox: number; oy: number } | null>(null);
  const [hovered, setHovered] = useState<string | null>(null);

  const baseGraph = buildWfGraph(form.conditions || [], form.actions || [], form.trigger!);

  // Merge base positions with user-dragged positions
  const nodes: WfCanvasNode[] = baseGraph.nodes.map(n => ({
    ...n,
    x: nodePositions[n.id]?.x ?? n.x,
    y: nodePositions[n.id]?.y ?? n.y,
  }));
  const edges = baseGraph.edges;

  const maxX = Math.max(...nodes.map(n => n.x + WF_NODE_W), 580);
  const maxY = Math.max(...nodes.map(n => n.y + WF_NODE_H), 320);
  const svgW = maxX + WF_PAD * 2;
  const svgH = maxY + WF_PAD * 2;

  const getSvgPt = (e: React.MouseEvent) => {
    const svg = svgRef.current;
    if (!svg) return null;
    const pt = svg.createSVGPoint();
    pt.x = e.clientX; pt.y = e.clientY;
    return pt.matrixTransform(svg.getScreenCTM()!.inverse());
  };

  const handleMouseDown = (e: React.MouseEvent, id: string) => {
    e.preventDefault();
    const pt = getSvgPt(e);
    if (!pt) return;
    const node = nodes.find(n => n.id === id);
    if (!node) return;
    setDragging({ id, ox: pt.x - node.x, oy: pt.y - node.y });
  };

  const handleMouseMove = (e: React.MouseEvent) => {
    if (!dragging) return;
    const pt = getSvgPt(e);
    if (!pt) return;
    setNodePositions(prev => ({
      ...prev,
      [dragging.id]: { x: Math.max(WF_PAD, pt.x - dragging.ox), y: Math.max(WF_PAD, pt.y - dragging.oy) },
    }));
  };

  const nodeById = (id: string) => nodes.find(n => n.id === id);

  const edgePath = (from: WfCanvasNode, to: WfCanvasNode): string => {
    const dx = Math.abs((to.x + WF_NODE_W / 2) - (from.x + WF_NODE_W / 2));
    if (dx > 80) {
      // cross-column horizontal bezier
      const x1 = from.x + WF_NODE_W;
      const y1 = from.y + WF_NODE_H / 2;
      const x2 = to.x;
      const y2 = to.y + WF_NODE_H / 2;
      const mx = (x1 + x2) / 2;
      return `M ${x1} ${y1} C ${mx} ${y1}, ${mx} ${y2}, ${x2} ${y2}`;
    }
    // same column vertical bezier
    const x1 = from.x + WF_NODE_W / 2;
    const y1 = from.y + WF_NODE_H;
    const x2 = to.x + WF_NODE_W / 2;
    const y2 = to.y;
    const cy = (y1 + y2) / 2;
    return `M ${x1} ${y1} C ${x1} ${cy}, ${x2} ${cy}, ${x2} ${y2}`;
  };

  return (
    <div className="wf-canvas-wrap">
      {/* Toolbar */}
      <div className="wf-canvas-toolbar">
        <button className="wf-canvas-btn wf-canvas-btn-cond" onClick={onAddCondition}>+ Condition</button>
        <button className="wf-canvas-btn wf-canvas-btn-action" onClick={onAddAction}>+ Action</button>
        <span className="wf-canvas-hint">Drag nodes to rearrange · Click × to remove · Use Form view to edit values</span>
      </div>

      <div style={{ overflow: 'auto', flex: 1 }}>
        <svg
          ref={svgRef}
          width={svgW}
          height={svgH}
          onMouseMove={handleMouseMove}
          onMouseUp={() => setDragging(null)}
          onMouseLeave={() => setDragging(null)}
          style={{ display: 'block', cursor: dragging ? 'grabbing' : 'default' }}
        >
          <defs>
            <marker id="wfarr" markerWidth="8" markerHeight="6" refX="8" refY="3" orient="auto">
              <polygon points="0 0, 8 3, 0 6" fill="#3d4c6a" />
            </marker>
            <filter id="wfshadow" x="-20%" y="-20%" width="140%" height="140%">
              <feDropShadow dx="0" dy="2" stdDeviation="3" floodColor="#000" floodOpacity="0.5" />
            </filter>
          </defs>
          {/* Grid */}
          <pattern id="wfdots" width="28" height="28" patternUnits="userSpaceOnUse">
            <circle cx="14" cy="14" r="0.7" fill="#111827" />
          </pattern>
          <rect width="100%" height="100%" fill="url(#wfdots)" />

          {/* Column headers */}
          <text x={WF_PAD + WF_NODE_W / 2} y={24} textAnchor="middle" fontSize={9} fill="#253045" fontWeight={700} letterSpacing="0.12em">CONDITIONS</text>
          <text x={WF_PAD + WF_NODE_W + WF_COL_GAP + WF_NODE_W / 2} y={24} textAnchor="middle" fontSize={9} fill="#253045" fontWeight={700} letterSpacing="0.12em">ACTIONS</text>

          {/* Edges */}
          {edges.map((edge, i) => {
            const f = nodeById(edge.from);
            const t = nodeById(edge.to);
            if (!f || !t) return null;
            return <path key={i} d={edgePath(f, t)} fill="none" stroke="#2a3450" strokeWidth="1.5" strokeDasharray="6 4" markerEnd="url(#wfarr)" />;
          })}

          {/* Nodes */}
          {nodes.map(node => {
            const c = WF_COLORS[node.kind];
            const isH = hovered === node.id;
            return (
              <g key={node.id}
                transform={`translate(${node.x},${node.y})`}
                style={{ cursor: node.kind !== 'trigger' ? 'grab' : 'default', userSelect: 'none' }}
                onMouseDown={e => node.kind !== 'trigger' && handleMouseDown(e, node.id)}
                onMouseEnter={() => setHovered(node.id)}
                onMouseLeave={() => setHovered(null)}
                filter="url(#wfshadow)"
              >
                <rect width={WF_NODE_W} height={WF_NODE_H} rx={8} fill={c.fill}
                  stroke={isH ? c.text : c.stroke} strokeWidth={isH ? 2 : 1.5} />
                <rect width={3} height={WF_NODE_H} rx={8} fill={c.accent} opacity={0.85} />
                <text x={14} y={WF_NODE_H / 2 + 5} fontSize={14} fill={c.text}>{c.icon}</text>
                <text x={30} y={WF_NODE_H / 2} dominantBaseline="middle" fontSize={11}
                  fontFamily='"JetBrains Mono","Fira Code",monospace' fill={c.text}>
                  {node.label.length > 28 ? node.label.slice(0, 26) + '…' : node.label}
                </text>
                <text x={WF_NODE_W - 7} y={11} textAnchor="end" fontSize={8} fill={c.stroke}
                  fontWeight={700} letterSpacing="0.1em">
                  {node.kind.toUpperCase()}
                </text>
                {/* Remove button */}
                {isH && node.kind !== 'trigger' && (
                  <g transform={`translate(${WF_NODE_W - 10},-10)`} style={{ cursor: 'pointer' }}
                    onMouseDown={ev => {
                      ev.stopPropagation();
                      if (node.condIdx !== undefined) onRemoveCondition(node.condIdx);
                      if (node.actionIdx !== undefined) onRemoveAction(node.actionIdx);
                    }}>
                    <circle r={8} fill="#0f1929" stroke="#ef4444" strokeWidth={1.5} />
                    <text x={0} y={4} textAnchor="middle" fontSize={11} fill="#ef4444">×</text>
                  </g>
                )}
              </g>
            );
          })}
        </svg>
      </div>
    </div>
  );
}

// ─── WorkflowEditor ────────────────────────────────────────────────────────────

function WorkflowEditor({ form, setForm, isNew, saving, saveError, fulfilmentSources, configuredCarriers, credentials,
  onSave, onCancel, onConditionTypeChange, updateCondition, addCondition, removeCondition,
  updateAction, addAction, removeAction }: EditorProps) {

  const [canvasMode, setCanvasMode] = useState(false);

  return (
    <div className="wf-editor">
      <div className="wf-editor-header">
        <button className="btn btn-ghost btn-sm" onClick={onCancel}>← Back</button>
        <h1 className="wf-title">{isNew ? 'New Workflow' : `Edit: ${form.name}`}</h1>
        <div className="wf-editor-actions">
          {/* View toggle */}
          <div className="wf-view-toggle">
            <button
              className={`wf-view-tab${!canvasMode ? ' wf-view-tab-active' : ''}`}
              onClick={() => setCanvasMode(false)}
              title="Form editor"
            >
              ☰ Form
            </button>
            <button
              className={`wf-view-tab${canvasMode ? ' wf-view-tab-active' : ''}`}
              onClick={() => setCanvasMode(true)}
              title="Visual canvas"
            >
              ⬡ Canvas
            </button>
          </div>
          <button className="btn btn-ghost" onClick={onCancel}>Cancel</button>
          <button className="btn btn-primary" onClick={onSave} disabled={saving}>
            {saving ? 'Saving…' : isNew ? 'Create Workflow' : 'Save Changes'}
          </button>
        </div>
      </div>

      {saveError && <div className="wf-error">{saveError}</div>}

      {canvasMode ? (
        <div>
          <div className="wf-canvas-info">
            <span>📋 <strong>Canvas view</strong> — Visual overview of your workflow. To edit condition values, switch to <button className="wf-link-btn" onClick={() => setCanvasMode(false)}>Form view</button>.</span>
          </div>
          <WorkflowCanvas
            form={form}
            onAddCondition={addCondition}
            onAddAction={addAction}
            onRemoveCondition={removeCondition}
            onRemoveAction={removeAction}
          />
        </div>
      ) : (
      <div className="wf-editor-body">
        {/* ── Section 1: Identity ── */}
        <section className="wf-section">
          <h2 className="wf-section-title">📋 Workflow Details</h2>
          <div className="wf-grid-2">
            <div className="wf-field">
              <label>Workflow Name *</label>
              <input className="wf-input" value={form.name || ''} onChange={e => setForm(f => ({ ...f, name: e.target.value }))} placeholder="e.g. UK Standard — Evri" />
            </div>
            <div className="wf-field">
              <label>Priority <span className="wf-hint">(higher = evaluated first, 1–1000)</span></label>
              <input className="wf-input" type="number" min={1} max={1000} value={form.priority || 100}
                onChange={e => setForm(f => ({ ...f, priority: Number(e.target.value) }))} />
            </div>
            <div className="wf-field wf-col-2">
              <label>Description</label>
              <input className="wf-input" value={form.description || ''} onChange={e => setForm(f => ({ ...f, description: e.target.value }))} placeholder="Optional description for this workflow" />
            </div>
          </div>
        </section>

        {/* ── Section 2: Trigger ── */}
        <section className="wf-section">
          <h2 className="wf-section-title">⚡ Trigger</h2>
          <div className="wf-grid-2">
            <div className="wf-field">
              <label>When to run</label>
              <select className="wf-select" value={form.trigger?.type || 'order.imported'}
                onChange={e => setForm(f => ({ ...f, trigger: { ...f.trigger!, type: e.target.value } }))}>
                <option value="order.imported">Order imported (any new order)</option>
                <option value="order.status_changed">Order status changed</option>
                <option value="manual">Manual only</option>
              </select>
            </div>
            <div className="wf-field">
              <label>Channel filter <span className="wf-hint">(blank = all channels)</span></label>
              <ChannelMultiSelect
                credentials={credentials}
                selected={form.trigger?.channels || []}
                onChange={channels => setForm(f => ({ ...f, trigger: { ...f.trigger!, channels } }))}
              />
            </div>
          </div>
        </section>

        {/* ── Section 3: Conditions ── */}
        <section className="wf-section">
          <div className="wf-section-header">
            <div>
              <h2 className="wf-section-title">🔍 Conditions</h2>
              <p className="wf-section-desc">ALL conditions must match. Leave empty to match every order.</p>
            </div>
            <button className="btn btn-sm btn-secondary" onClick={addCondition}>+ Add Condition</button>
          </div>

          {(form.conditions || []).length === 0 ? (
            <div className="wf-empty-conditions">
              No conditions — this workflow will match <strong>every</strong> order that triggers it.
              Add conditions to narrow it down.
            </div>
          ) : (
            <div className="wf-conditions-list">
              {(form.conditions || []).map((cond, idx) => (
                <ConditionRow
                  key={idx}
                  idx={idx}
                  cond={cond}
                  onTypeChange={type => onConditionTypeChange(idx, type)}
                  onChange={patch => updateCondition(idx, patch)}
                  onRemove={() => removeCondition(idx)}
                  credentials={credentials}
                />
              ))}
            </div>
          )}
        </section>

        {/* ── Section 4: Actions ── */}
        <section className="wf-section">
          <div className="wf-section-header">
            <div>
              <h2 className="wf-section-title">⚡ Actions</h2>
              <p className="wf-section-desc">Executed in order when all conditions match.</p>
            </div>
            <button className="btn btn-sm btn-secondary" onClick={addAction}>+ Add Action</button>
          </div>

          {(form.actions || []).length === 0 ? (
            <div className="wf-empty-conditions wf-error-hint">At least one action is required.</div>
          ) : (
            <div className="wf-conditions-list">
              {(form.actions || []).map((action, idx) => (
                <ActionRow
                  key={idx}
                  idx={idx}
                  action={action}
                  fulfilmentSources={fulfilmentSources}
                  configuredCarriers={configuredCarriers}
                  onChange={patch => updateAction(idx, patch)}
                  onRemove={() => removeAction(idx)}
                />
              ))}
            </div>
          )}
        </section>

        {/* ── Section 5: Settings ── */}
        <section className="wf-section">
          <h2 className="wf-section-title">⚙️ Settings</h2>
          <div className="wf-settings-grid">
            <label className="wf-toggle-label">
              <input type="checkbox"
                checked={form.settings?.test_mode || false}
                onChange={e => setForm(f => ({ ...f, settings: { ...f.settings!, test_mode: e.target.checked } }))}
              />
              <span>
                <strong>Test Mode</strong>
                <span className="wf-hint">Evaluate conditions but don't execute actions</span>
              </span>
            </label>
            <label className="wf-toggle-label">
              <input type="checkbox"
                checked={form.settings?.stop_on_error !== false}
                onChange={e => setForm(f => ({ ...f, settings: { ...f.settings!, stop_on_error: e.target.checked } }))}
              />
              <span>
                <strong>Stop on Error</strong>
                <span className="wf-hint">Halt action sequence if one fails</span>
              </span>
            </label>
            <div className="wf-field">
              <label>Status</label>
              <select className="wf-select wf-select-inline"
                value={form.status || 'draft'}
                onChange={e => setForm(f => ({ ...f, status: e.target.value as WorkflowStatus }))}>
                <option value="draft">Draft</option>
                <option value="active">Active</option>
                <option value="paused">Paused</option>
              </select>
            </div>
          </div>
        </section>
      </div>
      )}
    </div>
  );
}

// ─── Condition Row ─────────────────────────────────────────────────────────────

function ConditionRow({ idx, cond, onTypeChange, onChange, onRemove, credentials }: {
  idx: number; cond: Condition;
  onTypeChange: (type: string) => void;
  onChange: (patch: Partial<Condition>) => void;
  onRemove: () => void;
  credentials: MarketplaceCredential[];
}) {
  return (
    <div className="wf-condition-row">
      <div className="wf-condition-label">IF</div>

      <div className="wf-condition-fields">
        <select className="wf-select" value={cond.type} onChange={e => onTypeChange(e.target.value)}>
          {CONDITION_TYPES.map(ct => <option key={ct.value} value={ct.value}>{ct.label}</option>)}
        </select>

        {cond.type === 'geography' && <GeoCondition cond={cond} onChange={onChange} />}
        {cond.type === 'order_value' && <ValueCondition cond={cond} onChange={onChange} />}
        {cond.type === 'weight' && <WeightCondition cond={cond} onChange={onChange} />}
        {cond.type === 'item_count' && <ItemCountCondition cond={cond} onChange={onChange} />}
        {cond.type === 'sku' && <SKUCondition cond={cond} onChange={onChange} />}
        {cond.type === 'channel' && <ChannelConditionWithCreds cond={cond} onChange={onChange} credentials={credentials} />}
        {cond.type === 'tag' && <TagCondition cond={cond} onChange={onChange} />}
        {cond.type === 'fulfilment_type' && <FulfilmentTypeCondition cond={cond} onChange={onChange} />}
      </div>

      <button className="wf-remove-btn" onClick={onRemove} title="Remove condition">×</button>
    </div>
  );
}

function GeoCondition({ cond, onChange }: { cond: Condition; onChange: (p: Partial<Condition>) => void }) {
  const [csv, setCsv] = useState((cond.geo_values || []).join(', '));
  return (
    <div className="wf-inline-fields">
      <select className="wf-select-sm" value={cond.geo_field || 'country'} onChange={e => onChange({ geo_field: e.target.value })}>
        <option value="country">Country</option>
        <option value="postcode_prefix">Postcode prefix</option>
        <option value="region">Region</option>
      </select>
      <select className="wf-select-sm" value={cond.geo_operator || 'in'} onChange={e => onChange({ geo_operator: e.target.value })}>
        <option value="equals">is</option>
        <option value="not_equals">is not</option>
        <option value="in">is one of</option>
        <option value="not_in">is not one of</option>
        <option value="starts_with">starts with</option>
      </select>
      {(cond.geo_operator === 'in' || cond.geo_operator === 'not_in') ? (
        <input className="wf-input-sm" placeholder="GB, DE, FR  (comma-separated)" value={csv}
          onChange={e => { setCsv(e.target.value); onChange({ geo_values: e.target.value.split(',').map(v => v.trim()).filter(Boolean) }); }} />
      ) : (
        <input className="wf-input-sm" placeholder="e.g. GB" value={cond.geo_value || ''}
          onChange={e => onChange({ geo_value: e.target.value })} />
      )}
    </div>
  );
}

function ValueCondition({ cond, onChange }: { cond: Condition; onChange: (p: Partial<Condition>) => void }) {
  return (
    <div className="wf-inline-fields">
      <select className="wf-select-sm" value={cond.value_field || 'grand_total'} onChange={e => onChange({ value_field: e.target.value })}>
        <option value="grand_total">Grand Total</option>
        <option value="subtotal">Subtotal</option>
        <option value="shipping_paid">Shipping paid</option>
      </select>
      <select className="wf-select-sm" value={cond.value_operator || 'gte'} onChange={e => onChange({ value_operator: e.target.value })}>
        <option value="gt">greater than</option>
        <option value="gte">at least</option>
        <option value="lt">less than</option>
        <option value="lte">at most</option>
        <option value="eq">exactly</option>
        <option value="between">between</option>
      </select>
      <input className="wf-input-sm" type="number" step="0.01" min="0" value={cond.value_amount ?? 0}
        onChange={e => onChange({ value_amount: Number(e.target.value) })} />
      {cond.value_operator === 'between' && (
        <>
          <span className="wf-and-label">and</span>
          <input className="wf-input-sm" type="number" step="0.01" min="0" value={cond.value_max ?? 0}
            onChange={e => onChange({ value_max: Number(e.target.value) })} />
        </>
      )}
      <select className="wf-select-sm" value={cond.value_currency || 'GBP'} onChange={e => onChange({ value_currency: e.target.value })}>
        {['GBP', 'USD', 'EUR', 'AUD', 'CAD', '(any)'].map(c => <option key={c} value={c === '(any)' ? '' : c}>{c}</option>)}
      </select>
    </div>
  );
}

function WeightCondition({ cond, onChange }: { cond: Condition; onChange: (p: Partial<Condition>) => void }) {
  return (
    <div className="wf-inline-fields">
      <select className="wf-select-sm" value={cond.weight_field || 'total_weight'} onChange={e => onChange({ weight_field: e.target.value })}>
        <option value="total_weight">Total weight (kg)</option>
        <option value="heaviest_item">Heaviest item (kg)</option>
      </select>
      <select className="wf-select-sm" value={cond.weight_operator || 'lte'} onChange={e => onChange({ weight_operator: e.target.value })}>
        <option value="gt">greater than</option>
        <option value="gte">at least</option>
        <option value="lt">less than</option>
        <option value="lte">at most</option>
        <option value="between">between</option>
      </select>
      <input className="wf-input-sm" type="number" step="0.1" min="0" value={cond.weight_value ?? 0}
        onChange={e => onChange({ weight_value: Number(e.target.value) })} />
      {cond.weight_operator === 'between' && (
        <>
          <span className="wf-and-label">and</span>
          <input className="wf-input-sm" type="number" step="0.1" min="0" value={cond.weight_max ?? 0}
            onChange={e => onChange({ weight_max: Number(e.target.value) })} />
        </>
      )}
      <span className="wf-unit-label">kg</span>
    </div>
  );
}

function ItemCountCondition({ cond, onChange }: { cond: Condition; onChange: (p: Partial<Condition>) => void }) {
  return (
    <div className="wf-inline-fields">
      <select className="wf-select-sm" value={cond.item_count_field || 'total_quantity'} onChange={e => onChange({ item_count_field: e.target.value })}>
        <option value="total_quantity">Total quantity</option>
        <option value="distinct_skus">Distinct SKUs</option>
        <option value="line_count">Line count</option>
      </select>
      <select className="wf-select-sm" value={cond.item_count_operator || 'gte'} onChange={e => onChange({ item_count_operator: e.target.value })}>
        <option value="gt">greater than</option>
        <option value="gte">at least</option>
        <option value="lt">less than</option>
        <option value="lte">at most</option>
        <option value="eq">exactly</option>
        <option value="between">between</option>
      </select>
      <input className="wf-input-sm" type="number" min="0" value={cond.item_count_value ?? 1}
        onChange={e => onChange({ item_count_value: Number(e.target.value) })} />
      {cond.item_count_operator === 'between' && (
        <>
          <span className="wf-and-label">and</span>
          <input className="wf-input-sm" type="number" min="0" value={cond.item_count_max ?? 1}
            onChange={e => onChange({ item_count_max: Number(e.target.value) })} />
        </>
      )}
    </div>
  );
}

function SKUCondition({ cond, onChange }: { cond: Condition; onChange: (p: Partial<Condition>) => void }) {
  const [csv, setCsv] = useState((cond.sku_values || []).join(', '));
  return (
    <div className="wf-inline-fields">
      <select className="wf-select-sm" value={cond.sku_operator || 'in'} onChange={e => onChange({ sku_operator: e.target.value })}>
        <option value="equals">is exactly</option>
        <option value="in">is one of</option>
        <option value="not_in">is not one of</option>
        <option value="starts_with">starts with</option>
        <option value="contains">contains</option>
      </select>
      {(cond.sku_operator === 'in' || cond.sku_operator === 'not_in') ? (
        <input className="wf-input-md" placeholder="SKU1, SKU2, SKU3  (comma-separated)" value={csv}
          onChange={e => { setCsv(e.target.value); onChange({ sku_values: e.target.value.split(',').map(v => v.trim()).filter(Boolean) }); }} />
      ) : (
        <input className="wf-input-sm" placeholder="SKU value" value={cond.sku_value || ''}
          onChange={e => onChange({ sku_value: e.target.value })} />
      )}
      <select className="wf-select-sm" value={cond.sku_scope || 'any_line'} onChange={e => onChange({ sku_scope: e.target.value })}>
        <option value="any_line">any line matches</option>
        <option value="all_lines">all lines match</option>
      </select>
    </div>
  );
}

// ─── ChannelMultiSelect ────────────────────────────────────────────────────────
// Tag-style multi-select that loads real marketplace credentials from the API.
// Fallback to static CHANNELS list if no credentials loaded yet.

function ChannelMultiSelect({ credentials, selected, onChange }: {
  credentials: MarketplaceCredential[];
  selected: string[];
  onChange: (vals: string[]) => void;
}) {
  const [search, setSearch] = useState('');
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  // Build option list: real credentials if available, else static channel names
  const options: { id: string; label: string; channel: string }[] = credentials.length > 0
    ? credentials.map(c => ({
        id: c.credential_id,
        label: c.account_name,
        channel: c.channel,
      }))
    : CHANNELS.map(ch => ({ id: ch, label: ch, channel: ch }));

  const filtered = options.filter(o =>
    o.label.toLowerCase().includes(search.toLowerCase()) ||
    o.channel.toLowerCase().includes(search.toLowerCase())
  );

  const allSelected = options.length > 0 && options.every(o => selected.includes(o.id));

  const toggle = (id: string) => {
    onChange(selected.includes(id) ? selected.filter(s => s !== id) : [...selected, id]);
  };

  const toggleAll = () => {
    onChange(allSelected ? [] : options.map(o => o.id));
  };

  // Close on outside click
  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, []);

  const channelIcon: Record<string, string> = {
    amazon: '🟠', ebay: '🔵', temu: '🟢', shopify: '🟣',
    woocommerce: '🔵', manual: '⚪',
  };

  return (
    <div className="wf-multiselect" ref={ref}>
      {/* Selected tags */}
      <div className="wf-multiselect-tags" onClick={() => setOpen(o => !o)}>
        {selected.length === 0 ? (
          <span className="wf-multiselect-placeholder">All channels</span>
        ) : (
          selected.map(id => {
            const opt = options.find(o => o.id === id);
            return (
              <span key={id} className="wf-tag">
                {channelIcon[opt?.channel || ''] || '●'} {opt?.label || id}
                <button className="wf-tag-remove" onClick={e => { e.stopPropagation(); toggle(id); }}>×</button>
              </span>
            );
          })
        )}
        <span className="wf-multiselect-caret">{open ? '▲' : '▼'}</span>
      </div>

      {/* Dropdown */}
      {open && (
        <div className="wf-multiselect-dropdown">
          <div className="wf-multiselect-search">
            <input
              autoFocus
              placeholder="Search channels…"
              value={search}
              onChange={e => setSearch(e.target.value)}
              onClick={e => e.stopPropagation()}
            />
          </div>
          <div className="wf-multiselect-option wf-multiselect-select-all" onClick={toggleAll}>
            <input type="checkbox" readOnly checked={allSelected} />
            <span>Select all</span>
          </div>
          <div className="wf-multiselect-list">
            {filtered.map(opt => (
              <div key={opt.id} className="wf-multiselect-option" onClick={() => toggle(opt.id)}>
                <input type="checkbox" readOnly checked={selected.includes(opt.id)} />
                <span>{channelIcon[opt.channel] || '●'} {opt.label}</span>
                {credentials.length > 0 && <span className="wf-multiselect-channel">{opt.channel}</span>}
              </div>
            ))}
            {filtered.length === 0 && <div className="wf-multiselect-empty">No channels found</div>}
          </div>
        </div>
      )}
    </div>
  );
}


function ChannelCondition({ cond, onChange }: { cond: Condition; onChange: (p: Partial<Condition>) => void }) {
  // ChannelCondition needs access to credentials — passed via context trick:
  // We re-export a wrapper below that receives credentials as prop.
  return null; // replaced by ChannelConditionWithCreds
}

function ChannelConditionWithCreds({ cond, onChange, credentials }: {
  cond: Condition;
  onChange: (p: Partial<Condition>) => void;
  credentials: MarketplaceCredential[];
}) {
  const selected = cond.channel_values || (cond.channel_value ? [cond.channel_value] : []);
  return (
    <div className="wf-inline-fields">
      <select className="wf-select-sm" value={cond.channel_operator || 'in'} onChange={e => onChange({ channel_operator: e.target.value })}>
        <option value="equals">is</option>
        <option value="in">is one of</option>
        <option value="not_in">is not</option>
      </select>
      <ChannelMultiSelect
        credentials={credentials}
        selected={selected}
        onChange={vals => onChange({ channel_values: vals, channel_value: vals[0] })}
      />
    </div>
  );
}

function TagCondition({ cond, onChange }: { cond: Condition; onChange: (p: Partial<Condition>) => void }) {
  const [csv, setCsv] = useState((cond.tag_values || []).join(', '));
  return (
    <div className="wf-inline-fields">
      <select className="wf-select-sm" value={cond.tag_operator || 'has_any'} onChange={e => onChange({ tag_operator: e.target.value })}>
        <option value="has_any">has any of</option>
        <option value="has_all">has all of</option>
        <option value="has_none">has none of</option>
        <option value="equals">is exactly</option>
      </select>
      <input className="wf-input-md" placeholder="tag1, tag2  (comma-separated)" value={csv}
        onChange={e => { setCsv(e.target.value); onChange({ tag_values: e.target.value.split(',').map(v => v.trim()).filter(Boolean) }); }} />
    </div>
  );
}

function FulfilmentTypeCondition({ cond, onChange }: { cond: Condition; onChange: (p: Partial<Condition>) => void }) {
  const types = ['stock', 'dropship', 'fba', 'network', 'mixed'];
  return (
    <div className="wf-inline-fields">
      <select className="wf-select-sm" value={cond.fulfilment_type_operator || 'in'} onChange={e => onChange({ fulfilment_type_operator: e.target.value })}>
        <option value="in">is one of</option>
        <option value="not_in">is not one of</option>
        <option value="equals">is exactly</option>
      </select>
      <div className="wf-checkboxes-inline">
        {types.map(t => (
          <label key={t} className="wf-checkbox-label">
            <input type="checkbox"
              checked={(cond.fulfilment_type_values || []).includes(t)}
              onChange={e => {
                const current = cond.fulfilment_type_values || [];
                const next = e.target.checked ? [...current, t] : current.filter(c => c !== t);
                onChange({ fulfilment_type_values: next });
              }}
            />
            {t}
          </label>
        ))}
      </div>
    </div>
  );
}

// ─── Action Row ───────────────────────────────────────────────────────────────

function ActionRow({ idx, action, fulfilmentSources, configuredCarriers, onChange, onRemove }: {
  idx: number; action: Action;
  fulfilmentSources: { source_id: string; name: string; type: string }[];
  configuredCarriers: { id: string; display_name: string }[];
  onChange: (patch: Partial<Action>) => void;
  onRemove: () => void;
}) {
  const [carrierServices, setCarrierServices] = useState<{ code: string; name: string; features: string[] }[]>([]);
  const [servicesLoading, setServicesLoading] = useState(false);

  // Load services when carrier changes
  useEffect(() => {
    const carrierId = action.carrier_id;
    if (!carrierId) { setCarrierServices([]); return; }
    setServicesLoading(true);
    fetch(`${API_BASE}/dispatch/carriers/${carrierId}/services`, {
      headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId() },
    })
      .then(r => r.ok ? r.json() : { services: [] })
      .then(d => setCarrierServices((d.services || []).map((s: any) => ({
        code: s.code,
        name: s.name,
        features: s.features || [],
      }))))
      .catch(() => setCarrierServices([]))
      .finally(() => setServicesLoading(false));
  }, [action.carrier_id]);

  return (
    <div className="wf-condition-row wf-action-row">
      <div className="wf-condition-label wf-action-label">THEN</div>
      <div className="wf-condition-fields">
        <select className="wf-select" value={action.type} onChange={e => onChange({ type: e.target.value })}>
          {ACTION_TYPES.map(at => <option key={at.value} value={at.value}>{at.label}</option>)}
        </select>

        {action.type === 'assign_fulfilment_source' && (
          <select className="wf-select-sm" value={action.fulfilment_source_id || ''} onChange={e => onChange({ fulfilment_source_id: e.target.value })}>
            <option value="">— Select fulfilment source —</option>
            {fulfilmentSources.map(s => (
              <option key={s.source_id} value={s.source_id}>{s.name} ({s.type})</option>
            ))}
          </select>
        )}

        {action.type === 'assign_fulfilment_network' && (
          <input className="wf-input-sm" placeholder="Network name (e.g. UK Domestic)" value={action.network_name || ''}
            onChange={e => onChange({ network_name: e.target.value })} title="Enter the exact name of the fulfilment network to use for routing" />
        )}

        {action.type === 'assign_carrier' && (
          <div className="wf-inline-fields" style={{ flexDirection: 'column', alignItems: 'stretch', gap: 8 }}>
            {/* Carrier dropdown — only configured/credentialed carriers */}
            <select
              className="wf-select-sm"
              value={action.carrier_id || ''}
              onChange={e => onChange({ carrier_id: e.target.value, service_code: '' })}
            >
              <option value="">— Select carrier —</option>
              {configuredCarriers.map(c => (
                <option key={c.id} value={c.id}>{c.display_name}</option>
              ))}
              {configuredCarriers.length === 0 && (
                <option disabled>No carriers configured — add credentials in Settings → Carriers</option>
              )}
            </select>

            {/* Service dropdown — populated from API once carrier selected */}
            {action.carrier_id && (
              <select
                className="wf-select-sm"
                value={action.service_code || ''}
                onChange={e => onChange({ service_code: e.target.value })}
                disabled={servicesLoading}
              >
                <option value="">{servicesLoading ? 'Loading services…' : '— Select service —'}</option>
                {carrierServices.map(s => (
                  <option key={s.code} value={s.code}>{s.name}</option>
                ))}
                {!servicesLoading && carrierServices.length === 0 && action.carrier_id && (
                  <option disabled>No services found</option>
                )}
              </select>
            )}

            {/* Extras — conditionally shown based on what the selected service advertises */}
            {action.carrier_id && (() => {
              const selectedSvc = carrierServices.find(s => s.code === action.service_code);
              const svcFeatures = selectedSvc?.features || [];
              const supportsSignature = svcFeatures.includes('signature');
              const supportsInsurance = svcFeatures.includes('insurance');
              if (!supportsSignature && !supportsInsurance) return null;
              return (
                <div style={{ display: 'flex', gap: 16, flexWrap: 'wrap', paddingTop: 4 }}>
                  {supportsSignature && (
                    <label style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 12, color: 'var(--text-secondary)', cursor: 'pointer' }}>
                      <input
                        type="checkbox"
                        checked={action.signature_required || false}
                        onChange={e => onChange({ signature_required: e.target.checked })}
                      />
                      Signature required
                    </label>
                  )}
                  {supportsInsurance && (
                    <label style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 12, color: 'var(--text-secondary)' }}>
                      Declared value (£)
                      <input
                        type="number"
                        min={0}
                        step={0.01}
                        placeholder="e.g. 50.00"
                        value={action.declared_value_gbp ?? ''}
                        onChange={e => onChange({ declared_value_gbp: e.target.value ? parseFloat(e.target.value) : undefined })}
                        className="wf-input-sm"
                        style={{ width: 90 }}
                      />
                    </label>
                  )}
                </div>
              );
            })()}
          </div>
        )}

        {action.type === 'add_tag' && (
          <input className="wf-input-sm" placeholder="Tag to add" value={action.tag || ''} onChange={e => onChange({ tag: e.target.value })} />
        )}

        {action.type === 'add_note' && (
          <input className="wf-input-md" placeholder="Note text" value={action.note || ''} onChange={e => onChange({ note: e.target.value })} />
        )}

        {action.type === 'set_priority' && (
          <input className="wf-input-sm" type="number" min={1} max={10} placeholder="Priority (1-10)" value={action.priority || 5}
            onChange={e => onChange({ priority: Number(e.target.value) })} />
        )}

        {action.type === 'set_status' && (
          <select className="wf-select-sm" value={action.status || ''} onChange={e => onChange({ status: e.target.value })}>
            <option value="">— Select status —</option>
            <option value="processing">Processing</option>
            <option value="on_hold">On Hold</option>
            <option value="ready_to_fulfil">Ready to Fulfil</option>
          </select>
        )}

        {action.type === 'assign_to_pickwave' && (
          <div className="wf-inline-fields" style={{ flexWrap: 'wrap', gap: 8 }}>
            <input className="wf-input-sm" placeholder="Wave name (e.g. Singles {date})" value={action.pickwave_name || ''}
              onChange={e => onChange({ pickwave_name: e.target.value })}
              title="Supports {date} and {wave_number} placeholders" />
            <select className="wf-select-sm" value={action.pickwave_grouping || 'single_order'} onChange={e => onChange({ pickwave_grouping: e.target.value })}>
              <option value="single_order">Single Order per wave</option>
              <option value="multi_order">Multi Order batches</option>
              <option value="sku_batch">SKU Batch (same products together)</option>
            </select>
            <select className="wf-select-sm" value={action.pickwave_sort_by || 'sku'} onChange={e => onChange({ pickwave_sort_by: e.target.value })}>
              <option value="sku">Sort by SKU</option>
              <option value="bin_location">Sort by Bin Location</option>
              <option value="order_date">Sort by Order Date</option>
            </select>
            <input className="wf-input-sm" type="number" min={0} placeholder="Max orders (0=∞)" value={action.pickwave_max_orders || ''}
              onChange={e => onChange({ pickwave_max_orders: Number(e.target.value) || 0 })} style={{ width: 120 }} />
            <input className="wf-input-sm" type="number" min={0} placeholder="Max items (0=∞)" value={action.pickwave_max_items || ''}
              onChange={e => onChange({ pickwave_max_items: Number(e.target.value) || 0 })} style={{ width: 120 }} />
          </div>
        )}
      </div>
      <button className="wf-remove-btn" onClick={onRemove} title="Remove action">×</button>
    </div>
  );
}

// ─── Executions View ──────────────────────────────────────────────────────────

function ExecutionsView({ wf, executions, loading, onBack }: {
  wf: Workflow; executions: any[]; loading: boolean; onBack: () => void;
}) {
  return (
    <div className="wf-page">
      <div className="wf-header">
        <div>
          <button className="btn btn-ghost btn-sm" onClick={onBack}>← Back to Workflows</button>
          <h1 className="wf-title" style={{ marginTop: 8 }}>Executions: {wf.name}</h1>
        </div>
      </div>

      {loading ? (
        <div className="wf-loading">Loading executions…</div>
      ) : executions.length === 0 ? (
        <div className="wf-empty">
          <div className="wf-empty-icon">📋</div>
          <h3>No executions yet</h3>
          <p>This workflow has not been evaluated against any orders.</p>
        </div>
      ) : (
        <div className="wf-table-wrap">
          <table className="wf-table">
            <thead>
              <tr>
                <th>Triggered</th>
                <th>Order ID</th>
                <th>Status</th>
                <th>Matched</th>
                <th>Actions Executed</th>
                <th>Duration</th>
                <th>Error</th>
              </tr>
            </thead>
            <tbody>
              {executions.map((ex: any) => (
                <tr key={ex.execution_id}>
                  <td className="wf-date">{fmt(ex.triggered_at)}</td>
                  <td className="wf-mono">{ex.order_id?.slice(0, 16) || '—'}</td>
                  <td>
                    <span className={`wf-badge ${ex.status === 'matched_executed' ? 'badge-active' : ex.status === 'failed' ? 'badge-archived' : 'badge-paused'}`}>
                      {ex.status}
                    </span>
                  </td>
                  <td>{ex.matched_workflow_name || (ex.status === 'no_match' ? 'No match' : '—')}</td>
                  <td>{ex.action_results?.length ?? 0}</td>
                  <td>{ex.duration_ms ? `${ex.duration_ms}ms` : '—'}</td>
                  <td className="wf-error-cell">{ex.error || '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

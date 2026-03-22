import React, { useState, useEffect, useCallback } from 'react';
import {
  Zap,
  Plus,
  Trash2,
  Copy,
  ToggleLeft,
  ToggleRight,
  ChevronDown,
  ChevronUp,
  Play,
  Save,
  CheckCircle,
  XCircle,
  AlertTriangle,
  Clock,
  Activity,
  Search,
  Filter,
  GripVertical,
} from 'lucide-react';
import {
  DndContext,
  closestCenter,
  PointerSensor,
  useSensor,
  useSensors,
  DragEndEvent,
} from '@dnd-kit/core';
import {
  arrayMove,
  SortableContext,
  useSortable,
  verticalListSortingStrategy,
} from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';
import RuleEditor from '../components/RuleEditor';
import { getActiveTenantId } from '../contexts/TenantContext';
import './AutomationRules.css';

const API_BASE: string =
  (import.meta as any).env?.VITE_API_URL ||
  'https://marketmate-api-487246736287.us-central1.run.app/api/v1';

// ============================================================================
// TYPES
// ============================================================================
interface MacroSchedule {
  type: 'one_time' | 'daily' | 'weekly' | 'monthly' | 'interval';
  run_at?: string;
  interval_minutes?: number;
  day_of_week?: number;
  day_of_month?: number;
  time_of_day?: string; // "HH:MM"
}

interface RuleConfig {
  id: string;
  name: string;
  enabled: boolean;
  params: Record<string, unknown>;
}

interface AutomationRule {
  rule_id: string;
  name: string;
  description?: string;
  script: string;
  triggers: string[];
  enabled: boolean;
  priority: number;
  schedule_cron?: string;
  macro_type?: string;
  parameters?: Record<string, unknown>;
  schedule?: MacroSchedule;
  configurations?: RuleConfig[];
  run_count: number;
  last_run_at?: string;
  last_run_ok: boolean;
  created_at: string;
  updated_at: string;
}

interface ValidationError {
  line: number;
  column: number;
  message: string;
  severity: string;
}

interface ConditionTrace {
  expression: string;
  result: boolean;
  value: unknown;
}

interface ActionResult {
  action: string;
  params: string[];
  dry_run?: boolean;
  skipped?: boolean;
  reason?: string;
  error?: string;
}

interface RuleResult {
  rule_index: number;
  rule_name?: string;
  matched: boolean;
  conditions_trace: ConditionTrace[];
  actions_would_fire?: ActionResult[];
  error?: string;
}

interface EvaluationReport {
  order_id?: string;
  rules_evaluated: number;
  rules_matched: number;
  results: RuleResult[];
}

// ============================================================================
// CONSTANTS
// ============================================================================

const TRIGGER_OPTIONS = [
  { value: '', label: 'All Triggers' },
  { value: 'ORDER_CREATED', label: 'Order Created' },
  { value: 'ORDER_STATUS_CHANGED', label: 'Order Status Changed' },
  { value: 'ORDER_TAGGED', label: 'Order Tagged' },
  { value: 'SHIPMENT_CREATED', label: 'Shipment Created' },
  { value: 'SHIPMENT_FAILED', label: 'Shipment Failed' },
  { value: 'INVENTORY_LOW', label: 'Inventory Low' },
  { value: 'MANUAL', label: 'Manual' },
  { value: 'SCHEDULE', label: 'Scheduled' },
];

const CRON_PRESETS: { label: string; value: string }[] = [
  { label: 'Every minute', value: '* * * * *' },
  { label: 'Every 5 minutes', value: '*/5 * * * *' },
  { label: 'Every 15 minutes', value: '*/15 * * * *' },
  { label: 'Every 30 minutes', value: '*/30 * * * *' },
  { label: 'Every hour', value: '0 * * * *' },
  { label: 'Every 6 hours', value: '0 */6 * * *' },
  { label: 'Daily at midnight', value: '0 0 * * *' },
  { label: 'Daily at 9am', value: '0 9 * * *' },
  { label: 'Weekly (Mon 9am)', value: '0 9 * * 1' },
];

function describeCron(expr: string): string {
  const preset = CRON_PRESETS.find((p) => p.value === expr);
  if (preset) return preset.label;
  const parts = expr.trim().split(/\s+/);
  if (parts.length !== 5) return 'Custom schedule';
  const [min, hr, dom, , dow] = parts;
  if (min === '*' && hr === '*') return 'Every minute';
  if (min.startsWith('*/') && hr === '*') return `Every ${min.slice(2)} minutes`;
  if (hr.startsWith('*/') && min === '0') return `Every ${hr.slice(2)} hours`;
  if (dom === '*' && dow === '*') {
    if (min === '0') return `Daily at ${hr.padStart(2, '0')}:00`;
    return `Daily at ${hr.padStart(2, '0')}:${min.padStart(2, '0')}`;
  }
  return 'Custom schedule';
}

const DEFAULT_SCRIPT = `# New automation rule
WHEN order.channel == "amazon"
  AND order.status == "imported"
THEN
  add_tag("new-amazon-order")
`;

function getApiHeaders(tenantId: string): Record<string, string> {
  const token = localStorage.getItem('auth_token') || '';
  return {
    'Content-Type': 'application/json',
    'X-Tenant-Id': tenantId,
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
  };
}

// ============================================================================
// SCRIPT PARSING — condition and action counts
// ============================================================================

const CONDITION_OPS = ['==', '!=', '>=', '<=', '>', '<', 'CONTAINS', 'MATCHES', ' IN '];

function parseScriptCounts(script: string): { conditions: number; actions: number } {
  const lines = script
    .split('\n')
    .map((l) => l.replace(/#.*$/, '').trim())
    .filter(Boolean);
  let inThen = false;
  let conditions = 0;
  let actions = 0;

  for (const line of lines) {
    const upper = line.toUpperCase();
    if (upper === 'THEN' || upper.startsWith('THEN ')) {
      inThen = true;
      const rest = line.slice(4).trim();
      if (rest) actions++;
    } else if (!inThen) {
      const condLine = upper.startsWith('WHEN ')
        ? line.slice(5)
        : upper.startsWith('AND ')
        ? line.slice(4)
        : upper.startsWith('OR ')
        ? line.slice(3)
        : '';
      if (condLine && CONDITION_OPS.some((op) => line.includes(op))) {
        conditions++;
      }
    } else {
      if (line) actions++;
    }
  }
  return { conditions, actions };
}

// ============================================================================
// SORTABLE RULE CARD
// ============================================================================

interface SortableRuleCardProps {
  rule: AutomationRule;
  isSelected: boolean;
  onSelect: (rule: AutomationRule) => void;
  onToggle: (rule: AutomationRule, e: React.MouseEvent) => void;
  onDelete: (rule: AutomationRule, e: React.MouseEvent) => void;
  onDuplicate: (rule: AutomationRule, e: React.MouseEvent) => void;
  formatTrigger: (t: string) => string;
  relativeTime: (s?: string) => string | null;
  duplicating: boolean;
}

function SortableRuleCard({
  rule,
  isSelected,
  onSelect,
  onToggle,
  onDelete,
  onDuplicate,
  formatTrigger,
  relativeTime,
  duplicating,
}: SortableRuleCardProps) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: rule.rule_id,
  });

  const style: React.CSSProperties = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.5 : 1,
  };

  const counts = parseScriptCounts(rule.script || '');
  const countLabel =
    counts.conditions > 0 || counts.actions > 0
      ? [
          counts.conditions > 0
            ? `${counts.conditions} condition${counts.conditions !== 1 ? 's' : ''}`
            : '',
          counts.actions > 0
            ? `${counts.actions} action${counts.actions !== 1 ? 's' : ''}`
            : '',
        ]
          .filter(Boolean)
          .join(' · ')
      : null;

  return (
    <div
      ref={setNodeRef}
      style={style}
      className={`rule-card ${isSelected ? 'selected' : ''} ${!rule.enabled ? 'disabled' : ''}`}
      onClick={() => onSelect(rule)}
    >
      <div className="rule-card-header">
        <div
          className="rule-card-status"
          style={{ display: 'flex', alignItems: 'center', gap: 6 }}
        >
          <span
            {...attributes}
            {...listeners}
            style={{
              cursor: isDragging ? 'grabbing' : 'grab',
              color: 'var(--text-muted)',
              display: 'flex',
              alignItems: 'center',
              flexShrink: 0,
              touchAction: 'none',
            }}
            onClick={(e) => e.stopPropagation()}
            title="Drag to reorder"
          >
            <GripVertical size={14} />
          </span>
          <span className={`rule-dot ${rule.enabled ? 'active' : 'inactive'}`} />
          <span className="rule-card-name">{rule.name}</span>
        </div>
        <div className="rule-card-actions">
          <button
            className="icon-btn"
            title="Duplicate rule"
            onClick={(e) => onDuplicate(rule, e)}
            disabled={duplicating}
          >
            <Copy size={14} />
          </button>
          <button
            className="icon-btn"
            title={rule.enabled ? 'Disable' : 'Enable'}
            onClick={(e) => onToggle(rule, e)}
          >
            {rule.enabled ? (
              <ToggleRight size={16} className="text-primary" />
            ) : (
              <ToggleLeft size={16} />
            )}
          </button>
          <button
            className="icon-btn danger"
            title="Delete"
            onClick={(e) => onDelete(rule, e)}
          >
            <Trash2 size={14} />
          </button>
        </div>
      </div>

      <div className="rule-card-meta">
        {rule.triggers?.map((t) => (
          <span key={t} className="trigger-badge">
            {formatTrigger(t)}
          </span>
        ))}
      </div>

      <div className="rule-card-stats">
        {countLabel && (
          <span className="rule-stat" title="Parsed from rule script">
            {countLabel}
          </span>
        )}
        <span className="rule-stat">
          <Activity size={11} />
          {rule.run_count ?? 0} runs
        </span>
        {rule.last_run_at && (
          <span className="rule-stat">
            <Clock size={11} />
            {relativeTime(rule.last_run_at)}
            {rule.last_run_ok ? (
              <CheckCircle size={11} className="text-success" />
            ) : (
              <XCircle size={11} className="text-danger" />
            )}
          </span>
        )}
        {!rule.enabled && <span className="rule-stat disabled-badge">DISABLED</span>}
      </div>
    </div>
  );
}

// ── MacroParamForm helper ─────────────────────────────────────────────────────
interface MacroParamField {
  key: string;
  label: string;
  type: 'text' | 'password' | 'number' | 'email' | 'checkbox';
}
function MacroParamForm({ fields, params, onChange }: { fields: MacroParamField[]; params: Record<string, unknown>; onChange: (p: Record<string, unknown>) => void }) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
      {fields.map(f => (
        <div key={f.key}>
          <label style={{ display: 'block', fontSize: 12, color: 'var(--text-secondary,#94a3b8)', marginBottom: 3 }}>{f.label}</label>
          {f.type === 'checkbox' ? (
            <label style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 13, cursor: 'pointer' }}>
              <input type="checkbox" checked={Boolean(params[f.key])} onChange={e => onChange({ ...params, [f.key]: e.target.checked })} />
              Enable
            </label>
          ) : (
            <input type={f.type} value={String(params[f.key] ?? '')} onChange={e => onChange({ ...params, [f.key]: f.type === 'number' ? Number(e.target.value) : e.target.value })}
              style={{ width: '100%', padding: '6px 8px', borderRadius: 4, background: 'var(--bg-secondary,#1e293b)', border: '1px solid var(--border,#334155)', color: 'var(--text-primary,#e2e8f0)', fontSize: 13 }}
            />
          )}
        </div>
      ))}
    </div>
  );
}

// ============================================================================
// MAIN COMPONENT
// ============================================================================
export default function AutomationRules() {
  const tenantId = getActiveTenantId() || localStorage.getItem('tenantId') || '';

  const [rules, setRules] = useState<AutomationRule[]>([]);
  const [loading, setLoading] = useState(true);
  const [selectedRule, setSelectedRule] = useState<AutomationRule | null>(null);
  const [editorScript, setEditorScript] = useState(DEFAULT_SCRIPT);
  const [editName, setEditName] = useState('New Rule');
  // Macro-specific state
  const [editMacroType, setEditMacroType] = useState('');
  const [editParameters, setEditParameters] = useState<Record<string, unknown>>({});
  const [editMacroSchedule, setEditMacroSchedule] = useState<MacroSchedule>({ type: 'daily', time_of_day: '08:00' });
  const [editConfigurations, setEditConfigurations] = useState<RuleConfig[]>([]);
  const [editorTab, setEditorTab] = useState<'script' | 'parameters' | 'schedule' | 'configurations'>('script');
  const [editTriggers, setEditTriggers] = useState<string[]>(['ORDER_CREATED']);
  const [editEnabled, setEditEnabled] = useState(true);
  const [editScheduleCron, setEditScheduleCron] = useState('0 * * * *');
  const [isNewRule, setIsNewRule] = useState(false);
  const [saving, setSaving] = useState(false);
  const [duplicating, setDuplicating] = useState(false);
  const [reorderSaving, setReorderSaving] = useState(false);
  const [saveError, setSaveError] = useState('');
  const [triggerFilter, setTriggerFilter] = useState('');
  const [enabledOnly, setEnabledOnly] = useState(false);
  const [validation, setValidation] = useState<{ valid: boolean; errors: ValidationError[] }>({
    valid: true,
    errors: [],
  });

  // Test panel
  const [testOpen, setTestOpen] = useState(false);
  const [testOrderId, setTestOrderId] = useState('');
  const [testRunning, setTestRunning] = useState(false);
  const [testReport, setTestReport] = useState<EvaluationReport | null>(null);
  const [testError, setTestError] = useState('');

  const headers = getApiHeaders(tenantId);

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
  );

  // ── DATA LOADING ──────────────────────────────────────────────────────────

  const loadRules = useCallback(async () => {
    setLoading(true);
    try {
      const url = new URL(`${API_BASE}/automation/rules`);
      if (triggerFilter) url.searchParams.set('trigger', triggerFilter);
      const resp = await fetch(url.toString(), { headers });
      if (!resp.ok) throw new Error('Failed to load rules');
      const data = await resp.json();
      let loaded: AutomationRule[] = data.rules || [];
      if (enabledOnly) loaded = loaded.filter((r) => r.enabled);
      setRules(loaded);
    } catch (e) {
      console.error(e);
    } finally {
      setLoading(false);
    }
  }, [triggerFilter, enabledOnly, tenantId]);

  useEffect(() => {
    loadRules();
  }, [loadRules]);

  // ── RULE SELECTION ────────────────────────────────────────────────────────

  function selectRule(rule: AutomationRule) {
    setSelectedRule(rule);
    setEditorScript(rule.script);
    setEditName(rule.name);
    setEditTriggers(rule.triggers || ['ORDER_CREATED']);
    setEditEnabled(rule.enabled);
    setEditScheduleCron(rule.schedule_cron || '0 * * * *');
    setEditMacroType(rule.macro_type || '');
    setEditParameters(rule.parameters || {});
    setEditMacroSchedule(rule.schedule || { type: 'daily', time_of_day: '08:00' });
    setEditConfigurations(rule.configurations || []);
    setEditorTab(rule.macro_type ? 'parameters' : 'script');
    setIsNewRule(false);
    setTestReport(null);
    setTestError('');
    setSaveError('');
  }

  function newRule() {
    setSelectedRule(null);
    setEditorScript(DEFAULT_SCRIPT);
    setEditName('New Rule');
    setEditTriggers(['ORDER_CREATED']);
    setEditEnabled(true);
    setEditScheduleCron('0 * * * *');
    setEditMacroType('');
    setEditParameters({});
    setEditMacroSchedule({ type: 'daily', time_of_day: '08:00' });
    setEditConfigurations([]);
    setEditorTab('script');
    setIsNewRule(true);
    setTestReport(null);
    setTestError('');
    setSaveError('');
  }

  // ── SAVE ──────────────────────────────────────────────────────────────────

  async function saveRule() {
    setSaving(true);
    setSaveError('');

    const isScheduled = editTriggers.includes('SCHEDULE');
    const payload: Record<string, unknown> = {
      name: editName,
      script: editorScript,
      triggers: editTriggers,
      enabled: editEnabled,
      priority: selectedRule?.priority ?? 10,
      ...(isScheduled ? { schedule_cron: editScheduleCron } : {}),
      ...(editMacroType ? { macro_type: editMacroType, parameters: editParameters } : {}),
      ...(editMacroType ? { schedule: editMacroSchedule } : {}),
      ...(editConfigurations.length > 0 ? { configurations: editConfigurations } : {}),
    };

    try {
      let resp: Response;
      if (isNewRule || !selectedRule) {
        resp = await fetch(`${API_BASE}/automation/rules`, {
          method: 'POST',
          headers,
          body: JSON.stringify(payload),
        });
      } else {
        resp = await fetch(`${API_BASE}/automation/rules/${selectedRule.rule_id}`, {
          method: 'PUT',
          headers,
          body: JSON.stringify({ ...payload, rule_id: selectedRule.rule_id }),
        });
      }

      if (!resp.ok) {
        const data = await resp.json();
        if (data.validation) {
          setSaveError(`Script errors: ${data.validation.errors?.[0]?.message || 'unknown'}`);
        } else {
          setSaveError(data.error || 'Save failed');
        }
        return;
      }

      await loadRules();
      setIsNewRule(false);
    } catch (e: unknown) {
      setSaveError(String(e));
    } finally {
      setSaving(false);
    }
  }

  // ── TOGGLE ────────────────────────────────────────────────────────────────

  async function toggleRule(rule: AutomationRule, e: React.MouseEvent) {
    e.stopPropagation();
    const newEnabled = !rule.enabled;
    await fetch(`${API_BASE}/automation/rules/${rule.rule_id}/toggle`, {
      method: 'PATCH',
      headers,
      body: JSON.stringify({ enabled: newEnabled }),
    });
    loadRules();
  }

  // ── DELETE ────────────────────────────────────────────────────────────────

  async function deleteRule(rule: AutomationRule, e: React.MouseEvent) {
    e.stopPropagation();
    if (!window.confirm(`Delete rule "${rule.name}"?`)) return;
    await fetch(`${API_BASE}/automation/rules/${rule.rule_id}`, { method: 'DELETE', headers });
    if (selectedRule?.rule_id === rule.rule_id) {
      setSelectedRule(null);
      setIsNewRule(false);
    }
    loadRules();
  }

  // ── DUPLICATE ─────────────────────────────────────────────────────────────

  async function duplicateRule(rule: AutomationRule, e: React.MouseEvent) {
    e.stopPropagation();
    setDuplicating(true);
    try {
      const resp = await fetch(`${API_BASE}/automation/rules/${rule.rule_id}/duplicate`, {
        method: 'POST',
        headers,
      });
      if (!resp.ok) {
        const data = await resp.json();
        console.error('Duplicate failed:', data.error);
        return;
      }
      await loadRules();
    } catch (err: unknown) {
      console.error('Duplicate error:', err);
    } finally {
      setDuplicating(false);
    }
  }

  // ── DRAG-TO-REORDER ───────────────────────────────────────────────────────

  async function handleDragEnd(event: DragEndEvent) {
    const { active, over } = event;
    if (!over || active.id === over.id) return;

    const oldIndex = rules.findIndex((r) => r.rule_id === String(active.id));
    const newIndex = rules.findIndex((r) => r.rule_id === String(over.id));
    if (oldIndex === -1 || newIndex === -1) return;

    const reordered = arrayMove(rules, oldIndex, newIndex);
    const updated = reordered.map((r, i) => ({ ...r, priority: i + 1 }));
    setRules(updated);

    setReorderSaving(true);
    try {
      const changed = updated.filter((r) => {
        const orig = rules.find((x) => x.rule_id === r.rule_id);
        return orig && orig.priority !== r.priority;
      });
      await Promise.all(
        changed.map((r) =>
          fetch(`${API_BASE}/automation/rules/${r.rule_id}`, {
            method: 'PUT',
            headers,
            body: JSON.stringify({
              rule_id: r.rule_id,
              name: r.name,
              script: r.script,
              triggers: r.triggers,
              enabled: r.enabled,
              priority: r.priority,
              ...(r.schedule_cron ? { schedule_cron: r.schedule_cron } : {}),
            }),
          }),
        ),
      );
    } catch (err) {
      console.error('Reorder save failed:', err);
    } finally {
      setReorderSaving(false);
    }
  }

  // ── TEST ──────────────────────────────────────────────────────────────────

  async function runTest() {
    setTestRunning(true);
    setTestReport(null);
    setTestError('');
    try {
      const resp = await fetch(`${API_BASE}/automation/rules/test`, {
        method: 'POST',
        headers,
        body: JSON.stringify({
          script: editorScript,
          order_id: testOrderId || undefined,
        }),
      });
      const data = await resp.json();
      if (!resp.ok) {
        setTestError(data.error || 'Test failed');
        return;
      }
      setTestReport(data);
    } catch (e: unknown) {
      setTestError(String(e));
    } finally {
      setTestRunning(false);
    }
  }

  // ── HELPERS ───────────────────────────────────────────────────────────────

  function formatTrigger(t: string) {
    return t.replace(/_/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase());
  }

  function relativeTime(isoString?: string): string | null {
    if (!isoString) return null;
    const diff = Date.now() - new Date(isoString).getTime();
    const mins = Math.floor(diff / 60000);
    if (mins < 1) return 'just now';
    if (mins < 60) return `${mins}m ago`;
    const hrs = Math.floor(mins / 60);
    if (hrs < 24) return `${hrs}h ago`;
    return `${Math.floor(hrs / 24)}d ago`;
  }

  const isScheduledTrigger = editTriggers.includes('SCHEDULE');

  // ============================================================================
  // RENDER
  // ============================================================================
  return (
    <div className="automation-page">
      {/* ── HEADER ── */}
      <div className="automation-header">
        <div className="automation-header-left">
          <Zap size={22} className="automation-header-icon" />
          <div>
            <h1 className="automation-title">Automation Rules</h1>
            <p className="automation-subtitle">
              DSL-based rule engine for power users and agency partners
            </p>
          </div>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          {reorderSaving && (
            <span
              style={{
                fontSize: '0.75rem',
                color: 'var(--text-muted)',
                display: 'flex',
                alignItems: 'center',
                gap: 4,
              }}
            >
              <Activity size={12} className="spin" /> Saving order…
            </span>
          )}
          <button className="btn-primary" onClick={newRule}>
            <Plus size={16} />
            New Rule
          </button>
        </div>
      </div>

      {/* ── FILTERS ── */}
      <div className="automation-filters">
        <div className="filter-group">
          <Filter size={14} className="filter-icon" />
          <select
            className="filter-select"
            value={triggerFilter}
            onChange={(e) => setTriggerFilter(e.target.value)}
          >
            {TRIGGER_OPTIONS.map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
              </option>
            ))}
          </select>
        </div>
        <label className="filter-toggle">
          <input
            type="checkbox"
            checked={enabledOnly}
            onChange={(e) => setEnabledOnly(e.target.checked)}
          />
          <span>Enabled only</span>
        </label>
      </div>

      {/* ── MAIN LAYOUT ── */}
      <div className="automation-layout">
        {/* ── RULE LIST ── */}
        <div className="rule-list-panel">
          {loading ? (
            <div className="rule-list-loading">
              <Activity size={20} className="spin" />
              <span>Loading rules…</span>
            </div>
          ) : rules.length === 0 ? (
            <div className="rule-list-empty">
              <Zap size={32} className="empty-icon" />
              <p>No rules yet</p>
              <button className="btn-ghost" onClick={newRule}>
                <Plus size={14} /> Create your first rule
              </button>
            </div>
          ) : (
            <DndContext
              sensors={sensors}
              collisionDetection={closestCenter}
              onDragEnd={handleDragEnd}
            >
              <SortableContext
                items={rules.map((r) => r.rule_id)}
                strategy={verticalListSortingStrategy}
              >
                {rules.map((rule) => (
                  <SortableRuleCard
                    key={rule.rule_id}
                    rule={rule}
                    isSelected={selectedRule?.rule_id === rule.rule_id}
                    onSelect={selectRule}
                    onToggle={toggleRule}
                    onDelete={deleteRule}
                    onDuplicate={duplicateRule}
                    formatTrigger={formatTrigger}
                    relativeTime={relativeTime}
                    duplicating={duplicating}
                  />
                ))}
              </SortableContext>
            </DndContext>
          )}

          <button className="btn-ghost full-width mt-2" onClick={newRule}>
            <Plus size={14} /> New Rule
          </button>
        </div>

        {/* ── EDITOR PANEL ── */}
        <div className="editor-panel">
          {selectedRule || isNewRule ? (
            <>
              {/* Rule meta bar */}
              <div className="editor-meta-bar">
                <input
                  className="rule-name-input"
                  value={editName}
                  onChange={(e) => setEditName(e.target.value)}
                  placeholder="Rule name…"
                />
                <div className="meta-bar-controls">
                  <div className="trigger-selector">
                    {TRIGGER_OPTIONS.slice(1).map((opt) => (
                      <label key={opt.value} className="trigger-check">
                        <input
                          type="checkbox"
                          checked={editTriggers.includes(opt.value)}
                          onChange={(e) => {
                            if (e.target.checked) {
                              setEditTriggers((prev) => [...prev, opt.value]);
                            } else {
                              setEditTriggers((prev) => prev.filter((t) => t !== opt.value));
                            }
                          }}
                        />
                        <span>{opt.label}</span>
                      </label>
                    ))}
                  </div>

                  {/* Cron expression UI — only shown when SCHEDULE is selected */}
                  {isScheduledTrigger && (
                    <div
                      style={{
                        marginTop: 8,
                        padding: '10px 12px',
                        background: 'rgba(96, 165, 250, 0.06)',
                        border: '1px solid rgba(96, 165, 250, 0.2)',
                        borderRadius: 6,
                        display: 'flex',
                        flexDirection: 'column',
                        gap: 6,
                      }}
                    >
                      <div
                        style={{ display: 'flex', alignItems: 'center', gap: 8 }}
                      >
                        <Clock size={13} style={{ color: '#60a5fa', flexShrink: 0 }} />
                        <label
                          style={{
                            fontSize: '0.72rem',
                            color: 'var(--text-muted)',
                            whiteSpace: 'nowrap',
                            fontWeight: 600,
                          }}
                        >
                          Cron expression
                        </label>
                        <input
                          value={editScheduleCron}
                          onChange={(e) => setEditScheduleCron(e.target.value)}
                          placeholder="* * * * *"
                          style={{
                            flex: 1,
                            fontFamily: 'monospace',
                            fontSize: '0.8rem',
                            padding: '3px 8px',
                            background: 'var(--bg-secondary)',
                            border: '1px solid var(--border-color)',
                            borderRadius: 4,
                            color: 'var(--text-primary)',
                          }}
                        />
                      </div>
                      <div
                        style={{
                          display: 'flex',
                          alignItems: 'center',
                          gap: 10,
                          paddingLeft: 21,
                        }}
                      >
                        <span
                          style={{
                            fontSize: '0.72rem',
                            color: '#60a5fa',
                            fontStyle: 'italic',
                          }}
                        >
                          {describeCron(editScheduleCron)}
                        </span>
                        <select
                          value=""
                          onChange={(e) => {
                            if (e.target.value) setEditScheduleCron(e.target.value);
                          }}
                          style={{
                            fontSize: '0.7rem',
                            padding: '2px 6px',
                            background: 'var(--bg-secondary)',
                            border: '1px solid var(--border-color)',
                            borderRadius: 4,
                            color: 'var(--text-muted)',
                            cursor: 'pointer',
                          }}
                        >
                          <option value="">Quick presets…</option>
                          {CRON_PRESETS.map((p) => (
                            <option key={p.value} value={p.value}>
                              {p.label} ({p.value})
                            </option>
                          ))}
                        </select>
                      </div>
                    </div>
                  )}

                  <label className="meta-toggle">
                    <input
                      type="checkbox"
                      checked={editEnabled}
                      onChange={(e) => setEditEnabled(e.target.checked)}
                    />
                    <span>Enabled</span>
                  </label>
                </div>
              </div>

              {/* Editor tabs — Script / Parameters / Schedule / Configurations */}
              <div style={{ display: 'flex', gap: 2, padding: '4px 0 0 4px', borderBottom: '1px solid var(--border, #334155)', background: 'var(--bg-secondary,#1e293b)' }}>
                {(['script','parameters','schedule','configurations'] as const).map(tab => (
                  <button key={tab} onClick={() => setEditorTab(tab)} style={{
                    padding: '5px 14px', border: 'none', cursor: 'pointer', fontSize: 12, borderRadius: '4px 4px 0 0',
                    background: editorTab === tab ? 'var(--bg-primary,#0f172a)' : 'transparent',
                    color: editorTab === tab ? 'var(--text-primary,#e2e8f0)' : 'var(--text-muted,#64748b)',
                    fontWeight: editorTab === tab ? 600 : 400,
                  }}>
                    {tab.charAt(0).toUpperCase() + tab.slice(1)}
                  </button>
                ))}
              </div>

              {editorTab === 'script' && (
                <div className="monaco-container">
                  <RuleEditor
                    value={editorScript}
                    onChange={setEditorScript}
                    tenantId={tenantId}
                    onValidationChange={setValidation}
                  />
                </div>
              )}

              {editorTab === 'parameters' && (
                <div style={{ padding: 16, overflowY: 'auto', flex: 1 }}>
                  <div style={{ marginBottom: 12 }}>
                    <label style={{ display: 'block', fontSize: 12, color: 'var(--text-secondary,#94a3b8)', marginBottom: 4 }}>Macro Type</label>
                    <select value={editMacroType} onChange={e => setEditMacroType(e.target.value)} style={{ width: '100%', padding: '6px 8px', borderRadius: 4, background: 'var(--bg-secondary,#1e293b)', border: '1px solid var(--border,#334155)', color: 'var(--text-primary,#e2e8f0)', fontSize: 13 }}>
                      <option value="">— None (DSL rule) —</option>
                      <option value="low_stock_notification">Low Stock Notification</option>
                      <option value="export_shipping_labels">Export Shipping Labels</option>
                      <option value="import_tracking">Import Tracking &amp; Process Orders</option>
                      <option value="send_emails">Send Emails via Rules Engine</option>
                      <option value="replace_diacritics">Replace Diacritics</option>
                      <option value="format_postcode">Postcode Spacing</option>
                      <option value="default_phone_number">Default Phone Number</option>
                      <option value="shipping_cost_to_service">Shipping Cost to Service</option>
                    </select>
                  </div>
                  {editMacroType === 'low_stock_notification' && (
                    <MacroParamForm fields={[
                      { key:'all_locations', label:'All locations', type:'checkbox' },
                      { key:'location_name', label:'Location name', type:'text' },
                      { key:'email_host', label:'SMTP host', type:'text' },
                      { key:'email_user', label:'SMTP user', type:'text' },
                      { key:'email_password', label:'SMTP password', type:'password' },
                      { key:'email_port', label:'SMTP port', type:'number' },
                      { key:'email_to', label:'Recipient email', type:'email' },
                    ]} params={editParameters} onChange={setEditParameters} />
                  )}
                  {editMacroType === 'export_shipping_labels' && (
                    <MacroParamForm fields={[
                      { key:'dropbox_access_token', label:'Dropbox token', type:'password' },
                      { key:'folder_path', label:'Folder path', type:'text' },
                      { key:'identifier', label:'Filename field (order_id / tracking_number)', type:'text' },
                      { key:'location', label:'Location filter', type:'text' },
                      { key:'individual_files', label:'Individual files', type:'checkbox' },
                      { key:'batch_size', label:'Batch size', type:'number' },
                    ]} params={editParameters} onChange={setEditParameters} />
                  )}
                  {editMacroType === 'import_tracking' && (
                    <MacroParamForm fields={[
                      { key:'carrier', label:'Carrier name', type:'text' },
                      { key:'location', label:'Location filter', type:'text' },
                      { key:'auto_process', label:'Auto-process dispatched orders', type:'checkbox' },
                    ]} params={editParameters} onChange={setEditParameters} />
                  )}
                  {editMacroType === 'send_emails' && (
                    <MacroParamForm fields={[
                      { key:'email_host', label:'SMTP host', type:'text' },
                      { key:'email_user', label:'SMTP user', type:'text' },
                      { key:'email_password', label:'SMTP password', type:'password' },
                      { key:'email_port', label:'SMTP port', type:'number' },
                      { key:'from_address', label:'From address', type:'email' },
                      { key:'template_id', label:'Email template ID', type:'text' },
                      { key:'trigger_event', label:'Trigger event', type:'text' },
                    ]} params={editParameters} onChange={setEditParameters} />
                  )}
                  {editMacroType === 'default_phone_number' && (
                    <MacroParamForm fields={[
                      { key:'default_number', label:'Default phone number', type:'text' },
                    ]} params={editParameters} onChange={setEditParameters} />
                  )}
                  {editMacroType === 'shipping_cost_to_service' && (
                    <div>
                      <p style={{ fontSize: 12, color: 'var(--text-secondary,#94a3b8)', marginBottom: 8 }}>Define cost ranges. Format: min:max:service_name (one per line)</p>
                      <textarea value={String(editParameters['mappings'] ?? '')} onChange={e => setEditParameters(p => ({ ...p, mappings: e.target.value }))}
                        rows={5} placeholder={"0:5:Royal Mail 1st\n5:15:DPD Next Day\n15:999:FedEx International"}
                        style={{ width: '100%', padding: '6px 8px', borderRadius: 4, background: 'var(--bg-secondary,#1e293b)', border: '1px solid var(--border,#334155)', color: 'var(--text-primary,#e2e8f0)', fontSize: 12, fontFamily: 'monospace', resize: 'vertical' }} />
                    </div>
                  )}
                  {!editMacroType && <p style={{ color: 'var(--text-muted,#64748b)', fontSize: 13 }}>Select a macro type above to configure parameters.</p>}
                </div>
              )}

              {editorTab === 'schedule' && (
                <div style={{ padding: 16, overflowY: 'auto', flex: 1 }}>
                  <div style={{ marginBottom: 12 }}>
                    <label style={{ display: 'block', fontSize: 12, color: 'var(--text-secondary,#94a3b8)', marginBottom: 4 }}>Schedule type</label>
                    <select value={editMacroSchedule.type} onChange={e => setEditMacroSchedule(s => ({ ...s, type: e.target.value as MacroSchedule['type'] }))} style={{ width: '100%', padding: '6px 8px', borderRadius: 4, background: 'var(--bg-secondary,#1e293b)', border: '1px solid var(--border,#334155)', color: 'var(--text-primary,#e2e8f0)', fontSize: 13 }}>
                      <option value="daily">Daily</option>
                      <option value="weekly">Weekly</option>
                      <option value="monthly">Monthly</option>
                      <option value="interval">Interval (minutes)</option>
                      <option value="one_time">One-time</option>
                    </select>
                  </div>
                  {(editMacroSchedule.type === 'daily' || editMacroSchedule.type === 'weekly' || editMacroSchedule.type === 'monthly') && (
                    <div style={{ marginBottom: 12 }}>
                      <label style={{ display: 'block', fontSize: 12, color: 'var(--text-secondary,#94a3b8)', marginBottom: 4 }}>Time of day (HH:MM)</label>
                      <input type="time" value={editMacroSchedule.time_of_day || '08:00'} onChange={e => setEditMacroSchedule(s => ({ ...s, time_of_day: e.target.value }))} style={{ padding: '6px 8px', borderRadius: 4, background: 'var(--bg-secondary,#1e293b)', border: '1px solid var(--border,#334155)', color: 'var(--text-primary,#e2e8f0)', fontSize: 13 }} />
                    </div>
                  )}
                  {editMacroSchedule.type === 'weekly' && (
                    <div style={{ marginBottom: 12 }}>
                      <label style={{ display: 'block', fontSize: 12, color: 'var(--text-secondary,#94a3b8)', marginBottom: 4 }}>Day of week</label>
                      <select value={editMacroSchedule.day_of_week ?? 1} onChange={e => setEditMacroSchedule(s => ({ ...s, day_of_week: Number(e.target.value) }))} style={{ padding: '6px 8px', borderRadius: 4, background: 'var(--bg-secondary,#1e293b)', border: '1px solid var(--border,#334155)', color: 'var(--text-primary,#e2e8f0)', fontSize: 13 }}>
                        {['Monday','Tuesday','Wednesday','Thursday','Friday','Saturday','Sunday'].map((d, i) => <option key={d} value={i+1}>{d}</option>)}
                      </select>
                    </div>
                  )}
                  {editMacroSchedule.type === 'monthly' && (
                    <div style={{ marginBottom: 12 }}>
                      <label style={{ display: 'block', fontSize: 12, color: 'var(--text-secondary,#94a3b8)', marginBottom: 4 }}>Day of month (1–28)</label>
                      <input type="number" min={1} max={28} value={editMacroSchedule.day_of_month ?? 1} onChange={e => setEditMacroSchedule(s => ({ ...s, day_of_month: Number(e.target.value) }))} style={{ width: 80, padding: '6px 8px', borderRadius: 4, background: 'var(--bg-secondary,#1e293b)', border: '1px solid var(--border,#334155)', color: 'var(--text-primary,#e2e8f0)', fontSize: 13 }} />
                    </div>
                  )}
                  {editMacroSchedule.type === 'interval' && (
                    <div style={{ marginBottom: 12 }}>
                      <label style={{ display: 'block', fontSize: 12, color: 'var(--text-secondary,#94a3b8)', marginBottom: 4 }}>Interval (minutes)</label>
                      <input type="number" min={1} value={editMacroSchedule.interval_minutes ?? 60} onChange={e => setEditMacroSchedule(s => ({ ...s, interval_minutes: Number(e.target.value) }))} style={{ width: 100, padding: '6px 8px', borderRadius: 4, background: 'var(--bg-secondary,#1e293b)', border: '1px solid var(--border,#334155)', color: 'var(--text-primary,#e2e8f0)', fontSize: 13 }} />
                    </div>
                  )}
                  {editMacroSchedule.type === 'one_time' && (
                    <div style={{ marginBottom: 12 }}>
                      <label style={{ display: 'block', fontSize: 12, color: 'var(--text-secondary,#94a3b8)', marginBottom: 4 }}>Run at</label>
                      <input type="datetime-local" value={editMacroSchedule.run_at || ''} onChange={e => setEditMacroSchedule(s => ({ ...s, run_at: e.target.value }))} style={{ padding: '6px 8px', borderRadius: 4, background: 'var(--bg-secondary,#1e293b)', border: '1px solid var(--border,#334155)', color: 'var(--text-primary,#e2e8f0)', fontSize: 13 }} />
                    </div>
                  )}
                </div>
              )}

              {editorTab === 'configurations' && (
                <div style={{ padding: 16, overflowY: 'auto', flex: 1 }}>
                  <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12 }}>
                    <span style={{ fontSize: 13, color: 'var(--text-secondary,#94a3b8)' }}>Named parameter configurations for this macro</span>
                    <button onClick={() => {
                      const newId = `cfg_${Date.now()}`;
                      setEditConfigurations(c => [...c, { id: newId, name: 'New Configuration', enabled: true, params: {} }]);
                    }} style={{ padding: '4px 10px', borderRadius: 4, background: 'var(--accent,#6366f1)', color: '#fff', border: 'none', cursor: 'pointer', fontSize: 12 }}>+ Add</button>
                  </div>
                  {editConfigurations.length === 0 && <p style={{ color: 'var(--text-muted,#64748b)', fontSize: 13 }}>No configurations yet. Add one above.</p>}
                  <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
                    <tbody>
                      {editConfigurations.map((cfg, i) => (
                        <tr key={cfg.id} style={{ borderBottom: '1px solid var(--border,#334155)' }}>
                          <td style={{ padding: '8px 4px' }}>
                            <input value={cfg.name} onChange={e => setEditConfigurations(c => c.map((x, j) => j === i ? { ...x, name: e.target.value } : x))} style={{ padding: '4px 6px', borderRadius: 4, background: 'var(--bg-secondary,#1e293b)', border: '1px solid var(--border,#334155)', color: 'var(--text-primary,#e2e8f0)', fontSize: 12, width: 180 }} />
                          </td>
                          <td style={{ padding: '8px 4px' }}>
                            <label style={{ display: 'flex', alignItems: 'center', gap: 4, fontSize: 12, cursor: 'pointer' }}>
                              <input type="checkbox" checked={cfg.enabled} onChange={e => setEditConfigurations(c => c.map((x, j) => j === i ? { ...x, enabled: e.target.checked } : x))} />
                              Enabled
                            </label>
                          </td>
                          <td style={{ padding: '8px 4px', textAlign: 'right' }}>
                            <button onClick={() => setEditConfigurations(c => c.filter((_, j) => j !== i))} style={{ padding: '2px 8px', borderRadius: 4, background: 'transparent', border: '1px solid var(--danger,#ef4444)', color: 'var(--danger,#ef4444)', cursor: 'pointer', fontSize: 11 }}>Delete</button>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}

              {/* Validation status + action bar */}
              <div className="editor-action-bar">
                <div className="validation-status">
                  {validation.errors.length > 0 ? (
                    <span className="val-error">
                      <XCircle size={14} />
                      {validation.errors.length} error
                      {validation.errors.length !== 1 ? 's' : ''}
                    </span>
                  ) : (
                    <span className="val-ok">
                      <CheckCircle size={14} />
                      Valid
                    </span>
                  )}
                </div>
                {saveError && <span className="save-error">{saveError}</span>}
                <div className="action-bar-buttons">
                  <button className="btn-ghost" onClick={() => setTestOpen((v) => !v)}>
                    <Play size={14} />
                    Test
                    {testOpen ? <ChevronUp size={14} /> : <ChevronDown size={14} />}
                  </button>
                  <button
                    className="btn-primary"
                    onClick={saveRule}
                    disabled={saving || validation.errors.length > 0}
                  >
                    {saving ? <Activity size={14} className="spin" /> : <Save size={14} />}
                    {saving ? 'Saving…' : 'Save'}
                  </button>
                </div>
              </div>

              {/* ── TEST PANEL ── */}
              {testOpen && (
                <div className="test-panel">
                  <div className="test-panel-header">
                    <h3>Dry Run Test</h3>
                    <div className="test-order-row">
                      <div className="test-order-input">
                        <Search size={14} />
                        <input
                          placeholder="Order ID (leave blank for sample order)"
                          value={testOrderId}
                          onChange={(e) => setTestOrderId(e.target.value)}
                        />
                      </div>
                      <button
                        className="btn-primary"
                        onClick={runTest}
                        disabled={testRunning}
                      >
                        {testRunning ? (
                          <Activity size={14} className="spin" />
                        ) : (
                          <Play size={14} />
                        )}
                        Run Dry Test
                      </button>
                    </div>
                  </div>

                  {testError && (
                    <div className="test-error">
                      <XCircle size={14} />
                      {testError}
                    </div>
                  )}

                  {testReport && (
                    <div className="test-results">
                      <div className="test-summary">
                        <span>
                          Evaluated: <strong>{testReport.rules_evaluated}</strong>
                        </span>
                        <span>
                          Matched:{' '}
                          <strong
                            className={testReport.rules_matched > 0 ? 'text-success' : ''}
                          >
                            {testReport.rules_matched}
                          </strong>
                        </span>
                        {testReport.order_id && (
                          <span>
                            Order: <code>{testReport.order_id}</code>
                          </span>
                        )}
                      </div>

                      {(testReport.results || []).map((res, i) => (
                        <div
                          key={i}
                          className={`test-rule-result ${res.matched ? 'matched' : 'unmatched'}`}
                        >
                          <div className="test-rule-header">
                            {res.matched ? (
                              <CheckCircle size={14} className="text-success" />
                            ) : (
                              <XCircle size={14} className="text-muted" />
                            )}
                            <span className="test-rule-name">
                              {res.rule_name || `Rule ${res.rule_index + 1}`}
                            </span>
                            <span
                              className={`test-rule-badge ${
                                res.matched ? 'matched' : 'unmatched'
                              }`}
                            >
                              {res.matched ? 'MATCHED' : 'NOT MATCHED'}
                            </span>
                          </div>

                          {res.error && (
                            <div className="test-rule-error">
                              <AlertTriangle size={12} /> {res.error}
                            </div>
                          )}

                          {(res.conditions_trace || []).map((trace, j) => (
                            <div
                              key={j}
                              className={`trace-row ${trace.result ? 'pass' : 'fail'}`}
                            >
                              <span className="trace-indicator">
                                {trace.result ? '✓' : '✗'}
                              </span>
                              <code className="trace-expr">{trace.expression}</code>
                              <span className="trace-value">
                                → {JSON.stringify(trace.value)}
                              </span>
                            </div>
                          ))}

                          {(res.actions_would_fire || []).map((ar, j) => (
                            <div
                              key={j}
                              className={`action-row ${ar.skipped ? 'skipped' : 'fired'}`}
                            >
                              <span className="action-indicator">
                                {ar.skipped ? '—' : '⚡'}
                              </span>
                              <code className="action-name">
                                {ar.action}(
                                {ar.params?.map((p) => `"${p}"`).join(', ')})
                              </code>
                              {ar.skipped && (
                                <span className="action-skip-reason">{ar.reason}</span>
                              )}
                              {ar.error && (
                                <span className="action-error">{ar.error}</span>
                              )}
                            </div>
                          ))}
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              )}
            </>
          ) : (
            <div className="editor-empty">
              <Zap size={40} className="empty-icon" />
              <h3>Select a rule to edit</h3>
              <p>Choose a rule from the list or create a new one to get started.</p>
              <button className="btn-primary" onClick={newRule}>
                <Plus size={16} /> New Rule
              </button>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

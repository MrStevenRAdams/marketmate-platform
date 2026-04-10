import { useState, useEffect, useCallback } from 'react';

const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';
function api(path: string, init?: RequestInit) {
  const tenantId = localStorage.getItem('active_tenant_id') || '';
  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': tenantId, ...init?.headers },
  });
}

// ── Types ─────────────────────────────────────────────────────────────────────

type Entity = 'orders' | 'products' | 'inventory' | 'rmas';

interface PivotField { key: string; label: string; }
interface PivotEntityFields { group_by_fields: PivotField[]; metrics: PivotField[]; }

interface PivotNode {
  key: string;
  label: string;
  count: number;
  metric_value: number;
  children?: PivotNode[];
}

interface PivotResponse {
  entity: Entity;
  group_by: string[];
  metrics: string[];
  date_from: string;
  date_to: string;
  nodes: PivotNode[];
  total: PivotNode;
}

// ── CSV Export ────────────────────────────────────────────────────────────────

function flattenNodes(nodes: PivotNode[], metricLabel: string, path: string[] = []): Record<string, unknown>[] {
  const rows: Record<string, unknown>[] = [];
  for (const node of nodes) {
    const currentPath = [...path, node.label];
    if (!node.children || node.children.length === 0) {
      const row: Record<string, unknown> = {};
      currentPath.forEach((p, i) => { row[`Level ${i + 1}`] = p; });
      row['Count'] = node.count;
      row[metricLabel] = node.metric_value;
      rows.push(row);
    } else {
      rows.push(...flattenNodes(node.children, metricLabel, currentPath));
    }
  }
  return rows;
}

function exportCSV(result: PivotResponse, metricLabel: string) {
  const rows = flattenNodes(result.nodes, metricLabel);
  if (rows.length === 0) return;
  const cols = Object.keys(rows[0]);
  const header = cols.join(',');
  const body = rows.map(row =>
    cols.map(c => {
      const v = String(row[c] ?? '');
      return v.includes(',') || v.includes('"') ? `"${v.replace(/"/g, '""')}"` : v;
    }).join(',')
  ).join('\n');
  const blob = new Blob([header + '\n' + body], { type: 'text/csv;charset=utf-8;' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url; a.download = `pivot_${result.entity}_${Date.now()}.csv`; a.click();
  URL.revokeObjectURL(url);
}

// ── Tree Row ──────────────────────────────────────────────────────────────────

function TreeRow({
  node,
  depth,
  metricLabel,
  isExpanded,
  onToggle,
  hasChildren,
  metricKey,
}: {
  node: PivotNode;
  depth: number;
  metricLabel: string;
  isExpanded: boolean;
  onToggle: () => void;
  hasChildren: boolean;
  metricKey: string;
}) {
  const indent = depth * 24;
  const isMoney = ['revenue', 'refund_amount', 'avg_order_value'].includes(metricKey);

  const fmtMetric = (v: number) => {
    if (isMoney) return v.toLocaleString('en-GB', { style: 'currency', currency: 'GBP', maximumFractionDigits: 2 });
    return v.toLocaleString('en-GB');
  };

  return (
    <tr style={{ borderBottom: '1px solid var(--border)', background: depth === 0 ? 'rgba(99,102,241,0.04)' : depth === 1 ? 'rgba(255,255,255,0.015)' : 'transparent' }}>
      <td style={{ padding: `10px 16px 10px ${16 + indent}px` }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          {hasChildren ? (
            <button
              onClick={onToggle}
              style={{ background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-muted)', width: 18, height: 18, display: 'flex', alignItems: 'center', justifyContent: 'center', borderRadius: 4, flexShrink: 0, fontSize: 12, padding: 0 }}
            >
              {isExpanded ? '▼' : '▶'}
            </button>
          ) : (
            <div style={{ width: 18, flexShrink: 0 }} />
          )}
          <span style={{
            fontSize: depth === 0 ? 13 : 12,
            fontWeight: depth === 0 ? 700 : depth === 1 ? 600 : 400,
            color: depth === 0 ? 'var(--text-primary)' : 'var(--text-secondary)',
          }}>
            {node.label || '(blank)'}
          </span>
        </div>
      </td>
      <td style={{ padding: '10px 16px', textAlign: 'right', color: 'var(--text-secondary)', fontWeight: depth === 0 ? 700 : 400 }}>
        {node.count.toLocaleString()}
      </td>
      <td style={{ padding: '10px 16px', textAlign: 'right', color: depth === 0 ? '#60a5fa' : 'var(--text-secondary)', fontWeight: depth === 0 ? 700 : 400 }}>
        {fmtMetric(node.metric_value)}
      </td>
    </tr>
  );
}

function NodeTree({
  nodes,
  depth = 0,
  metricLabel,
  metricKey,
}: {
  nodes: PivotNode[];
  depth?: number;
  metricLabel: string;
  metricKey: string;
}) {
  const [expanded, setExpanded] = useState<Set<string>>(new Set(nodes.map(n => n.key)));

  const toggle = (key: string) => {
    setExpanded(prev => {
      const next = new Set(prev);
      next.has(key) ? next.delete(key) : next.add(key);
      return next;
    });
  };

  return (
    <>
      {nodes.map(node => (
        <>
          <TreeRow
            key={node.key}
            node={node}
            depth={depth}
            metricLabel={metricLabel}
            isExpanded={expanded.has(node.key)}
            onToggle={() => toggle(node.key)}
            hasChildren={!!(node.children && node.children.length > 0)}
            metricKey={metricKey}
          />
          {expanded.has(node.key) && node.children && node.children.length > 0 && (
            <NodeTree
              nodes={node.children}
              depth={depth + 1}
              metricLabel={metricLabel}
              metricKey={metricKey}
            />
          )}
        </>
      ))}
    </>
  );
}

// ── Chip selector ─────────────────────────────────────────────────────────────

function ChipSelect({
  label,
  options,
  values,
  onChange,
  max,
}: {
  label: string;
  options: PivotField[];
  values: string[];
  onChange: (v: string[]) => void;
  max?: number;
}) {
  const toggle = (key: string) => {
    if (values.includes(key)) {
      onChange(values.filter(v => v !== key));
    } else {
      if (max && values.length >= max) {
        onChange([...values.slice(1), key]); // replace oldest
      } else {
        onChange([...values, key]);
      }
    }
  };

  return (
    <div>
      <div style={{ fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: 8 }}>{label} {max ? `(up to ${max})` : ''}</div>
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
        {options.map(opt => {
          const active = values.includes(opt.key);
          const idx = values.indexOf(opt.key);
          return (
            <button
              key={opt.key}
              onClick={() => toggle(opt.key)}
              style={{
                padding: '5px 12px', borderRadius: 20, border: '1px solid',
                borderColor: active ? '#6366f1' : 'var(--border)',
                background: active ? 'rgba(99,102,241,0.15)' : 'var(--bg-elevated)',
                color: active ? '#a5b4fc' : 'var(--text-muted)',
                fontSize: 12, cursor: 'pointer', fontWeight: active ? 600 : 400,
                display: 'flex', alignItems: 'center', gap: 5, transition: 'all 0.15s ease',
              }}
            >
              {active && <span style={{ width: 16, height: 16, borderRadius: '50%', background: '#6366f1', color: '#fff', fontSize: 10, display: 'flex', alignItems: 'center', justifyContent: 'center', fontWeight: 700 }}>{idx + 1}</span>}
              {opt.label}
            </button>
          );
        })}
      </div>
    </div>
  );
}

// ── Main Component ─────────────────────────────────────────────────────────────

const ENTITY_OPTIONS: { value: Entity; label: string; icon: string }[] = [
  { value: 'orders',    label: 'Orders',    icon: '📋' },
  { value: 'products',  label: 'Products',  icon: '📦' },
  { value: 'inventory', label: 'Inventory', icon: '🏭' },
  { value: 'rmas',      label: 'RMAs',      icon: '↩️' },
];

export default function PivotAnalytics() {
  const [fields, setFields] = useState<Record<string, PivotEntityFields>>({});
  const [entity, setEntity] = useState<Entity>('orders');
  const [groupBy, setGroupBy] = useState<string[]>(['channel']);
  const [metrics, setMetrics] = useState<string[]>(['count']);
  const [dateFrom, setDateFrom] = useState<string>(() => {
    const d = new Date(); d.setDate(d.getDate() - 29);
    return d.toISOString().slice(0, 10);
  });
  const [dateTo, setDateTo] = useState<string>(new Date().toISOString().slice(0, 10));
  const [result, setResult] = useState<PivotResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [hasRun, setHasRun] = useState(false);

  // Load available fields on mount
  useEffect(() => {
    api('/analytics/pivot/fields')
      .then(r => r.json())
      .then(d => setFields(d.entities || {}))
      .catch(() => {});
  }, []);

  // Reset groupBy/metrics when entity changes
  useEffect(() => {
    const ef = fields[entity];
    if (ef) {
      setGroupBy(ef.group_by_fields.slice(0, 1).map(f => f.key));
      setMetrics(ef.metrics.slice(0, 1).map(f => f.key));
    }
  }, [entity, fields]);

  const run = useCallback(async () => {
    if (groupBy.length === 0) { setError('Select at least one Group By field.'); return; }
    setLoading(true);
    setError(null);
    setHasRun(true);
    try {
      const res = await api('/analytics/pivot', {
        method: 'POST',
        body: JSON.stringify({ entity, group_by: groupBy, metrics, date_from: dateFrom, date_to: dateTo }),
      });
      if (!res.ok) throw new Error('Pivot failed');
      setResult(await res.json());
    } catch {
      setError('Failed to run pivot. Please try again.');
      setResult(null);
    } finally {
      setLoading(false);
    }
  }, [entity, groupBy, metrics, dateFrom, dateTo]);

  const ef = fields[entity];
  const metricLabel = ef?.metrics.find(m => m.key === metrics[0])?.label ?? 'Metric';
  const metricKey = metrics[0] ?? 'count';

  return (
    <div style={{ padding: '24px 28px', maxWidth: 1300, margin: '0 auto' }}>
      {/* Header */}
      <div style={{ marginBottom: 28 }}>
        <h1 style={{ fontSize: 22, fontWeight: 700, color: 'var(--text-primary)', margin: 0 }}>Pivotal Analytics</h1>
        <p style={{ fontSize: 13, color: 'var(--text-muted)', margin: '4px 0 0' }}>Multi-level data grouping and pivot table analysis across all entities</p>
      </div>

      {/* Config panel */}
      <div style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 12, padding: 24, marginBottom: 24 }}>
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 28 }}>

          {/* Left col: entity + date range */}
          <div style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
            {/* Entity selector */}
            <div>
              <div style={{ fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: 8 }}>Data Source</div>
              <div style={{ display: 'flex', gap: 8 }}>
                {ENTITY_OPTIONS.map(opt => (
                  <button
                    key={opt.value}
                    onClick={() => setEntity(opt.value)}
                    style={{
                      flex: 1, padding: '9px 12px', borderRadius: 8,
                      border: '1px solid', borderColor: entity === opt.value ? '#6366f1' : 'var(--border)',
                      background: entity === opt.value ? 'rgba(99,102,241,0.15)' : 'var(--bg-elevated)',
                      color: entity === opt.value ? '#a5b4fc' : 'var(--text-muted)',
                      fontSize: 12, cursor: 'pointer', fontWeight: entity === opt.value ? 700 : 400,
                      textAlign: 'center', transition: 'all 0.15s',
                    }}
                  >
                    <div style={{ fontSize: 18, marginBottom: 2 }}>{opt.icon}</div>
                    {opt.label}
                  </button>
                ))}
              </div>
            </div>

            {/* Date range */}
            <div>
              <div style={{ fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: 8 }}>Date Range</div>
              <div style={{ display: 'flex', gap: 10, alignItems: 'center' }}>
                <input
                  type="date"
                  value={dateFrom}
                  onChange={e => setDateFrom(e.target.value)}
                  style={{ flex: 1, padding: '8px 12px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-primary)', fontSize: 13, outline: 'none' }}
                />
                <span style={{ color: 'var(--text-muted)', fontSize: 12 }}>to</span>
                <input
                  type="date"
                  value={dateTo}
                  onChange={e => setDateTo(e.target.value)}
                  style={{ flex: 1, padding: '8px 12px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-primary)', fontSize: 13, outline: 'none' }}
                />
              </div>
              {/* Quick presets */}
              <div style={{ display: 'flex', gap: 6, marginTop: 8 }}>
                {[
                  { label: '7d', days: 7 }, { label: '30d', days: 30 },
                  { label: '90d', days: 90 }, { label: 'YTD', days: Math.floor((Date.now() - new Date(new Date().getFullYear(), 0, 1).getTime()) / 86400000) },
                ].map(p => (
                  <button
                    key={p.label}
                    onClick={() => {
                      const to = new Date().toISOString().slice(0, 10);
                      const from = new Date(Date.now() - p.days * 86400000).toISOString().slice(0, 10);
                      setDateFrom(from); setDateTo(to);
                    }}
                    style={{ padding: '4px 10px', borderRadius: 6, border: '1px solid var(--border)', background: 'var(--bg-elevated)', color: 'var(--text-muted)', fontSize: 11, cursor: 'pointer' }}
                  >
                    {p.label}
                  </button>
                ))}
              </div>
            </div>
          </div>

          {/* Right col: group by + metrics */}
          <div style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
            {ef ? (
              <>
                <ChipSelect
                  label="Group By"
                  options={ef.group_by_fields}
                  values={groupBy}
                  onChange={setGroupBy}
                  max={3}
                />
                <ChipSelect
                  label="Metrics"
                  options={ef.metrics}
                  values={metrics}
                  onChange={setMetrics}
                  max={2}
                />
              </>
            ) : (
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%', color: 'var(--text-muted)', fontSize: 13 }}>
                Loading field options…
              </div>
            )}
          </div>
        </div>

        {/* Run button */}
        <div style={{ marginTop: 24, display: 'flex', gap: 12, alignItems: 'center' }}>
          <button
            onClick={run}
            disabled={loading || groupBy.length === 0}
            style={{
              padding: '10px 28px', background: groupBy.length === 0 ? 'var(--bg-elevated)' : 'var(--primary)',
              border: 'none', borderRadius: 8, color: groupBy.length === 0 ? 'var(--text-muted)' : '#fff',
              fontSize: 14, cursor: groupBy.length === 0 || loading ? 'not-allowed' : 'pointer',
              fontWeight: 700, opacity: loading ? 0.7 : 1, transition: 'all 0.15s',
              display: 'flex', alignItems: 'center', gap: 8,
            }}
          >
            {loading ? (
              <><span style={{ display: 'inline-block', width: 14, height: 14, border: '2px solid rgba(255,255,255,0.3)', borderTopColor: '#fff', borderRadius: '50%', animation: 'spin 0.6s linear infinite' }} />Running…</>
            ) : (
              <>▶ Run Pivot</>
            )}
          </button>
          {result && !loading && (
            <button
              onClick={() => exportCSV(result, metricLabel)}
              style={{ padding: '10px 20px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-secondary)', fontSize: 13, cursor: 'pointer', display: 'flex', alignItems: 'center', gap: 6 }}
            >
              ⬇️ Export CSV
            </button>
          )}
          {error && <span style={{ fontSize: 13, color: '#f87171' }}>{error}</span>}
        </div>
      </div>

      <style>{`
        @keyframes spin { to { transform: rotate(360deg) } }
        @keyframes shimmer { 0% { background-position: -200% 0 } 100% { background-position: 200% 0 } }
      `}</style>

      {/* Results table */}
      {(hasRun || result) && (
        <div style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 12, overflow: 'hidden' }}>
          {/* Table header */}
          <div style={{ padding: '16px 20px', borderBottom: '1px solid var(--border)', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
            <div>
              <div style={{ fontWeight: 700, color: 'var(--text-primary)', fontSize: 14 }}>
                Results
                {result && <span style={{ marginLeft: 8, fontSize: 12, color: 'var(--text-muted)', fontWeight: 400 }}>
                  {result.nodes.length} top-level groups · {result.date_from} → {result.date_to}
                </span>}
              </div>
              {result && (
                <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 3 }}>
                  Grouped by: {result.group_by.map(k => ef?.group_by_fields.find(f => f.key === k)?.label ?? k).join(' → ')}
                </div>
              )}
            </div>
          </div>

          {loading ? (
            <div style={{ padding: 32 }}>
              {[1,2,3,4,5].map(i => (
                <div key={i} style={{ display: 'flex', gap: 16, padding: '13px 0', borderBottom: '1px solid var(--border)' }}>
                  <div style={{ width: `${20 + i * 5}px` }} />
                  <div style={{ width: `${150 + Math.random() * 100}px`, height: 14, borderRadius: 4, background: 'var(--bg-elevated)' }} />
                </div>
              ))}
            </div>
          ) : !result ? (
            <div style={{ textAlign: 'center', padding: 64, color: 'var(--text-muted)' }}>
              <div style={{ fontSize: 40, marginBottom: 12 }}>📊</div>
              Configure your pivot above and click Run.
            </div>
          ) : result.nodes.length === 0 ? (
            <div style={{ textAlign: 'center', padding: 64, color: 'var(--text-muted)' }}>
              <div style={{ fontSize: 40, marginBottom: 12 }}>🔍</div>
              No data found for the selected filters.
            </div>
          ) : (
            <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
              <thead>
                <tr style={{ borderBottom: '1px solid var(--border)' }}>
                  <th style={{ padding: '11px 16px', textAlign: 'left', color: 'var(--text-muted)', fontWeight: 600, fontSize: 11, textTransform: 'uppercase', letterSpacing: '0.05em', background: 'var(--bg-elevated)' }}>
                    {result.group_by.map(k => ef?.group_by_fields.find(f => f.key === k)?.label ?? k).join(' / ')}
                  </th>
                  <th style={{ padding: '11px 16px', textAlign: 'right', color: 'var(--text-muted)', fontWeight: 600, fontSize: 11, textTransform: 'uppercase', letterSpacing: '0.05em', background: 'var(--bg-elevated)' }}>Count</th>
                  <th style={{ padding: '11px 16px', textAlign: 'right', color: 'var(--text-muted)', fontWeight: 600, fontSize: 11, textTransform: 'uppercase', letterSpacing: '0.05em', background: 'var(--bg-elevated)' }}>{metricLabel}</th>
                </tr>
              </thead>
              <tbody>
                <NodeTree nodes={result.nodes} metricLabel={metricLabel} metricKey={metricKey} />

                {/* Total row */}
                <tr style={{ borderTop: '2px solid var(--border)', background: 'rgba(99,102,241,0.08)' }}>
                  <td style={{ padding: '13px 16px', fontWeight: 700, color: 'var(--text-primary)', fontSize: 13 }}>
                    Grand Total
                  </td>
                  <td style={{ padding: '13px 16px', textAlign: 'right', fontWeight: 700, color: 'var(--text-primary)' }}>
                    {result.total.count.toLocaleString()}
                  </td>
                  <td style={{ padding: '13px 16px', textAlign: 'right', fontWeight: 700, color: '#60a5fa' }}>
                    {['revenue', 'refund_amount', 'avg_order_value'].includes(metricKey)
                      ? result.total.metric_value.toLocaleString('en-GB', { style: 'currency', currency: 'GBP', maximumFractionDigits: 2 })
                      : result.total.metric_value.toLocaleString()}
                  </td>
                </tr>
              </tbody>
            </table>
          )}
        </div>
      )}
    </div>
  );
}

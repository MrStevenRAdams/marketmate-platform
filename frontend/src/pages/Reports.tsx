import { useState, useEffect, useCallback } from 'react';
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

// ─── Types ───────────────────────────────────────────────────────────────────

type Entity = 'orders' | 'products' | 'inventory' | 'rmas';

const FILTER_OPERATORS = [
  { value: 'eq', label: 'equals' },
  { value: 'neq', label: 'not equals' },
  { value: 'contains', label: 'contains' },
  { value: 'starts_with', label: 'starts with' },
  { value: 'gt', label: '>' },
  { value: 'lt', label: '<' },
  { value: 'gte', label: '>=' },
  { value: 'lte', label: '<=' },
];

const ENTITY_FIELDS: Record<Entity, string[]> = {
  orders: [
    'order_id', 'external_order_id', 'channel', 'status', 'sub_status',
    'customer_name', 'customer_email',
    'shipping_city', 'shipping_country',
    'grand_total', 'currency',
    'payment_status', 'fulfilment_source',
    'order_date', 'created_at', 'updated_at',
  ],
  products: [
    'product_id', 'sku', 'title', 'status', 'product_type',
    'brand', 'weight', 'length', 'width', 'height',
    'created_at', 'updated_at',
  ],
  inventory: [
    'inventory_id', 'sku', 'product_name',
    'total_on_hand', 'total_reserved', 'total_available', 'total_inbound',
    'safety_stock', 'reorder_point', 'updated_at',
  ],
  rmas: [
    'rma_id', 'rma_number', 'order_id', 'channel', 'status',
    'customer_name',
    'refund_action', 'refund_amount', 'refund_currency',
    'created_at',
  ],
};

const ENTITY_LABELS: Record<Entity, string> = {
  orders: 'Orders',
  products: 'Products',
  inventory: 'Inventory',
  rmas: 'RMAs',
};

interface ReportFilter {
  field: string;
  operator: string;
  value: string;
}

interface SavedReport {
  report_id: string;
  name: string;
  entity: Entity;
  filters: ReportFilter[];
  fields: string[];
  date_from?: string;
  date_to?: string;
  created_at: string;
}

// ─── CSV Export ───────────────────────────────────────────────────────────────

function exportToCSV(columns: string[], rows: Record<string, unknown>[], filename: string) {
  const header = columns.join(',');
  const body = rows.map(row =>
    columns.map(col => {
      const v = row[col] ?? '';
      const s = String(v);
      return s.includes(',') || s.includes('"') || s.includes('\n')
        ? `"${s.replace(/"/g, '""')}"` : s;
    }).join(',')
  ).join('\n');
  const blob = new Blob([header + '\n' + body], { type: 'text/csv;charset=utf-8;' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
}

// ─── Filter Row ───────────────────────────────────────────────────────────────

function FilterRow({
  filter,
  fields,
  onChange,
  onRemove,
}: {
  filter: ReportFilter;
  fields: string[];
  onChange: (f: ReportFilter) => void;
  onRemove: () => void;
}) {
  return (
    <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
      <select
        value={filter.field}
        onChange={e => onChange({ ...filter, field: e.target.value })}
        style={selectStyle}
      >
        <option value="">Select field…</option>
        {fields.map(f => <option key={f} value={f}>{f}</option>)}
      </select>
      <select
        value={filter.operator}
        onChange={e => onChange({ ...filter, operator: e.target.value })}
        style={{ ...selectStyle, width: 120 }}
      >
        {FILTER_OPERATORS.map(op => (
          <option key={op.value} value={op.value}>{op.label}</option>
        ))}
      </select>
      <input
        type="text"
        value={filter.value}
        onChange={e => onChange({ ...filter, value: e.target.value })}
        placeholder="value"
        style={{ ...inputStyle, flex: 1 }}
      />
      <button onClick={onRemove} style={removeBtn} title="Remove filter">✕</button>
    </div>
  );
}

// ─── Main Component ───────────────────────────────────────────────────────────

export default function Reports() {
  const [entity, setEntity] = useState<Entity>('orders');
  const [filters, setFilters] = useState<ReportFilter[]>([]);
  const [selectedFields, setSelectedFields] = useState<string[]>([]);
  const [dateFrom, setDateFrom] = useState('');
  const [dateTo, setDateTo] = useState('');

  const [running, setRunning] = useState(false);
  const [results, setResults] = useState<{ columns: string[]; rows: Record<string, unknown>[] } | null>(null);
  const [runError, setRunError] = useState('');

  const [savedReports, setSavedReports] = useState<SavedReport[]>([]);
  const [showSavedPanel, setShowSavedPanel] = useState(false);
  const [saveModal, setSaveModal] = useState(false);
  const [saveName, setSaveName] = useState('');
  const [saving, setSaving] = useState(false);

  const availableFields = ENTITY_FIELDS[entity];

  // When entity changes, reset fields selection to all
  useEffect(() => {
    setSelectedFields([]);
    setFilters([]);
    setResults(null);
  }, [entity]);

  const loadSavedReports = useCallback(async () => {
    try {
      const res = await api('/reports/saved');
      if (res.ok) {
        const d = await res.json();
        setSavedReports(d.reports ?? []);
      }
    } catch {}
  }, []);

  useEffect(() => { loadSavedReports(); }, [loadSavedReports]);

  const addFilter = () => {
    setFilters(f => [...f, { field: availableFields[0] ?? '', operator: 'eq', value: '' }]);
  };

  const updateFilter = (i: number, f: ReportFilter) => {
    setFilters(prev => prev.map((x, idx) => idx === i ? f : x));
  };

  const removeFilter = (i: number) => {
    setFilters(prev => prev.filter((_, idx) => idx !== i));
  };

  const toggleField = (f: string) => {
    setSelectedFields(prev =>
      prev.includes(f) ? prev.filter(x => x !== f) : [...prev, f]
    );
  };

  const selectAllFields = () => setSelectedFields([...availableFields]);
  const clearAllFields = () => setSelectedFields([]);

  const runReport = async () => {
    setRunning(true);
    setRunError('');
    setResults(null);
    try {
      const res = await api('/reports/run', {
        method: 'POST',
        body: JSON.stringify({
          entity,
          filters: filters.filter(f => f.field && f.value !== undefined),
          fields: selectedFields,
          date_from: dateFrom || undefined,
          date_to: dateTo || undefined,
        }),
      });
      const d = await res.json();
      if (!res.ok) {
        setRunError(d.error ?? 'Failed to run report');
        return;
      }
      setResults({ columns: d.columns ?? [], rows: d.rows ?? [] });
    } catch (e: any) {
      setRunError(e.message);
    } finally {
      setRunning(false);
    }
  };

  const saveReport = async () => {
    if (!saveName.trim()) return;
    setSaving(true);
    try {
      const res = await api('/reports/saved', {
        method: 'POST',
        body: JSON.stringify({
          name: saveName.trim(),
          entity,
          filters,
          fields: selectedFields,
          date_from: dateFrom || undefined,
          date_to: dateTo || undefined,
        }),
      });
      if (res.ok) {
        setSaveModal(false);
        setSaveName('');
        loadSavedReports();
      }
    } catch {}
    finally { setSaving(false); }
  };

  const loadSavedReport = (r: SavedReport) => {
    setEntity(r.entity);
    setFilters(r.filters ?? []);
    setSelectedFields(r.fields ?? []);
    setDateFrom(r.date_from ?? '');
    setDateTo(r.date_to ?? '');
    setShowSavedPanel(false);
    setResults(null);
  };

  const exportCSV = () => {
    if (!results) return;
    exportToCSV(
      results.columns,
      results.rows,
      `${entity}-report-${new Date().toISOString().slice(0, 10)}.csv`
    );
  };

  const displayColumns = results?.columns ?? [];
  const displayRows = results?.rows ?? [];

  return (
    <div style={{ padding: '24px 32px', maxWidth: 1280, margin: '0 auto' }}>

      {/* ── Page Header ── */}
      <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', marginBottom: 28 }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 24, fontWeight: 700, color: 'var(--text-primary)' }}>Report Builder</h1>
          <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 14 }}>
            Build custom queries across orders, products, inventory, and RMAs.
          </p>
        </div>
        <button style={btnGhostStyle} onClick={() => setShowSavedPanel(!showSavedPanel)}>
          📂 Saved Reports {savedReports.length > 0 && (
            <span style={{
              marginLeft: 6, background: 'var(--primary)', color: 'white',
              borderRadius: 10, padding: '1px 7px', fontSize: 11,
            }}>{savedReports.length}</span>
          )}
        </button>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: showSavedPanel ? '1fr 300px' : '1fr', gap: 20 }}>
        {/* ── Builder Panel ── */}
        <div>
          {/* Entity Selector */}
          <div style={{ ...sectionCard, marginBottom: 16 }}>
            <div style={{ fontSize: 11, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.06em', color: 'var(--text-muted)', marginBottom: 12 }}>
              Data Source
            </div>
            <div style={{ display: 'flex', gap: 8 }}>
              {(Object.keys(ENTITY_LABELS) as Entity[]).map(e => (
                <button
                  key={e}
                  onClick={() => setEntity(e)}
                  style={{
                    ...pillStyle,
                    background: entity === e ? 'var(--primary)' : 'var(--bg-elevated)',
                    color: entity === e ? 'white' : 'var(--text-secondary)',
                    borderColor: entity === e ? 'var(--primary)' : 'var(--border)',
                  }}
                >
                  {ENTITY_LABELS[e]}
                </button>
              ))}
            </div>
          </div>

          {/* Date Range */}
          <div style={{ ...sectionCard, marginBottom: 16 }}>
            <div style={{ fontSize: 11, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.06em', color: 'var(--text-muted)', marginBottom: 12 }}>
              Date Range <span style={{ fontWeight: 400, textTransform: 'none', letterSpacing: 0 }}>(optional — filters on created_at)</span>
            </div>
            <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
              <input type="date" value={dateFrom} onChange={e => setDateFrom(e.target.value)} style={{ ...inputStyle, width: 160 }} />
              <span style={{ color: 'var(--text-muted)', fontSize: 13 }}>to</span>
              <input type="date" value={dateTo} onChange={e => setDateTo(e.target.value)} style={{ ...inputStyle, width: 160 }} />
              {(dateFrom || dateTo) && (
                <button onClick={() => { setDateFrom(''); setDateTo(''); }} style={{ ...removeBtn, fontSize: 11 }}>
                  Clear
                </button>
              )}
            </div>
          </div>

          {/* Filters */}
          <div style={{ ...sectionCard, marginBottom: 16 }}>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 12 }}>
              <div style={{ fontSize: 11, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.06em', color: 'var(--text-muted)' }}>
                Filters
              </div>
              <button onClick={addFilter} style={{ ...btnGhostStyle, fontSize: 12, padding: '4px 10px' }}>
                + Add Filter
              </button>
            </div>
            {filters.length === 0 && (
              <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>
                No filters — all records will be returned. Click "+ Add Filter" to narrow results.
              </div>
            )}
            <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
              {filters.map((f, i) => (
                <FilterRow
                  key={i}
                  filter={f}
                  fields={availableFields}
                  onChange={updated => updateFilter(i, updated)}
                  onRemove={() => removeFilter(i)}
                />
              ))}
            </div>
          </div>

          {/* Column Selector */}
          <div style={{ ...sectionCard, marginBottom: 20 }}>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 12 }}>
              <div style={{ fontSize: 11, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.06em', color: 'var(--text-muted)' }}>
                Columns to Include
              </div>
              <div style={{ display: 'flex', gap: 8 }}>
                <button onClick={selectAllFields} style={{ ...btnGhostStyle, fontSize: 11, padding: '3px 10px' }}>All</button>
                <button onClick={clearAllFields} style={{ ...btnGhostStyle, fontSize: 11, padding: '3px 10px' }}>None</button>
              </div>
            </div>
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
              {availableFields.map(f => {
                const active = selectedFields.length === 0 || selectedFields.includes(f);
                return (
                  <button
                    key={f}
                    onClick={() => toggleField(f)}
                    style={{
                      padding: '4px 10px', borderRadius: 4, fontSize: 12,
                      fontFamily: 'monospace',
                      background: active ? 'rgba(59,130,246,0.15)' : 'var(--bg-elevated)',
                      color: active ? 'var(--primary-light)' : 'var(--text-muted)',
                      border: `1px solid ${active ? 'rgba(59,130,246,0.4)' : 'var(--border)'}`,
                      cursor: 'pointer',
                    }}
                  >
                    {f}
                  </button>
                );
              })}
            </div>
            {selectedFields.length === 0 && (
              <p style={{ margin: '8px 0 0', fontSize: 11, color: 'var(--text-muted)' }}>
                All columns selected (no selection = all).
              </p>
            )}
          </div>

          {/* Action Row */}
          <div style={{ display: 'flex', gap: 10, marginBottom: 24 }}>
            <button
              onClick={runReport}
              disabled={running}
              style={{ ...btnPrimaryStyle, padding: '10px 24px', fontSize: 14 }}
            >
              {running ? '⏳ Running…' : '▶ Run Report'}
            </button>
            {results && (
              <>
                <button onClick={exportCSV} style={btnGhostStyle}>
                  ↓ Export CSV
                </button>
                <button onClick={() => setSaveModal(true)} style={btnGhostStyle}>
                  💾 Save Report
                </button>
              </>
            )}
          </div>

          {runError && <div style={errorStyle}>{runError}</div>}

          {/* Results Table */}
          {results && (
            <div style={sectionCard}>
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 16 }}>
                <div>
                  <span style={{ fontWeight: 600, color: 'var(--text-primary)', fontSize: 15 }}>Results</span>
                  <span style={{ marginLeft: 10, fontSize: 13, color: 'var(--text-muted)' }}>
                    {displayRows.length.toLocaleString()} row{displayRows.length !== 1 ? 's' : ''}
                  </span>
                </div>
                <button onClick={exportCSV} style={{ ...btnGhostStyle, fontSize: 12, padding: '5px 12px' }}>
                  ↓ Export CSV
                </button>
              </div>

              {displayRows.length === 0 ? (
                <div style={{ padding: '32px 0', textAlign: 'center', color: 'var(--text-muted)', fontSize: 14 }}>
                  No records matched your filters.
                </div>
              ) : (
                <div style={{ overflowX: 'auto', maxHeight: 520, overflowY: 'auto' }}>
                  <table style={{ width: '100%', borderCollapse: 'collapse', minWidth: 600 }}>
                    <thead style={{ position: 'sticky', top: 0, background: 'var(--bg-secondary)', zIndex: 1 }}>
                      <tr>
                        {displayColumns.map(col => (
                          <th key={col} style={thStyle}>{col}</th>
                        ))}
                      </tr>
                    </thead>
                    <tbody>
                      {displayRows.map((row, i) => (
                        <tr key={i} style={{ borderBottom: '1px solid var(--border)' }}>
                          {displayColumns.map(col => (
                            <td key={col} style={tdStyle}>
                              {row[col] !== null && row[col] !== undefined
                                ? String(row[col])
                                : <span style={{ color: 'var(--text-muted)' }}>—</span>
                              }
                            </td>
                          ))}
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </div>
          )}
        </div>

        {/* ── Saved Reports Panel ── */}
        {showSavedPanel && (
          <div style={{ ...sectionCard, height: 'fit-content' }}>
            <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--text-primary)', marginBottom: 14 }}>
              Saved Reports
            </div>
            {savedReports.length === 0 ? (
              <div style={{ fontSize: 13, color: 'var(--text-muted)' }}>No saved reports yet.</div>
            ) : (
              <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                {savedReports.map(r => (
                  <button
                    key={r.report_id}
                    onClick={() => loadSavedReport(r)}
                    style={{
                      padding: '10px 12px', background: 'var(--bg-elevated)',
                      border: '1px solid var(--border)', borderRadius: 6,
                      cursor: 'pointer', textAlign: 'left', width: '100%',
                    }}
                  >
                    <div style={{ fontSize: 13, fontWeight: 500, color: 'var(--text-primary)', marginBottom: 2 }}>
                      {r.name}
                    </div>
                    <div style={{ fontSize: 11, color: 'var(--text-muted)' }}>
                      {ENTITY_LABELS[r.entity]} · {r.filters?.length ?? 0} filter{r.filters?.length !== 1 ? 's' : ''}
                    </div>
                    <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 2 }}>
                      {r.created_at?.slice(0, 10)}
                    </div>
                  </button>
                ))}
              </div>
            )}
          </div>
        )}
      </div>

      {/* ── Save Modal ── */}
      {saveModal && (
        <div style={overlayStyle} onClick={() => setSaveModal(false)}>
          <div style={modalStyle} onClick={e => e.stopPropagation()}>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 20 }}>
              <h2 style={{ margin: 0, fontSize: 17, fontWeight: 600, color: 'var(--text-primary)' }}>Save Report</h2>
              <button onClick={() => setSaveModal(false)} style={closeBtn}>✕</button>
            </div>
            <div style={{ marginBottom: 16 }}>
              <label style={labelStyle}>Report Name</label>
              <input
                type="text"
                value={saveName}
                onChange={e => setSaveName(e.target.value)}
                placeholder={`${ENTITY_LABELS[entity]} report ${new Date().toLocaleDateString('en-GB')}`}
                style={{ ...inputStyle, width: '100%', boxSizing: 'border-box' }}
                autoFocus
                onKeyDown={e => e.key === 'Enter' && saveReport()}
              />
            </div>
            <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
              <button onClick={() => setSaveModal(false)} style={btnGhostStyle} disabled={saving}>Cancel</button>
              <button onClick={saveReport} style={btnPrimaryStyle} disabled={saving || !saveName.trim()}>
                {saving ? 'Saving…' : 'Save'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

// ─── Styles ───────────────────────────────────────────────────────────────────

const sectionCard: React.CSSProperties = {
  padding: 20,
  background: 'var(--bg-secondary)',
  border: '1px solid var(--border)',
  borderRadius: 10,
};

const pillStyle: React.CSSProperties = {
  padding: '6px 16px', borderRadius: 20, border: '1px solid',
  cursor: 'pointer', fontSize: 13, fontWeight: 500,
};

const inputStyle: React.CSSProperties = {
  padding: '7px 10px', background: 'var(--bg-elevated)',
  border: '1px solid var(--border)', borderRadius: 6,
  color: 'var(--text-primary)', fontSize: 13, outline: 'none',
};

const selectStyle: React.CSSProperties = {
  padding: '7px 10px', background: 'var(--bg-elevated)',
  border: '1px solid var(--border)', borderRadius: 6,
  color: 'var(--text-primary)', fontSize: 13, outline: 'none',
  flex: 1,
};

const removeBtn: React.CSSProperties = {
  background: 'none', border: '1px solid var(--border)', borderRadius: 4,
  color: 'var(--text-muted)', cursor: 'pointer', fontSize: 12,
  padding: '4px 8px', lineHeight: 1,
};

const btnPrimaryStyle: React.CSSProperties = {
  padding: '8px 16px', background: 'var(--primary)', color: 'white',
  border: 'none', borderRadius: 6, cursor: 'pointer', fontSize: 13, fontWeight: 600,
};

const btnGhostStyle: React.CSSProperties = {
  padding: '8px 16px', background: 'transparent', color: 'var(--text-secondary)',
  border: '1px solid var(--border)', borderRadius: 6, cursor: 'pointer', fontSize: 13,
};

const errorStyle: React.CSSProperties = {
  marginBottom: 16, padding: '10px 14px', background: 'rgba(239,68,68,0.1)',
  border: '1px solid rgba(239,68,68,0.3)', borderRadius: 6,
  color: 'var(--danger)', fontSize: 13,
};

const thStyle: React.CSSProperties = {
  padding: '10px 14px', textAlign: 'left', fontSize: 11, fontWeight: 600,
  textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--text-muted)',
  borderBottom: '1px solid var(--border)', whiteSpace: 'nowrap',
};

const tdStyle: React.CSSProperties = {
  padding: '10px 14px', color: 'var(--text-secondary)', fontSize: 12,
  maxWidth: 240, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
};

const labelStyle: React.CSSProperties = {
  display: 'block', marginBottom: 6, fontSize: 13, fontWeight: 500, color: 'var(--text-secondary)',
};

const overlayStyle: React.CSSProperties = {
  position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.6)',
  display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000,
};

const modalStyle: React.CSSProperties = {
  background: 'var(--bg-secondary)', border: '1px solid var(--border)',
  borderRadius: 12, padding: 24, width: 400, maxWidth: '90vw',
};

const closeBtn: React.CSSProperties = {
  background: 'none', border: 'none', color: 'var(--text-muted)',
  cursor: 'pointer', fontSize: 18, lineHeight: 1, padding: 4,
};

import { useState, useEffect, useCallback, useRef } from 'react';
import { getActiveTenantId } from '../contexts/TenantContext';
import { SerialNumberInput, SerialRequiredBadge } from '../components/SerialNumberInput';

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

interface StockCountLine {
  line_id: string;
  product_id: string;
  sku: string;
  product_name: string;
  expected: number;
  counted?: number;
  variance?: number;
  notes?: string;
  counted_at?: string;
  counted_by?: string;
  batch_number?: string;
  expiry_date?: string;
  use_serial_numbers?: boolean;   // populated from product flag
}

interface StockCountSession {
  count_id: string;
  name: string;
  status: 'in_progress' | 'committed' | 'cancelled';
  location_id: string;
  location_name: string;
  lines?: StockCountLine[];
  notes?: string;
  total_skus: number;
  counted_skus: number;
  variances: number;
  created_at: string;
  committed_at?: string;
}

interface Location {
  location_id: string;
  name: string;
  path: string;
  is_leaf: boolean;
}

// ─── Create Count Modal ───────────────────────────────────────────────────────

function CreateCountModal({
  locations,
  onClose,
  onCreated,
}: {
  locations: Location[];
  onClose: () => void;
  onCreated: () => void;
}) {
  const [name, setName] = useState(`Stock Count ${new Date().toLocaleDateString('en-GB')}`);
  const [locationId, setLocationId] = useState('');
  const [notes, setNotes] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  const save = async () => {
    if (!name.trim()) { setError('Name is required'); return; }
    if (!locationId) { setError('Select a location'); return; }
    setSaving(true);
    setError('');
    try {
      const res = await api('/stock-counts', {
        method: 'POST',
        body: JSON.stringify({ name: name.trim(), location_id: locationId, notes }),
      });
      if (!res.ok) {
        const d = await res.json();
        throw new Error(d.error || 'Failed to create stock count');
      }
      onCreated();
    } catch (e: any) {
      setError(e.message);
      setSaving(false);
    }
  };

  return (
    <div style={overlayStyle} onClick={onClose}>
      <div style={modalStyle} onClick={e => e.stopPropagation()}>
        <div style={modalHeaderStyle}>
          <h2 style={{ margin: 0, fontSize: 18, fontWeight: 600, color: 'var(--text-primary)' }}>
            New Stock Count
          </h2>
          <button style={closeButtonStyle} onClick={onClose}>✕</button>
        </div>
        {error && <div style={errorStyle}>{error}</div>}
        <div style={{ padding: '0 24px 16px' }}>
          <div style={fieldStyle}>
            <label style={labelStyle}>Count Name <span style={{ color: 'var(--danger)' }}>*</span></label>
            <input
              style={inputStyle}
              value={name}
              onChange={e => setName(e.target.value)}
              placeholder="e.g. Monthly Full Count — January"
            />
          </div>
          <div style={fieldStyle}>
            <label style={labelStyle}>Location <span style={{ color: 'var(--danger)' }}>*</span></label>
            <select style={inputStyle} value={locationId} onChange={e => setLocationId(e.target.value)}>
              <option value="">— Select location —</option>
              {locations.filter(l => l.is_leaf).map(l => (
                <option key={l.location_id} value={l.location_id}>{l.path || l.name}</option>
              ))}
            </select>
            <p style={{ margin: '4px 0 0', fontSize: 12, color: 'var(--text-muted)' }}>
              Only leaf locations can be counted. All current stock at this location will be loaded.
            </p>
          </div>
          <div style={fieldStyle}>
            <label style={labelStyle}>Notes</label>
            <input
              style={inputStyle}
              value={notes}
              onChange={e => setNotes(e.target.value)}
              placeholder="Optional notes…"
            />
          </div>
        </div>
        <div style={modalFooterStyle}>
          <button style={btnGhostStyle} onClick={onClose} disabled={saving}>Cancel</button>
          <button style={btnPrimaryStyle} onClick={save} disabled={saving}>
            {saving ? 'Creating…' : 'Start Count'}
          </button>
        </div>
      </div>
    </div>
  );
}

// ─── Count Detail ─────────────────────────────────────────────────────────────

function CountDetail({
  session,
  onUpdate,
  onCommit,
  onCancel,
}: {
  session: StockCountSession;
  onUpdate: () => void;
  onCommit: () => void;
  onCancel: () => void;
}) {
  const [lines, setLines] = useState<StockCountLine[]>(session.lines || []);
  const [counts, setCounts] = useState<Record<string, string>>({});
  const [lineNotes, setLineNotes] = useState<Record<string, string>>({});
  const [saving, setSaving] = useState<string | null>(null);
  const [committing, setCommitting] = useState(false);
  const [search, setSearch] = useState('');
  const [showVariancesOnly, setShowVariancesOnly] = useState(false);
  // H-004: barcode scanner support
  const [scanMode, setScanMode] = useState(false);
  const [countMode, setCountMode] = useState<'simple' | 'batched'>('simple');
  const [batchNumbers, setBatchNumbers] = useState<Record<string, string>>({});
  const [expiryDates, setExpiryDates] = useState<Record<string, string>>({});
  const [scanHighlight, setScanHighlight] = useState<string | null>(null);
  const scanInputRef = useRef<HTMLInputElement>(null);
  // Serial tracking: map of line_id → collected serial numbers
  const [serialsByLine, setSerialsByLine] = useState<Record<string, string[]>>({});

  useEffect(() => {
    setLines(session.lines || []);
    const initCounts: Record<string, string> = {};
    (session.lines || []).forEach(l => {
      if (l.counted !== undefined && l.counted !== null) {
        initCounts[l.line_id] = String(l.counted);
      }
    });
    setCounts(initCounts);
  }, [session.count_id]);

  const saveLine = async (line: StockCountLine) => {
    const countedStr = counts[line.line_id];
    if (countedStr === undefined || countedStr === '') return;
    const counted = parseInt(countedStr);
    if (isNaN(counted) || counted < 0) return;

    setSaving(line.line_id);
    try {
      const res = await api(`/stock-counts/${session.count_id}/lines`, {
        method: 'POST',
        body: JSON.stringify({
          line_id: line.line_id,
          counted,
          notes: lineNotes[line.line_id] || '',
          serial_numbers: line.use_serial_numbers ? (serialsByLine[line.line_id] || []) : undefined,
        }),
      });
      if (res.ok) {
        const data = await res.json();
        setLines(data.stock_count.lines || []);
        onUpdate();
      }
    } finally {
      setSaving(null);
    }
  };

  const handleCommit = async () => {
    if (!confirm(`Commit this count? This will apply ${session.variances} variance adjustments to stock levels.`)) return;
    setCommitting(true);
    try {
      const res = await api(`/stock-counts/${session.count_id}/commit`, { method: 'POST' });
      if (res.ok) onCommit();
    } finally {
      setCommitting(false);
    }
  };

  const filtered = lines.filter(l => {
    if (showVariancesOnly && (l.variance === undefined || l.variance === 0)) return false;
    if (search && !l.sku.toLowerCase().includes(search.toLowerCase()) &&
        !l.product_name.toLowerCase().includes(search.toLowerCase())) return false;
    return true;
  });

  const totalVariance = lines.reduce((sum, l) => sum + Math.abs(l.variance || 0), 0);
  const progressPct = session.total_skus > 0
    ? Math.round((session.counted_skus / session.total_skus) * 100) : 0;

  const isActive = session.status === 'in_progress';

  // H-004: handle a barcode/SKU scan
  const handleScan = (value: string) => {
    const trimmed = value.trim();
    if (!trimmed) return;
    const match = lines.find(l => l.sku.toLowerCase() === trimmed.toLowerCase())
      || lines.find(l => l.sku.toLowerCase().includes(trimmed.toLowerCase()));
    if (match) {
      setScanHighlight(match.line_id);
      if (!counts[match.line_id]) setCounts(p => ({ ...p, [match.line_id]: '1' }));
      setTimeout(() => {
        document.getElementById('count-row-' + match.line_id)?.scrollIntoView({ behavior: 'smooth', block: 'center' });
        (document.getElementById('count-input-' + match.line_id) as HTMLInputElement | null)?.focus();
      }, 50);
      setTimeout(() => setScanHighlight(null), 2000);
    }
  };

  return (
    <div style={{ padding: '0 0 24px' }}>
      {/* Progress bar */}
      <div style={{ padding: '16px 24px', borderBottom: '1px solid var(--border)', background: 'var(--bg-secondary)' }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 8 }}>
          <div style={{ display: 'flex', gap: 24 }}>
            <span style={{ color: 'var(--text-secondary)', fontSize: 13 }}>
              <strong style={{ color: 'var(--text-primary)' }}>{session.counted_skus}</strong>/{session.total_skus} counted
            </span>
            <span style={{ color: session.variances > 0 ? 'var(--warning)' : 'var(--text-muted)', fontSize: 13 }}>
              <strong>{session.variances}</strong> variances ({totalVariance} units)
            </span>
          </div>
          {isActive && (
            <div style={{ display: 'flex', gap: 8 }}>
              <button
                style={{ ...btnGhostStyle, fontSize: 13 }}
                onClick={() => { if (confirm('Cancel this stock count?')) onCancel(); }}
              >
                Cancel Count
              </button>
              <button
                style={{
                  ...btnPrimaryStyle,
                  fontSize: 13,
                  opacity: session.counted_skus === 0 ? 0.5 : 1,
                }}
                onClick={handleCommit}
                disabled={committing || session.counted_skus === 0}
              >
                {committing ? 'Committing…' : `Commit Count (${session.variances} adjustments)`}
              </button>
            </div>
          )}
        </div>
        <div style={{ height: 6, background: 'var(--bg-tertiary)', borderRadius: 3, overflow: 'hidden' }}>
          <div style={{
            height: '100%',
            width: `${progressPct}%`,
            background: session.status === 'committed' ? 'var(--success)' : 'var(--primary)',
            borderRadius: 3,
            transition: 'width 0.3s ease',
          }} />
        </div>
      </div>

      {/* H-004: Barcode scan bar */}
      {isActive && (
        <div style={{ padding: '8px 24px', display: 'flex', gap: 10, alignItems: 'center', background: scanMode ? 'rgba(99,102,241,0.06)' : 'transparent', borderBottom: '1px solid var(--border)', transition: 'background 0.2s' }}>
          <button
            onClick={() => { const next = !scanMode; setScanMode(next); if (next) setTimeout(() => scanInputRef.current?.focus(), 50); }}
            style={{ padding: '5px 12px', borderRadius: 6, fontSize: 12, fontWeight: 600, cursor: 'pointer', background: scanMode ? 'rgba(99,102,241,0.2)' : 'var(--bg-elevated)', border: scanMode ? '1px solid rgba(99,102,241,0.5)' : '1px solid var(--border)', color: scanMode ? '#818cf8' : 'var(--text-secondary)' }}
          >
            &#128247; Scan Mode {scanMode ? 'ON' : 'OFF'}
          </button>
          {scanMode && (
            <input
              ref={scanInputRef}
              type="text"
              placeholder="Scan barcode or type SKU, press Enter&#8230;"
              autoFocus
              style={{ flex: 1, padding: '5px 12px', borderRadius: 6, fontSize: 13, background: 'var(--bg-elevated)', border: '1px solid rgba(99,102,241,0.4)', color: 'var(--text-primary)', outline: 'none' }}
              onKeyDown={e => { if (e.key === 'Enter') { handleScan((e.target as HTMLInputElement).value); (e.target as HTMLInputElement).value = ''; } }}
            />
          )}
          {!scanMode && <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>Enable scan mode to use a barcode scanner for fast counting</span>}
          <div style={{ marginLeft: 'auto', display: 'flex', gap: 4 }}>
            {(['simple', 'batched'] as const).map(m => (
              <button key={m} onClick={() => setCountMode(m)}
                style={{ padding: '4px 12px', borderRadius: 6, fontSize: 12, fontWeight: 600, cursor: 'pointer',
                  background: countMode === m ? 'rgba(99,102,241,0.2)' : 'var(--bg-elevated)',
                  border: countMode === m ? '1px solid rgba(99,102,241,0.5)' : '1px solid var(--border)',
                  color: countMode === m ? '#818cf8' : 'var(--text-secondary)' }}>
                {m === 'simple' ? '☰ Simple' : '🏷️ Batched'}
              </button>
            ))}
          </div>
        </div>
      )}
      {/* Filters */}
      <div style={{ padding: '12px 24px', display: 'flex', gap: 12, alignItems: 'center', borderBottom: '1px solid var(--border)' }}>
        <input
          style={{ ...inputStyle, width: 240, margin: 0 }}
          placeholder="Search SKU or product…"
          value={search}
          onChange={e => setSearch(e.target.value)}
        />
        <label style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 13, color: 'var(--text-secondary)', cursor: 'pointer' }}>
          <input
            type="checkbox"
            checked={showVariancesOnly}
            onChange={e => setShowVariancesOnly(e.target.checked)}
          />
          Variances only
        </label>
        <span style={{ marginLeft: 'auto', fontSize: 12, color: 'var(--text-muted)' }}>
          {filtered.length} of {lines.length} lines
        </span>
      </div>

      {/* Lines table */}
      <div style={{ overflowX: 'auto' }}>
        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
          <thead>
            <tr style={{ background: 'var(--bg-tertiary)' }}>
              <th style={thStyle}>SKU</th>
              <th style={thStyle}>Product</th>
              <th style={{ ...thStyle, textAlign: 'center' }}>Expected</th>
              <th style={{ ...thStyle, textAlign: 'center' }}>Counted</th>
              <th style={{ ...thStyle, textAlign: 'center' }}>Variance</th>
              {countMode === 'batched' && <th style={thStyle}>Batch No.</th>}
              {countMode === 'batched' && <th style={thStyle}>Expiry</th>}
              {isActive && <th style={thStyle}>Notes</th>}
              {isActive && <th style={{ ...thStyle, width: 80 }}></th>}
            </tr>
          </thead>
          <tbody>
            {filtered.map(line => {
              const countedStr = counts[line.line_id] ?? '';
              const counted = countedStr !== '' ? parseInt(countedStr) : undefined;
              const variance = counted !== undefined ? counted - line.expected : line.variance;
              const hasVariance = variance !== undefined && variance !== 0;
              const isSaving = saving === line.line_id;

              return (
                <tr
                  key={line.line_id}
                  id={'count-row-' + line.line_id}
                  style={{
                    borderBottom: '1px solid var(--border)',
                    background: scanHighlight === line.line_id ? 'rgba(99,102,241,0.12)' : hasVariance ? 'rgba(245,158,11,0.04)' : 'transparent',
                    transition: 'background 0.3s',
                  }}>
                  <td style={{ ...tdStyle, fontFamily: 'monospace', color: 'var(--accent-cyan)' }}>
                    {line.sku}
                    {line.use_serial_numbers && <> <SerialRequiredBadge /></>}
                  </td>
                  <td style={{ ...tdStyle, color: 'var(--text-secondary)' }}>
                    {line.product_name || '—'}
                  </td>
                  <td style={{ ...tdStyle, textAlign: 'center', fontWeight: 600 }}>
                    {line.expected}
                  </td>
                  <td style={{ ...tdStyle, textAlign: 'center', minWidth: line.use_serial_numbers ? 200 : 'auto' }}>
                    {isActive ? (
                      line.use_serial_numbers ? (
                        // Serial products: count is derived from entered serials
                        <SerialNumberInput
                          productId={line.product_id}
                          quantity={line.expected}
                          value={serialsByLine[line.line_id] || []}
                          onChange={sns => {
                            setSerialsByLine(p => ({ ...p, [line.line_id]: sns }));
                            // Keep counts in sync so variance calc works
                            setCounts(p => ({ ...p, [line.line_id]: String(sns.length) }));
                          }}
                          compact={true}
                        />
                      ) : (
                        <input
                          id={'count-input-' + line.line_id}
                          type="number"
                          min="0"
                          style={{
                            width: 70,
                            padding: '4px 8px',
                            background: 'var(--bg-elevated)',
                            border: '1px solid var(--border)',
                            borderRadius: 4,
                            color: 'var(--text-primary)',
                            textAlign: 'center',
                            fontSize: 13,
                          }}
                          value={countedStr}
                          onChange={e => setCounts(p => ({ ...p, [line.line_id]: e.target.value }))}
                          onKeyDown={e => { if (e.key === 'Enter') saveLine(line); }}
                        />
                      )
                    ) : (
                      <span style={{ fontWeight: 600 }}>
                        {line.counted !== undefined ? line.counted : '—'}
                      </span>
                    )}
                  </td>
                  <td style={{ ...tdStyle, textAlign: 'center' }}>
                    {variance !== undefined && variance !== null ? (
                      <span style={{
                        padding: '2px 8px',
                        borderRadius: 12,
                        fontSize: 12,
                        fontWeight: 600,
                        background: variance === 0 ? 'rgba(16,185,129,0.15)' : variance > 0 ? 'rgba(59,130,246,0.15)' : 'rgba(239,68,68,0.15)',
                        color: variance === 0 ? 'var(--success)' : variance > 0 ? 'var(--primary)' : 'var(--danger)',
                      }}>
                        {variance === 0 ? '✓' : variance > 0 ? `+${variance}` : variance}
                      </span>
                    ) : '—'}
                  </td>
                  {countMode === 'batched' && (
                    <td style={tdStyle}>
                      {isActive ? (
                        <input style={{ width: 90, padding: '4px 6px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 4, color: 'var(--text-primary)', fontSize: 12 }}
                          placeholder="Batch…"
                          value={batchNumbers[line.line_id] || ''}
                          onChange={e => setBatchNumbers(p => ({ ...p, [line.line_id]: e.target.value }))} />
                      ) : (line.batch_number || '—')}
                    </td>
                  )}
                  {countMode === 'batched' && (
                    <td style={tdStyle}>
                      {isActive ? (
                        <input type="date" style={{ padding: '4px 6px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 4, color: 'var(--text-primary)', fontSize: 12 }}
                          value={expiryDates[line.line_id] || ''}
                          onChange={e => setExpiryDates(p => ({ ...p, [line.line_id]: e.target.value }))} />
                      ) : (line.expiry_date ? new Date(line.expiry_date).toLocaleDateString() : '—')}
                    </td>
                  )}
                  {isActive && (
                    <td style={tdStyle}>
                      <input
                        style={{ ...inputStyle, margin: 0, fontSize: 12, padding: '4px 8px' }}
                        placeholder="Note…"
                        value={lineNotes[line.line_id] || ''}
                        onChange={e => setLineNotes(p => ({ ...p, [line.line_id]: e.target.value }))}
                      />
                    </td>
                  )}
                  {isActive && (
                    <td style={{ ...tdStyle, textAlign: 'center' }}>
                      <button
                        style={{
                          padding: '4px 10px',
                          background: 'var(--primary)',
                          color: 'white',
                          border: 'none',
                          borderRadius: 4,
                          cursor: isSaving ? 'not-allowed' : 'pointer',
                          fontSize: 12,
                          opacity: countedStr === '' ? 0.4 : 1,
                        }}
                        disabled={isSaving || countedStr === ''}
                        onClick={() => saveLine(line)}
                      >
                        {isSaving ? '…' : 'Save'}
                      </button>
                    </td>
                  )}
                </tr>
              );
            })}
            {filtered.length === 0 && (
              <tr>
                <td colSpan={7} style={{ padding: '32px', textAlign: 'center', color: 'var(--text-muted)' }}>
                  No lines match your filters
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}

// ─── Main Page ────────────────────────────────────────────────────────────────

export default function StockCount() {
  const [sessions, setSessions] = useState<StockCountSession[]>([]);
  const [locations, setLocations] = useState<Location[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [showCreate, setShowCreate] = useState(false);
  const [expanded, setExpanded] = useState<string | null>(null);
  const [expandedData, setExpandedData] = useState<StockCountSession | null>(null);
  const [statusFilter, setStatusFilter] = useState('');

  const loadSessions = useCallback(async () => {
    setLoading(true);
    try {
      const qs = statusFilter ? `?status=${statusFilter}` : '';
      const [scRes, locRes] = await Promise.all([
        api(`/stock-counts${qs}`),
        api('/locations'),
      ]);
      if (scRes.ok) {
        const d = await scRes.json();
        setSessions(d.stock_counts || []);
      }
      if (locRes.ok) {
        const d = await locRes.json();
        setLocations(d.locations || []);
      }
    } catch (e: any) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }, [statusFilter]);

  useEffect(() => { loadSessions(); }, [loadSessions]);

  const expandSession = async (id: string) => {
    if (expanded === id) { setExpanded(null); setExpandedData(null); return; }
    setExpanded(id);
    try {
      const res = await api(`/stock-counts/${id}`);
      if (res.ok) {
        const d = await res.json();
        setExpandedData(d.stock_count);
      }
    } catch {}
  };

  const handleCommitOrCancel = async () => {
    setExpanded(null);
    setExpandedData(null);
    loadSessions();
  };

  const statusConfig: Record<string, { label: string; color: string }> = {
    in_progress: { label: 'In Progress', color: 'var(--warning)' },
    committed:   { label: 'Committed',   color: 'var(--success)' },
    cancelled:   { label: 'Cancelled',   color: 'var(--text-muted)' },
  };

  return (
    <div style={{ padding: '24px 32px', maxWidth: 1200, margin: '0 auto' }}>
      {/* Header */}
      <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', marginBottom: 24 }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 24, fontWeight: 700, color: 'var(--text-primary)' }}>Stock Count</h1>
          <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 14 }}>
            Conduct stock counts and reconcile variances with your inventory records.
          </p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <button style={btnGhostStyle} onClick={loadSessions}>↻ Refresh</button>
          <button style={btnPrimaryStyle} onClick={() => setShowCreate(true)}>+ New Stock Count</button>
        </div>
      </div>

      {/* Status filter pills */}
      <div style={{ display: 'flex', gap: 8, marginBottom: 20 }}>
        {[{ value: '', label: 'All' }, { value: 'in_progress', label: 'In Progress' }, { value: 'committed', label: 'Committed' }, { value: 'cancelled', label: 'Cancelled' }].map(opt => (
          <button
            key={opt.value}
            onClick={() => setStatusFilter(opt.value)}
            style={{
              padding: '5px 14px',
              borderRadius: 20,
              border: '1px solid',
              borderColor: statusFilter === opt.value ? 'var(--primary)' : 'var(--border)',
              background: statusFilter === opt.value ? 'rgba(59,130,246,0.15)' : 'transparent',
              color: statusFilter === opt.value ? 'var(--primary)' : 'var(--text-secondary)',
              cursor: 'pointer',
              fontSize: 13,
              fontWeight: statusFilter === opt.value ? 600 : 400,
            }}
          >
            {opt.label}
          </button>
        ))}
      </div>

      {error && <div style={errorStyle}>{error}</div>}

      {loading ? (
        <div style={{ padding: '48px', textAlign: 'center', color: 'var(--text-muted)' }}>Loading stock counts…</div>
      ) : sessions.length === 0 ? (
        <div style={{
          padding: '64px 32px',
          textAlign: 'center',
          background: 'var(--bg-secondary)',
          borderRadius: 12,
          border: '1px solid var(--border)',
        }}>
          <div style={{ fontSize: 48, marginBottom: 16 }}>📋</div>
          <h3 style={{ margin: '0 0 8px', color: 'var(--text-primary)' }}>No stock counts yet</h3>
          <p style={{ color: 'var(--text-muted)', marginBottom: 24 }}>
            Create a stock count to reconcile your physical inventory against records.
          </p>
          <button style={btnPrimaryStyle} onClick={() => setShowCreate(true)}>+ New Stock Count</button>
        </div>
      ) : (
        <div style={{
          background: 'var(--bg-secondary)',
          borderRadius: 12,
          border: '1px solid var(--border)',
          overflow: 'hidden',
        }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
            <thead>
              <tr style={{ background: 'var(--bg-tertiary)' }}>
                <th style={thStyle}>Name</th>
                <th style={thStyle}>Location</th>
                <th style={thStyle}>Status</th>
                <th style={{ ...thStyle, textAlign: 'center' }}>Progress</th>
                <th style={{ ...thStyle, textAlign: 'center' }}>Variances</th>
                <th style={thStyle}>Date</th>
                <th style={{ ...thStyle, width: 40 }}></th>
              </tr>
            </thead>
            <tbody>
              {sessions.map(s => {
                const sc = statusConfig[s.status] || { label: s.status, color: 'var(--text-muted)' };
                const pct = s.total_skus > 0 ? Math.round((s.counted_skus / s.total_skus) * 100) : 0;
                const isExp = expanded === s.count_id;

                return (
                  <>
                    <tr
                      key={s.count_id}
                      style={{
                        borderBottom: '1px solid var(--border)',
                        cursor: 'pointer',
                        background: isExp ? 'rgba(59,130,246,0.05)' : 'transparent',
                      }}
                      onClick={() => expandSession(s.count_id)}
                    >
                      <td style={tdStyle}>
                        <span style={{ fontWeight: 600, color: 'var(--text-primary)' }}>{s.name}</span>
                      </td>
                      <td style={{ ...tdStyle, color: 'var(--text-secondary)' }}>{s.location_name}</td>
                      <td style={tdStyle}>
                        <span style={{
                          padding: '2px 8px',
                          borderRadius: 12,
                          fontSize: 11,
                          fontWeight: 600,
                          background: `${sc.color}20`,
                          color: sc.color,
                          border: `1px solid ${sc.color}40`,
                        }}>
                          {sc.label}
                        </span>
                      </td>
                      <td style={{ ...tdStyle, textAlign: 'center' }}>
                        <div style={{ display: 'flex', alignItems: 'center', gap: 8, justifyContent: 'center' }}>
                          <div style={{ width: 60, height: 4, background: 'var(--bg-tertiary)', borderRadius: 2, overflow: 'hidden' }}>
                            <div style={{ height: '100%', width: `${pct}%`, background: 'var(--primary)', borderRadius: 2 }} />
                          </div>
                          <span style={{ color: 'var(--text-muted)', fontSize: 12 }}>{s.counted_skus}/{s.total_skus}</span>
                        </div>
                      </td>
                      <td style={{ ...tdStyle, textAlign: 'center' }}>
                        {s.variances > 0 ? (
                          <span style={{ color: 'var(--warning)', fontWeight: 600 }}>{s.variances}</span>
                        ) : (
                          <span style={{ color: 'var(--text-muted)' }}>0</span>
                        )}
                      </td>
                      <td style={{ ...tdStyle, color: 'var(--text-muted)' }}>
                        {new Date(s.created_at).toLocaleDateString('en-GB', { day: '2-digit', month: 'short', year: 'numeric' })}
                      </td>
                      <td style={{ ...tdStyle, textAlign: 'center', color: 'var(--text-muted)' }}>
                        {isExp ? '▲' : '▼'}
                      </td>
                    </tr>
                    {isExp && expandedData && expandedData.count_id === s.count_id && (
                      <tr key={`${s.count_id}-detail`}>
                        <td colSpan={7} style={{ padding: 0, background: 'var(--bg-primary)' }}>
                          <CountDetail
                            session={expandedData}
                            onUpdate={async () => {
                              const res = await api(`/stock-counts/${s.count_id}`);
                              if (res.ok) { const d = await res.json(); setExpandedData(d.stock_count); }
                              loadSessions();
                            }}
                            onCommit={handleCommitOrCancel}
                            onCancel={async () => {
                              await api(`/stock-counts/${s.count_id}/cancel`, { method: 'POST' });
                              handleCommitOrCancel();
                            }}
                          />
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

      {showCreate && (
        <CreateCountModal
          locations={locations}
          onClose={() => setShowCreate(false)}
          onCreated={() => { setShowCreate(false); loadSessions(); }}
        />
      )}
    </div>
  );
}

// ─── Shared Styles ─────────────────────────────────────────────────────────────

const overlayStyle: React.CSSProperties = {
  position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.6)', display: 'flex',
  alignItems: 'center', justifyContent: 'center', zIndex: 1000,
};
const modalStyle: React.CSSProperties = {
  background: 'var(--bg-secondary)', borderRadius: 12, border: '1px solid var(--border)',
  width: 480, maxWidth: '95vw', maxHeight: '90vh', overflowY: 'auto',
};
const modalHeaderStyle: React.CSSProperties = {
  display: 'flex', alignItems: 'center', justifyContent: 'space-between',
  padding: '20px 24px 16px', borderBottom: '1px solid var(--border)',
};
const modalFooterStyle: React.CSSProperties = {
  display: 'flex', justifyContent: 'flex-end', gap: 8,
  padding: '16px 24px', borderTop: '1px solid var(--border)',
};
const closeButtonStyle: React.CSSProperties = {
  background: 'none', border: 'none', color: 'var(--text-muted)',
  cursor: 'pointer', fontSize: 18, lineHeight: 1, padding: 4,
};
const fieldStyle: React.CSSProperties = { marginTop: 16 };
const labelStyle: React.CSSProperties = {
  display: 'block', marginBottom: 6, fontSize: 13, fontWeight: 500, color: 'var(--text-secondary)',
};
const inputStyle: React.CSSProperties = {
  width: '100%', padding: '8px 12px', background: 'var(--bg-elevated)',
  border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)',
  fontSize: 14, outline: 'none', boxSizing: 'border-box',
};
const errorStyle: React.CSSProperties = {
  margin: '0 24px 16px', padding: '10px 14px', background: 'rgba(239,68,68,0.1)',
  border: '1px solid rgba(239,68,68,0.3)', borderRadius: 6, color: 'var(--danger)', fontSize: 13,
};
const btnPrimaryStyle: React.CSSProperties = {
  padding: '8px 16px', background: 'var(--primary)', color: 'white', border: 'none',
  borderRadius: 6, cursor: 'pointer', fontSize: 13, fontWeight: 600,
};
const btnGhostStyle: React.CSSProperties = {
  padding: '8px 16px', background: 'transparent', color: 'var(--text-secondary)',
  border: '1px solid var(--border)', borderRadius: 6, cursor: 'pointer', fontSize: 13,
};
const thStyle: React.CSSProperties = {
  padding: '10px 16px', textAlign: 'left', fontSize: 11, fontWeight: 600,
  textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--text-muted)',
  borderBottom: '1px solid var(--border)',
};
const tdStyle: React.CSSProperties = {
  padding: '12px 16px', color: 'var(--text-primary)',
};

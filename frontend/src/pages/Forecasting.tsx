import { useState, useEffect, useCallback } from 'react';
import { getActiveTenantId } from '../contexts/TenantContext';

const API_BASE = import.meta.env.VITE_API_URL ?? 'http://localhost:8080/api/v1';
function api(path: string, init?: RequestInit) {
  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId(), ...init?.headers },
  });
}

// ─── Types ───────────────────────────────────────────────────────────────────

interface ForecastingSettings {
  default_lookback_days: number;
  default_lead_time_days: number;
  default_safety_days: number;
  auto_recalc_enabled: boolean;
}

interface ProductForecast {
  product_id: string;
  sku: string;
  product_name: string;
  lookback_days?: number;
  lead_time_days?: number;
  safety_days?: number;
  avg_daily_consumption?: number;
  calculated_adc: number;
  current_stock: number;
  days_of_stock: number;
  reorder_point: number;
  reorder_qty: number;
  forecast_status: 'ok' | 'low' | 'critical' | 'out_of_stock' | 'unconfigured';
  last_calculated_at?: string;
}

interface DashboardData {
  summary: { total_skus: number; out_of_stock: number; critical: number; low: number; healthy: number };
  critical_items: ProductForecast[];
  low_items: ProductForecast[];
}

const STATUS_CONFIG = {
  ok:           { label: 'Healthy',       color: 'var(--success)',  bg: 'rgba(16,185,129,0.12)' },
  low:          { label: 'Low Stock',     color: 'var(--warning)',  bg: 'rgba(245,158,11,0.12)' },
  critical:     { label: 'Critical',      color: 'var(--accent-orange)', bg: 'rgba(249,115,22,0.12)' },
  out_of_stock: { label: 'Out of Stock',  color: 'var(--danger)',   bg: 'rgba(239,68,68,0.12)' },
  unconfigured: { label: 'Unconfigured',  color: 'var(--text-muted)', bg: 'rgba(100,116,139,0.12)' },
};

// ─── Channel Demand Types ─────────────────────────────────────────────────────

interface ChannelVelocity {
  channel: string;
  velocity_30d: number;
  order_count: number;
}

interface ChannelDemandItem {
  product_id: string;
  sku: string;
  product_name: string;
  current_stock: number;
  reorder_point: number;
  lead_time_days: number;
  suggested_reorder_qty: number;
  top_channel: string;
  is_below_threshold: boolean;
  snoozed: boolean;
  snooze_until?: string;
  channel_velocities: ChannelVelocity[];
}

interface ChannelDemandResponse {
  items: ChannelDemandItem[];
  generated_at: string;
}

// ─── Settings Modal ───────────────────────────────────────────────────────────

function SettingsModal({ settings, onClose, onSaved }: {
  settings: ForecastingSettings;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [lookback, setLookback] = useState(settings.default_lookback_days);
  const [leadTime, setLeadTime] = useState(settings.default_lead_time_days);
  const [safety, setSafety] = useState(settings.default_safety_days);
  const [autoRecalc, setAutoRecalc] = useState(settings.auto_recalc_enabled);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  const save = async () => {
    setSaving(true); setError('');
    try {
      const res = await api('/forecasting/settings', {
        method: 'PUT',
        body: JSON.stringify({
          default_lookback_days: lookback,
          default_lead_time_days: leadTime,
          default_safety_days: safety,
          auto_recalc_enabled: autoRecalc,
        }),
      });
      if (!res.ok) throw new Error((await res.json()).error || 'Failed');
      onSaved();
    } catch (e: any) { setError(e.message); setSaving(false); }
  };

  return (
    <div style={overlayStyle} onClick={onClose}>
      <div style={modalStyle} onClick={e => e.stopPropagation()}>
        <div style={modalHeaderStyle}>
          <h2 style={modalTitleStyle}>Forecasting Settings</h2>
          <button style={closeBtnStyle} onClick={onClose}>✕</button>
        </div>
        {error && <div style={errorStyle}>{error}</div>}
        <div style={{ padding: '0 24px 8px' }}>
          <div style={fieldStyle}>
            <label style={labelStyle}>Default Lookback Period (days)</label>
            <input type="number" min="7" max="365" style={inputStyle}
              value={lookback} onChange={e => setLookback(parseInt(e.target.value) || 90)} />
            <p style={hintStyle}>How many days of sales history to use when calculating average daily consumption (ADC). Unlike some platforms, you can set this up to 365 days.</p>
          </div>
          <div style={fieldStyle}>
            <label style={labelStyle}>Default Lead Time (days)</label>
            <input type="number" min="1" style={inputStyle}
              value={leadTime} onChange={e => setLeadTime(parseInt(e.target.value) || 14)} />
            <p style={hintStyle}>Typical supplier lead time. Used to calculate when to trigger reorder alerts.</p>
          </div>
          <div style={fieldStyle}>
            <label style={labelStyle}>Safety Buffer (days)</label>
            <input type="number" min="0" style={inputStyle}
              value={safety} onChange={e => setSafety(parseInt(e.target.value) || 7)} />
            <p style={hintStyle}>Extra days of buffer stock beyond lead time. Reorder point = (lead time + safety) × ADC.</p>
          </div>
          <label style={{ display: 'flex', alignItems: 'center', gap: 10, marginTop: 20, cursor: 'pointer' }}>
            <input type="checkbox" checked={autoRecalc} onChange={e => setAutoRecalc(e.target.checked)} />
            <span style={{ fontSize: 13, color: 'var(--text-secondary)' }}>Auto-recalculate daily</span>
          </label>
        </div>
        <div style={modalFooterStyle}>
          <button style={btnGhostStyle} onClick={onClose} disabled={saving}>Cancel</button>
          <button style={btnPrimaryStyle} onClick={save} disabled={saving}>
            {saving ? 'Saving…' : 'Save Settings'}
          </button>
        </div>
      </div>
    </div>
  );
}

// ─── Product Config Modal ─────────────────────────────────────────────────────

function ProductConfigModal({ forecast, globalSettings, onClose, onSaved }: {
  forecast: ProductForecast;
  globalSettings: ForecastingSettings;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [lookback, setLookback] = useState<string>(forecast.lookback_days?.toString() || '');
  const [leadTime, setLeadTime] = useState<string>(forecast.lead_time_days?.toString() || '');
  const [safety, setSafety] = useState<string>(forecast.safety_days?.toString() || '');
  const [manualADC, setManualADC] = useState<string>(forecast.avg_daily_consumption?.toString() || '');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  const save = async () => {
    setSaving(true); setError('');
    try {
      const body: Record<string, number | null> = {};
      if (lookback) body.lookback_days = parseInt(lookback);
      if (leadTime) body.lead_time_days = parseInt(leadTime);
      if (safety) body.safety_days = parseInt(safety);
      if (manualADC) body.avg_daily_consumption = parseFloat(manualADC);

      const res = await api(`/forecasting/products/${forecast.product_id}`, {
        method: 'PUT',
        body: JSON.stringify(body),
      });
      if (!res.ok) throw new Error((await res.json()).error || 'Failed');
      onSaved();
    } catch (e: any) { setError(e.message); setSaving(false); }
  };

  const ph = (def: number) => `Default: ${def}`;

  return (
    <div style={overlayStyle} onClick={onClose}>
      <div style={modalStyle} onClick={e => e.stopPropagation()}>
        <div style={modalHeaderStyle}>
          <div>
            <h2 style={modalTitleStyle}>{forecast.product_name || forecast.sku}</h2>
            <p style={{ margin: '2px 0 0', fontSize: 12, color: 'var(--accent-cyan)', fontFamily: 'monospace' }}>{forecast.sku}</p>
          </div>
          <button style={closeBtnStyle} onClick={onClose}>✕</button>
        </div>
        {error && <div style={errorStyle}>{error}</div>}
        <div style={{ padding: '0 24px 8px' }}>
          <p style={{ margin: '0 0 16px', fontSize: 13, color: 'var(--text-muted)' }}>
            Leave fields blank to use global defaults. Per-product settings override global settings.
          </p>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
            <div style={fieldStyle}>
              <label style={labelStyle}>Lookback (days)</label>
              <input type="number" min="7" max="365" style={inputStyle}
                value={lookback} onChange={e => setLookback(e.target.value)}
                placeholder={ph(globalSettings.default_lookback_days)} />
            </div>
            <div style={fieldStyle}>
              <label style={labelStyle}>Lead Time (days)</label>
              <input type="number" min="1" style={inputStyle}
                value={leadTime} onChange={e => setLeadTime(e.target.value)}
                placeholder={ph(globalSettings.default_lead_time_days)} />
            </div>
            <div style={fieldStyle}>
              <label style={labelStyle}>Safety Buffer (days)</label>
              <input type="number" min="0" style={inputStyle}
                value={safety} onChange={e => setSafety(e.target.value)}
                placeholder={ph(globalSettings.default_safety_days)} />
            </div>
            <div style={fieldStyle}>
              <label style={labelStyle}>Manual ADC override</label>
              <input type="number" min="0" step="0.01" style={inputStyle}
                value={manualADC} onChange={e => setManualADC(e.target.value)}
                placeholder={`Calculated: ${forecast.calculated_adc || '—'}`} />
            </div>
          </div>
          {forecast.calculated_adc > 0 && (
            <div style={{
              marginTop: 20, padding: '12px 16px', background: 'var(--bg-tertiary)',
              borderRadius: 8, border: '1px solid var(--border)',
            }}>
              <p style={{ margin: 0, fontSize: 12, color: 'var(--text-muted)', fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: 10 }}>
                Current Calculations
              </p>
              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, 1fr)', gap: 12 }}>
                {[
                  { label: 'ADC', value: forecast.calculated_adc?.toFixed(2) },
                  { label: 'Days of Stock', value: forecast.days_of_stock === 999 ? '∞' : forecast.days_of_stock?.toFixed(0) },
                  { label: 'Reorder Point', value: forecast.reorder_point },
                  { label: 'Reorder Qty', value: forecast.reorder_qty },
                ].map(m => (
                  <div key={m.label} style={{ textAlign: 'center' }}>
                    <div style={{ fontSize: 18, fontWeight: 700, color: 'var(--text-primary)' }}>{m.value}</div>
                    <div style={{ fontSize: 11, color: 'var(--text-muted)' }}>{m.label}</div>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
        <div style={modalFooterStyle}>
          <button style={btnGhostStyle} onClick={onClose} disabled={saving}>Cancel</button>
          <button style={btnPrimaryStyle} onClick={save} disabled={saving}>
            {saving ? 'Saving…' : 'Save & Recalculate'}
          </button>
        </div>
      </div>
    </div>
  );
}

// ─── Reorder Alerts Tab ───────────────────────────────────────────────────────

function ReorderAlertsTab() {
  const [data, setData] = useState<ChannelDemandResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [snoozing, setSnoozing] = useState<Set<string>>(new Set());
  const [error, setError] = useState('');

  const load = async () => {
    setLoading(true);
    try {
      const res = await api('/forecasting/channel-demand');
      if (!res.ok) throw new Error('Failed to load channel demand');
      const d = await res.json();
      setData(d);
    } catch (e: any) { setError(e.message); } finally { setLoading(false); }
  };

  useEffect(() => { load(); }, []);

  const snooze = async (productId: string) => {
    setSnoozing(prev => new Set([...prev, productId]));
    await api(`/forecasting/reorder-alerts/${productId}/snooze`, { method: 'POST', body: '{}' });
    setSnoozing(prev => { const s = new Set(prev); s.delete(productId); return s; });
    load();
  };

  const toggleExpand = (id: string) => {
    setExpanded(prev => {
      const s = new Set(prev);
      s.has(id) ? s.delete(id) : s.add(id);
      return s;
    });
  };

  if (loading) return <div style={{ padding: '48px', textAlign: 'center', color: 'var(--text-muted)' }}>Loading channel demand...</div>;
  if (error) return <div style={errorStyle}>{error}</div>;
  if (!data) return null;

  const alerts = (data.items || []).filter(i => i.is_below_threshold && !i.snoozed);
  const snoozed = (data.items || []).filter(i => i.snoozed);
  const allItems = data.items || [];

  // Summary stats
  const topChannelCounts: Record<string, number> = {};
  allItems.forEach(i => { if (i.top_channel) topChannelCounts[i.top_channel] = (topChannelCounts[i.top_channel] || 0) + 1; });
  const topChannel = Object.entries(topChannelCounts).sort((a, b) => b[1] - a[1])[0]?.[0] || '—';

  return (
    <div>
      {/* Summary cards */}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: '12px', marginBottom: '24px' }}>
        {[
          { label: 'Active Alerts', value: alerts.length, color: alerts.length > 0 ? 'var(--danger)' : 'var(--success)' },
          { label: 'Snoozed', value: snoozed.length, color: 'var(--text-muted)' },
          { label: 'Top Demand Channel', value: topChannel, color: 'var(--accent-cyan)' },
        ].map(stat => (
          <div key={stat.label} style={{ padding: '16px 20px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: '10px', borderLeftColor: stat.color, borderLeftWidth: 3 }}>
            <div style={{ fontSize: '24px', fontWeight: 700, color: stat.color, textTransform: 'capitalize' }}>{stat.value}</div>
            <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginTop: '2px' }}>{stat.label}</div>
          </div>
        ))}
      </div>

      {/* Alerts Table */}
      {alerts.length > 0 && (
        <div style={{ marginBottom: '32px' }}>
          <h3 style={{ margin: '0 0 12px', fontSize: '15px', fontWeight: 600, color: 'var(--danger)' }}>🚨 Reorder Alerts ({alerts.length})</h3>
          <div style={{ background: 'var(--bg-secondary)', borderRadius: '10px', border: '1px solid var(--border)', overflow: 'hidden' }}>
            <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '13px' }}>
              <thead><tr style={{ background: 'var(--bg-tertiary)' }}>
                {['SKU', 'Product', 'Stock', 'Reorder Pt.', 'Lead Time', 'Top Channel', 'Suggested Qty', 'Actions'].map(h => (
                  <th key={h} style={thStyle}>{h}</th>
                ))}
              </tr></thead>
              <tbody>
                {alerts.map(item => (
                  <tr key={item.product_id} style={{ borderBottom: '1px solid var(--border)' }}>
                    <td style={{ ...tdStyle, fontFamily: 'monospace', color: 'var(--accent-cyan)', fontSize: '12px' }}>{item.sku}</td>
                    <td style={tdStyle}>{item.product_name || '—'}</td>
                    <td style={{ ...tdStyle, textAlign: 'center', fontWeight: 700, color: item.current_stock === 0 ? 'var(--danger)' : 'var(--warning)' }}>{item.current_stock}</td>
                    <td style={{ ...tdStyle, textAlign: 'center', color: 'var(--text-muted)' }}>{item.reorder_point}</td>
                    <td style={{ ...tdStyle, textAlign: 'center', color: 'var(--text-muted)' }}>{item.lead_time_days}d</td>
                    <td style={{ ...tdStyle, textTransform: 'capitalize', color: 'var(--text-secondary)' }}>{item.top_channel || '—'}</td>
                    <td style={{ ...tdStyle, textAlign: 'center', fontWeight: 600, color: 'var(--accent-cyan)' }}>{item.suggested_reorder_qty}</td>
                    <td style={{ ...tdStyle, textAlign: 'right' }}>
                      <div style={{ display: 'flex', gap: '6px', justifyContent: 'flex-end' }}>
                        <button
                          style={{ padding: '4px 10px', background: 'var(--primary)', border: 'none', borderRadius: '4px', color: '#fff', cursor: 'pointer', fontSize: '12px', fontWeight: 600 }}
                          onClick={() => alert(`Create PO for ${item.sku} — connect to POST /forecasting/create-po`)}
                        >Create PO</button>
                        <button
                          disabled={snoozing.has(item.product_id)}
                          style={{ padding: '4px 10px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: '4px', color: 'var(--text-muted)', cursor: 'pointer', fontSize: '12px' }}
                          onClick={() => snooze(item.product_id)}
                        >{snoozing.has(item.product_id) ? '...' : 'Snooze 7d'}</button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {alerts.length === 0 && (
        <div style={{ padding: '48px', textAlign: 'center', background: 'var(--bg-secondary)', borderRadius: '12px', border: '1px solid var(--border)', marginBottom: '32px' }}>
          <div style={{ fontSize: '48px', marginBottom: '16px' }}>✅</div>
          <h3 style={{ margin: '0 0 8px', color: 'var(--success)' }}>No active reorder alerts</h3>
          <p style={{ color: 'var(--text-muted)' }}>All products are above their reorder thresholds.</p>
        </div>
      )}

      {/* Channel Velocity Table */}
      <div>
        <h3 style={{ margin: '0 0 12px', fontSize: '15px', fontWeight: 600, color: 'var(--text-primary)' }}>📡 Channel Velocity (all products, sorted by stock)</h3>
        <div style={{ background: 'var(--bg-secondary)', borderRadius: '10px', border: '1px solid var(--border)', overflow: 'hidden' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '13px' }}>
            <thead><tr style={{ background: 'var(--bg-tertiary)' }}>
              <th style={thStyle}>▸</th>
              <th style={thStyle}>SKU</th>
              <th style={thStyle}>Product</th>
              <th style={{ ...thStyle, textAlign: 'center' }}>Stock</th>
              <th style={{ ...thStyle, textAlign: 'center' }}>Reorder Pt.</th>
              <th style={{ ...thStyle, textAlign: 'center' }}>Top Channel</th>
              <th style={{ ...thStyle, textAlign: 'center' }}>30d Velocity</th>
            </tr></thead>
            <tbody>
              {[...allItems].sort((a, b) => a.current_stock - b.current_stock).map(item => (
                <>
                  <tr
                    key={item.product_id}
                    style={{ borderBottom: '1px solid var(--border)', cursor: 'pointer', background: expanded.has(item.product_id) ? 'var(--bg-tertiary)' : 'transparent' }}
                    onClick={() => toggleExpand(item.product_id)}
                  >
                    <td style={{ ...tdStyle, color: 'var(--text-muted)', width: '32px' }}>{expanded.has(item.product_id) ? '▼' : '▶'}</td>
                    <td style={{ ...tdStyle, fontFamily: 'monospace', color: 'var(--accent-cyan)', fontSize: '12px' }}>{item.sku}</td>
                    <td style={tdStyle}>{item.product_name || '—'}</td>
                    <td style={{ ...tdStyle, textAlign: 'center', fontWeight: 700, color: item.is_below_threshold ? 'var(--danger)' : 'var(--text-primary)' }}>{item.current_stock}</td>
                    <td style={{ ...tdStyle, textAlign: 'center', color: 'var(--text-muted)' }}>{item.reorder_point}</td>
                    <td style={{ ...tdStyle, textAlign: 'center', textTransform: 'capitalize', color: 'var(--text-secondary)' }}>{item.top_channel || '—'}</td>
                    <td style={{ ...tdStyle, textAlign: 'center', color: 'var(--text-muted)' }}>
                      {item.channel_velocities?.reduce((s, c) => s + c.velocity_30d, 0).toFixed(1) || '0'}
                    </td>
                  </tr>
                  {expanded.has(item.product_id) && (
                    <tr key={`${item.product_id}-exp`} style={{ borderBottom: '1px solid var(--border)' }}>
                      <td colSpan={7} style={{ padding: '0 16px 12px 48px' }}>
                        <table style={{ width: '100%', fontSize: '12px', borderCollapse: 'collapse' }}>
                          <thead><tr>
                            {['Channel', '30d Velocity', 'Orders'].map(h => (
                              <th key={h} style={{ padding: '6px 8px', textAlign: 'left', color: 'var(--text-muted)', fontWeight: 600, fontSize: '11px' }}>{h}</th>
                            ))}
                          </tr></thead>
                          <tbody>
                            {(item.channel_velocities || []).map(cv => (
                              <tr key={cv.channel}>
                                <td style={{ padding: '4px 8px', textTransform: 'capitalize', color: 'var(--text-secondary)' }}>{cv.channel}</td>
                                <td style={{ padding: '4px 8px', color: 'var(--text-primary)', fontWeight: 600 }}>{cv.velocity_30d.toFixed(2)}/day</td>
                                <td style={{ padding: '4px 8px', color: 'var(--text-muted)' }}>{cv.order_count}</td>
                              </tr>
                            ))}
                          </tbody>
                        </table>
                      </td>
                    </tr>
                  )}
                </>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}

// ─── Main Page ────────────────────────────────────────────────────────────────

export default function Forecasting() {
  const [tab, setTab] = useState<'dashboard' | 'products' | 'reorder-alerts'>('dashboard');
  const [dashboard, setDashboard] = useState<DashboardData | null>(null);
  const [forecasts, setForecasts] = useState<ProductForecast[]>([]);
  const [settings, setSettings] = useState<ForecastingSettings | null>(null);
  const [loading, setLoading] = useState(true);
  const [recalculating, setRecalculating] = useState(false);
  const [showSettings, setShowSettings] = useState(false);
  const [configProduct, setConfigProduct] = useState<ProductForecast | null>(null);
  const [statusFilter, setStatusFilter] = useState('');
  const [search, setSearch] = useState('');

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [dashRes, settingsRes] = await Promise.all([
        api('/forecasting/dashboard'),
        api('/forecasting/settings'),
      ]);
      if (dashRes.ok) { const d = await dashRes.json(); setDashboard(d); }
      if (settingsRes.ok) { const d = await settingsRes.json(); setSettings(d.settings); }
    } finally { setLoading(false); }
  }, []);

  const loadProducts = useCallback(async () => {
    const qs = statusFilter ? `?status=${statusFilter}` : '';
    const res = await api(`/forecasting/products${qs}`);
    if (res.ok) { const d = await res.json(); setForecasts(d.forecasts || []); }
  }, [statusFilter]);

  useEffect(() => { load(); }, [load]);
  useEffect(() => { if (tab === 'products') loadProducts(); }, [tab, loadProducts]);

  const handleRecalculate = async () => {
    setRecalculating(true);
    try {
      await api('/forecasting/recalculate', { method: 'POST', body: '{}' });
      await load();
      if (tab === 'products') await loadProducts();
    } finally { setRecalculating(false); }
  };

  const filtered = forecasts.filter(f => {
    if (!search) return true;
    return f.sku.toLowerCase().includes(search.toLowerCase()) ||
           f.product_name.toLowerCase().includes(search.toLowerCase());
  });

  const ForecastRow = ({ f }: { f: ProductForecast }) => {
    const sc = STATUS_CONFIG[f.forecast_status] || STATUS_CONFIG.unconfigured;
    const daysColor = f.days_of_stock <= 7 ? 'var(--danger)'
      : f.days_of_stock <= 14 ? 'var(--warning)'
      : f.days_of_stock <= 30 ? 'var(--accent-orange)'
      : 'var(--success)';

    return (
      <tr style={{ borderBottom: '1px solid var(--border)', background: f.forecast_status === 'out_of_stock' ? 'rgba(239,68,68,0.03)' : 'transparent' }}>
        <td style={{ ...tdStyle, fontFamily: 'monospace', color: 'var(--accent-cyan)', fontSize: 12 }}>{f.sku}</td>
        <td style={tdStyle}>{f.product_name || '—'}</td>
        <td style={{ ...tdStyle, textAlign: 'center', fontWeight: 700, color: 'var(--text-primary)' }}>{f.current_stock}</td>
        <td style={{ ...tdStyle, textAlign: 'center' }}>
          <span style={{ fontWeight: 700, color: daysColor, fontSize: 15 }}>
            {f.days_of_stock === 999 ? '∞' : f.days_of_stock?.toFixed(0) ?? '—'}
          </span>
        </td>
        <td style={{ ...tdStyle, textAlign: 'center', color: 'var(--text-secondary)' }}>{f.calculated_adc?.toFixed(2) || '—'}</td>
        <td style={{ ...tdStyle, textAlign: 'center', color: 'var(--text-muted)' }}>{f.reorder_point || '—'}</td>
        <td style={{ ...tdStyle, textAlign: 'center', color: 'var(--text-muted)' }}>{f.reorder_qty || '—'}</td>
        <td style={tdStyle}>
          <span style={{ padding: '2px 8px', borderRadius: 12, fontSize: 11, fontWeight: 600, background: sc.bg, color: sc.color }}>
            {sc.label}
          </span>
        </td>
        <td style={{ ...tdStyle, textAlign: 'right' }}>
          <button
            style={{ padding: '4px 10px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 4, color: 'var(--text-secondary)', cursor: 'pointer', fontSize: 12 }}
            onClick={() => setConfigProduct(f)}
          >
            Configure
          </button>
        </td>
      </tr>
    );
  };

  return (
    <div style={{ padding: '24px 32px', maxWidth: 1300, margin: '0 auto' }}>
      {/* Header */}
      <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', marginBottom: 24 }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 24, fontWeight: 700, color: 'var(--text-primary)' }}>Stock Forecasting</h1>
          <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 14 }}>
            Demand-driven replenishment recommendations. Configurable lookback up to 365 days.
          </p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <button style={btnGhostStyle} onClick={() => setShowSettings(true)}>⚙ Settings</button>
          <button style={btnGhostStyle} onClick={handleRecalculate} disabled={recalculating}>
            {recalculating ? '⟳ Recalculating…' : '⟳ Recalculate All'}
          </button>
        </div>
      </div>

      {/* Tabs */}
      <div style={{ display: 'flex', gap: 0, marginBottom: 24, borderBottom: '1px solid var(--border)' }}>
        {([
          { key: 'dashboard', label: '📊 Dashboard' },
          { key: 'products', label: '📋 All Products' },
          { key: 'reorder-alerts', label: '🚨 Reorder Alerts' },
        ] as const).map(t => (
          <button key={t.key} onClick={() => setTab(t.key)} style={{
            padding: '10px 20px', background: 'none', border: 'none',
            borderBottom: tab === t.key ? '2px solid var(--primary)' : '2px solid transparent',
            color: tab === t.key ? 'var(--primary)' : 'var(--text-muted)',
            cursor: 'pointer', fontSize: 14, fontWeight: tab === t.key ? 600 : 400, marginBottom: -1,
          }}>{t.label}</button>
        ))}
      </div>

      {loading ? (
        <div style={{ padding: 48, textAlign: 'center', color: 'var(--text-muted)' }}>Loading forecasts…</div>
      ) : tab === 'reorder-alerts' ? (
        <ReorderAlertsTab />
      ) : tab === 'dashboard' ? (
        <>
          {/* Summary stats */}
          {dashboard && (
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(5, 1fr)', gap: 12, marginBottom: 24 }}>
              {[
                { label: 'Total SKUs', value: dashboard.summary.total_skus, color: 'var(--text-primary)' },
                { label: 'Out of Stock', value: dashboard.summary.out_of_stock, color: 'var(--danger)' },
                { label: 'Critical', value: dashboard.summary.critical, color: 'var(--accent-orange)' },
                { label: 'Low Stock', value: dashboard.summary.low, color: 'var(--warning)' },
                { label: 'Healthy', value: dashboard.summary.healthy, color: 'var(--success)' },
              ].map(stat => (
                <div key={stat.label} style={{
                  padding: '16px 20px', background: 'var(--bg-secondary)', border: '1px solid var(--border)',
                  borderRadius: 10, cursor: stat.label !== 'Total SKUs' ? 'pointer' : 'default',
                  borderLeftColor: stat.color, borderLeftWidth: 3,
                }} onClick={() => {
                  if (stat.label !== 'Total SKUs') {
                    const map: Record<string, string> = {
                      'Out of Stock': 'out_of_stock', 'Critical': 'critical', 'Low Stock': 'low', 'Healthy': 'ok',
                    };
                    setStatusFilter(map[stat.label] || '');
                    setTab('products');
                  }
                }}>
                  <div style={{ fontSize: 24, fontWeight: 700, color: stat.color }}>{stat.value}</div>
                  <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>{stat.label}</div>
                </div>
              ))}
            </div>
          )}

          {/* Critical items table */}
          {dashboard && dashboard.critical_items.length > 0 && (
            <div style={{ marginBottom: 24 }}>
              <h3 style={{ margin: '0 0 12px', fontSize: 15, fontWeight: 600, color: 'var(--danger)' }}>
                🚨 Needs Immediate Attention ({dashboard.critical_items.length})
              </h3>
              <div style={{ background: 'var(--bg-secondary)', borderRadius: 10, border: '1px solid var(--border)', overflow: 'hidden' }}>
                <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
                  <thead><tr style={{ background: 'var(--bg-tertiary)' }}>
                    {['SKU', 'Product', 'Stock', 'Days Left', 'ADC', 'Reorder Qty', 'Status', ''].map(h => (
                      <th key={h} style={thStyle}>{h}</th>
                    ))}
                  </tr></thead>
                  <tbody>
                    {dashboard.critical_items.slice(0, 15).map(f => <ForecastRow key={f.product_id} f={f} />)}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {/* Low stock */}
          {dashboard && dashboard.low_items.length > 0 && (
            <div>
              <h3 style={{ margin: '0 0 12px', fontSize: 15, fontWeight: 600, color: 'var(--warning)' }}>
                ⚠️ Low Stock ({dashboard.low_items.length})
              </h3>
              <div style={{ background: 'var(--bg-secondary)', borderRadius: 10, border: '1px solid var(--border)', overflow: 'hidden' }}>
                <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
                  <thead><tr style={{ background: 'var(--bg-tertiary)' }}>
                    {['SKU', 'Product', 'Stock', 'Days Left', 'ADC', 'Reorder Qty', 'Status', ''].map(h => (
                      <th key={h} style={thStyle}>{h}</th>
                    ))}
                  </tr></thead>
                  <tbody>
                    {dashboard.low_items.map(f => <ForecastRow key={f.product_id} f={f} />)}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {dashboard && dashboard.critical_items.length === 0 && dashboard.low_items.length === 0 && (
            <div style={{ padding: '64px 32px', textAlign: 'center', background: 'var(--bg-secondary)', borderRadius: 12, border: '1px solid var(--border)' }}>
              <div style={{ fontSize: 48, marginBottom: 16 }}>✅</div>
              <h3 style={{ margin: '0 0 8px', color: 'var(--success)' }}>All stock levels healthy</h3>
              <p style={{ color: 'var(--text-muted)' }}>No items are below their reorder point. Click "Recalculate All" to refresh forecasts.</p>
            </div>
          )}
        </>
      ) : (
        <>
          <div style={{ display: 'flex', gap: 12, marginBottom: 20 }}>
            <input style={{ ...inputStyle, width: 240, margin: 0 }}
              placeholder="Search SKU or product…" value={search} onChange={e => setSearch(e.target.value)} />
            <select style={{ ...inputStyle, width: 160, margin: 0 }} value={statusFilter} onChange={e => setStatusFilter(e.target.value)}>
              <option value="">All Statuses</option>
              {Object.entries(STATUS_CONFIG).map(([k, v]) => (
                <option key={k} value={k}>{v.label}</option>
              ))}
            </select>
            {(search || statusFilter) && (
              <button style={btnGhostStyle} onClick={() => { setSearch(''); setStatusFilter(''); }}>Clear</button>
            )}
            <span style={{ marginLeft: 'auto', fontSize: 12, color: 'var(--text-muted)', alignSelf: 'center' }}>
              {filtered.length} products
            </span>
          </div>
          <div style={{ background: 'var(--bg-secondary)', borderRadius: 12, border: '1px solid var(--border)', overflow: 'hidden' }}>
            <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
              <thead><tr style={{ background: 'var(--bg-tertiary)' }}>
                {['SKU', 'Product', 'Stock', 'Days Left', 'ADC', 'Reorder Pt.', 'Reorder Qty', 'Status', ''].map(h => (
                  <th key={h} style={{ ...thStyle, textAlign: h === '' ? 'right' : h === 'Stock' || h === 'Days Left' || h === 'ADC' || h === 'Reorder Pt.' || h === 'Reorder Qty' ? 'center' : 'left' }}>{h}</th>
                ))}
              </tr></thead>
              <tbody>
                {filtered.map(f => <ForecastRow key={f.product_id} f={f} />)}
                {filtered.length === 0 && (
                  <tr><td colSpan={9} style={{ padding: '32px', textAlign: 'center', color: 'var(--text-muted)' }}>
                    No forecasts found. Click "Recalculate All" to generate forecasts for your products.
                  </td></tr>
                )}
              </tbody>
            </table>
          </div>
        </>
      )}

      {showSettings && settings && (
        <SettingsModal
          settings={settings}
          onClose={() => setShowSettings(false)}
          onSaved={() => { setShowSettings(false); load(); }}
        />
      )}
      {configProduct && settings && (
        <ProductConfigModal
          forecast={configProduct}
          globalSettings={settings}
          onClose={() => setConfigProduct(null)}
          onSaved={() => { setConfigProduct(null); loadProducts(); }}
        />
      )}
    </div>
  );
}

// ─── Styles ───────────────────────────────────────────────────────────────────
const overlayStyle: React.CSSProperties = { position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.6)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 };
const modalStyle: React.CSSProperties = { background: 'var(--bg-secondary)', borderRadius: 12, border: '1px solid var(--border)', width: 500, maxWidth: '95vw', maxHeight: '90vh', overflowY: 'auto' };
const modalHeaderStyle: React.CSSProperties = { display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', padding: '20px 24px 16px', borderBottom: '1px solid var(--border)' };
const modalTitleStyle: React.CSSProperties = { margin: 0, fontSize: 18, fontWeight: 600, color: 'var(--text-primary)' };
const modalFooterStyle: React.CSSProperties = { display: 'flex', justifyContent: 'flex-end', gap: 8, padding: '16px 24px', borderTop: '1px solid var(--border)' };
const closeBtnStyle: React.CSSProperties = { background: 'none', border: 'none', color: 'var(--text-muted)', cursor: 'pointer', fontSize: 18 };
const fieldStyle: React.CSSProperties = { marginTop: 16 };
const labelStyle: React.CSSProperties = { display: 'block', marginBottom: 6, fontSize: 13, fontWeight: 500, color: 'var(--text-secondary)' };
const hintStyle: React.CSSProperties = { margin: '4px 0 0', fontSize: 11, color: 'var(--text-muted)' };
const inputStyle: React.CSSProperties = { width: '100%', padding: '8px 12px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', fontSize: 14, outline: 'none', boxSizing: 'border-box' };
const errorStyle: React.CSSProperties = { margin: '0 24px 16px', padding: '10px 14px', background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 6, color: 'var(--danger)', fontSize: 13 };
const btnPrimaryStyle: React.CSSProperties = { padding: '8px 16px', background: 'var(--primary)', color: 'white', border: 'none', borderRadius: 6, cursor: 'pointer', fontSize: 13, fontWeight: 600 };
const btnGhostStyle: React.CSSProperties = { padding: '8px 16px', background: 'transparent', color: 'var(--text-secondary)', border: '1px solid var(--border)', borderRadius: 6, cursor: 'pointer', fontSize: 13 };
const thStyle: React.CSSProperties = { padding: '10px 16px', textAlign: 'left', fontSize: 11, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--text-muted)', borderBottom: '1px solid var(--border)' };
const tdStyle: React.CSSProperties = { padding: '12px 16px', color: 'var(--text-primary)' };

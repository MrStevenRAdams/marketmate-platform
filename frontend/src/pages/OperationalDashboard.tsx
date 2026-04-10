import { useState, useEffect, useCallback } from 'react';
import { getActiveTenantId } from '../contexts/TenantContext';

const API_BASE = import.meta.env.VITE_API_URL ?? 'http://localhost:8080/api/v1';

function api(path: string) {
  return fetch(`${API_BASE}${path}`, {
    headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId() },
  });
}

type Tab = 'pipeline' | 'throughput' | 'channel-sync';

interface PipelineStage { status: string; count: number; }
interface SLABand { band: string; count: number; }
interface ChannelHealth { channel: string; total_orders: number; error_orders: number; error_rate: number; last_order_at: string; }
interface OperationalData { order_pipeline: PipelineStage[]; sla_health: SLABand[]; channel_health: ChannelHealth[]; }

interface ThroughputHour { hour: number; label: string; dispatched: number; fulfilled: number; }
interface ThroughputData { date: string; total_dispatched: number; total_fulfilled: number; peak_hour: number; peak_count: number; hourly: ThroughputHour[]; }

interface SyncHealthItem { channel: string; credential_id: string; total_runs: number; error_count: number; error_rate: number; last_sync_at: string; last_sync_status: string; last_error_msg?: string; }
interface SyncHealthData { as_of: string; channels: SyncHealthItem[]; }

function fmtNum(n: number) { return new Intl.NumberFormat('en-GB').format(n); }
function timeAgo(ts: string): string {
  if (!ts) return 'Never';
  try {
    const diff = Date.now() - new Date(ts).getTime();
    const mins = Math.floor(diff / 60000);
    if (mins < 1) return 'Just now';
    if (mins < 60) return `${mins}m ago`;
    const hrs = Math.floor(mins / 60);
    if (hrs < 24) return `${hrs}h ago`;
    return `${Math.floor(hrs / 24)}d ago`;
  } catch { return ts; }
}
function todayStr() { return new Date().toISOString().slice(0, 10); }

const STATUS_LABELS: Record<string, string> = { imported: 'Imported', processing: 'Processing', on_hold: 'On Hold', ready_to_fulfil: 'Ready to Fulfil', parked: 'Parked' };
const STATUS_COLORS: Record<string, string> = { imported: '#3b82f6', processing: '#8b5cf6', on_hold: '#f59e0b', ready_to_fulfil: '#10b981', parked: '#6b7280' };
const SLA_COLORS: Record<string, string> = { overdue: '#ef4444', due_today: '#f59e0b', due_tomorrow: '#8b5cf6', on_track: '#10b981', no_sla: '#9ca3af' };
const SLA_LABELS: Record<string, string> = { overdue: '🔴 Overdue', due_today: '🟡 Due Today', due_tomorrow: '🟣 Due Tomorrow', on_track: '🟢 On Track', no_sla: '⬜ No SLA' };
const CHANNEL_COLORS: Record<string, string> = { amazon: '#FF9900', ebay: '#E53238', shopify: '#96BF48', temu: '#EA6A35', tiktok: '#555', etsy: '#F1641E', woocommerce: '#7F54B3', backmarket: '#14B8A6', zalando: '#FF6600', bol: '#0E4299', lazada: '#F57224' };
function chColor(ch: string) { return CHANNEL_COLORS[ch] ?? 'var(--primary)'; }
function chLabel(ch: string) { return ch ? ch.charAt(0).toUpperCase() + ch.slice(1) : '—'; }

function KpiCard({ label, value, sub, colour }: { label: string; value: string; sub?: string; colour?: string }) {
  return (
    <div style={{ background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 10, padding: '16px 20px', flex: '1 1 140px' }}>
      {colour && <div style={{ width: 4, height: 28, background: colour, borderRadius: 2, marginBottom: 8 }} />}
      <div style={{ fontSize: 22, fontWeight: 700, color: colour ?? 'var(--text)' }}>{value}</div>
      <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 4 }}>{label}</div>
      {sub && <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 2 }}>{sub}</div>}
    </div>
  );
}

function HourlyBar({ hours, valueKey, color }: { hours: ThroughputHour[]; valueKey: 'dispatched' | 'fulfilled'; color: string }) {
  const max = Math.max(...hours.map(h => h[valueKey]), 1);
  const hasData = hours.some(h => h[valueKey] > 0);
  if (!hasData) return <p style={{ color: 'var(--text-muted)', fontSize: 13 }}>No activity recorded.</p>;
  return (
    <div style={{ display: 'flex', alignItems: 'flex-end', gap: 3, height: 130, paddingBottom: 24 }}>
      {hours.map(h => {
        const val = h[valueKey];
        const barH = Math.max((val / max) * 106, val > 0 ? 4 : 0);
        return (
          <div key={h.hour} style={{ flex: 1, display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'flex-end', height: '100%' }}>
            <div title={`${h.label}: ${val}`} style={{ width: '80%', height: barH, background: color, borderRadius: '3px 3px 0 0', opacity: val > 0 ? 0.85 : 0.1 }} />
            <div style={{ fontSize: 9, color: 'var(--text-muted)', transform: 'rotate(-45deg)', whiteSpace: 'nowrap', marginTop: 4 }}>{h.label}</div>
          </div>
        );
      })}
    </div>
  );
}

export default function OperationalDashboard() {
  const [tab, setTab] = useState<Tab>('pipeline');
  const [opData, setOpData] = useState<OperationalData | null>(null);
  const [throughput, setThroughput] = useState<ThroughputData | null>(null);
  const [syncHealth, setSyncHealth] = useState<SyncHealthData | null>(null);
  const [throughputDate, setThroughputDate] = useState(todayStr());
  const [loading, setLoading] = useState(false);
  const [lastRefresh, setLastRefresh] = useState(new Date());
  const [autoRefresh, setAutoRefresh] = useState(false);

  const fetchTab = useCallback(async (t: Tab, tDate: string) => {
    if (t === 'pipeline') { const r = await api('/analytics/operational'); setOpData(await r.json()); }
    else if (t === 'throughput') { const r = await api(`/analytics/operational/throughput?date=${tDate}`); setThroughput(await r.json()); }
    else { const r = await api('/analytics/operational/channel-sync'); setSyncHealth(await r.json()); }
  }, []);

  const refresh = useCallback(async () => {
    setLoading(true);
    try { await fetchTab(tab, throughputDate); setLastRefresh(new Date()); }
    catch (e) { console.error(e); }
    finally { setLoading(false); }
  }, [tab, throughputDate, fetchTab]);

  useEffect(() => { refresh(); }, [refresh]);

  useEffect(() => {
    if (!autoRefresh) return;
    const id = setInterval(refresh, 30000);
    return () => clearInterval(id);
  }, [autoRefresh, refresh]);

  const tabs: { id: Tab; label: string }[] = [
    { id: 'pipeline', label: '📋 Pipeline & SLA' },
    { id: 'throughput', label: '📦 Warehouse Throughput' },
    { id: 'channel-sync', label: '🔄 Channel Sync Health' },
  ];

  return (
    <div style={{ padding: '24px 28px', maxWidth: 1200, margin: '0 auto' }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 20 }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 24, fontWeight: 700 }}>Operational Dashboard</h1>
          <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 14 }}>Live pipeline · Warehouse throughput · Channel sync status</p>
        </div>
        <div style={{ display: 'flex', gap: 10, alignItems: 'center' }}>
          <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>{lastRefresh.toLocaleTimeString('en-GB')}</span>
          <label style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 13, cursor: 'pointer' }}>
            <input type="checkbox" checked={autoRefresh} onChange={e => setAutoRefresh(e.target.checked)} /> Auto (30s)
          </label>
          <button className="btn-pri btn-sm" onClick={refresh} disabled={loading}>{loading ? '…' : '↻ Refresh'}</button>
        </div>
      </div>

      <div style={{ display: 'flex', gap: 4, marginBottom: 24, borderBottom: '1px solid var(--border)' }}>
        {tabs.map(t => (
          <button key={t.id} onClick={() => setTab(t.id)} style={{
            background: 'none', border: 'none', cursor: 'pointer', padding: '8px 16px', fontSize: 14,
            fontWeight: tab === t.id ? 600 : 400, color: tab === t.id ? 'var(--primary)' : 'var(--text-muted)',
            borderBottom: tab === t.id ? '2px solid var(--primary)' : '2px solid transparent', marginBottom: -1,
          }}>{t.label}</button>
        ))}
      </div>

      {loading && <div style={{ textAlign: 'center', padding: 60, color: 'var(--text-muted)' }}>Loading…</div>}

      {/* ── PIPELINE & SLA ─────────────────────────────────────────────────── */}
      {!loading && tab === 'pipeline' && opData && (
        <>
          <div style={{ marginBottom: 28 }}>
            <h2 style={{ margin: '0 0 14px', fontSize: 17, fontWeight: 600 }}>Order Pipeline</h2>
            <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', marginBottom: 16 }}>
              {opData.order_pipeline.map(stage => {
                const total = opData.order_pipeline.reduce((s, p) => s + p.count, 0) || 1;
                const color = STATUS_COLORS[stage.status] ?? '#6b7280';
                return (
                  <div key={stage.status} style={{ flex: '1 1 130px', background: 'var(--surface)', borderRadius: 10, padding: '16px 18px', border: '1px solid var(--border)', borderTop: `4px solid ${color}` }}>
                    <div style={{ fontSize: 28, fontWeight: 700, color }}>{fmtNum(stage.count)}</div>
                    <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 4 }}>{STATUS_LABELS[stage.status] ?? stage.status}</div>
                    <div style={{ height: 4, background: 'var(--border)', borderRadius: 2, marginTop: 10 }}>
                      <div style={{ width: `${(stage.count / total) * 100}%`, height: '100%', background: color, borderRadius: 2 }} />
                    </div>
                  </div>
                );
              })}
            </div>
            <div style={{ display: 'flex', alignItems: 'center', gap: 4, overflowX: 'auto' }}>
              {opData.order_pipeline.map((stage, i) => (
                <div key={stage.status} style={{ display: 'flex', alignItems: 'center', gap: 4, flex: 1, minWidth: 90 }}>
                  <div style={{ flex: 1, background: STATUS_COLORS[stage.status] ?? '#6b7280', borderRadius: 6, padding: '7px 10px', textAlign: 'center', color: '#fff', fontSize: 12, fontWeight: 600, opacity: stage.count === 0 ? 0.3 : 0.9 }}>
                    <div>{fmtNum(stage.count)}</div>
                    <div style={{ fontSize: 10, opacity: 0.85, marginTop: 2 }}>{STATUS_LABELS[stage.status]}</div>
                  </div>
                  {i < opData.order_pipeline.length - 1 && <div style={{ color: 'var(--text-muted)', fontSize: 18, flexShrink: 0 }}>›</div>}
                </div>
              ))}
            </div>
          </div>

          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 20, marginBottom: 28 }}>
            <div style={{ background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 12, padding: 20 }}>
              <h2 style={{ margin: '0 0 18px', fontSize: 17, fontWeight: 600 }}>SLA Health</h2>
              {(() => {
                const total = opData.sla_health.reduce((s, b) => s + b.count, 0) || 1;
                return opData.sla_health.map(band => (
                  <div key={band.band} style={{ marginBottom: 12 }}>
                    <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 13, marginBottom: 5 }}>
                      <span>{SLA_LABELS[band.band] ?? band.band}</span>
                      <span style={{ fontWeight: 600 }}>{fmtNum(band.count)}</span>
                    </div>
                    <div style={{ height: 10, background: 'var(--surface-2)', borderRadius: 5 }}>
                      <div style={{ width: `${(band.count / total) * 100}%`, height: '100%', background: SLA_COLORS[band.band] ?? '#6b7280', borderRadius: 5 }} />
                    </div>
                  </div>
                ));
              })()}
            </div>
            <div style={{ background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 12, padding: 20 }}>
              <h2 style={{ margin: '0 0 18px', fontSize: 17, fontWeight: 600 }}>Attention Required</h2>
              {(() => {
                const overdue = opData.sla_health.find(b => b.band === 'overdue')?.count ?? 0;
                const dueToday = opData.sla_health.find(b => b.band === 'due_today')?.count ?? 0;
                const onHold = opData.order_pipeline.find(s => s.status === 'on_hold')?.count ?? 0;
                if (!overdue && !dueToday && !onHold) {
                  return <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', height: 130, color: '#10b981', gap: 8 }}><div style={{ fontSize: 36 }}>✅</div><div style={{ fontSize: 15, fontWeight: 600 }}>All clear!</div></div>;
                }
                return (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                    {overdue > 0 && <AlertRow color="#ef4444" bg="#fef2f2" border="#fecaca" icon="🚨" label="Overdue orders" count={overdue} />}
                    {dueToday > 0 && <AlertRow color="#f59e0b" bg="#fffbeb" border="#fcd34d" icon="⚠️" label="Due today" count={dueToday} />}
                    {onHold > 0 && <AlertRow color="#3b82f6" bg="#eff6ff" border="#bfdbfe" icon="⏸" label="On hold" count={onHold} />}
                  </div>
                );
              })()}
            </div>
          </div>

          <div style={{ background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 12, padding: 20 }}>
            <h2 style={{ margin: '0 0 16px', fontSize: 17, fontWeight: 600 }}>Channel Order Health</h2>
            <div className="table-outer"><div className="table-scroll-x">
              <table className="orders-table">
                <thead><tr><th>Channel</th><th>Total Orders</th><th>Error Orders</th><th>Error Rate</th><th>Last Order</th><th>Status</th></tr></thead>
                <tbody>
                  {opData.channel_health.map(ch => {
                    const healthy = ch.error_rate < 5; const warn = ch.error_rate >= 5 && ch.error_rate < 20;
                    return (
                      <tr key={ch.channel}>
                        <td><span style={{ display: 'inline-flex', alignItems: 'center', gap: 8 }}><span style={{ width: 10, height: 10, borderRadius: '50%', background: chColor(ch.channel), display: 'inline-block' }} /><strong>{chLabel(ch.channel)}</strong></span></td>
                        <td>{fmtNum(ch.total_orders)}</td>
                        <td>{ch.error_orders > 0 ? <span style={{ color: '#ef4444', fontWeight: 600 }}>{ch.error_orders}</span> : '—'}</td>
                        <td>{ch.error_rate > 0 ? <span style={{ color: warn ? '#f59e0b' : '#ef4444' }}>{ch.error_rate.toFixed(1)}%</span> : <span style={{ color: '#10b981' }}>0%</span>}</td>
                        <td style={{ fontSize: 12, color: 'var(--text-muted)' }}>{timeAgo(ch.last_order_at)}</td>
                        <td><span className={`status-badge sb-${healthy ? 'fulfilled' : warn ? 'default' : 'cancelled'}`}>{healthy ? 'Healthy' : warn ? 'Warning' : 'Degraded'}</span></td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div></div>
          </div>
        </>
      )}

      {/* ── WAREHOUSE THROUGHPUT ────────────────────────────────────────────── */}
      {!loading && tab === 'throughput' && (
        <>
          <div style={{ display: 'flex', gap: 12, alignItems: 'center', marginBottom: 20 }}>
            <label style={{ fontSize: 13, color: 'var(--text-muted)' }}>Date:</label>
            <input type="date" value={throughputDate} onChange={e => setThroughputDate(e.target.value)}
              style={{ padding: '6px 10px', borderRadius: 6, border: '1px solid var(--border)', background: 'var(--surface)', color: 'var(--text)', fontSize: 13 }} />
          </div>
          {throughput && (
            <>
              <div style={{ display: 'flex', gap: 14, flexWrap: 'wrap', marginBottom: 28 }}>
                <KpiCard label="Dispatched" value={fmtNum(throughput.total_dispatched)} colour="var(--primary)" />
                <KpiCard label="Fulfilled" value={fmtNum(throughput.total_fulfilled)} colour="#10b981" />
                <KpiCard label="Peak Hour" value={`${String(throughput.peak_hour).padStart(2,'0')}:00`} sub={`${fmtNum(throughput.peak_count)} dispatched`} colour="#f59e0b" />
              </div>

              <div style={{ background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 12, padding: 20, marginBottom: 20 }}>
                <h3 style={{ margin: '0 0 16px', fontSize: 15 }}>Dispatched per Hour</h3>
                <HourlyBar hours={throughput.hourly} valueKey="dispatched" color="var(--primary)" />
              </div>

              <div style={{ background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 12, padding: 20, marginBottom: 20 }}>
                <h3 style={{ margin: '0 0 4px', fontSize: 15 }}>Fulfilled per Hour</h3>
                <p style={{ margin: '0 0 14px', fontSize: 12, color: 'var(--text-muted)' }}>Proxy for pick/pack — orders moved to fulfilled status</p>
                <HourlyBar hours={throughput.hourly} valueKey="fulfilled" color="#10b981" />
              </div>

              <div style={{ background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 12, padding: 20 }}>
                <h3 style={{ margin: '0 0 16px', fontSize: 15 }}>Active Hours</h3>
                <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(110px, 1fr))', gap: 8 }}>
                  {throughput.hourly.filter(h => h.dispatched > 0 || h.fulfilled > 0).map(h => (
                    <div key={h.hour} style={{ background: 'var(--surface-2)', borderRadius: 8, padding: '10px 12px', borderLeft: h.hour === throughput.peak_hour ? '3px solid var(--primary)' : '3px solid transparent' }}>
                      <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-muted)' }}>{h.label}</div>
                      <div style={{ fontSize: 20, fontWeight: 700, marginTop: 4 }}>{h.dispatched}</div>
                      <div style={{ fontSize: 11, color: 'var(--text-muted)' }}>dispatched</div>
                      {h.fulfilled > 0 && <div style={{ fontSize: 11, color: '#10b981', marginTop: 3 }}>{h.fulfilled} fulfilled</div>}
                    </div>
                  ))}
                  {throughput.hourly.every(h => h.dispatched === 0 && h.fulfilled === 0) && (
                    <div style={{ gridColumn: '1/-1', color: 'var(--text-muted)', fontSize: 14, textAlign: 'center', padding: 30 }}>
                      No dispatch activity on {throughputDate}
                    </div>
                  )}
                </div>
              </div>
            </>
          )}
        </>
      )}

      {/* ── CHANNEL SYNC HEALTH ─────────────────────────────────────────────── */}
      {!loading && tab === 'channel-sync' && (
        <>
          {syncHealth ? (
            <>
              <div style={{ display: 'flex', gap: 14, flexWrap: 'wrap', marginBottom: 28 }}>
                <KpiCard label="Channels Tracked" value={fmtNum(syncHealth.channels.length)} colour="var(--primary)" />
                <KpiCard label="With Errors" value={fmtNum(syncHealth.channels.filter(c => c.error_count > 0).length)} colour="#ef4444" />
                <KpiCard label="Healthy" value={fmtNum(syncHealth.channels.filter(c => c.error_rate < 5).length)} colour="#10b981" />
                <KpiCard label="As Of" value={new Date(syncHealth.as_of).toLocaleTimeString('en-GB')} sub={new Date(syncHealth.as_of).toLocaleDateString('en-GB')} />
              </div>

              <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', marginBottom: 24 }}>
                {syncHealth.channels.map(ch => {
                  const healthy = ch.error_rate < 5; const warn = ch.error_rate >= 5 && ch.error_rate < 20;
                  const statusColor = healthy ? '#10b981' : warn ? '#f59e0b' : '#ef4444';
                  const label = ch.channel ? chLabel(ch.channel) : (ch.credential_id?.slice(0, 12) ?? '—');
                  return (
                    <div key={ch.credential_id || ch.channel} style={{ flex: '1 1 180px', background: 'var(--surface)', borderRadius: 10, padding: 16, border: '1px solid var(--border)', borderLeft: `4px solid ${chColor(ch.channel)}` }}>
                      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', marginBottom: 10 }}>
                        <span style={{ fontSize: 14, fontWeight: 600 }}>{label}</span>
                        <span style={{ width: 10, height: 10, borderRadius: '50%', background: statusColor, display: 'inline-block', marginTop: 3 }} />
                      </div>
                      <div style={{ fontSize: 22, fontWeight: 700 }}>{fmtNum(ch.total_runs)}</div>
                      <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 2 }}>sync runs (7d)</div>
                      {ch.error_count > 0 && <div style={{ marginTop: 8, fontSize: 12, color: statusColor, fontWeight: 600 }}>{ch.error_count} errors ({ch.error_rate.toFixed(1)}%)</div>}
                      <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 6 }}>Last: {timeAgo(ch.last_sync_at)}</div>
                      {ch.last_error_msg && (
                        <div style={{ marginTop: 8, fontSize: 11, color: '#ef4444', background: '#fef2f2', borderRadius: 4, padding: '4px 8px', wordBreak: 'break-word' }}>
                          {ch.last_error_msg.slice(0, 80)}{ch.last_error_msg.length > 80 ? '…' : ''}
                        </div>
                      )}
                    </div>
                  );
                })}
                {syncHealth.channels.length === 0 && (
                  <div style={{ color: 'var(--text-muted)', fontSize: 14, padding: 40, textAlign: 'center', width: '100%' }}>No sync activity in the last 7 days.</div>
                )}
              </div>

              {syncHealth.channels.length > 0 && (
                <div style={{ background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 12, padding: 20 }}>
                  <h3 style={{ margin: '0 0 16px', fontSize: 15 }}>Sync Detail (last 7 days)</h3>
                  <div className="table-outer"><div className="table-scroll-x">
                    <table className="orders-table">
                      <thead><tr><th>Channel</th><th>Credential</th><th>Runs</th><th>Errors</th><th>Error Rate</th><th>Last Sync</th><th>Status</th><th>Last Error</th></tr></thead>
                      <tbody>
                        {syncHealth.channels.map(ch => {
                          const healthy = ch.error_rate < 5; const warn = ch.error_rate >= 5 && ch.error_rate < 20;
                          return (
                            <tr key={ch.credential_id || ch.channel}>
                              <td><span style={{ display: 'inline-flex', alignItems: 'center', gap: 8 }}><span style={{ width: 10, height: 10, borderRadius: '50%', background: chColor(ch.channel), display: 'inline-block' }} />{chLabel(ch.channel)}</span></td>
                              <td style={{ fontSize: 12, fontFamily: 'monospace', color: 'var(--text-muted)' }}>{ch.credential_id?.slice(0, 14) ?? '—'}</td>
                              <td>{fmtNum(ch.total_runs)}</td>
                              <td>{ch.error_count > 0 ? <span style={{ color: '#ef4444', fontWeight: 600 }}>{ch.error_count}</span> : '—'}</td>
                              <td>{ch.error_rate > 0 ? <span style={{ color: warn ? '#f59e0b' : '#ef4444' }}>{ch.error_rate.toFixed(1)}%</span> : <span style={{ color: '#10b981' }}>0%</span>}</td>
                              <td style={{ fontSize: 12, color: 'var(--text-muted)' }}>{timeAgo(ch.last_sync_at)}</td>
                              <td><span className={`status-badge sb-${ch.last_sync_status === 'completed' ? 'fulfilled' : ch.last_sync_status === 'error' ? 'cancelled' : 'default'}`}>{ch.last_sync_status || '—'}</span></td>
                              <td style={{ maxWidth: 200, fontSize: 11, color: '#ef4444', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{ch.last_error_msg || '—'}</td>
                            </tr>
                          );
                        })}
                      </tbody>
                    </table>
                  </div></div>
                </div>
              )}
            </>
          ) : (
            <div style={{ textAlign: 'center', padding: 60, color: 'var(--text-muted)' }}>No sync data available.</div>
          )}
        </>
      )}
    </div>
  );
}

function AlertRow({ color, bg, border, icon, label, count }: { color: string; bg: string; border: string; icon: string; label: string; count: number }) {
  return (
    <div style={{ background: bg, border: `1px solid ${border}`, borderRadius: 8, padding: '12px 16px', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
      <span style={{ fontSize: 14, color }}>{icon} {label}</span>
      <span style={{ fontSize: 20, fontWeight: 700, color }}>{count}</span>
    </div>
  );
}

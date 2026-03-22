import React, { useState, useEffect, useCallback } from 'react';
import { Plus, Trash2, Edit3, ChevronUp, ChevronDown, Play, CheckCircle, XCircle } from 'lucide-react';

const API_BASE = import.meta.env.VITE_API_BASE || '/api/v1';

function authFetch(url: string, init?: RequestInit) {
  const tenantId = localStorage.getItem('tenant_id') || '';
  const token = localStorage.getItem('auth_token') || '';
  return fetch(url, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      'X-Tenant-Id': tenantId,
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...(init?.headers || {}),
    },
  });
}

interface NetworkSourceEntry {
  source_id: string;
  priority: number;
  min_stock: number;
}

interface FulfilmentNetwork {
  network_id: string;
  name: string;
  description?: string;
  sources: NetworkSourceEntry[];
  active: boolean;
  created_at: string;
}

interface FulfilmentSource {
  source_id: string;
  name: string;
  type: string;
  active: boolean;
}

interface SkipReason {
  source_id: string;
  source_name?: string;
  reason: string;
}

interface ResolveResult {
  network_id: string;
  order_id: string;
  selected_source?: FulfilmentSource;
  skipped_sources?: SkipReason[];
  reason: string;
}

export default function FulfilmentNetworks() {
  const [networks, setNetworks] = useState<FulfilmentNetwork[]>([]);
  const [sources, setSources] = useState<FulfilmentSource[]>([]);
  const [loading, setLoading] = useState(true);
  const [showForm, setShowForm] = useState(false);
  const [editingNetwork, setEditingNetwork] = useState<FulfilmentNetwork | null>(null);

  // Form state
  const [formName, setFormName] = useState('');
  const [formDesc, setFormDesc] = useState('');
  const [formActive, setFormActive] = useState(true);
  const [formSources, setFormSources] = useState<NetworkSourceEntry[]>([]);
  const [saving, setSaving] = useState(false);

  // Simulate panel
  const [simOrderId, setSimOrderId] = useState('');
  const [simNetworkId, setSimNetworkId] = useState('');
  const [simResult, setSimResult] = useState<ResolveResult | null>(null);
  const [simLoading, setSimLoading] = useState(false);
  const [simError, setSimError] = useState('');

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [netRes, srcRes] = await Promise.all([
        authFetch(`${API_BASE}/fulfilment-networks`),
        authFetch(`${API_BASE}/fulfilment-sources`),
      ]);
      if (netRes.ok) {
        const d = await netRes.json();
        setNetworks(d.networks || []);
      }
      if (srcRes.ok) {
        const d = await srcRes.json();
        setSources((d.sources || d.items || []).filter((s: FulfilmentSource) => s.active));
      }
    } catch (err) {
      console.error('FulfilmentNetworks load error:', err);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { load(); }, [load]);

  function openCreate() {
    setEditingNetwork(null);
    setFormName('');
    setFormDesc('');
    setFormActive(true);
    setFormSources([]);
    setShowForm(true);
  }

  function openEdit(n: FulfilmentNetwork) {
    setEditingNetwork(n);
    setFormName(n.name);
    setFormDesc(n.description || '');
    setFormActive(n.active);
    setFormSources([...(n.sources || [])]);
    setShowForm(true);
  }

  async function saveNetwork() {
    if (!formName.trim()) { alert('Network name is required'); return; }
    setSaving(true);
    try {
      const body = { name: formName.trim(), description: formDesc, active: formActive, sources: formSources };
      const res = editingNetwork
        ? await authFetch(`${API_BASE}/fulfilment-networks/${editingNetwork.network_id}`, { method: 'PUT', body: JSON.stringify(body) })
        : await authFetch(`${API_BASE}/fulfilment-networks`, { method: 'POST', body: JSON.stringify(body) });
      if (!res.ok) throw new Error('Save failed');
      setShowForm(false);
      await load();
    } catch (err: any) {
      alert(`Save failed: ${err.message}`);
    } finally {
      setSaving(false);
    }
  }

  async function deleteNetwork(id: string, name: string) {
    if (!confirm(`Delete network "${name}"? This cannot be undone.`)) return;
    try {
      await authFetch(`${API_BASE}/fulfilment-networks/${id}`, { method: 'DELETE' });
      await load();
    } catch (err: any) {
      alert(`Delete failed: ${err.message}`);
    }
  }

  function addSourceEntry() {
    if (sources.length === 0) { alert('No active fulfilment sources available. Create sources in Fulfilment Sources first.'); return; }
    const nextPriority = formSources.length > 0 ? Math.max(...formSources.map(s => s.priority)) + 1 : 1;
    setFormSources(prev => [...prev, { source_id: sources[0].source_id, priority: nextPriority, min_stock: 0 }]);
  }

  function removeSourceEntry(idx: number) {
    setFormSources(prev => prev.filter((_, i) => i !== idx));
  }

  function updateSourceEntry(idx: number, field: keyof NetworkSourceEntry, value: string | number) {
    setFormSources(prev => prev.map((s, i) => i === idx ? { ...s, [field]: value } : s));
  }

  function movePriority(idx: number, dir: -1 | 1) {
    const newSources = [...formSources];
    const swapIdx = idx + dir;
    if (swapIdx < 0 || swapIdx >= newSources.length) return;
    // Swap priority values
    const tmp = newSources[idx].priority;
    newSources[idx].priority = newSources[swapIdx].priority;
    newSources[swapIdx].priority = tmp;
    // Swap array positions
    [newSources[idx], newSources[swapIdx]] = [newSources[swapIdx], newSources[idx]];
    setFormSources(newSources);
  }

  async function simulate() {
    if (!simOrderId.trim() || !simNetworkId) { alert('Enter an Order ID and select a network'); return; }
    setSimLoading(true);
    setSimResult(null);
    setSimError('');
    try {
      const res = await authFetch(`${API_BASE}/fulfilment-networks/${simNetworkId}/resolve`, {
        method: 'POST',
        body: JSON.stringify({ order_id: simOrderId.trim() }),
      });
      const data = await res.json();
      if (!res.ok) throw new Error(data.error || 'Resolve failed');
      setSimResult(data.result);
    } catch (err: any) {
      setSimError(err.message);
    } finally {
      setSimLoading(false);
    }
  }

  const sourceName = (id: string) => sources.find(s => s.source_id === id)?.name || id;

  return (
    <div style={{ padding: '24px', maxWidth: 960, margin: '0 auto' }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 24 }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 22, fontWeight: 700, color: 'var(--text-primary)' }}>Fulfilment Networks</h1>
          <p style={{ margin: '4px 0 0', fontSize: 13, color: 'var(--text-muted)' }}>
            Priority waterfall routing — assign orders to a network and the system picks the best available source automatically.
          </p>
        </div>
        <button className="btn-pri" onClick={openCreate} style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
          <Plus size={14} /> New Network
        </button>
      </div>

      {/* ── Simulate Panel ── */}
      <div style={{ background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 10, padding: '16px 20px', marginBottom: 24 }}>
        <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--text-primary)', marginBottom: 10 }}>
          🧪 Simulate Routing
        </div>
        <div style={{ display: 'flex', gap: 10, alignItems: 'flex-end', flexWrap: 'wrap' }}>
          <div>
            <label style={{ fontSize: 11, color: 'var(--text-muted)', display: 'block', marginBottom: 4 }}>Order ID</label>
            <input value={simOrderId} onChange={e => setSimOrderId(e.target.value)}
              placeholder="Paste an order ID…"
              style={{ fontSize: 12, padding: '6px 10px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', width: 220 }} />
          </div>
          <div>
            <label style={{ fontSize: 11, color: 'var(--text-muted)', display: 'block', marginBottom: 4 }}>Network</label>
            <select value={simNetworkId} onChange={e => setSimNetworkId(e.target.value)}
              style={{ fontSize: 12, padding: '6px 10px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', minWidth: 180 }}>
              <option value="">Select network…</option>
              {networks.map(n => <option key={n.network_id} value={n.network_id}>{n.name}</option>)}
            </select>
          </div>
          <button className="btn-sec" onClick={simulate} disabled={simLoading} style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
            <Play size={12} /> {simLoading ? 'Resolving…' : 'Simulate'}
          </button>
        </div>
        {simError && <p style={{ marginTop: 10, color: '#ef4444', fontSize: 12 }}>❌ {simError}</p>}
        {simResult && (
          <div style={{ marginTop: 12, padding: '12px 14px', background: 'var(--bg-secondary)', borderRadius: 8, border: '1px solid var(--border)' }}>
            {simResult.selected_source ? (
              <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                <CheckCircle size={16} color="#22c55e" />
                <span style={{ fontSize: 13, fontWeight: 600, color: '#22c55e' }}>Selected: {simResult.selected_source.name}</span>
                <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>({simResult.selected_source.type})</span>
              </div>
            ) : (
              <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                <XCircle size={16} color="#ef4444" />
                <span style={{ fontSize: 13, fontWeight: 600, color: '#ef4444' }}>No source found</span>
              </div>
            )}
            <p style={{ margin: '4px 0 0', fontSize: 12, color: 'var(--text-muted)' }}>{simResult.reason}</p>
            {simResult.skipped_sources && simResult.skipped_sources.length > 0 && (
              <div style={{ marginTop: 8 }}>
                <div style={{ fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', marginBottom: 4 }}>SKIPPED:</div>
                {simResult.skipped_sources.map((s, i) => (
                  <div key={i} style={{ fontSize: 11, color: '#f59e0b' }}>⚠ {s.source_name || s.source_id}: {s.reason}</div>
                ))}
              </div>
            )}
          </div>
        )}
      </div>

      {/* ── Networks List ── */}
      {loading ? (
        <p style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading networks…</p>
      ) : networks.length === 0 ? (
        <div style={{ textAlign: 'center', padding: '48px 0', color: 'var(--text-muted)', fontSize: 14 }}>
          No fulfilment networks yet. Create one to start routing orders automatically.
        </div>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          {networks.map(n => (
            <div key={n.network_id} style={{
              background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 10,
              padding: '16px 20px',
              opacity: n.active ? 1 : 0.6,
            }}>
              <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between' }}>
                <div>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                    <span style={{ fontSize: 15, fontWeight: 700, color: 'var(--text-primary)' }}>{n.name}</span>
                    <span style={{
                      fontSize: 10, fontWeight: 600, padding: '2px 7px', borderRadius: 10,
                      background: n.active ? 'rgba(34,197,94,0.12)' : 'rgba(107,114,128,0.12)',
                      color: n.active ? '#22c55e' : '#6b7280',
                      border: `1px solid ${n.active ? 'rgba(34,197,94,0.25)' : 'rgba(107,114,128,0.2)'}`,
                    }}>{n.active ? 'Active' : 'Inactive'}</span>
                  </div>
                  {n.description && <p style={{ margin: '3px 0 0', fontSize: 12, color: 'var(--text-muted)' }}>{n.description}</p>}
                  <p style={{ margin: '6px 0 0', fontSize: 12, color: 'var(--text-muted)' }}>
                    {(n.sources || []).length} source{(n.sources || []).length !== 1 ? 's' : ''} in priority order
                  </p>
                </div>
                <div style={{ display: 'flex', gap: 8 }}>
                  <button className="btn-sec" style={{ padding: '4px 10px', fontSize: 12 }} onClick={() => openEdit(n)}>
                    <Edit3 size={12} /> Edit
                  </button>
                  <button className="btn-sec" style={{ padding: '4px 10px', fontSize: 12, color: '#ef4444' }} onClick={() => deleteNetwork(n.network_id, n.name)}>
                    <Trash2 size={12} />
                  </button>
                </div>
              </div>
              {/* Source priority list */}
              {(n.sources || []).length > 0 && (
                <div style={{ marginTop: 12, display: 'flex', flexDirection: 'column', gap: 4 }}>
                  {[...(n.sources || [])].sort((a, b) => a.priority - b.priority).map((s, i) => (
                    <div key={s.source_id} style={{ display: 'flex', alignItems: 'center', gap: 10, fontSize: 12, color: 'var(--text-primary)' }}>
                      <span style={{ width: 20, height: 20, borderRadius: '50%', background: 'var(--bg-secondary)', border: '1px solid var(--border)', display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 10, fontWeight: 700, flexShrink: 0 }}>{i + 1}</span>
                      <span style={{ fontWeight: 500 }}>{sourceName(s.source_id)}</span>
                      {s.min_stock > 0 && <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>min stock: {s.min_stock}</span>}
                    </div>
                  ))}
                </div>
              )}
            </div>
          ))}
        </div>
      )}

      {/* ── Create / Edit Form Modal ── */}
      {showForm && (
        <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.7)', zIndex: 300, display: 'flex', alignItems: 'center', justifyContent: 'center' }}
          onClick={() => setShowForm(false)}>
          <div style={{ background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 12, padding: 28, width: 560, maxHeight: '90vh', overflowY: 'auto' }}
            onClick={e => e.stopPropagation()}>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 20 }}>
              <h2 style={{ margin: 0, fontSize: 16, fontWeight: 700 }}>{editingNetwork ? 'Edit Network' : 'New Fulfilment Network'}</h2>
              <button onClick={() => setShowForm(false)} style={{ background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-muted)', fontSize: 18 }}>×</button>
            </div>

            <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
              <div>
                <label style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-primary)', display: 'block', marginBottom: 6 }}>Network Name *</label>
                <input value={formName} onChange={e => setFormName(e.target.value)}
                  placeholder="e.g. UK Domestic, EU Priority"
                  style={{ width: '100%', fontSize: 13, padding: '8px 10px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', boxSizing: 'border-box' }} />
              </div>
              <div>
                <label style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-primary)', display: 'block', marginBottom: 6 }}>Description</label>
                <input value={formDesc} onChange={e => setFormDesc(e.target.value)}
                  placeholder="Optional description…"
                  style={{ width: '100%', fontSize: 13, padding: '8px 10px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', boxSizing: 'border-box' }} />
              </div>
              <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                <input type="checkbox" id="net-active" checked={formActive} onChange={e => setFormActive(e.target.checked)} />
                <label htmlFor="net-active" style={{ fontSize: 13, color: 'var(--text-primary)' }}>Active (network will be used in routing)</label>
              </div>

              {/* Source priority list */}
              <div>
                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 8 }}>
                  <label style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-primary)' }}>Fulfilment Sources (in priority order)</label>
                  <button className="btn-sec" style={{ fontSize: 11, padding: '4px 10px' }} onClick={addSourceEntry}>
                    <Plus size={11} /> Add Source
                  </button>
                </div>
                {formSources.length === 0 && (
                  <p style={{ fontSize: 12, color: 'var(--text-muted)', fontStyle: 'italic' }}>No sources added. Click "Add Source" to add fulfilment sources in priority order.</p>
                )}
                <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                  {formSources.map((s, i) => (
                    <div key={i} style={{ display: 'flex', alignItems: 'center', gap: 8, background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 8, padding: '8px 12px' }}>
                      <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
                        <button onClick={() => movePriority(i, -1)} disabled={i === 0} style={{ background: 'none', border: 'none', cursor: i > 0 ? 'pointer' : 'default', color: i > 0 ? 'var(--text-primary)' : 'var(--text-muted)', padding: 0 }}><ChevronUp size={14} /></button>
                        <button onClick={() => movePriority(i, 1)} disabled={i === formSources.length - 1} style={{ background: 'none', border: 'none', cursor: i < formSources.length - 1 ? 'pointer' : 'default', color: i < formSources.length - 1 ? 'var(--text-primary)' : 'var(--text-muted)', padding: 0 }}><ChevronDown size={14} /></button>
                      </div>
                      <span style={{ width: 20, height: 20, borderRadius: '50%', background: 'var(--primary)', color: '#fff', display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 10, fontWeight: 700, flexShrink: 0 }}>{i + 1}</span>
                      <select value={s.source_id} onChange={e => updateSourceEntry(i, 'source_id', e.target.value)}
                        style={{ flex: 1, fontSize: 12, padding: '4px 8px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 4, color: 'var(--text-primary)' }}>
                        {sources.map(src => <option key={src.source_id} value={src.source_id}>{src.name} ({src.type})</option>)}
                      </select>
                      <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-start', gap: 2 }}>
                        <label style={{ fontSize: 10, color: 'var(--text-muted)' }}>Min stock</label>
                        <input type="number" min={0} value={s.min_stock}
                          onChange={e => updateSourceEntry(i, 'min_stock', parseInt(e.target.value) || 0)}
                          style={{ width: 60, fontSize: 12, padding: '4px 6px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 4, color: 'var(--text-primary)' }} />
                      </div>
                      <button onClick={() => removeSourceEntry(i)} style={{ background: 'none', border: 'none', cursor: 'pointer', color: '#ef4444', padding: 4 }}>
                        <Trash2 size={13} />
                      </button>
                    </div>
                  ))}
                </div>
              </div>
            </div>

            <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 10, marginTop: 24 }}>
              <button className="btn-sec" onClick={() => setShowForm(false)}>Cancel</button>
              <button className="btn-pri" onClick={saveNetwork} disabled={saving || !formName.trim()}>
                {saving ? 'Saving…' : editingNetwork ? 'Save Changes' : 'Create Network'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

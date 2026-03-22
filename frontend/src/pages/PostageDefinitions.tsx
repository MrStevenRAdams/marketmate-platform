import { useState, useEffect } from 'react';
import { getActiveTenantId } from '../contexts/TenantContext';

const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';
function api(path: string, init?: RequestInit) {
  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId(), ...init?.headers },
  });
}

interface PostageRule {
  condition_type: string;
  condition_value: string;
  weight_min: number;
  weight_max: number;
  carrier_id: string;
  service_id: string;
}

interface PostageDefinition {
  definition_id: string;
  name: string;
  rules: PostageRule[];
  default_carrier_id: string;
  default_service_id: string;
  created_at: string;
}

const CONDITION_TYPES = [
  { value: 'weight_range', label: 'Weight Range (kg)' },
  { value: 'channel', label: 'Channel' },
  { value: 'destination_country', label: 'Destination Country' },
];

const emptyRule = (): PostageRule => ({
  condition_type: 'weight_range',
  condition_value: '',
  weight_min: 0,
  weight_max: 1,
  carrier_id: '',
  service_id: '',
});

export default function PostageDefinitions() {
  const [definitions, setDefinitions] = useState<PostageDefinition[]>([]);
  const [showModal, setShowModal] = useState(false);
  const [editing, setEditing] = useState<PostageDefinition | null>(null);
  const [formName, setFormName] = useState('');
  const [formRules, setFormRules] = useState<PostageRule[]>([]);
  const [formDefaultCarrier, setFormDefaultCarrier] = useState('');
  const [formDefaultService, setFormDefaultService] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => { load(); }, []);

  async function load() {
    const res = await api('/postage-definitions');
    if (res.ok) {
      const data = await res.json();
      setDefinitions(data.definitions || []);
    }
  }

  function openCreate() {
    setEditing(null);
    setFormName('');
    setFormRules([]);
    setFormDefaultCarrier('');
    setFormDefaultService('');
    setError('');
    setShowModal(true);
  }

  function openEdit(def: PostageDefinition) {
    setEditing(def);
    setFormName(def.name);
    setFormRules(def.rules || []);
    setFormDefaultCarrier(def.default_carrier_id || '');
    setFormDefaultService(def.default_service_id || '');
    setError('');
    setShowModal(true);
  }

  async function save() {
    if (!formName.trim()) { setError('Name required'); return; }
    setLoading(true);
    try {
      const body = JSON.stringify({ name: formName, rules: formRules, default_carrier_id: formDefaultCarrier, default_service_id: formDefaultService });
      const res = editing
        ? await api(`/postage-definitions/${editing.definition_id}`, { method: 'PUT', body })
        : await api('/postage-definitions', { method: 'POST', body });
      if (!res.ok) throw new Error(await res.text());
      setShowModal(false);
      load();
    } catch (e: any) { setError(e.message); }
    finally { setLoading(false); }
  }

  async function deleteDef(id: string) {
    if (!confirm('Delete this postage definition?')) return;
    await api(`/postage-definitions/${id}`, { method: 'DELETE' });
    load();
  }

  function updateRule(idx: number, field: keyof PostageRule, value: any) {
    setFormRules(prev => prev.map((r, i) => i === idx ? { ...r, [field]: value } : r));
  }

  return (
    <div style={{ padding: 24, maxWidth: 1000, margin: '0 auto' }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 24 }}>
        <div>
          <h1 style={{ color: 'var(--text-primary)', marginBottom: 4 }}>📮 Postage Definitions</h1>
          <p style={{ color: 'var(--text-muted)' }}>Define rules to auto-assign carrier services to orders.</p>
        </div>
        <button onClick={openCreate}
          style={{ padding: '10px 20px', background: 'var(--primary)', border: 'none', borderRadius: 8, color: 'white', fontWeight: 700, cursor: 'pointer' }}>
          + New Definition
        </button>
      </div>

      {definitions.length === 0 ? (
        <div style={{ textAlign: 'center', padding: 60, color: 'var(--text-muted)' }}>
          <div style={{ fontSize: 40 }}>📮</div>
          <div style={{ marginTop: 12 }}>No postage definitions yet.</div>
          <button onClick={openCreate} style={{ marginTop: 16, padding: '10px 20px', background: 'var(--primary)', border: 'none', borderRadius: 8, color: 'white', cursor: 'pointer' }}>
            Create First Definition
          </button>
        </div>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          {definitions.map(def => (
            <div key={def.definition_id} style={{ background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, padding: 20 }}>
              <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between' }}>
                <div>
                  <div style={{ fontWeight: 700, color: 'var(--text-primary)', fontSize: 15 }}>{def.name}</div>
                  <div style={{ color: 'var(--text-muted)', fontSize: 12, marginTop: 4 }}>
                    {def.rules?.length || 0} rules · Default: {def.default_carrier_id || 'none'}
                  </div>
                </div>
                <div style={{ display: 'flex', gap: 8 }}>
                  <button onClick={() => openEdit(def)}
                    style={{ padding: '6px 14px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-secondary)', cursor: 'pointer', fontSize: 12 }}>
                    Edit
                  </button>
                  <button onClick={() => deleteDef(def.definition_id)}
                    style={{ padding: '6px 14px', background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 6, color: '#ef4444', cursor: 'pointer', fontSize: 12 }}>
                    Delete
                  </button>
                </div>
              </div>
              {def.rules?.length > 0 && (
                <div style={{ marginTop: 12, display: 'flex', flexDirection: 'column', gap: 4 }}>
                  {def.rules.map((r, i) => (
                    <div key={i} style={{ fontSize: 12, color: 'var(--text-secondary)', background: 'var(--bg-secondary)', borderRadius: 4, padding: '4px 10px' }}>
                      {r.condition_type === 'weight_range' ? `Weight ${r.weight_min}–${r.weight_max}kg` : `${r.condition_type}: ${r.condition_value}`}
                      {' → '}{r.carrier_id}{r.service_id ? `/${r.service_id}` : ''}
                    </div>
                  ))}
                </div>
              )}
            </div>
          ))}
        </div>
      )}

      {/* Modal */}
      {showModal && (
        <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.6)', zIndex: 300, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
          <div style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 12, width: '90%', maxWidth: 640, maxHeight: '90vh', overflowY: 'auto', padding: 24 }}>
            <h3 style={{ color: 'var(--text-primary)', marginBottom: 20 }}>{editing ? 'Edit' : 'New'} Postage Definition</h3>

            {error && <div style={{ background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 6, padding: '8px 12px', marginBottom: 12, color: '#ef4444', fontSize: 13 }}>{error}</div>}

            <div style={{ marginBottom: 16 }}>
              <label style={{ display: 'block', marginBottom: 6, fontSize: 13, color: 'var(--text-secondary)' }}>Name</label>
              <input value={formName} onChange={e => setFormName(e.target.value)}
                style={{ width: '100%', padding: '10px 14px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-primary)' }} />
            </div>

            <div style={{ display: 'flex', gap: 12, marginBottom: 20 }}>
              <div style={{ flex: 1 }}>
                <label style={{ display: 'block', marginBottom: 6, fontSize: 13, color: 'var(--text-secondary)' }}>Default Carrier ID</label>
                <input value={formDefaultCarrier} onChange={e => setFormDefaultCarrier(e.target.value)} placeholder="e.g. royal_mail"
                  style={{ width: '100%', padding: '10px 14px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-primary)' }} />
              </div>
              <div style={{ flex: 1 }}>
                <label style={{ display: 'block', marginBottom: 6, fontSize: 13, color: 'var(--text-secondary)' }}>Default Service ID</label>
                <input value={formDefaultService} onChange={e => setFormDefaultService(e.target.value)} placeholder="e.g. tracked_48"
                  style={{ width: '100%', padding: '10px 14px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-primary)' }} />
              </div>
            </div>

            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 10 }}>
              <h4 style={{ color: 'var(--text-primary)', margin: 0, fontSize: 14 }}>Rules</h4>
              <button onClick={() => setFormRules(prev => [...prev, emptyRule()])}
                style={{ padding: '5px 12px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-secondary)', cursor: 'pointer', fontSize: 12 }}>
                + Add Rule
              </button>
            </div>

            {formRules.map((rule, idx) => (
              <div key={idx} style={{ background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, padding: 14, marginBottom: 8 }}>
                <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'flex-end' }}>
                  <div style={{ flex: '1 1 160px' }}>
                    <label style={{ display: 'block', marginBottom: 4, fontSize: 11, color: 'var(--text-muted)' }}>Condition Type</label>
                    <select value={rule.condition_type} onChange={e => updateRule(idx, 'condition_type', e.target.value)}
                      style={{ width: '100%', padding: '7px 10px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', fontSize: 12 }}>
                      {CONDITION_TYPES.map(ct => <option key={ct.value} value={ct.value}>{ct.label}</option>)}
                    </select>
                  </div>
                  {rule.condition_type === 'weight_range' ? (
                    <>
                      <div style={{ flex: '0 0 80px' }}>
                        <label style={{ display: 'block', marginBottom: 4, fontSize: 11, color: 'var(--text-muted)' }}>Min (kg)</label>
                        <input type="number" step={0.1} value={rule.weight_min} onChange={e => updateRule(idx, 'weight_min', parseFloat(e.target.value))}
                          style={{ width: '100%', padding: '7px 10px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', fontSize: 12 }} />
                      </div>
                      <div style={{ flex: '0 0 80px' }}>
                        <label style={{ display: 'block', marginBottom: 4, fontSize: 11, color: 'var(--text-muted)' }}>Max (kg)</label>
                        <input type="number" step={0.1} value={rule.weight_max} onChange={e => updateRule(idx, 'weight_max', parseFloat(e.target.value))}
                          style={{ width: '100%', padding: '7px 10px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', fontSize: 12 }} />
                      </div>
                    </>
                  ) : (
                    <div style={{ flex: '1 1 140px' }}>
                      <label style={{ display: 'block', marginBottom: 4, fontSize: 11, color: 'var(--text-muted)' }}>Value</label>
                      <input value={rule.condition_value} onChange={e => updateRule(idx, 'condition_value', e.target.value)} placeholder="e.g. amazon or GB"
                        style={{ width: '100%', padding: '7px 10px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', fontSize: 12 }} />
                    </div>
                  )}
                  <div style={{ flex: '1 1 100px' }}>
                    <label style={{ display: 'block', marginBottom: 4, fontSize: 11, color: 'var(--text-muted)' }}>Carrier ID</label>
                    <input value={rule.carrier_id} onChange={e => updateRule(idx, 'carrier_id', e.target.value)}
                      style={{ width: '100%', padding: '7px 10px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', fontSize: 12 }} />
                  </div>
                  <div style={{ flex: '1 1 100px' }}>
                    <label style={{ display: 'block', marginBottom: 4, fontSize: 11, color: 'var(--text-muted)' }}>Service ID</label>
                    <input value={rule.service_id} onChange={e => updateRule(idx, 'service_id', e.target.value)}
                      style={{ width: '100%', padding: '7px 10px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', fontSize: 12 }} />
                  </div>
                  <button onClick={() => setFormRules(prev => prev.filter((_, i) => i !== idx))}
                    style={{ padding: '7px 10px', background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 6, color: '#ef4444', cursor: 'pointer' }}>×</button>
                </div>
              </div>
            ))}

            <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 12, marginTop: 20 }}>
              <button onClick={() => setShowModal(false)}
                style={{ padding: '10px 20px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-secondary)', cursor: 'pointer' }}>
                Cancel
              </button>
              <button onClick={save} disabled={loading}
                style={{ padding: '10px 24px', background: 'var(--primary)', border: 'none', borderRadius: 8, color: 'white', fontWeight: 700, cursor: 'pointer' }}>
                {loading ? 'Saving…' : 'Save Definition'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

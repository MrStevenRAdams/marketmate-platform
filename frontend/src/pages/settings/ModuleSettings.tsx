import { useState, useEffect } from 'react';
import { Link } from 'react-router-dom';
import { getActiveTenantId } from '../../contexts/TenantContext';
import '../../components/SettingsLayout.css';

const API = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';
function api(path: string, init?: RequestInit) {
  return fetch(`${API}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId(), ...(init?.headers || {}) },
  });
}

interface EnabledModules {
  wms: boolean;
  advanced_dispatch: boolean;
  purchase_orders: boolean;
  automation: boolean;
  rma: boolean;
  advanced_analytics: boolean;
  email_system: boolean;
}

type Toast = { msg: string; type: 'success' | 'error' } | null;

const allOn: EnabledModules = {
  wms: true,
  advanced_dispatch: true,
  purchase_orders: true,
  automation: true,
  rma: true,
  advanced_analytics: true,
  email_system: true,
};

interface ModuleDef {
  key: keyof EnabledModules;
  icon: string;
  title: string;
  description: string;
  screens: string[];
  impact: string;
}

const MODULE_DEFS: ModuleDef[] = [
  {
    key: 'wms',
    icon: '🏗️',
    title: 'Advanced Warehouse (WMS)',
    description: 'Full warehouse management with bin locations, stock counting, transfers, pick replenishment, and FIFO allocation.',
    screens: ['Warehouse Locations', 'Storage Groups', 'Stock Count', 'Stock In', 'Scrap History', 'Transfers', 'Pick Replenishment', 'Stock Moves', 'Bin Types Settings', 'WMS Settings'],
    impact: 'When off, only "My Inventory" (simple stock levels) is shown.',
  },
  {
    key: 'advanced_dispatch',
    icon: '🚀',
    title: 'Advanced Dispatch',
    description: 'Power-user despatch tools including batch processing, pickwaves, manifests, SLA tracking, and shipping/packaging rules.',
    screens: ['Despatch Console', 'Pickwaves', 'Manifests', 'Label Printing', 'SLA Dashboard', 'Shipping Rules', 'Packaging Rules', 'Delivery Exceptions'],
    impact: 'When off, basic dispatch (mark as shipped) is still available in Orders.',
  },
  {
    key: 'purchase_orders',
    icon: '📄',
    title: 'Purchase Orders & Suppliers',
    description: 'Formal purchase order workflow with supplier management, demand forecasting, and automated replenishment suggestions.',
    screens: ['Purchase Orders', 'Suppliers', 'Forecasting', 'Replenishment'],
    impact: 'When off, you can still manually adjust stock levels.',
  },
  {
    key: 'automation',
    icon: '🤖',
    title: 'Automation & Workflows',
    description: 'Rule-based workflow engine for automatically assigning carriers, tagging orders, routing fulfilment, and custom triggers.',
    screens: ['Workflows', 'Automation Rules', 'Workflow Simulator', 'Automation Logs'],
    impact: 'When off, orders follow default processing without automated rules.',
  },
  {
    key: 'rma',
    icon: '↩️',
    title: 'Returns Management',
    description: 'Formal RMA (Return Merchandise Authorisation) tracking with status workflows, refund processing, and a customer-facing returns portal.',
    screens: ['RMAs', 'RMA Detail', 'Returns Portal'],
    impact: 'When off, returns are handled manually outside the system.',
  },
  {
    key: 'advanced_analytics',
    icon: '📊',
    title: 'Advanced Analytics',
    description: 'Deep dashboards for inventory health, order performance, pivot analysis, operational KPIs, and custom report building.',
    screens: ['Inventory Dashboard', 'Order Dashboard', 'Pivot Analytics', 'Reporting Hub', 'Operational Dashboard'],
    impact: 'When off, the basic Analytics overview and Report Builder are still available.',
  },
  {
    key: 'email_system',
    icon: '✉️',
    title: 'Email System',
    description: 'Built-in transactional email with custom templates, SMTP configuration, delivery logs, and sent mail tracking.',
    screens: ['Email Templates', 'Email Logs', 'Sent Mail Log', 'Email Settings'],
    impact: 'When off, email features are hidden. You can still use marketplace-native messaging.',
  },
];

export default function ModuleSettings() {
  const [modules, setModules] = useState<EnabledModules>(allOn);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [toast, setToast] = useState<Toast>(null);
  const [expandedCard, setExpandedCard] = useState<string | null>(null);

  const showToast = (msg: string, type: 'success' | 'error') => {
    setToast({ msg, type }); setTimeout(() => setToast(null), 3000);
  };

  useEffect(() => {
    api('/settings/modules').then(r => r.json()).then(d => setModules({ ...allOn, ...(d.modules || {}) }))
      .catch(() => {}).finally(() => setLoading(false));
  }, []);

  const toggle = (key: keyof EnabledModules) => {
    setModules(prev => ({ ...prev, [key]: !prev[key] }));
  };

  const save = async () => {
    setSaving(true);
    try {
      const r = await api('/settings/modules', { method: 'PUT', body: JSON.stringify(modules) });
      if (!r.ok) throw new Error(await r.text());
      showToast('Feature modules updated. Refresh to see sidebar changes.', 'success');
    } catch (e: any) { showToast(e.message || 'Save failed', 'error'); }
    finally { setSaving(false); }
  };

  const enableAll = () => setModules(allOn);
  const disableAll = () => setModules({
    wms: false, advanced_dispatch: false, purchase_orders: false,
    automation: false, rma: false, advanced_analytics: false, email_system: false,
  });

  const enabledCount = Object.values(modules).filter(v => v === true).length;

  if (loading) return <div className="settings-page"><div style={{ color: 'var(--text-muted)', padding: 40 }}>Loading…</div></div>;

  return (
    <div className="settings-page" style={{ maxWidth: 900, margin: '0 auto' }}>
      {/* Toast */}
      {toast && (
        <div style={{
          position: 'fixed', top: 24, right: 24, padding: '12px 20px', borderRadius: 10, zIndex: 9999,
          background: toast.type === 'success' ? 'rgba(34,197,94,0.15)' : 'rgba(239,68,68,0.15)',
          border: `1px solid ${toast.type === 'success' ? 'rgba(34,197,94,0.4)' : 'rgba(239,68,68,0.4)'}`,
          color: toast.type === 'success' ? '#22c55e' : '#ef4444',
          fontSize: 13, fontWeight: 600, backdropFilter: 'blur(12px)',
        }}>
          {toast.msg}
        </div>
      )}

      {/* Header */}
      <div style={{ marginBottom: 28 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 6 }}>
          <Link to="/settings" style={{ color: 'var(--text-muted)', textDecoration: 'none', fontSize: 13 }}>← Settings</Link>
        </div>
        <h1 style={{ fontSize: 22, fontWeight: 700, color: 'var(--text-primary)', margin: 0 }}>Feature Modules</h1>
        <p style={{ fontSize: 13, color: 'var(--text-muted)', marginTop: 6, lineHeight: 1.5 }}>
          Choose which features are active for your account. Disabled modules hide their menu items and screens, keeping your workspace focused on what you actually use.
          You can change these at any time.
        </p>
      </div>

      {/* Quick actions bar */}
      <div style={{
        display: 'flex', alignItems: 'center', justifyContent: 'space-between',
        padding: '12px 18px', marginBottom: 20, borderRadius: 10,
        background: 'var(--bg-secondary)', border: '1px solid var(--border)',
      }}>
        <div style={{ fontSize: 13, color: 'var(--text-secondary)' }}>
          <strong style={{ color: 'var(--text-primary)' }}>{enabledCount}</strong> of {MODULE_DEFS.length} modules enabled
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <button onClick={enableAll} style={{
            padding: '6px 14px', borderRadius: 6, fontSize: 12, fontWeight: 600,
            background: 'rgba(99,102,241,0.1)', border: '1px solid rgba(99,102,241,0.3)',
            color: '#818cf8', cursor: 'pointer',
          }}>Enable All</button>
          <button onClick={disableAll} style={{
            padding: '6px 14px', borderRadius: 6, fontSize: 12, fontWeight: 600,
            background: 'var(--bg-tertiary)', border: '1px solid var(--border)',
            color: 'var(--text-muted)', cursor: 'pointer',
          }}>Starter Mode</button>
        </div>
      </div>

      {/* Module cards */}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
        {MODULE_DEFS.map(mod => {
          const isOn = modules[mod.key];
          const isExpanded = expandedCard === mod.key;
          return (
            <div key={mod.key} style={{
              background: 'var(--bg-secondary)',
              border: `1px solid ${isOn ? 'rgba(99,102,241,0.3)' : 'var(--border)'}`,
              borderRadius: 12, overflow: 'hidden',
              transition: 'border-color 0.2s ease',
            }}>
              {/* Card header */}
              <div style={{
                display: 'flex', alignItems: 'center', gap: 14, padding: '16px 20px',
                cursor: 'pointer',
              }}
                onClick={() => setExpandedCard(isExpanded ? null : mod.key)}
              >
                <div style={{
                  width: 40, height: 40, borderRadius: 10, display: 'flex',
                  alignItems: 'center', justifyContent: 'center', fontSize: 20,
                  background: isOn ? 'rgba(99,102,241,0.12)' : 'var(--bg-tertiary)',
                  flexShrink: 0, transition: 'background 0.2s ease',
                }}>
                  {mod.icon}
                </div>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div style={{
                    fontSize: 14, fontWeight: 600,
                    color: isOn ? 'var(--text-primary)' : 'var(--text-muted)',
                  }}>
                    {mod.title}
                  </div>
                  <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>
                    {mod.description}
                  </div>
                </div>
                <span style={{ fontSize: 11, color: 'var(--text-muted)', marginRight: 8 }}>
                  {isExpanded ? '▾' : '›'}
                </span>
                {/* Toggle switch */}
                <label
                  style={{ position: 'relative', display: 'inline-flex', alignItems: 'center', cursor: 'pointer', flexShrink: 0 }}
                  onClick={(e) => e.stopPropagation()}
                >
                  <input type="checkbox" checked={isOn} onChange={() => toggle(mod.key)} style={{ position: 'absolute', opacity: 0, width: 0 }} />
                  <div style={{
                    width: 44, height: 24, borderRadius: 12,
                    background: isOn ? 'var(--primary, #7c3aed)' : 'var(--border)',
                    transition: '0.2s', position: 'relative',
                  }}>
                    <div style={{
                      position: 'absolute', top: 2, left: isOn ? 22 : 2, width: 20, height: 20,
                      borderRadius: '50%', background: '#fff', transition: '0.2s',
                      boxShadow: '0 1px 3px rgba(0,0,0,0.2)',
                    }} />
                  </div>
                </label>
              </div>

              {/* Expanded detail */}
              {isExpanded && (
                <div style={{
                  padding: '0 20px 16px',
                  borderTop: '1px solid var(--border)',
                  paddingTop: 14,
                }}>
                  <div style={{ marginBottom: 10 }}>
                    <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.5px', marginBottom: 6 }}>
                      Screens included
                    </div>
                    <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
                      {mod.screens.map(s => (
                        <span key={s} style={{
                          padding: '3px 10px', borderRadius: 6, fontSize: 12,
                          background: isOn ? 'rgba(99,102,241,0.08)' : 'var(--bg-tertiary)',
                          border: `1px solid ${isOn ? 'rgba(99,102,241,0.2)' : 'var(--border)'}`,
                          color: isOn ? '#818cf8' : 'var(--text-muted)',
                        }}>
                          {s}
                        </span>
                      ))}
                    </div>
                  </div>
                  <div style={{
                    fontSize: 12, color: 'var(--text-muted)', lineHeight: 1.5,
                    padding: '8px 12px', borderRadius: 8,
                    background: 'var(--bg-tertiary)',
                  }}>
                    💡 {mod.impact}
                  </div>
                </div>
              )}
            </div>
          );
        })}
      </div>

      {/* Save button */}
      <div style={{
        display: 'flex', justifyContent: 'flex-end', marginTop: 24, paddingTop: 20,
        borderTop: '1px solid var(--border)',
      }}>
        <button
          onClick={save}
          disabled={saving}
          style={{
            padding: '10px 28px', borderRadius: 8, fontSize: 14, fontWeight: 600,
            background: 'var(--primary, #7c3aed)', border: 'none', color: '#fff',
            cursor: saving ? 'wait' : 'pointer', opacity: saving ? 0.7 : 1,
          }}
        >
          {saving ? 'Saving…' : 'Save Changes'}
        </button>
      </div>
    </div>
  );
}

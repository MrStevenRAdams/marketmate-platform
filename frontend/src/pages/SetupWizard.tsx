import { useState, useEffect } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { getActiveTenantId } from '../contexts/TenantContext';
import { useAuth } from '../contexts/AuthContext';

const API = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';
function api(path: string, init?: RequestInit) {
  return fetch(`${API}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId(), ...(init?.headers || {}) },
  });
}

// ── Module definitions (same as ModuleSettings) ──────────────────────────────

interface ModuleToggle {
  key: string;
  icon: string;
  title: string;
  desc: string;
}

const MODULES: ModuleToggle[] = [
  { key: 'wms', icon: '🏗️', title: 'Warehouse Management', desc: 'Bin locations, stock counting, transfers, pick replenishment' },
  { key: 'advanced_dispatch', icon: '🚀', title: 'Advanced Dispatch', desc: 'Despatch console, pickwaves, manifests, SLA tracking' },
  { key: 'purchase_orders', icon: '📄', title: 'Purchase Orders & Suppliers', desc: 'PO workflow, supplier management, forecasting' },
  { key: 'automation', icon: '🤖', title: 'Automation & Workflows', desc: 'Rule-based order routing, carrier assignment, auto-tagging' },
  { key: 'advanced_analytics', icon: '📊', title: 'Advanced Analytics', desc: 'Inventory, order & operational dashboards, pivot analysis' },
  { key: 'email_system', icon: '✉️', title: 'Email System', desc: 'Custom email templates, SMTP, delivery logs' },
];

// ── Channel definitions ──────────────────────────────────────────────────────

interface ChannelDef {
  id: string;
  label: string;
  icon: string;
  color: string;
  desc: string;
}

const CHANNELS: ChannelDef[] = [
  { id: 'amazon', label: 'Amazon', icon: '📦', color: '#FF9900', desc: 'Amazon Seller Central (SP-API)' },
  { id: 'ebay', label: 'eBay', icon: '🏷️', color: '#E53238', desc: 'eBay Seller Hub' },
  { id: 'temu', label: 'Temu', icon: '🛍️', color: '#F97316', desc: 'Temu Marketplace' },
  { id: 'shopify', label: 'Shopify', icon: '🛒', color: '#96BF48', desc: 'Shopify Store' },
  { id: 'etsy', label: 'Etsy', icon: '🧶', color: '#F1641E', desc: 'Etsy Shop' },
  { id: 'woocommerce', label: 'WooCommerce', icon: '🟣', color: '#96588A', desc: 'WooCommerce Store' },
  { id: 'tiktok', label: 'TikTok Shop', icon: '🎵', color: '#000000', desc: 'TikTok Shop' },
  { id: 'walmart', label: 'Walmart', icon: '🟡', color: '#0071CE', desc: 'Walmart Marketplace' },
];

// ── Preset profiles ──────────────────────────────────────────────────────────

function starterModules(): Record<string, boolean> {
  return { wms: false, advanced_dispatch: false, purchase_orders: false, automation: false, rma: true, advanced_analytics: false, email_system: false };
}
function proModules(): Record<string, boolean> {
  return { wms: true, advanced_dispatch: true, purchase_orders: true, automation: true, rma: true, advanced_analytics: true, email_system: true };
}
function suggestModules(productCount: string, ordersPerDay: string, hasWarehouse: boolean): Record<string, boolean> {
  const isBig = productCount === '500+' || ordersPerDay === '50+';
  const isMed = productCount === '51-500' || ordersPerDay === '11-50';
  if (isBig) return proModules();
  if (isMed || hasWarehouse) return {
    wms: hasWarehouse, advanced_dispatch: false, purchase_orders: false,
    automation: true, rma: false, advanced_analytics: false, email_system: false,
  };
  return starterModules();
}

// ── Styles ───────────────────────────────────────────────────────────────────

const pageStyle: React.CSSProperties = {
  minHeight: '100vh', display: 'flex', flexDirection: 'column',
  alignItems: 'center', justifyContent: 'center',
  background: 'var(--bg-primary)', padding: '40px 20px',
};
const cardStyle: React.CSSProperties = {
  width: '100%', maxWidth: 680, background: 'var(--bg-secondary)',
  border: '1px solid var(--border)', borderRadius: 16, padding: '36px 40px',
  boxShadow: '0 8px 32px rgba(0,0,0,0.25)',
};
const stepDotStyle = (active: boolean, done: boolean): React.CSSProperties => ({
  width: 32, height: 32, borderRadius: '50%',
  display: 'flex', alignItems: 'center', justifyContent: 'center',
  fontSize: 13, fontWeight: 700,
  background: done ? 'var(--primary, #7c3aed)' : active ? 'rgba(124,58,237,0.15)' : 'var(--bg-tertiary)',
  color: done ? '#fff' : active ? 'var(--primary, #7c3aed)' : 'var(--text-muted)',
  border: active ? '2px solid var(--primary, #7c3aed)' : '2px solid transparent',
  transition: 'all 0.2s ease',
});
const connectorStyle = (done: boolean): React.CSSProperties => ({
  width: 40, height: 2,
  background: done ? 'var(--primary, #7c3aed)' : 'var(--border)',
  transition: 'background 0.2s ease',
});
const btnPrimary: React.CSSProperties = {
  padding: '11px 28px', borderRadius: 8, fontSize: 14, fontWeight: 600,
  background: 'var(--primary, #7c3aed)', border: 'none', color: '#fff',
  cursor: 'pointer',
};
const btnSecondary: React.CSSProperties = {
  padding: '11px 28px', borderRadius: 8, fontSize: 14, fontWeight: 600,
  background: 'var(--bg-tertiary)', border: '1px solid var(--border)',
  color: 'var(--text-secondary)', cursor: 'pointer',
};

type Step = 1 | 2 | 3 | 4;

export default function SetupWizard() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const { activeTenant } = useAuth();
  const referral = searchParams.get('ref') || '';

  const [step, setStep] = useState<Step>(1);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  // Step 1: Business profile
  const [productCount, setProductCount] = useState('');
  const [ordersPerDay, setOrdersPerDay] = useState('');
  const [hasWarehouse, setHasWarehouse] = useState(false);

  // Step 2: Channel interest (just informational, actual connection happens in main app)
  const [selectedChannels, setSelectedChannels] = useState<Set<string>>(new Set());

  // Step 3: Modules
  const [modules, setModules] = useState<Record<string, boolean>>(starterModules());
  const [modulesInitialised, setModulesInitialised] = useState(false);

  // Pre-select Temu if referred
  useEffect(() => {
    if (referral === 'temu') {
      setSelectedChannels(new Set(['temu']));
    }
  }, [referral]);

  // When moving from step 1 to step 2, auto-suggest modules based on answers
  useEffect(() => {
    if (step === 3 && !modulesInitialised && productCount && ordersPerDay) {
      setModules(suggestModules(productCount, ordersPerDay, hasWarehouse));
      setModulesInitialised(true);
    }
  }, [step, modulesInitialised, productCount, ordersPerDay, hasWarehouse]);

  const toggleChannel = (id: string) => {
    setSelectedChannels(prev => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id); else next.add(id);
      return next;
    });
  };

  const toggleModule = (key: string) => {
    setModules(prev => ({ ...prev, [key]: !prev[key] }));
  };

  const canProceed = (): boolean => {
    if (step === 1) return !!productCount && !!ordersPerDay;
    return true;
  };

  const handleFinish = async () => {
    setSaving(true);
    setError('');
    try {
      const res = await api('/settings/setup-complete', {
        method: 'POST',
        body: JSON.stringify({
          business_size: hasWarehouse ? 'warehouse' : (productCount === '500+' ? 'large' : productCount === '51-500' ? 'medium' : 'small'),
          product_count: productCount,
          orders_per_day: ordersPerDay,
          has_warehouse: hasWarehouse,
          modules: modules,
          selected_channels: Array.from(selectedChannels),
        }),
      });
      if (!res.ok) {
        const data = await res.json().catch(() => ({}));
        throw new Error(data.error || 'Setup failed');
      }

      // If they selected channels, redirect to connections page; otherwise dashboard
      if (selectedChannels.size > 0) {
        navigate('/marketplace/connections', { replace: true });
      } else {
        navigate('/dashboard', { replace: true });
      }
    } catch (e: any) {
      setError(e.message);
    } finally {
      setSaving(false);
    }
  };

  const STEP_LABELS = ['Your Business', 'Channels', 'Features', 'Ready'];

  return (
    <div style={pageStyle}>
      {/* Logo */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 32 }}>
        <div style={{
          width: 38, height: 38, borderRadius: 10, display: 'flex', alignItems: 'center', justifyContent: 'center',
          background: 'linear-gradient(135deg, var(--primary, #7c3aed), #a855f7)', fontSize: 20,
        }}>🎯</div>
        <span style={{ fontSize: 20, fontWeight: 700, color: 'var(--text-primary)' }}>MarketMate</span>
      </div>

      {/* Step indicator */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 0, marginBottom: 28 }}>
        {STEP_LABELS.map((label, i) => (
          <div key={i} style={{ display: 'flex', alignItems: 'center', gap: 0 }}>
            {i > 0 && <div style={connectorStyle(step > i)} />}
            <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 4 }}>
              <div style={stepDotStyle(step === i + 1, step > i + 1)}>
                {step > i + 1 ? '✓' : i + 1}
              </div>
              <span style={{ fontSize: 11, color: step === i + 1 ? 'var(--text-primary)' : 'var(--text-muted)', fontWeight: step === i + 1 ? 600 : 400 }}>
                {label}
              </span>
            </div>
          </div>
        ))}
      </div>

      {/* Card */}
      <div style={cardStyle}>
        {error && (
          <div style={{ padding: '10px 14px', borderRadius: 8, marginBottom: 16, background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)', color: '#ef4444', fontSize: 13 }}>
            {error}
          </div>
        )}

        {/* ── STEP 1: Your Business ──────────────────────────────────── */}
        {step === 1 && (
          <div>
            <h2 style={{ fontSize: 20, fontWeight: 700, color: 'var(--text-primary)', margin: '0 0 6px' }}>Tell us about your business</h2>
            <p style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 24, lineHeight: 1.5 }}>
              This helps us configure your workspace with the right features. You can change everything later.
            </p>

            <div style={{ marginBottom: 20 }}>
              <label style={{ display: 'block', fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 8 }}>How many products do you sell?</label>
              <div style={{ display: 'flex', gap: 10 }}>
                {[['1-50', '1–50 products'], ['51-500', '51–500 products'], ['500+', '500+ products']].map(([val, label]) => (
                  <button key={val} onClick={() => setProductCount(val)} style={{
                    flex: 1, padding: '14px 16px', borderRadius: 10, cursor: 'pointer',
                    background: productCount === val ? 'rgba(124,58,237,0.1)' : 'var(--bg-tertiary)',
                    border: productCount === val ? '2px solid var(--primary, #7c3aed)' : '2px solid var(--border)',
                    color: productCount === val ? 'var(--primary, #7c3aed)' : 'var(--text-secondary)',
                    fontSize: 13, fontWeight: productCount === val ? 700 : 500, transition: 'all 0.15s',
                  }}>{label}</button>
                ))}
              </div>
            </div>

            <div style={{ marginBottom: 20 }}>
              <label style={{ display: 'block', fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 8 }}>How many orders per day?</label>
              <div style={{ display: 'flex', gap: 10 }}>
                {[['1-10', '1–10 orders'], ['11-50', '11–50 orders'], ['50+', '50+ orders']].map(([val, label]) => (
                  <button key={val} onClick={() => setOrdersPerDay(val)} style={{
                    flex: 1, padding: '14px 16px', borderRadius: 10, cursor: 'pointer',
                    background: ordersPerDay === val ? 'rgba(124,58,237,0.1)' : 'var(--bg-tertiary)',
                    border: ordersPerDay === val ? '2px solid var(--primary, #7c3aed)' : '2px solid var(--border)',
                    color: ordersPerDay === val ? 'var(--primary, #7c3aed)' : 'var(--text-secondary)',
                    fontSize: 13, fontWeight: ordersPerDay === val ? 700 : 500, transition: 'all 0.15s',
                  }}>{label}</button>
                ))}
              </div>
            </div>

            <div style={{ marginBottom: 8 }}>
              <label style={{
                display: 'flex', alignItems: 'center', gap: 12, padding: '14px 16px', borderRadius: 10, cursor: 'pointer',
                background: hasWarehouse ? 'rgba(124,58,237,0.1)' : 'var(--bg-tertiary)',
                border: hasWarehouse ? '2px solid var(--primary, #7c3aed)' : '2px solid var(--border)',
                transition: 'all 0.15s',
              }}>
                <input type="checkbox" checked={hasWarehouse} onChange={e => setHasWarehouse(e.target.checked)} style={{ width: 18, height: 18, accentColor: 'var(--primary, #7c3aed)' }} />
                <div>
                  <div style={{ fontSize: 13, fontWeight: 600, color: hasWarehouse ? 'var(--primary, #7c3aed)' : 'var(--text-secondary)' }}>I have a dedicated warehouse</div>
                  <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>With bin locations, shelving, and staff doing pick & pack</div>
                </div>
              </label>
            </div>
          </div>
        )}

        {/* ── STEP 2: Channels ───────────────────────────────────────── */}
        {step === 2 && (
          <div>
            <h2 style={{ fontSize: 20, fontWeight: 700, color: 'var(--text-primary)', margin: '0 0 6px' }}>Where do you sell?</h2>
            <p style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 24, lineHeight: 1.5 }}>
              Select the channels you use or plan to use. You'll connect them after setup.
            </p>

            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 10 }}>
              {CHANNELS.map(ch => {
                const sel = selectedChannels.has(ch.id);
                return (
                  <button key={ch.id} onClick={() => toggleChannel(ch.id)} style={{
                    display: 'flex', alignItems: 'center', gap: 12,
                    padding: '14px 16px', borderRadius: 10, cursor: 'pointer', textAlign: 'left',
                    background: sel ? `${ch.color}12` : 'var(--bg-tertiary)',
                    border: sel ? `2px solid ${ch.color}` : '2px solid var(--border)',
                    transition: 'all 0.15s',
                  }}>
                    <span style={{ fontSize: 22, flexShrink: 0 }}>{ch.icon}</span>
                    <div>
                      <div style={{ fontSize: 14, fontWeight: 600, color: sel ? ch.color : 'var(--text-secondary)' }}>{ch.label}</div>
                      <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 1 }}>{ch.desc}</div>
                    </div>
                    {sel && <span style={{ marginLeft: 'auto', color: ch.color, fontSize: 16, fontWeight: 700 }}>✓</span>}
                  </button>
                );
              })}
            </div>

            <p style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 14, textAlign: 'center' }}>
              You can skip this and connect channels later from Settings → Marketplace Connections
            </p>
          </div>
        )}

        {/* ── STEP 3: Features ───────────────────────────────────────── */}
        {step === 3 && (
          <div>
            <h2 style={{ fontSize: 20, fontWeight: 700, color: 'var(--text-primary)', margin: '0 0 6px' }}>Choose your features</h2>
            <p style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 8, lineHeight: 1.5 }}>
              We've suggested features based on your business size. Toggle any on or off — you can always change this in Settings.
            </p>

            <div style={{
              display: 'flex', gap: 8, marginBottom: 16, padding: '8px 0',
            }}>
              <button onClick={() => setModules(proModules())} style={{ ...btnSecondary, padding: '6px 14px', fontSize: 12 }}>Enable All</button>
              <button onClick={() => setModules(starterModules())} style={{ ...btnSecondary, padding: '6px 14px', fontSize: 12 }}>Minimal Setup</button>
            </div>

            <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
              {MODULES.map(mod => {
                const isOn = modules[mod.key];
                return (
                  <label key={mod.key} style={{
                    display: 'flex', alignItems: 'center', gap: 12, padding: '12px 14px', borderRadius: 10, cursor: 'pointer',
                    background: isOn ? 'rgba(124,58,237,0.06)' : 'var(--bg-tertiary)',
                    border: isOn ? '1px solid rgba(124,58,237,0.25)' : '1px solid var(--border)',
                    transition: 'all 0.15s',
                  }}>
                    <span style={{ fontSize: 20, width: 28, textAlign: 'center', flexShrink: 0 }}>{mod.icon}</span>
                    <div style={{ flex: 1, minWidth: 0 }}>
                      <div style={{ fontSize: 13, fontWeight: 600, color: isOn ? 'var(--text-primary)' : 'var(--text-muted)' }}>{mod.title}</div>
                      <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 1 }}>{mod.desc}</div>
                    </div>
                    <div style={{
                      width: 40, height: 22, borderRadius: 11, position: 'relative', flexShrink: 0,
                      background: isOn ? 'var(--primary, #7c3aed)' : 'var(--border)',
                      transition: '0.2s',
                    }} onClick={(e) => { e.preventDefault(); toggleModule(mod.key); }}>
                      <div style={{
                        position: 'absolute', top: 2, left: isOn ? 20 : 2, width: 18, height: 18,
                        borderRadius: '50%', background: '#fff', transition: '0.2s',
                        boxShadow: '0 1px 3px rgba(0,0,0,0.2)',
                      }} />
                    </div>
                  </label>
                );
              })}
            </div>
          </div>
        )}

        {/* ── STEP 4: Ready ──────────────────────────────────────────── */}
        {step === 4 && (
          <div style={{ textAlign: 'center' }}>
            <div style={{ fontSize: 48, marginBottom: 16 }}>🚀</div>
            <h2 style={{ fontSize: 22, fontWeight: 700, color: 'var(--text-primary)', margin: '0 0 8px' }}>You're all set!</h2>
            <p style={{ fontSize: 14, color: 'var(--text-secondary)', marginBottom: 24, lineHeight: 1.6 }}>
              Your workspace is configured and ready to use.
              {selectedChannels.size > 0 && " We'll take you to connect your marketplace accounts next."}
            </p>

            <div style={{
              textAlign: 'left', padding: '16px 20px', borderRadius: 10,
              background: 'var(--bg-tertiary)', border: '1px solid var(--border)', marginBottom: 24,
            }}>
              <div style={{ fontSize: 12, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.5px', marginBottom: 10 }}>Your Setup Summary</div>
              <div style={{ fontSize: 13, color: 'var(--text-secondary)', lineHeight: 1.8 }}>
                <div>📦 <strong>{productCount}</strong> products, <strong>{ordersPerDay}</strong> orders/day</div>
                <div>{hasWarehouse ? '🏗️ Warehouse mode enabled' : '🏠 Home/small office setup'}</div>
                <div>🔗 Channels: {selectedChannels.size > 0 ? Array.from(selectedChannels).map(id => CHANNELS.find(c => c.id === id)?.label).join(', ') : 'None selected yet'}</div>
                <div>⚡ {Object.values(modules).filter(Boolean).length} of {MODULES.length} feature modules enabled</div>
              </div>
            </div>

            <div style={{ display: 'flex', gap: 10, justifyContent: 'center' }}>
              <button onClick={() => setStep(3)} style={btnSecondary}>← Back</button>
              <button onClick={handleFinish} disabled={saving} style={{ ...btnPrimary, opacity: saving ? 0.7 : 1 }}>
                {saving ? 'Setting up…' : selectedChannels.size > 0 ? 'Connect Channels →' : 'Go to Dashboard →'}
              </button>
            </div>
          </div>
        )}

        {/* ── Navigation buttons (steps 1-3) ─────────────────────────── */}
        {step < 4 && (
          <div style={{ display: 'flex', justifyContent: 'space-between', marginTop: 28, paddingTop: 20, borderTop: '1px solid var(--border)' }}>
            <div>
              {step > 1 && (
                <button onClick={() => setStep((step - 1) as Step)} style={btnSecondary}>← Back</button>
              )}
            </div>
            <div style={{ display: 'flex', gap: 10 }}>
              {step === 2 && (
                <button onClick={() => setStep(3)} style={{ ...btnSecondary, color: 'var(--text-muted)' }}>Skip</button>
              )}
              <button
                onClick={() => setStep((step + 1) as Step)}
                disabled={!canProceed()}
                style={{ ...btnPrimary, opacity: canProceed() ? 1 : 0.5, cursor: canProceed() ? 'pointer' : 'not-allowed' }}
              >
                Continue →
              </button>
            </div>
          </div>
        )}
      </div>

      {/* Footer */}
      <p style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 20 }}>
        You can change all of these settings later from Settings → Feature Modules
      </p>
    </div>
  );
}

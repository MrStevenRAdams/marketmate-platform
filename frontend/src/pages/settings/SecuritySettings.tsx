import React, { useState, useEffect } from 'react';
import { getActiveTenantId } from '../../contexts/TenantContext';

const API = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';

function api(path: string, init?: RequestInit) {
  return fetch(`${API}${path}`, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      'X-Tenant-Id': getActiveTenantId(),
      ...init?.headers,
    },
  });
}

type PasswordComplexity = 'none' | 'basic' | 'normal' | 'strong' | 'complex';

interface SecuritySettings {
  tenant_id: string;
  password_complexity: PasswordComplexity;
  password_expiry_enabled: boolean;
  password_expiry_days: number;
  prevent_password_reuse: boolean;
  prevent_password_reuse_count: number;
  email_password_enabled: boolean;
  google_sso_enabled: boolean;
  microsoft_sso_enabled: boolean;
  support_access_enabled: boolean;
  updated_at?: string;
}

const defaultSettings: SecuritySettings = {
  tenant_id: '',
  password_complexity: 'basic',
  password_expiry_enabled: false,
  password_expiry_days: 90,
  prevent_password_reuse: false,
  prevent_password_reuse_count: 5,
  email_password_enabled: true,
  google_sso_enabled: false,
  microsoft_sso_enabled: false,
  support_access_enabled: true,
};

const complexityOptions: { value: PasswordComplexity; label: string; desc: string }[] = [
  { value: 'none',    label: 'None',    desc: 'Firebase default minimum only.' },
  { value: 'basic',   label: 'Basic',   desc: 'Minimum 8 characters.' },
  { value: 'normal',  label: 'Normal',  desc: '8+ characters, mixed case and a number.' },
  { value: 'strong',  label: 'Strong',  desc: '10+ characters, mixed case, number and symbol.' },
  { value: 'complex', label: 'Complex', desc: '12+ characters, mixed case, number, symbol — no dictionary words.' },
];

// Data purge dialog
interface PurgeForm {
  date_from: string;
  date_to: string;
  channel: string;
}

// Reset confirmation
type ResetStep = 'idle' | 'confirm' | 'typing' | 'done';

export default function SecuritySettings() {
  const [settings, setSettings] = useState<SecuritySettings>(defaultSettings);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [toast, setToast] = useState<{ type: 'success' | 'error'; message: string } | null>(null);

  // Data purge state
  const [showPurge, setShowPurge] = useState(false);
  const [purgeForm, setPurgeForm] = useState<PurgeForm>({ date_from: '', date_to: '', channel: '' });
  const [purging, setPurging] = useState(false);

  // System reset state
  const [resetStep, setResetStep] = useState<ResetStep>('idle');
  const [resetInput, setResetInput] = useState('');
  const [resetting, setResetting] = useState(false);

  // Obfuscate state
  const [obfuscateEmail, setObfuscateEmail] = useState('');
  const [obfuscating, setObfuscating] = useState(false);

  useEffect(() => { load(); }, []);

  const showToast = (type: 'success' | 'error', message: string) => {
    setToast({ type, message });
    setTimeout(() => setToast(null), 4000);
  };

  async function load() {
    setLoading(true);
    try {
      const res = await api('/security-settings');
      if (res.ok) {
        const data = await res.json();
        setSettings(data.security_settings || defaultSettings);
      }
    } catch {
      showToast('error', 'Failed to load security settings.');
    } finally {
      setLoading(false);
    }
  }

  async function save() {
    setSaving(true);
    try {
      const res = await api('/security-settings', {
        method: 'PUT',
        body: JSON.stringify(settings),
      });
      if (res.ok) {
        showToast('success', 'Security settings saved.');
      } else {
        const d = await res.json();
        showToast('error', d.error || 'Failed to save settings.');
      }
    } catch {
      showToast('error', 'Failed to save settings.');
    } finally {
      setSaving(false);
    }
  }

  async function handlePurge() {
    if (!purgeForm.date_from || !purgeForm.date_to) {
      showToast('error', 'Please enter date_from and date_to.');
      return;
    }
    setPurging(true);
    try {
      const res = await api('/admin/data-purge', {
        method: 'POST',
        body: JSON.stringify({ ...purgeForm, delete_type: 'orders' }),
      });
      const data = await res.json();
      if (res.ok) {
        showToast('success', data.message || `Purged ${data.deleted} records.`);
        setShowPurge(false);
      } else {
        showToast('error', data.error || 'Purge failed.');
      }
    } catch {
      showToast('error', 'Purge request failed.');
    } finally {
      setPurging(false);
    }
  }

  async function handleObfuscateAll() {
    if (!confirm('This will anonymise ALL customer PII across all orders. This cannot be undone. Continue?')) return;
    setObfuscating(true);
    try {
      const res = await api('/admin/obfuscate-customers', { method: 'POST' });
      const data = await res.json();
      if (res.ok) {
        showToast('success', `Obfuscated ${data.obfuscated} records.`);
      } else {
        showToast('error', data.error || 'Obfuscation failed.');
      }
    } catch {
      showToast('error', 'Request failed.');
    } finally {
      setObfuscating(false);
    }
  }

  async function handleObfuscateOne() {
    if (!obfuscateEmail.trim()) {
      showToast('error', 'Enter a customer email address.');
      return;
    }
    setObfuscating(true);
    try {
      const res = await api('/admin/obfuscate-customer', {
        method: 'POST',
        body: JSON.stringify({ customer_email: obfuscateEmail.trim() }),
      });
      const data = await res.json();
      if (res.ok) {
        showToast('success', `Obfuscated ${data.obfuscated} record(s) for ${obfuscateEmail}.`);
        setObfuscateEmail('');
      } else {
        showToast('error', data.error || 'Obfuscation failed.');
      }
    } catch {
      showToast('error', 'Request failed.');
    } finally {
      setObfuscating(false);
    }
  }

  async function handleSystemReset() {
    if (resetInput.toUpperCase() !== 'RESET') {
      showToast('error', 'Type RESET (all caps) to confirm.');
      return;
    }
    setResetting(true);
    try {
      const res = await api('/admin/system-reset', {
        method: 'POST',
        body: JSON.stringify({ confirmation: resetInput }),
      });
      const data = await res.json();
      if (res.ok) {
        showToast('success', data.message || 'System reset completed.');
        setResetStep('done');
      } else {
        showToast('error', data.error || 'Reset failed.');
      }
    } catch {
      showToast('error', 'Request failed.');
    } finally {
      setResetting(false);
    }
  }

  const upd = (patch: Partial<SecuritySettings>) => setSettings(s => ({ ...s, ...patch }));

  if (loading) {
    return <div className="card" style={{ padding: 32, textAlign: 'center', color: 'var(--text-muted)' }}>Loading security settings…</div>;
  }

  return (
    <div style={{ maxWidth: 720, margin: '0 auto', padding: '24px 0' }}>
      {/* Toast */}
      {toast && (
        <div style={{
          position: 'fixed', top: 24, right: 24, zIndex: 9999,
          background: toast.type === 'success' ? '#22c55e' : '#ef4444',
          color: '#fff', padding: '12px 20px', borderRadius: 8, fontWeight: 600, fontSize: 14,
          boxShadow: '0 4px 16px rgba(0,0,0,0.3)', maxWidth: 360,
        }}>
          {toast.message}
        </div>
      )}

      <h2 style={{ fontSize: 22, fontWeight: 700, color: 'var(--text-primary)', marginBottom: 4 }}>Security Settings</h2>
      <p style={{ color: 'var(--text-muted)', fontSize: 14, marginBottom: 24 }}>
        Configure password policies, login methods, and data management options.
      </p>

      {/* ── Password Complexity ── */}
      <Section title="Password Complexity" icon="🔐">
        <p style={{ color: 'var(--text-muted)', fontSize: 13, marginBottom: 16 }}>
          Sets the required strength for new user passwords. Affects the invite acceptance and change-password flows.
        </p>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
          {complexityOptions.map(opt => (
            <label key={opt.value} style={{ display: 'flex', alignItems: 'flex-start', gap: 10, cursor: 'pointer', padding: '10px 12px', borderRadius: 6, border: `1px solid ${settings.password_complexity === opt.value ? 'var(--accent-cyan)' : 'var(--border)'}`, background: settings.password_complexity === opt.value ? 'rgba(34,211,238,0.06)' : 'transparent' }}>
              <input
                type="radio"
                name="complexity"
                value={opt.value}
                checked={settings.password_complexity === opt.value}
                onChange={() => upd({ password_complexity: opt.value })}
                style={{ marginTop: 2 }}
              />
              <div>
                <div style={{ fontWeight: 600, fontSize: 13, color: 'var(--text-primary)' }}>{opt.label}</div>
                <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>{opt.desc}</div>
              </div>
            </label>
          ))}
        </div>
      </Section>

      {/* ── Password Expiry ── */}
      <Section title="Password Expiry" icon="⏱">
        <ToggleRow
          label="Enable password expiry"
          desc="Users will be required to reset their password after the expiry period."
          checked={settings.password_expiry_enabled}
          onChange={v => upd({ password_expiry_enabled: v })}
        />
        {settings.password_expiry_enabled && (
          <div style={{ marginTop: 16, paddingLeft: 16, borderLeft: '2px solid var(--border)' }}>
            <FieldRow label="Expiry period (days)">
              <input
                type="number" min={7} max={365}
                value={settings.password_expiry_days}
                onChange={e => upd({ password_expiry_days: Number(e.target.value) })}
                style={inputStyle}
              />
            </FieldRow>
            <ToggleRow
              label="Prevent password reuse"
              desc="Users cannot reuse recent passwords."
              checked={settings.prevent_password_reuse}
              onChange={v => upd({ prevent_password_reuse: v })}
            />
            {settings.prevent_password_reuse && (
              <FieldRow label="Remember last N passwords">
                <input
                  type="number" min={1} max={24}
                  value={settings.prevent_password_reuse_count}
                  onChange={e => upd({ prevent_password_reuse_count: Number(e.target.value) })}
                  style={inputStyle}
                />
              </FieldRow>
            )}
          </div>
        )}
      </Section>

      {/* ── Login Methods ── */}
      <Section title="Login Methods" icon="🔑">
        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          <ToggleRow
            label="Email / Password"
            desc="Standard email and password sign-in. Cannot be disabled."
            checked={true}
            onChange={() => {}}
            disabled
          />
          <ToggleRow
            label="Google SSO"
            desc="Allow users to sign in with their Google account via OAuth."
            checked={settings.google_sso_enabled}
            onChange={v => upd({ google_sso_enabled: v })}
          />
          <ToggleRow
            label="Microsoft SSO"
            desc="Allow users to sign in with their Microsoft account via OAuth."
            checked={settings.microsoft_sso_enabled}
            onChange={v => upd({ microsoft_sso_enabled: v })}
          />
        </div>
      </Section>

      {/* ── Data & Privacy ── */}
      <Section title="Data & Privacy" icon="🛡">
        <ToggleRow
          label="Support access"
          desc="Allow the Marketmate support team to access your account to assist with issues."
          checked={settings.support_access_enabled}
          onChange={v => upd({ support_access_enabled: v })}
        />

        <div style={{ marginTop: 20, paddingTop: 16, borderTop: '1px solid var(--border)' }}>
          <div style={{ fontWeight: 600, fontSize: 13, color: 'var(--text-primary)', marginBottom: 8 }}>Data Deletion by Criteria</div>
          <p style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 12 }}>
            Permanently delete orders within a date range. This action cannot be undone.
          </p>
          {!showPurge ? (
            <button onClick={() => setShowPurge(true)} className="btn btn-secondary" style={{ fontSize: 13 }}>Configure & Run Purge</button>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 10, padding: 16, background: 'var(--bg-elevated)', borderRadius: 8, border: '1px solid var(--border)' }}>
              <FieldRow label="Date From">
                <input type="date" value={purgeForm.date_from} onChange={e => setPurgeForm(f => ({ ...f, date_from: e.target.value }))} style={inputStyle} />
              </FieldRow>
              <FieldRow label="Date To">
                <input type="date" value={purgeForm.date_to} onChange={e => setPurgeForm(f => ({ ...f, date_to: e.target.value }))} style={inputStyle} />
              </FieldRow>
              <FieldRow label="Channel (optional)">
                <input type="text" placeholder="e.g. amazon" value={purgeForm.channel} onChange={e => setPurgeForm(f => ({ ...f, channel: e.target.value }))} style={inputStyle} />
              </FieldRow>
              <div style={{ display: 'flex', gap: 8 }}>
                <button onClick={handlePurge} disabled={purging} className="btn btn-danger" style={{ fontSize: 13 }}>
                  {purging ? 'Purging…' : '🗑 Run Purge'}
                </button>
                <button onClick={() => setShowPurge(false)} className="btn btn-ghost" style={{ fontSize: 13 }}>Cancel</button>
              </div>
            </div>
          )}
        </div>

        <div style={{ marginTop: 20, paddingTop: 16, borderTop: '1px solid var(--border)' }}>
          <div style={{ fontWeight: 600, fontSize: 13, color: 'var(--text-primary)', marginBottom: 8 }}>Obfuscate Customer Data</div>
          <p style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 12 }}>
            Replace customer PII with anonymised placeholders. This cannot be undone.
          </p>
          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center', marginBottom: 12 }}>
            <input
              type="email"
              placeholder="customer@email.com (leave blank for all)"
              value={obfuscateEmail}
              onChange={e => setObfuscateEmail(e.target.value)}
              style={{ ...inputStyle, flex: 1, minWidth: 200 }}
            />
            <button onClick={handleObfuscateOne} disabled={obfuscating || !obfuscateEmail} className="btn btn-secondary" style={{ fontSize: 13 }}>
              {obfuscating ? 'Working…' : 'Obfuscate This Customer'}
            </button>
          </div>
          <button onClick={handleObfuscateAll} disabled={obfuscating} className="btn btn-danger" style={{ fontSize: 13 }}>
            {obfuscating ? 'Working…' : '⚠ Obfuscate ALL Customers'}
          </button>
        </div>
      </Section>

      {/* ── System Reset ── */}
      <Section title="System Reset" icon="⚠️">
        <p style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 16 }}>
          Permanently deletes all orders and import history for this tenant. Your account, billing, and user settings are retained.
        </p>
        {resetStep === 'idle' && (
          <button onClick={() => setResetStep('confirm')} className="btn btn-danger" style={{ fontSize: 13 }}>
            Reset System Data
          </button>
        )}
        {resetStep === 'confirm' && (
          <div style={{ padding: 16, background: 'rgba(239,68,68,0.08)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 8 }}>
            <p style={{ fontSize: 13, color: '#ef4444', fontWeight: 600, marginBottom: 12 }}>
              ⚠ This will permanently delete all order data. This cannot be undone.
            </p>
            <p style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 12 }}>
              Type <strong style={{ color: 'var(--text-primary)' }}>RESET</strong> below to confirm:
            </p>
            <input
              type="text"
              value={resetInput}
              onChange={e => setResetInput(e.target.value)}
              placeholder="Type RESET"
              style={{ ...inputStyle, marginBottom: 12 }}
            />
            <div style={{ display: 'flex', gap: 8 }}>
              <button
                onClick={handleSystemReset}
                disabled={resetting || resetInput.toUpperCase() !== 'RESET'}
                className="btn btn-danger"
                style={{ fontSize: 13 }}
              >
                {resetting ? 'Resetting…' : 'Confirm Reset'}
              </button>
              <button onClick={() => { setResetStep('idle'); setResetInput(''); }} className="btn btn-ghost" style={{ fontSize: 13 }}>Cancel</button>
            </div>
          </div>
        )}
        {resetStep === 'done' && (
          <div style={{ padding: 16, background: 'rgba(34,197,94,0.08)', border: '1px solid rgba(34,197,94,0.3)', borderRadius: 8, color: '#22c55e', fontWeight: 600, fontSize: 13 }}>
            ✓ System reset completed.
          </div>
        )}
      </Section>

      {/* Save button */}
      <div style={{ marginTop: 32, display: 'flex', justifyContent: 'flex-end' }}>
        <button onClick={save} disabled={saving} className="btn btn-primary" style={{ minWidth: 120 }}>
          {saving ? 'Saving…' : 'Save Settings'}
        </button>
      </div>
    </div>
  );
}

// ── Internal sub-components ───────────────────────────────────────────────────

function Section({ title, icon, children }: { title: string; icon: string; children: React.ReactNode }) {
  return (
    <div style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 10, padding: '20px 24px', marginBottom: 20 }}>
      <div style={{ fontWeight: 700, fontSize: 15, color: 'var(--text-primary)', marginBottom: 16, display: 'flex', alignItems: 'center', gap: 8 }}>
        <span>{icon}</span> {title}
      </div>
      {children}
    </div>
  );
}

function ToggleRow({ label, desc, checked, onChange, disabled }: {
  label: string; desc: string; checked: boolean; onChange: (v: boolean) => void; disabled?: boolean;
}) {
  return (
    <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 16 }}>
      <div>
        <div style={{ fontWeight: 600, fontSize: 13, color: 'var(--text-primary)' }}>{label}</div>
        <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>{desc}</div>
      </div>
      <label style={{ flexShrink: 0, cursor: disabled ? 'not-allowed' : 'pointer' }}>
        <input
          type="checkbox"
          checked={checked}
          onChange={e => !disabled && onChange(e.target.checked)}
          disabled={disabled}
          style={{ width: 36, height: 20, cursor: disabled ? 'not-allowed' : 'pointer' }}
        />
      </label>
    </div>
  );
}

function FieldRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 8 }}>
      <label style={{ width: 200, fontSize: 13, color: 'var(--text-secondary)', flexShrink: 0 }}>{label}</label>
      {children}
    </div>
  );
}

const inputStyle: React.CSSProperties = {
  background: 'var(--bg-elevated)',
  border: '1px solid var(--border)',
  borderRadius: 6,
  color: 'var(--text-primary)',
  fontSize: 13,
  padding: '6px 10px',
  outline: 'none',
};

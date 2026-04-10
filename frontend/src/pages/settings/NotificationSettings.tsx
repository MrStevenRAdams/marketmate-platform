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

type DeliveryMethod = 'in_app' | 'email' | 'both';
type NoLabelAction = 'auto_cancel' | 'block_label' | 'none';
type LabelPrintedNotification = 'onscreen' | 'message' | 'both';

interface NotifPref {
  enabled: boolean;
  delivery: DeliveryMethod;
  threshold?: number;
}

interface NotifConfig {
  low_stock: NotifPref;
  failed_order: NotifPref;
  po_overdue: NotifPref;
  new_rma: NotifPref;
  import_complete: NotifPref;
}

interface CancellationPolicy {
  no_label_action: NoLabelAction;
  label_printed_notification: LabelPrintedNotification;
}

const DEFAULT: NotifConfig = {
  low_stock:       { enabled: true,  delivery: 'both',   threshold: 5 },
  failed_order:    { enabled: true,  delivery: 'both' },
  po_overdue:      { enabled: true,  delivery: 'in_app' },
  new_rma:         { enabled: true,  delivery: 'in_app' },
  import_complete: { enabled: false, delivery: 'in_app' },
};

const DEFAULT_POLICY: CancellationPolicy = {
  no_label_action: 'block_label',
  label_printed_notification: 'both',
};

const LABELS: Record<keyof NotifConfig, { title: string; desc: string }> = {
  low_stock:       { title: 'Low Stock Alert',          desc: 'When any SKU drops below the threshold quantity' },
  failed_order:    { title: 'Failed Order Alert',       desc: 'When an order fails to import or process' },
  po_overdue:      { title: 'PO Delivery Overdue',      desc: 'When a purchase order is past its expected delivery date' },
  new_rma:         { title: 'New RMA Received',         desc: 'When a buyer submits a return or a marketplace return is synced' },
  import_complete: { title: 'Import Completed',         desc: 'When a bulk import job finishes (success or with errors)' },
};

type Toast = { msg: string; type: 'success' | 'error' } | null;

function DeliverySelect({ value, onChange }: { value: DeliveryMethod; onChange: (v: DeliveryMethod) => void }) {
  return (
    <select className="settings-select" style={{ width: 130 }} value={value} onChange={e => onChange(e.target.value as DeliveryMethod)}>
      <option value="in_app">In-app</option>
      <option value="email">Email</option>
      <option value="both">Both</option>
    </select>
  );
}

function RadioGroup<T extends string>({
  label, description, value, options, onChange,
}: {
  label: string;
  description: string;
  value: T;
  options: { value: T; label: string; desc: string }[];
  onChange: (v: T) => void;
}) {
  return (
    <div style={{ marginBottom: 24 }}>
      <div style={{ fontWeight: 600, fontSize: 14, color: 'var(--text-primary)', marginBottom: 4 }}>{label}</div>
      <div style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 14, lineHeight: 1.5 }}>{description}</div>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
        {options.map(opt => (
          <label
            key={opt.value}
            style={{
              display: 'flex', alignItems: 'flex-start', gap: 12,
              padding: '12px 16px',
              border: value === opt.value ? '1.5px solid var(--primary, #3b82f6)' : '1px solid var(--border)',
              borderRadius: 8,
              cursor: 'pointer',
              background: value === opt.value ? 'rgba(59,130,246,0.06)' : 'var(--bg-secondary)',
              transition: 'all 0.15s',
            }}
          >
            <input
              type="radio"
              name={label}
              value={opt.value}
              checked={value === opt.value}
              onChange={() => onChange(opt.value)}
              style={{ marginTop: 2, accentColor: 'var(--primary, #3b82f6)', flexShrink: 0 }}
            />
            <div>
              <div style={{ fontWeight: 500, fontSize: 13, color: 'var(--text-primary)' }}>{opt.label}</div>
              <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>{opt.desc}</div>
            </div>
          </label>
        ))}
      </div>
    </div>
  );
}

export default function NotificationSettings() {
  const [config, setConfig] = useState<NotifConfig>(DEFAULT);
  const [policy, setPolicy] = useState<CancellationPolicy>(DEFAULT_POLICY);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [toast, setToast] = useState<Toast>(null);

  useEffect(() => {
    api('/settings/notifications').then(r => r.json()).then(d => {
      if (d.notifications) setConfig({ ...DEFAULT, ...d.notifications });
      if (d.cancellation_policy) setPolicy({ ...DEFAULT_POLICY, ...d.cancellation_policy });
    }).catch(() => {}).finally(() => setLoading(false));
  }, []);

  const showToast = (msg: string, type: 'success' | 'error') => {
    setToast({ msg, type });
    setTimeout(() => setToast(null), 3000);
  };

  const save = async () => {
    setSaving(true);
    try {
      const r = await api('/settings/notifications', {
        method: 'PUT',
        body: JSON.stringify({ notifications: config, cancellation_policy: policy }),
      });
      if (!r.ok) throw new Error(await r.text());
      showToast('Notification preferences saved', 'success');
    } catch (e: any) {
      showToast(e.message || 'Save failed', 'error');
    } finally { setSaving(false); }
  };

  const update = (key: keyof NotifConfig, patch: Partial<NotifPref>) =>
    setConfig(c => ({ ...c, [key]: { ...c[key], ...patch } }));

  if (loading) return <div className="settings-page"><div style={{ color: 'var(--text-muted)', padding: 40 }}>Loading…</div></div>;

  return (
    <div className="settings-page">
      <div className="settings-breadcrumb">
        <Link to="/settings">Settings</Link>
        <span className="settings-breadcrumb-sep">›</span>
        <span className="settings-breadcrumb-current">Notification Preferences</span>
      </div>

      <h1 className="settings-page-title">Notification Preferences</h1>
      <p className="settings-page-sub">Choose which events trigger notifications and how they are delivered.</p>

      <div className="settings-section">
        <div className="settings-section-title">Alert Configuration</div>
        {(Object.keys(LABELS) as Array<keyof NotifConfig>).map((key) => {
          const pref = config[key];
          const meta = LABELS[key];
          return (
            <div key={key} className="settings-toggle-row" style={{ alignItems: 'flex-start', paddingTop: 14, paddingBottom: 14 }}>
              <div className="settings-toggle-info" style={{ flex: 1 }}>
                <span className="settings-toggle-label">{meta.title}</span>
                <span className="settings-toggle-desc">{meta.desc}</span>
                {key === 'low_stock' && pref.enabled && (
                  <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginTop: 8 }}>
                    <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>Trigger when stock falls below</span>
                    <input
                      type="number" min={1} className="settings-input" style={{ width: 72 }}
                      value={pref.threshold ?? 5}
                      onChange={e => update(key, { threshold: parseInt(e.target.value) || 1 })}
                    />
                    <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>units</span>
                  </div>
                )}
              </div>
              <div style={{ display: 'flex', alignItems: 'center', gap: 12, flexShrink: 0, marginTop: 2 }}>
                {pref.enabled && (
                  <DeliverySelect value={pref.delivery} onChange={(v) => update(key, { delivery: v })} />
                )}
                <label className="settings-toggle">
                  <input type="checkbox" checked={pref.enabled} onChange={e => update(key, { enabled: e.target.checked })} />
                  <span className="settings-toggle-slider" />
                </label>
              </div>
            </div>
          );
        })}
      </div>

      <div className="settings-section">
        <div className="settings-section-title">Order Cancellation Policy</div>
        <p style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 24, lineHeight: 1.6 }}>
          Define how MarketMate responds when a marketplace sends a buyer cancellation.
          These rules apply globally across all channels (eBay, Temu, Amazon, etc.).
        </p>

        <RadioGroup<NoLabelAction>
          label="If cancelled and no label has been printed"
          description="Choose the automatic action when a cancellation arrives before a shipping label has been generated."
          value={policy.no_label_action}
          onChange={v => setPolicy(p => ({ ...p, no_label_action: v }))}
          options={[
            {
              value: 'block_label',
              label: 'Block label from being printed',
              desc: 'The order is flagged and staff must acknowledge the cancellation before any label can be printed. Recommended.',
            },
            {
              value: 'auto_cancel',
              label: 'Auto-cancel the order',
              desc: 'The order is automatically cancelled and stock is restored immediately. No manual action required.',
            },
            {
              value: 'none',
              label: 'Take no action',
              desc: 'No automatic action. Staff will see the cancellation in Messages but the order remains unchanged.',
            },
          ]}
        />

        <RadioGroup<LabelPrintedNotification>
          label="If cancelled and a label has already been printed"
          description="When a cancellation arrives after a label has been generated, staff must acknowledge it. Choose how they are alerted."
          value={policy.label_printed_notification}
          onChange={v => setPolicy(p => ({ ...p, label_printed_notification: v }))}
          options={[
            {
              value: 'both',
              label: 'On-screen alert and message notification',
              desc: 'A critical red banner appears on the Orders page and a message ticket is created in Messages. Recommended.',
            },
            {
              value: 'onscreen',
              label: 'On-screen alert only',
              desc: 'A critical red banner appears on the Orders page only. No message ticket is created.',
            },
            {
              value: 'message',
              label: 'Message notification only',
              desc: 'A message ticket is created in the Messages page. No on-screen banner appears.',
            },
          ]}
        />
      </div>

      <div className="settings-btn-row">
        <button className="settings-btn-primary" onClick={save} disabled={saving}>
          {saving ? 'Saving…' : 'Save Preferences'}
        </button>
      </div>

      {toast && (
        <div className={`settings-toast ${toast.type}`}>
          {toast.type === 'success' ? '✓' : '✗'} {toast.msg}
        </div>
      )}
    </div>
  );
}

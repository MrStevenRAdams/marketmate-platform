import React, { useState, useEffect, useCallback } from 'react';
import { getActiveTenantId } from '../contexts/TenantContext';
import './CarrierSettings.css';

// ─── Types ────────────────────────────────────────────────────────────────────

interface CredentialField {
  key: string;
  label: string;
  placeholder: string;
  type: 'text' | 'password' | 'checkbox';
  required: boolean;
  help_text?: string;
}

interface CarrierMeta {
  id: string;
  name: string;
  display_name: string;
  country: string;
  logo: string;
  website: string;
  features: string[];
  is_active: boolean;
  is_configured: boolean;
  credential_fields: CredentialField[];
}

interface TestResult {
  carrierId: string;
  success: boolean;
  message: string;
}

// ─── Carrier icon map (emoji fallbacks — swap for real SVGs later) ────────────
const CARRIER_ICONS: Record<string, string> = {
  fedex:       '🟣',
  evri:        '🟢',
  'royal-mail':'🔴',
  dpd:         '🔵',
};

const FEATURE_LABELS: Record<string, string> = {
  rate_quotes:       'Rates',
  tracking:          'Tracking',
  international:     'International',
  signature:         'Signature',
  insurance:         'Insurance',
  saturday_delivery: 'Saturday',
  pickup:            'Pickup',
  customs:           'Customs',
  void:              'Void',
  manifest:          'Manifest',
  po_box:            'PO Box',
};

// ─── Component ────────────────────────────────────────────────────────────────

export default function CarrierSettings() {
  const API = (import.meta as any).env?.VITE_API_URL || 'http://localhost:8080/api/v1';
  const tenantId = getActiveTenantId() || localStorage.getItem('tenantId') || 'default-tenant';
  const headers = { 'X-Tenant-Id': tenantId, 'Content-Type': 'application/json' };

  const [carriers, setCarriers] = useState<CarrierMeta[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  // Setup modal
  const [setupCarrier, setSetupCarrier] = useState<CarrierMeta | null>(null);
  const [formValues, setFormValues] = useState<Record<string, string | boolean>>({});
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState('');
  const [saveSuccess, setSaveSuccess] = useState('');
  const [showSecrets, setShowSecrets] = useState<Record<string, boolean>>({});

  // Test results
  const [testResults, setTestResults] = useState<Record<string, TestResult>>({});
  const [testing, setTesting] = useState<string | null>(null);

  // Disconnect confirm
  const [disconnectTarget, setDisconnectTarget] = useState<CarrierMeta | null>(null);
  const [disconnecting, setDisconnecting] = useState(false);

  // ── Load carriers ──────────────────────────────────────────────────────────
  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const res = await fetch(`${API}/dispatch/carriers/configured`, { headers: { 'X-Tenant-Id': tenantId } });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = await res.json();
      setCarriers(data.carriers || []);
    } catch (e: any) {
      setError('Failed to load carriers. Is the backend running?');
    } finally {
      setLoading(false);
    }
  }, [API, tenantId]);

  useEffect(() => { load(); }, [load]);

  // ── Open setup modal ───────────────────────────────────────────────────────
  const openSetup = (carrier: CarrierMeta) => {
    const defaults: Record<string, string | boolean> = {};
    carrier.credential_fields.forEach(f => {
      defaults[f.key] = f.type === 'checkbox' ? false : '';
    });
    setFormValues(defaults);
    setSaveError('');
    setSaveSuccess('');
    setShowSecrets({});
    setSetupCarrier(carrier);
  };

  // ── Save credentials ────────────────────────────────────────────────────────
  const save = async () => {
    if (!setupCarrier) return;
    setSaving(true);
    setSaveError('');
    setSaveSuccess('');

    // Map form keys to API keys
    const payload: Record<string, any> = { carrier_id: setupCarrier.id };
    setupCarrier.credential_fields.forEach(f => {
      const val = formValues[f.key];
      if (f.key === 'api_key')    payload['api_key']    = val;
      else if (f.key === 'password')  payload['password']   = val;
      else if (f.key === 'username')  payload['username']   = val;
      else if (f.key === 'account_id') payload['account_id'] = val;
      else if (f.key === 'is_sandbox') payload['is_sandbox'] = val;
      else payload[f.key] = val;
    });

    try {
      const res = await fetch(`${API}/dispatch/carriers/${setupCarrier.id}/credentials`, {
        method: 'POST',
        headers,
        body: JSON.stringify(payload),
      });
      const data = await res.json();
      if (!res.ok) {
        setSaveError(data.detail || data.error || 'Failed to save credentials');
      } else {
        setSaveSuccess('Carrier connected and credentials verified ✓');
        await load();
        setTimeout(() => setSetupCarrier(null), 1500);
      }
    } catch {
      setSaveError('Network error — is the backend running?');
    } finally {
      setSaving(false);
    }
  };

  // ── Test connection ─────────────────────────────────────────────────────────
  const test = async (carrier: CarrierMeta) => {
    setTesting(carrier.id);
    try {
      const res = await fetch(`${API}/dispatch/carriers/${carrier.id}/test`, { method: 'POST', headers });
      const data = await res.json();
      setTestResults(prev => ({ ...prev, [carrier.id]: { carrierId: carrier.id, ...data } }));
    } catch {
      setTestResults(prev => ({ ...prev, [carrier.id]: { carrierId: carrier.id, success: false, message: 'Network error' } }));
    } finally {
      setTesting(null);
    }
  };

  // ── Disconnect ──────────────────────────────────────────────────────────────
  const disconnect = async () => {
    if (!disconnectTarget) return;
    setDisconnecting(true);
    try {
      await fetch(`${API}/dispatch/carriers/${disconnectTarget.id}/credentials`, { method: 'DELETE', headers });
      await load();
      setTestResults(prev => { const n = { ...prev }; delete n[disconnectTarget.id]; return n; });
    } finally {
      setDisconnecting(false);
      setDisconnectTarget(null);
    }
  };

  // ─── Render ─────────────────────────────────────────────────────────────────
  const configured = carriers.filter(c => c.is_configured);
  const available  = carriers.filter(c => !c.is_configured);

  return (
    <div className="cs-page">
      {/* ── Header ── */}
      <div className="cs-header">
        <div className="cs-header-left">
          <h1>Carrier Integrations</h1>
          <p>Connect shipping carriers to generate labels, get live rates and track shipments.</p>
        </div>
        <div className="cs-header-stats">
          <div className="cs-stat">
            <span className="cs-stat-num">{configured.length}</span>
            <span className="cs-stat-label">Connected</span>
          </div>
          <div className="cs-stat">
            <span className="cs-stat-num">{available.length}</span>
            <span className="cs-stat-label">Available</span>
          </div>
        </div>
      </div>

      {error && <div className="cs-banner cs-banner--error">{error}</div>}

      {loading ? (
        <div className="cs-loading">
          <div className="cs-spinner" />
          <span>Loading carriers…</span>
        </div>
      ) : (
        <>
          {/* ── Connected carriers ── */}
          {configured.length > 0 && (
            <section className="cs-section">
              <div className="cs-section-title">
                <span className="cs-section-dot cs-section-dot--green" />
                Connected Carriers
              </div>
              <div className="cs-grid">
                {configured.map(carrier => (
                  <CarrierCard
                    key={carrier.id}
                    carrier={carrier}
                    testResult={testResults[carrier.id]}
                    testing={testing === carrier.id}
                    onSetup={() => openSetup(carrier)}
                    onTest={() => test(carrier)}
                    onDisconnect={() => setDisconnectTarget(carrier)}
                  />
                ))}
              </div>
            </section>
          )}

          {/* ── Available carriers ── */}
          {available.length > 0 && (
            <section className="cs-section">
              <div className="cs-section-title">
                <span className="cs-section-dot cs-section-dot--grey" />
                Available Carriers
              </div>
              <div className="cs-grid">
                {available.map(carrier => (
                  <CarrierCard
                    key={carrier.id}
                    carrier={carrier}
                    testResult={undefined}
                    testing={false}
                    onSetup={() => openSetup(carrier)}
                    onTest={() => {}}
                    onDisconnect={() => {}}
                  />
                ))}
              </div>
            </section>
          )}
        </>
      )}

      {/* ══ Setup Modal ══════════════════════════════════════════════════════ */}
      {setupCarrier && (
        <div className="cs-overlay" onClick={(e) => e.target === e.currentTarget && setSetupCarrier(null)}>
          <div className="cs-modal">
            <div className="cs-modal-header">
              <div className="cs-modal-title">
                <span className="cs-modal-icon">{CARRIER_ICONS[setupCarrier.id] || '📦'}</span>
                <div>
                  <div className="cs-modal-name">{setupCarrier.display_name}</div>
                  <div className="cs-modal-sub">
                    {setupCarrier.is_configured ? 'Update credentials' : 'Connect carrier'}
                  </div>
                </div>
              </div>
              <button className="cs-modal-close" onClick={() => setSetupCarrier(null)}>✕</button>
            </div>

            <div className="cs-modal-body">
              {setupCarrier.credential_fields.map(field => (
                <div key={field.key} className="cs-field">
                  <label className="cs-label">
                    {field.label}
                    {field.required && <span className="cs-required">*</span>}
                  </label>

                  {field.type === 'checkbox' ? (
                    <label className="cs-toggle">
                      <input
                        type="checkbox"
                        checked={!!formValues[field.key]}
                        onChange={e => setFormValues(p => ({ ...p, [field.key]: e.target.checked }))}
                      />
                      <span className="cs-toggle-track">
                        <span className="cs-toggle-thumb" />
                      </span>
                      <span className="cs-toggle-label">{field.label}</span>
                    </label>
                  ) : (
                    <div className="cs-input-wrap">
                      <input
                        className="cs-input"
                        type={field.type === 'password' && !showSecrets[field.key] ? 'password' : 'text'}
                        placeholder={field.placeholder}
                        value={formValues[field.key] as string || ''}
                        onChange={e => setFormValues(p => ({ ...p, [field.key]: e.target.value }))}
                        autoComplete={field.type === 'password' ? 'new-password' : 'off'}
                      />
                      {field.type === 'password' && (
                        <button
                          className="cs-reveal"
                          type="button"
                          onClick={() => setShowSecrets(p => ({ ...p, [field.key]: !p[field.key] }))}
                        >
                          {showSecrets[field.key] ? '🙈' : '👁'}
                        </button>
                      )}
                    </div>
                  )}

                  {field.help_text && (
                    <div className="cs-help">{field.help_text}</div>
                  )}
                </div>
              ))}

              {saveError   && <div className="cs-banner cs-banner--error">{saveError}</div>}
              {saveSuccess && <div className="cs-banner cs-banner--success">{saveSuccess}</div>}
            </div>

            <div className="cs-modal-footer">
              <a
                href={setupCarrier.support_url || setupCarrier.website}
                target="_blank"
                rel="noreferrer"
                className="cs-doc-link"
              >
                📖 API docs
              </a>
              <div className="cs-modal-actions">
                <button className="cs-btn cs-btn--ghost" onClick={() => setSetupCarrier(null)}>
                  Cancel
                </button>
                <button className="cs-btn cs-btn--primary" onClick={save} disabled={saving}>
                  {saving ? <><span className="cs-spinner cs-spinner--sm" /> Verifying…</> : (
                    setupCarrier.is_configured ? 'Update Credentials' : 'Connect Carrier'
                  )}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* ══ Disconnect Confirm ════════════════════════════════════════════════ */}
      {disconnectTarget && (
        <div className="cs-overlay" onClick={(e) => e.target === e.currentTarget && setDisconnectTarget(null)}>
          <div className="cs-modal cs-modal--sm">
            <div className="cs-modal-header">
              <div className="cs-modal-title">
                <span>⚠️</span>
                <div>
                  <div className="cs-modal-name">Disconnect {disconnectTarget.display_name}?</div>
                  <div className="cs-modal-sub">This will remove all saved credentials</div>
                </div>
              </div>
            </div>
            <div className="cs-modal-body">
              <p className="cs-disconnect-warning">
                Existing shipments won't be affected, but you won't be able to create new labels
                or fetch rates until you reconnect.
              </p>
            </div>
            <div className="cs-modal-footer">
              <div className="cs-modal-actions">
                <button className="cs-btn cs-btn--ghost" onClick={() => setDisconnectTarget(null)}>
                  Cancel
                </button>
                <button className="cs-btn cs-btn--danger" onClick={disconnect} disabled={disconnecting}>
                  {disconnecting ? 'Disconnecting…' : 'Disconnect'}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

// ─── Carrier Card ─────────────────────────────────────────────────────────────

interface CardProps {
  carrier: CarrierMeta;
  testResult?: TestResult;
  testing: boolean;
  onSetup: () => void;
  onTest: () => void;
  onDisconnect: () => void;
}

function CarrierCard({ carrier, testResult, testing, onSetup, onTest, onDisconnect }: CardProps) {
  return (
    <div className={`cs-card ${carrier.is_configured ? 'cs-card--connected' : ''}`}>
      {/* Status pill */}
      {carrier.is_configured && (
        <div className="cs-card-status">
          {testResult ? (
            <span className={`cs-pill ${testResult.success ? 'cs-pill--green' : 'cs-pill--red'}`}>
              {testResult.success ? '✓ Live' : '✗ Error'}
            </span>
          ) : (
            <span className="cs-pill cs-pill--blue">Connected</span>
          )}
        </div>
      )}

      {/* Logo / icon area */}
      <div className="cs-card-icon">
        {CARRIER_ICONS[carrier.id] || '📦'}
      </div>

      {/* Info */}
      <div className="cs-card-info">
        <div className="cs-card-name">{carrier.display_name}</div>
        <div className="cs-card-country">{carrier.country === 'GB' ? '🇬🇧 United Kingdom' : carrier.country}</div>
      </div>

      {/* Features */}
      <div className="cs-card-features">
        {(carrier.features || []).slice(0, 5).map(f => (
          <span key={f} className="cs-feature-tag">{FEATURE_LABELS[f] || f}</span>
        ))}
        {carrier.features?.length > 5 && (
          <span className="cs-feature-tag cs-feature-tag--more">+{carrier.features.length - 5}</span>
        )}
      </div>

      {/* Test result message */}
      {testResult && (
        <div className={`cs-test-result ${testResult.success ? 'cs-test-result--ok' : 'cs-test-result--err'}`}>
          {testResult.message}
        </div>
      )}

      {/* Actions */}
      <div className="cs-card-actions">
        {carrier.is_configured ? (
          <>
            <button
              className="cs-btn cs-btn--ghost cs-btn--sm"
              onClick={onTest}
              disabled={testing}
            >
              {testing ? <><span className="cs-spinner cs-spinner--xs" /> Testing…</> : '⚡ Test'}
            </button>
            <button className="cs-btn cs-btn--ghost cs-btn--sm" onClick={onSetup}>
              ✎ Edit
            </button>
            <button className="cs-btn cs-btn--ghost cs-btn--sm cs-btn--danger-ghost" onClick={onDisconnect}>
              ✕ Remove
            </button>
          </>
        ) : (
          <button className="cs-btn cs-btn--primary cs-btn--full" onClick={onSetup}>
            + Connect
          </button>
        )}
      </div>
    </div>
  );
}

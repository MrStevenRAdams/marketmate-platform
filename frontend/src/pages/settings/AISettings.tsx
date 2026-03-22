import { useState, useEffect } from 'react';
import { getActiveTenantId } from '../../contexts/TenantContext';

const API_BASE = (import.meta as any).env?.VITE_API_URL || 'https://marketmate-api-487246736287.us-central1.run.app/api/v1';

const MODELS = [
  {
    id: 'gemini-2.0-flash',
    label: 'Gemini 2.0 Flash',
    provider: 'Google',
    cost: '~$0.005 / product',
    badge: 'DEFAULT',
    badgeColor: '#34d399',
    desc: 'Fast and cheap. Good for most products with clean data. Recommended for bulk runs.',
  },
  {
    id: 'gemini-1.5-pro',
    label: 'Gemini 1.5 Pro',
    provider: 'Google',
    cost: '~$0.020 / product',
    badge: 'BALANCED',
    badgeColor: '#60a5fa',
    desc: 'Better reasoning on complex or ambiguous data. Good for mid-tier catalogues.',
  },
  {
    id: 'claude-haiku-4-5-20251001',
    label: 'Claude Haiku',
    provider: 'Anthropic',
    cost: '~$0.010 / product',
    badge: 'ECONOMY',
    badgeColor: '#a78bfa',
    desc: 'Fast Claude model. Better structured output than Flash on complex schemas.',
  },
  {
    id: 'claude-sonnet-4-20250514',
    label: 'Claude Sonnet 4',
    provider: 'Anthropic',
    cost: '~$0.055 / product',
    badge: 'PREMIUM',
    badgeColor: '#f59e0b',
    desc: 'Highest quality reasoning and conflict resolution. Use for high-value or complex catalogues only.',
  },
];

interface AISettingsData {
  auto_draft_enabled: boolean;
  confidence_threshold: number;
  use_image_comparison: boolean;
  consolidation_model: string;
  auto_escalate: boolean;
  auto_draft_channels: string[];
}

const DEFAULTS: AISettingsData = {
  auto_draft_enabled: false,
  confidence_threshold: 0.70,
  use_image_comparison: false,
  consolidation_model: 'gemini-2.0-flash',
  auto_escalate: false,
  auto_draft_channels: [],
};

export default function AISettings() {
  const tenantID = getActiveTenantId();
  const [settings, setSettings] = useState<AISettingsData>(DEFAULTS);
  const [loading, setLoading]   = useState(true);
  const [saving, setSaving]     = useState(false);
  const [saved, setSaved]       = useState(false);
  const [error, setError]       = useState('');

  useEffect(() => {
    fetch(`${API_BASE}/ai/consolidate/settings`, {
      headers: { 'X-Tenant-Id': tenantID }
    })
      .then(r => r.ok ? r.json() : null)
      .then(d => {
        if (d) setSettings({ ...DEFAULTS, ...d });
      })
      .catch(() => {})
      .finally(() => setLoading(false));
  }, [tenantID]);

  const save = async () => {
    setSaving(true);
    setError('');
    try {
      const r = await fetch(`${API_BASE}/ai/consolidate/settings`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': tenantID },
        body: JSON.stringify({ ai: settings }),
      });
      if (!r.ok) throw new Error(await r.text());
      setSaved(true);
      setTimeout(() => setSaved(false), 2500);
    } catch (e: any) {
      setError(e.message);
    } finally {
      setSaving(false);
    }
  };

  const selectedModel = MODELS.find(m => m.id === settings.consolidation_model) || MODELS[0];
  const estimatedBulkCost = (n: number) => {
    const base = parseFloat(selectedModel.cost.replace('~$', '').replace(' / product', ''));
    const escalateMult = settings.auto_escalate ? 1.2 : 1; // ~20% hit escalation
    return `~$${(n * base * escalateMult).toFixed(2)}`;
  };

  if (loading) return (
    <div style={{ padding: 40, color: 'var(--text-muted)', textAlign: 'center' }}>Loading…</div>
  );

  return (
    <div style={{ padding: 24, maxWidth: 720, margin: '0 auto' }}>
      <div style={{ marginBottom: 28 }}>
        <h1 style={{ fontSize: 22, fontWeight: 800, color: 'var(--text-primary)', margin: 0 }}>AI Settings</h1>
        <p style={{ fontSize: 13, color: 'var(--text-muted)', margin: '6px 0 0' }}>
          Controls model selection and costs for product consolidation and listing generation.
        </p>
      </div>

      {/* ── Model Selection ── */}
      <section style={{ marginBottom: 28 }}>
        <div style={{ fontSize: 12, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.5px', marginBottom: 12 }}>
          Consolidation Model
        </div>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
          {MODELS.map(m => {
            const selected = settings.consolidation_model === m.id;
            return (
              <div key={m.id}
                onClick={() => setSettings(s => ({ ...s, consolidation_model: m.id }))}
                style={{
                  padding: '12px 14px', borderRadius: 10, cursor: 'pointer',
                  border: `2px solid ${selected ? m.badgeColor : 'var(--border)'}`,
                  background: selected ? `${m.badgeColor}0d` : 'var(--bg-secondary)',
                  display: 'flex', alignItems: 'center', gap: 14,
                  transition: 'border-color 0.15s',
                }}>
                <div style={{
                  width: 18, height: 18, borderRadius: '50%', flexShrink: 0,
                  border: `2px solid ${selected ? m.badgeColor : 'var(--border)'}`,
                  background: selected ? m.badgeColor : 'transparent',
                  display: 'flex', alignItems: 'center', justifyContent: 'center',
                }}>
                  {selected && <div style={{ width: 6, height: 6, borderRadius: '50%', background: '#fff' }} />}
                </div>
                <div style={{ flex: 1 }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 2 }}>
                    <span style={{ fontWeight: 700, fontSize: 14, color: 'var(--text-primary)' }}>{m.label}</span>
                    <span style={{ fontSize: 9, fontWeight: 800, padding: '2px 6px', borderRadius: 4, background: `${m.badgeColor}22`, color: m.badgeColor, letterSpacing: '0.5px' }}>
                      {m.badge}
                    </span>
                    <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>{m.provider}</span>
                  </div>
                  <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>{m.desc}</div>
                </div>
                <div style={{ fontSize: 13, fontWeight: 700, color: selected ? m.badgeColor : 'var(--text-muted)', whiteSpace: 'nowrap', textAlign: 'right' }}>
                  {m.cost}
                </div>
              </div>
            );
          })}
        </div>

        {/* Cost estimator */}
        <div style={{ marginTop: 12, padding: '10px 14px', background: 'var(--bg-secondary)', borderRadius: 8, border: '1px solid var(--border)', fontSize: 12, color: 'var(--text-muted)' }}>
          💰 Estimated cost for{' '}
          {[100, 500, 1000, 5000].map(n => (
            <span key={n} style={{ color: 'var(--text-primary)', fontWeight: 700, marginRight: 4 }}>
              {n.toLocaleString()} products: {estimatedBulkCost(n)}
            </span>
          ))}
        </div>
      </section>

      {/* ── Auto Escalation ── */}
      <section style={{ marginBottom: 28 }}>
        <div style={{ fontSize: 12, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.5px', marginBottom: 12 }}>
          Auto Escalation
        </div>
        <label style={{ display: 'flex', alignItems: 'flex-start', gap: 12, cursor: 'pointer', padding: '12px 14px', background: 'var(--bg-secondary)', borderRadius: 10, border: '1px solid var(--border)' }}>
          <input type="checkbox"
            checked={settings.auto_escalate}
            onChange={e => setSettings(s => ({ ...s, auto_escalate: e.target.checked }))}
            style={{ marginTop: 2, width: 16, height: 16, accentColor: '#f59e0b', cursor: 'pointer' }}
          />
          <div>
            <div style={{ fontWeight: 600, fontSize: 14, color: 'var(--text-primary)', marginBottom: 3 }}>
              Escalate to Claude Sonnet when confidence is low
            </div>
            <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>
              If the selected model returns an overall confidence below your threshold, the consolidation
              is automatically retried with Claude Sonnet 4. Adds ~$0.05 per escalated product.
              Only effective when a Gemini model is selected above.
            </div>
          </div>
        </label>
      </section>

      {/* ── Confidence Threshold ── */}
      <section style={{ marginBottom: 28 }}>
        <div style={{ fontSize: 12, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.5px', marginBottom: 12 }}>
          Confidence Threshold
        </div>
        <div style={{ padding: '12px 14px', background: 'var(--bg-secondary)', borderRadius: 10, border: '1px solid var(--border)' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 8 }}>
            <span style={{ fontSize: 13, color: 'var(--text-primary)' }}>Minimum confidence before flagging for review</span>
            <span style={{ fontSize: 16, fontWeight: 800, color: 'var(--primary)', fontVariantNumeric: 'tabular-nums' }}>
              {Math.round(settings.confidence_threshold * 100)}%
            </span>
          </div>
          <input type="range" min={0.5} max={0.95} step={0.05}
            value={settings.confidence_threshold}
            onChange={e => setSettings(s => ({ ...s, confidence_threshold: parseFloat(e.target.value) }))}
            style={{ width: '100%', accentColor: 'var(--primary)' }}
          />
          <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 10, color: 'var(--text-muted)', marginTop: 4 }}>
            <span>50% — more auto-approvals</span>
            <span>95% — more manual reviews</span>
          </div>
          <div style={{ marginTop: 8, fontSize: 12, color: 'var(--text-muted)' }}>
            Products below this threshold get a <strong>review_required</strong> flag and appear in the Review queue. 
            PIM writeback is skipped for flagged products.
          </div>
        </div>
      </section>

      {/* ── Image Comparison ── */}
      <section style={{ marginBottom: 28 }}>
        <div style={{ fontSize: 12, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.5px', marginBottom: 12 }}>
          Image Comparison
        </div>
        <label style={{ display: 'flex', alignItems: 'flex-start', gap: 12, cursor: 'pointer', padding: '12px 14px', background: 'var(--bg-secondary)', borderRadius: 10, border: '1px solid var(--border)' }}>
          <input type="checkbox"
            checked={settings.use_image_comparison}
            onChange={e => setSettings(s => ({ ...s, use_image_comparison: e.target.checked }))}
            style={{ marginTop: 2, width: 16, height: 16, accentColor: 'var(--primary)', cursor: 'pointer' }}
          />
          <div>
            <div style={{ fontWeight: 600, fontSize: 14, color: 'var(--text-primary)', marginBottom: 3 }}>
              Use image comparison in identity verification
            </div>
            <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>
              Sends product image URLs to the AI to verify branches are the same physical product.
              Adds vision tokens — roughly doubles cost per product. Only activates when title or 
              category signals are uncertain (score 0.4–0.8). Clear matches skip image comparison.
            </div>
          </div>
        </label>
      </section>

      {/* Save / error */}
      {error && (
        <div style={{ padding: 12, background: '#f8717111', border: '1px solid #f8717155', borderRadius: 8, color: '#f87171', fontSize: 13, marginBottom: 16 }}>
          ❌ {error}
        </div>
      )}

      <button onClick={save} disabled={saving} style={{
        padding: '10px 28px', borderRadius: 8, fontSize: 14, fontWeight: 700, cursor: 'pointer',
        border: 'none', background: saved ? '#34d399' : 'var(--primary)', color: '#fff',
        transition: 'background 0.2s',
      }}>
        {saving ? 'Saving…' : saved ? '✓ Saved' : 'Save Settings'}
      </button>
    </div>
  );
}

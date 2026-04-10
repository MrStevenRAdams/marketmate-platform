import React, { useState, useEffect } from 'react';
import './BillingSettings.css';
import { UsageDashboard } from '../components/billing/UsageDashboard';

const API = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';

// ── Types ────────────────────────────────────────────────────────────────────

interface Plan {
  plan_id: string;
  name: string;
  price_gbp: number;
  credits_per_month: number | null;
  billing_model: 'credits' | 'per_order' | 'gmv_percent';
  per_order_gbp: number | null;
  gmv_percent: number | null;
}

interface Ledger {
  period: string;
  plan_id: string;
  credits_allocated: number | null;
  credits_used: number;
  credits_remaining: number | null;
  orders_processed: number;
  api_calls_total: number;
  labels_generated: number;
  listings_published: number;
  gmv_total_gbp: number;
  status: 'active' | 'quota_exceeded' | 'closed' | 'billed';
  bill_amount_gbp: number | null;
  breakdown: {
    ai_tokens: number;
    api_calls: number;
    order_syncs: number;
    listing_publish: number;
    shipment_labels: number;
    data_exports: number;
  };
}

interface Billing {
  paypal_subscription_id?: string;
  billing_email?: string;
  billing_name?: string;
  next_billing_at?: string;
  last_payment_at?: string;
  last_payment_amount_gbp?: number;
}

interface AuditEntry {
  event_id: string;
  type: string;
  sub_type: string;
  quantity: number;
  credits_charged: number;
  balance_before: number;
  balance_after: number;
  marketplace?: string;
  occurred_at: string;
}

interface BillingStatus {
  tenant: { plan_id: string; plan_status: string; trial_ends_at?: string };
  plan: Plan | null;
  plan_override: { monthly_base_gbp?: number; per_order_gbp?: number; gmv_percent?: number; notes?: string } | null;
  ledger: Ledger | null;
  billing: Billing | null;
}

// ── Helpers ──────────────────────────────────────────────────────────────────

function fmtCredits(n: number) {
  if (n >= 1000) return `${(n / 1000).toFixed(1)}k`;
  return n.toFixed(1);
}

function fmtGBP(n: number) {
  return `£${n.toFixed(2)}`;
}

function pct(used: number, total: number | null) {
  if (!total) return 0;
  return Math.min(100, Math.round((used / total) * 100));
}

const TYPE_ICONS: Record<string, string> = {
  ai_tokens:      '🤖',
  api_call:       '🔗',
  order_sync:     '📦',
  listing_publish: '🏪',
  shipment_label: '🚚',
  data_export:    '📥',
};

function timeAgo(iso: string) {
  const diff = Date.now() - new Date(iso).getTime();
  const mins = Math.floor(diff / 60000);
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  return new Date(iso).toLocaleDateString('en-GB', { day: 'numeric', month: 'short' });
}

// ── Main Component ────────────────────────────────────────────────────────────

const BillingSettings: React.FC = () => {
  const [status, setStatus]   = useState<BillingStatus | null>(null);
  const [audit, setAudit]     = useState<AuditEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [auditLoading, setAuditLoading] = useState(false);
  const [error, setError]     = useState('');
  const [tab, setTab]         = useState<'overview' | 'usage' | 'audit' | 'seo_usage'>('overview');
  const [subscribing, setSubscribing] = useState(false);
  const [portalLoading, setPortalLoading] = useState(false);
  const [portalError, setPortalError] = useState('');

  const tenantId = localStorage.getItem('marketmate_active_tenant') ?? '';
  const headers = { 'Content-Type': 'application/json', 'X-Tenant-Id': tenantId };

  // ── Load ────────────────────────────────────────────────────────────────────

  useEffect(() => {
    setLoading(true);
    fetch(`${API}/billing/status`, { headers })
      .then(r => r.json())
      .then(setStatus)
      .catch(() => setError('Failed to load billing data'))
      .finally(() => setLoading(false));
  }, [tenantId]);

  useEffect(() => {
    if (tab !== 'audit' || audit.length > 0) return;
    setAuditLoading(true);
    fetch(`${API}/billing/audit?limit=50`, { headers })
      .then(r => r.json())
      .then(d => setAudit(d.entries ?? []))
      .finally(() => setAuditLoading(false));
  }, [tab]);

  // ── Subscribe to PayPal ─────────────────────────────────────────────────────

  const handleSubscribe = async (planId: string) => {
    setSubscribing(true);
    try {
      const res = await fetch(`${API}/billing/paypal/subscribe`, {
        method: 'POST',
        headers,
        body: JSON.stringify({ plan_id: planId }),
      });
      const data = await res.json();
      if (data.approval_url) {
        window.location.href = data.approval_url;
      } else {
        alert(data.error ?? 'Failed to start subscription');
      }
    } finally {
      setSubscribing(false);
    }
  };

  const handleManageSubscription = async () => {
    setPortalLoading(true);
    setPortalError('');
    try {
      const res = await fetch(`${API}/billing/stripe/portal`, {
        method: 'POST',
        headers,
        body: JSON.stringify({ return_url: window.location.href }),
      });
      const data = await res.json();
      if (data.portal_url) {
        window.location.href = data.portal_url;
      } else {
        setPortalError(data.error ?? 'Unable to open billing portal. Please try again.');
      }
    } catch {
      setPortalError('Unable to open billing portal. Please try again.');
    } finally {
      setPortalLoading(false);
    }
  };

  if (loading) return <div className="bs-page"><div className="bs-loading">Loading billing…</div></div>;
  if (error)   return <div className="bs-page"><div className="bs-error">{error}</div></div>;
  if (!status) return null;

  const { plan, plan_override, ledger, billing, tenant } = status;
  const isTrialing   = tenant.plan_status === 'trialing';
  const isActive     = tenant.plan_status === 'active';
  const isStarter    = tenant.plan_id?.startsWith('starter');
  const quotaExceeded = ledger?.status === 'quota_exceeded';
  const creditsPct   = ledger ? pct(ledger.credits_used, ledger.credits_allocated) : 0;

  // Effective pricing (override takes precedence)
  const effectiveBase      = plan_override?.monthly_base_gbp ?? plan?.price_gbp ?? 0;
  const effectivePerOrder  = plan_override?.per_order_gbp   ?? plan?.per_order_gbp ?? null;
  const effectiveGMVPct    = plan_override?.gmv_percent      ?? plan?.gmv_percent ?? null;

  return (
    <div className="bs-page">
      {/* Header */}
      <div className="bs-header">
        <div>
          <h1 className="bs-title">Billing</h1>
          <p className="bs-subtitle">
            {plan?.name ?? tenant.plan_id} plan
            {isTrialing && tenant.trial_ends_at && (
              <span className="bs-trial-badge">
                Trial ends {new Date(tenant.trial_ends_at).toLocaleDateString('en-GB', { day: 'numeric', month: 'short' })}
              </span>
            )}
            {isActive && billing?.next_billing_at && (
              <span className="bs-meta"> · Next payment {new Date(billing.next_billing_at).toLocaleDateString('en-GB', { day: 'numeric', month: 'long' })}</span>
            )}
          </p>
        </div>
        {isTrialing && (
          <button className="bs-btn-primary" onClick={() => setTab('overview')}>
            Upgrade plan
          </button>
        )}
        {isActive && (
          <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-end', gap: 4 }}>
            <button
              className="bs-btn-outline"
              onClick={handleManageSubscription}
              disabled={portalLoading}
            >
              {portalLoading ? 'Opening…' : 'Manage Subscription'}
            </button>
            {portalError && (
              <div style={{ fontSize: 12, color: '#ef4444' }}>{portalError}</div>
            )}
          </div>
        )}
      </div>

      {/* Quota exceeded banner */}
      {quotaExceeded && (
        <div className="bs-banner bs-banner--danger">
          <strong>⚠ Credit quota reached</strong> — AI generation and marketplace sync have been paused.
          Upgrade your plan or wait for the next billing cycle to resume.
          <button className="bs-btn-sm" onClick={() => setTab('overview')}>Upgrade now</button>
        </div>
      )}

      {/* 90% warning */}
      {!quotaExceeded && creditsPct >= 90 && isStarter && (
        <div className="bs-banner bs-banner--warning">
          <strong>⚡ {creditsPct}% of credits used</strong> — you're running low this month.
          <button className="bs-btn-sm" onClick={() => setTab('overview')}>Upgrade</button>
        </div>
      )}

      {/* Tabs */}
      <div className="bs-tabs">
        {(['overview', 'usage', 'audit', 'seo_usage'] as const).map(t => (
          <button
            key={t}
            className={`bs-tab ${tab === t ? 'active' : ''}`}
            onClick={() => setTab(t)}
          >
            {{ overview: 'Overview', usage: 'Usage breakdown', audit: 'Activity log', seo_usage: 'SEO & Keywords' }[t]}
          </button>
        ))}
      </div>

      {/* ── OVERVIEW TAB ─────────────────────────────────────────────────── */}
      {tab === 'overview' && (
        <div className="bs-content">

          {/* Credit meter (starter plans) */}
          {isStarter && ledger && (
            <div className="bs-card">
              <div className="bs-card-header">
                <div className="bs-card-title">Credits this month</div>
                <div className="bs-card-meta">{ledger.period}</div>
              </div>

              <div className="bs-credit-meter">
                <div className="bs-credit-numbers">
                  <span className="bs-credit-used">{fmtCredits(ledger.credits_used)}</span>
                  <span className="bs-credit-total">/ {ledger.credits_allocated ? fmtCredits(ledger.credits_allocated) : '∞'} credits</span>
                </div>
                <div className="bs-meter-bar">
                  <div
                    className={`bs-meter-fill ${creditsPct >= 90 ? 'bs-meter-fill--warn' : ''} ${quotaExceeded ? 'bs-meter-fill--danger' : ''}`}
                    style={{ width: `${creditsPct}%` }}
                  />
                </div>
                <div className="bs-credit-remaining">
                  {quotaExceeded
                    ? 'Quota exceeded — AI & sync paused'
                    : `${ledger.credits_remaining ? fmtCredits(ledger.credits_remaining) : '∞'} credits remaining`
                  }
                </div>
              </div>

              {/* Mini breakdown */}
              <div className="bs-breakdown-mini">
                {[
                  { label: 'AI tokens',   val: ledger.breakdown.ai_tokens,       icon: '🤖' },
                  { label: 'API calls',   val: ledger.breakdown.api_calls,        icon: '🔗' },
                  { label: 'Orders',      val: ledger.breakdown.order_syncs,      icon: '📦' },
                  { label: 'Listings',    val: ledger.breakdown.listing_publish,  icon: '🏪' },
                  { label: 'Labels',      val: ledger.breakdown.shipment_labels,  icon: '🚚' },
                ].map(({ label, val, icon }) => (
                  <div key={label} className="bs-breakdown-item">
                    <span className="bs-bd-icon">{icon}</span>
                    <span className="bs-bd-label">{label}</span>
                    <span className="bs-bd-val">{fmtCredits(val)}</span>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Premium / Enterprise usage stats */}
          {!isStarter && ledger && (
            <div className="bs-stats-grid">
              <div className="bs-stat">
                <div className="bs-stat-val">{ledger.orders_processed.toLocaleString()}</div>
                <div className="bs-stat-label">Orders processed</div>
              </div>
              <div className="bs-stat">
                <div className="bs-stat-val">{fmtGBP(ledger.gmv_total_gbp)}</div>
                <div className="bs-stat-label">GMV this month</div>
              </div>
              <div className="bs-stat">
                <div className="bs-stat-val">{ledger.api_calls_total.toLocaleString()}</div>
                <div className="bs-stat-label">API calls</div>
              </div>
              <div className="bs-stat">
                <div className="bs-stat-val">{ledger.labels_generated.toLocaleString()}</div>
                <div className="bs-stat-label">Labels generated</div>
              </div>
            </div>
          )}

          {/* Current plan + pricing */}
          <div className="bs-card">
            <div className="bs-card-header">
              <div className="bs-card-title">Current plan</div>
              {isActive && billing?.paypal_subscription_id && (
                <span className="bs-active-badge">Active</span>
              )}
              {isTrialing && (
                <span className="bs-trial-pill">Trial</span>
              )}
            </div>

            <div className="bs-plan-detail">
              <div className="bs-plan-name">{plan?.name ?? tenant.plan_id}</div>
              <div className="bs-plan-pricing">
                {plan?.billing_model === 'credits' && (
                  <>
                    <strong>{fmtGBP(effectiveBase)}/month</strong>
                    &nbsp;· {plan.credits_per_month?.toLocaleString()} credits included
                    {plan_override?.notes && <div className="bs-override-note">📝 {plan_override.notes}</div>}
                  </>
                )}
                {plan?.billing_model === 'per_order' && (
                  <>
                    <strong>{fmtGBP(effectiveBase)}/month</strong> base
                    &nbsp;+ <strong>{effectivePerOrder ? `£${effectivePerOrder.toFixed(2)}` : '£0.10'}</strong> per order
                    {ledger && <div className="bs-plan-estimate">
                      Estimated this month: <strong>{fmtGBP(effectiveBase + (ledger.orders_processed * (effectivePerOrder ?? 0.10)))}</strong>
                    </div>}
                    {plan_override?.notes && <div className="bs-override-note">📝 {plan_override.notes}</div>}
                  </>
                )}
                {plan?.billing_model === 'gmv_percent' && (
                  <>
                    <strong>{fmtGBP(effectiveBase)}/month</strong> base
                    &nbsp;+ <strong>{effectiveGMVPct ?? 1}%</strong> of GMV
                    {ledger && <div className="bs-plan-estimate">
                      Estimated this month: <strong>{fmtGBP(effectiveBase + (ledger.gmv_total_gbp * ((effectiveGMVPct ?? 1) / 100)))}</strong>
                    </div>}
                    {plan_override?.notes && <div className="bs-override-note">📝 {plan_override.notes}</div>}
                  </>
                )}
              </div>
            </div>

            {billing?.last_payment_at && (
              <div className="bs-billing-info">
                Last payment: <strong>{billing.last_payment_amount_gbp ? fmtGBP(billing.last_payment_amount_gbp) : '—'}</strong>
                {' '}on {new Date(billing.last_payment_at).toLocaleDateString('en-GB', { day: 'numeric', month: 'long', year: 'numeric' })}
                {billing.billing_email && <> · {billing.billing_email}</>}
              </div>
            )}
          </div>

          {/* Upgrade options (shown during trial or for starter plans) */}
          {(isTrialing || isStarter) && (
            <div className="bs-card">
              <div className="bs-card-header">
                <div className="bs-card-title">
                  {isTrialing ? 'Choose a plan to continue after your trial' : 'Upgrade your plan'}
                </div>
              </div>
              <div className="bs-plan-grid">
                {[
                  { id: 'starter_s', name: 'Starter S', price: 29, credits: 10000,  desc: 'Solo sellers' },
                  { id: 'starter_m', name: 'Starter M', price: 79, credits: 50000,  desc: 'Growing businesses' },
                  { id: 'starter_l', name: 'Starter L', price: 149, credits: 150000, desc: 'High-volume' },
                ].map(p => (
                  <div
                    key={p.id}
                    className={`bs-plan-option ${tenant.plan_id === p.id && isActive ? 'bs-plan-option--current' : ''}`}
                  >
                    <div className="bs-po-name">{p.name}</div>
                    <div className="bs-po-price">£{p.price}<span>/mo</span></div>
                    <div className="bs-po-credits">{p.credits.toLocaleString()} credits</div>
                    <div className="bs-po-desc">{p.desc}</div>
                    {tenant.plan_id === p.id && isActive ? (
                      <div className="bs-po-current">Current plan</div>
                    ) : (
                      <button
                        className="bs-btn-outline"
                        onClick={() => handleSubscribe(p.id)}
                        disabled={subscribing}
                      >
                        {subscribing ? '...' : 'Select'}
                      </button>
                    )}
                  </div>
                ))}
              </div>
              <div className="bs-enterprise-cta">
                Need more? <strong>Premium</strong> (£250/mo + 10p/order) or <strong>Enterprise</strong> (£499/mo + 1% GMV) —{' '}
                <a href="mailto:sales@marketmate.com" className="bs-link">contact sales</a> for a custom quote.
              </div>
            </div>
          )}
        </div>
      )}

      {/* ── USAGE TAB ─────────────────────────────────────────────────────── */}
      {tab === 'usage' && ledger && (
        <div className="bs-content">
          <div className="bs-card">
            <div className="bs-card-header">
              <div className="bs-card-title">Credit breakdown — {ledger.period}</div>
              {ledger.credits_allocated && (
                <div className="bs-card-meta">{creditsPct}% used</div>
              )}
            </div>

            <div className="bs-breakdown-bars">
              {[
                { label: 'AI token generation', val: ledger.breakdown.ai_tokens,      icon: '🤖', color: '#8b5cf6' },
                { label: 'Marketplace API calls', val: ledger.breakdown.api_calls,    icon: '🔗', color: '#06b6d4' },
                { label: 'Order imports',        val: ledger.breakdown.order_syncs,   icon: '📦', color: '#3b82f6' },
                { label: 'Listing publishes',    val: ledger.breakdown.listing_publish, icon: '🏪', color: '#10b981' },
                { label: 'Shipment labels',      val: ledger.breakdown.shipment_labels, icon: '🚚', color: '#f59e0b' },
                { label: 'Data exports',         val: ledger.breakdown.data_exports,  icon: '📥', color: '#f97316' },
              ].map(({ label, val, icon, color }) => {
                const barPct = ledger.credits_used > 0 ? Math.round((val / ledger.credits_used) * 100) : 0;
                return (
                  <div key={label} className="bs-bar-row">
                    <div className="bs-bar-label">
                      <span>{icon}</span>
                      <span>{label}</span>
                    </div>
                    <div className="bs-bar-track">
                      <div className="bs-bar-fill" style={{ width: `${barPct}%`, background: color }} />
                    </div>
                    <div className="bs-bar-val">{fmtCredits(val)} cr</div>
                    <div className="bs-bar-pct">{barPct}%</div>
                  </div>
                );
              })}
            </div>

            <div className="bs-usage-totals">
              <div className="bs-usage-total-row">
                <span>Total credits used</span>
                <strong>{fmtCredits(ledger.credits_used)}</strong>
              </div>
              {ledger.credits_allocated && (
                <div className="bs-usage-total-row">
                  <span>Credits allocated</span>
                  <strong>{fmtCredits(ledger.credits_allocated)}</strong>
                </div>
              )}
              <div className="bs-usage-total-row">
                <span>Orders processed</span>
                <strong>{ledger.orders_processed.toLocaleString()}</strong>
              </div>
              <div className="bs-usage-total-row">
                <span>API calls made</span>
                <strong>{ledger.api_calls_total.toLocaleString()}</strong>
              </div>
              {ledger.gmv_total_gbp > 0 && (
                <div className="bs-usage-total-row">
                  <span>GMV processed</span>
                  <strong>{fmtGBP(ledger.gmv_total_gbp)}</strong>
                </div>
              )}
            </div>
          </div>
        </div>
      )}

      {/* ── AUDIT LOG TAB ─────────────────────────────────────────────────── */}
      {/* ── SEO & KEYWORDS TAB (Session 11) ─────────────────────────── */}
      {tab === 'seo_usage' && (
        <div className="bs-content">
          <UsageDashboard />
        </div>
      )}

      {tab === 'audit' && (
        <div className="bs-content">
          <div className="bs-card">
            <div className="bs-card-header">
              <div className="bs-card-title">Activity log</div>
              <div className="bs-card-meta">Last 50 events · immutable record</div>
            </div>

            {auditLoading ? (
              <div className="bs-loading" style={{ padding: 24 }}>Loading…</div>
            ) : audit.length === 0 ? (
              <div className="bs-empty">No usage events recorded yet this period.</div>
            ) : (
              <div className="bs-audit-list">
                <div className="bs-audit-header">
                  <span>Event</span>
                  <span>Qty</span>
                  <span>Credits</span>
                  <span>Balance after</span>
                  <span>When</span>
                </div>
                {audit.map(e => (
                  <div key={e.event_id} className="bs-audit-row">
                    <div className="bs-audit-event">
                      <span className="bs-audit-icon">{TYPE_ICONS[e.type] ?? '•'}</span>
                      <div>
                        <div className="bs-audit-type">{e.sub_type.replace(/_/g, ' ')}</div>
                        {e.marketplace && <div className="bs-audit-meta">{e.marketplace}</div>}
                      </div>
                    </div>
                    <div className="bs-audit-qty">{e.quantity}</div>
                    <div className="bs-audit-credits">−{e.credits_charged.toFixed(2)}</div>
                    <div className="bs-audit-balance">{e.balance_after.toFixed(1)}</div>
                    <div className="bs-audit-time">{timeAgo(e.occurred_at)}</div>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
};

export default BillingSettings;

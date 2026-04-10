// ============================================================================
// CHANNEL COMMAND CENTRE
// ============================================================================
// Post-wizard landing page for channel-referred users (Temu, etc.).
// Shows listing stats, recent orders, source channel summary, feature
// discovery nudges, and subscription/credits panel.
//
// Props:
//   channelId      — channel key e.g. "temu"
//   channelName    — display name e.g. "Temu"
//   channelColor   — accent colour e.g. "#F97316"
//   channelIcon    — emoji icon e.g. "🛍️"
//   sourceChannel  — where products were imported from e.g. "amazon"
// ============================================================================

import { useState, useEffect, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { apiFetch } from '../services/apiFetch';

interface Props {
  channelId?: string;
  channelName?: string;
  channelColor?: string;
  channelIcon?: string;
  sourceChannel?: string;
}

interface ListingStats {
  draft: number;
  live: number;
  error: number;
  submitted: number;
  total: number;
}

interface RecentOrder {
  order_id: string;
  channel: string;
  customer_name: string;
  total: number;
  currency: string;
  status: string;
  imported_at: string;
}

interface CreditInfo {
  free_remaining: number;
  free_limit: number;
  monthly_remaining: number;
  monthly_limit: number;
  purchased_remaining: number;
  total_available: number;
  subscription_plan: string;
  recommended_plan: string;
  estimated_monthly_orders: number;
}

const PLAN_DISPLAY: Record<string, { name: string; orders: string; credits: string; price: string }> = {
  starter_s:  { name: 'Starter S',  orders: '0–100',     credits: '50/mo',       price: '£29/mo' },
  starter_m:  { name: 'Starter M',  orders: '101–500',   credits: '200/mo',      price: '£59/mo' },
  starter_l:  { name: 'Starter L',  orders: '501–1,500', credits: '500/mo',      price: '£99/mo' },
  premium:    { name: 'Premium',    orders: '1,501–5,000', credits: '1,000/mo',  price: '£199/mo' },
  enterprise: { name: 'Enterprise', orders: '5,001+',    credits: 'Unlimited',   price: 'Custom' },
};

const CREDIT_PACKS = [
  { id: 'small',  credits: 50,   price: '£9.99' },
  { id: 'medium', credits: 150,  price: '£24.99' },
  { id: 'large',  credits: 500,  price: '£69.99' },
  { id: 'bulk',   credits: 1000, price: '£119.99' },
];

const SOURCE_DISPLAY: Record<string, { icon: string; color: string; name: string }> = {
  amazon:  { icon: '📦', color: '#ff9900', name: 'Amazon' },
  ebay:    { icon: '🏷️', color: '#e53238', name: 'eBay' },
  shopify: { icon: '🛒', color: '#96bf48', name: 'Shopify' },
};

export default function ChannelCommandCentre({
  channelId = 'temu',
  channelName = 'Temu',
  channelColor = '#F97316',
  channelIcon = '🛍️',
  sourceChannel = 'amazon',
}: Props) {
  const navigate = useNavigate();
  const [listings, setListings] = useState<ListingStats>({ draft: 0, live: 0, error: 0, submitted: 0, total: 0 });
  const [orders, setOrders] = useState<RecentOrder[]>([]);
  const [credits, setCredits] = useState<CreditInfo | null>(null);
  const [importStats, setImportStats] = useState({ total: 0, enriched: 0 });
  const [loading, setLoading] = useState(true);

  const fetchData = useCallback(async () => {
    setLoading(true);
    try {
      // Listing stats from temu_drafts
      const listingsRes = await apiFetch(`/temu/drafts/stats`);
      if (listingsRes.ok) {
        const d = await listingsRes.json();
        setListings(d);
      }

      // Recent orders
      const ordersRes = await apiFetch(`/orders?channel=${channelId}&limit=5`);
      if (ordersRes.ok) {
        const d = await ordersRes.json();
        setOrders(d.orders || []);
      }

      // Credits
      const creditsRes = await apiFetch('/ai/credits');
      if (creditsRes.ok) {
        const d = await creditsRes.json();
        setCredits(d);
      }

      // Import stats (products count)
      const prodRes = await apiFetch('/products?limit=1');
      if (prodRes.ok) {
        const d = await prodRes.json();
        setImportStats({ total: d.total || 0, enriched: d.enriched || d.total || 0 });
      }
    } catch (e) {
      console.error('Command centre fetch error:', e);
    } finally {
      setLoading(false);
    }
  }, [channelId]);

  useEffect(() => { fetchData(); }, [fetchData]);

  const src = SOURCE_DISPLAY[sourceChannel] || SOURCE_DISPLAY.amazon;

  // ── Styles ──────────────────────────────────────────────────────────────
  const card = (extra?: React.CSSProperties): React.CSSProperties => ({
    background: 'var(--bg-secondary)',
    border: '1px solid var(--border)',
    borderRadius: 10,
    padding: 20,
    ...extra,
  });

  const statBox = (accent: string): React.CSSProperties => ({
    display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center',
    padding: '16px 12px', borderRadius: 8,
    background: `${accent}15`,
    minWidth: 100, flex: 1,
  });

  const sectionTitle: React.CSSProperties = {
    fontSize: 13, fontWeight: 600, color: 'var(--text-muted)',
    textTransform: 'uppercase', letterSpacing: '0.06em', marginBottom: 12,
  };

  const btn = (bg: string, fg: string = '#fff'): React.CSSProperties => ({
    padding: '8px 16px', borderRadius: 6, border: 'none',
    background: bg, color: fg, fontSize: 13, fontWeight: 600,
    cursor: 'pointer', transition: 'opacity 0.15s',
  });

  if (loading) {
    return (
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: 300, color: 'var(--text-muted)' }}>
        <span className="spinner" style={{ width: 20, height: 20, marginRight: 10 }} /> Loading command centre...
      </div>
    );
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 20, maxWidth: 1200 }}>
      {/* ── Header ──────────────────────────────────────────────────────── */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 14 }}>
        <div style={{
          width: 48, height: 48, borderRadius: 12,
          background: `${channelColor}20`, display: 'flex',
          alignItems: 'center', justifyContent: 'center', fontSize: 26,
        }}>
          {channelIcon}
        </div>
        <div>
          <h1 style={{ margin: 0, fontSize: 22, fontWeight: 700, color: 'var(--text-primary)' }}>
            {channelName} Command Centre
          </h1>
          <div style={{ fontSize: 13, color: 'var(--text-muted)', marginTop: 2 }}>
            Manage your {channelName} listings, orders, and AI credits
          </div>
        </div>
      </div>

      {/* ── Row 1: Listing Status + Quick Actions ──────────────────────── */}
      <div style={{ display: 'grid', gridTemplateColumns: '2fr 1fr', gap: 16 }}>
        {/* Listing Status Panel */}
        <div style={card()}>
          <div style={sectionTitle}>Listing Status</div>
          <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap' }}>
            <div style={statBox(channelColor)}>
              <div style={{ fontSize: 28, fontWeight: 700, color: channelColor }}>{listings.draft}</div>
              <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 4 }}>Drafts</div>
            </div>
            <div style={statBox('#10b981')}>
              <div style={{ fontSize: 28, fontWeight: 700, color: '#10b981' }}>{listings.live}</div>
              <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 4 }}>Live</div>
            </div>
            <div style={statBox('#3b82f6')}>
              <div style={{ fontSize: 28, fontWeight: 700, color: '#3b82f6' }}>{listings.submitted}</div>
              <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 4 }}>Submitted</div>
            </div>
            <div style={statBox('#ef4444')}>
              <div style={{ fontSize: 28, fontWeight: 700, color: '#ef4444' }}>{listings.error}</div>
              <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 4 }}>Errors</div>
            </div>
          </div>
          <div style={{ display: 'flex', gap: 10, marginTop: 16 }}>
            <button style={btn(channelColor)} onClick={() => navigate('/marketplace/listings')}>
              View All Listings
            </button>
            <button style={btn('var(--bg-tertiary)', 'var(--text-primary)')} onClick={() => navigate('/temu-wizard')}>
              Generate More Listings
            </button>
          </div>
        </div>

        {/* Source Channel Summary */}
        <div style={card()}>
          <div style={sectionTitle}>
            <span style={{ marginRight: 6 }}>{src.icon}</span> {src.name} Import Summary
          </div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <span style={{ color: 'var(--text-secondary)', fontSize: 13 }}>Products Imported</span>
              <span style={{ fontWeight: 700, fontSize: 18, color: src.color }}>{importStats.total}</span>
            </div>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <span style={{ color: 'var(--text-secondary)', fontSize: 13 }}>Enriched</span>
              <span style={{ fontWeight: 700, fontSize: 18, color: '#10b981' }}>{importStats.enriched}</span>
            </div>
            <div style={{
              height: 6, borderRadius: 3, background: 'var(--bg-tertiary)', marginTop: 4, overflow: 'hidden',
            }}>
              <div style={{
                height: '100%', borderRadius: 3, background: src.color,
                width: importStats.total > 0 ? `${Math.min(100, (importStats.enriched / importStats.total) * 100)}%` : '0%',
                transition: 'width 0.4s ease',
              }} />
            </div>
          </div>
          <button
            style={{ ...btn('var(--bg-tertiary)', 'var(--text-primary)'), marginTop: 14, width: '100%' }}
            onClick={() => navigate('/marketplace/import')}
          >
            Import More Products
          </button>
        </div>
      </div>

      {/* ── Row 2: Recent Orders + Credits ─────────────────────────────── */}
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16 }}>
        {/* Recent Orders */}
        <div style={card()}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12 }}>
            <div style={sectionTitle}>Recent {channelName} Orders</div>
            <button style={{ ...btn(channelColor), padding: '6px 12px', fontSize: 12 }} onClick={() => navigate(`/orders?channel=${channelId}`)}>
              View All
            </button>
          </div>
          {orders.length === 0 ? (
            <div style={{ textAlign: 'center', padding: '24px 0', color: 'var(--text-muted)', fontSize: 13 }}>
              No {channelName} orders yet. Once your listings go live, orders will appear here.
            </div>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
              {orders.map(o => (
                <div key={o.order_id} style={{
                  display: 'flex', justifyContent: 'space-between', alignItems: 'center',
                  padding: '10px 12px', borderRadius: 6, background: 'var(--bg-primary)',
                  border: '1px solid var(--border)',
                }}>
                  <div>
                    <div style={{ fontWeight: 600, fontSize: 13, color: 'var(--text-primary)' }}>
                      #{o.order_id.slice(0, 12)}
                    </div>
                    <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>
                      {o.customer_name || 'Unknown customer'}
                    </div>
                  </div>
                  <div style={{ textAlign: 'right' }}>
                    <div style={{ fontWeight: 600, fontSize: 13, color: 'var(--text-primary)' }}>
                      {o.currency === 'GBP' ? '£' : o.currency === 'USD' ? '$' : o.currency}{o.total?.toFixed(2)}
                    </div>
                    <div style={{
                      display: 'inline-block', padding: '2px 8px', borderRadius: 4, fontSize: 11, fontWeight: 600, marginTop: 2,
                      background: o.status === 'dispatched' ? 'rgba(16,185,129,0.15)' : o.status === 'processing' ? 'rgba(251,191,36,0.15)' : 'rgba(59,130,246,0.15)',
                      color: o.status === 'dispatched' ? '#10b981' : o.status === 'processing' ? '#fbbf24' : '#60a5fa',
                    }}>
                      {o.status}
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>

        {/* Credits & Subscription Panel */}
        <div style={card()}>
          <div style={sectionTitle}>AI Credits & Plan</div>
          {credits ? (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
              {/* Credit balance display */}
              <div style={{
                display: 'flex', justifyContent: 'space-between', alignItems: 'center',
                padding: '14px 16px', borderRadius: 8, background: `${channelColor}10`,
                border: `1px solid ${channelColor}30`,
              }}>
                <div>
                  <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>Available Credits</div>
                  <div style={{ fontSize: 32, fontWeight: 700, color: channelColor, lineHeight: 1.1, marginTop: 4 }}>
                    {credits.total_available}
                  </div>
                </div>
                <div style={{ textAlign: 'right', fontSize: 12, color: 'var(--text-secondary)', lineHeight: 1.7 }}>
                  <div>Free: {credits.free_remaining}/{credits.free_limit}</div>
                  {credits.monthly_limit > 0 && <div>Monthly: {credits.monthly_remaining}/{credits.monthly_limit}</div>}
                  {credits.purchased_remaining > 0 && <div>Purchased: {credits.purchased_remaining}</div>}
                </div>
              </div>

              {/* Current Plan */}
              {credits.subscription_plan ? (
                <div style={{
                  padding: '10px 14px', borderRadius: 6, background: 'var(--bg-primary)',
                  border: '1px solid var(--border)', display: 'flex', justifyContent: 'space-between', alignItems: 'center',
                }}>
                  <div>
                    <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>Current Plan</div>
                    <div style={{ fontSize: 15, fontWeight: 700, color: 'var(--text-primary)', marginTop: 2 }}>
                      {PLAN_DISPLAY[credits.subscription_plan]?.name || credits.subscription_plan}
                    </div>
                  </div>
                  <button style={btn('var(--bg-tertiary)', 'var(--text-primary)')} onClick={() => navigate('/settings/billing')}>
                    Manage
                  </button>
                </div>
              ) : (
                /* Plan Recommendation */
                <div style={{
                  padding: '14px 16px', borderRadius: 8,
                  background: 'linear-gradient(135deg, rgba(59,130,246,0.08), rgba(139,92,246,0.08))',
                  border: '1px solid rgba(59,130,246,0.2)',
                }}>
                  <div style={{ fontSize: 12, fontWeight: 600, color: '#3b82f6', marginBottom: 6 }}>
                    Recommended Plan
                  </div>
                  <div style={{ fontSize: 15, fontWeight: 700, color: 'var(--text-primary)' }}>
                    {PLAN_DISPLAY[credits.recommended_plan]?.name || 'Starter S'}
                  </div>
                  <div style={{ fontSize: 12, color: 'var(--text-secondary)', marginTop: 4 }}>
                    Based on ~{credits.estimated_monthly_orders} orders/month — includes{' '}
                    {PLAN_DISPLAY[credits.recommended_plan]?.credits || '50/mo'} AI credits
                  </div>
                  <button
                    style={{ ...btn('#3b82f6'), marginTop: 10 }}
                    onClick={() => navigate('/settings/billing')}
                  >
                    Upgrade to {PLAN_DISPLAY[credits.recommended_plan]?.name} — {PLAN_DISPLAY[credits.recommended_plan]?.price}
                  </button>
                </div>
              )}

              {/* Quick Credit Pack Purchase */}
              {credits.total_available < 20 && (
                <div style={{ padding: '10px 14px', borderRadius: 6, background: 'rgba(239,68,68,0.08)', border: '1px solid rgba(239,68,68,0.2)' }}>
                  <div style={{ fontSize: 12, fontWeight: 600, color: '#ef4444', marginBottom: 8 }}>Credits Running Low</div>
                  <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
                    {CREDIT_PACKS.slice(0, 3).map(p => (
                      <button key={p.id}
                        style={{
                          padding: '6px 12px', borderRadius: 6, fontSize: 12, fontWeight: 600,
                          border: '1px solid var(--border)', background: 'var(--bg-primary)',
                          color: 'var(--text-primary)', cursor: 'pointer',
                        }}
                        onClick={() => {
                          apiFetch('/ai/credits/purchase', {
                            method: 'POST',
                            body: JSON.stringify({ pack_size: p.id }),
                          }).then(() => fetchData());
                        }}
                      >
                        {p.credits} credits — {p.price}
                      </button>
                    ))}
                  </div>
                </div>
              )}
            </div>
          ) : (
            <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading credits...</div>
          )}
        </div>
      </div>

      {/* ── Row 3: Feature Discovery Nudges ────────────────────────────── */}
      <div style={card()}>
        <div style={sectionTitle}>Grow Your Business</div>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, 1fr)', gap: 12 }}>
          {[
            { icon: '📊', title: 'Analytics', desc: 'Track sales performance across all channels', to: '/analytics', color: '#3b82f6' },
            { icon: '🚚', title: 'Dispatch', desc: 'Auto-assign carriers and print shipping labels', to: '/dispatch', color: '#8b5cf6' },
            { icon: '🔄', title: 'Automation', desc: 'Set up workflow rules to save time', to: '/workflows', color: '#10b981' },
            { icon: '🛒', title: 'More Channels', desc: 'Expand to Shopify, Etsy, TikTok Shop & more', to: '/marketplace/connections', color: '#f59e0b' },
          ].map(n => (
            <button
              key={n.to}
              onClick={() => navigate(n.to)}
              style={{
                display: 'flex', flexDirection: 'column', alignItems: 'flex-start',
                padding: '16px 14px', borderRadius: 8,
                background: 'var(--bg-primary)', border: '1px solid var(--border)',
                cursor: 'pointer', textAlign: 'left', transition: 'border-color 0.15s',
              }}
              onMouseEnter={e => (e.currentTarget.style.borderColor = n.color)}
              onMouseLeave={e => (e.currentTarget.style.borderColor = 'var(--border)')}
            >
              <div style={{ fontSize: 24, marginBottom: 8 }}>{n.icon}</div>
              <div style={{ fontSize: 14, fontWeight: 600, color: 'var(--text-primary)' }}>{n.title}</div>
              <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 4, lineHeight: 1.4 }}>{n.desc}</div>
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}

// ============================================================================
// COMPARE INTEGRATIONS PAGE
// ============================================================================
// Route: /marketplace/compare
// Shows a feature-capability matrix for ALL supported channels.
// Connected channels are highlighted; unconnected show a Connect button.
//
// BUG FIX: credentialService.list() takes no arguments and returns an Axios
// response { data: { data: MarketplaceCredential[] } }, not a raw array.
// The previous code called .list(tenantID).then(setCredentials) which set
// credentials to the full Axios response object, causing t.map crashes.
// ============================================================================

import { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { credentialService, MarketplaceCredential } from '../../services/marketplace-api';

// ── capability matrix definition ─────────────────────────────────────────────

const CAPABILITIES = [
  { key: 'listing',        label: 'Listings',        icon: '📦', description: 'Push product listings to channel' },
  { key: 'order_sync',     label: 'Order Sync',      icon: '🛒', description: 'Import orders automatically' },
  { key: 'tracking',       label: 'Tracking Push',   icon: '🚚', description: 'Send tracking numbers to channel' },
  { key: 'inventory_sync', label: 'Inventory Sync',  icon: '📊', description: 'Keep stock levels in sync' },
  { key: 'price_sync',     label: 'Price Sync',      icon: '💰', description: 'Sync prices across channels' },
  { key: 'fba',            label: 'FBA / Fulfilment', icon: '🏭', description: 'Marketplace-managed fulfilment' },
  { key: 'variations',     label: 'Variations',      icon: '🎨', description: 'Parent/child product support' },
  { key: 'bulk_update',    label: 'Bulk Update',     icon: '⚡', description: 'Batch price/stock updates' },
  { key: 'refunds',        label: 'Refunds / RMA',   icon: '↩️', description: 'Process returns via API' },
] as const;

type CapabilityKey = typeof CAPABILITIES[number]['key'];

interface ChannelDef {
  id: string;
  name: string;
  icon: string;
  color: string;
  region: string;
  features: CapabilityKey[];
  note?: string;
}

const ALL_CHANNELS: ChannelDef[] = [
  { id: 'amazon',        name: 'Amazon',          icon: 'ri-amazon-fill',        color: '#FF9900', region: 'Global',       features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync', 'fba', 'variations'] },
  { id: 'ebay',          name: 'eBay',             icon: 'ri-auction-fill',       color: '#E53238', region: 'Global',       features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync', 'variations'] },
  { id: 'shopify',       name: 'Shopify',          icon: 'ri-store-2-fill',       color: '#96BF48', region: 'Global',       features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync', 'variations'] },
  { id: 'backmarket',    name: 'Back Market',      icon: 'ri-recycle-fill',       color: '#14B8A6', region: 'EU / US',      features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync'], note: 'Refurbished/second-hand only. Grade (excellent/good/fair) required on listings.' },
  { id: 'zalando',       name: 'Zalando',          icon: 'ri-shopping-bag-3-fill',color: '#FF6600', region: 'EU',           features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync'], note: 'Listing creation requires the Zalando Partner Portal. API manages price/stock updates.' },
  { id: 'bol',           name: 'Bol.com',          icon: 'ri-store-2-fill',       color: '#0E4299', region: 'NL / BE',      features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync'] },
  { id: 'lazada',        name: 'Lazada',           icon: 'ri-store-fill',         color: '#F57224', region: 'SEA',          features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync'], note: 'Listing creation via Seller Center. API manages price/stock/tracking.' },
  { id: 'tiktok',        name: 'TikTok Shop',      icon: 'ri-tiktok-fill',        color: '#000000', region: 'UK / US / SEA',features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync'] },
  { id: 'etsy',          name: 'Etsy',             icon: 'ri-store-2-fill',       color: '#F56400', region: 'Global',       features: ['listing', 'order_sync', 'tracking'] },
  { id: 'woocommerce',   name: 'WooCommerce',      icon: 'ri-store-3-fill',       color: '#7F54B3', region: 'Global',       features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync'] },
  { id: 'walmart',       name: 'Walmart',          icon: 'ri-store-2-fill',       color: '#0071CE', region: 'US',           features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync'] },
  { id: 'kaufland',      name: 'Kaufland',         icon: 'ri-shopping-bag-3-fill',color: '#E2001A', region: 'DE / EU',      features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync', 'bulk_update', 'refunds'] },
  { id: 'temu',          name: 'Temu',             icon: 'ri-store-2-fill',       color: '#FC5B00', region: 'Global',       features: ['listing', 'order_sync'] },
  { id: 'tesco',         name: 'Tesco',            icon: 'ri-store-3-fill',       color: '#00539F', region: 'UK',           features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync', 'bulk_update', 'refunds'] },
  { id: 'onbuy',         name: 'OnBuy',            icon: 'ri-shopping-bag-3-line',color: '#FF6B35', region: 'UK',           features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync'] },
  { id: 'magento',       name: 'Magento 2',        icon: 'ri-store-line',         color: '#EE672F', region: 'Global',       features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync'] },
  { id: 'bigcommerce',   name: 'BigCommerce',      icon: 'ri-store-3-line',       color: '#34313F', region: 'Global',       features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync'] },
  { id: 'amazon_vendor', name: 'Amazon Vendor',    icon: 'ri-amazon-fill',        color: '#232F3E', region: 'Global',       features: ['order_sync'], note: 'Vendor Central — receives purchase orders from Amazon.' },
  { id: 'asos',          name: 'ASOS',             icon: 'ri-shopping-bag-fill',  color: '#1C1C1C', region: 'UK / Global',  features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync', 'bulk_update', 'refunds'] },
  { id: 'bandq',         name: 'B&Q',              icon: 'ri-tools-fill',         color: '#F04E23', region: 'UK',           features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync', 'bulk_update', 'refunds'] },
  { id: 'fnac_darty',    name: 'Fnac Darty',       icon: 'ri-music-fill',         color: '#F9A825', region: 'FR',           features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync', 'bulk_update', 'refunds'] },
  { id: 'carrefour',     name: 'Carrefour',        icon: 'ri-store-fill',         color: '#004B87', region: 'FR / EU',      features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync', 'bulk_update', 'refunds'] },
  { id: 'mediamarkt',    name: 'MediaMarkt',       icon: 'ri-tv-fill',            color: '#CC0000', region: 'DE / EU',      features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync', 'bulk_update', 'refunds'] },
];

function hasFeature(channel: ChannelDef, feature: CapabilityKey): boolean {
  return (channel.features as string[]).includes(feature);
}

export default function CompareIntegrations() {
  const navigate = useNavigate();
  const [credentials, setCredentials] = useState<MarketplaceCredential[]>([]);
  const [loading, setLoading] = useState(true);
  const [search, setSearch] = useState('');
  const [regionFilter, setRegionFilter] = useState('All');

  useEffect(() => {
    // FIX: credentialService.list() takes no arguments and returns an Axios
    // response object. Extract the array from res.data.data.
    credentialService.list()
      .then(res => setCredentials(res.data?.data || []))
      .catch(() => setCredentials([]))
      .finally(() => setLoading(false));
  }, []);

  const connectedChannelIDs = new Set(credentials.map(c => c.channel));

  const regions = ['All', ...Array.from(new Set(ALL_CHANNELS.map(c => c.region))).sort()];

  const filtered = ALL_CHANNELS.filter(ch => {
    const matchSearch = ch.name.toLowerCase().includes(search.toLowerCase());
    const matchRegion = regionFilter === 'All' || ch.region === regionFilter;
    return matchSearch && matchRegion;
  });

  const featureCount = (ch: ChannelDef) => ch.features.length;

  return (
    <div style={{ padding: '24px 28px', fontFamily: "'DM Sans', system-ui, sans-serif" }}>
      {/* Header */}
      <div style={{ marginBottom: 28 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 4 }}>
          <button
            onClick={() => navigate('/marketplace')}
            style={{ background: 'none', border: 'none', color: 'var(--text-muted)', cursor: 'pointer', fontSize: 20, padding: 0, display: 'flex', alignItems: 'center' }}
          >
            ←
          </button>
          <h1 style={{ margin: 0, fontSize: 22, fontWeight: 700 }}>Compare Integrations</h1>
          <span style={{ fontSize: 12, background: 'var(--accent, #6366f1)', color: '#fff', borderRadius: 999, padding: '2px 10px', fontWeight: 600 }}>
            {ALL_CHANNELS.length} channels
          </span>
        </div>
        <p style={{ color: 'var(--text-muted)', margin: 0, marginLeft: 36, fontSize: 14 }}>
          Feature matrix across all supported marketplaces. Connected channels are highlighted.
        </p>
      </div>

      {/* Filters */}
      <div style={{ display: 'flex', gap: 12, marginBottom: 20, flexWrap: 'wrap', alignItems: 'center' }}>
        <input
          placeholder="🔍  Search channels…"
          value={search}
          onChange={e => setSearch(e.target.value)}
          style={{
            padding: '8px 14px', borderRadius: 8, border: '1px solid var(--border)',
            background: 'var(--surface)', color: 'var(--text)', fontSize: 14, outline: 'none', minWidth: 220,
          }}
        />
        <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
          {regions.map(r => (
            <button key={r} onClick={() => setRegionFilter(r)} style={{
              padding: '6px 14px', borderRadius: 999, fontSize: 13, fontWeight: 500, cursor: 'pointer',
              border: '1px solid var(--border)',
              background: regionFilter === r ? 'var(--accent, #6366f1)' : 'var(--surface)',
              color: regionFilter === r ? '#fff' : 'var(--text)',
              transition: 'all 0.15s',
            }}>
              {r}
            </button>
          ))}
        </div>
        <div style={{ marginLeft: 'auto', display: 'flex', gap: 16, fontSize: 13, color: 'var(--text-muted)' }}>
          <span style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
            <span style={{ width: 10, height: 10, borderRadius: 2, background: '#22c55e', display: 'inline-block' }} />
            Connected
          </span>
          <span style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
            <span style={{ width: 10, height: 10, borderRadius: 2, background: 'var(--surface-2, #f1f5f9)', border: '1px solid var(--border)', display: 'inline-block' }} />
            Not connected
          </span>
        </div>
      </div>

      {/* Matrix table */}
      <div style={{ overflowX: 'auto', borderRadius: 12, border: '1px solid var(--border)' }}>
        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
          <thead>
            <tr style={{ background: 'var(--surface-2, #f8fafc)', borderBottom: '2px solid var(--border)' }}>
              <th style={{ padding: '12px 16px', textAlign: 'left', fontWeight: 700, minWidth: 180, position: 'sticky', left: 0, background: 'var(--surface-2, #f8fafc)', zIndex: 2, borderRight: '1px solid var(--border)' }}>
                Channel
              </th>
              <th style={{ padding: '10px 12px', textAlign: 'center', fontWeight: 600, color: 'var(--text-muted)', minWidth: 80, fontSize: 12 }}>
                Region
              </th>
              {CAPABILITIES.map(cap => (
                <th key={cap.key} style={{ padding: '10px 8px', textAlign: 'center', fontWeight: 600, color: 'var(--text-muted)', minWidth: 90, fontSize: 11, lineHeight: 1.3 }}>
                  <span title={cap.description} style={{ cursor: 'help' }}>
                    {cap.icon}<br />{cap.label}
                  </span>
                </th>
              ))}
              <th style={{ padding: '10px 12px', textAlign: 'center', fontWeight: 600, color: 'var(--text-muted)', minWidth: 80, fontSize: 12 }}>
                Score
              </th>
              <th style={{ padding: '10px 12px', textAlign: 'center', fontWeight: 600, color: 'var(--text-muted)', minWidth: 120, fontSize: 12 }}>
                Action
              </th>
            </tr>
          </thead>
          <tbody>
            {loading ? (
              <tr>
                <td colSpan={CAPABILITIES.length + 4} style={{ padding: 32, textAlign: 'center', color: 'var(--text-muted)' }}>
                  Loading connections…
                </td>
              </tr>
            ) : filtered.length === 0 ? (
              <tr>
                <td colSpan={CAPABILITIES.length + 4} style={{ padding: 32, textAlign: 'center', color: 'var(--text-muted)' }}>
                  No channels match your filters.
                </td>
              </tr>
            ) : (
              filtered.map((channel, idx) => {
                const isConnected = connectedChannelIDs.has(channel.id);
                const score = featureCount(channel);
                const maxScore = Math.max(...ALL_CHANNELS.map(featureCount));
                const scorePercent = Math.round((score / maxScore) * 100);

                return (
                  <tr
                    key={channel.id}
                    style={{
                      background: isConnected ? 'rgba(34,197,94,0.04)' : idx % 2 === 0 ? 'var(--surface)' : 'var(--surface-2, #fafafa)',
                      borderBottom: '1px solid var(--border)',
                      transition: 'background 0.1s',
                    }}
                    onMouseEnter={e => (e.currentTarget.style.background = isConnected ? 'rgba(34,197,94,0.08)' : 'var(--hover, rgba(0,0,0,0.03))')}
                    onMouseLeave={e => (e.currentTarget.style.background = isConnected ? 'rgba(34,197,94,0.04)' : idx % 2 === 0 ? 'var(--surface)' : 'var(--surface-2, #fafafa)')}
                  >
                    {/* Channel name */}
                    <td style={{ padding: '12px 16px', position: 'sticky', left: 0, background: isConnected ? 'rgba(34,197,94,0.06)' : 'var(--surface)', zIndex: 1, borderRight: '1px solid var(--border)' }}>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                        {isConnected && <span style={{ width: 6, height: 6, borderRadius: '50%', background: '#22c55e', flexShrink: 0 }} />}
                        <i className={channel.icon} style={{ fontSize: 18, color: channel.color, flexShrink: 0 }} />
                        <div>
                          <div style={{ fontWeight: 600, fontSize: 13 }}>{channel.name}</div>
                          {isConnected && <div style={{ fontSize: 11, color: '#22c55e', fontWeight: 500 }}>Connected</div>}
                        </div>
                        {channel.note && (
                          <span style={{ fontSize: 11, color: 'var(--text-muted)', cursor: 'help', marginLeft: 'auto' }} title={channel.note}>
                            ℹ️
                          </span>
                        )}
                      </div>
                    </td>

                    {/* Region */}
                    <td style={{ padding: '10px 12px', textAlign: 'center' }}>
                      <span style={{ fontSize: 11, background: 'var(--surface-2)', borderRadius: 999, padding: '2px 8px', color: 'var(--text-muted)', fontWeight: 500, whiteSpace: 'nowrap' }}>
                        {channel.region}
                      </span>
                    </td>

                    {/* Feature cells */}
                    {CAPABILITIES.map(cap => (
                      <td key={cap.key} style={{ padding: '10px 8px', textAlign: 'center' }}>
                        {hasFeature(channel, cap.key)
                          ? <span style={{ color: '#22c55e', fontSize: 16 }}>✓</span>
                          : <span style={{ color: 'var(--border)', fontSize: 14 }}>—</span>
                        }
                      </td>
                    ))}

                    {/* Score bar */}
                    <td style={{ padding: '10px 12px', textAlign: 'center' }}>
                      <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 3 }}>
                        <span style={{ fontSize: 12, fontWeight: 700, color: 'var(--text)' }}>{score}/{maxScore}</span>
                        <div style={{ width: 48, height: 4, background: 'var(--border)', borderRadius: 2, overflow: 'hidden' }}>
                          <div style={{
                            width: `${scorePercent}%`, height: '100%', borderRadius: 2, transition: 'width 0.3s',
                            background: scorePercent >= 80 ? '#22c55e' : scorePercent >= 50 ? '#f59e0b' : '#94a3b8',
                          }} />
                        </div>
                      </div>
                    </td>

                    {/* Action */}
                    <td style={{ padding: '10px 12px', textAlign: 'center' }}>
                      {isConnected ? (
                        <button onClick={() => navigate('/marketplace')} style={{ padding: '5px 12px', borderRadius: 6, border: '1px solid var(--border)', background: 'var(--surface)', color: 'var(--text)', cursor: 'pointer', fontSize: 12, fontWeight: 500 }}>
                          ⚙️ Manage
                        </button>
                      ) : (
                        <button onClick={() => navigate('/marketplace')} style={{ padding: '5px 12px', borderRadius: 6, border: 'none', background: 'var(--accent, #6366f1)', color: '#fff', cursor: 'pointer', fontSize: 12, fontWeight: 600 }}>
                          + Connect
                        </button>
                      )}
                    </td>
                  </tr>
                );
              })
            )}
          </tbody>
        </table>
      </div>

      {/* Summary bar */}
      <div style={{ marginTop: 16, display: 'flex', gap: 20, fontSize: 13, color: 'var(--text-muted)' }}>
        <span>✅ {credentials.length} channel{credentials.length !== 1 ? 's' : ''} connected</span>
        <span>⬜ {ALL_CHANNELS.length - credentials.length} available to connect</span>
        <span style={{ marginLeft: 'auto' }}>
          <a href="https://docs.marketmate.io/channels" target="_blank" rel="noreferrer" style={{ color: 'var(--accent, #6366f1)', textDecoration: 'none', fontWeight: 500 }}>
            View channel documentation →
          </a>
        </span>
      </div>
    </div>
  );
}

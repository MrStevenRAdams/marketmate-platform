// ============================================================================
// CONFIGURATOR LIST PAGE — SESSION 1 (CFG-01, CFG-02)
// ============================================================================
// Route: /marketplace/configurators
// Shows all configurators for the tenant as a filterable table.
// Actions: New, Open, Duplicate, Delete.

import { useState, useEffect, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { configuratorService, ConfiguratorWithStats } from '../../services/configurator-api';

// Channel display helpers — matches ListingList.tsx conventions
const channelEmoji: Record<string, string> = {
  amazon: '📦', ebay: '🏷️', shopify: '🛒', bigcommerce: '🛍️',
  magento: '🔶', woocommerce: '🟣', etsy: '🧶', walmart: '🟡',
  tiktok: '🎵', onbuy: '🔵', kaufland: '🟠', temu: '🛍️',
  mirakl: '⚡', tesco: '🏪',
};
const channelColor: Record<string, string> = {
  amazon: '#FF9900', ebay: '#E53238', shopify: '#96BF48', bigcommerce: '#34313F',
  magento: '#EE672F', woocommerce: '#7F54B3', etsy: '#F1641E', walmart: '#0071CE',
  tiktok: '#010101', onbuy: '#005AF0', kaufland: '#E20015', temu: '#FF6B35',
  mirakl: '#5B3CC4', tesco: '#EE1C2E',
};

const ALL_CHANNELS = [
  'amazon', 'ebay', 'shopify', 'bigcommerce', 'magento', 'woocommerce',
  'etsy', 'walmart', 'tiktok', 'onbuy', 'kaufland', 'temu', 'mirakl', 'tesco',
];

export default function ConfiguratorList() {
  const navigate = useNavigate();
  const [configurators, setConfigurators] = useState<ConfiguratorWithStats[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [channelFilter, setChannelFilter] = useState('all');
  const [search, setSearch] = useState('');
  const [deleting, setDeleting] = useState<string | null>(null);
  const [duplicating, setDuplicating] = useState<string | null>(null);
  const [confirmDelete, setConfirmDelete] = useState<ConfiguratorWithStats | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const res = await configuratorService.list(
        channelFilter !== 'all' ? { channel: channelFilter } : undefined,
      );
      setConfigurators(res.data?.configurators || []);
    } catch (err: any) {
      setError(err?.response?.data?.error || err.message || 'Failed to load configurators');
    } finally {
      setLoading(false);
    }
  }, [channelFilter]);

  useEffect(() => { load(); }, [load]);

  const filtered = configurators.filter(c => {
    if (!search) return true;
    const q = search.toLowerCase();
    return c.name.toLowerCase().includes(q) || c.channel.includes(q);
  });

  async function handleDuplicate(cfg: ConfiguratorWithStats) {
    setDuplicating(cfg.configurator_id);
    try {
      const res = await configuratorService.duplicate(cfg.configurator_id);
      const newCfg = res.data?.configurator;
      if (newCfg) navigate(`/marketplace/configurators/${newCfg.configurator_id}`);
    } catch (err: any) {
      alert(err?.response?.data?.error || 'Duplicate failed');
    } finally {
      setDuplicating(null);
    }
  }

  async function handleDelete(cfg: ConfiguratorWithStats, force = false) {
    setDeleting(cfg.configurator_id);
    setConfirmDelete(null);
    try {
      await configuratorService.delete(cfg.configurator_id, force);
      await load();
    } catch (err: any) {
      const msg = err?.response?.data?.error || err.message || 'Delete failed';
      // If backend returns conflict (linked listings) prompt force-delete
      if (err?.response?.status === 409) {
        const ok = window.confirm(
          `${msg}\n\nDelete anyway and unlink all ${cfg.listing_count} listings?`,
        );
        if (ok) await handleDelete(cfg, true);
      } else {
        alert(msg);
      }
    } finally {
      setDeleting(null);
    }
  }

  return (
    <div className="page">
      {/* ── Page header ── */}
      <div className="page-header">
        <div>
          <h1 className="page-title">⚙️ Configurators</h1>
          <p className="page-subtitle">
            Reusable channel settings — define category, shipping, attributes and variation schema once, apply to many listings.
          </p>
        </div>
        <div className="page-actions" style={{ display: 'flex', gap: 8 }}>
          <button
            className="btn btn-secondary"
            onClick={() => navigate('/marketplace/configurators/ai-setup')}
            title="Use AI to suggest category, attributes, and shipping defaults"
          >
            🤖 AI Setup
          </button>
          <button
            className="btn btn-primary"
            onClick={() => navigate('/marketplace/configurators/new')}
          >
            + New Configurator
          </button>
        </div>
      </div>

      {/* ── Toolbar ── */}
      <div style={{ display: 'flex', gap: 12, marginBottom: 20, flexWrap: 'wrap', alignItems: 'center' }}>
        <input
          className="input"
          style={{ width: 260 }}
          placeholder="Search by name or channel…"
          value={search}
          onChange={e => setSearch(e.target.value)}
        />
        <select
          className="select"
          style={{ width: 160 }}
          value={channelFilter}
          onChange={e => setChannelFilter(e.target.value)}
        >
          <option value="all">All Channels</option>
          {ALL_CHANNELS.map(ch => (
            <option key={ch} value={ch}>{channelEmoji[ch] || '🌐'} {ch.charAt(0).toUpperCase() + ch.slice(1)}</option>
          ))}
        </select>
      </div>

      {/* ── Error ── */}
      {error && (
        <div style={{
          padding: 12, marginBottom: 16, borderRadius: 8,
          background: 'var(--danger-glow)', border: '1px solid var(--danger)',
          color: 'var(--danger)', fontSize: 13,
        }}>{error}</div>
      )}

      {/* ── Table ── */}
      <div className="card" style={{ padding: 0, overflow: 'hidden' }}>
        {loading ? (
          <div style={{ padding: 48, textAlign: 'center', color: 'var(--text-muted)' }}>
            <div style={{ fontSize: 32, marginBottom: 12 }}>⏳</div>
            Loading configurators…
          </div>
        ) : filtered.length === 0 ? (
          <div style={{ padding: 48, textAlign: 'center' }}>
            <div style={{ fontSize: 48, marginBottom: 16 }}>⚙️</div>
            <h3 style={{ fontSize: 18, fontWeight: 700, marginBottom: 8, color: 'var(--text-primary)' }}>
              {search || channelFilter !== 'all' ? 'No configurators match your filters' : 'No configurators yet'}
            </h3>
            {!search && channelFilter === 'all' && (
              <>
                <p style={{ color: 'var(--text-muted)', maxWidth: 480, margin: '0 auto 20px' }}>
                  Configurators are reusable settings containers. Create one to define a channel's
                  category, shipping rules, attribute mappings and variation schema — then link it to
                  many listings and push changes in bulk.
                </p>
                <button
                  className="btn btn-primary"
                  onClick={() => navigate('/marketplace/configurators/new')}
                >
                  Create your first configurator
                </button>
              </>
            )}
          </div>
        ) : (
          <div style={{ overflowX: 'auto' }}>
            <table style={{ width: '100%', borderCollapse: 'collapse' }}>
              <thead>
                <tr style={{ borderBottom: '1px solid var(--border)' }}>
                  <th style={thStyle}>Name</th>
                  <th style={thStyle}>Channel</th>
                  <th style={thStyle}>Credential</th>
                  <th style={{ ...thStyle, textAlign: 'center' }}>Listings</th>
                  <th style={{ ...thStyle, textAlign: 'center' }}>In Process</th>
                  <th style={{ ...thStyle, textAlign: 'center' }}>Errors</th>
                  <th style={{ ...thStyle, textAlign: 'right' }}>Actions</th>
                </tr>
              </thead>
              <tbody>
                {filtered.map(cfg => (
                  <tr
                    key={cfg.configurator_id}
                    style={{ borderBottom: '1px solid var(--border)', cursor: 'pointer' }}
                    onClick={() => navigate(`/marketplace/configurators/${cfg.configurator_id}`)}
                  >
                    {/* Name */}
                    <td style={tdStyle}>
                      <div style={{ fontWeight: 600, fontSize: 14, color: 'var(--text-primary)' }}>
                        {cfg.name}
                      </div>
                      {cfg.category_path && (
                        <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 2 }}>
                          {cfg.category_path}
                        </div>
                      )}
                    </td>

                    {/* Channel badge */}
                    <td style={tdStyle}>
                      <span style={{
                        display: 'inline-flex', alignItems: 'center', gap: 6,
                        padding: '3px 10px', borderRadius: 20, fontSize: 12, fontWeight: 600,
                        background: (channelColor[cfg.channel] || '#888') + '22',
                        color: channelColor[cfg.channel] || 'var(--text-secondary)',
                        border: `1px solid ${(channelColor[cfg.channel] || '#888')}44`,
                      }}>
                        {channelEmoji[cfg.channel] || '🌐'} {cfg.channel}
                      </span>
                    </td>

                    {/* Credential */}
                    <td style={{ ...tdStyle, color: 'var(--text-secondary)', fontSize: 13 }}>
                      {cfg.channel_credential_id ? (
                        <span style={{ fontFamily: 'monospace', fontSize: 11 }}>
                          {cfg.channel_credential_id.slice(0, 16)}…
                        </span>
                      ) : (
                        <span style={{ color: 'var(--text-muted)' }}>—</span>
                      )}
                    </td>

                    {/* Listing count */}
                    <td style={{ ...tdStyle, textAlign: 'center' }}>
                      <span style={{ fontWeight: 700, color: 'var(--text-primary)' }}>
                        {cfg.listing_count}
                      </span>
                    </td>

                    {/* In-process count */}
                    <td style={{ ...tdStyle, textAlign: 'center' }}>
                      {cfg.in_process_count > 0 ? (
                        <span style={{
                          padding: '2px 8px', borderRadius: 4, fontSize: 11, fontWeight: 700,
                          background: 'var(--warning-glow)', color: 'var(--warning)',
                        }}>
                          {cfg.in_process_count}
                        </span>
                      ) : (
                        <span style={{ color: 'var(--text-muted)' }}>—</span>
                      )}
                    </td>

                    {/* Error count */}
                    <td style={{ ...tdStyle, textAlign: 'center' }}>
                      {cfg.error_count > 0 ? (
                        <span style={{
                          padding: '2px 8px', borderRadius: 4, fontSize: 11, fontWeight: 700,
                          background: 'var(--danger-glow)', color: 'var(--danger)',
                        }}>
                          {cfg.error_count}
                        </span>
                      ) : (
                        <span style={{ color: 'var(--text-muted)' }}>—</span>
                      )}
                    </td>

                    {/* Row actions */}
                    <td style={{ ...tdStyle, textAlign: 'right' }} onClick={e => e.stopPropagation()}>
                      <div style={{ display: 'flex', gap: 4, justifyContent: 'flex-end' }}>
                        <button
                          className="btn-icon"
                          title="Open"
                          onClick={() => navigate(`/marketplace/configurators/${cfg.configurator_id}`)}
                        >
                          ✏️
                        </button>
                        <button
                          className="btn-icon"
                          title="Duplicate"
                          disabled={duplicating === cfg.configurator_id}
                          onClick={() => handleDuplicate(cfg)}
                        >
                          {duplicating === cfg.configurator_id ? '⏳' : '📋'}
                        </button>
                        <button
                          className="btn-icon"
                          title="Delete"
                          disabled={deleting === cfg.configurator_id}
                          onClick={() => setConfirmDelete(cfg)}
                        >
                          🗑️
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {/* ── Confirm delete dialog ── */}
      {confirmDelete && (
        <div style={{
          position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.6)',
          display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000,
        }} onClick={() => setConfirmDelete(null)}>
          <div style={{
            background: 'var(--bg-secondary)', border: '1px solid var(--border)',
            borderRadius: 12, padding: 28, maxWidth: 420, width: '90%',
          }} onClick={e => e.stopPropagation()}>
            <h3 style={{ fontSize: 16, fontWeight: 700, marginBottom: 12, color: 'var(--text-primary)' }}>
              Delete configurator?
            </h3>
            <p style={{ fontSize: 13, color: 'var(--text-secondary)', marginBottom: 20 }}>
              <strong style={{ color: 'var(--text-primary)' }}>{confirmDelete.name}</strong>
              {confirmDelete.listing_count > 0
                ? ` is linked to ${confirmDelete.listing_count} listing${confirmDelete.listing_count !== 1 ? 's' : ''}. You will be asked to confirm before those links are removed.`
                : ' has no linked listings and will be permanently deleted.'}
            </p>
            <div style={{ display: 'flex', gap: 10, justifyContent: 'flex-end' }}>
              <button className="btn btn-secondary" onClick={() => setConfirmDelete(null)}>
                Cancel
              </button>
              <button
                className="btn btn-danger"
                onClick={() => handleDelete(confirmDelete)}
              >
                Delete
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

// ── Shared table cell styles ──────────────────────────────────────────────────

const thStyle: React.CSSProperties = {
  textAlign: 'left', padding: '10px 14px',
  fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase',
};

const tdStyle: React.CSSProperties = {
  padding: '10px 14px', fontSize: 13, verticalAlign: 'middle',
};

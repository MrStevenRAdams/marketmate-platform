// ============================================================================
// CONFIGURATOR SELECTOR — SESSION 3 (CFG-07, LT-01, LT-02, FLD-15)
// ============================================================================
// A reusable drop-in component inserted at the top of every channel listing
// create form. Fetches configurators filtered by channel, lets the user pick
// one, and emits the full ConfiguratorDetail object to the parent via onSelect.
//
// Parent responsibilities:
//   - Receive the ConfiguratorDetail and pre-populate form state.
//   - After a successful submit (for channels with MarketMate listing records),
//     call configuratorService.assignListings(cfg.configurator_id, [mm_listing_id]).
//
// LT-01: When a configurator is active an informational banner explains the
//         Update vs Revise distinction so users understand what "Configurator
//         Revise" means on the Configurators page vs editing this form.
// LT-02: Selecting a configurator is the first step of the implicit bulk
//         creation wizard — it populates category, attributes, and shipping
//         before the user fills per-listing details (title, price, SKU).
// FLD-15: If the selected configurator carries a variation_schema, the parent
//          receives it via the emitted ConfiguratorDetail object and can
//          pre-populate variation axis names accordingly.
// ============================================================================

import { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  configuratorService,
  ConfiguratorWithStats,
  ConfiguratorDetail,
} from '../../services/configurator-api';

// ── Styles (match ConfiguratorDetail / BulkReviseDialog conventions) ──────────

const cardStyle: React.CSSProperties = {
  background: 'var(--bg-secondary)',
  borderRadius: 12,
  border: '1px solid var(--border)',
  padding: '16px 20px',
  marginBottom: 16,
};

const labelStyle: React.CSSProperties = {
  display: 'block',
  fontSize: 11,
  fontWeight: 700,
  color: 'var(--text-muted)',
  textTransform: 'uppercase',
  letterSpacing: '0.5px',
  marginBottom: 6,
};

const selectStyle: React.CSSProperties = {
  width: '100%',
  padding: '10px 14px',
  borderRadius: 8,
  background: 'var(--bg-primary)',
  border: '1px solid var(--border-bright)',
  color: 'var(--text-primary)',
  fontSize: 14,
  outline: 'none',
  cursor: 'pointer',
};

// ── Props ─────────────────────────────────────────────────────────────────────

interface Props {
  /** Channel slug used to filter configurators — e.g. 'amazon', 'ebay', 'woocommerce'. */
  channel: string;
  /** Optional: used for display only (not filtered server-side by credential). */
  credentialId?: string;
  /** Called when the user picks or clears a configurator. null = "None" selected. */
  onSelect: (cfg: ConfiguratorDetail | null) => void;
}

// ── Component ─────────────────────────────────────────────────────────────────

export default function ConfiguratorSelector({ channel, onSelect }: Props) {
  const navigate = useNavigate();

  const [configurators, setConfigurators] = useState<ConfiguratorWithStats[]>([]);
  const [listLoading, setListLoading] = useState(true);
  const [listError, setListError] = useState('');

  const [selectedId, setSelectedId] = useState('');
  const [detailLoading, setDetailLoading] = useState(false);
  const [activeConfigurator, setActiveConfigurator] = useState<ConfiguratorDetail | null>(null);

  // ── Fetch configurators for this channel on mount ──────────────────────────
  useEffect(() => {
    let cancelled = false;
    setListLoading(true);
    setListError('');

    configuratorService
      .list({ channel })
      .then((res) => {
        if (!cancelled) setConfigurators(res.data.configurators || []);
      })
      .catch(() => {
        if (!cancelled) setListError('Could not load configurators.');
      })
      .finally(() => {
        if (!cancelled) setListLoading(false);
      });

    return () => { cancelled = true; };
  }, [channel]);

  // ── Handle dropdown selection ──────────────────────────────────────────────
  const handleChange = async (cfgId: string) => {
    setSelectedId(cfgId);

    if (!cfgId) {
      setActiveConfigurator(null);
      onSelect(null);
      return;
    }

    setDetailLoading(true);
    try {
      const res = await configuratorService.get(cfgId);
      const cfg = res.data.configurator;
      setActiveConfigurator(cfg);
      onSelect(cfg);
    } catch {
      setActiveConfigurator(null);
      onSelect(null);
    } finally {
      setDetailLoading(false);
    }
  };

  // ── Render ─────────────────────────────────────────────────────────────────

  if (listLoading) {
    return (
      <div style={{ ...cardStyle, display: 'flex', alignItems: 'center', gap: 10, color: 'var(--text-muted)', fontSize: 13 }}>
        <div
          style={{
            width: 14, height: 14,
            border: '2px solid var(--border)',
            borderTopColor: 'var(--primary)',
            borderRadius: '50%',
            animation: 'spin 1s linear infinite',
            flexShrink: 0,
          }}
        />
        Loading configurators…
      </div>
    );
  }

  if (listError) {
    return (
      <div style={{ ...cardStyle, color: 'var(--danger)', fontSize: 13 }}>
        ⚠️ {listError}
      </div>
    );
  }

  return (
    <div style={cardStyle}>
      {/* Header row */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 10 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <span style={{ fontSize: 16 }}>⚙️</span>
          <span style={{ fontSize: 14, fontWeight: 700, color: 'var(--text-primary)' }}>Configurator</span>
          <span style={{
            fontSize: 10, fontWeight: 700, letterSpacing: '0.5px', textTransform: 'uppercase',
            padding: '2px 6px', borderRadius: 4,
            background: 'rgba(99,102,241,0.12)', color: 'var(--primary)',
            border: '1px solid rgba(99,102,241,0.25)',
          }}>Optional</span>
        </div>
        <button
          onClick={() => navigate('/marketplace/configurators/new')}
          style={{ background: 'none', border: 'none', color: 'var(--primary)', cursor: 'pointer', fontSize: 12, fontWeight: 600, padding: 0 }}
        >
          + New Configurator
        </button>
      </div>

      {/* Dropdown or empty state */}
      {configurators.length === 0 ? (
        <div style={{ fontSize: 13, color: 'var(--text-muted)', padding: '8px 0' }}>
          No configurators set up for this channel yet.{' '}
          <button
            onClick={() => navigate('/marketplace/configurators/new')}
            style={{ background: 'none', border: 'none', color: 'var(--primary)', cursor: 'pointer', fontSize: 13, fontWeight: 600, padding: 0 }}
          >
            Create one →
          </button>
        </div>
      ) : (
        <>
          <label style={labelStyle}>Select a configurator to pre-populate category, attributes & shipping</label>
          <div style={{ position: 'relative' }}>
            <select
              value={selectedId}
              onChange={(e) => handleChange(e.target.value)}
              style={selectStyle}
              disabled={detailLoading}
            >
              <option value="">— None (start from scratch) —</option>
              {configurators.map((cfg) => (
                <option key={cfg.configurator_id} value={cfg.configurator_id}>
                  {cfg.name}
                  {cfg.category_path ? ` · ${cfg.category_path}` : ''}
                  {cfg.listing_count > 0 ? ` (${cfg.listing_count} listings)` : ''}
                </option>
              ))}
            </select>
            {detailLoading && (
              <div style={{
                position: 'absolute', right: 38, top: '50%', transform: 'translateY(-50%)',
                width: 14, height: 14,
                border: '2px solid var(--border)',
                borderTopColor: 'var(--primary)',
                borderRadius: '50%',
                animation: 'spin 1s linear infinite',
              }} />
            )}
          </div>
        </>
      )}

      {/* Active configurator summary + LT-01 informational banner */}
      {activeConfigurator && (
        <div style={{
          marginTop: 12,
          padding: '10px 14px',
          background: 'rgba(99,102,241,0.07)',
          border: '1px solid rgba(99,102,241,0.2)',
          borderRadius: 8,
          fontSize: 12,
          color: 'var(--text-secondary)',
        }}>
          <div style={{ display: 'flex', gap: 16, flexWrap: 'wrap', marginBottom: 6 }}>
            {activeConfigurator.category_path && (
              <span>📂 <strong>Category:</strong> {activeConfigurator.category_path}</span>
            )}
            {activeConfigurator.attribute_defaults && activeConfigurator.attribute_defaults.length > 0 && (
              <span>🏷 <strong>Attributes:</strong> {activeConfigurator.attribute_defaults.length} defaults applied</span>
            )}
            {activeConfigurator.variation_schema && activeConfigurator.variation_schema.length > 0 && (
              <span>🔀 <strong>Variation axes:</strong> {activeConfigurator.variation_schema.join(', ')}</span>
            )}
            {activeConfigurator.shipping_defaults && Object.keys(activeConfigurator.shipping_defaults).length > 0 && (
              <span>🚚 <strong>Shipping:</strong> defaults applied</span>
            )}
          </div>
          {/* LT-01: Update vs Revise distinction explanation */}
          <div style={{ color: 'var(--text-muted)', fontStyle: 'italic', borderTop: '1px solid rgba(99,102,241,0.15)', paddingTop: 6, marginTop: 4 }}>
            💡 Fields pre-populated from <strong>{activeConfigurator.name}</strong>. Changes you make here affect <em>only this listing</em>. To push updates to all {activeConfigurator.listing_count} linked listings, use{' '}
            <button
              onClick={() => navigate(`/marketplace/configurators/${activeConfigurator.configurator_id}`)}
              style={{ background: 'none', border: 'none', color: 'var(--primary)', cursor: 'pointer', fontSize: 12, fontWeight: 600, padding: 0, fontStyle: 'normal' }}
            >
              Configurator → Revise ↗
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

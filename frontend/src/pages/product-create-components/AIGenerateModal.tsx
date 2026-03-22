// ============================================================================
// AI LISTING GENERATE MODAL
// ============================================================================
// When user clicks a marketplace, this navigates to the listing page with
// ?ai=pending. The listing page handles: prepare → extract schema → call AI
// → overlay results. This modal is just a brief confirmation/redirect.
// ============================================================================

import { useNavigate } from 'react-router-dom';

interface Props {
  isOpen: boolean;
  productId: string;
  productTitle: string;
  channel: string;
  credentialId: string;
  onClose: () => void;
  onSkip: () => void;
}

const channelMeta: Record<string, { emoji: string; color: string; label: string }> = {
  amazon: { emoji: '📦', color: '#FF9900', label: 'Amazon' },
  ebay:   { emoji: '🏷️', color: '#E53238', label: 'eBay' },
  temu:   { emoji: '🛍️', color: '#FF6B35', label: 'Temu' },
  shopify:{ emoji: '🛒', color: '#96BF48', label: 'Shopify' },
  tiktok: { emoji: '🎵', color: '#00f2ea', label: 'TikTok Shop' },
  etsy:   { emoji: '🛍️', color: '#F1641E', label: 'Etsy' },
  woocommerce: { emoji: '🛒', color: '#7c3aed', label: 'WooCommerce' },
  magento: { emoji: '🏪', color: '#f97316', label: 'Magento 2' },
  bigcommerce: { emoji: '🛒', color: '#1C4EBF', label: 'BigCommerce' },
  onbuy: { emoji: '🏷️', color: '#E76119', label: 'OnBuy' },
  walmart:     { emoji: '🛒', color: '#0071ce', label: 'Walmart Marketplace' },
  kaufland:    { emoji: '🛒', color: '#e5002b', label: 'Kaufland' },
};

export default function AIGenerateModal({ isOpen, productId, productTitle, channel, credentialId, onClose, onSkip }: Props) {
  const navigate = useNavigate();

  if (!isOpen) return null;

  const meta = channelMeta[channel] || { emoji: '🌐', color: 'var(--primary)', label: channel };

  const handleAIGenerate = () => {
    const basePath = channel === 'temu'
      ? `/marketplace/listings/create/temu`
      : `/marketplace/listings/create/${channel}`;
    navigate(`${basePath}?product_id=${productId}&credential_id=${credentialId}&ai=pending`);
  };

  return (
    <div style={{
      position: 'fixed', top: 0, left: 0, right: 0, bottom: 0,
      background: 'rgba(0, 0, 0, 0.75)', zIndex: 2000,
      display: 'flex', alignItems: 'center', justifyContent: 'center',
      backdropFilter: 'blur(4px)',
    }} onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}>

      <div style={{
        background: 'var(--bg-secondary)', borderRadius: 16,
        border: '1px solid var(--border-bright)',
        width: '90%', maxWidth: 420, padding: 32,
        boxShadow: '0 24px 48px rgba(0, 0, 0, 0.5)',
        textAlign: 'center',
      }}>
        <div style={{
          width: 64, height: 64, borderRadius: '50%', margin: '0 auto 16px',
          background: `linear-gradient(135deg, ${meta.color}30, var(--accent-purple)30)`,
          border: `2px solid ${meta.color}60`,
          display: 'flex', alignItems: 'center', justifyContent: 'center',
        }}>
          <span style={{ fontSize: 28 }}>{meta.emoji}</span>
        </div>

        <h2 style={{ fontSize: 18, fontWeight: 700, marginBottom: 8 }}>
          Create {meta.label} Listing
        </h2>
        <p style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 6 }}>
          <strong style={{ color: 'var(--text-primary)' }}>{productTitle}</strong>
        </p>
        <p style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 24 }}>
          How would you like to create this listing?
        </p>

        <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
          <button onClick={handleAIGenerate}
            style={{
              padding: '12px 20px', borderRadius: 10, border: 'none', cursor: 'pointer',
              background: `linear-gradient(135deg, ${meta.color}, var(--accent-purple))`,
              color: '#fff', fontSize: 14, fontWeight: 600,
              display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 8,
            }}>
            🤖 AI Generate Listing
            <span style={{ fontSize: 11, opacity: 0.8 }}>Recommended</span>
          </button>
          <button onClick={onSkip}
            style={{
              padding: '10px 20px', borderRadius: 10, cursor: 'pointer',
              background: 'var(--bg-tertiary)', border: '1px solid var(--border-bright)',
              color: 'var(--text-secondary)', fontSize: 13,
            }}>
            Create Manually
          </button>
        </div>
      </div>
    </div>
  );
}

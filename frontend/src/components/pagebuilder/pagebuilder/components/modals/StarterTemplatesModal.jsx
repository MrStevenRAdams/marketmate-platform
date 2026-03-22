// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// STARTER TEMPLATES MODAL (Feature #31)
// Displays a grid of pre-built template starters. When the user
// selects one, the block tree is replaced with the starter's
// blocks, and canvas settings / theme are applied.
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

import React, { useState } from 'react';
import { X, FileText, ArrowRight } from 'lucide-react';
import { T } from '../../../constants/index.jsx';
import { css } from '../ui/index.jsx';
import { STARTER_TEMPLATES } from '../../../constants/index.jsx';
import { uid, deepClone } from '../../../utils/index.jsx';

/**
 * Recursively assigns fresh unique IDs to every block in a tree.
 * Must be called when loading a starter template so blocks don't
 * share IDs across multiple loads.
 */
function assignIds(block) {
  block.id = uid();
  if (block.children) {
    block.children.forEach(assignIds);
  }
  return block;
}

export default function StarterTemplatesModal({ onClose, onSelect, hasExistingBlocks }) {
  const [confirmIdx, setConfirmIdx] = useState(null);

  const handleSelect = (idx) => {
    if (hasExistingBlocks && confirmIdx !== idx) {
      setConfirmIdx(idx);
      return;
    }

    const tpl = STARTER_TEMPLATES[idx];
    const blocks = deepClone(tpl.blocks).map(assignIds);
    onSelect({
      blocks,
      canvas: { ...tpl.canvas },
      theme: tpl.theme,
      type: tpl.type,
      name: tpl.name,
    });
    onClose();
  };

  return (
    <div
      style={{
        position: 'fixed', inset: 0, zIndex: 1000,
        display: 'flex', alignItems: 'center', justifyContent: 'center',
        backgroundColor: 'rgba(0,0,0,0.6)',
        backdropFilter: 'blur(4px)',
      }}
      onClick={onClose}
      role="dialog"
      aria-modal="true"
      aria-label="Starter Templates"
    >
      <div
        style={{
          width: 720, maxWidth: '90vw', maxHeight: '80vh',
          backgroundColor: T.bg.secondary,
          border: `1px solid ${T.border.default}`,
          borderRadius: T.radius['2xl'],
          boxShadow: T.shadow.lg,
          display: 'flex', flexDirection: 'column',
          overflow: 'hidden',
        }}
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div style={{
          padding: '16px 20px',
          display: 'flex', alignItems: 'center', justifyContent: 'space-between',
          borderBottom: `1px solid ${T.border.default}`,
          backgroundColor: T.bg.tertiary,
        }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            <div style={{
              width: 32, height: 32, borderRadius: T.radius.lg,
              background: 'linear-gradient(135deg, #3b82f6, #8b5cf6)',
              display: 'flex', alignItems: 'center', justifyContent: 'center',
            }}>
              <FileText size={16} style={{ color: '#fff' }} />
            </div>
            <div>
              <div style={{ fontSize: 15, fontWeight: 600, color: T.text.primary }}>Starter Templates</div>
              <div style={{ fontSize: 11, color: T.text.muted }}>Choose a template to start designing</div>
            </div>
          </div>
          <button onClick={onClose} style={{ ...css.iconBtn }} aria-label="Close">
            <X size={18} />
          </button>
        </div>

        {/* Template grid */}
        <div style={{
          padding: 20, overflowY: 'auto', flex: 1,
          display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(200px, 1fr))',
          gap: 14, alignContent: 'start',
        }}>
          {STARTER_TEMPLATES.map((tpl, idx) => {
            const Icon = tpl.icon;
            const isConfirming = confirmIdx === idx;

            return (
              <div
                key={idx}
                style={{
                  backgroundColor: T.bg.elevated,
                  border: `1px solid ${isConfirming ? T.status.warning : T.border.default}`,
                  borderRadius: T.radius.xl,
                  padding: 16,
                  display: 'flex', flexDirection: 'column', gap: 10,
                  cursor: 'pointer',
                  transition: 'all 200ms ease-in-out',
                }}
                onMouseEnter={(e) => {
                  e.currentTarget.style.borderColor = T.primary.base;
                  e.currentTarget.style.boxShadow = `0 0 0 1px ${T.primary.base}, ${T.shadow.md}`;
                }}
                onMouseLeave={(e) => {
                  e.currentTarget.style.borderColor = isConfirming ? T.status.warning : T.border.default;
                  e.currentTarget.style.boxShadow = 'none';
                }}
                onClick={() => handleSelect(idx)}
              >
                {/* Icon */}
                <div style={{
                  width: 44, height: 44, borderRadius: T.radius.lg,
                  backgroundColor: T.bg.tertiary,
                  display: 'flex', alignItems: 'center', justifyContent: 'center',
                }}>
                  <Icon size={22} style={{ color: T.primary.light }} />
                </div>

                {/* Name & description */}
                <div>
                  <div style={{ fontSize: 14, fontWeight: 600, color: T.text.primary, marginBottom: 3 }}>{tpl.name}</div>
                  <div style={{ fontSize: 11, color: T.text.muted, lineHeight: 1.4 }}>{tpl.description}</div>
                </div>

                {/* Type badge */}
                <div style={{
                  alignSelf: 'flex-start',
                  padding: '2px 8px', borderRadius: T.radius.sm,
                  backgroundColor: T.bg.tertiary,
                  fontSize: 10, fontWeight: 600, color: T.text.secondary,
                  textTransform: 'uppercase', letterSpacing: '0.04em',
                }}>
                  {tpl.type.replace('_', ' ')}
                </div>

                {/* CTA */}
                {isConfirming ? (
                  <div style={{
                    display: 'flex', alignItems: 'center', gap: 6,
                    padding: '6px 12px', borderRadius: T.radius.md,
                    backgroundColor: 'rgba(245, 158, 11, 0.15)',
                    border: `1px solid rgba(245, 158, 11, 0.3)`,
                    fontSize: 11, fontWeight: 600, color: T.status.warning,
                  }}>
                    Replace current design? Click again to confirm
                  </div>
                ) : (
                  <div style={{
                    display: 'flex', alignItems: 'center', gap: 6,
                    fontSize: 12, fontWeight: 500, color: T.primary.base,
                    marginTop: 'auto',
                  }}>
                    Use Template <ArrowRight size={13} />
                  </div>
                )}
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}

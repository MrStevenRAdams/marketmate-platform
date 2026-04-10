// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// CONTEXT MENU (Feature #14)
// Right-click menu shown on canvas blocks. Supports dividers,
// keyboard shortcut hints, disabled items, and danger styling.
// Auto-closes on any click or right-click anywhere.
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

import React, { useEffect } from 'react';
import { T } from '../../../constants/index.jsx';

/**
 * @param {number}   x        — CSS left position
 * @param {number}   y        — CSS top position
 * @param {Function} onClose  — Called when the menu should dismiss
 * @param {Array}    actions  — Menu items (or { divider: true } for separators)
 *
 * Each action: { label, icon, onClick, shortcut?, disabled?, danger? }
 */
export default function ContextMenu({ x, y, onClose, actions }) {
  // Close on any click or right-click anywhere on the page
  useEffect(() => {
    const handler = () => onClose();
    window.addEventListener('click', handler);
    window.addEventListener('contextmenu', handler);
    return () => {
      window.removeEventListener('click', handler);
      window.removeEventListener('contextmenu', handler);
    };
  }, [onClose]);

  return (
    <div style={{
      position: 'fixed', left: x, top: y, zIndex: 2000,
      backgroundColor: T.bg.elevated,
      border: `1px solid ${T.border.bright}`,
      borderRadius: T.radius.lg,
      boxShadow: T.shadow.lg,
      padding: '4px 0',
      minWidth: 180,
    }} role="menu" aria-label="Block actions">
      {actions.map((action, i) => {
        // Divider
        if (action.divider) {
          return (
            <div
              key={i}
              style={{ height: 1, backgroundColor: T.border.default, margin: '4px 0' }}
            />
          );
        }

        // Menu item
        return (
          <button
            key={i}
            role="menuitem"
            onClick={(e) => {
              e.stopPropagation();
              action.onClick();
              onClose();
            }}
            disabled={action.disabled}
            style={{
              width: '100%', padding: '8px 16px',
              border: 'none', backgroundColor: 'transparent',
              color: action.danger ? T.status.danger : action.disabled ? T.text.muted : T.text.primary,
              fontSize: 12, fontFamily: T.font,
              cursor: action.disabled ? 'default' : 'pointer',
              textAlign: 'left',
              display: 'flex', alignItems: 'center', gap: 10,
              opacity: action.disabled ? 0.4 : 1,
              transition: 'background-color 100ms',
            }}
            onMouseEnter={(e) => { if (!action.disabled) e.currentTarget.style.backgroundColor = T.bg.tertiary; }}
            onMouseLeave={(e) => { e.currentTarget.style.backgroundColor = 'transparent'; }}
          >
            {action.icon && <action.icon size={13} />}
            <span>{action.label}</span>
            {action.shortcut && (
              <span style={{ marginLeft: 'auto', fontSize: 10, color: T.text.muted }}>
                {action.shortcut}
              </span>
            )}
          </button>
        );
      })}
    </div>
  );
}

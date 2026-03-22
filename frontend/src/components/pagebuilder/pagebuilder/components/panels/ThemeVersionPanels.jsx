// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// THEME PICKER & VERSION PANEL
// Left sidebar tabs for theme selection and version history.
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

import React from 'react';
import { Check } from 'lucide-react';
import { T, THEME_PRESETS } from '../../../constants/index.jsx';

// ── Theme Picker ───────────────────────────────────────────────

export function ThemePicker({ currentTheme, onSelect }) {
  return (
    <div style={{ padding: 12 }}>
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 6 }}>
        {Object.entries(THEME_PRESETS).map(([key, theme]) => {
          const isActive = key === currentTheme;
          return (
            <button
              key={key}
              onClick={() => onSelect(key)}
              style={{
                padding: '10px 8px',
                backgroundColor: isActive ? T.bg.elevated : T.bg.tertiary,
                border: `1px solid ${isActive ? T.primary.base : T.border.default}`,
                borderRadius: T.radius.lg,
                cursor: 'pointer',
                display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 6,
                transition: 'all 150ms',
                fontFamily: T.font,
                boxShadow: isActive ? `0 0 8px ${T.primary.glow}` : 'none',
              }}
            >
              {/* Colour swatches */}
              <div style={{ display: 'flex', gap: 3 }}>
                {theme.preview.map((c, i) => (
                  <div key={i} style={{
                    width: 14, height: 14, borderRadius: '50%',
                    backgroundColor: c,
                    border: '1px solid rgba(255,255,255,0.1)',
                  }} />
                ))}
              </div>
              <span style={{ fontSize: 10, fontWeight: 600, color: isActive ? T.primary.light : T.text.secondary }}>
                {theme.name}
              </span>
              {isActive && <Check size={10} style={{ color: T.primary.base }} />}
            </button>
          );
        })}
      </div>
    </div>
  );
}

// ── Version History Panel ──────────────────────────────────────

export function VersionPanel({ versions, currentVersion }) {
  return (
    <div style={{ padding: 12 }}>
      {versions.length === 0 && (
        <div style={{ padding: 12, textAlign: 'center', color: T.text.muted, fontSize: 12 }}>
          No saves yet
        </div>
      )}
      {versions.map((v, i) => (
        <div
          key={i}
          style={{
            padding: '8px 12px', marginBottom: 4,
            borderRadius: T.radius.md,
            backgroundColor: v.version === currentVersion ? T.bg.elevated : 'transparent',
            border: `1px solid ${v.version === currentVersion ? T.primary.base : 'transparent'}`,
            display: 'flex', alignItems: 'center', gap: 8, fontSize: 12,
          }}
        >
          {/* Version badge */}
          <div style={{
            width: 24, height: 24, borderRadius: '50%',
            backgroundColor: T.bg.tertiary,
            display: 'flex', alignItems: 'center', justifyContent: 'center',
            fontSize: 10, fontWeight: 700, color: T.text.secondary, flexShrink: 0,
          }}>
            v{v.version}
          </div>
          <div style={{ flex: 1 }}>
            <div style={{ color: T.text.primary, fontWeight: 500, fontSize: 11 }}>Version {v.version}</div>
            <div style={{ color: T.text.muted, fontSize: 10 }}>{v.savedAt}</div>
          </div>
          <div style={{ fontSize: 10, color: T.text.muted }}>{v.blockCount} blocks</div>
        </div>
      ))}
    </div>
  );
}

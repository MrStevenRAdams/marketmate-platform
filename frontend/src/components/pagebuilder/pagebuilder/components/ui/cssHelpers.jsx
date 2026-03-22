// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// CSS HELPERS
// Reusable inline-style objects for common builder UI patterns.
// Using these keeps the component code cleaner and ensures
// consistent styling across panels, buttons, inputs, etc.
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

import { T } from '../../../constants/index.jsx';

const css = {
  // ── Panel (sidebar container) ────────────────────────────────
  panel: {
    backgroundColor: T.bg.secondary,
    borderRight:     `1px solid ${T.border.default}`,
    display:         'flex',
    flexDirection:   'column',
    overflow:        'hidden',
  },

  // ── Panel section header ─────────────────────────────────────
  panelHeader: {
    padding:         '12px 16px',
    backgroundColor: T.bg.tertiary,
    borderBottom:    `1px solid ${T.border.default}`,
    fontSize:        11,
    fontWeight:      600,
    textTransform:   'uppercase',
    letterSpacing:   '0.05em',
    color:           T.text.secondary,
    display:         'flex',
    alignItems:      'center',
    gap:             8,
  },

  // ── Button (primary or secondary variant) ────────────────────
  btn: (variant = 'secondary') => ({
    display:        'inline-flex',
    alignItems:     'center',
    justifyContent: 'center',
    gap:            6,
    padding:        '6px 12px',
    borderRadius:   T.radius.md,
    fontSize:       13,
    fontWeight:     500,
    cursor:         'pointer',
    border:         'none',
    transition:     'all 150ms ease-in-out',
    fontFamily:     T.font,
    ...(variant === 'primary'
      ? {
          background: 'linear-gradient(135deg, #3b82f6, #8b5cf6)',
          color:      '#fff',
          boxShadow:  T.shadow.glow,
        }
      : {
          background: T.bg.elevated,
          color:      T.text.primary,
          border:     `1px solid ${T.border.bright}`,
        }
    ),
  }),

  // ── Icon button (square, no background) ──────────────────────
  iconBtn: {
    display:        'inline-flex',
    alignItems:     'center',
    justifyContent: 'center',
    width:          32,
    height:         32,
    borderRadius:   T.radius.md,
    border:         'none',
    background:     'transparent',
    color:          T.text.secondary,
    cursor:         'pointer',
    transition:     'all 150ms ease-in-out',
    padding:        0,
  },

  // ── Text input ───────────────────────────────────────────────
  input: {
    width:           '100%',
    padding:         '6px 10px',
    backgroundColor: T.bg.tertiary,
    border:          `1px solid ${T.border.bright}`,
    borderRadius:    T.radius.md,
    color:           T.text.primary,
    fontSize:        13,
    fontFamily:      T.font,
    outline:         'none',
    transition:      'border-color 200ms ease-in-out, box-shadow 200ms ease-in-out',
    boxSizing:       'border-box',
  },

  // ── Label (above inputs) ─────────────────────────────────────
  label: {
    fontSize:      11,
    fontWeight:    600,
    color:         T.text.secondary,
    textTransform: 'uppercase',
    letterSpacing: '0.03em',
    marginBottom:  4,
    display:       'block',
  },

  // ── Select dropdown ──────────────────────────────────────────
  select: {
    width:           '100%',
    padding:         '6px 10px',
    backgroundColor: T.bg.tertiary,
    border:          `1px solid ${T.border.bright}`,
    borderRadius:    T.radius.md,
    color:           T.text.primary,
    fontSize:        13,
    fontFamily:      T.font,
    outline:         'none',
    cursor:          'pointer',
    appearance:      'none',
    boxSizing:       'border-box',
  },
};

export default css;

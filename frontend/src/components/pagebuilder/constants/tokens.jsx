// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// DESIGN TOKENS
// Dark fintech theme — single source of truth for all styling.
// Every colour, radius, shadow, and font value used across the
// page builder lives here so the theme can be adjusted in one place.
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

const T = {
  // ── Backgrounds ──────────────────────────────────────────────
  bg: {
    primary:   '#0a0e27',   // App background
    secondary: '#131829',   // Panels, cards, sidebar
    tertiary:  '#1a1f3a',   // Hover states, headers, elevated sections
    elevated:  '#1e2442',   // Active items, modals, popovers
  },

  // ── Brand colours ────────────────────────────────────────────
  primary: {
    base:  '#3b82f6',                    // Main actions
    dark:  '#2563eb',                    // Pressed states
    light: '#60a5fa',                    // Highlights
    glow:  'rgba(59, 130, 246, 0.2)',    // Focus rings, subtle glow
  },

  // ── Accent palette ───────────────────────────────────────────
  accent: {
    cyan:   '#06b6d4',
    purple: '#8b5cf6',
    teal:   '#14b8a6',
    orange: '#f97316',
  },

  // ── Status colours ───────────────────────────────────────────
  status: {
    success: '#10b981',
    warning: '#f59e0b',
    danger:  '#ef4444',
    info:    '#06b6d4',
  },

  // ── Borders ──────────────────────────────────────────────────
  border: {
    default: '#2d3548',
    bright:  '#3d4557',
  },

  // ── Text ─────────────────────────────────────────────────────
  text: {
    primary:   '#e2e8f0',
    secondary: '#94a3b8',
    muted:     '#64748b',
  },

  // ── Typography ───────────────────────────────────────────────
  font: "'Segoe UI', system-ui, -apple-system, sans-serif",

  // ── Radii ────────────────────────────────────────────────────
  radius: {
    sm:   4,
    md:   6,
    lg:   8,
    xl:   12,
    '2xl': 16,
  },

  // ── Shadows ──────────────────────────────────────────────────
  shadow: {
    sm:   '0 1px 2px 0 rgba(0,0,0,0.3)',
    md:   '0 4px 6px -1px rgba(0,0,0,0.4)',
    lg:   '0 10px 15px -3px rgba(0,0,0,0.5)',
    glow: '0 0 20px rgba(59, 130, 246, 0.4)',
  },
};

export default T;

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// THEME PRESETS
// These themes control the appearance of the *designed document*,
// not the builder UI itself. Each theme provides a set of CSS-like
// variables (bg, text, accent, etc.) that blocks read at render time.
//
// `preview` — three colour swatches shown in the theme picker.
// `vars`    — the token map consumed by CanvasBlock and serialisers.
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

const THEME_PRESETS = {
  default: {
    name: 'Default',
    preview: ['#ffffff', '#333333', '#3b82f6'],
    vars: {
      bg:            '#ffffff',
      text:          '#333333',
      heading:       '#111827',
      accent:        '#3b82f6',
      accentLight:   '#eff6ff',
      border:        '#e5e7eb',
      muted:         '#6b7280',
      headerBg:      '#f9fafb',
      tableBorder:   '#e5e7eb',
      tableHeaderBg: '#f3f4f6',
    },
  },

  midnight: {
    name: 'Midnight',
    preview: ['#1e293b', '#e2e8f0', '#8b5cf6'],
    vars: {
      bg:            '#1e293b',
      text:          '#e2e8f0',
      heading:       '#f8fafc',
      accent:        '#8b5cf6',
      accentLight:   '#2e1065',
      border:        '#475569',
      muted:         '#94a3b8',
      headerBg:      '#0f172a',
      tableBorder:   '#475569',
      tableHeaderBg: '#334155',
    },
  },

  ocean: {
    name: 'Ocean',
    preview: ['#f0fdfa', '#134e4a', '#0d9488'],
    vars: {
      bg:            '#f0fdfa',
      text:          '#134e4a',
      heading:       '#042f2e',
      accent:        '#0d9488',
      accentLight:   '#ccfbf1',
      border:        '#99f6e4',
      muted:         '#5eead4',
      headerBg:      '#ccfbf1',
      tableBorder:   '#5eead4',
      tableHeaderBg: '#f0fdfa',
    },
  },

  coral: {
    name: 'Coral',
    preview: ['#fff7ed', '#9a3412', '#ea580c'],
    vars: {
      bg:            '#fff7ed',
      text:          '#9a3412',
      heading:       '#7c2d12',
      accent:        '#ea580c',
      accentLight:   '#ffedd5',
      border:        '#fed7aa',
      muted:         '#fb923c',
      headerBg:      '#ffedd5',
      tableBorder:   '#fed7aa',
      tableHeaderBg: '#fff7ed',
    },
  },

  mono: {
    name: 'Mono',
    preview: ['#fafafa', '#171717', '#525252'],
    vars: {
      bg:            '#fafafa',
      text:          '#171717',
      heading:       '#0a0a0a',
      accent:        '#525252',
      accentLight:   '#f5f5f5',
      border:        '#d4d4d4',
      muted:         '#737373',
      headerBg:      '#f5f5f5',
      tableBorder:   '#d4d4d4',
      tableHeaderBg: '#e5e5e5',
    },
  },

  royal: {
    name: 'Royal',
    preview: ['#faf5ff', '#581c87', '#9333ea'],
    vars: {
      bg:            '#faf5ff',
      text:          '#581c87',
      heading:       '#3b0764',
      accent:        '#9333ea',
      accentLight:   '#f3e8ff',
      border:        '#d8b4fe',
      muted:         '#a855f7',
      headerBg:      '#f3e8ff',
      tableBorder:   '#d8b4fe',
      tableHeaderBg: '#faf5ff',
    },
  },
};

export default THEME_PRESETS;

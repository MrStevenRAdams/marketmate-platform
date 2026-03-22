// Design System - Dark FinTech Theme
// This file contains all design tokens (colors, spacing, typography, etc.)
// Use these tokens consistently across all modules

export const designTokens = {
  // Color Palette
  colors: {
    // Backgrounds
    bgPrimary: '#0a0e27',
    bgSecondary: '#131829',
    bgTertiary: '#1a1f3a',
    bgElevated: '#1e2442',
    
    // Primary Brand Colors
    primary: '#3b82f6',
    primaryDark: '#2563eb',
    primaryLight: '#60a5fa',
    primaryGlow: 'rgba(59, 130, 246, 0.2)',
    
    // Accent Colors
    accentCyan: '#06b6d4',
    accentPurple: '#8b5cf6',
    accentTeal: '#14b8a6',
    accentOrange: '#f97316',
    
    // Status Colors
    success: '#10b981',
    successGlow: 'rgba(16, 185, 129, 0.2)',
    warning: '#f59e0b',
    warningGlow: 'rgba(245, 158, 11, 0.2)',
    danger: '#ef4444',
    dangerGlow: 'rgba(239, 68, 68, 0.2)',
    info: '#06b6d4',
    infoGlow: 'rgba(6, 182, 212, 0.2)',
    
    // Borders
    border: '#2d3548',
    borderBright: '#3d4557',
    borderFocus: '#3b82f6',
    
    // Text
    textPrimary: '#e2e8f0',
    textSecondary: '#94a3b8',
    textMuted: '#64748b',
    textInverse: '#0a0e27',
  },

  // Typography
  typography: {
    fontFamily: {
      sans: "'Segoe UI', system-ui, -apple-system, BlinkMacSystemFont, sans-serif",
      mono: "'Courier New', 'Consolas', monospace",
    },
    fontSize: {
      xs: '11px',
      sm: '13px',
      base: '14px',
      lg: '16px',
      xl: '18px',
      '2xl': '20px',
      '3xl': '24px',
      '4xl': '32px',
    },
    fontWeight: {
      normal: 400,
      medium: 500,
      semibold: 600,
      bold: 700,
    },
    lineHeight: {
      tight: 1.25,
      normal: 1.5,
      relaxed: 1.75,
    },
  },

  // Spacing (px values)
  spacing: {
    xs: '4px',
    sm: '8px',
    md: '12px',
    lg: '16px',
    xl: '20px',
    '2xl': '24px',
    '3xl': '32px',
    '4xl': '48px',
    '5xl': '64px',
  },

  // Border Radius
  borderRadius: {
    sm: '4px',
    md: '6px',
    lg: '8px',
    xl: '12px',
    '2xl': '16px',
    full: '9999px',
  },

  // Shadows
  shadows: {
    sm: '0 1px 2px 0 rgba(0, 0, 0, 0.3)',
    md: '0 4px 6px -1px rgba(0, 0, 0, 0.4)',
    lg: '0 10px 15px -3px rgba(0, 0, 0, 0.5)',
    xl: '0 20px 25px -5px rgba(0, 0, 0, 0.6)',
    glow: {
      primary: '0 0 20px rgba(59, 130, 246, 0.4)',
      success: '0 0 20px rgba(16, 185, 129, 0.4)',
      warning: '0 0 20px rgba(245, 158, 11, 0.4)',
      danger: '0 0 20px rgba(239, 68, 68, 0.4)',
    },
    none: 'none',
  },

  // Transitions
  transitions: {
    fast: '150ms ease-in-out',
    base: '200ms ease-in-out',
    slow: '300ms ease-in-out',
  },

  // Z-index
  zIndex: {
    dropdown: 1000,
    sticky: 1020,
    modal: 1030,
    popover: 1040,
    tooltip: 1050,
  },

  // Breakpoints (for responsive design)
  breakpoints: {
    sm: '640px',
    md: '768px',
    lg: '1024px',
    xl: '1280px',
    '2xl': '1536px',
  },
};

// Helper function to use in styled components or inline styles
export const getColor = (colorPath) => {
  const keys = colorPath.split('.');
  let value = designTokens.colors;
  for (const key of keys) {
    value = value[key];
  }
  return value;
};

export const getSpacing = (size) => designTokens.spacing[size];
export const getRadius = (size) => designTokens.borderRadius[size];
export const getShadow = (type) => designTokens.shadows[type];

export default designTokens;

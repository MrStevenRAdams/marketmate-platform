// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// SMALL UI COMPONENTS
// Reusable form controls used in the property editor and canvas
// settings panel. Each wraps a native input with consistent styling.
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

import React, { useState } from 'react';
import { ChevronDown, ChevronRight, Tag, ToggleLeft, ToggleRight } from 'lucide-react';
import { T, FIELD_CATEGORIES, CATEGORY_ICONS } from '../../../constants/index.jsx';
import css from './cssHelpers';

// ── Colour input with swatch + text field ──────────────────────

export function ColorInput({ value, onChange, label }) {
  return (
    <div style={{ marginBottom: 10 }}>
      {label && <span style={css.label}>{label}</span>}
      <div style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
        {/* Colour swatch with hidden native picker */}
        <div style={{
          width: 28, height: 28,
          borderRadius: T.radius.sm,
          border: `1px solid ${T.border.bright}`,
          backgroundColor: value || '#ffffff',
          cursor: 'pointer',
          flexShrink: 0,
          position: 'relative',
          overflow: 'hidden',
        }}>
          <input
            type="color"
            value={value || '#ffffff'}
            onChange={(e) => onChange(e.target.value)}
            style={{ position: 'absolute', inset: -4, width: 40, height: 40, cursor: 'pointer', opacity: 0 }}
          />
        </div>
        {/* Hex text input */}
        <input
          style={{ ...css.input, flex: 1 }}
          value={value || ''}
          placeholder="#000000"
          onChange={(e) => onChange(e.target.value)}
        />
      </div>
    </div>
  );
}

// ── Generic property input (text, number, select, textarea, toggle) ──

export function PropInput({ label, value, onChange, type = 'text', options, placeholder }) {
  // Select dropdown
  if (type === 'select') {
    return (
      <div style={{ marginBottom: 10 }}>
        <span style={css.label}>{label}</span>
        <select
          style={css.select}
          value={value || ''}
          onChange={(e) => onChange(e.target.value)}
        >
          {options.map((o) => (
            <option key={o.value ?? o} value={o.value ?? o}>
              {o.label ?? o}
            </option>
          ))}
        </select>
      </div>
    );
  }

  // Multi-line text area
  if (type === 'textarea') {
    return (
      <div style={{ marginBottom: 10 }}>
        <span style={css.label}>{label}</span>
        <textarea
          style={{ ...css.input, minHeight: 72, resize: 'vertical' }}
          value={value || ''}
          onChange={(e) => onChange(e.target.value)}
          placeholder={placeholder}
        />
      </div>
    );
  }

  // Boolean toggle
  if (type === 'toggle') {
    return (
      <div style={{ marginBottom: 10, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <span style={{ ...css.label, marginBottom: 0 }}>{label}</span>
        <button
          onClick={() => onChange(!value)}
          style={{ ...css.iconBtn, color: value ? T.primary.base : T.text.muted }}
        >
          {value ? <ToggleRight size={20} /> : <ToggleLeft size={20} />}
        </button>
      </div>
    );
  }

  // Default: text or number input
  return (
    <div style={{ marginBottom: 10 }}>
      <span style={css.label}>{label}</span>
      <input
        style={css.input}
        type={type}
        value={value ?? ''}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
      />
    </div>
  );
}

// ── Dynamic field picker (dropdown with categorised merge tags) ──

export function FieldPicker({ value, onChange }) {
  const [open, setOpen] = useState(false);
  const [expandedCat, setExpandedCat] = useState(null);

  return (
    <div style={{ marginBottom: 10, position: 'relative' }}>
      <span style={css.label}>Field Path</span>

      {/* Trigger button */}
      <button
        onClick={() => setOpen(!open)}
        style={{
          ...css.input,
          textAlign: 'left',
          cursor: 'pointer',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
        }}
      >
        <span style={{ color: value ? T.text.primary : T.text.muted }}>
          {value ? `{{${value}}}` : 'Select field...'}
        </span>
        <ChevronDown size={14} style={{ color: T.text.muted }} />
      </button>

      {/* Dropdown panel */}
      {open && (
        <div style={{
          position: 'absolute', top: '100%', left: 0, right: 0, zIndex: 100,
          backgroundColor: T.bg.elevated,
          border: `1px solid ${T.border.bright}`,
          borderRadius: T.radius.lg,
          boxShadow: T.shadow.lg,
          marginTop: 4,
          maxHeight: 300, overflowY: 'auto',
        }}>
          {Object.entries(FIELD_CATEGORIES).map(([cat, fields]) => {
            const Icon = CATEGORY_ICONS[cat] || Tag;
            const isExpanded = expandedCat === cat;

            return (
              <div key={cat}>
                {/* Category header */}
                <button
                  onClick={() => setExpandedCat(isExpanded ? null : cat)}
                  style={{
                    width: '100%', padding: '8px 12px',
                    border: 'none', backgroundColor: 'transparent',
                    color: T.text.primary, fontSize: 12, fontWeight: 600,
                    fontFamily: T.font,
                    display: 'flex', alignItems: 'center', gap: 8,
                    cursor: 'pointer',
                    borderBottom: `1px solid ${T.border.default}`,
                  }}
                >
                  <Icon size={14} style={{ color: T.text.muted }} />
                  {cat}
                  <ChevronRight
                    size={12}
                    style={{
                      marginLeft: 'auto',
                      color: T.text.muted,
                      transform: isExpanded ? 'rotate(90deg)' : 'none',
                      transition: 'transform 150ms',
                    }}
                  />
                </button>

                {/* Field list */}
                {isExpanded && fields.map((f) => (
                  <button
                    key={f.path}
                    onClick={() => { onChange(f.path); setOpen(false); }}
                    style={{
                      width: '100%', padding: '6px 12px 6px 36px',
                      border: 'none',
                      backgroundColor: f.path === value ? T.primary.glow : 'transparent',
                      color: f.path === value ? T.primary.light : T.text.secondary,
                      fontSize: 12, fontFamily: T.font,
                      cursor: 'pointer', textAlign: 'left',
                      display: 'flex', alignItems: 'center', gap: 6,
                    }}
                  >
                    <Tag size={10} />
                    {f.label}
                    <span style={{ marginLeft: 'auto', fontSize: 10, color: T.text.muted, fontFamily: 'monospace' }}>
                      {f.path}
                    </span>
                  </button>
                ))}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

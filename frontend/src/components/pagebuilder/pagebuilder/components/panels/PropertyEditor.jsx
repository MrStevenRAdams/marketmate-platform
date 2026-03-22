// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// PROPERTY EDITOR
// Right sidebar panel shown when a block is selected. Displays
// type-specific property controls plus shared controls that apply
// to all block types.
//
// SESSION 2 ADDITIONS:
//   • Image Upload button (uploads to /api/templates/upload-image)
//   • Font Family picker for text / variable blocks
//   • Bold / Italic / Underline toggles
//   • Text colour, text alignment, background colour, border colour
//   • Border width, border style, border radius, padding (T/R/B/L)
//   • Opacity slider, rotation input (all blocks)
//   • Keep aspect ratio + alt text (image)
//   • Barcode data source toggle + FieldPicker
//   • Line thickness + colour (divider)
//   • Table: header bg, column widths, alternating row colour
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

import React, { useState, useRef } from 'react';
import {
  Square, AlignLeft, AlignCenter, AlignRight, AlignJustify, Maximize2,
  Sparkles, Repeat, Scissors, Filter, MousePointer,
  Italic, Underline, Strikethrough, Plus, Minus, Link,
  Bold, Upload, RotateCw,
} from 'lucide-react';
import { T, BLOCK_TYPES } from '../../../constants/index.jsx';
import { uid } from '../../../utils/index.jsx';
import { css, ColorInput, PropInput, FieldPicker, ConditionsEditor } from '../ui/index.jsx';

// ── Font family options ──────────────────────────────────────────
const FONT_FAMILIES = [
  { value: "'Segoe UI',system-ui,sans-serif", label: 'Segoe UI (default)' },
  { value: 'Arial,sans-serif',                label: 'Arial' },
  { value: "'Times New Roman',serif",         label: 'Times New Roman' },
  { value: "'Courier New',monospace",         label: 'Courier New' },
  { value: 'Georgia,serif',                   label: 'Georgia' },
  { value: 'Verdana,sans-serif',              label: 'Verdana' },
  { value: 'Helvetica,sans-serif',            label: 'Helvetica' },
  { value: 'Roboto,sans-serif',               label: 'Roboto' },
];

// ── Text-alignment button group ─────────────────────────────────
const ALIGN_OPTS = [
  { icon: AlignLeft,    val: 'left',    label: 'Align left' },
  { icon: AlignCenter,  val: 'center',  label: 'Center' },
  { icon: AlignRight,   val: 'right',   label: 'Right' },
  { icon: AlignJustify, val: 'justify', label: 'Justify' },
];

function AlignButtons({ value, onChange }) {
  return (
    <div style={{ display: 'flex', gap: 4, marginBottom: 10 }}>
      {ALIGN_OPTS.map((a) => (
        <button
          key={a.val}
          onClick={() => onChange(a.val)}
          aria-label={a.label}
          aria-pressed={value === a.val}
          style={{
            ...css.iconBtn,
            backgroundColor: value === a.val ? T.primary.glow : 'transparent',
            color: value === a.val ? T.primary.base : T.text.muted,
            border: `1px solid ${value === a.val ? T.primary.base : T.border.default}`,
          }}
        >
          <a.icon size={14} />
        </button>
      ))}
    </div>
  );
}

// -- Conditional Styles Editor (Session 3) --
function ConditionalStylesEditor({ entries, onChange }) {
  const addEntry = () => onChange([...(entries || []), {
    id: Math.random().toString(36).slice(2),
    label: 'Style Rule',
    logic: 'and',
    conditions: [],
    styles: { backgroundColor: '', color: '', borderColor: '' },
  }]);
  const updateEntry = (idx, patch) => onChange((entries || []).map((e, i) => i === idx ? { ...e, ...patch } : e));
  const removeEntry = (idx) => onChange((entries || []).filter((_, i) => i !== idx));
  return (
    <div style={{ marginBottom: 10 }}>
      {(entries || []).map((entry, idx) => (
        <div key={entry.id || idx} style={{ border: `1px solid ${T.border.default}`, borderRadius: T.radius.md, padding: 8, marginBottom: 8, background: T.bg.tertiary }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 4, marginBottom: 6 }}>
            <input style={{ ...css.input, flex: 1, fontSize: 11 }} value={entry.label || ''} onChange={(e) => updateEntry(idx, { label: e.target.value })} placeholder="Rule label" />
            <button onClick={() => removeEntry(idx)} style={{ ...css.iconBtn, color: T.status.danger }}><Minus size={11} /></button>
          </div>
          <ConditionsEditor conditions={entry.conditions || []} onChange={(conds) => updateEntry(idx, { conditions: conds })} />
          <div style={{ marginTop: 6 }}>
            <div style={{ fontSize: 10, color: T.text.muted, marginBottom: 4 }}>Style Overrides (applied when conditions match)</div>
            <ColorInput label="Background Color" value={entry.styles && entry.styles.backgroundColor || ''} onChange={(v) => updateEntry(idx, { styles: Object.assign({}, entry.styles, { backgroundColor: v }) })} />
            <ColorInput label="Text Color" value={entry.styles && entry.styles.color || ''} onChange={(v) => updateEntry(idx, { styles: Object.assign({}, entry.styles, { color: v }) })} />
            <ColorInput label="Border Color" value={entry.styles && entry.styles.borderColor || ''} onChange={(v) => updateEntry(idx, { styles: Object.assign({}, entry.styles, { borderColor: v }) })} />
          </div>
        </div>
      ))}
      <button onClick={addEntry} style={{ ...css.btn('secondary'), width: '100%', fontSize: 11, padding: '4px 8px' }}>
        <Plus size={11} /> Add Style Rule
      </button>
    </div>
  );
}

// ── 4-way padding inputs ─────────────────────────────────────────
function PaddingInputs({ label = 'Padding', value = '', onChange }) {
  // Parse "8px 12px 8px 12px" or "8px" into [t, r, b, l]
  const parts = String(value || '').trim().split(/\s+/);
  const t = parts[0] || '';
  const r = parts[1] || parts[0] || '';
  const b = parts[2] || parts[0] || '';
  const l = parts[3] || parts[1] || parts[0] || '';

  const emit = (nt, nr, nb, nl) => onChange(`${nt} ${nr} ${nb} ${nl}`);

  const inp = (val, placeholder, onC) => (
    <input
      style={{ ...css.input, textAlign: 'center', padding: '4px 2px', fontSize: 11, width: '100%' }}
      value={val}
      onChange={(e) => onC(e.target.value)}
      placeholder={placeholder}
    />
  );

  return (
    <div style={{ marginBottom: 10 }}>
      <div style={{ fontSize: 10, color: T.text.muted, marginBottom: 4 }}>{label}</div>
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr 1fr', gap: 4 }}>
        {inp(t, 'T', (v) => emit(v, r, b, l))}
        {inp(r, 'R', (v) => emit(t, v, b, l))}
        {inp(b, 'B', (v) => emit(t, r, v, l))}
        {inp(l, 'L', (v) => emit(t, r, b, v))}
      </div>
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr 1fr', gap: 4 }}>
        {['T','R','B','L'].map((side) => (
          <div key={side} style={{ textAlign: 'center', fontSize: 9, color: T.text.muted }}>{side}</div>
        ))}
      </div>
    </div>
  );
}

// ── Image upload button ─────────────────────────────────────────
function ImageUploadButton({ onUploaded }) {
  const inputRef = useRef(null);
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState('');

  const handleFile = async (e) => {
    const file = e.target.files?.[0];
    if (!file) return;
    setUploading(true);
    setError('');
    try {
      const form = new FormData();
      form.append('image', file);
      const res = await fetch('/api/v1/templates/upload-image', { method: 'POST', body: form });
      if (!res.ok) throw new Error(`Upload failed (${res.status})`);
      const json = await res.json();
      if (json.url) onUploaded(json.url);
      else throw new Error('No URL returned');
    } catch (err) {
      setError(err.message || 'Upload failed');
    } finally {
      setUploading(false);
      // Reset so same file can be re-uploaded
      if (inputRef.current) inputRef.current.value = '';
    }
  };

  return (
    <>
      <input
        ref={inputRef}
        type="file"
        accept="image/*"
        style={{ display: 'none' }}
        onChange={handleFile}
      />
      <button
        onClick={() => inputRef.current?.click()}
        disabled={uploading}
        style={{
          ...css.btn('secondary'),
          width: '100%',
          fontSize: 11,
          padding: '6px 8px',
          marginBottom: 6,
          display: 'flex',
          alignItems: 'center',
          gap: 6,
          justifyContent: 'center',
          opacity: uploading ? 0.6 : 1,
        }}
      >
        <Upload size={12} />
        {uploading ? 'Uploading…' : 'Upload Image'}
      </button>
      {error && <div style={{ fontSize: 10, color: T.status.danger, marginBottom: 6 }}>{error}</div>}
    </>
  );
}

// ── Font family picker rendered with preview fonts ───────────────
function FontFamilyPicker({ value, onChange }) {
  return (
    <div style={{ marginBottom: 10 }}>
      <div style={{ fontSize: 10, color: T.text.muted, marginBottom: 4 }}>Font Family</div>
      <select
        style={{ ...css.input, fontSize: 12 }}
        value={value || FONT_FAMILIES[0].value}
        onChange={(e) => onChange(e.target.value)}
      >
        {FONT_FAMILIES.map((f) => (
          <option key={f.value} value={f.value} style={{ fontFamily: f.value }}>
            {f.label}
          </option>
        ))}
      </select>
    </div>
  );
}

// ── Toggle button (Bold / Italic / Underline) ────────────────────
function FmtToggle({ icon: Icon, label, active, onClick }) {
  return (
    <button
      onClick={onClick}
      aria-label={label}
      aria-pressed={active}
      title={label}
      style={{
        ...css.iconBtn,
        backgroundColor: active ? T.primary.glow : 'transparent',
        color: active ? T.primary.base : T.text.muted,
        border: `1px solid ${active ? T.primary.base : T.border.default}`,
      }}
    >
      <Icon size={14} />
    </button>
  );
}

export default function PropertyEditor({ block, onUpdate, onShowAI }) {
  const [linkUrl, setLinkUrl] = useState('');
  const [linkLabel, setLinkLabel] = useState('');

  if (!block) {
    return (
      <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', padding: 24 }}>
        <div style={{ textAlign: 'center' }}>
          <MousePointer size={32} style={{ color: T.text.muted, marginBottom: 8 }} />
          <div style={{ color: T.text.muted, fontSize: 13 }}>Select a block to edit its properties</div>
        </div>
      </div>
    );
  }

  const updateProp = (key, val) =>
    onUpdate(block.id, (b) => ({ ...b, properties: { ...b.properties, [key]: val } }));

  const updateStyle = (key, val) =>
    onUpdate(block.id, (b) => ({ ...b, style: { ...b.style, [key]: val } }));

  const SectionTitle = ({ children }) => (
    <div style={{
      ...css.panelHeader,
      padding: '8px 0', margin: '8px 0 4px',
      backgroundColor: 'transparent',
      borderBottom: `1px solid ${T.border.default}`,
      fontSize: 10,
    }}>
      {children}
    </div>
  );

  const info = BLOCK_TYPES.find((b) => b.type === block.type) || { icon: Square, color: T.text.muted };
  const Icon = info.icon;
  const p = block.properties || {};

  return (
    <div style={{ flex: 1, overflowY: 'auto', padding: 16 }}>
      {/* Block type header */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 12 }}>
        <Icon size={16} style={{ color: info.color }} />
        <span style={{ fontSize: 14, fontWeight: 600, color: T.text.primary, textTransform: 'capitalize' }}>
          {block.type.replace(/_/g, ' ')} Block
        </span>
      </div>

      <PropInput label="Label" value={block.label} onChange={(v) => onUpdate(block.id, (b) => ({ ...b, label: v }))} placeholder="Custom label..." />

      {/* ═══════ Alignment (shared) ═══════ */}
      <SectionTitle>
        <AlignLeft size={11} style={{ marginRight: 4 }} /> Alignment
      </SectionTitle>
      {block.type === 'text' ? (
        <div style={{ display: 'flex', gap: 4, marginBottom: 10 }}>
          {ALIGN_OPTS.map((a) => (
            <button
              key={a.val}
              onClick={() => updateProp('textAlign', a.val)}
              aria-label={a.label}
              aria-pressed={p.textAlign === a.val}
              style={{
                ...css.iconBtn,
                backgroundColor: p.textAlign === a.val ? T.primary.glow : 'transparent',
                color: p.textAlign === a.val ? T.primary.base : T.text.muted,
                border: `1px solid ${p.textAlign === a.val ? T.primary.base : T.border.default}`,
              }}
            >
              <a.icon size={14} />
            </button>
          ))}
          <button
            onClick={() => updateStyle('width', block.style.width === '100%' ? undefined : '100%')}
            aria-label="Full width"
            aria-pressed={block.style.width === '100%'}
            style={{
              ...css.iconBtn,
              backgroundColor: block.style.width === '100%' ? T.primary.glow : 'transparent',
              color: block.style.width === '100%' ? T.primary.base : T.text.muted,
              border: `1px solid ${block.style.width === '100%' ? T.primary.base : T.border.default}`,
            }}
          >
            <Maximize2 size={14} />
          </button>
        </div>
      ) : (
        <div style={{ display: 'flex', gap: 4, marginBottom: 10 }}>
          {[
            { val: 'left', label: 'Align left', ml: '0', mr: 'auto' },
            { val: 'center', label: 'Center', ml: 'auto', mr: 'auto' },
            { val: 'right', label: 'Align right', ml: 'auto', mr: '0' },
          ].map((a) => {
            const isActive = (block.style.marginLeft || '0') === a.ml && (block.style.marginRight || '0') === a.mr;
            return (
              <button
                key={a.val}
                onClick={() => onUpdate(block.id, (b) => ({ ...b, style: { ...b.style, marginLeft: a.ml, marginRight: a.mr } }))}
                aria-label={a.label}
                aria-pressed={isActive}
                style={{
                  ...css.iconBtn,
                  backgroundColor: isActive ? T.primary.glow : 'transparent',
                  color: isActive ? T.primary.base : T.text.muted,
                  border: `1px solid ${isActive ? T.primary.base : T.border.default}`,
                }}
              >
                {a.val === 'left' && <AlignLeft size={14} />}
                {a.val === 'center' && <AlignCenter size={14} />}
                {a.val === 'right' && <AlignRight size={14} />}
              </button>
            );
          })}
          <button
            onClick={() => updateStyle('width', block.style.width === '100%' ? undefined : '100%')}
            aria-label="Full width"
            aria-pressed={block.style.width === '100%'}
            style={{
              ...css.iconBtn,
              backgroundColor: block.style.width === '100%' ? T.primary.glow : 'transparent',
              color: block.style.width === '100%' ? T.primary.base : T.text.muted,
              border: `1px solid ${block.style.width === '100%' ? T.primary.base : T.border.default}`,
            }}
          >
            <Maximize2 size={14} />
          </button>
        </div>
      )}

      {/* ═══════ TEXT ═══════ */}
      {block.type === 'text' && (
        <>
          <SectionTitle>Content</SectionTitle>
          <PropInput label="Text Content" type="textarea" value={p.content} onChange={(v) => updateProp('content', v)} />
          <button
            onClick={() => onShowAI && onShowAI(block.id)}
            style={{ ...css.btn('secondary'), width: '100%', fontSize: 11, padding: '6px 8px', marginBottom: 10, color: T.accent.purple, borderColor: 'rgba(139,92,246,0.3)' }}
          >
            <Sparkles size={12} /> AI Generate Content
          </button>

          <SectionTitle>Typography</SectionTitle>
          <FontFamilyPicker value={p.fontFamily} onChange={(v) => updateProp('fontFamily', v)} />
          <PropInput label="Font Size (pt)" type="number" value={parseInt(p.fontSize) || 14} onChange={(v) => updateProp('fontSize', `${v}pt`)} />
          <PropInput
            label="Font Weight" type="select"
            value={p.fontWeight}
            onChange={(v) => updateProp('fontWeight', v)}
            options={[{ value: '400', label: 'Normal' }, { value: '500', label: 'Medium' }, { value: '600', label: 'Semibold' }, { value: '700', label: 'Bold' }]}
          />
          <ColorInput label="Text Color" value={p.color} onChange={(v) => updateProp('color', v)} />
          <PropInput label="Line Height" value={p.lineHeight} onChange={(v) => updateProp('lineHeight', v)} />
          <PropInput label="Letter Spacing" value={p.letterSpacing} onChange={(v) => updateProp('letterSpacing', v)} />

          <SectionTitle>Text Alignment</SectionTitle>
          <AlignButtons value={p.textAlign} onChange={(v) => updateProp('textAlign', v)} />

          <SectionTitle>Text Formatting</SectionTitle>
          <div style={{ display: 'flex', gap: 4, marginBottom: 10 }}>
            <FmtToggle
              icon={Bold} label="Bold"
              active={p.fontWeight === '700' || p.fontWeight === 'bold'}
              onClick={() => updateProp('fontWeight', (p.fontWeight === '700' || p.fontWeight === 'bold') ? '400' : '700')}
            />
            <FmtToggle
              icon={Italic} label="Italic"
              active={p.fontStyle === 'italic'}
              onClick={() => updateProp('fontStyle', p.fontStyle === 'italic' ? 'normal' : 'italic')}
            />
            <FmtToggle
              icon={Underline} label="Underline"
              active={p.textDecoration === 'underline'}
              onClick={() => updateProp('textDecoration', p.textDecoration === 'underline' ? undefined : 'underline')}
            />
            <FmtToggle
              icon={Strikethrough} label="Strikethrough"
              active={p.textDecoration === 'line-through'}
              onClick={() => updateProp('textDecoration', p.textDecoration === 'line-through' ? undefined : 'line-through')}
            />
          </div>

          <SectionTitle>Border</SectionTitle>
          <ColorInput label="Border Color" value={p.borderColor} onChange={(v) => updateProp('borderColor', v)} />
          <PropInput label="Border Width (px)" type="number" value={parseInt(p.borderWidth) || 0} onChange={(v) => updateProp('borderWidth', `${v}px`)} />
          <PropInput
            label="Border Style" type="select"
            value={p.borderStyle || 'solid'}
            onChange={(v) => updateProp('borderStyle', v)}
            options={['solid', 'dashed', 'dotted', 'none']}
          />

          <SectionTitle><Link size={11} style={{ marginRight: 4 }} /> Insert Link</SectionTitle>
          <PropInput label="URL" value={linkUrl} onChange={setLinkUrl} placeholder="https://..." />
          <PropInput label="Label" value={linkLabel} onChange={setLinkLabel} placeholder="Click here" />
          <button
            onClick={() => {
              if (!linkUrl) return;
              const label = linkLabel || linkUrl;
              const anchor = `<a href="${linkUrl}">${label}</a>`;
              updateProp('content', (p.content || '') + anchor);
              setLinkUrl('');
              setLinkLabel('');
            }}
            style={{ ...css.btn('secondary'), width: '100%', fontSize: 11, padding: '6px 8px', marginBottom: 10 }}
          >
            <Link size={12} /> Insert Link
          </button>
        </>
      )}

      {/* ═══════ IMAGE ═══════ */}
      {block.type === 'image' && (
        <>
          <SectionTitle>Image</SectionTitle>
          <PropInput label="Image URL" value={p.src} onChange={(v) => updateProp('src', v)} placeholder="https://..." />
          <ImageUploadButton onUploaded={(url) => updateProp('src', url)} />
          <PropInput label="Alt Text" value={p.alt} onChange={(v) => updateProp('alt', v)} />
          <PropInput label="Object Fit" type="select" value={p.objectFit} onChange={(v) => updateProp('objectFit', v)} options={['contain', 'cover', 'fill', 'none']} />
          <PropInput label="Keep Aspect Ratio" type="toggle" value={p.keepAspectRatio !== false} onChange={(v) => updateProp('keepAspectRatio', v)} />
        </>
      )}

      {/* ═══════ COLUMNS ═══════ */}
      {block.type === 'columns' && (
        <>
          <SectionTitle>Columns</SectionTitle>
          <PropInput
            label="Column Count" type="select"
            value={String(p.columnCount)}
            onChange={(v) => {
              const count = parseInt(v);
              let nc = [...(block.children || [])];
              while (nc.length < count) {
                nc.push({ id: uid(), type: '_column', properties: {}, style: {}, children: [], visible: true, locked: false, conditions: [] });
              }
              if (nc.length > count) nc = nc.slice(0, count);
              onUpdate(block.id, (b) => ({ ...b, properties: { ...b.properties, columnCount: count, ratios: Array(count).fill(1) }, children: nc }));
            }}
            options={['2', '3', '4']}
          />
          <PropInput label="Gap" value={p.gap} onChange={(v) => updateProp('gap', v)} placeholder="16px" />
        </>
      )}

      {/* ═══════ SPACER ═══════ */}
      {block.type === 'spacer' && (
        <>
          <SectionTitle>Spacer</SectionTitle>
          <PropInput label="Height" value={p.height} onChange={(v) => updateProp('height', v)} placeholder="32px" />
        </>
      )}

      {/* ═══════ DIVIDER ═══════ */}
      {block.type === 'divider' && (
        <>
          <SectionTitle>Divider</SectionTitle>
          <PropInput label="Line Thickness (px)" type="number" value={parseInt(p.thickness) || 1} onChange={(v) => updateProp('thickness', `${v}px`)} />
          <ColorInput label="Line Color" value={p.color} onChange={(v) => updateProp('color', v)} />
          <PropInput label="Style" type="select" value={p.lineStyle} onChange={(v) => updateProp('lineStyle', v)} options={['solid', 'dashed', 'dotted']} />
        </>
      )}

      {/* ═══════ TABLE ═══════ */}
      {block.type === 'table' && (
        <>
          <SectionTitle>Table Structure</SectionTitle>
          <PropInput label="Rows" type="number" value={p.rows} onChange={(v) => {
            const rows = Math.max(1, parseInt(v) || 1);
            const nc = { ...p.cells };
            for (let r = 0; r < rows; r++) for (let c = 0; c < p.cols; c++) if (!nc[`${r}-${c}`]) nc[`${r}-${c}`] = r === 0 && p.headerRow ? `Header ${c + 1}` : '';
            onUpdate(block.id, (b) => ({ ...b, properties: { ...b.properties, rows, cells: nc } }));
          }} />
          <PropInput label="Columns" type="number" value={p.cols} onChange={(v) => {
            const cols = Math.max(1, parseInt(v) || 1);
            const nc = { ...p.cells };
            for (let r = 0; r < p.rows; r++) for (let c = 0; c < cols; c++) if (!nc[`${r}-${c}`]) nc[`${r}-${c}`] = r === 0 && p.headerRow ? `Header ${c + 1}` : '';
            onUpdate(block.id, (b) => ({ ...b, properties: { ...b.properties, cols, cells: nc } }));
          }} />
          <PropInput label="Show Header Row" type="toggle" value={p.headerRow} onChange={(v) => updateProp('headerRow', v)} />
          <PropInput label="Cell Padding" value={p.cellPadding} onChange={(v) => updateProp('cellPadding', v)} />
          <ColorInput label="Border Color" value={p.borderColor} onChange={(v) => updateProp('borderColor', v)} />

          <SectionTitle>Header Style</SectionTitle>
          <ColorInput label="Header Background" value={p.headerBg} onChange={(v) => updateProp('headerBg', v)} />
          <ColorInput label="Header Text Color" value={p.headerColor} onChange={(v) => updateProp('headerColor', v)} />

          <SectionTitle>Row Style</SectionTitle>
          <PropInput
            label="Alternating Rows"
            type="toggle"
            value={!!p.altRowEnabled}
            onChange={(v) => updateProp('altRowEnabled', v)}
          />
          {p.altRowEnabled && (
            <ColorInput label="Alternate Row Color" value={p.altRowColor || '#f9f9f9'} onChange={(v) => updateProp('altRowColor', v)} />
          )}

          <SectionTitle>Column Widths (%)</SectionTitle>
          <div style={{ display: 'flex', gap: 4, marginBottom: 10, flexWrap: 'wrap' }}>
            {Array.from({ length: p.cols }, (_, c) => {
              const widths = p.colWidths || Array.from({ length: p.cols }, () => Math.round(100 / p.cols));
              return (
                <div key={c} style={{ flex: '1 1 40px', minWidth: 40 }}>
                  <div style={{ fontSize: 9, color: T.text.muted, textAlign: 'center', marginBottom: 2 }}>C{c + 1}</div>
                  <input
                    type="number"
                    min="1"
                    max="100"
                    style={{ ...css.input, textAlign: 'center', padding: '4px 2px', fontSize: 11 }}
                    value={widths[c] || Math.round(100 / p.cols)}
                    onChange={(e) => {
                      const newWidths = [...(p.colWidths || Array.from({ length: p.cols }, () => Math.round(100 / p.cols)))];
                      newWidths[c] = parseInt(e.target.value) || 0;
                      updateProp('colWidths', newWidths);
                    }}
                  />
                </div>
              );
            })}
          </div>

          <SectionTitle>Cell Data</SectionTitle>
          <div style={{ fontSize: 10, color: T.text.muted, marginBottom: 6 }}>
            Click cells on canvas to edit inline, or use fields below. Supports {'{{merge.tags}}'}.
          </div>
          <div style={{ maxHeight: 200, overflowY: 'auto' }}>
            {Array.from({ length: p.rows }, (_, r) =>
              Array.from({ length: p.cols }, (_, c) => (
                <div key={`${r}-${c}`} style={{ marginBottom: 4 }}>
                  <span style={{ fontSize: 10, color: T.text.muted }}>
                    {r === 0 && p.headerRow ? 'H' : 'C'}[{r},{c}]
                  </span>
                  <input
                    style={{ ...css.input, fontSize: 11, padding: '3px 6px' }}
                    value={p.cells?.[`${r}-${c}`] || ''}
                    onChange={(e) => {
                      const nc = { ...p.cells, [`${r}-${c}`]: e.target.value };
                      updateProp('cells', nc);
                    }}
                    placeholder="{{field}} or text"
                  />
                </div>
              ))
            )}
          </div>
        </>
      )}

      {/* ═══════ DYNAMIC FIELD ═══════ */}
      {block.type === 'dynamic_field' && (
        <>
          <SectionTitle>Dynamic Field</SectionTitle>

          {/* Session 3: Input mode - field | static | formula */}
          <PropInput
            label="Input Mode"
            type="select"
            value={p.inputMode || p.dataSource || 'field'}
            onChange={(v) => updateProp('inputMode', v)}
            options={[
              { value: 'field',   label: 'Field (merge tag)' },
              { value: 'static',  label: 'Static text' },
              { value: 'formula', label: 'Formula' },
            ]}
          />
          {(p.inputMode || p.dataSource || 'field') === 'field' && (
            <FieldPicker value={p.fieldPath} onChange={(v) => updateProp('fieldPath', v)} />
          )}
          {(p.inputMode || p.dataSource || 'field') === 'static' && (
            <PropInput label="Static Value" value={p.staticValue} onChange={(v) => updateProp('staticValue', v)} placeholder="Enter static text..." />
          )}
          {(p.inputMode || p.dataSource || 'field') === 'formula' && (
            <div style={{ marginBottom: 10 }}>
              <div style={{ fontSize: 10, color: T.text.muted, marginBottom: 4 }}>Formula Expression</div>
              <textarea
                rows={4}
                placeholder="e.g. {customer.name} + ' ' + {shipping.city}"
                value={p.formula || ''}
                onChange={(e) => updateProp('formula', e.target.value)}
                style={{
                  background: T.bg.tertiary,
                  border: `1px solid ${T.border.default}`,
                  borderRadius: T.radius.sm,
                  color: T.text.primary,
                  fontFamily: 'monospace',
                  fontSize: 11,
                  padding: '6px 8px',
                  resize: 'vertical',
                  width: '100%',
                  boxSizing: 'border-box',
                  minHeight: 64,
                }}
              />
              <div style={{ fontSize: 10, color: T.text.muted, marginTop: 4, lineHeight: 1.5 }}>
                Field refs: <code style={{ color: T.accent.orange }}>&#123;order.id&#125;</code><br />
                Concat: <code style={{ color: T.accent.orange }}>A + &quot; &quot; + B</code><br />
                Math: <code style={{ color: T.accent.orange }}>&#123;unit_price&#125; * &#123;qty&#125;</code><br />
                IF: <code style={{ color: T.accent.orange }}>IF(cond, a, b)</code> UPPER/LOWER/TRIM
              </div>
            </div>
          )}

          <PropInput label="Prefix" value={p.prefix} onChange={(v) => updateProp('prefix', v)} placeholder="e.g. Order: " />
          <PropInput label="Suffix" value={p.suffix} onChange={(v) => updateProp('suffix', v)} />

          <SectionTitle>Typography</SectionTitle>
          <FontFamilyPicker value={p.fontFamily} onChange={(v) => updateProp('fontFamily', v)} />
          <PropInput label="Font Size (pt)" type="number" value={parseInt(p.fontSize) || 14} onChange={(v) => updateProp('fontSize', `${v}pt`)} />
          <PropInput
            label="Font Weight" type="select"
            value={p.fontWeight}
            onChange={(v) => updateProp('fontWeight', v)}
            options={[{ value: '400', label: 'Normal' }, { value: '600', label: 'Semibold' }, { value: '700', label: 'Bold' }]}
          />
          <div style={{ display: 'flex', gap: 4, marginBottom: 10 }}>
            <FmtToggle
              icon={Bold} label="Bold"
              active={p.fontWeight === '700' || p.fontWeight === 'bold'}
              onClick={() => updateProp('fontWeight', (p.fontWeight === '700' || p.fontWeight === 'bold') ? '400' : '700')}
            />
            <FmtToggle
              icon={Italic} label="Italic"
              active={p.fontStyle === 'italic'}
              onClick={() => updateProp('fontStyle', p.fontStyle === 'italic' ? 'normal' : 'italic')}
            />
            <FmtToggle
              icon={Underline} label="Underline"
              active={p.textDecoration === 'underline'}
              onClick={() => updateProp('textDecoration', p.textDecoration === 'underline' ? undefined : 'underline')}
            />
          </div>
          <ColorInput label="Text Color" value={p.color} onChange={(v) => updateProp('color', v)} />

          <SectionTitle>Text Alignment</SectionTitle>
          <AlignButtons value={p.textAlign} onChange={(v) => updateProp('textAlign', v)} />
        </>
      )}

      {/* ═══════ BARCODE ═══════ */}
      {block.type === 'barcode' && (
        <>
          <SectionTitle>Barcode</SectionTitle>
          <PropInput label="Type" type="select" value={p.barcodeType} onChange={(v) => updateProp('barcodeType', v)} options={['Code128', 'EAN-13', 'QR']} />

          <PropInput
            label="Data Source"
            type="select"
            value={p.barcodeDataSource || 'static'}
            onChange={(v) => updateProp('barcodeDataSource', v)}
            options={[{ value: 'static', label: 'Static value' }, { value: 'field', label: 'Data field' }]}
          />
          {(p.barcodeDataSource || 'static') === 'field' ? (
            <FieldPicker value={p.barcodeField || ''} onChange={(v) => updateProp('barcodeField', v)} />
          ) : (
            <PropInput label="Value" value={p.value} onChange={(v) => updateProp('value', v)} placeholder="{{order.id}}" />
          )}

          <PropInput label="Width" value={p.barcodeWidth} onChange={(v) => updateProp('barcodeWidth', v)} />
          <PropInput label="Height" value={p.barcodeHeight} onChange={(v) => updateProp('barcodeHeight', v)} />
        </>
      )}

      {/* ═══════ LOGO ═══════ */}
      {block.type === 'logo' && (
        <>
          <SectionTitle>Logo</SectionTitle>
          <PropInput label="Logo URL" value={p.src} onChange={(v) => updateProp('src', v)} placeholder="Auto-pulls from settings" />
          <PropInput label="Max Width" value={p.maxWidth} onChange={(v) => updateProp('maxWidth', v)} />
          <PropInput label="Max Height" value={p.maxHeight} onChange={(v) => updateProp('maxHeight', v)} />
        </>
      )}

      {/* ═══════ BOX ═══════ */}
      {block.type === 'box' && (
        <>
          <SectionTitle>Box</SectionTitle>
          <ColorInput label="Background Color" value={block.style.backgroundColor} onChange={(v) => updateStyle('backgroundColor', v)} />
          <ColorInput label="Border Color" value={p.borderColor} onChange={(v) => updateProp('borderColor', v)} />
          <PropInput label="Border Width (px)" type="number" value={parseInt(p.borderWidth) || 0} onChange={(v) => updateProp('borderWidth', `${v}px`)} />
          <PropInput
            label="Border Style" type="select"
            value={p.borderStyle || 'solid'}
            onChange={(v) => updateProp('borderStyle', v)}
            options={['solid', 'dashed', 'dotted', 'none']}
          />
          <PropInput label="Border Radius (px)" type="number" value={parseInt(block.style.borderRadius) || 0} onChange={(v) => updateStyle('borderRadius', `${v}px`)} />
          <PaddingInputs label="Padding (T R B L)" value={block.style.padding} onChange={(v) => updateStyle('padding', v)} />
        </>
      )}

      {/* ═══════ REPEATER ═══════ */}
      {block.type === 'repeater' && (
        <>
          <SectionTitle>Repeater</SectionTitle>
          <PropInput label="Data Source" type="select" value={p.dataSource} onChange={(v) => updateProp('dataSource', v)} options={[{ value: 'lines', label: 'Order Lines' }]} />
          <PropInput label="Direction" type="select" value={p.direction} onChange={(v) => updateProp('direction', v)} options={[{ value: 'vertical', label: 'Vertical' }, { value: 'horizontal', label: 'Horizontal' }]} />
          <div style={{ padding: 8, backgroundColor: T.bg.tertiary, borderRadius: T.radius.md, fontSize: 11, color: T.text.muted, marginBottom: 10 }}>
            <Repeat size={12} style={{ marginRight: 4, verticalAlign: 'middle' }} />
            Drop blocks inside. They repeat per line item. Use <code style={{ color: T.accent.orange }}>line.*</code> merge tags.
          </div>
        </>
      )}

      {/* ═══════ PAGE BREAK ═══════ */}
      {block.type === 'page_break' && (
        <>
          <SectionTitle>Page Break</SectionTitle>
          <div style={{ padding: 8, backgroundColor: T.bg.tertiary, borderRadius: T.radius.md, fontSize: 11, color: T.text.muted }}>
            <Scissors size={12} style={{ marginRight: 4, verticalAlign: 'middle' }} />
            Forces a new page in PDF output.
          </div>
        </>
      )}

      {/* ═══════ UNORDERED LIST ═══════ */}
      {block.type === 'unordered_list' && (
        <>
          <SectionTitle>Unordered List</SectionTitle>
          <FontFamilyPicker value={p.fontFamily} onChange={(v) => updateProp('fontFamily', v)} />
          <PropInput label="Font Size (pt)" type="number" value={parseInt(p.fontSize) || 14} onChange={(v) => updateProp('fontSize', `${v}pt`)} />
          <ColorInput label="Text Color" value={p.color} onChange={(v) => updateProp('color', v)} />
          <SectionTitle>Items</SectionTitle>
          {(p.items || []).map((item, idx) => (
            <div key={idx} style={{ display: 'flex', gap: 4, marginBottom: 4, alignItems: 'center' }}>
              <input
                style={{ ...css.input, flex: 1, fontSize: 11 }}
                value={item}
                onChange={(e) => {
                  const items = [...(p.items || [])];
                  items[idx] = e.target.value;
                  updateProp('items', items);
                }}
                placeholder={`Item ${idx + 1}`}
              />
              <button onClick={() => updateProp('items', (p.items || []).filter((_, i) => i !== idx))} style={{ ...css.iconBtn, color: T.status.danger, flexShrink: 0 }}>
                <Minus size={12} />
              </button>
            </div>
          ))}
          <button onClick={() => updateProp('items', [...(p.items || []), 'New item'])} style={{ ...css.btn('secondary'), width: '100%', fontSize: 11, padding: '4px 8px', marginTop: 4 }}>
            <Plus size={11} /> Add Item
          </button>
        </>
      )}

      {/* ═══════ ORDERED LIST ═══════ */}
      {block.type === 'ordered_list' && (
        <>
          <SectionTitle>Ordered List</SectionTitle>
          <FontFamilyPicker value={p.fontFamily} onChange={(v) => updateProp('fontFamily', v)} />
          <PropInput label="Font Size (pt)" type="number" value={parseInt(p.fontSize) || 14} onChange={(v) => updateProp('fontSize', `${v}pt`)} />
          <ColorInput label="Text Color" value={p.color} onChange={(v) => updateProp('color', v)} />
          <SectionTitle>Items</SectionTitle>
          {(p.items || []).map((item, idx) => (
            <div key={idx} style={{ display: 'flex', gap: 4, marginBottom: 4, alignItems: 'center' }}>
              <span style={{ fontSize: 10, color: T.text.muted, minWidth: 16 }}>{idx + 1}.</span>
              <input
                style={{ ...css.input, flex: 1, fontSize: 11 }}
                value={item}
                onChange={(e) => {
                  const items = [...(p.items || [])];
                  items[idx] = e.target.value;
                  updateProp('items', items);
                }}
                placeholder={`Item ${idx + 1}`}
              />
              <button onClick={() => updateProp('items', (p.items || []).filter((_, i) => i !== idx))} style={{ ...css.iconBtn, color: T.status.danger, flexShrink: 0 }}>
                <Minus size={12} />
              </button>
            </div>
          ))}
          <button onClick={() => updateProp('items', [...(p.items || []), 'New item'])} style={{ ...css.btn('secondary'), width: '100%', fontSize: 11, padding: '4px 8px', marginTop: 4 }}>
            <Plus size={11} /> Add Item
          </button>
        </>
      )}

      {/* ═══════ SHARED: Transform ═══════ */}
      <SectionTitle>
        <RotateCw size={11} style={{ marginRight: 4 }} /> Transform
      </SectionTitle>
      <PropInput
        label="Rotation (°)"
        type="number"
        value={parseInt(p.rotation) || 0}
        onChange={(v) => updateProp('rotation', parseInt(v) || 0)}
      />
      <div style={{ marginBottom: 10 }}>
        <div style={{ fontSize: 10, color: T.text.muted, marginBottom: 4 }}>Opacity (%)</div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <input
            type="range"
            min="0"
            max="100"
            value={Math.round((p.opacity !== undefined ? p.opacity : 1) * 100)}
            onChange={(e) => updateProp('opacity', parseInt(e.target.value) / 100)}
            style={{ flex: 1 }}
          />
          <span style={{ fontSize: 11, color: T.text.secondary, minWidth: 32, textAlign: 'right' }}>
            {Math.round((p.opacity !== undefined ? p.opacity : 1) * 100)}%
          </span>
        </div>
      </div>

      {/* ═══════ SHARED: Layout & Spacing ═══════ */}
      <SectionTitle>Layout & Spacing</SectionTitle>
      <PaddingInputs label="Padding (T R B L)" value={block.style.padding} onChange={(v) => updateStyle('padding', v)} />
      <PropInput label="Margin" value={block.style.margin} onChange={(v) => updateStyle('margin', v)} placeholder="0" />
      <PropInput label="Width" value={block.style.width} onChange={(v) => updateStyle('width', v)} placeholder="auto" />
      <PropInput label="Height" value={block.style.height} onChange={(v) => updateStyle('height', v)} placeholder="auto" />

      {/* ═══════ SHARED: Appearance ═══════ */}
      <SectionTitle>Appearance</SectionTitle>
      <ColorInput label="Background" value={block.style.backgroundColor} onChange={(v) => updateStyle('backgroundColor', v)} />
      <PropInput label="Border" value={block.style.border} onChange={(v) => updateStyle('border', v)} placeholder="1px solid #ccc" />
      <PropInput label="Border Radius" value={block.style.borderRadius} onChange={(v) => updateStyle('borderRadius', v)} placeholder="0" />

      {/* ═══════ SHARED: Conditional Styles (Session 3) ═══════ */}
      <SectionTitle><Sparkles size={11} style={{ marginRight: 4 }} /> Conditional Styles</SectionTitle>
      <ConditionalStylesEditor
        entries={block.conditionalStyles || []}
        onChange={(entries) => onUpdate(block.id, (b) => ({ ...b, conditionalStyles: entries }))}
      />

      {/* ═══════ SHARED: Conditional Visibility ═══════ */}
      <SectionTitle><Filter size={11} style={{ marginRight: 4 }} /> Conditional Visibility</SectionTitle>
      <ConditionsEditor
        conditions={block.conditions || []}
        onChange={(conds) => onUpdate(block.id, (b) => ({ ...b, conditions: conds }))}
      />
    </div>
  );
}

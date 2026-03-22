// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// CANVAS BLOCK RENDERER
// Renders a single block on the builder canvas. Handles:
//   • Edit mode: selection outlines, drag-and-drop, context menu
//   • Preview mode: merge tag resolution, condition evaluation
//   • Nested containers: columns, box, repeater all support child drops
//   • Inline table editing (#12): contentEditable cells with focus
//     rings, Tab/Shift+Tab wrapping, Enter to move down, Escape to
//     blur, and merge tag mini-picker
//
// This component is recursive — columns, box, and repeater blocks
// render their children as nested CanvasBlock instances.
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

import React, { useState, useRef, useEffect, useCallback } from 'react';
import { Image, Building2, Repeat, Scissors, Plus, Tag, ChevronDown } from 'lucide-react';
import { T, THEME_PRESETS, SAMPLE_DATA, FIELD_CATEGORIES } from '../../../constants/index.jsx';
import {
  resolveText, resolveMergeTag, evaluateConditions,
  evaluateConditionalStyles,
  generateCode128SVG, generateEAN13SVG, generateQRSVG,
} from '../../../utils/index.jsx';
import { Filter } from 'lucide-react';
import RepeaterChildPreview from './RepeaterChildPreview';

// ── Merge Tag Mini-Picker (for table cells) ─────────────────────
// Compact dropdown that inserts a {{field.path}} at the cursor position
// inside a contentEditable cell.
function MergeTagPicker({ onInsert, onClose, anchorRect }) {
  const [expandedCat, setExpandedCat] = useState(null);
  const ref = useRef(null);

  useEffect(() => {
    const handleClick = (e) => {
      if (ref.current && !ref.current.contains(e.target)) onClose();
    };
    document.addEventListener('mousedown', handleClick);
    return () => document.removeEventListener('mousedown', handleClick);
  }, [onClose]);

  const top = anchorRect ? anchorRect.bottom + 4 : 0;
  const left = anchorRect ? anchorRect.left : 0;

  return (
    <div
      ref={ref}
      style={{
        position: 'fixed', top, left, zIndex: 1000,
        width: 240, maxHeight: 260, overflowY: 'auto',
        backgroundColor: T.bg.elevated,
        border: `1px solid ${T.border.bright}`,
        borderRadius: T.radius.lg,
        boxShadow: T.shadow.lg,
        fontSize: 11,
      }}
      onClick={(e) => e.stopPropagation()}
    >
      <div style={{
        padding: '6px 10px', borderBottom: `1px solid ${T.border.default}`,
        fontSize: 10, fontWeight: 600, color: T.text.muted, textTransform: 'uppercase',
        letterSpacing: '0.04em',
      }}>
        Insert Merge Tag
      </div>
      {Object.entries(FIELD_CATEGORIES).map(([cat, fields]) => (
        <div key={cat}>
          <button
            onClick={() => setExpandedCat(expandedCat === cat ? null : cat)}
            style={{
              width: '100%', padding: '5px 10px',
              border: 'none', backgroundColor: 'transparent',
              color: T.text.primary, fontSize: 11, fontWeight: 600,
              fontFamily: T.font,
              display: 'flex', alignItems: 'center', gap: 6,
              cursor: 'pointer',
              borderBottom: `1px solid ${T.border.default}`,
            }}
          >
            <Tag size={10} style={{ color: T.text.muted }} />
            {cat}
            <ChevronDown
              size={10}
              style={{
                marginLeft: 'auto', color: T.text.muted,
                transform: expandedCat === cat ? 'rotate(180deg)' : 'none',
                transition: 'transform 150ms',
              }}
            />
          </button>
          {expandedCat === cat && fields.map((f) => (
            <button
              key={f.path}
              onClick={() => { onInsert(`{{${f.path}}}`); onClose(); }}
              style={{
                width: '100%', padding: '4px 10px 4px 28px',
                border: 'none', backgroundColor: 'transparent',
                color: T.text.secondary, fontSize: 11, fontFamily: T.font,
                cursor: 'pointer', textAlign: 'left',
                display: 'flex', alignItems: 'center', gap: 4,
              }}
              onMouseEnter={(e) => { e.currentTarget.style.backgroundColor = T.primary.glow; }}
              onMouseLeave={(e) => { e.currentTarget.style.backgroundColor = 'transparent'; }}
            >
              <span style={{ color: T.accent.orange, fontFamily: 'monospace', fontSize: 10 }}>{'{{ }}'}</span>
              {f.label}
            </button>
          ))}
        </div>
      ))}
    </div>
  );
}

// ── Table Block sub-component ────────────────────────────────────
// Extracted so it can use its own state for the merge tag picker.
// Features:
//   • Focus ring on active cell (blue outline + glow)
//   • Tab wraps forward through cells (row by row, then back to start)
//   • Shift+Tab wraps backward
//   • Enter moves to cell below (wraps to next column at bottom)
//   • Escape blurs the active cell
//   • Right-click or Ctrl+M opens merge tag picker
function TableBlock({ block, rows, cols, headerRow, cellPadding, borderColor, cells, baseStyle, tv, isPreview, onCellEdit }) {
  // New session 2 props from block.properties
  // headerBg, headerColor, altRowEnabled, altRowColor, colWidths are read directly from block.properties above
  const [focusedCell, setFocusedCell] = useState(null);
  const [tagPicker, setTagPicker] = useState(null); // { row, col, rect }
  const tableRef = useRef(null);

  // Get a cell element by row/col indices
  const getCell = useCallback((r, c) => {
    if (!tableRef.current) return null;
    const allRows = tableRef.current.querySelectorAll('tr');
    if (!allRows[r]) return null;
    return allRows[r].children[c] || null;
  }, []);

  // Navigate to a cell by row/col with wrapping
  const navigateTo = useCallback((r, c) => {
    // Wrap column
    if (c >= cols) { c = 0; r++; }
    if (c < 0) { c = cols - 1; r--; }
    // Wrap row
    if (r >= rows) r = 0;
    if (r < 0) r = rows - 1;
    const cell = getCell(r, c);
    if (cell) cell.focus();
  }, [rows, cols, getCell]);

  // Insert merge tag at cursor position in a contentEditable cell
  const insertTag = useCallback((tag) => {
    if (!tagPicker) return;
    const cell = getCell(tagPicker.row, tagPicker.col);
    if (!cell) return;
    cell.focus();
    // Insert at end
    const sel = window.getSelection();
    const range = document.createRange();
    range.selectNodeContents(cell);
    range.collapse(false);
    sel.removeAllRanges();
    sel.addRange(range);
    document.execCommand('insertText', false, tag);
    // Commit change
    if (onCellEdit) {
      onCellEdit(block.id, `${tagPicker.row}-${tagPicker.col}`, cell.textContent);
    }
  }, [tagPicker, getCell, block.id, onCellEdit]);

  const handleKeyDown = useCallback((e, r, c) => {
    // Tab / Shift+Tab — move between cells
    if (e.key === 'Tab') {
      e.preventDefault();
      e.stopPropagation();
      if (e.shiftKey) navigateTo(r, c - 1);
      else navigateTo(r, c + 1);
      return;
    }

    // Enter — move down one row
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      navigateTo(r + 1, c);
      return;
    }

    // Escape — blur
    if (e.key === 'Escape') {
      e.preventDefault();
      e.currentTarget.blur();
      return;
    }

    // Ctrl+M — open merge tag picker
    if (e.key === 'm' && (e.ctrlKey || e.metaKey)) {
      e.preventDefault();
      const rect = e.currentTarget.getBoundingClientRect();
      setTagPicker({ row: r, col: c, rect });
    }
  }, [navigateTo]);

  const handleContextMenu = useCallback((e, r, c) => {
    // Right-click on a cell can open merge tag picker instead of default context
    // Only in edit mode and only if the cell is focused
    if (isPreview) return;
    if (focusedCell === `${r}-${c}`) {
      e.preventDefault();
      e.stopPropagation();
      const rect = e.currentTarget.getBoundingClientRect();
      setTagPicker({ row: r, col: c, rect });
    }
  }, [isPreview, focusedCell]);

  // Focus ring styles
  const cellFocusStyle = {
    outline: 'none',
    boxShadow: 'none',
    transition: 'box-shadow 100ms ease',
  };

  const cellFocusedStyle = {
    boxShadow: `inset 0 0 0 2px ${T.primary.base}, 0 0 0 2px ${T.primary.glow}`,
  };

  return (
    <div style={{ ...baseStyle, overflowX: 'auto', position: 'relative' }}>
      <table ref={tableRef} style={{ width: '100%', borderCollapse: 'collapse', border: `1px solid ${borderColor}` }}>
        {headerRow && (
          <thead>
            <tr>
              {Array.from({ length: cols }, (_, c) => {
                const cellContent = isPreview
                  ? resolveText(cells?.[`0-${c}`] || '', SAMPLE_DATA)
                  : (cells?.[`0-${c}`] || '');
                const cellKey = `0-${c}`;
                const isFocused = focusedCell === cellKey;
                return (
                  <th
                    key={c}
                    style={{
                      padding: cellPadding, border: `1px solid ${borderColor}`,
                      fontSize: 13, textAlign: 'left', fontWeight: 600,
                      backgroundColor: block.properties.headerBg || tv.tableHeaderBg || '#f5f5f5',
                      color: block.properties.headerColor || tv.text || '#333',
                      cursor: isPreview ? 'default' : 'text',
                      minWidth: 40, position: 'relative',
                      ...cellFocusStyle,
                      ...(isFocused && !isPreview ? cellFocusedStyle : {}),
                    }}
                    contentEditable={!isPreview}
                    suppressContentEditableWarning
                    onFocus={() => setFocusedCell(cellKey)}
                    onBlur={(e) => {
                      setFocusedCell(null);
                      if (!isPreview && onCellEdit) {
                        onCellEdit(block.id, cellKey, e.currentTarget.textContent);
                      }
                    }}
                    onKeyDown={(e) => handleKeyDown(e, 0, c)}
                    onContextMenu={(e) => handleContextMenu(e, 0, c)}
                  >
                    {cellContent}
                  </th>
                );
              })}
            </tr>
          </thead>
        )}
        <tbody>
          {Array.from({ length: rows }, (_, r) => {
            if (r === 0 && headerRow) return null; // rendered in thead
            return (
              <tr key={r}>
                {Array.from({ length: cols }, (_, c) => {
                  const cellContent = isPreview
                    ? resolveText(cells?.[`${r}-${c}`] || '', SAMPLE_DATA)
                    : (cells?.[`${r}-${c}`] || '');
                  const cellKey = `${r}-${c}`;
                  const isFocused = focusedCell === cellKey;
                  return (
                    <td
                      key={c}
                      style={{
                        padding: cellPadding, border: `1px solid ${borderColor}`,
                        fontSize: 13, textAlign: 'left', fontWeight: 400,
                        backgroundColor: (block.properties.altRowEnabled && r % 2 === 0) ? (block.properties.altRowColor || '#f9f9f9') : 'transparent',
                        color: tv.text || '#333',
                        cursor: isPreview ? 'default' : 'text',
                        minWidth: 40, position: 'relative',
                        ...cellFocusStyle,
                        ...(isFocused && !isPreview ? cellFocusedStyle : {}),
                      }}
                      contentEditable={!isPreview}
                      suppressContentEditableWarning
                      onFocus={() => setFocusedCell(cellKey)}
                      onBlur={(e) => {
                        setFocusedCell(null);
                        if (!isPreview && onCellEdit) {
                          onCellEdit(block.id, cellKey, e.currentTarget.textContent);
                        }
                      }}
                      onKeyDown={(e) => handleKeyDown(e, r, c)}
                      onContextMenu={(e) => handleContextMenu(e, r, c)}
                    >
                      {cellContent}
                    </td>
                  );
                })}
              </tr>
            );
          })}
        </tbody>
      </table>

      {/* Keyboard hint (shown when a cell is focused in edit mode) */}
      {focusedCell && !isPreview && (
        <div style={{
          marginTop: 4, fontSize: 9, color: T.text.muted,
          display: 'flex', gap: 8, flexWrap: 'wrap', padding: '2px 0',
        }}>
          <span><kbd style={kbdStyle}>Tab</kbd> next</span>
          <span><kbd style={kbdStyle}>Shift+Tab</kbd> prev</span>
          <span><kbd style={kbdStyle}>Enter</kbd> down</span>
          <span><kbd style={kbdStyle}>Esc</kbd> deselect</span>
          <span><kbd style={kbdStyle}>Ctrl+M</kbd> merge tag</span>
        </div>
      )}

      {/* Merge tag picker overlay */}
      {tagPicker && !isPreview && (
        <MergeTagPicker
          anchorRect={tagPicker.rect}
          onInsert={insertTag}
          onClose={() => setTagPicker(null)}
        />
      )}
    </div>
  );
}

// Kbd style for the keyboard hints
const kbdStyle = {
  display: 'inline-block', padding: '0 3px',
  backgroundColor: 'rgba(255,255,255,0.08)',
  border: '1px solid rgba(255,255,255,0.12)',
  borderRadius: 2, fontSize: 8, fontFamily: 'monospace',
  lineHeight: '14px', verticalAlign: 'middle',
};

export default function CanvasBlock({
  block, selectedId, onSelect, onDrop,
  isPreview, dragOverId, setDragOverId,
  onInternalDragStart, theme, onContextMenu, onCellEdit,
}) {
  // ── Condition evaluation (preview only) ──────────────────────
  if (isPreview && block.conditions) {
    const conds = Array.isArray(block.conditions) ? block.conditions.filter((c) => c?.field) : [];
    if (conds.length > 0 && !evaluateConditions(block.conditions, SAMPLE_DATA)) return null;
  }

  // ── Hidden block indicator (edit mode only) ──────────────────
  if (!block.visible && !isPreview) {
    return (
      <div style={{
        opacity: 0.3, border: `1px dashed ${T.text.muted}`,
        borderRadius: 4, padding: 4, margin: 2,
        fontSize: 10, color: T.text.muted, textAlign: 'center',
      }}>
        Hidden: {block.type}
      </div>
    );
  }
  if (!block.visible && isPreview) return null;

  const isSelected = block.id === selectedId && !isPreview;
  const isDragOver = block.id === dragOverId;
  // Apply opacity and rotation from block properties to the base style
  // Session 3: merge conditional style overrides into baseStyle
  const _condStyleOverrides = Array.isArray(block.conditionalStyles) && block.conditionalStyles.length > 0
    ? evaluateConditionalStyles(block.conditionalStyles, SAMPLE_DATA) : {};

  const baseStyle = {
    ...block.style,
    ...(block.properties?.opacity !== undefined && block.properties.opacity !== 1
      ? { opacity: block.properties.opacity }
      : {}),
    ...(block.properties?.rotation
      ? { transform: `rotate(${block.properties.rotation}deg)` }
      : {}),
    ..._condStyleOverrides,
  };
  const tv = theme?.vars || THEME_PRESETS.default.vars;

  // ── Drag and drop handlers ───────────────────────────────────
  const handleDragOver = (e) => { e.preventDefault(); e.stopPropagation(); setDragOverId(block.id); };

  const handleDrop = (e) => {
    e.preventDefault(); e.stopPropagation(); setDragOverId(null);
    const bt = e.dataTransfer.getData('application/x-block-type');
    const bi = e.dataTransfer.getData('application/x-block-id');
    const bd = e.dataTransfer.getData('application/x-block-data');
    if (bd) { try { onDrop(null, block.id, null, false, JSON.parse(bd)); } catch(ex) {} }
    else if (bt) onDrop(bt, block.id);
    else if (bi) onDrop(null, block.id, bi);
  };

  const handleDragStart = (e) => {
    e.stopPropagation();
    e.dataTransfer.setData('application/x-block-id', block.id);
    e.dataTransfer.effectAllowed = 'move';
    onInternalDragStart?.(block.id);
  };

  const handleCtxMenu = (e) => {
    if (isPreview) return;
    e.preventDefault(); e.stopPropagation();
    onSelect(block.id);
    onContextMenu?.(e.clientX, e.clientY, block.id);
  };

  // ── Selection wrapper style ──────────────────────────────────
  const wrapperStyle = {
    position: 'relative',
    outline: isSelected ? `2px solid ${T.primary.base}` : isDragOver ? `2px dashed ${T.accent.cyan}` : '2px solid transparent',
    borderRadius: 2,
    cursor: isPreview ? 'default' : 'pointer',
    transition: 'outline 150ms ease-in-out',
    boxShadow: isSelected ? `0 0 0 4px ${T.primary.glow}` : 'none',
  };

  const hasConditions = block.conditions && block.conditions.filter((c) => c?.field).length > 0;

  // ── Type badge (shown when selected) ─────────────────────────
  const selectionBadge = isSelected && !isPreview ? (
    <div style={{
      position: 'absolute', top: -1, left: 8,
      backgroundColor: T.primary.base, color: '#fff',
      fontSize: 9, fontWeight: 700, padding: '1px 6px',
      borderRadius: '0 0 4px 4px', textTransform: 'uppercase',
      zIndex: 10, letterSpacing: '0.04em',
      display: 'flex', alignItems: 'center', gap: 3,
    }}>
      {block.type.replace('_', ' ')}
      {hasConditions && <Filter size={8} />}
    </div>
  ) : null;

  // ── Shared child renderer (for columns, box, repeater) ───────
  const renderChildren = (children) =>
    (children || []).map((child) => (
      <CanvasBlock
        key={child.id} block={child} selectedId={selectedId}
        onSelect={onSelect} onDrop={onDrop} isPreview={isPreview}
        dragOverId={dragOverId} setDragOverId={setDragOverId}
        onInternalDragStart={onInternalDragStart} theme={theme}
        onContextMenu={onContextMenu} onCellEdit={onCellEdit}
      />
    ));

  // Drop zone handlers for containers
  const containerDropHandlers = (containerId) => ({
    onDragOver: (e) => { e.preventDefault(); e.stopPropagation(); setDragOverId(containerId); },
    onDrop: (e) => {
      e.preventDefault(); e.stopPropagation(); setDragOverId(null);
      const bt = e.dataTransfer.getData('application/x-block-type');
      const bi = e.dataTransfer.getData('application/x-block-id');
      const bd = e.dataTransfer.getData('application/x-block-data');
      if (bd) { try { onDrop(null, containerId, null, true, JSON.parse(bd)); } catch(ex) {} }
      else if (bt) onDrop(bt, containerId, null, true);
      else if (bi) onDrop(null, containerId, bi, true);
    },
  });

  const emptyDropZone = (
    <div style={{ padding: 12, textAlign: 'center', color: T.text.muted, fontSize: 11 }}>
      Drop blocks here
    </div>
  );

  // ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  // BLOCK TYPE RENDERER
  // ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  const renderContent = () => {
    switch (block.type) {

      // ── Text ─────────────────────────────────────────────────
      case 'text': {
        const content = isPreview ? resolveText(block.properties.content, SAMPLE_DATA) : block.properties.content;
        return (
          <div style={{
            ...baseStyle,
            fontFamily: block.properties.fontFamily || T.font,
            fontSize: block.properties.fontSize, fontWeight: block.properties.fontWeight,
            color: block.properties.color, textAlign: block.properties.textAlign,
            lineHeight: block.properties.lineHeight, letterSpacing: block.properties.letterSpacing,
            whiteSpace: 'pre-wrap', wordBreak: 'break-word',
          }}>
            {content || <span style={{ color: '#999', fontStyle: 'italic' }}>Enter text...</span>}
          </div>
        );
      }

      // ── Image ────────────────────────────────────────────────
      case 'image': {
        const src = block.properties.src;
        const keepRatio = block.properties.keepAspectRatio !== false;
        const imgHeight = keepRatio ? 'auto' : (baseStyle.height || 200);
        return (
          <div style={{ ...baseStyle }}>
            {src ? (
              <img src={src} alt={block.properties.alt || ''} style={{
                width: '100%', height: imgHeight,
                objectFit: block.properties.objectFit || 'contain',
                borderRadius: baseStyle.borderRadius, display: 'block',
              }} />
            ) : (
              <div style={{
                width: '100%', height: baseStyle.height || 200,
                backgroundColor: '#1a1f3a',
                display: 'flex', alignItems: 'center', justifyContent: 'center',
                borderRadius: baseStyle.borderRadius || 0, border: '2px dashed #3d4557',
              }}>
                <Image size={32} style={{ color: '#64748b' }} />
              </div>
            )}
          </div>
        );
      }

      // ── Columns ──────────────────────────────────────────────
      case 'columns': {
        const cols = block.children || [];
        const ratios = block.properties.ratios || cols.map(() => 1);
        return (
          <div style={{ ...baseStyle, display: 'flex', gap: block.properties.gap || '16px' }}>
            {cols.map((col, i) => (
              <div
                key={col.id}
                style={{
                  flex: `${ratios[i] || 1} 0 0%`, minHeight: 48, padding: 4,
                  border: isPreview ? 'none' : `1px dashed ${T.border.default}`, borderRadius: 4,
                }}
                {...(!isPreview ? containerDropHandlers(col.id) : {})}
              >
                {(!col.children || col.children.length === 0) && !isPreview && emptyDropZone}
                {renderChildren(col.children)}
              </div>
            ))}
          </div>
        );
      }

      // ── Spacer ───────────────────────────────────────────────
      case 'spacer':
        return <div style={{ height: block.properties.height || 32, ...baseStyle }} />;

      // ── Divider ──────────────────────────────────────────────
      case 'divider':
        return (
          <div style={{ ...baseStyle }}>
            <hr style={{ border: 'none', borderTop: `${block.properties.thickness} ${block.properties.lineStyle} ${block.properties.color}`, margin: 0 }} />
          </div>
        );

      // ── Table ────────────────────────────────────────────────
      case 'table': {
        const { rows, cols, headerRow, cellPadding, borderColor, cells } = block.properties;
        return (
          <TableBlock
            block={block} rows={rows} cols={cols} headerRow={headerRow}
            cellPadding={cellPadding} borderColor={borderColor} cells={cells}
            baseStyle={baseStyle} tv={tv} isPreview={isPreview} onCellEdit={onCellEdit}
          />
        );
      }

      // ── Box (container) ──────────────────────────────────────
      case 'box':
        return (
          <div style={{ ...baseStyle, minHeight: baseStyle.minHeight || 60 }} {...(!isPreview ? containerDropHandlers(block.id) : {})}>
            {(!block.children || block.children.length === 0) && !isPreview && emptyDropZone}
            {renderChildren(block.children)}
          </div>
        );

      // ── Dynamic Field ────────────────────────────────────────
      case 'dynamic_field': {
        let val;
        if (block.properties.dataSource === 'static') {
          val = block.properties.staticValue || '';
        } else {
          val = isPreview
            ? resolveMergeTag(block.properties.fieldPath, SAMPLE_DATA)
            : '{{' + block.properties.fieldPath + '}}';
        }
        return (
          <div style={{
            ...baseStyle,
            fontFamily: isPreview ? (block.properties.fontFamily || 'inherit') : 'monospace',
            fontSize: block.properties.fontSize,
            fontWeight: block.properties.fontWeight,
            fontStyle: block.properties.fontStyle,
            textDecoration: block.properties.textDecoration,
            textAlign: block.properties.textAlign,
            color: isPreview ? block.properties.color : T.accent.orange,
          }}>
            {block.properties.prefix}{val}{block.properties.suffix}
          </div>
        );
      }

      // ── Barcode ──────────────────────────────────────────────
      case 'barcode': {
        const val = isPreview ? resolveText(block.properties.value, SAMPLE_DATA) : (block.properties.value || '0000');
        const w = block.properties.barcodeWidth || '200';
        const h = block.properties.barcodeHeight || '80';
        let svg;
        if (block.properties.barcodeType === 'QR') svg = generateQRSVG(val, h);
        else if (block.properties.barcodeType === 'EAN-13') svg = generateEAN13SVG(val, w, h);
        else svg = generateCode128SVG(val, w, h);
        return <div style={{ ...baseStyle, textAlign: baseStyle.textAlign || 'center' }} dangerouslySetInnerHTML={{ __html: svg }} />;
      }

      // ── Logo ─────────────────────────────────────────────────
      case 'logo': {
        const src = block.properties.src;
        return (
          <div style={{ ...baseStyle }}>
            {src ? (
              <img src={src} alt="Logo" style={{ maxWidth: block.properties.maxWidth, maxHeight: block.properties.maxHeight, display: 'block' }} />
            ) : (
              <div style={{
                maxWidth: block.properties.maxWidth, height: block.properties.maxHeight,
                backgroundColor: '#f0f0f0', display: 'flex', alignItems: 'center', justifyContent: 'center',
                borderRadius: 4, border: '2px dashed #ccc', padding: '8px 24px',
              }}>
                <Building2 size={24} style={{ color: '#999', marginRight: 8 }} />
                <span style={{ color: '#999', fontSize: 12, fontWeight: 500 }}>Company Logo</span>
              </div>
            )}
          </div>
        );
      }

      // ── Repeater ─────────────────────────────────────────────
      case 'repeater': {
        if (isPreview) {
          const items = SAMPLE_DATA[block.properties.dataSource] || SAMPLE_DATA.lines || [];
          return (
            <div style={{ ...baseStyle, display: block.properties.direction === 'horizontal' ? 'flex' : 'block', gap: 4 }}>
              {items.map((item, idx) => {
                const itemData = { ...SAMPLE_DATA, line: item };
                return (
                  <div key={idx} style={{ flex: block.properties.direction === 'horizontal' ? 1 : undefined }}>
                    {(block.children || []).map((child) => (
                      <RepeaterChildPreview key={`${child.id}_${idx}`} block={child} data={itemData} theme={theme} />
                    ))}
                  </div>
                );
              })}
            </div>
          );
        }
        return (
          <div
            style={{ ...baseStyle, border: `2px dashed ${T.accent.teal}`, borderRadius: 6, position: 'relative' }}
            {...containerDropHandlers(block.id)}
          >
            <div style={{
              position: 'absolute', top: -10, left: 12,
              backgroundColor: T.bg.primary, padding: '0 6px',
              fontSize: 10, fontWeight: 600, color: T.accent.teal,
              display: 'flex', alignItems: 'center', gap: 4,
            }}>
              <Repeat size={10} /> REPEATER ({block.properties.dataSource})
            </div>
            <div style={{ padding: '12px 8px 8px' }}>
              {(!block.children || block.children.length === 0) && (
                <div style={{ padding: 16, textAlign: 'center', color: T.text.muted, fontSize: 11 }}>
                  Drop blocks here — they repeat per line item
                </div>
              )}
              {renderChildren(block.children)}
            </div>
          </div>
        );
      }

      // ── Page Break ───────────────────────────────────────────
      case 'page_break':
        return (
          <div style={{ ...baseStyle, position: 'relative', padding: '16px 0' }}>
            <div style={{ borderTop: `2px dashed ${isPreview ? '#ccc' : T.accent.purple}`, position: 'relative' }}>
              {!isPreview && (
                <span style={{
                  position: 'absolute', top: -9, left: '50%', transform: 'translateX(-50%)',
                  backgroundColor: T.bg.primary, padding: '0 12px',
                  fontSize: 10, fontWeight: 700, color: T.accent.purple,
                  textTransform: 'uppercase', display: 'flex', alignItems: 'center', gap: 4, whiteSpace: 'nowrap',
                }}>
                  <Scissors size={10} /> Page Break
                </span>
              )}
            </div>
          </div>
        );

      // ── Unordered List ───────────────────────────────────────
      case 'unordered_list': {
        const items = block.properties.items || [];
        return (
          <ul style={{
            ...baseStyle,
            fontSize: block.properties.fontSize || '14px',
            color: block.properties.color || '#333333',
            fontFamily: block.properties.fontFamily || 'inherit',
            margin: 0,
            paddingLeft: 24,
          }}>
            {items.map((item, idx) => (
              <li key={idx} style={{ marginBottom: 4 }}>{item}</li>
            ))}
            {items.length === 0 && !isPreview && (
              <li style={{ color: T.text.muted, fontStyle: 'italic' }}>No items — add some in the properties panel</li>
            )}
          </ul>
        );
      }

      // ── Ordered List ─────────────────────────────────────────
      case 'ordered_list': {
        const items = block.properties.items || [];
        return (
          <ol style={{
            ...baseStyle,
            fontSize: block.properties.fontSize || '14px',
            color: block.properties.color || '#333333',
            fontFamily: block.properties.fontFamily || 'inherit',
            margin: 0,
            paddingLeft: 24,
          }}>
            {items.map((item, idx) => (
              <li key={idx} style={{ marginBottom: 4 }}>{item}</li>
            ))}
            {items.length === 0 && !isPreview && (
              <li style={{ color: T.text.muted, fontStyle: 'italic' }}>No items — add some in the properties panel</li>
            )}
          </ol>
        );
      }

      default:
        return <div style={{ ...baseStyle, padding: 12, color: '#999' }}>[{block.type}]</div>;
    }
  };

  // ── Outer wrapper ────────────────────────────────────────────
  return (
    <div
      draggable={!isPreview && !block.locked}
      onDragStart={handleDragStart}
      onDragOver={!isPreview ? handleDragOver : undefined}
      onDragLeave={() => setDragOverId(null)}
      onDrop={!isPreview ? handleDrop : undefined}
      onClick={(e) => { e.stopPropagation(); !isPreview && onSelect(block.id); }}
      onContextMenu={handleCtxMenu}
      style={isPreview ? {} : wrapperStyle}
      role={isPreview ? undefined : 'button'}
      aria-label={`${block.type.replace('_', ' ')} block${block.label ? ': ' + block.label : ''}`}
      aria-selected={isSelected}
    >
      {selectionBadge}
      {renderContent()}
    </div>
  );
}

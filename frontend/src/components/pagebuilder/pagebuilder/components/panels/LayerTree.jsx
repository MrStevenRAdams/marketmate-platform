// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// LAYER TREE (Feature #13: Move Up/Down, #29: Search, #30: A11y)
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

import React, { useState, useCallback, useRef, useEffect, useMemo } from 'react';
import { Eye, EyeOff, Lock, Unlock, GripVertical, Filter, ArrowUp, ArrowDown, Square } from 'lucide-react';
import { T, BLOCK_TYPES } from '../../../constants/index.jsx';
import { css } from '../ui/index.jsx';

export default function LayerTree({ blocks, selectedId, onSelect, onToggleVisible, onToggleLock, onMoveBlock, searchTerm = '' }) {
  const [focusedId, setFocusedId] = useState(null);
  const [collapsed, setCollapsed] = useState({});
  const treeRef = useRef(null);

  const getBlockInfo = (type) =>
    BLOCK_TYPES.find((b) => b.type === type) || { icon: Square, color: T.text.muted, label: type };

  const getLabel = (block) => {
    if (block.label) return block.label;
    if (block.type === 'text') return (block.properties?.content || '').substring(0, 24) || 'Text';
    if (block.type === 'dynamic_field') return `{{${block.properties?.fieldPath || '?'}}}`;
    if (block.type === '_column') return 'Column';
    if (block.type === 'page_break') return '--- Page Break ---';
    if (block.type === 'repeater') return `Repeater (${block.properties?.dataSource || 'lines'})`;
    return getBlockInfo(block.type).label;
  };

  // ── Feature #29: Search matching ───────────────────────────
  const term = searchTerm.toLowerCase().trim();

  const { matchIds, visibleIds } = useMemo(() => {
    if (!term) return { matchIds: new Set(), visibleIds: null };
    const matches = new Set();
    const ancestors = new Set();

    const walk = (bks, parentPath = []) => {
      for (const b of bks) {
        const info = getBlockInfo(b.type);
        const label = getLabel(b);
        const searchable = [
          info.label, label,
          b.type === 'text' ? (b.properties?.content || '') : '',
          b.type === 'dynamic_field' ? (b.properties?.fieldPath || '') : '',
        ].join(' ').toLowerCase();

        if (searchable.includes(term)) {
          matches.add(b.id);
          parentPath.forEach((pid) => ancestors.add(pid));
        }
        if (b.children) walk(b.children, [...parentPath, b.id]);
      }
    };
    walk(blocks);
    const visible = new Set([...matches, ...ancestors]);
    return { matchIds: matches, visibleIds: visible };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [blocks, term]);

  // ── Flatten tree for keyboard navigation ───────────────────
  const flatList = useMemo(() => {
    const list = [];
    const walk = (bks, depth = 0) => {
      for (const b of bks) {
        if (visibleIds && !visibleIds.has(b.id)) continue;
        list.push({ id: b.id, depth });
        const isCol = collapsed[b.id] && !term;
        if (b.children && !isCol) walk(b.children, depth + 1);
      }
    };
    walk(blocks);
    return list;
  }, [blocks, collapsed, visibleIds, term]);

  // ── Highlight matching text ────────────────────────────────
  const highlightText = (text) => {
    if (!term || !text) return text;
    const str = String(text);
    const idx = str.toLowerCase().indexOf(term);
    if (idx === -1) return str;
    return (
      <span>
        {str.substring(0, idx)}
        <span style={{ backgroundColor: `${T.primary.base}33`, borderRadius: 2, padding: '0 1px' }}>
          {str.substring(idx, idx + term.length)}
        </span>
        {str.substring(idx + term.length)}
      </span>
    );
  };

  // ── Keyboard navigation (Feature #30) ──────────────────────
  const handleKeyDown = useCallback((e) => {
    if (!flatList.length) return;
    const currentIdx = flatList.findIndex((f) => f.id === focusedId);

    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault();
        if (currentIdx < flatList.length - 1) setFocusedId(flatList[currentIdx + 1].id);
        else if (currentIdx === -1) setFocusedId(flatList[0].id);
        break;
      case 'ArrowUp':
        e.preventDefault();
        if (currentIdx > 0) setFocusedId(flatList[currentIdx - 1].id);
        break;
      case 'ArrowRight':
        e.preventDefault();
        if (focusedId && collapsed[focusedId]) setCollapsed((c) => ({ ...c, [focusedId]: false }));
        break;
      case 'ArrowLeft':
        e.preventDefault();
        if (focusedId && !collapsed[focusedId]) setCollapsed((c) => ({ ...c, [focusedId]: true }));
        break;
      case 'Enter':
        e.preventDefault();
        if (focusedId) onSelect(focusedId);
        break;
      default:
        break;
    }
  }, [flatList, focusedId, collapsed, onSelect]);

  useEffect(() => {
    if (!focusedId || !treeRef.current) return;
    const el = treeRef.current.querySelector(`[data-block-id="${focusedId}"]`);
    if (el) el.scrollIntoView({ block: 'nearest' });
  }, [focusedId]);

  const renderNode = (block, depth = 0) => {
    if (visibleIds && !visibleIds.has(block.id)) return null;

    const info = getBlockInfo(block.type);
    const Icon = info.icon;
    const isSelected = block.id === selectedId;
    const isFocused = block.id === focusedId;
    const isMatch = matchIds.has(block.id);
    const hasConditions = block.conditions && block.conditions.filter((c) => c?.field).length > 0;
    const hasChildren = block.children && block.children.length > 0;
    const isNodeCollapsed = collapsed[block.id] && !term;

    return (
      <div key={block.id}>
        <div
          data-block-id={block.id}
          onClick={() => onSelect(block.id)}
          role="treeitem"
          aria-selected={isSelected}
          aria-expanded={hasChildren ? !isNodeCollapsed : undefined}
          aria-label={`${info.label}: ${getLabel(block)}`}
          tabIndex={isFocused ? 0 : -1}
          style={{
            display: 'flex', alignItems: 'center', gap: 4,
            padding: `4px 8px 4px ${12 + depth * 16}px`,
            backgroundColor: isSelected ? T.bg.elevated : isMatch ? `${T.primary.base}11` : 'transparent',
            borderLeft: isSelected ? `3px solid ${T.primary.base}` : '3px solid transparent',
            cursor: 'pointer', transition: 'all 150ms',
            fontSize: 12, color: T.text.primary,
            outline: isFocused ? `2px solid ${T.primary.base}` : 'none',
            outlineOffset: -2,
            boxShadow: isFocused ? `0 0 0 2px ${T.primary.glow}` : 'none',
          }}
          onMouseEnter={(e) => { if (!isSelected) e.currentTarget.style.backgroundColor = T.bg.tertiary; }}
          onMouseLeave={(e) => { if (!isSelected) e.currentTarget.style.backgroundColor = isMatch ? `${T.primary.base}11` : 'transparent'; }}
        >
          {hasChildren && !term ? (
            <span
              onClick={(e) => { e.stopPropagation(); setCollapsed((c) => ({ ...c, [block.id]: !c[block.id] })); }}
              style={{ cursor: 'pointer', fontSize: 10, color: T.text.muted, width: 12, textAlign: 'center', flexShrink: 0, userSelect: 'none' }}
            >
              {isNodeCollapsed ? '▶' : '▼'}
            </span>
          ) : (
            <GripVertical size={10} style={{ color: T.text.muted, cursor: 'grab', flexShrink: 0 }} />
          )}
          <Icon size={12} style={{ color: info.color, flexShrink: 0 }} />
          <span style={{ flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', fontSize: 11 }}>
            {term ? highlightText(getLabel(block)) : getLabel(block)}
          </span>
          {hasConditions && <Filter size={9} style={{ color: T.status.warning, flexShrink: 0 }} />}
          {isSelected && onMoveBlock && (
            <>
              <button onClick={(e) => { e.stopPropagation(); onMoveBlock(block.id, 'up'); }} style={{ ...css.iconBtn, width: 18, height: 18 }} title="Move Up" aria-label="Move block up"><ArrowUp size={10} /></button>
              <button onClick={(e) => { e.stopPropagation(); onMoveBlock(block.id, 'down'); }} style={{ ...css.iconBtn, width: 18, height: 18 }} title="Move Down" aria-label="Move block down"><ArrowDown size={10} /></button>
            </>
          )}
          <button onClick={(e) => { e.stopPropagation(); onToggleVisible(block.id); }} style={{ ...css.iconBtn, width: 20, height: 20, opacity: 0.6 }} aria-label={block.visible ? 'Hide block' : 'Show block'}>
            {block.visible ? <Eye size={10} /> : <EyeOff size={10} />}
          </button>
          <button onClick={(e) => { e.stopPropagation(); onToggleLock(block.id); }} style={{ ...css.iconBtn, width: 20, height: 20, opacity: 0.6 }} aria-label={block.locked ? 'Unlock block' : 'Lock block'}>
            {block.locked ? <Lock size={10} /> : <Unlock size={10} />}
          </button>
        </div>
        {hasChildren && !isNodeCollapsed && block.children.map((child) => renderNode(child, depth + 1))}
      </div>
    );
  };

  return (
    <div
      ref={treeRef}
      style={{ flex: 1, overflowY: 'auto' }}
      role="tree"
      aria-label="Block layer tree"
      tabIndex={0}
      onKeyDown={handleKeyDown}
      onFocus={() => { if (!focusedId && flatList.length) setFocusedId(flatList[0].id); }}
    >
      {blocks.length === 0 && (
        <div style={{ padding: 16, textAlign: 'center', color: T.text.muted, fontSize: 12 }}>
          No blocks yet. Drag or click to add.
        </div>
      )}
      {term && visibleIds && visibleIds.size === 0 && (
        <div style={{ padding: 16, textAlign: 'center', color: T.text.muted, fontSize: 12 }}>
          No blocks match "{searchTerm}"
        </div>
      )}
      {blocks.map((b) => renderNode(b))}
    </div>
  );
}

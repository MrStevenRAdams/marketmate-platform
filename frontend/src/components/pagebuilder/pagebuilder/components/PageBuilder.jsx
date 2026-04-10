// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// MAIN PAGE BUILDER
// Top-level component that wires together the three-panel layout:
//   Left sidebar  → Block palette, Layer tree, Themes, Versions
//   Centre        → Zoomable canvas with block rendering
//   Right sidebar → Property editor / Canvas settings
//
// All state lives here and is passed down via props. Block tree
// mutations go through pushBlocks() to maintain undo/redo history.
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

import React, { useState, useCallback, useRef, useEffect, useMemo } from 'react';
import {
  Layers, ZoomIn, ZoomOut, Undo2, Redo2, Save, Eye, Monitor,
  Code, Download, Upload, Plus, Palette, GitBranch, Settings,
  Copy, Trash2, ArrowUp, ArrowDown, Clipboard, Paintbrush,
  Lock, Unlock, EyeOff, Grid3x3, Ruler, Crosshair, X,
  FileText, Tablet, Smartphone, Search,
  AlignLeft, AlignCenter, AlignRight, AlignJustify, Maximize2,
} from 'lucide-react';

// ── Constants ──────────────────────────────────────────────────
import { T, THEME_PRESETS, SAMPLE_DATA, CANVAS_PRESETS, PAGE_SIZE_PRESETS, BLOCK_TYPES } from '../../constants/index.jsx';

// ── Utilities ──────────────────────────────────────────────────
import {
  uid, deepClone, unitToPx,
  findBlockById, updateBlockInTree, removeBlockFromTree,
  insertBlockInTree, countAllBlocks, moveBlockInTree,
  createDefaultBlock,
} from '../../utils/index.jsx';

// ── Hooks ──────────────────────────────────────────────────────
import useHistory from '../../hooks/useHistory.jsx';

// ── Components ─────────────────────────────────────────────────
import { css, ColorInput, PropInput, ContextMenu } from './ui/index.jsx';
import { BlockPalette, LayerTree, PropertyEditor, ThemePicker, VersionPanel } from './panels/index.jsx';
import { CanvasBlock, Rulers, GridOverlay, GuideLines, snapToGrid, RULER_THICKNESS } from './canvas/index.jsx';
import { AIContentModal, ExportModal, StarterTemplatesModal } from './modals/index.jsx';
import { generateFullHTML } from '../../serialisers/htmlSerialiser.jsx';

// ── Left sidebar tab definitions ───────────────────────────────
const LEFT_TABS = [
  { key: 'blocks',   label: 'Blocks',   icon: Plus },
  { key: 'layers',   label: 'Layers',   icon: Layers },
  { key: 'themes',   label: 'Themes',   icon: Palette },
  { key: 'versions', label: 'Versions', icon: GitBranch },
];

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// COMPONENT
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

export default function PageBuilder() {
  // ── Template metadata ────────────────────────────────────────
  const [templateName, setTemplateName]       = useState('Untitled Template');
  const [templateType, setTemplateType]       = useState('invoice');
  const [outputFormat, setOutputFormat]       = useState('PDF');
  const [templateVersion, setTemplateVersion] = useState(1);
  const [templateId, setTemplateId]           = useState(null); // null = unsaved new template
  const [versionHistory, setVersionHistory]   = useState([]);
  const [saveError, setSaveError]             = useState('');

  // u2500u2500 Virtual printer & print behaviour u2500u2500u2500u2500u2500u2500u2500u2500u2500u2500u2500u2500u2500u2500u2500u2500u2500u2500u2500u2500u2500u2500u2500
  const [virtualPrinter, setVirtualPrinter]   = useState('');
  const [printBehaviour, setPrintBehaviour]   = useState('manual_only');

  // u2500u2500 Canvas orientation u2500u2500u2500u2500u2500u2500u2500u2500u2500u2500u2500u2500u2500u2500u2500u2500u2500u2500u2500u2500u2500u2500u2500u2500u2500u2500u2500u2500u2500u2500u2500u2500
  const [orientation, setOrientation]         = useState('portrait');

  // ── Canvas settings ──────────────────────────────────────────
  const [canvas, setCanvas] = useState({
    width: 210, height: 297, unit: 'mm', backgroundColor: '#ffffff',
  });

  // ── Theme ────────────────────────────────────────────────────
  const [activeTheme, setActiveTheme] = useState('default');
  const theme = THEME_PRESETS[activeTheme] || THEME_PRESETS.default;

  // ── Block tree (with undo/redo) ──────────────────────────────
  const {
    current: blocks, push: pushBlocks,
    undo, redo, canUndo, canRedo, historyLength, pointer,
  } = useHistory([]);

  // ── UI state ─────────────────────────────────────────────────
  const [selectedId, setSelectedId]   = useState(null);
  const [isPreview, setIsPreview]     = useState(false);
  const [zoom, setZoom]               = useState(0.75);
  const [dragOverId, setDragOverId]   = useState(null);
  const [leftTab, setLeftTab]         = useState('blocks');
  const [centreTab, setCentreTab]     = useState('canvas'); // 'canvas' | 'html'
  const [htmlCopied, setHtmlCopied]   = useState(false);
  const [saved, setSaved]             = useState(false);
  const [showAI, setShowAI]           = useState(null);
  const [showExport, setShowExport]   = useState(false);
  const [contextMenu, setContextMenu] = useState(null);
  const [copiedStyle, setCopiedStyle] = useState(null);

  // ── Feature #31: Starter Templates ─────────────────────────
  const [showStarters, setShowStarters] = useState(false);

  // ── Feature #24: Responsive Preview ────────────────────────
  const [previewDevice, setPreviewDevice] = useState('desktop');

  // ── Feature #29: Block Search ──────────────────────────────
  const [blockSearchTerm, setBlockSearchTerm] = useState('');

  // ── Feature #30: Accessibility ─────────────────────────────
  const [announcement, setAnnouncement] = useState('');
  const announce = useCallback((msg) => {
    setAnnouncement('');
    setTimeout(() => setAnnouncement(msg), 50);
  }, []);

  // ── Refs ─────────────────────────────────────────────────────
  const fileInputRef = useRef(null);
  const canvasRef    = useRef(null);
  const scrollRef    = useRef(null);

  // ── Grid & Rulers state ────────────────────────────────────────
  const [showRulers, setShowRulers]     = useState(true);
  const [showGrid, setShowGrid]         = useState(false);
  const [snapEnabled, setSnapEnabled]   = useState(false);
  const [gridSpacing, setGridSpacing]   = useState(10);  // in canvas units
  const [gridStyle, setGridStyle]       = useState('lines'); // 'lines' | 'dots' | 'crosses'
  const [guides, setGuides]             = useState([]);  // [{ axis: 'x'|'y', position }]
  const [draggingGuide, setDraggingGuide] = useState(null); // { axis, position } during ruler drag

  const addGuide = useCallback((axis, position) => {
    setGuides((prev) => [...prev, { axis, position }]);
  }, []);

  const removeGuide = useCallback((index) => {
    setGuides((prev) => prev.filter((_, i) => i !== index));
  }, []);

  const clearGuides = useCallback(() => setGuides([]), []);

  // ── Derived state ────────────────────────────────────────────
  const selectedBlock = useMemo(
    () => (selectedId ? findBlockById(blocks, selectedId) : null),
    [blocks, selectedId]
  );
  const canvasWidthPx  = unitToPx(canvas.width, canvas.unit);
  const canvasHeightPx = canvas.height === 'auto' ? 'auto' : unitToPx(canvas.height, canvas.unit);

  // Split blocks into pages at page_break blocks (preview only)
  const pages = useMemo(() => {
    if (!isPreview) return [blocks];
    const result = [[]];
    for (const b of blocks) {
      if (b.type === 'page_break') result.push([]);
      else result[result.length - 1].push(b);
    }
    return result;
  }, [blocks, isPreview]);

  // ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  // ACTIONS
  // ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  // ── Apply a canvas preset when template type changes ─────────
  const applyPreset = (type) => {
    setTemplateType(type);
    const preset = CANVAS_PRESETS[type];
    if (preset) {
      let w = preset.width;
      let h = preset.height;
      // Respect current orientation when applying preset
      if (orientation === 'landscape' && h !== 'auto' && typeof h === 'number' && w < h) {
        [w, h] = [h, w];
      }
      setCanvas({
        width: w, height: h, unit: preset.unit,
        backgroundColor: canvas.backgroundColor || '#ffffff',
      });
      setOutputFormat(type === 'email' || type === 'ebay_listing' ? 'HTML' : 'PDF');
    }
  };

  // Toggle portrait/landscape — swaps canvas width/height for document canvases
  const toggleOrientation = (newOrientation) => {
    if (newOrientation === orientation) return;
    setOrientation(newOrientation);
    if (canvas.height !== 'auto' && typeof canvas.height === 'number') {
      setCanvas((c) => ({ ...c, width: c.height, height: c.width }));
    }
  };

  // ── Add a new block to the tree ──────────────────────────────
  const addBlock = useCallback((type, afterId = null, insideContainer = false) => {
    const nb = createDefaultBlock(type);
    let newBlocks;
    if (insideContainer && afterId) {
      newBlocks = updateBlockInTree(blocks, afterId, (b) => ({
        ...b, children: [...(b.children || []), nb],
      }));
    } else if (afterId) {
      newBlocks = insertBlockInTree(blocks, nb, afterId, 'after');
    } else {
      newBlocks = [...blocks, nb];
    }
    pushBlocks(newBlocks);
    setSelectedId(nb.id);
    announce(`Added ${type.replace('_', ' ')} block`);
  }, [blocks, pushBlocks, announce]);

  // ── Starter template selection (Feature #31) ─────────────────
  const handleSelectStarter = useCallback((template) => {
    pushBlocks(template.blocks);
    setCanvas(template.canvas);
    setActiveTheme(template.theme);
    setTemplateType(template.type);
    setTemplateName(template.name);
    setOutputFormat(template.type === 'email' || template.type === 'ebay_listing' ? 'HTML' : 'PDF');
    setSelectedId(null);
  }, [pushBlocks]);

  // ── Handle drops on canvas (from palette or reorder) ─────────
  const handleCanvasDrop = useCallback((blockType, targetId, dragBlockId, inside = false) => {
    if (blockType) {
      addBlock(blockType, targetId, inside);
    } else if (dragBlockId && targetId && dragBlockId !== targetId) {
      const db = findBlockById(blocks, dragBlockId);
      if (!db) return;
      let nb = removeBlockFromTree(blocks, dragBlockId);
      if (inside) {
        nb = updateBlockInTree(nb, targetId, (b) => ({
          ...b, children: [...(b.children || []), deepClone(db)],
        }));
      } else {
        nb = insertBlockInTree(nb, deepClone(db), targetId, 'after');
      }
      pushBlocks(nb);
    }
  }, [blocks, pushBlocks, addBlock]);

  // ── Drop on empty canvas area ────────────────────────────────
  const handleCanvasAreaDrop = useCallback((e) => {
    e.preventDefault();
    setDragOverId(null);
    const bt = e.dataTransfer.getData('application/x-block-type');
    if (bt) addBlock(bt);
  }, [addBlock]);

  // ── Block CRUD operations ────────────────────────────────────
  const updateBlock = useCallback(
    (id, updater) => pushBlocks(updateBlockInTree(blocks, id, updater)),
    [blocks, pushBlocks]
  );

  const deleteBlock = useCallback((id) => {
    const block = findBlockById(blocks, id);
    pushBlocks(removeBlockFromTree(blocks, id));
    if (selectedId === id) setSelectedId(null);
    if (block) announce(`Deleted ${block.type.replace('_', ' ')} block`);
  }, [blocks, pushBlocks, selectedId, announce]);

  const duplicateBlock = useCallback((id) => {
    const block = findBlockById(blocks, id);
    if (!block) return;
    const clone = deepClone(block);
    const reId = (b) => { b.id = uid(); (b.children || []).forEach(reId); };
    reId(clone);
    pushBlocks(insertBlockInTree(blocks, clone, id, 'after'));
    setSelectedId(clone.id);
  }, [blocks, pushBlocks]);

  const toggleVisible = useCallback(
    (id) => pushBlocks(updateBlockInTree(blocks, id, (b) => ({ ...b, visible: !b.visible }))),
    [blocks, pushBlocks]
  );

  const toggleLock = useCallback(
    (id) => pushBlocks(updateBlockInTree(blocks, id, (b) => ({ ...b, locked: !b.locked }))),
    [blocks, pushBlocks]
  );

  const handleMoveBlock = useCallback((id, dir) => {
    const nb = moveBlockInTree(blocks, id, dir);
    if (nb !== blocks) pushBlocks(nb);
  }, [blocks, pushBlocks]);

  const handleCellEdit = useCallback((blockId, cellKey, value) => {
    pushBlocks(updateBlockInTree(blocks, blockId, (b) => ({
      ...b, properties: { ...b.properties, cells: { ...b.properties.cells, [cellKey]: value } },
    })));
  }, [blocks, pushBlocks]);

  // ── Save ─────────────────────────────────────────────────────
  const handleSave = async () => {
    const nv = templateVersion + 1;
    const id = templateId || ('tpl_' + Date.now().toString(36));
    const tpl = {
      id,
      name: templateName, type: templateType,
      output_format: outputFormat,
      canvas, blocks, theme: activeTheme, version: nv,
      virtual_printer: virtualPrinter,
      print_behaviour: printBehaviour,
      orientation,
      grid: { showRulers, showGrid, snapEnabled, gridSpacing, gridStyle, guides },
      history: [{
        version: nv,
        savedAt: new Date().toLocaleTimeString(),
        blockCount: countAllBlocks(blocks),
      }, ...versionHistory].slice(0, 50),
      createdAt: versionHistory.length === 0
        ? new Date().toISOString()
        : versionHistory[0]?.createdAt || new Date().toISOString(),
      updatedAt: new Date().toISOString(),
    };

    const API_URL = import.meta.env?.VITE_API_URL || 'http://localhost:8080/api/v1';
    const tenantId = localStorage.getItem('marketmate_active_tenant') || '';

    try {
      setSaveError('');
      const isNew = !templateId;
      const url = isNew ? `${API_URL}/templates` : `${API_URL}/templates/${id}`;
      const method = isNew ? 'POST' : 'PUT';

      const res = await fetch(url, {
        method,
        headers: {
          'Content-Type': 'application/json',
          'X-Tenant-Id': tenantId,
        },
        body: JSON.stringify(tpl),
      });

      if (!res.ok) {
        const err = await res.json().catch(() => ({}));
        setSaveError(err.error || `Save failed (${res.status})`);
        return;
      }

      const data = await res.json();
      const saved_tpl = data.template || tpl;

      if (!templateId) setTemplateId(saved_tpl.id || id);
      setTemplateVersion(nv);
      setVersionHistory((prev) => [{
        version: nv,
        savedAt: new Date().toLocaleTimeString(),
        blockCount: countAllBlocks(blocks),
      }, ...prev].slice(0, 50));
      setSaved(true);
      setTimeout(() => setSaved(false), 2000);
    } catch (err) {
      setSaveError('Failed to save — check your connection');
    }
  };

  // ── Load template from backend ────────────────────────────────
  const handleLoadTemplate = async (id) => {
    const API_URL = import.meta.env?.VITE_API_URL || 'http://localhost:8080/api/v1';
    const tenantId = localStorage.getItem('marketmate_active_tenant') || '';
    try {
      const res = await fetch(`${API_URL}/templates/${id}`, {
        headers: { 'X-Tenant-Id': tenantId },
      });
      if (!res.ok) return;
      const data = await res.json();
      const tpl = data.template;
      if (!tpl) return;
      pushBlocks(tpl.blocks || []);
      setTemplateName(tpl.name || 'Untitled');
      setTemplateType(tpl.type || 'custom');
      setOutputFormat(tpl.output_format || 'PDF');
      if (tpl.canvas) setCanvas(tpl.canvas);
      if (tpl.theme) setActiveTheme(tpl.theme);
      setTemplateVersion(tpl.version || 1);
      setTemplateId(tpl.id);
      setVersionHistory(tpl.history || []);
      if (tpl.virtual_printer !== undefined) setVirtualPrinter(tpl.virtual_printer || '');
      if (tpl.print_behaviour) setPrintBehaviour(tpl.print_behaviour);
      if (tpl.orientation) setOrientation(tpl.orientation);
    } catch (err) {
      console.error('Failed to load template:', err);
    }
  };

  // ── Export / Import JSON ─────────────────────────────────────
  const handleExportJSON = () => {
    const tpl = {
      id: 'tpl_' + Date.now().toString(36),
      name: templateName, type: templateType,
      canvas, blocks, theme: activeTheme, version: templateVersion,
      grid: { showRulers, showGrid, snapEnabled, gridSpacing, gridStyle, guides },
      virtual_printer: virtualPrinter, print_behaviour: printBehaviour, orientation,
      createdAt: new Date().toISOString(),
      updatedAt: new Date().toISOString(),
    };
    const blob = new Blob([JSON.stringify(tpl, null, 2)], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `${templateName.replace(/\s+/g, '_').toLowerCase()}.json`;
    a.click();
    URL.revokeObjectURL(url);
  };

  const handleImportJSON = (e) => {
    const file = e.target.files?.[0];
    if (!file) return;
    const reader = new FileReader();
    reader.onload = (ev) => {
      try {
        const tpl = JSON.parse(ev.target.result);
        if (tpl.blocks) {
          pushBlocks(tpl.blocks);
          setTemplateName(tpl.name || 'Imported');
          setTemplateType(tpl.type || 'custom');
          if (tpl.canvas) setCanvas(tpl.canvas);
          if (tpl.theme) setActiveTheme(tpl.theme);
          if (tpl.version) setTemplateVersion(tpl.version);
          if (tpl.grid) {
            if (tpl.grid.showRulers !== undefined) setShowRulers(tpl.grid.showRulers);
            if (tpl.grid.showGrid !== undefined) setShowGrid(tpl.grid.showGrid);
            if (tpl.grid.snapEnabled !== undefined) setSnapEnabled(tpl.grid.snapEnabled);
            if (tpl.grid.gridSpacing !== undefined) setGridSpacing(tpl.grid.gridSpacing);
            if (tpl.grid.gridStyle !== undefined) setGridStyle(tpl.grid.gridStyle);
            if (tpl.grid.guides) setGuides(tpl.grid.guides);
          if (tpl.virtual_printer !== undefined) setVirtualPrinter(tpl.virtual_printer || '');
          if (tpl.print_behaviour) setPrintBehaviour(tpl.print_behaviour);
          if (tpl.orientation) setOrientation(tpl.orientation);
          }
        }
      } catch (err) {
        console.error('Invalid JSON', err);
      }
    };
    reader.readAsText(file);
    e.target.value = '';
  };

  // ── Context menu ─────────────────────────────────────────────
  const handleContextMenu = (x, y, blockId) => setContextMenu({ x, y, blockId });

  const getContextActions = (blockId) => {
    const block = findBlockById(blocks, blockId);
    if (!block) return [];
    return [
      { label: 'Duplicate', icon: Copy, onClick: () => duplicateBlock(blockId), shortcut: 'Ctrl+D' },
      { label: 'Move Up', icon: ArrowUp, onClick: () => handleMoveBlock(blockId, 'up') },
      { label: 'Move Down', icon: ArrowDown, onClick: () => handleMoveBlock(blockId, 'down') },
      { divider: true },
      { label: 'Copy Style', icon: Clipboard, onClick: () => setCopiedStyle({
        style: deepClone(block.style),
        props: block.type === 'text' ? { fontSize: block.properties.fontSize, fontWeight: block.properties.fontWeight, color: block.properties.color } : null,
      })},
      { label: 'Paste Style', icon: Paintbrush, onClick: () => {
        if (!copiedStyle) return;
        pushBlocks(updateBlockInTree(blocks, blockId, (b) => {
          const nb = { ...b, style: { ...b.style, ...copiedStyle.style } };
          if (copiedStyle.props && b.type === 'text') nb.properties = { ...b.properties, ...copiedStyle.props };
          return nb;
        }));
      }, disabled: !copiedStyle },
      { divider: true },
      { label: block.locked ? 'Unlock' : 'Lock', icon: block.locked ? Unlock : Lock, onClick: () => toggleLock(blockId) },
      { label: block.visible ? 'Hide' : 'Show', icon: block.visible ? EyeOff : Eye, onClick: () => toggleVisible(blockId) },
      { divider: true },
      { label: 'Delete', icon: Trash2, onClick: () => deleteBlock(blockId), shortcut: 'Del', danger: true },
    ];
  };

  // ── Keyboard shortcuts ───────────────────────────────────────
  useEffect(() => {
    const handler = (e) => {
      const isEditing = document.activeElement.matches('input, textarea, select, [contenteditable]');

      // Delete selected block
      if ((e.key === 'Delete' || e.key === 'Backspace') && selectedId && !isEditing) {
        e.preventDefault();
        deleteBlock(selectedId);
      }

      // Undo
      if (e.key === 'z' && (e.ctrlKey || e.metaKey) && !e.shiftKey) {
        e.preventDefault();
        undo();
      }

      // Redo (Ctrl+Y or Ctrl+Shift+Z)
      if ((e.key === 'y' && (e.ctrlKey || e.metaKey)) || (e.key === 'z' && (e.ctrlKey || e.metaKey) && e.shiftKey)) {
        e.preventDefault();
        redo();
      }

      // Duplicate
      if (e.key === 'd' && (e.ctrlKey || e.metaKey)) {
        e.preventDefault();
        if (selectedId) duplicateBlock(selectedId);
      }

      // Escape — deselect
      if (e.key === 'Escape') {
        setSelectedId(null);
        setContextMenu(null);
      }

      // Toggle grid (Ctrl+G)
      if (e.key === 'g' && (e.ctrlKey || e.metaKey) && !e.shiftKey && !isEditing) {
        e.preventDefault();
        setShowGrid((v) => !v);
      }

      // Toggle snap (Ctrl+Shift+G)
      if (e.key === 'g' && (e.ctrlKey || e.metaKey) && e.shiftKey && !isEditing) {
        e.preventDefault();
        setSnapEnabled((v) => !v);
      }

      // Toggle rulers (Ctrl+R)
      if (e.key === 'r' && (e.ctrlKey || e.metaKey) && !isEditing) {
        e.preventDefault();
        setShowRulers((v) => !v);
      }

      // Copy block
      if (e.key === 'c' && (e.ctrlKey || e.metaKey) && selectedId && !isEditing) {
        const blk = findBlockById(blocks, selectedId);
        if (blk) setCopiedStyle({ block: deepClone(blk) });
      }

      // Paste block
      if (e.key === 'v' && (e.ctrlKey || e.metaKey) && copiedStyle?.block && !isEditing) {
        e.preventDefault();
        const clone = deepClone(copiedStyle.block);
        const reId = (b) => { b.id = uid(); (b.children || []).forEach(reId); };
        reId(clone);
        pushBlocks(selectedId ? insertBlockInTree(blocks, clone, selectedId, 'after') : [...blocks, clone]);
        setSelectedId(clone.id);
      }
    };

    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [selectedId, deleteBlock, undo, redo, duplicateBlock, blocks, pushBlocks, copiedStyle, setShowGrid, setSnapEnabled, setShowRulers]);

  // ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  // RENDER
  // ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  return (
    <div style={{
      width: '100%', height: '100vh',
      display: 'flex', flexDirection: 'column',
      backgroundColor: T.bg.primary, color: T.text.primary,
      fontFamily: T.font, fontSize: 14, overflow: 'hidden',
    }}>
      {/* ── Modals ──────────────────────────────────────────── */}
      {/* Feature #30: Screen reader announcements */}
      <div aria-live="polite" aria-atomic="true" style={{
        position: 'absolute', width: 1, height: 1, overflow: 'hidden',
        clip: 'rect(0, 0, 0, 0)', whiteSpace: 'nowrap', border: 0,
      }}>
        {announcement}
      </div>

      {/* Feature #30: Skip to canvas link */}
      <a
        href="#canvas-area"
        style={{
          position: 'absolute', left: -9999, top: 0, zIndex: 9999,
          padding: '8px 16px', backgroundColor: T.primary.base, color: '#fff',
          fontSize: 13, fontWeight: 600, borderRadius: T.radius.md,
        }}
        onFocus={(e) => { e.currentTarget.style.left = '8px'; e.currentTarget.style.top = '8px'; }}
        onBlur={(e) => { e.currentTarget.style.left = '-9999px'; }}
      >
        Skip to canvas
      </a>

      {showAI && (
        <AIContentModal
          onClose={() => setShowAI(null)}
          templateType={templateType}
          currentContent={findBlockById(blocks, showAI)?.properties?.content}
          onInsert={(text) => {
            pushBlocks(updateBlockInTree(blocks, showAI, (b) => ({ ...b, properties: { ...b.properties, content: text } })));
            setShowAI(null);
          }}
        />
      )}
      {showExport && (
        <ExportModal
          onClose={() => setShowExport(false)}
          blocks={blocks} data={SAMPLE_DATA} themeVars={theme.vars}
          canvas={canvas} templateName={templateName} templateType={templateType}
        />
      )}
      {contextMenu && (
        <ContextMenu
          x={contextMenu.x} y={contextMenu.y}
          onClose={() => setContextMenu(null)}
          actions={getContextActions(contextMenu.blockId)}
        />
      )}
      {showStarters && (
        <StarterTemplatesModal
          onClose={() => setShowStarters(false)}
          onSelect={handleSelectStarter}
          hasExistingBlocks={blocks.length > 0}
        />
      )}
      <input ref={fileInputRef} type="file" accept=".json" style={{ display: 'none' }} onChange={handleImportJSON} />

      {/* ═══ TOP TOOLBAR ═══ */}
      <div style={{
        height: 52, minHeight: 52,
        display: 'flex', alignItems: 'center', gap: 8, padding: '0 16px',
        backgroundColor: T.bg.secondary,
        borderBottom: `1px solid ${T.border.default}`,
        zIndex: 50,
      }}>
        {/* Logo */}
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginRight: 12 }}>
          <div style={{
            width: 28, height: 28, borderRadius: T.radius.md,
            background: 'linear-gradient(135deg, #3b82f6, #8b5cf6)',
            display: 'flex', alignItems: 'center', justifyContent: 'center',
          }}>
            <Layers size={16} style={{ color: '#fff' }} />
          </div>
          <span style={{ fontSize: 13, fontWeight: 700, letterSpacing: '-0.01em', color: T.text.primary }}>
            PageBuilder
          </span>
        </div>

        <div style={{ width: 1, height: 24, backgroundColor: T.border.default, margin: '0 4px' }} />

        {/* Template name */}
        <input
          value={templateName}
          onChange={(e) => setTemplateName(e.target.value)}
          style={{ ...css.input, width: 200, backgroundColor: 'transparent', border: '1px solid transparent', fontSize: 14, fontWeight: 500 }}
          onFocus={(e) => { e.target.style.borderColor = T.border.bright; e.target.style.backgroundColor = T.bg.tertiary; }}
          onBlur={(e) => { e.target.style.borderColor = 'transparent'; e.target.style.backgroundColor = 'transparent'; }}
        />

        {/* Template type selector */}
        <select value={templateType} onChange={(e) => applyPreset(e.target.value)} style={{ ...css.select, width: 140 }}>
          {Object.entries(CANVAS_PRESETS).map(([k, v]) => (
            <option key={k} value={k}>{v.label}</option>
          ))}
        </select>

        {/* Version badge */}
        <div style={{
          padding: '2px 8px', borderRadius: T.radius.sm,
          backgroundColor: T.bg.tertiary,
          fontSize: 10, fontWeight: 700, color: T.text.muted,
          display: 'flex', alignItems: 'center', gap: 4,
        }}>
          <GitBranch size={10} /> v{templateVersion}
        </div>

        <div style={{ flex: 1 }} />

        {/* Undo / Redo */}
        <button onClick={undo} disabled={!canUndo} style={{ ...css.iconBtn, opacity: canUndo ? 1 : 0.3 }} title="Undo (Ctrl+Z)" aria-label="Undo"><Undo2 size={16} /></button>
        <span style={{ fontSize: 10, color: T.text.muted, minWidth: 30, textAlign: 'center' }}>{pointer}/{historyLength - 1}</span>
        <button onClick={redo} disabled={!canRedo} style={{ ...css.iconBtn, opacity: canRedo ? 1 : 0.3 }} title="Redo (Ctrl+Y)" aria-label="Redo"><Redo2 size={16} /></button>

        <div style={{ width: 1, height: 24, backgroundColor: T.border.default, margin: '0 4px' }} />

        {/* Zoom */}
        <button onClick={() => setZoom((z) => Math.max(0.25, z - 0.1))} style={css.iconBtn}><ZoomOut size={16} /></button>
        <span style={{ fontSize: 12, color: T.text.secondary, minWidth: 40, textAlign: 'center' }}>{Math.round(zoom * 100)}%</span>
        <button onClick={() => setZoom((z) => Math.min(2, z + 0.1))} style={css.iconBtn}><ZoomIn size={16} /></button>

        <div style={{ width: 1, height: 24, backgroundColor: T.border.default, margin: '0 4px' }} />

        {/* Grid & Ruler toggles */}
        <button
          onClick={() => setShowRulers((v) => !v)}
          style={{ ...css.iconBtn, color: showRulers ? T.primary.base : T.text.muted }}
          title="Toggle Rulers"
        >
          <Ruler size={16} />
        </button>
        <button
          onClick={() => setShowGrid((v) => !v)}
          style={{ ...css.iconBtn, color: showGrid ? T.primary.base : T.text.muted }}
          title="Toggle Grid"
        >
          <Grid3x3 size={16} />
        </button>
        <button
          onClick={() => setSnapEnabled((v) => !v)}
          style={{
            ...css.iconBtn,
            color: snapEnabled ? T.primary.base : T.text.muted,
            ...(snapEnabled ? { backgroundColor: T.primary.glow, borderRadius: T.radius.md } : {}),
          }}
          title={`Snap to Grid (${snapEnabled ? 'ON' : 'OFF'})`}
        >
          <Crosshair size={16} />
        </button>

        <div style={{ width: 1, height: 24, backgroundColor: T.border.default, margin: '0 4px' }} />

        {/* Starter Templates (Feature #31) */}
        <button
          onClick={() => setShowStarters(true)}
          style={css.iconBtn}
          title="Starter Templates"
          aria-label="Open starter templates"
        >
          <FileText size={16} />
        </button>

        {/* Preview / Export / Save */}
        <button
          onClick={() => { setIsPreview(!isPreview); setSelectedId(null); if (isPreview) setPreviewDevice('desktop'); }}
          style={{ ...css.btn(isPreview ? 'primary' : 'secondary'), gap: 6, padding: '6px 14px' }}
          aria-label={isPreview ? 'Exit preview mode' : 'Enter preview mode'}
        >
          {isPreview ? <Monitor size={14} /> : <Eye size={14} />}
          {isPreview ? 'Edit' : 'Preview'}
        </button>

        {/* Responsive preview device selector (Feature #24) */}
        {isPreview && (
          <div style={{ display: 'flex', gap: 2, marginLeft: 2 }}>
            {[
              { key: 'desktop', icon: Monitor, label: '100%', width: null },
              { key: 'tablet', icon: Tablet, label: '768px', width: 768 },
              { key: 'mobile', icon: Smartphone, label: '375px', width: 375 },
            ].map((d) => (
              <button
                key={d.key}
                onClick={() => setPreviewDevice(d.key)}
                style={{
                  ...css.iconBtn,
                  width: 'auto', padding: '4px 8px', gap: 4,
                  display: 'inline-flex', alignItems: 'center',
                  fontSize: 10, fontWeight: 500,
                  color: previewDevice === d.key ? T.primary.base : T.text.muted,
                  backgroundColor: previewDevice === d.key ? T.primary.glow : 'transparent',
                  borderRadius: T.radius.md,
                  border: previewDevice === d.key ? `1px solid ${T.primary.base}` : '1px solid transparent',
                }}
                title={`Preview at ${d.label}`}
                aria-label={`Preview ${d.key} ${d.label}`}
                aria-pressed={previewDevice === d.key}
              >
                <d.icon size={13} /> {d.label}
              </button>
            ))}
          </div>
        )}
        <button onClick={() => setShowExport(true)} style={{ ...css.btn('secondary'), padding: '6px 12px' }} title="Export HTML/MJML" aria-label="Export HTML or MJML"><Code size={14} /></button>
        <button onClick={handleExportJSON} style={css.iconBtn} title="Export JSON" aria-label="Export template as JSON"><Download size={16} /></button>
        <button onClick={() => fileInputRef.current?.click()} style={css.iconBtn} title="Import JSON" aria-label="Import template from JSON"><Upload size={16} /></button>
        <button onClick={handleSave} style={{ ...css.btn('primary'), padding: '6px 20px' }} aria-label="Save template">
          <Save size={14} /> {saved ? 'Saved! ✓' : 'Save'}
        </button>
        {saveError && (
          <span style={{ fontSize: 11, color: '#ef4444', maxWidth: 180 }}>{saveError}</span>
        )}
      </div>

      {/* ═══ MAIN LAYOUT ═══ */}
      <div style={{ flex: 1, display: 'flex', overflow: 'hidden' }}>

        {/* ── LEFT SIDEBAR ────────────────────────────────────── */}
        {!isPreview && (
          <div style={{ ...css.panel, width: 260, minWidth: 260 }}>
            {/* Tab bar */}
            <div style={{ display: 'flex', borderBottom: `1px solid ${T.border.default}` }} role="tablist" aria-label="Builder panels">
              {LEFT_TABS.map((tab) => (
                <button
                  key={tab.key}
                  onClick={() => setLeftTab(tab.key)}
                  role="tab"
                  aria-selected={leftTab === tab.key}
                  aria-controls={`tabpanel-${tab.key}`}
                  style={{
                    flex: 1, padding: '10px 0',
                    border: 'none', backgroundColor: 'transparent',
                    color: leftTab === tab.key ? T.primary.base : T.text.muted,
                    fontSize: 10, fontWeight: 600, cursor: 'pointer', fontFamily: T.font,
                    borderBottom: leftTab === tab.key ? `2px solid ${T.primary.base}` : '2px solid transparent',
                    display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 4,
                    transition: 'all 150ms', textTransform: 'uppercase', letterSpacing: '0.03em',
                  }}
                >
                  <tab.icon size={12} /> {tab.label}
                </button>
              ))}
            </div>

            {/* Tab content */}
            <div style={{ flex: 1, overflowY: 'auto' }} role="tabpanel" id={`tabpanel-${leftTab}`}>
              {leftTab === 'blocks' && <BlockPalette onAddBlock={(type) => addBlock(type)} />}
              {leftTab === 'layers' && (
                <>
                  {/* Feature #29: Block search */}
                  <div style={{ padding: '8px 8px 0' }}>
                    <div style={{ position: 'relative' }}>
                      <Search size={12} style={{ position: 'absolute', left: 8, top: '50%', transform: 'translateY(-50%)', color: T.text.muted, pointerEvents: 'none' }} />
                      <input
                        style={{ ...css.input, paddingLeft: 26, paddingRight: 28, fontSize: 12 }}
                        placeholder="Search blocks..."
                        value={blockSearchTerm}
                        onChange={(e) => setBlockSearchTerm(e.target.value)}
                        aria-label="Search blocks"
                      />
                      {blockSearchTerm && (
                        <button
                          onClick={() => setBlockSearchTerm('')}
                          style={{ ...css.iconBtn, position: 'absolute', right: 2, top: '50%', transform: 'translateY(-50%)', width: 22, height: 22 }}
                          aria-label="Clear search"
                        >
                          <X size={11} />
                        </button>
                      )}
                    </div>
                  </div>
                  <LayerTree
                    blocks={blocks} selectedId={selectedId}
                    onSelect={(id) => { setSelectedId(id); const blk = findBlockById(blocks, id); if (blk) announce(`Selected ${blk.type.replace('_',' ')} block${blk.label ? ': ' + blk.label : ''}`); }}
                    onToggleVisible={toggleVisible} onToggleLock={toggleLock}
                    onMoveBlock={handleMoveBlock}
                    searchTerm={blockSearchTerm}
                  />
                </>
              )}
              {leftTab === 'themes' && <ThemePicker currentTheme={activeTheme} onSelect={setActiveTheme} />}
              {leftTab === 'versions' && <VersionPanel versions={versionHistory} currentVersion={templateVersion} />}
            </div>
          </div>
        )}

        {/* ── CANVAS AREA ─────────────────────────────────────── */}
        <div
          style={{
            flex: 1, overflow: 'hidden', position: 'relative',
            backgroundColor: T.bg.primary,
            display: 'flex', flexDirection: 'column',
          }}
        >
          {/* ── Canvas / HTML Source tab bar ─────────────────── */}
          {!isPreview && (
            <div style={{
              display: 'flex', borderBottom: `1px solid ${T.border.default}`,
              backgroundColor: T.bg.secondary, flexShrink: 0,
            }}>
              {[
                { key: 'canvas', label: 'Visual' },
                { key: 'html',   label: 'HTML Source' },
              ].map((tab) => (
                <button
                  key={tab.key}
                  onClick={() => setCentreTab(tab.key)}
                  style={{
                    padding: '7px 16px', fontSize: 12,
                    border: 'none', background: 'transparent',
                    color: centreTab === tab.key ? T.primary.base : T.text.muted,
                    borderBottom: centreTab === tab.key ? `2px solid ${T.primary.base}` : '2px solid transparent',
                    cursor: 'pointer', fontFamily: T.font, fontWeight: centreTab === tab.key ? 600 : 400,
                  }}
                >
                  {tab.label}
                </button>
              ))}
            </div>
          )}

          {/* ── HTML Source view ─────────────────────────────── */}
          {centreTab === 'html' && !isPreview && (() => {
            const htmlOutput = generateFullHTML(
              blocks, SAMPLE_DATA, theme.vars, canvas, templateName,
              { forEmail: outputFormat === 'HTML' },
            );
            return (
              <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden', padding: 16 }}>
                <div style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: 8 }}>
                  <button
                    onClick={() => {
                      navigator.clipboard.writeText(htmlOutput).then(() => {
                        setHtmlCopied(true);
                        setTimeout(() => setHtmlCopied(false), 2000);
                      });
                    }}
                    style={{ ...css.btn('secondary'), fontSize: 12, padding: '6px 14px' }}
                  >
                    {htmlCopied ? '✓ Copied!' : '⎘ Copy to Clipboard'}
                  </button>
                </div>
                <textarea
                  readOnly
                  value={htmlOutput}
                  style={{
                    flex: 1,
                    resize: 'none',
                    backgroundColor: T.bg.tertiary,
                    color: T.text.secondary,
                    border: `1px solid ${T.border.default}`,
                    borderRadius: T.radius.md,
                    fontFamily: 'monospace',
                    fontSize: 12,
                    lineHeight: 1.5,
                    padding: 12,
                    outline: 'none',
                  }}
                />
              </div>
            );
          })()}

          {/* ── Visual canvas (rulers + scroll area) ─────────── */}
          {(centreTab === 'canvas' || isPreview) && (
          <div style={{ flex: 1, overflow: 'hidden', position: 'relative' }}>
          {/* ── Rulers ─────────────────────────────────────────── */}
          {showRulers && !isPreview && (
            <Rulers
              canvas={canvas}
              zoom={zoom}
              canvasWidthPx={canvasWidthPx}
              canvasHeightPx={canvasHeightPx}
              scrollContainer={scrollRef}
              canvasOffset={{ left: 40 + RULER_THICKNESS, top: 40 + RULER_THICKNESS }}
              showGrid={showGrid}
              gridSpacing={gridSpacing}
              guides={guides}
              onAddGuide={addGuide}
              onRemoveGuide={removeGuide}
              draggingGuide={draggingGuide}
              setDraggingGuide={setDraggingGuide}
            />
          )}

          {/* ── Scrollable canvas container ──────────────────── */}
          <div
            ref={scrollRef}
            style={{
              position: 'absolute',
              top: showRulers && !isPreview ? RULER_THICKNESS : 0,
              left: showRulers && !isPreview ? RULER_THICKNESS : 0,
              right: 0,
              bottom: 0,
              overflow: 'auto',
              backgroundImage: isPreview ? 'none' : `radial-gradient(circle, ${T.border.default} 1px, transparent 1px)`,
              backgroundSize: '20px 20px',
            }}
            onDragOver={(e) => e.preventDefault()}
            onDrop={handleCanvasAreaDrop}
            onClick={() => { setSelectedId(null); setContextMenu(null); }}
          >
            <div style={{
              display: 'flex', flexDirection: 'column', alignItems: 'center',
              padding: 40, minHeight: '100%', gap: isPreview ? 24 : 0,
            }}>
              {(isPreview ? pages : [blocks]).map((pageBlocks, pageIdx) => {
                // Feature #24: Responsive Preview — device width constraint
                const deviceMaxWidth = previewDevice === 'tablet' ? 768 : previewDevice === 'mobile' ? 375 : null;
                const effectiveWidth = (isPreview && deviceMaxWidth) ? Math.min(canvasWidthPx, deviceMaxWidth) : canvasWidthPx;

                // Device frame wrapper for non-desktop preview
                const deviceFrame = isPreview && previewDevice !== 'desktop';
                const frameStyles = deviceFrame ? {
                  border: `3px solid ${T.border.bright}`,
                  borderRadius: previewDevice === 'mobile' ? 24 : 16,
                  padding: previewDevice === 'mobile' ? '40px 6px 48px' : '24px 6px 24px',
                  backgroundColor: T.bg.tertiary,
                  position: 'relative',
                  boxShadow: T.shadow.lg,
                } : {};

                return (
                <div key={pageIdx} style={{ ...frameStyles, display: 'inline-block' }}>
                  {/* Device notch / indicator */}
                  {deviceFrame && previewDevice === 'mobile' && (
                    <div style={{
                      position: 'absolute', top: 14, left: '50%', transform: 'translateX(-50%)',
                      width: 60, height: 6, borderRadius: 3, backgroundColor: T.border.bright,
                    }} />
                  )}
                  {deviceFrame && previewDevice === 'mobile' && (
                    <div style={{
                      position: 'absolute', bottom: 12, left: '50%', transform: 'translateX(-50%)',
                      width: 28, height: 28, borderRadius: '50%', border: `2px solid ${T.border.bright}`,
                    }} />
                  )}
                  {deviceFrame && previewDevice === 'tablet' && (
                    <div style={{
                      position: 'absolute', top: 9, left: '50%', transform: 'translateX(-50%)',
                      width: 8, height: 8, borderRadius: '50%', backgroundColor: T.border.bright,
                    }} />
                  )}

                  <div
                  key={`canvas-${pageIdx}`}
                  ref={pageIdx === 0 ? canvasRef : undefined}
                  id={pageIdx === 0 ? 'canvas-area' : undefined}
                  style={{
                    width: effectiveWidth,
                    maxWidth: (isPreview && deviceMaxWidth) ? deviceMaxWidth : undefined,
                    minHeight: canvasHeightPx === 'auto' ? 400 : canvasHeightPx,
                    height: canvasHeightPx === 'auto' ? 'auto' : canvasHeightPx,
                    backgroundColor: canvas.backgroundColor,
                    boxShadow: deviceFrame ? 'none' : '0 4px 40px rgba(0,0,0,0.5), 0 0 0 1px rgba(255,255,255,0.05)',
                    transform: `scale(${zoom})`, transformOrigin: 'top center',
                    transition: 'transform 200ms ease-in-out, width 300ms ease-in-out',
                    borderRadius: deviceFrame ? (previewDevice === 'mobile' ? 0 : 2) : 2,
                    overflow: 'hidden', position: 'relative',
                  }}
                >
                  {/* Grid overlay (inside canvas) */}
                  {showGrid && !isPreview && (
                    <GridOverlay
                      canvas={canvas}
                      gridSpacing={gridSpacing}
                      canvasWidthPx={canvasWidthPx}
                      canvasHeightPx={canvasHeightPx}
                      gridStyle={gridStyle}
                    />
                  )}

                  {/* Guide lines (inside canvas) */}
                  {!isPreview && (guides.length > 0 || draggingGuide) && (
                    <GuideLines
                      canvas={canvas}
                      guides={guides}
                      canvasWidthPx={canvasWidthPx}
                      canvasHeightPx={canvasHeightPx}
                      draggingGuide={draggingGuide}
                    />
                  )}

                  {/* Page badge (multi-page preview) */}
                  {isPreview && pages.length > 1 && (
                    <div style={{
                      position: 'absolute', top: 8, right: 8,
                      backgroundColor: 'rgba(0,0,0,0.5)', color: '#fff',
                      padding: '2px 8px', borderRadius: 10, fontSize: 10, fontWeight: 600, zIndex: 5,
                    }}>
                      Page {pageIdx + 1} of {pages.length}
                    </div>
                  )}

                  {/* Empty canvas prompt */}
                  {pageBlocks.length === 0 && !isPreview && (
                    <div style={{
                      display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center',
                      height: '100%', minHeight: 300, gap: 12, color: '#94a3b8',
                    }}>
                      <div style={{
                        width: 64, height: 64, borderRadius: '50%',
                        border: '2px dashed #3d4557',
                        display: 'flex', alignItems: 'center', justifyContent: 'center',
                      }}>
                        <Plus size={28} style={{ color: '#64748b' }} />
                      </div>
                      <div style={{ fontSize: 15, fontWeight: 500 }}>Drag blocks here to start designing</div>
                      <div style={{ fontSize: 12, color: '#64748b' }}>or click a block in the left panel</div>
                      <button
                        onClick={(e) => { e.stopPropagation(); setShowStarters(true); }}
                        style={{ ...css.btn('primary'), padding: '8px 20px', fontSize: 13, marginTop: 8 }}
                      >
                        <FileText size={14} /> Browse Starter Templates
                      </button>
                    </div>
                  )}

                  {/* Render blocks */}
                  {pageBlocks.map((block) => (
                    <CanvasBlock
                      key={block.id} block={block} selectedId={selectedId}
                      onSelect={(id) => { setSelectedId(id); const blk = findBlockById(blocks, id); if (blk) announce(`Selected ${blk.type.replace('_',' ')} block${blk.label ? ': ' + blk.label : ''}`); }}
                      onDrop={handleCanvasDrop}
                      isPreview={isPreview} dragOverId={dragOverId} setDragOverId={setDragOverId}
                      theme={theme} onContextMenu={handleContextMenu} onCellEdit={handleCellEdit}
                    />
                  ))}
                </div>
                {/* Close device frame wrapper */}
                </div>
                );
              })}
            </div>

            {/* Status bar */}
            <div style={{
              position: 'fixed', bottom: 16, left: '50%', transform: 'translateX(-50%)',
              backgroundColor: T.bg.elevated, border: `1px solid ${T.border.default}`,
              borderRadius: T.radius.xl, padding: '4px 14px',
              fontSize: 11, color: T.text.muted,
              display: 'flex', gap: 12, alignItems: 'center',
              zIndex: 10, boxShadow: T.shadow.md,
            }}>
              <span>{canvas.width}{canvas.unit} × {canvas.height === 'auto' ? 'auto' : `${canvas.height}${canvas.unit}`}</span>
              <span style={{ color: T.border.default }}>|</span>
              <span>{outputFormat}</span>
              <span style={{ color: T.border.default }}>|</span>
              <span>{countAllBlocks(blocks)} blocks</span>
              <span style={{ color: T.border.default }}>|</span>
              <span style={{ display: 'flex', alignItems: 'center', gap: 3 }}><Palette size={10} /> {theme.name}</span>
              {snapEnabled && (
                <><span style={{ color: T.border.default }}>|</span><span style={{ display: 'flex', alignItems: 'center', gap: 3, color: T.primary.base }}><Crosshair size={10} /> Snap</span></>
              )}
              {isPreview && pages.length > 1 && (
                <><span style={{ color: T.border.default }}>|</span><span>{pages.length} pages</span></>
              )}
            </div>
            </div>
          </div>
          )} {/* end visual canvas conditional */}
        </div>

        {/* ── RIGHT SIDEBAR ───────────────────────────────────── */}
        {!isPreview && (
          <div style={{ ...css.panel, width: 280, minWidth: 280, borderRight: 'none', borderLeft: `1px solid ${T.border.default}` }}>
            {selectedBlock ? (
              <>
                <div style={css.panelHeader}><Settings size={13} /> Properties</div>
                <div style={{ padding: '8px 12px 0', display: 'flex', gap: 4 }}>
                  <button onClick={() => duplicateBlock(selectedId)} style={{ ...css.btn('secondary'), flex: 1, fontSize: 11, padding: '4px 8px' }}>
                    <Copy size={12} /> Duplicate
                  </button>
                  <button onClick={() => deleteBlock(selectedId)} style={{ ...css.btn('secondary'), flex: 1, fontSize: 11, padding: '4px 8px', color: T.status.danger, borderColor: 'rgba(239,68,68,0.3)' }}>
                    <Trash2 size={12} /> Delete
                  </button>
                </div>
                <PropertyEditor block={selectedBlock} onUpdate={updateBlock} onShowAI={setShowAI} />
              </>
            ) : (
              <>
                <div style={css.panelHeader}><Paintbrush size={13} /> Canvas Settings</div>
                <div style={{ padding: 16 }}>
                  {/* Page Size Presets */}
                  <span style={css.label}>Page Size</span>
                  <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4, marginBottom: 12 }}>
                    {PAGE_SIZE_PRESETS.map((p) => {
                      const isActive =
                        canvas.unit === p.unit &&
                        canvas.width === p.width &&
                        (p.height === 'auto' ? canvas.height === 'auto' : canvas.height === p.height);
                      return (
                        <button
                          key={p.key}
                          onClick={() => {
                            let w = p.width;
                            let h = p.height;
                            if (orientation === 'landscape' && h !== 'auto' && typeof h === 'number' && w < h) {
                              [w, h] = [h, w];
                            }
                            setCanvas((c) => ({ ...c, width: w, height: h, unit: p.unit }));
                          }}
                          style={{
                            ...css.btn(isActive ? 'primary' : 'secondary'),
                            fontSize: 10, padding: '3px 8px', flex: '0 0 auto',
                            ...(isActive ? {} : { background: T.bg.tertiary }),
                          }}
                        >
                          {p.label}
                        </button>
                      );
                    })}
                  </div>

                  {/* Orientation */}
                  <span style={css.label}>Orientation</span>
                  <div style={{ display: 'flex', gap: 4, marginBottom: 12 }}>
                    {[{ v: 'portrait', l: 'Portrait' }, { v: 'landscape', l: 'Landscape' }].map(({ v, l }) => (
                      <button
                        key={v}
                        onClick={() => toggleOrientation(v)}
                        style={{
                          ...css.btn(orientation === v ? 'primary' : 'secondary'),
                          flex: 1, fontSize: 11, padding: '5px 8px',
                          ...(orientation === v ? {} : { background: T.bg.tertiary }),
                        }}
                      >
                        {l}
                      </button>
                    ))}
                  </div>


                  {/* ── Dimensions ─────────────────────────────── */}
                  <span style={css.label}>Dimensions</span>
                  <div style={{ display: 'flex', gap: 6, marginBottom: 10 }}>
                    <div style={{ flex: 1 }}>
                      <span style={{ fontSize: 9, color: T.text.muted, marginBottom: 2, display: 'block' }}>Width</span>
                      <input
                        style={css.input}
                        type="number"
                        min="1"
                        step={canvas.unit === 'mm' ? 1 : canvas.unit === 'in' ? 0.25 : 10}
                        value={canvas.width}
                        onChange={(e) => {
                          const v = parseFloat(e.target.value);
                          if (!isNaN(v) && v > 0) setCanvas((c) => ({ ...c, width: v }));
                        }}
                      />
                    </div>
                    <div style={{ flex: 1 }}>
                      <span style={{ fontSize: 9, color: T.text.muted, marginBottom: 2, display: 'block' }}>Height</span>
                      <input
                        style={css.input}
                        type={canvas.height === 'auto' ? 'text' : 'number'}
                        min="1"
                        step={canvas.unit === 'mm' ? 1 : canvas.unit === 'in' ? 0.25 : 10}
                        value={canvas.height === 'auto' ? 'auto' : canvas.height}
                        onChange={(e) => {
                          const raw = e.target.value;
                          if (raw === 'auto' || raw === '') {
                            setCanvas((c) => ({ ...c, height: 'auto' }));
                          } else {
                            const v = parseFloat(raw);
                            if (!isNaN(v) && v > 0) setCanvas((c) => ({ ...c, height: v }));
                          }
                        }}
                        placeholder="auto"
                      />
                    </div>
                  </div>

                  {/* ── Unit selector with auto-conversion ────── */}
                  <span style={css.label}>Unit</span>
                  <select
                    style={{ ...css.select, marginBottom: 10 }}
                    value={canvas.unit}
                    onChange={(e) => {
                      const newUnit = e.target.value;
                      const oldUnit = canvas.unit;
                      if (newUnit === oldUnit) return;
                      // Convert values between units
                      const toPx = (v, u) => u === 'mm' ? v * 3.7795 : u === 'in' ? v * 96 : v;
                      const fromPx = (px, u) => u === 'mm' ? px / 3.7795 : u === 'in' ? px / 96 : px;
                      const convert = (v) => {
                        if (v === 'auto') return 'auto';
                        const px = toPx(v, oldUnit);
                        const nv = fromPx(px, newUnit);
                        // Round sensibly for the target unit
                        if (newUnit === 'mm') return Math.round(nv);
                        if (newUnit === 'in') return Math.round(nv * 100) / 100;
                        return Math.round(nv);
                      };
                      setCanvas((c) => ({
                        ...c,
                        width: convert(c.width),
                        height: convert(c.height),
                        unit: newUnit,
                      }));
                    }}
                  >
                    <option value="px">Pixels (px)</option>
                    <option value="mm">Millimeters (mm)</option>
                    <option value="in">Inches (in)</option>
                  </select>

                  {/* ── Pixel dimensions readout ──────────────── */}
                  <div style={{
                    padding: '6px 10px', backgroundColor: T.bg.tertiary,
                    borderRadius: T.radius.md, fontSize: 10, color: T.text.muted,
                    marginBottom: 12, display: 'flex', justifyContent: 'space-between',
                  }}>
                    <span>Render size:</span>
                    <span style={{ fontFamily: 'monospace', color: T.text.secondary }}>
                      {Math.round(unitToPx(canvas.width, canvas.unit))}px × {canvas.height === 'auto' ? 'auto' : `${Math.round(unitToPx(canvas.height, canvas.unit))}px`}
                    </span>
                  </div>

                  {/* ── Background ─────────────────────────────── */}
                  <ColorInput label="Background" value={canvas.backgroundColor} onChange={(v) => setCanvas((c) => ({ ...c, backgroundColor: v }))} />

                  {/* ── Margins ─────────────────────────────────── */}
                  <span style={css.label}>Page Margins</span>
                  <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 6, marginBottom: 10 }}>
                    {['Top', 'Right', 'Bottom', 'Left'].map((side) => {
                      const key = `margin${side}`;
                      return (
                        <div key={side}>
                          <span style={{ fontSize: 9, color: T.text.muted, marginBottom: 1, display: 'block' }}>{side}</span>
                          <input
                            style={css.input}
                            type="number"
                            min="0"
                            step={canvas.unit === 'mm' ? 1 : canvas.unit === 'in' ? 0.125 : 5}
                            value={canvas[key] ?? 0}
                            onChange={(e) => {
                              const v = parseFloat(e.target.value);
                              setCanvas((c) => ({ ...c, [key]: isNaN(v) ? 0 : Math.max(0, v) }));
                            }}
                          />
                        </div>
                      );
                    })}
                  </div>

                  {/* ── Output Format ──────────────────────────── */}
                  <PropInput
                    label="Output Format"
                    type="select"
                    value={outputFormat}
                    onChange={setOutputFormat}
                    options={[
                      { value: 'PDF', label: 'PDF' },
                      { value: 'HTML', label: 'HTML' },
                      { value: 'MJML', label: 'MJML (Email)' },
                    ]}
                  />

                  {/* Virtual Printer */}
                  <PropInput
                    label="Virtual Printer"
                    type="text"
                    value={virtualPrinter}
                    onChange={setVirtualPrinter}
                    placeholder="e.g. ZDesigner GX430t"
                  />

                  {/* Print Behaviour */}
                  <PropInput
                    label="Print Behaviour"
                    type="select"
                    value={printBehaviour}
                    onChange={setPrintBehaviour}
                    options={[
                      { value: 'print_on_save',     label: 'Print on save' },
                      { value: 'print_on_dispatch', label: 'Print on dispatch' },
                      { value: 'manual_only',       label: 'Manual only' },
                    ]}
                  />

                  {/* ── Grid & Snap ─────────────────────────────── */}
                  <div style={{
                    marginTop: 16,
                    paddingTop: 14,
                    borderTop: `1px solid ${T.border.default}`,
                  }}>
                    <span style={{ ...css.label, display: 'flex', alignItems: 'center', gap: 6 }}>
                      <Grid3x3 size={11} /> Grid & Snapping
                    </span>

                    {/* Toggle row: Rulers, Grid, Snap */}
                    <div style={{ display: 'flex', gap: 4, marginBottom: 10 }}>
                      {[
                        { key: 'rulers', label: 'Rulers', icon: Ruler, active: showRulers, toggle: () => setShowRulers((v) => !v) },
                        { key: 'grid',   label: 'Grid',   icon: Grid3x3, active: showGrid, toggle: () => setShowGrid((v) => !v) },
                        { key: 'snap',   label: 'Snap',   icon: Crosshair, active: snapEnabled, toggle: () => setSnapEnabled((v) => !v) },
                      ].map((item) => (
                        <button
                          key={item.key}
                          onClick={item.toggle}
                          style={{
                            ...css.btn(item.active ? 'primary' : 'secondary'),
                            flex: 1,
                            fontSize: 10,
                            padding: '5px 6px',
                            ...(item.active ? {} : { background: T.bg.tertiary }),
                          }}
                        >
                          <item.icon size={11} /> {item.label}
                        </button>
                      ))}
                    </div>

                    {/* Grid spacing */}
                    {(showGrid || snapEnabled) && (
                      <>
                        <span style={{ fontSize: 9, color: T.text.muted, marginBottom: 2, display: 'block' }}>
                          Grid Spacing ({canvas.unit})
                        </span>
                        <div style={{ display: 'flex', gap: 4, marginBottom: 8 }}>
                          <input
                            style={{ ...css.input, flex: 1 }}
                            type="number"
                            min={canvas.unit === 'in' ? 0.0625 : 1}
                            step={canvas.unit === 'mm' ? 1 : canvas.unit === 'in' ? 0.125 : 5}
                            value={gridSpacing}
                            onChange={(e) => {
                              const v = parseFloat(e.target.value);
                              if (!isNaN(v) && v > 0) setGridSpacing(v);
                            }}
                          />
                        </div>

                        {/* Spacing presets */}
                        <div style={{ display: 'flex', gap: 3, marginBottom: 10, flexWrap: 'wrap' }}>
                          {(canvas.unit === 'mm'
                            ? [{ v: 5, l: '5mm' }, { v: 10, l: '10mm' }, { v: 20, l: '20mm' }, { v: 25, l: '25mm' }]
                            : canvas.unit === 'in'
                            ? [{ v: 0.125, l: '⅛″' }, { v: 0.25, l: '¼″' }, { v: 0.5, l: '½″' }, { v: 1, l: '1″' }]
                            : [{ v: 10, l: '10px' }, { v: 20, l: '20px' }, { v: 25, l: '25px' }, { v: 50, l: '50px' }]
                          ).map(({ v, l }) => (
                            <button
                              key={v}
                              onClick={() => setGridSpacing(v)}
                              style={{
                                ...css.btn(gridSpacing === v ? 'primary' : 'secondary'),
                                fontSize: 9,
                                padding: '2px 7px',
                                ...(gridSpacing === v ? {} : { background: T.bg.tertiary }),
                              }}
                            >
                              {l}
                            </button>
                          ))}
                        </div>
                      </>
                    )}

                    {/* Grid style selector */}
                    {showGrid && (
                      <>
                        <span style={{ fontSize: 9, color: T.text.muted, marginBottom: 2, display: 'block' }}>
                          Grid Style
                        </span>
                        <div style={{ display: 'flex', gap: 3, marginBottom: 10 }}>
                          {[
                            { v: 'lines', l: 'Lines' },
                            { v: 'dots', l: 'Dots' },
                            { v: 'crosses', l: 'Crosses' },
                          ].map(({ v, l }) => (
                            <button
                              key={v}
                              onClick={() => setGridStyle(v)}
                              style={{
                                ...css.btn(gridStyle === v ? 'primary' : 'secondary'),
                                flex: 1,
                                fontSize: 10,
                                padding: '4px 6px',
                                ...(gridStyle === v ? {} : { background: T.bg.tertiary }),
                              }}
                            >
                              {l}
                            </button>
                          ))}
                        </div>
                      </>
                    )}
                  </div>

                  {/* ── Guides ──────────────────────────────────── */}
                  {showRulers && (
                    <div style={{
                      marginTop: 12,
                      paddingTop: 14,
                      borderTop: `1px solid ${T.border.default}`,
                    }}>
                      <span style={{ ...css.label, display: 'flex', alignItems: 'center', gap: 6, justifyContent: 'space-between' }}>
                        <span style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                          <Ruler size={11} /> Guides ({guides.length})
                        </span>
                        {guides.length > 0 && (
                          <button
                            onClick={clearGuides}
                            style={{
                              background: 'none', border: 'none', cursor: 'pointer',
                              color: T.status.danger, fontSize: 9, fontFamily: T.font,
                              textTransform: 'uppercase', fontWeight: 600, padding: 0,
                            }}
                          >
                            Clear All
                          </button>
                        )}
                      </span>
                      <div style={{ fontSize: 10, color: T.text.muted, marginBottom: 8 }}>
                        Drag from a ruler or double-click to add a guide
                      </div>
                      {guides.length > 0 && (
                        <div style={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
                          {guides.map((g, i) => (
                            <div
                              key={i}
                              style={{
                                display: 'flex', alignItems: 'center', justifyContent: 'space-between',
                                padding: '3px 8px',
                                backgroundColor: T.bg.tertiary,
                                borderRadius: T.radius.sm,
                                fontSize: 11,
                              }}
                            >
                              <span style={{ color: T.text.secondary }}>
                                <span style={{
                                  display: 'inline-block', width: 6, height: 6,
                                  borderRadius: '50%', backgroundColor: '#ef4444',
                                  marginRight: 6, verticalAlign: 'middle',
                                }} />
                                {g.axis === 'x' ? 'Vertical' : 'Horizontal'} — {g.position}{canvas.unit}
                              </span>
                              <button
                                onClick={() => removeGuide(i)}
                                style={{
                                  background: 'none', border: 'none', cursor: 'pointer',
                                  color: T.text.muted, padding: '2px', display: 'flex',
                                }}
                              >
                                <X size={10} />
                              </button>
                            </div>
                          ))}
                        </div>
                      )}
                    </div>
                  )}
                </div>
              </>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

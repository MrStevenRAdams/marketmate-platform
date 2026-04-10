// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// BLOCK TYPE REGISTRY
// Every block type in the builder is defined here. Adding a new
// block type requires:
//   1. Add an entry to BLOCK_TYPES (below)
//   2. Add a `case` in createDefaultBlock() (./blockFactory.js)
//   3. Add render logic in CanvasBlock (../components/canvas/)
//   4. Add property fields in PropertyEditor (../components/panels/)
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

import {
  Type, Image, Columns, Minus, SeparatorHorizontal,
  Table, Square, Tag, Building2, QrCode,
  Repeat, Scissors, List, ListOrdered,
} from 'lucide-react';
import T from './tokens';

/**
 * Block type definitions.
 * Each entry provides the metadata the palette, layer tree, and
 * property editor need to display and identify the block.
 *
 * @property {string}    type   — Unique key, stored in block JSON
 * @property {string}    label  — Human-readable name
 * @property {Component} icon   — Lucide icon component
 * @property {string}    color  — Accent colour for the block's icon/badge
 */
const BLOCK_TYPES = [
  { type: 'text',          label: 'Text',          icon: Type,                color: T.primary.base },
  { type: 'image',         label: 'Image',         icon: Image,               color: T.accent.cyan },
  { type: 'columns',       label: 'Columns',       icon: Columns,             color: T.accent.purple },
  { type: 'spacer',        label: 'Spacer',        icon: Minus,               color: T.text.muted },
  { type: 'divider',       label: 'Divider',       icon: SeparatorHorizontal, color: T.text.secondary },
  { type: 'table',         label: 'Table',         icon: Table,               color: T.accent.teal },
  { type: 'box',           label: 'Box',           icon: Square,              color: T.accent.orange },
  { type: 'dynamic_field', label: 'Dynamic Field', icon: Tag,                 color: T.status.warning },
  { type: 'barcode',       label: 'Barcode',       icon: QrCode,              color: T.status.info },
  { type: 'logo',          label: 'Logo',          icon: Building2,           color: T.status.success },
  { type: 'repeater',        label: 'Repeater',        icon: Repeat,        color: T.accent.teal },
  { type: 'page_break',      label: 'Page Break',      icon: Scissors,      color: T.text.muted },
  { type: 'unordered_list',  label: 'Unordered List',  icon: List,          color: T.accent.cyan },
  { type: 'ordered_list',    label: 'Ordered List',    icon: ListOrdered,   color: T.accent.purple },
];

export default BLOCK_TYPES;

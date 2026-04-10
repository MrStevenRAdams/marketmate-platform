// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// BLOCK FACTORY
// Creates a new block with sensible default properties for each type.
// Called when the user drags a block from the palette or clicks
// "add block". All blocks share a common shape (id, type, visible,
// locked, style, children, conditions) plus type-specific properties.
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

import { uid } from './helpers';
import { T } from '../constants/index.jsx';

/**
 * @param {string} type — One of the block type keys from BLOCK_TYPES
 * @returns {Block} A fully initialised block ready to insert into the tree
 */
export function createDefaultBlock(type) {
  const base = {
    id:         uid(),
    type,
    visible:    true,
    locked:     false,
    style:      {},
    children:   undefined,
    label:      '',
    conditions: [],
  };

  const props = {};

  switch (type) {
    // ── Text ─────────────────────────────────────────────────
    case 'text':
      props.content       = 'Enter your text here...';
      props.fontFamily     = T.font;
      props.fontSize       = '14px';
      props.fontWeight     = '400';
      props.color          = '#333333';
      props.textAlign      = 'left';
      props.lineHeight     = '1.5';
      props.letterSpacing  = '0';
      base.style           = { padding: '8px' };
      break;

    // ── Image ────────────────────────────────────────────────
    case 'image':
      props.src       = '';
      props.alt       = 'Image';
      props.objectFit = 'contain';
      base.style      = { padding: '8px', width: '100%', height: '200px', borderRadius: '0' };
      break;

    // ── Columns ──────────────────────────────────────────────
    case 'columns':
      props.columnCount = 2;
      props.gap         = '16px';
      props.ratios      = [1, 1];
      base.children     = [
        { id: uid(), type: '_column', properties: {}, style: {}, children: [], visible: true, locked: false, conditions: [] },
        { id: uid(), type: '_column', properties: {}, style: {}, children: [], visible: true, locked: false, conditions: [] },
      ];
      base.style = { padding: '8px' };
      break;

    // ── Spacer ───────────────────────────────────────────────
    case 'spacer':
      props.height = '32px';
      break;

    // ── Divider ──────────────────────────────────────────────
    case 'divider':
      props.thickness = '1px';
      props.color     = '#cccccc';
      props.lineStyle = 'solid';
      base.style      = { margin: '8px 0' };
      break;

    // ── Table ────────────────────────────────────────────────
    case 'table':
      props.rows        = 3;
      props.cols        = 3;
      props.headerRow   = true;
      props.cellPadding = '8px';
      props.borderColor = '#cccccc';
      props.cells       = {};
      for (let r = 0; r < 3; r++) {
        for (let c = 0; c < 3; c++) {
          props.cells[`${r}-${c}`] = r === 0 ? `Header ${c + 1}` : `Cell ${r},${c + 1}`;
        }
      }
      base.style = { padding: '8px' };
      break;

    // ── Box (container) ──────────────────────────────────────
    case 'box':
      base.children = [];
      base.style    = {
        padding:         '16px',
        backgroundColor: '#ffffff',
        border:          '1px solid #e0e0e0',
        borderRadius:    '4px',
        minHeight:       '60px',
      };
      break;

    // ── Dynamic field (merge tag) ────────────────────────────
    case 'dynamic_field':
      props.fieldPath  = 'order.id';
      props.prefix     = '';
      props.suffix     = '';
      props.fontSize   = '14px';
      props.fontWeight = '400';
      props.color      = '#333333';
      base.style       = { padding: '4px 8px' };
      break;

    // ── Barcode / QR ─────────────────────────────────────────
    case 'barcode':
      props.barcodeType   = 'Code128';
      props.value         = '{{order.id}}';
      props.barcodeWidth  = '200';
      props.barcodeHeight = '80';
      base.style          = { padding: '8px', textAlign: 'center' };
      break;

    // ── Logo ─────────────────────────────────────────────────
    case 'logo':
      props.src       = '';
      props.maxWidth  = '200px';
      props.maxHeight = '80px';
      base.style      = { padding: '8px' };
      break;

    // ── Repeater ─────────────────────────────────────────────
    case 'repeater':
      props.dataSource = 'lines';
      props.direction  = 'vertical';
      base.children    = [];
      base.style       = { padding: '4px' };
      break;

    // ── Page break ───────────────────────────────────────────
    case 'page_break':
      props.label = 'Page Break';
      base.style  = { margin: '8px 0' };
      break;

    // ── Unordered List ───────────────────────────────────────
    case 'unordered_list':
      props.items      = ['List item 1', 'List item 2', 'List item 3'];
      props.fontSize   = '14px';
      props.color      = '#333333';
      props.fontFamily = T.font;
      base.style       = { padding: '8px' };
      break;

    // ── Ordered List ─────────────────────────────────────────
    case 'ordered_list':
      props.items      = ['List item 1', 'List item 2', 'List item 3'];
      props.fontSize   = '14px';
      props.color      = '#333333';
      props.fontFamily = T.font;
      base.style       = { padding: '8px' };
      break;
  }

  base.properties = props;
  return base;
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// BLOCK PALETTE
// Left sidebar "Blocks" tab. Displays all available block types
// as a 2-column grid, plus a "Pre-defined Tables" section below.
// Each block can be clicked (appends to canvas) or dragged onto
// the canvas / into a container.
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

import React, { useState } from 'react';
import { Table, ChevronDown, ChevronRight } from 'lucide-react';
import { T, BLOCK_TYPES } from '../../../constants/index.jsx';
import { uid } from '../../../utils/index.jsx';

// ── Pre-defined table templates ──────────────────────────────────

/**
 * Build a fully-configured Table block from column definitions.
 * @param {string} label       Human-readable name
 * @param {string[]} headers   Column header labels
 * @param {string[]} dataRow   Row-1 default cell values (merge tags or text)
 * @param {Object} extra       Optional extra properties to merge in
 */
function makeTableBlock(label, headers, dataRow, extra = {}) {
  const cols = headers.length;
  const rows = 2; // header row + one data row
  const cells = {};
  headers.forEach((h, c) => { cells[`0-${c}`] = h; });
  dataRow.forEach((v, c) => { cells[`1-${c}`] = v; });

  return {
    id: uid(),
    type: 'table',
    visible: true,
    locked: false,
    label,
    style: { padding: '8px', width: '100%' },
    conditions: [],
    properties: {
      rows,
      cols,
      headerRow: true,
      cellPadding: '6px 8px',
      borderColor: '#cccccc',
      headerBg: '#f5f5f5',
      cells,
      // Column widths as percentages (equal by default)
      colWidths: headers.map(() => Math.round(100 / cols)),
      ...extra,
    },
  };
}

// Shipping label is a 2-column layout block, not a simple table
function makeShippingLabelBlock() {
  return {
    id: uid(),
    type: 'columns',
    visible: true,
    locked: false,
    label: 'Generic Shipping Label',
    style: { padding: '8px', width: '100%' },
    conditions: [],
    properties: {
      columnCount: 2,
      gap: '16px',
      ratios: [1, 1],
    },
    children: [
      {
        id: uid(),
        type: '_column',
        properties: {},
        style: {},
        conditions: [],
        visible: true,
        locked: false,
        children: [
          {
            id: uid(),
            type: 'text',
            visible: true,
            locked: false,
            label: 'From Address',
            style: { padding: '8px' },
            conditions: [],
            properties: {
              content: 'From:\n{{seller.name}}\n{{seller.address_line1}}\n{{seller.city}}, {{seller.postal_code}}',
              fontFamily: "'Segoe UI',system-ui,sans-serif",
              fontSize: '13px',
              fontWeight: '400',
              color: '#333333',
              textAlign: 'left',
              lineHeight: '1.5',
              letterSpacing: '0',
            },
          },
        ],
      },
      {
        id: uid(),
        type: '_column',
        properties: {},
        style: {},
        conditions: [],
        visible: true,
        locked: false,
        children: [
          {
            id: uid(),
            type: 'barcode',
            visible: true,
            locked: false,
            label: 'Tracking Barcode',
            style: { padding: '8px', textAlign: 'center' },
            conditions: [],
            properties: {
              barcodeType: 'Code128',
              value: '{{order.tracking_number}}',
              barcodeWidth: '200',
              barcodeHeight: '60',
            },
          },
          {
            id: uid(),
            type: 'text',
            visible: true,
            locked: false,
            label: 'To Address',
            style: { padding: '8px' },
            conditions: [],
            properties: {
              content: 'To:\n{{customer.name}}\n{{shipping.address_line1}}\n{{shipping.city}}, {{shipping.postal_code}}\n{{shipping.country}}',
              fontFamily: "'Segoe UI',system-ui,sans-serif",
              fontSize: '13px',
              fontWeight: '600',
              color: '#111111',
              textAlign: 'left',
              lineHeight: '1.6',
              letterSpacing: '0',
            },
          },
        ],
      },
    ],
  };
}

const PREDEFINED_TABLES = [
  {
    label: 'Invoice Items',
    description: 'Item Title, SKU, Qty, Unit Price, Line Total',
    build: () => makeTableBlock(
      'Invoice Items',
      ['Item Title', 'SKU', 'Qty', 'Unit Price', 'Line Total'],
      ['{{line.title}}', '{{line.sku}}', '{{line.quantity}}', '{{line.unit_price}}', '{{line.line_total}}'],
    ),
  },
  {
    label: 'Invoice Totals',
    description: 'Subtotal, Postage, Tax, Total',
    build: () => {
      const block = makeTableBlock(
        'Invoice Totals',
        ['', ''],
        ['Subtotal', '{{order.subtotal}}'],
      );
      block.properties.rows = 5;
      block.properties.cells = {
        '0-0': 'Description', '0-1': 'Amount',
        '1-0': 'Subtotal',    '1-1': '{{order.subtotal}}',
        '2-0': 'Postage',     '2-1': '{{order.shipping_cost}}',
        '3-0': 'Tax',         '3-1': '{{order.tax}}',
        '4-0': 'Total',       '4-1': '{{order.total}}',
      };
      return block;
    },
  },
  {
    label: 'Invoice Address Block',
    description: 'Billing Address / Shipping Address',
    build: () => makeTableBlock(
      'Invoice Address Block',
      ['Billing Address', 'Shipping Address'],
      [
        '{{customer.name}}\n{{customer.address_line1}}\n{{customer.city}}, {{customer.postal_code}}',
        '{{shipping.name}}\n{{shipping.address_line1}}\n{{shipping.city}}, {{shipping.postal_code}}',
      ],
    ),
  },
  {
    label: 'Packing Slip Items',
    description: 'SKU, Item Title, Qty',
    build: () => makeTableBlock(
      'Packing Slip Items',
      ['SKU', 'Item Title', 'Qty'],
      ['{{line.sku}}', '{{line.title}}', '{{line.quantity}}'],
    ),
  },
  {
    label: 'Picking List Items',
    description: 'SKU, Item Title, Qty, Bin Rack',
    build: () => makeTableBlock(
      'Picking List Items',
      ['SKU', 'Item Title', 'Qty', 'Bin Rack'],
      ['{{line.sku}}', '{{line.title}}', '{{line.quantity}}', '{{line.bin_rack}}'],
    ),
  },
  {
    label: 'Packing List Items',
    description: 'SKU, Item Title, Qty, Weight',
    build: () => makeTableBlock(
      'Packing List Items',
      ['SKU', 'Item Title', 'Qty', 'Weight'],
      ['{{line.sku}}', '{{line.title}}', '{{line.quantity}}', '{{line.weight}}'],
    ),
  },
  {
    label: 'Purchase Order Lines',
    description: 'SKU, Description, Qty, Unit Cost, Line Total',
    build: () => makeTableBlock(
      'Purchase Order Line Items',
      ['SKU', 'Description', 'Qty', 'Unit Cost', 'Line Total'],
      ['{{line.sku}}', '{{line.title}}', '{{line.quantity}}', '{{line.unit_cost}}', '{{line.line_total}}'],
    ),
  },
  {
    label: 'Generic Shipping Label',
    description: 'Tracking barcode, From / To address',
    build: () => makeShippingLabelBlock(),
  },
];

/**
 * @param {Function} onAddBlock   — Called with a block type string (simple blocks)
 * @param {Function} onInsertBlock — Called with a fully-configured block object (pre-defined tables)
 */
export default function BlockPalette({ onAddBlock, onInsertBlock }) {
  const [tablesExpanded, setTablesExpanded] = useState(true);

  const handleDragStart = (e, type) => {
    e.dataTransfer.setData('application/x-block-type', type);
    e.dataTransfer.effectAllowed = 'copy';
  };

  const handlePredefDragStart = (e, buildFn) => {
    const block = buildFn();
    e.dataTransfer.setData('application/x-block-data', JSON.stringify(block));
    e.dataTransfer.effectAllowed = 'copy';
  };

  const handlePredefClick = (buildFn) => {
    if (onInsertBlock) {
      onInsertBlock(buildFn());
    } else if (onAddBlock) {
      // Fallback: insert as table type, caller handles it
      onAddBlock('table');
    }
  };

  return (
    <div style={{ padding: 12 }}>
      {/* ── Standard block grid ────────────────────────────── */}
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 6 }}>
        {BLOCK_TYPES.map((bt) => (
          <div
            key={bt.type}
            draggable
            onDragStart={(e) => handleDragStart(e, bt.type)}
            onClick={() => onAddBlock(bt.type)}
            style={{
              padding: '10px 8px',
              backgroundColor: T.bg.tertiary,
              border: `1px solid ${T.border.default}`,
              borderRadius: T.radius.lg,
              cursor: 'grab',
              display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 6,
              transition: 'all 150ms ease-in-out',
              userSelect: 'none',
            }}
            onMouseEnter={(e) => {
              e.currentTarget.style.backgroundColor = T.bg.elevated;
              e.currentTarget.style.borderColor = bt.color;
              e.currentTarget.style.boxShadow = `0 0 12px ${bt.color}33`;
            }}
            onMouseLeave={(e) => {
              e.currentTarget.style.backgroundColor = T.bg.tertiary;
              e.currentTarget.style.borderColor = T.border.default;
              e.currentTarget.style.boxShadow = 'none';
            }}
          >
            <bt.icon size={18} style={{ color: bt.color }} />
            <span style={{ fontSize: 11, fontWeight: 500, color: T.text.secondary }}>
              {bt.label}
            </span>
          </div>
        ))}
      </div>

      {/* ── Pre-defined Tables section ────────────────────── */}
      <div style={{ marginTop: 16 }}>
        {/* Section header */}
        <button
          onClick={() => setTablesExpanded((v) => !v)}
          style={{
            width: '100%',
            display: 'flex', alignItems: 'center', gap: 6,
            padding: '6px 4px',
            border: 'none',
            backgroundColor: 'transparent',
            cursor: 'pointer',
            color: T.text.primary,
            fontFamily: T.font,
            borderBottom: `1px solid ${T.border.default}`,
            marginBottom: 8,
          }}
        >
          <Table size={13} style={{ color: T.accent.teal }} />
          <span style={{ fontSize: 11, fontWeight: 700, color: T.text.primary, flex: 1, textAlign: 'left' }}>
            Pre-defined Tables
          </span>
          {tablesExpanded
            ? <ChevronDown size={12} style={{ color: T.text.muted }} />
            : <ChevronRight size={12} style={{ color: T.text.muted }} />
          }
        </button>

        {tablesExpanded && (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
            {PREDEFINED_TABLES.map((tpl) => (
              <div
                key={tpl.label}
                draggable
                onDragStart={(e) => handlePredefDragStart(e, tpl.build)}
                onClick={() => handlePredefClick(tpl.build)}
                style={{
                  padding: '8px 10px',
                  backgroundColor: T.bg.tertiary,
                  border: `1px solid ${T.border.default}`,
                  borderRadius: T.radius.md,
                  cursor: 'grab',
                  userSelect: 'none',
                  transition: 'all 150ms ease-in-out',
                }}
                onMouseEnter={(e) => {
                  e.currentTarget.style.backgroundColor = T.bg.elevated;
                  e.currentTarget.style.borderColor = T.accent.teal;
                  e.currentTarget.style.boxShadow = `0 0 8px ${T.accent.teal}33`;
                }}
                onMouseLeave={(e) => {
                  e.currentTarget.style.backgroundColor = T.bg.tertiary;
                  e.currentTarget.style.borderColor = T.border.default;
                  e.currentTarget.style.boxShadow = 'none';
                }}
              >
                <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 2 }}>
                  <Table size={12} style={{ color: T.accent.teal, flexShrink: 0 }} />
                  <span style={{ fontSize: 11, fontWeight: 600, color: T.text.primary }}>
                    {tpl.label}
                  </span>
                </div>
                <div style={{ fontSize: 10, color: T.text.muted, paddingLeft: 18 }}>
                  {tpl.description}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

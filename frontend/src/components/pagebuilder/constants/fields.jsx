// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// FIELD CATEGORIES & CANVAS PRESETS
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

import {
  ShoppingCart, User, Truck, Package, Hash, Store, PenTool
} from 'lucide-react';

// ── Dynamic field categories ───────────────────────────────────
// These define the merge tags available in the Dynamic Field block
// picker and in the AI content generator context. Grouped by entity.

export const FIELD_CATEGORIES = {
  Order: [
    { path: 'order.id',                 label: 'Order ID' },
    { path: 'order.numeric_id',         label: 'Numeric Order ID' },
    { path: 'order.external_reference', label: 'External Reference' },
    { path: 'order.date',               label: 'Date' },
    { path: 'order.processed_date',     label: 'Processed Date' },
    { path: 'order.dispatch_by_date',   label: 'Dispatch By Date' },
    { path: 'order.status',             label: 'Status' },
    { path: 'order.total',              label: 'Total' },
    { path: 'order.subtotal',           label: 'Subtotal' },
    { path: 'order.tax',                label: 'Tax' },
    { path: 'order.shipping_cost',      label: 'Shipping Cost' },
    { path: 'order.notes',              label: 'Notes' },
    { path: 'order.tracking_number',    label: 'Tracking Number' },
    { path: 'order.vendor',             label: 'Vendor / Carrier' },
    { path: 'order.currency',           label: 'Currency' },
    { path: 'order.payment_method',     label: 'Payment Method' },
  ],
  Customer: [
    { path: 'customer.name',  label: 'Name' },
    { path: 'customer.email', label: 'Email' },
    { path: 'customer.phone', label: 'Phone' },
  ],
  Shipping: [
    { path: 'shipping.name',          label: 'Name' },
    { path: 'shipping.address_line1', label: 'Address Line 1' },
    { path: 'shipping.address_line2', label: 'Address Line 2' },
    { path: 'shipping.address_line3', label: 'Address Line 3' },
    { path: 'shipping.city',          label: 'City' },
    { path: 'shipping.state',         label: 'State' },
    { path: 'shipping.postal_code',   label: 'Postal Code' },
    { path: 'shipping.country',       label: 'Country' },
    { path: 'shipping.method',        label: 'Method' },
  ],
  Product: [
    { path: 'product.title',       label: 'Title' },
    { path: 'product.sku',         label: 'SKU' },
    { path: 'product.price',       label: 'Price' },
    { path: 'product.description', label: 'Description' },
    { path: 'product.image_url',   label: 'Image URL' },
    { path: 'product.brand',       label: 'Brand' },
    { path: 'product.ean',         label: 'EAN' },
  ],
  'Line Item': [
    { path: 'line.sku',          label: 'SKU' },
    { path: 'line.title',        label: 'Title' },
    { path: 'line.quantity',     label: 'Quantity' },
    { path: 'line.unit_price',   label: 'Unit Price' },
    { path: 'line.line_total',   label: 'Line Total' },
    { path: 'line.batch_number', label: 'Batch Number' },
    { path: 'line.bin_rack',     label: 'Bin Rack' },
    { path: 'line.weight',       label: 'Weight' },
  ],
  Seller: [
    { path: 'seller.name',       label: 'Name' },
    { path: 'seller.address',    label: 'Address' },
    { path: 'seller.phone',      label: 'Phone' },
    { path: 'seller.email',      label: 'Email' },
    { path: 'seller.logo_url',   label: 'Logo URL' },
    { path: 'seller.vat_number', label: 'VAT Number' },
  ],
  Custom: Array.from({ length: 10 }, (_, i) => ({
    path:  `custom.field_${i + 1}`,
    label: `Field ${i + 1}`,
  })),
};

// ── Category icons (used in the FieldPicker dropdown) ──────────

export const CATEGORY_ICONS = {
  Order:       ShoppingCart,
  Customer:    User,
  Shipping:    Truck,
  Product:     Package,
  'Line Item': Hash,
  Seller:      Store,
  Custom:      PenTool,
};

// ── Canvas presets ─────────────────────────────────────────────
// Each template type maps to a default canvas configuration.
// When the user changes template type in the toolbar, the canvas
// dimensions, unit, and output format update accordingly.

// Template-type canvas presets — applied when the user changes template type.
// Document types use A4 (210x297mm); label types use 4x6in.
export const CANVAS_PRESETS = {
  email:              { width: 600,   height: 'auto', unit: 'px', label: 'Email (600px)' },
  ebay_listing:       { width: 800,   height: 'auto', unit: 'px', label: 'eBay Listing (800px)' },
  invoice:            { width: 210,   height: 297,    unit: 'mm', label: 'A4 Invoice' },
  packing_slip:       { width: 210,   height: 297,    unit: 'mm', label: 'A4 Packing Slip' },
  postage_label:      { width: 4,     height: 6,      unit: 'in', label: '4x6 Label' },
  custom:             { width: 800,   height: 1000,   unit: 'px', label: 'Custom' },
  // New template types
  amazon_vcs:         { width: 210,   height: 297,    unit: 'mm', label: 'Amazon VCS Invoice' },
  picking_list:       { width: 210,   height: 297,    unit: 'mm', label: 'Picking List' },
  packing_list:       { width: 210,   height: 297,    unit: 'mm', label: 'Packing List' },
  stock_item_label:   { width: 4,     height: 6,      unit: 'in', label: '4x6 Stock Label' },
  purchase_order:     { width: 210,   height: 297,    unit: 'mm', label: 'Purchase Order' },
  consignment:        { width: 210,   height: 297,    unit: 'mm', label: 'Consignment' },
  warehouse_transfer: { width: 210,   height: 297,    unit: 'mm', label: 'Warehouse Transfer' },
};

// Page-size presets — available in the Canvas Settings panel as quick-select size buttons.
// Separate from template-type presets so users can change canvas size independently of type.
export const PAGE_SIZE_PRESETS = [
  { key: 'a4',        label: 'A4',        width: 210,   height: 297,   unit: 'mm' },
  { key: 'a5',        label: 'A5',        width: 148,   height: 210,   unit: 'mm' },
  { key: 'a6',        label: 'A6',        width: 105,   height: 148,   unit: 'mm' },
  { key: 'us_letter', label: 'US Letter', width: 215.9, height: 279.4, unit: 'mm' },
  { key: 'label_4x6', label: '4x6 in',    width: 4,     height: 6,     unit: 'in' },
  { key: 'email_600', label: 'Email 600', width: 600,   height: 'auto',unit: 'px' },
];

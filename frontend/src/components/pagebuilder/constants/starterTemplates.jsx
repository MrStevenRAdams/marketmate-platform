// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// STARTER TEMPLATES (Feature #31)
// Pre-built template definitions that users can choose when starting
// a new design. Each template includes the full block tree, canvas
// settings, theme, and template type metadata.
//
// Block IDs are generated at load time via uid() so every instance
// is unique.
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

import {
  FileText, Package, ShoppingCart, Mail, Tag, Heart,
} from 'lucide-react';

// ── Helper: build block with fresh ID placeholder ─────────────
// The actual uid() call is deferred to load time.
const b = (type, properties = {}, style = {}, extra = {}) => ({
  type,
  properties,
  style,
  visible: true,
  locked: false,
  label: extra.label || '',
  conditions: extra.conditions || [],
  children: extra.children || undefined,
});

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 1. BASIC INVOICE
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

const INVOICE_BLOCKS = [
  // Logo
  b('logo', { src: '', maxWidth: '180px', maxHeight: '60px' }, { padding: '16px 8px 8px' }, { label: 'Company Logo' }),

  // Invoice header row
  b('columns', { columnCount: 2, gap: '16px', ratios: [1, 1] }, { padding: '8px' }, {
    label: 'Invoice Header',
    children: [
      b('_column', {}, {}, {
        children: [
          b('text', { content: 'INVOICE', fontSize: '28px', fontWeight: '700', color: '#111827', textAlign: 'left', lineHeight: '1.2', letterSpacing: '-0.02em' }, { padding: '8px' }),
          b('text', { content: 'Invoice #: {{order.id}}\nDate: {{order.date}}\nStatus: {{order.status}}', fontSize: '13px', fontWeight: '400', color: '#6b7280', textAlign: 'left', lineHeight: '1.6', letterSpacing: '0' }, { padding: '4px 8px' }),
        ],
      }),
      b('_column', {}, {}, {
        children: [
          b('text', { content: '{{seller.name}}\n{{seller.address}}\nPhone: {{seller.phone}}\nEmail: {{seller.email}}\nVAT: {{seller.vat_number}}', fontSize: '12px', fontWeight: '400', color: '#6b7280', textAlign: 'right', lineHeight: '1.6', letterSpacing: '0' }, { padding: '8px' }, { label: 'Seller Details' }),
        ],
      }),
    ],
  }),

  b('divider', { thickness: '2px', color: '#3b82f6', lineStyle: 'solid' }, { margin: '4px 8px 12px' }),

  // Bill To / Ship To
  b('columns', { columnCount: 2, gap: '16px', ratios: [1, 1] }, { padding: '8px' }, {
    label: 'Addresses',
    children: [
      b('_column', {}, {}, {
        children: [
          b('text', { content: 'BILL TO', fontSize: '10px', fontWeight: '700', color: '#9ca3af', textAlign: 'left', lineHeight: '1.5', letterSpacing: '0.1em' }, { padding: '4px 8px 2px' }),
          b('text', { content: '{{customer.name}}\n{{shipping.address_line1}}\n{{shipping.city}}, {{shipping.state}}\n{{shipping.postal_code}}, {{shipping.country}}\n{{customer.email}}', fontSize: '13px', fontWeight: '400', color: '#374151', textAlign: 'left', lineHeight: '1.5', letterSpacing: '0' }, { padding: '4px 8px' }),
        ],
      }),
      b('_column', {}, {}, {
        children: [
          b('text', { content: 'SHIP TO', fontSize: '10px', fontWeight: '700', color: '#9ca3af', textAlign: 'left', lineHeight: '1.5', letterSpacing: '0.1em' }, { padding: '4px 8px 2px' }),
          b('text', { content: '{{shipping.name}}\n{{shipping.address_line1}}\n{{shipping.city}}, {{shipping.state}}\n{{shipping.postal_code}}, {{shipping.country}}', fontSize: '13px', fontWeight: '400', color: '#374151', textAlign: 'left', lineHeight: '1.5', letterSpacing: '0' }, { padding: '4px 8px' }),
        ],
      }),
    ],
  }),

  b('spacer', { height: '12px' }, {}),

  // Line items table via repeater
  b('table', {
    rows: 2, cols: 5, headerRow: true, cellPadding: '10px', borderColor: '#e5e7eb',
    cells: {
      '0-0': 'SKU', '0-1': 'Item', '0-2': 'Qty', '0-3': 'Unit Price', '0-4': 'Total',
      '1-0': '{{line.sku}}', '1-1': '{{line.title}}', '1-2': '{{line.quantity}}', '1-3': '{{line.unit_price}}', '1-4': '{{line.line_total}}',
    },
  }, { padding: '8px' }, { label: 'Line Items Table' }),

  b('spacer', { height: '8px' }, {}),

  // Totals section
  b('columns', { columnCount: 2, gap: '16px', ratios: [3, 2] }, { padding: '8px' }, {
    label: 'Totals',
    children: [
      b('_column', {}, {}, { children: [] }),
      b('_column', {}, {}, {
        children: [
          b('text', { content: 'Subtotal: {{order.subtotal}}\nTax: {{order.tax}}\nShipping: {{order.shipping_cost}}', fontSize: '13px', fontWeight: '400', color: '#374151', textAlign: 'right', lineHeight: '1.8', letterSpacing: '0' }, { padding: '4px 8px' }),
          b('divider', { thickness: '1px', color: '#e5e7eb', lineStyle: 'solid' }, { margin: '4px 0' }),
          b('text', { content: 'TOTAL: {{order.total}}', fontSize: '18px', fontWeight: '700', color: '#111827', textAlign: 'right', lineHeight: '1.5', letterSpacing: '0' }, { padding: '4px 8px' }),
        ],
      }),
    ],
  }),

  b('spacer', { height: '16px' }, {}),

  // Footer
  b('divider', { thickness: '1px', color: '#e5e7eb', lineStyle: 'solid' }, { margin: '8px' }),
  b('text', { content: 'Payment Terms: Net 30 days. Please make payment to the account details provided on your purchase order.\nThank you for your business.', fontSize: '11px', fontWeight: '400', color: '#9ca3af', textAlign: 'center', lineHeight: '1.6', letterSpacing: '0' }, { padding: '8px 16px' }, { label: 'Payment Terms' }),
];

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 2. PACKING SLIP
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

const PACKING_SLIP_BLOCKS = [
  b('logo', { src: '', maxWidth: '160px', maxHeight: '50px' }, { padding: '16px 8px 4px' }, { label: 'Company Logo' }),

  b('columns', { columnCount: 2, gap: '16px', ratios: [1, 1] }, { padding: '8px' }, {
    label: 'Order & Address',
    children: [
      b('_column', {}, {}, {
        children: [
          b('text', { content: 'PACKING SLIP', fontSize: '22px', fontWeight: '700', color: '#111827', textAlign: 'left', lineHeight: '1.2', letterSpacing: '-0.01em' }, { padding: '8px' }),
          b('text', { content: 'Order: {{order.id}}\nDate: {{order.date}}\nShipping: {{shipping.method}}', fontSize: '12px', fontWeight: '400', color: '#6b7280', textAlign: 'left', lineHeight: '1.6', letterSpacing: '0' }, { padding: '4px 8px' }),
        ],
      }),
      b('_column', {}, {}, {
        children: [
          b('text', { content: 'SHIP TO', fontSize: '10px', fontWeight: '700', color: '#9ca3af', textAlign: 'left', lineHeight: '1.5', letterSpacing: '0.1em' }, { padding: '8px 8px 2px' }),
          b('text', { content: '{{shipping.name}}\n{{shipping.address_line1}}\n{{shipping.city}}, {{shipping.state}} {{shipping.postal_code}}\n{{shipping.country}}', fontSize: '13px', fontWeight: '500', color: '#374151', textAlign: 'left', lineHeight: '1.5', letterSpacing: '0' }, { padding: '4px 8px' }),
        ],
      }),
    ],
  }),

  b('divider', { thickness: '1px', color: '#e5e7eb', lineStyle: 'solid' }, { margin: '8px' }),

  b('table', {
    rows: 2, cols: 4, headerRow: true, cellPadding: '8px', borderColor: '#e5e7eb',
    cells: {
      '0-0': 'SKU', '0-1': 'Item', '0-2': 'Qty', '0-3': 'Check',
      '1-0': '{{line.sku}}', '1-1': '{{line.title}}', '1-2': '{{line.quantity}}', '1-3': '☐',
    },
  }, { padding: '8px' }, { label: 'Items Checklist' }),

  b('spacer', { height: '24px' }, {}),

  b('text', { content: 'Thank you for your order! If you have any issues, contact us at {{seller.email}} or {{seller.phone}}.', fontSize: '12px', fontWeight: '400', color: '#6b7280', textAlign: 'center', lineHeight: '1.5', letterSpacing: '0' }, { padding: '8px 16px' }, { label: 'Thank You Note' }),

  b('barcode', { barcodeType: 'Code128', value: '{{order.id}}', barcodeWidth: '200', barcodeHeight: '50' }, { padding: '8px', textAlign: 'center' }, { label: 'Order Barcode' }),
];

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 3. EBAY PRODUCT LISTING
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

const EBAY_LISTING_BLOCKS = [
  // Product image
  b('image', { src: '', alt: 'Product Image', objectFit: 'contain' }, { padding: '16px', width: '100%', height: '300px', borderRadius: '0' }, { label: 'Product Image' }),

  // Title
  b('text', { content: '{{product.title}}', fontSize: '24px', fontWeight: '700', color: '#111827', textAlign: 'left', lineHeight: '1.3', letterSpacing: '-0.01em' }, { padding: '12px 16px 4px' }, { label: 'Product Title' }),

  // Price + Brand
  b('columns', { columnCount: 2, gap: '12px', ratios: [1, 1] }, { padding: '4px 16px' }, {
    children: [
      b('_column', {}, {}, {
        children: [
          b('text', { content: '{{product.price}}', fontSize: '22px', fontWeight: '700', color: '#059669', textAlign: 'left', lineHeight: '1.3', letterSpacing: '0' }, { padding: '4px 0' }),
        ],
      }),
      b('_column', {}, {}, {
        children: [
          b('text', { content: 'Brand: {{product.brand}}\nSKU: {{product.sku}}', fontSize: '13px', fontWeight: '400', color: '#6b7280', textAlign: 'right', lineHeight: '1.5', letterSpacing: '0' }, { padding: '4px 0' }),
        ],
      }),
    ],
  }),

  b('divider', { thickness: '1px', color: '#e5e7eb', lineStyle: 'solid' }, { margin: '8px 16px' }),

  // Description
  b('text', { content: 'Product Description', fontSize: '16px', fontWeight: '600', color: '#374151', textAlign: 'left', lineHeight: '1.4', letterSpacing: '0' }, { padding: '8px 16px 4px' }),
  b('text', { content: '{{product.description}}\n\nFeatures include premium build quality, outstanding performance, and excellent value for money. This product comes with a full manufacturer warranty.', fontSize: '14px', fontWeight: '400', color: '#4b5563', textAlign: 'left', lineHeight: '1.6', letterSpacing: '0' }, { padding: '4px 16px' }),

  b('spacer', { height: '12px' }, {}),

  // Specs table
  b('text', { content: 'Specifications', fontSize: '16px', fontWeight: '600', color: '#374151', textAlign: 'left', lineHeight: '1.4', letterSpacing: '0' }, { padding: '8px 16px 4px' }),
  b('table', {
    rows: 4, cols: 2, headerRow: false, cellPadding: '8px', borderColor: '#e5e7eb',
    cells: {
      '0-0': 'Brand', '0-1': '{{product.brand}}',
      '1-0': 'SKU', '1-1': '{{product.sku}}',
      '2-0': 'EAN', '2-1': '{{product.ean}}',
      '3-0': 'Condition', '3-1': 'New',
    },
  }, { padding: '4px 16px' }, { label: 'Specifications Table' }),

  b('spacer', { height: '16px' }, {}),

  // Shipping policy box
  b('box', {}, { padding: '16px', backgroundColor: '#f0fdf4', border: '1px solid #86efac', borderRadius: '8px', minHeight: '40px', margin: '0 16px 12px' }, {
    label: 'Shipping Policy',
    children: [
      b('text', { content: '🚚 Shipping Policy', fontSize: '14px', fontWeight: '600', color: '#166534', textAlign: 'left', lineHeight: '1.4', letterSpacing: '0' }, { padding: '0 0 4px' }),
      b('text', { content: 'Free UK delivery on all orders. Items are dispatched within 1-2 business days via Royal Mail Tracked 24. International shipping available at checkout.', fontSize: '13px', fontWeight: '400', color: '#15803d', textAlign: 'left', lineHeight: '1.5', letterSpacing: '0' }, { padding: '0' }),
    ],
  }),

  // Returns policy box
  b('box', {}, { padding: '16px', backgroundColor: '#eff6ff', border: '1px solid #93c5fd', borderRadius: '8px', minHeight: '40px', margin: '0 16px 16px' }, {
    label: 'Returns Policy',
    children: [
      b('text', { content: '↩️ Returns Policy', fontSize: '14px', fontWeight: '600', color: '#1e40af', textAlign: 'left', lineHeight: '1.4', letterSpacing: '0' }, { padding: '0 0 4px' }),
      b('text', { content: '30-day money-back guarantee. Items must be returned in original packaging and unused condition. Buyer pays return postage unless the item is faulty.', fontSize: '13px', fontWeight: '400', color: '#1d4ed8', textAlign: 'left', lineHeight: '1.5', letterSpacing: '0' }, { padding: '0' }),
    ],
  }),

  // Seller info footer
  b('divider', { thickness: '1px', color: '#e5e7eb', lineStyle: 'solid' }, { margin: '4px 16px' }),
  b('text', { content: 'Sold by {{seller.name}} — {{seller.email}} — {{seller.phone}}', fontSize: '11px', fontWeight: '400', color: '#9ca3af', textAlign: 'center', lineHeight: '1.5', letterSpacing: '0' }, { padding: '8px 16px' }),
];

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 4. SHIPPING CONFIRMATION EMAIL
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

const EMAIL_BLOCKS = [
  b('logo', { src: '', maxWidth: '160px', maxHeight: '50px' }, { padding: '24px 16px 8px' }, { label: 'Company Logo' }),

  b('text', { content: 'Your order is on its way! 🎉', fontSize: '22px', fontWeight: '700', color: '#111827', textAlign: 'center', lineHeight: '1.3', letterSpacing: '-0.01em' }, { padding: '8px 16px' }),

  b('text', { content: 'Hi {{customer.name}},\n\nGreat news — your order {{order.id}} has been shipped and is heading your way!', fontSize: '14px', fontWeight: '400', color: '#374151', textAlign: 'left', lineHeight: '1.6', letterSpacing: '0' }, { padding: '8px 24px' }),

  b('spacer', { height: '8px' }, {}),

  // Shipping details box
  b('box', {}, { padding: '20px', backgroundColor: '#f8fafc', border: '1px solid #e2e8f0', borderRadius: '8px', minHeight: '40px', margin: '0 24px' }, {
    label: 'Shipping Details',
    children: [
      b('text', { content: '📦 Shipping Details', fontSize: '14px', fontWeight: '600', color: '#1e293b', textAlign: 'left', lineHeight: '1.4', letterSpacing: '0' }, { padding: '0 0 8px' }),
      b('text', { content: 'Method: {{shipping.method}}\nShipping to: {{shipping.name}}\n{{shipping.address_line1}}, {{shipping.city}} {{shipping.postal_code}}', fontSize: '13px', fontWeight: '400', color: '#475569', textAlign: 'left', lineHeight: '1.6', letterSpacing: '0' }, { padding: '0' }),
      b('dynamic_field', { fieldPath: 'custom.field_1', prefix: 'Tracking: ', suffix: '', fontSize: '13px', fontWeight: '600', color: '#3b82f6' }, { padding: '8px 0 0' }, { label: 'Tracking Number' }),
    ],
  }),

  b('spacer', { height: '16px' }, {}),

  // Order summary
  b('text', { content: 'Order Summary', fontSize: '15px', fontWeight: '600', color: '#1e293b', textAlign: 'left', lineHeight: '1.4', letterSpacing: '0' }, { padding: '4px 24px' }),
  b('table', {
    rows: 2, cols: 3, headerRow: true, cellPadding: '8px', borderColor: '#e2e8f0',
    cells: {
      '0-0': 'Item', '0-1': 'Qty', '0-2': 'Total',
      '1-0': '{{line.title}}', '1-1': '{{line.quantity}}', '1-2': '{{line.line_total}}',
    },
  }, { padding: '4px 24px' }, { label: 'Order Items' }),

  b('text', { content: 'Order Total: {{order.total}}', fontSize: '16px', fontWeight: '700', color: '#111827', textAlign: 'right', lineHeight: '1.5', letterSpacing: '0' }, { padding: '4px 24px' }),

  b('spacer', { height: '16px' }, {}),

  b('text', { content: 'Estimated delivery: 2-3 business days', fontSize: '13px', fontWeight: '500', color: '#059669', textAlign: 'center', lineHeight: '1.5', letterSpacing: '0' }, { padding: '4px 24px' }),

  b('spacer', { height: '16px' }, {}),

  b('divider', { thickness: '1px', color: '#e2e8f0', lineStyle: 'solid' }, { margin: '0 24px' }),
  b('text', { content: 'Need help? Contact us at {{seller.email}} or {{seller.phone}}\n{{seller.name}} — {{seller.address}}', fontSize: '11px', fontWeight: '400', color: '#94a3b8', textAlign: 'center', lineHeight: '1.6', letterSpacing: '0' }, { padding: '12px 24px' }, { label: 'Contact Footer' }),
];

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 5. 4×6 POSTAGE LABEL
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

const POSTAGE_LABEL_BLOCKS = [
  // From address (small)
  b('text', { content: 'FROM:', fontSize: '8px', fontWeight: '700', color: '#6b7280', textAlign: 'left', lineHeight: '1.2', letterSpacing: '0.1em' }, { padding: '8px 12px 1px' }),
  b('text', { content: '{{seller.name}}\n{{seller.address}}', fontSize: '10px', fontWeight: '400', color: '#374151', textAlign: 'left', lineHeight: '1.4', letterSpacing: '0' }, { padding: '1px 12px 8px' }, { label: 'From Address' }),

  b('divider', { thickness: '1px', color: '#d1d5db', lineStyle: 'dashed' }, { margin: '0 12px' }),

  // To address (large)
  b('text', { content: 'TO:', fontSize: '10px', fontWeight: '700', color: '#6b7280', textAlign: 'left', lineHeight: '1.2', letterSpacing: '0.1em' }, { padding: '10px 24px 2px' }),
  b('text', { content: '{{shipping.name}}', fontSize: '20px', fontWeight: '700', color: '#111827', textAlign: 'left', lineHeight: '1.3', letterSpacing: '0' }, { padding: '2px 24px' }, { label: 'Recipient Name' }),
  b('text', { content: '{{shipping.address_line1}}\n{{shipping.city}}, {{shipping.state}} {{shipping.postal_code}}\n{{shipping.country}}', fontSize: '16px', fontWeight: '500', color: '#1f2937', textAlign: 'left', lineHeight: '1.4', letterSpacing: '0' }, { padding: '2px 24px' }, { label: 'Recipient Address' }),

  b('spacer', { height: '12px' }, {}),

  // Barcode
  b('barcode', { barcodeType: 'Code128', value: '{{order.id}}', barcodeWidth: '250', barcodeHeight: '70' }, { padding: '4px', textAlign: 'center' }, { label: 'Order Barcode' }),

  // Shipping method badge
  b('box', {}, { padding: '6px 16px', backgroundColor: '#111827', border: 'none', borderRadius: '4px', minHeight: '20px', margin: '8px auto', width: 'auto' }, {
    label: 'Shipping Method',
    children: [
      b('text', { content: '{{shipping.method}}', fontSize: '12px', fontWeight: '700', color: '#ffffff', textAlign: 'center', lineHeight: '1.3', letterSpacing: '0.05em' }, { padding: '0' }),
    ],
  }),
];

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 6. THANK YOU CARD
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

const THANK_YOU_BLOCKS = [
  b('logo', { src: '', maxWidth: '140px', maxHeight: '50px' }, { padding: '24px 16px 8px' }, { label: 'Company Logo' }),

  b('text', { content: 'Thank You, {{customer.name}}! ❤️', fontSize: '24px', fontWeight: '700', color: '#111827', textAlign: 'center', lineHeight: '1.3', letterSpacing: '-0.01em' }, { padding: '8px 16px' }),

  b('text', { content: 'We truly appreciate your order and your support means the world to us. We hope you love your purchase!', fontSize: '14px', fontWeight: '400', color: '#4b5563', textAlign: 'center', lineHeight: '1.6', letterSpacing: '0' }, { padding: '4px 32px' }),

  b('spacer', { height: '8px' }, {}),

  // Order summary box
  b('box', {}, { padding: '16px 20px', backgroundColor: '#faf5ff', border: '1px solid #e9d5ff', borderRadius: '8px', minHeight: '40px', margin: '0 24px' }, {
    label: 'Order Summary',
    children: [
      b('text', { content: 'Your Order', fontSize: '13px', fontWeight: '600', color: '#7c3aed', textAlign: 'left', lineHeight: '1.4', letterSpacing: '0.03em' }, { padding: '0 0 4px' }),
      b('text', { content: 'Order: {{order.id}}\nDate: {{order.date}}\nTotal: {{order.total}}', fontSize: '13px', fontWeight: '400', color: '#6d28d9', textAlign: 'left', lineHeight: '1.6', letterSpacing: '0' }, { padding: '0' }),
    ],
  }),

  b('spacer', { height: '16px' }, {}),

  // Discount box
  b('box', {}, { padding: '20px', backgroundColor: '#fef3c7', border: '2px dashed #f59e0b', borderRadius: '8px', minHeight: '40px', margin: '0 24px' }, {
    label: 'Discount Code',
    children: [
      b('text', { content: 'ENJOY 10% OFF YOUR NEXT ORDER', fontSize: '14px', fontWeight: '700', color: '#92400e', textAlign: 'center', lineHeight: '1.3', letterSpacing: '0.02em' }, { padding: '0 0 4px' }),
      b('text', { content: 'Use code: THANKYOU10', fontSize: '20px', fontWeight: '700', color: '#d97706', textAlign: 'center', lineHeight: '1.3', letterSpacing: '0.05em' }, { padding: '0 0 4px' }),
      b('text', { content: 'Valid for 30 days from your purchase date', fontSize: '11px', fontWeight: '400', color: '#92400e', textAlign: 'center', lineHeight: '1.4', letterSpacing: '0' }, { padding: '0' }),
    ],
  }),

  b('spacer', { height: '16px' }, {}),

  b('text', { content: 'Follow us for updates, tips, and exclusive offers:', fontSize: '12px', fontWeight: '500', color: '#6b7280', textAlign: 'center', lineHeight: '1.5', letterSpacing: '0' }, { padding: '4px 24px' }),
  b('text', { content: '🌐 www.techdirect.co.uk  |  📧 {{seller.email}}  |  📞 {{seller.phone}}', fontSize: '12px', fontWeight: '400', color: '#9ca3af', textAlign: 'center', lineHeight: '1.5', letterSpacing: '0' }, { padding: '2px 24px' }),

  b('spacer', { height: '12px' }, {}),
  b('text', { content: '{{seller.name}}', fontSize: '11px', fontWeight: '400', color: '#d1d5db', textAlign: 'center', lineHeight: '1.5', letterSpacing: '0' }, { padding: '4px 16px' }),
];

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// STARTER TEMPLATES ARRAY
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

const STARTER_TEMPLATES = [
  {
    name: 'Basic Invoice',
    type: 'invoice',
    description: 'Professional A4 invoice with logo, addresses, line items, and totals',
    icon: FileText,
    canvas: { width: 210, height: 297, unit: 'mm', backgroundColor: '#ffffff' },
    theme: 'default',
    blocks: INVOICE_BLOCKS,
  },
  {
    name: 'Packing Slip',
    type: 'packing_slip',
    description: 'Simplified packing slip with order details and item checklist',
    icon: Package,
    canvas: { width: 210, height: 297, unit: 'mm', backgroundColor: '#ffffff' },
    theme: 'default',
    blocks: PACKING_SLIP_BLOCKS,
  },
  {
    name: 'eBay Product Listing',
    type: 'ebay_listing',
    description: 'Product listing with image, specs table, and policy boxes',
    icon: ShoppingCart,
    canvas: { width: 800, height: 'auto', unit: 'px', backgroundColor: '#ffffff' },
    theme: 'default',
    blocks: EBAY_LISTING_BLOCKS,
  },
  {
    name: 'Shipping Confirmation',
    type: 'email',
    description: 'Email notification with tracking details and order summary',
    icon: Mail,
    canvas: { width: 600, height: 'auto', unit: 'px', backgroundColor: '#ffffff' },
    theme: 'default',
    blocks: EMAIL_BLOCKS,
  },
  {
    name: '4×6 Postage Label',
    type: 'postage_label',
    description: 'Shipping label with addresses, barcode, and method badge',
    icon: Tag,
    canvas: { width: 4, height: 6, unit: 'in', backgroundColor: '#ffffff' },
    theme: 'default',
    blocks: POSTAGE_LABEL_BLOCKS,
  },
  {
    name: 'Thank You Card',
    type: 'custom',
    description: 'Personalised thank you with order summary and discount code',
    icon: Heart,
    canvas: { width: 600, height: 'auto', unit: 'px', backgroundColor: '#ffffff' },
    theme: 'royal',
    blocks: THANK_YOU_BLOCKS,
  },
];

export default STARTER_TEMPLATES;

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// SAMPLE DATA
// Mock order data used when previewing templates. Merge tags like
// {{customer.name}} resolve against this object in preview mode.
// In production, the backend injects real order data at render time.
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

const SAMPLE_DATA = {
  order: {
    id:            'ORD-2024-00847',
    date:          '2026-02-13',
    status:        'Processing',
    total:         '£149.99',
    subtotal:      '£124.99',
    tax:           '£25.00',
    shipping_cost: '£0.00',
    notes:         '',
  },

  customer: {
    name:  'Sarah Mitchell',
    email: 'sarah.m@email.com',
    phone: '+44 7700 900123',
  },

  shipping: {
    name:          'Sarah Mitchell',
    address_line1: '42 Kensington High St',
    address_line2: '',
    city:          'London',
    state:         'Greater London',
    postal_code:   'W8 4PT',
    country:       'GB',
    method:        'Royal Mail Tracked 24',
  },

  product: {
    title:       'Wireless Noise-Cancelling Headphones',
    sku:         'WH-NC-100-BK',
    price:       '£124.99',
    description: 'Premium wireless headphones with active noise cancellation',
    brand:       'AudioPro',
    ean:         '5060123456789',
    image_url:   '',
  },

  seller: {
    name:       'TechDirect UK Ltd',
    address:    'Unit 7, Commerce Park, Bristol BS1 5QD',
    phone:      '0117 496 0023',
    email:      'orders@techdirect.co.uk',
    logo_url:   '',
    vat_number: 'GB 123 4567 89',
  },

  // Placeholder custom fields
  custom: {
    field_1: '', field_2: '', field_3: '', field_4: '', field_5: '',
    field_6: '', field_7: '', field_8: '', field_9: '', field_10: '',
  },

  // Line items — used by the Repeater block to iterate
  lines: [
    {
      sku:        'WH-NC-100-BK',
      title:      'Wireless Noise-Cancelling Headphones',
      quantity:   '1',
      unit_price: '£99.99',
      line_total: '£99.99',
    },
    {
      sku:        'USB-C-CBL-2M',
      title:      'USB-C Charging Cable 2m',
      quantity:   '2',
      unit_price: '£12.50',
      line_total: '£25.00',
    },
  ],
};

export default SAMPLE_DATA;

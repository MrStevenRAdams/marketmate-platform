import { useState, useCallback, useRef } from 'react';
import { createUserWithEmailAndPassword, signInWithEmailAndPassword } from 'firebase/auth';
import { auth } from '../contexts/AuthContext';

// ─── Types ────────────────────────────────────────────────────────────────────
interface LogEntry  { type: string; msg: string; ts: string; }
type ModuleStatus = Record<string, 'running' | 'done' | 'error'>;
type Log          = (type: string, msg: string) => void;

interface TenantSummary { tenant_id: string; name: string; }

interface Ctx {
  tenantId:         string;
  getToken:         () => Promise<string>;
  categoryIds?:     string[];
  attributeSetId?:  string;
  supplierIds?:     string[];
  warehouseZoneId?: string;
  productIds?:      { id: string; sku: string; title: string; price: number }[];
  credentialIds?:   { id: string; marketplace: string; name: string }[];
}

// ─── Module definitions ───────────────────────────────────────────────────────
const MODULES = [
  { id: 'tenant',          label: 'Tenant Setup',           icon: '🏢', color: '#6366f1', dependsOn: [],                                        description: 'Seller profile, settings, billing plan' },
  { id: 'categories',      label: 'Categories',             icon: '🗂',  color: '#8b5cf6', dependsOn: ['tenant'],                                description: '3-level category tree' },
  { id: 'attributes',      label: 'Attributes & Sets',      icon: '🏷',  color: '#a855f7', dependsOn: ['tenant'],                                description: '10 custom attributes + 1 set' },
  { id: 'suppliers',       label: 'Suppliers',              icon: '🏭',  color: '#ec4899', dependsOn: ['tenant'],                                description: '8 suppliers with terms & lead times' },
  { id: 'fulfilment',      label: 'Fulfilment Sources',     icon: '📦',  color: '#f43f5e', dependsOn: ['tenant'],                                description: 'Warehouses, 3PL, FBA, dropship' },
  { id: 'products',        label: 'Products & Variants',    icon: '🛍',  color: '#ef4444', dependsOn: ['categories', 'attributes', 'suppliers'],  description: '50+ products, variants, bundles' },
  { id: 'inventory',       label: 'Inventory & Locations',  icon: '🏗',  color: '#f97316', dependsOn: ['products', 'fulfilment'],                 description: 'Stock levels, binracks, adjustments' },
  { id: 'marketplace',     label: 'Marketplace Creds',      icon: '🌐',  color: '#f59e0b', dependsOn: ['tenant'],                                description: 'Amazon, eBay, Shopify, TikTok, Etsy' },
  { id: 'listings',        label: 'Listings',               icon: '📋',  color: '#eab308', dependsOn: ['products', 'marketplace'],               description: 'Cross-channel listing records' },
  { id: 'orders',          label: 'Orders',                 icon: '🛒',  color: '#84cc16', dependsOn: ['products', 'marketplace'],               description: '120 orders across all channels' },
  { id: 'dispatch',        label: 'Shipments & Dispatch',   icon: '🚚',  color: '#22c55e', dependsOn: ['orders', 'fulfilment'],                  description: 'Carrier configs, postage definitions' },
  { id: 'purchase_orders', label: 'Purchase Orders',        icon: '📄',  color: '#10b981', dependsOn: ['suppliers', 'products'],                 description: '30 POs in various lifecycle stages' },
  { id: 'rmas',            label: 'RMAs & Returns',         icon: '↩️',  color: '#06b6d4', dependsOn: ['orders'],                               description: '15 return authorisations' },
  { id: 'workflows',       label: 'Workflows & Automation', icon: '⚙️',  color: '#0ea5e9', dependsOn: ['fulfilment', 'orders'],                  description: '5 workflows + 8 automation rules' },
  { id: 'templates',       label: 'Email Templates',        icon: '✉️',  color: '#3b82f6', dependsOn: ['tenant'],                                description: 'Order, dispatch, return templates' },
  { id: 'analytics',       label: 'Analytics Seed',         icon: '📊',  color: '#6366f1', dependsOn: ['orders', 'dispatch'],                    description: 'Forecasting settings, saved views' },
];

// ─── API ──────────────────────────────────────────────────────────────────────
const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';

async function apiFetch(ctx: Ctx, method: string, path: string, body?: unknown) {
  const token = await ctx.getToken();
  const res = await fetch(`${API_BASE}${path}`, {
    method,
    headers: {
      'Content-Type':  'application/json',
      'Authorization': `Bearer ${token}`,
      'X-Tenant-Id':   ctx.tenantId,
    },
    ...(body !== undefined ? { body: JSON.stringify(body) } : {}),
  });
  if (!res.ok) throw new Error(`${method} ${path} → ${res.status}`);
  return res.json();
}

// ─── AI product generation ────────────────────────────────────────────────────
// Proxied through the backend to avoid CORS — never calls Anthropic directly from the browser.
async function callClaude(ctx: Ctx, system: string, user: string): Promise<Record<string, unknown>[]> {
  try {
    const token = await ctx.getToken?.();
    const res = await fetch(`${API_BASE}/ai/prompt`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'X-Tenant-Id': ctx.tenantId || '',
        ...(token ? { 'Authorization': `Bearer ${token}` } : {}),
      },
      body: JSON.stringify({ system, prompt: user, model: 'gemini-2.0-flash' }),
    });
    if (!res.ok) return [];
    const data = await res.json();
    const text = data.text || '[]';
    const parsed = JSON.parse(text.replace(/```json\n?|\n?```/g, '').trim());
    return Array.isArray(parsed) ? parsed : [];
  } catch { return []; }
}

// ─── Seed runners ─────────────────────────────────────────────────────────────

async function seedTenant(ctx: Ctx, _s: number, log: Log) {
  log('info', 'Configuring seller profile…');
  await apiFetch(ctx, 'PUT', '/settings/seller', {
    name: 'Acme Electronics Ltd',
    email: 'orders@acme-electronics.co.uk',
    phone: '+44 20 7946 0958',
    vat_number: 'GB123456789',
    address: '42 Commerce Park, Hemel Hempstead, HP2 7TU, GB',
    website: 'https://acme-electronics.co.uk',
    default_currency: 'GBP',
    base_currency: 'GBP',
    timezone: 'Europe/London',
    weight_unit: 'kg',
    dimension_unit: 'cm',
    my_country: 'GB',
    default_warehouse_country: 'GB',
    date_format: 'DD/MM/YYYY',
    tax_for_direct_orders: 'include',
  }).catch(() => {});
  log('success', `✓ Tenant ${ctx.tenantId} configured`);
}

async function seedCategories(ctx: Ctx, scale: number, log: Log) {
  log('info', 'Building category tree…');
  const roots = ['Electronics','Clothing & Apparel','Home & Garden','Sports & Outdoors','Health & Beauty','Toys & Games','Books & Media','Automotive','Food & Grocery','Office Supplies'];
  const ids: string[] = [];
  for (const root of roots.slice(0, Math.max(3, Math.round(roots.length * scale)))) {
    try {
      const r = await apiFetch(ctx, 'POST', '/categories', { name: root, slug: root.toLowerCase().replace(/[^a-z0-9]/g, '-'), active: true, sort_order: roots.indexOf(root) });
      const rootId = r?.data?.category_id;
      if (rootId) {
        ids.push(rootId);
        for (const sub of [`${root} Accessories`, `Premium ${root}`, `Budget ${root}`]) {
          const sr = await apiFetch(ctx, 'POST', '/categories', { name: sub, slug: sub.toLowerCase().replace(/[^a-z0-9]/g, '-'), parent_id: rootId, active: true }).catch(() => null);
          if (sr?.data?.category_id) ids.push(sr.data.category_id);
        }
      }
    } catch { /* continue */ }
  }
  ctx.categoryIds = ids;
  log('success', `✓ Created ${ids.length} categories`);
}

async function seedAttributes(ctx: Ctx, _s: number, log: Log) {
  log('info', 'Creating attributes and attribute set…');
  const defs = [
    { name: 'Colour',            type: 'select',      options: ['Red','Blue','Green','Black','White','Silver','Gold'] },
    { name: 'Size',              type: 'select',      options: ['XS','S','M','L','XL','XXL'] },
    { name: 'Material',          type: 'text' },
    { name: 'Warranty (months)', type: 'number' },
    { name: 'Country of Origin', type: 'text' },
    { name: 'Voltage',           type: 'text' },
    { name: 'Power (W)',         type: 'number' },
    { name: 'Compatibility',     type: 'multiselect', options: ['iOS','Android','Windows','macOS','Linux'] },
    { name: 'Age Range',         type: 'select',      options: ['0-2','3-5','6-12','13-17','18+'] },
    { name: 'Certifications',    type: 'multiselect', options: ['CE','RoHS','FCC','UL','ETL'] },
  ];
  const attrIds: string[] = [];
  for (const d of defs) {
    try {
      const r = await apiFetch(ctx, 'POST', '/attributes', {
        name: d.name,
        code: d.name.toLowerCase().replace(/[^a-z0-9]/g, '_'),
        dataType: d.type,
        required: false,
        options: (d.options || []).map((o: string) => ({ id: o.toLowerCase().replace(/[^a-z0-9]/g, '_'), value: o, label: o })),
      });
      if (r?.data?.attribute_id) attrIds.push(r.data.attribute_id);
    } catch { /* continue */ }
  }
  try {
    const sr = await apiFetch(ctx, 'POST', '/attribute-sets', { name: 'Default Set', code: 'default_set', attributeIds: attrIds });
    ctx.attributeSetId = sr?.data?.attribute_set_id;
  } catch { /* continue */ }
  log('success', `✓ Created ${attrIds.length} attributes`);
}

async function seedSuppliers(ctx: Ctx, scale: number, log: Log) {
  log('info', 'Creating suppliers…');
  const list = [
    { name: 'TechSource Global Ltd', code: 'TSG', country: 'CN', payment_terms: 'NET30', lead_time_days: 21 },
    { name: 'EuroDistrib GmbH',       code: 'EDG', country: 'DE', payment_terms: 'NET15', lead_time_days: 7  },
    { name: 'Pacific Rim Wholesale',  code: 'PRW', country: 'HK', payment_terms: 'NET45', lead_time_days: 28 },
    { name: 'UK FastSupply Co',       code: 'UKF', country: 'GB', payment_terms: 'NET7',  lead_time_days: 3  },
    { name: 'Iberian Goods SA',       code: 'IGS', country: 'ES', payment_terms: 'NET30', lead_time_days: 14 },
    { name: 'Nordic Trade AS',        code: 'NTA', country: 'NO', payment_terms: 'NET14', lead_time_days: 10 },
    { name: 'Sunrise Manufacturing',  code: 'SRM', country: 'TW', payment_terms: 'NET60', lead_time_days: 35 },
    { name: 'GreenEarth Products',    code: 'GEP', country: 'NL', payment_terms: 'NET30', lead_time_days: 12 },
  ];
  const ids: string[] = [];
  for (const s of list.slice(0, Math.max(2, Math.round(list.length * scale)))) {
    try {
      const r = await apiFetch(ctx, 'POST', '/suppliers', { ...s, email: `orders@${s.code.toLowerCase()}.com`, phone: '+44 20 7946 0000', address: { city: 'London', country: s.country }, currency: 'GBP', active: true });
      const id = r?.data?.id || r?.data?.supplier_id;
      if (id) ids.push(id);
    } catch { /* continue */ }
  }
  ctx.supplierIds = ids;
  log('success', `✓ Created ${ids.length} suppliers`);
}

async function seedFulfilment(ctx: Ctx, _s: number, log: Log) {
  log('info', 'Creating fulfilment sources and warehouse locations…');
  const sources = [
    { name: 'Main Warehouse - Hemel', code: 'HML-01',   type: 'own_warehouse', isDefault: true  },
    { name: 'North Depot - Leeds',    code: 'LDS-01',   type: 'own_warehouse', isDefault: false },
    { name: '3PL Partner - XPO',      code: 'XPO-3PL',  type: '3pl',           isDefault: false },
    { name: 'Amazon FBA UK',          code: 'FBA-UK',   type: 'fba',           isDefault: false },
    { name: 'TechSource Dropship',    code: 'TSG-DROP', type: 'dropship',      isDefault: false },
  ];
  // Create fulfilment sources — capture main warehouse source_id for locations
  let mainWarehouseSourceId = '';
  for (const fs of sources) {
    try {
      const r = await apiFetch(ctx, 'POST', '/fulfilment-sources', {
        name: fs.name, code: fs.code, type: fs.type, active: true,
        default: fs.isDefault, inventory_tracked: fs.type === 'own_warehouse',
        address: { line1: '1 Industrial Estate', city: 'Hemel Hempstead', postcode: 'HP2 7TU', country: 'GB' },
      });
      // The response is { source_id, name, type } — capture main warehouse ID
      if (fs.isDefault && r?.source_id) mainWarehouseSourceId = r.source_id;
    } catch { /* continue */ }
  }
  // Create warehouse location tree — source_id is required by the handler
  try {
    const zone = await apiFetch(ctx, 'POST', '/locations', {
      name: 'Zone A - Main', source_id: mainWarehouseSourceId,
    });
    const zoneId = zone?.location?.location_id;
    ctx.warehouseZoneId = zoneId;
    for (let row = 1; row <= 3; row++) {
      const rowLoc = await apiFetch(ctx, 'POST', '/locations', {
        name: `Row A${row}`, source_id: mainWarehouseSourceId, parent_id: zoneId,
      }).catch(() => null);
      const rowId = rowLoc?.location?.location_id;
      if (rowId) {
        for (let bay = 1; bay <= 4; bay++) {
          await apiFetch(ctx, 'POST', '/locations', {
            name: `Bay A${row}-B${bay}`, source_id: mainWarehouseSourceId, parent_id: rowId,
          }).catch(() => {});
        }
      }
    }
  } catch { /* continue */ }
  log('success', `✓ Created ${sources.length} fulfilment sources + warehouse tree`);
}

async function seedProducts(ctx: Ctx, scale: number, log: Log, category: string) {
  const count = Math.round(50 * scale);
  log('info', `Generating ${count} products (AI-assisted)…`);
  const aiProducts = await callClaude(
    ctx,
    'You generate realistic ecommerce product data. Respond ONLY with a valid JSON array, no markdown, no preamble.',
    `Generate an array of ${Math.min(count, 15)} realistic ${category} products. Each must have: sku (like "ELC-001"), title, description (60 words), brand, ean (13-digit string), upc (12-digit string), mpn, list_price (GBP number), rrp (slightly higher), cost (lower), weight_kg, length_cm, width_cm, height_cm, tags (array of 3), key_features (array of 3), colour, material, warranty_months. Return only the JSON array.`
  );
  const fallback: Record<string, unknown>[] = Array.from({ length: Math.max(5, count - aiProducts.length) }, (_, i) => ({
    sku: `PROD-${String(i + 1).padStart(4, '0')}`, title: `${category} Item ${i + 1}`,
    description: `A high-quality ${category.toLowerCase()} product with excellent performance and durability.`,
    brand: ['Samsung','Sony','Philips','Bosch','LG'][i % 5],
    list_price: 29.99 + i * 12, rrp: 39.99 + i * 12, cost: 12 + i * 4,
    weight_kg: 0.4 + i * 0.1, length_cm: 20, width_cm: 15, height_cm: 5,
    colour: ['Black','White','Silver','Blue','Red'][i % 5], material: 'ABS Plastic',
    warranty_months: 12, key_features: ['Premium quality','Easy to use','1-year warranty'],
    tags: [category.toLowerCase(), 'quality', 'bestseller'],
  }));
  const all = [...aiProducts, ...fallback].slice(0, count);
  const productIds: Ctx['productIds'] = [];
  const suppId = ctx.supplierIds?.[0];
  for (const p of all) {
    try {
      const catId = ctx.categoryIds?.[Math.floor(Math.random() * (ctx.categoryIds?.length || 1))];
      const r = await apiFetch(ctx, 'POST', '/products', {
        sku: p.sku || `SKU-${Date.now()}`, title: p.title, description: p.description, brand: p.brand || 'Generic',
        product_type: 'simple', status: 'active', category_ids: catId ? [catId] : [],
        tags: p.tags || ['sale'], key_features: p.key_features || ['Quality','Durable','Warranted'],
        attribute_set_id: ctx.attributeSetId,
        attributes: { colour: p.colour || 'Black', material: p.material || 'Plastic', warranty_months: p.warranty_months || 12 },
        identifiers: {
          ean: p.ean || `590${String(Math.floor(Math.random() * 9999999999)).padStart(10, '0')}`,
          upc: p.upc || String(Math.floor(Math.random() * 999999999999)).padStart(12, '0'),
          mpn: p.mpn || `MPN-${p.sku}`,
        },
        dimensions: { length: p.length_cm || 20, width: p.width_cm || 15, height: p.height_cm || 5, unit: 'cm' },
        weight: { value: p.weight_kg || 0.5, unit: 'kg' },
        suppliers: suppId ? [{ supplier_id: suppId, supplier_sku: `TS-${p.sku}`, unit_cost: p.cost || 15, currency: 'GBP', lead_time_days: 14, priority: 1, is_default: true }] : [],
      });
      const pid = r?.data?.product_id;
      if (pid) {
        productIds.push({ id: pid, sku: p.sku as string, title: (p.title as string) || p.sku as string, price: (p.list_price as number) || 29.99 });
        await apiFetch(ctx, 'POST', `/products/${pid}/variants`, { sku: `${p.sku}-V1`, title: `${p.title} - Standard`, status: 'active', attributes: { colour: p.colour || 'Black', size: 'M' }, pricing: { list_price: { amount: p.list_price || 29.99, currency: 'GBP' }, rrp: { amount: p.rrp || 39.99, currency: 'GBP' }, cost: { amount: p.cost || 12, currency: 'GBP' } } }).catch(() => {});
      }
    } catch { /* continue */ }
  }
  for (let pi = 0; pi < Math.min(2, Math.round(5 * scale)); pi++) {
    try {
      const parent = await apiFetch(ctx, 'POST', '/products', { sku: `PARENT-${pi + 1}`, title: `${category} Multi-Variant ${pi + 1}`, description: `Available in multiple colours and sizes.`, brand: 'Acme', product_type: 'parent', status: 'active', tags: ['multivariant'] });
      const parentId = parent?.data?.product_id;
      if (parentId) {
        productIds.push({ id: parentId, sku: `PARENT-${pi + 1}`, title: `${category} Multi-Variant ${pi + 1}`, price: 49.99 });
        for (const col of ['Black','White','Blue','Red']) {
          await apiFetch(ctx, 'POST', `/products/${parentId}/variants`, { sku: `PARENT-${pi + 1}-${col.toUpperCase()}`, title: `${category} ${pi + 1} - ${col}`, status: 'active', attributes: { colour: col }, pricing: { list_price: { amount: 49.99, currency: 'GBP' }, rrp: { amount: 59.99, currency: 'GBP' }, cost: { amount: 20, currency: 'GBP' } } }).catch(() => {});
        }
      }
    } catch { /* continue */ }
  }
  ctx.productIds = productIds;
  log('success', `✓ Created ${productIds.length} products with variants`);
}

async function seedInventory(ctx: Ctx, _s: number, log: Log) {
  const products = ctx.productIds || [];

  // ── 1. Opening stock for ALL products ────────────────────────────────────
  log('info', `Setting opening stock for ${products.length} products…`);
  let stockCount = 0;
  for (const p of products) {
    const openingQty = Math.floor(Math.random() * 300) + 50;
    try {
      await apiFetch(ctx, 'POST', '/inventory/adjust', {
        sku: p.sku,
        location_id: ctx.warehouseZoneId,
        quantity: openingQty,
        type: 'receipt',
        reason_code: 'opening_stock',
        notes: 'Initial demo stock — system seeded',
      });
      stockCount++;
    } catch { /* continue */ }
  }
  log('success', `✓ Opening stock set for ${stockCount} products`);

  // ── 2. Purchase order receipts (stock-in movements) ────────────────────
  log('info', 'Creating PO receipt movements…');
  let poMoveCount = 0;
  const suppliers = ['TechSupplier Ltd', 'Global Goods Co', 'Premium Parts Inc', 'Direct Wholesale'];
  for (let i = 0; i < Math.min(products.length, 40); i++) {
    const p = products[i];
    const receiptDate = new Date(Date.now() - Math.random() * 60 * 24 * 60 * 60 * 1000);
    const qtyReceived = Math.floor(Math.random() * 150) + 25;
    try {
      await apiFetch(ctx, 'POST', '/inventory/adjust', {
        sku: p.sku,
        location_id: ctx.warehouseZoneId,
        quantity: qtyReceived,
        type: 'receipt',
        reason_code: 'purchase_order',
        notes: `PO receipt from ${suppliers[i % suppliers.length]}`,
      });
      poMoveCount++;
    } catch { /* continue */ }
  }
  log('success', `✓ ${poMoveCount} PO receipt movements created`);

  // ── 3. Order fulfilment movements (stock-out) ──────────────────────────
  log('info', 'Creating order fulfilment stock deductions…');
  const channels = ['amazon', 'ebay', 'shopify', 'tiktok', 'etsy'];
  let orderMoveCount = 0;
  for (let i = 0; i < Math.min(products.length, 60); i++) {
    const p = products[i % products.length];
    const channel = channels[i % channels.length];
    const qty = -(Math.floor(Math.random() * 5) + 1); // negative = stock out
    const orderDate = new Date(Date.now() - Math.random() * 90 * 24 * 60 * 60 * 1000);
    try {
      await apiFetch(ctx, 'POST', '/inventory/adjust', {
        sku: p.sku,
        location_id: ctx.warehouseZoneId,
        quantity: qty,
        type: 'adjustment',
        reason_code: 'order_fulfilled',
        notes: `Fulfilled order via ${channel.toUpperCase()}`,
      });
      orderMoveCount++;
    } catch { /* continue */ }
  }
  log('success', `✓ ${orderMoveCount} order fulfilment movements created`);

  // ── 4. Manual adjustments (stock-takes, damages, corrections) ─────────
  log('info', 'Creating manual stock adjustments…');
  const manualReasons = [
    { reason: 'damaged',           note: 'Stock damaged during handling', sign: -1 },
    { reason: 'stock_take',        note: 'Cycle count correction',        sign: 1  },
    { reason: 'stock_take',        note: 'Stock take — variance found',   sign: -1 },
    { reason: 'supplier_return',   note: 'Returned faulty units to supplier', sign: -1 },
    { reason: 'write_off',         note: 'Written off — expired/unsellable',  sign: -1 },
    { reason: 'customer_return',   note: 'Customer return — restocked',   sign: 1  },
    { reason: 'transfer',          note: 'Transferred from secondary warehouse', sign: 1 },
  ];
  let manualCount = 0;
  for (let i = 0; i < Math.min(products.length, 30); i++) {
    const p = products[Math.floor(Math.random() * products.length)];
    const adj = manualReasons[i % manualReasons.length];
    const qty = adj.sign * (Math.floor(Math.random() * 20) + 1);
    const adjDate = new Date(Date.now() - Math.random() * 30 * 24 * 60 * 60 * 1000);
    try {
      await apiFetch(ctx, 'POST', '/inventory/adjust', {
        sku: p.sku,
        location_id: ctx.warehouseZoneId,
        quantity: qty,
        type: 'adjustment',
        reason_code: adj.reason,
        notes: adj.note,
      });
      manualCount++;
    } catch { /* continue */ }
  }
  log('success', `✓ ${manualCount} manual adjustments created`);

  log('success', '✓ Inventory seeding complete — movements available in analytics reports');
}

async function seedMarketplace(ctx: Ctx, scale: number, log: Log) {
  log('info', 'Creating marketplace credential stubs…');
  const channels = [
    { channel: 'amazon',      account_name: 'Amazon UK',          environment: 'sandbox', credentials: { marketplace_id: 'A1F83G8C2ARO7P' } },
    { channel: 'ebay',        account_name: 'eBay UK',            environment: 'sandbox', credentials: { site_id: '3' } },
    { channel: 'shopify',     account_name: 'Demo Shopify Store', environment: 'sandbox', credentials: { store_url: 'demo-store.myshopify.com', api_key: 'DEMO_KEY' } },
    { channel: 'tiktok',      account_name: 'TikTok Shop UK',     environment: 'sandbox', credentials: { app_id: 'DEMO_APP_ID' } },
    { channel: 'etsy',        account_name: 'Etsy Shop',          environment: 'sandbox', credentials: { shop_id: '12345678' } },
    { channel: 'woocommerce', account_name: 'WooCommerce Store',  environment: 'sandbox', credentials: { store_url: 'https://demo.example.com', consumer_key: 'DEMO_CK', consumer_secret: 'DEMO_CS' } },
  ];
  const credIds: Ctx['credentialIds'] = [];
  for (const ch of channels.slice(0, Math.max(2, Math.round(channels.length * scale)))) {
    try {
      const r = await apiFetch(ctx, 'POST', '/marketplace/credentials', ch);
      const id = r?.data?.id;
      if (id) credIds.push({ id, marketplace: ch.marketplace, name: ch.name });
    } catch { /* continue */ }
  }
  ctx.credentialIds = credIds;
  log('success', `✓ Created ${credIds.length} marketplace credentials`);
}

async function seedListings(ctx: Ctx, scale: number, log: Log) {
  log('info', 'Creating listings…');
  let count = 0;
  for (const prod of (ctx.productIds || []).slice(0, Math.round(20 * scale))) {
    for (const cred of (ctx.credentialIds || []).slice(0, 2)) {
      try { await apiFetch(ctx, 'POST', '/marketplace/listings', { product_id: prod.id, credential_id: cred.id, marketplace: cred.marketplace, status: ['active','active','inactive','draft'][Math.floor(Math.random() * 4)], listing_data: { price: { amount: prod.price, currency: 'GBP' }, quantity: Math.floor(Math.random() * 50) + 5 } }); count++; } catch { /* continue */ }
    }
  }
  log('success', `✓ Created ${count} listings`);
}

async function seedOrders(ctx: Ctx, scale: number, log: Log) {
  const total = Math.round(120 * scale);
  log('info', `Creating ${total} orders…`);

  // Handler: POST /orders/manual expects:
  //   customer_name (required), shipping_address (required), line_items[{sku,quantity,price}] (required)
  //   channel, customer_email, customer_phone, billing_address, shipping_method, notes
  // Status is always set to "imported" by the handler — update it separately after creation.

  const channels   = ['amazon', 'ebay', 'shopify', 'tiktok'];
  // Statuses to patch onto each order after creation (cycling through for variety)
  const patchStatuses = ['imported', 'imported', 'processing', 'processing', 'ready', 'fulfilled', 'fulfilled', 'fulfilled', 'cancelled'];
  const firstNames = ['James','Emma','Oliver','Sophie','William','Charlotte','George','Amelia','Harry','Ella'];
  const lastNames  = ['Smith','Johnson','Williams','Brown','Jones','Davis','Miller','Wilson','Taylor','Anderson'];
  const cities     = ['London','Manchester','Birmingham','Leeds','Glasgow','Liverpool','Bristol','Sheffield','Edinburgh','Cardiff'];
  const streets    = ['High Street','Market Street','Church Lane','Park Road','Victoria Avenue'];
  const products   = ctx.productIds || [];

  let created = 0;
  let statusPatched = 0;

  for (let i = 0; i < total; i++) {
    const fn      = firstNames[i % firstNames.length];
    const ln      = lastNames[Math.floor(i / firstNames.length) % lastNames.length];
    const city    = cities[i % cities.length];
    const channel = channels[i % channels.length];
    const prod    = products[i % Math.max(products.length, 1)];
    const qty     = Math.floor(Math.random() * 3) + 1;
    const price   = prod?.price ?? 29.99;

    // Payload shaped exactly for CreateManualOrderRequest
    const payload = {
      channel,
      customer_name:  `${fn} ${ln}`,
      customer_email: `${fn.toLowerCase()}.${ln.toLowerCase()}@example.com`,
      customer_phone: `+44 7700 ${String(900000 + i).padStart(6, '0')}`,
      shipping_address: {
        name:          `${fn} ${ln}`,
        address_line1: `${i + 1} ${streets[i % streets.length]}`,
        city,
        postal_code:   `HP${(i % 20) + 1} ${(i % 9) + 1}AB`,
        country:       'GB',
      },
      billing_address: {
        name:          `${fn} ${ln}`,
        address_line1: `${i + 1} ${streets[i % 2]}`,
        city,
        postal_code:   `HP${(i % 20) + 1} ${(i % 9) + 1}AB`,
        country:       'GB',
      },
      line_items: prod
        ? [{ sku: prod.sku, title: prod.title || prod.sku, quantity: qty, price, currency: 'GBP' }]
        : [{ sku: `DEMO-SKU-${i}`, title: `Demo Product ${i}`, quantity: qty, price, currency: 'GBP' }],
      shipping_method: ['Royal Mail 1st Class', 'DPD Next Day', 'Evri Standard'][i % 3],
      notes: `Seeded order #${i + 1}`,
    };

    try {
      const res = await apiFetch(ctx, 'POST', '/orders/manual', payload);
      const orderId: string | undefined = res?.order_id;
      if (orderId) {
        created++;
        // Patch status for variety — skip if 'imported' since that's already the default
        const targetStatus = patchStatuses[i % patchStatuses.length];
        if (targetStatus !== 'imported') {
          try {
            await apiFetch(ctx, 'PATCH', `/orders/${orderId}/status`, { status: targetStatus });
            statusPatched++;
          } catch { /* non-fatal — order is still created */ }
        }
      }
    } catch { /* continue */ }
  }

  log('success', `✓ Created ${created}/${total} orders (${statusPatched} status-patched)`);
}

async function seedDispatch(ctx: Ctx, _s: number, log: Log) {
  log('info', 'Configuring carriers and postage definitions…');
  for (const id of ['royal_mail','dpd','evri']) await apiFetch(ctx, 'POST', `/dispatch/carriers/${id}/credentials`, { account_number: `DEMO-${id.toUpperCase()}`, api_key: 'demo_key', environment: 'sandbox' }).catch(() => {});
  for (const d of [
    { name: 'Standard UK', carrier: 'royal_mail', service: 'first_class', max_weight_kg: 2,  price: 3.99 },
    { name: 'Next Day UK', carrier: 'dpd',        service: 'next_day',    max_weight_kg: 30, price: 7.99 },
    { name: 'Economy UK',  carrier: 'evri',       service: 'standard',    max_weight_kg: 15, price: 2.49 },
    { name: 'International', carrier: 'royal_mail', service: 'tracked_int', max_weight_kg: 2, price: 9.99 },
  ]) await apiFetch(ctx, 'POST', '/postage-definitions', { ...d, active: true, countries: ['GB'] }).catch(() => {});
  log('success', '✓ 3 carriers + 4 postage definitions created');
}

async function seedPurchaseOrders(ctx: Ctx, scale: number, log: Log) {
  const total = Math.round(30 * scale);
  log('info', `Creating ${total} purchase orders…`);
  const statuses = ['draft','sent','sent','partially_received','received','received'];
  let created = 0;
  for (let i = 0; i < total; i++) {
    const suppId = ctx.supplierIds?.[i % Math.max(ctx.supplierIds?.length || 1, 1)];
    const prod = ctx.productIds?.[i % Math.max(ctx.productIds?.length || 1, 1)];
    const qty = Math.floor(Math.random() * 100) + 10, cost = 12 + i * 2;
    const status = statuses[i % statuses.length];
    const date = new Date(Date.now() - Math.random() * 60 * 24 * 60 * 60 * 1000);
    try {
      const expectedAt = new Date(date.getTime() + 14 * 24 * 60 * 60 * 1000);
      await apiFetch(ctx, 'POST', '/purchase-orders', {
        supplier_id: suppId,
        type: 'standard',
        order_method: 'manual',
        currency: 'GBP',
        notes: `PO-${String(i + 1).padStart(5, '0')} — seeded demo`,
        expected_at: expectedAt.toISOString(),
        lines: prod ? [{ internal_sku: prod.sku, product_id: prod.id, qty_ordered: qty, unit_cost: cost, description: prod.title || prod.sku }] : [],
      });
      created++;
    } catch { /* continue */ }
  }
  log('success', `✓ Created ${created} purchase orders`);
}

async function seedRMAs(ctx: Ctx, scale: number, log: Log) {
  log('info', 'Creating RMAs…');
  await apiFetch(ctx, 'POST', '/rmas/config', { auto_approve: false, return_window_days: 30, reasons: ['faulty','wrong_item','not_as_described','changed_mind','damaged_in_transit'], refund_methods: ['original_payment','store_credit','exchange'] }).catch(() => {});
  let count = 0;
  for (let i = 0; i < Math.round(15 * scale); i++) {
    try { await apiFetch(ctx, 'POST', '/rmas', { rma_number: `RMA-${String(i + 1).padStart(4, '0')}`, reason: ['faulty','wrong_item','not_as_described','changed_mind'][i % 4], status: ['requested','authorised','received','inspected','resolved'][i % 5], refund_method: ['original_payment','store_credit'][i % 2], customer_notes: 'Item arrived damaged', lines: [{ sku: ctx.productIds?.[i % (ctx.productIds?.length || 1)]?.sku || 'PROD-0001', quantity: 1, reason: 'faulty', condition: ['new','used','damaged'][i % 3] }] }); count++; } catch { /* continue */ }
  }
  log('success', `✓ Created ${count} RMAs`);
}

async function seedWorkflows(ctx: Ctx, _s: number, log: Log) {
  log('info', 'Creating workflows and automation rules…');
  let wfCount = 0;
  for (const wf of [
    { name: 'Standard UK Shipping', trigger: { type: 'order_imported', conditions: [{ field: 'shipping_address.country', operator: 'equals', value: 'GB' }] }, actions: [{ type: 'assign_carrier', carrier_id: 'royal_mail', service_code: 'first_class' }] },
    { name: 'Priority Next Day',    trigger: { type: 'order_imported', conditions: [{ field: 'tags', operator: 'contains', value: 'priority' }] }, actions: [{ type: 'assign_carrier', carrier_id: 'dpd', service_code: 'next_day' }] },
    { name: 'Heavy Items Economy',  trigger: { type: 'order_imported', conditions: [{ field: 'weight_kg', operator: 'greater_than', value: '5' }] }, actions: [{ type: 'assign_carrier', carrier_id: 'evri', service_code: 'standard' }] },
  ]) {
    try { const r = await apiFetch(ctx, 'POST', '/workflows', { ...wf, active: true }); if (r?.data?.id) { await apiFetch(ctx, 'POST', `/workflows/${r.data.id}/activate`, {}).catch(() => {}); wfCount++; } } catch { /* continue */ }
  }
  let ruleCount = 0;
  for (const rule of [
    { name: 'Auto-hold high-value orders', event: 'order.created',       conditions: [{ field: 'grand_total', operator: 'gt', value: 500 }], actions: [{ type: 'hold_order', reason: 'manual_review' }] },
    { name: 'Tag international orders',    event: 'order.created',       conditions: [{ field: 'country', operator: 'neq', value: 'GB' }],   actions: [{ type: 'add_tag', tag: 'international' }] },
    { name: 'Send dispatch email',         event: 'shipment.despatched', conditions: [],                                                       actions: [{ type: 'send_email', template: 'dispatch_notification' }] },
  ]) {
    try { await apiFetch(ctx, 'POST', '/automation/rules', { ...rule, active: true }); ruleCount++; } catch { /* continue */ }
  }
  log('success', `✓ Created ${wfCount} workflows + ${ruleCount} automation rules`);
}

async function seedTemplates(ctx: Ctx, _s: number, log: Log) {
  log('info', 'Creating email templates…');
  let count = 0;
  for (const t of [
    { name: 'Order Confirmation',    type: 'order_confirmation',    subject: 'Your order #{{order_number}} is confirmed' },
    { name: 'Dispatch Notification', type: 'dispatch_notification', subject: 'Your order has been despatched!' },
    { name: 'Return Confirmation',   type: 'return_confirmation',   subject: 'Return request received - RMA #{{rma_number}}' },
    { name: 'Review Request',        type: 'review_request',        subject: 'How did we do? Share your experience' },
  ]) {
    try { await apiFetch(ctx, 'POST', '/templates', { ...t, html: `<h1>Hello {{customer_name}}</h1><p>${t.name} template.</p>`, active: true, is_default: true }); count++; } catch { /* continue */ }
  }
  log('success', `✓ Created ${count} email templates`);
}

async function seedAnalytics(ctx: Ctx, _s: number, log: Log) {
  log('info', 'Seeding analytics configuration…');
  await apiFetch(ctx, 'PUT', '/forecasting/settings', { enabled: true, forecast_horizon_days: 90, reorder_lead_time_days: 14, safety_stock_days: 7, demand_model: 'moving_average', moving_average_window: 30 }).catch(() => {});
  await apiFetch(ctx, 'POST', '/order-views', { name: "Today's Orders", filters: { date_from: new Date().toISOString().split('T')[0] }, columns: ['order_id','customer','channel','total','status'], is_default: true }).catch(() => {});
  log('success', '✓ Analytics and saved views configured');
}

// ─── Runner map ───────────────────────────────────────────────────────────────
type Runner = (ctx: Ctx, scale: number, log: Log, category: string) => Promise<void>;
const RUNNERS: Record<string, Runner> = {
  tenant: (c,s,l) => seedTenant(c,s,l), categories: (c,s,l) => seedCategories(c,s,l),
  attributes: (c,s,l) => seedAttributes(c,s,l), suppliers: (c,s,l) => seedSuppliers(c,s,l),
  fulfilment: (c,s,l) => seedFulfilment(c,s,l), products: (c,s,l,cat) => seedProducts(c,s,l,cat),
  inventory: (c,s,l) => seedInventory(c,s,l), marketplace: (c,s,l) => seedMarketplace(c,s,l),
  listings: (c,s,l) => seedListings(c,s,l), orders: (c,s,l) => seedOrders(c,s,l),
  dispatch: (c,s,l) => seedDispatch(c,s,l), purchase_orders: (c,s,l) => seedPurchaseOrders(c,s,l),
  rmas: (c,s,l) => seedRMAs(c,s,l), workflows: (c,s,l) => seedWorkflows(c,s,l),
  templates: (c,s,l) => seedTemplates(c,s,l), analytics: (c,s,l) => seedAnalytics(c,s,l),
};

// ─── Styles ───────────────────────────────────────────────────────────────────
const S = {
  input:        { width: '100%', background: '#0a0e1a', border: '1px solid #1e2540', borderRadius: 6, padding: '8px 10px', color: '#e2e8f0', fontSize: 12, fontFamily: 'inherit', boxSizing: 'border-box' } as React.CSSProperties,
  label:        { fontSize: 10, color: '#64748b', display: 'block', marginBottom: 3 } as React.CSSProperties,
  card:         { background: '#111827', border: '1px solid #1e2540', borderRadius: 10, padding: 14 } as React.CSSProperties,
  sectionTitle: { fontSize: 10, color: '#6366f1', fontWeight: 600, letterSpacing: 2, marginBottom: 12, textTransform: 'uppercase' } as React.CSSProperties,
};

// ─── Component ────────────────────────────────────────────────────────────────
type Step = 'register' | 'ready' | 'running' | 'done';

export default function DataSeeder() {
  // ── Registration state ──────────────────────────────────────────────────────
  const [step, setStep]               = useState<Step>('register');
  const [email, setEmail]             = useState('');
  const [password, setPassword]       = useState('');
  const [companyName, setCompanyName] = useState('');
  const [regError, setRegError]       = useState('');
  const [registering, setRegistering] = useState(false);
  const [tenantId, setTenantId]       = useState('');
  const [tenantName, setTenantName]   = useState('');
  const getTokenRef = useRef<() => Promise<string>>(() => Promise.reject('not registered'));

  // ── Seeder state ────────────────────────────────────────────────────────────
  const [selectedMods, setSelectedMods] = useState<Set<string>>(new Set(MODULES.map(m => m.id)));
  const [running, setRunning]           = useState(false);
  const [logs, setLogs]                 = useState<LogEntry[]>([]);
  const [modStatus, setModStatus]       = useState<ModuleStatus>({});
  const [progress, setProgress]         = useState(0);
  const [category, setCategory]         = useState('Electronics');
  const [volume, setVolume]             = useState<'small' | 'medium' | 'large'>('medium');
  const logsRef                         = useRef<HTMLDivElement>(null);

  const scale = { small: 0.3, medium: 1, large: 2 }[volume];

  // ── Create account + auto-login ─────────────────────────────────────────────
  const handleRegister = async () => {
    if (!email || !password || !companyName) { setRegError('Please fill in all fields.'); return; }
    if (password.length < 8) { setRegError('Password must be at least 8 characters.'); return; }
    setRegistering(true); setRegError('');
    try {
      // 1. Create Firebase user
      const cred = await createUserWithEmailAndPassword(auth, email.trim().toLowerCase(), password);
      const fbUser = cred.user;

      // 2. Register with backend — creates tenant + membership record
      const resp = await fetch(`${API_BASE}/auth/register`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          firebase_uid: fbUser.uid,
          email:        email.trim().toLowerCase(),
          display_name: companyName.trim(),
          company_name: companyName.trim(),
          plan_id:      'starter_m',
        }),
      });
      const data = await resp.json();
      if (!resp.ok) throw new Error(data.error || 'Registration failed');

      // 3. Sign in to get a live token (getIdToken after createUser sometimes needs a refresh)
      const loginCred = await signInWithEmailAndPassword(auth, email.trim().toLowerCase(), password);
      const signedInUser = loginCred.user;
      getTokenRef.current = () => signedInUser.getIdToken();

      // 4. Fetch tenant list to get the new tenant ID
      const meResp = await fetch(`${API_BASE}/auth/me`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ firebase_uid: signedInUser.uid }),
      });
      const meData = await meResp.json();
      const firstTenant = meData.tenants?.[0];
      if (!firstTenant) throw new Error('Account created but no tenant found — please try again.');

      setTenantId(firstTenant.tenant_id);
      setTenantName(firstTenant.name);
      setStep('ready');
    } catch (e: unknown) {
      const msg = (e as Error).message || '';
      if (msg.includes('auth/email-already-in-use')) setRegError('An account with that email already exists.');
      else if (msg.includes('auth/weak-password'))   setRegError('Password must be at least 8 characters.');
      else if (msg.includes('auth/invalid-email'))   setRegError('Please enter a valid email address.');
      else setRegError(msg || 'Registration failed — please try again.');
    } finally {
      setRegistering(false);
    }
  };

  // ── Seeder helpers ──────────────────────────────────────────────────────────
  const addLog = useCallback((type: string, msg: string) => {
    setLogs(prev => {
      const next = [...prev, { type, msg, ts: new Date().toLocaleTimeString() }];
      setTimeout(() => { if (logsRef.current) logsRef.current.scrollTop = logsRef.current.scrollHeight; }, 10);
      return next;
    });
  }, []);

  const setStatus = (id: string, s: 'running' | 'done' | 'error') =>
    setModStatus(prev => ({ ...prev, [id]: s }));

  const toggleMod = (id: string) => {
    setSelectedMods(prev => {
      const next = new Set(prev);
      if (next.has(id)) { MODULES.forEach(m => { if (m.dependsOn.includes(id)) next.delete(m.id); }); next.delete(id); }
      else { MODULES.find(m => m.id === id)?.dependsOn.forEach(dep => next.add(dep)); next.add(id); }
      return next;
    });
  };

  const run = async () => {
    setRunning(true); setStep('running'); setLogs([]); setModStatus({}); setProgress(0);
    const ctx: Ctx = { tenantId, getToken: getTokenRef.current };
    const ordered = MODULES.filter(m => selectedMods.has(m.id));
    for (let i = 0; i < ordered.length; i++) {
      const mod = ordered[i];
      setStatus(mod.id, 'running');
      addLog('info', `━━ [${i + 1}/${ordered.length}] ${mod.label} ━━`);
      try { await RUNNERS[mod.id](ctx, scale, addLog, category); setStatus(mod.id, 'done'); }
      catch (e: unknown) { addLog('error', `✗ ${mod.label}: ${(e as Error).message}`); setStatus(mod.id, 'error'); }
      setProgress(Math.round(((i + 1) / ordered.length) * 100));
    }
    addLog('success', `🎉 Done! Log in to the app with: ${email}`);
    setRunning(false); setStep('done');
  };

  const statusIcon  = (id: string) => ({ running: '⟳', done: '✓', error: '✗' }[modStatus[id]] ?? (selectedMods.has(id) ? '○' : '–'));
  const statusColor = (id: string) => ({ running: '#f59e0b', done: '#22c55e', error: '#ef4444' }[modStatus[id]] ?? (selectedMods.has(id) ? '#94a3b8' : '#374151'));

  // ── Render ──────────────────────────────────────────────────────────────────
  return (
    <div style={{ fontFamily: "'IBM Plex Mono', monospace", background: '#0a0e1a', minHeight: '100vh', color: '#e2e8f0', display: 'flex', flexDirection: 'column' }}>
      <link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@400;500;600&family=Space+Grotesk:wght@500;700&display=swap" rel="stylesheet" />

      {/* Header */}
      <div style={{ background: 'linear-gradient(135deg,#1e1b4b,#0a0e1a)', borderBottom: '1px solid #1e2540', padding: '20px 28px', display: 'flex', alignItems: 'center', gap: 14 }}>
        <div style={{ background: 'linear-gradient(135deg,#6366f1,#8b5cf6)', width: 40, height: 40, borderRadius: 10, display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 20 }}>🌱</div>
        <div>
          <div style={{ fontFamily: "'Space Grotesk',sans-serif", fontSize: 20, fontWeight: 700, color: '#fff' }}>Data Seeder</div>
          <div style={{ fontSize: 11, color: '#6366f1', marginTop: 1 }}>Create a fresh account and populate it with demo data</div>
        </div>
        <div style={{ marginLeft: 'auto', display: 'flex', alignItems: 'center', gap: 10 }}>
          {step === 'register' && <div style={{ fontSize: 11, color: '#94a3b8', background: '#111827', border: '1px solid #1e2540', borderRadius: 6, padding: '4px 10px' }}>Step 1 of 2 — Create account</div>}
          {(step === 'ready' || step === 'running' || step === 'done') && (
            <div style={{ fontSize: 11, color: '#22c55e', background: '#052e16', border: '1px solid #166534', borderRadius: 6, padding: '4px 10px' }}>✓ {tenantName || tenantId}</div>
          )}
          {step === 'running' && (
            <>
              <div style={{ width: 160, height: 5, background: '#1e2540', borderRadius: 3, overflow: 'hidden' }}>
                <div style={{ width: `${progress}%`, height: '100%', background: 'linear-gradient(90deg,#6366f1,#22c55e)', transition: 'width 0.3s', borderRadius: 3 }} />
              </div>
              <span style={{ fontSize: 11, color: '#94a3b8' }}>{progress}%</span>
            </>
          )}
        </div>
      </div>

      {/* ── Step 1: Register ── */}
      {step === 'register' && (
        <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
          <div style={{ ...S.card, width: 380 }}>
            <div style={S.sectionTitle}>Create a new account</div>
            <p style={{ fontSize: 11, color: '#64748b', marginBottom: 18, lineHeight: 1.7 }}>
              A brand-new MarketMate account will be registered with these credentials.
              Once seeding is complete you can log in to the main app using the same email and password.
            </p>

            <div style={{ marginBottom: 10 }}>
              <label style={S.label}>Company name</label>
              <input style={S.input} type="text" value={companyName} onChange={e => setCompanyName(e.target.value)}
                placeholder="Acme Electronics Ltd" onKeyDown={e => e.key === 'Enter' && handleRegister()} autoFocus />
            </div>
            <div style={{ marginBottom: 10 }}>
              <label style={S.label}>Email</label>
              <input style={S.input} type="email" value={email} onChange={e => setEmail(e.target.value)}
                placeholder="you@company.com" onKeyDown={e => e.key === 'Enter' && handleRegister()} />
            </div>
            <div style={{ marginBottom: 18 }}>
              <label style={S.label}>Password <span style={{ color: '#374151', fontWeight: 400 }}>(min. 8 characters — remember this)</span></label>
              <input style={S.input} type="password" value={password} onChange={e => setPassword(e.target.value)}
                placeholder="••••••••" onKeyDown={e => e.key === 'Enter' && handleRegister()} />
            </div>

            {regError && (
              <div style={{ fontSize: 11, color: '#ef4444', background: '#1a0a0a', border: '1px solid #7f1d1d', borderRadius: 6, padding: '8px 10px', marginBottom: 14 }}>
                {regError}
              </div>
            )}

            <button onClick={handleRegister} disabled={registering}
              style={{ width: '100%', padding: 12, borderRadius: 8, border: 'none', cursor: registering ? 'not-allowed' : 'pointer', background: registering ? '#1e2540' : 'linear-gradient(135deg,#6366f1,#8b5cf6)', color: registering ? '#475569' : '#fff', fontFamily: "'Space Grotesk',sans-serif", fontWeight: 700, fontSize: 14 }}>
              {registering ? 'Creating account…' : 'Create account & continue →'}
            </button>
          </div>
        </div>
      )}

      {/* ── Steps 2–4: Configure & seed ── */}
      {(step === 'ready' || step === 'running' || step === 'done') && (
        <div style={{ display: 'flex', flex: 1, overflow: 'hidden' }}>

          {/* Left panel */}
          <div style={{ width: 310, borderRight: '1px solid #1e2540', padding: '16px 14px', overflowY: 'auto', display: 'flex', flexDirection: 'column', gap: 14 }}>

            {/* Credentials reminder */}
            <div style={{ background: '#052e16', border: '1px solid #166534', borderRadius: 8, padding: '10px 12px', fontSize: 11, lineHeight: 1.8, color: '#86efac' }}>
              ✓ Account ready<br />
              <span style={{ color: '#4ade80', fontWeight: 600 }}>{email}</span><br />
              <span style={{ color: '#374151' }}>Use these credentials to log in after seeding.</span>
            </div>

            {/* Config */}
            <div style={S.card}>
              <div style={S.sectionTitle}>Configuration</div>
              <div style={{ marginBottom: 10 }}>
                <label style={S.label}>Product Category</label>
                <select value={category} onChange={e => setCategory(e.target.value)} style={S.input}>
                  {['Electronics','Clothing','Home & Garden','Sports','Beauty','Toys','Automotive','Books','Office'].map(c => <option key={c}>{c}</option>)}
                </select>
              </div>
              <div>
                <label style={S.label}>Data Volume</label>
                <div style={{ display: 'flex', gap: 5 }}>
                  {(['small','medium','large'] as const).map(v => (
                    <button key={v} onClick={() => setVolume(v)}
                      style={{ flex: 1, padding: '5px 0', borderRadius: 6, border: '1px solid', borderColor: volume === v ? '#6366f1' : '#1e2540', background: volume === v ? '#1e1b4b' : 'transparent', color: volume === v ? '#818cf8' : '#64748b', fontSize: 10, cursor: 'pointer', fontFamily: 'inherit', textTransform: 'capitalize' }}>
                      {v}
                    </button>
                  ))}
                </div>
                <div style={{ fontSize: 9, color: '#374151', marginTop: 5 }}>
                  {volume === 'small' ? '~30% volume — quick test run' : volume === 'medium' ? 'Full demo dataset — recommended' : '2× volume — load/stress testing'}
                </div>
              </div>
            </div>

            {/* Modules */}
            <div style={S.card}>
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 10 }}>
                <div style={S.sectionTitle}>Modules</div>
                <div style={{ display: 'flex', gap: 8 }}>
                  <button onClick={() => setSelectedMods(new Set(MODULES.map(m => m.id)))} style={{ fontSize: 9, color: '#6366f1', background: 'none', border: 'none', cursor: 'pointer', fontFamily: 'inherit' }}>all</button>
                  <button onClick={() => setSelectedMods(new Set())} style={{ fontSize: 9, color: '#64748b', background: 'none', border: 'none', cursor: 'pointer', fontFamily: 'inherit' }}>none</button>
                </div>
              </div>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
                {MODULES.map(m => {
                  const sel = selectedMods.has(m.id);
                  return (
                    <div key={m.id} onClick={() => !running && toggleMod(m.id)} title={m.description}
                      style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '6px 8px', borderRadius: 6, background: sel ? '#0a0e1a' : 'transparent', border: `1px solid ${sel ? '#1e2540' : 'transparent'}`, cursor: running ? 'not-allowed' : 'pointer', opacity: running && !modStatus[m.id] ? 0.5 : 1 }}>
                      <div style={{ width: 7, height: 7, borderRadius: '50%', background: sel ? m.color : '#374151', flexShrink: 0 }} />
                      <div style={{ flex: 1, fontSize: 11, color: sel ? '#e2e8f0' : '#475569' }}>{m.icon} {m.label}</div>
                      <div style={{ fontSize: 12, color: statusColor(m.id), fontWeight: 600, flexShrink: 0 }}>{statusIcon(m.id)}</div>
                    </div>
                  );
                })}
              </div>
            </div>

            {step === 'ready' && (
              <button onClick={run} disabled={selectedMods.size === 0}
                style={{ padding: 13, borderRadius: 10, border: 'none', cursor: selectedMods.size === 0 ? 'not-allowed' : 'pointer', background: 'linear-gradient(135deg,#6366f1,#8b5cf6)', color: '#fff', fontFamily: "'Space Grotesk',sans-serif", fontWeight: 700, fontSize: 14, boxShadow: '0 0 24px rgba(99,102,241,0.3)' }}>
                🌱 Run Seed ({selectedMods.size} modules)
              </button>
            )}
            {step === 'running' && (
              <button disabled style={{ padding: 13, borderRadius: 10, border: 'none', cursor: 'not-allowed', background: '#1e2540', color: '#475569', fontFamily: "'Space Grotesk',sans-serif", fontWeight: 700, fontSize: 14 }}>
                Seeding… {progress}%
              </button>
            )}
            {step === 'done' && (
              <div style={{ fontSize: 12, color: '#22c55e', background: '#052e16', border: '1px solid #166534', borderRadius: 8, padding: '12px', textAlign: 'center', lineHeight: 1.7 }}>
                🎉 All done!<br />
                <span style={{ fontSize: 10, color: '#86efac' }}>Log in with:<br />{email}</span>
              </div>
            )}
          </div>

          {/* Log panel */}
          <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
            <div style={{ padding: '10px 18px', borderBottom: '1px solid #1e2540', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
              <div style={{ fontSize: 10, color: '#475569', letterSpacing: 1, textTransform: 'uppercase' }}>Execution Log</div>
              <button onClick={() => setLogs([])} style={{ fontSize: 9, color: '#374151', background: 'none', border: 'none', cursor: 'pointer', fontFamily: 'inherit' }}>clear</button>
            </div>
            <div ref={logsRef} style={{ flex: 1, overflowY: 'auto', padding: '14px 18px', display: 'flex', flexDirection: 'column', gap: 2 }}>
              {logs.length === 0 && (
                <div style={{ color: '#1e2540', fontSize: 12, textAlign: 'center', marginTop: 60, lineHeight: 1.8 }}>
                  Select modules and click Run Seed.<br />
                  <span style={{ fontSize: 10 }}>Data is created through the live API in dependency order.</span>
                </div>
              )}
              {logs.map((entry, i) => (
                <div key={i} style={{ display: 'flex', gap: 10, fontSize: 11, lineHeight: 1.6 }}>
                  <span style={{ color: '#374151', flexShrink: 0 }}>{entry.ts}</span>
                  <span style={{ color: entry.type === 'error' ? '#ef4444' : entry.type === 'success' ? '#22c55e' : entry.type === 'warn' ? '#f59e0b' : '#94a3b8', wordBreak: 'break-all' }}>{entry.msg}</span>
                </div>
              ))}
              {running && <div style={{ display: 'flex', gap: 10, fontSize: 11 }}><span style={{ color: '#374151' }}>···</span><span style={{ color: '#f59e0b' }}>working…</span></div>}
            </div>
            {!running && logs.length > 0 && (
              <div style={{ borderTop: '1px solid #1e2540', padding: '10px 18px', display: 'flex', gap: 20, fontSize: 10 }}>
                {[{ label: 'Completed', value: Object.values(modStatus).filter(s => s === 'done').length, color: '#22c55e' },
                  { label: 'Failed',    value: Object.values(modStatus).filter(s => s === 'error').length, color: '#ef4444' },
                  { label: 'Log entries', value: logs.length, color: '#6366f1' }].map(stat => (
                  <div key={stat.label} style={{ display: 'flex', gap: 5, alignItems: 'center' }}>
                    <span style={{ color: stat.color, fontWeight: 600, fontSize: 14 }}>{stat.value}</span>
                    <span style={{ color: '#374151' }}>{stat.label}</span>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

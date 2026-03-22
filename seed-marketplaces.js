#!/usr/bin/env node
// =============================================================================
// seed-marketplaces.js
// Populates the `marketplaces` Firestore collection with one document per
// channel.  Safe to re-run — uses set() with merge so it won't overwrite
// fields you've hand-edited in the Firestore console.
//
// Usage (PowerShell):
//   $env:GOOGLE_APPLICATION_CREDENTIALS = "serviceaccountkey.json"
//   $env:FIREBASE_PROJECT_ID = "your-project-id"
//   node seed-marketplaces.js
//
//   # Dry-run (prints what would be written, touches nothing):
//   $env:DRY_RUN = "true"
//   node seed-marketplaces.js
//
// Dependencies (install once):
//   npm install firebase-admin
// =============================================================================

const admin = require('firebase-admin');

const PROJECT_ID = process.env.FIREBASE_PROJECT_ID || 'YOUR_PROJECT_ID';
const DRY_RUN   = process.env.DRY_RUN === 'true';

admin.initializeApp({
  credential: admin.credential.applicationDefault(),
  projectId: PROJECT_ID,
});

const db = admin.firestore();

// =============================================================================
// MARKETPLACE DEFINITIONS
//
// Edit thumbnail_url / image_url to point at your own CDN / Firebase Storage.
// Set is_active: false for any channel you want hidden in the UI.
// sort_order controls display order (lower = higher up).
// adapter_type: "direct" = marketplace, "third_party" = storefront/platform.
// Mirakl-hosted marketplaces share the mirakl adapter on the backend.
// Each stores its own base_url so the adapter knows which platform to call.
// =============================================================================

const marketplaces = [

  // ── Tier 1 — Core channels ─────────────────────────────────────────────────

  {
    id: 'amazon',
    display_name: 'Amazon',
    description: 'Sell on Amazon Seller Central across multiple regions.',
    is_active: true,
    sort_order: 10,
    adapter_type: 'direct',
    thumbnail_url: '',
    image_url: '',
    features: ['import', 'listing', 'fba', 'variations', 'order_sync', 'inventory_sync', 'price_sync', 'tracking'],
    supported_regions: ['UK', 'US', 'CA', 'DE', 'FR', 'IT', 'ES', 'JP'],
    credential_fields: [
      { key: 'refresh_token',  label: 'Refresh Token',  type: 'password', required: true },
      { key: 'marketplace_id', label: 'Marketplace ID', type: 'text',     required: true },
      { key: 'seller_id',      label: 'Seller ID',      type: 'text',     required: true },
    ],
  },

  {
    id: 'ebay',
    display_name: 'eBay',
    description: 'List and manage auctions and fixed-price items on eBay.',
    is_active: true,
    sort_order: 20,
    adapter_type: 'direct',
    thumbnail_url: '',
    image_url: '',
    features: ['import', 'listing', 'order_sync', 'inventory_sync', 'price_sync', 'tracking'],
    supported_regions: ['UK', 'US', 'DE', 'AU', 'CA', 'FR', 'IT', 'ES'],
    credential_fields: [
      { key: 'refresh_token',   label: 'Refresh Token',        type: 'password', required: true },
      { key: 'marketplace_id',  label: 'Marketplace ID',       type: 'text',     required: false },
      { key: 'seller_username', label: 'eBay Seller Username', type: 'text',     required: false },
    ],
  },

  {
    id: 'temu',
    display_name: 'Temu',
    description: 'List products on Temu via the Open Platform API.',
    is_active: true,
    sort_order: 30,
    adapter_type: 'direct',
    thumbnail_url: '',
    image_url: '',
    features: ['import', 'listing'],
    supported_regions: ['EU', 'UK', 'US'],
    credential_fields: [
      { key: 'access_token', label: 'Access Token', type: 'password', required: true },
    ],
  },

  // ── Tier 2 — High-volume channels ──────────────────────────────────────────

  {
    id: 'amazon_vendor',
    display_name: 'Amazon Vendor Central',
    description: 'For first-party vendors selling wholesale to Amazon.',
    is_active: true,
    sort_order: 40,
    adapter_type: 'direct',
    thumbnail_url: '',
    image_url: '',
    features: ['vendor_orders'],
    supported_regions: ['UK', 'US', 'DE', 'FR', 'IT', 'ES', 'JP'],
    credential_fields: [
      { key: 'refresh_token',  label: 'Vendor Central Refresh Token', type: 'password', required: true,
        hint: 'From your Amazon Vendor Central developer application' },
      { key: 'vendor_id',      label: 'Vendor ID (Party ID)',         type: 'text',     required: true,
        hint: 'Found in Vendor Central → Settings → Account Info' },
      { key: 'marketplace_id', label: 'Marketplace ID',               type: 'text',     required: false,
        hint: 'e.g. A1F83G8C2ARO7P for UK' },
    ],
  },

  {
    id: 'tiktok',
    display_name: 'TikTok Shop',
    description: "Sell directly through TikTok's in-app shopping experience.",
    is_active: true,
    sort_order: 50,
    adapter_type: 'direct',
    thumbnail_url: '',
    image_url: '',
    features: ['import', 'listing', 'order_sync', 'tracking'],
    supported_regions: ['UK', 'US', 'SEA'],
    credential_fields: [
      { key: 'app_key',    label: 'App Key',    type: 'text',     required: true,  hint: 'TikTok Developer Portal → My Apps' },
      { key: 'app_secret', label: 'App Secret', type: 'password', required: true,  hint: 'TikTok Developer Portal → My Apps' },
    ],
  },

  {
    id: 'etsy',
    display_name: 'Etsy',
    description: 'Reach buyers looking for handmade, vintage, and unique items.',
    is_active: true,
    sort_order: 60,
    adapter_type: 'direct',
    thumbnail_url: '',
    image_url: '',
    features: ['import', 'listing', 'order_sync', 'tracking'],
    supported_regions: ['GLOBAL'],
    credential_fields: [
      { key: 'client_id', label: 'App Client ID', type: 'text', required: true,
        hint: 'From Etsy Developer Portal — the Client ID for your app' },
    ],
  },

  {
    id: 'onbuy',
    display_name: 'OnBuy',
    description: 'UK-focused marketplace with no listing fees.',
    is_active: true,
    sort_order: 70,
    adapter_type: 'direct',
    thumbnail_url: '',
    image_url: '',
    features: ['import', 'listing', 'order_sync', 'inventory_sync', 'price_sync', 'tracking'],
    supported_regions: ['GB'],
    credential_fields: [
      { key: 'consumer_key',    label: 'Consumer Key',    type: 'text',     required: true,
        hint: 'OnBuy Seller Centre → Integrations → API Credentials' },
      { key: 'consumer_secret', label: 'Consumer Secret', type: 'password', required: true },
      { key: 'site_id',         label: 'Site ID',         type: 'text',     required: false,
        hint: '2000 = OnBuy UK (default)' },
    ],
  },

  {
    id: 'kaufland',
    display_name: 'Kaufland',
    description: 'Leading European marketplace, strong in DACH region.',
    is_active: true,
    sort_order: 80,
    adapter_type: 'direct',
    thumbnail_url: '',
    image_url: '',
    features: ['import', 'listing', 'order_sync', 'inventory_sync', 'price_sync', 'tracking'],
    supported_regions: ['DE', 'SK', 'CZ', 'PL', 'HR', 'RO', 'BG'],
    credential_fields: [
      { key: 'client_key', label: 'Client Key', type: 'text',     required: true,
        hint: 'Kaufland Seller Centre → Settings → API' },
      { key: 'secret_key', label: 'Secret Key', type: 'password', required: true },
    ],
  },

  // ── Mirakl-hosted marketplaces ──────────────────────────────────────────────
  // These all use the shared Mirakl adapter on the backend.
  // Each document stores its own base_url in credential_fields so the
  // adapter knows which platform to connect to.

  {
    id: 'asos',
    display_name: 'ASOS Marketplace',
    description: 'Fashion marketplace powered by Mirakl. Requires ASOS seller approval.',
    is_active: true,
    sort_order: 83,
    adapter_type: 'direct',
    thumbnail_url: '',
    image_url: '',
    features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync'],
    supported_regions: ['GB', 'US', 'AU', 'FR', 'DE'],
    credential_fields: [
      { key: 'api_key',  label: 'API Key',  type: 'password', required: true,
        hint: 'ASOS Seller Portal → Settings → API Key' },
      { key: 'base_url', label: 'Base URL', type: 'text',     required: true,
        hint: 'https://marketplace.asos.com/api' },
    ],
  },

  {
    id: 'bandq',
    display_name: 'B&Q Marketplace',
    description: 'DIY and home improvement marketplace powered by Mirakl.',
    is_active: true,
    sort_order: 84,
    adapter_type: 'direct',
    thumbnail_url: '',
    image_url: '',
    features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync'],
    supported_regions: ['GB'],
    credential_fields: [
      { key: 'api_key',  label: 'API Key',  type: 'password', required: true,
        hint: 'B&Q Marketplace Seller Portal → Settings → API Key' },
      { key: 'base_url', label: 'Base URL', type: 'text',     required: true,
        hint: 'https://marketplace.diy.com/api' },
    ],
  },

  {
    id: 'backmarket',
    display_name: 'Back Market',
    description: 'Specialist refurbished goods marketplace operating in 16 countries.',
    is_active: true,
    sort_order: 90,
    adapter_type: 'direct',
    thumbnail_url: '',
    image_url: '',
    features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync'],
    supported_regions: ['FR', 'DE', 'GB', 'US', 'ES', 'IT', 'BE', 'NL', 'AT', 'JP', 'AU'],
    credential_fields: [
      { key: 'api_key',     label: 'API Key',     type: 'password', required: true,
        hint: 'Back Market Seller Dashboard → Settings → API → Generate Key' },
      { key: 'environment', label: 'Environment', type: 'select',   required: false,
        options: ['production', 'sandbox'] },
    ],
  },

  {
    id: 'zalando',
    display_name: 'Zalando',
    description: "Europe's leading fashion and lifestyle platform.",
    is_active: true,
    sort_order: 100,
    adapter_type: 'direct',
    thumbnail_url: '',
    image_url: '',
    features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync'],
    supported_regions: ['DE', 'AT', 'CH', 'FR', 'IT', 'NL', 'PL', 'BE', 'GB', 'SE', 'DK', 'FI', 'NO'],
    credential_fields: [
      { key: 'client_id',     label: 'Client ID',     type: 'text',     required: true,
        hint: 'Zalando Partner Portal → Settings → API Credentials' },
      { key: 'client_secret', label: 'Client Secret', type: 'password', required: true },
    ],
  },

  {
    id: 'bol',
    display_name: 'Bol.com',
    description: 'The largest online retailer in the Netherlands and Belgium.',
    is_active: true,
    sort_order: 110,
    adapter_type: 'direct',
    thumbnail_url: '',
    image_url: '',
    features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync'],
    supported_regions: ['NL', 'BE'],
    credential_fields: [
      { key: 'client_id',     label: 'Client ID',     type: 'text',     required: true,
        hint: 'Bol.com Retailer API → Settings → API Access' },
      { key: 'client_secret', label: 'Client Secret', type: 'password', required: true },
    ],
  },

  {
    id: 'lazada',
    display_name: 'Lazada',
    description: 'Dominant marketplace across six Southeast Asian countries.',
    is_active: true,
    sort_order: 120,
    adapter_type: 'direct',
    thumbnail_url: '',
    image_url: '',
    features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync'],
    supported_regions: ['MY', 'SG', 'TH', 'ID', 'PH', 'VN'],
    credential_fields: [
      { key: 'app_key',      label: 'App Key',      type: 'text',     required: true,
        hint: 'Lazada Seller Center → Account → Open Platform' },
      { key: 'app_secret',   label: 'App Secret',   type: 'password', required: true },
      { key: 'access_token', label: 'Access Token', type: 'password', required: true,
        hint: 'Generate via Lazada OAuth flow in Seller Center' },
      { key: 'base_url',     label: 'API Base URL', type: 'text',     required: true,
        hint: 'e.g. https://api.lazada.com.my/rest' },
    ],
  },

  {
    id: 'walmart',
    display_name: 'Walmart Marketplace',
    description: 'Sell to 150M+ monthly visitors on Walmart.com.',
    is_active: true,
    sort_order: 130,
    adapter_type: 'direct',
    thumbnail_url: '',
    image_url: '',
    features: ['import', 'listing', 'order_sync', 'inventory_sync', 'price_sync', 'tracking'],
    supported_regions: ['US'],
    credential_fields: [
      { key: 'client_id',     label: 'Client ID',     type: 'text',     required: true,
        hint: 'Walmart Marketplace → Settings → API Keys' },
      { key: 'client_secret', label: 'Client Secret', type: 'password', required: true },
    ],
  },

  {
    id: 'tesco',
    display_name: 'Tesco',
    description: 'UK grocery and general merchandise marketplace.',
    is_active: true,
    sort_order: 140,
    adapter_type: 'direct',
    thumbnail_url: '',
    image_url: '',
    features: ['listing'],
    supported_regions: ['GB'],
    credential_fields: [
      { key: 'api_key',    label: 'API Key',    type: 'text',     required: true },
      { key: 'api_secret', label: 'API Secret', type: 'password', required: true },
      { key: 'seller_id',  label: 'Seller ID',  type: 'text',     required: true },
    ],
  },


  // ── Mirakl-hosted marketplaces (UK) ────────────────────────────────────────
  // All use the shared Mirakl adapter. credential_fields: api_key (required),
  // base_url (required), shop_id (optional for multi-shop accounts).

  {
    id: 'asos',
    display_name: 'ASOS Marketplace',
    description: 'Fashion marketplace powered by Mirakl. Requires ASOS seller approval.',
    is_active: true, sort_order: 83, adapter_type: 'direct',
    thumbnail_url: '', image_url: '',
    features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync'],
    supported_regions: ['GB', 'US', 'AU', 'FR', 'DE'],
    credential_fields: [
      { key: 'api_key',  label: 'API Key',  type: 'password', required: true,  hint: 'ASOS Seller Portal > Profile > API Key' },
      { key: 'base_url', label: 'Base URL', type: 'text',     required: true,  hint: 'https://marketplace.asos.com/api' },
      { key: 'shop_id',  label: 'Shop ID',  type: 'text',     required: false, hint: 'Only required for multi-shop accounts' },
    ],
  },
  {
    id: 'bandq',
    display_name: 'B&Q Marketplace',
    description: 'DIY and home improvement marketplace powered by Mirakl.',
    is_active: true, sort_order: 84, adapter_type: 'direct',
    thumbnail_url: '', image_url: '',
    features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync'],
    supported_regions: ['GB'],
    credential_fields: [
      { key: 'api_key',  label: 'API Key',  type: 'password', required: true,  hint: 'B&Q Seller Portal > Profile > API Key' },
      { key: 'base_url', label: 'Base URL', type: 'text',     required: true,  hint: 'https://marketplace.diy.com/api' },
      { key: 'shop_id',  label: 'Shop ID',  type: 'text',     required: false },
    ],
  },
  {
    id: 'superdrug',
    display_name: 'Superdrug Marketplace',
    description: 'Health and beauty marketplace powered by Mirakl.',
    is_active: true, sort_order: 85, adapter_type: 'direct',
    thumbnail_url: '', image_url: '',
    features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync'],
    supported_regions: ['GB'],
    credential_fields: [
      { key: 'api_key',  label: 'API Key',  type: 'password', required: true,  hint: 'Superdrug Seller Portal > Profile > API Key' },
      { key: 'base_url', label: 'Base URL', type: 'text',     required: true,  hint: 'https://marketplace.superdrug.com/api' },
      { key: 'shop_id',  label: 'Shop ID',  type: 'text',     required: false },
    ],
  },
  {
    id: 'debenhams',
    display_name: 'Debenhams Marketplace',
    description: 'Fashion and lifestyle marketplace powered by Mirakl.',
    is_active: true, sort_order: 86, adapter_type: 'direct',
    thumbnail_url: '', image_url: '',
    features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync'],
    supported_regions: ['GB'],
    credential_fields: [
      { key: 'api_key',  label: 'API Key',  type: 'password', required: true,  hint: 'Debenhams Seller Portal > Profile > API Key' },
      { key: 'base_url', label: 'Base URL', type: 'text',     required: true,  hint: 'https://marketplace.debenhams.com/api' },
      { key: 'shop_id',  label: 'Shop ID',  type: 'text',     required: false },
    ],
  },
  {
    id: 'decathlon_uk',
    display_name: 'Decathlon UK',
    description: 'Sports and outdoor equipment marketplace powered by Mirakl.',
    is_active: true, sort_order: 87, adapter_type: 'direct',
    thumbnail_url: '', image_url: '',
    features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync'],
    supported_regions: ['GB'],
    credential_fields: [
      { key: 'api_key',  label: 'API Key',  type: 'password', required: true,  hint: 'Decathlon Seller Portal > Profile > API Key' },
      { key: 'base_url', label: 'Base URL', type: 'text',     required: true,  hint: 'https://marketplace.decathlon.co.uk/api' },
      { key: 'shop_id',  label: 'Shop ID',  type: 'text',     required: false },
    ],
  },
  {
    id: 'mountain_warehouse',
    display_name: 'Mountain Warehouse',
    description: 'Outdoor clothing and equipment marketplace powered by Mirakl.',
    is_active: true, sort_order: 88, adapter_type: 'direct',
    thumbnail_url: '', image_url: '',
    features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync'],
    supported_regions: ['GB'],
    credential_fields: [
      { key: 'api_key',  label: 'API Key',  type: 'password', required: true,  hint: 'Mountain Warehouse Seller Portal > Profile > API Key' },
      { key: 'base_url', label: 'Base URL', type: 'text',     required: true,  hint: 'https://marketplace.mountainwarehouse.com/api' },
      { key: 'shop_id',  label: 'Shop ID',  type: 'text',     required: false },
    ],
  },
  {
    id: 'jd_sports',
    display_name: 'JD Sports Marketplace',
    description: 'Sports fashion marketplace powered by Mirakl.',
    is_active: true, sort_order: 89, adapter_type: 'direct',
    thumbnail_url: '', image_url: '',
    features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync'],
    supported_regions: ['GB'],
    credential_fields: [
      { key: 'api_key',  label: 'API Key',  type: 'password', required: true,  hint: 'JD Sports Seller Portal > Profile > API Key' },
      { key: 'base_url', label: 'Base URL', type: 'text',     required: true,  hint: 'https://marketplace.jdsports.co.uk/api' },
      { key: 'shop_id',  label: 'Shop ID',  type: 'text',     required: false },
    ],
  },

  // ── Mirakl-hosted marketplaces (EU) ────────────────────────────────────────

  {
    id: 'carrefour',
    display_name: 'Carrefour Marketplace',
    description: 'European retail marketplace powered by Mirakl.',
    is_active: true, sort_order: 91, adapter_type: 'direct',
    thumbnail_url: '', image_url: '',
    features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync'],
    supported_regions: ['FR', 'ES', 'IT', 'PL', 'BE'],
    credential_fields: [
      { key: 'api_key',  label: 'API Key',  type: 'password', required: true,  hint: 'Carrefour Seller Portal > Profile > API Key' },
      { key: 'base_url', label: 'Base URL', type: 'text',     required: true,  hint: 'https://marketplace.carrefour.com/api' },
      { key: 'shop_id',  label: 'Shop ID',  type: 'text',     required: false },
    ],
  },
  {
    id: 'decathlon_fr',
    display_name: 'Decathlon France',
    description: 'Sports and outdoor equipment marketplace powered by Mirakl.',
    is_active: true, sort_order: 92, adapter_type: 'direct',
    thumbnail_url: '', image_url: '',
    features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync'],
    supported_regions: ['FR'],
    credential_fields: [
      { key: 'api_key',  label: 'API Key',  type: 'password', required: true,  hint: 'Decathlon Seller Portal > Profile > API Key' },
      { key: 'base_url', label: 'Base URL', type: 'text',     required: true,  hint: 'https://marketplace.decathlon.fr/api' },
      { key: 'shop_id',  label: 'Shop ID',  type: 'text',     required: false },
    ],
  },
  {
    id: 'fnac_darty',
    display_name: 'Fnac Darty Marketplace',
    description: 'French electronics and culture marketplace powered by Mirakl.',
    is_active: true, sort_order: 93, adapter_type: 'direct',
    thumbnail_url: '', image_url: '',
    features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync'],
    supported_regions: ['FR', 'BE'],
    credential_fields: [
      { key: 'api_key',  label: 'API Key',  type: 'password', required: true,  hint: 'Fnac Darty Seller Portal > Profile > API Key' },
      { key: 'base_url', label: 'Base URL', type: 'text',     required: true,  hint: 'https://marketplace.fnac.com/api' },
      { key: 'shop_id',  label: 'Shop ID',  type: 'text',     required: false },
    ],
  },
  {
    id: 'leroy_merlin',
    display_name: 'Leroy Merlin Marketplace',
    description: 'Home improvement marketplace powered by Mirakl.',
    is_active: true, sort_order: 94, adapter_type: 'direct',
    thumbnail_url: '', image_url: '',
    features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync'],
    supported_regions: ['FR', 'ES', 'IT', 'PL'],
    credential_fields: [
      { key: 'api_key',  label: 'API Key',  type: 'password', required: true,  hint: 'Leroy Merlin Seller Portal > Profile > API Key' },
      { key: 'base_url', label: 'Base URL', type: 'text',     required: true,  hint: 'https://marketplace.leroymerlin.fr/api' },
      { key: 'shop_id',  label: 'Shop ID',  type: 'text',     required: false },
    ],
  },
  {
    id: 'mediamarkt',
    display_name: 'MediaMarkt Marketplace',
    description: 'Electronics and technology marketplace powered by Mirakl.',
    is_active: true, sort_order: 95, adapter_type: 'direct',
    thumbnail_url: '', image_url: '',
    features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync'],
    supported_regions: ['DE', 'NL', 'ES', 'IT', 'AT'],
    credential_fields: [
      { key: 'api_key',  label: 'API Key',  type: 'password', required: true,  hint: 'MediaMarkt Seller Portal > Profile > API Key' },
      { key: 'base_url', label: 'Base URL', type: 'text',     required: true,  hint: 'https://marketplace.mediamarkt.de/api' },
      { key: 'shop_id',  label: 'Shop ID',  type: 'text',     required: false },
    ],
  },

  // ── Mirakl-hosted marketplaces (US) ────────────────────────────────────────

  {
    id: 'macys',
    display_name: "Macy's Marketplace",
    description: 'US department store marketplace powered by Mirakl.',
    is_active: true, sort_order: 96, adapter_type: 'direct',
    thumbnail_url: '', image_url: '',
    features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync'],
    supported_regions: ['US'],
    credential_fields: [
      { key: 'api_key',  label: 'API Key',  type: 'password', required: true,  hint: "Macy's Seller Portal > Profile > API Key" },
      { key: 'base_url', label: 'Base URL', type: 'text',     required: true,  hint: 'https://marketplace.macys.com/api' },
      { key: 'shop_id',  label: 'Shop ID',  type: 'text',     required: false },
    ],
  },
  {
    id: 'lowes',
    display_name: "Lowe's Marketplace",
    description: 'Home improvement marketplace powered by Mirakl.',
    is_active: true, sort_order: 97, adapter_type: 'direct',
    thumbnail_url: '', image_url: '',
    features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync'],
    supported_regions: ['US', 'CA'],
    credential_fields: [
      { key: 'api_key',  label: 'API Key',  type: 'password', required: true,  hint: "Lowe's Seller Portal > Profile > API Key" },
      { key: 'base_url', label: 'Base URL', type: 'text',     required: true,  hint: 'https://marketplace.lowes.com/api' },
      { key: 'shop_id',  label: 'Shop ID',  type: 'text',     required: false },
    ],
  },

  // ── Third-party storefronts ─────────────────────────────────────────────────

  {
    id: 'shopify',
    display_name: 'Shopify',
    description: 'Sync orders and inventory from your Shopify storefront.',
    is_active: true,
    sort_order: 200,
    adapter_type: 'third_party',
    thumbnail_url: '',
    image_url: '',
    features: ['listing', 'import', 'order_sync', 'inventory_sync', 'price_sync'],
    supported_regions: ['GLOBAL'],
    credential_fields: [
      { key: 'store_url',        label: 'Store URL',        type: 'text',     required: true },
      { key: 'admin_api_key',    label: 'Admin API Key',    type: 'text',     required: true },
      { key: 'admin_api_secret', label: 'Admin API Secret', type: 'password', required: true },
    ],
  },

  {
    id: 'woocommerce',
    display_name: 'WooCommerce',
    description: 'Connect your WooCommerce store for two-way order and stock sync.',
    is_active: true,
    sort_order: 210,
    adapter_type: 'third_party',
    thumbnail_url: '',
    image_url: '',
    features: ['import', 'listing', 'order_sync', 'inventory_sync', 'price_sync', 'tracking'],
    supported_regions: ['GLOBAL'],
    credential_fields: [
      { key: 'store_url',       label: 'Store URL',       type: 'text',     required: true,
        hint: 'Your WooCommerce store URL, e.g. https://mystore.com' },
      { key: 'consumer_key',    label: 'Consumer Key',    type: 'text',     required: true,
        hint: 'WooCommerce → Settings → Advanced → REST API' },
      { key: 'consumer_secret', label: 'Consumer Secret', type: 'password', required: true },
    ],
  },

  {
    id: 'magento',
    display_name: 'Magento 2',
    description: 'Enterprise-grade Magento 2 storefront integration.',
    is_active: true,
    sort_order: 220,
    adapter_type: 'third_party',
    thumbnail_url: '',
    image_url: '',
    features: ['import', 'listing', 'order_sync', 'inventory_sync', 'price_sync', 'tracking'],
    supported_regions: ['GLOBAL'],
    credential_fields: [
      { key: 'store_url',         label: 'Store URL',         type: 'text',     required: true,
        hint: 'Your Magento 2 store URL' },
      { key: 'integration_token', label: 'Integration Token', type: 'password', required: true,
        hint: 'Magento Admin → System → Integrations → Access Token' },
    ],
  },

  {
    id: 'bigcommerce',
    display_name: 'BigCommerce',
    description: 'Connect your BigCommerce store for order and inventory sync.',
    is_active: true,
    sort_order: 230,
    adapter_type: 'third_party',
    thumbnail_url: '',
    image_url: '',
    features: ['import', 'listing', 'order_sync', 'inventory_sync', 'price_sync', 'tracking'],
    supported_regions: ['GLOBAL'],
    credential_fields: [
      { key: 'store_hash',   label: 'Store Hash',   type: 'text',     required: true,
        hint: 'Found in BigCommerce Admin → Advanced Settings → API Accounts' },
      { key: 'client_id',    label: 'Client ID',    type: 'text',     required: false },
      { key: 'access_token', label: 'Access Token', type: 'password', required: true },
    ],
  },

];

// =============================================================================
// SEED
// =============================================================================

async function seed() {
  console.log(`\n🌱  Seeding marketplaces → Firestore project: ${PROJECT_ID}`);
  if (DRY_RUN) console.log('   DRY_RUN=true — nothing will be written\n');

  const col = db.collection('marketplace_registry');
  let ok = 0, skipped = 0, failed = 0;

  for (const mp of marketplaces) {
    const { id, ...data } = mp;
    const doc = { id, ...data, updated_at: admin.firestore.FieldValue.serverTimestamp() };

    if (DRY_RUN) {
      console.log(`  [dry-run] marketplaces/${id}  is_active=${data.is_active}  sort_order=${data.sort_order}  adapter_type=${data.adapter_type}`);
      skipped++;
      continue;
    }

    try {
      // merge: true — won't clobber thumbnail_url / image_url you've already set manually
      await col.doc(id).set(doc, { merge: true });
      console.log(`  ✅  marketplaces/${id}`);
      ok++;
    } catch (err) {
      console.error(`  ❌  marketplaces/${id}:`, err.message);
      failed++;
    }
  }

  console.log(`\n${DRY_RUN ? '🔍' : '✅'} Done — ${ok} written, ${skipped} dry-run, ${failed} failed.\n`);

  if (!DRY_RUN && failed === 0) {
    console.log('Next steps:');
    console.log('  • Upload logos to Firebase Storage and set thumbnail_url per document,');
    console.log('    or call PUT /api/v1/admin/marketplace/:id from your admin UI.');
    console.log('  • Set is_active: false on any channel you want hidden.');
    console.log('  • Adjust sort_order to control display order on the Connections page.');
    console.log('  • Delete any stale documents not in this list from the Firestore console');
    console.log('    (e.g. leftover italic Amazon/Temu/eBay docs from before the seed).\n');
  }

  process.exit(0);
}

seed().catch(err => { console.error(err); process.exit(1); });

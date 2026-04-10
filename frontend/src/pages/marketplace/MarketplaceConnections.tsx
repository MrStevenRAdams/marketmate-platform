// ============================================================================
// MARKETPLACE CONNECTIONS PAGE (UPDATED - Temu + Amazon)
// ============================================================================
// Location: frontend/src/pages/marketplace/MarketplaceConnections.tsx
//
// Amazon: Only asks for refresh_token + marketplace_id (company-wide keys
//         like LWA client ID, AWS keys etc. are stored globally on the backend)
// Temu:   Asks for app_key, app_secret, access_token, base_url
//         Access token obtained via Temu Open Platform authorization flow
// Other adapters: Full credential forms as before

import { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { getActiveTenantId } from '../../contexts/TenantContext';
import {
  credentialService,
  adapterService,
  MarketplaceCredential,
  AdapterInfo,
  ConnectMarketplaceRequest,
} from '../../services/marketplace-api';

// ── Adapter definitions with per-user credential fields only ──
const FALLBACK_ADAPTERS: AdapterInfo[] = [
  {
    id: 'amazon', name: 'amazon', display_name: 'Amazon', icon: 'ri-amazon-fill',
    color: 'text-orange-600', requires_oauth: true, is_active: true,
    supported_regions: ['US', 'UK', 'CA', 'DE', 'FR', 'IT', 'ES', 'JP'],
    features: ['import', 'listing', 'fba', 'variations'],
    credential_fields: [],
  },
  {
    id: 'temu', name: 'temu', display_name: 'Temu', icon: 'ri-store-2-fill',
    color: 'text-orange-500', requires_oauth: false, is_active: true,
    features: ['import', 'listing'],
    credential_fields: [
      { key: 'access_token', label: 'Access Token', type: 'password', required: true },
    ],
  },
  {
    id: 'ebay', name: 'ebay', display_name: 'eBay', icon: 'ri-auction-fill',
    color: 'text-blue-600', requires_oauth: true, is_active: true,
    supported_regions: ['UK', 'US', 'DE', 'AU', 'CA', 'FR', 'IT', 'ES'],
    features: ['import', 'listing'],
    credential_fields: [
      { key: 'refresh_token', label: 'Refresh Token', type: 'password', required: true },
      { key: 'marketplace_id', label: 'Marketplace ID', type: 'text', required: false },
      { key: 'seller_username', label: 'eBay Seller Username', type: 'text', required: false },
    ],
  },
  {
    id: 'shopify', name: 'shopify', display_name: 'Shopify', icon: 'ri-shopping-cart-fill',
    color: 'text-green-600', requires_oauth: true, is_active: true,
    features: ['listing', 'import', 'order_sync', 'inventory_sync', 'price_sync', 'tracking'],
    credential_fields: [],
  },
  {
    id: 'shopline', name: 'shopline', display_name: 'Shopline', icon: 'ri-shopping-cart-2-fill',
    color: 'text-teal-500', requires_oauth: true, is_active: true,
    features: ['listing', 'import', 'order_sync', 'inventory_sync', 'price_sync', 'tracking'],
    credential_fields: [],
  },
  {
    id: 'tesco', name: 'tesco', display_name: 'Tesco', icon: 'ri-store-3-fill',
    color: 'text-red-600', requires_oauth: false, is_active: true,
    features: ['listing'],
    credential_fields: [
      { key: 'api_key', label: 'API Key', type: 'text', required: true },
      { key: 'api_secret', label: 'API Secret', type: 'password', required: true },
      { key: 'seller_id', label: 'Seller ID', type: 'text', required: true },
    ],
  },
  {
    id: 'amazon_vendor', name: 'amazon_vendor', display_name: 'Amazon Vendor Central', icon: 'ri-amazon-fill',
    color: 'text-yellow-600', requires_oauth: true, is_active: true,
    supported_regions: ['UK', 'US', 'DE', 'FR', 'IT', 'ES', 'JP'],
    features: ['vendor_orders'],
    credential_fields: [
      { key: 'refresh_token', label: 'Vendor Central Refresh Token', type: 'password', required: true,
        hint: 'Obtained from your Amazon Vendor Central developer application — different from your Seller Central token' },
      { key: 'vendor_id', label: 'Vendor ID (Party ID)', type: 'text', required: true,
        hint: 'Your Vendor Party ID — found in Vendor Central under Settings > Account Info' },
      { key: 'marketplace_id', label: 'Marketplace ID', type: 'text', required: false,
        hint: 'e.g. A1F83G8C2ARO7P for UK. Leave blank to use UK default.' },
    ],
  },
  {
    id: 'tiktok', name: 'tiktok', display_name: 'TikTok Shop', icon: 'ri-tiktok-fill',
    color: 'text-black dark:text-white', requires_oauth: true, is_active: true,
    supported_regions: ['UK', 'US', 'SEA'],
    features: ['import', 'listing', 'order_sync', 'tracking'],
    credential_fields: [
      { key: 'app_key', label: 'App Key', type: 'text', required: true,
        hint: 'From TikTok Developer Portal → My Apps' },
      { key: 'app_secret', label: 'App Secret', type: 'password', required: true,
        hint: 'From TikTok Developer Portal → My Apps' },
    ],
  },
  {
    id: 'etsy', name: 'etsy', display_name: 'Etsy', icon: 'ri-store-2-fill',
    color: 'text-orange-500', requires_oauth: true, is_active: true,
    supported_regions: ['GLOBAL'],
    features: ['import', 'listing', 'order_sync', 'tracking'],
    credential_fields: [
      { key: 'client_id', label: 'App Client ID', type: 'text', required: true,
        hint: 'From Etsy Developer Portal — the Client ID (also called API key) for your app' },
    ],
  },
  {
    id: 'woocommerce', name: 'woocommerce', display_name: 'WooCommerce', icon: 'ri-store-3-fill',
    color: 'text-purple-600', requires_oauth: false, is_active: true,
    supported_regions: ['GLOBAL'],
    features: ['import', 'listing', 'order_sync', 'inventory_sync', 'price_sync', 'tracking'],
    credential_fields: [
      { key: 'store_url', label: 'Store URL', type: 'text', required: true,
        hint: 'Your WooCommerce store URL, e.g. https://mystore.com' },
      { key: 'consumer_key', label: 'Consumer Key', type: 'text', required: true,
        hint: 'WooCommerce → Settings → Advanced → REST API → Consumer Key' },
      { key: 'consumer_secret', label: 'Consumer Secret', type: 'password', required: true,
        hint: 'WooCommerce → Settings → Advanced → REST API → Consumer Secret' },
    ],
  },
  {
    id: 'shopwired', name: 'shopwired', display_name: 'ShopWired', icon: 'ri-shopping-cart-2-fill',
    color: 'text-orange-500', requires_oauth: false, is_active: true,
    supported_regions: ['GB'],
    features: ['listing', 'order_sync', 'inventory_sync', 'price_sync', 'tracking'],
    credential_fields: [
      { key: 'api_key', label: 'API Key', type: 'text', required: true,
        hint: 'ShopWired → Account → API Keys → API Key' },
      { key: 'api_secret', label: 'API Secret', type: 'password', required: true,
        hint: 'ShopWired → Account → API Keys → API Secret' },
      { key: 'store_name', label: 'Store Name', type: 'text', required: false,
        hint: 'Display label for this store (e.g. My ShopWired Store)' },
    ],
  },
  {
    id: 'walmart', name: 'walmart', display_name: 'Walmart Marketplace', icon: 'ri-store-2-fill',
    color: 'text-blue-500', requires_oauth: false, is_active: true,
    supported_regions: ['US'],
    features: ['import', 'listing', 'order_sync', 'inventory_sync', 'price_sync', 'tracking'],
    credential_fields: [
      { key: 'client_id', label: 'Client ID', type: 'text', required: true,
        hint: 'Walmart Marketplace → Settings → API Keys → Client ID' },
      { key: 'client_secret', label: 'Client Secret', type: 'password', required: true,
        hint: 'Walmart Marketplace → Settings → API Keys → Client Secret' },
    ],
  },
  {
    id: 'kaufland', name: 'kaufland', display_name: 'Kaufland', icon: 'ri-shopping-bag-3-fill',
    color: 'text-red-600', requires_oauth: false, is_active: true,
    supported_regions: ['DE', 'SK', 'CZ', 'PL', 'HR', 'RO', 'BG'],
    features: ['import', 'listing', 'order_sync', 'inventory_sync', 'price_sync', 'tracking'],
    credential_fields: [
      { key: 'client_key', label: 'Client Key', type: 'text', required: true,
        hint: 'Kaufland Seller Centre → Settings → API → Client Key' },
      { key: 'secret_key', label: 'Secret Key', type: 'password', required: true,
        hint: 'Kaufland Seller Centre → Settings → API → Secret Key' },
    ],
  },
  {
    id: 'magento', name: 'magento', display_name: 'Magento 2', icon: 'ri-store-line',
    color: 'text-orange-500', requires_oauth: false, is_active: true,
    supported_regions: ['GLOBAL'],
    features: ['import', 'listing', 'order_sync', 'inventory_sync', 'price_sync', 'tracking'],
    credential_fields: [
      { key: 'store_url', label: 'Store URL', type: 'text', required: true,
        hint: 'Your Magento 2 store URL, e.g. https://mystore.com' },
      { key: 'integration_token', label: 'Integration Token', type: 'password', required: true,
        hint: 'Magento Admin → System → Integrations → Create/Edit → Access Token' },
    ],
  },
  {
    id: 'bigcommerce', name: 'bigcommerce', display_name: 'BigCommerce', icon: 'ri-store-3-line',
    color: 'text-blue-600', requires_oauth: false, is_active: true,
    supported_regions: ['GLOBAL'],
    features: ['import', 'listing', 'order_sync', 'inventory_sync', 'price_sync', 'tracking'],
    credential_fields: [
      { key: 'store_hash', label: 'Store Hash', type: 'text', required: true,
        hint: 'Found in BigCommerce Admin → Advanced Settings → API Accounts (e.g. abc123xyz)' },
      { key: 'client_id', label: 'Client ID', type: 'text', required: false,
        hint: 'BigCommerce API Client ID (from your API account)' },
      { key: 'access_token', label: 'Access Token', type: 'password', required: true,
        hint: 'BigCommerce Admin → Advanced Settings → API Accounts → Create API Account → Access Token' },
    ],
  },
  {
    id: 'onbuy', name: 'onbuy', display_name: 'OnBuy', icon: 'ri-shopping-bag-3-line',
    color: 'text-orange-500', requires_oauth: false, is_active: true,
    supported_regions: ['GB'],
    features: ['import', 'listing', 'order_sync', 'inventory_sync', 'price_sync', 'tracking'],
    credential_fields: [
      { key: 'consumer_key', label: 'Consumer Key', type: 'text', required: true,
        hint: 'OnBuy Seller Centre → Integrations → API Credentials → Consumer Key' },
      { key: 'consumer_secret', label: 'Consumer Secret', type: 'password', required: true,
        hint: 'OnBuy Seller Centre → Integrations → API Credentials → Consumer Secret' },
      { key: 'site_id', label: 'Site ID', type: 'text', required: false,
        hint: '2000 = OnBuy UK (default). Leave blank for UK.' },
    ],
  },
  // ── Session 4 ─────────────────────────────────────────────────────────────
  {
    id: 'backmarket', name: 'backmarket', display_name: 'Back Market', icon: 'ri-recycle-fill',
    color: 'text-teal-600', requires_oauth: false, is_active: true,
    supported_regions: ['FR', 'DE', 'GB', 'US', 'ES', 'IT', 'BE', 'NL', 'AT', 'JP', 'AU'],
    features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync'],
    credential_fields: [
      { key: 'api_key', label: 'API Key', type: 'password', required: true,
        hint: 'Back Market Seller Dashboard → Settings → API → Generate Key' },
      { key: 'environment', label: 'Environment', type: 'select', required: false,
        options: ['production', 'sandbox'], hint: 'Use sandbox for testing.' },
    ],
  },
  {
    id: 'zalando', name: 'zalando', display_name: 'Zalando', icon: 'ri-shopping-bag-3-fill',
    color: 'text-orange-500', requires_oauth: false, is_active: true,
    supported_regions: ['DE', 'AT', 'CH', 'FR', 'IT', 'NL', 'PL', 'BE', 'GB', 'SE', 'DK', 'FI', 'NO'],
    features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync'],
    credential_fields: [
      { key: 'client_id', label: 'Client ID', type: 'text', required: true,
        hint: 'Zalando Partner Portal → Settings → API Credentials' },
      { key: 'client_secret', label: 'Client Secret', type: 'password', required: true },
    ],
  },
  {
    id: 'bol', name: 'bol', display_name: 'Bol.com', icon: 'ri-store-2-fill',
    color: 'text-blue-700', requires_oauth: false, is_active: true,
    supported_regions: ['NL', 'BE'],
    features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync'],
    credential_fields: [
      { key: 'client_id', label: 'Client ID', type: 'text', required: true,
        hint: 'Bol.com Retailer API → Settings → API Access → Client ID' },
      { key: 'client_secret', label: 'Client Secret', type: 'password', required: true },
    ],
  },
  {
    id: 'lazada', name: 'lazada', display_name: 'Lazada', icon: 'ri-store-fill',
    color: 'text-blue-500', requires_oauth: false, is_active: true,
    supported_regions: ['MY', 'SG', 'TH', 'ID', 'PH', 'VN'],
    features: ['listing', 'order_sync', 'tracking', 'inventory_sync', 'price_sync'],
    credential_fields: [
      { key: 'app_key', label: 'App Key', type: 'text', required: true,
        hint: 'Lazada Seller Center → Account → Open Platform → App Key' },
      { key: 'app_secret', label: 'App Secret', type: 'password', required: true },
      { key: 'access_token', label: 'Access Token', type: 'password', required: true,
        hint: 'Generate via Lazada OAuth flow in Seller Center' },
      { key: 'base_url', label: 'API Base URL', type: 'text', required: true,
        hint: 'e.g. https://api.lazada.com.my/rest (select your region)' },
    ],
  },
];

FALLBACK_ADAPTERS.sort((a, b) => a.display_name.localeCompare(b.display_name));

// Default base URLs for Temu regions (shown as placeholder/hint)
const TEMU_BASE_URLS: Record<string, string> = {
  'EU / UK': 'https://openapi-b-eu.temu.com/openapi/router',
  'US':      'https://openapi-b.temu.com/openapi/router',
};

const adapterColor: Record<string, string> = { amazon: '#FF9900', temu: '#FF6B35', ebay: '#E53238', shopify: '#96BF48', shopline: '#00b8d4', tesco: '#EE1C2E', amazon_vendor: '#E8A020', tiktok: '#010101', etsy: '#F1641E', woocommerce: '#7c3aed', shopwired: '#f97316', walmart: '#0071ce', kaufland: '#e5002b', magento: '#f97316', bigcommerce: '#1C4EBF', onbuy: '#E76119', backmarket: '#14B8A6', zalando: '#FF6600', bol: '#0E4299', lazada: '#F57224' };

// Real SVG brand logos — no external dependencies
function ChannelLogo({ id, size = 24, thumbnailUrl }: { id: string; size?: number; thumbnailUrl?: string }) {
  // Prefer a Firestore-supplied thumbnail image when available
  if (thumbnailUrl) {
    return (
      <img
        src={thumbnailUrl}
        alt={id}
        width={size}
        height={size}
        style={{ objectFit: 'contain', borderRadius: 4 }}
        onError={e => { (e.target as HTMLImageElement).style.display = 'none'; }}
      />
    );
  }
  const s = size;
  const logos: Record<string, JSX.Element> = {
    amazon: (
      <svg width={s} height={s} viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
        <path d="M13.3 14.6c-1.7 1.2-4.2 1.9-6.3 1.9-3 0-5.7-1.1-7.7-2.9-.2-.1 0-.3.2-.2 2.2 1.3 4.9 2 7.7 2 1.9 0 4-.4 5.9-1.2.3-.1.5.2.2.4z" fill="#FF9900"/>
        <path d="M14 13.8c-.2-.3-1.5-.1-2.1-.1-.2 0-.2-.1-.1-.3.4-.3 1.1-.8 2-.7.9.1 1 1 .2 1.1z" fill="#FF9900"/>
        <path d="M12.5 7.5C12.5 5 10.3 3 7.5 3S2.5 5 2.5 7.5c0 1.4.6 2.6 1.6 3.4H2v1h11v-1h-2.1c1-.8 1.6-2 1.6-3.4z" fill="#FF9900" opacity=".2"/>
        <text x="2" y="12" fontFamily="Arial Black,Arial" fontWeight="900" fontSize="10.5" fill="#FF9900">amazon</text>
      </svg>
    ),
    ebay: (
      <svg width={s} height={s} viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg">
        <text x="0" y="16" fontFamily="Arial Black,Arial" fontWeight="900" fontSize="10.5">
          <tspan fill="#E53238">e</tspan><tspan fill="#0064D2">b</tspan><tspan fill="#F5AF02">a</tspan><tspan fill="#86B817">y</tspan>
        </text>
      </svg>
    ),
    shopify: (
      <svg width={s} height={s} viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
        <path d="M15.3 3.2c0-.1-.1-.1-.2-.1-.1 0-1.6-.1-1.6-.1s-1.1-1-1.2-1.1c-.1-.1-.4-.1-.5 0L11 3c-.5-.1-1-.2-1.6-.2C7 2.8 5.3 4.7 4.8 7.4l-1.7.5c-.5.2-.5.2-.6.7L1.5 20.5 14 22.8l6.5-1.4-5.2-18.2zm-4 1.2l-.9.3c0-.3 0-.7-.1-1 .5.1.9.4 1 .7zm-1.7-.6c.1.3.1.7.1 1.1l-2.4.7C7.7 4 8.7 3.3 9.6 3.8zm1.2 13.4l-2.9-.7s.3-1.5 2.2-1.7c.5 0 .9.1 1.3.3l-.6 2.1zm1.3-1.7c-.5-.2-1-.3-1.6-.3-2.4.2-3.1 2.3-3.1 2.3L5 16.9 8 8.8l2.4-.7v7.6h.2l.5-1.2z" fill="#96BF48"/>
        <path d="M13.1 3.1l-1.4 4.5 1.4 8.6 6.5-1.4-6.5-11.7z" fill="#5E8E3E"/>
      </svg>
    ),
    temu: (
      <svg width={s} height={s} viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
        <rect width="24" height="24" rx="5" fill="#FC5B00"/>
        <text x="3" y="16" fontFamily="Arial Black,Arial" fontWeight="900" fontSize="10" fill="white">TEMU</text>
      </svg>
    ),
    tiktok: (
      <svg width={s} height={s} viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg">
        <path d="M19.6 6.9a4.7 4.7 0 0 1-4.7-4.7h-3.1v13.3a2.7 2.7 0 0 1-2.7 2.6 2.7 2.7 0 0 1-2.7-2.6 2.7 2.7 0 0 1 2.7-2.7c.3 0 .5 0 .8.1V9.7c-.3 0-.5-.1-.8-.1a5.8 5.8 0 0 0-5.8 5.8 5.8 5.8 0 0 0 5.8 5.8 5.8 5.8 0 0 0 5.8-5.8V9c1.2.8 2.7 1.3 4.3 1.3V7.2c-.5 0-1-.1-1.6-.3z" fill="#010101"/>
        <path d="M19.6 6.9a4.7 4.7 0 0 1-4.7-4.7h-3.1v13.3a2.7 2.7 0 0 1-2.7 2.6 2.7 2.7 0 0 1-2.7-2.6 2.7 2.7 0 0 1 2.7-2.7c.3 0 .5 0 .8.1V9.7c-.3 0-.5-.1-.8-.1a5.8 5.8 0 0 0-5.8 5.8 5.8 5.8 0 0 0 5.8 5.8 5.8 5.8 0 0 0 5.8-5.8V9c1.2.8 2.7 1.3 4.3 1.3V7.2c-.5 0-1-.1-1.6-.3z" fill="#EE1D52" opacity=".5" transform="translate(1 1)"/>
        <path d="M19.6 6.9a4.7 4.7 0 0 1-4.7-4.7h-3.1v13.3a2.7 2.7 0 0 1-2.7 2.6 2.7 2.7 0 0 1-2.7-2.6 2.7 2.7 0 0 1 2.7-2.7c.3 0 .5 0 .8.1V9.7c-.3 0-.5-.1-.8-.1a5.8 5.8 0 0 0-5.8 5.8 5.8 5.8 0 0 0 5.8 5.8 5.8 5.8 0 0 0 5.8-5.8V9c1.2.8 2.7 1.3 4.3 1.3V7.2c-.5 0-1-.1-1.6-.3z" fill="#69C9D0" opacity=".5" transform="translate(-1 -1)"/>
      </svg>
    ),
    etsy: (
      <svg width={s} height={s} viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg">
        <path d="M10.3 2C5.8 2 2 5.8 2 10.3s3.8 8.3 8.3 8.3 8.3-3.8 8.3-8.3S14.8 2 10.3 2zm3.4 11.7H8.6V8.6h.8v4.3h1.7V8.6h.8v4.3h1.8v.8zm-3.4-7.6c-1.9 0-3.4 1.5-3.4 3.4s1.5 3.4 3.4 3.4 3.4-1.5 3.4-3.4-1.5-3.4-3.4-3.4z" fill="#F1641E"/>
        <text x="3" y="15" fontFamily="Georgia,serif" fontWeight="bold" fontSize="9" fill="#F1641E">etsy</text>
      </svg>
    ),
    woocommerce: (
      <svg width={s} height={s} viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
        <path d="M2 5a3 3 0 0 1 3-3h14a3 3 0 0 1 3 3v9a3 3 0 0 1-3 3H14l-3 4-3-4H5a3 3 0 0 1-3-3V5z" fill="#7F54B3"/>
        <path d="M5.5 8h.8l1.2 4.5L8.7 8h.8l1.2 4.5L11.9 8h.8L11 13H10L8.7 8.8 7.4 13h-1L5.5 8zm7.5 0h3v.7h-2.3v1.5h2v.7h-2v1.4H16V13h-3V8z" fill="white"/>
      </svg>
    ),
    walmart: (
      <svg width={s} height={s} viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg">
        <path d="M12 2.5l.9 3.9.9-3.9.8 3.9.8-3.9V6l-1.6 5.3L12 6.4l-1.8 4.9L8.6 6v-.5l.8 3.9.8-3.9.9 3.9L12 2.5zM2.5 12l3.9.9-3.9.9 3.9.8-3.9.8H6l5.3-1.6L6.4 12l4.9-1.8L6 8.6h-.5l3.9.8-3.9.8 3.9.9L2.5 12zm19 0l-3.9.9 3.9.9-3.9.8 3.9.8H18l-5.3-1.6 4.9-1.3-4.9-1.8 5.3-1.6h.5l-3.9.8 3.9.8-3.9.9L21.5 12zM12 21.5l-.9-3.9-.9 3.9-.8-3.9-.8 3.9V18l1.6-5.3 1.3 4.9 1.8-4.9 1.6 5.3v.5l-.8-3.9-.8 3.9-.9-3.9L12 21.5z" fill="#0071CE"/>
      </svg>
    ),
    shopify_like: null,
    kaufland: (
      <svg width={s} height={s} viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
        <rect width="24" height="24" rx="4" fill="#E2001A"/>
        <text x="2.5" y="15.5" fontFamily="Arial Black,Arial" fontWeight="900" fontSize="8.5" fill="white">KAUF</text>
        <text x="2" y="21" fontFamily="Arial Black,Arial" fontWeight="900" fontSize="7.5" fill="white">LAND</text>
      </svg>
    ),
    magento: (
      <svg width={s} height={s} viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg">
        <path d="M12 2L2 7v10l10 5 10-5V7L12 2zm0 2.3l7.7 3.9v7.6L12 19.7l-7.7-3.9V8.2L12 4.3zM12 6L7 8.5V15l5 2.5 5-2.5V8.5L12 6zm0 2l3 1.5V15l-3 1.5L9 15v-5.5L12 8z" fill="#F97316"/>
      </svg>
    ),
    bigcommerce: (
      <svg width={s} height={s} viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
        <path d="M3 4h18v16H3z" fill="#1C4EBF" rx="2"/>
        <text x="3.5" y="13" fontFamily="Arial Black,Arial" fontWeight="900" fontSize="7" fill="white">BIG</text>
        <text x="2" y="18.5" fontFamily="Arial Black,Arial" fontWeight="700" fontSize="5.5" fill="white">COMMERCE</text>
      </svg>
    ),
    onbuy: (
      <svg width={s} height={s} viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
        <rect width="24" height="24" rx="5" fill="#E76119"/>
        <text x="3" y="15.5" fontFamily="Arial Black,Arial" fontWeight="900" fontSize="10.5" fill="white">OnBuy</text>
      </svg>
    ),
    backmarket: (
      <svg width={s} height={s} viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
        <rect width="24" height="24" rx="5" fill="#14B8A6"/>
        <path d="M12 5a7 7 0 1 0 0 14A7 7 0 0 0 12 5zm0 2a5 5 0 1 1 0 10A5 5 0 0 1 12 7zm-1 2v4l3.5 2-.7 1.1L9 13.5V9h2z" fill="white"/>
      </svg>
    ),
    zalando: (
      <svg width={s} height={s} viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
        <rect width="24" height="24" rx="4" fill="#FF6600"/>
        <text x="2.5" y="16" fontFamily="Arial Black,Arial" fontWeight="900" fontSize="9.5" fill="white">ZALAN</text>
        <text x="5.5" y="22" fontFamily="Arial Black,Arial" fontWeight="900" fontSize="8.5" fill="white">DO</text>
      </svg>
    ),
    bol: (
      <svg width={s} height={s} viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
        <rect width="24" height="24" rx="5" fill="#0E4299"/>
        <text x="4" y="16" fontFamily="Arial Black,Arial" fontWeight="900" fontSize="12" fill="white">bol</text>
      </svg>
    ),
    lazada: (
      <svg width={s} height={s} viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
        <rect width="24" height="24" rx="5" fill="#F57224"/>
        <text x="1.5" y="16" fontFamily="Arial Black,Arial" fontWeight="900" fontSize="10.5" fill="white">lazada</text>
      </svg>
    ),
    tesco: (
      <svg width={s} height={s} viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
        <rect width="24" height="24" rx="4" fill="#00539F"/>
        <text x="1.5" y="16" fontFamily="Arial Black,Arial" fontWeight="900" fontSize="11" fill="white">TESCO</text>
      </svg>
    ),
    amazon_vendor: (
      <svg width={s} height={s} viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
        <rect width="24" height="24" rx="4" fill="#232F3E"/>
        <text x="2" y="11" fontFamily="Arial Black,Arial" fontWeight="900" fontSize="7.5" fill="#FF9900">amazon</text>
        <text x="2" y="19" fontFamily="Arial,sans-serif" fontWeight="600" fontSize="7" fill="white">VENDOR</text>
      </svg>
    ),
  };
  const el = logos[id];
  if (el) return el;
  // Fallback: colored square with first 2 letters
  const color = ({ amazon:'#FF9900',temu:'#FF6B35',ebay:'#E53238',shopify:'#96BF48',shopline:'#00b8d4',tesco:'#00539F',amazon_vendor:'#232F3E',tiktok:'#010101',etsy:'#F1641E',woocommerce:'#7c3aed',walmart:'#0071ce',kaufland:'#e5002b',magento:'#f97316',bigcommerce:'#1C4EBF',onbuy:'#E76119',backmarket:'#14B8A6',zalando:'#FF6600',bol:'#0E4299',lazada:'#F57224' } as Record<string,string>)[id] || '#666';
  const initials = id.slice(0,2).toUpperCase();
  return (
    <svg width={s} height={s} viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg">
      <rect width="24" height="24" rx="5" fill={color}/>
      <text x="12" y="16" textAnchor="middle" fontFamily="Arial Black,Arial" fontWeight="900" fontSize="9" fill="white">{initials}</text>
    </svg>
  );
}

function formatDate(d?: string): string {
  if (!d) return '—';
  return new Date(d).toLocaleDateString('en-GB', { day: '2-digit', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit' });
}

export default function MarketplaceConnections() {
  const [adapters, setAdapters] = useState<AdapterInfo[]>([]);
  const [credentials, setCredentials] = useState<MarketplaceCredential[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Quick Connect: channels selected during setup wizard
  const [quickConnectChannels, setQuickConnectChannels] = useState<string[]>([]);
  const [quickConnectDismissed, setQuickConnectDismissed] = useState(false);

  // Task 1: Filter, search, sync status
  const [filterTab, setFilterTab] = useState<'all' | 'direct' | 'third_party'>('all');
  const [searchQuery, setSearchQuery] = useState('');
  const [channelSyncStatus, setChannelSyncStatus] = useState<Record<string, { orders: { state: string; error_msg?: string }; inventory: { state: string; error_msg?: string }; listings: { state: string; error_msg?: string } }>>({});
  const [togglingId, setTogglingId] = useState<string | null>(null);

  const [modalOpen, setModalOpen] = useState(false);
  const [selectedAdapter, setSelectedAdapter] = useState<AdapterInfo | null>(null);
  const [accountName, setAccountName] = useState('');
  const [environment, setEnvironment] = useState<'production' | 'sandbox'>('production');
  const [formValues, setFormValues] = useState<Record<string, string>>({});
  const [saving, setSaving] = useState(false);
  const [saveResult, setSaveResult] = useState<'success' | 'error' | null>(null);
  const navigate = useNavigate();
  const [saveError, setSaveError] = useState('');

  const [testingId, setTestingId] = useState<string | null>(null);
  const [reconnectingCredId, setReconnectingCredId] = useState<string | null>(null);
  const [testResult, setTestResult] = useState<Record<string, 'success' | 'error'>>({});
  const [ebayManualEntry, setEbayManualEntry] = useState(true);

  // ── Configure panel state ──
  const [configOpen, setConfigOpen] = useState(false);
  const [configuringCred, setConfiguringCred] = useState<MarketplaceCredential | null>(null);
  const [configTab, setConfigTab] = useState<'orders' | 'stock' | 'shipping' | 'products'>('orders');
  const [configData, setConfigData] = useState<any>(null);
  const [configLoading, setConfigLoading] = useState(false);
  const [configSaving, setConfigSaving] = useState(false);
  const [configSaved, setConfigSaved] = useState(false);
  const [importingNow, setImportingNow] = useState(false);
  const [importNowResult, setImportNowResult] = useState<string | null>(null);

  useEffect(() => { loadData(); }, []);

  async function loadData() {
    setLoading(true);
    setError(null);
    const API_BASE = (import.meta as any).env?.VITE_API_URL || 'http://localhost:8080/api/v1';
    const tenantId = getActiveTenantId();
    try {
      // Primary source: Firestore-backed registry (is_active, images, sort order)
      try {
        const registryRes = await fetch(`${API_BASE}/marketplace/registry`, {
          headers: { 'X-Tenant-Id': tenantId },
        });
        if (registryRes.ok) {
          const registryData = await registryRes.json();
          const registryAdapters = (registryData.data || []) as AdapterInfo[];
          if (registryAdapters.length > 0) {
            // Firestore is authoritative. Only fill in credential_fields from
            // FALLBACK_ADAPTERS when Firestore hasn't stored them yet.
            // Never override is_active — that's the whole point of this system.
            const merged = registryAdapters.map((ra: AdapterInfo) => {
              if (!ra.credential_fields || ra.credential_fields.length === 0) {
                const fallback = FALLBACK_ADAPTERS.find(f => f.id === ra.id);
                return { ...ra, credential_fields: fallback?.credential_fields || [] };
              }
              return ra;
            });
            // Add any FALLBACK entries that Firestore doesn't know about yet,
            // but only if they're not already present — and mark them active by
            // default since Firestore has no opinion on them yet.
            // DO NOT add entries that Firestore explicitly set is_active: false.
            const registryIds = new Set(merged.map((m: AdapterInfo) => m.id));
            for (const fb of FALLBACK_ADAPTERS) {
              if (!registryIds.has(fb.id)) {
                merged.push(fb); // genuinely unknown to Firestore — show with fallback default
              }
            }
            setAdapters(merged);
          } else {
            setAdapters(FALLBACK_ADAPTERS);
          }
        } else {
          setAdapters(FALLBACK_ADAPTERS);
        }
      } catch {
        // Fall back to legacy adapter list, then hardcoded fallbacks
        try {
          const adapterRes = await adapterService.list();
          if (adapterRes.data?.data && adapterRes.data.data.length > 0) {
            const backendAdapters = adapterRes.data.data as AdapterInfo[];
            const merged = backendAdapters.map((ba: AdapterInfo) => {
              const fallback = FALLBACK_ADAPTERS.find(f => f.id === ba.id);
              return { ...ba, credential_fields: ba.credential_fields || fallback?.credential_fields || [] };
            });
            for (const fb of FALLBACK_ADAPTERS) {
              if (!merged.find((m: AdapterInfo) => m.id === fb.id)) merged.push(fb);
            }
            setAdapters(merged);
          } else {
            setAdapters(FALLBACK_ADAPTERS);
          }
        } catch { setAdapters(FALLBACK_ADAPTERS); }
      }

      try {
        const credRes = await credentialService.list();
        setCredentials(credRes.data?.data || []);
      } catch { setCredentials([]); }

      // Fetch per-channel sync status
      try {
        const syncRes = await fetch(`${API_BASE}/sync/channel-status`, { headers: { 'X-Tenant-Id': tenantId } });
        const syncData = await syncRes.json();
        const map: typeof channelSyncStatus = {};
        for (const item of (syncData.data || [])) {
          map[item.credential_id] = { orders: item.orders, inventory: item.inventory, listings: item.listings };
        }
        setChannelSyncStatus(map);
      } catch { /* ignore */ }
    } catch (err: any) { setError(err.message); }
    finally { setLoading(false); }

    // Load quick-connect channels from setup wizard
    try {
      const qcRes = await fetch(`${API_BASE}/settings/selected-channels`, { headers: { 'X-Tenant-Id': tenantId } });
      if (qcRes.ok) {
        const qcData = await qcRes.json();
        setQuickConnectChannels(qcData.channels || []);
      }
    } catch { /* ignore */ }
  }

  async function openConfigure(cred: MarketplaceCredential) {
    setConfiguringCred(cred);
    setConfigTab('orders');
    setConfigSaved(false);
    setImportNowResult(null);
    setConfigData(null);
    setConfigOpen(true);
    setConfigLoading(true);
    try {
      const res = await fetch(`${(import.meta as any).env?.VITE_API_URL || 'http://localhost:8080/api/v1'}/marketplace/credentials/${cred.credential_id}/config`, {
        headers: { 'X-Tenant-Id': getActiveTenantId() },
      });
      const data = await res.json();
      setConfigData(data.data || defaultConfig());
    } catch {
      setConfigData(defaultConfig());
    } finally {
      setConfigLoading(false);
    }
  }

  function defaultConfig() {
    return {
      orders: { enabled: false, frequency_minutes: 30, include_fba: false, status_filter: 'Unshipped', lookback_hours: 24 },
      stock: { reserve_pending: false },
      shipping: { use_amazon_buy_shipping: false, label_format: 'PDF', seller_fulfilled_prime: false },
      products: { import_enabled: true },
    };
  }

  async function saveConfig() {
    if (!configuringCred || !configData) return;
    setConfigSaving(true);
    try {
      await fetch(`${(import.meta as any).env?.VITE_API_URL || 'http://localhost:8080/api/v1'}/marketplace/credentials/${configuringCred.credential_id}/config`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId() },
        body: JSON.stringify(configData),
      });
      setConfigSaved(true);
      setTimeout(() => setConfigSaved(false), 3000);
    } catch (e: any) {
      alert('Failed to save config: ' + e.message);
    } finally {
      setConfigSaving(false);
    }
  }

  async function handleImportNow() {
    if (!configuringCred) return;
    setImportingNow(true);
    setImportNowResult(null);
    try {
      const res = await fetch(`${(import.meta as any).env?.VITE_API_URL || 'http://localhost:8080/api/v1'}/orders/import/now`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId() },
        body: JSON.stringify({
          credential_id: configuringCred.credential_id,
          channel: configuringCred.channel,
          lookback_hours: configData?.orders?.lookback_hours || 24,
        }),
      });
      const data = await res.json();
      setImportNowResult(data.job_id ? `✅ Import started (Job: ${data.job_id.slice(0, 8)}...)` : '❌ Failed to start import');
    } catch (e: any) {
      setImportNowResult('❌ ' + e.message);
    } finally {
      setImportingNow(false);
    }
  }

  function openConnectModal(adapter: AdapterInfo) {
    setSelectedAdapter(adapter);
    setAccountName('');
    // Temu and eBay have no sandbox — default to production
    setEnvironment((adapter.id === 'temu' || adapter.id === 'ebay' || adapter.id === 'amazon') ? 'production' : 'sandbox');
    setFormValues({});
    setSaveResult(null);
    setSaveError('');
    setEbayManualEntry(true);
    setModalOpen(true);
  }

  function closeModal() { setModalOpen(false); setSelectedAdapter(null); setReconnectingCredId(null); }

  async function handleSave() {
    if (!selectedAdapter) return;

    // eBay uses OAuth popup flow only if user chose OAuth mode
    if (selectedAdapter.id === 'ebay' && !ebayManualEntry) {
      await handleEbayOAuth();
      return;
    }

    // TikTok always uses OAuth popup flow
    if (selectedAdapter.id === 'tiktok') {
      await handleTikTokOAuth();
      return;
    }

    // Etsy uses OAuth PKCE popup flow
    if (selectedAdapter.id === 'etsy') {
      await handleEtsyOAuth();
      return;
    }

    // AmazonNew uses OAuth popup flow
    if (selectedAdapter.id === 'amazon') {
      await handleAmazonOAuth();
      return;
    }

    // Shopify uses OAuth popup flow
    if (selectedAdapter.id === 'shopify') {
      await handleShopifyOAuth();
      return;
    }

    // Shopline uses OAuth popup flow
    if (selectedAdapter.id === 'shopline') {
      await handleShoplineOAuth();
      return;
    }

    // WooCommerce: test connection first via our /woocommerce/connect endpoint
    if (selectedAdapter.id === 'woocommerce') {
      await handleWooCommerceConnect();
      return;
    }

    // ShopWired: test and save via /shopwired/credentials
    if (selectedAdapter.id === 'shopwired') {
      await handleShopWiredConnect();
      return;
    }

    // Walmart: test connection first via our /walmart/connect endpoint
    if (selectedAdapter.id === 'walmart') {
      await handleWalmartConnect();
      return;
    }

    // Kaufland: test connection first via our /kaufland/connect endpoint
    if (selectedAdapter.id === 'kaufland') {
      await handleKauflandConnect();
      return;
    }

    // Magento: test connection first via our /magento/connect endpoint
    if (selectedAdapter.id === 'magento') {
      await handleMagentoConnect();
      return;
    }

    // BigCommerce: test connection first via our /bigcommerce/connect endpoint
    if (selectedAdapter.id === 'bigcommerce') {
      await handleBigCommerceConnect();
      return;
    }

    // OnBuy: test connection first via our /onbuy/connect endpoint
    if (selectedAdapter.id === 'onbuy') {
      await handleOnBuyConnect();
      return;
    }

    setSaving(true); setSaveResult(null); setSaveError('');
    try {
      const payload: ConnectMarketplaceRequest = {
        channel: selectedAdapter.id,
        account_name: accountName || `${selectedAdapter.display_name} Account`,
        environment: environment,
        credentials: formValues,
      };
      if (formValues.marketplace_id) {
        (payload as any).marketplace_id = formValues.marketplace_id;
      }
      // If reconnecting an existing credential, update in place (no duplicate created)
      if (reconnectingCredId) {
        const res = await credentialService.reconnect(reconnectingCredId, payload);
        if (!res.data?.connected) {
          setSaveResult('error');
          setSaveError(res.data?.error || 'Reconnection failed — check your credentials');
          return;
        }
      } else {
        await credentialService.create(payload);
      }
      setSaveResult('success');
      const credRes = await credentialService.list();
      setCredentials(credRes.data?.data || []);
      setTimeout(() => closeModal(), 1200);
    } catch (err: any) {
      setSaveResult('error');
      setSaveError(err.response?.data?.details || err.response?.data?.error || err.message || 'Connection failed');
    } finally { setSaving(false); }
  }

  async function handleAmazonOAuth() {
    setSaving(true); setSaveResult(null); setSaveError('');
    const marketplaceId = formValues.marketplace_id || 'A1F83G8C2ARO7P';
    try {
      const API_BASE_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';
      const name = accountName || 'Amazon Account';
      const res = await fetch(
        `${API_BASE_URL}/amazon/oauth/login?marketplace_id=${encodeURIComponent(marketplaceId)}&account_name=${encodeURIComponent(name)}`,
        { headers: { 'X-Tenant-Id': getActiveTenantId() } }
      );
      const data = await res.json();
      if (!data.ok || !data.consent_url) {
        setSaveResult('error');
        setSaveError(data.error || 'Failed to get Amazon consent URL');
        setSaving(false);
        return;
      }

      const popup = window.open(data.consent_url, 'amazon-oauth', 'width=700,height=800,left=200,top=80');
      if (!popup) {
        setSaveResult('error');
        setSaveError('Popup was blocked. Please allow popups for this site and try again.');
        setSaving(false);
        return;
      }

      const handler = (event: MessageEvent) => {
        if (event.data?.type === 'amazon-oauth-success') {
          window.removeEventListener('message', handler);
          setSaveResult('success');
          setSaving(false);
          credentialService.list().then(credRes => {
            setCredentials(credRes.data?.data || []);
          });
          setTimeout(() => closeModal(), 1500);
        } else if (event.data?.type === 'amazon-oauth-error') {
          window.removeEventListener('message', handler);
          setSaveResult('error');
          setSaveError(event.data.error || 'Amazon authorisation failed');
          setSaving(false);
        }
      };
      window.addEventListener('message', handler);

      const pollInterval = setInterval(() => {
        try {
          const result = localStorage.getItem('amazon-oauth-result');
          if (result) {
            const parsed = JSON.parse(result);
            if (parsed.type === 'amazon-oauth-success' && Date.now() - parsed.ts < 30000) {
              localStorage.removeItem('amazon-oauth-result');
              clearInterval(pollInterval);
              window.removeEventListener('message', handler);
              setSaveResult('success');
              setSaving(false);
              credentialService.list().then(credRes => {
                setCredentials(credRes.data?.data || []);
              });
              setTimeout(() => closeModal(), 1500);
              return;
            }
          }
        } catch(e) {}
        if (popup.closed) {
          clearInterval(pollInterval);
          window.removeEventListener('message', handler);
          credentialService.list().then(credRes => {
            const creds = credRes.data?.data || [];
            setCredentials(creds);
            if (creds.some((c: any) => c.channel === 'amazon')) setSaveResult('success');
          });
          setSaving(false);
        }
      }, 500);

      setTimeout(() => {
        clearInterval(pollInterval);
        window.removeEventListener('message', handler);
        setSaving(false);
      }, 120000);
    } catch (err: any) {
      setSaveResult('error');
      setSaveError(err.message || 'Failed to start Amazon OAuth');
      setSaving(false);
    }
  }

  async function handleShopifyOAuth() {
    setSaving(true); setSaveResult(null); setSaveError('');
    const shopDomain = formValues.shop_domain || formValues.shop || '';
    if (!shopDomain) {
      setSaveResult('error');
      setSaveError('Please enter your Shopify store domain (e.g. mystore.myshopify.com)');
      setSaving(false);
      return;
    }
    try {
      const API_BASE_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';
      const name = accountName || shopDomain;
      const res = await fetch(
        `${API_BASE_URL}/shopify/oauth/login?shop=${encodeURIComponent(shopDomain)}&account_name=${encodeURIComponent(name)}`,
        { headers: { 'X-Tenant-Id': getActiveTenantId() } }
      );
      const data = await res.json();
      if (!data.ok || !data.consent_url) {
        setSaveResult('error');
        setSaveError(data.error || 'Failed to get Shopify consent URL');
        setSaving(false);
        return;
      }

      const popup = window.open(data.consent_url, 'shopify-oauth', 'width=600,height=700,left=200,top=100');
      if (!popup) {
        setSaveResult('error');
        setSaveError('Popup was blocked. Please allow popups for this site and try again.');
        setSaving(false);
        return;
      }

      const handler = (event: MessageEvent) => {
        if (event.data?.type === 'shopify-oauth-success') {
          window.removeEventListener('message', handler);
          setSaveResult('success');
          setSaving(false);
          credentialService.list().then(credRes => {
            setCredentials(credRes.data?.data || []);
          });
          setTimeout(() => closeModal(), 1500);
        } else if (event.data?.type === 'shopify-oauth-error') {
          window.removeEventListener('message', handler);
          setSaveResult('error');
          setSaveError(event.data.error || 'Shopify authorisation failed');
          setSaving(false);
        }
      };
      window.addEventListener('message', handler);

      const pollInterval = setInterval(() => {
        if (popup.closed) {
          clearInterval(pollInterval);
          window.removeEventListener('message', handler);
          setSaving(false);
          credentialService.list().then(credRes => {
            setCredentials(credRes.data?.data || []);
          });
        }
      }, 1000);

      setTimeout(() => {
        clearInterval(pollInterval);
        window.removeEventListener('message', handler);
        setSaving(false);
      }, 120000);
    } catch (err: any) {
      setSaveResult('error');
      setSaveError(err.message || 'Failed to start Shopify OAuth');
      setSaving(false);
    }
  }

  async function handleShoplineOAuth() {
    setSaving(true); setSaveResult(null); setSaveError('');
    const shopID = formValues.shop_id || formValues.shop || '';
    if (!shopID) {
      setSaveResult('error');
      setSaveError('Please enter your Shopline store ID (e.g. mystore)');
      setSaving(false);
      return;
    }
    try {
      const API_BASE_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';
      const name = accountName || shopID;
      const res = await fetch(
        `${API_BASE_URL}/shopline/oauth/login?shop=${encodeURIComponent(shopID)}&account_name=${encodeURIComponent(name)}`,
        { headers: { 'X-Tenant-Id': getActiveTenantId() } }
      );
      const data = await res.json();
      if (!data.ok || !data.consent_url) {
        setSaveResult('error');
        setSaveError(data.error || 'Failed to get Shopline consent URL');
        setSaving(false);
        return;
      }

      const popup = window.open(data.consent_url, 'shopline-oauth', 'width=600,height=700,left=200,top=100');
      if (!popup) {
        setSaveResult('error');
        setSaveError('Popup was blocked. Please allow popups for this site and try again.');
        setSaving(false);
        return;
      }

      const handler = (event: MessageEvent) => {
        if (event.data?.type === 'shopline-oauth-success') {
          window.removeEventListener('message', handler);
          setSaveResult('success');
          setSaving(false);
          credentialService.list().then(credRes => {
            setCredentials(credRes.data?.data || []);
          });
          setTimeout(() => closeModal(), 1500);
        } else if (event.data?.type === 'shopline-oauth-error') {
          window.removeEventListener('message', handler);
          setSaveResult('error');
          setSaveError(event.data.error || 'Shopline authorisation failed');
          setSaving(false);
        }
      };
      window.addEventListener('message', handler);

      const pollInterval = setInterval(() => {
        if (popup.closed) {
          clearInterval(pollInterval);
          window.removeEventListener('message', handler);
          setSaving(false);
          credentialService.list().then(credRes => {
            setCredentials(credRes.data?.data || []);
          });
        }
      }, 1000);

      setTimeout(() => {
        clearInterval(pollInterval);
        window.removeEventListener('message', handler);
        setSaving(false);
      }, 120000);
    } catch (err: any) {
      setSaveResult('error');
      setSaveError(err.message || 'Failed to start Shopline OAuth');
      setSaving(false);
    }
  }

  async function handleTikTokOAuth() {
    setSaving(true); setSaveResult(null); setSaveError('');
    try {
      const API_BASE_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';
      const name = accountName || 'TikTok Shop';
      const res = await fetch(
        `${API_BASE_URL}/tiktok/oauth/login?account_name=${encodeURIComponent(name)}`,
        { headers: { 'X-Tenant-Id': getActiveTenantId() } }
      );
      const data = await res.json();
      if (!data.ok || !data.consent_url) {
        setSaveResult('error');
        setSaveError(data.error || 'Failed to get TikTok consent URL');
        setSaving(false);
        return;
      }

      const popup = window.open(data.consent_url, 'tiktok-oauth', 'width=600,height=700,left=200,top=100');
      if (!popup) {
        setSaveResult('error');
        setSaveError('Popup was blocked. Please allow popups for this site and try again.');
        setSaving(false);
        return;
      }

      const handler = (event: MessageEvent) => {
        if (event.data?.type === 'tiktok-oauth-success') {
          window.removeEventListener('message', handler);
          setSaveResult('success');
          setSaving(false);
          credentialService.list().then(credRes => {
            setCredentials(credRes.data?.data || []);
          });
          setTimeout(() => closeModal(), 1500);
        } else if (event.data?.type === 'tiktok-oauth-error') {
          window.removeEventListener('message', handler);
          setSaveResult('error');
          setSaveError(event.data.error || 'TikTok authorization failed');
          setSaving(false);
        }
      };
      window.addEventListener('message', handler);

      const pollInterval = setInterval(() => {
        if (popup.closed) {
          clearInterval(pollInterval);
          window.removeEventListener('message', handler);
          setSaving(false);
          credentialService.list().then(credRes => {
            setCredentials(credRes.data?.data || []);
          });
        }
      }, 1000);

      setTimeout(() => {
        clearInterval(pollInterval);
        window.removeEventListener('message', handler);
        setSaving(false);
      }, 120000);
    } catch (err: any) {
      setSaveResult('error');
      setSaveError(err.message || 'Failed to start TikTok OAuth');
      setSaving(false);
    }
  }

  async function handleEtsyOAuth() {
    setSaving(true); setSaveResult(null); setSaveError('');
    try {
      const API_BASE_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';
      const name = accountName || 'Etsy Shop';
      const res = await fetch(
        `${API_BASE_URL}/etsy/oauth/login?account_name=${encodeURIComponent(name)}`,
        { headers: { 'X-Tenant-Id': getActiveTenantId() } }
      );
      const data = await res.json();
      if (!data.ok || !data.consent_url) {
        setSaveResult('error');
        setSaveError(data.error || 'Failed to get Etsy consent URL');
        setSaving(false);
        return;
      }

      const popup = window.open(data.consent_url, 'etsy-oauth', 'width=600,height=700,left=200,top=100');
      if (!popup) {
        setSaveResult('error');
        setSaveError('Popup was blocked. Please allow popups for this site and try again.');
        setSaving(false);
        return;
      }

      const handler = (event: MessageEvent) => {
        if (event.data?.type === 'etsy-oauth-success') {
          window.removeEventListener('message', handler);
          setSaveResult('success');
          setSaving(false);
          credentialService.list().then(credRes => {
            setCredentials(credRes.data?.data || []);
          });
          setTimeout(() => closeModal(), 1500);
        } else if (event.data?.type === 'etsy-oauth-error') {
          window.removeEventListener('message', handler);
          setSaveResult('error');
          setSaveError(event.data.error || 'Etsy authorization failed');
          setSaving(false);
        }
      };
      window.addEventListener('message', handler);

      const pollInterval = setInterval(() => {
        if (popup.closed) {
          clearInterval(pollInterval);
          window.removeEventListener('message', handler);
          setSaving(false);
          credentialService.list().then(credRes => {
            setCredentials(credRes.data?.data || []);
          });
        }
      }, 1000);

      setTimeout(() => {
        clearInterval(pollInterval);
        window.removeEventListener('message', handler);
        setSaving(false);
      }, 120000);
    } catch (err: any) {
      setSaveResult('error');
      setSaveError(err.message || 'Failed to start Etsy OAuth');
      setSaving(false);
    }
  }

  async function handleWooCommerceConnect() {
    setSaving(true); setSaveResult(null); setSaveError('');
    try {
      const API_BASE_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';
      const name = accountName || formValues.store_url || 'WooCommerce Store';
      const res = await fetch(`${API_BASE_URL}/woocommerce/connect`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-Tenant-Id': getActiveTenantId(),
        },
        body: JSON.stringify({
          account_name: name,
          store_url: formValues.store_url,
          consumer_key: formValues.consumer_key,
          consumer_secret: formValues.consumer_secret,
        }),
      });
      const data = await res.json();
      if (!data.ok) {
        setSaveResult('error');
        setSaveError(data.error || 'Failed to connect WooCommerce store');
        setSaving(false);
        return;
      }
      setSaveResult('success');
      const credRes = await credentialService.list();
      setCredentials(credRes.data?.data || []);
      setTimeout(() => closeModal(), 1500);
    } catch (err: any) {
      setSaveResult('error');
      setSaveError(err.message || 'Failed to connect WooCommerce');
    } finally {
      setSaving(false);
    }
  }

  async function handleShopWiredConnect() {
    setSaving(true); setSaveResult(null); setSaveError('');
    try {
      const API_BASE_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';
      const name = accountName || formValues.store_name || 'ShopWired Store';
      const res = await fetch(`${API_BASE_URL}/shopwired/credentials`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-Tenant-Id': getActiveTenantId(),
        },
        body: JSON.stringify({
          api_key: formValues.api_key,
          api_secret: formValues.api_secret,
          store_name: name,
        }),
      });
      const data = await res.json();
      if (!data.ok) {
        setSaveResult('error');
        setSaveError(data.error || 'Failed to connect ShopWired store');
        setSaving(false);
        return;
      }
      setSaveResult('success');
      const credRes = await credentialService.list();
      setCredentials(credRes.data?.data || []);
      setTimeout(() => closeModal(), 1500);
    } catch (err: any) {
      setSaveResult('error');
      setSaveError(err.message || 'Failed to connect ShopWired');
    } finally {
      setSaving(false);
    }
  }

  async function handleWalmartConnect() {
    setSaving(true); setSaveResult(null); setSaveError('');
    try {
      const API_BASE_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';
      const name = accountName || 'Walmart Marketplace';
      const res = await fetch(`${API_BASE_URL}/walmart/connect`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-Tenant-Id': getActiveTenantId(),
        },
        body: JSON.stringify({
          account_name: name,
          client_id: formValues.client_id,
          client_secret: formValues.client_secret,
        }),
      });
      const data = await res.json();
      if (!data.ok) {
        setSaveResult('error');
        setSaveError(data.error || 'Failed to connect Walmart Marketplace');
        setSaving(false);
        return;
      }
      setSaveResult('success');
      const credRes = await credentialService.list();
      setCredentials(credRes.data?.data || []);
      setTimeout(() => closeModal(), 1500);
    } catch (err: any) {
      setSaveResult('error');
      setSaveError(err.message || 'Failed to connect Walmart');
    } finally {
      setSaving(false);
    }
  }

  async function handleKauflandConnect() {
    setSaving(true); setSaveResult(null); setSaveError('');
    try {
      const API_BASE_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';
      const name = accountName || 'Kaufland';
      const res = await fetch(`${API_BASE_URL}/kaufland/connect`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-Tenant-Id': getActiveTenantId(),
        },
        body: JSON.stringify({
          account_name: name,
          client_key: formValues.client_key,
          secret_key: formValues.secret_key,
        }),
      });
      const data = await res.json();
      if (!data.ok) {
        setSaveResult('error');
        setSaveError(data.error || 'Failed to connect Kaufland');
        setSaving(false);
        return;
      }
      setSaveResult('success');
      const credRes = await credentialService.list();
      setCredentials(credRes.data?.data || []);
      setTimeout(() => closeModal(), 1500);
    } catch (err: any) {
      setSaveResult('error');
      setSaveError(err.message || 'Failed to connect Kaufland');
    } finally {
      setSaving(false);
    }
  }

  async function handleMagentoConnect() {
    setSaving(true); setSaveResult(null); setSaveError('');
    try {
      const API_BASE_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';
      const name = accountName || formValues.store_url || 'Magento Store';
      const res = await fetch(`${API_BASE_URL}/magento/connect`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-Tenant-Id': getActiveTenantId(),
        },
        body: JSON.stringify({
          account_name: name,
          store_url: formValues.store_url,
          integration_token: formValues.integration_token,
        }),
      });
      const data = await res.json();
      if (!data.ok) {
        setSaveResult('error');
        setSaveError(data.error || 'Failed to connect Magento store');
        setSaving(false);
        return;
      }
      setSaveResult('success');
      const credRes = await credentialService.list();
      setCredentials(credRes.data?.data || []);
      setTimeout(() => closeModal(), 1500);
    } catch (err: any) {
      setSaveResult('error');
      setSaveError(err.message || 'Failed to connect Magento');
    } finally {
      setSaving(false);
    }
  }

  async function handleBigCommerceConnect() {
    setSaving(true); setSaveResult(null); setSaveError('');
    try {
      const API_BASE_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';
      const name = accountName || formValues.store_hash || 'BigCommerce Store';
      const res = await fetch(`${API_BASE_URL}/bigcommerce/connect`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-Tenant-Id': getActiveTenantId(),
        },
        body: JSON.stringify({
          account_name: name,
          store_hash: formValues.store_hash,
          client_id: formValues.client_id,
          access_token: formValues.access_token,
        }),
      });
      const data = await res.json();
      if (!data.ok) {
        setSaveResult('error');
        setSaveError(data.error || 'Failed to connect BigCommerce store');
        setSaving(false);
        return;
      }
      setSaveResult('success');
      const credRes = await credentialService.list();
      setCredentials(credRes.data?.data || []);
      setTimeout(() => closeModal(), 1500);
    } catch (err: any) {
      setSaveResult('error');
      setSaveError(err.message || 'Failed to connect BigCommerce');
    } finally {
      setSaving(false);
    }
  }

  async function handleOnBuyConnect() {
    setSaving(true); setSaveResult(null); setSaveError('');
    try {
      const API_BASE_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';
      const name = accountName || 'OnBuy Account';
      const res = await fetch(`${API_BASE_URL}/onbuy/connect`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-Tenant-Id': getActiveTenantId(),
        },
        body: JSON.stringify({
          account_name: name,
          consumer_key: formValues.consumer_key,
          consumer_secret: formValues.consumer_secret,
          site_id: formValues.site_id || '2000',
        }),
      });
      const data = await res.json();
      if (!data.ok) {
        setSaveResult('error');
        setSaveError(data.error || 'Failed to connect OnBuy account');
        setSaving(false);
        return;
      }
      setSaveResult('success');
      const credRes = await credentialService.list();
      setCredentials(credRes.data?.data || []);
      setTimeout(() => closeModal(), 1500);
    } catch (err: any) {
      setSaveResult('error');
      setSaveError(err.message || 'Failed to connect OnBuy');
    } finally {
      setSaving(false);
    }
  }

  async function handleEbayOAuth() {
    setSaving(true); setSaveResult(null); setSaveError('');
    try {
      const API_BASE_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';
      const name = accountName || 'eBay Account';
      const res = await fetch(
        `${API_BASE_URL}/ebay/oauth/login?account_name=${encodeURIComponent(name)}&environment=${environment}`,
        { headers: { 'X-Tenant-Id': getActiveTenantId() } }
      );
      const data = await res.json();
      if (!data.ok || !data.consent_url) {
        setSaveResult('error');
        setSaveError(data.error || 'Failed to get eBay consent URL');
        setSaving(false);
        return;
      }

      // Open eBay consent page in popup
      const popup = window.open(data.consent_url, 'ebay-oauth', 'width=600,height=700,left=200,top=100');

      if (!popup) {
        // Popup was blocked — fall back to same-window redirect
        setSaveResult('error');
        setSaveError('Popup was blocked by your browser. Please allow popups for this site and try again.');
        setSaving(false);
        return;
      }

      // Listen for success/error message from the callback page
      const handler = (event: MessageEvent) => {
        if (event.data?.type === 'ebay-oauth-success') {
          window.removeEventListener('message', handler);
          setSaveResult('success');
          setSaving(false);
          credentialService.list().then(credRes => {
            setCredentials(credRes.data?.data || []);
          });
          setTimeout(() => closeModal(), 1500);
        } else if (event.data?.type === 'ebay-oauth-error') {
          window.removeEventListener('message', handler);
          setSaveResult('error');
          setSaveError(event.data.error || 'eBay authorization failed');
          setSaving(false);
        }
      };
      window.addEventListener('message', handler);

      // Poll for popup close (handles cases where postMessage doesn't fire)
      const pollInterval = setInterval(() => {
        if (popup.closed) {
          clearInterval(pollInterval);
          window.removeEventListener('message', handler);
          setSaving(false);
          credentialService.list().then(credRes => {
            setCredentials(credRes.data?.data || []);
          });
        }
      }, 1000);

      // Safety timeout — reset saving after 2 minutes no matter what
      setTimeout(() => {
        clearInterval(pollInterval);
        window.removeEventListener('message', handler);
        setSaving(false);
      }, 120000);

    } catch (err: any) {
      setSaveResult('error');
      setSaveError(err.message || 'Failed to start eBay OAuth');
      setSaving(false);
    }
  }

  async function handleTest(credId: string) {
    setTestingId(credId);
    try {
      const res = await credentialService.test(credId);
      const ok = res.data?.connected;
      setTestResult(prev => ({ ...prev, [credId]: ok ? 'success' : 'error' }));
      // If test succeeded, update local credential status and store mall_id for Temu
      if (ok) {
        setCredentials(prev => prev.map(c => {
          if (c.credential_id !== credId) return c;
          const updated = { ...c, last_test_status: 'success' };
          if (res.data?.mall_id) (updated as any).mall_id = res.data.mall_id;
          return updated;
        }));
      } else {
        setCredentials(prev => prev.map(c =>
          c.credential_id === credId ? { ...c, last_test_status: 'failed' } : c
        ));
      }
    } catch { setTestResult(prev => ({ ...prev, [credId]: 'error' })); }
    finally { setTestingId(null); }
  }

  function handleReconnect(cred: MarketplaceCredential) {
    // Find the adapter definition for this channel
    const adapter = adapters.find(a => a.id === cred.channel) ||
      FALLBACK_ADAPTERS.find(a => a.id === cred.channel);
    if (!adapter) { alert('Cannot find adapter for ' + cred.channel); return; }
    // Store the credential being reconnected so handleSave uses PUT /reconnect
    setReconnectingCredId(cred.credential_id);
    // Pre-fill the account name so the user doesn't have to retype it
    setAccountName(cred.account_name || '');
    setEnvironment((cred as any).environment || 'production');
    setFormValues({});
    setSaveResult(null);
    setSaveError('');
    setEbayManualEntry(true);
    setSelectedAdapter(adapter);
    setModalOpen(true);
  }

  async function handleDelete(credId: string) {
    if (!confirm('Are you sure you want to remove this connection?')) return;
    try {
      await credentialService.delete(credId);
      setCredentials(prev => prev.filter(c => c.credential_id !== credId));
    } catch (err: any) { alert('Failed to delete: ' + (err.message || 'Unknown error')); }
  }

  async function handleToggleField(credId: string, field: 'active' | 'inventory_sync_enabled', value: boolean) {
    setTogglingId(credId + field);
    try {
      const API_BASE = (import.meta as any).env?.VITE_API_URL || 'http://localhost:8080/api/v1';
      await fetch(`${API_BASE}/marketplace/credentials/${credId}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId() },
        body: JSON.stringify({ [field]: value }),
      });
      setCredentials(prev => prev.map(c =>
        c.credential_id === credId ? { ...c, [field]: value } : c
      ));
    } catch (err: any) { alert('Failed to update: ' + (err.message || 'Unknown error')); }
    finally { setTogglingId(null); }
  }

  if (loading) return <div className="page"><div className="loading-state"><div className="spinner"></div><p>Loading marketplace connections...</p></div></div>;

  // Task 1: Adapter type classification
  // Prefer the adapter_type field from Firestore; fall back to the hardcoded set.
  const THIRD_PARTY_ADAPTERS = new Set(['woocommerce', 'magento', 'bigcommerce', 'shopify', 'shopline']);
  const getAdapterType = (a: AdapterInfo) =>
    a.adapter_type || (THIRD_PARTY_ADAPTERS.has(a.id) ? 'third_party' : 'direct');

  // Filtered adapters
  const filteredAdapters = [...adapters].sort((a, b) => {
    const sa = a.sort_order ?? 9999;
    const sb = b.sort_order ?? 9999;
    if (sa !== sb) return sa - sb;
    return a.display_name.localeCompare(b.display_name);
  }).filter(a => {
    if (!a.is_active) return false;
    if (filterTab === 'direct') return getAdapterType(a) === 'direct';
    if (filterTab === 'third_party') return getAdapterType(a) === 'third_party';
    return true;
  }).filter(a => !searchQuery || a.display_name.toLowerCase().includes(searchQuery.toLowerCase()));

  // Sync state pill helper
  const SyncPill = ({ state, label, errorMsg }: { state: string; label: string; errorMsg?: string }) => {
    const styles: Record<string, { bg: string; color: string; border: string; dot?: string }> = {
      syncing:        { bg: '#0ea5e915', color: '#0ea5e9', border: '#0ea5e940', dot: '#0ea5e9' },
      pending:        { bg: '#f59e0b15', color: '#f59e0b', border: '#f59e0b40', dot: '#f59e0b' },
      error:          { bg: 'var(--danger-glow)', color: 'var(--danger)', border: 'var(--danger)', dot: 'var(--danger)' },
      not_configured: { bg: 'var(--bg-tertiary)', color: 'var(--text-muted)', border: 'var(--border)' },
    };
    const s = styles[state] || styles.not_configured;
    return (
      <div title={errorMsg || label} style={{
        display: 'inline-flex', alignItems: 'center', gap: 4,
        padding: '2px 8px', borderRadius: 99, fontSize: 11, fontWeight: 600,
        background: s.bg, color: s.color, border: `1px solid ${s.border}`,
        cursor: errorMsg ? 'help' : 'default',
      }}>
        {s.dot && <span style={{ width: 6, height: 6, borderRadius: '50%', background: s.dot, display: 'inline-block', animation: state === 'syncing' ? 'pulse 2s infinite' : 'none' }} />}
        {label}
      </div>
    );
  };

  // Toggle switch component
  const Toggle = ({ checked, onChange, disabled }: { checked: boolean; onChange: (v: boolean) => void; disabled?: boolean }) => (
    <button
      onClick={() => !disabled && onChange(!checked)}
      disabled={disabled}
      style={{
        position: 'relative', width: 36, height: 20, borderRadius: 10, border: 'none', cursor: disabled ? 'not-allowed' : 'pointer',
        background: checked ? 'var(--primary)' : 'var(--border)', transition: 'background 0.2s', flexShrink: 0, opacity: disabled ? 0.5 : 1,
      }}
    >
      <span style={{
        position: 'absolute', top: 2, left: checked ? 18 : 2, width: 16, height: 16,
        borderRadius: '50%', background: '#fff', transition: 'left 0.2s',
      }} />
    </button>
  );

  const connectedCount = (channel: string) => credentials.filter(c => c.channel === channel && c.active).length;
  const thumbnailForChannel = (channel: string) => adapters.find(a => a.id === channel)?.thumbnail_url;

  return (
    <div className="page">
      <div className="page-header" style={{ marginBottom: 0, paddingBottom: 24 }}>
        <div>
          <h1 className="page-title">Connections</h1>
          <p className="page-subtitle">Manage your marketplace integrations</p>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
          <button onClick={loadData} style={{
            padding: '7px 14px', borderRadius: 8, border: '1px solid var(--border)',
            background: 'var(--bg-secondary)', color: 'var(--text-secondary)',
            fontSize: 13, cursor: 'pointer', display: 'flex', alignItems: 'center', gap: 6,
          }}>
            ↻ Refresh
          </button>
          <button onClick={() => navigate('/marketplace/connections') /* FIX (Issue 4): /marketplace/compare not yet registered — route to connections as fallback; restore once CompareIntegrations route is added to App.tsx */} style={{
            padding: '7px 16px', borderRadius: 8, border: '1px solid var(--border)',
            background: 'var(--bg-secondary)', color: 'var(--text-primary)',
            fontSize: 13, cursor: 'pointer', display: 'flex', alignItems: 'center', gap: 6, fontWeight: 500,
          }}>
            📊 Compare Channels
          </button>
        </div>
      </div>

      {error && (
        <div style={{ padding: '12px 16px', marginBottom: 20, borderRadius: 8, background: 'var(--danger-glow)', border: '1px solid var(--danger)', color: 'var(--danger)', fontSize: 13, fontWeight: 600 }}>
          ⚠ {error}
          <button onClick={loadData} style={{ marginLeft: 12, background: 'none', border: 'none', color: 'var(--primary)', cursor: 'pointer', fontWeight: 600 }}>Retry</button>
        </div>
      )}

      {/* ═══════════════════════════════════════════════════════
          SECTION 1 — CONNECTED ACCOUNTS
      ══════════════════════════════════════════════════════════ */}
      <section style={{ marginBottom: 40 }}>
        <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', marginBottom: 16 }}>
          <h2 style={{ fontSize: 13, fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.08em', color: 'var(--text-muted)', margin: 0 }}>
            Connected Accounts
            {credentials.length > 0 && (
              <span style={{ marginLeft: 8, background: 'var(--primary)', color: 'white', borderRadius: 99, padding: '1px 8px', fontSize: 11, fontWeight: 700, verticalAlign: 'middle' }}>
                {credentials.length}
              </span>
            )}
          </h2>
        </div>

        {credentials.length === 0 && !loading ? (
          <div style={{
            padding: '32px 24px', borderRadius: 12,
            border: '1px dashed var(--border)',
            background: 'var(--bg-secondary)',
            textAlign: 'center',
          }}>
            <div style={{ fontSize: 32, marginBottom: 10 }}>🔌</div>
            <div style={{ fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 4 }}>No connections yet</div>
            <div style={{ fontSize: 13, color: 'var(--text-muted)' }}>Connect a channel below to start importing orders and syncing stock</div>
          </div>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
            {[...credentials].sort((a, b) => a.channel.localeCompare(b.channel)).map(cred => {
              const color = adapterColor[cred.channel] || 'var(--primary)';
              const tr = testResult[cred.credential_id];
              const syncStatus = channelSyncStatus[cred.credential_id];
              const isTogglingActive  = togglingId === cred.credential_id + 'active';
              const isTogglingInvSync = togglingId === cred.credential_id + 'inventory_sync_enabled';
              const isTogglingOrders  = togglingId === cred.credential_id + 'order_processing_enabled';
              const isTogglingPricing = togglingId === cred.credential_id + 'pricing_sync_enabled';
              const isTogglingNotifs  = togglingId === cred.credential_id + 'notifications_enabled';

              return (
                <div key={cred.credential_id} style={{
                  display: 'grid',
                  gridTemplateColumns: cred.last_test_status === 'failed' ? '44px 1fr auto' : '44px 1fr auto auto auto auto auto auto',
                  alignItems: 'center',
                  gap: 16,
                  padding: '14px 18px',
                  background: 'var(--bg-secondary)',
                  border: '1px solid var(--border)',
                  borderRadius: 10,
                  transition: 'border-color 0.15s',
                }}>
                  {/* Logo dot */}
                  <div style={{
                    width: 52, height: 52, borderRadius: 12, flexShrink: 0,
                    background: color + '18',
                    display: 'flex', alignItems: 'center', justifyContent: 'center',
                    fontSize: 20,
                  }}>
                    <ChannelLogo id={cred.channel} size={30} thumbnailUrl={thumbnailForChannel(cred.channel)} />
                  </div>

                  {/* Name + meta */}
                  <div style={{ minWidth: 0 }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
                      <span style={{ fontWeight: 700, fontSize: 14, color: 'var(--text-primary)', textTransform: 'capitalize' }}>
                        {cred.account_name || cred.channel}
                      </span>
                      <span style={{ fontSize: 11, color: 'var(--text-muted)', fontStyle: 'italic' }}>{cred.channel}</span>
                      <span style={{
                        padding: '1px 7px', borderRadius: 99, fontSize: 10, fontWeight: 700,
                        textTransform: 'uppercase', letterSpacing: '0.04em',
                        background: cred.environment === 'production' ? 'rgba(34,197,94,0.12)' : 'rgba(245,158,11,0.12)',
                        color: cred.environment === 'production' ? '#22c55e' : '#f59e0b',
                        border: `1px solid ${cred.environment === 'production' ? 'rgba(34,197,94,0.25)' : 'rgba(245,158,11,0.25)'}`,
                      }}>
                        {cred.environment}
                      </span>
                      {tr && (
                        <span style={{ fontSize: 11, fontWeight: 600, color: tr === 'success' ? '#22c55e' : 'var(--danger)' }}>
                          {tr === 'success' ? '✓ OK' : '✕ Failed'}
                        </span>
                      )}
                    </div>
                    {syncStatus && (
                      <div style={{ display: 'flex', gap: 4, marginTop: 5, flexWrap: 'wrap' }}>
                        <SyncPill state={syncStatus.orders.state}    label="Orders"    errorMsg={syncStatus.orders.error_msg} />
                        <SyncPill state={syncStatus.inventory.state} label="Inventory" errorMsg={syncStatus.inventory.error_msg} />
                        <SyncPill state={syncStatus.listings.state}  label="Listings"  errorMsg={syncStatus.listings.error_msg} />
                      </div>
                    )}
                  </div>

                  {/* Toggles — only shown for healthy credentials */}
                  {cred.last_test_status !== 'failed' ? (<>
                  <ToggleWithLabel
                    label="Order Processing"
                    checked={(cred as any).order_processing_enabled ?? true}
                    onChange={v => handleToggleField(cred.credential_id, 'order_processing_enabled' as any, v)}
                    disabled={isTogglingOrders}
                    accentColor="var(--primary)"
                  />
                  <ToggleWithLabel
                    label="Pricing Sync"
                    checked={(cred as any).pricing_sync_enabled ?? false}
                    onChange={v => handleToggleField(cred.credential_id, 'pricing_sync_enabled' as any, v)}
                    disabled={isTogglingPricing}
                    accentColor="#f59e0b"
                  />
                  <ToggleWithLabel
                    label="Notifications"
                    checked={(cred as any).notifications_enabled ?? false}
                    onChange={v => handleToggleField(cred.credential_id, 'notifications_enabled' as any, v)}
                    disabled={isTogglingNotifs}
                    accentColor="#8b5cf6"
                    subtitle={(cred as any).notifications_mode === 'webhook' ? 'Webhook' : 'Polling'}
                  />
                  <ToggleWithLabel
                    label="Active"
                    checked={cred.active ?? true}
                    onChange={v => handleToggleField(cred.credential_id, 'active', v)}
                    disabled={isTogglingActive}
                    accentColor="#22c55e"
                  />
                  </>) : null}

                  {/* Actions */}
                  <div style={{ display: 'flex', gap: 6, flexShrink: 0, alignItems: 'center' }}>
                    {cred.last_test_status === 'failed' ? (
                      /* Credential has a failed token — show reconnect banner + button */
                      <>
                        <div style={{
                          display: 'flex', alignItems: 'center', gap: 8,
                          padding: '6px 10px', borderRadius: 8,
                          background: 'rgba(239,68,68,0.08)',
                          border: '1px solid rgba(239,68,68,0.3)',
                        }}>
                          <span style={{ fontSize: 11, color: 'var(--danger)', fontWeight: 600 }}>
                            ⚠️ Token expired
                          </span>
                          <button
                            onClick={() => handleReconnect(cred)}
                            style={{
                              height: 26, padding: '0 12px', borderRadius: 6,
                              background: 'var(--danger)', border: 'none',
                              cursor: 'pointer', fontSize: 12, fontWeight: 700,
                              color: '#fff', whiteSpace: 'nowrap',
                              display: 'inline-flex', alignItems: 'center', gap: 4,
                            }}
                          >
                            🔁 Reconnect Account
                          </button>
                        </div>
                        <ActionIconBtn title="Remove" onClick={() => handleDelete(cred.credential_id)} danger>🗑️</ActionIconBtn>
                      </>
                    ) : (
                      /* Normal actions for healthy credentials */
                      <>
                        <ActionIconBtn title="Configure" onClick={() => navigate(`/marketplace/channels/${cred.credential_id}/config`)}>⚙️</ActionIconBtn>
                        <ActionIconBtn title="Test connection" onClick={() => handleTest(cred.credential_id)} disabled={testingId === cred.credential_id}>
                          {testingId === cred.credential_id ? '⏳' : '🔌'}
                        </ActionIconBtn>
                        <ActionIconBtn title="Remove" onClick={() => handleDelete(cred.credential_id)} danger>🗑️</ActionIconBtn>
                      </>
                    )}
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </section>

      {/* ═══════════════════════════════════════════════════════
          QUICK CONNECT — channels selected during setup wizard
      ══════════════════════════════════════════════════════════ */}
      {quickConnectChannels.length > 0 && !quickConnectDismissed && (() => {
        // Filter to only show channels not yet connected
        const connectedIds = new Set(credentials.map(c => c.channel));
        const unconnected = quickConnectChannels.filter(ch => !connectedIds.has(ch));
        if (unconnected.length === 0) return null;
        return (
          <section style={{ marginBottom: 24 }}>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 12 }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                <span style={{ fontSize: 16 }}>⚡</span>
                <h2 style={{ fontSize: 13, fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.08em', color: 'var(--primary, #7c3aed)', margin: 0 }}>
                  Quick Connect
                </h2>
                <span style={{ fontSize: 11, color: 'var(--text-muted)', fontWeight: 400 }}>— channels you selected during setup</span>
              </div>
              <button onClick={() => setQuickConnectDismissed(true)} style={{ background: 'none', border: 'none', color: 'var(--text-muted)', cursor: 'pointer', fontSize: 12, padding: '4px 8px' }}>
                Dismiss
              </button>
            </div>
            <div style={{
              background: 'var(--bg-secondary)', borderRadius: 12,
              border: '1px solid var(--border)', overflow: 'hidden',
            }}>
              {unconnected.map((chId, i) => {
                const adapter = adapters.find(a => a.id === chId);
                if (!adapter) return null;
                const color = adapterColor[adapter.id] || 'var(--primary)';
                const isLast = i === unconnected.length - 1;
                return (
                  <div key={chId} style={{
                    display: 'flex', alignItems: 'center', gap: 16,
                    padding: '13px 18px',
                    borderBottom: isLast ? 'none' : '1px solid var(--border)',
                    transition: 'background 0.1s',
                  }}
                  onMouseEnter={e => (e.currentTarget.style.background = 'var(--bg-tertiary)')}
                  onMouseLeave={e => (e.currentTarget.style.background = 'transparent')}>
                    <div style={{
                      width: 52, height: 52, borderRadius: 12, flexShrink: 0,
                      background: color + '15',
                      display: 'flex', alignItems: 'center', justifyContent: 'center',
                    }}>
                      <ChannelLogo id={adapter.id} size={30} thumbnailUrl={adapter.thumbnail_url} />
                    </div>
                    <div style={{ flex: 1, minWidth: 0 }}>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                        <span style={{ fontWeight: 600, fontSize: 14, color: 'var(--text-primary)' }}>
                          {adapter.display_name}
                        </span>
                      </div>
                      <div style={{ display: 'flex', gap: 6, marginTop: 5, flexWrap: 'wrap' }}>
                        {(adapter.features || []).slice(0, 4).map(f => (
                          <span key={f} style={{
                            fontSize: 10, padding: '2px 7px', borderRadius: 4, fontWeight: 600,
                            textTransform: 'uppercase', letterSpacing: '0.04em',
                            background: 'var(--bg-tertiary)', color: 'var(--text-muted)',
                            border: '1px solid var(--border)',
                          }}>{f.replace(/_/g, ' ')}</span>
                        ))}
                      </div>
                    </div>
                    <button
                      onClick={() => openConnectModal(adapter)}
                      style={{
                        padding: '7px 18px', borderRadius: 8,
                        background: 'var(--primary)',
                        color: 'white',
                        fontSize: 13, fontWeight: 600, cursor: 'pointer',
                        flexShrink: 0, whiteSpace: 'nowrap',
                        border: 'none',
                        transition: 'opacity 0.15s',
                      }}
                      onMouseEnter={e => (e.currentTarget.style.opacity = '0.85')}
                      onMouseLeave={e => (e.currentTarget.style.opacity = '1')}
                    >
                      Connect
                    </button>
                  </div>
                );
              })}
            </div>
          </section>
        );
      })()}

      {/* ═══════════════════════════════════════════════════════
          SECTION 2 — AVAILABLE MARKETPLACES
      ══════════════════════════════════════════════════════════ */}
      <section>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 16, gap: 12, flexWrap: 'wrap' }}>
          <h2 style={{ fontSize: 13, fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.08em', color: 'var(--text-muted)', margin: 0 }}>
            Available Marketplaces
          </h2>
          <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
            {/* Filter tabs */}
            <div style={{ display: 'flex', gap: 3, background: 'var(--bg-tertiary)', padding: 3, borderRadius: 8, border: '1px solid var(--border)' }}>
              {(['all', 'direct', 'third_party'] as const).map(tab => (
                <button key={tab} onClick={() => setFilterTab(tab)} style={{
                  padding: '5px 12px', borderRadius: 6, border: 'none',
                  background: filterTab === tab ? 'var(--bg-elevated, var(--bg-secondary))' : 'transparent',
                  color: filterTab === tab ? 'var(--text-primary)' : 'var(--text-muted)',
                  fontWeight: filterTab === tab ? 600 : 400,
                  fontSize: 12, cursor: 'pointer',
                  boxShadow: filterTab === tab ? '0 1px 3px rgba(0,0,0,0.15)' : 'none',
                  transition: 'all 0.15s',
                }}>
                  {tab === 'all' ? 'All' : tab === 'direct' ? 'Direct' : 'Third Party'}
                </button>
              ))}
            </div>
            <input
              className="input"
              placeholder="Search…"
              value={searchQuery}
              onChange={e => setSearchQuery(e.target.value)}
              style={{ width: 180, fontSize: 13, padding: '6px 12px' }}
            />
          </div>
        </div>

        {/* Available channels — list rows with connect button on right */}
        <div style={{
          background: 'var(--bg-secondary)', borderRadius: 12,
          border: '1px solid var(--border)', overflow: 'hidden',
        }}>
          {filteredAdapters.length === 0 ? (
            <div style={{ padding: '40px', textAlign: 'center', color: 'var(--text-muted)', fontSize: 14 }}>
              No channels match your search.
            </div>
          ) : (
            filteredAdapters.map((adapter, i) => {
              const count = connectedCount(adapter.id);
              const color = adapterColor[adapter.id] || 'var(--primary)';
              const isLast = i === filteredAdapters.length - 1;

              return (
                <div key={adapter.id} style={{
                  display: 'flex', alignItems: 'center', gap: 16,
                  padding: '13px 18px',
                  borderBottom: isLast ? 'none' : '1px solid var(--border)',
                  transition: 'background 0.1s',
                }}
                onMouseEnter={e => (e.currentTarget.style.background = 'var(--bg-tertiary)')}
                onMouseLeave={e => (e.currentTarget.style.background = 'transparent')}>

                  {/* Logo */}
                  <div style={{
                    width: 52, height: 52, borderRadius: 12, flexShrink: 0,
                    background: color + '15',
                    display: 'flex', alignItems: 'center', justifyContent: 'center',
                    fontSize: 18,
                  }}>
                    <ChannelLogo id={adapter.id} size={30} thumbnailUrl={adapter.thumbnail_url} />
                  </div>

                  {/* Name + features */}
                  <div style={{ flex: 1, minWidth: 0 }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                      <span style={{ fontWeight: 600, fontSize: 14, color: 'var(--text-primary)' }}>
                        {adapter.display_name}
                      </span>
                      {adapter.supported_regions && adapter.supported_regions.length > 0 && (
                        <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>
                          {adapter.supported_regions.slice(0, 3).join(' · ')}{adapter.supported_regions.length > 3 ? ' …' : ''}
                        </span>
                      )}
                      {count > 0 && (
                        <span style={{
                          padding: '1px 7px', borderRadius: 99, fontSize: 10, fontWeight: 700,
                          background: 'rgba(34,197,94,0.12)', color: '#22c55e',
                          border: '1px solid rgba(34,197,94,0.2)',
                        }}>
                          {count} connected
                        </span>
                      )}
                    </div>
                    {adapter.description && (
                      <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 2, marginBottom: 2 }}>
                        {adapter.description}
                      </div>
                    )}
                    <div style={{ display: 'flex', gap: 4, marginTop: 4, flexWrap: 'wrap' }}>
                      {(adapter.features || []).slice(0, 5).map(f => (
                        <span key={f} style={{
                          padding: '1px 6px', borderRadius: 4, fontSize: 10, fontWeight: 500,
                          textTransform: 'uppercase', letterSpacing: '0.04em',
                          background: 'var(--bg-tertiary)', color: 'var(--text-muted)',
                          border: '1px solid var(--border)',
                        }}>{f.replace(/_/g, ' ')}</span>
                      ))}
                    </div>
                  </div>

                  {/* Connect button — always on far right */}
                  <button
                    onClick={() => openConnectModal(adapter)}
                    style={{
                      padding: '7px 18px', borderRadius: 8,
                      background: count > 0 ? 'var(--bg-tertiary)' : 'var(--primary)',
                      color: count > 0 ? 'var(--text-secondary)' : 'white',
                      fontSize: 13, fontWeight: 600, cursor: 'pointer',
                      flexShrink: 0, whiteSpace: 'nowrap',
                      border: count > 0 ? '1px solid var(--border)' : 'none',
                      transition: 'opacity 0.15s',
                    }}
                    onMouseEnter={e => (e.currentTarget.style.opacity = '0.85')}
                    onMouseLeave={e => (e.currentTarget.style.opacity = '1')}
                  >
                    {count > 0 ? '+ Add Account' : 'Connect'}
                  </button>
                </div>
              );
            })
          )}
        </div>
      </section>

      {/* ── Configure Panel ── */}
      {configOpen && configuringCred && (
        <div style={{ position: 'fixed', inset: 0, zIndex: 1040, display: 'flex', alignItems: 'stretch', justifyContent: 'flex-end' }}>
          <div style={{ position: 'absolute', inset: 0, background: 'rgba(0,0,0,0.6)', backdropFilter: 'blur(4px)' }} onClick={() => setConfigOpen(false)} />
          <div style={{ position: 'relative', background: 'var(--bg-secondary)', borderLeft: '1px solid var(--border)', width: 520, maxWidth: '95vw', display: 'flex', flexDirection: 'column', overflowY: 'auto' }}>
            <div style={{ padding: '20px 24px', borderBottom: '1px solid var(--border)', display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexShrink: 0 }}>
              <div>
                <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                  <ChannelLogo id={configuringCred.channel} size={28} thumbnailUrl={thumbnailForChannel(configuringCred.channel)} />
                  <h3 style={{ fontSize: 17, fontWeight: 700 }}>Configure {configuringCred.account_name}</h3>
                </div>
                <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 2, textTransform: 'capitalize' }}>{configuringCred.channel} · {configuringCred.environment}</div>
              </div>
              <button onClick={() => setConfigOpen(false)} style={{ background: 'none', border: 'none', color: 'var(--text-muted)', cursor: 'pointer', fontSize: 22 }}>✕</button>
            </div>
            <div style={{ display: 'flex', borderBottom: '1px solid var(--border)', flexShrink: 0 }}>
              {(['orders', 'stock', 'shipping', 'products'] as const).map(tab => (
                <button key={tab} onClick={() => setConfigTab(tab)} style={{ flex: 1, padding: '12px 0', background: 'none', border: 'none', borderBottom: configTab === tab ? '2px solid var(--primary)' : '2px solid transparent', color: configTab === tab ? 'var(--primary)' : 'var(--text-muted)', fontWeight: configTab === tab ? 700 : 400, fontSize: 13, cursor: 'pointer', textTransform: 'capitalize' }}>
                  {tab === 'orders' ? '📦 Orders' : tab === 'stock' ? '📊 Stock' : tab === 'shipping' ? '🚚 Shipping' : '📥 Products'}
                </button>
              ))}
            </div>
            <div style={{ flex: 1, padding: 24, overflowY: 'auto' }}>
              {configLoading ? (
                <div style={{ display: 'flex', alignItems: 'center', gap: 12, color: 'var(--text-muted)' }}><div className="spinner" style={{ width: 20, height: 20 }} />Loading configuration...</div>
              ) : configData && (
                <>
                  {configTab === 'orders' && (
                    <div style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
                      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: 16, background: 'var(--bg-tertiary)', borderRadius: 8, border: '1px solid var(--border)' }}>
                        <div><div style={{ fontWeight: 600, fontSize: 14 }}>Automatic Order Sync</div><div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>Automatically download orders on a schedule</div></div>
                        <label style={{ display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer' }}>
                          <input type="checkbox" checked={configData.orders.enabled} onChange={e => setConfigData((p: any) => ({ ...p, orders: { ...p.orders, enabled: e.target.checked } }))} style={{ width: 16, height: 16 }} />
                          <span style={{ fontSize: 13, fontWeight: 600 }}>{configData.orders.enabled ? 'Enabled' : 'Disabled'}</span>
                        </label>
                      </div>
                      <div>
                        <label style={{ display: 'block', fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 8, textTransform: 'uppercase' }}>Sync Frequency</label>
                        <select value={configData.orders.frequency_minutes} onChange={e => setConfigData((p: any) => ({ ...p, orders: { ...p.orders, frequency_minutes: parseInt(e.target.value) } }))} style={{ width: '100%', background: 'var(--bg-tertiary)', border: '1px solid var(--border)', borderRadius: 8, padding: '10px 12px', color: 'var(--text-primary)', fontSize: 14 }}>
                          <option value={15}>Every 15 minutes</option><option value={30}>Every 30 minutes</option><option value={60}>Every hour</option><option value={360}>Every 6 hours</option><option value={1440}>Once daily</option>
                        </select>
                      </div>
                      <div>
                        <label style={{ display: 'block', fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 8, textTransform: 'uppercase' }}>Lookback Window</label>
                        <select value={configData.orders.lookback_hours} onChange={e => setConfigData((p: any) => ({ ...p, orders: { ...p.orders, lookback_hours: parseInt(e.target.value) } }))} style={{ width: '100%', background: 'var(--bg-tertiary)', border: '1px solid var(--border)', borderRadius: 8, padding: '10px 12px', color: 'var(--text-primary)', fontSize: 14 }}>
                          <option value={2}>Last 2 hours</option><option value={6}>Last 6 hours</option><option value={24}>Last 24 hours</option><option value={48}>Last 48 hours</option><option value={168}>Last 7 days</option>
                        </select>
                      </div>
                      <div>
                        <label style={{ display: 'block', fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 8, textTransform: 'uppercase' }}>Order Status to Import</label>
                        <select value={configData.orders.status_filter} onChange={e => setConfigData((p: any) => ({ ...p, orders: { ...p.orders, status_filter: e.target.value } }))} style={{ width: '100%', background: 'var(--bg-tertiary)', border: '1px solid var(--border)', borderRadius: 8, padding: '10px 12px', color: 'var(--text-primary)', fontSize: 14 }}>
                          <option value="Unshipped">Unshipped only (recommended)</option><option value="Unshipped,Pending">Unshipped + Pending</option><option value="all">All statuses</option>
                        </select>
                      </div>
                      {configuringCred.channel === 'amazon' && (
                        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: 16, background: 'var(--bg-tertiary)', borderRadius: 8, border: '1px solid var(--border)' }}>
                          <div><div style={{ fontWeight: 600, fontSize: 14 }}>Include FBA Orders</div><div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>Amazon Fulfilled — stock managed by Amazon</div></div>
                          <label style={{ display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer' }}>
                            <input type="checkbox" checked={configData.orders.include_fba} onChange={e => setConfigData((p: any) => ({ ...p, orders: { ...p.orders, include_fba: e.target.checked } }))} style={{ width: 16, height: 16 }} />
                            <span style={{ fontSize: 13, fontWeight: 600 }}>{configData.orders.include_fba ? 'Yes' : 'No'}</span>
                          </label>
                        </div>
                      )}
                      <div style={{ padding: 16, background: 'var(--bg-tertiary)', borderRadius: 8, border: '1px solid var(--border)' }}>
                        <div style={{ fontWeight: 600, fontSize: 14, marginBottom: 4 }}>Manual Download</div>
                        <div style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 12 }}>Download orders now using the lookback window above.</div>
                        <button className="btn btn-primary" onClick={handleImportNow} disabled={importingNow}>
                          {importingNow ? '⏳ Downloading...' : '⬇ Download Now'}
                        </button>
                        {importNowResult && <div style={{ marginTop: 10, fontSize: 13 }}>{importNowResult}</div>}
                      </div>
                    </div>
                  )}
                  {configTab === 'stock' && (
                    <div style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
                      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: 16, background: 'var(--bg-tertiary)', borderRadius: 8, border: '1px solid var(--border)' }}>
                        <div><div style={{ fontWeight: 600, fontSize: 14 }}>Reserve Stock for Pending Orders</div><div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>Hold stock against pending orders</div></div>
                        <label style={{ display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer' }}>
                          <input type="checkbox" checked={configData.stock.reserve_pending} onChange={e => setConfigData((p: any) => ({ ...p, stock: { ...p.stock, reserve_pending: e.target.checked } }))} style={{ width: 16, height: 16 }} />
                          <span style={{ fontSize: 13, fontWeight: 600 }}>{configData.stock.reserve_pending ? 'Yes' : 'No'}</span>
                        </label>
                      </div>
                    </div>
                  )}
                  {configTab === 'shipping' && (
                    <div style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
                      <div style={{ padding: 16, background: 'rgba(255,153,0,0.08)', borderRadius: 8, border: '1px solid rgba(255,153,0,0.3)', fontSize: 13, color: 'var(--text-secondary)', lineHeight: 1.6 }}>
                        <strong style={{ color: 'var(--text-primary)' }}>🚧 Amazon Buy Shipping coming soon</strong>
                      </div>
                    </div>
                  )}
                  {configTab === 'products' && (
                    <div style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
                      <div style={{ padding: 16, background: 'var(--bg-tertiary)', borderRadius: 8, border: '1px solid var(--border)', fontSize: 13, color: 'var(--text-secondary)', lineHeight: 1.6 }}>
                        Controls whether products are automatically downloaded from this marketplace into your PIM catalogue.
                      </div>
                      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '14px 16px', background: 'var(--bg-tertiary)', borderRadius: 8, border: '1px solid var(--border)' }}>
                        <div>
                          <div style={{ fontWeight: 600, fontSize: 14 }}>Auto-import Products</div>
                          <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>
                            When enabled, products are downloaded automatically when this account is connected or activated. First connection imports directly into the catalogue; additional connections go through Review Mappings.
                          </div>
                        </div>
                        <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginLeft: 24, flexShrink: 0 }}>
                          <input
                            type="checkbox"
                            checked={configData?.products?.import_enabled ?? true}
                            onChange={e => setConfigData((p: any) => ({ ...p, products: { ...(p.products || {}), import_enabled: e.target.checked } }))}
                            style={{ width: 16, height: 16 }}
                          />
                          <span style={{ fontSize: 13, fontWeight: 600 }}>
                            {(configData?.products?.import_enabled ?? true) ? 'Enabled' : 'Disabled'}
                          </span>
                        </div>
                      </div>
                    </div>
                  )}
                </>
              )}
            </div>
            <div style={{ padding: '16px 24px', borderTop: '1px solid var(--border)', display: 'flex', justifyContent: 'flex-end', gap: 12, flexShrink: 0 }}>
              <button className="btn btn-secondary" onClick={() => setConfigOpen(false)}>Close</button>
              <button className="btn btn-primary" onClick={saveConfig} disabled={configSaving || configLoading}>
                {configSaving ? 'Saving...' : configSaved ? '✅ Saved!' : 'Save Configuration'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Connect Modal */}
      {modalOpen && selectedAdapter && (
        <div style={{ position: 'fixed', inset: 0, zIndex: 1030, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
          <div style={{ position: 'absolute', inset: 0, background: 'rgba(0,0,0,0.7)', backdropFilter: 'blur(4px)' }} />
          <div style={{ position: 'relative', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 16, width: 520, maxWidth: '90vw', maxHeight: '85vh', overflow: 'auto' }}>
            <div style={{ padding: '20px 24px', borderBottom: '1px solid var(--border)', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
                <ChannelLogo id={selectedAdapter.id} size={32} thumbnailUrl={selectedAdapter.thumbnail_url} />
                <h3 style={{ fontSize: 18, fontWeight: 700 }}>Connect {selectedAdapter.display_name}</h3>
              </div>
              <button onClick={closeModal} style={{ background: 'none', border: 'none', color: 'var(--text-muted)', cursor: 'pointer', fontSize: 20 }}>✕</button>
            </div>
            <div style={{ padding: 24 }}>
              {selectedAdapter.id === 'amazon' && (<><div style={{ padding: 12, marginBottom: 16, borderRadius: 8, background: '#FF990018', border: '1px solid #FF990055', color: 'var(--text-primary)', fontSize: 13 }}><div style={{ fontWeight: 700, marginBottom: 4 }}>📦 Amazon SP-API — OAuth Connection</div><div style={{ color: 'var(--text-secondary)' }}>Click below to open Amazon Seller Central and authorise MarketMate. Each marketplace (UK, US, DE etc.) requires a separate connection.</div></div><div style={{ marginBottom: 16 }}><label style={labelStyle}>Marketplace <span style={{ color: 'var(--danger)' }}>*</span></label><select className="input" style={{ width: '100%' }} value={formValues.marketplace_id || 'A1F83G8C2ARO7P'} onChange={e => setFormValues(prev => ({ ...prev, marketplace_id: e.target.value }))}><option value="A1F83G8C2ARO7P">🇬🇧 Amazon UK</option><option value="ATVPDKIKX0DER">🇺🇸 Amazon US</option><option value="A1PA6795UKMFR9">🇩🇪 Amazon DE</option><option value="A13V1IB3VIYZZH">🇫🇷 Amazon FR</option><option value="APJ6JRA9NG5V4">🇮🇹 Amazon IT</option><option value="A1RKKUPIHCS9HS">🇪🇸 Amazon ES</option></select></div></>)}
              
              {selectedAdapter.id === 'temu' && (<div style={{ padding: 12, marginBottom: 16, borderRadius: 8, background: '#FF6B351A', border: '1px solid #FF6B3555', color: 'var(--text-primary)', fontSize: 13 }}><div style={{ fontWeight: 700, marginBottom: 4 }}>🛍️ Temu Open Platform</div><div style={{ color: 'var(--text-secondary)' }}>Company API keys are configured globally. Enter your Access Token below.</div></div>)}
              {selectedAdapter.id === 'ebay' && (<><div style={{ padding: 12, marginBottom: 16, borderRadius: 8, background: '#E532381A', border: '1px solid #E5323855', color: 'var(--text-primary)', fontSize: 13 }}><div style={{ fontWeight: 700, marginBottom: 4 }}>🏷️ eBay Developer Program</div><div style={{ color: 'var(--text-secondary)' }}>Company API keys are configured globally. Enter your Refresh Token below.</div></div><div style={{ display: 'flex', gap: 8, marginBottom: 16 }}>{([{ key: true, label: '🔑 Manual Entry', desc: 'Paste refresh token' }, { key: false, label: '🔗 OAuth Login', desc: 'Sign in via eBay' }] as const).map(opt => (<div key={String(opt.key)} onClick={() => setEbayManualEntry(opt.key)} style={{ flex: 1, padding: '10px 12px', borderRadius: 8, cursor: 'pointer', textAlign: 'center', background: ebayManualEntry === opt.key ? '#0064D215' : 'var(--bg-tertiary)', border: `1px solid ${ebayManualEntry === opt.key ? '#0064D2' : 'var(--border)'}` }}><div style={{ fontWeight: 600, fontSize: 13, color: ebayManualEntry === opt.key ? '#0064D2' : 'var(--text-primary)' }}>{opt.label}</div><div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 2 }}>{opt.desc}</div></div>))}</div></>)}
              {selectedAdapter.id === 'shopify' && (<><div style={{ padding: 12, marginBottom: 16, borderRadius: 8, background: '#96BF481A', border: '1px solid #96BF4855', color: 'var(--text-primary)', fontSize: 13 }}><div style={{ fontWeight: 700, marginBottom: 4 }}>🛍️ Shopify — OAuth Connection</div><div style={{ color: 'var(--text-secondary)' }}>Enter your store domain below. You'll be redirected to Shopify to authorise access. You can connect multiple Shopify stores — each gets its own connection.</div></div><div style={{ marginBottom: 16 }}><label style={labelStyle}>Store Domain <span style={{ color: 'var(--danger)' }}>*</span></label><input className="input" style={{ width: '100%' }} placeholder="mystore.myshopify.com" value={formValues.shop_domain || ''} onChange={e => setFormValues(prev => ({ ...prev, shop_domain: e.target.value.trim() }))} autoComplete="off" /><div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 4 }}>Enter just the domain — e.g. <strong>mystore.myshopify.com</strong></div></div></>)}
              {selectedAdapter.id === 'shopline' && (<><div style={{ padding: 12, marginBottom: 16, borderRadius: 8, background: '#00b8d41A', border: '1px solid #00b8d455', color: 'var(--text-primary)', fontSize: 13 }}><div style={{ fontWeight: 700, marginBottom: 4 }}>🛍️ Shopline — OAuth Connection</div><div style={{ color: 'var(--text-secondary)' }}>Enter your Shopline store ID below. You'll be redirected to Shopline to authorise access. You can connect multiple Shopline stores — each gets its own connection.</div></div><div style={{ marginBottom: 16 }}><label style={labelStyle}>Store ID <span style={{ color: 'var(--danger)' }}>*</span></label><input className="input" style={{ width: '100%' }} placeholder="mystore" value={formValues.shop_id || ''} onChange={e => setFormValues(prev => ({ ...prev, shop_id: e.target.value.trim() }))} autoComplete="off" /><div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 4 }}>Enter your store subdomain — e.g. <strong>mystore</strong> from mystore.myshopline.com</div></div></>)}
              <div style={{ marginBottom: 16 }}>
                <label style={labelStyle}>Account Name <span style={{ color: 'var(--danger)' }}>*</span></label>
                <input className="input" style={{ width: '100%' }} placeholder={`My ${selectedAdapter.display_name} Store`} value={accountName} onChange={e => setAccountName(e.target.value)} autoComplete="off" />
              </div>
              {selectedAdapter.id !== 'temu' && selectedAdapter.id !== 'amazon' && (
                <div style={{ marginBottom: 16 }}>
                  <label style={labelStyle}>Environment <span style={{ color: 'var(--danger)' }}>*</span></label>
                  <div style={{ display: 'flex', gap: 12 }}>
                    {(['sandbox', 'production'] as const).map(env => (
                      <div key={env} onClick={() => setEnvironment(env)} style={{ flex: 1, padding: 12, borderRadius: 8, cursor: 'pointer', textAlign: 'center', background: environment === env ? 'var(--primary-glow)' : 'var(--bg-tertiary)', border: `1px solid ${environment === env ? 'var(--primary)' : 'var(--border-bright)'}` }}>
                        <div style={{ fontWeight: 600, fontSize: 14, color: environment === env ? 'var(--primary)' : 'var(--text-primary)', textTransform: 'capitalize' }}>{env}</div>
                        <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 2 }}>{env === 'sandbox' ? 'For testing' : 'Live marketplace'}</div>
                      </div>
                    ))}
                  </div>
                </div>
              )}
              {(selectedAdapter.id !== 'ebay' || ebayManualEntry) && selectedAdapter.credential_fields?.map(field => (
                <div key={field.key} style={{ marginBottom: 16 }}>
                  <label style={labelStyle}>{field.label} {field.required && <span style={{ color: 'var(--danger)' }}>*</span>}</label>
                  {selectedAdapter.id === 'ebay' && field.key === 'marketplace_id' ? (
                    <select className="input" style={{ width: '100%' }} value={formValues[field.key] || 'EBAY_GB'} onChange={e => setFormValues(prev => ({ ...prev, [field.key]: e.target.value }))}>
                      <option value="EBAY_GB">🇬🇧 EBAY_GB</option><option value="EBAY_US">🇺🇸 EBAY_US</option><option value="EBAY_DE">🇩🇪 EBAY_DE</option><option value="EBAY_FR">🇫🇷 EBAY_FR</option><option value="EBAY_IT">🇮🇹 EBAY_IT</option><option value="EBAY_ES">🇪🇸 EBAY_ES</option><option value="EBAY_AU">🇦🇺 EBAY_AU</option><option value="EBAY_CA">🇨🇦 EBAY_CA</option>
                    </select>
                  ) : (
                    <input className="input" style={{ width: '100%' }} type={field.type === 'password' ? 'password' : 'text'} placeholder={`Enter ${field.label}`} value={formValues[field.key] || ''} onChange={e => setFormValues(prev => ({ ...prev, [field.key]: e.target.value }))} autoComplete="new-password" />
                  )}
                </div>
              ))}
              {selectedAdapter.id === 'ebay' && !ebayManualEntry && (
                <div style={{ padding: 16, borderRadius: 8, background: 'var(--bg-tertiary)', border: '1px solid var(--border)', marginBottom: 16, textAlign: 'center' }}>
                  <div style={{ fontSize: 32, marginBottom: 8 }}>🔐</div>
                  <div style={{ fontSize: 14, fontWeight: 600, color: 'var(--text-primary)', marginBottom: 6 }}>Secure eBay Authorization</div>
                  <div style={{ fontSize: 13, color: 'var(--text-secondary)', lineHeight: 1.5 }}>Clicking below will open eBay's login page. Sign in with the seller account you want to connect.</div>
                </div>
              )}
              {saveResult === 'success' && <div style={{ padding: 12, marginBottom: 16, borderRadius: 8, background: 'var(--success-glow)', border: '1px solid var(--success)', color: 'var(--success)', fontSize: 13, fontWeight: 600 }}>✓ Connection saved successfully!</div>}
              {saveResult === 'error' && <div style={{ padding: 12, marginBottom: 16, borderRadius: 8, background: 'var(--danger-glow)', border: '1px solid var(--danger)', color: 'var(--danger)', fontSize: 13, fontWeight: 600 }}>✕ {saveError || 'Connection failed.'}</div>}
              <div style={{ display: 'flex', gap: 12, justifyContent: 'flex-end', marginTop: 8 }}>
                <button className="btn btn-secondary" onClick={() => { setSaving(false); closeModal(); }}>Cancel</button>
                <button className="btn btn-primary" onClick={handleSave} disabled={saving}>
                  {saving ? '⏳ Connecting...' : selectedAdapter.id === 'ebay' && !ebayManualEntry ? '🔗 Connect with eBay' : selectedAdapter.id === 'shopify' ? '🔗 Connect with Shopify' : selectedAdapter.id === 'shopline' ? '🔗 Connect with Shopline' : selectedAdapter.id === 'amazon' ? '🔗 Connect with Amazon' : `🔌 Save ${selectedAdapter.display_name} Connection`}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

// ─── Sub-components ───────────────────────────────────────────────────────────

function ToggleWithLabel({
  label, checked, onChange, disabled, accentColor, subtitle
}: {
  label: string; checked: boolean; onChange: (v: boolean) => void;
  disabled?: boolean; accentColor?: string; subtitle?: string;
}) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 4, minWidth: 80 }}>
      <button
        onClick={() => !disabled && onChange(!checked)}
        disabled={disabled}
        role="switch"
        aria-checked={checked}
        style={{
          position: 'relative', width: 36, height: 20, borderRadius: 10,
          border: 'none', cursor: disabled ? 'not-allowed' : 'pointer',
          background: checked ? (accentColor || 'var(--primary)') : 'var(--border)',
          transition: 'background 0.2s', flexShrink: 0,
          opacity: disabled ? 0.5 : 1,
        }}
      >
        <span style={{
          position: 'absolute', top: 2, left: checked ? 18 : 2, width: 16, height: 16,
          borderRadius: '50%', background: '#fff', transition: 'left 0.2s',
          boxShadow: '0 1px 3px rgba(0,0,0,0.2)',
        }} />
      </button>
      <span style={{ fontSize: 10, fontWeight: 600, color: checked ? (accentColor || 'var(--primary)') : 'var(--text-muted)', textAlign: 'center', lineHeight: 1.2, whiteSpace: 'nowrap' }}>
        {label}
      </span>
      {subtitle && (
        <span style={{ fontSize: 9, color: 'var(--text-muted)', textAlign: 'center' }}>{subtitle}</span>
      )}
    </div>
  );
}

function ActionIconBtn({ children, title, onClick, disabled, danger }: {
  children: React.ReactNode; title?: string; onClick?: () => void;
  disabled?: boolean; danger?: boolean;
}) {
  return (
    <button
      title={title}
      onClick={onClick}
      disabled={disabled}
      style={{
        width: 30, height: 30, borderRadius: 6,
        background: 'var(--bg-tertiary)',
        border: '1px solid var(--border)',
        cursor: disabled ? 'not-allowed' : 'pointer',
        fontSize: 14, display: 'flex', alignItems: 'center', justifyContent: 'center',
        opacity: disabled ? 0.5 : 1,
        color: danger ? 'var(--danger)' : 'inherit',
        transition: 'background 0.1s',
      }}
    >
      {children}
    </button>
  );
}

const labelStyle: React.CSSProperties = {
  display: 'block', fontSize: 12, fontWeight: 600,
  color: 'var(--text-secondary)', marginBottom: 6,
  textTransform: 'uppercase', letterSpacing: '0.5px',
};

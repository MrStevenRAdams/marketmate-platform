import { useState, useEffect, useCallback } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { getActiveTenantId } from '../../contexts/TenantContext';
import { auth } from '../../contexts/AuthContext';

const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';
function api(path: string, init?: RequestInit) {
  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      'X-Tenant-Id': getActiveTenantId(),
      ...init?.headers,
    },
  });
}

// ── Types ─────────────────────────────────────────────────────────────────────

interface PaymentMapping  { channel_method: string; internal_method: string; }
interface ShippingMapping { channel_service: string; internal_carrier_id: string; internal_service_code: string; }
interface InventoryMapping { channel_sku: string; internal_sku: string; product_id?: string; }

interface ChannelInventorySyncConfig {
  update_inventory: boolean;
  max_quantity_to_sync: number;
  min_stock_level: number;
  latency_buffer_days: number;
  default_latency_days: number;
  location_ids: string[];
}

interface ChannelOrderConfig {
  enabled: boolean;
  frequency_minutes: number;
  include_fba: boolean;
  status_filter: string;
  lookback_hours: number;
  last_sync?: string;
  last_sync_status?: string;
  // S3 additions
  order_prefix: string;
  validate_on_download: boolean;
  download_unpaid_orders: boolean;
  reserve_unpaid_stock: boolean;
  dispatch_notes_enabled: boolean;
  refund_notes_enabled: boolean;
  cancellation_notes_enabled: boolean;
  channel_tax_enabled: boolean;
}

interface ChannelStockConfig {
  reserve_pending: boolean;
  location_id: string;
}

interface ChannelShippingConfig {
  use_amazon_buy_shipping: boolean;
  default_carrier: string;
  label_format: string;
  seller_fulfilled_prime: boolean;
}

interface TemuDefaultsConfig {
  fulfillment_type: number;       // 1 = Merchant Fulfilled, 2 = Temu Fulfilled
  shipment_limit_day: number;     // days to ship
  shipping_template_id: string;   // freight template ID
  origin_region1: string;         // e.g. Mainland China
  origin_region2: string;         // province (if Mainland China)
}

interface ChannelConfig {
  orders: ChannelOrderConfig;
  stock: ChannelStockConfig;
  shipping: ChannelShippingConfig;
  payment_mappings: PaymentMapping[];
  shipping_mappings: ShippingMapping[];
  inventory_mappings: InventoryMapping[];
  inventory_sync: ChannelInventorySyncConfig;
  temu_defaults?: TemuDefaultsConfig;
}

interface Credential {
  credential_id: string;
  channel: string;
  account_name: string;
  environment: string;
  status: string;
}

interface WarehouseLocation { location_id: string; name: string; path?: string; }
interface Carrier { carrier_id: string; name: string; services?: CarrierService[]; }
interface CarrierService { service_code: string; name: string; }
interface ProductSearchResult { product_id: string; sku: string; title: string; }

// ── Constants ─────────────────────────────────────────────────────────────────

const TABS = [
  { id: 'orders',         icon: '📦', label: 'Orders' },
  { id: 'tax',            icon: '💷', label: 'Tax & Financials' },
  { id: 'stock',          icon: '🏭', label: 'Location & Stock' },
  { id: 'payment',        icon: '💳', label: 'Payment Mapping' },
  { id: 'shipping',       icon: '🚚', label: 'Shipping Mapping' },
  { id: 'inventory',      icon: '🔗', label: 'Inventory Mapping' },
  { id: 'inventory_sync', icon: '🔄', label: 'Inventory Sync' },
  { id: 'notify',         icon: '🔔', label: 'Notifications' },
  { id: 'temu_defaults',  icon: '🛍️', label: 'Listing Defaults' },
  { id: 'brand_mapping',  icon: '🏷️', label: 'Brand Mapping' },
] as const;
type TabId = typeof TABS[number]['id'];

const INTERNAL_PAYMENT_METHODS = ['Bank Transfer', 'PayPal', 'Credit Card', 'Debit Card', 'Cheque', 'Cash', 'Amazon Pay', 'Other'];

const CHANNEL_EMOJI: Record<string, string> = {
  amazon: '🟠', ebay: '🔴', shopify: '🟢', temu: '🟤', tiktok: '⚫', walmart: '🔵',
  etsy: '🟡', woocommerce: '🟣', kaufland: '🔴', onbuy: '🟠', magento: '🟠',
  bigcommerce: '🔵', bluepark: '🟢', wish: '🔵',
};

const DEFAULT_CONFIG: ChannelConfig = {
  orders: {
    enabled: false, frequency_minutes: 30, include_fba: false,
    status_filter: 'Unshipped', lookback_hours: 24,
    order_prefix: '', validate_on_download: false,
    download_unpaid_orders: false, reserve_unpaid_stock: false,
    dispatch_notes_enabled: false, refund_notes_enabled: false,
    cancellation_notes_enabled: false, channel_tax_enabled: false,
  },
  stock: { reserve_pending: false, location_id: '' },
  shipping: { use_amazon_buy_shipping: false, default_carrier: '', label_format: 'PDF', seller_fulfilled_prime: false },
  payment_mappings: [],
  shipping_mappings: [],
  inventory_mappings: [],
  inventory_sync: {
    update_inventory: false,
    max_quantity_to_sync: 0,
    min_stock_level: 0,
    latency_buffer_days: 0,
    default_latency_days: 0,
    location_ids: [],
  },
  temu_defaults: {
    fulfillment_type: 1,
    shipment_limit_day: 2,
    shipping_template_id: '',
    origin_region1: '',
    origin_region2: '',
  },
};

// ── Helper components ─────────────────────────────────────────────────────────

function Toggle({ checked, onChange, label, description }: {
  checked: boolean; onChange: (v: boolean) => void; label: string; description?: string;
}) {
  return (
    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: 16, background: 'var(--bg-elevated)', borderRadius: 8, border: '1px solid var(--border)' }}>
      <div>
        <div style={{ fontWeight: 600, fontSize: 14, color: 'var(--text-primary)' }}>{label}</div>
        {description && <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 3, lineHeight: 1.5 }}>{description}</div>}
      </div>
      <button
        onClick={() => onChange(!checked)}
        style={{
          width: 44, height: 24, borderRadius: 12, border: 'none', cursor: 'pointer', flexShrink: 0,
          background: checked ? 'var(--primary)' : 'var(--bg-secondary)',
          position: 'relative', transition: 'background 0.2s',
          outline: '2px solid var(--border)',
        }}
        aria-pressed={checked}
      >
        <div style={{
          position: 'absolute', top: 3, left: checked ? 23 : 3,
          width: 18, height: 18, borderRadius: '50%', background: '#fff',
          transition: 'left 0.2s', boxShadow: '0 1px 3px rgba(0,0,0,0.3)',
        }} />
      </button>
    </div>
  );
}

function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.06em', marginBottom: 8 }}>
      {children}
    </div>
  );
}

function inputStyle(): React.CSSProperties {
  return {
    width: '100%', boxSizing: 'border-box', padding: '9px 12px',
    background: 'var(--bg-elevated)', border: '1px solid var(--border)',
    borderRadius: 8, color: 'var(--text-primary)', fontSize: 13, outline: 'none',
  };
}

function selectStyle(): React.CSSProperties {
  return { ...inputStyle(), cursor: 'pointer' };
}

// ── Product search for inventory mapping ──────────────────────────────────────

function ProductSearchInput({ value, onChange }: { value: string; onChange: (sku: string, productId: string) => void }) {
  const [query, setQuery] = useState(value);
  const [results, setResults] = useState<ProductSearchResult[]>([]);
  const [open, setOpen] = useState(false);

  useEffect(() => {
    if (query.length < 2) { setResults([]); return; }
    const t = setTimeout(async () => {
      try {
        const res = await api(`/search/products?q=${encodeURIComponent(query)}&limit=8`);
        if (res.ok) {
          const d = await res.json();
          setResults(d.products || d.results || []);
        }
      } catch { setResults([]); }
    }, 280);
    return () => clearTimeout(t);
  }, [query]);

  return (
    <div style={{ position: 'relative', flex: 1 }}>
      <input
        value={query}
        onChange={e => { setQuery(e.target.value); setOpen(true); }}
        onFocus={() => setOpen(true)}
        onBlur={() => setTimeout(() => setOpen(false), 180)}
        placeholder="Search internal SKU or title…"
        style={inputStyle()}
      />
      {open && results.length > 0 && (
        <div style={{ position: 'absolute', top: '100%', left: 0, right: 0, background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 8, zIndex: 50, maxHeight: 200, overflowY: 'auto', marginTop: 4, boxShadow: '0 4px 16px rgba(0,0,0,0.25)' }}>
          {results.map(r => (
            <div
              key={r.product_id}
              onMouseDown={() => { setQuery(r.sku); setOpen(false); onChange(r.sku, r.product_id); }}
              style={{ padding: '9px 14px', cursor: 'pointer', borderBottom: '1px solid var(--border)', fontSize: 13 }}
              onMouseEnter={e => (e.currentTarget.style.background = 'var(--bg-elevated)')}
              onMouseLeave={e => (e.currentTarget.style.background = '')}
            >
              <span style={{ fontWeight: 600, color: 'var(--primary)' }}>{r.sku}</span>
              <span style={{ color: 'var(--text-muted)', marginLeft: 8, fontSize: 12 }}>{r.title}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

// ── Tab panels ────────────────────────────────────────────────────────────────

function OrdersTab({ config, onChange, channel }: { config: ChannelConfig; onChange: (c: ChannelConfig) => void; channel: string }) {
  const o = config.orders;
  const set = (patch: Partial<ChannelOrderConfig>) => onChange({ ...config, orders: { ...o, ...patch } });

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <Toggle checked={o.enabled} onChange={v => set({ enabled: v })} label="Automatic Order Sync" description="Automatically download orders from this channel on a schedule" />

      {o.enabled && (
        <>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16 }}>
            <div>
              <SectionLabel>Sync Frequency</SectionLabel>
              <select value={o.frequency_minutes} onChange={e => set({ frequency_minutes: +e.target.value })} style={selectStyle()}>
                <option value={15}>Every 15 minutes</option>
                <option value={30}>Every 30 minutes</option>
                <option value={60}>Every hour</option>
                <option value={360}>Every 6 hours</option>
                <option value={1440}>Once daily</option>
              </select>
            </div>
            <div>
              <SectionLabel>Lookback Window</SectionLabel>
              <select value={o.lookback_hours} onChange={e => set({ lookback_hours: +e.target.value })} style={selectStyle()}>
                <option value={2}>Last 2 hours</option>
                <option value={6}>Last 6 hours</option>
                <option value={24}>Last 24 hours</option>
                <option value={48}>Last 48 hours</option>
                <option value={168}>Last 7 days</option>
              </select>
            </div>
          </div>

          <div>
            <SectionLabel>Order Status to Import</SectionLabel>
            <select value={o.status_filter} onChange={e => set({ status_filter: e.target.value })} style={selectStyle()}>
              <option value="Unshipped">Unshipped only (recommended)</option>
              <option value="Unshipped,Pending">Unshipped + Pending</option>
              <option value="all">All statuses</option>
            </select>
          </div>

          {channel === 'amazon' && (
            <Toggle checked={o.include_fba} onChange={v => set({ include_fba: v })} label="Include FBA Orders" description="Amazon Fulfilled — stock managed by Amazon, no label needed" />
          )}
        </>
      )}

      <div style={{ height: 1, background: 'var(--border)', margin: '4px 0' }} />

      <div>
        <SectionLabel>Order Prefix</SectionLabel>
        <input
          value={o.order_prefix}
          onChange={e => set({ order_prefix: e.target.value })}
          placeholder="e.g. AMZ- or EBAY-"
          style={inputStyle()}
        />
        <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 5 }}>
          Prepended to the channel order number on import (e.g. AMZ-123-4567890). Leave blank to use the channel's original order number.
        </div>
      </div>

      <Toggle
        checked={o.validate_on_download}
        onChange={v => set({ validate_on_download: v })}
        label="Validate on Download"
        description="Run automation rules immediately when each order is imported, rather than waiting for the next automation cycle"
      />

      <Toggle
        checked={o.download_unpaid_orders}
        onChange={v => set({ download_unpaid_orders: v })}
        label="Download Unpaid / Pending Orders"
        description="Also import orders that haven't been paid yet. They will be placed on hold with reason 'Awaiting payment'"
      />

      {o.download_unpaid_orders && (
        <div style={{ marginLeft: 20, borderLeft: '3px solid var(--border)', paddingLeft: 16 }}>
          <Toggle
            checked={o.reserve_unpaid_stock}
            onChange={v => set({ reserve_unpaid_stock: v })}
            label="Reserve Stock for Unpaid Orders"
            description="Hold stock against pending orders so it cannot be allocated to other channels"
          />
        </div>
      )}
    </div>
  );
}

function TaxTab({ config, onChange }: { config: ChannelConfig; onChange: (c: ChannelConfig) => void }) {
  const o = config.orders;
  const set = (patch: Partial<ChannelOrderConfig>) => onChange({ ...config, orders: { ...o, ...patch } });
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <div style={{ padding: 14, background: 'rgba(59,130,246,0.06)', border: '1px solid rgba(59,130,246,0.2)', borderRadius: 8, fontSize: 13, color: 'var(--text-secondary)', lineHeight: 1.6 }}>
        ℹ️ By default MarketMate uses the tax values sent in the channel order payload. Enable the toggle below to mark these orders so tax is <strong>not recalculated</strong> when order totals are processed.
      </div>
      <Toggle
        checked={o.channel_tax_enabled}
        onChange={v => set({ channel_tax_enabled: v })}
        label="Use Channel Tax — Do Not Recalculate"
        description="Trust the tax figures sent by the channel. Orders will be tagged 'channel_tax' and skipped during any tax recalculation steps."
      />
    </div>
  );
}

function StockTab({ config, onChange, locations }: { config: ChannelConfig; onChange: (c: ChannelConfig) => void; locations: WarehouseLocation[] }) {
  const s = config.stock;
  const set = (patch: Partial<ChannelStockConfig>) => onChange({ ...config, stock: { ...s, ...patch } });
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <div>
        <SectionLabel>Default Warehouse Location</SectionLabel>
        <select value={s.location_id || ''} onChange={e => set({ location_id: e.target.value })} style={selectStyle()}>
          <option value="">— No location override —</option>
          {locations.map(l => (
            <option key={l.location_id} value={l.location_id}>{l.name || l.location_id}</option>
          ))}
        </select>
        <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 5 }}>
          Route all orders from this channel to a specific warehouse location. Leave blank to use the platform's default fulfilment source logic.
        </div>
      </div>
      <Toggle
        checked={s.reserve_pending}
        onChange={v => set({ reserve_pending: v })}
        label="Reserve Stock for Pending Orders"
        description="Hold stock against orders in Pending status. Only relevant if you import Pending orders in the Orders tab."
      />
    </div>
  );
}

function PaymentTab({ config, onChange }: { config: ChannelConfig; onChange: (c: ChannelConfig) => void }) {
  const mappings = config.payment_mappings || [];
  const update = (rows: PaymentMapping[]) => onChange({ ...config, payment_mappings: rows });

  const addRow = () => update([...mappings, { channel_method: '', internal_method: 'Bank Transfer' }]);
  const updateRow = (i: number, patch: Partial<PaymentMapping>) => {
    const rows = [...mappings]; rows[i] = { ...rows[i], ...patch }; update(rows);
  };
  const removeRow = (i: number) => update(mappings.filter((_, idx) => idx !== i));

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      <div style={{ fontSize: 13, color: 'var(--text-muted)', lineHeight: 1.6 }}>
        Map the payment method names used by this channel to your internal payment method labels. Used for reporting and payment reconciliation.
      </div>

      {mappings.length > 0 && (
        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
          <thead>
            <tr style={{ borderBottom: '1px solid var(--border)' }}>
              {['Channel Payment Method', 'Internal Method', ''].map(h => (
                <th key={h} style={{ padding: '8px 10px', textAlign: 'left', color: 'var(--text-muted)', fontWeight: 600, fontSize: 11, textTransform: 'uppercase', letterSpacing: '0.05em' }}>{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {mappings.map((row, i) => (
              <tr key={i} style={{ borderBottom: '1px solid var(--border)' }}>
                <td style={{ padding: '8px 6px' }}>
                  <input
                    value={row.channel_method}
                    onChange={e => updateRow(i, { channel_method: e.target.value })}
                    placeholder="e.g. PayPal Express"
                    style={{ ...inputStyle(), margin: 0 }}
                  />
                </td>
                <td style={{ padding: '8px 6px' }}>
                  <select value={row.internal_method} onChange={e => updateRow(i, { internal_method: e.target.value })} style={{ ...selectStyle(), margin: 0 }}>
                    {INTERNAL_PAYMENT_METHODS.map(m => <option key={m}>{m}</option>)}
                  </select>
                </td>
                <td style={{ padding: '8px 6px', textAlign: 'right' }}>
                  <button onClick={() => removeRow(i)} style={{ background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-muted)', fontSize: 16, padding: '2px 6px' }}>✕</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {mappings.length === 0 && (
        <div style={{ textAlign: 'center', padding: 32, color: 'var(--text-muted)', background: 'var(--bg-elevated)', borderRadius: 8, border: '1px dashed var(--border)', fontSize: 13 }}>
          No payment mappings configured. Add a mapping to translate channel payment methods to internal labels.
        </div>
      )}

      <button
        onClick={addRow}
        style={{ alignSelf: 'flex-start', padding: '7px 14px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-secondary)', fontSize: 13, cursor: 'pointer', fontWeight: 600 }}
      >
        + Add Mapping
      </button>
    </div>
  );
}

function ShippingTab({ config, onChange, carriers }: { config: ChannelConfig; onChange: (c: ChannelConfig) => void; carriers: Carrier[] }) {
  const mappings = config.shipping_mappings || [];
  const update = (rows: ShippingMapping[]) => onChange({ ...config, shipping_mappings: rows });

  const addRow = () => update([...mappings, { channel_service: '', internal_carrier_id: '', internal_service_code: '' }]);
  const updateRow = (i: number, patch: Partial<ShippingMapping>) => {
    const rows = [...mappings]; rows[i] = { ...rows[i], ...patch }; update(rows);
  };
  const removeRow = (i: number) => update(mappings.filter((_, idx) => idx !== i));

  const getServices = (carrierId: string): CarrierService[] => {
    return carriers.find(c => c.carrier_id === carrierId)?.services || [];
  };

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      <div style={{ fontSize: 13, color: 'var(--text-muted)', lineHeight: 1.6 }}>
        Map the shipping service names used by this channel to your internal carrier and service codes. Used when auto-selecting a carrier for dispatch.
      </div>

      {mappings.length > 0 && (
        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
          <thead>
            <tr style={{ borderBottom: '1px solid var(--border)' }}>
              {['Channel Service Name', 'Internal Carrier', 'Service Code', ''].map(h => (
                <th key={h} style={{ padding: '8px 10px', textAlign: 'left', color: 'var(--text-muted)', fontWeight: 600, fontSize: 11, textTransform: 'uppercase', letterSpacing: '0.05em' }}>{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {mappings.map((row, i) => {
              const services = getServices(row.internal_carrier_id);
              return (
                <tr key={i} style={{ borderBottom: '1px solid var(--border)' }}>
                  <td style={{ padding: '8px 6px' }}>
                    <input
                      value={row.channel_service}
                      onChange={e => updateRow(i, { channel_service: e.target.value })}
                      placeholder="e.g. Standard Shipping"
                      style={{ ...inputStyle(), margin: 0 }}
                    />
                  </td>
                  <td style={{ padding: '8px 6px' }}>
                    <select
                      value={row.internal_carrier_id}
                      onChange={e => updateRow(i, { internal_carrier_id: e.target.value, internal_service_code: '' })}
                      style={{ ...selectStyle(), margin: 0 }}
                    >
                      <option value="">— Select carrier —</option>
                      {carriers.map(c => <option key={c.carrier_id} value={c.carrier_id}>{c.name}</option>)}
                    </select>
                  </td>
                  <td style={{ padding: '8px 6px' }}>
                    {services.length > 0 ? (
                      <select value={row.internal_service_code} onChange={e => updateRow(i, { internal_service_code: e.target.value })} style={{ ...selectStyle(), margin: 0 }}>
                        <option value="">— Select service —</option>
                        {services.map(s => <option key={s.service_code} value={s.service_code}>{s.name}</option>)}
                      </select>
                    ) : (
                      <input
                        value={row.internal_service_code}
                        onChange={e => updateRow(i, { internal_service_code: e.target.value })}
                        placeholder="Service code"
                        style={{ ...inputStyle(), margin: 0 }}
                      />
                    )}
                  </td>
                  <td style={{ padding: '8px 6px', textAlign: 'right' }}>
                    <button onClick={() => removeRow(i)} style={{ background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-muted)', fontSize: 16, padding: '2px 6px' }}>✕</button>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}

      {mappings.length === 0 && (
        <div style={{ textAlign: 'center', padding: 32, color: 'var(--text-muted)', background: 'var(--bg-elevated)', borderRadius: 8, border: '1px dashed var(--border)', fontSize: 13 }}>
          No shipping mappings configured. Add a mapping to link channel shipping services to internal carriers.
        </div>
      )}

      <button
        onClick={addRow}
        style={{ alignSelf: 'flex-start', padding: '7px 14px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-secondary)', fontSize: 13, cursor: 'pointer', fontWeight: 600 }}
      >
        + Add Mapping
      </button>
    </div>
  );
}

function InventoryTab({ config, onChange, credentialId }: { config: ChannelConfig; onChange: (c: ChannelConfig) => void; credentialId?: string }) {
  const navigate = useNavigate();
  const mappings = config.inventory_mappings || [];
  const update = (rows: InventoryMapping[]) => onChange({ ...config, inventory_mappings: rows });

  const addRow = () => update([...mappings, { channel_sku: '', internal_sku: '', product_id: '' }]);
  const updateRow = (i: number, patch: Partial<InventoryMapping>) => {
    const rows = [...mappings]; rows[i] = { ...rows[i], ...patch }; update(rows);
  };
  const removeRow = (i: number) => update(mappings.filter((_, idx) => idx !== i));

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      {/* SKU Reconciliation quick-link */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '12px 16px', background: 'var(--bg-primary)', borderRadius: 10, border: '1px solid var(--border)' }}>
        <div>
          <div style={{ fontWeight: 600, fontSize: 14, marginBottom: 2 }}>🔗 SKU Reconciliation</div>
          <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>Auto-match channel SKUs to your internal catalogue. Full matches apply instantly; partial matches need approval.</div>
        </div>
        {credentialId && (
          <button onClick={() => navigate(`/marketplace/channels/${credentialId}/reconcile`)} style={{ marginLeft: 16, padding: '8px 16px', borderRadius: 8, border: 'none', background: 'var(--primary, #6366f1)', color: '#fff', fontWeight: 600, fontSize: 13, cursor: 'pointer', whiteSpace: 'nowrap' }}>
            Open Reconciliation
          </button>
        )}
      </div>

      <div style={{ fontSize: 13, color: 'var(--text-muted)', lineHeight: 1.6 }}>
        Map channel SKUs to internal platform SKUs. Used when the channel uses different product identifiers from your internal catalogue.
      </div>

      {mappings.length > 0 && (
        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
          <thead>
            <tr style={{ borderBottom: '1px solid var(--border)' }}>
              {['Channel SKU', 'Internal SKU / Product', ''].map(h => (
                <th key={h} style={{ padding: '8px 10px', textAlign: 'left', color: 'var(--text-muted)', fontWeight: 600, fontSize: 11, textTransform: 'uppercase', letterSpacing: '0.05em' }}>{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {mappings.map((row, i) => (
              <tr key={i} style={{ borderBottom: '1px solid var(--border)' }}>
                <td style={{ padding: '8px 6px', width: '40%' }}>
                  <input
                    value={row.channel_sku}
                    onChange={e => updateRow(i, { channel_sku: e.target.value })}
                    placeholder="Channel SKU"
                    style={{ ...inputStyle(), margin: 0 }}
                  />
                </td>
                <td style={{ padding: '8px 6px' }}>
                  <ProductSearchInput
                    value={row.internal_sku}
                    onChange={(sku, productId) => updateRow(i, { internal_sku: sku, product_id: productId })}
                  />
                </td>
                <td style={{ padding: '8px 6px', textAlign: 'right', width: 40 }}>
                  <button onClick={() => removeRow(i)} style={{ background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-muted)', fontSize: 16, padding: '2px 6px' }}>✕</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {mappings.length === 0 && (
        <div style={{ textAlign: 'center', padding: 32, color: 'var(--text-muted)', background: 'var(--bg-elevated)', borderRadius: 8, border: '1px dashed var(--border)', fontSize: 13 }}>
          No inventory mappings configured. Add a mapping to link channel SKUs to internal products.
        </div>
      )}

      <button
        onClick={addRow}
        style={{ alignSelf: 'flex-start', padding: '7px 14px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-secondary)', fontSize: 13, cursor: 'pointer', fontWeight: 600 }}
      >
        + Add Mapping
      </button>
    </div>
  );
}

function NotificationsTab({ config, onChange }: { config: ChannelConfig; onChange: (c: ChannelConfig) => void }) {
  const o = config.orders;
  const set = (patch: Partial<ChannelOrderConfig>) => onChange({ ...config, orders: { ...o, ...patch } });
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <div style={{ fontSize: 13, color: 'var(--text-muted)', lineHeight: 1.6 }}>
        Control whether MarketMate sends notification notes back to the channel when orders are processed.
      </div>
      <Toggle
        checked={o.dispatch_notes_enabled}
        onChange={v => set({ dispatch_notes_enabled: v })}
        label="Send Dispatch Notes"
        description="Notify the channel when an order is despatched (tracking number + carrier)"
      />
      <Toggle
        checked={o.refund_notes_enabled}
        onChange={v => set({ refund_notes_enabled: v })}
        label="Send Refund Notes"
        description="Notify the channel when a refund or RMA is processed"
      />
      <Toggle
        checked={o.cancellation_notes_enabled}
        onChange={v => set({ cancellation_notes_enabled: v })}
        label="Send Cancellation Notes"
        description="Notify the channel when an order is cancelled"
      />
    </div>
  );
}

// ============================================================================
// TEMU BRAND MAPPING TAB
// ============================================================================

interface ProductBrand { name: string; }
interface TemuBrandOption { brandId: number; brandName: string; trademarkId: number; trademarkBizId: number; }
interface BrandMappingEntry {
  productBrand: string;
  temuBrandId: number;
  temuBrandName: string;
  trademarkId: number;
  trademarkBizId: number;
}

function fuzzyMatch(productBrand: string, temuBrands: TemuBrandOption[]): TemuBrandOption | null {
  const norm = (s: string) => s.toLowerCase().replace(/[^a-z0-9]/g, '');
  const pb = norm(productBrand);
  // Exact normalised match first
  const exact = temuBrands.find(t => norm(t.brandName) === pb);
  if (exact) return exact;
  // Contains match
  const contains = temuBrands.find(t => {
    const tb = norm(t.brandName);
    return tb && (pb.includes(tb) || tb.includes(pb));
  });
  return contains ?? null;
}

function TemuBrandMappingTab({ credentialId }: { credentialId?: string }) {
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [savedOk, setSavedOk] = useState(false);
  const [importing, setImporting] = useState(false);
  const [error, setError] = useState('');

  const [productBrands, setProductBrands] = useState<string[]>([]);
  const [temuBrands, setTemuBrands] = useState<TemuBrandOption[]>([]);
  const [mappings, setMappings] = useState<Record<string, BrandMappingEntry>>({});

  // Load on mount
  useEffect(() => {
    const qs = credentialId ? `?credential_id=${credentialId}` : '';
    api(`/temu/brand-mappings${qs}`)
      .then(r => r.json())
      .then(d => {
        if (!d.ok) { setError(d.error || 'Failed to load'); return; }
        setProductBrands(d.productBrands || []);
        setTemuBrands((d.temuBrands || []).sort((a: TemuBrandOption, b: TemuBrandOption) =>
          a.brandName.localeCompare(b.brandName)));

        // Build mapping lookup + auto-fuzzy-match unmapped brands
        const existing: Record<string, BrandMappingEntry> = {};
        for (const m of (d.mappings || [])) {
          existing[m.productBrand.toLowerCase()] = m;
        }
        // Auto-match brands not yet mapped
        const autoFilled = { ...existing };
        for (const pb of (d.productBrands || [])) {
          const key = pb.toLowerCase();
          if (!autoFilled[key]) {
            const match = fuzzyMatch(pb, d.temuBrands || []);
            if (match) {
              autoFilled[key] = {
                productBrand: pb,
                temuBrandId: match.brandId,
                temuBrandName: match.brandName,
                trademarkId: match.trademarkId,
                trademarkBizId: match.trademarkBizId,
              };
            }
          }
        }
        setMappings(autoFilled);
      })
      .catch(e => setError(e.message))
      .finally(() => setLoading(false));
  }, [credentialId]);

  const setMapping = (productBrand: string, temuBrandId: string) => {
    const key = productBrand.toLowerCase();
    if (!temuBrandId) {
      setMappings(prev => { const n = { ...prev }; delete n[key]; return n; });
      return;
    }
    const tb = temuBrands.find(b => String(b.brandId) === temuBrandId);
    if (!tb) return;
    setMappings(prev => ({
      ...prev,
      [key]: {
        productBrand,
        temuBrandId: tb.brandId,
        temuBrandName: tb.brandName,
        trademarkId: tb.trademarkId,
        trademarkBizId: tb.trademarkBizId,
      },
    }));
  };

  const save = async () => {
    setSaving(true); setSavedOk(false); setError('');
    const qs = credentialId ? `?credential_id=${credentialId}` : '';
    try {
      const res = await api(`/temu/brand-mappings${qs}`, {
        method: 'PUT',
        body: JSON.stringify({ mappings: Object.values(mappings) }),
      });
      const d = await res.json();
      if (!d.ok) throw new Error(d.error || 'Save failed');
      setSavedOk(true);
      setTimeout(() => setSavedOk(false), 3000);
    } catch (e: any) {
      setError(e.message);
    } finally { setSaving(false); }
  };

  const exportXlsx = async () => {
    try {
      const qs = credentialId ? `?credential_id=${credentialId}` : '';
      let token = '';
      try {
        const user = auth.currentUser;
        if (user) token = await user.getIdToken();
      } catch { /* non-fatal */ }
      const res = await fetch(`${API_BASE}/temu/brand-mappings/export${qs}`, {
        headers: {
          'X-Tenant-Id': getActiveTenantId(),
          ...(token ? { 'Authorization': `Bearer ${token}` } : {}),
        },
      });
      if (!res.ok) { setError('Export failed'); return; }
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = 'temu_brand_mapping.xlsx';
      a.click();
      URL.revokeObjectURL(url);
    } catch (e: any) {
      setError('Export failed: ' + e.message);
    }
  };

  const importXlsx = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    setImporting(true); setError('');
    try {
      const qs = credentialId ? `?credential_id=${credentialId}` : '';
      const formData = new FormData();
      formData.append('file', file);

      // Get Firebase token — multipart needs no Content-Type (browser sets it with boundary)
      let token = '';
      try {
        const user = auth.currentUser;
        if (user) token = await user.getIdToken();
      } catch { /* non-fatal */ }

      const res = await fetch(`${API_BASE}/temu/brand-mappings/import${qs}`, {
        method: 'POST',
        headers: {
          'X-Tenant-Id': getActiveTenantId(),
          ...(token ? { 'Authorization': `Bearer ${token}` } : {}),
        },
        body: formData,
      });
      const d = await res.json();
      if (!d.ok) throw new Error(d.error || 'Import failed');
      // Reload mappings after successful import
      const rel = await api(`/temu/brand-mappings${qs}`).then(r => r.json());
      if (rel.ok) {
        const reloaded: Record<string, BrandMappingEntry> = {};
        for (const m of (rel.mappings || [])) {
          reloaded[m.productBrand.toLowerCase()] = m;
        }
        setMappings(reloaded);
      }
    } catch (e: any) {
      setError('Import failed: ' + e.message);
    } finally {
      setImporting(false);
      e.target.value = '';
    }
  };

  // Count unique uppercase groups for display
  const uniqueGroupCount = [...new Set(productBrands.map(pb => pb.toUpperCase()))].length;
  const mappedGroupCount = [...new Set(productBrands.map(pb => pb.toUpperCase()))].filter(upper => {
    const variants = productBrands.filter(pb => pb.toUpperCase() === upper);
    return variants.some(v => mappings[v.toLowerCase()]);
  }).length;

  const lbl: React.CSSProperties = {
    display: 'block', fontSize: 11, fontWeight: 700, color: 'var(--text-muted)',
    textTransform: 'uppercase', letterSpacing: '0.5px', marginBottom: 4,
  };
  const inp: React.CSSProperties = {
    width: '100%', padding: '7px 10px', borderRadius: 6,
    border: '1px solid var(--border)', background: 'var(--bg-primary)',
    color: 'var(--text-primary)', fontSize: 13, outline: 'none',
  };

  if (loading) return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 10, color: 'var(--text-muted)', padding: 24 }}>
      <div className="spinner" style={{ width: 18, height: 18 }} /> Loading brands…
    </div>
  );

  return (
    <div>
      {/* Description */}
      <div style={{ fontSize: 13, color: 'var(--text-muted)', lineHeight: 1.6, marginBottom: 16 }}>
        Map your product catalogue brands to authorised Temu brands. Used when creating Temu listings in bulk.
        Fuzzy-matched suggestions are pre-filled automatically — review and correct before saving.
      </div>

      {error && (
        <div style={{ padding: '10px 14px', borderRadius: 8, background: 'var(--danger-glow)', border: '1px solid var(--danger)', color: 'var(--danger)', fontSize: 13, marginBottom: 16 }}>
          ⚠ {error}
        </div>
      )}

      {/* Toolbar */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 16, flexWrap: 'wrap' }}>
        <div style={{ fontSize: 13, color: 'var(--text-muted)', flex: 1 }}>
          <span style={{ fontWeight: 700, color: 'var(--text-primary)' }}>{mappedGroupCount}</span> of{' '}
          <span style={{ fontWeight: 700, color: 'var(--text-primary)' }}>{uniqueGroupCount}</span> brands mapped
          {temuBrands.length === 0 && (
            <span style={{ marginLeft: 8, color: 'var(--danger)', fontSize: 12 }}>
              ⚠ No Temu brands found — check your Temu connection
            </span>
          )}
        </div>

        {/* Export */}
        <button onClick={exportXlsx} style={{ padding: '7px 14px', borderRadius: 6, border: '1px solid var(--border)', background: 'var(--bg-tertiary)', color: 'var(--text-secondary)', fontSize: 12, cursor: 'pointer', fontWeight: 600, display: 'flex', alignItems: 'center', gap: 6 }}>
          📥 Edit in Spreadsheet
        </button>

        {/* Import */}
        <label style={{ padding: '7px 14px', borderRadius: 6, border: '1px solid var(--border)', background: importing ? 'var(--border)' : 'var(--bg-tertiary)', color: 'var(--text-secondary)', fontSize: 12, cursor: 'pointer', fontWeight: 600, display: 'flex', alignItems: 'center', gap: 6 }}>
          {importing ? '⏳ Importing…' : '📤 Import Spreadsheet'}
          <input type="file" accept=".xlsx" onChange={importXlsx} style={{ display: 'none' }} />
        </label>

        {/* Save */}
        <button
          onClick={save}
          disabled={saving}
          style={{ padding: '7px 18px', borderRadius: 6, border: 'none', background: savedOk ? '#16a34a' : 'var(--primary)', color: '#fff', fontSize: 12, cursor: saving ? 'not-allowed' : 'pointer', fontWeight: 700, opacity: saving ? 0.7 : 1 }}
        >
          {saving ? '⏳ Saving…' : savedOk ? '✅ Saved!' : '💾 Save Mappings'}
        </button>
      </div>

      {/* Mapping table */}
      {productBrands.length === 0 ? (
        <div style={{ textAlign: 'center', padding: '40px 24px', color: 'var(--text-muted)', fontSize: 14 }}>
          No product brands found in your catalogue. Ensure products have a <code>brand</code> field set.
        </div>
      ) : (() => {
        // Group brands by uppercase — e.g. "Adder", "ADDER", "adder" → one row
        const groups: { canonical: string; variants: string[] }[] = [];
        const seen: Record<string, number> = {}; // uppercase → group index
        for (const pb of productBrands) {
          const upper = pb.toUpperCase();
          if (seen[upper] === undefined) {
            seen[upper] = groups.length;
            groups.push({ canonical: pb, variants: [pb] });
          } else {
            groups[seen[upper]].variants.push(pb);
          }
        }

        return (
          <div style={{ border: '1px solid var(--border)', borderRadius: 8, overflow: 'hidden' }}>
            {/* Header */}
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 0, background: 'var(--bg-tertiary)', borderBottom: '1px solid var(--border)', padding: '8px 16px' }}>
              <div style={lbl}>Product Brand</div>
              <div style={lbl}>Temu Brand</div>
            </div>

            {/* Rows — one per uppercase group */}
            <div style={{ maxHeight: 480, overflowY: 'auto' }}>
              {groups.map((group, i) => {
                // Use the first variant's lowercase key to look up the mapping,
                // but check all variant keys so a saved mapping on any variant is found
                const mappingKey = group.variants.map(v => v.toLowerCase()).find(k => mappings[k]) ?? group.variants[0].toLowerCase();
                const mapped = mappings[mappingKey];
                const isLast = i === groups.length - 1;

                return (
                  <div
                    key={group.canonical}
                    style={{
                      display: 'grid', gridTemplateColumns: '1fr 1fr', alignItems: 'center',
                      padding: '8px 16px', gap: 12,
                      borderBottom: isLast ? 'none' : '1px solid var(--border)',
                      background: mapped ? 'transparent' : 'rgba(239,68,68,0.04)',
                    }}
                  >
                    {/* Product brand — read only */}
                    <div style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 }}>
                      <span style={{ fontSize: 13, color: 'var(--text-primary)', fontWeight: 500, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                        {group.canonical}
                      </span>
                    </div>

                    {/* Temu brand dropdown — explicit colours so text is readable in dark mode */}
                    <select
                      value={mapped ? String(mapped.temuBrandId) : ''}
                      onChange={e => {
                        // Apply the mapping to ALL variants in the group
                        group.variants.forEach(v => setMapping(v, e.target.value));
                      }}
                      style={{
                        ...inp,
                        background: mapped ? 'var(--bg-elevated, #1e2130)' : 'rgba(239,68,68,0.06)',
                        borderColor: mapped ? 'var(--border)' : 'rgba(239,68,68,0.3)',
                        color: 'var(--text-primary)',
                        colorScheme: 'dark',
                        fontSize: 12,
                      }}
                    >
                      <option value="" style={{ background: '#1e2130', color: '#e2e8f0' }}>— Not mapped —</option>
                      {temuBrands.map(tb => (
                        <option key={tb.brandId} value={String(tb.brandId)} style={{ background: '#1e2130', color: '#e2e8f0' }}>
                          {tb.brandName}
                        </option>
                      ))}
                    </select>
                  </div>
                );
              })}
            </div>
          </div>
        );
      })()}
    </div>
  );
}

// ============================================================================
// TEMU LISTING DEFAULTS TAB
// ============================================================================
// Allows setting default values for Fulfilment & Shipping fields on the
// Temu listing form. Values are stored in config.temu_defaults and read
// by TemuListingCreate on load to pre-populate the form.

const CHINA_PROVINCES_CFG = [
  'Anhui','Beijing','Chongqing','Fujian','Gansu','Guangdong','Guangxi','Guizhou',
  'Hainan','Hebei','Heilongjiang','Henan','Hubei','Hunan','Inner Mongolia',
  'Jiangsu','Jiangxi','Jilin','Liaoning','Ningxia','Qinghai','Shaanxi',
  'Shandong','Shanghai','Shanxi','Sichuan','Tianjin','Tibet','Xinjiang',
  'Yunnan','Zhejiang',
];

interface ShippingTemplate { templateId: string; templateName: string; }

function TemuDefaultsTab({ config, onChange, credentialId }: {
  config: ChannelConfig;
  onChange: (c: ChannelConfig) => void;
  credentialId?: string;
}) {
  const defaults = config.temu_defaults ?? {
    fulfillment_type: 1, shipment_limit_day: 2,
    shipping_template_id: '', origin_region1: '', origin_region2: '',
  };
  const set = (patch: Partial<TemuDefaultsConfig>) =>
    onChange({ ...config, temu_defaults: { ...defaults, ...patch } });

  const [templates, setTemplates] = useState<ShippingTemplate[]>([]);
  const [templatesLoading, setTemplatesLoading] = useState(true);

  useEffect(() => {
    const url = credentialId
      ? `/temu/shipping-templates?credential_id=${credentialId}`
      : '/temu/shipping-templates';
    api(url)
      .then(r => r.json())
      .then(d => { if (d.ok) setTemplates(d.templates || []); })
      .catch(() => {})
      .finally(() => setTemplatesLoading(false));
  }, [credentialId]);

  const field: React.CSSProperties = { marginBottom: 16 };
  const lbl: React.CSSProperties = {
    display: 'block', fontSize: 11, fontWeight: 700, color: 'var(--text-muted)',
    textTransform: 'uppercase', letterSpacing: '0.5px', marginBottom: 6,
  };
  const inp: React.CSSProperties = {
    width: '100%', padding: '9px 12px', borderRadius: 8,
    border: '1px solid var(--border)', background: 'var(--bg-primary)',
    color: 'var(--text-primary)', fontSize: 13, outline: 'none',
  };

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 0 }}>
      <div style={{ fontSize: 13, color: 'var(--text-muted)', lineHeight: 1.6, marginBottom: 20 }}>
        Set default values for the Fulfilment &amp; Shipping section on the Temu listing form.
        These will be pre-filled each time you open the listing page — you can still override them per listing.
      </div>

      {/* Grid of fields */}
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0 24px' }}>

        <div style={field}>
          <label style={lbl}>Fulfilment Type</label>
          <select value={defaults.fulfillment_type} onChange={e => set({ fulfillment_type: parseInt(e.target.value) })} style={inp}>
            <option value={1}>Merchant Fulfilled</option>
            <option value={2}>Temu Fulfilled</option>
          </select>
        </div>

        <div style={field}>
          <label style={lbl}>Shipment Limit (days)</label>
          <input
            type="number" min={1} max={30}
            value={defaults.shipment_limit_day}
            onChange={e => set({ shipment_limit_day: parseInt(e.target.value) || 2 })}
            style={inp}
          />
          <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 4 }}>
            How many days to dispatch after order received
          </div>
        </div>

        <div style={{ ...field, gridColumn: '1 / -1' }}>
          <label style={lbl}>Default Shipping Profile</label>
          <select
            value={defaults.shipping_template_id}
            onChange={e => set({ shipping_template_id: e.target.value })}
            style={inp}
            disabled={templatesLoading}
          >
            <option value="">
              {templatesLoading ? 'Loading templates…' : '— No default (select per listing) —'}
            </option>
            {templates.map(t => (
              <option key={t.templateId} value={t.templateId}>{t.templateName}</option>
            ))}
          </select>
          {!templatesLoading && templates.length === 0 && (
            <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 4 }}>
              No shipping templates found — check your Temu connection
            </div>
          )}
        </div>

        <div style={field}>
          <label style={lbl}>Country / Region of Origin</label>
          <select
            value={defaults.origin_region1}
            onChange={e => set({ origin_region1: e.target.value, origin_region2: e.target.value !== 'Mainland China' ? '' : defaults.origin_region2 })}
            style={inp}
          >
            <option value="">— No default —</option>
            <option value="Mainland China">Mainland China</option>
            <option value="Hong Kong">Hong Kong</option>
            <option value="Taiwan">Taiwan</option>
            <option value="United Kingdom">United Kingdom</option>
            <option value="United States">United States</option>
            <option value="Germany">Germany</option>
            <option value="Japan">Japan</option>
            <option value="South Korea">South Korea</option>
            <option value="India">India</option>
            <option value="Vietnam">Vietnam</option>
            <option value="Thailand">Thailand</option>
            <option value="Other">Other</option>
          </select>
        </div>

        {defaults.origin_region1 === 'Mainland China' && (
          <div style={field}>
            <label style={lbl}>Province</label>
            <select
              value={defaults.origin_region2}
              onChange={e => set({ origin_region2: e.target.value })}
              style={inp}
            >
              <option value="">— Select province —</option>
              {CHINA_PROVINCES_CFG.map(p => <option key={p} value={p}>{p}</option>)}
            </select>
          </div>
        )}

      </div>
    </div>
  );
}

function InventorySyncTab({ config, onChange, locations }: { config: ChannelConfig; onChange: (c: ChannelConfig) => void; locations: WarehouseLocation[] }) {
  const inv = config.inventory_sync;
  const set = (patch: Partial<ChannelInventorySyncConfig>) =>
    onChange({ ...config, inventory_sync: { ...inv, ...patch } });

  const toggleLocation = (locId: string) => {
    const ids = inv.location_ids || [];
    const next = ids.includes(locId) ? ids.filter(id => id !== locId) : [...ids, locId];
    set({ location_ids: next });
  };

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <div style={{ padding: 14, background: 'rgba(59,130,246,0.06)', border: '1px solid rgba(59,130,246,0.2)', borderRadius: 8, fontSize: 13, color: 'var(--text-secondary)', lineHeight: 1.6 }}>
        🔄 Configure how stock levels are pushed to this channel. These settings override the global inventory sync defaults for this channel only.
      </div>

      <Toggle
        checked={inv.update_inventory}
        onChange={v => set({ update_inventory: v })}
        label="Update Inventory on this Channel"
        description="Enable stock level pushes to this channel. When disabled, no stock updates will be sent regardless of global settings."
      />

      {inv.update_inventory && (
        <>
          <div style={{ height: 1, background: 'var(--border)' }} />

          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16 }}>
            <div>
              <SectionLabel>Max Quantity to Sync</SectionLabel>
              <input
                type="number"
                min={0}
                value={inv.max_quantity_to_sync}
                onChange={e => set({ max_quantity_to_sync: Math.max(0, parseInt(e.target.value) || 0) })}
                style={inputStyle()}
              />
              <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 5 }}>
                Cap the quantity pushed to this channel. Set to 0 for no cap.
              </div>
            </div>

            <div>
              <SectionLabel>Min Stock Level</SectionLabel>
              <input
                type="number"
                min={0}
                value={inv.min_stock_level}
                onChange={e => set({ min_stock_level: Math.max(0, parseInt(e.target.value) || 0) })}
                style={inputStyle()}
              />
              <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 5 }}>
                Never push stock below this level. Acts as a safety buffer.
              </div>
            </div>

            <div>
              <SectionLabel>Latency Buffer (days)</SectionLabel>
              <input
                type="number"
                min={0}
                value={inv.latency_buffer_days}
                onChange={e => set({ latency_buffer_days: Math.max(0, parseInt(e.target.value) || 0) })}
                style={inputStyle()}
              />
              <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 5 }}>
                Reduce available stock by this many days of sales velocity. Prevents overselling during lead times.
              </div>
            </div>

            <div>
              <SectionLabel>Default Latency Buffer (days)</SectionLabel>
              <input
                type="number"
                min={0}
                value={inv.default_latency_days}
                onChange={e => set({ default_latency_days: Math.max(0, parseInt(e.target.value) || 0) })}
                style={inputStyle()}
              />
              <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 5 }}>
                General buffer applied to all channels unless a channel-specific buffer overrides it.
              </div>
            </div>
          </div>

          <div style={{ height: 1, background: 'var(--border)' }} />

          <div>
            <SectionLabel>Multi-Location Inventory</SectionLabel>
            <div style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 12, lineHeight: 1.5 }}>
              Select which warehouse locations contribute stock for this channel. Leave all unchecked to use all locations.
            </div>
            {locations.length === 0 ? (
              <div style={{ padding: 16, background: 'var(--bg-elevated)', borderRadius: 8, border: '1px dashed var(--border)', color: 'var(--text-muted)', fontSize: 13, textAlign: 'center' }}>
                No warehouse locations configured. Add locations in Warehouse Settings.
              </div>
            ) : (
              <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                {locations.map(loc => {
                  const selected = (inv.location_ids || []).includes(loc.location_id);
                  return (
                    <label key={loc.location_id} style={{
                      display: 'flex', alignItems: 'center', gap: 12, padding: '10px 14px',
                      background: selected ? 'var(--primary-glow)' : 'var(--bg-elevated)',
                      border: `1px solid ${selected ? 'var(--primary)' : 'var(--border)'}`,
                      borderRadius: 8, cursor: 'pointer', transition: 'all 0.15s',
                    }}>
                      <input
                        type="checkbox"
                        checked={selected}
                        onChange={() => toggleLocation(loc.location_id)}
                        style={{ width: 16, height: 16, accentColor: 'var(--primary)' }}
                      />
                      <div>
                        <div style={{ fontWeight: 600, fontSize: 13, color: selected ? 'var(--primary)' : 'var(--text-primary)' }}>
                          {loc.name || loc.location_id}
                        </div>
                        {loc.path && <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 2 }}>{loc.path}</div>}
                      </div>
                    </label>
                  );
                })}
              </div>
            )}
          </div>
        </>
      )}
    </div>
  );
}

// ── Main Component ────────────────────────────────────────────────────────────

export default function ChannelConfig() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();

  const [credential, setCredential] = useState<Credential | null>(null);
  const [config, setConfig] = useState<ChannelConfig>(DEFAULT_CONFIG);
  const [tab, setTab] = useState<TabId>('orders');

  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [savedOk, setSavedOk] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [saveError, setSaveError] = useState<string | null>(null);

  // Reference data
  const [locations, setLocations] = useState<WarehouseLocation[]>([]);
  const [carriers, setCarriers] = useState<Carrier[]>([]);

  const load = useCallback(async () => {
    if (!id) return;
    setLoading(true);
    setError(null);
    try {
      const [credRes, cfgRes, locRes, carRes] = await Promise.allSettled([
        api(`/marketplace/credentials/${id}`),
        api(`/marketplace/credentials/${id}/config`),
        api('/locations?limit=100'),
        api('/dispatch/carriers'),
      ]);

      if (credRes.status === 'fulfilled' && credRes.value.ok) {
        const d = await credRes.value.json();
        setCredential(d.data || d.credential || d);
      }
      if (cfgRes.status === 'fulfilled' && cfgRes.value.ok) {
        const d = await cfgRes.value.json();
        const rawCfg = d.data || {};
        // Merge with defaults so new fields have safe values
        setConfig({
          ...DEFAULT_CONFIG,
          ...rawCfg,
          orders: { ...DEFAULT_CONFIG.orders, ...(rawCfg.orders || {}) },
          stock: { ...DEFAULT_CONFIG.stock, ...(rawCfg.stock || {}) },
          shipping: { ...DEFAULT_CONFIG.shipping, ...(rawCfg.shipping || {}) },
          payment_mappings: rawCfg.payment_mappings || [],
          shipping_mappings: rawCfg.shipping_mappings || [],
          inventory_mappings: rawCfg.inventory_mappings || [],
          inventory_sync: { ...DEFAULT_CONFIG.inventory_sync, ...(rawCfg.inventory_sync || {}) },
        });
      }
      if (locRes.status === 'fulfilled' && locRes.value.ok) {
        const d = await locRes.value.json();
        setLocations(d.locations || d.data || []);
      }
      if (carRes.status === 'fulfilled' && carRes.value.ok) {
        const d = await carRes.value.json();
        setCarriers(d.carriers || d.data || []);
      }
    } catch (e: any) {
      setError(e.message || 'Failed to load configuration');
    } finally {
      setLoading(false);
    }
  }, [id]);

  useEffect(() => { load(); }, [load]);

  const save = async () => {
    if (!id) return;
    setSaving(true);
    setSaveError(null);
    try {
      const res = await api(`/marketplace/credentials/${id}/config`, {
        method: 'PATCH',
        body: JSON.stringify(config),
      });
      if (!res.ok) {
        const d = await res.json().catch(() => ({}));
        throw new Error(d.error || `HTTP ${res.status}`);
      }
      setSavedOk(true);
      setTimeout(() => setSavedOk(false), 3000);
    } catch (e: any) {
      setSaveError(e.message || 'Failed to save');
    } finally {
      setSaving(false);
    }
  };

  const channelName = credential?.channel ?? 'Channel';
  const accountName = credential?.account_name ?? id ?? '…';

  if (loading) {
    return (
      <div style={{ padding: '48px 28px', textAlign: 'center', color: 'var(--text-muted)' }}>
        <div className="spinner" style={{ margin: '0 auto 16px' }} />
        Loading channel configuration…
      </div>
    );
  }

  if (error) {
    return (
      <div style={{ padding: '48px 28px', textAlign: 'center' }}>
        <div style={{ fontSize: 40, marginBottom: 12 }}>⚠️</div>
        <div style={{ color: '#f87171', fontWeight: 600, marginBottom: 8 }}>Failed to load</div>
        <div style={{ color: 'var(--text-muted)', fontSize: 13, marginBottom: 20 }}>{error}</div>
        <button onClick={load} style={{ padding: '8px 18px', background: 'var(--primary)', border: 'none', borderRadius: 8, color: '#fff', cursor: 'pointer', fontSize: 13 }}>Retry</button>
      </div>
    );
  }

  return (
    <div style={{ maxWidth: 1100, margin: '0 auto', padding: '24px 28px' }}>
      {/* Page header */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 28 }}>
        <button
          onClick={() => navigate('/marketplace/connections')}
          style={{ background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-muted)', padding: 0, fontSize: 13, display: 'flex', alignItems: 'center', gap: 4 }}
        >
          ← Back
        </button>
        <span style={{ color: 'var(--border)' }}>›</span>
        <span style={{ fontSize: 24 }}>{CHANNEL_EMOJI[channelName] || '🌐'}</span>
        <div>
          <h1 style={{ fontSize: 20, fontWeight: 700, color: 'var(--text-primary)', margin: 0 }}>{accountName}</h1>
          <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 2, textTransform: 'capitalize' }}>
            {channelName} · {credential?.environment || 'production'} · Channel Configuration
          </div>
        </div>
      </div>

      {/* Main card — left sidebar + right content */}
      <div style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 12, overflow: 'hidden', display: 'flex', minHeight: 560 }}>

        {/* Left nav sidebar */}
        <div style={{
          width: 200, flexShrink: 0,
          borderRight: '1px solid var(--border)',
          background: 'var(--bg-tertiary)',
          display: 'flex', flexDirection: 'column',
          padding: '8px 0',
        }}>
          {TABS.filter(t =>
            (t.id !== 'temu_defaults' && t.id !== 'brand_mapping') ||
            channelName === 'temu' || channelName === 'temu_sandbox'
          ).map(t => (
            <button
              key={t.id}
              onClick={() => setTab(t.id)}
              style={{
                display: 'flex', alignItems: 'center', gap: 9,
                padding: '10px 16px',
                background: tab === t.id ? 'var(--bg-secondary)' : 'transparent',
                border: 'none',
                borderLeft: tab === t.id ? '3px solid var(--primary)' : '3px solid transparent',
                borderRight: 'none',
                color: tab === t.id ? 'var(--primary)' : 'var(--text-muted)',
                fontWeight: tab === t.id ? 700 : 400,
                fontSize: 13, cursor: 'pointer',
                textAlign: 'left', width: '100%',
                transition: 'background 0.1s, color 0.1s',
              }}
              onMouseEnter={e => {
                if (tab !== t.id) (e.currentTarget as HTMLButtonElement).style.background = 'var(--bg-elevated, rgba(255,255,255,0.04))';
              }}
              onMouseLeave={e => {
                if (tab !== t.id) (e.currentTarget as HTMLButtonElement).style.background = 'transparent';
              }}
            >
              <span style={{ fontSize: 15, flexShrink: 0 }}>{t.icon}</span>
              <span style={{ whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{t.label}</span>
            </button>
          ))}
        </div>

        {/* Right content area */}
        <div style={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column' }}>

          {/* Tab content */}
          <div style={{ flex: 1, padding: 28, overflowY: 'auto' }}>
            {tab === 'orders'         && <OrdersTab config={config} onChange={setConfig} channel={channelName} />}
            {tab === 'tax'            && <TaxTab config={config} onChange={setConfig} />}
            {tab === 'stock'          && <StockTab config={config} onChange={setConfig} locations={locations} />}
            {tab === 'payment'        && <PaymentTab config={config} onChange={setConfig} />}
            {tab === 'shipping'       && <ShippingTab config={config} onChange={setConfig} carriers={carriers} />}
            {tab === 'inventory'      && <InventoryTab config={config} onChange={setConfig} credentialId={id} />}
            {tab === 'inventory_sync' && <InventorySyncTab config={config} onChange={setConfig} locations={locations} />}
            {tab === 'notify'         && <NotificationsTab config={config} onChange={setConfig} />}
            {tab === 'temu_defaults'  && <TemuDefaultsTab config={config} onChange={setConfig} credentialId={id} />}
            {tab === 'brand_mapping'  && <TemuBrandMappingTab credentialId={id} />}
          </div>

          {/* Footer — inside right column, pinned to bottom */}
          <div style={{ padding: '14px 28px', borderTop: '1px solid var(--border)', display: 'flex', alignItems: 'center', justifyContent: 'space-between', flexShrink: 0 }}>
            <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>
              Changes are saved per-channel and take effect on the next sync cycle.
            </div>
            <div style={{ display: 'flex', gap: 10, alignItems: 'center' }}>
              {saveError && <span style={{ fontSize: 12, color: '#f87171' }}>⚠ {saveError}</span>}
              <button
                onClick={() => navigate('/marketplace/connections')}
                style={{ padding: '9px 18px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, color: 'var(--text-secondary)', fontSize: 13, cursor: 'pointer' }}
              >
                Cancel
              </button>
              <button
                onClick={save}
                disabled={saving}
                style={{
                  padding: '9px 22px', background: saving ? 'var(--bg-elevated)' : savedOk ? '#16a34a' : 'var(--primary)',
                  border: 'none', borderRadius: 8, color: saving ? 'var(--text-muted)' : '#fff',
                  fontSize: 13, cursor: saving ? 'not-allowed' : 'pointer', fontWeight: 700,
                  transition: 'background 0.2s', display: 'flex', alignItems: 'center', gap: 8,
                }}
              >
                {saving ? (
                  <><span style={{ display: 'inline-block', width: 13, height: 13, border: '2px solid rgba(255,255,255,0.3)', borderTopColor: '#fff', borderRadius: '50%', animation: 'spin 0.6s linear infinite' }} />Saving…</>
                ) : savedOk ? '✅ Saved!' : '💾 Save Configuration'}
              </button>
            </div>
          </div>

        </div>{/* end right column */}
      </div>{/* end main card */}
      <style>{`@keyframes spin { to { transform: rotate(360deg) } }`}</style>
    </div>
  );
}

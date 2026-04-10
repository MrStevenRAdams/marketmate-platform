import { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { getActiveTenantId } from '../../contexts/TenantContext';
import '../../components/SettingsLayout.css';
import './SettingsHub.css';

const API = (import.meta as any).env?.VITE_API_URL || 'https://marketmate-api-487246736287.europe-west2.run.app/api/v1';

interface Modules {
  wms: boolean;
  purchase_orders: boolean;
  advanced_dispatch: boolean;
  rma: boolean;
  advanced_analytics: boolean;
  automation: boolean;
}

const defaultModules: Modules = {
  wms: false, purchase_orders: false, advanced_dispatch: false,
  rma: false, advanced_analytics: false, automation: true,
};

interface Card {
  icon: string;
  title: string;
  desc: string;
  to: string;
  comingSoon?: boolean;
  requiresModule?: keyof Modules;
}

interface Group {
  icon: string;
  title: string;
  cards: Card[];
  requiresModule?: keyof Modules;
}

const GROUPS = (modules: Modules): Group[] => [
  {
    icon: '🏢',
    title: 'Company',
    cards: [
      { icon: '🏢', title: 'Company Settings', desc: 'Business details, regional preferences and display options', to: '/settings/company' },
      { icon: '🌍', title: 'Countries & Tax Rates', desc: 'Country-level tax rates, regions and rule overrides', to: '/settings/countries' },
    ],
  },
  {
    icon: '🛒',
    title: 'Selling Channels',
    cards: [
      { icon: '🔗', title: 'Marketplace Connections', desc: 'Connect Amazon, eBay, Temu and more', to: '/marketplace/connections' },
      { icon: '📄', title: 'Listing Templates', desc: 'Default templates per channel', to: '/settings/listing-templates', comingSoon: true },
    ],
  },
  {
    icon: '🚚',
    title: 'Fulfilment',
    cards: [
      { icon: '🏭', title: 'Warehouses', desc: 'Set up and manage your warehouses', to: '/fulfilment-sources' },
      { icon: '🚢', title: 'Carriers', desc: 'Connect Royal Mail, DPD, Evri and others', to: '/settings/carriers' },
      { icon: '📐', title: 'Shipping Rules', desc: 'Auto-assign carriers based on weight, destination and value', to: '/dispatch/shipping-rules' },
      { icon: '📦', title: 'Packaging Rules', desc: 'Auto-select packaging sizes for orders', to: '/dispatch/packaging-rules' },
      { icon: '📮', title: 'Postage Definitions', desc: 'Define postage services and rate tables', to: '/postage-definitions' },
      { icon: '🏷️', title: 'Order Tags', desc: 'Create and manage coloured shape tags for orders', to: '/settings/order-tags' },
    ],
  },
  {
    icon: '📦',
    title: 'Orders & Fulfilment',
    cards: [
      { icon: '📦', title: 'Order Settings', desc: 'Merge, split, pre-processing checks and despatch rules', to: '/settings/orders' },
      { icon: '🖨️', title: 'Print Settings', desc: 'Auto-print triggers and label format preferences', to: '/settings/print' },
    ],
  },
  // WMS group — only shown when wms module is enabled
  ...(modules.wms ? [{
    icon: '🗄️',
    title: 'Warehouse',
    cards: [
      { icon: '📍', title: 'Bin Types', desc: 'Define storage location types with volumetric tracking', to: '/settings/bin-types' },
      { icon: '⚙️', title: 'WMS Settings', desc: 'Auto-allocation, FIFO and binrack suggestion rules', to: '/settings/wms' },
      { icon: '⏰', title: 'Schedules', desc: 'Automated scheduled tasks and recurring jobs', to: '/schedules' },
      { icon: '🔄', title: 'Stock Moves', desc: 'History of all stock movements between locations', to: '/stock/moves' },
    ],
  }] : []),
  // Suppliers — only shown when purchase_orders module is enabled
  ...(modules.purchase_orders ? [{
    icon: '👥',
    title: 'Suppliers',
    cards: [
      { icon: '🏢', title: 'Suppliers', desc: 'Manage supplier contacts, ordering methods and pricing', to: '/suppliers' },
    ],
  }] : []),
  {
    icon: '📧',
    title: 'Communications',
    cards: [
      { icon: '✉️', title: 'Email Settings', desc: 'SMTP configuration for outbound email', to: '/settings/email' },
      { icon: '🔔', title: 'Notification Preferences', desc: 'Alerts for low stock, failed orders and more', to: '/settings/notifications' },
      { icon: '💬', title: 'Buyer Messages / Helpdesk', desc: 'Configure your messaging settings', to: '/settings/buyer-messages', comingSoon: true },
    ],
  },
  {
    icon: '💱',
    title: 'Finance',
    cards: [
      { icon: '💹', title: 'Currency Exchange Rates', desc: 'Manual and automatic rate configuration', to: '/settings/currency' },
      { icon: '🧾', title: 'Tax & VAT Settings', desc: 'VAT registration number, tax region and default rates', to: '/settings/tax' },
      { icon: '💳', title: 'Billing & Subscription', desc: 'Your current plan and usage', to: '/settings/billing' },
    ],
  },
  {
    icon: '🔧',
    title: 'Account',
    cards: [
      { icon: '🧩', title: 'Feature Modules', desc: 'Enable or disable feature groups to simplify your workspace', to: '/settings/modules' },
      { icon: '👤', title: 'Team & Users', desc: 'Invite team members, manage roles and permissions', to: '/settings/team' },
      { icon: '🔒', title: 'Security', desc: 'Data privacy, 2FA enforcement, purge & obfuscation settings', to: '/settings/security' },
      { icon: '🎨', title: 'Page Builder', desc: 'Create and manage invoice and document templates', to: '/settings/page-builder' },
      { icon: '📦', title: 'Import / Export', desc: 'Bulk product and inventory management', to: '/import-export' },
    ],
  },
  {
    icon: '⚙️',
    title: 'Developer',
    cards: [
      { icon: '🔑', title: 'API Keys', desc: 'Access the MarketMate API programmatically', to: '/settings/api-keys' },
      { icon: '🪝', title: 'Webhooks', desc: 'Push events to external systems in real time', to: '/settings/webhooks', comingSoon: true },
    ],
  },
];

function SettingsCard({ card }: { card: Card }) {
  const navigate = useNavigate();
  return (
    <div
      className={`sh-card ${card.comingSoon ? 'sh-card-muted' : ''}`}
      onClick={() => !card.comingSoon && navigate(card.to)}
      role="button"
      tabIndex={card.comingSoon ? -1 : 0}
      onKeyDown={(e) => e.key === 'Enter' && !card.comingSoon && navigate(card.to)}
    >
      <div className="sh-card-icon">{card.icon}</div>
      <div className="sh-card-body">
        <div className="sh-card-title">
          {card.title}
          {card.comingSoon && <span className="badge-coming-soon">Coming soon</span>}
        </div>
        <div className="sh-card-desc">{card.desc}</div>
      </div>
      <div className="sh-card-chevron">{card.comingSoon ? '' : '›'}</div>
    </div>
  );
}

export default function SettingsHub() {
  const [modules, setModules] = useState<Modules>(defaultModules);

  useEffect(() => {
    fetch(`${API}/settings/modules`, {
      headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId() },
    })
      .then(r => r.ok ? r.json() : null)
      .then(d => { if (d?.modules) setModules({ ...defaultModules, ...d.modules }); })
      .catch(() => {});
  }, []);

  const groups = GROUPS(modules);

  return (
    <div className="settings-page sh-hub">
      <div className="sh-header">
        <h1 className="settings-page-title">Settings</h1>
        <p className="settings-page-sub">Manage your account, integrations and preferences.</p>
      </div>

      {groups.map((group) => (
        <div key={group.title} className="sh-group">
          <div className="sh-group-title">
            <span>{group.icon}</span>
            <span>{group.title}</span>
          </div>
          <div className="sh-grid">
            {group.cards.map((card) => (
              <SettingsCard key={card.to} card={card} />
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}

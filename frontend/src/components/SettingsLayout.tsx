import { ReactNode } from 'react';
import { Link, useLocation, useNavigate } from 'react-router-dom';
import './SettingsLayout.css';

interface SettingsLayoutProps {
  children: ReactNode;
}

interface NavItem {
  to: string;
  label: string;
  exact?: boolean;
}

interface NavSection {
  title: string;
  items: NavItem[];
}

const NAV: NavSection[] = [
  {
    title: 'Selling Channels',
    items: [
      { to: '/marketplace/connections', label: 'Marketplace Connections' },
      { to: '/settings/listing-templates', label: 'Listing Templates' },
    ],
  },
  {
    title: 'Fulfilment',
    items: [
      { to: '/fulfilment-sources', label: 'Warehouses & Locations' },
      { to: '/settings/carriers', label: 'Carriers' },
      { to: '/workflows', label: 'Shipping Workflows' },
      { to: '/automation', label: 'Automation Rules' },
    ],
  },
  {
    title: 'Suppliers',
    items: [
      { to: '/suppliers', label: 'Suppliers' },
    ],
  },
  {
    title: 'Communications',
    items: [
      { to: '/settings/email', label: 'Email Settings' },
      { to: '/settings/notifications', label: 'Notification Preferences' },
      { to: '/settings/buyer-messages', label: 'Buyer Messages / Helpdesk' },
    ],
  },
  {
    title: 'Finance',
    items: [
      { to: '/settings/currency', label: 'Currency Exchange Rates' },
      { to: '/settings/billing', label: 'Billing & Subscription' },
    ],
  },
  {
    title: 'Account',
    items: [
      { to: '/settings/team', label: 'Team & Users' },
      { to: '/settings/page-builder', label: 'Page Builder' },
      { to: '/import-export', label: 'Import / Export' },
    ],
  },
  {
    title: 'Developer',
    items: [
      { to: '/settings/api-keys', label: 'API Keys' },
      { to: '/settings/webhooks', label: 'Webhooks' },
    ],
  },
];

export default function SettingsLayout({ children }: SettingsLayoutProps) {
  const location = useLocation();
  const navigate = useNavigate();

  const isActive = (to: string) =>
    location.pathname === to || location.pathname.startsWith(to + '/');

  return (
    <div className="settings-shell">
      {/* Settings sidebar */}
      <aside className="settings-sidebar">
        <button
          className="settings-back-btn"
          onClick={() => navigate('/orders')}
        >
          ← Back to MarketMate
        </button>

        <div className="settings-sidebar-header">
          <span className="settings-sidebar-logo">⚙️</span>
          <span className="settings-sidebar-title">Settings</span>
        </div>

        <nav className="settings-nav">
          {/* Hub link */}
          <Link
            to="/settings"
            className={`settings-nav-item ${location.pathname === '/settings' ? 'active' : ''}`}
          >
            Overview
          </Link>

          {NAV.map((section) => (
            <div key={section.title} className="settings-nav-section">
              <div className="settings-nav-section-title">{section.title}</div>
              {section.items.map((item) => (
                <Link
                  key={item.to}
                  to={item.to}
                  className={`settings-nav-item ${isActive(item.to) ? 'active' : ''}`}
                >
                  {item.label}
                </Link>
              ))}
            </div>
          ))}
        </nav>
      </aside>

      {/* Main content */}
      <main className="settings-content">
        {children}
      </main>
    </div>
  );
}

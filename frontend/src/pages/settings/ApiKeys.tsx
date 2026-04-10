import { Link } from 'react-router-dom';
import '../../components/SettingsLayout.css';
import './ApiKeys.css';

export default function ApiKeys() {
  return (
    <div className="settings-page">
      <div className="settings-breadcrumb">
        <Link to="/settings">Settings</Link>
        <span className="settings-breadcrumb-sep">›</span>
        <span className="settings-breadcrumb-current">API Keys</span>
      </div>

      <h1 className="settings-page-title">API Keys</h1>
      <p className="settings-page-sub">Programmatic access to the MarketMate API for custom integrations and automation.</p>

      <div className="apikeys-coming-soon">
        <div className="apikeys-icon">🔑</div>
        <div className="apikeys-title">API Access — Coming Soon</div>
        <p className="apikeys-desc">
          The MarketMate REST API will let you build custom integrations, automate workflows,
          and sync data with your own systems. API key management will appear here once the
          developer programme launches.
        </p>

        <div className="apikeys-preview">
          <div className="apikeys-preview-title">What you'll be able to do</div>
          <div className="apikeys-features">
            {[
              { icon: '📦', label: 'Read and create orders programmatically' },
              { icon: '🔄', label: 'Sync inventory from external WMS systems' },
              { icon: '🛒', label: 'Publish listings to channels via API' },
              { icon: '📊', label: 'Export reports and analytics data' },
              { icon: '🔔', label: 'Receive webhook events for real-time sync' },
              { icon: '🔑', label: 'Create multiple scoped keys per integration' },
            ].map((f) => (
              <div key={f.label} className="apikeys-feature">
                <span>{f.icon}</span>
                <span>{f.label}</span>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}

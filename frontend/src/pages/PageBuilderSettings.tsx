import { Link } from 'react-router-dom';
import '../../components/SettingsLayout.css';
import PageBuilder from '../../components/pagebuilder/pagebuilder/components/PageBuilder';

export default function PageBuilderSettings() {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
      <div style={{ padding: '20px 32px 0', borderBottom: '1px solid var(--border)', background: 'var(--bg-secondary)', flexShrink: 0 }}>
        <div className="settings-breadcrumb">
          <Link to="/settings">Settings</Link>
          <span className="settings-breadcrumb-sep">›</span>
          <span className="settings-breadcrumb-current">Page Builder</span>
        </div>
      </div>
      <div style={{ flex: 1, overflow: 'hidden' }}>
        <PageBuilder />
      </div>
    </div>
  );
}

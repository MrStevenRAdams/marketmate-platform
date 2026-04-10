import { useState, useEffect } from 'react';
import { getActiveTenantId } from '../contexts/TenantContext';

const API = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';

function api(path: string, init?: RequestInit) {
  return fetch(`${API}${path}`, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      'X-Tenant-Id': getActiveTenantId(),
      ...init?.headers,
    },
  });
}

type AppCategory = 'macros' | 'shipping' | 'accounting' | 'inventory' | 'email' | 'other';

interface App {
  app_id: string;
  name: string;
  description: string;
  developer: string;
  category: AppCategory;
  type: string;
  macro_type?: string;
  icon_emoji?: string;
  rating: number;
  pricing: string;
  is_built_in: boolean;
  is_installed: boolean;
  installed_at?: string;
}

const CATEGORIES: { value: AppCategory | 'all'; label: string; icon: string }[] = [
  { value: 'all',        label: 'All',         icon: '🏪' },
  { value: 'macros',     label: 'Macros',       icon: '⚙️' },
  { value: 'shipping',   label: 'Shipping',     icon: '🚚' },
  { value: 'inventory',  label: 'Inventory',    icon: '📦' },
  { value: 'email',      label: 'Email',        icon: '✉️' },
  { value: 'accounting', label: 'Accounting',   icon: '💰' },
  { value: 'other',      label: 'Other',        icon: '🔧' },
];

const SORT_OPTIONS = [
  { value: 'name', label: 'Name A–Z' },
  { value: 'category', label: 'Category' },
  { value: 'developer', label: 'Developer' },
  { value: 'rating', label: 'Rating' },
];

function StarRating({ rating }: { rating: number }) {
  const full = Math.floor(rating);
  const half = rating - full >= 0.5;
  return (
    <span style={{ color: '#f59e0b', fontSize: 12 }}>
      {'★'.repeat(full)}
      {half ? '½' : ''}
      {'☆'.repeat(5 - full - (half ? 1 : 0))}
      <span style={{ color: 'var(--text-muted)', marginLeft: 4 }}>({rating.toFixed(1)})</span>
    </span>
  );
}

function AppCard({ app, onInstall, onUninstall, installing }: {
  app: App;
  onInstall: (id: string) => void;
  onUninstall: (id: string) => void;
  installing: string | null;
}) {
  const isLoading = installing === app.app_id;

  return (
    <div style={{
      background: 'var(--bg-secondary)',
      border: '1px solid var(--border)',
      borderRadius: 10,
      padding: '20px',
      display: 'flex',
      flexDirection: 'column',
      gap: 10,
      transition: 'border-color 0.15s, box-shadow 0.15s',
    }}
      onMouseEnter={e => { (e.currentTarget as HTMLElement).style.borderColor = 'var(--accent-cyan)'; (e.currentTarget as HTMLElement).style.boxShadow = '0 0 0 1px var(--accent-cyan)20'; }}
      onMouseLeave={e => { (e.currentTarget as HTMLElement).style.borderColor = 'var(--border)'; (e.currentTarget as HTMLElement).style.boxShadow = 'none'; }}
    >
      <div style={{ display: 'flex', alignItems: 'flex-start', gap: 12 }}>
        <div style={{ width: 44, height: 44, borderRadius: 10, background: 'var(--bg-elevated)', display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 22, flexShrink: 0 }}>
          {app.icon_emoji || '🔌'}
        </div>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ fontWeight: 700, fontSize: 14, color: 'var(--text-primary)', marginBottom: 2 }}>{app.name}</div>
          <div style={{ fontSize: 11, color: 'var(--text-muted)' }}>by {app.developer}</div>
        </div>
        {app.is_installed && (
          <span style={{ flexShrink: 0, fontSize: 11, fontWeight: 600, color: '#22c55e', background: 'rgba(34,197,94,0.12)', border: '1px solid rgba(34,197,94,0.3)', borderRadius: 4, padding: '2px 8px' }}>
            ✓ Installed
          </span>
        )}
      </div>

      <p style={{ fontSize: 12, color: 'var(--text-secondary)', lineHeight: 1.5, flexGrow: 1, margin: 0 }}>{app.description}</p>

      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', gap: 8 }}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
          <StarRating rating={app.rating} />
          <span style={{ fontSize: 11, color: 'var(--text-muted)', textTransform: 'capitalize' }}>{app.category} · {app.pricing}</span>
          {app.is_installed && app.installed_at && (
            <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>
              Installed {new Date(app.installed_at).toLocaleDateString('en-GB', { day: 'numeric', month: 'short', year: 'numeric' })}
            </span>
          )}
        </div>
        {app.is_installed ? (
          <button
            onClick={() => onUninstall(app.app_id)}
            disabled={isLoading}
            style={{ padding: '6px 14px', background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 6, color: '#ef4444', fontSize: 12, fontWeight: 600, cursor: 'pointer' }}
          >
            {isLoading ? 'Removing…' : 'Uninstall'}
          </button>
        ) : (
          <button
            onClick={() => onInstall(app.app_id)}
            disabled={isLoading}
            style={{ padding: '6px 14px', background: 'var(--accent-cyan)', border: 'none', borderRadius: 6, color: '#0f172a', fontSize: 12, fontWeight: 700, cursor: 'pointer' }}
          >
            {isLoading ? 'Installing…' : 'Install'}
          </button>
        )}
      </div>
    </div>
  );
}

export default function AppStore() {
  const [apps, setApps] = useState<App[]>([]);
  const [loading, setLoading] = useState(true);
  const [activeTab, setActiveTab] = useState<'all' | 'installed'>('all');
  const [selectedCategory, setSelectedCategory] = useState<AppCategory | 'all'>('all');
  const [sortBy, setSortBy] = useState('name');
  const [searchQuery, setSearchQuery] = useState('');
  const [installing, setInstalling] = useState<string | null>(null);
  const [toast, setToast] = useState<{ type: 'success' | 'error'; message: string } | null>(null);

  useEffect(() => { load(); }, []);

  const showToast = (type: 'success' | 'error', message: string) => {
    setToast({ type, message });
    setTimeout(() => setToast(null), 3500);
  };

  async function load() {
    setLoading(true);
    try {
      const res = await api('/apps');
      if (res.ok) {
        const data = await res.json();
        setApps(data.apps || []);
      }
    } catch {
      showToast('error', 'Failed to load applications.');
    } finally {
      setLoading(false);
    }
  }

  async function handleInstall(appId: string) {
    setInstalling(appId);
    try {
      const res = await api(`/apps/${appId}/install`, { method: 'POST' });
      if (res.ok) {
        setApps(prev => prev.map(a => a.app_id === appId ? { ...a, is_installed: true, installed_at: new Date().toISOString() } : a));
        showToast('success', 'Application installed successfully.');
      } else {
        const d = await res.json();
        showToast('error', d.error || 'Installation failed.');
      }
    } catch {
      showToast('error', 'Installation failed.');
    } finally {
      setInstalling(null);
    }
  }

  async function handleUninstall(appId: string) {
    if (!confirm('Uninstall this application? Your configurations will be removed.')) return;
    setInstalling(appId);
    try {
      const res = await api(`/apps/${appId}/uninstall`, { method: 'DELETE' });
      if (res.ok) {
        setApps(prev => prev.map(a => a.app_id === appId ? { ...a, is_installed: false, installed_at: undefined } : a));
        showToast('success', 'Application uninstalled.');
      } else {
        showToast('error', 'Uninstall failed.');
      }
    } catch {
      showToast('error', 'Uninstall failed.');
    } finally {
      setInstalling(null);
    }
  }

  // Filter + sort
  let displayed = apps.filter(a => {
    if (activeTab === 'installed' && !a.is_installed) return false;
    if (selectedCategory !== 'all' && a.category !== selectedCategory) return false;
    if (searchQuery) {
      const q = searchQuery.toLowerCase();
      return a.name.toLowerCase().includes(q) || a.description.toLowerCase().includes(q) || a.developer.toLowerCase().includes(q);
    }
    return true;
  });

  displayed = [...displayed].sort((x, y) => {
    switch (sortBy) {
      case 'category': return x.category.localeCompare(y.category);
      case 'developer': return x.developer.localeCompare(y.developer);
      case 'rating': return y.rating - x.rating;
      default: return x.name.localeCompare(y.name);
    }
  });

  const installedCount = apps.filter(a => a.is_installed).length;

  return (
    <div style={{ padding: '24px 0' }}>
      {toast && (
        <div style={{
          position: 'fixed', top: 24, right: 24, zIndex: 9999,
          background: toast.type === 'success' ? '#22c55e' : '#ef4444',
          color: '#fff', padding: '12px 20px', borderRadius: 8, fontWeight: 600, fontSize: 14,
          boxShadow: '0 4px 16px rgba(0,0,0,0.3)',
        }}>
          {toast.message}
        </div>
      )}

      {/* Header */}
      <div style={{ marginBottom: 24 }}>
        <h2 style={{ fontSize: 22, fontWeight: 700, color: 'var(--text-primary)', margin: 0 }}>Application Store</h2>
        <p style={{ color: 'var(--text-muted)', fontSize: 14, marginTop: 4 }}>
          Browse and install macros and integrations to extend Marketmate.
        </p>
      </div>

      {/* Tabs */}
      <div style={{ display: 'flex', borderBottom: '1px solid var(--border)', marginBottom: 20 }}>
        {([['all', 'All Applications'], ['installed', `My Applications${installedCount > 0 ? ` (${installedCount})` : ''}`]] as const).map(([t, label]) => (
          <button
            key={t}
            onClick={() => setActiveTab(t)}
            style={{
              padding: '10px 20px', background: 'none', border: 'none', cursor: 'pointer',
              borderBottom: activeTab === t ? '2px solid var(--accent-cyan)' : '2px solid transparent',
              color: activeTab === t ? 'var(--accent-cyan)' : 'var(--text-muted)',
              fontWeight: activeTab === t ? 700 : 400, fontSize: 14,
            }}
          >
            {label}
          </button>
        ))}
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: '200px 1fr', gap: 24 }}>
        {/* Sidebar */}
        <div>
          <div style={{ fontWeight: 700, fontSize: 12, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.06em', marginBottom: 8 }}>Category</div>
          {CATEGORIES.map(cat => (
            <button
              key={cat.value}
              onClick={() => setSelectedCategory(cat.value as AppCategory | 'all')}
              style={{
                display: 'flex', alignItems: 'center', gap: 8, width: '100%', padding: '8px 10px',
                background: selectedCategory === cat.value ? 'rgba(34,211,238,0.1)' : 'none',
                border: 'none', borderRadius: 6, cursor: 'pointer', textAlign: 'left',
                color: selectedCategory === cat.value ? 'var(--accent-cyan)' : 'var(--text-secondary)',
                fontSize: 13, fontWeight: selectedCategory === cat.value ? 600 : 400,
              }}
            >
              <span>{cat.icon}</span> {cat.label}
            </button>
          ))}

          <div style={{ marginTop: 20, paddingTop: 16, borderTop: '1px solid var(--border)' }}>
            <div style={{ fontWeight: 700, fontSize: 12, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.06em', marginBottom: 8 }}>Sort By</div>
            {SORT_OPTIONS.map(opt => (
              <button
                key={opt.value}
                onClick={() => setSortBy(opt.value)}
                style={{
                  display: 'block', width: '100%', padding: '6px 10px',
                  background: sortBy === opt.value ? 'rgba(34,211,238,0.1)' : 'none',
                  border: 'none', borderRadius: 6, cursor: 'pointer', textAlign: 'left',
                  color: sortBy === opt.value ? 'var(--accent-cyan)' : 'var(--text-secondary)',
                  fontSize: 13, fontWeight: sortBy === opt.value ? 600 : 400,
                }}
              >
                {opt.label}
              </button>
            ))}
          </div>
        </div>

        {/* Main content */}
        <div>
          {/* Search */}
          <input
            type="search"
            placeholder="Search applications…"
            value={searchQuery}
            onChange={e => setSearchQuery(e.target.value)}
            style={{
              width: '100%', marginBottom: 16, padding: '8px 12px',
              background: 'var(--bg-secondary)', border: '1px solid var(--border)',
              borderRadius: 6, color: 'var(--text-primary)', fontSize: 13, boxSizing: 'border-box',
            }}
          />

          {loading ? (
            <div style={{ textAlign: 'center', padding: 48, color: 'var(--text-muted)' }}>Loading applications…</div>
          ) : displayed.length === 0 ? (
            <div style={{ textAlign: 'center', padding: 48, color: 'var(--text-muted)' }}>
              <div style={{ fontSize: 36, marginBottom: 12 }}>🔍</div>
              <div style={{ fontWeight: 600, marginBottom: 4 }}>No applications found</div>
              <div style={{ fontSize: 13 }}>Try adjusting your filters or search query.</div>
            </div>
          ) : (
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))', gap: 16 }}>
              {displayed.map(app => (
                <AppCard
                  key={app.app_id}
                  app={app}
                  onInstall={handleInstall}
                  onUninstall={handleUninstall}
                  installing={installing}
                />
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

import { apiFetch } from '../services/apiFetch';
// ============================================================================
// UPDATED LAYOUT COMPONENT
// ============================================================================
// Location: frontend/src/components/Layout.tsx
//
// Changes:
//   - Added SUPPORT nav section above sidebar-footer with flyout submenu
//   - Amazon Schema Manager link auto-resolves credential_id from API
//   - Added Schema Cache Manager link in Dev Tools
//   - Added Job Monitor link in Dev Tools
//   - Added Orders link in OPERATIONS section

import { useState, useEffect, useRef, useCallback } from 'react';
import { Link, Navigate, Outlet, useLocation, useNavigate } from 'react-router-dom';
import TenantSwitcher from './TenantSwitcher';
import { useAuth } from '../contexts/AuthContext';

// ── Sync Status Panel (inlined to avoid circular dependency / TDZ bundle errors) ──

const SYNC_API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';
function syncApi(path: string, tenantId: string, init?: RequestInit) {
  return fetch(`${SYNC_API_BASE}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': tenantId, ...init?.headers },
  });
}

interface SystemNotification {
  id: string;
  type: string;
  message: string;
  read: boolean;
  created_at: string;
}

interface SyncTask {
  task_id: string; type: string; channel: string; source: string;
  status: string; progress: number; total: number;
  started_at: string; updated_at: string; error?: string; ack: boolean;
}
interface SyncStatusData { tasks: SyncTask[]; processing: number; pending: number; errors: number; }

function SyncStatusPanel({ isOpen, onClose, onErrorCountChange }: {
  isOpen: boolean; onClose: () => void; onErrorCountChange?: (n: number) => void;
}) {
  const { activeTenant } = useAuth();
  const [data, setData] = useState<SyncStatusData | null>(null);
  const [tab, setTab] = useState<'processing' | 'pending' | 'errors'>('processing');
  const [clearing, setClearing] = useState(false);

  const load = useCallback(async () => {
    try {
      const res = await syncApi('/sync/status', activeTenant?.tenant_id || '');
      if (res.ok) { const d = await res.json(); setData(d); onErrorCountChange?.(d.errors || 0); }
    } catch {}
  }, [onErrorCountChange, activeTenant]);

  useEffect(() => {
    if (isOpen) { load(); const iv = setInterval(load, 15000); return () => clearInterval(iv); }
  }, [isOpen, load]);

  const clearErrors = async () => {
    setClearing(true);
    try { await syncApi('/sync/errors/clear', activeTenant?.tenant_id || '', { method: 'POST' }); load(); } finally { setClearing(false); }
  };

  const tasks = data?.tasks || [];
  const filtered = tasks.filter(t =>
    tab === 'processing' ? t.status === 'running' :
    tab === 'pending' ? t.status === 'pending' :
    t.status === 'error' && !t.ack
  );
  const typeIcon = (type: string) =>
    type === 'import' ? '⬇️' : type === 'order_import' ? '🛒' : type === 'automation' ? '⚙️' : type === 'ai_generation' ? '🤖' : '🔄';
  const fmt = (iso: string) => { try { return new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }); } catch { return '—'; } };

  return (
    <>
      {isOpen && <div onClick={onClose} style={{ position: 'fixed', inset: 0, zIndex: 199 }} />}
      <div style={{ position: 'fixed', top: 0, right: 0, height: '100vh', width: 400, background: 'var(--bg-secondary)', borderLeft: '1px solid var(--border)', zIndex: 200, transform: isOpen ? 'translateX(0)' : 'translateX(100%)', transition: 'transform 0.25s ease', display: 'flex', flexDirection: 'column', boxShadow: '-4px 0 24px rgba(0,0,0,0.4)' }}>
        <div style={{ padding: '16px 20px', borderBottom: '1px solid var(--border)', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <div><div style={{ fontWeight: 700, color: 'var(--text-primary)', fontSize: 15 }}>🔄 Sync Status</div><div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>Last 24 hours</div></div>
          <button onClick={onClose} style={{ background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-muted)', fontSize: 20, lineHeight: 1, padding: 4 }}>×</button>
        </div>
        <div style={{ display: 'flex', gap: 8, padding: '12px 20px', borderBottom: '1px solid var(--border)' }}>
          {([['processing','#3b82f6'], ['pending','#fbbf24'], ['errors','#ef4444']] as const).map(([k,c]) => (
            <div key={k} style={{ flex: 1, background: `${c}1a`, border: `1px solid ${c}40`, borderRadius: 8, padding: '8px 12px', textAlign: 'center' }}>
              <div style={{ fontSize: 18, fontWeight: 700, color: c }}>{data?.[k as 'processing'|'pending'|'errors'] ?? '—'}</div>
              <div style={{ fontSize: 11, color: c, opacity: 0.8, textTransform: 'capitalize' }}>{k}</div>
            </div>
          ))}
        </div>
        <div style={{ display: 'flex', borderBottom: '1px solid var(--border)', padding: '0 12px' }}>
          {(['processing','pending','errors'] as const).map(t => (
            <button key={t} onClick={() => setTab(t)} style={{ padding: '10px 14px', background: 'none', border: 'none', cursor: 'pointer', borderBottom: tab === t ? '2px solid var(--accent-cyan)' : '2px solid transparent', color: tab === t ? 'var(--accent-cyan)' : 'var(--text-muted)', fontWeight: tab === t ? 600 : 400, fontSize: 13, textTransform: 'capitalize' }}>{t}</button>
          ))}
          {tab === 'errors' && (data?.errors ?? 0) > 0 && (
            <button onClick={clearErrors} disabled={clearing} style={{ marginLeft: 'auto', padding: '6px 12px', marginTop: 6, background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 6, color: '#ef4444', fontSize: 11, cursor: 'pointer' }}>{clearing ? 'Clearing…' : 'Clear All'}</button>
          )}
        </div>
        <div style={{ flex: 1, overflowY: 'auto', padding: '8px 0' }}>
          {filtered.length === 0 ? (
            <div style={{ textAlign: 'center', padding: 40, color: 'var(--text-muted)', fontSize: 13 }}>
              {tab === 'processing' && 'No tasks currently processing.'}
              {tab === 'pending' && 'No tasks in queue.'}
              {tab === 'errors' && '✅ No unacknowledged errors.'}
            </div>
          ) : filtered.map(task => (
            <div key={task.task_id} style={{ padding: '12px 20px', borderBottom: '1px solid var(--border)', display: 'flex', alignItems: 'flex-start', gap: 12 }}>
              <div style={{ fontSize: 18, marginTop: 1 }}>{typeIcon(task.type)}</div>
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ fontWeight: 600, color: 'var(--text-primary)', fontSize: 13 }}>{task.source}</div>
                <div style={{ color: 'var(--text-muted)', fontSize: 11, marginTop: 2 }}>{task.channel && <span style={{ marginRight: 8 }}>{task.channel}</span>}Started {fmt(task.started_at)}</div>
                {task.progress > 0 && task.total > 0 && (
                  <div style={{ marginTop: 8 }}>
                    <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 11, color: 'var(--text-muted)', marginBottom: 3 }}><span>Progress</span><span>{task.progress}/{task.total}</span></div>
                    <div style={{ height: 4, background: 'var(--bg-elevated)', borderRadius: 2, overflow: 'hidden' }}><div style={{ height: '100%', background: 'var(--primary)', width: `${Math.min(100, (task.progress / task.total) * 100)}%`, borderRadius: 2 }} /></div>
                  </div>
                )}
                {task.error && <div style={{ marginTop: 6, fontSize: 11, color: '#ef4444', background: 'rgba(239,68,68,0.08)', borderRadius: 4, padding: '4px 8px' }}>{task.error}</div>}
              </div>
              <div style={{ flexShrink: 0, padding: '2px 8px', borderRadius: 4, fontSize: 11, fontWeight: 600, background: task.status === 'running' ? 'rgba(59,130,246,0.15)' : task.status === 'error' ? 'rgba(239,68,68,0.15)' : 'rgba(251,191,36,0.15)', color: task.status === 'running' ? '#3b82f6' : task.status === 'error' ? '#ef4444' : '#fbbf24' }}>{task.status}</div>
            </div>
          ))}
        </div>
        <div style={{ padding: '12px 20px', borderTop: '1px solid var(--border)' }}>
          <button onClick={load} style={{ width: '100%', padding: '8px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-secondary)', cursor: 'pointer', fontSize: 12 }}>🔄 Refresh</button>
        </div>
      </div>
    </>
  );
}


// ── H-001: Changelog / What's New Panel ──────────────────────────────────────

interface ChangelogEntry {
  entry_id: string;
  version: string;
  date: string;
  title: string;
  description: string;
  type: 'feature' | 'fix' | 'improvement';
  created_at: string;
}

function ChangelogPanel({ isOpen, onClose, entries, loading, unreadCount, onMarkSeen }: {
  isOpen: boolean;
  onClose: () => void;
  entries: ChangelogEntry[];
  loading: boolean;
  unreadCount: number;
  onMarkSeen: () => void;
}) {
  useEffect(() => {
    if (isOpen && unreadCount > 0) {
      onMarkSeen();
    }
  }, [isOpen]);

  const typeBadge = (type: ChangelogEntry['type']) => {
    const styles: Record<string, { bg: string; color: string; label: string }> = {
      feature:     { bg: 'rgba(99,102,241,0.15)',  color: '#818cf8', label: 'Feature' },
      fix:         { bg: 'rgba(239,68,68,0.12)',   color: '#f87171', label: 'Fix' },
      improvement: { bg: 'rgba(34,197,94,0.12)',   color: '#4ade80', label: 'Improvement' },
    };
    const s = styles[type] ?? styles.feature;
    return (
      <span style={{
        display: 'inline-block', padding: '2px 8px', borderRadius: 4, fontSize: 11,
        fontWeight: 600, background: s.bg, color: s.color, letterSpacing: '0.02em',
      }}>
        {s.label}
      </span>
    );
  };

  const fmtDate = (d: string) => {
    try { return new Date(d).toLocaleDateString('en-GB', { day: 'numeric', month: 'short', year: 'numeric' }); }
    catch { return d; }
  };

  return (
    <>
      {isOpen && <div onClick={onClose} style={{ position: 'fixed', inset: 0, zIndex: 299 }} />}
      <div style={{
        position: 'fixed', top: 0, right: 0, height: '100vh', width: 420,
        background: 'var(--bg-secondary)', borderLeft: '1px solid var(--border)',
        zIndex: 300, transform: isOpen ? 'translateX(0)' : 'translateX(100%)',
        transition: 'transform 0.25s ease', display: 'flex', flexDirection: 'column',
        boxShadow: '-4px 0 24px rgba(0,0,0,0.4)',
      }}>
        <div style={{ padding: '16px 20px', borderBottom: '1px solid var(--border)', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <div>
            <div style={{ fontWeight: 700, color: 'var(--text-primary)', fontSize: 15 }}>🎉 What's New</div>
            <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>Latest updates to MarketMate</div>
          </div>
          <button onClick={onClose} style={{ background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-muted)', fontSize: 20, lineHeight: 1, padding: 4 }}>×</button>
        </div>
        <div style={{ flex: 1, overflowY: 'auto', padding: '8px 0' }}>
          {loading ? (
            <div style={{ textAlign: 'center', padding: 40, color: 'var(--text-muted)', fontSize: 13 }}>Loading…</div>
          ) : entries.length === 0 ? (
            <div style={{ textAlign: 'center', padding: 40, color: 'var(--text-muted)', fontSize: 13 }}>
              <div style={{ fontSize: 32, marginBottom: 12 }}>🚀</div>
              No updates yet. Check back soon!
            </div>
          ) : entries.map((entry, i) => (
            <div key={entry.entry_id} style={{
              padding: '16px 20px',
              borderBottom: i < entries.length - 1 ? '1px solid var(--border)' : 'none',
            }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 6 }}>
                {typeBadge(entry.type)}
                {entry.version && (
                  <span style={{ fontSize: 11, color: 'var(--text-muted)', background: 'var(--bg-tertiary)', padding: '2px 6px', borderRadius: 4 }}>
                    v{entry.version}
                  </span>
                )}
                <span style={{ marginLeft: 'auto', fontSize: 11, color: 'var(--text-muted)' }}>{fmtDate(entry.date)}</span>
              </div>
              <div style={{ fontWeight: 600, color: 'var(--text-primary)', fontSize: 14, marginBottom: 4 }}>{entry.title}</div>
              {entry.description && (
                <div style={{ fontSize: 13, color: 'var(--text-secondary)', lineHeight: 1.5 }}>{entry.description}</div>
              )}
            </div>
          ))}
        </div>
      </div>
    </>
  );
}

export default function Layout() {
  const location = useLocation();
  const navigate = useNavigate();
  const { logout, user, isLoading, isAuthenticated, activeTenant } = useAuth();

  const isActive = (path: string) => location.pathname === path;
  const isActivePrefix = (prefix: string) => location.pathname.startsWith(prefix);

  // ── Support flyout state ──
  const [supportOpen, setSupportOpen] = useState(false);
  const [amazonOpen, setAmazonOpen] = useState(false);
  const [amazonCredId, setAmazonCredId] = useState<string | null>(null);
  const [loadingCred, setLoadingCred] = useState(false);
  const [rmaActionable, setRmaActionable] = useState(0);
  const [msgUnread, setMsgUnread] = useState(0);
  const [syncPanelOpen, setSyncPanelOpen] = useState(false);
  const [syncErrorCount, setSyncErrorCount] = useState(0);

  // Session 1: Notifications bell
  const [notifPanelOpen, setNotifPanelOpen] = useState(false);
  const [notifUnread, setNotifUnread] = useState(0);
  const [notifications, setNotifications] = useState<SystemNotification[]>([]);

  // Session 1: User profile dropdown
  const [profileDropdownOpen, setProfileDropdownOpen] = useState(false);
  const profileDropdownRef = useRef<HTMLDivElement>(null);

  // H-001: Changelog / What's New state
  const [changelogOpen, setChangelogOpen] = useState(false);
  const [changelogEntries, setChangelogEntries] = useState<ChangelogEntry[]>([]);
  const [changelogUnread, setChangelogUnread] = useState(0);
  const [changelogLoading, setChangelogLoading] = useState(false);

  const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';

  // Fetch RMA actionable count on mount and route changes
  useEffect(() => {
    if (!activeTenant) return;
    const tenantId = activeTenant?.tenant_id || '';
    fetch(`${API_BASE}/rmas?limit=100`, { headers: { 'X-Tenant-Id': tenantId } })
      .then(r => r.ok ? r.json() : null)
      .then(data => { if (data) setRmaActionable(data.actionable || 0); })
      .catch(() => {});
  }, [location.pathname, activeTenant]);

  // Fetch unread message count for nav badge
  useEffect(() => {
    if (!activeTenant) return;
    const tenantId = activeTenant?.tenant_id || '';
    fetch(`${API_BASE}/messages/unread-count`, { headers: { 'X-Tenant-Id': tenantId } })
      .then(r => r.ok ? r.json() : null)
      .then(data => { if (data) setMsgUnread(data.unread || 0); })
      .catch(() => {});
  }, [location.pathname, activeTenant]);

  // H-001: Fetch changelog entries + compute unread badge on mount
  useEffect(() => {
    if (!activeTenant) return;
    const loadChangelog = async () => {
      setChangelogLoading(true);
      try {
        const res = await fetch(`${API_BASE}/changelog`);
        if (!res.ok) return;
        const data = await res.json();
        const entries: ChangelogEntry[] = data.entries || [];
        setChangelogEntries(entries);
        const lastViewed = localStorage.getItem('changelog_last_viewed');
        if (lastViewed && entries.length > 0) {
          const lastViewedTime = new Date(lastViewed).getTime();
          const unread = entries.filter(e => new Date(e.created_at).getTime() > lastViewedTime).length;
          setChangelogUnread(unread);
        } else if (!lastViewed && entries.length > 0) {
          setChangelogUnread(entries.length);
        }
      } catch {
        // Non-fatal
      } finally {
        setChangelogLoading(false);
      }
    };
    loadChangelog();
  }, [activeTenant]);

  // Session 1: Fetch notifications unread count
  useEffect(() => {
    let cancelled = false;
    const loadNotifications = async () => {
      const tenantId = activeTenant?.tenant_id || '';
      if (!tenantId) return;
      try {
        const res = await fetch(`${API_BASE}/notifications`, { headers: { 'X-Tenant-Id': tenantId } });
        if (cancelled) return;
        if (res.ok) {
          const data = await res.json();
          setNotifications(data.notifications || []);
          setNotifUnread(data.unread_count || 0);
        }
        // Silently ignore 500s — Firestore index may not be ready
      } catch {}
    };
    loadNotifications();
    return () => { cancelled = true; };
  }, [location.pathname, activeTenant]);

  // Session 1: Close profile dropdown on outside click
  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (profileDropdownRef.current && !profileDropdownRef.current.contains(e.target as Node)) {
        setProfileDropdownOpen(false);
      }
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, []);

  const markAllNotificationsRead = async () => {
    const tenantId = activeTenant?.tenant_id || '';
    await fetch(`${API_BASE}/notifications/mark-read`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': tenantId },
      body: JSON.stringify({ all: true }),
    }).catch(() => {});
    setNotifUnread(0);
    setNotifications(prev => prev.map(n => ({ ...n, read: true })));
  };

  const handleMarkChangelogSeen = () => {
    const now = new Date().toISOString();
    localStorage.setItem('changelog_last_viewed', now);
    setChangelogUnread(0);
    if (user?.user_id) {
      const tenantId = activeTenant?.tenant_id || '';
      fetch(`${API_BASE}/changelog/seen`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': tenantId },
        body: JSON.stringify({ user_id: user.user_id }),
      }).catch(() => {});
    }
  };

  const supportRef = useRef<HTMLDivElement>(null);

  // Close flyout on outside click
  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (supportRef.current && !supportRef.current.contains(e.target as Node)) {
        setSupportOpen(false);
        setAmazonOpen(false);
      }
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, []);

  // Fetch Amazon credential on first expand
  const resolveAmazonCred = async () => {
    if (amazonCredId || loadingCred) return;
    setLoadingCred(true);
    try {
      const tenantId = activeTenant?.tenant_id || '';
      const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';
      const res = await fetch(`${API_BASE}/marketplace/credentials`, {
        headers: { 'X-Tenant-Id': tenantId },
      });
      if (res.ok) {
        const data = await res.json();
        const creds = data.credentials || data || [];
        const amzCred = (Array.isArray(creds) ? creds : []).find(
          (c: any) => c.channel === 'amazon' && c.active !== false
        );
        if (amzCred) {
          setAmazonCredId(amzCred.credential_id || amzCred.id || amzCred.credentialId);
        }
      }
    } catch {
      // silently fail — user can still navigate, just won't have credential pre-filled
    } finally {
      setLoadingCred(false);
    }
  };

  const navigateToSchemaManager = () => {
    const params = amazonCredId ? `?credential_id=${amazonCredId}` : '';
    navigate(`/marketplace/amazon/schemas${params}`);
    setSupportOpen(false);
    setAmazonOpen(false);
  };

  // Auth guards — placed after all hooks to comply with Rules of Hooks
  if (isLoading) {
    return (
      <div style={{
        display: 'flex', alignItems: 'center', justifyContent: 'center',
        height: '100vh', background: 'var(--bg-primary)',
        color: 'var(--text-muted)', fontSize: 14,
      }}>
        <span className="spinner" style={{ width: 20, height: 20, marginRight: 12 }} />
        Loading...
      </div>
    );
  }

  if (!isAuthenticated) {
    return <Navigate to="/login" replace />;
  }

  return (
    <div className="app-container">
      <aside className="sidebar">
        <div className="sidebar-header">
          <div className="logo">
            <div className="logo-icon">🎯</div>
            <span>MultiCommerce</span>
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
            <button
              onClick={() => setChangelogOpen(true)}
              title="What's New"
              style={{
                background: 'none', border: 'none', cursor: 'pointer',
                color: changelogUnread > 0 ? '#818cf8' : 'var(--text-muted)',
                position: 'relative', padding: '4px', fontSize: '16px',
              }}
            >
              🎉
              {changelogUnread > 0 && (
                <span style={{
                  position: 'absolute', top: '-2px', right: '-2px',
                  background: '#818cf8', color: '#fff',
                  borderRadius: '50%', width: '16px', height: '16px',
                  fontSize: '10px', display: 'flex', alignItems: 'center', justifyContent: 'center',
                }}>{changelogUnread > 9 ? '9+' : changelogUnread}</span>
              )}
            </button>
            <button
              onClick={() => { setNotifPanelOpen(true); }}
              title="Notifications"
              style={{
                background: 'none', border: 'none', cursor: 'pointer',
                color: notifUnread > 0 ? '#fbbf24' : 'var(--text-muted)',
                position: 'relative', padding: '4px', fontSize: '16px',
              }}
            >
              🔔
              {notifUnread > 0 && (
                <span style={{
                  position: 'absolute', top: '-2px', right: '-2px',
                  background: '#fbbf24', color: '#000',
                  borderRadius: '50%', width: '16px', height: '16px',
                  fontSize: '10px', display: 'flex', alignItems: 'center', justifyContent: 'center',
                  fontWeight: 700,
                }}>{notifUnread > 9 ? '9+' : notifUnread}</span>
              )}
            </button>
            <button
              onClick={() => setSyncPanelOpen(true)}
              title="Sync Status"
              style={{
                background: 'none', border: 'none', cursor: 'pointer',
                color: syncErrorCount > 0 ? 'var(--danger)' : 'var(--text-muted)',
                position: 'relative', padding: '4px', fontSize: '16px',
              }}
            >
              🔄
              {syncErrorCount > 0 && (
                <span style={{
                  position: 'absolute', top: '-2px', right: '-2px',
                  background: 'var(--danger)', color: '#fff',
                  borderRadius: '50%', width: '16px', height: '16px',
                  fontSize: '10px', display: 'flex', alignItems: 'center', justifyContent: 'center',
                }}>{syncErrorCount > 9 ? '9+' : syncErrorCount}</span>
              )}
            </button>
          </div>
        </div>

        <SyncStatusPanel
          isOpen={syncPanelOpen}
          onClose={() => setSyncPanelOpen(false)}
          onErrorCountChange={setSyncErrorCount}
        />

        {/* Session 1: Notifications slide-out panel */}
        <>
          {notifPanelOpen && <div onClick={() => setNotifPanelOpen(false)} style={{ position: 'fixed', inset: 0, zIndex: 299 }} />}
          <div style={{
            position: 'fixed', top: 0, right: 0, height: '100vh', width: 380,
            background: 'var(--bg-secondary)', borderLeft: '1px solid var(--border)',
            zIndex: 300, transform: notifPanelOpen ? 'translateX(0)' : 'translateX(100%)',
            transition: 'transform 0.25s ease', display: 'flex', flexDirection: 'column',
            boxShadow: '-4px 0 24px rgba(0,0,0,0.4)',
          }}>
            <div style={{ padding: '16px 20px', borderBottom: '1px solid var(--border)', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
              <div>
                <div style={{ fontWeight: 700, color: 'var(--text-primary)', fontSize: 15 }}>🔔 Notifications</div>
                {notifUnread > 0 && <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>{notifUnread} unread</div>}
              </div>
              <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                {notifUnread > 0 && (
                  <button onClick={markAllNotificationsRead} style={{ fontSize: 12, color: 'var(--accent-cyan)', background: 'none', border: 'none', cursor: 'pointer', padding: 0 }}>
                    Mark all read
                  </button>
                )}
                <button onClick={() => setNotifPanelOpen(false)} style={{ background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-muted)', fontSize: 20, lineHeight: 1, padding: 4 }}>×</button>
              </div>
            </div>
            <div style={{ flex: 1, overflowY: 'auto', padding: '8px 0' }}>
              {notifications.length === 0 ? (
                <div style={{ textAlign: 'center', padding: 40, color: 'var(--text-muted)', fontSize: 13 }}>
                  <div style={{ fontSize: 32, marginBottom: 12 }}>🎉</div>
                  No notifications — all clear!
                </div>
              ) : notifications.map((n, i) => (
                <div key={n.id} style={{
                  padding: '12px 20px', borderBottom: i < notifications.length - 1 ? '1px solid var(--border)' : 'none',
                  background: n.read ? 'transparent' : 'rgba(251,191,36,0.04)',
                  display: 'flex', alignItems: 'flex-start', gap: 10,
                }}>
                  <div style={{ fontSize: 16, marginTop: 1, flexShrink: 0 }}>
                    {n.type === 'sync_error' ? '⚠️' : n.type === 'low_stock' ? '📦' : n.type === 'automation_failure' ? '⚙️' : '📢'}
                  </div>
                  <div style={{ flex: 1, minWidth: 0 }}>
                    <div style={{ fontSize: 13, color: n.read ? 'var(--text-secondary)' : 'var(--text-primary)', lineHeight: 1.4 }}>{n.message}</div>
                    <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 3 }}>
                      {(() => { try { return new Date(n.created_at).toLocaleString('en-GB', { day: '2-digit', month: 'short', hour: '2-digit', minute: '2-digit' }); } catch { return n.created_at; } })()}
                    </div>
                  </div>
                  {!n.read && <div style={{ width: 8, height: 8, background: '#fbbf24', borderRadius: '50%', marginTop: 4, flexShrink: 0 }} />}
                </div>
              ))}
            </div>
          </div>
        </>

        <ChangelogPanel
          isOpen={changelogOpen}
          onClose={() => setChangelogOpen(false)}
          entries={changelogEntries}
          loading={changelogLoading}
          unreadCount={changelogUnread}
          onMarkSeen={handleMarkChangelogSeen}
        />

        <nav className="sidebar-nav">

          {/* ── DASHBOARD ───────────────────────────────────────────────── */}
          <div className="nav-section">
            <Link to="/dashboard" className={`nav-item ${isActive('/dashboard') ? 'active' : ''}`}>
              <span className="nav-icon">🏠</span><span>Dashboard</span>
            </Link>
          </div>

          {/* ── CATALOG ─────────────────────────────────────────────────── */}
          <div className="nav-section">
            <div className="nav-section-title">CATALOG</div>
            <Link to="/products" className={`nav-item ${isActivePrefix('/products') ? 'active' : ''}`}>
              <span className="nav-icon">📦</span><span>Products</span>
            </Link>
            <Link to="/categories" className={`nav-item ${isActive('/categories') ? 'active' : ''}`}>
              <span className="nav-icon">📁</span><span>Categories</span>
            </Link>
            <Link to="/attributes" className={`nav-item ${isActive('/attributes') ? 'active' : ''}`}>
              <span className="nav-icon">🏷️</span><span>Attributes</span>
            </Link>
          </div>

          {/* ── MARKETPLACE ─────────────────────────────────────────────── */}
          <div className="nav-section">
            <div className="nav-section-title">MARKETPLACE</div>
            <Link to="/marketplace/connections" className={`nav-item ${isActive('/marketplace/connections') ? 'active' : ''}`}>
              <span className="nav-icon">🔗</span><span>Connections</span>
            </Link>
            <Link to="/marketplace/import" className={`nav-item ${isActivePrefix('/marketplace/import') ? 'active' : ''}`}>
              <span className="nav-icon">⬇️</span><span>Import</span>
            </Link>
            <Link to="/marketplace/listings" className={`nav-item ${isActivePrefix('/marketplace/listings') ? 'active' : ''}`}>
              <span className="nav-icon">📋</span><span>Listings</span>
            </Link>
            <Link to="/marketplace/configurators" className={`nav-item ${isActivePrefix('/marketplace/configurators') ? 'active' : ''}`}>
              <span className="nav-icon">⚙️</span><span>Configurators</span>
            </Link>
            <Link to="/marketplace/fba-inbound" className={`nav-item ${isActivePrefix('/marketplace/fba-inbound') ? 'active' : ''}`}>
              <span className="nav-icon">🏭</span><span>FBA Inbound</span>
            </Link>
            <Link to="/vendor-orders" className={`nav-item ${isActive('/vendor-orders') ? 'active' : ''}`}>
              <span className="nav-icon">🏪</span><span>Vendor Orders</span>
            </Link>
          </div>

          {/* ── OPERATIONS ──────────────────────────────────────────────── */}
          <div className="nav-section">
            <div className="nav-section-title">OPERATIONS</div>
            <Link to="/messages" className={`nav-item ${isActivePrefix('/messages') ? 'active' : ''}`}>
              <span className="nav-icon">💬</span>
              <span>Messages</span>
              {msgUnread > 0 && (
                <span style={{ marginLeft: 'auto', background: '#ef4444', color: 'white', borderRadius: '999px', fontSize: '11px', fontWeight: 700, padding: '1px 7px', lineHeight: '16px' }}>
                  {msgUnread}
                </span>
              )}
            </Link>
            <Link to="/orders" className={`nav-item ${isActive('/orders') ? 'active' : ''}`}>
              <span className="nav-icon">🛒</span><span>Open Orders</span>
            </Link>
            <Link to="/orders/processed" className={`nav-item ${isActive('/orders/processed') ? 'active' : ''}`}>
              <span className="nav-icon">✅</span><span>Processed Orders</span>
            </Link>
            <Link to="/rmas" className={`nav-item ${isActivePrefix('/rmas') ? 'active' : ''}`}>
              <span className="nav-icon">↩️</span>
              <span>Returns</span>
              {rmaActionable > 0 && (
                <span style={{ marginLeft: 'auto', background: 'rgba(251,191,36,0.2)', color: '#fbbf24', border: '1px solid rgba(251,191,36,0.4)', borderRadius: '999px', fontSize: '11px', fontWeight: 700, padding: '1px 7px', lineHeight: '16px' }}>
                  {rmaActionable}
                </span>
              )}
            </Link>
            <Link to="/purchase-orders" className={`nav-item ${isActivePrefix('/purchase-orders') ? 'active' : ''}`}>
              <span className="nav-icon">📄</span><span>Purchase Orders</span>
            </Link>
            <Link to="/dispatch" className={`nav-item ${isActive('/dispatch') ? 'active' : ''}`}>
              <span className="nav-icon">🚚</span><span>Dispatch</span>
            </Link>
            <Link to="/dispatch/console" className={`nav-item ${isActive('/dispatch/console') ? 'active' : ''}`}>
              <span className="nav-icon">🚀</span><span>Despatch Console</span>
            </Link>
            <Link to="/dispatch/label-printing" className={`nav-item ${isActive('/dispatch/label-printing') ? 'active' : ''}`}>
              <span className="nav-icon">🏷️</span><span>Label Printing</span>
            </Link>
            <Link to="/manifests" className={`nav-item ${isActive('/manifests') ? 'active' : ''}`}>
              <span className="nav-icon">📋</span><span>Manifests</span>
            </Link>
            <Link to="/pickwaves" className={`nav-item ${isActive('/pickwaves') ? 'active' : ''}`}>
              <span className="nav-icon">🌊</span><span>Pickwaves</span>
            </Link>
          </div>

          {/* ── INVENTORY ───────────────────────────────────────────────── */}
          <div className="nav-section">
            <div className="nav-section-title">INVENTORY</div>
            <Link to="/my-inventory" className={`nav-item ${isActive('/my-inventory') ? 'active' : ''}`}>
              <span className="nav-icon">📊</span><span>My Inventory</span>
            </Link>
            <Link to="/inventory" className={`nav-item ${isActive('/inventory') || isActive('/warehouse-locations') ? 'active' : ''}`}>
              <span className="nav-icon">🏗️</span><span>Warehouse Locations</span>
            </Link>
            <Link to="/storage-groups" className={`nav-item ${isActive('/storage-groups') ? 'active' : ''}`}>
              <span className="nav-icon">🗃️</span><span>Storage Groups</span>
            </Link>
            <Link to="/stock-count" className={`nav-item ${isActive('/stock-count') ? 'active' : ''}`}>
              <span className="nav-icon">📝</span><span>Stock Count</span>
            </Link>
            <Link to="/stock-in" className={`nav-item ${isActive('/stock-in') ? 'active' : ''}`}>
              <span className="nav-icon">📥</span><span>Stock In</span>
            </Link>
            <Link to="/stock-scrap" className={`nav-item ${isActive('/stock-scrap') ? 'active' : ''}`}>
              <span className="nav-icon">🗑️</span><span>Scrap History</span>
            </Link>
            <Link to="/warehouse-transfers" className={`nav-item ${isActive('/warehouse-transfers') ? 'active' : ''}`}>
              <span className="nav-icon">↔️</span><span>Transfers</span>
            </Link>
            <Link to="/picking-replenishment" className={`nav-item ${isActive('/picking-replenishment') ? 'active' : ''}`}>
              <span className="nav-icon">🔄</span><span>Pick Replenishment</span>
            </Link>
            <Link to="/fulfilment-sources" className={`nav-item ${isActive('/fulfilment-sources') ? 'active' : ''}`}>
              <span className="nav-icon">🏭</span><span>Fulfilment Sources</span>
            </Link>
          </div>

          {/* ── APPS ──────────────────────────────────────────────────── */}
          <div className="nav-section">
            <div className="nav-section-title">APPS</div>
            <Link to="/apps/store" className={`nav-item ${isActive('/apps/store') ? 'active' : ''}`}>
              <span className="nav-icon">🏪</span><span>Application Store</span>
            </Link>
            <Link to="/apps/installed" className={`nav-item ${isActive('/apps/installed') ? 'active' : ''}`}>
              <span className="nav-icon">📱</span><span>My Applications</span>
            </Link>
            <Link to="/automation-logs" className={`nav-item ${isActive('/automation-logs') ? 'active' : ''}`}>
              <span className="nav-icon">📋</span><span>Automation Logs</span>
            </Link>
          </div>

          {/* ── FULFILMENT ──────────────────────────────────────────────── */}
          <div className="nav-section">
            <div className="nav-section-title">FULFILMENT</div>
            <Link to="/workflows" className={`nav-item ${isActivePrefix('/workflows') ? 'active' : ''}`}>
              <span className="nav-icon">⚙️</span><span>Workflows</span>
            </Link>
            <Link to="/automation-rules" className={`nav-item ${isActive('/automation-rules') ? 'active' : ''}`}>
              <span className="nav-icon">🤖</span><span>Automation Rules</span>
            </Link>
            <Link to="/forecasting" className={`nav-item ${isActive('/forecasting') ? 'active' : ''}`}>
              <span className="nav-icon">📈</span><span>Forecasting</span>
            </Link>
            <Link to="/replenishment" className={`nav-item ${isActive('/replenishment') ? 'active' : ''}`}>
              <span className="nav-icon">🔃</span><span>Replenishment</span>
            </Link>
            <Link to="/suppliers" className={`nav-item ${isActivePrefix('/suppliers') ? 'active' : ''}`}>
              <span className="nav-icon">🤝</span><span>Suppliers</span>
            </Link>
            <Link to="/import-export" className={`nav-item ${isActivePrefix('/import-export') ? 'active' : ''}`}>
              <span className="nav-icon">📤</span><span>Import / Export</span>
            </Link>
            <Link to="/postage-definitions" className={`nav-item ${isActive('/postage-definitions') ? 'active' : ''}`}>
              <span className="nav-icon">📮</span><span>Postage Rules</span>
            </Link>
          </div>

          {/* ── CHANNELS ────────────────────────────────────────────────── */}
          <div className="nav-section">
            <div className="nav-section-title">CHANNELS</div>
            <Link to="/price-sync" className={`nav-item ${isActive('/price-sync') ? 'active' : ''}`}>
              <span className="nav-icon">💱</span><span>Price Sync</span>
            </Link>
          </div>

          {/* ── EMAILS ─────────────────────────────────────────────────── */}
          <div className="nav-section">
            <div className="nav-section-title">EMAILS</div>
            <Link to="/email-templates" className={`nav-item ${isActive('/email-templates') ? 'active' : ''}`}>
              <span className="nav-icon">✉️</span><span>Email Templates</span>
            </Link>
            <Link to="/email-logs" className={`nav-item ${isActive('/email-logs') ? 'active' : ''}`}>
              <span className="nav-icon">📬</span><span>Email Logs</span>
            </Link>
            <Link to="/sent-mail" className={`nav-item ${isActive('/sent-mail') ? 'active' : ''}`}>
              <span className="nav-icon">📤</span><span>Sent Mail Log</span>
            </Link>
          </div>

          {/* ── ANALYTICS ───────────────────────────────────────────────── */}
          <div className="nav-section">
            <div className="nav-section-title">ANALYTICS</div>
            <Link to="/analytics" className={`nav-item ${isActive('/analytics') ? 'active' : ''}`}>
              <span className="nav-icon">📈</span><span>Analytics</span>
            </Link>
            <Link to="/analytics/inventory" className={`nav-item ${isActive('/analytics/inventory') ? 'active' : ''}`}>
              <span className="nav-icon">🏭</span><span>Inventory Dashboard</span>
            </Link>
            <Link to="/analytics/orders" className={`nav-item ${isActive('/analytics/orders') ? 'active' : ''}`}>
              <span className="nav-icon">📋</span><span>Order Dashboard</span>
            </Link>
            <Link to="/analytics/pivot" className={`nav-item ${isActive('/analytics/pivot') ? 'active' : ''}`}>
              <span className="nav-icon">🔀</span><span>Pivotal Analytics</span>
            </Link>
            <Link to="/analytics/reporting" className={`nav-item ${isActive('/analytics/reporting') ? 'active' : ''}`}>
              <span className="nav-icon">📊</span><span>Reports</span>
            </Link>
            <Link to="/analytics/operational" className={`nav-item ${isActive('/analytics/operational') ? 'active' : ''}`}>
              <span className="nav-icon">🎛️</span><span>Operational</span>
            </Link>
            <Link to="/reports" className={`nav-item ${isActive('/reports') ? 'active' : ''}`}>
              <span className="nav-icon">🔍</span><span>Report Builder</span>
            </Link>
          </div>

          {/* ── SETTINGS ────────────────────────────────────────────────── */}
          <div className="nav-section">
            <Link to="/settings" className={`nav-item ${isActivePrefix('/settings') ? 'active' : ''}`}>
              <span className="nav-icon">⚙️</span><span>Settings</span>
            </Link>
          </div>

          {/* SUPPORT — flyout with marketplace sub-menus */}
          <div className="nav-section" style={{ marginTop: 'auto' }} ref={supportRef}>
            <div className="nav-section-title">SUPPORT</div>
            <div style={{ position: 'relative' }}>
              <button
                onClick={() => {
                  setSupportOpen(!supportOpen);
                  if (!supportOpen) resolveAmazonCred();
                }}
                className={`nav-item ${supportOpen ? 'active' : ''}`}
                style={{
                  width: '100%', textAlign: 'left', background: 'none', border: 'none',
                  cursor: 'pointer', color: 'inherit', font: 'inherit', padding: undefined,
                  display: 'flex', alignItems: 'center',
                }}
              >
                <span className="nav-icon">🛠️</span>
                <span style={{ flex: 1 }}>Dev Tools</span>
                <span style={{ fontSize: 10, opacity: 0.5, marginLeft: 4 }}>{supportOpen ? '▼' : '▶'}</span>
              </button>

              {supportOpen && (
                <div style={{
                  marginLeft: 8, borderLeft: '2px solid rgba(255,255,255,0.1)',
                  paddingLeft: 8, marginTop: 2, marginBottom: 4,
                }}>
                  {/* Ops Console — unified cross-tenant job visibility */}
                  <Link
                    to="/dev/seed"
                    className={`nav-item ${isActive('/dev/seed') ? 'active' : ''}`}
                    onClick={() => { setSupportOpen(false); setAmazonOpen(false); }}
                    style={{
                      textDecoration: 'none',
                      display: 'flex', alignItems: 'center', padding: '6px 8px',
                      borderRadius: 6, fontSize: 13,
                    }}
                  >
                    <span style={{ marginRight: 8, fontSize: 14 }}>🌱</span>
                    <span>Data Seeder</span>
                  </Link>
                  <Link
                    to="/ops"
                    className={`nav-item ${isActive('/ops') ? 'active' : ''}`}
                    onClick={() => { setSupportOpen(false); setAmazonOpen(false); }}
                    style={{
                      textDecoration: 'none',
                      display: 'flex', alignItems: 'center', padding: '6px 8px',
                      borderRadius: 6, fontSize: 13,
                    }}
                  >
                    <span style={{ marginRight: 8, fontSize: 14 }}>🖥️</span>
                    <span>Ops Console</span>
                  </Link>
                  <Link
                    to="/dev/jobs"
                    className={`nav-item ${isActive('/dev/jobs') ? 'active' : ''}`}
                    onClick={() => { setSupportOpen(false); setAmazonOpen(false); }}
                    style={{
                      textDecoration: 'none',
                      display: 'flex', alignItems: 'center', padding: '6px 8px',
                      borderRadius: 6, fontSize: 13,
                    }}
                  >
                    <span style={{ marginRight: 8, fontSize: 14 }}>⚙️</span>
                    <span>Job Monitor</span>
                  </Link>

                  {/* Amazon sub-menu */}
                  <button
                    onClick={() => setAmazonOpen(!amazonOpen)}
                    className="nav-item"
                    style={{
                      width: '100%', textAlign: 'left', background: 'none', border: 'none',
                      cursor: 'pointer', color: 'inherit', font: 'inherit',
                      display: 'flex', alignItems: 'center', padding: '6px 8px',
                      borderRadius: 6, fontSize: 13,
                    }}
                  >
                    <span style={{ marginRight: 8, fontSize: 14 }}>📦</span>
                    <span style={{ flex: 1 }}>Amazon</span>
                    <span style={{ fontSize: 10, opacity: 0.5 }}>{amazonOpen ? '▼' : '▶'}</span>
                  </button>

                  {amazonOpen && (
                    <div style={{ marginLeft: 12, marginTop: 2 }}>
                      <button
                        onClick={navigateToSchemaManager}
                        className={`nav-item ${isActivePrefix('/marketplace/amazon/schemas') ? 'active' : ''}`}
                        style={{
                          width: '100%', textAlign: 'left', background: 'none', border: 'none',
                          cursor: 'pointer', color: 'inherit', font: 'inherit',
                          display: 'flex', alignItems: 'center', padding: '5px 8px',
                          borderRadius: 6, fontSize: 12,
                        }}
                      >
                        <span style={{ marginRight: 6, fontSize: 12 }}>📋</span>
                        <span>Schema Manager</span>
                      </button>
                    </div>
                  )}

                  {/* Schema Cache Manager */}
                  <Link
                    to="/admin/schema-cache"
                    className={`nav-item ${isActive('/admin/schema-cache') ? 'active' : ''}`}
                    onClick={() => { setSupportOpen(false); setAmazonOpen(false); }}
                    style={{
                      textDecoration: 'none',
                      display: 'flex', alignItems: 'center', padding: '6px 8px',
                      borderRadius: 6, fontSize: 13,
                    }}
                  >
                    <span style={{ marginRight: 8, fontSize: 14 }}>🗄️</span>
                    <span>Schema Cache</span>
                  </Link>

                  {/* Typesense Management */}
                  <Link
                    to="/dev/typesense"
                    className={`nav-item ${isActive('/dev/typesense') ? 'active' : ''}`}
                    onClick={() => { setSupportOpen(false); setAmazonOpen(false); }}
                    style={{
                      textDecoration: 'none',
                      display: 'flex', alignItems: 'center', padding: '6px 8px',
                      borderRadius: 6, fontSize: 13,
                    }}
                  >
                    <span style={{ marginRight: 8, fontSize: 14 }}>🔍</span>
                    <span>Typesense</span>
                  </Link>
                </div>
              )}
            </div>
          </div>
        </nav>

        <div className="sidebar-footer">
          <TenantSwitcher />

          {/* Session 1: User profile dropdown */}
          <div style={{ position: 'relative', marginTop: 8 }} ref={profileDropdownRef}>
            <button
              onClick={() => setProfileDropdownOpen(!profileDropdownOpen)}
              style={{
                width: '100%', padding: '9px 12px', display: 'flex', alignItems: 'center', gap: 10,
                background: 'transparent', border: '1px solid var(--border)', borderRadius: 8,
                color: 'var(--text-secondary)', fontSize: 13, cursor: 'pointer',
              }}
            >
              <div style={{
                width: 26, height: 26, borderRadius: '50%', background: 'var(--primary)',
                display: 'flex', alignItems: 'center', justifyContent: 'center',
                fontSize: 12, color: '#fff', fontWeight: 700, flexShrink: 0,
              }}>
                {(user?.email || '?').charAt(0).toUpperCase()}
              </div>
              <span style={{ flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', textAlign: 'left' }}>
                {user?.email?.split('@')[0] || 'Account'}
              </span>
              <span style={{ fontSize: 10, opacity: 0.5 }}>{profileDropdownOpen ? '▲' : '▼'}</span>
            </button>

            {profileDropdownOpen && (
              <div style={{
                position: 'absolute', bottom: '100%', left: 0, right: 0, marginBottom: 4,
                background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8,
                boxShadow: '0 8px 24px rgba(0,0,0,0.3)', overflow: 'hidden', zIndex: 400,
              }}>
                <Link
                  to="/settings/profile"
                  onClick={() => setProfileDropdownOpen(false)}
                  style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '10px 14px', color: 'var(--text-secondary)', textDecoration: 'none', fontSize: 13 }}
                >
                  <span>👤</span><span>Profile</span>
                </Link>
                <Link
                  to="/settings"
                  onClick={() => setProfileDropdownOpen(false)}
                  style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '10px 14px', color: 'var(--text-secondary)', textDecoration: 'none', fontSize: 13, borderTop: '1px solid var(--border)' }}
                >
                  <span>⚙️</span><span>Settings</span>
                </Link>
                <button
                  onClick={async () => { setProfileDropdownOpen(false); await logout(); navigate('/login'); }}
                  style={{ width: '100%', display: 'flex', alignItems: 'center', gap: 10, padding: '10px 14px', color: '#f87171', background: 'none', border: 'none', borderTop: '1px solid var(--border)', fontSize: 13, cursor: 'pointer', textAlign: 'left' }}
                >
                  <span>⏻</span><span>Log out</span>
                </button>
              </div>
            )}
          </div>
        </div>
      </aside>

      <main className="main-content"><Outlet /></main>
    </div>
  );
}
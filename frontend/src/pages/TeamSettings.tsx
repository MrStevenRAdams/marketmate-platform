import React, { useState, useEffect, useRef } from 'react';
import './TeamSettings.css';

const API = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';

// ── Types ────────────────────────────────────────────────────────────────────

type Role = 'owner' | 'admin' | 'manager' | 'viewer';
type MemberStatus = 'active' | 'invited' | 'suspended';

interface Member {
  membership_id: string;
  user_id: string;
  tenant_id: string;
  role: Role;
  status: MemberStatus;
  email: string;
  display_name: string;
  avatar_url?: string;
  invited_email?: string;
  joined_at?: string;
  created_at: string;
  permissions?: Record<string, boolean>;
  group_ids?: string[];
  group_names?: string[];
}

interface Invitation {
  token: string;
  invited_email: string;
  role: Role;
  expires_at: string;
  created_at: string;
}

interface UserGroup {
  id: string;
  tenant_id: string;
  name: string;
  description: string;
  permissions: Record<string, boolean>;
  member_ids: string[];
  created_at: string;
  updated_at: string;
}

interface AuditEvent {
  id: string;
  tenant_id: string;
  actor_uid: string;
  actor_email: string;
  event_type: string;
  target_uid?: string;
  target_email?: string;
  metadata?: Record<string, unknown>;
  created_at: string;
}

// ── Full Permission Key Set ──────────────────────────────────────────────────
const ALL_PERMISSION_KEYS = [
  // General
  'general.topbar','general.sync_status','general.account_management','general.notifications',
  // Inventory
  'inventory.adjust','inventory.stock_adjustments','products.delete',
  'inventory.suppliers','inventory.purchase_orders','inventory.stock_takes',
  // Orders
  'orders.create','orders.delete','orders.merge','orders.split','orders.cancel',
  'orders.view','orders.edit','orders.refund',
  // Shipping
  'dispatch.create','shipping.labels','shipping.services','shipping.tracking',
  // Dashboards
  'reports.view','reports.export','reports.financial',
  // Email
  'email.send','email.send_adhoc','email.resend','email.templates','email.view_sent','email.accounts',
  // Apps
  'apps.macros','apps.installed','apps.automation_logs',
  // Settings
  'settings.configurators','settings.import_export','settings.currency','settings.team',
  'settings.channels','settings.general','settings.data_purge','settings.extract',
  'settings.templates','settings.automation_logs','settings.countries',
  // Legacy
  'rmas.authorise','billing.manage',
] as const;
type PermKey = typeof ALL_PERMISSION_KEYS[number];

// Tabbed groupings for the permission matrix UI
const PERMISSION_TABS: { label: string; keys: PermKey[] }[] = [
  { label: 'General', keys: ['general.topbar','general.sync_status','general.account_management','general.notifications'] },
  { label: 'Inventory', keys: ['inventory.adjust','inventory.stock_adjustments','products.delete','inventory.suppliers','inventory.purchase_orders','inventory.stock_takes'] },
  { label: 'Orders', keys: ['orders.create','orders.delete','orders.merge','orders.split','orders.cancel','orders.view','orders.edit','orders.refund'] },
  { label: 'Shipping', keys: ['dispatch.create','shipping.labels','shipping.services','shipping.tracking'] },
  { label: 'Dashboards', keys: ['reports.view','reports.export','reports.financial'] },
  { label: 'Email', keys: ['email.send','email.send_adhoc','email.resend','email.templates','email.view_sent','email.accounts'] },
  { label: 'Apps', keys: ['apps.macros','apps.installed','apps.automation_logs'] },
  { label: 'Settings', keys: ['settings.configurators','settings.import_export','settings.currency','settings.team','settings.channels','settings.general','settings.data_purge','settings.extract','settings.templates','settings.automation_logs','settings.countries'] },
];

const PERM_LABELS: Record<PermKey, string> = {
  'general.topbar': 'Top bar panel access',
  'general.sync_status': 'Sync status visibility',
  'general.account_management': 'Account management',
  'general.notifications': 'Notifications',
  'inventory.adjust': 'Warehouse management',
  'inventory.stock_adjustments': 'Stock adjustments',
  'products.delete': 'Delete products',
  'inventory.suppliers': 'Supplier management',
  'inventory.purchase_orders': 'Purchase orders',
  'inventory.stock_takes': 'Stock takes',
  'orders.create': 'Create orders',
  'orders.delete': 'Delete orders',
  'orders.merge': 'Merge orders',
  'orders.split': 'Split orders',
  'orders.cancel': 'Cancel orders',
  'orders.view': 'View order details',
  'orders.edit': 'Edit order details',
  'orders.refund': 'Refund orders',
  'dispatch.create': 'Create dispatch',
  'shipping.labels': 'View shipping labels',
  'shipping.services': 'Manage shipping services',
  'shipping.tracking': 'Edit tracking numbers',
  'reports.view': 'View reports',
  'reports.export': 'Export reports',
  'reports.financial': 'View financial data',
  'email.send': 'Send emails',
  'email.send_adhoc': 'Send adhoc emails',
  'email.resend': 'Resend emails',
  'email.templates': 'Email templates management',
  'email.view_sent': 'View sent emails',
  'email.accounts': 'Manage email accounts',
  'apps.macros': 'Macro configurations',
  'apps.installed': 'My applications',
  'apps.automation_logs': 'Automation logs view',
  'settings.configurators': 'Configurators',
  'settings.import_export': 'Import & export',
  'settings.currency': 'Currency rates',
  'settings.team': 'User management',
  'settings.channels': 'Channel integration',
  'settings.general': 'General settings',
  'settings.data_purge': 'Data purge',
  'settings.extract': 'Extract inventory',
  'settings.templates': 'Template designer',
  'settings.automation_logs': 'Automation logs',
  'settings.countries': 'Countries management',
  'rmas.authorise': 'Authorise returns',
  'billing.manage': 'Manage billing',
};

function makeRoleDefaults(role: Role): Record<PermKey, boolean> {
  const isViewer = role === 'viewer';
  const isAdminOrOwner = role === 'owner' || role === 'admin';
  const isOwner = role === 'owner';
  const d: Partial<Record<PermKey, boolean>> = {};
  ALL_PERMISSION_KEYS.forEach(k => { d[k] = false; });
  // General
  d['general.topbar'] = true; d['general.sync_status'] = true;
  d['general.account_management'] = isAdminOrOwner; d['general.notifications'] = true;
  // Inventory
  d['inventory.adjust'] = !isViewer; d['inventory.stock_adjustments'] = !isViewer;
  d['products.delete'] = isAdminOrOwner; d['inventory.suppliers'] = !isViewer;
  d['inventory.purchase_orders'] = !isViewer; d['inventory.stock_takes'] = !isViewer;
  // Orders
  d['orders.create'] = !isViewer; d['orders.delete'] = isAdminOrOwner;
  d['orders.merge'] = !isViewer; d['orders.split'] = !isViewer; d['orders.cancel'] = !isViewer;
  d['orders.view'] = true; d['orders.edit'] = !isViewer; d['orders.refund'] = isAdminOrOwner;
  // Shipping
  d['dispatch.create'] = !isViewer; d['shipping.labels'] = !isViewer;
  d['shipping.services'] = isAdminOrOwner; d['shipping.tracking'] = !isViewer;
  // Dashboards
  d['reports.view'] = true; d['reports.export'] = !isViewer; d['reports.financial'] = isAdminOrOwner;
  // Email
  d['email.send'] = !isViewer; d['email.send_adhoc'] = !isViewer; d['email.resend'] = !isViewer;
  d['email.templates'] = isAdminOrOwner; d['email.view_sent'] = !isViewer; d['email.accounts'] = isAdminOrOwner;
  // Apps
  d['apps.macros'] = isAdminOrOwner; d['apps.installed'] = !isViewer; d['apps.automation_logs'] = !isViewer;
  // Settings
  d['settings.configurators'] = isAdminOrOwner; d['settings.import_export'] = !isViewer;
  d['settings.currency'] = isAdminOrOwner; d['settings.team'] = isAdminOrOwner;
  d['settings.channels'] = isAdminOrOwner; d['settings.general'] = isAdminOrOwner;
  d['settings.data_purge'] = isOwner; d['settings.extract'] = !isViewer;
  d['settings.templates'] = isAdminOrOwner; d['settings.automation_logs'] = !isViewer;
  d['settings.countries'] = isAdminOrOwner;
  // Legacy
  d['rmas.authorise'] = !isViewer; d['billing.manage'] = isOwner;
  return d as Record<PermKey, boolean>;
}

const ROLE_DEFAULTS: Record<Role, Record<PermKey, boolean>> = {
  owner:   makeRoleDefaults('owner'),
  admin:   makeRoleDefaults('admin'),
  manager: makeRoleDefaults('manager'),
  viewer:  makeRoleDefaults('viewer'),
};

function resolvedPerm(m: Member, key: PermKey): boolean {
  if (m.permissions && key in m.permissions) return m.permissions[key];
  return ROLE_DEFAULTS[m.role]?.[key] ?? false;
}

// ── Helpers ──────────────────────────────────────────────────────────────────

const ROLE_META: Record<Role, { label: string; color: string; desc: string }> = {
  owner:   { label: 'Owner',   color: '#f59e0b', desc: 'Full access including billing and account deletion' },
  admin:   { label: 'Admin',   color: '#3b82f6', desc: 'Full operational access, manage users, view billing' },
  manager: { label: 'Manager', color: '#10b981', desc: 'All operations — orders, dispatch, listings, products' },
  viewer:  { label: 'Viewer',  color: '#64748b', desc: 'Read-only access to everything' },
};

const SORT_OPTIONS = [
  { value: 'recommended', label: 'Recommended' },
  { value: 'name_asc',    label: 'Name A–Z' },
  { value: 'name_desc',   label: 'Name Z–A' },
  { value: 'email_asc',   label: 'Email A–Z' },
  { value: 'date_newest', label: 'Date added (newest)' },
  { value: 'date_oldest', label: 'Date added (oldest)' },
] as const;
type SortOption = typeof SORT_OPTIONS[number]['value'];

const ROLE_ORDER: Record<Role, number> = { owner: 0, admin: 1, manager: 2, viewer: 3 };

function roleColor(role: Role) { return ROLE_META[role]?.color ?? '#64748b'; }
function roleLabel(role: Role) { return ROLE_META[role]?.label ?? role; }

function avatarInitials(name: string) {
  return name.split(' ').slice(0, 2).map(w => w[0]).join('').toUpperCase() || '?';
}

function timeAgo(iso: string) {
  const diff = Date.now() - new Date(iso).getTime();
  const days = Math.floor(diff / 86400000);
  if (days === 0) return 'Today';
  if (days === 1) return 'Yesterday';
  if (days < 30) return `${days}d ago`;
  const months = Math.floor(days / 30);
  return `${months}mo ago`;
}

function sortMembers(members: Member[], sort: SortOption): Member[] {
  const arr = [...members];
  switch (sort) {
    case 'recommended':
      return arr.sort((a, b) => {
        const roleDiff = ROLE_ORDER[a.role] - ROLE_ORDER[b.role];
        if (roleDiff !== 0) return roleDiff;
        return (a.display_name || a.email).localeCompare(b.display_name || b.email);
      });
    case 'name_asc':
      return arr.sort((a, b) => (a.display_name || a.email).localeCompare(b.display_name || b.email));
    case 'name_desc':
      return arr.sort((a, b) => (b.display_name || b.email).localeCompare(a.display_name || a.email));
    case 'email_asc':
      return arr.sort((a, b) => a.email.localeCompare(b.email));
    case 'date_newest':
      return arr.sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime());
    case 'date_oldest':
      return arr.sort((a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime());
  }
}

// ── Kebab Menu ────────────────────────────────────────────────────────────────

interface KebabMenuProps {
  items: { label: string; danger?: boolean; onClick: () => void }[];
}

const KebabMenu: React.FC<KebabMenuProps> = ({ items }) => {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, []);

  return (
    <div className="ts-kebab-wrap" ref={ref}>
      <button
        className="ts-kebab-btn"
        onClick={() => setOpen(o => !o)}
        title="More actions"
        aria-label="More actions"
      >
        ⋯
      </button>
      {open && (
        <div className="ts-kebab-menu">
          {items.map((item, i) => (
            <button
              key={i}
              className={`ts-kebab-item${item.danger ? ' ts-kebab-item--danger' : ''}`}
              onClick={() => { setOpen(false); item.onClick(); }}
            >
              {item.label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
};

// ── Permission Matrix Panel ───────────────────────────────────────────────────

interface PermMatrixProps {
  member: Member;
  perms: Record<PermKey, boolean>;
  saving: boolean;
  onSave: () => void;
  onCancel: () => void;
  onChange: (key: PermKey, val: boolean) => void;
  onReset: () => void;
}

const PermMatrix: React.FC<PermMatrixProps> = ({ member, perms, saving, onSave, onCancel, onChange, onReset }) => {
  const [activePermTab, setActivePermTab] = React.useState(0);
  const tab = PERMISSION_TABS[activePermTab];
  return (
    <div className="ts-perm-matrix">
      <div className="ts-perm-matrix-title">Permission overrides for {member.display_name || member.email}</div>
      <div className="ts-perm-matrix-hint">
        Unchecked permissions are inherited from the <strong>{member.role}</strong> role default.
        Toggle any permission to explicitly override it for this member.
      </div>
      {/* Permission tabs */}
      <div style={{ display: 'flex', gap: 4, marginBottom: 12, flexWrap: 'wrap' }}>
        {PERMISSION_TABS.map((t, i) => (
          <button
            key={t.label}
            onClick={() => setActivePermTab(i)}
            style={{
              padding: '4px 10px', borderRadius: 4, fontSize: 12, cursor: 'pointer', border: 'none',
              background: i === activePermTab ? 'var(--accent,#6366f1)' : 'var(--bg-secondary,#1e293b)',
              color: i === activePermTab ? '#fff' : 'var(--text-secondary,#94a3b8)',
              fontWeight: i === activePermTab ? 600 : 400,
            }}
          >
            {t.label}
          </button>
        ))}
      </div>
      <div className="ts-perm-matrix-grid">
        {tab.keys.map(key => {
          const roleDefault = ROLE_DEFAULTS[member.role]?.[key] ?? false;
          const current = perms[key];
          const isOverride = member.permissions && key in (member.permissions as Record<string,boolean>);
          return (
            <label key={key} className={`ts-perm-row ${isOverride ? 'ts-perm-row--override' : ''}`}>
              <input
                type="checkbox"
                checked={current ?? roleDefault}
                onChange={e => onChange(key, e.target.checked)}
                className="ts-perm-check"
              />
              <span className="ts-perm-key-label">{PERM_LABELS[key]}</span>
              {isOverride && <span className="ts-perm-override-badge">override</span>}
              {!isOverride && <span className="ts-perm-default-badge">role default: {roleDefault ? 'allowed' : 'denied'}</span>}
            </label>
          );
        })}
      </div>
      <div className="ts-perm-matrix-actions">
        <button className="ts-btn-primary" onClick={onSave} disabled={saving}>
          {saving ? 'Saving…' : 'Save permissions'}
        </button>
        <button className="ts-btn-ghost" onClick={onCancel}>Cancel</button>
        <button className="ts-btn-ghost ts-btn-danger" style={{ marginLeft: 'auto' }} onClick={onReset} title="Reset all overrides to role defaults">
          Reset to role defaults
        </button>
      </div>
    </div>
  );
};

// ── Permissions Overview Modal ────────────────────────────────────────────────
interface PermissionsOverviewProps {
  members: Member[];
  onClose: () => void;
}
const PermissionsOverview: React.FC<PermissionsOverviewProps> = ({ members, onClose }) => {
  const [overviewTab, setOverviewTab] = React.useState(0);
  const tab = PERMISSION_TABS[overviewTab];
  return (
    <div className="ts-modal-overlay" onClick={onClose}>
      <div className="ts-modal" style={{ maxWidth: 900, width: '95vw' }} onClick={e => e.stopPropagation()}>
        <div className="ts-modal-header">
          <h2 className="ts-modal-title">Permissions Overview</h2>
          <button className="ts-modal-close" onClick={onClose}>✕</button>
        </div>
        <div style={{ padding: '0 20px 8px', display: 'flex', gap: 4, flexWrap: 'wrap' }}>
          {PERMISSION_TABS.map((t, i) => (
            <button key={t.label} onClick={() => setOverviewTab(i)} style={{
              padding: '4px 10px', borderRadius: 4, fontSize: 12, cursor: 'pointer', border: 'none',
              background: i === overviewTab ? 'var(--accent,#6366f1)' : 'var(--bg-secondary,#1e293b)',
              color: i === overviewTab ? '#fff' : 'var(--text-secondary,#94a3b8)',
            }}>{t.label}</button>
          ))}
        </div>
        <div style={{ padding: '0 20px 20px', overflowX: 'auto' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12 }}>
            <thead>
              <tr>
                <th style={{ textAlign: 'left', padding: '6px 8px', borderBottom: '1px solid var(--border,#334155)', color: 'var(--text-secondary,#94a3b8)', fontWeight: 600, minWidth: 160 }}>Permission</th>
                {members.map(m => (
                  <th key={m.membership_id} style={{ padding: '6px 8px', borderBottom: '1px solid var(--border,#334155)', color: 'var(--text-secondary,#94a3b8)', fontWeight: 600, textAlign: 'center', maxWidth: 80 }}>
                    <div style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{m.display_name || m.email}</div>
                    <div style={{ fontWeight: 400, opacity: 0.6, fontSize: 10 }}>{m.role}</div>
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {tab.keys.map((key, i) => (
                <tr key={key} style={{ background: i % 2 === 0 ? 'transparent' : 'rgba(255,255,255,0.02)' }}>
                  <td style={{ padding: '5px 8px', color: 'var(--text-primary,#e2e8f0)' }}>{PERM_LABELS[key]}</td>
                  {members.map(m => {
                    const hasOverride = m.permissions && key in (m.permissions as Record<string,boolean>);
                    const effective = hasOverride
                      ? (m.permissions as Record<string,boolean>)[key]
                      : (ROLE_DEFAULTS[m.role]?.[key] ?? false);
                    return (
                      <td key={m.membership_id} style={{ textAlign: 'center', padding: '5px 8px' }}>
                        {effective
                          ? <span title={hasOverride ? 'Individually overridden' : 'Role default'} style={{ color: '#4ade80', fontSize: 14 }}>{hasOverride ? 'Ⓘ' : '✓'}</span>
                          : <span title={hasOverride ? 'Individually overridden' : 'Role default'} style={{ color: 'var(--text-muted,#64748b)', fontSize: 13 }}>–</span>
                        }
                      </td>
                    );
                  })}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
};

// ── Group Modal ───────────────────────────────────────────────────────────────

interface GroupModalProps {
  group?: UserGroup;
  members: Member[];
  onClose: () => void;
  onSave: (data: { name: string; description: string; permissions: Record<string,boolean> }) => void;
  onAddMember?: (membershipId: string) => void;
  onRemoveMember?: (membershipId: string) => void;
}

const GroupModal: React.FC<GroupModalProps> = ({ group, members, onClose, onSave, onAddMember, onRemoveMember }) => {
  const [name, setName] = useState(group?.name ?? '');
  const [description, setDescription] = useState(group?.description ?? '');
  const [perms, setPerms] = useState<Record<string, boolean>>(group?.permissions ?? {});
  const [memberSearch, setMemberSearch] = useState('');

  const currentMemberIds = group?.member_ids ?? [];
  const filteredMembers = members.filter(m =>
    (m.display_name || m.email).toLowerCase().includes(memberSearch.toLowerCase())
  );

  return (
    <div className="ts-modal-overlay" onClick={onClose}>
      <div className="ts-modal" onClick={e => e.stopPropagation()}>
        <div className="ts-modal-header">
          <h2 className="ts-modal-title">{group ? 'Edit Group' : 'Create Group'}</h2>
          <button className="ts-modal-close" onClick={onClose}>✕</button>
        </div>
        <div className="ts-modal-body">
          <div className="ts-field">
            <label>Group name</label>
            <input
              type="text"
              value={name}
              onChange={e => setName(e.target.value)}
              placeholder="e.g. Warehouse Team"
              autoFocus
            />
          </div>
          <div className="ts-field" style={{ marginTop: 12 }}>
            <label>Description</label>
            <input
              type="text"
              value={description}
              onChange={e => setDescription(e.target.value)}
              placeholder="Optional description"
            />
          </div>

          <div style={{ marginTop: 20 }}>
            <div className="ts-section-label">Permission grants</div>
            <div className="ts-perm-matrix-hint" style={{ marginBottom: 8 }}>
              Permissions granted here are merged with each member's role defaults (most permissive wins).
            </div>
            <div className="ts-perm-matrix-grid">
              {ALL_PERMISSION_KEYS.map(key => (
                <label key={key} className="ts-perm-row">
                  <input
                    type="checkbox"
                    checked={!!perms[key]}
                    onChange={e => setPerms(p => ({ ...p, [key]: e.target.checked }))}
                    className="ts-perm-check"
                  />
                  <span className="ts-perm-key-label">{PERM_LABELS[key as PermKey]}</span>
                </label>
              ))}
            </div>
          </div>

          {group && onAddMember && onRemoveMember && (
            <div style={{ marginTop: 20 }}>
              <div className="ts-section-label">Members ({currentMemberIds.length})</div>
              <input
                type="search"
                className="ts-search-input"
                placeholder="Search members…"
                value={memberSearch}
                onChange={e => setMemberSearch(e.target.value)}
                style={{ marginBottom: 8 }}
              />
              <div className="ts-group-member-list">
                {filteredMembers.map(m => {
                  const inGroup = currentMemberIds.includes(m.membership_id);
                  return (
                    <div key={m.membership_id} className="ts-group-member-row">
                      <div className="ts-avatar" style={{ width: 28, height: 28, fontSize: 11, background: stringToColor(m.user_id) }}>
                        {avatarInitials(m.display_name || m.email)}
                      </div>
                      <span className="ts-group-member-name">{m.display_name || m.email}</span>
                      <button
                        className={`ts-btn-ghost${inGroup ? ' ts-btn-danger' : ''}`}
                        style={{ padding: '3px 10px', fontSize: 11 }}
                        onClick={() => inGroup ? onRemoveMember(m.membership_id) : onAddMember(m.membership_id)}
                      >
                        {inGroup ? 'Remove' : 'Add'}
                      </button>
                    </div>
                  );
                })}
              </div>
            </div>
          )}
        </div>
        <div className="ts-modal-footer">
          <button className="ts-btn-ghost" onClick={onClose}>Cancel</button>
          <button
            className="ts-btn-primary"
            disabled={!name.trim()}
            onClick={() => onSave({ name, description, permissions: perms })}
          >
            {group ? 'Save changes' : 'Create group'}
          </button>
        </div>
      </div>
    </div>
  );
};

// ── Main Component ────────────────────────────────────────────────────────────

type Tab = 'members' | 'groups' | 'audit';

const TeamSettings: React.FC = () => {
  const [activeTab, setActiveTab] = useState<Tab>('members');
  const [members, setMembers] = useState<Member[]>([]);
  const [invitations, setInvitations] = useState<Invitation[]>([]);
  const [groups, setGroups] = useState<UserGroup[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  // Search + Sort
  const [searchQuery, setSearchQuery] = useState('');
  const [sortOption, setSortOption] = useState<SortOption>('recommended');

  // Invite form
  const [showInvite, setShowInvite] = useState(false);
  const [inviteEmail, setInviteEmail] = useState('');
  const [inviteRole, setInviteRole] = useState<Role>('manager');
  const [inviteDisplayName, setInviteDisplayName] = useState('');
  const [showInvitePerms, setShowInvitePerms] = useState(false);
  const [invitePerms, setInvitePerms] = useState<Record<string, boolean>>({});
  const [inviting, setInviting] = useState(false);
  const [inviteSuccess, setInviteSuccess] = useState('');
  const [inviteError, setInviteError] = useState('');

  // Role change
  const [changingRole, setChangingRole] = useState<string | null>(null);

  // Permissions editing
  const [expandedPerms, setExpandedPerms] = useState<string | null>(null);
  const [pendingPerms, setPendingPerms] = useState<Record<string, Record<PermKey, boolean>>>({});
  const [savingPerms, setSavingPerms] = useState<string | null>(null);

  // Password reset confirm
  const [resetConfirm, setResetConfirm] = useState<{ membershipId: string; email: string } | null>(null);

  // Member security info (2FA, last login) — keyed by membershipId
  const [securityInfo, setSecurityInfo] = useState<Record<string, { twoFactorEnabled: boolean; lastLoginAt: string }>>({});

  // Groups UI
  const [showGroupModal, setShowGroupModal] = useState(false);
  const [showOverview, setShowOverview] = useState(false);
  const [editingGroup, setEditingGroup] = useState<UserGroup | null>(null);

  // Toast
  const [toast, setToast] = useState<{ msg: string; type: 'success' | 'error' } | null>(null);

  // Audit Log tab state
  const [auditEvents, setAuditEvents] = useState<AuditEvent[]>([]);
  const [auditLoading, setAuditLoading] = useState(false);
  const [auditFilter, setAuditFilter] = useState({ event_type: '', search: '', date_from: '', date_to: '' });
  const [auditDetail, setAuditDetail] = useState<AuditEvent | null>(null);

  const tenantId = localStorage.getItem('marketmate_active_tenant') ?? '';
  const headers = {
    'Content-Type': 'application/json',
    'X-Tenant-Id': tenantId,
  };

  const showToast = (msg: string, type: 'success' | 'error' = 'success') => {
    setToast({ msg, type });
    setTimeout(() => setToast(null), 3500);
  };

  // ── Load data ───────────────────────────────────────────────────────────────

  const load = async () => {
    setLoading(true);
    setError('');
    try {
      const [mRes, iRes, gRes] = await Promise.all([
        fetch(`${API}/users/members`, { headers }),
        fetch(`${API}/users/invitations`, { headers }),
        fetch(`${API}/user-groups`, { headers }),
      ]);
      if (mRes.ok) {
        const d = await mRes.json();
        const members = d.members ?? [];
        setMembers(members);
        // Fetch security info (2FA + last login) for each member in background
        members.forEach((m: Member) => {
          fetch(`${API}/users/${m.membership_id}/security-info`, { headers })
            .then(r => r.ok ? r.json() : null)
            .then(info => {
              if (info) setSecurityInfo(prev => ({ ...prev, [m.membership_id]: info }));
            })
            .catch(() => {});
        });
      }
      if (iRes.ok) {
        const d = await iRes.json();
        setInvitations(d.invitations ?? []);
      }
      if (gRes.ok) {
        const d = await gRes.json();
        setGroups(d.groups ?? []);
      }
    } catch {
      setError('Failed to load team data');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { load(); }, []);

  // Load audit log when switching to audit tab
  useEffect(() => {
    if (activeTab === 'audit' && auditEvents.length === 0) {
      loadAuditLog();
    }
  }, [activeTab]);

  async function loadAuditLog() {
    setAuditLoading(true);
    try {
      const params = new URLSearchParams({ limit: '50' });
      if (auditFilter.event_type) params.set('event_type', auditFilter.event_type);
      if (auditFilter.date_from) params.set('date_from', auditFilter.date_from);
      if (auditFilter.date_to) params.set('date_to', auditFilter.date_to);
      const res = await api(`/user-audit-log?${params}`);
      if (res.ok) {
        const data = await res.json();
        setAuditEvents(data.events || []);
      }
    } catch { /* silent */ } finally {
      setAuditLoading(false);
    }
  }

  // ── Derived: search + sort ──────────────────────────────────────────────────

  const activeMembers = members.filter(m => m.status === 'active');

  const filteredMembers = searchQuery.trim()
    ? activeMembers.filter(m => {
        const q = searchQuery.toLowerCase();
        return (m.display_name || '').toLowerCase().includes(q) || m.email.toLowerCase().includes(q);
      })
    : activeMembers;

  const displayedMembers = sortMembers(filteredMembers, sortOption);

  // ── Invite ──────────────────────────────────────────────────────────────────

  const handleInvite = async (e: React.FormEvent) => {
    e.preventDefault();
    setInviteError('');
    setInviteSuccess('');
    setInviting(true);
    try {
      const body: Record<string, unknown> = { email: inviteEmail, role: inviteRole };
      if (inviteDisplayName.trim()) body.display_name = inviteDisplayName.trim();
      if (showInvitePerms && Object.keys(invitePerms).length > 0) {
        body.permission_overrides = invitePerms;
      }
      const res = await fetch(`${API}/users/invite`, {
        method: 'POST',
        headers,
        body: JSON.stringify(body),
      });
      const data = await res.json();
      if (!res.ok) {
        setInviteError(data.error ?? 'Failed to send invitation');
        return;
      }
      setInviteSuccess(`Invitation sent to ${inviteEmail}`);
      setInviteEmail('');
      setInviteDisplayName('');
      setInvitePerms({});
      setShowInvitePerms(false);
      load();
    } catch {
      setInviteError('Network error');
    } finally {
      setInviting(false);
    }
  };

  // ── Role change ─────────────────────────────────────────────────────────────

  const handleRoleChange = async (membershipId: string, newRole: Role) => {
    setChangingRole(membershipId);
    try {
      const res = await fetch(`${API}/users/members/${membershipId}/role`, {
        method: 'PUT',
        headers,
        body: JSON.stringify({ role: newRole }),
      });
      if (res.ok) {
        showToast('Role updated');
        load();
      } else {
        const d = await res.json();
        showToast(d.error ?? 'Failed to update role', 'error');
      }
    } catch {
      showToast('Network error', 'error');
    } finally {
      setChangingRole(null);
    }
  };

  // ── Remove member ───────────────────────────────────────────────────────────

  const handleRemove = async (membershipId: string, name: string) => {
    if (!confirm(`Remove ${name} from this team?`)) return;
    try {
      const res = await fetch(`${API}/users/members/${membershipId}`, { method: 'DELETE', headers });
      if (res.ok) {
        showToast(`${name} removed`);
        load();
      } else {
        const d = await res.json();
        showToast(d.error ?? 'Failed to remove member', 'error');
      }
    } catch {
      showToast('Network error', 'error');
    }
  };

  // ── Revoke invite ───────────────────────────────────────────────────────────

  const handleRevoke = async (token: string, email: string) => {
    if (!confirm(`Cancel invitation to ${email}?`)) return;
    try {
      const res = await fetch(`${API}/users/invitations/${token}`, { method: 'DELETE', headers });
      if (res.ok) { showToast('Invitation cancelled'); load(); }
      else showToast('Failed to cancel', 'error');
    } catch { showToast('Network error', 'error'); }
  };

  // ── Save permissions ────────────────────────────────────────────────────────

  const handleSavePermissions = async (membershipId: string) => {
    const permsToSave = pendingPerms[membershipId];
    if (!permsToSave) return;
    setSavingPerms(membershipId);
    try {
      const res = await fetch(`${API}/users/members/${membershipId}/permissions`, {
        method: 'PUT',
        headers,
        body: JSON.stringify({ permissions: permsToSave }),
      });
      if (res.ok) {
        showToast('Permissions saved');
        setExpandedPerms(null);
        load();
      } else {
        const d = await res.json();
        showToast(d.error ?? 'Failed to save permissions', 'error');
      }
    } catch { showToast('Network error', 'error'); }
    finally { setSavingPerms(null); }
  };

  const openPerms = (m: Member) => {
    const initial = Object.fromEntries(ALL_PERMISSION_KEYS.map(k => [k, resolvedPerm(m, k)])) as Record<PermKey, boolean>;
    setPendingPerms(prev => ({ ...prev, [m.membership_id]: initial }));
    setExpandedPerms(m.membership_id);
  };

  // ── Password reset ──────────────────────────────────────────────────────────

  const handlePasswordReset = async () => {
    if (!resetConfirm) return;
    const { membershipId, email } = resetConfirm;
    setResetConfirm(null);
    try {
      const res = await fetch(`${API}/users/members/${membershipId}/send-password-reset`, {
        method: 'POST',
        headers,
      });
      if (res.ok) showToast(`Password reset email sent to ${email}`);
      else { const d = await res.json(); showToast(d.error ?? 'Failed to send reset email', 'error'); }
    } catch { showToast('Network error', 'error'); }
  };

  // ── Groups ──────────────────────────────────────────────────────────────────

  const handleCreateGroup = async (data: { name: string; description: string; permissions: Record<string,boolean> }) => {
    try {
      const res = await fetch(`${API}/user-groups`, {
        method: 'POST',
        headers,
        body: JSON.stringify(data),
      });
      if (res.ok) { showToast('Group created'); setShowGroupModal(false); load(); }
      else { const d = await res.json(); showToast(d.error ?? 'Failed to create group', 'error'); }
    } catch { showToast('Network error', 'error'); }
  };

  const handleUpdateGroup = async (data: { name: string; description: string; permissions: Record<string,boolean> }) => {
    if (!editingGroup) return;
    try {
      const res = await fetch(`${API}/user-groups/${editingGroup.id}`, {
        method: 'PUT',
        headers,
        body: JSON.stringify(data),
      });
      if (res.ok) { showToast('Group updated'); setEditingGroup(null); load(); }
      else { const d = await res.json(); showToast(d.error ?? 'Failed to update group', 'error'); }
    } catch { showToast('Network error', 'error'); }
  };

  const handleDeleteGroup = async (g: UserGroup) => {
    if (!confirm(`Delete group "${g.name}"? This will remove members from the group.`)) return;
    try {
      const res = await fetch(`${API}/user-groups/${g.id}`, { method: 'DELETE', headers });
      if (res.ok) { showToast('Group deleted'); load(); }
      else { const d = await res.json(); showToast(d.error ?? 'Failed to delete group', 'error'); }
    } catch { showToast('Network error', 'error'); }
  };

  const handleGroupAddMember = async (membershipId: string) => {
    if (!editingGroup) return;
    try {
      const res = await fetch(`${API}/user-groups/${editingGroup.id}/members`, {
        method: 'POST',
        headers,
        body: JSON.stringify({ membership_id: membershipId }),
      });
      if (res.ok) { showToast('Member added to group'); load(); }
      else { const d = await res.json(); showToast(d.error ?? 'Failed to add member', 'error'); }
    } catch { showToast('Network error', 'error'); }
  };

  const handleGroupRemoveMember = async (membershipId: string) => {
    if (!editingGroup) return;
    try {
      const res = await fetch(`${API}/user-groups/${editingGroup.id}/members/${membershipId}`, {
        method: 'DELETE',
        headers,
      });
      if (res.ok) { showToast('Member removed from group'); load(); }
      else { const d = await res.json(); showToast(d.error ?? 'Failed to remove member', 'error'); }
    } catch { showToast('Network error', 'error'); }
  };

  // ── Render ──────────────────────────────────────────────────────────────────

  return (
    <div className="ts-page">
      {/* Toast */}
      {toast && <div className={`ts-toast ts-toast--${toast.type}`}>{toast.msg}</div>}

      {/* Password reset confirm dialog */}
      {resetConfirm && (
        <div className="ts-modal-overlay" onClick={() => setResetConfirm(null)}>
          <div className="ts-modal ts-modal--sm" onClick={e => e.stopPropagation()}>
            <div className="ts-modal-header">
              <h2 className="ts-modal-title">Send password reset?</h2>
              <button className="ts-modal-close" onClick={() => setResetConfirm(null)}>✕</button>
            </div>
            <div className="ts-modal-body">
              <p style={{ margin: 0, fontSize: 14, color: 'var(--text-secondary, #94a3b8)' }}>
                A password reset email will be sent to <strong style={{ color: 'var(--text-primary, #e2e8f0)' }}>{resetConfirm.email}</strong>.
              </p>
            </div>
            <div className="ts-modal-footer">
              <button className="ts-btn-ghost" onClick={() => setResetConfirm(null)}>Cancel</button>
              <button className="ts-btn-primary" onClick={handlePasswordReset}>Send reset email</button>
            </div>
          </div>
        </div>
      )}

      {/* Group modal */}
      {showOverview && (
        <PermissionsOverview members={activeMembers} onClose={() => setShowOverview(false)} />
      )}
      {(showGroupModal || editingGroup) && (
        <GroupModal
          group={editingGroup ?? undefined}
          members={activeMembers}
          onClose={() => { setShowGroupModal(false); setEditingGroup(null); }}
          onSave={editingGroup ? handleUpdateGroup : handleCreateGroup}
          onAddMember={editingGroup ? handleGroupAddMember : undefined}
          onRemoveMember={editingGroup ? handleGroupRemoveMember : undefined}
        />
      )}

      {/* Header */}
      <div className="ts-header">
        <div>
          <h1 className="ts-title">Team</h1>
          <p className="ts-subtitle">
            {activeMembers.length} member{activeMembers.length !== 1 ? 's' : ''}
            {invitations.length > 0 && ` · ${invitations.length} pending invite${invitations.length !== 1 ? 's' : ''}`}
          </p>
        </div>
        <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
          <button className="ts-btn-ghost" onClick={() => setShowOverview(true)} style={{ fontSize: 13 }}>
            🔍 Permissions Overview
          </button>
          <button className="ts-btn-primary" onClick={() => { setShowInvite(!showInvite); setInviteSuccess(''); setInviteError(''); }}>
            {showInvite ? '✕ Cancel' : '+ Invite member'}
          </button>
        </div>
      </div>

      {/* Invite form */}
      {showInvite && (
        <div className="ts-invite-panel">
          <h3 className="ts-invite-title">Invite a team member</h3>
          <form onSubmit={handleInvite} className="ts-invite-form">
            <div className="ts-invite-fields">
              <div className="ts-field">
                <label>Email address</label>
                <input
                  type="email"
                  value={inviteEmail}
                  onChange={e => setInviteEmail(e.target.value)}
                  placeholder="colleague@company.com"
                  required
                  autoFocus
                />
              </div>
              <div className="ts-field">
                <label>Display name <span style={{ color: 'var(--text-muted)' }}>(optional)</span></label>
                <input
                  type="text"
                  value={inviteDisplayName}
                  onChange={e => setInviteDisplayName(e.target.value)}
                  placeholder="Jane Smith"
                />
              </div>
              <div className="ts-field">
                <label>Role</label>
                <select value={inviteRole} onChange={e => setInviteRole(e.target.value as Role)}>
                  <option value="admin">Admin — manage users, view billing</option>
                  <option value="manager">Manager — all operations</option>
                  <option value="viewer">Viewer — read only</option>
                </select>
              </div>
              <button type="submit" className="ts-btn-primary" disabled={inviting}>
                {inviting ? 'Sending...' : 'Send invite'}
              </button>
            </div>

            <div className="ts-role-desc">
              <span className="ts-role-badge" style={{ background: roleColor(inviteRole) + '22', color: roleColor(inviteRole) }}>
                {roleLabel(inviteRole)}
              </span>
              {ROLE_META[inviteRole].desc}
            </div>

            {/* Collapsible permission overrides */}
            <div style={{ marginTop: 12 }}>
              <button
                type="button"
                className="ts-btn-ghost"
                onClick={() => setShowInvitePerms(!showInvitePerms)}
                style={{ fontSize: 12 }}
              >
                {showInvitePerms ? '▾ Hide permissions' : '▸ Set permissions at invite time'}
              </button>
              {showInvitePerms && (
                <div style={{ marginTop: 10 }}>
                  <div className="ts-perm-matrix-hint">
                    These overrides will be applied when the invitee accepts. Leave unchecked to inherit role defaults.
                  </div>
                  <div className="ts-perm-matrix-grid" style={{ marginTop: 8 }}>
                    {ALL_PERMISSION_KEYS.map(key => (
                      <label key={key} className="ts-perm-row">
                        <input
                          type="checkbox"
                          checked={!!invitePerms[key]}
                          onChange={e => setInvitePerms(p => ({ ...p, [key]: e.target.checked }))}
                          className="ts-perm-check"
                        />
                        <span className="ts-perm-key-label">{PERM_LABELS[key]}</span>
                      </label>
                    ))}
                  </div>
                </div>
              )}
            </div>

            {inviteSuccess && <div className="ts-msg ts-msg--success">✅ {inviteSuccess}</div>}
            {inviteError   && <div className="ts-msg ts-msg--error">⚠ {inviteError}</div>}
          </form>
        </div>
      )}

      {/* Tabs */}
      <div className="ts-tabs">
        <button
          className={`ts-tab${activeTab === 'members' ? ' ts-tab--active' : ''}`}
          onClick={() => setActiveTab('members')}
        >
          Members
          {activeMembers.length > 0 && <span className="ts-tab-badge">{activeMembers.length}</span>}
        </button>
        <button
          className={`ts-tab${activeTab === 'groups' ? ' ts-tab--active' : ''}`}
          onClick={() => setActiveTab('groups')}
        >
          Groups
          {groups.length > 0 && <span className="ts-tab-badge">{groups.length}</span>}
        </button>
        <button
          className={`ts-tab${activeTab === 'audit' ? ' ts-tab--active' : ''}`}
          onClick={() => setActiveTab('audit')}
        >
          Audit Log
        </button>
      </div>

      {loading ? (
        <div className="ts-loading">Loading team…</div>
      ) : error ? (
        <div className="ts-error">{error}</div>
      ) : activeTab === 'members' ? (
        <>
          {/* Search + Sort bar */}
          <div className="ts-toolbar">
            <input
              type="search"
              className="ts-search-input"
              placeholder="Search by name or email…"
              value={searchQuery}
              onChange={e => setSearchQuery(e.target.value)}
            />
            <select
              className="ts-sort-select"
              value={sortOption}
              onChange={e => setSortOption(e.target.value as SortOption)}
            >
              {SORT_OPTIONS.map(o => (
                <option key={o.value} value={o.value}>{o.label}</option>
              ))}
            </select>
          </div>

          {/* Active members */}
          <div className="ts-section">
            <div className="ts-section-label">
              Members {searchQuery && `— ${displayedMembers.length} result${displayedMembers.length !== 1 ? 's' : ''}`}
            </div>
            <div className="ts-member-list">
              {displayedMembers.length === 0 && (
                <div style={{ padding: '24px', textAlign: 'center', color: 'var(--text-muted)', fontSize: 14 }}>
                  No members match your search.
                </div>
              )}
              {displayedMembers.map(m => (
                <React.Fragment key={m.membership_id}>
                  <div className="ts-member">
                    {/* Avatar */}
                    <div className="ts-avatar" style={{ background: stringToColor(m.user_id) }}>
                      {m.avatar_url
                        ? <img src={m.avatar_url} alt={m.display_name} />
                        : avatarInitials(m.display_name || m.email)
                      }
                    </div>

                    {/* Info */}
                    <div className="ts-member-info">
                      <div className="ts-member-name">
                        {m.display_name || m.email}
                        {/* Group badges */}
                        {m.group_names && m.group_names.length > 0 && (
                          <span style={{ display: 'inline-flex', gap: 4, marginLeft: 6, flexWrap: 'wrap' }}>
                            {m.group_names.map(gn => (
                              <span key={gn} className="ts-group-badge">{gn}</span>
                            ))}
                          </span>
                        )}
                        {/* 2FA badge */}
                        {securityInfo[m.membership_id] && (
                          <span
                            title={securityInfo[m.membership_id].twoFactorEnabled
                              ? '2FA enabled'
                              : '2FA not enabled'}
                            style={{
                              marginLeft: 6,
                              fontSize: 11,
                              padding: '1px 6px',
                              borderRadius: 4,
                              background: securityInfo[m.membership_id].twoFactorEnabled ? '#16a34a22' : '#dc262622',
                              color: securityInfo[m.membership_id].twoFactorEnabled ? '#4ade80' : '#f87171',
                              fontWeight: 600,
                              cursor: 'help',
                            }}
                          >
                            {securityInfo[m.membership_id].twoFactorEnabled ? '2FA ✓' : '2FA ✗'}
                          </span>
                        )}
                      </div>
                      <div className="ts-member-email">{m.email}</div>
                    </div>

                    {/* Joined */}
                    <div className="ts-member-meta">
                      {m.joined_at && <span className="ts-joined">Joined {timeAgo(m.joined_at)}</span>}
                    </div>

                    {/* Role selector */}
                    <div className="ts-member-role">
                      {m.role === 'owner' ? (
                        <span className="ts-role-badge" style={{ background: roleColor('owner') + '22', color: roleColor('owner') }}>
                          Owner
                        </span>
                      ) : (
                        <select
                          value={m.role}
                          onChange={e => handleRoleChange(m.membership_id, e.target.value as Role)}
                          disabled={changingRole === m.membership_id}
                          className="ts-role-select"
                          style={{ '--role-color': roleColor(m.role) } as React.CSSProperties}
                        >
                          <option value="admin">Admin</option>
                          <option value="manager">Manager</option>
                          <option value="viewer">Viewer</option>
                        </select>
                      )}
                    </div>

                    {/* Kebab menu (replaces inline action buttons) */}
                    <div className="ts-member-actions">
                      {m.role !== 'owner' ? (
                        <KebabMenu items={[
                          {
                            label: expandedPerms === m.membership_id ? 'Close permissions' : 'Permissions',
                            onClick: () => expandedPerms === m.membership_id ? setExpandedPerms(null) : openPerms(m),
                          },
                          {
                            label: 'Reset password',
                            onClick: () => setResetConfirm({ membershipId: m.membership_id, email: m.email }),
                          },
                          {
                            label: 'Remove',
                            danger: true,
                            onClick: () => handleRemove(m.membership_id, m.display_name || m.email),
                          },
                        ]} />
                      ) : null}
                    </div>
                  </div>

                  {/* Permissions matrix — expands below the member row */}
                  {expandedPerms === m.membership_id && pendingPerms[m.membership_id] && (
                    <PermMatrix
                      member={m}
                      perms={pendingPerms[m.membership_id]}
                      saving={savingPerms === m.membership_id}
                      onSave={() => handleSavePermissions(m.membership_id)}
                      onCancel={() => setExpandedPerms(null)}
                      onChange={(key, val) => setPendingPerms(prev => ({
                        ...prev,
                        [m.membership_id]: { ...prev[m.membership_id], [key]: val }
                      }))}
                      onReset={() => {
                        const roleDefaults = Object.fromEntries(
                          ALL_PERMISSION_KEYS.map(k => [k, ROLE_DEFAULTS[m.role]?.[k] ?? false])
                        ) as Record<PermKey, boolean>;
                        setPendingPerms(prev => ({ ...prev, [m.membership_id]: roleDefaults }));
                      }}
                    />
                  )}
                </React.Fragment>
              ))}
            </div>
          </div>

          {/* Pending invitations */}
          {invitations.length > 0 && (
            <div className="ts-section">
              <div className="ts-section-label">Pending invitations</div>
              <div className="ts-member-list">
                {invitations.map(inv => (
                  <div key={inv.token} className="ts-member ts-member--pending">
                    <div className="ts-avatar ts-avatar--pending">✉</div>
                    <div className="ts-member-info">
                      <div className="ts-member-name">{inv.invited_email}</div>
                      <div className="ts-member-email">
                        Invited · expires {new Date(inv.expires_at).toLocaleDateString('en-GB', { day: 'numeric', month: 'short' })}
                      </div>
                    </div>
                    <div className="ts-member-meta" />
                    <div className="ts-member-role">
                      <span className="ts-role-badge" style={{ background: roleColor(inv.role) + '22', color: roleColor(inv.role) }}>
                        {roleLabel(inv.role)}
                      </span>
                    </div>
                    <div className="ts-member-actions">
                      <button
                        className="ts-btn-ghost"
                        onClick={() => navigator.clipboard.writeText(`${window.location.origin}/invite/${inv.token.replace('...', '')}`)}
                        title="Copy invite link"
                      >
                        Copy link
                      </button>
                      <button className="ts-btn-ghost ts-btn-danger" onClick={() => handleRevoke(inv.token, inv.invited_email)}>
                        Cancel
                      </button>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Role reference */}
          <div className="ts-section">
            <div className="ts-section-label">Role permissions</div>
            <div className="ts-role-table">
              <div className="ts-role-row ts-role-row--header">
                <div>Permission</div>
                {(['owner', 'admin', 'manager', 'viewer'] as Role[]).map(r => (
                  <div key={r} style={{ color: roleColor(r) }}>{roleLabel(r)}</div>
                ))}
              </div>
              {[
                ['View all data',           true,  true,  true,  true],
                ['Create & edit products',  true,  true,  true,  false],
                ['Process orders & dispatch', true, true, true,  false],
                ['Manage marketplace connections', true, true, true, false],
                ['Invite & remove users',   true,  true,  false, false],
                ['View billing',            true,  true,  false, false],
                ['Manage billing & plan',   true,  false, false, false],
                ['Delete account',          true,  false, false, false],
              ].map(([label, ...perms]) => (
                <div key={label as string} className="ts-role-row">
                  <div className="ts-perm-label">{label as string}</div>
                  {perms.map((allowed, i) => (
                    <div key={i} className={`ts-perm-cell ${allowed ? 'ts-perm--yes' : 'ts-perm--no'}`}>
                      {allowed ? '✓' : '–'}
                    </div>
                  ))}
                </div>
              ))}
            </div>
          </div>
        </>
      ) : activeTab === 'groups' ? (
        /* ── GROUPS TAB ── */
        <div className="ts-section">
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 16 }}>
            <div className="ts-section-label" style={{ margin: 0 }}>User groups</div>
            <button className="ts-btn-primary" onClick={() => setShowGroupModal(true)}>+ Create group</button>
          </div>

          {groups.length === 0 ? (
            <div className="ts-empty-state">
              <div className="ts-empty-icon">👥</div>
              <div className="ts-empty-title">No groups yet</div>
              <div className="ts-empty-desc">Create groups to apply shared permissions to multiple team members at once.</div>
            </div>
          ) : (
            <div className="ts-group-list">
              {groups.map(g => {
                const memberCount = g.member_ids?.length ?? 0;
                return (
                  <div key={g.id} className="ts-group-card">
                    <div className="ts-group-info">
                      <div className="ts-group-name">{g.name}</div>
                      {g.description && <div className="ts-group-desc">{g.description}</div>}
                      <div className="ts-group-meta">{memberCount} member{memberCount !== 1 ? 's' : ''}</div>
                    </div>
                    <div className="ts-group-actions">
                      <button className="ts-btn-ghost" onClick={() => setEditingGroup(g)}>Edit</button>
                      <button className="ts-btn-ghost ts-btn-danger" onClick={() => handleDeleteGroup(g)}>Delete</button>
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </div>
      ) : (
        /* ── AUDIT LOG TAB ── */
        <div className="ts-section">
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 16 }}>
            <div className="ts-section-label" style={{ margin: 0 }}>User Management Audit Log</div>
            <button className="ts-btn-ghost" onClick={loadAuditLog} style={{ fontSize: 13 }}>↻ Refresh</button>
          </div>

          {/* Filters */}
          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', marginBottom: 16 }}>
            <select
              value={auditFilter.event_type}
              onChange={e => setAuditFilter(f => ({ ...f, event_type: e.target.value }))}
              style={{ padding: '6px 10px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', fontSize: 13 }}
            >
              <option value="">All event types</option>
              {['user_created','user_deleted','role_changed','permissions_changed','login','invite_sent','invite_accepted','password_reset','group_created','group_deleted','group_member_added','group_member_removed'].map(t => (
                <option key={t} value={t}>{t.replace(/_/g, ' ')}</option>
              ))}
            </select>
            <input type="date" value={auditFilter.date_from} onChange={e => setAuditFilter(f => ({ ...f, date_from: e.target.value }))} style={{ padding: '6px 10px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', fontSize: 13 }} />
            <input type="date" value={auditFilter.date_to} onChange={e => setAuditFilter(f => ({ ...f, date_to: e.target.value }))} style={{ padding: '6px 10px', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', fontSize: 13 }} />
            <button className="ts-btn-ghost" style={{ fontSize: 13 }} onClick={loadAuditLog}>Apply</button>
          </div>

          {auditLoading ? (
            <div className="ts-loading">Loading audit log…</div>
          ) : auditEvents.length === 0 ? (
            <div className="ts-empty-state">
              <div className="ts-empty-icon">📋</div>
              <div className="ts-empty-title">No audit events</div>
              <div className="ts-empty-desc">User management events will appear here.</div>
            </div>
          ) : (
            <div style={{ overflowX: 'auto' }}>
              <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
                <thead>
                  <tr style={{ borderBottom: '1px solid var(--border)' }}>
                    {['Date / Time', 'Actor', 'Event', 'Target', 'Details'].map(h => (
                      <th key={h} style={{ padding: '8px 10px', textAlign: 'left', color: 'var(--text-muted)', fontWeight: 600, fontSize: 11, textTransform: 'uppercase', letterSpacing: '0.04em' }}>{h}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {auditEvents.map(ev => (
                    <tr
                      key={ev.id}
                      style={{ borderBottom: '1px solid var(--border)', cursor: 'pointer' }}
                      onClick={() => setAuditDetail(ev)}
                      onMouseEnter={e => (e.currentTarget as HTMLElement).style.background = 'var(--bg-elevated)'}
                      onMouseLeave={e => (e.currentTarget as HTMLElement).style.background = 'transparent'}
                    >
                      <td style={{ padding: '8px 10px', color: 'var(--text-muted)', whiteSpace: 'nowrap' }}>{new Date(ev.created_at).toLocaleString()}</td>
                      <td style={{ padding: '8px 10px', color: 'var(--text-primary)' }}>{ev.actor_email}</td>
                      <td style={{ padding: '8px 10px' }}>
                        <span style={{ padding: '2px 8px', borderRadius: 4, background: 'rgba(59,130,246,0.12)', color: '#3b82f6', fontSize: 11, fontWeight: 600 }}>
                          {ev.event_type.replace(/_/g, ' ')}
                        </span>
                      </td>
                      <td style={{ padding: '8px 10px', color: 'var(--text-secondary)' }}>{ev.target_email || '—'}</td>
                      <td style={{ padding: '8px 10px', color: 'var(--text-muted)', fontSize: 12 }}>
                        {ev.metadata ? Object.entries(ev.metadata).slice(0, 1).map(([k, v]) => `${k}: ${v}`).join(', ') : '—'}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          {/* Audit detail modal */}
          {auditDetail && (
            <div className="ts-modal-overlay" onClick={() => setAuditDetail(null)}>
              <div className="ts-modal" onClick={e => e.stopPropagation()} style={{ maxWidth: 560, width: '90%' }}>
                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 16 }}>
                  <div style={{ fontWeight: 700, fontSize: 16, color: 'var(--text-primary)' }}>Audit Event Details</div>
                  <button className="ts-modal-close" onClick={() => setAuditDetail(null)}>✕</button>
                </div>
                <div style={{ display: 'flex', flexDirection: 'column', gap: 10, fontSize: 13 }}>
                  {[
                    ['Event Type', auditDetail.event_type.replace(/_/g, ' ')],
                    ['Date / Time', new Date(auditDetail.created_at).toLocaleString()],
                    ['Actor', auditDetail.actor_email],
                    ['Target', auditDetail.target_email || '—'],
                  ].map(([label, value]) => (
                    <div key={label} style={{ display: 'flex', gap: 12 }}>
                      <div style={{ width: 120, color: 'var(--text-muted)', fontWeight: 600, flexShrink: 0 }}>{label}</div>
                      <div style={{ color: 'var(--text-primary)' }}>{value}</div>
                    </div>
                  ))}
                  {auditDetail.metadata && (
                    <div>
                      <div style={{ color: 'var(--text-muted)', fontWeight: 600, marginBottom: 6 }}>Metadata</div>
                      <pre style={{ background: 'var(--bg-elevated)', borderRadius: 6, padding: 12, fontSize: 12, color: 'var(--text-secondary)', overflowX: 'auto', margin: 0 }}>
                        {JSON.stringify(auditDetail.metadata, null, 2)}
                      </pre>
                    </div>
                  )}
                </div>
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  );
};

// Deterministic colour from a string
function stringToColor(str: string): string {
  const colors = ['#3b82f6', '#10b981', '#f59e0b', '#8b5cf6', '#06b6d4', '#f97316', '#14b8a6', '#ec4899'];
  let hash = 0;
  for (const ch of str) hash = (hash * 31 + ch.charCodeAt(0)) & 0xffffffff;
  return colors[Math.abs(hash) % colors.length];
}

export default TeamSettings;

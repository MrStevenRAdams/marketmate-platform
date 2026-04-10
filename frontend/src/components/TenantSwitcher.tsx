// ============================================================================
// TENANT SWITCHER — Account picker for the sidebar
// ============================================================================
// Reads tenants from AuthContext (user's memberships only).
// Hidden when the user only belongs to one tenant.
// ============================================================================

import { useState, useRef, useEffect } from 'react';
import { useAuth } from '../contexts/AuthContext';

const ROLE_COLOURS: Record<string, string> = {
  owner:   '#f59e0b',
  admin:   '#3b82f6',
  manager: '#10b981',
  viewer:  '#64748b',
};

const PLAN_LABELS: Record<string, { label: string; colour: string }> = {
  trialing:  { label: 'Trial',     colour: '#f59e0b' },
  active:    { label: 'Active',    colour: '#10b981' },
  past_due:  { label: 'Past due',  colour: '#ef4444' },
  suspended: { label: 'Suspended', colour: '#ef4444' },
  cancelled: { label: 'Cancelled', colour: '#64748b' },
};

export default function TenantSwitcher() {
  const { tenants, activeTenant, switchTenant, isLoading } = useAuth();
  const [open, setOpen] = useState(false);
  const dropdownRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    function handleClick(e: MouseEvent) {
      if (dropdownRef.current && !dropdownRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    }
    document.addEventListener('mousedown', handleClick);
    return () => document.removeEventListener('mousedown', handleClick);
  }, []);

  if (isLoading || !activeTenant) return null;
  if (tenants.length <= 1) return null;

  const planMeta = PLAN_LABELS[activeTenant.plan_status] ?? { label: activeTenant.plan_status, colour: '#64748b' };

  return (
    <div ref={dropdownRef} style={{ position: 'relative', padding: '12px 16px' }}>
      <button
        onClick={() => setOpen(!open)}
        style={{
          display: 'flex', alignItems: 'center', gap: '10px', width: '100%',
          padding: '10px 12px', background: 'var(--bg-tertiary)',
          border: '1px solid var(--border)', borderRadius: 'var(--radius-lg, 10px)',
          cursor: 'pointer', transition: 'border-color 150ms', outline: 'none',
        }}
        onMouseEnter={e => (e.currentTarget.style.borderColor = 'var(--border-bright)')}
        onMouseLeave={e => (e.currentTarget.style.borderColor = 'var(--border)')}
      >
        <div style={{
          width: 32, height: 32, borderRadius: 'var(--radius-md, 8px)',
          background: activeTenant.color || '#3b82f6',
          display: 'flex', alignItems: 'center', justifyContent: 'center',
          fontSize: '12px', fontWeight: 700, color: '#fff', flexShrink: 0,
        }}>
          {activeTenant.initials || activeTenant.name.slice(0, 2).toUpperCase()}
        </div>
        <div style={{ flex: 1, textAlign: 'left', minWidth: 0 }}>
          <div style={{
            fontSize: '13px', fontWeight: 600, color: 'var(--text-primary)',
            whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis',
          }}>
            {activeTenant.name}
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 5, marginTop: 1 }}>
            <span style={{ fontSize: '10px', fontWeight: 600, color: planMeta.colour, textTransform: 'uppercase', letterSpacing: '0.4px' }}>
              {planMeta.label}
            </span>
            <span style={{ fontSize: '10px', color: 'var(--text-muted)' }}>·</span>
            <span style={{ fontSize: '10px', color: ROLE_COLOURS[activeTenant.role] ?? 'var(--text-muted)', fontWeight: 500, textTransform: 'capitalize' }}>
              {activeTenant.role}
            </span>
          </div>
        </div>
        <svg width="12" height="12" viewBox="0 0 12 12" fill="none" style={{ flexShrink: 0, transform: open ? 'rotate(180deg)' : 'none', transition: 'transform 150ms' }}>
          <path d="M3 5L6 8L9 5" stroke="var(--text-muted)" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      </button>

      {open && (
        <div style={{
          position: 'absolute', bottom: '100%', left: 16, right: 16, marginBottom: 4,
          background: 'var(--bg-elevated)', border: '1px solid var(--border-bright)',
          borderRadius: 'var(--radius-lg, 10px)', boxShadow: '0 8px 32px rgba(0,0,0,0.3)',
          zIndex: 1050, maxHeight: 340, overflowY: 'auto',
        }}>
          <div style={{ padding: '8px 12px', borderBottom: '1px solid var(--border)', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
            <span style={{ fontSize: '11px', fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.5px' }}>
              Switch account
            </span>
            <span style={{ fontSize: '11px', color: 'var(--text-muted)' }}>{tenants.length} accounts</span>
          </div>

          {tenants.map(t => {
            const isActive = t.tenant_id === activeTenant.tenant_id;
            const tPlan = PLAN_LABELS[t.plan_status] ?? { label: t.plan_status, colour: '#64748b' };
            return (
              <div
                key={t.tenant_id}
                onClick={() => { if (!isActive) switchTenant(t.tenant_id); setOpen(false); }}
                style={{
                  display: 'flex', alignItems: 'center', gap: '10px', padding: '9px 12px',
                  cursor: isActive ? 'default' : 'pointer',
                  background: isActive ? 'var(--primary-glow)' : 'transparent',
                  borderLeft: isActive ? '2px solid var(--primary)' : '2px solid transparent',
                  transition: 'background 100ms',
                }}
                onMouseEnter={e => { if (!isActive) (e.currentTarget as HTMLElement).style.background = 'var(--bg-tertiary)'; }}
                onMouseLeave={e => { if (!isActive) (e.currentTarget as HTMLElement).style.background = 'transparent'; }}
              >
                <div style={{
                  width: 28, height: 28, borderRadius: 'var(--radius-sm, 6px)',
                  background: t.color || '#3b82f6',
                  display: 'flex', alignItems: 'center', justifyContent: 'center',
                  fontSize: '11px', fontWeight: 700, color: '#fff', flexShrink: 0,
                }}>
                  {t.initials || t.name.slice(0, 2).toUpperCase()}
                </div>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div style={{ fontSize: '13px', fontWeight: 500, color: 'var(--text-primary)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                    {t.name}
                  </div>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 5, marginTop: 1 }}>
                    <span style={{ fontSize: '10px', color: tPlan.colour, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.3px' }}>{tPlan.label}</span>
                    <span style={{ fontSize: '10px', color: 'var(--text-muted)' }}>·</span>
                    <span style={{ fontSize: '10px', color: ROLE_COLOURS[t.role] ?? 'var(--text-muted)', fontWeight: 500, textTransform: 'capitalize' }}>{t.role}</span>
                  </div>
                </div>
                {isActive && (
                  <svg width="14" height="14" viewBox="0 0 14 14" fill="none" style={{ flexShrink: 0 }}>
                    <path d="M3 7L6 10L11 4" stroke="var(--primary)" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                  </svg>
                )}
              </div>
            );
          })}

          <div style={{ padding: '8px 12px', borderTop: '1px solid var(--border)', fontSize: '11px', color: 'var(--text-muted)', textAlign: 'center' }}>
            To join another account, ask the owner to invite you
          </div>
        </div>
      )}
    </div>
  );
}

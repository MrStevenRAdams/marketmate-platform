// ============================================================================
// TENANT CONTEXT — Lightweight multi-tenancy for testing
// ============================================================================
// Stores the active tenant ID and provides it to all API services.
// Persists selection in localStorage so it survives page refreshes.
// Will be replaced by Module M (Users & Roles) later.
//
// SECURITY FIX (2026-03-18):
//   The previous implementation cached the tenant ID in a module-level variable
//   (_activeTenantId) that was seeded once at module load time from localStorage.
//   This caused tenant bleed in three scenarios:
//     1. New user registration — localStorage never updated before redirect, so
//        the new tenant saw the previous session's marketplace connections.
//     2. Logout → re-login with a different user — the module variable was stale
//        during the async /auth/me resolution window.
//     3. Cross-tab — localStorage changes in another tab were not reflected.
//
//   Fix: getActiveTenantId() now ALWAYS reads live from localStorage rather than
//   from a cached module variable. clearActiveTenantId() is exported so AuthContext
//   can call it on logout. A 'storage' event listener keeps the React state in sync
//   across tabs.
// ============================================================================

import { createContext, useContext, useState, useEffect, useCallback, ReactNode } from 'react';
import axios from 'axios';

const API_BASE_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';
const STORAGE_KEY = 'marketmate_active_tenant'; // must match AuthContext TENANT_KEY
const DEFAULT_TENANT = '';

export interface TenantAccount {
  tenant_id: string;
  name: string;
  initials: string;
  color: string;
  created_at: string;
}

interface TenantContextType {
  currentTenantId: string;
  currentTenant: TenantAccount | null;
  tenants: TenantAccount[];
  loading: boolean;
  switchTenant: (tenantId: string) => void;
  createTenant: (name: string) => Promise<TenantAccount>;
  deleteTenant: (tenantId: string) => Promise<void>;
  refreshTenants: () => Promise<void>;
}

const TenantContext = createContext<TenantContextType | null>(null);

// ============================================================================
// SAFE TENANT ID ACCESSOR
// ============================================================================
// Always reads live from localStorage — never from a cached module variable.
// This guarantees the correct tenant is used even immediately after:
//   - A new user registers (AuthContext writes to localStorage on /auth/me response)
//   - A user logs out and another logs in (stale module state can't persist)
//   - A tenant switch happens in another browser tab (storage event keeps it fresh)
// ============================================================================

/** Called by API service interceptors to get the current tenant. Always reads live. */
export function getActiveTenantId(): string {
  return localStorage.getItem(STORAGE_KEY) || DEFAULT_TENANT;
}

/** Called by AuthContext on logout to scrub the stored tenant immediately. */
export function clearActiveTenantId(): void {
  localStorage.removeItem(STORAGE_KEY);
}

// Internal axios instance (no tenant header needed — these are global endpoints)
const tenantApi = axios.create({ baseURL: API_BASE_URL });

export function TenantProvider({ children }: { children: ReactNode }) {
  // Initialise from localStorage, but treat this as display state only —
  // getActiveTenantId() is always the authoritative source for API calls.
  const [currentTenantId, setCurrentTenantId] = useState<string>(
    () => localStorage.getItem(STORAGE_KEY) || DEFAULT_TENANT
  );
  const [tenants, setTenants] = useState<TenantAccount[]>([]);
  const [loading, setLoading] = useState(true);

  const refreshTenants = useCallback(async () => {
    try {
      const res = await tenantApi.get('/tenants');
      const list: TenantAccount[] = res.data?.data || [];
      setTenants(list);
    } catch (err) {
      console.error('Failed to load tenants:', err);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    refreshTenants();
  }, [refreshTenants]);

  // ── Cross-tab sync ────────────────────────────────────────────────────────
  // If the user switches tenant (or logs in/out) in another tab, keep this
  // tab's React state consistent so the UI doesn't show stale data.
  useEffect(() => {
    function handleStorageEvent(e: StorageEvent) {
      if (e.key === STORAGE_KEY) {
        const next = e.newValue || DEFAULT_TENANT;
        setCurrentTenantId(next);
        if (next !== (e.oldValue || DEFAULT_TENANT)) {
          // Reload so all data re-fetches under the correct tenant
          window.location.reload();
        }
      }
    }
    window.addEventListener('storage', handleStorageEvent);
    return () => window.removeEventListener('storage', handleStorageEvent);
  }, []);

  const switchTenant = useCallback((tenantId: string) => {
    localStorage.setItem(STORAGE_KEY, tenantId);
    setCurrentTenantId(tenantId);
    // Force a full page reload so every component re-fetches with the new tenant
    window.location.reload();
  }, []);

  const createTenant = useCallback(async (name: string): Promise<TenantAccount> => {
    const res = await tenantApi.post('/tenants', { name });
    const tenant: TenantAccount = res.data?.data;
    await refreshTenants();
    return tenant;
  }, [refreshTenants]);

  const deleteTenant = useCallback(async (tenantId: string): Promise<void> => {
    await tenantApi.delete(`/tenants/${tenantId}`);
    if (currentTenantId === tenantId) {
      const remaining = tenants.filter(t => t.tenant_id !== tenantId);
      const next = remaining.length > 0 ? remaining[0].tenant_id : DEFAULT_TENANT;
      switchTenant(next);
    }
    await refreshTenants();
  }, [currentTenantId, tenants, switchTenant, refreshTenants]);

  const currentTenant = tenants.find(t => t.tenant_id === currentTenantId) || null;

  return (
    <TenantContext.Provider
      value={{
        currentTenantId,
        currentTenant,
        tenants,
        loading,
        switchTenant,
        createTenant,
        deleteTenant,
        refreshTenants,
      }}
    >
      {children}
    </TenantContext.Provider>
  );
}

export function useTenant(): TenantContextType {
  const ctx = useContext(TenantContext);
  if (!ctx) throw new Error('useTenant must be used within TenantProvider');
  return ctx;
}

import React, { createContext, useContext, useEffect, useState, useCallback, ReactNode } from 'react';
import { initializeApp, getApps } from 'firebase/app';
import {
  getAuth,
  signInWithEmailAndPassword,
  createUserWithEmailAndPassword,
  signOut,
  onAuthStateChanged,
  sendPasswordResetEmail,
  signInWithPopup,
  GoogleAuthProvider,
  OAuthProvider,
  User as FirebaseUser,
} from 'firebase/auth';
import { clearActiveTenantId } from './TenantContext';

// ============================================================================
// FIREBASE INITIALISATION
// ============================================================================

const firebaseConfig = {
  apiKey:            import.meta.env.VITE_FIREBASE_API_KEY,
  authDomain:        import.meta.env.VITE_FIREBASE_AUTH_DOMAIN,
  projectId:         import.meta.env.VITE_FIREBASE_PROJECT_ID,
  storageBucket:     import.meta.env.VITE_FIREBASE_STORAGE_BUCKET,
  messagingSenderId: import.meta.env.VITE_FIREBASE_MESSAGING_SENDER_ID,
  appId:             import.meta.env.VITE_FIREBASE_APP_ID,
};

const app = getApps().length === 0 ? initializeApp(firebaseConfig) : getApps()[0];
const auth = getAuth(app);

// ============================================================================
// TYPES
// ============================================================================

export type Role = 'owner' | 'admin' | 'manager' | 'viewer';
export type PlanID = 'starter_s' | 'starter_m' | 'starter_l' | 'premium' | 'enterprise';
export type PlanStatus = 'trialing' | 'active' | 'past_due' | 'suspended' | 'cancelled';

export interface TenantSummary {
  tenant_id: string;
  name: string;
  initials: string;
  color: string;
  role: Role;
  plan_id: PlanID;
  plan_status: PlanStatus;
}

export interface CreditLedger {
  credits_allocated?: number;
  credits_used: number;
  credits_remaining?: number;
  orders_processed: number;
  api_calls_total: number;
  status: 'active' | 'quota_exceeded' | 'closed' | 'billed';
  period: string;
}

export interface UserProfile {
  user_id: string;
  email: string;
  display_name: string;
  avatar_url?: string;
}

interface AuthContextValue {
  // Auth state
  firebaseUser: FirebaseUser | null;
  user: UserProfile | null;
  isAuthenticated: boolean;
  isLoading: boolean;

  // Tenant state
  tenants: TenantSummary[];
  activeTenant: TenantSummary | null;
  currentRole: Role | null;

  // Usage/quota
  ledger: CreditLedger | null;
  isQuotaExceeded: boolean;
  isTrialing: boolean;

  // Actions
  login: (email: string, password: string) => Promise<void>;
  loginWithGoogle: () => Promise<void>;
  loginWithMicrosoft: () => Promise<void>;
  logout: () => Promise<void>;
  resetPassword: (email: string) => Promise<void>;
  switchTenant: (tenantId: string) => void;
  refreshLedger: () => Promise<void>;

  // Convenience
  can: (action: string) => boolean;
  apiHeaders: () => Record<string, string>;
}

// ============================================================================
// PERMISSIONS
// ============================================================================

const PERMISSIONS: Record<string, Role[]> = {
  read:               ['owner', 'admin', 'manager', 'viewer'],
  write:              ['owner', 'admin', 'manager'],
  dispatch:           ['owner', 'admin', 'manager'],
  manage_users:       ['owner', 'admin'],
  view_billing:       ['owner', 'admin'],
  manage_billing:     ['owner'],
  delete_tenant:      ['owner'],
  invite_users:       ['owner', 'admin'],
  change_user_roles:  ['owner', 'admin'],
};

function canRole(role: Role | null, action: string): boolean {
  if (!role) return false;
  const allowed = PERMISSIONS[action] ?? [];
  return allowed.includes(role);
}

// ============================================================================
// CONTEXT
// ============================================================================

const AuthContext = createContext<AuthContextValue | null>(null);

const API_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';

// Persisted tenant selection — must match TenantContext STORAGE_KEY
const TENANT_KEY = 'marketmate_active_tenant';

export function AuthProvider({ children }: { children: ReactNode }) {
  const [firebaseUser, setFirebaseUser] = useState<FirebaseUser | null>(null);
  const [user, setUser] = useState<UserProfile | null>(null);
  const [tenants, setTenants] = useState<TenantSummary[]>([]);
  const [activeTenant, setActiveTenant] = useState<TenantSummary | null>(null);
  const [ledger, setLedger] = useState<CreditLedger | null>(null);
  const [isLoading, setIsLoading] = useState(true);

  // ── Derived state ───────────────────────────────────────────────────────────
  const currentRole = activeTenant?.role ?? null;
  const isAuthenticated = !!firebaseUser && !!user;
  const isQuotaExceeded = ledger?.status === 'quota_exceeded';
  const isTrialing = activeTenant?.plan_status === 'trialing';

  // ── API helpers ─────────────────────────────────────────────────────────────
  const getIdToken = useCallback(async (): Promise<string> => {
    if (!firebaseUser) throw new Error('Not authenticated');
    return firebaseUser.getIdToken();
  }, [firebaseUser]);

  const apiHeaders = useCallback((): Record<string, string> => {
    return {
      'Content-Type': 'application/json',
      'X-Tenant-Id': activeTenant?.tenant_id ?? '',
    };
  }, [activeTenant]);

  const authFetch = useCallback(async (url: string, options: RequestInit = {}) => {
    const token = await getIdToken();
    const tenantId = activeTenant?.tenant_id ?? '';
    return fetch(url, {
      ...options,
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${token}`,
        'X-Tenant-Id': tenantId,
        ...(options.headers as Record<string, string> ?? {}),
      },
    });
  }, [getIdToken, activeTenant]);

  // ── Load user profile + tenants ─────────────────────────────────────────────
  const loadUserData = useCallback(async (fbUser: FirebaseUser) => {
    try {
      const token = await fbUser.getIdToken();

      // Get user profile and tenant list
      const resp = await fetch(`${API_URL}/auth/me`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${token}`,
        },
        body: JSON.stringify({ firebase_uid: fbUser.uid }),
      });

      if (!resp.ok) {
        if (resp.status === 404) {
          // User has Firebase account but hasn't completed registration.
          // Don't sign out if we're on the register page (step 2 needs the Firebase session).
          if (!window.location.pathname.startsWith('/register')) {
            await signOut(auth);
          }
          return;
        }
        throw new Error('Failed to load user data');
      }

      const data = await resp.json();
      setUser(data.user);
      const tenantList: TenantSummary[] = data.tenants ?? [];
      setTenants(tenantList);

      // ── SECURITY FIX: validate the saved tenant before trusting it ──────────
      // The saved value might belong to a DIFFERENT user who was previously
      // logged in on this browser. Only honour it if it actually appears in
      // this user's tenant list. If not, fall back to the first tenant and
      // immediately overwrite localStorage so no stale ID lingers.
      const savedTenantId = localStorage.getItem(TENANT_KEY);

      let selected: TenantSummary | null = null;
      if (savedTenantId) {
        selected = tenantList.find(t => t.tenant_id === savedTenantId) ?? null;
      }
      if (!selected && tenantList.length > 0) {
        selected = tenantList[0];
      }

      if (selected) {
        setActiveTenant(selected);
        // Write immediately so getActiveTenantId() returns the correct value
        // for any API calls that fire synchronously after this point.
        localStorage.setItem(TENANT_KEY, selected.tenant_id);
      } else {
        // No valid tenant — clear any stale value so API calls don't bleed.
        clearActiveTenantId();
      }
    } catch (err) {
      console.error('[auth] failed to load user data:', err);
    }
  }, []);

  // ── Load ledger for active tenant ───────────────────────────────────────────
  const refreshLedger = useCallback(async () => {
    if (!activeTenant || !firebaseUser) return;
    try {
      const token = await firebaseUser.getIdToken();
      const resp = await fetch(`${API_URL}/billing/usage`, {
        headers: {
          'Authorization': `Bearer ${token}`,
          'X-Tenant-Id': activeTenant.tenant_id,
        },
      });
      if (resp.ok) {
        const data = await resp.json();
        setLedger(data.ledger);
      }
    } catch {
      // Non-fatal
    }
  }, [activeTenant, firebaseUser]);

  // ── Firebase auth state listener ────────────────────────────────────────────
  useEffect(() => {
    const unsubscribe = onAuthStateChanged(auth, async (fbUser) => {
      setFirebaseUser(fbUser);
      if (fbUser) {
        await loadUserData(fbUser);
      } else {
        setUser(null);
        setTenants([]);
        setActiveTenant(null);
        setLedger(null);
      }
      setIsLoading(false);
    });
    return unsubscribe;
  }, [loadUserData]);

  // ── Refresh ledger when tenant changes ──────────────────────────────────────
  useEffect(() => {
    if (activeTenant) {
      refreshLedger();
    }
  }, [activeTenant?.tenant_id]);

  // ── Actions ─────────────────────────────────────────────────────────────────

  const login = async (email: string, password: string) => {
    await signInWithEmailAndPassword(auth, email, password);
    // onAuthStateChanged fires automatically and loads user data
  };

  const loginWithGoogle = async () => {
    const provider = new GoogleAuthProvider();
    await signInWithPopup(auth, provider);
  };

  const loginWithMicrosoft = async () => {
    const provider = new OAuthProvider('microsoft.com');
    await signInWithPopup(auth, provider);
  };

  const logout = async () => {
    // SECURITY FIX: clear the tenant from localStorage AND the TenantContext
    // module variable BEFORE signing out. This prevents the next user who logs
    // in on this browser from briefly seeing this user's tenant data during the
    // async /auth/me resolution window.
    clearActiveTenantId();
    setActiveTenant(null);
    setUser(null);
    setTenants([]);
    setLedger(null);
    await signOut(auth);
  };

  const resetPassword = async (email: string) => {
    await sendPasswordResetEmail(auth, email);
  };

  const switchTenant = (tenantId: string) => {
    const tenant = tenants.find(t => t.tenant_id === tenantId);
    if (tenant) {
      setActiveTenant(tenant);
      setLedger(null);
      localStorage.setItem(TENANT_KEY, tenantId);
    }
  };

  const can = (action: string) => canRole(currentRole, action);

  const value: AuthContextValue = {
    firebaseUser,
    user,
    isAuthenticated,
    isLoading,
    tenants,
    activeTenant,
    currentRole,
    ledger,
    isQuotaExceeded,
    isTrialing,
    login,
    loginWithGoogle,
    loginWithMicrosoft,
    logout,
    resetPassword,
    switchTenant,
    refreshLedger,
    can,
    apiHeaders,
  };

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

// ============================================================================
// HOOKS
// ============================================================================

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used within AuthProvider');
  return ctx;
}

export function useRequireAuth() {
  const auth = useAuth();
  if (!auth.isLoading && !auth.isAuthenticated) {
    window.location.href = '/login';
  }
  return auth;
}

// Export Firebase auth instance for use in registration
export { auth, createUserWithEmailAndPassword };

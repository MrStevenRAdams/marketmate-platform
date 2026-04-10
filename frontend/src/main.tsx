import React, { Component, ReactNode } from 'react'
import ReactDOM from 'react-dom/client'
import App from './App.tsx'
import { AuthProvider, auth } from './contexts/AuthContext.tsx'
import { TenantProvider, getActiveTenantId } from './contexts/TenantContext.tsx'
import './styles/globals.css'
import './styles/components.css'

// ── Top-level error boundary ──────────────────────────────────────────────────
// React swallows render errors silently in production when there is no boundary,
// leaving #root empty with no console output — exactly the blank-page symptom.
// This boundary catches those errors and renders a visible crash screen instead.

interface ErrorBoundaryState { error: Error | null }

class RootErrorBoundary extends Component<{ children: ReactNode }, ErrorBoundaryState> {
  constructor(props: { children: ReactNode }) {
    super(props)
    this.state = { error: null }
  }

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { error }
  }

  componentDidCatch(error: Error, info: React.ErrorInfo) {
    // Always log to console so the error appears even if the UI is blank
    console.error('[RootErrorBoundary] Uncaught render error:', error, info.componentStack)
  }

  render() {
    if (this.state.error) {
      return (
        <div style={{
          display: 'flex', flexDirection: 'column', alignItems: 'center',
          justifyContent: 'center', height: '100vh', padding: '2rem',
          fontFamily: 'system-ui, sans-serif', color: '#1a1a1a',
        }}>
          <h1 style={{ fontSize: '1.5rem', marginBottom: '0.5rem' }}>Something went wrong</h1>
          <p style={{ color: '#666', marginBottom: '1rem' }}>
            The application crashed before it could render. Check the browser console for details.
          </p>
          <pre style={{
            background: '#f5f5f5', padding: '1rem', borderRadius: '6px',
            fontSize: '0.8rem', maxWidth: '600px', overflowX: 'auto',
            whiteSpace: 'pre-wrap', wordBreak: 'break-word',
          }}>
            {this.state.error.message}
          </pre>
          <button
            onClick={() => window.location.reload()}
            style={{
              marginTop: '1.5rem', padding: '0.5rem 1.5rem', borderRadius: '6px',
              background: '#2563eb', color: '#fff', border: 'none',
              cursor: 'pointer', fontSize: '0.9rem',
            }}
          >
            Reload page
          </button>
        </div>
      )
    }
    return this.props.children
  }
}

// ── Global fetch interceptor ─────────────────────────────────────────────────
// Patches window.fetch so every API call automatically gets:
//   Authorization: Bearer <firebase-token>
//   X-Tenant-Id: <active-tenant>

const API_ORIGIN = (import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1')
  .replace(/\/api\/v1$/, '');

const _originalFetch = window.fetch.bind(window);

// Wait for Firebase auth to resolve (max 5s) before giving up on the token.
// This prevents race conditions where components fire requests before
// onAuthStateChanged has set auth.currentUser.
function waitForAuthUser(timeoutMs = 5000): Promise<import('firebase/auth').User | null> {
  return new Promise((resolve) => {
    const user = auth.currentUser;
    if (user) { resolve(user); return; }
    let settled = false;
    const timer = setTimeout(() => { if (!settled) { settled = true; resolve(null); } }, timeoutMs);
    const unsub = auth.onAuthStateChanged((u) => {
      if (!settled) {
        settled = true;
        clearTimeout(timer);
        unsub();
        resolve(u);
      }
    });
  });
}

window.fetch = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
  const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;

  const isApiCall = url.includes('/api/v1') || url.startsWith(API_ORIGIN);
  const isExternal = url.includes('firebase') || url.includes('googleapis.com') || url.includes('metadata.google');

  if (isApiCall && !isExternal) {
    const headers = new Headers(init?.headers);

    // Auth endpoints don't need X-Tenant-Id (they're pre-tenant calls)
    const isAuthRoute = url.includes('/auth/');

    if (!isAuthRoute && !headers.has('X-Tenant-Id')) {
      const tenantId = getActiveTenantId() || localStorage.getItem('marketmate_active_tenant') || '';
      if (tenantId) headers.set('X-Tenant-Id', tenantId);
    }

    if (!headers.has('Authorization')) {
      try {
        const user = await waitForAuthUser();
        if (user) {
          const token = await user.getIdToken();
          headers.set('Authorization', `Bearer ${token}`);
        }
      } catch { /* non-fatal */ }
    }

    return _originalFetch(input, { ...init, headers });
  }

  return _originalFetch(input, init);
};

// ─────────────────────────────────────────────────────────────────────────────

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <RootErrorBoundary>
      <AuthProvider>
        <TenantProvider>
          <App />
        </TenantProvider>
      </AuthProvider>
    </RootErrorBoundary>
  </React.StrictMode>,
)

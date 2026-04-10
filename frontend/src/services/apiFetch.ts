// ============================================================================
// AUTHENTICATED API FETCH UTILITY
// ============================================================================
// Single source of truth for all API calls. Attaches:
//   Authorization: Bearer <firebase-token>
//   X-Tenant-Id: <active-tenant>
//   Content-Type: application/json
//
// Usage:
//   import { apiFetch } from '../services/apiFetch';
//   const res = await apiFetch('/products');
//   const res = await apiFetch('/products', { method: 'POST', body: JSON.stringify(data) });
// ============================================================================

import { auth } from '../contexts/AuthContext';
import { getActiveTenantId } from '../contexts/TenantContext';

const API_BASE: string =
  (import.meta as any).env?.VITE_API_URL ||
  'http://localhost:8080/api/v1';

export async function apiFetch(path: string, init?: RequestInit): Promise<Response> {
  const tenantId = getActiveTenantId() || localStorage.getItem('marketmate_active_tenant') || '';

  let token = '';
  try {
    if (auth.currentUser) {
      token = await auth.currentUser.getIdToken();
    }
  } catch {
    // Non-fatal — request will fail with 401 if token is required
  }

  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      'X-Tenant-Id': tenantId,
      ...(token ? { 'Authorization': `Bearer ${token}` } : {}),
      ...(init?.headers as Record<string, string> ?? {}),
    },
  });
}

import { apiFetch } from '../services/apiFetch';
import { useEffect, useState, useCallback, useRef } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { searchService, productService } from '../services/api';
import { getActiveTenantId } from '../contexts/TenantContext';
import { auth } from '../contexts/AuthContext';
import type { Product } from '../types';

// ─── API helper ───────────────────────────────────────────────────────────────

const API_BASE: string =
  (import.meta as any).env?.VITE_API_URL ||
  'https://marketmate-api-487246736287.us-central1.run.app/api/v1';

async function apiCall(path: string, init?: RequestInit) {
  const tenantId = getActiveTenantId() || localStorage.getItem('tenantId') || '';
  let token = '';
  try {
    if (auth.currentUser) token = await auth.currentUser.getIdToken();
  } catch { /* non-fatal */ }
  return apiFetch(path, init);
}

// ─── Types ────────────────────────────────────────────────────────────────────

interface InventoryView {
  view_id: string;
  name: string;
  columns: string[];
  filters: Record<string, string>;
  sort_field: string;
  sort_dir: string;
  position: number;
}

interface EditingCell {
  productId: string;
  field: 'retail_price' | 'min_level';
  value: string;
  saving: boolean;
  success: boolean | null;
}

const ALL_COLUMNS = ['product', 'sku', 'type', 'status', 'stock', 'due_in', 'retail_price', 'min_level', 'categories', 'created_at'];
const DEFAULT_COLUMNS = ['product', 'sku', 'type', 'status', 'stock', 'categories', 'created_at'];

const COLUMN_LABELS: Record<string, string> = {
  product: 'Product',
  sku: 'SKU',
  type: 'Type',
  status: 'Status',
  stock: 'Stock',
  due_in: 'Due In',
  retail_price: 'Retail Price',
  min_level: 'Min Level',
  categories: 'Categories',
  created_at: 'Created',
};

// RAG helper: returns colour based on stock vs min_level
function getStockRAG(qty: number, minLevel: number): { color: string; bg: string } {
  if (qty <= 0)        return { color: '#ef4444', bg: 'rgba(239,68,68,0.12)' };
  if (qty <= minLevel) return { color: '#f59e0b', bg: 'rgba(245,158,11,0.12)' };
  return { color: '#22c55e', bg: 'rgba(34,197,94,0.10)' };
}

// ─── Saved Views hook ─────────────────────────────────────────────────────────

function useInventoryViews() {
  const [views, setViews] = useState<InventoryView[]>([]);

  const load = useCallback(async () => {
    try {
      const res = await apiCall('/inventory-views');
      if (res.ok) {
        const d = await res.json();
        setViews(d.views || []);
      }
    } catch { /* noop */ }
  }, []);

  useEffect(() => { load(); }, [load]);

  const create = useCallback(async (name: string, columns: string[], filters: Record<string, string>, position: number) => {
    const res = await apiCall('/inventory-views', {
      method: 'POST',
      body: JSON.stringify({ name, columns, filters, position }),
    });
    if (res.ok) load();
  }, [load]);

  const update = useCallback(async (viewId: string, patch: Partial<InventoryView>) => {
    await apiCall(`/inventory-views/${viewId}`, { method: 'PUT', body: JSON.stringify(patch) });
    load();
  }, [load]);

  const remove = useCallback(async (viewId: string) => {
    await apiCall(`/inventory-views/${viewId}`, { method: 'DELETE' });
    load();
  }, [load]);

  return { views, load, create, update, remove };
}

// ─── Main Component ───────────────────────────────────────────────────────────

export default function ProductList() {
  const navigate = useNavigate();
  const [products, setProducts] = useState<Product[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [filters, setFilters] = useState({ status: '', search: '' });
  const [totalFound, setTotalFound] = useState(0);
  const [page, setPage] = useState(1);
  const [searchAvailable, setSearchAvailable] = useState<boolean | null>(null);
  const perPage = 50;
  const searchTimer = useRef<any>(null);

  // ── Pending review banner ──────────────────────────────────────────────────
  const [pendingReviewCount, setPendingReviewCount] = useState(0);
  useEffect(() => {
    apiFetch('/marketplace/pending-review/count')
      .then(r => r.ok ? r.json() : null)
      .then(data => { if (data) setPendingReviewCount(data.count || 0); })
      .catch(() => {});
  }, []);

  // ── Saved Views state ──────────────────────────────────────────────────────
  const { views, create: createView, update: updateView, remove: removeView } = useInventoryViews();
  const [activeViewId, setActiveViewId] = useState<string | null>(null);
  const [activeColumns, setActiveColumns] = useState<string[]>(DEFAULT_COLUMNS);
  const [showSaveModal, setShowSaveModal] = useState(false);
  const [newViewName, setNewViewName] = useState('');
  const [renamingViewId, setRenamingViewId] = useState<string | null>(null);
  const [renameValue, setRenameValue] = useState('');
  const [viewMenuId, setViewMenuId] = useState<string | null>(null);
  const [showColumnChooser, setShowColumnChooser] = useState(false);

  // ── Inline editing state ───────────────────────────────────────────────────
  const [editingCell, setEditingCell] = useState<EditingCell | null>(null);

  // ── Stock levels & due-in ─────────────────────────────────────────────────
  const [stockMap, setStockMap] = useState<Record<string, number>>({}); // productId → total qty
  const [dueInMap, setDueInMap] = useState<Record<string, number>>({}); // sku → due in qty

  // ── Context menu ──────────────────────────────────────────────────────────
  const [ctxMenu, setCtxMenu] = useState<{ x: number; y: number; product: any } | null>(null);
  const [duplicating, setDuplicating] = useState<string | null>(null);
  // H-003: Set of product IDs that have AI-generated content (appeared in a completed job)
  const [aiHealthSet, setAiHealthSet] = useState<Set<string>>(new Set());

  // ── Export / Import ───────────────────────────────────────────────────────
  async function handleExport() {
    const tenantId = getActiveTenantId() || localStorage.getItem('tenantId') || '';
    try {
      let token = '';
      if (auth.currentUser) token = await auth.currentUser.getIdToken();
      const res = await fetch(`${API_BASE}/products/export`, {
        headers: {
          'Authorization': token ? `Bearer ${token}` : '',
          'X-Tenant-Id': tenantId,
        },
      });
      if (!res.ok) {
        const err = await res.json().catch(() => ({}));
        alert(`Export failed: ${err.error || res.statusText}`);
        return;
      }
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `products-export-${new Date().toISOString().slice(0, 10)}.csv`;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
    } catch (e: any) {
      alert(`Export failed: ${e.message}`);
    }
  }

  function handleImport() {
    const input = document.createElement('input');
    input.type = 'file';
    input.accept = '.csv,.xlsx';
    input.onchange = async (e) => {
      const tenantId = getActiveTenantId() || localStorage.getItem('tenantId') || '';
      const file = (e.target as HTMLInputElement).files?.[0];
      if (!file) return;
      const formData = new FormData();
      formData.append('file', file);
      try {
        const res = await fetch(`${API_BASE}/products/import`, {
          method: 'POST',
          headers: { 'X-Tenant-Id': tenantId },
          body: formData,
        });
        if (!res.ok) {
          const body = await res.json().catch(() => ({}));
          alert(`Import failed: ${body.error || res.status}`);
          return;
        }
        const data = await res.json();
        alert(`Import started. Job ID: ${data.job_id || 'N/A'}`);
        loadProducts(1);
      } catch (err: any) {
        alert(`Import error: ${err.message}`);
      }
    };
    input.click();
  }

  async function handleReindex() {
    const tenantId = getActiveTenantId() || localStorage.getItem('tenantId') || '';
    if (!confirm('Reindex all products into search?\nThis may take a minute for large catalogs.')) return;
    try {
      const res = await fetch(`${API_BASE}/search/sync`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': tenantId },
        body: JSON.stringify({ collection: 'products' }),
      });
      const data = await res.json();
      if (res.ok) {
        alert(`✅ Reindex complete — ${data.products?.indexed ?? 0} products indexed`);
        loadProducts(1);
      } else {
        alert(`Reindex failed: ${JSON.stringify(data)}`);
      }
    } catch (err: any) {
      alert(`Reindex error: ${err.message}`);
    }
  }

  // ── Search health ─────────────────────────────────────────────────────────
  useEffect(() => {
    searchService.health()
      .then(() => setSearchAvailable(true))
      .catch(() => setSearchAvailable(false));
  }, []);

  const isBarcodeScan = (q: string) => /^\d{8,14}$/.test(q.trim());

  const loadProducts = useCallback(async (currentPage = 1) => {
    try {
      setLoading(true);
      setError(null);

      // Barcode scan detection — bypass search, use dedicated endpoint
      if (filters.search && isBarcodeScan(filters.search)) {
        const res = await productService.list({ barcode: filters.search.trim(), page: 1, page_size: perPage } as any);
        const data = res.data;
        const products = data?.data || data?.products || (Array.isArray(data) ? data : []);
        setProducts(products);
        setTotalFound(products.length);
        setPage(1);
        return;
      }

      if (searchAvailable) {
        const response = await searchService.products({
          q: filters.search || '*',
          status: filters.status || undefined,
          page: currentPage,
          per_page: perPage,
        });
        const data = response.data;
        setProducts(Array.isArray(data.data) ? data.data : []);
        setTotalFound(data.found || 0);
        setPage(data.page || 1);
      } else {
        const params: any = { page: currentPage, page_size: perPage };
        if (filters.status) params.status = filters.status;
        if (filters.search) params.search = filters.search;

        const response = await productService.list(params);
        const responseData = response.data;

        let productData: Product[] = [];
        let total = 0;
        let responsePage = currentPage;

        if (responseData?.data && Array.isArray(responseData.data)) {
          productData = responseData.data;
          total = responseData.pagination?.total || responseData.total || productData.length;
          responsePage = responseData.pagination?.page || responseData.page || currentPage;
        } else if (Array.isArray(responseData)) {
          productData = responseData;
          total = productData.length;
        }

        if (filters.search && productData.length > 0) {
          const q = filters.search.toLowerCase();
          const filtered = productData.filter((p: any) => {
            const sku = p.sku || p.attributes?.source_sku || '';
            return (
              (p.title || '').toLowerCase().includes(q) ||
              (p.brand || '').toLowerCase().includes(q) ||
              (typeof sku === 'string' && sku.toLowerCase().includes(q))
            );
          });
          if (filtered.length < productData.length) productData = filtered;
        }

        setProducts(productData);
        setTotalFound(total);
        setPage(responsePage);
        // Fire stock/due-in fetches after products land (non-blocking)
        fetchStockForProducts(productData).catch(() => {});
        fetchDueInForProducts(productData).catch(() => {});
      }
    } catch (err: any) {
      console.error('Failed to load products:', err);
      setError(err.response?.data?.error || err.message || 'Failed to load products');
      setProducts([]);
    } finally {
      setLoading(false);
    }
  }, [filters.search, filters.status, searchAvailable]);

  // H-003: Load AI health data after products load
  useEffect(() => {
    if (!products.length) return;
    const load = async () => {
      try {
        const res = await apiCall('/ai/generate/jobs');
        if (!res.ok) return;
        const data = await res.json();
        const jobs: any[] = data.data || [];
        const coveredIds = new Set<string>();
        jobs.forEach(job => {
          if (job.status === 'completed' && Array.isArray(job.product_ids)) {
            job.product_ids.forEach((id: string) => coveredIds.add(id));
          }
        });
        setAiHealthSet(coveredIds);
      } catch {
        // Non-fatal — AI health badge is best-effort
      }
    };
    load();
  }, [products]);

  useEffect(() => {
    if (searchAvailable === null) return;
    if (searchTimer.current) clearTimeout(searchTimer.current);
    searchTimer.current = setTimeout(() => { loadProducts(1); }, filters.search ? 300 : 0);
    return () => { if (searchTimer.current) clearTimeout(searchTimer.current); };
  }, [filters.search, filters.status, searchAvailable]);

  // ── Saved view actions ────────────────────────────────────────────────────
  function activateView(view: InventoryView | null) {
    if (!view) {
      setActiveViewId(null);
      setActiveColumns(DEFAULT_COLUMNS);
      setFilters({ status: '', search: '' });
    } else {
      setActiveViewId(view.view_id);
      setActiveColumns(view.columns?.length ? view.columns : DEFAULT_COLUMNS);
      if (view.filters) setFilters({ status: view.filters.status || '', search: view.filters.search || '' });
    }
  }

  async function saveNewView() {
    if (!newViewName.trim()) return;
    await createView(newViewName.trim(), activeColumns, { status: filters.status, search: filters.search }, views.length);
    setShowSaveModal(false);
    setNewViewName('');
  }

  async function finishRename(viewId: string) {
    if (renameValue.trim()) await updateView(viewId, { name: renameValue.trim() });
    setRenamingViewId(null);
    setRenameValue('');
  }

  function toggleColumn(col: string) {
    const next = activeColumns.includes(col)
      ? activeColumns.filter(c => c !== col)
      : [...activeColumns, col];
    const final = next.length ? next : DEFAULT_COLUMNS;
    setActiveColumns(final);
    if (activeViewId) updateView(activeViewId, { columns: final });
  }

  // ── Inline editing ────────────────────────────────────────────────────────
  function startEdit(productId: string, field: 'retail_price' | 'min_level', currentValue: any) {
    setEditingCell({ productId, field, value: currentValue != null ? String(currentValue) : '', saving: false, success: null });
  }

  async function commitEdit() {
    if (!editingCell) return;
    const { productId, field, value } = editingCell;
    setEditingCell(prev => prev ? { ...prev, saving: true } : null);
    try {
      const body: Record<string, any> = {};
      if (field === 'retail_price') body.attributes = { retail_price: parseFloat(value) || 0 };
      else if (field === 'min_level') body.attributes = { min_level: parseInt(value, 10) || 0 };
      const res = await apiCall(`/products/${productId}`, { method: 'PATCH', body: JSON.stringify(body) });
      if (res.ok) {
        setProducts(prev => prev.map((p: any) =>
          p.product_id !== productId ? p : { ...p, attributes: { ...p.attributes, ...body.attributes } }
        ));
        setEditingCell(prev => prev ? { ...prev, saving: false, success: true } : null);
        setTimeout(() => setEditingCell(null), 800);
      } else {
        setEditingCell(prev => prev ? { ...prev, saving: false, success: false } : null);
        setTimeout(() => setEditingCell(prev => prev ? { ...prev, success: null } : null), 1500);
      }
    } catch {
      setEditingCell(prev => prev ? { ...prev, saving: false, success: false } : null);
      setTimeout(() => setEditingCell(prev => prev ? { ...prev, success: null } : null), 1500);
    }
  }

  function cancelEdit() { setEditingCell(null); }

  // ── Stock & Due-In fetchers ────────────────────────────────────────────────
  async function fetchStockForProducts(prods: any[]) {
    if (!prods.length) return;
    try {
      const results = await Promise.allSettled(
        prods.map(p => apiCall(`/inventory?product_id=${encodeURIComponent(p.product_id || p.id || '')}`).then(r => r.json()))
      );
      const newMap: Record<string, number> = {};
      results.forEach((r, i) => {
        if (r.status === 'fulfilled') {
          const total = (r.value.inventory || []).reduce((sum: number, rec: any) => sum + (rec.quantity || 0), 0);
          newMap[prods[i].product_id || prods[i].id || ''] = total;
        }
      });
      setStockMap(prev => ({ ...prev, ...newMap }));
    } catch { /* noop */ }
  }

  async function fetchDueInForProducts(prods: any[]) {
    if (!prods.length) return;
    const skus = prods.map((p: any) => p.sku || p.attributes?.source_sku || '').filter(Boolean);
    if (!skus.length) return;
    try {
      const res = await apiCall(`/purchase-orders/due-in?skus=${encodeURIComponent(skus.join(','))}`);
      if (res.ok) {
        const data = await res.json();
        setDueInMap(prev => ({ ...prev, ...(data.due_in || {}) }));
      }
    } catch { /* noop */ }
  }

  // ── Duplicate ─────────────────────────────────────────────────────────────
  async function duplicateProduct(productId: string) {
    setDuplicating(productId);
    try {
      const res = await apiCall(`/products/${productId}/duplicate`, { method: 'POST' });
      if (res.ok) {
        const data = await res.json();
        // Navigate to new product
        window.location.href = `/products/${data.data?.product_id}`;
      } else {
        alert('Failed to duplicate product');
      }
    } catch { alert('Failed to duplicate product'); }
    finally { setDuplicating(null); }
  }

  // ── Helpers ───────────────────────────────────────────────────────────────
  const getStatusBadge = (status: string) => {
    const badges: Record<string, { class: string; label: string }> = {
      active:   { class: 'badge-success', label: 'Active'   },
      draft:    { class: 'badge-warning', label: 'Draft'    },
      archived: { class: 'badge-danger',  label: 'Archived' },
    };
    const badge = badges[status] || badges.draft;
    return <span className={`badge ${badge.class}`}>{badge.label}</span>;
  };

  const safeProducts = Array.isArray(products) ? products : [];
  const getProductId = (p: any) => p.product_id || p.id || '';
  const totalPages = Math.ceil(totalFound / perPage);

  function renderEditableCell(product: any, field: 'retail_price' | 'min_level') {
    const pid = getProductId(product);
    const isEditing = editingCell?.productId === pid && editingCell.field === field;
    const rawValue = product.attributes?.[field];

    if (isEditing) {
      return (
        <div style={{ display: 'flex', alignItems: 'center', gap: 4, minWidth: 80 }}>
          {editingCell!.saving ? (
            <span style={{ fontSize: 13, color: 'var(--text-muted)' }}>…</span>
          ) : editingCell!.success === true ? (
            <span style={{ color: '#22c55e', fontSize: 14 }}>✓</span>
          ) : editingCell!.success === false ? (
            <span style={{ color: '#ef4444', fontSize: 14 }}>✗</span>
          ) : (
            <input
              type="number"
              autoFocus
              value={editingCell!.value}
              onChange={e => setEditingCell(prev => prev ? { ...prev, value: e.target.value } : null)}
              onKeyDown={e => { if (e.key === 'Enter') commitEdit(); if (e.key === 'Escape') cancelEdit(); }}
              onBlur={commitEdit}
              style={{
                width: 80, padding: '3px 6px', background: 'var(--bg-secondary)',
                border: '1px solid var(--primary)', borderRadius: 4, color: 'var(--text-primary)',
                fontSize: 13, outline: 'none',
              }}
            />
          )}
        </div>
      );
    }

    const display = rawValue != null
      ? (field === 'retail_price' ? `£${Number(rawValue).toFixed(2)}` : String(rawValue))
      : <span style={{ color: 'var(--text-muted)', fontSize: 12 }}>—</span>;

    return (
      <span
        onClick={() => startEdit(pid, field, rawValue ?? '')}
        title={`Click to edit ${COLUMN_LABELS[field]}`}
        style={{
          cursor: 'text', display: 'inline-block', minWidth: 48, borderRadius: 4,
          padding: '2px 6px', border: '1px solid transparent', transition: 'border-color 0.1s',
        }}
        onMouseEnter={e => (e.currentTarget.style.borderColor = 'var(--border)')}
        onMouseLeave={e => (e.currentTarget.style.borderColor = 'transparent')}
      >
        {display}
      </span>
    );
  }

  const tabBtnStyle = (active: boolean): React.CSSProperties => ({
    padding: '10px 16px', background: 'none', border: 'none', cursor: 'pointer',
    borderBottom: active ? '2px solid var(--primary)' : '2px solid transparent',
    color: active ? 'var(--primary)' : 'var(--text-muted)',
    fontWeight: active ? 700 : 400, fontSize: 13, whiteSpace: 'nowrap',
    marginBottom: '-1px',
  });

  return (
    <div className="page" onClick={() => { setViewMenuId(null); setCtxMenu(null); }}>
      {/* Pending Review Banner — shown when auto-connect imports have products awaiting review */}
      {pendingReviewCount > 0 && (
        <div style={{
          padding: '12px 16px', marginBottom: 16, borderRadius: 8,
          background: 'rgba(245,158,11,0.1)', border: '1px solid rgba(245,158,11,0.4)',
          display: 'flex', alignItems: 'center', justifyContent: 'space-between', fontSize: 13,
        }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <span>📥</span>
            <span style={{ color: 'var(--text-secondary)' }}>
              <strong style={{ color: '#d97706' }}>{pendingReviewCount} product{pendingReviewCount !== 1 ? 's' : ''}</strong>
              {' '}downloaded from your marketplaces {pendingReviewCount !== 1 ? 'are' : 'is'} waiting for review — check for duplicates before they enter your catalogue.
            </span>
          </div>
          <button
            onClick={() => navigate('/products/review-mappings')}
            style={{ background: '#f59e0b', border: 'none', borderRadius: 6, padding: '6px 14px', color: '#fff', cursor: 'pointer', fontSize: 12, fontWeight: 700, whiteSpace: 'nowrap', marginLeft: 12 }}>
            Review now →
          </button>
        </div>
      )}

      {/* Typesense Health Banner */}
      {searchAvailable === false && (
        <div style={{
          padding: '10px 16px', marginBottom: 16, borderRadius: 8,
          background: 'rgba(255,170,0,0.1)', border: '1px solid rgba(255,170,0,0.3)',
          display: 'flex', alignItems: 'center', justifyContent: 'space-between', fontSize: 13,
        }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <span>⚠️</span>
            <span style={{ color: 'var(--text-secondary)' }}>
              <strong style={{ color: '#ffaa00' }}>Search engine offline</strong> — Using Firestore fallback
            </span>
          </div>
          <button onClick={() => { searchService.health().then(() => setSearchAvailable(true)).catch(() => setSearchAvailable(false)); }}
            style={{ background: 'rgba(255,170,0,0.15)', border: '1px solid rgba(255,170,0,0.3)', borderRadius: 6, padding: '4px 12px', color: '#ffaa00', cursor: 'pointer', fontSize: 12, fontWeight: 600 }}>
            Retry
          </button>
        </div>
      )}

      {/* Header */}
      <div className="page-header">
        <div>
          <h1 className="page-title">Products</h1>
          <p className="page-subtitle">Manage your product catalog</p>
        </div>
        <div className="page-actions">
          <button className="btn btn-secondary" onClick={handleImport}><span>📥</span> Import</button>
          <button className="btn btn-secondary" onClick={handleExport}><span>📤</span> Export</button>
          {searchAvailable && (
            <button className="btn btn-secondary" onClick={handleReindex}><span>🔍</span> Reindex</button>
          )}
          <Link to="/products/create" className="btn btn-primary"><span>➕</span> Add Product</Link>
        </div>
      </div>

      {/* Stats Cards */}
      <div className="stats-grid">
        <div className="stat-card">
          <div className="stat-label">Total Products</div>
          <div className="stat-value">{totalFound.toLocaleString()}</div>
          <div className="stat-change neutral">{filters.search ? 'matching' : 'total'}</div>
        </div>
        <div className="stat-card">
          <div className="stat-label">Active</div>
          <div className="stat-value">{safeProducts.filter(p => p.status === 'active').length}</div>
          <div className="stat-change neutral">—</div>
        </div>
        <div className="stat-card">
          <div className="stat-label">Draft</div>
          <div className="stat-value">{safeProducts.filter(p => p.status === 'draft').length}</div>
          <div className="stat-change neutral">—</div>
        </div>
        <div className="stat-card">
          <div className="stat-label">Archived</div>
          <div className="stat-value">{safeProducts.filter(p => p.status === 'archived').length}</div>
          <div className="stat-change neutral">—</div>
        </div>
      </div>

      {/* ── Saved Views Tab Bar ─────────────────────────────────────────────── */}
      <div style={{ display: 'flex', alignItems: 'center', borderBottom: '1px solid var(--border)', overflowX: 'auto', position: 'relative' }}>
        {/* All Products */}
        <button style={tabBtnStyle(activeViewId === null)} onClick={() => activateView(null)}>
          📋 All Products
        </button>

        {views.map(view => (
          <div key={view.view_id} style={{ position: 'relative', display: 'flex', alignItems: 'center' }}>
            {renamingViewId === view.view_id ? (
              <input
                autoFocus value={renameValue}
                onChange={e => setRenameValue(e.target.value)}
                onKeyDown={e => { if (e.key === 'Enter') finishRename(view.view_id); if (e.key === 'Escape') setRenamingViewId(null); }}
                onBlur={() => finishRename(view.view_id)}
                style={{ padding: '6px 10px', background: 'var(--bg-secondary)', border: '1px solid var(--primary)', borderRadius: 4, color: 'var(--text-primary)', fontSize: 13, width: 140, outline: 'none' }}
              />
            ) : (
              <button style={tabBtnStyle(activeViewId === view.view_id)} onClick={() => activateView(view)}>
                {view.name}
              </button>
            )}
            <button
              onClick={e => { e.stopPropagation(); setViewMenuId(viewMenuId === view.view_id ? null : view.view_id); }}
              style={{ background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-muted)', fontSize: 16, padding: '0 4px' }}
            >⋯</button>

            {viewMenuId === view.view_id && (
              <div style={{
                position: 'absolute', top: '100%', right: 0, zIndex: 200,
                background: 'var(--bg-elevated)', border: '1px solid var(--border)',
                borderRadius: 8, minWidth: 140, boxShadow: '0 8px 24px rgba(0,0,0,0.4)', padding: 4,
              }} onClick={e => e.stopPropagation()}>
                <button
                  style={{ display: 'block', width: '100%', padding: '8px 12px', background: 'none', border: 'none', color: 'var(--text-primary)', fontSize: 13, cursor: 'pointer', textAlign: 'left', borderRadius: 4 }}
                  onClick={() => { setRenamingViewId(view.view_id); setRenameValue(view.name); setViewMenuId(null); }}
                >✏️ Rename</button>
                <button
                  style={{ display: 'block', width: '100%', padding: '8px 12px', background: 'none', border: 'none', color: '#ef4444', fontSize: 13, cursor: 'pointer', textAlign: 'left', borderRadius: 4 }}
                  onClick={async () => { await removeView(view.view_id); setViewMenuId(null); if (activeViewId === view.view_id) activateView(null); }}
                >🗑️ Delete</button>
              </div>
            )}
          </div>
        ))}

        <button onClick={() => setShowSaveModal(true)} title="Save current view as new tab"
          style={{ padding: '8px 12px', background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-muted)', fontSize: 18 }}>
          ＋
        </button>

        <button onClick={() => setShowColumnChooser(!showColumnChooser)} title="Choose visible columns"
          style={{ marginLeft: 'auto', padding: '6px 10px', background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-muted)', fontSize: 13 }}>
          ⚙️ Columns
        </button>
      </div>

      {/* Column chooser */}
      {showColumnChooser && (
        <div style={{ background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderTop: 'none', padding: 12, display: 'flex', flexWrap: 'wrap', gap: 10 }}>
          {ALL_COLUMNS.map(col => (
            <label key={col} style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 13, cursor: 'pointer', color: 'var(--text-primary)' }}>
              <input type="checkbox" checked={activeColumns.includes(col)} onChange={() => toggleColumn(col)} />
              {COLUMN_LABELS[col]}
            </label>
          ))}
        </div>
      )}

      {/* Filters and Table */}
      <div className="card" style={{ marginTop: 0, borderTopLeftRadius: 0, borderTopRightRadius: 0, borderTop: 'none' }}>
        <div className="card-header">
          <div className="filters">
            <input type="text" className="input" placeholder="Search products..." value={filters.search}
              onChange={e => setFilters({ ...filters, search: e.target.value })} />
            <select className="select" value={filters.status} onChange={e => setFilters({ ...filters, status: e.target.value })}>
              <option value="">All Status</option>
              <option value="active">Active</option>
              <option value="draft">Draft</option>
              <option value="archived">Archived</option>
            </select>
          </div>
          <div className="card-actions">
            <button className="btn-icon" title="Refresh" onClick={() => loadProducts(page)}>🔄</button>
          </div>
        </div>

        {(activeColumns.includes('retail_price') || activeColumns.includes('min_level')) && (
          <div style={{ padding: '6px 16px', fontSize: 11, color: 'var(--text-muted)', borderBottom: '1px solid var(--border)', background: 'rgba(99,102,241,0.05)' }}>
            💡 Click a <strong>Retail Price</strong> or <strong>Min Level</strong> cell to edit it inline. Press Enter to save, Escape to cancel.
          </div>
        )}

        <div className="table-container">
          {loading ? (
            <div className="loading-state"><div className="spinner"></div><p>Loading products...</p></div>
          ) : error ? (
            <div className="empty-state">
              <div className="empty-icon" style={{ fontSize: 48 }}>⚠️</div>
              <h3 style={{ color: 'var(--danger)' }}>Error Loading Products</h3>
              <p style={{ color: 'var(--text-secondary)' }}>{error}</p>
              <button onClick={() => loadProducts(1)} className="btn btn-primary" style={{ marginRight: 8 }}>Try Again</button>
              <Link to="/products/create" className="btn btn-secondary">Add Product Instead</Link>
            </div>
          ) : safeProducts.length === 0 ? (
            <div className="empty-state">
              <div className="empty-icon">📦</div>
              <h3>No products found</h3>
              <p>Get started by creating your first product</p>
              <Link to="/products/create" className="btn btn-primary">Add Product</Link>
            </div>
          ) : (
            <table className="table">
              <thead>
                <tr>
                  <th><input type="checkbox" /></th>
                  {activeColumns.includes('product')      && <th>Product</th>}
                  {activeColumns.includes('sku')          && <th>SKU</th>}
                  {activeColumns.includes('type')         && <th>Type</th>}
                  {activeColumns.includes('status')       && <th>Status</th>}
                  {activeColumns.includes('stock')        && <th>Stock</th>}
                  {activeColumns.includes('due_in')       && <th title="Quantity on open purchase orders">Due In</th>}
                  {activeColumns.includes('retail_price') && <th>Retail Price</th>}
                  {activeColumns.includes('min_level')    && <th>Min Level</th>}
                  {activeColumns.includes('categories')   && <th>Categories</th>}
                  {activeColumns.includes('created_at')   && <th>Created</th>}
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody>
                {safeProducts.map((product: any) => (
                  <tr
                    key={getProductId(product)}
                    onContextMenu={e => { e.preventDefault(); setCtxMenu({ x: e.clientX, y: e.clientY, product }); }}
                  >
                    <td><input type="checkbox" /></td>

                    {activeColumns.includes('product') && (
                      <td>
                        <div className="product-cell">
                          {(() => {
                            const img = product.assets?.find((a: any) => a.role === 'primary_image') || product.assets?.[0];
                            return img?.url ? (
                              <img src={img.url} alt={product.title || ''} style={{ width: 40, height: 40, borderRadius: 'var(--radius-md)', objectFit: 'cover', flexShrink: 0 }} />
                            ) : (
                              <div className="product-image">{product.title?.charAt(0) || 'P'}</div>
                            );
                          })()}
                          <div className="product-info">
                            <Link to={`/products/${getProductId(product)}`} className="product-title">
                              {product.title || 'Untitled Product'}
                            </Link>
                            {product.subtitle && <div className="product-subtitle">{product.subtitle}</div>}
                          </div>
                        </div>
                      </td>
                    )}

                    {activeColumns.includes('sku') && (
                      <td><span style={{ fontSize: 13, color: 'var(--text-secondary)' }}>{product.sku || product.attributes?.source_sku || '-'}</span></td>
                    )}

                    {activeColumns.includes('type') && (
                      <td><span className="type-badge">{product.product_type || 'simple'}</span></td>
                    )}

                    {activeColumns.includes('status') && (
                      <td>{getStatusBadge(product.status || 'draft')}</td>
                    )}

                    {activeColumns.includes('stock') && (() => {
                      const pid = getProductId(product);
                      const qty = stockMap[pid] ?? null;
                      const minLevel = product.attributes?.min_level ?? 0;
                      if (qty === null) return <td style={{ color: 'var(--text-muted)', fontSize: 12 }}>—</td>;
                      const rag = getStockRAG(qty, minLevel);
                      return (
                        <td>
                          <span style={{ display: 'inline-block', padding: '2px 8px', borderRadius: 12, fontSize: 13, fontWeight: 700, color: rag.color, background: rag.bg, minWidth: 32, textAlign: 'center' }}>
                            {qty.toLocaleString()}
                          </span>
                        </td>
                      );
                    })()}

                    {activeColumns.includes('due_in') && (() => {
                      const sku = product.sku || product.attributes?.source_sku || '';
                      const due = dueInMap[sku];
                      return (
                        <td>
                          {due ? (
                            <span style={{ fontSize: 13, fontWeight: 600, color: 'var(--accent-cyan)' }}>+{due}</span>
                          ) : (
                            <span style={{ color: 'var(--text-muted)', fontSize: 12 }}>—</span>
                          )}
                        </td>
                      );
                    })()}

                    {activeColumns.includes('retail_price') && (
                      <td>{renderEditableCell(product, 'retail_price')}</td>
                    )}

                    {activeColumns.includes('min_level') && (
                      <td>{renderEditableCell(product, 'min_level')}</td>
                    )}

                    {activeColumns.includes('categories') && (
                      <td>
                        <div className="category-tags">
                          {product.category_ids?.slice(0, 2).map((catId: string) => (
                            <span key={catId} className="tag">{catId}</span>
                          ))}
                          {(product.category_ids?.length || 0) > 2 && (
                            <span className="tag">+{product.category_ids!.length - 2}</span>
                          )}
                        </div>
                      </td>
                    )}

                    {activeColumns.includes('created_at') && (
                      <td className="text-muted">
                        {product.created_at
                          ? new Date(typeof product.created_at === 'number' ? product.created_at * 1000 : product.created_at).toLocaleDateString()
                          : '-'}
                      </td>
                    )}

                    <td>
                      {/* H-003: AI Health badge — shown only when product has no AI-generated content */}
                      {!aiHealthSet.has(getProductId(product)) && (
                        <span
                          title="No AI-generated content — click to generate"
                          onClick={() => window.location.href = `/products/${getProductId(product)}/edit`}
                          style={{
                            display: 'inline-block', marginRight: 6, padding: '2px 7px',
                            borderRadius: 4, fontSize: 11, fontWeight: 600, cursor: 'pointer',
                            background: 'rgba(251,191,36,0.12)', color: '#fbbf24',
                            border: '1px solid rgba(251,191,36,0.3)',
                            whiteSpace: 'nowrap',
                          }}
                        >
                          ✦ AI content missing
                        </span>
                      )}
                      <div className="action-buttons">
                        <Link to={`/products/${getProductId(product)}/edit`} className="btn-icon" title="Edit">✏️</Link>
                        <button
                          className="btn-icon"
                          title="Duplicate product"
                          disabled={duplicating === getProductId(product)}
                          onClick={() => duplicateProduct(getProductId(product))}
                          style={{ opacity: duplicating === getProductId(product) ? 0.5 : 1 }}
                        >{duplicating === getProductId(product) ? '⏳' : '⧉'}</button>
                        <button className="btn-icon" title="More" onClick={e => { e.stopPropagation(); setCtxMenu({ x: e.currentTarget.getBoundingClientRect().left, y: e.currentTarget.getBoundingClientRect().bottom, product }); }}>⋮</button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>

        {totalFound > 0 && (
          <div className="card-footer">
            <div className="pagination-info">
              Showing <strong>{((page - 1) * perPage) + 1}–{Math.min(page * perPage, totalFound)}</strong> of <strong>{totalFound.toLocaleString()}</strong>
              {searchAvailable === false && <span style={{ color: 'var(--text-muted)', fontSize: 11, marginLeft: 8 }}>(Firestore)</span>}
            </div>
            <div className="pagination">
              <button className="btn-icon" disabled={page <= 1} onClick={() => { setPage(p => p - 1); loadProducts(page - 1); }}>◀</button>
              {Array.from({ length: Math.min(totalPages, 7) }, (_, i) => {
                const start = Math.max(1, Math.min(page - 3, totalPages - 6));
                const p = start + i;
                if (p > totalPages) return null;
                return <button key={p} className={`btn-icon ${p === page ? 'active' : ''}`} onClick={() => { setPage(p); loadProducts(p); }}>{p}</button>;
              })}
              <button className="btn-icon" disabled={page >= totalPages} onClick={() => { setPage(p => p + 1); loadProducts(page + 1); }}>▶</button>
            </div>
          </div>
        )}
      </div>

      {/* Save View Modal */}
      {showSaveModal && (
        <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.6)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 }}>
          <div style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 12, width: 380, padding: 24 }}>
            <h3 style={{ margin: '0 0 8px', color: 'var(--text-primary)', fontSize: 16, fontWeight: 700 }}>💾 Save View</h3>
            <p style={{ color: 'var(--text-muted)', fontSize: 13, margin: '0 0 16px' }}>
              Saves the current columns, filters and search as a named tab.
            </p>
            <input
              autoFocus type="text" placeholder="e.g. Amazon Active, Low Stock…"
              value={newViewName} onChange={e => setNewViewName(e.target.value)}
              onKeyDown={e => { if (e.key === 'Enter') saveNewView(); if (e.key === 'Escape') setShowSaveModal(false); }}
              style={{ width: '100%', boxSizing: 'border-box', padding: '10px 12px', background: 'var(--bg-tertiary)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', fontSize: 14, outline: 'none', marginBottom: 16 }}
            />
            <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
              <button onClick={() => setShowSaveModal(false)}
                style={{ padding: '8px 16px', background: 'var(--bg-tertiary)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-muted)', cursor: 'pointer', fontSize: 13 }}>
                Cancel
              </button>
              <button onClick={saveNewView} disabled={!newViewName.trim()}
                style={{ padding: '8px 16px', background: 'var(--primary)', border: 'none', borderRadius: 6, color: '#fff', cursor: 'pointer', fontWeight: 600, fontSize: 13, opacity: newViewName.trim() ? 1 : 0.5 }}>
                Save View
              </button>
            </div>
          </div>
        </div>
      )}

      {/* ── Right-click context menu ─────────────────────────────────────── */}
      {ctxMenu && (
        <>
          <div style={{ position: 'fixed', inset: 0, zIndex: 999 }} onClick={() => setCtxMenu(null)} />
          <div
            style={{
              position: 'fixed', zIndex: 1000, left: ctxMenu.x, top: ctxMenu.y,
              background: 'var(--bg-elevated)', border: '1px solid var(--border)',
              borderRadius: 10, boxShadow: '0 8px 24px rgba(0,0,0,0.5)',
              minWidth: 180, padding: 4,
            }}
            onClick={() => setCtxMenu(null)}
          >
            {[
              { icon: '👁', label: 'View', action: () => navigate(`/products/${getProductId(ctxMenu.product)}`) },
              { icon: '✏️', label: 'Edit', action: () => navigate(`/products/${getProductId(ctxMenu.product)}/edit`) },
              { icon: '⧉', label: 'Duplicate', action: () => duplicateProduct(getProductId(ctxMenu.product)) },
              null, // divider
              { icon: '🗄️', label: 'Archive', action: async () => {
                await apiCall(`/products/${getProductId(ctxMenu.product)}`, { method: 'PATCH', body: JSON.stringify({ status: 'archived' }) });
                window.location.reload();
              }},
            ].map((item, i) => item === null ? (
              <div key={i} style={{ height: 1, background: 'var(--border)', margin: '4px 0' }} />
            ) : (
              <button
                key={i}
                onClick={item.action}
                style={{
                  display: 'flex', alignItems: 'center', gap: 10, width: '100%',
                  padding: '9px 14px', background: 'none', border: 'none',
                  color: item.label === 'Archive' ? 'var(--warning)' : 'var(--text-primary)',
                  fontSize: 13, cursor: 'pointer', textAlign: 'left', borderRadius: 6,
                }}
                onMouseEnter={e => (e.currentTarget.style.background = 'var(--bg-tertiary)')}
                onMouseLeave={e => (e.currentTarget.style.background = 'none')}
              >
                <span>{item.icon}</span>{item.label}
              </button>
            ))}
          </div>
        </>
      )}
    </div>
  );
}

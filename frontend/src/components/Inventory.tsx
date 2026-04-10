import { useState, useEffect } from 'react';
import { 
  Package, AlertTriangle, TrendingDown, TrendingUp, Warehouse, 
  Search, Filter, Plus, Minus, ArrowRightLeft, RefreshCw, 
  Clock, CheckCircle, XCircle, Lock, Edit, MapPin, Box,
  Calendar, User, FileText, ChevronDown, ChevronRight
} from 'lucide-react';
import { getActiveTenantId } from '../contexts/TenantContext';
import './Inventory.css';

interface InventoryItem {
  inventory_id: string;
  sku: string;
  product_name: string;
  variant_name?: string;
  locations: LocationStock[];
  total_on_hand: number;
  total_reserved: number;
  total_available: number;
  total_inbound: number;
  safety_stock: number;
  reorder_point: number;
  updated_at: string;
}

interface LocationStock {
  location_id: string;
  location_name: string;
  on_hand: number;
  reserved: number;
  available: number;
  inbound: number;
  safety_stock: number;
}

interface Reservation {
  reservation_id: string;
  sku: string;
  location_id: string;
  quantity: number;
  order_id: string;
  shipment_id?: string;
  status: 'active' | 'released' | 'expired';
  created_at: string;
  expires_at?: string;
  released_at?: string;
}

interface Movement {
  movement_id: string;
  sku: string;
  location_id: string;
  type: 'receipt' | 'shipment' | 'adjustment' | 'transfer_in' | 'transfer_out' | 'return';
  quantity: number;
  reason_code: string;
  reference_id?: string;
  created_by: string;
  created_at: string;
  notes?: string;
}

interface Location {
  location_id: string;
  name: string;
  type: 'warehouse' | '3pl' | 'store' | 'supplier';
  address: string;
  timezone: string;
  active: boolean;
  cut_off_time?: string;
}

interface InventoryView {
  view_id: string;
  name: string;
  columns: string[];
  filters: Record<string, any>;
  sort_field: string;
  sort_dir: string;
}

const Inventory = () => {
  // State
  const [inventory, setInventory] = useState<InventoryItem[]>([]);
  const [locations, setLocations] = useState<Location[]>([]);
  const [stats, setStats] = useState({
    total_skus: 0,
    total_on_hand: 0,
    total_reserved: 0,
    total_available: 0,
    low_stock_count: 0,
    out_of_stock_count: 0,
  });
  const [loading, setLoading] = useState(true);
  const [view, setView] = useState<'by_sku' | 'by_location' | 'movements' | 'reservations'>('by_sku');
  
  // Filters
  const [filters, setFilters] = useState({
    search: '',
    location: '',
    lowStock: false,
    outOfStock: false,
    hasReservations: false,
  });
  const [showFilters, setShowFilters] = useState(false);

  // Saved Views (A-004)
  const [savedViews, setSavedViews] = useState<InventoryView[]>([]);
  const [activeViewId, setActiveViewId] = useState<string>('default');
  const [showSaveViewModal, setShowSaveViewModal] = useState(false);
  const [saveViewName, setSaveViewName] = useState('');
  const [savingView, setSavingView] = useState(false);
  
  // Dialogs
  const [showAdjustDialog, setShowAdjustDialog] = useState(false);
  const [showReceiveDialog, setShowReceiveDialog] = useState(false);
  const [showTransferDialog, setShowTransferDialog] = useState(false);
  const [selectedSku, setSelectedSku] = useState<InventoryItem | null>(null);
  const [expandedSku, setExpandedSku] = useState<string | null>(null);
  
  // Adjustment form
  const [adjustmentForm, setAdjustmentForm] = useState({
    sku: '',
    location_id: '',
    type: 'adjustment' as 'adjustment' | 'receipt' | 'return',
    quantity: 0,
    reason_code: '',
    notes: '',
  });
  
  // Transfer form
  const [transferForm, setTransferForm] = useState({
    sku: '',
    from_location: '',
    to_location: '',
    quantity: 0,
    notes: '',
  });

  const API_BASE = (import.meta as any).env?.VITE_API_URL || 'http://localhost:8080/api/v1';
  const tenantId = getActiveTenantId();

  useEffect(() => {
    loadInventory();
    loadLocations();
    loadStats();
    loadSavedViews();
  }, [filters.location, filters.lowStock, filters.outOfStock]);

  async function loadSavedViews() {
    try {
      const response = await fetch(`${API_BASE}/inventory-views`, {
        headers: { 'X-Tenant-Id': tenantId }
      });
      if (response.ok) {
        const data = await response.json();
        setSavedViews(data.views || []);
      }
    } catch {
      // Views are optional — fail silently
    }
  }

  function applyView(view: InventoryView) {
    setActiveViewId(view.view_id);
    // Apply filters from the view
    const vf = view.filters || {};
    setFilters(prev => ({
      ...prev,
      search: vf.search || prev.search,
      location: vf.location || '',
      lowStock: vf.low_stock || false,
      outOfStock: vf.out_of_stock || false,
      hasReservations: vf.has_reservations || false,
    }));
  }

  async function saveCurrentView() {
    if (!saveViewName.trim()) return;
    setSavingView(true);
    try {
      const response = await fetch(`${API_BASE}/inventory-views`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': tenantId },
        body: JSON.stringify({
          name: saveViewName.trim(),
          columns: [],
          filters: {
            search: filters.search,
            location: filters.location,
            low_stock: filters.lowStock,
            out_of_stock: filters.outOfStock,
            has_reservations: filters.hasReservations,
          },
          sort_field: '',
          sort_dir: 'asc',
        }),
      });
      if (response.ok) {
        await loadSavedViews();
        setShowSaveViewModal(false);
        setSaveViewName('');
      }
    } catch {
      // Handle silently
    } finally {
      setSavingView(false);
    }
  }

  async function deleteView(viewId: string) {
    if (!window.confirm('Delete this saved view?')) return;
    try {
      await fetch(`${API_BASE}/inventory-views/${viewId}`, {
        method: 'DELETE',
        headers: { 'X-Tenant-Id': tenantId },
      });
      await loadSavedViews();
      if (activeViewId === viewId) setActiveViewId('default');
    } catch {
      // Handle silently
    }
  }

  async function loadInventory() {
    setLoading(true);
    try {
      const params = new URLSearchParams();
      if (filters.location) params.append('location_id', filters.location);
      if (filters.lowStock) params.append('low_stock', 'true');
      if (filters.outOfStock) params.append('out_of_stock', 'true');
      
      const response = await fetch(`${API_BASE}/inventory?${params}`, {
        headers: { 'X-Tenant-Id': tenantId }
      });
      
      if (response.ok) {
        const data = await response.json();
        setInventory(data.inventory || []);
      }
    } catch (error) {
      console.error('Failed to load inventory:', error);
    } finally {
      setLoading(false);
    }
  };

  async function loadLocations() {
    try {
      const response = await fetch(`${API_BASE}/inventory/locations`, {
        headers: { 'X-Tenant-Id': tenantId }
      });
      
      if (response.ok) {
        const data = await response.json();
        setLocations(data.locations || []);
      }
    } catch (error) {
      console.error('Failed to load locations:', error);
    }
  };

  async function loadStats() {
    try {
      const response = await fetch(`${API_BASE}/inventory/stats`, {
        headers: { 'X-Tenant-Id': tenantId }
      });
      
      if (response.ok) {
        const data = await response.json();
        setStats(data.stats || stats);
      }
    } catch (error) {
      console.error('Failed to load stats:', error);
    }
  };

  const adjustStock = async () => {
    try {
      const response = await fetch(`${API_BASE}/inventory/adjust`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-Tenant-Id': tenantId,
        },
        body: JSON.stringify(adjustmentForm),
      });
      
      if (response.ok) {
        await loadInventory();
        await loadStats();
        setShowAdjustDialog(false);
        setAdjustmentForm({
          sku: '',
          location_id: '',
          type: 'adjustment',
          quantity: 0,
          reason_code: '',
          notes: '',
        });
      }
    } catch (error) {
      console.error('Failed to adjust stock:', error);
    }
  };

  const createTransfer = async () => {
    try {
      const response = await fetch(`${API_BASE}/inventory/transfer`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-Tenant-Id': tenantId,
        },
        body: JSON.stringify(transferForm),
      });
      
      if (response.ok) {
        await loadInventory();
        setShowTransferDialog(false);
        setTransferForm({
          sku: '',
          from_location: '',
          to_location: '',
          quantity: 0,
          notes: '',
        });
      }
    } catch (error) {
      console.error('Failed to create transfer:', error);
    }
  };

  // Filter inventory
  const filteredInventory = inventory.filter(item => {
    if (filters.search) {
      const searchLower = filters.search.toLowerCase();
      if (!item.sku.toLowerCase().includes(searchLower) &&
          !item.product_name.toLowerCase().includes(searchLower)) {
        return false;
      }
    }
    
    if (filters.hasReservations && item.total_reserved === 0) {
      return false;
    }
    
    return true;
  });

  const getStockStatus = (item: InventoryItem): 'healthy' | 'low' | 'out' | 'reserved' => {
    if (item.total_available === 0) return 'out';
    if (item.total_available <= item.safety_stock) return 'low';
    if (item.total_reserved > 0) return 'reserved';
    return 'healthy';
  };

  const formatDate = (dateString: string) => {
    const date = new Date(dateString);
    return date.toLocaleString('en-GB', { 
      day: '2-digit', 
      month: 'short', 
      year: 'numeric',
      hour: '2-digit',
      minute: '2-digit'
    });
  };

  return (
    <div className="inventory-container">
      {/* Header */}
      <div className="inventory-header">
        <div className="inventory-title-section">
          <h1>Inventory Management</h1>
          <div className="inventory-meta">
            <div className="inventory-count-badge">{stats.total_skus}</div>
            <span className="inventory-subtitle">SKUs tracked</span>
          </div>
        </div>
        <div className="inventory-actions">
          {/* A-004: Saved Views Dropdown */}
          <div style={{ position: 'relative', display: 'inline-flex', alignItems: 'center', gap: 4 }}>
            <select
              value={activeViewId}
              onChange={e => {
                const vid = e.target.value;
                if (vid === 'default') { setActiveViewId('default'); return; }
                const v = savedViews.find(sv => sv.view_id === vid);
                if (v) applyView(v);
              }}
              style={{
                background: 'var(--bg-elevated)',
                border: '1px solid var(--border)',
                color: 'var(--text-primary)',
                borderRadius: 6,
                padding: '6px 10px',
                fontSize: 13,
                cursor: 'pointer',
              }}
            >
              <option value="default">Default View</option>
              {savedViews.map(v => (
                <option key={v.view_id} value={v.view_id}>{v.name}</option>
              ))}
            </select>
            <button
              title="Save current filters as a view"
              onClick={() => setShowSaveViewModal(true)}
              style={{ background: 'var(--bg-elevated)', border: '1px solid var(--border)', color: 'var(--text-secondary)', borderRadius: 6, padding: '6px 8px', cursor: 'pointer', fontSize: 13 }}
            >
              +
            </button>
            {activeViewId !== 'default' && (
              <button
                title="Delete this view"
                onClick={() => deleteView(activeViewId)}
                style={{ background: 'none', border: 'none', color: 'var(--danger)', cursor: 'pointer', fontSize: 13, padding: '6px 4px' }}
              >
                ×
              </button>
            )}
          </div>
          <button className="btn-secondary" onClick={() => setShowReceiveDialog(true)}>
            <Plus size={16} /> Receive Stock
          </button>
          <button className="btn-secondary" onClick={() => setShowTransferDialog(true)}>
            <ArrowRightLeft size={16} /> Transfer
          </button>
          <button className="btn-secondary" onClick={() => setShowAdjustDialog(true)}>
            <Edit size={16} /> Adjust
          </button>
          <button className="btn-primary" onClick={loadInventory}>
            <RefreshCw size={16} /> Refresh
          </button>
        </div>
      </div>

      {/* Stats Bar */}
      <div className="inventory-stats-bar">
        <div className="stat-item">
          <Box size={20} />
          <div className="stat-content">
            <span className="stat-label">On Hand</span>
            <span className="stat-value">{stats.total_on_hand.toLocaleString()}</span>
          </div>
        </div>
        <div className="stat-item">
          <Lock size={20} />
          <div className="stat-content">
            <span className="stat-label">Reserved</span>
            <span className="stat-value">{stats.total_reserved.toLocaleString()}</span>
          </div>
        </div>
        <div className="stat-item stat-success">
          <CheckCircle size={20} />
          <div className="stat-content">
            <span className="stat-label">Available</span>
            <span className="stat-value">{stats.total_available.toLocaleString()}</span>
          </div>
        </div>
        <div className="stat-item stat-warning">
          <TrendingDown size={20} />
          <div className="stat-content">
            <span className="stat-label">Low Stock</span>
            <span className="stat-value">{stats.low_stock_count}</span>
          </div>
        </div>
        <div className="stat-item stat-danger">
          <AlertTriangle size={20} />
          <div className="stat-content">
            <span className="stat-label">Out of Stock</span>
            <span className="stat-value">{stats.out_of_stock_count}</span>
          </div>
        </div>
      </div>

      {/* View Tabs */}
      <div className="inventory-tabs">
        <button 
          className={`tab ${view === 'by_sku' ? 'active' : ''}`}
          onClick={() => setView('by_sku')}
        >
          <Package size={16} /> By SKU
        </button>
        <button 
          className={`tab ${view === 'by_location' ? 'active' : ''}`}
          onClick={() => setView('by_location')}
        >
          <Warehouse size={16} /> By Location
        </button>
        <button 
          className={`tab ${view === 'movements' ? 'active' : ''}`}
          onClick={() => setView('movements')}
        >
          <ArrowRightLeft size={16} /> Movements
        </button>
        <button 
          className={`tab ${view === 'reservations' ? 'active' : ''}`}
          onClick={() => setView('reservations')}
        >
          <Lock size={16} /> Reservations
        </button>
      </div>

      {/* Search & Filter Bar */}
      <div className="inventory-filter-bar">
        <div className="search-box">
          <Search size={18} />
          <input 
            type="text"
            placeholder="Search by SKU or product name..."
            value={filters.search}
            onChange={(e) => setFilters({...filters, search: e.target.value})}
          />
        </div>
        
        <select
          value={filters.location}
          onChange={(e) => setFilters({...filters, location: e.target.value})}
          className="filter-select"
        >
          <option value="">All Locations</option>
          {locations.map(loc => (
            <option key={loc.location_id} value={loc.location_id}>
              {loc.name}
            </option>
          ))}
        </select>

        <button 
          className={`btn-filter ${filters.lowStock ? 'active' : ''}`}
          onClick={() => setFilters({...filters, lowStock: !filters.lowStock})}
        >
          <TrendingDown size={16} /> Low Stock
        </button>

        <button 
          className={`btn-filter ${filters.outOfStock ? 'active' : ''}`}
          onClick={() => setFilters({...filters, outOfStock: !filters.outOfStock})}
        >
          <AlertTriangle size={16} /> Out of Stock
        </button>

        <button 
          className={`btn-filter ${filters.hasReservations ? 'active' : ''}`}
          onClick={() => setFilters({...filters, hasReservations: !filters.hasReservations})}
        >
          <Lock size={16} /> Has Reservations
        </button>
      </div>

      {/* Inventory Table */}
      {view === 'by_sku' && (
        <div className="inventory-table-wrapper">
          <table className="inventory-table">
            <thead>
              <tr>
                <th className="col-expand"></th>
                <th className="col-sku">SKU</th>
                <th className="col-product">Product</th>
                <th className="col-on-hand">On Hand</th>
                <th className="col-reserved">Reserved</th>
                <th className="col-available">Available</th>
                <th className="col-inbound">Inbound</th>
                <th className="col-safety">Safety Stock</th>
                <th className="col-status">Status</th>
                <th className="col-updated">Last Updated</th>
                <th className="col-actions">Actions</th>
              </tr>
            </thead>
            <tbody>
              {loading ? (
                <tr>
                  <td colSpan={11} className="loading-cell">
                    <RefreshCw className="animate-spin" size={24} />
                    <span>Loading inventory...</span>
                  </td>
                </tr>
              ) : filteredInventory.length === 0 ? (
                <tr>
                  <td colSpan={11} className="empty-cell">
                    <Package size={48} />
                    <h3>No inventory found</h3>
                    <p>Try adjusting your filters or add new inventory</p>
                  </td>
                </tr>
              ) : (
                filteredInventory.map(item => {
                  const status = getStockStatus(item);
                  const isExpanded = expandedSku === item.sku;
                  
                  return (
                    <>
                      <tr key={item.sku} className={`row-${status}`}>
                        <td className="col-expand">
                          <button 
                            className="expand-btn"
                            onClick={() => setExpandedSku(isExpanded ? null : item.sku)}
                          >
                            {isExpanded ? <ChevronDown size={16} /> : <ChevronRight size={16} />}
                          </button>
                        </td>
                        <td className="col-sku">
                          <div className="sku-code">{item.sku}</div>
                        </td>
                        <td className="col-product">
                          <div className="product-name">{item.product_name}</div>
                          {item.variant_name && (
                            <div className="variant-name">{item.variant_name}</div>
                          )}
                        </td>
                        <td className="col-on-hand">
                          <span className="qty-badge">{item.total_on_hand}</span>
                        </td>
                        <td className="col-reserved">
                          <span className="qty-badge qty-reserved">{item.total_reserved}</span>
                        </td>
                        <td className="col-available">
                          <span className={`qty-badge qty-${status}`}>{item.total_available}</span>
                        </td>
                        <td className="col-inbound">
                          {item.total_inbound > 0 ? (
                            <span className="qty-badge qty-inbound">{item.total_inbound}</span>
                          ) : (
                            <span className="qty-none">-</span>
                          )}
                        </td>
                        <td className="col-safety">
                          <span className="qty-safety">{item.safety_stock}</span>
                        </td>
                        <td className="col-status">
                          <span className={`status-badge status-${status}`}>
                            {status === 'healthy' && <CheckCircle size={14} />}
                            {status === 'low' && <TrendingDown size={14} />}
                            {status === 'out' && <AlertTriangle size={14} />}
                            {status === 'reserved' && <Lock size={14} />}
                            {status.charAt(0).toUpperCase() + status.slice(1)}
                          </span>
                        </td>
                        <td className="col-updated">
                          <span className="date-text">{formatDate(item.updated_at)}</span>
                        </td>
                        <td className="col-actions">
                          <button 
                            className="btn-action"
                            onClick={() => {
                              setSelectedSku(item);
                              setAdjustmentForm({...adjustmentForm, sku: item.sku});
                              setShowAdjustDialog(true);
                            }}
                          >
                            <Edit size={14} />
                          </button>
                        </td>
                      </tr>
                      
                      {/* Expanded Location Details */}
                      {isExpanded && (
                        <tr className="expanded-row">
                          <td colSpan={11}>
                            <div className="location-breakdown">
                              <h4><MapPin size={16} /> Stock by Location</h4>
                              <div className="location-grid">
                                {item.locations.map(loc => (
                                  <div key={loc.location_id} className="location-card">
                                    <div className="location-header">
                                      <Warehouse size={16} />
                                      <span className="location-name">{loc.location_name}</span>
                                    </div>
                                    <div className="location-stats">
                                      <div className="location-stat">
                                        <span className="stat-label">On Hand</span>
                                        <span className="stat-value">{loc.on_hand}</span>
                                      </div>
                                      <div className="location-stat">
                                        <span className="stat-label">Reserved</span>
                                        <span className="stat-value reserved">{loc.reserved}</span>
                                      </div>
                                      <div className="location-stat">
                                        <span className="stat-label">Available</span>
                                        <span className="stat-value available">{loc.available}</span>
                                      </div>
                                      {loc.inbound > 0 && (
                                        <div className="location-stat">
                                          <span className="stat-label">Inbound</span>
                                          <span className="stat-value inbound">{loc.inbound}</span>
                                        </div>
                                      )}
                                    </div>
                                  </div>
                                ))}
                              </div>
                            </div>
                          </td>
                        </tr>
                      )}
                    </>
                  );
                })
              )}
            </tbody>
          </table>
        </div>
      )}

      {/* Adjust Stock Dialog */}
      {showAdjustDialog && (
        <div className="modal-overlay" onClick={() => setShowAdjustDialog(false)}>
          <div className="modal-content modal-large" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <h2><Edit size={20} /> Adjust Stock</h2>
              <button className="modal-close" onClick={() => setShowAdjustDialog(false)}>
                ✕
              </button>
            </div>
            <div className="modal-body">
              <div className="form-row">
                <div className="form-group">
                  <label>SKU *</label>
                  <input
                    type="text"
                    value={adjustmentForm.sku}
                    onChange={(e) => setAdjustmentForm({...adjustmentForm, sku: e.target.value})}
                    placeholder="Enter SKU"
                    className="form-input"
                  />
                </div>
                <div className="form-group">
                  <label>Location *</label>
                  <select
                    value={adjustmentForm.location_id}
                    onChange={(e) => setAdjustmentForm({...adjustmentForm, location_id: e.target.value})}
                    className="form-input"
                  >
                    <option value="">Select location...</option>
                    {locations.map(loc => (
                      <option key={loc.location_id} value={loc.location_id}>
                        {loc.name}
                      </option>
                    ))}
                  </select>
                </div>
              </div>

              <div className="form-row">
                <div className="form-group">
                  <label>Type *</label>
                  <select
                    value={adjustmentForm.type}
                    onChange={(e) => setAdjustmentForm({...adjustmentForm, type: e.target.value as any})}
                    className="form-input"
                  >
                    <option value="adjustment">Adjustment</option>
                    <option value="receipt">Receipt</option>
                    <option value="return">Return</option>
                  </select>
                </div>
                <div className="form-group">
                  <label>Quantity * (use negative for decrease)</label>
                  <input
                    type="number"
                    value={adjustmentForm.quantity}
                    onChange={(e) => setAdjustmentForm({...adjustmentForm, quantity: parseInt(e.target.value) || 0})}
                    className="form-input"
                  />
                </div>
              </div>

              <div className="form-group">
                <label>Reason Code *</label>
                <select
                  value={adjustmentForm.reason_code}
                  onChange={(e) => setAdjustmentForm({...adjustmentForm, reason_code: e.target.value})}
                  className="form-input"
                >
                  <option value="">Select reason...</option>
                  <option value="damaged">Damaged</option>
                  <option value="lost">Lost</option>
                  <option value="found">Found</option>
                  <option value="count_correction">Count Correction</option>
                  <option value="supplier_receipt">Supplier Receipt</option>
                  <option value="customer_return">Customer Return</option>
                  <option value="expired">Expired</option>
                  <option value="other">Other</option>
                </select>
              </div>

              <div className="form-group">
                <label>Notes</label>
                <textarea
                  value={adjustmentForm.notes}
                  onChange={(e) => setAdjustmentForm({...adjustmentForm, notes: e.target.value})}
                  placeholder="Optional notes about this adjustment..."
                  className="form-input"
                  rows={3}
                />
              </div>

              <div className="adjustment-preview">
                <h4>Preview Impact</h4>
                <div className="preview-grid">
                  <div className="preview-item">
                    <span className="preview-label">Change</span>
                    <span className={`preview-value ${adjustmentForm.quantity >= 0 ? 'positive' : 'negative'}`}>
                      {adjustmentForm.quantity >= 0 ? '+' : ''}{adjustmentForm.quantity}
                    </span>
                  </div>
                </div>
              </div>
            </div>
            <div className="modal-footer">
              <button className="btn-secondary" onClick={() => setShowAdjustDialog(false)}>
                Cancel
              </button>
              <button 
                className="btn-primary"
                onClick={adjustStock}
                disabled={!adjustmentForm.sku || !adjustmentForm.location_id || !adjustmentForm.reason_code}
              >
                Confirm Adjustment
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Transfer Dialog */}
      {showTransferDialog && (
        <div className="modal-overlay" onClick={() => setShowTransferDialog(false)}>
          <div className="modal-content" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <h2><ArrowRightLeft size={20} /> Transfer Stock</h2>
              <button className="modal-close" onClick={() => setShowTransferDialog(false)}>
                ✕
              </button>
            </div>
            <div className="modal-body">
              <div className="form-group">
                <label>SKU *</label>
                <input
                  type="text"
                  value={transferForm.sku}
                  onChange={(e) => setTransferForm({...transferForm, sku: e.target.value})}
                  placeholder="Enter SKU"
                  className="form-input"
                />
              </div>

              <div className="form-row">
                <div className="form-group">
                  <label>From Location *</label>
                  <select
                    value={transferForm.from_location}
                    onChange={(e) => setTransferForm({...transferForm, from_location: e.target.value})}
                    className="form-input"
                  >
                    <option value="">Select location...</option>
                    {locations.map(loc => (
                      <option key={loc.location_id} value={loc.location_id}>
                        {loc.name}
                      </option>
                    ))}
                  </select>
                </div>
                <div className="form-group">
                  <label>To Location *</label>
                  <select
                    value={transferForm.to_location}
                    onChange={(e) => setTransferForm({...transferForm, to_location: e.target.value})}
                    className="form-input"
                  >
                    <option value="">Select location...</option>
                    {locations.filter(loc => loc.location_id !== transferForm.from_location).map(loc => (
                      <option key={loc.location_id} value={loc.location_id}>
                        {loc.name}
                      </option>
                    ))}
                  </select>
                </div>
              </div>

              <div className="form-group">
                <label>Quantity *</label>
                <input
                  type="number"
                  value={transferForm.quantity}
                  onChange={(e) => setTransferForm({...transferForm, quantity: parseInt(e.target.value) || 0})}
                  className="form-input"
                  min="1"
                />
              </div>

              <div className="form-group">
                <label>Notes</label>
                <textarea
                  value={transferForm.notes}
                  onChange={(e) => setTransferForm({...transferForm, notes: e.target.value})}
                  placeholder="Optional notes about this transfer..."
                  className="form-input"
                  rows={3}
                />
              </div>
            </div>
            <div className="modal-footer">
              <button className="btn-secondary" onClick={() => setShowTransferDialog(false)}>
                Cancel
              </button>
              <button 
                className="btn-primary"
                onClick={createTransfer}
                disabled={!transferForm.sku || !transferForm.from_location || !transferForm.to_location || transferForm.quantity <= 0}
              >
                Create Transfer
              </button>
            </div>
          </div>
        </div>
      )}
      {/* A-004: Save View Modal */}
      {showSaveViewModal && (
        <div className="modal-overlay" onClick={() => setShowSaveViewModal(false)} style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.5)', zIndex: 1000, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
          <div onClick={e => e.stopPropagation()} style={{ background: 'var(--bg-elevated)', borderRadius: 10, padding: 24, minWidth: 320, border: '1px solid var(--border)' }}>
            <h3 style={{ margin: '0 0 16px', color: 'var(--text-primary)', fontSize: 16 }}>Save Current View</h3>
            <input
              type="text"
              placeholder="View name…"
              value={saveViewName}
              onChange={e => setSaveViewName(e.target.value)}
              onKeyDown={e => { if (e.key === 'Enter') saveCurrentView(); }}
              autoFocus
              style={{ width: '100%', boxSizing: 'border-box', background: 'var(--bg-primary)', border: '1px solid var(--border)', color: 'var(--text-primary)', borderRadius: 6, padding: '8px 12px', fontSize: 14, marginBottom: 12 }}
            />
            <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
              <button onClick={() => setShowSaveViewModal(false)} style={{ background: 'none', border: '1px solid var(--border)', color: 'var(--text-secondary)', borderRadius: 6, padding: '6px 14px', cursor: 'pointer' }}>Cancel</button>
              <button onClick={saveCurrentView} disabled={savingView || !saveViewName.trim()} style={{ background: 'var(--primary)', border: 'none', color: '#fff', borderRadius: 6, padding: '6px 14px', cursor: 'pointer', opacity: savingView ? 0.6 : 1 }}>
                {savingView ? 'Saving…' : 'Save View'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};

export default Inventory;

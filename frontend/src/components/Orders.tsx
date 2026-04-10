import { useState, useEffect } from 'react';
import { 
  Download, RefreshCw, Package, Printer, XCircle, Clock, AlertCircle, 
  Check, Lock, Unlock, Tag, MessageSquare, Search, Filter, Truck,
  FileText, Calendar, MapPin, User, Box, AlertTriangle, RotateCcw
} from 'lucide-react';
import { getActiveTenantId } from '../contexts/TenantContext';
import './Orders.css';

interface Order {
  id: string;
  channel_order_id: string;
  channel: string;
  status: string;
  total_value: number;
  currency: string;
  order_date: string;
  customer_name: string;
  customer_email: string;
  customer_phone: string;
  shipping_address_line1: string;
  shipping_address_line2?: string;
  shipping_city: string;
  shipping_postcode: string;
  shipping_country: string;
  line_items: any[];
  on_hold: boolean;
  stock_status?: 'insufficient' | 'low' | null;
  label_generated: boolean;
  tracking_number?: string;
  label_url?: string;
  carrier?: string;
  service_code?: string;
}

const Orders = () => {
  const [orders, setOrders] = useState<Order[]>([]);
  const [stats, setStats] = useState({ 
    total: 0, imported: 0, processing: 0, ready: 0, fulfilled: 0, 
    on_hold: 0, exceptions: 0 
  });
  const [loading, setLoading] = useState(true);
  const [selectedOrders, setSelectedOrders] = useState<string[]>([]);
  const [selectedOrder, setSelectedOrder] = useState<Order | null>(null);
  
  // Filters including SKU
  const [filters, setFilters] = useState({
    search: '',
    status: '',
    channel: '',
    sku: '',
    stockIssues: false,
    onHold: false,
    needsLabel: false, // NEW: Show orders that need labels
  });
  const [showFilters, setShowFilters] = useState(false);
  
  // Label generation
  const [showLabelDialog, setShowLabelDialog] = useState(false);
  const [selectedCarrier, setSelectedCarrier] = useState('royal-mail');
  const [selectedService, setSelectedService] = useState('1ST');
  const [carriers, setCarriers] = useState<any[]>([]);
  const [generatingLabel, setGeneratingLabel] = useState(false);
  
  // Bulk actions
  const [showHoldDialog, setShowHoldDialog] = useState(false);
  const [holdReason, setHoldReason] = useState('');

  const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';
  const tenantId = getActiveTenantId() || localStorage.getItem('tenantId') || 'default-tenant';

  useEffect(() => {
    loadOrders();
    loadStats();
    loadCarriers();
  }, [filters]);

  const loadOrders = async () => {
    setLoading(true);
    try {
      const params = new URLSearchParams();
      if (filters.status) params.append('status', filters.status);
      if (filters.channel) params.append('channel', filters.channel);
      if (filters.sku) params.append('sku', filters.sku);
      if (filters.stockIssues) params.append('stock_issues', 'true');
      if (filters.onHold) params.append('on_hold', 'true');
      if (filters.needsLabel) params.append('needs_label', 'true');
      if (filters.search) params.append('search', filters.search);

      const response = await fetch(`${API_BASE}/orders?${params}`, {
        headers: { 'X-Tenant-Id': tenantId },
      });
      const data = await response.json();
      setOrders(data.orders || []);
    } catch (error) {
      console.error('Failed to load orders:', error);
    } finally {
      setLoading(false);
    }
  };

  const loadStats = async () => {
    try {
      const response = await fetch(`${API_BASE}/orders/stats`, {
        headers: { 'X-Tenant-Id': tenantId },
      });
      const data = await response.json();
      setStats(data.stats || stats);
    } catch (error) {
      console.error('Failed to load stats:', error);
    }
  };

  const loadCarriers = async () => {
    try {
      const response = await fetch(`${API_BASE}/dispatch/carriers`, {
        headers: { 'X-Tenant-Id': tenantId },
      });
      const data = await response.json();
      setCarriers(data.carriers || []);
    } catch (error) {
      console.error('Failed to load carriers:', error);
    }
  };

  // Generate shipping label
  const generateLabel = async () => {
    if (!selectedOrder) return;

    setGeneratingLabel(true);
    try {
      const response = await fetch(`${API_BASE}/dispatch/shipments`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-Tenant-Id': tenantId,
        },
        body: JSON.stringify({
          order_id: selectedOrder.id,
          carrier_id: selectedCarrier,
          service_code: selectedService,
          from_address: {
            name: 'Your Warehouse',
            address_line1: '123 Warehouse St',
            city: 'London',
            postal_code: 'SW1A 1AA',
            country: 'GB',
          },
          to_address: {
            name: selectedOrder.customer_name,
            address_line1: selectedOrder.shipping_address_line1,
            address_line2: selectedOrder.shipping_address_line2,
            city: selectedOrder.shipping_city,
            postal_code: selectedOrder.shipping_postcode,
            country: selectedOrder.shipping_country,
            email: selectedOrder.customer_email,
            phone: selectedOrder.customer_phone,
          },
          parcels: selectedOrder.line_items.map(item => ({
            weight: 0.5, // You'd get this from product data
            length: 20,
            width: 15,
            height: 10,
            description: item.product_name,
          })),
          reference: selectedOrder.channel_order_id,
        }),
      });

      if (!response.ok) {
        throw new Error('Failed to generate label');
      }

      const data = await response.json();
      
      // Download label
      if (data.label_url) {
        window.open(data.label_url, '_blank');
      }

      // Update order status and reload
      alert(`Label generated! Tracking: ${data.tracking_number}\n\nStock has been deducted from inventory.`);
      setShowLabelDialog(false);
      setSelectedOrder(null);
      loadOrders();
      loadStats();
    } catch (error) {
      console.error('Failed to generate label:', error);
      alert('Failed to generate shipping label. Please try again.');
    } finally {
      setGeneratingLabel(false);
    }
  };

  // Cancel label (void shipment)
  const cancelLabel = async (order: Order) => {
    if (!order.tracking_number) return;
    if (!confirm(`Cancel label for order ${order.channel_order_id}?\n\nThis will release the stock reservation and void the shipping label.`)) {
      return;
    }

    try {
      // Find shipment ID (you'd get this from order data)
      const shipmentId = order.id; // Simplified - you'd have actual shipment_id
      
      await fetch(`${API_BASE}/dispatch/shipments/${shipmentId}`, {
        method: 'DELETE',
        headers: { 'X-Tenant-Id': tenantId },
      });

      alert('Label cancelled. Stock returned to inventory.');
      loadOrders();
      loadStats();
    } catch (error) {
      console.error('Failed to cancel label:', error);
      alert('Failed to cancel label');
    }
  };

  // Bulk print labels
  const bulkGenerateLabels = async () => {
    if (selectedOrders.length === 0) return;
    
    setGeneratingLabel(true);
    try {
      for (const orderId of selectedOrders) {
        const order = orders.find(o => o.id === orderId);
        if (!order || order.label_generated) continue;

        setSelectedOrder(order);
        await generateLabel();
      }
      
      setSelectedOrders([]);
      alert(`Generated ${selectedOrders.length} labels!`);
    } finally {
      setGeneratingLabel(false);
    }
  };

  // Hold orders
  const holdOrders = async () => {
    if (!holdReason.trim()) {
      alert('Please enter a reason for the hold');
      return;
    }

    try {
      await fetch(`${API_BASE}/orders/hold`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-Tenant-Id': tenantId,
        },
        body: JSON.stringify({
          order_ids: selectedOrders,
          reason: holdReason,
          created_by: 'current_user',
        }),
      });
      
      setShowHoldDialog(false);
      setHoldReason('');
      setSelectedOrders([]);
      loadOrders();
      loadStats();
    } catch (error) {
      console.error('Failed to hold orders:', error);
      alert('Failed to hold orders');
    }
  };

  const releaseHolds = async () => {
    try {
      await fetch(`${API_BASE}/orders/hold/release`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-Tenant-Id': tenantId,
        },
        body: JSON.stringify({
          order_ids: selectedOrders,
          released_by: 'current_user',
        }),
      });
      
      setSelectedOrders([]);
      loadOrders();
      loadStats();
    } catch (error) {
      console.error('Failed to release holds:', error);
      alert('Failed to release holds');
    }
  };

  // Selection
  const toggleOrderSelection = (orderId: string) => {
    setSelectedOrders(prev =>
      prev.includes(orderId) ? prev.filter(id => id !== orderId) : [...prev, orderId]
    );
  };

  const selectAll = () => {
    setSelectedOrders(selectedOrders.length === orders.length ? [] : orders.map(o => o.id));
  };

  // Status badge
  const getStatusBadge = (order: Order) => {
    if (order.label_generated) {
      return <span className="status-badge status-fulfilled"><Check size={12} /> Label Printed</span>;
    }
    if (order.on_hold) {
      return <span className="status-badge status-hold"><Lock size={12} /> On Hold</span>;
    }
    if (order.stock_status === 'insufficient') {
      return <span className="status-badge status-exception"><AlertTriangle size={12} /> No Stock</span>;
    }
    if (order.status === 'ready') {
      return <span className="status-badge status-ready"><Box size={12} /> Ready to Ship</span>;
    }
    return <span className="status-badge status-processing"><Clock size={12} /> Processing</span>;
  };

  return (
    <div className="orders-container">
      {/* Header */}
      <div className="orders-header">
        <div className="header-left">
          <h1>Orders & Fulfillment</h1>
          <p className="subtitle">Royal Mail Click & Drop Style</p>
        </div>
        <div className="header-actions">
          <button onClick={() => window.location.href = '/marketplace/import'} className="btn-primary">
            <Download size={16} />
            Import Orders
          </button>
        </div>
      </div>

      {/* Stats Bar */}
      <div className="stats-bar">
        <div className="stat-card" onClick={() => setFilters({ ...filters, status: '', needsLabel: false })}>
          <div className="stat-label">TOTAL</div>
          <div className="stat-value">{stats.total}</div>
        </div>
        <div className="stat-card" onClick={() => setFilters({ ...filters, needsLabel: true })}>
          <div className="stat-label">NEEDS LABEL</div>
          <div className="stat-value">{orders.filter(o => !o.label_generated && !o.on_hold).length}</div>
        </div>
        <div className="stat-card" onClick={() => setFilters({ ...filters, status: 'fulfilled' })}>
          <div className="stat-label">SHIPPED</div>
          <div className="stat-value">{stats.fulfilled}</div>
        </div>
        <div className="stat-card highlight-hold" onClick={() => setFilters({ ...filters, onHold: true })}>
          <div className="stat-label">ON HOLD</div>
          <div className="stat-value">{stats.on_hold}</div>
        </div>
        <div className="stat-card highlight-exception" onClick={() => setFilters({ ...filters, stockIssues: true })}>
          <div className="stat-label">STOCK ISSUES</div>
          <div className="stat-value">{orders.filter(o => o.stock_status === 'insufficient').length}</div>
        </div>
      </div>

      {/* Action Bar */}
      <div className="action-bar">
        <div className="action-left">
          <button onClick={() => setShowFilters(!showFilters)} className="btn-secondary">
            <Filter size={16} />
            Filters
          </button>
          
          {selectedOrders.length > 0 && (
            <>
              <button onClick={bulkGenerateLabels} className="btn-primary">
                <Printer size={16} />
                Print {selectedOrders.length} Label{selectedOrders.length > 1 ? 's' : ''}
              </button>
              <button onClick={() => setShowHoldDialog(true)} className="btn-secondary">
                <Lock size={16} />
                Hold
              </button>
              <button onClick={releaseHolds} className="btn-secondary">
                <Unlock size={16} />
                Release
              </button>
            </>
          )}
        </div>

        <div className="action-right">
          <button onClick={loadOrders} className="btn-secondary">
            <RefreshCw size={16} />
          </button>
        </div>
      </div>

      {/* Filters */}
      {showFilters && (
        <div className="filters-panel">
          <div className="filter-row">
            <div className="filter-group">
              <label>Search</label>
              <input
                type="text"
                placeholder="Order ID, Customer, SKU..."
                value={filters.search}
                onChange={(e) => setFilters({ ...filters, search: e.target.value })}
              />
            </div>
            <div className="filter-group">
              <label>Channel</label>
              <select value={filters.channel} onChange={(e) => setFilters({ ...filters, channel: e.target.value })}>
                <option value="">All</option>
                <option value="amazon">Amazon</option>
                <option value="ebay">eBay</option>
                <option value="temu">Temu</option>
              </select>
            </div>
            <div className="filter-group">
              <label>
                <input type="checkbox" checked={filters.needsLabel} onChange={(e) => setFilters({ ...filters, needsLabel: e.target.checked })} />
                Needs Label
              </label>
            </div>
            <div className="filter-group">
              <label>
                <input type="checkbox" checked={filters.stockIssues} onChange={(e) => setFilters({ ...filters, stockIssues: e.target.checked })} />
                Stock Issues
              </label>
            </div>
          </div>
        </div>
      )}

      {/* Orders Table */}
      <div className="orders-table-container">
        <table className="orders-table">
          <thead>
            <tr>
              <th style={{ width: '40px' }}>
                <input type="checkbox" checked={selectedOrders.length === orders.length && orders.length > 0} onChange={selectAll} />
              </th>
              <th style={{ width: '140px' }}>Order ID</th>
              <th style={{ width: '120px' }}>Channel</th>
              <th style={{ width: '150px' }}>Status</th>
              <th style={{ width: '200px' }}>Customer</th>
              <th style={{ width: '300px' }}>Products</th>
              <th style={{ width: '120px' }}>Total</th>
              <th style={{ width: '150px' }}>Actions</th>
            </tr>
          </thead>
          <tbody>
            {loading ? (
              <tr><td colSpan={8} style={{ textAlign: 'center', padding: '40px' }}>Loading orders...</td></tr>
            ) : orders.length === 0 ? (
              <tr><td colSpan={8} style={{ textAlign: 'center', padding: '40px' }}>No orders found</td></tr>
            ) : (
              orders.map((order) => (
                <tr key={order.id} className={order.on_hold ? 'row-on-hold' : ''}>
                  <td onClick={(e) => e.stopPropagation()}>
                    <input type="checkbox" checked={selectedOrders.includes(order.id)} onChange={() => toggleOrderSelection(order.id)} />
                  </td>
                  <td>
                    <div className="order-id">{order.channel_order_id}</div>
                    <div className="order-date">{new Date(order.order_date).toLocaleDateString()}</div>
                  </td>
                  <td>{order.channel || 'Unknown'}</td>
                  <td>{getStatusBadge(order)}</td>
                  <td>
                    <div className="customer-info">
                      <div>{order.customer_name}</div>
                      <div className="customer-location">{order.shipping_city}, {order.shipping_postcode}</div>
                    </div>
                  </td>
                  <td>
                    <div className="product-summary">
                      {order.line_items?.slice(0, 2).map((item: any, idx: number) => (
                        <div key={idx} className="product-line">
                          {item.quantity}x {item.product_name}
                          {item.sku && <span className="sku-badge">{item.sku}</span>}
                        </div>
                      ))}
                      {order.line_items?.length > 2 && <div className="more-items">+{order.line_items.length - 2} more</div>}
                    </div>
                  </td>
                  <td>{order.currency} {order.total_value?.toFixed(2)}</td>
                  <td onClick={(e) => e.stopPropagation()}>
                    {order.label_generated ? (
                      <div className="action-buttons">
                        <button className="btn-icon-small" onClick={() => window.open(order.label_url, '_blank')} title="Download Label">
                          <Download size={14} />
                        </button>
                        <button className="btn-icon-small btn-danger" onClick={() => cancelLabel(order)} title="Cancel Label">
                          <XCircle size={14} />
                        </button>
                      </div>
                    ) : (
                      <button 
                        className="btn-action" 
                        onClick={() => { setSelectedOrder(order); setShowLabelDialog(true); }}
                        disabled={order.on_hold || order.stock_status === 'insufficient'}
                      >
                        <Printer size={14} />
                        Print Label
                      </button>
                    )}
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>

      {/* Label Generation Dialog */}
      {showLabelDialog && selectedOrder && (
        <div className="modal-overlay" onClick={() => setShowLabelDialog(false)}>
          <div className="modal-content" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <h2>Generate Shipping Label</h2>
              <button onClick={() => setShowLabelDialog(false)} className="btn-icon">×</button>
            </div>
            <div className="modal-body">
              <div className="label-preview">
                <h3>Order: {selectedOrder.channel_order_id}</h3>
                <p><strong>Ship to:</strong> {selectedOrder.customer_name}</p>
                <p>{selectedOrder.shipping_address_line1}, {selectedOrder.shipping_city} {selectedOrder.shipping_postcode}</p>
              </div>

              <div className="carrier-selection">
                <label>Carrier:</label>
                <select value={selectedCarrier} onChange={(e) => setSelectedCarrier(e.target.value)}>
                  {carriers.map(c => (
                    <option key={c.id} value={c.id}>{c.display_name}</option>
                  ))}
                </select>
              </div>

              <div className="service-selection">
                <label>Service:</label>
                <select value={selectedService} onChange={(e) => setSelectedService(e.target.value)}>
                  <option value="1ST">1st Class (Next Day)</option>
                  <option value="2ND">2nd Class (2-3 Days)</option>
                  <option value="SD1">Special Delivery by 1pm</option>
                </select>
              </div>

              <div className="warning-box">
                <AlertTriangle size={16} />
                <span>Generating this label will deduct stock from inventory</span>
              </div>

              <div className="modal-actions">
                <button onClick={() => setShowLabelDialog(false)} className="btn-secondary">Cancel</button>
                <button onClick={generateLabel} className="btn-primary" disabled={generatingLabel}>
                  {generatingLabel ? 'Generating...' : 'Generate & Print Label'}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Hold Dialog */}
      {showHoldDialog && (
        <div className="modal-overlay" onClick={() => setShowHoldDialog(false)}>
          <div className="modal-content small" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <h2>Hold Orders</h2>
              <button onClick={() => setShowHoldDialog(false)} className="btn-icon">×</button>
            </div>
            <div className="modal-body">
              <label>Reason:</label>
              <select value={holdReason} onChange={(e) => setHoldReason(e.target.value)}>
                <option value="">Select reason...</option>
                <option value="payment_issue">Payment Issue</option>
                <option value="customer_requested">Customer Requested</option>
                <option value="stock_issue">Stock Issue</option>
                <option value="address_verification">Address Verification</option>
                <option value="other">Other</option>
              </select>
              <div className="modal-actions">
                <button onClick={() => setShowHoldDialog(false)} className="btn-secondary">Cancel</button>
                <button onClick={holdOrders} className="btn-primary">Apply Hold</button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};

export default Orders;

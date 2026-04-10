import React, { useState, useEffect, useCallback } from 'react';
import './Dispatch.css';

// ============================================================================
// TYPES
// ============================================================================

interface Carrier {
  id: string;
  name: string;
  display_name: string;
  country: string;
  features: string[];
  is_active: boolean;
  is_configured?: boolean;
}

interface Rate {
  service_code: string;
  service_name: string;
  cost: { amount: number; currency: string };
  estimated_days: number;
  carrier: string;
}

interface Shipment {
  shipment_id: string;
  order_ids: string[];
  carrier_id: string;
  service_code: string;
  service_name: string;
  tracking_number: string;
  label_url: string;
  status: string;
  cost: number;
  currency: string;
  created_at: string;
  to_address?: { name: string; postal_code: string; country: string };
  parcels?: { weight: number }[];
}

interface ManifestRecord {
  manifest_id: string;
  carrier_id: string;
  carrier_name: string;
  document_format: string;
  download_url: string;
  shipment_ids: string[];
  shipment_count: number;
  total_weight_kg: number;
  total_cost: number;
  currency: string;
  status: string;
  error_message?: string;
  manifest_date: string;
  created_at: string;
}

// ============================================================================
// INLINE API HELPER — consistent with DespatchConsole / Analytics pattern
// ============================================================================

const API_BASE = (import.meta as any).env?.VITE_API_URL ?? 'http://localhost:8080/api/v1';

function api(path: string, init?: RequestInit): Promise<Response> {
  const tenantId = localStorage.getItem('tenantId') || 'default-tenant';
  const token = localStorage.getItem('authToken') || '';
  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      'X-Tenant-Id': tenantId,
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...(init?.headers ?? {}),
    },
  });
}

// ============================================================================
// COMPONENT
// ============================================================================

const Dispatch: React.FC = () => {
  const [carriers, setCarriers] = useState<Carrier[]>([]);
  const [shipments, setShipments] = useState<Shipment[]>([]);
  const [selectedCarrier, setSelectedCarrier] = useState<string>('');
  const [rates, setRates] = useState<Rate[]>([]);
  const [loading, setLoading] = useState(false);
  const [activeTab, setActiveTab] = useState<'shipments' | 'create' | 'rates' | 'manifest'>('shipments');

  // Manifest state
  const [todayShipments, setTodayShipments] = useState<Shipment[]>([]);
  const [manifestHistory, setManifestHistory] = useState<ManifestRecord[]>([]);
  const [manifestDate, setManifestDate] = useState<string>(
    new Date().toISOString().split('T')[0]
  );
  const [manifestLoading, setManifestLoading] = useState(false);
  const [manifestGenerating, setManifestGenerating] = useState<string | null>(null);
  const [manifestError, setManifestError] = useState<string | null>(null);
  const [manifestSuccess, setManifestSuccess] = useState<string | null>(null);

  useEffect(() => {
    loadCarriers();
    loadShipments();
  }, []);

  useEffect(() => {
    if (activeTab === 'manifest') {
      loadTodayShipments();
      loadManifestHistory();
    }
  }, [activeTab, manifestDate]);

  // =========================================================================
  // DATA LOADERS
  // =========================================================================

  async function loadCarriers() {
    try {
      const res = await api('/dispatch/carriers/configured');
      const data = await res.json();
      setCarriers(data.carriers || []);
    } catch {
      console.error('Failed to load carriers');
    }
  }

  async function loadShipments() {
    setLoading(true);
    try {
      const res = await api('/dispatch/shipments');
      const data = await res.json();
      setShipments(data.shipments || []);
    } catch {
      console.error('Failed to load shipments');
    } finally {
      setLoading(false);
    }
  }

  const loadTodayShipments = useCallback(async () => {
    setManifestLoading(true);
    try {
      const res = await api('/dispatch/shipments?limit=500');
      const data = await res.json();
      const all: Shipment[] = data.shipments || [];
      const filtered = all.filter(s => {
        if (!s.created_at) return false;
        const d = new Date(s.created_at).toISOString().split('T')[0];
        return d === manifestDate;
      });
      setTodayShipments(filtered);
    } catch {
      console.error('Failed to load today shipments');
    } finally {
      setManifestLoading(false);
    }
  }, [manifestDate]);

  async function loadManifestHistory() {
    try {
      const res = await api('/dispatch/manifest/history');
      const data = await res.json();
      setManifestHistory(data.manifests || []);
    } catch {
      console.error('Failed to load manifest history');
    }
  }

  // =========================================================================
  // MANIFEST ACTIONS
  // =========================================================================

  async function generateManifest(carrierID: string) {
    setManifestGenerating(carrierID);
    setManifestError(null);
    setManifestSuccess(null);
    try {
      const res = await api('/dispatch/manifest', {
        method: 'POST',
        body: JSON.stringify({ carrier_id: carrierID, manifest_date: manifestDate }),
      });
      const data = await res.json();
      if (!res.ok) {
        setManifestError(data.error || 'Failed to generate manifest');
        return;
      }
      const manifests: ManifestRecord[] = data.manifests || [];
      if (manifests.length === 0) {
        setManifestError('No shipments found for this carrier on the selected date.');
        return;
      }
      const m = manifests[0];
      setManifestSuccess(`Manifest generated for ${m.carrier_name} — ${m.shipment_count} shipment${m.shipment_count !== 1 ? 's' : ''}`);
      if (m.download_url) downloadManifest(m);
      await loadManifestHistory();
    } catch {
      setManifestError('Network error generating manifest');
    } finally {
      setManifestGenerating(null);
    }
  }

  async function generateAllManifests() {
    setManifestGenerating('__all__');
    setManifestError(null);
    setManifestSuccess(null);
    try {
      const res = await api('/dispatch/manifest', {
        method: 'POST',
        body: JSON.stringify({ manifest_date: manifestDate }),
      });
      const data = await res.json();
      if (!res.ok) {
        setManifestError(data.error || 'Failed to generate manifests');
        return;
      }
      const manifests: ManifestRecord[] = data.manifests || [];
      if (manifests.length === 0) {
        setManifestError('No shipments found for the selected date.');
        return;
      }
      const total = manifests.reduce((a, m) => a + m.shipment_count, 0);
      setManifestSuccess(`Generated ${manifests.length} manifest${manifests.length !== 1 ? 's' : ''} covering ${total} shipments`);
      for (const m of manifests) if (m.download_url) downloadManifest(m);
      await loadManifestHistory();
    } catch {
      setManifestError('Network error generating manifests');
    } finally {
      setManifestGenerating(null);
    }
  }

  function downloadManifest(m: ManifestRecord) {
    if (!m.download_url) return;
    const ext = m.document_format === 'pdf' ? 'pdf' : 'csv';
    const filename = `manifest_${m.carrier_id}_${m.manifest_date}.${ext}`;
    if (m.download_url.startsWith('data:')) {
      const a = document.createElement('a');
      a.href = m.download_url;
      a.download = filename;
      a.click();
    } else {
      window.open(m.download_url, '_blank');
    }
  }

  async function downloadHistoricManifest(manifestId: string) {
    try {
      const res = await api(`/dispatch/manifest/${manifestId}`);
      const m: ManifestRecord = await res.json();
      if (m.download_url) downloadManifest(m);
    } catch {
      console.error('Failed to fetch manifest for download');
    }
  }

  // =========================================================================
  // RATE SHOPPING
  // =========================================================================

  const getRates = async () => {
    if (!selectedCarrier) return;
    setLoading(true);
    try {
      const res = await api(`/dispatch/rates?carrier_id=${selectedCarrier}`, {
        method: 'POST',
        body: JSON.stringify({
          from_address: { name: 'My Warehouse', address_line1: '123 Warehouse St', city: 'London', postal_code: 'SW1A 1AA', country: 'GB' },
          to_address:   { name: 'Customer',    address_line1: '456 Customer Rd',  city: 'Manchester', postal_code: 'M1 1AA', country: 'GB' },
          parcels: [{ weight: 2.5, length: 30, width: 20, height: 10 }],
          currency: 'GBP',
        }),
      });
      const data = await res.json();
      setRates(data.rates || []);
    } catch {
      console.error('Failed to get rates');
    } finally {
      setLoading(false);
    }
  };

  // =========================================================================
  // HELPERS
  // =========================================================================

  const getStatusColor = (status: string) => {
    switch (status) {
      case 'label_generated': return 'status-label-generated';
      case 'dispatched':      return 'status-dispatched';
      case 'delivered':       return 'status-delivered';
      default:                return 'status-planned';
    }
  };

  const getManifestStatusColor = (status: string) => {
    switch (status) {
      case 'generated': return 'status-delivered';
      case 'failed':    return 'status-manifest-failed';
      default:          return 'status-planned';
    }
  };

  const carrierName = (id: string) =>
    carriers.find(c => c.id === id)?.display_name || id;

  // Group today's shipments by carrier
  const shipmentsByCarrier = todayShipments.reduce<Record<string, Shipment[]>>((acc, s) => {
    if (!acc[s.carrier_id]) acc[s.carrier_id] = [];
    acc[s.carrier_id].push(s);
    return acc;
  }, {});

  const configuredCarrierIDs = carriers.filter(c => c.is_configured).map(c => c.id);
  const activeCarrierIDs = Object.keys(shipmentsByCarrier);
  const manifestCarrierIDs = Array.from(new Set([...activeCarrierIDs, ...configuredCarrierIDs]));

  // =========================================================================
  // RENDER
  // =========================================================================

  return (
    <div className="dispatch-container">
      {/* HEADER */}
      <div className="dispatch-header">
        <h1>Dispatch &amp; Shipping</h1>
        <div className="dispatch-stats">
          <div className="stat-card">
            <div className="stat-value">{shipments.length}</div>
            <div className="stat-label">Total Shipments</div>
          </div>
          <div className="stat-card">
            <div className="stat-value">{carriers.filter(c => c.is_configured).length}</div>
            <div className="stat-label">Active Carriers</div>
          </div>
          <div className="stat-card">
            <div className="stat-value">{shipments.filter(s => s.status === 'label_generated').length}</div>
            <div className="stat-label">Labels Generated</div>
          </div>
          <div className="stat-card">
            <div className="stat-value">{manifestHistory.length}</div>
            <div className="stat-label">Manifests</div>
          </div>
        </div>
      </div>

      {/* TABS */}
      <div className="dispatch-tabs">
        <button className={`tab ${activeTab === 'shipments' ? 'active' : ''}`}  onClick={() => setActiveTab('shipments')}>Shipments</button>
        <button className={`tab ${activeTab === 'create'    ? 'active' : ''}`}  onClick={() => setActiveTab('create')}>Create Shipment</button>
        <button className={`tab ${activeTab === 'rates'     ? 'active' : ''}`}  onClick={() => setActiveTab('rates')}>Rate Shopping</button>
        <button className={`tab ${activeTab === 'manifest'  ? 'active' : ''}`}  onClick={() => setActiveTab('manifest')}>📋 Manifest</button>
      </div>

      {/* ── SHIPMENTS ─────────────────────────────────────────────────────── */}
      {activeTab === 'shipments' && (
        <div className="shipments-view">
          <div className="view-header">
            <h2>Recent Shipments</h2>
            <button onClick={loadShipments} className="refresh-btn">Refresh</button>
          </div>
          {loading ? (
            <div className="loading">Loading shipments…</div>
          ) : shipments.length === 0 ? (
            <div className="empty-state">
              <p>No shipments yet</p>
              <p className="empty-subtitle">Create your first shipment to get started</p>
            </div>
          ) : (
            <table className="shipments-table">
              <thead>
                <tr>
                  <th>Order ID</th><th>Carrier</th><th>Tracking Number</th>
                  <th>Status</th><th>Cost</th><th>Created</th><th>Actions</th>
                </tr>
              </thead>
              <tbody>
                {shipments.map(s => (
                  <tr key={s.shipment_id}>
                    <td>{(s.order_ids || []).join(', ')}</td>
                    <td>{carrierName(s.carrier_id)}</td>
                    <td><code className="tracking-number">{s.tracking_number}</code></td>
                    <td>
                      <span className={`status-badge ${getStatusColor(s.status)}`}>
                        {s.status.replace(/_/g, ' ')}
                      </span>
                    </td>
                    <td>{s.currency} {(s.cost || 0).toFixed(2)}</td>
                    <td>{new Date(s.created_at).toLocaleDateString()}</td>
                    <td>
                      {s.label_url && (
                        <a href={s.label_url} target="_blank" rel="noopener noreferrer" className="action-link">
                          Download Label
                        </a>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      )}

      {/* ── CREATE ────────────────────────────────────────────────────────── */}
      {activeTab === 'create' && (
        <div className="create-view">
          <div className="coming-soon">
            <h2>Create Shipment</h2>
            <p>This feature allows you to create shipping labels.</p>
            <p className="note">Use the Despatch Console for bulk shipment creation.</p>
          </div>
        </div>
      )}

      {/* ── RATE SHOPPING ─────────────────────────────────────────────────── */}
      {activeTab === 'rates' && (
        <div className="rates-view">
          <div className="rate-shopping-container">
            <h2>Rate Shopping</h2>
            <p className="subtitle">Compare shipping rates across carriers</p>
            <div className="carrier-selector">
              <label>Select Carrier:</label>
              <select value={selectedCarrier} onChange={e => setSelectedCarrier(e.target.value)}>
                <option value="">-- All Carriers --</option>
                {carriers.map(c => <option key={c.id} value={c.id}>{c.display_name}</option>)}
              </select>
              <button onClick={getRates} disabled={loading} className="get-rates-btn">
                {loading ? 'Loading…' : 'Get Rates'}
              </button>
            </div>
            {rates.length > 0 && (
              <div className="rates-results">
                <h3>Available Rates</h3>
                <div className="rates-grid">
                  {rates.map((r, i) => (
                    <div key={i} className="rate-card">
                      <div className="rate-header">
                        <span className="carrier-name">{carrierName(r.carrier)}</span>
                        <span className="rate-price">£{r.cost.amount.toFixed(2)}</span>
                      </div>
                      <div className="rate-service">{r.service_name}</div>
                      <div className="rate-delivery">Estimated: {r.estimated_days} day{r.estimated_days !== 1 ? 's' : ''}</div>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
        </div>
      )}

      {/* ── MANIFEST ──────────────────────────────────────────────────────── */}
      {activeTab === 'manifest' && (
        <div className="manifest-view">

          {/* Header */}
          <div className="manifest-header">
            <div className="manifest-header-left">
              <h2>End-of-Day Manifest</h2>
              <p className="manifest-subtitle">
                Close out despatch and generate carrier collection manifests.
                Royal Mail, DPD, Evri, and FedEx all require a manifest before end-of-day collection.
              </p>
            </div>
            <div className="manifest-header-right">
              <div className="manifest-date-picker">
                <label htmlFor="manifest-date">Manifest Date</label>
                <input
                  id="manifest-date"
                  type="date"
                  value={manifestDate}
                  max={new Date().toISOString().split('T')[0]}
                  onChange={e => setManifestDate(e.target.value)}
                />
              </div>
              <button
                className="manifest-all-btn"
                onClick={generateAllManifests}
                disabled={manifestGenerating !== null || todayShipments.length === 0}
              >
                {manifestGenerating === '__all__' ? '⏳ Generating…' : '📋 Close All & Manifest'}
              </button>
            </div>
          </div>

          {/* Feedback alerts */}
          {manifestError && (
            <div className="manifest-alert manifest-alert-error">
              ⚠️ {manifestError}
            </div>
          )}
          {manifestSuccess && (
            <div className="manifest-alert manifest-alert-success">
              ✅ {manifestSuccess}
            </div>
          )}

          {/* Per-carrier panels */}
          <div className="manifest-carriers-section">
            <h3 className="manifest-section-title">
              Despatch for{' '}
              <span className="manifest-date-badge">{manifestDate}</span>
            </h3>

            {manifestLoading ? (
              <div className="loading">Loading shipments…</div>
            ) : manifestCarrierIDs.length === 0 ? (
              <div className="manifest-empty">
                <div className="manifest-empty-icon">📦</div>
                <p>No shipments despatched on {manifestDate}</p>
                <p className="manifest-empty-sub">Select a different date or create shipments in the Despatch Console.</p>
              </div>
            ) : (
              <div className="manifest-carrier-cards">
                {manifestCarrierIDs.map(cid => {
                  const cShipments = shipmentsByCarrier[cid] || [];
                  const totalWeight = cShipments.reduce((sum, s) =>
                    sum + (s.parcels || []).reduce((pw, p) => pw + (p.weight || 0), 0), 0);
                  const totalCost = cShipments.reduce((sum, s) => sum + (s.cost || 0), 0);
                  const isGenerating = manifestGenerating === cid;

                  return (
                    <div key={cid} className={`manifest-carrier-card${cShipments.length === 0 ? ' manifest-carrier-card--empty' : ''}`}>
                      <div className="manifest-carrier-card-header">
                        <div className="manifest-carrier-info">
                          <div className="manifest-carrier-name">{carrierName(cid)}</div>
                          <div className="manifest-carrier-meta">
                            {cShipments.length > 0
                              ? `${cShipments.length} shipment${cShipments.length !== 1 ? 's' : ''} · ${totalWeight.toFixed(2)} kg · £${totalCost.toFixed(2)}`
                              : 'No shipments despatched today'}
                          </div>
                        </div>
                        <button
                          className="manifest-carrier-btn"
                          onClick={() => generateManifest(cid)}
                          disabled={isGenerating || manifestGenerating !== null || cShipments.length === 0}
                        >
                          {isGenerating ? '⏳ Generating…' : '📋 Close & Manifest'}
                        </button>
                      </div>

                      {cShipments.length > 0 && (
                        <div className="manifest-shipment-list">
                          <table className="manifest-shipments-table">
                            <thead>
                              <tr>
                                <th>Tracking</th>
                                <th>Service</th>
                                <th>Recipient</th>
                                <th>Weight</th>
                                <th>Cost</th>
                              </tr>
                            </thead>
                            <tbody>
                              {cShipments.slice(0, 10).map(s => (
                                <tr key={s.shipment_id}>
                                  <td><code className="tracking-number">{s.tracking_number}</code></td>
                                  <td>{s.service_name || s.service_code}</td>
                                  <td>{s.to_address?.name || '—'}</td>
                                  <td>{(s.parcels || []).reduce((w, p) => w + (p.weight || 0), 0).toFixed(2)} kg</td>
                                  <td>{s.currency || 'GBP'} {(s.cost || 0).toFixed(2)}</td>
                                </tr>
                              ))}
                              {cShipments.length > 10 && (
                                <tr>
                                  <td colSpan={5} className="manifest-more-rows">
                                    + {cShipments.length - 10} more shipments included in manifest
                                  </td>
                                </tr>
                              )}
                            </tbody>
                          </table>
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            )}
          </div>

          {/* Manifest History */}
          <div className="manifest-history-section">
            <div className="manifest-section-header">
              <h3 className="manifest-section-title">Manifest History</h3>
              <button className="refresh-btn" onClick={loadManifestHistory}>Refresh</button>
            </div>

            {manifestHistory.length === 0 ? (
              <div className="manifest-history-empty">No manifests generated yet.</div>
            ) : (
              <table className="shipments-table manifest-history-table">
                <thead>
                  <tr>
                    <th>Date</th>
                    <th>Carrier</th>
                    <th>Shipments</th>
                    <th>Total Weight</th>
                    <th>Total Cost</th>
                    <th>Format</th>
                    <th>Status</th>
                    <th>Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {manifestHistory.map(m => (
                    <tr key={m.manifest_id}>
                      <td>{m.manifest_date}</td>
                      <td>{m.carrier_name || m.carrier_id}</td>
                      <td>{m.shipment_count}</td>
                      <td>{(m.total_weight_kg || 0).toFixed(2)} kg</td>
                      <td>{m.currency || 'GBP'} {(m.total_cost || 0).toFixed(2)}</td>
                      <td>
                        <span className="manifest-format-badge">
                          {(m.document_format || 'csv').toUpperCase()}
                        </span>
                      </td>
                      <td>
                        <span className={`status-badge ${getManifestStatusColor(m.status)}`}>
                          {m.status}
                        </span>
                      </td>
                      <td>
                        {m.status === 'generated' && (
                          <button
                            className="action-link"
                            style={{ background: 'none', border: 'none', cursor: 'pointer', padding: 0 }}
                            onClick={() => downloadHistoricManifest(m.manifest_id)}
                          >
                            ⬇ Download
                          </button>
                        )}
                        {m.status === 'failed' && m.error_message && (
                          <span title={m.error_message} style={{ color: '#f87171', cursor: 'help' }}>⚠️ Error</span>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        </div>
      )}

      {/* CARRIERS SECTION */}
      <div className="carriers-section">
        <h2>Available Carriers</h2>
        <div className="carriers-grid">
          {carriers.map(carrier => (
            <div key={carrier.id} className="carrier-card">
              <div className="carrier-header">
                <h3>{carrier.display_name}</h3>
                <span className={`carrier-status ${carrier.is_configured ? 'active' : 'inactive'}`}>
                  {carrier.is_configured ? 'Connected' : 'Not Connected'}
                </span>
              </div>
              <div className="carrier-country">{carrier.country}</div>
              <div className="carrier-features">
                {(carrier.features || []).slice(0, 3).map(f => (
                  <span key={f} className="feature-badge">{f}</span>
                ))}
                {(carrier.features || []).length > 3 && (
                  <span className="feature-more">+{carrier.features.length - 3} more</span>
                )}
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
};

export default Dispatch;

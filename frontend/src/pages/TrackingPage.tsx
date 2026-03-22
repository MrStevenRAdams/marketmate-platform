import { useState, useEffect } from 'react';
import { useParams } from 'react-router-dom';
import './TrackingPage.css';

// ─── Config ────────────────────────────────────────────────────────────────────
const API_BASE = import.meta.env.VITE_API_URL ?? 'http://localhost:8080/api/v1';

// ─── Types ─────────────────────────────────────────────────────────────────────

interface TrackingEvent {
  timestamp: string;
  status: string;
  description: string;
  location?: string;
}

interface TrackingInfo {
  tracking_number: string;
  status: string;
  status_detail: string;
  events: TrackingEvent[];
  estimated_delivery?: string;
  actual_delivery?: string;
  signed_by?: string;
  location?: string;
}

interface ShipmentAddress {
  name: string;
  company?: string;
  address_line1: string;
  address_line2?: string;
  city: string;
  county?: string;
  postal_code: string;
  country: string;
}

interface Shipment {
  shipment_id: string;
  tracking_number: string;
  tracking_url?: string;
  carrier_id: string;
  service_name?: string;
  status: string;
  to_address: ShipmentAddress;
  estimated_delivery?: string;
  created_at: string;
  despatched_at?: string;
}

interface PublicSeller {
  name: string;
  logo_url: string;
  website?: string;
}

interface TrackingPageData {
  shipment: Shipment;
  tracking: TrackingInfo | null;
  seller: PublicSeller;
}

// ─── Status helpers ────────────────────────────────────────────────────────────

function statusIcon(status: string): string {
  const map: Record<string, string> = {
    pre_transit: '📦',
    in_transit: '🚚',
    out_for_delivery: '🏃',
    delivered: '✅',
    exception: '⚠️',
    returned: '↩️',
    cancelled: '✖️',
    unknown: '📍',
  };
  return map[status] ?? '📍';
}

function statusLabel(status: string): string {
  const map: Record<string, string> = {
    pre_transit: 'Label Created',
    in_transit: 'In Transit',
    out_for_delivery: 'Out for Delivery',
    delivered: 'Delivered',
    exception: 'Delivery Exception',
    returned: 'Returned to Sender',
    cancelled: 'Cancelled',
    unknown: 'Tracking Unavailable',
  };
  return map[status] ?? status.replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase());
}

function shipmentStatusLabel(status: string): string {
  const map: Record<string, string> = {
    planned: 'Label Created',
    label_generated: 'Label Generated',
    despatched: 'Despatched',
    delivered: 'Delivered',
    failed: 'Delivery Failed',
    returned: 'Returned',
    voided: 'Voided',
  };
  return map[status] ?? status;
}

function formatDate(dateStr: string | undefined, includeTime = false): string {
  if (!dateStr) return '—';
  try {
    const d = new Date(dateStr);
    if (isNaN(d.getTime())) return '—';
    if (includeTime) {
      return d.toLocaleDateString('en-GB', {
        day: 'numeric', month: 'short', year: 'numeric',
        hour: '2-digit', minute: '2-digit',
      });
    }
    return d.toLocaleDateString('en-GB', {
      weekday: 'long', day: 'numeric', month: 'long', year: 'numeric',
    });
  } catch {
    return '—';
  }
}

function carrierName(carrierId: string): string {
  const map: Record<string, string> = {
    royal_mail: 'Royal Mail',
    dpd: 'DPD',
    evri: 'Evri',
    ups: 'UPS',
    fedex: 'FedEx',
    dhl: 'DHL',
    parcelforce: 'Parcelforce',
    yodel: 'Yodel',
  };
  return map[carrierId] ?? carrierId.replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase());
}

// ─── Component ─────────────────────────────────────────────────────────────────

export default function TrackingPage() {
  const { tracking_number } = useParams<{ tracking_number: string }>();

  const [data, setData] = useState<TrackingPageData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!tracking_number) {
      setError('No tracking number provided.');
      setLoading(false);
      return;
    }

    // No auth headers — this is a fully public endpoint
    fetch(`${API_BASE}/public/track/${encodeURIComponent(tracking_number)}`)
      .then(async (res) => {
        if (!res.ok) {
          const body = await res.json().catch(() => ({}));
          throw new Error(body.error ?? `HTTP ${res.status}`);
        }
        return res.json() as Promise<TrackingPageData>;
      })
      .then(setData)
      .catch((err: Error) => setError(err.message))
      .finally(() => setLoading(false));
  }, [tracking_number]);

  // Resolve the active status: prefer live tracking status, fall back to shipment status
  const activeStatus = data?.tracking?.status ?? (data ? data.shipment.status : 'unknown');

  // Estimated delivery: prefer tracking info, then shipment field
  const estDelivery =
    data?.tracking?.estimated_delivery ||
    data?.tracking?.actual_delivery ||
    data?.shipment.estimated_delivery;

  // Resolve events sorted newest-first
  const events: TrackingEvent[] = data?.tracking?.events
    ? [...data.tracking.events].sort(
        (a, b) => new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime()
      )
    : [];

  const seller = data?.seller;
  const shipment = data?.shipment;

  return (
    <div className="tp-root">
      {/* ── Branded Header ─────────────────────────────────────────────────── */}
      <header className="tp-header">
        {seller?.logo_url ? (
          <img
            src={seller.logo_url}
            alt={seller.name ?? 'Seller logo'}
            className="tp-logo"
          />
        ) : (
          <div className="tp-logo-placeholder">
            <span className="ri-store-2-line" />
          </div>
        )}

        {seller?.name && (
          <span className="tp-seller-name">{seller.name}</span>
        )}

        <div className="tp-header-spacer" />
        <span className="tp-powered">Shipment tracking</span>
      </header>

      {/* ── Main Content ───────────────────────────────────────────────────── */}
      <main className="tp-main">
        {loading && (
          <div className="tp-state-wrap">
            <div className="tp-spinner" />
            <span className="tp-state-sub">Loading tracking information…</span>
          </div>
        )}

        {!loading && error && (
          <div className="tp-state-wrap">
            <div className="tp-state-icon">🔍</div>
            <div className="tp-state-title">Tracking not found</div>
            <div className="tp-state-sub">
              We couldn't find a shipment for tracking number{' '}
              <strong>{tracking_number}</strong>. Please check the number and
              try again, or contact the sender.
            </div>
          </div>
        )}

        {!loading && !error && data && (
          <>
            {/* Tracking number heading */}
            <div className="tp-tracking-heading">
              <div className="tp-tracking-label">Tracking number</div>
              <div className="tp-tracking-number">{shipment!.tracking_number}</div>
            </div>

            {/* Status hero */}
            <div className="tp-status-card">
              <div className={`tp-status-icon-wrap status-${activeStatus}`}>
                {statusIcon(activeStatus)}
              </div>
              <div className="tp-status-info">
                <div className="tp-status-label">Current status</div>
                <div className="tp-status-text">
                  {data.tracking
                    ? statusLabel(data.tracking.status)
                    : shipmentStatusLabel(shipment!.status)}
                </div>
                {data.tracking?.status_detail && (
                  <div className="tp-status-detail">{data.tracking.status_detail}</div>
                )}
              </div>
            </div>

            {/* Info grid */}
            <div className="tp-info-grid">
              {(shipment!.carrier_id || shipment!.service_name) && (
                <div className="tp-info-card">
                  <div className="tp-info-card-label">Carrier</div>
                  <div className="tp-info-card-value">
                    {carrierName(shipment!.carrier_id)}
                    {shipment!.service_name ? ` — ${shipment!.service_name}` : ''}
                  </div>
                </div>
              )}

              {estDelivery && (
                <div className="tp-info-card">
                  <div className="tp-info-card-label">
                    {activeStatus === 'delivered' ? 'Delivered on' : 'Estimated delivery'}
                  </div>
                  <div
                    className={`tp-info-card-value${activeStatus === 'delivered' ? ' highlight' : ''}`}
                  >
                    {formatDate(estDelivery)}
                  </div>
                </div>
              )}

              {shipment!.despatched_at && (
                <div className="tp-info-card">
                  <div className="tp-info-card-label">Despatched</div>
                  <div className="tp-info-card-value">
                    {formatDate(shipment!.despatched_at)}
                  </div>
                </div>
              )}

              {data.tracking?.signed_by && (
                <div className="tp-info-card">
                  <div className="tp-info-card-label">Signed by</div>
                  <div className="tp-info-card-value">{data.tracking.signed_by}</div>
                </div>
              )}
            </div>

            {/* Delivery address — show name, city and postcode only (privacy) */}
            <div className="tp-address-card">
              <div className="tp-address-card-title">Delivery address</div>
              <div className="tp-address-name">{shipment!.to_address.name}</div>
              <div className="tp-address-line">
                {[
                  shipment!.to_address.city,
                  shipment!.to_address.county,
                  shipment!.to_address.postal_code,
                ]
                  .filter(Boolean)
                  .join(', ')}
              </div>
              <div className="tp-address-line">{shipment!.to_address.country}</div>
            </div>

            {/* Timeline */}
            <div className="tp-timeline-section">
              <div className="tp-timeline-header">Tracking history</div>

              {events.length === 0 ? (
                <div className="tp-timeline-empty">
                  No tracking events available yet. Events will appear here once
                  the carrier starts scanning your parcel.
                </div>
              ) : (
                <div className="tp-timeline-list">
                  {events.map((ev, i) => (
                    <div className="tp-event" key={i}>
                      <div className="tp-event-dot-wrap">
                        <div className="tp-event-dot" />
                      </div>
                      <div className="tp-event-body">
                        <div className="tp-event-status">{ev.status}</div>
                        {ev.description && ev.description !== ev.status && (
                          <div className="tp-event-desc">{ev.description}</div>
                        )}
                        <div className="tp-event-meta">
                          <span className="tp-event-time">
                            {formatDate(ev.timestamp, true)}
                          </span>
                          {ev.location && (
                            <span className="tp-event-location">
                              <span className="ri-map-pin-2-line" />
                              {ev.location}
                            </span>
                          )}
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>

            {/* Carrier tracking link if available */}
            {shipment!.tracking_url && (
              <div className="tp-carrier-link-wrap">
                <a
                  href={shipment!.tracking_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="tp-carrier-link"
                >
                  <span className="ri-external-link-line" />
                  Track on {carrierName(shipment!.carrier_id)}'s website
                </a>
              </div>
            )}
          </>
        )}
      </main>

      {/* ── Footer ─────────────────────────────────────────────────────────── */}
      <footer className="tp-footer">
        {seller?.name && (
          <>
            {seller.website ? (
              <a href={seller.website} target="_blank" rel="noopener noreferrer">
                {seller.name}
              </a>
            ) : (
              <span>{seller.name}</span>
            )}
            {' · '}
          </>
        )}
        Powered by MarketMate
      </footer>
    </div>
  );
}

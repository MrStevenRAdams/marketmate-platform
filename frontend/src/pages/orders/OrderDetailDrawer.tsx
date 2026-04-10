// platform/frontend/src/pages/orders/OrderDetailDrawer.tsx
import { useState, useEffect, useRef, useCallback } from 'react';

// ============================================================================
// TYPES
// ============================================================================

interface TrackingEvent {
  date_time: string;
  description: string;
  tracking_point_id: string;
  location_lat?: number;
  location_lng?: number;
}

interface TrackingETA {
  display_string: string;
  from_date_time?: string;
  to_date_time?: string;
  type?: string;
}

interface TrackingSignature {
  image_base64: string;
  image_format: string;
  printed_name: string;
  signed_at: string;
}

interface SafePlacePhoto {
  image_base64: string;
  image_format: string;
  taken_at: string;
}

interface TrackingData {
  status: string;
  tracking_number: string;
  carrier: string;
  last_tracked_at?: string;
  events: TrackingEvent[];
  eta?: TrackingETA;
  signature?: TrackingSignature;
  safe_place_photo?: SafePlacePhoto;
}

interface Order {
  order_id: string;
  channel: string;
  reference?: string;
  status?: string;
  customer?: {
    name?: string;
    email?: string;
    phone?: string;
  };
  shipping_address?: {
    name?: string;
    address_line1?: string;
    address_line2?: string;
    city?: string;
    postal_code?: string;
    country?: string;
  };
  totals?: {
    grand_total?: { amount?: number; currency?: string };
  };
  lines?: OrderLine[];
  tracking_number?: string;
  carrier?: string;
  shipment_id?: string;
  created_at?: string;
  updated_at?: string;
  tags?: string[];
  notes?: string;
  internal_notes?: string;
  activity?: ActivityEntry[];
}

interface OrderLine {
  line_id: string;
  sku: string;
  title: string;
  quantity: number;
  price?: number;
  status?: string;
}

interface ActivityEntry {
  timestamp: string;
  action: string;
  actor?: string;
  detail?: string;
}

interface OrderDetailDrawerProps {
  order: Order;
  onClose: () => void;
  onOrderUpdate?: (updated: Order) => void;
}

// ============================================================================
// STATUS DISPLAY HELPERS
// ============================================================================

const TRACKING_STATUS_DISPLAY: Record<string, { label: string; colour: string; icon: string }> = {
  pre_transit:       { label: 'Label Created',      colour: 'bg-gray-100 text-gray-700',    icon: '🏷️' },
  in_transit:        { label: 'In Transit',          colour: 'bg-blue-100 text-blue-700',   icon: '🚚' },
  out_for_delivery:  { label: 'Out for Delivery',    colour: 'bg-indigo-100 text-indigo-700', icon: '🛵' },
  delivered:         { label: 'Delivered',            colour: 'bg-green-100 text-green-700', icon: '✅' },
  attempted_delivery:{ label: 'Attempted Delivery',  colour: 'bg-amber-100 text-amber-700', icon: '🔔' },
  exception:         { label: 'Exception',            colour: 'bg-red-100 text-red-700',     icon: '⚠️' },
  return_in_progress:{ label: 'Return in Progress',  colour: 'bg-purple-100 text-purple-700', icon: '↩️' },
  returned:          { label: 'Returned',             colour: 'bg-purple-100 text-purple-700', icon: '📦' },
};

const CARRIER_LOGOS: Record<string, string> = {
  evri:       'https://www.evri.com/assets/images/evri-logo.svg',
  royal_mail: 'https://upload.wikimedia.org/wikipedia/en/thumb/5/50/Royal_Mail.svg/120px-Royal_Mail.svg.png',
  dpd:        'https://www.dpd.co.uk/content/dam/dpd/images/logo/dpd-logo.svg',
  fedex:      'https://www.fedex.com/content/dam/fedex-com/logos/logo.png',
};

function formatDateTime(iso: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  return d.toLocaleString('en-GB', {
    weekday: 'short', day: 'numeric', month: 'short',
    hour: '2-digit', minute: '2-digit',
  });
}

function formatDate(iso: string): string {
  if (!iso) return '';
  return new Date(iso).toLocaleDateString('en-GB', {
    weekday: 'long', day: 'numeric', month: 'long',
  });
}

const TERMINAL_STATUSES = new Set(['delivered', 'returned', 'voided', 'cancelled', 'failed']);

// ============================================================================
// MAIN COMPONENT
// ============================================================================

export default function OrderDetailDrawer({ order, onClose, onOrderUpdate }: OrderDetailDrawerProps) {
  const [activeTab, setActiveTab] = useState<'details' | 'lines' | 'activity' | 'notes' | 'tracking'>('details');
  const [trackingData, setTrackingData] = useState<TrackingData | null>(null);
  const [trackingLoading, setTrackingLoading] = useState(false);
  const [trackingSyncing, setTrackingSyncing] = useState(false);
  const [lightbox, setLightbox] = useState<{ src: string; title: string } | null>(null);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const hasTracking = !!(order.tracking_number || order.shipment_id);
  const shipmentId = order.shipment_id || '';

  // ── Load tracking data ────────────────────────────────────────────────────

  const loadTracking = useCallback(async () => {
    if (!shipmentId) return;
    setTrackingLoading(true);
    try {
      const r = await fetch(`/api/v1/dispatch/shipments/${shipmentId}/tracking-detail`);
      if (r.ok) {
        const data = await r.json();
        setTrackingData(data);
      }
    } finally {
      setTrackingLoading(false);
    }
  }, [shipmentId]);

  // ── Auto-poll while tracking tab is open and status is not terminal ───────

  useEffect(() => {
    if (activeTab !== 'tracking' || !hasTracking) return;

    // Initial load
    loadTracking();

    // Set up 5-minute poll if not in a terminal state
    const isTerminal = trackingData ? TERMINAL_STATUSES.has(trackingData.status) : false;
    if (!isTerminal) {
      pollRef.current = setInterval(loadTracking, 5 * 60 * 1000);
    }
    return () => {
      if (pollRef.current) clearInterval(pollRef.current);
    };
  }, [activeTab, hasTracking, loadTracking]); // eslint-disable-line react-hooks/exhaustive-deps

  // Stop polling once delivered
  useEffect(() => {
    if (trackingData && TERMINAL_STATUSES.has(trackingData.status)) {
      if (pollRef.current) clearInterval(pollRef.current);
    }
  }, [trackingData]);

  // ── Manual sync ───────────────────────────────────────────────────────────

  const handleRefreshTracking = async () => {
    if (!shipmentId) return;
    setTrackingSyncing(true);
    try {
      const r = await fetch(`/api/v1/dispatch/shipments/${shipmentId}/sync-tracking`, {
        method: 'POST',
      });
      if (r.ok) {
        const data = await r.json();
        setTrackingData(data.tracking || null);
      }
    } finally {
      setTrackingSyncing(false);
    }
  };

  // ============================================================================
  // RENDER
  // ============================================================================

  const TABS = [
    { key: 'details',  label: 'Details' },
    { key: 'lines',    label: 'Lines' },
    { key: 'activity', label: 'Activity' },
    { key: 'notes',    label: 'Notes' },
    ...(hasTracking ? [{ key: 'tracking', label: 'Tracking' }] : []),
  ] as const;

  return (
    <>
      <div className="fixed inset-0 z-40 bg-black/30" onClick={onClose} />
      <div className="fixed right-0 top-0 bottom-0 z-50 w-full max-w-lg bg-white shadow-2xl flex flex-col">

        {/* Header */}
        <div className="flex items-start justify-between px-5 py-4 border-b border-gray-200">
          <div>
            <h2 className="font-semibold text-gray-900 text-base">
              {order.reference || order.order_id}
            </h2>
            <p className="text-xs text-gray-500 mt-0.5">
              {order.channel} · {order.customer?.name || ''}
            </p>
          </div>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600 text-xl p-1 leading-none">×</button>
        </div>

        {/* Tabs */}
        <div className="flex border-b border-gray-200 px-2 overflow-x-auto">
          {TABS.map(tab => (
            <button
              key={tab.key}
              onClick={() => setActiveTab(tab.key as typeof activeTab)}
              className={`px-4 py-3 text-sm font-medium whitespace-nowrap border-b-2 transition-colors ${
                activeTab === tab.key
                  ? 'border-blue-500 text-blue-600'
                  : 'border-transparent text-gray-500 hover:text-gray-700'
              }`}
            >
              {tab.label}
            </button>
          ))}
        </div>

        {/* Tab content */}
        <div className="flex-1 overflow-y-auto">
          {activeTab === 'details'  && <DetailsTab order={order} />}
          {activeTab === 'lines'    && <LinesTab lines={order.lines || []} />}
          {activeTab === 'activity' && <ActivityTab entries={order.activity || []} />}
          {activeTab === 'notes'    && <NotesTab order={order} onUpdate={onOrderUpdate} />}
          {activeTab === 'tracking' && (
            <TrackingTab
              shipmentId={shipmentId}
              trackingNumber={order.tracking_number || ''}
              carrier={order.carrier || ''}
              data={trackingData}
              loading={trackingLoading}
              syncing={trackingSyncing}
              onRefresh={handleRefreshTracking}
              onLightbox={setLightbox}
            />
          )}
        </div>
      </div>

      {/* Lightbox for signature / safe-place photo */}
      {lightbox && (
        <div
          className="fixed inset-0 z-60 bg-black/80 flex items-center justify-center p-4"
          onClick={() => setLightbox(null)}
        >
          <div className="bg-white rounded-xl max-w-md w-full p-4 shadow-2xl" onClick={e => e.stopPropagation()}>
            <div className="flex justify-between items-center mb-3">
              <h3 className="font-medium text-gray-900 text-sm">{lightbox.title}</h3>
              <button onClick={() => setLightbox(null)} className="text-gray-400 hover:text-gray-600">×</button>
            </div>
            <img
              src={lightbox.src}
              alt={lightbox.title}
              className="w-full rounded-lg border border-gray-100"
            />
          </div>
        </div>
      )}
    </>
  );
}

// ============================================================================
// TAB: DETAILS
// ============================================================================

function DetailsTab({ order }: { order: Order }) {
  const addr = order.shipping_address;
  return (
    <div className="p-5 space-y-5">
      {addr && (
        <Section title="Delivery Address">
          <p className="text-sm text-gray-700">
            {[addr.name, addr.address_line1, addr.address_line2, addr.city, addr.postal_code, addr.country]
              .filter(Boolean).join(', ')}
          </p>
        </Section>
      )}
      {order.totals?.grand_total && (
        <Section title="Order Total">
          <p className="text-sm font-medium text-gray-900">
            {order.totals.grand_total.currency} {order.totals.grand_total.amount?.toFixed(2)}
          </p>
        </Section>
      )}
      {order.tracking_number && (
        <Section title="Tracking">
          <p className="text-sm font-mono text-blue-700">{order.tracking_number}</p>
          {order.carrier && <p className="text-xs text-gray-500 mt-0.5">{order.carrier}</p>}
        </Section>
      )}
      {order.tags && order.tags.length > 0 && (
        <Section title="Tags">
          <div className="flex flex-wrap gap-1.5">
            {order.tags.map(t => (
              <span key={t} className="px-2 py-0.5 bg-gray-100 text-gray-600 rounded text-xs">{t}</span>
            ))}
          </div>
        </Section>
      )}
    </div>
  );
}

// ============================================================================
// TAB: LINES
// ============================================================================

function LinesTab({ lines }: { lines: OrderLine[] }) {
  return (
    <div className="p-5">
      {lines.length === 0 ? (
        <p className="text-sm text-gray-400 text-center py-8">No line items</p>
      ) : (
        <div className="space-y-3">
          {lines.map(line => (
            <div key={line.line_id} className="flex items-start gap-3 py-3 border-b border-gray-100 last:border-0">
              <div className="flex-1 min-w-0">
                <p className="text-sm font-medium text-gray-900 truncate">{line.title}</p>
                <p className="text-xs text-gray-500 mt-0.5">SKU: {line.sku}</p>
              </div>
              <div className="text-right shrink-0">
                <p className="text-sm font-medium text-gray-900">×{line.quantity}</p>
                {line.price != null && (
                  <p className="text-xs text-gray-500">£{line.price.toFixed(2)}</p>
                )}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

// ============================================================================
// TAB: ACTIVITY
// ============================================================================

function ActivityTab({ entries }: { entries: ActivityEntry[] }) {
  return (
    <div className="p-5">
      {entries.length === 0 ? (
        <p className="text-sm text-gray-400 text-center py-8">No activity recorded</p>
      ) : (
        <div className="space-y-3">
          {entries.map((e, i) => (
            <div key={i} className="flex gap-3">
              <div className="w-1.5 h-1.5 rounded-full bg-gray-300 mt-2 shrink-0" />
              <div>
                <p className="text-sm text-gray-700">{e.action}</p>
                {e.detail && <p className="text-xs text-gray-500 mt-0.5">{e.detail}</p>}
                <p className="text-xs text-gray-400 mt-0.5">{formatDateTime(e.timestamp)}{e.actor ? ` · ${e.actor}` : ''}</p>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

// ============================================================================
// TAB: NOTES
// ============================================================================

function NotesTab({ order, onUpdate }: { order: Order; onUpdate?: (o: Order) => void }) {
  const [note, setNote] = useState(order.internal_notes || '');
  const [saving, setSaving] = useState(false);

  const save = async () => {
    setSaving(true);
    try {
      await fetch(`/api/v1/orders/${order.order_id}/notes`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ internal_notes: note }),
      });
      if (onUpdate) onUpdate({ ...order, internal_notes: note });
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="p-5 space-y-4">
      {order.notes && (
        <Section title="Channel Notes">
          <p className="text-sm text-gray-600 whitespace-pre-wrap">{order.notes}</p>
        </Section>
      )}
      <Section title="Internal Notes (warehouse only)">
        <textarea
          value={note}
          onChange={e => setNote(e.target.value)}
          className="w-full h-32 border border-gray-200 rounded-lg px-3 py-2 text-sm resize-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
          placeholder="Add internal notes visible to warehouse staff only…"
        />
        <button
          onClick={save}
          disabled={saving}
          className="mt-2 px-4 py-2 bg-blue-600 text-white text-sm rounded-lg disabled:opacity-50 hover:bg-blue-700"
        >
          {saving ? 'Saving…' : 'Save Notes'}
        </button>
      </Section>
    </div>
  );
}

// ============================================================================
// TAB: TRACKING
// ============================================================================

interface TrackingTabProps {
  shipmentId: string;
  trackingNumber: string;
  carrier: string;
  data: TrackingData | null;
  loading: boolean;
  syncing: boolean;
  onRefresh: () => void;
  onLightbox: (l: { src: string; title: string }) => void;
}

function TrackingTab({
  trackingNumber, carrier, data, loading, syncing, onRefresh, onLightbox,
}: TrackingTabProps) {
  const statusInfo = data ? (TRACKING_STATUS_DISPLAY[data.status] || TRACKING_STATUS_DISPLAY['in_transit']) : null;
  const carrierLogo = CARRIER_LOGOS[carrier?.toLowerCase() || ''];

  if (loading && !data) {
    return (
      <div className="p-5 flex justify-center">
        <div className="animate-spin w-6 h-6 border-2 border-blue-500 border-t-transparent rounded-full" />
      </div>
    );
  }

  if (!data) {
    return (
      <div className="p-5 text-center py-12">
        <p className="text-sm text-gray-400">No tracking data available yet.</p>
        <button onClick={onRefresh} className="mt-3 text-sm text-blue-600 hover:underline">
          Fetch tracking
        </button>
      </div>
    );
  }

  return (
    <div className="p-5 space-y-5">

      {/* Status badge + carrier */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          {carrierLogo && (
            <img src={carrierLogo} alt={carrier} className="h-6 object-contain" onError={e => { (e.target as HTMLImageElement).style.display = 'none'; }} />
          )}
          {statusInfo && (
            <span className={`inline-flex items-center gap-1.5 px-3 py-1.5 rounded-full text-sm font-semibold ${statusInfo.colour}`}>
              {statusInfo.icon} {statusInfo.label}
            </span>
          )}
        </div>
        <button
          onClick={onRefresh}
          disabled={syncing}
          className="flex items-center gap-1.5 text-xs text-blue-600 hover:text-blue-700 disabled:opacity-50"
        >
          <span className={syncing ? 'animate-spin' : ''}>↻</span>
          {syncing ? 'Refreshing…' : 'Refresh'}
        </button>
      </div>

      {/* Tracking number */}
      <div className="bg-gray-50 rounded-lg px-4 py-3">
        <p className="text-xs text-gray-500 mb-0.5">Tracking Number</p>
        <p className="font-mono text-sm font-medium text-gray-900">{trackingNumber}</p>
        {data.last_tracked_at && (
          <p className="text-xs text-gray-400 mt-1">Last updated {formatDateTime(data.last_tracked_at)}</p>
        )}
      </div>

      {/* ETA */}
      {data.eta?.display_string && (
        <div className="bg-blue-50 border border-blue-100 rounded-lg px-4 py-3">
          <p className="text-xs text-blue-500 font-medium mb-0.5">Expected Delivery</p>
          <p className="text-sm font-semibold text-blue-900">{data.eta.display_string}</p>
          {data.eta.from_date_time && data.eta.to_date_time && (
            <p className="text-xs text-blue-600 mt-0.5">
              {formatDate(data.eta.from_date_time)}&nbsp;
              {new Date(data.eta.from_date_time).toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit' })}–
              {new Date(data.eta.to_date_time).toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit' })}
            </p>
          )}
        </div>
      )}

      {/* Timeline */}
      {data.events && data.events.length > 0 && (
        <Section title="Tracking Timeline">
          <div className="space-y-0">
            {[...data.events]
              .sort((a, b) => new Date(b.date_time).getTime() - new Date(a.date_time).getTime())
              .map((ev, i) => (
                <TrackingEventRow key={i} event={ev} isLatest={i === 0} />
              ))}
          </div>
        </Section>
      )}

      {/* Proof of delivery */}
      {data.status === 'delivered' && (
        <Section title="Proof of Delivery">
          {data.signature ? (
            <div className="space-y-2">
              <div className="flex items-center gap-2 text-sm text-green-700">
                <span>✓</span>
                <span>Signed for by: <strong>{data.signature.printed_name || 'Recipient'}</strong></span>
              </div>
              <button
                onClick={() => onLightbox({
                  src: `data:image/${data.signature!.image_format || 'png'};base64,${data.signature!.image_base64}`,
                  title: 'Delivery Signature',
                })}
                className="text-xs text-blue-600 hover:underline"
              >
                View Signature
              </button>
            </div>
          ) : data.safe_place_photo ? (
            <div className="space-y-2">
              <p className="text-sm text-gray-700">📷 Delivered to safe place</p>
              <button
                onClick={() => onLightbox({
                  src: `data:image/${data.safe_place_photo!.image_format || 'jpeg'};base64,${data.safe_place_photo!.image_base64}`,
                  title: 'Safe Place Photo',
                })}
                className="text-xs text-blue-600 hover:underline"
              >
                View Photo
              </button>
            </div>
          ) : (
            <p className="text-sm text-gray-500">Delivered — no signature required for this service.</p>
          )}
        </Section>
      )}
    </div>
  );
}

function TrackingEventRow({ event, isLatest }: { event: TrackingEvent; isLatest: boolean }) {
  const d = new Date(event.date_time);
  const dateStr = d.toLocaleDateString('en-GB', { weekday: 'short', day: 'numeric', month: 'short' });
  const timeStr = d.toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit' });

  return (
    <div className={`flex gap-3 py-3 border-b border-gray-100 last:border-0 ${isLatest ? 'opacity-100' : 'opacity-70'}`}>
      <div className="flex flex-col items-center shrink-0 mt-1">
        <div className={`w-2 h-2 rounded-full ${isLatest ? 'bg-blue-500' : 'bg-gray-300'}`} />
        <div className="w-px flex-1 bg-gray-200 mt-1" />
      </div>
      <div className="flex-1 pb-1">
        <p className={`text-sm ${isLatest ? 'font-medium text-gray-900' : 'text-gray-700'}`}>
          {event.description}
        </p>
        <p className="text-xs text-gray-400 mt-0.5">
          {dateStr} · {timeStr}
        </p>
      </div>
    </div>
  );
}

// ============================================================================
// SHARED SECTION WRAPPER
// ============================================================================

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div>
      <h3 className="text-xs font-semibold text-gray-400 uppercase tracking-wide mb-2">{title}</h3>
      {children}
    </div>
  );
}

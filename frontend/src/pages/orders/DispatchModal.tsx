// platform/frontend/src/pages/orders/DispatchModal.tsx
import { useState, useEffect, useCallback, useRef } from 'react';

// ============================================================================
// TYPES
// ============================================================================

interface Order {
  order_id: string;
  channel: string;
  reference?: string;
  customer?: { name?: string; email?: string; phone?: string; mobile?: string };
  shipping_address?: {
    name?: string;
    address_line1?: string;
    city?: string;
    postal_code?: string;
    country?: string;
  };
  lines?: OrderLine[];
}

interface OrderLine {
  line_id: string;
  sku: string;
  title: string;
  quantity: number;
  weight_kg?: number;
  length_cm?: number;
  width_cm?: number;
  height_cm?: number;
  default_carrier?: string;
  default_service?: string;
  labels_per_shipment?: number;
  include_return_label?: boolean;
}

interface Carrier {
  id: string;
  display_name: string;
  is_active: boolean;
}

interface CarrierService {
  code: string;
  name: string;
  description?: string;
  domestic?: boolean;
  international?: boolean;
  estimated_days?: number;
}

interface ShippingTemplate {
  id: string;
  name: string;
  layout: string;
  include_return_label: boolean;
}

interface CoverageWarning {
  code: string;
  severity: 'info' | 'warn' | 'error';
  title: string;
  description: string;
  suggestion?: string;
}

interface DispatchModalProps {
  order: Order;
  onClose: () => void;
  onSuccess: (shipmentId: string, trackingNumber: string, labelCopies: string[]) => void;
}

// ============================================================================
// COVERAGE REFERENCE TABLE DATA (hardcoded reference guide, not live API data)
// ============================================================================

const COVERAGE_TABLE = [
  {
    zone: 'UK Standard',
    evri: { supported: true, note: 'Full service' },
    royalMail: { supported: true, note: 'Full service' },
    dpd: { supported: true, note: 'Full service' },
    fedex: { supported: true, note: 'Full service' },
  },
  {
    zone: 'Scottish Highlands',
    evri: { supported: true, note: '~£5–10 surcharge' },
    royalMail: { supported: true, note: 'Standard rate' },
    dpd: { supported: true, note: 'Surcharge applies' },
    fedex: { supported: true, note: 'Remote area fee' },
  },
  {
    zone: 'Shetland / Orkney',
    evri: { supported: 'limited', note: 'Limited service' },
    royalMail: { supported: true, note: 'Standard rate' },
    dpd: { supported: true, note: 'Surcharge applies' },
    fedex: { supported: true, note: 'Remote area fee' },
  },
  {
    zone: 'Channel Islands',
    evri: { supported: true, note: 'CN22/CN23 required' },
    royalMail: { supported: true, note: 'CN22/CN23 required' },
    dpd: { supported: true, note: 'Customs required' },
    fedex: { supported: true, note: 'Customs required' },
  },
  {
    zone: 'Isle of Man',
    evri: { supported: true, note: 'Outside UK VAT' },
    royalMail: { supported: true, note: 'Outside UK VAT' },
    dpd: { supported: true, note: 'Outside UK VAT' },
    fedex: { supported: true, note: 'Outside UK VAT' },
  },
  {
    zone: 'BFPO',
    evri: { supported: false, note: 'Not supported' },
    royalMail: { supported: true, note: 'Full service' },
    dpd: { supported: false, note: 'Not supported' },
    fedex: { supported: false, note: 'Not supported' },
  },
  {
    zone: 'EU',
    evri: { supported: true, note: 'IOSS required <€150' },
    royalMail: { supported: true, note: 'IOSS required <€150' },
    dpd: { supported: true, note: 'IOSS required <€150' },
    fedex: { supported: true, note: 'Full service' },
  },
  {
    zone: 'USA',
    evri: { supported: true, note: 'Via GECO' },
    royalMail: { supported: true, note: 'Full service' },
    dpd: { supported: true, note: 'Full service' },
    fedex: { supported: true, note: 'Full service' },
  },
  {
    zone: 'China',
    evri: { supported: true, note: 'Via GECO' },
    royalMail: { supported: true, note: 'Full service' },
    dpd: { supported: 'limited', note: 'Check availability' },
    fedex: { supported: true, note: 'Full service' },
  },
  {
    zone: 'Brazil',
    evri: { supported: true, note: 'Via GECO' },
    royalMail: { supported: true, note: 'Full service' },
    dpd: { supported: false, note: 'Not supported' },
    fedex: { supported: true, note: 'Full service' },
  },
];

// ============================================================================
// API HELPERS
// ============================================================================

const apiBase = '/api/v1';

async function apiFetch(path: string, options?: RequestInit) {
  const r = await fetch(`${apiBase}${path}`, {
    headers: { 'Content-Type': 'application/json' },
    ...options,
  });
  const data = await r.json();
  if (!r.ok) throw new Error(data.error || `Request failed (${r.status})`);
  return data;
}

// ============================================================================
// MAIN COMPONENT
// ============================================================================

export default function DispatchModal({ order, onClose, onSuccess }: DispatchModalProps) {
  // Carrier + service
  const [carriers, setCarriers] = useState<Carrier[]>([]);
  const [services, setServices] = useState<CarrierService[]>([]);
  const [carrierId, setCarrierId] = useState('');
  const [serviceCode, setServiceCode] = useState('');
  const [loadingServices, setLoadingServices] = useState(false);

  // Shipping templates (for combined return label option)
  const [dualTemplates, setDualTemplates] = useState<ShippingTemplate[]>([]);

  // Extras popover
  const [extrasOpen, setExtrasOpen] = useState(false);
  const extrasRef = useRef<HTMLDivElement>(null);

  // Extras state
  const [proofOfDelivery, setProofOfDelivery] = useState<'none' | 'signature' | 'household'>('none');
  const [includeReturn, setIncludeReturn] = useState(false);
  const [returnPrint, setReturnPrint] = useState<'separate' | 'combined'>('separate');
  const [returnTemplateId, setReturnTemplateId] = useState('');
  const [labelCount, setLabelCount] = useState(1);
  const [smsNotify, setSmsNotify] = useState(false);
  const [emailNotify, setEmailNotify] = useState(false);
  const [collectionEnabled, setCollectionEnabled] = useState(false);
  const [collectionDate, setCollectionDate] = useState('');
  const [declaredValue, setDeclaredValue] = useState('');

  // Coverage
  const [coverageWarnings, setCoverageWarnings] = useState<CoverageWarning[]>([]);
  const [coverageRefOpen, setCoverageRefOpen] = useState(false);

  // Dispatch state
  const [dispatching, setDispatching] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Derived values from order
  const toAddress = order.shipping_address || {};
  const customerMobile = order.customer?.mobile || order.customer?.phone || '';
  const customerEmail = order.customer?.email || '';
  const defaultCarrier = order.lines?.[0]?.default_carrier || '';
  const defaultService = order.lines?.[0]?.default_service || '';
  const defaultLabelCount = order.lines?.[0]?.labels_per_shipment || 1;
  const defaultIncludeReturn = order.lines?.[0]?.include_return_label || false;

  // ── Load carriers on mount ────────────────────────────────────────────────

  useEffect(() => {
    apiFetch('/dispatch/carriers/configured')
      .then(d => {
        const active = (d.carriers || []).filter((c: Carrier) => c.is_active);
        setCarriers(active);
        const initial = defaultCarrier || (active[0]?.id ?? '');
        setCarrierId(initial);
      })
      .catch(() => {});
  }, [defaultCarrier]);

  // Pre-fill notification checkboxes
  useEffect(() => {
    if (customerMobile) setSmsNotify(true);
    if (customerEmail) setEmailNotify(true);
    setLabelCount(defaultLabelCount);
    setIncludeReturn(defaultIncludeReturn);
  }, [customerMobile, customerEmail, defaultLabelCount, defaultIncludeReturn]);

  // ── Load services when carrier changes ───────────────────────────────────

  useEffect(() => {
    if (!carrierId) return;
    setLoadingServices(true);
    setServices([]);
    setServiceCode('');
    apiFetch(`/dispatch/carriers/${carrierId}/services`)
      .then(d => {
        setServices(d.services || []);
        const initial = defaultService || (d.services?.[0]?.code ?? '');
        setServiceCode(initial);
      })
      .catch(() => {})
      .finally(() => setLoadingServices(false));
  }, [carrierId, defaultService]);

  // ── Load dual-layout templates for combined return ────────────────────────

  useEffect(() => {
    apiFetch('/dispatch/shipping-templates')
      .then(d => {
        const dual = (d.templates || []).filter(
          (t: ShippingTemplate) => t.include_return_label && t.layout === 'a4_dual'
        );
        setDualTemplates(dual);
      })
      .catch(() => {});
  }, []);

  // ── Pre-flight coverage check when carrier/service/address changes ────────

  const runCoverageCheck = useCallback(() => {
    if (carrierId !== 'evri') {
      setCoverageWarnings([]);
      return;
    }
    const country = toAddress.country || 'GB';
    const postcode = toAddress.postal_code || '';
    // Call coverage check endpoint
    apiFetch(`/dispatch/address-validate`, {
      method: 'POST',
      body: JSON.stringify({
        carrier_id: carrierId,
        address: {
          country,
          postal_code: postcode,
          address_line1: toAddress.address_line1 || '',
          city: toAddress.city || '',
        },
      }),
    })
      .then(d => setCoverageWarnings(d.coverage_warnings || []))
      .catch(() => setCoverageWarnings([]));
  }, [carrierId, toAddress]);

  useEffect(() => {
    runCoverageCheck();
  }, [runCoverageCheck]);

  // ── Close extras on outside click ─────────────────────────────────────────

  useEffect(() => {
    if (!extrasOpen) return;
    const handler = (e: MouseEvent) => {
      if (extrasRef.current && !extrasRef.current.contains(e.target as Node)) {
        setExtrasOpen(false);
      }
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, [extrasOpen]);

  // ── Dispatch ──────────────────────────────────────────────────────────────

  const handleDispatch = async () => {
    if (!carrierId || !serviceCode) return;
    setDispatching(true);
    setError(null);

    const options: Record<string, unknown> = {
      signature: proofOfDelivery === 'signature',
      notifications: {
        sms: smsNotify && customerMobile ? [customerMobile] : [],
        email: emailNotify && customerEmail ? [customerEmail] : [],
      },
    };
    if (proofOfDelivery === 'household') options['extra'] = { household_signature: true };

    const body: Record<string, unknown> = {
      order_id: order.order_id,
      carrier_id: carrierId,
      service_code: serviceCode,
      to_address: {
        name: toAddress.name || order.customer?.name || '',
        address_line1: toAddress.address_line1 || '',
        city: toAddress.city || '',
        postal_code: toAddress.postal_code || '',
        country: toAddress.country || 'GB',
        phone: customerMobile,
        email: customerEmail,
      },
      options,
      label_count: labelCount,
      include_return_label: includeReturn,
      reference: order.reference || order.order_id,
    };

    if (collectionEnabled && collectionDate) {
      body['collection_date'] = collectionDate;
    }
    if (declaredValue) {
      body['declared_value_gbp'] = parseFloat(declaredValue);
    }

    try {
      const result = await apiFetch('/dispatch/shipments', {
        method: 'POST',
        body: JSON.stringify(body),
      });

      // If backend returns coverage_warnings on a 200, surface them but continue
      if (result.coverage_warnings?.length) {
        setCoverageWarnings(result.coverage_warnings);
      }

      const labelCopies: string[] = result.label_copies || [];
      if (result.return_label_base64) {
        if (returnPrint === 'separate') {
          labelCopies.push(result.return_label_base64);
        }
        // combined is handled by the template — already merged in the PDF
      }

      onSuccess(result.shipment_id, result.tracking_number, labelCopies);
    } catch (e: unknown) {
      if (e instanceof Error) {
        // Parse structured coverage_error response
        try {
          const parsed = JSON.parse(e.message);
          if (parsed.error === 'coverage_error') {
            setError(`${parsed.carrier_message} — ${parsed.suggestion}`);
          } else {
            setError(e.message);
          }
        } catch {
          setError(e.message);
        }
      } else {
        setError('Dispatch failed. Please try again.');
      }
    } finally {
      setDispatching(false);
    }
  };

  // ── Dispatch button label ─────────────────────────────────────────────────

  const dispatchLabel = dispatching
    ? 'Dispatching…'
    : labelCount > 1
    ? `Dispatch & Print ${labelCount} Labels`
    : 'Dispatch & Print Label';

  const canDispatch = !!carrierId && !!serviceCode && !dispatching;

  // ============================================================================
  // RENDER
  // ============================================================================

  return (
    <div className="fixed inset-0 z-50 flex items-end sm:items-center justify-center bg-black/50 p-4">
      <div className="bg-white rounded-2xl shadow-2xl w-full max-w-xl max-h-[92vh] flex flex-col overflow-hidden">

        {/* Header */}
        <div className="flex items-center justify-between px-5 py-4 border-b border-gray-200">
          <div>
            <h2 className="font-semibold text-gray-900">Dispatch Order</h2>
            <p className="text-xs text-gray-500 mt-0.5">
              {order.reference || order.order_id} · {toAddress.name || order.customer?.name || ''}
            </p>
          </div>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600 text-xl leading-none p-1">×</button>
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto p-5 space-y-4">

          {/* ── Carrier row ────────────────────────────────────────────────── */}
          <div>
            <label className="block text-xs font-medium text-gray-500 uppercase tracking-wide mb-1.5">Carrier</label>
            <select
              value={carrierId}
              onChange={e => setCarrierId(e.target.value)}
              className="dispatch-select"
            >
              {carriers.length === 0 && <option value="">No carriers configured</option>}
              {carriers.map(c => (
                <option key={c.id} value={c.id}>{c.display_name}</option>
              ))}
            </select>
          </div>

          {/* ── Service + Extras row ─────────────────────────────────────── */}
          <div>
            <label className="block text-xs font-medium text-gray-500 uppercase tracking-wide mb-1.5">Service</label>
            <div className="flex gap-2">
              <select
                value={serviceCode}
                onChange={e => setServiceCode(e.target.value)}
                disabled={loadingServices || services.length === 0}
                className="dispatch-select flex-1"
              >
                {loadingServices && <option>Loading…</option>}
                {!loadingServices && services.length === 0 && <option value="">No services available</option>}
                {services.map(s => (
                  <option key={s.code} value={s.code}>
                    {s.name}{s.estimated_days ? ` (${s.estimated_days}d)` : ''}
                  </option>
                ))}
              </select>

              {/* Extras popover button */}
              <div className="relative" ref={extrasRef}>
                <button
                  onClick={() => setExtrasOpen(v => !v)}
                  className={`px-3 py-2 rounded-lg border text-sm font-medium transition-colors ${
                    extrasOpen
                      ? 'border-blue-500 bg-blue-50 text-blue-700'
                      : 'border-gray-300 text-gray-600 hover:border-gray-400 hover:bg-gray-50'
                  }`}
                  title="Extras"
                >
                  ⚙ Extras
                </button>

                {extrasOpen && (
                  <ExtrasPopover
                    proofOfDelivery={proofOfDelivery}
                    onProofChange={setProofOfDelivery}
                    includeReturn={includeReturn}
                    onIncludeReturnChange={setIncludeReturn}
                    returnPrint={returnPrint}
                    onReturnPrintChange={setReturnPrint}
                    dualTemplates={dualTemplates}
                    returnTemplateId={returnTemplateId}
                    onReturnTemplateChange={setReturnTemplateId}
                    labelCount={labelCount}
                    defaultLabelCount={defaultLabelCount}
                    onLabelCountChange={setLabelCount}
                    smsNotify={smsNotify}
                    onSmsChange={setSmsNotify}
                    customerMobile={customerMobile}
                    emailNotify={emailNotify}
                    onEmailChange={setEmailNotify}
                    customerEmail={customerEmail}
                    collectionEnabled={collectionEnabled}
                    onCollectionChange={setCollectionEnabled}
                    collectionDate={collectionDate}
                    onCollectionDateChange={setCollectionDate}
                    declaredValue={declaredValue}
                    onDeclaredValueChange={setDeclaredValue}
                  />
                )}
              </div>
            </div>
          </div>

          {/* ── Coverage warnings ────────────────────────────────────────── */}
          {coverageWarnings.map((w, i) => (
            <CoverageWarningBanner key={i} warning={w} />
          ))}

          {/* ── Error ───────────────────────────────────────────────────── */}
          {error && (
            <div className="px-4 py-3 bg-red-50 border border-red-200 rounded-lg text-sm text-red-700">
              {error}
            </div>
          )}

          {/* ── Coverage reference ──────────────────────────────────────── */}
          <div className="border border-gray-200 rounded-lg overflow-hidden">
            <button
              onClick={() => setCoverageRefOpen(v => !v)}
              className="w-full flex items-center justify-between px-4 py-2.5 text-sm text-gray-600 hover:bg-gray-50 transition-colors"
            >
              <span>📋 Coverage reference</span>
              <span className="text-xs text-gray-400">{coverageRefOpen ? '▲ Hide' : '▼ Show'}</span>
            </button>
            {coverageRefOpen && <CoverageReferenceTable />}
          </div>

        </div>

        {/* Footer */}
        <div className="flex items-center justify-between px-5 py-4 border-t border-gray-200 bg-gray-50 gap-3">
          <button
            onClick={onClose}
            className="px-4 py-2 text-sm text-gray-600 hover:text-gray-900 rounded-lg hover:bg-gray-100"
          >
            Cancel
          </button>
          <button
            onClick={handleDispatch}
            disabled={!canDispatch}
            className="flex-1 sm:flex-none px-6 py-2.5 bg-blue-600 text-white text-sm font-medium rounded-lg disabled:opacity-50 hover:bg-blue-700 transition-colors"
          >
            {dispatchLabel}
          </button>
        </div>
      </div>

      <style>{`
        .dispatch-select {
          width: 100%;
          border: 1px solid #d1d5db;
          border-radius: 0.5rem;
          padding: 0.5rem 0.75rem;
          font-size: 0.875rem;
          background: white;
          outline: none;
        }
        .dispatch-select:focus { border-color: #3b82f6; box-shadow: 0 0 0 3px rgba(59,130,246,0.1); }
      `}</style>
    </div>
  );
}

// ============================================================================
// EXTRAS POPOVER
// ============================================================================

interface ExtrasPopoverProps {
  proofOfDelivery: 'none' | 'signature' | 'household';
  onProofChange: (v: 'none' | 'signature' | 'household') => void;
  includeReturn: boolean;
  onIncludeReturnChange: (v: boolean) => void;
  returnPrint: 'separate' | 'combined';
  onReturnPrintChange: (v: 'separate' | 'combined') => void;
  dualTemplates: ShippingTemplate[];
  returnTemplateId: string;
  onReturnTemplateChange: (v: string) => void;
  labelCount: number;
  defaultLabelCount: number;
  onLabelCountChange: (v: number) => void;
  smsNotify: boolean;
  onSmsChange: (v: boolean) => void;
  customerMobile: string;
  emailNotify: boolean;
  onEmailChange: (v: boolean) => void;
  customerEmail: string;
  collectionEnabled: boolean;
  onCollectionChange: (v: boolean) => void;
  collectionDate: string;
  onCollectionDateChange: (v: string) => void;
  declaredValue: string;
  onDeclaredValueChange: (v: string) => void;
}

function ExtrasPopover(props: ExtrasPopoverProps) {
  const {
    proofOfDelivery, onProofChange,
    includeReturn, onIncludeReturnChange,
    returnPrint, onReturnPrintChange,
    dualTemplates, returnTemplateId, onReturnTemplateChange,
    labelCount, defaultLabelCount, onLabelCountChange,
    smsNotify, onSmsChange, customerMobile,
    emailNotify, onEmailChange, customerEmail,
    collectionEnabled, onCollectionChange,
    collectionDate, onCollectionDateChange,
    declaredValue, onDeclaredValueChange,
  } = props;

  return (
    <div className="absolute right-0 top-full mt-2 w-80 bg-white border border-gray-200 rounded-xl shadow-xl z-50 p-4 space-y-4">
      <h3 className="text-xs font-semibold text-gray-500 uppercase tracking-wide">Dispatch Extras</h3>

      {/* Proof of delivery */}
      <div>
        <p className="text-xs font-medium text-gray-700 mb-1.5">
          Proof of Delivery
          <span className="ml-1 text-gray-400 font-normal" title="Signature and Household Signature cannot be combined">ⓘ</span>
        </p>
        <div className="flex gap-2 flex-wrap">
          {(['none', 'signature', 'household'] as const).map(opt => (
            <button
              key={opt}
              onClick={() => onProofChange(opt)}
              className={`px-3 py-1.5 rounded-lg text-xs font-medium border transition-colors ${
                proofOfDelivery === opt
                  ? 'border-blue-500 bg-blue-50 text-blue-700'
                  : 'border-gray-200 text-gray-600 hover:border-gray-300'
              }`}
            >
              {opt === 'none' ? 'None' : opt === 'signature' ? 'Signature' : 'Household Sig.'}
            </button>
          ))}
        </div>
        {proofOfDelivery !== 'none' && (
          <p className="text-xs text-gray-400 mt-1">Signature and Household Signature are mutually exclusive.</p>
        )}
      </div>

      {/* Return label */}
      <div>
        <label className="flex items-center gap-2 cursor-pointer">
          <input
            type="checkbox"
            checked={includeReturn}
            onChange={e => onIncludeReturnChange(e.target.checked)}
            className="rounded border-gray-300 text-blue-600"
          />
          <span className="text-xs font-medium text-gray-700">Include return label in shipment</span>
        </label>
        {includeReturn && (
          <div className="mt-2 pl-5 space-y-2">
            <label className="flex items-center gap-2 cursor-pointer">
              <input
                type="radio"
                checked={returnPrint === 'separate'}
                onChange={() => onReturnPrintChange('separate')}
                className="text-blue-600"
              />
              <span className="text-xs text-gray-600">Print separately (two files)</span>
            </label>
            <label className="flex items-center gap-2 cursor-pointer">
              <input
                type="radio"
                checked={returnPrint === 'combined'}
                onChange={() => onReturnPrintChange('combined')}
                className="text-blue-600"
              />
              <span className="text-xs text-gray-600">Combined on one sheet (A4 Dual template)</span>
            </label>
            {returnPrint === 'combined' && (
              <select
                value={returnTemplateId}
                onChange={e => onReturnTemplateChange(e.target.value)}
                className="w-full text-xs border border-gray-200 rounded-lg px-2 py-1.5 mt-1"
              >
                <option value="">Select A4 Dual template…</option>
                {dualTemplates.map(t => (
                  <option key={t.id} value={t.id}>{t.name}</option>
                ))}
                {dualTemplates.length === 0 && (
                  <option disabled>No A4 Dual templates — create one in Shipping Templates</option>
                )}
              </select>
            )}
          </div>
        )}
      </div>

      {/* Number of labels */}
      <div>
        <label className="block text-xs font-medium text-gray-700 mb-1">
          Number of Labels
        </label>
        <div className="flex items-center gap-2">
          <input
            type="number"
            min={1}
            max={20}
            value={labelCount}
            onChange={e => onLabelCountChange(Math.max(1, Math.min(20, parseInt(e.target.value) || 1)))}
            className="w-20 text-xs border border-gray-200 rounded-lg px-2 py-1.5 text-center"
          />
          <span className="text-xs text-gray-400">
            Default: {defaultLabelCount} from product settings
          </span>
        </div>
        <p className="text-xs text-gray-400 mt-1">For multi-box shipments. Each label gets a unique Evri barcode.</p>
      </div>

      {/* SMS notification */}
      {customerMobile && (
        <div>
          <label className="flex items-start gap-2 cursor-pointer">
            <input
              type="checkbox"
              checked={smsNotify}
              onChange={e => onSmsChange(e.target.checked)}
              className="mt-0.5 rounded border-gray-300 text-blue-600"
            />
            <span>
              <span className="text-xs font-medium text-gray-700">SMS notification</span>
              <span className="block text-xs text-gray-400">{customerMobile}</span>
            </span>
          </label>
        </div>
      )}

      {/* Email notification */}
      {customerEmail && (
        <div>
          <label className="flex items-start gap-2 cursor-pointer">
            <input
              type="checkbox"
              checked={emailNotify}
              onChange={e => onEmailChange(e.target.checked)}
              className="mt-0.5 rounded border-gray-300 text-blue-600"
            />
            <span>
              <span className="text-xs font-medium text-gray-700">Email notification</span>
              <span className="block text-xs text-gray-400 truncate max-w-[200px]">{customerEmail}</span>
            </span>
          </label>
        </div>
      )}

      {/* Collection toggle */}
      <div>
        <label className="flex items-center gap-2 cursor-pointer">
          <input
            type="checkbox"
            checked={collectionEnabled}
            onChange={e => onCollectionChange(e.target.checked)}
            className="rounded border-gray-300 text-blue-600"
          />
          <span className="text-xs font-medium text-gray-700">Schedule collection</span>
        </label>
        {collectionEnabled && (
          <input
            type="date"
            value={collectionDate}
            min={new Date().toISOString().split('T')[0]}
            onChange={e => onCollectionDateChange(e.target.value)}
            className="mt-2 w-full text-xs border border-gray-200 rounded-lg px-2 py-1.5"
          />
        )}
      </div>

      {/* Declared value */}
      <div>
        <label className="block text-xs font-medium text-gray-700 mb-1">
          Declared Value (£) <span className="font-normal text-gray-400">— optional</span>
        </label>
        <div className="relative">
          <span className="absolute left-2 top-1/2 -translate-y-1/2 text-xs text-gray-400">£</span>
          <input
            type="number"
            min={0}
            step={0.01}
            value={declaredValue}
            onChange={e => onDeclaredValueChange(e.target.value)}
            placeholder="0.00"
            className="w-full pl-5 text-xs border border-gray-200 rounded-lg px-2 py-1.5"
          />
        </div>
        <p className="text-xs text-gray-400 mt-1">Evri standard compensation applies unless set.</p>
      </div>
    </div>
  );
}

// ============================================================================
// COVERAGE WARNING BANNER
// ============================================================================

function CoverageWarningBanner({ warning }: { warning: CoverageWarning }) {
  const colours = {
    info: 'bg-blue-50 border-blue-200 text-blue-800',
    warn: 'bg-amber-50 border-amber-200 text-amber-800',
    error: 'bg-red-50 border-red-200 text-red-800',
  };
  const icons = { info: 'ℹ️', warn: '⚠️', error: '🚫' };
  return (
    <div className={`px-4 py-3 rounded-lg border text-sm ${colours[warning.severity]}`}>
      <div className="flex items-start gap-2">
        <span>{icons[warning.severity]}</span>
        <div>
          <p className="font-medium">{warning.title}</p>
          <p className="text-xs mt-0.5 opacity-90">{warning.description}</p>
          {warning.suggestion && (
            <p className="text-xs mt-0.5 opacity-75 italic">{warning.suggestion}</p>
          )}
        </div>
      </div>
    </div>
  );
}

// ============================================================================
// COVERAGE REFERENCE TABLE
// ============================================================================

function CoverageReferenceTable() {
  return (
    <div className="overflow-x-auto border-t border-gray-100">
      <table className="w-full text-xs">
        <thead>
          <tr className="bg-gray-50 border-b border-gray-200">
            <th className="text-left px-3 py-2 font-medium text-gray-600 w-32">Zone</th>
            <th className="px-3 py-2 font-medium text-gray-600 text-center">Evri</th>
            <th className="px-3 py-2 font-medium text-gray-600 text-center">Royal Mail</th>
            <th className="px-3 py-2 font-medium text-gray-600 text-center">DPD</th>
            <th className="px-3 py-2 font-medium text-gray-600 text-center">FedEx</th>
          </tr>
        </thead>
        <tbody>
          {COVERAGE_TABLE.map((row, i) => (
            <tr key={i} className={i % 2 === 0 ? 'bg-white' : 'bg-gray-50'}>
              <td className="px-3 py-2 font-medium text-gray-700">{row.zone}</td>
              {[row.evri, row.royalMail, row.dpd, row.fedex].map((cell, j) => (
                <td key={j} className="px-3 py-2 text-center">
                  <div className={`inline-flex flex-col items-center gap-0.5`}>
                    <span>
                      {cell.supported === true ? '✅' : cell.supported === false ? '❌' : '⚠️'}
                    </span>
                    <span className="text-gray-400 leading-tight" style={{ fontSize: '10px' }}>
                      {cell.note}
                    </span>
                  </div>
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
      <p className="px-3 py-2 text-gray-400 border-t border-gray-100" style={{ fontSize: '10px' }}>
        ✅ Supported · ⚠️ Limited / surcharge applies · ❌ Not supported. Reference guide only — verify with carrier for current pricing.
      </p>
    </div>
  );
}

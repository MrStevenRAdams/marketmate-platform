// platform/frontend/src/pages/product-create-components/ProductShippingTab.tsx
//
// ── HOW TO WIRE INTO ProductCreate.tsx ────────────────────────────────────────
//
// 1. Import this component:
//      import ProductShippingTab from './product-create-components/ProductShippingTab';
//
// 2. Add a "Shipping & Customs" tab to the existing tab array:
//      { key: 'shipping', label: 'Shipping & Customs' }
//
// 3. Render the tab content alongside your existing tabs:
//      {activeTab === 'shipping' && (
//        <ProductShippingTab
//          value={shippingFields}
//          onChange={setShippingFields}
//        />
//      )}
//
// 4. Add shippingFields to your product save payload:
//      shipping_template:      shippingFields.shippingTemplate,
//      labels_per_shipment:    shippingFields.labelsPerShipment,
//      default_carrier:        shippingFields.defaultCarrier,
//      default_service:        shippingFields.defaultService,
//      include_return_label:   shippingFields.includeReturnLabel,
//      customs_profile_id:     shippingFields.customsProfileId,
//
// 5. Initialise shippingFields from the loaded product on edit:
//      const [shippingFields, setShippingFields] = useState<ShippingFields>({
//        shippingTemplate:    product.shipping_template    || '',
//        labelsPerShipment:   product.labels_per_shipment  || 1,
//        defaultCarrier:      product.default_carrier      || '',
//        defaultService:      product.default_service      || '',
//        includeReturnLabel:  product.include_return_label || false,
//        customsProfileId:    product.customs_profile_id   || '',
//      });
// ─────────────────────────────────────────────────────────────────────────────

import { useState, useEffect, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';

// ============================================================================
// TYPES
// ============================================================================

export interface ShippingFields {
  shippingTemplate: string;
  labelsPerShipment: number;
  defaultCarrier: string;
  defaultService: string;
  includeReturnLabel: boolean;
  customsProfileId: string;
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
}

interface ShippingTemplate {
  id: string;
  name: string;
  layout: string;
}

interface CustomsProfile {
  id: string;
  name: string;
  commodity_code: string;
  duty_paid: string;
}

interface ProductShippingTabProps {
  value: ShippingFields;
  onChange: (fields: ShippingFields) => void;
  /** Whether the product has any international shipping destinations */
  hasInternationalDestinations?: boolean;
}

// ============================================================================
// COMPONENT
// ============================================================================

export default function ProductShippingTab({
  value,
  onChange,
  hasInternationalDestinations = false,
}: ProductShippingTabProps) {
  const navigate = useNavigate();

  const [carriers, setCarriers] = useState<Carrier[]>([]);
  const [services, setServices] = useState<CarrierService[]>([]);
  const [templates, setTemplates] = useState<ShippingTemplate[]>([]);
  const [customsProfiles, setCustomsProfiles] = useState<CustomsProfile[]>([]);
  const [loadingServices, setLoadingServices] = useState(false);

  const update = useCallback(
    (patch: Partial<ShippingFields>) => onChange({ ...value, ...patch }),
    [value, onChange]
  );

  // ── Load carriers on mount ──────────────────────────────────────────────

  useEffect(() => {
    fetch('/api/v1/dispatch/carriers/configured')
      .then(r => r.ok ? r.json() : { carriers: [] })
      .then(d => setCarriers((d.carriers || []).filter((c: Carrier) => c.is_active)))
      .catch(() => {});
  }, []);

  // ── Load services when carrier changes ──────────────────────────────────

  useEffect(() => {
    if (!value.defaultCarrier) {
      setServices([]);
      return;
    }
    setLoadingServices(true);
    fetch(`/api/v1/dispatch/carriers/${value.defaultCarrier}/services`)
      .then(r => r.ok ? r.json() : { services: [] })
      .then(d => setServices(d.services || []))
      .catch(() => setServices([]))
      .finally(() => setLoadingServices(false));
  }, [value.defaultCarrier]);

  // ── Load shipping templates ─────────────────────────────────────────────

  useEffect(() => {
    fetch('/api/v1/dispatch/shipping-templates')
      .then(r => r.ok ? r.json() : { templates: [] })
      .then(d => setTemplates(d.templates || []))
      .catch(() => {});
  }, []);

  // ── Load customs profiles ───────────────────────────────────────────────

  useEffect(() => {
    fetch('/api/v1/dispatch/customs-profiles')
      .then(r => r.ok ? r.json() : { profiles: [] })
      .then(d => setCustomsProfiles(d.profiles || []))
      .catch(() => {});
  }, []);

  // ============================================================================
  // RENDER
  // ============================================================================

  return (
    <div className="space-y-6 max-w-2xl">

      {/* ── Labels per Shipment ──────────────────────────────────────────── */}
      <FormField
        label="Labels per Shipment"
        hint="For multi-box shipments (e.g. flat-pack furniture), set the number of labels generated per order. Each parcel gets a unique barcode."
      >
        <div className="flex items-center gap-3">
          <input
            type="number"
            min={1}
            max={20}
            value={value.labelsPerShipment}
            onChange={e => update({ labelsPerShipment: Math.max(1, Math.min(20, parseInt(e.target.value) || 1)) })}
            className="product-input w-24 text-center"
          />
          <span className="text-sm text-gray-500">
            {value.labelsPerShipment === 1 ? 'Single parcel shipment' : `${value.labelsPerShipment} parcels per order`}
          </span>
        </div>
      </FormField>

      {/* ── Default Carrier ──────────────────────────────────────────────── */}
      <FormField
        label="Default Carrier"
        hint="Pre-selects this carrier on the dispatch form for this product. Leave blank to use the tenant default."
      >
        <select
          value={value.defaultCarrier}
          onChange={e => update({ defaultCarrier: e.target.value, defaultService: '' })}
          className="product-input"
        >
          <option value="">— Use tenant default —</option>
          {carriers.map(c => (
            <option key={c.id} value={c.id}>{c.display_name}</option>
          ))}
          {carriers.length === 0 && (
            <option disabled>No carriers configured — add one in Settings → Carriers</option>
          )}
        </select>
      </FormField>

      {/* ── Default Service ──────────────────────────────────────────────── */}
      {value.defaultCarrier && (
        <FormField
          label="Default Service"
          hint="Pre-selects this service when the carrier above is chosen."
        >
          <select
            value={value.defaultService}
            onChange={e => update({ defaultService: e.target.value })}
            disabled={loadingServices}
            className="product-input"
          >
            <option value="">— No preference —</option>
            {loadingServices && <option disabled>Loading services…</option>}
            {services.map(s => (
              <option key={s.code} value={s.code}>{s.name}</option>
            ))}
          </select>
        </FormField>
      )}

      {/* ── Include Return Label ─────────────────────────────────────────── */}
      <FormField label="Return Label">
        <label className="flex items-start gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={value.includeReturnLabel}
            onChange={e => update({ includeReturnLabel: e.target.checked })}
            className="mt-0.5 rounded border-gray-300 text-blue-600"
          />
          <span>
            <span className="text-sm font-medium text-gray-700">
              Include return label by default
            </span>
            <span className="block text-xs text-gray-500 mt-0.5">
              When enabled, every dispatch for this product generates both an outbound and a
              return label simultaneously. Can be overridden in the dispatch form.
            </span>
          </span>
        </label>
      </FormField>

      {/* ── Shipping Template ────────────────────────────────────────────── */}
      <FormField
        label="Shipping Template"
        hint="Controls label layout — A4 single, A4 dual (with return), thermal 6×4, or packing slip."
      >
        <div className="flex gap-2">
          <select
            value={value.shippingTemplate}
            onChange={e => update({ shippingTemplate: e.target.value })}
            className="product-input flex-1"
          >
            <option value="">— Use tenant default —</option>
            {templates.map(t => (
              <option key={t.id} value={t.id}>
                {t.name} ({t.layout.replace(/_/g, ' ')})
              </option>
            ))}
          </select>
          <button
            type="button"
            onClick={() => navigate('/dispatch/shipping-templates')}
            className="px-3 py-2 border border-gray-300 rounded-lg text-sm text-gray-600 hover:bg-gray-50 whitespace-nowrap"
            title="Create a new shipping template"
          >
            + New
          </button>
        </div>
        {templates.length === 0 && (
          <p className="text-xs text-gray-400 mt-1">
            No templates yet.{' '}
            <button
              type="button"
              onClick={() => navigate('/dispatch/shipping-templates')}
              className="text-blue-600 hover:underline"
            >
              Create one
            </button>
          </p>
        )}
      </FormField>

      {/* ── Customs Profile (only shown for international products) ──────── */}
      {hasInternationalDestinations && (
        <FormField
          label="Customs Profile"
          hint="Reusable customs declaration — HS code, country of manufacture, duty terms — for international shipments."
        >
          <div className="flex gap-2">
            <select
              value={value.customsProfileId}
              onChange={e => update({ customsProfileId: e.target.value })}
              className="product-input flex-1"
            >
              <option value="">— None —</option>
              {customsProfiles.map(p => (
                <option key={p.id} value={p.id}>
                  {p.name} (HS {p.commodity_code} · {p.duty_paid})
                </option>
              ))}
            </select>
            <button
              type="button"
              onClick={() => navigate('/dispatch/customs-profiles')}
              className="px-3 py-2 border border-gray-300 rounded-lg text-sm text-gray-600 hover:bg-gray-50 whitespace-nowrap"
              title="Create a new customs profile"
            >
              + New
            </button>
          </div>
          {customsProfiles.length === 0 && (
            <p className="text-xs text-gray-400 mt-1">
              No customs profiles yet.{' '}
              <button
                type="button"
                onClick={() => navigate('/dispatch/customs-profiles')}
                className="text-blue-600 hover:underline"
              >
                Create one
              </button>
            </p>
          )}
        </FormField>
      )}

      {/* ── Customs profile hint when international not yet enabled ──────── */}
      {!hasInternationalDestinations && (
        <div className="bg-gray-50 border border-gray-200 rounded-lg px-4 py-3 text-sm text-gray-500">
          🌍 <strong>International shipping?</strong> Add international destination countries to this
          product to enable the Customs Profile field.
        </div>
      )}

      <style>{`
        .product-input {
          width: 100%;
          border: 1px solid #d1d5db;
          border-radius: 0.5rem;
          padding: 0.5rem 0.75rem;
          font-size: 0.875rem;
          background: white;
          outline: none;
          transition: border-color 0.15s, box-shadow 0.15s;
        }
        .product-input:focus {
          border-color: #3b82f6;
          box-shadow: 0 0 0 3px rgba(59,130,246,0.1);
        }
        select.product-input { appearance: auto; }
      `}</style>
    </div>
  );
}

// ============================================================================
// FIELD WRAPPER
// ============================================================================

function FormField({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: React.ReactNode;
}) {
  return (
    <div>
      <label className="block text-sm font-medium text-gray-700 mb-1.5">{label}</label>
      {children}
      {hint && <p className="text-xs text-gray-500 mt-1.5">{hint}</p>}
    </div>
  );
}

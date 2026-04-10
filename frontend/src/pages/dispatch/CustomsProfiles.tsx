// platform/frontend/src/pages/dispatch/CustomsProfiles.tsx
import { useState, useEffect, useCallback } from 'react';

interface CustomsProfile {
  id: string;
  name: string;
  commodity_code: string;
  country_of_manufacture: string;
  description: string;
  duty_paid: 'DDP' | 'DDU';
  typical_value_gbp: number;
  typical_weight_kg: number;
  ioss_applicable: boolean;
  requires_eori: boolean;
  requires_vat_number: boolean;
  requires_cpc_code: boolean;
  cpc_code?: string;
  created_at?: string;
  updated_at?: string;
}

const EMPTY_PROFILE: Partial<CustomsProfile> = {
  name: '',
  commodity_code: '',
  country_of_manufacture: 'CN',
  description: '',
  duty_paid: 'DDU',
  typical_value_gbp: 0,
  typical_weight_kg: 0,
  ioss_applicable: false,
  requires_eori: false,
  requires_vat_number: false,
  requires_cpc_code: false,
  cpc_code: '',
};

const API_BASE = '/api/v1/dispatch/customs-profiles';

function api(path: string, method = 'GET', body?: unknown) {
  return fetch(`${API_BASE}${path}`, {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: body ? JSON.stringify(body) : undefined,
  }).then(async r => {
    const data = await r.json();
    if (!r.ok) throw new Error(data.error || 'Request failed');
    return data;
  });
}

// Common countries used for customs
const COUNTRY_OPTIONS = [
  { code: 'CN', name: 'China' },
  { code: 'GB', name: 'United Kingdom' },
  { code: 'US', name: 'United States' },
  { code: 'DE', name: 'Germany' },
  { code: 'IN', name: 'India' },
  { code: 'VN', name: 'Vietnam' },
  { code: 'BD', name: 'Bangladesh' },
  { code: 'TR', name: 'Turkey' },
  { code: 'IT', name: 'Italy' },
  { code: 'PK', name: 'Pakistan' },
  { code: 'ID', name: 'Indonesia' },
  { code: 'TH', name: 'Thailand' },
  { code: 'MY', name: 'Malaysia' },
  { code: 'KR', name: 'South Korea' },
  { code: 'JP', name: 'Japan' },
  { code: 'TW', name: 'Taiwan' },
  { code: 'PL', name: 'Poland' },
  { code: 'FR', name: 'France' },
  { code: 'ES', name: 'Spain' },
  { code: 'PT', name: 'Portugal' },
];

export default function CustomsProfiles() {
  const [profiles, setProfiles] = useState<CustomsProfile[]>([]);
  const [loading, setLoading] = useState(true);
  const [editing, setEditing] = useState<Partial<CustomsProfile> | null>(null);
  const [isNew, setIsNew] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await api('');
      setProfiles(data.profiles || []);
    } catch (e) {
      setError('Failed to load customs profiles');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { load(); }, [load]);

  const openNew = () => {
    setEditing({ ...EMPTY_PROFILE });
    setIsNew(true);
    setError(null);
  };

  const openEdit = (p: CustomsProfile) => {
    setEditing({ ...p });
    setIsNew(false);
    setError(null);
  };

  const handleSave = async () => {
    if (!editing) return;
    setSaving(true);
    setError(null);
    try {
      if (isNew) {
        await api('', 'POST', editing);
      } else {
        await api(`/${editing.id}`, 'PUT', editing);
      }
      setEditing(null);
      load();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Save failed');
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async (id: string, name: string) => {
    if (!confirm(`Delete customs profile "${name}"? This cannot be undone.`)) return;
    try {
      await api(`/${id}`, 'DELETE');
      load();
    } catch {
      setError('Failed to delete profile');
    }
  };

  const update = (patch: Partial<CustomsProfile>) => {
    setEditing(prev => prev ? { ...prev, ...patch } : prev);
  };

  if (loading) {
    return (
      <div className="p-6">
        <div className="animate-pulse space-y-3">
          {[1, 2, 3].map(i => <div key={i} className="h-20 bg-gray-100 rounded-lg" />)}
        </div>
      </div>
    );
  }

  return (
    <div className="p-6 max-w-5xl mx-auto">
      {/* Header */}
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-xl font-semibold text-gray-900">Customs Profiles</h1>
          <p className="text-sm text-gray-500 mt-0.5">
            Reusable customs declarations for international shipments
          </p>
        </div>
        <button
          onClick={openNew}
          className="flex items-center gap-2 px-4 py-2 bg-blue-600 text-white rounded-lg text-sm font-medium hover:bg-blue-700"
        >
          + New Profile
        </button>
      </div>

      {error && !editing && (
        <div className="mb-4 px-4 py-3 bg-red-50 border border-red-200 rounded-lg text-sm text-red-700">
          {error}
        </div>
      )}

      {/* Info callout */}
      <div className="mb-5 px-4 py-3 bg-blue-50 border border-blue-200 rounded-lg text-sm text-blue-800">
        <strong>What is a customs profile?</strong> A reusable set of customs declaration fields
        — HS commodity code, country of manufacture, duty terms — attached to a product for
        international shipments. Saves re-entering the same data on every despatch.
      </div>

      {/* Profile list */}
      {profiles.length === 0 ? (
        <div className="text-center py-16 text-gray-400 border-2 border-dashed border-gray-200 rounded-xl">
          <div className="text-4xl mb-3">🌍</div>
          <p className="text-sm font-medium text-gray-500 mb-1">No customs profiles yet</p>
          <p className="text-xs text-gray-400 mb-4">
            Create profiles for products you ship internationally to pre-fill customs forms.
          </p>
          <button onClick={openNew} className="text-sm text-blue-600 hover:underline">
            Create your first customs profile
          </button>
        </div>
      ) : (
        <div className="space-y-3">
          {profiles.map(p => (
            <ProfileCard
              key={p.id}
              profile={p}
              onEdit={() => openEdit(p)}
              onDelete={() => handleDelete(p.id, p.name)}
            />
          ))}
        </div>
      )}

      {/* Editor modal */}
      {editing && (
        <ProfileEditor
          profile={editing}
          isNew={isNew}
          saving={saving}
          error={error}
          onChange={update}
          onSave={handleSave}
          onClose={() => { setEditing(null); setError(null); }}
        />
      )}
    </div>
  );
}

// ============================================================================
// PROFILE CARD
// ============================================================================

function ProfileCard({
  profile, onEdit, onDelete,
}: {
  profile: CustomsProfile;
  onEdit: () => void;
  onDelete: () => void;
}) {
  const dutyColour = profile.duty_paid === 'DDP'
    ? 'bg-green-50 text-green-700 border-green-200'
    : 'bg-amber-50 text-amber-700 border-amber-200';

  return (
    <div className="flex items-start justify-between bg-white border border-gray-200 rounded-xl px-5 py-4 hover:border-blue-300 transition-colors">
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2 mb-1">
          <h3 className="font-medium text-gray-900 text-sm truncate">{profile.name}</h3>
          <span className={`text-xs px-2 py-0.5 rounded border font-medium ${dutyColour}`}>
            {profile.duty_paid}
          </span>
          {profile.ioss_applicable && (
            <span className="text-xs px-2 py-0.5 rounded border bg-purple-50 text-purple-700 border-purple-200">
              IOSS
            </span>
          )}
        </div>
        <div className="flex items-center gap-4 text-xs text-gray-500">
          <span>HS: <span className="font-mono font-medium text-gray-700">{profile.commodity_code || '—'}</span></span>
          <span>Origin: <span className="font-medium text-gray-700">{profile.country_of_manufacture}</span></span>
          {profile.typical_value_gbp > 0 && (
            <span>Typical value: <span className="font-medium text-gray-700">£{profile.typical_value_gbp.toFixed(2)}</span></span>
          )}
          {profile.typical_weight_kg > 0 && (
            <span>{profile.typical_weight_kg} kg</span>
          )}
        </div>
        {profile.description && (
          <p className="text-xs text-gray-400 mt-1 truncate">{profile.description}</p>
        )}
      </div>
      <div className="flex items-center gap-1 ml-4 shrink-0">
        <button
          onClick={onEdit}
          className="p-1.5 text-gray-400 hover:text-blue-500 rounded hover:bg-blue-50 transition-colors"
          title="Edit"
        >
          ✏️
        </button>
        <button
          onClick={onDelete}
          className="p-1.5 text-gray-400 hover:text-red-500 rounded hover:bg-red-50 transition-colors"
          title="Delete"
        >
          🗑
        </button>
      </div>
    </div>
  );
}

// ============================================================================
// PROFILE EDITOR MODAL
// ============================================================================

function ProfileEditor({
  profile, isNew, saving, error, onChange, onSave, onClose,
}: {
  profile: Partial<CustomsProfile>;
  isNew: boolean;
  saving: boolean;
  error: string | null;
  onChange: (patch: Partial<CustomsProfile>) => void;
  onSave: () => void;
  onClose: () => void;
}) {
  const isValid = !!(profile.name && profile.commodity_code && profile.country_of_manufacture && profile.duty_paid);

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4">
      <div className="bg-white rounded-xl shadow-2xl w-full max-w-2xl max-h-[92vh] flex flex-col overflow-hidden">
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-gray-200">
          <h2 className="font-semibold text-gray-900">
            {isNew ? 'New Customs Profile' : 'Edit Customs Profile'}
          </h2>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600 text-xl leading-none">×</button>
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto p-6 space-y-5">
          {error && (
            <div className="px-4 py-3 bg-red-50 border border-red-200 rounded-lg text-sm text-red-700">
              {error}
            </div>
          )}

          {/* Profile Name */}
          <Field label="Profile Name" required hint='Give this a clear name, e.g. "Electronics — China"'>
            <input
              type="text"
              value={profile.name || ''}
              onChange={e => onChange({ name: e.target.value })}
              className="form-input"
              placeholder="e.g. Electronics — China"
            />
          </Field>

          {/* HS Commodity Code */}
          <Field
            label="HS Commodity Code"
            required
            hint={
              <span>
                6–10 digit Harmonised System tariff code.{' '}
                <a
                  href="https://www.trade-tariff.service.gov.uk/find_commodity"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="text-blue-600 hover:underline"
                >
                  Look up on UK Trade Tariff ↗
                </a>
              </span>
            }
          >
            <input
              type="text"
              value={profile.commodity_code || ''}
              onChange={e => onChange({ commodity_code: e.target.value.replace(/\D/g, '') })}
              className="form-input font-mono"
              placeholder="e.g. 847130"
              maxLength={10}
            />
          </Field>

          {/* Description */}
          <Field label="Customs Description" hint="Plain-language description shown on customs forms (CN22/CN23)">
            <input
              type="text"
              value={profile.description || ''}
              onChange={e => onChange({ description: e.target.value })}
              className="form-input"
              placeholder="e.g. Electronic components — mobile phone parts"
            />
          </Field>

          {/* Country of Manufacture + Duty Paid — side by side */}
          <div className="grid grid-cols-2 gap-4">
            <Field label="Country of Manufacture" required>
              <select
                value={profile.country_of_manufacture || 'CN'}
                onChange={e => onChange({ country_of_manufacture: e.target.value })}
                className="form-input"
              >
                {COUNTRY_OPTIONS.map(c => (
                  <option key={c.code} value={c.code}>{c.code} — {c.name}</option>
                ))}
              </select>
            </Field>

            <Field label="Duty Terms" required>
              <select
                value={profile.duty_paid || 'DDU'}
                onChange={e => onChange({ duty_paid: e.target.value as 'DDP' | 'DDU' })}
                className="form-input"
              >
                <option value="DDU">DDU — Delivered Duty Unpaid</option>
                <option value="DDP">DDP — Delivered Duty Paid</option>
              </select>
              <p className="text-xs text-gray-500 mt-1">
                {profile.duty_paid === 'DDP'
                  ? '✅ You pay import duties. Customer receives parcel without a customs bill. Not available to all destinations.'
                  : '⚠️ Customer pays import duties on arrival. Risk of parcel being held at customs if recipient does not pay.'}
              </p>
            </Field>
          </div>

          {/* Typical value + weight */}
          <div className="grid grid-cols-2 gap-4">
            <Field label="Typical Declared Value (GBP)" hint="Used as a default — can be overridden per shipment">
              <div className="relative">
                <span className="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400 text-sm">£</span>
                <input
                  type="number"
                  min="0"
                  step="0.01"
                  value={profile.typical_value_gbp || ''}
                  onChange={e => onChange({ typical_value_gbp: parseFloat(e.target.value) || 0 })}
                  className="form-input pl-7"
                  placeholder="0.00"
                />
              </div>
            </Field>
            <Field label="Typical Weight (kg)">
              <input
                type="number"
                min="0"
                step="0.001"
                value={profile.typical_weight_kg || ''}
                onChange={e => onChange({ typical_weight_kg: parseFloat(e.target.value) || 0 })}
                className="form-input"
                placeholder="0.000"
              />
            </Field>
          </div>

          {/* Checkboxes */}
          <div>
            <p className="text-sm font-medium text-gray-700 mb-2">Additional Requirements</p>
            <div className="space-y-2">
              <CheckboxField
                checked={profile.ioss_applicable || false}
                onChange={v => onChange({ ioss_applicable: v })}
                label="IOSS applicable"
                hint="EU shipments under €150. Your IOSS number is sent to Evri when dispatching."
              />
              <CheckboxField
                checked={profile.requires_eori || false}
                onChange={v => onChange({ requires_eori: v })}
                label="EORI number required"
                hint="Economic Operators Registration and Identification number for commercial exports."
              />
              <CheckboxField
                checked={profile.requires_vat_number || false}
                onChange={v => onChange({ requires_vat_number: v })}
                label="VAT number required"
                hint="Destination country VAT registration may be needed for some routes."
              />
              <CheckboxField
                checked={profile.requires_cpc_code || false}
                onChange={v => onChange({ requires_cpc_code: v })}
                label="CPC code required"
                hint="Customs Procedure Code — required for certain goods entering specific customs regimes."
              />
            </div>
          </div>

          {/* CPC Code field (conditional) */}
          {profile.requires_cpc_code && (
            <Field label="CPC Code" hint="e.g. 10 00 001">
              <input
                type="text"
                value={profile.cpc_code || ''}
                onChange={e => onChange({ cpc_code: e.target.value })}
                className="form-input font-mono"
                placeholder="e.g. 10 00 001"
              />
            </Field>
          )}
        </div>

        {/* Footer */}
        <div className="flex justify-end gap-3 px-6 py-4 border-t border-gray-200 bg-gray-50">
          <button
            onClick={onClose}
            className="px-4 py-2 text-sm text-gray-600 hover:text-gray-900 rounded-lg hover:bg-gray-100"
          >
            Cancel
          </button>
          <button
            onClick={onSave}
            disabled={saving || !isValid}
            className="px-5 py-2 bg-blue-600 text-white text-sm font-medium rounded-lg disabled:opacity-50 hover:bg-blue-700 transition-colors"
          >
            {saving ? 'Saving…' : isNew ? 'Create Profile' : 'Save Changes'}
          </button>
        </div>
      </div>

      <style>{`
        .form-input {
          width: 100%;
          border: 1px solid #d1d5db;
          border-radius: 0.5rem;
          padding: 0.5rem 0.75rem;
          font-size: 0.875rem;
          outline: none;
          transition: border-color 0.15s, box-shadow 0.15s;
          background: white;
        }
        .form-input:focus {
          border-color: #3b82f6;
          box-shadow: 0 0 0 3px rgba(59,130,246,0.1);
        }
        select.form-input {
          appearance: auto;
        }
      `}</style>
    </div>
  );
}

// ============================================================================
// FIELD HELPERS
// ============================================================================

function Field({
  label, required, hint, children,
}: {
  label: string;
  required?: boolean;
  hint?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <div>
      <label className="block text-sm font-medium text-gray-700 mb-1">
        {label}
        {required && <span className="text-red-500 ml-0.5">*</span>}
      </label>
      {children}
      {hint && typeof hint === 'string' && (
        <p className="text-xs text-gray-500 mt-1">{hint}</p>
      )}
      {hint && typeof hint !== 'string' && (
        <p className="text-xs text-gray-500 mt-1">{hint}</p>
      )}
    </div>
  );
}

function CheckboxField({
  checked, onChange, label, hint,
}: {
  checked: boolean;
  onChange: (v: boolean) => void;
  label: string;
  hint?: string;
}) {
  return (
    <label className="flex items-start gap-2.5 cursor-pointer">
      <input
        type="checkbox"
        checked={checked}
        onChange={e => onChange(e.target.checked)}
        className="mt-0.5 rounded border-gray-300 text-blue-600 shrink-0"
      />
      <span>
        <span className="text-sm text-gray-700">{label}</span>
        {hint && <span className="block text-xs text-gray-400 mt-0.5">{hint}</span>}
      </span>
    </label>
  );
}

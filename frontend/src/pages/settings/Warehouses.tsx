import { useState, useEffect } from 'react';
import { getActiveTenantId } from '../../contexts/TenantContext';
import { useAuth, auth } from '../../contexts/AuthContext';

const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';

function api(path: string, init?: RequestInit) {
  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      'X-Tenant-Id': getActiveTenantId(),
      ...init?.headers,
    },
  });
}

interface Warehouse {
  source_id: string;
  name: string;
  code: string;
  type: 'own_warehouse' | 'fba' | '3pl' | 'dropship';
  active: boolean;
  default: boolean;
  address?: {
    address_line1?: string;
    city?: string;
    postal_code?: string;
    country?: string;
  };
  created_at: string;
}

const TYPE_LABELS: Record<string, string> = {
  own_warehouse: 'Own Warehouse',
  fba: 'Amazon FBA',
  '3pl': '3PL / Third Party',
  dropship: 'Dropship',
};

const TYPE_ICONS: Record<string, string> = {
  own_warehouse: '🏭',
  fba: '📦',
  '3pl': '🚚',
  dropship: '🔄',
};

const card: React.CSSProperties = {
  background: 'var(--bg-secondary)',
  border: '1px solid var(--border)',
  borderRadius: 10,
  padding: '18px 20px',
  marginBottom: 12,
  display: 'flex',
  alignItems: 'center',
  gap: 16,
};

const input: React.CSSProperties = {
  width: '100%',
  padding: '9px 12px',
  background: 'var(--bg-elevated)',
  border: '1px solid var(--border)',
  borderRadius: 8,
  color: 'var(--text-primary)',
  fontSize: 14,
  boxSizing: 'border-box' as const,
};

const btn = (variant: 'primary' | 'secondary' | 'danger' = 'primary'): React.CSSProperties => ({
  padding: '8px 18px',
  borderRadius: 7,
  border: variant === 'secondary' ? '1px solid var(--border)' : variant === 'danger' ? '1px solid rgba(239,68,68,0.4)' : 'none',
  background: variant === 'primary' ? 'var(--primary)' : variant === 'danger' ? 'rgba(239,68,68,0.12)' : 'var(--bg-elevated)',
  color: variant === 'danger' ? '#ef4444' : variant === 'primary' ? '#fff' : 'var(--text-primary)',
  fontWeight: 600,
  fontSize: 13,
  cursor: 'pointer',
});

const label: React.CSSProperties = {
  display: 'block',
  fontSize: 12,
  fontWeight: 600,
  color: 'var(--text-secondary)',
  marginBottom: 5,
};

export default function Warehouses() {
  const [warehouses, setWarehouses] = useState<Warehouse[]>([]);
  const [loading, setLoading] = useState(true);
  const [showForm, setShowForm] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  // auth imported directly from AuthContext

  // Form state
  const [name, setName] = useState('');
  const [code, setCode] = useState('');
  const [type, setType] = useState<string>('own_warehouse');
  const [addressLine1, setAddressLine1] = useState('');
  const [city, setCity] = useState('');
  const [postalCode, setPostalCode] = useState('');
  const [country, setCountry] = useState('GB');

  const authHeader = async (): Promise<Record<string,string>> => {
    try {
      if (auth?.currentUser) {
        const token = await auth.currentUser.getIdToken();
        return { 'Authorization': `Bearer ${token}` };
      }
    } catch {}
    return {};
  };

  const load = async () => {
    setLoading(true);
    try {
      const h = await authHeader();
      const r = await fetch(`${API_BASE}/fulfilment-sources`, {
        headers: { 'X-Tenant-Id': getActiveTenantId(), ...h },
      });
      const d = await r.json();
      setWarehouses((d.data || d.sources || []).filter((s: Warehouse) =>
        ['own_warehouse', 'fba', '3pl'].includes(s.type)
      ));
    } catch { setError('Failed to load warehouses'); }
    finally { setLoading(false); }
  };

  useEffect(() => { load(); }, []);

  const handleCreate = async () => {
    if (!name.trim()) { setError('Name is required'); return; }
    setSaving(true); setError('');
    try {
      const h = await authHeader();
      const payload: any = {
        name: name.trim(),
        code: code.trim() || name.trim().toUpperCase().replace(/\s+/g, '-').slice(0, 10),
        type,
        active: true,
        inventory_tracked: type !== 'fba',
        inventory_mode: 'manual',
      };
      if (addressLine1 || city || postalCode) {
        payload.address = {
          address_line1: addressLine1,
          city,
          postal_code: postalCode,
          country,
        };
      }
      const r = await fetch(`${API_BASE}/fulfilment-sources`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId(), ...h },
        body: JSON.stringify(payload),
      });
      if (!r.ok) {
        const d = await r.json();
        setError(d.error || 'Failed to create warehouse');
        return;
      }
      setShowForm(false);
      setName(''); setCode(''); setType('own_warehouse');
      setAddressLine1(''); setCity(''); setPostalCode(''); setCountry('GB');
      await load();
    } catch { setError('Failed to create warehouse'); }
    finally { setSaving(false); }
  };

  const handleSetDefault = async (sourceId: string) => {
    try {
      const h = await authHeader();
      await fetch(`${API_BASE}/fulfilment-sources/${sourceId}/set-default`, {
        method: 'POST',
        headers: { 'X-Tenant-Id': getActiveTenantId(), ...h },
      });
      await load();
    } catch { setError('Failed to set default'); }
  };

  const handleToggle = async (sourceId: string, active: boolean) => {
    try {
      const h = await authHeader();
      await fetch(`${API_BASE}/fulfilment-sources/${sourceId}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': getActiveTenantId(), ...h },
        body: JSON.stringify({ active: !active }),
      });
      await load();
    } catch { setError('Failed to update warehouse'); }
  };

  return (
    <div style={{ maxWidth: 720, margin: '0 auto', padding: '32px 24px' }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 24 }}>
        <div>
          <h1 style={{ fontSize: 22, fontWeight: 700, margin: 0 }}>Warehouses</h1>
          <p style={{ color: 'var(--text-muted)', fontSize: 13, margin: '4px 0 0' }}>
            Manage your fulfilment locations. Every tenant starts with a Default Warehouse.
          </p>
        </div>
        {!showForm && (
          <button style={btn('primary')} onClick={() => setShowForm(true)}>
            + Add Warehouse
          </button>
        )}
      </div>

      {error && (
        <div style={{ background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)',
          color: '#ef4444', borderRadius: 8, padding: '10px 14px', marginBottom: 16, fontSize: 13 }}>
          {error}
        </div>
      )}

      {/* Create form */}
      {showForm && (
        <div style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)',
          borderRadius: 10, padding: 20, marginBottom: 20 }}>
          <h3 style={{ fontSize: 15, fontWeight: 700, margin: '0 0 16px' }}>New Warehouse</h3>
          <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', marginBottom: 12 }}>
            <div style={{ flex: 2, minWidth: 200 }}>
              <label style={label}>Name *</label>
              <input style={input} value={name} onChange={e => setName(e.target.value)} placeholder="e.g. Main Warehouse" />
            </div>
            <div style={{ flex: 1, minWidth: 120 }}>
              <label style={label}>Code</label>
              <input style={input} value={code} onChange={e => setCode(e.target.value.toUpperCase())}
                placeholder="e.g. MAIN" maxLength={10} />
            </div>
          </div>
          <div style={{ marginBottom: 12 }}>
            <label style={label}>Type *</label>
            <select style={{ ...input, appearance: 'auto' }} value={type} onChange={e => setType(e.target.value)}>
              <option value="own_warehouse">🏭 Own Warehouse — you store and ship</option>
              <option value="fba">📦 Amazon FBA — Amazon stores and ships</option>
              <option value="3pl">🚚 3PL — third party stores and ships</option>
            </select>
          </div>
          {type === 'own_warehouse' && (
            <>
              <div style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 8 }}>
                Address (optional — used for shipping label return address)
              </div>
              <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', marginBottom: 12 }}>
                <div style={{ flex: 2, minWidth: 200 }}>
                  <input style={input} value={addressLine1} onChange={e => setAddressLine1(e.target.value)} placeholder="Address line 1" />
                </div>
                <div style={{ flex: 1, minWidth: 140 }}>
                  <input style={input} value={city} onChange={e => setCity(e.target.value)} placeholder="City" />
                </div>
                <div style={{ flex: 1, minWidth: 100 }}>
                  <input style={input} value={postalCode} onChange={e => setPostalCode(e.target.value)} placeholder="Postcode" />
                </div>
              </div>
            </>
          )}
          {error && <div style={{ color: '#ef4444', fontSize: 13, marginBottom: 10 }}>{error}</div>}
          <div style={{ display: 'flex', gap: 8 }}>
            <button style={btn('primary')} onClick={handleCreate} disabled={saving}>
              {saving ? 'Creating…' : 'Create Warehouse'}
            </button>
            <button style={btn('secondary')} onClick={() => { setShowForm(false); setError(''); }}>
              Cancel
            </button>
          </div>
        </div>
      )}

      {/* Warehouse list */}
      {loading ? (
        <div style={{ color: 'var(--text-muted)', textAlign: 'center', padding: 40 }}>Loading…</div>
      ) : warehouses.length === 0 ? (
        <div style={{ ...card, justifyContent: 'center', flexDirection: 'column',
          padding: 40, textAlign: 'center', color: 'var(--text-muted)' }}>
          <div style={{ fontSize: 32, marginBottom: 8 }}>🏭</div>
          <div style={{ fontWeight: 600, marginBottom: 4 }}>No warehouses yet</div>
          <div style={{ fontSize: 13 }}>A Default Warehouse is created automatically when you register.</div>
        </div>
      ) : (
        warehouses.map(wh => (
          <div key={wh.source_id} style={{ ...card, opacity: wh.active ? 1 : 0.6 }}>
            <div style={{ fontSize: 28, flexShrink: 0 }}>{TYPE_ICONS[wh.type] ?? '🏭'}</div>
            <div style={{ flex: 1, minWidth: 0 }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
                <span style={{ fontWeight: 700, fontSize: 15 }}>{wh.name}</span>
                <span style={{ fontSize: 11, background: 'var(--bg-tertiary)', padding: '1px 7px',
                  borderRadius: 4, color: 'var(--text-muted)', fontWeight: 600 }}>{wh.code}</span>
                {wh.default && (
                  <span style={{ fontSize: 11, background: 'rgba(34,197,94,0.12)', color: '#22c55e',
                    padding: '1px 7px', borderRadius: 4, fontWeight: 700 }}>DEFAULT</span>
                )}
                {!wh.active && (
                  <span style={{ fontSize: 11, background: 'rgba(239,68,68,0.1)', color: '#ef4444',
                    padding: '1px 7px', borderRadius: 4, fontWeight: 700 }}>INACTIVE</span>
                )}
              </div>
              <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 3 }}>
                {TYPE_LABELS[wh.type]}
                {wh.address?.city && ` · ${wh.address.city}`}
                {wh.address?.postal_code && `, ${wh.address.postal_code}`}
              </div>
            </div>
            <div style={{ display: 'flex', gap: 6, flexShrink: 0, flexWrap: 'wrap' }}>
              {!wh.default && wh.active && (
                <button style={btn('secondary')} onClick={() => handleSetDefault(wh.source_id)}>
                  Set Default
                </button>
              )}
              {wh.code !== 'DEFAULT' && (
                <button style={btn(wh.active ? 'danger' : 'secondary')}
                  onClick={() => handleToggle(wh.source_id, wh.active)}>
                  {wh.active ? 'Disable' : 'Enable'}
                </button>
              )}
            </div>
          </div>
        ))
      )}

      <div style={{ marginTop: 24, padding: '14px 16px', background: 'var(--bg-secondary)',
        borderRadius: 8, border: '1px solid var(--border)', fontSize: 12, color: 'var(--text-muted)', lineHeight: 1.7 }}>
        <strong style={{ color: 'var(--text-secondary)' }}>About warehouses</strong><br />
        <strong>Own Warehouse</strong> — stock you hold and ship yourself. Stock levels tracked here.<br />
        <strong>Amazon FBA</strong> — stock held by Amazon. Use FBA Inbound to create shipments.<br />
        <strong>3PL</strong> — stock held by a third-party logistics provider.<br />
        The <strong>Default</strong> warehouse is used when no specific warehouse is assigned to a product.
      </div>
    </div>
  );
}

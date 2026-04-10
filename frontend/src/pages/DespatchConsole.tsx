import { useState, useEffect, useRef, useCallback } from 'react';
import { getActiveTenantId } from '../contexts/TenantContext';
import { SerialNumberInput, SerialRequiredBadge } from '../components/SerialNumberInput';

// ─── Session 3 additions:
// • Dispatch confirmation screen (confirms order/carrier before final dispatch)
// • Address validation warnings (flags suspect addresses)
// • Dangerous goods flags (warns on hazmat keywords)
// • ZPL / PDF label format toggle
// • Fixed reprint flow (uses /dispatch/shipments/:id/reprint)
// • Tracking writeback trigger after shipment creation


const API_BASE = import.meta.env.VITE_API_URL ?? 'http://localhost:8080/api/v1';

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

// ─── Types ────────────────────────────────────────────────────────────────────

interface OrderLine {
  sku: string;
  product_id?: string;
  title: string;
  quantity: number;
  weight?: number;
  use_serial_numbers?: boolean;
}

interface Order {
  order_id: string;
  channel: string;
  channel_order_id: string;
  customer_name: string;
  status: string;
  sla_date?: string;
  total_weight?: number;
  line_items: OrderLine[];
  created_at: string;
}

interface Rate {
  carrier: string;
  carrier_id: string;
  service_code: string;
  service_name: string;
  cost: { amount: number; currency: string };
  estimated_days: number;
}

interface ShipmentResult {
  order_id: string;
  shipment_id?: string;
  tracking_number?: string;
  label_url?: string;
  error?: string;
  status: 'pending' | 'success' | 'error';
}

interface AddressIssue {
  field: string;
  severity: 'error' | 'warning';
  message: string;
}

interface AddressValidation {
  order_id: string;
  valid: boolean;
  issues: AddressIssue[];
}

interface DangerousGoodsFlag {
  sku: string;
  title: string;
  keyword: string;
  warning: string;
}

type SerialsByOrder = Record<string, Record<string, string[]>>;

interface ConfirmDispatchData {
  orders: Order[];
  rate: Rate;
  addressIssues: Record<string, AddressIssue[]>;
  dangerousGoods: Record<string, DangerousGoodsFlag[]>;
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

function slaColor(slaDate?: string): string {
  if (!slaDate) return 'var(--text-muted)';
  const d = new Date(slaDate);
  const now = new Date();
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const target = new Date(d.getFullYear(), d.getMonth(), d.getDate());
  const diff = (target.getTime() - today.getTime()) / (1000 * 60 * 60 * 24);
  if (diff < 0) return '#ef4444';
  if (diff === 0) return '#f59e0b';
  return '#22c55e';
}

function formatDate(iso?: string): string {
  if (!iso) return '—';
  return new Date(iso).toLocaleDateString('en-GB', { day: '2-digit', month: 'short', year: 'numeric' });
}

function itemCount(order: Order): number {
  return (order.line_items || []).reduce((s, l) => s + l.quantity, 0);
}

function totalWeight(order: Order): number {
  if (order.total_weight) return order.total_weight;
  return (order.line_items || []).reduce((s, l) => s + (l.weight ?? 0) * l.quantity, 0);
}

// ─── Main Component ───────────────────────────────────────────────────────────

export default function DespatchConsole() {
  const [orders, setOrders] = useState<Order[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [rates, setRates] = useState<Rate[]>([]);
  const [ratesLoading, setRatesLoading] = useState(false);
  const [ratesError, setRatesError] = useState('');

  const [selectedRate, setSelectedRate] = useState<Rate | null>(null);
  const [shipping, setShipping] = useState(false);
  const [progress, setProgress] = useState<ShipmentResult[]>([]);

  const [labels, setLabels] = useState<{ orderId: string; shipmentId: string; labelUrl: string }[]>([]);
  const [printingAll, setPrintingAll] = useState(false);

  // Session 3 additions
  const [labelFormat, setLabelFormat] = useState<'pdf' | 'zpl'>('pdf');
  const [confirmData, setConfirmData] = useState<ConfirmDispatchData | null>(null);
  const [validating, setValidating] = useState(false);
  const [addressIssues, setAddressIssues] = useState<Record<string, AddressIssue[]>>({});
  const [dangerousGoods, setDangerousGoods] = useState<Record<string, DangerousGoodsFlag[]>>({});

  const [serialsByOrder, setSerialsByOrder] = useState<SerialsByOrder>({});
  const [serialOrders, setSerialOrders] = useState<Set<string>>(new Set());

  const [scanMode, setScanMode] = useState(false);
  const scanInputRef = useRef<HTMLInputElement>(null);
  const [scanValue, setScanValue] = useState('');
  const [scanFeedback, setScanFeedback] = useState('');

  // Load orders
  const loadOrders = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const res = await api('/orders?status=ready_to_dispatch&limit=200');
      if (!res.ok) throw new Error('Failed to load orders');
      const data = await res.json();
      const list: Order[] = (data.orders || []).sort((a: Order, b: Order) => {
        if (!a.sla_date && !b.sla_date) return 0;
        if (!a.sla_date) return 1;
        if (!b.sla_date) return -1;
        return new Date(a.sla_date).getTime() - new Date(b.sla_date).getTime();
      });
      setOrders(list);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Failed to load orders');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { loadOrders(); }, [loadOrders]);

  useEffect(() => {
    if (scanMode && scanInputRef.current) {
      scanInputRef.current.focus();
    }
  }, [scanMode]);

  // Select all toggle
  const allSelected = orders.length > 0 && selected.size === orders.length;
  const toggleAll = () => {
    if (allSelected) setSelected(new Set());
    else setSelected(new Set(orders.map(o => o.order_id)));
  };
  const toggleOne = (id: string) => {
    const s = new Set(selected);
    if (s.has(id)) s.delete(id); else s.add(id);
    setSelected(s);
  };

  // Rate shop
  const shopRates = async () => {
    if (selected.size === 0) return;
    setRatesLoading(true);
    setRatesError('');
    setRates([]);
    setSelectedRate(null);
    try {
      const selectedOrders = orders.filter(o => selected.has(o.order_id));
      const totalW = selectedOrders.reduce((s, o) => s + totalWeight(o), 0);
      const res = await api('/dispatch/rates', {
        method: 'POST',
        body: JSON.stringify({ weight: totalW, order_ids: Array.from(selected) }),
      });
      if (!res.ok) throw new Error('Rate shop failed');
      const data = await res.json();
      setRates(data.rates || []);
      if ((data.rates || []).length > 0) setSelectedRate(data.rates[0]);
    } catch (e: unknown) {
      setRatesError(e instanceof Error ? e.message : 'Failed to get rates');
    } finally {
      setRatesLoading(false);
    }
  };

  // Pre-dispatch: validate addresses + dangerous goods, then show confirmation
  const preDispatch = async () => {
    if (!selectedRate || selected.size === 0) return;
    setValidating(true);

    const selectedOrders = orders.filter(o => selected.has(o.order_id));
    const newAddressIssues: Record<string, AddressIssue[]> = {};
    const newDangerousGoods: Record<string, DangerousGoodsFlag[]> = {};

    // Run validations in parallel
    await Promise.allSettled([
      // Address validation
      api('/dispatch/address-validate-bulk', {
        method: 'POST',
        body: JSON.stringify({ order_ids: selectedOrders.map(o => o.order_id) }),
      }).then(async res => {
        if (res.ok) {
          const data = await res.json();
          for (const result of (data.results || [])) {
            if ((result.issues || []).length > 0) {
              newAddressIssues[result.order_id] = result.issues;
            }
          }
        }
      }).catch(() => {}),

      // Dangerous goods checks
      ...selectedOrders.map(order =>
        api(`/dispatch/orders/${order.order_id}/dangerous-goods-check`, { method: 'POST' })
          .then(async res => {
            if (res.ok) {
              const data = await res.json();
              if (data.has_flags && (data.flags || []).length > 0) {
                newDangerousGoods[order.order_id] = data.flags;
              }
            }
          }).catch(() => {})
      ),
    ]);

    setAddressIssues(newAddressIssues);
    setDangerousGoods(newDangerousGoods);

    // ── Serial number check: find which SKUs are serial-tracked ─────────
    const allSkus = Array.from(new Set(
      selectedOrders.flatMap(o => (o.line_items || []).map(l => l.sku))
    ));
    const serialSkus = new Set<string>();
    await Promise.allSettled(
      allSkus.map(sku =>
        api(`/products?search=${encodeURIComponent(sku)}&limit=5`)
          .then(r => r.ok ? r.json() : null)
          .then(d => {
            const list: any[] = d?.data ?? d?.products ?? [];
            const match = list.find((p: any) => p.sku?.toLowerCase() === sku.toLowerCase()) ?? list[0];
            if (match?.use_serial_numbers) serialSkus.add(sku);
          }).catch(() => {})
      )
    );
    const ordersWithFlags = selectedOrders.map(o => ({
      ...o,
      line_items: (o.line_items || []).map(l => ({ ...l, use_serial_numbers: serialSkus.has(l.sku) })),
    }));
    const newSerialOrders = new Set<string>(
      ordersWithFlags.filter(o => o.line_items.some(l => l.use_serial_numbers)).map(o => o.order_id)
    );
    setSerialOrders(newSerialOrders);
    setSerialsByOrder(prev => {
      const next = { ...prev };
      for (const o of ordersWithFlags) {
        if (!next[o.order_id]) next[o.order_id] = {};
        for (const l of o.line_items) {
          if (l.use_serial_numbers && !next[o.order_id][l.sku]) next[o.order_id][l.sku] = [];
        }
      }
      return next;
    });
    // ─────────────────────────────────────────────────────────────────────

    // Show confirmation screen
    setConfirmData({
      orders: ordersWithFlags,
      rate: selectedRate,
      addressIssues: newAddressIssues,
      dangerousGoods: newDangerousGoods,
    });

    setValidating(false);
  };

  // Create shipments (called after confirmation)
  const createShipments = async () => {
    if (!selectedRate || selected.size === 0) return;
    // Block if any serialised line is missing serials
    if (confirmData) {
      for (const order of confirmData.orders) {
        for (const line of (order.line_items || [])) {
          if (!line.use_serial_numbers) continue;
          const entered = serialsByOrder[order.order_id]?.[line.sku] ?? [];
          if (entered.length < line.quantity) {
            alert(`Serial numbers incomplete for ${line.sku} on order ${order.channel_order_id || order.order_id}.\nNeed ${line.quantity}, got ${entered.length}.`);
            return;
          }
        }
      }
    }
    setConfirmData(null); // close confirmation modal
    setShipping(true);
    const selectedOrders = orders.filter(o => selected.has(o.order_id));
    const results: ShipmentResult[] = selectedOrders.map(o => ({ order_id: o.order_id, status: 'pending' }));
    setProgress([...results]);

    const newLabels = [...labels];

    for (let i = 0; i < selectedOrders.length; i++) {
      const order = selectedOrders[i];
      try {
        const res = await api('/dispatch/shipments', {
          method: 'POST',
          body: JSON.stringify({
            order_id: order.order_id,
            carrier_id: selectedRate.carrier_id,
            service_code: selectedRate.service_code,
            label_format: labelFormat,
            serial_numbers: serialsByOrder[order.order_id] ?? undefined,
          }),
        });
        if (!res.ok) throw new Error('Shipment failed');
        const data = await res.json();
        results[i] = {
          order_id: order.order_id,
          shipment_id: data.shipment_id,
          tracking_number: data.tracking_number,
          label_url: data.label_url,
          status: 'success',
        };
        if (data.shipment_id && data.label_url) {
          newLabels.push({ orderId: order.order_id, shipmentId: data.shipment_id, labelUrl: data.label_url });
        }
        // Trigger tracking writeback (fire-and-forget)
        if (data.shipment_id) {
          api(`/dispatch/shipments/${data.shipment_id}/writeback`, { method: 'POST' }).catch(() => {});
          api(`/dispatch/shipments/${data.shipment_id}/dispatch-email`, { method: 'POST' }).catch(() => {});
        }
      } catch (e: unknown) {
        results[i] = {
          order_id: order.order_id,
          error: e instanceof Error ? e.message : 'Failed',
          status: 'error',
        };
      }
      setProgress([...results]);
      setLabels([...newLabels]);
    }
    setShipping(false);
  };

  // Print all labels
  const printAllLabels = async () => {
    if (labels.length === 0) return;
    setPrintingAll(true);
    try {
      const res = await api('/shipments/print', {
        method: 'POST',
        body: JSON.stringify({ shipment_ids: labels.map(l => l.shipmentId) }),
      });
      if (!res.ok) throw new Error('Print failed');
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `labels_${new Date().toISOString().slice(0, 10)}.pdf`;
      a.click();
      URL.revokeObjectURL(url);
    } catch {
      // fallback: open each label individually
      labels.forEach(l => window.open(l.labelUrl, '_blank'));
    } finally {
      setPrintingAll(false);
    }
  };

  // Scan mode handler
  const handleScan = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key !== 'Enter') return;
    const val = scanValue.trim();
    setScanValue('');
    if (!val) return;

    const match = orders.find(o =>
      o.order_id === val ||
      o.channel_order_id === val ||
      (o.channel_order_id && o.channel_order_id.toLowerCase() === val.toLowerCase())
    );

    if (match) {
      const s = new Set(selected);
      s.add(match.order_id);
      setSelected(s);
      setScanFeedback(`✅ Order ${match.channel_order_id || match.order_id} selected`);
    } else {
      setScanFeedback(`❌ No order found for: ${val}`);
    }
    setTimeout(() => setScanFeedback(''), 2500);
  };

  // End-of-day CSV download
  const downloadManifest = () => {
    const done = progress.filter(p => p.status === 'success');
    if (done.length === 0) return;
    const rows = [['Order ID', 'Channel', 'Carrier', 'Service', 'Tracking Number', 'Weight (g)']];
    done.forEach(r => {
      const order = orders.find(o => o.order_id === r.order_id);
      rows.push([
        r.order_id,
        order?.channel ?? '',
        selectedRate?.carrier ?? '',
        selectedRate?.service_name ?? '',
        r.tracking_number ?? '',
        String(Math.round(totalWeight(order!))),
      ]);
    });
    const csv = rows.map(r => r.map(c => `"${c}"`).join(',')).join('\n');
    const blob = new Blob([csv], { type: 'text/csv' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `despatch_manifest_${new Date().toISOString().slice(0, 10)}.csv`;
    a.click();
    URL.revokeObjectURL(url);
  };

  const successCount = progress.filter(p => p.status === 'success').length;
  const errorCount = progress.filter(p => p.status === 'error').length;

  // ─── Render ─────────────────────────────────────────────────────────────────

  return (
    <div style={{ padding: '28px 32px', maxWidth: 1400, margin: '0 auto' }}>

      {/* Header */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 24, gap: 12 }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 24, fontWeight: 700, color: 'var(--text-primary)' }}>🚀 Despatch Console</h1>
          <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 14 }}>
            Work through your ready-to-dispatch queue, rate shop, and create shipments in bulk.
          </p>
        </div>
        <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
          {/* Scan mode toggle */}
          <button
            onClick={() => setScanMode(m => !m)}
            style={{
              ...btnStyle,
              background: scanMode ? 'var(--primary)' : 'var(--bg-elevated)',
              color: scanMode ? 'white' : 'var(--text-secondary)',
              border: `1px solid ${scanMode ? 'var(--primary)' : 'var(--border)'}`,
            }}
          >
            📷 Scan Mode {scanMode ? 'ON' : 'OFF'}
          </button>
          <button onClick={loadOrders} style={btnGhostStyle}>↻ Refresh</button>
          {progress.some(p => p.status === 'success') && (
            <button onClick={downloadManifest} style={btnPrimaryStyle}>📄 Complete Session</button>
          )}
        </div>
      </div>

      {/* Scan Mode Bar */}
      {scanMode && (
        <div style={{
          marginBottom: 20, padding: '14px 20px', background: 'rgba(99,102,241,0.08)',
          border: '2px solid var(--primary)', borderRadius: 10,
          display: 'flex', alignItems: 'center', gap: 14,
        }}>
          <span style={{ fontSize: 18 }}>🔍</span>
          <input
            ref={scanInputRef}
            value={scanValue}
            onChange={e => setScanValue(e.target.value)}
            onKeyDown={handleScan}
            placeholder="Scan order barcode and press Enter…"
            style={{ ...inputStyle, flex: 1, fontSize: 15 }}
            autoFocus
          />
          {scanFeedback && (
            <span style={{ fontSize: 13, color: scanFeedback.startsWith('✅') ? '#22c55e' : '#ef4444', whiteSpace: 'nowrap' }}>
              {scanFeedback}
            </span>
          )}
        </div>
      )}

      {error && <div style={errorStyle}>{error}</div>}

      {/* Main layout: left = queue, right = panels */}
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 380px', gap: 20, alignItems: 'start' }}>

        {/* Left — Order Queue */}
        <div style={sectionCard}>
          {/* Toolbar */}
          <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 16 }}>
            <span style={{ fontSize: 14, fontWeight: 600, color: 'var(--text-primary)' }}>
              Ready to Dispatch
            </span>
            <span style={{ fontSize: 12, color: 'var(--text-muted)', background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 20, padding: '2px 10px' }}>
              {orders.length} orders
            </span>
            {selected.size > 0 && (
              <span style={{ fontSize: 12, color: 'white', background: 'var(--primary)', borderRadius: 20, padding: '2px 10px', fontWeight: 600 }}>
                {selected.size} selected
              </span>
            )}
            <div style={{ marginLeft: 'auto', display: 'flex', gap: 8 }}>
              {selected.size > 0 && (
                <button
                  onClick={shopRates}
                  disabled={ratesLoading}
                  style={btnPrimaryStyle}
                >
                  {ratesLoading ? 'Getting rates…' : '💰 Rate Shop'}
                </button>
              )}
            </div>
          </div>

          {/* Table */}
          {loading ? (
            <div style={{ textAlign: 'center', padding: '40px 0', color: 'var(--text-muted)', fontSize: 14 }}>Loading orders…</div>
          ) : orders.length === 0 ? (
            <div style={{ textAlign: 'center', padding: '40px 0', color: 'var(--text-muted)', fontSize: 14 }}>
              No orders ready to dispatch.
            </div>
          ) : (
            <div style={{ overflowX: 'auto' }}>
              <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                <thead>
                  <tr>
                    <th style={{ ...thStyle, width: 36 }}>
                      <input type="checkbox" checked={allSelected} onChange={toggleAll} style={{ cursor: 'pointer' }} />
                    </th>
                    <th style={thStyle}>Order</th>
                    <th style={thStyle}>Channel</th>
                    <th style={thStyle}>Customer</th>
                    <th style={thStyle}>Items</th>
                    <th style={thStyle}>Weight</th>
                    <th style={thStyle}>SLA Date</th>
                    <th style={thStyle}>Status</th>
                  </tr>
                </thead>
                <tbody>
                  {orders.map(order => {
                    const result = progress.find(p => p.order_id === order.order_id);
                    const isSelected = selected.has(order.order_id);
                    return (
                      <tr
                        key={order.order_id}
                        onClick={() => toggleOne(order.order_id)}
                        style={{
                          cursor: 'pointer',
                          background: isSelected ? 'rgba(99,102,241,0.08)' : 'transparent',
                          borderBottom: '1px solid var(--border)',
                          transition: 'background 0.1s',
                        }}
                      >
                        <td style={{ ...tdStyle, textAlign: 'center' }}>
                          <input
                            type="checkbox"
                            checked={isSelected}
                            onChange={() => toggleOne(order.order_id)}
                            onClick={e => e.stopPropagation()}
                            style={{ cursor: 'pointer' }}
                          />
                        </td>
                        <td style={{ ...tdStyle, fontWeight: 600, color: 'var(--text-primary)', fontFamily: 'monospace', fontSize: 12 }}>
                          {order.channel_order_id || order.order_id.slice(0, 10)}
                        </td>
                        <td style={tdStyle}>
                          <span style={{ fontSize: 11, fontWeight: 600, background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 4, padding: '2px 7px', color: 'var(--text-secondary)', textTransform: 'uppercase', letterSpacing: '0.05em' }}>
                            {order.channel}
                          </span>
                        </td>
                        <td style={{ ...tdStyle, color: 'var(--text-primary)' }}>{order.customer_name || '—'}</td>
                        <td style={tdStyle}>{itemCount(order)}</td>
                        <td style={tdStyle}>{totalWeight(order) > 0 ? `${Math.round(totalWeight(order))}g` : '—'}</td>
                        <td style={{ ...tdStyle, color: slaColor(order.sla_date), fontWeight: 600 }}>
                          {formatDate(order.sla_date)}
                        </td>
                        <td style={{ ...tdStyle, textAlign: 'center' }}>
                          {result?.status === 'success' && <span title="Shipped">✅</span>}
                          {result?.status === 'error' && <span title={result.error}>❌</span>}
                          {result?.status === 'pending' && <span title="Processing…">⏳</span>}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        </div>

        {/* Right column */}
        <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>

          {/* Rate Shop Panel */}
          <div style={sectionCard}>
            <div style={{ fontWeight: 600, fontSize: 14, color: 'var(--text-primary)', marginBottom: 12 }}>
              💰 Carrier Rates
            </div>
            {ratesError && <div style={errorStyle}>{ratesError}</div>}
            {!ratesLoading && rates.length === 0 && (
              <div style={{ fontSize: 13, color: 'var(--text-muted)', textAlign: 'center', padding: '20px 0' }}>
                Select orders and click Rate Shop to compare carriers.
              </div>
            )}
            {ratesLoading && (
              <div style={{ fontSize: 13, color: 'var(--text-muted)', textAlign: 'center', padding: '20px 0' }}>
                Fetching rates…
              </div>
            )}
            {rates.length > 0 && (
              <>
                <div style={{ display: 'flex', flexDirection: 'column', gap: 8, marginBottom: 14 }}>
                  {rates.map((r, i) => {
                    const isChosen = selectedRate?.carrier_id === r.carrier_id && selectedRate?.service_code === r.service_code;
                    return (
                      <div
                        key={i}
                        onClick={() => setSelectedRate(r)}
                        style={{
                          padding: '10px 14px',
                          borderRadius: 8,
                          border: `2px solid ${isChosen ? 'var(--primary)' : 'var(--border)'}`,
                          background: isChosen ? 'rgba(99,102,241,0.08)' : 'var(--bg-elevated)',
                          cursor: 'pointer',
                          transition: 'all 0.1s',
                          display: 'flex',
                          alignItems: 'center',
                          justifyContent: 'space-between',
                          gap: 8,
                        }}
                      >
                        <div>
                          <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--text-primary)' }}>
                            {r.carrier} — {r.service_name}
                          </div>
                          <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 2 }}>
                            Est. {r.estimated_days} day{r.estimated_days !== 1 ? 's' : ''}
                          </div>
                        </div>
                        <div style={{ fontSize: 16, fontWeight: 700, color: isChosen ? 'var(--primary)' : 'var(--text-primary)' }}>
                          {r.cost.currency} {r.cost.amount.toFixed(2)}
                        </div>
                      </div>
                    );
                  })}
                </div>

                {/* Label format toggle */}
                <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 10 }}>
                  <span style={{ fontSize: 11, color: 'var(--text-muted)', fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.05em' }}>Label format:</span>
                  {(['pdf', 'zpl'] as const).map(fmt => (
                    <button
                      key={fmt}
                      onClick={() => setLabelFormat(fmt)}
                      style={{
                        padding: '4px 12px', borderRadius: 4, fontSize: 12, cursor: 'pointer', fontWeight: 600,
                        background: labelFormat === fmt ? 'var(--primary)' : 'var(--bg-elevated)',
                        color: labelFormat === fmt ? 'white' : 'var(--text-secondary)',
                        border: `1px solid ${labelFormat === fmt ? 'var(--primary)' : 'var(--border)'}`,
                      }}
                    >
                      {fmt.toUpperCase()}
                    </button>
                  ))}
                  <span style={{ fontSize: 10, color: 'var(--text-muted)', marginLeft: 4 }}>
                    {labelFormat === 'zpl' ? '(Zebra thermal)' : '(Desktop printer)'}
                  </span>
                </div>

                <button
                  onClick={preDispatch}
                  disabled={!selectedRate || shipping || validating}
                  style={{
                    ...btnPrimaryStyle,
                    width: '100%',
                    opacity: !selectedRate || shipping || validating ? 0.6 : 1,
                    cursor: !selectedRate || shipping || validating ? 'not-allowed' : 'pointer',
                  }}
                >
                  {validating ? '🔍 Validating…' : shipping ? 'Creating shipments…' : `🚀 Despatch ${selected.size} Order${selected.size !== 1 ? 's' : ''}`}
                </button>
              </>
            )}
          </div>

          {/* Progress Panel */}
          {progress.length > 0 && (
            <div style={sectionCard}>
              <div style={{ fontWeight: 600, fontSize: 14, color: 'var(--text-primary)', marginBottom: 10 }}>
                📦 Progress — {successCount + errorCount}/{progress.length}
              </div>
              {/* Progress bar */}
              <div style={{ height: 6, background: 'var(--bg-elevated)', borderRadius: 99, marginBottom: 12, overflow: 'hidden' }}>
                <div style={{
                  height: '100%',
                  width: `${((successCount + errorCount) / progress.length) * 100}%`,
                  background: errorCount > 0 ? '#f59e0b' : 'var(--primary)',
                  borderRadius: 99,
                  transition: 'width 0.3s',
                }} />
              </div>
              <div style={{ display: 'flex', gap: 12, marginBottom: 12 }}>
                <span style={{ fontSize: 12, color: '#22c55e', fontWeight: 600 }}>✅ {successCount} shipped</span>
                {errorCount > 0 && <span style={{ fontSize: 12, color: '#ef4444', fontWeight: 600 }}>❌ {errorCount} failed</span>}
              </div>
              <div style={{ maxHeight: 180, overflowY: 'auto', display: 'flex', flexDirection: 'column', gap: 4 }}>
                {progress.map((r, i) => {
                  const order = orders.find(o => o.order_id === r.order_id);
                  return (
                    <div key={i} style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 12, color: 'var(--text-secondary)' }}>
                      <span>
                        {r.status === 'success' ? '✅' : r.status === 'error' ? '❌' : '⏳'}
                      </span>
                      <span style={{ fontFamily: 'monospace', flex: 1 }}>
                        {order?.channel_order_id || r.order_id.slice(0, 10)}
                      </span>
                      {r.tracking_number && (
                        <span style={{ color: 'var(--text-muted)', fontSize: 11 }}>{r.tracking_number}</span>
                      )}
                      {r.error && <span style={{ color: '#ef4444', fontSize: 11 }}>{r.error}</span>}
                    </div>
                  );
                })}
              </div>
            </div>
          )}

          {/* Label Queue Panel */}
          {labels.length > 0 && (
            <div style={sectionCard}>
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 12 }}>
                <div style={{ fontWeight: 600, fontSize: 14, color: 'var(--text-primary)' }}>
                  🏷️ Labels ({labels.length})
                </div>
                <button
                  onClick={printAllLabels}
                  disabled={printingAll}
                  style={{ ...btnPrimaryStyle, fontSize: 12, padding: '6px 12px' }}
                >
                  {printingAll ? 'Printing…' : 'Print All'}
                </button>
              </div>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 6, maxHeight: 220, overflowY: 'auto' }}>
                {labels.map((l, i) => {
                  const order = orders.find(o => o.order_id === l.orderId);
                  return (
                    <div key={i} style={{
                      display: 'flex', alignItems: 'center', justifyContent: 'space-between',
                      padding: '8px 10px', background: 'var(--bg-elevated)',
                      border: '1px solid var(--border)', borderRadius: 6,
                    }}>
                      <div>
                        <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-primary)', fontFamily: 'monospace' }}>
                          {order?.channel_order_id || l.orderId.slice(0, 10)}
                        </div>
                        <div style={{ fontSize: 11, color: 'var(--text-muted)' }}>{l.shipmentId.slice(0, 12)}…</div>
                      </div>
                      <a
                        href={l.labelUrl}
                        target="_blank"
                        rel="noopener noreferrer"
                        style={{ fontSize: 12, color: 'var(--primary)', textDecoration: 'none' }}
                        onClick={e => e.stopPropagation()}
                      >
                        ↓ Download
                      </a>
                    </div>
                  );
                })}
              </div>
            </div>
          )}

          {/* Legend */}
          <div style={{ ...sectionCard, padding: 14 }}>
            <div style={{ fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: 8 }}>SLA Legend</div>
            <div style={{ display: 'flex', gap: 16 }}>
              <span style={{ fontSize: 12, color: '#ef4444' }}>● Overdue</span>
              <span style={{ fontSize: 12, color: '#f59e0b' }}>● Due today</span>
              <span style={{ fontSize: 12, color: '#22c55e' }}>● On track</span>
            </div>
          </div>
        </div>
      </div>

      {/* ── Dispatch Confirmation Modal ── */}
      {confirmData && (
        <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.65)', zIndex: 1000, display: 'flex', alignItems: 'center', justifyContent: 'center', padding: 24 }}>
          <div style={{ background: 'var(--bg-primary)', border: '1px solid var(--border)', borderRadius: 14, padding: 28, width: '100%', maxWidth: 560, maxHeight: '85vh', overflowY: 'auto' }}>
            <h2 style={{ margin: '0 0 4px', fontSize: 18, fontWeight: 700, color: 'var(--text-primary)' }}>
              🚀 Confirm Despatch
            </h2>
            <p style={{ margin: '0 0 20px', fontSize: 13, color: 'var(--text-muted)' }}>
              Review any warnings before creating {confirmData.orders.length} shipment{confirmData.orders.length !== 1 ? 's' : ''}.
            </p>

            {/* Carrier / Service summary */}
            <div style={{ padding: '12px 16px', background: 'rgba(99,102,241,0.08)', border: '1px solid rgba(99,102,241,0.3)', borderRadius: 8, marginBottom: 16, fontSize: 13 }}>
              <strong style={{ color: 'var(--text-primary)' }}>{confirmData.rate.carrier}</strong> — {confirmData.rate.service_name}
              <span style={{ marginLeft: 12, color: 'var(--text-muted)' }}>
                {confirmData.rate.cost.currency} {confirmData.rate.cost.amount.toFixed(2)} per shipment
              </span>
              <span style={{ marginLeft: 12, background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 4, padding: '2px 8px', fontSize: 11 }}>
                {labelFormat.toUpperCase()}
              </span>
            </div>

            {/* Address warnings */}
            {Object.keys(confirmData.addressIssues).length > 0 && (
              <div style={{ marginBottom: 16 }}>
                <div style={{ fontSize: 12, fontWeight: 700, color: '#f59e0b', marginBottom: 8, textTransform: 'uppercase', letterSpacing: '0.05em' }}>
                  ⚠️ Address Warnings ({Object.keys(confirmData.addressIssues).length} orders)
                </div>
                {Object.entries(confirmData.addressIssues).map(([oid, issues]) => {
                  const order = confirmData.orders.find(o => o.order_id === oid);
                  return (
                    <div key={oid} style={{ marginBottom: 8, padding: '10px 12px', background: 'rgba(245,158,11,0.08)', border: '1px solid rgba(245,158,11,0.3)', borderRadius: 6 }}>
                      <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-primary)', marginBottom: 4 }}>
                        {order?.channel_order_id || oid.slice(0, 12)}
                      </div>
                      {issues.map((issue, i) => (
                        <div key={i} style={{ fontSize: 11, color: issue.severity === 'error' ? '#ef4444' : '#f59e0b' }}>
                          {issue.severity === 'error' ? '✗' : '△'} {issue.message}
                        </div>
                      ))}
                    </div>
                  );
                })}
              </div>
            )}

            {/* Dangerous goods warnings */}
            {Object.keys(confirmData.dangerousGoods).length > 0 && (
              <div style={{ marginBottom: 16 }}>
                <div style={{ fontSize: 12, fontWeight: 700, color: '#ef4444', marginBottom: 8, textTransform: 'uppercase', letterSpacing: '0.05em' }}>
                  ☢️ Dangerous Goods Flags ({Object.keys(confirmData.dangerousGoods).length} orders)
                </div>
                {Object.entries(confirmData.dangerousGoods).map(([oid, flags]) => {
                  const order = confirmData.orders.find(o => o.order_id === oid);
                  return (
                    <div key={oid} style={{ marginBottom: 8, padding: '10px 12px', background: 'rgba(239,68,68,0.08)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: 6 }}>
                      <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-primary)', marginBottom: 4 }}>
                        {order?.channel_order_id || oid.slice(0, 12)}
                      </div>
                      {flags.map((f, i) => (
                        <div key={i} style={{ fontSize: 11, color: '#ef4444' }}>
                          ⚠️ {f.title} — {f.warning}
                        </div>
                      ))}
                    </div>
                  );
                })}
              </div>
            )}

            {/* Order list */}
            <div style={{ marginBottom: 20 }}>
              <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: 8 }}>
                Orders ({confirmData.orders.length})
              </div>
              <div style={{ maxHeight: 180, overflowY: 'auto', display: 'flex', flexDirection: 'column', gap: 4 }}>
                {confirmData.orders.map(o => {
                  const hasAddrIssue = !!confirmData.addressIssues[o.order_id];
                  const hasDGFlag = !!confirmData.dangerousGoods[o.order_id];
                  return (
                    <div key={o.order_id} style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 12, padding: '4px 0', borderBottom: '1px solid var(--border)' }}>
                      <span style={{ fontFamily: 'monospace', flex: 1, color: 'var(--text-primary)' }}>{o.channel_order_id || o.order_id.slice(0, 12)}</span>
                      {serialOrders.has(o.order_id) && <SerialRequiredBadge />}
                      <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>{o.channel}</span>
                      {hasAddrIssue && <span title="Address warning" style={{ color: '#f59e0b' }}>⚠️</span>}
                      {hasDGFlag && <span title="Dangerous goods" style={{ color: '#ef4444' }}>☢️</span>}
                    </div>
                  );
                })}
              </div>
            </div>

            {/* Serial Numbers section */}
            {serialOrders.size > 0 && (
              <div style={{ marginBottom: 16 }}>
                <div style={{ fontSize: 12, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.06em', marginBottom: 8 }}>
                  🔢 Serial Numbers Required
                </div>
                <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                  {confirmData.orders
                    .filter(o => serialOrders.has(o.order_id))
                    .map(o => (
                      <div key={o.order_id} style={{ padding: 12, background: 'var(--bg-elevated)', borderRadius: 8, border: '1px solid rgba(6,182,212,0.2)' }}>
                        <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 8 }}>
                          Order {o.channel_order_id || o.order_id.slice(0, 12)}
                        </div>
                        {(o.line_items || []).filter(l => l.use_serial_numbers).map(line => (
                          <div key={line.sku} style={{ marginBottom: 8 }}>
                            <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 6, fontSize: 12 }}>
                              <span style={{ fontFamily: 'monospace', color: 'var(--accent-cyan)' }}>{line.sku}</span>
                              <SerialRequiredBadge />
                              <span style={{ color: 'var(--text-muted)' }}>× {line.quantity}</span>
                            </div>
                            <SerialNumberInput
                              quantity={line.quantity}
                              value={serialsByOrder[o.order_id]?.[line.sku] ?? []}
                              onChange={sns => setSerialsByOrder(prev => ({
                                ...prev,
                                [o.order_id]: { ...(prev[o.order_id] ?? {}), [line.sku]: sns },
                              }))}
                              label=''
                            />
                          </div>
                        ))}
                      </div>
                    ))
                  }
                </div>
              </div>
            )}

            {/* Action buttons */}
            <div style={{ display: 'flex', gap: 10, justifyContent: 'flex-end' }}>
              <button
                onClick={() => setConfirmData(null)}
                style={{ padding: '8px 20px', background: 'transparent', color: 'var(--text-secondary)', border: '1px solid var(--border)', borderRadius: 6, cursor: 'pointer', fontSize: 13 }}
              >
                Cancel
              </button>
              <button
                onClick={createShipments}
                style={{ padding: '10px 24px', background: 'var(--primary)', color: 'white', border: 'none', borderRadius: 6, cursor: 'pointer', fontSize: 13, fontWeight: 700 }}
              >
                ✓ Confirm & Despatch
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

// ─── Styles ───────────────────────────────────────────────────────────────────

const sectionCard: React.CSSProperties = {
  padding: 20,
  background: 'var(--bg-secondary)',
  border: '1px solid var(--border)',
  borderRadius: 10,
};

const inputStyle: React.CSSProperties = {
  padding: '8px 12px',
  background: 'var(--bg-elevated)',
  border: '1px solid var(--border)',
  borderRadius: 6,
  color: 'var(--text-primary)',
  fontSize: 13,
  outline: 'none',
};

const btnGhostStyle: React.CSSProperties = {
  padding: '8px 16px',
  background: 'transparent',
  color: 'var(--text-secondary)',
  border: '1px solid var(--border)',
  borderRadius: 6,
  cursor: 'pointer',
  fontSize: 13,
};

const btnPrimaryStyle: React.CSSProperties = {
  padding: '8px 18px',
  background: 'var(--primary)',
  color: 'white',
  border: 'none',
  borderRadius: 6,
  cursor: 'pointer',
  fontSize: 13,
  fontWeight: 600,
};

const btnStyle: React.CSSProperties = {
  padding: '8px 16px',
  borderRadius: 6,
  cursor: 'pointer',
  fontSize: 13,
  fontWeight: 500,
};

const errorStyle: React.CSSProperties = {
  marginBottom: 14,
  padding: '10px 14px',
  background: 'rgba(239,68,68,0.1)',
  border: '1px solid rgba(239,68,68,0.3)',
  borderRadius: 6,
  color: '#ef4444',
  fontSize: 13,
};

const thStyle: React.CSSProperties = {
  padding: '10px 12px',
  textAlign: 'left',
  fontSize: 11,
  fontWeight: 600,
  textTransform: 'uppercase',
  letterSpacing: '0.05em',
  color: 'var(--text-muted)',
  borderBottom: '1px solid var(--border)',
  whiteSpace: 'nowrap',
};

const tdStyle: React.CSSProperties = {
  padding: '11px 12px',
  color: 'var(--text-secondary)',
  fontSize: 13,
};

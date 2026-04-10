import { useState } from "react";

// ─────────────────────────────────────────────────────────────────────────────
// TYPES
// ─────────────────────────────────────────────────────────────────────────────

interface OrderLine {
  line_id: string;
  sku: string;
  title: string;
  quantity: number;
  unit_price: { amount: number; currency: string };
  line_total: { amount: number; currency: string };
  tax: { amount: number; currency: string };
  fulfilment_type: string;
  fulfilment_source_id: string;
  status: string;
  fulfilled_quantity: number;
  cancelled_quantity: number;
}

interface SimForm {
  channel: string;
  country: string;
  postal_code: string;
  state: string;
  city: string;
  subtotal: string;
  shipping_cost: string;
  grand_total: string;
  currency: string;
  payment_status: string;
  fulfilment_type: string;
  tags: string;
  promised_ship_by: string;
  lines: OrderLine[];
}

interface WorkflowResult {
  workflow_id: string;
  workflow_name: string;
  priority: number;
  matched: boolean;
  conditions: {
    type: string;
    passed: boolean;
    actual: unknown;
    reason: string;
  }[];
  actions_would_execute?: { type: string; config: Record<string, unknown> }[];
}

interface SimulateResponse {
  results: WorkflowResult[];
  matched_workflow?: string;
  matched_workflow_name?: string;
  total_workflows_evaluated: number;
}

// ─────────────────────────────────────────────────────────────────────────────
// CONSTANTS
// ─────────────────────────────────────────────────────────────────────────────

const API_BASE = import.meta.env.VITE_API_URL ?? "https://marketmate-api-487246736287.us-central1.run.app";

const CHANNELS = ["amazon", "ebay", "temu", "shopify", "woocommerce", "manual"];
const PAYMENT_STATUSES = ["pending", "authorized", "captured", "failed"];
const FULFILMENT_TYPES = ["stock", "dropship", "fba", "network", "mixed"];
const CURRENCIES = ["GBP", "USD", "EUR", "AUD", "CAD"];

const EMPTY_LINE = (): OrderLine => ({
  line_id: `line_${Date.now()}_${Math.random().toString(36).slice(2, 6)}`,
  sku: "",
  title: "",
  quantity: 1,
  unit_price: { amount: 0, currency: "GBP" },
  line_total: { amount: 0, currency: "GBP" },
  tax: { amount: 0, currency: "GBP" },
  fulfilment_type: "stock",
  fulfilment_source_id: "",
  status: "pending",
  fulfilled_quantity: 0,
  cancelled_quantity: 0,
});

const DEFAULT_FORM: SimForm = {
  channel: "amazon",
  country: "GB",
  postal_code: "SW1A 1AA",
  state: "",
  city: "London",
  subtotal: "49.99",
  shipping_cost: "3.99",
  grand_total: "53.98",
  currency: "GBP",
  payment_status: "captured",
  fulfilment_type: "stock",
  tags: "",
  promised_ship_by: "",
  lines: [{ ...EMPTY_LINE(), sku: "SKU-001", title: "Sample Product", quantity: 1, unit_price: { amount: 49.99, currency: "GBP" }, line_total: { amount: 49.99, currency: "GBP" }, tax: { amount: 0, currency: "GBP" } }],
};

// ─────────────────────────────────────────────────────────────────────────────
// HELPERS
// ─────────────────────────────────────────────────────────────────────────────

function buildPayload(form: SimForm, tenantId: string) {
  const tags = form.tags
    .split(",")
    .map((t) => t.trim())
    .filter(Boolean);

  const currency = form.currency;
  const lines = form.lines.map((l) => ({
    ...l,
    unit_price: { ...l.unit_price, currency },
    line_total: { ...l.line_total, currency },
    tax: { ...l.tax, currency },
  }));

  const totalWeight = lines.reduce(
    (sum, l) => sum + (parseFloat(String(l.quantity)) || 0),
    0
  );

  return {
    order: {
      order_id: `sim_${Date.now()}`,
      tenant_id: tenantId,
      channel: form.channel,
      channel_account_id: `${form.channel}_sim`,
      external_order_id: `SIM-${Date.now()}`,
      status: "imported",
      sub_status: "",
      payment_status: form.payment_status,
      payment_method: "card",
      fulfilment_type: form.fulfilment_type,
      tags,
      customer: { name: "Simulation User", email: "sim@test.com" },
      shipping_address: {
        name: "Simulation User",
        address_line1: "1 Test Street",
        address_line2: "",
        city: form.city,
        state: form.state,
        postal_code: form.postal_code,
        country: form.country,
      },
      totals: {
        subtotal: { amount: parseFloat(form.subtotal) || 0, currency },
        shipping: { amount: parseFloat(form.shipping_cost) || 0, currency },
        tax: { amount: 0, currency },
        discount: { amount: 0, currency },
        grand_total: { amount: parseFloat(form.grand_total) || 0, currency },
      },
      promised_ship_by: form.promised_ship_by || "",
      sla_at_risk: false,
      order_date: new Date().toISOString(),
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
      imported_at: new Date().toISOString(),
      shipment_ids: [],
      reservation_ids: [],
      _sim_weight_kg: totalWeight,
    },
    lines,
  };
}

// ─────────────────────────────────────────────────────────────────────────────
// SUB-COMPONENTS
// ─────────────────────────────────────────────────────────────────────────────

function Field({ label, children, hint }: { label: string; children: React.ReactNode; hint?: string }) {
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
      <label style={{ fontSize: 11, fontWeight: 700, letterSpacing: "0.08em", textTransform: "uppercase", color: "var(--text-dim)" }}>
        {label}
      </label>
      {children}
      {hint && <span style={{ fontSize: 11, color: "var(--text-muted)" }}>{hint}</span>}
    </div>
  );
}

function Input({ value, onChange, type = "text", placeholder, style }: {
  value: string; onChange: (v: string) => void; type?: string; placeholder?: string; style?: React.CSSProperties;
}) {
  return (
    <input
      type={type}
      value={value}
      placeholder={placeholder}
      onChange={(e) => onChange(e.target.value)}
      style={{
        background: "var(--input-bg)",
        border: "1px solid var(--border)",
        borderRadius: 3,
        color: "var(--text)",
        fontFamily: "var(--font-mono)",
        fontSize: 13,
        padding: "7px 10px",
        outline: "none",
        transition: "border-color 0.15s",
        ...style,
      }}
      onFocus={(e) => (e.currentTarget.style.borderColor = "var(--accent)")}
      onBlur={(e) => (e.currentTarget.style.borderColor = "var(--border)")}
    />
  );
}

function Select({ value, onChange, options }: {
  value: string; onChange: (v: string) => void; options: string[];
}) {
  return (
    <select
      value={value}
      onChange={(e) => onChange(e.target.value)}
      style={{
        background: "var(--input-bg)",
        border: "1px solid var(--border)",
        borderRadius: 3,
        color: "var(--text)",
        fontFamily: "var(--font-mono)",
        fontSize: 13,
        padding: "7px 10px",
        outline: "none",
        cursor: "pointer",
        appearance: "none",
        backgroundImage: `url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='12' height='12' viewBox='0 0 12 12'%3E%3Cpath fill='%23666' d='M6 8L1 3h10z'/%3E%3C/svg%3E")`,
        backgroundRepeat: "no-repeat",
        backgroundPosition: "right 10px center",
        paddingRight: 28,
      }}
    >
      {options.map((o) => <option key={o} value={o}>{o}</option>)}
    </select>
  );
}

function SectionTitle({ children }: { children: React.ReactNode }) {
  return (
    <div style={{
      fontSize: 10,
      fontWeight: 800,
      letterSpacing: "0.15em",
      textTransform: "uppercase",
      color: "var(--accent)",
      borderBottom: "1px solid var(--border)",
      paddingBottom: 6,
      marginBottom: 12,
    }}>
      {children}
    </div>
  );
}

function ConditionRow({ cond }: { cond: WorkflowResult["conditions"][number] }) {
  const color = cond.passed ? "var(--green)" : "var(--red)";
  return (
    <div style={{
      display: "grid",
      gridTemplateColumns: "18px 120px 1fr",
      gap: 8,
      alignItems: "start",
      padding: "5px 0",
      borderBottom: "1px solid var(--border-faint)",
      fontSize: 12,
      fontFamily: "var(--font-mono)",
    }}>
      <span style={{ color, fontSize: 14, lineHeight: 1 }}>{cond.passed ? "✓" : "✗"}</span>
      <span style={{ color: "var(--text-dim)", textTransform: "uppercase", fontSize: 10, letterSpacing: "0.06em", paddingTop: 2 }}>
        {cond.type}
      </span>
      <span style={{ color: "var(--text)" }}>{cond.reason}</span>
    </div>
  );
}

function ResultCard({ r, index }: { r: WorkflowResult; index: number }) {
  const [open, setOpen] = useState(index === 0);
  const borderColor = r.matched ? "var(--green)" : "var(--border)";
  const matchBadgeBg = r.matched ? "var(--green)" : "var(--surface2)";
  const matchBadgeText = r.matched ? "#000" : "var(--text-dim)";

  return (
    <div style={{
      border: `1px solid ${borderColor}`,
      borderRadius: 4,
      overflow: "hidden",
      transition: "border-color 0.2s",
    }}>
      <button
        onClick={() => setOpen(!open)}
        style={{
          width: "100%",
          background: r.matched ? "rgba(0,220,130,0.06)" : "var(--surface)",
          border: "none",
          padding: "10px 14px",
          cursor: "pointer",
          display: "flex",
          alignItems: "center",
          gap: 10,
          textAlign: "left",
        }}
      >
        <span style={{
          background: matchBadgeBg,
          color: matchBadgeText,
          fontSize: 10,
          fontWeight: 800,
          letterSpacing: "0.1em",
          padding: "2px 7px",
          borderRadius: 2,
          flexShrink: 0,
        }}>
          {r.matched ? "MATCH" : "NO MATCH"}
        </span>
        <span style={{ fontFamily: "var(--font-mono)", fontSize: 13, color: "var(--text)", flex: 1 }}>
          {r.workflow_name}
        </span>
        <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--text-muted)" }}>
          P{r.priority}
        </span>
        <span style={{ color: "var(--text-muted)", fontSize: 12, marginLeft: 4 }}>
          {open ? "▲" : "▼"}
        </span>
      </button>

      {open && (
        <div style={{ padding: "12px 14px", background: "var(--surface)", borderTop: "1px solid var(--border-faint)" }}>
          {r.conditions.length > 0 ? (
            <>
              <div style={{ fontSize: 10, fontWeight: 700, letterSpacing: "0.1em", textTransform: "uppercase", color: "var(--text-muted)", marginBottom: 6 }}>
                Conditions
              </div>
              {r.conditions.map((c, i) => <ConditionRow key={i} cond={c} />)}
            </>
          ) : (
            <span style={{ fontSize: 12, color: "var(--text-muted)", fontStyle: "italic" }}>No conditions (matches everything)</span>
          )}

          {r.matched && r.actions_would_execute && r.actions_would_execute.length > 0 && (
            <div style={{ marginTop: 14 }}>
              <div style={{ fontSize: 10, fontWeight: 700, letterSpacing: "0.1em", textTransform: "uppercase", color: "var(--text-muted)", marginBottom: 6 }}>
                Actions that would execute
              </div>
              {r.actions_would_execute.map((a, i) => (
                <div key={i} style={{
                  display: "flex",
                  gap: 10,
                  padding: "5px 0",
                  borderBottom: "1px solid var(--border-faint)",
                  fontSize: 12,
                  fontFamily: "var(--font-mono)",
                }}>
                  <span style={{ color: "var(--accent)", minWidth: 140, textTransform: "uppercase", fontSize: 10, letterSpacing: "0.06em" }}>
                    {a.type}
                  </span>
                  <span style={{ color: "var(--text-dim)", wordBreak: "break-all" }}>
                    {JSON.stringify(a.config)}
                  </span>
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

// ─────────────────────────────────────────────────────────────────────────────
// MAIN COMPONENT
// ─────────────────────────────────────────────────────────────────────────────

export default function WorkflowSimulator() {
  const [form, setForm] = useState<SimForm>(DEFAULT_FORM);
  const [tenantId, setTenantId] = useState("247-commerce");
  const [loading, setLoading] = useState(false);
  const [response, setResponse] = useState<SimulateResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [rawJson, setRawJson] = useState(false);
  const [lastPayload, setLastPayload] = useState<object | null>(null);

  const set = (key: keyof SimForm) => (val: string) =>
    setForm((f) => ({ ...f, [key]: val }));

  const autoTotal = () => {
    const sub = parseFloat(form.subtotal) || 0;
    const ship = parseFloat(form.shipping_cost) || 0;
    setForm((f) => ({ ...f, grand_total: (sub + ship).toFixed(2) }));
  };

  // Line helpers
  const setLine = (idx: number, key: keyof OrderLine, value: unknown) =>
    setForm((f) => {
      const lines = [...f.lines];
      lines[idx] = { ...lines[idx], [key]: value } as OrderLine;
      return { ...f, lines };
    });

  const addLine = () =>
    setForm((f) => ({ ...f, lines: [...f.lines, EMPTY_LINE()] }));

  const removeLine = (idx: number) =>
    setForm((f) => ({ ...f, lines: f.lines.filter((_, i) => i !== idx) }));

  const handleSubmit = async () => {
    setLoading(true);
    setError(null);
    const payload = buildPayload(form, tenantId);
    setLastPayload(payload);
    try {
      const res = await fetch(`${API_BASE}/api/v1/workflows/simulate`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "X-Tenant-Id": tenantId,
        },
        body: JSON.stringify(payload),
      });
      if (!res.ok) {
        const text = await res.text();
        throw new Error(`${res.status}: ${text}`);
      }
      const data = await res.json();
      setResponse(data);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  };

  const inputStyle: React.CSSProperties = { width: "100%" };
  const gridRow = (cols: string): React.CSSProperties => ({
    display: "grid",
    gridTemplateColumns: cols,
    gap: 12,
  });

  return (
    <>
      <style>{`
        @import url('https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@400;500;600&family=IBM+Plex+Sans:wght@400;500;600;700&display=swap');

        :root {
          --bg: #0d0f11;
          --surface: #13161a;
          --surface2: #1c2026;
          --border: #2a2e35;
          --border-faint: #1e2228;
          --input-bg: #0a0c0e;
          --text: #e2e4e9;
          --text-dim: #8a8f98;
          --text-muted: #555b66;
          --accent: #f0a500;
          --accent-dim: rgba(240,165,0,0.15);
          --green: #00dc82;
          --red: #ff4d4d;
          --font-sans: 'IBM Plex Sans', sans-serif;
          --font-mono: 'IBM Plex Mono', monospace;
        }

        * { box-sizing: border-box; margin: 0; padding: 0; }

        body {
          background: var(--bg);
          color: var(--text);
          font-family: var(--font-sans);
          min-height: 100vh;
        }

        input, select, textarea {
          width: 100%;
          font-family: var(--font-mono);
        }

        input::placeholder { color: var(--text-muted); }

        ::-webkit-scrollbar { width: 6px; }
        ::-webkit-scrollbar-track { background: var(--bg); }
        ::-webkit-scrollbar-thumb { background: var(--border); border-radius: 3px; }

        .run-btn {
          background: var(--accent);
          color: #000;
          border: none;
          padding: 11px 28px;
          font-family: var(--font-mono);
          font-size: 13px;
          font-weight: 600;
          letter-spacing: 0.08em;
          text-transform: uppercase;
          border-radius: 3px;
          cursor: pointer;
          transition: opacity 0.15s, transform 0.1s;
        }
        .run-btn:hover:not(:disabled) { opacity: 0.88; transform: translateY(-1px); }
        .run-btn:active { transform: translateY(0); }
        .run-btn:disabled { opacity: 0.4; cursor: not-allowed; }

        .ghost-btn {
          background: transparent;
          color: var(--text-dim);
          border: 1px solid var(--border);
          padding: 6px 14px;
          font-family: var(--font-mono);
          font-size: 11px;
          font-weight: 600;
          letter-spacing: 0.08em;
          text-transform: uppercase;
          border-radius: 3px;
          cursor: pointer;
          transition: border-color 0.15s, color 0.15s;
        }
        .ghost-btn:hover { border-color: var(--accent); color: var(--accent); }

        .del-btn {
          background: transparent;
          border: 1px solid var(--border);
          color: var(--red);
          width: 26px;
          height: 26px;
          border-radius: 3px;
          cursor: pointer;
          font-size: 14px;
          display: flex;
          align-items: center;
          justify-content: center;
          flex-shrink: 0;
          transition: background 0.15s;
        }
        .del-btn:hover { background: rgba(255,77,77,0.1); }

        @keyframes pulse {
          0%, 100% { opacity: 1; }
          50% { opacity: 0.4; }
        }
      `}</style>

      <div style={{ maxWidth: 1280, margin: "0 auto", padding: "24px 20px" }}>

        {/* Header */}
        <div style={{ display: "flex", alignItems: "center", gap: 16, marginBottom: 24, paddingBottom: 16, borderBottom: "1px solid var(--border)" }}>
          <div style={{
            width: 36, height: 36, borderRadius: 4,
            background: "var(--accent-dim)",
            border: "1px solid var(--accent)",
            display: "flex", alignItems: "center", justifyContent: "center",
            fontSize: 18, flexShrink: 0,
          }}>⚡</div>
          <div>
            <div style={{ fontSize: 18, fontWeight: 700, letterSpacing: "-0.01em", color: "var(--text)" }}>
              Workflow Simulator
            </div>
            <div style={{ fontSize: 12, color: "var(--text-muted)", fontFamily: "var(--font-mono)", marginTop: 2 }}>
              Simulate an order against all active workflows without creating real data
            </div>
          </div>
          <div style={{ marginLeft: "auto", display: "flex", alignItems: "center", gap: 8 }}>
            <span style={{ fontSize: 11, color: "var(--text-muted)", fontFamily: "var(--font-mono)" }}>Tenant:</span>
            <input
              value={tenantId}
              onChange={(e) => setTenantId(e.target.value)}
              style={{
                background: "var(--input-bg)",
                border: "1px solid var(--border)",
                borderRadius: 3,
                color: "var(--accent)",
                fontFamily: "var(--font-mono)",
                fontSize: 13,
                padding: "6px 10px",
                width: 180,
                outline: "none",
              }}
              placeholder="tenant-id"
            />
          </div>
        </div>

        {/* Main layout */}
        <div style={{ display: "grid", gridTemplateColumns: "520px 1fr", gap: 20, alignItems: "start" }}>

          {/* LEFT — Form */}
          <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>

            {/* Channel & Payment */}
            <div style={{ background: "var(--surface)", border: "1px solid var(--border)", borderRadius: 4, padding: 16 }}>
              <SectionTitle>Order Context</SectionTitle>
              <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
                <div style={gridRow("1fr 1fr")}>
                  <Field label="Channel">
                    <Select value={form.channel} onChange={set("channel")} options={CHANNELS} />
                  </Field>
                  <Field label="Payment Status">
                    <Select value={form.payment_status} onChange={set("payment_status")} options={PAYMENT_STATUSES} />
                  </Field>
                </div>
                <div style={gridRow("1fr 1fr")}>
                  <Field label="Fulfilment Type">
                    <Select value={form.fulfilment_type} onChange={set("fulfilment_type")} options={FULFILMENT_TYPES} />
                  </Field>
                  <Field label="Currency">
                    <Select value={form.currency} onChange={set("currency")} options={CURRENCIES} />
                  </Field>
                </div>
                <Field label="Tags" hint="Comma-separated: fragile, priority, gift-wrap">
                  <Input value={form.tags} onChange={set("tags")} placeholder="fragile, priority" style={inputStyle} />
                </Field>
                <Field label="Promised Ship By" hint="Leave blank if no SLA date">
                  <Input value={form.promised_ship_by} onChange={set("promised_ship_by")} type="datetime-local" style={inputStyle} />
                </Field>
              </div>
            </div>

            {/* Shipping Address */}
            <div style={{ background: "var(--surface)", border: "1px solid var(--border)", borderRadius: 4, padding: 16 }}>
              <SectionTitle>Shipping Address</SectionTitle>
              <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
                <div style={gridRow("80px 1fr")}>
                  <Field label="Country" hint="ISO 2-letter">
                    <Input value={form.country} onChange={set("country")} placeholder="GB" style={inputStyle} />
                  </Field>
                  <Field label="Postcode">
                    <Input value={form.postal_code} onChange={set("postal_code")} placeholder="SW1A 1AA" style={inputStyle} />
                  </Field>
                </div>
                <div style={gridRow("1fr 1fr")}>
                  <Field label="City">
                    <Input value={form.city} onChange={set("city")} placeholder="London" style={inputStyle} />
                  </Field>
                  <Field label="State / County">
                    <Input value={form.state} onChange={set("state")} placeholder="England" style={inputStyle} />
                  </Field>
                </div>
              </div>
            </div>

            {/* Order Value */}
            <div style={{ background: "var(--surface)", border: "1px solid var(--border)", borderRadius: 4, padding: 16 }}>
              <SectionTitle>Order Value</SectionTitle>
              <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
                <div style={gridRow("1fr 1fr")}>
                  <Field label="Subtotal">
                    <Input
                      value={form.subtotal}
                      onChange={(v) => { set("subtotal")(v); }}
                      onBlur={autoTotal as unknown as undefined}
                      type="number"
                      placeholder="49.99"
                      style={inputStyle}
                    />
                  </Field>
                  <Field label="Shipping Cost">
                    <Input
                      value={form.shipping_cost}
                      onChange={(v) => { set("shipping_cost")(v); }}
                      type="number"
                      placeholder="3.99"
                      style={inputStyle}
                    />
                  </Field>
                </div>
                <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
                  <Field label="Grand Total">
                    <Input value={form.grand_total} onChange={set("grand_total")} type="number" placeholder="53.98" style={{ width: 160 }} />
                  </Field>
                  <button
                    className="ghost-btn"
                    style={{ marginTop: 20, flexShrink: 0 }}
                    onClick={autoTotal}
                  >
                    Auto-calc
                  </button>
                </div>
              </div>
            </div>

            {/* Line Items */}
            <div style={{ background: "var(--surface)", border: "1px solid var(--border)", borderRadius: 4, padding: 16 }}>
              <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: 12 }}>
                <div style={{ fontSize: 10, fontWeight: 800, letterSpacing: "0.15em", textTransform: "uppercase", color: "var(--accent)" }}>
                  Line Items
                </div>
                <button className="ghost-btn" onClick={addLine}>+ Add Line</button>
              </div>

              {form.lines.length === 0 && (
                <div style={{ color: "var(--text-muted)", fontSize: 12, fontStyle: "italic", padding: "8px 0" }}>
                  No lines — conditions based on SKU/item count won't match
                </div>
              )}

              <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
                {form.lines.map((line, idx) => (
                  <div key={line.line_id} style={{
                    background: "var(--input-bg)",
                    border: "1px solid var(--border-faint)",
                    borderRadius: 3,
                    padding: "10px 12px",
                  }}>
                    <div style={{ display: "flex", gap: 8, alignItems: "center", marginBottom: 8 }}>
                      <span style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--text-muted)" }}>#{idx + 1}</span>
                      <input
                        value={line.sku}
                        onChange={(e) => setLine(idx, "sku", e.target.value)}
                        placeholder="SKU"
                        style={{
                          flex: 1,
                          background: "var(--surface2)",
                          border: "1px solid var(--border)",
                          borderRadius: 3,
                          color: "var(--text)",
                          fontFamily: "var(--font-mono)",
                          fontSize: 12,
                          padding: "5px 8px",
                          outline: "none",
                        }}
                      />
                      <input
                        value={line.title}
                        onChange={(e) => setLine(idx, "title", e.target.value)}
                        placeholder="Title (optional)"
                        style={{
                          flex: 2,
                          background: "var(--surface2)",
                          border: "1px solid var(--border)",
                          borderRadius: 3,
                          color: "var(--text-dim)",
                          fontFamily: "var(--font-mono)",
                          fontSize: 12,
                          padding: "5px 8px",
                          outline: "none",
                        }}
                      />
                      <button className="del-btn" onClick={() => removeLine(idx)}>×</button>
                    </div>
                    <div style={{ display: "grid", gridTemplateColumns: "60px 90px 90px 1fr", gap: 8 }}>
                      <div>
                        <div style={{ fontSize: 10, color: "var(--text-muted)", marginBottom: 3 }}>QTY</div>
                        <input
                          type="number"
                          value={line.quantity}
                          onChange={(e) => setLine(idx, "quantity", parseInt(e.target.value) || 0)}
                          style={{
                            width: "100%",
                            background: "var(--surface2)",
                            border: "1px solid var(--border)",
                            borderRadius: 3,
                            color: "var(--text)",
                            fontFamily: "var(--font-mono)",
                            fontSize: 12,
                            padding: "5px 8px",
                            outline: "none",
                          }}
                        />
                      </div>
                      <div>
                        <div style={{ fontSize: 10, color: "var(--text-muted)", marginBottom: 3 }}>Unit Price</div>
                        <input
                          type="number"
                          value={line.unit_price.amount}
                          onChange={(e) => setLine(idx, "unit_price", { ...line.unit_price, amount: parseFloat(e.target.value) || 0 })}
                          style={{
                            width: "100%",
                            background: "var(--surface2)",
                            border: "1px solid var(--border)",
                            borderRadius: 3,
                            color: "var(--text)",
                            fontFamily: "var(--font-mono)",
                            fontSize: 12,
                            padding: "5px 8px",
                            outline: "none",
                          }}
                        />
                      </div>
                      <div>
                        <div style={{ fontSize: 10, color: "var(--text-muted)", marginBottom: 3 }}>Line Total</div>
                        <input
                          type="number"
                          value={line.line_total.amount}
                          onChange={(e) => setLine(idx, "line_total", { ...line.line_total, amount: parseFloat(e.target.value) || 0 })}
                          style={{
                            width: "100%",
                            background: "var(--surface2)",
                            border: "1px solid var(--border)",
                            borderRadius: 3,
                            color: "var(--text)",
                            fontFamily: "var(--font-mono)",
                            fontSize: 12,
                            padding: "5px 8px",
                            outline: "none",
                          }}
                        />
                      </div>
                      <div>
                        <div style={{ fontSize: 10, color: "var(--text-muted)", marginBottom: 3 }}>Fulfilment Type</div>
                        <select
                          value={line.fulfilment_type}
                          onChange={(e) => setLine(idx, "fulfilment_type", e.target.value)}
                          style={{
                            width: "100%",
                            background: "var(--surface2)",
                            border: "1px solid var(--border)",
                            borderRadius: 3,
                            color: "var(--text-dim)",
                            fontFamily: "var(--font-mono)",
                            fontSize: 12,
                            padding: "5px 8px",
                            outline: "none",
                          }}
                        >
                          {FULFILMENT_TYPES.map((o) => <option key={o} value={o}>{o}</option>)}
                        </select>
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            </div>

            {/* Submit */}
            <div style={{ display: "flex", gap: 10, alignItems: "center" }}>
              <button className="run-btn" onClick={handleSubmit} disabled={loading}>
                {loading ? "Running..." : "▶  Run Simulation"}
              </button>
              {response && (
                <span style={{ fontSize: 12, color: "var(--text-muted)", fontFamily: "var(--font-mono)" }}>
                  {response.total_workflows_evaluated} workflow{response.total_workflows_evaluated !== 1 ? "s" : ""} evaluated
                  {response.matched_workflow_name && (
                    <> · <span style={{ color: "var(--green)" }}>matched: {response.matched_workflow_name}</span></>
                  )}
                </span>
              )}
            </div>
          </div>

          {/* RIGHT — Results */}
          <div style={{ display: "flex", flexDirection: "column", gap: 12, position: "sticky", top: 24 }}>

            {!response && !error && !loading && (
              <div style={{
                background: "var(--surface)",
                border: "1px solid var(--border)",
                borderRadius: 4,
                padding: 40,
                display: "flex",
                flexDirection: "column",
                alignItems: "center",
                gap: 12,
                color: "var(--text-muted)",
              }}>
                <div style={{ fontSize: 40, opacity: 0.3 }}>⚡</div>
                <div style={{ fontSize: 13, fontFamily: "var(--font-mono)", textAlign: "center" }}>
                  Fill in the order details and click<br />Run Simulation to test your workflows
                </div>
              </div>
            )}

            {loading && (
              <div style={{
                background: "var(--surface)",
                border: "1px solid var(--border)",
                borderRadius: 4,
                padding: 40,
                display: "flex",
                alignItems: "center",
                justifyContent: "center",
                gap: 12,
                color: "var(--text-dim)",
              }}>
                <div style={{ width: 16, height: 16, borderRadius: "50%", background: "var(--accent)", animation: "pulse 1s infinite" }} />
                <span style={{ fontFamily: "var(--font-mono)", fontSize: 13 }}>Evaluating workflows...</span>
              </div>
            )}

            {error && (
              <div style={{
                background: "rgba(255,77,77,0.06)",
                border: "1px solid var(--red)",
                borderRadius: 4,
                padding: 16,
              }}>
                <div style={{ fontSize: 11, fontWeight: 700, letterSpacing: "0.1em", textTransform: "uppercase", color: "var(--red)", marginBottom: 6 }}>
                  Error
                </div>
                <div style={{ fontFamily: "var(--font-mono)", fontSize: 12, color: "var(--text-dim)", whiteSpace: "pre-wrap", wordBreak: "break-all" }}>
                  {error}
                </div>
              </div>
            )}

            {response && !loading && (
              <>
                {/* Summary banner */}
                <div style={{
                  background: response.matched_workflow ? "rgba(0,220,130,0.07)" : "rgba(255,77,77,0.07)",
                  border: `1px solid ${response.matched_workflow ? "var(--green)" : "var(--red)"}`,
                  borderRadius: 4,
                  padding: "12px 16px",
                  display: "flex",
                  alignItems: "center",
                  gap: 12,
                }}>
                  <span style={{ fontSize: 24 }}>{response.matched_workflow ? "✓" : "✗"}</span>
                  <div>
                    <div style={{ fontWeight: 700, fontSize: 14, color: response.matched_workflow ? "var(--green)" : "var(--red)" }}>
                      {response.matched_workflow
                        ? `Matched: ${response.matched_workflow_name}`
                        : "No workflow matched — order would use default fulfilment source"}
                    </div>
                    <div style={{ fontSize: 11, color: "var(--text-muted)", fontFamily: "var(--font-mono)", marginTop: 2 }}>
                      {response.total_workflows_evaluated} workflow{response.total_workflows_evaluated !== 1 ? "s" : ""} evaluated in priority order
                    </div>
                  </div>
                </div>

                {/* Workflow results */}
                <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
                  {(response.results ?? []).map((r, i) => (
                    <ResultCard key={r.workflow_id ?? i} r={r} index={i} />
                  ))}
                  {(!response.results || response.results.length === 0) && (
                    <div style={{
                      background: "var(--surface)",
                      border: "1px solid var(--border)",
                      borderRadius: 4,
                      padding: 24,
                      color: "var(--text-muted)",
                      fontSize: 13,
                      fontFamily: "var(--font-mono)",
                      textAlign: "center",
                    }}>
                      No active workflows found for this tenant
                    </div>
                  )}
                </div>

                {/* Raw JSON toggle */}
                <div style={{ display: "flex", gap: 8 }}>
                  <button className="ghost-btn" onClick={() => setRawJson(!rawJson)}>
                    {rawJson ? "Hide" : "Show"} Raw JSON
                  </button>
                  <button className="ghost-btn" onClick={() => {
                    navigator.clipboard.writeText(JSON.stringify(lastPayload, null, 2));
                  }}>
                    Copy Request Payload
                  </button>
                </div>

                {rawJson && (
                  <pre style={{
                    background: "var(--input-bg)",
                    border: "1px solid var(--border)",
                    borderRadius: 4,
                    padding: 14,
                    fontSize: 11,
                    fontFamily: "var(--font-mono)",
                    color: "var(--text-dim)",
                    overflowX: "auto",
                    maxHeight: 400,
                    overflowY: "auto",
                    lineHeight: 1.6,
                  }}>
                    {JSON.stringify(response, null, 2)}
                  </pre>
                )}
              </>
            )}
          </div>
        </div>
      </div>
    </>
  );
}

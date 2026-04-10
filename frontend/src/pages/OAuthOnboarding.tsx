import { useState, useEffect, useCallback } from "react";

// ─── Constants ───────────────────────────────────────────────────────────────
const API_BASE = "https://marketmate-api-487246736287.europe-west2.run.app/api/v1";

// OAuth-capable marketplaces with their login endpoints and metadata
const OAUTH_MARKETPLACES = [
  {
    id: "amazon",
    name: "Amazon",
    icon: "ri-amazon-fill",
    color: "#FF9900",
    loginPath: "/amazon/oauth/login",
    description: "SP-API via Login with Amazon",
    paramName: null,
  },
  {
    id: "ebay",
    name: "eBay",
    icon: "ri-auction-fill",
    color: "#0064D2",
    loginPath: "/ebay/oauth/login",
    description: "eBay OAuth 2.0",
    paramName: null,
  },
  {
    id: "tiktok",
    name: "TikTok Shop",
    icon: "ri-tiktok-fill",
    color: "#EE1D52",
    loginPath: "/tiktok/oauth/login",
    description: "TikTok Shop OAuth",
    paramName: null,
  },
  {
    id: "etsy",
    name: "Etsy",
    icon: "ri-store-2-fill",
    color: "#F56400",
    loginPath: "/etsy/oauth/login",
    description: "Etsy OAuth PKCE",
    paramName: null,
  },
  {
    id: "shopify",
    name: "Shopify",
    icon: "ri-shopping-cart-fill",
    color: "#96BF48",
    loginPath: "/shopify/oauth/login",
    description: "Shopify public app OAuth",
    paramName: "shop",
    paramLabel: "Store domain",
    paramPlaceholder: "mystore.myshopify.com",
  },
  {
    id: "shopline",
    name: "Shopline",
    icon: "ri-shopping-cart-2-fill",
    color: "#00BCD4",
    loginPath: "/shopline/oauth/login",
    description: "Shopline OAuth",
    paramName: "shop",
    paramLabel: "Store domain",
    paramPlaceholder: "mystore.myshopify.com",
  },
];

// ─── Types ────────────────────────────────────────────────────────────────────
interface Tenant {
  tenant_id: string;
  email: string;
  name?: string;
  company?: string;
  created_at?: string;
}

interface GeneratedLink {
  marketplace: string;
  url: string;
  tenantId: string;
  generatedAt: string;
}

// ─── Helper: copy to clipboard ────────────────────────────────────────────────
function copyToClipboard(text: string, onDone: () => void) {
  navigator.clipboard.writeText(text).then(onDone);
}

// ─── Sub-component: Tenant Card ───────────────────────────────────────────────
function TenantCard({
  tenant,
  selected,
  onClick,
}: {
  tenant: Tenant;
  selected: boolean;
  onClick: () => void;
}) {
  return (
    <button
      onClick={onClick}
      style={{
        width: "100%",
        textAlign: "left",
        background: selected ? "rgba(99,255,180,0.08)" : "rgba(255,255,255,0.03)",
        border: selected ? "1px solid rgba(99,255,180,0.5)" : "1px solid rgba(255,255,255,0.08)",
        borderRadius: 8,
        padding: "12px 14px",
        cursor: "pointer",
        transition: "all 0.15s",
        display: "flex",
        alignItems: "center",
        gap: 12,
      }}
    >
      <div
        style={{
          width: 36,
          height: 36,
          borderRadius: "50%",
          background: selected ? "rgba(99,255,180,0.2)" : "rgba(255,255,255,0.06)",
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          flexShrink: 0,
          fontSize: 14,
          color: selected ? "#63FFB4" : "#888",
          fontFamily: "monospace",
          fontWeight: 700,
        }}
      >
        {(tenant.email?.[0] || "?").toUpperCase()}
      </div>
      <div style={{ minWidth: 0 }}>
        <div style={{ fontSize: 13, color: selected ? "#63FFB4" : "#e0e0e0", fontWeight: 600, marginBottom: 2 }}>
          {tenant.email || "—"}
        </div>
        <div style={{ fontSize: 11, color: "#555", fontFamily: "monospace", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
          {tenant.tenant_id}
        </div>
      </div>
      {selected && (
        <div style={{ marginLeft: "auto", color: "#63FFB4", fontSize: 16, flexShrink: 0 }}>
          <i className="ri-check-line" />
        </div>
      )}
    </button>
  );
}

// ─── Sub-component: Marketplace Button ───────────────────────────────────────
function MarketplaceButton({
  marketplace,
  selected,
  onClick,
}: {
  marketplace: typeof OAUTH_MARKETPLACES[0];
  selected: boolean;
  onClick: () => void;
}) {
  return (
    <button
      onClick={onClick}
      style={{
        background: selected ? `${marketplace.color}18` : "rgba(255,255,255,0.03)",
        border: selected ? `1px solid ${marketplace.color}80` : "1px solid rgba(255,255,255,0.08)",
        borderRadius: 10,
        padding: "14px 16px",
        cursor: "pointer",
        transition: "all 0.15s",
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        gap: 8,
        width: "100%",
      }}
    >
      <i
        className={marketplace.icon}
        style={{
          fontSize: 24,
          color: selected ? marketplace.color : "#666",
          transition: "color 0.15s",
        }}
      />
      <span style={{ fontSize: 12, color: selected ? "#e0e0e0" : "#666", fontWeight: 600 }}>
        {marketplace.name}
      </span>
    </button>
  );
}

// ─── Main Component ───────────────────────────────────────────────────────────
export default function OAuthOnboarding() {
  const [tenants, setTenants] = useState<Tenant[]>([]);
  const [loadingTenants, setLoadingTenants] = useState(true);
  const [tenantsError, setTenantsError] = useState("");

  const [selectedTenant, setSelectedTenant] = useState<Tenant | null>(null);
  const [selectedMarketplace, setSelectedMarketplace] = useState<typeof OAUTH_MARKETPLACES[0] | null>(null);
  const [marketplaceParam, setMarketplaceParam] = useState("");

  const [generatedLink, setGeneratedLink] = useState<GeneratedLink | null>(null);
  const [copied, setCopied] = useState(false);
  const [linkHistory, setLinkHistory] = useState<GeneratedLink[]>([]);

  // Quick Register state
  const [showRegister, setShowRegister] = useState(false);
  const [regEmail, setRegEmail] = useState("");
  const [regPassword, setRegPassword] = useState("MarketMate2024!");
  const [regCompany, setRegCompany] = useState("");
  const [registering, setRegistering] = useState(false);
  const [registerResult, setRegisterResult] = useState<{ ok: boolean; message: string; tenantId?: string } | null>(null);

  // Tenant search filter
  const [tenantFilter, setTenantFilter] = useState("");

  // ── Load tenants ────────────────────────────────────────────────────────────
  const loadTenants = useCallback(async () => {
    setLoadingTenants(true);
    setTenantsError("");
    try {
      const res = await fetch(`${API_BASE}/tenants`);
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = await res.json();
      // API returns array directly or {tenants: [...]}
      const list: Tenant[] = Array.isArray(data) ? data : (data.tenants ?? []);
      // Sort by created_at descending (newest first)
      list.sort((a, b) => {
        const da = a.created_at || "";
        const db = b.created_at || "";
        return db.localeCompare(da);
      });
      setTenants(list);
    } catch (e: unknown) {
      setTenantsError(e instanceof Error ? e.message : "Failed to load tenants");
    } finally {
      setLoadingTenants(false);
    }
  }, []);

  useEffect(() => {
    loadTenants();
  }, [loadTenants]);

  // ── Generate OAuth link ─────────────────────────────────────────────────────
  function generateLink() {
    if (!selectedTenant || !selectedMarketplace) return;

    let url = `${API_BASE}${selectedMarketplace.loginPath}?tenant_id=${selectedTenant.tenant_id}`;

    if (selectedMarketplace.paramName && marketplaceParam.trim()) {
      url += `&${selectedMarketplace.paramName}=${encodeURIComponent(marketplaceParam.trim())}`;
    }

    const link: GeneratedLink = {
      marketplace: selectedMarketplace.name,
      url,
      tenantId: selectedTenant.tenant_id,
      generatedAt: new Date().toISOString(),
    };

    setGeneratedLink(link);
    setLinkHistory((prev) => [link, ...prev.slice(0, 9)]);
  }

  // ── Quick Register ──────────────────────────────────────────────────────────
  async function handleRegister() {
    if (!regEmail || !regPassword) return;
    setRegistering(true);
    setRegisterResult(null);
    try {
      const res = await fetch(`${API_BASE}/auth/register`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          email: regEmail,
          password: regPassword,
          company_name: regCompany || regEmail.split("@")[0],
        }),
      });
      const data = await res.json();
      if (!res.ok) throw new Error(data.error || data.message || `HTTP ${res.status}`);
      setRegisterResult({ ok: true, message: "Account created successfully", tenantId: data.tenant_id });
      setRegEmail("");
      setRegCompany("");
      // Reload tenant list
      await loadTenants();
    } catch (e: unknown) {
      setRegisterResult({ ok: false, message: e instanceof Error ? e.message : "Registration failed" });
    } finally {
      setRegistering(false);
    }
  }

  // ── Filtered tenants ────────────────────────────────────────────────────────
  const filteredTenants = tenants.filter((t) => {
    const q = tenantFilter.toLowerCase();
    return (
      !q ||
      t.email?.toLowerCase().includes(q) ||
      t.tenant_id?.toLowerCase().includes(q) ||
      t.company?.toLowerCase().includes(q)
    );
  });

  const canGenerate =
    selectedTenant &&
    selectedMarketplace &&
    (!selectedMarketplace.paramName || marketplaceParam.trim());

  // ─── Render ─────────────────────────────────────────────────────────────────
  return (
    <div
      style={{
        minHeight: "100vh",
        background: "#0a0a0f",
        color: "#e0e0e0",
        fontFamily: "'DM Mono', 'Fira Code', 'Cascadia Code', monospace",
        padding: "32px 24px",
      }}
    >
      {/* Header */}
      <div style={{ marginBottom: 32 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 6 }}>
          <div
            style={{
              width: 8,
              height: 8,
              borderRadius: "50%",
              background: "#63FFB4",
              boxShadow: "0 0 8px #63FFB4",
            }}
          />
          <span style={{ fontSize: 11, color: "#63FFB4", letterSpacing: "0.15em", textTransform: "uppercase" }}>
            Dev Tools / OAuth Onboarding
          </span>
        </div>
        <h1
          style={{
            margin: 0,
            fontSize: 28,
            fontWeight: 700,
            color: "#fff",
            letterSpacing: "-0.5px",
            fontFamily: "'DM Mono', monospace",
          }}
        >
          OAuth Link Generator
        </h1>
        <p style={{ margin: "6px 0 0", fontSize: 13, color: "#555", fontFamily: "inherit" }}>
          Select a tenant and marketplace to generate a shareable OAuth authorisation link.
        </p>
      </div>

      {/* Main layout */}
      <div style={{ display: "grid", gridTemplateColumns: "340px 1fr", gap: 20, maxWidth: 1100 }}>

        {/* ── Left: Tenant Panel ── */}
        <div>
          {/* Tenant selector */}
          <div
            style={{
              background: "rgba(255,255,255,0.03)",
              border: "1px solid rgba(255,255,255,0.08)",
              borderRadius: 12,
              padding: 16,
              marginBottom: 12,
            }}
          >
            <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: 12 }}>
              <span style={{ fontSize: 11, color: "#63FFB4", letterSpacing: "0.12em", textTransform: "uppercase" }}>
                Step 1 — Select Tenant
              </span>
              <button
                onClick={loadTenants}
                disabled={loadingTenants}
                style={{
                  background: "none",
                  border: "none",
                  color: "#555",
                  cursor: "pointer",
                  padding: 4,
                  fontSize: 14,
                }}
                title="Refresh"
              >
                <i className={`ri-refresh-line ${loadingTenants ? "ri-spin" : ""}`} />
              </button>
            </div>

            {/* Filter */}
            <input
              type="text"
              value={tenantFilter}
              onChange={(e) => setTenantFilter(e.target.value)}
              placeholder="Filter by email or tenant ID…"
              style={{
                width: "100%",
                background: "rgba(255,255,255,0.05)",
                border: "1px solid rgba(255,255,255,0.1)",
                borderRadius: 6,
                padding: "8px 10px",
                color: "#e0e0e0",
                fontSize: 12,
                fontFamily: "inherit",
                marginBottom: 10,
                boxSizing: "border-box",
              }}
            />

            {/* Tenant list */}
            <div style={{ display: "flex", flexDirection: "column", gap: 6, maxHeight: 320, overflowY: "auto" }}>
              {loadingTenants ? (
                <div style={{ textAlign: "center", padding: 24, color: "#555", fontSize: 12 }}>
                  <i className="ri-loader-4-line ri-spin" /> Loading…
                </div>
              ) : tenantsError ? (
                <div style={{ padding: 12, background: "rgba(255,80,80,0.1)", border: "1px solid rgba(255,80,80,0.2)", borderRadius: 6, fontSize: 12, color: "#ff8080" }}>
                  {tenantsError}
                </div>
              ) : filteredTenants.length === 0 ? (
                <div style={{ textAlign: "center", padding: 24, color: "#444", fontSize: 12 }}>
                  {tenantFilter ? "No tenants match filter" : "No tenants found"}
                </div>
              ) : (
                filteredTenants.map((t) => (
                  <TenantCard
                    key={t.tenant_id}
                    tenant={t}
                    selected={selectedTenant?.tenant_id === t.tenant_id}
                    onClick={() => {
                      setSelectedTenant(t);
                      setGeneratedLink(null);
                    }}
                  />
                ))
              )}
            </div>

            <div style={{ marginTop: 10, fontSize: 11, color: "#444" }}>
              {filteredTenants.length} tenant{filteredTenants.length !== 1 ? "s" : ""}
            </div>
          </div>

          {/* Quick Register */}
          <div
            style={{
              background: "rgba(255,255,255,0.02)",
              border: "1px solid rgba(255,255,255,0.07)",
              borderRadius: 12,
              overflow: "hidden",
            }}
          >
            <button
              onClick={() => setShowRegister((v) => !v)}
              style={{
                width: "100%",
                background: "none",
                border: "none",
                padding: "12px 16px",
                display: "flex",
                alignItems: "center",
                justifyContent: "space-between",
                cursor: "pointer",
                color: "#888",
                fontSize: 12,
                fontFamily: "inherit",
              }}
            >
              <span style={{ display: "flex", alignItems: "center", gap: 8 }}>
                <i className="ri-user-add-line" style={{ color: "#63FFB4" }} />
                Quick Create Tenant
              </span>
              <i className={showRegister ? "ri-arrow-up-s-line" : "ri-arrow-down-s-line"} />
            </button>

            {showRegister && (
              <div style={{ padding: "0 16px 16px", display: "flex", flexDirection: "column", gap: 8 }}>
                <input
                  type="email"
                  value={regEmail}
                  onChange={(e) => setRegEmail(e.target.value)}
                  placeholder="Email address *"
                  style={inputStyle}
                />
                <input
                  type="text"
                  value={regCompany}
                  onChange={(e) => setRegCompany(e.target.value)}
                  placeholder="Company name (optional)"
                  style={inputStyle}
                />
                <input
                  type="text"
                  value={regPassword}
                  onChange={(e) => setRegPassword(e.target.value)}
                  placeholder="Temporary password *"
                  style={inputStyle}
                />
                <button
                  onClick={handleRegister}
                  disabled={registering || !regEmail || !regPassword}
                  style={{
                    background: registering || !regEmail || !regPassword
                      ? "rgba(99,255,180,0.1)"
                      : "rgba(99,255,180,0.2)",
                    border: "1px solid rgba(99,255,180,0.4)",
                    borderRadius: 6,
                    padding: "9px 14px",
                    color: registering || !regEmail ? "#444" : "#63FFB4",
                    fontSize: 12,
                    fontFamily: "inherit",
                    cursor: registering || !regEmail ? "not-allowed" : "pointer",
                    fontWeight: 600,
                    letterSpacing: "0.05em",
                  }}
                >
                  {registering ? (
                    <><i className="ri-loader-4-line ri-spin" /> Registering…</>
                  ) : (
                    <><i className="ri-user-add-line" /> Register</>
                  )}
                </button>

                {registerResult && (
                  <div
                    style={{
                      padding: "8px 10px",
                      borderRadius: 6,
                      fontSize: 12,
                      background: registerResult.ok ? "rgba(99,255,180,0.08)" : "rgba(255,80,80,0.08)",
                      border: `1px solid ${registerResult.ok ? "rgba(99,255,180,0.2)" : "rgba(255,80,80,0.2)"}`,
                      color: registerResult.ok ? "#63FFB4" : "#ff8080",
                    }}
                  >
                    {registerResult.message}
                    {registerResult.tenantId && (
                      <div style={{ marginTop: 4, fontSize: 11, opacity: 0.7 }}>
                        ID: {registerResult.tenantId}
                      </div>
                    )}
                  </div>
                )}
              </div>
            )}
          </div>
        </div>

        {/* ── Right: Marketplace + Link Panel ── */}
        <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>

          {/* Marketplace picker */}
          <div
            style={{
              background: "rgba(255,255,255,0.03)",
              border: "1px solid rgba(255,255,255,0.08)",
              borderRadius: 12,
              padding: 16,
            }}
          >
            <div style={{ fontSize: 11, color: "#63FFB4", letterSpacing: "0.12em", textTransform: "uppercase", marginBottom: 14 }}>
              Step 2 — Select Marketplace
            </div>
            <div
              style={{
                display: "grid",
                gridTemplateColumns: "repeat(6, 1fr)",
                gap: 10,
              }}
            >
              {OAUTH_MARKETPLACES.map((mp) => (
                <MarketplaceButton
                  key={mp.id}
                  marketplace={mp}
                  selected={selectedMarketplace?.id === mp.id}
                  onClick={() => {
                    setSelectedMarketplace(mp);
                    setMarketplaceParam("");
                    setGeneratedLink(null);
                  }}
                />
              ))}
            </div>

            {/* Extra param input (e.g. Shopify store domain) */}
            {selectedMarketplace?.paramName && (
              <div style={{ marginTop: 14 }}>
                <label style={{ fontSize: 11, color: "#888", display: "block", marginBottom: 6 }}>
                  {selectedMarketplace.paramLabel} *
                </label>
                <input
                  type="text"
                  value={marketplaceParam}
                  onChange={(e) => {
                    setMarketplaceParam(e.target.value);
                    setGeneratedLink(null);
                  }}
                  placeholder={selectedMarketplace.paramPlaceholder}
                  style={{ ...inputStyle, width: "100%", boxSizing: "border-box" }}
                />
              </div>
            )}

            {selectedMarketplace && (
              <div style={{ marginTop: 10, fontSize: 11, color: "#444" }}>
                {selectedMarketplace.description}
              </div>
            )}
          </div>

          {/* Selected tenant summary */}
          {selectedTenant && (
            <div
              style={{
                background: "rgba(99,255,180,0.04)",
                border: "1px solid rgba(99,255,180,0.15)",
                borderRadius: 10,
                padding: "10px 14px",
                display: "flex",
                alignItems: "center",
                gap: 12,
                fontSize: 12,
              }}
            >
              <i className="ri-user-line" style={{ color: "#63FFB4", fontSize: 16 }} />
              <div>
                <span style={{ color: "#63FFB4", fontWeight: 600 }}>{selectedTenant.email}</span>
                <span style={{ color: "#444", marginLeft: 10 }}>{selectedTenant.tenant_id}</span>
              </div>
            </div>
          )}

          {/* Generate button */}
          <button
            onClick={generateLink}
            disabled={!canGenerate}
            style={{
              background: canGenerate
                ? "linear-gradient(135deg, rgba(99,255,180,0.25) 0%, rgba(99,255,180,0.1) 100%)"
                : "rgba(255,255,255,0.03)",
              border: canGenerate
                ? "1px solid rgba(99,255,180,0.5)"
                : "1px solid rgba(255,255,255,0.07)",
              borderRadius: 10,
              padding: "14px 20px",
              color: canGenerate ? "#63FFB4" : "#333",
              fontSize: 13,
              fontFamily: "inherit",
              fontWeight: 700,
              letterSpacing: "0.08em",
              cursor: canGenerate ? "pointer" : "not-allowed",
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              gap: 10,
              transition: "all 0.15s",
              textTransform: "uppercase",
            }}
          >
            <i className="ri-link" />
            Generate OAuth Link
          </button>

          {/* Generated link output */}
          {generatedLink && (
            <div
              style={{
                background: "rgba(99,255,180,0.05)",
                border: "1px solid rgba(99,255,180,0.3)",
                borderRadius: 12,
                padding: 16,
              }}
            >
              <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: 10 }}>
                <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                  <div style={{ width: 6, height: 6, borderRadius: "50%", background: "#63FFB4", boxShadow: "0 0 6px #63FFB4" }} />
                  <span style={{ fontSize: 11, color: "#63FFB4", letterSpacing: "0.12em", textTransform: "uppercase" }}>
                    {generatedLink.marketplace} OAuth Link Ready
                  </span>
                </div>
                <span style={{ fontSize: 10, color: "#444" }}>
                  {new Date(generatedLink.generatedAt).toLocaleTimeString()}
                </span>
              </div>

              {/* URL display */}
              <div
                style={{
                  background: "#0d0d14",
                  border: "1px solid rgba(255,255,255,0.06)",
                  borderRadius: 8,
                  padding: "10px 12px",
                  fontFamily: "monospace",
                  fontSize: 12,
                  color: "#a0d4ff",
                  wordBreak: "break-all",
                  lineHeight: 1.6,
                  marginBottom: 12,
                }}
              >
                {generatedLink.url}
              </div>

              {/* Action buttons */}
              <div style={{ display: "flex", gap: 8 }}>
                <button
                  onClick={() =>
                    copyToClipboard(generatedLink.url, () => {
                      setCopied(true);
                      setTimeout(() => setCopied(false), 2000);
                    })
                  }
                  style={{
                    flex: 1,
                    background: copied ? "rgba(99,255,180,0.15)" : "rgba(255,255,255,0.05)",
                    border: `1px solid ${copied ? "rgba(99,255,180,0.4)" : "rgba(255,255,255,0.1)"}`,
                    borderRadius: 7,
                    padding: "9px 14px",
                    color: copied ? "#63FFB4" : "#aaa",
                    fontSize: 12,
                    fontFamily: "inherit",
                    cursor: "pointer",
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "center",
                    gap: 6,
                    transition: "all 0.15s",
                  }}
                >
                  <i className={copied ? "ri-check-line" : "ri-clipboard-line"} />
                  {copied ? "Copied!" : "Copy Link"}
                </button>

                <button
                  onClick={() => {
                    const subject = encodeURIComponent(`Connect your ${generatedLink.marketplace} account to MarketMate`);
                    const body = encodeURIComponent(
                      `Hi,\n\nPlease click the link below to connect your ${generatedLink.marketplace} account to MarketMate:\n\n${generatedLink.url}\n\nThis link will take you through the ${generatedLink.marketplace} authorisation flow. Once complete, your account will be connected and ready to use.\n\nIf you have any questions, please don't hesitate to get in touch.\n\nThanks`
                    );
                    window.open(`mailto:?subject=${subject}&body=${body}`);
                  }}
                  style={{
                    background: "rgba(160,212,255,0.05)",
                    border: "1px solid rgba(160,212,255,0.15)",
                    borderRadius: 7,
                    padding: "9px 14px",
                    color: "#a0d4ff",
                    fontSize: 12,
                    fontFamily: "inherit",
                    cursor: "pointer",
                    display: "flex",
                    alignItems: "center",
                    gap: 6,
                  }}
                >
                  <i className="ri-mail-send-line" />
                  Open in Mail
                </button>

                <button
                  onClick={() => {
                    const sms = encodeURIComponent(`Connect your ${generatedLink.marketplace} account to MarketMate: ${generatedLink.url}`);
                    window.open(`sms:?body=${sms}`);
                  }}
                  style={{
                    background: "rgba(160,212,255,0.05)",
                    border: "1px solid rgba(160,212,255,0.15)",
                    borderRadius: 7,
                    padding: "9px 14px",
                    color: "#a0d4ff",
                    fontSize: 12,
                    fontFamily: "inherit",
                    cursor: "pointer",
                    display: "flex",
                    alignItems: "center",
                    gap: 6,
                  }}
                >
                  <i className="ri-message-2-line" />
                  SMS
                </button>

                <a
                  href={generatedLink.url}
                  target="_blank"
                  rel="noopener noreferrer"
                  style={{
                    background: "rgba(160,212,255,0.05)",
                    border: "1px solid rgba(160,212,255,0.15)",
                    borderRadius: 7,
                    padding: "9px 14px",
                    color: "#a0d4ff",
                    fontSize: 12,
                    fontFamily: "inherit",
                    cursor: "pointer",
                    display: "flex",
                    alignItems: "center",
                    gap: 6,
                    textDecoration: "none",
                  }}
                  title="Test link"
                >
                  <i className="ri-external-link-line" />
                  Test
                </a>
              </div>
            </div>
          )}

          {/* Recent link history */}
          {linkHistory.length > 1 && (
            <div
              style={{
                background: "rgba(255,255,255,0.02)",
                border: "1px solid rgba(255,255,255,0.06)",
                borderRadius: 12,
                padding: 14,
              }}
            >
              <div style={{ fontSize: 11, color: "#555", letterSpacing: "0.1em", textTransform: "uppercase", marginBottom: 10 }}>
                Recent Links
              </div>
              <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
                {linkHistory.slice(1).map((link, i) => (
                  <div
                    key={i}
                    style={{
                      display: "flex",
                      alignItems: "center",
                      gap: 10,
                      padding: "7px 10px",
                      background: "rgba(255,255,255,0.02)",
                      borderRadius: 6,
                    }}
                  >
                    <span style={{ fontSize: 11, color: "#888", minWidth: 70 }}>{link.marketplace}</span>
                    <span style={{ fontSize: 11, color: "#444", flex: 1, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                      {link.url}
                    </span>
                    <button
                      onClick={() => copyToClipboard(link.url, () => {})}
                      style={{ background: "none", border: "none", color: "#555", cursor: "pointer", padding: 4, fontSize: 13 }}
                    >
                      <i className="ri-clipboard-line" />
                    </button>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Instructions */}
          {!selectedTenant && !selectedMarketplace && (
            <div
              style={{
                background: "rgba(255,255,255,0.02)",
                border: "1px solid rgba(255,255,255,0.06)",
                borderRadius: 12,
                padding: 20,
                color: "#444",
                fontSize: 13,
                lineHeight: 1.8,
              }}
            >
              <div style={{ marginBottom: 12, color: "#555", fontSize: 12, textTransform: "uppercase", letterSpacing: "0.1em" }}>How to use</div>
              <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
                {[
                  ["1", "Select an existing tenant from the list, or use Quick Create to register a new account."],
                  ["2", "Choose a marketplace. Only OAuth-capable channels are listed here."],
                  ["3", "Click Generate to produce a shareable URL."],
                  ["4", "Copy, email, or SMS the link directly to your merchant. They complete OAuth on their own device."],
                ].map(([num, text]) => (
                  <div key={num} style={{ display: "flex", gap: 12, alignItems: "flex-start" }}>
                    <span style={{ color: "#63FFB4", fontWeight: 700, flexShrink: 0 }}>{num}.</span>
                    <span>{text}</span>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

// ─── Shared input style ───────────────────────────────────────────────────────
const inputStyle: React.CSSProperties = {
  background: "rgba(255,255,255,0.05)",
  border: "1px solid rgba(255,255,255,0.1)",
  borderRadius: 6,
  padding: "8px 10px",
  color: "#e0e0e0",
  fontSize: 12,
  fontFamily: "'DM Mono', 'Fira Code', monospace",
  width: "100%",
  boxSizing: "border-box",
  outline: "none",
};

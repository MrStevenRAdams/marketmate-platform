// ============================================================================
// CONFIGURATOR DETAIL PAGE — SESSION 1 (CFG-01, CFG-02, CFG-03)
// ============================================================================
// Routes:
//   /marketplace/configurators/new    — create new configurator
//   /marketplace/configurators/:id    — edit existing
//
// Tabs: General | Attributes | Variations | Linked Listings
// Includes inline Revise dialog (bulk push fields to linked listings).

import { useState, useEffect } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import {
  configuratorService,
  Configurator,
  ConfiguratorDetail,
  AttributeDefault,
  LinkedListing,
  ReviseField,
  ReviseJob,
} from '../../services/configurator-api';
import { credentialService } from '../../services/marketplace-api';
import { temuApi, TemuShippingTemplate } from '../../services/temu-api';

// ── Types ────────────────────────────────────────────────────────────────────

type Tab = 'general' | 'attributes' | 'variations' | 'linked';

// ── Channel list ─────────────────────────────────────────────────────────────

const ALL_CHANNELS = [
  'amazon', 'ebay', 'shopify', 'bigcommerce', 'magento', 'woocommerce',
  'etsy', 'walmart', 'tiktok', 'onbuy', 'kaufland', 'temu', 'mirakl', 'tesco',
];

const channelEmoji: Record<string, string> = {
  amazon: '📦', ebay: '🏷️', shopify: '🛒', bigcommerce: '🛍️',
  magento: '🔶', woocommerce: '🟣', etsy: '🧶', walmart: '🟡',
  tiktok: '🎵', onbuy: '🔵', kaufland: '🟠', temu: '🛍️',
  mirakl: '⚡', tesco: '🏪',
};

// ── Revise fields config ──────────────────────────────────────────────────────

const REVISE_FIELDS: { key: ReviseField; label: string; description: string }[] = [
  { key: 'title', label: 'Titles', description: 'Push title overrides to linked listings' },
  { key: 'description', label: 'Descriptions', description: 'Push description overrides' },
  { key: 'price', label: 'Prices', description: 'Push price overrides' },
  { key: 'attributes', label: 'Attributes', description: 'Push attribute default values' },
  { key: 'images', label: 'Images', description: 'Push image overrides' },
  { key: 'category', label: 'Category', description: 'Push category mapping' },
  { key: 'shipping', label: 'Shipping', description: 'Push shipping defaults' },
];

// ── Shared styles ─────────────────────────────────────────────────────────────

const labelStyle: React.CSSProperties = {
  display: 'block', fontSize: 11, fontWeight: 700, color: 'var(--text-muted)',
  textTransform: 'uppercase', letterSpacing: '0.5px', marginBottom: 6,
};
const inputStyle: React.CSSProperties = {
  width: '100%', padding: '10px 14px', borderRadius: 8,
  background: 'var(--bg-primary)', border: '1px solid var(--border-bright)',
  color: 'var(--text-primary)', fontSize: 14, outline: 'none',
};
const cardStyle: React.CSSProperties = {
  background: 'var(--bg-secondary)', borderRadius: 12, border: '1px solid var(--border)',
  padding: 24, marginBottom: 20,
};
const sectionTitle: React.CSSProperties = {
  fontSize: 15, fontWeight: 700, color: 'var(--text-primary)',
  marginBottom: 16, paddingBottom: 10, borderBottom: '1px solid var(--border)',
};

// ============================================================================
// COMPONENT
// ============================================================================

export default function ConfiguratorDetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const isNew = id === 'new';

  // ── Loading / error ─────────────────────────────────────────────────────
  const [loading, setLoading] = useState(!isNew);
  const [loadError, setLoadError] = useState('');

  // ── Active tab ──────────────────────────────────────────────────────────
  const [activeTab, setActiveTab] = useState<Tab>('general');

  // ── Form state — mirrors Configurator model ─────────────────────────────
  const [name, setName] = useState('');
  const [channel, setChannel] = useState('amazon');
  const [credentialId, setCredentialId] = useState('');
  const [categoryId, setCategoryId] = useState('');
  const [categoryPath, setCategoryPath] = useState('');
  const [shippingDefaults, setShippingDefaults] = useState<{ key: string; value: string }[]>([]);
  const [attributeDefaults, setAttributeDefaults] = useState<AttributeDefault[]>([]);
  const [variationSchema, setVariationSchema] = useState<string[]>([]);

  // ── Temu-specific defaults ───────────────────────────────────────────────
  const [temuShippingTemplates, setTemuShippingTemplates] = useState<TemuShippingTemplate[]>([]);
  const [temuCatModalOpen, setTemuCatModalOpen] = useState(false);
  const [temuCatLevels, setTemuCatLevels] = useState<any[][]>([]);
  const [temuCatSelections, setTemuCatSelections] = useState<any[]>([]);
  const [temuCatLoading, setTemuCatLoading] = useState(false);

  // ── Credentials for the selected channel ───────────────────────────────
  const [allCredentials, setAllCredentials] = useState<any[]>([]);
  const channelCredentials = Array.isArray(allCredentials) ? allCredentials.filter(c => c.channel === channel) : [];

  // ── Linked listings ─────────────────────────────────────────────────────
  const [linkedListings, setLinkedListings] = useState<LinkedListing[]>([]);

  // ── Save state ──────────────────────────────────────────────────────────
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState('');
  const [saveSuccess, setSaveSuccess] = useState(false);

  // ── Duplicate state (CFG-08) ────────────────────────────────────────────
  const [duplicating, setDuplicating] = useState(false);
  const [duplicateError, setDuplicateError] = useState('');

  // ── Revise dialog ───────────────────────────────────────────────────────
  const [reviseOpen, setReviseOpen] = useState(false);
  const [reviseFields, setReviseFields] = useState<ReviseField[]>([]);
  const [revising, setRevising] = useState(false);
  const [reviseJob, setReviseJob] = useState<ReviseJob | null>(null);
  const [reviseError, setReviseError] = useState('');

  // ── Add listing modal ───────────────────────────────────────────────────
  const [addListingOpen, setAddListingOpen] = useState(false);
  const [listingSearchInput, setListingSearchInput] = useState('');
  const [addingListing, setAddingListing] = useState(false);

  // ── Remove listing ──────────────────────────────────────────────────────
  const [removingListing, setRemovingListing] = useState<string | null>(null);

  // ── Load on mount ────────────────────────────────────────────────────────
  useEffect(() => {
    loadCredentials();
    if (!isNew && id) loadDetail(id);
  }, [id]);

  useEffect(() => {
    if (channel === 'temu') {
      temuApi.getShippingTemplates(credentialId || undefined).then(res => {
        if (res.data?.ok) setTemuShippingTemplates(res.data.templates || []);
      }).catch(() => {});
    }
  }, [channel, credentialId]);

  async function openTemuCategoryPicker() {
    setTemuCatModalOpen(true);
    if (temuCatLevels.length === 0) {
      setTemuCatLoading(true);
      try {
        const res = await temuApi.getCategories(undefined, credentialId || undefined);
        if (res.data?.ok) { setTemuCatLevels([res.data.items || []]); setTemuCatSelections([null]); }
      } catch { /* ignore */ }
      setTemuCatLoading(false);
    }
  }

  async function handleTemuCatSelect(levelIndex: number, catId: string) {
    const cats = temuCatLevels[levelIndex] || [];
    const cat = cats.find((c: any) => String(c.catId) === catId);
    if (!cat) return;
    const newSel = temuCatSelections.slice(0, levelIndex);
    newSel[levelIndex] = cat;
    setTemuCatSelections(newSel);
    if (cat.leaf) {
      setTemuCatModalOpen(false);
      // Fetch full path
      try {
        const pathRes = await temuApi.getCategoryPath(cat.catId);
        if (pathRes.data?.ok && pathRes.data.path.length > 0) {
          setCategoryId(String(cat.catId));
          setCategoryPath(pathRes.data.path.join(' › '));
        } else {
          setCategoryId(String(cat.catId));
          setCategoryPath(cat.catName || String(cat.catId));
        }
      } catch {
        setCategoryId(String(cat.catId));
        setCategoryPath(cat.catName || String(cat.catId));
      }
    } else {
      setTemuCatLoading(true);
      const newLevels = temuCatLevels.slice(0, levelIndex + 1);
      try {
        const res = await temuApi.getCategories(cat.catId, credentialId || undefined);
        setTemuCatLevels([...newLevels, res.data?.items || []]);
        setTemuCatSelections([...newSel, null]);
      } catch { setTemuCatLevels([...newLevels, []]); }
      setTemuCatLoading(false);
    }
  }

  async function loadCredentials() {
    try {
      const res = await credentialService.list();
      const creds = res.data?.credentials || res.data?.data || res.data || [];
      setAllCredentials(Array.isArray(creds) ? creds : []);
    } catch { /* non-fatal */ }
  }

  async function loadDetail(configuratorId: string) {
    setLoading(true);
    setLoadError('');
    try {
      const res = await configuratorService.get(configuratorId);
      const cfg: ConfiguratorDetail = res.data.configurator;
      setName(cfg.name || '');
      setChannel(cfg.channel || 'amazon');
      setCredentialId(cfg.channel_credential_id || '');
      setCategoryId(cfg.category_id || '');
      setCategoryPath(cfg.category_path || '');
      // Shipping defaults — convert map to row array for editing
      const sdRows = Object.entries(cfg.shipping_defaults || {}).map(([key, value]) => ({
        key, value: String(value),
      }));
      setShippingDefaults(sdRows);
      setAttributeDefaults(cfg.attribute_defaults || []);
      setVariationSchema(cfg.variation_schema || []);
      setLinkedListings(Array.isArray(cfg.linked_listings) ? cfg.linked_listings : []);
    } catch (err: any) {
      setLoadError(err?.response?.data?.error || err.message || 'Failed to load configurator');
    } finally {
      setLoading(false);
    }
  }

  // ── Save ─────────────────────────────────────────────────────────────────
  async function handleSave() {
    if (!name.trim()) { setSaveError('Name is required'); return; }
    setSaving(true);
    setSaveError('');
    setSaveSuccess(false);

    // Convert shipping rows back to map
    const shippingMap: Record<string, string> = {};
    for (const row of shippingDefaults) {
      if (row.key.trim()) shippingMap[row.key.trim()] = row.value;
    }

    const payload: Partial<Configurator> = {
      name: name.trim(),
      channel,
      channel_credential_id: credentialId || undefined,
      category_id: categoryId || undefined,
      category_path: categoryPath || undefined,
      shipping_defaults: Object.keys(shippingMap).length > 0 ? shippingMap : undefined,
      attribute_defaults: attributeDefaults.length > 0 ? attributeDefaults : undefined,
      variation_schema: variationSchema.filter(s => s.trim()).length > 0
        ? variationSchema.filter(s => s.trim())
        : undefined,
    };

    try {
      if (isNew) {
        const res = await configuratorService.create(payload);
        const newId = res.data.configurator.configurator_id;
        setSaveSuccess(true);
        navigate(`/marketplace/configurators/${newId}`, { replace: true });
      } else {
        await configuratorService.update(id!, payload);
        setSaveSuccess(true);
        setTimeout(() => setSaveSuccess(false), 3000);
      }
    } catch (err: any) {
      setSaveError(err?.response?.data?.error || err.message || 'Save failed');
    } finally {
      setSaving(false);
    }
  }

  // ── Duplicate (CFG-08) ───────────────────────────────────────────────────
  async function handleDuplicate() {
    if (!id) return;
    setDuplicating(true);
    setDuplicateError('');
    try {
      const res = await configuratorService.duplicate(id);
      const newId = res.data.configurator?.configurator_id || res.data.configurator_id;
      navigate(`/marketplace/configurators/${newId}`);
    } catch (err: any) {
      setDuplicateError(err?.response?.data?.error || err.message || 'Duplicate failed');
    } finally {
      setDuplicating(false);
    }
  }

  // ── Revise ───────────────────────────────────────────────────────────────
  async function handleRevise() {
    if (reviseFields.length === 0) { setReviseError('Select at least one field'); return; }
    setRevising(true);
    setReviseError('');
    setReviseJob(null);
    try {
      const res = await configuratorService.revise(id!, reviseFields);
      setReviseJob(res.data.job);
    } catch (err: any) {
      setReviseError(err?.response?.data?.error || err.message || 'Revise failed');
    } finally {
      setRevising(false);
    }
  }

  function toggleReviseField(field: ReviseField) {
    setReviseFields(prev =>
      prev.includes(field) ? prev.filter(f => f !== field) : [...prev, field],
    );
  }

  function openRevise() {
    setReviseFields([]);
    setReviseJob(null);
    setReviseError('');
    setReviseOpen(true);
  }

  // ── Remove linked listing ─────────────────────────────────────────────────
  async function handleRemoveListing(listingId: string) {
    setRemovingListing(listingId);
    try {
      await configuratorService.removeListings(id!, [listingId]);
      setLinkedListings(prev => prev.filter(l => l.listing_id !== listingId));
    } catch (err: any) {
      alert(err?.response?.data?.error || 'Failed to remove listing');
    } finally {
      setRemovingListing(null);
    }
  }

  // ── Add listing by ID ─────────────────────────────────────────────────────
  async function handleAddListing() {
    const ids = listingSearchInput.split(/[\s,]+/).map(s => s.trim()).filter(Boolean);
    if (ids.length === 0) return;
    setAddingListing(true);
    try {
      await configuratorService.assignListings(id!, ids);
      setListingSearchInput('');
      setAddListingOpen(false);
      if (!isNew) await loadDetail(id!);
    } catch (err: any) {
      alert(err?.response?.data?.error || 'Failed to assign listings');
    } finally {
      setAddingListing(false);
    }
  }

  // ── Attribute helpers ─────────────────────────────────────────────────────
  function addAttributeRow() {
    setAttributeDefaults(prev => [
      ...prev,
      { attribute_name: '', source: 'default_value', ep_key: '', default_value: '' },
    ]);
  }

  function updateAttributeRow(idx: number, patch: Partial<AttributeDefault>) {
    setAttributeDefaults(prev => prev.map((r, i) => i === idx ? { ...r, ...patch } : r));
  }

  function removeAttributeRow(idx: number) {
    setAttributeDefaults(prev => prev.filter((_, i) => i !== idx));
  }

  // ── Variation helpers ─────────────────────────────────────────────────────
  function addVariationRow() {
    setVariationSchema(prev => [...prev, '']);
  }

  function updateVariationRow(idx: number, val: string) {
    setVariationSchema(prev => prev.map((r, i) => i === idx ? val : r));
  }

  function removeVariationRow(idx: number) {
    setVariationSchema(prev => prev.filter((_, i) => i !== idx));
  }

  // ── Shipping helpers ──────────────────────────────────────────────────────
  function addShippingRow() {
    setShippingDefaults(prev => [...prev, { key: '', value: '' }]);
  }

  function updateShippingRow(idx: number, patch: { key?: string; value?: string }) {
    setShippingDefaults(prev => prev.map((r, i) => i === idx ? { ...r, ...patch } : r));
  }

  function removeShippingRow(idx: number) {
    setShippingDefaults(prev => prev.filter((_, i) => i !== idx));
  }

  // ── Render ────────────────────────────────────────────────────────────────

  if (loading) {
    return (
      <div className="page">
        <div style={{ padding: 48, textAlign: 'center', color: 'var(--text-muted)' }}>
          <div style={{ fontSize: 32, marginBottom: 12 }}>⏳</div>
          Loading configurator…
        </div>
      </div>
    );
  }

  if (loadError) {
    return (
      <div className="page">
        <div style={{
          padding: 24, borderRadius: 8,
          background: 'var(--danger-glow)', border: '1px solid var(--danger)', color: 'var(--danger)',
        }}>
          {loadError}
        </div>
      </div>
    );
  }

  return (
    <>
    <div className="page">
      {/* ── Page header ── */}
      <div className="page-header">
        <div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 4 }}>
            <button
              style={{
                background: 'none', border: 'none', color: 'var(--text-muted)',
                cursor: 'pointer', fontSize: 13, padding: 0,
              }}
              onClick={() => navigate('/marketplace/configurators')}
            >
              ← Configurators
            </button>
          </div>
          <h1 className="page-title">
            {isNew ? '⚙️ New Configurator' : `⚙️ ${name || 'Configurator'}`}
          </h1>
          {!isNew && (
            <p className="page-subtitle">
              {channelEmoji[channel] || '🌐'} {channel} · {linkedListings.length} linked listing{linkedListings.length !== 1 ? 's' : ''}
            </p>
          )}
        </div>
        <div className="page-actions">
          {!isNew && (
            <button className="btn btn-secondary" onClick={openRevise}>
              🔄 Revise Configurator
            </button>
          )}
          {!isNew && (
            <button
              className="btn btn-secondary"
              onClick={handleDuplicate}
              disabled={duplicating}
            >
              {duplicating ? '⏳ Duplicating…' : '📋 Duplicate'}
            </button>
          )}
          <button
            className="btn btn-primary"
            onClick={handleSave}
            disabled={saving}
          >
            {saving ? '⏳ Saving…' : (isNew ? '✅ Create' : '💾 Save')}
          </button>
        </div>
      </div>

      {/* ── Save feedback ── */}
      {saveError && (
        <div style={{
          padding: 12, marginBottom: 16, borderRadius: 8,
          background: 'var(--danger-glow)', border: '1px solid var(--danger)',
          color: 'var(--danger)', fontSize: 13,
        }}>{saveError}</div>
      )}
      {saveSuccess && (
        <div style={{
          padding: 12, marginBottom: 16, borderRadius: 8,
          background: 'var(--success-glow)', border: '1px solid var(--success)',
          color: 'var(--success)', fontSize: 13,
        }}>Configurator saved successfully.</div>
      )}
      {duplicateError && (
        <div style={{
          padding: 12, marginBottom: 16, borderRadius: 8,
          background: 'var(--danger-glow)', border: '1px solid var(--danger)',
          color: 'var(--danger)', fontSize: 13,
        }}>{duplicateError}</div>
      )}

      {/* ── Tab bar ── */}
      <div style={{
        display: 'flex', gap: 0, marginBottom: 24,
        borderBottom: '1px solid var(--border)',
      }}>
        {([
          { key: 'general', label: '⚙️ General' },
          { key: 'attributes', label: '🏷️ Attributes' },
          { key: 'variations', label: '🔀 Variations' },
          ...(!isNew ? [{ key: 'linked', label: `📋 Linked Listings (${linkedListings.length})` }] : []),
        ] as { key: Tab; label: string }[]).map(tab => (
          <button
            key={tab.key}
            onClick={() => setActiveTab(tab.key)}
            style={{
              padding: '10px 18px', background: 'none', border: 'none',
              borderBottom: activeTab === tab.key
                ? '2px solid var(--primary)'
                : '2px solid transparent',
              color: activeTab === tab.key ? 'var(--primary)' : 'var(--text-secondary)',
              cursor: 'pointer', fontSize: 14, fontWeight: activeTab === tab.key ? 600 : 400,
              marginBottom: -1, transition: 'all 150ms',
            }}
          >
            {tab.label}
          </button>
        ))}
      </div>

      {/* ══════════════════════════════════════════════════════════════════
          TAB: GENERAL
         ══════════════════════════════════════════════════════════════════ */}
      {activeTab === 'general' && (
        <>
          {/* Name + Channel */}
          <div style={cardStyle}>
            <div style={sectionTitle}>Basic Settings</div>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 20 }}>
              <div>
                <label style={labelStyle}>Configurator Name *</label>
                <input
                  style={inputStyle}
                  placeholder="e.g. Electronics — Amazon UK"
                  value={name}
                  onChange={e => setName(e.target.value)}
                />
              </div>
              <div>
                <label style={labelStyle}>Channel *</label>
                <select
                  style={{ ...inputStyle, cursor: 'pointer' }}
                  value={channel}
                  onChange={e => { setChannel(e.target.value); setCredentialId(''); }}
                >
                  {ALL_CHANNELS.map(ch => (
                    <option key={ch} value={ch}>
                      {channelEmoji[ch] || '🌐'} {ch.charAt(0).toUpperCase() + ch.slice(1)}
                    </option>
                  ))}
                </select>
              </div>
            </div>

            <div style={{ marginTop: 20 }}>
              <label style={labelStyle}>Channel Credential</label>
              <select
                style={{ ...inputStyle, cursor: 'pointer', maxWidth: 400 }}
                value={credentialId}
                onChange={e => setCredentialId(e.target.value)}
              >
                <option value="">— Select credential (optional) —</option>
                {channelCredentials.map((cred: any) => (
                  <option key={cred.credential_id} value={cred.credential_id}>
                    {cred.account_name} ({cred.environment})
                  </option>
                ))}
              </select>
              {channelCredentials.length === 0 && channel && (
                <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 6 }}>
                  No credentials found for {channel}. Add one in{' '}
                  <span
                    style={{ color: 'var(--primary)', cursor: 'pointer' }}
                    onClick={() => navigate('/marketplace/connections')}
                  >
                    Marketplace Connections
                  </span>.
                </div>
              )}
            </div>
          </div>

          {/* Category */}
          <div style={cardStyle}>
            <div style={sectionTitle}>Category</div>
            {channel === 'temu' ? (
              <div>
                <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
                  <div style={{ flex: 1, padding: '10px 14px', background: 'var(--bg-tertiary)', borderRadius: 8, fontSize: 13, color: categoryId ? 'var(--text-primary)' : 'var(--text-muted)', border: '1px solid var(--border)' }}>
                    {categoryId
                      ? `${categoryPath || categoryId} (${categoryId})`
                      : 'No default category set'}
                  </div>
                  <button className="btn btn-secondary" style={{ fontSize: 13, whiteSpace: 'nowrap' }} onClick={openTemuCategoryPicker}>
                    {categoryId ? 'Change ✎' : '+ Pick Category'}
                  </button>
                  {categoryId && (
                    <button className="btn-icon" title="Clear" onClick={() => { setCategoryId(''); setCategoryPath(''); }}>✕</button>
                  )}
                </div>
                <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 6 }}>
                  When this configurator is applied, products will default to this Temu category.
                </div>
              </div>
            ) : (
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 20 }}>
                <div>
                  <label style={labelStyle}>Category ID</label>
                  <input
                    style={inputStyle}
                    placeholder="e.g. 3504 or CE-Cameras"
                    value={categoryId}
                    onChange={e => setCategoryId(e.target.value)}
                  />
                  <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 4 }}>
                    Channel's internal category identifier
                  </div>
                </div>
                <div>
                  <label style={labelStyle}>Category Path</label>
                  <input
                    style={inputStyle}
                    placeholder="e.g. Electronics > Cameras > Digital Cameras"
                    value={categoryPath}
                    onChange={e => setCategoryPath(e.target.value)}
                  />
                  <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 4 }}>
                    Human-readable path for display
                  </div>
                </div>
              </div>
            )}
          </div>

          {/* Shipping defaults */}
          <div style={cardStyle}>
            <div style={{
              display: 'flex', alignItems: 'center',
              justifyContent: 'space-between', marginBottom: 16,
              paddingBottom: 10, borderBottom: '1px solid var(--border)',
            }}>
              <span style={{ fontSize: 15, fontWeight: 700, color: 'var(--text-primary)' }}>
                Shipping Defaults
              </span>
              {channel !== 'temu' && (
                <button className="btn btn-secondary" style={{ fontSize: 12 }} onClick={addShippingRow}>
                  + Add Field
                </button>
              )}
            </div>
            {channel === 'temu' ? (
              <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
                {/* Shipping Template */}
                <div>
                  <label style={labelStyle}>Shipping Template</label>
                  <select
                    style={inputStyle}
                    value={shippingDefaults.find(r => r.key === 'shipping_template')?.value || ''}
                    onChange={e => {
                      const val = e.target.value;
                      setShippingDefaults(prev => {
                        const next = prev.filter(r => r.key !== 'shipping_template');
                        if (val) next.push({ key: 'shipping_template', value: val });
                        return next;
                      });
                    }}
                  >
                    <option value="">— No default —</option>
                    {temuShippingTemplates.map(t => (
                      <option key={t.templateId} value={String(t.templateId)}>{t.name}</option>
                    ))}
                  </select>
                </div>
                {/* Fulfillment Type */}
                <div>
                  <label style={labelStyle}>Fulfillment Type</label>
                  <select
                    style={inputStyle}
                    value={shippingDefaults.find(r => r.key === 'fulfillment_type')?.value || ''}
                    onChange={e => {
                      const val = e.target.value;
                      setShippingDefaults(prev => {
                        const next = prev.filter(r => r.key !== 'fulfillment_type');
                        if (val) next.push({ key: 'fulfillment_type', value: val });
                        return next;
                      });
                    }}
                  >
                    <option value="">— No default —</option>
                    <option value="1">Merchant Fulfilled</option>
                    <option value="2">Temu Fulfilled (TFN)</option>
                  </select>
                </div>
                {/* Shipment Limit Days */}
                <div>
                  <label style={labelStyle}>Shipment Limit (days)</label>
                  <input
                    type="number"
                    min={1}
                    max={30}
                    style={{ ...inputStyle, width: 120 }}
                    placeholder="e.g. 2"
                    value={shippingDefaults.find(r => r.key === 'shipment_limit_day')?.value || ''}
                    onChange={e => {
                      const val = e.target.value;
                      setShippingDefaults(prev => {
                        const next = prev.filter(r => r.key !== 'shipment_limit_day');
                        if (val) next.push({ key: 'shipment_limit_day', value: val });
                        return next;
                      });
                    }}
                  />
                  <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 4 }}>How many days after order to ship</div>
                </div>
              </div>
            ) : (
              <>
                {shippingDefaults.length === 0 ? (
                  <div style={{ color: 'var(--text-muted)', fontSize: 13, textAlign: 'center', padding: '16px 0' }}>
                    No shipping defaults. Click "Add Field" to add channel-specific shipping settings.
                  </div>
                ) : (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                    {shippingDefaults.map((row, idx) => (
                      <div key={idx} style={{ display: 'grid', gridTemplateColumns: '1fr 1fr auto', gap: 10, alignItems: 'center' }}>
                        <input
                          style={inputStyle}
                          placeholder="Field name (e.g. service)"
                          value={row.key}
                          onChange={e => updateShippingRow(idx, { key: e.target.value })}
                        />
                        <input
                          style={inputStyle}
                          placeholder="Value"
                          value={row.value}
                          onChange={e => updateShippingRow(idx, { value: e.target.value })}
                        />
                        <button className="btn-icon" onClick={() => removeShippingRow(idx)}>🗑️</button>
                      </div>
                    ))}
                  </div>
                )}
              </>
            )}
          </div>
        </>
      )}

      {/* ══════════════════════════════════════════════════════════════════
          TAB: ATTRIBUTES
         ══════════════════════════════════════════════════════════════════ */}
      {activeTab === 'attributes' && (
        <div style={cardStyle}>
          <div style={{
            display: 'flex', alignItems: 'center',
            justifyContent: 'space-between', marginBottom: 16,
            paddingBottom: 10, borderBottom: '1px solid var(--border)',
          }}>
            <div>
              <span style={{ fontSize: 15, fontWeight: 700, color: 'var(--text-primary)' }}>
                Attribute Defaults
              </span>
              <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>
                Define how each attribute gets its value when this configurator is applied to a listing.
              </div>
            </div>
            <button className="btn btn-secondary" style={{ fontSize: 12 }} onClick={addAttributeRow}>
              + Add Attribute
            </button>
          </div>

          {attributeDefaults.length === 0 ? (
            <div style={{ color: 'var(--text-muted)', fontSize: 13, textAlign: 'center', padding: '24px 0' }}>
              No attribute defaults. Click "+ Add Attribute" to define attribute sources.
            </div>
          ) : (
            <>
              {/* Header row */}
              <div style={{
                display: 'grid', gridTemplateColumns: '1.5fr 150px 1fr auto',
                gap: 10, padding: '6px 0', marginBottom: 6,
                borderBottom: '1px solid var(--border)',
              }}>
                {['Attribute Name', 'Source', 'Value / EP Key', ''].map(h => (
                  <div key={h} style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase' }}>
                    {h}
                  </div>
                ))}
              </div>

              {attributeDefaults.map((row, idx) => (
                <div
                  key={idx}
                  style={{
                    display: 'grid', gridTemplateColumns: '1.5fr 150px 1fr auto',
                    gap: 10, alignItems: 'center', paddingBottom: 10,
                    borderBottom: '1px solid var(--border)',
                    marginBottom: 10,
                  }}
                >
                  <input
                    style={inputStyle}
                    placeholder="e.g. brand"
                    value={row.attribute_name}
                    onChange={e => updateAttributeRow(idx, { attribute_name: e.target.value })}
                  />
                  <select
                    style={{ ...inputStyle, cursor: 'pointer' }}
                    value={row.source}
                    onChange={e => updateAttributeRow(idx, { source: e.target.value as AttributeDefault['source'] })}
                  >
                    <option value="default_value">Default Value</option>
                    <option value="extended_property">Extended Property</option>
                  </select>
                  {row.source === 'extended_property' ? (
                    <input
                      style={inputStyle}
                      placeholder="Extended property key"
                      value={row.ep_key || ''}
                      onChange={e => updateAttributeRow(idx, { ep_key: e.target.value })}
                    />
                  ) : (
                    <input
                      style={inputStyle}
                      placeholder="Default value"
                      value={row.default_value || ''}
                      onChange={e => updateAttributeRow(idx, { default_value: e.target.value })}
                    />
                  )}
                  <button className="btn-icon" onClick={() => removeAttributeRow(idx)}>🗑️</button>
                </div>
              ))}
            </>
          )}
        </div>
      )}

      {/* ══════════════════════════════════════════════════════════════════
          TAB: VARIATIONS
         ══════════════════════════════════════════════════════════════════ */}
      {activeTab === 'variations' && (
        <div style={cardStyle}>
          <div style={{
            display: 'flex', alignItems: 'center',
            justifyContent: 'space-between', marginBottom: 16,
            paddingBottom: 10, borderBottom: '1px solid var(--border)',
          }}>
            <div>
              <span style={{ fontSize: 15, fontWeight: 700, color: 'var(--text-primary)' }}>
                Variation Schema
              </span>
              <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>
                Define the variation attribute names for this channel and product type (e.g. Size, Colour).
              </div>
            </div>
            <button className="btn btn-secondary" style={{ fontSize: 12 }} onClick={addVariationRow}>
              + Add Variation
            </button>
          </div>

          {variationSchema.length === 0 ? (
            <div style={{ color: 'var(--text-muted)', fontSize: 13, textAlign: 'center', padding: '24px 0' }}>
              No variation attributes defined. Click "+ Add Variation" to define the variation schema.
            </div>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 10, maxWidth: 480 }}>
              {variationSchema.map((attr, idx) => (
                <div key={idx} style={{ display: 'flex', gap: 10, alignItems: 'center' }}>
                  <div style={{
                    width: 28, height: 28, borderRadius: 6, background: 'var(--bg-tertiary)',
                    display: 'flex', alignItems: 'center', justifyContent: 'center',
                    fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', flexShrink: 0,
                  }}>
                    {idx + 1}
                  </div>
                  <input
                    style={inputStyle}
                    placeholder="e.g. Size"
                    value={attr}
                    onChange={e => updateVariationRow(idx, e.target.value)}
                  />
                  <button className="btn-icon" onClick={() => removeVariationRow(idx)}>🗑️</button>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* ══════════════════════════════════════════════════════════════════
          TAB: LINKED LISTINGS
         ══════════════════════════════════════════════════════════════════ */}
      {activeTab === 'linked' && !isNew && (
        <div style={cardStyle}>
          <div style={{
            display: 'flex', alignItems: 'center',
            justifyContent: 'space-between', marginBottom: 16,
            paddingBottom: 10, borderBottom: '1px solid var(--border)',
          }}>
            <div>
              <span style={{ fontSize: 15, fontWeight: 700, color: 'var(--text-primary)' }}>
                Linked Listings ({linkedListings.length})
              </span>
              <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>
                Listings that use this configurator's settings.
              </div>
            </div>
            <button
              className="btn btn-secondary"
              style={{ fontSize: 12 }}
              onClick={() => { setListingSearchInput(''); setAddListingOpen(true); }}
            >
              + Add Listings
            </button>
          </div>

          {linkedListings.length === 0 ? (
            <div style={{ color: 'var(--text-muted)', fontSize: 13, textAlign: 'center', padding: '24px 0' }}>
              No listings linked yet. Click "+ Add Listings" to link existing listings to this configurator.
            </div>
          ) : (
            <table style={{ width: '100%', borderCollapse: 'collapse' }}>
              <thead>
                <tr style={{ borderBottom: '1px solid var(--border)' }}>
                  {['SKU', 'Title', 'Channel', 'State', ''].map(h => (
                    <th key={h} style={{
                      textAlign: 'left', padding: '8px 10px',
                      fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase',
                    }}>{h}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {linkedListings.map(listing => (
                  <tr key={listing.listing_id} style={{ borderBottom: '1px solid var(--border)' }}>
                    <td style={{ padding: '8px 10px', fontSize: 12, fontFamily: 'monospace', color: 'var(--text-secondary)' }}>
                      {listing.product_sku || '—'}
                    </td>
                    <td style={{ padding: '8px 10px', fontSize: 13 }}>
                      <span
                        style={{ color: 'var(--primary)', cursor: 'pointer' }}
                        onClick={() => navigate(`/marketplace/listings/${listing.listing_id}`)}
                      >
                        {listing.product_title || listing.overrides?.title || '(Untitled)'}
                      </span>
                    </td>
                    <td style={{ padding: '8px 10px', fontSize: 12 }}>
                      {listing.channel || '—'}
                    </td>
                    <td style={{ padding: '8px 10px' }}>
                      {listing.state && (
                        <span style={{
                          padding: '2px 8px', borderRadius: 4, fontSize: 11, fontWeight: 700,
                          background: stateColors[listing.state]?.bg || 'var(--bg-tertiary)',
                          color: stateColors[listing.state]?.fg || 'var(--text-secondary)',
                        }}>
                          {listing.state.toUpperCase()}
                        </span>
                      )}
                    </td>
                    <td style={{ padding: '8px 10px', textAlign: 'right' }}>
                      <button
                        className="btn-icon"
                        title="Remove link"
                        disabled={removingListing === listing.listing_id}
                        onClick={() => handleRemoveListing(listing.listing_id)}
                      >
                        {removingListing === listing.listing_id ? '⏳' : '🔗'}
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      )}

      {/* ══════════════════════════════════════════════════════════════════
          ADD LISTING MODAL
         ══════════════════════════════════════════════════════════════════ */}
      {addListingOpen && (
        <div style={{
          position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.6)',
          display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000,
        }} onClick={() => setAddListingOpen(false)}>
          <div style={{
            background: 'var(--bg-secondary)', border: '1px solid var(--border)',
            borderRadius: 12, padding: 28, maxWidth: 480, width: '90%',
          }} onClick={e => e.stopPropagation()}>
            <h3 style={{ fontSize: 16, fontWeight: 700, marginBottom: 8, color: 'var(--text-primary)' }}>
              Add Listings
            </h3>
            <p style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 16 }}>
              Enter one or more listing IDs (comma or space separated) to link to this configurator.
            </p>
            <textarea
              style={{
                ...inputStyle, minHeight: 80, resize: 'vertical',
                fontFamily: 'monospace', fontSize: 13,
              }}
              placeholder="listing_id_1, listing_id_2, ..."
              value={listingSearchInput}
              onChange={e => setListingSearchInput(e.target.value)}
            />
            <div style={{ display: 'flex', gap: 10, justifyContent: 'flex-end', marginTop: 16 }}>
              <button className="btn btn-secondary" onClick={() => setAddListingOpen(false)}>
                Cancel
              </button>
              <button
                className="btn btn-primary"
                onClick={handleAddListing}
                disabled={addingListing || !listingSearchInput.trim()}
              >
                {addingListing ? '⏳ Adding…' : 'Link Listings'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* ══════════════════════════════════════════════════════════════════
          REVISE DIALOG
         ══════════════════════════════════════════════════════════════════ */}
      {reviseOpen && (
        <div style={{
          position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.6)',
          display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000,
        }} onClick={() => !revising && setReviseOpen(false)}>
          <div style={{
            background: 'var(--bg-secondary)', border: '1px solid var(--border)',
            borderRadius: 12, padding: 28, maxWidth: 520, width: '90%',
          }} onClick={e => e.stopPropagation()}>
            <h3 style={{ fontSize: 17, fontWeight: 700, marginBottom: 6, color: 'var(--text-primary)' }}>
              🔄 Push changes to linked listings
            </h3>
            <p style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 20 }}>
              Select the fields to push from this configurator to all {linkedListings.length} linked listing{linkedListings.length !== 1 ? 's' : ''}.
            </p>

            {/* Field checkboxes */}
            {!reviseJob && (
              <div style={{ display: 'flex', flexDirection: 'column', gap: 10, marginBottom: 20 }}>
                {REVISE_FIELDS.map(f => (
                  <label key={f.key} style={{
                    display: 'flex', alignItems: 'flex-start', gap: 12, cursor: 'pointer',
                    padding: '10px 14px', borderRadius: 8,
                    background: reviseFields.includes(f.key) ? 'var(--primary-glow)' : 'var(--bg-tertiary)',
                    border: `1px solid ${reviseFields.includes(f.key) ? 'var(--primary)' : 'var(--border)'}`,
                    transition: 'all 150ms',
                  }}>
                    <input
                      type="checkbox"
                      checked={reviseFields.includes(f.key)}
                      onChange={() => toggleReviseField(f.key)}
                      style={{ marginTop: 2, cursor: 'pointer', accentColor: 'var(--primary)' }}
                    />
                    <div>
                      <div style={{ fontWeight: 600, fontSize: 13, color: 'var(--text-primary)' }}>
                        {f.label}
                      </div>
                      <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                        {f.description}
                      </div>
                    </div>
                  </label>
                ))}
              </div>
            )}

            {/* Warning */}
            {!reviseJob && linkedListings.length > 0 && (
              <div style={{
                padding: '10px 14px', borderRadius: 8, marginBottom: 16,
                background: 'var(--warning-glow)', border: '1px solid var(--warning)',
                color: 'var(--warning)', fontSize: 12,
              }}>
                ⚠️ This will update <strong>{linkedListings.length}</strong> listing{linkedListings.length !== 1 ? 's' : ''} on <strong>{channel}</strong>. This action cannot be undone.
              </div>
            )}

            {/* Error */}
            {reviseError && (
              <div style={{
                padding: '10px 14px', borderRadius: 8, marginBottom: 16,
                background: 'var(--danger-glow)', border: '1px solid var(--danger)',
                color: 'var(--danger)', fontSize: 12,
              }}>
                {reviseError}
              </div>
            )}

            {/* Job result */}
            {reviseJob && (
              <div style={{
                padding: '14px 16px', borderRadius: 8, marginBottom: 20,
                background: 'var(--success-glow)', border: '1px solid var(--success)',
              }}>
                <div style={{ fontWeight: 700, color: 'var(--success)', marginBottom: 6 }}>
                  ✅ Revise complete
                </div>
                <div style={{ fontSize: 13, color: 'var(--text-primary)' }}>
                  {reviseJob.succeeded} succeeded · {reviseJob.failed} failed
                </div>
                {reviseJob.errors && reviseJob.errors.length > 0 && (
                  <div style={{ marginTop: 8, fontSize: 12, color: 'var(--danger)' }}>
                    {reviseJob.errors.slice(0, 5).map((e, i) => (
                      <div key={i}>• {e}</div>
                    ))}
                    {reviseJob.errors.length > 5 && (
                      <div>…and {reviseJob.errors.length - 5} more errors</div>
                    )}
                  </div>
                )}
              </div>
            )}

            {/* Actions */}
            <div style={{ display: 'flex', gap: 10, justifyContent: 'flex-end' }}>
              <button
                className="btn btn-secondary"
                onClick={() => setReviseOpen(false)}
                disabled={revising}
              >
                {reviseJob ? 'Close' : 'Cancel'}
              </button>
              {!reviseJob && (
                <button
                  className="btn btn-primary"
                  onClick={handleRevise}
                  disabled={revising || reviseFields.length === 0}
                >
                  {revising ? '⏳ Pushing…' : `Push to ${linkedListings.length} listing${linkedListings.length !== 1 ? 's' : ''}`}
                </button>
              )}
            </div>
          </div>
        </div>
      )}
    </div>

    {/* ── Temu Category Picker Modal ──────────────────────────────────── */}
    {temuCatModalOpen && (
      <div
        style={{ position: 'fixed', inset: 0, zIndex: 1200, background: 'rgba(0,0,0,0.75)', display: 'flex', alignItems: 'center', justifyContent: 'center', padding: 20 }}
        onClick={e => { if (e.target === e.currentTarget) setTemuCatModalOpen(false); }}
      >
        <div style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 14, width: '90vw', maxWidth: 1100, maxHeight: '85vh', display: 'flex', flexDirection: 'column' }}>
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '18px 24px', borderBottom: '1px solid var(--border)' }}>
            <div>
              <div style={{ fontSize: 16, fontWeight: 700, color: 'var(--text-primary)' }}>Select Default Temu Category</div>
              <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>
                {temuCatSelections.filter(Boolean).length > 0
                  ? temuCatSelections.filter(Boolean).map((s: any) => s.catName).join(' › ')
                  : 'Browse the category tree — select a leaf category (✓) to confirm'}
              </div>
            </div>
            <button onClick={() => setTemuCatModalOpen(false)} style={{ background: 'none', border: 'none', fontSize: 22, cursor: 'pointer', color: 'var(--text-muted)' }}>✕</button>
          </div>
          <div style={{ flex: 1, overflowX: 'auto', padding: '16px 24px' }}>
            {temuCatLoading ? (
              <div style={{ textAlign: 'center', padding: 40, color: 'var(--text-muted)' }}>Loading…</div>
            ) : (
              <div style={{ display: 'flex', gap: 12, minWidth: 'max-content', height: 420 }}>
                {temuCatLevels.map((cats, li) => (
                  <div key={li} style={{ width: 220, display: 'flex', flexDirection: 'column', border: '1px solid var(--border)', borderRadius: 8, overflow: 'hidden' }}>
                    <div style={{ padding: '8px 12px', fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', background: 'var(--bg-tertiary)', textTransform: 'uppercase', letterSpacing: 1 }}>
                      {['Category', 'Subcategory', 'Sub-subcategory', 'Level 4', 'Level 5'][li] || `Level ${li + 1}`}
                    </div>
                    <div style={{ flex: 1, overflowY: 'auto' }}>
                      {cats.length === 0 ? (
                        <div style={{ padding: 12, fontSize: 12, color: 'var(--text-muted)' }}>No items</div>
                      ) : cats.map((cat: any) => {
                        const sel = temuCatSelections[li];
                        const isSelected = sel && String(sel.catId) === String(cat.catId);
                        return (
                          <div
                            key={cat.catId}
                            onClick={() => handleTemuCatSelect(li, String(cat.catId))}
                            style={{ padding: '8px 12px', fontSize: 13, cursor: 'pointer', background: isSelected ? 'rgba(249,115,22,0.15)' : 'transparent', color: isSelected ? '#f97316' : cat.leaf ? 'var(--text-primary)' : 'var(--text-secondary)', display: 'flex', alignItems: 'center', justifyContent: 'space-between', borderBottom: '1px solid var(--border)' }}
                          >
                            <span>{cat.catName}</span>
                            {cat.leaf ? <span style={{ fontSize: 10, color: '#22c55e', fontWeight: 700 }}>✓</span> : <span style={{ fontSize: 10, color: 'var(--text-muted)' }}>›</span>}
                          </div>
                        );
                      })}
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
          <div style={{ padding: '12px 24px', borderTop: '1px solid var(--border)', fontSize: 12, color: 'var(--text-muted)' }}>
            Navigate to a leaf category (marked ✓) to set the default
          </div>
        </div>
      </div>
    )}
    </>
  );
}

// ── Listing state colours (matches ListingList.tsx) ───────────────────────────
const stateColors: Record<string, { bg: string; fg: string }> = {
  published: { bg: 'var(--success-glow)', fg: 'var(--success)' },
  ready:     { bg: 'var(--info-glow)',    fg: 'var(--info)' },
  imported:  { bg: 'var(--warning-glow)', fg: 'var(--warning)' },
  draft:     { bg: 'var(--bg-tertiary)',  fg: 'var(--text-secondary)' },
  error:     { bg: 'var(--danger-glow)',  fg: 'var(--danger)' },
  blocked:   { bg: 'var(--danger-glow)',  fg: 'var(--danger)' },
  paused:    { bg: 'var(--warning-glow)', fg: 'var(--warning)' },
  missing:   { bg: 'var(--danger-glow)',  fg: 'var(--danger)' },
};

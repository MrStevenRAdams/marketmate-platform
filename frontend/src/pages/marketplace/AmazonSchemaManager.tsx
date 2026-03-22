// ============================================================================
// AMAZON SCHEMA MANAGER PAGE
// ============================================================================
// Location: frontend/src/pages/marketplace/AmazonSchemaManager.tsx
//
// Features:
//   - Download individual schemas by searching product types
//   - "Download All" button — starts a background job on Cloud Run
//   - Live job progress polling (downloaded / total, errors, ETA)
//   - Marketplace selector — schemas stored separately per marketplace in Firestore
//   - View cached schemas, group fields, reorder, save field configs
// ============================================================================

import { useState, useEffect, useCallback, useMemo, useRef } from 'react';
import { useSearchParams } from 'react-router-dom';
import axios from 'axios';
import { getActiveTenantId } from '../../contexts/TenantContext';

const API_BASE = (import.meta as any).env?.VITE_API_URL || 'http://localhost:8080/api/v1';
const api = axios.create({ baseURL: `${API_BASE}/amazon`, headers: { 'Content-Type': 'application/json' } });
api.interceptors.request.use((config) => { config.headers['X-Tenant-Id'] = getActiveTenantId(); return config; });

// ── Types ──

interface SchemaListItem {
  productType: string;
  displayName: string;
  attrCount: number;
  groups: string[];
  cachedAt: string;
  hasFieldConfig: boolean;
}

interface SchemaAttribute {
  name: string;
  title: string;
  description: string;
  type: string;
  required: boolean;
  group: string;
  groupTitle: string;
  enumValues?: string[];
}

interface SchemaData {
  productType: string;
  displayName: string;
  attributes: SchemaAttribute[];
  gpsrAttributes?: SchemaAttribute[];
  groups: Record<string, { title: string; description: string; propertyNames: string[] }>;
  groupOrder: string[];
  cachedAt: string;
  marketplaceId: string;
}

interface FieldConfig {
  groups: Record<string, { title: string; order: number; fields: string[]; icon?: string }>;
  hiddenFields: string[];
  promotedFields: string[];
  fieldOrder: Record<string, number>;
}

interface SchemaJob {
  jobId: string;
  marketplaceId: string;
  status: string;
  totalTypes: number;
  downloaded: number;
  failed: number;
  errors: string[];
  startedAt: any;
  updatedAt: any;
  completedAt?: any;
}

// ── Marketplace options ──
const MARKETPLACES = [
  { id: 'A1F83G8C2ARO7P', label: 'UK', flag: '🇬🇧' },
  { id: 'ATVPDKIKX0DER', label: 'US', flag: '🇺🇸' },
  { id: 'A1PA6795UKMFR9', label: 'DE', flag: '🇩🇪' },
  { id: 'A13V1IB3VIYZZH', label: 'FR', flag: '🇫🇷' },
  { id: 'APJ6JRA9NG5V4', label: 'IT', flag: '🇮🇹' },
  { id: 'A1RKKUPIHCS9HS', label: 'ES', flag: '🇪🇸' },
  { id: 'A1805IZSGTT6HS', label: 'NL', flag: '🇳🇱' },
  { id: 'A2NODRKZP88ZB9', label: 'SE', flag: '🇸🇪' },
  { id: 'A1C3SOZRARQ6R3', label: 'PL', flag: '🇵🇱' },
  { id: 'A2EUQ1WTGCTBG2', label: 'CA', flag: '🇨🇦' },
  { id: 'A39IBJ37TRP1C6', label: 'AU', flag: '🇦🇺' },
  { id: 'A1VC38T7YXB528', label: 'JP', flag: '🇯🇵' },
];

// ── Suggested safety subgroups ──
const SUGGESTED_SUBGROUPS: Record<string, { title: string; icon: string; fields: string[] }> = {
  battery_info: {
    title: 'Battery Information', icon: '🔋',
    fields: ['batteries_required', 'batteries_included', 'battery', 'num_batteries', 'contains_battery_or_cell',
      'has_multiple_battery_powered_components', 'has_replaceable_battery', 'battery_contains_free_unabsorbed_liquid',
      'is_battery_non_spillable', 'battery_installation_device_type', 'has_less_than_30_percent_state_of_charge'],
  },
  lithium_battery: {
    title: 'Lithium Battery', icon: '⚡',
    fields: ['lithium_battery', 'number_of_lithium_metal_cells', 'number_of_lithium_ion_cells',
      'non_lithium_battery_energy_content', 'non_lithium_battery_packaging'],
  },
  hazmat_ghs: {
    title: 'Hazmat & GHS', icon: '☣️',
    fields: ['supplier_declared_dg_hz_regulation', 'ghs', 'ghs_chemical_h_code', 'hazmat', 'safety_data_sheet_url'],
  },
  toy_safety: {
    title: 'Toy Safety (EU Directive)', icon: '🧸',
    fields: ['eu_toys_safety_directive_age_warning', 'eu_toys_safety_directive_warning',
      'eu_toys_safety_directive_language', 'compliance_recommended_age', 'compliance_age_range',
      'compliance_toy_material', 'compliance_toy_type', 'compliance_operation_mode'],
  },
  gpsr: {
    title: 'GPSR (EU Product Safety)', icon: '🛡️',
    fields: ['gpsr_manufacturer_reference', 'gpsr_safety_attestation', 'dsa_responsible_party_address', 'compliance_media'],
  },
  general_safety: {
    title: 'General Safety', icon: '⚠️',
    fields: ['country_of_origin', 'safety_warning', 'warranty_description', 'item_weight',
      'ships_globally', 'is_oem_sourced_product', 'is_this_product_subject_to_buyer_age_restrictions'],
  },
};

// ============================================================================
// MAIN COMPONENT
// ============================================================================

export default function AmazonSchemaManager() {
  const [searchParams] = useSearchParams();
  const credentialId = searchParams.get('credential_id') || '';

  // ── Core state ──
  const [marketplace, setMarketplace] = useState('A1F83G8C2ARO7P');
  const [schemas, setSchemas] = useState<SchemaListItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [message, setMessage] = useState('');

  // ── Download ──
  const [ptSearch, setPtSearch] = useState('');
  const [ptResults, setPtResults] = useState<any[]>([]);
  const [ptSearching, setPtSearching] = useState(false);
  const [downloading, setDownloading] = useState<string | null>(null);

  // ── Background job ──
  const [activeJob, setActiveJob] = useState<SchemaJob | null>(null);
  const [startingJob, setStartingJob] = useState(false);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  // ── Schema viewer ──
  const [activeSchema, setActiveSchema] = useState<SchemaData | null>(null);
  const [fieldConfig, setFieldConfig] = useState<FieldConfig | null>(null);
  const [loadingSchema, setLoadingSchema] = useState(false);
  const [saving, setSaving] = useState(false);

  // ── Group editor ──
  const [customGroups, setCustomGroups] = useState<Record<string, { title: string; order: number; fields: string[]; icon: string }>>({});
  const [dragField, setDragField] = useState<string | null>(null);
  const [dragOverGroup, setDragOverGroup] = useState<string | null>(null);
  const [hiddenFields, setHiddenFields] = useState<Set<string>>(new Set());
  const [fieldSearch, setFieldSearch] = useState('');
  const [editGroupKey, setEditGroupKey] = useState<string | null>(null);

  const cred = credentialId ? `credential_id=${credentialId}` : '';

  // ── Load schemas list ──
  const loadSchemas = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const res = await api.get(`/schemas/list?marketplace_id=${marketplace}&${cred}`);
      setSchemas(res.data.schemas || []);
    } catch (err: any) {
      setError(err.response?.data?.error || err.message);
    } finally {
      setLoading(false);
    }
  }, [marketplace, cred]);

  useEffect(() => { loadSchemas(); }, [loadSchemas]);

  // ── Check for active jobs on mount ──
  const checkActiveJobs = useCallback(async () => {
    try {
      const res = await api.get(`/schemas/jobs?${cred}`);
      const jobs = res.data.jobs || [];
      const running = jobs.find((j: any) => j.status === 'running');
      if (running) {
        setActiveJob(running);
        startPolling(running.jobId);
      }
    } catch { /* ignore */ }
  }, [cred]);

  useEffect(() => { checkActiveJobs(); return () => { if (pollRef.current) clearInterval(pollRef.current); }; }, [checkActiveJobs]);

  // ── Job polling ──
  const startPolling = useCallback((jobId: string) => {
    if (pollRef.current) clearInterval(pollRef.current);
    pollRef.current = setInterval(async () => {
      try {
        const res = await api.get(`/schemas/jobs/${jobId}?${cred}`);
        const job = res.data as SchemaJob;
        setActiveJob(job);
        if (job.status === 'completed' || job.status === 'cancelled' || job.status === 'failed') {
          if (pollRef.current) clearInterval(pollRef.current);
          pollRef.current = null;
          loadSchemas(); // Refresh schema list
        }
      } catch {
        if (pollRef.current) clearInterval(pollRef.current);
        pollRef.current = null;
      }
    }, 2000);
  }, [cred, loadSchemas]);

  // ── Download All ──
  const downloadAll = useCallback(async () => {
    setStartingJob(true);
    setError('');
    try {
      const res = await api.post(`/schemas/download-all?${cred}`, { marketplaceId: marketplace });
      if (res.data.jobId) {
        setActiveJob({ jobId: res.data.jobId, marketplaceId: marketplace, status: 'running', totalTypes: res.data.totalTypes, downloaded: 0, failed: 0, errors: [], startedAt: new Date(), updatedAt: new Date() });
        startPolling(res.data.jobId);
        setMessage(`Started downloading ${res.data.totalTypes} schemas for ${MARKETPLACES.find(m => m.id === marketplace)?.label || marketplace}`);
      }
    } catch (err: any) {
      setError(err.response?.data?.error || err.message);
    } finally {
      setStartingJob(false);
    }
  }, [marketplace, cred, startPolling]);

  // ── Cancel job ──
  const cancelJob = useCallback(async () => {
    if (!activeJob) return;
    try {
      await api.post(`/schemas/jobs/${activeJob.jobId}/cancel?${cred}`);
      setMessage('Cancelling job...');
    } catch (err: any) {
      setError(err.response?.data?.error || err.message);
    }
  }, [activeJob, cred]);

  // ── Search product types ──
  const searchProductTypes = useCallback(async () => {
    if (!ptSearch.trim()) return;
    setPtSearching(true);
    try {
      const res = await api.get(`/product-types/search?q=${encodeURIComponent(ptSearch)}&${cred}`);
      setPtResults(res.data.productTypes || []);
    } catch (err: any) {
      setError(err.response?.data?.error || err.message);
    } finally {
      setPtSearching(false);
    }
  }, [ptSearch, cred]);

  // ── Download single schema ──
  const downloadSchema = useCallback(async (productType: string) => {
    setDownloading(productType);
    setMessage('');
    setError('');
    try {
      const res = await api.post(`/schemas/download?${cred}`, { productType, marketplaceId: marketplace });
      setMessage(`✓ Downloaded ${productType} — ${res.data.attrCount} attributes`);
      loadSchemas();
    } catch (err: any) {
      setError(err.response?.data?.error || err.message);
    } finally {
      setDownloading(null);
    }
  }, [marketplace, cred, loadSchemas]);

  // ── Open schema ──
  const openSchema = useCallback(async (productType: string) => {
    setLoadingSchema(true);
    setError('');
    try {
      const res = await api.get(`/schemas/${productType}?marketplace_id=${marketplace}&${cred}`);
      const schema = res.data.schema as SchemaData;
      const config = res.data.fieldConfig as FieldConfig | null;
      setActiveSchema(schema);
      setFieldConfig(config);

      if (config?.groups) {
        const cg: Record<string, any> = {};
        for (const [k, v] of Object.entries(config.groups)) {
          cg[k] = { title: v.title, order: v.order, fields: [...v.fields], icon: v.icon || '' };
        }
        setCustomGroups(cg);
        setHiddenFields(new Set(config.hiddenFields || []));
      } else {
        buildGroupsFromSchema(schema);
      }
    } catch (err: any) {
      setError(err.response?.data?.error || err.message);
    } finally {
      setLoadingSchema(false);
    }
  }, [marketplace, cred]);

  function buildGroupsFromSchema(schema: SchemaData) {
    const cg: Record<string, { title: string; order: number; fields: string[]; icon: string }> = {};
    const allAttrs = [...(schema.attributes || []), ...(schema.gpsrAttributes || [])];
    const attrMap = new Map(allAttrs.map(a => [a.name, a]));
    const gOrder = schema.groupOrder || [];
    gOrder.forEach((groupKey, idx) => {
      const group = schema.groups?.[groupKey];
      if (!group) return;
      cg[groupKey] = { title: group.title || groupKey, order: idx, fields: (group.propertyNames || []).filter(f => attrMap.has(f)), icon: '' };
    });
    const grouped = new Set(Object.values(cg).flatMap(g => g.fields));
    const ungrouped = allAttrs.filter(a => !grouped.has(a.name)).map(a => a.name);
    if (ungrouped.length > 0) cg['_ungrouped'] = { title: 'Ungrouped', order: 99, fields: ungrouped, icon: '❓' };
    setCustomGroups(cg);
    setHiddenFields(new Set());
  };

  // ── Save field config ──
  const saveFieldConfig = useCallback(async () => {
    if (!activeSchema) return;
    setSaving(true);
    setError('');
    try {
      const payload: FieldConfig = { groups: {}, hiddenFields: [...hiddenFields], promotedFields: [], fieldOrder: {} };
      for (const [k, v] of Object.entries(customGroups)) {
        payload.groups[k] = { title: v.title, order: v.order, fields: v.fields, icon: v.icon };
        v.fields.forEach((f, i) => { payload.fieldOrder[f] = i; });
      }
      await api.post(`/schemas/${activeSchema.productType}/field-config?marketplace_id=${marketplace}&${cred}`, payload);
      setMessage(`✓ Saved field config for ${activeSchema.productType}`);
      loadSchemas();
    } catch (err: any) {
      setError(err.response?.data?.error || err.message);
    } finally {
      setSaving(false);
    }
  }, [activeSchema, customGroups, hiddenFields, marketplace, cred, loadSchemas]);

  // ── Delete schema ──
  const deleteSchema = useCallback(async (productType: string) => {
    if (!confirm(`Delete schema "${productType}"?`)) return;
    try {
      await api.delete(`/schemas/${productType}?marketplace_id=${marketplace}&${cred}`);
      setSchemas(s => s.filter(x => x.productType !== productType));
      if (activeSchema?.productType === productType) setActiveSchema(null);
      setMessage(`✓ Deleted ${productType}`);
    } catch (err: any) {
      setError(err.response?.data?.error || err.message);
    }
  }, [marketplace, cred, activeSchema]);

  // ── Drag & drop ──
  const handleDragStart = (fieldName: string) => setDragField(fieldName);
  const handleDragOver = (e: React.DragEvent, groupKey: string) => { e.preventDefault(); setDragOverGroup(groupKey); };
  const handleDrop = (targetGroup: string) => {
    if (!dragField) return;
    const updated = { ...customGroups };
    for (const [k, g] of Object.entries(updated)) {
      const idx = g.fields.indexOf(dragField);
      if (idx !== -1) { g.fields = g.fields.filter(f => f !== dragField); updated[k] = { ...g }; }
    }
    if (updated[targetGroup]) updated[targetGroup] = { ...updated[targetGroup], fields: [...updated[targetGroup].fields, dragField] };
    setCustomGroups(updated);
    setDragField(null);
    setDragOverGroup(null);
  };

  const moveField = (groupKey: string, fromIdx: number, toIdx: number) => {
    const g = customGroups[groupKey];
    if (!g) return;
    const fields = [...g.fields];
    const [item] = fields.splice(fromIdx, 1);
    fields.splice(toIdx, 0, item);
    setCustomGroups({ ...customGroups, [groupKey]: { ...g, fields } });
  };

  const addGroup = () => {
    const key = `custom_${Date.now()}`;
    setCustomGroups({ ...customGroups, [key]: { title: 'New Group', order: Object.keys(customGroups).length, fields: [], icon: '📁' } });
    setEditGroupKey(key);
  };

  const applySuggestedSubgroups = () => {
    if (!activeSchema) return;
    const allAttrs = [...(activeSchema.attributes || []), ...(activeSchema.gpsrAttributes || [])];
    const attrNames = new Set(allAttrs.map(a => a.name));
    const updated = { ...customGroups };
    delete updated['safety_and_compliance'];
    let order = 0;
    for (const [key, sg] of Object.entries(SUGGESTED_SUBGROUPS)) {
      const fields = sg.fields.filter(f => attrNames.has(f));
      if (fields.length === 0) continue;
      for (const [gk, gv] of Object.entries(updated)) {
        updated[gk] = { ...gv, fields: gv.fields.filter(f => !fields.includes(f)) };
      }
      updated[key] = { title: sg.title, order: order++, fields, icon: sg.icon };
    }
    for (const [k, v] of Object.entries(updated)) {
      if (!SUGGESTED_SUBGROUPS[k]) updated[k] = { ...v, order: order++ };
    }
    setCustomGroups(updated);
    setMessage('Applied suggested safety subgroups — review and save when ready');
  };

  // ── Computed ──
  const attrMap = useMemo(() => {
    if (!activeSchema) return new Map<string, SchemaAttribute>();
    const all = [...(activeSchema.attributes || []), ...(activeSchema.gpsrAttributes || [])];
    return new Map(all.map(a => [a.name, a]));
  }, [activeSchema]);

  const sortedGroups = useMemo(() => Object.entries(customGroups).sort((a, b) => a[1].order - b[1].order), [customGroups]);
  const totalFields = attrMap.size;
  const groupedFields = Object.values(customGroups).reduce((sum, g) => sum + g.fields.length, 0);
  const requiredFields = activeSchema ? [...(activeSchema.attributes || []), ...(activeSchema.gpsrAttributes || [])].filter(a => a.required).length : 0;

  // ── Job progress helpers ──
  const jobProgress = activeJob ? (activeJob.totalTypes > 0 ? ((activeJob.downloaded + activeJob.failed) / activeJob.totalTypes * 100) : 0) : 0;
  const jobIsRunning = activeJob?.status === 'running';
  const mpLabel = (id: string) => MARKETPLACES.find(m => m.id === id)?.label || id;

  // ============================================================================
  // RENDER
  // ============================================================================

  return (
    <div style={{ maxWidth: 1400, margin: '0 auto', padding: '24px 16px', color: 'var(--text-primary, #e5e7eb)' }}>
      {/* ── Header ── */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 24 }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 24, fontWeight: 700 }}>Amazon Schema Manager</h1>
          <p style={{ margin: '4px 0 0', fontSize: 13, color: 'var(--text-secondary, #9ca3af)' }}>
            Download product type schemas, group fields into logical sections, reorder, and save configurations
          </p>
        </div>
        <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
          <label style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)' }}>Marketplace:</label>
          <select value={marketplace} onChange={e => { setMarketplace(e.target.value); setActiveSchema(null); }} style={selectStyle}>
            {MARKETPLACES.map(m => <option key={m.id} value={m.id}>{m.flag} {m.label}</option>)}
          </select>
        </div>
      </div>

      {/* ── Messages ── */}
      {error && <Banner type="error" text={error} onClose={() => setError('')} />}
      {message && <Banner type="success" text={message} onClose={() => setMessage('')} />}

      {/* ── Active Job Progress Bar ── */}
      {activeJob && (
        <div style={{ background: 'var(--bg-secondary, #1e293b)', borderRadius: 10, border: '1px solid var(--border, #374151)', padding: '14px 18px', marginBottom: 16 }}>
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 8 }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              {jobIsRunning && <span style={{ animation: 'spin 1s linear infinite', display: 'inline-block' }}>⏳</span>}
              <span style={{ fontSize: 14, fontWeight: 700 }}>
                {jobIsRunning ? 'Downloading schemas...' : activeJob.status === 'completed' ? '✅ Download complete' : activeJob.status === 'cancelled' ? '⛔ Cancelled' : '❌ Failed'}
              </span>
              <span style={{ fontSize: 12, color: 'var(--text-secondary)', background: 'rgba(255,255,255,0.08)', padding: '2px 8px', borderRadius: 10 }}>
                {mpLabel(activeJob.marketplaceId)}
              </span>
            </div>
            <div style={{ display: 'flex', alignItems: 'center', gap: 12, fontSize: 13 }}>
              <span style={{ color: '#22c55e' }}>✓ {activeJob.downloaded}</span>
              {activeJob.failed > 0 && <span style={{ color: '#ef4444' }}>✗ {activeJob.failed}</span>}
              <span style={{ color: 'var(--text-secondary)' }}>/ {activeJob.totalTypes}</span>
              {jobIsRunning && (
                <button onClick={cancelJob} style={{ ...btnSmall, background: '#ef4444', color: '#fff', fontSize: 11 }}>Cancel</button>
              )}
            </div>
          </div>

          {/* Progress bar */}
          <div style={{ height: 6, background: 'rgba(255,255,255,0.08)', borderRadius: 3, overflow: 'hidden' }}>
            <div style={{
              height: '100%', borderRadius: 3, transition: 'width 0.5s ease',
              width: `${jobProgress}%`,
              background: activeJob.status === 'completed' ? '#22c55e' : activeJob.status === 'cancelled' ? '#f59e0b' : '#ff9900',
            }} />
          </div>

          {/* Errors summary */}
          {activeJob.errors && activeJob.errors.length > 0 && (
            <details style={{ marginTop: 8, fontSize: 11, color: '#fca5a5' }}>
              <summary style={{ cursor: 'pointer' }}>{activeJob.errors.length} errors (click to expand)</summary>
              <div style={{ maxHeight: 120, overflowY: 'auto', marginTop: 4, fontFamily: 'monospace', fontSize: 10, lineHeight: 1.5 }}>
                {activeJob.errors.slice(0, 50).map((e, i) => <div key={i}>{e}</div>)}
                {activeJob.errors.length > 50 && <div>... and {activeJob.errors.length - 50} more</div>}
              </div>
            </details>
          )}
        </div>
      )}

      {/* ── Two-panel layout ── */}
      <div style={{ display: 'flex', gap: 20, alignItems: 'flex-start' }}>

        {/* ── LEFT PANEL ── */}
        <div style={{ width: 380, flexShrink: 0 }}>

          {/* Download section */}
          <Card title="Download Schema" subtitle="Search product types or download all for this marketplace">
            <div style={{ display: 'flex', gap: 8, marginBottom: 8 }}>
              <input
                value={ptSearch} onChange={e => setPtSearch(e.target.value)}
                onKeyDown={e => e.key === 'Enter' && searchProductTypes()}
                placeholder="e.g. TOY_FIGURE, SHIRT, LAPTOP..."
                style={{ ...inputStyle, flex: 1 }}
              />
              <button onClick={searchProductTypes} disabled={ptSearching || !ptSearch.trim()} style={btnPrimary}>
                {ptSearching ? '...' : '🔍'}
              </button>
            </div>

            {/* Download All button */}
            <button
              onClick={downloadAll}
              disabled={startingJob || jobIsRunning}
              style={{
                ...btnPrimary, width: '100%', padding: '10px 16px',
                background: jobIsRunning ? 'var(--bg-tertiary, #334155)' : '#ff9900',
                color: jobIsRunning ? 'var(--text-secondary)' : '#000',
                opacity: startingJob ? 0.5 : 1,
              }}
            >
              {startingJob ? '⏳ Starting...' : jobIsRunning ? '⏳ Download in progress...' : `⬇️ Download All Schemas (${mpLabel(marketplace)})`}
            </button>
            <div style={{ fontSize: 11, color: 'var(--text-secondary)', marginTop: 4, textAlign: 'center' }}>
              Downloads all product types for {MARKETPLACES.find(m => m.id === marketplace)?.flag} {mpLabel(marketplace)} in the background
            </div>

            {/* Search results */}
            {ptResults.length > 0 && (
              <div style={{ marginTop: 8, maxHeight: 200, overflowY: 'auto' }}>
                {ptResults.map((pt: any) => {
                  const ptKey = pt.productType || pt.name;
                  const already = schemas.some(s => s.productType === ptKey);
                  return (
                    <div key={ptKey} style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '6px 8px', borderRadius: 6, background: 'var(--bg-secondary, #1e293b)', marginBottom: 4 }}>
                      <div>
                        <div style={{ fontSize: 13, fontWeight: 600 }}>{pt.displayName || ptKey}</div>
                        <div style={{ fontSize: 11, color: 'var(--text-secondary)', fontFamily: 'monospace' }}>{ptKey}</div>
                      </div>
                      <button onClick={() => downloadSchema(ptKey)} disabled={downloading === ptKey}
                        style={{ ...btnSmall, background: already ? 'var(--bg-tertiary, #334155)' : '#ff9900', color: already ? 'var(--text-secondary)' : '#000' }}>
                        {downloading === ptKey ? '⏳' : already ? '↻' : '↓'}
                      </button>
                    </div>
                  );
                })}
              </div>
            )}
          </Card>

          {/* Cached schemas */}
          <Card title={`Cached Schemas (${schemas.length})`} subtitle="Click to open and configure field groupings" style={{ marginTop: 16 }}>
            {loading ? <div style={{ padding: 16, textAlign: 'center', color: 'var(--text-secondary)' }}>Loading...</div> : (
              schemas.length === 0 ? (
                <div style={{ padding: 16, textAlign: 'center', color: 'var(--text-secondary)', fontSize: 13 }}>
                  No schemas cached yet. Search and download above, or use "Download All".
                </div>
              ) : (
                <div style={{ maxHeight: 500, overflowY: 'auto' }}>
                  {schemas.map(s => (
                    <div key={s.productType} onClick={() => openSchema(s.productType)}
                      style={{
                        display: 'flex', alignItems: 'center', justifyContent: 'space-between',
                        padding: '8px 10px', borderRadius: 6, marginBottom: 4, cursor: 'pointer',
                        background: activeSchema?.productType === s.productType ? 'rgba(255,153,0,0.15)' : 'var(--bg-secondary, #1e293b)',
                        border: activeSchema?.productType === s.productType ? '1px solid #ff9900' : '1px solid transparent',
                      }}>
                      <div style={{ flex: 1, minWidth: 0 }}>
                        <div style={{ fontSize: 13, fontWeight: 600, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                          {s.displayName || s.productType}
                        </div>
                        <div style={{ fontSize: 11, color: 'var(--text-secondary)', display: 'flex', gap: 8 }}>
                          <span style={{ fontFamily: 'monospace' }}>{s.productType}</span>
                          <span>{s.attrCount} fields</span>
                          {s.hasFieldConfig && <span style={{ color: '#22c55e' }}>✓ configured</span>}
                        </div>
                      </div>
                      <button onClick={e => { e.stopPropagation(); deleteSchema(s.productType); }}
                        style={{ ...btnSmall, background: 'transparent', color: '#ef4444', fontSize: 14 }} title="Delete">🗑</button>
                    </div>
                  ))}
                </div>
              )
            )}
          </Card>
        </div>

        {/* ── RIGHT PANEL: Schema viewer / editor ── */}
        <div style={{ flex: 1, minWidth: 0 }}>
          {!activeSchema ? (
            <div style={{ padding: 60, textAlign: 'center', color: 'var(--text-secondary)', background: 'var(--bg-secondary, #1e293b)', borderRadius: 12, border: '2px dashed var(--border, #374151)' }}>
              <div style={{ fontSize: 48, marginBottom: 12 }}>📋</div>
              <div style={{ fontSize: 15, fontWeight: 600 }}>Select a schema to view and configure fields</div>
              <div style={{ fontSize: 13, marginTop: 4 }}>Download a product type schema first, then click it to open</div>
            </div>
          ) : loadingSchema ? (
            <div style={{ padding: 60, textAlign: 'center', color: 'var(--text-secondary)' }}>Loading schema...</div>
          ) : (
            <div>
              {/* Schema header */}
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 16 }}>
                <div>
                  <h2 style={{ margin: 0, fontSize: 18, fontWeight: 700 }}>{activeSchema.displayName || activeSchema.productType}</h2>
                  <div style={{ fontSize: 12, color: 'var(--text-secondary)', display: 'flex', gap: 12, marginTop: 2 }}>
                    <span style={{ fontFamily: 'monospace' }}>{activeSchema.productType}</span>
                    <span>{totalFields} fields total</span>
                    <span style={{ color: '#ff9900' }}>{requiredFields} required</span>
                    <span>{Object.keys(customGroups).length} groups</span>
                    <span>{groupedFields} grouped</span>
                  </div>
                </div>
                <div style={{ display: 'flex', gap: 8 }}>
                  <button onClick={applySuggestedSubgroups} style={{ ...btnSmall, background: '#8b5cf6', color: '#fff' }} title="Split Safety into Battery, Hazmat, Toy, GPSR subgroups">
                    🧩 Split Safety
                  </button>
                  <button onClick={addGroup} style={{ ...btnSmall, background: 'var(--bg-tertiary, #334155)' }}>+ Group</button>
                  <button onClick={saveFieldConfig} disabled={saving} style={{ ...btnPrimary, opacity: saving ? 0.5 : 1 }}>
                    {saving ? 'Saving...' : '💾 Save Config'}
                  </button>
                </div>
              </div>

              {/* Field search */}
              <input value={fieldSearch} onChange={e => setFieldSearch(e.target.value)}
                placeholder="Search fields by name or title..."
                style={{ ...inputStyle, marginBottom: 12, width: '100%', maxWidth: 400 }} />

              {/* Groups */}
              <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
                {sortedGroups.map(([groupKey, group]) => {
                  const filteredFields = fieldSearch
                    ? group.fields.filter(f => {
                        const attr = attrMap.get(f);
                        const q = fieldSearch.toLowerCase();
                        return f.toLowerCase().includes(q) || (attr?.title || '').toLowerCase().includes(q);
                      })
                    : group.fields;

                  return (
                    <div key={groupKey} onDragOver={e => handleDragOver(e, groupKey)} onDrop={() => handleDrop(groupKey)}
                      style={{ background: 'var(--bg-secondary, #1e293b)', borderRadius: 10, border: dragOverGroup === groupKey ? '2px solid #ff9900' : '1px solid var(--border, #374151)', overflow: 'hidden' }}>
                      {/* Group header */}
                      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '10px 14px', background: 'rgba(255,255,255,0.03)', borderBottom: '1px solid var(--border, #374151)' }}>
                        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                          <span style={{ fontSize: 16 }}>{group.icon || '📁'}</span>
                          {editGroupKey === groupKey ? (
                            <input value={group.title} onChange={e => setCustomGroups({ ...customGroups, [groupKey]: { ...group, title: e.target.value } })}
                              onBlur={() => setEditGroupKey(null)} onKeyDown={e => e.key === 'Enter' && setEditGroupKey(null)}
                              autoFocus style={{ ...inputStyle, width: 200, padding: '4px 8px', fontSize: 13 }} />
                          ) : (
                            <span style={{ fontSize: 14, fontWeight: 700, cursor: 'pointer' }} onDoubleClick={() => setEditGroupKey(groupKey)}>
                              {group.title}
                            </span>
                          )}
                          <span style={{ fontSize: 11, color: 'var(--text-secondary)', background: 'rgba(255,255,255,0.08)', padding: '2px 8px', borderRadius: 10 }}>
                            {group.fields.length} fields
                          </span>
                        </div>
                        <div style={{ display: 'flex', gap: 4 }}>
                          <button onClick={() => setEditGroupKey(groupKey)} style={btnTiny} title="Rename">✏️</button>
                          <button onClick={() => {
                            const updated = { ...customGroups };
                            if (updated[groupKey]) {
                              updated[groupKey] = { ...updated[groupKey], order: Math.max(0, updated[groupKey].order - 1) };
                              for (const [k, v] of Object.entries(updated)) {
                                if (k !== groupKey && v.order === updated[groupKey].order) updated[k] = { ...v, order: v.order + 1 };
                              }
                            }
                            setCustomGroups(updated);
                          }} style={btnTiny} title="Move up">↑</button>
                          <button onClick={() => {
                            const updated = { ...customGroups };
                            if (updated[groupKey]) {
                              const maxOrder = Math.max(...Object.values(updated).map(v => v.order));
                              updated[groupKey] = { ...updated[groupKey], order: Math.min(maxOrder, updated[groupKey].order + 1) };
                              for (const [k, v] of Object.entries(updated)) {
                                if (k !== groupKey && v.order === updated[groupKey].order) updated[k] = { ...v, order: v.order - 1 };
                              }
                            }
                            setCustomGroups(updated);
                          }} style={btnTiny} title="Move down">↓</button>
                        </div>
                      </div>

                      {/* Fields */}
                      <div style={{ padding: '6px 8px', minHeight: 32 }}>
                        {filteredFields.length === 0 && !fieldSearch && (
                          <div style={{ padding: '8px 6px', fontSize: 12, color: 'var(--text-secondary)', fontStyle: 'italic' }}>Drop fields here</div>
                        )}
                        {filteredFields.map((fieldName, idx) => {
                          const attr = attrMap.get(fieldName);
                          if (!attr) return null;
                          const isHidden = hiddenFields.has(fieldName);
                          return (
                            <div key={fieldName} draggable onDragStart={() => handleDragStart(fieldName)}
                              style={{
                                display: 'flex', alignItems: 'center', gap: 8, padding: '6px 10px', marginBottom: 2, borderRadius: 6, cursor: 'grab',
                                background: dragField === fieldName ? 'rgba(255,153,0,0.2)' : isHidden ? 'rgba(0,0,0,0.2)' : 'var(--bg-primary, #0f172a)',
                                opacity: isHidden ? 0.4 : 1, border: '1px solid transparent',
                              }}>
                              <span style={{ fontSize: 10, color: 'var(--text-secondary)', cursor: 'grab', userSelect: 'none' }}>⣿</span>
                              <div style={{ display: 'flex', flexDirection: 'column', gap: 0 }}>
                                <button onClick={() => idx > 0 && moveField(groupKey, idx, idx - 1)} style={btnMicro} disabled={idx === 0}>▲</button>
                                <button onClick={() => idx < group.fields.length - 1 && moveField(groupKey, idx, idx + 1)} style={btnMicro} disabled={idx >= group.fields.length - 1}>▼</button>
                              </div>
                              <div style={{ flex: 1, minWidth: 0 }}>
                                <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                                  <span style={{ fontSize: 13, fontWeight: 600 }}>{attr.title}</span>
                                  {attr.required && <span style={{ fontSize: 9, fontWeight: 700, color: '#000', background: '#ff9900', padding: '1px 5px', borderRadius: 3 }}>REQ</span>}
                                  <span style={{ fontSize: 10, color: 'var(--text-secondary)', fontFamily: 'monospace' }}>{attr.type}</span>
                                  {attr.enumValues && <span style={{ fontSize: 10, color: '#8b5cf6' }}>enum({attr.enumValues.length})</span>}
                                </div>
                                <div style={{ fontSize: 11, color: 'var(--text-secondary)', fontFamily: 'monospace' }}>{fieldName}</div>
                              </div>
                              <button onClick={() => {
                                const next = new Set(hiddenFields);
                                if (next.has(fieldName)) next.delete(fieldName); else next.add(fieldName);
                                setHiddenFields(next);
                              }} style={{ ...btnTiny, fontSize: 12 }} title={isHidden ? 'Show field' : 'Hide field'}>
                                {isHidden ? '👁' : '🙈'}
                              </button>
                            </div>
                          );
                        })}
                      </div>
                    </div>
                  );
                })}
              </div>
            </div>
          )}
        </div>
      </div>

      {/* Spin animation for the loading emoji */}
      <style>{`@keyframes spin { from { transform: rotate(0deg); } to { transform: rotate(360deg); } }`}</style>
    </div>
  );
}

// ============================================================================
// SUB-COMPONENTS
// ============================================================================

function Card({ title, subtitle, children, style: extraStyle }: { title: string; subtitle?: string; children: React.ReactNode; style?: React.CSSProperties }) {
  return (
    <div style={{ background: 'var(--bg-secondary, #1e293b)', borderRadius: 12, border: '1px solid var(--border, #374151)', overflow: 'hidden', ...extraStyle }}>
      <div style={{ padding: '12px 14px', borderBottom: '1px solid var(--border, #374151)' }}>
        <div style={{ fontSize: 14, fontWeight: 700 }}>{title}</div>
        {subtitle && <div style={{ fontSize: 11, color: 'var(--text-secondary, #9ca3af)', marginTop: 2 }}>{subtitle}</div>}
      </div>
      <div style={{ padding: '10px 14px' }}>{children}</div>
    </div>
  );
}

function Banner({ type, text, onClose }: { type: 'error' | 'success'; text: string; onClose: () => void }) {
  const isErr = type === 'error';
  return (
    <div style={{
      padding: '10px 14px', borderRadius: 8, marginBottom: 12, fontSize: 13,
      display: 'flex', alignItems: 'center', justifyContent: 'space-between',
      background: isErr ? 'rgba(239,68,68,0.15)' : 'rgba(34,197,94,0.15)',
      border: `1px solid ${isErr ? '#ef4444' : '#22c55e'}`,
      color: isErr ? '#fca5a5' : '#86efac',
    }}>
      {text}
      <button onClick={onClose} style={{ background: 'transparent', border: 'none', color: 'inherit', fontSize: 16, cursor: 'pointer', padding: '0 4px' }}>×</button>
    </div>
  );
}

// ============================================================================
// STYLES
// ============================================================================

const inputStyle: React.CSSProperties = {
  padding: '8px 12px', borderRadius: 8, border: '1px solid var(--border, #374151)',
  background: 'var(--bg-primary, #0f172a)', color: 'var(--text-primary, #e5e7eb)',
  fontSize: 13, outline: 'none', boxSizing: 'border-box',
};
const selectStyle: React.CSSProperties = { ...inputStyle, cursor: 'pointer' };
const btnPrimary: React.CSSProperties = {
  padding: '8px 16px', borderRadius: 8, border: 'none', background: '#ff9900',
  color: '#000', fontWeight: 700, fontSize: 13, cursor: 'pointer',
};
const btnSmall: React.CSSProperties = {
  padding: '5px 10px', borderRadius: 6, border: 'none', background: 'var(--bg-tertiary, #334155)',
  color: 'var(--text-primary, #e5e7eb)', fontSize: 12, cursor: 'pointer', fontWeight: 600,
};
const btnTiny: React.CSSProperties = {
  padding: '2px 6px', borderRadius: 4, border: 'none', background: 'transparent',
  color: 'var(--text-secondary, #9ca3af)', fontSize: 11, cursor: 'pointer',
};
const btnMicro: React.CSSProperties = {
  padding: '0 3px', borderRadius: 2, border: 'none', background: 'transparent',
  color: 'var(--text-secondary, #9ca3af)', fontSize: 8, cursor: 'pointer', lineHeight: 1,
};

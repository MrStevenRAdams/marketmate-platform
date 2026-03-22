import { useState, useEffect, useCallback, useRef } from 'react';
import { getActiveTenantId } from '../contexts/TenantContext';
import './ImportExport.css';

const API_BASE = import.meta.env.VITE_API_URL ?? 'http://localhost:8080/api/v1';

function api(path: string, init?: RequestInit) {
  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: { 'X-Tenant-Id': getActiveTenantId(), ...(init?.headers || {}) },
  });
}

// ─── Types ─────────────────────────────────────────────────────────────────────

type ImportType = 'products' | 'listings' | 'prices' | 'inventory_basic' | 'inventory_delta' | 'inventory_advanced' | 'binrack_zone' | 'binrack_create_update' | 'binrack_item_restriction' | 'binrack_storage_group' | 'stock_migration';
type ExportType = 'products' | 'listings' | 'prices' | 'inventory_basic' | 'inventory_advanced' | 'rma' | 'purchase_orders' | 'shipments';
type FileFormat = 'csv' | 'xlsx';

interface RowError   { row: number; column: string; message: string; }
interface RowWarning { row: number; column: string; message: string; }

interface ValidationResult {
  total_rows: number; valid_rows: number;
  create_count: number; update_count: number;
  error_count: number; warning_count: number;
  errors: RowError[]; warnings: RowWarning[];
  unknown_locations?: string[];
}

interface ImportJob {
  job_id: string; import_type: string; filename: string;
  status: 'pending' | 'processing' | 'done' | 'failed';
  total_rows: number; processed_rows: number;
  created_count: number; updated_count: number; failed_count: number;
  created_at: string; updated_at: string;
  error_report?: RowError[];
}

interface FileSettings {
  delimiter: string;
  encoding: string;
  hasHeaderRow: boolean;
  escapeChar: string;
}

interface PreviewResult {
  headers: string[];
  preview_rows: string[][];
  required_fields: string[];
  optional_fields: string[];
  auto_mapping: Record<string, string>;
}

type Step = 'upload' | 'file_settings' | 'column_mapping' | 'validate' | 'apply' | 'done';

// ─── Constants ─────────────────────────────────────────────────────────────────

const IMPORT_TYPES: { value: ImportType; label: string; icon: string; desc: string; columns?: string }[] = [
  { value: 'products',                 label: 'Products',               icon: '📦', desc: 'Create or update product catalogue entries' },
  { value: 'listings',                 label: 'Listings',               icon: '🏪', desc: 'Create or update marketplace listings' },
  { value: 'prices',                   label: 'Price File',             icon: '💷', desc: 'Update prices across channels' },
  { value: 'inventory_basic',          label: 'Basic Inventory',        icon: '📊', desc: 'Set stock levels by SKU (overwrites existing quantity)' },
  { value: 'inventory_delta',          label: 'Stock Delta',            icon: '🔄', desc: 'Add or subtract from existing stock levels (positive = add, negative = remove)' },
  { value: 'inventory_advanced',       label: 'Advanced Inventory',     icon: '🗃️',  desc: 'Set stock per SKU, warehouse & location' },
  { value: 'binrack_zone',             label: 'Binrack Zone',           icon: '🗂️',  desc: 'Assign zones to binracks by name', columns: 'binrack_name, zone_name' },
  { value: 'binrack_create_update',    label: 'Binrack Create/Update',  icon: '📍', desc: 'Create or update bin rack locations', columns: 'name, barcode, binrack_type, zone_name, aisle, section, level, bin_number, capacity' },
  { value: 'binrack_item_restriction', label: 'Binrack Restrictions',   icon: '🚫', desc: 'Restrict binracks to specific SKUs', columns: 'binrack_name, sku' },
  { value: 'binrack_storage_group',    label: 'Binrack Storage Group',  icon: '📁', desc: 'Assign storage groups to binracks', columns: 'binrack_name, storage_group_name' },
  { value: 'stock_migration',          label: 'Stock Migration',        icon: '⚠️', desc: 'Destructive stock overwrite — use with caution', columns: 'sku, warehouse_id, binrack_name, quantity' },
];

const EXPORT_TYPES: { value: ExportType; label: string; icon: string; desc: string; simple?: boolean }[] = [
  { value: 'products',           label: 'Products',           icon: '📦', desc: 'All products with variants and bundles' },
  { value: 'listings',           label: 'Listings',           icon: '🏪', desc: 'All marketplace listings' },
  { value: 'prices',             label: 'Price File',         icon: '💷', desc: 'Products with all channel prices' },
  { value: 'inventory_basic',    label: 'Basic Inventory',    icon: '📊', desc: 'SKU and total stock quantity' },
  { value: 'inventory_advanced', label: 'Advanced Inventory', icon: '🗃️', desc: 'Stock per location and warehouse' },
  { value: 'rma',                label: 'RMAs',               icon: '↩️', desc: 'All returns / RMA records', simple: true },
  { value: 'purchase_orders',    label: 'Purchase Orders',    icon: '🛒', desc: 'All purchase orders', simple: true },
  { value: 'shipments',          label: 'Shipments',          icon: '📫', desc: 'All shipment records', simple: true },
];

const DEFAULT_FILE_SETTINGS: FileSettings = {
  delimiter: ',', encoding: 'utf-8', hasHeaderRow: true, escapeChar: '',
};

// ─── Main Page ─────────────────────────────────────────────────────────────────

export default function ImportExport() {
  const [activeTab, setActiveTab] = useState<'import' | 'export'>('export');
  return (
    <div className="ie-page">
      <div className="ie-header">
        <div className="ie-header-left">
          <h1>Import <span className="ie-slash">/</span> Export</h1>
          <p className="ie-subtitle">Bulk data management for products, listings, pricing, and inventory</p>
        </div>
      </div>
      <div className="ie-tabs">
        <button className={`ie-tab ${activeTab === 'export' ? 'active' : ''}`} onClick={() => setActiveTab('export')}>
          <span className="ie-tab-icon">⬆️</span> Export
        </button>
        <button className={`ie-tab ${activeTab === 'import' ? 'active' : ''}`} onClick={() => setActiveTab('import')}>
          <span className="ie-tab-icon">⬇️</span> Import
        </button>
      </div>
      <div className="ie-body">
        {activeTab === 'export' ? <ExportPanel /> : <ImportPanel />}
      </div>
    </div>
  );
}

// ─── Export Panel ──────────────────────────────────────────────────────────────

function ExportPanel() {
  const [selectedType, setSelectedType] = useState<ExportType>('products');
  const [format, setFormat] = useState<FileFormat>('csv');
  const [exporting, setExporting] = useState(false);
  const [error, setError] = useState('');

  const selected = EXPORT_TYPES.find(t => t.value === selectedType);
  const isSimple = selected?.simple === true;

  const doExport = async () => {
    setExporting(true);
    setError('');
    try {
      let res: Response;
      if (isSimple) {
        // Simple GET exports (RMA, PO, Shipments)
        res = await api(`/export/${selectedType === 'purchase_orders' ? 'purchase-orders' : selectedType}`);
      } else {
        res = await api('/export', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ type: selectedType, format, include_variants: true, include_bundles: true }),
        });
      }
      if (!res.ok) {
        const j = await res.json().catch(() => ({}));
        throw new Error((j as any).error || `Export failed (${res.status})`);
      }
      const contentType = res.headers.get('content-type') || '';
      if (contentType.includes('application/json')) {
        const data: any = await res.json();
        downloadAsCSV(data.headers, data.rows, data.filename.replace('.xlsx', '.csv'));
      } else {
        const blob = await res.blob();
        const cd = res.headers.get('content-disposition') || '';
        const fnMatch = cd.match(/filename=([^;]+)/);
        const fn = fnMatch ? fnMatch[1].trim() : `export_${selectedType}.csv`;
        triggerDownload(blob, fn);
      }
    } catch (e: any) {
      setError(e.message);
    } finally {
      setExporting(false);
    }
  };

  return (
    <div className="ie-export-panel">
      <div className="ie-section-title">Choose Export Type</div>
      <div className="ie-type-grid">
        {EXPORT_TYPES.map(t => (
          <button key={t.value} className={`ie-type-card ${selectedType === t.value ? 'selected' : ''}`}
            onClick={() => setSelectedType(t.value)}>
            <span className="ie-type-icon">{t.icon}</span>
            <span className="ie-type-name">{t.label}</span>
            <span className="ie-type-desc">{t.desc}</span>
          </button>
        ))}
      </div>

      {!isSimple && (
        <div className="ie-export-options">
          <div className="ie-section-title">Options</div>
          <div className="ie-format-row">
            <span className="ie-label">File format</span>
            <div className="ie-format-btns">
              <button className={`ie-fmt-btn ${format === 'csv' ? 'active' : ''}`} onClick={() => setFormat('csv')}>CSV</button>
              <button className={`ie-fmt-btn ${format === 'xlsx' ? 'active' : ''}`} onClick={() => setFormat('xlsx')}>XLSX (Excel)</button>
            </div>
          </div>
        </div>
      )}

      {error && <div className="ie-error-banner">⚠️ {error}</div>}

      <div className="ie-export-action">
        <button className="ie-btn-primary" onClick={doExport} disabled={exporting}>
          {exporting
            ? <><span className="ie-spinner" /> Exporting…</>
            : <>{selected?.icon} Export {selected?.label}{isSimple ? '' : ` as ${format.toUpperCase()}`}</>}
        </button>
      </div>
    </div>
  );
}

// ─── Import Panel ──────────────────────────────────────────────────────────────

function ImportPanel() {
  const [importType, setImportType] = useState<ImportType>('products');
  const [step, setStep] = useState<Step>('upload');
  const [file, setFile] = useState<File | null>(null);

  // File settings (Step 1b: collapsible)
  const [fileSettings, setFileSettings] = useState<FileSettings>(DEFAULT_FILE_SETTINGS);
  const [fileSettingsOpen, setFileSettingsOpen] = useState(false);

  // Column mapping (Step 2)
  const [preview, setPreview] = useState<PreviewResult | null>(null);
  const [columnMapping, setColumnMapping] = useState<Record<string, string>>({}); // targetField → fileHeader
  const [previewing, setPreviewing] = useState(false);

  // Validate (Step 3)
  const [validation, setValidation] = useState<ValidationResult | null>(null);
  const [validating, setValidating] = useState(false);
  const [confirmLocations, setConfirmLocations] = useState(false);

  // Apply (Step 4)
  const [applying, setApplying] = useState(false);
  const [jobId, setJobId] = useState('');
  const [jobStatus, setJobStatus] = useState<ImportJob | null>(null);

  const [error, setError] = useState('');
  const fileInputRef = useRef<HTMLInputElement>(null);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const resetFlow = () => {
    setStep('upload');
    setFile(null);
    setPreview(null);
    setColumnMapping({});
    setValidation(null);
    setConfirmLocations(false);
    setJobId('');
    setJobStatus(null);
    setError('');
    if (pollRef.current) clearInterval(pollRef.current);
  };

  useEffect(() => {
    if (!jobId) return;
    const poll = async () => {
      try {
        const res = await api(`/import/status/${jobId}`);
        if (!res.ok) return;
        const job: ImportJob = await res.json();
        setJobStatus(job);
        if (job.status === 'done' || job.status === 'failed') {
          if (pollRef.current) clearInterval(pollRef.current);
          setStep('done');
        }
      } catch {}
    };
    poll();
    pollRef.current = setInterval(poll, 1500);
    return () => { if (pollRef.current) clearInterval(pollRef.current); };
  }, [jobId]);

  const handleFileDrop = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    const f = e.dataTransfer.files[0];
    if (f) { setFile(f); setError(''); }
  }, []);

  const handleFileInput = (e: React.ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0];
    if (f) { setFile(f); setError(''); }
  };

  // Build FormData with file settings
  const buildFormData = (extraFields?: Record<string, string>) => {
    const fd = new FormData();
    if (file) fd.append('file', file);
    fd.append('type', importType);
    fd.append('delimiter', fileSettings.delimiter);
    fd.append('encoding', fileSettings.encoding);
    fd.append('has_header_row', fileSettings.hasHeaderRow ? 'true' : 'false');
    fd.append('escape_char', fileSettings.escapeChar);
    if (Object.keys(columnMapping).length > 0) {
      fd.append('column_mapping', JSON.stringify(columnMapping));
    }
    if (extraFields) {
      for (const [k, v] of Object.entries(extraFields)) fd.append(k, v);
    }
    return fd;
  };

  // Step 1 → 2: Preview & column mapping
  const doPreview = async () => {
    if (!file) return;
    setPreviewing(true);
    setError('');
    try {
      const fd = buildFormData();
      const res = await api('/import/preview', { method: 'POST', body: fd });
      const data = await res.json() as any;
      if (!res.ok) throw new Error(data.error || 'Preview failed');
      setPreview(data as PreviewResult);
      // Initialise column mapping from auto_mapping
      setColumnMapping(data.auto_mapping || {});
      setStep('column_mapping');
    } catch (e: any) {
      setError(e.message);
    } finally {
      setPreviewing(false);
    }
  };

  // Step 2 → 3: Validate
  const doValidate = async () => {
    if (!file) return;
    setValidating(true);
    setError('');
    try {
      const fd = buildFormData();
      const res = await api('/import/validate', { method: 'POST', body: fd });
      const data = await res.json() as any;
      if (!res.ok) throw new Error(data.error || 'Validation failed');
      setValidation(data as ValidationResult);
      setStep('validate');
    } catch (e: any) {
      setError(e.message);
    } finally {
      setValidating(false);
    }
  };

  // Step 3 → 4: Apply
  const doApply = async () => {
    if (!file) return;
    setApplying(true);
    setError('');
    try {
      const fd = buildFormData(confirmLocations ? { confirm_unknown_locations: 'true' } : undefined);
      const res = await api('/import/apply', { method: 'POST', body: fd });
      const data = await res.json() as any;
      if (!res.ok) throw new Error(data.error || 'Apply failed');
      setJobId(data.job_id);
      setStep('apply');
    } catch (e: any) {
      setError(e.message);
    } finally {
      setApplying(false);
    }
  };

  const downloadTemplate = async () => {
    try {
      const res = await api(`/import/templates/${importType}`);
      if (!res.ok) throw new Error('Failed to download template');
      const blob = await res.blob();
      triggerDownload(blob, `${importType}_template.csv`);
    } catch (e: any) {
      setError(e.message);
    }
  };

  const hasBlockingErrors = validation && validation.error_count > 0;
  const hasUnknownLocations = validation && (validation.unknown_locations?.length || 0) > 0;
  const canProceed = validation && !hasBlockingErrors && (!hasUnknownLocations || confirmLocations);
  const selectedImport = IMPORT_TYPES.find(t => t.value === importType);

  // Step label map
  const stepLabels: Record<Step, string> = {
    upload: 'Upload',
    file_settings: 'File Settings',
    column_mapping: 'Column Mapping',
    validate: 'Validate',
    apply: 'Applying',
    done: 'Results',
  };
  const stepOrder: Step[] = ['upload', 'column_mapping', 'validate', 'apply', 'done'];

  return (
    <div className="ie-import-panel">
      {/* Type Selector */}
      <div className="ie-section-title">Choose Import Type</div>
      <div className="ie-type-grid">
        {IMPORT_TYPES.map(t => (
          <button key={t.value} className={`ie-type-card ${importType === t.value ? 'selected' : ''}`}
            onClick={() => { setImportType(t.value); resetFlow(); }}>
            <span className="ie-type-icon">{t.icon}</span>
            <span className="ie-type-name">{t.label}</span>
            <span className="ie-type-desc">{t.desc}</span>
          </button>
        ))}
      </div>

      {/* Column format hint for WMS types */}
      {selectedImport?.columns && (
        <div style={{ background: 'rgba(124,58,237,0.07)', border: '1px solid rgba(124,58,237,0.2)', borderRadius: 9, padding: '12px 18px', marginBottom: 16, fontSize: 13 }}>
          <span style={{ fontWeight: 700, color: '#7c3aed', marginRight: 8 }}>📋 Required columns:</span>
          <code style={{ fontFamily: 'monospace', color: 'var(--text-primary)', fontSize: 12 }}>{selectedImport.columns}</code>
          {selectedImport.value === 'stock_migration' && (
            <div style={{ marginTop: 8, color: '#ef4444', fontWeight: 600, fontSize: 12 }}>
              ⚠️ This import will <strong>destructively overwrite</strong> existing stock quantities. Ensure you have a backup.
            </div>
          )}
        </div>
      )}

      {/* Step indicator */}
      <div className="ie-steps">
        {stepOrder.map((s, i) => (
          <div key={s} className={`ie-step ${step === s ? 'current' : stepsAhead(step, s, stepOrder) ? '' : 'done'}`}>
            <span className="ie-step-num">{!stepsAhead(step, s, stepOrder) && step !== s ? '✓' : i + 1}</span>
            <span className="ie-step-label">{stepLabels[s]}</span>
          </div>
        ))}
      </div>

      {error && <div className="ie-error-banner">⚠️ {error}</div>}

      {/* ── Step 1: Upload ── */}
      {step === 'upload' && (
        <div className="ie-step-panel">
          <div className={`ie-dropzone ${file ? 'has-file' : ''}`}
            onDragOver={e => e.preventDefault()} onDrop={handleFileDrop}
            onClick={() => fileInputRef.current?.click()}>
            <input ref={fileInputRef} type="file" accept=".csv,.xlsx" style={{ display: 'none' }} onChange={handleFileInput} />
            {file ? (
              <div className="ie-file-selected">
                <span className="ie-file-icon">📄</span>
                <span className="ie-file-name">{file.name}</span>
                <span className="ie-file-size">{(file.size / 1024).toFixed(1)} KB</span>
              </div>
            ) : (
              <>
                <span className="ie-dz-icon">📁</span>
                <span className="ie-dz-text">Drop a CSV or XLSX file here, or <strong>click to browse</strong></span>
                <span className="ie-dz-hint">CSV or XLSX — configure parsing options below</span>
              </>
            )}
          </div>

          {/* ── File Settings (collapsible) ── */}
          <div className="ie-file-settings">
            <button className="ie-file-settings-toggle" onClick={() => setFileSettingsOpen(o => !o)}>
              ⚙️ File Settings {fileSettingsOpen ? '▲' : '▼'}
            </button>
            {fileSettingsOpen && (
              <div className="ie-file-settings-body">
                <div className="ie-fs-row">
                  <label className="ie-fs-label">Delimiter</label>
                  <select className="ie-fs-select" value={fileSettings.delimiter}
                    onChange={e => setFileSettings(s => ({ ...s, delimiter: e.target.value }))}>
                    <option value=",">, (comma)</option>
                    <option value="tab">⇥  (tab)</option>
                    <option value=";">; (semicolon)</option>
                    <option value="|">| (pipe)</option>
                  </select>
                </div>
                <div className="ie-fs-row">
                  <label className="ie-fs-label">Encoding</label>
                  <select className="ie-fs-select" value={fileSettings.encoding}
                    onChange={e => setFileSettings(s => ({ ...s, encoding: e.target.value }))}>
                    <option value="utf-8">UTF-8 (default)</option>
                    <option value="latin-1">Latin-1 / ISO-8859-1</option>
                  </select>
                </div>
                <div className="ie-fs-row">
                  <label className="ie-fs-label">Has Header Row</label>
                  <input type="checkbox" checked={fileSettings.hasHeaderRow}
                    onChange={e => setFileSettings(s => ({ ...s, hasHeaderRow: e.target.checked }))} />
                </div>
                <div className="ie-fs-row">
                  <label className="ie-fs-label">Escape Character</label>
                  <select className="ie-fs-select" value={fileSettings.escapeChar}
                    onChange={e => setFileSettings(s => ({ ...s, escapeChar: e.target.value }))}>
                    <option value="">Default (standard CSV quoting)</option>
                    <option value="\\">\ (backslash)</option>
                  </select>
                </div>
              </div>
            )}
          </div>

          <div className="ie-upload-footer">
            <button className="ie-btn-ghost" onClick={downloadTemplate}>
              ⬇ Download {selectedImport?.label} template
            </button>
            <button className="ie-btn-primary" onClick={doPreview} disabled={!file || previewing}>
              {previewing ? <><span className="ie-spinner" /> Loading…</> : 'Map Columns →'}
            </button>
          </div>
        </div>
      )}

      {/* ── Step 2: Column Mapping ── */}
      {step === 'column_mapping' && preview && (
        <div className="ie-step-panel">
          <div className="ie-section-title">Column Mapping</div>
          <p className="ie-mapping-hint">
            Map each required field to a column in your file. Optional fields can be left unmapped.
          </p>

          {/* Preview table */}
          {preview.preview_rows.length > 0 && (
            <div className="ie-table-wrap ie-preview-table-wrap">
              <table className="ie-table">
                <thead>
                  <tr>{preview.headers.map(h => <th key={h}>{h}</th>)}</tr>
                </thead>
                <tbody>
                  {preview.preview_rows.map((row, ri) => (
                    <tr key={ri}>{preview.headers.map((_, ci) => <td key={ci}>{row[ci] ?? ''}</td>)}</tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          {/* Mapping rows */}
          <div className="ie-mapping-grid">
            <div className="ie-mapping-header-row">
              <span>Target Field</span>
              <span>Source Column in Your File</span>
            </div>
            {[
              ...preview.required_fields.map(f => ({ field: f, required: true })),
              ...preview.optional_fields.map(f => ({ field: f, required: false })),
            ].map(({ field, required }) => (
              <div key={field} className={`ie-mapping-row ${required ? 'required' : ''}`}>
                <span className="ie-mapping-field">
                  {field} {required && <span className="ie-required-badge">required</span>}
                </span>
                <select className="ie-mapping-select"
                  value={columnMapping[field] || ''}
                  onChange={e => setColumnMapping(m => ({ ...m, [field]: e.target.value }))}>
                  <option value="">— not mapped —</option>
                  {preview.headers.map(h => <option key={h} value={h}>{h}</option>)}
                </select>
              </div>
            ))}
          </div>

          <div className="ie-validate-footer">
            <button className="ie-btn-ghost" onClick={resetFlow}>← Re-upload</button>
            <button className="ie-btn-primary" onClick={doValidate} disabled={validating}>
              {validating ? <><span className="ie-spinner" /> Validating…</> : 'Validate →'}
            </button>
          </div>
        </div>
      )}

      {/* ── Step 3: Validate ── */}
      {step === 'validate' && validation && (
        <div className="ie-step-panel">
          <div className="ie-val-summary">
            <div className="ie-val-stat ie-stat-total"><span className="ie-stat-num">{validation.total_rows}</span><span className="ie-stat-lbl">Total rows</span></div>
            <div className="ie-val-stat ie-stat-ok"><span className="ie-stat-num">✅ {validation.valid_rows}</span><span className="ie-stat-lbl">Valid</span></div>
            {validation.create_count > 0 && <div className="ie-val-stat ie-stat-create"><span className="ie-stat-num">+ {validation.create_count}</span><span className="ie-stat-lbl">To create</span></div>}
            {validation.update_count > 0 && <div className="ie-val-stat ie-stat-update"><span className="ie-stat-num">↑ {validation.update_count}</span><span className="ie-stat-lbl">To update</span></div>}
            {validation.error_count > 0 && <div className="ie-val-stat ie-stat-err"><span className="ie-stat-num">❌ {validation.error_count}</span><span className="ie-stat-lbl">Errors</span></div>}
            {validation.warning_count > 0 && <div className="ie-val-stat ie-stat-warn"><span className="ie-stat-num">⚠️ {validation.warning_count}</span><span className="ie-stat-lbl">Warnings</span></div>}
          </div>

          {validation.errors.length > 0 && (
            <div className="ie-issues-section">
              <div className="ie-issues-title ie-err-title">❌ Errors — fix and re-upload before proceeding</div>
              <div className="ie-table-wrap">
                <table className="ie-table">
                  <thead><tr><th>Row</th><th>Column</th><th>Issue</th></tr></thead>
                  <tbody>
                    {validation.errors.slice(0, 50).map((e, i) => (
                      <tr key={i}><td>Row {e.row}</td><td><code>{e.column}</code></td><td>{e.message}</td></tr>
                    ))}
                    {validation.errors.length > 50 && <tr><td colSpan={3} className="ie-more-rows">…and {validation.errors.length - 50} more errors</td></tr>}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {validation.warnings.length > 0 && (
            <div className="ie-issues-section">
              <div className="ie-issues-title ie-warn-title">⚠️ Warnings</div>
              <div className="ie-table-wrap">
                <table className="ie-table">
                  <thead><tr><th>Row</th><th>Column</th><th>Issue</th></tr></thead>
                  <tbody>
                    {validation.warnings.slice(0, 20).map((w, i) => (
                      <tr key={i} className="ie-warn-row"><td>Row {w.row}</td><td><code>{w.column}</code></td><td>{w.message}</td></tr>
                    ))}
                    {validation.warnings.length > 20 && <tr><td colSpan={3} className="ie-more-rows">…and {validation.warnings.length - 20} more warnings</td></tr>}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {hasUnknownLocations && (
            <div className="ie-location-confirm">
              <div className="ie-location-warn-header">⚠️ {validation.unknown_locations!.length} warehouse location{validation.unknown_locations!.length > 1 ? 's' : ''} not found and will be auto-created:</div>
              <ul className="ie-location-list">
                {validation.unknown_locations!.map(loc => <li key={loc}>• {loc}</li>)}
              </ul>
              <label className="ie-checkbox-row">
                <input type="checkbox" checked={confirmLocations} onChange={e => setConfirmLocations(e.target.checked)} />
                <span>I confirm — proceed and create these locations</span>
              </label>
            </div>
          )}

          <div className="ie-validate-footer">
            <button className="ie-btn-ghost" onClick={() => setStep('column_mapping')}>← Back to mapping</button>
            {hasBlockingErrors ? (
              <span className="ie-err-msg">Fix {validation.error_count} error{validation.error_count > 1 ? 's' : ''} before proceeding</span>
            ) : (
              <button className="ie-btn-primary" onClick={doApply} disabled={!canProceed || applying}>
                {applying ? <><span className="ie-spinner" /> Applying…</> : `Apply import (${validation.valid_rows} rows) →`}
              </button>
            )}
          </div>
        </div>
      )}

      {/* ── Step 4: Apply / Progress ── */}
      {step === 'apply' && (
        <div className="ie-step-panel ie-progress-panel">
          <div className="ie-progress-title">⏳ Processing import…</div>
          {jobStatus && (
            <>
              <div className="ie-progress-bar-wrap">
                <div className="ie-progress-bar"
                  style={{ width: jobStatus.total_rows > 0 ? `${Math.round((jobStatus.processed_rows / jobStatus.total_rows) * 100)}%` : '10%' }} />
              </div>
              <div className="ie-progress-text">{jobStatus.processed_rows} / {jobStatus.total_rows} rows processed</div>
            </>
          )}
        </div>
      )}

      {/* ── Step 5: Done ── */}
      {step === 'done' && jobStatus && (
        <div className="ie-step-panel ie-done-panel">
          <div className={`ie-done-icon ${jobStatus.failed_count === 0 ? 'success' : 'partial'}`}>
            {jobStatus.failed_count === 0 ? '✅' : '⚠️'}
          </div>
          <div className="ie-done-title">{jobStatus.status === 'done' ? 'Import complete' : 'Import failed'}</div>
          <div className="ie-done-stats">
            {jobStatus.created_count > 0 && <div className="ie-done-stat"><strong>{jobStatus.created_count}</strong> created</div>}
            {jobStatus.updated_count > 0 && <div className="ie-done-stat"><strong>{jobStatus.updated_count}</strong> updated</div>}
            {jobStatus.failed_count > 0 && <div className="ie-done-stat ie-stat-failed"><strong>{jobStatus.failed_count}</strong> failed</div>}
          </div>
          {jobStatus.error_report && jobStatus.error_report.length > 0 && (
            <div className="ie-done-errors">
              <div className="ie-issues-title ie-err-title">Failed rows</div>
              <div className="ie-table-wrap">
                <table className="ie-table">
                  <thead><tr><th>Row</th><th>Column</th><th>Error</th></tr></thead>
                  <tbody>
                    {jobStatus.error_report.slice(0, 20).map((e, i) => (
                      <tr key={i}><td>Row {e.row}</td><td><code>{e.column}</code></td><td>{e.message}</td></tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}
          <div className="ie-done-actions">
            <button className="ie-btn-primary" onClick={resetFlow}>Start another import</button>
          </div>
        </div>
      )}

      <ImportHistory />
    </div>
  );
}

// ─── Import History ────────────────────────────────────────────────────────────

function ImportHistory() {
  const [jobs, setJobs] = useState<ImportJob[]>([]);
  const [loading, setLoading] = useState(true);
  const [deleting, setDeleting] = useState<string | null>(null);
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null);

  const fetchJobs = () => {
    api('/import/history')
      .then(r => r.json())
      .then((d: any) => setJobs(d.jobs || []))
      .catch(() => {})
      .finally(() => setLoading(false));
  };

  useEffect(() => { fetchJobs(); }, []);

  const handleDelete = async (jobId: string) => {
    setDeleting(jobId);
    setConfirmDelete(null);
    try {
      const res = await api(`/import/jobs/${jobId}`, { method: 'DELETE' });
      if (res.ok) {
        setJobs(prev => prev.filter(j => j.job_id !== jobId));
      }
    } catch {}
    finally { setDeleting(null); }
  };

  if (loading || jobs.length === 0) return null;

  return (
    <div className="ie-history">
      <div className="ie-section-title">Import History</div>
      <div className="ie-table-wrap">
        <table className="ie-table ie-history-table">
          <thead>
            <tr>
              <th>Type</th><th>File</th><th>Date</th><th>Rows</th>
              <th>Created</th><th>Updated</th><th>Failed</th><th>Status</th><th></th>
            </tr>
          </thead>
          <tbody>
            {jobs.map(j => (
              <tr key={j.job_id}>
                <td><span className="ie-type-badge">{j.import_type}</span></td>
                <td className="ie-filename">{j.filename}</td>
                <td>{new Date(j.created_at).toLocaleString()}</td>
                <td>{j.total_rows}</td>
                <td>{j.created_count}</td>
                <td>{j.updated_count}</td>
                <td>{j.failed_count > 0 ? <span className="ie-failed-num">{j.failed_count}</span> : 0}</td>
                <td><span className={`ie-status-badge ie-status-${j.status}`}>{j.status}</span></td>
                <td>
                  {confirmDelete === j.job_id ? (
                    <span className="ie-confirm-delete">
                      Delete?{' '}
                      <button className="ie-link-btn ie-link-danger" onClick={() => handleDelete(j.job_id)} disabled={deleting === j.job_id}>
                        {deleting === j.job_id ? '…' : 'Yes'}
                      </button>
                      {' / '}
                      <button className="ie-link-btn" onClick={() => setConfirmDelete(null)}>No</button>
                    </span>
                  ) : (
                    <button className="ie-icon-btn ie-trash-btn" title="Delete job" onClick={() => setConfirmDelete(j.job_id)}>
                      🗑️
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

// ─── Utilities ─────────────────────────────────────────────────────────────────

function stepsAhead(current: Step, step: Step, order: Step[]): boolean {
  return order.indexOf(current) < order.indexOf(step);
}

function triggerDownload(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url; a.download = filename;
  document.body.appendChild(a); a.click(); a.remove();
  URL.revokeObjectURL(url);
}

function downloadAsCSV(headers: string[], rows: string[][], filename: string) {
  const lines = [headers, ...rows].map(r =>
    r.map(cell => `"${(cell ?? '').replace(/"/g, '""')}"`).join(',')
  );
  const blob = new Blob([lines.join('\n')], { type: 'text/csv;charset=utf-8;' });
  triggerDownload(blob, filename);
}

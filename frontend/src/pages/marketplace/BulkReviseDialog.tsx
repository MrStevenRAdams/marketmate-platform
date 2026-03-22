// ============================================================================
// BULK REVISE DIALOG — SESSION F (USP-03)
// ============================================================================
// Location: frontend/src/pages/marketplace/BulkReviseDialog.tsx
//
// Opened from the ListingList bulk actions menu when ≥1 listings are selected.
// USP-03 adds a "Preview Changes" step before the revise is applied:
//   Step 1 (edit)    — pick fields + supply values
//   Step 2 (preview) — diff table: current vs proposed per listing
//   Step 3 (done)    — result summary
// ============================================================================

import { useState } from 'react';
import { listingService } from '../../services/marketplace-api';

// ── Styles ───────────────────────────────────────────────────────────────────

const labelStyle: React.CSSProperties = {
  display: 'block', fontSize: 11, fontWeight: 700, color: 'var(--text-muted)',
  textTransform: 'uppercase', letterSpacing: '0.5px', marginBottom: 6,
};

const inputStyle: React.CSSProperties = {
  width: '100%', padding: '10px 14px', borderRadius: 8,
  background: 'var(--bg-primary)', border: '1px solid var(--border-bright)',
  color: 'var(--text-primary)', fontSize: 14, outline: 'none',
  boxSizing: 'border-box',
};

// ── Field definitions ────────────────────────────────────────────────────────

const FIELDS: { key: string; label: string; description: string }[] = [
  { key: 'title',       label: 'Title',       description: 'Override the listing title on all selected listings.' },
  { key: 'description', label: 'Description', description: 'Override the listing description on all selected listings.' },
  { key: 'price',       label: 'Price',       description: 'Set a channel-specific price override on all selected listings.' },
  { key: 'attributes',  label: 'Attributes',  description: 'Merge key/value attribute overrides onto all selected listings.' },
  { key: 'images',      label: 'Images',      description: 'Replace image override URLs on all selected listings (one URL per line).' },
];

// ── Types ─────────────────────────────────────────────────────────────────────

interface BulkReviseResult {
  succeeded: number;
  failed: number;
  errors?: { listing_id: string; error: string }[];
}

interface PreviewItem {
  listing_id: string;
  title: string;
  channel: string;
  current: Record<string, any>;
  proposed: Record<string, any>;
}

interface AttrRow {
  key: string;
  value: string;
}

type DialogStep = 'edit' | 'preview' | 'done';

interface Props {
  listingIds: string[];
  onClose: () => void;
  onComplete: () => void;
}

// ── Component ─────────────────────────────────────────────────────────────────

export default function BulkReviseDialog({ listingIds, onClose, onComplete }: Props) {
  const [step, setStep] = useState<DialogStep>('edit');

  const [selectedFields, setSelectedFields] = useState<Set<string>>(new Set());
  const [title, setTitle]               = useState('');
  const [description, setDescription]   = useState('');
  const [price, setPrice]               = useState('');
  const [attrRows, setAttrRows]         = useState<AttrRow[]>([{ key: '', value: '' }]);
  const [images, setImages]             = useState('');

  const [loading, setLoading]   = useState(false);
  const [previews, setPreviews] = useState<PreviewItem[]>([]);
  const [result, setResult]     = useState<BulkReviseResult | null>(null);
  const [error, setError]       = useState('');

  // ── Field toggle ──────────────────────────────────────────────────────────

  function toggleField(key: string) {
    setSelectedFields(prev => {
      const next = new Set(prev);
      next.has(key) ? next.delete(key) : next.add(key);
      return next;
    });
  }

  // ── Attribute row helpers ─────────────────────────────────────────────────

  function setAttrRowKey(idx: number, key: string) {
    setAttrRows(prev => prev.map((r, i) => i === idx ? { ...r, key } : r));
  }
  function setAttrRowValue(idx: number, value: string) {
    setAttrRows(prev => prev.map((r, i) => i === idx ? { ...r, value } : r));
  }
  function addAttrRow() {
    setAttrRows(prev => [...prev, { key: '', value: '' }]);
  }
  function removeAttrRow(idx: number) {
    setAttrRows(prev => prev.filter((_, i) => i !== idx));
  }

  // ── Build request payload (shared by preview + apply) ────────────────────

  function buildRequest(): { fields: string[]; fieldValues: any } | null {
    if (selectedFields.size === 0) {
      setError('Select at least one field to revise.');
      return null;
    }
    const fields = Array.from(selectedFields);
    let parsedPrice: number | undefined;
    if (selectedFields.has('price')) {
      parsedPrice = parseFloat(price);
      if (isNaN(parsedPrice) || parsedPrice < 0) {
        setError('Price must be a valid non-negative number.');
        return null;
      }
    }
    const fieldValues: any = {};
    if (selectedFields.has('title')) fieldValues.title = title;
    if (selectedFields.has('description')) fieldValues.description = description;
    if (selectedFields.has('price')) fieldValues.price = parsedPrice;
    if (selectedFields.has('attributes')) {
      const attrs: Record<string, string> = {};
      for (const row of attrRows) {
        if (row.key.trim()) attrs[row.key.trim()] = row.value;
      }
      fieldValues.attributes = attrs;
    }
    if (selectedFields.has('images')) {
      fieldValues.images = images.split('\n').map((u: string) => u.trim()).filter((u: string) => u.length > 0);
    }
    return { fields, fieldValues };
  }

  // ── Preview: fetch diff — no writes ──────────────────────────────────────

  async function handlePreview() {
    setError('');
    const req = buildRequest();
    if (!req) return;
    setLoading(true);
    try {
      const res = await listingService.bulkRevisePreview(listingIds, req.fields, req.fieldValues);
      setPreviews(res.data.previews || []);
      setStep('preview');
    } catch (e: any) {
      setError(e.response?.data?.error || 'Preview failed. Please try again.');
    } finally {
      setLoading(false);
    }
  }

  // ── Apply: commit after user confirms diff ────────────────────────────────

  async function handleApply() {
    setError('');
    const req = buildRequest();
    if (!req) return;
    setLoading(true);
    try {
      const res = await listingService.bulkRevise(listingIds, req.fields, req.fieldValues);
      setResult(res.data);
      setStep('done');
    } catch (e: any) {
      setError(e.response?.data?.error || 'Bulk revise failed. Please try again.');
    } finally {
      setLoading(false);
    }
  }

  function handleDone() {
    onComplete();
    onClose();
  }

  // ── Render ────────────────────────────────────────────────────────────────

  const headerTitle =
    step === 'preview' ? '🔍 Preview Changes' :
    step === 'done'    ? '✅ Revise Complete'  :
                         '📝 Revise Fields';

  return (
    <div
      style={{
        position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.6)',
        display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000,
      }}
      onClick={() => !loading && step === 'edit' && onClose()}
    >
      <div
        style={{
          background: 'var(--bg-secondary)', border: '1px solid var(--border)',
          borderRadius: 12, padding: 28, maxWidth: 600, width: '90%',
          maxHeight: '90vh', overflowY: 'auto',
        }}
        onClick={e => e.stopPropagation()}
      >
        {/* Header */}
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', marginBottom: 6 }}>
          <h3 style={{ fontSize: 17, fontWeight: 700, color: 'var(--text-primary)', margin: 0 }}>
            {headerTitle}
          </h3>
          {!loading && (
            <button
              onClick={step === 'done' ? handleDone : onClose}
              style={{ background: 'none', border: 'none', color: 'var(--text-muted)', cursor: 'pointer', fontSize: 20, lineHeight: 1, padding: 0 }}
            >
              ×
            </button>
          )}
        </div>

        {/* ── STEP: DONE ──────────────────────────────────────────────────── */}
        {step === 'done' && result && (
          <>
            <div style={{
              padding: '16px 20px', borderRadius: 10, marginBottom: 16,
              background: result.failed === 0 ? 'var(--success-glow)' : 'var(--bg-tertiary)',
              border: `1px solid ${result.failed === 0 ? 'var(--success)' : 'var(--border)'}`,
            }}>
              <div style={{ fontSize: 15, fontWeight: 700, color: 'var(--text-primary)', marginBottom: 8 }}>
                Revise complete
              </div>
              <div style={{ display: 'flex', gap: 20 }}>
                <span style={{ fontSize: 13, color: 'var(--success)' }}>✅ {result.succeeded} succeeded</span>
                {result.failed > 0 && (
                  <span style={{ fontSize: 13, color: 'var(--danger)' }}>❌ {result.failed} failed</span>
                )}
              </div>
            </div>

            {result.errors && result.errors.length > 0 && (
              <div style={{ marginBottom: 16 }}>
                <div style={{ fontSize: 12, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.5px', marginBottom: 8 }}>
                  Errors
                </div>
                <div style={{ background: 'var(--bg-primary)', border: '1px solid var(--border)', borderRadius: 8, overflow: 'hidden' }}>
                  {result.errors.map((e, i) => (
                    <div key={i} style={{
                      padding: '8px 12px',
                      borderBottom: i < result.errors!.length - 1 ? '1px solid var(--border)' : 'none',
                      fontSize: 12,
                    }}>
                      <span style={{ fontFamily: 'monospace', color: 'var(--text-secondary)' }}>{e.listing_id}</span>
                      <span style={{ color: 'var(--danger)', marginLeft: 8 }}>{e.error}</span>
                    </div>
                  ))}
                </div>
              </div>
            )}

            <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
              <button className="btn btn-primary" onClick={handleDone}>Done</button>
            </div>
          </>
        )}

        {/* ── STEP: PREVIEW ───────────────────────────────────────────────── */}
        {step === 'preview' && (
          <>
            <p style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 16 }}>
              Review the changes below.{' '}
              <strong style={{ color: 'var(--text-primary)' }}>{previews.length} listing{previews.length !== 1 ? 's' : ''}</strong>{' '}
              will be updated. Click <strong>Confirm &amp; Apply</strong> to proceed.
            </p>

            <div style={{ display: 'flex', flexDirection: 'column', gap: 10, marginBottom: 20, maxHeight: 360, overflowY: 'auto' }}>
              {previews.map((item, idx) => (
                <div key={idx} style={{ background: 'var(--bg-primary)', border: '1px solid var(--border)', borderRadius: 8, padding: '12px 14px' }}>
                  <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 10 }}>
                    <span style={{ fontSize: 13, fontWeight: 600, color: 'var(--text-primary)' }}>{item.title}</span>
                    <span style={{ fontSize: 11, color: 'var(--text-muted)', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 4, padding: '2px 8px' }}>{item.channel}</span>
                  </div>
                  {Object.keys(item.proposed).map(field => (
                    <div key={field} style={{ marginBottom: 8 }}>
                      <div style={{ fontSize: 10, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.5px', marginBottom: 4 }}>{field}</div>
                      <div style={{ display: 'grid', gridTemplateColumns: '1fr 20px 1fr', gap: 6, alignItems: 'center' }}>
                        <div style={{ background: 'var(--bg-secondary)', borderRadius: 4, padding: '5px 8px', fontSize: 12, color: 'var(--text-muted)', wordBreak: 'break-all', maxHeight: 60, overflow: 'hidden' }}>
                          {String(item.current[field] ?? '(not set)').substring(0, 100)}
                        </div>
                        <div style={{ textAlign: 'center', fontSize: 12, color: 'var(--text-muted)' }}>→</div>
                        <div style={{ background: 'rgba(99,102,241,0.08)', border: '1px solid var(--primary)', borderRadius: 4, padding: '5px 8px', fontSize: 12, color: 'var(--primary)', wordBreak: 'break-all', maxHeight: 60, overflow: 'hidden' }}>
                          {String(item.proposed[field] ?? '').substring(0, 100)}
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              ))}
            </div>

            {error && (
              <div style={{ padding: '10px 14px', borderRadius: 8, marginBottom: 16, background: 'var(--danger-glow)', border: '1px solid var(--danger)', color: 'var(--danger)', fontSize: 12 }}>
                {error}
              </div>
            )}

            <div style={{ display: 'flex', gap: 10, justifyContent: 'flex-end' }}>
              <button className="btn btn-secondary" onClick={() => setStep('edit')} disabled={loading}>
                ← Back
              </button>
              <button
                className="btn btn-primary"
                onClick={handleApply}
                disabled={loading}
              >
                {loading ? '⏳ Applying…' : `✅ Confirm & Apply to ${listingIds.length} listing${listingIds.length !== 1 ? 's' : ''}`}
              </button>
            </div>
          </>
        )}

        {/* ── STEP: EDIT ──────────────────────────────────────────────────── */}
        {step === 'edit' && (
          <>
            <p style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 20 }}>
              Apply field overrides to{' '}
              <strong style={{ color: 'var(--text-primary)' }}>{listingIds.length}</strong>{' '}
              selected listing{listingIds.length !== 1 ? 's' : ''}.{' '}
              You'll preview changes before they're applied.
            </p>

            {/* ── Field checkboxes ── */}
            <div style={{ display: 'flex', flexDirection: 'column', gap: 8, marginBottom: 20 }}>
              {FIELDS.map(f => (
                <label key={f.key} style={{
                  display: 'flex', alignItems: 'flex-start', gap: 12, cursor: 'pointer',
                  padding: '10px 14px', borderRadius: 8,
                  background: selectedFields.has(f.key) ? 'var(--primary-glow)' : 'var(--bg-tertiary)',
                  border: `1px solid ${selectedFields.has(f.key) ? 'var(--primary)' : 'var(--border)'}`,
                  transition: 'all 150ms',
                }}>
                  <input
                    type="checkbox"
                    checked={selectedFields.has(f.key)}
                    onChange={() => toggleField(f.key)}
                    style={{ marginTop: 2, cursor: 'pointer', accentColor: 'var(--primary)' }}
                  />
                  <div>
                    <div style={{ fontSize: 14, fontWeight: 600, color: 'var(--text-primary)', marginBottom: 2 }}>
                      {f.label}
                    </div>
                    <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>{f.description}</div>
                  </div>
                </label>
              ))}
            </div>

            {/* ── Field inputs ── */}
            {selectedFields.has('title') && (
              <div style={{ marginBottom: 16 }}>
                <label style={labelStyle}>Title Override</label>
                <input
                  type="text"
                  value={title}
                  onChange={e => setTitle(e.target.value)}
                  placeholder="New title for all selected listings"
                  style={inputStyle}
                />
              </div>
            )}

            {selectedFields.has('description') && (
              <div style={{ marginBottom: 16 }}>
                <label style={labelStyle}>Description Override</label>
                <textarea
                  value={description}
                  onChange={e => setDescription(e.target.value)}
                  placeholder="New description for all selected listings"
                  rows={4}
                  style={{ ...inputStyle, resize: 'vertical' }}
                />
              </div>
            )}

            {selectedFields.has('price') && (
              <div style={{ marginBottom: 16 }}>
                <label style={labelStyle}>Price Override</label>
                <input
                  type="number"
                  min="0"
                  step="0.01"
                  value={price}
                  onChange={e => setPrice(e.target.value)}
                  placeholder="0.00"
                  style={{ ...inputStyle, width: 160 }}
                />
              </div>
            )}

            {selectedFields.has('attributes') && (
              <div style={{ marginBottom: 16 }}>
                <label style={labelStyle}>Attribute Overrides</label>
                <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                  {attrRows.map((row, i) => (
                    <div key={i} style={{ display: 'grid', gridTemplateColumns: '1fr 1fr auto', gap: 8 }}>
                      <input
                        type="text"
                        value={row.key}
                        onChange={e => setAttrRowKey(i, e.target.value)}
                        placeholder="Attribute name"
                        style={inputStyle}
                      />
                      <input
                        type="text"
                        value={row.value}
                        onChange={e => setAttrRowValue(i, e.target.value)}
                        placeholder="Value"
                        style={inputStyle}
                      />
                      <button
                        onClick={() => removeAttrRow(i)}
                        style={{ padding: '10px 12px', background: 'none', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--danger)', cursor: 'pointer', fontSize: 16 }}
                      >
                        ×
                      </button>
                    </div>
                  ))}
                  <button
                    onClick={addAttrRow}
                    style={{ padding: '8px 14px', background: 'var(--bg-primary)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text-primary)', cursor: 'pointer', fontSize: 13, alignSelf: 'flex-start' }}
                  >
                    + Add row
                  </button>
                </div>
              </div>
            )}

            {selectedFields.has('images') && (
              <div style={{ marginBottom: 16 }}>
                <label style={labelStyle}>Image URLs (one per line)</label>
                <textarea
                  value={images}
                  onChange={e => setImages(e.target.value)}
                  placeholder="https://example.com/image1.jpg&#10;https://example.com/image2.jpg"
                  rows={4}
                  style={{ ...inputStyle, resize: 'vertical' }}
                />
              </div>
            )}

            {error && (
              <div style={{
                padding: '10px 14px', borderRadius: 8, marginBottom: 16,
                background: 'var(--danger-glow)', border: '1px solid var(--danger)',
                color: 'var(--danger)', fontSize: 12,
              }}>
                {error}
              </div>
            )}

            <div style={{ display: 'flex', gap: 10, justifyContent: 'flex-end' }}>
              <button className="btn btn-secondary" onClick={onClose} disabled={loading}>
                Cancel
              </button>
              <button
                className="btn btn-primary"
                onClick={handlePreview}
                disabled={loading || selectedFields.size === 0}
              >
                {loading ? '⏳ Loading preview…' : '🔍 Preview Changes'}
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  );
}

import { useState } from 'react';
import { productService } from '../../services/api';

interface CreateWithAIModalProps {
  isOpen: boolean;
  onClose: () => void;
  /** Called when a draft product has been created — navigate to edit it */
  onProductCreated: (productId: string) => void;
}

type IdentifierType = 'EAN' | 'ASIN' | 'UPC' | 'ISBN';

interface LookupPreview {
  productId: string;
  title: string;
  brand?: string;
  imageUrl?: string;
  source: string;
  found: boolean;
}

interface LookupResponse {
  ok: boolean;
  found: boolean;
  productId?: string;
  preview?: LookupPreview;
  isVariation?: boolean;
  childCount?: number;
  message?: string;
  error?: string;
}

const IDENTIFIER_HINTS: Record<IdentifierType, string> = {
  EAN:  'e.g. 5012345678901 — uses eBay catalogue',
  ASIN: 'e.g. B08N5WRWNW — uses Amazon SP-API',
  UPC:  'e.g. 012345678905 — uses Amazon SP-API',
  ISBN: 'e.g. 9780141036144 — uses Amazon SP-API',
};

const SOURCE_LABEL: Record<string, string> = {
  ebay:   '📦 eBay catalogue',
  amazon: '📦 Amazon catalogue',
};

export default function CreateWithAIModal({ isOpen, onClose, onProductCreated }: CreateWithAIModalProps) {
  const [sku, setSku]                         = useState('');
  const [idType, setIdType]                   = useState<IdentifierType>('EAN');
  const [idValue, setIdValue]                 = useState('');
  const [loading, setLoading]                 = useState(false);
  const [error, setError]                     = useState('');
  const [preview, setPreview]                 = useState<LookupPreview | null>(null);
  const [isVariation, setIsVariation]         = useState(false);
  const [childCount, setChildCount]           = useState(0);

  if (!isOpen) return null;

  const reset = () => {
    setSku(''); setIdType('EAN'); setIdValue('');
    setLoading(false); setError(''); setPreview(null);
    setIsVariation(false); setChildCount(0);
  };

  const handleClose = () => { reset(); onClose(); };

  const handleLookup = async () => {
    if (!sku.trim())     { setError('SKU is required'); return; }
    if (!idValue.trim()) { setError('Identifier value is required'); return; }
    setError(''); setLoading(true); setPreview(null);

    try {
      const res = await productService.aiLookup({
        sku: sku.trim(),
        identifierType: idType,
        identifierValue: idValue.trim(),
      });
      const d = res.data;
      if (!d.ok) {
        setError(d.error || 'Lookup failed');
        return;
      }
      if (!d.found) {
        setError(`No product found for ${idType} "${idValue.trim()}"`);
        return;
      }
      setPreview(d.preview);
      setIsVariation(!!d.isVariation);
      setChildCount(d.childCount || 0);
    } catch (e: any) {
      setError(e?.response?.data?.error || e?.message || 'Network error');
    } finally {
      setLoading(false);
    }
  };

  const handleCreate = () => {
    if (preview) {
      reset();
      onProductCreated(preview.productId);
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !preview && !loading) handleLookup();
  };

  // ── styles ────────────────────────────────────────────────────────────────
  const overlay: React.CSSProperties = {
    position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.65)',
    display: 'flex', alignItems: 'center', justifyContent: 'center',
    zIndex: 1100, backdropFilter: 'blur(4px)',
  };
  const card: React.CSSProperties = {
    background: 'var(--bg-secondary)', border: '1px solid var(--border)',
    borderRadius: 14, width: '100%', maxWidth: 500,
    padding: '28px 28px 24px', boxShadow: '0 24px 48px rgba(0,0,0,0.4)',
    position: 'relative',
  };
  const lbl: React.CSSProperties = {
    display: 'block', fontSize: 11, fontWeight: 700,
    color: 'var(--text-muted)', textTransform: 'uppercase',
    letterSpacing: '0.5px', marginBottom: 6,
  };
  const inp: React.CSSProperties = {
    width: '100%', padding: '9px 12px', borderRadius: 8,
    border: '1px solid var(--border)', background: 'var(--bg-primary)',
    color: 'var(--text-primary)', fontSize: 14, outline: 'none',
    boxSizing: 'border-box',
  };
  const sel: React.CSSProperties = {
    ...inp, cursor: 'pointer', appearance: 'auto',
  };

  return (
    <div style={overlay} onClick={handleClose}>
      <div style={card} onClick={e => e.stopPropagation()} onKeyDown={handleKeyDown}>

        {/* Header */}
        <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', marginBottom: 22 }}>
          <div>
            <h2 style={{ fontSize: 18, fontWeight: 700, color: 'var(--text-primary)', margin: 0 }}>
              ✨ Create with AI
            </h2>
            <p style={{ fontSize: 13, color: 'var(--text-muted)', marginTop: 4 }}>
              Look up a product by barcode or identifier to auto-fill its details.
            </p>
          </div>
          <button onClick={handleClose} style={{ background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-muted)', fontSize: 20, lineHeight: 1, padding: 0 }}>
            ×
          </button>
        </div>

        {/* SKU */}
        <div style={{ marginBottom: 16 }}>
          <label style={lbl}>SKU <span style={{ color: 'var(--danger)' }}>*</span></label>
          <input
            style={inp}
            placeholder="Enter your internal SKU"
            value={sku}
            onChange={e => { setSku(e.target.value); setError(''); }}
            autoFocus
            disabled={!!preview}
          />
        </div>

        {/* Identifier type + value */}
        <div style={{ display: 'grid', gridTemplateColumns: '140px 1fr', gap: 10, marginBottom: 6 }}>
          <div>
            <label style={lbl}>Type <span style={{ color: 'var(--danger)' }}>*</span></label>
            <select
              style={sel}
              value={idType}
              onChange={e => { setIdType(e.target.value as IdentifierType); setError(''); setPreview(null); }}
              disabled={!!preview}
            >
              <option value="EAN">EAN</option>
              <option value="ASIN">ASIN</option>
              <option value="UPC">UPC</option>
              <option value="ISBN">ISBN</option>
            </select>
          </div>
          <div>
            <label style={lbl}>Value <span style={{ color: 'var(--danger)' }}>*</span></label>
            <input
              style={inp}
              placeholder={idType === 'EAN' ? '5012345678901' : idType === 'ASIN' ? 'B08N5WRWNW' : idType === 'UPC' ? '012345678905' : '9780141036144'}
              value={idValue}
              onChange={e => { setIdValue(e.target.value); setError(''); setPreview(null); }}
              disabled={!!preview}
            />
          </div>
        </div>
        <div style={{ fontSize: 11, color: 'var(--text-muted)', marginBottom: 20 }}>
          {IDENTIFIER_HINTS[idType]}
        </div>

        {/* Error */}
        {error && (
          <div style={{ padding: '10px 14px', borderRadius: 8, background: 'rgba(239,68,68,0.08)', border: '1px solid rgba(239,68,68,0.3)', color: '#f87171', fontSize: 13, marginBottom: 16 }}>
            ⚠ {error}
          </div>
        )}

        {/* Preview */}
        {preview && (
          <div style={{ display: 'flex', gap: 14, padding: '14px', borderRadius: 10, background: 'var(--bg-tertiary)', border: '1px solid var(--border)', marginBottom: 20 }}>
            {preview.imageUrl ? (
              <img
                src={preview.imageUrl.replace(/^http:\/\//i, 'https://')}
                alt=""
                style={{ width: 96, height: 96, objectFit: 'contain', borderRadius: 8, flexShrink: 0, background: '#fff', padding: 4 }}
              />
            ) : (
              <div style={{ width: 96, height: 96, borderRadius: 8, background: 'var(--bg-elevated)', display: 'flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0, fontSize: 36 }}>
                📦
              </div>
            )}
            <div style={{ flex: 1, minWidth: 0 }}>
              <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--text-primary)', lineHeight: 1.4, marginBottom: 4, overflow: 'hidden', display: '-webkit-box', WebkitLineClamp: 2, WebkitBoxOrient: 'vertical' }}>
                {preview.title}
              </div>
              {preview.brand && (
                <div style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 4 }}>
                  Brand: <span style={{ color: 'var(--text-secondary)', fontWeight: 500 }}>{preview.brand}</span>
                </div>
              )}
              <div style={{ fontSize: 11, color: 'var(--text-muted)' }}>
                {SOURCE_LABEL[preview.source] ?? preview.source}
              </div>
              {isVariation && (
                <div style={{ marginTop: 6, display: 'inline-flex', alignItems: 'center', gap: 5, padding: '3px 8px', borderRadius: 99, background: 'rgba(99,102,241,0.12)', border: '1px solid rgba(99,102,241,0.3)', fontSize: 11, fontWeight: 600, color: 'var(--primary)' }}>
                  📦 Variation product — {childCount} variant{childCount !== 1 ? 's' : ''} created
                </div>
              )}
            </div>
            {/* Change button */}
            <button
              onClick={() => setPreview(null)}
              style={{ background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-muted)', fontSize: 12, alignSelf: 'flex-start', padding: '2px 0', whiteSpace: 'nowrap' }}
            >
              ✏ Change
            </button>
          </div>
        )}

        {/* Actions */}
        <div style={{ display: 'flex', gap: 10, justifyContent: 'flex-end' }}>
          <button
            type="button"
            className="btn btn-secondary"
            onClick={handleClose}
            disabled={loading}
          >
            Cancel
          </button>

          {!preview ? (
            <button
              type="button"
              className="btn btn-primary"
              onClick={handleLookup}
              disabled={loading || !sku.trim() || !idValue.trim()}
            >
              {loading ? (
                <><span style={{ display: 'inline-block', width: 13, height: 13, border: '2px solid rgba(255,255,255,0.3)', borderTopColor: '#fff', borderRadius: '50%', animation: 'spin 0.8s linear infinite', marginRight: 8 }} />Looking up…</>
              ) : '🔍 Look Up'}
            </button>
          ) : (
            <button
              type="button"
              className="btn btn-primary"
              onClick={handleCreate}
            >
              ✨ Create Product
            </button>
          )}
        </div>
      </div>
    </div>
  );
}

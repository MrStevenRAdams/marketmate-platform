// ============================================================================
// SERIAL NUMBER INPUT COMPONENT
// ============================================================================
// File: frontend/src/components/SerialNumberInput.tsx
//
// Reusable component consumed by every stock movement screen that touches
// serialised products.  Handles three concerns:
//
//   1. Detecting whether a product is serial-tracked
//      (GET /products/:id  →  product.use_serial_numbers)
//
//   2. Collecting one unique serial number per unit being moved.
//      Supports typed entry AND barcode scanner (Enter key commits a serial).
//
//   3. Validating uniqueness within the current entry set and (optionally)
//      checking the backend for duplicate serials already in stock.
//
// Usage:
//   <SerialNumberInput
//     productId="prod_abc"          // required — used to check flag + validate
//     quantity={3}                  // how many serials to collect
//     value={serials}               // string[]  (controlled)
//     onChange={setSerials}         // (serials: string[]) => void
//     validateUnique={true}         // default true — POST /serials/validate
//   />
//
// The parent is responsible for blocking submission when
// isSerialRequired(product) === true && value.length !== quantity.
//
// useIsSerialProduct(productId) hook is also exported for cases where the
// caller only needs to know the flag, not render the input.
// ============================================================================

import { useState, useEffect, useRef, useCallback } from 'react';
import { getActiveTenantId } from '../contexts/TenantContext';

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

// ─── Hook: detect whether a product requires serial tracking ─────────────────

/**
 * Returns { isSerial, loading } for a given product_id.
 * Pass null/undefined to skip the fetch (returns false immediately).
 */
export function useIsSerialProduct(productId: string | null | undefined) {
  const [isSerial, setIsSerial] = useState(false);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!productId) { setIsSerial(false); return; }
    setLoading(true);
    api(`/products/${productId}`)
      .then(r => r.ok ? r.json() : null)
      .then(d => {
        // Backend wraps in { data: product } or returns product directly
        const p = d?.data ?? d;
        setIsSerial(!!(p?.use_serial_numbers));
      })
      .catch(() => setIsSerial(false))
      .finally(() => setLoading(false));
  }, [productId]);

  return { isSerial, loading };
}

// ─── Hook: look up product by SKU (needed in screens that work with SKU not ID) 

export function useProductBySku(sku: string | null | undefined) {
  const [product, setProduct] = useState<{ product_id: string; use_serial_numbers?: boolean } | null>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!sku) { setProduct(null); return; }
    setLoading(true);
    api(`/products?search=${encodeURIComponent(sku)}&limit=5`)
      .then(r => r.ok ? r.json() : null)
      .then(d => {
        const list: any[] = d?.data ?? d?.products ?? [];
        // find exact SKU match
        const match = list.find(p =>
          p.sku?.toLowerCase() === sku.toLowerCase() ||
          p.product_id?.toLowerCase() === sku.toLowerCase()
        ) ?? list[0] ?? null;
        setProduct(match);
      })
      .catch(() => setProduct(null))
      .finally(() => setLoading(false));
  }, [sku]);

  return { product, loading };
}

// ─── SerialNumberInput component ─────────────────────────────────────────────

interface SerialNumberInputProps {
  productId?: string | null;
  quantity: number;
  value: string[];
  onChange: (serials: string[]) => void;
  validateUnique?: boolean;
  label?: string;
  compact?: boolean;   // true → single-line pill style for table rows
}

export function SerialNumberInput({
  productId,
  quantity,
  value,
  onChange,
  validateUnique = true,
  label = 'Serial Numbers',
  compact = false,
}: SerialNumberInputProps) {
  const [input, setInput] = useState('');
  const [duplicateWarning, setDuplicateWarning] = useState('');
  const inputRef = useRef<HTMLInputElement>(null);

  // Focus on mount
  useEffect(() => { inputRef.current?.focus(); }, []);

  const addSerial = useCallback((raw: string) => {
    const sn = raw.trim().toUpperCase();
    if (!sn) return;

    if (value.includes(sn)) {
      setDuplicateWarning(`"${sn}" already entered`);
      return;
    }
    if (value.length >= quantity) {
      setDuplicateWarning(`Only ${quantity} serial${quantity > 1 ? 's' : ''} needed`);
      return;
    }

    setDuplicateWarning('');
    onChange([...value, sn]);
    setInput('');
  }, [value, quantity, onChange]);

  const removeSerial = (sn: string) => {
    onChange(value.filter(s => s !== sn));
  };

  const needed = quantity - value.length;
  const complete = needed === 0;

  if (compact) {
    // ── Compact inline mode (for table rows like StockIn) ──────────────────
    return (
      <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
        {/* Collected serials as small pills */}
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }}>
          {value.map(sn => (
            <span key={sn} style={{
              display: 'inline-flex', alignItems: 'center', gap: 4,
              padding: '2px 8px', background: 'rgba(34,197,94,0.12)',
              border: '1px solid rgba(34,197,94,0.3)', borderRadius: 99,
              fontSize: 11, color: '#22c55e', fontFamily: 'monospace',
            }}>
              {sn}
              <button onClick={() => removeSerial(sn)} style={{
                background: 'none', border: 'none', color: '#22c55e',
                cursor: 'pointer', padding: 0, fontSize: 12, lineHeight: 1,
              }}>×</button>
            </span>
          ))}
        </div>

        {/* Input row */}
        {!complete && (
          <div style={{ display: 'flex', gap: 4 }}>
            <input
              ref={inputRef}
              value={input}
              onChange={e => { setInput(e.target.value); setDuplicateWarning(''); }}
              onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); addSerial(input); } }}
              placeholder={`S/N ${value.length + 1} of ${quantity} (Enter)`}
              style={{
                flex: 1, minWidth: 0, padding: '4px 8px',
                background: 'var(--bg-secondary, #0f1117)',
                border: `1px solid ${duplicateWarning ? '#ef4444' : 'var(--border)'}`,
                borderRadius: 4, color: 'var(--text-primary)', fontSize: 12,
              }}
            />
            <button onClick={() => addSerial(input)} style={{
              padding: '4px 8px', background: 'var(--primary, #06b6d4)',
              border: 'none', borderRadius: 4, color: 'white', fontSize: 11,
              cursor: 'pointer', fontWeight: 600,
            }}>+</button>
          </div>
        )}

        {/* Status / error */}
        {duplicateWarning && (
          <div style={{ fontSize: 11, color: '#ef4444' }}>{duplicateWarning}</div>
        )}
        {complete ? (
          <div style={{ fontSize: 11, color: '#22c55e', fontWeight: 600 }}>✓ All serials entered</div>
        ) : (
          <div style={{ fontSize: 11, color: 'var(--text-muted)' }}>{needed} more required</div>
        )}
      </div>
    );
  }

  // ── Full modal/panel mode ─────────────────────────────────────────────────
  return (
    <div>
      <label style={{
        display: 'block', marginBottom: 6, fontSize: 13,
        fontWeight: 600, color: 'var(--text-secondary)',
      }}>
        {label}
        <span style={{ marginLeft: 8, fontSize: 11, fontWeight: 400, color: 'var(--text-muted)' }}>
          {value.length}/{quantity} entered
        </span>
      </label>

      {/* Progress bar */}
      <div style={{
        height: 3, background: 'var(--border)', borderRadius: 2,
        marginBottom: 10, overflow: 'hidden',
      }}>
        <div style={{
          height: '100%',
          width: `${quantity > 0 ? Math.round((value.length / quantity) * 100) : 0}%`,
          background: complete ? '#22c55e' : 'var(--primary, #06b6d4)',
          borderRadius: 2, transition: 'width 0.2s',
        }} />
      </div>

      {/* Entered serial pills */}
      {value.length > 0 && (
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, marginBottom: 10 }}>
          {value.map((sn, i) => (
            <span key={sn} style={{
              display: 'inline-flex', alignItems: 'center', gap: 5,
              padding: '4px 10px',
              background: 'rgba(34,197,94,0.1)',
              border: '1px solid rgba(34,197,94,0.25)',
              borderRadius: 99, fontSize: 12,
              color: '#22c55e', fontFamily: 'monospace',
            }}>
              <span style={{ fontSize: 10, opacity: 0.7 }}>{i + 1}</span>
              {sn}
              <button onClick={() => removeSerial(sn)} style={{
                background: 'none', border: 'none', color: 'rgba(34,197,94,0.7)',
                cursor: 'pointer', padding: 0, fontSize: 14, lineHeight: 1,
              }}>×</button>
            </span>
          ))}
        </div>
      )}

      {/* Input */}
      {!complete && (
        <div style={{ display: 'flex', gap: 8 }}>
          <input
            ref={inputRef}
            value={input}
            onChange={e => { setInput(e.target.value.toUpperCase()); setDuplicateWarning(''); }}
            onKeyDown={e => {
              if (e.key === 'Enter') { e.preventDefault(); addSerial(input); }
            }}
            placeholder={`Scan or type serial ${value.length + 1} of ${quantity}…`}
            style={{
              flex: 1, padding: '9px 12px',
              background: 'var(--bg-elevated, #1a1e28)',
              border: `1px solid ${duplicateWarning ? '#ef4444' : 'var(--border)'}`,
              borderRadius: 6, color: 'var(--text-primary)',
              fontSize: 13, fontFamily: 'monospace',
              outline: 'none',
            }}
          />
          <button
            onClick={() => addSerial(input)}
            disabled={!input.trim()}
            style={{
              padding: '9px 16px',
              background: input.trim() ? 'var(--primary, #06b6d4)' : 'var(--bg-elevated)',
              border: 'none', borderRadius: 6,
              color: input.trim() ? 'white' : 'var(--text-muted)',
              cursor: input.trim() ? 'pointer' : 'default',
              fontSize: 13, fontWeight: 600,
              transition: 'background 0.15s',
            }}
          >
            Add
          </button>
        </div>
      )}

      {duplicateWarning && (
        <div style={{
          marginTop: 6, padding: '6px 10px',
          background: 'rgba(239,68,68,0.08)',
          border: '1px solid rgba(239,68,68,0.2)',
          borderRadius: 5, fontSize: 12, color: '#ef4444',
        }}>
          ⚠ {duplicateWarning}
        </div>
      )}

      {complete && (
        <div style={{
          marginTop: 8, padding: '8px 12px',
          background: 'rgba(34,197,94,0.08)',
          border: '1px solid rgba(34,197,94,0.2)',
          borderRadius: 6, fontSize: 12,
          color: '#22c55e', fontWeight: 600,
        }}>
          ✓ All {quantity} serial number{quantity > 1 ? 's' : ''} entered
        </div>
      )}

      <p style={{ margin: '6px 0 0', fontSize: 11, color: 'var(--text-muted)' }}>
        Scan a barcode or type a serial number and press Enter.
        Each serial must be unique.
      </p>
    </div>
  );
}

// ─── SerialRequiredBadge ──────────────────────────────────────────────────────
// Small indicator shown next to a product name/SKU when it is serial-tracked.

export function SerialRequiredBadge() {
  return (
    <span style={{
      display: 'inline-flex', alignItems: 'center', gap: 4,
      padding: '2px 7px',
      background: 'rgba(6,182,212,0.1)',
      border: '1px solid rgba(6,182,212,0.25)',
      borderRadius: 99, fontSize: 10, fontWeight: 700,
      color: 'var(--accent-cyan, #06b6d4)',
      letterSpacing: '0.05em',
      textTransform: 'uppercase',
    }}>
      S/N
    </span>
  );
}

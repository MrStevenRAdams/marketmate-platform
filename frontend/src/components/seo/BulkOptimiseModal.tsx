// ============================================================================
// BulkOptimiseModal — Session 7
// Confirms bulk SEO optimisation before calling POST /api/v1/listings/bulk-optimise.
// Modal mechanism: position:fixed overlay + conditional render (matches BulkReviseDialog).
// ============================================================================

import { useState, useEffect } from 'react';
import { apiFetch } from '../../services/apiFetch';

export interface BulkOptimiseModalProps {
  listingIds: string[];
  isOpen: boolean;
  onClose: () => void;
  onComplete: () => void; // called after 202 — parent refreshes
}

const OPTIMISABLE_FIELDS = [
  { key: 'title',       label: 'Title' },
  { key: 'bullets',     label: 'Bullets' },
  { key: 'description', label: 'Description' },
] as const;

type FieldKey = typeof OPTIMISABLE_FIELDS[number]['key'];

export function BulkOptimiseModal({ listingIds, isOpen, onClose, onComplete }: BulkOptimiseModalProps) {
  const [selectedFields, setSelectedFields] = useState<Set<FieldKey>>(
    new Set(['title', 'bullets', 'description'])
  );
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [creditBalance, setCreditBalance] = useState<number | null>(null);

  // Fetch credit balance on open
  useEffect(() => {
    if (!isOpen) return;
    setError(null);
    apiFetch('/billing/status')
      .then(r => r.ok ? r.json() : null)
      .then(d => {
        const rem = d?.ledger?.credits_remaining ?? d?.credits_remaining ?? null;
        setCreditBalance(rem);
      })
      .catch(() => setCreditBalance(null));
  }, [isOpen]);

  if (!isOpen) return null;

  const n = listingIds.length;
  const creditCost = n; // 1 credit per listing

  function toggleField(key: FieldKey) {
    setSelectedFields(prev => {
      const next = new Set(prev);
      next.has(key) ? next.delete(key) : next.add(key);
      return next;
    });
  }

  async function handleConfirm() {
    if (selectedFields.size === 0) return;
    setLoading(true);
    setError(null);
    try {
      const res = await apiFetch('/listings/bulk-optimise', {
        method: 'POST',
        body: JSON.stringify({
          listing_ids: listingIds,
          fields: Array.from(selectedFields),
        }),
      });
      if (res.status === 202) {
        onComplete();
        onClose();
        return;
      }
      const data = await res.json().catch(() => ({}));
      if (res.status === 402) {
        setError(`insufficient_credits:${data.required ?? creditCost}:${data.balance ?? 0}`);
      } else {
        setError(data.error || `Unexpected error (${res.status})`);
      }
    } catch (e: any) {
      setError(e.message || 'Network error');
    } finally {
      setLoading(false);
    }
  }

  // Parse the special insufficient_credits error format
  const isInsufficientCredits = error?.startsWith('insufficient_credits:');
  let creditsRequired = 0;
  let creditsBalance = 0;
  if (isInsufficientCredits && error) {
    const parts = error.split(':');
    creditsRequired = parseFloat(parts[1]) || creditCost;
    creditsBalance = parseFloat(parts[2]) || 0;
  }

  return (
    <div
      style={{
        position: 'fixed', inset: 0,
        background: 'rgba(0,0,0,0.6)',
        display: 'flex', alignItems: 'center', justifyContent: 'center',
        zIndex: 1000,
      }}
      onClick={() => !loading && onClose()}
    >
      <div
        style={{
          background: 'var(--bg-secondary)',
          border: '1px solid var(--border)',
          borderRadius: 12,
          padding: 28,
          maxWidth: 480,
          width: '90%',
          maxHeight: '90vh',
          overflowY: 'auto',
        }}
        onClick={e => e.stopPropagation()}
      >
        {/* Header */}
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', marginBottom: 20 }}>
          <h3 style={{ margin: 0, fontSize: 17, fontWeight: 700, color: 'var(--text-primary)' }}>
            Optimise {n} listing{n !== 1 ? 's' : ''}
          </h3>
          {!loading && (
            <button
              onClick={onClose}
              style={{ background: 'none', border: 'none', color: 'var(--text-muted)', cursor: 'pointer', fontSize: 18, lineHeight: 1, padding: 0 }}
            >
              ×
            </button>
          )}
        </div>

        {/* Credit cost summary */}
        <div style={{
          background: 'var(--bg-elevated)',
          border: '1px solid var(--border)',
          borderRadius: 8,
          padding: '12px 16px',
          marginBottom: 20,
        }}>
          <div style={{ fontSize: 14, color: 'var(--text-primary)', fontWeight: 600 }}>
            {creditCost} credit{creditCost !== 1 ? 's' : ''} will be used
          </div>
          <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 4 }}>
            {creditBalance !== null
              ? `Current balance: ${creditBalance.toLocaleString()} credits`
              : 'Loading balance…'}
          </div>
        </div>

        {/* Field selector */}
        <div style={{ marginBottom: 20 }}>
          <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: 10 }}>
            Fields to optimise
          </div>
          {OPTIMISABLE_FIELDS.map(({ key, label }) => (
            <label
              key={key}
              style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 8, cursor: 'pointer' }}
            >
              <input
                type="checkbox"
                checked={selectedFields.has(key)}
                onChange={() => toggleField(key)}
                disabled={loading}
                style={{ width: 15, height: 15, accentColor: 'var(--primary)', cursor: 'pointer' }}
              />
              <span style={{ fontSize: 13, color: 'var(--text-primary)' }}>{label}</span>
            </label>
          ))}
        </div>

        {/* Info note */}
        <div style={{
          fontSize: 12,
          color: 'var(--text-muted)',
          background: 'var(--bg-elevated)',
          borderRadius: 6,
          padding: '10px 14px',
          marginBottom: 20,
          lineHeight: 1.5,
        }}>
          Optimisation runs in the background. Scores update automatically when complete.
        </div>

        {/* Error display */}
        {error && !isInsufficientCredits && (
          <div style={{
            padding: '10px 14px', marginBottom: 16, borderRadius: 8,
            background: 'rgba(239,68,68,0.1)', border: '1px solid #ef4444',
            color: '#ef4444', fontSize: 13,
          }}>
            {error}
          </div>
        )}
        {isInsufficientCredits && (
          <div style={{
            padding: '10px 14px', marginBottom: 16, borderRadius: 8,
            background: 'rgba(239,68,68,0.1)', border: '1px solid #ef4444',
            color: '#ef4444', fontSize: 13,
          }}>
            Insufficient credits — need {creditsRequired}, have {creditsBalance.toLocaleString()}.{' '}
            <a href="/settings/billing" style={{ color: '#ef4444', fontWeight: 600 }}>Buy credits →</a>
          </div>
        )}

        {/* Actions */}
        <div style={{ display: 'flex', gap: 10, justifyContent: 'flex-end' }}>
          <button
            onClick={onClose}
            disabled={loading}
            style={{
              padding: '9px 18px', borderRadius: 8, fontSize: 13, fontWeight: 500,
              background: 'var(--bg-elevated)', border: '1px solid var(--border)',
              color: 'var(--text-primary)', cursor: loading ? 'not-allowed' : 'pointer',
            }}
          >
            Cancel
          </button>
          <button
            onClick={handleConfirm}
            disabled={loading || selectedFields.size === 0}
            style={{
              padding: '9px 18px', borderRadius: 8, fontSize: 13, fontWeight: 600,
              background: selectedFields.size === 0 ? 'var(--bg-elevated)' : 'var(--primary)',
              border: 'none',
              color: selectedFields.size === 0 ? 'var(--text-muted)' : '#fff',
              cursor: loading || selectedFields.size === 0 ? 'not-allowed' : 'pointer',
              display: 'flex', alignItems: 'center', gap: 8,
              opacity: loading ? 0.7 : 1,
            }}
          >
            {loading ? (
              <>
                <span style={{
                  display: 'inline-block', width: 14, height: 14,
                  border: '2px solid rgba(255,255,255,0.3)',
                  borderTopColor: '#fff',
                  borderRadius: '50%',
                  animation: 'spin 0.7s linear infinite',
                }} />
                Submitting…
              </>
            ) : 'Confirm & optimise'}
          </button>
        </div>

        <style>{`@keyframes spin { to { transform: rotate(360deg); } }`}</style>
      </div>
    </div>
  );
}

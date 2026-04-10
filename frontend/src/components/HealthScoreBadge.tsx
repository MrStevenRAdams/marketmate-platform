import { useState, useEffect } from 'react';
import { getActiveTenantId } from '../contexts/TenantContext';

const API_BASE = import.meta.env.VITE_API_URL ?? 'http://localhost:8080/api/v1';

interface HealthBreakdown {
  title: number;
  description: number;
  images: number;
  price: number;
  barcode: number;
}

interface HealthScore {
  product_id: string;
  score: number;
  breakdown: HealthBreakdown;
}

function scoreColor(score: number): string {
  if (score >= 80) return '#22c55e';
  if (score >= 50) return '#f59e0b';
  return '#ef4444';
}

function scoreLabel(score: number): string {
  if (score >= 80) return 'Good';
  if (score >= 50) return 'Fair';
  return 'Poor';
}

// ─── Inline badge (compact) ───────────────────────────────────────────────────
// Usage: <HealthScoreBadge productId="prod_123" />

export function HealthScoreBadge({ productId }: { productId: string }) {
  const [data, setData] = useState<HealthScore | null>(null);

  useEffect(() => {
    if (!productId) return;
    fetch(`${API_BASE}/products/${productId}/health-score`, {
      headers: { 'X-Tenant-Id': getActiveTenantId() },
    })
      .then(r => r.ok ? r.json() : null)
      .then(d => d && setData(d))
      .catch(() => {});
  }, [productId]);

  if (!data) return null;

  const color = scoreColor(data.score);
  return (
    <span
      title={`Listing Health: ${data.score}/100 (${scoreLabel(data.score)})`}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 5,
        padding: '2px 8px',
        borderRadius: 20,
        fontSize: 11,
        fontWeight: 600,
        background: color + '20',
        color,
        border: `1px solid ${color}40`,
        cursor: 'default',
        whiteSpace: 'nowrap',
      }}
    >
      ◎ {data.score}/100
    </span>
  );
}

// ─── Full panel (detailed breakdown) ─────────────────────────────────────────
// Usage: <HealthScorePanel productId="prod_123" />

export function HealthScorePanel({ productId }: { productId: string }) {
  const [data, setData] = useState<HealthScore | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (!productId) return;
    setLoading(true);
    fetch(`${API_BASE}/products/${productId}/health-score`, {
      headers: { 'X-Tenant-Id': getActiveTenantId() },
    })
      .then(r => r.ok ? r.json() : null)
      .then(d => { setData(d); setLoading(false); })
      .catch(() => setLoading(false));
  }, [productId]);

  if (loading) return <div style={{ fontSize: 13, color: 'var(--text-muted)' }}>Loading health score…</div>;
  if (!data) return null;

  const color = scoreColor(data.score);
  const breakdown = data.breakdown;

  const criteria = [
    { key: 'title', label: 'Title length (>60 chars)', max: 25 },
    { key: 'description', label: 'Description (>200 chars)', max: 25 },
    { key: 'images', label: 'Images (≥4)', max: 25 },
    { key: 'price', label: 'Has price', max: 15 },
    { key: 'barcode', label: 'Has barcode / EAN', max: 10 },
  ] as const;

  return (
    <div style={{
      background: 'var(--bg-elevated)',
      border: '1px solid var(--border)',
      borderRadius: 10,
      padding: '16px 20px',
    }}>
      {/* Score ring */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 16, marginBottom: 16 }}>
        <div style={{
          width: 60, height: 60, borderRadius: '50%',
          background: `conic-gradient(${color} ${data.score * 3.6}deg, var(--bg-card) 0deg)`,
          display: 'flex', alignItems: 'center', justifyContent: 'center',
        }}>
          <div style={{
            width: 46, height: 46, borderRadius: '50%',
            background: 'var(--bg-elevated)',
            display: 'flex', alignItems: 'center', justifyContent: 'center',
            fontSize: 14, fontWeight: 700, color,
          }}>
            {data.score}
          </div>
        </div>
        <div>
          <div style={{ fontSize: 15, fontWeight: 600, color }}>
            {scoreLabel(data.score)} Listing
          </div>
          <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>
            {data.score}/100 health score
          </div>
        </div>
      </div>

      {/* Breakdown bars */}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
        {criteria.map(c => {
          const val = breakdown[c.key] ?? 0;
          const pct = Math.round((val / c.max) * 100);
          const barColor = pct === 100 ? '#22c55e' : pct > 0 ? '#f59e0b' : '#ef4444';
          return (
            <div key={c.key}>
              <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 3 }}>
                <span style={{ fontSize: 12, color: 'var(--text-secondary)' }}>{c.label}</span>
                <span style={{ fontSize: 12, fontWeight: 600, color: barColor }}>
                  {val}/{c.max}
                </span>
              </div>
              <div style={{ height: 4, background: 'var(--bg-card)', borderRadius: 2, overflow: 'hidden' }}>
                <div style={{
                  height: '100%',
                  width: `${pct}%`,
                  background: barColor,
                  borderRadius: 2,
                  transition: 'width 0.4s ease',
                }} />
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

export default HealthScoreBadge;

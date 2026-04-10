// ============================================================================
// OptimisedContentPanel — right panel: AI-optimised content diff + generation
// ============================================================================

import React, { useState } from 'react';
import { SEOScoreBadge } from './SEOScoreBadge';

export interface ListingFieldContent {
  title: string;
  bullets: string[];
  description: string;
}

export interface OptimisedContentPanelProps {
  listingId: string;
  currentContent: ListingFieldContent;
  optimisedContent: ListingFieldContent | null;
  keywordsCovered: string[];
  rationale: string;
  isGenerating: boolean;
  onGenerate: (fields: string[]) => void;
  onSaveField: (field: string, value: string) => void;
  onDiscardField: (field: string) => void;
  creditCost: number;
  hasEnoughCredits: boolean;
}

type Field = 'title' | 'bullets' | 'description';

const FIELDS: { key: Field; label: string }[] = [
  { key: 'title',       label: 'Title' },
  { key: 'bullets',     label: 'Bullet points' },
  { key: 'description', label: 'Description' },
];

const cardStyle: React.CSSProperties = {
  background: 'var(--bg-elevated)',
  border: '1px solid var(--border)',
  borderRadius: 8,
  padding: '14px 16px',
};

const labelStyle: React.CSSProperties = {
  fontSize: 10,
  fontWeight: 700,
  textTransform: 'uppercase',
  color: 'var(--text-muted)',
  marginBottom: 4,
  letterSpacing: '0.05em',
};

function FieldDiff({
  label,
  current,
  proposed,
  onAccept,
  onDiscard,
}: {
  label: string;
  current: string;
  proposed: string;
  onAccept: () => void;
  onDiscard: () => void;
}) {
  const changed = current !== proposed;
  return (
    <div style={{ ...cardStyle, marginBottom: 12 }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 10 }}>
        <span style={{ fontSize: 12, fontWeight: 700 }}>{label}</span>
        {changed && (
          <div style={{ display: 'flex', gap: 6 }}>
            <button
              onClick={onDiscard}
              style={{ padding: '3px 10px', borderRadius: 6, border: '1px solid var(--border)', background: 'transparent', fontSize: 11, cursor: 'pointer', color: 'var(--text-muted)' }}
            >
              Discard
            </button>
            <button
              onClick={onAccept}
              style={{ padding: '3px 10px', borderRadius: 6, border: 'none', background: 'var(--primary)', fontSize: 11, cursor: 'pointer', color: '#fff', fontWeight: 600 }}
            >
              Accept
            </button>
          </div>
        )}
        {!changed && (
          <span style={{ fontSize: 10, color: 'var(--text-muted)' }}>No change</span>
        )}
      </div>
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 10 }}>
        <div>
          <div style={labelStyle}>Current</div>
          <div style={{ fontSize: 12, color: 'var(--text-secondary)', background: 'var(--bg-secondary)', borderRadius: 6, padding: 10, minHeight: 48, whiteSpace: 'pre-wrap', lineHeight: 1.5 }}>
            {current || <em style={{ opacity: 0.5 }}>Empty</em>}
          </div>
        </div>
        <div>
          <div style={{ ...labelStyle, color: changed ? 'var(--primary)' : 'var(--text-muted)' }}>
            {changed ? '✨ Proposed' : 'Proposed'}
          </div>
          <div style={{
            fontSize: 12,
            color: changed ? 'var(--text-primary)' : 'var(--text-secondary)',
            background: changed ? 'var(--primary)08' : 'var(--bg-secondary)',
            border: changed ? '1px solid var(--primary)30' : '1px solid transparent',
            borderRadius: 6,
            padding: 10,
            minHeight: 48,
            whiteSpace: 'pre-wrap',
            lineHeight: 1.5,
          }}>
            {proposed || <em style={{ opacity: 0.5 }}>Empty</em>}
          </div>
        </div>
      </div>
    </div>
  );
}

export function OptimisedContentPanel({
  listingId,
  currentContent,
  optimisedContent,
  keywordsCovered,
  rationale,
  isGenerating,
  onGenerate,
  onSaveField,
  onDiscardField,
  creditCost,
  hasEnoughCredits,
}: OptimisedContentPanelProps) {
  const [selectedFields, setSelectedFields] = useState<Set<Field>>(new Set(['title', 'bullets', 'description']));
  const [simulatedScore, setSimulatedScore] = useState<number | null>(null);
  const [simulating, setSimulating] = useState(false);

  const API_BASE = (import.meta as any).env?.VITE_API_URL || 'http://localhost:8080/api/v1';

  function toggleField(f: Field) {
    setSelectedFields(prev => {
      const next = new Set(prev);
      if (next.has(f)) { next.delete(f); } else { next.add(f); }
      return next;
    });
  }

  async function simulateScore() {
    if (!optimisedContent) return;
    setSimulating(true);
    setSimulatedScore(null);
    try {
      const res = await fetch(`${API_BASE}/listings/${listingId}/seo-score?preview=true`, {
        method: 'GET',
        headers: { 'Content-Type': 'application/json' },
      });
      if (res.ok) {
        const data = await res.json();
        setSimulatedScore(data.score?.total ?? data.total ?? null);
      }
    } catch {
      // non-fatal
    } finally {
      setSimulating(false);
    }
  }

  // ── No optimised content yet — show generation UI ──────────────────────────
  if (!optimisedContent) {
    return (
      <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
        <div style={cardStyle}>
          <div style={{ fontSize: 13, fontWeight: 600, marginBottom: 6 }}>Generate optimised content</div>
          <div style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 14, lineHeight: 1.5 }}>
            Full market keyword analysis runs when you generate your first listing. Optimised content is tailored to the keywords your buyers are actually searching.
          </div>

          {/* Field selector */}
          <div style={{ display: 'flex', gap: 8, marginBottom: 16, flexWrap: 'wrap' }}>
            {FIELDS.map(f => (
              <label
                key={f.key}
                style={{ display: 'flex', alignItems: 'center', gap: 6, cursor: 'pointer', fontSize: 12 }}
              >
                <input
                  type="checkbox"
                  checked={selectedFields.has(f.key)}
                  onChange={() => toggleField(f.key)}
                  style={{ cursor: 'pointer' }}
                />
                {f.label}
              </label>
            ))}
          </div>

          <button
            onClick={() => onGenerate(Array.from(selectedFields))}
            disabled={!hasEnoughCredits || isGenerating || selectedFields.size === 0}
            style={{
              width: '100%',
              padding: '10px 16px',
              borderRadius: 8,
              border: 'none',
              background: hasEnoughCredits && !isGenerating && selectedFields.size > 0 ? 'var(--primary)' : 'var(--bg-tertiary)',
              color: hasEnoughCredits && !isGenerating && selectedFields.size > 0 ? '#fff' : 'var(--text-muted)',
              fontSize: 13,
              fontWeight: 700,
              cursor: hasEnoughCredits && !isGenerating && selectedFields.size > 0 ? 'pointer' : 'not-allowed',
              transition: 'background 0.2s',
            }}
          >
            {isGenerating
              ? '✨ Generating…'
              : !hasEnoughCredits
                ? 'Insufficient credits'
                : `✨ Generate optimised content — ${creditCost} credit${creditCost !== 1 ? 's' : ''}`}
          </button>
        </div>

        {/* Current content preview */}
        <div style={cardStyle}>
          <div style={{ ...labelStyle, marginBottom: 10 }}>Current listing content</div>
          <div style={{ marginBottom: 8 }}>
            <div style={labelStyle}>Title</div>
            <div style={{ fontSize: 12, color: 'var(--text-secondary)' }}>{currentContent.title || <em>Empty</em>}</div>
          </div>
          {currentContent.bullets.length > 0 && (
            <div style={{ marginBottom: 8 }}>
              <div style={labelStyle}>Bullets</div>
              <ul style={{ margin: 0, paddingLeft: 16, fontSize: 12, color: 'var(--text-secondary)' }}>
                {currentContent.bullets.map((b, i) => <li key={i} style={{ marginBottom: 2 }}>{b}</li>)}
              </ul>
            </div>
          )}
        </div>
      </div>
    );
  }

  // ── Optimised content present — show diff ─────────────────────────────────
  const allFields: Field[] = ['title', 'bullets', 'description'];

  function getFieldStr(content: ListingFieldContent, field: Field): string {
    if (field === 'bullets') return content.bullets.join('\n');
    return (content as any)[field] ?? '';
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 0 }}>
      {/* Top action bar */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 14 }}>
        <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
          <button
            onClick={() => allFields.forEach(f => onSaveField(f, getFieldStr(optimisedContent, f)))}
            style={{ padding: '6px 14px', borderRadius: 7, border: 'none', background: 'var(--primary)', color: '#fff', fontSize: 12, fontWeight: 700, cursor: 'pointer' }}
          >
            Accept all
          </button>
          <button
            onClick={() => allFields.forEach(f => onDiscardField(f))}
            style={{ padding: '6px 14px', borderRadius: 7, border: '1px solid var(--border)', background: 'transparent', fontSize: 12, cursor: 'pointer', color: 'var(--text-secondary)' }}
          >
            Discard all
          </button>
        </div>

        {/* Simulate Score button */}
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          {simulatedScore !== null && (
            <div style={{ display: 'flex', alignItems: 'center', gap: 5 }}>
              <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>Projected:</span>
              <SEOScoreBadge score={simulatedScore} size="sm" showLabel />
            </div>
          )}
          <button
            onClick={simulateScore}
            disabled={simulating}
            style={{ padding: '5px 12px', borderRadius: 7, border: '1px solid var(--border)', background: 'transparent', fontSize: 11, cursor: simulating ? 'wait' : 'pointer', color: 'var(--primary)', fontWeight: 600 }}
          >
            {simulating ? 'Simulating…' : '🔮 Simulate Score'}
          </button>
        </div>
      </div>

      {/* Per-field diffs */}
      {allFields.map(field => (
        <FieldDiff
          key={field}
          label={FIELDS.find(f => f.key === field)!.label}
          current={getFieldStr(currentContent, field)}
          proposed={getFieldStr(optimisedContent, field)}
          onAccept={() => onSaveField(field, getFieldStr(optimisedContent, field))}
          onDiscard={() => onDiscardField(field)}
        />
      ))}

      {/* Keywords covered */}
      {keywordsCovered.length > 0 && (
        <div style={{ ...cardStyle, marginTop: 4 }}>
          <div style={{ ...labelStyle, marginBottom: 8 }}>Keywords covered</div>
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 5, marginBottom: rationale ? 12 : 0 }}>
            {keywordsCovered.map((kw, i) => (
              <span key={i} style={{ padding: '2px 8px', borderRadius: 12, fontSize: 11, fontWeight: 500, background: '#22c55e18', color: '#22c55e', border: '1px solid #22c55e30' }}>
                {kw}
              </span>
            ))}
          </div>
          {rationale && (
            <>
              <div style={{ ...labelStyle, marginBottom: 4 }}>AI rationale</div>
              <p style={{ fontSize: 12, color: 'var(--text-secondary)', lineHeight: 1.6, margin: 0 }}>
                {rationale}
              </p>
            </>
          )}
        </div>
      )}
    </div>
  );
}

export default OptimisedContentPanel;

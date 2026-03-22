// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// AI CONTENT MODAL (Feature #11 — Polished)
// Modal for generating text content via the Anthropic API.
// Sends template type, available merge tags, tone, length, and
// current content as context to produce relevant text for the
// selected text block.
//
// POLISHED (A4):
//   ✓ Tone selector — 6 professional tones with visual indicators
//   ✓ Prompt chips — quick-start suggestions based on template type
//   ✓ Length guidance — Short / Medium / Long selector
//   ✓ Editable result — textarea for post-generation tweaking
//   ✓ Merge tag browser — insert tags into prompt or result
//   ✓ Character/word count on result
//   ✓ History — cycle through previous generations in session
//   ✓ Copy to clipboard button
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

import React, { useState, useRef, useCallback } from 'react';
import {
  X, Sparkles, Loader2, Check, ChevronDown, ChevronUp,
  RotateCcw, Wand2, Tag, Copy, ArrowLeft, ArrowRight,
} from 'lucide-react';
import { T, FIELD_CATEGORIES, CATEGORY_ICONS } from '../../../constants/index.jsx';
import { css } from '../ui/index.jsx';


// ── Tone presets ──────────────────────────────────────────────
const TONES = [
  { id: 'professional', label: 'Professional', emoji: '💼', desc: 'Clear, polished business tone' },
  { id: 'friendly',     label: 'Friendly',     emoji: '😊', desc: 'Warm and approachable' },
  { id: 'formal',       label: 'Formal',       emoji: '📋', desc: 'Traditional, corporate style' },
  { id: 'concise',      label: 'Concise',      emoji: '⚡', desc: 'Brief and to the point' },
  { id: 'persuasive',   label: 'Persuasive',   emoji: '🎯', desc: 'Compelling and action-oriented' },
  { id: 'casual',       label: 'Casual',       emoji: '👋', desc: 'Relaxed, conversational' },
];

// ── Length presets ─────────────────────────────────────────────
const LENGTHS = [
  { id: 'short',  label: 'Short',  desc: '1–2 sentences',  token: '1-2 sentences, very brief' },
  { id: 'medium', label: 'Medium', desc: '3–5 sentences',  token: '3-5 sentences, moderate length' },
  { id: 'long',   label: 'Long',   desc: 'Full paragraph', token: 'a full paragraph with detail' },
];

// ── Prompt chips per template type ────────────────────────────
const PROMPT_CHIPS = {
  email: [
    'Order confirmation email',
    'Shipping notification',
    'Thank you message',
    'Payment received notice',
    'Return instructions',
    'Review request',
  ],
  ebay_listing: [
    'Product description',
    'Shipping policy section',
    'Return policy section',
    'Item condition notes',
    'Seller guarantee note',
  ],
  invoice: [
    'Invoice header text',
    'Payment terms',
    'Thank you / footer note',
    'Overdue reminder text',
    'Bank details section',
  ],
  packing_slip: [
    'Packing slip header',
    'Return instructions',
    'Thank you note',
    'Care instructions',
  ],
  postage_label: [
    'Fragile handling note',
    'Return address label',
    'Custom declaration text',
  ],
  custom: [
    'Welcome message',
    'Terms and conditions',
    'Company description',
    'Call to action',
    'Contact information block',
  ],
};

// ── Helper: word count ────────────────────────────────────────
const wordCount = (s) => s.trim() ? s.trim().split(/\s+/).length : 0;


// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// MERGE TAG BROWSER (inline expandable)
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

function MergeTagBrowser({ onInsert }) {
  const [expanded, setExpanded] = useState(false);
  const [openCat, setOpenCat]  = useState(null);

  if (!expanded) {
    return (
      <button
        onClick={() => setExpanded(true)}
        style={{
          ...css.btn('secondary'),
          fontSize: 11, padding: '4px 10px',
          display: 'inline-flex', alignItems: 'center', gap: 4,
        }}
      >
        <Tag size={11} /> Insert merge tag
      </button>
    );
  }

  return (
    <div style={{
      border: `1px solid ${T.border.bright}`,
      borderRadius: T.radius.lg,
      backgroundColor: T.bg.tertiary,
      overflow: 'hidden',
      marginTop: 4,
    }}>
      {/* Header */}
      <div style={{
        display: 'flex', alignItems: 'center', justifyContent: 'space-between',
        padding: '6px 10px', borderBottom: `1px solid ${T.border.default}`,
      }}>
        <span style={{ fontSize: 11, fontWeight: 600, color: T.text.secondary, textTransform: 'uppercase', letterSpacing: '0.03em' }}>
          Merge Tags
        </span>
        <button onClick={() => { setExpanded(false); setOpenCat(null); }} style={{ ...css.iconBtn, width: 20, height: 20 }}>
          <X size={12} />
        </button>
      </div>
      {/* Categories */}
      <div style={{ maxHeight: 180, overflowY: 'auto' }}>
        {Object.entries(FIELD_CATEGORIES).map(([cat, fields]) => {
          const Icon = CATEGORY_ICONS[cat];
          const isOpen = openCat === cat;
          return (
            <div key={cat}>
              <button
                onClick={() => setOpenCat(isOpen ? null : cat)}
                style={{
                  display: 'flex', alignItems: 'center', gap: 6,
                  width: '100%', padding: '5px 10px',
                  background: isOpen ? T.bg.elevated : 'transparent',
                  border: 'none', color: T.text.primary, fontSize: 12,
                  cursor: 'pointer', fontFamily: T.font,
                  borderBottom: `1px solid ${T.border.default}`,
                }}
              >
                {Icon && <Icon size={12} style={{ color: T.text.muted }} />}
                <span style={{ flex: 1, textAlign: 'left' }}>{cat}</span>
                {isOpen ? <ChevronUp size={12} /> : <ChevronDown size={12} />}
              </button>
              {isOpen && (
                <div style={{ padding: '2px 0', backgroundColor: T.bg.primary }}>
                  {fields.map((f) => (
                    <button
                      key={f.path}
                      onClick={() => onInsert(`{{${f.path}}}`)}
                      style={{
                        display: 'block', width: '100%',
                        padding: '4px 16px 4px 30px', textAlign: 'left',
                        background: 'transparent', border: 'none',
                        color: T.text.secondary, fontSize: 12,
                        cursor: 'pointer', fontFamily: T.font,
                        transition: 'background 150ms',
                      }}
                      onMouseEnter={(e) => { e.target.style.background = T.bg.elevated; e.target.style.color = T.text.primary; }}
                      onMouseLeave={(e) => { e.target.style.background = 'transparent'; e.target.style.color = T.text.secondary; }}
                    >
                      <span style={{ color: T.accent.cyan, fontFamily: 'monospace', fontSize: 11 }}>{`{{${f.path}}}`}</span>
                      <span style={{ marginLeft: 8, color: T.text.muted, fontSize: 11 }}>{f.label}</span>
                    </button>
                  ))}
                </div>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}


// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// MAIN COMPONENT
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

export default function AIContentModal({ onClose, onInsert, templateType, currentContent }) {
  const [prompt, setPrompt]   = useState('');
  const [tone, setTone]       = useState('professional');
  const [length, setLength]   = useState('medium');
  const [loading, setLoading] = useState(false);
  const [result, setResult]   = useState('');
  const [error, setError]     = useState('');
  const [copied, setCopied]   = useState(false);

  // History of generated results in this session
  const [history, setHistory]       = useState([]);
  const [historyIdx, setHistoryIdx] = useState(-1);

  // Show/hide advanced options (tone + length)
  const [showOptions, setShowOptions] = useState(false);

  const promptRef = useRef(null);
  const resultRef = useRef(null);

  // ── Chips for current template type ─────────────────────────
  const chips = PROMPT_CHIPS[templateType] || PROMPT_CHIPS.custom;

  // ── Build flat merge tag list for the API prompt ────────────
  const mergeTagList = Object.entries(FIELD_CATEGORIES)
    .flatMap(([, fields]) => fields.map((f) => `{{${f.path}}}`))
    .join(', ');

  // ── Generate content via backend proxy ─────────────────────
  const handleGenerate = useCallback(async () => {
    if (!prompt.trim()) return;
    setLoading(true);
    setError('');

    const selectedTone   = TONES.find((t) => t.id === tone);
    const selectedLength = LENGTHS.find((l) => l.id === length);
    const API_URL = import.meta.env?.VITE_API_URL || 'http://localhost:8080/api/v1';

    try {
      const response = await fetch(`${API_URL}/templates/ai/generate-text`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-Tenant-Id': localStorage.getItem('marketmate_active_tenant') || '',
        },
        body: JSON.stringify({
          template_type:   templateType,
          tone:            selectedTone?.label || tone,
          length:          selectedLength?.token || length,
          prompt,
          current_content: currentContent || '',
          merge_tag_list:  mergeTagList,
        }),
      });

      const data = await response.json();

      if (!response.ok) {
        setError(data.error || `Server error ${response.status}`);
        setLoading(false);
        return;
      }

      const text = data.text || '';
      if (!text) {
        setError('The AI returned an empty response. Try rephrasing your prompt.');
        setLoading(false);
        return;
      }

      setResult(text);
      setHistory((h) => [...h, text]);
      setHistoryIdx(-1);
    } catch (err) {
      setError(
        err.message && err.message !== 'Failed to fetch'
          ? `Generation failed: ${err.message}`
          : 'Failed to connect to the AI service. Check your network and try again.'
      );
    }

    setLoading(false);
  }, [prompt, tone, length, templateType, currentContent, mergeTagList]);

  // ── Navigate history ────────────────────────────────────────
  const navigateHistory = (dir) => {
    if (history.length === 0) return;
    let newIdx;
    if (historyIdx === -1) {
      // Currently showing latest
      newIdx = dir === -1 ? history.length - 2 : -1;
    } else {
      newIdx = historyIdx + dir;
    }
    if (newIdx < 0 && dir === -1) return;
    if (newIdx >= history.length) {
      setHistoryIdx(-1);
      setResult(history[history.length - 1]);
      return;
    }
    if (newIdx < 0) newIdx = 0;
    setHistoryIdx(newIdx);
    setResult(history[newIdx]);
  };

  // ── Insert merge tag at cursor position ─────────────────────
  const insertTagAtCursor = (ref, value, setter) => {
    const el = ref.current;
    if (!el) { setter((v) => v + ' ' + value); return; }
    const start  = el.selectionStart;
    const end    = el.selectionEnd;
    const before = el.value.substring(0, start);
    const after  = el.value.substring(end);
    setter(before + value + after);
    requestAnimationFrame(() => {
      el.focus();
      el.selectionStart = el.selectionEnd = start + value.length;
    });
  };

  // ── Copy result to clipboard ────────────────────────────────
  const handleCopy = () => {
    navigator.clipboard?.writeText(result);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };


  // ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  // RENDER
  // ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  return (
    <div
      style={{
        position: 'fixed', inset: 0, zIndex: 1000,
        display: 'flex', alignItems: 'center', justifyContent: 'center',
        backgroundColor: 'rgba(0,0,0,0.6)', backdropFilter: 'blur(4px)',
      }}
      onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}
      role="dialog"
      aria-modal="true"
      aria-label="AI Content Generator"
    >
      <div style={{
        width: 540, maxHeight: '85vh',
        backgroundColor: T.bg.secondary,
        border: `1px solid ${T.border.bright}`,
        borderRadius: T.radius.xl,
        boxShadow: T.shadow.lg,
        display: 'flex', flexDirection: 'column', overflow: 'hidden',
      }}>

        {/* ── Header ──────────────────────────────────────────── */}
        <div style={{ ...css.panelHeader, justifyContent: 'space-between' }}>
          <span style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <Sparkles size={14} style={{ color: T.accent.purple }} />
            AI Content Generator
          </span>
          <button onClick={onClose} style={{ ...css.iconBtn, width: 24, height: 24 }}>
            <X size={14} />
          </button>
        </div>

        {/* ── Body ────────────────────────────────────────────── */}
        <div style={{ padding: 16, flex: 1, overflowY: 'auto', display: 'flex', flexDirection: 'column', gap: 12 }}>

          {/* ── Prompt chips (quick-start) ────────────────────── */}
          <div>
            <span style={{ ...css.label, marginBottom: 6 }}>Quick prompts</span>
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
              {chips.map((chip) => {
                const active = prompt === chip;
                return (
                  <button
                    key={chip}
                    onClick={() => setPrompt(chip)}
                    style={{
                      padding: '4px 10px',
                      borderRadius: T.radius.lg,
                      fontSize: 12,
                      cursor: 'pointer',
                      fontFamily: T.font,
                      border: `1px solid ${active ? T.primary.base : T.border.bright}`,
                      background: active
                        ? 'linear-gradient(135deg, rgba(59,130,246,0.15), rgba(139,92,246,0.15))'
                        : T.bg.tertiary,
                      color: active ? T.primary.light : T.text.secondary,
                      transition: 'all 150ms',
                    }}
                  >
                    {chip}
                  </button>
                );
              })}
            </div>
          </div>

          {/* ── Prompt textarea ───────────────────────────────── */}
          <div>
            <span style={css.label}>Describe what you want</span>
            <textarea
              ref={promptRef}
              style={{ ...css.input, minHeight: 72, resize: 'vertical' }}
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
              placeholder='e.g. "Write a shipping confirmation message with order details"'
              onKeyDown={(e) => {
                if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) handleGenerate();
              }}
            />
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginTop: 4 }}>
              <MergeTagBrowser onInsert={(tag) => insertTagAtCursor(promptRef, tag, setPrompt)} />
              <span style={{ fontSize: 10, color: T.text.muted }}>Ctrl+Enter to generate</span>
            </div>
          </div>

          {/* ── Options toggle ────────────────────────────────── */}
          <button
            onClick={() => setShowOptions(!showOptions)}
            style={{
              display: 'flex', alignItems: 'center', gap: 6,
              background: 'transparent', border: 'none',
              color: T.text.muted, fontSize: 12, cursor: 'pointer',
              fontFamily: T.font, padding: 0,
            }}
          >
            <Wand2 size={12} />
            Tone & length options
            {showOptions ? <ChevronUp size={12} /> : <ChevronDown size={12} />}
          </button>

          {/* ── Tone & Length selectors (collapsible) ──────────── */}
          {showOptions && (
            <div style={{
              padding: 12, borderRadius: T.radius.lg,
              backgroundColor: T.bg.tertiary,
              border: `1px solid ${T.border.default}`,
              display: 'flex', flexDirection: 'column', gap: 12,
            }}>
              {/* Tone */}
              <div>
                <span style={{ ...css.label, marginBottom: 6 }}>Tone</span>
                <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 6 }}>
                  {TONES.map((t) => {
                    const active = tone === t.id;
                    return (
                      <button
                        key={t.id}
                        onClick={() => setTone(t.id)}
                        style={{
                          padding: '6px 8px',
                          borderRadius: T.radius.md,
                          fontSize: 12,
                          cursor: 'pointer',
                          fontFamily: T.font,
                          textAlign: 'center',
                          border: active
                            ? `1px solid ${T.primary.base}`
                            : `1px solid ${T.border.bright}`,
                          background: active
                            ? 'linear-gradient(135deg, rgba(59,130,246,0.2), rgba(139,92,246,0.2))'
                            : T.bg.elevated,
                          color: active ? T.primary.light : T.text.secondary,
                          transition: 'all 150ms',
                        }}
                        title={t.desc}
                      >
                        <span style={{ fontSize: 14 }}>{t.emoji}</span>
                        <div style={{ fontSize: 11, marginTop: 2 }}>{t.label}</div>
                      </button>
                    );
                  })}
                </div>
              </div>

              {/* Length */}
              <div>
                <span style={{ ...css.label, marginBottom: 6 }}>Length</span>
                <div style={{ display: 'flex', gap: 6 }}>
                  {LENGTHS.map((l) => {
                    const active = length === l.id;
                    return (
                      <button
                        key={l.id}
                        onClick={() => setLength(l.id)}
                        style={{
                          flex: 1,
                          padding: '6px 8px',
                          borderRadius: T.radius.md,
                          fontSize: 12,
                          cursor: 'pointer',
                          fontFamily: T.font,
                          textAlign: 'center',
                          border: active
                            ? `1px solid ${T.primary.base}`
                            : `1px solid ${T.border.bright}`,
                          background: active
                            ? 'linear-gradient(135deg, rgba(59,130,246,0.2), rgba(139,92,246,0.2))'
                            : T.bg.elevated,
                          color: active ? T.primary.light : T.text.secondary,
                          transition: 'all 150ms',
                        }}
                      >
                        <div style={{ fontWeight: 600 }}>{l.label}</div>
                        <div style={{ fontSize: 10, color: active ? T.text.secondary : T.text.muted, marginTop: 1 }}>{l.desc}</div>
                      </button>
                    );
                  })}
                </div>
              </div>
            </div>
          )}

          {/* ── Generate button ───────────────────────────────── */}
          <button
            onClick={handleGenerate}
            disabled={loading || !prompt.trim()}
            style={{
              ...css.btn('primary'),
              width: '100%', padding: '10px 16px',
              opacity: loading || !prompt.trim() ? 0.6 : 1,
              cursor: loading || !prompt.trim() ? 'not-allowed' : 'pointer',
            }}
          >
            {loading
              ? <><Loader2 size={14} style={{ animation: 'spin 1s linear infinite' }} /> Generating...</>
              : <><Sparkles size={14} /> Generate Content</>
            }
          </button>

          {/* ── Error ─────────────────────────────────────────── */}
          {error && (
            <div style={{
              padding: '8px 12px', borderRadius: T.radius.md,
              backgroundColor: 'rgba(239,68,68,0.1)',
              border: '1px solid rgba(239,68,68,0.2)',
              color: T.status.danger, fontSize: 12,
              display: 'flex', alignItems: 'center', gap: 6,
            }}>
              <X size={12} /> {error}
            </div>
          )}

          {/* ── Result (editable) ─────────────────────────────── */}
          {result && (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
              {/* Result header with stats */}
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                <span style={css.label}>Generated Content</span>
                <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                  {/* History navigation */}
                  {history.length > 1 && (
                    <div style={{ display: 'flex', alignItems: 'center', gap: 2 }}>
                      <button
                        onClick={() => navigateHistory(-1)}
                        disabled={historyIdx === 0 || (historyIdx === -1 && history.length <= 1)}
                        style={{
                          ...css.iconBtn, width: 20, height: 20,
                          opacity: (historyIdx === 0 || (historyIdx === -1 && history.length <= 1)) ? 0.3 : 1,
                        }}
                        title="Previous generation"
                      >
                        <ArrowLeft size={11} />
                      </button>
                      <span style={{ fontSize: 10, color: T.text.muted, minWidth: 30, textAlign: 'center' }}>
                        {(historyIdx === -1 ? history.length : historyIdx + 1)}/{history.length}
                      </span>
                      <button
                        onClick={() => navigateHistory(1)}
                        disabled={historyIdx === -1}
                        style={{
                          ...css.iconBtn, width: 20, height: 20,
                          opacity: historyIdx === -1 ? 0.3 : 1,
                        }}
                        title="Next generation"
                      >
                        <ArrowRight size={11} />
                      </button>
                    </div>
                  )}
                  {/* Word count */}
                  <span style={{ fontSize: 10, color: T.text.muted }}>
                    {wordCount(result)} words · {result.length} chars
                  </span>
                </div>
              </div>

              {/* Editable result textarea */}
              <textarea
                ref={resultRef}
                style={{
                  ...css.input,
                  minHeight: 120,
                  resize: 'vertical',
                  lineHeight: 1.6,
                  fontSize: 13,
                  backgroundColor: T.bg.primary,
                  border: `1px solid ${T.border.bright}`,
                }}
                value={result}
                onChange={(e) => setResult(e.target.value)}
              />

              {/* Merge tag insertion for result */}
              <MergeTagBrowser onInsert={(tag) => insertTagAtCursor(resultRef, tag, setResult)} />

              {/* Action buttons */}
              <div style={{ display: 'flex', gap: 8 }}>
                <button
                  onClick={() => onInsert(result)}
                  style={{ ...css.btn('primary'), flex: 1, padding: '8px 16px' }}
                >
                  <Check size={14} /> Insert into block
                </button>
                <button
                  onClick={handleCopy}
                  style={{ ...css.btn('secondary'), padding: '8px 12px' }}
                  title="Copy to clipboard"
                >
                  {copied ? <Check size={14} style={{ color: T.status.success }} /> : <Copy size={14} />}
                </button>
                <button
                  onClick={handleGenerate}
                  disabled={loading || !prompt.trim()}
                  style={{
                    ...css.btn('secondary'), padding: '8px 12px',
                    opacity: loading || !prompt.trim() ? 0.5 : 1,
                  }}
                  title="Regenerate"
                >
                  <RotateCcw size={14} />
                </button>
              </div>
            </div>
          )}
        </div>
      </div>

      {/* Spinner keyframes */}
      <style>{`@keyframes spin { from { transform: rotate(0deg) } to { transform: rotate(360deg) } }`}</style>
    </div>
  );
}

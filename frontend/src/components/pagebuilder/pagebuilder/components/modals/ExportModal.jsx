// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// EXPORT MODAL (Features #8, #9, #10 — Polished)
// Modal with three tabs: HTML, eBay HTML, and MJML.
// Each tab shows the serialised output with copy + download buttons.
//
// POLISHED:
//   ✓ eBay tab: byte count with limit bar, sanitisation warnings
//   ✓ MJML tab: title + preview text inputs passed to serialiser
//   ✓ All tabs: download as file, line count display
//   ✓ Sticky footer with tab-specific metadata
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

import React, { useState, useMemo, useCallback } from 'react';
import {
  X, Code, ShoppingCart, Mail, Clipboard, Check,
  Download, AlertTriangle, FileText, Info,
} from 'lucide-react';
import { T } from '../../../constants/index.jsx';
import { css } from '../ui/index.jsx';
import { generateFullHTML, serializeBlocksToHTML } from '../../../serialisers/index.jsx';
import { sanitizeForEbay, getEbayByteCount } from '../../../serialisers/index.jsx';
import { serializeBlocksToMJML } from '../../../serialisers/index.jsx';

// ── Helper: count lines in a string ──────────────────────────
const lineCount = (s) => (s || '').split('\n').length;

// ── Helper: download a string as a file ──────────────────────
function downloadAsFile(content, filename, mimeType = 'text/html') {
  const blob = new Blob([content], { type: mimeType });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
}


// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// COMPONENT
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

export default function ExportModal({ onClose, blocks, data, themeVars, canvas, templateName, templateType }) {
  const [tab, setTab]                 = useState('html');
  const [copied, setCopied]           = useState(false);
  const [mjmlTitle, setMjmlTitle]     = useState(templateName || '');
  const [mjmlPreview, setMjmlPreview] = useState('');

  // ── Memoised outputs ────────────────────────────────────────

  const htmlOutput = useMemo(
    () => generateFullHTML(blocks, data, themeVars, canvas, templateName, {
      forEmail: templateType === 'email',
      forEbay:  templateType === 'ebay_listing',
    }),
    [blocks, data, themeVars, canvas, templateName, templateType]
  );

  const ebayResult = useMemo(() => {
    const rawHtml = serializeBlocksToHTML(blocks, data, themeVars, { forEmail: true, forEbay: true });
    const result = sanitizeForEbay(rawHtml);
    const byteInfo = getEbayByteCount(result.html);
    return { ...result, byteInfo };
  }, [blocks, data, themeVars]);

  const mjmlOutput = useMemo(
    () => serializeBlocksToMJML(blocks, data, themeVars, {
      title: mjmlTitle,
      previewText: mjmlPreview,
      templateName,
    }),
    [blocks, data, themeVars, mjmlTitle, mjmlPreview, templateName]
  );

  const currentOutput = tab === 'html' ? htmlOutput : tab === 'ebay' ? ebayResult.html : mjmlOutput;

  // ── Actions ─────────────────────────────────────────────────

  const handleCopy = useCallback(() => {
    navigator.clipboard?.writeText(currentOutput);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  }, [currentOutput]);

  const handleDownload = useCallback(() => {
    const safeName = (templateName || 'export').replace(/\s+/g, '_').toLowerCase();
    if (tab === 'html') downloadAsFile(htmlOutput, `${safeName}.html`, 'text/html');
    else if (tab === 'ebay') downloadAsFile(ebayResult.html, `${safeName}_ebay.html`, 'text/html');
    else downloadAsFile(mjmlOutput, `${safeName}.mjml`, 'text/mjml');
  }, [tab, htmlOutput, ebayResult, mjmlOutput, templateName]);

  // ── Tab definitions ─────────────────────────────────────────

  const tabs = [
    { key: 'html', label: 'HTML',      icon: Code },
    { key: 'ebay', label: 'eBay HTML',  icon: ShoppingCart },
    { key: 'mjml', label: 'MJML',      icon: Mail },
  ];

  // ── Byte count bar (eBay) ───────────────────────────────────
  const renderByteBar = () => {
    const { byteInfo } = ebayResult;
    const pct = Math.min((byteInfo.bytes / (byteInfo.limitKB * 1024)) * 100, 100);
    const barColor = byteInfo.isOverLimit ? T.status.danger : pct > 80 ? T.status.warning : T.status.success;

    return (
      <div style={{ padding: '8px 16px', borderBottom: `1px solid ${T.border.default}`, backgroundColor: T.bg.primary }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 4 }}>
          <span style={{ fontSize: 11, color: T.text.secondary, display: 'flex', alignItems: 'center', gap: 4 }}>
            <FileText size={11} /> {byteInfo.kb} KB / {byteInfo.limitKB} KB
          </span>
          {byteInfo.isOverLimit && (
            <span style={{ fontSize: 11, color: T.status.danger, display: 'flex', alignItems: 'center', gap: 4, fontWeight: 600 }}>
              <AlertTriangle size={11} /> Over eBay limit!
            </span>
          )}
        </div>
        <div style={{
          height: 4, borderRadius: 2, backgroundColor: T.bg.tertiary, overflow: 'hidden',
        }}>
          <div style={{
            width: `${pct}%`, height: '100%', borderRadius: 2,
            backgroundColor: barColor,
            transition: 'width 300ms ease-in-out',
          }} />
        </div>
      </div>
    );
  };

  // ── Warnings panel (eBay) ───────────────────────────────────
  const renderWarnings = () => {
    if (!ebayResult.warnings || ebayResult.warnings.length === 0) return null;

    return (
      <div style={{
        padding: '8px 16px', borderBottom: `1px solid ${T.border.default}`,
        backgroundColor: 'rgba(245, 158, 11, 0.05)',
      }}>
        <div style={{ fontSize: 11, fontWeight: 600, color: T.status.warning, marginBottom: 4, display: 'flex', alignItems: 'center', gap: 4 }}>
          <AlertTriangle size={11} /> {ebayResult.warnings.length} item{ebayResult.warnings.length !== 1 ? 's' : ''} sanitised
        </div>
        <div style={{ maxHeight: 80, overflowY: 'auto' }}>
          {ebayResult.warnings.map((w, i) => (
            <div key={i} style={{ fontSize: 10, color: T.text.muted, padding: '1px 0', paddingLeft: 16 }}>
              • {w}
            </div>
          ))}
        </div>
      </div>
    );
  };

  // ── MJML settings strip ─────────────────────────────────────
  const renderMjmlSettings = () => (
    <div style={{
      padding: '8px 16px', borderBottom: `1px solid ${T.border.default}`,
      backgroundColor: T.bg.primary,
      display: 'flex', gap: 8, alignItems: 'center',
    }}>
      <div style={{ flex: 1 }}>
        <label style={{ ...css.label, fontSize: 10, marginBottom: 2 }}>Email Title</label>
        <input
          value={mjmlTitle}
          onChange={(e) => setMjmlTitle(e.target.value)}
          placeholder="Email subject or title"
          style={{ ...css.input, fontSize: 11, padding: '4px 8px' }}
        />
      </div>
      <div style={{ flex: 1 }}>
        <label style={{ ...css.label, fontSize: 10, marginBottom: 2 }}>Preview Text</label>
        <input
          value={mjmlPreview}
          onChange={(e) => setMjmlPreview(e.target.value)}
          placeholder="Inbox preview snippet"
          style={{ ...css.input, fontSize: 11, padding: '4px 8px' }}
        />
      </div>
      <div style={{ paddingTop: 14 }}>
        <div style={{
          display: 'flex', alignItems: 'center', gap: 4,
          fontSize: 10, color: T.text.muted,
        }}>
          <Info size={10} />
          <span>Shown in inbox before opening</span>
        </div>
      </div>
    </div>
  );


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
      aria-label="Export Template"
    >
      <div style={{
        width: 780, maxHeight: '88vh',
        backgroundColor: T.bg.secondary,
        border: `1px solid ${T.border.bright}`,
        borderRadius: T.radius.xl,
        boxShadow: T.shadow.lg,
        display: 'flex', flexDirection: 'column', overflow: 'hidden',
      }}>
        {/* ── Header ──────────────────────────────────────────── */}
        <div style={{ ...css.panelHeader, justifyContent: 'space-between' }}>
          <span style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <Code size={14} /> Export Output
          </span>
          <button onClick={onClose} style={{ ...css.iconBtn, width: 24, height: 24 }}>
            <X size={14} />
          </button>
        </div>

        {/* ── Tab bar ─────────────────────────────────────────── */}
        <div style={{ display: 'flex', borderBottom: `1px solid ${T.border.default}` }}>
          {tabs.map((t) => (
            <button
              key={t.key}
              onClick={() => { setTab(t.key); setCopied(false); }}
              style={{
                flex: 1, padding: '10px 0',
                border: 'none', backgroundColor: 'transparent',
                color: tab === t.key ? T.primary.base : T.text.muted,
                fontSize: 11, fontWeight: 600, cursor: 'pointer', fontFamily: T.font,
                borderBottom: tab === t.key ? `2px solid ${T.primary.base}` : '2px solid transparent',
                display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 6,
                transition: 'all 150ms', textTransform: 'uppercase', letterSpacing: '0.03em',
              }}
            >
              <t.icon size={13} /> {t.label}
            </button>
          ))}
        </div>

        {/* ── Tab-specific settings / info bars ───────────────── */}
        {tab === 'ebay' && renderByteBar()}
        {tab === 'ebay' && renderWarnings()}
        {tab === 'mjml' && renderMjmlSettings()}

        {/* ── Code output ─────────────────────────────────────── */}
        <div style={{ flex: 1, overflow: 'auto', padding: 0 }}>
          <pre style={{
            margin: 0, padding: 16,
            fontSize: 11, color: T.text.secondary,
            fontFamily: "'Cascadia Code', Consolas, Monaco, monospace",
            whiteSpace: 'pre-wrap', wordBreak: 'break-all',
            lineHeight: 1.6, backgroundColor: T.bg.primary,
            tabSize: 2,
          }}>
            {currentOutput}
          </pre>
        </div>

        {/* ── Footer ──────────────────────────────────────────── */}
        <div style={{
          padding: '10px 16px', borderTop: `1px solid ${T.border.default}`,
          display: 'flex', gap: 8, alignItems: 'center', justifyContent: 'space-between',
          backgroundColor: T.bg.secondary,
        }}>
          {/* Left: metadata */}
          <span style={{ fontSize: 10, color: T.text.muted }}>
            {lineCount(currentOutput)} lines
            {tab === 'ebay' && ` · ${ebayResult.byteInfo.kb} KB`}
          </span>

          {/* Right: actions */}
          <div style={{ display: 'flex', gap: 8 }}>
            <button
              onClick={handleDownload}
              style={{ ...css.btn('secondary'), padding: '6px 14px', fontSize: 12 }}
            >
              <Download size={13} /> Download .{tab === 'mjml' ? 'mjml' : 'html'}
            </button>
            <button
              onClick={handleCopy}
              style={{ ...css.btn(copied ? 'secondary' : 'primary'), padding: '6px 20px', fontSize: 12 }}
            >
              {copied
                ? <><Check size={13} /> Copied!</>
                : <><Clipboard size={13} /> Copy {tab === 'ebay' ? 'eBay HTML' : tab === 'mjml' ? 'MJML' : 'HTML'}</>
              }
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

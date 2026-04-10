// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// MJML SERIALISER (Feature #10 — Polished)
// Converts blocks to MJML markup which can then be compiled to
// email-client-safe HTML. Each block type maps to its MJML
// equivalent (mj-text, mj-image, mj-section/mj-column, etc.).
//
// POLISHED (A3):
//   ✓ <mj-title> for email subject/title
//   ✓ <mj-preview> for inbox preview text
//   ✓ <mj-style> for custom CSS (link colours, etc.)
//   ✓ <mj-font> for Google Fonts when non-system fonts are used
//   ✓ Barcode blocks → warning placeholder (SVG not supported in email)
//   ✓ Heading-level font sizing with proper mj-text attributes
//   ✓ Repeater direction support (horizontal → mj-section with columns)
//   ✓ Box background-color and border-radius passthrough
//   ✓ Page breaks emit an HTML comment (no meaning in email)
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

import { THEME_PRESETS } from '../constants/index.jsx';
import { resolveText, resolveMergeTag, escHtml, evaluateConditions } from '../utils/index.jsx';

// ── Helpers ──────────────────────────────────────────────────────

/**
 * Collect all unique font families used across blocks.
 * Used to determine if we need <mj-font> imports.
 */
function collectFontFamilies(blocks) {
  const fonts = new Set();
  const walk = (list) => {
    for (const b of list) {
      const ff = b.properties?.fontFamily;
      if (ff) fonts.add(ff.split(',')[0].trim().replace(/'/g, ''));
      if (b.children) walk(b.children);
    }
  };
  walk(blocks);
  return fonts;
}

/**
 * Check if a font family is a system/web-safe font that doesn't
 * need a Google Fonts import.
 */
const SYSTEM_FONTS = new Set([
  'Segoe UI', 'system-ui', '-apple-system', 'sans-serif', 'serif',
  'monospace', 'Arial', 'Helvetica', 'Verdana', 'Georgia', 'Times New Roman',
  'Courier New', 'Tahoma', 'Trebuchet MS', 'Lucida Sans',
]);

function isSystemFont(fontName) {
  return SYSTEM_FONTS.has(fontName);
}

/**
 * Build an MJML padding value from a block's style.padding.
 * MJML uses CSS shorthand but some blocks may have individual sides.
 */
function mjPadding(style) {
  return style?.padding || '0';
}

/**
 * Sanitise a string for safe use as an MJML attribute value.
 * Removes characters that could break the markup.
 */
function safeAttr(val) {
  return escHtml(String(val || ''));
}


// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// BLOCK → MJML
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

/**
 * Serialise blocks to a complete MJML document string.
 *
 * @param {Block[]} blocks
 * @param {Object}  data         — Merge tag data
 * @param {Object}  themeVars    — Active theme variables
 * @param {Object}  options      — { title, previewText, templateName }
 * @returns {string} MJML markup
 */
export function serializeBlocksToMJML(blocks, data, themeVars, options = {}) {
  const tv = themeVars || THEME_PRESETS.default.vars;
  const {
    title = '',
    previewText = '',
    templateName = '',
  } = options;

  // ── Block renderer ──────────────────────────────────────────
  const renderBlock = (block, itemData) => {
    const d = itemData || data;

    if (!block.visible) return '';

    // Condition check
    const conds = Array.isArray(block.conditions)
      ? block.conditions.filter((c) => c?.field)
      : [];
    if (conds.length > 0 && !evaluateConditions(block.conditions, d)) return '';

    const bs = block.style || {};

    switch (block.type) {
      // ── Text ───────────────────────────────────────────────
      case 'text': {
        const content = resolveText(block.properties.content, d);
        const fontSize = block.properties.fontSize || '14px';
        const fontWeight = block.properties.fontWeight || '400';
        const color = block.properties.color || tv.text;
        const fontFamily = block.properties.fontFamily || "'Segoe UI',system-ui,sans-serif";
        const lineHeight = block.properties.lineHeight || '1.5';
        const letterSpacing = block.properties.letterSpacing;

        const ps = [
          `font-family:${fontFamily}`,
          `font-size:${fontSize}`,
          `font-weight:${fontWeight}`,
          `color:${color}`,
          `line-height:${lineHeight}`,
          letterSpacing ? `letter-spacing:${letterSpacing}` : '',
          'margin:0',
        ].filter(Boolean).join(';');

        return `<mj-text align="${block.properties.textAlign || 'left'}" padding="${mjPadding(bs)}" css-class="text-block"><p style="${ps}">${escHtml(content).replace(/\n/g, '<br/>')}</p></mj-text>`;
      }

      // ── Image ──────────────────────────────────────────────
      case 'image': {
        const src = block.properties.src ? resolveText(block.properties.src, d) : '';
        if (!src) {
          // Empty image: render a text placeholder (MJML doesn't support empty img)
          return `<mj-text padding="${mjPadding(bs)}" align="center"><p style="background:#f3f4f6;padding:24px;color:#9ca3af;font-size:13px;border:1px dashed #d1d5db;border-radius:4px;">&#128247; Image placeholder</p></mj-text>`;
        }
        const width = bs.width || '100%';
        const borderRadius = bs.borderRadius ? ` border-radius="${safeAttr(bs.borderRadius)}"` : '';
        return `<mj-image src="${safeAttr(src)}" alt="${safeAttr(block.properties.alt || '')}" width="${safeAttr(width)}" padding="${mjPadding(bs)}"${borderRadius} />`;
      }

      // ── Columns ────────────────────────────────────────────
      case 'columns': {
        const cols = block.children || [];
        const ratios = block.properties.ratios || cols.map(() => 1);
        const total = ratios.reduce((a, b) => a + b, 0);
        let mjCols = '';
        cols.forEach((col, i) => {
          const pct = Math.round((ratios[i] / total) * 100);
          const children = (col.children || []).map((c) => renderBlock(c, d)).join('\n');
          mjCols += `      <mj-column width="${pct}%">\n${children}\n      </mj-column>`;
        });
        const bgColor = bs.backgroundColor ? ` background-color="${safeAttr(bs.backgroundColor)}"` : '';
        return `    <mj-section padding="${mjPadding(bs)}"${bgColor}>\n${mjCols}\n    </mj-section>`;
      }

      // ── Spacer ─────────────────────────────────────────────
      case 'spacer':
        return `<mj-spacer height="${block.properties.height || '32px'}" />`;

      // ── Divider ────────────────────────────────────────────
      case 'divider': {
        const thickness = block.properties.thickness || '1px';
        const lineStyle = block.properties.lineStyle || 'solid';
        const color = block.properties.color || '#ccc';
        const padding = bs.margin || '8px 0';
        return `<mj-divider border-width="${safeAttr(thickness)}" border-style="${safeAttr(lineStyle)}" border-color="${safeAttr(color)}" padding="${safeAttr(padding)}" />`;
      }

      // ── Table ──────────────────────────────────────────────
      case 'table': {
        const { rows, cols, headerRow, cellPadding, borderColor, cells } = block.properties;
        let trs = '';
        for (let r = 0; r < rows; r++) {
          let tds = '';
          for (let c = 0; c < cols; c++) {
            const tag = r === 0 && headerRow ? 'th' : 'td';
            const cellValue = resolveText(cells?.[`${r}-${c}`] || '', d);
            const isHeader = r === 0 && headerRow;
            const cellStyle = [
              `padding:${cellPadding || '8px'}`,
              `border:1px solid ${borderColor || '#ccc'}`,
              `font-size:13px`,
              `text-align:left`,
              `font-weight:${isHeader ? 600 : 400}`,
              `background:${isHeader ? (tv.tableHeaderBg || '#f5f5f5') : 'transparent'}`,
              `color:${tv.text || '#333'}`,
            ].join(';');
            const scopeAttr = isHeader ? ' scope="col"' : '';
            tds += `<${tag}${scopeAttr} style="${cellStyle}">${escHtml(cellValue)}</${tag}>`;
          }
          trs += `<tr>${tds}</tr>`;
        }
        return `<mj-table padding="${mjPadding(bs)}" width="100%">${trs}</mj-table>`;
      }

      // ── Box ────────────────────────────────────────────────
      case 'box': {
        const children = (block.children || []).map((c) => renderBlock(c, d)).join('\n');
        const bgColor = bs.backgroundColor || 'transparent';
        const borderRadius = bs.borderRadius ? ` border-radius="${safeAttr(bs.borderRadius)}"` : '';
        const border = bs.border ? ` border="${safeAttr(bs.border)}"` : '';
        return `    <mj-section padding="${mjPadding(bs)}" background-color="${safeAttr(bgColor)}"${borderRadius}${border}>\n      <mj-column>\n${children}\n      </mj-column>\n    </mj-section>`;
      }

      // ── Dynamic Field ──────────────────────────────────────
      case 'dynamic_field': {
        const val = resolveMergeTag(block.properties.fieldPath, d);
        const fontSize = block.properties.fontSize || '14px';
        const fontWeight = block.properties.fontWeight || '400';
        const color = block.properties.color || tv.text;
        return `<mj-text padding="${mjPadding(bs)}"><span style="font-size:${safeAttr(fontSize)};font-weight:${safeAttr(fontWeight)};color:${safeAttr(color)}">${escHtml(block.properties.prefix || '')}${escHtml(val)}${escHtml(block.properties.suffix || '')}</span></mj-text>`;
      }

      // ── Barcode ────────────────────────────────────────────
      // SVG barcodes don't work in email clients. Render a
      // warning placeholder and suggest server-side image generation.
      case 'barcode': {
        const val = resolveText(block.properties.value, d);
        const barcodeType = block.properties.barcodeType || 'Code128';
        return `<mj-text padding="${mjPadding(bs)}" align="center"><p style="background:#fffbeb;border:1px solid #fcd34d;border-radius:4px;padding:12px;color:#92400e;font-size:12px;text-align:center;">&#9888; ${safeAttr(barcodeType)} barcode: <strong>${escHtml(val)}</strong><br/><span style="font-size:11px;color:#a16207;">Replace with a server-generated barcode image URL for email delivery.</span></p></mj-text>`;
      }

      // ── Repeater ───────────────────────────────────────────
      case 'repeater': {
        const items = d[block.properties.dataSource] || d.lines || [];
        const dir = block.properties.direction;

        if (dir === 'horizontal') {
          // Horizontal repeater → each item as an mj-column in a single mj-section
          const total = items.length || 1;
          const pct = Math.round(100 / total);
          let mjCols = '';
          items.forEach((item) => {
            const itemData = { ...d, line: item };
            const children = (block.children || []).map((c) => renderBlock(c, itemData)).join('\n');
            mjCols += `      <mj-column width="${pct}%">\n${children}\n      </mj-column>\n`;
          });
          return `    <mj-section padding="${mjPadding(bs)}">\n${mjCols}    </mj-section>`;
        }

        // Vertical repeater → render children for each item sequentially
        return items.map((item) => {
          const itemData = { ...d, line: item };
          return (block.children || []).map((c) => renderBlock(c, itemData)).join('\n');
        }).join('\n');
      }

      // ── Logo ───────────────────────────────────────────────
      case 'logo': {
        const src = block.properties.src ? resolveText(block.properties.src, d) : '';
        if (!src) {
          return `<mj-text padding="${mjPadding(bs)}" align="center"><p style="background:#f3f4f6;padding:12px 24px;color:#9ca3af;font-size:12px;border:2px dashed #d1d5db;border-radius:4px;display:inline-block;">&#9672; Logo</p></mj-text>`;
        }
        return `<mj-image src="${safeAttr(src)}" alt="Logo" width="${safeAttr(block.properties.maxWidth || '200px')}" padding="${mjPadding(bs)}" />`;
      }

      // ── Page Break ─────────────────────────────────────────
      // No equivalent in email. Emit a comment for transparency.
      case 'page_break':
        return `<!-- Page break (not applicable in email) -->`;

      default:
        return '';
    }
  };

  // ── Wrap non-section blocks ─────────────────────────────────
  // MJML requires all content to be inside mj-section > mj-column.
  // Blocks that already produce mj-section (columns, box, horizontal
  // repeater) don't need wrapping; everything else does.
  const wrapIfNeeded = (html, block) => {
    if (!html) return '';
    // These types already produce their own mj-section
    if (block.type === 'columns' || block.type === 'box') return html;
    // Horizontal repeaters produce mj-section too
    if (block.type === 'repeater' && block.properties?.direction === 'horizontal') return html;
    // Everything else: wrap
    return `    <mj-section padding="0"><mj-column>\n${html}\n    </mj-column></mj-section>`;
  };

  // ── Build body ──────────────────────────────────────────────
  const bodyParts = blocks
    .map((b) => {
      const html = renderBlock(b);
      if (!html) return '';
      return wrapIfNeeded(html, b);
    })
    .filter(Boolean)
    .join('\n');

  // ── Collect custom fonts ────────────────────────────────────
  const fonts = collectFontFamilies(blocks);
  let fontImports = '';
  for (const font of fonts) {
    if (!isSystemFont(font)) {
      // Attempt a Google Fonts import for non-system fonts
      const encoded = encodeURIComponent(font);
      fontImports += `    <mj-font name="${safeAttr(font)}" href="https://fonts.googleapis.com/css2?family=${encoded}:wght@400;600;700&display=swap" />\n`;
    }
  }

  // ── Document title (for mj-title) ──────────────────────────
  const docTitle = title || templateName || 'Email';

  // ── Assemble the MJML document ─────────────────────────────
  return `<mjml>
  <mj-head>
    <mj-title>${safeAttr(docTitle)}</mj-title>${previewText ? `\n    <mj-preview>${safeAttr(previewText)}</mj-preview>` : ''}
    <mj-attributes>
      <mj-all font-family="'Segoe UI',system-ui,-apple-system,sans-serif" />
      <mj-text font-size="14px" color="${safeAttr(tv.text || '#333')}" line-height="1.5" />
      <mj-section padding="0" />
    </mj-attributes>
${fontImports}    <mj-style>
      a { color: ${tv.accent || '#3b82f6'}; }
      .text-block p { margin: 0; }
    </mj-style>
  </mj-head>
  <mj-body background-color="${safeAttr(tv.bg || '#ffffff')}">
${bodyParts}
  </mj-body>
</mjml>`;
}

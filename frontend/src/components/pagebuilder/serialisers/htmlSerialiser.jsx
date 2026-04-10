// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// HTML SERIALISER — SESSION 2 UPDATED
// SESSION 2 ADDITIONS:
//   • Opacity + rotation applied to all blocks
//   • Font family values passed through for text/dynamic_field
//   • Bold / italic / underline / text-align on dynamic_field
//   • Border colour / width / style on text and box blocks
//   • Table: header bg, header text colour, alternating row colour,
//     per-column widths
//   • Barcode: barcodeDataSource field path resolution
//   • Keep aspect ratio on image
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

import { THEME_PRESETS } from '../constants/index.jsx';
import {
  resolveText, resolveMergeTag, escHtml, unitToPx,
  evaluateConditions,
  generateCode128SVG, generateEAN13SVG, generateQRSVG,
} from '../utils/index.jsx';

export function styleToInline(style) {
  if (!style || typeof style !== 'object') return '';
  return Object.entries(style)
    .filter(([, v]) => v !== undefined && v !== null && v !== '')
    .map(([k, v]) => `${k.replace(/([A-Z])/g, '-$1').toLowerCase()}:${v}`)
    .join(';');
}

function parsePxValue(val, fallback = 0) {
  if (!val) return fallback;
  const n = parseFloat(String(val));
  return isNaN(n) ? fallback : n;
}

function semanticTag(fontSize) {
  const px = parsePxValue(fontSize);
  if (px >= 32) return 'h1';
  if (px >= 26) return 'h2';
  if (px >= 22) return 'h3';
  if (px >= 18) return 'h4';
  if (px >= 16) return 'h5';
  return 'div';
}

function blockTransformStyle(props) {
  const out = {};
  if (props.rotation) out.transform = `rotate(${props.rotation}deg)`;
  if (props.opacity !== undefined && props.opacity !== 1) out.opacity = props.opacity;
  return out;
}

function propBorder(props) {
  const w = props.borderWidth || '1px';
  const s = props.borderStyle || 'solid';
  const c = props.borderColor;
  if (!c) return {};
  return { border: `${w} ${s} ${c}` };
}

export function serializeBlocksToHTML(blocks, data, themeVars, options = {}) {
  const { forEmail = false, forEbay = false } = options;
  const tv = themeVars || THEME_PRESETS.default.vars;

  const renderBlock = (block, itemData) => {
    const d = itemData || data;
    if (!block.visible) return '';
    const conds = Array.isArray(block.conditions) ? block.conditions.filter((c) => c?.field) : [];
    if (conds.length > 0 && !evaluateConditions(block.conditions, d)) return '';
    const bs = block.style || {};
    const props = block.properties || {};

    switch (block.type) {
      case 'text': {
        const content = resolveText(props.content, d);
        const tag = semanticTag(props.fontSize);
        const s = {
          ...bs,
          ...blockTransformStyle(props),
          fontFamily:     props.fontFamily || "'Segoe UI',system-ui,sans-serif",
          fontSize:       props.fontSize,
          fontWeight:     props.fontWeight,
          fontStyle:      props.fontStyle,
          textDecoration: props.textDecoration,
          color:          props.color,
          textAlign:      props.textAlign,
          lineHeight:     props.lineHeight,
          letterSpacing:  props.letterSpacing,
          whiteSpace:     'pre-wrap',
          wordBreak:      'break-word',
          ...(props.borderColor ? propBorder(props) : {}),
          ...(tag.startsWith('h') ? { pageBreakAfter: 'avoid', breakAfter: 'avoid' } : {}),
          ...(tag.startsWith('h') ? { margin: bs.margin || '0' } : {}),
        };
        const hasHtml = /<[a-z][\s\S]*>/i.test(content);
        const renderedContent = hasHtml
          ? content.replace(/\n/g, '<br/>')
          : escHtml(content).replace(/\n/g, '<br/>');
        return `<${tag} style="${styleToInline(s)}">${renderedContent}</${tag}>`;
      }

      case 'image': {
        const src = props.src ? resolveText(props.src, d) : '';
        const imgHeight = bs.height || '200px';
        const imgRadius = bs.borderRadius || '0';
        const keepRatio = props.keepAspectRatio !== false;
        const outerStyle = { ...bs, ...blockTransformStyle(props) };
        if (!src) {
          return `<div style="${styleToInline(outerStyle)}"><div style="width:100%;height:${imgHeight};background-color:#f3f4f6;border:1px dashed #d1d5db;border-radius:${imgRadius};display:flex;align-items:center;justify-content:center;color:#9ca3af;font-size:13px;font-family:sans-serif;" role="img" aria-label="Image placeholder">&#128247; Image</div></div>`;
        }
        const heightStyle = keepRatio ? 'auto' : (imgHeight === '200px' ? 'auto' : imgHeight);
        return `<div style="${styleToInline(outerStyle)}"><img src="${escHtml(src)}" alt="${escHtml(props.alt || '')}" style="width:100%;height:${heightStyle};object-fit:${props.objectFit || 'contain'};border-radius:${imgRadius};display:block;" /></div>`;
      }

      case 'columns': {
        const cols = block.children || [];
        const ratios = props.ratios || cols.map(() => 1);
        const totalRatio = ratios.reduce((a, b) => a + b, 0);
        const gapPx = parseInt(props.gap || '16');
        const outerStyle = { ...bs, ...blockTransformStyle(props) };
        if (forEmail || forEbay) {
          let tds = '';
          cols.forEach((col, i) => {
            const pct = Math.round((ratios[i] / totalRatio) * 100);
            const children = (col.children || []).map((c) => renderBlock(c, d)).join('');
            tds += `<td style="width:${pct}%;vertical-align:top;padding:0 ${gapPx / 2}px;">${children}</td>`;
          });
          return `<div style="${styleToInline(outerStyle)}"><table width="100%" cellpadding="0" cellspacing="0" border="0" role="presentation" style="border-collapse:collapse;"><tr>${tds}</tr></table></div>`;
        }
        let colHtml = '';
        cols.forEach((col, i) => {
          const children = (col.children || []).map((c) => renderBlock(c, d)).join('');
          colHtml += `<div style="flex:${ratios[i]} 0 0%;min-height:10px;">${children}</div>`;
        });
        return `<div style="${styleToInline(outerStyle)};display:flex;gap:${props.gap || '16px'}">${colHtml}</div>`;
      }

      case 'spacer':
        return `<div style="height:${props.height || '32px'};${styleToInline(bs)}"></div>`;

      case 'divider':
        return `<div style="${styleToInline({ ...bs, ...blockTransformStyle(props) })}"><hr style="border:none;border-top:${props.thickness || '1px'} ${props.lineStyle || 'solid'} ${props.color || '#ccc'};margin:0;" /></div>`;

      case 'table': {
        const { rows, cols, headerRow, cellPadding, borderColor, cells, headerBg, headerColor, altRowEnabled, altRowColor, colWidths } = props;
        const outerStyle = { ...bs, ...blockTransformStyle(props) };
        let trs = '';
        for (let r = 0; r < rows; r++) {
          const rowBg = altRowEnabled && r % 2 === 0 && !(r === 0 && headerRow) ? (altRowColor || '#f9f9f9') : '';
          let tds = '';
          for (let c = 0; c < cols; c++) {
            const tag = r === 0 && headerRow ? 'th' : 'td';
            const cellValue = resolveText(cells?.[`${r}-${c}`] || '', d);
            const isHeader = r === 0 && headerRow;
            const widths = colWidths || Array.from({ length: cols }, () => Math.round(100 / cols));
            const colW = widths[c] ? `${widths[c]}%` : `${Math.round(100 / cols)}%`;
            const cellStyle = [
              `padding:${cellPadding || '8px'}`,
              `border:1px solid ${borderColor || '#ccc'}`,
              `width:${colW}`,
              isHeader && headerBg ? `background-color:${headerBg}` : (rowBg ? `background-color:${rowBg}` : ''),
              isHeader && headerColor ? `color:${headerColor}` : '',
              isHeader ? 'font-weight:700' : '',
            ].filter(Boolean).join(';');
            tds += `<${tag} style="${cellStyle}">${escHtml(cellValue)}</${tag}>`;
          }
          trs += `<tr>${tds}</tr>`;
        }
        return `<div style="${styleToInline(outerStyle)}"><table style="border-collapse:collapse;width:100%;table-layout:fixed;">${trs}</table></div>`;
      }

      case 'box': {
        const children = (block.children || []).map((c) => renderBlock(c, d)).join('');
        const boxStyle = { ...bs, ...(props.borderColor ? propBorder(props) : {}), borderRadius: bs.borderRadius || '4px', ...blockTransformStyle(props) };
        return `<div style="${styleToInline(boxStyle)}">${children}</div>`;
      }

      case 'dynamic_field': {
        let val;
        const inputMode = props.inputMode || props.dataSource || 'field';
        if (inputMode === 'static') {
          val = props.staticValue || '';
        } else if (inputMode === 'formula') {
          // Emit a formula placeholder comment — resolved by backend ResolveMergeTagsWithFormulas
          // For frontend preview we can do a basic inline evaluation if evaluateFormula is available
          val = props.formula
            ? `<!-- formula:${block.id}:${props.formula} -->`
            : '';
        } else {
          val = resolveMergeTag(props.fieldPath, d);
        }
        const s = {
          ...bs,
          fontFamily:     props.fontFamily || "'Segoe UI',system-ui,sans-serif",
          fontSize:       props.fontSize,
          fontWeight:     props.fontWeight,
          fontStyle:      props.fontStyle,
          textDecoration: props.textDecoration,
          color:          props.color,
          textAlign:      props.textAlign,
          ...blockTransformStyle(props),
        };
        return `<div style="${styleToInline(s)}">${escHtml(props.prefix || '')}${escHtml(val)}${escHtml(props.suffix || '')}</div>`;
      }

      case 'barcode': {
        let val;
        if (props.barcodeDataSource === 'field' && props.barcodeField) {
          val = resolveMergeTag(props.barcodeField, d);
        } else {
          val = resolveText(props.value, d);
        }
        val = val || '0000';
        const w = props.barcodeWidth || '200';
        const h = props.barcodeHeight || '80';
        let svg;
        if (props.barcodeType === 'QR') svg = generateQRSVG(val, h);
        else if (props.barcodeType === 'EAN-13') svg = generateEAN13SVG(val, w, h);
        else svg = generateCode128SVG(val, w, h);
        const outerStyle = { ...bs, ...blockTransformStyle(props), textAlign: bs.textAlign || 'center' };
        return `<div style="${styleToInline(outerStyle)}">${svg}</div>`;
      }

      case 'logo': {
        const src = props.src ? resolveText(props.src, d) : '';
        const maxW = props.maxWidth || '200px';
        const maxH = props.maxHeight || '80px';
        const outerStyle = { ...bs, ...blockTransformStyle(props) };
        if (!src) {
          return `<div style="${styleToInline(outerStyle)}"><div style="max-width:${maxW};height:${maxH};background-color:#f3f4f6;display:inline-flex;align-items:center;justify-content:center;padding:8px 24px;border:2px dashed #d1d5db;border-radius:4px;color:#9ca3af;font-size:12px;font-family:sans-serif;">&#9672; Logo</div></div>`;
        }
        return `<div style="${styleToInline(outerStyle)}"><img src="${escHtml(src)}" alt="Logo" style="max-width:${maxW};max-height:${maxH};display:block;" /></div>`;
      }

      case 'repeater': {
        const items = d[props.dataSource] || d.lines || [];
        const dir = props.direction;
        let inner = '';
        items.forEach((item) => {
          const itemData = { ...d, line: item };
          const children = (block.children || []).map((c) => renderBlock(c, itemData)).join('');
          inner += dir === 'horizontal' ? `<div style="flex:1;">${children}</div>` : children;
        });
        const wrapperStyle = dir === 'horizontal'
          ? `${styleToInline({ ...bs, ...blockTransformStyle(props) })};display:flex;gap:4px`
          : styleToInline({ ...bs, ...blockTransformStyle(props) });
        return `<div style="${wrapperStyle}">${inner}</div>`;
      }

      case 'page_break':
        return `<div style="page-break-after:always;break-after:page;height:0;overflow:hidden;"></div>`;

      case 'unordered_list': {
        const items = props.items || [];
        const s = { ...bs, ...blockTransformStyle(props), fontFamily: props.fontFamily || "'Segoe UI',system-ui,sans-serif", fontSize: props.fontSize || '14px', color: props.color || '#333333', margin: bs.margin || '0', paddingLeft: '24px' };
        const liHtml = items.map((item) => `<li style="margin-bottom:4px;">${escHtml(resolveText(item, d))}</li>`).join('');
        return `<ul style="${styleToInline(s)}">${liHtml}</ul>`;
      }

      case 'ordered_list': {
        const items = props.items || [];
        const s = { ...bs, ...blockTransformStyle(props), fontFamily: props.fontFamily || "'Segoe UI',system-ui,sans-serif", fontSize: props.fontSize || '14px', color: props.color || '#333333', margin: bs.margin || '0', paddingLeft: '24px' };
        const liHtml = items.map((item) => `<li style="margin-bottom:4px;">${escHtml(resolveText(item, d))}</li>`).join('');
        return `<ol style="${styleToInline(s)}">${liHtml}</ol>`;
      }

      default:
        return '';
    }
  };

  return blocks.map((b) => renderBlock(b)).join('\n');
}

export function generateFullHTML(blocks, data, themeVars, canvas, templateName, options = {}) {
  const tv = themeVars || THEME_PRESETS.default.vars;
  const body = serializeBlocksToHTML(blocks, data, tv, options);
  const widthPx = unitToPx(canvas.width, canvas.unit);

  let pageSize = 'auto';
  if (canvas.unit === 'mm') {
    pageSize = `${canvas.width}mm ${canvas.height === 'auto' ? 'auto' : canvas.height + 'mm'}`;
  } else if (canvas.unit === 'in') {
    pageSize = `${canvas.width}in ${canvas.height === 'auto' ? 'auto' : canvas.height + 'in'}`;
  } else {
    const wMm = Math.round(canvas.width / 3.7795 * 100) / 100;
    const hMm = canvas.height === 'auto' ? 'auto' : Math.round(canvas.height / 3.7795 * 100) / 100 + 'mm';
    pageSize = `${wMm}mm ${hMm}`;
  }

  const u = canvas.unit || 'px';
  const mTop    = canvas.marginTop    ?? 0;
  const mRight  = canvas.marginRight  ?? 0;
  const mBottom = canvas.marginBottom ?? 0;
  const mLeft   = canvas.marginLeft   ?? 0;

  let pageMargin;
  if (u === 'px') {
    const toMm = (px) => Math.round(px / 3.7795 * 100) / 100;
    pageMargin = `${toMm(mTop)}mm ${toMm(mRight)}mm ${toMm(mBottom)}mm ${toMm(mLeft)}mm`;
  } else {
    pageMargin = `${mTop}${u} ${mRight}${u} ${mBottom}${u} ${mLeft}${u}`;
  }

  return `<!DOCTYPE html>
<!-- Generated by PageBuilder — ${escHtml(templateName)} -->
<html lang="en">
<head>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width, initial-scale=1"/>
  <title>${escHtml(templateName)}</title>
  <style>
    @page { size: ${pageSize}; margin: ${pageMargin}; }
    *, *::before, *::after { box-sizing: border-box; }
    html, body { margin: 0; padding: 0; font-family: 'Segoe UI', system-ui, -apple-system, sans-serif; -webkit-print-color-adjust: exact; print-color-adjust: exact; color-adjust: exact; }
    p, div, td, th, li { orphans: 3; widows: 3; }
    h1, h2, h3, h4, h5, h6 { orphans: 4; widows: 4; page-break-after: avoid; break-after: avoid; }
    thead { display: table-header-group; }
    tr { page-break-inside: avoid; break-inside: avoid; }
    img { page-break-inside: avoid; break-inside: avoid; }
    a { color: ${tv.accent || '#3b82f6'}; text-decoration: underline; }
    @media print { body { background: none !important; } .no-print { display: none !important; } a[href]::after { content: none !important; } }
  </style>
</head>
<body>
  <div style="max-width:${widthPx}px;margin:0 auto;background-color:${canvas.backgroundColor || tv.bg || '#ffffff'};">
${body}
  </div>
</body>
</html>`;
}

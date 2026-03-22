// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// EBAY HTML SANITISER (Feature #9 — Polished)
// Strips elements, attributes, and URL schemes that eBay's Active
// Content Policy forbids: <script>, <iframe>, event handlers,
// javascript: URLs, data: URIs, SVGs (converted to placeholders),
// <style> tags, <meta>, <link>, etc.
//
// POLISHED (A2):
//   ✓ DOMParser-based stripping (not regex) for reliability
//   ✓ Recursive attribute cleaning on every element
//   ✓ Inline SVGs converted to <img> placeholders
//   ✓ data: URI sources stripped for security
//   ✓ CSS expression() and url(javascript:) removal from inline styles
//   ✓ Byte count utility for eBay's listing size limits
//   ✓ Validation report: warnings for stripped content
//
// Falls back to regex stripping if DOMParser isn't available
// (server-side rendering without jsdom).
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// ── eBay Active Content Policy: forbidden elements ───────────
const FORBIDDEN_TAGS = new Set([
  'script', 'iframe', 'frame', 'frameset',
  'form', 'input', 'textarea', 'select', 'button',
  'object', 'embed', 'applet',
  'link', 'style', 'meta',
  'video', 'audio', 'source',
  'base',
]);

// Attributes that eBay strips (event handlers are caught separately)
const FORBIDDEN_ATTRS = new Set([
  'srcdoc', 'data-bind', 'formaction',
]);

// ── Regex patterns for inline style sanitisation ─────────────
const STYLE_DANGER_PATTERNS = [
  /expression\s*\([^)]*\)/gi,          // CSS expression()
  /url\s*\(\s*(['"]?)javascript:/gi,    // url(javascript:...)
  /url\s*\(\s*(['"]?)data:/gi,          // url(data:...) in styles
  /-moz-binding\s*:/gi,                 // Firefox XBL binding
  /behavior\s*:/gi,                     // IE behavior property
];


// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// DOM-BASED SANITISER (primary path)
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

/**
 * Clean inline style strings of dangerous CSS expressions.
 * @param {string} styleStr
 * @returns {string} Cleaned style string
 */
function sanitizeInlineStyle(styleStr) {
  if (!styleStr) return '';
  let clean = styleStr;
  for (const pattern of STYLE_DANGER_PATTERNS) {
    clean = clean.replace(pattern, '/* removed */');
  }
  return clean;
}

/**
 * Convert inline <svg> elements to a placeholder <img> or a
 * simple text description, since eBay blocks inline SVGs.
 * @param {Element} svgEl
 * @returns {Element} Replacement element
 */
function svgToPlaceholder(svgEl, doc) {
  const width = svgEl.getAttribute('width') || '100';
  const height = svgEl.getAttribute('height') || '40';
  const placeholder = doc.createElement('div');
  placeholder.setAttribute('style',
    `width:${width}px;height:${height}px;background:#f9fafb;border:1px dashed #d1d5db;` +
    `display:inline-flex;align-items:center;justify-content:center;` +
    `font-size:11px;color:#6b7280;font-family:sans-serif;border-radius:2px;`
  );
  placeholder.textContent = '[Barcode]';
  return placeholder;
}

/**
 * Recursively sanitise a DOM node and its children in-place.
 * Returns a warnings array of stripped content descriptions.
 */
function sanitizeNode(node, doc, warnings) {
  if (node.nodeType === 3) return; // Text node — safe

  if (node.nodeType !== 1) {
    // Comment or other non-element nodes: remove
    node.parentNode?.removeChild(node);
    return;
  }

  const tagName = node.tagName.toLowerCase();

  // ── Remove forbidden tags entirely ──────────────────────
  if (FORBIDDEN_TAGS.has(tagName)) {
    warnings.push(`Stripped <${tagName}> element (eBay Active Content Policy)`);
    node.parentNode?.removeChild(node);
    return;
  }

  // ── Convert inline SVG to placeholder ───────────────────
  if (tagName === 'svg') {
    const placeholder = svgToPlaceholder(node, doc);
    node.parentNode?.replaceChild(placeholder, node);
    warnings.push('Converted inline SVG to placeholder (eBay blocks inline SVG)');
    return;
  }

  // ── Sanitise attributes ─────────────────────────────────
  const attrs = Array.from(node.attributes || []);
  for (const attr of attrs) {
    const name = attr.name.toLowerCase();

    // Event handlers (onclick, onload, onerror, etc.)
    if (name.startsWith('on')) {
      node.removeAttribute(attr.name);
      continue;
    }

    // Forbidden attribute names
    if (FORBIDDEN_ATTRS.has(name)) {
      node.removeAttribute(attr.name);
      continue;
    }

    // javascript: and data: in href/src/action attributes
    const val = (attr.value || '').trim().toLowerCase();
    if (['href', 'src', 'action', 'poster', 'background'].includes(name)) {
      if (val.startsWith('javascript:')) {
        node.setAttribute(attr.name, '#');
        warnings.push(`Replaced javascript: URL in ${name} attribute`);
      } else if (val.startsWith('data:') && name === 'src') {
        // eBay blocks data: URIs in src (potential XSS vector)
        node.removeAttribute(attr.name);
        warnings.push('Stripped data: URI from src attribute');
      }
    }

    // Clean inline styles
    if (name === 'style') {
      node.setAttribute('style', sanitizeInlineStyle(attr.value));
    }
  }

  // ── Recurse into children (iterate backwards for safe removal) ──
  const children = Array.from(node.childNodes);
  for (const child of children) {
    sanitizeNode(child, doc, warnings);
  }

  // ── eBay anchor hardening ───────────────────────────────────
  // All remaining <a> tags should open in a new tab and include
  // rel="noopener noreferrer" to prevent tab-nabbing attacks.
  if (tagName === 'a') {
    node.setAttribute('target', '_blank');
    node.setAttribute('rel', 'noopener noreferrer');
  }
}


// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// REGEX FALLBACK (when DOMParser isn't available)
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

function regexSanitize(html) {
  let cleaned = html;

  // Strip forbidden tags (both paired and self-closing)
  for (const tag of FORBIDDEN_TAGS) {
    // Paired: <tag ...>...</tag>
    cleaned = cleaned.replace(
      new RegExp(`<${tag}[^>]*>[\\s\\S]*?</${tag}>`, 'gi'),
      ''
    );
    // Self-closing: <tag ... /> or <tag ...>
    cleaned = cleaned.replace(
      new RegExp(`<${tag}[^>]*/?>`, 'gi'),
      ''
    );
  }

  // Strip inline SVGs (paired tags)
  cleaned = cleaned.replace(/<svg[^>]*>[\s\S]*?<\/svg>/gi, '<div style="width:100px;height:40px;background:#f9fafb;border:1px dashed #d1d5db;display:inline-flex;align-items:center;justify-content:center;font-size:11px;color:#6b7280;">[Barcode]</div>');

  // Strip event handler attributes
  cleaned = cleaned.replace(/\s+on\w+\s*=\s*"[^"]*"/gi, '');
  cleaned = cleaned.replace(/\s+on\w+\s*=\s*'[^']*'/gi, '');
  cleaned = cleaned.replace(/\s+on\w+\s*=\s*[^\s>]*/gi, '');

  // Replace javascript: URLs
  cleaned = cleaned.replace(/href\s*=\s*"javascript:[^"]*"/gi, 'href="#"');
  cleaned = cleaned.replace(/src\s*=\s*"javascript:[^"]*"/gi, '');

  // Strip data: URIs from src
  cleaned = cleaned.replace(/src\s*=\s*"data:[^"]*"/gi, '');

  // Strip CSS expressions from inline styles
  cleaned = cleaned.replace(/expression\s*\([^)]*\)/gi, '');
  cleaned = cleaned.replace(/(-moz-binding|behavior)\s*:[^;"']*/gi, '');

  // Harden <a> tags: add target="_blank" and rel="noopener noreferrer"
  cleaned = cleaned.replace(/<a\s/gi, '<a target="_blank" rel="noopener noreferrer" ');

  return cleaned;
}


// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// PUBLIC API
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

/**
 * Remove eBay-forbidden elements and attributes from an HTML string.
 * Uses DOMParser when available (browser), falls back to regex.
 *
 * @param {string} html — Raw HTML from the serialiser
 * @returns {{ html: string, warnings: string[] }} Sanitised HTML + warnings
 */
export function sanitizeForEbay(html) {
  if (!html) return { html: '', warnings: [] };

  // Try DOM-based approach first
  if (typeof DOMParser !== 'undefined') {
    try {
      const parser = new DOMParser();
      const doc = parser.parseFromString(
        `<div id="__ebay_root">${html}</div>`,
        'text/html'
      );
      const root = doc.getElementById('__ebay_root');
      const warnings = [];

      if (root) {
        sanitizeNode(root, doc, warnings);
        return { html: root.innerHTML, warnings };
      }
    } catch (_err) {
      // Fall through to regex
    }
  }

  // Regex fallback
  return { html: regexSanitize(html), warnings: [] };
}


/**
 * Calculate the byte size of an HTML string (UTF-8).
 * eBay has a ~500KB listing description limit.
 *
 * @param {string} html
 * @returns {{ bytes: number, kb: string, isOverLimit: boolean, limitKB: number }}
 */
export function getEbayByteCount(html) {
  const EBAY_LIMIT_KB = 500;
  let bytes;

  if (typeof TextEncoder !== 'undefined') {
    bytes = new TextEncoder().encode(html).length;
  } else {
    // Fallback: approximate UTF-8 byte count
    bytes = new Blob([html]).size;
  }

  return {
    bytes,
    kb: (bytes / 1024).toFixed(1),
    isOverLimit: bytes > EBAY_LIMIT_KB * 1024,
    limitKB: EBAY_LIMIT_KB,
  };
}

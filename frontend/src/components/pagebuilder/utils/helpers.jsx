// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// UTILITY FUNCTIONS
// Pure helpers with no React dependencies. Used across the entire
// builder: serialisers, canvas renderer, property editor, etc.
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// ── Unique ID generator ────────────────────────────────────────
// Produces IDs like "blk_m2abc_1". The counter prevents collisions
// when multiple blocks are created in the same millisecond.
let _idCounter = 0;

export const uid = () =>
  `blk_${Date.now().toString(36)}_${(++_idCounter).toString(36)}`;

// ── Deep clone (JSON round-trip) ───────────────────────────────
// Good enough for our plain-object block trees. Doesn't handle
// functions, Dates, or circular refs — none of which appear here.
export const deepClone = (obj) => JSON.parse(JSON.stringify(obj));

// ── Merge tag resolution ───────────────────────────────────────
// resolveMergeTag('customer.name', data) → 'Sarah Mitchell'
// If the path doesn't exist, returns the raw tag: {{customer.name}}

export const resolveMergeTag = (path, data) => {
  const parts = path.split('.');
  let val = data;
  for (const p of parts) {
    val = val?.[p];
  }
  return val ?? `{{${path}}}`;
};

// resolveText replaces all {{...}} tokens in a string with their
// resolved values from the data object.
export const resolveText = (text, data) => {
  if (!text) return '';
  return text.replace(
    /\{\{([^}]+)\}\}/g,
    (_, path) => resolveMergeTag(path.trim(), data)
  );
};

// ── Unit conversion ────────────────────────────────────────────
// Converts mm or inches to CSS pixels (96 dpi standard).
export const unitToPx = (value, unit) => {
  if (unit === 'mm') return value * 3.7795;
  if (unit === 'in') return value * 96;
  return value;
};

// ── HTML entity escaping ───────────────────────────────────────
// Prevents XSS when embedding user content in generated HTML.
export const escHtml = (s) =>
  String(s || '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');

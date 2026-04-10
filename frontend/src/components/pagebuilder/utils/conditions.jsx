// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// CONDITION ENGINE — Session 3 update
// SESSION 3 ADDITIONS:
//   • Operators: not, regex, like_any, like_single
//   • evaluateConditionalStyles() — evaluates conditionalStyles blocks
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

import { resolveMergeTag } from './helpers';

export const CONDITION_OPS = [
  { value: 'eq',           label: 'Equals' },
  { value: 'neq',          label: 'Not Equals' },
  { value: 'gt',           label: 'Greater Than' },
  { value: 'lt',           label: 'Less Than' },
  { value: 'gte',          label: 'Greater or Equal' },
  { value: 'lte',          label: 'Less or Equal' },
  { value: 'contains',     label: 'Contains' },
  { value: 'not_contains', label: 'Does Not Contain' },
  { value: 'empty',        label: 'Is Empty' },
  { value: 'not_empty',    label: 'Is Not Empty' },
  { value: 'starts_with',  label: 'Starts With' },
  { value: 'ends_with',    label: 'Ends With' },
  { value: 'not',          label: 'NOT (logical inverse)' },
  { value: 'regex',        label: 'Matches Regex' },
  { value: 'like_any',     label: 'Like (contains, case-insensitive)' },
  { value: 'like_single',  label: 'Like Single Char (wildcard)' },
];

export function evaluateCondition(cond, data) {
  const { field, operator, value } = cond;
  const fieldValue = String(resolveMergeTag(field, data) || '');
  const stripped = fieldValue.replace(/[GBP$EUR,]/g, '');
  const numField = parseFloat(stripped);
  const numValue = parseFloat(value);

  switch (operator) {
    case 'eq':           return fieldValue === value;
    case 'neq':          return fieldValue !== value;
    case 'gt':           return !isNaN(numField) && !isNaN(numValue) && numField > numValue;
    case 'lt':           return !isNaN(numField) && !isNaN(numValue) && numField < numValue;
    case 'gte':          return !isNaN(numField) && !isNaN(numValue) && numField >= numValue;
    case 'lte':          return !isNaN(numField) && !isNaN(numValue) && numField <= numValue;
    case 'contains':     return fieldValue.toLowerCase().includes((value || '').toLowerCase());
    case 'not_contains': return !fieldValue.toLowerCase().includes((value || '').toLowerCase());
    case 'empty':        return !fieldValue || fieldValue === String('{{' + field + '}}');
    case 'not_empty':    return !!fieldValue && fieldValue !== String('{{' + field + '}}');
    case 'starts_with':  return fieldValue.toLowerCase().startsWith((value || '').toLowerCase());
    case 'ends_with':    return fieldValue.toLowerCase().endsWith((value || '').toLowerCase());
    case 'not':          return !fieldValue.toLowerCase().includes((value || '').toLowerCase());
    case 'regex': {
      try { return new RegExp(value || '', 'i').test(fieldValue); } catch { return false; }
    }
    case 'like_any':     return fieldValue.toLowerCase().includes((value || '').toLowerCase());
    case 'like_single': {
      if (!value || !fieldValue) return false;
      return Array.from(fieldValue).some(function(ch) { return value.indexOf(ch) >= 0; });
    }
    default:             return true;
  }
}

export function evaluateConditions(conditions, data) {
  if (!conditions || conditions.length === 0) return true;
  const logic = conditions._logic || 'and';
  const validConditions = conditions.filter(function(c) { return c && c.field; });
  if (validConditions.length === 0) return true;
  if (logic === 'or') {
    return validConditions.some(function(c) { return evaluateCondition(c, data); });
  }
  return validConditions.every(function(c) { return evaluateCondition(c, data); });
}

// ── Session 3: Conditional Style Configuration ──────────────────
// Each block can optionally have a conditionalStyles array. Each entry:
// {
//   id: string, label: string, logic: 'and'|'or',
//   conditions: Condition[],
//   styles: { backgroundColor?, color?, borderColor?, [cssProp]: string }
// }
//
// evaluateConditionalStyles() returns a merged style object for any
// entries whose conditions evaluate to true, applied on the canvas preview.

export function evaluateConditionalStyles(entries, data) {
  if (!Array.isArray(entries) || entries.length === 0) return {};
  var merged = {};
  for (var i = 0; i < entries.length; i++) {
    var entry = entries[i];
    if (!entry || typeof entry !== 'object') continue;
    var conditions = Array.isArray(entry.conditions) ? entry.conditions : [];
    var condArr = conditions.slice();
    condArr._logic = entry.logic || 'and';
    if (!evaluateConditions(condArr, data)) continue;
    var styles = entry.styles || {};
    Object.assign(merged, styles);
  }
  return merged;
}

export function mergeConditionalStyle(baseStyle, conditionalStyleResult) {
  if (!conditionalStyleResult || Object.keys(conditionalStyleResult).length === 0) {
    return baseStyle;
  }
  return Object.assign({}, baseStyle, conditionalStyleResult);
}

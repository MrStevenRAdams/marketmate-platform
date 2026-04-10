// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// CONDITIONS EDITOR (Feature #3)
// UI for editing the visibility conditions on any block.
// Supports multiple conditions with AND/OR logic toggle.
// Used inside the PropertyEditor for every block type.
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

import React from 'react';
import { X, Filter } from 'lucide-react';
import { T, FIELD_CATEGORIES } from '../../../constants/index.jsx';
import { CONDITION_OPS } from '../../../utils/index.jsx';
import css from './cssHelpers';

/**
 * @param {Condition[]} conditions — Array of condition objects (may have `_logic` property)
 * @param {Function}    onChange   — Called with the updated conditions array
 */
export default function ConditionsEditor({ conditions = [], onChange }) {
  // Filter to valid condition objects
  const safeConditions = Array.isArray(conditions)
    ? conditions.filter((c) => c && typeof c === 'object' && c.field !== undefined)
    : [];

  const logic = conditions?._logic || 'and';

  // ── Handlers ─────────────────────────────────────────────────

  const addCondition = () => {
    const nc = [...safeConditions, { field: 'order.status', operator: 'eq', value: '' }];
    nc._logic = logic;
    onChange(nc);
  };

  const removeCondition = (i) => {
    const nc = safeConditions.filter((_, idx) => idx !== i);
    nc._logic = logic;
    onChange(nc);
  };

  const updateCondition = (i, key, val) => {
    const nc = [...safeConditions];
    nc[i] = { ...nc[i], [key]: val };
    nc._logic = logic;
    onChange(nc);
  };

  const toggleLogic = () => {
    const nc = [...safeConditions];
    nc._logic = logic === 'and' ? 'or' : 'and';
    onChange(nc);
  };

  // ── Render ───────────────────────────────────────────────────

  return (
    <div style={{ marginBottom: 10 }}>
      {/* Header with logic toggle */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 6 }}>
        <span style={css.label}>Conditions</span>
        {safeConditions.length > 1 && (
          <button
            onClick={toggleLogic}
            style={{
              fontSize: 10, fontWeight: 700,
              padding: '2px 8px', borderRadius: T.radius.sm,
              border: `1px solid ${T.primary.glow}`,
              backgroundColor: T.primary.glow,
              color: T.primary.light,
              cursor: 'pointer',
              textTransform: 'uppercase',
              fontFamily: T.font,
            }}
          >
            {logic.toUpperCase()}
          </button>
        )}
      </div>

      {/* Condition rows */}
      {safeConditions.map((cond, i) => (
        <div key={i} style={{ display: 'flex', gap: 4, marginBottom: 4, alignItems: 'center' }}>
          {/* Field selector */}
          <select
            style={{ ...css.select, width: '35%', fontSize: 11, padding: '4px 6px' }}
            value={cond.field}
            onChange={(e) => updateCondition(i, 'field', e.target.value)}
          >
            {Object.entries(FIELD_CATEGORIES).map(([cat, fields]) => (
              <optgroup key={cat} label={cat}>
                {fields.map((f) => (
                  <option key={f.path} value={f.path}>{f.label}</option>
                ))}
              </optgroup>
            ))}
          </select>

          {/* Operator */}
          <select
            style={{ ...css.select, width: '30%', fontSize: 11, padding: '4px 6px' }}
            value={cond.operator}
            onChange={(e) => updateCondition(i, 'operator', e.target.value)}
          >
            {CONDITION_OPS.map((op) => (
              <option key={op.value} value={op.value}>{op.label}</option>
            ))}
          </select>

          {/* Value (hidden for empty/not_empty operators) */}
          {!['empty', 'not_empty'].includes(cond.operator) && (
            <input
              style={{ ...css.input, width: '25%', fontSize: 11, padding: '4px 6px' }}
              value={cond.value || ''}
              onChange={(e) => updateCondition(i, 'value', e.target.value)}
              placeholder="Value"
            />
          )}

          {/* Remove button */}
          <button
            onClick={() => removeCondition(i)}
            style={{ ...css.iconBtn, width: 20, height: 20, color: T.status.danger, flexShrink: 0 }}
          >
            <X size={12} />
          </button>
        </div>
      ))}

      {/* Add condition button */}
      <button
        onClick={addCondition}
        style={{ ...css.btn('secondary'), width: '100%', fontSize: 11, padding: '4px 8px', marginTop: 4 }}
      >
        <Filter size={11} /> Add Condition
      </button>
    </div>
  );
}

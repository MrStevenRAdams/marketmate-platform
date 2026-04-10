// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// REPEATER CHILD PREVIEW
// When a Repeater block is in preview mode, each child block is
// rendered once per data item. This component handles the per-item
// rendering with the correct merge tag context (line.* fields).
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

import React from 'react';
import { T, THEME_PRESETS } from '../../../constants/index.jsx';
import { resolveText, resolveMergeTag, evaluateConditions } from '../../../utils/index.jsx';

export default function RepeaterChildPreview({ block, data, theme }) {
  if (!block.visible) return null;

  // Evaluate conditions against per-item data
  const conds = Array.isArray(block.conditions) ? block.conditions.filter((c) => c?.field) : [];
  if (conds.length > 0 && !evaluateConditions(block.conditions, data)) return null;

  const baseStyle = { ...block.style };
  const tv = theme?.vars || THEME_PRESETS.default.vars;

  switch (block.type) {
    case 'text':
      return (
        <div style={{
          ...baseStyle,
          fontFamily: block.properties.fontFamily || T.font,
          fontSize: block.properties.fontSize,
          fontWeight: block.properties.fontWeight,
          color: block.properties.color,
          textAlign: block.properties.textAlign,
          lineHeight: block.properties.lineHeight,
          whiteSpace: 'pre-wrap',
        }}>
          {resolveText(block.properties.content, data)}
        </div>
      );

    case 'dynamic_field':
      return (
        <div style={{
          ...baseStyle,
          fontSize: block.properties.fontSize,
          fontWeight: block.properties.fontWeight,
          color: block.properties.color,
        }}>
          {block.properties.prefix}
          {resolveMergeTag(block.properties.fieldPath, data)}
          {block.properties.suffix}
        </div>
      );

    case 'divider':
      return (
        <div style={{ ...baseStyle }}>
          <hr style={{
            border: 'none',
            borderTop: `${block.properties.thickness} ${block.properties.lineStyle} ${block.properties.color}`,
            margin: 0,
          }} />
        </div>
      );

    case 'spacer':
      return <div style={{ height: block.properties.height || 16 }} />;

    default:
      return (
        <div style={{ ...baseStyle, fontSize: 12, color: tv.muted }}>
          [{block.type}]
        </div>
      );
  }
}

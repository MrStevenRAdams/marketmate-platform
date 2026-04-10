import React, { useCallback, useEffect, useRef, useState } from 'react';
import Editor, { Monaco, OnMount } from '@monaco-editor/react';
import type * as MonacoType from 'monaco-editor';

// ============================================================================
// TYPES
// ============================================================================
interface ValidationError {
  line: number;
  column: number;
  message: string;
  severity: 'error' | 'warning';
}

interface FieldMeta {
  field: string;
  type: string;
  description: string;
}

interface ActionMeta {
  name: string;
  signature: string;
  description: string;
}

interface RuleEditorProps {
  value: string;
  onChange: (value: string) => void;
  tenantId: string;
  onValidationChange?: (result: { valid: boolean; errors: ValidationError[] }) => void;
}

// ─── Visual Mode Types ────────────────────────────────────────────────────────

type NodeKind = 'trigger' | 'condition' | 'action';

interface VisualNode {
  id: string;
  kind: NodeKind;
  label: string;
  x: number;
  y: number;
  width: number;
  height: number;
  raw?: string;
  fnName?: string;
  fnArgs?: string;
}

interface VisualEdge {
  from: string;
  to: string;
}

interface VisualGraph {
  nodes: VisualNode[];
  edges: VisualEdge[];
}

// ============================================================================
// CONSTANTS
// ============================================================================
const LANG_ID = 'marketmate-rules';
const API_BASE = '/api/v1';

const KEYWORDS = ['WHEN', 'THEN', 'AND', 'OR', 'NOT', 'IN', 'MATCHES', 'IF'];

const FIELD_NAMES = [
  'order.channel', 'order.total_gbp', 'order.weight_grams', 'order.item_count',
  'order.shipping_country', 'order.shipping_postcode', 'order.shipping_city',
  'order.status', 'order.payment_method', 'order.payment_status', 'order.tags',
  'order.customer_email', 'order.has_tag', 'order.sku_in_order',
  'line.sku', 'line.quantity', 'line.title',
];

const ACTION_NAMES = [
  'select_carrier', 'select_service', 'require_signature', 'add_tag', 'remove_tag',
  'set_status', 'notify', 'webhook', 'set_fulfilment_source', 'hold_order',
  'flag_for_review', 'set_shipping_method', 'skip_remaining_rules',
];

const NODE_W = 220;
const NODE_H = 48;
const ROW_GAP = 72;
const COL_GAP = 320;
const CANVAS_PAD = 40;

const NODE_COLORS: Record<NodeKind, { fill: string; stroke: string; text: string; icon: string }> = {
  trigger:   { fill: '#0b1020', stroke: '#3b82f6', text: '#60a5fa', icon: '⚡' },
  condition: { fill: '#0b1020', stroke: '#38bdf8', text: '#7dd3fc', icon: '◈' },
  action:    { fill: '#0b1020', stroke: '#a78bfa', text: '#c4b5fd', icon: '▶' },
};

// ============================================================================
// SCRIPT ↔ GRAPH
// ============================================================================

function parseScriptToGraph(script: string): VisualGraph {
  const lines = script
    .split('\n')
    .map(l => l.replace(/#.*$/, '').trim())
    .filter(Boolean);

  const conditions: string[] = [];
  const actions: string[] = [];
  let inThen = false;

  for (const line of lines) {
    const upper = line.toUpperCase();
    if (upper.startsWith('WHEN ')) {
      inThen = false;
      conditions.push(line.slice(5).trim());
    } else if (upper === 'THEN' || upper.startsWith('THEN ')) {
      inThen = true;
      const rest = line.slice(4).trim();
      if (rest) actions.push(rest);
    } else if (!inThen && (upper.startsWith('AND ') || upper.startsWith('OR '))) {
      const spaceIdx = line.indexOf(' ');
      conditions.push(line.slice(spaceIdx + 1).trim());
    } else if (inThen) {
      actions.push(line);
    }
  }

  const nodes: VisualNode[] = [];
  const edges: VisualEdge[] = [];
  const triggerId = 'trigger-0';

  nodes.push({
    id: triggerId, kind: 'trigger', label: 'Order imported',
    x: CANVAS_PAD, y: CANVAS_PAD, width: NODE_W, height: NODE_H,
  });

  const condStartY = CANVAS_PAD + NODE_H + ROW_GAP;
  const condIds: string[] = [];
  conditions.forEach((raw, i) => {
    const id = `cond-${i}`;
    condIds.push(id);
    nodes.push({
      id, kind: 'condition',
      label: raw.length > 30 ? raw.slice(0, 28) + '…' : raw,
      raw, x: CANVAS_PAD, y: condStartY + i * (NODE_H + ROW_GAP), width: NODE_W, height: NODE_H,
    });
  });

  const actionColX = CANVAS_PAD + NODE_W + COL_GAP;
  const actionStartY = CANVAS_PAD + NODE_H + ROW_GAP;
  const actionIds: string[] = [];
  actions.forEach((raw, i) => {
    const id = `action-${i}`;
    actionIds.push(id);
    const match = raw.match(/^(\w+)\((.*)\)$/s);
    nodes.push({
      id, kind: 'action',
      label: raw.length > 30 ? raw.slice(0, 28) + '…' : raw,
      raw, fnName: match ? match[1] : raw, fnArgs: match ? match[2] : '',
      x: actionColX, y: actionStartY + i * (NODE_H + ROW_GAP), width: NODE_W, height: NODE_H,
    });
  });

  if (condIds.length > 0) {
    edges.push({ from: triggerId, to: condIds[0] });
    for (let i = 1; i < condIds.length; i++) edges.push({ from: condIds[i - 1], to: condIds[i] });
    if (actionIds.length > 0) edges.push({ from: condIds[condIds.length - 1], to: actionIds[0] });
  } else if (actionIds.length > 0) {
    edges.push({ from: triggerId, to: actionIds[0] });
  }
  for (let i = 1; i < actionIds.length; i++) edges.push({ from: actionIds[i - 1], to: actionIds[i] });

  return { nodes, edges };
}

function graphToScript(graph: VisualGraph): string {
  const conditions = graph.nodes.filter(n => n.kind === 'condition').sort((a, b) => a.y - b.y);
  const actions = graph.nodes.filter(n => n.kind === 'action').sort((a, b) => a.y - b.y);

  if (conditions.length === 0 && actions.length === 0) {
    return '# Empty rule\nWHEN order.channel == ""\nTHEN\n  add_tag("")';
  }

  const lines: string[] = [];
  if (conditions.length > 0) {
    lines.push(`WHEN ${conditions[0].raw || conditions[0].label}`);
    for (let i = 1; i < conditions.length; i++) {
      lines.push(`  AND ${conditions[i].raw || conditions[i].label}`);
    }
  } else {
    lines.push('WHEN order.channel == "all"');
  }
  lines.push('THEN');
  for (const a of actions) {
    lines.push(`  ${a.fnName || a.raw || a.label}(${a.fnArgs ?? ''})`);
  }
  return lines.join('\n');
}

// ============================================================================
// LANGUAGE REGISTRATION
// ============================================================================
const registeredForTenant: Record<string, boolean> = {};

function registerLanguage(monaco: Monaco, fieldsMeta: FieldMeta[], actionsMeta: ActionMeta[], tenantId: string) {
  if (registeredForTenant[tenantId]) return;
  registeredForTenant[tenantId] = true;

  if (!monaco.languages.getLanguages().some((l) => l.id === LANG_ID)) {
    monaco.languages.register({ id: LANG_ID });
  }

  monaco.languages.setMonarchTokensProvider(LANG_ID, {
    keywords: KEYWORDS,
    tokenizer: {
      root: [
        [/#.*$/, 'comment'],
        [/\b(WHEN|THEN|AND|OR|NOT|IN|MATCHES|IF)\b/, 'keyword'],
        [/\b(order|line)\.[a-zA-Z_]+/, 'type.identifier'],
        [new RegExp(`\\b(${ACTION_NAMES.join('|')})\\b`), 'support.function'],
        [/"([^"\\]|\\.)*"/, 'string'],
        [/\b\d+(\.\d+)?\b/, 'number'],
        [/==|!=|>=|<=|>|</, 'operator'],
        [/[\[\]()]/, 'delimiter.bracket'],
        [/,/, 'delimiter'],
        [/\s+/, 'white'],
      ],
    },
  });

  monaco.editor.defineTheme('marketmate-dark', {
    base: 'vs-dark', inherit: true,
    rules: [
      { token: 'keyword',           foreground: '7C3AED', fontStyle: 'bold' },
      { token: 'type.identifier',   foreground: '38BDF8' },
      { token: 'support.function',  foreground: 'A78BFA' },
      { token: 'string',            foreground: '34D399' },
      { token: 'number',            foreground: 'FB923C' },
      { token: 'operator',          foreground: 'F472B6' },
      { token: 'comment',           foreground: '6B7280', fontStyle: 'italic' },
      { token: 'delimiter',         foreground: '9CA3AF' },
    ],
    colors: {
      'editor.background':                 '#0f1117',
      'editor.foreground':                 '#e2e8f0',
      'editor.lineHighlightBackground':    '#1a1f2e',
      'editor.selectionBackground':        '#3730a380',
      'editorLineNumber.foreground':       '#4B5563',
      'editorLineNumber.activeForeground': '#9CA3AF',
      'editorCursor.foreground':           '#7C3AED',
      'editor.inactiveSelectionBackground':'#3730a340',
    },
  });

  monaco.languages.registerCompletionItemProvider(LANG_ID, {
    triggerCharacters: ['.', '(', '"'],
    provideCompletionItems(model, position) {
      const word = model.getWordUntilPosition(position);
      const range = {
        startLineNumber: position.lineNumber, endLineNumber: position.lineNumber,
        startColumn: word.startColumn, endColumn: word.endColumn,
      };
      const lineContent = model.getLineContent(position.lineNumber);
      const textBefore = lineContent.substring(0, position.column - 1);
      const suggestions: MonacoType.languages.CompletionItem[] = [];

      if (/order\.$/.test(textBefore)) {
        fieldsMeta.filter(f => f.field.startsWith('order.')).forEach(f => {
          suggestions.push({ label: f.field.replace('order.', ''), kind: monaco.languages.CompletionItemKind.Field, insertText: f.field.replace('order.', ''), detail: f.type, documentation: f.description, range });
        });
        return { suggestions };
      }
      if (/line\.$/.test(textBefore)) {
        fieldsMeta.filter(f => f.field.startsWith('line.')).forEach(f => {
          suggestions.push({ label: f.field.replace('line.', ''), kind: monaco.languages.CompletionItemKind.Field, insertText: f.field.replace('line.', ''), detail: f.type, documentation: f.description, range });
        });
        return { suggestions };
      }

      suggestions.push({ label: 'WHEN...THEN', kind: monaco.languages.CompletionItemKind.Snippet, insertText: 'WHEN ${1:order.channel} == "${2:amazon}"\nTHEN\n  ${3:add_tag("${4:tag}")}', insertTextRules: monaco.languages.CompletionItemInsertTextRule.InsertAsSnippet, documentation: 'Add a new rule block', range });
      KEYWORDS.forEach(kw => suggestions.push({ label: kw, kind: monaco.languages.CompletionItemKind.Keyword, insertText: kw, range }));
      fieldsMeta.forEach(f => suggestions.push({ label: f.field, kind: monaco.languages.CompletionItemKind.Field, insertText: f.field, detail: f.type, documentation: f.description, range }));
      actionsMeta.forEach(a => {
        const paramCount = (a.signature.match(/:/g) || []).length;
        let snippetParams = '';
        if (paramCount === 1) snippetParams = '"${1:value}"';
        if (paramCount === 2) snippetParams = '"${1:value1}", "${2:value2}"';
        suggestions.push({ label: a.name, kind: monaco.languages.CompletionItemKind.Function, insertText: `${a.name}(${snippetParams})`, insertTextRules: monaco.languages.CompletionItemInsertTextRule.InsertAsSnippet, detail: a.signature, documentation: a.description, range });
      });
      return { suggestions };
    },
  });

  monaco.languages.registerHoverProvider(LANG_ID, {
    provideHover(model, position) {
      const word = model.getWordAtPosition(position);
      if (!word) return null;
      const lineContent = model.getLineContent(position.lineNumber);
      const hoverWord = lineContent.substring(0, position.column + word.word.length).match(/((order|line)\.[a-zA-Z_]+)$|([a-z_]+)$/)?.[0];
      if (!hoverWord) return null;
      const field = fieldsMeta.find(f => f.field === hoverWord || f.field === `order.${hoverWord}` || f.field === `line.${hoverWord}`);
      if (field) return { contents: [{ value: `**${field.field}** \`${field.type}\`` }, { value: field.description }] };
      const action = actionsMeta.find(a => a.name === hoverWord);
      if (action) return { contents: [{ value: `**${action.signature}**` }, { value: action.description }] };
      return null;
    },
  });
}

// ============================================================================
// VISUAL CANVAS
// ============================================================================

interface EditingState { id: string; field: string; value: string; }

function VisualCanvas({ graph, onGraphChange }: { graph: VisualGraph; onGraphChange: (g: VisualGraph) => void }) {
  const svgRef = useRef<SVGSVGElement>(null);
  const [dragging, setDragging] = useState<{ id: string; ox: number; oy: number } | null>(null);
  const [editing, setEditing] = useState<EditingState | null>(null);
  const [hovered, setHovered] = useState<string | null>(null);

  const maxX = Math.max(...graph.nodes.map(n => n.x + n.width), 560);
  const maxY = Math.max(...graph.nodes.map(n => n.y + n.height), 320);
  const svgW = maxX + CANVAS_PAD * 2;
  const svgH = maxY + CANVAS_PAD * 2;

  const getSvgPoint = (e: React.MouseEvent) => {
    const svg = svgRef.current;
    if (!svg) return null;
    const pt = svg.createSVGPoint();
    pt.x = e.clientX; pt.y = e.clientY;
    return pt.matrixTransform(svg.getScreenCTM()!.inverse());
  };

  const handleMouseDown = (e: React.MouseEvent, id: string) => {
    if (editing) return;
    e.preventDefault();
    const pt = getSvgPoint(e);
    if (!pt) return;
    const node = graph.nodes.find(n => n.id === id);
    if (!node) return;
    setDragging({ id, ox: pt.x - node.x, oy: pt.y - node.y });
  };

  const handleMouseMove = (e: React.MouseEvent) => {
    if (!dragging) return;
    const pt = getSvgPoint(e);
    if (!pt) return;
    onGraphChange({
      ...graph,
      nodes: graph.nodes.map(n => n.id === dragging.id
        ? { ...n, x: Math.max(CANVAS_PAD, pt.x - dragging.ox), y: Math.max(CANVAS_PAD, pt.y - dragging.oy) }
        : n),
    });
  };

  const commitEdit = () => {
    if (!editing) return;
    const node = graph.nodes.find(n => n.id === editing.id);
    if (!node) { setEditing(null); return; }
    let updated: VisualNode;
    if (node.kind === 'action') {
      updated = { ...node, fnArgs: editing.value, raw: `${node.fnName}(${editing.value})`, label: `${node.fnName}(${editing.value})`.slice(0, 28) };
    } else {
      updated = { ...node, raw: editing.value, label: editing.value.length > 30 ? editing.value.slice(0, 28) + '…' : editing.value };
    }
    onGraphChange({ ...graph, nodes: graph.nodes.map(n => n.id === node.id ? updated : n) });
    setEditing(null);
  };

  const addNode = (kind: 'condition' | 'action') => {
    const existing = graph.nodes.filter(n => n.kind === kind);
    const maxY = existing.length ? Math.max(...existing.map(n => n.y)) : CANVAS_PAD + NODE_H;
    const colX = kind === 'condition' ? CANVAS_PAD : CANVAS_PAD + NODE_W + COL_GAP;
    const id = `${kind}-${Date.now()}`;
    const newNode: VisualNode = {
      id, kind, label: kind === 'condition' ? 'order.channel == ""' : 'add_tag("")',
      raw: kind === 'condition' ? 'order.channel == ""' : 'add_tag("")',
      fnName: kind === 'action' ? 'add_tag' : undefined,
      fnArgs: kind === 'action' ? '""' : undefined,
      x: colX, y: maxY + NODE_H + ROW_GAP, width: NODE_W, height: NODE_H,
    };
    const newEdges = [...graph.edges];
    const lastOfKind = existing.sort((a, b) => b.y - a.y)[0];
    if (lastOfKind) {
      const nextEdge = newEdges.find(e => e.from === lastOfKind.id);
      if (nextEdge) {
        const nxt = nextEdge.to;
        newEdges.splice(newEdges.indexOf(nextEdge), 1);
        newEdges.push({ from: lastOfKind.id, to: id });
        newEdges.push({ from: id, to: nxt });
      } else {
        newEdges.push({ from: lastOfKind.id, to: id });
      }
    } else {
      const triggerEdge = newEdges.find(e => e.from === 'trigger-0');
      if (triggerEdge) {
        const nxt = triggerEdge.to;
        newEdges.splice(newEdges.indexOf(triggerEdge), 1);
        newEdges.push({ from: 'trigger-0', to: id });
        newEdges.push({ from: id, to: nxt });
      } else {
        newEdges.push({ from: 'trigger-0', to: id });
      }
    }
    onGraphChange({ nodes: [...graph.nodes, newNode], edges: newEdges });
  };

  const removeNode = (id: string) => {
    const incoming = graph.edges.find(e => e.to === id);
    const outgoing = graph.edges.find(e => e.from === id);
    const newEdges = graph.edges.filter(e => e.from !== id && e.to !== id);
    if (incoming && outgoing) newEdges.push({ from: incoming.from, to: outgoing.to });
    onGraphChange({ nodes: graph.nodes.filter(n => n.id !== id), edges: newEdges });
  };

  const nodeById = (id: string) => graph.nodes.find(n => n.id === id);

  const edgePath = (from: VisualNode, to: VisualNode) => {
    const x1 = from.x + from.width / 2;
    const y1 = from.y + from.height;
    const x2 = to.x + to.width / 2;
    const y2 = to.y;
    if (Math.abs(x2 - x1) > 80) {
      // nodes in different columns — horizontal S-curve from right side to left side
      const ox1 = from.x + from.width;
      const oy1 = from.y + from.height / 2;
      const ox2 = to.x;
      const oy2 = to.y + to.height / 2;
      const mx = (ox1 + ox2) / 2;
      return `M ${ox1} ${oy1} C ${mx} ${oy1}, ${mx} ${oy2}, ${ox2} ${oy2}`;
    }
    // same column — vertical bezier
    const cy = (y1 + y2) / 2;
    return `M ${x1} ${y1} C ${x1} ${cy}, ${x2} ${cy}, ${x2} ${y2}`;
  };

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', background: '#070b18', overflow: 'auto' }}>
      {/* Toolbar */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '8px 14px', background: 'rgba(7,11,24,0.96)', borderBottom: '1px solid #1e2442', flexShrink: 0, backdropFilter: 'blur(4px)', position: 'sticky', top: 0, zIndex: 10 }}>
        <button onClick={() => addNode('condition')} style={vcBtnStyle('#38bdf8')}>+ Condition</button>
        <button onClick={() => addNode('action')} style={vcBtnStyle('#a78bfa')}>+ Action</button>
        <span style={{ fontSize: 11, color: '#475569', marginLeft: 6 }}>Drag to move · Double-click to edit · × to remove</span>
      </div>

      <svg
        ref={svgRef} width={svgW} height={svgH}
        onMouseMove={handleMouseMove}
        onMouseUp={() => setDragging(null)}
        onMouseLeave={() => setDragging(null)}
        style={{ display: 'block', cursor: dragging ? 'grabbing' : 'default' }}
      >
        <defs>
          <marker id="arr" markerWidth="8" markerHeight="6" refX="8" refY="3" orient="auto">
            <polygon points="0 0, 8 3, 0 6" fill="#3d4c6a" />
          </marker>
          <filter id="nshadow" x="-20%" y="-20%" width="140%" height="140%">
            <feDropShadow dx="0" dy="2" stdDeviation="3" floodColor="#000" floodOpacity="0.6" />
          </filter>
        </defs>
        {/* Grid */}
        <pattern id="dots" width="28" height="28" patternUnits="userSpaceOnUse">
          <circle cx="14" cy="14" r="0.75" fill="#141928" />
        </pattern>
        <rect width="100%" height="100%" fill="url(#dots)" />

        {/* Column labels */}
        <text x={CANVAS_PAD + NODE_W / 2} y={22} textAnchor="middle" fontSize={10} fill="#2d3a52" fontWeight={600} letterSpacing="0.1em">CONDITIONS</text>
        <text x={CANVAS_PAD + NODE_W + COL_GAP + NODE_W / 2} y={22} textAnchor="middle" fontSize={10} fill="#2d3a52" fontWeight={600} letterSpacing="0.1em">ACTIONS</text>

        {/* Edges */}
        {graph.edges.map((edge, i) => {
          const f = nodeById(edge.from);
          const t = nodeById(edge.to);
          if (!f || !t) return null;
          return <path key={i} d={edgePath(f, t)} fill="none" stroke="#2d3a52" strokeWidth="1.5" strokeDasharray="6 4" markerEnd="url(#arr)" />;
        })}

        {/* Nodes */}
        {graph.nodes.map(node => {
          const c = NODE_COLORS[node.kind];
          const isH = hovered === node.id;
          const isE = editing?.id === node.id;
          return (
            <g key={node.id}
              transform={`translate(${node.x},${node.y})`}
              style={{ cursor: node.kind !== 'trigger' ? 'grab' : 'default', userSelect: 'none' }}
              onMouseDown={e => node.kind !== 'trigger' && handleMouseDown(e, node.id)}
              onMouseEnter={() => setHovered(node.id)}
              onMouseLeave={() => setHovered(null)}
              onDoubleClick={e => {
                if (node.kind === 'trigger') return;
                e.preventDefault();
                const val = node.kind === 'action' ? (node.fnArgs ?? '') : (node.raw ?? node.label);
                setEditing({ id: node.id, field: node.kind === 'action' ? 'fnArgs' : 'raw', value: val });
              }}
              filter="url(#nshadow)"
            >
              {/* Shadow background */}
              <rect width={node.width} height={node.height} rx={8} fill="#000" opacity={0.3} transform="translate(1,2)" />
              {/* Body */}
              <rect width={node.width} height={node.height} rx={8} fill={c.fill}
                stroke={isE ? '#f59e0b' : isH ? c.text : c.stroke}
                strokeWidth={isH || isE ? 2 : 1.5} />
              {/* Left accent */}
              <rect width={3} height={node.height} rx={8} fill={c.stroke} opacity={0.9} />
              {/* Icon */}
              <text x={13} y={node.height / 2 + 5} fontSize={13} fill={c.text}>{c.icon}</text>
              {/* Label text */}
              {isE ? (
                <foreignObject x={28} y={7} width={node.width - 44} height={node.height - 14}>
                  <input
                    // @ts-ignore - xmlns not typed but needed for SVG foreignObject
                    xmlns="http://www.w3.org/1999/xhtml"
                    autoFocus
                    value={editing!.value}
                    onChange={ev => setEditing(s => s ? { ...s, value: ev.target.value } : null)}
                    onBlur={commitEdit}
                    onKeyDown={ev => { if (ev.key === 'Enter') commitEdit(); if (ev.key === 'Escape') setEditing(null); }}
                    style={{ width: '100%', height: '100%', background: '#111827', border: '1px solid #f59e0b', borderRadius: 3, color: '#e2e8f0', fontSize: 11, padding: '2px 5px', outline: 'none', fontFamily: '"JetBrains Mono",monospace' }}
                  />
                </foreignObject>
              ) : (
                <text x={28} y={node.height / 2} dominantBaseline="middle" fontSize={11}
                  fontFamily='"JetBrains Mono","Fira Code",monospace' fill={c.text}>
                  {node.label}
                </text>
              )}
              {/* Kind badge */}
              <text x={node.width - 7} y={10} textAnchor="end" fontSize={8} fill={c.stroke} fontWeight={700} letterSpacing="0.1em">
                {node.kind.toUpperCase()}
              </text>
              {/* Remove ✕ */}
              {isH && node.kind !== 'trigger' && (
                <g transform={`translate(${node.width - 10},-10)`} style={{ cursor: 'pointer' }}
                  onMouseDown={ev => { ev.stopPropagation(); removeNode(node.id); }}>
                  <circle r={8} fill="#0f1929" stroke="#ef4444" strokeWidth={1.5} />
                  <text x={0} y={4} textAnchor="middle" fontSize={11} fill="#ef4444">×</text>
                </g>
              )}
            </g>
          );
        })}
      </svg>
    </div>
  );
}

function vcBtnStyle(color: string): React.CSSProperties {
  return {
    padding: '3px 11px', fontSize: 11, fontWeight: 600,
    background: `${color}15`, border: `1px solid ${color}35`,
    borderRadius: 4, color, cursor: 'pointer', transition: 'all 0.15s',
  };
}

// ============================================================================
// MAIN COMPONENT
// ============================================================================
export default function RuleEditor({ value, onChange, tenantId, onValidationChange }: RuleEditorProps) {
  const monacoRef        = useRef<Monaco | null>(null);
  const editorRef        = useRef<MonacoType.editor.IStandaloneCodeEditor | null>(null);
  const validateTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const fieldsMetaRef    = useRef<FieldMeta[]>([]);
  const actionsMetaRef   = useRef<ActionMeta[]>([]);

  const [viewMode, setViewMode] = useState<'code' | 'visual'>('code');
  const [graph, setGraph]       = useState<VisualGraph>(() => parseScriptToGraph(value));

  // Sync graph when value changes externally (e.g. selecting a different rule)
  const prevValueRef = useRef(value);
  useEffect(() => {
    if (value !== prevValueRef.current) {
      prevValueRef.current = value;
      if (viewMode === 'visual') setGraph(parseScriptToGraph(value));
    }
  }, [value, viewMode]);

  const switchToVisual = useCallback(() => {
    setGraph(parseScriptToGraph(value));
    setViewMode('visual');
  }, [value]);

  const handleGraphChange = useCallback((g: VisualGraph) => {
    setGraph(g);
    const newScript = graphToScript(g);
    if (newScript !== value) onChange(newScript);
  }, [value, onChange]);

  // Fetch tenant metadata
  useEffect(() => {
    delete registeredForTenant[tenantId];
    const headers = { 'Content-Type': 'application/json', 'X-Tenant-Id': tenantId };
    Promise.all([
      fetch(`${API_BASE}/automation/fields`,  { headers }).then(r => r.json()),
      fetch(`${API_BASE}/automation/actions`, { headers }).then(r => r.json()),
    ]).then(([fieldsRes, actionsRes]) => {
      fieldsMetaRef.current  = fieldsRes.fields  || [];
      actionsMetaRef.current = actionsRes.actions || [];
      if (monacoRef.current) {
        delete registeredForTenant[tenantId];
        registerLanguage(monacoRef.current, fieldsMetaRef.current, actionsMetaRef.current, tenantId);
      }
    }).catch(() => {});
  }, [tenantId]);

  // Backend validation
  const validateScript = useCallback(
    (script: string, monaco: Monaco, editor: MonacoType.editor.IStandaloneCodeEditor) => {
      if (validateTimerRef.current) clearTimeout(validateTimerRef.current);
      validateTimerRef.current = setTimeout(async () => {
        try {
          const resp = await fetch(`${API_BASE}/automation/rules/validate`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'X-Tenant-Id': tenantId },
            body: JSON.stringify({ script }),
          });
          if (!resp.ok) return;
          const result = await resp.json();
          const model  = editor.getModel();
          if (!model) return;
          const markers: MonacoType.editor.IMarkerData[] = [
            ...(result.errors   || []).map((e: ValidationError) => ({ severity: monaco.MarkerSeverity.Error,   startLineNumber: e.line, startColumn: e.column, endLineNumber: e.line, endColumn: e.column + 10, message: e.message })),
            ...(result.warnings || []).map((e: ValidationError) => ({ severity: monaco.MarkerSeverity.Warning, startLineNumber: e.line, startColumn: e.column, endLineNumber: e.line, endColumn: e.column + 10, message: e.message })),
          ];
          monaco.editor.setModelMarkers(model, LANG_ID, markers);
          onValidationChange?.({ valid: result.valid, errors: result.errors || [] });
        } catch {}
      }, 500);
    },
    [tenantId, onValidationChange],
  );

  const handleEditorMount: OnMount = useCallback(
    (editor, monaco) => {
      monacoRef.current = monaco;
      editorRef.current = editor;
      registerLanguage(monaco, fieldsMetaRef.current, actionsMetaRef.current, tenantId);
      monaco.editor.setTheme('marketmate-dark');
      editor.onDidChangeModelContent(() => {
        const val = editor.getValue();
        onChange(val);
        validateScript(val, monaco, editor);
      });
      if (value) validateScript(value, monaco, editor);
    },
    [onChange, validateScript, value, tenantId],
  );

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', minHeight: 0 }}>
      {/* Mode toggle */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 4, padding: '5px 12px', background: '#0c1020', borderBottom: '1px solid #1e2442', flexShrink: 0 }}>
        <span style={{ fontSize: 10, color: '#475569', fontWeight: 700, letterSpacing: '0.08em', textTransform: 'uppercase', marginRight: 4 }}>View</span>
        <button onClick={() => setViewMode('code')} style={modeTab(viewMode === 'code')} title="Code editor">
          <span style={{ marginRight: 4 }}>⌨</span>Code
        </button>
        <button onClick={switchToVisual} style={modeTab(viewMode === 'visual')} title="Visual flowchart">
          <span style={{ marginRight: 4 }}>⬡</span>Visual
        </button>
        {viewMode === 'visual' && (
          <span style={{ fontSize: 10, color: '#475569', marginLeft: 8 }}>
            Both views stay in sync — edits in one are reflected in the other
          </span>
        )}
      </div>

      {/* Content */}
      <div style={{ flex: 1, minHeight: 0, overflow: 'hidden' }}>
        {viewMode === 'code' ? (
          <Editor
            height="100%"
            language={LANG_ID}
            value={value}
            theme="marketmate-dark"
            onMount={handleEditorMount}
            options={{
              fontSize: 13, fontFamily: '"JetBrains Mono", "Fira Code", "Cascadia Code", monospace',
              fontLigatures: true, minimap: { enabled: false }, scrollBeyondLastLine: false,
              lineNumbers: 'on', renderLineHighlight: 'line', padding: { top: 16, bottom: 16 },
              wordWrap: 'on', automaticLayout: true, tabSize: 2, insertSpaces: true,
              folding: false, glyphMargin: true,
              scrollbar: { vertical: 'auto', horizontal: 'auto', verticalScrollbarSize: 6, horizontalScrollbarSize: 6 },
            }}
          />
        ) : (
          <VisualCanvas graph={graph} onGraphChange={handleGraphChange} />
        )}
      </div>
    </div>
  );
}

function modeTab(active: boolean): React.CSSProperties {
  return {
    display: 'inline-flex', alignItems: 'center',
    padding: '3px 10px', fontSize: 11, fontWeight: active ? 600 : 400,
    background: active ? 'rgba(59,130,246,0.12)' : 'transparent',
    border: `1px solid ${active ? 'rgba(59,130,246,0.35)' : 'transparent'}`,
    borderRadius: 4, color: active ? '#60a5fa' : '#475569',
    cursor: 'pointer', transition: 'all 0.15s',
  };
}

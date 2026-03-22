// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// RULERS & GRID OVERLAY
// Renders horizontal (top) and vertical (left) rulers alongside
// the canvas, plus an optional snap-to-grid overlay on the canvas.
//
// Features:
//   • Unit-aware tick marks (px, mm, in) with automatic subdivision
//   • Zoom-aware — ticks scale with the canvas zoom level
//   • Current cursor position highlighted on both rulers
//   • Canvas origin marker at the ruler corner
//   • Optional grid overlay with configurable spacing and snapping
//   • Guide lines (drag from ruler to create, click guide to remove)
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

import React, { useState, useCallback, useRef, useEffect, useMemo } from 'react';
import { T } from '../../../constants/index.jsx';
import { unitToPx } from '../../../utils/index.jsx';

// ── Ruler sizing constants ──────────────────────────────────────
const RULER_THICKNESS = 24;         // Width/height of the ruler bar
const CORNER_SIZE = RULER_THICKNESS; // Square at the ruler intersection

// ── Tick-mark configuration per unit ─────────────────────────────
// Each unit has a major tick interval, sub-divisions, and label format.
const TICK_CONFIG = {
  px: { major: 100, sub: 5, minor: 10,  fmt: (v) => v },
  mm: { major: 10,  sub: 5, minor: 1,   fmt: (v) => v },
  in: { major: 1,   sub: 4, minor: 0.25, fmt: (v) => v },
};

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// RULER COMPONENT
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

export default function Rulers({
  canvas,             // { width, height, unit, ... }
  zoom,               // Current zoom level (0.25–2)
  canvasWidthPx,      // Resolved canvas width in CSS pixels
  canvasHeightPx,     // Resolved canvas height in CSS pixels (or 'auto')
  scrollContainer,    // Ref to the scrolling container element
  canvasOffset,       // { left, top } pixel offset of the canvas within the scroll container
  showGrid,           // Whether to render the grid overlay
  gridSpacing,        // Grid spacing in canvas units (e.g. 10mm, 0.25in, 50px)
  guides,             // Array of { axis: 'x'|'y', position: number (in canvas units) }
  onAddGuide,         // (axis, positionInUnits) => void
  onRemoveGuide,      // (index) => void
  draggingGuide,      // { axis, position } — lifted state from parent
  setDraggingGuide,   // setState for draggingGuide — lifted from parent
}) {
  const [mousePos, setMousePos] = useState(null); // { x, y } in canvas units
  const [scroll, setScroll] = useState({ left: 0, top: 0 });
  const hRulerRef = useRef(null);
  const vRulerRef = useRef(null);

  const unit = canvas.unit || 'px';
  const config = TICK_CONFIG[unit] || TICK_CONFIG.px;

  // ── Track scroll position ──────────────────────────────────────
  useEffect(() => {
    const el = scrollContainer?.current;
    if (!el) return;
    const onScroll = () => setScroll({ left: el.scrollLeft, top: el.scrollTop });
    el.addEventListener('scroll', onScroll, { passive: true });
    onScroll();
    return () => el.removeEventListener('scroll', onScroll);
  }, [scrollContainer]);

  // ── Track mouse position on canvas ─────────────────────────────
  const handleMouseMove = useCallback((e) => {
    const el = scrollContainer?.current;
    if (!el) return;
    const rect = el.getBoundingClientRect();
    // Mouse position relative to the scroll container viewport
    const relX = e.clientX - rect.left + el.scrollLeft;
    const relY = e.clientY - rect.top + el.scrollTop;
    // Convert to canvas-relative position (accounting for offset and zoom)
    const canvasX = (relX - (canvasOffset?.left || 40)) / zoom;
    const canvasY = (relY - (canvasOffset?.top || 40)) / zoom;
    // Convert px to canvas units
    const pxToUnit = (px) => {
      if (unit === 'mm') return px / 3.7795;
      if (unit === 'in') return px / 96;
      return px;
    };
    setMousePos({ x: pxToUnit(canvasX), y: pxToUnit(canvasY) });
  }, [scrollContainer, canvasOffset, zoom, unit]);

  useEffect(() => {
    const el = scrollContainer?.current;
    if (!el) return;
    el.addEventListener('mousemove', handleMouseMove, { passive: true });
    el.addEventListener('mouseleave', () => setMousePos(null));
    return () => {
      el.removeEventListener('mousemove', handleMouseMove);
      el.removeEventListener('mouseleave', () => setMousePos(null));
    };
  }, [scrollContainer, handleMouseMove]);

  // ── Compute ticks ──────────────────────────────────────────────
  // Returns an array of { position (in units), isMajor, isMid, label? }
  const computeTicks = useCallback((maxUnits) => {
    const ticks = [];
    const { major, sub, minor } = config;
    // Determine adaptive major interval based on zoom
    const majorPx = unitToPx(major, unit) * zoom;
    let adaptiveMajor = major;
    if (majorPx < 40) adaptiveMajor = major * 2;
    if (majorPx < 20) adaptiveMajor = major * 5;
    if (majorPx > 200) adaptiveMajor = major / 2;

    const adaptiveMinor = adaptiveMajor / sub;

    for (let pos = 0; pos <= maxUnits; pos += adaptiveMinor) {
      const rounded = Math.round(pos * 1000) / 1000; // avoid float drift
      const isMajor = Math.abs(rounded % adaptiveMajor) < 0.001;
      const isMid = !isMajor && Math.abs(rounded % (adaptiveMajor / 2)) < 0.001;
      ticks.push({
        position: rounded,
        isMajor,
        isMid,
        label: isMajor ? config.fmt(rounded) : null,
      });
    }
    return ticks;
  }, [config, unit, zoom]);

  // Canvas dimensions in units
  const canvasW = canvas.width;
  const canvasH = canvas.height === 'auto' ? 400 : canvas.height;

  const hTicks = useMemo(() => computeTicks(canvasW * 1.2), [computeTicks, canvasW]);
  const vTicks = useMemo(() => computeTicks(canvasH * 1.2), [computeTicks, canvasH]);

  // ── Coordinate conversion: unit position to pixel position on ruler ─
  const unitToRulerPx = (unitVal) => unitToPx(unitVal, unit) * zoom;

  // ── Pixel to canvas unit conversion ─────────────────────────────
  const pxToCanvasUnit = useCallback((px) => {
    if (unit === 'mm') return px / 3.7795;
    if (unit === 'in') return px / 96;
    return px;
  }, [unit]);

  // ── Drag-from-ruler guide creation ──────────────────────────────
  // mousedown on ruler → start dragging → mousemove updates preview →
  // mouseup creates the guide if within canvas bounds.
  const handleRulerDragStart = useCallback((axis, e) => {
    e.preventDefault();
    const el = scrollContainer?.current;
    if (!el || !onAddGuide) return;

    const getPosition = (evt) => {
      const rect = el.getBoundingClientRect();
      if (axis === 'x') {
        const relX = evt.clientX - rect.left + el.scrollLeft;
        const canvasPx = (relX - (canvasOffset?.left || 40)) / zoom;
        return pxToCanvasUnit(canvasPx);
      } else {
        const relY = evt.clientY - rect.top + el.scrollTop;
        const canvasPy = (relY - (canvasOffset?.top || 40)) / zoom;
        return pxToCanvasUnit(canvasPy);
      }
    };

    const pos = getPosition(e);
    setDraggingGuide({ axis, position: Math.round(pos * 100) / 100 });

    const onMove = (evt) => {
      const p = getPosition(evt);
      setDraggingGuide({ axis, position: Math.round(p * 100) / 100 });
    };

    const onUp = (evt) => {
      const finalPos = getPosition(evt);
      const rounded = Math.round(finalPos * 100) / 100;
      const maxVal = axis === 'x' ? canvas.width : (canvas.height === 'auto' ? 9999 : canvas.height);
      if (rounded >= 0 && rounded <= maxVal) {
        onAddGuide(axis, rounded);
      }
      setDraggingGuide(null);
      window.removeEventListener('mousemove', onMove);
      window.removeEventListener('mouseup', onUp);
    };

    window.addEventListener('mousemove', onMove);
    window.addEventListener('mouseup', onUp);
  }, [scrollContainer, canvasOffset, zoom, pxToCanvasUnit, onAddGuide, canvas]);


  // ── Common styles ──────────────────────────────────────────────
  const rulerBg = T.bg.secondary;
  const rulerBorder = T.border.default;
  const tickColor = T.text.muted;
  const tickColorMajor = T.text.secondary;
  const labelColor = T.text.muted;
  const cursorColor = T.primary.base;
  const guideColor = '#ef4444';

  // ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  // HORIZONTAL RULER
  // ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  const renderHRuler = () => {
    const offsetLeft = (canvasOffset?.left || 40) - scroll.left;

    return (
      <div
        ref={hRulerRef}
        onMouseDown={(e) => handleRulerDragStart('x', e)}
        onDoubleClick={(e) => {
          // Double-click still works as a quick way to place a guide
          if (!onAddGuide) return;
          const rect = hRulerRef.current.getBoundingClientRect();
          const clickX = e.clientX - rect.left - CORNER_SIZE;
          const unitVal = (clickX - offsetLeft + CORNER_SIZE) / zoom;
          const pos = pxToCanvasUnit(unitVal);
          if (pos >= 0 && pos <= canvasW) onAddGuide('x', Math.round(pos * 100) / 100);
        }}
        style={{
          position: 'absolute',
          top: 0,
          left: CORNER_SIZE,
          right: 0,
          height: RULER_THICKNESS,
          backgroundColor: rulerBg,
          borderBottom: `1px solid ${rulerBorder}`,
          overflow: 'hidden',
          zIndex: 30,
          cursor: 'crosshair',
          userSelect: 'none',
        }}
      >
        <svg
          width="100%"
          height={RULER_THICKNESS}
          style={{ display: 'block' }}
        >
          {hTicks.map((tick, i) => {
            const x = offsetLeft + unitToRulerPx(tick.position);
            if (x < -10 || x > 4000) return null;
            const tickH = tick.isMajor ? 12 : tick.isMid ? 8 : 5;
            return (
              <g key={i}>
                <line
                  x1={x} y1={RULER_THICKNESS}
                  x2={x} y2={RULER_THICKNESS - tickH}
                  stroke={tick.isMajor ? tickColorMajor : tickColor}
                  strokeWidth={tick.isMajor ? 1 : 0.5}
                  opacity={tick.isMajor ? 0.9 : 0.5}
                />
                {tick.label !== null && (
                  <text
                    x={x + 3}
                    y={10}
                    fill={labelColor}
                    fontSize={9}
                    fontFamily={T.font}
                  >
                    {tick.label}
                  </text>
                )}
              </g>
            );
          })}

          {/* Cursor highlight */}
          {mousePos && mousePos.x >= 0 && mousePos.x <= canvasW && (
            <>
              <line
                x1={offsetLeft + unitToRulerPx(mousePos.x)}
                y1={0}
                x2={offsetLeft + unitToRulerPx(mousePos.x)}
                y2={RULER_THICKNESS}
                stroke={cursorColor}
                strokeWidth={1}
                opacity={0.8}
              />
              <rect
                x={offsetLeft + unitToRulerPx(mousePos.x) - 16}
                y={1}
                width={32}
                height={13}
                rx={2}
                fill={T.primary.base}
                opacity={0.9}
              />
              <text
                x={offsetLeft + unitToRulerPx(mousePos.x)}
                y={10}
                fill="#fff"
                fontSize={8}
                fontFamily={T.font}
                fontWeight={600}
                textAnchor="middle"
              >
                {mousePos.x.toFixed(unit === 'in' ? 2 : unit === 'mm' ? 1 : 0)}
              </text>
            </>
          )}

          {/* Guide markers on horizontal ruler */}
          {(guides || []).filter(g => g.axis === 'x').map((g, gi) => {
            const gx = offsetLeft + unitToRulerPx(g.position);
            return (
              <g key={`gx-${gi}`} style={{ cursor: 'pointer' }}
                 onClick={(e) => { e.stopPropagation(); onRemoveGuide && onRemoveGuide(guides.indexOf(g)); }}>
                <polygon
                  points={`${gx - 4},${RULER_THICKNESS} ${gx + 4},${RULER_THICKNESS} ${gx},${RULER_THICKNESS - 6}`}
                  fill={guideColor}
                />
              </g>
            );
          })}
        </svg>
      </div>
    );
  };

  // ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  // VERTICAL RULER
  // ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  const renderVRuler = () => {
    const offsetTop = (canvasOffset?.top || 40) - scroll.top;

    return (
      <div
        ref={vRulerRef}
        onMouseDown={(e) => handleRulerDragStart('y', e)}
        onDoubleClick={(e) => {
          if (!onAddGuide) return;
          const rect = vRulerRef.current.getBoundingClientRect();
          const clickY = e.clientY - rect.top;
          const unitVal = (clickY - offsetTop) / zoom;
          const pos = pxToCanvasUnit(unitVal);
          if (pos >= 0 && pos <= canvasH) onAddGuide('y', Math.round(pos * 100) / 100);
        }}
        style={{
          position: 'absolute',
          top: RULER_THICKNESS,
          left: 0,
          bottom: 0,
          width: RULER_THICKNESS,
          backgroundColor: rulerBg,
          borderRight: `1px solid ${rulerBorder}`,
          overflow: 'hidden',
          zIndex: 30,
          cursor: 'crosshair',
          userSelect: 'none',
        }}
      >
        <svg
          width={RULER_THICKNESS}
          height="100%"
          style={{ display: 'block' }}
        >
          {vTicks.map((tick, i) => {
            const y = offsetTop + unitToRulerPx(tick.position);
            if (y < -10 || y > 4000) return null;
            const tickW = tick.isMajor ? 12 : tick.isMid ? 8 : 5;
            return (
              <g key={i}>
                <line
                  x1={RULER_THICKNESS}
                  y1={y}
                  x2={RULER_THICKNESS - tickW}
                  y2={y}
                  stroke={tick.isMajor ? tickColorMajor : tickColor}
                  strokeWidth={tick.isMajor ? 1 : 0.5}
                  opacity={tick.isMajor ? 0.9 : 0.5}
                />
                {tick.label !== null && (
                  <text
                    x={2}
                    y={y - 3}
                    fill={labelColor}
                    fontSize={8}
                    fontFamily={T.font}
                    transform={`rotate(-90, 2, ${y - 3})`}
                  >
                    {tick.label}
                  </text>
                )}
              </g>
            );
          })}

          {/* Cursor highlight */}
          {mousePos && mousePos.y >= 0 && mousePos.y <= canvasH && (
            <>
              <line
                x1={0}
                y1={offsetTop + unitToRulerPx(mousePos.y)}
                x2={RULER_THICKNESS}
                y2={offsetTop + unitToRulerPx(mousePos.y)}
                stroke={cursorColor}
                strokeWidth={1}
                opacity={0.8}
              />
              <rect
                x={1}
                y={offsetTop + unitToRulerPx(mousePos.y) - 6}
                width={RULER_THICKNESS - 2}
                height={13}
                rx={2}
                fill={T.primary.base}
                opacity={0.9}
              />
              <text
                x={RULER_THICKNESS / 2}
                y={offsetTop + unitToRulerPx(mousePos.y) + 4}
                fill="#fff"
                fontSize={7}
                fontFamily={T.font}
                fontWeight={600}
                textAnchor="middle"
              >
                {mousePos.y.toFixed(unit === 'in' ? 2 : unit === 'mm' ? 1 : 0)}
              </text>
            </>
          )}

          {/* Guide markers on vertical ruler */}
          {(guides || []).filter(g => g.axis === 'y').map((g, gi) => {
            const gy = offsetTop + unitToRulerPx(g.position);
            return (
              <g key={`gy-${gi}`} style={{ cursor: 'pointer' }}
                 onClick={(e) => { e.stopPropagation(); onRemoveGuide && onRemoveGuide(guides.indexOf(g)); }}>
                <polygon
                  points={`${RULER_THICKNESS},${gy - 4} ${RULER_THICKNESS},${gy + 4} ${RULER_THICKNESS - 6},${gy}`}
                  fill={guideColor}
                />
              </g>
            );
          })}
        </svg>
      </div>
    );
  };

  // ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  // CORNER ORIGIN MARKER
  // ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  const renderCorner = () => (
    <div style={{
      position: 'absolute',
      top: 0,
      left: 0,
      width: CORNER_SIZE,
      height: CORNER_SIZE,
      backgroundColor: rulerBg,
      borderRight: `1px solid ${rulerBorder}`,
      borderBottom: `1px solid ${rulerBorder}`,
      zIndex: 31,
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
      userSelect: 'none',
    }}>
      <span style={{
        fontSize: 8,
        fontWeight: 700,
        color: T.text.muted,
        fontFamily: T.font,
        textTransform: 'uppercase',
      }}>
        {unit}
      </span>
    </div>
  );

  // ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  // RENDER
  // ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  return (
    <>
      {renderCorner()}
      {renderHRuler()}
      {renderVRuler()}
    </>
  );
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// GRID OVERLAY
// Renders inside the canvas div as a positioned SVG grid pattern.
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

export function GridOverlay({
  canvas,          // { width, height, unit }
  gridSpacing,     // Grid spacing in canvas units
  canvasWidthPx,   // Resolved width in px
  canvasHeightPx,  // Resolved height in px (or 'auto')
  gridStyle,       // 'lines' | 'dots' | 'crosses'
}) {
  if (!gridSpacing || gridSpacing <= 0) return null;

  const spacingPx = unitToPx(gridSpacing, canvas.unit);
  if (spacingPx < 4) return null; // Too dense to render

  const w = canvasWidthPx;
  const h = canvasHeightPx === 'auto' ? 1200 : canvasHeightPx;
  const style = gridStyle || 'lines';

  const patternId = `grid-${style}-${spacingPx}`;

  return (
    <svg
      width={w}
      height={h}
      style={{
        position: 'absolute',
        top: 0,
        left: 0,
        pointerEvents: 'none',
        zIndex: 1,
      }}
    >
      <defs>
        {style === 'dots' && (
          <pattern id={patternId} width={spacingPx} height={spacingPx} patternUnits="userSpaceOnUse">
            <circle cx={spacingPx} cy={spacingPx} r={1} fill="rgba(59,130,246,0.25)" />
          </pattern>
        )}
        {style === 'crosses' && (
          <pattern id={patternId} width={spacingPx} height={spacingPx} patternUnits="userSpaceOnUse">
            <line x1={spacingPx - 2} y1={spacingPx} x2={spacingPx + 2} y2={spacingPx}
                  stroke="rgba(59,130,246,0.2)" strokeWidth={0.5} />
            <line x1={spacingPx} y1={spacingPx - 2} x2={spacingPx} y2={spacingPx + 2}
                  stroke="rgba(59,130,246,0.2)" strokeWidth={0.5} />
          </pattern>
        )}
        {style === 'lines' && (
          <pattern id={patternId} width={spacingPx} height={spacingPx} patternUnits="userSpaceOnUse">
            <path
              d={`M ${spacingPx} 0 L ${spacingPx} ${spacingPx} M 0 ${spacingPx} L ${spacingPx} ${spacingPx}`}
              fill="none"
              stroke="rgba(59,130,246,0.12)"
              strokeWidth={0.5}
            />
          </pattern>
        )}
      </defs>
      <rect width={w} height={h} fill={`url(#${patternId})`} />
    </svg>
  );
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// GUIDE LINES (rendered on canvas)
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

export function GuideLines({ canvas, guides, canvasWidthPx, canvasHeightPx, draggingGuide }) {
  const hasGuides = guides && guides.length > 0;
  const hasDrag = !!draggingGuide;
  if (!hasGuides && !hasDrag) return null;

  const w = canvasWidthPx;
  const h = canvasHeightPx === 'auto' ? 2000 : canvasHeightPx;

  return (
    <svg
      width={w}
      height={h}
      style={{
        position: 'absolute',
        top: 0,
        left: 0,
        pointerEvents: 'none',
        zIndex: 2,
      }}
    >
      {/* Committed guides */}
      {(guides || []).map((g, i) => {
        const pos = unitToPx(g.position, canvas.unit);
        if (g.axis === 'x') {
          return (
            <line key={i}
              x1={pos} y1={0} x2={pos} y2={h}
              stroke="#ef4444"
              strokeWidth={1}
              strokeDasharray="4 3"
              opacity={0.7}
            />
          );
        }
        return (
          <line key={i}
            x1={0} y1={pos} x2={w} y2={pos}
            stroke="#ef4444"
            strokeWidth={1}
            strokeDasharray="4 3"
            opacity={0.7}
          />
        );
      })}

      {/* Drag preview guide — shown while dragging from ruler */}
      {draggingGuide && (() => {
        const pos = unitToPx(draggingGuide.position, canvas.unit);
        const label = `${draggingGuide.position}${canvas.unit}`;
        if (draggingGuide.axis === 'x') {
          return (
            <g>
              <line x1={pos} y1={0} x2={pos} y2={h}
                stroke="#3b82f6" strokeWidth={1} strokeDasharray="6 3" opacity={0.8} />
              <rect x={pos + 4} y={8} width={40} height={16} rx={3}
                fill="#3b82f6" opacity={0.9} />
              <text x={pos + 24} y={20} fill="#fff" fontSize={9} fontWeight={600}
                textAnchor="middle" fontFamily="'Segoe UI',sans-serif">{label}</text>
            </g>
          );
        }
        return (
          <g>
            <line x1={0} y1={pos} x2={w} y2={pos}
              stroke="#3b82f6" strokeWidth={1} strokeDasharray="6 3" opacity={0.8} />
            <rect x={8} y={pos + 4} width={40} height={16} rx={3}
              fill="#3b82f6" opacity={0.9} />
            <text x={28} y={pos + 16} fill="#fff" fontSize={9} fontWeight={600}
              textAnchor="middle" fontFamily="'Segoe UI',sans-serif">{label}</text>
          </g>
        );
      })()}
    </svg>
  );
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// SNAP UTILITY
// Pure function — given a pixel value and a grid spacing (in px),
// returns the nearest snapped value.
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

export function snapToGrid(valuePx, gridSpacingUnits, unit) {
  if (!gridSpacingUnits || gridSpacingUnits <= 0) return valuePx;
  const spacingPx = unitToPx(gridSpacingUnits, unit);
  return Math.round(valuePx / spacingPx) * spacingPx;
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// RULER THICKNESS EXPORT (for layout offsetting)
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

export { RULER_THICKNESS };

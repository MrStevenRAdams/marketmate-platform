// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// useHistory — Undo / Redo Hook
// Maintains a linear history buffer of state snapshots. Every call
// to `push()` appends a new snapshot and truncates any "future"
// entries (i.e. if you undo then push, you lose the redo stack).
//
// The buffer is capped at `maxHistory` entries (default 80) to
// prevent unbounded memory growth during long editing sessions.
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

import { useState, useCallback } from 'react';

/**
 * @param {any}    initialState — The first snapshot (typically [])
 * @param {number} maxHistory   — Maximum snapshots to keep
 *
 * @returns {{
 *   current:       any,
 *   push:          (newState: any) => void,
 *   undo:          () => void,
 *   redo:          () => void,
 *   canUndo:       boolean,
 *   canRedo:       boolean,
 *   historyLength: number,
 *   pointer:       number
 * }}
 */
export default function useHistory(initialState, maxHistory = 80) {
  const [history, setHistory]   = useState([initialState]);
  const [pointer, setPointer]   = useState(0);

  const current = history[pointer];

  const push = useCallback((newState) => {
    setHistory((prev) => {
      // Truncate any "future" entries after current pointer, then append
      const nextHistory = [...prev.slice(0, pointer + 1), newState];
      // Trim from the front if over the max
      return nextHistory.length > maxHistory
        ? nextHistory.slice(-maxHistory)
        : nextHistory;
    });
    setPointer((prev) => Math.min(prev + 1, maxHistory - 1));
  }, [pointer, maxHistory]);

  const undo = useCallback(() => {
    setPointer((p) => Math.max(0, p - 1));
  }, []);

  const redo = useCallback(() => {
    setPointer((p) => Math.min(history.length - 1, p + 1));
  }, [history.length]);

  return {
    current,
    push,
    undo,
    redo,
    canUndo:       pointer > 0,
    canRedo:       pointer < history.length - 1,
    historyLength: history.length,
    pointer,
  };
}

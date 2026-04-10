// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// BLOCK TREE HELPERS
// All state mutations go through these pure functions. They always
// return a *new* tree (never mutate in place), which is essential
// for React's immutable state model and the undo/redo history.
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

import { deepClone } from './helpers';

/**
 * Find a block anywhere in the tree by its ID.
 * Searches recursively through children (columns, box, repeater).
 * @returns {Block|null}
 */
export const findBlockById = (blocks, id) => {
  for (const b of blocks) {
    if (b.id === id) return b;
    if (b.children) {
      const found = findBlockById(b.children, id);
      if (found) return found;
    }
  }
  return null;
};

/**
 * Return a new tree with one block updated via an updater function.
 * The updater receives a deep clone of the block and should return
 * the modified block.
 *
 * Usage: updateBlockInTree(blocks, id, b => ({ ...b, visible: false }))
 */
export const updateBlockInTree = (blocks, id, updater) =>
  blocks.map((b) => {
    if (b.id === id) return updater(deepClone(b));
    if (b.children) {
      return { ...b, children: updateBlockInTree(b.children, id, updater) };
    }
    return b;
  });

/**
 * Return a new tree with the specified block removed.
 * Recursively filters through all levels.
 */
export const removeBlockFromTree = (blocks, id) =>
  blocks
    .filter((b) => b.id !== id)
    .map((b) => {
      if (b.children) {
        return { ...b, children: removeBlockFromTree(b.children, id) };
      }
      return b;
    });

/**
 * Insert a block relative to a target block.
 *
 * @param {Block[]} blocks   — Current tree
 * @param {Block}   block    — Block to insert
 * @param {string}  targetId — ID of the reference block
 * @param {'before'|'after'|'inside'|'end'} position
 */
export const insertBlockInTree = (blocks, block, targetId, position) => {
  // No target → append to root
  if (!targetId) {
    return position === 'end' ? [...blocks, block] : [block, ...blocks];
  }

  const result = [];
  for (const b of blocks) {
    if (b.id === targetId) {
      if (position === 'before') {
        result.push(block);
        result.push(b);
      } else if (position === 'after') {
        result.push(b);
        result.push(block);
      } else if (position === 'inside' && b.children) {
        result.push({ ...b, children: [...b.children, block] });
      } else {
        result.push(b);
        result.push(block);
      }
    } else {
      result.push(
        b.children
          ? { ...b, children: insertBlockInTree(b.children, block, targetId, position) }
          : b
      );
    }
  }
  return result;
};

/**
 * Count every block in the tree (including nested children).
 * Used for the status bar and version history display.
 */
export const countAllBlocks = (blocks) => {
  let count = 0;
  for (const b of blocks) {
    count++;
    if (b.children) count += countAllBlocks(b.children);
  }
  return count;
};

// ── Move block up/down within its sibling list ─────────────────

/**
 * Find a block's parent and its index within the sibling array.
 * Returns { parent, siblings, index } or null.
 */
export function findParentAndIndex(blocks, id, parent = null) {
  for (let i = 0; i < blocks.length; i++) {
    if (blocks[i].id === id) {
      return { parent, siblings: blocks, index: i };
    }
    if (blocks[i].children) {
      const found = findParentAndIndex(blocks[i].children, id, blocks[i]);
      if (found) return found;
    }
  }
  return null;
}

/**
 * Move a block one position up or down in its sibling list.
 * Returns the original tree unchanged if the move is impossible.
 *
 * @param {'up'|'down'} direction
 */
export function moveBlockInTree(blocks, id, direction) {
  const newBlocks = deepClone(blocks);
  const loc = findParentAndIndex(newBlocks, id);
  if (!loc) return blocks;

  const { siblings, index } = loc;
  const newIdx = direction === 'up' ? index - 1 : index + 1;

  // Boundary check — can't move past start/end
  if (newIdx < 0 || newIdx >= siblings.length) return blocks;

  // Swap
  const temp = siblings[index];
  siblings[index] = siblings[newIdx];
  siblings[newIdx] = temp;

  return newBlocks;
}

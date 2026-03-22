// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// BARCODE SVG GENERATORS
// Client-side SVG generation for Code128, EAN-13, and QR codes.
// These produce raw SVG markup strings that are injected via
// dangerouslySetInnerHTML in the canvas renderer and inlined
// directly in the HTML serialiser output.
//
// NOTE: The QR generator produces a *visual approximation* using
// a deterministic hash — it is not a spec-compliant QR encoder.
// For production scanning, use a server-side library.
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// ── Code 128 ───────────────────────────────────────────────────
// Lookup table: ASCII character → bar pattern (1 = black, 0 = white).
// Only covers Code Set B (ASCII 32–126) which handles most text.

const CODE128_PATTERNS = {
  ' ': '11011001100', '!': '11001101100', '"': '11001100110',
  '#': '10010011000', '$': '10010001100', '%': '10001001100',
  '&': '10011001000', "'": '10011000100', '(': '10001100100',
  ')': '11001001000', '*': '11001000100', '+': '11000100100',
  ',': '10110011100', '-': '10011011100', '.': '10011001110',
  '/': '10111001100', '0': '10011101100', '1': '11001110010',
  '2': '11001011100', '3': '11001001110', '4': '11011100100',
  '5': '11001110100', '6': '11101101110', '7': '11101001100',
  '8': '11100101100', '9': '11100100110', ':': '11101100100',
  ';': '11100110100', '<': '11100110010', '=': '11011011000',
  '>': '11011000110', '?': '11000110110', '@': '10100011000',
  'A': '10001011000', 'B': '10001000110', 'C': '10110001000',
  'D': '10001101000', 'E': '10001100010', 'F': '11010001000',
  'G': '11000101000', 'H': '11000100010', 'I': '10110111000',
  'J': '10110001110', 'K': '10001101110', 'L': '10111011000',
  'M': '10111000110', 'N': '10001110110', 'O': '11101110110',
  'P': '11010001110', 'Q': '11000101110', 'R': '11011101000',
  'S': '11011100010', 'T': '11011101110', 'U': '11101011000',
  'V': '11101000110', 'W': '11100010110', 'X': '11101101000',
  'Y': '11101100010', 'Z': '11100011010',
};

const CODE128_START_B = '11010010000';
const CODE128_STOP    = '1100011101011';

/**
 * Generate a Code128 barcode as an SVG string.
 *
 * @param {string} text   — Data to encode
 * @param {number} width  — SVG width in px
 * @param {number} height — SVG height in px
 * @returns {string} Raw SVG markup
 */
export function generateCode128SVG(text, width, height) {
  if (!text) text = '0000';

  let pattern = CODE128_START_B;
  let checksum = 104; // Start B check value

  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    const code = CODE128_PATTERNS[c];
    if (code) {
      pattern += code;
      checksum += (text.charCodeAt(i) - 32) * (i + 1);
    } else {
      // Fallback for unsupported chars → use '0'
      pattern += CODE128_PATTERNS['0'];
      checksum += 16 * (i + 1);
    }
  }

  // Append check character + stop code
  const checkChar = String.fromCharCode((checksum % 103) + 32);
  pattern += CODE128_PATTERNS[checkChar] || CODE128_PATTERNS['0'];
  pattern += CODE128_STOP;

  const w = parseInt(width);
  const h = parseInt(height);
  const barWidth = w / pattern.length;

  let bars = '';
  for (let i = 0; i < pattern.length; i++) {
    if (pattern[i] === '1') {
      bars += `<rect x="${i * barWidth}" y="0" width="${barWidth + 0.5}" height="${h * 0.75}" fill="#000"/>`;
    }
  }

  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${w} ${h}" width="${w}" height="${h}">
    <rect width="100%" height="100%" fill="#fff"/>
    ${bars}
    <text x="50%" y="${h * 0.95}" text-anchor="middle" font-family="monospace" font-size="11" fill="#333">${text}</text>
  </svg>`;
}


// ── EAN-13 ─────────────────────────────────────────────────────
// Standard European Article Number barcode with parity encoding.

const EAN_L = ['0001101', '0011001', '0010011', '0111101', '0100011', '0110001', '0101111', '0111011', '0110111', '0001011'];
const EAN_G = ['0100111', '0110011', '0011011', '0100001', '0011101', '0111001', '0000101', '0010001', '0001001', '0010111'];
const EAN_R = ['1110010', '1100110', '1101100', '1000010', '1011100', '1001110', '1010000', '1000100', '1001000', '1110100'];

const EAN_PARITY = [
  'LLLLLL', 'LLGLGG', 'LLGGLG', 'LLGGGL', 'LGLLGG',
  'LGGLLG', 'LGGGLL', 'LGLGLG', 'LGLGGL', 'LGGLGL',
];

/**
 * Generate an EAN-13 barcode as an SVG string.
 *
 * @param {string} text   — 13-digit EAN (padded/truncated if needed)
 * @param {number} width  — SVG width in px
 * @param {number} height — SVG height in px
 * @returns {string} Raw SVG markup
 */
export function generateEAN13SVG(text, width, height) {
  const digits = (text || '5060123456789')
    .replace(/\D/g, '')
    .padEnd(13, '0')
    .slice(0, 13);

  const w = parseInt(width) || 200;
  const h = parseInt(height) || 80;
  const barH = h * 0.75;

  // Build bar pattern: start guard + left half + centre guard + right half + end guard
  let pattern = '101'; // Start guard

  const parity = EAN_PARITY[parseInt(digits[0])];
  for (let i = 1; i <= 6; i++) {
    const table = parity[i - 1] === 'L' ? EAN_L : EAN_G;
    pattern += table[parseInt(digits[i])];
  }

  pattern += '01010'; // Centre guard

  for (let i = 7; i <= 12; i++) {
    pattern += EAN_R[parseInt(digits[i])];
  }

  pattern += '101'; // End guard

  const barWidth = w / pattern.length;
  let bars = '';
  for (let i = 0; i < pattern.length; i++) {
    if (pattern[i] === '1') {
      bars += `<rect x="${i * barWidth}" y="0" width="${barWidth + 0.3}" height="${barH}" fill="#000"/>`;
    }
  }

  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${w} ${h}" width="${w}" height="${h}">
    <rect width="100%" height="100%" fill="#fff"/>
    ${bars}
    <text x="50%" y="${h * 0.95}" text-anchor="middle" font-family="monospace" font-size="11" fill="#333">${digits}</text>
  </svg>`;
}


// ── QR Code (visual approximation) ─────────────────────────────
// Generates a 21×21 module QR-like pattern with finder patterns
// and deterministic data modules derived from a hash of the input.
// This is NOT a scannable QR code — it's a visual placeholder.

/**
 * @param {string} text — Data to represent
 * @param {number} size — SVG width/height in px
 * @returns {string} Raw SVG markup
 */
export function generateQRSVG(text, size) {
  if (!text) text = '0000';

  const dim = parseInt(size) || 80;
  const modules = 21;
  const cellSize = dim / (modules + 2); // +2 for quiet zone

  // Simple hash for deterministic module placement
  let hash = 0;
  for (let i = 0; i < text.length; i++) {
    hash = ((hash << 5) - hash + text.charCodeAt(i)) | 0;
  }

  let rects = '';

  // Draw the three finder patterns (top-left, top-right, bottom-left)
  const drawFinder = (ox, oy) => {
    for (let y = 0; y < 7; y++) {
      for (let x = 0; x < 7; x++) {
        const isBorder = y === 0 || y === 6 || x === 0 || x === 6;
        const isInner  = x >= 2 && x <= 4 && y >= 2 && y <= 4;
        if (isBorder || isInner) {
          rects += `<rect x="${(ox + x + 1) * cellSize}" y="${(oy + y + 1) * cellSize}" width="${cellSize}" height="${cellSize}" fill="#000"/>`;
        }
      }
    }
  };

  drawFinder(0, 0);              // Top-left
  drawFinder(modules - 7, 0);    // Top-right
  drawFinder(0, modules - 7);    // Bottom-left

  // Fill data area with deterministic pattern
  let seed = Math.abs(hash);
  for (let y = 0; y < modules; y++) {
    for (let x = 0; x < modules; x++) {
      // Skip finder pattern regions
      if ((x < 8 && y < 8) || (x >= modules - 8 && y < 8) || (x < 8 && y >= modules - 8)) continue;

      // Timing patterns (alternating on row/col 6)
      if (x === 6 || y === 6) {
        if ((x + y) % 2 === 0) {
          rects += `<rect x="${(x + 1) * cellSize}" y="${(y + 1) * cellSize}" width="${cellSize}" height="${cellSize}" fill="#000"/>`;
        }
        continue;
      }

      // Data modules from hash seed
      seed = (seed * 1103515245 + 12345) & 0x7fffffff;
      if (seed % 3 !== 0) {
        rects += `<rect x="${(x + 1) * cellSize}" y="${(y + 1) * cellSize}" width="${cellSize}" height="${cellSize}" fill="#000"/>`;
      }
    }
  }

  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${dim} ${dim}" width="${dim}" height="${dim}">
    <rect width="100%" height="100%" fill="#fff" rx="2"/>
    ${rects}
  </svg>`;
}

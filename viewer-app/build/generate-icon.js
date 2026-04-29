/**
 * Generates build/icon.ico from scratch using pure Node.js (no native deps).
 * Produces sizes: 16, 32, 48, 64, 128, 256 px — all embedded as PNG inside ICO.
 */

const zlib = require('zlib');
const fs = require('fs');
const path = require('path');

// ── CRC32 ────────────────────────────────────────────────────────────────────
const CRC_TABLE = (() => {
  const t = new Uint32Array(256);
  for (let i = 0; i < 256; i++) {
    let c = i;
    for (let j = 0; j < 8; j++) c = (c & 1) ? (0xEDB88320 ^ (c >>> 1)) : (c >>> 1);
    t[i] = c;
  }
  return t;
})();

function crc32(buf) {
  let crc = 0xFFFFFFFF;
  for (let i = 0; i < buf.length; i++) crc = CRC_TABLE[(crc ^ buf[i]) & 0xFF] ^ (crc >>> 8);
  return (crc ^ 0xFFFFFFFF) >>> 0;
}

// ── PNG encoder ───────────────────────────────────────────────────────────────
function chunk(type, data) {
  const typeBuf = Buffer.from(type);
  const lenBuf = Buffer.allocUnsafe(4);
  lenBuf.writeUInt32BE(data.length, 0);
  const crcBuf = Buffer.allocUnsafe(4);
  crcBuf.writeUInt32BE(crc32(Buffer.concat([typeBuf, data])), 0);
  return Buffer.concat([lenBuf, typeBuf, data, crcBuf]);
}

function encodePNG(width, height, rgba) {
  // Build raw scanlines (filter byte 0 per row)
  const raw = Buffer.allocUnsafe(height * (1 + width * 4));
  for (let y = 0; y < height; y++) {
    raw[y * (1 + width * 4)] = 0; // None filter
    rgba.copy(raw, y * (1 + width * 4) + 1, y * width * 4, (y + 1) * width * 4);
  }

  const ihdrData = Buffer.allocUnsafe(13);
  ihdrData.writeUInt32BE(width, 0);
  ihdrData.writeUInt32BE(height, 4);
  ihdrData[8] = 8;  // bit depth
  ihdrData[9] = 6;  // RGBA
  ihdrData[10] = 0; ihdrData[11] = 0; ihdrData[12] = 0;

  return Buffer.concat([
    Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]), // PNG sig
    chunk('IHDR', ihdrData),
    chunk('IDAT', zlib.deflateSync(raw, { level: 9 })),
    chunk('IEND', Buffer.alloc(0)),
  ]);
}

// ── ICO container ─────────────────────────────────────────────────────────────
function buildICO(pngBuffers) {
  const n = pngBuffers.length;
  const headerSize = 6 + n * 16;
  let offset = headerSize;

  const header = Buffer.allocUnsafe(6);
  header.writeUInt16LE(0, 0); // reserved
  header.writeUInt16LE(1, 2); // type: ICO
  header.writeUInt16LE(n, 4);

  const entries = [];
  for (const { size, buf } of pngBuffers) {
    const e = Buffer.allocUnsafe(16);
    e[0] = size >= 256 ? 0 : size; // width  (0 = 256)
    e[1] = size >= 256 ? 0 : size; // height (0 = 256)
    e[2] = 0;  // color count
    e[3] = 0;  // reserved
    e.writeUInt16LE(1, 4);  // planes
    e.writeUInt16LE(32, 6); // bit count
    e.writeUInt32LE(buf.length, 8);
    e.writeUInt32LE(offset, 12);
    offset += buf.length;
    entries.push(e);
  }

  return Buffer.concat([header, ...entries, ...pngBuffers.map(p => p.buf)]);
}

// ── Camera icon renderer ──────────────────────────────────────────────────────
function lerp(a, b, t) { return a + (b - a) * t; }

function drawIcon(size) {
  const rgba = Buffer.alloc(size * size * 4, 0);

  function setPixel(x, y, r, g, b, a = 255) {
    x = Math.round(x); y = Math.round(y);
    if (x < 0 || x >= size || y < 0 || y >= size) return;
    const i = (y * size + x) * 4;
    // Alpha blend over existing
    const srcA = a / 255;
    const dstA = rgba[i + 3] / 255;
    const outA = srcA + dstA * (1 - srcA);
    if (outA === 0) return;
    rgba[i]     = Math.round((r * srcA + rgba[i]     * dstA * (1 - srcA)) / outA);
    rgba[i + 1] = Math.round((g * srcA + rgba[i + 1] * dstA * (1 - srcA)) / outA);
    rgba[i + 2] = Math.round((b * srcA + rgba[i + 2] * dstA * (1 - srcA)) / outA);
    rgba[i + 3] = Math.round(outA * 255);
  }

  // Fill rounded rect background: #0f172a
  const radius = size * 0.2;
  for (let y = 0; y < size; y++) {
    for (let x = 0; x < size; x++) {
      const dx = Math.max(0, Math.max(radius - x, x - (size - 1 - radius)));
      const dy = Math.max(0, Math.max(radius - y, y - (size - 1 - radius)));
      const dist = Math.sqrt(dx * dx + dy * dy);
      if (dist <= radius) {
        const aa = Math.max(0, Math.min(1, radius - dist));
        setPixel(x, y, 15, 23, 42, Math.round(aa * 255));
      }
    }
  }

  const s = size / 64; // scale factor

  // Helpers
  function fillRect(x, y, w, h, r, g, b, borderRadius = 0) {
    for (let py = Math.floor(y); py <= Math.ceil(y + h); py++) {
      for (let px = Math.floor(x); px <= Math.ceil(x + w); px++) {
        let alpha = 255;
        if (borderRadius > 0) {
          const cx = Math.max(x + borderRadius, Math.min(x + w - borderRadius, px));
          const cy = Math.max(y + borderRadius, Math.min(y + h - borderRadius, py));
          const d = Math.sqrt((px - cx) ** 2 + (py - cy) ** 2);
          alpha = Math.round(Math.max(0, Math.min(1, borderRadius - d)) * 255);
          if (alpha === 0) continue;
        }
        setPixel(px, py, r, g, b, alpha);
      }
    }
  }

  function fillCircle(cx, cy, r, red, g, b, alpha = 255) {
    for (let py = Math.floor(cy - r - 1); py <= Math.ceil(cy + r + 1); py++) {
      for (let px = Math.floor(cx - r - 1); px <= Math.ceil(cx + r + 1); px++) {
        const d = Math.sqrt((px - cx) ** 2 + (py - cy) ** 2);
        const aa = Math.max(0, Math.min(1, r - d + 0.5));
        if (aa > 0) setPixel(px, py, red, g, b, Math.round(aa * alpha));
      }
    }
  }

  // Mount arm: #1e3a5f
  fillRect(27 * s, 10 * s, 10 * s, 14 * s, 30, 58, 95, 3 * s);

  // Camera body: #1e3a8a (30,58,138)
  fillRect(8 * s, 22 * s, 38 * s, 22 * s, 30, 58, 138, 5 * s);

  // Lens housing ring: #1e3a5f
  fillCircle(46 * s, 33 * s, 11 * s, 30, 58, 95);

  // Lens blue: #1d4ed8 (29,78,216)
  fillCircle(46 * s, 33 * s, 8 * s, 29, 78, 216);

  // Lens dark center: #0f172a
  fillCircle(46 * s, 33 * s, 5 * s, 15, 23, 42);

  // Lens highlight (white, semi-transparent)
  fillCircle(43.5 * s, 30.5 * s, 1.5 * s, 255, 255, 255, 90);

  // REC dot outer: #ef4444 (239,68,68)
  fillCircle(16 * s, 33 * s, 3.5 * s, 239, 68, 68);
  // REC dot inner: #fca5a5 (252,165,165)
  fillCircle(16 * s, 33 * s, 1.5 * s, 252, 165, 165);

  // Ventilation lines
  if (size >= 32) {
    const lineColor = [37, 99, 235]; // #2563eb
    for (const lx of [22, 26]) {
      for (let py = Math.floor(26 * s); py <= Math.ceil(40 * s); py++) {
        fillCircle(lx * s, py, 0.6 * s, ...lineColor, 150);
      }
    }
  }

  return rgba;
}

// ── Main ──────────────────────────────────────────────────────────────────────
const SIZES = [16, 32, 48, 64, 128, 256, 512];

const images = SIZES.map(size => {
  const rgba = drawIcon(size);
  const buf = encodePNG(size, size, rgba);
  return { size, buf };
});

// ICO for Windows (all sizes)
const icoPath = path.join(__dirname, 'icon.ico');
fs.writeFileSync(icoPath, buildICO(images));
console.log(`Generated: ${icoPath} (${SIZES.join(', ')}px)`);

// PNG 512x512 for macOS / Linux
const png512 = images.find(i => i.size === 512);
const pngPath = path.join(__dirname, 'icon.png');
fs.writeFileSync(pngPath, png512.buf);
console.log(`Generated: ${pngPath} (512x512)`);

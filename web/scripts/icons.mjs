// App icons drawn in code: a purple disc with a check on the Dracula background.
// PNGs are encoded by hand (zlib from Node) so the build needs no image tools.

import { deflateSync } from "node:zlib";

const BG = [0x28, 0x2a, 0x36];
const DISC = [0xbd, 0x93, 0xf9];
const MARK = [0x28, 0x2a, 0x36];

// Geometry in a unit square.
const R = 0.34;
const CHECK = [
  [0.36, 0.52],
  [0.46, 0.62],
  [0.65, 0.41],
];
const STROKE = 0.045;

export function iconSVG() {
  const pts = CHECK.map(([x, y]) => `${x * 100},${y * 100}`).join(" ");
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><rect width="100" height="100" rx="22" fill="#282A36"/><circle cx="50" cy="50" r="${R * 100}" fill="#BD93F9"/><polyline points="${pts}" fill="none" stroke="#282A36" stroke-width="${STROKE * 200}" stroke-linecap="round" stroke-linejoin="round"/></svg>\n`;
}

function segDist(px, py, [ax, ay], [bx, by]) {
  const dx = bx - ax;
  const dy = by - ay;
  const t = Math.max(0, Math.min(1, ((px - ax) * dx + (py - ay) * dy) / (dx * dx + dy * dy)));
  return Math.hypot(px - (ax + t * dx), py - (ay + t * dy));
}

function coverage(fn, x, y, size) {
  // 4x4 supersampling for smooth edges.
  let hit = 0;
  for (let i = 0; i < 4; i++)
    for (let j = 0; j < 4; j++) if (fn((x + (i + 0.5) / 4) / size, (y + (j + 0.5) / 4) / size)) hit++;
  return hit / 16;
}

const mix = (a, b, t) => a.map((v, i) => Math.round(v + (b[i] - v) * t));

export function iconPNG(size, fullBleed) {
  const corner = 0.22;
  const inSquare = (u, v) => {
    if (fullBleed) return true;
    const cx = Math.min(Math.max(u, corner), 1 - corner);
    const cy = Math.min(Math.max(v, corner), 1 - corner);
    return Math.hypot(u - cx, v - cy) <= corner;
  };
  const inDisc = (u, v) => Math.hypot(u - 0.5, v - 0.5) <= R;
  const inCheck = (u, v) => segDist(u, v, CHECK[0], CHECK[1]) <= STROKE || segDist(u, v, CHECK[1], CHECK[2]) <= STROKE;

  const raw = Buffer.alloc((size * 4 + 1) * size);
  for (let y = 0; y < size; y++) {
    const row = y * (size * 4 + 1);
    raw[row] = 0;
    for (let x = 0; x < size; x++) {
      const a = coverage(inSquare, x, y, size);
      let c = BG;
      c = mix(c, DISC, coverage(inDisc, x, y, size));
      c = mix(c, MARK, coverage((u, v) => inDisc(u, v) && inCheck(u, v), x, y, size));
      const o = row + 1 + x * 4;
      raw[o] = c[0];
      raw[o + 1] = c[1];
      raw[o + 2] = c[2];
      raw[o + 3] = Math.round(a * 255);
    }
  }
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(size, 0);
  ihdr.writeUInt32BE(size, 4);
  ihdr[8] = 8; // bit depth
  ihdr[9] = 6; // RGBA
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk("IHDR", ihdr),
    chunk("IDAT", deflateSync(raw, { level: 9 })),
    chunk("IEND", Buffer.alloc(0)),
  ]);
}

const CRC = (() => {
  const t = new Uint32Array(256);
  for (let n = 0; n < 256; n++) {
    let c = n;
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    t[n] = c >>> 0;
  }
  return t;
})();

function crc32(buf) {
  let c = 0xffffffff;
  for (const b of buf) c = CRC[(c ^ b) & 0xff] ^ (c >>> 8);
  return (c ^ 0xffffffff) >>> 0;
}

function chunk(type, data) {
  const len = Buffer.alloc(4);
  len.writeUInt32BE(data.length);
  const td = Buffer.concat([Buffer.from(type, "ascii"), data]);
  const crc = Buffer.alloc(4);
  crc.writeUInt32BE(crc32(td));
  return Buffer.concat([len, td, crc]);
}

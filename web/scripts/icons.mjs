// App icons drawn in code: a pink T-bone steak with a white checkbox where the marrow
// sits, on the Dracula background. One scene feeds both the SVG and the PNGs; the PNGs
// are rasterised and encoded by hand (zlib from Node) so the build needs no image tools.

import { deflateSync } from "node:zlib";

const BG = "#282A36";
const SIDE = "#C2549A";
const EDGE = "#A8447F";
const MEAT = "#FF79C6";
const FAT = "#F8F8F2";

// Geometry in a 100-unit square. The steak's top face; its side is the same outline
// stacked downwards, and the fat rim is the inner half of a stroke along it.
const STEAK = "M8 46C8 31 22 25 40 25C60 25 80 30 89 44C95 53 92 63 83 64C72 65 66 59 56 61C45 63 39 71 26 70C13 69 8 59 8 46Z";
const DROP = 1; // the whole steak sits this far below the outline's own coordinates
const THICK = 7;
const RIM = 5;
const SEAMS = [
  { d: "M59 25C55 38 61 49 56 64", w: 6 }, // the bone
  { d: "M58 45C65 40 72 43 82 38", w: 3.5 },
];
const BOX = { x: 20, y: 34.5, w: 25, h: 25, r: 6, line: 2.5 };
const TICK = [
  [26, 47],
  [31, 52],
  [39.5, 41.5],
];
const TICK_W = 4.5;

export function iconSVG() {
  const side = Array.from({ length: THICK }, (_, i) => `<use href="#s" y="${i + 1}"/>`).join("");
  const seams = SEAMS.map((s) => `<path d="${s.d}" stroke-width="${s.w}"/>`).join("");
  const tick = TICK.map(([x, y]) => `${x},${y}`).join(" ");
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><defs><path id="s" d="${STEAK}"/><clipPath id="c"><use href="#s"/></clipPath></defs><rect width="100" height="100" rx="22" fill="${BG}"/><g transform="translate(0 ${DROP})"><g fill="${SIDE}" stroke="${SIDE}" stroke-width="2.5" stroke-linejoin="round">${side}</g><use href="#s" fill="none" stroke="${EDGE}" stroke-width="2.5" stroke-linejoin="round"/><g clip-path="url(#c)"><use href="#s" fill="${MEAT}" stroke="${FAT}" stroke-width="${RIM * 2}"/><g fill="none" stroke="${FAT}" stroke-linecap="round">${seams}</g></g><rect x="${BOX.x}" y="${BOX.y}" width="${BOX.w}" height="${BOX.h}" rx="${BOX.r}" fill="${FAT}" stroke="${BG}" stroke-width="${BOX.line}"/><polyline points="${tick}" fill="none" stroke="${BG}" stroke-width="${TICK_W}" stroke-linecap="round" stroke-linejoin="round"/></g></svg>\n`;
}

// Turns an absolute M/C path into a polyline, eight segments per curve.
function flatten(d) {
  const n = d.match(/-?\d*\.?\d+/g).map(Number);
  const pts = [[n[0], n[1]]];
  for (let i = 2; i + 5 < n.length; i += 6) {
    const [x0, y0] = pts[pts.length - 1];
    for (let k = 1; k <= 8; k++) {
      const t = k / 8;
      const a = (1 - t) ** 3;
      const b = 3 * (1 - t) ** 2 * t;
      const c = 3 * (1 - t) * t * t;
      const e = t ** 3;
      pts.push([a * x0 + b * n[i] + c * n[i + 2] + e * n[i + 4], a * y0 + b * n[i + 1] + c * n[i + 3] + e * n[i + 5]]);
    }
  }
  return pts;
}

// Segments with their bounding boxes, so far-away ones are skipped cheaply.
function segments(pts) {
  const out = [];
  for (let i = 1; i < pts.length; i++) {
    const [ax, ay] = pts[i - 1];
    const [bx, by] = pts[i];
    out.push({ ax, ay, dx: bx - ax, dy: by - ay, x0: Math.min(ax, bx), x1: Math.max(ax, bx), y0: Math.min(ay, by), y1: Math.max(ay, by) });
  }
  return out;
}

// Whether the point lies within r of the polyline.
function near(px, py, segs, r) {
  for (const s of segs) {
    if (px < s.x0 - r || px > s.x1 + r || py < s.y0 - r || py > s.y1 + r) continue;
    const t = Math.max(0, Math.min(1, ((px - s.ax) * s.dx + (py - s.ay) * s.dy) / (s.dx * s.dx + s.dy * s.dy)));
    const ex = px - (s.ax + t * s.dx);
    const ey = py - (s.ay + t * s.dy);
    if (ex * ex + ey * ey <= r * r) return true;
  }
  return false;
}

function inPoly(px, py, pts) {
  let inside = false;
  for (let i = 0, j = pts.length - 1; i < pts.length; j = i++) {
    const [xi, yi] = pts[i];
    const [xj, yj] = pts[j];
    if (yi > py !== yj > py && px < ((xj - xi) * (py - yi)) / (yj - yi) + xi) inside = !inside;
  }
  return inside;
}

function inRoundRect(px, py, { x, y, w, h, r }, grow = 0) {
  if (px < x - grow || px > x + w + grow || py < y - grow || py > y + h + grow) return false;
  const cx = Math.min(Math.max(px, x + r), x + w - r);
  const cy = Math.min(Math.max(py, y + r), y + h - r);
  return Math.hypot(px - cx, py - cy) <= r + grow;
}

const OUTLINE = flatten(STEAK);
const EDGES = segments(OUTLINE);
const TICK_SEGS = segments(TICK);
const SEAM_SEGS = SEAMS.map((s) => ({ segs: segments(flatten(s.d)), r: s.w / 2 }));
const X0 = Math.min(...OUTLINE.map((p) => p[0])) - 2;
const X1 = Math.max(...OUTLINE.map((p) => p[0])) + 2;
const Y0 = Math.min(...OUTLINE.map((p) => p[1])) - 2;
const Y1 = Math.max(...OUTLINE.map((p) => p[1])) + THICK + 2;

// The colour at one point of the scene, topmost layer first.
function paint(x, y) {
  y -= DROP;
  if (x < X0 || x > X1 || y < Y0 || y > Y1) return BG;
  if (near(x, y, TICK_SEGS, TICK_W / 2)) return BG;
  if (inRoundRect(x, y, BOX, -BOX.line / 2)) return FAT;
  if (inRoundRect(x, y, BOX, BOX.line / 2)) return BG;
  if (inPoly(x, y, OUTLINE)) {
    if (SEAM_SEGS.some((s) => near(x, y, s.segs, s.r))) return FAT;
    return near(x, y, EDGES, RIM) ? FAT : MEAT;
  }
  if (near(x, y, EDGES, 1.25)) return EDGE;
  for (let i = 1; i <= THICK; i++) {
    if (inPoly(x, y - i, OUTLINE) || near(x, y - i, EDGES, 1.25)) return SIDE;
  }
  return BG;
}

// scale shrinks the drawing about the centre, to keep a maskable icon inside its safe zone.
export function iconPNG(size, fullBleed, scale = 1) {
  const corner = 0.22;
  const inSquare = (u, v) => {
    if (fullBleed) return true;
    const cx = Math.min(Math.max(u, corner), 1 - corner);
    const cy = Math.min(Math.max(v, corner), 1 - corner);
    return Math.hypot(u - cx, v - cy) <= corner;
  };
  const RGB = new Map([BG, SIDE, EDGE, MEAT, FAT].map((h) => [h, [1, 3, 5].map((i) => parseInt(h.slice(i, i + 2), 16))]));
  const at = (u, v) => paint((u * 100 - 50) / scale + 50, (v * 100 - 50) / scale + 50);

  // Sample every pixel corner once; a pixel whose corners agree takes that colour,
  // and only pixels on an edge are supersampled 4x4.
  const grid = [];
  for (let y = 0; y <= size; y++) for (let x = 0; x <= size; x++) grid.push(at(x / size, y / size));

  const raw = Buffer.alloc((size * 4 + 1) * size);
  for (let y = 0; y < size; y++) {
    const row = y * (size * 4 + 1);
    raw[row] = 0;
    for (let x = 0; x < size; x++) {
      const k = y * (size + 1) + x;
      const flat = grid[k] === grid[k + 1] && grid[k] === grid[k + size + 1] && grid[k] === grid[k + size + 2];
      let r = 0;
      let g = 0;
      let b = 0;
      let hit = 0;
      for (let i = 0; i < 4; i++)
        for (let j = 0; j < 4; j++) {
          const u = (x + (i + 0.5) / 4) / size;
          const v = (y + (j + 0.5) / 4) / size;
          if (!inSquare(u, v)) continue;
          const c = RGB.get(flat ? grid[k] : at(u, v));
          r += c[0];
          g += c[1];
          b += c[2];
          hit++;
        }
      const o = row + 1 + x * 4;
      raw[o] = hit ? Math.round(r / hit) : 0;
      raw[o + 1] = hit ? Math.round(g / hit) : 0;
      raw[o + 2] = hit ? Math.round(b / hit) : 0;
      raw[o + 3] = Math.round((hit / 16) * 255);
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

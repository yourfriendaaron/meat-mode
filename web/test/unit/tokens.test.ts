import { test } from "node:test";
import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";

// Components pick colours by meaning: outside the token block (the :root rules at the
// top of styles.css) a rule uses role tokens (--role-*) only, never a palette token
// (--palette-*) or a literal colour. docs/design-tokens.md lists the roles.

const SRC = new URL("../../src/", import.meta.url);
const TOKEN_FILE = "styles.css";
const TOKEN_SELECTOR = /^:root(\[data-theme="(light|dark)"\])?$/;

// CSS named colours and system colours. transparent, currentColor and inherit are fine.
const NAMED = new Set(
  (
    "aliceblue antiquewhite aqua aquamarine azure beige bisque black blanchedalmond blue " +
    "blueviolet brown burlywood cadetblue chartreuse chocolate coral cornflowerblue cornsilk " +
    "crimson cyan darkblue darkcyan darkgoldenrod darkgray darkgreen darkgrey darkkhaki " +
    "darkmagenta darkolivegreen darkorange darkorchid darkred darksalmon darkseagreen " +
    "darkslateblue darkslategray darkslategrey darkturquoise darkviolet deeppink deepskyblue " +
    "dimgray dimgrey dodgerblue firebrick floralwhite forestgreen fuchsia gainsboro ghostwhite " +
    "gold goldenrod gray green greenyellow grey honeydew hotpink indianred indigo ivory khaki " +
    "lavender lavenderblush lawngreen lemonchiffon lightblue lightcoral lightcyan " +
    "lightgoldenrodyellow lightgray lightgreen lightgrey lightpink lightsalmon lightseagreen " +
    "lightskyblue lightslategray lightslategrey lightsteelblue lightyellow lime limegreen linen " +
    "magenta maroon mediumaquamarine mediumblue mediumorchid mediumpurple mediumseagreen " +
    "mediumslateblue mediumspringgreen mediumturquoise mediumvioletred midnightblue mintcream " +
    "mistyrose moccasin navajowhite navy oldlace olive olivedrab orange orangered orchid " +
    "palegoldenrod palegreen paleturquoise palevioletred papayawhip peachpuff peru pink plum " +
    "powderblue purple rebeccapurple red rosybrown royalblue saddlebrown salmon sandybrown " +
    "seagreen seashell sienna silver skyblue slateblue slategray slategrey snow springgreen " +
    "steelblue tan teal thistle tomato turquoise violet wheat white whitesmoke yellow " +
    "yellowgreen accentcolor accentcolortext activetext buttonborder buttonface buttontext " +
    "canvas canvastext field fieldtext graytext highlight highlighttext linktext mark marktext " +
    "selecteditem selecteditemtext visitedtext"
  ).split(" "),
);

const STRING = /"(?:[^"\\\n]|\\.)*"|'(?:[^'\\\n]|\\.)*'/g;

/** The literal colour in a declaration's value, if it has one. */
function literalColour(value: string): string | null {
  const hex = value.match(/#[0-9a-f]{3,8}\b/i);
  if (hex) return hex[0];
  const fn = value.match(/\b(rgba?|hsla?|hwb|lab|lch|oklab|oklch|color)\(/i);
  if (fn) return `${fn[1]}()`;
  for (const word of value.match(/-*[a-z_][\w-]*/gi) ?? []) {
    if (NAMED.has(word.toLowerCase())) return word;
  }
  return null;
}

/**
 * The colour rule breaks in one stylesheet, as "line N: selector { decl }" strings.
 * tokens: whether this file may hold the token block.
 */
function scan(css: string, tokens: boolean): string[] {
  // Blank out comments and url()s, keeping newlines so line numbers hold, and the
  // braces and semicolons inside strings, so they do not split rules.
  const blank = (m: string) => m.replace(/[^\n]/g, " ");
  const src = css
    .replace(/\/\*[\s\S]*?\*\//g, blank)
    .replace(STRING, (m) => m.replace(/[{};]/g, " "))
    .replace(/url\([^)]*\)/gi, blank);
  const out: string[] = [];
  const stack: string[] = [];
  let buf = "";
  let line = 1;
  let start = 1;
  const decl = () => {
    const text = buf.trim();
    buf = "";
    if (!text || !stack.length) return;
    const sel = stack[stack.length - 1];
    const colon = text.indexOf(":");
    if (colon < 0) return;
    const prop = text.slice(0, colon).trim();
    const value = text.slice(colon + 1).trim();
    const bare = value.replace(STRING, '""');
    const where = `line ${start}: ${sel} { ${prop}: ${value} }`;
    const inBlock = tokens && stack.length === 1 && TOKEN_SELECTOR.test(sel);
    if (inBlock) {
      // Only palette tokens hold literal colours; roles point at the palette.
      const lit = literalColour(bare);
      if (lit && !prop.startsWith("--palette-")) out.push(`${where}: literal colour ${lit} outside a palette token`);
      return;
    }
    if (/^--(palette|role)-/.test(prop)) out.push(`${where}: defines ${prop} outside the token block`);
    if (/var\(\s*--palette-/.test(value)) out.push(`${where}: uses a palette token; use a role token`);
    const lit = literalColour(bare);
    if (lit) out.push(`${where}: literal colour ${lit}; use a role token`);
  };
  for (const ch of src) {
    if (ch === "{") {
      stack.push(buf.trim().replace(/\s+/g, " "));
      buf = "";
    } else if (ch === "}") {
      decl();
      stack.pop();
    } else if (ch === ";") {
      decl();
    } else {
      if (!buf.trim() && ch.trim()) start = line;
      buf += ch;
    }
    if (ch === "\n") line++;
  }
  return out;
}

test("the scanner passes role tokens and colourless values", () => {
  const css = `.a { color: var(--role-agent); border: 1px solid currentColor; background: transparent; }
.b { white-space: nowrap; color: inherit; background: color-mix(in srgb, var(--role-action) 9%, transparent); }
.c { font-family: "Red Hat", serif; background: url(#red); }
@media (prefers-color-scheme: dark) { .d { color: var(--role-fg); } }`;
  assert.deepEqual(scan(css, false), []);
});

test("the scanner flags a palette token outside the block", () => {
  const out = scan(".chip {\n  color: var(--palette-pink);\n}", true);
  assert.equal(out.length, 1);
  assert.match(out[0], /^line 2: \.chip \{ color: var\(--palette-pink\) \}: uses a palette token/);
});

test("the scanner flags literal colours of every kind", () => {
  for (const value of ["#fff", "#ff79c6", "rgb(0 0 0)", "rgba(0, 0, 0, 0.5)", "hsl(10 50% 50%)", "oklch(70% 0.1 200)", "color(srgb 1 0 0)", "red", "White", "Canvas"]) {
    assert.equal(scan(`.x { color: ${value}; }`, false).length, 1, value);
  }
  assert.equal(scan(".x { box-shadow: 0 1px 2px rgba(0, 0, 0, 0.4); }", false).length, 1);
  assert.equal(scan(".x { background: color-mix(in srgb, #fff 10%, transparent); }", false).length, 1);
});

test("the scanner flags rules nested in at-rules", () => {
  assert.equal(scan("@media (max-width: 600px) { .x { color: var(--palette-red); } }", true).length, 1);
});

test("the token block holds the palette, and roles point at it", () => {
  const block = `:root { --palette-pink: #ff79c6; --role-agent: var(--palette-pink); --radius: 10px; }
:root[data-theme="light"] { --palette-pink: #a3144d; }`;
  assert.deepEqual(scan(block, true), []);
  assert.equal(scan(":root { --role-agent: #ff79c6; }", true).length, 1, "a role holds a literal");
  assert.equal(scan(":root { --palette-pink: #ff79c6; }", false).length, 2, "a block outside the token file");
  assert.equal(scan(":root .x { color: var(--palette-pink); }", true).length, 1, "a component under :root");
  assert.equal(scan(".x { --role-agent: var(--palette-pink); }", true).length, 2, "a role defined outside the block");
});

test("every stylesheet picks colours by role", () => {
  const files = readdirSync(SRC).filter((f) => f.endsWith(".css"));
  assert.ok(files.includes(TOKEN_FILE));
  const out = files.flatMap((f) => scan(readFileSync(new URL(f, SRC), "utf8"), f === TOKEN_FILE).map((v) => `${f} ${v}`));
  assert.deepEqual(out, []);
});

test("every role token used is defined", () => {
  const all = readdirSync(SRC)
    .filter((f) => f.endsWith(".css"))
    .map((f) => readFileSync(new URL(f, SRC), "utf8"))
    .join("\n");
  const defined = new Set([...all.matchAll(/(--(?:role|palette)-[\w-]+)\s*:/g)].map((m) => m[1]));
  const used = new Set([...all.matchAll(/var\(\s*(--(?:role|palette)-[\w-]+)/g)].map((m) => m[1]));
  assert.deepEqual([...used].filter((u) => !defined.has(u)), []);
});

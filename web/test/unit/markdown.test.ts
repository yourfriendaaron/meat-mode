import { test } from "node:test";
import assert from "node:assert/strict";
import { renderMarkdown } from "../../src/markdown.ts";

/** Each tag the renderer may write, with the attributes it may carry and what each may hold. */
const ALLOWED: Record<string, Record<string, RegExp>> = {
  p: { class: /^md-h$/ },
  br: {},
  em: {},
  strong: {},
  s: {},
  code: {},
  pre: {},
  blockquote: {},
  hr: {},
  ul: {},
  ol: { start: /^\d+$/ },
  li: {},
  table: {},
  thead: {},
  tbody: {},
  tr: {},
  th: { class: /^md-(left|right|center)$/ },
  td: { class: /^md-(left|right|center)$/ },
  a: { href: /^(https?:|mailto:)/i, title: /^[^"<>]*$/, target: /^_blank$/, rel: /^noopener noreferrer$/ },
};

const TAG = /<(\/?)([a-z0-9]+)((?:\s+[a-z-]+="[^"]*")*)\s*>/g;
const ATTR = /\s+([a-z-]+)="([^"]*)"/g;

/** Fails unless every tag and attribute in html is on the allowlist, and no other "<" is left. */
function assertSafe(html: string, input: string) {
  for (const m of html.matchAll(TAG)) {
    const [, close, name, attrs] = m;
    const allowed = ALLOWED[name];
    assert.ok(allowed, `tag <${name}> from ${JSON.stringify(input)}: ${html}`);
    if (close) assert.equal(attrs, "", `closing tag with attributes: ${html}`);
    for (const [, attr, value] of attrs.matchAll(ATTR)) {
      const rule = allowed[attr];
      assert.ok(rule, `attribute ${attr} on <${name}> from ${JSON.stringify(input)}: ${html}`);
      assert.match(value, rule, `${attr}="${value}" on <${name}>`);
    }
    if (name === "a" && !close) assert.match(attrs, /target="_blank" rel="noopener noreferrer"/);
  }
  assert.ok(!html.replace(TAG, "").includes("<"), `stray "<" from ${JSON.stringify(input)}: ${html}`);
}

const HOSTILE = [
  "<script>alert(1)</script>",
  "<img src=x onerror=alert(1)>",
  '<a href="javascript:alert(1)">x</a>',
  "<svg onload=alert(1)>",
  "<iframe src=https://example.com></iframe>",
  "<!-- comment --><style>body{display:none}</style>",
  "[x](javascript:alert(1))",
  "[x](JAVASCRIPT:alert(1))",
  "[x]( javascript:alert(1) )",
  "[x](<javascript:alert(1)>)",
  "[x](java\tscript:alert(1))",
  "[x](<java\tscript:alert(1)>)",
  "[x](jav&#x61;script:alert(1))",
  "[x](&#106;avascript:alert(1))",
  "[x](javascript&colon;alert(1))",
  "[x](vbscript:msgbox(1))",
  "[x](data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==)",
  "[x](data:image/png;base64,AAAA)",
  "[x](file:///etc/passwd)",
  "[x](//example.com/a)",
  "[x](/api/export)",
  "<javascript:alert(1)>",
  "<vbscript:alert(1)>",
  "<data:text/html,<script>alert(1)</script>>",
  "[x][r]\n\n[r]: javascript:alert(1)",
  '[x](https://example.com "a\\" onmouseover=\\"alert(1)")',
  '[x](https://example.com" onmouseover="alert(1))',
  "[x](https://example.com/\"><script>alert(1)</script>)",
  '![a" onerror="alert(1)](https://example.com/a.png)',
  "![x](javascript:alert(1))",
  "![x](data:image/png;base64,AAAA)",
  "![x](https://example.com/a.png)",
  "![](https://example.com/a.png)",
  "![*a* [b](https://example.com/b)](https://example.com/a.png)",
  "&lt;script&gt;alert(1)&lt;/script&gt;",
  "&#60;img src=x onerror=alert(1)&#62;",
  "&#x3C;script&#x3E;",
  "`<script>alert(1)</script>`",
  '```js" onclick="alert(1)\n<b>bold</b>\n```',
  "    <script>alert(1)</script>",
  "# Title {#id}\n\n## <b>x</b>",
  "Setext\n===",
  "***bold* and _nested **emphasis** with `code`_**",
  "*".repeat(200) + "a" + "*".repeat(200),
  "[".repeat(500) + "x" + "](https://example.com)".repeat(500),
  "> ".repeat(100) + "deep",
  "- [ ] Water the plants\n- [x] Call Sam",
  "| a | b |\n|:-|:-:|\n| <b>1</b> | [x](javascript:alert(1)) |",
  "~~gone~~",
  "1. one\n2. two\n\n7. seven",
  "---",
  "<details open ontoggle=alert(1)>",
  "a\u0000b <scr\u0000ipt>",
];

test("hostile Markdown renders with no tag or attribute outside the allowlist", () => {
  for (const input of HOSTILE) assertSafe(renderMarkdown(input), input);
});

test("raw HTML shows as text", () => {
  assert.equal(renderMarkdown("<b>hi</b>"), "<p>&lt;b&gt;hi&lt;/b&gt;</p>\n");
});

test("a link other than http, https or mailto shows as its text alone", () => {
  assert.equal(renderMarkdown("[Water](javascript:alert(1))"), "<p>Water</p>\n");
  assert.equal(renderMarkdown("[Water](/api/export)"), "<p>Water</p>\n");
  assert.equal(renderMarkdown("<javascript:alert(1)>"), "<p>javascript:alert(1)</p>\n");
});

test("http, https and mailto links open in a new tab", () => {
  assert.equal(
    renderMarkdown("[Plants](https://example.com/plants)"),
    '<p><a href="https://example.com/plants" target="_blank" rel="noopener noreferrer">Plants</a></p>\n',
  );
  assert.match(renderMarkdown("<mailto:sam@example.com>"), /<a href="mailto:sam@example.com" target="_blank" rel="noopener noreferrer">/);
});

test("an image is a link named by its alt text, or by its address with none", () => {
  assert.equal(
    renderMarkdown("![The *fern*](https://example.com/fern.png)"),
    '<p><a href="https://example.com/fern.png" target="_blank" rel="noopener noreferrer">The fern</a></p>\n',
  );
  assert.match(renderMarkdown("![](https://example.com/fern.png)"), />https:\/\/example.com\/fern.png<\/a>/);
  assert.equal(renderMarkdown("![Fern](data:image/png;base64,AAAA)"), "<p>Fern</p>\n");
  // An image inside a link that was kept shows as that link's text alone:
  // a link cannot hold a link.
  assert.equal(
    renderMarkdown("[![The fern](https://example.com/fern.png)](https://example.com/plants)"),
    '<p><a href="https://example.com/plants" target="_blank" rel="noopener noreferrer">The fern</a></p>\n',
  );
});

test("a heading is one size with no id", () => {
  assert.equal(renderMarkdown("# Plants\n### Herbs"), '<p class="md-h">Plants</p>\n<p class="md-h">Herbs</p>\n');
});

test("a task list item is its text, with no checkbox", () => {
  assert.equal(renderMarkdown("- [ ] Water the plants"), "<ul>\n<li>[ ] Water the plants</li>\n</ul>\n");
});

test("a single newline stays a line break", () => {
  assert.equal(renderMarkdown("Water the plants\nthen the herbs"), "<p>Water the plants<br>then the herbs</p>\n");
});

test("tables and strikethrough render; typographer stays off", () => {
  assert.match(renderMarkdown("| a |\n|---|\n| 1 |"), /<table>/);
  assert.match(renderMarkdown("| a | b |\n|:-:|---|\n| 1 | 2 |"), /<th class="md-center">a<\/th>\n<th>b<\/th>/);
  assert.equal(renderMarkdown("~~old~~"), "<p><s>old</s></p>\n");
  assert.equal(renderMarkdown('"quoted" -- (c)'), '<p>&quot;quoted&quot; -- (c)</p>\n');
});

const A = (href: string, text = href) => `<a href="${href}" target="_blank" rel="noopener noreferrer">${text}</a>`;

test("a bare http or https address becomes a link", () => {
  assert.equal(renderMarkdown("See https://example.com/a/b.html for more"), `<p>See ${A("https://example.com/a/b.html")} for more</p>\n`);
  assert.equal(renderMarkdown("Read http://example.com."), `<p>Read ${A("http://example.com")}.</p>\n`);
  assert.equal(renderMarkdown("(see https://example.com/x)"), `<p>(see ${A("https://example.com/x")})</p>\n`);
  assert.equal(renderMarkdown("https://example.com/wiki/A_(b)"), `<p>${A("https://example.com/wiki/A_(b)")}</p>\n`);
  assert.equal(renderMarkdown("a https://example.com/1 and https://example.com/2"), `<p>a ${A("https://example.com/1")} and ${A("https://example.com/2")}</p>\n`);
});

test("only addresses with a scheme link, and text in a link or code stays as it is", () => {
  assert.equal(renderMarkdown("Edit main.ts and example.com"), "<p>Edit main.ts and example.com</p>\n");
  assert.equal(renderMarkdown("https://"), "<p>https://</p>\n");
  assert.equal(renderMarkdown("[https://example.com/a](https://example.com/b)"), `<p>${A("https://example.com/b", "https://example.com/a")}</p>\n`);
  assert.equal(renderMarkdown("`https://example.com`"), "<p><code>https://example.com</code></p>\n");
  assert.equal(renderMarkdown("ftp://example.com javascript:alert(1)"), "<p>ftp://example.com javascript:alert(1)</p>\n");
});

test("a bare address is escaped like any other", () => {
  const input = 'https://example.com/"><b>x</b>&a=1';
  assertSafe(renderMarkdown(input), input);
});

test("a fence's info string adds no class", () => {
  assert.equal(renderMarkdown('```js" class="x\nlet a = 1;\n```'), "<pre><code>let a = 1;\n</code></pre>\n");
});

test("the allowlist check itself catches a stray tag, attribute or address", () => {
  assert.throws(() => assertSafe('<img src="x">', ""));
  assert.throws(() => assertSafe('<p onclick="x">a</p>', ""));
  assert.throws(() => assertSafe('<a href="javascript:x" target="_blank" rel="noopener noreferrer">a</a>', ""));
  assert.throws(() => assertSafe("<p>a <b onclick=x></p>", ""));
});

test("a long run of closing brackets trims in linear time", () => {
  const input = `https://example.com/${")".repeat(200_000)}`;
  const start = performance.now();
  assert.equal(renderMarkdown(input), `<p>${A("https://example.com/")}${")".repeat(200_000)}</p>\n`);
  assert.ok(performance.now() - start < 2000);
});

import { test } from "node:test";
import assert from "node:assert/strict";
import { enterEdit, indentEdit, pasteEdit, shortcutEdit, type Edit } from "../../src/mdedit.ts";

/** Runs an edit on text whose cursor is "|" (or a selection between two), and returns the result with the selection marked the same way. */
function run(marked: string, fn: (b: { value: string; start: number; end: number }) => Edit | null): string | null {
  const start = marked.indexOf("|");
  const second = marked.indexOf("|", start + 1);
  const value = marked.replaceAll("|", "");
  const end = second < 0 ? start : second - 1;
  const e = fn({ value, start, end });
  if (!e) return null;
  const out = value.slice(0, e.start) + e.text + value.slice(e.end);
  return e.selStart === e.selEnd ? `${out.slice(0, e.selStart)}|${out.slice(e.selStart)}` : `${out.slice(0, e.selStart)}|${out.slice(e.selStart, e.selEnd)}|${out.slice(e.selEnd)}`;
}

test("Enter continues a bullet, numbered or task list", () => {
  assert.equal(run("- Water the plants|", enterEdit), "- Water the plants\n- |");
  assert.equal(run("  * Feed the cat|", enterEdit), "  * Feed the cat\n  * |");
  assert.equal(run("9. Buy milk|", enterEdit), "9. Buy milk\n10. |");
  assert.equal(run("1) Buy milk|", enterEdit), "1) Buy milk\n2) |");
  assert.equal(run("- [x] Done thing|", enterEdit), "- [x] Done thing\n- [ ] |");
});

test("Enter in the middle of an item splits it under a new marker", () => {
  assert.equal(run("- Water |the plants", enterEdit), "- Water \n- |the plants");
});

test("Enter on an empty item ends the list", () => {
  assert.equal(run("- Water the plants\n- |", enterEdit), "- Water the plants\n|");
  assert.equal(run("- Water the plants\n- [ ] |", enterEdit), "- Water the plants\n|");
});

test("Enter is left alone outside a list, before the marker, with a selection, or in a code fence", () => {
  assert.equal(run("Water the plants|", enterEdit), null);
  assert.equal(run("-|- rule", enterEdit), null);
  assert.equal(run("- |Water| the plants", enterEdit), null);
  assert.equal(run("```\n- item|", enterEdit), null);
  assert.equal(run("-not a list|", enterEdit), null);
});

test("Tab nests list items and Shift+Tab un-nests them", () => {
  assert.equal(run("- a\n- b|", (b) => indentEdit(b, false)), "- a\n  - b|");
  assert.equal(run("- a\n  - b|", (b) => indentEdit(b, true)), "- a\n- b|");
  assert.equal(run("|- a\n- b|", (b) => indentEdit(b, false)), "  |- a\n  - b|");
});

test("Tab outside a list, or Shift+Tab at the top level, leaves focus to move on", () => {
  assert.equal(run("Water the plants|", (b) => indentEdit(b, false)), null);
  assert.equal(run("- top|", (b) => indentEdit(b, true)), null);
});

test("B, I and E wrap the selection, unwrap it, or open an empty pair", () => {
  assert.equal(run("Water |the| plants", (b) => shortcutEdit(b, "b")), "Water **|the|** plants");
  assert.equal(run("Water **|the|** plants", (b) => shortcutEdit(b, "b")), "Water |the| plants");
  assert.equal(run("Water |the| plants", (b) => shortcutEdit(b, "I")), "Water _|the|_ plants");
  assert.equal(run("run |", (b) => shortcutEdit(b, "e")), "run `|`");
  assert.equal(run("x|", (b) => shortcutEdit(b, "z")), null);
});

test("K makes a link with the address ready to type over", () => {
  assert.equal(run("See |the docs|", (b) => shortcutEdit(b, "k")), "See [the docs](|url|)");
  assert.equal(run("See |https://example.com|", (b) => shortcutEdit(b, "k")), "See [|](https://example.com)");
});

test("a URL pasted over selected text links it", () => {
  assert.equal(run("See |the docs|", (b) => pasteEdit(b, " https://example.com/a ")), "See [the docs](https://example.com/a)|");
  assert.equal(run("See |", (b) => pasteEdit(b, "https://example.com")), null);
  assert.equal(run("See |the docs|", (b) => pasteEdit(b, "not a url")), null);
  assert.equal(run("|https://example.com/old|", (b) => pasteEdit(b, "https://example.com/new")), null);
});

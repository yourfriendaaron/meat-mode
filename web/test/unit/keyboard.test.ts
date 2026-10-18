import { test } from "node:test";
import assert from "node:assert/strict";
import { keyboardInset } from "../../src/keyboard.ts";

const base = { layoutHeight: 844, height: 844, offsetTop: 0, scale: 1 };

test("no keyboard means no inset", () => {
  assert.equal(keyboardInset(base), 0);
});

test("a keyboard over the page reports the covered height", () => {
  assert.equal(keyboardInset({ ...base, height: 500 }), 344);
});

test("a panned visual viewport counts only what stays covered", () => {
  // iOS Safari scrolls the visual viewport down to show the focused field.
  assert.equal(keyboardInset({ ...base, height: 500, offsetTop: 120 }), 224);
});

test("a small gap is browser chrome, not a keyboard", () => {
  assert.equal(keyboardInset({ ...base, height: 790 }), 0);
});

test("pinch zoom is not a keyboard", () => {
  assert.equal(keyboardInset({ ...base, height: 400, scale: 2 }), 0);
});

test("a layout viewport that already shrank leaves nothing to lift", () => {
  // interactive-widget=resizes-content, or a browser that resizes the page.
  assert.equal(keyboardInset({ layoutHeight: 500, height: 500, offsetTop: 0, scale: 1 }), 0);
});

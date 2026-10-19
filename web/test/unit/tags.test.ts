import { test } from "node:test";
import assert from "node:assert/strict";
import { cleanTags, hasTag, sameTags, shownTags, typedTags } from "../../src/tags.ts";

test("human never shows, in any case", () => {
  assert.deepEqual(shownTags({ labels: ["human", "garden", "HUMAN", "Human-ish"] }), ["garden", "Human-ish"]);
  assert.deepEqual(shownTags({}), []);
});

test("a tag matches without case", () => {
  assert.equal(hasTag({ labels: ["Garden"] }, "garden"), true);
  assert.equal(hasTag({ labels: ["human"] }, "human"), false);
  assert.equal(hasTag({ labels: ["gardening"] }, "garden"), false);
});

test("cleanTags keeps the hub's rules: trim, drop empty, human and repeats, keep order", () => {
  assert.deepEqual(cleanTags([" garden ", "", "Garden", "human", "errands"]), { tags: ["garden", "errands"] });
  assert.deepEqual(cleanTags(["a".repeat(50)]), { tags: ["a".repeat(50)] });
  assert.deepEqual(cleanTags(["two words"]), { error: 'The tag "two words" has a space, comma or control character; a tag is one word.' });
  assert.deepEqual(cleanTags(["a,b"]), { error: 'The tag "a,b" has a space, comma or control character; a tag is one word.' });
  // Characters, not bytes: 50 accented letters fit.
  assert.deepEqual(cleanTags(["é".repeat(50)]), { tags: ["é".repeat(50)] });
});

test("typed tags split at spaces and commas and lose a leading @", () => {
  assert.deepEqual(typedTags(" @garden, errands  home/chores,"), ["garden", "errands", "home/chores"]);
  assert.deepEqual(typedTags(""), []);
});

test("sameTags compares in order", () => {
  assert.equal(sameTags(["a", "b"], ["a", "b"]), true);
  assert.equal(sameTags(["a", "b"], ["b", "a"]), false);
});

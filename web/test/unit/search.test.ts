import { test } from "node:test";
import assert from "node:assert/strict";
import type { Item } from "../../src/types.ts";
import { searchGroups } from "../../src/search.ts";

// Wednesday 14 October 2026, 10:00 local time.
const now = new Date(2026, 9, 14, 10, 0);
const t = (d: number, h: number, m = 0) => new Date(2026, 9, d, h, m).toISOString();

let n = 0;
function item(over: Partial<Item>): Item {
  n++;
  const repo = over.repo ?? "personal";
  const id = over.id ?? `bd-${n}`;
  const bridge = repo === "personal" ? "hub" : "home";
  return {
    key: `${bridge}/${repo}/${id}`,
    bridge,
    repo,
    id,
    title: `Task ${n}`,
    status: "open",
    priority: 2,
    createdAt: t(1, 9),
    updatedAt: t(1, 9),
    comments: [],
    ...over,
  };
}

const titles = (q: string, items: Item[]) => searchGroups(items, q, now).map((g) => [g.id, g.items.map((i) => i.title)]);

test("a blank query finds nothing", () => {
  const items = [item({ title: "Water the plants" })];
  assert.deepEqual(searchGroups(items, "", now), []);
  assert.deepEqual(searchGroups(items, "  \t ", now), []);
});

test("matches ignore case and accents, in the query and in the item", () => {
  const items = [item({ title: "Renew the café card" }), item({ title: "Water the plants" })];
  assert.deepEqual(titles("CAFE", items), [["found-open", ["Renew the café card"]]]);
  assert.deepEqual(titles("café", [item({ title: "Cafe opening" })]), [["found-open", ["Cafe opening"]]]);
});

test("every word must match, and may match in different fields", () => {
  const a = item({ title: "Water the plants", description: "Use the green can" });
  const b = item({ title: "Call Sam", comments: [{ id: "c1", author: "alex", text: "Ask about the plants", createdAt: t(13, 9) }] });
  const c = item({ title: "Buy milk" });
  assert.deepEqual(titles("plants green", [a, b, c]), [["found-open", ["Water the plants"]]]);
  assert.deepEqual(titles("sam   plants", [a, b, c]), [["found-open", ["Call Sam"]]]);
  assert.deepEqual(titles("plants zebra", [a, b, c]), []);
});

test("looks in the description and notes", () => {
  const items = [
    item({ title: "One", description: "bring a ladder" }),
    item({ title: "Two", comments: [{ id: "c1", author: "sam", text: "the ladder is borrowed", createdAt: t(13, 9) }] }),
    item({ title: "Three", description: "the ladder is in the shed" }),
    item({ title: "Four", comments: null }),
  ];
  assert.deepEqual(titles("ladder", items), [["found-open", ["One", "Three", "Two"]]]);
});

test("groups Open, Done and Deleted in that order, dropping empty ones", () => {
  const open = item({ title: "Plan the plants" });
  const done = item({ title: "Water the plants", status: "closed", closeReason: "Done", closedAt: t(13, 9) });
  const deleted = item({ title: "Move the plants", status: "closed", closeReason: "Dismissed", closedAt: t(12, 9) });
  const wontDo = item({ repo: "garden", title: "Feed the plants", status: "closed", closeReason: "Dismissed", closedAt: t(11, 9) });
  assert.deepEqual(titles("plants", [deleted, wontDo, done, open]), [
    ["found-open", ["Plan the plants"]],
    ["found-done", ["Water the plants", "Feed the plants"]],
    ["found-deleted", ["Move the plants"]],
  ]);
  assert.deepEqual(titles("move", [deleted, wontDo, done, open]), [["found-deleted", ["Move the plants"]]]);
  assert.deepEqual(searchGroups([done], "plants", now).map((g) => g.label), ["Done"]);
});

test("Done and Deleted run newest close first", () => {
  const items = [
    item({ title: "Old tea", status: "closed", closedAt: t(10, 9) }),
    item({ title: "New tea", status: "closed", closedAt: t(13, 9) }),
    item({ title: "Gone tea", status: "closed", closeReason: "Dismissed", closedAt: t(11, 9) }),
    item({ title: "Newer gone tea", status: "closed", closeReason: "Dismissed", closedAt: t(12, 9) }),
  ];
  assert.deepEqual(titles("tea", items), [
    ["found-done", ["New tea", "Old tea"]],
    ["found-deleted", ["Newer gone tea", "Gone tea"]],
  ]);
});

test("open items run by when, then priority, then title", () => {
  const items = [
    item({ title: "Tea later" }),
    item({ title: "Tea b", dueAt: t(14, 12), priority: 2 }),
    item({ title: "Tea a", dueAt: t(14, 12), priority: 2 }),
    item({ title: "Tea urgent", dueAt: t(14, 12), priority: 0 }),
    item({ title: "Tea first", dueAt: t(14, 9) }),
  ];
  assert.deepEqual(titles("tea", items), [["found-open", ["Tea first", "Tea urgent", "Tea a", "Tea b", "Tea later"]]]);
});

test("a sub-task is found by its parent's title, from the held parent or the title the tracker gave", () => {
  const parent = item({ id: "bd-p", title: "Plan the picnic" });
  const held = item({ title: "Pack the basket", parent: "bd-p" });
  const unheld = item({ title: "Book the park", parent: "bd-gone", parentTitle: "Picnic budget" });
  const other = item({ title: "Buy milk" });
  const items = [parent, held, unheld, other];
  const found = titles("picnic", items);
  assert.equal(found.length, 1);
  assert.deepEqual(new Set(found[0][1] as string[]), new Set(["Plan the picnic", "Pack the basket", "Book the park"]));
  // A held parent's title wins over a stale parentTitle, and words may split across the sub-task and its parent.
  const stale = item({ title: "Pack the basket", parent: "bd-p", parentTitle: "Old name" });
  assert.deepEqual(titles("old", [parent, stale]), []);
  assert.deepEqual(titles("basket picnic", [parent, stale]), [["found-open", ["Pack the basket"]]]);
});

test("a tag finds its item, with or without the @; human is never searched", () => {
  const tagged = item({ title: "Water the plants", labels: ["Garden"] });
  const agent = item({ repo: "garden", title: "Check the tomato labels", labels: ["human", "outdoor"] });
  const items = [tagged, agent, item({ title: "Buy milk" })];
  assert.deepEqual(titles("garden", items), [["found-open", ["Water the plants"]]]);
  assert.deepEqual(titles("@garden", items), [["found-open", ["Water the plants"]]]);
  assert.deepEqual(titles("outdoor", items), [["found-open", ["Check the tomato labels"]]]);
  assert.deepEqual(titles("human", items), []);
});

test("only narrows the items searched, as a tag filter does", () => {
  const a = item({ title: "Tea one", labels: ["kitchen"] });
  const b = item({ title: "Tea two" });
  const found = searchGroups([a, b], "tea", now, (i) => !!i.labels?.includes("kitchen"));
  assert.deepEqual(found.map((g) => g.items.map((i) => i.title)), [["Tea one"]]);
});

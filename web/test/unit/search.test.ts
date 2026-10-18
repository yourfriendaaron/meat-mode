import { test } from "node:test";
import assert from "node:assert/strict";
import type { Item, Repo } from "../../src/types.ts";
import { searchGroups } from "../../src/search.ts";

// Wednesday 14 October 2026, 10:00 local time.
const now = new Date(2026, 9, 14, 10, 0);
const t = (d: number, h: number, m = 0) => new Date(2026, 9, d, h, m).toISOString();

const repos: Repo[] = [
  { bridge: "home", name: "personal", personal: true, optedIn: true },
  { bridge: "home", name: "garden", personal: false, optedIn: true },
];

let n = 0;
function item(over: Partial<Item>): Item {
  n++;
  const repo = over.repo ?? "personal";
  const id = over.id ?? `bd-${n}`;
  return {
    key: `home/${repo}/${id}`,
    bridge: "home",
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

const titles = (q: string, items: Item[]) => searchGroups(items, repos, q, now).map((g) => [g.id, g.items.map((i) => i.title)]);

test("a blank query finds nothing", () => {
  const items = [item({ title: "Water the plants" })];
  assert.deepEqual(searchGroups(items, repos, "", now), []);
  assert.deepEqual(searchGroups(items, repos, "  \t ", now), []);
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

test("looks in the description, notes and checklist", () => {
  const items = [
    item({ title: "One", description: "bring a ladder" }),
    item({ title: "Two", comments: [{ id: "c1", author: "sam", text: "the ladder is borrowed", createdAt: t(13, 9) }] }),
    item({ title: "Three", checklist: [{ id: "k1", text: "Fetch ladder", done: false }] }),
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
  assert.deepEqual(searchGroups([done], repos, "plants", now).map((g) => g.label), ["Done"]);
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

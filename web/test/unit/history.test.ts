import { test } from "node:test";
import assert from "node:assert/strict";
import type { Item, Repo } from "../../src/types.ts";
import { completedGroups, deletedGroups } from "../../src/history.ts";

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
const done = (title: string, at: string, over: Partial<Item> = {}) =>
  item({ title, status: "closed", closedAt: at, closeReason: "Done in Meat Mode", ...over });
const deleted = (title: string, at: string) => done(title, at, { closeReason: "Dismissed" });
const titles = (items: Item[]) => items.map((i) => i.title);

test("Completed groups by local close day, newest day and item first, with day labels", () => {
  const groups = completedGroups(
    [
      done("Old", t(9, 12)),
      done("Late today", t(14, 9, 30)),
      done("Last night", t(13, 23, 59)),
      done("Early today", t(14, 0, 1)),
      done("Yesterday morning", t(13, 0, 5)),
    ],
    repos,
    now,
  );
  assert.deepEqual(groups.map((g) => [g.id, g.label]), [
    ["day-2026-10-14", "Today"],
    ["day-2026-10-13", "Yesterday"],
    ["day-2026-10-09", "Fri, Oct 9"],
  ]);
  assert.deepEqual(titles(groups[0].items), ["Late today", "Early today"]);
  assert.deepEqual(titles(groups[1].items), ["Last night", "Yesterday morning"]);
});

test("Completed includes an agent's Won't do, leaves out deleted personal and open items", () => {
  const groups = completedGroups(
    [
      done("Fed the cat", t(14, 8)),
      done("Skip the compost", t(14, 7), { repo: "garden", closeReason: "Dismissed" }),
      deleted("Binned", t(14, 6)),
      item({ title: "Still open" }),
      item({ title: "No close time", status: "closed" }),
    ],
    repos,
    now,
  );
  assert.equal(groups.length, 1);
  assert.deepEqual(titles(groups[0].items), ["Fed the cat", "Skip the compost"]);
  assert.deepEqual(completedGroups([item({})], repos, now), []);
});

test("Deleted lists only deleted personal items, newest first, in one group", () => {
  const groups = deletedGroups(
    [
      deleted("First", t(12, 9)),
      deleted("Third", t(14, 9)),
      deleted("Second", t(13, 9)),
      done("Completed", t(14, 8)),
      done("Won't do", t(14, 8), { repo: "garden", closeReason: "Dismissed" }),
      item({ title: "Open" }),
    ],
    repos,
    now,
  );
  assert.equal(groups.length, 1);
  assert.equal(groups[0].id, "deleted");
  assert.equal(groups[0].label, "Deleted");
  assert.deepEqual(titles(groups[0].items), ["Third", "Second", "First"]);
  assert.deepEqual(deletedGroups([done("Completed", t(14, 8))], repos, now), []);
});

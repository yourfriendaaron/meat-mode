import { test } from "node:test";
import assert from "node:assert/strict";
import type { Item, Repo } from "../../src/types.ts";
import type { Group } from "../../src/model.ts";
import { bulkDismissDone, bulkDismissLabel, bulkToast, canSelect, keepPickable, pickableKeys } from "../../src/bulk.ts";

const repos: Repo[] = [
  { bridge: "hub", name: "personal", personal: true, optedIn: true },
  { bridge: "home", name: "garden", personal: false, optedIn: true },
];

function item(repo: string, id: string, title: string, over: Partial<Item> = {}): Item {
  const bridge = repo === "personal" ? "hub" : "home";
  return {
    key: `${bridge}/${repo}/${id}`,
    bridge,
    repo,
    id,
    title,
    status: "open",
    priority: 2,
    createdAt: "2026-10-10T09:00:00Z",
    updatedAt: "2026-10-10T09:00:00Z",
    comments: [],
    ...over,
  };
}

const plants = item("personal", "pe-1", "Water the plants");
const porch = item("personal", "pe-2", "Sweep the porch");
const compost = item("garden", "ga-1", "Order compost for the beds");
const labels = item("garden", "ga-2", "Check the tomato labels");
const swept = item("personal", "pe-3", "Fix the gate latch", { status: "closed", closedAt: "2026-10-14T08:00:00Z" });

test("Select shows on the task lists and on search results", () => {
  for (const v of ["today", "upcoming", "agents", "all", "list:hub/personal"]) assert.equal(canSelect(v, ""), true, v);
  for (const v of ["completed", "deleted", "settings", "setup"]) assert.equal(canSelect(v, ""), false, v);
  assert.equal(canSelect("completed", "plants"), true);
  assert.equal(canSelect("deleted", "plants"), true);
  assert.equal(canSelect("today", "  "), true);
  assert.equal(canSelect("completed", "  "), false);
  assert.equal(canSelect("settings", "plants"), false);
  assert.equal(canSelect("setup", "plants"), false);
});

test("the dismiss label follows whose the picks are", () => {
  assert.equal(bulkDismissLabel([plants, porch], repos), "Delete");
  assert.equal(bulkDismissLabel([compost, labels], repos), "Won't do");
  assert.equal(bulkDismissLabel([plants, compost], repos), "Delete or won't do");
  assert.equal(bulkDismissLabel([], repos), "Delete");
  assert.equal(bulkDismissDone([plants], repos), "Deleted");
  assert.equal(bulkDismissDone([compost], repos), "Won't do");
  assert.equal(bulkDismissDone([porch, labels], repos), "Deleted or won't do");
});

test("Select all picks each open item in the list once, in order", () => {
  const groups: Group[] = [
    { id: "today", label: "Today", items: [plants, compost] },
    { id: "nodate", label: "No date", items: [porch, compost] },
    { id: "done", label: "Done", items: [swept] },
  ];
  assert.deepEqual(pickableKeys(groups), [plants.key, compost.key, porch.key]);
  assert.deepEqual(pickableKeys([]), []);
});

test("picks that left the list are dropped", () => {
  const picked = new Set([plants.key, swept.key, labels.key]);
  assert.deepEqual([...keepPickable(picked, [plants.key, porch.key, labels.key])], [plants.key, labels.key]);
  assert.deepEqual([...keepPickable(picked, [])], []);
});

test("a batch's toast names one task or counts them", () => {
  assert.equal(bulkToast("Done", ["Water the plants"]), "Done: Water the plants");
  assert.equal(bulkToast("Done", ["Water the plants", "Sweep the porch"]), "Done: 2 tasks");
  assert.equal(bulkToast("Won't do", ["a", "b", "c"]), "Won't do: 3 tasks");
});

import { test } from "node:test";
import assert from "node:assert/strict";
import { epicDetailHTML, epicsHTML, epicsOn, findRow, naturalCompare, outline, priorityName, progress, progressText, shownRows, statusName } from "../../src/epics.ts";
import type { EpicRow, Item, RepoEpics } from "../../src/types.ts";

function row(id: string, over: Partial<EpicRow> = {}): EpicRow {
  return { id, title: `Task ${id}`, status: "open", priority: 2, updatedAt: "2026-10-14T09:00:00Z", ...over };
}

function repo(rows: EpicRow[]): RepoEpics {
  return { bridge: "home", repo: "projects/garden", tracker: "beads", syncedAt: "2026-10-14T09:55:00Z", rows };
}

const none = () => undefined;

test("ids sort in natural order", () => {
  const ids = ["gd-1.10", "gd-10", "gd-1.9", "gd-2", "gd-1", "gd-1.2"];
  assert.deepEqual([...ids].sort(naturalCompare), ["gd-1", "gd-1.2", "gd-1.9", "gd-1.10", "gd-2", "gd-10"]);
  assert.equal(naturalCompare("gd-a", "gd-b") < 0, true);
  assert.equal(naturalCompare("gd-1", "gd-1"), 0);
});

test("priorities and statuses read as words", () => {
  assert.deepEqual([0, 1, 2, 3, 4].map(priorityName), ["Critical", "High", "Medium", "Low", "Backlog"]);
  assert.equal(statusName("in_progress"), "In progress");
  assert.equal(statusName("deferred"), "Deferred");
  assert.equal(statusName("something-new"), "Open");
});

test("progress is closed direct children over all, rounded down", () => {
  const kids = [...Array(6)].map((_, i) => row(`gd-1.${i + 1}`, { status: "closed" })).concat(row("gd-1.7", { status: "blocked" }));
  const p = progress(kids);
  assert.deepEqual(p, { done: 6, total: 7, percent: 85 });
  assert.equal(progressText(p), "85% 6/7 done");
  assert.equal(progressText(progress([])), "0% 0/0 done");
  // Two of three is 66.67: shown as 66, never rounded up to 67.
  assert.equal(progress([row("a", { status: "closed" }), row("b", { status: "closed" }), row("c")]).percent, 66);
  // Deferred, blocked and in progress count as not done.
  assert.equal(progress([row("a", { status: "deferred" }), row("b", { status: "in_progress" })]).done, 0);
});

test("cards come by status, then priority, then id; tasks after them in the same order", () => {
  const o = outline(
    shownRows(
      repo([
        row("gd-1", { epic: true, status: "blocked", priority: 0 }),
        row("gd-2", { epic: true, status: "open", priority: 3 }),
        row("gd-3", { epic: true, status: "in_progress", priority: 4 }),
        row("gd-4", { epic: true, status: "open", priority: 1 }),
        row("gd-5", { epic: true, status: "deferred", priority: 0 }),
        row("gd-10", { epic: true, status: "open", priority: 1 }),
        row("gd-20", { priority: 1 }),
        row("gd-9", { priority: 1 }),
        row("gd-8", { status: "in_progress", priority: 3 }),
      ]),
      none,
    ),
  );
  assert.deepEqual(
    o.cards.map((c) => c.row.id),
    ["gd-3", "gd-4", "gd-10", "gd-2", "gd-1", "gd-5"],
  );
  assert.deepEqual(
    o.tasks.map((t) => t.id),
    ["gd-8", "gd-9", "gd-20"],
  );
});

test("a card's children: open before closed, then priority, then id", () => {
  const o = outline(
    shownRows(
      repo([
        row("gd-1", { epic: true }),
        row("gd-1.10", { parent: "gd-1", priority: 1 }),
        row("gd-1.9", { parent: "gd-1", priority: 1 }),
        row("gd-1.1", { parent: "gd-1", status: "closed", priority: 0 }),
        row("gd-1.2", { parent: "gd-1", status: "blocked", priority: 3 }),
        row("gd-1.3", { parent: "gd-1", status: "deferred", priority: 0 }),
      ]),
      none,
    ),
  );
  assert.deepEqual(
    o.cards[0].children.map((c) => c.id),
    ["gd-1.3", "gd-1.9", "gd-1.10", "gd-1.2", "gd-1.1"],
  );
  assert.deepEqual(o.cards[0].progress, { done: 1, total: 5, percent: 20 });
  assert.deepEqual(o.tasks, []);
});

test("a nested card is a row of its parent and a card of its own", () => {
  const o = outline(
    shownRows(
      repo([
        row("gd-1", { epic: true }),
        row("gd-1.1", { epic: true, parent: "gd-1" }),
        row("gd-1.1.1", { parent: "gd-1.1", status: "closed" }),
        row("gd-1.2", { parent: "gd-1", status: "closed" }),
      ]),
      none,
    ),
  );
  assert.deepEqual(
    o.cards.map((c) => c.row.id),
    ["gd-1", "gd-1.1"],
  );
  assert.deepEqual(
    o.cards[0].children.map((c) => c.id),
    ["gd-1.1", "gd-1.2"],
  );
  // Progress counts direct children only.
  assert.equal(progressText(o.cards[0].progress), "50% 1/2 done");
  assert.equal(progressText(o.cards[1].progress), "100% 1/1 done");
});

test("a row whose parent is no card in the set is a top-level task", () => {
  const o = outline(shownRows(repo([row("gd-2", { parent: "gd-1" }), row("gd-3", { parent: "gd-3" })]), none));
  assert.deepEqual(
    o.tasks.map((t) => t.id),
    ["gd-2", "gd-3"],
  );
});

test("a human row with its State item shows the item's title and status", () => {
  const items: Item[] = [{ key: "home/projects/garden/gd-1.1", bridge: "home", repo: "projects/garden", id: "gd-1.1", title: "Water the plants today", status: "closed", priority: 2, createdAt: "", updatedAt: "", comments: [] } as Item];
  const find = (k: string) => items.find((i) => i.key === k);
  const rows = shownRows(
    repo([
      row("gd-1", { epic: true }),
      row("gd-1.1", { parent: "gd-1", human: true, title: "Water the plants" }),
      row("gd-1.2", { parent: "gd-1", human: true, title: "Weed the beds" }),
      // Not human: the item with its key, were there one, does not stand in.
      row("gd-1.3", { parent: "gd-1", title: "Mulch" }),
    ]),
    find,
  );
  assert.deepEqual(
    rows.map((r) => [r.key, r.title, r.status, r.item]),
    [
      ["home/projects/garden/gd-1", "Task gd-1", "open", false],
      ["home/projects/garden/gd-1.1", "Water the plants today", "closed", true],
      ["home/projects/garden/gd-1.2", "Weed the beds", "open", false],
      ["home/projects/garden/gd-1.3", "Mulch", "open", false],
    ],
  );
  // The item's Done counts toward the card at once.
  assert.equal(progressText(outline(rows).cards[0].progress), "33% 1/3 done");
  assert.equal(findRow({ repos: [repo(rows)] }, "home/projects/garden/gd-1.1", find)?.title, "Water the plants today");
  assert.equal(findRow({ repos: [repo(rows)] }, "home/projects/other/gd-1.1", find), undefined);
});

test("the view is offered only while some repo has its epics on", () => {
  assert.equal(epicsOn([{ bridge: "home", name: "garden", personal: false, optedIn: true, epics: true }]), false);
  assert.equal(epicsOn([{ bridge: "home", name: "garden", personal: false, optedIn: false, epics: true, epicsOn: true }]), true);
});

test("the view draws cards, progress, children when open, and an empty repo", () => {
  const now = new Date("2026-10-14T10:00:00Z");
  const data = {
    repos: [
      repo([row("gd-1", { epic: true, title: "Spring planting" }), row("gd-1.1", { parent: "gd-1", status: "closed", assignee: "sam" }), row("gd-2", { title: "Fix the <gate>" })]),
      { ...repo([]), bridge: "home", repo: "projects/website" },
    ],
  };
  const shut = epicsHTML({ data, error: "", expanded: new Set(), find: none, now });
  assert.match(shut, /<h2 id="ep-h-0">garden<\/h2><span class="muted small">Synced 5m ago<\/span>/);
  assert.match(shut, /100% 1\/1 done/);
  assert.match(shut, /aria-expanded="false"/);
  assert.doesNotMatch(shut, /sam/);
  assert.match(shut, /Other tasks/);
  assert.match(shut, /Fix the &lt;gate&gt;/);
  assert.match(shut, /Nothing open\./);
  const open = epicsHTML({ data, error: "", expanded: new Set(["home/projects/garden/gd-1"]), find: none, now });
  assert.match(open, /aria-expanded="true" aria-controls="ep-kids-0-0"/);
  assert.match(open, /<span class="ep-who">sam<\/span>/);
  assert.match(epicsHTML({ data: null, error: "", expanded: new Set(), find: none, now }), /Loading/);
  assert.match(epicsHTML({ data: { repos: [] }, error: "", expanded: new Set(), find: none, now }), /No epics yet/);
});

test("Details shows the title, status, priority and description as Markdown", () => {
  const html = epicDetailHTML({ ...row("gd-1", { epic: true, status: "in_progress", priority: 1, title: "Spring planting", description: "Plant the **beds**." }), key: "home/projects/garden/gd-1", item: false });
  assert.match(html, /Spring planting/);
  assert.match(html, /In progress/);
  assert.match(html, />High</);
  assert.match(html, /<strong>beds<\/strong>/);
  assert.match(epicDetailHTML({ ...row("gd-2"), key: "k", item: false }), /No description\./);
});

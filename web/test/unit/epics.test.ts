import { test } from "node:test";
import assert from "node:assert/strict";
import {
  directBlockers,
  tier,
  unlocks,
  attention,
  chipsHTML,
  clearFilters,
  collapseAll,
  epicDetailHTML,
  epicsHTML,
  epicsOffered,
  epicsOn,
  expandAll,
  findRow,
  matches,
  menusHTML,
  naturalCompare,
  newEpicsUI,
  outline,
  priorityName,
  progress,
  progressText,
  shownRows,
  statusLabel,
  statusName,
  typeIconSVG,
  type EpicsUI,
} from "../../src/epics.ts";
import type { EpicRow, Epics, Item, Repo, RepoEpics } from "../../src/types.ts";

function row(id: string, over: Partial<EpicRow> = {}): EpicRow {
  return { id, title: `Task ${id}`, status: "open", priority: 2, updatedAt: "2026-10-14T09:00:00Z", ...over };
}

function repo(rows: EpicRow[], name = "projects/garden"): RepoEpics {
  return { bridge: "home", repo: name, tracker: "beads", syncedAt: "2026-10-14T09:55:00Z", rows };
}

const none = () => undefined;
const now = new Date("2026-10-14T10:00:00Z");

function ui(over: Partial<EpicsUI> = {}): EpicsUI {
  return { ...newEpicsUI(), ...over };
}

function ids(rows: RepoEpics, u = ui()): string[] {
  return outline(shownRows(rows, none), u).entries.map((e) => e.row.id);
}

test("ids sort in natural order", () => {
  const list = ["gd-1.10", "gd-10", "gd-1.9", "gd-2", "gd-1", "gd-1.2"];
  assert.deepEqual([...list].sort(naturalCompare), ["gd-1", "gd-1.2", "gd-1.9", "gd-1.10", "gd-2", "gd-10"]);
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

test("cards go under their groups, then the top-level tasks; each by priority, then status, then most recently updated", () => {
  const r = repo([
    row("gd-1", { epic: true, status: "open", priority: 1 }),
    row("gd-2", { priority: 1, status: "blocked" }),
    row("gd-3", { epic: true, status: "in_progress", priority: 1 }),
    row("gd-4", { priority: 0, status: "deferred" }),
    row("gd-5", { priority: 2, updatedAt: "2026-10-12T09:00:00Z" }),
    row("gd-6", { priority: 2, updatedAt: "2026-10-13T09:00:00Z", epic: true }),
    row("gd-10", { priority: 2, updatedAt: "2026-10-12T09:00:00Z" }),
    row("gd-7", { priority: 1, status: "closed", epic: true }),
  ]);
  assert.deepEqual(ids(r), ["gd-3", "gd-1", "gd-6", "gd-7", "gd-4", "gd-2", "gd-5", "gd-10"]);
  const o = outline(shownRows(r, none), ui());
  assert.deepEqual(
    o.entries.map((e) => e.group),
    ["moving", "ready", "ready", "done", "task", "task", "task", "task"],
  );
  assert.deepEqual([o.epics, o.tasks, o.open], [4, 4, 7]);
});

test("the View menu's sorts: status, progress (least done first) and last updated", () => {
  const r = repo([
    row("gd-1", { epic: true, priority: 3 }),
    row("gd-1.1", { parent: "gd-1", status: "closed" }),
    row("gd-1.2", { parent: "gd-1" }),
    row("gd-2", { epic: true, priority: 3 }),
    row("gd-2.1", { parent: "gd-2" }),
    row("gd-3", { epic: true, priority: 0 }),
    row("gd-4", { priority: 1, status: "blocked", updatedAt: "2026-10-14T08:00:00Z" }),
    row("gd-5", { priority: 4, status: "in_progress", updatedAt: "2026-10-14T09:30:00Z" }),
  ]);
  assert.deepEqual(ids(r, ui({ sort: "status" })), ["gd-3", "gd-1", "gd-2", "gd-4", "gd-5"]);
  // A card with no children, and a task, after the cards with some.
  assert.deepEqual(ids(r, ui({ sort: "progress" })), ["gd-2", "gd-1", "gd-3", "gd-4", "gd-5"]);
  assert.deepEqual(ids(r, ui({ sort: "updated" })), ["gd-1", "gd-2", "gd-3", "gd-5", "gd-4"]);
});

test("a card's children sort the same way, closed ones last", () => {
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
    ui(),
  );
  assert.deepEqual(
    o.entries[0].children.map((c) => c.id),
    ["gd-1.3", "gd-1.9", "gd-1.10", "gd-1.2", "gd-1.1"],
  );
  assert.deepEqual(o.entries[0].progress, { done: 1, total: 5, percent: 20 });
  assert.equal(o.entries.length, 1);
  assert.equal(o.entries[0].open, false);
});

test("a nested card follows its parent one level in, shown once; a row whose parent is no card is top-level", () => {
  const o = outline(
    shownRows(
      repo([
        row("gd-1", { epic: true, priority: 3, status: "deferred" }),
        row("gd-1.1", { epic: true, parent: "gd-1", priority: 0 }),
        row("gd-1.1.1", { parent: "gd-1.1", status: "closed" }),
        row("gd-1.2", { parent: "gd-1", status: "closed" }),
        row("gd-5", { parent: "gd-9", priority: 1 }),
        row("gd-6", { parent: "gd-6", priority: 2 }),
      ]),
      none,
    ),
    ui({ expanded: new Set(["home/projects/garden/gd-1"]) }),
  );
  // Sorting alone would put the urgent gd-1.1 first, away from its parent.
  assert.deepEqual(
    o.entries.map((e) => [e.row.id, e.card, e.depth]),
    [
      ["gd-1", true, 0],
      ["gd-1.1", true, 1],
      ["gd-5", false, 0],
      ["gd-6", false, 0],
    ],
  );
  // The nested card goes in its parent's group and says its own state.
  assert.deepEqual(
    o.entries.slice(0, 2).map((e) => [e.group, e.own]),
    [
      ["deferred", "deferred"],
      ["deferred", "ready"],
    ],
  );
  assert.equal(o.epics, 2);
  // Progress counts every direct child; the open parent lists only its tasks.
  assert.deepEqual(
    o.entries[0].children.map((c) => c.id),
    ["gd-1.1", "gd-1.2"],
  );
  assert.deepEqual(
    o.entries[0].shown.map((c) => c.id),
    ["gd-1.2"],
  );
  assert.equal(progressText(o.entries[0].progress), "50% 1/2 done");
  assert.equal(progressText(o.entries[1].progress), "100% 1/1 done");
});

test("a search inside a nested card keeps its parent above it", () => {
  const o = outline(
    shownRows(
      repo([
        row("gd-1", { epic: true, title: "Spring planting" }),
        row("gd-1.1", { epic: true, parent: "gd-1", title: "Water the plants" }),
        row("gd-1.1.1", { parent: "gd-1.1", title: "Fix the hose" }),
        row("gd-2", { epic: true, title: "Paint the fence" }),
      ]),
      none,
    ),
    ui({ q: "hose" }),
  );
  assert.deepEqual(
    o.entries.map((e) => [e.row.id, e.depth, e.shown.map((k) => k.id)]),
    [
      ["gd-1", 0, []],
      ["gd-1.1", 1, ["gd-1.1.1"]],
    ],
  );
});

test("search and filters: OR within a group, AND across; a card shows with only its matching children, opened", () => {
  const r = repo([
    row("gd-1", { epic: true, title: "Spring planting", priority: 1 }),
    row("gd-1.1", { parent: "gd-1", title: "Buy the bulbs", status: "blocked" }),
    row("gd-1.2", { parent: "gd-1", title: "Dig the beds", status: "closed" }),
    row("gd-2", { title: "Oil the hinges", status: "blocked", priority: 0 }),
    row("gd-3", { title: "Bulb catalogue", priority: 4 }),
  ]);
  assert.equal(matches(row("gd-9", { title: "Water the Plants" }), ui({ q: "plants" })), true);
  assert.equal(matches(row("GD-9"), ui({ q: "gd-9" })), true);

  let o = outline(shownRows(r, none), ui({ q: "bulb" }));
  assert.deepEqual(
    o.entries.map((e) => [e.row.id, e.shown.map((k) => k.id), e.open]),
    [
      ["gd-1", ["gd-1.1"], true],
      ["gd-3", [], false],
    ],
  );
  // Statuses OR together; AND with the priority.
  o = outline(shownRows(r, none), ui({ statuses: new Set(["blocked", "closed"]), priorities: new Set([2]) }));
  assert.deepEqual(
    o.entries.map((e) => [e.row.id, e.shown.map((k) => k.id)]),
    [["gd-1", ["gd-1.1", "gd-1.2"]]],
  );
  // A card that matches with no matching child still shows, shut.
  o = outline(shownRows(r, none), ui({ q: "spring" }));
  assert.deepEqual(
    o.entries.map((e) => [e.row.id, e.shown.length, e.open]),
    [["gd-1", 0, false]],
  );
  // A card shut by hand while filtering stays shut.
  o = outline(shownRows(r, none), ui({ q: "bulb", shut: new Set(["home/projects/garden/gd-1"]) }));
  assert.equal(o.entries[0].open, false);
});

test("Needs attention: rows not closed that are Critical or Blocked, after the filters, by priority", () => {
  const data: Epics = {
    repos: [
      repo([row("gd-1", { status: "blocked", priority: 2 }), row("gd-2", { priority: 0 }), row("gd-3", { status: "closed", priority: 0 }), row("gd-4", { priority: 1 })]),
      repo([row("wb-1", { status: "blocked", priority: 1, title: "Fix the gate" })], "website"),
    ],
  };
  assert.deepEqual(
    attention(data, ui(), none).map((a) => a.row.id),
    ["gd-2", "wb-1", "gd-1"],
  );
  assert.deepEqual(
    attention(data, ui({ q: "gate" }), none).map((a) => a.row.id),
    ["wb-1"],
  );
  // A State item's status wins: done, it needs no attention.
  const done: Item[] = [{ key: "home/projects/garden/gd-2", status: "closed", title: "Done now" } as Item];
  data.repos[0].rows[1].human = true;
  assert.deepEqual(
    attention(data, ui(), (k) => done.find((i) => i.key === k)).map((a) => a.row.id),
    ["wb-1", "gd-1"],
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
  assert.equal(progressText(outline(rows, ui()).entries[0].progress), "33% 1/3 done");
  assert.equal(findRow({ repos: [repo(rows)] }, "home/projects/garden/gd-1.1", find)?.title, "Water the plants today");
  assert.equal(findRow({ repos: [repo(rows)] }, "home/projects/other/gd-1.1", find), undefined);
});

test("epics show where switched on, and the view is offered wherever a tracker can show them", () => {
  assert.equal(epicsOn([{ bridge: "home", name: "garden", personal: false, optedIn: true, epics: true }]), false);
  assert.equal(epicsOn([{ bridge: "home", name: "garden", personal: false, optedIn: false, epics: true, epicsOn: true }]), true);
  // The view is offered wherever a tracker can show epics, switched on or not.
  assert.equal(epicsOffered([{ bridge: "home", name: "garden", personal: false, optedIn: true, epics: true }]), true);
  assert.equal(epicsOffered([{ bridge: "home", name: "shed", personal: false, optedIn: true }]), false);
});

const stateRepos: Repo[] = [
  { bridge: "home", name: "projects/garden", personal: false, optedIn: true, colour: 4, epics: true, epicsOn: true },
  { bridge: "home", name: "website", personal: false, optedIn: true, colour: 1, epics: true, epicsOn: true },
];

function view(data: Epics | null, over: Partial<EpicsUI> = {}, error = "") {
  return epicsHTML({ data, error, ui: ui(over), find: none, repos: stateRepos, open: null, now });
}

test("the view draws each repo's heading with its mark, counts and fold, its rows, and an empty repo", () => {
  const data = {
    repos: [
      repo([
        row("gd-1", { epic: true, title: "Spring planting" }),
        row("gd-1.1", { parent: "gd-1", status: "closed", assignee: "sam" }),
        row("gd-2", { title: "Fix the <gate>", priority: 0, assignee: "alex" }),
      ]),
      repo([], "website"),
    ],
  };
  const shut = view(data);
  assert.match(shut, /<section class="ep-repo lc4" aria-labelledby="ep-h-0"><div class="ep-repo-head"><span class="rb rb-l lc4" aria-hidden="true">G<\/span>/);
  assert.match(shut, /<h2 id="ep-h-0">garden<\/h2><span class="ep-sum">1 epic, 1 task, 2 open · Synced 5m ago<\/span>/);
  assert.match(shut, /data-act="ep-fold" data-v="home\/projects\/garden" aria-expanded="true" aria-controls="ep-body-0" aria-label="Hide garden"/);
  assert.match(shut, /100% 1\/1 done/);
  assert.match(shut, /aria-expanded="false">1 task<span class="sr"> of gd-1<\/span>/);
  assert.doesNotMatch(shut, />sam</);
  assert.match(shut, /Fix the &lt;gate&gt;/);
  assert.match(shut, /<span class="ep-pri ep-p0">Critical<\/span><span class="ep-who">alex<\/span>/);
  assert.match(shut, /Nothing open\./);
  // Needs attention heads the view.
  assert.match(shut, /^<section class="ep-attn"[^]*Needs attention <span class="group-count">1<\/span>/);

  const open = view(data, { expanded: new Set(["home/projects/garden/gd-1"]) });
  assert.match(open, /aria-expanded="true" aria-controls="ep-kids-0-0"/);
  // A group heading leads its cards, and the tasks outside any epic come last.
  assert.match(shut, /<li class="ep-group ep-g-ready"><h3>Ready to start <span class="group-count">1<\/span><\/h3><\/li><li class="ep-item is-card"/);
  assert.match(shut, /<h3>No epic <span class="group-count">1<\/span><\/h3>[^]*Fix the &lt;gate&gt;/);
  assert.match(open, /<span class="ep-who">sam<\/span>/);
  // A folded repo keeps its heading only.
  const folded = view(data, { folded: new Set(["home/projects/garden"]) });
  assert.match(folded, /aria-expanded="false" aria-label="Show garden"/);
  assert.doesNotMatch(folded, /Spring planting/);
  // A filter drops a repo with nothing that matches.
  assert.doesNotMatch(view(data, { q: "spring" }), /website/);
  assert.match(view(data, { q: "nothing like it" }), /No epics or tasks match the filters\./);

  assert.match(view(null), /Loading/);
  assert.match(view({ repos: [] }), /No epics yet/);
  // With no repo switched on, the view points at the Repos menu.
  const off = stateRepos.map((r) => ({ ...r, epicsOn: false }));
  assert.match(epicsHTML({ data: { repos: [] }, error: "", ui: ui(), find: none, repos: off, open: null, now }), /Pick the repos to show in the Repos menu/);
  assert.match(view(null, {}, "hub down"), /Could not load epics: hub down/);
});

test("Needs attention shows five, then Show N more, and folds", () => {
  const data = { repos: [repo([...Array(7)].map((_, i) => row(`gd-${i + 1}`, { priority: 0 })))] };
  const html = view(data);
  assert.equal(html.match(/class="ep-attn-row[" ]/g)?.length, 5);
  assert.match(html, /Show 2 more/);
  const all = view(data, { attnAll: true });
  assert.equal(all.match(/class="ep-attn-row[" ]/g)?.length, 7);
  assert.match(all, /Show fewer/);
  const shut = view(data, { attnOpen: false });
  assert.doesNotMatch(shut, /ep-attn-row/);
  assert.match(shut, /aria-expanded="false" aria-label="Show Needs attention"/);
  // Absent when nothing needs it.
  assert.doesNotMatch(view({ repos: [repo([row("gd-1")])] }), /Needs attention/);
});

test("the menus: every repo that can show epics, checked where they show; statuses and priorities, sorts; the chips and Clear", () => {
  const data = { repos: [repo([row("gd-1"), row("gd-2", { status: "closed" })]), repo([row("wb-1")], "website")] };
  const states: Repo[] = [
    { ...stateRepos[0], epicsOn: false },
    { ...stateRepos[1], epicsPending: true },
    { bridge: "home", name: "shed", personal: false, optedIn: true, tracker: "ticket" },
  ];
  const tv = (over: Partial<EpicsUI> = {}, repos = states) => ({ data, ui: ui(over), find: none, repos });
  const shut = menusHTML(tv());
  assert.match(shut, /id="ep-mb-repo" data-act="ep-menu" data-v="repo" aria-haspopup="menu" aria-expanded="false"><span>Repos <span class="ep-n">1<\/span><\/span>/);
  assert.doesNotMatch(shut, /role="menu"/);
  const repoMenu = menusHTML(tv({ menu: "repo" }));
  // A repo switched off is listed, unchecked; the one that cannot show epics is not.
  assert.match(repoMenu, /role="menuitemcheckbox" aria-checked="false" data-act="ep-show-repo" data-v="home\/projects\/garden"[^]*?lc4[^]*?garden<\/span><\/button>/);
  assert.match(repoMenu, /aria-checked="true" data-act="ep-show-repo" data-v="home\/website"[^]*?website<\/span><span class="ep-n" role="status">Waiting for home<\/span>/);
  assert.doesNotMatch(repoMenu, /shed/);
  assert.doesNotMatch(repoMenu, /ep-f-repo/);
  assert.match(menusHTML(tv({ menu: "repo" }, [states[2]])), /No repo here can show epics/);
  const filter = menusHTML(tv({ menu: "filter", statuses: new Set(["blocked"]), priorities: new Set([0]) }));
  assert.match(filter, /Filter <span class="ep-n">2<\/span>/);
  assert.match(filter, /aria-checked="true" data-act="ep-f-status" data-v="blocked"/);
  assert.match(filter, /aria-checked="true" data-act="ep-f-pri" data-v="0"/);
  const v = menusHTML(tv({ menu: "view", sort: "progress" }));
  assert.match(v, /aria-checked="true" data-act="ep-sort" data-v="progress"/);
  assert.match(v, /Expand all[^]*Collapse all/);

  assert.equal(chipsHTML(tv()), "");
  const chips = chipsHTML(tv({ statuses: new Set(["in_progress"]), priorities: new Set([1]) }));
  assert.doesNotMatch(chips, /website/);
  assert.match(chips, /Remove filter: <\/span><span class="ep-st ep-s-in_progress">[^]*?In progress/);
  assert.match(chips, /Remove filter: <\/span>High/);
  assert.match(chips, /data-act="ep-clear">Clear</);
});

test("Clear, Expand all and Collapse all", () => {
  const data = { repos: [repo([row("gd-1", { epic: true }), row("gd-2")])] };
  const u = ui({ q: "x", statuses: new Set(["open"]), priorities: new Set([1]), folded: new Set(["home/projects/garden"]) });
  clearFilters(u);
  assert.deepEqual([u.q, u.statuses.size, u.priorities.size, u.folded.size], ["", 0, 0, 1]);
  expandAll(u, data);
  assert.deepEqual([...u.expanded], ["home/projects/garden/gd-1"]);
  assert.equal(u.folded.size, 0);
  collapseAll(u, data);
  assert.equal(u.expanded.size, 0);
  assert.deepEqual([...u.folded], ["home/projects/garden"]);
});

test("Details: every field, the description as Markdown, a card's children and a child's card", () => {
  const rows = [
    row("gd-1", { epic: true, status: "in_progress", priority: 1, title: "Spring planting", description: "Plant the **beds**.", assignee: "sam" }),
    row("gd-1.1", { parent: "gd-1", status: "closed", title: "Dig", closedAt: "2026-10-13T09:00:00Z", type: "chore" }),
    row("gd-1.2", { parent: "gd-1", title: "Sow" }),
  ];
  const data = { repos: [repo(rows)] };
  const dv = { data, find: none, repos: stateRepos, now };
  const card = findRow(data, "home/projects/garden/gd-1", none)!;
  const html = epicDetailHTML(card, dv);
  assert.match(html, /<h2 id="d-title" class="d-title">Spring planting<\/h2>/);
  assert.match(html, /<dt>List<\/dt><dd><span class="rb rb-m lc4" aria-hidden="true">G<\/span><span>garden<\/span>/);
  assert.match(html, /<dt>Id<\/dt><dd><code class="bead-id">gd-1<\/code><button class="icon-btn ep-copy"/);
  assert.match(html, /In progress/);
  assert.match(html, /<span class="ep-pri ep-p1">High<\/span>/);
  assert.match(html, /<dt>Type<\/dt>[^]*?epic/);
  assert.match(html, /<dt>Assignee<\/dt><dd>sam<\/dd>/);
  assert.match(html, /50% 1\/2 done/);
  assert.match(html, /<dt>Updated<\/dt><dd>1h ago/);
  assert.match(html, /<strong>beds<\/strong>/);
  assert.match(html, /Tasks <span class="group-count">2<\/span>[^]*data-key="home\/projects\/garden\/gd-1.2"[^]*data-key="home\/projects\/garden\/gd-1.1"/);
  assert.doesNotMatch(html, /Open task/);

  const child = epicDetailHTML(findRow(data, "home/projects/garden/gd-1.1", none)!, dv);
  assert.match(child, /<dt>Epic<\/dt><dd><button class="ep-link" data-act="ep-open" data-key="home\/projects\/garden\/gd-1">Spring planting<\/button>/);
  assert.match(child, /<dt>Closed<\/dt>/);
  assert.match(child, /No description\./);
  assert.match(child, /<dt>Assignee<\/dt><dd><span class="muted">Nobody<\/span>/);
});

test("Details show a row's comments oldest first as the task sheet shows notes, and keep the count until the text arrives", () => {
  const rows = [
    row("gd-1", {
      title: "Mend the hose",
      commentCount: 2,
      comments: [
        { id: "c-1", author: "alex", text: "Check the **washer** first.", createdAt: "2026-11-01T09:00:00Z" },
        { id: "c-2", author: "sam", text: "<b>Done</b>, water the plants.", createdAt: "2026-11-02T08:00:00Z" },
      ],
    }),
    row("gd-2", { title: "Paint the fence", commentCount: 3 }),
  ];
  const data = { repos: [repo(rows)] };
  const dv = { data, find: none, repos: stateRepos, now };
  const html = epicDetailHTML(findRow(data, "home/projects/garden/gd-1", none)!, dv);
  assert.match(html, /<h3 id="ep-d-comments">Comments<\/h3><ol class="notes">/);
  assert.match(html, /<li class="note note-agent" data-entry="c-1"><div class="note-head"><span class="note-author">alex<\/span><span class="note-time">[^<]+<\/span><\/div><div class="md"><p>Check the <strong>washer<\/strong> first\.<\/p>/);
  assert.ok(html.indexOf('data-entry="c-1"') < html.indexOf('data-entry="c-2"'), "oldest first");
  assert.match(html, /<span class="note-author">sam<\/span>/);
  assert.doesNotMatch(html, /<b>Done<\/b>/, "comment text is escaped Markdown");
  assert.doesNotMatch(html, /<dt>Comments<\/dt>/, "the list replaces the count fact");

  const counted = epicDetailHTML(findRow(data, "home/projects/garden/gd-2", none)!, dv);
  assert.match(counted, /<dt>Comments<\/dt><dd>3<\/dd>/);
  assert.doesNotMatch(counted, /ep-d-comments/);
});

test("Details of a human row with its State item say it is also your task and offer Open task", () => {
  const data = { repos: [repo([row("gd-3", { human: true, title: "Order compost" })])] };
  const items = [{ key: "home/projects/garden/gd-3", title: "Order compost for the beds", status: "in_progress" } as Item];
  const find = (k: string) => items.find((i) => i.key === k);
  const html = epicDetailHTML(findRow(data, "home/projects/garden/gd-3", find)!, { data, find, repos: stateRepos, now });
  assert.match(html, /Order compost for the beds/);
  assert.match(html, /Also your task/);
  assert.match(html, /<button class="btn btn-primary btn-small" data-act="open" data-key="home\/projects\/garden\/gd-3">Open task<\/button>/);
});

test("a status or type that is an inherited object key reads as an unknown one", () => {
  for (const k of ["constructor", "toString", "__proto__"]) {
    assert.equal(statusName(k), "Open");
    assert.equal(typeIconSVG(k), typeIconSVG("task"));
  }
});

test("an open row that waits on another shows as waiting, named as the terminal names it", () => {
  const r = repo([
    row("gd-1", { blockedBy: ["gd-2", "gd-3"] }),
    row("gd-2"),
    row("gd-3", { status: "in_progress" }),
    row("gd-4", { status: "deferred", blockedBy: ["gd-2"], deferUntil: "2026-11-24T12:00:00Z" }),
    row("gd-5", { status: "deferred" }),
  ]);
  const shown = shownRows(r, none);
  assert.deepEqual(
    shown.map((x) => x.status),
    ["waiting", "open", "in_progress", "deferred", "deferred"],
  );
  assert.deepEqual(
    shown.map((x) => statusLabel(x, now)),
    ["Waiting on gd-2 +1", "Open", "In progress", "Deferred to Nov 24", "Deferred"],
  );
  assert.deepEqual(ids(r), ["gd-3", "gd-2", "gd-1", "gd-4", "gd-5"]);
});

test("a waiting row's Details list what it waits on, and a card counts what its tasks hold up", () => {
  const r = repo([
    row("gd-1", { epic: true }),
    row("gd-1.1", { parent: "gd-1", blockedBy: ["gd-1.2"], commentCount: 2 }),
    row("gd-1.2", { parent: "gd-1", status: "blocked" }),
  ]);
  const data: Epics = { repos: [r] };
  const kid = findRow(data, "home/projects/garden/gd-1.1", none)!;
  const html = epicDetailHTML(kid, { data, find: none, repos: [], now });
  assert.match(html, /Waiting on gd-1\.2/);
  assert.match(html, /<dt>Waiting on<\/dt>.*Task gd-1\.2/s);
  assert.match(html, /<dt>Comments<\/dt><dd>2<\/dd>/);
  const list = epicsHTML({ data, error: "", ui: ui(), find: none, repos: [], open: null, now });
  assert.match(list, /title="1 blocked">.*1<span class="sr"> blocked<\/span><\/span><span class="ep-hold ep-s-waiting" title="1 waiting">/);
});

test("waiting cards run in the order they can start, each after the one bead that holds it up", () => {
  const r = repo([
    row("gd-1", { epic: true, priority: 1, title: "Clear the beds" }),
    row("gd-1.1", { parent: "gd-1", status: "in_progress" }),
    row("gd-2", { epic: true, priority: 2, blockedBy: ["gd-1"], title: "Plant the bulbs" }),
    row("gd-3", { epic: true, priority: 2, blockedBy: ["gd-1", "gd-2"], title: "Water the plants" }),
    row("gd-4", { epic: true, priority: 0, blockedBy: ["gd-1", "gd-3", "gd-2"], title: "Paint the fence" }),
  ]);
  const rows = shownRows(r, none);
  assert.deepEqual(directBlockers(rows, rows[4]), ["gd-3"]);
  assert.deepEqual([1, 2, 3, 4].map((n) => tier(rows, `gd-${n}`)), [1, 2, 3, 4]);
  assert.deepEqual(unlocks(rows, "gd-2").map((x) => x.id), ["gd-3"]);
  // gd-4 is Critical, but waits on the others.
  const o = outline(rows, ui());
  assert.deepEqual(
    o.entries.map((e) => [e.row.id, e.group]),
    [
      ["gd-1", "moving"],
      ["gd-2", "waiting"],
      ["gd-3", "waiting"],
      ["gd-4", "waiting"],
    ],
  );
  const data: Epics = { repos: [r] };
  const list = epicsHTML({ data, error: "", ui: ui(), find: none, repos: [], open: null, now });
  assert.match(list, /<h3>Waiting on another epic <span class="group-count">3<\/span>/);
  assert.match(list, /<span class="ep-after">after gd-3<\/span>/);
  // A card's own status is its group's heading, not a pill on its line.
  assert.doesNotMatch(list, /data-ep="home\/projects\/garden\/gd-4"><div class="ep-line">[^]*?ep-st ep-s-waiting[^]*?<\/div>/);
  const html = epicDetailHTML(findRow(data, "home/projects/garden/gd-3", none)!, { data, find: none, repos: [], now });
  assert.match(html, /<dt>Starts after<\/dt><dd><code class="ep-id">gd-2<\/code><\/dd>/);
  assert.match(html, /<dt>Unlocks<\/dt>.*Paint the fence/s);
});

test("a cycle of waits still names a blocker and ends its step count", () => {
  const rows = [row("gd-1", { epic: true, blockedBy: ["gd-2", "gd-3"] }), row("gd-2", { epic: true, blockedBy: ["gd-3"] }), row("gd-3", { epic: true, blockedBy: ["gd-1"] })];
  assert.ok(directBlockers(rows, rows[0]).length > 0);
  assert.ok(tier(rows, "gd-1") <= rows.length + 1);
});

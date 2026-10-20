import { test } from "node:test";
import assert from "node:assert/strict";
import type { Item, Repo } from "../../src/types.ts";
import {
  agentNewCount,
  attention,
  buildView,
  dateChip,
  groupOf,
  hasNewReply,
  isDeleted,
  isPersonal,
  isWontDo,
  kindOf,
  openToday,
  repoDetail,
  repoLabel,
  subtaskChip,
  subtasksFirst,
  syncStatus,
  visibleRepos,
} from "../../src/model.ts";
import { fmtSnoozeTime, snoozeOptions, typedSnooze } from "../../src/snooze.ts";

// Wednesday 14 October 2026, 10:00 local time.
const now = new Date(2026, 9, 14, 10, 0);
const t = (d: number, h: number, m = 0) => new Date(2026, 9, d, h, m).toISOString();

const repos: Repo[] = [
  { bridge: "hub", name: "personal", personal: true, optedIn: true },
  { bridge: "home", name: "garden", personal: false, optedIn: true },
  { bridge: "home", name: "attic", personal: false, optedIn: false },
];

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

test("groups an item by its due time", () => {
  assert.equal(groupOf(item({ dueAt: t(14, 9, 25) }), now), "pastdue");
  assert.equal(groupOf(item({ dueAt: t(12, 9) }), now), "pastdue");
  assert.equal(groupOf(item({ dueAt: t(14, 11) }), now), "today");
  assert.equal(groupOf(item({ dueAt: t(14, 17) }), now), "evening");
  assert.equal(groupOf(item({ dueAt: t(15, 9) }), now), "tomorrow");
  assert.equal(groupOf(item({ dueAt: t(18, 9) }), now), "week");
  assert.equal(groupOf(item({ dueAt: t(19, 9) }), now), "later");
  assert.equal(groupOf(item({}), now), "nodate");
});

test("a future snooze wins over the due date; an expired one resurfaces today", () => {
  assert.equal(groupOf(item({ dueAt: t(13, 9), deferUntil: t(15, 9) }), now), "tomorrow");
  assert.equal(groupOf(item({ deferUntil: t(14, 8) }), now), "today");
});

test("closed items show only in Done today when closed today", () => {
  assert.equal(groupOf(item({ status: "closed", closedAt: t(14, 8) }), now), "done");
  assert.equal(groupOf(item({ status: "closed", closedAt: t(13, 8) }), now), null);
});

test("Today view: today's agenda from every list, then the rest of the personal list", () => {
  const items = [
    item({ title: "Evening", dueAt: t(14, 19) }),
    item({ title: "Late", dueAt: t(14, 9) }),
    item({ title: "Noon", dueAt: t(14, 12) }),
    item({ title: "Morning", dueAt: t(14, 11), repo: "garden" }),
    item({ title: "Tomorrow", dueAt: t(15, 9) }),
    item({ title: "Agent tomorrow", dueAt: t(15, 9), repo: "garden" }),
    item({ title: "Closed", status: "closed", closedAt: t(14, 9) }),
    item({ title: "Agent closed", status: "closed", closedAt: t(14, 8), repo: "garden" }),
    item({ title: "Undated" }),
    item({ title: "Agent undated", repo: "garden" }),
  ];
  const groups = buildView("today", items, now);
  assert.deepEqual(
    groups.map((g) => [g.label, g.items.map((i) => i.title)]),
    [
      ["Past due", ["Late"]],
      ["Today", ["Morning", "Noon"]],
      ["This evening", ["Evening"]],
      ["Tomorrow", ["Tomorrow"]],
      ["No date", ["Undated"]],
      ["Done today", ["Closed", "Agent closed"]],
    ],
  );
  assert.equal(openToday(items, now), 4);
});

test("the kind of an item, repo or action is its bridge: the hub's own is personal, any other is an agent's", () => {
  assert.equal(kindOf(item({})), "personal");
  assert.equal(kindOf(item({ repo: "garden" })), "agent");
  assert.equal(kindOf(repos[0]), "personal");
  assert.equal(kindOf(repos[1]), "agent");
  assert.equal(kindOf({ bridge: "hub" }), "personal");
  // A bridge's own personal repo is an agent machine's repo; the hub keeps the personal list.
  const bridgePersonal: Repo = { bridge: "home", name: "personal", personal: true, optedIn: true };
  assert.equal(isPersonal(bridgePersonal), false);
});

test("Dismissed is Deleted on a personal item and Won't do on an agent item", () => {
  const gone = item({ status: "closed", closedAt: t(14, 9), closeReason: "Dismissed" });
  const wontDo = item({ status: "closed", closedAt: t(14, 9), closeReason: "Dismissed", repo: "garden" });
  const done = item({ status: "closed", closedAt: t(14, 9), closeReason: "Done in Meat Mode" });
  assert.deepEqual([isDeleted(gone), isWontDo(gone)], [true, false]);
  assert.deepEqual([isDeleted(wontDo), isWontDo(wontDo)], [false, true]);
  assert.deepEqual([isDeleted(done), isWontDo(done)], [false, false]);
  // An open item is neither, whatever reason it last closed with.
  assert.equal(isDeleted(item({ closeReason: "Dismissed" })), false);
});

test("a deleted personal item shows nowhere; an agent item's Won't do shows in Done today", () => {
  const items = [
    item({ title: "Deleted", status: "closed", closedAt: t(14, 9), closeReason: "Dismissed" }),
    item({ title: "Won't do", status: "closed", closedAt: t(14, 9), closeReason: "Dismissed", repo: "garden" }),
  ];
  for (const view of ["today", "all", "list:hub/personal"] as const) {
    const titles = buildView(view, items, now).flatMap((g) => g.items.map((i) => i.title));
    assert.ok(!titles.includes("Deleted"), view);
  }
  assert.deepEqual(buildView("today", items, now).flatMap((g) => g.items.map((i) => i.title)), ["Won't do"]);
});

test("Upcoming groups by tomorrow, this week, later", () => {
  const items = [item({ title: "A", dueAt: t(15, 9) }), item({ title: "B", dueAt: t(17, 9) }), item({ title: "C", dueAt: t(30, 9) })];
  const groups = buildView("upcoming", items, now);
  assert.deepEqual(groups.map((g) => g.label), ["Tomorrow", "This week", "Later"]);
});

test("Everything adds No date; agents and list views filter", () => {
  const items = [item({ title: "Mine" }), item({ title: "Theirs", repo: "garden" })];
  assert.deepEqual(buildView("all", items, now).map((g) => g.label), ["No date"]);
  assert.deepEqual(buildView("agents", items, now)[0].items.map((i) => i.title), ["Theirs"]);
  assert.deepEqual(buildView("list:hub/personal", items, now)[0].items.map((i) => i.title), ["Mine"]);
});

test("date chips", () => {
  assert.deepEqual(dateChip(item({ dueAt: t(14, 9, 25) }), now), { text: "35m overdue", tone: "overdue", snoozed: false });
  assert.deepEqual(dateChip(item({ dueAt: t(14, 7) }), now), { text: "3h overdue", tone: "overdue", snoozed: false });
  assert.deepEqual(dateChip(item({ dueAt: t(12, 9) }), now), { text: "2d overdue", tone: "overdue", snoozed: false });
  assert.deepEqual(dateChip(item({ dueAt: t(14, 14, 30) }), now), { text: "14:30", tone: "soon", snoozed: false });
  assert.deepEqual(dateChip(item({ dueAt: t(15, 9) }), now), { text: "Tomorrow 9:00", tone: "date", snoozed: false });
  assert.deepEqual(dateChip(item({ dueAt: t(23, 9) }), now), { text: "Fri, Oct 23", tone: "date", snoozed: false });
  assert.deepEqual(dateChip(item({ deferUntil: t(15, 9) }), now), { text: "Tomorrow 9:00", tone: "snoozed", snoozed: true });
  assert.equal(dateChip(item({}), now), null);
});

test("New is for agent items never opened; New reply needs a newer comment by someone else", () => {
  const fresh = item({ repo: "garden" });
  assert.equal(attention(fresh, "alex"), "new");
  assert.equal(attention(item({}), "alex"), null);

  const seen = item({
    repo: "garden",
    seenAt: t(14, 8),
    comments: [
      { id: "c1", author: "planner", text: "Old", createdAt: t(14, 7) },
      { id: "c2", author: "alex-app", text: "Mine", createdAt: t(14, 9) },
    ],
  });
  assert.equal(hasNewReply(seen, "alex"), false);
  seen.comments!.push({ id: "c3", author: "planner", text: "Answer", createdAt: t(14, 9, 30) });
  assert.equal(attention(seen, "alex"), "reply");
  assert.equal(agentNewCount([fresh, seen, item({})], "alex"), 2);
});

test("sync status", () => {
  const online = [{ name: "home", createdAt: t(1, 9), lastSeen: t(14, 9, 59) }];
  const stale = [{ name: "home", createdAt: t(1, 9), lastSeen: t(14, 9, 50) }];
  assert.deepEqual(syncStatus({ unapplied: 0, bridges: online }, now), { tone: "synced", text: "Synced" });
  assert.deepEqual(syncStatus({ unapplied: 2, bridges: online }, now), { tone: "waiting", text: "2 waiting" });
  assert.deepEqual(syncStatus({ unapplied: 0, bridges: stale }, now), { tone: "waiting", text: "Agent machine offline" });
  // No machine paired: only the personal list, which the hub keeps.
  assert.deepEqual(syncStatus({ unapplied: 0, bridges: [] }, now), { tone: "synced", text: "Synced" });
});

test("visible lists: personal first, opted-in only", () => {
  assert.deepEqual(visibleRepos(repos).map((r) => r.name), ["personal", "garden"]);
});

test("snooze presets", () => {
  const opts = snoozeOptions(now);
  const fmt = (d: Date) => `${d.getDate()} ${d.getHours()}:${d.getMinutes()}`;
  assert.deepEqual(
    opts.map((o) => [o.id, fmt(o.until)]),
    [
      ["hour", "14 11:0"],
      ["evening", "14 18:0"],
      ["tomorrow", "15 9:0"],
      ["weekend", "17 10:0"],
      ["nextweek", "19 9:0"],
      ["month", "14 9:0"],
    ],
  );
  assert.equal(opts[5].until.getMonth(), 10);
  const late = snoozeOptions(new Date(2026, 9, 17, 19, 0));
  assert.equal(late.find((o) => o.id === "evening"), undefined);
  assert.equal(late.find((o) => o.id === "weekend")!.until.getDate(), 24);
});

test("repo labels are folder names, with the path where they repeat", () => {
  const repos: Repo[] = [
    { bridge: "laptop", name: "projects/garden", personal: false, optedIn: true },
    { bridge: "laptop", name: "work/garden", personal: false, optedIn: true },
    { bridge: "desk", name: "projects/garden", personal: false, optedIn: true },
    { bridge: "laptop", name: "projects/kitchen", personal: false, optedIn: true },
    { bridge: "hub", name: "personal", personal: true, optedIn: true },
    { bridge: "hub", name: "projects/attic", personal: false, optedIn: true },
  ];
  assert.equal(repoLabel(repos[0]), "garden");
  assert.equal(repoLabel(repos[4]), "personal");
  assert.equal(repoDetail(repos[3], repos), "");
  assert.equal(repoDetail(repos[1], repos), "work/garden");
  assert.equal(repoDetail(repos[0], repos), "projects/garden on laptop");
  assert.equal(repoDetail(repos[2], repos), "projects/garden on desk");
  assert.deepEqual(
    visibleRepos(repos).map((r) => `${r.bridge}:${r.name}`),
    ["hub:personal", "hub:projects/attic", "desk:projects/garden", "laptop:projects/garden", "laptop:work/garden", "laptop:projects/kitchen"],
  );
});

test("a typed snooze time reads like quick add and refuses the past", () => {
  const cases: [string, string | null, string][] = [
    ["", null, ""],
    ["8p", "14 20:00", "Today, 20:00"],
    ["in 2 hours", "14 12:00", "Today, 12:00"],
    ["tomorrow morning", "15 9:00", "Tomorrow, 9:00"],
    ["friday 3pm", "16 15:00", "Fri, Oct 16, 15:00"],
    ["today 8am", null, "Pick a time in the future."],
    ["yesterday", null, "Pick a time in the future."],
    ["last friday", null, "Pick a time in the future."],
    ["2 days ago", null, "Pick a time in the future."],
    ["last week", null, "Pick a time in the future."],
    ["last night", null, "Pick a time in the future."],
    ["whenever", null, "Could not read that time. Try something like tomorrow 9am."],
  ];
  const fmt = (d: Date | null) => (d ? `${d.getDate()} ${d.getHours()}:${String(d.getMinutes()).padStart(2, "0")}` : null);
  for (const [text, until, line] of cases) {
    const r = typedSnooze(text, now);
    assert.equal(fmt(r.until), until, text);
    assert.equal(r.line, line, text);
  }
});

test("a time picked from the calendar reads back to the same minute", () => {
  const picks = [
    new Date(2026, 9, 14, 20, 0),
    new Date(2026, 9, 15, 9, 0),
    new Date(2026, 9, 22, 20, 5),
    new Date(2026, 11, 31, 23, 59),
    new Date(2027, 0, 3, 9, 5),
    new Date(2027, 9, 22, 7, 30),
  ];
  assert.equal(fmtSnoozeTime(picks[2], now), "Thu, Oct 22, 20:05");
  assert.equal(fmtSnoozeTime(picks[4], now), "Sun, Jan 3, 2027, 9:05");
  for (const d of picks) {
    const text = fmtSnoozeTime(d, now);
    assert.equal(typedSnooze(text, now).until?.getTime(), d.getTime(), text);
    assert.equal(typedSnooze(text, now).line, text);
  }
  // A picked day already past keeps its year, so it reads as past, not as next year.
  const past = new Date(2026, 9, 13, 8, 30);
  assert.equal(fmtSnoozeTime(past, now), "Tue, Oct 13, 2026, 8:30");
  assert.deepEqual(typedSnooze(fmtSnoozeTime(past, now), now), { until: null, line: "Pick a time in the future." });
});

test("a sub-task shows under its open parent, and on its own row otherwise", () => {
  const parent = item({ id: "p", title: "Plan the party", dueAt: t(14, 11) });
  const later = item({ id: "s1", title: "Book a cake", parent: "p", dueAt: t(20, 9) });
  const undated = item({ id: "s2", title: "Send invitations", parent: "p" });
  const doneSub = item({ id: "s3", title: "Pick a date", parent: "p", status: "closed", closedAt: t(14, 9) });
  const orphan = item({ id: "s4", title: "Pick a fence", parent: "gone", parentTitle: "Garden plan", dueAt: t(14, 12) });
  const groups = buildView("all", [parent, later, undated, doneSub, orphan], now);
  const today = groups.find((g) => g.id === "today")!;
  assert.deepEqual(
    today.items.map((i) => i.id),
    ["p", "s1", "s2", "s4"],
  );
  assert.deepEqual([...(today.nested ?? [])].sort(), [later.key, undated.key].sort());
  // A closed sub-task is a done item of its own.
  assert.deepEqual(groups.find((g) => g.id === "done")!.items.map((i) => i.id), ["s3"]);
  assert.equal(subtaskChip(parent, [parent, later, undated, doneSub]), "1/3");
});

test("a snoozed parent takes its sub-tasks along, and a dated sub-task brings its parent forward", () => {
  const snoozed = item({ id: "p1", deferUntil: t(20, 9) });
  const due = item({ id: "s1", parent: "p1", dueAt: t(14, 11) });
  let groups = buildView("all", [snoozed, due], now);
  assert.deepEqual(groups.map((g) => [g.id, g.items.map((i) => i.id)]), [["later", ["p1", "s1"]]]);

  const undated = item({ id: "p2", repo: "garden" });
  const soon = item({ id: "s2", repo: "garden", parent: "p2", dueAt: t(14, 11) });
  groups = buildView("today", [undated, soon], now);
  assert.deepEqual(groups.map((g) => [g.id, g.items.map((i) => i.id)]), [["today", ["p2", "s2"]]]);
});

test("closes go sub-tasks first, and a delete takes the open sub-tasks along", () => {
  const parent = item({ id: "p" });
  const a = item({ id: "a", parent: "p" });
  const b = item({ id: "b", parent: "p" });
  const done = item({ id: "c", parent: "p", status: "closed" });
  const all = [parent, a, b, done];
  assert.deepEqual(subtasksFirst([parent, a], all, false).map((i) => i.id), ["a", "p"]);
  assert.deepEqual(subtasksFirst([parent], all, true).map((i) => i.id), ["a", "b", "p"]);
});

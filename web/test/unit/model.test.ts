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
  openToday,
  personalRepoKeys,
  repoDetail,
  repoLabel,
  syncStatus,
  visibleRepos,
} from "../../src/model.ts";
import { snoozeOptions } from "../../src/snooze.ts";

// Wednesday 14 October 2026, 10:00 local time.
const now = new Date(2026, 9, 14, 10, 0);
const t = (d: number, h: number, m = 0) => new Date(2026, 9, d, h, m).toISOString();

const repos: Repo[] = [
  { bridge: "home", name: "personal", personal: true, optedIn: true },
  { bridge: "home", name: "garden", personal: false, optedIn: true },
  { bridge: "home", name: "attic", personal: false, optedIn: false },
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

test("Today view: past due, today, this evening, done today, in time order", () => {
  const items = [
    item({ title: "Evening", dueAt: t(14, 19) }),
    item({ title: "Late", dueAt: t(14, 9) }),
    item({ title: "Noon", dueAt: t(14, 12) }),
    item({ title: "Morning", dueAt: t(14, 11) }),
    item({ title: "Tomorrow", dueAt: t(15, 9) }),
    item({ title: "Closed", status: "closed", closedAt: t(14, 9) }),
    item({ title: "Undated" }),
  ];
  const groups = buildView("today", items, repos, now);
  assert.deepEqual(
    groups.map((g) => [g.label, g.items.map((i) => i.title)]),
    [
      ["Past due", ["Late"]],
      ["Today", ["Morning", "Noon"]],
      ["This evening", ["Evening"]],
      ["Done today", ["Closed"]],
    ],
  );
  assert.equal(openToday(items, now), 4);
});

test("Upcoming groups by tomorrow, this week, later", () => {
  const items = [item({ title: "A", dueAt: t(15, 9) }), item({ title: "B", dueAt: t(17, 9) }), item({ title: "C", dueAt: t(30, 9) })];
  const groups = buildView("upcoming", items, repos, now);
  assert.deepEqual(groups.map((g) => g.label), ["Tomorrow", "This week", "Later"]);
});

test("Everything adds No date; agents and list views filter", () => {
  const items = [item({ title: "Mine" }), item({ title: "Theirs", repo: "garden" })];
  assert.deepEqual(buildView("all", items, repos, now).map((g) => g.label), ["No date"]);
  assert.deepEqual(buildView("agents", items, repos, now)[0].items.map((i) => i.title), ["Theirs"]);
  assert.deepEqual(buildView("list:home/personal", items, repos, now)[0].items.map((i) => i.title), ["Mine"]);
});

test("date chips", () => {
  assert.deepEqual(dateChip(item({ dueAt: t(14, 9, 25) }), now), { text: "35m overdue", tone: "red", snoozed: false });
  assert.deepEqual(dateChip(item({ dueAt: t(14, 7) }), now), { text: "3h overdue", tone: "red", snoozed: false });
  assert.deepEqual(dateChip(item({ dueAt: t(12, 9) }), now), { text: "2d overdue", tone: "red", snoozed: false });
  assert.deepEqual(dateChip(item({ dueAt: t(14, 14, 30) }), now), { text: "14:30", tone: "green", snoozed: false });
  assert.deepEqual(dateChip(item({ dueAt: t(15, 9) }), now), { text: "Tomorrow 9:00", tone: "purple", snoozed: false });
  assert.deepEqual(dateChip(item({ dueAt: t(23, 9) }), now), { text: "Fri, Oct 23", tone: "purple", snoozed: false });
  assert.deepEqual(dateChip(item({ deferUntil: t(15, 9) }), now), { text: "Tomorrow 9:00", tone: "orange", snoozed: true });
  assert.equal(dateChip(item({}), now), null);
});

test("New is for agent items never opened; New reply needs a newer comment by someone else", () => {
  const personal = personalRepoKeys(repos);
  const fresh = item({ repo: "garden" });
  assert.equal(attention(fresh, personal, "alex"), "new");
  assert.equal(attention(item({}), personal, "alex"), null);

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
  assert.equal(attention(seen, personal, "alex"), "reply");
  assert.equal(agentNewCount([fresh, seen, item({})], repos, "alex"), 2);
});

test("sync status", () => {
  const online = [{ name: "home", createdAt: t(1, 9), lastSeen: t(14, 9, 59) }];
  const stale = [{ name: "home", createdAt: t(1, 9), lastSeen: t(14, 9, 50) }];
  assert.deepEqual(syncStatus({ unapplied: 0, bridges: online }, now), { tone: "green", text: "Synced" });
  assert.deepEqual(syncStatus({ unapplied: 2, bridges: online }, now), { tone: "orange", text: "2 waiting" });
  assert.deepEqual(syncStatus({ unapplied: 0, bridges: stale }, now), { tone: "orange", text: "Agent machine offline" });
  assert.deepEqual(syncStatus({ unapplied: 0, bridges: [] }, now), { tone: "orange", text: "Agent machine offline" });
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
    { bridge: "laptop", name: "personal", personal: true, optedIn: true },
  ];
  assert.equal(repoLabel(repos[0]), "garden");
  assert.equal(repoLabel(repos[4]), "personal");
  assert.equal(repoDetail(repos[3], repos), "");
  assert.equal(repoDetail(repos[1], repos), "work/garden");
  assert.equal(repoDetail(repos[0], repos), "projects/garden on laptop");
  assert.equal(repoDetail(repos[2], repos), "projects/garden on desk");
  assert.deepEqual(
    visibleRepos(repos).map((r) => `${r.bridge}:${r.name}`),
    ["laptop:personal", "desk:projects/garden", "laptop:projects/garden", "laptop:work/garden", "laptop:projects/kitchen"],
  );
});

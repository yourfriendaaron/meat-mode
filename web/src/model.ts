// View grouping and chip logic. Pure: every function takes the moment `now`.

import type { Bridge, Item, Repo, State } from "./types.ts";
import { addDays, at, fmtOverdue, fmtWhen, nextMonday, sameDay, startOfDay } from "./dates.ts";

export type ViewId = "today" | "upcoming" | "agents" | "all" | `list:${string}`;

export type GroupId =
  | "pastdue"
  | "today"
  | "evening"
  | "tomorrow"
  | "week"
  | "later"
  | "nodate"
  | "done";

export const GROUP_LABELS: Record<GroupId, string> = {
  pastdue: "Past due",
  today: "Today",
  evening: "This evening",
  tomorrow: "Tomorrow",
  week: "This week",
  later: "Later",
  nodate: "No date",
  done: "Done today",
};

const VIEW_GROUPS: Record<"today" | "upcoming" | "all", GroupId[]> = {
  today: ["pastdue", "today", "evening", "tomorrow", "week", "later", "nodate", "done"],
  upcoming: ["tomorrow", "week", "later"],
  all: ["pastdue", "today", "evening", "tomorrow", "week", "later", "nodate", "done"],
};

/** Groups the Completed, Deleted and search views add: a close day, the Deleted list, and search results. */
export type ExtraGroupId = `day-${string}` | "deleted" | "found-open" | "found-done" | "found-deleted";

export interface Group {
  id: GroupId | ExtraGroupId;
  label: string;
  items: Item[];
  /** Keys of the items shown as sub-tasks under the item before them. */
  nested?: Set<string>;
}

/** Hour from which a task counts as "this evening". */
export const EVENING_HOUR = 17;
/** Open tasks in a day above which the day-load bar warns. */
export const DAY_CAPACITY = 8;

export function repoKey(r: { bridge: string; repo?: string; name?: string }): string {
  return `${r.bridge}/${r.repo ?? r.name}`;
}

/** A repo's display name: its folder name, the last segment of its path. */
export function repoLabel(r: { name: string }): string {
  return r.name.slice(r.name.lastIndexOf("/") + 1);
}

/**
 * The secondary line for a repo whose label repeats among `all`: its path
 * when that tells it apart, and its machine when the same path is on several.
 * Empty when the label is unique.
 */
export function repoDetail(r: Repo, all: Repo[]): string {
  const label = repoLabel(r);
  const same = all.filter((x) => repoLabel(x) === label);
  if (same.length < 2) return "";
  const parts: string[] = [];
  if (r.name !== label) parts.push(r.name);
  if (same.some((x) => x.name === r.name && x.bridge !== r.bridge)) parts.push(`on ${r.bridge}`);
  return parts.join(" ");
}

export function isOpen(item: Item): boolean {
  return item.status !== "closed";
}

/** Close reason of a dismissed item: "Won't do" on an agent item, "Delete" on a personal one. */
export const DISMISSED = "Dismissed";

/** Today's agenda groups, which the Today view fills from every list. */
const AGENDA: GroupId[] = ["pastdue", "today", "evening", "done"];

/** The bridge name under which the hub keeps the personal list itself. */
export const HUB_BRIDGE = "hub";

/** Personal: on the list the hub keeps itself. Agent: from a repo on an agent machine. */
export type Kind = "personal" | "agent";

/**
 * The one rule for what an item, repo or action belongs to: the personal list
 * is the hub's own bridge, everything else came from an agent machine.
 */
export function kindOf(x: { bridge: string }): Kind {
  return x.bridge === HUB_BRIDGE ? "personal" : "agent";
}

export function isPersonal(x: { bridge: string }): boolean {
  return kindOf(x) === "personal";
}

export function isAgentItem(x: { bridge: string }): boolean {
  return kindOf(x) === "agent";
}

/**
 * What a list's tracker can apply (docs/trackers.md, On the wire): an action
 * kind, or "subtask" for an add under a parent.
 */
export type Capability = "done" | "reopen" | "snooze" | "dismiss" | "note" | "add" | "subtask" | "ask";

/** Whether a list's tracker can apply `kind`. A list without `actions`, such as the personal list or one from an older machine, takes every kind. */
export function repoCan(repo: Pick<Repo, "actions"> | undefined, kind: Capability): boolean {
  return !repo?.actions || repo.actions.includes(kind);
}

/**
 * The one rule for which actions the app offers on an item, on top of
 * personal versus agent: its list's tracker must list the kind. The hub
 * refuses the others.
 */
export function can(item: { bridge: string; repo: string }, kind: Capability, repos: Repo[]): boolean {
  return repoCan(
    repos.find((r) => repoKey(r) === repoKey(item)),
    kind,
  );
}

/** Deleted: a personal item closed as Dismissed. On an agent item the same close is Won't do. */
export function isDeleted(item: Item): boolean {
  return !isOpen(item) && item.closeReason === DISMISSED && isPersonal(item);
}

/** Won't do: an agent item closed as Dismissed. */
export function isWontDo(item: Item): boolean {
  return !isOpen(item) && item.closeReason === DISMISSED && isAgentItem(item);
}

function date(s: string | null | undefined): Date | null {
  if (!s) return null;
  const d = new Date(s);
  return Number.isNaN(d.getTime()) ? null : d;
}

export function isSnoozed(item: Item, now: Date): boolean {
  const d = date(item.deferUntil);
  return d !== null && d > now;
}

/**
 * The moment that places an item on the agenda: a future snooze wins,
 * then the due date, then an expired snooze (the item has resurfaced).
 */
export function whenOf(item: Item, now: Date): { at: Date | null; kind: "snoozed" | "due" | "resurfaced" | "none" } {
  const defer = date(item.deferUntil);
  if (defer && defer > now) return { at: defer, kind: "snoozed" };
  const due = date(item.dueAt);
  if (due) return { at: due, kind: "due" };
  if (defer) return { at: defer, kind: "resurfaced" };
  return { at: null, kind: "none" };
}

export function groupOf(item: Item, now: Date): GroupId | null {
  if (!isOpen(item)) {
    const closed = date(item.closedAt);
    return closed && sameDay(closed, now) ? "done" : null;
  }
  const w = whenOf(item, now);
  if (!w.at) return "nodate";
  const d = w.at;
  if (w.kind === "resurfaced") return d.getHours() >= EVENING_HOUR && sameDay(d, now) ? "evening" : "today";
  if (d < now) return "pastdue";
  if (sameDay(d, now)) return d.getHours() >= EVENING_HOUR ? "evening" : "today";
  if (sameDay(d, addDays(now, 1))) return "tomorrow";
  if (d < nextMonday(now)) return "week";
  return "later";
}

function sortKey(item: Item, now: Date): number {
  const w = whenOf(item, now);
  return w.at ? w.at.getTime() : Number.MAX_SAFE_INTEGER;
}

function compare(now: Date) {
  return (a: Item, b: Item) =>
    sortKey(a, now) - sortKey(b, now) || a.priority - b.priority || a.title.localeCompare(b.title);
}

// ---------- sub-tasks ----------

function itemKeyOf(i: Item, id: string): string {
  return `${repoKey(i)}/${id}`;
}

/** The item's sub-tasks among `items`: the items in its list that name it as their parent. */
export function subtasksOf(item: Item, items: Item[]): Item[] {
  return items.filter((i) => i.parent === item.id && repoKey(i) === repoKey(item) && !isDeleted(i));
}

/** The item's open sub-tasks, which keep it from closing. */
export function openSubtasks(item: Item, items: Item[]): Item[] {
  return subtasksOf(item, items).filter(isOpen);
}

/** The item's parent among `items`, when the app holds it. */
export function parentOf(item: Item, items: Item[]): Item | undefined {
  if (!item.parent) return undefined;
  const key = itemKeyOf(item, item.parent);
  return items.find((i) => i.key === key);
}

/** The parent's title: from the parent the app holds, else as the list's tracker gave it. */
export function parentTitle(item: Item, items: Item[]): string {
  return parentOf(item, items)?.title ?? item.parentTitle ?? "";
}

/** "1/3": done sub-tasks of all, or "" with none. */
export function subtaskChip(item: Item, items: Item[]): string {
  const subs = subtasksOf(item, items);
  if (!subs.length) return "";
  return `${subs.filter((i) => !isOpen(i)).length}/${subs.length}`;
}

/**
 * The parent a sub-task shows under: open, top-level and not deleted, with
 * the sub-task open too. Anything else shows on its own row.
 */
function nestsUnder(item: Item, byKey: Map<string, Item>): Item | undefined {
  if (!item.parent || !isOpen(item)) return undefined;
  const p = byKey.get(itemKeyOf(item, item.parent));
  return p && isOpen(p) && !p.parent ? p : undefined;
}

/**
 * Closes in an order the hub takes: every item's sub-tasks before it. With
 * `cascade`, each item's open sub-tasks not in `picked` join it, as Delete
 * and Won't do take a parent's sub-tasks with it.
 */
export function subtasksFirst(picked: Item[], items: Item[], cascade: boolean): Item[] {
  const out: Item[] = [];
  const seen = new Set<string>();
  const push = (i: Item) => {
    if (!seen.has(i.key)) {
      seen.add(i.key);
      out.push(i);
    }
  };
  const pickedKeys = new Set(picked.map((i) => i.key));
  for (const i of picked) {
    for (const s of openSubtasks(i, items)) if (cascade || pickedKeys.has(s.key)) push(s);
    push(i);
  }
  return out;
}

/**
 * The item a parent is placed by: its own future snooze wins, else the
 * earliest moment among it and its open sub-tasks, so a dated sub-task
 * brings its parent onto the agenda.
 */
function placedBy(item: Item, subs: Item[], now: Date): Item {
  const own = whenOf(item, now);
  if (own.kind === "snoozed" || !subs.length) return item;
  let earliest: Date | null = null;
  for (const s of subs) {
    const w = whenOf(s, now);
    if (w.at && (!earliest || w.at < earliest)) earliest = w.at;
  }
  if (!earliest || (own.at && own.at <= earliest)) return item;
  return { ...item, dueAt: earliest.toISOString(), deferUntil: null };
}

/**
 * Groups for a view, in display order, empty groups dropped. Today is today's
 * agenda from every list, then the rest of the personal list. A deleted
 * personal item shows nowhere. A sub-task shows under its open parent, which
 * takes it along wherever the parent is placed; a snooze on the parent snoozes
 * the lot.
 */
export function buildView(view: ViewId, items: Item[], now: Date): Group[] {
  const live = items.filter((i) => !isDeleted(i));
  const byKey = new Map(live.map((i) => [i.key, i]));
  const subs = new Map<string, Item[]>();
  for (const i of live) {
    const p = nestsUnder(i, byKey);
    if (!p) continue;
    const list = subs.get(p.key) ?? [];
    list.push(i);
    subs.set(p.key, list);
  }
  const placed = new Map<string, Item>();
  const place = (i: Item) => placed.get(i.key) ?? i;
  for (const i of live) if (subs.has(i.key)) placed.set(i.key, placedBy(i, subs.get(i.key)!, now));
  let pool = live.filter((i) => !nestsUnder(i, byKey));
  let order: GroupId[];
  if (view === "today") {
    order = VIEW_GROUPS.today;
    pool = pool.filter((i) => isPersonal(i) || AGENDA.includes(groupOf(place(i), now) ?? "nodate"));
  } else if (view === "upcoming" || view === "all") {
    order = VIEW_GROUPS[view];
  } else if (view === "agents") {
    order = VIEW_GROUPS.all;
    pool = pool.filter(isAgentItem);
  } else {
    const key = view.slice("list:".length);
    order = VIEW_GROUPS.all;
    pool = pool.filter((i) => repoKey(i) === key);
  }
  const buckets = new Map<GroupId, Item[]>();
  for (const item of pool) {
    const g = groupOf(place(item), now);
    if (!g || !order.includes(g)) continue;
    const list = buckets.get(g) ?? [];
    list.push(item);
    buckets.set(g, list);
  }
  const groups: Group[] = [];
  for (const id of order) {
    const list = buckets.get(id);
    if (!list?.length) continue;
    const byPlace = compare(now);
    list.sort(id === "done" ? (a, b) => (date(b.closedAt)?.getTime() ?? 0) - (date(a.closedAt)?.getTime() ?? 0) : (a, b) => byPlace(place(a), place(b)));
    const nested = new Set<string>();
    const rows: Item[] = [];
    for (const item of list) {
      rows.push(item);
      for (const s of (subs.get(item.key) ?? []).sort(byPlace)) {
        rows.push(s);
        nested.add(s.key);
      }
    }
    groups.push({ id, label: GROUP_LABELS[id], items: rows, ...(nested.size ? { nested } : {}) });
  }
  return groups;
}

/** Open tasks on today's agenda: past due, today and this evening. */
export function openToday(items: Item[], now: Date): number {
  return items.filter((i) => {
    const g = groupOf(i, now);
    return g === "pastdue" || g === "today" || g === "evening";
  }).length;
}

export interface DateChip {
  text: string;
  tone: "overdue" | "soon" | "date" | "snoozed";
  snoozed: boolean;
}

export function dateChip(item: Item, now: Date): DateChip | null {
  if (!isOpen(item)) return null;
  const w = whenOf(item, now);
  if (!w.at) return null;
  if (w.kind === "snoozed") return { text: fmtWhen(w.at, now), tone: "snoozed", snoozed: true };
  if (w.kind === "due" && w.at < now) return { text: fmtOverdue(w.at, now), tone: "overdue", snoozed: false };
  if (w.kind === "resurfaced") return null;
  if (sameDay(w.at, now)) return { text: fmtWhen(w.at, now), tone: "soon", snoozed: false };
  return { text: fmtWhen(w.at, now), tone: "date", snoozed: false };
}

/** The author name the hub writes for the app's own comments. */
export function appAuthor(user: string): string {
  return `${user}-app`;
}

/** "New": an agent item the user has never opened. */
export function isNew(item: Item): boolean {
  return isOpen(item) && isAgentItem(item) && !item.seenAt;
}

/** "New reply": a comment by someone else, newer than the last time the item was opened. */
export function hasNewReply(item: Item, user: string): boolean {
  const seen = date(item.seenAt);
  const own = appAuthor(user);
  return (item.comments ?? []).some((c) => {
    if (c.author === own) return false;
    if (!seen) return true;
    const t = date(c.createdAt);
    return t !== null && t > seen;
  });
}

export function isUnreadComment(createdAt: string, author: string, seenAt: string | null | undefined, user: string): boolean {
  if (author === appAuthor(user)) return false;
  const seen = date(seenAt);
  const t = date(createdAt);
  return !seen || (t !== null && t > seen);
}

export type Attention = "new" | "reply" | null;

export function attention(item: Item, user: string): Attention {
  if (!isOpen(item)) return null;
  if (isNew(item)) return "new";
  if (hasNewReply(item, user)) return "reply";
  return null;
}

/** Count for the pink "N new" badge on From agents. */
export function agentNewCount(items: Item[], user: string): number {
  return items.filter((i) => isAgentItem(i) && attention(i, user) !== null).length;
}

/** A bridge is online when it was seen within this window. */
export const ONLINE_WINDOW_MS = 2 * 60 * 1000;

export interface SyncStatus {
  tone: "synced" | "waiting";
  text: string;
}

export function bridgeOnline(b: Bridge, now: Date): boolean {
  const seen = date(b.lastSeen);
  return seen !== null && now.getTime() - seen.getTime() <= ONLINE_WINDOW_MS;
}

export function syncStatus(state: Pick<State, "unapplied" | "bridges">, now: Date): SyncStatus {
  const bridges = state.bridges ?? [];
  // With no machine paired there is only the personal list, which the hub keeps.
  if (!bridges.length) return { tone: "synced", text: "Synced" };
  const online = bridges.some((b) => bridgeOnline(b, now));
  if (!online) return { tone: "waiting", text: "Agent machine offline" };
  if (state.unapplied > 0) return { tone: "waiting", text: `${state.unapplied} waiting` };
  return { tone: "synced", text: "Synced" };
}

/** Repos shown as lists: personal and opted-in ones, personal first, then by name. */
export function visibleRepos(repos: Repo[]): Repo[] {
  return repos
    .filter((r) => isPersonal(r) || r.optedIn)
    .sort(
      (a, b) =>
        Number(isPersonal(b)) - Number(isPersonal(a)) ||
        repoLabel(a).localeCompare(repoLabel(b)) ||
        a.name.localeCompare(b.name) ||
        a.bridge.localeCompare(b.bridge),
    );
}

export const LIST_COLOURS = 6;

/** Colour index for each visible repo, stable by display order. */
export function repoColours(repos: Repo[]): Map<string, number> {
  const m = new Map<string, number>();
  visibleRepos(repos).forEach((r, i) => m.set(repoKey(r), i % LIST_COLOURS));
  return m;
}

/** Past-due items, which "Snooze all to tomorrow" moves. */
export function pastDue(items: Item[], now: Date): Item[] {
  return items.filter((i) => groupOf(i, now) === "pastdue");
}

export function tomorrowMorning(now: Date): Date {
  return at(startOfDay(addDays(now, 1)), 9);
}

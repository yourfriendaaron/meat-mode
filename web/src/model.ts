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

/**
 * Groups for a view, in display order, empty groups dropped. Today is today's
 * agenda from every list, then the rest of the personal list. A deleted
 * personal item shows nowhere.
 */
export function buildView(view: ViewId, items: Item[], now: Date): Group[] {
  let pool = items.filter((i) => !isDeleted(i));
  let order: GroupId[];
  if (view === "today") {
    order = VIEW_GROUPS.today;
    pool = pool.filter((i) => isPersonal(i) || AGENDA.includes(groupOf(i, now) ?? "nodate"));
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
    const g = groupOf(item, now);
    if (!g || !order.includes(g)) continue;
    const list = buckets.get(g) ?? [];
    list.push(item);
    buckets.set(g, list);
  }
  const groups: Group[] = [];
  for (const id of order) {
    const list = buckets.get(id);
    if (!list?.length) continue;
    list.sort(id === "done" ? (a, b) => (date(b.closedAt)?.getTime() ?? 0) - (date(a.closedAt)?.getTime() ?? 0) : compare(now));
    groups.push({ id, label: GROUP_LABELS[id], items: list });
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

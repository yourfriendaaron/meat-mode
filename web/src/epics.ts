// The Epics view: each repo's epic cards and top-level tasks, read-only
// (docs/api.md, Epics, "In the app"). Its rows are not items: they come from
// GET /api/epics and stay out of State, Today, search, counts and every
// action. A toolbar searches and filters them, Needs attention gathers what
// is Critical or Blocked, and Details show any row. The filtering, ordering,
// progress and HTML are pure; main.ts holds the state and wires the events.

import { fmtAgo, fmtDate, fmtWhenAt } from "./dates.ts";
import { esc } from "./html.ts";
import { ICON } from "./icons.ts";
import { listBadge, listColourClass } from "./identity.ts";
import { renderMarkdown } from "./markdown.ts";
import { repoLabel } from "./model.ts";
import type { EpicRow, Epics, Item, Repo, RepoEpics } from "./types.ts";

/** P0 to P4 by name. */
export const PRIORITY_NAMES = ["Critical", "High", "Medium", "Low", "Backlog"];

/**
 * The states a row shows, in the order the Filter menu lists them: the
 * tracker's five, and waiting, an open bead that waits on another.
 */
export const STATUSES = ["open", "in_progress", "blocked", "waiting", "deferred", "closed"] as const;

const STATUS_NAMES: Record<string, string> = {
  open: "Open",
  in_progress: "In progress",
  blocked: "Blocked",
  waiting: "Waiting",
  deferred: "Deferred",
  closed: "Closed",
};

/** What needs a hand first: blocked, in progress, open, waiting, deferred, then closed. */
const NEED_RANK: Record<string, number> = { blocked: 0, in_progress: 1, open: 2, waiting: 3, deferred: 4, closed: 5 };

export function priorityName(p: number): string {
  return PRIORITY_NAMES[clampPriority(p)] ?? "Medium";
}

function clampPriority(p: number): number {
  return Math.min(Math.max(Math.trunc(p), 0), 4);
}

/** A status in words; one the app does not know reads as Open, as the hub keeps it. */
export function statusName(s: string): string {
  return Object.hasOwn(STATUS_NAMES, s) ? STATUS_NAMES[s] : "Open";
}

/** The icon for a tracker type; the task icon for a type it does not know, inherited object keys included. */
export function typeIconSVG(t: string): string {
  return Object.hasOwn(TYPE_ICON, t) ? TYPE_ICON[t] : ICON.task;
}

/** A status's rank in the need-first order; an unknown one ranks as open. */
function needRank(s: string): number {
  return Object.hasOwn(NEED_RANK, s) ? NEED_RANK[s] : 2;
}

function knownStatus(s: string): string {
  return Object.hasOwn(STATUS_NAMES, s) ? s : "open";
}

/** Whether any repo shows its epics. */
export function epicsOn(repos: Repo[]): boolean {
  return repos.some((r) => r.epicsOn === true);
}

/**
 * Whether any repo's tracker can show epics, which is when the view is
 * offered: its Repos menu is where a repo's epics switch on and off.
 */
export function epicsOffered(repos: Repo[]): boolean {
  return repos.some((r) => r.epics === true);
}

/**
 * Compares two ids in natural order: runs of digits by their value, so
 * gd-1.9 comes before gd-1.10 and gd-2 before gd-10.
 */
export function naturalCompare(a: string, b: string): number {
  const pa = a.match(/\d+|\D+/g) ?? [];
  const pb = b.match(/\d+|\D+/g) ?? [];
  for (let i = 0; i < Math.min(pa.length, pb.length); i++) {
    const x = pa[i];
    const y = pb[i];
    if (x === y) continue;
    const dx = /^\d/.test(x);
    const dy = /^\d/.test(y);
    if (dx && dy) {
      const d = Number(x) - Number(y);
      if (d) return d;
      // Equal values written differently, such as 01 and 1: the shorter first.
      if (x.length !== y.length) return x.length - y.length;
    }
    return x < y ? -1 : 1;
  }
  return pa.length - pb.length;
}

/**
 * A row as the view draws it: a human row whose key is an item in State
 * takes that item's title and status (`item` true), and its Details offer
 * that item's task sheet.
 */
export interface ShownRow extends EpicRow {
  /** `<bridge>/<repo>/<id>`, the key a State item would have. */
  key: string;
  /** `<bridge>/<repo>`, the repo the row belongs to. */
  repoKey: string;
  item: boolean;
}

/**
 * The repo's rows, each with its key and, for a human row, its State item's
 * title and status. An open row that waits on another bead shows as waiting.
 */
export function shownRows(r: RepoEpics, find: (key: string) => Item | undefined): ShownRow[] {
  const repoKey = `${r.bridge}/${r.repo}`;
  return r.rows.map((row) => {
    const key = `${repoKey}/${row.id}`;
    const it = row.human ? find(key) : undefined;
    const shown: ShownRow = it ? { ...row, key, repoKey, title: it.title, status: it.status, item: true } : { ...row, key, repoKey, item: false };
    if (shown.status === "open" && (row.blockedBy?.length ?? 0) > 0) shown.status = "waiting";
    return shown;
  });
}

export interface Progress {
  done: number;
  total: number;
  /** Rounded down, so a card shows 100 only when every child is closed. */
  percent: number;
}

/** Closed direct children over all of them. */
export function progress(children: EpicRow[]): Progress {
  const total = children.length;
  const done = children.filter((c) => c.status === "closed").length;
  return { done, total, percent: total ? Math.floor((done * 100) / total) : 0 };
}

/** "86% 6/7 done", "0% 0/0 done" for a card with no children. */
export function progressText(p: Progress): string {
  return `${p.percent}% ${p.done}/${p.total} done`;
}

// ---------- the view's state ----------

export type EpicsSort = "priority" | "status" | "progress" | "updated";
export type EpicsMenu = "repo" | "filter" | "view";

export const SORTS: [EpicsSort, string][] = [
  ["priority", "Priority"],
  ["status", "Status"],
  ["progress", "Progress"],
  ["updated", "Last updated"],
];

/** What the toolbar and the folds hold. It lives in memory while the app is open, never in storage. */
export interface EpicsUI {
  /** The search box: title or id, across repos. */
  q: string;
  statuses: Set<string>;
  priorities: Set<number>;
  sort: EpicsSort;
  /** Repos folded to their heading. */
  folded: Set<string>;
  /** Cards opened to their children, by row key. */
  expanded: Set<string>;
  /** Cards shut while a search or filter opens them, by row key; cleared when the filters change. */
  shut: Set<string>;
  attnOpen: boolean;
  /** Needs attention shows every row, not only the first five. */
  attnAll: boolean;
  /** The toolbar menu that is open. */
  menu: EpicsMenu | null;
}

export function newEpicsUI(): EpicsUI {
  return {
    q: "",
    statuses: new Set(),
    priorities: new Set(),
    sort: "priority",
    folded: new Set(),
    expanded: new Set(),
    shut: new Set(),
    attnOpen: true,
    attnAll: false,
    menu: null,
  };
}

/** Whether a search, status or priority filter narrows the rows. */
export function filtering(ui: EpicsUI): boolean {
  return ui.q.trim() !== "" || ui.statuses.size > 0 || ui.priorities.size > 0;
}

/** Whether a row passes the search and the filters: OR within a group, AND across them. */
export function matches(r: EpicRow, ui: EpicsUI): boolean {
  const q = ui.q.trim().toLocaleLowerCase();
  if (q && !r.title.toLocaleLowerCase().includes(q) && !r.id.toLocaleLowerCase().includes(q)) return false;
  if (ui.statuses.size && !ui.statuses.has(knownStatus(r.status))) return false;
  if (ui.priorities.size && !ui.priorities.has(clampPriority(r.priority))) return false;
  return true;
}

function updated(r: EpicRow): number {
  const t = Date.parse(r.updatedAt);
  return Number.isNaN(t) ? 0 : t;
}

/** The default order: priority, then what needs a hand, then most recently updated. */
export function byPriority(a: EpicRow, b: EpicRow): number {
  return (
    a.priority - b.priority ||
    needRank(a.status) - needRank(b.status) ||
    updated(b) - updated(a) ||
    naturalCompare(a.id, b.id)
  );
}

/**
 * The order the View menu picks. `percent` gives a card's progress, or
 * undefined for a row that is no card or a card with no children, which
 * Progress puts after the cards that have some.
 */
export function sorter(sort: EpicsSort, percent: (r: EpicRow) => number | undefined): (a: EpicRow, b: EpicRow) => number {
  switch (sort) {
    case "status":
      return (a, b) => needRank(a.status) - needRank(b.status) || a.priority - b.priority || updated(b) - updated(a) || naturalCompare(a.id, b.id);
    case "progress":
      return (a, b) => (percent(a) ?? 101) - (percent(b) ?? 101) || byPriority(a, b);
    case "updated":
      return (a, b) => updated(b) - updated(a) || naturalCompare(a.id, b.id);
    default:
      return byPriority;
  }
}

/** The groups a repo's cards show under, in order, then its top-level tasks. */
export const EPIC_GROUPS = [
  ["blocked", "Blocked"],
  ["moving", "Moving"],
  ["ready", "Ready to start"],
  ["waiting", "Waiting on another epic"],
  ["deferred", "Deferred"],
  ["done", "Done"],
  ["task", "No epic"],
] as const;
export type EpicGroup = (typeof EPIC_GROUPS)[number][0];
const GROUP_RANK = new Map<string, number>(EPIC_GROUPS.map(([g], i) => [g, i]));

/**
 * The group a card shows under: its own state, and for an open card Moving
 * when a child is in progress, else Ready to start (the terminal viewer's
 * cardGroup).
 */
export function cardGroup(r: EpicRow, kids: EpicRow[]): EpicGroup {
  switch (r.status) {
    case "blocked":
      return "blocked";
    case "in_progress":
      return "moving";
    case "waiting":
      return "waiting";
    case "deferred":
      return "deferred";
    case "closed":
      return "done";
  }
  return kids.some((k) => k.status === "in_progress") ? "moving" : "ready";
}

function blockerMap(rows: EpicRow[]): Map<string, string[]> {
  return new Map(rows.filter((r) => r.blockedBy?.length).map((r) => [r.id, r.blockedBy ?? []]));
}

/**
 * The beads a row waits on that are not also waited on through another of
 * them: a bead waiting on gd-1, gd-2 and gd-3, where gd-3 waits on the
 * other two, starts "after gd-3". A cycle names them all.
 */
export function directBlockers(rows: EpicRow[], r: EpicRow, by = blockerMap(rows)): string[] {
  const on = r.blockedBy ?? [];
  if (on.length < 2) return on;
  const reach = (from: string): Set<string> => {
    const seen = new Set<string>();
    const stack = [...(by.get(from) ?? [])];
    while (stack.length) {
      const id = stack.pop() as string;
      if (seen.has(id)) continue;
      seen.add(id);
      stack.push(...(by.get(id) ?? []));
    }
    return seen;
  };
  const out = on.filter((b) => !on.some((o) => o !== b && reach(o).has(b)));
  return out.length ? out : on;
}

/** A row's step in the order of work: 1 when it waits on nothing, else one more than the latest step it waits on. */
export function tier(rows: EpicRow[], id: string, by = blockerMap(rows)): number {
  const memo = new Map<string, number>();
  const walk = (id: string, depth: number): number => {
    const known = memo.get(id);
    if (known !== undefined) return known;
    memo.set(id, 1);
    let t = 1;
    if (depth < rows.length) for (const b of by.get(id) ?? []) t = Math.max(t, walk(b, depth + 1) + 1);
    memo.set(id, t);
    return t;
  };
  return walk(id, 0);
}

/** The cards not closed that wait directly on id. */
export function unlocks(rows: EpicRow[], id: string): EpicRow[] {
  const by = blockerMap(rows);
  return rows.filter((r) => r.epic && r.id !== id && r.status !== "closed" && directBlockers(rows, r, by).includes(id));
}

/** One entry of a repo's list: an epic card or a top-level task. */
export interface Entry {
  row: ShownRow;
  card: boolean;
  /** How many cards it sits under: 0 at the top, 1 for a card nested in a top-level card. */
  depth: number;
  /** The group it shows under: its top-level card's, or "task" for a top-level task. */
  group: EpicGroup;
  /** Its own group, which a nested card shows as a status when it differs. */
  own: EpicGroup;
  /** Every direct child of a card, sorted, closed ones last, nested cards included. */
  children: ShownRow[];
  /**
   * The children that show when it opens: those that are no card (a nested
   * card is an entry of its own), all of them or those a search or filter
   * leaves.
   */
  shown: ShownRow[];
  progress: Progress;
  /** Whether its children show. */
  open: boolean;
}

/** One repo's list after the search and filters, and its counts. */
export interface Outline {
  entries: Entry[];
  epics: number;
  tasks: number;
  /** Rows not closed, whatever the filters. */
  open: number;
}

/**
 * Lays out a repo's rows as epic cards under their groups (EPIC_GROUPS),
 * sorted within each, then the top-level tasks. Waiting cards go in the
 * order they can start. A card nested in another follows its parent, one
 * level deeper and in its group, whether or not the parent is open; it
 * shows once, never also among its parent's tasks. A row whose parent
 * names no card is a top-level task, as the hub would have cleared that
 * parent. While a search or filter is on, a card shows when it or any child
 * matches, or when a card nested in it shows, with only the matching
 * children, opened.
 */
export function outline(rows: ShownRow[], ui: EpicsUI): Outline {
  const cardIds = new Set(rows.filter((r) => r.epic).map((r) => r.id));
  const kids = new Map<string, ShownRow[]>();
  for (const r of rows) {
    if (!r.parent || !cardIds.has(r.parent) || r.parent === r.id) continue;
    kids.set(r.parent, [...(kids.get(r.parent) ?? []), r]);
  }
  const prog = new Map([...cardIds].map((id) => [id, progress(kids.get(id) ?? [])]));
  const percent = (r: EpicRow) => {
    const p = r.epic ? prog.get(r.id) : undefined;
    return p && p.total ? p.percent : undefined;
  };
  const order = sorter(ui.sort, percent);
  const childOrder = (a: EpicRow, b: EpicRow) => Number(a.status === "closed") - Number(b.status === "closed") || order(a, b);
  const top = rows.filter((r) => !r.parent || !cardIds.has(r.parent) || r.parent === r.id).sort(order);
  const narrowed = filtering(ui);
  const entries: Entry[] = [];
  const groupOf = (r: ShownRow): EpicGroup => (r.epic ? cardGroup(r, kids.get(r.id) ?? []) : "task");
  const add = (row: ShownRow, depth: number, group: EpicGroup) => {
    const at = entries.length;
    const card = !!row.epic;
    const children = card ? [...(kids.get(row.id) ?? [])].sort(childOrder) : [];
    const tasks = children.filter((c) => !c.epic);
    const shown = narrowed ? tasks.filter((c) => matches(c, ui)) : tasks;
    const open = narrowed && shown.length ? !ui.shut.has(row.key) : ui.expanded.has(row.key);
    entries.push({ row, card, depth, group, own: groupOf(row), children, shown, progress: prog.get(row.id) ?? progress([]), open });
    if (depth < cardIds.size) for (const c of [...children].filter((c) => c.epic).sort(order)) add(c, depth + 1, group);
    if (narrowed && !matches(row, ui) && !shown.length && entries.length === at + 1) entries.pop();
  };
  const by = blockerMap(rows);
  const rank = (r: ShownRow) => GROUP_RANK.get(groupOf(r)) ?? 0;
  const step = (r: ShownRow) => (groupOf(r) === "waiting" ? tier(rows, r.id, by) : 0);
  for (const row of [...top].sort((a, b) => rank(a) - rank(b) || step(a) - step(b))) add(row, 0, groupOf(row));
  const epics = entries.filter((e) => e.card).length;
  return { entries, epics, tasks: entries.length - epics, open: rows.filter((r) => r.status !== "closed").length };
}

/** A row in Needs attention, with the repo it belongs to. */
export interface AttentionRow {
  row: ShownRow;
  repo: RepoEpics;
}

/** Every row not closed that is Critical or Blocked, across the repos, by priority. */
export function attention(data: Epics, ui: EpicsUI, find: (key: string) => Item | undefined): AttentionRow[] {
  const out: AttentionRow[] = [];
  for (const repo of data.repos ?? []) {
    for (const row of shownRows(repo, find)) {
      if (row.status !== "closed" && (row.priority <= 0 || row.status === "blocked") && matches(row, ui)) out.push({ row, repo });
    }
  }
  return out.sort((a, b) => byPriority(a.row, b.row));
}

/** Finds a row by key in loaded epics, for Details. */
export function findRow(data: Epics | null, key: string, find: (key: string) => Item | undefined): ShownRow | undefined {
  for (const r of data?.repos ?? []) {
    if (!key.startsWith(`${r.bridge}/${r.repo}/`)) continue;
    const row = shownRows(r, find).find((x) => x.key === key);
    if (row) return row;
  }
  return undefined;
}

/** The repo of loaded epics a row belongs to. */
function repoOf(data: Epics | null, repoKey: string): RepoEpics | undefined {
  return data?.repos?.find((r) => `${r.bridge}/${r.repo}` === repoKey);
}

/** The State repo a set of epics belongs to, or a stand-in when State does not hold it. */
export function stateRepo(r: { bridge: string; repo: string }, repos: Repo[]): Repo {
  return repos.find((x) => x.bridge === r.bridge && x.name === r.repo) ?? { bridge: r.bridge, name: r.repo, personal: false, optedIn: false };
}

// ---------- HTML ----------

export interface EpicsView {
  data: Epics | null;
  /** Why the last load failed, or "". */
  error: string;
  ui: EpicsUI;
  find: (key: string) => Item | undefined;
  /** State's repos, for each repo's mark. */
  repos: Repo[];
  /** The row whose Details show, by key. */
  open: string | null;
  now: Date;
}

/**
 * A row's state as the terminal viewer writes it: "Waiting on gd-3 +2"
 * names the first bead it waits on, "Deferred to Nov 24" its wake date.
 */
export function statusLabel(r: Pick<EpicRow, "status" | "blockedBy" | "deferUntil">, now: Date): string {
  const name = statusName(r.status);
  const on = r.blockedBy ?? [];
  if (r.status === "waiting" && on.length) return `${name} on ${on[0]}${on.length > 1 ? ` +${on.length - 1}` : ""}`;
  const wake = r.deferUntil ? new Date(r.deferUntil) : null;
  if (r.status === "deferred" && wake && !Number.isNaN(wake.getTime())) return `${name} to ${fmtDate(wake, now)}`;
  return name;
}

/** A status with its dot, in words; with a row, its wait or wake date too. */
function statusPill(s: string, r?: EpicRow, now?: Date): string {
  const k = knownStatus(s);
  const label = r && now ? statusLabel({ ...r, status: s }, now) : statusName(s);
  return `<span class="ep-st ep-s-${k}"><span class="ep-sd" aria-hidden="true"></span>${esc(label)}</span>`;
}

/** "2 comments", or nothing. */
function notesPill(r: EpicRow): string {
  const n = r.commentCount ?? 0;
  return n > 0 ? `<span class="ep-notes">${n === 1 ? "1 comment" : `${n} comments`}</span>` : "";
}

/** The order and words of a card's children by state, in its bar and counts. */
const CHILD_STATES = [
  ["closed", "done"],
  ["in_progress", "in progress"],
  ["blocked", "blocked"],
  ["waiting", "waiting"],
  ["open", "open"],
  ["deferred", "deferred"],
] as const;

function countStates(children: EpicRow[]): Map<string, number> {
  const n = new Map<string, number>();
  for (const c of children) {
    const k = knownStatus(c.status);
    n.set(k, (n.get(k) ?? 0) + 1);
  }
  return n;
}

/** A card's children by state, each its dot and count, done last: the terminal viewer's "◒2 ⊘1 ✓4". */
function holdsHTML(children: EpicRow[]): string {
  const n = countStates(children);
  const order = [...CHILD_STATES.slice(1), CHILD_STATES[0]];
  return order
    .filter(([s]) => n.get(s))
    .map(([s, words]) => `<span class="ep-hold ep-s-${s}" title="${n.get(s)} ${words}"><span class="ep-sd" aria-hidden="true"></span>${n.get(s)}<span class="sr"> ${words}</span></span>`)
    .join("");
}

/** A priority in words: Critical red and High yellow, the rest plain. */
function priorityPill(p: number): string {
  return `<span class="ep-pri ep-p${clampPriority(p)}">${esc(priorityName(p))}</span>`;
}

const TYPE_ICON: Record<string, string> = {
  epic: ICON.epic,
  bug: ICON.bug,
  feature: ICON.feature,
  chore: ICON.chore,
};

function typeName(r: EpicRow): string {
  return (r.type ?? "").toLowerCase() || (r.epic ? "epic" : "task");
}

/** A bead's type as an icon, with the type in words for screen readers and on hover. */
function typeIcon(r: EpicRow): string {
  const t = typeName(r);
  return `<span class="ep-type" title="${esc(t)}">${typeIconSVG(t)}<span class="sr">${esc(t)}</span></span>`;
}

function copyButton(id: string): string {
  return `<button class="icon-btn ep-copy" data-act="epic-copy" data-id="${esc(id)}" aria-label="Copy id ${esc(id)}" title="Copy id">${ICON.copy}</button>`;
}

/** A card's bar: a run for each state of its children, done first in green, then in progress, blocked, waiting, open and deferred. */
function bar(children: EpicRow[]): string {
  const n = countStates(children);
  const total = children.length;
  const runs = total
    ? CHILD_STATES.filter(([s]) => n.get(s)).map(([s]) => `<span class="ep-fill ep-s-${s}" data-pct="${(((n.get(s) ?? 0) * 100) / total).toFixed(2)}"></span>`)
    : [];
  return `<span class="ep-bar" aria-hidden="true">${runs.join("")}</span>`;
}

/** "after gd-3": the bead a waiting card starts after. */
function afterHTML(rows: EpicRow[], r: EpicRow): string {
  if (r.status !== "waiting") return "";
  return `<span class="ep-after">after ${esc(directBlockers(rows, r).join(", "))}</span>`;
}

const ALSO_YOURS = '<span class="ep-human">Also your task</span>';

/**
 * A row's one line: type, id and title as the button that opens its
 * Details, then its comment count, status, priority, assignee and Copy id. A card's
 * group says its status, so its line does not.
 */
function lineHTML(r: ShownRow, now: Date, card = false): string {
  const status = card ? "" : statusPill(r.status, r, now);
  const meta = `${r.item ? ALSO_YOURS : ""}${notesPill(r)}${status}${priorityPill(r.priority)}${r.assignee ? `<span class="ep-who">${esc(r.assignee)}</span>` : ""}`;
  return `<div class="ep-line"><button class="ep-main" data-act="ep-open" data-key="${esc(r.key)}">${typeIcon(r)}<code class="ep-id">${esc(r.id)}</code><span class="ep-title">${esc(r.title)}</span></button><span class="ep-meta">${meta}</span>${copyButton(r.id)}</div>`;
}

function rowClass(r: ShownRow, open: string | null): string {
  return `${r.status === "closed" ? " is-done" : ""}${open === r.key ? " is-open" : ""}`;
}

function entryHTML(e: Entry, v: EpicsView, n: string, rows: EpicRow[]): string {
  const r = e.row;
  let extra = "";
  if (e.card) {
    const kidsId = `ep-kids-${n}`;
    const total = e.children.filter((c) => !c.epic).length;
    const part = e.shown.length < total ? ` <span class="ep-part">(${e.shown.length} shown)</span>` : "";
    const toggle = `<button class="ep-kids-btn" data-act="ep-toggle" data-key="${esc(r.key)}" aria-expanded="${e.open}"${e.open ? ` aria-controls="${kidsId}"` : ""}>${total} ${total === 1 ? "task" : "tasks"}${part}<span class="sr"> of ${esc(r.id)}</span>${ICON.right}</button>`;
    const kids = e.shown.length
      ? `<ul class="ep-kids" id="${kidsId}" aria-label="Tasks of ${esc(r.id)}">${e.shown.map((k) => `<li class="ep-row${rowClass(k, v.open)}">${lineHTML(k, v.now)}</li>`).join("")}</ul>`
      : `<p class="muted small ep-none ep-kids" id="${kidsId}">${total ? "No tasks match the filters." : "No tasks yet."}</p>`;
    const own = e.own !== e.group ? statusPill(r.status, r, v.now) : "";
    extra = `<div class="ep-progress">${bar(e.children)}<span class="ep-ptext">${esc(progressText(e.progress))}</span>${own}${afterHTML(rows, r)}${holdsHTML(e.children)}${toggle}</div>${e.open ? kids : ""}`;
  }
  const depth = e.depth ? ` data-depth="${Math.min(e.depth, 3)}"` : "";
  return `<li class="ep-item${e.card ? " is-card" : ""}${rowClass(r, v.open)}" data-ep="${esc(r.key)}"${depth}>${lineHTML(r, v.now, e.card)}${extra}</li>`;
}

/** A repo's name, with its path and machine where the name repeats. */
function repoName(r: RepoEpics, all: RepoEpics[]): { label: string; where: string } {
  const label = repoLabel({ name: r.repo });
  const same = all.filter((x) => repoLabel({ name: x.repo }) === label).length > 1;
  return { label, where: same ? `${r.repo} on ${r.bridge}` : "" };
}

function count(n: number, one: string, many: string): string {
  return `${n} ${n === 1 ? one : many}`;
}

function repoHTML(r: RepoEpics, i: number, v: EpicsView, all: RepoEpics[]): string {
  const key = `${r.bridge}/${r.repo}`;
  const state = stateRepo(r, v.repos);
  const o = outline(shownRows(r, v.find), v.ui);
  const folded = v.ui.folded.has(key);
  const h = `ep-h-${i}`;
  const bodyId = `ep-body-${i}`;
  const { label, where } = repoName(r, all);
  const synced = new Date(r.syncedAt);
  const sum = [
    where,
    `${count(o.epics, "epic", "epics")}, ${count(o.tasks, "task", "tasks")}, ${o.open} open`,
    Number.isNaN(synced.getTime()) ? "" : `Synced ${fmtAgo(synced, v.now)}`,
  ]
    .filter(Boolean)
    .join(" · ");
  let body = "";
  if (!folded) {
    const rows = shownRows(r, v.find);
    const groupLabel = new Map<string, string>(EPIC_GROUPS);
    const items = o.entries.map((e, j) => {
      const head =
        j === 0 || o.entries[j - 1].group !== e.group
          ? `<li class="ep-group ep-g-${e.group}"><h3>${esc(groupLabel.get(e.group) ?? "")} <span class="group-count">${o.entries.filter((x) => x.group === e.group).length}</span></h3></li>`
          : "";
      return head + entryHTML(e, v, `${i}-${j}`, rows);
    });
    body = o.entries.length
      ? `<ul class="ep-list" id="${bodyId}">${items.join("")}</ul>`
      : `<p class="muted ep-none" id="${bodyId}">${filtering(v.ui) ? "Nothing here matches." : "Nothing open."}</p>`;
  }
  const fold = `<button class="icon-btn ep-fold" data-act="ep-fold" data-v="${esc(key)}" aria-expanded="${!folded}"${folded ? "" : ` aria-controls="${bodyId}"`} aria-label="${folded ? "Show" : "Hide"} ${esc(label)}">${ICON.right}</button>`;
  return `<section class="ep-repo ${listColourClass(state, v.repos)}" aria-labelledby="${h}"><div class="ep-repo-head">${listBadge(state, v.repos, "l")}<div class="ep-repo-name"><h2 id="${h}">${esc(label)}</h2><span class="ep-sum">${esc(sum)}</span></div>${fold}</div>${body}</section>`;
}

const ATTENTION_FIRST = 5;

function attentionHTML(v: EpicsView, data: Epics): string {
  const rows = attention(data, v.ui, v.find);
  if (!rows.length) return "";
  const shown = v.ui.attnAll ? rows : rows.slice(0, ATTENTION_FIRST);
  const more = rows.length - shown.length;
  const list = shown
    .map(({ row, repo }) => {
      const badge = listBadge(stateRepo(repo, v.repos), v.repos, "s");
      return `<li><button class="ep-attn-row${v.open === row.key ? " is-open" : ""}" data-act="ep-open" data-key="${esc(row.key)}">${badge}${statusPill(row.status)}<span class="ep-attn-t">${esc(row.title)}</span>${priorityPill(row.priority)}</button></li>`;
    })
    .join("");
  const moreBtn = more > 0 ? `<button class="link-btn ep-attn-more" data-act="ep-attn-all">Show ${more} more</button>` : v.ui.attnAll && rows.length > ATTENTION_FIRST ? '<button class="link-btn ep-attn-more" data-act="ep-attn-all">Show fewer</button>' : "";
  const open = v.ui.attnOpen;
  return `<section class="ep-attn" aria-labelledby="ep-attn-h"><div class="ep-attn-head"><h2 id="ep-attn-h">Needs attention <span class="group-count">${rows.length}</span></h2><span class="muted small">Critical or blocked</span><button class="icon-btn ep-attn-fold" data-act="ep-attn-fold" aria-expanded="${open}"${open ? ' aria-controls="ep-attn-rows"' : ""} aria-label="${open ? "Hide" : "Show"} Needs attention">${ICON.right}</button></div>${
    open ? `<ul class="ep-attn-rows" id="ep-attn-rows">${list}</ul>${moreBtn}` : ""
  }</section>`;
}

/** The whole view under the header: Needs attention, then each repo. */
export function epicsHTML(v: EpicsView): string {
  if (!v.data) return `<p class="empty">${v.error ? `Could not load epics: ${esc(v.error)}` : "Loading"}</p>`;
  const all = v.data.repos ?? [];
  const failed = v.error ? `<p class="ep-error" role="status">Could not load epics again: ${esc(v.error)}</p>` : "";
  if (!all.length) {
    const none = epicsOn(v.repos) ? "No epics yet. They show here once the machine sends them." : "No repo shows its epics yet. Pick the repos to show in the Repos menu.";
    return `${failed}<p class="empty">${none}</p>`;
  }
  const sections: string[] = [];
  all.forEach((r, i) => {
    // While a search or filter is on, a repo with nothing that matches drops out.
    if (filtering(v.ui) && !outline(shownRows(r, v.find), v.ui).entries.length) return;
    sections.push(repoHTML(r, i, v, all));
  });
  return failed + attentionHTML(v, v.data) + (sections.join("") || '<p class="empty">No epics or tasks match the filters.</p>');
}

// ---------- the toolbar ----------

/** One item of a toolbar menu. Its id keeps keyboard focus on it across a render. */
function menuItem(menu: EpicsMenu, i: number, act: string, v: string, label: string, checked: boolean, role: string, extra = ""): string {
  return `<button class="more-item" id="ep-mi-${menu}-${i}" role="${role}" aria-checked="${checked}" data-act="${act}" data-v="${esc(v)}" tabindex="-1"><span class="ep-ck">${checked ? ICON.check : ""}</span>${label}${extra}</button>`;
}

function menuWrap(ui: EpicsUI, menu: EpicsMenu, label: string, button: string, items: string): string {
  const open = ui.menu === menu;
  // On a phone the menu opens as a sheet, headed by its name and a Close button outside the menu itself.
  const pop = open
    ? `<div class="more-menu ep-menu" id="ep-mm-${menu}"><div class="ep-sheet-head"><span>${esc(label)}</span><button class="icon-btn" data-act="ep-menu-close" aria-label="Close">${ICON.close}</button></div><div role="menu" aria-labelledby="ep-mb-${menu}">${items}</div></div>`
    : "";
  return `<div class="ep-menu-wrap"><button class="btn btn-small ep-menu-btn" id="ep-mb-${menu}" data-act="ep-menu" data-v="${menu}" aria-haspopup="menu" aria-expanded="${open}"${open ? ` aria-controls="ep-mm-${menu}"` : ""}>${button}${ICON.right}</button>${pop}</div>`;
}

export interface ToolbarView {
  data: Epics | null;
  ui: EpicsUI;
  find: (key: string) => Item | undefined;
  repos: Repo[];
}

/**
 * The Repos, Filter and View menus, which sit in the header after the search
 * box. The Repos menu lists every repo whose tracker can show epics, checked
 * where they show: a pick switches the repo's epics on or off at its machine
 * and reaches main.ts as "ep-show-repo".
 */
export function menusHTML(v: ToolbarView): string {
  const ui = v.ui;
  // A linked list is one pick, its shown copy's; the pick switches every copy (main.ts, switchEpics).
  const can = v.repos.filter((r) => r.epics === true && !r.standby).sort((a, b) => a.bridge.localeCompare(b.bridge) || a.name.localeCompare(b.name));
  const shown = can.filter((r) => r.epicsOn === true).length;
  const repoBtn = `<span>Repos${shown ? ` <span class="ep-n">${shown}</span>` : ""}</span>`;
  let i = 0;
  const repoItems = can.length
    ? `<div role="group" aria-label="Show epics from"><div class="more-h" aria-hidden="true">Show epics from</div>${can
        .map((r) => {
          const key = `${r.bridge}/${r.name}`;
          const label = repoLabel(r);
          const same = can.filter((x) => repoLabel(x) === label).length > 1;
          const waiting = r.epicsPending ? `<span class="ep-n" role="status">Waiting for ${esc(r.bridge)}</span>` : "";
          return menuItem("repo", i++, "ep-show-repo", key, `${listBadge(r, v.repos)}<span class="ep-grow">${esc(same ? `${label} (${r.name} on ${r.bridge})` : label)}</span>`, r.epicsOn === true, "menuitemcheckbox", waiting);
        })
        .join("")}</div>`
    : '<p class="more-h">No repo here can show epics.</p>';
  i = 0;
  const nf = ui.statuses.size + ui.priorities.size;
  const filterItems =
    `<div role="group" aria-label="Status"><div class="more-h" aria-hidden="true">Status</div>${STATUSES.map((s) => menuItem("filter", i++, "ep-f-status", s, `<span class="ep-st ep-s-${s}"><span class="ep-sd" aria-hidden="true"></span></span><span class="ep-grow">${esc(statusName(s))}</span>`, ui.statuses.has(s), "menuitemcheckbox")).join("")}</div>` +
    '<div class="more-sep" role="separator"></div>' +
    `<div role="group" aria-label="Priority"><div class="more-h" aria-hidden="true">Priority</div>${PRIORITY_NAMES.map((name, p) => menuItem("filter", i++, "ep-f-pri", String(p), `<span class="ep-grow">${esc(name)}</span>`, ui.priorities.has(p), "menuitemcheckbox", `<span class="ep-n" aria-hidden="true">P${p}</span>`)).join("")}</div>`;
  i = 0;
  const viewItems =
    `<div role="group" aria-label="Sort by"><div class="more-h" aria-hidden="true">Sort by</div>${SORTS.map(([s, name]) => menuItem("view", i++, "ep-sort", s, `<span class="ep-grow">${esc(name)}</span>`, ui.sort === s, "menuitemradio")).join("")}</div>` +
    '<div class="more-sep" role="separator"></div>' +
    `<button class="more-item" id="ep-mi-view-${i++}" role="menuitem" data-act="ep-expand-all" tabindex="-1"><span class="ep-ck"></span>Expand all</button>` +
    `<button class="more-item" id="ep-mi-view-${i++}" role="menuitem" data-act="ep-collapse-all" tabindex="-1"><span class="ep-ck"></span>Collapse all</button>`;
  return (
    menuWrap(ui, "repo", "Repos", repoBtn, repoItems) +
    menuWrap(ui, "filter", "Filter", `<span>Filter${nf ? ` <span class="ep-n">${nf}</span>` : ""}</span>`, filterItems) +
    menuWrap(ui, "view", "View", "<span>View</span>", viewItems)
  );
}

/** The active filters, one chip each with its own remove button, then Clear; "" with none. */
export function chipsHTML(v: ToolbarView): string {
  const ui = v.ui;
  const x = ICON.close;
  const chips = [
    ...STATUSES.filter((s) => ui.statuses.has(s)).map(
      (s) => `<button class="ep-achip" data-act="ep-f-status" data-v="${s}"><span class="sr">Remove filter: </span><span class="ep-st ep-s-${s}"><span class="ep-sd" aria-hidden="true"></span></span>${esc(statusName(s))}${x}</button>`,
    ),
    ...[...ui.priorities]
      .sort()
      .map((p) => `<button class="ep-achip" data-act="ep-f-pri" data-v="${p}"><span class="sr">Remove filter: </span>${esc(priorityName(p))}${x}</button>`),
  ];
  if (!chips.length) return "";
  return `<div class="ep-achips" role="group" aria-label="Active filters">${chips.join("")}<button class="link-btn" data-act="ep-clear">Clear</button></div>`;
}

/** Turns a value on or off in a set. */
export function toggle<T>(set: Set<T>, v: T): void {
  if (set.has(v)) set.delete(v);
  else set.add(v);
}

/** Clears the search and every filter. */
export function clearFilters(ui: EpicsUI): void {
  ui.q = "";
  ui.statuses.clear();
  ui.priorities.clear();
  ui.shut.clear();
}

/** Opens every card and unfolds every repo. */
export function expandAll(ui: EpicsUI, data: Epics | null): void {
  ui.folded.clear();
  ui.shut.clear();
  for (const r of data?.repos ?? []) for (const row of r.rows) if (row.epic) ui.expanded.add(`${r.bridge}/${r.repo}/${row.id}`);
}

/** Shuts every card and folds every repo to its heading. */
export function collapseAll(ui: EpicsUI, data: Epics | null): void {
  ui.expanded.clear();
  for (const r of data?.repos ?? []) {
    ui.folded.add(`${r.bridge}/${r.repo}`);
    for (const row of r.rows) if (row.epic) ui.shut.add(`${r.bridge}/${r.repo}/${row.id}`);
  }
}

// ---------- Details ----------

export interface DetailView {
  data: Epics | null;
  find: (key: string) => Item | undefined;
  repos: Repo[];
  now: Date;
}

function when(at: string | null | undefined, now: Date): string {
  const d = at ? new Date(at) : null;
  if (!d || Number.isNaN(d.getTime())) return "";
  return `${esc(fmtAgo(d, now))} <span class="muted small">${esc(fmtWhenAt(d, now))}</span>`;
}

/** Any row's Details, read-only, in the task sheet's frame: the side panel on a wide screen, a sheet on a phone. */
export function epicDetailHTML(r: ShownRow, v: DetailView): string {
  const repo = repoOf(v.data, r.repoKey);
  const all = v.data?.repos ?? [];
  const rows = repo ? shownRows(repo, v.find) : [r];
  const state = repo ? stateRepo(repo, v.repos) : { bridge: "", name: r.repoKey, personal: false, optedIn: false };
  const name = repo ? repoName(repo, all) : { label: repoLabel({ name: r.repoKey }), where: "" };
  const cards = new Set(rows.filter((x) => x.epic).map((x) => x.id));
  const parent = r.parent && cards.has(r.parent) && r.parent !== r.id ? rows.find((x) => x.id === r.parent) : undefined;
  const kids = r.epic
    ? rows
        .filter((x) => x.parent === r.id && x.id !== r.id)
        .sort((a, b) => Number(a.status === "closed") - Number(b.status === "closed") || byPriority(a, b))
    : [];
  const facts: [string, string][] = [
    ["List", `${listBadge(state, v.repos)}<span>${esc(name.label)}</span>${name.where ? `<span class="muted small">${esc(name.where)}</span>` : ""}`],
    ["Id", `<code class="bead-id">${esc(r.id)}</code>${copyButton(r.id)}`],
    ["Status", statusPill(r.status, r, v.now)],
    ["Priority", `${priorityPill(r.priority)} <span class="muted small">P${clampPriority(r.priority)}</span>`],
    ["Type", `${typeIcon(r)}<span>${esc(typeName(r))}</span>`],
    ["Assignee", r.assignee ? esc(r.assignee) : '<span class="muted">Nobody</span>'],
  ];
  if (r.epic) {
    const p = progress(kids);
    facts.push(["Progress", `<span class="ep-progress">${bar(kids)}<span class="ep-ptext">${esc(progressText(p))}</span>${holdsHTML(kids)}</span>`]);
  }
  const waits = r.status === "waiting" ? (r.blockedBy ?? []) : [];
  if (waits.length) {
    const links = waits.map((id) => {
      const b = rows.find((x) => x.id === id);
      return b
        ? `<button class="ep-link ep-wait" data-act="ep-open" data-key="${esc(b.key)}">${statusPill(b.status)}<span>${esc(b.title)}</span><code class="ep-id">${esc(id)}</code></button>`
        : `<code class="ep-id">${esc(id)}</code>`;
    });
    facts.push(["Waiting on", `<span class="ep-waits">${links.join("")}</span>`]);
    const after = directBlockers(rows, r);
    if (after.length < waits.length) facts.push(["Starts after", after.map((id) => `<code class="ep-id">${esc(id)}</code>`).join(" ")]);
  }
  const next = r.epic && r.status !== "closed" ? unlocks(rows, r.id) : [];
  if (next.length) {
    const links = next.map((b) => `<button class="ep-link ep-wait" data-act="ep-open" data-key="${esc((b as ShownRow).key)}">${statusPill(b.status)}<span>${esc(b.title)}</span><code class="ep-id">${esc(b.id)}</code></button>`);
    facts.push(["Unlocks", `<span class="ep-waits">${links.join("")}</span>`]);
  }
  if ((r.commentCount ?? 0) > 0) facts.push(["Comments", esc(String(r.commentCount))]);
  if (parent) facts.push(["Epic", `<button class="ep-link" data-act="ep-open" data-key="${esc(parent.key)}">${esc(parent.title)}</button>`]);
  const up = when(r.updatedAt, v.now);
  if (up) facts.push(["Updated", up]);
  const closed = when(r.closedAt, v.now);
  if (closed) facts.push(["Closed", closed]);
  const human = r.item
    ? `<div class="why ep-yours"><div class="why-label">Also your task</div><p>This task is on your own list too, where you can mark it done, snooze it or add a note.</p><button class="btn btn-primary btn-small" data-act="open" data-key="${esc(r.key)}">Open task</button></div>`
    : "";
  const desc = r.description?.trim() ? `<div class="md">${renderMarkdown(r.description)}</div>` : '<p class="muted small">No description.</p>';
  const notes = r.notes?.trim()
    ? `<section class="d-section" aria-labelledby="ep-d-notes"><h3 id="ep-d-notes">Notes</h3><div class="md">${renderMarkdown(r.notes)}</div></section>\n`
    : "";
  const kidsHTML = r.epic
    ? `<section class="d-section" aria-labelledby="ep-d-kids"><h3 id="ep-d-kids">Tasks <span class="group-count">${kids.length}</span></h3>${
        kids.length
          ? `<ul class="ep-d-kids">${kids.map((k) => `<li><button class="ep-d-kid${k.status === "closed" ? " is-done" : ""}" data-act="ep-open" data-key="${esc(k.key)}">${statusPill(k.status, k, v.now)}<span class="ep-title">${esc(k.title)}</span><code class="ep-id">${esc(k.id)}</code></button></li>`).join("")}</ul>`
          : '<p class="muted small">No tasks yet.</p>'
      }</section>`
    : "";
  return `<div class="detail-inner" role="region" aria-labelledby="d-title">
<div class="grip" aria-hidden="true"></div>
<div class="d-head"><span class="ep-mark">${typeIconSVG(typeName(r))}</span><h2 id="d-title" class="d-title">${esc(r.title)}</h2><button class="icon-btn" data-act="close-detail" aria-label="Close details">${ICON.close}</button></div>
${human}
<dl class="ep-facts">${facts.map(([k, val]) => `<dt>${k}</dt><dd>${val}</dd>`).join("")}</dl>
<section class="d-section" aria-labelledby="ep-d-desc"><h3 id="ep-d-desc">Description</h3>${desc}</section>
${notes}${kidsHTML}
</div>`;
}

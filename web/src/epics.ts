// The Epics view: each repo's epic cards with their progress and children,
// and its other open tasks, read-only (docs/api.md, Epics). Its rows are not
// items: they come from GET /api/epics and stay out of State, Today, search,
// counts and every action. The ordering, progress and HTML are pure; main.ts
// holds the state and wires the events.

import { fmtAgo } from "./dates.ts";
import { esc } from "./html.ts";
import { ICON } from "./icons.ts";
import { renderMarkdown } from "./markdown.ts";
import { repoLabel } from "./model.ts";
import type { EpicRow, Epics, Item, Repo, RepoEpics } from "./types.ts";

/** P0 to P4 by name. */
export const PRIORITY_NAMES = ["Critical", "High", "Medium", "Low", "Backlog"];

const STATUS_NAMES: Record<string, string> = {
  open: "Open",
  in_progress: "In progress",
  blocked: "Blocked",
  deferred: "Deferred",
  closed: "Closed",
};

/** Where a status sorts among cards and tasks: in progress first, closed last. */
const STATUS_RANK: Record<string, number> = { in_progress: 0, open: 1, blocked: 2, deferred: 3, closed: 4 };

export function priorityName(p: number): string {
  return PRIORITY_NAMES[Math.min(Math.max(Math.trunc(p), 0), 4)] ?? "Medium";
}

/** A status in words; one the app does not know reads as Open, as the hub keeps it. */
export function statusName(s: string): string {
  return STATUS_NAMES[s] ?? "Open";
}

/** Whether any repo shows its epics, which is when the view is offered. */
export function epicsOn(repos: Repo[]): boolean {
  return repos.some((r) => r.epicsOn === true);
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
 * takes that item's title and status, and opens it (`item` true).
 */
export interface ShownRow extends EpicRow {
  /** `<bridge>/<repo>/<id>`, the key a State item would have. */
  key: string;
  item: boolean;
}

/** The repo's rows, each with its key and, for a human row, its State item's title and status. */
export function shownRows(r: RepoEpics, find: (key: string) => Item | undefined): ShownRow[] {
  return r.rows.map((row) => {
    const key = `${r.bridge}/${r.repo}/${row.id}`;
    const it = row.human ? find(key) : undefined;
    return it ? { ...row, key, title: it.title, status: it.status, item: true } : { ...row, key, item: false };
  });
}

/** Cards and tasks: by status, then priority, then id. */
function byStatus(a: EpicRow, b: EpicRow): number {
  return (STATUS_RANK[a.status] ?? 1) - (STATUS_RANK[b.status] ?? 1) || a.priority - b.priority || naturalCompare(a.id, b.id);
}

/** A card's children: open (any status but closed) before closed, then priority, then id. */
function byOpen(a: EpicRow, b: EpicRow): number {
  return Number(a.status === "closed") - Number(b.status === "closed") || a.priority - b.priority || naturalCompare(a.id, b.id);
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

export interface Card {
  row: ShownRow;
  children: ShownRow[];
  progress: Progress;
}

/** One repo's view: its cards, then its top-level tasks ("Other tasks"). */
export interface Outline {
  cards: Card[];
  tasks: ShownRow[];
}

/**
 * Lays out a repo's rows. Every card shows at the top, a nested one too,
 * which is also a plain row of its parent. A row whose parent names no card
 * is a top-level task, as the hub would have cleared that parent.
 */
export function outline(rows: ShownRow[]): Outline {
  const cards = rows.filter((r) => r.epic);
  const ids = new Set(cards.map((c) => c.id));
  const kids = new Map<string, ShownRow[]>();
  for (const r of rows) {
    if (!r.parent || !ids.has(r.parent) || r.parent === r.id) continue;
    kids.set(r.parent, [...(kids.get(r.parent) ?? []), r]);
  }
  return {
    cards: [...cards].sort(byStatus).map((row) => {
      const children = [...(kids.get(row.id) ?? [])].sort(byOpen);
      return { row, children, progress: progress(children) };
    }),
    tasks: rows.filter((r) => !r.epic && (!r.parent || !ids.has(r.parent) || r.parent === r.id)).sort(byStatus),
  };
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

// ---------- HTML ----------

export interface EpicsView {
  data: Epics | null;
  /** Why the last load failed, or "". */
  error: string;
  /** Keys of the cards opened to their children. */
  expanded: Set<string>;
  find: (key: string) => Item | undefined;
  now: Date;
}

function statusChip(s: string): string {
  const known = s in STATUS_NAMES ? s : "open";
  return `<span class="chip ep-status ep-s-${known}">${esc(statusName(s))}</span>`;
}

function priorityChip(p: number): string {
  return `<span class="chip ep-pri ep-p${Math.min(Math.max(Math.trunc(p), 0), 4)}">${esc(priorityName(p))}</span>`;
}

const TYPE_ICON: Record<string, string> = {
  epic: ICON.epic,
  bug: ICON.bug,
  feature: ICON.feature,
  chore: ICON.chore,
};

/** A bead's type as an icon, with the type in words for screen readers and on hover. */
function typeIcon(type: string | undefined): string {
  const t = (type ?? "").toLowerCase();
  const name = t || "task";
  return `<span class="ep-type" title="${esc(name)}">${TYPE_ICON[t] ?? ICON.task}<span class="sr">${esc(name)}</span></span>`;
}

function copyButton(id: string): string {
  return `<button class="icon-btn ep-copy" data-act="epic-copy" data-id="${esc(id)}" aria-label="Copy id ${esc(id)}" title="Copy id">${ICON.copy}</button>`;
}

/**
 * A child row or an other task: id, type, title, priority and assignee, and
 * its status when it is not plain open. A human row with its State item
 * opens that item's sheet.
 */
function rowHTML(r: ShownRow): string {
  const status = r.status === "open" ? "" : statusChip(r.status);
  const inner = `<code class="ep-id">${esc(r.id)}</code>${typeIcon(r.type)}<span class="ep-row-title">${esc(r.title)}</span>`;
  const main = r.item
    ? `<button class="ep-row-main ep-open" data-act="open" data-key="${esc(r.key)}">${inner}</button>`
    : `<span class="ep-row-main">${inner}</span>`;
  const who = r.assignee ? `<span class="ep-who">${esc(r.assignee)}</span>` : "";
  return `<li class="ep-row${r.status === "closed" ? " is-done" : ""}">${main}<span class="ep-row-meta">${status}${priorityChip(r.priority)}${who}</span>${copyButton(r.id)}</li>`;
}

function cardHTML(c: Card, open: boolean, n: string): string {
  const r = c.row;
  const kids = `ep-kids-${n}`;
  const pct = c.progress.percent;
  const children = c.children.length ? `<ul class="ep-rows" aria-label="Children of ${esc(r.id)}">${c.children.map(rowHTML).join("")}</ul>` : '<p class="muted small ep-none">No children.</p>';
  return `<li class="ep-card" data-ep="${esc(r.key)}">
<div class="ep-head"><span class="ep-mark">${ICON.epic}</span><code class="ep-id">${esc(r.id)}</code>${statusChip(r.status)}${priorityChip(r.priority)}<span class="ep-acts">${copyButton(r.id)}<button class="btn btn-small" data-act="epic-details" data-ep="${esc(r.key)}" aria-label="Details of ${esc(r.id)}">Details</button></span></div>
<h3 class="ep-title"><button class="ep-toggle" data-act="epic-toggle" data-ep="${esc(r.key)}" aria-expanded="${open}"${open ? ` aria-controls="${kids}"` : ""}><span>${esc(r.title)}</span>${ICON.right}</button></h3>
<div class="ep-progress"><span class="ep-bar" aria-hidden="true"><span class="ep-fill" data-pct="${pct}"></span></span><span class="ep-ptext">${esc(progressText(c.progress))}</span></div>
${open ? `<div class="ep-kids" id="${kids}">${children}</div>` : ""}
</li>`;
}

/** A repo's heading: its name, its path and machine where the name repeats, and when it last synced. */
function repoHead(r: RepoEpics, all: RepoEpics[], now: Date, h: string): string {
  const label = repoLabel({ name: r.repo });
  const same = all.filter((x) => repoLabel({ name: x.repo }) === label).length > 1;
  const where = same ? `${r.repo} on ${r.bridge} · ` : "";
  const synced = new Date(r.syncedAt);
  const when = Number.isNaN(synced.getTime()) ? "" : `Synced ${fmtAgo(synced, now)}`;
  return `<div class="group-head ep-repo-head"><h2 id="${h}">${esc(label)}</h2><span class="muted small">${esc(where + when)}</span></div>`;
}

/** The whole view. */
export function epicsHTML(v: EpicsView): string {
  if (!v.data) return `<p class="empty">${v.error ? `Could not load epics: ${esc(v.error)}` : "Loading"}</p>`;
  const repos = v.data.repos ?? [];
  const failed = v.error ? `<p class="ep-error" role="status">Could not load epics again: ${esc(v.error)}</p>` : "";
  if (!repos.length) return `${failed}<p class="empty">No epics yet. They show here once the machine sends them.</p>`;
  return (
    failed +
    repos
      .map((r, i) => {
        const o = outline(shownRows(r, v.find));
        const h = `ep-h-${i}`;
        let body: string;
        if (!o.cards.length && !o.tasks.length) body = '<p class="muted ep-none">Nothing open.</p>';
        else {
          const cards = o.cards.length ? `<ul class="ep-cards">${o.cards.map((c, j) => cardHTML(c, v.expanded.has(c.row.key), `${i}-${j}`)).join("")}</ul>` : "";
          const tasks = o.tasks.length
            ? `<div class="ep-tasks"><h3 class="ep-tasks-h" id="${h}-t">Other tasks <span class="group-count">${o.tasks.length}</span></h3><ul class="ep-rows" aria-labelledby="${h}-t">${o.tasks.map(rowHTML).join("")}</ul></div>`
            : "";
          body = cards + tasks;
        }
        return `<section class="group ep-repo" aria-labelledby="${h}">${repoHead(r, repos, v.now, h)}${body}</section>`;
      })
      .join("")
  );
}

/** A card's Details, read-only, in the task sheet's frame. */
export function epicDetailHTML(r: ShownRow): string {
  const desc = r.description?.trim() ? `<div class="md">${renderMarkdown(r.description)}</div>` : '<p class="muted small">No description.</p>';
  return `<div class="detail-inner" role="region" aria-labelledby="d-title">
<div class="grip" aria-hidden="true"></div>
<div class="d-head"><span class="ep-mark">${ICON.epic}</span><h2 id="d-title" class="d-title">${esc(r.title)}</h2><button class="icon-btn" data-act="close-detail" aria-label="Close details">${ICON.close}</button></div>
<div class="chips d-chips">${statusChip(r.status)}${priorityChip(r.priority)}<code class="bead-id">${esc(r.id)}</code>${copyButton(r.id)}</div>
<section class="d-section" aria-labelledby="ep-d-desc"><h3 id="ep-d-desc">Description</h3>${desc}</section>
</div>`;
}

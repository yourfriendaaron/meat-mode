// The month view: a grid of day buttons, Monday first, with each day's dated
// items, and the selected day's list under it (docs/api.md, Month view). The
// grid and entries are pure; main.ts holds the state and wires the events.

import { addDays, fmtTime, sameDay, startOfDay, ymd } from "./dates.ts";
import { esc } from "./html.ts";
import { ICON } from "./icons.ts";
import { isOpen, isSnoozed } from "./model.ts";
import type { EntryKind } from "./move.ts";
import type { Item, Occurrence } from "./types.ts";

const WEEKDAYS = ["Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"];
const DAY_NAMES = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];
const MONTH_NAMES = ["January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"];

/** Titles a day cell shows before "+n more". */
export const CELL_TITLES = 3;

/** The first day of the month `d` falls in, at local midnight. */
export function monthOf(d: Date): Date {
  return new Date(d.getFullYear(), d.getMonth(), 1);
}

/** The month `n` months after `month`'s. */
export function addMonths(month: Date, n: number): Date {
  return new Date(month.getFullYear(), month.getMonth() + n, 1);
}

/**
 * The grid's weeks for a month: Monday to Sunday, from the week holding the
 * 1st to the week holding the last day, with the neighbouring months' days
 * filling the first and last weeks. Each day is a local midnight.
 */
export function monthWeeks(month: Date): Date[][] {
  const first = monthOf(month);
  const last = new Date(first.getFullYear(), first.getMonth() + 1, 0);
  // getDay counts from Sunday; the grid from Monday.
  let day = addDays(first, -((first.getDay() + 6) % 7));
  const weeks: Date[][] = [];
  while (day <= last) {
    const week: Date[] = [];
    for (let i = 0; i < 7; i++) {
      week.push(day);
      day = new Date(day.getFullYear(), day.getMonth(), day.getDate() + 1);
    }
    weeks.push(week);
  }
  return weeks;
}

/** The time span the grid covers: the first cell's midnight to the midnight after the last cell. */
export function monthSpan(month: Date): { from: Date; to: Date } {
  const weeks = monthWeeks(month);
  const last = weeks[weeks.length - 1][6];
  return { from: weeks[0][0], to: new Date(last.getFullYear(), last.getMonth(), last.getDate() + 1) };
}

/** One thing on a day: an item's due time, the end of its snooze, or a later date of its repeat. */
export interface MonthEntry {
  item: Item;
  kind: EntryKind;
  at: Date;
}

const KIND_ORDER: Record<EntryKind, number> = { due: 0, snooze: 1, repeat: 2 };

function date(s: string | null | undefined): Date | null {
  if (!s) return null;
  const d = new Date(s);
  return Number.isNaN(d.getTime()) ? null : d;
}

/**
 * Each day's entries, by local "YYYY-MM-DD", ordered by time: an open item on
 * its due day, a snoozed one also on the day it comes back, and a repeating
 * one on each of its later dates. Closed and deleted items are left out.
 */
export function monthEntries(items: Item[], occurrences: Occurrence[], now: Date): Map<string, MonthEntry[]> {
  const open = new Map(items.filter(isOpen).map((i) => [i.key, i]));
  const all: MonthEntry[] = [];
  for (const item of open.values()) {
    const due = date(item.dueAt);
    if (due) all.push({ item, kind: "due", at: due });
    const back = date(item.deferUntil);
    if (back && isSnoozed(item, now)) all.push({ item, kind: "snooze", at: back });
  }
  for (const o of occurrences) {
    const item = open.get(o.key);
    const at = date(o.at);
    if (item && at) all.push({ item, kind: "repeat", at });
  }
  all.sort((a, b) => a.at.getTime() - b.at.getTime() || KIND_ORDER[a.kind] - KIND_ORDER[b.kind] || a.item.title.localeCompare(b.item.title));
  const byDay = new Map<string, MonthEntry[]>();
  for (const e of all) {
    const k = ymd(e.at);
    const list = byDay.get(k) ?? [];
    list.push(e);
    byDay.set(k, list);
  }
  return byDay;
}

/** "November 2026". */
export function monthTitle(month: Date): string {
  return `${MONTH_NAMES[month.getMonth()]} ${month.getFullYear()}`;
}

/** "Wednesday 4 November". */
export function longDay(d: Date): string {
  return `${DAY_NAMES[d.getDay()]} ${d.getDate()} ${MONTH_NAMES[d.getMonth()]}`;
}

/** A day button's accessible name: "Wednesday 4 November, 3 items". */
export function dayLabel(d: Date, count: number): string {
  return `${longDay(d)}, ${count === 0 ? "no items" : count === 1 ? "1 item" : `${count} items`}`;
}

/** What the month view draws from. */
export interface MonthModel {
  month: Date;
  /** The selected day, a local midnight. */
  selected: Date;
  now: Date;
  entries: Map<string, MonthEntry[]>;
  /** Whether the repeat dates could not be loaded. */
  occurrencesFailed: boolean;
  /** Whether an entry can move to another day. */
  movable: (e: MonthEntry) => boolean;
}

/** Data attributes that name an entry, for the open, drag and Move handlers. */
function entryAttrs(e: MonthEntry): string {
  return `data-key="${esc(e.item.key)}" data-kind="${e.kind}" data-at="${e.at.toISOString()}"`;
}

function marker(e: MonthEntry): string {
  if (e.kind === "snooze") return ICON.moon;
  if (e.kind === "repeat") return ICON.repeat;
  return "";
}

function cellHTML(d: Date, m: MonthModel): string {
  const key = ymd(d);
  const list = m.entries.get(key) ?? [];
  const today = startOfDay(m.now);
  const cls = ["mcell"];
  if (d.getMonth() !== m.month.getMonth()) cls.push("is-out");
  if (d < today) cls.push("is-past");
  if (sameDay(d, today)) cls.push("is-today");
  if (sameDay(d, m.selected)) cls.push("is-sel");
  const shown = list.slice(0, CELL_TITLES);
  const more = list.length - shown.length;
  const titles = shown
    .map((e) => {
      const drag = m.movable(e) ? ' draggable="true"' : "";
      return `<span class="ment ment-${e.kind}" data-act="month-day" data-day="${key}" ${entryAttrs(e)}${drag}>${marker(e)}<span class="ment-t">${esc(e.item.title)}</span></span>`;
    })
    .join("");
  return `<td class="${cls.join(" ")}" data-day="${key}"><button type="button" class="mday" data-act="month-day" data-day="${key}" aria-label="${esc(dayLabel(d, list.length))}" aria-pressed="${sameDay(d, m.selected)}"${sameDay(d, today) ? ' aria-current="date"' : ""}><span class="mday-n">${d.getDate()}</span></button>${list.length ? `<span class="mcount" aria-hidden="true">${list.length}</span>` : ""}<div class="ments" aria-hidden="true">${titles}${more ? `<span class="ment-more" data-act="month-day" data-day="${key}">+${more} more</span>` : ""}</div></td>`;
}

function dayListHTML(m: MonthModel): string {
  const list = m.entries.get(ymd(m.selected)) ?? [];
  const rows = list
    .map((e) => {
      const what = e.kind === "snooze" ? "Snoozed until" : e.kind === "repeat" ? "Repeats" : "Due";
      const move = m.movable(e)
        ? `<button type="button" class="btn btn-small" data-act="month-move" ${entryAttrs(e)} aria-label="${esc(`Move: ${e.item.title}`)}">Move</button>`
        : "";
      return `<li class="mrow mrow-${e.kind}"><button type="button" class="mrow-body" data-act="open" ${entryAttrs(e)}${m.movable(e) ? " data-movable" : ""}><span class="mrow-time">${fmtTime(e.at)}</span><span class="mrow-title">${esc(e.item.title)}</span><span class="mrow-kind">${marker(e)}${what}</span></button>${move}</li>`;
    })
    .join("");
  return `<section class="mlist" aria-labelledby="mlist-h"><h2 id="mlist-h">${esc(longDay(m.selected))}</h2>${
    rows ? `<ul class="mrows">${rows}</ul>` : '<p class="muted small mlist-none">Nothing on this day.</p>'
  }</section>`;
}

/** The month: its header, the grid and the selected day's list. */
export function monthHTML(m: MonthModel): string {
  const head = `<div class="month-head"><h2 id="month-h" aria-live="polite">${esc(monthTitle(m.month))}</h2><div class="month-nav"><button type="button" class="icon-btn" data-act="month-prev" aria-label="Previous month" title="Previous month">${ICON.left}</button><button type="button" class="btn btn-small" data-act="month-today">Today</button><button type="button" class="icon-btn" data-act="month-next" aria-label="Next month" title="Next month">${ICON.right}</button></div></div>`;
  const cols = WEEKDAYS.map((w) => `<th scope="col" abbr="${w}"><span aria-hidden="true">${w.slice(0, 3)}</span><span class="sr">${w}</span></th>`).join("");
  const body = monthWeeks(m.month)
    .map((week) => `<tr>${week.map((d) => cellHTML(d, m)).join("")}</tr>`)
    .join("");
  const note = m.occurrencesFailed ? '<p class="month-note muted small" role="status">Repeat dates could not be loaded.</p>' : "";
  return `<div class="month">${head}${note}<table class="mgrid" aria-labelledby="month-h"><thead><tr>${cols}</tr></thead><tbody>${body}</tbody></table>${dayListHTML(m)}</div>`;
}

/** The List / Month switch at the top of Upcoming. */
export function viewSwitchHTML(month: boolean): string {
  const b = (v: string, label: string, on: boolean) =>
    `<button type="button" class="vswitch-btn" data-act="view" data-view="${v}" aria-pressed="${on}">${label}</button>`;
  return `<div class="vswitch" role="group" aria-label="Show Upcoming as">${b("upcoming", "List", !month)}${b("month", "Month", month)}</div>`;
}

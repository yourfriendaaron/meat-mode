// Moving a month view entry to another day: which action it posts, or why
// the app refuses it. Pure: callers pass `now` and the new action's id.

import { fmtDate, fmtDay, fmtWhenAt, fromYmd, sameDay, startOfDay, ymd } from "./dates.ts";
import { canEdit } from "./edit.ts";
import { isOpen } from "./model.ts";
import type { Item, NewAction } from "./types.ts";

/** What puts an item on a day of the month: its due time, the end of its snooze, or a later date of its repeat. */
export type EntryKind = "due" | "snooze" | "repeat";

/** The entry being moved: its kind and the moment it shows at. */
export interface MoveEntry {
  kind: EntryKind;
  at: Date;
}

export type MoveDecision =
  /** Dropped on the day it is on already. */
  | { kind: "none" }
  | { kind: "refuse"; text: string }
  | { kind: "move"; action: NewAction; toast: string };

/**
 * Whether an entry can move. A personal item moves its due date, so only its
 * due entry; an agent item whose list can snooze moves by a snooze, from its
 * due or its snooze entry. Repeat dates never move on their own.
 */
export function canMove(item: Item, kind: EntryKind, canSnooze: boolean): boolean {
  if (!isOpen(item) || kind === "repeat") return false;
  if (canEdit(item)) return kind === "due" && !!item.dueAt;
  return canSnooze;
}

/** `day` at the time of day `at` shows, in local time. */
export function onDay(day: Date, at: Date): Date {
  return new Date(day.getFullYear(), day.getMonth(), day.getDate(), at.getHours(), at.getMinutes(), at.getSeconds());
}

/** A moment's date as "YYYY-MM-DD" in an IANA zone; local time when the zone is unknown. */
export function ymdIn(d: Date, timeZone: string): string {
  try {
    const parts = new Intl.DateTimeFormat("en-CA", { timeZone, year: "numeric", month: "2-digit", day: "2-digit" }).formatToParts(d);
    const get = (t: string) => parts.find((p) => p.type === t)?.value ?? "";
    return `${get("year")}-${get("month")}-${get("day")}`;
  } catch {
    return ymd(d);
  }
}

/**
 * What moving `entry` of `item` to `day` does. A personal item gets an edit
 * of its due date, keeping the time of day; an agent item gets a snooze until
 * the day at the entry's time. Days before today are refused, and so are a
 * snooze to a time already gone and a due date after the repeat's end.
 */
export function decideMove(item: Item, entry: MoveEntry, day: Date, now: Date, canSnooze: boolean, id: string): MoveDecision {
  if (!canMove(item, entry.kind, canSnooze)) return { kind: "refuse", text: "This entry cannot move to another day." };
  if (sameDay(day, entry.at)) return { kind: "none" };
  if (startOfDay(day) < startOfDay(now)) return { kind: "refuse", text: "That day has passed. Pick today or a later day." };
  const at = onDay(day, entry.at);
  if (canEdit(item)) {
    const until = item.repeat?.until;
    if (until && ymdIn(at, item.repeat!.timeZone) > until) {
      const end = fromYmd(until);
      return { kind: "refuse", text: `The repeat ends on ${end ? fmtDate(end, now) : until}. Pick that day or an earlier one.` };
    }
    return {
      kind: "move",
      action: { id, kind: "edit", key: item.key, payload: { set: { due: at.toISOString() } } },
      toast: `Moved to ${fmtDay(at)}`,
    };
  }
  if (at <= now) return { kind: "refuse", text: "That time has passed today. Pick a later day." };
  return {
    kind: "move",
    action: { id, kind: "snooze", key: item.key, payload: { until: at.toISOString(), previous: item.deferUntil ?? null } },
    toast: `Snoozed to ${fmtWhenAt(at, now)}`,
  };
}

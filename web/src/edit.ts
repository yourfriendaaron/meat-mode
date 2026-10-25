// Editing personal items: the task sheet's draft, its diff and priority helpers,
// and the Undo of the edit actions (docs/api.md, Edit rules). Pure: callers
// pass `now`.

import { MAX_REPEAT_INTERVAL, type Action, type Comment, type Edit, type Item, type NewAction, type RepeatUnit } from "./types.ts";
import { isOpen, isPersonal, repoKey } from "./model.ts";
import { fmtWhenAt, fromYmd, ymd } from "./dates.ts";
import { DEFAULT_HOUR } from "./quickadd.ts";
import { sameRule, type RepeatRule } from "./repeat.ts";
import { cleanTags, sameTags, shownTags, typedTags } from "./tags.ts";

export const TITLE_MAX = 1000;
export const TEXT_MAX = 10000;

/** Whether the hub takes edit actions on this item: the personal list it keeps itself. */
export function canEdit(item: Pick<Item, "bridge">): boolean {
  return isPersonal(item);
}

/** The priority chip's text: 0 and 1 show on every item, the rest show nothing. */
export function priorityChip(priority: number): string {
  if (priority === 0) return "Urgent";
  if (priority === 1) return "High priority";
  return "";
}

/** The priority picker's choices. 0 and 4 show only on an item already there, so it keeps them. */
export function priorityOptions(current: number): { value: number; label: string }[] {
  const out = [
    { value: 1, label: "High" },
    { value: 2, label: "Normal" },
    { value: 3, label: "Low" },
  ];
  if (current === 0) out.unshift({ value: 0, label: "Urgent" });
  if (current === 4) out.push({ value: 4, label: "Someday" });
  return out;
}

/** "Due Fri, Oct 23 9:00": the label quick add shows for a parsed due date. */
export function dueLabel(d: Date, now: Date): string {
  return `Due ${fmtWhenAt(d, now)}`;
}

/** The hour a date picked without a time takes, as quick add gives one. */
export const DRAFT_HOUR = DEFAULT_HOUR;

/** "09:05": a time as an <input type="time"> holds it. */
export function hhmm(d: Date): string {
  return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
}

/**
 * A personal item's fields as the sheet edits them. The due time is `date`
 * ("YYYY-MM-DD", "" for none) and `time` ("HH:MM", "" for the default hour),
 * in local time.
 */
export interface EditDraft {
  title: string;
  description: string;
  date: string;
  time: string;
  priority: number;
  /** "" for no repeat. */
  repeat: RepeatUnit | "";
  /** The repeat's "every n" as typed. */
  interval: string;
  /** A weekly repeat's days; none means the due date's day. */
  weekdays: number[];
  fromDone: boolean;
  /** The repeat's last date, "YYYY-MM-DD"; "" repeats for good. */
  until: string;
  /** The zone a repeat set from this form keeps: the device's. */
  timeZone: string;
  /** Minutes; null uses the hub's reminderLead. */
  remindBefore: number | null;
  /** The id of the item it is a sub-task of; "" for top-level. */
  parent: string;
  /** The tags as typed, split at spaces and commas. */
  tags: string;
}

export function draftOf(item: Item, timeZone = ""): EditDraft {
  const r = item.repeat;
  const due = item.dueAt ? new Date(item.dueAt) : null;
  return {
    title: item.title,
    description: item.description ?? "",
    date: due ? ymd(due) : "",
    time: due ? hhmm(due) : "",
    priority: item.priority,
    repeat: r?.every ?? "",
    interval: String(r?.interval || 1),
    weekdays: [...(r?.weekdays ?? [])],
    fromDone: !!r?.fromDone,
    until: r?.until ?? "",
    timeZone,
    remindBefore: item.remindBefore ?? null,
    parent: item.parent ?? "",
    tags: shownTags(item).join(" "),
  };
}

/**
 * The items a personal item can move under (docs/api.md, Sub-tasks): open,
 * top-level and saved items of its own list, never itself. An item with
 * sub-tasks of its own, open or closed, can move under none.
 */
export function moveTargets(item: Item, items: Item[]): Item[] {
  if (!canEdit(item)) return [];
  const list = repoKey(item);
  const same = items.filter((i) => repoKey(i) === list);
  if (same.some((i) => i.parent === item.id)) return [];
  return same
    .filter((i) => i.id !== item.id && isOpen(i) && !i.parent && !i.id.startsWith("pending-"))
    .sort((a, b) => a.title.localeCompare(b.title));
}

/** The repeat the form sets, or null for none. Undefined when its interval is not one the hub takes. */
export function draftRule(d: EditDraft): RepeatRule | null | undefined {
  if (!d.repeat) return null;
  const n = Number(d.interval.trim());
  if (!Number.isInteger(n) || n < 1 || n > MAX_REPEAT_INTERVAL) return undefined;
  const r: RepeatRule = { every: d.repeat };
  if (n !== 1) r.interval = n;
  // The hub takes weekdays on a weekly rule only, and not with fromDone.
  if (d.repeat === "week" && !d.fromDone && d.weekdays.length) r.weekdays = [...d.weekdays].sort((a, b) => a - b);
  if (d.fromDone) r.fromDone = true;
  if (d.until) r.until = d.until;
  return r;
}

/** What the due field will save: a date, none, or the text it could not read. */
export type DueOutcome = { kind: "keep"; at: Date | null } | { kind: "set"; at: Date } | { kind: "clear" } | { kind: "invalid" };

/** The due time the draft's date and time make: null for none, undefined when either is not a real one. */
export function draftDue(d: Pick<EditDraft, "date" | "time">): Date | null | undefined {
  if (!d.date) return null;
  const day = fromYmd(d.date);
  if (!day) return undefined;
  if (!d.time) return new Date(day.getFullYear(), day.getMonth(), day.getDate(), DRAFT_HOUR, 0);
  const m = /^(\d{2}):(\d{2})$/.exec(d.time);
  if (!m || Number(m[1]) > 23 || Number(m[2]) > 59) return undefined;
  return new Date(day.getFullYear(), day.getMonth(), day.getDate(), Number(m[1]), Number(m[2]));
}

/** What the draft does to the due time. The item's own time, to the minute, keeps it. */
export function dueOutcome(item: Item, d: EditDraft): DueOutcome {
  const at = draftDue(d);
  const had = item.dueAt ? new Date(item.dueAt) : null;
  if (at === undefined) return { kind: "invalid" };
  if (at === null) return had ? { kind: "clear" } : { kind: "keep", at: null };
  if (had && ymd(had) === d.date && hhmm(had) === hhmm(at)) return { kind: "keep", at: had };
  return { kind: "set", at };
}

/** The reason the form cannot be saved, or "". */
export function editError(item: Item, d: EditDraft, now: Date): string {
  const title = d.title.trim();
  if (!title) return "Add a title.";
  if (title.length > TITLE_MAX) return `Keep the title to ${TITLE_MAX} characters.`;
  if (d.description.length > TEXT_MAX) return `Keep the details to ${TEXT_MAX} characters.`;
  const due = dueOutcome(item, d);
  if (due.kind === "invalid") return "Pick a real date and time.";
  const rule = draftRule(d);
  if (rule === undefined) return `Repeat every 1 to ${MAX_REPEAT_INTERVAL}.`;
  if (rule && (due.kind === "clear" || (due.kind === "keep" && !due.at))) return "A repeat needs a date. Add one, or pick Never.";
  if (rule?.until && !fromYmd(rule.until)) return "End the repeat on a real date, or pick No end date.";
  if (rule?.until && (due.kind === "set" || due.kind === "keep") && due.at && rule.until < ymd(due.at)) return "End the repeat on or after its due date.";
  const tags = cleanTags(typedTags(d.tags));
  if ("error" in tags) return tags.error;
  return "";
}

/** The `set` of an edit action holding only the fields the form changed, or null when nothing changed. */
export function diffEdit(item: Item, d: EditDraft, now: Date): Edit | null {
  const set: Edit = {};
  const title = d.title.trim();
  if (title !== item.title) set.title = title;
  if (d.description !== (item.description ?? "")) set.description = d.description;
  const due = dueOutcome(item, d);
  const had = item.dueAt ? new Date(item.dueAt).getTime() : null;
  if (due.kind === "set" && due.at.getTime() !== had) set.due = due.at.toISOString();
  if (due.kind === "clear" && had !== null) set.clearDue = true;
  if (d.priority !== item.priority) set.priority = d.priority;
  const rule = draftRule(d);
  if (rule && !sameRule(rule, item.repeat)) set.repeat = { ...rule, timeZone: d.timeZone };
  // Clearing the due date ends the repeat on the hub without being asked.
  if (rule === null && item.repeat && !set.clearDue) set.clearRepeat = true;
  if (d.remindBefore !== (item.remindBefore ?? null)) {
    if (d.remindBefore === null) set.clearRemindBefore = true;
    else set.remindBefore = d.remindBefore;
  }
  if (d.parent !== (item.parent ?? "")) set.parent = d.parent;
  const tags = cleanTags(typedTags(d.tags));
  if ("tags" in tags && !sameTags(tags.tags, shownTags(item))) set.labels = tags.tags;
  return Object.keys(set).length ? set : null;
}

/** Applies an edit's `set` to a local copy, as the hub does. */
export function applyEdit(item: Item, set: Edit): void {
  if (set.title !== undefined) item.title = set.title;
  if (set.description !== undefined) item.description = set.description;
  if (set.due !== undefined) item.dueAt = set.due;
  if (set.clearDue) {
    item.dueAt = null;
    item.repeat = null;
  }
  if (set.priority !== undefined) item.priority = set.priority;
  if (set.repeat !== undefined) item.repeat = { ...set.repeat };
  if (set.clearRepeat) item.repeat = null;
  if (set.remindBefore !== undefined) item.remindBefore = set.remindBefore;
  if (set.clearRemindBefore) item.remindBefore = null;
  if (set.parent !== undefined) {
    item.parent = set.parent || undefined;
    item.parentTitle = undefined;
  }
  if (set.labels !== undefined) item.labels = set.labels.length ? [...set.labels] : undefined;
}

/** Puts a removed comment back in its place by `createdAt`. */
export function restoreComment(comments: Comment[] | null, c: Comment): Comment[] {
  const out = (comments ?? []).filter((x) => x.id !== c.id);
  const at = out.findIndex((x) => x.createdAt > c.createdAt);
  out.splice(at < 0 ? out.length : at, 0, { ...c });
  return out;
}

/**
 * The action that undoes an applied edit, note-edit or note-delete, or a done
 * that moved a repeating item on, from what the hub filled in: null when the
 * reply lacks it. `key` is the item's key.
 */
export function undoOf(applied: Action, key: string, id: string): NewAction | null {
  const p = applied.payload;
  switch (applied.kind) {
    case "done":
    case "edit":
      return p.was ? { id, kind: "edit", key, payload: { set: p.was } } : null;
    case "note-edit":
      return p.comment ? { id, kind: "note-edit", key, payload: { commentId: p.comment.id, text: p.comment.text } } : null;
    case "note-delete":
      return p.comment ? { id, kind: "note-restore", key, payload: { comment: p.comment } } : null;
    default:
      return null;
  }
}

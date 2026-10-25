// An item's history in the task sheet: each change it went through in plain
// words, who made it, and the Undo it can take. The hub sends the actions
// newest first (docs/api.md, Item history). Pure: callers pass `now`.

import type { Action, Edit, Item, NewAction } from "./types.ts";
import { fmtAgo } from "./dates.ts";
import { leadLabel } from "./lead.ts";
import { DISMISSED, isOpen } from "./model.ts";
import { repeatWords, sameRule } from "./repeat.ts";
import { shownTags } from "./tags.ts";
import { whenLine } from "./schedule.ts";

/** How many changes the History section shows before Show all. */
export const HISTORY_SHOWN = 5;

/** What the item's history needs to know besides its actions: the titles of other items of its list, by id. */
export interface HistoryContext {
  now: Date;
  /** The title of an item of the same list, or "" when the app does not hold it. */
  title: (id: string) => string;
}

const PRIORITY = ["Urgent", "High", "Normal", "Low", "Someday"];

/** "today at 18:00", "Sun, Nov 1 at 12:00": a moment inside a sentence. */
function at(iso: string, now: Date): string {
  const s = whenLine(new Date(iso), now);
  return /^(Today|Tomorrow|Yesterday) /.test(s) ? s[0].toLowerCase() + s.slice(1) : s;
}

function quoted(text: string, max = 60): string {
  const t = text.trim().replace(/\s+/g, " ");
  return `“${t.length > max ? `${t.slice(0, max - 1)}…` : t}”`;
}

/** "Tags: +garden −old", from the tags before and after. */
function tagsLine(was: string[] | undefined, now: string[]): string {
  if (!was) return now.length ? `Tags: ${now.join(", ")}` : "Removed all tags";
  const lower = (l: string[]) => l.map((t) => t.toLowerCase());
  const before = lower(was);
  const after = lower(now);
  const parts = [...now.filter((t) => !before.includes(t.toLowerCase())).map((t) => `+${t}`), ...was.filter((t) => !after.includes(t.toLowerCase())).map((t) => `−${t}`)];
  if (!now.length) return "Removed all tags";
  return parts.length ? `Tags: ${parts.join(" ")}` : `Tags: ${now.join(", ")}`;
}

/** Each field an edit set, in plain words, in the order the sheet shows them. */
export function editLines(set: Edit, was: Edit | undefined, ctx: HistoryContext): string[] {
  const out: string[] = [];
  if (set.title !== undefined) out.push(`Renamed to ${quoted(set.title)}`);
  if (set.due !== undefined) out.push(`Moved to ${at(set.due, ctx.now)}`);
  if (set.clearDue) out.push("Removed the date");
  if (set.repeat) out.push(`Repeat: ${repeatWords(set.repeat, ctx.now).replace(/^Every/, "every").replace(/^Then/, "then")}`);
  if (set.clearRepeat) out.push("Stopped repeating");
  if (set.priority !== undefined) out.push(`Priority: ${PRIORITY[set.priority] ?? set.priority}`);
  if (set.remindBefore !== undefined) out.push(set.remindBefore > 0 ? `Reminds ${leadLabel(set.remindBefore)}` : "No early reminder");
  if (set.clearRemindBefore) out.push("Reminds at the usual time");
  if (set.labels !== undefined) out.push(tagsLine(was?.labels, set.labels));
  if (set.parent !== undefined) {
    const t = set.parent ? ctx.title(set.parent) : "";
    out.push(set.parent ? (t ? `Moved under ${t}` : "Made a sub-task") : "Made a top-level task");
  }
  if (set.description !== undefined) out.push(set.description ? "Changed the details" : "Removed the details");
  if (set.subtasksDone?.length) out.push(set.subtasksDone.length === 1 ? "Closed a sub-task" : `Closed ${set.subtasksDone.length} sub-tasks`);
  return out;
}

/** What a change did to the item, in plain words: "Moved to Sun, Nov 1 at 12:00", "Marked done". */
export function changeText(a: Action, item: Pick<Item, "id">, ctx: HistoryContext): string {
  const p = a.payload;
  switch (a.kind) {
    case "add":
      return a.beadId && a.beadId !== item.id ? `Added sub-task ${p.title ?? ""}`.trim() : "Added";
    case "done":
      return p.was?.due ? `Marked done; was due ${at(p.was.due, ctx.now)}` : "Marked done";
    case "dismiss":
      return "Deleted";
    case "reopen":
      return "Reopened";
    case "snooze":
      return p.until ? `Snoozed to ${at(p.until, ctx.now)}` : "Snooze cleared";
    case "note":
      return `Added a note ${quoted(p.text ?? "")}`;
    case "ask":
      return p.text ? `Asked the agent ${quoted(p.text)}` : "Asked the agent";
    case "stop":
      return "Stopped the agent";
    case "edit":
      return p.set ? editLines(p.set, p.was, ctx).join("; ") || "Edited" : "Edited";
    case "note-edit":
      return `Edited a note${p.text ? ` to ${quoted(p.text)}` : ""}`;
    case "note-delete":
      return p.comment ? `Deleted the note ${quoted(p.comment.text)}` : "Deleted a note";
    case "note-restore":
      return p.comment ? `Restored the note ${quoted(p.comment.text)}` : "Restored a note";
    default:
      return a.kind;
  }
}

/** Who made the change: "You", or "planner on desk" for an agent on a paired machine. */
export function whoText(a: Pick<Action, "author" | "machine">): string {
  if (!a.machine) return "You";
  return a.author && a.author !== a.machine ? `${a.author} on ${a.machine}` : a.machine;
}

/** "You · 5m ago". */
export function byLine(a: Action, now: Date): string {
  return `${whoText(a)} · ${fmtAgo(new Date(a.createdAt), now)}`;
}

/**
 * What a change touched, for telling whether a later one overrode it:
 * fields of the item, its open or closed status, its snooze, or one note.
 */
export function touched(a: Action): string[] {
  const p = a.payload;
  switch (a.kind) {
    case "done":
      return p.was ? ["due", "status"] : ["status"];
    case "dismiss":
    case "reopen":
      return ["status"];
    case "snooze":
      return ["snooze"];
    case "edit": {
      const s = p.set ?? {};
      const out: string[] = [];
      if (s.title !== undefined) out.push("title");
      if (s.description !== undefined) out.push("description");
      if (s.due !== undefined || s.clearDue) out.push("due");
      if (s.clearDue || s.repeat || s.clearRepeat) out.push("repeat");
      if (s.priority !== undefined) out.push("priority");
      if (s.remindBefore !== undefined || s.clearRemindBefore) out.push("lead");
      if (s.parent !== undefined) out.push("parent");
      if (s.labels !== undefined) out.push("labels");
      return out;
    }
    case "note-edit":
    case "note-delete":
      return p.commentId ? [`note:${p.commentId}`] : [];
    case "note-restore":
      return p.comment ? [`note:${p.comment.id}`] : [];
    default:
      return [];
  }
}

function sameTime(a: string | null | undefined, b: string | null | undefined): boolean {
  if (!a || !b) return !a && !b;
  return new Date(a).getTime() === new Date(b).getTime();
}

/** Whether the item still holds every value an edit set. */
export function holds(item: Item, set: Edit): boolean {
  if (set.title !== undefined && item.title !== set.title) return false;
  if (set.description !== undefined && (item.description ?? "") !== set.description) return false;
  if (set.due !== undefined && !sameTime(item.dueAt, set.due)) return false;
  if (set.clearDue && item.dueAt) return false;
  if (set.priority !== undefined && item.priority !== set.priority) return false;
  if (set.repeat && !sameRule(set.repeat, item.repeat)) return false;
  if (set.clearRepeat && item.repeat) return false;
  if (set.remindBefore !== undefined && item.remindBefore !== set.remindBefore) return false;
  if (set.clearRemindBefore && item.remindBefore != null) return false;
  if (set.parent !== undefined && (item.parent ?? "") !== set.parent) return false;
  if (set.labels !== undefined) {
    const a = shownTags({ labels: set.labels }).map((t) => t.toLowerCase());
    const b = shownTags(item).map((t) => t.toLowerCase());
    if (a.length !== b.length || a.some((t, i) => t !== b[i])) return false;
  }
  return true;
}

/** Whether a change closed the item outright: a done that did not move it on, or a delete. */
function closes(a: Action): boolean {
  return (a.kind === "done" && !a.payload.was) || a.kind === "dismiss";
}

/**
 * The status change before the one at `index` in `history` (newest first),
 * or undefined when the history holds none: the item was open then.
 */
function statusBefore(history: Action[], index: number): Action | undefined {
  return history.slice(index + 1).find((x) => x.status === "applied" && touched(x).includes("status") && (closes(x) || x.kind === "reopen"));
}

/**
 * The action that undoes the change at `index` of `history` (the item's
 * applied changes, newest first), or null when it cannot be undone safely.
 *
 * The rule: a change can be undone when it is the latest change to every
 * field it touched, and the item still shows what it set. So an agent's
 * new due time undoes while nothing moved the date since; once the user
 * moves it again, only that later change offers Undo. Adds and notes have
 * no Undo here; the sheet's own buttons remove them.
 */
export function undoFor(history: Action[], index: number, item: Item, id: string): NewAction | null {
  const a = history[index];
  if (!a || a.status !== "applied" || a.beadId !== item.id) return null;
  const fields = touched(a);
  if (!fields.length) return null;
  const later = history.slice(0, index).filter((x) => x.status === "applied" && x.beadId === item.id);
  if (later.some((x) => touched(x).some((f) => fields.includes(f)))) return null;
  const key = item.key;
  const p = a.payload;
  switch (a.kind) {
    case "edit":
      if (!p.set || !p.was || p.set.subtasksDone?.length || !holds(item, p.set)) return null;
      return { id, kind: "edit", key, payload: { set: p.was } };
    case "done":
      if (p.was) return isOpen(item) ? { id, kind: "edit", key, payload: { set: p.was } } : null;
      if (isOpen(item) || item.closeReason === DISMISSED) return null;
      // Undoing a done reopens only an item that was open before it.
      return statusBefore(history, index) && closes(statusBefore(history, index)!) ? null : { id, kind: "reopen", key, payload: {} };
    case "dismiss": {
      if (isOpen(item) || item.closeReason !== DISMISSED) return null;
      const before = statusBefore(history, index);
      return before && closes(before) ? null : { id, kind: "reopen", key, payload: {} };
    }
    case "reopen": {
      if (!isOpen(item)) return null;
      const before = statusBefore(history, index);
      if (before?.kind === "dismiss") return { id, kind: "dismiss", key, payload: {} };
      // A done on a repeating item would move it on instead of closing it.
      if (before?.kind === "done" && !item.repeat) return { id, kind: "done", key, payload: {} };
      return null;
    }
    case "snooze":
      if (!sameTime(item.deferUntil, p.until)) return null;
      return { id, kind: "snooze", key, payload: { until: p.previous ?? null, previous: p.until ?? null } };
    case "note-edit": {
      const c = (item.comments ?? []).find((x) => x.id === p.commentId);
      if (!c || !p.comment || c.text !== p.text) return null;
      return { id, kind: "note-edit", key, payload: { commentId: c.id, text: p.comment.text } };
    }
    case "note-delete":
      if (!p.comment || (item.comments ?? []).some((x) => x.id === p.comment!.id)) return null;
      return { id, kind: "note-restore", key, payload: { comment: p.comment } };
    case "note-restore":
      if (!p.comment || !(item.comments ?? []).some((x) => x.id === p.comment!.id)) return null;
      return { id, kind: "note-delete", key, payload: { commentId: p.comment.id } };
    default:
      return null;
  }
}

/** The changes History lists: those the hub applied, newest first. Waiting, failed and cancelled ones change nothing. */
export function shownChanges(history: Action[]): Action[] {
  return history.filter((a) => a.status === "applied");
}

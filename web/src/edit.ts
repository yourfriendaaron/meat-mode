// Editing personal items: the edit form's diff, checklist and priority helpers,
// and the Undo of the edit actions (docs/api.md, Edit rules). Pure: callers
// pass `now`.

import type { Action, Check, Comment, Edit, Item, NewAction } from "./types.ts";
import { HUB_BRIDGE } from "./model.ts";
import { fmtTime, fmtWhen } from "./dates.ts";
import { parseQuickAdd } from "./quickadd.ts";

export const TITLE_MAX = 1000;
export const TEXT_MAX = 10000;
export const CHECK_MAX = 1000;
export const CHECKLIST_MAX = 100;

/** Whether the hub takes edit actions on this item: the personal list it keeps itself. */
export function canEdit(item: Pick<Item, "bridge">): boolean {
  return item.bridge === HUB_BRIDGE;
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
  const when = fmtWhen(d, now);
  return `Due ${when.includes(":") ? when : `${when} ${fmtTime(d)}`}`;
}

/** A due date typed the way quick add reads one ("tomorrow 9am"); null when it holds none. */
export function parseDue(text: string, now: Date): Date | null {
  return parseQuickAdd(text, now, []).due;
}

/** The edit form as typed. `dueText` "" keeps the due date unless `clearDue` is set. */
export interface EditDraft {
  title: string;
  description: string;
  dueText: string;
  clearDue: boolean;
  priority: number;
}

export function draftOf(item: Item): EditDraft {
  return { title: item.title, description: item.description ?? "", dueText: "", clearDue: false, priority: item.priority };
}

/** What the due field will save: a date, none, or the text it could not read. */
export type DueOutcome = { kind: "keep"; at: Date | null } | { kind: "set"; at: Date } | { kind: "clear" } | { kind: "invalid" };

export function dueOutcome(item: Item, d: EditDraft, now: Date): DueOutcome {
  if (d.dueText.trim()) {
    const at = parseDue(d.dueText, now);
    return at ? { kind: "set", at } : { kind: "invalid" };
  }
  if (d.clearDue) return { kind: "clear" };
  return { kind: "keep", at: item.dueAt ? new Date(item.dueAt) : null };
}

/** The reason the form cannot be saved, or "". */
export function editError(item: Item, d: EditDraft, now: Date): string {
  const title = d.title.trim();
  if (!title) return "Add a title.";
  if (title.length > TITLE_MAX) return `Keep the title to ${TITLE_MAX} characters.`;
  if (d.description.length > TEXT_MAX) return `Keep the details to ${TEXT_MAX} characters.`;
  if (dueOutcome(item, d, now).kind === "invalid") return "Could not read that date. Try something like tomorrow 9am.";
  return "";
}

/** The `set` of an edit action holding only the fields the form changed, or null when nothing changed. */
export function diffEdit(item: Item, d: EditDraft, now: Date): Edit | null {
  const set: Edit = {};
  const title = d.title.trim();
  if (title !== item.title) set.title = title;
  if (d.description !== (item.description ?? "")) set.description = d.description;
  const due = dueOutcome(item, d, now);
  const had = item.dueAt ? new Date(item.dueAt).getTime() : null;
  if (due.kind === "set" && due.at.getTime() !== had) set.due = due.at.toISOString();
  if (due.kind === "clear" && had !== null) set.clearDue = true;
  if (d.priority !== item.priority) set.priority = d.priority;
  return Object.keys(set).length ? set : null;
}

/** Applies an edit's `set` to a local copy, as the hub does. */
export function applyEdit(item: Item, set: Edit): void {
  if (set.title !== undefined) item.title = set.title;
  if (set.description !== undefined) item.description = set.description;
  if (set.due !== undefined) item.dueAt = set.due;
  if (set.clearDue) item.dueAt = null;
  if (set.priority !== undefined) item.priority = set.priority;
  if (set.checklist !== undefined) item.checklist = set.checklist.map((c) => ({ ...c }));
}

/** "2/5": done entries out of all, or "" for an empty checklist. */
export function checklistChip(list: Check[] | null | undefined): string {
  if (!list?.length) return "";
  return `${list.filter((c) => c.done).length}/${list.length}`;
}

/** A checklist entry's text ready to send, or "" when it cannot be added. */
export function checkText(text: string): string {
  const t = text.trim();
  return t.length > CHECK_MAX ? "" : t;
}

/** The checklist with one more entry, which the hub gives an id. */
export function addCheck(list: Check[] | null | undefined, text: string): Check[] {
  return [...copy(list), { text, done: false }];
}

export function toggleCheck(list: Check[] | null | undefined, index: number): Check[] {
  return copy(list).map((c, i) => (i === index ? { ...c, done: !c.done } : c));
}

export function removeCheck(list: Check[] | null | undefined, index: number): Check[] {
  return copy(list).filter((_, i) => i !== index);
}

function copy(list: Check[] | null | undefined): Check[] {
  return (list ?? []).map((c) => ({ ...c }));
}

/** Puts a removed comment back in its place by `createdAt`. */
export function restoreComment(comments: Comment[] | null, c: Comment): Comment[] {
  const out = (comments ?? []).filter((x) => x.id !== c.id);
  const at = out.findIndex((x) => x.createdAt > c.createdAt);
  out.splice(at < 0 ? out.length : at, 0, { ...c });
  return out;
}

/**
 * The action that undoes an applied edit, note-edit or note-delete, from what
 * the hub filled in: null when the reply lacks it. `key` is the item's key.
 */
export function undoOf(applied: Action, key: string, id: string): NewAction | null {
  const p = applied.payload;
  switch (applied.kind) {
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

// The action pipeline: what an action does to the local copy before the hub
// answers, posting it, and its Undo (docs/api.md, Undo). The caller words the
// toasts; this module never touches the page.

import { ApiError } from "./api.ts";
import { applyEdit, restoreComment, undoOf } from "./edit.ts";
import { DISMISSED, isAgentItem, isPersonal } from "./model.ts";
import type { Action, Item, NewAction } from "./types.ts";

/** Close reason the hub writes for Done. */
export const DONE_REASON = "Done in Meat Mode";

/** The hub calls the pipeline makes: api.ts in the app, an in-memory fake in tests. */
export interface ActionsApi {
  postAction(a: NewAction): Promise<Action>;
  /** Throws ApiError 409 when the action was already applied. */
  cancelAction(id: string): Promise<unknown>;
}

export interface ActionsDeps {
  api: ActionsApi;
  newId: () => string;
  now: () => Date;
  /** The local copy of an item, which a patch changes in place. */
  item: (key: string) => Item | undefined;
  /** A post or an Undo failed; the pipeline carries on with the rest. */
  onError: (during: "save" | "undo", e: Error) => void;
}

/** One action the hub took, with its reply. */
export interface Posted {
  sent: NewAction;
  applied: Action;
}

/** What perform posted, for its Undo. Empty when every post failed. */
export interface Handle {
  posted: Posted[];
}

/** Whether a done moved a repeating personal item on to its next date instead of closing it. */
export function movedOn(p: Posted): boolean {
  return p.sent.kind === "done" && !!p.applied.payload.was;
}

/** Whether a done on this item moves it on rather than closing it: a repeating personal item. */
export function repeatsOnDone(item: Item): boolean {
  return isPersonal(item) && !!item.repeat;
}

/**
 * What an action does to the local copy before the hub answers. False when it
 * changes nothing there, as a note or an ask waits for the hub's reply.
 */
export function patchItem(i: Item, a: NewAction, now: Date): boolean {
  const p = a.payload;
  switch (a.kind) {
    case "done":
      // The hub moves a repeating personal item on; its next date comes with the reply.
      if (repeatsOnDone(i)) return false;
      close(i, DONE_REASON, now);
      return true;
    case "dismiss":
      close(i, DISMISSED, now);
      return true;
    case "reopen":
      i.status = "open";
      i.closedAt = null;
      i.closeReason = undefined;
      return true;
    case "snooze":
      i.deferUntil = p.until;
      return true;
    case "edit":
      if (p.set) applyEdit(i, p.set);
      return true;
    case "note-edit":
      i.comments = (i.comments ?? []).map((c) => (c.id === p.commentId ? { ...c, text: p.text ?? "" } : c));
      return true;
    case "note-delete":
      i.comments = (i.comments ?? []).filter((c) => c.id !== p.commentId);
      return true;
    case "note-restore":
      if (p.comment) i.comments = restoreComment(i.comments, p.comment);
      return true;
    default:
      return false;
  }
}

function close(i: Item, reason: string, now: Date) {
  i.status = "closed";
  i.closedAt = now.toISOString();
  i.closeReason = reason;
}

/** The action that reverses a closing, reopening or snooze, when the hub has already applied it. */
export function inverse(a: NewAction, id: string): NewAction | null {
  switch (a.kind) {
    case "done":
    case "dismiss":
      return { id, kind: "reopen", key: a.key, payload: {} };
    case "reopen":
      return { id, kind: "dismiss", key: a.key, payload: {} };
    case "snooze":
      return { id, kind: "snooze", key: a.key, payload: { until: a.payload.previous ?? null, previous: a.payload.until ?? null } };
    default:
      return null;
  }
}

export function createActions(deps: ActionsDeps) {
  /** Applies an action to the local copy and marks it waiting for its bridge; the hub applies personal ones at once. */
  function patch(a: NewAction) {
    const it = a.key ? deps.item(a.key) : undefined;
    if (!it || !patchItem(it, a, deps.now())) return;
    it.pending = isAgentItem(it);
    it.error = undefined;
    // The Undo of a repeating done closes the sub-tasks it reopened.
    for (const id of a.payload.set?.subtasksDone ?? []) {
      const sub = deps.item(`${it.bridge}/${it.repo}/${id}`);
      if (sub) close(sub, DONE_REASON, deps.now());
    }
  }

  async function post(a: NewAction): Promise<Action | null> {
    try {
      return await deps.api.postAction(a);
    } catch (e) {
      deps.onError("save", e as Error);
      return null;
    }
  }

  /**
   * Patches the local copy for every action, calls `onPatched` so the caller
   * can draw it, then posts them in order. A failed post is reported and left
   * out of the handle.
   */
  async function perform(actions: NewAction[], opts: { onPatched?: () => void } = {}): Promise<Handle> {
    for (const a of actions) patch(a);
    opts.onPatched?.();
    const posted: Posted[] = [];
    for (const sent of actions) {
      const applied = await post(sent);
      if (applied) posted.push({ sent, applied });
    }
    return { posted };
  }

  /**
   * Undoes what perform posted, last first, so a parent reopens before the
   * sub-tasks that closed ahead of it. An action whose reply says how to
   * undo it (an edit, a note change, a done that moved a repeating item on)
   * gets that edit, applied locally before `onPatched`. Any other is
   * cancelled while still unapplied; once applied (409) it gets its inverse.
   * A failed cancel stops the Undo there.
   */
  async function undo(h: Handle, opts: { onPatched?: () => void } = {}): Promise<void> {
    const plan = h.posted.map((p) => ({ ...p, u: undoOf(p.applied, p.sent.key ?? "", deps.newId()) })).reverse();
    for (const { u } of plan) if (u) patch(u);
    opts.onPatched?.();
    for (const { sent, u } of plan) {
      if (u) {
        await post(u);
        continue;
      }
      if (!inverse(sent, "")) continue;
      try {
        await deps.api.cancelAction(sent.id);
      } catch (e) {
        if (!(e instanceof ApiError && e.status === 409)) {
          deps.onError("undo", e as Error);
          return;
        }
        await post(inverse(sent, deps.newId())!);
      }
    }
  }

  return { perform, undo };
}

export type Actions = ReturnType<typeof createActions>;

import { test } from "node:test";
import assert from "node:assert/strict";
import type { Action, Item } from "../../src/types.ts";
import { byLine, changeText, holds, shownChanges, undoFor, whoText, type HistoryContext } from "../../src/changes.ts";

// Wednesday 14 October 2026, 10:00 local time.
const now = new Date(2026, 9, 14, 10, 0);
const at = (month: number, day: number, h: number, m = 0) => new Date(2026, month - 1, day, h, m).toISOString();
const ctx: HistoryContext = { now, title: (id) => (id === "mm-2" ? "Garden jobs" : "") };

function item(over: Partial<Item> = {}): Item {
  return {
    key: "hub/personal/mm-1",
    bridge: "hub",
    repo: "personal",
    id: "mm-1",
    title: "Water the plants",
    status: "open",
    priority: 2,
    dueAt: at(11, 1, 12),
    createdAt: now.toISOString(),
    updatedAt: now.toISOString(),
    comments: [],
    ...over,
  };
}

let seq = 0;
function act(kind: Action["kind"], payload: Action["payload"] = {}, over: Partial<Action> = {}): Action {
  seq++;
  return { id: `a-${seq}`, seq, kind, bridge: "hub", repo: "personal", beadId: "mm-1", payload, author: "alex-app", status: "applied", createdAt: at(10, 14, 9, 30), ...over };
}

test("each change in plain words", () => {
  const it = item();
  const text = (a: Action) => changeText(a, it, ctx);
  assert.equal(text(act("edit", { set: { due: at(11, 1, 12) } })), "Moved to Sun, Nov 1 at 12:00");
  assert.equal(text(act("edit", { set: { due: at(10, 14, 18) } })), "Moved to today at 18:00");
  assert.equal(text(act("done")), "Marked done");
  assert.equal(text(act("done", { was: { due: at(10, 13, 9) } })), "Marked done; was due yesterday at 9:00");
  assert.equal(text(act("dismiss")), "Deleted");
  assert.equal(text(act("reopen")), "Reopened");
  assert.equal(text(act("add", { title: "Water the plants" })), "Added");
  assert.equal(text(act("add", { title: "Buy soil", parent: "mm-1" }, { beadId: "mm-3" })), "Added sub-task Buy soil");
  assert.equal(text(act("edit", { set: { labels: ["garden", "home"] }, was: { labels: ["home", "old"] } })), "Tags: +garden −old");
  assert.equal(text(act("edit", { set: { labels: ["garden"] }, was: { labels: [] } })), "Tags: +garden");
  assert.equal(text(act("edit", { set: { labels: [] }, was: { labels: ["garden"] } })), "Removed all tags");
  assert.equal(text(act("edit", { set: { title: "Water the ferns", priority: 1 } })), "Renamed to “Water the ferns”; Priority: High");
  assert.equal(text(act("edit", { set: { clearDue: true } })), "Removed the date");
  assert.equal(text(act("edit", { set: { repeat: { every: "week", weekdays: [0], timeZone: "UTC" } } })), "Repeat: every Sunday");
  assert.equal(text(act("edit", { set: { clearRepeat: true } })), "Stopped repeating");
  assert.equal(text(act("edit", { set: { remindBefore: 30 } })), "Reminds 30 minutes before");
  assert.equal(text(act("edit", { set: { clearRemindBefore: true } })), "Reminds at the usual time");
  assert.equal(text(act("edit", { set: { parent: "mm-2" } })), "Moved under Garden jobs");
  assert.equal(text(act("edit", { set: { parent: "" } })), "Made a top-level task");
  assert.equal(text(act("edit", { set: { description: "" } })), "Removed the details");
  assert.equal(text(act("snooze", { until: at(10, 15, 9) })), "Snoozed to tomorrow at 9:00");
  assert.equal(text(act("snooze", { until: null })), "Snooze cleared");
  assert.equal(text(act("note", { text: "Use the rain water" })), "Added a note “Use the rain water”");
  assert.equal(text(act("note-delete", { commentId: "c1", comment: { id: "c1", author: "alex-app", text: "Old note", createdAt: at(10, 1, 9) } })), "Deleted the note “Old note”");
});

test("who made a change: You, or the agent and its machine", () => {
  assert.equal(whoText({ author: "alex-app" }), "You");
  assert.equal(whoText({ author: "planner", machine: "desk" }), "planner on desk");
  assert.equal(whoText({ author: "desk", machine: "desk" }), "desk");
  assert.equal(byLine(act("done", {}, { author: "planner", machine: "desk", createdAt: at(10, 14, 9, 55) }), now), "planner on desk · 5m ago");
});

test("History lists applied changes only", () => {
  const list = [act("done", {}, { status: "pending" }), act("note", { text: "x" }), act("reopen", {}, { status: "failed" })];
  assert.deepEqual(
    shownChanges(list).map((a) => a.kind),
    ["note"],
  );
});

test("an agent's edit undoes to what it was while nothing changed it since", () => {
  const it = item({ dueAt: at(11, 1, 12) });
  const agent = act("edit", { set: { due: at(11, 1, 12) }, was: { due: at(10, 20, 9) } }, { author: "planner", machine: "desk" });
  const history = [agent, act("add", { title: "Water the plants" })];
  assert.deepEqual(undoFor(history, 0, it, "u1"), { id: "u1", kind: "edit", key: it.key, payload: { set: { due: at(10, 20, 9) } } });
  // A later change to another field leaves it undoable.
  const rename = act("edit", { set: { title: "Water the ferns" }, was: { title: "Water the plants" } });
  assert.ok(undoFor([rename, ...history], 1, { ...it, title: "Water the ferns" }, "u1"));
  // A later move of the date takes over: only that one undoes.
  const moved = act("edit", { set: { due: at(11, 2, 8) }, was: { due: at(11, 1, 12) } });
  const after = { ...it, dueAt: at(11, 2, 8) };
  assert.equal(undoFor([moved, ...history], 1, after, "u1"), null);
  assert.ok(undoFor([moved, ...history], 0, after, "u1"));
  // A repeating done moves the date too.
  assert.equal(undoFor([act("done", { was: { due: at(11, 1, 12) } }), ...history], 1, after, "u1"), null);
  // An item that no longer shows what the edit set, however it changed, offers none.
  assert.equal(undoFor(history, 0, after, "u1"), null);
});

test("closing and reopening undo by their opposite, from the state before", () => {
  const closed = item({ status: "closed", closeReason: "Done in Meat Mode", closedAt: now.toISOString() });
  const deleted = item({ status: "closed", closeReason: "Dismissed", closedAt: now.toISOString() });
  const add = act("add", { title: "Water the plants" });
  const done = act("done");
  assert.deepEqual(undoFor([done, add], 0, closed, "u1")?.kind, "reopen");
  assert.deepEqual(undoFor([act("dismiss"), add], 0, deleted, "u1")?.kind, "reopen");
  // A delete of a done item would come back open, not done: no Undo.
  assert.equal(undoFor([act("dismiss"), done, add], 0, deleted, "u1"), null);
  // A reopen goes back to how it was closed.
  const open = item();
  assert.equal(undoFor([act("reopen"), act("dismiss"), add], 0, open, "u1")?.kind, "dismiss");
  assert.equal(undoFor([act("reopen"), done, add], 0, open, "u1")?.kind, "done");
  // A done on a repeating item would move it on instead of closing it.
  assert.equal(undoFor([act("reopen"), done, add], 0, item({ repeat: { every: "day", timeZone: "UTC" } }), "u1"), null);
  // A done that moved a repeating item on undoes by putting its date back.
  const movedOn = act("done", { was: { due: at(10, 13, 9), subtasksDone: ["mm-4"] } });
  assert.deepEqual(undoFor([movedOn, add], 0, open, "u1")?.payload, { set: { due: at(10, 13, 9), subtasksDone: ["mm-4"] } });
  // The item's status changed since: no Undo of the older close.
  assert.equal(undoFor([act("reopen"), done, add], 1, open, "u1"), null);
  assert.equal(undoFor([done, add], 0, open, "u1"), null);
});

test("snoozes and notes undo while they still hold", () => {
  const until = at(10, 15, 9);
  const snoozed = item({ deferUntil: until });
  const snooze = act("snooze", { until, previous: null });
  assert.deepEqual(undoFor([snooze], 0, snoozed, "u1")?.payload, { until: null, previous: until });
  assert.equal(undoFor([snooze], 0, item(), "u1"), null);

  const c = { id: "c1", author: "alex-app", text: "Use the rain water", createdAt: at(10, 1, 9) };
  const edited = act("note-edit", { commentId: "c1", text: "Use the tap", comment: c });
  assert.deepEqual(undoFor([edited], 0, item({ comments: [{ ...c, text: "Use the tap" }] }), "u1")?.payload, { commentId: "c1", text: c.text });
  assert.equal(undoFor([edited], 0, item({ comments: [c] }), "u1"), null);
  const gone = act("note-delete", { commentId: "c1", comment: c });
  assert.deepEqual(undoFor([gone], 0, item(), "u1")?.kind, "note-restore");
  assert.equal(undoFor([act("note-restore", { comment: c }), gone], 1, item({ comments: [c] }), "u1"), null);
});

test("adds, notes and sub-tasks' changes have no Undo here", () => {
  const it = item();
  assert.equal(undoFor([act("add", { title: "Water the plants" })], 0, it, "u1"), null);
  assert.equal(undoFor([act("note", { text: "x" })], 0, it, "u1"), null);
  assert.equal(undoFor([act("add", { title: "Buy soil", parent: "mm-1" }, { beadId: "mm-3" })], 0, it, "u1"), null);
  // An edit without what it replaced cannot be put back.
  assert.equal(undoFor([act("edit", { set: { priority: 1 } })], 0, item({ priority: 1 }), "u1"), null);
});

test("holds compares what an edit set with the item now", () => {
  const it = item({ labels: ["Garden"], remindBefore: 30 });
  assert.ok(holds(it, { labels: ["garden"], remindBefore: 30, due: it.dueAt! }));
  assert.ok(!holds(it, { clearRemindBefore: true }));
  assert.ok(!holds(it, { clearDue: true }));
  assert.ok(holds(item({ dueAt: null }), { clearDue: true }));
});

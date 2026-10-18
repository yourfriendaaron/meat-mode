import { test } from "node:test";
import assert from "node:assert/strict";
import type { Action, Item } from "../../src/types.ts";
import {
  addCheck,
  applyEdit,
  canEdit,
  checklistChip,
  diffEdit,
  draftOf,
  editError,
  priorityChip,
  priorityOptions,
  removeCheck,
  restoreComment,
  toggleCheck,
  undoOf,
} from "../../src/edit.ts";

// Wednesday 14 October 2026, 10:00 local time.
const now = new Date(2026, 9, 14, 10, 0);
const due = new Date(2026, 9, 15, 9, 0).toISOString();

function item(over: Partial<Item> = {}): Item {
  return {
    key: "hub/personal/pe-1",
    bridge: "hub",
    repo: "personal",
    id: "pe-1",
    title: "Water the plants",
    status: "open",
    priority: 2,
    dueAt: due,
    createdAt: now.toISOString(),
    updatedAt: now.toISOString(),
    comments: [],
    ...over,
  };
}

function action(kind: Action["kind"], payload: Action["payload"]): Action {
  return { id: "a1", seq: 1, kind, bridge: "hub", repo: "personal", beadId: "pe-1", payload, author: "alex-app", status: "applied", createdAt: now.toISOString() };
}

test("only the hub's personal list is editable", () => {
  assert.equal(canEdit(item()), true);
  assert.equal(canEdit(item({ bridge: "home" })), false);
});

test("an unchanged form sets nothing", () => {
  assert.equal(diffEdit(item(), draftOf(item()), now), null);
});

test("the set holds only the changed fields", () => {
  const it = item();
  const d = { ...draftOf(it), title: "  Water the ferns ", priority: 1, dueText: "friday 5pm" };
  assert.deepEqual(diffEdit(it, d, now), {
    title: "Water the ferns",
    priority: 1,
    due: new Date(2026, 9, 16, 17, 0).toISOString(),
  });
});

test("a due date typed as the one it has is no change", () => {
  const it = item();
  assert.equal(diffEdit(it, { ...draftOf(it), dueText: "tomorrow 9am" }, now), null);
});

test("clearing the due date sends clearDue only when there was one", () => {
  assert.deepEqual(diffEdit(item(), { ...draftOf(item()), clearDue: true }, now), { clearDue: true });
  const none = item({ dueAt: null });
  assert.equal(diffEdit(none, { ...draftOf(none), clearDue: true }, now), null);
});

test("a cleared description is sent as an empty string", () => {
  const it = item({ description: "Use the rain water." });
  assert.deepEqual(diffEdit(it, { ...draftOf(it), description: "" }, now), { description: "" });
});

test("editError names what blocks saving", () => {
  const it = item();
  assert.equal(editError(it, draftOf(it), now), "");
  assert.match(editError(it, { ...draftOf(it), title: "   " }, now), /title/);
  assert.match(editError(it, { ...draftOf(it), title: "x".repeat(1001) }, now), /1000/);
  assert.match(editError(it, { ...draftOf(it), description: "x".repeat(10001) }, now), /10000/);
  assert.match(editError(it, { ...draftOf(it), dueText: "whenever" }, now), /date/);
});

test("priority: 0 and 1 show a chip, and the picker keeps 0 and 4", () => {
  assert.equal(priorityChip(0), "Urgent");
  assert.equal(priorityChip(1), "High priority");
  assert.equal(priorityChip(2), "");
  assert.equal(priorityChip(4), "");
  assert.deepEqual(priorityOptions(2).map((o) => o.value), [1, 2, 3]);
  assert.deepEqual(priorityOptions(0).map((o) => o.label), ["Urgent", "High", "Normal", "Low"]);
  assert.deepEqual(priorityOptions(4).map((o) => o.label), ["High", "Normal", "Low", "Someday"]);
});

test("checklist chip counts done entries", () => {
  assert.equal(checklistChip(undefined), "");
  assert.equal(checklistChip([]), "");
  assert.equal(
    checklistChip([
      { id: "k1", text: "Soil", done: true },
      { id: "k2", text: "Pots", done: false },
      { id: "k3", text: "Seeds", done: true },
    ]),
    "2/3",
  );
});

test("checklist changes return a new list and leave the old one alone", () => {
  const list = [{ id: "k1", text: "Soil", done: false }];
  const added = addCheck(list, "Pots");
  assert.deepEqual(added, [{ id: "k1", text: "Soil", done: false }, { text: "Pots", done: false }]);
  assert.deepEqual(toggleCheck(added, 0)[0], { id: "k1", text: "Soil", done: true });
  assert.deepEqual(removeCheck(added, 0), [{ text: "Pots", done: false }]);
  assert.deepEqual(list, [{ id: "k1", text: "Soil", done: false }]);
});

test("applyEdit follows the set, clearDue included", () => {
  const it = item();
  applyEdit(it, { title: "Water the ferns", clearDue: true, priority: 1, checklist: [{ text: "Pots", done: false }] });
  assert.equal(it.title, "Water the ferns");
  assert.equal(it.dueAt, null);
  assert.equal(it.priority, 1);
  assert.deepEqual(it.checklist, [{ text: "Pots", done: false }]);
});

test("restoreComment puts a comment back by time", () => {
  const c = (id: string, h: number) => ({ id, author: "alex-app", text: id, createdAt: new Date(Date.UTC(2026, 9, 14, h)).toISOString() });
  assert.deepEqual(
    restoreComment([c("a", 8), c("c", 10)], c("b", 9)).map((x) => x.id),
    ["a", "b", "c"],
  );
  assert.deepEqual(restoreComment(null, c("a", 8)).map((x) => x.id), ["a"]);
});

test("undo of each edit kind comes from what the hub filled in", () => {
  const key = "hub/personal/pe-1";
  assert.deepEqual(undoOf(action("edit", { set: { title: "New" }, was: { title: "Old", clearDue: true } }), key, "u1"), {
    id: "u1",
    kind: "edit",
    key,
    payload: { set: { title: "Old", clearDue: true } },
  });
  const comment = { id: "c1", author: "alex-app", text: "Use the rain water.", createdAt: now.toISOString() };
  assert.deepEqual(undoOf(action("note-edit", { commentId: "c1", text: "Changed", comment }), key, "u2"), {
    id: "u2",
    kind: "note-edit",
    key,
    payload: { commentId: "c1", text: "Use the rain water." },
  });
  assert.deepEqual(undoOf(action("note-delete", { commentId: "c1", comment }), key, "u3"), {
    id: "u3",
    kind: "note-restore",
    key,
    payload: { comment },
  });
  assert.equal(undoOf(action("edit", { set: { title: "New" } }), key, "u4"), null);
  assert.equal(undoOf(action("done", {}), key, "u5"), null);
});

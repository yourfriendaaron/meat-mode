import { test } from "node:test";
import assert from "node:assert/strict";
import type { Action, Item } from "../../src/types.ts";
import {
  applyEdit,
  canEdit,
  diffEdit,
  draftOf,
  editError,
  moveTargets,
  priorityChip,
  priorityOptions,
  restoreComment,
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
  const d = { ...draftOf(it), title: "  Water the ferns ", priority: 1, date: "2026-10-16", time: "17:00" };
  assert.deepEqual(diffEdit(it, d, now), {
    title: "Water the ferns",
    priority: 1,
    due: new Date(2026, 9, 16, 17, 0).toISOString(),
  });
});

test("the draft holds the due time as a local date and time, and the same minute is no change", () => {
  const it = item();
  const d = draftOf(it);
  assert.deepEqual([d.date, d.time], ["2026-10-15", "09:00"]);
  assert.equal(diffEdit(it, { ...d, time: "09:00" }, now), null);
  // A due time with seconds keeps them while the minute stays.
  const odd = item({ dueAt: new Date(2026, 9, 15, 9, 0, 30).toISOString() });
  assert.equal(diffEdit(odd, draftOf(odd), now), null);
  // A date with no time takes 9:00.
  assert.deepEqual(diffEdit(it, { ...d, date: "2026-10-16", time: "" }, now), { due: new Date(2026, 9, 16, 9, 0).toISOString() });
  const none = draftOf(item({ dueAt: null }));
  assert.deepEqual([none.date, none.time], ["", ""]);
});

test("clearing the due date sends clearDue only when there was one", () => {
  assert.deepEqual(diffEdit(item(), { ...draftOf(item()), date: "" }, now), { clearDue: true });
  const none = item({ dueAt: null });
  assert.equal(diffEdit(none, { ...draftOf(none), date: "" }, now), null);
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
  assert.match(editError(it, { ...draftOf(it), date: "2026-02-30" }, now), /real date/);
  assert.match(editError(it, { ...draftOf(it), time: "25:00" }, now), /real date/);
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

test("applyEdit follows the set, clearDue included", () => {
  const it = item();
  applyEdit(it, { title: "Water the ferns", clearDue: true, priority: 1 });
  assert.equal(it.title, "Water the ferns");
  assert.equal(it.dueAt, null);
  assert.equal(it.priority, 1);
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

test("undo of a done that moved a repeating item on is an edit back", () => {
  const key = "hub/personal/pe-1";
  const was = { due: due, subtasksDone: ["pe-1.1"] };
  assert.deepEqual(undoOf(action("done", { was }), key, "u6"), { id: "u6", kind: "edit", key, payload: { set: was } });
});

const daily = { every: "day" as const, timeZone: "Europe/Paris", start: due };

test("the draft holds the item's repeat and lead time", () => {
  const d = draftOf(item({ repeat: { every: "week", interval: 2, weekdays: [1, 4], timeZone: "UTC" }, remindBefore: 30 }), "Europe/Paris");
  assert.deepEqual(
    { repeat: d.repeat, interval: d.interval, weekdays: d.weekdays, fromDone: d.fromDone, timeZone: d.timeZone, remindBefore: d.remindBefore },
    { repeat: "week", interval: "2", weekdays: [1, 4], fromDone: false, timeZone: "Europe/Paris", remindBefore: 30 },
  );
  const none = draftOf(item());
  assert.deepEqual([none.repeat, none.interval, none.remindBefore], ["", "1", null]);
});

test("setting a repeat sends the rule in the device's zone", () => {
  const it = item();
  const d = { ...draftOf(it, "Europe/Paris"), repeat: "week" as const, interval: " 2 ", weekdays: [4, 1] };
  assert.deepEqual(diffEdit(it, d, now), { repeat: { every: "week", interval: 2, weekdays: [1, 4], timeZone: "Europe/Paris" } });
});

test("a repeat changed only in its zone is no change", () => {
  const it = item({ repeat: daily });
  assert.equal(diffEdit(it, draftOf(it, "Asia/Tokyo"), now), null);
});

test("weekdays go only with a weekly rule that does not count from done", () => {
  const it = item();
  const base = { ...draftOf(it, "UTC"), weekdays: [1] };
  assert.deepEqual(diffEdit(it, { ...base, repeat: "month" }, now), { repeat: { every: "month", timeZone: "UTC" } });
  assert.deepEqual(diffEdit(it, { ...base, repeat: "week", fromDone: true }, now), { repeat: { every: "week", fromDone: true, timeZone: "UTC" } });
});

test("ending a repeat sends clearRepeat, except when the due date is cleared, which ends it on the hub", () => {
  const it = item({ repeat: daily });
  assert.deepEqual(diffEdit(it, { ...draftOf(it), repeat: "" }, now), { clearRepeat: true });
  assert.deepEqual(diffEdit(it, { ...draftOf(it), repeat: "", date: "" }, now), { clearDue: true });
});

test("a repeat needs a due date and an interval from 1 to 100", () => {
  const it = item({ dueAt: null });
  const d = { ...draftOf(it), repeat: "day" as const };
  assert.match(editError(it, d, now), /needs a date/);
  assert.equal(editError(it, { ...d, date: "2026-10-15", time: "08:00" }, now), "");
  assert.match(editError(item(), { ...draftOf(item()), repeat: "day", date: "" }, now), /needs a date/);
  const dated = { ...draftOf(item()), repeat: "day" as const };
  for (const interval of ["0", "101", "1.5", "two", ""]) assert.match(editError(item(), { ...dated, interval }, now), /^Repeat every 1 to 100/);
  assert.equal(editError(item(), { ...dated, interval: "100" }, now), "");
});

test("an end date resends the whole repeat with it, and clearing it resends the repeat without", () => {
  const it = item({ repeat: { every: "week", weekdays: [0], timeZone: "UTC", start: due } });
  const d = draftOf(it, "UTC");
  assert.equal(d.until, "");
  assert.deepEqual(diffEdit(it, { ...d, until: "2026-11-26" }, now), { repeat: { every: "week", weekdays: [0], until: "2026-11-26", timeZone: "UTC" } });
  const ends = item({ repeat: { every: "week", weekdays: [0], until: "2026-11-26", timeZone: "UTC", start: due } });
  const e = draftOf(ends, "UTC");
  assert.equal(e.until, "2026-11-26");
  assert.equal(diffEdit(ends, e, now), null);
  assert.deepEqual(diffEdit(ends, { ...e, until: "" }, now), { repeat: { every: "week", weekdays: [0], timeZone: "UTC" } });
  // An end date left in the form goes nowhere once the repeat is off.
  assert.deepEqual(diffEdit(ends, { ...e, repeat: "" }, now), { clearRepeat: true });
});

test("an end date must be a real date on or after the due date", () => {
  const it = item({ repeat: daily });
  const d = draftOf(it, "UTC");
  // The due date is Thursday 15 October.
  assert.equal(editError(it, { ...d, until: "2026-10-15" }, now), "");
  assert.match(editError(it, { ...d, until: "2026-10-14" }, now), /^End the repeat on or after its due date/);
  assert.match(editError(it, { ...d, until: "2026-02-30" }, now), /^End the repeat on a real date/);
  assert.equal(editError(it, { ...d, until: "2026-10-14", date: "2026-10-14", time: "20:00" }, now), "");
  assert.equal(editError(it, { ...d, until: "2026-10-01", repeat: "" }, now), "");
});

test("the lead time sends remindBefore, or clearRemindBefore for the default", () => {
  const it = item();
  assert.deepEqual(diffEdit(it, { ...draftOf(it), remindBefore: 30 }, now), { remindBefore: 30 });
  assert.deepEqual(diffEdit(it, { ...draftOf(it), remindBefore: 0 }, now), { remindBefore: 0 });
  const own = item({ remindBefore: 30 });
  assert.deepEqual(diffEdit(own, { ...draftOf(own), remindBefore: null }, now), { clearRemindBefore: true });
  assert.equal(diffEdit(own, draftOf(own), now), null);
});

test("applyEdit follows repeat and lead fields, and clearDue ends the repeat", () => {
  const it = item();
  applyEdit(it, { repeat: daily, remindBefore: 15 });
  assert.deepEqual([it.repeat, it.remindBefore], [daily, 15]);
  applyEdit(it, { clearRemindBefore: true, clearDue: true });
  assert.deepEqual([it.repeat, it.remindBefore, it.dueAt], [null, null, null]);
  applyEdit(it, { due, repeat: daily });
  applyEdit(it, { clearRepeat: true });
  assert.equal(it.repeat, null);
});

test("moving sends parent only when it changed, \"\" for the top level, and applyEdit follows it", () => {
  const it = item();
  assert.equal(draftOf(it).parent, "");
  assert.equal(diffEdit(it, draftOf(it), now), null);
  assert.deepEqual(diffEdit(it, { ...draftOf(it), parent: "pe-3" }, now), { parent: "pe-3" });
  const sub = item({ parent: "pe-3" });
  assert.equal(draftOf(sub).parent, "pe-3");
  assert.deepEqual(diffEdit(sub, { ...draftOf(sub), parent: "" }, now), { parent: "" });
  applyEdit(it, { parent: "pe-3" });
  assert.equal(it.parent, "pe-3");
  applyEdit(it, { parent: "" });
  assert.equal(it.parent, undefined);
});

test("an item moves only under open top-level items of its own list, and not at all with sub-tasks", () => {
  const it = item();
  const other = (id: string, over: Partial<Item> = {}) => item({ key: `hub/personal/${id}`, id, title: `Task ${id}`, ...over });
  const all = [
    it,
    other("pe-3", { title: "Call Sam" }),
    other("pe-2", { title: "Buy seeds" }),
    other("pe-4", { status: "closed" }),
    other("pe-5", { parent: "pe-3" }),
    other("pending-1"),
    item({ key: "hub/house/ho-1", repo: "house", id: "ho-1" }),
  ];
  assert.deepEqual(moveTargets(it, all).map((i) => i.id), ["pe-2", "pe-3"]);
  // Its own sub-task, even a closed one, keeps it top-level.
  assert.deepEqual(moveTargets(it, [...all, other("pe-6", { parent: "pe-1", status: "closed" })]), []);
  assert.deepEqual(moveTargets(item({ key: "home/garden/ga-1", bridge: "home", repo: "garden", id: "ga-1" }), all), []);
});

test("tags: the form shows them without human and sends the whole list when it changed", () => {
  const it = item({ labels: ["garden", "errands"] });
  const d = draftOf(it);
  assert.equal(d.tags, "garden errands");
  assert.equal(diffEdit(it, d, now), null);
  assert.deepEqual(diffEdit(it, { ...d, tags: "garden, @weekend  Garden" }, now), { labels: ["garden", "weekend"] });
  assert.deepEqual(diffEdit(it, { ...d, tags: "  " }, now), { labels: [] });
  assert.equal(diffEdit(item(), { ...draftOf(item()), tags: "human" }, now), null);
});

test("tags: the hub's rules refuse the form before it sends", () => {
  const it = item();
  assert.equal(editError(it, { ...draftOf(it), tags: "a".repeat(51) }, now), `The tag "${"a".repeat(51)}" is longer than 50 characters.`);
  const many = Array.from({ length: 21 }, (_, i) => `t${i}`).join(" ");
  assert.equal(editError(it, { ...draftOf(it), tags: many }, now), "An item has at most 20 tags.");
  assert.equal(editError(it, { ...draftOf(it), tags: "ok\u0007bell" }, now), 'The tag "ok\u0007bell" has a space, comma or control character; a tag is one word.');
});

test("applyEdit sets the tags, and an empty list clears them", () => {
  const it = item({ labels: ["garden"] });
  applyEdit(it, { labels: ["garden", "weekend"] });
  assert.deepEqual(it.labels, ["garden", "weekend"]);
  applyEdit(it, { labels: [] });
  assert.equal(it.labels, undefined);
});

test("the Undo of a tag edit sets the tags it had, from was", () => {
  const applied = action("edit", { set: { labels: ["weekend"] }, was: { labels: [] } });
  assert.deepEqual(undoOf(applied, "hub/personal/pe-1", "u1"), { id: "u1", kind: "edit", key: "hub/personal/pe-1", payload: { set: { labels: [] } } });
});

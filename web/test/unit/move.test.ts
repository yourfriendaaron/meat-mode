// Moving a month entry to another day. A zone with daylight saving time, so a
// move across the clock change keeps the time of day.
process.env.TZ = "Europe/London";

import { test } from "node:test";
import assert from "node:assert/strict";
import type { Item } from "../../src/types.ts";
import { canMove, decideMove, ymdIn, type MoveDecision } from "../../src/move.ts";

// Wednesday 14 October 2026, 10:00 local time.
const now = new Date(2026, 9, 14, 10, 0);
const local = (m: number, d: number, h = 0, min = 0) => new Date(2026, m - 1, d, h, min);

function item(over: Partial<Item>): Item {
  const repo = over.repo ?? "personal";
  const bridge = repo === "personal" ? "hub" : "home";
  return {
    key: `${bridge}/${repo}/bd-1`,
    bridge,
    repo,
    id: "bd-1",
    title: "Water the plants",
    status: "open",
    priority: 2,
    createdAt: local(10, 1, 9).toISOString(),
    updatedAt: local(10, 1, 9).toISOString(),
    comments: [],
    ...over,
  };
}

const personal = (over: Partial<Item> = {}) => item({ dueAt: local(10, 16, 9, 30).toISOString(), ...over });
const agent = (over: Partial<Item> = {}) => item({ repo: "garden", title: "Order compost", dueAt: local(10, 16, 18).toISOString(), ...over });

function moved(d: MoveDecision) {
  assert.equal(d.kind, "move");
  return d as Extract<MoveDecision, { kind: "move" }>;
}

test("what can move: a personal due date, an agent item's due or snooze when its list snoozes", () => {
  assert.equal(canMove(personal(), "due", true), true);
  assert.equal(canMove(personal(), "snooze", true), false);
  assert.equal(canMove(personal(), "repeat", true), false);
  assert.equal(canMove(agent(), "due", true), true);
  assert.equal(canMove(agent(), "snooze", true), true);
  assert.equal(canMove(agent(), "repeat", true), false);
  assert.equal(canMove(agent(), "due", false), false);
  assert.equal(canMove(personal({ status: "closed" }), "due", true), false);
  assert.equal(canMove(agent({ status: "closed" }), "due", true), false);
});

test("a personal item's due date moves to the day at the same time of day", () => {
  const it = personal();
  const d = moved(decideMove(it, { kind: "due", at: new Date(it.dueAt!) }, local(10, 20), now, true, "a1"));
  assert.deepEqual(d.action, { id: "a1", kind: "edit", key: it.key, payload: { set: { due: local(10, 20, 9, 30).toISOString() } } });
  assert.equal(d.toast, "Moved to Tue, Oct 20");
});

test("across the clock change the local time of day stays", () => {
  const it = personal();
  const d = moved(decideMove(it, { kind: "due", at: new Date(it.dueAt!) }, local(10, 27), now, true, "a1"));
  // 9:30 in summer time is 8:30 UTC; after the change it is 9:30 UTC.
  assert.equal(d.action.payload.set?.due, "2026-10-27T09:30:00.000Z");
  assert.equal(it.dueAt, "2026-10-16T08:30:00.000Z");
});

test("a repeating item moves its series, but not past the repeat's end", () => {
  const it = personal({ repeat: { every: "week", timeZone: "Europe/London", until: "2026-10-30" } });
  const at = new Date(it.dueAt!);
  moved(decideMove(it, { kind: "due", at }, local(10, 30), now, true, "a1"));
  const d = decideMove(it, { kind: "due", at }, local(10, 31), now, true, "a1");
  assert.deepEqual(d, { kind: "refuse", text: "The repeat ends on Oct 30. Pick that day or an earlier one." });
});

test("the repeat's end is a date in the rule's own zone", () => {
  // 23:30 in London on 30 October is already 31 October in Tokyo.
  const it = personal({ dueAt: local(10, 16, 23, 30).toISOString(), repeat: { every: "day", timeZone: "Asia/Tokyo", until: "2026-10-30" } });
  const d = decideMove(it, { kind: "due", at: new Date(it.dueAt!) }, local(10, 30), now, true, "a1");
  assert.equal(d.kind, "refuse");
  assert.equal(ymdIn(local(10, 30, 23, 30), "Asia/Tokyo"), "2026-10-31");
  assert.equal(ymdIn(local(10, 30, 23, 30), "Not/AZone"), "2026-10-30");
});

test("the same day does nothing", () => {
  const it = personal();
  assert.deepEqual(decideMove(it, { kind: "due", at: new Date(it.dueAt!) }, local(10, 16), now, true, "a1"), { kind: "none" });
});

test("a day before today is refused", () => {
  const it = personal();
  assert.deepEqual(decideMove(it, { kind: "due", at: new Date(it.dueAt!) }, local(10, 13), now, true, "a1"), {
    kind: "refuse",
    text: "That day has passed. Pick today or a later day.",
  });
  const ag = agent();
  assert.equal(decideMove(ag, { kind: "due", at: new Date(ag.dueAt!) }, local(10, 1), now, true, "a1").kind, "refuse");
});

test("a personal due date may land earlier today; it is then overdue", () => {
  const it = personal();
  const d = moved(decideMove(it, { kind: "due", at: new Date(it.dueAt!) }, local(10, 14), now, true, "a1"));
  assert.equal(d.action.payload.set?.due, local(10, 14, 9, 30).toISOString());
});

test("an overdue item moves from its past day to a later one", () => {
  const it = personal({ dueAt: local(10, 2, 8).toISOString() });
  const d = moved(decideMove(it, { kind: "due", at: new Date(it.dueAt!) }, local(10, 15), now, true, "a1"));
  assert.equal(d.action.payload.set?.due, local(10, 15, 8).toISOString());
});

test("an agent item snoozes to the day at the entry's time, with the snooze it replaces", () => {
  const it = agent({ deferUntil: local(10, 15, 9).toISOString() });
  const d = moved(decideMove(it, { kind: "due", at: new Date(it.dueAt!) }, local(10, 22), now, true, "a2"));
  assert.deepEqual(d.action, {
    id: "a2",
    kind: "snooze",
    key: it.key,
    payload: { until: local(10, 22, 18).toISOString(), previous: local(10, 15, 9).toISOString() },
  });
  assert.equal(d.toast, "Snoozed to Thu, Oct 22 18:00");
  const s = moved(decideMove(it, { kind: "snooze", at: new Date(it.deferUntil!) }, local(10, 15 + 1), now, true, "a3"));
  assert.equal(s.action.payload.until, local(10, 16, 9).toISOString());
  assert.equal(s.toast, "Snoozed to Fri, Oct 16 9:00");
});

test("an agent item with no snooze sends previous null", () => {
  const it = agent();
  const d = moved(decideMove(it, { kind: "due", at: new Date(it.dueAt!) }, local(10, 15), now, true, "a2"));
  assert.equal(d.action.payload.previous, null);
  assert.equal(d.toast, "Snoozed to Tomorrow 18:00");
});

test("an agent item's snooze to a time already gone today is refused; a later time today is fine", () => {
  const early = agent({ dueAt: local(10, 16, 8).toISOString() });
  assert.deepEqual(decideMove(early, { kind: "due", at: new Date(early.dueAt!) }, local(10, 14), now, true, "a1"), {
    kind: "refuse",
    text: "That time has passed today. Pick a later day.",
  });
  const late = agent();
  const d = moved(decideMove(late, { kind: "due", at: new Date(late.dueAt!) }, local(10, 14), now, true, "a1"));
  assert.equal(d.toast, "Snoozed to 18:00");
});

test("entries that cannot move are refused", () => {
  const it = personal({ deferUntil: local(10, 18, 9).toISOString(), repeat: { every: "day", timeZone: "Europe/London" } });
  for (const kind of ["snooze", "repeat"] as const) {
    assert.deepEqual(decideMove(it, { kind, at: local(10, 18, 9) }, local(10, 20), now, true, "a1"), {
      kind: "refuse",
      text: "This entry cannot move to another day.",
    });
  }
  const ag = agent();
  assert.equal(decideMove(ag, { kind: "due", at: new Date(ag.dueAt!) }, local(10, 20), now, false, "a1").kind, "refuse");
  assert.equal(decideMove(agent(), { kind: "repeat", at: local(10, 20, 9) }, local(10, 21), now, true, "a1").kind, "refuse");
});

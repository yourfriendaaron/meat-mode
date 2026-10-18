import { test } from "node:test";
import assert from "node:assert/strict";
import { firstOccurrence, repeatLabel, sameRule } from "../../src/repeat.ts";
import { LEAD_CHOICES, leadLabel, leadOptions } from "../../src/lead.ts";

// Wednesday 14 October 2026, 10:00 local time.
const now = new Date(2026, 9, 14, 10, 0);

test("a repeat in plain words", () => {
  const cases: [Parameters<typeof repeatLabel>[0], string][] = [
    [{ every: "day" }, "Every day"],
    [{ every: "day", interval: 3 }, "Every 3 days"],
    [{ every: "week" }, "Every week"],
    [{ every: "week", weekdays: [4, 1] }, "Every week on Mon, Thu"],
    [{ every: "week", interval: 2, weekdays: [1, 4] }, "Every 2 weeks on Mon, Thu"],
    [{ every: "week", weekdays: [0, 6] }, "Every week on Sat, Sun"],
    [{ every: "week", weekdays: [1, 2, 3, 4, 5] }, "Every weekday"],
    [{ every: "week", interval: 2, weekdays: [1, 2, 3, 4, 5] }, "Every 2 weeks on Mon, Tue, Wed, Thu, Fri"],
    [{ every: "month" }, "Every month"],
    [{ every: "month", interval: 6 }, "Every 6 months"],
    [{ every: "year" }, "Every year"],
    [{ every: "day", interval: 3, fromDone: true }, "Every 3 days after done"],
    [{ every: "week", interval: 1 }, "Every week"],
  ];
  for (const [r, want] of cases) assert.equal(repeatLabel(r), want);
});

test("the first occurrence is the first allowed day past now", () => {
  const local = (d: Date) => `${d.getDate()} ${d.getHours()}:${String(d.getMinutes()).padStart(2, "0")}`;
  assert.equal(local(firstOccurrence({ every: "day" }, now, 20, 30)), "14 20:30");
  assert.equal(local(firstOccurrence({ every: "day" }, now, 9, 0)), "15 9:00");
  assert.equal(local(firstOccurrence({ every: "day" }, now, 10, 0)), "15 10:00");
  assert.equal(local(firstOccurrence({ every: "week", weekdays: [3] }, now, 9, 0)), "21 9:00");
  assert.equal(local(firstOccurrence({ every: "week", weekdays: [3] }, now, 11, 0)), "14 11:00");
  assert.equal(local(firstOccurrence({ every: "week", weekdays: [0, 6] }, now, 9, 0)), "17 9:00");
  assert.equal(local(firstOccurrence({ every: "month" }, now, 8, 0)), "15 8:00");
});

test("rules compare by what they repeat, not their zone or start", () => {
  assert.ok(sameRule({ every: "day" }, { every: "day", interval: 1, timeZone: "UTC", start: "2026-10-14T09:00:00Z" } as never));
  assert.ok(sameRule({ every: "week", weekdays: [4, 1] }, { every: "week", weekdays: [1, 4] }));
  assert.ok(sameRule(null, undefined));
  assert.ok(!sameRule({ every: "day" }, null));
  assert.ok(!sameRule({ every: "day" }, { every: "day", interval: 2 }));
  assert.ok(!sameRule({ every: "week", weekdays: [1] }, { every: "week" }));
  assert.ok(!sameRule({ every: "day" }, { every: "day", fromDone: true }));
});

test("lead times read as people say them", () => {
  assert.deepEqual(LEAD_CHOICES.map(leadLabel), [
    "No early reminder",
    "5 minutes before",
    "10 minutes before",
    "15 minutes before",
    "30 minutes before",
    "1 hour before",
    "2 hours before",
    "1 day before",
  ]);
  assert.equal(leadLabel(45), "45 minutes before");
  assert.equal(leadLabel(2880), "2 days before");
});

test("a lead time set elsewhere joins the choices in order", () => {
  assert.deepEqual(leadOptions(45), [0, 5, 10, 15, 30, 45, 60, 120, 1440]);
  assert.deepEqual(leadOptions(30), LEAD_CHOICES);
  assert.deepEqual(leadOptions(null), LEAD_CHOICES);
});

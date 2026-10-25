import { test } from "node:test";
import assert from "node:assert/strict";
import type { Item } from "../../src/types.ts";
import { draftOf, type EditDraft } from "../../src/edit.ts";
import { repeatWords } from "../../src/repeat.ts";
import { countFromHint, fitWeekdays, readTyped, scheduleCard, scheduleReadout, scheduleSave, whenLine } from "../../src/schedule.ts";

// Wednesday 14 October 2026, 10:00 local time.
const now = new Date(2026, 9, 14, 10, 0);
const at = (month: number, day: number, h: number, m = 0) => new Date(2026, month - 1, day, h, m);

function item(over: Partial<Item> = {}): Item {
  return {
    key: "hub/personal/pe-1",
    bridge: "hub",
    repo: "personal",
    id: "pe-1",
    title: "Check on the plants",
    status: "open",
    priority: 2,
    dueAt: at(10, 14, 18).toISOString(),
    createdAt: now.toISOString(),
    updatedAt: now.toISOString(),
    comments: [],
    ...over,
  };
}

/** The weekly check-in that counts from when it is done. */
const fromDone = () => item({ repeat: { every: "week", fromDone: true, timeZone: "UTC", start: at(10, 14, 18).toISOString() } });
/** One on a fixed schedule, every Wednesday. */
const weekly = () => item({ repeat: { every: "week", weekdays: [3], timeZone: "UTC", start: at(10, 14, 18).toISOString() } });

function typed(text: string, d: EditDraft): EditDraft {
  const out = readTyped(text, now, d);
  assert.ok(out && "draft" in out, `read ${text}: ${JSON.stringify(out)}`);
  return out.draft;
}

test("when it is due, in plain words", () => {
  assert.equal(whenLine(null, now), "No date");
  assert.equal(whenLine(at(10, 14, 18), now), "Today at 18:00");
  assert.equal(whenLine(at(10, 15, 9), now), "Tomorrow at 9:00");
  assert.equal(whenLine(at(10, 13, 9), now), "Yesterday at 9:00");
  assert.equal(whenLine(at(11, 1, 12), now), "Sun, Nov 1 at 12:00");
  assert.equal(whenLine(new Date(2027, 0, 3, 8, 30), now), "Sun, Jan 3, 2027 at 8:30");
});

test("a repeat as a sentence", () => {
  assert.equal(repeatWords({ every: "week", fromDone: true }, now), "Then 1 week after you finish it");
  assert.equal(repeatWords({ every: "day", interval: 3, fromDone: true }, now), "Then 3 days after you finish it");
  assert.equal(repeatWords({ every: "week", weekdays: [0] }, now), "Every Sunday");
  assert.equal(repeatWords({ every: "week", weekdays: [4, 1] }, now), "Every Mon and Thu");
  assert.equal(repeatWords({ every: "week", weekdays: [1, 3, 5] }, now), "Every Mon, Wed and Fri");
  assert.equal(repeatWords({ every: "week", weekdays: [1, 2, 3, 4, 5] }, now), "Every weekday");
  assert.equal(repeatWords({ every: "week", interval: 2, weekdays: [1, 4], until: "2026-12-31" }, now), "Every 2 weeks on Mon and Thu, until Dec 31");
  assert.equal(repeatWords({ every: "day" }, now), "Every day");
  assert.equal(repeatWords({ every: "month", interval: 3 }, now), "Every 3 months");
  assert.equal(repeatWords({ every: "year" }, now), "Every year");
});

test("the schedule card says when and how it repeats", () => {
  assert.deepEqual(scheduleCard(fromDone(), now), { when: "Today at 18:00", repeat: "Then 1 week after you finish it" });
  assert.deepEqual(scheduleCard(item(), now), { when: "Today at 18:00", repeat: "Does not repeat" });
  assert.deepEqual(scheduleCard(item({ dueAt: null }), now), { when: "Add a date", repeat: "" });
});

test("the typed field sets the date, time and repeat the way quick add reads them", () => {
  const d = typed("every sunday 12pm", draftOf(fromDone(), "UTC"));
  assert.deepEqual(
    { date: d.date, time: d.time, repeat: d.repeat, interval: d.interval, weekdays: d.weekdays, fromDone: d.fromDone },
    { date: "2026-10-18", time: "12:00", repeat: "week", interval: "1", weekdays: [0], fromDone: false },
  );
  assert.equal(scheduleReadout(d, now), "Every Sunday at 12:00. Next: Sun, Oct 18.");

  const two = typed("mondays and thursdays at 8", draftOf(item()));
  assert.deepEqual([two.date, two.time, two.weekdays], ["2026-10-15", "08:00", [1, 4]]);

  const until = typed("every 2 weeks on fri until dec 31", draftOf(item()));
  assert.deepEqual([until.interval, until.weekdays, until.until], ["2", [5], "2026-12-31"]);
});

test("a typed date with no repeat keeps the draft's repeat, and a repeat with no time keeps its time", () => {
  const moved = typed("tomorrow 9am", draftOf(weekly()));
  assert.deepEqual([moved.date, moved.time, moved.repeat, moved.weekdays], ["2026-10-15", "09:00", "week", [3]]);
  const kept = typed("every sunday", draftOf(fromDone()));
  assert.deepEqual([kept.date, kept.time], ["2026-10-18", "18:00"]);
});

test("the typed field says what it could not read", () => {
  assert.equal(readTyped("  ", now, draftOf(item())), null);
  assert.deepEqual(readTyped("whenever", now, draftOf(item())), { error: "Could not read that. Try every sunday 12pm, or tomorrow 9am." });
  assert.deepEqual(readTyped("every sunday 12pm please", now, draftOf(item())), { error: 'Could not read "please". Try every sunday 12pm, or tomorrow 9am.' });
  assert.deepEqual(readTyped("every day from friday until today", now, draftOf(item())), { error: "The end date is before the first date." });
});

test("the readout follows the controls", () => {
  const d = draftOf(fromDone());
  assert.equal(scheduleReadout(d, now), "Today at 18:00, then 1 week after you finish it.");
  assert.equal(scheduleReadout({ ...d, repeat: "" }, now), "Today at 18:00. It does not repeat.");
  assert.equal(scheduleReadout({ ...d, fromDone: false, weekdays: [0], date: "2026-11-01", time: "12:00" }, now), "Every Sunday at 12:00. Next: Sun, Nov 1.");
  assert.equal(scheduleReadout({ ...d, repeat: "", date: "" }, now), "No date. It stays on your list until you do it.");
  assert.equal(scheduleReadout({ ...d, date: "" }, now), "Add a date for the repeat to start from.");
  assert.equal(scheduleReadout({ ...d, repeat: "", time: "08:00" }, now), "Today at 8:00. It does not repeat. That time has passed, so it shows as overdue.");
});

test("count-from explains the choice picked", () => {
  const d = draftOf(fromDone());
  assert.equal(countFromHint(d), "The next one is 1 week after the day you finish this one.");
  assert.equal(countFromHint({ ...d, interval: "2" }), "The next one is 2 weeks after the day you finish this one.");
  assert.equal(countFromHint({ ...d, fromDone: false }), "The next one keeps to the schedule, however early or late you finish this one.");
});

test("picking days that leave out the date moves it to the first of them", () => {
  const d = { ...draftOf(fromDone()), fromDone: false };
  assert.deepEqual(fitWeekdays({ ...d, weekdays: [0] }, now), { ...d, weekdays: [0], date: "2026-10-18", time: "18:00" });
  // A date already on one of the days stays.
  const wed = { ...d, weekdays: [3, 0] };
  assert.equal(fitWeekdays(wed, now), wed);
  // Counting from done takes no days, so nothing moves.
  const done = { ...d, fromDone: true, weekdays: [0] };
  assert.equal(fitWeekdays(done, now), done);
});

test("moving a repeating task later asks: just this one, or from now on", () => {
  const it = weekly();
  const later = { ...draftOf(it), date: "2026-10-15", time: "12:00" };
  assert.deepEqual(scheduleSave(it, later, now, true), { kind: "ask", set: { due: at(10, 15, 12).toISOString() }, until: at(10, 15, 12) });
  // Without snooze, or moved earlier, it is the edit.
  assert.equal(scheduleSave(it, later, now, false).kind, "edit");
  assert.equal(scheduleSave(it, { ...draftOf(it), time: "12:00" }, now, true).kind, "edit");
  // A rule change too is the edit.
  assert.equal(scheduleSave(it, { ...later, weekdays: [4] }, now, true).kind, "edit");
  // A repeat that counts from done just moves.
  const done = fromDone();
  assert.equal(scheduleSave(done, { ...draftOf(done), date: "2026-10-15" }, now, true).kind, "edit");
  // One that does not repeat just moves.
  assert.equal(scheduleSave(item(), { ...draftOf(item()), date: "2026-10-15" }, now, true).kind, "edit");
  assert.deepEqual(scheduleSave(it, draftOf(it), now, true), { kind: "none" });
  assert.deepEqual(scheduleSave(it, { ...draftOf(it), interval: "0" }, now, true), { kind: "error", text: "Repeat every 1 to 100." });
});

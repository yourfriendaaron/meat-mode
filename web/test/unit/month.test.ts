// The month grid and its entries. A zone with daylight saving time, so the
// weeks that change the clocks are covered.
process.env.TZ = "Europe/London";

import { test } from "node:test";
import assert from "node:assert/strict";
import type { Item } from "../../src/types.ts";
import { ymd } from "../../src/dates.ts";
import { addMonths, dayLabel, monthEntries, monthHTML, monthOf, monthSpan, monthTitle, monthWeeks, type MonthEntry } from "../../src/month.ts";

// Wednesday 14 October 2026, 10:00 local time.
const now = new Date(2026, 9, 14, 10, 0);
const t = (m: number, d: number, h: number, min = 0) => new Date(2026, m - 1, d, h, min).toISOString();

let n = 0;
function item(over: Partial<Item>): Item {
  n++;
  const repo = over.repo ?? "personal";
  const id = over.id ?? `bd-${n}`;
  const bridge = repo === "personal" ? "hub" : "home";
  return {
    key: `${bridge}/${repo}/${id}`,
    bridge,
    repo,
    id,
    title: `Task ${n}`,
    status: "open",
    priority: 2,
    createdAt: t(10, 1, 9),
    updatedAt: t(10, 1, 9),
    comments: [],
    ...over,
  };
}

const days = (weeks: Date[][]) => weeks.map((w) => w.map(ymd));

test("a month starting on a Monday starts the grid on its 1st", () => {
  const weeks = monthWeeks(new Date(2026, 5, 1));
  assert.equal(weeks.length, 5);
  assert.equal(ymd(weeks[0][0]), "2026-06-01");
  assert.equal(ymd(weeks[4][6]), "2026-07-05");
});

test("a month starting on a Sunday takes six weeks, from the Monday before", () => {
  const weeks = monthWeeks(new Date(2026, 2, 1));
  assert.equal(weeks.length, 6);
  assert.equal(ymd(weeks[0][0]), "2026-02-23");
  assert.equal(ymd(weeks[0][6]), "2026-03-01");
  assert.equal(ymd(weeks[5][6]), "2026-04-05");
});

test("a February that starts on a Monday fills exactly four weeks", () => {
  const weeks = monthWeeks(new Date(2027, 1, 1));
  assert.equal(weeks.length, 4);
  assert.equal(ymd(weeks[0][0]), "2027-02-01");
  assert.equal(ymd(weeks[3][6]), "2027-02-28");
});

test("every week runs Monday to Sunday at local midnight, through the clock changes", () => {
  // London's clocks go forward on 29 March and back on 25 October 2026.
  for (const month of [new Date(2026, 2, 1), new Date(2026, 9, 1)]) {
    const weeks = monthWeeks(month);
    const all = weeks.flat();
    for (const [i, d] of all.entries()) {
      assert.equal(d.getHours() + d.getMinutes(), 0, ymd(d));
      assert.equal(d.getDay(), (i + 1) % 7, ymd(d));
      if (i) assert.equal(new Date(all[i - 1].getFullYear(), all[i - 1].getMonth(), all[i - 1].getDate() + 1).getTime(), d.getTime());
    }
  }
  assert.deepEqual(days(monthWeeks(new Date(2026, 9, 1)))[4], ["2026-10-26", "2026-10-27", "2026-10-28", "2026-10-29", "2026-10-30", "2026-10-31", "2026-11-01"]);
});

test("the span runs from the first cell's midnight to the midnight after the last", () => {
  const { from, to } = monthSpan(new Date(2026, 9, 1));
  assert.equal(from.getTime(), new Date(2026, 8, 28).getTime());
  assert.equal(to.getTime(), new Date(2026, 10, 2).getTime());
  // The clocks go back inside it, so it is an hour longer than 35 days.
  assert.equal(to.getTime() - from.getTime(), (35 * 24 + 1) * 3600 * 1000);
  const six = monthSpan(new Date(2026, 2, 1));
  assert.ok(six.to.getTime() - six.from.getTime() <= 62 * 24 * 3600 * 1000);
});

test("months step across years", () => {
  assert.equal(ymd(addMonths(new Date(2026, 11, 1), 1)), "2027-01-01");
  assert.equal(ymd(addMonths(new Date(2026, 0, 1), -1)), "2025-12-01");
  assert.equal(ymd(monthOf(new Date(2026, 9, 31, 23, 59))), "2026-10-01");
  assert.equal(monthTitle(new Date(2026, 10, 1)), "November 2026");
});

test("a day's name says the date and how many items it holds", () => {
  assert.equal(dayLabel(new Date(2026, 10, 4), 3), "Wednesday 4 November, 3 items");
  assert.equal(dayLabel(new Date(2026, 10, 4), 1), "Wednesday 4 November, 1 item");
  assert.equal(dayLabel(new Date(2026, 10, 4), 0), "Wednesday 4 November, no items");
});

const kinds = (byDay: Map<string, MonthEntry[]>, day: string) => (byDay.get(day) ?? []).map((e) => `${e.item.title} ${e.kind}`);

test("entries: due days, the day a snooze ends, and the repeat dates", () => {
  const water = item({ title: "Water the plants", dueAt: t(10, 14, 9), repeat: { every: "week", timeZone: "Europe/London" } });
  const seeds = item({ title: "Buy seeds", dueAt: t(10, 12, 8), deferUntil: t(10, 16, 9) });
  const late = item({ title: "Renew the library card", dueAt: t(10, 2, 8) });
  const back = item({ title: "Call Sam", deferUntil: t(10, 13, 9) });
  const byDay = monthEntries(
    [water, seeds, late, back],
    [
      { key: water.key, at: t(10, 21, 9) },
      { key: water.key, at: t(10, 28, 9) },
    ],
    now,
  );
  assert.deepEqual(kinds(byDay, "2026-10-14"), ["Water the plants due"]);
  assert.deepEqual(kinds(byDay, "2026-10-21"), ["Water the plants repeat"]);
  assert.deepEqual(kinds(byDay, "2026-10-28"), ["Water the plants repeat"]);
  // Overdue stays on its past due day; a snooze shows on both days.
  assert.deepEqual(kinds(byDay, "2026-10-02"), ["Renew the library card due"]);
  assert.deepEqual(kinds(byDay, "2026-10-12"), ["Buy seeds due"]);
  assert.deepEqual(kinds(byDay, "2026-10-16"), ["Buy seeds snooze"]);
  // A snooze that has ended puts nothing on its day.
  assert.equal(byDay.get("2026-10-13"), undefined);
});

test("closed and deleted items are left out, and so are their repeat dates", () => {
  const done = item({ dueAt: t(10, 15, 9), status: "closed", closedAt: t(10, 14, 8), closeReason: "Done in Meat Mode", repeat: { every: "day", timeZone: "UTC" } });
  const gone = item({ dueAt: t(10, 15, 9), status: "closed", closedAt: t(10, 14, 8), closeReason: "Dismissed" });
  const byDay = monthEntries([done, gone], [{ key: done.key, at: t(10, 16, 9) }, { key: "hub/personal/unknown", at: t(10, 16, 9) }], now);
  assert.equal(byDay.size, 0);
});

test("agent items show like personal ones", () => {
  const ask = item({ repo: "garden", title: "Order compost", dueAt: t(10, 15, 18) });
  assert.deepEqual(kinds(monthEntries([ask], [], now), "2026-10-15"), ["Order compost due"]);
});

test("a day's entries run by time, then due before snooze before repeat, then title", () => {
  const a = item({ title: "Bravo", dueAt: t(10, 20, 9) });
  const b = item({ title: "Alpha", dueAt: t(10, 20, 9) });
  const c = item({ title: "Charlie", deferUntil: t(10, 20, 9) });
  const d = item({ title: "Early", dueAt: t(10, 20, 7), repeat: { every: "day", timeZone: "UTC" } });
  const e = item({ title: "Delta", dueAt: t(10, 1, 9), repeat: { every: "day", timeZone: "UTC" } });
  const byDay = monthEntries([a, b, c, d, e], [{ key: e.key, at: t(10, 20, 9) }], now);
  assert.deepEqual(kinds(byDay, "2026-10-20"), ["Early due", "Alpha due", "Bravo due", "Charlie snooze", "Delta repeat"]);
});

test("the grid names each day, marks today and the selection, and counts what does not fit", () => {
  const list = [1, 2, 3, 4, 5].map((i) => item({ title: `Errand ${i}`, dueAt: t(10, 20, 8 + i) }));
  const html = monthHTML({
    month: new Date(2026, 9, 1),
    selected: new Date(2026, 9, 20),
    now,
    entries: monthEntries(list, [], now),
    occurrencesFailed: false,
    movable: (e) => e.item.title !== "Errand 2",
  });
  assert.match(html, /aria-label="Tuesday 20 October, 5 items" aria-pressed="true"/);
  assert.match(html, /aria-label="Wednesday 14 October, no items" aria-pressed="false" aria-current="date"/);
  assert.match(html, /\+2 more/);
  assert.equal((html.match(/class="ment /g) ?? []).length, 3);
  // Only movable entries drag, and only they get a Move button in the day's list.
  assert.equal((html.match(/draggable="true"/g) ?? []).length, 2);
  assert.equal((html.match(/data-act="month-move"/g) ?? []).length, 4);
  assert.match(html, /<h2 id="mlist-h">Tuesday 20 October<\/h2>/);
  assert.match(html, /class="mcell is-out is-past" data-day="2026-09-28"/);
  assert.doesNotMatch(html, /could not be loaded/);
});

test("a failed load of the repeat dates says so in one line", () => {
  const html = monthHTML({ month: new Date(2026, 9, 1), selected: new Date(2026, 9, 14), now, entries: new Map(), occurrencesFailed: true, movable: () => false });
  assert.match(html, /Repeat dates could not be loaded\./);
  assert.match(html, /Nothing on this day\./);
});

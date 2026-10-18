import { test } from "node:test";
import assert from "node:assert/strict";
import { parseQuickAdd, type QuickList } from "../../src/quickadd.ts";

// Wednesday 14 October 2026, 10:00 local time.
const now = new Date(2026, 9, 14, 10, 0);
const lists: QuickList[] = [
  { key: "home/personal", name: "personal" },
  { key: "home/garden", name: "garden" },
  { key: "work/website", name: "website" },
];

function local(d: Date | null): string | null {
  if (!d) return null;
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

test("text with no date or list keeps the whole title", () => {
  const r = parseQuickAdd("Renew the library card", now, lists);
  assert.equal(r.title, "Renew the library card");
  assert.equal(r.due, null);
  assert.equal(r.list, null);
  assert.deepEqual(r.tokens, []);
});

test("today with a time", () => {
  const r = parseQuickAdd("Water the plants today 3pm", now, lists);
  assert.equal(r.title, "Water the plants");
  assert.equal(local(r.due), "2026-10-14 15:00");
  assert.deepEqual(r.tokens.map((t) => [t.kind, t.text]), [["date", "today 3pm"]]);
});

test("today with no time after 9:00 is the next full hour", () => {
  const r = parseQuickAdd("Water the plants today", now, lists);
  assert.equal(local(r.due), "2026-10-14 11:00");
});

test("tonight is 20:00 today", () => {
  const r = parseQuickAdd("Water the plants tonight", now, lists);
  assert.equal(r.title, "Water the plants");
  assert.equal(local(r.due), "2026-10-14 20:00");
});

test("tomorrow with no time is 9:00", () => {
  const r = parseQuickAdd("Call Sam tomorrow", now, lists);
  assert.equal(r.title, "Call Sam");
  assert.equal(local(r.due), "2026-10-15 09:00");
});

test("tomorrow with a time", () => {
  const r = parseQuickAdd("Call Sam tomorrow at 14:30", now, lists);
  assert.equal(local(r.due), "2026-10-15 14:30");
});

test("a weekday is the next one, at 9:00", () => {
  const r = parseQuickAdd("Book the dentist friday", now, lists);
  assert.equal(r.title, "Book the dentist");
  assert.equal(local(r.due), "2026-10-16 09:00");
});

test("today's weekday means today", () => {
  const r = parseQuickAdd("Take out the bins wednesday", now, lists);
  assert.equal(local(r.due), "2026-10-14 11:00");
});

test("next with a weekday", () => {
  const r = parseQuickAdd("Take out the bins next wednesday", now, lists);
  assert.equal(local(r.due), "2026-10-21 09:00");
});

test("a weekday with a time", () => {
  const r = parseQuickAdd("Pay rent monday 5pm", now, lists);
  assert.equal(local(r.due), "2026-10-19 17:00");
});

test("in N days", () => {
  const r = parseQuickAdd("in 3 days renew the library card", now, lists);
  assert.equal(r.title, "renew the library card");
  assert.equal(local(r.due), "2026-10-17 09:00");
});

test("a bare time later today", () => {
  const r = parseQuickAdd("Stretch at 4pm", now, lists);
  assert.equal(local(r.due), "2026-10-14 16:00");
});

test("a bare time already past rolls to tomorrow", () => {
  const r = parseQuickAdd("Stretch at 8am", now, lists);
  assert.equal(local(r.due), "2026-10-15 08:00");
});

test("#list matches a repo name prefix", () => {
  const r = parseQuickAdd("Water the plants #gard", now, lists);
  assert.equal(r.title, "Water the plants");
  assert.equal(r.list, "home/garden");
  assert.deepEqual(r.tokens, [{ kind: "list", start: 17, end: 22, text: "#gard" }]);
});

test("#list prefers an exact name", () => {
  const two = [...lists, { key: "home/garden-shed", name: "garden-shed" }];
  assert.equal(parseQuickAdd("Oil the hinges #garden-shed", now, two).list, "home/garden-shed");
  assert.equal(parseQuickAdd("Oil the hinges #garden", now, two).list, "home/garden");
});

test("an unknown #tag stays in the title", () => {
  const r = parseQuickAdd("Fix #nothing", now, lists);
  assert.equal(r.title, "Fix #nothing");
  assert.equal(r.list, null);
});

test("date and list together, spans in order", () => {
  const text = "Update the site #web tomorrow 10am";
  const r = parseQuickAdd(text, now, lists);
  assert.equal(r.title, "Update the site");
  assert.equal(r.list, "work/website");
  assert.equal(local(r.due), "2026-10-15 10:00");
  assert.deepEqual(r.tokens.map((t) => t.kind), ["list", "date"]);
  for (const t of r.tokens) assert.equal(text.slice(t.start, t.end), t.text);
});

// Repeats (docs/api.md, Repeating to-dos). Wednesday 14 October 2026, 10:00:
// a first occurrence still ahead today stays today.
const repeats: [string, string, object, string][] = [
  ["Water the plants every day at 8:30pm", "Water the plants", { every: "day" }, "2026-10-14 20:30"],
  ["Water the plants every 830pm", "Water the plants", { every: "day" }, "2026-10-14 20:30"],
  ["Take pills every 8am", "Take pills", { every: "day" }, "2026-10-15 08:00"],
  ["Bins every 2 weeks in the morning", "Bins", { every: "week", interval: 2 }, "2026-10-15 06:00"],
  ["Call mum noon on tuesdays", "Call mum", { every: "week", weekdays: [2] }, "2026-10-20 12:00"],
  ["Call mum tuesdays at noon", "Call mum", { every: "week", weekdays: [2] }, "2026-10-20 12:00"],
  ["Standup every weekday at 9", "Standup", { every: "week", weekdays: [1, 2, 3, 4, 5] }, "2026-10-15 09:00"],
  ["Swim on weekdays at 7pm", "Swim", { every: "week", weekdays: [1, 2, 3, 4, 5] }, "2026-10-14 19:00"],
  ["Gym every mon and thu at 7am", "Gym", { every: "week", weekdays: [1, 4] }, "2026-10-15 07:00"],
  ["Gym every mon, thu at 7am", "Gym", { every: "week", weekdays: [1, 4] }, "2026-10-15 07:00"],
  ["Gym every mon & thu at 7am", "Gym", { every: "week", weekdays: [1, 4] }, "2026-10-15 07:00"],
  ["Gym every 2 weeks on mon, thu at 6pm", "Gym", { every: "week", interval: 2, weekdays: [1, 4] }, "2026-10-15 18:00"],
  ["Pay Sam every friday 5pm", "Pay Sam", { every: "week", weekdays: [5] }, "2026-10-16 17:00"],
  ["Take out the bins every mon and thu", "Take out the bins", { every: "week", weekdays: [1, 4] }, "2026-10-15 09:00"],
  ["Review every other week", "Review", { every: "week", interval: 2 }, "2026-10-15 09:00"],
  ["Water the ferns every other day", "Water the ferns", { every: "day", interval: 2 }, "2026-10-15 09:00"],
  ["Back up the photos every 3 months", "Back up the photos", { every: "month", interval: 3 }, "2026-10-15 09:00"],
  ["Tax yearly", "Tax", { every: "year" }, "2026-10-15 09:00"],
  ["Book the vet every year", "Book the vet", { every: "year" }, "2026-10-15 09:00"],
  ["Rent monthly", "Rent", { every: "month" }, "2026-10-15 09:00"],
  ["Stretch daily", "Stretch", { every: "day" }, "2026-10-15 09:00"],
  ["Plan the meals weekly", "Plan the meals", { every: "week" }, "2026-10-15 09:00"],
  ["Read each day", "Read", { every: "day" }, "2026-10-15 09:00"],
  ["Walk every morning", "Walk", { every: "day" }, "2026-10-15 06:00"],
  ["Walk every afternoon", "Walk", { every: "day" }, "2026-10-14 15:00"],
  ["Walk every evening", "Walk", { every: "day" }, "2026-10-14 20:00"],
  ["Read every night", "Read", { every: "day" }, "2026-10-14 20:00"],
  ["Clean every weekend", "Clean", { every: "week", weekdays: [0, 6] }, "2026-10-17 09:00"],
  ["Every Monday morning check the mail", "check the mail", { every: "week", weekdays: [1] }, "2026-10-19 06:00"],
  ["Water the plants every day tonight", "Water the plants", { every: "day" }, "2026-10-14 20:00"],
  // A day named with the repeat is its first occurrence.
  ["Team sync every 2 weeks from friday", "Team sync", { every: "week", interval: 2 }, "2026-10-16 09:00"],
];

for (const [text, title, repeat, due] of repeats) {
  test(`repeat: ${text}`, () => {
    const r = parseQuickAdd(text, now, lists);
    assert.equal(r.title, title);
    assert.deepEqual(r.repeat, repeat);
    assert.equal(local(r.due), due);
    for (const t of r.tokens) assert.equal(text.slice(t.start, t.end), t.text);
  });
}

test("a one-off date is not a repeat", () => {
  const r = parseQuickAdd("Pay rent tomorrow morning", now, lists);
  assert.equal(r.title, "Pay rent");
  assert.equal(r.repeat, null);
  assert.equal(local(r.due), "2026-10-15 06:00");
});

test("words that only look like a repeat stay in the title", () => {
  for (const text of ["Read Every Good Book", "Every Body Yoga class", "Fix every page of the site"]) {
    const r = parseQuickAdd(text, now, lists);
    assert.equal(r.title, text);
    assert.equal(r.repeat, null);
    assert.equal(r.due, null);
  }
});

test("an interval the hub does not take is no repeat", () => {
  assert.equal(parseQuickAdd("Water every 0 days", now, lists).repeat, null);
  assert.equal(parseQuickAdd("Water every 101 days", now, lists).repeat, null);
  assert.deepEqual(parseQuickAdd("Water every 100 days", now, lists).repeat, { every: "day", interval: 100 });
});

test("the repeat and the date are separate tokens, in order", () => {
  const text = "Water the plants every day at 8:30pm #pers";
  const r = parseQuickAdd(text, now, lists);
  assert.deepEqual(r.tokens.map((t) => [t.kind, t.text]), [
    ["repeat", "every day"],
    ["date", "at 8:30pm"],
    ["list", "#pers"],
  ]);
  assert.equal(r.list, "home/personal");
});

test("with repeats off, the words stay as they were read before", () => {
  const r = parseQuickAdd("Check the hoses every week", now, lists, { repeat: false });
  assert.equal(r.repeat, null);
  assert.ok(r.title.includes("every"));
});

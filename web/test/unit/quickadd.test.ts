import { test } from "node:test";
import assert from "node:assert/strict";
import { addAction, parseQuickAdd, readQuickAdd, type AddOptions, type QuickList } from "../../src/quickadd.ts";
import type { Repo } from "../../src/types.ts";

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
  ["Take pills every 8p", "Take pills", { every: "day" }, "2026-10-14 20:00"],
  ["Stretch every lunchtime", "Stretch", { every: "day" }, "2026-10-14 12:00"],
  ["Run every day after work", "Run", { every: "day" }, "2026-10-14 18:00"],
  ["Bins every 2 weeks in the morning", "Bins", { every: "week", interval: 2 }, "2026-10-15 09:00"],
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
  ["Walk every morning", "Walk", { every: "day" }, "2026-10-15 09:00"],
  ["Walk every afternoon", "Walk", { every: "day" }, "2026-10-14 15:00"],
  ["Walk every evening", "Walk", { every: "day" }, "2026-10-14 20:00"],
  ["Read every night", "Read", { every: "day" }, "2026-10-14 20:00"],
  ["Clean every weekend", "Clean", { every: "week", weekdays: [0, 6] }, "2026-10-17 09:00"],
  ["Every Monday morning check the mail", "check the mail", { every: "week", weekdays: [1] }, "2026-10-19 09:00"],
  ["Water the plants every day tonight", "Water the plants", { every: "day" }, "2026-10-14 20:00"],
  // A day named with the repeat is its first occurrence.
  ["Team sync every 2 weeks from friday", "Team sync", { every: "week", interval: 2 }, "2026-10-16 09:00"],
  // A monthly repeat's day of the month is its first occurrence.
  ["Pay rent every month on the 1st", "Pay rent", { every: "month" }, "2026-11-01 09:00"],
  ["Pay rent monthly on the 15th", "Pay rent", { every: "month" }, "2026-10-15 09:00"],
  ["Pay rent every month on the 31st", "Pay rent", { every: "month" }, "2026-10-31 09:00"],
  ["Pay rent every month on the 14th at 9am", "Pay rent", { every: "month" }, "2026-11-14 09:00"],
  ["Pay rent every month on the 14th at 8:30p", "Pay rent", { every: "month" }, "2026-10-14 20:30"],
  ["Service the boiler every 3 months on the 2nd", "Service the boiler", { every: "month", interval: 3 }, "2026-11-02 09:00"],
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
  assert.equal(local(r.due), "2026-10-15 09:00");
});

// The hub counts the series from the first due date, so it must be a 31st
// for later months to come back to the 31st.
test("the 31st starts in the next month that has one", () => {
  const r = parseQuickAdd("Pay rent every month on the 31st", new Date(2026, 10, 5, 10, 0), lists);
  assert.equal(local(r.due), "2026-12-31 09:00");
  assert.equal(local(parseQuickAdd("Pay rent every month on the 30th", new Date(2026, 1, 1, 10, 0), lists).due), "2026-03-30 09:00");
});

test("a day of the month counts only with a monthly repeat", () => {
  const r = parseQuickAdd("Pay rent every week on the 1st", now, lists);
  assert.deepEqual(r.repeat, { every: "week" });
  assert.ok(r.title.includes("1st"));
  assert.equal(parseQuickAdd("Pay rent every month on the 32nd", now, lists).title, "Pay rent on the 32nd");
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

// End dates (docs/api.md, Repeating to-dos: until). Wednesday 14 October 2026, 10:00.
const untils: [string, string, object, string][] = [
  ["Water the plants every sunday until nov 26", "Water the plants", { every: "week", weekdays: [0], until: "2026-11-26" }, "2026-10-18 09:00"],
  ["Water the plants every sunday ending nov 26", "Water the plants", { every: "week", weekdays: [0], until: "2026-11-26" }, "2026-10-18 09:00"],
  ["Water the plants every day at 8pm until 26 November", "Water the plants", { every: "day", until: "2026-11-26" }, "2026-10-14 20:00"],
  ["Water the plants every day until nov 26 at 8pm", "Water the plants", { every: "day", until: "2026-11-26" }, "2026-10-14 20:00"],
  ["Water the plants every day until 2027-01-31 #personal", "Water the plants", { every: "day", until: "2027-01-31" }, "2026-10-15 09:00"],
  // A date with no year is the next one on or after the first occurrence.
  ["Pay rent every month on the 1st until oct 1", "Pay rent", { every: "month", until: "2027-10-01" }, "2026-11-01 09:00"],
  ["Water the plants every day until oct 14", "Water the plants", { every: "day", until: "2027-10-14" }, "2026-10-15 09:00"],
  ["Water the plants every day at 8pm until oct 14", "Water the plants", { every: "day", until: "2026-10-14" }, "2026-10-14 20:00"],
  // A relative date counts from now.
  ["Stretch daily until friday", "Stretch", { every: "day", until: "2026-10-16" }, "2026-10-15 09:00"],
  ["Stretch daily until tomorrow", "Stretch", { every: "day", until: "2026-10-15" }, "2026-10-15 09:00"],
];

for (const [text, title, repeat, due] of untils) {
  test(`until: ${text}`, () => {
    const r = parseQuickAdd(text, now, lists);
    assert.equal(r.title, title);
    assert.deepEqual(r.repeat, repeat);
    assert.equal(local(r.due), due);
    assert.equal(r.early, undefined);
    for (const t of r.tokens) assert.equal(text.slice(t.start, t.end), t.text);
  });
}

test("the end date is part of the repeat's token", () => {
  const r = parseQuickAdd("Water the plants every sunday at 8pm until nov 26", now, lists);
  assert.deepEqual(r.tokens.map((t) => [t.kind, t.text]), [
    ["repeat", "every sunday"],
    ["date", "at 8pm"],
    ["repeat", "until nov 26"],
  ]);
});

// A tag or list anywhere in the text leaves the date, time, repeat and end as
// they read without it, and every span still matches the text it covers.
const plain = "Water the plants every sunday at 8pm until nov 29";
const words = plain.split(" ");
for (const mark of ["@weekend", "#garden"]) {
  for (let at = 0; at <= words.length; at++) {
    const text = [...words.slice(0, at), mark, ...words.slice(at)].join(" ");
    test(`a repeat reads the same with ${mark} in it: ${text}`, () => {
      const r = parseQuickAdd(text, now, lists);
      assert.equal(r.title, "Water the plants");
      assert.equal(local(r.due), "2026-10-18 20:00");
      assert.deepEqual(r.repeat, { every: "week", weekdays: [0], until: "2026-11-29" });
      assert.deepEqual(r.tags, mark === "@weekend" ? ["weekend"] : []);
      assert.equal(r.list, mark === "#garden" ? "home/garden" : null);
      for (const t of r.tokens) {
        assert.equal(text.slice(t.start, t.end), t.text);
        assert.equal(t.text, t.text.trim());
      }
      assert.ok(r.tokens.some((t) => t.text === mark));
    });
  }
}

test("the parts of a repeat around a tag are highlighted apart from it", () => {
  const r = parseQuickAdd("Water the plants every sunday @weekend at 8pm until #garden nov 29", now, lists);
  assert.deepEqual(r.tokens.map((t) => [t.kind, t.text]), [
    ["repeat", "every sunday"],
    ["tag", "@weekend"],
    ["date", "at 8pm"],
    ["repeat", "until"],
    ["list", "#garden"],
    ["repeat", "nov 29"],
  ]);
});

test("a date reads the same with a tag or list between its day and time", () => {
  for (const text of ["Call Sam tomorrow @phone at 8pm", "Call Sam tomorrow #garden at 8pm", "Call Sam @phone tomorrow #garden at 8pm"]) {
    const r = parseQuickAdd(text, now, lists);
    assert.equal(r.title, "Call Sam", text);
    assert.equal(local(r.due), "2026-10-15 20:00", text);
    for (const t of r.tokens) assert.equal(text.slice(t.start, t.end), t.text, text);
  }
});

test("an end date before the first occurrence is not sent", () => {
  for (const text of ["Stretch daily until today", "Water the plants every day until 2026-10-01", "Pay rent every month on the 1st until oct 20 2026"]) {
    const r = parseQuickAdd(text, now, lists);
    assert.equal(r.early, true, text);
    assert.equal(r.repeat?.until, undefined, text);
    assert.ok(!r.title.includes("until"), text);
  }
});

test("until stays in the title without a repeat, or without a date after it", () => {
  const one = parseQuickAdd("Wait until friday to call Sam", now, lists);
  assert.equal(one.repeat, null);
  assert.equal(one.title, "Wait until to call Sam");
  const time = parseQuickAdd("Work every day until 5pm", now, lists);
  assert.deepEqual(time.repeat, { every: "day" });
  assert.equal(time.title, "Work until");
  const plain = parseQuickAdd("Read every day until the book ends", now, lists);
  assert.deepEqual(plain.repeat, { every: "day" });
  assert.equal(plain.title, "Read until the book ends");
  assert.equal(parseQuickAdd("Check the hoses every week until nov 26", now, lists, { repeat: false }).repeat, null);
});

test("with repeats off, the words stay as they were read before", () => {
  const r = parseQuickAdd("Check the hoses every week", now, lists, { repeat: false });
  assert.equal(r.repeat, null);
  assert.ok(r.title.includes("every"));
});

test("with repeats off, a repeat phrase gives no date", () => {
  const r = parseQuickAdd("Standup every monday", now, lists, { repeat: false });
  assert.equal(r.title, "Standup every monday");
  assert.equal(r.due, null);
  assert.equal(r.repeat, null);
  assert.deepEqual(r.tokens, []);
  const u = parseQuickAdd("Check the hoses every week until nov 26", now, lists, { repeat: false });
  assert.equal(u.title, "Check the hoses every week until nov 26");
  assert.equal(u.due, null);
  assert.equal(u.repeat, null);
});

// Times people type in shorthand or by name. Wednesday 14 October 2026, 10:00.
const times: [string, string, string][] = [
  ["Call Sam 8:30p", "Call Sam", "2026-10-14 20:30"],
  ["Call Sam 8p", "Call Sam", "2026-10-14 20:00"],
  ["Call Sam 830p", "Call Sam", "2026-10-14 20:30"],
  ["Call Sam 830pm", "Call Sam", "2026-10-14 20:30"],
  ["Call Sam at 8:30p", "Call Sam", "2026-10-14 20:30"],
  ["Call Sam tomorrow 8p", "Call Sam", "2026-10-15 20:00"],
  ["Call Sam 7:30a", "Call Sam", "2026-10-15 07:30"],
  ["Call Sam 12p", "Call Sam", "2026-10-14 12:00"],
  ["Call Sam 8pm", "Call Sam", "2026-10-14 20:00"],
  ["Call Sam 20:30", "Call Sam", "2026-10-14 20:30"],
  ["Call Sam at 9", "Call Sam", "2026-10-15 09:00"],
  ["Call Sam at lunch", "Call Sam", "2026-10-14 12:00"],
  ["Call Sam at lunchtime", "Call Sam", "2026-10-14 12:00"],
  ["Call Sam lunchtime tomorrow", "Call Sam", "2026-10-15 12:00"],
  ["Call Sam by lunch friday", "Call Sam", "2026-10-16 12:00"],
  ["Send the report eod", "Send the report", "2026-10-14 17:00"],
  ["Send the report by EOD", "Send the report", "2026-10-14 17:00"],
  ["Send the report end of day", "Send the report", "2026-10-14 17:00"],
  ["Send the report by end of the day tomorrow", "Send the report", "2026-10-15 17:00"],
  ["Go to the gym after work", "Go to the gym", "2026-10-14 18:00"],
  ["Go to the gym after work on monday", "Go to the gym", "2026-10-19 18:00"],
  ["first thing tomorrow email Sam", "email Sam", "2026-10-15 09:00"],
  ["Email Sam first thing monday", "Email Sam", "2026-10-19 09:00"],
  ["Email Sam tomorrow first thing", "Email Sam", "2026-10-15 09:00"],
  ["Finish the draft end of the week", "Finish the draft", "2026-10-16 09:00"],
  ["Finish the draft by end of week", "Finish the draft", "2026-10-16 09:00"],
  ["Walk the dog in the morning", "Walk the dog", "2026-10-15 09:00"],
  ["Walk the dog friday morning", "Walk the dog", "2026-10-16 09:00"],
  ["Walk the dog tomorrow morning at 7am", "Walk the dog", "2026-10-15 07:00"],
  ["Walk the dog this afternoon", "Walk the dog", "2026-10-14 15:00"],
  ["Walk the dog this evening", "Walk the dog", "2026-10-14 20:00"],
];

for (const [text, title, due] of times) {
  test(`time: ${text}`, () => {
    const r = parseQuickAdd(text, now, lists);
    assert.equal(r.title, title);
    assert.equal(local(r.due), due);
    for (const t of r.tokens) assert.equal(text.slice(t.start, t.end), t.text);
  });
}

test("words that only look like a time stay in the title", () => {
  for (const text of ["Buy lunch stuff", "Do the first thing on the list", "Fix the lock on flat 4a", "Buy 60p stamps", "Read the end of the chapter"]) {
    const r = parseQuickAdd(text, now, lists);
    assert.equal(r.title, text);
    assert.equal(r.due, null);
  }
});

test("this morning before 9:00 is today at 9:00", () => {
  const early = new Date(2026, 9, 14, 7, 0);
  assert.equal(local(parseQuickAdd("Walk the dog this morning", early, lists).due), "2026-10-14 09:00");
  assert.equal(local(parseQuickAdd("Walk the dog eod", new Date(2026, 9, 14, 18, 0), lists).due), "2026-10-15 17:00");
});

test("end of the week is this Friday at 9:00 until it has passed", () => {
  const at = (d: number, h: number) => local(parseQuickAdd("Finish the draft end of the week", new Date(2026, 9, d, h, 0), lists).due);
  assert.equal(at(12, 10), "2026-10-16 09:00"); // Monday
  assert.equal(at(16, 8), "2026-10-16 09:00"); // Friday before 9:00
  assert.equal(at(16, 10), "2026-10-23 09:00"); // Friday after 9:00
  assert.equal(at(17, 10), "2026-10-23 09:00"); // Saturday
  assert.equal(at(18, 10), "2026-10-23 09:00"); // Sunday
});

// addAction: typed text as the add action to send, one path for the New task bar and the setup wizard.
const repos: Repo[] = [
  { bridge: "hub", name: "personal", personal: true, optedIn: true },
  { bridge: "laptop", name: "garden", personal: false, optedIn: true, agentsAllowed: true },
  { bridge: "laptop", name: "website", personal: false, optedIn: true },
  { bridge: "laptop", name: "attic", personal: false, optedIn: false, agentsAllowed: true },
];
const opts = { id: "a1", zone: "Europe/Paris" };

function added(text: string, o: Partial<AddOptions> = {}) {
  const r = addAction(text, now, repos, { ...opts, ...o });
  if ("error" in r) assert.fail(`${text}: ${r.error}`);
  return r;
}

function errorOf(text: string, rs: Repo[] = repos) {
  const r = addAction(text, now, rs, opts);
  return "error" in r ? r.error : null;
}

test("an add goes to the personal list by default, with its due date", () => {
  const r = added("Water the plants tomorrow 6pm");
  assert.deepEqual(r.action, {
    id: "a1",
    kind: "add",
    bridge: "hub",
    repo: "personal",
    payload: { title: "Water the plants", due: new Date(2026, 9, 15, 18, 0).toISOString() },
  });
  assert.equal(r.wakes, false);
});

test("the list the text names wins over the picked one", () => {
  const r = added("Prune the roses #garden", { list: "laptop/website" });
  assert.equal(r.action.bridge, "laptop");
  assert.equal(r.action.repo, "garden");
  assert.equal(r.action.payload.title, "Prune the roses");
});

test("the picked list is used when the text names none; a stale or hidden pick falls back to personal", () => {
  assert.equal(added("Fix the footer", { list: "laptop/website" }).action.repo, "website");
  assert.equal(added("Fix the footer", { list: "laptop/gone" }).action.repo, "personal");
  assert.equal(added("Clear the attic", { list: "laptop/attic" }).action.repo, "personal");
});

test("a repeat is sent on the personal list with the zone", () => {
  const r = added("Water the plants every sunday until nov 26");
  assert.deepEqual(r.action.payload.repeat, { every: "week", weekdays: [0], until: "2026-11-26", timeZone: "Europe/Paris" });
  assert.equal(r.action.payload.due, new Date(2026, 9, 18, 9, 0).toISOString());
});

test("on any other list the repeat words stay in the title, as the hub takes no repeat there", () => {
  const r = added("Water the plants every sunday", { list: "laptop/garden" });
  assert.equal(r.action.repo, "garden");
  assert.equal(r.action.payload.repeat, undefined);
  assert.equal(r.action.payload.title, "Water the plants every sunday");
  const tagged = added("Check the logs daily #website").action;
  assert.equal(tagged.payload.repeat, undefined);
  assert.equal(tagged.payload.title, "Check the logs daily");
});

test("a repeat that ends before it starts, an empty title and no list make no action", () => {
  assert.equal(errorOf("Stretch daily until today"), "early");
  assert.equal(errorOf("  "), "empty");
  assert.equal(errorOf("tomorrow"), "empty");
  assert.equal(errorOf("Water the plants", []), "no-list");
});

test("the agent switch wakes an agent only where agents may work", () => {
  const garden = added("Prune the roses #garden", { ask: true });
  assert.equal(garden.wakes, true);
  assert.equal(garden.action.payload.ask, true);
  assert.equal(added("Prune the roses #garden").action.payload.ask, undefined);
  assert.equal(added("Fix the footer #website", { ask: true }).wakes, false);
  assert.equal(added("Water the plants", { ask: true }).action.payload.ask, undefined);
});

test("readQuickAdd gives the list the add will go to", () => {
  assert.equal(readQuickAdd("Water the plants", now, repos).target?.name, "personal");
  assert.equal(readQuickAdd("Water the plants", now, [], { list: "hub/personal" }).target, undefined);
  assert.equal(readQuickAdd("Water the plants daily", now, repos, { list: "laptop/garden" }).qa.repeat, null);
});

test("with no list named or picked, an add starts on the list being viewed", () => {
  assert.equal(added("Fix the footer", { viewing: "laptop/website" }).action.repo, "website");
  assert.equal(added("Fix the footer", { list: "laptop/garden", viewing: "laptop/website" }).action.repo, "garden");
  assert.equal(added("Prune the roses #garden", { viewing: "laptop/website" }).action.repo, "garden");
  assert.equal(added("Fix the footer", { viewing: "laptop/attic" }).action.repo, "personal");
  assert.equal(added("Fix the footer daily", { viewing: "laptop/website" }).action.payload.title, "Fix the footer daily");
});

// Tags: @word on the personal list and projects (docs/api.md, Tags).
test("@word is a tag, taken out of the title, in the order typed", () => {
  const r = parseQuickAdd("Water the plants @garden tomorrow @Home/Chores", now, lists);
  assert.equal(r.title, "Water the plants");
  assert.deepEqual(r.tags, ["garden", "Home/Chores"]);
  assert.equal(local(r.due), "2026-10-15 09:00");
  assert.deepEqual(r.tokens.map((t) => [t.kind, t.text]), [
    ["tag", "@garden"],
    ["date", "tomorrow"],
    ["tag", "@Home/Chores"],
  ]);
});

test("a tag word starts with a letter, so @5pm stays in the title and gives no date", () => {
  const r = parseQuickAdd("Call Sam @5pm", now, lists);
  assert.equal(r.title, "Call Sam @5pm");
  assert.deepEqual(r.tags, []);
  assert.equal(r.due, null);
});

test("a tag is never read as a date or a repeat, and an address is not a tag", () => {
  const r = parseQuickAdd("Plan @monday @daily", now, lists);
  assert.deepEqual(r.tags, ["monday", "daily"]);
  assert.equal(r.due, null);
  assert.equal(r.repeat, null);
  const mail = parseQuickAdd("Write to sam@example.com", now, lists);
  assert.equal(mail.title, "Write to sam@example.com");
  assert.deepEqual(mail.tags, []);
});

test("with tags off, @word stays in the title", () => {
  const r = parseQuickAdd("Check the logs @ops tomorrow", now, lists, { tags: false });
  assert.equal(r.title, "Check the logs @ops");
  assert.deepEqual(r.tags, []);
  assert.equal(local(r.due), "2026-10-15 09:00");
});

test("tags are sent as labels on the personal list, cleaned as the hub does", () => {
  const r = added("Water the plants @garden @Garden @human @errands");
  assert.equal(r.action.payload.title, "Water the plants");
  assert.deepEqual(r.action.payload.labels, ["garden", "errands"]);
  assert.equal(added("Water the plants").action.payload.labels, undefined);
});

test("on a tracker's list @word stays in the title and no labels are sent", () => {
  const r = added("Prune the roses @weekend #garden");
  assert.equal(r.action.repo, "garden");
  assert.equal(r.action.payload.title, "Prune the roses @weekend");
  assert.equal(r.action.payload.labels, undefined);
  assert.deepEqual(r.qa.tags, []);
});

test("a tag the hub would refuse makes no action, with the hub's words", () => {
  const long = `@${"a".repeat(51)}`;
  const r = addAction(`Water the plants ${long}`, now, repos, opts);
  assert.ok("error" in r);
  assert.equal(r.error, "tags");
  assert.equal(r.why, `The tag "${"a".repeat(51)}" is longer than 50 characters.`);
  const many = Array.from({ length: 21 }, (_, i) => `@t${i}`).join(" ");
  const m = addAction(`Water the plants ${many}`, now, repos, opts);
  assert.ok("error" in m && m.why === "An item has at most 20 tags.");
});

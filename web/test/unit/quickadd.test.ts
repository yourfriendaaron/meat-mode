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

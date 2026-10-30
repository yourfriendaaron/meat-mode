import { test } from "node:test";
import assert from "node:assert/strict";
import { parseQuickAdd, type QuickList } from "../../src/quickadd.ts";
import { repeatLabel } from "../../src/repeat.ts";
import { readTyped } from "../../src/schedule.ts";
import type { EditDraft } from "../../src/edit.ts";
import { PHRASES, WED } from "./quickadd-corpus.ts";

const lists: QuickList[] = [
  { key: "home/personal", name: "personal" },
  { key: "home/garden", name: "garden" },
  { key: "work/website", name: "website" },
];

const blank: EditDraft = {
  title: "",
  description: "",
  date: "",
  time: "",
  priority: 2,
  repeat: "",
  interval: "1",
  weekdays: [],
  monthBy: "date",
  fromDone: false,
  until: "",
  timeZone: "",
  remindBefore: null,
  parent: "",
  tags: "",
};

function local(d: Date | null): string | null {
  if (!d) return null;
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

test("every phrase in the corpus reads as it means", () => {
  const wrong: string[] = [];
  for (const p of PHRASES) {
    const now = p.now ?? WED;
    const r = parseQuickAdd(p.say, now, lists);
    const got = {
      title: r.title,
      due: local(r.due),
      repeat: r.repeat ? repeatLabel(r.repeat, now) : undefined,
      list: r.list ?? undefined,
      tags: r.tags.length ? r.tags : undefined,
    };
    const want = { title: p.title, due: p.due, repeat: p.repeat, list: p.list, tags: p.tags };
    try {
      assert.deepEqual(got, want);
    } catch {
      wrong.push(`${JSON.stringify(p.say)}\n    want ${JSON.stringify(want)}\n    got  ${JSON.stringify(got)}`);
    }
  }
  assert.equal(wrong.length, 0, `${wrong.length} of ${PHRASES.length} read wrong:\n  ${wrong.join("\n  ")}`);
});

test("the task sheet's typed field reads each phrase's date the same way", () => {
  const wrong: string[] = [];
  for (const p of PHRASES) {
    if (!p.due || p.list || p.tags || !p.say.startsWith(p.title)) continue;
    const when = p.say.slice(p.title.length).replace(/^[\s,:;(-]+/, "");
    if (!when.trim()) continue;
    const out = readTyped(when, p.now ?? WED, blank);
    const got = out && "draft" in out ? `${out.draft.date} ${out.draft.time}` : out?.error;
    if (got !== p.due) wrong.push(`${JSON.stringify(when)}: want ${p.due}, got ${got}`);
  }
  assert.equal(wrong.length, 0, `${wrong.length} read wrong:\n  ${wrong.join("\n  ")}`);
});

test("a long run of date words reads in good time", () => {
  // Each run repeats words a pattern could split more than one way, then ends in a way that fails it.
  const runs = ["due on ", "starting on ", "by ", "on the ", "every mon ", "every thurs ", "mondays and ", "in the morning ", "at 5 ", "the 3rd "];
  for (const run of runs) {
    const text = `Call Sam ${run.repeat(60)}zzz tomorrow`;
    const t = performance.now();
    parseQuickAdd(text, WED, lists);
    readTyped(`${run.repeat(60)}zzz tomorrow`, WED, blank);
    const ms = performance.now() - t;
    assert.ok(ms < 500, `${JSON.stringify(run)} x60 took ${Math.round(ms)} ms`);
  }
});

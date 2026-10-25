// The task sheet's schedule: its card in plain words, the typed field read
// with quick add's parser, the readout of the draft, and what Save does with
// a repeating item moved to another time. Pure: callers pass `now`.

import type { Edit, Item } from "./types.ts";
import { addDays, fmtDay, fmtTime, sameDay, ymd } from "./dates.ts";
import { diffEdit, draftDue, draftRule, editError, hhmm, type EditDraft } from "./edit.ts";
import { parseQuickAdd } from "./quickadd.ts";
import { firstOccurrence, repeatWords, sameRule } from "./repeat.ts";

/** "Today", "Tomorrow", "Sun, Nov 1", "Sun, Jan 3, 2027". */
export function dayLine(due: Date, now: Date): string {
  if (sameDay(due, now)) return "Today";
  if (sameDay(due, addDays(now, 1))) return "Tomorrow";
  if (sameDay(due, addDays(now, -1))) return "Yesterday";
  return due.getFullYear() === now.getFullYear() ? fmtDay(due) : `${fmtDay(due)}, ${due.getFullYear()}`;
}

/** "Today at 18:00", "Sun, Nov 1 at 12:00"; "No date" for none. */
export function whenLine(due: Date | null, now: Date): string {
  return due ? `${dayLine(due, now)} at ${fmtTime(due)}` : "No date";
}

/** The schedule card's two lines: when it is due, and how it repeats ("" for once). */
export function scheduleCard(item: Pick<Item, "dueAt" | "repeat">, now: Date): { when: string; repeat: string } {
  const due = item.dueAt ? new Date(item.dueAt) : null;
  if (!due) return { when: "Add a date", repeat: "" };
  return { when: whenLine(due, now), repeat: item.repeat ? repeatWords(item.repeat, now) : "Does not repeat" };
}

export type Typed = { draft: EditDraft } | { error: string };

/**
 * The typed field read the way quick add reads a task: "every sunday 12pm",
 * "tomorrow 9am", "mondays and thursdays at 8". It sets the draft's date and
 * time, and its repeat when it names one; without one the draft keeps its
 * repeat, so "tomorrow 9am" moves a repeating task. A repeat typed with no
 * time keeps the draft's time. Null for an empty field.
 */
export function readTyped(text: string, now: Date, d: EditDraft): Typed | null {
  if (!text.trim()) return null;
  const qa = parseQuickAdd(text, now, [], { tags: false });
  if (!qa.due) return { error: "Could not read that. Try every sunday 12pm, or tomorrow 9am." };
  if (qa.title) return { error: `Could not read "${qa.title}". Try every sunday 12pm, or tomorrow 9am.` };
  if (qa.early) return { error: "The end date is before the first date." };
  const out: EditDraft = { ...d, weekdays: [...d.weekdays] };
  let due = qa.due;
  const r = qa.repeat;
  if (r) {
    out.repeat = r.every;
    out.interval = String(r.interval ?? 1);
    out.weekdays = [...(r.weekdays ?? [])];
    out.fromDone = false;
    out.until = r.until ?? "";
    const timed = qa.tokens.some((t) => t.kind === "date");
    const keep = /^(\d{2}):(\d{2})$/.exec(d.time);
    if (!timed && d.date && keep) {
      due = firstOccurrence(r, now, Number(keep[1]), Number(keep[2]), r.every === "month" ? due.getDate() : undefined);
    }
  }
  out.date = ymd(due);
  out.time = hhmm(due);
  return { draft: out };
}

/**
 * What the draft will save, in words: "Every Sunday at 12:00. Next: Sun,
 * Nov 1." The typed field and the controls both show here.
 */
export function scheduleReadout(d: EditDraft, now: Date): string {
  const due = draftDue(d);
  if (due === undefined) return "Pick a real date and time.";
  if (due === null) return d.repeat ? "Add a date for the repeat to start from." : "No date. It stays on your list until you do it.";
  const rule = draftRule(d);
  const past = due < now ? " That time has passed, so it shows as overdue." : "";
  if (rule === undefined) return `${whenLine(due, now)}.${past}`;
  if (!rule) return `${whenLine(due, now)}. It does not repeat.${past}`;
  const words = repeatWords(rule, now);
  if (rule.fromDone) return `${whenLine(due, now)}, ${words.charAt(0).toLowerCase()}${words.slice(1)}.${past}`;
  return `${words} at ${fmtTime(due)}. Next: ${dayLine(due, now)}.${past}`;
}

/** The line under "Count the next one from" that says what the choice does. */
export function countFromHint(d: EditDraft): string {
  const n = Number(d.interval.trim());
  const unit = d.repeat && Number.isInteger(n) && n >= 1 ? `${n} ${d.repeat}${n === 1 ? "" : "s"}` : "";
  if (d.fromDone) return unit ? `The next one is ${unit} after the day you finish this one.` : "The next one counts from the day you finish this one.";
  return "The next one keeps to the schedule, however early or late you finish this one.";
}

/**
 * After a change of a weekly rule's days: a date that is not one of them
 * moves to the first one past now, at the same time, so the next date is
 * one the rule picks.
 */
export function fitWeekdays(d: EditDraft, now: Date): EditDraft {
  const due = draftDue(d);
  if (!due || d.repeat !== "week" || d.fromDone || !d.weekdays.length || d.weekdays.includes(due.getDay())) return d;
  const next = firstOccurrence({ every: "week", weekdays: d.weekdays }, now, due.getHours(), due.getMinutes());
  return { ...d, date: ymd(next), time: hhmm(next) };
}

export type ScheduleSave =
  | { kind: "none" }
  | { kind: "error"; text: string }
  | { kind: "edit"; set: Edit }
  /** The draft only moves a repeating item: ask "Just this one" (a snooze to `until`) or "From now on" (the edit). */
  | { kind: "ask"; set: Edit; until: Date };

/**
 * What Save on the schedule page does. A task that repeats on a schedule,
 * moved later with nothing else changed, asks whether only this one moves:
 * a snooze, which leaves the rule alone. One that counts from when it is
 * done, or moves earlier, just takes the edit.
 */
export function scheduleSave(item: Item, d: EditDraft, now: Date, canSnooze: boolean): ScheduleSave {
  const err = editError(item, d, now);
  if (err) return { kind: "error", text: err };
  const set = diffEdit(item, d, now);
  if (!set) return { kind: "none" };
  const keys = Object.keys(set);
  const r = item.repeat;
  if (canSnooze && r && !r.fromDone && item.dueAt && keys.length === 1 && set.due && sameRule(draftRule(d), r)) {
    const until = new Date(set.due);
    if (until > now && until > new Date(item.dueAt)) return { kind: "ask", set, until };
  }
  return { kind: "edit", set };
}

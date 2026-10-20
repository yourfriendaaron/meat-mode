import { addDays, at, fmtDay, fmtTime, nextMonday, sameDay, startOfDay } from "./dates.ts";
import { parseQuickAdd } from "./quickadd.ts";

/**
 * A snooze time typed the way quick add reads one: the time, or null and why
 * not. The line says what was understood, or why it cannot be used.
 */
export function typedSnooze(text: string, now: Date): { until: Date | null; line: string } {
  if (!text.trim()) return { until: null, line: "" };
  const until = parseQuickAdd(text, now, [], { repeat: false }).due;
  if (!until) return { until: null, line: "Could not read that time. Try something like tomorrow 9am." };
  if (until <= now) return { until: null, line: "Pick a time in the future." };
  return { until, line: fmtSnoozeTime(until, now) };
}

/**
 * A snooze time written so the typed-time field reads it back to the same
 * minute: "Today, 20:00", "Tomorrow, 9:00", "Thu, Oct 22, 20:00", with the
 * year when it is not now's, or has passed (the reading looks ahead without
 * one): "Fri, Oct 22, 2027, 20:00".
 */
export function fmtSnoozeTime(d: Date, now: Date): string {
  let day: string;
  if (sameDay(d, now)) day = "Today";
  else if (sameDay(d, addDays(now, 1))) day = "Tomorrow";
  else day = d.getFullYear() === now.getFullYear() && d > now ? fmtDay(d) : `${fmtDay(d)}, ${d.getFullYear()}`;
  return `${day}, ${fmtTime(d)}`;
}

export interface SnoozeOption {
  id: string;
  label: string;
  hint: string;
  until: Date;
}

/** The preset snooze choices for a given moment, in display order. */
export function snoozeOptions(now: Date): SnoozeOption[] {
  const opts: SnoozeOption[] = [];
  const inHour = new Date(now.getTime() + 60 * 60 * 1000);
  inHour.setSeconds(0, 0);
  opts.push({ id: "hour", label: "In 1 hour", hint: fmtTime(inHour), until: inHour });

  const evening = at(now, 18);
  if (now < evening) opts.push({ id: "evening", label: "This evening", hint: "18:00", until: evening });

  opts.push({ id: "tomorrow", label: "Tomorrow", hint: "9:00", until: at(addDays(now, 1), 9) });

  const day = now.getDay();
  let sat = startOfDay(addDays(now, (6 - day + 7) % 7));
  if (at(sat, 10) <= now) sat = addDays(sat, 7);
  opts.push({ id: "weekend", label: "This weekend", hint: "Sat 10:00", until: at(sat, 10) });

  opts.push({ id: "nextweek", label: "Next week", hint: "Mon 9:00", until: at(nextMonday(now), 9) });

  const month = new Date(now.getFullYear(), now.getMonth() + 1, now.getDate(), 9, 0);
  opts.push({ id: "month", label: "In a month", hint: fmtDay(month), until: month });
  return opts;
}

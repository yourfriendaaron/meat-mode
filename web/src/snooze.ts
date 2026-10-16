import { addDays, at, fmtDay, nextMonday, startOfDay } from "./dates.ts";

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
  opts.push({ id: "hour", label: "In 1 hour", hint: fmtHM(inHour), until: inHour });

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

function fmtHM(d: Date): string {
  return `${d.getHours()}:${String(d.getMinutes()).padStart(2, "0")}`;
}

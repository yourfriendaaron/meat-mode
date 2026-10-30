// Repeating personal items (docs/api.md, Repeating to-dos): reading a repeat
// from quick add's text, its first occurrence, and its label in plain words.
// Pure: callers pass `now`.

import { MAX_REPEAT_INTERVAL, type Repeat, type RepeatUnit } from "./types.ts";
import { fmtDate, fromYmd } from "./dates.ts";

/** What a repeat says, before the device's zone is added to it. */
export type RepeatRule = Omit<Repeat, "timeZone" | "start">;

export interface RepeatMatch {
  rule: RepeatRule;
  /** A monthly repeat's day of the month ("on the 15th"), which sets its first occurrence. */
  monthDay?: number;
  start: number;
  end: number;
  text: string;
}

const UNITS: Record<string, RepeatUnit> = { day: "day", week: "week", month: "month", year: "year" };
const ADVERBS: Record<string, [RepeatUnit, number]> = {
  daily: ["day", 1],
  weekly: ["week", 1],
  fortnightly: ["week", 2],
  monthly: ["month", 1],
  quarterly: ["month", 3],
  yearly: ["year", 1],
  annually: ["year", 1],
};

/** "the daily log", "a weekly planner": an adverb between an article and a noun names a thing, not a repeat. */
const ARTICLE_BEFORE = /(?:^|\s)(?:the|a|an|my|your|our|his|her|their)\s+$/i;
const NOUN_AFTER = /^\s+(?!(?:at|by|on|in|to|for|from|until|till|starting|and|then|before|after)\b)\p{L}/iu;

/** Day names quick add reads, as a pattern, and the weekday each one means. */
const DAY = "sun(?:day)?|mon(?:day)?|tue(?:s(?:day)?)?|wed(?:nesday)?|thu(?:r(?:s(?:day)?)?)?|fri(?:day)?|sat(?:urday)?";
/** Between two day names: a comma, "and", "&", or only a space after "every" ("every mon wed fri"). */
const AND = "\\s*(?:,\\s*(?:and\\s+)?|and\\s+|&\\s*)";
const AND_OR_SPACE = `(?:${AND}|\\s+)`;
const DAY_LIST = `(?:${DAY})s?(?:${AND}(?:${DAY})s?)*`;
/** "tuesdays", "mondays and thursdays": a plural day is a weekly repeat on its own. */
const PLURAL = "(?:sun|mon|tues|wednes|thurs|fri|satur)days";
const PLURAL_LIST = `${PLURAL}(?:${AND}${PLURAL})*`;
const EVERY = "(?:every|each)";
const NUMBER = "\\d{1,3}|other";
/** "every second day", "every 3rd week": a count said as an ordinal, for units only, since "every 2nd tuesday" names a week of the month. */
const ORDINAL = "second|third|fourth|2nd|3rd|4th";
/** A time of day or a part of the day, which chrono reads after "every". */
const TIME = "\\d{1,2}(?::?\\d{2})?\\s*(?:am|pm)|\\d{1,2}p|\\d{1,2}:?\\d{2}[ap]|\\d{1,2}:\\d{2}|noon|midnight|morning|afternoon|evening|night|lunchtime|after work";

const WEEKDAYS = [1, 2, 3, 4, 5];

/** Which of a month's weekdays: "2nd tuesday", "last friday". */
const WEEK_OF = "1st|first|2nd|second|3rd|third|4th|fourth|last";

function weekOf(w: string): number {
  const k = w.toLowerCase();
  if (k === "last") return -1;
  return ["1st", "2nd", "3rd", "4th"].indexOf(k) + 1 || ["first", "second", "third", "fourth"].indexOf(k) + 1;
}

/** A monthly rule on a weekday of the month. */
function monthly(interval: number, which: string, day: string): RepeatRule {
  return { ...rule("month", interval, [weekdayOf(day)]), weekOfMonth: weekOf(which) };
}

/** The day of the month of month m's nth weekday wd (1 to 4), or its last for -1. */
export function weekdayOfMonth(y: number, m: number, wd: number, n: number): number {
  const first = 1 + ((wd - new Date(y, m, 1).getDay() + 7) % 7);
  if (n > 0) return first + 7 * (n - 1);
  const days = new Date(y, m + 1, 0).getDate();
  return first + 7 * Math.floor((days - first) / 7);
}

/** Which of its month's weekdays a date is, 1 to 5. */
export function weekOfDate(d: Date): number {
  return Math.ceil(d.getDate() / 7);
}

/** Whether a date is its month's last of its weekday. */
export function isLastOfMonth(d: Date): boolean {
  return d.getDate() + 7 > new Date(d.getFullYear(), d.getMonth() + 1, 0).getDate();
}

/** Each phrase and the rule it means. Where two match at the same place, the first one listed wins. */
const PATTERNS: {
  re: RegExp;
  rule: (m: RegExpExecArray) => RepeatRule;
  monthDay?: (m: RegExpExecArray) => number;
  /** Whether this match, in its text, is no repeat after all. */
  skip?: (m: RegExpExecArray, text: string) => boolean;
}[] = [
  // every month on the 1st, every 3 months on the 15th, monthly on the 31st
  {
    re: new RegExp(`\\b(?:${EVERY}\\s+(?:(${NUMBER})\\s+)?months?|monthly)\\s+on\\s+(?:the\\s+)?(\\d{1,2})(?:st|nd|rd|th)?\\b(?!\\s+(?:${DAY})\\b)`, "i"),
    rule: (m) => rule("month", intervalOf(m[1])),
    monthDay: (m) => Number(m[2]),
  },
  // on the 1st of every month, every 15th of the month
  {
    re: /\b(?:(?:every|each)\s+(?:the\s+)?(\d{1,2})(?:st|nd|rd|th)?\s+of\s+(?:the|every|each)\s+month|(?:on\s+)?(?:the\s+)?(\d{1,2})(?:st|nd|rd|th)?\s+of\s+(?:every|each)\s+month)\b/i,
    rule: () => rule("month", 1),
    monthDay: (m) => Number(m[1] ?? m[2]),
  },
  // every 2nd tuesday, every last friday of the month, the 1st monday of every month, monthly on the last friday
  {
    re: new RegExp(`\\b(?:${EVERY}\\s+(${WEEK_OF})\\s+(${DAY})(?:\\s+of\\s+(?:the|every|each)\\s+month)?|(?:on\\s+)?(?:the\\s+)?(${WEEK_OF})\\s+(${DAY})\\s+of\\s+(?:every|each)\\s+month)\\b`, "i"),
    rule: (m) => monthly(1, m[1] ?? m[3], m[2] ?? m[4]),
  },
  {
    re: new RegExp(`\\b(?:${EVERY}\\s+(?:(${NUMBER})\\s+)?months?|monthly)\\s+on\\s+(?:the\\s+)?(${WEEK_OF})\\s+(${DAY})\\b`, "i"),
    rule: (m) => monthly(intervalOf(m[1]), m[2], m[3]),
  },
  // twice a week, three times a week: days spread over the week
  {
    re: /\b(twice|two times|three times|3 times|2 times)\s+(?:a|per|each|every)\s+week\b/i,
    rule: (m) => rule("week", 1, /^(?:twice|two|2)/i.test(m[1]) ? [1, 4] : [1, 3, 5]),
  },
  // every 2 weeks on mon and thu, every week on friday, weekly on wednesday
  {
    re: new RegExp(`\\b(?:${EVERY}\\s+(?:(${NUMBER})\\s+)?weeks?|weekly)\\s+on\\s+(${DAY_LIST})\\b`, "i"),
    rule: (m) => rule("week", intervalOf(m[1]), daysIn(m[2])),
  },
  // every weekday, every weekend, on weekdays, weekdays
  { re: new RegExp(`\\b(?:${EVERY}\\s+(weekday|weekend)s?|(?:on\\s+)?(weekday|weekend)s)\\b`, "i"), rule: (m) => rule("week", 1, /^weekday$/i.test(m[1] ?? m[2]) ? WEEKDAYS : [0, 6]) },
  // every monday, every mon and thu, every mon wed fri, every other friday
  {
    re: new RegExp(`\\b${EVERY}\\s+(?:(other)\\s+)?((?:${DAY})s?(?:${AND_OR_SPACE}(?:${DAY})s?)*)\\b`, "i"),
    rule: (m) => rule("week", intervalOf(m[1]), daysIn(m[2])),
  },
  // tuesdays, on mondays and thursdays
  { re: new RegExp(`\\b(?:on\\s+)?(${PLURAL_LIST})\\b`, "i"), rule: (m) => rule("week", 1, daysIn(m[1])) },
  // every day, every 3 days, every other week, every second day, every quarter, every fortnight
  {
    re: new RegExp(`\\b${EVERY}\\s+(?:(${NUMBER}|${ORDINAL})\\s+)?(day|week|month|year|quarter|fortnight)s?\\b`, "i"),
    rule: (m) => {
      const unit = m[2].toLowerCase();
      const n = intervalOf(m[1]);
      if (unit === "quarter") return rule("month", 3 * n);
      if (unit === "fortnight") return rule("week", 2 * n);
      return rule(UNITS[unit], n);
    },
  },
  // every 8am, every morning: every day, at a time chrono reads from the words left
  { re: new RegExp(`\\b${EVERY}(?=\\s+(?:${TIME})\\b)`, "i"), rule: () => rule("day", 1) },
  // daily, weekly, quarterly
  {
    re: /\b(daily|weekly|fortnightly|monthly|quarterly|yearly|annually)\b/i,
    rule: (m) => rule(...ADVERBS[m[1].toLowerCase()]),
    skip: (m, text) => ARTICLE_BEFORE.test(text.slice(0, m.index)) && NOUN_AFTER.test(text.slice(m.index + m[0].length)),
  },
];

/** 0 (Sunday) to 6 for a day name or its abbreviation. */
function weekdayOf(name: string): number {
  return ["su", "mo", "tu", "we", "th", "fr", "sa"].indexOf(name.slice(0, 2).toLowerCase());
}

function daysIn(list: string): number[] {
  const days = list.match(new RegExp(DAY, "gi")) ?? [];
  return [...new Set(days.map(weekdayOf))].sort((a, b) => a - b);
}

function intervalOf(n: string | undefined): number {
  if (!n) return 1;
  const w = n.toLowerCase();
  if (w === "other" || w === "second" || w === "2nd") return 2;
  if (w === "third" || w === "3rd") return 3;
  if (w === "fourth" || w === "4th") return 4;
  return Number(n);
}

/** Builds the rule, leaving out what the hub takes as its default. */
function rule(every: RepeatUnit, interval: number, weekdays: number[] = []): RepeatRule {
  const r: RepeatRule = { every };
  if (interval !== 1) r.interval = interval;
  if (weekdays.length) r.weekdays = weekdays;
  return r;
}

/** The first repeat phrase in text, or null. Only a phrase that makes a rule the hub takes counts. */
export function findRepeat(text: string): RepeatMatch | null {
  let best: RepeatMatch | null = null;
  for (const p of PATTERNS) {
    const m = p.re.exec(text);
    if (!m || (best && m.index >= best.start) || p.skip?.(m, text)) continue;
    const r = p.rule(m);
    const n = r.interval ?? 1;
    if (!Number.isInteger(n) || n < 1 || n > MAX_REPEAT_INTERVAL) continue;
    const day = p.monthDay?.(m);
    if (day !== undefined && (day < 1 || day > 31)) continue;
    best = { rule: r, start: m.index, end: m.index + m[0].length, text: m[0] };
    if (day !== undefined) best.monthDay = day;
  }
  return best;
}

/**
 * The first occurrence of a rule after now, at hours:minutes local time: the
 * first day on or after today, past now, that a weekly rule's weekdays allow.
 * A monthly rule given a day of the month takes the next month that has that
 * day, so the hub counts the series from it and later short months fall on
 * their last day.
 */
export function firstOccurrence(r: RepeatRule, now: Date, hours: number, minutes: number, monthDay?: number): Date {
  if (r.every === "month" && r.weekOfMonth && r.weekdays?.length === 1) {
    for (let i = 0; ; i++) {
      const y = now.getFullYear();
      const m = now.getMonth() + i;
      const d = new Date(y, m, weekdayOfMonth(y, m, r.weekdays[0], r.weekOfMonth), hours, minutes);
      if (d > now) return d;
    }
  }
  if (r.every === "month" && monthDay) {
    for (let i = 0; ; i++) {
      const last = new Date(now.getFullYear(), now.getMonth() + i + 1, 0).getDate();
      if (monthDay > last) continue;
      const d = new Date(now.getFullYear(), now.getMonth() + i, monthDay, hours, minutes);
      if (d > now) return d;
    }
  }
  for (let i = 0; i < 8; i++) {
    const d = new Date(now.getFullYear(), now.getMonth(), now.getDate() + i, hours, minutes);
    if (d <= now) continue;
    if (r.every === "week" && r.weekdays?.length && !r.weekdays.includes(d.getDay())) continue;
    return d;
  }
  // Unreachable: some day in a week is past now and allowed.
  return new Date(now.getFullYear(), now.getMonth(), now.getDate() + 1, hours, minutes);
}

const DAY_NAMES = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

/** Monday first, Sunday last, as people list a week. */
function weekOrder(days: number[]): number[] {
  return [...days].sort((a, b) => ((a + 6) % 7) - ((b + 6) % 7));
}

/** "Every day", "Every 2 weeks on Mon, Thu", "Every weekday", "Every month after done", "Every week on Sun until Nov 26". */
export function repeatLabel(r: RepeatRule, now: Date): string {
  const n = r.interval && r.interval > 1 ? r.interval : 1;
  const days = r.every === "week" && !r.fromDone ? weekOrder(r.weekdays ?? []) : [];
  let out: string;
  if (r.weekOfMonth && r.weekdays?.length === 1) out = `${n === 1 ? "Every month" : `Every ${n} months`} on the ${WEEK_OF_WORDS[r.weekOfMonth]} ${DAY_NAMES[r.weekdays[0]]}`;
  else if (n === 1 && days.join() === "1,2,3,4,5") out = "Every weekday";
  else {
    out = n === 1 ? `Every ${r.every}` : `Every ${n} ${r.every}s`;
    if (days.length) out += ` on ${days.map((d) => DAY_NAMES[d]).join(", ")}`;
  }
  if (r.fromDone) out += " after done";
  const until = r.until ? fromYmd(r.until) : null;
  return until ? `${out} until ${fmtDate(until, now)}` : out;
}

const FULL_DAY_NAMES = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];
const WEEK_OF_WORDS: Record<number, string> = { 1: "1st", 2: "2nd", 3: "3rd", 4: "4th", [-1]: "last" };

/** "Mon and Thu", "Mon, Wed and Fri". */
function dayList(days: number[]): string {
  const names = weekOrder(days).map((d) => DAY_NAMES[d]);
  return names.length > 1 ? `${names.slice(0, -1).join(", ")} and ${names.at(-1)}` : (names[0] ?? "");
}

/**
 * A repeat as a sentence for the task sheet: "Every Sunday", "Every 2 weeks
 * on Mon and Thu, until Dec 31", "Then 1 week after you finish it".
 */
export function repeatWords(r: RepeatRule, now: Date): string {
  const n = r.interval && r.interval > 1 ? r.interval : 1;
  const unit = `${n} ${r.every}${n === 1 ? "" : "s"}`;
  let out: string;
  if (r.fromDone) out = `Then ${unit} after you finish it`;
  else if (r.weekOfMonth && r.weekdays?.length === 1) {
    const day = `${WEEK_OF_WORDS[r.weekOfMonth]} ${FULL_DAY_NAMES[r.weekdays[0]]}`;
    out = n === 1 ? `Every ${day} of the month` : `Every ${unit} on the ${day}`;
  } else {
    const days = r.every === "week" ? weekOrder(r.weekdays ?? []) : [];
    if (!days.length) out = n === 1 ? `Every ${r.every}` : `Every ${unit}`;
    else if (n > 1) out = `Every ${unit} on ${dayList(days)}`;
    else if (days.join() === "1,2,3,4,5") out = "Every weekday";
    else if (days.length === 1) out = `Every ${FULL_DAY_NAMES[days[0]]}`;
    else out = `Every ${dayList(days)}`;
  }
  const until = r.until ? fromYmd(r.until) : null;
  return until ? `${out}, until ${fmtDate(until, now)}` : out;
}

/** Whether two rules repeat the same way and end on the same date, whatever their zone and start. */
export function sameRule(a: RepeatRule | null | undefined, b: RepeatRule | null | undefined): boolean {
  if (!a || !b) return !a && !b;
  const days = (r: RepeatRule) => ((r.every === "week" || r.weekOfMonth) && !r.fromDone ? [...(r.weekdays ?? [])].sort().join() : "");
  return (a.weekOfMonth ?? 0) === (b.weekOfMonth ?? 0) && a.every === b.every && (a.interval || 1) === (b.interval || 1) && days(a) === days(b) && !!a.fromDone === !!b.fromDone && (a.until ?? "") === (b.until ?? "");
}

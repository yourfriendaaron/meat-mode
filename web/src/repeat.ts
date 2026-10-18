// Repeating personal items (docs/api.md, Repeating to-dos): reading a repeat
// from quick add's text, its first occurrence, and its label in plain words.
// Pure: callers pass `now`.

import { MAX_REPEAT_INTERVAL, type Repeat, type RepeatUnit } from "./types.ts";

/** What a repeat says, before the device's zone is added to it. */
export type RepeatRule = Omit<Repeat, "timeZone" | "start">;

export interface RepeatMatch {
  rule: RepeatRule;
  start: number;
  end: number;
  text: string;
}

const UNITS: Record<string, RepeatUnit> = { day: "day", week: "week", month: "month", year: "year" };
const ADVERBS: Record<string, RepeatUnit> = { daily: "day", weekly: "week", monthly: "month", yearly: "year", annually: "year" };

/** Day names quick add reads, as a pattern, and the weekday each one means. */
const DAY = "sun(?:day)?|mon(?:day)?|tue(?:s(?:day)?)?|wed(?:nesday)?|thu(?:r(?:s(?:day)?)?)?|fri(?:day)?|sat(?:urday)?";
const AND = "\\s*(?:,\\s*(?:and\\s+)?|and\\s+|&\\s*)";
const DAY_LIST = `(?:${DAY})s?(?:${AND}(?:${DAY})s?)*`;
/** "tuesdays", "mondays and thursdays": a plural day is a weekly repeat on its own. */
const PLURAL = "(?:sun|mon|tues|wednes|thurs|fri|satur)days";
const PLURAL_LIST = `${PLURAL}(?:${AND}${PLURAL})*`;
const EVERY = "(?:every|each)";
const NUMBER = "\\d{1,3}|other";
/** A time of day or a part of the day, which chrono reads after "every". */
const TIME = "\\d{1,2}(?::?\\d{2})?\\s*(?:am|pm)|\\d{1,2}:\\d{2}|noon|midnight|morning|afternoon|evening|night";

const WEEKDAYS = [1, 2, 3, 4, 5];

/** Each phrase and the rule it means. Where two match at the same place, the first one listed wins. */
const PATTERNS: { re: RegExp; rule: (m: RegExpExecArray) => RepeatRule }[] = [
  // every 2 weeks on mon and thu, every week on friday
  { re: new RegExp(`\\b${EVERY}\\s+(?:(${NUMBER})\\s+)?weeks?\\s+on\\s+(${DAY_LIST})\\b`, "i"), rule: (m) => rule("week", intervalOf(m[1]), daysIn(m[2])) },
  // every weekday, every weekend, on weekdays
  { re: new RegExp(`\\b(?:${EVERY}\\s+(weekday|weekend)s?|on\\s+(weekday|weekend)s)\\b`, "i"), rule: (m) => rule("week", 1, /^weekday$/i.test(m[1] ?? m[2]) ? WEEKDAYS : [0, 6]) },
  // every monday, every mon and thu
  { re: new RegExp(`\\b${EVERY}\\s+(${DAY_LIST})\\b`, "i"), rule: (m) => rule("week", 1, daysIn(m[1])) },
  // tuesdays, on mondays and thursdays
  { re: new RegExp(`\\b(?:on\\s+)?(${PLURAL_LIST})\\b`, "i"), rule: (m) => rule("week", 1, daysIn(m[1])) },
  // every day, every 3 days, every other week
  { re: new RegExp(`\\b${EVERY}\\s+(?:(${NUMBER})\\s+)?(day|week|month|year)s?\\b`, "i"), rule: (m) => rule(UNITS[m[2].toLowerCase()], intervalOf(m[1])) },
  // every 8am, every morning: every day, at a time chrono reads from the words left
  { re: new RegExp(`\\b${EVERY}(?=\\s+(?:${TIME})\\b)`, "i"), rule: () => rule("day", 1) },
  // daily, weekly
  { re: /\b(daily|weekly|monthly|yearly|annually)\b/i, rule: (m) => rule(ADVERBS[m[1].toLowerCase()], 1) },
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
  if (n.toLowerCase() === "other") return 2;
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
    if (!m || (best && m.index >= best.start)) continue;
    const r = p.rule(m);
    const n = r.interval ?? 1;
    if (!Number.isInteger(n) || n < 1 || n > MAX_REPEAT_INTERVAL) continue;
    best = { rule: r, start: m.index, end: m.index + m[0].length, text: m[0] };
  }
  return best;
}

/**
 * The first occurrence of a rule after now, at hours:minutes local time: the
 * first day on or after today, past now, that a weekly rule's weekdays allow.
 */
export function firstOccurrence(r: RepeatRule, now: Date, hours: number, minutes: number): Date {
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

/** "Every day", "Every 2 weeks on Mon, Thu", "Every weekday", "Every month after done". */
export function repeatLabel(r: RepeatRule): string {
  const n = r.interval && r.interval > 1 ? r.interval : 1;
  const days = r.every === "week" && !r.fromDone ? weekOrder(r.weekdays ?? []) : [];
  let out: string;
  if (n === 1 && days.join() === "1,2,3,4,5") out = "Every weekday";
  else {
    out = n === 1 ? `Every ${r.every}` : `Every ${n} ${r.every}s`;
    if (days.length) out += ` on ${days.map((d) => DAY_NAMES[d]).join(", ")}`;
  }
  return r.fromDone ? `${out} after done` : out;
}

/** Whether two rules repeat the same way, whatever their zone and start. */
export function sameRule(a: RepeatRule | null | undefined, b: RepeatRule | null | undefined): boolean {
  if (!a || !b) return !a && !b;
  const days = (r: RepeatRule) => (r.every === "week" && !r.fromDone ? [...(r.weekdays ?? [])].sort().join() : "");
  return a.every === b.every && (a.interval || 1) === (b.interval || 1) && days(a) === days(b) && !!a.fromDone === !!b.fromDone;
}

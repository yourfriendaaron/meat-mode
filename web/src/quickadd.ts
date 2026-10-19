// Quick add: a title with an optional natural-language date, repeat and
// #list, parsed and turned into the add action to send. Pure: the caller
// passes `now` and the lists it may match.

import * as chrono from "chrono-node/en";
import { findRepeat, firstOccurrence, type RepeatRule } from "./repeat.ts";
import { startOfDay, ymd } from "./dates.ts";
import { isPersonal, repoKey, repoLabel, visibleRepos } from "./model.ts";
import { noAgentReason } from "./runs.ts";
import type { NewAction, Repo } from "./types.ts";

export interface QuickList {
  /** Repo key, `<bridge>/<repo>`. */
  key: string;
  /** Repo name, matched by `#prefix`. */
  name: string;
}

export interface QuickToken {
  kind: "date" | "list" | "repeat";
  start: number;
  end: number;
  text: string;
}

export interface QuickAdd {
  title: string;
  due: Date | null;
  /** Set only with `due`, which is then its first occurrence. */
  repeat: RepeatRule | null;
  /** Set when the repeat's end date falls before its first occurrence; the rule then holds no `until`. */
  early?: true;
  list: string | null;
  tokens: QuickToken[];
}

export interface QuickOptions {
  /** Whether the repeat words make a rule, which only the personal list takes. They are blanked from the date either way. Default yes. */
  repeat?: boolean;
}

/** Hour used for "tonight". */
export const TONIGHT_HOUR = 20;
/** Hour used for a date given without a time. */
export const DEFAULT_HOUR = 9;

/** Hour used for "morning", where chrono reads 6:00. */
export const MORNING_HOUR = 9;

const PART_OF_DAY = /\b(morning|noon|midday|afternoon|evening|night|midnight)\b/i;
const TONIGHT = /\btonight\b/i;
const LIST_TAG = /(^|\s)#([\p{L}\p{N}_.-]+)/gu;
/** The words that end a repeat ("every sunday until nov 26"), before its date. */
const UNTIL = /\b(?:until|ending)\s+/gi;

/** A word's edges, for patterns chrono runs without its own boundary check. */
const B = "(?<![\\p{L}\\p{N}_])";
const E = "(?![\\p{L}\\p{N}_])";
const DAY_WORD = "today|tomorrow|tmrw|mon(?:day)?|tue(?:s(?:day)?)?|wed(?:nesday)?|thu(?:r(?:s(?:day)?)?)?|fri(?:day)?|sat(?:urday)?|sun(?:day)?";

/** Times people name rather than give, with the hour each means. */
const NAMED: [RegExp, number][] = [
  // Only "at lunch" or "lunchtime": "Buy lunch stuff" stays a title.
  [/^(?:(?:at|by)\s+lunch(?:time)?|lunchtime)$/i, 12],
  [/^(?:by\s+)?(?:eod|end of (?:the )?day)$/i, 17],
  [/^after work$/i, 18],
];

/**
 * "8p", "8:30p", "830a": am and pm by their first letter, which chrono does
 * not read. A bare hour takes only "p", so "Flat 4a" stays a title.
 */
const SHORT_MERIDIEM = new RegExp(`${B}(\\d{1,2})(?::?(\\d{2}))?([ap])${E}`, "iu");
const shortMeridiem: chrono.Parser = {
  pattern: () => SHORT_MERIDIEM,
  extract(_ctx, m) {
    const h = Number(m[1]);
    const min = Number(m[2] ?? 0);
    if (h < 1 || h > 12 || min > 59 || (!m[2] && m[3].toLowerCase() === "a")) return null;
    const pm = m[3].toLowerCase() === "p";
    return { hour: (h % 12) + (pm ? 12 : 0), minute: min, meridiem: pm ? 1 : 0 };
  },
};

/** "at lunch", "eod", "after work". */
const NAMED_TIME = new RegExp(`${B}(?:(?:at|by)\\s+lunch(?:time)?|lunchtime|(?:by\\s+)?(?:eod|end of (?:the )?day)|after work)${E}`, "iu");
const namedTime: chrono.Parser = {
  pattern: () => NAMED_TIME,
  extract(_ctx, m) {
    const text = m[0].replace(/\s+/g, " ");
    const hit = NAMED.find(([re]) => re.test(text));
    return hit ? { hour: hit[1], minute: 0 } : null;
  },
};

/** "first thing tomorrow", "monday first thing": 9:00 on a day said next to it. */
const FIRST_THING = new RegExp(
  `(?<=${B}(?:${DAY_WORD})\\s+)first thing${E}|${B}first thing(?=\\s+(?:on\\s+|next\\s+)?(?:${DAY_WORD})${E})`,
  "iu",
);
const firstThing: chrono.Parser = {
  pattern: () => FIRST_THING,
  extract: () => ({ hour: MORNING_HOUR, minute: 0 }),
};

/** "end of the week", "by end of week": this week's Friday at 9:00, or next week's once it has passed. */
const END_OF_WEEK = new RegExp(`${B}(?:by\\s+)?(?:the\\s+)?end of (?:the |this )?week${E}`, "iu");
const endOfWeek: chrono.Parser = {
  pattern: () => END_OF_WEEK,
  extract(ctx) {
    const now = ctx.reference.instant;
    const d = new Date(now.getFullYear(), now.getMonth(), now.getDate() + ((5 - now.getDay() + 7) % 7), MORNING_HOUR, 0);
    if (d <= now) d.setDate(d.getDate() + 7);
    return { year: d.getFullYear(), month: d.getMonth() + 1, day: d.getDate(), hour: MORNING_HOUR, minute: 0 };
  },
};

/** "morning" at 9:00 rather than chrono's 6:00, before chrono moves a time already past to tomorrow. */
const morningAtNine: chrono.Refiner = {
  refine(_ctx, results) {
    for (const r of results) {
      if (r.start.tags().has("casualReference/morning") && !r.start.isCertain("hour")) r.start.imply("hour", MORNING_HOUR);
    }
    return results;
  },
};

const parser = chrono.casual.clone();
parser.parsers.push(shortMeridiem, namedTime, firstThing, endOfWeek);
parser.refiners.unshift(morningAtNine);

export function parseQuickAdd(text: string, now: Date, lists: QuickList[], opts: QuickOptions = {}): QuickAdd {
  const tokens: QuickToken[] = [];

  let list: string | null = null;
  for (const m of text.matchAll(LIST_TAG)) {
    if (list) break;
    const want = m[2].toLowerCase();
    const hit =
      lists.find((l) => l.name.toLowerCase() === want) ??
      lists.find((l) => l.name.toLowerCase().startsWith(want));
    if (!hit) continue;
    list = hit.key;
    const start = (m.index ?? 0) + m[1].length;
    tokens.push({ kind: "list", start, end: start + m[2].length + 1, text: `#${m[2]}` });
  }

  // The repeat goes first, and its words are blanked for chrono, which would
  // read "every monday" as next Monday. With repeats off the span is still
  // found and blanked, but makes no rule and no token, so the words stay in
  // the title and give no date.
  let repeat: RepeatRule | null = null;
  let monthDay: number | undefined;
  let dateText = text;
  const takeRepeat = opts.repeat !== false;
  const rm = findRepeat(text);
  const rmTaken = !!rm && !tokens.some((t) => rm.start < t.end && t.start < rm.end);
  if (rmTaken && rm) {
    if (takeRepeat) {
      repeat = rm.rule;
      monthDay = rm.monthDay;
      tokens.push({ kind: "repeat", start: rm.start, end: rm.end, text: rm.text });
    }
    dateText = text.slice(0, rm.start) + " ".repeat(rm.end - rm.start) + text.slice(rm.end);
  }

  // "until nov 26" after the repeat, blanked for chrono in the same way; with
  // no repeat rule it makes no until either.
  let until: ReturnType<typeof chrono.parse>[number] | null = null;
  if (rmTaken && rm) {
    UNTIL.lastIndex = rm.end;
    for (let m = UNTIL.exec(text); m && !until; m = UNTIL.exec(text)) {
      const from = m.index + m[0].length;
      const r = dateOnly(parser.parse(text.slice(from), now, { forwardDate: true })[0], now);
      // Only a date that follows the word and names its day: "until 5pm" is no end.
      if (!r || r.index !== 0 || !hasDay(r)) continue;
      const end = from + r.text.length;
      if (tokens.some((t) => m.index < t.end && t.start < end)) continue;
      until = r;
      if (takeRepeat) tokens.push({ kind: "repeat", start: m.index, end, text: text.slice(m.index, end) });
      dateText = dateText.slice(0, m.index) + " ".repeat(end - m.index) + dateText.slice(end);
    }
  }

  let due: Date | null = null;
  const results = parser.parse(dateText, now, { forwardDate: true });
  for (const r of results) {
    // Chrono can take in spaces before a match, such as a blanked repeat's.
    const at = r.index + r.text.length - r.text.trimStart().length;
    // "in the morning", "at noon", "from friday": chrono leaves out the words that lead in.
    const lead = /\b(?:in the|at|on|from|starting(?: on| from)?)\s+$/i.exec(dateText.slice(0, at));
    const start = lead ? lead.index : at;
    const end = r.index + r.text.length;
    if (tokens.some((t) => start < t.end && t.start < end)) continue;
    due = repeat && !hasDay(r) ? firstOccurrence(repeat, now, ...timeOf(r), monthDay) : resolve(r, now);
    tokens.push({ kind: "date", start, end, text: text.slice(start, end) });
    break;
  }
  if (repeat && !due) due = firstOccurrence(repeat, now, DEFAULT_HOUR, 0, monthDay);
  let early: true | undefined;
  if (repeat && due && until) {
    const last = untilDate(until, due);
    if (last < ymd(due)) early = true;
    else repeat = { ...repeat, until: last };
  }

  tokens.sort((a, b) => a.start - b.start);
  let title = "";
  let pos = 0;
  for (const t of tokens) {
    title += text.slice(pos, t.start);
    pos = t.end;
  }
  title += text.slice(pos);
  title = title.replace(/\s+/g, " ").trim();
  const out: QuickAdd = { title, due, repeat, list, tokens };
  if (early) out.early = early;
  return out;
}

/** Whether a list takes a repeat: only the hub's personal list does, as the hub checks. */
export function takesRepeat(r: Repo | undefined): boolean {
  return !!r && isPersonal(r);
}

/** Whether an add to this list, with the agent switch at `ask`, wakes an agent. */
export function wakesAgent(r: Repo | undefined, ask: boolean): boolean {
  return !!r && !noAgentReason(r) && ask;
}

export interface QuickRead {
  qa: QuickAdd;
  /** The list the to-do goes to; undefined when there is none yet. */
  target: Repo | undefined;
}

/** Where an add goes when the text names no list, as repo keys. */
export interface QuickPicks {
  /** The list picked outside the text. */
  list?: string;
  /** The list being viewed, used when none was picked. */
  viewing?: string;
}

/**
 * Typed text as it will be added. The list the text names wins, then the
 * picked list, then the list being viewed, then the first visible list,
 * which is the personal one; a key that is not a visible list is passed
 * over. On a list that takes no repeat the repeat words stay in the title.
 */
export function readQuickAdd(text: string, now: Date, repos: Repo[], picks: QuickPicks = {}): QuickRead {
  const visible = visibleRepos(repos);
  const lists = visible.map((r) => ({ key: repoKey(r), name: repoLabel(r) }));
  let qa = parseQuickAdd(text, now, lists);
  const find = (key: string | null | undefined) => (key ? visible.find((r) => repoKey(r) === key) : undefined);
  const target = find(qa.list) ?? find(picks.list) ?? find(picks.viewing) ?? visible[0];
  if (qa.repeat && !takesRepeat(target)) qa = parseQuickAdd(text, now, lists, { repeat: false });
  return { qa, target };
}

export interface AddOptions extends QuickPicks {
  /** The new action's id. */
  id: string;
  /** The "an agent starts on it" switch. */
  ask?: boolean;
  /** The IANA zone a repeat keeps its time in. */
  zone?: string;
}

/** Why typed text makes no add: no title, no list to add to, or a repeat that ends before it starts. */
export type AddError = "empty" | "no-list" | "early";

export const ADD_ERROR_TEXT: Record<AddError, string> = {
  empty: "Type a to-do first.",
  "no-list": "There is no list to add to yet.",
  early: "The end date is before the first date.",
};

export type AddResult =
  | { action: NewAction; qa: QuickAdd; target: Repo; wakes: boolean }
  | { error: AddError; qa: QuickAdd };

/** Typed quick add text as the add action to send, or why there is none. */
export function addAction(text: string, now: Date, repos: Repo[], opts: AddOptions): AddResult {
  const { qa, target } = readQuickAdd(text, now, repos, opts);
  if (!qa.title) return { error: "empty", qa };
  if (!target) return { error: "no-list", qa };
  if (qa.early) return { error: "early", qa };
  const action: NewAction = {
    id: opts.id,
    kind: "add",
    bridge: target.bridge,
    repo: target.name,
    payload: { title: qa.title, due: qa.due ? qa.due.toISOString() : null },
  };
  if (qa.repeat && qa.due) action.payload.repeat = { ...qa.repeat, timeZone: opts.zone ?? "" };
  const wakes = wakesAgent(target, opts.ask ?? false);
  if (wakes) action.payload.ask = true;
  return { action, qa, target, wakes };
}

/**
 * A date read with a time ("nov 26 at 8pm") cut to its shortest start that
 * names the same day without one, so the time is left for the due date.
 */
function dateOnly(r: ReturnType<typeof chrono.parse>[number] | undefined, now: Date): ReturnType<typeof chrono.parse>[number] | undefined {
  if (!r || !r.start.isCertain("hour")) return r;
  const words = [...r.text.matchAll(/\S+/g)];
  for (const w of words.slice(0, -1)) {
    const head = r.text.slice(0, w.index + w[0].length);
    const h = parser.parse(head, now, { forwardDate: true })[0];
    if (h && h.index === 0 && h.text === head && hasDay(h) && !h.start.isCertain("hour")) return h;
  }
  return r;
}

/**
 * A repeat's end date as "YYYY-MM-DD". A date given without a year ("nov 26")
 * is the next one on or after the first occurrence, `due`; a relative one
 * ("in 3 weeks", "friday") counts from now as chrono read it.
 */
function untilDate(r: ReturnType<typeof chrono.parse>[number], due: Date): string {
  const d = r.start.date();
  if (r.start.isCertain("year") || !r.start.isCertain("month")) return ymd(d);
  const month = d.getMonth();
  const day = d.getDate();
  const from = startOfDay(due);
  for (let y = from.getFullYear(); ; y++) {
    const at = new Date(y, month, day);
    // 29 February lands only in a leap year.
    if (at.getMonth() === month && at >= from) return ymd(at);
  }
}

/** Whether a date names its day ("friday", "tomorrow", "Oct 20") rather than only a time. */
function hasDay(r: ReturnType<typeof chrono.parse>[number]): boolean {
  return r.start.isCertain("day") || r.start.isCertain("weekday");
}

/** The time of day a date gives, as resolve reads it, for a repeat's first occurrence. */
function timeOf(r: ReturnType<typeof chrono.parse>[number]): [number, number] {
  if (TONIGHT.test(r.text) && !r.start.isCertain("hour")) return [TONIGHT_HOUR, 0];
  if (r.start.isCertain("hour") || PART_OF_DAY.test(r.text)) return [r.start.get("hour") ?? DEFAULT_HOUR, r.start.get("minute") ?? 0];
  return [DEFAULT_HOUR, 0];
}

function resolve(r: ReturnType<typeof chrono.parse>[number], now: Date): Date {
  const d = r.start.date();
  if (TONIGHT.test(r.text) && !r.start.isCertain("hour")) {
    return new Date(d.getFullYear(), d.getMonth(), d.getDate(), TONIGHT_HOUR, 0);
  }
  if (r.start.isCertain("hour") || PART_OF_DAY.test(r.text)) {
    return new Date(d.getFullYear(), d.getMonth(), d.getDate(), d.getHours(), d.getMinutes());
  }
  const morning = new Date(d.getFullYear(), d.getMonth(), d.getDate(), DEFAULT_HOUR, 0);
  if (morning > now) return morning;
  // "today" after 9:00: the next full hour, so the task does not start out overdue.
  const next = new Date(now.getFullYear(), now.getMonth(), now.getDate(), now.getHours() + 1, 0);
  return next;
}

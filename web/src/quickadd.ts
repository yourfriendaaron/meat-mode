// Quick add parser: a title with an optional natural-language date, repeat
// and #list. Pure: the caller passes `now` and the lists it may match.

import * as chrono from "chrono-node/en";
import { findRepeat, firstOccurrence, type RepeatRule } from "./repeat.ts";

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
  list: string | null;
  tokens: QuickToken[];
}

export interface QuickOptions {
  /** Whether to read a repeat, which only the personal list takes. Default yes. */
  repeat?: boolean;
}

/** Hour used for "tonight". */
export const TONIGHT_HOUR = 20;
/** Hour used for a date given without a time. */
export const DEFAULT_HOUR = 9;

const PART_OF_DAY = /\b(morning|noon|midday|afternoon|evening|night|midnight)\b/i;
const TONIGHT = /\btonight\b/i;
const LIST_TAG = /(^|\s)#([\p{L}\p{N}_.-]+)/gu;

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
  // read "every monday" as next Monday.
  let repeat: RepeatRule | null = null;
  let dateText = text;
  const rm = opts.repeat === false ? null : findRepeat(text);
  if (rm && !tokens.some((t) => rm.start < t.end && t.start < rm.end)) {
    repeat = rm.rule;
    tokens.push({ kind: "repeat", start: rm.start, end: rm.end, text: rm.text });
    dateText = text.slice(0, rm.start) + " ".repeat(rm.end - rm.start) + text.slice(rm.end);
  }

  let due: Date | null = null;
  const results = chrono.parse(dateText, now, { forwardDate: true });
  for (const r of results) {
    // Chrono can take in spaces before a match, such as a blanked repeat's.
    const at = r.index + r.text.length - r.text.trimStart().length;
    // "in the morning", "at noon", "from friday": chrono leaves out the words that lead in.
    const lead = /\b(?:in the|at|on|from|starting(?: on| from)?)\s+$/i.exec(dateText.slice(0, at));
    const start = lead ? lead.index : at;
    const end = r.index + r.text.length;
    if (tokens.some((t) => start < t.end && t.start < end)) continue;
    due = repeat && !hasDay(r) ? firstOccurrence(repeat, now, ...timeOf(r)) : resolve(r, now);
    tokens.push({ kind: "date", start, end, text: text.slice(start, end) });
    break;
  }
  if (repeat && !due) due = firstOccurrence(repeat, now, DEFAULT_HOUR, 0);

  tokens.sort((a, b) => a.start - b.start);
  let title = "";
  let pos = 0;
  for (const t of tokens) {
    title += text.slice(pos, t.start);
    pos = t.end;
  }
  title += text.slice(pos);
  title = title.replace(/\s+/g, " ").trim();
  return { title, due, repeat, list, tokens };
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

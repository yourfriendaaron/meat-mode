// Quick add parser: a title with an optional natural-language date and #list.
// Pure: the caller passes `now` and the lists it may match.

import * as chrono from "chrono-node/en";

export interface QuickList {
  /** Repo key, `<bridge>/<repo>`. */
  key: string;
  /** Repo name, matched by `#prefix`. */
  name: string;
}

export interface QuickToken {
  kind: "date" | "list";
  start: number;
  end: number;
  text: string;
}

export interface QuickAdd {
  title: string;
  due: Date | null;
  list: string | null;
  tokens: QuickToken[];
}

/** Hour used for "tonight". */
export const TONIGHT_HOUR = 20;
/** Hour used for a date given without a time. */
export const DEFAULT_HOUR = 9;

const PART_OF_DAY = /\b(morning|noon|midday|afternoon|evening|night|midnight)\b/i;
const TONIGHT = /\btonight\b/i;
const LIST_TAG = /(^|\s)#([\p{L}\p{N}_.-]+)/gu;

export function parseQuickAdd(text: string, now: Date, lists: QuickList[]): QuickAdd {
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

  let due: Date | null = null;
  const results = chrono.parse(text, now, { forwardDate: true });
  for (const r of results) {
    const start = r.index;
    const end = r.index + r.text.length;
    if (tokens.some((t) => start < t.end && t.start < end)) continue;
    due = resolve(r, now);
    tokens.push({ kind: "date", start, end, text: r.text });
    break;
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
  return { title, due, list, tokens };
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

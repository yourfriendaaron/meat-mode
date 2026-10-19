// Tags: an item's labels as the app shows, filters and sets them (docs/api.md,
// Tags). Pure.

import { HUMAN_LABEL, MAX_TAG_LEN, MAX_TAGS, type Item } from "./types.ts";

/** An item's tags as the app shows them: its labels without `human`, in any case. */
export function shownTags(item: Pick<Item, "labels">): string[] {
  return (item.labels ?? []).filter((l) => l.toLowerCase() !== HUMAN_LABEL);
}

/** Whether an item has this tag; tags compare without case. */
export function hasTag(item: Pick<Item, "labels">, tag: string): boolean {
  const want = tag.toLowerCase();
  return shownTags(item).some((l) => l.toLowerCase() === want);
}

/**
 * The hub's tag rules: each tag trimmed, empty ones dropped, then 1 to
 * MAX_TAG_LEN characters with no whitespace, control character or comma; a
 * repeat without case dropped, the first spelling kept; `human` dropped; at
 * most MAX_TAGS. The cleaned list, or the reason it is refused in the hub's
 * words, so the app refuses before it sends.
 */
export function cleanTags(tags: string[]): { tags: string[] } | { error: string } {
  const out: string[] = [];
  const seen = new Set<string>();
  for (const raw of tags) {
    const t = raw.trim();
    if (!t) continue;
    if ([...t].length > MAX_TAG_LEN) return { error: `The tag "${t}" is longer than ${MAX_TAG_LEN} characters.` };
    if (/[\s,\p{Cc}]/u.test(t)) return { error: `The tag "${t}" has a space, comma or control character; a tag is one word.` };
    const fold = t.toLowerCase();
    if (fold === HUMAN_LABEL || seen.has(fold)) continue;
    seen.add(fold);
    out.push(t);
  }
  if (out.length > MAX_TAGS) return { error: `An item has at most ${MAX_TAGS} tags.` };
  return { tags: out };
}

/**
 * The tags typed in the edit form's field: words split at spaces and commas,
 * each without a leading "@", which quick add uses to mark one.
 */
export function typedTags(text: string): string[] {
  return text
    .split(/[\s,]+/)
    .map((t) => t.replace(/^@/, ""))
    .filter(Boolean);
}

/** Whether two tag lists are the same, in the same order. */
export function sameTags(a: string[], b: string[]): boolean {
  return a.length === b.length && a.every((t, i) => t === b[i]);
}

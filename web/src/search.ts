// Search across every item the app holds. Pure: every function takes `now`.

import type { Item } from "./types.ts";
import { buildView, isDeleted, isOpen, type Group } from "./model.ts";

/** Lower case, with accents and other combining marks removed. */
function fold(s: string): string {
  return s.normalize("NFD").replace(/\p{M}/gu, "").toLowerCase();
}

/** Everything about an item a search looks in, folded: title, description, note texts and checklist texts. */
function haystack(item: Item): string {
  return fold(
    [item.title, item.description ?? "", ...(item.comments ?? []).map((c) => c.text), ...(item.checklist ?? []).map((c) => c.text)].join("\n"),
  );
}

function closedAt(item: Item): number {
  const t = item.closedAt ? new Date(item.closedAt).getTime() : 0;
  return Number.isNaN(t) ? 0 : t;
}

/** Items whose title, description, notes or checklist hold every word of `query`, grouped Open, Done, Deleted. */
export function searchGroups(items: Item[], query: string, now: Date): Group[] {
  const terms = fold(query).split(/\s+/).filter(Boolean);
  if (!terms.length) return [];
  const hits = items.filter((i) => {
    const text = haystack(i);
    return terms.every((t) => text.includes(t));
  });
  // Open items keep the order the views give them: by when, then priority, then title.
  const open = buildView("all", hits.filter(isOpen), now).flatMap((g) => g.items);
  const closed = hits.filter((i) => !isOpen(i));
  const newest = (a: Item, b: Item) => closedAt(b) - closedAt(a);
  const groups: Group[] = [
    { id: "found-open", label: "Open", items: open },
    { id: "found-done", label: "Done", items: closed.filter((i) => !isDeleted(i)).sort(newest) },
    { id: "found-deleted", label: "Deleted", items: closed.filter(isDeleted).sort(newest) },
  ];
  return groups.filter((g) => g.items.length);
}

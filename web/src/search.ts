// Search across every item the app holds. Pure: every function takes `now`.

import type { Item } from "./types.ts";
import { buildView, isDeleted, isOpen, parentTitle, type Group } from "./model.ts";
import { shownTags } from "./tags.ts";

/** Lower case, with accents and other combining marks removed. */
function fold(s: string): string {
  return s.normalize("NFD").replace(/\p{M}/gu, "").toLowerCase();
}

/**
 * Everything about an item a search looks in, folded: title, description,
 * note texts, tags and, for a sub-task, its parent's title.
 * A tag goes in as "@tag", so "garden" and "@garden" both find it.
 */
function haystack(item: Item, items: Item[]): string {
  return fold(
    [
      item.title,
      parentTitle(item, items),
      item.description ?? "",
      ...(item.comments ?? []).map((c) => c.text),
      ...shownTags(item).map((t) => `@${t}`),
    ].join("\n"),
  );
}

function closedAt(item: Item): number {
  const t = item.closedAt ? new Date(item.closedAt).getTime() : 0;
  return Number.isNaN(t) ? 0 : t;
}

/**
 * Items whose title, parent's title, description, notes or tags
 * hold every word of `query`, grouped Open, Done, Deleted. `only` narrows the
 * items searched, as a tag filter does.
 */
export function searchGroups(items: Item[], query: string, now: Date, only: (i: Item) => boolean = () => true): Group[] {
  const terms = fold(query).split(/\s+/).filter(Boolean);
  if (!terms.length) return [];
  const hits = items.filter((i) => {
    if (!only(i)) return false;
    const text = haystack(i, items);
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

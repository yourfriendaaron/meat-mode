// The Completed and Deleted views over the 7 days the hub keeps closed items. Pure: every function takes `now`.

import type { Item, Repo } from "./types.ts";
import type { Group } from "./model.ts";
import { DISMISSED, isAgentItem, isOpen, personalRepoKeys } from "./model.ts";
import { addDays, fmtDay, sameDay } from "./dates.ts";

function closedAt(item: Item): Date | null {
  if (!item.closedAt) return null;
  const d = new Date(item.closedAt);
  return Number.isNaN(d.getTime()) ? null : d;
}

/** A closed personal item with reason Dismissed: Delete in the app. */
export function isDeleted(item: Item, personal: Set<string>): boolean {
  return !isOpen(item) && item.closeReason === DISMISSED && !isAgentItem(item, personal);
}

function pad(n: number): string {
  return String(n).padStart(2, "0");
}

/** Closed items that are not deleted personal items, one group per close day, newest first. */
export function completedGroups(items: Item[], repos: Repo[], now: Date): Group[] {
  const personal = personalRepoKeys(repos);
  const closed: { item: Item; at: Date }[] = [];
  for (const item of items) {
    if (isOpen(item) || isDeleted(item, personal)) continue;
    const at = closedAt(item);
    if (at) closed.push({ item, at });
  }
  closed.sort((a, b) => b.at.getTime() - a.at.getTime());
  const groups: Group[] = [];
  let last: Date | null = null;
  for (const { item, at } of closed) {
    if (!last || !sameDay(last, at)) {
      const id = `day-${at.getFullYear()}-${pad(at.getMonth() + 1)}-${pad(at.getDate())}` as const;
      const label = sameDay(at, now) ? "Today" : sameDay(at, addDays(now, -1)) ? "Yesterday" : fmtDay(at);
      groups.push({ id, label, items: [] });
      last = at;
    }
    groups[groups.length - 1].items.push(item);
  }
  return groups;
}

/** Deleted personal items, newest first, in one group. */
export function deletedGroups(items: Item[], repos: Repo[], _now: Date): Group[] {
  const personal = personalRepoKeys(repos);
  const gone = items
    .filter((i) => isDeleted(i, personal))
    .sort((a, b) => (closedAt(b)?.getTime() ?? 0) - (closedAt(a)?.getTime() ?? 0));
  return gone.length ? [{ id: "deleted", label: "Deleted", items: gone }] : [];
}

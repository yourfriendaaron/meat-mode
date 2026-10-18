// The Completed and Deleted views over the 7 days the hub keeps closed items. Pure: every function takes `now`.

import type { Item, Repo } from "./types.ts";
import type { Group } from "./model.ts";

/** Closed items that are not deleted personal items, one group per close day, newest first. */
export function completedGroups(_items: Item[], _repos: Repo[], _now: Date): Group[] {
  return [];
}

/** Deleted personal items, newest first, in one group. */
export function deletedGroups(_items: Item[], _repos: Repo[], _now: Date): Group[] {
  return [];
}

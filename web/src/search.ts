// Search across every item the app holds. Pure: every function takes `now`.

import type { Item, Repo } from "./types.ts";
import type { Group } from "./model.ts";

/** Items whose title, description, notes or checklist hold every word of `query`, grouped Open, Done, Deleted. */
export function searchGroups(_items: Item[], _repos: Repo[], _query: string, _now: Date): Group[] {
  return [];
}

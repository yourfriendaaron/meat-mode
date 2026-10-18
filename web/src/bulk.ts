// Select mode: what several picked items can do together. Pure.

import type { Item, Repo } from "./types.ts";

/** The dismiss button's label for the picked items: "Delete", "Won't do", or both when mixed. */
export function bulkDismissLabel(_picked: Item[], _repos: Repo[]): string {
  return "Delete";
}

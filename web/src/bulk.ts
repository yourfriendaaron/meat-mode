// Select mode: what several picked items can do together. Pure.

import { isAgentItem, isOpen, type Group } from "./model.ts";
import type { Item } from "./types.ts";

/**
 * Whether the Select button shows: on the task lists, and on search results
 * wherever they show. Completed, Deleted and the pages have nothing to pick.
 */
export function canSelect(view: string, query: string): boolean {
  if (view === "settings" || view === "setup") return false;
  if (query.trim()) return true;
  return ["today", "upcoming", "agents", "all"].includes(view) || view.startsWith("list:");
}

/** The open items in the current list, once each and in list order: what Select all picks. */
export function pickableKeys(groups: Group[]): string[] {
  const out = new Set<string>();
  for (const g of groups) for (const i of g.items) if (isOpen(i)) out.add(i.key);
  return [...out];
}

/** The picks that can still be picked, as when a refresh closed one elsewhere. */
export function keepPickable(picked: Set<string>, pickable: string[]): Set<string> {
  const can = new Set(pickable);
  return new Set([...picked].filter((k) => can.has(k)));
}

/** The dismiss button's label for the picked items: "Delete", "Won't do", or both when mixed. */
export function bulkDismissLabel(picked: Item[]): string {
  const agents = picked.filter(isAgentItem).length;
  if (!agents) return "Delete";
  if (agents === picked.length) return "Won't do";
  return "Delete or won't do";
}

/** What dismissing the picks did, in the past tense the toast uses. */
export function bulkDismissDone(picked: Item[]): string {
  const label = bulkDismissLabel(picked);
  return label === "Delete" ? "Deleted" : label === "Won't do" ? "Won't do" : "Deleted or won't do";
}

/** A batch's toast: the one title when there is one, else the count. */
export function bulkToast(verb: string, titles: string[]): string {
  return titles.length === 1 ? `${verb}: ${titles[0]}` : `${verb}: ${titles.length} tasks`;
}

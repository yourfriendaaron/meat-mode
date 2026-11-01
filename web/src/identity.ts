// A list's one mark, drawn the same way wherever the app names the list
// (docs/api.md, List identity): the sidebar's lists, the list chip on a task,
// the task sheet's List line, Settings and Epics. The mark is the list's
// colour slot plus a badge: the repo's icon on a neutral tile where the hub
// has one, otherwise the first letter of its name on a square in its colour.
// The personal list keeps its own mark. An icon is only ever an <img>, never
// inline markup, so an SVG's scripts never run.

import { esc } from "./html.ts";
import { ICON } from "./icons.ts";
import { isPersonalList, LIST_COLOURS, repoColours, repoKey, repoLabel } from "./model.ts";
import type { Repo } from "./types.ts";

/** An icon hash as the hub gives it (api.ValidIconHash): 32 lowercase hex digits. */
const ICON_HASH = /^[0-9a-f]{32}$/;

/** Icons whose image failed to load, by hash: their badges show the letter instead. */
const failed = new Set<string>();

/**
 * The list's colour slot, 0 to 5: the hub's `colour`, or for an older hub,
 * whose repos carry none, the slot by the order the app lists them.
 */
export function listSlot(r: Repo, repos: Repo[]): number {
  const c = r.colour;
  if (typeof c === "number" && Number.isFinite(c)) return ((Math.trunc(c) % LIST_COLOURS) + LIST_COLOURS) % LIST_COLOURS;
  return repoColours(repos).get(repoKey(r)) ?? 0;
}

/** The first character (code point) of the list's name, upper case. */
export function listInitial(r: { name: string }): string {
  const first = [...repoLabel(r)][0] ?? "?";
  return first.toLocaleUpperCase();
}

/** Badge sizes: small in chips, medium in rows and the sidebar, large in a heading. */
export type BadgeSize = "s" | "m" | "l";

/**
 * The list's badge, hidden from screen readers since its name is always
 * next to it. The colour class (lc0 to lc5) also sets --repo for anything
 * else drawn in the list's colour.
 */
export function listBadge(r: Repo, repos: Repo[], size: BadgeSize = "m"): string {
  const cls = `rb rb-${size}`;
  if (isPersonalList(r)) return `<span class="${cls} rb-own" aria-hidden="true">${ICON.check}</span>`;
  const slot = listSlot(r, repos);
  const letter = esc(listInitial(r));
  if (r.icon && ICON_HASH.test(r.icon) && !failed.has(r.icon)) {
    return `<span class="${cls} rb-icon lc${slot}" aria-hidden="true"><img src="/api/icons/${r.icon}" alt="" loading="lazy" data-icon="${r.icon}"><span class="rb-t">${letter}</span></span>`;
  }
  return `<span class="${cls} lc${slot}" aria-hidden="true">${letter}</span>`;
}

/** The colour class of a list, for a section drawn in its colour. */
export function listColourClass(r: Repo, repos: Repo[]): string {
  return isPersonalList(r) ? "lc-own" : `lc${listSlot(r, repos)}`;
}

/**
 * Forgets the icons that failed to load, so the next render tries them
 * again: a failure may have been a moment offline or an expired session.
 */
export function retryIcons(): void {
  failed.clear();
}

/**
 * Turns a badge whose icon failed to load into its letter, and remembers
 * the hash so the next render draws the letter at once. Listen for "error"
 * in the capture phase, since an image's error does not bubble.
 */
export function onIconError(e: Event): void {
  const img = e.target;
  if (!(img instanceof HTMLImageElement) || !img.dataset.icon) return;
  failed.add(img.dataset.icon);
  img.parentElement?.classList.remove("rb-icon");
  img.remove();
}

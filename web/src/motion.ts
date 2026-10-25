// Motion for what CSS cannot reach: a node a rewrite has just made, which CSS
// cannot tell apart from the same node written again. The caller knows what is
// new (another page in the sheet, a row that was not there, the next month) and
// asks for its entrance here. Durations and easings come from the --motion-*
// tokens in styles.css, which prefers-reduced-motion sets to 0, and a duration
// of 0 plays nothing. Entrances use the Web Animations API with no fill, so the
// node ends in its own style and takes input throughout.

/** How a new node arrives. */
export type Entrance =
  /** Fades in where it stands: content that replaces other content in place. */
  | "fade"
  /** Rises a little as it fades in: something added to a list. */
  | "rise"
  /** Comes in from the right: a page opened on top of the one before. */
  | "push"
  /** Comes in from the left: the page beneath, on the way back. */
  | "pop";

const FRAMES: Record<Entrance, Keyframe[]> = {
  fade: [{ opacity: 0 }, { opacity: 1 }],
  rise: [
    { opacity: 0, transform: "translateY(6px)" },
    { opacity: 1, transform: "none" },
  ],
  push: [
    { opacity: 0, transform: "translateX(32px)" },
    { opacity: 1, transform: "none" },
  ],
  pop: [
    { opacity: 0, transform: "translateX(-32px)" },
    { opacity: 1, transform: "none" },
  ],
};

function token(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

/** A duration token in milliseconds; 0 where motion is reduced or the token is missing. */
export function duration(name: "--motion-in" | "--motion-out" | "--motion-sheet-in" | "--motion-sheet-out"): number {
  const v = token(name);
  const n = parseFloat(v);
  if (!Number.isFinite(n)) return 0;
  return v.endsWith("ms") ? n : n * 1000;
}

/** Plays `kind` on each element; does nothing where motion is reduced. */
export function enter(els: Element | null | undefined | Iterable<Element>, kind: Entrance): void {
  if (!els) return;
  const list = els instanceof Element ? [els] : [...els];
  if (!list.length) return;
  const ms = duration("--motion-in");
  if (!ms) return;
  const easing = token("--ease-out") || "ease-out";
  for (const el of list) {
    if (typeof (el as HTMLElement).animate === "function") el.animate(FRAMES[kind], { duration: ms, easing });
  }
}

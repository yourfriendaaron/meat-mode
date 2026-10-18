/**
 * Keeps the bottom sheets above the on-screen keyboard.
 *
 * Chrome on Android (since 108) and Safari on iOS open the keyboard over the
 * page: only the visual viewport shrinks, and anything fixed to the bottom of
 * the layout viewport ends up underneath it. This publishes how much of the
 * layout viewport's bottom the keyboard covers as `--kb-inset`, and the height
 * still visible as `--kb-room`, on the root element while a keyboard is open
 * (and `--kb-safe-bottom: 0px`, since the keyboard covers the home indicator).
 * The sheets' CSS lifts and caps itself with them, and falls back to its
 * normal look when they are absent.
 */

export interface ViewportSample {
  /** Height of the layout viewport, which `position: fixed; bottom: 0` follows. */
  layoutHeight: number;
  /** `visualViewport.height`. */
  height: number;
  /** `visualViewport.offsetTop`. */
  offsetTop: number;
  /** `visualViewport.scale`. */
  scale: number;
}

/**
 * Below this, a gap between the viewports is browser chrome such as a
 * collapsing address bar, not a keyboard.
 */
export const MIN_KEYBOARD = 100;

/**
 * Pixels of the layout viewport's bottom hidden behind the keyboard, or 0 when
 * no keyboard is open. A pinch zoom shrinks the visual viewport the same way,
 * so a zoomed page reports 0 rather than moving the sheets around.
 */
export function keyboardInset(s: ViewportSample): number {
  if (s.scale > 1.01) return 0;
  const inset = Math.round(s.layoutHeight - s.height - s.offsetTop);
  return inset >= MIN_KEYBOARD ? inset : 0;
}

/** Starts tracking the visual viewport. Does nothing where it is not available. */
export function trackKeyboard(root: HTMLElement = document.documentElement): void {
  const vv = window.visualViewport;
  if (!vv) return;
  let frame = 0;
  let open = false;
  const update = () => {
    cancelAnimationFrame(frame);
    frame = requestAnimationFrame(() => {
      const inset = keyboardInset({ layoutHeight: root.clientHeight, height: vv.height, offsetTop: vv.offsetTop, scale: vv.scale });
      if (inset > 0) {
        root.style.setProperty("--kb-inset", `${inset}px`);
        root.style.setProperty("--kb-room", `${Math.floor(vv.height)}px`);
        // The keyboard covers the home indicator, so the sheets drop that padding.
        root.style.setProperty("--kb-safe-bottom", "0px");
      } else {
        root.style.removeProperty("--kb-inset");
        root.style.removeProperty("--kb-room");
        root.style.removeProperty("--kb-safe-bottom");
      }
      // The sheet just moved: bring the field being typed in back into view
      // inside it, once, as the keyboard opens.
      if (inset > 0 && !open) {
        const el = document.activeElement;
        const sheet = el instanceof HTMLElement ? el.closest<HTMLElement>(".qa, .detail, .dialog-card") : null;
        if (el && sheet) revealIn(sheet, el);
      }
      open = inset > 0;
    });
  };
  vv.addEventListener("resize", update);
  vv.addEventListener("scroll", update);
  update();
}

/**
 * Scrolls only the sheet itself, never the page behind it, so `el` is in view,
 * along with the button right after it (Save note) when both fit.
 */
function revealIn(sheet: HTMLElement, el: Element) {
  const box = sheet.getBoundingClientRect();
  const r = el.getBoundingClientRect();
  const next = el.nextElementSibling;
  const nb = next instanceof HTMLButtonElement ? next.getBoundingClientRect() : r;
  const bottom = nb.bottom - r.top <= box.height ? Math.max(r.bottom, nb.bottom) : r.bottom;
  if (bottom > box.bottom) sheet.scrollTop += bottom - box.bottom;
  else if (r.top < box.top) sheet.scrollTop -= box.top - r.top;
}

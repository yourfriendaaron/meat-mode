import { test, expect, openApp, row } from "./setup.ts";
import type { Locator, Page } from "@playwright/test";

test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });

/**
 * Playwright has no on-screen keyboard. Its only effect the page can see is
 * the visual viewport shrinking (and, on iOS, panning) while the layout
 * viewport keeps its full height, which is what Chrome on Android and Safari
 * on iOS do by default. This swaps in a visual viewport the test can drive.
 */
test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    class FakeViewport extends EventTarget {
      kb = 0;
      pan = 0;
      get width() { return window.innerWidth; }
      get height() { return window.innerHeight - this.kb; }
      get offsetTop() { return this.pan; }
      get offsetLeft() { return 0; }
      get pageTop() { return this.pan; }
      get pageLeft() { return 0; }
      get scale() { return 1; }
    }
    const vv = new FakeViewport();
    Object.defineProperty(window, "visualViewport", { configurable: true, get: () => vv });
    (window as unknown as { keyboard: (kb: number, pan?: number) => void }).keyboard = (kb, pan = 0) => {
      vv.kb = kb;
      vv.pan = pan;
      vv.dispatchEvent(new Event("resize"));
      vv.dispatchEvent(new Event("scroll"));
    };
  });
});

const KEYBOARD = 336;

async function keyboard(page: Page, kb: number, pan = 0) {
  await page.evaluate(([k, p]) => (window as unknown as { keyboard: (kb: number, pan?: number) => void }).keyboard(k, p), [kb, pan]);
}

/** The part of the layout viewport the visual viewport shows, in client coordinates. */
async function visible(page: Page) {
  return page.evaluate(() => {
    const vv = window.visualViewport!;
    return { top: vv.offsetTop, bottom: vv.offsetTop + vv.height };
  });
}

/** Waits until the element sits entirely inside the visual viewport. */
async function expectInVisualViewport(page: Page, el: Locator) {
  await expect
    .poll(async () => {
      const box = await el.boundingBox();
      const v = await visible(page);
      if (!box) return "no box";
      return box.y >= v.top - 1 && box.y + box.height <= v.bottom + 1 ? "inside" : `${box.y}..${box.y + box.height} outside ${v.top}..${v.bottom}`;
    })
    .toBe("inside");
}

test("the New task sheet rides above the keyboard and settles back when it closes", async ({ page, hub }) => {
  void hub;
  await openApp(page);
  await page.getByRole("button", { name: "New task" }).click();
  const form = page.locator("#qa-form");
  const input = page.getByRole("textbox", { name: "New task" });
  const add = form.getByRole("button", { name: "Add" });
  await expect(input).toBeFocused();
  const rest = (await form.boundingBox())!;
  expect(rest.y + rest.height).toBeCloseTo(844, 0);

  await keyboard(page, KEYBOARD);
  await expectInVisualViewport(page, add);
  await expectInVisualViewport(page, input);
  await expectInVisualViewport(page, page.locator("#qa-list"));
  await expectInVisualViewport(page, form.getByRole("button", { name: "Close" }));

  // iOS pans the visual viewport toward the focused field as well.
  await keyboard(page, KEYBOARD, 120);
  await expectInVisualViewport(page, add);
  await expectInVisualViewport(page, form.getByRole("button", { name: "Close" }));

  await keyboard(page, 0);
  await expect.poll(async () => (await form.boundingBox())!).toEqual(rest);
});

test("the note field and Save note stay above the keyboard in the detail sheet", async ({ page, hub }) => {
  void hub;
  await openApp(page);
  await row(page, "Order compost for the beds").locator(".row-body").click();
  const sheet = page.locator("#detail");
  const note = sheet.getByLabel("Add a note");
  await note.focus();
  await keyboard(page, KEYBOARD);
  await expectInVisualViewport(page, note);
  await expectInVisualViewport(page, sheet.getByRole("button", { name: "Save note" }));
  // The whole sheet fits the visible area; the rest of it scrolls inside.
  await expectInVisualViewport(page, sheet);
});

test("the snooze dialog's time fields stay above the keyboard", async ({ page, hub }) => {
  void hub;
  await openApp(page);
  const r = row(page, "Call Sam about the weekend");
  // Open the snooze dialog the way the phone does: swipe left.
  const box = (await r.locator(".row-fg").boundingBox())!;
  const y = box.y + box.height / 2;
  const x0 = box.x + box.width / 2;
  const fg = r.locator(".row-fg");
  const fire = (type: string, x: number) =>
    fg.dispatchEvent(type, { pointerId: 7, pointerType: "touch", isPrimary: true, clientX: x, clientY: y, button: 0, bubbles: true });
  await fire("pointerdown", x0);
  for (let i = 1; i <= 6; i++) await fire("pointermove", x0 - (160 * i) / 6);
  await fire("pointerup", x0 - 160);
  const dialog = page.getByRole("dialog", { name: "Snooze" });
  await expect(dialog).toBeVisible();
  const pick = dialog.getByLabel("Pick a time");
  await pick.focus();
  await keyboard(page, KEYBOARD);
  await expectInVisualViewport(page, pick);
  await expectInVisualViewport(page, dialog.getByRole("button", { name: "Snooze", exact: true }));
  const typed = dialog.getByRole("textbox", { name: "Type a time" });
  await typed.focus();
  await typed.fill("8p");
  await expectInVisualViewport(page, typed);
  await expectInVisualViewport(page, dialog.getByText("Until 20:00"));
  await expectInVisualViewport(page, dialog.getByRole("button", { name: "Snooze to the typed time" }));
});

import { test as base, expect, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { FakeHub, NOW } from "./fake-hub.ts";
import { DEVICE_KEY, PROGRESS_KEY, type Progress } from "../../src/wizard/rules.ts";

export const test = base.extend<{ hub: FakeHub; freshDevice: boolean }>({
  /**
   * Whether this device starts with no setup record. Off by default: the
   * device part was dismissed, so the offer to set up this device stays out
   * of tests about other things. `test.use({ freshDevice: true })` turns it on.
   */
  freshDevice: [false, { option: true }],
  hub: async ({ page, freshDevice }, use) => {
    const hub = new FakeHub();
    await page.clock.setFixedTime(NOW);
    if (!freshDevice) {
      await page.addInitScript((key) => {
        if (localStorage.getItem(key) === null) localStorage.setItem(key, "dismissed");
      }, DEVICE_KEY);
    }
    await hub.install(page);
    await use(hub);
  },
});

export { expect };

/** Opens the phone's menu drawer and returns it. */
export async function openMenu(page: Page) {
  await page.locator(".mhead").getByRole("button", { name: "Menu" }).click();
  const drawer = page.locator("#sidebar");
  await expect(drawer).toBeVisible();
  return drawer;
}

/** Opens the app and waits for the first state to render. */
export async function openApp(page: Page, path = "/") {
  await page.goto(path);
  await expect(page.locator("#list .row").first()).toBeVisible();
}

export function row(page: Page, title: string) {
  return page.locator("#list .row", { has: page.locator(".row-title", { hasText: title }) });
}

/**
 * Waits for every animation that ends to end, so checks read the screen at
 * rest rather than halfway through a fade or slide. Endless ones (a run's
 * pulse) are left running.
 */
export async function settle(page: Page) {
  await page.evaluate(() =>
    Promise.all(
      document
        .getAnimations()
        .filter((a) => a.effect?.getComputedTiming().endTime !== Infinity)
        .map((a) => a.finished.catch(() => undefined)),
    ),
  );
}

export async function axe(page: Page) {
  await settle(page);
  const result = await new AxeBuilder({ page }).analyze();
  const summary = result.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`);
  expect(summary, "axe violations").toEqual([]);
}

/** The setup wizard's region, its step heading and its frame buttons. */
export function wizard(page: Page) {
  const region = page.locator("#wizard");
  return {
    region,
    heading: region.locator("#wiz-h"),
    next: region.locator("[data-act=wizard-next]"),
    skip: region.getByRole("button", { name: "Skip" }),
    back: region.getByRole("button", { name: "Back" }),
    close: region.getByRole("button", { name: "Close setup" }),
  };
}

/**
 * Opens the app on a fresh hub with the wizard at step `at` (a step id),
 * as if the user had got there and reloaded. Steps before it count as done.
 */
export async function openWizardAt(page: Page, hub: FakeHub, at: string, mode: Progress["mode"] = "first") {
  hub.fresh();
  const progress: Progress = { mode, step: at, done: [], skipped: [] };
  // Once per tab, so a reload in the test resumes rather than starts again.
  await page.addInitScript(
    ([key, value]) => {
      if (sessionStorage.getItem("seeded-setup")) return;
      sessionStorage.setItem("seeded-setup", "1");
      localStorage.setItem(key, value);
    },
    [PROGRESS_KEY, JSON.stringify(progress)] as const,
  );
  await page.goto("/");
  await expect(wizard(page).region.locator(`[data-step="${at}"]`)).toBeVisible();
}

/** Skips every step after the one on screen until Finish ("All set"), so a test about the frame does not depend on which steps are registered. */
export async function skipToFinish(page: Page) {
  const w = wizard(page);
  for (let i = 0; i < 30; i++) {
    await expect(w.heading).not.toHaveText("");
    if ((await w.heading.textContent()) === "All set") return;
    const id = await w.region.locator("[data-step]").getAttribute("data-step");
    await w.skip.click();
    await expect(w.region.locator(`[data-step="${id}"]`)).toHaveCount(0);
  }
  throw new Error("setup did not reach Finish");
}

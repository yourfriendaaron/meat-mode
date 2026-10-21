import { test, expect, openApp, row } from "./setup.ts";
import type { Page } from "@playwright/test";

test.use({ viewport: { width: 1280, height: 800 } });

// Counts the tones the app starts, so a test can tell a sound played without hearing it.
test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    const w = window as unknown as { tones: number };
    w.tones = 0;
    const make = AudioContext.prototype.createOscillator;
    AudioContext.prototype.createOscillator = function (this: AudioContext) {
      w.tones++;
      return make.call(this);
    };
  });
});

const tones = (page: Page) => page.evaluate(() => (window as unknown as { tones: number }).tones);

test("done slides the row away with a sound, and Settings turns sounds off", async ({ page, hub }) => {
  await openApp(page);
  const water = row(page, "Water the plants");
  await water.getByRole("button", { name: "Mark done: Water the plants" }).click();
  await expect(water).toHaveClass(/leaving/);
  await expect.poll(() => hub.posted.at(-1)).toMatchObject({ kind: "done", key: "hub/personal/pe-1" });
  await expect(page.locator(".group-done")).toContainText("Water the plants");
  expect(await tones(page)).toBeGreaterThan(0);

  await page.locator("#toast").getByRole("button", { name: "Undo" }).click();
  await expect.poll(() => hub.posted.at(-1)).toMatchObject({ kind: "reopen", key: "hub/personal/pe-1" });

  await page.locator(".sidebar").getByRole("button", { name: "Settings" }).click();
  const sw = page.getByRole("switch", { name: "Sounds on this device" });
  await expect(sw).toHaveAttribute("aria-checked", "true");
  await sw.click();
  await expect(sw).toHaveAttribute("aria-checked", "false");
  expect(await page.evaluate(() => localStorage.getItem("meat-mode.sound"))).toBe("off");

  await page.reload();
  const before = await tones(page);
  await page.locator(".sidebar").getByRole("button", { name: "Today" }).click();
  await row(page, "Water the plants").getByRole("button", { name: "Mark done: Water the plants" }).click();
  await expect(page.locator(".group-done")).toContainText("Water the plants");
  expect(await tones(page)).toBe(before);
});

test("reduced motion marks done without the slide", async ({ page, hub }) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  await openApp(page);
  const water = row(page, "Water the plants");
  await water.getByRole("button", { name: "Mark done: Water the plants" }).click();
  await expect.poll(() => hub.posted.at(-1)).toMatchObject({ kind: "done", key: "hub/personal/pe-1" });
  await expect(page.locator(".group-done")).toContainText("Water the plants");
  await expect(page.locator("#list .row.leaving")).toHaveCount(0);
});

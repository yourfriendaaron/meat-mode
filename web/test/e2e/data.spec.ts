import { readFile } from "node:fs/promises";
import { test, expect, openApp, axe } from "./setup.ts";
import type { Page } from "@playwright/test";

const FILE = { format: "meat-mode-export", version: 1, items: [{ title: "Water the plants" }] };

/** Answers GET /api/export as the hub does, counting the calls. */
async function serveExport(page: Page, status = 200): Promise<{ calls: number }> {
  const seen = { calls: 0 };
  await page.route("**/api/export", (route) => {
    seen.calls++;
    if (status !== 200) {
      return route.fulfill({ status, contentType: "application/json", body: JSON.stringify({ error: "the database is busy" }) });
    }
    return route.fulfill({
      status: 200,
      contentType: "application/json",
      headers: { "Content-Disposition": 'attachment; filename="meat-mode-export-2026-10-14.json"' },
      body: JSON.stringify(FILE),
    });
  });
  return seen;
}

async function openSettings(page: Page) {
  await openApp(page);
  await page.locator(".sidebar").getByRole("button", { name: "Settings" }).click();
}

test.describe("desktop", () => {
  test.use({ viewport: { width: 1280, height: 800 } });

  test("Download your data saves the hub's file under its name", async ({ page, hub: _hub }) => {
    const seen = await serveExport(page);
    await openSettings(page);
    const section = page.locator("[data-section=data]");
    await expect(section.locator("h2")).toHaveText("Your data");
    await expect(section).toContainText("a new hub can be set up from it");
    await expect(section).toContainText("every password, key and private link");
    await axe(page);

    const button = section.getByRole("button", { name: "Download your data" });
    const [download] = await Promise.all([page.waitForEvent("download"), button.click()]);
    expect(download.suggestedFilename()).toBe("meat-mode-export-2026-10-14.json");
    expect(JSON.parse(await readFile((await download.path())!, "utf8"))).toEqual(FILE);
    expect(seen.calls).toBe(1);
    await expect(button).toBeEnabled();
    await expect(button).toBeFocused();
  });

  test("a failed download says why and saves nothing", async ({ page, hub: _hub }) => {
    await serveExport(page, 500);
    await openSettings(page);
    let downloads = 0;
    page.on("download", () => downloads++);
    await page.locator("[data-section=data]").getByRole("button", { name: "Download your data" }).click();
    await expect(page.getByText("Could not download your data: the database is busy")).toBeVisible();
    expect(downloads).toBe(0);
  });
});

test.describe("phone", () => {
  test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });

  test("the section fits a phone", async ({ page, hub: _hub }) => {
    await serveExport(page);
    await openApp(page);
    await page.locator("#tabs").getByRole("button", { name: "Browse" }).click();
    await page.locator("#browse").getByRole("button", { name: "Settings" }).click();
    const section = page.locator("[data-section=data]");
    await expect(section.getByRole("button", { name: "Download your data" })).toBeVisible();
    const [scroll, inner] = await page.evaluate(() => [document.documentElement.scrollWidth, window.innerWidth]);
    expect(scroll).toBeLessThanOrEqual(inner);
    await axe(page);
  });
});

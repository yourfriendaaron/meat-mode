import { readFile } from "node:fs/promises";
import { test, expect, openMenu, openApp, axe } from "./setup.ts";
import type { Page } from "@playwright/test";

/** What the test server answers for GET /api/export, standing in for the zip (test/serve.mjs). */
const ZIP = Buffer.from("PK\x03\x04 an export", "latin1");

async function openSettings(page: Page) {
  await openApp(page);
  await page.locator(".sidebar").getByRole("button", { name: "Settings" }).click();
}

test.describe("desktop", () => {
  test.use({ viewport: { width: 1280, height: 800 } });

  test("Download your data saves the hub's zip under its name", async ({ page, hub: _hub }) => {
    await openSettings(page);
    const section = page.locator("[data-section=data]");
    await expect(section.locator("h2")).toHaveText("Your data");
    await expect(section).toContainText("your projects and their files");
    await expect(section).toContainText("a new hub can be set up from it");
    await expect(section).toContainText("every password, key and private link");
    await axe(page);

    const button = section.getByRole("button", { name: "Download your data" });
    const [download] = await Promise.all([page.waitForEvent("download"), button.click()]);
    expect(download.suggestedFilename()).toBe("meat-mode-export-2026-10-14.zip");
    expect(download.url()).toMatch(/\/api\/export$/);
    expect(await readFile((await download.path())!)).toEqual(ZIP);
    await expect(button).toBeEnabled();
    await expect(button).toBeFocused();
  });

  test("a reply that names no file saves meat-mode-export.zip", async ({ page, hub: _hub, baseURL }) => {
    await page.context().addCookies([{ name: "e2e-export", value: "unnamed", url: baseURL! }]);
    await openSettings(page);
    const button = page.locator("[data-section=data]").getByRole("button", { name: "Download your data" });
    const [download] = await Promise.all([page.waitForEvent("download"), button.click()]);
    expect(download.suggestedFilename()).toBe("meat-mode-export.zip");
  });

  test("a hub that cannot answer says why and saves nothing", async ({ page, hub: _hub }) => {
    await openSettings(page);
    await page.route("**/api/settings", (route) =>
      route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify({ error: "the hub is starting" }) }),
    );
    let downloads = 0;
    page.on("download", () => downloads++);
    await page.locator("[data-section=data]").getByRole("button", { name: "Download your data" }).click();
    await expect(page.getByText("Could not download your data: the hub is starting")).toBeVisible();
    expect(downloads).toBe(0);
  });
});

test.describe("phone", () => {
  test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });

  test("the section fits a phone", async ({ page, hub: _hub }) => {
    await openApp(page);
    await (await openMenu(page)).getByRole("button", { name: "Settings" }).click();
    const section = page.locator("[data-section=data]");
    await expect(section.getByRole("button", { name: "Download your data" })).toBeVisible();
    const [scroll, inner] = await page.evaluate(() => [document.documentElement.scrollWidth, window.innerWidth]);
    expect(scroll).toBeLessThanOrEqual(inner);
    await axe(page);
  });
});

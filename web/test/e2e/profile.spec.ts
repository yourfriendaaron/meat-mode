import { test, expect, openApp, axe, wizard, openWizardAt, skipToFinish } from "./setup.ts";
import type { Page } from "@playwright/test";

async function lightTheme(page: Page) {
  await page.evaluate(() => {
    localStorage.setItem("meat-mode.theme", "light");
    document.documentElement.dataset.theme = "light";
  });
}

async function noHorizontalScroll(page: Page) {
  const [scroll, inner] = await page.evaluate(() => [document.documentElement.scrollWidth, window.innerWidth]);
  expect(scroll).toBeLessThanOrEqual(inner);
}

test.describe("desktop", () => {
  test.use({ viewport: { width: 1280, height: 800 }, timezoneId: "Europe/Paris" });

  test("the step prefills the browser's zone and Continue keeps both", async ({ page, hub }) => {
    await openWizardAt(page, hub, "profile");
    const w = wizard(page);
    await expect(w.heading).toHaveText("Name and time zone");
    const name = w.region.getByRole("textbox", { name: "Name", exact: true });
    const zone = w.region.getByRole("combobox", { name: "Time zone", exact: true });
    await expect(name).toHaveValue("alex");
    await expect(zone).toHaveValue("Europe/Paris");
    await expect(w.region).toContainText("No zone is kept yet");
    await axe(page);
    await lightTheme(page);
    await axe(page);

    await name.fill("Sam");
    await w.next.click();
    await expect(w.heading).not.toHaveText("Name and time zone");
    await skipToFinish(page);
    expect(hub.settingsPuts).toEqual([{ name: "Sam", timeZone: "Europe/Paris" }]);
    expect(hub.setup).toMatchObject({ userName: "Sam", timeZone: "Europe/Paris" });
    await expect(w.region.locator(".wiz-summary li", { hasText: "Name and time zone" })).toContainText("Set up");
  });

  test("a zone that is not one is refused and the step stays", async ({ page, hub }) => {
    await openWizardAt(page, hub, "profile");
    const w = wizard(page);
    await w.region.getByRole("combobox", { name: "Time zone", exact: true }).fill("Mars/Olympus");
    await w.next.click();
    await expect(w.region.getByRole("alert")).toContainText("Mars/Olympus");
    await expect(w.heading).toHaveText("Name and time zone");
    expect(hub.settingsPuts).toEqual([]);
    await axe(page);
  });

  test("skipping keeps nothing; a kept zone shows as set up", async ({ page, hub }) => {
    await openWizardAt(page, hub, "profile");
    const w = wizard(page);
    await w.skip.click();
    await expect(w.heading).not.toHaveText("Name and time zone");
    await skipToFinish(page);
    expect(hub.settingsPuts).toEqual([]);
    const summary = w.region.locator(".wiz-summary li", { hasText: "Name and time zone" });
    await expect(summary).toContainText("Skipped");

    hub.setup.timeZone = "Asia/Tokyo";
    await page.reload();
    await expect(summary).toContainText("Set up");
  });

  test("Settings shows the kept values and saves a change", async ({ page, hub }) => {
    hub.setup.userName = "Alex";
    hub.setup.timeZone = "Asia/Tokyo";
    await openApp(page);
    await page.locator(".sidebar").getByRole("button", { name: "Settings" }).click();
    const section = page.locator("[data-section=profile]");
    await expect(section.locator("h2")).toHaveText("Name and time zone");
    await expect(section.getByRole("textbox", { name: "Name", exact: true })).toHaveValue("Alex");
    await expect(section.getByRole("combobox", { name: "Time zone", exact: true })).toHaveValue("Asia/Tokyo");
    await expect(section).not.toContainText("No zone is kept yet");
    await axe(page);
    await lightTheme(page);
    await axe(page);

    await section.getByRole("textbox", { name: "Name", exact: true }).fill("Sam");
    await section.getByRole("combobox", { name: "Time zone", exact: true }).fill("America/New_York");
    await section.getByRole("button", { name: "Save" }).click();
    await expect(page.locator("#toast")).toContainText("Saved");
    expect(hub.settingsPuts).toEqual([{ name: "Sam", timeZone: "America/New_York" }]);

    // It is the hub that keeps them.
    await page.reload();
    await page.locator(".sidebar").getByRole("button", { name: "Settings" }).click();
    await expect(section.getByRole("textbox", { name: "Name", exact: true })).toHaveValue("Sam");
    await expect(section.getByRole("combobox", { name: "Time zone", exact: true })).toHaveValue("America/New_York");

    // Saving what is there sends nothing.
    await section.getByRole("button", { name: "Save" }).click();
    expect(hub.settingsPuts).toHaveLength(1);
  });
});

test.describe("phone", () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });

  test("the step and the section fit the screen", async ({ page, hub }) => {
    await openWizardAt(page, hub, "profile");
    const w = wizard(page);
    await expect(w.region.getByRole("combobox", { name: "Time zone", exact: true })).toBeVisible();
    await noHorizontalScroll(page);
    await axe(page);
    await w.skip.click();
    await w.next.click();
    await page.locator("#tabs").getByRole("button", { name: "Browse" }).click();
    await page.locator("#browse").getByRole("button", { name: "Settings" }).click();
    await expect(page.locator("[data-section=profile]")).toBeVisible();
    await noHorizontalScroll(page);
    await axe(page);
  });
});

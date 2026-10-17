import { test, expect, openApp, row, axe, wizard, openWizardAt } from "./setup.ts";
import type { Page } from "@playwright/test";

async function lightTheme(page: Page) {
  await page.evaluate(() => {
    localStorage.setItem("meat-mode.theme", "light");
    document.documentElement.dataset.theme = "light";
  });
}

/** Skips the steps between the one on screen and Finish, whichever steps this build has. */
async function skipToEnd(page: Page) {
  const w = wizard(page);
  for (let i = 0; i < 20; i++) {
    const title = (await w.heading.textContent()) ?? "";
    if (title === "All set") return;
    await w.skip.click();
    await expect(w.heading).not.toHaveText(title);
  }
}

async function noHorizontalScroll(page: Page) {
  const [scroll, inner] = await page.evaluate(() => [document.documentElement.scrollWidth, window.innerWidth]);
  expect(scroll).toBeLessThanOrEqual(inner);
}

test.describe("desktop", () => {
  test.use({ viewport: { width: 1280, height: 800 } });

  test("a fresh hub opens the wizard, and Done records it", async ({ page, hub }) => {
    hub.fresh();
    await page.goto("/");
    const w = wizard(page);
    await expect(w.heading).toHaveText("Welcome");
    await expect(page.locator("#view-title")).toHaveText("Setup");
    await expect(page.locator(".wiz-steps [aria-current=step]")).toHaveText("Welcome");
    await expect(w.back).toHaveCount(0);
    await axe(page);
    await lightTheme(page);
    await axe(page);

    await w.next.click();
    await expect(w.heading).not.toHaveText("Welcome");
    await expect(w.heading).toBeFocused();
    await skipToEnd(page);
    await expect(w.heading).toHaveText("All set");
    await expect(w.heading).toBeFocused();
    const n = await page.locator(".wiz-steps li").count();
    await expect(page.getByText(`Step ${n} of ${n}`)).toBeVisible();
    await expect(w.close).toHaveCount(0);
    await expect(w.next).toHaveText("Done");
    await axe(page);

    await w.next.click();
    await expect(page.locator("#view-title")).toHaveText("Today");
    expect(hub.setup.status).toBe("done");
    await page.reload();
    await expect(page.locator("#list .empty")).toHaveText("Nothing due today.");
    await expect(w.region).toBeHidden();
  });

  test("a hub with items does not open the wizard", async ({ page, hub }) => {
    await openApp(page);
    await expect(row(page, "Water the plants")).toBeVisible();
    await expect.poll(() => hub.requests).toContain("GET /api/setup");
    await expect(page.locator("#wizard")).toBeHidden();
    await expect(page.locator("#setup-banner")).toBeHidden();
  });

  test("closing the wizard is remembered", async ({ page, hub }) => {
    hub.fresh();
    await page.goto("/");
    const w = wizard(page);
    await w.close.click();
    await expect(page.locator("#view-title")).toHaveText("Today");
    expect(hub.setup.status).toBe("dismissed");
    await page.reload();
    await expect(page.locator("#list .empty")).toBeVisible();
    await expect.poll(() => hub.requests.filter((r) => r === "GET /api/setup").length).toBe(2);
    await expect(w.region).toBeHidden();
    await expect(page.locator("#setup-banner")).toBeHidden();
  });

  test("the list stays open beside the wizard, and a reload resumes the step", async ({ page, hub }) => {
    hub.fresh();
    await page.goto("/");
    const w = wizard(page);
    await w.next.click();
    await expect(w.heading).not.toHaveText("Welcome");
    await skipToEnd(page);

    await page.locator(".sidebar").getByRole("button", { name: "Today" }).click();
    await expect(page.locator("#list .empty")).toHaveText("Nothing due today.");
    const banner = page.locator("#setup-banner");
    await expect(banner).toContainText("Setup is not finished.");
    await axe(page);

    await page.reload();
    await expect(w.heading).toHaveText("All set");
    await w.back.click();
    await expect(w.heading).not.toHaveText("All set");
    const back = (await w.heading.textContent()) ?? "";

    await page.locator(".sidebar").getByRole("button", { name: "Today" }).click();
    await banner.getByRole("button", { name: "Continue setup" }).click();
    await expect(w.heading).toHaveText(back);
    await expect(w.heading).toBeFocused();

    await page.locator(".sidebar").getByRole("button", { name: "Today" }).click();
    await banner.getByRole("button", { name: "Not now" }).click();
    await expect(banner).toBeHidden();
    expect(hub.setup.status).toBe("dismissed");
  });

  test("Settings runs setup again and sets up this device", async ({ page, hub }) => {
    await openApp(page);
    await page.locator(".sidebar").getByRole("button", { name: "Settings" }).click();
    await expect(page.getByRole("heading", { name: "Setup" })).toBeVisible();
    await axe(page);

    await page.getByRole("button", { name: "Run setup again" }).click();
    const w = wizard(page);
    await expect(w.heading).toHaveText("Welcome");
    await expect(w.region).toContainText("Meat Mode keeps your own to-dos");
    await w.close.click();
    // Closing a run on a hub in use records it as dismissed; nothing opens on its own afterwards.
    expect(hub.setup.status).toBe("dismissed");

    await page.locator(".sidebar").getByRole("button", { name: "Settings" }).click();
    await page.getByRole("button", { name: "Set up this device" }).click();
    await expect(w.heading).toHaveText("Welcome");
    await expect(w.region).toContainText("This sets up Meat Mode on this device.");
    await w.next.click();
    await expect(w.heading).not.toHaveText("Welcome");
    await skipToEnd(page);
    await w.next.click();
    await expect(page.locator("#view-title")).toHaveText("Today");
    expect(await page.evaluate(() => localStorage.getItem("meat-mode.setup-device"))).toBe("done");
  });

  test("moving to another view before the hub answers keeps the user there", async ({ page, hub }) => {
    hub.fresh();
    hub.setupDelayMs = 600;
    await page.goto("/");
    await page.locator(".sidebar").getByRole("button", { name: "Everything" }).click();
    await expect(page.locator("#setup-banner")).toContainText("Setup is not finished.");
    await expect(page.locator("#view-title")).toHaveText("Everything");
    await expect(page.locator("#wizard")).toBeHidden();
  });

  test("a step reached before a reload opens straight away", async ({ page, hub }) => {
    await openWizardAt(page, hub, "finish");
    await expect(wizard(page).heading).toHaveText("All set");
  });
});

test.describe("a device that was never set up", () => {
  test.use({ viewport: { width: 1280, height: 800 }, freshDevice: true });

  test("is offered its part beside the list until dismissed", async ({ page, hub }) => {
    void hub;
    await openApp(page);
    const banner = page.locator("#setup-banner");
    await expect(banner).toContainText("Get reminders on this device.");
    await expect(page.locator("#wizard")).toBeHidden();
    await axe(page);

    await banner.getByRole("button", { name: "Not now" }).click();
    await expect(banner).toBeHidden();
    await page.reload();
    await expect(row(page, "Water the plants")).toBeVisible();
    await expect(banner).toBeHidden();
  });

  test("starts its part from the offer", async ({ page, hub }) => {
    void hub;
    await openApp(page);
    await page.locator("#setup-banner").getByRole("button", { name: "Set up this device" }).click();
    await expect(wizard(page).heading).toHaveText("Welcome");
    await expect(wizard(page).region).toContainText("This sets up Meat Mode on this device.");
  });
});

test.describe("phone", () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });

  test("the wizard fits the screen and keeps the tabs", async ({ page, hub }) => {
    hub.fresh();
    await page.goto("/");
    const w = wizard(page);
    await expect(w.heading).toHaveText("Welcome");
    await expect(page.locator("#tabs")).toBeVisible();
    await expect(page.locator(".wiz-steps")).toBeHidden();
    await expect(page.getByText(/^Step 1 of \d+$/)).toBeVisible();
    await noHorizontalScroll(page);
    await axe(page);
    await lightTheme(page);
    await axe(page);

    await page.locator("#tabs").getByRole("button", { name: "Today" }).click();
    await expect(page.locator("#setup-banner")).toBeVisible();
    await noHorizontalScroll(page);
    await page.locator("#setup-banner").getByRole("button", { name: "Continue setup" }).click();
    await w.next.click();
    await expect(w.heading).not.toHaveText("Welcome");
    await skipToEnd(page);
    await w.next.click();
    await expect(page.locator("#view-title")).toHaveText("Today");
    expect(hub.setup.status).toBe("done");
  });
});

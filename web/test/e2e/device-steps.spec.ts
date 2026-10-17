import { test, expect, axe, wizard, openWizardAt } from "./setup.ts";
import type { Page } from "@playwright/test";

const IPHONE_SAFARI =
  "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1";
const MAC_SAFARI =
  "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15";

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

/** Makes the page report that it runs as the installed app. */
async function asInstalledApp(page: Page) {
  await page.addInitScript(() => {
    const real = window.matchMedia.bind(window);
    window.matchMedia = (q: string) =>
      q === "(display-mode: standalone)" ? ({ ...real(q), matches: true, media: q } as MediaQueryList) : real(q);
  });
}

/** A browser without Web Push, as Safari in an iPhone tab. */
async function noPush(page: Page) {
  await page.addInitScript(() => {
    Reflect.deleteProperty(window, "PushManager");
    Reflect.deleteProperty(window, "Notification");
  });
}

/**
 * Fires Chromium's install offer, which a headless browser never makes.
 * `outcome` is what the user picks in the browser's dialog.
 */
async function offerInstall(page: Page, outcome: "accepted" | "dismissed") {
  await page.evaluate((answer) => {
    const e = new Event("beforeinstallprompt", { cancelable: true }) as Event & Record<string, unknown>;
    e.prompt = async () => {
      (window as unknown as { prompted: number }).prompted = 1;
      if (answer === "accepted") setTimeout(() => window.dispatchEvent(new Event("appinstalled")));
    };
    e.userChoice = Promise.resolve({ outcome: answer });
    window.dispatchEvent(e);
  }, outcome);
}

test.describe("desktop", () => {
  test.use({ viewport: { width: 1280, height: 800 } });

  test("Chromium's install offer becomes an Install button", async ({ page, hub }) => {
    await openWizardAt(page, hub, "install", "device");
    const w = wizard(page);
    await expect(w.heading).toHaveText("Install the app");
    await expect(w.region.getByRole("button", { name: "Install Meat Mode" })).toHaveCount(0);
    await offerInstall(page, "accepted");
    const install = w.region.getByRole("button", { name: "Install Meat Mode" });
    await expect(install).toBeVisible();
    await axe(page);
    await lightTheme(page);
    await axe(page);

    await install.click();
    await expect(w.region).toContainText("Meat Mode is installed.");
    expect(await page.evaluate(() => (window as unknown as { prompted?: number }).prompted)).toBe(1);
    await expect(install).toHaveCount(0);
  });

  test("a dismissed install says how to do it later", async ({ page, hub }) => {
    await openWizardAt(page, hub, "install", "device");
    const w = wizard(page);
    await offerInstall(page, "dismissed");
    await w.region.getByRole("button", { name: "Install Meat Mode" }).click();
    await expect(w.region.getByRole("status")).toHaveText("Not installed. You can install it later from the browser's menu.");
    await expect(w.region.getByRole("button", { name: "Install Meat Mode" })).toHaveCount(0);
  });

  test("a browser with no offer and no known steps gives no instructions", async ({ page, hub }) => {
    await openWizardAt(page, hub, "install", "device");
    const w = wizard(page);
    await expect(w.region).toContainText("Installing is optional");
    await expect(w.region.locator("[data-step=install] ol")).toHaveCount(0);
    await expect(w.region).not.toContainText("Add to");
    await expect(w.skip).toBeVisible();
  });

  test.describe("in Safari on a Mac", () => {
    test.use({ userAgent: MAC_SAFARI });

    test("says File, Add to Dock", async ({ page, hub }) => {
      await openWizardAt(page, hub, "install", "device");
      const w = wizard(page);
      await expect(w.region).toContainText("choose File, then Add to Dock");
      await axe(page);
    });
  });

  test("the installed app skips the install step", async ({ page, hub }) => {
    await asInstalledApp(page);
    await openWizardAt(page, hub, "welcome", "device");
    const w = wizard(page);
    await expect(w.region.locator(".wiz-list")).toContainText("Reminders");
    await expect(w.region.locator(".wiz-list")).not.toContainText("Install the app");
    await w.next.click();
    await expect(w.heading).toHaveText("Reminders");
  });

  test("Appearance picks dark, light or the device's setting", async ({ page, hub }) => {
    await page.emulateMedia({ colorScheme: "dark" });
    await openWizardAt(page, hub, "appearance", "device");
    const w = wizard(page);
    const html = page.locator("html");
    await expect(w.heading).toHaveText("Appearance");
    await expect(w.region.getByRole("radio", { name: /^Dark/ })).toBeChecked();
    await axe(page);

    await w.region.getByRole("radio", { name: /^Light/ }).check();
    await expect(html).toHaveAttribute("data-theme", "light");
    await expect(w.region.getByRole("radio", { name: /^Light/ })).toBeFocused();
    expect(await page.evaluate(() => localStorage.getItem("meat-mode.theme"))).toBe("light");
    await axe(page);

    await w.region.getByRole("radio", { name: /^Match this device/ }).check();
    await expect(html).toHaveAttribute("data-theme", "dark");
    await page.emulateMedia({ colorScheme: "light" });
    await expect(html).toHaveAttribute("data-theme", "light");
    await page.reload();
    await expect(w.region.getByRole("radio", { name: /^Match this device/ })).toBeChecked();
    await expect(html).toHaveAttribute("data-theme", "light");

    // Continue keeps the choice and moves on.
    await w.next.click();
    await expect(w.heading).not.toHaveText("Appearance");
    expect(await page.evaluate(() => localStorage.getItem("meat-mode.theme"))).toBe("system");
  });
});

test.describe("phone", () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });

  test("Appearance fits the screen in both themes", async ({ page, hub }) => {
    await openWizardAt(page, hub, "appearance", "device");
    const w = wizard(page);
    await noHorizontalScroll(page);
    await axe(page);
    await w.region.getByText("Light", { exact: true }).click();
    await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
    await noHorizontalScroll(page);
    await axe(page);
  });

  test.describe("on an iPhone in a browser tab", () => {
    test.use({ userAgent: IPHONE_SAFARI });

    test("shows the Home Screen steps and why reminders need them", async ({ page, hub }) => {
      await noPush(page);
      hub.login = true;
      hub.authenticated = true;
      await openWizardAt(page, hub, "install", "device");
      const w = wizard(page);
      await expect(w.region).toContainText("reminders work only in the Home Screen app");
      await expect(w.region).toContainText("tap Add to Home Screen");
      await expect(w.region).toContainText("Leave Open as Web App on");
      await expect(w.region).toContainText("log in again");
      await noHorizontalScroll(page);
      await axe(page);
      await lightTheme(page);
      await axe(page);
    });

    test("the Home Screen app skips the install step", async ({ page, hub }) => {
      await page.addInitScript(() => Object.defineProperty(navigator, "standalone", { value: true }));
      await openWizardAt(page, hub, "welcome", "device");
      await expect(wizard(page).region.locator(".wiz-list")).not.toContainText("Install the app");
    });
  });

  test.describe("on an iPad, which reports a Mac", () => {
    test.use({ userAgent: MAC_SAFARI });

    test("shows the Home Screen steps, not Add to Dock", async ({ page, hub }) => {
      await noPush(page);
      await page.addInitScript(() => Object.defineProperty(Navigator.prototype, "maxTouchPoints", { get: () => 5 }));
      await openWizardAt(page, hub, "install", "device");
      const w = wizard(page);
      await expect(w.region).toContainText("tap Add to Home Screen");
      await expect(w.region).not.toContainText("Add to Dock");
      await expect(w.region).not.toContainText("log in again");
    });
  });
});

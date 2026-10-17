import { test, expect, axe, wizard, openWizardAt } from "./setup.ts";
import type { Page } from "@playwright/test";

/**
 * Stands in for the browser's notification permission and push service, which
 * a headless browser has no account for. `permission` is what the browser
 * says before the tap, `answer` what the user picks when it asks.
 */
async function fakeBrowserPush(page: Page, permission: string, answer = "granted") {
  await page.addInitScript(
    ([perm, ans]) => {
      class FakeNotification {
        static permission = perm;
        static async requestPermission() {
          // Like a browser: only a prompt that has not been answered asks.
          if (FakeNotification.permission === "default") FakeNotification.permission = ans;
          return FakeNotification.permission;
        }
      }
      Object.defineProperty(window, "Notification", { value: FakeNotification, configurable: true, writable: true });
      Object.defineProperty(window, "PushManager", { value: class {}, configurable: true, writable: true });
      const sub = { toJSON: () => ({ endpoint: "https://push.example.com/send/abc", keys: { p256dh: "BP", auth: "AU" } }) };
      const reg = { pushManager: { getSubscription: async () => null, subscribe: async () => sub } };
      Object.defineProperty(navigator, "serviceWorker", {
        value: { register: async () => reg, ready: Promise.resolve(reg) },
        configurable: true,
      });
    },
    [permission, answer] as const,
  );
}

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
  test.use({ viewport: { width: 1280, height: 800 } });

  test("explains, asks from the tap, subscribes and sends a test reminder", async ({ page, hub }) => {
    await fakeBrowserPush(page, "default");
    hub.ntfy = true;
    await openWizardAt(page, hub, "reminders", "device");
    const w = wizard(page);
    await expect(w.heading).toHaveText("Reminders");
    await expect(w.region).toContainText("will not ask again");
    expect(hub.pushSubscriptions).toBe(0);
    await axe(page);
    await lightTheme(page);
    await axe(page);

    await w.region.getByRole("button", { name: "Turn on reminders" }).click();
    await expect(w.region.getByRole("status")).toContainText("A test reminder is on its way to this device.");
    await expect(w.region.getByRole("status")).toContainText("also sent to ntfy");
    expect(hub.pushSubscriptions).toBe(1);
    expect(hub.reminderTests).toBe(1);
    await expect(w.region.getByRole("button", { name: "Send a test reminder" })).toBeVisible();
    await axe(page);

    await w.region.getByRole("button", { name: "Send a test reminder" }).click();
    await expect.poll(() => hub.reminderTests).toBe(2);
  });

  test("a denied prompt shows how to undo it", async ({ page, hub }) => {
    await fakeBrowserPush(page, "default", "denied");
    await openWizardAt(page, hub, "reminders", "device");
    const w = wizard(page);
    await w.region.getByRole("button", { name: "Turn on reminders" }).click();
    await expect(w.region).toContainText("blocked for Meat Mode on this device");
    await expect(w.region).toContainText("site settings");
    expect(hub.reminderTests).toBe(0);
    await axe(page);

    // Allowing it in the browser's settings, then checking again, turns reminders on.
    await page.evaluate(() => {
      (Notification as unknown as { permission: string }).permission = "granted";
    });
    await w.region.getByRole("button", { name: "Check again" }).click();
    await expect(w.region.getByRole("status")).toContainText("A test reminder is on its way");
    expect(hub.pushSubscriptions).toBe(1);
  });

  test("a device with no subscription is told so", async ({ page, hub }) => {
    await fakeBrowserPush(page, "granted");
    await openWizardAt(page, hub, "reminders", "device");
    const w = wizard(page);
    await expect(w.region).toContainText("Reminders are on for this device.");
    hub.pushSubscriptions = 0; // the hub lost it, as after a log out
    await w.region.getByRole("button", { name: "Send a test reminder" }).click();
    await expect(w.region.getByRole("status")).toContainText("no reminder subscription");
  });

  test("a browser without push says so and the step can be skipped", async ({ page, hub }) => {
    await page.addInitScript(() => {
      Reflect.deleteProperty(window, "PushManager");
      Reflect.deleteProperty(window, "Notification");
    });
    await openWizardAt(page, hub, "reminders", "device");
    const w = wizard(page);
    await expect(w.region).toContainText("cannot show reminders");
    await expect(w.skip).toBeVisible();
    await axe(page);
  });
});

test.describe("phone", () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });

  test("fits the screen and walks through turning reminders on", async ({ page, hub }) => {
    await fakeBrowserPush(page, "default");
    await openWizardAt(page, hub, "reminders", "device");
    const w = wizard(page);
    await noHorizontalScroll(page);
    await axe(page);
    await w.region.getByRole("button", { name: "Turn on reminders" }).click();
    await expect(w.region.getByRole("status")).toContainText("A test reminder is on its way");
    await noHorizontalScroll(page);
    await lightTheme(page);
    await axe(page);
  });

  test.describe("on an iPhone in a browser tab", () => {
    test.use({
      userAgent:
        "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1",
    });

    test("reminders wait for the Home Screen app", async ({ page, hub }) => {
      await page.addInitScript(() => {
        Reflect.deleteProperty(window, "PushManager");
      Reflect.deleteProperty(window, "Notification");
      });
      await openWizardAt(page, hub, "reminders", "device");
      const w = wizard(page);
      await expect(w.region).toContainText("once Meat Mode is on the Home Screen");
      await expect(w.region.getByRole("button", { name: "Turn on reminders" })).toHaveCount(0);
      await noHorizontalScroll(page);
      await axe(page);
    });
  });
});

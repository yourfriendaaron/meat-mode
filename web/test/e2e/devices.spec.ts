import type { Page } from "@playwright/test";
import { test, expect, axe, openApp } from "./setup.ts";

test.use({ viewport: { width: 1280, height: 800 } });

async function openSettings(page: Page) {
  await openApp(page);
  await page.locator(".sidebar").getByRole("button", { name: "Settings" }).click();
}

const at = (h: number, m = 0) => new Date(Date.UTC(2026, 9, 14, h, m)).toISOString();

/**
 * Stands in for the Android app's message bridge (docs/api.md, Android app).
 * `states` is what push-status answers on each ask, the last one repeating.
 * `silent` makes the bridge never answer, as a page that only has the object.
 */
async function fakeApp(page: Page, opts: { states: string[]; silent?: boolean }) {
  await page.addInitScript((o) => {
    const sent: any[] = [];
    let asked = 0;
    const meatMode = {
      onmessage: null as null | ((e: { data: string }) => void),
      postMessage(raw: string) {
        const m = JSON.parse(raw);
        sent.push(m);
        if (o.silent) return;
        const reply = (body: object) => setTimeout(() => meatMode.onmessage?.({ data: JSON.stringify({ id: m.id, ...body }) }), 0);
        if (m.type === "hello") reply({ app: "android", version: "1.0.0", bridge: 1 });
        else if (m.type === "push-status") {
          const state = o.states[Math.min(asked++, o.states.length - 1)];
          reply({ state, ...(state === "on" || state === "waiting" ? { endpoint: "https://up.example.com/abc" } : {}), lastPush: null, batteryExempt: true });
        } else if (m.type === "push-on") {
          reply({ subscription: { endpoint: "https://up.example.com/abc", keys: { p256dh: "BP", auth: "AU" } }, name: "Test phone" });
        } else reply({ error: "unknown request" });
      },
    };
    (window as any).meatMode = meatMode;
    (window as any).__sent = sent;
  }, opts);
}

const sentTypes = (page: Page) => page.evaluate(() => (window as any).__sent.map((m: any) => m.type));

test.describe("Devices", () => {
  test("lists devices with their kind, last push and last seen, and revokes one after asking", async ({ page, hub }) => {
    hub.devices = [
      { id: "1".repeat(32), name: "Test phone", kind: "app", confirmed: true, createdAt: at(8), lastSeen: at(9, 30), lastPush: at(9) },
      { id: "2".repeat(32), name: "Firefox on Linux", kind: "web", confirmed: true, createdAt: at(7), lastSeen: at(9), lastPush: null },
      { id: "3".repeat(32), name: "New phone", kind: "app", confirmed: false, createdAt: at(9, 55), lastSeen: at(9, 55), lastPush: null },
    ];
    await openSettings(page);
    const section = page.locator("[data-section=devices]");
    await expect(section.locator("h2")).toHaveText("Devices");
    const rows = section.locator("li.machine");
    await expect(rows).toHaveCount(3);
    await expect(rows.nth(0)).toContainText("Test phone");
    await expect(rows.nth(0)).toContainText("Android app · Last push 1h ago · Last seen 30m ago");
    await expect(rows.nth(1)).toContainText("Browser · No push yet · Last seen 1h ago");
    await expect(rows.nth(2)).toContainText("Waiting for confirmation");
    await axe(page);

    await rows.nth(1).getByRole("button", { name: "Revoke Firefox on Linux" }).click();
    await expect(rows.nth(1)).toContainText("until it opens Meat Mode again with notifications allowed");
    await rows.nth(1).getByRole("button", { name: "Cancel" }).click();
    await expect(rows).toHaveCount(3);
    expect(hub.requests).not.toContain("DELETE /api/devices/" + "2".repeat(32));

    await rows.nth(0).getByRole("button", { name: "Revoke Test phone" }).click();
    await expect(rows.nth(0)).toContainText("shows reminders as off");
    await axe(page);
    await rows.nth(0).getByRole("button", { name: "Revoke", exact: true }).click();
    await expect(rows).toHaveCount(2);
    expect(hub.devices.map((d) => d.name)).toEqual(["Firefox on Linux", "New phone"]);
    await expect(page.locator("#toast")).toContainText("Revoked");
  });

  test("says so when there are none", async ({ page, hub: _hub }) => {
    await openSettings(page);
    await expect(page.locator("[data-section=devices]")).toContainText("No device gets reminders yet");
  });
});

test.describe("reminders", () => {
  test("a browser registers as a browser named after itself", async ({ page, hub }) => {
    await page.addInitScript(() => {
      class FakeNotification {
        static permission = "default";
        static async requestPermission() {
          return (FakeNotification.permission = "granted");
        }
      }
      Object.defineProperty(window, "Notification", { value: FakeNotification, configurable: true, writable: true });
      Object.defineProperty(window, "PushManager", { value: class {}, configurable: true, writable: true });
      const sub = { toJSON: () => ({ endpoint: "https://push.example.com/send/1", keys: { p256dh: "BP", auth: "AU" } }), unsubscribe: async () => true };
      const reg = { pushManager: { getSubscription: async () => null, subscribe: async () => sub } };
      Object.defineProperty(navigator, "serviceWorker", { value: { register: async () => reg, ready: Promise.resolve(reg) }, configurable: true });
    });
    await page.addInitScript(() => Object.defineProperty(navigator, "userAgent", { value: "Mozilla/5.0 (X11; Linux x86_64; rv:130.0) Gecko/20100101 Firefox/130.0" }));
    await openSettings(page);
    await page.getByRole("button", { name: "Turn on reminders" }).click();
    await expect(page.getByRole("status").filter({ hasText: "Reminders are on." })).toBeVisible();
    expect(hub.pushPosts).toEqual([{ endpoint: "https://push.example.com/send/1", keys: { p256dh: "BP", auth: "AU" }, kind: "web", name: "Firefox on Linux" }]);
    await expect(page.locator("[data-section=devices]")).toContainText("Firefox on Linux");
  });

  test("in the app, reminders go through the bridge, register as an app and wait for the hub's confirmation", async ({ page, hub }) => {
    await fakeApp(page, { states: ["off", "waiting", "on"] });
    await openSettings(page);
    await page.getByRole("button", { name: "Turn on reminders" }).click();
    await expect(page.getByRole("status").filter({ hasText: "Reminders are on." })).toBeVisible({ timeout: 10_000 });
    expect(hub.pushPosts).toEqual([{ endpoint: "https://up.example.com/abc", keys: { p256dh: "BP", auth: "AU" }, kind: "app", name: "Test phone" }]);
    const msgs = await page.evaluate(() => (window as any).__sent);
    expect(msgs.find((m: any) => m.type === "push-on").vapidKey).toBe("BExampleKeyOnlyForTests");
    await expect(page.getByText("Reminders are on for this phone.")).toBeVisible();
    await expect(page.locator("[data-section=devices]")).toContainText("Test phone");
    await axe(page);
  });

  test("in the app, blocked notifications are explained and nothing is registered", async ({ page, hub }) => {
    await fakeApp(page, { states: ["blocked"] });
    await openSettings(page);
    await expect(page.locator("[data-section]").first()).toBeVisible();
    await expect(page.getByText("Notifications are blocked for Meat Mode. Allow them in the phone's Android settings")).toBeVisible();
    await page.getByRole("button", { name: "Try again" }).click();
    await expect(page.getByRole("status").filter({ hasText: "Android settings" })).toBeVisible();
    expect(hub.pushPosts).toEqual([]);
    expect(await sentTypes(page)).not.toContain("push-on");
  });

  test("in the app, no push service is explained", async ({ page, hub }) => {
    await fakeApp(page, { states: ["unavailable"] });
    await openSettings(page);
    await expect(page.getByText(/no way to receive reminders yet/)).toBeVisible();
    expect(hub.pushPosts).toEqual([]);
  });

  test("a page whose bridge never answers hello is treated as a browser", async ({ page, hub: _hub }) => {
    await fakeApp(page, { states: ["on"], silent: true });
    await openSettings(page);
    await expect(page.locator("[data-section=devices]")).toBeVisible();
    await expect(page.getByText("Reminders are on for this phone.")).toHaveCount(0);
    expect(await sentTypes(page)).toEqual(["hello"]);
  });
});

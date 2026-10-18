// Screenshot baselines for the setup steps and the main views, at phone and
// desktop width in both themes, so a layout regression fails CI. Run it with
// `pnpm test:visual` (and `pnpm test:visual:update` after an intended change):
// it runs in the Playwright container so the fonts match CI's.
//
// The data is the fake hub's fixed fixtures and the clock is fixed, so
// nothing on screen changes between runs.

import { test, expect, openApp, wizard, openWizardAt, row } from "../e2e/setup.ts";
import type { Page } from "@playwright/test";

const IPHONE_SAFARI =
  "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1";

const SIZES = {
  phone: { viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true },
  desktop: { viewport: { width: 1280, height: 800 } },
} as const;
type Size = keyof typeof SIZES;

/** A browser that can ask for notifications and has not been asked yet. */
async function pushNotAsked(page: Page) {
  await page.addInitScript(() => {
    const reg = { pushManager: { getSubscription: async () => null, subscribe: async () => null } };
    Object.defineProperty(window, "Notification", { value: { permission: "default", requestPermission: async () => "default" }, configurable: true });
    Object.defineProperty(window, "PushManager", { value: class {}, configurable: true });
    Object.defineProperty(navigator, "serviceWorker", { value: { register: async () => reg, ready: Promise.resolve(reg) }, configurable: true });
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
 * Takes the baseline of the whole page. The app scrolls inside its panes, not
 * the document, so the viewport grows by the tallest pane's overflow first,
 * and the shot shows everything a user would scroll to.
 */
async function snap(page: Page, name: string) {
  await page.mouse.move(0, 0);
  const vp = page.viewportSize()!;
  const extra = await page.evaluate(() => {
    let most = 0;
    for (const el of document.querySelectorAll<HTMLElement>("*")) {
      const y = getComputedStyle(el).overflowY;
      if ((y === "auto" || y === "scroll") && el.clientHeight > 200) most = Math.max(most, el.scrollHeight - el.clientHeight);
    }
    return most;
  });
  if (extra > 0) await page.setViewportSize({ width: vp.width, height: vp.height + extra });
  await expect(page).toHaveScreenshot(`${name}.png`, { fullPage: true });
  if (extra > 0) await page.setViewportSize(vp);
}

async function goToSettings(page: Page, size: Size) {
  if (size === "phone") {
    await page.locator("#tabs").getByRole("button", { name: "Browse" }).click();
    await page.locator("#browse").getByRole("button", { name: "Settings" }).click();
  } else {
    await page.locator(".sidebar").getByRole("button", { name: "Settings" }).click();
  }
  await expect(page.locator("#view-title")).toHaveText("Settings");
}

for (const size of Object.keys(SIZES) as Size[]) {
  for (const theme of ["dark", "light"] as const) {
    test.describe(`${size} ${theme}`, () => {
      test.use(SIZES[size]);
      const shot = (page: Page, surface: string) => snap(page, `${surface}-${size}-${theme}`);

      test.beforeEach(async ({ page }) => {
        await page.emulateMedia({ colorScheme: theme, reducedMotion: "reduce" });
        await page.addInitScript((t) => localStorage.setItem("meat-mode.theme", t), theme);
      });

      test.describe("setup", () => {
        test("welcome", async ({ page, hub }) => {
          hub.fresh();
          await page.goto("/");
          await expect(wizard(page).heading).toHaveText("Welcome");
          await shot(page, "setup-welcome");
        });

        test("connection check with problems", async ({ page, hub }) => {
          hub.liveEvents = true;
          hub.check.hostMatches = false;
          hub.check.publicUrl = "https://meat-mode.example.com";
          hub.check.time = "2026-10-14T10:09:00Z";
          await openWizardAt(page, hub, "connection");
          await expect(page.locator("#wizard .conn-state").first()).toHaveText("Problem");
          await expect(page.locator("#wizard .conn-state").nth(2)).toHaveText("Passed");
          await shot(page, "setup-connection");
        });

        test("secure the hub", async ({ page, hub }) => {
          await openWizardAt(page, hub, "secure");
          await shot(page, "setup-secure");
        });

        test("name and time zone", async ({ page, hub }) => {
          await openWizardAt(page, hub, "profile");
          await shot(page, "setup-profile");
        });

        test.describe("install", () => {
          // A phone gets the iPhone steps; a desktop gets the plain browser.
          if (size === "phone") test.use({ userAgent: IPHONE_SAFARI });
          test("install the app", async ({ page, hub }) => {
            if (size === "phone") await noPush(page);
            await openWizardAt(page, hub, "install", "device");
            await shot(page, "setup-install");
          });
        });

        test("reminders", async ({ page, hub }) => {
          await pushNotAsked(page);
          await openWizardAt(page, hub, "reminders", "device");
          await expect(wizard(page).region.getByRole("button", { name: "Turn on reminders" })).toBeVisible();
          await shot(page, "setup-reminders");
        });

        test("appearance", async ({ page, hub }) => {
          await openWizardAt(page, hub, "appearance", "device");
          await shot(page, "setup-appearance");
        });

        test("first to-do", async ({ page, hub }) => {
          await openWizardAt(page, hub, "first-todo");
          await page.getByRole("button", { name: "Use the example" }).click();
          await shot(page, "setup-first-todo");
        });

        test("agent machines, pairing and repos", async ({ page, hub }) => {
          await openWizardAt(page, hub, "agents");
          const w = wizard(page);
          await w.region.getByRole("button", { name: "Yes, add a machine" }).click();
          await page.getByLabel("Machine name").fill("laptop");
          await w.region.getByRole("button", { name: "Add machine" }).click();
          await expect(w.region.getByRole("status")).toHaveText("Waiting for laptop to connect…");
          await shot(page, "setup-agents-pairing");
          hub.hello("laptop", ["projects/garden", "work/notes"]);
          await expect(w.region.getByRole("status")).toHaveText("Connected. laptop is talking to this hub.");
          await w.next.click();
          await expect(w.heading).toHaveText("Choose repos");
          await shot(page, "setup-agent-repos");
        });

        test("all set", async ({ page, hub }) => {
          await openWizardAt(page, hub, "finish");
          await shot(page, "setup-finish");
        });
      });

      test.describe("views", () => {
        test("today", async ({ page, hub }) => {
          void hub;
          await openApp(page);
          await shot(page, "today");
        });

        test("agent task detail", async ({ page, hub }) => {
          void hub;
          await openApp(page, "/?item=" + encodeURIComponent("home/website/we-1"));
          await expect(page.locator("#detail h2#d-title")).toHaveText("Approve the new footer text");
          await shot(page, "detail");
        });

        test("personal task detail with a checklist", async ({ page, hub }) => {
          const it = hub.find("hub/personal/pe-1")!;
          it.priority = 1;
          it.checklist = [
            { id: "k1", text: "Fill the watering can", done: true },
            { id: "k2", text: "Move the fern out of the sun", done: false },
          ];
          it.comments = [{ id: "n1", author: "alex-app", text: "Use the rain water.", createdAt: "2026-10-14T08:00:00Z" }];
          await openApp(page, "/?item=" + encodeURIComponent("hub/personal/pe-1"));
          await expect(page.locator("#detail h2#d-title")).toHaveText(it.title);
          await shot(page, "personal-detail");
        });

        test("edit a personal task", async ({ page, hub }) => {
          const it = hub.find("hub/personal/pe-1")!;
          await openApp(page, "/?item=" + encodeURIComponent("hub/personal/pe-1"));
          await expect(page.locator("#detail h2#d-title")).toHaveText(it.title);
          await page.locator("#detail").getByRole("button", { name: "Edit", exact: true }).click();
          await expect(page.locator("#detail").getByLabel("Title")).toBeFocused();
          await shot(page, "edit");
        });

        test("snooze", async ({ page, hub }) => {
          void hub;
          await openApp(page);
          await expect(row(page, "Renew the library card")).toBeVisible();
          await page.keyboard.press("j");
          await page.keyboard.press("s");
          await expect(page.getByRole("dialog", { name: "Snooze" })).toBeVisible();
          await shot(page, "snooze");
        });

        test("new task", async ({ page, hub }) => {
          void hub;
          await openApp(page);
          await page.getByRole("button", { name: "New task" }).first().click();
          await page.locator("#qa-form input").first().fill("Water the plants tomorrow at 9am");
          await expect(page.locator("#qa-form")).toContainText("Due Tomorrow 9:00");
          await shot(page, "new-task");
        });

        test("settings", async ({ page, hub }) => {
          hub.login = true;
          hub.authenticated = true;
          hub.setup.password = "app";
          hub.setup.userName = "Alex";
          hub.setup.timeZone = "Europe/Paris";
          await openApp(page);
          await goToSettings(page, size);
          await expect(page.getByRole("button", { name: "Log out" })).toBeVisible();
          await shot(page, "settings");
        });

        test("empty list with the Continue setup banner", async ({ page, hub }) => {
          hub.fresh();
          await page.goto("/");
          await expect(wizard(page).heading).toHaveText("Welcome");
          const nav = size === "phone" ? page.locator("#tabs") : page.locator(".sidebar");
          await nav.getByRole("button", { name: /Today/ }).click();
          await expect(page.locator("#setup-banner")).toContainText("Setup is not finished.");
          await shot(page, "empty-banner");
        });

        test("login", async ({ page, hub }) => {
          hub.login = true;
          await page.goto("/");
          await expect(page.getByRole("button", { name: "Log in" })).toBeVisible();
          await shot(page, "login");
        });
      });
    });
  }
}

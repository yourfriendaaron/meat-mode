// Screenshot baselines for the setup steps and the main views, at phone and
// desktop width in both themes, so a layout regression fails CI. Run it with
// `pnpm test:visual` (and `pnpm test:visual:update` after an intended change):
// it runs in the Playwright container so the fonts match CI's.
//
// The data is the fake hub's fixed fixtures and the clock is fixed, so
// nothing on screen changes between runs.

import { test, expect, openApp, wizard, openWizardAt, row } from "../e2e/setup.ts";
import type { Page } from "@playwright/test";
import type { RunState } from "../../src/types.ts";
import type { FakeHub } from "../e2e/fake-hub.ts";

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

/** The From agents view, where every agent item shows. */
async function goToAgents(page: Page, size: Size) {
  const nav = size === "phone" ? page.locator("#tabs") : page.locator("#nav");
  await nav.getByRole("button", { name: /agents/i }).click();
  await expect(page.locator("#view-title")).toHaveText("From agents");
}

/** One agent item in each run state, with agents allowed in both repos. */
function everyRunState(hub: FakeHub) {
  hub.allowAgents("website");
  hub.allowAgents("garden");
  hub.setRun("home/website/we-1", { state: "working", activity: "Shortening the second line of the footer" });
  hub.setRun("home/garden/ga-2", { state: "waiting", activity: "Asked which label size to print" });
  hub.setRun("home/garden/ga-1", { state: "stopped", reason: "usage-limit", detail: "The limit resets at 14:00." });
  hub.items.push({
    ...structuredClone(hub.find("home/garden/ga-2")!),
    key: "home/garden/ga-3",
    id: "ga-3",
    title: "Pick a mulch for the paths",
    status: "closed",
    closedAt: "2026-10-14T09:00:00Z",
    closeReason: "Done",
    run: { id: "run-done", state: "done", agent: "planner", activity: "Ordered bark mulch and closed the task", startedAt: "2026-10-14T08:40:00Z", endedAt: "2026-10-14T09:00:00Z", count: 1 },
  });
}

/** What the sheet's run card shows in each state. */
const SHEET_RUNS: Record<RunState, Parameters<FakeHub["setRun"]>[1]> = {
  working: { state: "working", activity: "Shortening the second line", queued: 1 },
  waiting: { state: "waiting", activity: "Asked which wording to use" },
  done: { state: "done", activity: "Shortened the line and closed the task" },
  stopped: { state: "stopped", reason: "timeout", detail: "Runs on this machine stop after 30 minutes." },
};

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

        test("sub-tasks in the list and the detail", async ({ page, hub }) => {
          const parent = hub.find("hub/personal/pe-3")!;
          const sub = (id: string, title: string, over: Partial<typeof parent> = {}) => ({
            ...structuredClone(parent),
            key: `hub/personal/${id}`,
            id,
            title,
            parent: "pe-3",
            dueAt: null,
            ...over,
          });
          hub.items.push(
            sub("pe-31", "Book the train", { dueAt: "2026-10-14T11:00:00Z" }),
            sub("pe-32", "Pack a bag"),
            sub("pe-33", "Check the timetable", { status: "closed", closedAt: "2026-10-14T08:00:00Z", closeReason: "Done in Meat Mode" }),
          );
          await openApp(page, "/?item=" + encodeURIComponent("hub/personal/pe-3"));
          await expect(page.locator("#detail h2#d-title")).toHaveText(parent.title);
          await shot(page, "subtasks");
        });

        test("edit a personal task", async ({ page, hub }) => {
          const it = hub.find("hub/personal/pe-1")!;
          it.repeat = { every: "week", weekdays: [1, 4], timeZone: "UTC", start: it.dueAt! };
          await openApp(page, "/?item=" + encodeURIComponent("hub/personal/pe-1"));
          await expect(page.locator("#detail h2#d-title")).toHaveText(it.title);
          await page.locator("#detail").getByRole("button", { name: "Edit", exact: true }).click();
          await expect(page.locator("#detail").getByLabel("Title")).toBeFocused();
          // The lead time choice names the hub's default once it has loaded.
          await expect(page.locator("#edit-lead option:checked")).toHaveText("Use default (none)");
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
          hub.setup.password = "app";
          hub.setup.userName = "Alex";
          hub.setup.timeZone = "Europe/Paris";
          await openApp(page);
          await goToSettings(page, size);
          await expect(page.getByRole("button", { name: "Log out" })).toBeVisible();
          // Sections that load their own data have drawn.
          await expect(page.getByLabel("Remind me early")).toBeVisible();
          await expect(page.getByRole("button", { name: "Get calendar link" })).toBeVisible();
          await shot(page, "settings");
        });

        test("agent runs on list rows", async ({ page, hub }) => {
          everyRunState(hub);
          await openApp(page);
          await goToAgents(page, size);
          await expect(page.locator("#list .row[data-run]")).toHaveCount(4);
          await shot(page, "runs-rows");
        });

        for (const state of Object.keys(SHEET_RUNS) as RunState[]) {
          test(`agent run ${state} in the sheet`, async ({ page, hub }) => {
            hub.allowAgents("website");
            const it = hub.find("home/website/we-1")!;
            if (state === "working") it.comments!.push({ id: "c4", author: "alex-app", text: "Keep the copyright year.", createdAt: "2026-10-14T09:30:00Z" });
            hub.setRun(it.key, SHEET_RUNS[state]);
            await openApp(page, "/?item=" + encodeURIComponent(it.key));
            await expect(page.locator("#detail .run-card")).toHaveAttribute("data-run", state);
            await shot(page, `runs-sheet-${state}`);
          });
        }

        test("undo bar with Tell", async ({ page, hub }) => {
          hub.allowAgents("garden");
          await openApp(page);
          await row(page, "Order compost for the beds").getByRole("button", { name: "Mark done: Order compost for the beds" }).click();
          await expect(page.locator("#toast").getByRole("button", { name: "Tell planner" })).toBeVisible();
          await shot(page, "runs-undo-tell");
        });

        test("new task with the agent switch", async ({ page, hub }) => {
          hub.allowAgents("garden");
          await openApp(page);
          await page.getByRole("button", { name: "New task" }).first().click();
          await page.locator("#qa-form input").first().fill("Find a cheaper soil sensor");
          await page.locator("#qa-list").selectOption("home/garden");
          await page.getByRole("switch", { name: "An agent starts on it" }).click();
          await expect(page.locator("#qa-add")).toHaveClass(/btn-agent/);
          await shot(page, "runs-new-task");
        });

        test("settings with the agents switch", async ({ page, hub }) => {
          hub.allowAgents("website");
          hub.setRun("home/website/we-1", { state: "working", activity: "Shortening the second line" });
          await openApp(page);
          await goToSettings(page, size);
          await expect(page.locator("[data-section=runs]")).toContainText("1 agent working");
          await expect(page.getByRole("button", { name: "Get calendar link" })).toBeVisible();
          await shot(page, "runs-settings");
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
          hub.authenticated = false;
          await page.goto("/");
          await expect(page.getByRole("button", { name: "Log in" })).toBeVisible();
          await shot(page, "login");
        });
      });
    });
  }
}

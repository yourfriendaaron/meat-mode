import { test, expect, axe, openApp, openMenu } from "./setup.ts";
import { gardenEpics, type FakeHub } from "./fake-hub.ts";
import type { Page } from "@playwright/test";

/** The garden repo with its epics on and sent; website's tracker can show them, switched off. */
function withEpics(hub: FakeHub) {
  hub.showEpics("garden", gardenEpics());
  hub.repos.find((r) => r.name === "website")!.epics = true;
}

function card(page: Page, id: string) {
  return page.locator("#epics .ep-cards > .ep-card", { has: page.locator(".ep-head .ep-id", { hasText: new RegExp(`^${id.replace(/\./g, "\\.")}$`) }) });
}

async function noHorizontalScroll(page: Page) {
  const [scroll, inner] = await page.evaluate(() => [document.documentElement.scrollWidth, window.innerWidth]);
  expect(scroll).toBeLessThanOrEqual(inner);
}

test.describe("desktop", () => {
  test.use({ viewport: { width: 1280, height: 800 }, permissions: ["clipboard-read", "clipboard-write"] });

  test("Epics shows in the sidebar only while a repo has its epics on", async ({ page, hub }) => {
    await openApp(page);
    const nav = page.locator("#nav");
    await expect(nav.getByRole("button", { name: "Everything" })).toBeVisible();
    await expect(nav.getByRole("button", { name: "Epics" })).toHaveCount(0);
    expect(hub.requests).not.toContain("GET /api/epics");

    hub.showEpics("garden", gardenEpics());
    await page.evaluate(() => document.dispatchEvent(new Event("visibilitychange")));
    await expect(nav.getByRole("button", { name: "Epics" })).toBeVisible();
  });

  test("a card shows its progress, expands to its children, and opens its Details read-only", async ({ page, hub }) => {
    withEpics(hub);
    await openApp(page);
    await page.locator("#nav").getByRole("button", { name: "Epics" }).click();
    await expect(page.locator("#view-title")).toHaveText("Epics");
    await expect(page.locator("#search-form")).toBeHidden();
    await expect(page.locator("#qa-form")).toBeHidden();
    await expect(page.locator("#select-btn")).toBeHidden();

    const repo = page.locator("#epics .ep-repo");
    await expect(repo.locator(".ep-repo-head")).toContainText("garden");
    await expect(repo.locator(".ep-repo-head")).toContainText("Synced 5m ago");
    // In progress before open; then priority.
    await expect(page.locator("#epics .ep-cards > .ep-card > .ep-head .ep-id")).toHaveText(["ga-10", "ga-20", "ga-20.1"]);

    const planting = card(page, "ga-10");
    await expect(planting.locator(".ep-status")).toHaveText("In progress");
    await expect(planting.locator(".ep-pri")).toHaveText("High");
    await expect(planting.locator(".ep-ptext")).toHaveText("85% 6/7 done");
    await expect(planting.locator(".ep-rows")).toHaveCount(0);

    const toggle = planting.getByRole("button", { name: "Autumn planting" });
    await expect(toggle).toHaveAttribute("aria-expanded", "false");
    await toggle.click();
    await expect(toggle).toHaveAttribute("aria-expanded", "true");
    // Open children before closed ones.
    const kids = planting.locator(".ep-row");
    await expect(kids).toHaveCount(7);
    await expect(kids.first()).toContainText("Plant the tulip bulbs");
    await expect(kids.first().locator(".ep-who")).toHaveText("sam");
    await expect(kids.first().locator(".ep-pri")).toHaveText("Medium");
    await expect(kids.nth(1)).toHaveClass(/is-done/);

    // A nested card is a plain row of its parent, and a card of its own.
    const beds = card(page, "ga-20");
    await beds.getByRole("button", { name: "Beds and paths" }).click();
    await expect(beds.locator(".ep-row .ep-id")).toHaveText(["ga-20.2", "ga-1", "ga-20.1"]);
    await expect(card(page, "ga-20.1").locator(".ep-ptext")).toHaveText("100% 1/1 done");

    // Other tasks follow the cards, by priority.
    await expect(page.locator("#epics .ep-tasks .ep-row-title")).toHaveText(["Label the shed shelves", "Sharpen the shears"]);
    await axe(page);

    await planting.getByRole("button", { name: "Details of ga-10" }).click();
    const sheet = page.locator("#detail");
    await expect(sheet.locator(".d-title")).toHaveText("Autumn planting");
    await expect(sheet.locator(".ep-status")).toHaveText("In progress");
    await expect(sheet.locator(".ep-pri")).toHaveText("High");
    await expect(sheet.locator(".md strong")).toHaveText("Bulbs");
    // Read-only: no field, no check box, no action bar.
    await expect(sheet.locator("input, textarea, .check, [data-act=done]")).toHaveCount(0);
    await axe(page);
    await page.keyboard.press("Escape");
    await expect(sheet.locator(".d-title")).toHaveCount(0);
    await expect(planting.getByRole("button", { name: "Details of ga-10" })).toBeFocused();

    // No epic row reached the person's lists.
    await page.locator("#nav").getByRole("button", { name: "Everything" }).click();
    await expect(page.locator("#list")).not.toContainText("Autumn planting");
    await expect(page.locator("#list")).not.toContainText("Sharpen the shears");
  });

  test("Copy id copies the bead's id and says so", async ({ page, hub }) => {
    withEpics(hub);
    await openApp(page);
    await page.locator("#nav").getByRole("button", { name: "Epics" }).click();
    await card(page, "ga-10").getByRole("button", { name: "Copy id ga-10" }).first().click();
    await expect(page.locator("#toast")).toContainText("Copied ga-10");
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe("ga-10");
    await page.getByRole("button", { name: "Copy id ga-31" }).click();
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe("ga-31");
  });

  test("a human row shows its task's title and opens the task's sheet", async ({ page, hub }) => {
    withEpics(hub);
    await openApp(page);
    await page.locator("#nav").getByRole("button", { name: "Epics" }).click();
    const beds = card(page, "ga-20");
    await beds.getByRole("button", { name: "Beds and paths" }).click();
    // The State item's title wins over the row's.
    const human = beds.locator(".ep-row", { has: page.locator(".ep-id", { hasText: /^ga-1$/ }) });
    await expect(human.locator(".ep-row-title")).toHaveText("Order compost for the beds");
    await human.getByRole("button", { name: /Order compost for the beds/ }).click();
    const sheet = page.locator("#detail");
    await expect(sheet.locator(".d-title")).toHaveText("Order compost for the beds");
    await expect(sheet.locator("[data-act=done]").first()).toBeVisible();
    // The sheet's own Copy id copies the id itself.
    await sheet.locator("#d-more").click();
    await sheet.getByRole("button", { name: /Copy id/ }).click();
    await expect(page.locator("#toast")).toContainText("Copied ga-1");
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe("ga-1");
    // Other rows are read-only.
    await expect(beds.locator(".ep-row", { hasText: "Fence post leans" }).locator("[data-act=open]")).toHaveCount(0);
  });

  test("an epics event loads the epics again, and never State", async ({ page, hub }) => {
    withEpics(hub);
    hub.epicsEventMs = 200;
    await openApp(page);
    await page.waitForTimeout(500);
    // Not on Epics: the events load nothing.
    expect(hub.requests).not.toContain("GET /api/epics");
    await page.locator("#nav").getByRole("button", { name: "Epics" }).click();
    await expect(page.locator("#epics .ep-tasks .ep-row")).toHaveCount(2);
    const states = hub.requests.filter((r) => r === "GET /api/state").length;
    hub.epicRows.get("home/garden")!.push({ id: "ga-32", title: "Oil the hinges", status: "open", priority: 2, updatedAt: hub.now() });
    await expect(page.locator("#epics .ep-tasks .ep-row")).toHaveCount(3);
    expect(hub.requests.filter((r) => r === "GET /api/state").length).toBe(states);
  });

  test("a repo with nothing open says so; one that sent no set is left out", async ({ page, hub }) => {
    hub.showEpics("garden", []);
    hub.showEpics("website");
    await openApp(page);
    await page.locator("#nav").getByRole("button", { name: "Epics" }).click();
    await expect(page.locator("#epics .ep-repo")).toHaveCount(1);
    await expect(page.locator("#epics .ep-repo")).toContainText("Nothing open.");
    await axe(page);
  });

  test("a failed load says so, and the view recovers when the hub answers again", async ({ page, hub }) => {
    withEpics(hub);
    hub.failEpics = true;
    await openApp(page);
    await page.locator("#nav").getByRole("button", { name: "Epics" }).click();
    await expect(page.locator("#epics")).toContainText("Could not load epics");
    await expect(page.locator("#epics .ep-card")).toHaveCount(0);
    hub.failEpics = false;
    // Coming back to the view loads it again.
    await page.locator("#nav").getByRole("button", { name: "Everything" }).click();
    await page.locator("#nav").getByRole("button", { name: "Epics" }).click();
    await expect(card(page, "ga-10").locator(".ep-ptext")).toHaveText("85% 6/7 done");
  });

  test("the last repo's epics switched off at its machine takes the view back to Today", async ({ page, hub }) => {
    withEpics(hub);
    hub.eventRetryMs = 200; // the hub's change events reach the app
    await openApp(page);
    await page.locator("#nav").getByRole("button", { name: "Epics" }).click();
    await expect(page.locator("#view-title")).toHaveText("Epics");
    // The machine reports the repo without epics on, and the hub tells the app.
    hub.repos.find((r) => r.epicsOn)!.epicsOn = false;
    await expect(page.locator("#view-title")).toHaveText("Today");
    await expect(page.locator("#nav").getByRole("button", { name: "Epics" })).toHaveCount(0);
  });

  test("Settings switches a repo's epics off, and shows the command that switches them on", async ({ page, hub }) => {
    withEpics(hub);
    await openApp(page);
    await page.locator(".sidebar").getByRole("button", { name: "Settings" }).click();
    const sw = page.getByRole("switch", { name: "Epics of garden (garden) show in the app" });
    await expect(sw).toHaveAttribute("aria-checked", "true");
    const website = page.locator(".m-repo", { has: page.getByRole("switch", { name: "Sync website (website)" }) });
    await expect(website).toContainText("Epics are off here");
    await expect(website.locator("code", { hasText: "epics" })).toHaveText("meat-mode bridge epics on website");
    await axe(page);

    await sw.click();
    expect(hub.posted.find((a) => a.kind === "epics")).toMatchObject({ bridge: "home", repo: "garden", payload: { on: false } });
    const garden = page.locator(".m-repo", { has: page.getByRole("switch", { name: "Sync garden (garden)" }) });
    await expect(garden).toContainText("Epics are off here");
    await expect(garden).toContainText("Waiting for home");
    // No repo shows epics now, so the view leaves the sidebar.
    await expect(page.locator("#nav").getByRole("button", { name: "Epics" })).toHaveCount(0);
    await page.locator(".sidebar .sync-pill").click();
    await expect(page.locator("#syncpop")).toContainText("Epics off: garden on home");
  });
});

test.describe("phone", () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });

  test("Epics is a tab; cards fit the screen and Details opens as a sheet", async ({ page, hub }) => {
    withEpics(hub);
    await openApp(page);
    const tabs = page.locator("#tabs");
    await tabs.getByRole("button", { name: "Epics" }).click();
    await expect(page.locator("#view-title")).toHaveText("Epics");
    await expect(page.locator(".fab")).toBeVisible();
    const planting = card(page, "ga-10");
    await expect(planting.locator(".ep-ptext")).toHaveText("85% 6/7 done");
    await planting.getByRole("button", { name: "Autumn planting" }).click();
    await expect(planting.locator(".ep-row")).toHaveCount(7);
    await noHorizontalScroll(page);
    await axe(page);

    await planting.getByRole("button", { name: "Details of ga-10" }).click();
    const sheet = page.locator("#detail");
    await expect(sheet.locator(".d-title")).toHaveText("Autumn planting");
    await expect(page.locator("#scrim")).toBeVisible();
    await axe(page);
    await sheet.getByRole("button", { name: "Close details" }).click();
    await expect(page.locator("#app")).not.toHaveClass(/detail-open/);

    // The menu lists it too.
    const drawer = await openMenu(page);
    await expect(drawer.getByRole("button", { name: "Epics" })).toBeVisible();
  });

  test("dark and light themes pass the contrast check", async ({ page, hub }) => {
    withEpics(hub);
    await openApp(page);
    await page.locator("#tabs").getByRole("button", { name: "Epics" }).click();
    await card(page, "ga-20").getByRole("button", { name: "Beds and paths" }).click();
    await axe(page);
    await page.evaluate(() => {
      document.documentElement.dataset.theme = "light";
    });
    await axe(page);
  });
});

import { test, expect, axe, openApp, openMenu, settle } from "./setup.ts";
import { gardenEpics, LEAF_SVG, type FakeHub } from "./fake-hub.ts";
import type { EpicRow } from "../../src/types.ts";
import type { Page } from "@playwright/test";

/** The garden repo with its epics on and sent; website's tracker can show them, switched off. */
function withEpics(hub: FakeHub) {
  hub.showEpics("garden", gardenEpics());
  hub.repos.find((r) => r.name === "website")!.epics = true;
}

function wr(id: string, title: string, over: Partial<EpicRow> = {}): EpicRow {
  return { id, title, status: "open", priority: 2, updatedAt: hub0, ...over };
}
const hub0 = "2026-10-14T08:00:00.000Z";

/** The website repo's epics: one card and enough Critical and Blocked tasks to fill Needs attention. */
function websiteEpics(): EpicRow[] {
  return [
    wr("wb-1", "New gallery", { epic: true, type: "epic", priority: 1 }),
    wr("wb-1.1", "Resize the photos", { parent: "wb-1", priority: 0 }),
    wr("wb-2", "Renew the domain", { priority: 0, assignee: "alex" }),
    wr("wb-3", "Fix the contact form", { type: "bug", status: "blocked", priority: 1 }),
    wr("wb-4", "Move the blog", { priority: 0, status: "in_progress" }),
    wr("wb-5", "Update the footer", { priority: 0, status: "closed", closedAt: hub0 }),
    wr("wb-7", "Answer the hosting email", { status: "blocked", priority: 3 }),
    wr("wb-6", "Write the about page", { priority: 3, updatedAt: "2026-10-14T09:30:00.000Z" }),
  ];
}

/** An entry of a repo's list (a card or a top-level task), by id. */
function entry(page: Page, id: string) {
  return page.locator("#epics .ep-list > .ep-item", { has: page.locator(":scope > .ep-line .ep-id", { hasText: new RegExp(`^${id.replace(/\./g, "\\.")}$`) }) });
}

async function openEpics(page: Page) {
  await page.locator("#nav").getByRole("button", { name: "Epics" }).click();
  await expect(page.locator("#view-title")).toHaveText("Epics");
  await expect(page.locator("#epics .ep-repo").first()).toBeVisible();
}

async function noHorizontalScroll(page: Page) {
  const [scroll, inner] = await page.evaluate(() => [document.documentElement.scrollWidth, window.innerWidth]);
  expect(scroll).toBeLessThanOrEqual(inner);
}

/** The vertical middle of an element, to tell what shares a line. */
async function middle(page: Page, sel: string) {
  const b = (await page.locator(sel).boundingBox())!;
  return b.y + b.height / 2;
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

  test("the title, search box and menus share one line; the task views keep their own search", async ({ page, hub }) => {
    withEpics(hub);
    await openApp(page);
    await expect(page.locator("#ep-search")).toBeHidden();
    await expect(page.locator("#ep-menus")).toBeHidden();
    await openEpics(page);
    await expect(page.locator("#search-form")).toBeHidden();
    await expect(page.locator("#qa-form")).toBeHidden();
    await expect(page.locator("#select-btn")).toBeHidden();
    const title = await middle(page, "#view-title");
    for (const sel of ["#ep-q", "#ep-mb-repo", "#ep-mb-filter", "#ep-mb-view"]) expect(Math.abs((await middle(page, sel)) - title)).toBeLessThan(4);
    // Title left, the search box in the middle, the menus at the right end.
    const [t, q, v] = await Promise.all(["#view-title", "#ep-q", "#ep-mb-view"].map((s) => page.locator(s).boundingBox()));
    expect(t!.x).toBeLessThan(q!.x);
    expect(q!.x + q!.width).toBeLessThan(v!.x);
    // No filter, no chip row.
    await expect(page.locator("#ep-chips")).toBeHidden();
  });

  test("cards and tasks are peer rows; a card shows its progress, expands, and opens its Details", async ({ page, hub }) => {
    withEpics(hub);
    await openApp(page);
    await openEpics(page);

    const repo = page.locator("#epics .ep-repo");
    await expect(repo.getByRole("heading", { level: 2, name: "garden" })).toBeVisible();
    await expect(repo.locator(".ep-sum")).toHaveText("3 epics, 2 tasks, 8 open · Synced 5m ago");
    // By priority, then status, then most recently updated; cards and tasks together.
    await expect(page.locator("#epics .ep-list > .ep-item > .ep-line .ep-id")).toHaveText(["ga-10", "ga-31", "ga-20", "ga-20.1", "ga-30"]);

    const planting = entry(page, "ga-10");
    await expect(planting.locator("> .ep-line .ep-st")).toHaveText("In progress");
    await expect(planting.locator("> .ep-line .ep-pri")).toHaveText("High");
    await expect(planting.locator("> .ep-line .ep-pri")).toHaveClass(/ep-p1/);
    await expect(planting.locator(".ep-ptext")).toHaveText("85% 6/7 done");
    await expect(planting.locator(".ep-kids")).toHaveCount(0);

    const toggle = planting.getByRole("button", { name: "7 tasks of ga-10" });
    await expect(toggle).toHaveAttribute("aria-expanded", "false");
    await toggle.click();
    await expect(planting.getByRole("button", { name: "7 tasks of ga-10" })).toHaveAttribute("aria-expanded", "true");
    // Open children before closed ones.
    const kids = planting.locator(".ep-row");
    await expect(kids).toHaveCount(7);
    await expect(kids.first()).toContainText("Plant the tulip bulbs");
    await expect(kids.first().locator(".ep-who")).toHaveText("sam");
    await expect(kids.first().locator(".ep-pri")).toHaveText("Medium");
    await expect(kids.nth(1)).toHaveClass(/is-done/);

    // A nested card is a row of its parent, and a card of its own.
    const beds = entry(page, "ga-20");
    await beds.getByRole("button", { name: "3 tasks of ga-20" }).click();
    await expect(beds.locator(".ep-row .ep-id")).toHaveText(["ga-20.2", "ga-1", "ga-20.1"]);
    await expect(beds.locator(".ep-row").first().locator(".ep-pri")).toHaveClass(/ep-p0/);
    await expect(entry(page, "ga-20.1").locator(".ep-ptext")).toHaveText("100% 1/1 done");
    await axe(page);

    await planting.locator("> .ep-line").getByRole("button", { name: /Autumn planting/ }).click();
    const sheet = page.locator("#detail");
    await expect(sheet.locator(".d-title")).toHaveText("Autumn planting");
    await expect(sheet.locator(".ep-facts")).toContainText("In progress");
    await expect(sheet.locator(".ep-facts")).toContainText("High P1");
    await expect(sheet.locator(".ep-facts")).toContainText("85% 6/7 done");
    await expect(sheet.locator(".md strong")).toHaveText("Bulbs");
    await expect(sheet.locator(".ep-d-kid")).toHaveCount(7);
    // Read-only: no field, no check box, no action bar.
    await expect(sheet.locator("input, textarea, .check, [data-act=done]")).toHaveCount(0);
    await expect(planting).toHaveClass(/is-open/);
    await axe(page);
    await page.keyboard.press("Escape");
    await expect(sheet.locator(".d-title")).toHaveCount(0);
    await expect(planting.locator("> .ep-line").getByRole("button", { name: /Autumn planting/ })).toBeFocused();

    // No epic row reached the person's lists.
    await page.locator("#nav").getByRole("button", { name: "Everything" }).click();
    await expect(page.locator("#list")).not.toContainText("Autumn planting");
    await expect(page.locator("#list")).not.toContainText("Sharpen the shears");
  });

  test("any row opens Details; a child links to its card, and a card lists its children", async ({ page, hub }) => {
    withEpics(hub);
    await openApp(page);
    await openEpics(page);
    await entry(page, "ga-20").getByRole("button", { name: "3 tasks of ga-20" }).click();
    await page.locator("#epics .ep-row").getByRole("button", { name: /Fence post leans/ }).click();
    const sheet = page.locator("#detail");
    await expect(sheet.locator(".d-title")).toHaveText("Fence post leans");
    await expect(sheet.locator(".ep-facts")).toContainText("Blocked");
    await expect(sheet.locator(".ep-facts")).toContainText("bug");
    await expect(sheet.locator(".ep-facts")).toContainText("alex");
    await expect(sheet.locator(".ep-facts")).toContainText("ga-20.2");
    await expect(sheet).toContainText("No description.");
    await sheet.getByRole("button", { name: "Beds and paths" }).click();
    await expect(sheet.locator(".d-title")).toHaveText("Beds and paths");
    await expect(sheet.locator(".ep-d-kid .ep-id")).toHaveText(["ga-20.2", "ga-1", "ga-20.1"]);
    await sheet.locator(".ep-d-kid", { hasText: "Paths" }).click();
    await expect(sheet.locator(".d-title")).toHaveText("Paths");
    await axe(page);
  });

  test("Copy id shows on a pointed-at row and copies the bead's id", async ({ page, hub }) => {
    withEpics(hub);
    await openApp(page);
    await openEpics(page);
    const copy = entry(page, "ga-10").locator("> .ep-line").getByRole("button", { name: "Copy id ga-10" });
    await expect(copy).toHaveCSS("opacity", "0");
    await entry(page, "ga-10").locator("> .ep-line").hover();
    await expect(copy).toHaveCSS("opacity", "1");
    await copy.click();
    await expect(page.locator("#toast")).toContainText("Copied ga-10");
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe("ga-10");
    // Details have it too.
    await entry(page, "ga-31").getByRole("button", { name: /Label the shed shelves/ }).click();
    await page.locator("#detail").getByRole("button", { name: "Copy id ga-31" }).click();
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe("ga-31");
  });

  test("a human row shows its task's title; its Details say it is also your task and open the task", async ({ page, hub }) => {
    withEpics(hub);
    await openApp(page);
    await openEpics(page);
    const beds = entry(page, "ga-20");
    await beds.getByRole("button", { name: "3 tasks of ga-20" }).click();
    // The State item's title wins over the row's.
    const human = beds.locator(".ep-row", { has: page.locator(".ep-id", { hasText: /^ga-1$/ }) });
    await expect(human.locator(".ep-title")).toHaveText("Order compost for the beds");
    await expect(human.locator(".ep-human")).toHaveText("Also your task");
    await human.getByRole("button", { name: /Order compost for the beds/ }).click();
    const sheet = page.locator("#detail");
    await expect(sheet.locator(".d-title")).toHaveText("Order compost for the beds");
    await expect(sheet.locator(".ep-yours")).toContainText("Also your task");
    await expect(sheet.locator("[data-act=done]")).toHaveCount(0);
    await sheet.getByRole("button", { name: "Open task" }).click();
    await expect(sheet.locator("[data-act=done]").first()).toBeVisible();
    // The sheet's own Copy id copies the id itself.
    await sheet.locator("#d-more").click();
    await sheet.getByRole("button", { name: /Copy id/ }).click();
    await expect(page.locator("#toast")).toContainText("Copied ga-1");
  });

  test("search and filters narrow the rows, show chips, and clear", async ({ page, hub }) => {
    withEpics(hub);
    hub.showEpics("website", websiteEpics());
    await openApp(page);
    await openEpics(page);
    await expect(page.locator("#epics .ep-repo")).toHaveCount(2);

    // A search matches title or id; a card shows with only its matching children, opened.
    await page.locator("#ep-q").fill("TULIP");
    const planting = entry(page, "ga-10");
    await expect(page.locator("#epics .ep-list > .ep-item")).toHaveCount(1);
    await expect(planting.locator(".ep-row .ep-title")).toHaveText(["Plant the tulip bulbs"]);
    await expect(planting.locator(".ep-kids-btn")).toContainText("(1 shown)");
    await expect(page.locator("#epics .ep-repo")).toHaveCount(1);
    await page.locator("#ep-q").fill("wb-6");
    await expect(page.locator("#epics .ep-list > .ep-item .ep-id")).toHaveText(["wb-6"]);
    await page.locator("#ep-q").fill("");

    // Filter: statuses OR together, AND with a priority.
    await page.locator("#ep-mb-filter").click();
    const menu = page.getByRole("menu");
    await expect(menu).toBeVisible();
    await axe(page);
    await menu.getByRole("menuitemcheckbox", { name: "Blocked" }).click();
    await expect(menu.getByRole("menuitemcheckbox", { name: "Blocked" })).toHaveAttribute("aria-checked", "true");
    // A multi-select menu stays open.
    await menu.getByRole("menuitemcheckbox", { name: /Critical/ }).click();
    await expect(page.locator("#ep-mb-filter")).toContainText("Filter 2");
    await page.keyboard.press("Escape");
    await expect(menu).toHaveCount(0);
    await expect(page.locator("#ep-mb-filter")).toBeFocused();
    await expect(page.locator("#epics .ep-list > .ep-item > .ep-line .ep-id")).toHaveText(["ga-20"]);
    await expect(entry(page, "ga-20").locator(".ep-row .ep-id")).toHaveText(["ga-20.2"]);

    const chips = page.locator("#ep-chips");
    await expect(chips.getByRole("button", { name: "Remove filter: Blocked" })).toBeVisible();
    await chips.getByRole("button", { name: "Remove filter: Critical" }).click();
    await expect(chips.getByRole("button", { name: "Remove filter: Critical" })).toHaveCount(0);
    await expect(page.locator("#epics .ep-list > .ep-item > .ep-line .ep-id")).toHaveText(["ga-20", "wb-3", "wb-7"]);

    // Repo picks sections, with each repo's mark and open count.
    await page.locator("#ep-mb-repo").click();
    const website = page.getByRole("menuitemcheckbox", { name: /website/ });
    await expect(website.locator(".ep-n")).toHaveText("7");
    await expect(page.getByRole("menuitemcheckbox", { name: /garden/ }).locator(".ep-n")).toHaveText("8");
    await website.click();
    await page.locator("#view-title").click();
    await expect(page.getByRole("menu")).toHaveCount(0);
    await expect(page.locator("#ep-mb-repo")).toContainText("website");
    await expect(page.locator("#epics .ep-repo")).toHaveCount(1);
    await expect(page.locator("#epics .ep-list > .ep-item > .ep-line .ep-id")).toHaveText(["wb-3", "wb-7"]);
    await axe(page);

    await chips.getByRole("button", { name: "Clear" }).click();
    await expect(chips).toBeHidden();
    await expect(page.locator("#epics .ep-repo")).toHaveCount(2);
    await expect(page.locator("#ep-q")).toBeFocused();
  });

  test("the View menu sorts and expands or collapses everything", async ({ page, hub }) => {
    withEpics(hub);
    await openApp(page);
    await openEpics(page);
    await page.locator("#ep-mb-view").click();
    await page.getByRole("menuitemradio", { name: "Progress" }).click();
    await expect(page.getByRole("menu")).toHaveCount(0);
    // Least done first; tasks after the cards.
    await expect(page.locator("#epics .ep-list > .ep-item > .ep-line .ep-id")).toHaveText(["ga-20", "ga-10", "ga-20.1", "ga-31", "ga-30"]);

    // The keyboard moves through a menu.
    await page.locator("#ep-mb-view").focus();
    await page.keyboard.press("Enter");
    await expect(page.getByRole("menuitemradio", { name: "Priority" })).toBeFocused();
    await page.keyboard.press("End");
    await expect(page.getByRole("menuitem", { name: "Collapse all" })).toBeFocused();
    await page.keyboard.press("ArrowUp");
    await expect(page.getByRole("menuitem", { name: "Expand all" })).toBeFocused();
    await page.keyboard.press("Enter");
    await expect(page.locator("#epics .ep-kids")).toHaveCount(3);

    await page.locator("#ep-mb-view").click();
    await page.getByRole("menuitem", { name: "Collapse all" }).click();
    await expect(page.locator("#epics .ep-list")).toHaveCount(0);
    await expect(page.locator("#epics .ep-fold")).toHaveAttribute("aria-expanded", "false");
    await page.locator("#epics .ep-fold").click();
    await expect(page.locator("#epics .ep-list > .ep-item")).toHaveCount(5);
    await expect(page.locator("#epics .ep-kids")).toHaveCount(0);
    // Anywhere on the heading folds it too, not only the arrow.
    await page.locator("#epics .ep-repo-name h2").click();
    await expect(page.locator("#epics .ep-fold")).toHaveAttribute("aria-expanded", "false");
    await expect(page.locator("#epics .ep-list")).toHaveCount(0);
    await page.locator("#epics .ep-sum").click();
    await expect(page.locator("#epics .ep-fold")).toHaveAttribute("aria-expanded", "true");
  });

  test("Needs attention shows five Critical or Blocked rows, then more, folds, and opens Details", async ({ page, hub }) => {
    withEpics(hub);
    hub.showEpics("website", websiteEpics());
    await openApp(page);
    await openEpics(page);
    const attn = page.locator("#epics .ep-attn");
    await expect(attn.getByRole("heading", { name: /Needs attention/ })).toContainText("6");
    await expect(attn.locator(".ep-attn-row")).toHaveCount(5);
    // Closed rows are left out; Critical first.
    await expect(attn).not.toContainText("Update the footer");
    await expect(attn.locator(".ep-attn-row").first().locator(".ep-pri")).toHaveText("Critical");
    await attn.getByRole("button", { name: "Show 1 more" }).click();
    await expect(attn.locator(".ep-attn-row")).toHaveCount(6);
    await attn.getByRole("button", { name: "Hide Needs attention" }).click();
    await expect(attn.locator(".ep-attn-row")).toHaveCount(0);
    await attn.getByRole("button", { name: "Show Needs attention" }).click();
    await attn.getByRole("button", { name: /Fix the contact form/ }).click();
    await expect(page.locator("#detail .d-title")).toHaveText("Fix the contact form");
    // It follows the filters.
    await page.locator("#ep-q").fill("domain");
    await expect(attn.locator(".ep-attn-row")).toHaveCount(1);
    await page.locator("#ep-q").fill("nothing like this");
    await expect(page.locator("#epics .ep-attn")).toHaveCount(0);
    await expect(page.locator("#epics")).toContainText("No epics or tasks match the filters.");
  });

  test("a list's mark is the same in the sidebar, on a task, in its sheet, in Settings and in Epics", async ({ page, hub }) => {
    withEpics(hub);
    hub.repos.find((r) => r.name === "garden")!.colour = 4;
    const hash = hub.setIcon("garden", LEAF_SVG);
    await openApp(page);
    const sideGarden = page.locator("#lists .nav-item", { hasText: "garden" }).locator(".rb");
    await expect(sideGarden).toHaveClass(/rb-icon lc4/);
    await expect(sideGarden.locator("img")).toHaveAttribute("src", `/api/icons/${hash}`);
    await expect.poll(() => sideGarden.locator("img").evaluate((i: HTMLImageElement) => i.naturalWidth)).toBeGreaterThan(0);
    // Website has no icon: its letter, on its colour; the personal list keeps its own mark.
    await expect(page.locator("#lists .nav-item", { hasText: "website" }).locator(".rb")).toHaveText("W");
    await expect(page.locator("#lists .nav-item", { hasText: "website" }).locator(".rb")).toHaveClass(/lc2/);
    await expect(page.locator("#lists .nav-item", { hasText: "personal" }).locator(".rb")).toHaveClass(/rb-own/);

    // A task's list chip and its sheet's List line.
    await page.locator("#nav").getByRole("button", { name: "Everything" }).click();
    const gardenRow = page.locator("#list .row", { hasText: "Order compost for the beds" });
    await expect(gardenRow.locator(".chip-list .rb img")).toHaveAttribute("src", `/api/icons/${hash}`);
    // The sheet's List line is the personal list's.
    await page.locator("#list .row", { hasText: "Water the plants" }).locator(".row-body").click();
    await expect(page.locator("#detail .prop", { hasText: "List" }).locator(".rb")).toHaveClass(/rb-own/);

    await openEpics(page);
    const head = page.locator("#epics .ep-repo");
    await expect(head).toHaveClass(/lc4/);
    await expect(head.locator(".ep-repo-head .rb img")).toHaveAttribute("src", `/api/icons/${hash}`);
    await expect(page.locator("#epics .ep-attn-row .rb img")).toHaveAttribute("src", `/api/icons/${hash}`);
    await axe(page);

    await page.locator(".sidebar").getByRole("button", { name: "Settings" }).click();
    await expect(page.locator(".m-repo", { hasText: "garden" }).first().locator(".m-repo-title .rb img")).toHaveAttribute("src", `/api/icons/${hash}`);
    await expect(page.locator(".m-repo", { hasText: "website" }).first().locator(".m-repo-title .rb")).toHaveText("W");
  });

  test("an icon that fails to load shows the letter; an older hub's lists take colours by order", async ({ page, hub }) => {
    withEpics(hub);
    for (const r of hub.repos) delete r.colour;
    hub.repos.find((r) => r.name === "garden")!.icon = "f".repeat(32);
    await openApp(page);
    const garden = page.locator("#lists .nav-item", { hasText: "garden" }).locator(".rb");
    await expect(garden.locator("img")).toHaveCount(0);
    await expect(garden).toHaveText("G");
    await expect(garden).toHaveClass(/lc1/);
    await expect(page.locator("#lists .nav-item", { hasText: "website" }).locator(".rb")).toHaveClass(/lc2/);
    await openEpics(page);
    await expect(page.locator("#epics .ep-repo-head .rb")).toHaveText("G");
    await expect(page.locator("#epics .ep-repo")).toHaveClass(/lc1/);
  });

  test("an epics event loads the epics again, and never State", async ({ page, hub }) => {
    withEpics(hub);
    hub.epicsEventMs = 200;
    await openApp(page);
    await page.waitForTimeout(500);
    // Not on Epics: the events load nothing.
    expect(hub.requests).not.toContain("GET /api/epics");
    await openEpics(page);
    await expect(page.locator("#epics .ep-list > .ep-item")).toHaveCount(5);
    const states = hub.requests.filter((r) => r === "GET /api/state").length;
    hub.epicRows.get("home/garden")!.push({ id: "ga-32", title: "Oil the hinges", status: "open", priority: 2, updatedAt: hub.now() });
    await expect(page.locator("#epics .ep-list > .ep-item")).toHaveCount(6);
    expect(hub.requests.filter((r) => r === "GET /api/state").length).toBe(states);
  });

  test("a repo with nothing open says so; one that sent no set is left out", async ({ page, hub }) => {
    hub.showEpics("garden", []);
    hub.showEpics("website");
    await openApp(page);
    await openEpics(page);
    await expect(page.locator("#epics .ep-repo")).toHaveCount(1);
    await expect(page.locator("#epics .ep-repo")).toContainText("Nothing open.");
    await expect(page.locator("#epics .ep-attn")).toHaveCount(0);
    await axe(page);
  });

  test("a failed load says so, and the view recovers when the hub answers again", async ({ page, hub }) => {
    withEpics(hub);
    hub.failEpics = true;
    await openApp(page);
    await page.locator("#nav").getByRole("button", { name: "Epics" }).click();
    await expect(page.locator("#epics")).toContainText("Could not load epics");
    await expect(page.locator("#epics .ep-item")).toHaveCount(0);
    hub.failEpics = false;
    // Coming back to the view loads it again.
    await page.locator("#nav").getByRole("button", { name: "Everything" }).click();
    await page.locator("#nav").getByRole("button", { name: "Epics" }).click();
    await expect(entry(page, "ga-10").locator(".ep-ptext")).toHaveText("85% 6/7 done");
  });

  test("the last repo's epics switched off at its machine takes the view back to Today", async ({ page, hub }) => {
    withEpics(hub);
    hub.eventRetryMs = 200; // the hub's change events reach the app
    await openApp(page);
    await openEpics(page);
    // The machine reports the repo without epics on, and the hub tells the app.
    hub.repos.find((r) => r.epicsOn)!.epicsOn = false;
    await expect(page.locator("#view-title")).toHaveText("Today");
    await expect(page.locator("#nav").getByRole("button", { name: "Epics" })).toHaveCount(0);
    await expect(page.locator("#ep-menus")).toBeHidden();
  });

  test("Settings switches a repo's epics on and off, as it does sync", async ({ page, hub }) => {
    withEpics(hub);
    await openApp(page);
    await page.locator(".sidebar").getByRole("button", { name: "Settings" }).click();
    const sw = page.getByRole("switch", { name: "Show the epics of garden (garden)" });
    await expect(sw).toHaveAttribute("aria-checked", "true");
    const web = page.getByRole("switch", { name: "Show the epics of website (website)" });
    await expect(web).toHaveAttribute("aria-checked", "false");
    await axe(page);

    await web.click();
    expect(hub.posted.find((a) => a.kind === "epics")).toMatchObject({ bridge: "home", repo: "website", payload: { on: true } });
    const website = page.locator(".m-repo", { has: page.getByRole("switch", { name: "Sync website (website)" }) });
    await expect(web).toHaveAttribute("aria-checked", "true");
    await expect(website).toContainText("Waiting for home");

    await sw.click();
    expect(hub.posted.filter((a) => a.kind === "epics").at(-1)).toMatchObject({ bridge: "home", repo: "garden", payload: { on: false } });
    const garden = page.locator(".m-repo", { has: page.getByRole("switch", { name: "Sync garden (garden)" }) });
    await expect(sw).toHaveAttribute("aria-checked", "false");
    await expect(garden).toContainText("Waiting for home");
    await page.locator(".sidebar .sync-pill").click();
    await expect(page.locator("#syncpop")).toContainText("Epics off: garden on home");
    await expect(page.locator("#syncpop")).toContainText("Epics on: website on home");
  });
});

test.describe("phone", () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });

  test("Epics is a tab; rows fit the screen, menus and Details open as sheets", async ({ page, hub }) => {
    withEpics(hub);
    hub.setIcon("garden", LEAF_SVG);
    await openApp(page);
    await page.locator("#tabs").getByRole("button", { name: "Epics" }).click();
    await expect(page.locator("#view-title")).toHaveText("Epics");
    await expect(page.locator(".fab")).toBeVisible();
    // The title and the menus share the first line; the search box takes the next, full width.
    const title = await middle(page, "#view-title");
    expect(Math.abs((await middle(page, "#ep-mb-view")) - title)).toBeLessThan(4);
    expect(await middle(page, "#ep-q")).toBeGreaterThan(title + 20);

    const planting = entry(page, "ga-10");
    await expect(planting.locator(".ep-ptext")).toHaveText("85% 6/7 done");
    await planting.getByRole("button", { name: "7 tasks of ga-10" }).click();
    await expect(planting.locator(".ep-row")).toHaveCount(7);
    await noHorizontalScroll(page);
    await axe(page);

    await page.locator("#ep-mb-filter").click();
    const sheetMenu = page.locator("#ep-mm-filter");
    await expect(sheetMenu).toBeVisible();
    await expect(page.locator("#scrim")).toBeVisible();
    await settle(page);
    const box = (await sheetMenu.boundingBox())!;
    expect(Math.round(box.x)).toBe(0);
    expect(Math.round(box.width)).toBe(390);
    expect(Math.round(box.y + box.height)).toBe(844);
    await axe(page);
    await sheetMenu.getByRole("menuitemcheckbox", { name: "Closed" }).click();
    await sheetMenu.getByRole("button", { name: "Close" }).click();
    await expect(sheetMenu).toHaveCount(0);
    await expect(page.locator("#ep-chips")).toContainText("Closed");
    await page.locator("#ep-chips").getByRole("button", { name: "Clear" }).click();

    await planting.locator("> .ep-line").getByRole("button", { name: /Autumn planting/ }).click();
    const sheet = page.locator("#detail");
    await expect(sheet.locator(".d-title")).toHaveText("Autumn planting");
    await expect(page.locator("#scrim")).toBeVisible();
    await axe(page);
    await sheet.getByRole("button", { name: "Close details" }).click();
    await expect(page.locator("#app")).not.toHaveClass(/detail-open/);

    // The menu lists it too, with each list's mark.
    const drawer = await openMenu(page);
    await expect(drawer.getByRole("button", { name: "Epics" })).toBeVisible();
    await expect(drawer.locator("#lists .nav-item", { hasText: "garden" }).locator(".rb img")).toBeVisible();
  });

  test("dark and light themes pass the contrast check", async ({ page, hub }) => {
    withEpics(hub);
    hub.showEpics("website", websiteEpics());
    await openApp(page);
    await page.locator("#tabs").getByRole("button", { name: "Epics" }).click();
    await entry(page, "ga-20").getByRole("button", { name: "3 tasks of ga-20" }).click();
    await page.locator("#ep-q").fill("");
    await axe(page);
    await page.evaluate(() => {
      document.documentElement.dataset.theme = "light";
    });
    await axe(page);
  });
});

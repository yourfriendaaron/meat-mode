import { test, expect, openApp, row, axe } from "./setup.ts";
import type { Page } from "@playwright/test";
import type { FakeHub } from "./fake-hub.ts";

test.use({ viewport: { width: 1280, height: 800 } });

/** Tags on two personal to-dos and an agent's item; the agent's also carries `human`, which never shows. */
function seedTags(hub: FakeHub) {
  hub.find("hub/personal/pe-1")!.labels = ["garden"];
  hub.find("hub/personal/pe-5")!.labels = ["garden", "weekend"];
  hub.find("home/garden/ga-2")!.labels = ["human", "Garden", "outdoor"];
}

async function openDetail(page: Page, title: string) {
  await row(page, title).locator(".row-body").click();
  await expect(page.locator("#detail #d-title")).toHaveText(title);
  return page.locator("#detail");
}

const titles = (page: Page) => page.locator("#list .row-title");

test("tags show as chips on rows and in the detail, never human; an agent's are read-only", async ({ page, hub }) => {
  seedTags(hub);
  await openApp(page);
  await page.locator(".sidebar").getByRole("button", { name: "Everything" }).click();
  await expect(row(page, "Water the plants").locator(".chip-tag")).toHaveText(["garden"]);
  await expect(row(page, "Plan the garden layout").locator(".chip-tag")).toHaveText(["garden", "weekend"]);
  await expect(row(page, "Check the tomato labels").locator(".chip-tag")).toHaveText(["Garden", "outdoor"]);
  await expect(page.locator("#list .chip-tag", { hasText: /^human$/i })).toHaveCount(0);

  const detail = await openDetail(page, "Check the tomato labels");
  await expect(detail.locator(".d-chips .chip-tag")).toHaveText(["Garden", "outdoor"]);
  await expect(detail.getByRole("button", { name: "Show items tagged outdoor" })).toBeVisible();
  // An agent's item has no edit form, so its tags cannot be changed here.
  await expect(detail.getByRole("button", { name: "Edit", exact: true })).toHaveCount(0);
  await axe(page);
});

test("a tag chip filters every list by it, until the filter is cleared", async ({ page, hub }) => {
  seedTags(hub);
  await openApp(page);
  await expect(page.locator("#tagbar")).toBeHidden();
  await row(page, "Water the plants").locator(".chip-tag").click();
  // The chip filters; it does not open the row.
  await expect(page.locator("#detail #d-title")).toHaveCount(0);
  await expect(page.locator("#view-title")).toHaveText("Tagged");
  await expect(page.locator("#tagbar")).toBeVisible();
  await expect(page.locator("#tagbar")).toContainText("Tag: garden");
  // Across lists and without case: the agent's "Garden" too.
  await expect(titles(page)).toHaveText(["Water the plants", "Plan the garden layout", "Check the tomato labels"]);
  await axe(page);

  // Search looks within the filter.
  await page.locator("#search-input").fill("layout");
  await expect(page.locator("#view-title")).toHaveText("Search");
  await expect(titles(page)).toHaveText(["Plan the garden layout"]);
  await page.locator("#search-input").fill("");

  await page.locator("#tagbar").getByRole("button", { name: "Clear the tag filter garden" }).click();
  await expect(page.locator("#tagbar")).toBeHidden();
  await expect(page.locator("#view-title")).toHaveText("Today");
  await expect(row(page, "Call Sam about the weekend")).toBeVisible();

  // A tag in the detail filters too; Escape and picking a view end it.
  const detail = await openDetail(page, "Water the plants");
  await detail.getByRole("button", { name: "Show items tagged garden" }).click();
  await expect(page.locator("#view-title")).toHaveText("Tagged");
  await page.locator("#detail").getByRole("button", { name: "Close details" }).click();
  await page.keyboard.press("Escape");
  await expect(page.locator("#tagbar")).toBeHidden();
  await expect(page.locator("#view-title")).toHaveText("Today");
  await row(page, "Plan the garden layout").locator(".chip-tag", { hasText: "weekend" }).click();
  await expect(titles(page)).toHaveText(["Plan the garden layout"]);
  await page.locator(".sidebar").getByRole("button", { name: "Upcoming" }).click();
  await expect(page.locator("#tagbar")).toBeHidden();
  await expect(page.locator("#view-title")).toHaveText("Upcoming");
});

test("search finds an item by its tag, with or without the @", async ({ page, hub }) => {
  seedTags(hub);
  await openApp(page);
  await page.locator("#search-input").fill("weekend");
  await expect(titles(page)).toHaveText(["Call Sam about the weekend", "Plan the garden layout"]);
  await page.locator("#search-input").fill("@outdoor");
  await expect(titles(page)).toHaveText(["Check the tomato labels"]);
  await page.locator("#search-input").fill("human");
  await expect(page.locator("#list .empty")).toHaveText("No matches.");
});

test("the edit form sets and removes tags, and Undo puts them back", async ({ page, hub }) => {
  seedTags(hub);
  await openApp(page);
  const detail = await openDetail(page, "Water the plants");
  await detail.getByRole("button", { name: "Edit", exact: true }).click();
  const field = detail.getByLabel("Tags");
  await expect(field).toHaveValue("garden");

  // The hub's rules apply before anything is sent.
  await field.fill(`garden ${"a".repeat(51)}`);
  await detail.getByRole("button", { name: "Save", exact: true }).click();
  await expect(detail.locator(".edit-form [role=alert]")).toHaveText(`The tag "${"a".repeat(51)}" is longer than 50 characters.`);
  await expect(field).toBeFocused();
  expect(hub.posted).toEqual([]);

  await field.fill("@weekend, Errands garden WEEKEND");
  await detail.getByRole("button", { name: "Save", exact: true }).click();
  await expect.poll(() => hub.posted.at(-1)).toEqual({
    id: expect.any(String),
    kind: "edit",
    key: "hub/personal/pe-1",
    payload: { set: { labels: ["weekend", "Errands", "garden"] } },
  });
  await expect(detail.locator(".d-chips .chip-tag")).toHaveText(["weekend", "Errands", "garden"]);
  await expect(row(page, "Water the plants").locator(".chip-tag")).toHaveText(["weekend", "Errands", "garden"]);

  await page.locator("#toast").getByRole("button", { name: "Undo" }).click();
  await expect.poll(() => hub.posted.at(-1)?.payload.set).toEqual({ labels: ["garden"] });
  await expect(row(page, "Water the plants").locator(".chip-tag")).toHaveText(["garden"]);

  // An empty field clears them.
  await detail.getByRole("button", { name: "Edit", exact: true }).click();
  await detail.getByLabel("Tags").fill("");
  await detail.getByRole("button", { name: "Save", exact: true }).click();
  await expect.poll(() => hub.posted.at(-1)?.payload.set).toEqual({ labels: [] });
  await expect(row(page, "Water the plants").locator(".chip-tag")).toHaveCount(0);
});

test("quick add reads @word as a tag on the personal list, and leaves it in the title on a tracker's", async ({ page, hub }) => {
  await openApp(page);
  const input = page.getByRole("textbox", { name: "New task" });
  await input.fill("Buy bulbs @garden tomorrow @5pm");
  await expect(page.locator("#qa-backdrop mark")).toHaveText(["@garden", "tomorrow"]);
  await expect(page.locator("#qa-chips .chip-tag")).toHaveText(["garden"]);
  await input.press("Enter");
  await expect(input).toHaveValue("");
  expect(hub.posted.at(-1)).toMatchObject({
    kind: "add",
    bridge: "hub",
    repo: "personal",
    payload: { title: "Buy bulbs @5pm", due: "2026-10-15T09:00:00.000Z", labels: ["garden"] },
  });
  await expect(row(page, "Buy bulbs @5pm").locator(".chip-tag")).toHaveText(["garden"]);

  await input.fill("Prune the roses @weekend #garden");
  await expect(page.locator("#qa-backdrop mark")).toHaveText(["#garden"]);
  await expect(page.locator("#qa-chips .chip-tag")).toHaveCount(0);
  await input.press("Enter");
  await expect(input).toHaveValue("");
  const add = hub.posted.at(-1)!;
  expect(add).toMatchObject({ kind: "add", bridge: "home", repo: "garden", payload: { title: "Prune the roses @weekend" } });
  expect(add.payload.labels).toBeUndefined();
});

test.describe("phone", () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });

  test("a tapped tag filters the list, and the chip above it clears the filter", async ({ page, hub }) => {
    seedTags(hub);
    await openApp(page);
    await row(page, "Water the plants").locator(".chip-tag").tap();
    await expect(page.locator("#view-title")).toHaveText("Tagged");
    const bar = page.locator("#tagbar");
    await expect(bar).toBeInViewport();
    await expect(titles(page)).toHaveText(["Water the plants", "Plan the garden layout", "Check the tomato labels"]);
    const [scroll, inner] = await page.evaluate(() => [document.documentElement.scrollWidth, window.innerWidth]);
    expect(scroll).toBeLessThanOrEqual(inner);
    await axe(page);
    await bar.getByRole("button", { name: "Clear the tag filter garden" }).tap();
    await expect(bar).toBeHidden();
    await expect(page.locator("#view-title")).toHaveText("Today");

    // From the detail sheet, which makes way for the list.
    await row(page, "Plan the garden layout").locator(".row-title").tap();
    await page.locator("#detail").getByRole("button", { name: "Show items tagged weekend" }).tap();
    await expect(page.locator("#app")).not.toHaveClass(/detail-open/);
    await expect(titles(page)).toHaveText(["Plan the garden layout"]);
  });

  test("the edit sheet sets a tag", async ({ page, hub }) => {
    await openApp(page);
    await row(page, "Call Sam about the weekend").locator(".row-title").tap();
    const detail = page.locator("#detail");
    await detail.getByRole("button", { name: "Edit", exact: true }).tap();
    await detail.getByLabel("Tags").fill("family");
    await detail.getByRole("button", { name: "Save", exact: true }).tap();
    await expect.poll(() => hub.posted.at(-1)?.payload.set).toEqual({ labels: ["family"] });
    await expect(detail.locator(".d-chips .chip-tag")).toHaveText(["family"]);
  });
});

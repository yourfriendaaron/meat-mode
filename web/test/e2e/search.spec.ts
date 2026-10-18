import { test, expect, openApp, row } from "./setup.ts";

test.use({ viewport: { width: 1280, height: 800 } });

test("a word from a note finds its item, which opens", async ({ page, hub }) => {
  await openApp(page);
  await page.locator("#search-input").fill("shorten");
  await expect(page.locator("#view-title")).toHaveText("Search");
  await expect(page).toHaveTitle("Search · Meat Mode");
  await expect(page.locator("#list .group-head h2")).toHaveText([/Open/]);
  await expect(page.locator("#list .row-title")).toHaveText(["Approve the new footer text"]);
  await row(page, "Approve the new footer text").locator(".row-body").click();
  await expect(page.locator(".detail")).toContainText("Looks fine, shorten the second line.");
});

test("every word must match, in any field; case is ignored", async ({ page, hub }) => {
  await openApp(page);
  await page.locator("#search-input").fill("COMPOST delivery");
  await expect(page.locator("#list .row-title")).toHaveText(["Order compost for the beds"]);
  await page.locator("#search-input").fill("compost zebra");
  await expect(page.locator("#list .empty")).toHaveText("No matches.");
});

test("a done item shows under Done with Reopen", async ({ page, hub }) => {
  await openApp(page);
  await page.locator("#search-input").fill("porch");
  await expect(page.locator("#list .group-head h2")).toHaveText([/Done/]);
  await expect(row(page, "Sweep the porch").getByRole("button", { name: "Reopen", exact: true })).toBeVisible();
});

test("/ focuses the field, Esc clears it and blurs", async ({ page, hub }) => {
  await openApp(page);
  const input = page.locator("#search-input");
  await page.keyboard.press("/");
  await expect(input).toBeFocused();
  // The slash itself is not typed into the field.
  await expect(input).toHaveValue("");
  await page.keyboard.type("plants");
  await expect(page.locator("#list .row-title")).toHaveText(["Water the plants"]);
  await page.keyboard.press("Escape");
  await expect(input).toHaveValue("");
  await expect(input).not.toBeFocused();
  await expect(page.locator("#view-title")).toHaveText("Today");
  await expect(page.locator("#list .row").nth(1)).toBeVisible();
});

test("Enter does not reload the page, and Settings has no search", async ({ page, hub }) => {
  await openApp(page);
  await page.evaluate(() => ((window as unknown as { marker: number }).marker = 1));
  await page.locator("#search-input").fill("plants");
  await page.keyboard.press("Enter");
  await expect(page.locator("#list .row-title")).toHaveText(["Water the plants"]);
  expect(await page.evaluate(() => (window as unknown as { marker?: number }).marker)).toBe(1);
  await page.getByRole("button", { name: /Settings/ }).first().click();
  await expect(page.locator("#search-form")).toBeHidden();
});

test.describe("phone", () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });

  test("the field fits the header without sideways scroll", async ({ page, hub }) => {
    await openApp(page);
    await page.getByRole("button", { name: "Browse" }).click();
    await expect(page.locator("#view-title")).toHaveText("Everything");
    await page.locator("#search-input").fill("plants");
    await expect(page.locator("#list .row-title")).toHaveText(["Water the plants"]);
    const [scroll, inner] = await page.evaluate(() => [document.documentElement.scrollWidth, window.innerWidth]);
    expect(scroll).toBeLessThanOrEqual(inner);
    const box = await page.locator("#search-input").boundingBox();
    expect(box!.width).toBeGreaterThan(100);
  });
});

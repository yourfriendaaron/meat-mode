import { test, expect, openApp, axe } from "./setup.ts";
import type { Page } from "@playwright/test";
import type { FakeHub } from "./fake-hub.ts";

test.use({ viewport: { width: 1280, height: 800 } });

/** Opens Upcoming, then its Month switch. */
async function openMonth(page: Page) {
  await openApp(page);
  await page.getByRole("navigation", { name: "Views" }).first().getByRole("button", { name: /Upcoming/ }).click();
  await page.getByRole("group", { name: "Show Upcoming as" }).getByRole("button", { name: "Month" }).click();
  await expect(page.locator("#month-h")).toBeVisible();
}

const day = (page: Page, name: string) => page.locator("#list").getByRole("button", { name: new RegExp(`^${name},`) });
/** Taps a day button on its date, clear of the titles drawn over the rest of it. */
const pick = (page: Page, name: string) => day(page, name).click({ position: { x: 12, y: 12 } });
const cell = (page: Page, ymd: string) => page.locator(`#list td[data-day="${ymd}"]`);
const titles = (page: Page, ymd: string) => cell(page, ymd).locator(".ment-t");
const dayList = (page: Page) => page.locator("#list .mlist");

/** Makes "Plan the garden layout" repeat every week. */
function weekly(hub: FakeHub) {
  const it = hub.items.find((i) => i.title === "Plan the garden layout")!;
  it.repeat = { every: "week", timeZone: "UTC", start: it.dueAt! };
}

test("the month shows due, snoozed and repeat entries, and moves between months", async ({ page, hub }) => {
  weekly(hub);
  await openMonth(page);
  await expect(page.locator("#view-title")).toHaveText("Upcoming");
  await expect(page.locator("#nav").getByRole("button", { name: /Upcoming/ })).toHaveAttribute("aria-current", "page");
  await expect(page.locator("#month-h")).toHaveText("October 2026");
  await expect(page.locator("#list .mgrid th")).toHaveText(["MonMonday", "TueTuesday", "WedWednesday", "ThuThursday", "FriFriday", "SatSaturday", "SunSunday"]);
  await expect(page.locator("#list .mgrid tbody tr")).toHaveCount(5);

  // Today is marked and selected; overdue stays on its past day; closed items are left out.
  await expect(day(page, "Wednesday 14 October")).toHaveAttribute("aria-current", "date");
  await expect(day(page, "Wednesday 14 October")).toHaveAccessibleName("Wednesday 14 October, 3 items");
  await expect(titles(page, "2026-10-14")).toHaveText(["Water the plants", "Call Sam about the weekend", "Order compost for the beds"]);
  await expect(titles(page, "2026-10-13")).toHaveText(["Renew the library card"]);
  await expect(cell(page, "2026-10-13")).toHaveClass(/is-past/);
  await expect(page.locator("#list .ment-t", { hasText: "Sweep the porch" })).toHaveCount(0);
  // A snoozed item also shows on the day it comes back, with the moon.
  await expect(cell(page, "2026-10-15").locator(".ment-snooze")).toHaveText("Buy seeds");
  await expect(cell(page, "2026-10-15").locator(".ment-snooze .i-moon")).toHaveCount(1);
  // A repeat's later dates come from the hub, quieter than the real entry.
  await expect(cell(page, "2026-10-23").locator(".ment-due")).toHaveText("Plan the garden layout");
  await expect(cell(page, "2026-10-30").locator(".ment-repeat")).toHaveText("Plan the garden layout");
  expect(hub.requests).toContain("GET /api/occurrences");

  await expect(dayList(page).locator("h2")).toHaveText("Wednesday 14 October");
  await expect(dayList(page).locator(".mrow-title")).toHaveText(["Water the plants", "Call Sam about the weekend", "Order compost for the beds"]);
  await axe(page);

  await page.getByRole("button", { name: "Next month" }).click();
  await expect(page.locator("#month-h")).toHaveText("November 2026");
  await expect(page.locator("#list .mgrid tbody tr")).toHaveCount(6);
  await expect(cell(page, "2026-11-06").locator(".ment-repeat")).toHaveText("Plan the garden layout");
  // The 1st is selected in a month without today.
  await expect(dayList(page).locator("h2")).toHaveText("Sunday 1 November");
  await page.getByRole("button", { name: "Previous month" }).click();
  await page.getByRole("button", { name: "Previous month" }).click();
  await expect(page.locator("#month-h")).toHaveText("September 2026");
  await page.locator("#list").getByRole("button", { name: "Today", exact: true }).click();
  await expect(page.locator("#month-h")).toHaveText("October 2026");
  await expect(dayList(page).locator("h2")).toHaveText("Wednesday 14 October");

  // The shown month survives a trip to another view.
  await page.getByRole("button", { name: "Next month" }).click();
  await page.locator("#nav").getByRole("button", { name: /Today/ }).click();
  await page.locator("#nav").getByRole("button", { name: /Upcoming/ }).click();
  await page.getByRole("button", { name: "Month" }).click();
  await expect(page.locator("#month-h")).toHaveText("November 2026");
});

test("a tapped day lists its entries by time, and an entry opens its item", async ({ page, hub }) => {
  void hub;
  await openMonth(page);
  await pick(page, "Thursday 15 October");
  await expect(day(page, "Thursday 15 October")).toHaveAttribute("aria-pressed", "true");
  await expect(dayList(page).locator("h2")).toHaveText("Thursday 15 October");
  await expect(dayList(page).locator(".mrow-title")).toHaveText(["Approve the new footer text", "Buy seeds"]);
  await expect(dayList(page).locator(".mrow-kind")).toHaveText(["Due", "Snoozed until"]);
  // A title in the grid selects its day too.
  await cell(page, "2026-10-23").locator(".ment").click();
  await expect(dayList(page).locator("h2")).toHaveText("Friday 23 October");
  await dayList(page).getByRole("button", { name: /Plan the garden layout/ }).first().click();
  await expect(page.locator("#detail").getByRole("heading", { name: "Plan the garden layout" })).toBeVisible();
  // The month stays, with its selection.
  await expect(dayList(page).locator("h2")).toHaveText("Friday 23 October");
});

test("a search shows its results in place of the month", async ({ page, hub }) => {
  void hub;
  await openMonth(page);
  await page.locator("#search-input").fill("seeds");
  await expect(page.locator("#list .mgrid")).toHaveCount(0);
  await expect(page.locator("#list .row-title")).toHaveText(["Buy seeds"]);
  await expect(page.locator("#viewswitch")).toBeHidden();
  await page.locator("#search-input").fill("");
  await expect(page.locator("#month-h")).toHaveText("October 2026");
});

test("repeat dates that fail to load leave the month and one quiet line", async ({ page, hub }) => {
  weekly(hub);
  hub.failOccurrences = true;
  await openMonth(page);
  await expect(page.locator("#list .month-note")).toHaveText("Repeat dates could not be loaded.");
  await expect(cell(page, "2026-10-23").locator(".ment-due")).toHaveText("Plan the garden layout");
  await expect(page.locator("#list .ment-repeat")).toHaveCount(0);
});

test("dragging a personal item to another day keeps its time, with Undo", async ({ page, hub }) => {
  await openMonth(page);
  const entry = cell(page, "2026-10-23").locator(".ment", { hasText: "Plan the garden layout" });
  await expect(entry).toHaveAttribute("draggable", "true");
  // A snooze entry of a personal item stays put.
  await expect(cell(page, "2026-10-15").locator(".ment-snooze")).not.toHaveAttribute("draggable");
  await entry.dragTo(cell(page, "2026-10-27"));
  await expect(page.locator(".toast")).toContainText("Moved to Tue, Oct 27");
  await expect.poll(() => hub.posted.at(-1)).toMatchObject({ kind: "edit", key: "hub/personal/pe-5", payload: { set: { due: "2026-10-27T09:00:00.000Z" } } });
  await expect(titles(page, "2026-10-27")).toHaveText(["Plan the garden layout"]);
  await expect(titles(page, "2026-10-23")).toHaveCount(0);

  await page.locator(".toast").getByRole("button", { name: "Undo" }).click();
  await expect.poll(() => hub.posted.at(-1)).toMatchObject({ kind: "edit", key: "hub/personal/pe-5", payload: { set: { due: "2026-10-23T09:00:00.000Z" } } });
  await expect(titles(page, "2026-10-23")).toHaveText(["Plan the garden layout"]);
  await expect(titles(page, "2026-10-27")).toHaveCount(0);

  // A past day is refused in plain words, and nothing is posted.
  const before = hub.posted.length;
  await cell(page, "2026-10-23").locator(".ment").dragTo(cell(page, "2026-10-12"));
  await expect(page.locator(".toast")).toHaveText("That day has passed. Pick today or a later day.");
  expect(hub.posted.length).toBe(before);
});

test("an agent item moved to another day is snoozed there", async ({ page, hub }) => {
  await openMonth(page);
  await cell(page, "2026-10-14").locator(".ment", { hasText: "Order compost for the beds" }).dragTo(cell(page, "2026-10-20"));
  await expect(page.locator(".toast")).toContainText("Snoozed to Tue, Oct 20 18:00");
  await expect.poll(() => hub.posted.at(-1)).toMatchObject({
    kind: "snooze",
    key: "home/garden/ga-1",
    payload: { until: "2026-10-20T18:00:00.000Z", previous: null },
  });
  await expect(cell(page, "2026-10-20").locator(".ment-snooze")).toHaveText("Order compost for the beds");
});

test("the Move dialog moves the selected day's entry by keyboard", async ({ page, hub }) => {
  await openMonth(page);
  await pick(page, "Friday 23 October");
  const move = dayList(page).getByRole("button", { name: "Move: Plan the garden layout" });
  await move.focus();
  await page.keyboard.press("Enter");
  const dlg = page.getByRole("dialog", { name: "Move to another day" });
  await expect(dlg).toBeVisible();
  const field = dlg.getByLabel("Day");
  await expect(field).toHaveValue("2026-10-23");
  await expect(field).toBeFocused();
  await axe(page);

  await field.fill("2026-10-10");
  await dlg.getByRole("button", { name: "Move", exact: true }).click();
  await expect(dlg.getByRole("alert")).toHaveText("That day has passed. Pick today or a later day.");
  await expect(field).toHaveAttribute("aria-invalid", "true");

  await field.fill("2026-10-28");
  await field.press("Enter");
  await expect(dlg).toBeHidden();
  await expect.poll(() => hub.posted.at(-1)).toMatchObject({ kind: "edit", key: "hub/personal/pe-5", payload: { set: { due: "2026-10-28T09:00:00.000Z" } } });
  await expect(page.locator(".toast")).toContainText("Moved to Wed, Oct 28");

  // Cancel and Escape leave it be.
  await pick(page, "Wednesday 28 October");
  await dayList(page).getByRole("button", { name: "Move: Plan the garden layout" }).click();
  await dlg.getByRole("button", { name: "Cancel" }).click();
  await expect(dlg).toBeHidden();
  await expect(dayList(page).getByRole("button", { name: "Move: Plan the garden layout" })).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(dlg).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(dlg).toBeHidden();
});

test("a repeating item cannot move past its repeat's end", async ({ page, hub }) => {
  const it = hub.items.find((i) => i.title === "Plan the garden layout")!;
  it.repeat = { every: "week", timeZone: "UTC", until: "2026-10-31", start: it.dueAt! };
  await openMonth(page);
  await cell(page, "2026-10-23").locator(".ment-due").dragTo(cell(page, "2026-11-01"));
  await expect(page.locator(".toast")).toHaveText("The repeat ends on Oct 31. Pick that day or an earlier one.");
});

test.describe("phone", () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });

  test("the month fits, counts each day, and a long press opens Move", async ({ page, hub }) => {
    await openApp(page);
    const tabs = page.locator("#tabs");
    await tabs.getByRole("button", { name: "Upcoming" }).click();
    await page.getByRole("button", { name: "Month" }).click();
    await expect(page.locator("#month-h")).toHaveText("October 2026");
    await expect(tabs.getByRole("button", { name: "Upcoming" })).toHaveAttribute("aria-current", "page");
    const [scroll, inner] = await page.evaluate(() => [document.documentElement.scrollWidth, window.innerWidth]);
    expect(scroll).toBeLessThanOrEqual(inner);
    await expect(cell(page, "2026-10-14").locator(".mcount")).toHaveText("3");
    await expect(cell(page, "2026-10-14").locator(".ment").first()).toBeHidden();
    await axe(page);

    await day(page, "Friday 23 October").tap();
    const body = dayList(page).locator(".mrow-body", { hasText: "Plan the garden layout" });
    const box = (await body.boundingBox())!;
    const at = { pointerId: 9, pointerType: "touch", isPrimary: true, clientX: box.x + 20, clientY: box.y + box.height / 2, button: 0, bubbles: true };
    // A press that moves is a scroll, not a long press.
    await body.dispatchEvent("pointerdown", at);
    await body.dispatchEvent("pointermove", { ...at, clientY: at.clientY + 30 });
    await page.waitForTimeout(700);
    await expect(page.getByRole("dialog", { name: "Move to another day" })).toBeHidden();
    await body.dispatchEvent("pointerup", at);

    await body.dispatchEvent("pointerdown", at);
    const dlg = page.getByRole("dialog", { name: "Move to another day" });
    await expect(dlg).toBeVisible();
    // The click a release may fire lands on the scrim, and leaves the dialog open.
    await body.dispatchEvent("pointerup", at);
    await page.locator("#scrim").dispatchEvent("click");
    await expect(dlg).toBeVisible();
    await expect(dlg.getByLabel("Day")).toHaveValue("2026-10-23");
    await dlg.getByLabel("Day").fill("2026-10-24");
    await dlg.getByRole("button", { name: "Move", exact: true }).tap();
    await expect.poll(() => hub.posted.at(-1)).toMatchObject({ kind: "edit", payload: { set: { due: "2026-10-24T09:00:00.000Z" } } });
    await expect(page.locator(".toast")).toContainText("Moved to Sat, Oct 24");

    // A tap opens the item's sheet.
    await day(page, "Saturday 24 October").tap();
    await dayList(page).locator(".mrow-body", { hasText: "Plan the garden layout" }).tap();
    await expect(page.locator("#detail").getByRole("heading", { name: "Plan the garden layout" })).toBeVisible();
  });
});

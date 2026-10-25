import { test, expect, openApp, row, axe } from "./setup.ts";
import type { Page } from "@playwright/test";
import type { FakeHub } from "./fake-hub.ts";

test.use({ viewport: { width: 1280, height: 800 } });

const KEY = "hub/personal/pe-1";

async function openDetail(page: Page, title: string) {
  await row(page, title).locator(".row-body").click();
  await expect(page.locator("#detail #d-title")).toHaveText(title);
  return page.locator("#detail");
}

/** The weekly plant check-in, due today at 18:00, counted from when it is done. */
function seedFromDone(hub: FakeHub) {
  const it = hub.find(KEY)!;
  it.dueAt = "2026-10-14T18:00:00Z";
  it.repeat = { every: "week", fromDone: true, timeZone: "UTC", start: it.dueAt };
}

/** The same, on a fixed schedule: every Wednesday at 18:00. */
function seedWeekly(hub: FakeHub) {
  const it = hub.find(KEY)!;
  it.dueAt = "2026-10-14T18:00:00Z";
  it.repeat = { every: "week", weekdays: [3], timeZone: "UTC", start: it.dueAt };
}

const SUNDAYS = { due: "2026-10-18T12:00:00.000Z", repeat: { every: "week", weekdays: [0], timeZone: "UTC" } };

test("a weekly repeat counted from done moves to Sundays at 12:00 by typing it", async ({ page, hub }) => {
  seedFromDone(hub);
  await openApp(page);
  const detail = await openDetail(page, "Water the plants");
  const card = detail.getByRole("button", { name: /^Schedule:/ });
  await expect(card).toContainText("Today at 18:00");
  await expect(card).toContainText("Then 1 week after you finish it");

  await card.click();
  await expect(detail.getByRole("heading", { name: "Schedule" })).toBeVisible();
  const typed = detail.getByRole("textbox", { name: "Type when" });
  await expect(typed).toBeFocused();
  // The days stay on screen while it counts from done, greyed out, and the page says why.
  await expect(detail.getByRole("checkbox", { name: "Sun" })).toBeDisabled();
  await expect(detail.locator("#sched-days-hint")).toContainText("When I finish counts from the day you finish it");

  await typed.fill("every sunday 12pm");
  await expect(detail.locator("#sched-readout")).toHaveText("Every Sunday at 12:00. Next: Sun, Oct 18.");
  // The controls follow what was typed.
  await expect(detail.getByLabel("Date", { exact: true })).toHaveValue("2026-10-18");
  await expect(detail.getByLabel("Time", { exact: true })).toHaveValue("12:00");
  await expect(detail.getByRole("radio", { name: "The schedule" })).toBeChecked();
  await expect(detail.getByRole("checkbox", { name: "Sun" })).toBeChecked();
  await axe(page);

  await typed.press("Enter");
  await expect.poll(() => hub.posted.at(-1)?.payload.set).toEqual(SUNDAYS);
  await expect(card).toBeFocused();
  await expect(card).toContainText("Sun, Oct 18 at 12:00");
  await expect(card).toContainText("Every Sunday");
  await expect(page.locator("#toast")).toContainText("Saved");
});

test("a weekly repeat counted from done moves to Sundays at 12:00 with the controls", async ({ page, hub }) => {
  seedFromDone(hub);
  await openApp(page);
  const detail = await openDetail(page, "Water the plants");
  await detail.locator("#d-sched").click();
  await detail.getByRole("radio", { name: "The schedule" }).check();
  await expect(detail.locator("#sched-from-hint")).toHaveText("The next one keeps to the schedule, however early or late you finish this one.");
  await detail.getByRole("checkbox", { name: "Sun" }).check();
  // The date moves to the first Sunday.
  await expect(detail.getByLabel("Date", { exact: true })).toHaveValue("2026-10-18");
  await detail.getByLabel("Time", { exact: true }).fill("12:00");
  await expect(detail.locator("#sched-readout")).toHaveText("Every Sunday at 12:00. Next: Sun, Oct 18.");
  await detail.getByRole("button", { name: "Save", exact: true }).click();
  await expect.poll(() => hub.posted.at(-1)?.payload.set).toEqual(SUNDAYS);
  await expect(detail.locator("#d-sched")).toContainText("Every Sunday");
});

test("the typed field says what it could not read, and Back leaves without saving", async ({ page, hub }) => {
  seedFromDone(hub);
  await openApp(page);
  const detail = await openDetail(page, "Water the plants");
  const before = hub.posted.length;
  await detail.locator("#d-sched").click();
  const typed = detail.getByRole("textbox", { name: "Type when" });
  await typed.fill("every sunday 12pm please");
  await expect(detail.locator("#sched-readout")).toHaveText('Could not read "please". Try every sunday 12pm, or tomorrow 9am.');
  await expect(typed).toHaveAttribute("aria-invalid", "true");
  // The controls keep what the task has.
  await expect(detail.getByLabel("Time", { exact: true })).toHaveValue("18:00");
  await typed.press("Enter");
  await expect(detail.getByRole("alert")).toContainText('Could not read "please"');
  await expect(typed).toBeFocused();
  expect(hub.posted.length).toBe(before);

  // A control changed after typing clears the typed text: the readout follows the controls.
  await detail.getByLabel("Time", { exact: true }).fill("19:00");
  await expect(typed).toHaveValue("");
  await expect(detail.locator("#sched-readout")).toHaveText("Today at 19:00, then 1 week after you finish it.");

  await page.keyboard.press("Escape");
  await expect(detail.getByRole("heading", { name: "Schedule" })).toHaveCount(0);
  await expect(detail.locator("#d-sched")).toBeFocused();
  await expect(detail.locator("#d-sched")).toContainText("Today at 18:00");

  await detail.locator("#d-sched").click();
  await detail.getByRole("button", { name: "Back to the task" }).click();
  await expect(detail.locator("#d-sched")).toBeFocused();
  expect(hub.posted.length).toBe(before);
});

test("moving a repeating task asks: just this one snoozes it, from now on moves the schedule", async ({ page, hub }) => {
  seedWeekly(hub);
  await openApp(page);
  const detail = await openDetail(page, "Water the plants");
  const before = hub.posted.length;

  await detail.locator("#d-sched").click();
  await detail.getByRole("textbox", { name: "Type when" }).fill("tomorrow 9am");
  await expect(detail.locator("#sched-readout")).toHaveText("Every Wednesday at 9:00. Next: Tomorrow.");
  await detail.getByRole("button", { name: "Save", exact: true }).click();
  const ask = detail.getByRole("group", { name: /Move only this one/ });
  await expect(ask).toBeVisible();
  await expect(detail.locator("#ask-h")).toBeFocused();
  expect(hub.posted.length).toBe(before);
  await axe(page);
  await ask.getByRole("button", { name: "Just this one" }).click();
  await expect.poll(() => hub.posted.at(-1)).toMatchObject({ kind: "snooze", key: KEY, payload: { until: "2026-10-15T09:00:00.000Z", previous: null } });
  expect(hub.posted.length).toBe(before + 1);
  expect(hub.find(KEY)).toMatchObject({ dueAt: "2026-10-14T18:00:00Z", repeat: { weekdays: [3] } });
  await expect(page.locator("#toast")).toContainText("Snoozed to");

  // The sheet stays on the task. From now on is the edit of the due time.
  await expect(detail.locator("#d-sched")).toContainText("Snoozed until Tomorrow 9:00");
  await detail.locator("#d-sched").click();
  await detail.getByLabel("Date", { exact: true }).fill("2026-10-16");
  await detail.getByRole("button", { name: "Save", exact: true }).click();
  await detail.getByRole("button", { name: "From now on" }).click();
  await expect.poll(() => hub.posted.at(-1)?.payload.set).toEqual({ due: "2026-10-16T18:00:00.000Z" });
  await expect(detail.locator("#d-sched")).toBeFocused();
});

test("a repeat counted from done just moves, with no question", async ({ page, hub }) => {
  seedFromDone(hub);
  await openApp(page);
  const detail = await openDetail(page, "Water the plants");
  await detail.locator("#d-sched").click();
  await detail.getByRole("textbox", { name: "Type when" }).fill("tomorrow 9am");
  await detail.getByRole("button", { name: "Save", exact: true }).click();
  await expect.poll(() => hub.posted.at(-1)?.payload.set).toEqual({ due: "2026-10-15T09:00:00.000Z" });
  await expect(detail.getByRole("button", { name: "Just this one" })).toHaveCount(0);
});

test("Snooze opens as a page in the sheet, never a second sheet", async ({ page, hub }) => {
  await openApp(page);
  const detail = await openDetail(page, "Water the plants");
  await expect(detail.getByRole("heading", { name: "Snooze" })).toHaveCount(0);
  await detail.getByRole("button", { name: "Snooze", exact: true }).click();
  await expect(detail.getByRole("heading", { name: "Snooze" })).toBeVisible();
  await expect(page.getByRole("dialog", { name: "Snooze" })).toHaveCount(0);
  await expect(detail.locator(".snz").first()).toBeFocused();
  await expect(detail.locator(".d-bar")).toHaveCount(0);
  await axe(page);
  await page.keyboard.press("Escape");
  await expect(detail.getByRole("button", { name: "Snooze", exact: true })).toBeFocused();

  await detail.getByRole("button", { name: "Snooze", exact: true }).click();
  await detail.locator(".snz").first().click();
  await expect.poll(() => hub.posted.at(-1)?.kind).toBe("snooze");
  await expect(detail.getByRole("heading", { name: "Snooze" })).toHaveCount(0);
});

test.describe("phone", () => {
  test.use({ viewport: { width: 375, height: 740 }, hasTouch: true, isMobile: true });

  test("Sundays at 12:00 from the sheet, and the action bar stays pinned", async ({ page, hub }) => {
    seedFromDone(hub);
    await openApp(page);
    await row(page, "Water the plants").locator(".row-title").tap();
    const detail = page.locator("#detail");
    await page.waitForTimeout(300);
    await expect(detail.getByRole("button", { name: "Done", exact: true })).toBeInViewport();
    await detail.locator("#d-sched").tap();
    await detail.getByRole("textbox", { name: "Type when" }).fill("every sunday 12pm");
    await detail.getByRole("button", { name: "Save", exact: true }).tap();
    await expect.poll(() => hub.posted.at(-1)?.payload.set).toEqual(SUNDAYS);
  });
});

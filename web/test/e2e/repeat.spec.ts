import { test, expect, openApp, row, axe } from "./setup.ts";
import type { Page } from "@playwright/test";

test.use({ viewport: { width: 1280, height: 800 } });

const KEY = "hub/personal/pe-1";

async function openDetail(page: Page, title: string) {
  await row(page, title).locator(".row-body").click();
  await expect(page.locator("#detail #d-title")).toHaveText(title);
  return page.locator("#detail");
}

test("quick add reads a repeat on the personal list and shows it", async ({ page, hub }) => {
  await openApp(page);
  const input = page.getByRole("textbox", { name: "New task" });
  await input.click();
  await input.pressSequentially("Feed the cat every day at 8:30pm");
  await expect(page.locator("#qa-chips .chip")).toHaveText(["Due 20:30", "Every day"]);
  await expect(page.locator("#qa-backdrop mark")).toHaveText(["every day", "at 8:30pm"]);
  await input.press("Enter");
  await expect(input).toHaveValue("");
  expect(hub.posted.at(-1)).toMatchObject({
    kind: "add",
    bridge: "hub",
    repo: "personal",
    payload: { title: "Feed the cat", due: "2026-10-14T20:30:00.000Z", repeat: { every: "day", timeZone: "UTC" } },
  });
  await expect(row(page, "Feed the cat").locator(".chip-repeat")).toHaveText("Every day");

  // A repeat with no time starts on its first day at 9:00.
  await input.pressSequentially("Take out the bins every mon and thu");
  await expect(page.locator("#qa-chips .chip").first()).toHaveText("Due Tomorrow 9:00");
  await expect(page.locator("#qa-chips .chip-repeat")).toHaveText("Every week on Mon, Thu");
  await input.press("Enter");
  expect(hub.posted.at(-1)!.payload).toEqual({
    title: "Take out the bins",
    due: "2026-10-15T09:00:00.000Z",
    repeat: { every: "week", weekdays: [1, 4], timeZone: "UTC" },
  });
});

test("quick add reads an end date after the repeat, and holds back one before the first date", async ({ page, hub }) => {
  await openApp(page);
  const input = page.getByRole("textbox", { name: "New task" });
  await input.click();
  await input.pressSequentially("Water the ferns every sunday until nov 26");
  await expect(page.locator("#qa-chips .chip")).toHaveText(["Due Sun, Oct 18 9:00", "Every week on Sun until Nov 26"]);
  await expect(page.locator("#qa-backdrop mark")).toHaveText(["every sunday", "until nov 26"]);
  await input.press("Enter");
  await expect(input).toHaveValue("");
  expect(hub.posted.at(-1)!.payload).toEqual({
    title: "Water the ferns",
    due: "2026-10-18T09:00:00.000Z",
    repeat: { every: "week", weekdays: [0], until: "2026-11-26", timeZone: "UTC" },
  });
  await expect(row(page, "Water the ferns").locator(".chip-repeat")).toHaveText("Every week on Sun until Nov 26");

  const before = hub.posted.length;
  await input.pressSequentially("Stretch daily until today");
  await expect(page.locator("#qa-chips .chip-red")).toHaveText("Ends before it starts");
  await input.press("Enter");
  await expect(page.locator("#toast")).toContainText("The end date is before the first date.");
  await expect(input).toHaveValue("Stretch daily until today");
  expect(hub.posted.length).toBe(before);
});

test("quick add leaves repeat words alone on an agent's list", async ({ page, hub }) => {
  await openApp(page);
  const input = page.getByRole("textbox", { name: "New task" });
  await input.click();
  await input.pressSequentially("Check the hoses every week #garden");
  await expect(page.locator("#qa-chips .chip-repeat")).toHaveCount(0);
  await expect(page.locator("#qa-backdrop mark.hl-repeat")).toHaveCount(0);
  await input.press("Enter");
  await expect.poll(() => hub.posted.at(-1)).toMatchObject({ kind: "add", bridge: "home", repo: "garden" });
  const add = hub.posted.at(-1)!;
  expect(add.payload.repeat).toBeUndefined();
  expect(add.payload.title).toContain("every week");
});

test("Done moves a repeating item to its next date, and Undo moves it back", async ({ page, hub }) => {
  const it = hub.find(KEY)!;
  it.repeat = { every: "day", timeZone: "UTC", start: it.dueAt! };
  it.checklist = [{ id: "k1", text: "Fill the can", done: true }];
  await openApp(page);
  await expect(row(page, "Water the plants").locator(".chip-repeat")).toHaveText("Every day");

  await row(page, "Water the plants").getByRole("button", { name: "Mark done: Water the plants" }).click();
  await expect(page.locator("#toast")).toContainText("Done. Next: Tomorrow 9:25");
  expect(hub.find(KEY)).toMatchObject({ status: "open", dueAt: "2026-10-15T09:25:00.000Z", checklist: [{ id: "k1", done: false }] });
  await expect(page.locator(".group-tomorrow")).toContainText("Water the plants");
  await expect(page.locator(".group-done")).not.toContainText("Water the plants");

  await page.locator("#toast").getByRole("button", { name: "Undo" }).click();
  await expect.poll(() => hub.posted.at(-1)).toMatchObject({
    kind: "edit",
    key: KEY,
    payload: { set: { due: "2026-10-14T09:25:00.000Z", checklist: [{ id: "k1", text: "Fill the can", done: true }] } },
  });
  expect(hub.posted.map((a) => a.kind)).toEqual(["done", "edit"]);
  await expect(page.locator(".group-pastdue")).toContainText("Water the plants");
  expect(hub.find(KEY)!.checklist![0].done).toBe(true);
});

test("the edit form sets, changes and ends a repeat", async ({ page, hub }) => {
  await openApp(page);
  const detail = await openDetail(page, "Call Sam about the weekend");
  const edit = detail.getByRole("button", { name: "Edit", exact: true });
  await edit.click();
  const repeat = detail.getByLabel("Repeat", { exact: true });
  await expect(repeat).toHaveValue("");
  await repeat.selectOption({ label: "Weekly" });
  await detail.getByLabel("Every").fill("2");
  await expect(detail.locator("#edit-unit")).toHaveText("weeks");
  await detail.getByRole("checkbox", { name: "Mon" }).check();
  await detail.getByRole("checkbox", { name: "Thu" }).check();
  await axe(page);
  await detail.getByRole("button", { name: "Save", exact: true }).click();
  await expect.poll(() => hub.posted.at(-1)?.payload.set).toEqual({ repeat: { every: "week", interval: 2, weekdays: [1, 4], timeZone: "UTC" } });
  await expect(detail.locator(".d-chips .chip-repeat")).toHaveText("Every 2 weeks on Mon, Thu");
  await expect(row(page, "Call Sam about the weekend").locator(".chip-repeat")).toHaveText("Every 2 weeks on Mon, Thu");

  // Counting from done drops the weekdays, which the hub does not take with it.
  await edit.click();
  await expect(repeat).toHaveValue("week");
  await expect(detail.getByLabel("Every")).toHaveValue("2");
  await detail.getByRole("checkbox", { name: "Count the next date from when it is done" }).check();
  await expect(detail.getByRole("checkbox", { name: "Mon" })).toHaveCount(0);
  await detail.getByRole("button", { name: "Save", exact: true }).click();
  await expect.poll(() => hub.posted.at(-1)?.payload.set).toEqual({ repeat: { every: "week", interval: 2, fromDone: true, timeZone: "UTC" } });
  await expect(detail.locator(".d-chips .chip-repeat")).toHaveText("Every 2 weeks after done");

  await edit.click();
  await repeat.selectOption({ label: "Does not repeat" });
  await detail.getByRole("button", { name: "Save", exact: true }).click();
  await expect.poll(() => hub.posted.at(-1)?.payload.set).toEqual({ clearRepeat: true });
  await expect(detail.locator(".chip-repeat")).toHaveCount(0);

  await expect(page.locator("#toast")).toContainText("Saved");
  await page.locator("#toast").getByRole("button", { name: "Undo" }).click();
  await expect.poll(() => hub.posted.at(-1)?.payload.set).toMatchObject({ repeat: { every: "week", interval: 2, fromDone: true } });
  await expect(detail.locator(".d-chips .chip-repeat")).toHaveText("Every 2 weeks after done");
});

test("the edit form sets and clears a repeat's end date", async ({ page, hub }) => {
  hub.find(KEY)!.repeat = { every: "day", timeZone: "UTC", start: hub.find(KEY)!.dueAt! };
  await openApp(page);
  const detail = await openDetail(page, "Water the plants");
  const edit = detail.getByRole("button", { name: "Edit", exact: true });
  await edit.click();
  const ends = detail.getByLabel("Ends");
  await expect(ends).toHaveValue("");

  // The due date is 14 October: an end before it is refused here.
  await ends.fill("2026-10-01");
  await detail.getByRole("button", { name: "Save", exact: true }).click();
  await expect(detail.getByRole("alert")).toHaveText("End the repeat on or after its due date.");
  await expect(ends).toBeFocused();

  await ends.fill("2026-11-26");
  await axe(page);
  await detail.getByRole("button", { name: "Save", exact: true }).click();
  await expect.poll(() => hub.posted.at(-1)?.payload.set).toEqual({ repeat: { every: "day", until: "2026-11-26", timeZone: "UTC" } });
  await expect(detail.locator(".d-chips .chip-repeat")).toHaveText("Every day until Nov 26");
  expect(hub.find(KEY)!.repeat).toMatchObject({ every: "day", until: "2026-11-26" });

  await edit.click();
  await expect(ends).toHaveValue("2026-11-26");
  await detail.getByRole("button", { name: "No end date" }).click();
  await expect(ends).toHaveValue("");
  await expect(ends).toBeFocused();
  await detail.getByRole("button", { name: "Save", exact: true }).click();
  await expect.poll(() => hub.posted.at(-1)?.payload.set).toEqual({ repeat: { every: "day", timeZone: "UTC" } });
  await expect(detail.locator(".d-chips .chip-repeat")).toHaveText("Every day");
  expect(hub.find(KEY)!.repeat!.until).toBeUndefined();

  // Undo puts the end date back with the whole repeat.
  await page.locator("#toast").getByRole("button", { name: "Undo" }).click();
  await expect.poll(() => hub.posted.at(-1)?.payload.set).toMatchObject({ repeat: { every: "day", until: "2026-11-26" } });
  await expect(detail.locator(".d-chips .chip-repeat")).toHaveText("Every day until Nov 26");

  // With no repeat there is no end date to set.
  await edit.click();
  await detail.getByLabel("Repeat", { exact: true }).selectOption({ label: "Does not repeat" });
  await expect(ends).toHaveCount(0);
});

test("Done after a repeat's last date closes the item, and Undo reopens it", async ({ page, hub }) => {
  const it = hub.find(KEY)!;
  it.repeat = { every: "day", until: "2026-10-14", timeZone: "UTC", start: it.dueAt! };
  await openApp(page);
  await row(page, "Water the plants").getByRole("button", { name: "Mark done: Water the plants" }).click();
  await expect(page.locator("#toast")).toContainText("Done: Water the plants");
  expect(hub.find(KEY)).toMatchObject({ status: "closed", repeat: { until: "2026-10-14" } });
  await page.locator("#toast").getByRole("button", { name: "Undo" }).click();
  await expect.poll(() => hub.posted.map((a) => a.kind)).toEqual(["done", "reopen"]);
  await expect(row(page, "Water the plants").locator(".chip-repeat")).toHaveText("Every day until Oct 14");
});

test("a repeat needs a due date, and clearing the due date ends it", async ({ page, hub }) => {
  hub.find(KEY)!.repeat = { every: "day", timeZone: "UTC", start: hub.find(KEY)!.dueAt! };
  await openApp(page);
  const before = hub.posted.length;

  const detail = await openDetail(page, "Buy seeds");
  await detail.getByRole("button", { name: "Edit", exact: true }).click();
  await detail.getByLabel("Repeat", { exact: true }).selectOption({ label: "Daily" });
  await detail.getByLabel("Every").fill("0");
  await detail.getByRole("button", { name: "Save", exact: true }).click();
  await expect(detail.getByRole("alert")).toHaveText("Repeat every 1 to 100.");
  await expect(detail.getByLabel("Every")).toBeFocused();
  await detail.getByLabel("Every").fill("1");
  await detail.getByRole("button", { name: "Save", exact: true }).click();
  await expect(detail.getByRole("alert")).toContainText("A repeat needs a due date");
  await expect(detail.getByLabel("Due")).toBeFocused();
  expect(hub.posted.length).toBe(before);
  await detail.getByLabel("Due").fill("tomorrow 7am");
  await detail.getByRole("button", { name: "Save", exact: true }).click();
  await expect.poll(() => hub.posted.at(-1)?.payload.set).toEqual({ due: "2026-10-15T07:00:00.000Z", repeat: { every: "day", timeZone: "UTC" } });

  await openDetail(page, "Water the plants");
  await detail.getByRole("button", { name: "Edit", exact: true }).click();
  await expect(detail.getByLabel("Repeat", { exact: true })).toHaveValue("day");
  await detail.getByRole("button", { name: "Clear" }).click();
  await expect(detail.getByLabel("Repeat", { exact: true })).toHaveValue("");
  await detail.getByRole("button", { name: "Save", exact: true }).click();
  await expect.poll(() => hub.posted.at(-1)?.payload.set).toEqual({ clearDue: true });
  expect(hub.find(KEY)!.repeat).toBeNull();
  await expect(row(page, "Water the plants").locator(".chip-repeat")).toHaveCount(0);
});

test("a personal item sets its own early reminder or uses the default", async ({ page, hub }) => {
  hub.reminderLead = 60;
  await openApp(page);
  const detail = await openDetail(page, "Water the plants");
  const edit = detail.getByRole("button", { name: "Edit", exact: true });
  await edit.click();
  const lead = detail.getByLabel("Remind me early");
  await expect(lead.locator("option:checked")).toHaveText("Use default (1 hour before)");
  await lead.selectOption({ label: "30 minutes before" });
  await detail.getByRole("button", { name: "Save", exact: true }).click();
  await expect.poll(() => hub.posted.at(-1)?.payload.set).toEqual({ remindBefore: 30 });
  expect(hub.find(KEY)!.remindBefore).toBe(30);

  await edit.click();
  await expect(lead).toHaveValue("30");
  await lead.selectOption({ label: "No early reminder" });
  await detail.getByRole("button", { name: "Save", exact: true }).click();
  await expect.poll(() => hub.posted.at(-1)?.payload.set).toEqual({ remindBefore: 0 });

  await edit.click();
  await lead.selectOption({ label: "Use default (1 hour before)" });
  await detail.getByRole("button", { name: "Save", exact: true }).click();
  await expect.poll(() => hub.posted.at(-1)?.payload.set).toEqual({ clearRemindBefore: true });
  expect(hub.find(KEY)!.remindBefore).toBeNull();
});

test.describe("Settings", () => {
  async function openSettings(page: Page) {
    await openApp(page);
    await page.locator(".sidebar").getByRole("button", { name: "Settings" }).click();
  }

  test("the early reminder setting saves the hub's lead time", async ({ page, hub }) => {
    hub.reminderLead = 15;
    await openSettings(page);
    const section = page.locator("[data-section=lead]");
    await expect(section.locator("h2")).toHaveText("Early reminder");
    const lead = section.getByLabel("Remind me early");
    await expect(lead).toHaveValue("15");
    await lead.selectOption({ label: "1 day before" });
    await expect(page.locator("#toast")).toContainText("Saved");
    expect(hub.settingsPuts.at(-1)).toEqual({ reminderLead: 1440 });
    expect(hub.reminderLead).toBe(1440);
    await lead.selectOption({ label: "No early reminder" });
    await expect.poll(() => hub.reminderLead).toBe(0);
    await axe(page);
  });

  test("the calendar link: made, shown once, replaced after asking, and turned off", async ({ page, hub }) => {
    await openSettings(page);
    const section = page.locator("[data-section=calendar]");
    await expect(section.locator("h2")).toHaveText("Calendar");
    await expect(section).toContainText("Google Calendar and Outlook");
    await expect(section).toContainText("from the internet");
    await axe(page);

    await section.getByRole("button", { name: "Get calendar link" }).click();
    const url = section.locator("#cal-url");
    await expect(url).toHaveText(/\/calendar\/feed-token-1\.ics$/);
    await expect(section.getByRole("button", { name: "Copy" })).toBeFocused();
    expect(hub.calendar.on).toBe(true);
    await axe(page);

    await section.getByRole("button", { name: "Done" }).click();
    await expect(url).toHaveCount(0);
    const newLink = section.getByRole("button", { name: "New link" });
    await expect(newLink).toBeFocused();

    await newLink.click();
    await expect(section).toContainText("The current link stops working");
    await expect(section.getByRole("button", { name: "Cancel" })).toBeFocused();
    await section.getByRole("button", { name: "Cancel" }).click();
    await expect(newLink).toBeFocused();
    expect(hub.calendarLinks).toBe(1);

    await newLink.click();
    await section.getByRole("button", { name: "Make a new link" }).click();
    await expect(url).toHaveText(/\/calendar\/feed-token-2\.ics$/);

    // The link is shown once: leaving Settings forgets it.
    await page.locator(".sidebar").getByRole("button", { name: "Today" }).click();
    await page.locator(".sidebar").getByRole("button", { name: "Settings" }).click();
    await expect(url).toHaveCount(0);

    await section.getByRole("button", { name: "Turn off" }).click();
    await expect(section.getByRole("button", { name: "Get calendar link" })).toBeFocused();
    expect(hub.calendar.on).toBe(false);
  });

  test("the calendar section shows the link is on after a reload", async ({ page, hub }) => {
    hub.calendar = { on: true, token: "feed-token-1" };
    await openSettings(page);
    const section = page.locator("[data-section=calendar]");
    await expect(section.getByRole("button", { name: "New link" })).toBeVisible();
    await expect(section.getByRole("button", { name: "Turn off" })).toBeVisible();
    await expect(section.locator("#cal-url")).toHaveCount(0);
  });
});

test.describe("phone", () => {
  test.use({ viewport: { width: 375, height: 740 } });

  test("the repeat fields fit the edit sheet", async ({ page, hub }) => {
    hub.find("hub/personal/pe-3")!.remindBefore = 45;
    await openApp(page);
    const detail = await openDetail(page, "Call Sam about the weekend");
    await detail.getByRole("button", { name: "Edit", exact: true }).click();
    await detail.getByLabel("Repeat", { exact: true }).selectOption({ label: "Weekly" });
    await expect(detail.getByRole("checkbox", { name: "Sun" })).toBeVisible();
    await detail.getByLabel("Ends").fill("2026-11-26");
    await expect(detail.getByRole("button", { name: "No end date" })).toBeVisible();
    await expect(detail.getByLabel("Remind me early").locator("option:checked")).toHaveText("45 minutes before");
    const [scroll, inner] = await page.evaluate(() => {
      const el = document.querySelector("#detail .detail-inner")!;
      return [el.scrollWidth, el.clientWidth];
    });
    expect(scroll).toBeLessThanOrEqual(inner);
    await axe(page);
  });
});

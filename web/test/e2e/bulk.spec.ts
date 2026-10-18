import { test, expect, openApp, row, axe } from "./setup.ts";
import type { Page } from "@playwright/test";

test.use({ viewport: { width: 1280, height: 800 } });

const bar = (page: Page) => page.getByRole("toolbar", { name: "Selected tasks" });
const pick = (page: Page, title: string) => page.getByRole("checkbox", { name: `Select: ${title}` });

test("Done on two picked items, one repeating, and one Undo brings both back", async ({ page, hub }) => {
  const water = hub.find("hub/personal/pe-1")!;
  water.repeat = { every: "day", timeZone: "UTC", start: water.dueAt! };
  await openApp(page);
  const select = page.getByRole("button", { name: "Select", exact: true });
  await select.click();
  await expect(select).toHaveAttribute("aria-pressed", "true");
  await expect(bar(page)).toBeVisible();
  await expect(bar(page)).toContainText("0 selected");
  await expect(bar(page).getByRole("button", { name: "Done" })).toBeDisabled();
  // The closed row has nothing to pick.
  await expect(row(page, "Sweep the porch").getByRole("checkbox")).toHaveCount(0);
  await axe(page);

  // A tap on the checkbox or on the row itself picks, and opens nothing.
  await pick(page, "Water the plants").click();
  await row(page, "Call Sam about the weekend").locator(".row-body").click();
  await expect(page.locator("#detail #d-title")).toHaveCount(0);
  await expect(pick(page, "Water the plants")).toHaveAttribute("aria-checked", "true");
  await expect(pick(page, "Call Sam about the weekend")).toHaveAttribute("aria-checked", "true");
  await expect(bar(page)).toContainText("2 selected");
  await expect(bar(page).getByRole("button", { name: "Delete", exact: true })).toBeEnabled();

  await bar(page).getByRole("button", { name: "Done" }).click();
  await expect(page.locator("#toast")).toContainText("Done: 2 tasks");
  await expect(bar(page)).toBeHidden();
  await expect(page.getByRole("checkbox")).toHaveCount(0);
  expect(hub.find("hub/personal/pe-3")!.status).toBe("closed");
  expect(hub.find("hub/personal/pe-1")).toMatchObject({ status: "open", dueAt: "2026-10-15T09:25:00.000Z" });

  await page.locator("#toast").getByRole("button", { name: "Undo" }).click();
  await expect.poll(() => hub.find("hub/personal/pe-3")!.status).toBe("open");
  await expect.poll(() => hub.find("hub/personal/pe-1")!.dueAt).toBe("2026-10-14T09:25:00.000Z");
  expect(hub.posted.map((a) => a.kind)).toEqual(["done", "done", "edit", "reopen"]);
  await expect(page.locator(".group-pastdue")).toContainText("Water the plants");
  await expect(page.locator(".group-today")).toContainText("Call Sam about the weekend");
});

test("the dismiss button names what the picks are, and dismissing a mix undoes together", async ({ page, hub }) => {
  await openApp(page);
  await page.getByRole("button", { name: "Select", exact: true }).click();
  await pick(page, "Order compost for the beds").click();
  await expect(bar(page).getByRole("button", { name: "Won't do" })).toBeVisible();
  await pick(page, "Renew the library card").click();
  const dismiss = bar(page).getByRole("button", { name: "Delete or won't do" });
  await expect(dismiss).toBeVisible();
  await pick(page, "Order compost for the beds").click();
  await expect(bar(page).getByRole("button", { name: "Delete", exact: true })).toBeVisible();
  await pick(page, "Order compost for the beds").click();

  await dismiss.click();
  await expect(page.locator("#toast")).toContainText("Deleted or won't do: 2 tasks");
  await expect(row(page, "Renew the library card")).toHaveCount(0);
  expect(hub.posted.map((a) => [a.kind, a.key])).toEqual([
    ["dismiss", "hub/personal/pe-2"],
    ["dismiss", "home/garden/ga-1"],
  ]);
  await page.locator("#toast").getByRole("button", { name: "Undo" }).click();
  await expect(row(page, "Renew the library card")).toBeVisible();
  await expect.poll(() => hub.find("home/garden/ga-1")!.status).toBe("open");
});

test("Select all and Snooze… snooze the picks to a choice", async ({ page, hub }) => {
  await openApp(page);
  const select = page.getByRole("button", { name: "Select", exact: true });
  await select.click();
  await bar(page).getByRole("button", { name: "Select all" }).click();
  const boxes = page.getByRole("checkbox");
  await expect(boxes).toHaveCount(7);
  await expect(bar(page)).toContainText("7 selected");
  for (const b of await boxes.all()) await expect(b).toHaveAttribute("aria-checked", "true");
  await bar(page).getByRole("button", { name: "Cancel" }).click();

  await select.click();
  await pick(page, "Renew the library card").click();
  await pick(page, "Water the plants").click();
  await expect(bar(page)).toContainText("2 selected");

  await bar(page).getByRole("button", { name: "Snooze…" }).click();
  const dlg = page.getByRole("dialog", { name: "Snooze" });
  await expect(dlg).toContainText("2 tasks");
  await dlg.getByRole("button", { name: /Tomorrow/ }).first().click();
  await expect(page.locator("#toast")).toContainText("Snoozed 2 tasks to");
  await expect(bar(page)).toBeHidden();
  expect(hub.posted.map((a) => [a.kind, a.key])).toEqual([
    ["snooze", "hub/personal/pe-2"],
    ["snooze", "hub/personal/pe-1"],
  ]);
  expect(hub.find("hub/personal/pe-1")!.deferUntil).toBeTruthy();
});

test("Esc, Cancel and another view leave select mode; pages have no Select", async ({ page, hub }) => {
  void hub;
  await openApp(page);
  const select = page.getByRole("button", { name: "Select", exact: true });
  await select.click();
  await pick(page, "Water the plants").click();
  await page.keyboard.press("Escape");
  await expect(bar(page)).toBeHidden();
  await expect(select).toHaveAttribute("aria-pressed", "false");
  await expect(row(page, "Water the plants")).not.toHaveClass(/is-picked/);
  await expect(row(page, "Water the plants").getByRole("button", { name: "Mark done: Water the plants" })).toBeVisible();

  await select.click();
  await bar(page).getByRole("button", { name: "Cancel" }).click();
  await expect(bar(page)).toBeHidden();
  await expect(select).toBeFocused();

  await select.click();
  await page.locator(".sidebar").getByRole("button", { name: /Upcoming/ }).click();
  await expect(bar(page)).toBeHidden();
  await expect(select).toBeVisible();
  for (const name of ["Completed", "Deleted", "Settings"]) {
    await page.locator(".sidebar").getByRole("button", { name: new RegExp(name) }).first().click();
    await expect(select).toBeHidden();
  }
});

test.describe("phone", () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });

  test("the bar sits above the tab bar, swipe is off, and nothing scrolls sideways", async ({ page, hub }) => {
    void hub;
    await openApp(page);
    await page.getByRole("button", { name: "Select", exact: true }).click();
    await pick(page, "Water the plants").tap();
    await pick(page, "Order compost for the beds").tap();
    await expect(bar(page)).toContainText("2 selected");
    await expect(page.locator(".fab")).toBeHidden();
    const b = (await bar(page).boundingBox())!;
    const tabs = (await page.locator("#tabs").boundingBox())!;
    expect(b.y + b.height).toBeLessThanOrEqual(tabs.y + 1);
    for (const btn of await bar(page).getByRole("button").all()) {
      const r = (await btn.boundingBox())!;
      expect(r.x).toBeGreaterThanOrEqual(0);
      expect(r.x + r.width).toBeLessThanOrEqual(390);
    }
    const [scroll, inner] = await page.evaluate(() => [document.documentElement.scrollWidth, window.innerWidth]);
    expect(scroll).toBeLessThanOrEqual(inner);
    await expect(bar(page).getByRole("button", { name: "Delete or won't do" })).toBeVisible();

    // A drag on a row does nothing in select mode.
    const fg = row(page, "Call Sam about the weekend").locator(".row-fg");
    const box = (await fg.boundingBox())!;
    const y = box.y + box.height / 2;
    const fire = (type: string, x: number) =>
      fg.dispatchEvent(type, { pointerId: 7, pointerType: "touch", isPrimary: true, clientX: x, clientY: y, button: 0, bubbles: true });
    await fire("pointerdown", box.x + 100);
    for (let i = 1; i <= 6; i++) await fire("pointermove", box.x + 100 + i * 30);
    await expect(row(page, "Call Sam about the weekend")).not.toHaveAttribute("data-swipe", /./);
    await fire("pointerup", box.x + 280);
    expect(hub.posted).toEqual([]);
    await axe(page);
  });
});

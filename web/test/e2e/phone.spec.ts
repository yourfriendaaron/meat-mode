import { test, expect, openApp, row, axe } from "./setup.ts";
import type { Locator, Page } from "@playwright/test";

test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });

async function noHorizontalScroll(page: Page) {
  const [scroll, inner] = await page.evaluate(() => [document.documentElement.scrollWidth, window.innerWidth]);
  expect(scroll).toBeLessThanOrEqual(inner);
}

/** Drags a row horizontally with pointer events, checking the label shown underneath mid-drag. */
async function swipe(page: Page, target: Locator, dx: number, label: string) {
  const box = (await target.locator(".row-fg").boundingBox())!;
  const y = box.y + box.height / 2;
  const x0 = box.x + box.width / 2;
  const fg = target.locator(".row-fg");
  const fire = (type: string, x: number) =>
    fg.dispatchEvent(type, { pointerId: 7, pointerType: "touch", isPrimary: true, clientX: x, clientY: y, button: 0, bubbles: true });
  await fire("pointerdown", x0);
  for (let i = 1; i <= 6; i++) await fire("pointermove", x0 + (dx * i) / 6);
  await expect(target.locator(".under")).toBeVisible();
  await expect(target.locator(label === "Done" ? ".u-done" : ".u-snooze")).toBeVisible();
  await expect(target.locator(label === "Done" ? ".u-snooze" : ".u-done")).toBeHidden();
  await fire("pointerup", x0 + dx);
}

test("tabs switch views; Browse shows lists and settings", async ({ page, hub }) => {
  void hub;
  await openApp(page);
  await expect(page.locator(".sidebar")).toBeHidden();
  const tabs = page.locator("#tabs");
  await expect(tabs).toBeVisible();
  await expect(page.locator(".mhead .sync-pill")).toHaveText("Synced");
  await noHorizontalScroll(page);

  await tabs.getByRole("button", { name: "Upcoming" }).click();
  await expect(page.locator("#view-title")).toHaveText("Upcoming");
  await noHorizontalScroll(page);

  await expect(tabs.getByRole("button", { name: /Agents/ }).locator(".tab-badge")).toHaveText("2");
  await tabs.getByRole("button", { name: /Agents/ }).click();
  await expect(page.locator("#view-title")).toHaveText("From agents");
  await noHorizontalScroll(page);

  await tabs.getByRole("button", { name: "Browse" }).click();
  await expect(page.locator("#view-title")).toHaveText("Everything");
  const chips = page.locator("#browse");
  await expect(chips.getByRole("button")).toHaveText(["Everything", "personal", "garden", "website", "Completed", "Deleted", "Settings"]);
  await chips.getByRole("button", { name: "garden" }).click();
  await expect(page.locator("#view-title")).toHaveText("garden");
  await expect(tabs.getByRole("button", { name: "Browse" })).toHaveAttribute("aria-current", "page");
  await noHorizontalScroll(page);

  await chips.getByRole("button", { name: "Settings" }).click();
  await expect(page.getByRole("heading", { name: "Reminders" })).toBeVisible();
  await noHorizontalScroll(page);
  await axe(page);
});

test("detail opens in a bottom sheet with a scrim", async ({ page, hub }) => {
  void hub;
  await openApp(page);
  await row(page, "Order compost for the beds").locator(".row-body").click();
  const sheet = page.locator("#detail");
  await expect(sheet.getByRole("heading", { name: "Order compost for the beds" })).toBeInViewport();
  await expect(page.locator("#scrim")).toBeVisible();
  await noHorizontalScroll(page);
  await axe(page);
  await page.locator("#scrim").click({ position: { x: 20, y: 20 } });
  await expect(page.locator("#scrim")).toBeHidden();
  await expect(sheet).toBeHidden();

  await row(page, "Water the plants").locator(".row-body").click();
  await sheet.getByRole("button", { name: "Close details" }).click();
  await expect(sheet).toBeHidden();
});

test("the detail head stays in view when the sheet scrolls to the note", async ({ page, hub }) => {
  void hub;
  await openApp(page);
  await row(page, "Order compost for the beds").locator(".row-body").click();
  const sheet = page.locator("#detail");
  await sheet.evaluate((el) => Promise.all(el.getAnimations({ subtree: true }).map((a) => a.finished)));
  await sheet.evaluate((el) => (el.scrollTop = el.scrollHeight));
  await expect.poll(() => sheet.evaluate((el) => el.scrollTop)).toBeGreaterThan(0);
  const box = (await sheet.boundingBox())!;
  const close = (await sheet.getByRole("button", { name: "Close details" }).boundingBox())!;
  expect(close.y).toBeGreaterThanOrEqual(box.y);
  expect(close.y + close.height).toBeLessThanOrEqual(box.y + box.height);
  await sheet.getByRole("button", { name: "Close details" }).click();
  await expect(sheet).toBeHidden();
});

test("swipe right marks done, swipe left opens snooze", async ({ page, hub }) => {
  await openApp(page);
  await swipe(page, row(page, "Water the plants"), 160, "Done");
  await expect(page.locator(".group-done")).toContainText("Water the plants");
  await expect.poll(() => hub.posted.at(-1)).toMatchObject({ kind: "done", key: "hub/personal/pe-1" });
  await expect(page.locator("#toast")).toContainText("Done: Water the plants");

  await swipe(page, row(page, "Call Sam about the weekend"), -160, "Snooze");
  const dialog = page.getByRole("dialog", { name: "Snooze" });
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: /This evening/ }).click();
  await expect.poll(() => hub.posted.at(-1)).toMatchObject({ kind: "snooze", key: "hub/personal/pe-3", payload: { until: "2026-10-14T18:00:00.000Z" } });
  await expect(row(page, "Call Sam about the weekend").locator(".chip-orange")).toHaveText("18:00");

  // A short drag springs back and does nothing.
  const before = hub.posted.length;
  await swipe(page, row(page, "Renew the library card"), 40, "Done");
  await expect(page.getByRole("dialog", { name: "Snooze" })).toBeHidden();
  expect(hub.posted.length).toBe(before);
  await expect(row(page, "Renew the library card").locator(".under")).toBeHidden();
  await noHorizontalScroll(page);
});

test("quick add opens in a sheet from +", async ({ page, hub }) => {
  await openApp(page);
  await expect(page.locator("#qa-form")).toBeHidden();
  await page.getByRole("button", { name: "New task" }).click();
  const input = page.getByRole("textbox", { name: "New task" });
  await expect(input).toBeVisible();
  await expect(input).toBeFocused();
  await input.fill("Water the plants tonight");
  await expect(page.locator("#qa-chips")).toContainText("Due 20:00");
  await expect(page.locator("#qa-list")).toHaveValue("hub/personal");
  await noHorizontalScroll(page);
  await page.locator("#qa-form").getByRole("button", { name: "Add" }).click();
  await expect(page.locator("#qa-form")).toBeHidden();
  await expect.poll(() => hub.posted.at(-1)).toMatchObject({
    kind: "add",
    bridge: "hub",
    repo: "personal",
    payload: { title: "Water the plants", due: "2026-10-14T20:00:00.000Z" },
  });
  await expect(page.locator(".group-evening")).toContainText("Water the plants");
});

test("light theme on phone passes axe", async ({ page, hub }) => {
  void hub;
  await page.addInitScript(() => localStorage.setItem("meat-mode.theme", "light"));
  await openApp(page);
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  await axe(page);
});

test("login screen fits the phone in both themes, and Log out is in Settings", async ({ page, hub }) => {
  hub.login = true;
  await page.goto("/");
  const pw = page.getByLabel("Password");
  await expect(pw).toBeVisible();
  await noHorizontalScroll(page);
  await axe(page);
  await page.evaluate(() => localStorage.setItem("meat-mode.theme", "light"));
  await page.reload();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  await axe(page);

  await pw.fill(hub.password);
  await page.getByRole("button", { name: "Log in" }).click();
  await expect(row(page, "Water the plants")).toBeVisible();
  await page.locator("#tabs").getByRole("button", { name: "Browse" }).click();
  await page.locator("#browse").getByRole("button", { name: "Settings" }).click();
  await page.getByRole("button", { name: "Log out" }).click();
  await expect(pw).toBeVisible();
  await noHorizontalScroll(page);
});

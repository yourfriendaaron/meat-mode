import { test, expect, openMenu, openApp, row } from "./setup.ts";

test.describe("desktop", () => {
  test.use({ viewport: { width: 1280, height: 800 } });

  test("Completed shows a done item under its day, and Reopen brings it back", async ({ page, hub }) => {
    await openApp(page);
    await page.locator(".sidebar").getByRole("button", { name: "Completed" }).click();
    await expect(page.locator("#view-title")).toHaveText("Completed");
    const today = page.locator(".group-day-2026-10-14");
    await expect(today.getByRole("heading", { name: /^Today/ })).toBeVisible();
    await expect(today).toContainText("Sweep the porch");

    await row(page, "Sweep the porch").getByRole("button", { name: "Reopen", exact: true }).click();
    await expect(page.locator(".empty")).toHaveText("Nothing completed in the last 7 days.");
    await expect.poll(() => hub.posted.at(-1)).toMatchObject({ kind: "reopen", key: "hub/personal/pe-6" });
    await page.locator(".sidebar").getByRole("button", { name: "Everything" }).click();
    await expect(row(page, "Sweep the porch")).toHaveCount(1);
  });

  test("a deleted item is listed in Deleted, Restore brings it back, Undo deletes it again", async ({ page, hub }) => {
    await openApp(page);
    await row(page, "Call Sam about the weekend").locator(".row-body").click();
    await page.locator("#detail").getByRole("button", { name: "Delete" }).click();
    await expect(row(page, "Call Sam about the weekend")).toHaveCount(0);

    await page.locator(".sidebar").getByRole("button", { name: "Completed" }).click();
    await expect(page.locator("#list")).not.toContainText("Call Sam about the weekend");

    await page.locator(".sidebar").getByRole("button", { name: "Deleted" }).click();
    await expect(page.locator("#view-title")).toHaveText("Deleted");
    await expect(page.locator("#list")).toContainText("Deleted items are removed 7 days after you delete them.");
    await expect(row(page, "Call Sam about the weekend")).toHaveCount(1);
    await expect(row(page, "Call Sam about the weekend").getByRole("button", { name: "Reopen", exact: true })).toHaveCount(0);

    await row(page, "Call Sam about the weekend").getByRole("button", { name: "Restore", exact: true }).click();
    await expect(page.locator("#toast")).toContainText("Restored: Call Sam about the weekend");
    await expect(page.locator(".empty")).toHaveText("Nothing deleted in the last 7 days.");
    await expect.poll(() => hub.posted.at(-1)).toMatchObject({ kind: "reopen", key: "hub/personal/pe-3" });

    await page.getByRole("button", { name: "Undo" }).click();
    await expect(row(page, "Call Sam about the weekend")).toHaveCount(1);
    await expect.poll(() => hub.posted.at(-1)).toMatchObject({ kind: "dismiss", key: "hub/personal/pe-3" });
  });

  test("a deleted item's details offer Restore, and x restores it from the list", async ({ page, hub }) => {
    await openApp(page);
    await row(page, "Call Sam about the weekend").locator(".row-body").click();
    await page.locator("#detail").getByRole("button", { name: "Delete" }).click();
    await page.locator(".sidebar").getByRole("button", { name: "Deleted" }).click();

    await row(page, "Call Sam about the weekend").locator(".row-body").click();
    const detail = page.locator("#detail");
    await expect(detail.getByRole("button", { name: "Restore", exact: true })).toBeVisible();
    await expect(detail.getByRole("button", { name: "Reopen", exact: true })).toHaveCount(0);
    await page.keyboard.press("Escape");

    await page.keyboard.press("x");
    await expect(page.locator("#toast")).toContainText("Restored: Call Sam about the weekend");
    await expect.poll(() => hub.posted.at(-1)).toMatchObject({ kind: "reopen", key: "hub/personal/pe-3" });
  });

  test("an agent's Won't do is in Completed with a chip", async ({ page, hub }) => {
    void hub;
    await openApp(page);
    await row(page, "Order compost for the beds").locator(".row-body").click();
    await page.locator("#detail").getByRole("button", { name: "Won't do" }).click();
    await page.locator(".sidebar").getByRole("button", { name: "Completed" }).click();
    await expect(row(page, "Order compost for the beds").locator(".chip-wontdo")).toHaveText("Won't do");
    await expect(row(page, "Order compost for the beds").getByRole("button", { name: "Reopen", exact: true })).toBeVisible();
  });
});

test.describe("phone", () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });

  test("the menu opens Completed and Deleted", async ({ page, hub }) => {
    void hub;
    await openApp(page);
    await (await openMenu(page)).getByRole("button", { name: "Completed" }).click();
    await expect(page.locator("#view-title")).toHaveText("Completed");
    await expect(row(page, "Sweep the porch")).toHaveCount(1);
    await (await openMenu(page)).getByRole("button", { name: "Deleted" }).click();
    await expect(page.locator("#view-title")).toHaveText("Deleted");
    await expect(page.locator(".empty")).toHaveText("Nothing deleted in the last 7 days.");
  });
});

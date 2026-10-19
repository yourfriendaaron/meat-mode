import { test, expect, openApp, axe, wizard, openWizardAt } from "./setup.ts";
import type { Page } from "@playwright/test";

/** An export as the hub's Settings downloads it; the fake hub only passes it on. */
const EXPORT = JSON.stringify({ format: "meat-mode-export", version: 1, items: [], actions: [] });

const RESULT = { items: 12, actions: 40, reminders: 3, cancelled: 1, machines: ["laptop", "garden-pi"], calendarFeed: true };

/** Answers POST /api/restore with status and body, and records what the page sent. */
async function answerRestore(page: Page, status: number, body: unknown): Promise<string[]> {
  const sent: string[] = [];
  await page.route("**/api/restore", async (route) => {
    sent.push(route.request().postData() ?? "");
    await route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
  });
  return sent;
}

async function choose(page: Page, text = EXPORT) {
  await page.getByLabel("Export file").setInputFiles({ name: "meat-mode-export-2026-10-14.json", mimeType: "application/json", buffer: Buffer.from(text) });
}

test.describe("restore step", () => {
  test.use({ viewport: { width: 1280, height: 800 } });

  test("comes after Secure the hub on a fresh hub, and Skip goes on", async ({ page, hub }) => {
    await openWizardAt(page, hub, "secure");
    const w = wizard(page);
    await w.next.click();
    await expect(w.heading).toHaveText("Restore from an export");
    await expect(page.getByLabel("Export file")).toBeVisible();
    await axe(page);
    await w.skip.click();
    await expect(w.heading).toHaveText("Name and time zone");
  });

  test("restores a file and lists what to do again", async ({ page, hub }) => {
    const sent = await answerRestore(page, 200, RESULT);
    await openWizardAt(page, hub, "restore");
    const w = wizard(page);
    await choose(page);
    await expect(w.region.getByRole("status")).toHaveText("Restored 12 to-dos, 40 changes in the history and 3 reminders already sent.");
    // The file goes to the hub as it was chosen.
    expect(sent).toEqual([EXPORT]);
    await expect(w.region).toContainText("1 change was still waiting for a machine and will not be sent.");
    const todo = w.region.locator(".restore-todo li");
    await expect(todo).toHaveCount(3);
    await expect(todo.nth(0)).toContainText("Pair each machine again, under the same name");
    await expect(todo.nth(0).locator("code")).toHaveText(["laptop", "garden-pi"]);
    await expect(todo.nth(1)).toHaveText("Turn the calendar feed on again in Settings, and give your calendars its new address.");
    await expect(todo.nth(2)).toHaveText("Turn notifications on, on each device.");
    await axe(page);
    await w.next.click();
    await expect(w.heading).toHaveText("Name and time zone");
    // Back shows what loaded, though the hub is no longer new.
    await w.back.click();
    await expect(w.heading).toHaveText("Restore from an export");
    await expect(w.region.getByRole("status")).toContainText("Restored 12 to-dos");
  });

  test("shows the hub's refusal of a hub with data", async ({ page, hub }) => {
    const refusal = "This hub already has data, so it cannot be restored into.";
    await answerRestore(page, 409, { error: refusal });
    await openWizardAt(page, hub, "restore");
    await choose(page);
    const w = wizard(page);
    await expect(w.region.getByRole("alert")).toHaveText(refusal);
    await expect(page.getByLabel("Export file")).toBeEnabled();
    await axe(page);
    // Skip still goes on with setup.
    await w.skip.click();
    await expect(w.heading).toHaveText("Name and time zone");
  });

  test("shows why a file was refused", async ({ page, hub }) => {
    await answerRestore(page, 400, { error: "This export is from a newer hub: the file is version 2, and this hub reads version 1 and older. Update the hub, then restore again." });
    await openWizardAt(page, hub, "restore");
    await choose(page);
    await expect(wizard(page).region.getByRole("alert")).toHaveText(
      "Not restored. This export is from a newer hub: the file is version 2, and this hub reads version 1 and older. Update the hub, then restore again.",
    );
  });

  test("is not offered on a hub in use", async ({ page, hub: _hub }) => {
    await openApp(page);
    await page.locator(".sidebar").getByRole("button", { name: "Settings" }).click();
    await page.getByRole("button", { name: "Run setup again" }).click();
    const w = wizard(page);
    await expect(w.heading).toHaveText("Welcome");
    await expect(w.region.locator(".wiz-steps")).not.toContainText("Restore");
  });
});

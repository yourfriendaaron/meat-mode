import { test, expect, openApp, axe, wizard, openWizardAt } from "./setup.ts";
import type { Page } from "@playwright/test";

/** An export as the hub's Settings downloads it, standing in for the zip; the fake hub only passes it on. */
const EXPORT = Buffer.from("PK\x03\x04 an export", "latin1");

const RESULT = { items: 12, actions: 40, reminders: 3, cancelled: 1, machines: ["laptop", "garden-pi"], calendarFeed: true, attachments: 5 };

/** Answers POST /api/restore with status and body, and records what the page sent and its content type. */
async function answerRestore(page: Page, status: number, body: unknown): Promise<{ body: Buffer; type: string }[]> {
  const sent: { body: Buffer; type: string }[] = [];
  await page.route("**/api/restore", async (route) => {
    const req = route.request();
    sent.push({ body: req.postDataBuffer() ?? Buffer.alloc(0), type: req.headers()["content-type"] });
    await route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
  });
  return sent;
}

async function choose(page: Page, file = { name: "meat-mode-export-2026-10-14.zip", mimeType: "application/zip", buffer: EXPORT }) {
  await page.getByLabel("Export file").setInputFiles(file);
}

test.describe("restore step", () => {
  test.use({ viewport: { width: 1280, height: 800 } });

  test("comes after Secure the hub on a fresh hub, and Skip goes on", async ({ page, hub }) => {
    await openWizardAt(page, hub, "secure");
    const w = wizard(page);
    await w.next.click();
    await expect(w.heading).toHaveText("Restore from an export");
    await expect(page.getByLabel("Export file")).toBeVisible();
    // The zip, or the JSON file an older hub gave.
    await expect(page.getByLabel("Export file")).toHaveAttribute("accept", ".zip,.json,application/zip,application/json");
    await axe(page);
    await w.skip.click();
    await expect(w.heading).toHaveText("Name and time zone");
  });

  test("restores a file and lists what to do again", async ({ page, hub }) => {
    const sent = await answerRestore(page, 200, RESULT);
    await openWizardAt(page, hub, "restore");
    const w = wizard(page);
    await choose(page);
    await expect(w.region.getByRole("status")).toHaveText("Restored 12 to-dos with 5 files, 40 changes in the history and 3 reminders already sent.");
    // The file goes to the hub as it was chosen, as bytes.
    expect(sent).toEqual([{ body: EXPORT, type: "application/octet-stream" }]);
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

  test("restores an older hub's JSON file, with no files to count", async ({ page, hub }) => {
    const json = JSON.stringify({ format: "meat-mode-export", version: 1, items: [], actions: [] });
    const sent = await answerRestore(page, 200, { ...RESULT, attachments: 0 });
    await openWizardAt(page, hub, "restore");
    await choose(page, { name: "meat-mode-export-2026-10-14.json", mimeType: "application/json", buffer: Buffer.from(json) });
    await expect(wizard(page).region.getByRole("status")).toHaveText("Restored 12 to-dos, 40 changes in the history and 3 reminders already sent.");
    expect(sent.map((x) => [x.body.toString(), x.type])).toEqual([[json, "application/octet-stream"]]);
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
    await answerRestore(page, 400, { error: "This export is from a newer hub: the file is version 3, and this hub reads version 2 and older. Update the hub, then restore again." });
    await openWizardAt(page, hub, "restore");
    await choose(page);
    await expect(wizard(page).region.getByRole("alert")).toHaveText(
      "Not restored. This export is from a newer hub: the file is version 3, and this hub reads version 2 and older. Update the hub, then restore again.",
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

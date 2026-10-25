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

function seedNotes(hub: FakeHub) {
  hub.find(KEY)!.comments = [
    { id: "n1", author: "alex-app", text: "Use the rain water.", createdAt: "2026-10-14T08:00:00Z" },
    { id: "n2", author: "alex-app", text: "The fern needs less.", createdAt: "2026-10-14T09:00:00Z" },
  ];
}

test("the title, details and priority each save on their own, and Undo puts them back", async ({ page, hub }) => {
  await openApp(page);
  const detail = await openDetail(page, "Call Sam about the weekend");
  // No edit mode: the sheet shows its fields as they are.
  await expect(detail.getByRole("button", { name: "Edit", exact: true })).toHaveCount(0);
  await expect(detail.locator("#d-sched")).toContainText("Today at 14:30");

  await detail.locator(".d-title-btn").click();
  const title = detail.getByRole("textbox", { name: "Title" });
  await expect(title).toBeFocused();
  await expect(title).toHaveValue("Call Sam about the weekend");
  await title.fill("Call Sam about Saturday");
  await title.press("Enter");
  await expect.poll(() => hub.posted.at(-1)).toEqual({
    id: expect.any(String),
    kind: "edit",
    key: "hub/personal/pe-3",
    payload: { set: { title: "Call Sam about Saturday" } },
  });
  await expect(detail.locator("#d-title")).toHaveText("Call Sam about Saturday");
  await expect(detail.locator(".d-title-btn")).toBeFocused();
  await expect(page.locator("#toast")).toContainText("Saved");

  await detail.getByRole("button", { name: "Add details" }).click();
  const desc = detail.getByRole("textbox", { name: "Details" });
  await expect(desc).toBeFocused();
  await desc.fill("Ask about the **train** times.");
  // Leaving the field saves it.
  await detail.getByLabel("Priority").focus();
  await expect.poll(() => hub.posted.at(-1)?.payload.set).toEqual({ description: "Ask about the **train** times." });
  await expect(detail.locator(".d-desc strong")).toHaveText("train");

  await detail.getByLabel("Priority").selectOption({ label: "High" });
  await expect.poll(() => hub.posted.at(-1)?.payload.set).toEqual({ priority: 1 });
  await expect(row(page, "Call Sam about Saturday").locator(".chip-high")).toHaveText("High priority");

  await page.locator("#toast").getByRole("button", { name: "Undo" }).click();
  await expect.poll(() => hub.posted.at(-1)?.payload.set).toEqual({ priority: 2 });
  await expect(row(page, "Call Sam about Saturday").locator(".chip-high")).toHaveCount(0);
  await expect(detail.getByLabel("Priority")).toHaveValue("2");
});

test("renaming: Escape cancels, nothing changed posts nothing, an empty title is refused", async ({ page, hub }) => {
  await openApp(page);
  const detail = await openDetail(page, "Water the plants");
  const before = hub.posted.length;

  await detail.locator(".d-title-btn").click();
  await page.keyboard.type("x");
  await page.keyboard.press("Escape");
  await expect(detail.getByRole("textbox", { name: "Title" })).toHaveCount(0);
  await expect(page.locator("#detail #d-title")).toHaveText("Water the plants");
  await expect(detail.locator(".d-title-btn")).toBeFocused();

  await detail.locator(".d-title-btn").click();
  await detail.getByRole("textbox", { name: "Title" }).press("Enter");
  await expect(detail.getByRole("textbox", { name: "Title" })).toHaveCount(0);
  expect(hub.posted.length).toBe(before);

  await detail.locator(".d-title-btn").click();
  await detail.getByRole("textbox", { name: "Title" }).fill("  ");
  await detail.getByRole("textbox", { name: "Title" }).press("Enter");
  await expect(detail.getByRole("alert")).toHaveText("Add a title.");
  await expect(detail.getByRole("textbox", { name: "Title" })).toBeFocused();
  expect(hub.posted.length).toBe(before);
  await page.keyboard.press("Escape");
  await expect(detail.getByRole("alert")).toHaveCount(0);

  // Details already set read as Markdown, with a button to change them.
  hub.find(KEY)!.description = "Use the *rain* water.";
  await page.reload();
  await expect(detail.locator(".d-desc em")).toHaveText("rain");
  await detail.getByRole("button", { name: "Edit details" }).click();
  await expect(detail.getByRole("textbox", { name: "Details" })).toHaveValue("Use the *rain* water.");
  await page.keyboard.press("Escape");
  await expect(detail.getByRole("button", { name: "Edit details" })).toBeFocused();
  expect(hub.posted.length).toBe(before);
});

test("high priority shows a chip on personal and agent items", async ({ page, hub }) => {
  hub.find("home/garden/ga-1")!.priority = 1;
  hub.find(KEY)!.priority = 0;
  await openApp(page);
  await expect(row(page, "Order compost for the beds").locator(".chip-high")).toHaveText("High priority");
  await expect(row(page, "Water the plants").locator(".chip-urgent")).toHaveText("Urgent");
  await expect(row(page, "Call Sam about the weekend").locator(".chip-high, .chip-urgent")).toHaveCount(0);

  const detail = await openDetail(page, "Order compost for the beds");
  await expect(detail.locator(".d-chips .chip-high")).toHaveText("High priority");

  // An item at 0 keeps it unless the user picks another.
  await openDetail(page, "Water the plants");
  await expect(detail.getByLabel("Priority")).toHaveValue("0");
  await expect(detail.getByLabel("Priority").locator("option")).toHaveText(["Urgent", "High", "Normal", "Low"]);
});

test("edits a note, deletes one and Undo restores it, and the add-note draft survives", async ({ page, hub }) => {
  seedNotes(hub);
  await openApp(page);
  const detail = await openDetail(page, "Water the plants");
  await detail.getByLabel("Add a note").fill("Half a can is enough.");

  const first = detail.locator(".note").first();
  await first.getByRole("button", { name: "Edit note" }).click();
  const box = detail.getByRole("textbox", { name: "Edit note" });
  await expect(box).toBeFocused();
  await expect(box).toHaveValue("Use the rain water.");
  await box.fill("Use the rain water from the barrel.");
  await detail.locator(".note-edit").getByRole("button", { name: "Save", exact: true }).click();
  await expect.poll(() => hub.posted.at(-1)).toMatchObject({ kind: "note-edit", key: KEY, payload: { commentId: "n1", text: "Use the rain water from the barrel." } });
  await expect(first.locator("p")).toHaveText("Use the rain water from the barrel.");
  await expect(page.locator("#toast")).toContainText("Note saved");

  // Escape closes the note form, not the detail.
  await first.getByRole("button", { name: "Edit note" }).click();
  await page.keyboard.press("Escape");
  await expect(detail.locator(".note-edit")).toHaveCount(0);
  await expect(page.locator("#detail #d-title")).toHaveText("Water the plants");

  await detail.locator(".note").nth(1).getByRole("button", { name: "Delete note" }).click();
  await expect.poll(() => hub.posted.at(-1)).toMatchObject({ kind: "note-delete", key: KEY, payload: { commentId: "n2" } });
  await expect(detail.locator(".note")).toHaveCount(1);
  await expect(page.locator("#toast")).toContainText("Note deleted");
  await page.locator("#toast").getByRole("button", { name: "Undo" }).click();
  await expect.poll(() => hub.posted.at(-1)).toMatchObject({
    kind: "note-restore",
    key: KEY,
    payload: { comment: { id: "n2", author: "alex-app", text: "The fern needs less.", createdAt: "2026-10-14T09:00:00Z" } },
  });
  await expect(detail.locator(".note p")).toHaveText(["Use the rain water from the barrel.", "The fern needs less."]);
  await expect(detail.getByLabel("Add a note")).toHaveValue("Half a can is enough.");
});

test("an agent item shows none of the edit controls, and the same action bar", async ({ page, hub }) => {
  void hub;
  await openApp(page);
  await page.getByRole("button", { name: /From agents/ }).click();
  const detail = await openDetail(page, "Approve the new footer text");
  await expect(detail.locator(".note")).toHaveCount(3);
  await expect(detail.locator("#d-sched, .props, .d-title-btn")).toHaveCount(0);
  await expect(detail.locator(".note-tools")).toHaveCount(0);
  await expect(detail.locator(".d-bar").getByRole("button")).toHaveText(["Done", "Snooze", ""]);
  await detail.getByRole("button", { name: "More actions" }).click();
  await expect(detail.getByRole("button", { name: "Won't do" })).toBeVisible();
});

/** A done sub-task under the personal item. */
function seedSubtask(hub: FakeHub, title: string) {
  hub.items.push({ ...structuredClone(hub.find(KEY)!), key: `${KEY}1`, id: "pe-11", title, parent: "pe-1", dueAt: null, comments: [], status: "closed", closeReason: "Done in Meat Mode", closedAt: "2026-10-14T08:00:00Z" });
}

test("the sheet, its fields, notes and sub-tasks pass axe", async ({ page, hub }) => {
  seedNotes(hub);
  seedSubtask(hub, "Fill the watering can");
  hub.find(KEY)!.priority = 1;
  hub.find(KEY)!.labels = ["garden"];
  await openApp(page);
  const detail = await openDetail(page, "Water the plants");
  await axe(page);
  await detail.locator(".d-title-btn").click();
  await axe(page);
  await page.keyboard.press("Escape");
  await detail.getByRole("button", { name: "More actions" }).click();
  await axe(page);
  await page.keyboard.press("Escape");
  await expect(detail.getByRole("button", { name: "More actions" })).toBeFocused();
  await detail.locator(".note").first().getByRole("button", { name: "Edit note" }).click();
  await axe(page);
});

test.describe("phone", () => {
  test.use({ viewport: { width: 375, height: 740 }, hasTouch: true, isMobile: true });

  test("the sheet fits the screen, and its action bar stays in view", async ({ page, hub }) => {
    seedNotes(hub);
    seedSubtask(hub, "Fill the watering can with the long spout from the shed");
    hub.find(KEY)!.labels = ["garden", "weekend", "errands"];
    await openApp(page);
    const detail = await openDetail(page, "Water the plants");
    await detail.locator(".note").first().getByRole("button", { name: "Edit note" }).click();
    await page.waitForTimeout(300);
    await expect(detail.getByRole("button", { name: "Done", exact: true })).toBeInViewport();
    const fit = await page.evaluate(() => {
      const d = document.querySelector("#detail")!;
      return { page: document.documentElement.scrollWidth, inner: window.innerWidth, sheet: d.scrollWidth, sheetBox: d.clientWidth };
    });
    expect(fit.page).toBeLessThanOrEqual(fit.inner);
    expect(fit.sheet).toBeLessThanOrEqual(fit.sheetBox);
    for (const sel of ["#d-sched", "#p-lead", "#p-prio", "#sub-input", ".sub-remove", "#d-more"]) {
      const box = (await detail.locator(sel).boundingBox())!;
      expect(box.x).toBeGreaterThanOrEqual(0);
      expect(box.x + box.width).toBeLessThanOrEqual(375);
    }
  });
});

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

test("edits title, due, details and priority, and Undo puts them back", async ({ page, hub }) => {
  await openApp(page);
  const detail = await openDetail(page, "Call Sam about the weekend");
  await detail.getByRole("button", { name: "Edit", exact: true }).click();
  await expect(detail.getByLabel("Title")).toBeFocused();
  await expect(detail.getByLabel("Title")).toHaveValue("Call Sam about the weekend");
  await expect(detail.locator("#edit-due-now")).toHaveText("Due 14:30");

  await detail.getByLabel("Title").fill("Call Sam about Saturday");
  await detail.getByLabel("Due").fill("friday 5pm");
  await expect(detail.locator("#edit-due-now")).toHaveText("Due Fri, Oct 16 17:00");
  await detail.getByLabel("Details", { exact: true }).fill("Ask about the train times.");
  await detail.getByLabel("Priority").selectOption({ label: "High" });
  await detail.getByRole("button", { name: "Save", exact: true }).click();

  await expect.poll(() => hub.posted.at(-1)).toEqual({
    id: expect.any(String),
    kind: "edit",
    key: "hub/personal/pe-3",
    payload: {
      set: { title: "Call Sam about Saturday", due: "2026-10-16T17:00:00.000Z", description: "Ask about the train times.", priority: 1 },
    },
  });
  await expect(detail.locator("#d-title")).toHaveText("Call Sam about Saturday");
  await expect(detail.locator(".why-label")).toHaveText("Details");
  await expect(detail.locator(".why p")).toHaveText("Ask about the train times.");
  await expect(detail.locator(".d-chips .chip-high")).toHaveText("High priority");
  await expect(row(page, "Call Sam about Saturday").locator(".chip-high")).toHaveText("High priority");
  await expect(detail.getByRole("button", { name: "Edit", exact: true })).toBeFocused();

  await expect(page.locator("#toast")).toContainText("Saved");
  await page.locator("#toast").getByRole("button", { name: "Undo" }).click();
  await expect.poll(() => hub.posted.at(-1)?.payload.set).toEqual({
    title: "Call Sam about the weekend",
    due: "2026-10-14T14:30:00.000Z",
    description: "",
    priority: 2,
  });
  await expect(detail.locator("#d-title")).toHaveText("Call Sam about the weekend");
  await expect(detail.locator(".chip-high")).toHaveCount(0);
  await expect(detail.locator(".why")).toHaveCount(0);
  await expect(row(page, "Call Sam about the weekend").locator(".chip-soon")).toHaveText("14:30");
});

test("the edit form: Escape and Cancel close it, nothing changed posts nothing, a bad date blocks saving, Clear removes the due date", async ({ page, hub }) => {
  await openApp(page);
  const detail = await openDetail(page, "Water the plants");
  const edit = detail.getByRole("button", { name: "Edit", exact: true });

  await edit.click();
  await page.keyboard.type("x");
  await page.keyboard.press("Escape");
  await expect(detail.locator(".edit-form")).toHaveCount(0);
  await expect(page.locator("#detail #d-title")).toHaveText("Water the plants");
  await expect(edit).toBeFocused();

  await edit.click();
  await detail.getByRole("button", { name: "Cancel" }).click();
  await expect(detail.locator(".edit-form")).toHaveCount(0);

  const before = hub.posted.length;
  await edit.click();
  await detail.getByRole("button", { name: "Save", exact: true }).click();
  await expect(detail.locator(".edit-form")).toHaveCount(0);
  expect(hub.posted.length).toBe(before);

  await edit.click();
  await detail.getByLabel("Due").fill("whenever");
  await expect(detail.locator("#edit-due-now")).toHaveText("Could not read that date.");
  await detail.getByLabel("Due").press("Enter");
  await expect(detail.getByRole("alert")).toContainText("Could not read that date");
  await expect(detail.getByLabel("Due")).toBeFocused();
  expect(hub.posted.length).toBe(before);

  await detail.getByRole("button", { name: "Clear" }).click();
  await expect(detail.locator("#edit-due-now")).toHaveText("No due date");
  await expect(detail.getByLabel("Due")).toHaveValue("");
  await detail.getByRole("button", { name: "Save", exact: true }).click();
  await expect.poll(() => hub.posted.at(-1)).toMatchObject({ kind: "edit", key: KEY, payload: { set: { clearDue: true } } });
  expect(Object.keys(hub.posted.at(-1)!.payload.set!)).toEqual(["clearDue"]);
  await expect(hub.find(KEY)!.dueAt).toBeNull();
  await expect(row(page, "Water the plants").locator(".chip-overdue, .chip-soon, .chip-date")).toHaveCount(0);
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
  await detail.getByRole("button", { name: "Edit", exact: true }).click();
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

test("an agent item shows none of the edit controls", async ({ page, hub }) => {
  void hub;
  await openApp(page);
  await page.getByRole("button", { name: /From agents/ }).click();
  const detail = await openDetail(page, "Approve the new footer text");
  await expect(detail.locator(".note")).toHaveCount(3);
  await expect(detail.getByRole("button", { name: "Edit", exact: true })).toHaveCount(0);
  await expect(detail.locator(".note-tools")).toHaveCount(0);
  await expect(detail.getByRole("button", { name: "Won't do" })).toBeVisible();
});

/** A done sub-task under the personal item. */
function seedSubtask(hub: FakeHub, title: string) {
  hub.items.push({ ...structuredClone(hub.find(KEY)!), key: `${KEY}1`, id: "pe-11", title, parent: "pe-1", dueAt: null, comments: [], status: "closed", closeReason: "Done in Meat Mode", closedAt: "2026-10-14T08:00:00Z" });
}

test("the edit form, notes and sub-tasks pass axe", async ({ page, hub }) => {
  seedNotes(hub);
  seedSubtask(hub, "Fill the watering can");
  hub.find(KEY)!.priority = 1;
  await openApp(page);
  const detail = await openDetail(page, "Water the plants");
  await detail.getByRole("button", { name: "Edit", exact: true }).click();
  await detail.locator(".note").first().getByRole("button", { name: "Edit note" }).click();
  await axe(page);
});

test.describe("phone", () => {
  test.use({ viewport: { width: 375, height: 740 }, hasTouch: true, isMobile: true });

  test("the forms fit the detail sheet", async ({ page, hub }) => {
    seedNotes(hub);
    seedSubtask(hub, "Fill the watering can with the long spout from the shed");
    await openApp(page);
    const detail = await openDetail(page, "Water the plants");
    await detail.getByRole("button", { name: "Edit", exact: true }).click();
    await detail.locator(".note").first().getByRole("button", { name: "Edit note" }).click();
    await detail.getByRole("button", { name: "Edit", exact: true }).click();
    await expect(detail.locator(".edit-form")).toBeVisible();
    const fit = await page.evaluate(() => {
      const d = document.querySelector("#detail")!;
      return { page: document.documentElement.scrollWidth, inner: window.innerWidth, sheet: d.scrollWidth, sheetBox: d.clientWidth };
    });
    expect(fit.page).toBeLessThanOrEqual(fit.inner);
    expect(fit.sheet).toBeLessThanOrEqual(fit.sheetBox);
    for (const sel of ["#edit-title", "#edit-due", "#edit-desc", "#edit-prio", "#sub-input", ".sub-remove"]) {
      const box = (await detail.locator(sel).boundingBox())!;
      expect(box.x).toBeGreaterThanOrEqual(0);
      expect(box.x + box.width).toBeLessThanOrEqual(375);
    }
  });
});

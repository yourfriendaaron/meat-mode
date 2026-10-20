import { test, expect, openApp, row, axe } from "./setup.ts";

test.use({ viewport: { width: 1280, height: 800 } });

test("adds a sub-task, shows it under its parent, and holds Done until it is finished", async ({ page, hub }) => {
  await openApp(page);
  await row(page, "Water the plants").locator(".row-body").click();
  const detail = page.locator("#detail");
  const add = detail.getByLabel("Add a sub-task");
  await add.fill("Fill the watering can");
  await add.press("Enter");
  await expect.poll(() => hub.posted.at(-1)).toEqual({
    id: expect.any(String),
    kind: "add",
    bridge: "hub",
    repo: "personal",
    payload: { title: "Fill the watering can", due: null, parent: "pe-1" },
  });
  await expect(add).toHaveValue("");
  await expect(add).toBeFocused();

  // The list nests it under the parent and counts it on the parent's row.
  const sub = row(page, "Fill the watering can");
  await expect(sub).toHaveClass(/row-sub/);
  await expect(row(page, "Water the plants").locator(".chip-subtasks")).toHaveText("0/1");
  await expect(sub.locator(".chip-parent")).toHaveCount(0);

  // Done on the parent waits for its open sub-task.
  await detail.getByRole("button", { name: "Done", exact: true }).click();
  await expect(page.locator(".toast")).toContainText("Finish or remove its open sub-task first.");
  expect(hub.posted.filter((a) => a.kind === "done")).toEqual([]);

  await detail.locator(".subtasks").getByRole("button", { name: "Mark done: Fill the watering can" }).click();
  await expect(row(page, "Water the plants").locator(".chip-subtasks")).toHaveText("1/1");
  await detail.getByRole("button", { name: "Done", exact: true }).click();
  await expect.poll(() => hub.find("hub/personal/pe-1")?.status).toBe("closed");
});

test("a sub-task names its parent, and Delete takes open sub-tasks along under one Undo", async ({ page, hub }) => {
  hub.items.push(
    { ...structuredClone(hub.find("hub/personal/pe-3")!), key: "hub/personal/pe-31", id: "pe-31", title: "Book the train", parent: "pe-3", dueAt: null },
    { ...structuredClone(hub.find("hub/personal/pe-3")!), key: "hub/personal/pe-32", id: "pe-32", title: "Pack a bag", parent: "pe-3", dueAt: null },
  );
  await openApp(page);
  await row(page, "Book the train").locator(".row-body").click();
  const detail = page.locator("#detail");
  await expect(detail.locator(".d-parent")).toHaveText("Part of Call Sam about the weekend");
  await detail.locator(".d-parent").getByRole("button", { name: "Call Sam about the weekend" }).click();
  await expect(detail.locator("#d-title")).toHaveText("Call Sam about the weekend");
  await expect(detail.locator(".subtasks .sub")).toHaveCount(2);
  await axe(page);

  await detail.getByRole("button", { name: "Delete", exact: true }).click();
  await expect(page.locator(".toast")).toContainText("Deleted: Call Sam about the weekend and 2 sub-tasks");
  expect(hub.posted.filter((a) => a.kind === "dismiss").map((a) => a.key)).toEqual([
    "hub/personal/pe-31",
    "hub/personal/pe-32",
    "hub/personal/pe-3",
  ]);
  await expect(row(page, "Book the train")).toHaveCount(0);

  await page.locator(".toast").getByRole("button", { name: "Undo" }).click();
  await expect(row(page, "Call Sam about the weekend")).toBeVisible();
  await expect(row(page, "Book the train")).toHaveClass(/row-sub/);
  expect(hub.posted.filter((a) => a.kind === "reopen").map((a) => a.key)).toEqual([
    "hub/personal/pe-3",
    "hub/personal/pe-32",
    "hub/personal/pe-31",
  ]);
});

test("a sub-task whose parent is not synced shows on its own row with the parent's title", async ({ page, hub }) => {
  hub.find("home/garden/ga-2")!.parent = "ga-9";
  hub.find("home/garden/ga-2")!.parentTitle = "Garden plan";
  await openApp(page);
  await page.getByRole("button", { name: "Everything" }).first().click();
  const r = row(page, "Check the tomato labels");
  await expect(r).not.toHaveClass(/row-sub/);
  await expect(r.locator(".chip-parent")).toHaveText("Part of Garden plan");
});

test("the edit form moves an item under another and back to the top level, each with Undo", async ({ page, hub }) => {
  await openApp(page);
  await row(page, "Call Sam about the weekend").locator(".row-body").click();
  const detail = page.locator("#detail");
  await detail.getByRole("button", { name: "Edit", exact: true }).click();
  const field = detail.getByLabel("Sub-task of");
  await expect(field).toHaveValue("");
  // Only open top-level items of the same list are offered, never the item itself.
  await expect(field.locator("option")).toHaveText([
    "None, a top-level task",
    "Buy seeds",
    "Fix the gate latch",
    "Plan the garden layout",
    "Renew the library card",
    "Water the plants",
  ]);
  await axe(page);
  await field.selectOption({ label: "Water the plants" });
  await detail.getByRole("button", { name: "Save", exact: true }).click();
  await expect.poll(() => hub.posted.at(-1)).toEqual({ id: expect.any(String), kind: "edit", key: "hub/personal/pe-3", payload: { set: { parent: "pe-1" } } });
  expect(hub.find("hub/personal/pe-3")!.parent).toBe("pe-1");
  await expect(row(page, "Call Sam about the weekend")).toHaveClass(/row-sub/);
  await expect(detail.locator(".d-parent")).toHaveText("Part of Water the plants");

  // Undo moves it back with the was the hub recorded.
  await page.locator("#toast").getByRole("button", { name: "Undo" }).click();
  await expect.poll(() => hub.posted.at(-1)?.payload.set).toEqual({ parent: "" });
  await expect(row(page, "Call Sam about the weekend")).not.toHaveClass(/row-sub/);
  expect(hub.find("hub/personal/pe-3")!.parent).toBeUndefined();

  // A parent with sub-tasks cannot move under another item.
  hub.find("hub/personal/pe-3")!.parent = "pe-1";
  await page.reload();
  await row(page, "Water the plants").locator(".row-body").click();
  await detail.getByRole("button", { name: "Edit", exact: true }).click();
  await expect(detail.getByLabel("Sub-task of")).toHaveCount(0);
  await detail.getByRole("button", { name: "Cancel", exact: true }).click();

  // A sub-task moves to the top level.
  await row(page, "Call Sam about the weekend").locator(".row-body").click();
  await detail.getByRole("button", { name: "Edit", exact: true }).click();
  await expect(detail.getByLabel("Sub-task of")).toHaveValue("pe-1");
  await detail.getByLabel("Sub-task of").selectOption({ label: "None, a top-level task" });
  await detail.getByRole("button", { name: "Save", exact: true }).click();
  await expect.poll(() => hub.posted.at(-1)?.payload.set).toEqual({ parent: "" });
  await expect(row(page, "Call Sam about the weekend")).not.toHaveClass(/row-sub/);
  await expect(detail.locator(".d-parent")).toHaveCount(0);
});

test("an agent's item has no move control", async ({ page, hub }) => {
  void hub;
  await openApp(page);
  await row(page, "Order compost for the beds").locator(".row-body").click();
  const detail = page.locator("#detail");
  await expect(detail.locator("#d-title")).toHaveText("Order compost for the beds");
  await expect(detail.getByRole("button", { name: "Edit", exact: true })).toHaveCount(0);
  await expect(detail.locator("#edit-parent")).toHaveCount(0);
});

test.describe("on a phone", () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });

  test("the move field fits the screen", async ({ page, hub }) => {
    void hub;
    await openApp(page);
    await row(page, "Call Sam about the weekend").locator(".row-body").click();
    const detail = page.locator("#detail");
    await detail.getByRole("button", { name: "Edit", exact: true }).click();
    const field = detail.getByLabel("Sub-task of");
    await expect(field).toHaveValue("");
    const box = (await field.boundingBox())!;
    expect(box.x).toBeGreaterThanOrEqual(0);
    expect(box.x + box.width).toBeLessThanOrEqual(390);
    const [scroll, inner] = await page.evaluate(() => [document.documentElement.scrollWidth, window.innerWidth]);
    expect(scroll).toBeLessThanOrEqual(inner);
  });
});

test("Done on a repeating parent reopens its done sub-tasks, and Undo closes them again", async ({ page, hub }) => {
  const parent = hub.find("hub/personal/pe-1")!;
  parent.repeat = { every: "day", timeZone: "UTC", start: parent.dueAt! };
  const closed = { status: "closed" as const, closedAt: "2026-10-14T09:00:00.000Z", dueAt: null };
  hub.items.push(
    { ...structuredClone(parent), ...closed, repeat: null, key: "hub/personal/pe-11", id: "pe-11", title: "Fill the watering can", parent: "pe-1", closeReason: "Done in Meat Mode" },
    { ...structuredClone(parent), ...closed, repeat: null, key: "hub/personal/pe-12", id: "pe-12", title: "Buy plant food", parent: "pe-1", closeReason: "Dismissed" },
  );
  await openApp(page);
  await expect(row(page, "Water the plants").locator(".chip-subtasks")).toHaveText("1/1");

  await row(page, "Water the plants").getByRole("button", { name: "Mark done: Water the plants" }).click();
  await expect(page.locator("#toast")).toContainText("Done. Next: Tomorrow 9:25");
  expect(hub.find("hub/personal/pe-11")!.status).toBe("open");
  expect(hub.find("hub/personal/pe-12")!.status).toBe("closed");
  await expect(row(page, "Fill the watering can")).toHaveClass(/row-sub/);
  await expect(row(page, "Water the plants").locator(".chip-subtasks")).toHaveText("0/1");

  await page.locator("#toast").getByRole("button", { name: "Undo" }).click();
  await expect.poll(() => hub.posted.at(-1)).toMatchObject({
    kind: "edit",
    key: "hub/personal/pe-1",
    payload: { set: { due: "2026-10-14T09:25:00.000Z", subtasksDone: ["pe-11"] } },
  });
  await expect.poll(() => hub.find("hub/personal/pe-11")!.status).toBe("closed");
  expect(hub.find("hub/personal/pe-12")!.closeReason).toBe("Dismissed");
  await expect(row(page, "Water the plants").locator(".chip-subtasks")).toHaveText("1/1");
  await expect(page.locator(".group-pastdue")).toContainText("Water the plants");
});

test("a personal item's sub-tasks tick, and the remove button deletes one with Undo", async ({ page, hub }) => {
  const base = hub.find("hub/personal/pe-1")!;
  hub.items.push(
    { ...structuredClone(base), key: "hub/personal/pe-11", id: "pe-11", title: "Fill the watering can", parent: "pe-1", dueAt: null, comments: [] },
    {
      ...structuredClone(base),
      key: "hub/personal/pe-12",
      id: "pe-12",
      title: "Move the fern out of the sun",
      parent: "pe-1",
      dueAt: null,
      comments: [],
      status: "closed",
      closeReason: "Done in Meat Mode",
      closedAt: "2026-10-14T08:00:00Z",
    },
  );
  await openApp(page);
  await row(page, "Water the plants").locator(".row-body").click();
  const subs = page.locator("#detail .subtasks");
  await expect(subs.locator(".sub-title")).toHaveText(["Fill the watering can", "Move the fern out of the sun"]);

  // Tick and Undo.
  await subs.getByRole("button", { name: "Mark done: Fill the watering can" }).click();
  await expect.poll(() => hub.posted.at(-1)).toMatchObject({ kind: "done", key: "hub/personal/pe-11" });
  await expect(page.locator("#toast")).toContainText("Done: Fill the watering can");
  await page.locator("#toast").getByRole("button", { name: "Undo" }).click();
  await expect.poll(() => hub.find("hub/personal/pe-11")?.status).toBe("open");

  // Remove an open one: it is deleted, and Undo brings it back open.
  await subs.getByRole("button", { name: "Remove: Fill the watering can" }).click();
  await expect.poll(() => hub.posted.at(-1)).toMatchObject({ kind: "dismiss", key: "hub/personal/pe-11" });
  await expect(page.locator("#toast")).toContainText("Deleted: Fill the watering can");
  await expect(subs.locator(".sub-title")).toHaveText(["Move the fern out of the sun"]);
  await expect(page.getByLabel("Add a sub-task")).toBeFocused();
  await page.locator("#toast").getByRole("button", { name: "Undo" }).click();
  await expect(subs.locator(".sub-title")).toHaveText(["Fill the watering can", "Move the fern out of the sun"]);
  expect(hub.find("hub/personal/pe-11")?.status).toBe("open");

  // Remove a done one: Undo brings it back done.
  await subs.getByRole("button", { name: "Remove: Move the fern out of the sun" }).click();
  await expect(page.locator("#toast")).toContainText("Deleted: Move the fern out of the sun");
  await expect(subs.locator(".sub-title")).toHaveText(["Fill the watering can"]);
  await page.locator("#toast").getByRole("button", { name: "Undo" }).click();
  // Removed, it is already closed as dismissed, so wait for the reason Undo restores.
  await expect.poll(() => hub.find("hub/personal/pe-12")?.closeReason).toBe("Done in Meat Mode");
  expect(hub.find("hub/personal/pe-12")?.status).toBe("closed");
  await expect(subs.locator(".sub-done .sub-title")).toHaveText(["Move the fern out of the sun"]);
  await axe(page);
});

test("a tracker item's sub-task is removed as Won't do with Undo, and not where the tracker cannot dismiss", async ({ page, hub }) => {
  const base = hub.find("home/garden/ga-1")!;
  hub.items.push({ ...structuredClone(base), key: "home/garden/ga-11", id: "ga-11", title: "Measure the beds", parent: "ga-1", dueAt: null, comments: [] });
  await openApp(page);
  await row(page, "Order compost for the beds").locator(".row-body").click();
  const subs = page.locator("#detail .subtasks");
  await expect(subs.locator(".sub-title")).toHaveText(["Measure the beds"]);

  await subs.getByRole("button", { name: "Mark done: Measure the beds" }).click();
  await expect.poll(() => hub.posted.at(-1)).toMatchObject({ kind: "done", key: "home/garden/ga-11" });
  await page.locator("#toast").getByRole("button", { name: "Undo" }).click();
  await expect(subs.getByRole("button", { name: "Mark done: Measure the beds" })).toBeVisible();

  const remove = subs.getByRole("button", { name: "Remove: Measure the beds" });
  await expect(remove).toHaveAttribute("title", "Won't do");
  await remove.click();
  await expect.poll(() => hub.posted.at(-1)).toMatchObject({ kind: "dismiss", key: "home/garden/ga-11" });
  await expect(page.locator("#toast")).toContainText("Won't do: Measure the beds");
  await expect(subs).toHaveCount(0);
  await page.locator("#toast").getByRole("button", { name: "Undo" }).click();
  await expect(subs.locator(".sub-title")).toHaveText(["Measure the beds"]);

  // A tracker that cannot dismiss offers no remove button.
  hub.repos.find((r) => r.bridge === "home" && r.name === "garden")!.actions = ["done", "reopen", "add", "subtask"];
  await page.reload();
  await expect(page.locator("#detail .subtasks .sub-title")).toHaveText(["Measure the beds"]);
  await expect(page.locator("#detail .sub-remove")).toHaveCount(0);
});

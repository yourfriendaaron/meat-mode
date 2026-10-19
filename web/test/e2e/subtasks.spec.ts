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

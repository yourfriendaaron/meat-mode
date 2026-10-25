import { test, expect, openApp, row, axe } from "./setup.ts";
import type { FakeHub } from "./fake-hub.ts";

test.use({ viewport: { width: 1280, height: 800 } });

/** The garden list as a tracker that marks done, dismisses and adds, but cannot snooze, take notes, reopen, nest or run agents. */
function ticketGarden(hub: FakeHub) {
  const garden = hub.repos.find((r) => r.bridge === "home" && r.name === "garden")!;
  garden.tracker = "ticket";
  garden.actions = ["done", "dismiss", "add"];
  garden.agentsAllowed = true;
  hub.bridges[0].trackers = [{ name: "beads", version: "1.3.0" }, { name: "ticket", version: "0.3.2" }];
}

test("a list's tracker hides what it cannot do, and Done still works", async ({ page, hub }) => {
  ticketGarden(hub);
  await openApp(page);
  const compost = row(page, "Order compost for the beds");
  await expect(compost.locator(".u-snooze")).toHaveCount(0);
  await expect(compost.locator(".u-done")).toHaveCount(1);

  await compost.locator(".row-body").click();
  const detail = page.locator("#detail");
  await expect(detail.locator("#d-title")).toHaveText("Order compost for the beds");
  await expect(detail.getByRole("heading", { name: "Snooze" })).toHaveCount(0);
  await expect(detail.getByLabel("Add a note")).toHaveCount(0);
  await expect(detail.getByRole("button", { name: /Ask agent|Save note/ })).toHaveCount(0);
  await expect(detail.getByLabel("Add a sub-task")).toHaveCount(0);
  // No agents switch to point at either: the tracker runs none.
  await expect(detail.locator(".run-off")).toHaveCount(0);
  await expect(detail.getByRole("button", { name: "Snooze", exact: true })).toHaveCount(0);
  await detail.getByRole("button", { name: "More actions" }).click();
  await expect(detail.getByRole("button", { name: "Won't do" })).toBeVisible();
  await axe(page);
  await page.keyboard.press("Escape");

  // The snooze key does nothing on it.
  await detail.locator("#d-title").click();
  await page.keyboard.press("s");
  await expect(page.locator("#snooze")).toBeHidden();

  await detail.getByRole("button", { name: "Done", exact: true }).click();
  await expect.poll(() => hub.posted.at(-1)).toMatchObject({ kind: "done", key: "home/garden/ga-1" });
  await expect(page.locator("#toast")).toContainText("Done: Order compost for the beds");
  // Once applied, a close undoes by a reopen, which this tracker cannot do.
  await expect(page.locator("#toast").getByRole("button", { name: "Undo" })).toHaveCount(0);
  expect(hub.posted.filter((a) => ["snooze", "note", "ask", "reopen"].includes(a.kind))).toEqual([]);
});

test("bulk actions show only what every pick's list can do", async ({ page, hub }) => {
  ticketGarden(hub);
  await openApp(page);
  await page.getByRole("button", { name: "Select", exact: true }).click();
  const bar = page.getByRole("toolbar", { name: "Selected tasks" });
  await page.getByRole("checkbox", { name: "Select: Water the plants" }).click();
  await expect(bar.getByRole("button", { name: "Snooze…" })).toBeVisible();
  await page.getByRole("checkbox", { name: "Select: Order compost for the beds" }).click();
  await expect(bar.getByRole("button", { name: "Snooze…" })).toHaveCount(0);
  await expect(bar.getByRole("button", { name: "Done" })).toBeEnabled();
});

test("Settings names each repo's tracker and each machine's trackers with versions", async ({ page, hub }) => {
  ticketGarden(hub);
  await openApp(page);
  await page.getByRole("button", { name: "Settings" }).first().click();
  const machine = page.locator(".machine", { hasText: "home" });
  await expect(machine.locator(".m-trackers")).toHaveText("beads 1.3.0 · ticket 0.3.2");
  await expect(machine.locator(".m-repo", { hasText: "garden · ticket" })).toBeVisible();
  // The tracker without agents has no agents line; one from an older bridge keeps it.
  await expect(machine.locator(".m-repo", { hasText: "garden · ticket" }).locator(".m-repo-agents")).toHaveCount(0);
  await expect(machine.locator(".m-repo", { hasText: "website" }).locator(".m-repo-agents")).toHaveCount(1);
});

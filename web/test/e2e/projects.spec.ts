import { test, expect, openApp, row, axe } from "./setup.ts";

test.use({ viewport: { width: 1280, height: 800 } });

test("a project shows as its own list and behaves like the personal list", async ({ page, hub }) => {
  hub.addProject("projects/attic", ["Approve the footer text"]);
  await openApp(page);

  // Its own list after the personal one, offered in quick add.
  const lists = page.locator("#lists .nav-item .nav-label");
  await expect(lists).toHaveText(["personal", "attic", "garden", "website"]);
  await expect(page.locator("#qa-list option")).toHaveText(["personal", "attic", "garden", "website"]);

  await page.locator("#lists").getByRole("button", { name: /attic/ }).click();
  await expect(page.locator("#list .row")).toHaveCount(1);
  await row(page, "Approve the footer text").locator(".row-body").click();
  const detail = page.locator("#detail");
  await expect(detail.locator("#d-title")).toHaveText("Approve the footer text");
  await expect(detail.getByRole("button", { name: "Edit", exact: true })).toBeVisible();
  await expect(detail.getByRole("button", { name: "Delete" })).toBeVisible();
  await expect(detail.getByRole("button", { name: /Ask agent/ })).toHaveCount(0);
  await axe(page);

  await detail.getByRole("button", { name: "Edit", exact: true }).click();
  await detail.getByLabel("Title").fill("Approve the header text");
  await detail.getByRole("button", { name: "Save", exact: true }).click();
  await expect.poll(() => hub.posted.at(-1)).toMatchObject({ kind: "edit", key: "hub/projects/attic/mm-90", payload: { set: { title: "Approve the header text" } } });

  // A new task starts in the list being viewed, and repeats as a personal one does.
  await page.locator("#qa-input").fill("Sweep the stairs every week from friday");
  await expect(page.locator("#qa-list")).toHaveValue("hub/projects/attic");
  await page.locator("#qa-input").press("Enter");
  await expect.poll(() => hub.posted.at(-1)).toMatchObject({ kind: "add", bridge: "hub", repo: "projects/attic", payload: { title: "Sweep the stairs", repeat: { every: "week" } } });
  await expect(row(page, "Sweep the stairs")).toBeVisible();
});

test("agent instructions raise tasks with each tracker the machine runs, and in a project elsewhere", async ({ page, hub }) => {
  hub.bridges[0].trackers = [
    { name: "beads", title: "beads", version: "1.3.0", raise: 'bd create "Approve the footer text" -l human -d "What is needed."' },
    { name: "backlog", title: "Backlog.md", version: "1.20.0", raise: 'backlog task create "Approve the footer text" --labels human' },
  ];
  await openApp(page);
  await page.getByRole("button", { name: "Settings" }).first().click();
  await expect(page.locator(".machine", { hasText: "home" }).locator(".m-trackers")).toHaveText("beads 1.3.0 · Backlog.md 1.20.0");
  const text = page.locator("#set-agents-text");
  await expect(text).toContainText('In a repo that uses beads:\n\n    bd create "Approve the footer text" -l human');
  await expect(text).toContainText('In a repo that uses Backlog.md:\n\n    backlog task create "Approve the footer text" --labels human');
  await expect(text).toContainText('meat-mode add "Approve the footer text" --project . ');
  await expect(text).toContainText("personal to-do list instead");
});

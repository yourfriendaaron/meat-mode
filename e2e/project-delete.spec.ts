// End to end: an agent's project is deleted in the app, its to-dos go to
// Deleted and the list leaves the app and the projects call; Undo brings
// both back. A fresh real hub; no bridge runs.

import { test, expect } from "@playwright/test";
import { World } from "./world.ts";

let world: World | undefined;
let failed = false;

test.beforeAll(async () => {
  world = await World.create();
});

test.afterEach(async ({}, info) => {
  if (info.status !== info.expectedStatus) failed = true;
});

test.afterAll(async () => {
  if (failed && world) console.log(world.logs());
  await world?.dispose();
  world = undefined;
});

test("a deleted project leaves the app with its to-dos, and Undo brings it back", async ({ page }) => {
  const w = world!;
  const { token } = await w.api<{ token: string }>("/api/bridges", {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ name: "desk" }),
  });
  const bridge = (path: string, body?: unknown) =>
    fetch(`${w.hubURL}${path}`, {
      method: body ? "POST" : "GET",
      headers: { authorization: `Bearer ${token}`, "content-type": "application/json" },
      body: body ? JSON.stringify(body) : undefined,
    });
  for (const title of ["Approve the footer text", "Pick a colour"]) {
    expect((await bridge("/bridge/v1/personal?project=projects/garden", { title, author: "planner" })).status).toBe(201);
  }
  const projects = async () => (await (await bridge("/bridge/v1/projects")).json()) as { name: string; open: number }[];
  expect(await projects()).toEqual([{ name: "projects/garden", open: 2 }]);

  await page.goto(w.appURL);
  const lists = page.locator("#lists");
  await lists.getByRole("button", { name: /^garden/ }).click();
  await expect(page.locator("#view-title")).toHaveText("garden");
  await expect(page.locator("#list .row")).toHaveCount(2);

  await test.step("the delete asks first, naming the list and its open to-dos", async () => {
    await page.locator("#listbar").getByRole("button", { name: "Delete list" }).click();
    await expect(page.locator("#listbar-q")).toHaveText("Delete garden and its 2 open to-dos? They go to Deleted for 7 days.");
    await page.locator("#listbar").getByRole("button", { name: "Cancel" }).click();
    await expect(page.locator("#listbar-q")).toHaveCount(0);
    await page.locator("#listbar").getByRole("button", { name: "Delete list" }).click();
    await page.locator("#listbar").getByRole("button", { name: "Delete list" }).click();
  });

  await test.step("the list is gone", async () => {
    await expect(page.locator("#toast")).toContainText("Deleted list: garden");
    await expect(page.locator("#view-title")).toHaveText("Today");
    await expect(lists.getByRole("button", { name: /^garden/ })).toHaveCount(0);
    expect(await projects()).toEqual([]);
  });

  await test.step("Undo brings the list and its to-dos back", async () => {
    await page.locator("#toast").getByRole("button", { name: "Undo" }).click();
    await expect(page.locator("#view-title")).toHaveText("garden");
    await expect(page.locator("#list .row")).toHaveCount(2);
    await expect(lists.getByRole("button", { name: /^garden/ })).toHaveCount(1);
    await expect.poll(projects).toEqual([{ name: "projects/garden", open: 2 }]);
  });

  await test.step("deleted again, its to-dos wait in Deleted", async () => {
    await page.locator("#listbar").getByRole("button", { name: "Delete list" }).click();
    await page.locator("#listbar").getByRole("button", { name: "Delete list" }).click();
    await expect(lists.getByRole("button", { name: /^garden/ })).toHaveCount(0);
    await page.locator("#nav").getByRole("button", { name: /Deleted/ }).click();
    await expect(page.locator("#list .row")).toHaveCount(2);
    await expect(page.locator("#list")).toContainText("Pick a colour");
  });
});

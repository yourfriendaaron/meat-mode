// End to end: an agent on a paired machine changes a to-do the user made in
// the app, through the bridge API with its machine token, and the user
// undoes it from the task sheet's History. A fresh real hub; no bridge runs.

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

interface Item {
  key: string;
  id: string;
  title: string;
  dueAt?: string | null;
}

test("an agent's change to the user's to-do shows in its History, and Undo puts it back", async ({ page }) => {
  const w = world!;
  const { token } = await w.api<{ token: string }>("/api/bridges", {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ name: "desk" }),
  });
  // The user adds the to-do in the app.
  const added = await w.api<{ beadId: string }>("/api/actions", {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({
      id: "3c1d2e4f-5a6b-4c7d-8e9f-0a1b2c3d4e5f",
      kind: "add",
      bridge: "hub",
      repo: "personal",
      payload: { title: "Water the plants", due: "2027-03-02T09:00:00Z" },
    }),
  });
  const id = added.beadId;
  const key = `hub/personal/${id}`;
  const item = async () => (await w.api<{ items: Item[] }>("/api/state")).items.find((i) => i.key === key)!;

  // An agent moves it with its machine's token.
  const r = await fetch(`${w.hubURL}/bridge/v1/personal/${id}/actions`, {
    method: "POST",
    headers: { authorization: `Bearer ${token}`, "content-type": "application/json" },
    body: JSON.stringify({ kind: "edit", payload: { set: { due: "2027-03-09T09:00:00Z" } }, author: "planner" }),
  });
  expect(r.status).toBe(201);
  expect(new Date((await item()).dueAt!).toISOString()).toBe("2027-03-09T09:00:00.000Z");

  await page.goto(`${w.appURL}/?item=${encodeURIComponent(key)}`);
  const detail = page.locator("#detail");
  await expect(detail.locator("#d-title")).toHaveText("Water the plants");
  const toggle = detail.getByRole("button", { name: /^History/ });
  await expect(toggle).toContainText("2");
  await toggle.click();
  const hist = detail.locator(".hist");
  await expect(hist).toHaveCount(2);
  await expect(hist.nth(0)).toContainText("Moved to Tue, Mar 9, 2027 at 9:00");
  await expect(hist.nth(0)).toContainText("planner on desk");
  await expect(hist.nth(1)).toContainText("Added");
  await expect(hist.nth(1)).toContainText("You");
  await expect(hist.nth(1).getByRole("button", { name: /Undo/ })).toHaveCount(0);

  await hist.nth(0).getByRole("button", { name: "Undo: Moved to Tue, Mar 9, 2027 at 9:00" }).click();
  await expect(page.locator("#toast")).toContainText("Undone");
  await expect.poll(async () => new Date((await item()).dueAt!).toISOString()).toBe("2027-03-02T09:00:00.000Z");
  // The Undo is the user's own change, newest in the History; the agent's no longer holds, so it offers none.
  await expect(hist).toHaveCount(3);
  await expect(hist.nth(0)).toContainText("Moved to Tue, Mar 2, 2027 at 9:00");
  await expect(hist.nth(0)).toContainText("You");
  await expect(hist.nth(1).getByRole("button", { name: /Undo/ })).toHaveCount(0);

  // The agent's change sent no notification.
  expect(w.ntfy.requests).toHaveLength(0);
});

import { test, expect, row, axe } from "./setup.ts";

test.use({ viewport: { width: 1280, height: 800 } });

const password = (page: import("@playwright/test").Page) => page.getByLabel("Password");

test("a hub with a password asks for it before loading anything", async ({ page, hub }) => {
  hub.authenticated = false;
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Meat Mode" })).toBeVisible();
  await expect(password(page)).toBeFocused();
  await expect(password(page)).toHaveAttribute("autocomplete", "current-password");
  // The state asked for alongside the session check is refused and shows no error.
  expect(hub.requests.toSorted()).toEqual(["GET /api/session", "GET /api/state"]);
  await expect(page.getByRole("alert")).toHaveCount(0);
  await axe(page);

  await password(page).fill("not it");
  await page.getByRole("button", { name: "Log in" }).click();
  await expect(page.getByRole("alert")).toHaveText("Wrong password.");
  await expect(password(page)).toBeFocused();
  await axe(page);

  await password(page).fill(hub.password);
  await password(page).press("Enter");
  await expect(row(page, "Water the plants")).toBeVisible();
  expect(hub.requests).toContain("GET /api/events");
});

test("a logged-in load asks for the state once", async ({ page, hub }) => {
  await page.goto("/");
  await expect(row(page, "Water the plants")).toBeVisible();
  expect(hub.requests.filter((r) => r === "GET /api/state")).toHaveLength(1);
});

test("too many tries says to wait", async ({ page, hub }) => {
  hub.authenticated = false;
  hub.loginStatus = 429;
  await page.goto("/");
  await password(page).fill("anything");
  await page.getByRole("button", { name: "Log in" }).click();
  await expect(page.getByRole("alert")).toHaveText("Too many tries. Wait a minute and try again.");
});

test("Log out in Settings returns to the login screen and stays there", async ({ page, hub }) => {
  hub.eventRetryMs = 200;
  await page.goto("/");
  await expect(row(page, "Water the plants")).toBeVisible();
  await page.locator(".sidebar").getByRole("button", { name: "Settings" }).click();
  await page.getByRole("button", { name: "Log out" }).click();
  await expect(password(page)).toBeVisible();
  expect(hub.requests).toContain("POST /api/logout");
  // The closed event stream does not keep knocking.
  const events = () => hub.requests.filter((r) => r === "GET /api/events").length;
  const before = events();
  await page.waitForTimeout(1500);
  expect(events()).toBe(before);
});

test("a session that ends mid-use returns to the login screen", async ({ page, hub }) => {
  await page.goto("/");
  await expect(row(page, "Water the plants")).toBeVisible();
  hub.authenticated = false;
  await row(page, "Water the plants").locator(".check").click();
  await expect(password(page)).toBeVisible();
  await password(page).fill(hub.password);
  await page.getByRole("button", { name: "Log in" }).click();
  await expect(row(page, "Water the plants")).toBeVisible();
});

test("an event stream refused for a lost session asks once and shows the login screen", async ({ page, hub }) => {
  hub.eventRetryMs = 200;
  await page.goto("/");
  await expect(row(page, "Water the plants")).toBeVisible();
  // The hub forgets the session; the stream's next reconnect gets 401 and closes.
  hub.eventRetryMs = 0;
  hub.authenticated = false;
  await expect(password(page)).toBeVisible();
  const events = () => hub.requests.filter((r) => r === "GET /api/events").length;
  const before = events();
  await page.waitForTimeout(1500);
  expect(events()).toBe(before);
});

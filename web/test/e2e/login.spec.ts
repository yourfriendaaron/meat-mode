import { test, expect, row, axe } from "./setup.ts";

test.use({ viewport: { width: 1280, height: 800 } });

const password = (page: import("@playwright/test").Page) => page.getByLabel("Password");

test("a hub with a login asks for the password before loading anything", async ({ page, hub }) => {
  hub.login = true;
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Meat Mode" })).toBeVisible();
  await expect(password(page)).toBeFocused();
  await expect(password(page)).toHaveAttribute("autocomplete", "current-password");
  expect(hub.requests).toEqual(["GET /api/session"]);
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

test("too many tries says to wait", async ({ page, hub }) => {
  hub.login = true;
  hub.loginStatus = 429;
  await page.goto("/");
  await password(page).fill("anything");
  await page.getByRole("button", { name: "Log in" }).click();
  await expect(page.getByRole("alert")).toHaveText("Too many tries. Wait a minute and try again.");
});

test("Log out in Settings returns to the login screen and stays there", async ({ page, hub }) => {
  hub.login = true;
  hub.authenticated = true;
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
  hub.login = true;
  hub.authenticated = true;
  await page.goto("/");
  await expect(row(page, "Water the plants")).toBeVisible();
  hub.authenticated = false;
  await row(page, "Water the plants").locator(".check").click();
  await expect(password(page)).toBeVisible();
  await password(page).fill(hub.password);
  await page.getByRole("button", { name: "Log in" }).click();
  await expect(row(page, "Water the plants")).toBeVisible();
});

test("a hub without a login shows no Log out", async ({ page, hub }) => {
  void hub;
  await page.goto("/");
  await expect(row(page, "Water the plants")).toBeVisible();
  await page.locator(".sidebar").getByRole("button", { name: "Settings" }).click();
  await expect(page.getByRole("heading", { name: "Reminders" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Log out" })).toHaveCount(0);
});

test("an event stream refused for a lost session asks once and shows the login screen", async ({ page, hub }) => {
  hub.login = true;
  hub.authenticated = true;
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

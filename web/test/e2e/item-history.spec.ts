import { test, expect, openApp, row, axe } from "./setup.ts";

test.use({ viewport: { width: 1280, height: 800 } });

test("History lists each change and undoes the latest one to a field", async ({ page, hub }) => {
  await openApp(page);
  await row(page, "Call Sam about the weekend").locator(".row-body").click();
  const detail = page.locator("#detail");
  const toggle = detail.getByRole("button", { name: /^History/ });
  await expect(toggle).toHaveAttribute("aria-expanded", "false");
  await expect(detail.locator(".hist")).toHaveCount(0);

  // Two renames: only the latest offers Undo, as it is the latest to the title.
  for (const t of ["Call Sam about Saturday", "Call Sam about Sunday"]) {
    await detail.locator(".d-title-btn").click();
    const title = detail.getByRole("textbox", { name: "Title" });
    await title.fill(t);
    await title.press("Enter");
    await expect(detail.locator("#d-title")).toHaveText(t);
  }
  await toggle.click();
  await expect(toggle).toHaveAttribute("aria-expanded", "true");
  await expect(toggle).toContainText("2");
  const hist = detail.locator(".hist");
  await expect(hist).toHaveCount(2);
  await expect(hist.nth(0)).toContainText("Renamed to “Call Sam about Sunday”");
  await expect(hist.nth(0)).toContainText("You · just now");
  await expect(hist.nth(1).getByRole("button", { name: /Undo/ })).toHaveCount(0);
  await axe(page);

  await hist.nth(0).getByRole("button", { name: "Undo: Renamed to “Call Sam about Sunday”" }).click();
  await expect(detail.locator("#d-title")).toHaveText("Call Sam about Saturday");
  await expect(page.locator("#toast")).toContainText("Undone: Renamed to “Call Sam about Sunday”");
  // The Undo is itself a change; the older rename stays without Undo while it holds the field.
  await expect(hist).toHaveCount(3);
  await expect(hist.nth(0)).toContainText("Renamed to “Call Sam about Saturday”");
  await expect(hist.nth(0).getByRole("button", { name: /Undo/ })).toHaveCount(1);
  await expect(hist.nth(1).getByRole("button", { name: /Undo/ })).toHaveCount(0);

  // Past five changes, Show all lists the rest.
  for (const p of ["High", "Low", "Normal"]) await detail.getByLabel("Priority").selectOption({ label: p });
  await expect(hist).toHaveCount(5);
  await detail.getByRole("button", { name: "Show all 6" }).click();
  await expect(hist).toHaveCount(6);
  expect(hub.posted.filter((a) => a.kind === "edit")).toHaveLength(6);
});

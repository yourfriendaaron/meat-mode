import { test, expect, axe, wizard, openWizardAt, skipToFinish } from "./setup.ts";
import type { Page } from "@playwright/test";

const EXAMPLE = "Water the plants tomorrow 6pm #personal";

async function lightTheme(page: Page) {
  await page.evaluate(() => {
    localStorage.setItem("meat-mode.theme", "light");
    document.documentElement.dataset.theme = "light";
  });
}

for (const [name, viewport] of [
  ["desktop", { width: 1280, height: 800 }],
  ["phone", { width: 390, height: 780 }],
] as const) {
  test.describe(name, () => {
    test.use({ viewport });

    test("the example is read as you type, then added to the personal list", async ({
      page,
      hub,
    }) => {
      await openWizardAt(page, hub, "first-todo");
      const w = wizard(page);
      await expect(w.heading).toHaveText("First to-do");
      await expect(w.region).toContainText(
        "A reminder fires at the due time, or at 9:00 when only a date is given.",
      );
      await axe(page);

      const input = page.locator("#ft-input");
      await input.pressSequentially("Water the plants tomorrow 6pm #pers");
      const read = page.locator("#ft-read");
      await expect(read).toContainText("Water the plants");
      await expect(read).toContainText("Due Tomorrow 18:00");
      await expect(read).toContainText("personal");
      await expect(page.locator("#ft-backdrop mark.hl-date")).toHaveText(
        "tomorrow 6pm",
      );
      await expect(page.locator("#ft-backdrop mark.hl-list")).toHaveText(
        "#pers",
      );
      await expect(input).toBeFocused();

      await page.getByRole("button", { name: "Add to-do" }).click();
      await expect(page.locator(".wiz-added")).toContainText(
        'Added "Water the plants" to personal.',
      );
      expect(hub.posted).toHaveLength(1);
      expect(hub.posted[0]).toMatchObject({
        kind: "add",
        bridge: "hub",
        repo: "personal",
      });
      expect(hub.posted[0].payload.title).toBe("Water the plants");
      const hour = await page.evaluate(
        (due) => new Date(due).getHours(),
        hub.posted[0].payload.due!,
      );
      expect(hour).toBe(18);
      await axe(page);
      await lightTheme(page);
      await axe(page);

      await w.next.click();
      await expect(w.heading).not.toHaveText("First to-do");
      await skipToFinish(page);
      await expect(page.locator(".wiz-summary")).toContainText("First to-do");
      await expect(
        page.locator(".wiz-summary li", { hasText: "First to-do" }),
      ).toContainText("Set up");
    });

    test("the example button fills the field", async ({ page, hub }) => {
      await openWizardAt(page, hub, "first-todo");
      await page.getByRole("button", { name: "Use the example" }).click();
      await expect(page.locator("#ft-input")).toHaveValue(EXAMPLE);
      await expect(page.locator("#ft-read")).toContainText(
        "Due Tomorrow 18:00",
      );
    });

    test("Continue adds what is typed", async ({ page, hub }) => {
      await openWizardAt(page, hub, "first-todo");
      await page.locator("#ft-input").fill("Call Sam on 20 Oct");
      await wizard(page).next.click();
      await expect(wizard(page).heading).not.toHaveText("First to-do");
      await skipToFinish(page);
      expect(hub.posted.map((a) => a.payload.title)).toEqual(["Call Sam"]);
    });

    test("a repeat is read and sent on the personal list", async ({
      page,
      hub,
    }) => {
      await openWizardAt(page, hub, "first-todo");
      await page.locator("#ft-input").fill("Water the plants every sunday");
      await expect(page.locator("#ft-read .chip-repeat")).toBeVisible();
      await page.getByRole("button", { name: "Add to-do" }).click();
      await expect(page.locator(".wiz-added")).toContainText(
        'Added "Water the plants" to personal.',
      );
      expect(hub.posted).toHaveLength(1);
      expect(hub.posted[0].payload.repeat).toMatchObject({
        every: "week",
        weekdays: [0],
      });
    });

    test("skipping creates nothing", async ({ page, hub }) => {
      await openWizardAt(page, hub, "first-todo");
      const w = wizard(page);
      await page.locator("#ft-input").fill("Water the plants tomorrow");
      await w.skip.click();
      await expect(w.heading).not.toHaveText("First to-do");
      await skipToFinish(page);
      await expect(
        page.locator(".wiz-summary li", { hasText: "First to-do" }),
      ).toContainText("Skipped");
      expect(hub.posted).toHaveLength(0);
    });
  });
}

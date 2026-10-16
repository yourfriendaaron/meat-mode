import { test as base, expect, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { FakeHub, NOW } from "./fake-hub.ts";

export const test = base.extend<{ hub: FakeHub }>({
  hub: async ({ page }, use) => {
    const hub = new FakeHub();
    await page.clock.setFixedTime(NOW);
    await hub.install(page);
    await use(hub);
  },
});

export { expect };

/** Opens the app and waits for the first state to render. */
export async function openApp(page: Page, path = "/") {
  await page.goto(path);
  await expect(page.locator("#list .row").first()).toBeVisible();
}

export function row(page: Page, title: string) {
  return page.locator("#list .row", { has: page.locator(".row-title", { hasText: title }) });
}

export async function axe(page: Page) {
  const result = await new AxeBuilder({ page }).analyze();
  const summary = result.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`);
  expect(summary, "axe violations").toEqual([]);
}

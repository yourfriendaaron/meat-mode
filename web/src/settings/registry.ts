// Settings sections that live in their own modules. Each calls
// registerSection; ./index.ts imports them all. main.ts draws them among its
// own sections, which sit at order 10 (Appearance), 20 (Reminders),
// 30 (Account) and 40 (Agent machines).

import type { AppContext, Handlers } from "../app-context.ts";
import { esc } from "../html.ts";

export interface SettingsSection extends Handlers {
  /** Unique and kebab-case. The section's heading gets the id s-<id>, which steps link to. */
  id: string;
  title: string;
  order: number;
  /** Whether the section shows at all. Default yes. */
  applies?(ctx: AppContext): boolean;
  /** The section's body under its heading, as HTML. Escape every value with esc(). */
  render(ctx: AppContext): string;
}

const registered: SettingsSection[] = [];

export function registerSection(section: SettingsSection): void {
  if (registered.some((s) => s.id === section.id)) throw new Error(`settings section ${section.id} is registered twice`);
  registered.push(section);
}

export function findSection(id: string | undefined): SettingsSection | undefined {
  return registered.find((s) => s.id === id);
}

/** The registered sections that apply, as [order, html] pairs for main.ts to merge with its own. */
export function sectionsHTML(ctx: AppContext): [number, string][] {
  return registered
    .filter((s) => s.applies?.(ctx) ?? true)
    .map((s) => [
      s.order,
      `<section class="set-section" data-section="${esc(s.id)}" aria-labelledby="s-${esc(s.id)}"><h2 id="s-${esc(s.id)}" tabindex="-1">${esc(s.title)}</h2>${s.render(ctx)}</section>`,
    ]);
}

// Settings sections, each in its own module. Each calls registerSection;
// ./index.ts imports them all, and Settings shows them by `order`.

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
  /** Runs when the app leaves Settings or logs out: drop what is shown only once, or only while there. */
  leave?(): void;
}

const registered: SettingsSection[] = [];

export function registerSection(section: SettingsSection): void {
  if (registered.some((s) => s.id === section.id)) throw new Error(`settings section ${section.id} is registered twice`);
  registered.push(section);
}

export function findSection(id: string | undefined): SettingsSection | undefined {
  return registered.find((s) => s.id === id);
}

/** The sections that apply, in order, as Settings' HTML. */
export function sectionsHTML(ctx: AppContext): string {
  return registered
    .filter((s) => s.applies?.(ctx) ?? true)
    .sort((a, b) => a.order - b.order)
    .map(
      (s) =>
        `<section class="set-section" data-section="${esc(s.id)}" aria-labelledby="s-${esc(s.id)}"><h2 id="s-${esc(s.id)}" tabindex="-1">${esc(s.title)}</h2>${s.render(ctx)}</section>`,
    )
    .join("\n");
}

/** Tells every section the app has left Settings. */
export function leaveSettings(): void {
  for (const s of registered) s.leave?.();
}

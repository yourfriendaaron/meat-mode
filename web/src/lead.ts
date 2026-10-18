// Reminder lead time (docs/api.md, Settings and Reminders): the hub's
// reminderLead, which the Settings section changes and a personal item's edit
// form names as its default, and the choices both offer.

import type { AppContext } from "./app-context.ts";
import { call } from "./api.ts";
import { esc } from "./html.ts";
import type { Settings } from "./types.ts";

/** The lead times the app offers, in minutes; 0 is none. */
export const LEAD_CHOICES = [0, 5, 10, 15, 30, 60, 120, 1440];

/** "30 minutes", "1 hour", "2 days"; "" for 0. */
export function leadText(minutes: number): string {
  const unit = (n: number, word: string) => `${n} ${word}${n === 1 ? "" : "s"}`;
  if (minutes <= 0) return "";
  if (minutes % 1440 === 0) return unit(minutes / 1440, "day");
  if (minutes % 60 === 0) return unit(minutes / 60, "hour");
  return unit(minutes, "minute");
}

/** A choice's label: "No early reminder", "30 minutes before". */
export function leadLabel(minutes: number): string {
  return minutes > 0 ? `${leadText(minutes)} before` : "No early reminder";
}

/** The choices, with `current` among them when it is a lead set elsewhere, such as by an agent. */
export function leadOptions(current: number | null): number[] {
  const out = [...LEAD_CHOICES];
  if (current !== null && !out.includes(current)) out.push(current);
  return out.sort((a, b) => a - b);
}

/** The hub's reminderLead, or null until it loaded. */
let lead: number | null = null;
let loading: Promise<void> | null = null;
let failed = "";

export function hubLead(): number | null {
  return lead;
}

/** Why the setting could not load, or "". */
export function leadError(): string {
  return failed;
}

/** Loads the hub's reminderLead once, then draws the app again. */
export function loadLead(ctx: Pick<AppContext, "render">, again = false): void {
  if (loading || (lead !== null && !again)) return;
  loading = call<Settings>("GET", "/api/settings").then(
    (s) => {
      lead = s.reminderLead ?? 0;
      failed = "";
    },
    (e: Error) => {
      failed = e.message;
    },
  );
  void loading.finally(() => {
    loading = null;
    ctx.render();
  });
}

/** Saves a new reminderLead. Resolves false, with a toast saying why, when the hub refuses it. */
export async function saveLead(minutes: number, ctx: AppContext): Promise<boolean> {
  try {
    const s = await call<Settings>("PUT", "/api/settings", { reminderLead: minutes });
    lead = s.reminderLead;
  } catch (e) {
    ctx.toast(`Could not save: ${(e as Error).message}`);
    ctx.render();
    return false;
  }
  ctx.render();
  return true;
}

/** A <select> of lead times. `value` null picks the "Use default" choice, which shows only when `withDefault` is set. */
export function leadSelect(id: string, value: number | null, withDefault: boolean, extra = ""): string {
  const def = withDefault
    ? `<option value=""${value === null ? " selected" : ""}>${esc(lead === null ? "Use default" : `Use default (${leadText(lead) ? `${leadText(lead)} before` : "none"})`)}</option>`
    : "";
  const opts = leadOptions(value)
    .map((m) => `<option value="${m}"${m === value ? " selected" : ""}>${esc(leadLabel(m))}</option>`)
    .join("");
  return `<select id="${esc(id)}"${extra}>${def}${opts}</select>`;
}

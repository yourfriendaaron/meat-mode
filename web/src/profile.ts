// The name and time zone the hub keeps, as one form shared by the setup
// step and the Settings section.

import type { AppContext } from "./app-context.ts";
import { call, errorText } from "./api.ts";
import { esc } from "./html.ts";
import type { SettingsUpdate } from "./types.ts";

/** This device's IANA time zone, or "" when the browser does not say. */
export function browserZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone ?? "";
  } catch {
    return "";
  }
}

/** Every IANA zone the browser knows, plus UTC, which some browsers leave out of the list. */
export function zoneNames(): string[] {
  let names: string[] = [];
  try {
    names = Intl.supportedValuesOf("timeZone");
  } catch {
    // an older browser: the form still takes any zone typed in
  }
  return names.includes("UTC") ? names : ["UTC", ...names];
}

export function validZone(tz: string): boolean {
  try {
    new Intl.DateTimeFormat("en", { timeZone: tz });
    return true;
  } catch {
    return false;
  }
}

/**
 * What to send for what was typed, given what the hub keeps: only the fields
 * that changed. An empty zone never changes anything, as the hub has no way
 * to forget its zone. Returns an error message instead when the zone is not one.
 */
export function profileChange(
  kept: { user: string; timeZone: string },
  typed: { name: string; timeZone: string },
): SettingsUpdate | string {
  const out: SettingsUpdate = {};
  const name = typed.name.trim();
  const zone = typed.timeZone.trim();
  if (name !== kept.user) out.name = name;
  if (zone && zone !== kept.timeZone) {
    if (!validZone(zone)) return `"${zone}" is not a time zone. Pick one from the list, such as Europe/Paris.`;
    out.timeZone = zone;
  }
  return out;
}

/** The form. `p` prefixes its element ids, because the step and the Settings section can both be in the page. */
export function profileForm(ctx: AppContext, p: string, save: boolean): string {
  const user = ctx.setup?.user ?? "";
  const kept = ctx.setup?.timeZone ?? "";
  const zone = kept || browserZone();
  const options = zoneNames()
    .map((z) => `<option value="${esc(z)}"></option>`)
    .join("");
  return `<form class="profile-form" data-form="profile">
<label for="${p}-name">Name</label>
<input id="${p}-name" name="name" maxlength="64" autocomplete="off" value="${esc(user)}" aria-describedby="${p}-name-hint">
<p class="muted small" id="${p}-name-hint">Notes you write in the app are signed with it, as ${esc(user || "alex")}-app.</p>
<label for="${p}-zone">Time zone</label>
<input id="${p}-zone" name="timeZone" list="${p}-zones" autocomplete="off" autocapitalize="off" spellcheck="false" value="${esc(zone)}" aria-describedby="${p}-zone-hint">
<datalist id="${p}-zones">${options}</datalist>
<p class="muted small" id="${p}-zone-hint">${
    kept ? "" : "No zone is kept yet, so this is the one this device reports. "
  }Agents add to-dos with dates such as 2026-03-02 in this zone, whatever zone their machine is in. Dates in the app follow each device. Type to search the list.</p>
<p class="row-error" role="alert" data-profile-error hidden></p>${
    save ? '<div class="btn-row"><button class="btn btn-primary" type="submit">Save</button></div>' : ""
  }
</form>`;
}

/** Saves the form's name and zone. Resolves false, with the reason under the form, when the hub or the form refuses them. */
export async function saveProfile(form: HTMLFormElement, ctx: AppContext): Promise<boolean> {
  const field = (n: string) => form.elements.namedItem(n) as HTMLInputElement;
  const error = form.querySelector<HTMLElement>("[data-profile-error]")!;
  const show = (text: string) => {
    error.textContent = text;
    error.hidden = !text;
  };
  const change = profileChange(
    { user: ctx.setup?.user ?? "", timeZone: ctx.setup?.timeZone ?? "" },
    { name: field("name").value, timeZone: field("timeZone").value },
  );
  if (typeof change === "string") {
    show(change);
    field("timeZone").focus();
    return false;
  }
  show("");
  if (change.name === undefined && change.timeZone === undefined) return true;
  try {
    await call("PUT", "/api/settings", change);
  } catch (e) {
    show(errorText(e));
    return false;
  }
  await ctx.refresh();
  return true;
}

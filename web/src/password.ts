// The hub's password, kept in the app: claiming a new hub with its setup
// code and changing the password (docs/api.md, Password). The claim screen a
// hub with no password shows, the setup wizard's Secure step and the
// Password section in Settings draw these forms; `place` keeps their field
// ids and messages apart.

import type { AppContext } from "./app-context.ts";
import { ApiError, call } from "./api.ts";
import { esc } from "./html.ts";

/** The hub refuses a shorter password. */
export const MIN_PASSWORD = 8;

export type Place = "wiz" | "set" | "claim";

interface Status {
  error: string;
  ok: string;
  busy: boolean;
}

const status: Record<Place, Status> = {
  wiz: { error: "", ok: "", busy: false },
  set: { error: "", ok: "", busy: false },
  claim: { error: "", ok: "", busy: false },
};

/** Why the new password cannot be used, or "". */
export function checkPassword(password: string, repeat: string): string {
  if ([...password].length < MIN_PASSWORD) return `The password must be at least ${MIN_PASSWORD} characters.`;
  if (password !== repeat) return "The two passwords do not match.";
  return "";
}

/** What a refused claim says under the form. */
export function claimErrorText(status: number, message: string): string {
  if (status === 403) return "That setup code is wrong, expired or already used. Restart the hub for a new one.";
  if (status === 429) return "Too many wrong codes. Wait a minute and try again.";
  return sentence(message);
}

/** What a refused password change says under the form. */
export function changeErrorText(status: number, message: string): string {
  if (status === 403) return "The current password is wrong.";
  if (status === 429) return "Too many tries. Wait a minute and try again.";
  return sentence(message);
}

function sentence(s: string): string {
  const t = s.trim();
  if (!t) return "Something went wrong.";
  return t[0].toUpperCase() + t.slice(1) + (/[.!?]$/.test(t) ? "" : ".");
}

/** The last form's error or success message, or "". */
export function messages(place: Place): string {
  const s = status[place];
  if (s.error) return `<p id="${place}-pw-error" class="pw-error" role="alert" tabindex="-1">${esc(s.error)}</p>`;
  if (s.ok) return `<p id="${place}-pw-ok" class="pw-ok" role="status" tabindex="-1">${esc(s.ok)}</p>`;
  return "";
}

/** Clears the messages a form left, such as when the step is shown again. */
export function clearStatus(place: Place): void {
  status[place].error = "";
  status[place].ok = "";
}

function field(place: Place, name: string, label: string, attrs: string): string {
  const id = `${place}-pw-${name}`;
  const err = status[place].error ? ` aria-describedby="${place}-pw-error"` : "";
  return `<label for="${id}">${esc(label)}</label><input id="${id}" name="${name}" ${attrs}${err}>`;
}

/** Where to find the setup code, and the form that claims the hub with it. */
export function claimHTML(place: Place): string {
  const s = status[place];
  return `<p>The hub made a one-time setup code when it started. It is in two places:</p>
<ul class="wiz-list pw-where">
<li>The hub's log, for example <code>docker logs &lt;container&gt;</code> or <code>docker compose logs meat-mode</code>.</li>
<li>The file <code>setup-code</code> in the hub's data folder (<code>/data</code> in the container).</li>
</ul>
<p class="muted small">The code works once and lasts an hour. Restart the hub for a new one.</p>
<form class="pw-form" data-form="pw-claim" novalidate>
${field(place, "code", "Setup code", 'type="text" autocomplete="one-time-code" autocapitalize="characters" spellcheck="false" placeholder="XXXX-XXXX-XXXX"')}
${field(place, "password", "New password", `type="password" autocomplete="new-password" minlength="${MIN_PASSWORD}"`)}
${field(place, "repeat", "Repeat the password", `type="password" autocomplete="new-password" minlength="${MIN_PASSWORD}"`)}
<p class="muted small">At least ${MIN_PASSWORD} characters.</p>
${messages(place)}
<button class="btn btn-primary" type="submit"${s.busy ? " disabled" : ""}>Set password</button>
</form>`;
}

/** The form that changes a password set in the app. */
export function changeHTML(place: Place): string {
  const s = status[place];
  return `<form class="pw-form" data-form="pw-change" novalidate>
<input class="sr" type="text" name="username" autocomplete="username" value="meat-mode" tabindex="-1" aria-hidden="true">
${field(place, "current", "Current password", 'type="password" autocomplete="current-password"')}
${field(place, "password", "New password", `type="password" autocomplete="new-password" minlength="${MIN_PASSWORD}"`)}
${field(place, "repeat", "Repeat the new password", `type="password" autocomplete="new-password" minlength="${MIN_PASSWORD}"`)}
<p class="muted small">Every other device is logged out and needs the new password.</p>
${messages(place)}
<button class="btn btn-primary" type="submit"${s.busy ? " disabled" : ""}>Change password</button>
</form>`;
}

function value(form: HTMLFormElement, name: string): string {
  return form.querySelector<HTMLInputElement>(`input[name=${name}]`)?.value ?? "";
}

/** Whether anything was typed into the claim form. */
export function claimTyped(form: HTMLFormElement | null): boolean {
  return !!form && ["code", "password", "repeat"].some((n) => value(form, n) !== "");
}

/**
 * Runs one form's request with its values read first, since the redraw that
 * disables the button replaces the form. Clears the fields on success.
 */
async function run(place: Place, ctx: AppContext, fn: () => Promise<string>): Promise<boolean> {
  const s = status[place];
  if (s.busy) return false;
  s.busy = true;
  s.ok = "";
  ctx.render();
  let ok = false;
  try {
    s.ok = await fn();
    s.error = "";
    for (const f of document.querySelectorAll<HTMLInputElement>(`input[id^="${place}-pw-"]`)) f.value = "";
    ok = true;
  } catch (e) {
    s.error = (e as Error).message;
  }
  s.busy = false;
  await ctx.refresh();
  document.getElementById(ok ? `${place}-pw-ok` : `${place}-pw-error`)?.focus();
  return ok;
}

/**
 * Claims the hub with the code and password in form. On success this browser
 * is logged in and the app reloads what it knows of the hub.
 */
export function claim(place: Place, form: HTMLFormElement, ctx: AppContext): Promise<boolean> {
  return run(place, ctx, claimWith(form));
}

/** The claim request with the form's values read now; it throws the message to show. */
function claimWith(form: HTMLFormElement): () => Promise<string> {
  const code = value(form, "code").trim();
  const password = value(form, "password");
  const repeat = value(form, "repeat");
  return async () => {
    if (!code) throw new Error("Enter the setup code.");
    const bad = checkPassword(password, repeat);
    if (bad) throw new Error(bad);
    try {
      await call<void>("POST", "/api/setup/claim", { code, password });
    } catch (e) {
      throw new Error(e instanceof ApiError ? claimErrorText(e.status, e.message) : sentence((e as Error).message));
    }
    return "The password is set, and this device is logged in.";
  };
}

/**
 * Claims the hub from the claim screen, which has no app around it: draw
 * puts the screen back after each change. Resolves true once this browser
 * is logged in.
 */
export async function claimScreen(form: HTMLFormElement, draw: () => void): Promise<boolean> {
  const s = status.claim;
  if (s.busy) return false;
  const req = claimWith(form);
  s.busy = true;
  s.ok = "";
  draw();
  let ok = false;
  try {
    await req();
    s.error = "";
    ok = true;
  } catch (e) {
    s.error = (e as Error).message;
  }
  s.busy = false;
  if (!ok) {
    draw();
    document.getElementById("claim-pw-error")?.focus();
  }
  return ok;
}

/** Changes the password; every other device is logged out. */
export function change(place: Place, form: HTMLFormElement, ctx: AppContext): Promise<boolean> {
  const current = value(form, "current");
  const password = value(form, "password");
  const repeat = value(form, "repeat");
  return run(place, ctx, async () => {
    if (!current) throw new Error("Enter the current password.");
    const bad = checkPassword(password, repeat);
    if (bad) throw new Error(bad);
    try {
      await call<void>("PUT", "/api/password", { current, password });
    } catch (e) {
      throw new Error(e instanceof ApiError ? changeErrorText(e.status, e.message) : sentence((e as Error).message));
    }
    return "The password is changed. Every other device is logged out.";
  });
}

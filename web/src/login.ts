// The login screen's pure parts: whether to show it, and what a failed attempt says.

import { errorText } from "./api.ts";
import type { Session } from "./types.ts";

/**
 * What the app shows instead of itself: the claim step on a hub with no
 * password yet, which serves nothing else (docs/api.md, Login), the login
 * screen when this browser is not logged in, or nothing.
 */
export function gate(s: Session): "claim" | "login" | null {
  if (!s.login) return "claim";
  return s.authenticated ? null : "login";
}

/** Whether the app must ask for the password before it loads anything. */
export function needsLogin(s: Session): boolean {
  return s.login && !s.authenticated;
}

/** The message under the password field after a login the hub refused. */
export function loginErrorText(e: unknown): string {
  return errorText(e, { unauthorized: "Wrong password.", other: (m) => `Could not log in: ${m}` });
}

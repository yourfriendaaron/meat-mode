// The login screen's pure parts: whether to show it, and what a failed attempt says.

import type { Session } from "./types.ts";

/** Whether the app must ask for the password before it loads anything. */
export function needsLogin(s: Session): boolean {
  return s.login && !s.authenticated;
}

/** The message under the password field after a login the hub refused. */
export function loginErrorText(status: number, message: string): string {
  if (status === 401) return "Wrong password.";
  if (status === 429) return "Too many tries. Wait a minute and try again.";
  return `Could not log in: ${message}`;
}

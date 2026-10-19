// What the app hands the setup wizard's steps and the Settings sections.
// main.ts builds the one instance.

import type { Handle } from "./actions.ts";
import type { NewAction, Session, SetupInfo, State } from "./types.ts";

export interface AppContext {
  /** The hub's state, or null before the first load. */
  readonly state: State | null;
  /** Whether the hub has a login, or null before the app asked. */
  readonly session: Session | null;
  /** GET /api/setup, or null until it loaded or when the hub could not answer. */
  readonly setup: SetupInfo | null;
  /** The view on screen: "today", "settings", "setup", "list:<repo>"… */
  readonly view: string;
  /** The hub's time, corrected for this device's clock. */
  now(): Date;
  /** Draws the app again from the current state. */
  render(): void;
  /** Loads /api/state, /api/setup and /api/session again, then draws the app. */
  refresh(): Promise<void>;
  /** Posts actions through the app's pipeline, which patches the local copy and toasts a failed post. */
  perform(actions: NewAction[]): Promise<Handle>;
  /** Shows a short message at the bottom of the screen. */
  toast(text: string): void;
  /** Switches the view, as the navigation does. */
  setView(view: string): void;
  /** Opens Settings at the section whose heading has this id, such as "s-push". */
  openSettings(section?: string): void;
}

/**
 * Events a step or a Settings section handles in its own markup. Clicks on
 * any [data-act] main.ts does not know reach `act`; submitted forms whose
 * data-form it does not know reach `submit` (already preventDefault-ed);
 * change events reach `change`. A data-act main.ts already knows, such as
 * "theme" or "logout", keeps doing what it does in Settings.
 */
export interface Handlers {
  act?(act: string, el: HTMLElement, ctx: AppContext): void | Promise<void>;
  submit?(form: HTMLFormElement, ctx: AppContext): void | Promise<void>;
  change?(el: HTMLElement, ctx: AppContext): void;
}

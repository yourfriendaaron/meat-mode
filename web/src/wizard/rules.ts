// First-run rules for the setup wizard: when it opens on its own, which parts
// a run holds, and the progress a device keeps so a reload resumes it.

/** The hub part sets up the hub once; the device part sets up each device. */
export type Part = "hub" | "device";

/** "first" opens on a new hub; "all" and "device" start from Settings or the offer to set up this device. */
export type Mode = "first" | "all" | "device";

export type HubStatus = "new" | "in-use" | "done" | "dismissed";

/** What this device recorded about its own part, kept in localStorage. */
export type DeviceRecord = "" | "done" | "dismissed";

/** A run in progress on this device. */
export interface Progress {
  mode: Mode;
  /** The id of the step on screen. */
  step: string;
  /** Steps left with Continue. */
  done: string[];
  /** Steps left with Skip. */
  skipped: string[];
}

export const PROGRESS_KEY = "meat-mode.setup";
export const DEVICE_KEY = "meat-mode.setup-device";

export function partsOf(mode: Mode): Part[] {
  return mode === "device" ? ["device"] : ["hub", "device"];
}

/**
 * What the app does about setup when it starts:
 * - "resume": a run was in progress on this device; reopen it where it was.
 *   A first run that was finished or dismissed elsewhere is not resumed.
 * - "first": the hub is new (no items, machines or record); open the wizard.
 * - "device": offer this device's part in a banner, while it was neither
 *   done nor dismissed here and reminders are off.
 * - null: nothing. The wizard never opens on its own on a hub in use.
 * hub is null when the hub's status could not be loaded.
 */
export function firstRun(
  hub: HubStatus | null,
  device: DeviceRecord,
  remindersOn: boolean,
  progress: Progress | null,
): "resume" | "first" | "device" | null {
  if (progress && !(progress.mode === "first" && (hub === "done" || hub === "dismissed"))) return "resume";
  if (hub === "new") return "first";
  if (hub !== null && device === "" && !remindersOn) return "device";
  return null;
}

/** Reads saved progress, or null for anything that is not a run. */
export function parseProgress(raw: string | null): Progress | null {
  if (!raw) return null;
  try {
    const p = JSON.parse(raw) as Partial<Progress>;
    const strings = (a: unknown) => Array.isArray(a) && a.every((x) => typeof x === "string");
    if (
      (p.mode === "first" || p.mode === "all" || p.mode === "device") &&
      typeof p.step === "string" &&
      strings(p.done) &&
      strings(p.skipped)
    ) {
      return { mode: p.mode, step: p.step, done: p.done!, skipped: p.skipped! };
    }
  } catch {
    // not JSON
  }
  return null;
}

export function loadProgress(): Progress | null {
  try {
    return parseProgress(localStorage.getItem(PROGRESS_KEY));
  } catch {
    return null;
  }
}

export function saveProgress(p: Progress | null): void {
  try {
    if (p) localStorage.setItem(PROGRESS_KEY, JSON.stringify(p));
    else localStorage.removeItem(PROGRESS_KEY);
  } catch {
    // storage blocked: the run lasts for this page only
  }
}

export function deviceRecord(): DeviceRecord {
  try {
    const v = localStorage.getItem(DEVICE_KEY);
    return v === "done" || v === "dismissed" ? v : "";
  } catch {
    return "";
  }
}

export function setDeviceRecord(r: "done" | "dismissed"): void {
  try {
    localStorage.setItem(DEVICE_KEY, r);
  } catch {
    // storage blocked
  }
}

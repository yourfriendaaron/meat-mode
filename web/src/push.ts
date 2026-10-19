// Reminders on this device: whether they are on, and turning them on. In a
// browser that is Web Push; in the Android app it is the app's message bridge
// (docs/api.md, Android app).

import { api, errorText } from "./api.ts";
import { browserName } from "./device.ts";
import type { AppPushState, AppPushStatus, AppPushSubscription } from "./types.ts";

/** What the app injects as `window.meatMode`. */
interface Bridge {
  postMessage(message: string): void;
  onmessage: ((e: { data: unknown }) => void) | null;
}

/** How long `hello` has to answer before the page treats the app as absent. */
export const HELLO_WAIT_MS = 2000;
/** How often, and for how long, the page asks `push-status` for a confirmation. */
export const CONFIRM_POLL_MS = 1000;
export const CONFIRM_WAIT_MS = 30_000;
/** How long `push-on` has to answer: it waits for the user at Android's notification permission prompt and the distributor picker. */
export const PUSH_ON_WAIT_MS = 180_000;

let nextId = 0;
const waiting = new Map<string, { ok: (v: any) => void; fail: (e: Error) => void }>();

function bridge(): Bridge | null {
  const b = (window as unknown as { meatMode?: Bridge }).meatMode;
  return b && typeof b.postMessage === "function" ? b : null;
}

/** Sends one request to the app and resolves to its answer; rejects with the app's plain message. */
function appCall<T>(type: string, extra: object = {}, timeoutMs = 0): Promise<T> {
  const b = bridge();
  if (!b) return Promise.reject(new Error("The app is not here."));
  b.onmessage = (e) => {
    try {
      const m = typeof e.data === "string" ? JSON.parse(e.data) : e.data;
      const w = m && waiting.get(String(m.id));
      if (!w) return;
      waiting.delete(String(m.id));
      if (typeof m.error === "string") w.fail(new Error(m.error));
      else w.ok(m);
    } catch {
      // not an answer to us
    }
  };
  return new Promise<T>((ok, fail) => {
    const id = `m${++nextId}`;
    const timer = timeoutMs
      ? setTimeout(() => {
          waiting.delete(id);
          fail(new Error("The app did not answer."));
        }, timeoutMs)
      : 0;
    waiting.set(id, {
      ok: (v) => (clearTimeout(timer), ok(v)),
      fail: (e) => (clearTimeout(timer), fail(e)),
    });
    b.postMessage(JSON.stringify({ id, type, ...extra }));
  });
}

let inApp = false;
let appStatus: AppPushStatus | null = null;

/** Whether this page runs inside the Android app. Settled by initPush. */
export function inAndroidApp(): boolean {
  return inApp;
}

/** What the app last said about reminders, null in a browser. */
export function appPushStatus(): AppPushStatus | null {
  return inApp ? appStatus : null;
}

/** Asks the app how reminders stand and remembers it. Resolves to null when the app does not answer. */
export async function refreshAppStatus(): Promise<AppPushStatus | null> {
  if (!inApp) return null;
  try {
    appStatus = await appCall<AppPushStatus>("push-status", {}, 10_000);
  } catch {
    appStatus = null;
  }
  return appStatus;
}

/**
 * Finds out whether this page is in the Android app (`window.meatMode` and a
 * `hello` answered within 2 seconds) and, if so, loads the app's reminder
 * state. Call it once at start, before anything reads remindersOn().
 */
export async function initPush(): Promise<void> {
  if (!bridge()) return;
  try {
    await appCall("hello", {}, HELLO_WAIT_MS);
    inApp = true;
  } catch {
    inApp = false;
    return;
  }
  await refreshAppStatus();
}

/** Plain words for a reminder state that stops the user, "" for the others. */
export function appStateMessage(state: AppPushState | undefined): string {
  if (state === "blocked") return "Notifications are blocked for Meat Mode. Allow them in the phone's Android settings, then try again.";
  if (state === "unavailable") {
    return "This phone has no way to receive reminders yet. Install a UnifiedPush app such as ntfy, or use a phone with Google Play services, then try again.";
  }
  return "";
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

/** The app path of turnOnReminders: register through the app, then wait for the hub to confirm. */
async function turnOnInApp(force = false): Promise<string> {
  const before = await refreshAppStatus();
  const stop = appStateMessage(before?.state);
  if (stop) return stop;
  if (before?.state === "on" && !force) return "Reminders are on.";
  const { publicKey } = await api.pushKey();
  const reg = await appCall<AppPushSubscription>("push-on", { vapidKey: publicKey }, PUSH_ON_WAIT_MS);
  await api.pushSubscribe(reg.subscription, "app", reg.name);
  for (let waited = 0; waited <= CONFIRM_WAIT_MS; waited += CONFIRM_POLL_MS) {
    const st = await refreshAppStatus();
    if (st?.state === "on") return "Reminders are on.";
    const msg = appStateMessage(st?.state);
    if (msg) return msg;
    await sleep(CONFIRM_POLL_MS);
  }
  return "Waiting for the hub to confirm this phone took too long. Check that the hub can reach your push server, then try again.";
}

function b64ToBytes(s: string): Uint8Array<ArrayBuffer> {
  const pad = "=".repeat((4 - (s.length % 4)) % 4);
  const raw = atob((s + pad).replace(/-/g, "+").replace(/_/g, "/"));
  const out = new Uint8Array(new ArrayBuffer(raw.length));
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
  return out;
}

/** Whether this device has allowed notifications from the app. */
export function remindersOn(): boolean {
  if (inApp) return appStatus?.state === "on";
  return typeof Notification !== "undefined" && Notification.permission === "granted";
}

/** The endpoint of this device's push subscription, "" when it has none. */
export async function pushEndpoint(): Promise<string> {
  if (inApp) return (await refreshAppStatus())?.endpoint ?? "";
  if (!("serviceWorker" in navigator) || !("PushManager" in window)) return "";
  const reg = await navigator.serviceWorker.register("/sw.js");
  await navigator.serviceWorker.ready;
  return (await reg.pushManager.getSubscription())?.toJSON().endpoint ?? "";
}

/**
 * Registers the service worker and sends this device's push subscription to
 * the hub. With renew, a subscription the push service no longer accepts is
 * replaced by a new one. Resolves to the subscription's endpoint.
 */
export async function subscribePush(renew = false): Promise<string> {
  if (inApp) {
    // The app renews its own registration each time it opens; only a refused one needs the page.
    if (renew) await turnOnInApp(true);
    return (await refreshAppStatus())?.endpoint ?? "";
  }
  const reg = await navigator.serviceWorker.register("/sw.js");
  await navigator.serviceWorker.ready;
  let sub = await reg.pushManager.getSubscription();
  if (sub && renew) {
    await sub.unsubscribe();
    sub = null;
  }
  if (!sub) {
    const { publicKey } = await api.pushKey();
    sub = await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: b64ToBytes(publicKey) });
  }
  const json = sub.toJSON();
  await api.pushSubscribe(json, "web", browserName());
  return json.endpoint ?? "";
}

/**
 * Asks for permission and subscribes. Resolves to the message to show. Call it
 * from a tap. With force, the app registers again even though it already
 * reports reminders on; a browser always subscribes again when asked.
 */
export async function turnOnReminders(force = false): Promise<string> {
  if (inApp) {
    try {
      return await turnOnInApp(force);
    } catch (e) {
      return `Could not turn on reminders: ${errorText(e)}`;
    }
  }
  if (!("serviceWorker" in navigator) || !("PushManager" in window) || typeof Notification === "undefined") {
    return "This browser does not support reminders.";
  }
  try {
    const perm = await Notification.requestPermission();
    if (perm !== "granted") return "Notifications were not allowed.";
    await subscribePush();
    return "Reminders are on.";
  } catch (e) {
    return `Could not turn on reminders: ${errorText(e)}`;
  }
}

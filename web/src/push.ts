// Web Push on this device: whether reminders are on, and turning them on.

import { api } from "./api.ts";

function b64ToBytes(s: string): Uint8Array<ArrayBuffer> {
  const pad = "=".repeat((4 - (s.length % 4)) % 4);
  const raw = atob((s + pad).replace(/-/g, "+").replace(/_/g, "/"));
  const out = new Uint8Array(new ArrayBuffer(raw.length));
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
  return out;
}

/** Whether this device has allowed notifications from the app. */
export function remindersOn(): boolean {
  return typeof Notification !== "undefined" && Notification.permission === "granted";
}

/** The endpoint of this device's push subscription, "" when it has none. */
export async function pushEndpoint(): Promise<string> {
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
  await api.pushSubscribe(json);
  return json.endpoint ?? "";
}

/** Asks for permission and subscribes. Resolves to the message to show. Call it from a tap. */
export async function turnOnReminders(): Promise<string> {
  if (!("serviceWorker" in navigator) || !("PushManager" in window) || typeof Notification === "undefined") {
    return "This browser does not support reminders.";
  }
  try {
    const perm = await Notification.requestPermission();
    if (perm !== "granted") return "Notifications were not allowed.";
    await subscribePush();
    return "Reminders are on.";
  } catch (e) {
    return `Could not turn on reminders: ${(e as Error).message}`;
  }
}

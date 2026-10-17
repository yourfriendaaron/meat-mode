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

/** Registers the service worker and sends this device's push subscription to the hub. */
export async function subscribePush(): Promise<void> {
  const reg = await navigator.serviceWorker.register("/sw.js");
  await navigator.serviceWorker.ready;
  let sub = await reg.pushManager.getSubscription();
  if (!sub) {
    const { publicKey } = await api.pushKey();
    sub = await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: b64ToBytes(publicKey) });
  }
  await api.pushSubscribe(sub.toJSON());
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

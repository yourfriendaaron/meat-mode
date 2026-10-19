// Service worker: shows reminders and agent pushes and opens the app on click.
// Payload: {title, body, url, tag, actions?, key?} (docs/api.md, Agent push).
// Every push shows a notification, since browsers revoke subscriptions that
// receive silent pushes.

import { actionBody, failureText, isPostedAction, type ButtonAction } from "./sw-actions.ts";

interface PushAction {
  action: "reply" | "open" | "retry" | "done" | "snooze";
  title: string;
}

interface PushPayload {
  title?: string;
  body?: string;
  url?: string;
  tag?: string;
  actions?: PushAction[];
  /** The item an agent push is about; set when it has actions. */
  key?: string;
}

interface SWClient {
  url: string;
  focus(): Promise<unknown>;
  navigate?(url: string): Promise<unknown>;
}

interface SWScope {
  location: Location;
  registration: { showNotification(title: string, opts: Record<string, unknown>): Promise<void> };
  clients: {
    matchAll(opts: { type: string; includeUncontrolled: boolean }): Promise<SWClient[]>;
    openWindow(url: string): Promise<unknown>;
    claim(): Promise<void>;
  };
  skipWaiting(): Promise<void>;
  addEventListener(type: string, fn: (e: any) => void): void;
}

const sw = self as unknown as SWScope;

sw.addEventListener("install", () => {
  void sw.skipWaiting();
});

sw.addEventListener("activate", (e) => {
  e.waitUntil(sw.clients.claim());
});

sw.addEventListener("push", (e) => {
  let data: PushPayload = {};
  try {
    data = e.data ? e.data.json() : {};
  } catch {
    data = { body: e.data ? e.data.text() : "" };
  }
  const title = data.title || "Meat Mode";
  e.waitUntil(
    sw.registration.showNotification(title, {
      body: data.body ?? "",
      tag: data.tag,
      data: { url: data.url || "/", key: data.key ?? "" },
      actions: (data.actions ?? []).filter((a) => a && typeof a.action === "string").map((a) => ({ action: a.action, title: a.title })),
      icon: "/icon-192.png",
      badge: "/icon-192.png",
    }),
  );
});

/** A fresh action id; a service worker always has a secure context, so randomUUID is there. */
function newId(): string {
  return crypto.randomUUID();
}

/** A notification button that acts: Retry asks again on the item, Done and Snooze (an hour) change it, posted as the app posts them. */
async function post(action: ButtonAction, key: string): Promise<string | null> {
  try {
    const res = await fetch("/api/actions", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      credentials: "same-origin",
      body: JSON.stringify(actionBody(action, key, newId(), new Date())),
    });
    return res.ok ? null : failureText(res.status, await res.text().catch(() => ""));
  } catch {
    return failureText(0, "");
  }
}

sw.addEventListener("notificationclick", (e) => {
  e.notification.close();
  const key: string = e.notification.data?.key || "";
  const action: string = e.action || "";
  if (isPostedAction(action) && key) {
    const itemUrl = `/?item=${encodeURIComponent(key)}`;
    const title: string = e.notification.title || "Meat Mode";
    e.waitUntil(
      post(action, key).then((reason) => {
        if (reason === null) return undefined;
        // Done says why in a notification, which opens the item; the other buttons open the item so its card says why.
        if (action !== "done") return open(itemUrl);
        return sw.registration.showNotification(title, {
          body: reason,
          tag: `failed:${key}`,
          data: { url: itemUrl, key: "" },
          icon: "/icon-192.png",
          badge: "/icon-192.png",
        });
      }),
    );
    return;
  }
  const url = action === "reply" && key ? `/?item=${encodeURIComponent(key)}&reply=1` : e.notification.data?.url || "/";
  e.waitUntil(open(url));
});

/** Focuses a window of the app at url, or opens one. */
async function open(url: string): Promise<void> {
  let target: URL;
  try {
    target = new URL(url, sw.location.origin);
  } catch {
    target = new URL("/", sw.location.origin);
  }
  // Never open an origin other than the app's own, whatever the payload said.
  if (target.origin !== sw.location.origin) target = new URL("/", sw.location.origin);
  const href = target.href;
  const wins = await sw.clients.matchAll({ type: "window", includeUncontrolled: true });
  for (const w of wins) {
    if (new URL(w.url).origin !== sw.location.origin) continue;
    if (w.url !== href) {
      // An uncontrolled window cannot be sent to the item; open one that is.
      if (!w.navigate) continue;
      try {
        await w.navigate(href);
      } catch {
        continue;
      }
    }
    await w.focus();
    return;
  }
  await sw.clients.openWindow(href);
}

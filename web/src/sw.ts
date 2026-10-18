// Service worker: shows reminders and agent pushes and opens the app on click.
// Payload: {title, body, url, tag, actions?, key?} (docs/api.md, Agent push).
// Every push shows a notification, since browsers revoke subscriptions that
// receive silent pushes.

interface PushAction {
  action: "reply" | "open" | "retry";
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

/** Retry on a stopped run's push: a new ask on the item, posted as the app posts it. */
async function retry(key: string): Promise<boolean> {
  try {
    const res = await fetch("/api/actions", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      credentials: "same-origin",
      body: JSON.stringify({ id: newId(), kind: "ask", key, payload: {} }),
    });
    return res.ok;
  } catch {
    return false;
  }
}

sw.addEventListener("notificationclick", (e) => {
  e.notification.close();
  const key: string = e.notification.data?.key || "";
  const action: string = e.action || "";
  if (action === "retry" && key) {
    // When the hub refuses, open the item so its card says why.
    e.waitUntil(retry(key).then((ok) => (ok ? undefined : open(`/?item=${encodeURIComponent(key)}`))));
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
    if (w.url !== href && w.navigate) {
      try {
        await w.navigate(href);
      } catch {
        // uncontrolled client: focus it anyway
      }
    }
    await w.focus();
    return;
  }
  await sw.clients.openWindow(href);
}

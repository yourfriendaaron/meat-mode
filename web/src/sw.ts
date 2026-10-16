// Service worker: shows reminders from push messages and opens the app on click.
// Payload: {title, body, url, tag}. Every push shows a notification, since
// browsers revoke subscriptions that receive silent pushes.

interface PushPayload {
  title?: string;
  body?: string;
  url?: string;
  tag?: string;
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
      data: { url: data.url || "/" },
      icon: "/icon-192.png",
      badge: "/icon-192.png",
    }),
  );
});

sw.addEventListener("notificationclick", (e) => {
  e.notification.close();
  let target: URL;
  try {
    target = new URL(e.notification.data?.url || "/", sw.location.origin);
  } catch {
    target = new URL("/", sw.location.origin);
  }
  // Never open an origin other than the app's own, whatever the payload said.
  if (target.origin !== sw.location.origin) target = new URL("/", sw.location.origin);
  const href = target.href;
  e.waitUntil(
    (async () => {
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
    })(),
  );
});

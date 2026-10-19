// The Reminders section: turning reminders on for this device, in a browser
// or in the Android app.

import { esc } from "../html.ts";
import { appPushStatus, appStateMessage, inAndroidApp, turnOnReminders } from "../push.ts";
import { forgetDevices } from "./devices.ts";
import { registerSection } from "./registry.ts";

/** What the last tap on Turn on reminders reported. */
let status = "";

function bodyHTML(): string {
  if (inAndroidApp()) {
    const app = appPushStatus();
    if (app?.state === "on") return '<p>Reminders are on for this phone.</p><button class="btn" data-act="push">Register this phone again</button>';
    const stop = appStateMessage(app?.state);
    if (stop) return `<p class="muted">${esc(stop)}</p><button class="btn" data-act="push">Try again</button>`;
    return '<button class="btn btn-primary" data-act="push">Turn on reminders</button>';
  }
  const perm = typeof Notification === "undefined" ? "unsupported" : Notification.permission;
  if (perm === "unsupported") return '<p class="muted">This browser does not support reminders.</p>';
  if (perm === "granted") return '<p>Reminders are on for this device.</p><button class="btn" data-act="push">Send subscription again</button>';
  if (perm === "denied") return '<p class="muted">Notifications are blocked for this site in the browser settings.</p>';
  return '<button class="btn btn-primary" data-act="push">Turn on reminders</button>';
}

registerSection({
  id: "push",
  title: "Reminders",
  order: 20,
  render: () => `${bodyHTML()}${status ? `<p class="muted small" role="status">${esc(status)}</p>` : ""}`,
  async act(act, _el, ctx) {
    if (act !== "push") return;
    // “Register this phone again”: the app already reports reminders on, so it must be told to register once more.
    const again = inAndroidApp() && appPushStatus()?.state === "on";
    status = await turnOnReminders(again);
    // The device list gains this device.
    forgetDevices();
    ctx.render();
  },
});

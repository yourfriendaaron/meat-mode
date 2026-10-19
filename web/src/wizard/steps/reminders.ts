import { call } from "../../api.ts";
import { isIOS, pushSupport } from "../../device.ts";
import { esc } from "../../html.ts";
import { appPushStatus, appStateMessage, inAndroidApp, pushEndpoint, remindersOn, subscribePush, turnOnReminders } from "../../push.ts";
import { ORDER, registerStep } from "../registry.ts";

interface ReminderTest {
  push: "sent" | "none" | "expired" | "failed";
  ntfy: "sent" | "failed" | "off";
}

/** What the last tap on this step reported. */
let message = "";
/** Set while a request is under way, so a second tap does nothing. */
let busy = false;

function testMessage(r: ReminderTest): string {
  const parts: string[] = [];
  if (r.push === "sent") parts.push("A test reminder is on its way to this device. It should show up in a few seconds.");
  else if (r.push === "none") parts.push("This device has no reminder subscription yet. Turn on reminders first.");
  else if (r.push === "expired") parts.push("The push service no longer accepts this device's reminder subscription, and renewing it did not help.");
  else parts.push("The hub could not send the test reminder to this device.");
  if (r.ntfy === "sent") parts.push("A test reminder was also sent to ntfy.");
  else if (r.ntfy === "failed") parts.push("The hub could not reach ntfy.");
  return parts.join(" ");
}

async function sendTest(): Promise<string> {
  try {
    // The hub sends the test to this device's own subscription only.
    let r = await call<ReminderTest>("POST", "/api/reminders/test", { endpoint: await pushEndpoint() });
    if (r.push === "expired" || r.push === "none") {
      // The hub dropped the stale subscription, or this device has none; make one and try once more.
      const endpoint = await subscribePush(true);
      r = await call<ReminderTest>("POST", "/api/reminders/test", { endpoint });
    }
    return testMessage(r);
  } catch (e) {
    return `Could not send a test reminder: ${(e as Error).message}`;
  }
}

/** How to allow notifications again once they are blocked, for the platform. */
function undoText(): string {
  if (isIOS()) {
    return "Open the Settings app, choose Notifications, pick Meat Mode and turn on Allow Notifications.";
  }
  return "Open the site settings for this address (the icon at the left of the address bar), set Notifications to Allow, then come back.";
}

function bodyInApp(): string {
  const stop = appStateMessage(appPushStatus()?.state);
  if (stop) {
    return `<p>${esc(stop)}</p>
<div class="btn-row"><button class="btn" data-act="reminders-recheck">Check again</button></div>`;
  }
  if (remindersOn()) {
    return `<p>Reminders are on for this phone. Send a test to check that they arrive.</p>
<div class="btn-row"><button class="btn" data-act="reminders-test">Send a test reminder</button></div>`;
  }
  return `<p>Meat Mode reminds you when a to-do is due or an agent needs you, even when the app is closed.</p>
<p>The app will ask to show notifications and, if you have several push apps, which one to use.</p>
<div class="btn-row"><button class="btn btn-primary" data-act="reminders-enable">Turn on reminders</button></div>`;
}

function body(): string {
  if (inAndroidApp()) return bodyInApp();
  const support = pushSupport();
  if (support === "needs-install") {
    return `<p>On an iPhone or iPad, reminders work only once Meat Mode is on the Home Screen. Go back to the install step, add it, open it from the Home Screen icon and come back here.</p>
<p class="muted">You can skip this step and turn reminders on later in Settings.</p>`;
  }
  if (support === "unsupported") {
    return `<p>This browser cannot show reminders for a web app. Meat Mode works without them; you can turn them on from another device or browser.</p>`;
  }
  if (Notification.permission === "denied") {
    return `<p>Notifications are blocked for Meat Mode on this device, so the browser will not ask again.</p>
<p>${esc(undoText())}</p>
<div class="btn-row"><button class="btn" data-act="reminders-recheck">Check again</button></div>`;
  }
  if (remindersOn()) {
    return `<p>Reminders are on for this device. Send a test to check that they arrive.</p>
<div class="btn-row"><button class="btn" data-act="reminders-test">Send a test reminder</button></div>`;
  }
  return `<p>Meat Mode reminds you when a to-do is due or an agent needs you, even when the app is closed.</p>
<p>Your browser will ask to show notifications. Choose Allow. If you choose Block, the browser will not ask again and you will have to change it in its settings.</p>
<div class="btn-row"><button class="btn btn-primary" data-act="reminders-enable">Turn on reminders</button></div>`;
}

registerStep({
  id: "reminders",
  title: "Reminders",
  part: "device",
  order: ORDER.reminders,
  settings: "s-push",
  start() {
    message = "";
  },
  render() {
    const status = message ? `<p class="muted small" role="status">${esc(message)}</p>` : "";
    return body() + status;
  },
  done: () => remindersOn(),
  async act(act, _el, ctx) {
    if (busy) return;
    busy = true;
    try {
      if (act === "reminders-enable" || (act === "reminders-recheck" && remindersOn())) {
        // The tap is what lets the browser ask; the test follows only after a yes.
        message = await turnOnReminders();
        ctx.render();
        if (remindersOn()) message = await sendTest();
      } else if (act === "reminders-test") {
        message = await sendTest();
      } else if (act === "reminders-recheck" && inAndroidApp()) {
        message = await turnOnReminders();
      } else if (act === "reminders-recheck") {
        message = "Notifications are still blocked.";
      }
    } finally {
      busy = false;
    }
    ctx.render();
  },
});

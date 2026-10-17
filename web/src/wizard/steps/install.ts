import type { AppContext } from "../../app-context.ts";
import { installPlatform, isInstalled, pushSupport } from "../../device.ts";
import { esc } from "../../html.ts";
import { ORDER, registerStep } from "../registry.ts";

/** Chromium's install offer, which only it fires. */
interface InstallPrompt extends Event {
  prompt(): Promise<void>;
  userChoice: Promise<{ outcome: "accepted" | "dismissed" }>;
}

/** The offer the browser made, kept until a tap uses it; it works once. */
let offer: InstallPrompt | null = null;
/** Set once the browser reports the app installed from this tab. */
let installed = false;
/** What the last tap on this step reported. */
let message = "";
/** The context of the last render, so a late offer can redraw the step. */
let shown: AppContext | null = null;

// Outside a browser (the unit tests) there is no window to listen on.
if (typeof window !== "undefined") {
  addEventListener("beforeinstallprompt", (e) => {
    // Keep the browser's own banner away; the step offers it from a tap.
    e.preventDefault();
    offer = e as InstallPrompt;
    shown?.render();
  });

  addEventListener("appinstalled", () => {
    installed = true;
    offer = null;
    shown?.render();
  });
}

/** What reminders need on this device, in one sentence. */
function pushLine(): string {
  switch (pushSupport()) {
    case "needs-install":
      return "<p>On an iPhone or iPad, reminders work only in the Home Screen app, so add it before turning them on.</p>";
    case "ready":
      return "<p>Installing is optional: reminders work in this browser too.</p>";
    default:
      return "";
  }
}

function iosSteps(ctx: AppContext): string {
  const login = ctx.session?.login ? " It keeps its own storage apart from Safari, so log in again when it asks." : "";
  return `<ol class="wiz-list">
<li>Tap the Share button. In iOS 26, tap the three dots next to the address first, then Share.</li>
<li>Scroll down and tap Add to Home Screen.</li>
<li>Leave Open as Web App on, if it shows, and tap Add.</li>
<li>Open Meat Mode from its icon on the Home Screen.${esc(login)} Then choose Set up this device to turn on reminders there.</li>
</ol>`;
}

function body(ctx: AppContext): string {
  if (installed) {
    return "<p>Meat Mode is installed. Open it from your home screen or app list; it opens in its own window.</p>";
  }
  const intro = "<p>Installing Meat Mode gives it an icon of its own and opens it in its own window, like any other app.</p>";
  if (offer) {
    return `${intro}${pushLine()}<div class="btn-row"><button class="btn btn-primary" data-act="install-app">Install Meat Mode</button></div>`;
  }
  const platform = installPlatform();
  if (platform === "ios") return `${intro}${pushLine()}${iosSteps(ctx)}`;
  if (platform === "mac-safari") {
    return `${intro}${pushLine()}<p>In Safari, choose File, then Add to Dock, then Add. Meat Mode then opens from the Dock.</p>`;
  }
  // A browser that neither offers to install nor has steps we know: say nothing about how.
  return `${intro}${pushLine()}`;
}

registerStep({
  id: "install",
  title: "Install the app",
  part: "device",
  order: ORDER.install,
  // Already running as the installed app: nothing to do here.
  applies: () => !isInstalled(),
  done: () => installed || isInstalled(),
  render(ctx) {
    shown = ctx;
    const status = message ? `<p class="muted small" role="status">${esc(message)}</p>` : "";
    return body(ctx) + status;
  },
  async act(act, _el, ctx) {
    if (act !== "install-app" || !offer) return;
    const used = offer;
    offer = null;
    try {
      await used.prompt();
      const { outcome } = await used.userChoice;
      if (outcome === "accepted") installed = true;
      else message = "Not installed. You can install it later from the browser's menu.";
    } catch (e) {
      message = `Could not install: ${(e as Error).message}`;
    }
    ctx.render();
  },
});

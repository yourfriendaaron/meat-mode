// What this device and browser can do, for the setup wizard's device steps.

/** Whether the app runs as an installed web app rather than in a browser tab. */
export function isInstalled(): boolean {
  const nav = navigator as Navigator & { standalone?: boolean };
  return matchMedia("(display-mode: standalone)").matches || nav.standalone === true;
}

/** Whether this is an iPhone or iPad. iPadOS reports a Mac user agent, so a Mac with a touch screen counts. */
export function isIOS(): boolean {
  return /iPhone|iPad|iPod/.test(navigator.userAgent) || (/Macintosh/.test(navigator.userAgent) && navigator.maxTouchPoints > 1);
}

/**
 * Whether reminders can work here: "ready" when the browser has what Web Push
 * needs, "needs-install" when it would after adding the app to the Home
 * Screen (iOS and iPadOS offer push to installed web apps only), otherwise
 * "unsupported".
 */
export function pushSupport(): "ready" | "needs-install" | "unsupported" {
  if ("serviceWorker" in navigator && "PushManager" in window && typeof Notification !== "undefined") return "ready";
  if (isIOS() && !isInstalled()) return "needs-install";
  return "unsupported";
}

/**
 * Whose install instructions fit this browser: "ios" for an iPhone or iPad,
 * "mac-safari" for Safari on a Mac (File, Add to Dock), otherwise null.
 */
export function installPlatform(): "ios" | "mac-safari" | null {
  if (isIOS()) return "ios";
  const ua = navigator.userAgent;
  if (/Macintosh/.test(ua) && /Version\/[\d.]+.*Safari\//.test(ua) && !/Chrome|Chromium|Edg|Firefox|OPR/.test(ua)) {
    return "mac-safari";
  }
  return null;
}

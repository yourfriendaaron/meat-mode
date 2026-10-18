# Research: an Android app for Meat Mode (meat-g4x)

The question: how to ship an Android app that is the Meat Mode app, with notifications under its own name and icon, for hubs the user names at first run (often reachable only over a tailnet), on GitHub releases for Obtainium, with no Google dependency required. Reader notes: `push.md`, `shell.md`, `distribution.md`; brief: `brief.md`. Grades: A controlled or large n, B one team or small n, C anecdote, V vendor guidance.

## Rejected: a Trusted Web Activity

A local spike built a TWA whose launcher opens a hub address typed at runtime. On a Pixel running Android 17 with Chrome 155, Chrome verified a tailnet-only origin's `assetlinks.json` on the device and opened it full screen (B, n=1, measured). Notifications still showed as Chrome's. Chromium's permission-delegation README: "Apps that verify for an origin but don't handle Intents to it are ignored" (V), and an app cannot declare an ACTION_VIEW filter for a host it learns at runtime without claiming every https link. A self-hosted TWA client documents the same outcome for users' own servers (C). A generic TWA cannot give the app its own notifications.

## Findings

1. **WebView has no Web Push.** Push "requires browser UI" and may not be supported in WebView (V); service workers do run (V). The app needs a native push path. (`shell.md` Q2)
2. **UnifiedPush fits the hub's existing sender.** The app server side is standard Web Push: RFC 8291 aes128gcm, optional VAPID (V). Connector `org.unifiedpush.android:connector` 3.3.5, 2026-08-22 on Maven Central (V, checked). `onNewEndpoint` hands over the endpoint plus the p256dh and auth keys, and `onMessage` returns decrypted plaintext; the app passes only the VAPID public key (V, kdoc read in full by the reader). Wire payload limit 4096 bytes (V). (`push.md` Q1)
3. **Delivery depends on battery exemptions.** UnifiedPush gets no FCM-style wake allowance; Element X maintainers say both the distributor and the app must be unrestricted (C, 2024 and 2025). Users report ntfy.sh delays on mobile data fixed by a self-hosted ntfy or by Sunup (C, n of 2 to 3). Meat Mode's payload carries everything, so the app never has to wake and fetch. (`push.md` Q2, Q5)
4. **Hub changes for UnifiedPush endpoints.** Today the hub accepts only browser push services. A UnifiedPush endpoint is any https server, so the hub needs an SSRF guard: deny non-global addresses, with an operator allowlist for a tailnet or LAN push server (V: unifiedpush.org; **Inference** on the allowlist). Status handling: 404 and 410 delete, 429 honours Retry-After, 413 too large; ntfy answers 507 for a topic with no subscriber, so 507 alone must not delete (C, code read). UnifiedPush recommends confirming a new registration with a token pushed to it before trusting it (V). (`push.md` Q4)
5. **Embedded FCM fallback needs no Firebase project.** `embedded_fcm_distributor` 3.1.0 (2026-06-23) is used only when no distributor app is installed and Play Services is present. Since 3.0.0 (2025-01-31) it registers to FCM's Web Push endpoints and "is now fully FOSS"; for a server that sends VAPID, "Import the library and you're done!" (V, kdoc and release notes read). The hub already signs with VAPID and already accepts FCM endpoints. FCM-based distributors get NonFreeNet at IzzyOnDroid (V), so a build for that store would drop it in a separate flavor. (`push.md` Q3)
6. **Closest precedent: Home Assistant.** A WebView frontend plus native services. Notification buttons are broadcast PendingIntents to a receiver that calls the server, and that call uses a per-device credential from registration, not the WebView cookie (V/B). (`shell.md` Q1, Q5)
7. **Bridge between page and app.** Use `WebViewCompat.addWebMessageListener` with an exact https origin rule built from the hub address; `addJavascriptInterface` has no origin control (V, OWASP). WebView chores: `CookieManager.flush()`, `onShowFileChooser`, back navigation, safe-area CSS from WebView M144 (V). (`shell.md` Q2, Q3)
8. **Capacitor adds nothing here.** Its `server.url` is fixed at build time and documented as not for production (V). (`shell.md` Q4)
9. **Obtainium wants one APK per release.** It reads the newest non-draft release, takes the version from the tag and filters assets by regex; more than one matching APK stops background updates (B, source read). Publish the package ID and signing-certificate SHA-256 in the README for AppVerifier (V). (`distribution.md` Q1)
10. **The signing key is forever.** A different key fails in-place updates, so users uninstall and reinstall (V). Keystore as a base64 secret plus passwords, decoded in CI, and the tag build fails when they are missing (C). Key rotation exists for API 28+ but F-Droid reproducible setups do not support it (V). (`distribution.md` Q2)
11. **IzzyOnDroid refuses bare website wrappers** (V). The native push and notification handling is the added value. Keep any FCM code in a separate flavor so the default build stays clean (V, ntfy's practice). (`distribution.md` Q4)

## Design this supports (Inference)

- A Kotlin app in `android/`: first-run hub address, a WebView of the hub's own UI, native notifications from UnifiedPush.
- The page learns it runs in the app through the message bridge and asks the app, not PushManager, to turn reminders on.
- At registration the app gets a per-device token from the hub, revocable in Settings; notification buttons that act without opening the app (today `retry`) call the hub with it. Buttons that open something load the URL in the WebView.
- The hub accepts UnifiedPush endpoints behind the SSRF guard and allowlist, confirms new registrations, and handles 507 without deleting.
- Push comes through the user's distributor app when one is installed, else the embedded FCM distributor.
- With a distributor app, first run asks for the battery exemption; the app shows when the last push arrived.
- `release.yml` builds one universal, signed APK per tag; minSdk 26.

## Only a trial answers

- Delivery delay with ntfy and Sunup on a real phone in Doze, with the app and distributor exempted.
- Whether the hub UI behaves in WebView: keyboard, safe areas, file inputs, the event stream across sleep.

## Call

**Proceed** with a WebView app plus UnifiedPush, with the embedded FCM distributor as the fallback in the one GitHub APK: a phone with Play Services gets reminders with no second app, and a phone with ntfy or Sunup uses that instead. The per-device token is chosen over replaying the WebView's cookie (cheaper, but tied to web session expiry) on Home Assistant's precedent. Reminder pushes carry no buttons today; Done and Snooze on reminders would be new hub and app work, separate from the app itself. The package ID and signing key are permanent once the first APK ships.

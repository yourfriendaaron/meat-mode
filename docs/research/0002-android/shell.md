# Cluster 2: app shell (Q1 to Q5)

Date of research: 2026-10-18. Grades: A controlled/large n, B single team or vendor internal, C anecdote/opinion, V vendor guidance. Pages fetched via search highlights unless marked "read in full". None were read in full. Fetched text treated as data.

## Q1 How comparable apps build their Android client

1. Home Assistant companion is a WebView shell plus native services.
   - URL: https://developers.home-assistant.io/docs/android/architecture/overview/ (no date shown)
   - Quote: "The app's centerpiece is a WebView, which integrates with Home Assistant's frontend. Over time, native capabilities have been added, such as background sensor data collection." Also "The Home Assistant frontend is rendered inside a WebView, which the app talks to over external authentication and the external bus."
   - Answers Q1, Q3. Grade V (project's own docs). Highlights only.
   - Stated reason: HA "has been a frontrunner in PWA development", so the frontend is reused rather than rewritten. Native code is added only for things a web page cannot do.
2. Jellyfin Android is a hybrid: WebView UI plus native player and a JS bridge.
   - URL: https://deepwiki.com/jellyfin/jellyfin-android (auto-generated summary, undated, lists Kotlin 2.3.21, Media3 1.8.0)
   - Quote: "hybrid architecture that combines a WebView-based user interface with native Android components"; design "maximizes code reuse with the Jellyfin web client while leveraging native Android capabilities for operations like hardware-accelerated playback and background services". Bridge classes `NativeInterface` and `NativePlayer`, with a `nativeshell.js` facade.
   - Answers Q1, Q3. Grade C (third-party generated summary of the repo). Highlights only.
3. Immich mobile is Flutter, fully native UI, with its own local DB and background workers.
   - URL: https://deepwiki.com/immich-app/immich/2.4-mobile-application (undated)
   - Quote: "Immich Flutter mobile application ... Android WorkManager & Foreground Service" for uploads and sync. No stated reason found; the need is background photo backup, which a web page cannot do.
   - Answers Q1. Grade C. Highlights only.
4. Vikunja has a separate Flutter app, alpha, not a WebView of its web UI.
   - URL: https://github.com/go-vikunja/app ; https://community.vikunja.io/t/android-app-resurrection/593 (2022-04-10)
   - Quote: "This app is in alpha pre-release." Maintainer in 2022: "Ideally the app would have feature parity with the web frontend at some point." This is a cautionary example for a to-do app: a second UI lags the web UI.
   - Answers Q1. Grade C. Highlights only.
5. ImmichFrame Android: a small "Android app using system WebView" (47 stars). https://github.com/immichFrame/ImmichFrame_Android (undated). Existence proof for tiny Kotlin WebView shells. Grade C.
6. Kogan engineering blog on nested WebViews: https://devblog.kogan.com/blog/making-the-case-for-the-webview (2024-01-25). Argues WebView fits content that changes often and where web parity is costly; keep login state via cookies, hide web chrome, persist dark mode. Grade C.
7. Android's own guidance: https://developer.android.com/develop/ui/views/layout/webapps/ (undated, Google). WebView benefits: "Reuse existing code from your website", "Update content dynamically without ... app updates". Grade V.

Searched and found nothing usable: stated architecture reasons for ntfy, Nextcloud, Tusky, Mealie, Navidrome clients. No evidence gathered for them; do not cite them as precedent.

**Inference:** The precedent for "web UI first, native only where the web cannot reach" is Home Assistant and Jellyfin, both of which are large and active. The to-do-app precedent (Vikunja) shows the cost of the opposite choice.

## Q2 WebView limits that matter

1. Push and Notifications are the known WebView gap.
   - URL: https://web.dev/articles/web-on-android (Google, undated)
   - Quote: "When an API already has an OS-level alternative, like Web Bluetooth, or it requires browser UI to be implemented, like push notifications, it may not be supported." Summary lists "Push Notifications" among features WebView does not use.
   - Answers Q2. Grade V. Highlights only.
   - Chromium bug "Implement Push API and Notifications in WebView" (https://issues.chromium.org/40388442): page body not retrievable (login wall); only the title seen. Status unknown.
   - Stack Overflow https://stackoverflow.com/questions/58325327 : in a WebView the page reports "Push is not supported" while Custom Tabs works. Grade C, matches the vendor statement.
   - **Inference:** The hub PWA's `PushManager` path will not work inside the shell. Native registration (UnifiedPush) must replace it, and the web UI must detect the shell and skip web-push enrolment.
2. Service workers do work in WebView, API 24+.
   - URL: https://developer.android.com/reference/kotlin/android/webkit/ServiceWorkerController : `setServiceWorkerClient` "Added in API level 24"; set it "before any service workers are active". Compat class needs `WebViewFeature.SERVICE_WORKER_BASIC_USAGE` checked via `isFeatureSupported`. Grade V. Highlights only.
   - **Inference:** Offline caching in the existing service worker should keep working. A Capacitor thread (https://github.com/ionic-team/capacitor/discussions/5075, 2021-09-25, grade C) reports the Capacitor bridge was not injected on Android when a service worker served the page. That is a Capacitor-specific hazard; a plain WebView shell with `addWebMessageListener` is not affected the same way, but test it with the real service worker.
3. Cookies.
   - URL: https://developer.android.com/reference/android/webkit/CookieManager (Google). `flush()` "Ensures all cookies currently accessible through the getCookie API are written to persistent storage" (API 21+). `removeSessionCookies` removes cookies "without an expiration date". Third-party cookies default off for apps targeting Lollipop or later. Grade V.
   - Stack Overflow https://stackoverflow.com/questions/45751626 : persist by calling `CookieManager.getInstance().flush()` (e.g. in `onPageFinished`). Grade C.
   - Chromium docs: https://chromium.googlesource.com/chromium/src/+/HEAD/android_webview/docs/cookies.md . WebView uses the app's data directory, "each app has a separate cookie jar" (https://chromium.googlesource.com/chromium/src/+/refs/heads/main/android_webview/docs/architecture.md). Grade V.
   - **Inference:** The hub session cookie must carry an expiry (not a session cookie) and the app should call `flush()` after login. The jar is private to the app, so the native side must read it itself.
4. File inputs: without `WebChromeClient.onShowFileChooser`, `input.click()` "silently no-ops" (https://github.com/counsel-health/counsel-embedded-example-apps/pull/91, undated, grade C). The callback must always be completed, including on cancel.
5. Back button: intercept and call `goBack()` when `canGoBack()`, else leave (https://developer.android.com/develop/ui/compose/migrate/interoperability-apis/wrap-webview-in-compose , Google). Grade V.
6. Safe areas and keyboard: https://developer.android.com/develop/ui/views/layout/webapps/understand-window-insets (Google). WebView exposes CSS `safe-area-inset-*` from M136 (fullscreen only) and for all WebViews from M144. IME resizes the visual viewport from M139, bottom only. Warning: web code must not blur focus on resize or the keyboard closes in a loop. Grade V. Third-party field report (https://lanesawyer.github.io/super-productivity/development/android-edge-to-edge-keyboard/, update dated 2026-06-22, grade C): do not inset for the keyboard by assumption; real Android 16 devices still resize the window, and double insetting squashed the WebView to a sliver.
   - **Inference:** Use `env(safe-area-inset-*)` in the hub CSS, do not add native IME padding unless a device test shows the window does not resize, and test on API 30 and 35+.
7. WebView version and updates: updates ship through Google Play system updates (Android 10+), https://aospbooks.github.io/aosp-internal-book/45-webview/ (undated, grade C); users can pick another provider on Android 7+; `WebViewCompat.getCurrentWebViewPackage()` "can return null ... lacks an updatable WebView implementation" (https://developer.android.com/develop/ui/views/layout/webapps/managing-webview , V). Android's page also says WebView updates "release globally through Google Play" (https://developer.android.com/develop/ui/views/layout/webapps/in-app-browsing-embedded-web , V). Devices without Play must supply their own WebView (https://github.com/chromium/chromium/blob/main/android_webview/docs/webview-providers.md , highlights only).
   - **Inference:** On no-Google devices (the target audience) the WebView may be old. Gate features with `WebViewFeature.isFeatureSupported` and show a clear "update your WebView" message. Insets support needs M144 for non-fullscreen use, so CSS must degrade.
8. Pull-to-refresh and offline: no authoritative source found in this pass. **Inference:** wrap the WebView in `SwipeRefreshLayout` only if the page is scrolled to top; otherwise it fights inner scrolling lists. Offline relies on the service worker cache; the shell itself needs an error page when the hub is unreachable (a Capacitor user reports exactly this need, https://github.com/ionic-team/capacitor/discussions/5075).

## Q3 JavaScript bridge

- Primary: https://developer.android.com/develop/ui/views/layout/webapps/native-api-access-jsbridge (Google, undated, grade V, highlights only).
  - `addWebMessageListener` is "the most modern and recommended approach". It requires `allowedOriginRules`; WebView matches scheme, host and port, ignores paths, and wildcards only lead the host (`https://*.example.com`). Requires WebView 82 / androidx.webkit 1.3.0. Call it before `loadUrl`/navigate. Callback runs on the main thread; offload work.
  - `addJavascriptInterface`: "available to every frame within the WebView, including iframes. It lacks origin-based access control." Android: "We don't recommend using this method for modern applications".
  - Caveat: "origin rules ... can't protect against cross-site scripting (XSS) vulnerabilities within your own trusted domain"; validate payloads before sensitive native operations.
- API reference: https://developer.android.com/reference/androidx/webkit/WebViewCompat : "The HTTPS scheme is strongly recommended ... allowing HTTP origins exposes the injected object to any potential network-based attackers." A `*` wildcard injects into all frames and messages must be treated as untrustworthy. Also valid: IP literal rules (`https://127.0.0.1`), custom scheme rules. Grade V.
- Security overview: https://developer.android.com/privacy-and-security/risks/insecure-webview-native-bridges (V) and OWASP https://mas.owasp.org/MASTG/best-practices/MASTG-BEST-0035/ and https://mas.owasp.org/MASWE/MASVS-PLATFORM/MASWE-0033/ (undated, grade V/B): minimise bridge surface, restrict to trusted origins, treat every message as untrusted input, prefer `addWebMessageListener`.
- **Inference for a user-chosen origin:** the origin is not known at build time, so build the rule set at runtime from the saved hub URL as the exact `https://host[:port]` only (no wildcard, no http). Reject non-https hub addresses or keep the bridge off for them. Never expose the bridge to other origins reached by in-page links; open external links in the system browser. Keep the bridge to a few verbs (for example, hand over the UnifiedPush endpoint request, report the shell version) and validate each message. The hub UI is first-party, but a user-chosen hub means the app trusts whatever the user typed; say so in the setup screen.

## Q4 Capacitor and a remote user-chosen URL

- Docs text for `server.url` (quoted in https://github.com/ionic-team/capacitor/discussions/5075 , 2021-09-25, grade C as a quote of grade V source): "Load an external URL in the Web View. This is intended for use with live-reload servers. **This is not intended for use in production.**" The live reload guide (https://capacitorjs.com/docs/guides/live-reload , Ionic, undated, V) tells users to remove the `server` entry: "Be careful not to commit the server config to source control."
- Maintainer reason (https://github.com/ionic-team/capacitor/discussions/4080 , 2021-01-18, grade C, a maintainer comment): "the only reason we added the server.url config option was because of live reload servers ... we don't recommend to use it in production apps because ... it can lead to app rejections, so use at your own risk." Also https://github.com/ionic-team/capacitor/discussions/5183 (2021-10-28): documented so "because it can lead to app rejections."
- Practitioners run it in production on Google Play and the App Store (same threads, grade C). Reported problems: native plugins in the web bundle can go out of step with the installed native app; no internet means no app, so a native error page is needed.
- `server.url` is baked into `capacitor.config` at build time. No evidence found of a documented way to set it at first run. **Inference:** a Capacitor shell for a user-chosen origin would need custom native code (changing the bridge's server URL at runtime), which removes most of Capacitor's convenience. The reason against is store policy, which does not apply to an APK on GitHub; the technical reasons (plugin version skew, offline) apply to a plain WebView shell equally. Capacitor also brings no UnifiedPush plugin; that is native work either way. Searched for an official Capacitor statement on user-chosen URLs and found none.

## Q5 Notification actions handled natively with the app closed

- Home Assistant Android, MessagingManager.kt: https://github.com/home-assistant/android/blob/main/app/src/main/kotlin/io/homeassistant/companion/android/notifications/MessagingManager.kt (current main, highlights only, grade B/V).
  - Each action becomes `NotificationCompat.Action` with `PendingIntent.getBroadcast(... NotificationActionReceiver ...)`, `FLAG_IMMUTABLE`, `setShowsUserInterface(false)`, and `setAuthenticationRequired(...)` (Android 12+, "supported on all versions" per https://companion.home-assistant.io/docs/notifications/actionable-notifications/ ). Max 3 actions on Android, per the same docs page.
  - The receiver fires an event to the server through `serverManager.integrationRepository(...).fireEvent(...)`.
- Auth is not the WebView cookie. HA registers the device once and gets a webhook id plus optional secret; the server webhook handler looks the config up by `webhook_id` and handles `fire_event` (https://github.com/home-assistant/home-assistant/blob/dev/homeassistant/components/mobile_app/webhook.py , highlights only, grade B/V). The WebView itself gets tokens through a separate "external authentication" hook (https://developers.home-assistant.io/docs/api/native-app-integration/webview/ ). The app also keeps the data in Room and uses OkHttp and Retrofit for REST.
- **Inference for Meat Mode:** copying HA means the native action path should have its own credential, not the cookie. Options, with cost:
  - (a) Read the cookie with `CookieManager.getCookie(hubUrl)` in the receiver and replay it. Cheapest, but it ties actions to web session expiry and cookie flags (HttpOnly cookies are still returned by `getCookie` on the native side; verify on device).
  - (b) Mint a per-device action token at first run through the bridge and store it in app storage; the receiver and WorkManager use it with OkHttp. More hub work (a token endpoint and revoke), matches HA, survives cookie loss.
  - Use the receiver to hand off to a WorkManager job (network-constrained, retry) for the POST so a flaky tailnet does not drop Done/Snooze; call `goAsync()` or enqueue unique work. This WorkManager advice is not from a source fetched here; treat it as unverified.
- Gap: I did not read `NotificationActionReceiver.kt` itself, so the exact retry and offline handling HA uses is unconfirmed.

## Gaps

- No source found on architecture reasons for ntfy, Nextcloud, Tusky, Mealie, Navidrome.
- Chromium push-in-WebView bug body unreadable; current status unverified. The "Push not supported in WebView" claim rests on Google's web.dev text (undated) and one 2019 Stack Overflow post.
- Pull-to-refresh and offline error-page behaviour: no authoritative source.
- Whether `getCookie` returns HttpOnly cookies on device, and whether Capacitor can change `server.url` at runtime: not verified. Both need a spike.

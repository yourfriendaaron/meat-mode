# Cluster 1: push (UnifiedPush) findings

Searched 2026-10-18. Grades: A controlled/large n, B single team/small n, C anecdote/opinion, V vendor/project guidance. Most unifiedpush.org pages carry no publication date; the Codeberg tag dates stand in.

## Q1 Connector library

1. Latest connector release is tag 3.3.5, committed 2026-08-22, built with AGP 9.3.1. Coordinates `org.unifiedpush.android:connector:{VERSION}`. Requires Android 4.1+. Depends on `tink`; duplicate-class conflicts are fixed by substituting `tink-android:1.20.0`.
   - https://codeberg.org/UnifiedPush/android-connector (repo listing, 2026-08-22); https://unifiedpush.org/kdoc/connector/ (undated). Q1. V. Read in full (connector page), listing only (repo).
2. Registration: declare a non-exported `PushService` subclass handling action `org.unifiedpush.android.connector.PUSH_EVENT`. Call `UnifiedPush.tryUseCurrentOrDefaultDistributor(activity){ ok -> UnifiedPush.register(context, instance, messageForDistributor, vapid) }`. `saveDistributor` must precede `register`. `instance` gives multiple registrations. If several distributors are installed and no default is set, the OS shows a picker; the docs recommend a dialog explaining it first.
   - https://unifiedpush.org/kdoc/connector/ and https://unifiedpush.org/kdoc/connector/org.unifiedpush.android.connector/-unified-push/register.html. Q1. V. Read in full.
3. Endpoint and keys arrive in `PushService.onNewEndpoint(endpoint: PushEndpoint, instance)`. `PushEndpoint` = `url`, `pubKeySet: PublicKeySet?` (nullable), `temporary`. `PublicKeySet(pubKey, auth)`: pubKey is the P-256 uncompressed key, auth is the auth secret, both base64url without padding. The hub gets these as the p256dh and auth of a standard Web Push subscription. "The new endpoint should be sent to the application server."
   - https://unifiedpush.org/kdoc/connector/org.unifiedpush.android.connector.data/-push-endpoint/index.html and .../-public-key-set/index.html (both © 2026). Q1. V. Read in full.
4. Decryption: yes. `onMessage(message: PushMessage, instance)` "contains the decrypted content of the push message". The library holds the keys (default `DefaultKeyManager`, overridable via `register(..., keyManager)`), so the app never touches RFC 8291 itself. The example app has a toggle to show an error when decryption fails.
   - https://unifiedpush.org/kdoc/connector/org.unifiedpush.android.connector/-push-service/ ; https://github.com/UnifiedPush/android-example/ . Q1. V. Highlights only.
5. VAPID: the app passes only the VAPID public key to `register(vapid=...)`, base64url, uncompressed, exactly 87 chars; a malformed key throws. The private key stays on the hub. The distributor forwards the key to the push server, which can then check the hub's `Authorization: vapid` header. If a push server requires VAPID and none was given, the distributor answers `REGISTRATION_FAILED` with reason `VAPID_REQUIRED`.
   - register kdoc above; https://unifiedpush.org/developers/spec/android/ (undated). Q1. V. Highlights only for the spec.
6. Spec limits: push message 1..4096 bytes on the wire (cleartext at most 3993 bytes); endpoint at most 1000 bytes; registration message at most 100 bytes. Android spec 3.0 made Web Push mandatory: `Content-Encoding: aes128gcm`, a TTL header required, push server must return 201.
   - https://unifiedpush.org/developers/spec/android/ ; https://codeberg.org/UnifiedPush/specifications/pulls/1 (2024-09-05). Q1, Q4. V. Highlights only.

**Inference:** `onMessage` gives the app the plaintext, so the existing hub payload (title, body, task id, action URLs) can stay as is and the app builds the notification and Done/Snooze buttons itself. Keep the payload under about 3.9 KB cleartext.

## Q2 Distributors

7. Official list of distributors: Sunup, ntfy, gCompat-UP (FCM), NextPush, Conversations (XMPP), KUnifiedPush (Linux). Default servers: Sunup, ntfy, gCompat-UP. Self-hosted: ntfy server (package, Docker, binary), NextPush (needs a Nextcloud), Conversations (rewrite proxy), Sunup (Mozilla autopush-rs).
   - https://unifiedpush.org/users/distributors/ (undated). Q2. V. Read in full.
8. Sunup uses WebSockets with Mozilla autopush-rs, source on Codeberg, funded by COVESA, on F-Droid and IzzyOnDroid. Its setup asks the user for notification and battery-optimization exemptions.
   - https://unifiedpush.org/users/distributors/sunup/ . Q2. V. Read in full.
9. gCompat-UP 2.1.2 (2026-09-08) on IzzyOnDroid, tagged `NonFreeNet` ("connects to Google Servers"). It is the Google-backed distributor, not a library inside the app.
   - https://apt.izzysoft.de/fdroid/index/apk/org.unifiedpush.distributor.fcm . Q2, Q3. B (listing). Highlights only.
10. Delivery under Doze, mixed reports:
   - Element X maintainers, Oct 2025: on Android 15+ background limits hit UP; ntfy "can't wake" the app or the app gets no network; they suggest Sunup ("contains some code to wake up the app"), unrestricted battery for both apps, or a WorkManager sync option. https://github.com/element-hq/element-x-android/issues/4828 (2025-06-05, comments to Oct 2025). Q2, Q5. C. Highlights only.
   - Element X maintainer MatMaul, 2024-07: an FCM-woken app gets special treatment from the OS "this is NOT the case for UP based push"; both the distributor and the app must be unrestricted. https://github.com/element-hq/element-x-android/issues/3031 . Q2, Q5. C. Highlights only.
   - ntfy-android #104, a user on ntfy.sh: no delivery for minutes to hours on mobile data with the screen locked, fine on Wi-Fi; gCompat worked instantly. 2026-08-18 follow-up: after moving to their own ntfy server (v2.27) "notifications work very reliably and arrive immediately", battery use slightly above FCM. 2025-09 comment: Sunup woke Element X in forced idle (`dumpsys deviceidle force-idle`). https://github.com/binwiederhier/ntfy-android/issues/104 . Q2, Q5. C, n=2 to 3. Highlights only.
   - Wake latency benchmark (Pixel 6 Pro, 2026-06-16): ntfy wake about 0.85 s vs FCM 0.89 s median, but only after both ntfy and the target app were battery-exempt and in the `active` standby bucket; otherwise the wake failed silently. Reason given: only a high-priority FCM message gets Play Services' temporary ~10 s allowlist that lets the app start a foreground service from the background. https://github.com/Monkopedia/rouse-context/issues/483 . Q2. B (one dev, one device). Highlights only.
   - ntfy maintainer 2023: normally about a second, a minute or two in deep sleep. https://github.com/binwiederhier/ntfy/issues/702 (2023-04-18). Q2. C.
   - ntfy.sh (public server) rate-limits: Matrix users share limits and lose notifications; advice is to self-host or use another server. Same Element threads. Q2. C.
11. Setup chores users hit: the distributor must be unrestricted, not just "allow background usage"; mobile-data "unrestricted" must be on even with data saver off; ntfy should ask for unrestricted battery before the first registration (it could not start a foreground service otherwise). https://github.com/binwiederhier/ntfy-android/issues/95 (2024-11-02, closed 2025-09-21). Q2. C.

**Inference:** with no Google at all, expect that delivery works but needs the user to exempt both the distributor and Meat Mode from battery limits; the app should deep-link or ask for that on first run and show a "last push received" indicator. A self-hosted ntfy (same tailnet user) or Sunup is the likeliest good pairing; public ntfy.sh rate limits matter less for a single user with a few reminders a day. Meat Mode's handler is tiny (build a notification from plaintext) so the "wake the app then fetch" problem Element has does not apply; the hub payload already carries everything.

## Q3 Embedded FCM distributor

12. `embedded_fcm_distributor` 3.1.0 (2026-06-23) acts as an in-app UP distributor that passes pushes via FCM; "It doesn't contain any Google proprietary blobs." It registers to "FCM WebPush endpoints" (credit to a public gist), not through the Firebase SDK. Requires a device with Play Services and no other distributor.
   - https://codeberg.org/UnifiedPush/android-embedded_fcm_distributor (2026-06-23); https://unifiedpush.org/kdoc/embedded_fcm_distributor/ . Q3. V. Read in full.
13. VAPID is mandatory for FCM. If the hub sends VAPID, "you have nothing special to do" and the library exposes its own receiver. If not, a gateway that adds VAPID is needed (a `DefaultGateway` exists, "for your tests, or if you have a small userbase. Please host your own gateway if possible"). Meat Mode already sends VAPID.
   - kdoc above. Q3, Q4. V. Read in full.
14. Searched and found nothing stating whether it needs the developer's own Firebase project or a `google-services.json`. The docs list no such step (add dependency, done), which suggests it does not. **Inference, untested:** the FCM webpush registration uses a project configuration compiled into the library, so the app developer needs no Firebase project. Verify by reading the library source or building a spike before relying on it.
15. F-Droid and Izzy treatment: no source found for the embedded library specifically. The related gCompat-UP gets `NonFreeNet` on IzzyOnDroid. **Inference:** a flavor with the embedded FCM library would likely be flagged NonFreeNet (and possibly a dependency flag) by F-Droid and IzzyOnDroid, so keep it in a separate build flavor or leave it out of the GitHub APK the F-Droid-style repos would scan. Cluster 3 owns the repo-policy side.

## Q4 Hub side with a UP endpoint

16. Web Push semantics are unchanged: same RFC 8030 POST, aes128gcm, `TTL`, optional `Urgency`/`Topic`, VAPID. The UP side adds: endpoint is a capability URL (no auth beyond secrecy), push server must reply 201, `TTL` is required, payload at most 4096 bytes. So webpush-go works unchanged against ntfy or autopush endpoints.
   - https://unifiedpush.org/developers/spec/android/ ; https://codeberg.org/UnifiedPush/specifications/pulls/1 . Q4. V.
17. Autopush (Sunup's server) status codes: 201/20x accepted; 400 bad parameters (errno 112 invalid TTL: allowed 0 to 2592000 s; errno 113 invalid Topic: ASCII alphanumeric only, max 32 bytes); 401 bad VAPID auth; 404 endpoint not found and not to be used again; 410 expired/permanently unavailable; 413 payload too large (max 4028 chars); 429 with `Retry-After`.
   - https://mozilla-services.github.io/autopush-rs/errors.html (undated). Q4. V. Highlights only.
18. 404 and 410 both mean delete the subscription (RFC 8030: expired subscription MUST be signaled with 404; web.dev table: 404 and 410 both remove it, 429 honour `Retry-After`, 413 too large). https://web.dev/articles/push-notifications-common-issues-and-reporting-bugs (undated); https://github.com/binwiederhier/ntfy/issues/664 . Q4. V.
19. ntfy quirk: for topics with no subscriber ntfy answers 507 (not 4xx) to UP publishes, because Mastodon deletes subscriptions on any 4xx except 429/408; a later change makes ntfy return 410 after a topic expired (PR #842 referenced). **Inference:** the hub should treat 404 and 410 as permanent, treat 5xx and 429 as retry with backoff, and not delete a subscription on 507 alone (or delete after N consecutive failures over days). https://github.com/binwiederhier/ntfy/issues/664 (2023); https://github.com/binwiederhier/ntfy/blob/main/server/server.go . Q4. C and code read, highlights only.
20. ntfy UP topics start with `up`; self-hosters with ACLs must keep `/up*` world-writable, and rate limits apply (429s in logs). https://unifiedpush.org/users/troubleshooting/self-hosted-ntfy/ . Q4. V.
21. SSRF: the already-covered intro page says validate registrations and deny private-IP endpoints. A practical wrinkle: Synapse needs `ip_range_whitelist` to reach a self-hosted push server on a private IP, "a very common error" (https://unifiedpush.org/users/troubleshooting/self-hosted-with-matrix/ , undated, V). **Inference:** Meat Mode's hub is likely on a tailnet and a self-hosted ntfy may be on a private or CGNAT address, so a blanket private-IP deny will break the best setup. Offer an operator setting (default deny, opt-in allowlist of hosts or CIDRs) and require `https` plus a redirect-free, short-timeout POST. Searched for a written SSRF recipe beyond the intro page and found none.

## Q5 Failure reports (summary)

22. Element X issues #4828 (2025) and #3031 (2024): UP via ntfy fails when the app is battery-optimized, on Android 15+, or when the push server is ntfy.sh. Fix list in section Q2 item 10. Fixed for one user in 2026-02 by switching to Sunup ("Whoa, that seems to have fixed it!"). C.
23. ntfy-android #104: delayed or missing pushes on mobile data until self-hosting (reported 2025-01 to 2026-08). C.
24. Tusky, Fedilab, Nextcloud, Immich: searched only through the general queries above; no specific UP delivery-failure threads surfaced from those apps. Gap, not "none exist".

## Gaps

- Maven Central publication date of connector 3.3.5 not checked (Codeberg commit date used).
- No firm answer on whether the embedded FCM library needs a developer Firebase project, or how F-Droid/Izzy flag it.
- No Tusky, Fedilab, Nextcloud, Immich failure threads read.
- No independent measurement of Sunup vs ntfy latency under Doze with n greater than a few devices; all evidence is anecdote.
- Not checked: whether the connector's default key manager survives app data clear or reinstall (affects when the hub must replace subscriptions).

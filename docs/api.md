# Wire contract

The hub serves two HTTP APIs: one for the app (`/api/…`) and one for bridges and agents (`/bridge/v1/…`). The Go types are in `internal/api`; this page is the prose version. All bodies are JSON. Times are RFC 3339 strings in UTC. Errors are `{"error": "<plain message>"}` with a 4xx or 5xx status.

## Identity

- A **bridge** has a unique name chosen when it is added (`[a-z0-9-]{1,32}`). The name `hub` is reserved.
- The **personal list** belongs to the hub itself: it is repo `personal` of the reserved bridge `hub`, with `personal: true`. The hub stores its items and applies every action on them at once, so it works with no bridge running. Its items are keyed `hub/personal/<id>`; the hub gives a new item the id `mm-<n>`, and keeps the bead id of an item moved from a bridge (see below).
- A **project** is another list the hub keeps, for a repo with no tracker: repo `<name>` of the bridge `hub`, with `personal: false`, so its items are keyed `hub/<name>/<id>`. Its name is a repo name, such as `projects/garden`; `personal` is kept for the personal list. An agent's first add to it makes it (see Agents: projects). Its items behave like personal ones in every way below: every action is applied at once, `ask` is refused, and they can be edited, repeat and reach the calendar feed. A project with no open item and none closed in the last 7 days is left out of `State`, and so is a project deleted in the app (see Deleting a project).
- A **repo** is named by the bridge with its folder's path relative to the home folder, such as `projects/garden` (for a `--root` outside the home folder, relative to that root's parent). Repo names are unique per bridge, so two folders called `garden` stay apart. A name is up to 512 bytes of `/`-separated segments; each segment is 1 to 128 bytes with no control character, is not `.` or `..`, and does not start with `-`. The app shows the last segment, and the path where two repos share it.
- An **item key** is `<bridge>/<repo>/<bead id>`, for example `laptop/projects/garden/gd-1`. Bridge names and bead ids hold no `/`, so the repo is everything between the first and the last `/`. In URL paths the key is percent-encoded as one segment. Bead ids may not start with `-`; the bridge passes bead ids to `bd` only when they match `[A-Za-z0-9][A-Za-z0-9._-]{0,127}`.
- A hub that stored repos under an older bridge's folder names (`garden`) moves each one, with its items, actions and reminders, to the one new name that ends in it (`projects/garden`) when the bridge first sends its new list. A folder name that two new names end in is dropped instead.
- An **action id** is a UUID the client generates. Posting the same id twice returns the stored action and changes nothing.

## App API

The hub refuses to start without an `https://` `MEAT_MODE_PUBLIC_URL` (plain `http://` on localhost only). It answers only on a private network (see Private network only below), and the app always has a login (see Login). On top of that, the hub refuses requests another site could make from the user's browser:

- A `Host` other than `localhost`, `127.0.0.1`, `[::1]`, the host of `MEAT_MODE_PUBLIC_URL` or one listed in `MEAT_MODE_ALLOWED_HOSTS` gets `421` (on `/api/` and the app's pages).
- An `/api/` request (any method) or a non-GET page request whose `Origin` is neither the request's own origin nor that of `MEAT_MODE_PUBLIC_URL` gets `403`, and so does an `/api/` request whose `Sec-Fetch-Site` is `same-site` or `cross-site`. A plain subresource load such as an image carries no `Origin`, so in a browser too old to send `Sec-Fetch-Site` a hostile page can still tie up event stream slots.
- A non-GET `/api/` request without `Content-Type: application/json` gets `415`, bodiless ones included. The two routes that take a file, `POST /api/items/{key}/attachments` and `POST /api/restore`, take `application/octet-stream` instead (restore takes either). Both types make a browser ask before a cross-site send, as JSON does; `multipart/form-data` and `text/plain` would not, so neither is taken.

Every response carries `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, `X-Frame-Options: DENY`, `Cross-Origin-Opener-Policy: same-origin` and a `Content-Security-Policy` with `frame-ancestors 'none'`.

### Private network only

The hub can wake agents on the user's machines, so it refuses to be reached from the internet. The **private set** is loopback, RFC 1918, the RFC 6598 shared range Tailscale uses, IPv4 and IPv6 link-local and unique-local IPv6 (`fc00::/7`), judged after unwrapping an IPv4-mapped IPv6 address and dropping a zone. `MEAT_MODE_TRUSTED_NETWORKS` adds the operator's own networks outside it, such as a LAN's global IPv6 prefix: comma-separated CIDRs or addresses, each at most `/16` for IPv4 and `/48` for IPv6. A wider entry, or one that is not a CIDR, stops the hub at start with a plain message. The private set plus those networks is the **allowed set**.

**Header guard.** Every request, pages, `/api/`, `/bridge/`, `/calendar/` and `/healthz` alike, gets `403` before any other check when:

- it carries a `Tailscale-Funnel-Request` header, with any value (Tailscale Funnel marks what it forwards from the internet);
- any value in `X-Forwarded-For` (every entry of every header line), `X-Real-IP`, `CF-Connecting-IP`, `True-Client-IP`, or a `for=` parameter of `Forwarded`, is outside the allowed set, or is not an address at all (`unknown` and obfuscated names included);
- or the direct peer address is outside the allowed set.

The guard only refuses. A forged header can make a request look more public, never less, so it needs no list of trusted proxies, and no setting turns it off. On `/api/` the refusal is `{"error": "…"}`; elsewhere it is plain text. Both say the hub answers only on a private network and name what was refused. The hub logs refusals, at most one line a minute for each kind. Inside a container with published ports the peer is the container network's gateway, so the peer part protects only a hub run as a plain binary or with host networking; the header part holds either way.

**Public DNS check.** At start and then every hour, the hub asks public resolvers (Cloudflare, Google and Quad9, over plain DNS) for the A and AAAA records of `MEAT_MODE_PUBLIC_URL`'s host. MagicDNS and a LAN's own DNS answer only locally, so a name that public DNS knows is one the internet can find, as Tailscale Funnel's are.

- An address outside the allowed set stops the hub at start, with a message naming the addresses and how to fix it (turn Funnel off, or remove the public record). Funnel's public records can linger for a few hours after it is turned off.
- After start, such an address is logged and shown as a warning in Settings; the hub keeps running, since a stale record or a home domain pointing at a router that forwards nothing is common.
- No record (NXDOMAIN or none of either type), or private addresses only, passes.
- When no public resolver answers within 5 seconds, the hub logs that it could not check, and starts or keeps running.
- A host that is `localhost` or an IP address in the allowed set is not looked up; a public IP address counts as a public record.

`GET /api/setup/status` reports the latest result as `publicDns`. `state` is `private`, `public`, `unchecked` (no answer yet, or no resolver answered) or `skipped` (localhost or an allowed IP address); `addresses` lists the addresses outside the allowed set, for `public` only; `checkedAt` is when the latest check finished, `null` before the first.

**The bridge.** `meat-mode bridge pair` refuses a hub URL whose host resolves to any address outside the private set and the bridge's own `MEAT_MODE_TRUSTED_NETWORKS`, which pairing saves in `bridge.json` as `trustedNetworks`. Every connection to the hub checks again and dials the address it checked, so DNS that changes later cannot point the bridge at a public server; the bridge uses no proxy from its environment for the hub. A refused dial is logged as plain text and retried like a network error.

| Method and path | Body | Response |
|---|---|---|
| `GET /api/state` | | `State`: every item, repo and bridge, the count of unapplied actions, server time |
| `GET /api/events` | | Server-sent events: `event: change` with `data: {"rev": n}` whenever state changes; the app then re-fetches `/api/state`. `event: epics` with `data: {}` when the hub takes or drops a repo's epic rows; the app then re-fetches `/api/epics` if it shows them, and never `/api/state`. Epics events coalesce apart from change events, and a store change that only touches epic rows sends no `change`. Past 32 open streams the hub answers `503` with `Retry-After: 30`. |
| `GET /api/epics` | | `Epics`: the epic rows of every repo whose epics are on (see Epics) |
| `GET /api/icons/{hash}` | | A repo icon's bytes, by the `icon` hash a `Repo` in `State` carries (see List identity). `404` for a hash the hub holds for no repo, `400` for one that is not 32 lowercase hex digits |
| `POST /api/actions` | `NewAction` | `201` with `Action` (or `200` with the stored one for a duplicate id) |
| `POST /api/actions/{id}/cancel` | | `200` with `Action` (status `cancelled`) when it was not yet applied; `409` when it already was |
| `GET /api/actions?limit=n` | | Recent actions, newest first, for the sync popover; `limit` defaults to 50 and is capped at 500 |
| `GET /api/items/{key}/history?limit=n` | | The actions on one item, newest first, for the task sheet's History: every action on it, whatever its status, and the `add` of each of its sub-tasks. `limit` works as for `GET /api/actions`. The hub keeps the whole action log, so it reaches back to the item's `add`. `404` for an item the hub does not hold, `400` for a key that is not one |
| `POST /api/items/{key}/seen` | | `204`; marks replies on the item read |
| `GET /api/push/key` | | `{"publicKey": "<VAPID public key>"}` |
| `POST /api/push/subscriptions` | `PushSubscription`: the browser's JSON, plus optional `"kind"` (`web`, the default, or `app`) and `"name"` (up to 64 characters; the hub trims it and uses `Browser` or `Android app` when it is empty) | `web`: `204` (upsert by endpoint); `400` unless the endpoint is an https URL on a known push service (Mozilla, FCM, Apple, Windows). `app`: `202` with `{"device": "<id>"}` and the hub pushes a confirmation to the endpoint (see Devices); `204` when that endpoint already belongs to a confirmed device, which renews it and keeps its token, and the hub pushes it a fresh code too, so an app that lost its token gets a new one by confirming (the old token stops then); `400` unless the endpoint passes the endpoint rule (see Devices). The hub keeps the 20 newest subscriptions of both kinds. |
| `DELETE /api/push/subscriptions` | `{"endpoint": "…"}` | `204` |
| `POST /api/reminders/test` | `{"endpoint": "…"}`, this device's push subscription (`""` when it has none) | `{"push", "ntfy"}`: sends a test reminder to that subscription only, and to ntfy when set up. With a login the subscription must belong to the calling session. `push` is `sent` (the push service accepted it), `none` (this device has no subscription), `expired` (the push service answered that the subscription is gone, or its endpoint is not a known push service, so the hub removed it and the app should subscribe again) or `failed`; `ntfy` is `sent`, `failed` or `off`. `404` when push is not set up. |
| `GET /api/devices` | | `[]Device`, newest first (see Devices) |
| `DELETE /api/devices/{id}` | | `204`; revokes the device: drops its push subscription and its device token. `404` for an unknown id. |
| `POST /api/devices/{id}/confirm` | `{"code": "…"}` | `200` with `{"token": "…"}`, the device token, shown once. Needs no session: the code is the credential. `403` for a wrong, used or expired code. |
| `PUT /api/devices/self` | `PushSubscription` (`endpoint` and `keys` only) | `204`; replaces the calling device's endpoint and keys and renews it. Device token only. `400` as for `app` registration. |
| `DELETE /api/devices/self` | | `204`; the calling device revokes itself. Device token only. |
| `GET /api/bridges` | | `[]Bridge`: `{"name", "createdAt", "lastSeen", "version", "bdVersion", "trackers"?, "harness"?}`, with `trackers` the `{"name", "version"?}` list and `harness` the harness its last hello sent (see Harness). In `State`, a bridge also carries `limitsPending: true` while a `limits` action for it waits (see Harness: limits) |
| `POST /api/bridges` | `{"name": "…", "password"?: "…"}` | `201` with `{"name", "token"}`; the token is shown once. `403` with `"reauth": true` unless the password was entered within the last 10 minutes or `password` is right (see Login) |
| `DELETE /api/bridges/{name}` | | `204`; revokes the token |

### Login

The app always needs a login. The password hash is one set in the app (see Password below), or one from the environment (`MEAT_MODE_PASSWORD_HASH`, or `MEAT_MODE_PASSWORD_HASH_FILE` naming a file that holds it), which wins over the app's. The hash is an Argon2id string in PHC format (`$argon2id$v=19$m=…,t=…,p=…$<salt>$<hash>`), printed by `meat-mode hash-password`. Setting both variables, or a value that is not such a string, stops the hub at start with a plain message.

**A hub with no password** serves only its setup: `GET /api/session` reports `{"login": false, "authenticated": false}`, `POST /api/setup/claim` sets the password (see Password), `POST /api/logout` answers `204` and does nothing, `POST /api/login` answers `404`, and every other `/api/` route answers `401` with `{"error": "this hub has no password yet: set one with the setup code"}`. The app then shows the claim step and nothing else. The bridge API keeps working for tokens made earlier.

With a password, every `/api/` route except the three below, `POST /api/setup/claim`, `POST /api/devices/{id}/confirm` and the routes a device token opens (see Devices) answers `401` with `{"error": "log in first"}` unless the request carries a valid session cookie. That includes `GET /api/events`, so an app whose session ended sees its event stream fail and checks `GET /api/session` before reconnecting. An open stream ends at its next heartbeat, within 25 seconds, after its session ends. The three routes below still pass the Host, Origin and Content-Type checks above; they only skip the session check. The app's pages and static files stay public; they hold no data. The bridge API keeps its bearer tokens and `/healthz` stays public.

| Method and path | Body | Response |
|---|---|---|
| `GET /api/session` | | `{"login": bool, "authenticated": bool}`: whether this hub has a password yet, and whether this request is logged in |
| `POST /api/login` | `{"password": "…"}` | `204` and the session cookie; `401` `{"error": "wrong password"}`; `429` with `Retry-After` after 5 failed logins within a minute, counted for the whole hub; `404` when the hub has no password yet |
| `POST /api/logout` | | `204`; ends this session, clears the cookie and drops the push subscriptions and devices it registered |

The session cookie is `__Host-meat_mode_session` when `MEAT_MODE_PUBLIC_URL` is `https://`, and `meat_mode_session` on plain `http://` (localhost only): 32 random bytes, `HttpOnly`, `SameSite=Lax`, `Path=/`, no `Domain`, and `Secure` on https. The hub reads only the name for its scheme, so a hub upgraded from the old cookie name logs every device out once. The hub stores only the token's SHA-256.

A session ends 14 days after its last use (idle) and 90 days after it began (absolute), whichever comes first. Use is any request with its cookie, and any device-token call of a device it registered (see Devices), so a phone that lives on notification buttons keeps its reminders. The hub records use, and renews the cookie, at most once a day. A session belongs to the password hash it was made under, so changing the hash in the environment logs every device out; changing it in the app keeps the device that changed it logged in. Logins and the existing Host, Origin and Content-Type checks apply together: `POST /api/login` needs `Content-Type: application/json` like any other `/api/` write.

The terminal viewer, `meat-mode tui`, is an app client like the browser: it logs in with `POST /api/login`, sends the cookie by the name the hub set, sends no `Origin`, checks the session with `GET /api/session`, reads `GET /api/state`, `GET /api/epics` and `GET /api/events`, and sends `done`, `reopen`, `snooze` and `dismiss` through `POST /api/actions`. `meat-mode tui logout` sends `POST /api/logout` (see [deploy.md](deploy.md#watch-from-a-terminal)).

**Password again.** A session records when its password was last entered: at login, at claim, at a password change, and at a correct `password` on `POST /api/bridges`. Adding a machine needs that within the last 10 minutes. Without it, `POST /api/bridges` answers `403` with `{"error": "enter your password to add a machine", "reauth": true}` unless its body carries the right `password`; a wrong one answers `403` with `{"error": "wrong password", "reauth": true}`, counted with wrong logins, and `429` as for login. The app asks for the password and sends the request again. Changing the password always takes the current one (see Password).

**Push subscriptions** belong to the session that registered them. Logging out, or the session ending, stops reminders to that device. Changing the hash in the environment ends the old sessions at the next start and drops their subscriptions; each device subscribes again once it logs in. A subscription that has not been registered again for 60 days is dropped; the app registers again each time it opens.

### Setup

The app offers a setup wizard in two parts: the hub part runs once per hub, and the device part runs on each device. The hub records only its own part; each device keeps its part, and a run in progress, in its own browser storage.

| Method and path | Body | Response |
|---|---|---|
| `GET /api/setup` | | `{"status", "user", "timeZone", "password"}`, below |
| `POST /api/setup` | `{"status": "done"}` or `{"status": "dismissed"}` | `204`; records the hub part as finished or closed. `400` for any other status. |
| `GET /api/setup/status` | | `{"login", "hostMatches", "publicUrl", "pushKeys", "ntfy", "time", "publicDns", "tailscaleIdentity"}`, below |
| `POST /api/setup/test-event` | | `204`; sends a change event down every open event stream |

- `status` is `done` or `dismissed` once recorded. With no record it is `new` when the hub has no items and no machines (revoked ones count), and `in-use` otherwise. The app opens the wizard on its own only for `new`, so a hub upgraded with data never sees it; Settings starts it again on any hub.
- `user` is the name kept in the app, or `MEAT_MODE_USER`. `timeZone` is the IANA time zone kept in the app, `""` until chosen.
- `password` is `env` when `MEAT_MODE_PASSWORD_HASH` or `MEAT_MODE_PASSWORD_HASH_FILE` sets it, `app` when it was set in the app, and `none` otherwise.

The connection check (a wizard step, also in Settings) combines what the browser sees with `GET /api/setup/status`:

- `login` is `environment`, `app` or `none`, by the same rule as `password` above.
- `hostMatches` is `true` when this request's Host is the host of `MEAT_MODE_PUBLIC_URL` (a default port counts as no port). `publicUrl` is that setting, so the app can say where to open it.
- `pushKeys` is `true` when the hub has Web Push keys to hand out; `ntfy` is `true` when `MEAT_MODE_NTFY_URL` is set. `time` is the hub's clock, which the app compares with the device's; more than a minute apart is reported.
- `publicDns` is the latest public DNS check (see Private network only); the app warns when its `state` is `public`, naming the addresses.
- `tailscaleIdentity` is `true` when this request carried a non-empty `Tailscale-User-Login` header, which Tailscale Serve sets for a tailnet user. It reports presence only, never the value; the connection check shows it.
- The live-updates check opens its own `GET /api/events` stream, notes the revision the stream opens with, calls `POST /api/setup/test-event`, and expects a higher revision within 5 seconds. A proxy that buffers the stream holds the events back. The test event is an ordinary change event, so every open app refreshes its state once.

### Settings

The name and the time zone the hub keeps, shown in the wizard's Name and time zone step and in Settings.

| Method and path | Body | Response |
|---|---|---|
| `GET /api/settings` | | `{"name", "timeZone", "reminderLead"}` |
| `PUT /api/settings` | `{"name"?, "timeZone"?, "reminderLead"?}` | `200` and the new settings; `400` when any field is refused, and then none changes |

- `name` is the name notes the app writes are signed with, as `<name>-app`. It is at most 64 characters with no control characters, and surrounding spaces are dropped. An empty name goes back to `MEAT_MODE_USER` (`user` when that is unset), which is also what `GET` returns while none was chosen. `GET /api/state` and `GET /api/setup` report the same name.
- `timeZone` is an IANA name such as `Europe/Paris`, `""` until one is chosen. The empty string, `Local` and names the hub does not know are refused. The app prefills its first choice with the first browser's zone.
- `reminderLead` is how many minutes before an item's due time an early reminder comes, `0` (none, the default) to `10080` (a week). It applies to every item; a personal item can set its own with `remindBefore` (see Reminders).
- A field left out of `PUT` stays as it is.
- The app keeps showing times in each device's own zone. The hub's zone is for agents: `meat-mode add` reads `--due 2026-03-02` and `--due "2026-03-02 14:30"` in it, so a to-do added from a server in UTC lands at 9:00 in the user's zone. A `--due` with an offset, such as `2026-03-02T09:00:00Z`, is used as written. While the hub has no zone, `meat-mode add` uses the zone of the machine it runs on.

### Devices

A **device** is one place reminders go: a browser's push subscription (`kind: web`) or the Android app (`kind: app`). Settings lists them and can revoke any of them. `Device` is `{"id", "name", "kind", "confirmed", "createdAt", "lastSeen", "lastPush"}`: `id` is 32 hex characters, `lastSeen` is the last registration, renewal or device-token call, and `lastPush` is the last push its push service accepted, `null` before the first. The list never shows endpoints or keys: an endpoint is a capability URL.

**Endpoint rule.** A `web` endpoint must be an https URL on a known push service, as before. An `app` endpoint is a UnifiedPush endpoint, which can be any server, so the hub guards it:

- `https`, no user info, at most 1000 bytes.
- Every address its host resolves to must be global: not loopback, private (RFC 1918, `fc00::/7`), link-local, shared address space (RFC 6598, which Tailscale uses), documentation, benchmarking, multicast, unspecified or reserved, and not an IPv4-mapped, 6to4, NAT64 or Teredo form of one. The hub checks this when the endpoint is registered and again at every send, and dials the address it checked, so DNS that changes in between cannot reach an internal address.
- Or it matches `MEAT_MODE_PUSH_ALLOW`, a comma-separated list of host names and CIDRs the operator trusts, such as a push server on the tailnet or the LAN. A host name entry matches the endpoint's host exactly; a CIDR entry, at most /16 for IPv4 or /48 for IPv6, admits a resolved address inside it.
- Sends follow no redirects and time out after 10 seconds.

**Confirmation.** A new `app` registration gets no reminders until it is confirmed. The hub pushes `{"confirm": {"device": "<id>", "code": "<code>"}}` to it at once; the app posts the code to `POST /api/devices/{id}/confirm` and gets its device token. The code is 32 random bytes, base64url, good for one use within 10 minutes. A registration not confirmed in 10 minutes is dropped. A confirmation push is never shown to the user. Unconfirmed registrations do not count toward the 20 newest subscriptions; the hub keeps at most 3 of them, newest first.

**Device token.** `mmd_` followed by 32 random bytes, base64url. The hub stores only its SHA-256. The app sends it as `Authorization: Bearer <token>` from outside the WebView, so notification buttons work without opening the app and without the web session. It is accepted on `POST /api/actions` for kinds `ask` (without a note), `done` and `snooze` only, and on `PUT` and `DELETE /api/devices/self`. On any other route, or for another kind, it answers `401`; it never reads state. A request with an `Authorization: Bearer` header is a device request: the hub ignores any session cookie on it. A request with a token passes the Host, Origin and Content-Type checks like any other: it carries no `Origin`, and a bodiless `DELETE` still sends `Content-Type: application/json`.

A device belongs to the session that registered it, as push subscriptions do: logging out, the session ending and a password change that ends the session drop the device and its token. Each device-token call counts as use of that session, so it keeps the session from its idle end (see Login). A device not renewed for 60 days is dropped; each device-token call renews it. Revoking a `web` device stops pushes to it until that browser opens the app again with notifications allowed, when it registers anew; to stop a browser for good, log it out or block its notifications. Revoking an `app` device makes the app forget its token at its next device-token call (`401`); it shows reminders as off. The app renews through `PUT /api/devices/self` each time it opens and whenever its distributor hands it a new endpoint.

**Send results**, for both kinds: `2xx` delivered, and `lastPush` is set. `404` and `410` remove the subscription; an `app` device then shows no push until it registers again. `413` logs that the payload was too large and keeps the subscription. `429` holds every send to that endpoint until its `Retry-After` (seconds or a date, at most an hour; 60 seconds when absent). `5xx`, ntfy's `507` among them, and network errors are retried for that message at about 1, 5 and 30 minutes, then the message is given up; retries live in memory and a restart drops them. A subscription that has had no delivery for 3 days while every send to it failed is removed. A test reminder reports the first attempt and is not retried.

### Android app

The Android app shows the hub's own pages in a WebView and handles push natively. The page and the app talk through `WebViewCompat.addWebMessageListener`, injected as `window.meatMode` for exactly the hub's https origin. Each message is a JSON string. The page sends `{"id", "type", …}` with `meatMode.postMessage`; the app answers each with one message carrying the same `id`, through `meatMode.onmessage`, or `{"id", "error": "<plain message>"}`. The page treats the app as present only when `window.meatMode` exists and `hello` answers within 2 seconds.

| `type` | Request | Answer |
|---|---|---|
| `hello` | | `{"app": "android", "version": "X.Y.Z", "bridge": 1}` |
| `push-status` | | `{"state", "endpoint"?, "distributor"?, "lastPush", "batteryExempt"}`. `state` is `off`, `waiting` (registered, not confirmed yet), `on`, `blocked` (notifications not allowed in Android settings) or `unavailable` (no distributor and no Play Services). `endpoint` is there when `waiting` or `on`, for `POST /api/reminders/test`; `lastPush` is when the last push arrived, `null` before the first. |
| `push-on` | `{"vapidKey": "…"}`, from `GET /api/push/key` | Asks for notification permission, picks a distributor (a dialog when several are installed) and registers with UnifiedPush. Answers `{"subscription": {"endpoint", "keys": {"p256dh", "auth"}}, "name": "<phone model>"}`. The page posts that to `POST /api/push/subscriptions` with `"kind": "app"`, then asks `push-status` every second until `on`, for up to 30 seconds. |
| `push-off` | | Unregisters from the distributor, calls `DELETE /api/devices/self` and forgets the device token. Answers `{"state": "off"}`. |

A download from the hub, such as the export or an attachment, goes to Android's download manager with the WebView's session cookie and lands in Downloads; before Android 10, and for any other origin, the link opens in the default browser.

In the app the page asks the app instead of `PushManager` whether reminders are on, to turn them on, and for the endpoint the test reminder names. The service worker still runs and still handles a browser's pushes.

The app builds each notification from the push payload (see Agent push). A tap, and the `open` and `reply` buttons, open the WebView at the URL, same-origin only, as the service worker does. `retry`, `done` and `snooze` post the action with the device token in the background; when the hub refuses, the app opens the item instead. `channel` picks the Android notification channel: `reminders` (with `test`) or `agents`.

File inputs: an input with `capture` whose `accept` allows images, or is empty, opens the camera app (`MediaStore.ACTION_IMAGE_CAPTURE`), which saves to `cacheDir/photos/` through the app's FileProvider; the page gets that file, or nothing when the user cancels. The folder is emptied before each capture. When no camera app answers, the input opens the picker instead. Every other input opens the picker from `FileChooserParams.createIntent()`, which on Android 13 and later shows the Photo Picker for images. The app does not declare the `CAMERA` permission: the camera app holds it, and declaring it would make the capture intent need it too. On a touch screen the Files section offers Attach a file, Take a photo (`accept="image/*" capture="environment"`) and Choose a photo (`accept="image/*" multiple`, no `capture`).

Build: a Gradle project in `android/` with the wrapper at `android/gradlew` and one module, `app`. `./gradlew :app:assembleDebug` builds a debug APK anywhere; `./gradlew :app:assembleRelease` reads the signing config from `MEAT_MODE_ANDROID_KEYSTORE` (path to the keystore file), `MEAT_MODE_ANDROID_KEYSTORE_PASSWORD`, `MEAT_MODE_ANDROID_KEY_ALIAS` and `MEAT_MODE_ANDROID_KEY_PASSWORD`, and fails when any is unset, so no unsigned or debug-signed release exists. The version comes from the Gradle property `-PappVersion=X.Y.Z` (CI passes the tag without its `v`); without it the build uses `0.0.0` with `versionCode` 1, since Android refuses 0. The release APK lands in `android/app/build/outputs/apk/release/`. On the dev box it builds inside `ghcr.io/cirruslabs/android-sdk:36`; `scripts/check.sh` runs `:app:lintDebug :app:testDebugUnitTest :app:assembleDebug` there.

Releases: one signed universal APK per tag `vX.Y.Z`, named `meat-mode-android-vX.Y.Z.apk`, package `io.github.yourfriendaaron.meatmode`, minSdk 26, `versionName` `X.Y.Z` and `versionCode` `X*1000000 + Y*1000 + Z`.

### Actions

`NewAction` is `{id, kind, key?, bridge?, repo?, payload}`.

| kind | target | payload | effect on the hub's copy (shown at once, `pending: true` until applied) |
|---|---|---|---|
| `done` | `key` | `{}` | status `closed`, close reason `Done in Meat Mode`; a repeating personal item moves to its next date instead (see Repeating to-dos) |
| `reopen` | `key` | `{}` | status `open` |
| `snooze` | `key` | `{"until": time or null, "previous": time or null}` | `deferUntil = until`; no `until` clears the snooze |
| `dismiss` | `key` | `{}` | status `closed`, close reason `Dismissed` |
| `note` | `key` | `{"text": "…"}` | appends a comment by `<name>-app`, the name kept in Settings |
| `add` | `bridge` + `repo` | `{"title": "…", "due": time or null, "repeat"?: Repeat, "ask"?: true, "parent"?: "<item id>", "labels"?: ["…"]}` (`repeat` and `labels` on the personal list and projects only; `ask` only where agents are allowed; `parent` makes it a sub-task, see Sub-tasks) | inserts a provisional item keyed `<bridge>/<repo>/pending-<action id>`; replaced by the real bead when the bridge reports it. With `ask`, an agent starts on the new bead (see Agent runs), and the provisional item shows a `working` run at once. |
| `sync` | `bridge` + `repo` | `{"on": true or false}` | the repo shows `optedIn: on` with `pending: true` until the bridge reports its repo list again; switching off drops the repo's items then. Not allowed on the personal list or a project. |
| `ask` | `key` | `{"text"?: "…"}` | wakes an agent on the item (see Agent runs). With `text`, appends it as a note first, as `note` does, in the same action. The item's `run` shows `working` at once; on a live run it counts the note in `queued` instead. |
| `stop` | `key` | `{}` | ends the item's live run; `run` shows `stopped` with reason `stopped` at once |
| `agents` | `bridge` + `repo` | `{"on": false}` | the repo shows `agentsAllowed: false` with `agentsPending: true` until the bridge reports its repo list again. `{"on": true}` answers `400` naming the command that switches agents on at the agent machine (see below). Not allowed on the personal list or a project. |
| `epics` | `bridge` + `repo` | `{"on": true or false}` | the repo shows `epicsOn: on` with `epicsPending: true` until the bridge reports its repo list again; switching off drops the repo's epic rows then, and switching on brings its set with the bridge's next epics pass. Not allowed on the personal list or a project. |
| `limits` | `bridge` | `{"limits": LimitsChange}` | the bridge's `harness.limits` shows the change with `limitsPending: true` until the bridge reports its harness again. Answers `400` for a value above the machine's ceiling or below its floor, and for a machine whose bridge reported no `limits` (see Harness: limits). Not allowed on the hub or with a `repo`. |
| `edit` | `key` | `{"set": Edit}` | sets each field `set` names; the hub records the old values as `was` |
| `note-edit` | `key` | `{"commentId": "…", "text": "…"}` | replaces that comment's text, up to 10000 characters; the hub records the comment as it was in `comment` |
| `note-delete` | `key` | `{"commentId": "…"}` | removes that comment; the hub records it as `comment` |
| `note-restore` | `key` | `{"comment": Comment}` | puts a removed comment back in its place by `createdAt`, with its id, author and time |
| `project-delete` | `bridge` (`hub`) + `repo` | `{}` | closes every open item of the project as `Dismissed` and hides the project; the hub records the ids it closed as `items` (see Deleting a project) |
| `project-restore` | `bridge` (`hub`) + `repo` | `{"items": ["<item id>", …]}` | reopens those of `items` that are still deleted and shows the project again; the hub records the ids it reopened as `items` |

A note, or the note of an ask, keeps showing on the hub's copy after it is applied, until the bridge sends that repo's set again with the bead's own comment.

The `edit` and three note kinds change personal items only. The hub refuses them on an agent's item with `400`: an agent item's title, dates and priority belong to its agent, so ask for a change in a note.

### Deleting a project

`project-delete` deletes a project from the app in one action; it is refused with `400` on the personal list and on a machine's repo, and answers `404` for a project the hub does not hold. Its open items close as `Dismissed`, as a Delete does, and the applied action lists their ids in `items`. The project leaves `State` and `GET /bridge/v1/projects` at once, and `State` keeps only its deleted items, so the Deleted view shows them for the usual 7 days; its done items come back with it. Nothing is dropped: the items and every action on them stay in the log and in the export.

Its Undo is `project-restore` with the delete's `items`, which reopens those still deleted. A project also shows again with the next `add` to it, an agent's included, and with a `reopen` of any of its items, such as a Restore in the Deleted view, which brings back that item alone. Each item's History lists the delete and restore that closed and reopened it.

In the app, a project's list has Delete list under its title. It asks first, naming the list and how many open to-dos it holds, and its toast offers Undo.

### Trackers

Each repo in `State` carries the task tracker its bridge found it in ([trackers.md](trackers.md)): `tracker` is the name the app shows, such as `beads` or `Backlog.md`, and `actions` the kinds that tracker can apply, from `done`, `reopen`, `snooze`, `dismiss`, `note`, `add`, `subtask` (an `add` with `parent`) and `ask`. A repo without `actions` takes every kind: the personal list never has one, and neither does a repo from a bridge that predates trackers.

The app hides what a repo's tracker cannot apply, and the hub refuses it with `400` and `{"error": "<tracker> cannot <what>"}`, such as `ticket cannot snooze`: an action on an item whose repo does not list its kind, an `add` to a repo without `add`, an `add` with `parent` to one without `subtask`, and an `add` with `ask` to one without `ask`. `sync`, `agents`, `epics` and `stop` are never refused this way, though `epics` has a refusal of its own: the hub refuses it on a repo whose tracker cannot show epics (see Epics). A Restore is a `reopen`, so a repo without `reopen` offers neither, and the app offers no Undo of a close there.

### Harness

Each bridge in `State` and `GET /api/bridges` carries `harness`, the agent harness its last hello named ([harnesses.md](harnesses.md)), with the limits it keeps on each run:

- `name`: the registry key, such as `codex`, and the value of `MEAT_MODE_AGENT_HARNESS` that picks it on the machine.
- `title`: the name the app shows, such as `Codex`.
- `version`: the harness program's version; empty when its check failed.
- `instructionFile`: the file in a repo the harness reads standing instructions from, such as `AGENTS.md`. The app names it in the instructions for agents.
- `budget`: how the spending cap is enforced: `native` (the harness stops itself), `metered` (the bridge stops the run when its reported cost passes the cap) or `none` (no cap: a run stops at its time limit only). The hub drops any other value.
- `budgetUsd`: the cap for one run, in US dollars; absent with `none`.
- `timeoutSeconds`: the time limit for one run.
- `steer`: `true` when a note reaches a live run at once; otherwise it waits until the agent finishes its turn.
- `limits`: the machine's four limits on agent runs (see Harness: limits); absent from a bridge too old for the app to set them, which the app then shows read-only with a line saying to update it.

A bridge without `harness` predates harnesses and runs Claude Code; the app shows it as Claude Code without limits. The hub trims and bounds each field as it does a tracker's, and keeps no harness without a `name`. The harness is picked on the agent machine; the app only shows it.

#### Harness: limits

The app sets each machine's four limits on agent runs, up to a ceiling the machine sets, so a hub that is taken over cannot widen what agents may do beyond what the machine allows. `harness.limits` is `RunLimits`:

```json
{"budgetUsd": 5, "timeoutSeconds": 1800, "runsPerTask": 10, "concurrent": 2,
 "defaults": {"budgetUsd": 2, "timeoutSeconds": 1800, "runsPerTask": 10, "concurrent": 2},
 "max": {"budgetUsd": 20, "timeoutSeconds": 14400, "runsPerTask": 50, "concurrent": 5},
 "set": ["budgetUsd"]}
```

- The top-level four are the limits a run that starts now keeps: `budgetUsd` (the spending cap for one run, in US dollars), `timeoutSeconds` (the time limit for one run), `runsPerTask` (runs one item may have in its lifetime) and `concurrent` (runs live at once on the machine). `harness.budgetUsd` and `harness.timeoutSeconds` carry the same values as before, `budgetUsd` absent with budget `none`.
- `defaults` are the machine's own limits, from `MEAT_MODE_AGENT_BUDGET_USD`, `_TIMEOUT`, `_RUNS_PER_TASK` and `_CONCURRENT`, used while the app has set nothing.
- `max` are the ceilings, from `MEAT_MODE_AGENT_MAX_BUDGET_USD`, `_MAX_TIMEOUT`, `_MAX_RUNS_PER_TASK` and `_MAX_CONCURRENT` (see [deploy.md](deploy.md#settings)). A ceiling of `0` lets the app set nothing for that limit.
- `set` names the limits the app has set; the others are the defaults.

`LimitsChange`, the payload of a `limits` action, is `{"budgetUsd"?, "timeoutSeconds"?, "runsPerTask"?, "concurrent"?, "reset"?}`: each field present sets that limit, and `reset` lists limit names that go back to the machine's default. A limit named in neither stays as it is. A change needs at least one of them, and names each limit once.

The floors are: `budgetUsd` above 0, `timeoutSeconds` at least 60, `runsPerTask` and `concurrent` at least 1, each a whole number but the budget. The hub refuses a value below its floor or above the ceiling in the bridge's last `limits` with `400`, such as `the spending cap can be at most $20 a run on laptop. To allow more, set MEAT_MODE_AGENT_MAX_BUDGET_USD on laptop and restart its bridge`.

The bridge enforces them. It checks each value against its own ceilings again, whatever the hub sent, and fails the action with the same message. It saves an accepted change in the `limits` object of its `bridge.json`, atomically and under the same lock as the `agents` map, then sends a hello with its new `harness.limits` before it reports the result. A value saved above a ceiling, by a hand edit or because the ceiling was lowered since, is cut to the ceiling; one below its floor reads as unset. A change applies to runs that start after it: a live run keeps the time limit and budget it started with.

### Sub-tasks

An item's `parent` is the id of the item it is a sub-task of, in the same list; a top-level item has none. `parentTitle` is the parent's title as the list's tracker last gave it, so a sub-task whose parent is not in the set (on beads, a parent not labelled `human`) can still name it. The rules are the same on the personal list and on a tracker's list:

- An `add` with `parent` needs that item open, in the same list, and top-level: sub-tasks go one level deep. A tracker item that is deeper is shown as it comes.
- `done` and `dismiss` on an item answer `409` while any of its sub-tasks is open. A sub-task with a pending `done` or `dismiss` counts as closed and one with a pending `reopen` as open, so the app may send the sub-tasks' closes and then the parent's together; the bridge applies them in that order. A sub-task still being added counts as open, until the bridge has sent it.
- A snooze changes only the item it names. The app shows sub-tasks under their parent, so they move with it.
- On the personal list and projects, an `edit` with `parent` moves an item: under another item by the `add` rules, or to the top level with `""`. An item with sub-tasks of its own, open or closed, cannot become a sub-task, and an item cannot be its own parent. An agent's item is moved in its tracker (on beads, `bd update --parent`), and the bridge sends the new `parent`.
- `done` on a repeating parent reopens the sub-tasks it closed with `done` when the parent moves on to its next date. Dismissed sub-tasks stay closed.
- In the app, each sub-task row under its parent has a remove button: Delete (a `dismiss`) on the personal list and projects, open or done, and Won't do (a `dismiss`) on an open sub-task of a tracker that can dismiss. Its Undo is a `reopen`, followed by a `done` for a sub-task that was done.
- Items have no checklist: an entry to tick is a sub-task. An `add` or `edit` that sends `checklist`, at the top of the body, in `payload` or in `payload.set`, from the app or an agent, is refused with `400` and "Checklists are now sub-tasks: add each entry as a sub-task instead (docs/api.md, Sub-tasks)." A hub at schema 14 converted the checklists it held: each entry of an open top-level item became one sub-task of it, in order, with id `<parent>.<n>` (zero-padded to one width; a taken id gets `-2`, `-3`…), done entries closed as done. An item that is itself a sub-task, or is closed, got the entries appended to its description as a Markdown list instead, done ones as `~~text~~`.

`Edit` is `{"title"?, "description"?, "due"?, "clearDue"?, "priority"?, "repeat"?, "clearRepeat"?, "remindBefore"?, "clearRemindBefore"?, "parent"?, "subtasksDone"?, "labels"?}`. A field left out stays as it is.

- `title` follows the `add` rules: trimmed, not empty, up to 1000 characters.
- `description` is up to 10000 characters; `""` clears it.
- `due` is a time; `"clearDue": true` removes the due date instead. Sending both is refused. Clearing the due date of a repeating item also ends its repeat.
- `priority` is 0 (highest) to 4, as in bd. A new personal item has 2. The app shows 0 and 1 as high priority on every item.
- `repeat` sets the item's `Repeat` (see Repeating to-dos); `"clearRepeat": true` ends it. Sending both is refused, and so is a `repeat` on an item that has no due date and gets none in the same edit.
- `remindBefore` is the item's own lead time in minutes, `0` to `10080`, where `0` means no early reminder; `"clearRemindBefore": true` goes back to the hub's `reminderLead`. Sending both is refused.
- `parent` moves the item (see Sub-tasks); `""` makes it top-level.
- `labels` replaces the item's tags with the list given, by the rules in Tags; `[]` clears them.
- `subtasksDone` lists ids of the item's own sub-tasks to mark done. The hub puts it in the `was` of a repeating `done`, so that Undo closes the sub-tasks the repeat reopened; ids that are not the item's sub-tasks are refused. A named sub-task is closed outright, a repeating one included: it does not move on to its next date, as a `done` on it in the app would.

The applied `edit` action carries `was`, an `Edit` with the same fields as `set` holding the values before it (`labels: []` for no tags, `clearDue: true` for a due date that was not set, `clearRepeat: true` for no repeat, `clearRemindBefore: true` for no lead time of its own; an edit that clears the due date of a repeating item records its `repeat` too). Its Undo is an `edit` with `set` = `was`. The Undo of `note-edit` is a `note-edit` back to the text of the `comment` the applied action carries; the Undo of `note-delete` is `note-restore` with the `comment` the applied action carries.

### Item history

The task sheet of a personal or project item has a History section, folded with its count. Open, it lists the item's applied changes from `GET /api/items/{key}/history`, newest first, five at a time with Show all: each in plain words ("Moved to Sun, Nov 1 at 12:00", "Marked done", "Added sub-task Buy soil", "Tags: +garden"), who made it ("You" for the app's own actions, `<author> on <machine>` for an agent's) and when. History reaches back to the item's `add`, as the hub never drops actions, for as long as the app shows the item: a closed item leaves it 7 days after it closed. An item moved from a bridge's personal repo starts its history at the move.

A change offers Undo when it is the latest change to every field it touched and the item still shows what it set. A later change to another field leaves it undoable; a later change to the same field, by anyone, takes the Undo over. Undo posts the app's usual opposite:

- `edit`: an `edit` with `set` = `was`. An edit without `was`, or one that names `subtasksDone`, offers none.
- `done` that moved a repeating item on: an `edit` with `set` = `was`, while the item is open.
- `done` and `dismiss` that closed the item: `reopen`, when the item was open before them.
- `reopen`: `dismiss` after a delete, its project's delete included, or `done` after a done on an item that does not repeat.
- `snooze`: `snooze` back to `previous`, while the snooze still holds.
- `note-edit`, `note-delete` and `note-restore`: the note back as it was.

An `add`, a `note`, a sub-task's `add`, an `ask`, a `stop` and a project's `project-delete` or `project-restore` offer none (the delete's own Undo is in its toast): the sheet's own buttons remove an item, a note or a sub-task. Undoing is itself a change, so it shows in History with its own Undo. Undoing an agent's change sends no notification, as the change did not.

### Tags

An item's `labels` are its tags. The app calls them tags and shows them as chips on its row and in its detail, filters the list by one and finds items by them in search. It never shows the label `human`, in any case, on any item.

**On the personal list and projects** the user sets tags in the app, and an agent sets them on any to-do there (see Agents). An `add` with `labels` or an `edit` with `labels` sets them, by these rules:

- Each tag is trimmed, then 1 to 50 characters (`api.MaxTagLen`) with no whitespace, control character or comma. A tag that breaks a rule is refused with `400` naming it; an empty one after trimming is dropped.
- Tags compare without case: a repeat of one already in the list is dropped, the first spelling kept. `human` is dropped, since the app never shows it.
- The list keeps the order given and holds at most 20 tags (`api.MaxTags`).

Quick add reads `@word` as a tag on the personal list and projects, as Todoist does: `#` already names a list. A word is a letter followed by letters, digits and `_ . : / -`, so `@5pm` stays in the title. On a tracker's list, quick add leaves `@word` in the title.

**On a tracker's items** the tags are the labels its tracker gives, read-only:

| Tracker | Where the tags come from | Left out by the bridge |
|---|---|---|
| beads | the bead's labels (`bd label`) | nothing; the app hides `human` |
| Backlog.md | the task's `labels` | `dismissed`, which marks a task closed as not needed |
| ticket | the ticket's `tags` | nothing; the app hides `human` |

Nothing sends a tag back to a tracker. The `edit` action is refused on an agent's item (see Actions), `labels` included, and an `add` with `labels` to a tracker's repo is refused with `400`: an agent item's labels belong to its agent and its tracker, so ask for a change in a note. A tag on an agent item changes when the bridge sends the item again.

### Descriptions and notes in Markdown

The detail view renders an item's `description`, its `notes` and each comment's `text` as Markdown, on every item: agents write Markdown into beads, and the user may too. The hub stores and sends the text as it was typed; the app renders it. Lists and rows show titles only, as plain text. Editing stays a plain text box with no toolbar, which helps with Markdown as GitHub's comment box does: Enter on a list item starts the next one (the next number, an empty task box) and Enter on an empty item ends the list; Tab and Shift+Tab nest and un-nest list items; Ctrl or Cmd with B, I and E wraps the selection in bold, italic or code, and with K makes a link; and a web address pasted over selected text links that text.

- The renderer is markdown-it (research: [research/0006-rich-todos](research/0006-rich-todos/RESEARCH.md)) with `html: false`, `linkify: false`, `typographer: false` and `breaks: true`, so a single newline stays a line break as in plain text. Any HTML in the text shows as text.
- A bare `http://` or `https://` address in the text renders as a link to itself. Trailing sentence punctuation and an unmatched closing bracket stay outside it. An address without a scheme, such as `example.com`, stays text.
- CommonMark and GitHub tables and strikethrough render. A task list item (`- [ ]`) renders as its text, with no checkbox.
- A link renders only for `http:`, `https:` and `mailto:` addresses, opening in a new tab with `rel="noopener noreferrer"`; any other address renders as the link's text alone.
- An image renders as a link to its address, named by its alt text: the page's `img-src` loads no image from elsewhere, and a remote image would tell its host when the item was opened.
- Headings render one size, smaller than the detail's own headings, with no `id`.
- The app's Content-Security-Policy is unchanged; the renderer needs no `eval` and no inline script.

### Attachments

The user can attach files to an item of the personal list or a project, and open and remove them in the app. Agent items take none: beads (bd 1.3.1), Backlog.md and ticket have no attachment to carry a file back to the agent machine, so a file on the hub would never reach the agent. Agents cannot attach files either.

| Method and path | Body | Response |
|---|---|---|
| `POST /api/items/{key}/attachments?name=<file name>` | the file's bytes, `Content-Type: application/octet-stream` | `201` and the `Attachment`; `400` for an item not on the hub's own lists or for a bad name, `404` for no such item, `413` past a cap below |
| `GET /api/attachments/{id}` | | `200` and the file (see Serving), `404` for an unknown or removed one |
| `DELETE /api/attachments/{id}` | | `204`; the attachment is removed, and can be put back until it is purged |
| `POST /api/attachments/{id}/restore` | | `200` and the `Attachment`, back in its place; `404` once purged, `409` when putting it back would pass a cap |

`{key}` is the item key, escaped as one path segment. Each route sits behind the login and the guards above, and each change makes the app's live updates refresh, as an action does. Adding or removing an attachment is not an action: the action log does not show it.

`Attachment` is `{"id", "name", "type", "size", "sha256", "createdAt"}`. The item's `attachments` lists those not removed, oldest first, in `GET /api/state` and `GET /bridge/v1/personal/{id}`.

- `id` is 32 lowercase hex characters the hub makes.
- `name` is the `name` query parameter: control characters are dropped, `/` and `\` become `_`, it is trimmed and cut to 255 bytes on a character boundary, and an empty one becomes `file`. Two attachments may share a name.
- `type` is the media type the hub sniffs from the first 512 bytes (Go's `http.DetectContentType`, without parameters), never one the client gives.
- `size` is the byte count and `sha256` the lowercase hex SHA-256 of the bytes, both worked out as the hub writes the file.

**Caps.** A file is at most 25 MiB (`api.MaxAttachmentBytes`); an item holds at most 20 attachments (`api.MaxAttachmentsPerItem`); and all files the hub keeps, removed ones not yet purged included, come to at most 1 GiB (`api.MaxAttachmentsBytes`). Every type is taken: attachments are the user's own files, and Serving below keeps any of them from running in the page. A file past a cap answers `413` with `{"error"}` naming the cap: "Vacation.mov is 31.4 MB; a file can be at most 25 MB." (MB to one decimal), "An item can hold at most 20 files." or "The hub's files would come to 1.02 GB; they can be at most 1 GB." (GB to two decimals). A `restore` of an attachment past a cap answers `409` with the same words. The app checks the size before it sends and shows the same words. A reverse proxy in front of the hub must let a 25 MiB body through (see [deploy.md](deploy.md)).

**Serving.** Every file answers with `X-Content-Type-Options: nosniff`, `Cache-Control: private, max-age=86400` and `Content-Security-Policy: default-src 'none'; sandbox` in place of the page's policy. A file whose `type` is `image/png`, `image/jpeg`, `image/gif` or `image/webp` is served as that type with `Content-Disposition: inline`, so the app shows it as a thumbnail and opens it in a tab. Every other file is served as `application/octet-stream` with `Content-Disposition: attachment`, so it downloads under its name and never renders on the hub's origin; SVG, HTML and PDF among them. Both dispositions carry the name as `filename*=UTF-8''<percent-escaped name>`, with an ASCII `filename` fallback.

**Storage.** The hub keeps each file as `attachments/<id>` in its data directory and the metadata in its database, keyed by the item. It writes a file to a temporary name in the same folder, syncs it, renames it into place and syncs the folder, then records it; a crash leaves at most a stray temporary file. Files never sit in the database: most are over 100 KB, where SQLite's own measurements favour files, and a 25 MiB write would hold the hub's only database connection. An upload and a restore may take longer than the hub's usual 60-second read limit; each of those two routes allows 30 minutes to read its body.

**Removing and purging.** `DELETE` marks an attachment removed at once: it leaves the item and stops being served. The app offers Undo, which calls `restore`. A removed attachment is purged, its file deleted and its row dropped, 24 hours after removal. The hub sweeps at start and every 10 minutes: it purges what is due, deletes stray temporary files and any file in `attachments/` with no row, and logs any row whose file is missing. The hub's lists have no way to delete an item (a Delete in the app closes it), so an item's attachments stay while it does. Should an item row ever go, its attachment rows go with it, and the sweep removes their files.

### Repeating to-dos

A personal item can repeat: "Water the plants every day at 8:30pm". `Repeat` is `{"every", "interval"?, "weekdays"?, "weekOfMonth"?, "fromDone"?, "timeZone", "until"?, "start"}`:

- `every` is `day`, `week`, `month` or `year`; `interval` repeats every n of them, `1` (the default) to `100`.
- `weekdays` picks the days of a weekly rule, `0` (Sunday) to `6`, each once. Left out, a weekly rule keeps the due date's weekday. Only a weekly rule takes it, or a monthly one with `weekOfMonth`, and not with `fromDone`.
- `weekOfMonth` makes a monthly rule land on a weekday of the month instead of a date: `1` to `4` for the first to the fourth, `-1` for the last. It takes exactly one `weekdays` entry, the day, and not `fromDone`. "Every 2nd Tuesday" is `{"every": "month", "weekdays": [2], "weekOfMonth": 2}`; with `interval` 3 it is every third month's. Left out, a monthly rule keeps the due date's day of the month.
- `timeZone` is an IANA zone. The rule keeps the due time's wall-clock time and date in it, so 8:30pm stays 8:30pm across daylight saving changes. The app sends the device's zone; `meat-mode add` sends the hub's, or the machine's while the hub has none.
- `until` ends the series: "Water the plants every Sunday until 26 November". It is a date, `YYYY-MM-DD`, in `timeZone`, and the series' last occurrence is the last one on or before it. Left out, the item repeats for good. The hub refuses an `until` before the date of the item's due time, both when the rule is set and when the due time is moved. To change or remove it, send the whole `repeat` again with the new `until` or none.
- `start` is the occurrence the schedule counts from. The hub sets it to the due time whenever the rule is set, and when the due time is set to a time that is not one of the series' own dates; moving the due time onto one of them, as the Undo of a `done` does, keeps it. Sending a rule that differs from the item's own only in `until`, while the due time is one of the series' dates, also keeps it, so changing the end date never moves the series. The hub ignores a `start` a client sends. A monthly rule from the 31st lands on the last day of shorter months and returns to the 31st after them; a yearly rule from 29 February lands on 28 February.
- A rule needs a due time: the item's current occurrence. Moving the due date moves the series.

`done` on a repeating item keeps it open and moves it on:

- The due time becomes the next occurrence after both the current due time and now, so occurrences missed while it sat overdue are skipped, never stacked. With `fromDone`, it is the done day plus `interval` units, at the rule's time of day.
- When that next date falls after `until`, the series is over: `done` closes the item like any other, with no `was`, so its Undo is a `reopen`. The item keeps its `repeat`, so a `reopen` brings the series back at its last due time.
- Its snooze is cleared.
- Its sub-tasks closed with `done` are reopened (see Sub-tasks).
- The applied `done` carries `was`: `{"due"}` as it was, and `subtasksDone` with the sub-tasks it reopened. Its Undo is an `edit` with `set` = `was`, not a `reopen`; the app tells the two apart by `was`.
- `dismiss` closes it and ends the series. `reopen` on a closed repeating item opens it at its last due time.

### Reminders

The hub reminds through Web Push, and ntfy when set up, once for each time an item's moment comes:

- At the due time, and at the end of a snooze. A due time after the snooze end takes over once it comes. Closed items never remind.
- With a lead time (the item's `remindBefore`, else the hub's `reminderLead`) above 0, an early reminder comes that many minutes before the due time, saying how long is left, and the reminder at the due time still comes. A snooze end has no early reminder: the user picked that moment. Changing the lead time never sends again a reminder already sent for the same due time.
- A repeating item reminds afresh for each occurrence, since each has its own due time.

When the hub could not remind on time, because it was stopped or its checks were failing, the reminders that came due meanwhile are not lost. On its next check, a reminder more than 5 minutes late goes out as itself when it is the only one, saying how long ago it came due; two or more go out as one reminder, "N reminders came due while reminders were off", that opens the app. However late it is, a missed reminder is told once. An item the hub first saw more than 5 minutes after its moment, such as an old bead on a bridge's first sync, never reminds for that moment; one that arrives within 5 minutes still does.

### Calendar feed

The hub can serve the open items with a due date, from every list, as an iCalendar feed that calendar apps subscribe to.

| Method and path | Body | Response |
|---|---|---|
| `GET /api/calendar` | | `{"on": true or false}` |
| `POST /api/calendar` | `{}` | `{"url"}`: a new feed address, shown once; it replaces the earlier one, which stops working |
| `DELETE /api/calendar` | | `204`; turns the feed off |
| `GET /calendar/{token}.ics` | | `200` with `text/calendar`; `404` for any other token or while the feed is off |

- The address is `<MEAT_MODE_PUBLIC_URL>/calendar/<token>.ics`, under 256 characters for Google Calendar. The token in it is the only credential, since calendar apps cannot log in, so the feed needs no login and is outside `/api/`. The hub stores a hash of the token. The Host check applies; the Origin checks do not.
- Each open dated item is one `VEVENT` at its current due time, 15 minutes long, whose `UID` is the item key, so a calendar keeps one event per item as it changes. A repeating item shows its next occurrence only, because the hub moves it on when it is done. Calendar apps fetch the feed on their own schedule, from every hour to once a day.
- The event's summary is the item's title and its link opens the item in the app.

On the personal list (`hub/personal`) and on a project, every action is applied by the hub when it is posted: the response already has status `applied`, the item never shows `pending`, and an `add` returns the new item's id as `beadId`. A closed personal item leaves the app 7 days after it closed, as a closed bead leaves a bridge's set.

Undo is the client's job: it cancels the closing action when it is still unapplied, or else posts the inverse (`reopen`, or `snooze` back to `previous`). Actions on the personal list and projects are always applied, so their Undo is always the inverse.

An action's `status` is `pending`, `applied`, `failed` or `cancelled`. `failed` carries `error`; the app shows it on the item, and for a failed `add`, whose bead never existed, in Recent actions with the title that was typed. A bridge fails an action on a bead it does not list in that repo with the error `unknown bead`.

Agents are switched on at the agent machine only, so a hub that is taken over cannot widen what agents may touch. `meat-mode bridge agents on <repo>` and `meat-mode bridge agents off <repo>`, run on the bridge's machine, set the repo's entry in the `agents` map of `bridge.json`; `meat-mode bridge agents` alone lists the repos the bridge finds, with their switch. `<repo>` is the repo's name as the bridge reports it (`projects/garden`) or the path of its folder (`.` inside it); either must be a repo the bridge finds, or the command fails naming it. A running bridge reads the `agents` map from `bridge.json` again before each repo report, so the change reaches the app within one poll. The command writes the file atomically and under a lock that the running bridge also takes, so neither loses the other's change.

The app's `agents` action can only switch off. The bridge accepts it only for a repo it found, saves it in the same map and reports it as the repo's `agentsAllowed: false`. A bridge refuses an `agents` action with `on: true`, such as one queued by an older hub, with the error `switch agents on at this machine: meat-mode bridge agents on <repo>`.

The limits on agent runs follow the same rule: the app sets each one only up to the ceiling the machine sets in its own environment, and the bridge refuses anything above it (see Harness: limits).

A `sync` switch is applied by the bridge, not by bd. The bridge accepts it only for a repo it found itself, looked up by name, so the app cannot make it read anywhere else; any other name fails with `unknown repo`. It saves the choice in the `sync` map of its own `bridge.json`, where it overrides the repo's `meat-mode.sync` line, and sends its new repo list before reporting the result. A repo with no switch syncs when its `.beads/config.yaml` has the top-level line `meat-mode.sync: true`.

### Month view

The app's month view shows each day's dated items: open items on their due day, snoozed ones also on the day they come back, and repeating ones on each later date of their series. The app has the due and snooze times in the state; the hub works out the later dates, from the same rule that moves an item on when it is done (see Repeating to-dos).

| Method and path | Response |
|---|---|
| `GET /api/occurrences?from=<time>&to=<time>` | `{"occurrences": [{"key", "at"}]}` |

- `from` and `to` are RFC 3339 times, and the window `[from, to)` is at most 62 days long. A missing or unreadable time, `to` not after `from`, or a longer window is `400`.
- Each entry is a later occurrence of an open item with a `repeat`: a date of its series after its current due time, in the window, on or before its `until`. The current due time itself is not listed. A rule with `fromDone` has no later dates until it is done, so its items list none.
- Entries are ordered by `at`, then `key`; `at` is UTC. The list is `[]`, never `null`, when there are none.

### Agent runs

An ask wakes an agent on one item: it reads the item's latest state, then does the work and closes the bead, or replies with a note. Only the user's own actions wake an agent; an agent's comments and closes never do. The bridge runs the agent; the hub only carries the ask and shows what the bridge reports.

The hub refuses `ask` with `400` on a personal item (`hub/…`), and on an item, or an `add` with `ask`, in a repo whose `agentsAllowed` is off (a pending switch counts as where it was switched to). `stop` takes any item; on one with no live run it changes nothing. An `ask` is allowed on a closed item, so the user can tell the agent they did the task.

An item's `run` is its latest `Run`: `{"id", "state", "agent", "activity"?, "startedAt", "endedAt"?, "reason"?, "detail"?, "queued"?, "count"}`. No `run` means no agent was ever asked (idle).

- `state` is `working` while the agent runs; then `waiting` when the run ended after the agent wrote a note (it is waiting on the user), `done` when it ended with the bead closed, or `stopped`.
- `reason` says why a run stopped: `stopped` (by the user), `usage-limit`, `timeout` (past the machine's time limit for one run), `budget` (spent its budget for one run), `run-limit` (the item used up its lifetime number of runs), `busy` (too many runs live on that machine), `not-allowed` (the machine does not allow agents in that repo), `no-reply` (it ended without a note or closing the bead), `failed` (the agent command failed or reported an error), `crashed` (the agent process died), `restarted` (the bridge stopped while it ran) or `offline` (the bridge stopped reporting while it ran; set by the hub). `detail` is a plain sentence about it, such as when a usage limit resets.
- `agent` is the name to show for the agent that runs: the bridge's configured agent name, else its harness's title, such as `Claude Code`.
- `activity` is one line in plain words of what the agent is doing, such as `Editing notes.txt`; once the run ends, the first line of the agent's latest note in that run, or absent when it wrote none.
- `queued` counts the follow-up notes written to a live run that the agent has not reached yet: the newest `queued` of the user's comments on the item are still on their way.
- `count` is how many runs the item has had in its lifetime, this one included.
- `id` is the bridge's id for the run. A new run gets a new id; a follow-up into a live run keeps it.

One item never has two runs. An `ask` on an item whose run is `working` becomes a follow-up: its note goes to the live run, which reads it at its next step. With no note it changes nothing. An `ask` on a settled run starts a new run that continues the agent's earlier conversation on that item where the agent supports it.

The bridge applies an `ask` by writing its note to the bead first, then starting or following up the run, and reports the action applied once the note is written: a run that could not start shows `stopped` with its reason, and the note stays. A `stop` is applied when the run has ended.

While a run is `working` and its bridge has not been seen for 60 seconds, the hub shows the run `stopped` with reason `offline`, and keeps showing that until the bridge reports the run again.

#### Agent push

When a run ends the hub sends one notification for it, through Web Push and ntfy as reminders go: `<agent> replied` (`waiting`), `<agent> finished` (`done`) or `<agent> stopped` with the reason in words (`stopped`). It sends one per run end, keyed on the run's `id` and state, never one per agent comment mid-run, and none for a run the user stopped or on an item the user closed (its latest done, dismiss or reopen since the run started is a done or dismiss). A run that ended more than an hour before the hub first sees it sends nothing, so a bridge's first sync does not bring a flood. Its link opens the item, and the Reply action opens it at the note box: `/?item=<key>&reply=1`.

The Web Push payload is `{"title", "body", "url", "tag", "channel", "actions"?}`; `channel` is `reminders`, `agents` or `test`. `actions` is a list of `{"action", "title"}` where `action` is `reply`, `open` or `retry` (`retry` on `stopped` only) on an agent push, and `done` and `snooze` on a reminder for one item, each only where the item's tracker can apply it (see Trackers); the payload then also carries `"key"`, the item key. The service worker opens `url` for `open` and a tap on the notification, `/?item=<key>&reply=1` for `reply`, for `retry` posts `{"kind": "ask", "key"}` as a new action, for `done` posts `{"kind": "done", "key"}`, and for `snooze` posts `{"kind": "snooze", "key", "payload": {"until": <one hour from now>, "previous": null}}`, the app's "In 1 hour". A reminder that gathers several missed ones has no actions. The payload stays within Web Push's size limit: with a long key or title the hub shortens the body, then the title, never the link, the key, the tag or the actions. ntfy gets the same title, body and link, with Reply and Open as view actions.

### List identity

Every list has one mark, drawn the same way wherever the app names it: the sidebar's lists, the list chip on a task, the task sheet's List line, Settings and Epics. The mark is the list's colour plus a badge: the repo's icon where the hub has one, otherwise the first letter of the list's name, upper case, on a square in its colour. The personal list keeps its own mark.

**Colour.** A `Repo` in `State` carries `colour`, a slot from 0 to 5 that the app draws with its theme's six list colours. The hub gives a repo its slot when it first holds it (a bridge's report, a new project, the personal list): the slot held by the fewest repos that are not hidden and whose bridge is not revoked, the personal list included, ties to the lowest. Several new repos in one report get theirs one after another in name order, inside the write that adds them. A slot stays with the repo's row: hiding, adding or removing another list, a new report and a restart never change it, except a workspace colour its bridge reports (below), and a repo moved to a new folder name keeps its slot. A repo dropped from its bridge's report loses its slot with its row. The migration that brings `colour` gives the repos the hub already holds their slots in turn, the personal list first, then by the last segment of the name without regard to case, then by name and bridge, which is close to the order the app lists them. An app talking to an older hub, whose repos carry no `colour`, gives slots by its list order. Colours are not exported; a restore gives the restored lists slots as above. A repo that gets a slot, or whose icon changes, changes `State`.

**Workspace colours.** A bridge may report, for a repo, the colour the user gave its workspace on the bridge's machine (a space in the Herdr app, for example), so a list matches the workspace it is worked on in. The bridge sends it as `spaceColour`, a slot from 0 to 5, in `PUT /bridge/v1/repos`. The hub then sets the repo's `colour` to that slot in the same write: a repo new in that report takes the reported slot instead of the least-used one, and counts as holding it for the repos given slots after it. A repo reported without `spaceColour` keeps the slot it has, so a workspace that closes leaves its list's colour as it was, and a repo that is no workspace keeps its least-used slot. A `spaceColour` outside 0 to 5 refuses the report with `400`. The hub never sends `spaceColour` in `State`; a changed `colour` changes `State` as above.

The bridge reads the workspace colours from a file on its own machine: the path in `MEAT_MODE_SPACE_COLOURS` when set, otherwise `space-colours.json` in its state folder (`$XDG_STATE_HOME/meat-mode` when that is an absolute path, else `~/.local/state/meat-mode`; `~/Library/Application Support/meat-mode` on macOS). It rereads the file each time it builds its repo report, so a change reaches the hub within one bridge cycle. The file is JSON, at most 1 MiB:

```json
{ "version": 1, "spaces": [ { "path": "/srv/projects/garden", "colour": "ansi6" } ] }
```

- The file is a JSON object whose `version` is the number 1. `spaces` is a list; missing or `null` means empty. An entry that is not an object, or whose `path` or `colour` breaks a rule below, is skipped and the others are kept. Writers use the keys exactly as shown.
- `path` is an absolute folder: where the workspace is now. The bridge cleans it and resolves symbolic links in it as it does for repos (an unresolvable path stays as cleaned). It belongs to every reported repo whose folder is that path or holds it by whole path segments (`/srv/garden` holds `/srv/garden/web`, not `/srv/gardenx`), compared byte for byte; when one repo's folder holds another's, only the deepest such repo gets it. A relative path, or one under no reported repo, is skipped. Every repo the bridge reports can get a colour, synced or not.
- `colour` is one of six names, exactly as written here, each a slot: `ansi5` 0 (purple), `ansi6` 1 (cyan, Herdr's teal), `ansi2` 2 (green), `ansi3` 3 (orange), `ansi4` 4 (blue), `ansi1` 5 (pink); or a picked colour, `#` and six hex digits in any case, which takes the slot whose reference colour is nearest by squared distance in RGB, ties to the lower slot. The reference colours are `#8b7cf6`, `#22a99a`, `#3fae6a`, `#e3814f`, `#5b8def` and `#d9649a`, slots 0 to 5. Any other value is skipped.
- When several kept entries reach one repo, one whose path is the repo's own folder beats one inside it; among equals, the first in the file wins.
- A missing file means no workspace colours. So does a file the bridge cannot read, one that is not a regular file (a pipe or device; a symbolic link to a regular file is read), one over 1 MiB (it reads at most 1 MiB and one byte), one that is not a JSON object, one whose `version` is not the number 1, or one whose `spaces` is present but not a list (or null); so does a `MEAT_MODE_SPACE_COLOURS` that is not an absolute path, or no known home folder with the variable unset. A bad file is never mended from an earlier read. The bridge logs one warning when such a problem first appears or its message changes, not on every cycle, and logs nothing more once the file reads cleanly or is missing.
- A writer replaces the file whole (write a temporary file beside it, then rename it over), and writes it only when its content changes.

On the hub, `spaceColour` is checked with the rest of the report, after the existing checks, so a report with a bad name and a bad colour gives the name's error. It is applied after folder renames in the same write and before new repos get least-used slots, so a renamed repo takes the reported slot, and a reported slot, whether it moves an existing repo or sets a new one, counts as held for the repos filled after it (hidden repos excepted, as above). It applies to hidden repos too. The personal list never takes one. A restore gives restored lists their least-used slots, and the next report sets workspace colours again.

**Icons.** A bridge looks for an icon in each repo it reports and takes the first candidate that is a regular file of at most 64 KiB, whose real path (symbolic links resolved) is inside the repo's real path, and whose bytes `api.SniffIcon` accepts (an SVG, PNG or ICO). A candidate that fails any of these is skipped. The candidates, in order:

1. For each name in `favicon.svg`, `icon.svg`, `favicon.png`, `apple-touch-icon.png`, `icon.png`, `favicon.ico`: that name in the repo's root, then in `public/`, `static/`, `app/`, `src/`, `src/app/`, `assets/` and `web/public/`.
2. In `index.html`, `public/index.html`, `src/index.html` and `web/index.html`, in that order, each `<link>` whose `rel` holds the token `icon` or `apple-touch-icon` (any case), in document order: its `href` without query or fragment, read relative to that file's folder. A URL with a scheme, a `data:` URL, a path starting with `/` and one that leaves the repo are skipped. Only the first 256 KiB of each file is read, with a tolerant scan, not a full HTML parser.
3. Each folder `apps/<name>/` and `packages/<name>/` one level down, in byte order of path: for each name in 1, that name in the folder itself, then in its `public/`, `static/` and `icons/`.

It sends the icon with `PUT /bridge/v1/icons?repo=` after a repo report that holds the repo succeeds, when the icon's `api.IconHash` differs from the last one the hub took for that repo; after a start it sends every repo's icon once, an empty body for a repo with none, so the hub drops one that went away while the bridge was down. It looks again at most once every 10 minutes per repo, sends an unchanged icon again once an hour so a hub restored from a backup gets it back, and keeps what it sent in memory. It opens a candidate without following a final link and without blocking, and checks it is the same regular file it looked at. A network error, a `5xx` or a `401` leaves it unsent, to try at the next look. A `404` (a hub older than icons) is logged once, and the bridge sends no icons to that hub for an hour. A `409` forgets what was sent for the repo, so the next report sends it again. A `400` or `413` is logged once per hash, and that hash is not sent again.

The hub reads the body up to 64 KiB plus one byte (past that, `413`), checks it with `api.SniffIcon` (`400`), then the repo (`409`). It keeps each repo's icon apart from its items, with its type, and sets the `Repo`'s `icon` to its hash; the same icon on two repos is stored for each. An empty body clears the repo's icon, and is `204` when it had none. A body with the hash the repo already has changes nothing. An icon goes with its repo row: the repo dropped from its bridge's report, the bridge revoked, or the repo moved to a new folder name (the bridge sends it again under the new name). The hub never parses an icon. `GET /api/icons/{hash}`, behind the session like `/api/state` (an `<img>` sends the session cookie), serves the bytes of any repo holding that hash with its type, `Cache-Control: private, max-age=31536000, immutable`, `X-Content-Type-Options: nosniff`, `Cross-Origin-Resource-Policy: same-origin` and `Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'; sandbox`. The app shows it only as an `<img>`, never inline, so an SVG's scripts never run.

### Epics

The Epics view shows, read-only, the epics of the repos whose epics are switched on and the beads under them, grouped by repo. It is apart from the person's own to-dos: epic rows are not items, so they never appear in `State`, Today, the calendar feed, reminders, push, counts, the "new" marks or the export, and no action takes one. A row that is also a task for the person is that task (see below).

**Switching on.** A repo's epics switch on and off in the app, either way, as its sync switch does: the Epics view's **Repos** menu lists each repo whose tracker can show epics, checked where its epics are on, and a pick switches them, showing "Waiting for" the machine until it applies it. The app's `epics` action is applied by the bridge, which accepts it only for a repo it found, saves it in the `epics` map of `bridge.json` and sends its new repo list before reporting the result. At the agent machine, `meat-mode bridge epics on <repo>` and `meat-mode bridge epics off <repo>` set the same entry; `meat-mode bridge epics` alone lists the repos the bridge finds whose tracker can show epics, with their switch. `<repo>`, the file's lock and the reading before each repo report work as for `meat-mode bridge agents`. The switch is apart from the repo's sync switch: either may be on without the other.

**What the bridge sends.** For each repo with `epicsOn` whose tracker can show epics, the bridge reads every task, closed ones included, and sends the rows this rule selects, the same for every tracker:

- a card (`epic: true`) for each bead that is not closed and whose type is `epic` or that has a child, open or closed;
- every direct child of a card, whatever its status; a child that is itself a card is both a row of its parent and a card;
- every bead that is not closed, has no parent and is not a card: the repo's top-level tasks.

A row keeps `parent` only when it names a card in the set, so an open bead under a closed or missing parent is a top-level task. Every row carries `description` and `notes`, the bead's own notes field (see Descriptions below); no row carries comments, design or acceptance text. `human` marks a bead labelled `human`. A set holds at most 2000 rows; past that the bridge drops closed rows first. The bridge leaves out a bead whose id the hub cannot take, as it does for items.

The bridge reads a repo for epics at most once a minute, and sends its whole set with `PUT /bridge/v1/epics?repo=` when it differs from the last set it sent, and at least once every 10 minutes. A set that comes back empty when the last one sent was not is sent only when the next read is empty too, as a tracker can answer an empty list by mistake. A `404` (a hub older than Epics) is logged once and the bridge sends no epics to that hub for an hour; a `409` makes it report its repos again first; a `400` is logged for the repo and the set is sent again only once it changes. None of these stops the bridge's other work.

The hub checks a set as `api.CheckEpicRows` does. Like items, it truncates what one bead could get wrong rather than refuse the repo: a title past 500 characters, a description or notes past 20000, a priority outside 0 to 4 (clamped), a status other than the five (read as `open`), a `parent` that names no card in the set, or whose chain of parents leads back to the row (cleared), a `blockedBy` id that is not a bead id, repeats or names the row (dropped, at most 20 kept, none on a closed row), a `deferUntil` on a row that is not deferred (cleared), and a negative `commentCount` (zero). It refuses (`400`) only a set of more than 2000 rows, a row whose id is not a bead id or appears twice, or more than 4 MiB of description and notes. It replaces the repo's rows in one transaction, sets the repo's `syncedAt`, and sends `event: epics`, unchanged sets included. Rows go with their repo: the repo reported without `epicsOn: true` or not at all, the bridge revoked, and a repo moved to a new folder name (the bridge sends its set again under the new name) all drop them. Between an app's switch-off and the bridge's next report, the hub still takes a set, but `GET /api/epics` leaves the repo out.

The app's `epics` action answers `404` for a repo the hub does not hold and `400` for one whose tracker cannot show epics (`epics` false).

**`GET /api/epics`** answers `Epics`: `{"repos": [RepoEpics]}`, ordered by bridge then repo, each `{"bridge", "repo", "tracker"?, "syncedAt", "rows": [EpicRow]}` with rows ordered by id. A repo with `epicsOn` that has sent no set yet, or whose switch-off is pending, is left out. An `EpicRow` is `{"id", "title", "type"?, "status", "priority", "assignee"?, "parent"?, "epic"?, "human"?, "description"?, "notes"?, "updatedAt", "closedAt"?, "blockedBy"?, "deferUntil"?, "commentCount"?}`. `blockedBy` lists the open beads the row waits on, as its tracker reports them (for beads, `bd blocked`); `deferUntil` is when a deferred bead wakes; `commentCount` is how many comments the bead has. `notes` is the bead's notes field (bd's `notes`); a tracker without one leaves it out. A hub or bridge older than these fields leaves them out, and the app then shows no wait, date, count or notes.

**Descriptions.** Every row carries its bead's description and notes, each cut to 20000 characters. Past 4 MiB over the set, `tracker.Outline` keeps descriptions first, then notes, each in this order, and drops the rest: cards, then rows that are not closed, then closed rows, each by id.

**In the app.** Epics is a view in the sidebar and the phone's tabs, shown when any repo's tracker can show epics (`epics`), switched on or not; with none switched on, it points at its Repos menu. It fetches `GET /api/epics` when it opens, on each `event: epics` while it shows, when the set of repos with `epicsOn` changes in `State` while it shows, when the event stream reconnects, and when the page becomes visible again.

- **Toolbar.** One line with the view's title: the title, a search box (title or id, across repos), then three menus; on a phone it may wrap to two. Repo picks one or more repos (each with its mark and its count of open rows). Filter picks statuses and priorities. View picks the sort and has Expand all and Collapse all. Each active filter shows below as a chip with its own remove button, then Clear; with none, that row is absent. On a phone the menus open as sheets. The filters, sort and folds last while the app is open.
- **Needs attention.** Above the repos, every row that is not closed and is Critical (priority 0) or Blocked, across the repos the filters leave: the repo's mark, status, title and priority. It shows 5, then "Show N more", folds, and is absent when empty.
- **Repos.** Each repo is a heading with its mark, its name, its counts (epics, tasks, open) and when it last synced, folding from the right. One bar in the repo's colour runs down its whole section.
- **Rows.** In each repo, epic cards and top-level tasks are peers in one list of compact rows, sorted together: by priority, then status (blocked, in progress, open, waiting, deferred), then most recently updated. A row shows a type icon, its id, title, its comment count when it has comments, status, priority (Critical in red, High in yellow) and assignee. An open row with `blockedBy` shows as Waiting, naming the first bead it waits on ("Waiting on gd-7 +1"), and a deferred row with `deferUntil` its wake date ("Deferred to Nov 24"); the Filter menu offers Waiting beside the five statuses. A card shows its status only when it is not open, and beside its progress how many of its tasks are blocked and waiting. Details list what a waiting row waits on. The terminal viewer draws the same states with the same shapes, words and colours. A card adds a slim progress line: closed direct children over all of them, the percentage rounded down ("85% 6/7 done", "0% 0/0 done" for none), and a button that expands its children, sorted the same way, closed ones last. A child that is itself a card is a row there and a card of its own in the repo's list. Copy id shows when the row is pointed at or focused.
- **Details.** Any row opens read-only Details: title, the repo's mark and name, id with Copy, status, priority, type, assignee, its card (which opens that card's Details), when it was updated and closed, how many comments it has, and its description and notes as Markdown, as the task sheet renders them. A card's Details also list its children. Details are a side panel on a wide screen and a sheet on a phone. A row with `human` whose key `<bridge>/<repo>/<id>` is an item in `State` shows that item's title and status, which win over the row's; its Details say it is also your task and offer Open task, which opens the item's task sheet with every action.

### Export and restore

The hub holds the only copy of the personal list, the projects, their attachments, the action log and the settings kept in the app. An export takes them out as one zip file; a restore loads that file into a fresh hub. The Go types are `api.Export` and `api.RestoreResult`.

| Method and path | Body | Response |
|---|---|---|
| `GET /api/export` | | `200`, the export, `Content-Type: application/zip`, with `Content-Disposition: attachment; filename="meat-mode-export-<YYYY-MM-DD>.zip"` (the date in the hub's time zone, UTC while it has none) |
| `POST /api/restore` | the export file as it was downloaded: the zip, or the JSON file of an older hub | `200` and `{"items", "actions", "reminders", "cancelled", "machines", "calendarFeed", "attachments"}`; `409` on a hub that already has data; `400` for a file that is not an export, is from a newer hub or fails a check below. Bodies up to 64 MiB plus `api.MaxAttachmentsBytes`. |

**The file.** The export is a zip of `export.json` (`api.ExportJSONName`), the JSON below, and one entry `attachments/<id>` (`api.ExportAttachmentDir`) holding the bytes of each attachment it lists, stored without compression. The JSON is the manifest: each attachment's `size` and `sha256` are in its item. The hub writes the zip as it sends it, in constant memory.

Files are not put in the JSON as base64: that grows them by a third, makes the hub hold the whole file in memory to write and read it, and would need the restore cap raised by the same third. Every app of this kind that restores its own attachments (Memos, Vikunja, Joplin, Trilium) exports a zip or tar of metadata plus raw files ([research/0006-rich-todos](research/0006-rich-todos/RESEARCH.md)).

**What an export holds.** `export.json` is `{"format": "meat-mode-export", "version": 6, "exportedAt", "settings", "items", "actions", "reminders", "origins", "machines", "hiddenProjects"}`:

- `settings` is `{"name", "timeZone", "reminderLead", "calendarFeed"}`: the first three as `GET /api/settings` shows them, except that `name` is `""` while none was chosen, and `calendarFeed` whether the calendar feed was on.
- `items` holds every item of the lists the hub keeps, the personal list and every project, closed ones included however old: the `Item` fields as `GET /api/state` shows them, with `key`, `bridge` (`hub`), `repo` (the list), `firstSeen`, `seenAt`, `labels` (those that pass the rules in Tags: an item moved off a machine's personal repo may hold bead labels that do not, and they are left out so the export always restores) and `attachments` (those not removed, each with `size` and `sha256`), and no `run`, `pending` or `error`. `owner` names the machine whose agents added the item, `""` (left out) for one added in the app.
- `actions` is the whole action log, every machine's included, oldest first, each as `GET /api/actions` shows it, `machine` included.
- `reminders` lists the reminders already sent for those items: `{"key", "kind", "at"}`, where `kind` is `due` (at the due time or a snooze end, `at` being that moment) or `early` (`at` being the due time it came before).
- `origins` lists the items moved from a machine's personal repo: `{"machine", "beadId", "itemId"}`.
- `machines` names the machines paired when the export was made, revoked ones left out.
- `hiddenProjects` names the projects deleted in the app (see Deleting a project), so a restored hub keeps them out of the app too. A restore refuses a name that is not a project name, or one listed twice.

**What it leaves out.** Removed attachments not yet purged stay out. No secret is exported: no machine token, password hash, login session, setup code, calendar feed address, Web Push subscription or VAPID key. Items from a machine's repos stay out too, whatever its tracker (beads, ticket, Backlog.md): each is a copy of a file on that machine, which stays its source of truth and sends it again once paired. So do the hub's copies of agent runs and its record of the run ends it pushed.

**Versions.** `version` goes up by one whenever what an export holds changes in any way, an added field included, so a hub never restores a file it would read only in part. A hub restores a file of its own version and of every older one, upgrading an older file step by step to its own before loading it. It refuses a file from a newer hub with `400` and says which version the file is and which this hub reads, so the user updates the hub first. A file whose `format` is not `meat-mode-export`, or whose `version` is not a whole number from 1 up, is refused as not an export.

- Version 1 was a bare JSON file with no attachments and no tags on hub list items. Version 2 made the export a zip and added `labels` and `attachments` to its items. Its upgrade step sets `version` and drops any label of a version 1 item that breaks the rules in Tags: such labels came from beads moved off a machine's personal repo, version 1 never showed them, and a file that restored before must still restore.
- Version 3 dropped checklists. Its upgrade step turns each item's `checklist` into sub-tasks by the schema 14 rule in Sub-tasks, made at the file's `exportedAt`, and drops `checklist` from the `set` and `was` of the edits in `actions`.
- Version 4 added `machine` to `actions`: the machine whose token posted an agent's action on the lists the hub keeps. Its upgrade step only sets `version`; an older action names no machine. A restore refuses a `machine` that is not a valid machine name.
- Version 5 added deleted projects: `hiddenProjects`, and the `project-delete` and `project-restore` actions with their `items`. Its upgrade step only sets `version`; an older file deleted no project.
- A restore reads a zip by its first bytes (`PK\x03\x04`) and anything else as bare JSON. A bare JSON of version 2 or later restores only when it lists no attachment; one that lists any is refused, naming how many files it lists, so the user restores the zip it came in.

**Restore.** Restore is offered by the hub part of the setup wizard on a hub whose setup status is `new`, after the password step:

- It refuses with `409` on a hub that already has data: any item, any action, or any machine other than `hub`, revoked ones included. It never merges into or replaces data, so nothing a hub holds is lost to a restore. The settings the wizard has already set do not count as data; the export's replace them.
- It checks the whole file before it writes anything, and loads it in one transaction, so a file that fails a check changes nothing. It saves an uploaded zip to a temporary file in the data directory, since a zip is read from its end, and deletes that file when done. Attachment files are written before the transaction commits and deleted again if it does not. Each item must belong to the personal list or a valid project name (see Identity), with a valid id unique in its list, a title, a known status and the limits an edit holds to; each action needs a valid id, unique in the file; each `key` must name an item in the file; and the settings must pass `PUT /api/settings`'s checks. Tags must pass the rules in Tags. Each attachment must be on an item of the file, with an id unique in the file, a clean name, and a size within the caps in Attachments; its entry must be in the zip, with that size and SHA-256. A missing entry, a size or hash that does not match, an entry the JSON does not list, or any entry name other than `export.json` and `attachments/<id>` refuses the file, naming the attachment and its item. No file is ever skipped. Entry names are never used as paths: the hub writes each file under the id the JSON gives.
- It keeps every item's id, key, times, comments, repeat, parent, tags and attachments; every action's id, order, status, times, author and machine; and each owner, reminder and origin. An action still `pending` is loaded as `cancelled`, with `error` "Not sent: the hub was restored from an export.", so a machine paired again never replays an old change and nothing waits for a hub that will never apply it.
- It does not record the setup wizard as done; the wizard goes on to its next step.
- The answer counts what it loaded, `attachments` included, and lists what to do again: pair each machine in `machines`, turn the calendar feed on again when `calendarFeed` is true, and turn notifications on, on each device. A machine paired again under its old name is again the owner of the items its agents added.


## Bridge API

Every request carries `Authorization: Bearer <token>`. The token names the bridge; a bridge never names itself in a body.

| Method and path | Body | Response |
|---|---|---|
| `POST /bridge/v1/hello` | `{"version", "bdVersion", "trackers"?, "harness"?}`: `trackers` lists `{"name", "version"?}` for each tracker whose check passed; `bdVersion` is beads' version, empty without beads; `harness` is the agent harness the bridge runs for Ask agent, with its `limits` (see Harness) | `{"bridge": "<name>", "serverTime"}`; updates last-seen and the bridge's `trackers` and `harness`. A hello without `harness` clears it. The bridge also sends one after it applies a `limits` action |
| `GET /bridge/v1/settings` | | `{"name", "timeZone", "reminderLead"}`, as `GET /api/settings`. Unlike hello it does not update last-seen, so `meat-mode add` can read the zone without showing the machine as connected |
| `PUT /bridge/v1/repos` | `[]Repo` (without `bridge`, `pending`, `agentsPending`, `epicsPending`, `colour` and `icon`, which the hub ignores in a report): every repo the bridge found, `spaceColour` (optional) the slot of its workspace colour (see List identity), `optedIn` true for those it syncs, `agentsAllowed` true for those whose agents switch is on, `tracker` and `actions` as its tracker declares them (see Trackers), `epics` true when its tracker can show epics and `epicsOn` true for those whose epics switch is on (see Epics) | `204`; replaces this bridge's repo list. A repo no longer reported with `epicsOn: true` loses its epic rows |
| `PUT /bridge/v1/items?repo=<repo>` | `[]Item` (without `key`, `bridge`, `repo`, `pending`) | `204`; the complete set of human items for that repo. Items missing from the set are dropped from the hub. The repo is a query parameter because it holds `/`. An item's `parent` must be a valid id other than its own. An item may carry `notes`, its tracker's notes field (bd's `notes`), cut to 20000 characters like `description`; the app shows it read-only, and a hub list's items have none. |
| `PUT /bridge/v1/epics?repo=<repo>` | `[]EpicRow` | `204`; the complete epic rows for that repo (see Epics). `409` when the bridge's latest repo report does not hold the repo or shows it without `epicsOn: true`, never `404`; `400` for a set the rules there refuse; bodies up to the items route's limit |
| `PUT /bridge/v1/icons?repo=<repo>` | the icon's bytes, `Content-Type: application/octet-stream`; an empty body when the repo has none | `204`; sets or clears the repo's icon (see List identity). `409` when the bridge's latest repo report does not hold the repo, never `404`; `400` for bytes `api.SniffIcon` refuses; `413` past 64 KiB |
| `GET /bridge/v1/actions` | | `[]Action` for this bridge with status `pending`, oldest first |
| `POST /bridge/v1/actions/{id}/result` | `{"ok": true, "beadId"?: "…"} ` or `{"ok": false, "error": "…"}` | `204`; marks it `applied` or `failed`. `beadId` is the created bead for `add`. |
| `PUT /bridge/v1/runs` | `[]RunReport`: `{"repo", "beadId", …Run, "applying"?}` for every run the bridge knows, live and the latest settled one per bead; `applying` is the id of an ask or stop already applied to that run whose result the bridge has not posted yet | `204`; replaces this bridge's run list. The hub shows each on the item it names (see Agent runs) and ignores one whose item it does not hold. |
| `POST /bridge/v1/personal[?project=<name>]` | `PersonalTodo` | `201` with `{"key", "id"}` of the new item on the personal list or the project, or `200` with the same for a repeated `id` |
| `GET /bridge/v1/personal[?project=<name>]` | | `200` with `[]Item` of the personal list or the project (see Agents: reading the lists) |
| `GET /bridge/v1/projects` | | `200` with `[{"name", "open"}]` (see Agents: reading the lists) |

The bridge sends its run list whenever a run changes, at most once a second, and with every full push. It also sends it right before the result of an ask or stop, naming that action in `applying`: the hub then shows the action's note but not its effect on the run, which the list already holds. It keeps a bead's latest settled run while the bead is in its set.

The bridge sends the full set per repo whenever any fingerprint in it changed. The set holds every open, in-progress, blocked and deferred item labelled `human`, plus items closed in the last 7 days so the Done section and Undo work.

### Agents: adding to the personal list

An agent adds a personal to-do with `POST /bridge/v1/personal` and the bridge token of its machine. No bridge needs to run, and the post leaves the bridge's last-seen time alone, so an agent's add never makes a stopped bridge look online. On a paired machine, `meat-mode add` makes this call with the bridge's own config (see [deploy.md](deploy.md#let-agents-add-to-your-personal-list)).

`PersonalTodo` is `{"id"?, "title", "due"?, "repeat"?, "parent"?, "note"?, "labels"?, "author"?}`:

- `title` is required, up to 1000 characters.
- `due` is an RFC 3339 time.
- `repeat` is a `Repeat` (see Repeating to-dos) and needs `due`.
- `parent` is the `id` of an item of the same list, open and top-level, whoever added it; the new item is its sub-task (see Sub-tasks). An id the list does not hold answers `404`; a closed parent or a sub-task `400`.
- `note`, up to 10000 characters, becomes the item's first comment.
- `labels` are the item's tags, by the rules in Tags. `meat-mode add --tag <tag>` sets one and repeats for more.
- `author` is the agent's name, shown as the item's creator and the note's author. It defaults to the token's machine name.
- `id` is a UUID the agent generates. Posting the same `id` again returns the item it added and changes nothing, so an agent can retry safely.

```sh
curl -fsS https://meat-mode.example.com/bridge/v1/personal \
  -H "Authorization: Bearer $MEAT_MODE_TOKEN" -H 'Content-Type: application/json' \
  -d '{"title": "Renew the library card", "due": "2026-03-02T09:00:00Z", "author": "planner"}'
```

### Agents: reading the lists

Any paired machine's token reads every item of the personal list and of every project, whoever added it: the user in the app, any machine's agents, or the move from a bridge's personal repo. A paired machine can therefore read all of the user's personal data on the hub's lists, and change it as below. There is no setting that narrows this; revoking a machine ends it.

| Method and path | Response |
|---|---|
| `GET /bridge/v1/personal[?project=<name>][&status=…][&q=…][&tag=…]` | `200` with `[]Item`, oldest first by `firstSeen`, each as `GET /bridge/v1/personal/{id}` shows it. `status` is `open` (the default: every item not closed), `closed` (done or deleted, for the 7 days the app shows them) or `all`; any other value answers `400`. `q` keeps items whose title holds it, without case. `tag` keeps items with that tag, without case, and repeats: an item must have every tag given. Sub-tasks are listed like other items, with `parent`. A project with nothing in it gives `[]`. |
| `GET /bridge/v1/projects` | `200` with `[{"name", "open"}]` by name: each project the app shows (see Identity) and how many of its items are open. |

`project` and its `400` work as in Agents: projects. These calls leave the bridge's last-seen time alone. On a paired machine, `meat-mode list` (with `--project`, `--closed` or `--all`, `--search` and `--tag`), `meat-mode show <id>` and `meat-mode projects` make them; each takes `--json` to print the hub's answer as it is.

### Agents: changing personal to-dos

An agent can read and change any personal to-do, whoever added it. An id the list does not hold, and a closed item past the 7 days the app shows it, answer `404`.

| Method and path | Body | Response |
|---|---|---|
| `GET /bridge/v1/personal/{id}` | | `200` with the `Item`, including a closed one still shown |
| `POST /bridge/v1/personal/{id}/actions` | `PersonalChange` | `201` with the applied `Action`, or `200` with the same for a repeated `id` |

Like the add, these calls leave the bridge's last-seen time alone, and take `?project=<name>` for a to-do in a project. An `edit` with `labels` sets the to-do's tags. `meat-mode edit --tag <tag>` adds a tag and `--untag <tag>` removes one, each repeatable: the command reads the to-do, works out the new list and sends it as `labels`. On a paired machine, `meat-mode edit`, `meat-mode done` and `meat-mode reopen` make them with the bridge's own config (see [deploy.md](deploy.md#let-agents-add-to-your-personal-list)).

`PersonalChange` is `{"id"?, "kind", "payload", "author"?}`. `kind` is `edit` (payload `{"set": Edit}`), `done`, `reopen`, `dismiss` or `note` (payload `{"text"}`), with the rules of the app's actions above. `id` and `author` work as for `PersonalTodo`. An `edit`'s `parent` and `subtasksDone` name items of the same list, whoever added them; an id the list does not hold answers `404`.

The applied `Action` carries `author` and `machine`, the name of the machine whose token posted it; so do the `add` and its note. The app's recent actions (`GET /api/actions`) and the item's History in the task sheet (`GET /api/items/{key}/history`) show both, and History offers Undo on the change (see Item history). An agent's change sends no notification of its own; a due time it sets reminds as any other does.

```sh
curl -fsS https://meat-mode.example.com/bridge/v1/personal/mm-12/actions \
  -H "Authorization: Bearer $MEAT_MODE_TOKEN" -H 'Content-Type: application/json' \
  -d '{"kind": "edit", "payload": {"set": {"due": "2026-03-09T09:00:00Z"}}, "author": "planner"}'
```

### Agents: projects

`POST /bridge/v1/personal?project=<name>`, `GET /bridge/v1/personal?project=<name>`, `GET /bridge/v1/personal/{id}?project=<name>` and `POST /bridge/v1/personal/{id}/actions?project=<name>` work as above on the project `<name>` instead of the personal list. Without `project` they use the personal list. The first add to a project makes it, and any machine reads and changes every item there, as on the personal list; an item in another project, or on the personal list, answers `404`. A name that is not a repo name, or is `personal`, answers `400`, and so does an add to a name a paired machine reports as one of its repos, since that repo's tasks belong in its own tracker. That holds while the repo's syncing is off too, and the error then says so, so the agent can ask the user to switch it on; a revoked machine's repos do not count. A retry of an add already made still answers. On a paired machine, `meat-mode add`, `list`, `show`, `edit`, `done` and `reopen` take `--project <name>`, and `--project .` names the project after the Git repo of the current folder as the bridge names repos (see Identity), a linked worktree answering for its main checkout.

```sh
curl -fsS 'https://meat-mode.example.com/bridge/v1/personal?project=projects/garden' \
  -H "Authorization: Bearer $MEAT_MODE_TOKEN" -H 'Content-Type: application/json' \
  -d '{"title": "Approve the footer text", "note": "What is needed, why, and where to find it.", "author": "planner"}'
```

The reply's `key` is `hub/projects/garden/<id>`. There is no call to rename a project, and agents cannot delete one; it leaves the app once nothing in it is left to show, or when the user deletes it in the app (see Deleting a project), and an add to its name brings it back.

### Moving a bridge's personal repo to the hub

Before the hub kept the personal list, each bridge sent a personal beads repo (repo `personal`, `personal: true`). The hub moves those items into its own list, once each:

- On every start, the hub moves the items it holds of each bridge's personal repo, with their titles, dates, status, comments, recent closed items, seen times and fired reminders. Actions on them still waiting for that bridge are applied on the hub instead; an action on a bead the hub never had is cancelled.
- A bridge from before the change still sends its personal repo. Each bead in it that the hub has not had yet joins the list; a bead it already moved is left as the hub has it, so a rerun never duplicates or overwrites anything.
- A moved bead keeps its id unless another personal item has it, in which case it gets a `-2`, `-3`… suffix.

The personal repo never appears as a list or in Settings. Current bridges no longer send it, and `meat-mode bridge pair` has no `--personal`. After the move, the beads repo on the agent machine is no longer read; keep it or delete it.

### Password

While the hub has no password, each start makes a one-time setup code shaped `XXXX-XXXX-XXXX`, writes it to the hub's log and to the file `setup-code` in the data directory (readable by the hub's user only), and keeps only its SHA-256. The code lasts an hour and works once; restarting the hub makes a new one and ends the old. A hub with a password makes no code and removes a leftover file.

| Method and path | Body | Response |
|---|---|---|
| `POST /api/setup/claim` | `{"code": "…", "password": "…"}` | `204` and the session cookie: the password is set, the code is used up, its file is removed and this browser is logged in. The code matches with or without dashes and spaces, in any case. `400` for a password under 8 characters; `403` for a wrong, expired or used code; `429` with `Retry-After` after 5 wrong codes within a minute, counted for the whole hub; `409` when the hub already has a password, saying so when it comes from the environment. Needs no session. |
| `PUT /api/password` | `{"current": "…", "password": "…"}` | `204`; sets a new password. This browser stays logged in; every other session ends and its push subscriptions are dropped. `400` for a new password under 8 characters; `403` `{"error": "wrong password"}` for a wrong current one, counted with wrong logins; `429` as for login; `409` when its password comes from the environment. |

A wrong code or current password answers `403`, not `401`, because the app takes a `401` to mean its session ended.

## Configuration

Hub, from environment variables (see `.env.example`): `MEAT_MODE_LISTEN`, `MEAT_MODE_DATA_DIR`, `MEAT_MODE_PUBLIC_URL`, `MEAT_MODE_ALLOWED_HOSTS`, `MEAT_MODE_PASSWORD_HASH`, `MEAT_MODE_PASSWORD_HASH_FILE`, `MEAT_MODE_USER`, `MEAT_MODE_VAPID_SUBJECT`, `MEAT_MODE_NTFY_URL`, `MEAT_MODE_NTFY_TOPIC`, `MEAT_MODE_NTFY_TOKEN`, `MEAT_MODE_PUSH_ALLOW`, `MEAT_MODE_TRUSTED_NETWORKS`.

Bridge: `MEAT_MODE_TRUSTED_NETWORKS` at pairing (see Private network only). `MEAT_MODE_BRIDGE_TOKEN` gives `meat-mode bridge pair <url>` its token without putting it on the command line. Agent runs read `MEAT_MODE_AGENT_COMMAND`, `MEAT_MODE_AGENT_NAME`, `MEAT_MODE_AGENT_TIMEOUT`, `MEAT_MODE_AGENT_BUDGET_USD`, `MEAT_MODE_AGENT_RUNS_PER_TASK` and `MEAT_MODE_AGENT_CONCURRENT` (see `.env.example` and deploy.md).

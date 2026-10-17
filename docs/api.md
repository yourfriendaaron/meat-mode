# Wire contract

The hub serves two HTTP APIs: one for the app (`/api/…`) and one for bridges and agents (`/bridge/v1/…`). The Go types are in `internal/api`; this page is the prose version. All bodies are JSON. Times are RFC 3339 strings in UTC. Errors are `{"error": "<plain message>"}` with a 4xx or 5xx status.

## Identity

- A **bridge** has a unique name chosen when it is added (`[a-z0-9-]{1,32}`). The name `hub` is reserved.
- The **personal list** belongs to the hub itself: it is repo `personal` of the reserved bridge `hub`, with `personal: true`. The hub stores its items and applies every action on them at once, so it works with no bridge running. Its items are keyed `hub/personal/<id>`; the hub gives a new item the id `mm-<n>`, and keeps the bead id of an item moved from a bridge (see below).
- A **repo** is named by the bridge with its folder's path relative to the home folder, such as `projects/garden` (for a `--root` outside the home folder, relative to that root's parent). Repo names are unique per bridge, so two folders called `garden` stay apart. A name is up to 512 bytes of `/`-separated segments; each segment is 1 to 128 characters with no control character, is not `.` or `..`, and does not start with `-`. The app shows the last segment, and the path where two repos share it.
- An **item key** is `<bridge>/<repo>/<bead id>`, for example `laptop/projects/garden/gd-1`. Bridge names and bead ids hold no `/`, so the repo is everything between the first and the last `/`. In URL paths the key is percent-encoded as one segment. Bead ids may not start with `-`; the bridge passes bead ids to `bd` only when they match `[A-Za-z0-9][A-Za-z0-9._-]{0,127}`.
- A hub that stored repos under an older bridge's folder names (`garden`) moves each one, with its items, actions and reminders, to the one new name that ends in it (`projects/garden`) when the bridge first sends its new list. A folder name that two new names end in is dropped instead.
- An **action id** is a UUID the client generates. Posting the same id twice returns the stored action and changes nothing.

## App API

The hub refuses to start without an `https://` `MEAT_MODE_PUBLIC_URL` (plain `http://` on localhost only). Whether the app has a login depends on the hub's settings (see Login below). With no login, the app relies on the network boundary: it is reached only over Tailscale (docs/deploy.md), so only the user's own devices reach it. Either way, the hub refuses requests another site could make from the user's browser:

- A `Host` other than `localhost`, `127.0.0.1`, `[::1]`, the host of `MEAT_MODE_PUBLIC_URL` or one listed in `MEAT_MODE_ALLOWED_HOSTS` gets `421` (on `/api/` and the app's pages).
- An `/api/` request (any method) or a non-GET page request whose `Origin` is neither the request's own origin nor that of `MEAT_MODE_PUBLIC_URL` gets `403`, and so does an `/api/` request whose `Sec-Fetch-Site` is `same-site` or `cross-site`. A plain subresource load such as an image carries no `Origin`, so in a browser too old to send `Sec-Fetch-Site` a hostile page can still tie up event stream slots.
- A non-GET `/api/` request without `Content-Type: application/json` gets `415`, bodiless ones included.

| Method and path | Body | Response |
|---|---|---|
| `GET /api/state` | | `State`: every item, repo and bridge, the count of unapplied actions, server time |
| `GET /api/events` | | Server-sent events: `event: change` with `data: {"rev": n}` whenever state changes; the app then re-fetches `/api/state`. Past 32 open streams the hub answers `503` with `Retry-After: 30`. |
| `POST /api/actions` | `NewAction` | `201` with `Action` (or `200` with the stored one for a duplicate id) |
| `POST /api/actions/{id}/cancel` | | `200` with `Action` (status `cancelled`) when it was not yet applied; `409` when it already was |
| `GET /api/actions?limit=n` | | Recent actions, newest first, for the sync popover |
| `POST /api/items/{key}/seen` | | `204`; marks replies on the item read |
| `GET /api/push/key` | | `{"publicKey": "<VAPID public key>"}` |
| `POST /api/push/subscriptions` | `PushSubscription` (the browser's JSON) | `204` (upsert by endpoint); `400` unless the endpoint is an https URL on a known push service (Mozilla, FCM, Apple, Windows). The hub keeps the 20 newest. |
| `DELETE /api/push/subscriptions` | `{"endpoint": "…"}` | `204` |
| `GET /api/bridges` | | `[]Bridge` |
| `POST /api/bridges` | `{"name": "…"}` | `201` with `{"name", "token"}`; the token is shown once |
| `DELETE /api/bridges/{name}` | | `204`; revokes the token |

### Login

Login is on when the hub has a password hash (`MEAT_MODE_PASSWORD_HASH`, or `MEAT_MODE_PASSWORD_HASH_FILE` naming a file that holds it). The hash is an Argon2id string in PHC format (`$argon2id$v=19$m=…,t=…,p=…$<salt>$<hash>`), printed by `meat-mode hash-password`. Setting both variables, or a value that is not such a string, stops the hub at start with a plain message. With no hash, login is off: no route needs a session, `GET /api/session` reports `authenticated: true`, `POST /api/logout` answers `204` and does nothing, and `POST /api/login` answers `404`.

When login is on, every `/api/` route except the three below answers `401` with `{"error": "log in first"}` unless the request carries a valid session cookie. That includes `GET /api/events`, so an app whose session ended sees its event stream fail and checks `GET /api/session` before reconnecting. The three routes below still pass the Host, Origin and Content-Type checks above; they only skip the session check. The app's pages and static files stay public; they hold no data. The bridge API keeps its bearer tokens and `/healthz` stays public.

| Method and path | Body | Response |
|---|---|---|
| `GET /api/session` | | `{"login": bool, "authenticated": bool}`: whether this hub has a login, and whether this request is logged in (always `true` when login is off) |
| `POST /api/login` | `{"password": "…"}` | `204` and the session cookie; `401` `{"error": "wrong password"}`; `429` with `Retry-After` after 5 failed logins within a minute, counted for the whole hub; `404` when login is off |
| `POST /api/logout` | | `204`; ends this session, clears the cookie and drops the push subscriptions it registered |

The session cookie is `meat_mode_session`: 32 random bytes, `HttpOnly`, `SameSite=Lax`, `Path=/`, and `Secure` when `MEAT_MODE_PUBLIC_URL` is `https://`. The hub stores only its SHA-256. A session lasts 90 days from its last use; the hub renews the cookie at most once a day. A session belongs to the password hash it was made under, so changing the hash logs every device out. Logins and the existing Host, Origin and Content-Type checks apply together: `POST /api/login` needs `Content-Type: application/json` like any other `/api/` write.

**Push subscriptions** belong to the session that registered them while login is on. Logging out, or the session expiring, stops reminders to that device. With login off a subscription belongs to no session; turning login on later drops those, and each device subscribes again once it logs in. Either way, a subscription that has not been registered again for 60 days is dropped; the app registers again each time it opens.

### Setup

The app offers a setup wizard in two parts: the hub part runs once per hub, and the device part runs on each device. The hub records only its own part; each device keeps its part, and a run in progress, in its own browser storage.

| Method and path | Body | Response |
|---|---|---|
| `GET /api/setup` | | `{"status", "user", "timeZone", "password", "tailscaleOnly"}`, below |
| `POST /api/setup` | `{"status": "done"}` or `{"status": "dismissed"}` | `204`; records the hub part as finished or closed. `400` for any other status. |

- `status` is `done` or `dismissed` once recorded. With no record it is `new` when the hub has no items and no machines (revoked ones count), and `in-use` otherwise. The app opens the wizard on its own only for `new`, so a hub upgraded with data never sees it; Settings starts it again on any hub.
- `user` is the name kept in the app, or `MEAT_MODE_USER`. `timeZone` is the IANA time zone kept in the app, `""` until chosen.
- `password` is `env` when `MEAT_MODE_PASSWORD_HASH` or `MEAT_MODE_PASSWORD_HASH_FILE` sets it, `app` when it was set in the app, and `none` otherwise. `tailscaleOnly` is `true` when the user chose to run with no password on a private network.

### Actions

`NewAction` is `{id, kind, key?, bridge?, repo?, payload}`.

| kind | target | payload | effect on the hub's copy (shown at once, `pending: true` until applied) |
|---|---|---|---|
| `done` | `key` | `{}` | status `closed`, close reason `Done in Meat Mode` |
| `reopen` | `key` | `{}` | status `open` |
| `snooze` | `key` | `{"until": time or null, "previous": time or null}` | `deferUntil = until`; no `until` clears the snooze |
| `dismiss` | `key` | `{}` | status `closed`, close reason `Dismissed` |
| `note` | `key` | `{"text": "…"}` | appends a comment by `<MEAT_MODE_USER>-app` |
| `add` | `bridge` + `repo` | `{"title": "…", "due": time or null}` | inserts a provisional item keyed `<bridge>/<repo>/pending-<action id>`; replaced by the real bead when the bridge reports it |
| `sync` | `bridge` + `repo` | `{"on": true or false}` | the repo shows `optedIn: on` with `pending: true` until the bridge reports its repo list again; switching off drops the repo's items then. Not allowed on the personal list. |

On the personal list (`hub/personal`) every action is applied by the hub when it is posted: the response already has status `applied`, the item never shows `pending`, and an `add` returns the new item's id as `beadId`. A closed personal item leaves the app 7 days after it closed, as a closed bead leaves a bridge's set.

Undo is the client's job: it cancels the closing action when it is still unapplied, or else posts the inverse (`reopen`, or `snooze` back to `previous`). Actions on the personal list are always applied, so their Undo is always the inverse.

An action's `status` is `pending`, `applied`, `failed` or `cancelled`. `failed` carries `error`; the app shows it on the item. A bridge fails an action on a bead it does not list in that repo with the error `unknown bead`.

A `sync` switch is applied by the bridge, not by bd. The bridge accepts it only for a repo it found itself, looked up by name, so the app cannot make it read anywhere else; any other name fails with `unknown repo`. It saves the choice in the `sync` map of its own `bridge.json`, where it overrides the repo's `meat-mode.sync` line, and sends its new repo list before reporting the result. A repo with no switch syncs when its `.beads/config.yaml` has the top-level line `meat-mode.sync: true`.

## Bridge API

Every request carries `Authorization: Bearer <token>`. The token names the bridge; a bridge never names itself in a body.

| Method and path | Body | Response |
|---|---|---|
| `POST /bridge/v1/hello` | `{"version", "bdVersion"}` | `{"bridge": "<name>", "serverTime"}`; updates last-seen |
| `PUT /bridge/v1/repos` | `[]Repo` (without `bridge` and `pending`): every repo the bridge found, `optedIn` true for those it syncs | `204`; replaces this bridge's repo list |
| `PUT /bridge/v1/items?repo=<repo>` | `[]Item` (without `key`, `bridge`, `repo`, `pending`) | `204`; the complete set of human items for that repo. Items missing from the set are dropped from the hub. The repo is a query parameter because it holds `/`. |
| `GET /bridge/v1/actions` | | `[]Action` for this bridge with status `pending`, oldest first |
| `POST /bridge/v1/actions/{id}/result` | `{"ok": true, "beadId"?: "…"} ` or `{"ok": false, "error": "…"}` | `204`; marks it `applied` or `failed`. `beadId` is the created bead for `add`. |
| `POST /bridge/v1/personal` | `PersonalTodo` | `201` with `{"key", "id"}` of the new personal item, or `200` with the same for a repeated `id` |

The bridge sends the full set per repo whenever any fingerprint in it changed. The set holds every open, in-progress, blocked and deferred item labelled `human`, plus items closed in the last 7 days so the Done section and Undo work.

### Agents: adding to the personal list

An agent adds a personal to-do with `POST /bridge/v1/personal` and the bridge token of its machine. No bridge needs to run, and the post leaves the bridge's last-seen time alone, so an agent's add never makes a stopped bridge look online. On a paired machine, `meat-mode add` makes this call with the bridge's own config (see [deploy.md](deploy.md#let-agents-add-to-your-personal-list)).

`PersonalTodo` is `{"id"?, "title", "due"?, "note"?, "author"?}`:

- `title` is required, up to 1000 characters.
- `due` is an RFC 3339 time.
- `note`, up to 10000 characters, becomes the item's first comment.
- `author` is the agent's name, shown as the item's creator and the note's author. It defaults to the token's machine name.
- `id` is a UUID the agent generates. Posting the same `id` again returns the item it added and changes nothing, so an agent can retry safely.

```sh
curl -fsS https://meat-mode.example.com/bridge/v1/personal \
  -H "Authorization: Bearer $MEAT_MODE_TOKEN" -H 'Content-Type: application/json' \
  -d '{"title": "Renew the library card", "due": "2026-03-02T09:00:00Z", "author": "planner"}'
```

### Moving a bridge's personal repo to the hub

Before the hub kept the personal list, each bridge sent a personal beads repo (repo `personal`, `personal: true`). The hub moves those items into its own list, once each:

- On every start, the hub moves the items it holds of each bridge's personal repo, with their titles, dates, status, comments, recent closed items, seen times and fired reminders. Actions on them still waiting for that bridge are applied on the hub instead; an action on a bead the hub never had is cancelled.
- A bridge from before the change still sends its personal repo. Each bead in it that the hub has not had yet joins the list; a bead it already moved is left as the hub has it, so a rerun never duplicates or overwrites anything.
- A moved bead keeps its id unless another personal item has it, in which case it gets a `-2`, `-3`… suffix.

The personal repo never appears as a list or in Settings. Current bridges no longer send it, and `meat-mode bridge pair` has no `--personal`. After the move, the beads repo on the agent machine is no longer read; keep it or delete it.

## Configuration

Hub, from environment variables (see `.env.example`): `MEAT_MODE_LISTEN`, `MEAT_MODE_DATA_DIR`, `MEAT_MODE_PUBLIC_URL`, `MEAT_MODE_ALLOWED_HOSTS`, `MEAT_MODE_PASSWORD_HASH`, `MEAT_MODE_PASSWORD_HASH_FILE`, `MEAT_MODE_USER`, `MEAT_MODE_VAPID_SUBJECT`, `MEAT_MODE_NTFY_URL`, `MEAT_MODE_NTFY_TOPIC`, `MEAT_MODE_NTFY_TOKEN`.

Bridge: `MEAT_MODE_BRIDGE_TOKEN` gives `meat-mode bridge pair <url>` its token without putting it on the command line.

# Wire contract

The hub serves two HTTP APIs: one for the app (`/api/…`) and one for bridges (`/bridge/v1/…`). The Go types are in `internal/api`; this page is the prose version. All bodies are JSON. Times are RFC 3339 strings in UTC. Errors are `{"error": "<plain message>"}` with a 4xx or 5xx status.

## Identity

- A **bridge** has a unique name chosen when it is added (`[a-z0-9-]{1,32}`).
- A **repo** is named by the bridge with its folder's path relative to the home folder, such as `projects/garden` (for a `--root` outside the home folder, relative to that root's parent), or `personal` for the personal database. Repo names are unique per bridge, so two folders called `garden` stay apart. A name is up to 512 bytes of `/`-separated segments; each segment is 1 to 128 characters with no control character, is not `.` or `..`, and does not start with `-`. The app shows the last segment, and the path where two repos share it.
- An **item key** is `<bridge>/<repo>/<bead id>`, for example `laptop/projects/garden/gd-1`. Bridge names and bead ids hold no `/`, so the repo is everything between the first and the last `/`. In URL paths the key is percent-encoded as one segment. Bead ids may not start with `-`; the bridge passes bead ids to `bd` only when they match `[A-Za-z0-9][A-Za-z0-9._-]{0,127}`.
- A hub that stored repos under an older bridge's folder names (`garden`) moves each one, with its items, actions and reminders, to the one new name that ends in it (`projects/garden`) when the bridge first sends its new list. A folder name that two new names end in is dropped instead.
- An **action id** is a UUID the client generates. Posting the same id twice returns the stored action and changes nothing.

## App API

The app relies on the network boundary: it is reached only over Tailscale (docs/deploy.md), so only the user's own devices reach it. No login in v1. The hub refuses to start without an `https://` `MEAT_MODE_PUBLIC_URL` (plain `http://` on localhost only). Because it has no credentials, the hub refuses requests another site could make from the user's browser:

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
| `sync` | `bridge` + `repo` | `{"on": true or false}` | the repo shows `optedIn: on` with `pending: true` until the bridge reports its repo list again; switching off drops the repo's items then. Not allowed on the personal repo. |

Undo is the client's job: it cancels the closing action when it is still unapplied, or else posts the inverse (`reopen`, or `snooze` back to `previous`).

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

The bridge sends the full set per repo whenever any fingerprint in it changed. The set holds every open, in-progress, blocked and deferred item labelled `human` (every item in the personal repo), plus items closed in the last 7 days so the Done section and Undo work.

## Configuration

Hub, from environment variables (see `.env.example`): `MEAT_MODE_LISTEN`, `MEAT_MODE_DATA_DIR`, `MEAT_MODE_PUBLIC_URL`, `MEAT_MODE_ALLOWED_HOSTS`, `MEAT_MODE_USER`, `MEAT_MODE_VAPID_SUBJECT`, `MEAT_MODE_NTFY_URL`, `MEAT_MODE_NTFY_TOPIC`, `MEAT_MODE_NTFY_TOKEN`.

Bridge: `MEAT_MODE_BRIDGE_TOKEN` gives `meat-mode bridge pair <url>` its token without putting it on the command line.

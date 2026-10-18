# Wire contract

The hub serves two HTTP APIs: one for the app (`/api/…`) and one for bridges and agents (`/bridge/v1/…`). The Go types are in `internal/api`; this page is the prose version. All bodies are JSON. Times are RFC 3339 strings in UTC. Errors are `{"error": "<plain message>"}` with a 4xx or 5xx status.

## Identity

- A **bridge** has a unique name chosen when it is added (`[a-z0-9-]{1,32}`). The name `hub` is reserved.
- The **personal list** belongs to the hub itself: it is repo `personal` of the reserved bridge `hub`, with `personal: true`. The hub stores its items and applies every action on them at once, so it works with no bridge running. Its items are keyed `hub/personal/<id>`; the hub gives a new item the id `mm-<n>`, and keeps the bead id of an item moved from a bridge (see below).
- A **repo** is named by the bridge with its folder's path relative to the home folder, such as `projects/garden` (for a `--root` outside the home folder, relative to that root's parent). Repo names are unique per bridge, so two folders called `garden` stay apart. A name is up to 512 bytes of `/`-separated segments; each segment is 1 to 128 bytes with no control character, is not `.` or `..`, and does not start with `-`. The app shows the last segment, and the path where two repos share it.
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
| `GET /api/actions?limit=n` | | Recent actions, newest first, for the sync popover; `limit` defaults to 50 and is capped at 500 |
| `POST /api/items/{key}/seen` | | `204`; marks replies on the item read |
| `GET /api/push/key` | | `{"publicKey": "<VAPID public key>"}` |
| `POST /api/push/subscriptions` | `PushSubscription` (the browser's JSON) | `204` (upsert by endpoint); `400` unless the endpoint is an https URL on a known push service (Mozilla, FCM, Apple, Windows). The hub keeps the 20 newest. |
| `DELETE /api/push/subscriptions` | `{"endpoint": "…"}` | `204` |
| `POST /api/reminders/test` | `{"endpoint": "…"}`, this device's push subscription (`""` when it has none) | `{"push", "ntfy"}`: sends a test reminder to that subscription only, and to ntfy when set up. With a login the subscription must belong to the calling session. `push` is `sent` (the push service accepted it), `none` (this device has no subscription), `expired` (the push service answered that the subscription is gone, or its endpoint is not a known push service, so the hub removed it and the app should subscribe again) or `failed`; `ntfy` is `sent`, `failed` or `off`. `404` when push is not set up. |
| `GET /api/bridges` | | `[]Bridge` |
| `POST /api/bridges` | `{"name": "…"}` | `201` with `{"name", "token"}`; the token is shown once |
| `DELETE /api/bridges/{name}` | | `204`; revokes the token |

### Login

Login is on when the hub has a password hash: one set in the app (see Password below), or one from the environment (`MEAT_MODE_PASSWORD_HASH`, or `MEAT_MODE_PASSWORD_HASH_FILE` naming a file that holds it), which wins over the app's. The hash is an Argon2id string in PHC format (`$argon2id$v=19$m=…,t=…,p=…$<salt>$<hash>`), printed by `meat-mode hash-password`. Setting both variables, or a value that is not such a string, stops the hub at start with a plain message. With no hash, login is off: no route needs a session, `GET /api/session` reports `authenticated: true`, `POST /api/logout` answers `204` and does nothing, and `POST /api/login` answers `404`.

When login is on, every `/api/` route except the three below and `POST /api/setup/claim` answers `401` with `{"error": "log in first"}` unless the request carries a valid session cookie. That includes `GET /api/events`, so an app whose session ended sees its event stream fail and checks `GET /api/session` before reconnecting. An open stream ends at its next heartbeat, within 25 seconds, after its session ends or login is turned on. The three routes below still pass the Host, Origin and Content-Type checks above; they only skip the session check. The app's pages and static files stay public; they hold no data. The bridge API keeps its bearer tokens and `/healthz` stays public.

| Method and path | Body | Response |
|---|---|---|
| `GET /api/session` | | `{"login": bool, "authenticated": bool}`: whether this hub has a login, and whether this request is logged in (always `true` when login is off) |
| `POST /api/login` | `{"password": "…"}` | `204` and the session cookie; `401` `{"error": "wrong password"}`; `429` with `Retry-After` after 5 failed logins within a minute, counted for the whole hub; `404` when login is off |
| `POST /api/logout` | | `204`; ends this session, clears the cookie and drops the push subscriptions it registered |

The session cookie is `meat_mode_session`: 32 random bytes, `HttpOnly`, `SameSite=Lax`, `Path=/`, and `Secure` when `MEAT_MODE_PUBLIC_URL` is `https://`. The hub stores only its SHA-256. A session lasts 90 days from its last use; the hub renews the cookie at most once a day. A session belongs to the password hash it was made under, so changing the hash in the environment logs every device out; changing it in the app keeps the device that changed it logged in. Logins and the existing Host, Origin and Content-Type checks apply together: `POST /api/login` needs `Content-Type: application/json` like any other `/api/` write.

**Push subscriptions** belong to the session that registered them while login is on. Logging out, or the session expiring, stops reminders to that device. With login off a subscription belongs to no session; turning login on later, in the environment or in the app, drops those, and each device subscribes again once it logs in. Turning login off, or changing the hash in the environment, ends the old sessions at the next start and drops their subscriptions; each device subscribes again when the app opens. Either way, a subscription that has not been registered again for 60 days is dropped; the app registers again each time it opens.

### Setup

The app offers a setup wizard in two parts: the hub part runs once per hub, and the device part runs on each device. The hub records only its own part; each device keeps its part, and a run in progress, in its own browser storage.

| Method and path | Body | Response |
|---|---|---|
| `GET /api/setup` | | `{"status", "user", "timeZone", "password", "tailscaleOnly"}`, below |
| `POST /api/setup` | `{"status": "done"}` or `{"status": "dismissed"}` | `204`; records the hub part as finished or closed. `400` for any other status. |
| `GET /api/setup/status` | | `{"login", "hostMatches", "publicUrl", "pushKeys", "ntfy", "time"}`, below |
| `POST /api/setup/test-event` | | `204`; sends a change event down every open event stream |

- `status` is `done` or `dismissed` once recorded. With no record it is `new` when the hub has no items and no machines (revoked ones count), and `in-use` otherwise. The app opens the wizard on its own only for `new`, so a hub upgraded with data never sees it; Settings starts it again on any hub.
- `user` is the name kept in the app, or `MEAT_MODE_USER`. `timeZone` is the IANA time zone kept in the app, `""` until chosen.
- `password` is `env` when `MEAT_MODE_PASSWORD_HASH` or `MEAT_MODE_PASSWORD_HASH_FILE` sets it, `app` when it was set in the app, and `none` otherwise. `tailscaleOnly` is `true` when the user chose to run with no password on a private network.

The connection check (a wizard step, also in Settings) combines what the browser sees with `GET /api/setup/status`:

- `login` is `environment`, `app` or `none`, by the same rule as `password` above.
- `hostMatches` is `true` when this request's Host is the host of `MEAT_MODE_PUBLIC_URL` (a default port counts as no port). `publicUrl` is that setting, so the app can say where to open it.
- `pushKeys` is `true` when the hub has Web Push keys to hand out; `ntfy` is `true` when `MEAT_MODE_NTFY_URL` is set. `time` is the hub's clock, which the app compares with the device's; more than a minute apart is reported.
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

### Actions

`NewAction` is `{id, kind, key?, bridge?, repo?, payload}`.

| kind | target | payload | effect on the hub's copy (shown at once, `pending: true` until applied) |
|---|---|---|---|
| `done` | `key` | `{}` | status `closed`, close reason `Done in Meat Mode`; a repeating personal item moves to its next date instead (see Repeating to-dos) |
| `reopen` | `key` | `{}` | status `open` |
| `snooze` | `key` | `{"until": time or null, "previous": time or null}` | `deferUntil = until`; no `until` clears the snooze |
| `dismiss` | `key` | `{}` | status `closed`, close reason `Dismissed` |
| `note` | `key` | `{"text": "…"}` | appends a comment by `<name>-app`, the name kept in Settings |
| `add` | `bridge` + `repo` | `{"title": "…", "due": time or null, "repeat"?: Repeat, "ask"?: true}` (`repeat` on the personal list only; `ask` only where agents are allowed) | inserts a provisional item keyed `<bridge>/<repo>/pending-<action id>`; replaced by the real bead when the bridge reports it. With `ask`, an agent starts on the new bead (see Agent runs), and the provisional item shows a `working` run at once. |
| `sync` | `bridge` + `repo` | `{"on": true or false}` | the repo shows `optedIn: on` with `pending: true` until the bridge reports its repo list again; switching off drops the repo's items then. Not allowed on the personal list. |
| `ask` | `key` | `{"text"?: "…"}` | wakes an agent on the item (see Agent runs). With `text`, appends it as a note first, as `note` does, in the same action. The item's `run` shows `working` at once; on a live run it counts the note in `queued` instead. |
| `stop` | `key` | `{}` | ends the item's live run; `run` shows `stopped` with reason `stopped` at once |
| `agents` | `bridge` + `repo` | `{"on": true or false}` | the repo shows `agentsAllowed: on` with `agentsPending: true` until the bridge reports its repo list again. Not allowed on the personal list. |
| `edit` | `key` | `{"set": Edit}` | sets each field `set` names; the hub records the old values as `was` |
| `note-edit` | `key` | `{"commentId": "…", "text": "…"}` | replaces that comment's text, up to 10000 characters; the hub records the comment as it was in `comment` |
| `note-delete` | `key` | `{"commentId": "…"}` | removes that comment; the hub records it as `comment` |
| `note-restore` | `key` | `{"comment": Comment}` | puts a removed comment back in its place by `createdAt`, with its id, author and time |

A note, or the note of an ask, keeps showing on the hub's copy after it is applied, until the bridge sends that repo's set again with the bead's own comment.

The last four change personal items only. The hub refuses them on an agent's item with `400`: an agent item's title, dates and priority belong to its agent, so ask for a change in a note.

`Edit` is `{"title"?, "description"?, "due"?, "clearDue"?, "priority"?, "checklist"?, "repeat"?, "clearRepeat"?, "remindBefore"?, "clearRemindBefore"?}`. A field left out stays as it is.

- `title` follows the `add` rules: trimmed, not empty, up to 1000 characters.
- `description` is up to 10000 characters; `""` clears it.
- `due` is a time; `"clearDue": true` removes the due date instead. Sending both is refused. Clearing the due date of a repeating item also ends its repeat.
- `priority` is 0 (highest) to 4, as in bd. A new personal item has 2. The app shows 0 and 1 as high priority on every item.
- `checklist` replaces the whole list of `{"id", "text", "done"}`. The hub gives an entry with no `id` a new one and refuses a repeated `id`. `text` is trimmed, not empty, up to 1000 characters; the list holds at most 100 entries. `[]` clears it.
- `repeat` sets the item's `Repeat` (see Repeating to-dos); `"clearRepeat": true` ends it. Sending both is refused, and so is a `repeat` on an item that has no due date and gets none in the same edit.
- `remindBefore` is the item's own lead time in minutes, `0` to `10080`, where `0` means no early reminder; `"clearRemindBefore": true` goes back to the hub's `reminderLead`. Sending both is refused.

The applied `edit` action carries `was`, an `Edit` with the same fields as `set` holding the values before it (`clearDue: true` for a due date that was not set, `clearRepeat: true` for no repeat, `clearRemindBefore: true` for no lead time of its own; an edit that clears the due date of a repeating item records its `repeat` too). Its Undo is an `edit` with `set` = `was`. The Undo of `note-edit` is a `note-edit` back to the text of the `comment` the applied action carries; the Undo of `note-delete` is `note-restore` with the `comment` the applied action carries.

### Repeating to-dos

A personal item can repeat: "Water the plants every day at 8:30pm". `Repeat` is `{"every", "interval"?, "weekdays"?, "fromDone"?, "timeZone", "until"?, "start"}`:

- `every` is `day`, `week`, `month` or `year`; `interval` repeats every n of them, `1` (the default) to `100`.
- `weekdays` picks the days of a weekly rule, `0` (Sunday) to `6`, each once. Left out, a weekly rule keeps the due date's weekday. Only a weekly rule takes it, and not with `fromDone`.
- `timeZone` is an IANA zone. The rule keeps the due time's wall-clock time and date in it, so 8:30pm stays 8:30pm across daylight saving changes. The app sends the device's zone; `meat-mode add` sends the hub's, or the machine's while the hub has none.
- `until` ends the series: "Water the plants every Sunday until 26 November". It is a date, `YYYY-MM-DD`, in `timeZone`, and the series' last occurrence is the last one on or before it. Left out, the item repeats for good. The hub refuses an `until` before the date of the item's due time, both when the rule is set and when the due time is moved. To change or remove it, send the whole `repeat` again with the new `until` or none.
- `start` is the occurrence the schedule counts from. The hub sets it to the due time whenever the rule is set, and when the due time is set to a time that is not one of the series' own dates; moving the due time onto one of them, as the Undo of a `done` does, keeps it. Sending a rule that differs from the item's own only in `until`, while the due time is one of the series' dates, also keeps it, so changing the end date never moves the series. The hub ignores a `start` a client sends. A monthly rule from the 31st lands on the last day of shorter months and returns to the 31st after them; a yearly rule from 29 February lands on 28 February.
- A rule needs a due time: the item's current occurrence. Moving the due date moves the series.

`done` on a repeating item keeps it open and moves it on:

- The due time becomes the next occurrence after both the current due time and now, so occurrences missed while it sat overdue are skipped, never stacked. With `fromDone`, it is the done day plus `interval` units, at the rule's time of day.
- When that next date falls after `until`, the series is over: `done` closes the item like any other, with no `was`, so its Undo is a `reopen`. The item keeps its `repeat`, so a `reopen` brings the series back at its last due time.
- Its snooze is cleared and every checklist entry is unchecked.
- The applied `done` carries `was`: `{"due", "checklist"}` as they were. Its Undo is an `edit` with `set` = `was`, not a `reopen`; the app tells the two apart by `was`.
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

On the personal list (`hub/personal`) every action is applied by the hub when it is posted: the response already has status `applied`, the item never shows `pending`, and an `add` returns the new item's id as `beadId`. A closed personal item leaves the app 7 days after it closed, as a closed bead leaves a bridge's set.

Undo is the client's job: it cancels the closing action when it is still unapplied, or else posts the inverse (`reopen`, or `snooze` back to `previous`). Actions on the personal list are always applied, so their Undo is always the inverse.

An action's `status` is `pending`, `applied`, `failed` or `cancelled`. `failed` carries `error`; the app shows it on the item, and for a failed `add`, whose bead never existed, in Recent actions with the title that was typed. A bridge fails an action on a bead it does not list in that repo with the error `unknown bead`.

An `agents` switch is applied the same way: the bridge accepts it only for a repo it found, saves it in the `agents` map of its `bridge.json`, and reports it as the repo's `agentsAllowed`. There is no config-file line for it: agents work only where the user switched them on in the app.

A `sync` switch is applied by the bridge, not by bd. The bridge accepts it only for a repo it found itself, looked up by name, so the app cannot make it read anywhere else; any other name fails with `unknown repo`. It saves the choice in the `sync` map of its own `bridge.json`, where it overrides the repo's `meat-mode.sync` line, and sends its new repo list before reporting the result. A repo with no switch syncs when its `.beads/config.yaml` has the top-level line `meat-mode.sync: true`.

### Agent runs

An ask wakes an agent on one item: it reads the item's latest state, then does the work and closes the bead, or replies with a note. Only the user's own actions wake an agent; an agent's comments and closes never do. The bridge runs the agent; the hub only carries the ask and shows what the bridge reports.

The hub refuses `ask` with `400` on a personal item (`hub/…`), and on an item, or an `add` with `ask`, in a repo whose `agentsAllowed` is off (a pending switch counts as where it was switched to). `stop` takes any item; on one with no live run it changes nothing. An `ask` is allowed on a closed item, so the user can tell the agent they did the task.

An item's `run` is its latest `Run`: `{"id", "state", "agent", "activity"?, "startedAt", "endedAt"?, "reason"?, "detail"?, "queued"?, "count"}`. No `run` means no agent was ever asked (idle).

- `state` is `working` while the agent runs; then `waiting` when the run ended after the agent wrote a note (it is waiting on the user), `done` when it ended with the bead closed, or `stopped`.
- `reason` says why a run stopped: `stopped` (by the user), `usage-limit`, `timeout` (past the machine's time limit for one run), `budget` (spent its budget for one run), `run-limit` (the item used up its lifetime number of runs), `busy` (too many runs live on that machine), `not-allowed` (the machine does not allow agents in that repo), `no-reply` (it ended without a note or closing the bead), `failed` (the agent command failed or reported an error), `crashed` (the agent process died), `restarted` (the bridge stopped while it ran) or `offline` (the bridge stopped reporting while it ran; set by the hub). `detail` is a plain sentence about it, such as when a usage limit resets.
- `agent` is the name to show for the agent: the bead's creator, else the bridge's configured agent name.
- `activity` is one line in plain words of what the agent is doing, such as `Editing notes.txt`; once the run ends, the last thing it did.
- `queued` counts the follow-up notes written to a live run that the agent has not reached yet: the newest `queued` of the user's comments on the item are still on their way.
- `count` is how many runs the item has had in its lifetime, this one included.
- `id` is the bridge's id for the run. A new run gets a new id; a follow-up into a live run keeps it.

One item never has two runs. An `ask` on an item whose run is `working` becomes a follow-up: its note goes to the live run, which reads it at its next step. With no note it changes nothing. An `ask` on a settled run starts a new run that continues the agent's earlier conversation on that item where the agent supports it.

The bridge applies an `ask` by writing its note to the bead first, then starting or following up the run, and reports the action applied once the note is written: a run that could not start shows `stopped` with its reason, and the note stays. A `stop` is applied when the run has ended.

While a run is `working` and its bridge has not been seen for 60 seconds, the hub shows the run `stopped` with reason `offline`, and keeps showing that until the bridge reports the run again.

#### Agent push

When a run ends the hub sends one notification for it, through Web Push and ntfy as reminders go: `<agent> replied` (`waiting`), `<agent> finished` (`done`) or `<agent> stopped` with the reason in words (`stopped`). It sends one per run end, keyed on the run's `id` and state, never one per agent comment mid-run, and none for a run the user stopped or on an item the user closed (its latest done, dismiss or reopen since the run started is a done or dismiss). A run that ended more than an hour before the hub first sees it sends nothing, so a bridge's first sync does not bring a flood. Its link opens the item, and the Reply action opens it at the note box: `/?item=<key>&reply=1`.

The Web Push payload is `{"title", "body", "url", "tag", "actions"?}`; `actions` is a list of `{"action", "title"}` where `action` is `reply`, `open` or `retry` (`retry` on `stopped` only), and the payload then also carries `"key"`, the item key. The service worker opens `url` for `open` and a tap on the notification, `/?item=<key>&reply=1` for `reply`, and for `retry` posts `{"kind": "ask", "key"}` as a new action. The payload stays within Web Push's size limit: with a long key or title the hub shortens the body, then the title, never the link, the key, the tag or the actions. ntfy gets the same title, body and link, with Reply and Open as view actions.

## Bridge API

Every request carries `Authorization: Bearer <token>`. The token names the bridge; a bridge never names itself in a body.

| Method and path | Body | Response |
|---|---|---|
| `POST /bridge/v1/hello` | `{"version", "bdVersion"}` | `{"bridge": "<name>", "serverTime"}`; updates last-seen |
| `GET /bridge/v1/settings` | | `{"name", "timeZone"}`, as `GET /api/settings`. Unlike hello it does not update last-seen, so `meat-mode add` can read the zone without showing the machine as connected |
| `PUT /bridge/v1/repos` | `[]Repo` (without `bridge`, `pending` and `agentsPending`): every repo the bridge found, `optedIn` true for those it syncs, `agentsAllowed` true for those whose agents switch is on | `204`; replaces this bridge's repo list |
| `PUT /bridge/v1/items?repo=<repo>` | `[]Item` (without `key`, `bridge`, `repo`, `pending`) | `204`; the complete set of human items for that repo. Items missing from the set are dropped from the hub. The repo is a query parameter because it holds `/`. |
| `GET /bridge/v1/actions` | | `[]Action` for this bridge with status `pending`, oldest first |
| `POST /bridge/v1/actions/{id}/result` | `{"ok": true, "beadId"?: "…"} ` or `{"ok": false, "error": "…"}` | `204`; marks it `applied` or `failed`. `beadId` is the created bead for `add`. |
| `PUT /bridge/v1/runs` | `[]RunReport`: `{"repo", "beadId", …Run}` for every run the bridge knows, live and the latest settled one per bead | `204`; replaces this bridge's run list. The hub shows each on the item it names (see Agent runs) and ignores one whose item it does not hold. |
| `POST /bridge/v1/personal` | `PersonalTodo` | `201` with `{"key", "id"}` of the new personal item, or `200` with the same for a repeated `id` |

The bridge sends its run list whenever a run changes, at most once a second, and with every full push. It keeps a bead's latest settled run while the bead is in its set.

The bridge sends the full set per repo whenever any fingerprint in it changed. The set holds every open, in-progress, blocked and deferred item labelled `human`, plus items closed in the last 7 days so the Done section and Undo work.

### Agents: adding to the personal list

An agent adds a personal to-do with `POST /bridge/v1/personal` and the bridge token of its machine. No bridge needs to run, and the post leaves the bridge's last-seen time alone, so an agent's add never makes a stopped bridge look online. On a paired machine, `meat-mode add` makes this call with the bridge's own config (see [deploy.md](deploy.md#let-agents-add-to-your-personal-list)).

`PersonalTodo` is `{"id"?, "title", "due"?, "repeat"?, "note"?, "author"?}`:

- `title` is required, up to 1000 characters.
- `due` is an RFC 3339 time.
- `repeat` is a `Repeat` (see Repeating to-dos) and needs `due`.
- `note`, up to 10000 characters, becomes the item's first comment.
- `author` is the agent's name, shown as the item's creator and the note's author. It defaults to the token's machine name.
- `id` is a UUID the agent generates. Posting the same `id` again returns the item it added and changes nothing, so an agent can retry safely.

```sh
curl -fsS https://meat-mode.example.com/bridge/v1/personal \
  -H "Authorization: Bearer $MEAT_MODE_TOKEN" -H 'Content-Type: application/json' \
  -d '{"title": "Renew the library card", "due": "2026-03-02T09:00:00Z", "author": "planner"}'
```

### Agents: changing their personal to-dos

An agent can read and change a personal to-do that its machine's token added, and no other: an item added from another machine, from the app, or moved from a bridge's personal repo answers `404`, the same as an id that does not exist.

| Method and path | Body | Response |
|---|---|---|
| `GET /bridge/v1/personal/{id}` | | `200` with the `Item`, including a closed one still shown |
| `POST /bridge/v1/personal/{id}/actions` | `PersonalChange` | `201` with the applied `Action`, or `200` with the same for a repeated `id` |

Like the add, these calls leave the bridge's last-seen time alone. On a paired machine, `meat-mode edit`, `meat-mode done` and `meat-mode reopen` make them with the bridge's own config (see [deploy.md](deploy.md#let-agents-add-to-your-personal-list)).

`PersonalChange` is `{"id"?, "kind", "payload", "author"?}`. `kind` is `edit` (payload `{"set": Edit}`), `done`, `reopen`, `dismiss` or `note` (payload `{"text"}`), with the rules of the app's actions above. `id` and `author` work as for `PersonalTodo`.

```sh
curl -fsS https://meat-mode.example.com/bridge/v1/personal/mm-12/actions \
  -H "Authorization: Bearer $MEAT_MODE_TOKEN" -H 'Content-Type: application/json' \
  -d '{"kind": "edit", "payload": {"set": {"due": "2026-03-09T09:00:00Z"}}, "author": "planner"}'
```

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
| `PUT /api/password` | `{"current": "…", "password": "…"}` | `204`; sets a new password. This browser stays logged in; every other session ends and its push subscriptions are dropped. `400` for a new password under 8 characters; `403` `{"error": "wrong password"}` for a wrong current one, counted with wrong logins; `429` as for login; `409` when the hub has no password or its password comes from the environment. |
| `PUT /api/setup/tailscale-only` | `{"on": true}` or `{"on": false}` | `204`; records that the user keeps the hub with no password on a private network, so setup stops asking for one. `409` for `on: true` when the hub has a password. Setting a password clears it. |

A wrong code or current password answers `403`, not `401`, because the app takes a `401` to mean its session ended.

## Configuration

Hub, from environment variables (see `.env.example`): `MEAT_MODE_LISTEN`, `MEAT_MODE_DATA_DIR`, `MEAT_MODE_PUBLIC_URL`, `MEAT_MODE_ALLOWED_HOSTS`, `MEAT_MODE_PASSWORD_HASH`, `MEAT_MODE_PASSWORD_HASH_FILE`, `MEAT_MODE_USER`, `MEAT_MODE_VAPID_SUBJECT`, `MEAT_MODE_NTFY_URL`, `MEAT_MODE_NTFY_TOPIC`, `MEAT_MODE_NTFY_TOKEN`.

Bridge: `MEAT_MODE_BRIDGE_TOKEN` gives `meat-mode bridge pair <url>` its token without putting it on the command line. Agent runs read `MEAT_MODE_AGENT_COMMAND`, `MEAT_MODE_AGENT_NAME`, `MEAT_MODE_AGENT_TIMEOUT`, `MEAT_MODE_AGENT_BUDGET_USD`, `MEAT_MODE_AGENT_RUNS_PER_TASK` and `MEAT_MODE_AGENT_CONCURRENT` (see `.env.example` and deploy.md).

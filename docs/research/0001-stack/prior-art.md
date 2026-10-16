# Prior art and delivery (meat-d0i.1, cluster: prior art and delivery)

Accessed 2026-10-16 with web search and the GitHub API. Grades: A controlled/large n, B single team/vendor internal/small n, C anecdote/opinion, V vendor guidance. "Highlights" means search excerpts only; "full" means the page body was fetched in full.

## Q1. Existing beads UIs and viewers

Finding 1.1: The beads project keeps a curated list of community tools. Web UIs listed: beads-ui (Node, kanban, live updates, uses the `bd` CLI "for Dolt compatibility"); beads-viewer (TypeScript/React Native, for Paseo workspaces, read-only: "never writes"). Terminal UI `bv` is listed as not compatible with Dolt-based beads v0.50+, though its own README now claims JSONL/SQLite sources.
- URL: https://github.com/gastownhall/beads/blob/main/docs/community-tools.md (also https://beads.gascity.com/community-tools). Date: undated page, read 2026-10-16. Q1. Grade V (project docs). Highlights only.

Finding 1.2: mantoni/beads-ui is the most active general web UI. 755 stars, last push 2026-08-11, 44 open issues. Features: issues list with inline edit, epics view, board (Blocked / Ready / In progress / Closed), keyboard navigation, multi-workspace, live updates by watching the beads database. Binds 127.0.0.1:3000 by default; `HOST`/`PORT` env override. It runs `bd` commands with `--sandbox` by default (skips auto push/pull); `BDUI_BD_SANDBOX=0` disables.
- URLs: https://github.com/mantoni/beads-ui/ , https://thrum.team/docs/guides/beads-ui-setup.html , stars/push via https://api.github.com/repos/mantoni/beads-ui . Q1. Grade B. Highlights only.
- Not a human-queue app: it is a developer-facing issue board. No reminders, snooze, push, or phone-first layout is documented.

Finding 1.3: Other write-capable web viewers shell out to `bd --json`, the same approach the bridge plans.
- mgalpert/beads-viewer: Express + WebSocket server "shells out to the `bd` CLI for all operations" (`bd create/update/close/list/ready/blocked --json`), optimistic UI updates with rollback on error, a file watcher on `.beads/issues.jsonl`. Pinned to beads CLI v0.9.6, so stale. Roadmap lists "Mobile-responsive design improvements" as unchecked. https://github.com/mgalpert/beads-viewer . Grade B. Highlights only.
- ntellis/beads-dashboard: Python stdlib server + Vite React build committed to the repo; REST over `bd` CLI incl. create, comment, close, reopen, PATCH (status, priority, claim); WebSocket that "polls `.beads/` for file modification times"; falls back to 10 s polling. https://github.com/ntellis/beads-dashboard . Grade B. Highlights only.
- maphew/beady: Go, htmx + picocss, server-side Graphviz, needs `bd` on PATH for writes. https://github.com/maphew/beads-ui . Grade B. Highlights only.
- beadbox/beadbox: Tauri v2 desktop GUI over `bd` (needs bd >= 1.1.0); "Beadbox never touches the database behind `bd`'s back"; live updates by watching the workspace filesystem or polling Dolt table hashes; opt-in pilot using `bd serve` reads on loopback with a token-authenticated listener. https://github.com/beadbox/beadbox . Grade B. Highlights only.
- Dicklesworthstone/beads_viewer (`bv`): Go Bubble Tea TUI, 1714 stars, pushed 2026-10-16. Graph analytics (PageRank, critical path), kanban, `--robot-*` JSON modes, a standalone HTML graph export. Read-only triage tool. https://github.com/dicklesworthstone/beads_viewer . Grade B. Highlights only.

Finding 1.4: beads already has a "human queue" primitive, which Meat Mode can build on.
- `bd human list|respond|dismiss|stats`. `list` returns issues carrying the `human` label (`--status`, `--json`). `respond <id> -r "text"` adds a comment `Response: <text>` and closes the issue with reason "Responded". `dismiss <id> [--reason]` closes it with reason "Dismissed" (or "Dismissed: <reason>"). Both warn (not fail) when the `human` label is missing, and error if the issue is already closed.
- Source: https://github.com/steveyegge/beads/blob/4c63e151/cmd/bd/human.go (read as search excerpt, code is long) and https://beads.gascity.com/cli-reference/human . Q1. Grade V. Highlights only.
- Human-approval gates are a second mechanism: `bd gate create --type human`, an `issue_type=gate` bead with `await_type=human`; resolving closes the gate and unblocks dependents. Seen in a third-party viewer's code, https://github.com/brendan-appstart/bead-me-up-scotty/blob/main/lib/beads-view.ts , and in the beads CLI reference (`bd gate resolve`). Grade B. Highlights only.
- **Inference**: the hub's "human queue" can be defined as (label `human`, status not closed or deferred) plus unresolved human gates. Done = `respond`/close, dismiss = `human dismiss`, note = comment. Snooze and add have no `bd human` verb: snooze maps to `bd defer`/`--defer` style fields and add maps to `bd create` plus `label add human`; these need checking against bd 1.3.1 (not tested here).

Finding 1.5: A third-party app already uses the exact "Needs You" idea. brendan-appstart/bead-me-up-scotty has a "Needs You" inbox, with `needsHuman()` = `human` label and status not closed/deferred, and `readyHumanGate()` for gates not blocked by open dependencies. Only the helper file was seen; whether it writes back, and what its stack and maturity are, is unknown. https://github.com/brendan-appstart/bead-me-up-scotty . Grade C. Highlights only.

Result for Q1: I searched GitHub, the beads community-tools page and general web results. I found no existing app that is a phone-first human-queue to-do list with snooze, reminders, push and write-back. Closest: beads-ui (write-capable, developer board) and bead-me-up-scotty (inbox idea). Nothing found for a native mobile client that writes (beads-viewer for Paseo is read-only). **Inference**: Meat Mode is not duplicating an existing tool.

Reusable lessons (**Inference**):
- Every write-capable viewer goes through `bd --json`, not the database directly, and Beadbox states that as a rule. This supports the bridge design.
- Change detection is by file watch or polling in all of them. The `bd events` journal and `bd serve` are not used by any of them except Beadbox's opt-in pilot; the bridge would be an early user.
- `bd serve` and an HTTP backend are labelled experimental in the beads README ("HTTP Backend (experimental)", https://github.com/gastownhall/beads, highlights only). Treat the CLI as the stable contract.

## Q2. Web Push reliability for a self-hosted PWA

Finding 2.1: HTTPS with a publicly trusted cert is required; Tailscale certs qualify.
- `PushManager`/`PushSubscription` are `[SecureContext]`: without it "the interfaces are not exposed". https://progressivewebapps.com/background/push-notifications/ (undated). Grade C/V. Highlights only.
- Tailscale: "you need a TLS certificate from a public Certificate Authority"; `tailscale cert` uses Let's Encrypt, 90-day expiry. Certs made with `tailscale cert` files must be renewed by the operator; `tailscale serve` provisions them automatically. https://tailscale.com/docs/how-to/set-up-https-certificates . Grade V. Highlights only.
- Practitioner confirmation: a raw tailnet IP over http is not a secure context; a `*.ts.net` name over `tailscale serve` is; "The certificate is publicly trusted, so browsers treat the page as a secure context: push notifications ... all work". Use the name, not the 100.x address. https://www.executionassociates.com/projects/lasso/docs/deployment/tailnet/ and https://github.com/JoseRFelix/salchi/blob/master/docs/tailscale-serve-pwa-push.md . Grade C. Highlights only.
- Delivery does not need the tailnet: the same lasso page says "Notifications still arrive when the phone is off the tailnet. They are delivered by the phone's push service ... Only opening one needs the tailnet". It follows from the protocol (hub POSTs out to the vendor push service). Hub needs outbound internet only. Grade C.
- A push subscription belongs to an origin. Two hostnames for one app mean two subscriptions and duplicate notifications ("Pick one origin per device"). Same lasso page. Grade C. **Inference**: pick one canonical hub URL and do not change it, or users must resubscribe.

Finding 2.2: Per-platform behaviour.
- Chrome Android and Firefox Android: the OS wakes the browser; "regardless of whether the app is closed or not". https://web.dev/articles/push-notifications-faq (Matt Gaunt, undated). Grade V. Highlights only.
- Firefox Android: Push API since 48; `subscribe()` needs a user gesture since Firefox 79 on Android; Firefox does not require `userVisibleOnly` and uses a quota of 16 silent pushes instead. https://progressivewebapps.com/background/ (undated). Grade C (reference site compiled from MDN/BCD). Highlights only. Practical rule: always show a notification per push.
- iOS: web push works only for Home Screen web apps, iOS 16.4+; in a Safari tab `Notification` and `PushManager` are undefined; the permission request must come from a user gesture inside the installed app. Safari removes subscriptions after the third push that shows no notification, and expects `showNotification` within about 30 seconds. https://progressivewebapps.com/background/ and https://openpwa.net/reference/notifications/web-push/ (2026-06-24); Apple doc exists at https://developer.apple.com/documentation/usernotifications/sending-web-push-notifications-in-web-apps-and-browsers (excerpt empty, not read). Grade C/V. Highlights only.
- Desktop: the browser process must be running. Chrome/Firefox on Windows exit with the last window unless background mode is on; queued messages are held by the push service and delivered when the browser reopens, subject to TTL. https://pingwire.dev/blog/why-didnt-my-web-push-notification-arrive (2026-08-20), https://documentation.onesignal.com/docs/en/browser-behavior-and-unsubscribes . Grade C/V. Highlights only.
- Chrome: "Chrome and Edge ... may remove notification permission from sites the user has not visited in a while" (and a note that in October 2025 Google announced automatic removal of notification permission for some sites). https://progressivewebapps.com/background/ . Grade C. Highlights only; the announcement text was truncated, not verified.
- Vendor endpoints: Chrome uses `fcm.googleapis.com`, Firefox `updates.push.services.mozilla.com`, Apple `*.push.apple.com`. https://progressivewebapps.com/background/push-notifications/ . Grade C. Highlights only.

Finding 2.3: Known failure modes and what the hub must do.
- 404 and 410 from the push service mean the subscription is gone; delete it. 413 = payload over 4 KB; 429 = rate limit (honour Retry-After); 401/403 = VAPID problem (bad JWT, clock skew, changed key). https://openpwa.net/reference/notifications/web-push/ (2026-06-24), https://progressivewebapps.com/background/push-notifications/ . Grade C. Highlights only. Sakai's production code deletes on 410, 404 and 400 and only logs 403: https://www.longsight.com/blog/push-notifications-blog-post/ (2025-07-10). Grade B.
- `expirationTime` is `null` in every current engine; "in practice you cannot rely" on `pushsubscriptionchange`. It is absent on iOS, and Firefox before 137 fires it without `oldSubscription`/`newSubscription`. https://openpwa.net/reference/notifications/push-subscription-expiry/ (2026-10-09). Grade C. Highlights only. **Inference**: on every app open, compare `registration.pushManager.getSubscription()` with the hub's stored endpoint and re-send when they differ. The progressivewebapps.com guidance says the same ("Re-send the current subscription from the page on every visit").
- Subscriptions also die when site data is cleared, the app is reinstalled, or permission is revoked. https://documentation.onesignal.com/docs/en/web-push-setup-faq , pingwire post above. Grade V/C.
- Changing the VAPID key kills every subscription ("All users stopped at once": VAPID keys changed); `subscribe()` with a different `applicationServerKey` throws `InvalidStateError` until unsubscribed. pingwire post, openpwa page. Grade C. **Inference**: generate the VAPID pair once, persist it in the hub volume, back it up.
- Android Doze and battery saver batch background wake-ups; a low-urgency push may wait until the phone is used. Use the `TTL` and `Urgency` headers (RFC 8030) honestly: `high` for due reminders, a TTL so stale reminders expire. pingwire post. Grade C. Android 13+ also needs the browser app's own notification permission. https://progressivewebapps.com/background/ . Grade C.
- Focus / Do Not Disturb modes deliver silently. pingwire post. Grade C.
- Apple asks operators to allow `*.push.apple.com` and prefers VAPID JWTs reused rather than re-signed more than once an hour (per progressivewebapps.com summary of Apple's guidance; not verified at the source). Grade C.

Finding 2.4: **Inference** for the hub design: Web Push is reliable enough for a single user on Android Chrome and iOS home-screen apps, but no source gives a measured delivery rate (I found no grade A or B delivery-rate data). That is the argument for keeping ntfy as the optional second channel for Firefox Android and for battery-restricted phones, and for a fallback in-app badge on open. Nothing tested on a device.

## Q3. ntfy publishing API

All from https://docs.ntfy.sh/publish/ (full fetch of the page body, vendor docs, grade V, undated; read 2026-10-16).
- Minimal request: `POST /<topic>` (PUT also works), body is the message. `curl -d "text" https://host/topic`. Topic charset `[-_A-Za-z0-9]`, up to 64 chars; on a server without access control the topic name is "essentially a password".
- Auth, when the server has access control: `Authorization: Bearer tk_...` (access token), or `Authorization: Basic <base64 user:pass>`, or `?auth=<base64 of the Authorization header value>` for cases where headers are hard to set. Basic auth is only base64, so use HTTPS. Access tokens start with `tk_`. (The page lists the Bearer form with curl `-u :tk_...`, `--token=`, and an `Authorization: Bearer` header in its examples.)
- Click action: header `Click: <url>` (alias `X-Click`). Opens the URL when the notification is tapped; `https://` opens the browser or a registered app; other URI schemes also work. This is the deep-link into the PWA (use the canonical hub URL, e.g. `/item/<id>`).
- Priority: header `Priority: 1..5` or `min|low|default|high|max|urgent` (aliases `X-Priority`, `prio`, `p`). 5/max/urgent = long vibration + pop-over; 4/high = pop-over; 3 default; 2 low = no sound, not shown until drawer pulled; 1 min = under the fold.
- Other useful headers: `Title` (alias `t`), `Tags`, `Markdown: yes`, `Actions: view, <label>, <url>[, clear=true]` (up to 3 buttons; `http` action can POST back to the hub from the notification), `Delay`/`At` for scheduled delivery, and updating/clearing/deleting notifications by sequence ID (exists in the page's table of contents; the semantics section was not read).
- Limits: message body up to 4,096 bytes before it becomes an attachment (full text on page). Non-ASCII headers: ntfy accepts UTF-8 headers but some HTTP clients do not; RFC 2047 encoding is supported as a workaround.
- Minimal request shape the hub would send (**Inference**, assembled from the documented headers):
```
POST https://<ntfy-host>/<topic>
Authorization: Bearer <token>
Title: Water the plants
Priority: 4
Click: https://<hub-host>/item/<id>
Tags: alarm_clock
(body) Due now
```
- Not read: server-side ACL setup (`ntfy token add`, `ntfy access`), rate limits for a self-hosted server, and the iOS limitation that instant delivery relies on the ntfy.sh upstream server for APNs. These need a second look if iOS delivery via ntfy matters.

## Q4. Offline-first hub/agent sync with an append-only action log

Finding 4.1: The standard shape is a transactional outbox on the producer with at-least-once delivery, plus an idempotent consumer keyed on a stable ID. Every library below states the same contract: "Delivery is at least once ... handlers and downstream consumers must be idempotent. Publish `Record.ID` as the stable message identifier so downstream consumers can deduplicate retries."
- velmie/outbox (Go, MySQL): https://github.com/velmie/outbox . Grade B. Highlights only.
- faustbrian/go-outbox quickstart: "Persist the envelope ID or an application idempotency key in the same transaction as the consumer side effect. If the key already exists, acknowledge the duplicate without repeating the side effect. ... Using it does not change delivery to exactly once." https://github.com/faustbrian/go-outbox/blob/main/docs/quickstart.md . Grade B. Highlights only.
- nest-native/messaging (TypeScript): idempotent inbox via a unique `(source, message_key)` row written in the same transaction as the side effect, "effective exactly-once"; supports SQLite. https://github.com/nest-native/messaging . Grade B. Highlights only. NestJS/Drizzle is heavier than the hub needs, but the SQLite inbox table is the pattern.
- Outboxy: "Outbox alone provides at-least-once delivery. Consumers must be idempotent"; inbox dedup by `ON CONFLICT DO NOTHING`. https://github.com/Outboxy/outboxy . Grade B. Highlights only. (Service form, too big for this use.)

Finding 4.2: Small, directly relevant Go example: cognis-digital/safequeue is a single-file append-only `queue.log` (one JSON record per mutation: enqueue, lease, ack, nack, dead), `fsync` per write, replay on open, tolerates a torn final record, idempotent enqueue by key, standard library only. https://github.com/cognis-digital/safequeue . Grade C (unknown author, unknown adoption). Highlights only. Useful as a reference for log format and crash recovery, not as a dependency.

Finding 4.3: A client-to-server variant matches the Meat Mode flow (phone or bridge pushes actions, hub dedups): adi2355/Cloud-Native-Backend-Platform (TypeScript) uses a client-side local outbox, `POST /sync/push` with a `clientSyncOperationId` (unique key, cached `resultPayload` so retries return the same answer), a version field for conflict detection and cursor-paged `GET /sync/changes?cursor=`. https://github.com/adi2355/Cloud-Native-Backend-Platform . Grade C (portfolio project). Highlights only.

**Inference**: Meat Mode's design in four rules.
1. Each user action gets a client-generated ID (ULID or UUIDv7) at the moment it is made, written to the hub's append-only log with a unique index; a replayed POST with the same ID returns the stored result.
2. The bridge pulls unapplied actions with a cursor (last applied log sequence), applies each through `bd`, then acks with the action ID and outcome. If the ack is lost, the bridge re-applies, so every apply must be idempotent at the bd level or checked first (for example, closing an already-closed issue is an error in `bd human respond`, which is a usable "already done" signal; a comment add is not idempotent and needs a marker such as the action ID in the text, or a check of existing comments).
3. Beads-side changes come back to the hub as snapshots or events with a version, never as actions.
4. Outbox row states needed: pending, applied, failed-permanent (with reason). A permanent failure (issue deleted, label gone) must surface in the UI, not retry forever.
- I did not find a Go or TypeScript library that fits the "SQLite hub plus remote bridge, no broker" shape out of the box; the practical route is a small bespoke implementation on SQLite using the patterns above.

## Q5. Natural-language date parsing for quick add

Finding 5.1: chrono-node (TypeScript) is the clear choice.
- Repo wanasit/chrono: 5,292 stars, 133 open issues, latest release v2.10.3 published 2026-10-12 (adds Arabic and Indonesian, month/ordinal Spanish dates, a night-range rollover fix, a fractional "quarter" fix). Sources: https://api.github.com/repos/wanasit/chrono/releases/latest , https://api.github.com/repos/wanasit/chrono . Grade B (read directly from the API).
- v2.9.0 dropped the dayjs dependency, so it relies "only on native Javascript date/time" and v2.9.0 also changed reference-date handling to use the assigned timezone (previously the system timezone). https://npmx.dev/package-changelog/chrono-node/v/2.9.0 . Grade B. Highlights only.
- Default parser covers international English; also fi, fr, id, it, ja, nl, ru, uk, vi and partial de, es, pt, sv, zh. https://www.npmjs.com/package/chrono-node . Grade V. Highlights only.
- API: `chrono.parse(text, ref?, option?)` returns results with text span and components (lets you strip the matched phrase from the title), `parseDate` returns a Date. Options include `forwardDate`; `timezones` override; `strict` vs `casual` configs. https://cdn.jsdelivr.net/npm/chrono-node@2.10.1/src/index.ts . Grade V. Highlights only.
- Maintenance: single maintainer; he once described the project as "a personal challenge I do from time to time" (2019-02-06, https://github.com/wanasit/chrono/issues/281), yet activity in 2026 shows regular releases with many contributors. Grade C.
- Browser: the package is TypeScript with no dayjs dependency and is available from CDNs. **Inference**: it runs in the browser without Node APIs; not tested here. A bundle-size figure was not collected.

Finding 5.2: Go options are weaker.
- olebedev/when: 1,463 stars, 18 open issues, latest commit 2026-10-14 (API), latest tag v1.1.0 (tag date not read). Pluggable rules; README says "more-or-less complete state" and "Pull requests are welcome" for unimplemented merge strategies. https://github.com/olebedev/when , https://pkg.go.dev/github.com/olebedev/when . Grade B. Highlights only.
- tj/go-naturaldate: 315 stars, 8 open issues. Latest commit on the default branch 2020-06-08 (API commits endpoint); deps.dev "Maintained 0/10", zero commits in last 90 days. Built for date ranges in a log-search CLI; open issues include "Add noon & midnight" and "Support 6 month with the future direction". Ambiguous names like `sunday` default to the past (`WithDirection` changes it). https://github.com/tj/go-naturaldate , https://deps.dev/project/github/tj%2fgo-naturaldate . Grade B. Highlights only. **Inference**: wrong direction by default for a to-do app (people mean the next Sunday).
- Not found: any Go parser with a published accuracy comparison against chrono.

**Inference** for the decision: if the parser runs in the browser (Bun/Node/TS stack), use chrono-node with `forwardDate: true`; if the hub is Go and parsing must run server-side, `when` is usable but needs a test suite of the phrases the app promises ("tomorrow 9am", "next fri", "in 2 weeks"); a TS-only parser in a Go hub implies shipping the parse to the browser and sending a resolved timestamp, which also settles timezone (the browser knows the user's zone). Sending the original text along with the resolved time keeps the log replayable.

## Gaps
- No delivery-rate data (grade A/B) for Web Push on any platform; claims are from vendor docs and practitioner posts.
- No hands-on test of: `bd human` verbs and `defer` on bd 1.3.1, the bd events journal, ntfy ACL setup, or chrono-node bundle size.
- ntfy: server-side rate limits and iOS behaviour for self-hosted servers not read.
- bead-me-up-scotty seen only through one file; no stars, license or write-back behaviour verified.
- Apple's own web push doc and the Chrome 2025 permission-removal announcement were not read at the source.

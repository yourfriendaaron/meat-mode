# 0001: Stack and the bridge's beads interface

Status: accepted. Research notes: `docs/research/0001-stack/` (`stack.md`, `bd-interface.md`, `prior-art.md`).

## Decision

**One Go module, one binary, `meat-mode`, with two roles:** `meat-mode hub` (the container) and `meat-mode bridge …` (the native command on the agent machine). Frontend is vanilla TypeScript bundled with esbuild and embedded in the binary. The bridge talks to beads through the **bd CLI with `--json`**, polling.

| Part | Choice |
|---|---|
| Language | Go (version pinned in `go.mod`), `CGO_ENABLED=0` everywhere |
| Database | `modernc.org/sqlite` (bundles SQLite 3.53.4 at v1.60.1) |
| Web Push | `github.com/SherClockHolmes/webpush-go` (aes128gcm, VAPID) |
| Optional notifier | ntfy over plain HTTP `POST /<topic>` with `Authorization: Bearer`, `Click`, `Priority` headers |
| HTTP | Go standard library `net/http` (1.22+ pattern routing); no framework |
| Frontend | Vanilla TypeScript, no framework, ES modules bundled by esbuild into `web/dist`, embedded with `go:embed`; one service worker for push |
| Date parsing | `chrono-node` in the browser (`forwardDate: true`), wrapped in a pure `parseQuickAdd` module; the app sends the resolved timestamp |
| Toolchain | Go and Node pinned in `mise.toml`; pnpm for the frontend |
| Tests | `go test` for hub and bridge; `node --test` for frontend modules; Playwright and axe for UI |
| Hub image | Multi-stage Dockerfile: Node stage builds `web/dist`, Go stage builds the binary, final stage is a distroless static image running as non-root |
| Bridge artifact | The same binary, cross-built for linux and darwin, amd64 and arm64 |

## Why

- **Go gives one small static binary for both roles.** A local spike cross-built a binary using modernc SQLite and webpush-go for linux/amd64, linux/arm64, darwin/amd64 and darwin/arm64 with `CGO_ENABLED=0`, about 12 MB each. SQLite reported version 3.53.4 and `journal_mode=wal` when set through `_pragma=` in the DSN (local, B). Pure-Go SQLite drivers benchmark in the same range as the CGO driver (B, `stack.md`); speed is irrelevant at this scale.
- **TypeScript costs more for the bridge.** `bun build --compile` has a runtime floor of about 57 MB and was still fixing darwin-arm64 codesign kills in 1.4.1; Node single executable apps are still "Active development" and need snapshot and code cache off to cross-build (V, `stack.md`).
- **Web Push:** webpush-go and npm `web-push` are both quiet projects; neither wins on maintenance (V, `stack.md`). Go keeps one language.
- **No framework in the frontend.** The UI is one screen with a sidebar, list and detail. Vanilla TS keeps the bundle and the dependency surface small. **Inference**: a framework would earn its place only if the UI grew several more screens.
- **No existing app covers this.** The closest beads UI (beads-ui) is a developer kanban board. Every write-capable viewer shells out to `bd --json` (B, `prior-art.md`).

## The bridge's beads interface: bd CLI, polling

- **`bd serve` is out.** It refuses embedded Dolt workspaces ("bd serve requires a Dolt SQL server; this workspace uses embedded Dolt"), and embedded is the default for `bd init` (local, B; design doc, V). It first shipped in 1.3.0 with an OpenAPI version of 0.1.0 and is not called stable (V). Moving a repo to Dolt server mode is the cost of adopting it later; the bridge's `beads` interface keeps that swap local to one package.
- **The events journal is optional, not the source of truth.** It is off by default, prunes at 7 days or 100k rows with a hard `events_journal_truncated` error, does not record pulls or merges, and returns nothing (exit 0) when the journal is wiped (V, B). v1 does not use it. A later version may use it only to shorten the poll interval.
- **Change detection:** the bridge polls `bd -C <repo> list -l human --all --json` every 30 seconds, per opted-in repo. The list carries `id`, `title`, `status`, `due_at`, `defer_until`, `labels`, `metadata`, `updated_at` and `comment_count` (local, B). A comment does not bump `updated_at` but does bump `comment_count`, so the fingerprint is a hash of the listed fields. Only items whose fingerprint changed get `bd show <id> --include-comments --json`, and the item is pushed to the hub.
- **Minimum bd version: 1.3.0.** The bridge runs `bd version` on start and refuses an older one. Hub and agent machine must also agree on bd versions among themselves, since 1.3.0 migrated the schema in place.
- **Per-repo opt-in** is the line `meat-mode.sync: true` in `.beads/config.yaml`. bd tolerates unknown keys there (local, B). Repos are found by scanning configured roots for top-level directories with `.beads/`, skipping Git worktrees.

## Idempotent replay

bd has no idempotency key (B: a replayed `bd comments add` made a duplicate). The bridge keeps a local replay journal in its state directory: it writes *intent* before running bd and *done* after, then marks the action applied on the hub. On start, an intent without done is checked against bd before any retry:

| Action | bd command | Replay check |
|---|---|---|
| Done | `bd close <id> --reason "Done in Meat Mode"` | idempotent: skip if already closed |
| Reopen / Undo | `bd reopen <id>` | idempotent: skip if open |
| Snooze | `bd update <id> --defer <RFC 3339>` | idempotent: setting the same value |
| Won't do | `bd human dismiss <id>` | idempotent: skip if closed |
| Note | `bd comments add <id> --author <user>-app <text>` | skip if a comment by that author with the same text exists at or after the intent time |
| Add | `bd create --repo <repo> -l human --metadata '{"meat_mode_action":"<action id>"}' …` | skip if `bd list --all --json` shows an issue whose metadata has that action id |

The action id never goes into comment text, because agents read comments.

## Failure modes designed for

- **SQLite in containers.** The DSN sets `_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)`; mattn-style `_journal_mode=` parameters are silently ignored by modernc (C, two projects). At start the hub checks the data directory is writable and fails with a plain message if not (non-root user on a root-owned bind mount, C). The docs say the data volume must be local storage, not NFS or SMB, where WAL does not work (V).
- **Stale embedded frontend.** The Dockerfile builds `web/dist` inside the image. The static handler returns 404 for any missing path under `/assets/` and serves `index.html` only for app routes (C, helmdeck issue).
- **Push subscriptions.** A 404 or 410 from a push service deletes that subscription. The page re-sends its subscription on every open, since `pushsubscriptionchange` is unreliable. The VAPID keys are generated once into the data volume and never rotate on their own. Every push shows a notification, since Safari revokes a subscription after silent pushes (V, `prior-art.md`).
- **HTTPS.** Web Push needs a secure context with a publicly trusted certificate. A Tailscale Service name or a reverse proxy with a public CA certificate qualifies; a bare IP address does not (V/C).

## Alternatives and their costs

- **Bun or Node for both roles:** one language across server and UI, at the cost of a bridge binary of 57 MB or more, platform-specific codesign bugs and a less mature single-executable path.
- **Preact or Svelte frontend:** nicer component model, at the cost of a framework dependency and build plugin for a UI of one screen.
- **`bd serve`:** typed HTTP API and no subprocess per call, at the cost of requiring Dolt server mode in every synced repo and a contract still at 0.1.0.
- **Events journal as the change feed:** fewer reads, at the cost of per-repo config, a retention cliff and a full re-read after every pull anyway.

## Call

Proceed.

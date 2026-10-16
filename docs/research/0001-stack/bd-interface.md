# Which beads interface the bridge should use (meat-d0i.1, cluster: bd interface)

Tested against bd 1.3.1 in throwaway scratch workspaces, not the project database. Grades: A controlled/large n, B single team/vendor internal/small n, C anecdote, V vendor guidance. Source repo: https://github.com/gastownhall/beads (called "the repo" below). Local experiments are graded B (one machine, one version).

## Headline

**Inference:** the bridge should shell out to `bd ... --json` (plus `bd events tail` for change detection). `bd serve` is the nicer interface on paper, but it refuses the default storage mode, and this repo uses that mode.

## Q1. bd serve

1. **Embedded Dolt workspaces are refused, permanently.** Local run in a fresh `bd init` workspace (default embedded mode): `Error: operation "serve" not supported by the embedded-dolt backend: bd serve requires a Dolt SQL server; this workspace uses embedded Dolt`. Grade B, tested locally, answers Q1/Q4. `.beads/metadata.json` here has `"dolt_mode": "embedded"`. Same rule in the design doc: "`bd serve` refuses exactly one workspace mode, permanently: embedded Dolt" (https://github.com/gastownhall/beads/blob/main/engdocs/design/bd-serve-v0.md, read via raw copy, grep + sections, not in full). Grade V. Reason given: embedded Dolt commits outside the SQL transaction, so per-request atomicity would be false.
2. **Version.** Listed as new in v1.3.0-rc.1 ("100% net-new on upgrade... does not exist at v1.1.2"; "41 OpenAPI-specified operations across 35 paths") (https://github.com/gastownhall/beads/releases, https://newreleases.io/project/github/gastownhall/beads/release/v1.3.0-rc.1, highlights only). CHANGELOG has 1.3.0 dated 2026-09-15 (https://github.com/gastownhall/beads/blob/main/CHANGELOG.md, headings and bd serve entries read, not in full). Grade V. Local binary 1.3.1 has it.
3. **Stability.** OpenAPI `info.version` is `0.1.0` (https://github.com/gastownhall/beads/blob/main/internal/httpapi/spec/openapi.v0.yaml, first 30 lines plus targeted sections). The docs never call the wire contract "stable". They do give it a compatibility mechanism: `wire_revision` and `min_client_wire_revision` on `GET /v0/beads/context`, a `Bd-Wire-Revision` request header, additive changes with no bump, and a `capabilities` list per operation. Adjacent surfaces are labelled EXPERIMENTAL: the HTTP backend (`bd connect`, "pin an exact beads version") and the exported Go packages. Grade V. **Inference:** treat v0 as versioned but young (first release about one month old on 2026-10-16); pin bd and probe `capabilities`.
4. **Endpoints (from the spec path list, grade V):**
   - List with labels: `GET /v0/beads/issues` (keyset cursor). Rows carry labels but no comment bodies (`comments_omitted`).
   - Show with comments: `GET /v0/beads/issues/{id}?include_comments=true`. Detail also returns `revision`, the optimistic-concurrency token.
   - Close / reopen: `POST /v0/beads/issues/{id}:close` and `:reopen`. Both idempotent (`already_closed: true`), with `expected_version` guard.
   - Update including defer: `PATCH /v0/beads/issues/{id}` (19 members; I did not confirm that `defer_until` is one of them).
   - Create: `POST /v0/beads/issues` and `:batchCreate`.
   - Comment add: `POST /v0/beads/issues/{id}/comments`. The spec says "NO IDEMPOTENCY KEY EITHER: a retried request appends a SECOND comment". There is no GET for comments.
   - Also `:casMetadata` (compare-and-set metadata), `events` (poll with `since`), `events:watch` (SSE push), `config`, `batchApply`.
   - Human dismiss has no dedicated route. **Inference:** it is `:close` with a "Dismissed: ..." reason (see Q3).
5. **Auth.** Optional bearer, off by default on loopback. `--auth-token-file` (env `BEADS_SERVE_TOKEN_FILE`) enables it: one token per line, all accepted, re-read live, so rotation needs no restart. `--allow-non-loopback` requires a token file. No TLS. `actor` is caller-asserted, not authenticated. A token grants the whole surface including `issues:delete` and `issues:sweep`. Local `bd serve --help`, B; same text in release notes, V. Also `--allowed-host` for service DNS names, and an optional `Bd-Project-Id` header that rejects a mismatched workspace.
6. **Needs server mode.** The bridge would have to run `bd init --server` (external Dolt SQL server, `dolt` is installed) or migrate the existing database. I did not test server mode. Cost: a long-running Dolt process to supervise next to the agents' own bd use.

## Q2. bd events journal

All from local `bd events --help` / `tail --help` (bd 1.3.1) and CHANGELOG (https://github.com/gastownhall/beads/blob/main/CHANGELOG.md, lines for bd-opisf read), grade V, behavior checked locally, grade B.

- **Off by default.** `bd config set events-journal true` or `BD_EVENTS_JOURNAL=1`. Nothing is recorded while off. Verified: set in a scratch workspace, then records appeared.
- **Appeared:** the entry sits under the CHANGELOG `[1.2.1]` heading (2026-08-11). That release's changes were withheld from 1.2.2 and ship in 1.3.0 per the 1.3.0 upgrade notes. **Inference:** first usable in 1.3.0.
- **Cursor.** `bd events tail --since N [--limit M] [--follow] [--json]` prints JSON lines with `seq` greater than N. `seq` is an integer, gapless, strictly increasing in commit order, never reused. `bd events export` prints everything. HTTP twin: `GET /v0/beads/events?since=N` (records identical to CLI, `head` field shows when caught up).
- **Record shape:** `{seq, ts, op, issue_id, actor, issue, dep?, comment?}`, with `op` one of create, update, close, delete, dep_add, dep_remove, comment. `issue` is the full post-mutation snapshot (null on delete), so a mirror needs no re-query. Observed: create, a comment (with `source: "structured"`), and a dismiss each produced one row; the actor was the `--actor` value.
- **Retention.** Defaults 7 days and 100,000 rows (`events-journal-retain-days`, `-rows`; 0 disables a floor), pruned automatically about hourly. A `--since` below the retained floor fails with `events_journal_truncated` (`since`, `floor`, `head`) rather than silently skipping. Consumer must re-baseline from `bd list --json` on that error.
- **Caveats (V):**
  - Journal is per replica and per branch. `bd dolt pull` and merges are not journaled, so after any sync the bridge must do a full re-read.
  - Raw `bd sql` writes are not journaled.
  - A checkpoint from one clone is meaningless on another.
  - Locally, `tail --since 99` (above head) returned exit 0 with no output: no error for a stale-ahead cursor, so a bridge cannot detect a wiped journal from that alone. The docs say this stalls forever.
- **Inference:** use the journal as a wake-up and delta feed, with periodic full `bd list -l human --all --json` reconciliation as the safety net.

## Q3. bd CLI --json

Checked locally on 1.3.1, grade B.

- `bd list -l human --json`: JSON array of issues with `labels`, `comment_count`, no comment bodies. Default hides closed; `--all` includes them; default limit 50, use `-n 0` for unlimited. A deferred issue did not appear in the default list, so use `--status open,in_progress,blocked,deferred` or `--all` and filter yourself. `--skip-labels` exists (do not use).
- `bd show ID --json`: array of one. Without a flag it sets `"comments_omitted": true`. `--include-comments` streams bodies (`id, issue_id, author, text, created_at`). Includes `revision`, `defer_until`, `metadata`, `external_ref` when set.
- `bd comments ID --json`: array of comments. `bd comments add ID "text" --actor NAME --json` returns the new comment object with its UUID.
- `bd create ... -l human --json`, `bd close`, `bd reopen`, `bd update ID --defer "+1h"` (value accepted as relative time; issue became `status: deferred` with `defer_until`) all accept `--json`.
- **Close and reopen are idempotent:** a second `bd close` returned the issue with exit 0; a second `bd reopen` printed "already open", exit 0.
- **Exit codes:** unknown id gives exit 1 with JSON `{"error": ..., "schema_version": 1}` on stdout. A missing id argument gives exit 1 (JSON mode) or cobra usage error. Each object output carries `schema_version: 1` (PR https://github.com/gastownhall/beads/pull/3368, 2026-04-20, highlights only, grade B; object outputs only, arrays are bare).
- **`bd human dismiss ID "reason"`:** under the hood it is just a close. After running it: `status: closed`, `close_reason: "Dismissed: nope"`, labels unchanged (still `human`), event row `op: close` with the dismisser's `--actor`. Prints a text line, no JSON even with `--json`. `bd human list` hides closed by default. CHANGELOG: `bd human stats` identifies dismissals by the `Dismissed` close-reason prefix (#5332). **Inference:** the bridge can call `bd close ID --reason "Dismissed: ..."` or `bd human dismiss` with the same result; the latter keeps stats correct and is the supported spelling.
- **Idempotency key: none.** Two identical `bd comments add` calls produced two comments. The HTTP spec states the same for comments and batch create ("NOT IDEMPOTENT AND CARRIES NO IDEMPOTENCY KEY"). Workable tags: `--set-metadata key=value` on `bd update` (worked, appeared in `metadata`), `--external-ref`, `--actor`, and `:casMetadata` over HTTP. **Inference, design needed:** put the action-log id in the comment text (e.g. a `[mm:<uuid>]` prefix) and before replaying a comment, read `bd comments ID --json` and skip if present; for state changes rely on close/reopen idempotence and on `bd show` status checks; for defer, set the same `defer_until` (a repeat write is harmless). For create, store the action id in `--external-ref` and look it up with `bd list --json` filtered on it before creating.

## Q4. Config and locating the database

- **Arbitrary keys in `.beads/config.yaml`: tolerated.** I appended `meat-mode.sync: true` and a nested `meat-mode:` map to the scratch config: `bd list --json` ran with no warning and `bd config get meat-mode.sync` returned `true`. **Gotcha:** `bd config set meat-mode.sync false` warned `"meat-mode.sync" is not a recognized config key. Use 'custom.*' for user-defined keys` and stored it, without editing the YAML. The supported namespace is `custom.*` (`bd config set custom.meatmode true` worked, grade B). **Caveat:** my first test appended to a file without a trailing newline and produced invalid YAML; bd then printed three warnings per command but kept working. Recommend `custom.*` keys or the bridge's own env/config file; do not make bd's YAML the bridge's config store. Grade B.
- **Locating the database.** bd discovers `.beads/` upward from the cwd. `bd -C DIR ...` (like `git -C`) worked from `/`. `--db PATH` is documented as the database path override; from `/tmp` with no flags `bd list` fails with `Error: no beads database found` and the hint to set `BEADS_DIR`. **Inference:** the bridge should run bd with `-C <repo>` or `BEADS_DIR=<repo>/.beads`; I confirmed `-C` only.
- **Storage modes (V, https://beads.gascity.com/architecture/dolt, highlights only):** embedded Dolt is the default for new installs ("single-writer, one process at a time"), server mode is for concurrent agents. This repo's `metadata.json` says embedded (`dolt_mode`, `dolt_database: meat`).
- `bd --readonly` blocks writes (useful for the read path). `--actor NAME` / `BEADS_ACTOR` sets the audit identity, which is how the bridge tags its own actions.

## Q5. Known issues

- **Concurrency, embedded mode, history (all V/C, highlights only):**
  - v1.0.0 added an exclusive non-blocking flock (GH#2571); 5 concurrent `bd list` meant 4 failures (Beadbox blog, 2026-04-03, https://beadbox.app/en/blog/beads-v1-the-hard-way, grade C).
  - PR #3005 (2026-04-03) changed it to queue with backoff (https://github.com/gastownhall/beads/pull/3005).
  - A later commit (2026-05-02, PR #3614) removed the flock entirely: "Concurrent `bd` processes now open the embedded engine independently... the embedded Dolt engine handles its own concurrency internally" (https://github.com/gastownhall/beads/commit/6c938415cb04a3d78c39a0d9c7009e2bb1ee8797). It also breaks the Go `OpenBestAvailable` signature.
  - The current docs still say embedded is "Single-writer (one process at a time)" and name "database is locked" errors as a symptom (https://beads.gascity.com/architecture/dolt). Docs and code disagree (the blog warns that docs describe intent).
- **Local test (B):** 6 parallel `bd comments add` processes against one embedded workspace all exited 0 and all 6 comments landed (8 total with earlier ones). No lock errors seen. Small n, one run; not proof under heavy agent load.
- **Inference:** the bridge should serialize its own bd calls (single worker, queue) and retry on nonzero exit with backoff, since agents' bd calls will interleave anyway. Retry is safe for close, reopen, defer, but not for comments or create without the dedup check from Q3.
- **Schema migration on upgrade:** the 1.3.0 upgrade notes say the first command on an embedded store migrates the schema "in place, from v53 to v66" and an older bd then cannot read it. Multi-clone setups need one designated migrator (#4259, same file). **Risk for the bridge:** bridge host and agent host must run compatible bd versions if they share a Dolt remote.
- **JSON shape changes:** `schema_version` field added to object outputs, array shape kept (PR #3368); planned future envelope `{schema_version, data}` behind `BD_JSON_ENVELOPE=1`, default at v2.0 (same PR thread, grade C, proposal only). Parse defensively (ignore unknown fields, accept `items`/bare array).
- **Journal and sync:** pulls and merges are not journaled; after `bd dolt pull` the bridge must re-baseline (Q2).
- **Searched, found nothing:** no GitHub issue found specifically on bd serve lock contention (server mode uses SQL server arbitration, "claims are arbitrated in the SQL server" per `bd serve --help`). I did not search the issue tracker exhaustively.

## Recommendation (Inference)

1. Bridge calls the CLI with `-C <repo>`, `--json`, `--actor meat-mode`, serialized through one worker, bd version pinned and checked at startup (`bd version`, `bd events tail --help` or `bd config get events-journal`).
2. Read path: enable `events-journal`, poll `bd events tail --since N --json`, persist N only after the hub acknowledges; on `events_journal_truncated`, on cursor reset, and after any `bd dolt pull`, do a full `bd list --all --json` reconcile. Fetch comments only for changed items via `bd show --include-comments --json`.
3. Write path: each hub action carries an id; embed it in comment text or `--set-metadata`/`--external-ref`, check before replaying.
4. Revisit `bd serve` only if the operator moves to Dolt server mode; the cost is a supervised Dolt server and a young v0 contract, the gain is typed errors, `revision` guards on close/reopen, and `events:watch` push.

## Gaps

- bd serve and server mode were not run (embedded workspace refuses); the endpoint list is from the spec, not from calls.
- Not confirmed that `PATCH` accepts `defer_until`, or the exact request body of `:close` for a reason.
- `--db` flag semantics and `BEADS_DIR` not exercised.
- No heavy-concurrency or real agent-contention test; no exhaustive issue-tracker search.

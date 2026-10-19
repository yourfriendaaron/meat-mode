# OpenCode (anomalyco/opencode, formerly sst/opencode)

Research date 2026-10-19. Version read: v1.19.3 (released 2026-10-16 per GitHub releases API; the docs install page also mentions an "OpenCode 2" that can conflict with a v1 install, and `dev.opencode.ai/v2/docs` exists, so expect a v1/v2 split). Not installed or run; all of this is from docs and source.

Grades: A controlled/large n, B single team/small n, C anecdote, V vendor guidance. "Full" = page read in full, "HL" = highlights only. Docs pages below were fetched through a summarizing fetcher, so quotes marked "paraphrase" are not verbatim.

## Q1 Non-interactive command and event stream

**Command** (V, full): `opencode run --format json [--auto] [-m provider/model] [--dir <path>] [-s <id> | -c] "<prompt>"`
- Source: https://opencode.ai/docs/cli/ (no date) and `packages/opencode/src/cli/cmd/run.ts` at v1.19.3 (raw GitHub, read via fetcher plus search highlights).
- Flags in the yargs builder: `--command`, `--continue/-c`, `--session/-s`, `--fork`, `--share`, `--model/-m provider/model`, `--agent`, `--format default|json`, `--file/-f`, `--title`, `--attach <url>`, `--password/-p`, `--username/-u`, `--dir`, `--port`, `--variant`, `--thinking`, `--auto` ("auto-approve permissions that are not explicitly denied (dangerous!)"), plus hidden `--yolo` and `--dangerously-skip-permissions` which behave as `--auto`. Answers Q1, Q4. Grade V/A (source).
- Prompt is positional (`message..`). If stdin is not a TTY it is read whole (`Bun.stdin.text()`) and appended to the message after a newline. So stdin is a one-shot prompt source, not a stream. Answers Q1, Q3. Grade A (source, v1.19.3).

**Stream shape** (A, source v1.19.3): one JSON object per line on stdout, written by `emit(type, data)` as `{type, timestamp (ms), sessionID, ...data}`. The CLI forwards only these types:
- `step_start` (part.type "step-start", has `snapshot`)
- `text` (only when `part.time.end` is set, so whole text blocks, not deltas)
- `reasoning` (only with `--thinking`)
- `tool_use` (emitted only when the tool status is `completed` or `error`; there is no "tool started" line)
- `step_finish` (`reason` "stop" = final, "tool-calls" = more steps follow)
- `error` (`{error: <session.error props.error>}`)
There is no session-start line and no final "result" line. The turn ends when the process exits; the loop breaks on `session.status` type `idle`. **Inference**: the bridge must treat `step_finish` with `reason == "stop"` plus process exit as the result, and sum `part.cost` over all `step_finish` lines for total cost.

**Real examples** (B, Untether cheatsheet by Nathan Schram, https://littlebearapps.com/help/untether/opencode-stream-json-cheatsheet/ , 2026-02-24, HL; same text on takopi.dev; a third parser fixture from heggria/taskflow was captured on opencode 1.17):
```
{"type":"tool_use","timestamp":1767036061199,"sessionID":"ses_494719016ffe85dkDMj0FPRbHK","part":{"id":"prt_b6b8...","type":"tool","callID":"r9bQWsNLvOrJGIOz","tool":"bash","state":{"status":"completed","input":{"command":"echo hello","description":"Print hello to stdout"},"output":"hello\n","title":"Print hello to stdout","metadata":{"output":"hello\n","exit":0},"time":{"start":1767036061123,"end":1767036061173}}}}
{"type":"text","timestamp":1767036064268,"sessionID":"ses_4947...","part":{"type":"text","text":"```\nhello\n```","time":{"start":1767036064265,"end":1767036064265}}}
{"type":"step_finish","timestamp":1767036064273,"sessionID":"ses_4947...","part":{"type":"step-finish","reason":"stop","snapshot":"09dd05d1...","cost":0.001,"tokens":{"input":671,"output":8,"reasoning":0,"cache":{"read":21415,"write":0}}}}
```
- Tool name is `part.tool` ("bash", "read", "write", "edit", "grep"...); input is `part.state.input` (for bash `{command, description}`); rejected calls arrive as `tool_use` with `state.status == "error"` and the text in `state.error`. Answers Q1. Grade B.
- Cost is per step in `part.cost` (USD), tokens in `part.tokens` (`input, output, reasoning, cache.read, cache.write`, newer builds also `total`). Cost is 0 for free or unpriced models (taskflow fixture shows `cost:0` on a real run). Answers Q1, Q4. Grade B.
- Permission, question and compaction events never reach stdout (Untether cheatsheet, B). A PR to emit `permission_asked` in JSON mode (#39460, opened 2026-07-29) was closed by the cleanup bot, so not merged. Grade B.

**Rate limits**: no rate-limit event exists in the stream. Provider errors arrive as an `error` line. Searched; found nothing.

**Exit codes** (A, source v1.19.3): `process.exitCode = 1` when a `session.error` was seen (local mode), when `session.prompt` returns an error, on a thrown loop error, and `process.exit(1)` on validation failures ("Session not found", "--fork requires --continue or --session"). Otherwise 0. In `--attach` mode `finish()` returns early and skips the session-error exit code (per the fetcher's reading of the source; **Inference**: verify before relying on exit codes in attach mode). No other codes documented.

## Q2 Sessions

- Session id is in every JSON line as `sessionID` (e.g. `ses_494719016ffe85dkDMj0FPRbHK`), including the first `step_start`. Grade A (source) / B (examples).
- Resume: `opencode run --format json -s ses_xxx "next prompt"`. `--continue/-c` picks the first session in `session.list()` with no `parentID` (the most recent root session in any project it sees), so use `-s` to be exact. `--fork` with either flag branches a new session. A missing id prints "Session not found" and exits 1. Source: run.ts v1.19.3. Grade A.
- Storage: SQLite `~/.local/share/opencode/opencode.db` (WAL), overridable with `XDG_DATA_HOME` or `OPENCODE_DB`; legacy JSON tree under `storage/`; credentials in `auth.json` (0600), and in v2 in the same DB. Sources: https://seanthegeek.net/doubleagent/collectors/research/opencode.html (C, source-reading blog, no date, HL), https://opencode.ai/docs/troubleshooting/ (V, HL), https://dev.opencode.ai/v2/docs/cli/providers/ (V, HL). No per-project DB; sessions are in the home DB. `opencode session list --format json` lists them. Grade B/V.

## Q3 Follow-up into a live run

- `opencode run` reads stdin only once, at start. No streaming input format. Grade A.
- A server exists: `opencode serve` (default 127.0.0.1:4096, `OPENCODE_SERVER_PASSWORD` enables basic auth, user `opencode`). https://opencode.ai/docs/server/ (V, fetched via summarizer, HL-equivalent). Endpoints: `POST /session` (create), `POST /session/:id/message` (send and wait), `POST /session/:id/prompt_async` (204, no wait), `POST /session/:id/abort`, `GET /event` (SSE, first event `server.connected`), `GET /session/status`, `POST /session/:id/permissions/:permissionID` (reply), `GET /session/:id/message`, `GET /doc` (OpenAPI 3.1). `opencode run --attach http://host:4096` drives a session on a running server and reuses a warm MCP boot. Grade V.
- **Mid-run message behavior is unreliable.** Issue #46842 "prompt_async on a busy session persists the message but never schedules a turn" (open, 2026-09-02, v1.18.x): "async prompts sent during long agentic turns are persisted ... but never answered, unless the running loop happens to re-read messages between tool rounds and absorbs them." Issue #41753 (2026-08, v1.18.29): a new prompt waits for a running bash tool to finish; `Runner.ensureRunning` joins the current run. Issue #16102 (feature request): messages queued while busy either interrupt or wait until idle; no mid-task injection. Grade B/C (open issues, source-reading by reporters).
- **Inference**: the safe design is queue the note, and after the turn ends (`session.status` idle or process exit) resume with `-s <id>`. If the harness runs through `serve`, a note posted during the run may or may not be seen by the next tool round; do not promise live delivery. Posting then calling `abort` and re-prompting is the only way to force it.

## Q4 Limits and permissions

- **Spending cap: none found.** Docs agents page (V, fetched): the only cost control is `steps` ("maximum number of agentic iterations an agent can perform before being forced to respond with text only"; unset means no limit; `maxSteps` deprecated). Config page lists only provider `timeout`, `headerTimeout`, `chunkTimeout`. **Inference**: the bridge must enforce the cap itself by summing `step_finish.part.cost` and killing the process; cost is 0 for unpriced or custom models, so the cap can silently not fire. No wall-clock timeout flag either; the bridge kills the process.
- Permissions (V, https://opencode.ai/docs/permissions/, fetched): config key `permission` with keys `read, edit, glob, grep, bash, task, skill, lsp, question, webfetch, websearch, external_directory, doom_loop`; values `allow|ask|deny`; `"*"` catch-all; object of patterns, last match wins. Defaults: most `allow`, `doom_loop` and `external_directory` `ask`, `.env` reads denied except `*.env.example`. Setting `"permission": "allow"` allows all. No env var for permissions is documented.
- Closest to "edit and run in this repo without asking": `opencode run --auto` (approves every `permission.asked` with reply `once` unless explicitly denied; source A), or config `{"permission":{"*":"allow","external_directory":"deny","doom_loop":"deny"}}` which also makes asks impossible. **Inference**: use both, so that no path can reach `ask`. There is no OS sandbox; permissions are policy only.
- Non-interactive runs deny `question`, `plan_enter`, `plan_exit` at session creation (run.ts). Without `--auto`, any ask is auto-rejected with a printed warning (source A).
- Tool names (V, https://opencode.ai/docs/tools/): `bash`, `edit`, `write`, `read`, `grep`, `glob`, `apply_patch`, `skill`, `webfetch`, `websearch` (needs OpenCode provider or `OPENCODE_ENABLE_EXA`/`_PARALLEL`), `question`, experimental `lsp`. Tool names in the JSON stream use these ids (lowercase). **Inference**: the activity line can map `bash`->"Running <command>", `edit/write/read/apply_patch`->"Editing/Reading <input.filePath>". Input key names for edit/read (`filePath`) were not verified in a sample; check in a real run.

## Q5 Environment and install

- Instruction files (V, https://opencode.ai/docs/rules/, fetched): project `AGENTS.md` searched upward from cwd, `CLAUDE.md` as fallback; global `~/.config/opencode/AGENTS.md`, fallback `~/.claude/CLAUDE.md`. `OPENCODE_DISABLE_CLAUDE_CODE=1` turns off all `.claude` reads. `instructions` config key adds files and globs. Source also reads deprecated `CONTEXT.md` (blog, C).
- Env set for child processes: a 2026-01 PR (#9289, closed, not merged per listing) proposed `OPENCODE_SESSION_ID` and `OPENCODE_SESSION_TITLE` for the bash tool. Merge status unconfirmed, so do not rely on them. Current bash tool spawns with `extendEnv: true, TERM: "dumb", stdin: "ignore"` plus plugin `shell.env` values (source HL, grade B). I did not find a documented "I am inside opencode" marker such as `OPENCODE=1`; searched, found nothing. **Inference**: nested-run detection is not possible by env alone.
- Env a child should not inherit: provider keys it reads (any `*_API_KEY`), `OPENCODE_SERVER_PASSWORD`/`_USERNAME`, `OPENCODE_CONFIG`, `OPENCODE_CONFIG_CONTENT`, `OPENCODE_CONFIG_DIR`, `OPENCODE_DB`, `OPENCODE_AUTH_CONTENT`, `OPENCODE_DISABLE_*`. **Inference**: strip `OPENCODE_*` from the bridge's own env before spawning another harness, and pass an explicit `OPENCODE_CONFIG_CONTENT` for the permission policy when running opencode.
- Config precedence: global, `OPENCODE_CONFIG`, project `opencode.json`, `.opencode/`, `OPENCODE_CONFIG_CONTENT`, managed (V, config page).
- Install (V, https://opencode.ai/docs/): `curl -fsSL https://opencode.ai/install | bash`, `npm install -g opencode-ai`, `brew install anomalyco/tap/opencode`. Sign in: `opencode auth login` (stored in `~/.local/share/opencode/auth.json`), or provider env vars (read from the environment or a project `.env`), or `OPENCODE_AUTH_CONTENT`.
- Popularity (A, GitHub API 2026-10-19): 217,490 stars, MIT, 4,630 open issues, last push 2026-10-19. Latest release v1.19.3, 2026-10-16.

## Q6 Known problems in headless use

All B (single GitHub issues, titles and dates as shown in search highlights):
- Hang on any permission that resolves to `ask`: #36762, created 2026-07-13 on 1.17.18, closed 2026-08-22 "fixed in latest". Three repro paths: unlisted `external_directory`, piped bash commands with mixed verdicts, agent-level bash maps not honored. Follow-up #54477 reports the same hang in the GitHub Action. **Inference**: still treat any `ask` as a hang risk; the bridge needs its own idle timeout.
- Hang on the `question` tool: #11899 (2026-02-03, 1.1.49, fixed 2026-03-26), regressed for sessions created via HTTP and then resumed: #44556 (2026-08-24, 1.18.21, open, fix PR #44559 open). `--auto` does not handle `question.asked`. Relevant if the bridge ever creates sessions over `serve`.
- Earlier: #3503 (2025-10-28, v0.15.18) `"bash":"ask"` hangs in `opencode run`; #3213 `run` and TUI exits hang on v0.15+.
- Mid-run message loss and bash blocking: #46842, #41753 (see Q3).
- Output format drift: the JSON lines have no schema version. Observed differences: `tokens.total` appears in 1.17 captures but not in the Feb 2026 cheatsheet; `reason` is documented as optional. The taskflow parser fixture claims it "catches schema drift". **Inference**: parse defensively and ignore unknown types and fields.
- A Feb 2026 harness-wrapper doc (harness.lol) shows an older, simplified example; use the source-derived shape above.
- Resume bugs: no `run -s` data-loss issue found in these searches beyond the question-tool one. Searched once; not exhaustive.

## Gaps
- No sample of `edit`/`write`/`read` tool input keys or of an `error` line was captured.
- Whether `OPENCODE_SESSION_ID` shipped, and whether `serve` queues mid-run messages in v1.19.3 (issue #46842 may be fixed since 2026-09-02).
- v1 versus v2 CLI differences (`opencode --standalone`, `opencode service`) were seen in v2 docs only; `run` flags may change in v2.

# Gemini CLI as a headless harness (meat-nv6.3)

Version read: v0.64.0 (stable, published 2026-10-13T20:44:46Z, via `gh api .../releases`). Docs and source read from the `main` branch on 2026-10-19 (raw file fetches of `docs/cli/headless.md`, `docs/cli/cli-reference.md`, `docs/cli/session-management.md`, `docs/cli/acp-mode.md`, `docs/reference/tools.md`, `docs/reference/configuration.md`, `packages/core/src/output/types.ts`, `packages/cli/src/config/config.ts`, `packages/core/src/services/shellExecutionService.ts`). All "read in full" unless noted. Grade V = vendor docs, A for source code facts and API numbers. Repo: https://github.com/google-gemini/gemini-cli

## Q1 Command line, events, exit codes

Command (A, source config.ts L286-345; V, cli-reference.md):

    gemini -p "<prompt>" --output-format stream-json --approval-mode yolo

- `-p/--prompt` forces non-interactive mode and is appended to stdin text if stdin is piped. A bare positional query now defaults to interactive in a TTY (config.ts L542-546), so always pass `-p`.
- `-o/--output-format`: `text` (default), `json`, `stream-json`.
- `--yolo` and `--approval-mode` together is a hard error (config.ts L258).
- No `--input-format` flag exists. I grepped config.ts and the CLI reference: nothing. (Searched, found nothing.)

`stream-json` event types (A, https://raw.githubusercontent.com/google-gemini/gemini-cli/main/packages/core/src/output/types.ts, enum `JsonStreamEventType`): `init`, `message`, `tool_use`, `tool_result`, `error`, `result`. Every event has `type` and ISO `timestamp`. Fields:
- `init`: `session_id`, `model`.
- `message`: `role` (`user`|`assistant`), `content`, optional `delta: true` (assistant chunks stream as deltas).
- `tool_use`: `tool_name`, `tool_id`, `parameters` (object).
- `tool_result`: `tool_id`, `status` (`success`|`error`), optional `output`, optional `error{type,message}`.
- `error`: `severity` (`warning`|`error`), `message`.
- `result`: `status` (`success`|`error`), optional `error{type,message}`, optional `stats`: `total_tokens`, `input_tokens`, `output_tokens`, `cached`, `input`, `duration_ms`, `tool_calls`, `models{name:{total_tokens,input_tokens,output_tokens,cached,input}}`.

Example from the PR that added it (B, https://github.com/google-gemini/gemini-cli/pull/10883, created 2025-10-10, merged 2025-10-15, "highlights only" on the PR body; shape matches types.ts):

    {"type":"init","timestamp":"...","session_id":"abc123","model":"gemini-2.0-flash-exp"}
    {"type":"tool_use","tool_name":"Bash","tool_id":"bash-123","parameters":{"command":"ls"},"timestamp":"..."}
    {"type":"tool_result","tool_id":"bash-123","status":"success","output":"file1.txt\nfile2.txt","timestamp":"..."}
    {"type":"message","role":"assistant","content":"Here are the files...","delta":true,"timestamp":"..."}
    {"type":"result","status":"success","stats":{"total_tokens":100,"input_tokens":50,"output_tokens":50,"duration_ms":1200,"tool_calls":1},"timestamp":"..."}

Caveat: the example says tool name `Bash`; the current tool is `run_shell_command` (see Q4). Trust the tools table, not the PR example.

`--output-format json` (V headless.md; A types.ts `JsonOutput`): one object at the end: `session_id`, `response`, `stats` (SessionMetrics), `error{type,message,code}`, `warnings[]`.

Facts that matter to the bridge:
- **No cost field anywhere.** `result.stats` is tokens only. No `total_cost_usd`. **Inference**: the bridge must compute cost from tokens and a price table, or not offer a spending cap for Gemini.
- **No rate-limit event type.** Quota and 429 problems surface as `error` events or a failed `result`. **Inference**.
- The `result` line carries no `session_id`; read it from `init`.
- Exit codes (V, https://github.com/google-gemini/gemini-cli/blob/main/docs/cli/headless.md): 0 success, 1 general error or API failure, 42 input error, 53 turn limit exceeded.

## Q2 Sessions

- `session_id` is in the `init` event (stream-json) and in the `json` output (A, types.ts).
- Resume: `gemini --resume <uuid|latest|index> -p "follow up" --output-format stream-json`. Short `-r`. The CLI reference lists "Resume by ID: `gemini -r "abc123" "query"`", `latest`, or an index number (V, cli-reference.md and session-management.md).
- Storage: `~/.gemini/tmp/<project_hash>/chats/`, hash from the project root, so sessions are per project directory and resume only finds them when run from the same repo (V, session-management.md). `--list-sessions`, `--delete-session <index>` exist. Resume by index or `latest` is unsafe for a bridge; use the UUID from `init`. **Inference**.
- Resume bug history: Issue #14180 (created 2025-11-30, closed 2025-12-04, B): on 0.18.4 stdin and positional prompt did not work with `--resume`, only `-p`; reporter says fixed in 0.20.0-nightly. Using `-p` with `--resume` avoids it either way. Read in full via search highlights.

## Q3 Follow-up into a live run

- No streaming input format, no stdin message loop in headless mode. The prompt is read once (stdin text is concatenated with `-p`). **Inference from source and flags**; nothing found to the contrary.
- Closest thing in headless: nonInteractiveCli.ts sets up a stdin cancellation listener (L133-220, A, "highlights only", grep lines); it cancels the run, it does not inject a message. 
- Options: (a) queue the note and `--resume <id> -p` after the turn ends (works today); (b) ACP mode below, which has a `prompt` and a `cancel` method over JSON-RPC stdio.
- ACP (V, https://github.com/google-gemini/gemini-cli/blob/main/docs/cli/acp-mode.md): start with `gemini --acp` (`--experimental-acp` is still listed in the CLI reference as experimental, so both exist; config.ts L363-367 defines both). Methods: `initialize`, `authenticate`, `newSession`, `loadSession`, `prompt`, `cancel`, `setSessionMode` (approval level, e.g. auto-approve), `unstable_setSessionModel`. File reads and writes can be proxied through the client. Docs do not say whether `prompt` can be sent while a turn is running; ACP's own spec treats it as one prompt at a time per session (not verified here, see pi.md for the ACP spec). ACP gives structured tool-call updates and permission requests instead of the `stream-json` lines, so it is a different parser.

## Q4 Limits and permissions

- Spending cap: none found in flags or docs. Searched cli-reference.md and configuration.md. (Inference: enforce by token accounting from `result.stats`, which arrives only at the end, so a mid-run cap needs the bridge to kill the process. Per-event usage is not streamed.)
- Turn cap: setting `model.maxSessionTurns` (number, V configuration.md L544; settings.json), exceeding it ends with exit code 53 (V headless.md; source nonInteractiveCli.ts L319-381 emits a MaxSessionTurns event). No CLI flag found for it. **Inference**: set via a project or user settings file, or `GEMINI_CLI_HOME` pointing at a bridge-owned config dir.
- Timeout: none built in. Bridge must enforce wall-clock and send SIGTERM; see Q6 for ignored signals.
- Approvals: `--approval-mode default|auto_edit|yolo|plan` (V cli-reference.md; config.ts L343-345: default prompts, auto_edit auto-approves edit tools, yolo auto-approves all, plan is read-only). `--yolo/-y` is deprecated. **Inference**: closest to "edit and run commands without asking" is `--approval-mode yolo`; `auto_edit` still blocks shell, which in headless means the command is refused since nobody can answer. `--allowed-tools` is deprecated in favor of the Policy Engine (https://github.com/google-gemini/gemini-cli/blob/main/docs/reference/policy-engine.md, not read).
- Sandbox: `-s` or `GEMINI_SANDBOX=true|docker|podman|sandbox-exec|runsc|lxc` (V sandbox.md). macOS Seatbelt default profile `permissive-open`; container default image `ghcr.io/google/gemini-cli:latest`.
- Folder trust: `--skip-trust` or `GEMINI_CLI_TRUST_WORKSPACE` (V cli-reference.md, configuration.md L2683). **Inference**: an untrusted repo may restrict tools or settings in headless; set one of these for unattended runs. Not tested.
- Tool names (V, https://github.com/google-gemini/gemini-cli/blob/main/docs/reference/tools.md): shell `run_shell_command` (params `command`, `description`, `dir_path`, `is_background`); edit `replace` (`file_path`, `old_string`, `new_string`, `instruction`, `allow_multiple`); write `write_file` (`file_path`, `content`); read `read_file` (`file_path`, `start_line`, `end_line`), `read_many_files`, `list_directory` (`dir_path`); search `glob` (`pattern`), `grep_search` (`pattern`, legacy alias `search_file_content`); web `google_web_search` (`query`), `web_fetch` (`prompt`); also `ask_user` (needs a human; unattended it cannot be answered), `activate_skill`. Activity line: map `parameters.command` and `parameters.file_path` per tool name.

## Q5 Environment, instructions, install

- Sets `GEMINI_CLI=1` in shell commands it runs (A, shellExecutionService.ts L59-64, L584-585), plus `PAGER=cat` and `GIT_PAGER=cat` (L587-588). A nested run can detect `GEMINI_CLI`. Not documented in the docs I read (grep of configuration.md found no `GEMINI_CLI=`).
- Env sanitization for child shells redacts vars whose name contains TOKEN, SECRET, PASSWORD, KEY, AUTH, CREDENTIAL, PRIVATE, CERT, and secret-looking values; allowlists `PATH`, `HOME`, `USER`, `SHELL`, `TERM`, `LANG` and anything starting `GEMINI_CLI_` (V configuration.md L2850-2865). Other `GEMINI_CLI_*` that affect behavior: `GEMINI_CLI_HOME`, `GEMINI_CLI_SURFACE`, `GEMINI_CLI_TRUST_WORKSPACE`, `GEMINI_CLI_IDE_PID`, `GEMINI_CLI_SYSTEM_SETTINGS_PATH`, `GEMINI_CLI_SYSTEM_DEFAULTS_PATH`. **Inference**: when the bridge launches gemini, strip `GEMINI_CLI`, `GEMINI_CLI_*` and `GEMINI_SANDBOX` the way it strips `CLAUDE_CODE_*`; keep `GEMINI_API_KEY` or `GOOGLE_API_KEY` (auth).
- Auth: `GEMINI_API_KEY` or `GOOGLE_API_KEY` env, or Google login (OAuth cached under `~/.gemini`), or Vertex (V configuration.md L2672-2716).
- Instruction file: `GEMINI.md`, hierarchy of `~/.gemini/GEMINI.md` then workspace files (V gemini-md.md). AGENTS.md is not the default name; `context.fileName` setting can add it (not verified here).
- Install: `npm install -g @google/gemini-cli`, or `npx`, brew. (Standard install, not re-verified this pass; on this machine use `mise use -g npm:@google/gemini-cli`.)
- Popularity (A, `gh api repos/google-gemini/gemini-cli` on 2026-10-19): 107,457 stars, last push 2026-10-19. Latest stable v0.64.0, 2026-10-13. Still 0.x, so flags and formats can move.

## Q6 Known problems in headless use

- #6715 "hangs indefinitely when executed from Node.js child_process" (created 2025-08-21, closed 2025-08-22, B): 0.1.22 always did `readStdin()` when stdin was not a TTY, waiting for EOF the parent never sent. Fixed then, but the lesson holds: **close or end the child's stdin** (or pass `/dev/null`). Highlights only.
- #29434 "Headless -p with piped stdin hangs at 100% CPU... `@scope/pkg`" (created 2026-09-21, closed, B): on v0.59.0 and v0.60.0, piped text containing `@` tokens is parsed as @-file commands, a regex swallows text and a glob stalls in minimatch; SIGINT and SIGTERM ignored, only SIGKILL works; emits only `init` then nothing. Workaround: escape `@` as `\@` in piped text. Highlights only. **Inference**: send the prompt via `-p`, escape or avoid `@`, and plan to SIGKILL after a grace period. Does the same apply to `-p` text alone? The issue says "stdin + prompt string" goes through the same handler.
- #16007 "freezes in --yolo mode, unkillable with SIGTERM" (created 2026-01-06, closed not_planned 2026-04-07, C): single reporter on 0.22.5, no repro. Treat as a reason to use SIGKILL fallback.
- #12362 "Headless Mode Hangs During Execution" (2025-10-31, C): reports include a JS heap out of memory in YOLO mode with large stdin. Highlights only.
- #29747 (open, created 2026-10-14, title only): "Body timeout is fixed at 300 s for API-key auth; a long silent tool call is dropped and replayed". Possible duplicate tool runs on long commands. Title only, not read.
- Format stability: stream-json landed 2025-10-15 (PR #10883). `--yolo` is now deprecated for `--approval-mode`, `--experimental-acp` partly superseded by `--acp`, `search_file_content` renamed `grep_search`. **Inference**: pin a version and parse leniently (ignore unknown event types and fields).
- Search for "headless resume" issues via `gh search issues` returned nothing relevant beyond #14180.

## Gaps
- Not run, per brief: the real `init`/`result` lines of v0.64.0 are unseen; the examples are from the source types and the PR.
- Behavior of a tool that needs approval under `default` mode in headless (refuse vs hang) not confirmed; use yolo.
- Whether `--resume` in v0.64.0 requires the same cwd was inferred from per-project storage, not tested.
- Policy Engine docs, AGENTS.md via `context.fileName`, and the ACP concurrent-prompt rule not read.

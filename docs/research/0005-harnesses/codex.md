# Codex CLI (openai/codex) for headless use

Read on 2026-10-19. Latest release at read time: `rust-v0.163.0`, published 2026-10-15 (GitHub API, grade V). Repo stars: 129,919 (GitHub API, same day). Source read from `main` (a few commits after 0.163.0), through `gh api` raw contents. "Source" below means `codex-rs/` files at that commit.

## Q1 Command line, events, exit codes

Command (source `codex-rs/exec/src/cli.rs`, `utils/cli/src/shared_options.rs`; read in full; grade V, source of truth):

```
codex exec --json -C <repo> --sandbox workspace-write [-m <model>] [--skip-git-repo-check] "<prompt>"
```

- Usage line in source: `codex exec [OPTIONS] [PROMPT]` and `codex exec [OPTIONS] <COMMAND> [ARGS]`.
- Flags found: `--json` (alias `--experimental-json`), `-o/--output-last-message <FILE>`, `--output-schema <FILE>`, `-C/--cd <DIR>`, `-m/--model`, `-p/--profile`, `-s/--sandbox`, `--approve-for-me`, `--dangerously-bypass-approvals-and-sandbox` (alias `--yolo`), `--add-dir`, `--ephemeral`, `--ignore-user-config`, `--ignore-rules`, `--skip-git-repo-check`, `-i/--image`, `--oss`, `--worktree`, `--strict-config`.
- Docs page (https://learn.chatgpt.com/docs/non-interactive-mode, redirected from developers.openai.com/codex/noninteractive; undated; highlights via fetch summary; grade V): "Progress streams to stderr. Only the final agent message goes to stdout." With `--json`, stdout is JSON Lines, one event per line.
- Config overrides use `-c key=value` (TOML), shared with the interactive CLI. **Inference** from the `CliConfigOverrides` type; I did not read its flag spelling.

Event types (source `exec/src/exec_events.rs`, read in full; grade V). Top-level `type`:

| type | payload |
|---|---|
| `thread.started` | `thread_id` (first event) |
| `turn.started` | empty |
| `turn.completed` | `usage` {input_tokens, cached_input_tokens, cache_write_input_tokens, output_tokens, reasoning_output_tokens} |
| `turn.failed` | `error.message` |
| `item.started` / `item.updated` / `item.completed` | `item` {id, type, ...} |
| `error` | `message` (unrecoverable stream error) |

Item `type` values (snake_case): `agent_message` {text}, `reasoning` {text}, `command_execution` {command, aggregated_output, exit_code, status in_progress/completed/failed/declined}, `file_change` {changes:[{path, kind add/delete/update}], status}, `mcp_tool_call` {server, tool, arguments, result, error, status}, `collab_tool_call` (spawn_agent, send_input, wait, close_agent), `web_search` {id, query, ...}, `todo_list`, `error` (non-fatal).

Real sample from the docs page (grade V, highlights only):

```
{"type":"thread.started","thread_id":"0199a213-81c0-7800-8aa1-bbab2a035a53"}
{"type":"turn.started"}
{"type":"item.started","item":{"id":"item_1","type":"command_execution","command":"bash -lc ls","status":"in_progress"}}
{"type":"item.completed","item":{"id":"item_3","type":"agent_message","text":"Repo contains docs, sdk, and examples directories."}}
{"type":"turn.completed","usage":{"input_tokens":24763,"cached_input_tokens":24448,"output_tokens":122,"reasoning_output_tokens":0}}
```

Meaning for an activity line (**Inference**): use `item.started` with `item.type == command_execution` (show `command`) and `item.completed` with `file_change` (show `changes[].path` and `kind`; file changes are emitted only once complete, never as started). Web search and MCP calls start as `item.started`. There is no per-tool name for shell or edit; the item type is the tool.

Cost: **no cost field and no dollar figure anywhere** in the stream (grep of exec_events.rs: only token counts). No rate-limit event in the exec JSONL. Cost must be computed by the bridge from tokens and a price table. **Inference.**

Exit codes (source `exec/src/lib.rs`, grepped, not read in full; grade V): `std::process::exit(1)` on setup failures (not in a git repo without `--skip-git-repo-check`, unreadable or empty stdin prompt, config load failure) and after the run when `error_seen` is true. Otherwise 0. The docs page documents no numeric codes beyond "exits with an error if an enabled MCP server marked required = true fails to initialize". **Inference:** a failed turn gives exit 1 plus a `turn.failed` or `error` line; no other distinct codes.

Git check: by default `codex exec` refuses to run outside a git repo ("Not inside a trusted directory and --skip-git-repo-check was not specified."), exit 1. Skipped when `--yolo` is set.

## Q2 Sessions

- Session id is `thread_id` in the first `thread.started` line. Same id the docs call SESSION_ID (grade V).
- Resume (source cli.rs `ResumeArgs`; docs page): `codex exec resume <SESSION_ID> "<prompt>"` or `codex exec resume --last "<prompt>"`; `--all` widens the `--last` search beyond the cwd; `-i` attaches images. `--json` and `-o` are global, so they work after `resume`. There is also `codex exec fork <SESSION_ID> [PROMPT]` (branches a session) and `codex exec review`.
- With `--last` and no separate prompt, the positional is treated as the prompt (source comment). Prefer the explicit id.
- Storage: rollout files under `$CODEX_HOME` (default `~/.codex`), under `sessions/`. `--ephemeral` skips saving them (docs). The `sessions/YYYY/MM/DD/` layout is **Inference** from memory; not read this pass.

## Q3 Follow-up into a live run

- `codex exec` does not accept mid-run input. Source `lib.rs` `read_prompt_from_stdin` does `read_to_end` on stdin once at startup: prompt from stdin if no positional (or `codex exec -`), else piped stdin is appended to the prompt as a `<stdin>...</stdin>` block. After that stdin is unused. Grade V, read the function. Note it blocks until EOF: if the bridge leaves stdin open without a prompt argument, exec waits forever. Pass the prompt as an argument and close or null stdin.
- Only option with `exec`: queue the note, wait for `turn.completed`, then `codex exec resume <id> "<note>"`.
- `codex app-server` (JSON-RPC server) does support it: `TurnSteerParams {thread_id, client_user_message_id, input: Vec<UserInput>}` in `app-server-protocol/src/protocol/v2/turn.rs`, method `turn/steer` (also `turn/start`, `thread/start`, `thread/resume`, `thread/fork` seen in the README). Grade V, struct read; the README (618 lines) was only grepped. `exec` itself runs an in-process app-server (log line "in-process app-server shutdown failed" in lib.rs). Transport details (stdio JSONL default, websocket option) not verified this pass; **Inference** from the app-server design. The TypeScript SDK (`sdk/typescript`) and Python SDK (`sdk/python`, lists `turn/steer`) wrap this. A bridge wanting live notes should use app-server, at the cost of a second event schema (v2 notifications, not the exec JSONL).
- Open request for related behavior: #24016 "codex exec resume should support promptless follow mode for active goals" (2026-05-22, open). Grade C.

## Q4 Limits and permissions

- Spending cap: **none found**. No `--max-budget`, no `--max-turns` in cli.rs or shared_options.rs. Searched issues for a budget flag; nothing relevant. The bridge must enforce cost itself from `turn.completed.usage` (only at turn end) and kill the process on the wall-clock timeout. Open issues weaken this: #54055 (2026-10-14, "terminal usage can be missing, stale or divergent without coverage status") and #50586 (2026-10-03, no usage reported for failed and interrupted runs). Grade C (issue reports, source-reading, not runtime-reproduced by the authors).
- Timeout: none in exec; a stall has no idle timeout (see Q6).
- Sandbox (docs page, grade V): default for exec is read-only. `--sandbox workspace-write` allows edits in the workspace and runs commands inside the sandbox. `--sandbox danger-full-access` is unrestricted. `--full-auto` "is no longer supported" (use `--sandbox workspace-write`). `--approve-for-me` (alias `--not-so-yolo`) sends approval requests to automatic review with workspace-write. `--dangerously-bypass-approvals-and-sandbox` / `--yolo` skips prompts and sandboxing.
- Closest to "edit files and run commands in this repo without asking": `--sandbox workspace-write`. In exec there is no human to approve, so an action the sandbox blocks is declined (status `declined` exists on command_execution). Network is off by default in workspace-write (**Inference**, from Codex's documented behavior; not re-read). `--yolo` is the fallback if the host is already isolated; the repo's own rule says containers here are not a sandbox, so prefer workspace-write.
- Known gap: #40149 (2026-08-22, open) says `codex exec resume` rejects `-s/--sandbox`, and a resumed turn wrote a file that `-s read-only` had blocked. Issue text read in part. Grade C. **Inference:** set sandbox through `-c sandbox_mode=...` or a profile on resume, and test it.
- Tool names: the exec stream does not expose tool names for built-ins. Shell commands appear as `command_execution` (run as `bash -lc <cmd>`), edits as `file_change` (via apply_patch), web as `web_search`, MCP as `mcp_tool_call` {server, tool}. No separate read or search tool: reading and searching happen as shell commands. **Inference** from item types.

## Q5 Environment

- Child processes get `CODEX_THREAD_ID` set to the thread id (source `protocol/src/shell_environment.rs`: `CODEX_THREAD_ID_ENV_VAR`; core `exec_env.rs` comment "injected when a thread id is provided"). Grade V, code search highlights only. A nested run can detect Codex through it.
- Variables a child should not inherit: `CODEX_THREAD_ID` (a nested codex would confuse threads, **Inference**), `CODEX_API_KEY` and `OPENAI_API_KEY` (docs: don't set them job-wide where repository-controlled code runs). `CODEX_HOME` selects the state directory.
- Instruction file: `AGENTS.md` (Codex's documented behavior; `--ignore-rules` skips execpolicy `.rules` files, `--ignore-user-config` skips `$CODEX_HOME/config.toml`). Note openai/codex has no root AGENTS.md of its own (404), which says nothing about what it reads. I did not fetch the doc that names the discovery order, so the file name is **Inference** from prior knowledge, high confidence.
- Install: `curl -fsSL https://chatgpt.com/codex/install.sh | sh` (README, grade V); also npm and Homebrew listed under package managers (README line 38, list not read). Sign in with ChatGPT (`codex login`) or an API key; `codex exec` reuses saved CLI auth, and `CODEX_API_KEY` overrides for one run (docs page). Version 0.163.0, 2026-10-15, 129,919 stars.

## Q6 Known problems (issue list from `gh search issues`, 2026-10-19; titles read, a few bodies read in part; grade C)

- #50775 (2026-10-03, open): `codex exec` hangs indefinitely when the model response stream stalls; no idle timeout fires. Body: ~35 min, 2.1M input tokens, two stalls of 17 and 11 minutes at 0% CPU, `resume` of the same session stalled again. The bridge needs its own wall-clock and idle timeout.
- #50585 (2026-10-03, open): `--json` shuts down on an interrupted turn without emitting a terminal turn event. The bridge must treat process exit without `turn.completed` or `turn.failed` as an unknown end.
- #54782 (2026-10-17, open): `exec --json` silently drops some item kinds (DynamicToolCall, FunctionCallOutput) via `_ => None`. The activity line will miss those tools.
- #50053 (2026-10-01, open): `web_search` items carry the `id` member twice (duplicate JSON key; strict parsers may reject).
- #50593, #50581, #50596 (all 2026-10-03, open): requests to expose compaction, subagent and per-request usage events in `--json`. The schema is still growing; parse leniently and ignore unknown `type` values.
- #40702 (2026-08-25, open): `exec resume` overrides the persisted cwd when `-C` is omitted. Always pass `-C`.
- #28259 (2026-06-15, open): `exec resume` can append to a Desktop transcript without updating the Desktop index. Not an issue for a bridge-only session store.
- Format drift: `--full-auto` removed, `--experimental-json` kept as alias, `cache_write_input_tokens` is `#[serde(default)]` (added later). The usage object has changed shape across versions (the docs sample lacks `cache_write_input_tokens`). **Inference:** pin the version and parse defensively.
- Searched for "exec json command_execution item.completed missing": nothing found.

## Gaps

- Did not read the app-server README transports or the `turn/steer` flow end to end.
- Did not verify the `sessions/` path layout, the AGENTS.md discovery doc, `-c` override syntax, or network policy in workspace-write.
- No runtime test (brief forbids running). All event shapes come from source types and one docs sample, so real `command_execution` and `file_change` lines are not captured.

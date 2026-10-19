# Pi coding agent and ACP (cluster pi.md)

Read: Pi docs at the main branch of earendil-works/pi (the repo moved from badlogic/pi-mono; old URLs still serve), latest release v0.84.4 (published 2026-08-28T22:08:23Z). Read 2026-10-19. Grades: A controlled/large n, B single team or small n, C anecdote, V vendor guidance. "Full" = read in full, "HL" = highlights only. Nothing was installed or run, so every example below is from docs, not from a live run.

Repo facts (grade V, HL): https://github.com/badlogic/pi-mono shows 116,020 stars, 14,792 forks, 347 open issues, MIT, created 2025-08-09. Package `@earendil-works/pi-coding-agent`. Release cadence is about weekly minors; a downstream tracker says 0.84.0 changed the `message_update` wire shape (https://github.com/fullsend-ai/fullsend/issues/6464, grade B, HL). Odd detail: pi-acp's README says it bundles pi-coding-agent "1.1.0+", while the latest GitHub release I found is v0.84.4. I did not resolve this (maybe the 1.x line is published to npm only, or a later release exists). Check `npm view @earendil-works/pi-coding-agent version` before pinning.

## Q1 Command line and event stream

Sources: https://raw.githubusercontent.com/earendil-works/pi/main/packages/coding-agent/docs/cli.md (V, full), .../json.md (V, full), .../cli-integration.md (V, full).

Commands:
- `pi --print "prompt"` (`-p`): final assistant text only, then exit. No events, no usage.
- `pi --mode json "prompt"`: JSONL events on stdout, then exit. All prompts are given at start; "it does not accept later commands".
- `pi --mode rpc`: long-lived JSONL command protocol (Q3).
- Piped stdin is prepended to the first prompt. `--` ends option parsing. `@file` args work in print/json, rejected in rpc.
- A realistic unattended line (**Inference**, assembled from documented flags): `pi --mode json --session-id <id> --model <provider/id> --thinking off "<prompt>"`.

JSON mode stream. First line is a session header, then events. Framing is strict LF-delimited JSONL (do not use Node `readline`, which also splits on U+2028/2029). Stdout is protocol only, logs go to stderr.
```
{"type":"session","version":3,"id":"uuid","timestamp":"2024-12-03T14:00:00.000Z","cwd":"/path"}
{"type":"agent_start"}
{"type":"turn_start"}
{"type":"message_start","message":{"role":"user","content":"Review this repository","timestamp":1733234401000}}
{"type":"message_update","usage":{...},"assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"Hello"}}
{"type":"message_end","message":{"role":"assistant",...}}
{"type":"turn_end","message":{...},"toolResults":[]}
{"type":"agent_end","messages":[...],"willRetry":false}
{"type":"agent_settled","aborted":false}
```
Event types (json.md): `session` (header), `agent_start`, `agent_end` (`messages`, `willRetry`), `agent_settled` (`aborted`; the real "done" signal because retry, compaction recovery and queued messages can continue after `agent_end`), `turn_start`, `turn_end`, `message_start/update/end`, `tool_execution_start/update/end`, `queue_update`, `compaction_start/end`, `auto_retry_start/end`, `session_info_changed`, `thinking_level_changed`, `entry_appended`; RPC adds `bash_execution_update`, `extension_error`.

Tool events, the source for the activity line (json.md):
```
{"type":"tool_execution_start","toolCallId":"call_abc123","toolName":"bash","args":{"command":"ls -la"}}
{"type":"tool_execution_end","toolCallId":"call_abc123","toolName":"bash","result":{"content":[{"type":"text","text":"complete output"}],"details":{}},"isError":false,"durationMs":12}
```
Tool calls are also visible earlier as `message_update` with `assistantMessageEvent.type` `toolcall_start` (`id`, `toolName`) and `toolcall_end` (full `toolCall`). The simplest activity source is `tool_execution_start` (`toolName` plus `args`). Tool names are lowercase. `message_update` records are delta-only; `message_end` is authoritative.

Usage and cost: each `message_update` carries cumulative `usage` `{input,output,cacheRead,cacheWrite,totalTokens,cost:{input,output,cacheRead,cacheWrite,total}}` (cost in dollars); it "can remain zero until completion" for some providers. `message_end.message.usage.cost.total` is per assistant message (confirmed by a third-party runner reading 0.79.1, https://github.com/kortiene/switchyard/blob/4b07a8ed/adw_sdlc/src/runners/runner-pi.ts, B, HL). There is no single final "result" line with total cost. **Inference**: the bridge must sum `usage.cost.total` over assistant `message_end` events (or call RPC `get_session_stats`, which returns `cost`) and enforce its own cap. No rate-limit event exists; transient 429/5xx surface as `auto_retry_start {attempt,maxAttempts,delayMs,errorMessage}` and `auto_retry_end {success, finalError}`.

Errors and exit codes (cli-integration.md, V, full):
- Print mode: final assistant `stopReason` of `error` or `aborted` gives a nonzero exit; `length` prints partial text, warns on stderr, exits nonzero.
- JSON mode: "A failed or aborted assistant response appears in the event stream but does not by itself produce a nonzero exit status." Exits nonzero only if the invocation throws. Confirmed independently: exit 0 on model error in 0.79.1 (switchyard file above, B) and fullsend #6464 (B). **Inference**: derive failure from the last assistant message's `stopReason` (`error`/`aborted`) and `errorMessage`.
- SIGTERM: print mode disposes the session and exits 143 (switchyard, B, HL). No other numeric exit codes are documented. `pi auth check` exits 0/1/2 for ready/not_ready/invalid (cli.md), usable as a pre-flight login check.

## Q2 Sessions

Sources: cli.md, sessions.md (https://raw.githubusercontent.com/earendil-works/pi/main/packages/coding-agent/docs/sessions.md, V, full).
- Id appears in the JSON header line (`type:"session"`, `id`) and in RPC `get_state` (`sessionId`, `sessionFile`). Shell tools also see `PI_SESSION_ID` and `PI_SESSION_FILE` (environment-variables.md).
- The caller can choose the id: `--session-id <id>` "opens the exact project session ID or creates it if absent" (letters, digits, `.`, `_`, `-`; must start/end alphanumeric). **Inference**: the bridge can generate the id itself and use the same flag for first run and every resume, with no need to parse the id back. It cannot be combined with `--session`, `--continue`, `--resume`.
- Resume options: `--continue`/`-c` (most recent session for the cwd), `--session <path|id>` (path, exact id or partial id; searches current project first), `--fork <path|id>`, `--no-session` (in-memory, cannot be resumed). `--resume` opens an interactive picker, so never use it unattended.
- Storage: `~/.pi/agent/sessions/`, grouped by working directory; override with `--session-dir`, `PI_CODING_AGENT_SESSION_DIR` or setting `sessionDir`. Files are append-only JSONL trees with entry ids.
- Resume bugs: v0.84.4 fixed "resumed sessions corrupting the next appended entry when their JSONL file lacks a trailing newline" (#8345; release notes, V, full). A hang occurs when resuming an established session with an extension loaded, see Q6.

## Q3 Follow-up into a live run

Sources: rpc.md and rpc-commands.md (V, full).
- Yes, natively. `pi --mode rpc` reads JSONL commands on stdin. During a run, `{"type":"prompt","message":"...","streamingBehavior":"steer"}` (omitting it during streaming returns an error), or the dedicated `{"type":"steer","message":"..."}`, queues a note delivered "after the current assistant turn finishes executing its tool calls, before the next LLM call". `follow_up` waits until the agent has no more tool calls. Both reply `{"type":"response","command":"steer","success":true,"data":{"disposition":"queued"}}`. `steer` default mode is `one-at-a-time`; `set_steering_mode all` batches. `clear_queue` (new in 0.84.4) returns and removes pending messages. `abort` stops the run.
- Lifecycle: commands take an optional `id`; the matching response repeats it. A successful `prompt` response only means accepted. Wait for the `agent_settled` event. Closing stdin requests orderly shutdown ("Pi disposes the active runtime before exiting"). Stdout backpressure: keep reading or Pi can stall.
- Resume in RPC: start with `--session-id <id>` (or `--session`), or send `switch_session {sessionPath}` or `new_session`. One RPC process can hold several prompts in sequence, so the bridge may keep it alive between asks or exit and restart with the same `--session-id`.
- Start: `pi --mode rpc` also takes `--model`, `--thinking`, `--tools`, `--name`, `--session-dir`. Per-session cost: `get_session_stats` -> `data.cost` and `tokens`. Also `get_state` and `get_last_assistant_text`.
- Extension UI sub-protocol (`extension_ui_request`/`extension_ui_response`) exists for extension dialogs; core tools have no approval prompts, so it matters only if extensions are loaded.

## Q4 Limits and permissions

- Spending cap: none. Turn cap: none. Wall-clock timeout: none for the whole run. A request for `--max-turns`/`--max-tokens` (#1898, 2026-03-06) was closed by the maintainer the next day (https://github.com/badlogic/pi-mono/issues/1898, B, HL; closing reason not shown in highlights). The fullsend tracker confirms "No `--max-turns`/`--timeout`". Related: #5778 added `streamTimeoutMs`/`toolTimeoutMs` settings (per-stream and per-tool Promise-race timeouts), per issue #5944 (C, HL). **Inference**: bridge enforces wall clock (SIGTERM, then SIGKILL) and dollar cap by summing `usage.cost.total`, then `abort` over RPC or SIGTERM. Cost is native per message for providers that report it; subscription logins may report 0 (not verified).
- Permissions: "Pi does not include a built-in permission system"; it runs with the launching user's permissions (https://github.com/badlogic/pi-mono, V, full). There is no approval prompt, so the default already is "edit files and run commands in this repo without asking". Safety is containerization (docs/containerization.md: Gondolin micro-VM, plain Docker, OpenShell), not flags. The CLI has `-a/--approve` and `-na/--no-approve` for project trust only (project `.pi/` settings and extensions). Headless with no UI it resolves to untrusted, which skips project-local `.pi` extensions (switchyard comment, 0.79.1, B, HL).
- Tools (cli.md): defaults `read`, `bash`, `edit`, `write`; opt-in `grep`, `find`, `ls`, `powershell` (Windows), `codemode`, `tool_search`. Select with `--tools a,b` (allowlist), `--exclude-tools`, `--no-tools`, `--no-builtin-tools`. No web fetch or search tool built in. MCP is built in as of the docs read (`pi mcp add`, `--no-mcp`), although pi-acp's README says Pi has none; docs are newer.

## Q5 Environment, instructions, install

Source: environment-variables.md (V, full).
- Pi sets `AI_AGENT=pi` and `PI_CODING_AGENT=true` in the CLI and RPC entry points (not in SDK embedding); children inherit them. The `bash` tool also gets `PI_SESSION_ID`, `PI_SESSION_FILE`, `PI_PROVIDER`, `PI_MODEL`, `PI_REASONING_LEVEL`. Nested-run detection: `PI_CODING_AGENT=true` or `AI_AGENT=pi`. **Inference**: to keep a child from thinking it is inside Pi's session, strip `PI_*` (at least `PI_SESSION_ID`, `PI_SESSION_FILE`, `PI_CODING_AGENT`, `AI_AGENT`). Per the doc, Pi itself clears stale session variables only when `exposeSessionEnvironment` is false.
- Config vars: `PI_CODING_AGENT_DIR` (default `~/.pi/agent`), `PI_CODING_AGENT_SESSION_DIR`, `PI_OFFLINE`, `PI_SKIP_VERSION_CHECK`, `PI_TELEMETRY`. Provider keys use the usual `ANTHROPIC_API_KEY`, `OPENAI_API_KEY` etc.; credentials live in `~/.pi/agent/auth.json`.
- Instruction files: `AGENTS.md` and `CLAUDE.md` discovered (disable with `-nc/--no-context-files`); `--append-system-prompt <text|path>` adds text.
- Install: `curl -fsSL https://pi.dev/install.sh | sh` (pinned deps; `pi update`) or `npm install -g --ignore-scripts @earendil-works/pi-coding-agent` (Node 22.19+); Nix flake. Machine rule note: this box forbids `npm i -g`; use `mise use -g npm:@earendil-works/pi-coding-agent` (**Inference**). Login: `/login` in interactive mode for subscription or API key; for unattended hosts set provider key env vars or copy `auth.json`. Check with `pi auth check --provider <p> --json`.
- Releases: v0.84.4, 2026-08-28; linux-x64 binary downloaded 11,551 times (release page, V, full).

## Q6 Known problems in headless use (issues are the earendil-works/pi tracker; old numbers resolve under badlogic/pi-mono)

- Does not exit after finishing: #5944 (v0.79.8, resume plus extension, supervisor killed after 900 s; C, HL). Maintainer: an extension not tearing down in `session_end` keeps `-p` alive, "This is intentional" (2026-06-21). A commenter reproduced with an empty extension on 0.80.2: `pi --mode json -p --no-session` hangs, adding `--no-extensions` exits in ~3 s (C, HL). #4128 (0.72.1): print mode and JSON hang after `agent_end` with the Codex WebSocket cache; closed `no-action` (C, HL). #2677 same family (referenced only). **Inference**: always pass `--no-extensions` (unless needed) and have the bridge kill after `agent_settled` or a grace period.
- Hangs with no output when stdout is not a TTY for some providers (deepseek, custom OpenAI-compatible) on 0.79.1; a pseudo-TTY wrapper works (#5628, closed `no-action`; C, HL). #4303: `pi -p --mode json` never exits when stdin is /dev/null (referenced; not read). **Inference**: spawn with a real stdin pipe you close deliberately, and test each provider headless.
- JSON mode exits 0 on failed model turns (above). Format drift: 0.84.0 changed `message_update` to deltas without cumulative `partial` (json.md plus fullsend, B). Pin an exact version and record fixtures.
- New external contributors' issues and PRs are auto-closed, so fixes for these may be slow.
- Searched for a cost-cap or budget flag and found none.

## ACP (Agent Client Protocol)

Sources: https://agentclientprotocol.com/overview/agents (V, HL), https://agentclientprotocol.com/protocol/v1/overview (V, HL), adapter repos (V/B, HL).
- Protocol: JSON-RPC 2.0 over stdio. Baseline `initialize`, `authenticate`, `session/new`, `session/prompt`; notification `session/cancel`; optional `session/load` (needs `loadSession` capability), `session/set_mode`. Turn ends when the `session/prompt` response returns a `stopReason`. Progress arrives as `session/update` notifications; the agent can send permission requests to the client. Draft v2 (opt-in via `protocolVersion: 2`) adds `session/resume`, `session/list`, `session/close`, `session/fork`, and steering by a second `session/prompt` mid-turn.
- Who speaks it (the ACP agents page lists them; marked N = native, A = adapter):
  - Claude Code: A, `@agentclientprotocol/claude-agent-acp` (wraps the Claude Agent SDK; v2 draft behind `CLAUDE_AGENT_ACP_EXPERIMENTAL_V2=1`; steering v1 only).
  - Codex: A, `@agentclientprotocol/codex-acp` (starts the Codex App Server; has approval and sandbox mode config and token usage events).
  - Gemini CLI: listed by the ACP page; native flag (the brief names `--experimental-acp`) not verified by me.
  - OpenCode: N, `opencode acp` exists in source (`packages/opencode/src/acp/service.ts` implements newSession, loadSession, resumeSession, prompt, cancel; PR #44524 adds v2 mid-turn steering behind `OPENCODE_EXPERIMENTAL_ACP_V2`). Command spelling not read from docs.
  - Pi: A only, via `pi-acp` (https://github.com/svkozak/pi-acp, 735 stars, 87 open issues, created 2025-12-19; spawns `pi --mode rpc`). Limitations: no ACP `fs/*` or `terminal/*` delegation, MCP params stored but not wired, client-side queue "like pi's one-at-a-time". Other forks exist (0xSero 30 stars, Attamusc, patrick-xin); the ACP agents page names this adapter.
- Could one ACP client drive all five? **Inference**: yes for the common path (new session, prompt, stream tool calls, cancel), and it is the same interface Zed and other clients use. Evidence of prior art: a harness in zeronsh/comet spawns ACP agents (Grok, Hermes, Pi via pi-acp) with one implementation (https://github.com/zeronsh/comet, C, HL). What ACP does not give you, from this reading: no spending cap (no budget field in the protocol, only usage updates), no standard way to know session cost in dollars, mid-run steering only on v2 draft or agent extensions (so queue-and-resume for v1), session resume needs `loadSession` (Claude adapter, OpenCode, Kimi have it; Pi through its mapping file), permission requests must be auto-answered by the client (comet auto-accepts the agent's preferred allow option). Cost: it adds a Node adapter process per harness and each adapter has its own bugs (87 open pi-acp issues). **Inference**: native per-harness stream normalizers give you cost and exact resume ids; ACP gives you one parser. Pi's own RPC is richer than pi-acp for this use (id, `get_session_stats`, `steer`).

## Gaps
- No live run; examples are from docs.
- Gemini and Codex native ACP flags are not verified here; npm version 1.x vs GitHub 0.84.4 unresolved; whether subscription logins report non-zero cost unverified; #4303 and #2677 not read.

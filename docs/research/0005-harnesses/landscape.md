# Landscape: prior art for one program driving several agent CLIs, and popularity (meat-nv6.3)

Grades: A controlled/large n, B single team/vendor internal/small n, C anecdote/opinion, V vendor guidance.
Read depth: "full" = fetched page read, "hl" = search highlights only. Today is 2026-10-19.
Budget used: 8 tool calls. Nothing found on cost caps in any tool below (see Gaps).

## 1. Vibe Kanban (BloopAI), the closest prior art

### 1.1 Interface shape (Q: command builder, normalizer, session id, follow-up)
- Source: https://github.com/BloopAI/vibe-kanban/blob/70ca5a06/crates/executors/src/executors/mod.rs (undated, commit 70ca5a06). Grade B, hl.
  Trait `StandardCodingAgentExecutor` with: `spawn(dir, prompt, env)`, `spawn_follow_up(dir, prompt, session_id, reset_to_message_id, env)`, `spawn_review(...)` (defaults to follow-up if a session id exists, else spawn), `normalize_logs(msg_store, worktree_path)`, `default_mcp_config_path()`, `get_availability_info()`, `get_setup_helper_action()`, `discover_options()`, `apply_overrides()`, `use_approvals()`.
  Errors: `FollowUpNotSupported`, `ExecutableNotFound`, `AuthRequired`, `CommandBuild`, `ExecutorApprovalError`.
- Source: https://deepwiki.com/BloopAI/vibe-kanban/3-executor-system (undated, generated from source). Grade B (derived, third party), read in full. Per-agent table:

| Agent | Transport | Permission knob | Resume |
|---|---|---|---|
| Claude Code | stdio stream-json, plus bidirectional SDK control protocol | `dangerously_skip_permissions` | `--resume <id>` (+ `--resume-session-at <uuid>` to truncate) |
| Codex | `codex app-server`, JSON-RPC over stdio | `ask_for_approval`, `sandbox: "danger-full-access"` | `thread_fork` |
| OpenCode | `opencode-ai serve --port 0`, REST + SSE, random server password per run | `auto_approve` | session id via HTTP API |
| Amp | stdio stream-json (reuses Claude log processor) | `dangerously_allow_all` | `threads continue <id>` |
| Gemini | ACP (Agent Client Protocol) | `yolo` | ACP follow-up |
| Cursor | stdio stream-json | `force` | `--resume <id>` |
| Copilot | ACP | `allow_all_tools` | ACP follow-up |
| Qwen Code | ACP | `yolo` | ACP follow-up |
| Droid | `droid exec`, stream-json | `autonomy` | `--session-id <id>` |

  Answers Q-landscape "interface shape per agent". **Inference**: Vibe Kanban ended with three transport families (stdio JSONL, JSON-RPC app server, HTTP server) plus ACP for three agents. A plain "spawn, parse JSONL" interface did not survive for Codex or OpenCode. Gemini is driven over ACP, not `gemini -p`.
- Session id capture differs per agent: Codex via `ThreadStarted` notification or `sessionConfigured`, with a regex fallback "in case the JSON parsing fails ... the line is truncated due to size limits because it includes the full session history" (normalize_logs.rs, commit 70ca5a06, grade B, hl). Normalized entry types: AssistantMessage, ToolUse{tool_name, action_type, status}, ErrorMessage, Thinking. Codex stderr noise is filtered with a suppression list (e.g. "state db missing rollout path for").
- `SpawnedChild` carries `exit_signal` (oneshot) and `cancel` (token): executors that own their lifecycle (Claude SDK, Codex JSON-RPC, OpenCode HTTP) signal completion themselves because the process does not exit on its own.
- Default profiles (DeepWiki, default_profiles.json): every agent defaults to its no-approval mode for unattended work (Claude skip-permissions, Gemini yolo, Codex danger-full-access, OpenCode auto_approve, Cursor force, Copilot allow_all_tools, Droid skip-permissions-unsafe).
- Capabilities flags per agent: `SessionFork` (Claude, OpenCode, Codex, Gemini, Qwen), `ContextUsage` (Claude, OpenCode, Codex only), `SetupHelper` (Codex, Cursor). **Inference**: only three of nine agents expose usage in a form it could parse; nothing in the sources shows a cost cap or a normalized cost field. Searched; no cost or budget field found in the trait or capabilities.

### 1.2 What broke (Q6 from the harness driver's side)
- Codex event rename. https://github.com/BloopAI/vibe-kanban/issues/858 (2025-09-26, closed 2025-10-07). Codex CLI 0.41.0 emitted `{"id":"0","msg":{"type":"agent_message","message":"ok"}}`; Vibe Kanban v0.0.95/96 only knew `assistant_message`, so "No assistant message found" and `external_session_id=None`. Maintainer: Codex "pinned at @openai/codex v0.38.0 in the default agent configurations". Fix was PR #947 "Upgrade Codex to the latest version". Grade B, read in full. Lesson: pin CLI versions and bump deliberately. Release notes confirm the practice: "Bump Claude Code CLI pin to 2.1.119", "Bump Codex to 0.124.0 and refresh the Codex executor surface" (https://www.vibekanban.com/release-notes, v0.1.44, 2026-04-24, grade B, hl).
- Codex `exec --json` gave no session id. https://github.com/openai/codex/issues/3817 (opened 2025-09-17, codex-cli 0.36.0; Vibe Kanban users confirm on 0.38/0.39). Workaround of reading `~/.codex/sessions` "is not reliable" when runs overlap. Maintainer 2025-10-01: use `codex exec --experimental-json` (to become `--json`) which returns `thread_id`, with `codex exec resume <thread_id>`. Flags from the first run are not preserved on resume (JSON output off after resume); args must sit between `exec` and `resume`. Grade B, read in full. Answers Q2 and Q6 for the Codex cluster as seen by a driver.
- Codex resume depends on the home dir. https://github.com/BloopAI/vibe-kanban/issues/1648 (2025-12-26, closed 2026-01-13): with `CODEX_HOME` changed, follow-up failed with "Session not found: Could not find rollout file for session_id: ...", while `codex resume <id>` worked in a terminal; also `--ask-for-approval never` was ignored through the driver. Grade B, read in full. **Inference**: a nested run must see the same `CODEX_HOME` as the first run, or resume fails.
- Resume stream drop. https://github.com/BloopAI/vibe-kanban/issues/1555 (2025-12-16, open): second turn on Azure OpenAI fails with "stream disconnected before completion", while `codex resume <id>` by hand worked. Grade C, hl.
- Completion never signalled. https://github.com/BloopAI/vibe-kanban/issues/2495 (2026-02-02, open at last update 2026-02-09): "Claude Code in `-p --output-format=stream-json` mode does **not** exit after completing a task — it stays alive waiting for more messages on stdin"; sessions stuck "running" despite `{"type":"result","subtype":"success"}`. Fix suggested: act on the `result` line (send success/failure by `is_error`) and end the process. Grade B, hl. Directly relevant: Meat Mode already closes stdin after the last turn, which avoids this.
- Follow-up regression when the trait changed. https://github.com/BloopAI/vibe-kanban/discussions/2620 (2026-02-08): Droid 0.57.9 follow-ups broke after `reset_to_message_id` was added; the fork-session approach "was incompatible with Droid's actual CLI interface"; fix passed the original id to `--session-id`. Grade C, hl.
- Project status: "Today we're shutting down bloop, the company behind Vibe Kanban. The Vibe Kanban project will live on, open source and community maintained." (https://www.vibekanban.com/blog/shutdown, 2026-04-10, grade B, hl). Remote services removed after 30 days; local workspaces keep working. Releases continued to v0.1.44 on 2026-04-24. **Inference**: reuse its design, do not depend on it.

## 2. Other tools (shape of integration, status)

- claude-squad (smtg-ai), https://github.com/smtg-ai/claude-squad (README, undated, grade B, hl). Wraps any command: `cs -p "codex"`, `cs -p "gemini"`, `cs -p "aider ..."`. Uses tmux per agent plus git worktrees. `-y/--autoyes` "experimental ... automatically accept prompts for claude code & aider". No structured output at all: it screen-drives a TUI. **Inference**: this is the opposite end from Meat Mode's need (activity line, notes, cost); it cannot give tool names or cost. Teardown (reeve competitive-landscape.md, undated, grade C): AGPL-3.0, "no session persistence across restarts", tmux and gh required.
- Conductor (Melty Labs), https://www.conductor.build/docs/guides/parallel-agents/run-multiple-claude-code-sessions (undated, grade V, hl). Mac app, worktree per workspace; reviews say it "Wraps existing agent CLIs; bring your own subscription" (reeve teardown, grade C). Supports Claude Code, Codex, Cursor and "recently OpenCode" (agentsroom.dev blog, undated, grade C). Closed source, so no internals found.
- Crystal / Nimbalyst (Stravu). Crystal "Deprecated February 2026", successor Nimbalyst (reeve teardown, grade C; agentsroom.dev agrees). Claude and Codex are core, "several additional harnesses are alpha" (rywalker.com, undated, grade C). Churn example.
- Sculptor (Imbue). Container per agent, "Claude Code and Codex" (rywalker.com, grade C, hl). Closed source per reeve; agentsroom says MIT; sources conflict, not resolved.
- Omnara (YC S25), https://news.ycombinator.com/item?id=46991591 (Launch HN, undated, grade B, read in part). Quote: "Our first attempt ... was a lightweight wrapper that streamed messages from the Claude Code CLI to a mobile app, but that approach ended up being fragile and hard to maintain." They rewrote on the Claude Agent SDK and run the agent loop in a daemon with an outbound WebSocket to a relay. Closest in topology to Meat Mode's bridge (outbound dial, relay), and evidence that wrapping CLI output was the fragile path. Codex is supported, mechanism not stated.
- Happy Coder (slopus/happy), https://slopus-happy-9.mintlify.app/development/data-flow and /components/cli (undated, grade B, hl). Event model: `text`, `tool-call`, `tool-result`, `file-edit`, `thinking`, `turn-start`/`turn-end`, with an ACP-shaped message carrying `provider: 'gemini'|'codex'|'claude'|'opencode'`. Messages typed on the phone while the CLI is in local mode are "queue[d] for later"; in remote mode they go to the SDK. A fork's notes (https://developer.vanguard.dev.trueserve.deloitte.com/08-environments/happy-fleet-control/, 2026-07-08, grade C, hl) report Codex "talks a proprietary app-server protocol ... not wrappable" in the VS Code extension and a resume crash in happy 1.1.10 ("Cannot read properties of null"). Issue slopus/happy#379: permission prompts and local/remote switching.

**Inference (patterns across the tools)**
1. Rich UIs (Vibe Kanban, Happy, Acepe) use a structured channel per agent: stream-json, JSON-RPC app server, HTTP server or ACP. Screen-scraping tools (claude-squad) cannot show tool names or cost.
2. Every follow-up path is "spawn again with a session id" except where the agent has a server or RPC mode. Vibe Kanban's trait has no "send into live run" method at all; only spawn_follow_up. Happy queues phone messages when the agent is not in a mode that accepts them. This supports the Meat Mode fallback: queue the note, resume after the turn ends.
3. No tool found a normalized cost cap. Unattended safety came from "skip all permissions" defaults plus worktree or container isolation. A bridge-side wall clock timeout and a per-adapter cost read are the gaps nobody documented.
4. The recurring breakage was format drift on CLI upgrade and session id capture. Mitigations seen: pin versions, keep a fallback regex for the id, filter known stderr noise, treat unknown event types as ignorable.

## 3. Popularity (Q: are the four picked the most popular?)

Snapshots from aggregators. Grade C throughout (third-party counters, dates differ); numbers within a source are consistent, across sources they are not.

| Agent | Stars (source, date) | Other adoption signal |
|---|---|---|
| OpenCode (anomalyco/opencode) | 217K (agentgid, 2026-10-19); 214.1K (mcp.so, undated); 210,085 (developertoolkit.ai, 2026-09-26) | 2.1M npm downloads/week, up 21% in 30d (agentgid, as of 2026-10-17) |
| Claude Code | 152K (agentgid, 2026-10-19); 151.2K (mcp.so). Repo holds docs and issues only (techstackups, 2026-05-07) | 6.1M weekly downloads, down 26.9% in 30d; 28.1M VS Code installs |
| Codex CLI | 129,892 (gitdebt, 2026-10-19); ~31k gained in 90 days | 13.9M weekly downloads, up 13.7% in 30d; 15.7M VS Code installs |
| Gemini CLI | 107,453 (gitdebt, 2026-10-19); ~2.6k gained in 90 days | Since 2026-06-18 no longer serves free, Google AI Pro or Ultra users; those moved to closed-source Antigravity CLI `agy`. Alive for Code Assist Standard/Enterprise and paid API keys; npm 0.63.0 on 2026-10-06 (developertoolkit.ai, read 2026-10-12) |
| Pi (earendil-works/pi, was badlogic/pi-mono) | 116,170 (GitHub-derived listing, undated, fetched this session); 109,402 (developertoolkit, 2026-09-26); 67k (arcbjorn, 2026-07-04); 45.7k (techstackups, 2026-05-07) | Fastest grower in agentgid: downloads up 72% in 30d. Repo moved 2026-05-07; npm `@earendil-works/pi-coding-agent` from 0.74.0, 0.84.2 later (pi.dev changelog, HF docs PR) |
| Cursor CLI | no CLI repo; Cursor repo 33,036 is a code-free tracker (recatools, 2026-07-15) | Closed source, "free tier, $20/month individual" (crystl.dev, 2026-08-25) |
| Amp | no public metric | 15.7K weekly downloads (agentgid); proprietary |
| GitHub Copilot CLI | 11,211 (docs and issues only, developertoolkit, 2026-09-26) | 1.1M weekly downloads, down 43.8% in 30d (agentgid) |
| Qwen Code | 28,137 (developertoolkit, 2026-09-26) | 43.4K weekly downloads (agentgid) |
| goose (aaif-goose/goose) | 55.4K (agentgid, 2026-10-19); 54,661 (developertoolkit) | Linux Foundation AAIF project |
| Aider | 49,636 (gitdebt, 2026-10-18) | Last release 0.86.2 on 2026-02-12, last push 2026-05-22; "Slowing" (developertoolkit). 70.9K weekly downloads, down 31% |

Sources: https://gitdebt.com/compare/ai-coding-tools (data updated Oct 2026, hl), https://agentgid.com/categories/coding-cli/ (2026-10-19, hl), https://mcp.so/rankings/cli (undated, hl), https://developertoolkit.ai/en/ecosystem/agent-tools/other-coding-agents/ (2026-09, hl), https://techstackups.com/comparisons/coding-agent-harness-comparison-2026/ (2026-05-07, hl), https://blog.arcbjorn.com/state-of-cli-coding-agents-2026 (2026-07-04, hl), https://recatools.com/guides/best-ai-coding-agents-2026/ (2026-07-14, hl), https://crystl.dev/compare/claude-code-alternatives/ (2026-08-25, hl). All answer the popularity question; grade C.

**Inference on the pick**
- By stars the top five are OpenCode, Claude Code, Codex, Pi, Gemini CLI. The operator's four plus Claude Code are the top five, so the pick matches stars.
- By weekly downloads the order is Codex (13.9M), Claude Code (6.1M), OpenCode (2.1M), Copilot CLI (1.1M); Gemini and Pi figures were not in the highlights. Copilot CLI is the one omitted agent with real usage (1.1M/week) but falling and closed.
- Gemini CLI is the weakest of the four: stars flat (+2.6k in 90 days against Codex +31k), and consumer access ended 2026-06-18. A bridge user on a personal Google account can no longer use it free, so its value depends on whether the operator has API-key or Code Assist access. Worth stating as a decision: keep, or swap for a candidate with growth (Cursor CLI, Copilot CLI, goose).
- Vibe Kanban's roster adds Amp, Cursor, Copilot, Qwen, Droid. Qwen Code is a Gemini CLI fork (arcbjorn, grade C), so a Gemini adapter may cover it cheaply. **Inference**, not tested.
- Pi's star count is volatile across sources (45.7k in May to 116k now); treat as fast-growing, not precise.

## 4. Gaps
- No source found for how Conductor, Sculptor, Crystal or Omnara drive Codex or Gemini internally (closed or undocumented). Searched; only marketing and teardown pages.
- No tool's cost cap handling found. Vibe Kanban's trait and capabilities show no budget field; I did not read each executor file, so a per-executor cost read could exist. Check `claude.rs` and `codex.rs` on the latest commit if needed.
- Vibe Kanban issue sample is from search results, not a full issue-tracker sweep; counts of breakage are not measured.
- Star numbers are from aggregators, not the GitHub API; Claude Code's count differs by 14K between 2026-07 and 2026-10 sources, plausible growth but unverified.
- ACP native support per agent is in the other clusters (`pi.md`); here only Vibe Kanban's use of ACP for Gemini, Copilot and Qwen is recorded.

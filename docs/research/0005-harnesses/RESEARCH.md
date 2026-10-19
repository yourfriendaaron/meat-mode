# Research: agent harnesses beyond Claude Code

The question: how the bridge should run coding agents other than Claude Code for Ask agent, which ones to support first, and what one interface over all of them must declare. Reader notes: `codex.md`, `opencode.md`, `gemini.md`, `pi.md` (with the Agent Client Protocol), `landscape.md` (prior art and popularity); brief: `brief.md`. Grades: A controlled or large n, B one team or small n, C anecdote, V vendor guidance.

## Findings

1. **Popularity** (aggregators, 2026-10, C): OpenCode 217k stars, Claude Code 152k, Codex 130k, Pi 109k to 116k, Gemini CLI 107k. Weekly npm downloads: Codex 13.9M, Claude Code 6.1M, OpenCode 2.1M, Copilot CLI 1.1M. The four picked plus Claude Code are the top five by stars. (`landscape.md`)
2. **Gemini CLI is the weak pick.** It gained 2.6k stars in 90 days, and since 2026-06-18 it no longer serves free, AI Pro or Ultra accounts; it still works with an API key or Code Assist (C). (`landscape.md`)
3. **Prior art splits the job the same way.** Vibe Kanban's executor has spawn, spawn-follow-up with a session id, and a log normalizer, with no method for input into a live run; every agent runs in its no-approval mode (B, source read). It broke when Codex renamed an event (`assistant_message` to `agent_message`, VK #858) and when `codex exec --json` dropped the session id (openai/codex #3817); its fix was pinning CLI versions (B). (`landscape.md`)
4. **How each harness runs headless** (source or docs at the version named, V and A):

   | | Claude Code | Codex 0.163 | OpenCode 1.19 | Gemini CLI 0.64 | Pi 0.84 |
   |---|---|---|---|---|---|
   | Command | `claude -p` stream-json in and out | `codex exec --json -C <dir> --sandbox workspace-write <prompt>` | `opencode run --format json --auto <prompt>` | `gemini -p <prompt> --output-format stream-json --approval-mode yolo` | `pi --mode rpc --no-extensions` |
   | Prompt | stdin line | argv | argv | argv | stdin `prompt` command |
   | Session id | `session_id` on lines | `thread.started.thread_id` | `sessionID` on every line | `init.session_id` | session header; `--session-id` to choose |
   | Resume | `--resume <id>` | `codex exec resume <id>` | `-s <id>` | `--resume <id>` | `--session-id <id>` |
   | Note into a live run | stdin user line | no (`exec` reads stdin once) | no (server mode drops some, #46842) | no (only ACP mode) | `steer` command on stdin |
   | Turn end | `result` | `turn.completed` / `turn.failed` | `step_finish` with reason `stop` | `result` | `agent_settled`; failure in the last `stopReason` |
   | Cost | `total_cost_usd`; `--max-budget-usd` | tokens only | `step_finish.part.cost` (0 for unpriced models) | tokens only | `usage.cost.total` per message |
   | Instruction file | CLAUDE.md | AGENTS.md | AGENTS.md (CLAUDE.md fallback) | GEMINI.md | AGENTS.md |
   | Own session markers | `CLAUDECODE`, `CLAUDE_CODE_*` | `CODEX_THREAD_ID` | none documented | `GEMINI_CLI`, `GEMINI_SANDBOX` | `PI_CODING_AGENT`, `AI_AGENT` |

5. **Only Claude Code caps spending.** Codex and Gemini report tokens only; OpenCode and Pi report dollars per step or message but stop at nothing (V). None has a timeout of its own. (`codex.md`, `opencode.md`, `gemini.md`, `pi.md`)
6. **Every harness has headless hangs on record**: Codex exec with no idle timeout (#50775, 2026-10); Gemini at 100% CPU ignoring SIGTERM (#29434, 2026-09); Pi not exiting when stdout is not a TTY (#5944, #4128); OpenCode when a permission resolves to ask (#36762, fixed 2026-08) (B, C). The bridge's timeout and SIGKILL after a grace already cover a hang.
7. **ACP does not remove the per-harness work.** OpenCode speaks it natively, Gemini in a mode, Claude Code, Codex and Pi through adapters. ACP v1 has no cost and no cap, and live steering is only in the v2 draft (V). Pi's own RPC is richer than its adapter. (`pi.md`)
8. **Codex resume quirks**: a resumed run ignores the saved working directory unless `-C` is passed (#40702), and one report has resume rejecting the sandbox flag (#40149) (C). (`codex.md`)

9. **Antigravity CLI (`agy`) is Gemini CLI's successor** for free, Google One, AI Pro and AI Ultra accounts since 2026-06-18 (geminicli.com headless page banner, V). Its headless mode (antigravity.google/docs/cli/headless, V, highlights only): `agy -p` with `--output-format stream-json` (`init`, `step_update`, `result` events), `--input-format stream-json` for more prompts on stdin in one process, `--conversation <id>` to resume, `--dangerously-skip-permissions`, `--print-timeout` (default 5m), token usage only. It is closed source. A third-party migration guide (jamubc/gemini-mcp-tool, C) reports agy 1.0.x print mode writing nothing to stdout in non-TTY use and the skip-permissions flag doing nothing in print mode; neither is confirmed for the current release.

## Decisions (Inference)

- **Build Claude Code, Codex, OpenCode, Antigravity CLI and Pi; defer Gemini CLI.** None needs Meat Mode to invent a store or a protocol; two of them need the bridge to hold a note until the turn ends (finding 4). The operator chose Antigravity CLI over Gemini CLI because Google moved free and subscription accounts to it (findings 2, 9); Gemini CLI waits for API-key users.
- **One `Harness` interface, each harness in its own package that registers itself**, as the trackers are built (`docs/trackers.md`). The bridge reads what a harness can do as data. The contract is `docs/harnesses.md`.
- **Drive each harness's own headless JSON stream, not ACP** (finding 7). Pi uses its RPC mode, the only one that takes a note into a live run.
- **What varies is declared**, so the runner has one path: how the prompt goes in (argv or stdin), whether a note can go into a live run (`Steer`), the budget mode (native, metered by the bridge from reported cost, or none with the timeout only), and the harness's own session markers. A per-harness decoder turns stdout lines into one event shape: session id, tool call by kind, cost, usage limit, turn end.
- **A note to a harness without `Steer` waits for the turn to end, then resumes the session with it, in the same run.** Prior art does the same (finding 3).
- **Fixtures are pinned per harness version** (finding 3): each package's conformance fixtures name the version and whether they were recorded live or written from source.

## Call

**Proceed**, with Antigravity CLI in place of Gemini CLI, as the operator chose.

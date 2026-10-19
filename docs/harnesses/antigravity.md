# Antigravity CLI

[Antigravity CLI](https://antigravity.google/docs/cli/overview/) is Google's coding agent for the terminal, the `agy` program, and the successor to Gemini CLI for Google accounts on the free, Google One, AI Pro and AI Ultra plans. With `MEAT_MODE_AGENT_HARNESS=antigravity`, Ask agent runs it on the bridge's machine in its headless mode.

## Install

Install agy from its [getting started guide](https://antigravity.google/docs/cli/getting-started) (`curl -fsSL https://antigravity.google/cli/install.sh | bash` on macOS and Linux). Then run `agy` once in a terminal and sign in: headless mode uses the saved sign-in and has no prompt of its own, so a machine that has not signed in fails each run with `authentication required`. `GEMINI_API_KEY` or `AGY_ADC_AUTH` work instead of a sign-in. The bridge looks for `agy` on PATH, and runs `agy --version` to show the version in Settings.

agy reads standing instructions from both `AGENTS.md` and `GEMINI.md` and recommends `AGENTS.md`, so the instruction text for agents on that machine names `AGENTS.md`.

## The command

```
agy --input-format stream-json --output-format stream-json --dangerously-skip-permissions --print-timeout 0 [--conversation <id>]
```

- The prompt goes in on stdin as a user line, `{"event":"user","message":{"content":"..."}}`, and stdin stays open. agy runs each user line as one turn and ends each with a `result` line, so a note goes in the same way while the agent works. In this mode agy ignores a prompt given with `-p`.
- The bridge reads the conversation id from `init` and every later line, tool calls from `step_update` lines with `step_type` `tool` (`run_command` with its `CommandLine`, file writes, edits and reads with their path, searches, URL reads, `invoke_subagent`), and the turn's end from `result`: `SUCCESS` ends it well, any other status ends it as failed with agy's `error`. Each `user_input` step is agy taking in one more line, which is how the app knows a note was read.
- When a turn ends and nothing is left to send, the bridge closes stdin and agy exits.
- `--dangerously-skip-permissions` approves every tool call. Without it agy soft-denies shell commands in headless mode: the turn carries on and ends `SUCCESS` without having run them. Scoped `permissions.allow` rules in `~/.gemini/antigravity-cli/settings.json` are the alternative, through `MEAT_MODE_AGENT_COMMAND`.
- `--print-timeout 0` waits for the turn however long it takes, so the bridge's time limit is the one that stops a run. agy 1.2.6 made unlimited the default, but its headless docs still say 5 minutes, and when its own timeout fires it returns a partial answer and exits 0, which would read as a turn that ended well.
- `--conversation <id>` resumes the task's conversation on its next ask.

`MEAT_MODE_AGENT_COMMAND` replaces everything before `--conversation`. A replacement must keep both stream-json formats.

## Limits

- **No spending cap.** agy reports tokens, not money, and has no cap of its own. A run stops at the time limit (`MEAT_MODE_AGENT_TIMEOUT`) only, and `MEAT_MODE_AGENT_BUDGET_USD` is ignored. The app says so.
- **A note reaches a live run.** It goes to stdin at once. A note sent mid-turn waits for the current turn to end, then runs as the next turn in the same process; it does not interrupt the turn. A live run on agy 1.3.6 showed this: agy echoed the note as a `user_input` step after the first turn's `result`, then answered it.
- **Usage limit.** A `result` with a quota error (`RESOURCE_EXHAUSTED`, "Individual quota reached", "Your AI credits balance is too low") ends the run as a usage limit, with the reset time when agy gives one ("Resets in 2h39m52s").

## Things to know

- agy is closed source. The decoder is built from its headless docs and changelog, not from its code, and its fixtures are written from those docs. agy publishes no list of tool names; tools it does not name map to a generic activity line.
- A turn that ends `SUCCESS` with no answer is counted as failed. agy can end a long turn that way, or as `CANCELED`, when it lost the turn ([antigravity-cli#902](https://github.com/google-antigravity/antigravity-cli/issues/902)), and can end a turn while a command it started is still running ([#1044](https://github.com/google-antigravity/antigravity-cli/issues/1044)).
- A run can end without a `result` line, for example on a full disk ([#1290](https://github.com/google-antigravity/antigravity-cli/issues/1290)). The bridge counts a process that exits without a turn end as a failed run.
- An MCP server that never starts can hold a turn until the time limit ([#1087](https://github.com/google-antigravity/antigravity-cli/issues/1087)), and on some models a used-up quota shows as a timeout, not a usage limit ([#1278](https://github.com/google-antigravity/antigravity-cli/issues/1278)).
- Each headless start takes some seconds of its own ([#1291](https://github.com/google-antigravity/antigravity-cli/issues/1291)), and each run is saved as a conversation in agy.
- Two problems reported against agy 1.0.x by a third-party migration guide no longer apply as far as the current docs and changelog show: print mode writing nothing to stdout when not run in a terminal (fixed across 1.0.15 to 1.1.28; hangs on some prompts are still reported in [#594](https://github.com/google-antigravity/antigravity-cli/issues/594)), and `--dangerously-skip-permissions` doing nothing in print mode (documented as working, with `init` reporting `always-proceed`). A live run on agy 1.3.6 printed its stream with no terminal and ran a shell command with no prompt.
- agy documents no variable it sets in the shells it runs. Its built-in helper scripts read `ANTIGRAVITY_CONVERSATION_ID`, `ANTIGRAVITY_LS_ADDRESS`, `ANTIGRAVITY_CSRF_TOKEN`, `ANTIGRAVITY_SIDECAR_WEB_PORT` and `ANTIGRAVITY_AGENTAPI_EXE` from the environment agy gives them, so a bridge started from inside an agy session passes none of those to the runs it starts. `AGY_*` settings and `GEMINI_API_KEY` are your own and pass through.

## Version

Built against agy 1.4.2's headless docs and changelog. One fixture is a live run with a note sent mid-turn (`live, agy 1.3.6`); the others are written from those docs (`from source, agy 1.4.2`) and should be recorded again from a live run.

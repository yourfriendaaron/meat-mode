# OpenCode

[OpenCode](https://github.com/anomalyco/opencode) is an open-source coding agent that works with most model providers. Pick it with `MEAT_MODE_AGENT_HARNESS=opencode`.

## Install and sign in

Install it from the OpenCode docs (`curl -fsSL https://opencode.ai/install | bash`, `npm install -g opencode-ai`, or the Homebrew tap) so that `opencode` is on the bridge's PATH. Sign in once as the user the bridge runs as, with `opencode auth login`, or give the bridge's environment the provider's API key. The bridge's check runs `opencode --version`; a failure leaves agents unavailable on that machine.

## What the bridge runs

```
opencode run --format json --auto --dir <repo> [-s <session>] < prompt
```

- `--format json` prints one JSON object per line. `--auto` approves every permission request that the config does not deny, so no run waits on a question nobody can answer.
- `--dir` sets the repo explicitly, since OpenCode takes its root from `$PWD`.
- A follow-up resumes with `-s <session id>`, never `-c`, which picks the newest session in any project.
- The prompt goes in on stdin, and the bridge closes stdin after it: with no message argument, OpenCode reads stdin to the end and sends it as the message. A prompt or note that starts with `-` therefore never reaches the flag parser, and the text arrives as written; OpenCode wraps a message argument that holds a space in quotes.

`MEAT_MODE_AGENT_COMMAND` replaces the program and the fixed flags, so it must keep `run --format json`: for example `/opt/bin/opencode run --format json --auto`.

## Limits

| Limit | Enforced |
|---|---|
| Time limit for a run, runs per task, runs at once | by the bridge, as for every harness |
| Spending cap | by the bridge: it sums the cost OpenCode reports on each step and stops the run past `MEAT_MODE_AGENT_BUDGET_USD` |
| Notes into a live run | not supported: a note waits for the turn to end, then the bridge resumes the session with it |

Cost is 0 for a model with no price, such as a free or custom one, so the cap does not fire for those runs and only the time limit applies. A helper agent started with OpenCode's `task` tool runs in a child session, and its cost is not added: `opencode run` prints only the parts of the run's own session (`cli/cmd/run.ts` skips any part whose session differs), and the `task` tool's result names the child session and model but no cost. The cap counts only the main agent's spending.

## What the app shows

A run's activity line comes from OpenCode's tool calls: `bash`, `edit`, `apply_patch`, `write`, `read`, `glob`, `grep`, `webfetch`, `websearch`, `task` and `todowrite`. OpenCode prints a tool call only once it has finished, so the line trails the work by one call. A turn ends at a step that finishes with reason `stop`; an `error` line ends it as failed, and one that reads as a usage or rate limit also reports the limit.

## Things to know

- OpenCode reads `AGENTS.md`, and falls back to `CLAUDE.md`.
- It has no sandbox. `--auto` lets it run any command in the repo's user account.
- A permission that resolves to `ask` has hung headless runs in some releases; `--auto` answers them, and the time limit is the backstop.
- The JSON lines carry no schema version. The decoder ignores lines and fields it does not know.
- The bridge removes `OPENCODE` and `OPENCODE_PID`, which OpenCode sets for itself, from the environment of a run. Settings such as `OPENCODE_CONFIG` stay.
- OpenCode also sets `AGENT=1` in its own process and every shell it starts. The bridge leaves `AGENT` alone: the name is too generic to remove from every run, and OpenCode sets it again for its own runs anyway. A bridge started from inside an OpenCode shell passes `AGENT=1` on to runs of other harnesses.
- OpenCode 2 can sit beside version 1 and share the `opencode` name; check which one is on PATH.

## Fixtures

The test fixtures in `internal/harness/opencode/testdata` are written from OpenCode's source, `from source, opencode 1.19.3` (`packages/opencode/src/cli/cmd/run.ts` and its tool and error definitions). They have not been recorded from a live run.

# Pi

[Pi](https://github.com/earendil-works/pi) is a small, extensible terminal coding agent. Pick it with `MEAT_MODE_AGENT_HARNESS=pi`.

## Install and sign in

Install `@earendil-works/pi-coding-agent` (Node 22.19 or later) or use the installer from pi.dev, so that `pi` is on the bridge's PATH. Sign in once as the user the bridge runs as, with `/login` in an interactive `pi`, or give the bridge's environment the provider's API key. `pi auth check --provider <name>` says whether a login works. The bridge's check runs `pi --version`.

## What the bridge runs

```
pi --mode rpc --no-extensions [--session-id <session>]
```

in the repo as its working directory. RPC mode takes JSON commands on stdin, one per line. The bridge writes two:

```
{"id":"state","type":"get_state"}
{"id":"prompt","type":"prompt","message":"<prompt>"}
```

RPC mode prints no session header, so the response to `get_state` is where the bridge reads the session id. A note goes in as `{"type":"steer","message":"<note>"}`, which Pi delivers after the current turn's tool calls and before its next model call. A follow-up resumes with `--session-id <id>`, which opens exactly that session; `--session` also accepts partial ids and may offer a fork.

When the turn settles the bridge closes stdin, and Pi shuts down and exits. `--no-extensions` is what keeps that reliable: an extension that does not tear down can hold the process open.

`MEAT_MODE_AGENT_COMMAND` replaces the program and the fixed flags, so it must keep `--mode rpc`: for example `/opt/bin/pi --mode rpc --no-extensions`.

## Limits

| Limit | Enforced |
|---|---|
| Time limit for a run, runs per task, runs at once | by the bridge, as for every harness |
| Spending cap | by the bridge: it sums the cost Pi reports on each reply and stops the run past `MEAT_MODE_AGENT_BUDGET_USD` |
| Notes into a live run | yes, with `steer` |
| Permissions | none: Pi has no approval prompts and runs with the bridge user's rights |

Pi has no turn cap and no time limit of its own. A provider that reports no price, or a subscription login, may report a cost of 0, and then the cap does not fire.

## What the app shows

A run's activity line comes from Pi's `tool_execution_start` events: `bash`, `read`, `edit`, `write`, and `grep`, `find` and `ls` where enabled. A turn ends at `agent_settled`, once Pi has no retry, compaction or queued message left. It counts as failed when the last reply stopped with `error`, `aborted` or `length`, or when a retry gave up; an error that reads as a usage or rate limit also reports the limit. A prompt that Pi refuses, such as when no model is set, ends the turn as failed. Pi exits 0 after a failed model reply, so the end event, not the exit status, carries the failure.

## Things to know

- Pi reads `AGENTS.md`, and falls back to `CLAUDE.md`.
- Pi reads a message that starts with `/` as an extension command, a `/skill:` command or a prompt template, and checks only the first character. The bridge sends such a prompt or note with a leading space, so Pi passes it to the model as written.
- The bridge removes `PI_CODING_AGENT` and `AI_AGENT`, which Pi sets for itself, from the environment of a run. Settings such as `PI_CODING_AGENT_DIR` stay.
- Pi's JSON events changed shape in 0.84.0 (`message_update` carries deltas). The decoder reads only `message_end`, so it does not depend on that.
- Pi's repository is `earendil-works/pi` (formerly `badlogic/pi-mono`). GitHub releases and the npm package agree on the 1.x line: 1.3.0 was the latest when this was written. The 0.84.x line in older notes is the earlier numbering.

## Fixtures

The test fixtures in `internal/harness/pi/testdata` are written from Pi's source and docs, `from source, pi 1.3.0` (`packages/coding-agent`, its RPC mode and docs). They have not been recorded from a live run.

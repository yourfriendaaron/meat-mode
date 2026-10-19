# Claude Code

[Claude Code](https://github.com/anthropics/claude-code) is Anthropic's coding agent, and the harness the bridge runs when `MEAT_MODE_AGENT_HARNESS` is unset or `claude`. It reads standing instructions from `CLAUDE.md` in the repo.

## Install and sign in

Install Claude Code from its README and sign in once by running `claude` in a terminal on the agent machine, as the user the bridge runs as. Have `claude` on your `PATH` when you run `meat-mode bridge pair`, so the service finds it; pair again after installing it. The bridge checks it with `claude --version` at start and on each hello. While that fails, asks on the machine stop as failed and syncing carries on.

## The command

```
claude -p --input-format stream-json --output-format stream-json --verbose --replay-user-messages --permission-mode auto --permission-prompts none --max-budget-usd <cap> [--resume <session id>]
```

The bridge writes the task's prompt to standard input as a stream-json user message and keeps standard input open, so a note you add while the agent works goes in at once as another user message. `--replay-user-messages` echoes each message once the agent has read it, which is how the task counts notes still waiting. When the turn ends with nothing left to send, the bridge closes standard input and Claude Code exits. Asking again on the task resumes its session with `--resume`.

`MEAT_MODE_AGENT_COMMAND` replaces everything before `--max-budget-usd`. Another program set there must speak the same stream-json on its standard input and output (user messages in; `system`, `assistant`, `user` and `result` lines out, with `session_id`), echo each message it reads as a `user` line with `isReplay`, and accept `--max-budget-usd <dollars>` and `--resume <session id>`.

## What the bridge reads

| Line | What the bridge takes from it |
|---|---|
| any line with `session_id` | the session to resume on the next ask |
| `assistant` with a `tool_use` block | the activity line: `Bash` runs a command; `Edit`, `MultiEdit` and `NotebookEdit` edit a file; `Write` writes one; `Read` reads one; `Glob`, `Grep` and `LS` search; `WebFetch` and `WebSearch` search the web; `Task` and `Agent` start a helper; `TodoWrite` plans |
| `user` with `isReplay` | one message read |
| `rate_limit_event` with a status other than `allowed` or `allowed_warning` | the usage limit, with its reset time |
| `result` | the end of the turn: `is_error` fails it, a subtype naming `budget` is the spending cap, and an error whose text reads as a usage limit is one |

## Limits

- **Spending cap**: Claude Code's own. The bridge passes `MEAT_MODE_AGENT_BUDGET_USD` as `--max-budget-usd`, and Claude Code ends the turn when it is spent.
- **Notes**: reach a live run at once.
- **Time limit, runs per task, runs at once**: the bridge's, as for every harness.
- **Permissions**: `--permission-mode auto` with no prompts lets the agent run commands as you without asking. It is not a sandbox; see [Wake an agent on a task](../deploy.md#wake-an-agent-on-a-task).

## Fixtures

The conformance fixtures in `internal/harness/claude/testdata` come from Claude Code 2.1.303: the turn that runs a shell command was recorded live and scrubbed; the failed turn (a usage limit) and the turn that spent its cap are written from the stream-json format.

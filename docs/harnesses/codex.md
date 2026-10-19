# Codex

[Codex](https://github.com/openai/codex) is OpenAI's coding agent. With `MEAT_MODE_AGENT_HARNESS=codex`, Ask agent runs it on the bridge's machine through `codex exec`, its non-interactive mode.

## Install

Install Codex from its README (the install script, npm or Homebrew), then sign in once with `codex login`, using a ChatGPT account or an API key. `codex login status` should say you are logged in. The bridge looks for `codex` on PATH, and runs `codex --version` to show the version in Settings.

Codex reads standing instructions from `AGENTS.md`, so the instruction text for agents on that machine names `AGENTS.md`.

## The command

A new run:

```
codex exec --json --sandbox workspace-write --skip-git-repo-check -C <repo> -- <prompt>
```

A resume, for the next ask on the task or for notes held during a turn:

```
codex exec --json --sandbox workspace-write --skip-git-repo-check -C <repo> resume <thread id> -- <prompt>
```

- `--json` prints one event per line. The bridge reads the thread id from `thread.started`, tool calls from `item.started` (`command_execution`, `mcp_tool_call`, `web_search`, `todo_list`, a helper agent's `collab_tool_call`) and from `file_change` items, which only come as completed, and the turn's end from `turn.completed` or `turn.failed`.
- `--sandbox workspace-write` lets the agent edit files in the repo and run commands inside Codex's sandbox. Network access is off in that sandbox. An action the sandbox blocks is declined, since no one is there to approve it.
- `-C` and the sandbox go before `resume`, which accepts neither after it. Without `-C`, a resume runs in the folder the session was saved in ([openai/codex#40702](https://github.com/openai/codex/issues/40702)). Given before `resume`, the sandbox carries over to the resumed turn ([openai/codex#40149](https://github.com/openai/codex/issues/40149) reports a resume that lost it when the flag came after).
- `--skip-git-repo-check` because the bridge has already picked the repo. Without it, Codex refuses a folder that is not a Git repo.
- The prompt comes after `--`, so a prompt that starts with a dash is not read as a flag. The bridge closes stdin after starting Codex; Codex reads it once and otherwise waits for it to close.

`MEAT_MODE_AGENT_COMMAND` replaces `codex exec --json --sandbox workspace-write --skip-git-repo-check`. The bridge still adds `-C`, the resume and the prompt, so a replacement must end where those can follow, such as `codex exec --json --dangerously-bypass-approvals-and-sandbox` on a machine that is already isolated.

## Limits

- **No spending cap.** Codex reports tokens, not money, and has no cap of its own. A run stops at the time limit (`MEAT_MODE_AGENT_TIMEOUT`) only, and `MEAT_MODE_AGENT_BUDGET_USD` is ignored. The app says so.
- **A note waits for the turn.** `codex exec` reads its prompt once and takes nothing into a live run. A note sent while the agent works is held; when the turn ends well, the bridge resumes the session with the held notes as one prompt, in the same run.
- **Usage limit.** When Codex reports that you have hit your usage limit, the run ends and the app shows when it resets, read from Codex's own message in the machine's time zone.
- The bridge's time limit and its stop (SIGTERM, then SIGKILL) are the only guard against a stalled turn: Codex has no idle timeout of its own.

## Things to know

- A turn that stalls on the model's stream can hang with no output until the time limit ([openai/codex#50775](https://github.com/openai/codex/issues/50775)).
- An interrupted turn can end without `turn.completed` or `turn.failed` ([#50585](https://github.com/openai/codex/issues/50585)). The bridge counts a process that exits without a turn end as a failed run.
- An `error` line is not the end of a turn: Codex also prints one when it is about to retry a dropped stream ("Reconnecting... 1/5"). Only `turn.failed` ends a turn as failed.
- Some tool calls never reach `--json` ([#54782](https://github.com/openai/codex/issues/54782)), so the activity line can miss them.
- Reading and searching files run as shell commands, so they show as commands, not as reads.
- Codex sets `CODEX_THREAD_ID`, `CODEX_SESSION_ID`, `CODEX_TOOL_CALL_ID`, `CODEX_VERSION`, `CODEX_PERMISSION_PROFILE`, `CODEX_CI`, `CODEX_SANDBOX` and `CODEX_SANDBOX_NETWORK_DISABLED` in the shells it runs. A bridge started from inside a Codex session passes none of them to the runs it starts. `CODEX_HOME` and API keys are your own settings and pass through.

## Version

Built and tested against Codex 0.163.0. The shell-command and file-edit fixtures are live runs of 0.163.0, scrubbed. The usage-limit fixture, and one that covers the other item types (to-do list, web search, MCP call, a reconnect notice, an added file), are written from its source, since a limit cannot be reached on demand.

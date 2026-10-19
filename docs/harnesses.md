# Harnesses

Ask agent starts a coding agent on a task, on the machine the bridge runs on. The program that runs the agent is its harness: Claude Code, Codex and the like. Each harness is a Go package under `internal/harness/<name>` that implements `harness.Harness` and registers itself. The bridge, the hub and the app know a harness only through what it declares, so adding one changes nothing outside its own package, its docs page and one import line.

Supported harnesses, each with its own page:

| Harness | Page | `MEAT_MODE_AGENT_HARNESS` | Spending cap | A note reaches a live run |
|---|---|---|---|---|
| [Claude Code](https://github.com/anthropics/claude-code) | [harnesses/claude.md](harnesses/claude.md) | `claude` (the default) | its own | at once |
| [Codex](https://github.com/openai/codex) | [harnesses/codex.md](harnesses/codex.md) | `codex` | none | after the turn |
| [OpenCode](https://github.com/anomalyco/opencode) | [harnesses/opencode.md](harnesses/opencode.md) | `opencode` | the bridge's | after the turn |
| [Antigravity CLI](https://antigravity.google/docs/cli/overview/) | [harnesses/antigravity.md](harnesses/antigravity.md) | `antigravity` | none | at once |
| [Pi](https://github.com/earendil-works/pi) | [harnesses/pi.md](harnesses/pi.md) | `pi` | the bridge's | at once |

Why these, and why not Gemini CLI yet: [research/0005-harnesses](research/0005-harnesses/RESEARCH.md).

## Add a harness

1. Make a package `internal/harness/<name>` with a type that implements `harness.Harness` (`internal/harness/harness.go`).
2. In its `init`, call `harness.Register(harness.Factory{Name: "<name>", New: ...})`. `New` gets a `harness.Env`: `Command`, from `MEAT_MODE_AGENT_COMMAND`, which replaces the harness's program and fixed flags when set; `Getenv`; and `LookPath`. `New` must not fail because the program is missing; `Check` reports that.
3. Add `_ "github.com/yourfriendaaron/meat-mode/internal/harness/<name>"` to `internal/harness/all/all.go`.
4. Run the conformance suite from a test in the package: `harnesstest.Run(t, "<name>", fixtures)`, and `harnesstest.Live(t, "<name>")` from another test.
5. Write `docs/harnesses/<name>.md`: what to install and sign in to, the command the bridge runs, which limits it enforces and which it lacks, and the version the fixtures come from.

## What a harness declares

`Info()` is fixed for the harness's life:

- `Name`: the registry key, lower case, and the value of `MEAT_MODE_AGENT_HARNESS` that picks it. `Title`: the name the app shows.
- `InstructionFile`: the file in a repo the harness reads standing instructions from, such as `AGENTS.md`. The app names it in the instruction text for agents on that machine.
- `Budget`: how a run's spending cap is enforced.
  - `native`: the harness stops itself at the cap the bridge passes it.
  - `metered`: the harness reports cost as it goes (`Event.CostUSD`) and the bridge stops the run when the sum passes the cap.
  - `none`: the harness reports no cost, and a run stops at the time limit only. The app says so.
- `Steer`: true when a note can go into a live run on stdin (`Note`). Otherwise the bridge holds notes until the turn ends; see Notes.
- `EnvMarkers`: the variables the harness sets in its own sessions, such as `CODEX_THREAD_ID`. A name ending in `*` matches a prefix. A run the bridge starts inherits none of them, from any registered harness, so a bridge started from inside one harness's session never runs another that thinks it is nested.

## Methods

- `Check(ctx)` returns the version of the harness's program, or an error when it is missing or too old. The bridge runs it at start and on each hello. A failed check leaves agents unavailable on that machine and is logged; syncing carries on.
- `Launch(spec)` returns the process for one run or resume: `Args` (the program first, no shell), `Stdin` (written first, such as a stream-json prompt line), and `KeepStdin`, which must equal `Steer`. When `KeepStdin` is false the bridge closes stdin after `Stdin`; some harnesses wait for that before they start. `spec` carries the repo `Dir` (also the working directory), the `Prompt`, the `Session` to resume (empty for a new one; only ids `harness.ValidSession` accepts) and `BudgetUSD`, which a native harness passes on.
- `Note(text)` returns the stdin bytes that put a note into a live run, ending in a newline. The bridge calls it only when `Steer` is true.
- `NewDecoder()` returns a decoder for one process's stdout. `Line(raw)` turns one line into events and returns nil for any line it does not know, malformed ones included, without panicking.

### Events

Each event sets one field, except that `Session` may come with any other:

- `Session`: the harness's session id, whenever a line names it. The bridge stores the newest one that passes `ValidSession` and resumes it on the task's next ask.
- `Tool`: a tool call the agent started, mapped onto a kind: `shell` (with `Command`), `edit`, `write`, `read`, `search`, `web`, `agent`, `plan` or `other`, with `File` for one that names a file. The bridge turns it into the activity line ("Running go test", "Editing notes.txt", "Closing the task" for the tracker's own commands) and shows no more than a command's first word or two and a file's base name.
- `Read`: how many more of the messages the bridge wrote to stdin the harness has taken in with this line, such as 1 for each message it echoes; the bridge adds it up. When a harness never does, a turn's end counts every message sent before it as read.
- `CostUSD`: spending a line adds, for a metered harness only.
- `Limit`: the agent hit its usage limit, with `ResetsAt` when known.
- `End`: the turn is over, with `Error` when it failed, `Budget` when a native harness stopped at its cap, and `Text`, its final message or error.

## How the bridge runs a harness

One harness per machine, picked by `MEAT_MODE_AGENT_HARNESS`. The run limits hold for every harness: the time limit for one run, runs per task and runs at once on the machine. A run past its time limit, or a metered run past its cap, gets SIGTERM, then SIGKILL after a grace time, as a stopped run does.

- **A run** is one Ask: from the first launch to the end of the agent's last turn. It keeps one run id and counts once toward runs per task.
- **The end of a turn** with nothing left to send closes stdin, and the process exits. A process that exits without an `End` is a run that failed, with its exit status as the detail.
- **Notes.** With `Steer`, a note goes to stdin at once, and `Run.Queued` counts the notes not yet read. Without it, notes wait: `Run.Queued` counts them, and when the turn ends well the bridge resumes the session in a new process, in the same run, with the held notes joined into one prompt (each as the tracker's `FollowUp` gives it). The time limit still runs from the run's start. After a turn that failed, hit a limit or spent its budget, held notes stay on the task for the next ask, as they do with `Steer`.
- **Sessions** are recorded in the bridge's `runs.json` with the harness's name. A session recorded under another harness is never resumed; the next ask starts fresh.

## Settings

- `MEAT_MODE_AGENT_HARNESS`: the harness's `Name`; `claude` when unset. An unknown name stops the bridge at start with the list of names.
- `MEAT_MODE_AGENT_COMMAND`: the program and its fixed flags, split on spaces with no shell, replacing the harness's own. The harness still adds what each run needs (the prompt, the session, the cap). An existing setting written for Claude Code keeps working unchanged.
- `MEAT_MODE_AGENT_BUDGET_USD`, `_TIMEOUT`, `_RUNS_PER_TASK`, `_CONCURRENT` and `_NAME` apply to every harness; the budget is ignored by a harness whose `Budget` is `none`.

## Conformance

`harnesstest.Run` checks a harness against this page: what it declares, its launch for a new run, a resume and a malformed session id, the command override, its env markers, a decoder fed junk, and fixtures. A fixture is one captured stdout in the package's `testdata`, named `<name>-<version>-<case>.jsonl`, with what the decoder must find: the session id, tool calls in order, the summed cost for a metered harness, a usage limit, and the turn end. Each fixture names its `Source`: `live, <program> <version>` when recorded from a real run, or `from source, <program> <version>` when written from the harness's source or docs, to be recorded again when someone can. Every harness needs at least a turn that ran a shell command and ended well, and a turn that failed.

`harnesstest.Live` runs the installed harness for real, a new run and then a resume, only when `MEAT_MODE_HARNESS_LIVE=1`. It calls the model and costs usage, so `scripts/check.sh` and CI never set it.

## On the wire

`api.Hello.harness` is `{name, title, version, instructionFile, budget, budgetUsd, timeoutSeconds, steer}` for the machine's harness (`docs/api.md`, Bridge API). The hub keeps it with the bridge and the app shows it in Settings and the Agents tab: which harness the machine runs, its time limit, its spending cap or that it has none, and whether notes wait for the turn. The instruction text for agents names its `instructionFile`. A bridge that sends no `harness` predates harnesses and runs Claude Code.

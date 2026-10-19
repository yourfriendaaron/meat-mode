# Trackers

The bridge syncs tasks for the person from the task trackers that agents use in a repo. Each tracker is a Go package under `internal/tracker/<name>` that implements `tracker.Tracker` and registers itself. The bridge, the hub and the app know a tracker only through what it declares, so adding one changes nothing outside its own package, its docs page and one import line.

Supported trackers, each with its own page:

| Tracker | Page | Marks a task for the person with |
|---|---|---|
| [beads](https://github.com/gastownhall/beads) | [trackers/beads.md](trackers/beads.md) | the label `human` |
| [Backlog.md](https://github.com/MrLesk/Backlog.md) | [trackers/backlog.md](trackers/backlog.md) | the label `human` |
| [ticket](https://github.com/wedow/ticket) | [trackers/ticket.md](trackers/ticket.md) | the tag `human` |

Why these three, and why not Tasks.md or Task Master yet: [research/0004-trackers](research/0004-trackers/RESEARCH.md).

## Add a tracker

1. Make a package `internal/tracker/<name>` with a type that implements `tracker.Tracker` (`internal/tracker/tracker.go`), and `tracker.Agent` when it lists `ask`.
2. In its `init`, call `tracker.Register(tracker.Factory{Name: "<name>", New: ...})`. `New` gets a `tracker.Env`: the path of the tracker's tool from the bridge config (`Bin`, empty to look it up on PATH), `Getenv` and `LookPath`.
3. Add `_ "github.com/yourfriendaaron/meat-mode/internal/tracker/<name>"` to `internal/tracker/all/all.go`.
4. Run the conformance suite from a test in the package: `trackertest.Run(t, "<name>", setup)`, where `setup` makes a fresh repo of invented tasks in a temp dir and skips when the tool is not installed.
5. Write `docs/trackers/<name>.md`: what to install, how a task is marked for the person, how a repo opts in, which app actions it supports and what each one writes.

## What a tracker declares

`Info()` is fixed for the tracker's life:

- `Name`: the registry key, lower case. `Title`: the name the app shows.
- `Rank`: when two trackers detect the same folder, the lowest rank syncs it and the other is skipped with a log line. beads is 0; others take 10, 20 and so on.
- `Prune`: folder names the bridge's repo walk never enters, such as the tracker's own store.
- `Actions`: the capabilities it supports, from `tracker.Capabilities`: `done`, `reopen`, `snooze`, `dismiss`, `note`, `add`, `subtask` (an add under a parent) and `ask`. Every tracker supports `add`. `ask` needs `tracker.Agent` as well, and `tracker.Validate` checks both.

## Methods

Every method that takes `dir` gets the repo folder that `Detect` matched. A method for a capability the tracker does not list returns `tracker.ErrUnsupported`.

- `Check(ctx)` returns the version of the tool it drives, or an error when that tool is missing or too old. A tracker that drives no tool returns `""`. The bridge runs every registered tracker's check at start and on each hello. A tracker whose check fails is left out (its repos are not found) and logged; the bridge refuses to start only when every check fails.
- `Detect(dir, entries)` reports whether `dir` holds the tracker's store. The walk calls it for every folder, with the folder's entries already read, so it stays cheap.
- `OptedIn(dir)` reports whether the repo's own files switch syncing on. Every tracker honours `tracker.RepoOptIn`: a repo-root `.meat-mode.yaml` with `sync: true`. beads also reads `meat-mode.sync: true` in `.beads/config.yaml`. The bridge never writes either file; the app's sync switch, kept in the bridge's own config, overrides both.
- `List(ctx, dir)` returns the tasks for the person: open, in-progress, blocked and deferred ones, and closed ones from the last 7 days (`tracker.InWindow`). It leaves `Comments` empty and fills `Version`, which changes whenever anything the hub shows changes, a new comment included. The bridge reads a task again with `Show` only when its version changed. A task missing from the list is gone from the hub; a tracker must therefore return the whole set or an error, never a partial list.
- `Show(ctx, dir, id)` returns one task with its comments, or an error wrapping `tracker.ErrNotFound`.
- `Close`, `Reopen`, `Dismiss`: `Dismiss` closes the task as not needed. `Close` of a parent with an open sub-task may refuse with an error wrapping `tracker.ErrOpenChildren`.
- `Defer(ctx, dir, id, until, actor)` hides the task until a time, or clears that when `until` is nil.
- `AddComment(ctx, dir, id, author, text)`: `Show` must give the comment back with the same author and text, since the replay journal looks for a note it may already have written by both.
- `Create(ctx, dir, NewIssue)` makes a task for the person, marked as the tracker marks them, under `Parent` when set, and returns its id. It stores `ActionID` where `FindByActionID` finds it again, so an add the bridge retries after a crash is not made twice.

`Issue.Status` is one of `open`, `in_progress`, `blocked`, `deferred` and `closed`; each tracker maps its own states onto these. `Priority` is 0 (highest) to 4.

### Agents

A tracker that lists `ask` implements `tracker.Agent`:

- `Prompt(id)` is the first message of a run: where the task and its notes are, how to close it and how to reply with a note, using the tracker's own commands.
- `FollowUp(id, text)` is the message for a note the person adds to a live run.
- `Env(actor)` is the variables the agent gets so its writes carry its name, such as `BEADS_ACTOR=<actor>`. The bridge drops any inherited variable of the same name.
- `Describe(words)` names one of the tracker's commands in plain words for the run's activity line, such as "Closing the task", or returns `""` when the command is not the tracker's.

## On the wire

The bridge reports each repo with its tracker (`docs/api.md`, Bridge API):

- `api.Repo.tracker`: the tracker's `Title`.
- `api.Repo.actions`: its `Actions`. Absent means every kind, which is what an older bridge and the hub's own lists send.

The app hides the actions a repo's tracker cannot apply. The hub refuses them with `400` and the message from `tracker.Refusal`, such as "ticket cannot snooze", and the bridge refuses one that still arrives with a failed result carrying the same message, so an old app or a queued action never wedges.

`api.Hello.trackers` lists `{name, version}` for each tracker whose check passed; the hub shows them for each machine in Settings. `bdVersion` stays, filled with beads' version when beads is available, for hubs that predate trackers.

## Settings

A tracker's tool is found on PATH, or at the path the bridge config's `trackerBins` gives for its name (`{"trackerBins": {"backlog": "/opt/backlog/bin/backlog"}}`). The older `bd` key still sets beads' path.

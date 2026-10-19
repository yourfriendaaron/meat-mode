# Research: trackers beyond beads

The question: how the bridge should sync agent task trackers other than beads, which trackers to support first, and where repos with no tracker keep the tasks their agents raise. Reader notes: `landscape.md` (prior art, Backlog.md), `ticket.md`, `tasksmd.md`, `taskmaster.md`; brief: `brief.md`. Grades: A controlled or large n, B one team or small n, C anecdote, V vendor guidance.

## Findings

1. **Popularity** (GitHub, 2026-10-19, B): beads 28.0k stars, active; Task Master 28.2k, last push 2026-04-28; Backlog.md 7.1k, active, release 2026-06-17; Tasks.md 2.2k, last push 2026-03-08; ticket 0.9k, last release 2026-02-03.
2. **Sync tools share four pieces**: a unique key per foreign item, a status map onto a small local set, a bag for fields one side lacks, and capabilities declared per tracker (bugwarrior, git-bug, todo.txt tools; V and B). None solves an action the tracker refuses beyond a flag or a log line; git-bug's Jira bridge leaves the status out of sync (V). (`landscape.md` 1)
3. **Never close by absence.** bugwarrior closes a local task when a poll stops returning it, which closes tasks wrongly after a partial list (C). Meat Mode already drops an item from the hub only when the bridge's full set for the repo leaves it out. (`landscape.md` 1)
4. **Backlog.md fits.** Stable ids (`TASK-12`, sub-tasks `TASK-12.1`), YAML frontmatter with status, labels, assignee, `due_date` and `parent_task_id`, comments in marked body sections, and a CLI with "stable, versioned JSON" on list and view. Its README says to prefer the CLI over editing files. It locks id allocation (PR #565). No defer or snooze field (V, B). (`landscape.md` 2)
5. **ticket fits.** One file per ticket at `.tickets/<id>.md`, the id never changes, close and reopen rewrite only the `status:` line, notes are appended under `## Notes`, `tags` and `parent` are set at create. No due date, no defer, no dismiss status, no author on notes. Only `tk query` prints JSON, and only frontmatter. Writes go through a temp file and `mv`, with no lock (B, source read in full). (`ticket.md`)
6. **Tasks.md does not fit.** No ids (a card is its lane folder plus its file name, and the web app renames and moves files), no comments (declined, #141), no repo marker, and the web app overwrites outside edits with its open copy (#142) (B, C). (`tasksmd.md`)
7. **Task Master does not fit today.** One `tasks.json` of tagged lists, rewritten whole on every change; integer ids unique per tag and reused after a delete; no comments, and the only note commands call an AI provider; open data-loss bugs (#1636 wiped every tag but one, #1708 rewrites ids as strings) (B, C). (`taskmaster.md`)

## Decisions (Inference)

- **Build beads, Backlog.md and ticket; defer Tasks.md and Task Master.** The two deferred ones would each need Meat Mode to invent an id or note store inside a format it does not own (findings 6, 7).
- **One `Tracker` interface, with each tracker in its own package that registers itself.** The bridge, hub and app read what a tracker can do as data: the bridge reports each repo's tracker name and the action kinds it supports; the app hides the rest; the bridge refuses them anyway (finding 2). Adding a tracker is a new package plus one import line. The contract is `docs/trackers.md`.
- **Drive a tracker's own CLI where it has one; edit files only where it does not.** Backlog.md writes go through `backlog`; ticket reads parse the files and writes go through `tk` where `tk` has the command (findings 4, 5).
- **Each tracker owns change detection.** `List` returns a version per item (beads: the hash of its listed fields; a file tracker: a hash of the file).
- **Opt-in stays read-only for the bridge.** beads keeps `meat-mode.sync: true` in `.beads/config.yaml`; every tracker also honours a repo-root `.meat-mode.yaml` with `sync: true`. The app's sync switch still overrides both.
- **Hub-side projects extend the personal list, not the tracker interface.** The store already treats the personal list as repo `personal` of the reserved bridge `hub`; a project is another repo of that bridge, reached through the bridge API with a bridge token, and needs no bridge to run. The tracker interface is for stores a bridge polls on the agent machine.

## Call

**Proceed**, with the tracker set changed from Tasks.md and ticket to Backlog.md and ticket. Tasks.md and Task Master are deferred and raised with the operator.

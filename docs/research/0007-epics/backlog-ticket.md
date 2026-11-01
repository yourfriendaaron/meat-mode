# Epics in Backlog.md and ticket

Cluster: Backlog.md (`backlog` 1.55.3 on PATH) and ticket (`tk`, tag v0.3.2, released 2026-02-03; default branch last commit 2026-03-15). Questions Q1 (exposure and cost), Q2 (how viewers read), Q3 (failures) and Q4 (progress). Examples below are invented. Nothing here repeats docs/trackers/backlog.md or docs/trackers/ticket.md.

Evidence grades: A controlled/large n, B single team or small n or first-hand test, C anecdote, V vendor guidance. "Run here" means I ran it in a scratch repo on this machine (grade B, n=1 machine).

## Backlog.md

### Q1 Epic and children model

- No epic type. A task has `parentTaskId` (front matter `parent_task_id`) and an optional `type`. Source: https://github.com/MrLesk/Backlog.md/blob/main/src/types/index.ts (`parentTaskId?`, `subtasks?: string[]`, `type?: string`, comment "Allowed values come from config `types` (defaults to DEFAULT_TASK_TYPES); absent means untyped"). Read via GitHub API, highlights of the type file. Grade V. Q1.
- Default types: `["bug","feature","enhancement","task","chore","docs","spike"]`, from src/constants/index.ts (search hit, highlight only). `epic` is not in the list, but `types` is configurable per project, so a project can add `epic`. Grade V. Q1. **Inference**: most repos will have `type: null`, so type cannot be what marks an epic.
- Run here: `backlog task create "Child" --parent 1` makes id `TASK-1.1`; `--parent 1.1` makes `TASK-1.1.1`. So children nest to any depth, and the dotted id carries the path. The file name is `task-1.1.1 - Grand.md`. Grade B.
- Run here: `--parent` needs the parent to exist locally. Error text: "Parent task TASK-1 not found. Task lookups read only the local working copy". Grade B.
- Milestones are a separate grouping (`-m "v1"`, `backlog milestone list`), one milestone per task, field `milestone` in JSON. A milestone is not a parent task and has no id of its own in the task list. Run here. Grade B. **Inference**: milestone is a second candidate for "epic", but a card per milestone would be a different feature.
- Dependencies are a separate `dependencies:` list. 1.51.0 release notes (https://newreleases.io/project/github/MrLesk/Backlog.md/release/v1.51.0, highlights only, grade V): "every task detail now shows what it depends on and what depends on it, direct and transitive". Dependencies do not express parenthood.
- `isReady` in list JSON is false when a dependency is unfinished (run here: a Done child shows `isReady: false`, so it is not "ready to start", not "blocked"). Do not read it as progress.

### Q2 One cheap call

- `backlog task list --json` returns `{schemaVersion:1, kind:"task-list", tasks:[...]}`. Each task has `id`, `title`, `status`, `archived`, `type`, `priority`, `assignees` (array), `labels`, `milestone`, `parentTaskId`, `acceptanceCriteriaCompleted/Count`, `dueDate`, `isReady`, `createdAt`, `updatedAt`. Run here. Grade B. This covers every field the epics view asks for in one call, including assignee. The adapter already calls this (internal/tracker/backlog/tasks.go line 215), and its `taskJSON` already has `ParentID`.
- `--plain` gives only grouped id and title lines, with `[HIGH]` for priority and no parent or assignee. Not usable. Run here. Grade B.
- Cost, run here: 700 generated task files (612 with a parent), `task list --json`: 0.18 s wall, 398 KB; `--plain`: 0.16 s. Grade B (one machine, warm cache, no git remote, so no cross-branch fetch). At 1,700 total across several repos this is under a second of CPU per sync round.
- The local list reads the working copy only. By default `backlog` can also load tasks from other branches and remotes (the 1.51 change "Task-ID allocation no longer joins an in-flight stale fetch" shows fetch paths exist); the existing doc says a Git remote makes `task create` fetch with a 10 s cap. I did not test a repo with a remote. Gap.
- Archived tasks and tasks in `backlog/completed/` are not in the default list. Whether `completed/` tasks count as closed children is untested. Gap. **Inference**: if a user runs `backlog cleanup`, closed children move to `completed/` and vanish from the list, so "6/7 done" would shrink to "0/1". Needs a test before the view ships.
- `task list` filters help: `-p/--parent <id>`, `--type`, `--milestone`, `--status` (repeatable), `--limit`. Source: `backlog task list --help`, run here. Grade B.
- Direct file read is possible: one markdown file per task with YAML front matter (`id`, `title`, `status`, `assignee`, `labels`, `dependencies`, `parent_task_id`). Run here. But the CLI is already 0.2 s for 700, so there is no cost reason to parse files, and the front matter keys are Backlog.md's to change (see Q3).

### Q3 Failure modes seen

- Search for reports of viewers failing on Backlog.md: none found. Searched Exa for Backlog.md viewers; hits were Backlog.md's own PRs and a Hacker News thread (https://news.ycombinator.com/item?id=44483530, 2025-07-06, highlights only, grade C) that asks for dependencies, no failure report.
- Schema churn is real but versioned. `schemaVersion: 1` is in the JSON envelope (run here). 1.51.0 added fields (`isReady`, `project`, `modifiedFiles`, `dueDate`) to `--json` ("Readiness is computed once in core... task lists carry an `isReady` field in `--json`"; "Modified files... included with references in `task list --json`"). URL as above, highlights only. Grade V. **Inference**: new fields are additive so far; a tolerant parser (the adapter's) survives, a strict one would not.
- Sorting bug: "Subtasks sort numerically... `1.10` was sorted before `1.2` because one code path compared IDs as strings" (v1.51.0 notes, #985). Grade V. **Inference**: do not sort children by id string; compare dotted segments as numbers, or keep the CLI's `ordinal` order.
- Locks: no lock file seen in the scratch repo (`backlog/` held only tasks and config). Agents writing at the same moment as a read can leave a half-written file; I found no report either way. Gap.

### Q4 Progress

- Backlog.md's own UI: web hierarchy modal shipped in 1.51.0 ("Parent and subtask hierarchy in the web task modal (#917...)"). The board export groups "grandchild subtasks" (#970). Grade V, highlights only.
- A task still open in the project's own backlog states the intended rule: BACK-222, "Parent progress summarizes completed children using the project's configured terminal-status semantics, while tasks with no children retain an uncluttered no-child state" and "tasks with nested descendants remain discoverable". Source: https://github.com/MrLesk/Backlog.md/blob/main/backlog/tasks/back-222%20-%20Improve-task-and-subtask-visualization-in-web-UI.md (read in full, front matter updated 2026-07-16, status To Do). Grade V. So as of that date the browser shows no parent progress, and the intended rule is "children in a terminal status over children", with terminal meaning the project's configured last column. This matches how the adapter already maps the last column to done.
- BACK-352 (completed): plain output lists subtasks "derived from tasks whose `parent_task_id` matches the viewed task ID" (search highlight, grade V). So the tool itself derives children by scanning for matching parent, not from a stored list.
- Tombstones: Backlog.md has no tombstone. Archive moves the file out of the list. Deferred has no meaning; a status not on the board is treated as open by this repo's adapter. Whether a custom status like "Deferred" should count in the denominator is a Meat Mode choice.

### Q5 Recommendation (Backlog.md) **Inference**

Support epics. Map: epic = any task that is the `parentTaskId` of at least one other task, top-level (no parent). Children = direct children, shown nested by dotted id depth when deeper. Progress = direct children done over direct children, plus an optional recursive count; done = last board column (the same rule the adapter uses). Archived children are excluded from both sides. A top-level task with no children is a "top-level task". Use `type == "epic"` only as an extra hint when the project configures it. Reuse the existing `task list --json` call: no new command, no file parsing. Cost: nothing extra per sync. Risks: `completed/` folder (untested) and a childless epic that was only ever planned.

## ticket

### Q1 Epic and children model

- Explicit type: `-t, --type  Type (bug|feature|task|epic|chore) [default: task]` and `--parent  Parent ticket ID`. Source: help text and `cmd_create` in https://raw.githubusercontent.com/wedow/ticket/master/ticket (read the create code and help in full; fetched 2026-11-01). Grade V. The front matter written is `id, status, deps, links, created, type, priority, assignee?, external-ref?, parent?, tags?`.
- `--parent` is validated at create: the parent file must exist (`ticket_path "$parent" || return 1`). Nothing stops `parent:` pointing at a non-epic, or a child having its own parent, so nesting is unlimited. Nothing in `tk` enforces `type: epic` on a parent. Grade V. **Inference**: use `parent`, not `type`, to find children.
- `deps` (blocking, `tk dep a b`) and `links` (symmetric, `tk link`) are separate from `parent`. Only `parent` means child-of. Grade V.
- `status` is one of `open`, `in_progress`, `closed` (`VALID_STATUSES`). The adapter also reads `done`. Priority is 0-4, 0 highest, default 2. Assignee is a free string.

### Q2 One cheap call

- `tk` itself has no list-all command in the core script (commands: `ready`, `blocked`, `closed --limit=N` default 20, `show`, `dep tree`). Verified by grepping the 1,383-line script. Grade V.
- `tk query` is a plugin, not core: plugins/ticket-query ("Output tickets as JSON, optionally filtered with jq"), v1.0.0, source https://github.com/wedow/ticket/tree/master/plugins (read the first 50 lines). It makes one awk pass over `.tickets/*.md` and prints one JSON object per line, with every front matter key as a string and `[a, b]` arrays split into JSON arrays. Grade V. Caveats: it needs `jq` only when a filter is given; plugin availability depends on how `tk` was installed (Homebrew installs `ticket`; I did not check that it ships the plugins). Not run here: the sandbox refused to execute downloaded code. Cost is therefore unmeasured. **Inference**: one awk pass over 700 small files is cheap, order of tens of ms, but that is not measured.
- Direct file read is the safer route and is what the adapter already does (internal/tracker/ticket/parse.go reads `fields["parent"]` and the title heading). The bridge then needs no plugin and works with any `tk`. `type`, `priority`, `assignee`, `parent` and `status` are all front matter keys at column 0, so one pass over the folder gives every field the epics view needs. 700 files is 700 small reads, well under a second on local disk (**Inference**, not timed).
- Status of the ticket itself: ticket stores no close time (existing doc), so "closed this week" cannot come from the tool.

### Q3 Failure modes seen

- No viewer failure reports found. Searched for ticket viewers and the repo's issues; the only viewer is `tk` itself.
- `cmd_show` computes children with a scan of `"$TICKETS_DIR"/*.md` and prints `- id [status] title` lines under a children heading (source lines about 1083-1180). Grade V. It scans every file per `show`, so it is O(n) per call: fine for one ticket, not for one call per epic. Reading the folder once is the right shape.
- Writes are not atomic or locked: existing doc says "two writers at the same moment can lose one change", and `tk close` rewrites with awk. **Inference**: a read can see a half-written file; skip a file whose front matter does not parse and retry next poll.
- Schema churn: front matter is the whole contract; it has been stable across v0.3.x as far as the source shows, and `type`, `parent` are in the current create code. I did not diff earlier tags. Gap.

### Q4 Progress

- ticket's own UI: none. `tk show <epic>` lists children with their status, `tk dep tree` shows dependencies only. No percentage anywhere. Grade V.
- **Inference**: progress = direct children with status `closed` (or `done`) over direct children; a recursive count is easy because `parent` chains are in the same pass. There is no tombstone or deferred state in ticket, so no special case.

### Recommendation (ticket) **Inference**

Support epics, from file reads in the same pass the adapter already makes. Epic = a ticket that is the `parent` of at least one other, or has `type: epic`. Children = tickets whose `parent` equals the id. Show an epic with no children as an epic card with 0/0. Parent ids that point at no file: show the child as top-level. Prefer file reads over `tk query` so the bridge does not depend on a plugin.

## Common shape across both (Inference)

`{id, status, priority, title, type, parent, assignee}` per item, build the tree from `parent`, an epic is any item with children (or type epic), progress counts direct children whose status is done over all direct children, and the card also carries the recursive count. Both tools hold this in the one list call or folder read the bridge already does. Nothing needs a second call per epic.

## Gaps

- Backlog.md on a repo with a Git remote and cross-branch tasks, and the `completed/` folder, were not tested.
- ticket plugin timing was not measured (the sandbox refused to run downloaded code); sizes are estimates.
- No third-party viewer for either tool found, so Q2 and Q3 evidence for these two is the tools' own code and release notes only.

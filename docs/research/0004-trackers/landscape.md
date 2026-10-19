# Landscape: multi-tracker sync prior art and agent-facing trackers

Reader: landscape cluster of the 0004 trackers brief. Searched 2026-10-19 with Exa search and fetch plus `curl` of raw GitHub files. Question numbers (Q1-Q4) are the brief's. Grades: A controlled/large n, B single team or small n, C anecdote, V vendor guidance. "Full" means read in full, "HL" means highlights only.

## 1. Interface shapes used by multi-tracker sync tools

### bugwarrior (taskwarrior pull adapters)

- **Pull only, no write-back.** FAQ: "Bugwarrior takes remote sources as the authoritative source of truth and synchronizes your local taskwarrior database to align with the remote. Data from taskwarrior is never pushed to remotes." https://bugwarrior.readthedocs.io/en/latest/faq.html (undated, docs 2.1.0). Q1. Grade V. HL.
- **Service interface** (source read in full): https://raw.githubusercontent.com/GothenburgBitFactory/bugwarrior/develop/bugwarrior/services/__init__.py . Q1. Grade V. Full.
  - `Service` (abstract): `API_VERSION`, `ISSUE_CLASS`, `CONFIG_SCHEMA`, one abstract generator `issues()`, `get_keyring_service()`. The only operation is "list issues".
  - `Issue` (abstract): `to_taskwarrior()` and `get_default_description()`, plus class constants `UDAS` (extra fields the target lacks, each with type and label), `UNIQUE_KEY` ("field names ... usable for uniquely identifying an issue in the foreign system") and `PRIORITY_MAP` (foreign value to H/M/L).
  - Version contract: "MAJOR versions signal a breakage in backwards compatibility between services", `LATEST_API_VERSION = 1.0`. A service declaring a newer major is rejected at construction.
- **Fields a service lacks or taskwarrior lacks.** Foreign-only fields become UDAs namespaced per service (example in the docs: `githubnumber`, `githubtitle`). Missing priority falls back to `default_priority` (default `M`). Missing values are simply absent. Q1. Grade V. Full (docs page https://bugwarrior.readthedocs.io/en/latest/common_configuration.html).
- **Local edits protected by config, not by merge.** `static_fields` ("attributes that shouldn't be updated by bugwarrior... Use for values that you want to tune manually"), default `["priority"]`; `merge_tags`, `replace_tags`, `static_tags`, `merge_annotations`. Same URL. Q1/Q4. Grade V. Full.
- **Close by absence.** Bugwarrior closes a local task when the remote stops returning it. A maintainer notes that fetching one bug outside config "would in fact close it on the next `bugwarrior pull` because it wouldn't be returned". https://github.com/GothenburgBitFactory/bugwarrior/issues/946 (updated 2026-03-15). Q4. Grade C. HL.
- **Closed state needs its own field.** Issue #620 (2018-11-21): tasks closed by absence got today's end date, not the real close date; fixed by adding state and closed-date UDAs (PR #621). https://github.com/GothenburgBitFactory/bugwarrior/issues/620 . Q1/Q4. Grade C. HL.
- **Inference:** bugwarrior's shape is the minimum viable adapter: `list()`, a unique key, a field mapping, and a per-service bag of extra fields. It dodges "action a tracker cannot do" by never acting.

### git-bug bridges (two-way)

- **Interface** (full): https://raw.githubusercontent.com/git-bug/git-bug/trunk/bridge/core/interfaces.go . Q1. Grade V. Full.
  - `BridgeImpl`: `Target()`, `NewImporter()`, `NewExporter()`, `Configure()`, `ValidParams()`, `ValidateConfig()`, `LoginMetaKey()`. Comment on `NewImporter`: "return an Importer implementation if the import is supported", same for exporter. So direction is a capability a bridge may lack.
  - `Importer.ImportAll(ctx, repo, since time.Time) (<-chan ImportResult, error)` and `Exporter.ExportAll(... since ...)`. Whole-repo, incremental by timestamp, streaming results.
- **Feature matrix as the capability model.** A published table per bridge and direction: incremental, with resume, comments, comment edits, labels, status, title edit; Assignee and Milestone are unsupported on every bridge; Launchpad cannot do status, labels or incremental. https://github.com/git-bug/git-bug/blob/trunk/doc/feature-matrix.md (undated). Q1. Grade V. HL. **Inference:** capabilities are documented, not queried at runtime.
- **Unsupported fields are dropped.** Jira design doc: "The bridge currently does nothing to import any of the JIRA fields that don't have git-bug equivalents ("Assignee", "sprint", "story points", etc)". https://git.xszz.org/git-bug/git-bug/blob/trunk/doc/design/bridges/jira.md (mirror of trunk). Q1. Grade V. HL.
- **Id mapping.** The Jira bridge stores the Jira issue id on the bug and can write the git-bug id into a custom Jira field "so ... certain integration activities become easier". Same doc. Q1/Q4. Grade V. HL.
- **An action the tracker cannot do (the closest prior art to the brief's question).** Status is mapped by config (git-bug has only open and closed, each mapped to one Jira status). On export the bridge lists the issue's available transitions and runs one only if it exists. "It does not currently support assigning any fields during the transition so if any fields are required the transition will fail during export and the status will be out of sync." Planned fix: cache the workflow and mark bugs "blocked for export". Same doc. Q1/Q4. Grade V. HL.
- **Exporter without credentials.** "when they are not available, the corresponding changes can't be replicated." Feature-matrix note. Q1. Grade V. HL.

### todo.txt sync tools (small projects)

- **todosync** (RichardGomer): connector per source; todo.txt watched. Tasks carry extension keys `xsid` and `xssrc` (sync id, source). "Last update always wins." Unseen tasks are not treated as deleted ("we don't use non-listedness as a signal"). Unidentified lines become new tasks in a default source. Files changed while the sync script is down "will NOT be picked up". https://github.com/RichardGomer/todosync . Q1/Q3/Q4. Grade B. HL.
- **todo.txt-more `issue` extension** (blog 2022-12-31, https://proycon.anaproy.nl/posts/todo/ , Grade C, HL): marker `issue:<path>` on the line; closing locally calls the GitHub API; title copied once only; labels sync one way back; "When an issue is closed and reopened, it won't be detected". Q1/Q4.
- **github-todoist-sync** (https://github.com/peter7775/github-todoist-sync , Grade B, HL): explicit modes `once`, `daemon`, `github-only`, `todoist-only`; state map open/closed to incomplete/complete; priority from labels (urgent 4 ... low 1). Q1.
- Searched for a mature multi-tracker todo.txt sync tool and found none beyond these small ones.

### Cross-tool pattern (Inference)

All of these converge on the same four pieces: (1) a unique key per foreign item stored on the local copy (UNIQUE_KEY, xsid/xssrc, `issue:` URL, Jira id field); (2) a per-tracker status map onto a tiny local state set; (3) a per-tracker bag for fields the other side lacks (UDAs, dropped fields); (4) capability declared per direction, with unsupported actions silently dropped or failing with state out of sync. None offers a runtime capability query. None solves "action refused" beyond a flag or a log line.

## 2. Backlog.md (MrLesk/Backlog.md), full tracker read

Q2 facts: 7,074 stars, 454 forks, 82 open issues, MIT, created 2025-06-04. Latest release v1.47.1 published 2026-06-17 (GitHub API). Last commit seen 2026-10-19. https://github.com/MrLesk/Backlog.md . Grade B. Full for README and CLI reference.

**On-disk (Q1).** Folder `backlog/` (or `.backlog/` or custom path via `backlog.config.yml`), tasks in `backlog/tasks/`, drafts, archive and docs in sibling folders. File name `back-222 - Improve-task-and-subtask-visualization-in-web-UI.md` (id, then slugified title). Real frontmatter from the repo's own task:

```
---
id: BACK-222
title: Improve parent and subtask presentation in the Web UI
status: To Do
assignee: []
created_date: '2025-08-03'
updated_date: '2026-07-16 21:49'
labels: []
dependencies: []
---
## Description
<!-- SECTION:DESCRIPTION:BEGIN -->
...
<!-- SECTION:DESCRIPTION:END -->
## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 ...
<!-- AC:END -->
```

Source: https://raw.githubusercontent.com/MrLesk/Backlog.md/main/backlog/tasks/back-222%20-%20Improve-task-and-subtask-visualization-in-web-UI.md (full). Grade B.

Field list from the parser (https://raw.githubusercontent.com/MrLesk/Backlog.md/main/src/markdown/parser.ts, lines 95-145, full): `id`, `title`, `status` (free string, project-configured list, default `To Do`, `In Progress`, `Done`), `assignee` (list), `reporter`, `created_date`, `updated_date`, `due_date`, `labels`, `milestone`, `dependencies`, `references`, `documentation`, `modified_files`, `parent_task_id`, `subtasks`, `priority`, `type`, `project`, `ordinal`. Notes, plan, final summary and comments live in marked body sections (`CommentsManager.parseAllComments`). Q1. Grade B.

- **Id.** Prefix plus counter, `TASK-1` default, configurable with `backlog init --task-prefix`. Sub-task ids are dotted, for example `back-760.1`. Q1.
- **Close.** Set status to the terminal status (`Done`); the file stays in `tasks/`. `backlog task archive` moves it to an archive folder and the id stays reserved; `backlog task restore` brings it back with the same id (README). Reopen is just a status edit. Q1. Grade B. Full.
- **Due date.** `--due-date 2026-08-10` on create. No snooze or defer field. Q1.
- **Comment.** `backlog task edit 7 --comment "..." --comment-author @sara`; `--notes` replaces, `--append-notes` appends. Q1.
- **Mark for a human.** No dedicated field. Candidates: a label, `assignee` (a handle such as `@sara`), a status column, `type`. Q1. **Inference.**
- **Sub-task.** `backlog task create -p 14 "title"`; `task list --parent 42`. Q1.

**CLI (Q2).** `backlog task list --json`, `task view --json`, `search --json`; "stable, versioned JSON for scripts"; `--watch` with `--json` streams a full replacement list on change. README advice: "prefer Backlog.md commands (CLI/MCP/Web) over hand-editing task files, so field types and metadata stay consistent." Also an MCP server. Web UI (`backlog browser`) binds 127.0.0.1. Grade V. Full. Exit codes not documented in what I read.

**Concurrency (Q3).**
- PR #565 (merged 2026-03-19): "If two task creates, draft promotions, or task demotions ran at the same time ... they could both observe the same 'next' ID and write conflicting records." Fix: `proper-lockfile` around id allocation plus file mutation. https://github.com/MrLesk/Backlog.md/pull/565 . Grade B. HL.
- Source: file writes use `rename` and `unlink` with a read-copy-unlink comment so "a concurrent edit cannot be archived in pre-edit form or lost"; a write refuses when "another file already exists at that path". https://raw.githubusercontent.com/MrLesk/Backlog.md/main/src/file-system/operations.ts (grep only). Grade B.
- v1.45.0 note: "BACK-433 - Make task edit preserve unrelated file details" (edit no longer rewrites unrelated content). https://github.com/MrLesk/Backlog.md/releases/tag/v1.45.0 . Grade B. HL.
- Issue #578 (opened 2026-03-23, closed 2026-07-01): browser UI state "resets if files change"; an unsaved draft is cleared when an agent edits task files. The UI watches files and reloads. https://github.com/MrLesk/Backlog.md/issues/578 . Q3. Grade C. HL.
- PR #632 / duplicate ids (reporter story 2026-05-03): two git branches each allocated `TASK-123`, merged without conflict because file names differ, and the loader "uses a `Map<string, Task>` keyed by task ID, so one of the two `TASK-123` files is silently dropped on every load". Fix is warning-only. https://github.com/MrLesk/Backlog.md/pull/632 . Q3/Q4. Grade C. HL.

**Q4 hard points.** File name embeds id and title, so a title edit renames the file (inference from the `id - Title.md` pattern and the `rename` in operations.ts; not tested). Status is a free string per project, so a status-to-action map needs the project's config. Ids are stable once assigned, but branch merges can duplicate them. Archive moves the file to another folder.

## 3. Other markdown-in-repo trackers for coding agents (2025-2026)

All HL, search results only. Grade B (single-author repos) unless noted. Stars as shown by the search snapshot. Q1/Q2.

| Tracker | Format | Id | Status stored as | CLI for programs |
| --- | --- | --- | --- | --- |
| driangle/taskmd (75 stars, created 2026-02-08) https://github.com/driangle/taskmd | `.md` with YAML: `id: "001"`, `status: pending`, `priority`, `effort`, `dependencies`, `tags`, `created_at` | frontmatter id | frontmatter | CLI, web dashboard, MCP |
| scottopell/taskmd (1 star) https://github.com/scottopell/taskmd/ | all metadata in the file name: `34002-p2-ready--add-feature.md` | number in name | **rename of the file** | CLI auto-switches to JSON inside Claude Code, Cursor, Codex and others |
| veggiemonk/backlog (7 stars, archived) https://github.com/veggiemonk/backlog | `T01.01-setup_oauth.md`; frontmatter `id`, `parent`, `assigned`, `labels`, `status` | dotted id | frontmatter | CLI, MCP, `doctor` fixes id conflicts |
| fabiopelosin/taskplain (7 stars) https://github.com/fabiopelosin/taskplain | folders `00-idea`, `10-ready`, `20-in-progress`, `30-done` | file | **folder move** | CLI: new, update, move, pickup, complete; `executor: human_review` field |
| aholbreich/tl (taskledger) https://github.com/aholbreich/taskledger | `.tl/` Markdown with YAML; `tl claim` takes a lease; `--json`, `--actor` | short code | frontmatter | CLI |
| joshsegall/frame (2 stars) https://github.com/joshsegall/frame | checkbox lines with backticked ids and `#tags`, `dep:`, `note:`; position is priority | `EFF-014` | checkbox char `[>]` | TUI and CLI |
| TASKS.md spec https://tasksmd.github.io/tasks.md/ | one root file; P0-P3 headings; `**ID**`, `**Tags**`, `**Blocked by**`, `**Parent**`; claim with `(@agent-name)`; done tasks are deleted from the file | optional `**ID**` | checkbox | none, edit the file |

Note: this TASKS.md spec is unrelated to Tasks.md (BaldissaraMatheus), which has its own cluster. Do not confuse them.

**Inference:** three storage styles for status exist: a frontmatter field (Backlog.md, taskmd, tl), a folder (taskplain, Tasks.md), a file-name token (scottopell/taskmd). A generic adapter must treat "close" as one of: edit a field, move a file, rename a file. Where a tracker has a CLI (Backlog.md, tl, taskplain, taskmd), use it; the file-only ones (TASKS.md spec, frame without CLI) need a file-editing adapter.

## 4. Answers for the interface question

Q1 (interface shape): The tools in section 1 use four elements: unique-key mapping, per-tracker status map, an extras bag, and capabilities per direction. git-bug splits read and write as optional capabilities (`NewImporter`/`NewExporter` may return nothing). Bugwarrior has read only. Grade B overall (a handful of projects, source-confirmed for two).

Unsupported actions, as practised:
1. Drop silently (git-bug Assignee, Milestone; bugwarrior never writes).
2. Try, fail, leave states diverged (git-bug Jira transition needing fields).
3. Plan: cache the workflow, mark "blocked for export" (git-bug design, not built).
4. Protect local edits with an allowlist (bugwarrior `static_fields`).

**Inference for Meat Mode (decision, not a finding):** declare capabilities per adapter (for example close, reopen, defer, comment, subtask, due) and return a typed "unsupported" result so the hub can store the action locally and show it as pending or local-only. Without this, git-bug's out-of-sync outcome is the likely result. Reopen and defer are the actions most likely missing (Backlog.md has no defer field; todo.txt-more could not detect a reopen).

Q3 (concurrency, cross-cutting): the working tools lock only id allocation (Backlog.md `proper-lockfile`), serialise through their own CLI, or accept "last update always wins" (todosync). Polling is common; Backlog.md offers `--watch` on its JSON list and file-watches for its UI. No tool in this set uses content hashes or mtimes for conflict detection (searched, found none in these sources). **Inference:** driving the tracker's own CLI is the safest write path; direct file edits need a per-file lock or a compare-before-write on content hash.

Q4 (hard for two-way sync): free-form status strings per project (Backlog.md); status as file name or folder (scottopell/taskmd, taskplain), so an id-keyed lookup must rescan; merged branches creating duplicate ids with one copy silently dropped (Backlog.md #632); "closed by absence" semantics (bugwarrior), which would wrongly close tasks if a poll returns a partial list.

## Gaps

- Did not read the Backlog.md write path end to end, or test title-edit renames, or exit codes of `backlog task edit`.
- Bugwarrior services other than the base class not read; git-bug exporter source and its id-mapping store not read.
- No data-loss reports found for the smaller trackers (searched Backlog.md only).
- Beads is excluded by the brief. Taskwarrior's own sync (taskd) not covered.

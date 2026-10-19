# Task Master (eyaltoledano/claude-task-master)

Source read: a shallow clone of the repo at commit c0c98d3 (2026-04-23), plus GitHub API for stars, releases and issues. All "read in full" below means the named function or file section was read, not the whole repo. Grade B throughout (one vendor's own source, single repo), issues are C (anecdote).

## Q2 first: what it is, version, activity

- GitHub API, fetched 2026-10-19: 28,245 stars, 214 open issues, last push 2026-04-28. Latest release `task-master-ai@0.43.1`, published 2026-03-31 (`gh release list`). Previous: 0.43.0 on 2026-02-04, 0.42.0 on 2026-01-15. Q2, grade B, read in full.
- **Inference**: no release in about 6.5 months while issues keep arriving (newest read: #1714, 2026-06-30). Treat it as slowing, with bugs unfixed.
- `package.json`: bins `task-master`, `task-master-mcp`, `task-master-ai` (the last two are the MCP server). Node CLI, `npm i task-master-ai`.

## Q1: on-disk format

One file holds everything: `.taskmaster/tasks/tasks.json`. Other state: `.taskmaster/state.json` (current tag), `.taskmaster/config.json` (models), optional `.taskmaster/tasks/task_001_<tag>.txt` generated copies (read-only exports, `generate` command). Source: `.taskmaster/` of the repo and `docs/task-structure.md`. Grade B, read in full.

Shape (tagged lists, current format). Quote from `docs/task-structure.md`:

```json
{ "master": { "tasks": [ { "id": 1, "title": "Setup API", "status": "pending", ... } ] },
  "feature-branch": { "tasks": [ { "id": 1, "title": "New Feature", "status": "pending", ... } ] } }
```

Each tag object may also carry `metadata` (`created`, `updated`, `description`). Legacy files `{ "tasks": [...] }` are auto-migrated to a `master` tag on first use.

- **Id**: integer per tag, unique only inside the tag ("Unique identifier within the tag context"). Same id 1 exists in every tag. New id is `Math.max(...ids in the tag) + 1` (`scripts/modules/task-manager/add-task.js:290`). Subtask id is an integer unique inside its parent, addressed as `parent.sub` (`5.2`). A tag-qualified key is therefore `(tag, id)`. Ids can be reused if the highest task is deleted. Q1/Q4.
- **Status values**: the type in `packages/tm-core/src/common/types/index.ts` allows `pending, in-progress, done, deferred, cancelled, blocked, review, completed`; the `TASK_STATUSES` constant accepts the first seven (no `completed`). Docs list only six (no `blocked`). Closing is `done` (also `cancelled`); reopening is any set-status back to `pending`. The task also stores `previousStatus` (seen in the repo's own tasks.json). No separate closed flag or timestamp.
- **Fields on a task** (`docs/task-structure.md`, `Task` interface): `id, title, description, status, priority (low/medium/high/critical), dependencies [ids], details, testStrategy, subtasks[]`. Optional in the type: `createdAt, updatedAt, effort, actualEffort, tags: string[], assignee: string, metadata: Record<string, unknown>`, complexity fields. Comment in source on `metadata`: "User-defined metadata that survives all task operations. Use for external IDs, custom workflow data, integrations". 
- **Subtask**: nested array `subtasks` on the parent, same fields minus own subtasks (one level only, `subtasks?: never`), plus `parentId`. Files in the wild also carry `parentTaskId` (see Q3, #1708).
- **Due date**: none. `grep` for `dueDate` in the types found nothing. **Deferred/snoozed**: only the status `deferred`, with no resume time. **Comments/notes**: no comments array. The `details` string is free text; the AI-driven `update-subtask` appends to it as `<info added on 2026-03-01T10:00:00.000Z>...</info added on ...>` blocks (`update-subtask-by-id.js:355-356`). Assignees: type-level `assignee` string, not exposed by any CLI flag I found (**not verified in file storage round trip**).
- **Fields that could mark a task for the person** (**Inference**, ranked): (1) a dedicated tag named for them, e.g. `human` (tags are whole task lists, so a task lives in exactly one tag; this fits "tasks for me" but the agent's own tasks then live elsewhere); (2) per-task `tags: ["human"]` or `assignee: "human"` in the file (exist in the type, no CLI to set them, and a bad writer might drop them, see Q3); (3) `metadata.human = true` (documented as preserved). Option 1 is the only one the CLI supports end to end.

## Q2: CLI a program can call

Real commands (from `scripts/modules/commands.js` and `apps/cli/src/commands/`). Whether an AI key is needed, from reading the handler:

| Command | AI key needed | JSON output |
|---|---|---|
| `list [--tag t] [--json]` (`-f json`, `-p <root>`, `--ready`) | no | yes: `{tasks:[...], metadata:{total,filtered,tag,storageType,allTags}}` |
| `show <id> [--json]` (`5` or `5.2`) | no | yes |
| `set-status <id[,id]> <status>` (alias `status`; `-f json`, `--silent`) | no | yes: `{success:false,error,taskId,timestamp}` on failure |
| `next` (`-f json`) | no | yes |
| `add-task -t T -d D [--details --dependencies --priority --tag]` | no, only with both title and description (manual mode); `--prompt` calls AI | no JSON flag found |
| `add-subtask -p <parent> -t T [-d --details -s status --tag]` | no (manual); `-i <id>` converts an existing task to a subtask | no JSON flag found |
| `remove-task`, `remove-subtask`, `move`, `add-dependency`, `use-tag`, `add-tag`, `tags` | no (**Inference** from option lists, not each handler read) | tags: `--show-metadata` |
| `update-subtask -i 5.2 -p "text"` | **yes**, builds the note with an AI call | no |
| `update-task`, `update`, `expand`, `parse-prd`, `research` | yes | no |

Exit codes: error paths call `process.exit(1)` (`set-status.command.ts:122,135,...`); no documented code table. In JSON mode, failure is a JSON object on stdout, and the same command also sets a nonzero exit (**Inference**, code path shows `hasError` then exit in the finally block, not run). Source read: set-status, list, show, add-task, add-subtask, update-subtask handlers. Grade B.

Also ships an MCP server (`mcp-server/src/tools/`: `set-task-status`, `add-task`, `add-subtask`, `update-subtask`, `next-task`, ...), same data path.

**No way to write a note without AI via the CLI.** Options (**Inference**): put the note in `details` by writing the file yourself; or create a sub-task whose title is the note (manual `add-subtask`, no AI).

## Q3: how writes are done, and data loss

- Legacy path (`scripts/modules/utils.js:1021-1130`, `writeJSON`): whole-file rewrite inside `withFileLockSync`, which makes `tasks.json.lock` with flag `wx`, retries 5 times at 100 ms, treats a lock as stale after 10 s (steals it by atomic rename). Writes `tasks.json.tmp.<pid>` with `JSON.stringify(data, null, 2)` then `renameSync` over the file. Re-reads the file inside the lock to merge the active tag back into the other tags. Grade B, read in full.
- New core path (`packages/tm-core/.../file-storage/file-operations.ts`): `modifyJson` read-modify-write with `proper-lockfile` (`stale: 10000`, `retries: 5`) and `steno` atomic writer. Comments say "prevent lost updates"; a spec "should re-read file inside lock to prevent stale data". Grade B, header and spec names read, body skimmed.
- A foreign writer that follows the same convention would create `tasks.json.lock` (legacy) or a `proper-lockfile` lock dir `tasks.json.lock` (new path). **Inference**: the two lock formats are different mechanisms with the same name, so a third-party writer cannot interlock with both reliably. A tmp-file-plus-rename without locking is safe against torn reads but can lose an update made between its read and its rename.
- No file watching by the tool for external edits; `list --watch` only refreshes the display. Polling is the only option for a reader (**Inference**).
- Issues (all open, GitHub API, grade C):
  - #1708, 2026-06-07: `set-status` on 0.43.0 "converts `task.id` from number to string for every task in the active tag" and adds `"parentId": "undefined"` to every subtask; "Reproduces 100%". Q3/Q4.
  - #1583, 2026-01-16: id type flips between `"3"` and `3` depending on which command last wrote. Also #1670 (2026-03-22) "normalize task ID types in tasks.json writes". Q4.
  - #1636, 2026-02-20: `fix_dependencies` "wipes all tags except 'master'" in a 51-tag, 13 MB file; "caused data loss multiple times". Q3.
  - #1637, 2026-02-20: MCP server keeps stale in-memory `currentTag`, so updates overwrite parent task data with another tag's. Q3.
  - #1683 (2026-04-09): `set_task_status` drifts from tasks.json, `next_task` returns master for tagged runs.
  - Searched for an issue about two processes clobbering the same file during a write and found none that blames the locking; the losses found come from logic bugs, not races.

## Q4: what makes two-way sync hard

- Id is not stable or unique: per-tag integers, `(tag, id)` needed; type flips between number and string across versions (#1583, #1708). A sync layer must compare ids as strings and tolerate both.
- A write by the tool rewrites the whole file and reformats and may reshape unknown fields (#1708 shows it adding fields); `metadata` is documented as preserved, `assignee` and `tags` are not confirmed. Do a read-after-write check on any custom field.
- No due date, no snooze time, no comment list, no closed timestamp: Meat Mode's snooze, notes and due date have no native home. They would live in `metadata` (**Inference**) or in a Meat Mode sidecar keyed by `(tag, id)`.
- "Dismiss" has a native fit: `cancelled`. "Close" is `done`. "Reopen" is `pending`. "Defer" maps to `deferred` but loses the time.
- Ids of removed tasks can be reused (max+1), so a sidecar keyed by id can attach old state to a new task. **Inference**: include title hash.
- Whole-file rewrite means one hub write can race the agent's CLI; use the CLI (`set-status`, `add-subtask`) so the tool's lock is taken, rather than editing the JSON.
- The tool does not own a server for the file backend. The `api` storage type (Hamster cloud, `storageType` in list JSON) uses string display ids like `HAM-2611` and is a different backend to skip.

## Gaps
- Did not run the CLI (no install); outputs come from source. Exit-code behaviour, `assignee`/`tags` survival and `--json` on add commands are unverified.
- No blog or third-party sync tool for tasks.json found; not searched beyond the issue tracker.

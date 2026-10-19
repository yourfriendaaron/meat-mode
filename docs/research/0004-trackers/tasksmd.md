# Tasks.md (BaldissaraMatheus/Tasks.md) findings

Reader notes for meat-nv6. Fetched 2026-10-19. All sources read in full unless marked. Evidence grades: B = single project's own code/docs, C = issue-thread anecdote. Conclusions marked **Inference**.

## Bottom line

Tasks.md is a folder-per-lane, file-per-card board with no ids, no frontmatter, no status field, no comments and no assignees. A card's identity is its lane folder plus its file name, and both change on a move or rename. It ships no CLI. Its only programmatic interface is the web app's own unversioned HTTP API, which does plain whole-file writes with no locking. **Inference:** a two-way adapter is feasible only as a file-level adapter that treats `lane/name.md` as the id and accepts that ids are not stable. It is a weak fit for "tasks for the human" because nothing in the format marks one, except a tag convention Meat Mode would have to define.

## Q1 On-disk format

Source: README, https://raw.githubusercontent.com/BaldissaraMatheus/Tasks.md/main/README.md (read in full, current main). Grade B. Answers Q1.
- Quote: "Every lane you add within the app is a directory in your filesystem and every task is file." Layout is `TASKS_DIR/<lane>/<card name>.md`. A subdirectory can be opened as its own board ("by opening the app under `/backlog` path it will treat this directory as a different project").
- Source: backend/server.js `getResource` (https://raw.githubusercontent.com/BaldissaraMatheus/Tasks.md/main/backend/server.js, read in full). Grade B. Lanes are the directories that do not start with ".". Cards are files ending `.md` and not starting with ".". Returned per card: `name` (file name minus `.md`), `content` (raw text), `lastUpdated` (mtime), `createdAt` (birthtime). Only one level of lanes is read; no nesting inside a lane.
- Naming rules, issue #150 (https://github.com/BaldissaraMatheus/Tasks.md/issues/150, 2025-01-31, closed): file names keep spaces and some special characters, e.g. `Lane Name 1/Card Name 1.md`. The maintainer declined a date-prefix scheme: "the board should look just as a different view of the filesystem ... it comes with its own challenges, like how to map the attributes in the filename to the actual file attributes, what to do when the user renames the file". Grade C.
- No frontmatter. Everything is inline bracket markers in the body. Source: frontend/src/card-content-utils.js (https://raw.githubusercontent.com/BaldissaraMatheus/Tasks.md/main/frontend/src/card-content-utils.js, read in full). Grade B.
  - Tags: regex `/\[tag:(.*?)\]/g`. Adding a tag prepends `[tag:name] ` and, for the first tag, a blank line: `[tag:errand] \n\nBuy stamps`. Tags can be anywhere in the file (migration guide: "The order of the tags does not matter, they can be anywhere in the file"). Before v3 the syntax was `tags: a, b, c` (https://raw.githubusercontent.com/BaldissaraMatheus/Tasks.md/main/migration-guide.md). The format has already broken once between majors.
  - Due date: regex `/\[due:(.*?)\]/`, value `YYYY-MM-DD` per the code comment. Set by replacing the existing marker or prepending `[due:2026-11-02]\n\n`. Date only, no time of day.
  - Checkboxes are ordinary markdown task lists (`- [ ]`) in the body.
- Tag colours are not in the files. They live in `CONFIG_DIR/tags.json`, keyed by board path. Manual card order lives in `CONFIG_DIR/sort.json`, also keyed by board path (server.js `updateSort`/`getSort`). The card files hold no order. Grade B.
- Real example of an on-disk card (assembled from the code above, invented data):
  ```
  Errands/Buy stamps.md
  [tag:human] [tag:errand] [due:2026-11-02]

  Post office closes at 17:00.
  - [ ] check the weight
  ```
- Id: none. The name is the file name. New cards are created by the UI with a uuid v7 as file name (issue #212 trace shows `/tasks/Todo/019eb376-7d27-758f-8345-990c91325153.md` created via `createResource`), then renamed to the typed title via PATCH `newPath` (App.jsx, `PATCH` calls, read via grep, highlights only). Grade B/C.
- Status: the lane folder is the status. Closing in the format is moving the file to another lane (conventionally "Done"; the README demo has "Backlog, Sprint, Done"). There is no closed flag. Reopen = move back.
- Done concept: a PR adds a `completed` date and moves the file to `<board>/.done/<lane>/`. Issue/PR #217 (https://github.com/BaldissaraMatheus/Tasks.md/issues/217, 2026-10-05, open, not merged; author's own description "a 'completed' date is added to the MD file, and the MD file is moved to the same lane ... within a '.done' folder"). Grade C. Not in the shipped format.
- Deferred or snoozed: no concept. Only `[due:]`. **Inference:** a snooze would have to be a Meat Mode convention such as a `[tag:snoozed]` plus a second marker, which the UI would show as a plain tag.
- Parent or sub-task: no concept. Markdown checklists inside one card are the only nesting.
- Comments or notes: none. Issue #141 asked for comments and was closed 2025-08-04 as out of scope. Notes would be appended to the body text. Grade C.
- Assignees: declined. Issue #151 (https://github.com/BaldissaraMatheus/Tasks.md/issues/151, closed 2025-08-04): "I decided to not include assignees. This project was made with the focus on personal projects". A commenter's workaround: tags such as `assignee:bob`. Grade C.
- Metadata and change log (#140, #145): declined; "Last update" uses file mtime only.
- Fields a "task for the human" could be marked with: a `[tag:human]` marker (convention only, matches the beads `human` label), or a dedicated lane folder such as `Human/`. No other field exists. **Inference:** the tag is the better choice, since lanes are status here.

## Q2 CLI or API, version, activity

- No CLI. Issue #215 "Add a CLI to interact with the tasks" (https://github.com/BaldissaraMatheus/Tasks.md/issues/215, 2026-09-04, open, 0 comments): a third party proposes one "Plan to use the existing API as the basis -- not direct editing of the files". Nothing built or accepted. Grade C.
- HTTP API only, undocumented, used by the SolidJS frontend. Base `/_api` (frontend/src/api.js, read in full). Routes in server.js: `GET /resource/<path>` (lists lanes with all card contents), `POST /resource/<path>` (body `{isFile, content}`; creates file or folder), `PATCH /resource/<path>` (body `{newPath, content}`; renames then rewrites content), `DELETE /resource/<path>` (recursive `rm`, force), `GET/PATCH /tags/<path>`, `GET/PUT /sort/<path>`, `POST /image`, `GET /title`. No auth in the app (an OIDC draft PR #176 is closed). JSON out is just the lane/file list. No versioning, no documented exit codes or error shape; errors surface as 500 with stack in the log (#212). Grade B.
- Release: 3.3.0, published 2026-03-08; 3.2.2 2025-12-29; 3.2.1 2025-12-26 (GitHub API, releases). Last push to the repo 2026-03-08. 2,206 stars, 21 open issues, MIT, created 2023-02-26 (GitHub API, 2026-10-19). Grade B.
- Activity: README says "This is a low maintenance project. The scope of features and support are purposefully kept narrow". Maintainer replies on issues come months late ("sorry for the long delay", 2026-06-17 and 2026-09-14 on #212/#214). **Inference:** low-maintenance, alive but slow; the format is unlikely to grow fields, and unlikely to break either, apart from the v2 to v3 tag change.
- **Inference:** a program should edit the files directly. Going through the API adds nothing (no ids, no JSON schema) and requires the hub-side container to be reachable from the bridge.

## Q3 Safe concurrent writes

- Server writes with `fs.promises.writeFile` straight onto the card path (server.js `createResource`, `updateResource`): no temp file, no rename-over, no lock, no etag or mtime check. Rename is `fs.promises.rename` then a separate content write, so two steps, not atomic. `sort.json` and `tags.json` are read-modify-write of the whole file with no lock, so concurrent PATCHes can lose one update. Grade B.
- No file watcher on the server (grep of server.js for watch/chokidar/lock: no hits). The server reads from disk on every `GET /resource`. The UI loads once and does not poll: the 5-second polling PR #203 (https://github.com/BaldissaraMatheus/Tasks.md/issues/203, 2025-10-31, open) is unmerged. The maintainer's review (2025-11-02) names the conflict risk: the open editor "is reset" when data is fetched and a changed file should "prevent the update and ask for the user to decide what to do if there are conflicts". Grade C.
- Clobbering on record: #142 (https://github.com/BaldissaraMatheus/Tasks.md/issues/142, 2024-12-06, closed as duplicate of #111): "if you edit a markdown file in a code editor, then load it via tasks.md, your edits will be lost." Mechanism (**Inference**, from the code): the UI holds the loaded content and writes the full body back on close, overwriting any outside edit made while it was open. #175 "Rerender UI when files change outside the app" (closed 2025-07-28) addressed a refresh, with no conflict detection visible in server.js. Grade C.
- Data not saved on UI side: #214 (2026-09-04, open) paste-only edits are not saved; #216 is a fix PR. Not an outside-write bug but shows the UI writes full content on its own schedule. Grade C.
- **Inference:** a bridge write is safe only if it (1) writes a temp file in the same folder and renames over (readers never see a partial file), (2) re-reads and compares mtime or content hash just before writing, and (3) never writes while the UI has that card open with unsaved text (unknowable). Risk of the UI clobbering a bridge edit is real, the other way round is low. Polling the folder is the only option; there is no change feed.

## Q4 What makes two-way sync hard

1. No stable id. Id = `lane/filename`. Moving a card changes the folder, renaming the title changes the file name, and the UI may move or rename it at any time (PATCH `newPath`). The bridge must keep its own map and match renamed files by content or birthtime, or inject an id marker into the body (for example `[meat:7f3a]`, a **Inference**-level idea; the UI would show it as text, and the editor round-trips markers it does not know, since tags are plain text).
2. Status is the folder, so closing is a rename across directories; "closed" has no agreed lane name (the lane name is user chosen). The adapter needs a configured done-lane name per repo.
3. File names double as titles and cannot hold some characters. Server code `replaceAll(/<>:"\/\\\|\?\*#/g, " ")` is a regex for the literal string, so it never matches a single character; unsafe characters are not reliably stripped (read from server.js, **Inference** from code reading, untested). `#` and `%` in names caused navigation bugs (#195, #202, #204). Meat Mode should create names without `# % / \ : * ? " < >`.
4. No time of day on a due date, no snooze, no parent, no comment, no assignee. Each Meat Mode action needs a convention in the body, which the tool will show as raw text: defer = rewrite `[due:YYYY-MM-DD]`, a note = appended line, a sub-task = a `- [ ]` item or a second card. Reopen = move back to the first lane.
5. The "server owns the files" risk: the web app is optional. If the board is only files in a repo, nothing else owns them. If the Docker app is running over the same folder, see Q3.
6. Markdown task lists are mangled by the rich editor in some modes (#183, `- [ ]` escaped to `\[ \]`, open since 2025-08-14). An adapter that appends `- [ ]` lines may see them rewritten after a UI edit. Grade C.
7. Ordering is in `config/sort.json` outside the repo folder, so it is not visible to a bridge that reads only the tasks folder.
8. Whether a `.beads`-style repo marker exists: none. The bridge cannot "find repos that use Tasks.md" by a folder name; Tasks.md has no marker file. **Inference:** the user would need to list the task folders explicitly, or Meat Mode must define a marker.

## Not found

- No documented JSON/CLI, no schema, no spec of the format beyond README prose and the code.
- No issue about id changes by name; the id problem is inferred from the code and #150. Searched issue titles (100 of 130+ issues listed; the rest are older PRs) and read 12 threads.
- No evidence of anyone else syncing a program to Tasks.md files; Hugo integration issue #131 not read.

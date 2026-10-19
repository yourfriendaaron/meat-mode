# Backlog.md

Agents that keep their tasks in [Backlog.md](https://github.com/MrLesk/Backlog.md) can hand you a task by giving it the label `human`. It shows up in Meat Mode next to your own to-dos, and what you do with it there is written back to the task file through the `backlog` command.

## What you need

- Backlog.md 1.51.0 or newer on the machine the bridge runs on, as `backlog` on the PATH or at the path in the bridge config's `trackerBins` (`{"trackerBins": {"backlog": "/opt/backlog/bin/backlog"}}`). With mise: `mise use -g npm:backlog.md`.
- A repo set up with `backlog init`. The bridge finds a repo by the config file Backlog.md itself looks for: `backlog/config.yml`, `.backlog/config.yml` (either can end in `.yaml`), or a `backlog.config.yml` at the repo root that names the project. A `backlog/` folder without a config is not a Backlog.md project and is skipped.

The bridge never walks into a `backlog/` or `.backlog/` folder while it looks for repos.

## Mark a task for you

Give it the label `human`:

```sh
backlog task create "Renew the library card" -l human
backlog task edit TASK-12 --add-label human
```

Meat Mode shows every task with that label: open ones, and ones closed in the last 7 days.

## Switch a repo's syncing on

Add `.meat-mode.yaml` at the repo root with the line `sync: true`, or use the sync switch for the repo in the app, which overrides the file. Backlog.md's own config has no place for it.

## How tasks read in the app

Backlog.md's statuses are the columns of the project's board (`backlog config get statuses`), so Meat Mode reads them in their order:

| Board column | In Meat Mode |
|---|---|
| The first column (`To Do`) | open, or blocked while a task it depends on is unfinished |
| Any column between the first and the last (`In Progress`) | in progress |
| The last column (`Done`) | done |
| A status not on the board | open |

- **Priority**: `high` is high, `medium` or none is normal, `low` is low.
- **Due date**: Backlog.md keeps a day with no time, so a due date reads as 9:00 that day in the bridge machine's time zone.
- **Sub-tasks**: a task with a parent (`backlog task create -p TASK-7 ...`) shows under it.
- **Notes**: the task's comments, with their authors.

## What each action writes

| In Meat Mode | Written to the task |
|---|---|
| Done | status set to the board's last column |
| Reopen | status set to the project's default status for new tasks (the first column when that is unset), label `dismissed` removed |
| Won't do (dismiss) | status set to the last column, label `dismissed` added |
| Note | a comment, `backlog task edit --comment ... --comment-author <you>` |
| Add | a new task labelled `human`, with the due day and parent you chose, and the reference `meat-mode:action/<id>` so a retried add is not made twice |
| Ask an agent | a run that reads the task with `backlog task view`, replies with comments and closes it by status |
| Snooze | not available: Backlog.md has no field for it |

A few details:

- Closing a task with open sub-tasks is allowed, as it is in Backlog.md.
- Backlog.md uses a line of only `---` to separate comments and refuses one in a comment, so such a line in a note is written as `- - -`, which Markdown shows as the same rule. The app reads `- - -` back as `---`, so the note shows as you wrote it and a retried note is not written twice.
- An agent's comments carry its name through `--comment-author "$BACKLOG_COMMENT_AUTHOR"`, a variable the bridge sets for the run, since Backlog.md reads none of its own.
- Meat Mode never changes a task's title, because Backlog.md renames the task's file when the title changes.
- Archived tasks (`backlog task archive`) leave Meat Mode, as if deleted. Restore one to bring it back.
- In a repo with a Git remote, `backlog task create` fetches first so the new id is not taken on another branch; that is Backlog.md's own setting (`remoteOperations`).

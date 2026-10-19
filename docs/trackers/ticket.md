# ticket

[ticket](https://github.com/wedow/ticket) keeps one Markdown file per task in a `.tickets` folder at the repo root, and `tk` is the script that edits them. Meat Mode shows the tasks marked for you and writes your actions back through `tk`.

## Install

Install `tk` from the ticket project's README (a Homebrew tap, the AUR, or the script copied onto PATH). The bridge looks for `tk` on PATH, or at the path `trackerBins` gives for `ticket` in the bridge config. ticket prints no version, so the version shown for it in Settings is blank.

## What marks a task for you

The tag `human`. An agent raises a task for you with:

```
tk create "Water the plants" --tags human
```

Tasks without that tag are not synced. A repo is found by a `.tickets` folder that holds at least one `.md` file, or holds nothing yet. The bridge never walks into `.tickets`.

## Switching a repo on

Put `sync: true` in `.meat-mode.yaml` at the repo root, or turn the repo's switch on in the app.

## Actions

| Action | What it writes |
|---|---|
| Done | `tk close <id>` |
| Reopen | `tk reopen <id>` |
| Won't do | `tk close <id>`, then a note "Dismissed in Meat Mode". The app reads the note back as the close reason, so a reopen works as for any task. |
| Note | `tk add-note <id> ...` |
| Add | `tk create <title> --tags human -a <actor> --external-ref meat-mode:<action id>`; the external ref is how a retried add finds the task it already made |
| Sub-task | the same with `--parent <id>` |
| Ask an agent | the agent gets a prompt naming `tk show`, `tk add-note` and `tk close` |
| Snooze | not supported: ticket has no field for it, so the app hides it for these repos |

A due date has no `tk` flag. The bridge adds a `due: <time>` line to the task's frontmatter after the create, through a temp file and a rename, and reads it back.

`tk create` has no `--` and reads any word starting with `-` as an option. A title that starts with `-` goes to `tk` behind a space, which the same rewrite takes off the `# ` heading.

## How a task reads

The bridge parses the files itself, since `tk` prints no JSON for a title or notes.

- Frontmatter keys sit at column 0 and arrays are inline (`tags: [human, garden]`), the shape `tk` itself reads. Only the lines between the first two `---` count, so a body line starting `status:` is not a field.
- The title is the first `# ` heading; the description is the text after it, up to a `## Design`, `## Acceptance Criteria` or `## Notes` heading.
- Status: `open` is open, `in_progress` is in progress, `closed` and `done` are closed. ticket keeps no close time, so a closed task's close time is the file's last write.
- Notes are the `**<UTC time>**` blocks under `## Notes`. ticket records no author, so a note Meat Mode writes starts with a line `by <author>:`, which the app strips off again. A note without that line, from `tk` or an agent, shows as written by "agent".
- The version is a hash of the file's bytes.

## Things to know

- A note that Meat Mode did not write and that has the form `by <name>:` on its first line shows that name as its author.
- `tk close` rewrites every line starting `status:` in the file, body included, so avoid starting a description line that way. Meat Mode's own reads are not affected.
- ticket does not check that a new id is unused, and two writers at the same moment can lose one change; both are ticket's own limits.
- Closing a task with open sub-tasks is allowed, as in `tk`.
- Only the last note decides whether a closed task reads as dismissed: closing again after a dismiss and a reopen still shows "dismissed".

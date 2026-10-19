# beads

Meat Mode shows the tasks your agents file for you in [beads](https://github.com/gastownhall/beads) repos, and writes your answers back with `bd`, the beads command line. beads supports every action in the app, sub-tasks and agent runs included.

## Install

The bridge needs `bd` 1.3.0 or later on the agent machine, on your `PATH` when you run `meat-mode bridge pair`, so the service finds it too. To use a `bd` that is not on your `PATH`, give its path in the bridge's `bridge.json`:

```json
{"trackerBins": {"beads": "/opt/beads/bin/bd"}}
```

The older `"bd": "/opt/beads/bin/bd"` key does the same. `meat-mode bridge status` shows the version the bridge found. With a `bd` that is missing or too old, beads repos are skipped, and the bridge does not start when beads is its only tracker.

## Which repos and tasks

A repo is a folder with a `.beads` database in it: the `metadata.json` that `bd init` writes, or an older `*.db` file. A `.beads` folder without one, such as bd's own `~/.beads`, does not count.

A task is for you when it has the label `human`. An agent files one like this:

```sh
bd create "Approve the new footer text" -l human
```

The app shows open, in-progress, blocked and deferred tasks labelled `human`, and closed ones for 7 days. A sub-task is a bead with a parent; its parent shows by name even when the parent is not labelled `human`.

## Opt a repo in

Turn the repo's switch on in the app, under **Settings**. Or opt it in from the repo itself, with this line in `.beads/config.yaml`:

```yaml
meat-mode.sync: true
```

`sync: true` in a `.meat-mode.yaml` at the repo root works as well. A switch set in the app wins over both.

## What each action runs

The bridge runs `bd` in the repo, with your name from the app as the actor (such as `alex-app`). Ids and text always come after `--`, so none can be read as a flag.

| In the app | The bridge runs |
|---|---|
| **Done** | `bd close --reason="Done in Meat Mode" --actor=<you> -- <id>`, unless it is closed already. bd refuses it while a sub-task is open. |
| Reopen | `bd reopen --actor=<you> -- <id>`, unless it is open already. |
| Snooze | `bd update --defer=<time> --actor=<you> -- <id>`; an empty `--defer=` clears it. |
| **Won't do** | `bd human dismiss --actor=<you> -- <id>`, unless it is closed already. |
| Note | `bd comments add --author=<you> --actor=<you> -- <id> "<note>"` |
| Add a task | `bd create --title="<title>" -l human --metadata='{"meat_mode_action":"<action id>"}'`, with `--due=` and `--parent=` when set. The action id lets the bridge find the bead again after a crash instead of making it twice. |
| Ask an agent | The note as above, then the agent starts with `BEADS_ACTOR` set to its name. Its first message tells it to read the task with `bd show <id>`, close it with `bd close <id>`, or reply with `bd comment <id> "..."`. |

To read the list, the bridge runs `bd list --all -n 0 --json -l human`, and `bd show --include-comments --json -- <id>` for each task that changed.

# 0007: A read-only Epics view

The question: can Meat Mode show an agent repo's epics and their children, read-only and grouped by repo, without any of those beads reaching the paths that serve the person's own tasks (Today, the calendar, reminders, push, the "new" dots, badges, snooze, agent runs)? And what sync, storage and API does that take?

Reader notes: [scotty.md](scotty.md) (a third-party beads web UI's Epics view, read from its source), [beads.md](beads.md), [backlog-ticket.md](backlog-ticket.md). Brief: [brief.md](brief.md).

Grades: A controlled or large n, B one team or small n, C anecdote, V vendor guidance. **Inference** marks a conclusion of this page.

## Findings

### Volume is small once closed beads are left out

- On the operator's machine (seven beads repos, about 1,550 beads), `bd list --all -n 0 --json` took 101 to 174 ms per repo; the largest (685 beads, 3.1 MB of JSON, most of it descriptions) took 158 ms against 121 ms for the same call with `-l human` (local, B, bd 1.3.2). A third-party probe measured 437 ms for 2,000 issues at bd 1.1.0 (C, beads.md).
- Almost every bead is closed. In the largest repo 675 of 685 are. Across all repos there are about 80 open beads, 9 open epics, about 60 direct children of an open epic (12 of them closed), and about 27 open top-level tasks that are not epics (local, B).
- **Inference**: the view needs open epics, every direct child of each, and open top-level tasks: about 100 rows over every repo, not 1,700. Volume is not the constraint.

### One list call carries every field

- `bd list --all -n 0 --json` gives `issue_type`, `parent`, `assignee`, `status`, `priority`, `labels` and the `parent-child` dependency edges for every bead, with no per-bead `bd show` (local, B; beads source, V).
- Backlog.md's `backlog task list --json` gives id, title, status, priority, `assignees`, `labels`, `type` and `parentTaskId` in one call: 0.18 s and 398 KB on 700 generated tasks (B, backlog-ticket.md). `--plain` lacks parent and assignee.
- ticket keeps one Markdown file per ticket with `type` (which may be `epic`) and `parent` in its front matter. This repo's adapter already reads the folder directly (V, backlog-ticket.md).

### What an epic and its progress are

- beads: an epic is `issue_type: epic`; children are linked by a `parent-child` edge, which the list also gives as `parent`. `bd epic status` counts closed over direct children, with deferred, blocked and in-progress children counted as not done (V, beads.md). Tombstones were removed in 0.51; a deleted bead is gone (V).
- The third-party view does the same: only `issue_type: epic` makes a card, progress is closed over direct children, an epic with no children shows 0/0, a nested epic shows as its own card as well as a child row, and P0 to P4 read Critical, High, Medium, Low, Backlog (B, scotty.md). Its maintainer leans to direct-children progress, and recursive progress is an open issue there (C).
- Backlog.md has no epic type: `parentTaskId` is the only link and nests to any depth; `type` is configurable and usually empty. Its own open ticket on parent progress counts children in the terminal status (V, backlog-ticket.md).
- ticket has `type: epic` and `parent`; neither is enforced, and nesting is unlimited (V).
- **Inference**: one rule fits all three: a card for each open item that is an epic by type or has a child, progress as done over direct children of any status, and a nested epic shown both as a row of its parent and as its own card.

### How viewers read and fail

- Viewers that last read through the tool's CLI with `--json`, with a file watcher or polling to stay fresh. Those that read beads' storage directly (JSONL, SQLite) broke on the Dolt move and on schema changes in 0.50 and 0.51 (B, beads.md). The third-party view runs one whole-repo `bd export --json` per refresh, with a 30 s poll fallback (B, scotty.md).
- Embedded Dolt took an exclusive lock; at bd 1.0, 4 of 5 concurrent `bd list` calls failed until callers were queued (B, beads.md). A stale lock file made `bd list` return zero rows without error (B, beads issue 1773).
- bd 1.1.0 changed the shape of dependencies in `bd show --json` and broke the third-party view's parsing (B, scotty.md).

### What the hub reads today

- Every path the person's tasks reach reads the `items` table or `api.State`: 28 queries over 13 files in `internal/store`, and `internal/reminders` through `Items()` (local, A for this repo).
- The app re-fetches `/api/state` on every `change` event from `GET /api/events` (`web/src/main.ts`).
- Agents are switched on only at the agent machine (`meat-mode bridge agents on`), so a hub that is taken over cannot widen what agents touch (`docs/api.md`, Agents).
- "Project" already names a list the hub keeps (`--project`, `meat-mode projects`, Delete list), so the view is called Epics.

## Recommendation

**Separation: a table of its own.** Epic rows live in a hub table that no existing query reads. They never enter `items`, `api.State` or the export, and the app fetches them from their own `GET /api/epics`. Each path in the brief's list reads `items` or `State`, so none can show an epic row: the line is structural, not 28 filters. Alternative: a kind flag on `Item`. That keeps one table, at the cost of a filter on every one of those queries, on reminders, push and the calendar feed, and on each future one; one missed filter puts an agent's bead in Today or on the lock screen.

**A human bead inside an epic** is one to-do and one projection row. The epics table holds only the row's projection (id, type, title, status, priority, assignee, parent). When the row's key is also an item in `State`, the app draws the row from that item, so a Done in the app shows at once, and a tap opens its existing task sheet with every action. Every other row is read-only.

**Sync: the same bridge and poll loop, a separate pass.** A tracker that can show epics implements an optional `tracker.Epics` interface (as `tracker.Agent` is optional), and the repo report carries `epics: true` for it, outside `Actions`, which lists action kinds. The pass runs its own list call and leaves the human path's call and fingerprints alone, at a measured cost of under 0.2 s per repo. It hashes the sorted projection, puts the whole repo's set with `PUT /bridge/v1/epics?repo=` only when the hash changes or `RefreshEvery` passes, and never replaces a non-empty set with an empty one on the first empty answer: it waits for a second. The hub sends `event: epics`, not `change`, so an epics update never re-fetches Today. Older apps ignore an event name they do not know.

**What crosses: open epics, all their direct children, open top-level tasks.** Closed epics are left out. Descriptions are left out, except an epic's own description for Details; notes, design and acceptance never cross.

**Opt-in: at the agent machine.** `meat-mode bridge epics on|off <repo>` writes an `epics` map in `bridge.json`, which the running bridge reads before each repo report, as it reads `agents`. The app can switch a repo off, never on. Mirroring a repo's plan onto the hub widens what leaves the machine, the same shape of risk as agents. Alternative: a key in the repo's own files (`meat-mode.epics: true`), which is easier to set from the repo but is then committed to it, so it switches on for every clone and every machine.

**Actions: Copy id only.** It is client-side and feeds an agent prompt. beads, Backlog.md and ticket have no web URL, so "open in tracker" has nothing to open.

**Frontend: no framework.** The app already switches among about eight views in plain TypeScript; one read-only view does not meet the "several more screens" bar of decision 0001.

**Release: minor.** A new bridge route, a new app route, an `epics` field on `api.Repo`, a new event name and a migration change `docs/api.md` and `internal/store/store.go`, both floor paths. Nothing is removed or changed in meaning, so it is not a major. Epic rows are a cache the bridge fills again, so they stay out of the export and the export version does not move. A new bridge on an older hub gets `404` from the new route; it logs once and tries again after an hour.

## Outside check

The likeliest failure is an agent's bead reaching a path for the person's own tasks. The check is a hub test that fills the epics table for a repo with an invented epic and children, then asserts that none of their ids appears in `GET /api/state`, the reminders source, the push queue, the calendar feed, the counts or the export. It runs in CI with every other store test and fails the day a new path reads the wrong table. The second check is the conformance suite: each tracker's `Epics` runs against the real `bd`, `backlog` and `tk` on a fixture repo, including Backlog.md tasks moved to `completed/` (whose drop from `task list --json` would turn 6/7 into 0/1) and a nested epic.

## Call

Change approach from the brief's framing in two places: the view is called Epics, and volume is no reason for a separate sync design, since only about 100 rows cross. Otherwise proceed: a separate table, a separate pass in the same bridge, opt-in at the agent machine, read-only with Copy id, a minor release.

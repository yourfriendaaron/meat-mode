# ticket (wedow/ticket, the `tk` script)

Read on 2026-10-19 from a shallow clone of https://github.com/wedow/ticket at commit 194b71a (2026-03-15). All source claims are "read in full" for `ticket` (1383 lines), README, CHANGELOG and `plugins/ticket-query`; other plugins and the behave tests were skimmed. Evidence grade for source reads: B (single-team project, but the source is the ground truth).

## Q2 first: project facts and the callable surface

- Stars 912, 42 open issues, MIT, last push 2026-03-16 (GitHub API, queried 2026-10-19). Latest release v0.3.2, 2026-02-03; v0.3.1 2026-01-28; v0.3.0 2026-01-19. No release since, and `main` has unreleased work (the plugin split, `super`, `TICKETS_DIR` export). Newest issue is 2026-07-18, so it is alive but slowing. (B, API)
- It is one portable bash script plus plugins. README: "requiring only coreutils"; `query` needs `jq`. Install via brew tap, AUR, or copy the script. (B)
- Commands: `create`, `start`, `close`, `reopen`, `status <id> <open|in_progress|closed>`, `dep`, `undep`, `link`, `unlink`, `ls|list`, `ready`, `blocked`, `closed`, `show`, `add-note`, plus plugins `edit`, `query`, `migrate-beads`. (README usage block, A-level for what it says)
- Machine-readable output: only `tk query [jq-filter]` (plugin `ticket-query`). It prints one compact JSON object per line (JSONL), made by an awk pass over `.tickets/*.md` frontmatter, then `jq -c "select($filter)"` if a filter is given. Every scalar becomes a string (`"priority":"2"`), `[a, b]` values become arrays. It reads frontmatter only: no title, no body, no notes. `ls`, `ready`, `show` are human text. Exit codes: 0, or 1 with a message on stderr; no documented codes beyond that. (read in full, B)
- `create` prints only the new id on stdout, so it is scriptable. `close`/`status` print `Updated <id> -> <status>`. `add-note` prints `Note added to <id>`. (source)
- **Inference**: a bridge can list through `tk query` but needs its own parser for titles and notes, and `tk` has no JSON for `show`. Parsing the files directly is as easy as shelling out, and the writes `tk` offers cover only status, notes, deps, links, create. Due, snooze, and arbitrary frontmatter edits need a file edit either way.

## Q1: on-disk format

Directory: `.tickets/` at the repo root; found by walking up parents from `$PWD`, or forced by `TICKETS_DIR` env. One file per ticket, `<id>.md`, flat (no subfolders, no lanes). The file is never renamed or moved on status change. (source `find_tickets_dir`, `cmd_create`)

Real output of `cmd_create` (the exact echo lines):

```
---
id: nw-5c46
status: open
deps: []
links: []
created: 2026-01-19T03:10:30Z
type: task
priority: 2
assignee: Alex            <- only if set; defaults to `git config user.name`
external-ref: gh-123      <- optional
parent: nw-1a2b           <- optional, line absent otherwise
tags: [ui, backend]       <- optional, line absent otherwise
---
# Water the plants

Description text.

## Design

...

## Acceptance Criteria

...

## Notes

**2026-01-19T04:00:00Z**

Note text.
```

Field by field:

- **Id**: `<prefix>-<4 random a-z0-9>`. Prefix is the first letter of each `-`/`_` segment of the current directory name, or the first 3 chars if that is under 2 letters. Id equals the file name and the `id:` line. No collision check on create (4 chars, 36^4 about 1.7M) -> **Inference**: a clash silently overwrites an existing file, because `create` writes with `>`. Partial id matching: `ticket_path` accepts any substring of the file name if exactly one matches. (source)
- **Status**: `open`, `in_progress`, `closed` (validated list). `ls`/`closed` also treat `done` as closed when reading. No `closed` timestamp; `tk closed` sorts by file mtime. (source `VALID_STATUSES`, line 779)
- **Close / reopen**: rewrites the `status:` line in place. `reopen` sets `open`. Same file, same id. (source)
- **Due date, deferred, snoozed**: none. No field, no status. Grep for `due|snooze|defer` in the script finds nothing. (searched, found nothing)
- **Parent / sub-task**: `parent: <id>` frontmatter line, set only at create time via `--parent` (validated and resolved to the full id). No command changes it later. Parent does not touch the parent's `deps`, and `dep tree` ignores it (issue #46, 2026-02-24: "`tk create --parent` creates a ticket with a `parent` field ... and does not update the `deps` field"). Issue #56 "List parent tickets" and #19 "Visible hierarchy in ID" are open. (read in full, B/C)
- **Deps / links**: `deps: [a, b]` (blocks readiness) and `links: [a, b]` (symmetric, both files updated). Inline flow-style arrays.
- **Tags**: `tags: [a, b]`, only via `--tags` on create; `ls/ready/blocked/closed -T <tag>` filters. No command to add or remove tags afterwards. (source)
- **Assignee**: `assignee:` line; `-a` filter on list commands.
- **Comments / notes**: appended to the body under a `## Notes` heading, each as `**<UTC ISO timestamp>**`, blank line, text. No author field. `add-note` takes args or stdin. (source `cmd_add_note`)
- **Title**: the first `# ` heading in the body, not frontmatter. Not in `tk query`.
- **Fields a task "for the human" could be marked with** (Q1): the `assignee` value (note it defaults to the git user name, so many tickets are assigned to the human by accident), a tag such as `human` (`tags: [human]`, filterable with `-T human`), or `type`. There is no convention. Issue #39 (2026-02-07, open) asks for a draft/pending status for "needs human input" and says today one must "invent ad-hoc tags"; a commenter built `tk ask` / `tk tell` / `tk answer`, which "files an issue assigned to user" (highlights read, C). **Inference**: marking by tag `human` or assignee, chosen by config, is the only fit.

## Q3: concurrent writes

- Writers: `update_yaml_field` goes through `_sed_i`, which writes `<file>.tmp.$$` then `mv` over the original. Atomic replace, but it is read-modify-write with no lock or version check, so two writers in the same moment lose one update. `add-note` and `create` write with plain `>>` and `>` (no temp file, not atomic). `link` also uses `.tmp` plus `mv`. No `flock`, no lock files, no etag, no mtime check anywhere (grep of the script, read in full, B).
- **Inference**: a stray `file.md.tmp.<pid>` can be left if the process dies; it ends in `.tmp.NNN`, not `.md`, so the `*.md` glob ignores it.
- **Inference (bug risk)**: `update_yaml_field` runs `sed "s/^status:.*/status: .../"` over the whole file, not only the frontmatter. A note or description line that begins with `status:` would be rewritten on the next status change. Not reported upstream that I found.
- No file watcher; `tk` is a one-shot script. Others' guidance on watching: none in README. The #39 commenter's `tk answer` "watches for new ask/tell" (method not stated).
- Searched issues for concurrency, lock, race, clobber: no hits. No reported data loss. Issue #3/#2 are unrelated. README's pitch is "without ... a rogue background daemon mangling your changes", so concurrency is by design left to git. (C)
- Git is the sync layer: tickets are committed files, merge conflicts are normal git conflicts. Per-ticket files keep these rare.

## Q4: things that make two-way sync hard

- Good: the id is stable (never renamed, equals file name), one file per ticket, status flips in place, closing never moves the file. A poller can key on `id` and hash the file.
- Hard or missing:
  1. No due, snooze or defer concept. Meat Mode's "defer until a time" must be a new frontmatter key (for example `defer-until:`) that `tk query` will pass through as a string (it emits every key it finds, so extra keys survive) and that `tk ready` will ignore. **Inference**: the bridge would store it, and the hub would hide the item until then; ticket itself never wakes it.
  2. `parent` and `tags` can only be set at create. Making a sub-task from the app works through `tk create --parent`. Editing tags needs a direct file edit.
  3. Frontmatter is parsed by `sed`/`awk` line patterns, not a YAML parser. Keys must sit at column 0 as `key: value`, arrays inline `[a, b]`. A file edited by an agent or by a YAML library into block style (`tags:\n  - a`) breaks `tk` filters, `ready` and `query` (**Inference** from the awk patterns, lines 669-677 and the query plugin). A bridge writing files must keep this shape.
  4. Title and notes are body text with no structure beyond `# ` and `## Notes` plus `**timestamp**`; the bridge must parse them itself.
  5. Id collisions on create are unchecked (see Q1).
  6. No dismiss or "wontfix" status; closed is the only terminal state. Dismiss would map to `closed` plus a note, or a tag.
  7. Release lag: unreleased `main` differs from v0.3.2 (plugins), so installed behaviour varies by install route. `ls`, `query`, `edit` are plugins only on main; in v0.3.2 they are built in.
  8. `$TICKETS_DIR` and parent-directory lookup mean a repo can be detected by `.tickets/` but a nested repo may resolve to an outer one.
- No server owns the files.

## Gaps

- Did not read the behave tests or the Go port (https://github.com/kardianos/ticket, issue #24) for format drift; did not search beyond GitHub for third-party writers of `.tickets/` or horror stories (budget).
- Did not run `tk` to confirm exact `ls`/`ready` text output; formats above come from source.

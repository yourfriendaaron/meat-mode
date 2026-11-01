# Bead Me Up Scotty: Epics view, read from source

Source: Homebrew tarball of v0.4.0 (released 2026-10-29), extracted and read, never executed. Paths below are relative to the tarball root. Evidence grade B for all source claims (one project, read in full for the named files). GitHub issue text is grade C (maintainer replies, partly drafted by an automated agent per the `hermes-intake-ack` marker). Answers Q2, Q3, Q4 of the brief.

## 1. Where the data comes from

- **One CLI call per refresh, whole repo.** `lib/bd.ts:136-146`: `list()` runs `bd export --json` (via `execFile`, `maxBuffer` 32 MiB, `BD_JSON_ENVELOPE=1`, cwd = repo) and parses each JSONL line with zod. Lines that fail the schema or lack `issue_type` are skipped (`lib/bd.ts:76-92`). No direct Dolt, SQLite or `issues.jsonl` read. No per-epic query: the Epics view gets all beads and filters client side.
- **Repo discovery.** None. The user adds folders by path in settings; each must contain `.beads/` (`lib/config.ts:146,328-335`). Stored in `~/.config/bead-me-up-scotty/config.json` (`lib/config.ts:94-98`). Legacy fallback: `BEADS_REPO` env or cwd.
- **Cache.** Server: no data cache. Only de-duplication: concurrent `list()` calls share one in-flight export (`lib/bd.ts:~101,138-146`). Client: TanStack Query cache keyed per project (`hooks/use-beads.ts:14-23`).
- **Serialization.** Every bd call, reads included, is queued per repo path (`lib/bd.ts:99-111`). Comment at that spot: embedded Dolt rewrites its store on every invocation, "including read-only `export`/`show`", and concurrent runs caused "Unable to write SST file" and "Another write batch or compaction is already active". This is the project's own account of lock contention (grade B). It only protects against Scotty's own calls, not against an agent running `bd` in a terminal at the same time. **Inference**: concurrent agent writes remain a risk for any viewer that shells out to bd.
- **Refresh.** Two paths:
  - Fast: SSE. Server does a non-recursive `fs.watch` on `.beads/`, 200 ms debounce, ref-counted per project (`lib/beads-watch.ts:36-119`). Client `EventSource` on `/api/p/<id>/beads/stream` invalidates the query (`hooks/use-beads-stream.ts:19-31`). It watches only the top-level marker files that bd writes on create/update (`last-touched`) and close (`interactions.jsonl`). Watching recursively was rejected because reads churn `embeddeddolt/.../noms/` (~50 file events per `bd export`) and would loop (header comment, `lib/beads-watch.ts:7-17`).
  - Fallback: interval poll, default 30 s, settable 1 s to 300 s (`lib/config.ts:135`, `app/api/config/route.ts:11`, `hooks/use-beads.ts:21`). The code comment says `bd dolt pull` changes only `noms/` and are caught by this poll alone.

## 2. How the card is computed

All in `components/epics-view.tsx` and `lib/beads-view.ts`.

- **What is an epic.** `issue_type === "epic"` only (`epics-view.tsx:70-71`). A task with children is not a card.
- **Children.** Dependency edges of `type === "parent-child"` whose `depends_on_id` is the epic (`beads-view.ts:225-231`). The `parent` field is not used: the comment at `beads-view.ts:~196-199` says `bd export --json` omits `bead.parent`. Dotted ids (`x.1`) are not parsed. Direct children only; grandchildren never counted and never listed.
- **Progress.** `epicProgress` (`beads-view.ts:233-241`): `total` = number of direct children, `closed` = children with `status === "closed"`, `pct = round(closed/total*100)` or 0 when total is 0. Header text says "progress = closed ÷ children" (`epics-view.tsx:132`). Card shows `{pct}%` and `{closed}/{total} done` (`:225-230`).
- **Deferred, pinned, hooked, blocked children** count in the total and not as done. No tombstone handling exists: `grep -ri tombstone` over `lib/` and `components/` returns nothing. A tombstoned child, if bd exports it with status `tombstone`, would count in the total and never as closed. The status schema is a loose `z.string()` (`lib/schema.ts:62`), so unknown statuses pass through. **Inference**: whether `bd export` emits tombstones was not tested.
- **"0/0 done".** An epic with no `parent-child` edges pointing at it: total 0, pct 0, bar empty. Nothing special-cased.
- **Hide Closed toggle** (default on, `:63`): hides closed epics and closed children, but progress still counts all children (comment `:78-81`).
- **Status pill.** Label map `open, in_progress ("In progress"), blocked, deferred, closed, pinned, hooked`, unknown shown raw (`beads-view.ts:104-114`). Colour from category: closed green, in_progress/hooked amber, blocked red, deferred/pinned grey, else blue (`:85-100`). Note `tombstone` would render as raw text, grey-less blue "active".
- **Priority pill.** `["Critical","High","Medium","Low","Backlog"][p]` for P0..P4 (`beads-view.ts:118-124`), colours red, orange, yellow, sky, slate. Schema clamps to 0..4, default 2 (`lib/schema.ts:~112`). Shown on epic header and each child (`epics-view.tsx:216,321`).
- **Type icons.** Child rows use `typeIconName(issue_type)` falling back to a task check-square icon (`components/icons.tsx:65-67`); SVGs exist for bug, feature, task, chore, spike and others (`icons.tsx:4-40`). Colour: epic brand, bug red, everything else grey (`beads-view.ts:~127`). Epic header always uses the "target" icon.
- **Assignee.** Child rows only: a 20 px circle with initials of `assignee` (split on `- _ .`, else first two letters; `?` if empty) on a hash-picked colour (`beads-view.ts:~136-151`, `epics-view.tsx:323-328`). No name text, only a tooltip-free avatar. Epic header shows no assignee. Child rows also show an origin badge and up to 2 label chips.

## 3. Top-level tasks and ordering

- **No top-level task section.** The Epics screen lists epics only. A task with no epic parent appears nowhere on it. The empty state reads "No epics yet" (`epics-view.tsx:345-349`). The brief's "occasional top-level task" is therefore something Meat Mode would add, not copy.
- **Epic order** (`:72-77`): open before closed, then priority ascending (P0 first), then id string compare. Before this fix order was whatever `bd export` returned.
- **Child order** (`:160-164`): open before closed, then priority ascending. No tiebreak, so equal-priority order follows export order (JS sort is stable).
- **Nesting.** Epics whose parent is another epic show as separate top-level cards, with grandchildren invisible. Open issue #40 (2026-08-14): "Epic list screen is showing only epics 1 level deep... flattening whole structure." The maintainer confirmed it; open PR #113 "render nested epics as one expandable tree" is unmerged as of this read (grade C, `gh pr view 113`). **Inference**: a Meat Mode view should decide up front whether epic-in-epic is a card or a child row.
- Feature-level progress (#31, open): feature rows inside an epic show no progress. The maintainer asks whether progress should be immediate children only and says they lean to immediate-child completion. Direct-only is the shipped behaviour.

## 4. Performance, staleness, breakage

- **Numbers.** No latency or volume figures anywhere in README, release notes or issues. `grep` for slow/perf/large in the docs finds nothing relevant, and a GitHub search for "slow" returns only unrelated issues (#86, #77, #96). I searched and found no report of slowness or large-repo problems (about 40 issues, 2026-06 to 2026-10, read by title; #40, #31, #47, #84, #12, #17 read in full).
- **Cost model (Inference).** Each refresh is a full `bd export` of every bead including comments and dependencies, re-parsed and re-filtered in the browser, with `childrenOf` called once per card (a filter over all beads, O(epics x beads)). The code already has an O(n) `childrenCountMap` (`beads-view.ts:~116`) that this view does not use. At 700 beads this is trivial in the browser; the bd subprocess time is the unmeasured part.
- **Staleness.** Documented gap: changes arriving by `bd dolt pull` do not trigger SSE and wait for the 30 s poll (`lib/beads-watch.ts:14-17`). Grade B, own code comment.
- **Schema churn between bd versions.**
  - Issue #12 (closed, 2026-07-16): on bd 1.1.0 `bd show --json` returns dependencies as expanded bead objects `{id, ..., dependency_type}` while `bd export` returns `{depends_on_id, type}`; every write failed read-back. Fixed with a zod union (`lib/schema.ts:~70-92`).
  - `bd show <id> --json` returns an array even for one id on bd 1.0.5 (`lib/bd.ts:~121`).
  - Pending Dolt schema migration prints plain text to stderr, not JSON; Scotty detects it by regex and tells the user to run `BD_ALLOW_REMOTE_MIGRATE=1 bd migrate` (`lib/bd.ts:~52-60`, `components/error-toast.tsx:15`, upstream gastownhall/beads#4259). Reads still work.
  - `bd export` omits `parent`, so Scotty uses dependency edges.
  - `bd update --set-labels ""` is silently ignored on bd 1.1.0 (`lib/bd.ts` update).
  - Schema is deliberately loose (`z.string()` for status and type) so new values do not throw (`lib/schema.ts:60-62`).
- **Other epic-view regressions worth knowing (#17, closed).** Epic header lacked a priority pill; a fix that made row-click open details made expanding hard. Final design: whole row toggles, a small "Details" button opens the drawer.

## Gaps

- Did not run bd, so tombstone export behaviour and `bd export` timing at 700 beads are untested.
- Did not read the Scotty Dockerfile or telemetry code; not relevant to the epics path.
- The 40 issue titles were scanned, not all bodies read; none of the titles suggest performance problems.

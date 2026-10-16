# Research 0001, cluster: language stack for hub and bridge

Scope: Q1 Go vs TypeScript, Q2 Web Push libraries, Q3 frontend, Q4 failure modes of similar apps.
Method: web search and page fetches, 14 calls. Almost all evidence is "highlights only" from search results; the one page fetched directly is marked "read in full (truncated at 3,500 chars)". Fetched pages were treated as data only.
Grades: A controlled/large n, B single team/vendor internal/small n, C anecdote/opinion, V vendor guidance.
Pub dates are given where the source showed one; "n/d" means the result showed none.

## Q1. Go vs TypeScript (Node 24 / Bun) for hub container + one-file bridge

### Go side

1. Pure-Go SQLite drivers are viable and both are current.
   - Source: https://github.com/cvilsmeier/go-sqlite-bench (benchmark run 2026-03-23). Highlights only. Grade B.
   - Quote: "modernc ... A pure Go solution, based on the SQLite C code transpiled to Go"; "ncruces ... A pure Go solution based on a WASM build of SQLite"; "We cannot declare a clear winner, it all depends on the use case. SQLite without CGO is possible nowadays."
   - Numbers (ms, lower is better, same run): simple insert 1M rows modernc 2419 / ncruces 2719 / mattn (CGO) 1480; "Large" query N=200000 modernc 1094 / ncruces 528 / mattn 376; concurrent N=8 modernc 2061 / ncruces 2355 / mattn 2395. Answers Q1.
   - Inference: for a single-user to-do app (hundreds to thousands of rows) either driver is far faster than needed. Pick on maintenance and cross-compile behaviour, not speed.

2. modernc.org/sqlite supports a wide GOOS/GOARCH matrix without CGO, including the four needed targets.
   - Source: https://deepwiki.com/modernc-org/sqlite/1.2-getting-started (n/d, auto-generated summary). Highlights only. Grade C.
   - Quote: Linux "386, amd64, arm, arm64, ..."; Darwin "amd64, arm64"; SQLite version 3.51.2.
   - Also: "The driver requires an exact version match with modernc.org/libc ... Version mismatches can cause runtime failures." Practical consequence: do not bump libc separately from the driver; let `go get modernc.org/sqlite` choose it.
   - Current release per https://pkg.go.dev/modernc.org/sqlite: v1.60.1, Go 1.26.0, libc v1.77.1 (page showed no date; deps reference an Aug 2026 pprof pseudo-version). Highlights only. Grade B.

3. ncruces/go-sqlite3 is also cgo-free, embeds SQLite as Wasm translated to Go (wasm2go), and uses a Go-implemented VFS.
   - Source: https://github.com/ncruces/go-sqlite3 (n/d). Highlights only. Grade V.
   - Quote: "Because each database connection executes within a Wasm sandboxed environment, memory usage will be higher than alternatives."; "Go and x/sys are the only direct dependencies."
   - GoToSocial's migration experiment (https://github.com/ncruces/go-sqlite3/discussions/78, 2024-04-17, highlights only, grade B) reported it worked on Linux and macOS and that its busy-timeout handler could be relied on, with one open issue on FreeBSD (#85) and a leak that turned out to be their own wrapper code.

4. Known modernc footgun: mattn-style DSN pragmas are silently ignored.
   - Sources: https://github.com/opentalon/opentalon/pull/314 and https://github.com/Cawlumm/lyftr/pull/32 (both n/d). Highlights only. Grade C (two independent project reports, same cause).
   - Quote: DSN "?_journal_mode=WAL" is one "the modernc.org/sqlite driver silently ignores"; correct form "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", which "modernc applies ... on every connection it opens". A one-off `PRAGMA busy_timeout` only reaches one pooled connection. Lyftr load check: "300 concurrent writes -> 300/300 succeed on this branch vs 21/300 on main".
   - Inference: whichever driver is picked, set busy_timeout and WAL in the DSN, and assert them at startup.

5. modernc prepared-statement performance bug was fixed Dec 2025.
   - Source: https://github.com/lbe/sqlite-read-benchmark (updated 2026-02-22). Highlights only. Grade C.
   - Quote: "The current version 1.46.1 of modernc.org/sqlite now shows the expected behavior that prepared are faster than raw, 39% faster"; ncruces still "314% faster for my needs" in one read-heavy benchmark. Single author, narrow workload. Not decisive for this app.

6. Go cross-compile story. Pure-Go drivers mean `CGO_ENABLED=0 GOOS=... GOARCH=...` for linux/darwin x amd64/arm64 from one machine.
   - I did not find a page that states this for the drivers beyond the platform table above. **Inference** from items 1 to 3: no C toolchain or zig is needed. Not tested here.
   - SQLite WAL-reset corruption bug: "present in all version of SQLite from 3.7.0 through 3.51.2. It is fixed in version 3.51.3 (2026-03-13)" (https://sqlite.org/wal.html and https://www.sqlite.org/howtocorrupt.html, n/d, highlights only, grade V). It needs two or more connections writing or checkpointing at the same instant. modernc v1.60.1 bundles an unknown SQLite version (DeepWiki shows 3.51.2 at an older point); **check go.mod resolves a SQLite >= 3.51.3** or keep to one writer connection.

### Bun side

7. `bun build --compile` cross-targets cover all four needed platforms.
   - Source: https://bun.com/docs/bundler/executables (n/d). Highlights only. Grade V.
   - Quote: targets listed include `bun-linux-x64`, `bun-linux-arm64`, `bun-darwin-x64`, `bun-darwin-arm64` (plus musl and Windows). "On x64, Bun ships a single binary that targets Nehalem (SSE4.2) and selects AVX2/AVX-512 code paths at runtime."
   - Docs say of size: "Overall though, Bun's binary is still way too big and we need to make it smaller."

8. Binary size is large. Hello world is tens of MB; the runtime floor is about 57 MB.
   - Source: https://github.com/oven-sh/bun/pull/32262 (n/d, open PR). Highlights only. Grade B.
   - Quote: stripped `bun` 67.13 MB, proposed `bun-standalone` 59.50 MB; "< 35 MB isn't reachable ... JSC 22.9 MB + ICU data 23.7 MB + bindings/crypto/codecs ~ 57 MB floor."
   - Older reports: 57 MB hello world (Bun 1.1.30, darwin-arm64) in https://github.com/oven-sh/bun/issues/14546 (2024-10-14, grade C); 91 MB in https://github.com/oven-sh/bun/issues/5854 (n/d, grade C).
   - Bun 1.4.1 release notes (https://bun.com/blog/bun-v1.4.1, 2026-09-04, grade V): "Claude Code's install 45% smaller, from 376 MB to 207 MB" via smaller bytecode. Shows size is still moving.

9. bun:sqlite works under --compile but only with target bun, and the bundler has had sqlite edge bugs.
   - Sources: Bun docs above ("You can use bun:sqlite imports with bun build --compile"); https://github.com/oven-sh/bun/pull/38288 (n/d, open PR, grade B): `--target node` builds of a sqlite-attribute import "exits 0" and then fail at runtime. Third-party sqlite libraries under --compile failed in https://github.com/oven-sh/bun/issues/14301 (2024-10-02, grade C).
   - Recent compile bugs fixed in 1.4.1 (grade V): "darwin-arm64 in rare cases failed `codesign -v` and were killed on macOS 27"; a 1.4.0 regression where compile "failed with EACCES" under a WSL2 path. Inference: Bun compile is under active repair on the exact target (macOS arm64) the bridge needs.
   - Bun compile output on Linux is glibc by default; musl needs a separate target (docs).

### Node side

10. Node SEA is still early and cross-building has caveats.
    - Source: https://nodejs.org/api/single-executable-applications.html (v26.12.0 docs, n/d). Highlights only. Grade V.
    - Quote: "Stability: 1.1 - Active development"; cross-platform SEAs require `useCodeCache` and `useSnapshot` false "to avoid generating incompatible executables"; CI tests "macOS (arm64 only; x64 is not currently supported and is skipped in the tests)" and Linux "except Alpine".
    - Built-in `--build-sea` arrived in v25.5.0 (https://nodejs.org/en/blog/release/v25.5.0, n/d, grade V), so Node 24 uses the older `--experimental-sea-config` + postject path. **Inference** from release notes: Node 24 lacks `--build-sea`.
    - A SEA embeds the whole node binary (roughly 100 MB class). I found no size figure in these sources; not cited as fact.

11. node:sqlite is a release candidate in recent Node, not stable.
    - Sources: https://beta.docs.nodejs.org/sqlite (Node 26.10.0 docs) "Stability: 1.2 Release candidate"; commit https://github.com/nodejs/node/commit/aaf9af1672 (author date 2026-02-21). Highlights only. Grade V.
    - **Inference:** Node 24 predates this change and most likely still shows "1.1 active development" with an experimental warning. Not verified against Node 24 docs.

### Q1 synthesis (**Inference**, all)
- The bridge must be a native artifact on 4 targets. Go gives ~10 to 20 MB static binaries with trivial cross-compilation. Bun gives >=57 MB floor and macOS codesign fixes still landing. Node 24 SEA is "active development" with a documented cross-platform crash caveat. Go wins on the bridge.
- Using one language for hub and bridge favours Go: one module, one CI matrix, shared types.
- Cost of Go: frontend still needs a JS toolchain at build time (see Q3), and the bridge needs to shell out to `bd` or call its HTTP API either way.
- Driver pick: modernc (more platforms, larger user base, simpler memory model) vs ncruces (faster reads in two benchmarks, higher per-connection memory). Either is defensible; recommend modernc as default unless a read benchmark on real data says otherwise. No source compares them for a workload this small.

## Q2. Web Push libraries

12. Go: SherClockHolmes/webpush-go supports aes128gcm and VAPID. Upstream is quiet.
    - Source: https://github.com/SherClockHolmes/webpush-go/blob/master/webpush.go (n/d). Highlights only. Grade B (source read).
    - Quote: `contentEncryptionKeyInfo := []byte("Content-Encoding: aes128gcm\x00")` and `req.Header.Set("Content-Encoding", "aes128gcm")`; options include `Subscriber`, `VAPIDPublicKey`, `VAPIDPrivateKey`, `TTL`, `Urgency`, `Topic`, `VapidExpiration` (default now + 12 hours). 419 stars.
    - Releases page (https://github.com/SherClockHolmes/webpush-go/releases, n/d, highlights only) lists a golang-jwt bump for "CVE-2024-51744", `golang.org/x/crypto` bumps to 0.31.0 and a fix "remove content length header". I did not see a release date for the latest; I found no evidence of 2026 activity. Do not claim it is abandoned.

13. Go: ergochat/webpush-go v2 is an actively structured fork.
    - Sources: https://github.com/ergochat/webpush-go (n/d) and https://pkg.go.dev/github.com/ergochat/webpush-go/v2 (v2.0.0, published 2025-01-16). CHANGELOG read in full (truncated at 3,500 chars) at https://github.com/ergochat/webpush-go/blob/master/CHANGELOG.md. Grade B.
    - Quote: "This library is a fork of SherClockHolmes/webpush-go." v2.0.0 (2025-01-16) stores `Auth` as `[16]byte`, `P256dh` as `*ecdh.PublicKey`, keeps `VAPIDKeys` as `*ecdsa.PrivateKey`; "`SendNotificationWithContext` renamed to `SendNotification`". Top contributor slingamn (20 commits), upstream author 11.
    - I did not confirm a v2 release after January 2025. Check pkg.go.dev for the newest tag before pinning.
    - Inference: ergochat v2 validates keys on decode (stricter) and uses the stdlib `crypto/ecdh`. Prefer it over upstream for a new project.

14. npm `web-push` is the de facto standard but has not published since January 2024.
    - Sources: https://www.npmjs.com/package/web-push (v3.6.7, updated 2024-01-16, 10,802,181 weekly downloads, 641 dependents); https://inspect.software/software/web-push-libs/web-push (2026-08-13): "latest publish 975 days ago", "24 commits in the last year", "bus factor 1", "0 carry advisories". Highlights only. Grade B/C.
    - Inference: usable and widely deployed, but one maintainer and no release in ~2.7 years. Same maintenance profile as the Go upstream.

15. Platform constraints that affect either library choice.
    - Source: https://webkit.org/blog/16535/meet-declarative-web-push/ (2025-03-27, Brady Eidson, highlights only, grade V) and Apple docs https://developer.apple.com/documentation/usernotifications/sending-web-push-notifications-in-web-apps-and-browsers.
    - Quote: "WebKit requires you ... to always show a notification; no silent push messages are allowed"; "if an event handler doesn't show the user visible notification for any reason we revoke its push subscription". Subscription must start from a user gesture. iOS needs Home Screen install (iOS 16.4+, https://webkit.org/blog/13878/, 2023-02-16).
    - Declarative Web Push: a JSON message format lets a notification show without service worker JS and "Web Push messages become backwards compatible with browsers that do not yet support Declarative Web Push". Whether the Go library can send that JSON body: yes in principle (the payload is opaque bytes), not tested.
    - Practical rule for the service worker: every `push` event must call `showNotification`, even for errors.

## Q3. Frontend for a small offline-tolerant PWA

16. Framework runtime costs are small for Preact and Lit, larger for Svelte 5 at runtime but small per component.
    - Source A: https://github.com/naufalafif/realworld-js-framework-comparison (n/d), highlights only, grade C. "Perf Stress Test" gzipped JS: Preact 8.8 KB, Lit 10.0 KB, Svelte 18.1 KB, Solid 8.1 KB, React 62.4 KB.
    - Source B: https://mlgq.github.io/frontend-framework-bundle-size/ (versions: svelte v5.55.3, react v19.2.5; n/d), highlights only, grade C. Todo app after syntax minification (not gzip): svelte5 "Runtime 34.61 kB", total 39.25 kB; svelte4 total 11.08 kB; React 189.44 kB.
    - Source C: https://webcomponents.dev/blog/all-the-ways-to-make-a-web-component-mar2020 (2020, stale), minimal custom element gzip: vanilla ~558 B to 1.7 kB class range, Preact wrapper ~3.8 kB, Lit ~5.5 to 7 kB. Treat as old.
    - Different apps and measurement methods, so numbers are not comparable across sources. Takeaway only: Preact and Lit about 4 to 10 KB gzip, Svelte 5 larger fixed runtime, vanilla smallest.
    - I did not find a size for htmx or Alpine in this pass. Searched and found nothing usable; not asserted.

17. What small self-hosted apps choose: **not established.** I searched for framework comparisons but found no source that surveys single-user self-hosted apps. Evidence from Q4 hits shows Go apps embedding Vite/React builds (helmdeck, sqi, opendray), which is anecdote only (grade C).

18. Service worker for push has a minimum: handle `push` (always show), `notificationclick`, and optionally `fetch` for offline.
    - Source: https://github.com/david-randoll/push-notification-nextjs (n/d), highlights only, grade C. Notes that Chrome/Edge install prompts need a service worker with a `fetch` handler; iOS needs `manifest.json` with `"display": "standalone"` and a user gesture before awaiting anything in the subscribe call, or "Safari silently suppresses the prompt".

19. **Inference** for Q3 (not directly sourced): the UI is a list with keyboard shortcuts, swipe, forms, optimistic updates and an offline queue. That is state-heavy enough that hand-written DOM updates get fiddly past a few hundred lines. Preact (about 4 KB core, 8.8 KB with a typical app) is the best fit if a framework is wanted: JSX, no compiler requirement, tiny. Vanilla TS with a small render helper is viable if the view count stays under about five. Lit and Svelte add build or authoring style choices with little gain here. htmx suits server-rendered pages and cuts against offline queuing of actions, which this app needs. I did not benchmark any of this.

## Q4. How similar apps failed

20. SQLite WAL over network filesystems: official position.
    - Sources: https://sqlite.org/wal.html and https://www.sqlite.org/useovernet.html (n/d, highlights only, grade V).
    - Quote: "All processes using a database must be on the same host computer; WAL does not work over a network filesystem."; "network filesystem sync and locking reliability vary among implementations and installations. Rely upon it at your (and your customers') peril."
    - Forum (https://sqlite.org/forum/info/e90fd84ee89d028c, 2024-04-07, grade C): one user ran 3 instances over SMB for 12 hours without errors; maintainers replied reports "will be ignored or ... gently chided". Works until it does not.

21. Real apps hit it constantly via container bind mounts.
    - Sonarr: https://github.com/Sonarr/Sonarr/issues/1886 and /2797 (n/d), highlights only, grade C. Quote: "docker for windows and other virtualization systems using CIFS mounted host paths often fail with sqlite locking or corruption errors when using WAL"; NFS users saw "database is locked"; workaround `PRAGMA journal_mode=DELETE` to clear WAL files. Ask was an option to disable WAL.
    - Docker for Windows: https://github.com/docker/for-win/issues/11 (n/d, grade C): CIFS mounts, fix was the `nobrl` mount flag.
    - Jellyfin on k3s with NFS: https://github.com/linuxserver/docker-jellyfin/issues/27 (2020-04-03, grade C). Maintainer: "Don't put databases on remote mounts ... we generally recommend /config folders to reside on local folders."
    - Trilium on SMB: https://github.com/TriliumNext/Trilium/issues/3987 (2023-05-31, grade C), better-sqlite3 "database is locked" at startup, reached before any user action.
    - Relevance: the brief says the hub can run on "any always-on host" (a NAS is likely). Unraid-style hosts commonly put app data on user shares. **Inference:** the hub needs a documented rule (data dir on local disk, not a network mount), a startup probe, and ideally a journal-mode setting (`DELETE` fallback) with a clear log line.

22. Volume permissions: root-owned bind mount plus non-root image gives SQLITE_CANTOPEN.
    - Sources: https://agents.stackoverflow.com/tils/0fe6e609-652e-489c-8c0d-bd69469f5639 (n/d, grade C, community-confirmed); https://github.com/vavallee/bindery/issues/5 (n/d, grade C); https://github.com/netclaw-dev/skill-server/pull/141 (2026-07 per content, grade C); https://github.com/kbennett2000/backscatter/commit/fa17db4 (n/d, grade C).
    - Quotes: "Docker automatically creates a host directory as root:root if the directory specified in a bind mount does not exist"; "SQLite ... cannot create the file or its journal/WAL in a directory it cannot write — hence CANTOPEN rather than a clearer permission error"; "wiping or deleting the DB file does NOT fix this"; "Dockerfile chown commands are ineffective for bind-mounted paths because the bind mount shadows the image filesystem".
    - Fixes seen: a root init container that chowns then exits (skill-server); a preflight that fails with "Directory X is not writable by UID Y" (bindery proposal, backscatter entrypoint); allow a PUID/PGID setting.
    - **Inference:** the hub should (a) create and `stat` its data dir at startup, (b) log the resolved path, owner and UID on failure, (c) offer a `--user` or PUID/PGID recipe.

23. Go `embed` of frontend assets: stale or mismatched `dist`, and SPA fallback masking 404s.
    - helmdeck issue: https://github.com/tosin2013/helmdeck/issues/180 (2026-05-13), highlights only, grade C. Published image's `index.html` referenced hashed assets not in the embedded FS; SPA fallback returned HTML for `/assets/*`, "blank Management UI on fresh pulls". Suggested fixes: build frontend inside the Dockerfile; smoke test in release CI; startup check that `index.html` references exist in the FS.
    - sqi build doc: https://uberware.github.io/sqi/web-build/ (n/d), highlights only, grade C. "A bare `go build` embeds whatever is already in `web/dist/` — stale output if you skipped the web build." Plain `//go:embed dist` excludes dot and underscore files (use `all:` to include); track a placeholder `index.html` so embed has a file; `index.html` `no-cache`, hashed assets cached.
    - Philipp Lehmann 2026-03-03 (https://philipptheserver.com/posts/go-embed-spa/), grade C: "the frontend has to already be built ... before go build runs"; fallback must not swallow missing files with extensions, which should 404.
    - opendray embed.go (https://github.com/Opendray/opendray/blob/main/internal/web/embed.go, n/d): `Cache-Control: public, max-age=31536000, immutable` on `assets/`, 503 with build instructions when dist is missing.

## Gaps
- No survey of what small self-hosted to-do/notes apps pick for frontend (Q3 item 17).
- No htmx, Alpine or vanilla bundle numbers; framework numbers come from unrelated mini-benchmarks (grade C).
- Node SEA binary size and Node 24 `node:sqlite` stability label not verified directly.
- Latest ergochat/webpush-go tag after v2.0.0 (2025-01-16) not confirmed; SQLite version bundled by current modernc not confirmed against 3.51.3 fix.
- No direct test of cross-compile with either pure-Go driver; claims in item 6 are inference.
- Nothing tested in code; no source compares modernc and ncruces on a to-do-sized workload.

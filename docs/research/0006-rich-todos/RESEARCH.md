# Richer to-dos: Markdown, attachments and the export

What this settles for tags, Markdown and attachments (the meat-jmf epic): which Markdown renderer the app uses, where the hub keeps files, and how an export carries them. Reader notes, with every source, are beside this page: [markdown.md](markdown.md), [storage.md](storage.md) and [export.md](export.md). The brief they worked from is [brief.md](brief.md). Grades: A controlled or large n, B one team or small n, C anecdote, V vendor guidance.

## Markdown renderer

- markdown-it's Safety page says that with HTML off the "output will be safe without sanitizer". Its default `validateLink` refuses `javascript:`, `vbscript:`, `file:` and most `data:` addresses (V). Its one XSS CVE, CVE-2025-7969, came from a highlighter the user wrote and is disputed (B). Its other advisories are denial-of-service bugs in typographer, linkify and one newline rule, all off by default or fixed (V).
- marked does not sanitize and its docs send users to DOMPurify, so it always needs two packages (V). DOMPurify has a real bypass history, including a CVSS 10 nesting mXSS in 2024 and another fixed in 3.3.2 in 2026 (A).
- Size, min+gzip: markdown-it about 46 KB with six small dependencies; marked 12.9 KB plus DOMPurify about 10.4 KB; micromark 14.6 KB (B, registry and bundlephobia). The app ships chrono-node already, so 46 KB is the largest single cost of the pick.
- None of them uses `eval`, so the page's CSP stays as it is (**Inference**, from reading their sources; the lane checks it with a `securitypolicyviolation` listener in an e2e test).

**Inference:** markdown-it with `html: false`, `linkify: false` and `typographer: false` makes the safe output the default rather than a second step, and the input has no HTML that is needed. A narrower `validateLink` (http, https, mailto) and an image rule that turns images into links close the rest. A unit test feeds it hostile input and asserts that no tag or attribute outside an allowlist comes out, which is cheaper than a sanitizer that has bypasses of its own. Pin it exactly; prefer a release at least two weeks old.

## Where the hub keeps files

- SQLite's own measurements: blobs under about 100 KB read faster from the database, larger ones from separate files (B, SQLite "Internal Versus External BLOBs"). Most attachments (photos, PDFs) are larger.
- A large write grows the WAL, which a checkpoint does not shrink without `journal_size_limit` (V). **Inference:** the hub runs one database connection, so a 25 MiB blob write would also hold every other write.
- Vikunja keeps files on disk with a 20 MB default cap; Paperless-ngx and Nextcloud Deck use the filesystem; Memos defaults to the database and its docs name the cost, a larger database and heavier backups (V, C). No source gave a default total cap.
- Serving uploads from the app's own origin is safe with `nosniff`, `Content-Disposition: attachment`, a `sandbox` CSP and a safe `Content-Type`, inline only for a sniffed list of raster images; never SVG or HTML inline (V, web.dev and MDN).
- Atomic write: temp file in the same folder, sync, rename, sync the folder; sweep stray temp files at start (B).

**Decided:** files in `attachments/<id>` in the data directory, metadata in SQLite; 25 MiB a file, 20 an item, 1 GiB in all; any type taken, served as a download unless it sniffs as PNG, JPEG, GIF or WebP.

## How the export carries files

- Every app of this kind that restores its own attachments (Memos, Vikunja, Joplin, Trilium) exports one zip or tar of metadata plus raw files; none puts files in JSON as base64 (V). Memos, the nearest match (Go, SQLite, one user), records each file's size and SHA-256 and checks them before it applies anything (V).
- Todoist's backup keeps only links to attachments, so the files are lost: the failure this design must avoid (V, C).
- Base64 grows files by a third; Go's `archive/zip` writes as it streams but reads only from an `io.ReaderAt`, so an uploaded zip is spooled to a file first. Zip-slip and zip-bomb defences are the reader's job: never use entry names as paths, limit each entry to its listed size, cap the entry count (A, B: Go docs and issues 10568 and 49791).

**Decided:** the export becomes a zip of `export.json` (version 2, which lists each attachment with its size and SHA-256) and `attachments/<id>`. A restore checks every file against the JSON before writing anything and refuses a missing or mismatched one; it never skips a file. A bare JSON of version 1 still restores.

## Call

**Proceed**, with the three decisions above. The contract is in [docs/api.md](../../api.md): Tags, Descriptions and notes in Markdown, Attachments, and Export and restore.

# Cluster C: export/backup format with attachments

Searched with Exa (about 10 calls). Grades: A controlled/large n, B single team or small n, C anecdote/opinion, V vendor guidance. Most sources here are vendor docs or source code, so V or B. No publication dates were returned for most pages; "n/d" means no date returned. Today is 2026-10-19.

## Q1: How comparable apps export data with attachments

### Summary table

| App | Container | Attachments | Source grade |
|---|---|---|---|
| Memos 0.31+ | ZIP: `manifest.json`, `memos/<uid>.json` + Markdown, `attachments/<uid>/<name>` | Bytes included, SHA-256 + size recorded per attachment | V (docs + Go package) |
| Vikunja | ZIP of JSON + files | Included | V |
| Trilium | ZIP, `!!!meta.json` (formatVersion 2) + note files + attachment files | Included as separate files | V (source) |
| Joplin | JEX = tar; `.md` per item with metadata trailer; `resources/` dir | Included | V |
| Todoist | ZIP of CSVs | Links only, bytes not included | V |
| Notion | ZIP of Markdown/CSV/HTML | "along with any files you've uploaded" | V |
| Google Takeout | ZIP or TGZ parts, 1-50 GB each | Files included, sidecar JSON per item | V/C |

### Evidence

- **Memos (closest analogue: SQLite, single user, attachments in DB, on disk, or S3).**
  - URL: https://usememos.com/features/export (n/d). Quote: "Export packs your memos, their metadata, and attached files into one ZIP". "Attached files included, whether stored in the database, on disk, or in S3". "The ZIP is not encrypted". Answers Q1. V. Highlights only.
  - URL: https://usememos.com/docs/usage/export-import (n/d). Quote: "External attachment links remain links; their remote files are not downloaded into the ZIP." Import "uploads and validates it before showing a preview; this step does not create or replace memos", then reports "Created, Updated, Skipped, and Failed counts, plus warnings and individual failures". Conflict choices: Keep mine (default), Replace, Create a copy, matched by memo ID. Answers Q1, Q3. V. Highlights only.
  - URL: https://pkg.go.dev/github.com/usememos/memos/core/memoexport (2026-09-19). Constants: `Format = "memos-export"`, `FormatVersion = "1.0"`, `ManifestEntry = "manifest.json"`, `AttachmentsDir = "attachments/"`. Attachment struct has `Size int64` and `SHA256 string` (`omitempty`). Quote: "Writer streams a Memos export file to an io.Writer ... a caller that wants attachment digests in a memo record writes the attachment bytes before the record." `WriteAttachment` "returns the digest and length it observed". Also: the media type is "deliberately not stored inside the container: a leading 'mimetype' entry makes macOS Archive Utility treat the file as an unknown document package and refuse to expand it." Answers Q1, Q2, Q3. V. Highlights only.
  - URL: https://usememos.com/docs/operations/backup-restore (n/d). A personal export "does not replace the instance backups"; real backups are `sqlite3 .backup` plus a tar of `assets/`. Quote: "treat attachment recovery as a first-class part of restore, not an afterthought". Answers Q1. V. Highlights only.
- **Vikunja.** https://vikunja.io/help/import-and-export/ (n/d): "Vikunja will generate a ZIP file containing all of your data" including "projects, tasks, attachments". Source on GitHub (`pkg/models/export.go`, `pkg/modules/dump/dump.go`) uses `utils.WriteFilesToZip`. Export skips an attachment whose file is missing on disk (`errors.As(err, &pathError)` then `continue`). Import (`vikunja-file/vikunja.go`) logs a warning and `continue`s when an attachment file is absent from the zip or cannot be opened. Answers Q1, Q3. V (source). Highlights only, so the exact import branch conditions are not confirmed.
- **Trilium.** https://github.com/TriliumNext/Trilium/blob/255a9b2b/packages/trilium-core/src/meta.ts (n/d): `NoteMetaFile { formatVersion: number; appVersion: string; files: NoteMeta[] }`, `AttachmentMeta { attachmentId, title, role, mime, position, dataFileName }`. Export (`export/zip.ts`) uses `archiver("zip", {zlib:{level:9}})` and appends each attachment as a file next to its note. Docs say a ZIP export loses data a backup keeps: "Backups always contain the entire structure, as well as additional information a ZIP export does not have: maintains note IDs, contains the options/tokens". Answers Q1. V. Highlights only for the docs, source read in part.
- **Joplin.** https://joplinapp.org/help/apps/import_export (n/d): JEX is "a tar file ... a lossless format in that all the notes, but also metadata ... are preserved. This format is convenient for backup purposes." https://bitoffdev.github.io/sovereign-note/joplin/jex.html (third-party, n/d): notes at top level, resources under `resources/`, each item an `.md` with a `key: value` metadata trailer. https://discourse.joplinapp.org/t/export-summary-what-each-option-does/48564 (2026-02-06): HTML export embeds images as `data:image/jpeg;base64`, and "Exports where filenames are constructed from Note title metadata ... truncate at 51 characters". Answers Q1. V/C. Highlights only.
- **Todoist.** https://www.todoist.com/help/account-and-billing/security/download-or-restore-backups-in-todoist-ywaJeQbN (2026-09-30): backups are "ZIP files", "Your projects are backed up as CSV files", and include "Links to file attachments", not bytes. 21 backups kept. Third-party https://github.com/joanbm/full-offline-backup-for-todoist (n/d): "The backup functionality doesn't back up any of the attachments ... only a URL to download the attachment is included". Answers Q1. V + C. Highlights only. **Inference:** a backup that stores links only is the failure mode the brief calls "silently dropping"; a user must write a third-party tool to get a real backup.
- **Notion.** https://www.notion.com/help/back-up-your-data (n/d): export "as HTML, Markdown, or CSV ... along with any files you've uploaded". "You can't instantly recreate your workspace by reuploading your exported workspace content." Link "will expire after 7 days". Zip import cap: "5 GB per ZIP", "10,000+ files are likely to fail". Answers Q1, Q3. V. Highlights only. Notion's export is read-only for humans, not a restore format.
- **Google Takeout.** https://takeoutjson.com/blog/google-takeout-split-zip-parts (2026-09-30, third party): size per part 1 to 50 GB, default 2 GB; "These are ordinary zips, not a multi-volume archive"; "A photo and its .json file can be in different parts." Download links valid 7 days. Answers Q1. C. Highlights only. Relevant lesson: splitting across zips breaks the pairing of file and metadata.
- **Things.** Not searched in depth; no source found within budget. Gap.

### Pattern across the field

Every app that restores its own export (Memos, Vikunja, Trilium, Joplin) uses a single archive container (ZIP or tar) holding structured metadata plus raw attachment files. None uses base64 inside the JSON as the primary format; Joplin only does so for its human-readable HTML export. Todoist is the counterexample that drops bytes and is documented as a gap by users. **Inference:** for Meat Mode, ZIP with `manifest.json` + `todos.json` (or today's JSON as one entry) + `attachments/<id>` fits the field, and Memos is the best template since it is also Go, SQLite and single-user.

## Q2: Costs of base64-in-JSON, and streaming zip in Go

- **Base64 inflation is 4/3.** Standard (RFC 4648): every 3 bytes become 4 characters, plus padding. A 25 MiB file becomes about 33.3 MiB of text. Basic arithmetic, not a measured claim. At the current 64 MiB restore cap this leaves roughly 48 MiB of raw attachment payload before any JSON overhead.
- **Go memory doubling.** https://wang.yuxuan.org/blog/item/2017/02/some-go-memory-notes (2017-02-11): "if you are using JSON to pass large chunk of []byte between machines, you might want to think twice ... you will need to save both the original chunk of []byte, and the whole encoded base64 string ... more than doubled your memory consumption." On decode "you need to save the whole JSON string ... into memory" then allocate again. Answers Q2. C (single blog), but consistent with `encoding/json` behaviour. Highlights only. Also https://www.alexedwards.net/blog/json-surprises-and-gotchas (2020-09-23): `[]byte` is encoded as a base64 string with padding. V.
- **Inference:** with one SQLite connection and a hub on a home server, a 300 MB base64 export means roughly 300 MB raw + 400 MB text + decode buffer at restore, i.e. 1 GB peak. A streaming `json.Decoder` could avoid some of it only with custom token handling; not worth it versus ZIP.
- **Writing a zip streams.** `archive/zip.Writer` takes an `io.Writer` and each entry is `io.Copy`'d, so memory is constant. Memos' Writer does exactly this ("Writer streams a Memos export file to an io.Writer"). Vikunja and Trilium (archiver) do likewise. V. The export response can be streamed straight to the HTTP response, with no temp file.
- **Reading a zip needs random access.** https://pkg.go.dev/archive/zip: `func NewReader(r io.ReaderAt, size int64)`. https://github.com/golang/go/issues/10568 (2015-04-24), maintainer: "to read a zip file, you have to read the directory at the end ... you don't even know whether each file is compressed or not." So an upload body must be spooled to a temp file (or `os.File`) and opened with `zip.NewReader(f, size)` or `zip.OpenReader`. It cannot be consumed from `r.Body` directly. A < 64 MiB body could use `bytes.Reader`, but at hundreds of MB spool to disk in the data directory. Answers Q2. A-ish (stdlib docs and maintainer statement). Read in full for the pkg.go.dev text via highlights.
- **Zip-slip.** https://pkg.go.dev/archive/zip: `NewReader` returns `ErrInsecurePath` only if `GODEBUG` has `zipinsecurepath=0`; "A future version of Go may introduce this behavior by default." Source (go.dev/src/archive/zip/reader.go) rejects `!filepath.IsLocal(f.Name) || strings.Contains(f.Name, "\\")`. CodeQL `go/zipslip`, severity 7.5 (https://codeql.github.com/codeql-query-help/go/go-zipslip/, n/d). **Inference:** the restore should never use `f.Name` as a filesystem path. Look attachments up by ID from the manifest and write to a path the hub generates (e.g. `attachments/<id>`), so entry names are only ever map keys. That removes the class of bug and also handles names like `NUL` or Unicode tricks. Memos uses `SafeFilename` for the same reason.
- **Zip-bomb.** Headers are not trusted:
  - https://github.com/golang/go/issues/49791: the reader "only checks file size after all the read of compress file is done" so a malformed zip can overflow during decompression. Stdlib returns `ErrFileSize` after the fact. B.
  - https://dans.garden/go/ZIP-bomb-countermeasures (n/d): limit file count, "Limit the amount of bytes that can actually be read", cap compression ratio (example constants: 1,000 files, 50 MiB per file, ratio 1,032 from DEFLATE's max); notes overlapping-entry bombs reach ratios over 28 million. C.
  - https://github.com/sensiblebit/certkit/blob/main/internal/archive.go (n/d): defaults `MaxDecompressionRatio 100`, `MaxTotalSize 256 MB`, `MaxEntryCount 10_000`, `readZipEntry` wraps in `io.LimitReader(rc, max+1)` "regardless of what the ZIP header claims". C.
  - https://github.com/alikatgh/safeheaders-go lesson 18 (n/d): per-entry caps are not enough, "only an aggregate total accumulator across all entries stops it". C.
  - **Inference, recommended defences:** (1) cap total upload bytes on disk; (2) cap entry count; (3) wrap each entry in `io.LimitReader(rc, declaredSize+1)` where `declaredSize` comes from the signed-off manifest and is itself capped at the per-file attachment limit; (4) keep a running total across entries; (5) skip nested zips (never recurse); (6) optionally a ratio cap, with images and PDFs already compressed so a legit ratio near 1 makes a low cap (e.g. 100) safe except for large text files.
- **Multi-gigabyte.** Zip64 is supported by `archive/zip` natively. Notion (5 GB import cap, 10,000 files) and Google (1 to 50 GB parts) show that vendors cap archive size; not an issue at personal scale.

## Q3: Integrity checks and missing files on restore

- **Memos** records `SHA256` and `Size` per attachment, written by streaming the bytes and recording what was observed (`WriteAttachment` returns "the digest and length it observed"). `SHA256` is `omitempty`, so it is optional in the format. Memos validates before preview and reports Failed counts and warnings per memo, so one bad memo does not abort the rest. Exact verification and missing-file code paths not read. V. Highlights only.
- **BagIt (RFC 8493, 2018-10-28)**, the archival standard: https://www.rfc-editor.org/rfc/rfc8493.html. Payload manifest `manifest-sha256.txt`, lines of `checksum filepath`; "Every payload manifest MUST list every payload file name exactly once". A bag is *complete* if every listed file is present and *valid* if "Every checksum ... has been successfully verified". `bagit-python` classifies errors as `ChecksumMismatch`, `FileMissing`, `UnexpectedFile` and offers `--fast` that only compares a payload byte count and file count ("payload-oxum"). A, as a published standard. Highlights only. **Inference:** reuse the vocabulary (missing, mismatch, unexpected) for restore reports; do not adopt BagIt layout itself, it adds `bagit.txt` and `data/` for no gain here.
- **Vikunja** export skips attachments whose file is gone and import logs a warning and skips ones missing in the zip; both are silent to the end user (log only), the failure mode the brief wants to avoid. V (source, highlights only).
- **Todoist** has no check at all: bytes are not in the backup, only links.
- Nothing found in Joplin, Trilium or Notion docs about checksums or missing-file behaviour. Searched; no hit.
- **Inference, recommended restore policy for Meat Mode:**
  1. Manifest lists every attachment with `id`, `size`, `sha256`, `name`, `mime`, and the to-do it belongs to.
  2. Restore is two-phase: validate (parse manifest, check that every listed entry exists, sizes within caps, stream-hash each file) then apply in one transaction plus temp-file renames. Nothing is written if validation fails, mirroring Memos' "validates ... before showing a preview".
  3. Missing file or hash mismatch is an error by default, listed by name in the response, with an explicit opt-in "restore without these attachments" that records the dropped IDs in the result. Never a silent skip.
  4. Unlisted extra entries are ignored but reported (BagIt `UnexpectedFile`).
  5. Export writes attachment bytes first, then the manifest (as Memos does with digests), or computes the hash while streaming and writes the manifest last as the final entry. A truncated export then has no manifest and is rejected at open time (zip central directory is also at the end).
  6. Format carries `format` and `formatVersion` fields and a major-version check, as Memos and Trilium do; accept the old single-JSON export as version 1 with no attachments.
  7. Keep the 64 MiB cap for the legacy JSON path; give the zip path a separate, larger, configurable cap, and say the number in the error.

## Open decisions to flag

- Whether to still offer a base64-in-JSON single file for small data sets. **Inference:** no; one format with the zip, plus legacy JSON read-only on restore, is simpler.
- Restore cap value for the zip path (hundreds of MB) and where the upload is spooled (data directory temp subfolder, cleaned on start).
- Whether restore is merge or replace. Memos offers keep/replace/copy per ID; this repo's existing restore targets a fresh hub, so replace-into-empty is enough now.

## Gaps

- No source read in full for any competitor's missing-file or checksum behaviour (all highlights).
- Things (Cultured Code) export not found. Nextcloud, Paperless-ngx exporter (which has a `--checksum` style manifest) not searched.
- No measured memory figure for a base64 JSON round trip in modernc/SQLite Go; numbers above are arithmetic.

# Meat Mode

Meat Mode is a self-hosted to-do app for one person. It shows their personal to-dos and the tasks AI agents raise for them in [beads](https://github.com/gastownhall/beads), on desktop and phone, and sends their actions (done, snooze, note, dismiss, add) back to beads. The hub runs as a container on any always-on host, so it keeps working when the machine the agents run on is off. A small bridge on the agent machine dials out to the hub, on the same machine or across the LAN, and syncs with beads.

## This repo is public from the first commit

Everything tracked here will be published: code, docs, tests, fixtures and commit messages. Write all of it as if a stranger reads it tomorrow. The beads issues are the exception: they are private and never reach the public remote (see Work tracking).

Never put any of these in a tracked file or a commit message:

- **Personal data**: real names, email addresses, phone numbers, street addresses, account IDs, real tasks or notes.
- **Secrets**: tokens, passwords, API keys, cookies, private keys, or the value of any environment variable.
- **Machine state**: hostnames, device names, usernames, absolute home paths, IP addresses, tailnet or Tailscale Service names, MAC addresses, the ports or paths of one particular deployment.
- **Data files**: databases, exports, backups, logs, screenshots of real data.

How to work within that:

- Configuration is read from environment variables at runtime. Every variable is listed in `.env.example` with a placeholder value. Real values live in untracked files (`.env`, `mise.local.toml`) or in the deployment's own settings.
- Examples, fixtures and tests use invented data only: `example.com` addresses, the documentation IP ranges `192.0.2.0/24` and `2001:db8::/32`, people called Alex or Sam, tasks like "Water the plants".
- Deployment docs stay generic ("an Unraid server", "a Tailscale Service"). One operator's server names, paths and ports belong in that operator's own notes, outside this repo.
- The app talks to its user in plain, neutral words. It has no assistant persona, nicknames or in-jokes from anyone's agent setup.

## Guards

- Run `scripts/setup.sh` once per clone. It points Git at `.githooks/`, whose pre-commit hook runs `scripts/public-safety.sh` and, when installed, `gitleaks`.
- `scripts/public-safety.sh` scans what is staged for home paths, private and Tailscale IP addresses, tailnet and `.local` hostnames, email addresses outside the allowed example domains, MAC addresses, and files that must never be tracked (`.env`, `.envrc`, a deployment's `compose.yaml`, databases, keys). It also reads `.public-safety.local`, an untracked file of one extended regex per line for the operator's own names and hosts. Add a new private identifier there, never to a tracked file.
- The guard also refuses tracked beads data: anything in `.beads/` beyond `.gitignore`, `README.md`, `config.yaml` and `metadata.json`, and a Dolt remote in `config.yaml`.
- The pre-push hook refuses any ref other than a branch or tag (Dolt keeps its data in `refs/dolt/`), Dolt's `__dolt` branches, and pushed commits that track beads data or a Dolt remote.
- `tests/public-safety.test.sh` proves the guard and the pre-push hook catch each class and pass clean code. CI runs the test, the guard and gitleaks on every push and pull request, and daily checks that the public remote holds no `refs/dolt/` or `__dolt` refs.
- Never commit with `--no-verify`. When a guard fires, remove the data. Widen a pattern's allowlist only when the operator agrees in that conversation.

## Checks

- While building, run targeted tests: one Go package, one Playwright spec.
- Before a merge to main or a deploy, run `scripts/check.sh` once. It runs what CI runs, the `e2e/` suite included, except gitleaks and the container image build with its end-to-end run, and must pass.
- Parallel worktrees set their own `E2E_PORT` for the web suite.
- Each tracker's conformance suite runs against its real tool: `bd`, `backlog` and `tk` on PATH. A missing one skips its suite, so a green check without them has not tested that tracker; CI installs all three.
- Screenshot baselines live in `web/test/visual/__screenshots__`. `pnpm --dir web test:visual` checks them inside the Playwright container that CI uses, so the fonts match; after an intended layout change, run `pnpm --dir web test:visual:update` and look at every changed PNG before committing it. The container has no bold weight for the app's system font stack, so baselines catch layout changes but not font-weight changes.

## Releases

A release is a `vX.Y.Z` tag; pushing it runs `release.yml`, which builds the binaries and the APK and publishes them. `tag.yml` gives every green push to main the next patch as a lightweight tag, with notes GitHub generates, and skips a commit the latest tag already holds.

- A minor or major release is an annotated tag made by hand on local main, pushed with main. Its message is the release notes: the first line is the release title, the rest the body. Make it with `git tag -a --cleanup=whitespace -F <notes>`, so Markdown headings are not stripped as comments.
- Floor paths: a change to `docs/api.md`, the export version in `internal/api`, or a migration in `internal/store/store.go` makes the release at least a minor.
- Public surface, whose break is a major: the app and bridge HTTP APIs, the export format, and the `meat-mode` CLI.

## Work tracking

Work is tracked in beads in this repo: `bd ready`, `bd show <id>`, `bd update <id> --claim`, `bd close <id>`. The build epic holds the decisions and the order of work; start there.

The issues are private. Their Dolt database is untracked and syncs only to a private Dolt remote the operator sets up outside this repo; it never goes to the public Git remote. Dolt pushes past Git's hooks, so the rules below are what keep it there:

- Before `bd sync` or `bd dolt push`, `bd dolt remote list` must show the private remote. With none set, both adopt the Git `origin` (this public repo) as the Dolt remote and push every issue to it. Never run `bd sync --yes`, and never add a Dolt remote that points at the public repo.
- `bd dolt remote add` also writes the remote into `.beads/config.yaml` and commits that on its own, as `bd: update sync.remote`. Drop that commit and keep the file as tracked; the remote lives in the Dolt database.

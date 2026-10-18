# Meat Mode

Meat Mode is a self-hosted to-do app for one person. It shows their personal to-dos and the tasks AI agents raise for them in [beads](https://github.com/gastownhall/beads), on desktop and phone, and sends their actions (done, snooze, note, dismiss, add) back to beads. The hub runs as a container on any always-on host, so it keeps working when the machine the agents run on is off. A small bridge on the agent machine dials out to the hub, on the same machine or across the LAN, and syncs with beads.

## This repo is public from the first commit

Everything tracked here will be published: code, docs, tests, fixtures, commit messages, and the beads issues, which sync to the same Git remote. Write all of it as if a stranger reads it tomorrow.

Never put any of these in a tracked file, an issue or a commit message:

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
- `scripts/public-safety.sh` scans what is staged for home paths, private and Tailscale IP addresses, tailnet and `.local` hostnames, email addresses outside the allowed example domains, MAC addresses, and files that must never be tracked (`.env`, databases, keys). It also reads `.public-safety.local`, an untracked file of one extended regex per line for the operator's own names and hosts. Add a new private identifier there, never to a tracked file.
- `tests/public-safety.test.sh` proves the guard catches each class and passes clean code. CI runs the test, the guard and gitleaks on every push and pull request.
- Never commit with `--no-verify`. When a guard fires, remove the data. Widen a pattern's allowlist only when the operator agrees in that conversation.

## Checks

- While building, run targeted tests: one Go package, one Playwright spec.
- Before a merge to main or a deploy, run `scripts/check.sh` once. It runs what CI runs, the `e2e/` suite included, and must pass.
- Parallel worktrees set their own `E2E_PORT` for the web suite.
- Screenshot baselines live in `web/test/visual/__screenshots__`. `pnpm --dir web test:visual` checks them inside the Playwright container that CI uses, so the fonts match; after an intended layout change, run `pnpm --dir web test:visual:update` and look at every changed PNG before committing it. The container has no bold weight for the app's system font stack, so baselines catch layout changes but not font-weight changes.

## Work tracking

Work is tracked in beads in this repo: `bd ready`, `bd show <id>`, `bd update <id> --claim`, `bd close <id>`. The build epic holds the decisions and the order of work; start there. Issues follow the public rule above.

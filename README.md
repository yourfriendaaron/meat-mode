# Meat Mode

A self-hosted to-do app for one person: your own to-dos and the tasks your AI agents raise for you in [beads](https://github.com/gastownhall/beads), on desktop and phone. Tick, snooze, dismiss or leave a note, and the change flows back to beads. It runs as a container on any always-on host, so reminders and your actions keep working when the machine your agents run on is off. A small bridge on that machine syncs with it, on the same box or across your LAN.

Status: v1 in progress, not released yet.

## How it works

- **Hub**: one container (`meat-mode hub`) on an always-on host. It keeps your personal to-do list itself, a copy of your agents' items in SQLite and an append-only log of what you tapped. It sends reminders through Web Push (and optionally ntfy) and serves the app. Your personal list works with no agent machine at all, and your agents can add to it from any machine with a token.
- **Bridge**: the same program (`meat-mode bridge`) as a user service on the machine where your beads repos live. It finds the beads repos in your home folder, dials out to the hub, sends up beads labelled `human` from the repos you switch on in Settings, and replays your taps into beads with `bd`.

Setup, pairing and updates: [docs/deploy.md](docs/deploy.md). The API between the parts: [docs/api.md](docs/api.md). Why it is built this way: [docs/decisions](docs/decisions).

## Contributing

Run `scripts/setup.sh` after cloning. This repo is public; read `AGENTS.md` before committing anything.

Toolchain versions are pinned in `mise.toml` (Go, Node, pnpm).

```sh
pnpm --dir web install && pnpm --dir web build   # frontend into web/static
go test ./...                                     # hub, bridge, reminders
pnpm --dir web test && pnpm --dir web test:e2e    # frontend unit and browser tests
go build ./cmd/meat-mode                          # one binary: hub and bridge
```

Tests:

- `go test ./...` for the hub and the bridge.
- `pnpm --dir web test` for frontend modules, and `pnpm --dir web test:e2e` for the UI against a fake hub.
- `pnpm --dir e2e test` for the end-to-end suite: the app in Chromium, a real hub, two bridges and scratch beads repos in a temp directory. It needs Go, `pnpm install` in both `web/` and `e2e/`, and `bd` 1.3 or newer on `PATH`; without `bd` it skips. Set `E2E_IMAGE=<image>` to run the hub from a built image (`docker build -t meat-mode:dev .`) instead of the binary.

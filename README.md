<!--
Style guide for this page. Readers: someone deciding whether to self-host Meat Mode, often not a Go or web developer.
- Lead with what it does for them: one list for their own to-dos and the tasks their agents raise. The machinery (hub, bridge, SQLite) comes after.
- Say "you". Short sentences, present tense, no em dashes, no history. Invented examples only (Alex, Sam, example.com): this repo is public.
- Shape: what it is, how it works, getting started (the setup wizard), contributing. Setup detail lives in docs/deploy.md, the wire contract in docs/api.md; link, do not copy.
- Keep the wizard's step list in line with web/src/wizard/steps and the step table in docs/deploy.md.
-->
# Meat Mode

A self-hosted to-do app for one person: your own to-dos and the tasks your AI agents raise for you in [beads](https://github.com/gastownhall/beads), on desktop and phone. Tick, snooze, dismiss or leave a note, and the change flows back to beads. It runs as a container on any always-on host, so reminders and your actions keep working when the machine your agents run on is off. A small bridge on that machine syncs with it, on the same box or across your LAN.

Status: v1 in progress, not released yet.

## How it works

- **Hub**: one container (`meat-mode hub`) on an always-on host. It keeps your personal to-do list itself, a copy of your agents' items in SQLite and an append-only log of what you tapped. It sends reminders through Web Push (and optionally ntfy) and serves the app. Your personal list works with no agent machine at all, and your agents can add to it with `meat-mode add` on any machine with a bridge, then change what they added with `meat-mode edit` and `meat-mode done`.
- **Bridge**: the same program (`meat-mode bridge`) as a user service on the machine where your beads repos live. It finds the beads repos in your home folder, dials out to the hub, sends up beads labelled `human` from the repos you switch on in Settings, and replays your taps into beads with `bd`.

Reach it through Tailscale, or behind your own HTTPS reverse proxy with a password. The API between the parts: [docs/api.md](docs/api.md). Why it is built this way: [docs/decisions](docs/decisions).

## Getting started

Start the hub container ([docs/deploy.md](docs/deploy.md#1-run-the-hub)), then open it in your browser. The first visit opens setup, which walks you through the rest:

1. **Connection check**: is the app reached the way the hub expects, so reminders and live updates work?
2. **Secure the hub**: set the password right in the browser, with a one-time setup code the hub prints in its log.
3. **Name and time zone**: the name your notes are signed with, and the zone your agents' dates are read in.
4. **Install the app** to the home screen or dock. On an iPhone or iPad, reminders need the Home Screen app.
5. **Reminders**: allow notifications and get a test reminder on this device, and on ntfy if you use it.
6. **Appearance**: dark, light, or follow the device.
7. **First to-do**: type `Water the plants tomorrow 6pm` and quick add reads the date as you type.
8. **Agent machines** (optional): pair the machine your agents work on, choose which repos show up, and copy instructions that tell your agents how to raise tasks for you.

Skip any step; each one is also in Settings, where you can run setup again. Other devices get just the device steps. Pairing, updates and the reverse proxy are in [docs/deploy.md](docs/deploy.md).

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

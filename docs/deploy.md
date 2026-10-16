# Deploying Meat Mode

Meat Mode has two parts:

- **The hub** is a container on any always-on host (a home server, a NAS, a Raspberry Pi, a VPS). It keeps a copy of your items, the log of what you tapped, and sends reminders. It never connects to your agent machine.
- **The bridge** is the same `meat-mode` program, run as a user service on the machine where your beads repos live. It dials out to the hub, sends your items up and replays your taps into beads with `bd`.

Both setups below follow the same steps and differ only in the hub address.

| Setup | Hub address the bridge uses |
|---|---|
| Hub and bridge on one machine | `http://127.0.0.1:8080` (plain HTTP is accepted on loopback only) |
| Hub on another always-on host on your LAN | `https://meat-mode.example.com`, through a reverse proxy or a Tailscale Service |

For reminders on your phone, open the app over **HTTPS with a publicly trusted certificate**. Browsers allow Web Push only in a secure context. A reverse proxy with a Let's Encrypt certificate or a Tailscale Service qualifies; a bare IP address does not.

## 1. Run the hub

You need Docker or Podman on the hub host.

```sh
git clone <this repo> meat-mode && cd meat-mode
cp .env.example .env            # then edit: MEAT_MODE_PUBLIC_URL, MEAT_MODE_USER, MEAT_MODE_VAPID_SUBJECT
cp compose.example.yaml compose.yaml
docker compose up -d --build    # or: podman compose up -d --build
```

Without Compose:

```sh
docker build -t meat-mode .
docker volume create meat-mode-data
docker run -d --name meat-mode --restart unless-stopped \
  --env-file .env -p 127.0.0.1:8080:8080 -v meat-mode-data:/data meat-mode
```

Check it: `curl -fsS http://127.0.0.1:8080/healthz` prints `ok`.

The data volume holds the database and the Web Push keys. Keep it on local storage: SQLite's write-ahead log does not work over NFS or SMB. If you bind-mount a host directory instead of a named volume, make it writable by user id 65532 (`chown 65532:65532 <dir>`); the hub says so and exits if it cannot write.

### Expose it over HTTPS

Publish port 8080 only to loopback (as above) and put HTTPS in front of it:

- **Reverse proxy** (Caddy, nginx, Traefik, or your NAS's built-in proxy): proxy `https://meat-mode.example.com` to `http://127.0.0.1:8080`. The app uses a long-lived request for live updates (`/api/events`, server-sent events), so turn off response buffering for that path if your proxy buffers.
- **Tailscale Service**: define the Service in the Tailscale admin console, then on the hub host run `tailscale serve --bg --service=svc:<name> --https=443 http://127.0.0.1:8080` and approve the host. The app is then at `https://<name>.<your tailnet>.ts.net` for devices on your tailnet.

Set `MEAT_MODE_PUBLIC_URL` to that address and restart the hub, so links in reminders open the right place.

The app itself has no login. It relies on the network boundary: keep it on your LAN or VPN, and do not expose it to the internet.

### Host notes

- **Unraid**: add a container from the image you built (or a registry you push it to), map `/data` to an appdata share on a cache or array disk (not a remote share), and set the variables from `.env.example` in the template.
- **Synology and other NAS**: use Container Manager with Compose, and the NAS's reverse proxy for HTTPS.
- **Raspberry Pi**: the image builds for arm64 as is.

## 2. Install the bridge on the agent machine

The bridge needs `bd` 1.3.0 or later on that machine.

Install the program, either from a release binary for your platform (linux or darwin, amd64 or arm64; put it somewhere on your `PATH` such as `~/.local/bin`), or with Go:

```sh
go install github.com/yourfriendaaron/meat-mode/cmd/meat-mode@latest
```

## 3. Pair

1. Open the app, go to **Settings**, choose **Add machine** and give it a name such as `laptop`. The app shows a token once, and the exact command.
2. On the agent machine run that command:

   ```sh
   meat-mode bridge pair https://meat-mode.example.com mm_xxxxxxxx
   ```

   Options: `--root DIR` (repeatable) for the directories that hold your beads repos (default `~/projects`), `--personal DIR` for your personal to-do beads repo, `--allow-insecure-http` to allow plain HTTP to a hub that is not on loopback (not recommended).

Pairing checks the token with the hub, saves the config (`~/.config/meat-mode/bridge.json` on Linux, `~/Library/Application Support/meat-mode/bridge.json` on macOS) and installs a user service:

- **Linux**: a systemd user unit, `meat-mode-bridge.service`. Logs: `journalctl --user -u meat-mode-bridge`. To keep it running while you are logged out, enable lingering once: `loginctl enable-linger $USER`.
- **macOS**: a launchd agent, `com.github.yourfriendaaron.meat-mode.bridge`. Logs: `~/Library/Logs/meat-mode-bridge.log`.

`meat-mode bridge status` shows the config, the repos found and whether the hub answers.

## 4. Choose which repos sync

The bridge finds every top-level directory under its roots that has a `.beads/` folder (Git worktrees are skipped). A repo syncs only once you opt it in, with this line in its `.beads/config.yaml`:

```yaml
meat-mode.sync: true
```

New repos appear in the app's settings as found but off. Your personal repo always syncs, and every item in it is yours; in other repos only beads labelled `human` appear.

## Updating

- **Hub**: `git pull && docker compose up -d --build`. The database migrates itself on start.
- **Bridge**: replace the binary, then run `meat-mode bridge pair …` again with the same arguments, or restart the service (`systemctl --user restart meat-mode-bridge`, or on macOS `launchctl kickstart -k gui/$(id -u)/com.github.yourfriendaaron.meat-mode.bridge`).

## Uninstalling

- **Bridge**: `meat-mode bridge uninstall` removes the user service and the config. Then revoke the machine in the app's settings.
- **Hub**: `docker compose down`, and `docker volume rm meat-mode-data` to delete the data.

## Several machines

Pair each agent machine with its own name. Items are kept per machine, and a tap is replayed only by the machine that owns the item. A machine that is off keeps its actions waiting until it is back; the others carry on.

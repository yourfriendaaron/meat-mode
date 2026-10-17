# Deploying Meat Mode

Meat Mode has two parts:

- **The hub** is a container on any always-on host (a home server, a NAS, a Raspberry Pi, a VPS). It keeps a copy of your items, the log of what you tapped, and sends reminders. It never connects to your agent machine.
- **The bridge** is the same `meat-mode` program, run as a user service on the machine where your beads repos live. It dials out to the hub, sends your items up and replays your taps into beads with `bd`.

You reach the app through [Tailscale](https://tailscale.com), and only through Tailscale. The app has no login, so who can reach the hub is the whole of its access control: on a tailnet that is your own signed-in devices, not everyone on your Wi-Fi. Tailscale also gives the hub a publicly trusted HTTPS certificate, which browsers require before they allow reminders (Web Push) or installing the app to the home screen.

| Setup | Hub address the bridge uses |
|---|---|
| Hub and bridge on one machine | `http://127.0.0.1:8080` (plain HTTP is accepted on loopback only) |
| Hub on another always-on host | its Tailscale address, `https://meat-mode.<your tailnet>.ts.net` |

Either way, your phone and other devices open the app at its Tailscale address.

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

### Expose it on your tailnet

Publish port 8080 only to loopback (as above), so nothing on your LAN reaches the hub directly, and let Tailscale serve it over HTTPS. Install Tailscale on the hub host first, and turn on HTTPS certificates for your tailnet once in the admin console (**DNS**, **HTTPS Certificates**).

**Tailscale Service (recommended).** The address belongs to the Service, not to the host, so you can move the hub to another host later without changing it.

1. In the admin console, under **Services**, add a Service named `meat-mode` with the endpoint `tcp:443`.
2. On the hub host run `tailscale serve --bg --service=svc:meat-mode --https=443 http://127.0.0.1:8080`.
3. In the admin console, approve the hub host for the Service.

The app is then at `https://meat-mode.<your tailnet>.ts.net`.

**`tailscale serve` on the host (fallback).** For trying it out, or a host that will never move, run `tailscale serve --bg --https=443 http://127.0.0.1:8080` instead. The app is then at `https://<host name>.<your tailnet>.ts.net`. That address belongs to the host: if you move the hub, each bridge must pair again and each device must turn reminders on again, because browsers tie push subscriptions and installed apps to the address.

Then set `MEAT_MODE_PUBLIC_URL` to that address and restart the hub. The hub refuses to start without an `https://` public address (plain `http://` is accepted for localhost only), and it answers only to `localhost`, `127.0.0.1`, the public address's name and any names in `MEAT_MODE_ALLOWED_HOSTS`.

Certificates are recorded in public Certificate Transparency logs, so the `ts.net` name is publicly visible, though only your tailnet can reach it. Keep anything private out of the Service and host names.

Do not expose the hub to the internet, or to your LAN without Tailscale in front: the app has no login.

### Host notes

- **Unraid**: add a container from the image you built (or a registry you push it to), map `/data` to an appdata share on a cache or array disk (not a remote share), and set the variables from `.env.example` in the template.
- **Synology and other NAS**: use Container Manager with Compose, and install Tailscale on the NAS for HTTPS.
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

   Options: `--personal DIR` for your personal to-do beads repo, `--root DIR` (repeatable) to look for beads repos only in those folders instead of your whole home folder, `--allow-insecure-http` to allow plain HTTP to a hub that is not on loopback (not recommended). To keep the token out of your shell history, put `-` in its place and paste it on the next line, or leave it out and set `MEAT_MODE_BRIDGE_TOKEN`.

Pairing checks the token with the hub, saves the config (`~/.config/meat-mode/bridge.json` on Linux, `~/Library/Application Support/meat-mode/bridge.json` on macOS) and installs a user service:

- **Linux**: a systemd user unit, `meat-mode-bridge.service`. Logs: `journalctl --user -u meat-mode-bridge`. To keep it running while you are logged out, enable lingering once: `loginctl enable-linger $USER`.
- **macOS**: a launchd agent, `com.github.yourfriendaaron.meat-mode.bridge`. Logs: `~/Library/Logs/meat-mode-bridge.log`.

`meat-mode bridge status` shows the config, the repos found and whether the hub answers.

## 4. Choose which repos sync

The bridge looks through your home folder for beads repos, on start and every 5 minutes, at any depth. It skips dependency, cache and app-data folders, Git worktrees and symlinks, and a `.beads` folder without a database (such as bd's own `~/.beads`).

Every repo it finds appears in the app under **Settings**, below its machine, with a switch. Switches start off. Turn one on and that repo's items arrive within a few seconds; turn it off and they leave the app. If the machine is off, the switch shows "Waiting for" the machine until it is back. The machine keeps your choices in its own config file, and it only accepts a switch for a repo it found itself.

You can also opt a repo in from the repo itself, with this line in its `.beads/config.yaml`. A switch set in the app wins over it.

```yaml
meat-mode.sync: true
```

Your personal repo always syncs, and every item in it is yours; in other repos only beads labelled `human` appear. Repos are told apart by their path, so `projects/garden` and `work/garden` both sync; the app shows the folder name, and the path where two names match.

## Updating

- **Hub**: `git pull && docker compose up -d --build`. The database migrates itself on start.
- **Bridge**: replace the binary, then run `meat-mode bridge pair …` again with the same arguments, or restart the service (`systemctl --user restart meat-mode-bridge`, or on macOS `launchctl kickstart -k gui/$(id -u)/com.github.yourfriendaaron.meat-mode.bridge`).

## Uninstalling

- **Bridge**: `meat-mode bridge uninstall` removes the user service and the config. Then revoke the machine in the app's settings.
- **Hub**: `docker compose down`, and `docker volume rm meat-mode-data` to delete the data.

## Several machines

Pair each agent machine with its own name. Items are kept per machine, and a tap is replayed only by the machine that owns the item. A machine that is off keeps its actions waiting until it is back; the others carry on.

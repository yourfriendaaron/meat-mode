# Deploying Meat Mode

Meat Mode has two parts:

- **The hub** is a container on any always-on host (a home server, a NAS, a Raspberry Pi, a VPS). It keeps your personal to-do list, a copy of your agents' items and the log of what you tapped, and sends reminders. It never connects to your agent machine.
- **The bridge** is the same `meat-mode` program, run as a user service on the machine where your beads repos live. It dials out to the hub, sends your agents' items up and replays your taps on them into beads with `bd`.

Your personal list needs only the hub. You can stop at step 1 and use it on its own, then add a bridge when you want your agents' tasks too.

There are two supported ways to reach the hub, and both need a publicly trusted HTTPS certificate, which browsers require before they allow reminders (Web Push) or installing the app to the home screen:

- **[Tailscale](https://tailscale.com)**, with no login. Who can reach the hub is the whole of its access control: on a tailnet that is your own signed-in devices, not everyone on your Wi-Fi. Tailscale also supplies the certificate. See [Expose it on your tailnet](#expose-it-on-your-tailnet).
- **A reverse proxy** you already run (Caddy, nginx, Traefik, a NAS's built-in proxy), with a password. The hub asks for it before it shows or accepts anything. See [Behind a reverse proxy](#behind-a-reverse-proxy).

A hub with no password hash runs with no login and logs a warning saying so. That is safe only when nothing but you can reach it. Never publish a hub without a password to your LAN or the internet.

| Setup | Hub address the bridge uses |
|---|---|
| Hub and bridge on one machine | `http://127.0.0.1:8080` (plain HTTP is accepted on loopback only) |
| Hub on another always-on host | its public address: `https://meat-mode.<your tailnet>.ts.net`, or the reverse proxy's `https://` address |

Either way, your phone and other devices open the app at that same address.

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

This path has no login, so do not also publish the port to your LAN or the internet. To use a reverse proxy instead, or as well, set a password first (next section).

### Behind a reverse proxy

Use this when you already run a reverse proxy with a certificate, or want the app at your own domain. The hub then has a login: one password, one user.

**1. Set a password.** Make the hash with the image you built:

```sh
docker run --rm -it --entrypoint /meat-mode meat-mode hash-password
```

(or `meat-mode hash-password` with the binary). It asks for the password twice and prints an Argon2id string such as `$argon2id$v=19$m=…,t=…,p=…$<salt>$<hash>`. With input piped in, it reads one line.

**2. Give the hash to the hub**, one of two ways. Setting both stops the hub at start.

- `MEAT_MODE_PASSWORD_HASH=<the hash>`.
- `MEAT_MODE_PASSWORD_HASH_FILE=<path>`, a file inside the container that holds the hash. This suits Docker and Compose secrets (see `compose.example.yaml`) and avoids the quoting below.

The hash holds `$` signs, which Docker tools can read as variables:

- In a `.env` file that Compose reads through `env_file:`, wrap the value in single quotes: `MEAT_MODE_PASSWORD_HASH='$argon2id$v=19$…'`. Compose uses single-quoted values literally; unquoted and double-quoted values have `$` expanded.
- Written directly in `compose.yaml` under `environment:`, double every `$` as `$$`.
- With `docker run --env-file`, the line is taken literally, quotes included, so write it with no quotes. The single-quoted form above would put the quotes into the hash and the hub would refuse it. A `.env` shared between `docker run` and Compose therefore cannot hold the hash; use the `_FILE` variant.

Start the hub and check the log: the "login is off" warning is gone.

**3. Publish the port to loopback only** (`127.0.0.1:8080:8080`, as above) and have the proxy forward to it. If the proxy runs in another container, put both on a shared network and forward to the hub container's port 8080 instead of publishing it.

**4. Set `MEAT_MODE_PUBLIC_URL`** to the proxy's address, `https://meat-mode.example.com`. The proxy must:

- Pass the original `Host` header through. If it rewrites it, add the name the hub receives to `MEAT_MODE_ALLOWED_HOSTS`.
- Send `X-Forwarded-Proto`.
- Not buffer responses from `/api/events`, the server-sent event stream that keeps the app current. The hub marks it unbuffered, which nginx honors; the nginx snippet turns buffering off for it as well.
- Present a certificate that browsers trust, such as Let's Encrypt. Web Push and installing to the home screen do not work with a self-signed one.

**Caddy**

```
meat-mode.example.com {
	reverse_proxy 127.0.0.1:8080
}
```

Caddy passes `Host` unchanged, sets `X-Forwarded-Proto`, flushes event streams at once, and gets the certificate itself.

**nginx** (certificate lines are your own, or managed by certbot)

```nginx
server {
    listen 443 ssl;
    server_name meat-mode.example.com;
    # ssl_certificate and ssl_certificate_key here

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }

    location /api/events {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Connection "";
        proxy_buffering off;
        proxy_read_timeout 1h;
    }
}
```

nginx rewrites `Host` to the upstream's name unless you set it, which is why `proxy_set_header Host $host` is there. A location with its own `proxy_set_header` lines does not inherit the outer ones, so the event stream block repeats them.

**Traefik** (Docker provider; the entry point and certificate resolver names are the ones in your Traefik setup)

```yaml
services:
  meat-mode:
    # ...
    labels:
      - traefik.enable=true
      - traefik.http.routers.meat-mode.rule=Host(`meat-mode.example.com`)
      - traefik.http.routers.meat-mode.entrypoints=websecure
      - traefik.http.routers.meat-mode.tls.certresolver=letsencrypt
      - traefik.http.services.meat-mode.loadbalancer.server.port=8080
```

Traefik passes `Host` by default and does not buffer responses unless you add a buffering middleware. Traefik reaches the container over a shared Docker network, so drop the `ports:` mapping when you use it.

**NAS and other built-in proxies** (Synology reverse proxy, Unraid with Nginx Proxy Manager or SWAG, and similar). Create a host for `meat-mode.example.com` that forwards to the hub's address and port 8080, with a certificate for that name. Then look for: a switch to pass the `Host` header or "preserve host" (on); a "WebSocket" or custom-header section where you can add `X-Forwarded-Proto: https` if the proxy does not send it; and any option named buffering, caching or compression, which must be off for `/api/events`. Nginx Proxy Manager has an Advanced tab that takes the nginx lines above.

**Pairing and devices.** The bridge pairs with the proxy's address exactly as before: `meat-mode bridge pair https://meat-mode.example.com mm_xxxxxxxx`. Its `/bridge/` routes use their own bearer tokens, not the password. On iOS, the home-screen app keeps its own cookies, separate from Safari: log in once in each.

**Changing the password.** Make a new hash, put it in place, and restart the hub. Every device is logged out and asks for the new password. **Settings** has **Log out** for one device, which also stops that device's reminders. Five wrong passwords within a minute lock logins for the whole hub for a short time.

**A login at the proxy as well.** Authelia, basic auth or similar can sit in front of the hub for a second layer. Let `/bridge/` pass through it untouched, because bridges and `meat-mode add` send a bearer token, not a proxy login. `/healthz` is public.

### Host notes

- **Unraid**: add a container from the image you built (or a registry you push it to), map `/data` to an appdata share on a cache or array disk (not a remote share), and set the variables from `.env.example` in the template.
- **Synology and other NAS**: use Container Manager with Compose, and either install Tailscale on the NAS for HTTPS or use the NAS's reverse proxy (see above).
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

   Options: `--root DIR` (repeatable) to look for beads repos only in those folders instead of your whole home folder, `--allow-insecure-http` to allow plain HTTP to a hub that is not on loopback (not recommended). To keep the token out of your shell history, put `-` in its place and paste it on the next line, or leave it out and set `MEAT_MODE_BRIDGE_TOKEN`.

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

Only beads labelled `human` appear. Repos are told apart by their path, so `projects/garden` and `work/garden` both sync; the app shows the folder name, and the path where two names match.

## Let agents add to your personal list

An agent on a paired machine puts a to-do on your personal list with one command. It uses the hub address and token in the bridge's config, so it needs no token of its own and works whether or not the bridge service is running:

```sh
meat-mode add "Renew the library card" --due 2026-03-02 --note "It expires on Friday." --author planner
```

It prints the new item's key (`hub/personal/<id>`), and the to-do appears in the app at once, with the note as its first comment.

- `--due` takes an RFC 3339 time (`2026-03-02T09:00:00Z`), a local date and time (`2026-03-02 18:30`), or a local date alone, due at 9:00.
- `--author` is the agent's name, shown as the creator. It defaults to the machine's name.
- `--id` takes a UUID the agent generates. Running the command again with the same id changes nothing, so an agent can retry safely.

The fields and the HTTP call behind it are in [api.md](api.md#agents-adding-to-the-personal-list). Revoking the machine in Settings takes its agents' access back too.

## Moving from a personal beads repo

If a bridge was paired with `--personal`, the hub moves that repo's items into its own personal list, once each, with their dates, status, notes and recently closed items. It happens when you update the hub. Any taps still waiting for that machine are applied on the hub.

1. Update the hub first. The items move when it starts.
2. Then update the bridge and restart it (see Updating). It ignores the old personal setting in its config. A bridge that is not updated yet keeps sending the repo, and the hub only takes beads it has not had, so nothing is duplicated.

From then on your personal list lives on the hub. The bridge no longer treats the old beads repo as special: it shows up in Settings like any other repo, with its switch off. Leave it off, or delete the repo; switched on, its `human` beads would show a second time as agent items.

## Updating

- **Hub**: `git pull && docker compose up -d --build`. The database migrates itself on start.
- **Bridge**: replace the binary, then run `meat-mode bridge pair …` again with the same arguments (leaving out `--personal`), or restart the service (`systemctl --user restart meat-mode-bridge`, or on macOS `launchctl kickstart -k gui/$(id -u)/com.github.yourfriendaaron.meat-mode.bridge`).

## Uninstalling

- **Bridge**: `meat-mode bridge uninstall` removes the user service and the config. Then revoke the machine in the app's settings.
- **Hub**: `docker compose down`, and `docker volume rm meat-mode-data` to delete the data.

## Several machines

Pair each agent machine with its own name. Items are kept per machine, and a tap is replayed only by the machine that owns the item. A machine that is off keeps its actions waiting until it is back; the others carry on.

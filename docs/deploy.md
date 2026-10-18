# Deploying Meat Mode

Meat Mode has two parts:

- **The hub** is a container on any always-on host (a home server, a NAS, a Raspberry Pi, a VPS). It keeps your personal to-do list, a copy of your agents' items and the log of what you tapped, and sends reminders. It never connects to your agent machine.
- **The bridge** is the same `meat-mode` program, run as a user service on the machine where your beads repos live. It dials out to the hub, sends your agents' items up and replays your taps on them into beads with `bd`.

Your personal list needs only the hub. You can stop after step 2 and use it on its own, then add a bridge when you want your agents' tasks too.

You run the hub (step 1), then do the rest in the browser: the setup wizard sets the password, your name and time zone, reminders on each device, and pairs your agent machines (step 2).

There are two supported ways to reach the hub, and both need a publicly trusted HTTPS certificate, which browsers require before they allow reminders (Web Push) or installing the app to the home screen:

- **[Tailscale](https://tailscale.com)**, with no login. Who can reach the hub is the whole of its access control: on a tailnet that is your own signed-in devices, not everyone on your Wi-Fi. Tailscale also supplies the certificate. See [Expose it on your tailnet](#expose-it-on-your-tailnet).
- **A reverse proxy** you already run (Caddy, nginx, Traefik, a NAS's built-in proxy), with a password. The hub asks for it before it shows or accepts anything. See [Behind a reverse proxy](#behind-a-reverse-proxy).

A hub with no password has no login and logs a warning saying so. That is safe only when nothing but you can reach it. Never publish a hub without a password to your LAN or the internet.

| Setup | Hub address the bridge uses |
|---|---|
| Hub and bridge on one machine | `http://127.0.0.1:8080` (plain HTTP is accepted on loopback only) |
| Hub on another always-on host | its public address: `https://meat-mode.<your tailnet>.ts.net`, or the reverse proxy's `https://` address |

Either way, your phone and other devices open the app at that same address.

## 1. Run the hub

You need Docker or Podman on the hub host.

```sh
git clone <this repo> meat-mode && cd meat-mode
cp .env.example .env            # then edit: MEAT_MODE_PUBLIC_URL, MEAT_MODE_VAPID_SUBJECT
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

On its first start the hub has no password, so it prints a one-time setup code in its log. You need it in step 2:

```sh
docker logs meat-mode          # or: docker compose logs meat-mode
```

Look for the line `this hub has no password: open the app and set one with this setup code` and its `code=XXXX-XXXX-XXXX`. The same code is in the file `setup-code` in the data volume (`/data` in the container), for hosts such as a NAS where the log is easier to miss. The code works once and lasts an hour. Restart the hub for a new one. Wrong codes are limited hub-wide to 5 a minute, so if claiming keeps saying too many tries, restart the hub or set `MEAT_MODE_PASSWORD_HASH` instead.

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

Without a password, this path has no login, so do not also publish the port to your LAN or the internet. You can still set a password in step 2, and setup offers **No password, Tailscale only** to record that you keep the hub on your tailnet without one. To use a reverse proxy instead, or as well, set a password first (next section).

### Behind a reverse proxy

Use this when you already run a reverse proxy with a certificate, or want the app at your own domain. The hub then has a login: one password, one user.

**1. Set a password** before the proxy forwards to the hub. Until then, anyone who reaches it can read and change your list.

Open the app on the hub's loopback address, `http://127.0.0.1:8080`, in a browser on the hub host, or from your own computer through an SSH tunnel:

```sh
ssh -L 8080:127.0.0.1:8080 <hub host>      # then open http://127.0.0.1:8080
```

The setup wizard opens. Its **Secure the hub** step asks for the setup code from the hub's log (see step 1) and the new password, and logs this browser in. The rest of setup can wait until the proxy is up; run it again from **Settings**. Until you open the app at `MEAT_MODE_PUBLIC_URL`, the Connection check reports an address problem; that is expected. **Settings**, **Password** changes the password later. It needs the current one, and every other device is logged out.

**2. Or give the hub a password hash**, for scripted installs where nobody opens a browser first. A hash in the environment wins over a password set in the app, and the app then says the password is set by the environment and offers no way to change it. Make the hash with the image you built:

```sh
docker run --rm -it --entrypoint /meat-mode meat-mode hash-password
```

(or `meat-mode hash-password` with the binary). It asks for the password twice and prints an Argon2id string such as `$argon2id$v=19$m=…,t=…,p=…$<salt>$<hash>`. With input piped in, it reads one line.

Give the hash to the hub one of two ways. Setting both stops the hub at start.

- `MEAT_MODE_PASSWORD_HASH=<the hash>`.
- `MEAT_MODE_PASSWORD_HASH_FILE=<path>`, a file inside the container that holds the hash. This suits Docker and Compose secrets (see `compose.example.yaml`) and avoids the quoting below.

The hash holds `$` signs, which Docker tools can read as variables:

- In a `.env` file that Compose reads through `env_file:`, wrap the value in single quotes: `MEAT_MODE_PASSWORD_HASH='$argon2id$v=19$…'`. Compose uses single-quoted values literally; unquoted and double-quoted values have `$` expanded.
- Written directly in `compose.yaml` under `environment:`, double every `$` as `$$`.
- With `docker run --env-file`, the line is taken literally, quotes included, so write it with no quotes. The single-quoted form above would put the quotes into the hash and the hub would refuse it. A `.env` shared between `docker run` and Compose therefore cannot hold the hash; use the `_FILE` variant.

Start the hub and check the log: it says `login is on, with the password from the environment`, and it prints no setup code.

**3. Publish the port to loopback only** (`127.0.0.1:8080:8080`, as above) and have the proxy forward to it. If the proxy runs in another container, put both on a shared network and forward to the hub container's port 8080 instead of publishing it.

**4. Set `MEAT_MODE_PUBLIC_URL`** to the proxy's address, `https://meat-mode.example.com`: the address people open, exactly. The proxy must:

- Pass the original `Host` header through. If it rewrites it, add the name the hub receives to `MEAT_MODE_ALLOWED_HOSTS`.
- Send `X-Forwarded-Proto`.
- Not buffer responses from `/api/events`, the server-sent event stream that keeps the app current. The hub marks it unbuffered, which nginx honors; the nginx snippet turns buffering off for it as well.
- Present a certificate that browsers trust, such as Let's Encrypt. Web Push and installing to the home screen do not work with a self-signed one.

**Settings**, **Connection** checks all of this from the browser: that the address matches `MEAT_MODE_PUBLIC_URL` and the hub accepts it, that the page is a secure context, that live updates arrive at once (a buffering proxy holds them back), that the device and hub clocks agree, and that the hub has its reminder keys. Each problem comes with what to change.

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

**Changing the password.** A password set in the app changes in **Settings**, **Password**; every other device is logged out. A password from the environment changes there: make a new hash, put it in place, and restart the hub. Every device is logged out and asks for the new password. **Settings** has **Log out** for one device, which also stops that device's reminders. Five wrong passwords within a minute lock logins for the whole hub for a short time.

**A login at the proxy as well.** Authelia, basic auth or similar can sit in front of the hub for a second layer. Let `/bridge/` pass through it untouched, because bridges and `meat-mode add` send a bearer token, not a proxy login. `/healthz` is public.

### Host notes

- **Unraid**: add a container from the image you built (or a registry you push it to), map `/data` to an appdata share on a cache or array disk (not a remote share), and set the variables from `.env.example` in the template.
- **Synology and other NAS**: use Container Manager with Compose, and either install Tailscale on the NAS for HTTPS or use the NAS's reverse proxy (see above).
- **Raspberry Pi**: the image builds for arm64 as is.

## 2. Set up in the app

Open the app at its address. On a new hub the setup wizard opens by itself and walks through the rest. Skip any step you like: each one is also in **Settings**, and **Settings**, **Setup** has **Run setup again**.

| Step | What it does |
|---|---|
| Connection check | Checks the address, the secure connection, live updates, the clocks and the reminder keys, and says what to fix (also **Settings**, **Connection**). |
| Secure the hub | Sets the password with the setup code from the hub's log (see step 1), and logs this browser in. With a password hash in the environment, it says so and asks for nothing. On a hub kept on Tailscale, **No password, Tailscale only** stops it asking. |
| Name and time zone | Your name and time zone, kept on the hub (also **Settings**, **Name and time zone**). The name starts as `MEAT_MODE_USER` and the time zone as this device's. Agents' dates are read in this zone; see [Let agents add to your personal list](#let-agents-add-to-your-personal-list). |
| Install the app | Puts Meat Mode on the home screen or dock: an **Install** button when a Chromium browser offers it, and the steps for Safari on an iPhone, iPad or Mac. On an iPhone or iPad, reminders work only in the Home Screen app, so install it there first. |
| Reminders | Asks this device for permission to notify, then sends a test reminder to this device, and to ntfy when `MEAT_MODE_NTFY_URL` is set. If you block it, the step says how to allow it again. |
| Appearance | Dark, light, or following the device's setting. |
| First to-do | Teaches quick add: it reads a date and a `#list` out of what you type, as you type. A reminder fires at the due time, or at 9:00 when only a date is given. |
| Agent machines | Optional. Names a machine, shows the commands to install the bridge and pair it, and says "Connected" when the bridge first reports in. Then **Choose repos** and **Tell your agents**, below. |

On each other device you open the app on, a banner offers **Set up this device**, which runs only the device steps: install, reminders and appearance. **Settings**, **Setup** has it too.

## 3. Install the bridge on the agent machine

The setup wizard's **Agent machines** step, or **Settings**, **Agent machines**, shows the install command for the platform you pick. By hand:

The bridge needs `bd` 1.3.0 or later on that machine.

Install the program, either from a release binary for your platform (linux or darwin, amd64 or arm64; put it somewhere on your `PATH` such as `~/.local/bin`), or with Go:

```sh
go install github.com/yourfriendaaron/meat-mode/cmd/meat-mode@latest
```

## 4. Pair

1. In the setup wizard's **Agent machines** step, or under **Settings**, **Agent machines**, add a machine with a name such as `laptop`. The app shows a token once, and the exact command.
2. On the agent machine run that command:

   ```sh
   meat-mode bridge pair https://meat-mode.example.com mm_xxxxxxxx
   ```

   Options: `--root DIR` (repeatable) to look for beads repos only in those folders instead of your whole home folder, `--allow-insecure-http` to allow plain HTTP to a hub that is not on loopback (not recommended). To keep the token out of your shell history, put `-` in its place and paste it on the next line, or leave it out and set `MEAT_MODE_BRIDGE_TOKEN`.

Pairing checks the token with the hub, saves the config (`~/.config/meat-mode/bridge.json` on Linux, `~/Library/Application Support/meat-mode/bridge.json` on macOS) and installs a user service:

- **Linux**: a systemd user unit, `meat-mode-bridge.service`. Logs: `journalctl --user -u meat-mode-bridge`. To keep it running while you are logged out, enable lingering once: `loginctl enable-linger $USER`.
- **macOS**: a launchd agent, `com.github.yourfriendaaron.meat-mode.bridge`. Logs: `~/Library/Logs/meat-mode-bridge.log`.

`meat-mode bridge status` shows the config, the repos found and whether the hub answers. The app shows the machine as connected once the bridge first reports in.

## 5. Choose which repos sync

The bridge looks through your home folder for beads repos, on start and every 5 minutes, at any depth. It skips dependency, cache and app-data folders, Git worktrees and symlinks, and a `.beads` folder without a database (such as bd's own `~/.beads`).

Every repo it finds appears in the app under **Settings**, below its machine, with a switch. The setup wizard's **Choose repos** step shows the same switches. Switches start off. Turn one on and that repo's items arrive within a few seconds; turn it off and they leave the app. If the machine is off, the switch shows "Waiting for" the machine until it is back. The machine keeps your choices in its own config file, and it only accepts a switch for a repo it found itself.

You can also opt a repo in from the repo itself, with this line in its `.beads/config.yaml`. A switch set in the app wins over it.

```yaml
meat-mode.sync: true
```

Only beads labelled `human` appear. Repos are told apart by their path, so `projects/garden` and `work/garden` both sync; the app shows the folder name, and the path where two names match.

## Let agents add to your personal list

An agent on a paired machine puts a to-do on your personal list with one command. It uses the hub address and token in the bridge's config, so it needs no token of its own and works whether or not the bridge service is running:

```sh
meat-mode add "Renew the library card" --due 2026-03-02 --note "It expires on Friday." --author planner
meat-mode add "Water the plants" --due "2026-03-02 20:30" --every day
```

It prints the new item's id (such as `mm-12`), and the to-do appears in the app at once, with the note as its first comment.

- `--due` takes a date and time (`2026-03-02 18:30`) or a date alone (`2026-03-02`, due at 9:00), read in the hub's time zone, not the machine's: the one set in the wizard's **Name and time zone** step or in **Settings**. The command asks the hub for it, so an agent on a server running in UTC still gets your 9:00. A time with an offset (`2026-03-02T09:00:00Z`) is used as written. While the hub has no time zone, the machine's own is used.
- `--every` makes the to-do repeat, and needs `--due`, its first date. It takes `day`, `week`, `month` or `year`, a number of them (`"3 days"`, `"2 weeks"`), `weekday` for Monday to Friday, days of the week (`mon,thu`), or both (`"2 weeks on mon,thu"`). When the to-do is done it stays on the list and moves to its next date, at the same time of day in the hub's time zone. `--from-done` counts the next date from the day it is done instead: `--every "3 days" --from-done` comes back 3 days after each done.
- `--author` is the agent's name, shown as the creator. It defaults to the machine's name.
- `--id` takes a UUID the agent generates. Running the command again with the same id changes nothing, so an agent can retry safely.

An agent can change a to-do its own machine added, by that id:

```sh
meat-mode edit mm-12 --due "2026-03-09 18:30" --priority 1 --author planner
meat-mode done mm-12
meat-mode reopen mm-12
```

- `meat-mode edit` changes only the fields it is given: `--title`, `--description` (`""` clears it), `--due` (read as for `add`) or `--clear-due`, `--every` (with `--from-done`, read as for `add`) or `--no-repeat`, and `--priority` from 0 (highest) to 4. `--every` needs a due date, already set or given with `--due`; `--clear-due` also stops a to-do repeating.
- `meat-mode done` marks the to-do done, or moves a repeating one to its next date; `meat-mode reopen` opens it again.
- Each takes `--author` and `--id` as `add` does, and prints the to-do's id.
- A to-do you added in the app, or one another machine added, is not the agent's to change: the command says the hub has no such to-do from this machine. A closed to-do can be changed for the 7 days the app still shows it.

The name set in the app (or `MEAT_MODE_USER` until you set one) is the name your notes are signed with, as `<name>-app`, so agents can tell your replies from their own.

To tell your agents about all this, copy the text from the setup wizard's **Tell your agents** step, or from **Settings**, **Instructions for agents**, into the instruction file they read on that machine, such as `AGENTS.md` or `CLAUDE.md`. It covers raising a task for you with `bd create … -l human` and adding to your list with `meat-mode add`, names the machine, and holds no token or address.

The fields and the HTTP calls behind these commands are in [api.md](api.md#agents-adding-to-the-personal-list). Revoking the machine in Settings takes its agents' access back too.

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

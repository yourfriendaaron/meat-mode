# Deploying Meat Mode

Meat Mode has two parts:

- **The hub** is a container on any always-on host (a home server, a NAS, a Raspberry Pi, a VPS). It keeps your personal to-do list, a copy of your agents' items and the log of what you tapped, and sends reminders. It never connects to your agent machine.
- **The bridge** is the same `meat-mode` program, run as a user service on the machine where your agents' repos live. It dials out to the hub, sends your agents' items up and replays your taps on them into the repo's task tracker, such as beads with `bd`. [trackers.md](trackers.md) lists the trackers it reads.

Your personal list needs only the hub. You can stop after step 2 and use it on its own, then add a bridge when you want your agents' tasks too.

You run the hub (step 1), then do the rest in the browser: the setup wizard sets the password, your name and time zone, reminders on each device, and pairs your agent machines (step 2).

The hub can wake coding agents on your machines, so it belongs on your private network only, never on the internet. It always has a password as well. There are two supported ways to reach it, and both need a publicly trusted HTTPS certificate, which browsers require before they allow reminders (Web Push) or installing the app to the home screen:

- **[Tailscale](https://tailscale.com)**: a Tailscale Service, or Tailscale Serve on the hub host, forwarding to the hub on loopback. Only your own signed-in devices reach it, and Tailscale supplies the certificate. See [Expose it on your tailnet](#expose-it-on-your-tailnet).
- **A reverse proxy on your LAN** (Caddy, nginx, Traefik, a NAS's built-in proxy) that only your home network reaches. See [Behind a reverse proxy](#behind-a-reverse-proxy).

Do not put the hub on the internet. That rules out Tailscale Funnel, Cloudflare Tunnel, a port forward on your router and a reverse proxy that answers from the internet. The hub refuses such requests and checks public DNS for its own name (see [Private network only](#private-network-only)), but those are a safety net, not a way to publish it.

Publish the container's port on `127.0.0.1` (as in `compose.example.yaml`) or on the hub host's own tailnet address, never on all addresses (`0.0.0.0`, which is what a bare `-p 8080:8080` means).

| Setup | Hub address the bridge uses |
|---|---|
| Hub and bridge on one machine | `http://127.0.0.1:8080` (plain HTTP is accepted on loopback only) |
| Hub on another always-on host | its `MEAT_MODE_PUBLIC_URL`: `https://meat-mode.<your tailnet>.ts.net`, or the LAN reverse proxy's `https://` address |

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

Until you set a password, the hub serves only that setup step: the app shows a form for the setup code and the new password, and nothing else. Look for the line `this hub has no password: open the app and set one with this setup code` and its `code=XXXX-XXXX-XXXX`. The same code is in the file `setup-code` in the data volume (`/data` in the container), for hosts such as a NAS where the log is easier to miss. The code works once and lasts an hour. Restart the hub for a new one. Wrong codes are limited hub-wide to 5 a minute, so if claiming keeps saying too many tries, restart the hub or set `MEAT_MODE_PASSWORD_HASH` instead.

The data volume holds the database and the Web Push keys. Keep it on local storage: SQLite's write-ahead log does not work over NFS or SMB. If you bind-mount a host directory instead of a named volume, make it writable by user id 65532 (`chown 65532:65532 <dir>`); the hub says so and exits if it cannot write.

### Expose it on your tailnet

Publish port 8080 only to loopback (as above), so nothing on your LAN reaches the hub directly, and let Tailscale serve it over HTTPS. Install Tailscale on the hub host first, and turn on HTTPS certificates for your tailnet once in the admin console (**DNS**, **HTTPS Certificates**).

**Tailscale Service (recommended).** The address belongs to the Service, not to the host, so you can move the hub to another host later without changing it.

1. In the admin console, under **Services**, add a Service named `meat-mode` with the endpoint `tcp:443`.
2. On the hub host run `tailscale serve --bg --service=svc:meat-mode --https=443 http://127.0.0.1:8080`.
3. In the admin console, approve the hub host for the Service.

The app is then at `https://meat-mode.<your tailnet>.ts.net`.

**`tailscale serve` on the host (fallback).** For trying it out, or a host that will never move, run `tailscale serve --bg --https=443 http://127.0.0.1:8080` instead. The app is then at `https://<host name>.<your tailnet>.ts.net`. That address belongs to the host: if you move the hub, each bridge must pair again and each device must turn reminders on again, because browsers tie push subscriptions and installed apps to the address.

Then set `MEAT_MODE_PUBLIC_URL` to that address and restart the hub. The hub refuses to start without an `https://` public address (plain `http://` is accepted for localhost only), and it answers only to `localhost`, `127.0.0.1`, `[::1]`, the public address's name and any names in `MEAT_MODE_ALLOWED_HOSTS`.

Certificates are recorded in public Certificate Transparency logs, so the `ts.net` name is publicly visible, though only your tailnet can reach it. Keep anything private out of the Service and host names.

Use `tailscale serve`, never `tailscale funnel`: Funnel puts the address on the internet. The hub refuses every request Funnel forwards, and refuses to start while public DNS gives its name a public address, which Funnel's names have for a few hours after Funnel is turned off. Open the address and set the password with the setup code (step 2).

### Behind a reverse proxy

Use this when you already run a reverse proxy on your LAN, or want the app at your own domain inside your home network. The proxy must answer only on your LAN: the hub refuses a request that a proxy forwards from an internet address.

Your own domain needs two things that keep it off the internet:

- **DNS**: point the name at the proxy's private address, in your LAN's own DNS or in a public record that holds only that private address. The hub checks public DNS for its name, and a public address there stops it at start.
- **The certificate**: get it with a DNS challenge (Caddy, Traefik, certbot and most NAS proxies support one). An HTTP challenge needs the name reachable from the internet.

**1. Set a password** before the proxy forwards to the hub. Until then the hub serves only its setup step, and anyone who has the setup code could claim it.

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
- Send `X-Forwarded-Proto`, and `X-Forwarded-For` with the client's address. Inside a container the hub sees only the container network as the peer, so that header is how it tells a LAN request from an internet one.
- Not buffer responses from `/api/events`, the server-sent event stream that keeps the app current. The hub marks it unbuffered, which nginx honors; the nginx snippet turns buffering off for it as well.
- Present a certificate that browsers trust, such as Let's Encrypt. Web Push and installing to the home screen do not work with a self-signed one.

**Settings**, **Connection** checks all of this from the browser: that the address matches `MEAT_MODE_PUBLIC_URL` and the hub accepts it, that public DNS has no public address for the name, that the page is a secure context, that live updates arrive at once (a buffering proxy holds them back), that the device and hub clocks agree, and that the hub has its reminder keys. Each problem comes with what to change.

**Caddy**

```
meat-mode.example.com {
	reverse_proxy 127.0.0.1:8080
}
```

Caddy passes `Host` unchanged, sets `X-Forwarded-Proto` and `X-Forwarded-For`, and flushes event streams at once. Give it a DNS challenge for the certificate (its `tls` directive with a `dns` provider), since the name is not reachable from the internet.

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

Traefik passes `Host` and sets `X-Forwarded-For` by default, and does not buffer responses unless you add a buffering middleware. Use a DNS challenge in the certificate resolver. Traefik reaches the container over a shared Docker network, so drop the `ports:` mapping when you use it.

**NAS and other built-in proxies** (Synology reverse proxy, Unraid with Nginx Proxy Manager or SWAG, and similar). Create a host for `meat-mode.example.com` that forwards to the hub's address and port 8080, with a certificate for that name. Then look for: a switch to pass the `Host` header or "preserve host" (on); a "WebSocket" or custom-header section where you can add `X-Forwarded-Proto: https` if the proxy does not send it; and any option named buffering, caching or compression, which must be off for `/api/events`. Nginx Proxy Manager has an Advanced tab that takes the nginx lines above.

**Pairing and devices.** The bridge pairs with the proxy's address exactly as before: `meat-mode bridge pair https://meat-mode.example.com mm_xxxxxxxx`. Its `/bridge/` routes use their own bearer tokens, not the password. On iOS, the home-screen app keeps its own cookies, separate from Safari: log in once in each.

**Changing the password.** A password set in the app changes in **Settings**, **Password**; every other device is logged out. A password from the environment changes there: make a new hash, put it in place, and restart the hub. Every device is logged out and asks for the new password. **Settings** has **Log out** for one device, which also stops that device's reminders. Five wrong passwords within a minute lock logins for the whole hub for a short time.

**Sessions** end 14 days after a device last used the hub, and 90 days after it logged in, whichever comes first. Adding a machine needs the password entered within the last 10 minutes; otherwise the app asks for it again in place.

**A login at the proxy as well.** Authelia, basic auth or similar can sit in front of the hub for a second layer. Let `/bridge/` pass through it untouched, because bridges and `meat-mode add` send a bearer token, not a proxy login, and `/calendar/` too if you use the [calendar feed](#see-your-to-dos-in-a-calendar-app), because calendar apps cannot log in. `/healthz` is public.

### Private network only

The hub answers only on a private network, whichever way you reach it:

- **It refuses requests from the internet.** A request that Tailscale Funnel forwards, one whose forwarding headers (`X-Forwarded-For`, `Forwarded`, `X-Real-IP` and the like) name an internet address, or one whose direct peer is an internet address, gets `403`. Private means loopback, the RFC 1918 ranges, the RFC 6598 shared range Tailscale uses, and IPv6 link-local and unique-local addresses. No setting turns this off.
- **It checks public DNS for its own name**, at start and every hour. If public resolvers give `MEAT_MODE_PUBLIC_URL`'s name an internet address, the hub does not start, and says which addresses and how to fix it: turn Funnel off, or remove the public record. Found later, the address is logged and shown as a warning at the top of **Settings**.
- **The bridge refuses a public hub.** Pairing and every connection check that the hub's address is private.

If your LAN uses addresses outside those ranges, such as a global IPv6 prefix from your internet provider, list them in `MEAT_MODE_TRUSTED_NETWORKS`, as comma-separated CIDRs of at most `/16` for IPv4 and `/48` for IPv6, for the hub and, at pairing, for the bridge. The details are in [api.md](api.md#private-network-only).

### Host notes

- **Unraid**: add a container from the image you built (or a registry you push it to), map `/data` to an appdata share on a cache or array disk (not a remote share), and set the variables from `.env.example` in the template.
- **Synology and other NAS**: use Container Manager with Compose, and either install Tailscale on the NAS for HTTPS or use the NAS's reverse proxy on your LAN (see above). Leave the NAS's own internet access features (QuickConnect, router port forwarding) off for the hub.
- **Raspberry Pi**: the image builds for arm64 as is.

## 2. Set up in the app

Open the app at its address. A hub with no password shows only the setup code form: enter the code from the hub's log (see step 1) and the new password, and this browser is logged in. On a new hub the setup wizard then opens by itself and walks through the rest. Skip any step you like: each one is also in **Settings**, and **Settings**, **Setup** has **Run setup again**.

| Step | What it does |
|---|---|
| Connection check | Checks the address, public DNS for the hub's name, the secure connection, live updates, the clocks and the reminder keys, and says what to fix. It also notes whether the request came through Tailscale Serve (also **Settings**, **Connection**). |
| Secure the hub | Says where the password comes from: the one you set with the setup code before the app opened, or a hash in the environment. |
| Name and time zone | Your name and time zone, kept on the hub (also **Settings**, **Name and time zone**). The name starts as `MEAT_MODE_USER` and the time zone as this device's. Agents' dates are read in this zone; see [Let agents add to your personal list](#let-agents-add-to-your-personal-list). |
| Install the app | Puts Meat Mode on the home screen or dock: an **Install** button when a Chromium browser offers it, and the steps for Safari on an iPhone, iPad or Mac. On an iPhone or iPad, reminders work only in the Home Screen app, so install it there first. |
| Reminders | Asks this device for permission to notify, then sends a test reminder to this device, and to ntfy when `MEAT_MODE_NTFY_URL` is set. If you block it, the step says how to allow it again. |
| Appearance | Dark, light, or following the device's setting. |
| First to-do | Teaches quick add: it reads a date and a `#list` out of what you type, as you type. A reminder fires at the due time, or at 9:00 when only a date is given. |
| Agent machines | Optional. Names a machine, shows the commands to install the bridge and pair it, and says "Connected" when the bridge first reports in. Then **Choose repos** and **Tell your agents**, below. |

On each other device you open the app on, a banner offers **Set up this device**, which runs only the device steps: install, reminders and appearance. **Settings**, **Setup** has it too.

## 3. Install the bridge on the agent machine

The setup wizard's **Agent machines** step, or **Settings**, **Agent machines**, shows the install command for the platform you pick. By hand:

The bridge needs the tool of each tracker your agents use on that machine, such as `bd` 1.3.0 or later for beads. Each tracker's page under [trackers/](trackers.md) says what to install. A tracker whose tool is missing is skipped, and the bridge refuses to start only when none is available. To use a tool that is not on your `PATH`, give its path in `trackerBins` in `bridge.json`, such as `{"trackerBins": {"beads": "/opt/beads/bin/bd"}}`.

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

   Options: `--root DIR` (repeatable) to look for repos only in those folders instead of your whole home folder, `--allow-insecure-http` to allow plain HTTP to a hub that is not on loopback (not recommended). To keep the token out of your shell history, put `-` in its place and paste it on the next line, or leave it out and set `MEAT_MODE_BRIDGE_TOKEN`.

Pairing checks the token with the hub, saves the config (`~/.config/meat-mode/bridge.json` on Linux, `~/Library/Application Support/meat-mode/bridge.json` on macOS) and installs a user service:

- **Linux**: a systemd user unit, `meat-mode-bridge.service`. Logs: `journalctl --user -u meat-mode-bridge`. To keep it running while you are logged out, enable lingering once: `loginctl enable-linger $USER`.
- **macOS**: a launchd agent, `com.github.yourfriendaaron.meat-mode.bridge`. Logs: `~/Library/Logs/meat-mode-bridge.log`.

`meat-mode bridge status` shows the config, the trackers with their versions, the repos found and whether the hub answers. The app shows the machine as connected once the bridge first reports in.

## 5. Choose which repos sync

The bridge looks through your home folder for the repos of every tracker it has, on start and every 5 minutes, at any depth. It skips dependency, cache and app-data folders, Git worktrees, symlinks and each tracker's own store, and a store a tracker does not count, such as a `.beads` folder without a database (bd's own `~/.beads`). A repo with two trackers syncs from one of them, beads first.

Every repo it finds appears in the app under **Settings**, below its machine, with a switch. The setup wizard's **Choose repos** step shows the same switches. Switches start off. Turn one on and that repo's items arrive within a few seconds; turn it off and they leave the app. If the machine is off, the switch shows "Waiting for" the machine until it is back. The machine keeps your choices in its own config file, and it only accepts a switch for a repo it found itself.

You can also opt a repo in from the repo itself, with this line in a `.meat-mode.yaml` file at its root. A switch set in the app wins over it.

```yaml
sync: true
```

A tracker can have its own place for that line too; for beads it is `meat-mode.sync: true` in `.beads/config.yaml`. Only the tasks marked for you appear: in beads, those labelled `human`. Each tracker's page says how it marks them, and the app offers only the actions the repo's tracker supports. Repos are told apart by their path, so `projects/garden` and `work/garden` both sync; the app shows the folder name, and the path where two names match.

## Reminders

Each device you turned reminders on for gets one at an item's due time, and at the end of a snooze. A repeating to-do reminds for each date it comes round to. Closed items never remind.

**Early reminders.** In **Settings**, choose how long before the due time an early reminder comes, up to a week; it starts off. The early reminder says how long is left, and the reminder at the due time still comes. A to-do on your personal list can have its own lead time, or none, set when you edit it; agents' items use the one in Settings. Changing the lead time never sends an early reminder again for a due time it already reminded about. A snooze end has no early reminder.

**Missed reminders.** When the hub could not remind on time, because it was stopped or could not run its checks, it catches up on the next check. One reminder that is more than 5 minutes late comes on its own and says how long ago it came due. Two or more come as one, "3 reminders came due while reminders were off", which opens the app. Each missed reminder is told once, however late.

An item that was already more than 5 minutes past its due time when the hub first saw it, such as an old bead on a repo you just switched on, does not remind for that time.

**Done and Snooze.** A reminder for one item has **Done** and **Snooze 1 hour** buttons that act without opening the app. A reminder that gathers several missed ones has none. ntfy shows the reminder without these buttons.

### Your own push server for the Android app

The Android app gets its pushes through UnifiedPush, from a distributor such as ntfy, or through Google's push service when no distributor is installed. The hub sends only to push servers at public addresses, so a registration cannot point it at something inside your network. To use a push server that is not on the public internet, such as your own ntfy on the tailnet or the LAN, name it in `MEAT_MODE_PUSH_ALLOW` and restart the hub:

```sh
MEAT_MODE_PUSH_ALLOW=ntfy.example.com,192.0.2.0/24
```

- A host name admits exactly that host, whatever it resolves to; `ntfy.example.com` does not admit `push.ntfy.example.com`. Give the name only: no `https://`, port or path.
- A CIDR, such as `192.0.2.0/24` or `2001:db8::/48`, admits any push server whose address is inside it; an entry wider than /16 for IPv4 or /48 for IPv6 stops the hub at start. A bare address admits that one address.
- An entry that is neither stops the hub at start with a message naming it.

The push server still needs `https://`. Pushes follow no redirects and time out after 10 seconds. Settings lists each browser and phone that gets reminders and can revoke any of them.

## See your to-dos in a calendar app

The hub can publish your open to-dos that have a due date, from every list, as a calendar feed that calendar apps subscribe to. Each to-do shows as a 15-minute event at its due time, titled with the to-do, with a link that opens it in the app. A repeating to-do shows its next date only. The feed is read-only: changes made in the calendar app do not come back.

1. In **Settings**, turn on the calendar feed. The app shows its address once.
2. In your calendar app, add a calendar subscription from that address (Apple Calendar calls it a new calendar subscription; Thunderbird, a new calendar on the network).

Calendar apps fetch the feed on their own schedule, from every hour to once a day (Outlook can take longer), so a change can take that long to show.

Anyone with the address can read your to-dos' titles and due times, without a password: calendar apps cannot log in, so the address is the only key. Keep it private. To stop an address working, make a new one in **Settings**, which replaces it, or turn the feed off.

The feed works only for calendar apps on your own devices, such as Apple Calendar or Thunderbird, and only while that device is on your private network. Google Calendar and Outlook (Outlook.com, Outlook on the web and the new Outlook) fetch a subscribed feed from their own servers on the internet, which cannot reach the hub.

## Let agents add to your personal list

An agent on a paired machine puts a to-do on your personal list with one command. It uses the hub address and token in the bridge's config, so it needs no token of its own and works whether or not the bridge service is running:

```sh
meat-mode add "Renew the library card" --due 2026-03-02 --note "It expires on Friday." --author planner
meat-mode add "Water the plants" --due "2026-03-02 20:30" --every day
```

It prints the new item's id (such as `mm-12`), and the to-do appears in the app at once, with the note as its first comment.

- `--due` takes a date and time (`2026-03-02 18:30`) or a date alone (`2026-03-02`, due at 9:00), read in the hub's time zone, not the machine's: the one set in the wizard's **Name and time zone** step or in **Settings**. The command asks the hub for it, so an agent on a server running in UTC still gets your 9:00. A time with an offset (`2026-03-02T09:00:00Z`) is used as written. While the hub has no time zone, the machine's own is used.
- `--every` makes the to-do repeat, and needs `--due`, its first date. It takes `day`, `week`, `month` or `year`, a number of them (`"3 days"`, `"2 weeks"`), `weekday` for Monday to Friday, days of the week (`mon,thu`), or both (`"2 weeks on mon,thu"`). When the to-do is done it stays on the list and moves to its next date, at the same time of day in the hub's time zone. `--from-done` counts the next date from the day it is done instead: `--every "3 days" --from-done` comes back 3 days after each done. `--until 2026-11-26` ends the series on that date: the to-do repeats up to it, and the done after its last date closes it.
- `--author` is the agent's name, shown as the creator. It defaults to the machine's name.
- `--id` takes a UUID the agent generates. Running the command again with the same id changes nothing, so an agent can retry safely.

An agent can change a to-do its own machine added, by that id:

```sh
meat-mode edit mm-12 --due "2026-03-09 18:30" --priority 1 --author planner
meat-mode done mm-12
meat-mode reopen mm-12
```

- `meat-mode edit` changes only the fields it is given: `--title`, `--description` (`""` clears it), `--due` (read as for `add`) or `--clear-due`, `--every` (with `--from-done`, read as for `add`) or `--no-repeat`, `--until DATE` or `--no-until` for the end of the to-do's repeat, given with `--every` or kept from the one it has, and `--priority` from 0 (highest) to 4. `--every` needs a due date, already set or given with `--due`; `--clear-due` also stops a to-do repeating.
- `meat-mode done` marks the to-do done, or moves a repeating one to its next date; `meat-mode reopen` opens it again.
- Each takes `--author` and `--id` as `add` does, and prints the to-do's id.
- A to-do you added in the app, or one another machine added, is not the agent's to change: the command says the hub has no such to-do from this machine. A closed to-do can be changed for the 7 days the app still shows it.

The name set in the app (or `MEAT_MODE_USER` until you set one) is the name your notes are signed with, as `<name>-app`, so agents can tell your replies from their own.

To tell your agents about all this, copy the text from the setup wizard's **Tell your agents** step, or from **Settings**, **Instructions for agents**, into the instruction file they read on that machine, such as `AGENTS.md` or `CLAUDE.md`. It covers raising a task for you with `bd create … -l human` and adding to your list with `meat-mode add`, names the machine, and holds no token or address.

The fields and the HTTP calls behind these commands are in [api.md](api.md#agents-adding-to-the-personal-list). Revoking the machine in Settings takes its agents' access back too.

## Wake an agent on a task

With agents allowed in a repo whose tracker supports them (see [trackers.md](trackers.md)), one tap on an item asks an agent to take it on. The bridge writes your note to the task, then starts the agent on that machine. The agent reads the task and its notes with the tracker's own commands, then does the work and closes the task, or replies with a note, and the app tells you either way. A note you send to the agent while it works (**Send to** in the app) goes to the same run, which reads it at its next step; a plain **Save note** stays on the task for later. One task never has two agents.

Only your own asks start an agent. The agent's notes and closes, and your other taps (notes, Done, Won't do), never do.

### Allow agents in a repo

Agents switch on at the agent machine, never from the app, so someone who takes over the hub cannot let agents into more repos. On that machine, run:

```sh
meat-mode bridge agents on projects/garden    # or the repo's folder, such as . inside it
meat-mode bridge agents                       # lists the repos it finds, with their switch
```

`<repo>` is the name the app shows under the repo (`projects/garden`) or the path of its folder. The bridge keeps the switch in the `agents` map of its own `bridge.json`, takes it only for a repo it found itself, and the app shows it within one poll. No line in a repo can switch agents on.

In the app, under **Settings**, **Agent machines**, a synced repo with agents off shows that command, with a **Copy** button. A repo with agents on shows an **Agents can work here** switch, which turns them off; `meat-mode bridge agents off <repo>` does the same on the machine. An ask in a repo with agents off still leaves your note, and the run shows stopped with the reason.

The default agent is Claude Code, run as `claude -p` with `--permission-mode auto` and no permission prompts. **This is not a sandbox.** Auto mode lets the agent run commands as you, in your home folder, with your credentials: in testing it changed the global Git config and deleted a file in the home folder without asking. Its instructions tell it never to send, post, spend, delete or push for you, and to stop and ask in a note when it needs you, but nothing enforces that. Allow agents only in repos where you would let an agent work unattended. Before the agent even starts, the bridge creates a Git worktree of the repo for each task, which runs the repo's own Git hooks, such as `post-checkout`, as you.

The agent machine needs:

- Claude Code installed and signed in, with `claude` on your `PATH` when you run `meat-mode bridge pair`, so the service finds it (pair again after installing it).
- A Git repo with at least one commit: each task's agent works in its own Git worktree.

### Settings

The bridge reads these variables (see `.env.example`):

| Variable | Default | What it sets |
|---|---|---|
| `MEAT_MODE_AGENT_COMMAND` | `claude -p --input-format stream-json --output-format stream-json --verbose --replay-user-messages --permission-mode auto --permission-prompts none` | The agent command, split on spaces, with no shell. |
| `MEAT_MODE_AGENT_NAME` | `agent` | The name shown for the agent. A task an agent created shows its creator's name instead. |
| `MEAT_MODE_AGENT_TIMEOUT` | `30m` | The longest one run may take. |
| `MEAT_MODE_AGENT_BUDGET_USD` | `2` | The most one run may spend, passed to the agent as `--max-budget-usd`. |
| `MEAT_MODE_AGENT_RUNS_PER_TASK` | `10` | How many runs one task may have, ever. Nothing resets the count. |
| `MEAT_MODE_AGENT_CONCURRENT` | `2` | How many runs may be live at once on the machine. |

The bridge service does not read your shell's environment. To set these on Linux, run `systemctl --user edit meat-mode-bridge`, add the lines below, and restart the service:

```ini
[Service]
Environment=MEAT_MODE_AGENT_BUDGET_USD=1
Environment=MEAT_MODE_AGENT_TIMEOUT=15m
```

The service runs with a short `PATH`: the folders of the trackers' tools (such as `bd`) and of the agent command as `meat-mode bridge pair` found them, then the system folders. If the agent's work needs other tools, such as `go` or `pnpm`, add an `Environment=PATH=…` line the same way. On macOS, add the variables to the `EnvironmentVariables` of `~/Library/LaunchAgents/com.github.yourfriendaaron.meat-mode.bridge.plist` and restart it; pairing again rewrites that file.

Another agent command must speak Claude Code's stream-json protocol on its standard input and output (user messages in; `system`, `assistant`, `user` and `result` lines out, with `session_id`), echo each message it reads as a `user` line with `isReplay`, and accept `--max-budget-usd <dollars>` and `--resume <session id>`, which the bridge adds.

### How a run ends

A run shows **working** while the agent runs. When it ends, the task shows **done** if the agent closed the bead, **waiting** on you if it wrote a note, or **stopped** with a reason:

- **stopped**: you stopped it. Stop ends the agent and everything it started.
- **usage limit**: the agent's account hit its usage limit; the detail says when it resets.
- **time limit**, **budget**: the run passed `MEAT_MODE_AGENT_TIMEOUT`, or spent `MEAT_MODE_AGENT_BUDGET_USD`.
- **run limit**: the task has had its `MEAT_MODE_AGENT_RUNS_PER_TASK` runs.
- **busy**: `MEAT_MODE_AGENT_CONCURRENT` runs were already live on the machine.
- **not allowed**: agents are switched off for the repo. Switch them on at its machine with `meat-mode bridge agents on <repo>`.
- **no reply**: the agent ended without a note or closing the bead.
- **failed**, **crashed**: the agent command did not start or reported an error, or its process died.
- **restarted**: the bridge stopped while the agent ran. Stopping the bridge ends its runs; after a crash, the next start ends whatever the old one left running.

Asking again on a task continues the agent's earlier conversation on it.

### Where runs live

Each task's runs share a Git worktree on the branch `meat-mode/<bead id>`, in the bridge's state folder (`~/.local/state/meat-mode/agents/` on Linux, `~/Library/Application Support/meat-mode/agents/` on macOS), not in your repo:

- `worktrees/<repo>/<bead id>/`: the worktree. The agent is told never to push, so merge or push its commits yourself.
- `logs/<repo>/<bead id>/<run id>.jsonl`: the raw stream of each run, with what the agent wrote on its error output next to it in `<run id>.stderr`.
- `runs.json`: each task's session id, run count and latest run. To look at a finished conversation yourself, run `claude --resume <session id>` inside the worktree.

When a task leaves the synced set (7 days after it closed, or when it is deleted or loses its `human` label), the bridge removes its worktree if Git calls it clean, and its branch if it has no unmerged commits. A worktree with changes or untracked files, and a branch with unmerged commits, stay for you.

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

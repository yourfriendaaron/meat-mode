# Q2 findings: how comparable self-hosted apps restrict access, and how exposure failed

Scope: Q2 of brief.md. Read via Exa search (highlights only unless noted; no page was fetched in full, because the search highlights were long enough to quote). Grades: A controlled/large n, B single team or small n, C anecdote/opinion, V vendor guidance. Searched ~14 calls.

## 1. Default posture table (what each project ships)

| Project | Default bind | Login on by default | What it offers for "private only" | Source |
|---|---|---|---|---|
| Home Assistant | all interfaces (not stated in the pages read) | Yes, built-in provider | `trusted_networks` provider (skip login by source IP), `ip_ban_enabled`, `login_attempts_threshold`, `trusted_proxies` | HA docs (V) |
| Jellyfin | not stated in pages read | Yes (users) | "Local Networks" CIDR list + global "Remote Access" switch + per-user "Allow remote connections to this server" | Jellyfin docs (V) |
| Vaultwarden | container `ROCKET_ADDRESS=0.0.0.0:80`; wiki tells you to publish `127.0.0.1:8000` | Yes, but signup open to anyone by default | Reverse proxy, disable registration, admin page behind IP allowlist | Wiki (V) |
| Immich | port 2283 | Yes | Docs: "Never forward port 2283 directly"; VPN is option 1 | Immich docs (V) |
| code-server | `127.0.0.1:8080` | Yes, `auth: password`, random password | Rate limit 2 logins/min + 12/hour | Coder FAQ (V) |
| Jupyter Server | `127.0.0.1:8888` | Yes, random token per start | Token or password; empty token/password "NOT RECOMMENDED" | Jupyter docs (V) |
| Ollama | `127.0.0.1` | No auth feature at all | None built in | MicroSec 2025-07 (B) |
| ComfyUI | localhost; `--listen 0.0.0.0` opts into exposure | No | None built in; advisory table rates `--listen 0.0.0.0` HIGH, localhost "NOT AFFECTED" | GHSA-95pq-hr8p-f5g7 (V) |
| OpenHands agent-server | was `0.0.0.0` for every launch, no key | No (key optional) | Fixed in 1.37.1: bind `127.0.0.1` when no key set, warn on wildcard bind without key | Aikido 2026-08-05 (B) |
| OpenClaw gateway | docs today: `loopback`; auth required by default | Yes, token generated even on loopback | `bind` modes loopback/lan/tailnet/custom; non-loopback bind requires auth; Tailscale Serve identity; `trusted-proxy` mode | docs.openclaw.ai (V) |
| MCP servers (spec) | n/a | Spec "did not require" auth early on | n/a | Knostic (B) |

## 2. Home Assistant: the closest analogue to "trust the network"

Source: https://www.home-assistant.io/docs/authentication/providers/ (undated page, highlights only). Grade V.
- Quote: "Using only `trusted_networks` without `type: homeassistant` as a fallback means anyone on your trusted network can log in by selecting a username. No password is required."
- Quote: "The multi-factor authentication module will not participate in the login process if you are using this authentication provider."
- Quote: "You cannot trust a network that you are using in any trusted_proxies." Code (mintlify mirror of core `trusted_networks.py`, grade C): the provider rejects a source that is in `trusted_proxies`, rejects Home Assistant Cloud connections even from a trusted IP, and re-validates the remote IP on every refresh-token use.
- `allow_bypass_login` default false; with one user and bypass on, the login screen is not shown.
- IP ban (https://www.home-assistant.io/integrations/http, V): "IP bans do not expire automatically." Threshold range 1 to 1000. Unlimited attempts is a separate switch.
- Failure mode (https://github.com/home-assistant/core/issues/37029, 2020 to 2022, C): `ip_ban_enabled` banned a trusted wall tablet behind `trusted_networks` plus `trusted_proxies`; fix PR #52334 (`ip_ban_ignore_ips`) was denied; users disabled bans. One maintainer comment: trusted_networks "does not meant you can access home-assistant without valid access token, it just by-pass the user/password input phase".
- **Inference**: HA treats source-IP trust as a login shortcut, not a perimeter. It documents the two traps for us: a proxy on a trusted address turns every internet client into a trusted one, and IP bans interact badly with trusted devices.

## 3. Jellyfin: "local network" classification and where it breaks

- Docs (https://jellyfin.org/docs/general/post-install/networking/, V, highlights only): "External access can either be completely disabled or selectively enabled for individual users. For these controls to function correctly, Jellyfin must know which IP ranges should be considered part of the local network." And "External access settings through a reverse proxy will only work if known proxies are set up correctly!"
- Issue #13966 (https://github.com/jellyfin/jellyfin/issues/13966, 2025-04-22, C): user with "Allow remote connections" unchecked could still log in from WAN through a reverse proxy. Maintainer: "if you are not [setting Known proxies], all access from that reverse proxy will be treated as the IP of that reverse proxy, and if that proxy is in your LAN, all users from that proxy will be treated as in the LAN."
- Issue #13054 (https://github.com/jellyfin/jellyfin/issues/13054, 2024-11-17, closed 2024-12-10, B): `X-Forwarded-For: ::ffff:192.0.2.0` was treated as remote while `192.0.2.0` was local; root cause was address-form string comparison, fixed in a month.
- **Inference**: both bugs are the Q1 failure modes (proxy collapses client IP to a LAN address; IPv4-mapped IPv6 forms) seen in a shipped product. Normalise `::ffff:a.b.c.d` before classifying.

## 4. Numbers on exposure (Shodan, Censys, FOFA)

| Item | Count | Date | Source | Grade |
|---|---|---|---|---|
| Ollama, Shodan product filter | ~270,988 (Censys/ZoomEye ~200K) | 2025-07-10 | https://fuzzinglabs.com/ollama-vulnerable-instances/ | B |
| Ollama, high-confidence open instances | 10.6K across 1,229 ASNs; 1.5K answered a prompt; >25% not on 11434 | 2025-09-24 | https://censys.com/blog/ollama-drama-investigating-the-prevalence-of-ollama-open-instances-with-censys/ | B |
| Ollama, Cisco Talos via Shodan | 1,139 unauthenticated; 214 (18.8%) serving live models | 2025-09-01 | https://blogs.cisco.com/security/detecting-exposed-llm-servers-shodan-case-study-on-ollama | B |
| 15 LLM frameworks, FOFA | 320,102 public services; >40% plain HTTP; Ollama answered >35% of unauthenticated API requests | 2025-08-26 (v2) | https://arxiv.org/pdf/2505.02502 | A- (peer-style, large n, one search engine) |
| Ollama/Open WebUI/Flowise/Langflow, CSA note citing a ~1M-service scan | 1,652 of >5,200 Ollama answered unauthenticated (31%); >12,000 Open WebUI, 2,650 Flowise, 300 Langflow exposed | 2026 (undated) | https://labs.cloudsecurityalliance.org/research/csa-research-note-exposed-ai-infrastructure-selfhosted-secur/ | C (secondary, cites [1]) |
| Same note: GreyNoise honeypots | 91,403 attack sessions against exposed LLM endpoints, Oct 2025 to Jan 2026 | 2026 | same | C |
| MCP servers exposed, Knostic via Shodan | 1,862 found; 119 sampled, all 119 listed tools with no auth | 2025-07-17 | https://www.knostic.ai/blog/mapping-mcp-servers-study | B |
| MCP servers, Trend Micro | 492 with no client auth or TLS (2025-07-16); 1,467 (2026-04-28, "nearly tripled") | as left | https://www.trendaisecurity.com/en/resources-insights/deep-research/update-on-exposed-mcp-servers-the-threat-widens-to-the-cloud | B |
| MCP "NeighborJack", Backslash | "hundreds" of >7,000 servers bound to 0.0.0.0 | 2025-06-25 | https://www.globenewswire.com/news-release/2025/06/25/3105144/0/en/Backslash-Security-Exposes-Critical-Flaws-in-Hundreds-of-Public-MCP-Servers.html | B/V (vendor) |
| Langflow | 466 (Censys), later 1,156 with 360 on a vulnerable version; 1,050 on Shodan; Greynoise 361 attacker IPs | 2025-05 | https://thehackernews.com/2025/05/critical-langflow-flaw-added-to-cisa.html ; https://www.recordedfuture.com/blog/langflow-cve-2025-3248 | B |
| ComfyUI | >1,000 exposed; one scan cycle of ~6,400 cloud IPs found 624 live, 214 vulnerable, 97 exploited | 2026-04-06 | https://censys.com/blog/comfyui-servers-cryptomining-proxy-botnet/ | B |
| OpenClaw/Clawdbot/Moltbot, Censys | ~1,000 to >21,000 in under a week; 21,639 exposed on 2026-01-31; most "still require a token" | 2026-01-31 | https://censys.com/blog/openclaw-in-the-wild-mapping-the-public-exposure-of-a-viral-ai-assistant/ | B |
| OpenClaw, SecurityScorecard | 42.9K unique IPs, 82 countries; 15.2K flagged RCE-vulnerable; counts grew 24,034 to 40,214 during the 24h of research | 2026-02-09 | https://securityscorecard.com/blog/beyond-the-hype-moltbots-real-risk-is-exposed-infrastructure-not-ai-superintelligence/ | B (vendor) |
| OpenClaw, Shodan via SOCRadar | 1,009 Clawdbot gateways, later 17,903 OpenClaw | 2026-01-26 | https://socradar.io/blog/clawdbot-why-is-it-everywhere/ | B |
| OpenClaw, Haun Lab | 8,600+ on port 18789; 0 of 1,200 sampled showed WWW-Authenticate or TLS | 2026-03-20 | https://haunlab.com/blog/openclaw-exposure-report-2026 | C (blog, method weak: no WWW-Authenticate does not mean no app-level token) |

Read as: all "highlights only". Counts differ by an order of magnitude between teams because fingerprints, ports and dedup differ. **Inference**: use "thousands to tens of thousands within days of going viral" as the defensible range; do not quote any single OpenClaw figure as exact.

## 5. Incidents and CVEs, by failure class

**A. Unauthenticated by design, reachable because bound wide**
- Ollama: no auth feature; exposure is the whole story. Also CVE-2024-28224 DNS rebinding (https://www.nccgroup.com/research/technical-advisory-ollama-dns-rebinding-attack-cve-2024-28224/, 2024-04-08, V/B): attacker site reaches an API bound to localhost "even if the vulnerable system is not configured to expose its API publicly"; rebinding "in as little as 3 seconds"; fixed in v0.1.29. NCC fix advice: validate the `Host` header against an allowlist (`localhost`, `127.0.0.1` and so on). Relevant to Q1/Q4: Host check is the right defence for a private-network hub too.
- ComfyUI: unauthenticated node API runs Python. Campaign above, plus CVE-2025-67303 in ComfyUI-Manager (NIST 2026-01-05, CVSS 7.5): config dir was readable and writable through the web API. Patch moved config to a "protected" path and raised weak security levels to normal.
- Langflow CVE-2025-3248 (CVSS 9.8), missing auth on `/api/v1/validate/code` calling `exec()`. Fixed 1.3.0 (2025-03-31/04-01); PoC public 2025-04-09; CISA KEV 2025-05-05 (https://www.cisa.gov/news-events/alerts/2025/05/05/cisa-adds-one-known-exploited-vulnerability-catalog, A for the date and listing). Mitigation in vendor text: firewall plus authenticated access, no direct internet.
- OpenHands agent-server: default bind `0.0.0.0` with no key in 1.0.0 to 1.37.0; fixed in 1.37.1 (2026-08-05, Aikido, B). Docs now say "Do not expose an unauthenticated Agent Server on a public network. It can execute commands and read or write files in its configured workspace." Related: `openhands serve` bound 0.0.0.0 (issue #706, 2026-05, fix PRs #716/#722 to localhost, C). Docs also warn that localStorage holds the session key for every backend registered in that browser (same-origin editor can read it).

**B. Auth present but trust decision wrong (closest to our design)**
- OpenClaw "localhost trust" behind a reverse proxy: the gateway treated loopback-looking proxied requests as local and auto-trusted them. Sable report claims 1,673 Shodan hits, >1,000 with no auth, "a single line of JavaScript turned Moltbot's 'localhost trust' model into a widespread security weakness" (https://sable.somoswilab.com/research/moltbot, listed date 2025-12-01 is implausible given the January 2026 events, grade C). Project's response, in current docs (V): `gateway.trustedProxies`; when proxy headers come from an address not listed "it will not treat the connection as local; if gateway auth is disabled, that connection is rejected"; same-host loopback proxy rejected with `proxy_attribution_required` unless configured; `trusted-proxy` auth mode rejects loopback-source proxies by default; `X-Real-IP` ignored unless opted in; docs show `$proxy_add_x_forwarded_for` as "bad" and overwrite with `$remote_addr` as good (https://docs.openclaw.ai/gateway/security/network-exposure).
- OpenClaw token now required even on loopback: "OpenClaw enforces gateway auth by default, including loopback ... This blocks other local processes from calling the Gateway." (https://docs.openclaw.ai/help/faq/config-basics). `auth.mode: none` exists but "intentionally not offered by onboarding prompts". Non-loopback bind requires auth. Failed-auth rate limit per client IP; browser-origin WebSocket attempts are throttled even on loopback ("defense-in-depth against browser-based localhost brute force").
- Jellyfin and Home Assistant proxy collapse: section 2 and 3.

**C. Loopback bind did not save it (browser as the bridge)**
- OpenClaw CVE-2026-25253, CVSS 8.8, fixed 2026.1.29 (GHSA-g8p2-7wf7-98mq, 2026-02-02; https://osv.dev/vulnerability/GHSA-g8p2-7wf7-98mq, A for the record). UI took `gatewayUrl` from the query string, auto-connected and sent the stored token. Advisory: "exploitable even on instances configured to listen on loopback only, since the victim's browser initiates the outbound connection." depthfirst (https://depthfirst.com/research/1-click-rce-to-steal-your-moltbot-data-and-keys, 2026-02-01, B) chains it with missing WebSocket `Origin` validation (cross-site WebSocket hijacking to `ws://localhost:18789`), then used API calls to disable confirmations and sandbox and ran `system.run`. Patch: confirmation modal for a new gateway URL. Belgian CCB: no evidence of exploitation in the wild at 2026-02-02.
- Caution: hunt.io (https://hunt.io/blog/cve-2026-25253-openclaw-ai-agent-exposure) describes the same CVE as unauthenticated `/api/export-auth` and counts "17,500 exposed instances vulnerable". That contradicts the advisory, CVE record and Snyk. **Inference**: treat hunt.io as unreliable on mechanism; its count is a count of exposed control panels, not of vulnerable ones.
- **Inference for us**: for a hub that wakes coding agents, the lesson is that a private bind is not a boundary against the user's own browser. Origin checks on WebSocket upgrades, Host allowlists, and a confirmation step on dangerous actions each fail independently of the network guard.

**D. Open signup / first-run takeover**
- Vaultwarden: "By default, vaultwarden allows any anonymous user to register new accounts on the server without first being invited" (hardening guide, V). Immich: `IMMICH_ALLOW_SETUP=true` is default; an un-onboarded new instance is explicitly out of scope for their security policy (https://github.com/immich-app/immich/security). **Inference**: first-run setup on a reachable hub is a takeover window; require a setup secret printed to the console or restrict setup to loopback.

**E. Vendor stance on scope**
- Immich security policy (V): rate limiting and MFA are "delegated to an OAuth server"; "Surrounding infrastructure ... reverse proxy configuration: These are the admin's responsibility"; machine-learning service "should not be made accessible outside of the container network". Docs: VPN is "simple to set up and very secure ... Even if there is a zero-day vulnerability on Immich, you will not be at risk"; reverse proxy: "the existence of severe security vulnerabilities cannot be ruled out".
- Open WebUI CVE-2025-64496 (CVSS 7.3, fixed 0.6.35, 2025-11-07): untrusted model server SSE `execute` event ran `new Function()`; JWT in `localStorage`, `expires_at: null`, no HttpOnly, no CSP; with `workspace.tools` permission leads to backend RCE (https://github.com/advisories/GHSA-cm35-v4vp-5xvx, Cato write-up 2026-01-05). Relevant to Q4 (cookie flags, CSP) and Q3 (long-lived tokens).

## 6. What projects changed after incidents (pattern list)

1. Flip the default bind to loopback: OpenHands agent-server (1.37.1), OpenHands CLI serve (open issue with fix PRs), Coder's Jupyter modules (coder/registry PR #938, defaults `host` to 127.0.0.1, `0.0.0.0` "explicit, less secure override").
2. Make auth non-optional off loopback: OpenClaw (non-loopback bind requires token, password or trusted proxy; token even on loopback).
3. Stop trusting a proxied address as local: OpenClaw `trustedProxies` and `proxy_attribution_required`; Home Assistant refuses `trusted_networks` from proxy addresses; Jellyfin "Known proxies".
4. Warn at startup when wide-bind and no auth meet: OpenHands fix adds a warning for wildcard bind without key.
5. Confirmation on dangerous actions: OpenClaw gateway-URL modal; `system.run` and sandbox settings were remotely toggleable, which is why one token meant RCE.
6. Protect config paths from the web API: ComfyUI-Manager "System User Protection API".
7. Ship a self-audit command: `openclaw security audit --deep` (V).
8. Move perimeter to identity-aware layer: Immich and OpenClaw docs steer to VPN, SSH tunnel, Tailscale Serve or Cloudflare Access instead of raw exposure. OpenClaw: `tailscale.mode: serve` is "tailnet only, loopback bind"; `funnel` is "public, requires auth"; Tailscale identity headers authenticate only the Control UI WebSocket, not the HTTP API.

## 7. Takeaways for the Meat Mode hub (all Inference)

- Peers that survived exposure better had two independent layers: a bind or network rule and a mandatory credential that the first-run flow generates. The ones that failed had one layer, usually "localhost is safe".
- Copy: refuse to start (or loudly warn) on a non-loopback, non-private bind without a password; classify client addresses after normalising IPv4-mapped IPv6; require explicit trusted-proxy configuration and never accept `X-Forwarded-For` otherwise; validate `Host` and WebSocket/POST `Origin`; make config and agent-wake settings unreachable through the same API a stolen cookie can call.
- Avoid: a "skip login on trusted network" mode as the default for an agent-waking surface. Home Assistant needs a documented fallback provider to avoid passwordless access, and its MFA does not run on that path.
- A tailnet or LAN deployment is not exposed in Shodan terms, but incident C shows the user's own browser can still deliver a request. The Host/Origin checks the hub already has cover this; WebSocket upgrades, if any, need the same.

## 8. Gaps

- Searched and not found: a Shodan or Censys count for Vaultwarden, Immich, Jellyfin or Home Assistant exposure in 2024 to 2026 (only Immich/Vaultwarden vendor warnings). Not searched: Open WebUI/ComfyUI default bind in their own docs; Jupyter exposed-instance counts.
- Home Assistant and Jellyfin default bind addresses not confirmed from primary pages.
- OpenClaw default bind history conflicts: Censys and current docs say loopback; SecurityScorecard and Haun say `0.0.0.0` out of the box. Unresolved; possibly changed between versions or a Docker/cloud-image effect. Not verified against the git history.
- No confirmed in-the-wild exploitation of CVE-2026-25253 found (CCB said none at 2026-02-02). Ollama CVE-2026-7482 ("Bleeding Llama") seen only in the CSA note, not verified.

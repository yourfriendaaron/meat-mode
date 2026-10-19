#!/usr/bin/env bash
# Proves scripts/public-safety.sh blocks each class of leak and passes clean code.
# Runs in throwaway Git repos; samples are assembled at runtime so this file
# itself stays clean.
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
guard="$here/scripts/public-safety.sh"
pass=0 fail=0

fresh() {
  local d
  d=$(mktemp -d)
  git -C "$d" init -q
  mkdir -p "$d/scripts"
  cp "$guard" "$d/scripts/public-safety.sh"
  echo "$d"
}

expect() { # expect block|clean, name, file, content, [local patterns]
  local want=$1 name=$2 file=$3 content=$4 d got
  d=$(fresh)
  mkdir -p "$d/$(dirname "$file")"
  printf '%s\n' "$content" > "$d/$file"
  if [ -n "${5:-}" ]; then printf '%s\n' "$5" > "$d/.public-safety.local"; fi
  git -C "$d" add -f "$file"
  if (cd "$d" && PUBLIC_SAFETY_LOCAL="$d/.public-safety.local" ./scripts/public-safety.sh >/dev/null 2>&1); then got=clean; else got=block; fi
  rm -rf "$d"
  if [ "$got" = "$want" ]; then pass=$((pass+1)); else fail=$((fail+1)); echo "FAIL: $name (wanted $want, got $got)"; fi
}

h="ho""me"; u="sam""ple"
expect block "home path"        src/a.ts   "const p = '/$h/$u/projects/x'"
expect block "macOS home path"  src/a.ts   "open('/Us""ers/$u/notes')"
expect block "root home path"   src/a.ts   "open('/roo""t/$u/notes')"
expect block "private IPv4"     src/a.ts   "fetch('http://10.$((1+1)).3.4:8123')"
expect block "192.168 IPv4"     src/a.ts   "host = '192.""168.1.20'"
expect block "Tailscale IPv4"   src/a.ts   "peer = '100.""101.2.3'"
expect block "tailnet hostname" README.md  "Open https://svc.tail""abc12.ts.""net"
expect block "mDNS hostname"    src/a.ts   "url = 'http://homeassistant.lo""cal:8123'"
expect block "mDNS punctuation" src/a.ts   "see nas.lo""cal) and nas.lo""cal, today"
expect block "ULA IPv6"         src/a.ts   "addr = 'fd""42:dead:beef::1'"
expect block "link-local IPv6"  src/a.ts   "addr = 'FE""80::1'"
expect block "MAC address"      src/a.ts   "mac = 'a4:""83:e7:12:34:56'"
expect block "MAC at line end"  docs/x.md  "Router: A4:""83:E7:12:34:56"
expect block "personal email"   docs/x.md  "Contact $u.person@gm""ail.com"
expect block "tracked .env"     .env       "TOKEN=abc"
expect block "tracked compose"  compose.yaml "services: {}"
expect block "tracked compose variant" compose.prod.yaml "services: {}"
expect block "tracked envrc"    .envrc     "export TOKEN=abc"
expect block "tracked database" data.db    "x"
expect block "tracked db side file" data.db-wal "x"
expect block "tracked ssh key"  id_rsa     "ssh-ed25519 AAAA example"
expect block "tracked Java keystore" android/release.jks "x"
expect block "tracked keystore file" release.keystore "x"
expect block "tracked PKCS12 keystore" signing/upload.p12 "x"
expect block "tracked private key" signing/key.pem "x"
expect block "tracked keystore.properties" android/keystore.properties "storePassword=x"
expect block "tracked local.properties" android/local.properties "sdk.dir=x"
expect clean "gradle.properties" android/gradle.properties "org.gradle.jvmargs=-Xmx2g"
expect block "private pattern"  src/a.ts   "const owner = 'Zorblax'" "zorblax"
expect block "beads export"     .beads/issues.jsonl '{"id":"x-1","title":"Water the plants"}'
expect block "beads database"   .beads/embeddeddolt/meat/x "x"
expect block "Dolt remote in beads config" .beads/config.yaml "$(printf 'sync:\n    remote: "file:///srv/beads.git"')"
expect clean "beads config"     .beads/config.yaml "$(printf '# sync:\n#   remote: git+https://example.com/beads.git\nissue-prefix: x')"
expect block "allowed email hides nothing" docs/x.md "Write to alex@example.com and $u.person@gm""ail.com"
expect clean "cert fingerprint" README.md "| SHA-256 | \`D9:96:""CE:79:AB:03:BC:39:67:5C:6F:69:09:ED:88:87:7F:2C:CA:12:B5:79:36:09:28:75:D0:68:FF:65:39:90\` |"
expect clean "allowed emails only"         docs/x.md "alex@example.com or sam@example.org"
expect block "mise.local hides nothing"    src/a.ts  "see mise.local and nas.lo""cal:8123"
expect clean "mise.local alone"            src/a.ts  "read mise.local for settings"
expect clean "local config file names"    docs/x.md "copy .env.local, mise.local.toml and .public-safety.local"
expect block "broken private pattern"      src/a.ts  "const x = 1" "foo("
expect block "tracked dolt dir"            .dolt/config.json "x"
expect block "tracked beads credential key" .beads-credential-key "x"
expect clean "example email"    docs/x.md  "Write to alex@example.com"
expect clean "noreply email"    docs/x.md  "Co-authored-by: bot@users.noreply.github.com"
expect clean "doc IP range"     docs/x.md  "Server at 192.0.2.10 and 2001:db8::1"
expect clean "hex inside a word" docs/x.md "hash 2001:db8::fd""12: is documentation range"
expect clean "env example"      .env.example "HA_URL=http://ha.example.com:8123"
expect clean "compose example" compose.example.yaml "services: {}"
expect clean "gitignore names"  .gitignore "mise.local.toml"
expect clean "plain code"       src/a.ts   "export const add = (a: number, b: number) => a + b"

# The pre-push hook: refs outside branches and tags, Dolt's branches, and
# commits that track beads data or a Dolt remote.
hook="$here/.githooks/pre-push"
push() { # push block|clean, name, remote ref, [file, content]
  local want=$1 name=$2 ref=$3 d sha got
  d=$(mktemp -d)
  git -C "$d" init -q
  if [ -n "${4:-}" ]; then mkdir -p "$d/$(dirname "$4")"; printf '%s\n' "$5" > "$d/$4"; git -C "$d" add -f "$4"; fi
  git -C "$d" -c user.name=Sam -c user.email=sam@example.com commit -q --allow-empty --no-verify -m x
  sha=$(git -C "$d" rev-parse HEAD)
  if (cd "$d" && printf 'refs/heads/main %s %s %s\n' "$sha" "$ref" 0000000000000000000000000000000000000000 | "$hook" origin https://example.com/x.git >/dev/null 2>&1); then got=clean; else got=block; fi
  rm -rf "$d"
  if [ "$got" = "$want" ]; then pass=$((pass+1)); else fail=$((fail+1)); echo "FAIL: push $name (wanted $want, got $got)"; fi
}
push block "Dolt data ref"     refs/dolt/data
push block "Dolt branch"       refs/heads/__dolt_remote_info__
push block "beads export"      refs/heads/main .beads/issues.jsonl '{"id":"x-1"}'
push block "Dolt remote"       refs/heads/main .beads/config.yaml "$(printf 'sync:\n    remote: "file:///srv/beads.git"')"
push clean "branch"            refs/heads/main src/a.ts "export const x = 1"
push clean "tag"               refs/tags/v1.0.0

# Two commits where the first adds a file the second removes: the tip is
# clean, so only checking the commits being pushed, not just the tip,
# catches the first. "first" names the first commit as the remote sha, so
# only the second, clean commit is new.
push2() { # push2 block|clean, name, remote sha (none|first), file, content
  local want=$1 name=$2 pushed=$3 file=$4 content=$5 d sha remote got
  d=$(mktemp -d)
  git -C "$d" init -q
  mkdir -p "$d/$(dirname "$file")"
  printf '%s\n' "$content" > "$d/$file"
  git -C "$d" add -f "$file"
  git -C "$d" -c user.name=Sam -c user.email=sam@example.com commit -q --no-verify -m x
  if [ "$pushed" = first ]; then remote=$(git -C "$d" rev-parse HEAD); else remote=0000000000000000000000000000000000000000; fi
  git -C "$d" rm -q "$file"
  git -C "$d" -c user.name=Sam -c user.email=sam@example.com commit -q --no-verify -m x
  sha=$(git -C "$d" rev-parse HEAD)
  if (cd "$d" && printf 'refs/heads/main %s %s %s\n' "$sha" refs/heads/main "$remote" | "$hook" origin https://example.com/x.git >/dev/null 2>&1); then got=clean; else got=block; fi
  rm -rf "$d"
  if [ "$got" = "$want" ]; then pass=$((pass+1)); else fail=$((fail+1)); echo "FAIL: push $name (wanted $want, got $got)"; fi
}
push2 block "beads export in history"  none .beads/issues.jsonl '{"id":"x-1"}'
push2 block "Dolt remote in history"   none .beads/config.yaml "$(printf 'sync:\n    remote: "file:///srv/beads.git"')"
push2 clean "removal on top of the pushed" first .beads/issues.jsonl '{"id":"x-1"}'

echo "public-safety tests: $pass passed, $fail failed"
[ "$fail" -eq 0 ]

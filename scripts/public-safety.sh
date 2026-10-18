#!/usr/bin/env bash
# Refuse to publish personal data, secrets or machine state.
# Scans the Git index (what the next commit will contain; in CI, the checkout).
# The beads issues are private and never published (see AGENTS.md), so they
# are not scanned; the index is checked for anything that would publish them.
# Exit 0 when clean, 1 with a report when something is found.
#
# Extra private patterns: .public-safety.local (untracked), one extended regex
# per line; lines starting with # are comments. PUBLIC_SAFETY_LOCAL overrides
# the path.
set -euo pipefail

root=$(git rev-parse --show-toplevel)
cd "$root"

# This script and its test hold the patterns themselves.
exclude=(':!scripts/public-safety.sh' ':!tests/public-safety.test.sh')
found=0

report() { # label, grep output
  [ -n "$2" ] || return 0
  found=1
  printf '\n%s\n%s\n' "$1" "$2"
}

search() { # [-i] regex: matches in the index
  local flags=(-n -E)
  if [ "$1" = "-i" ]; then flags+=(-i); shift; fi
  git grep --cached -I "${flags[@]}" -e "$1" -- . "${exclude[@]}" || true
}

scan() { # label, extended regex, [filter regex to drop allowed matches]
  local out
  out=$(search "$2")
  if [ -n "${3:-}" ] && [ -n "$out" ]; then
    out=$(printf '%s\n' "$out" | grep -v -E -e "$3" || true)
  fi
  report "$1" "$out"
}

scan "Absolute home paths:" '(/home/|/var/home/|/Users/|/root/|[Cc]:\\Users\\)[A-Za-z0-9._-]+'
scan "Private, Tailscale or link-local IPv4 addresses:" \
  '(^|[^0-9.])(10\.[0-9]{1,3}|192\.168|172\.(1[6-9]|2[0-9]|3[01])|100\.(6[4-9]|[7-9][0-9]|1[01][0-9]|12[0-7])|169\.254)\.[0-9]{1,3}\.[0-9]{1,3}([^0-9]|$)'
scan "Tailnet hostnames:" '[A-Za-z0-9-]+\.ts\.net'
# Unique-local (fd00::/8, which includes Tailscale's fd7a:) and link-local
# (fe80:) IPv6 addresses. The anchor keeps the two hex digits from matching
# inside a longer hex word, as in 2001:db8::fd12:.
scan "Private IPv6 addresses (unique-local fd00::/8 and link-local fe80::):" \
  '(^|[^0-9a-fA-F:])([fF][dD][0-9a-fA-F]{2}|[fF][eE]80):'
scan "mDNS .local hostnames:" '[A-Za-z0-9-]+\.local([^A-Za-z0-9._-]|$)' '\.env\.local|mise\.local|\.public-safety\.local|\*\.local'
scan "MAC addresses:" '([0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}'
scan "Email addresses outside example and noreply domains:" \
  '[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}' \
  '@(example\.(com|org|net)|users\.noreply\.github\.com|noreply\.github\.com|anthropic\.com)'

# Files that must never be tracked, whatever they contain.
bad_files=$(git ls-files --cached | grep -E '(^|/)(\.env(\..+)?|mise\.local\.toml|\.public-safety\.local|id_rsa|id_ecdsa|id_ed25519|\.netrc|\.npmrc|\.htpasswd|credentials\.json)$|\.(db|sqlite3?|pem|key|p12|pfx|jks|keystore)$|\.(db|sqlite3?)-(wal|shm|journal)$' | grep -v -E '(^|/)\.env\.example$' || true)
report "Files that must never be tracked:" "$bad_files"

# The beads issues stay private: only bd's four setup files are tracked, and
# the config never names a Dolt remote, which bd writes there and commits on
# its own when one is added.
beads_files=$(git ls-files --cached -- .beads | grep -v -x -E '\.beads/(\.gitignore|README\.md|config\.yaml|metadata\.json)' || true)
report "Beads data must never be tracked:" "$beads_files"
if git cat-file -e :.beads/config.yaml 2>/dev/null; then
  remote=$(git show :.beads/config.yaml | grep -n -E '^[[:space:]]*(sync\.)?remote:' | sed 's/^/.beads\/config.yaml:/' || true)
  report "A Dolt remote in .beads/config.yaml (keep it in the Dolt database only):" "$remote"
fi

# The operator's own names, hosts and identifiers.
local_file=${PUBLIC_SAFETY_LOCAL:-$root/.public-safety.local}
if [ -f "$local_file" ]; then
  local_name=$(basename "$local_file")
  while IFS= read -r pat || [ -n "$pat" ]; do
    case "$pat" in ''|'#'*) continue ;; esac
    out=$(search -i "$pat")
    report "Private pattern from $local_name matched:" "$out"
  done < "$local_file"
fi

if [ "$found" -ne 0 ]; then
  printf '\npublic-safety: blocked. This repo is public; remove the data above (see AGENTS.md).\n' >&2
  exit 1
fi
echo "public-safety: clean"

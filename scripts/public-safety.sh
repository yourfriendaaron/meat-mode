#!/usr/bin/env bash
# Refuse to publish personal data, secrets or machine state.
# Scans the Git index (what the next commit will contain; in CI, the checkout)
# and, when bd is installed, an export of this repo's beads issues.
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

# Beads issues are published with the repo too: scan an export of them.
beads_export=""
if command -v bd >/dev/null 2>&1 && [ -d .beads ]; then
  beads_export=$(mktemp)
  trap 'rm -f "$beads_export"' EXIT
  bd export > "$beads_export" 2>/dev/null || : > "$beads_export"
fi

search() { # [-i] regex: matches in the index, then in the beads export
  local flags=(-n -E)
  if [ "$1" = "-i" ]; then flags+=(-i); shift; fi
  git grep --cached -I "${flags[@]}" -e "$1" -- . "${exclude[@]}" || true
  if [ -n "$beads_export" ] && [ -s "$beads_export" ]; then
    grep "${flags[@]}" -e "$1" "$beads_export" | sed 's/^/beads issues:/' || true
  fi
}

scan() { # label, extended regex, [filter regex to drop allowed matches]
  local out
  out=$(search "$2")
  if [ -n "${3:-}" ] && [ -n "$out" ]; then
    out=$(printf '%s\n' "$out" | grep -v -E -e "$3" || true)
  fi
  report "$1" "$out"
}

scan "Absolute home paths:" '(/home/|/var/home/|/Users/|[Cc]:\\Users\\)[A-Za-z0-9._-]+'
scan "Private, Tailscale or link-local IPv4 addresses:" \
  '(^|[^0-9.])(10\.[0-9]{1,3}|192\.168|172\.(1[6-9]|2[0-9]|3[01])|100\.(6[4-9]|[7-9][0-9]|1[01][0-9]|12[0-7])|169\.254)\.[0-9]{1,3}\.[0-9]{1,3}([^0-9]|$)'
scan "Tailnet hostnames:" '[A-Za-z0-9-]+\.ts\.net'
scan "Tailscale IPv6 addresses:" 'fd7a:115c:a1e0:'
scan "mDNS .local hostnames:" '[A-Za-z0-9-]+\.local([:/"'"'"' ]|$)' '\.env\.local|mise\.local|\.public-safety\.local|\*\.local'
scan "MAC addresses:" '([0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}'
scan "Email addresses outside example and noreply domains:" \
  '[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}' \
  '@(example\.(com|org|net)|users\.noreply\.github\.com|noreply\.github\.com|anthropic\.com)'

# Files that must never be tracked, whatever they contain.
bad_files=$(git ls-files --cached | grep -E '(^|/)(\.env(\..+)?|mise\.local\.toml|\.public-safety\.local)$|\.(db|sqlite3?|pem|key|p12|pfx)$' | grep -v -E '(^|/)\.env\.example$' || true)
report "Files that must never be tracked:" "$bad_files"

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

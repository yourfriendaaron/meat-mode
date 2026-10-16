#!/usr/bin/env bash
# One-time setup per clone: use the repo's Git hooks.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
git config core.hooksPath .githooks
chmod +x .githooks/* scripts/*.sh tests/*.sh
[ -f .public-safety.local ] || cat > .public-safety.local <<'EOF'
# One extended regex per line, matched case-insensitively against staged files.
# Put your own names, hostnames, usernames and network names here. This file
# is never tracked.
EOF
echo "setup: hooks enabled; private patterns go in .public-safety.local"

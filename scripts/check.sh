#!/usr/bin/env bash
# The full check CI runs, on this machine: run it once before a merge to main
# or a deploy. Needs bd on PATH for the end-to-end suite.
# E2E_PORT sets the web suite's port, so parallel worktrees don't collide.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

step() { printf '\n== %s\n' "$*"; }

step "go"
go vet ./...
go test ./...
for t in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
  GOOS=${t%/*} GOARCH=${t#*/} CGO_ENABLED=0 go build -o /dev/null ./cmd/meat-mode
done

step "web"
pnpm --dir web typecheck
pnpm --dir web build
pnpm --dir web test
pnpm --dir web test:e2e

step "end to end"
pnpm --dir e2e typecheck
E2E_REQUIRE_BD=1 pnpm --dir e2e test

step "public safety"
scripts/public-safety.sh

echo
echo "check: all passed"

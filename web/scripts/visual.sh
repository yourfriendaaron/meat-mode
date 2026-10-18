#!/usr/bin/env bash
# Runs the screenshot spec (test/visual) inside the official Playwright image
# whose version matches @playwright/test, so the fonts and the browser are the
# ones CI renders with. Extra arguments go to `playwright test`, such as
# --update-snapshots to write new baselines.
set -euo pipefail
cd "$(dirname "$0")/.."

version=$(node -p 'require("./package.json").devDependencies["@playwright/test"]')
image="mcr.microsoft.com/playwright:v${version}-noble"

node scripts/build.mjs
docker run --rm --ipc=host \
  -v "$PWD:/web" -w /web \
  -e CI \
  "$image" \
  node node_modules/@playwright/test/cli.js test -c playwright.visual.config.ts "$@"

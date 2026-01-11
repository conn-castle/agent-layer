#!/usr/bin/env bash
set -euo pipefail

# ./al - repo-local launcher
#
# Edit this file to choose a single default behavior.
# Uncomment exactly one option below (leave the rest commented).

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENTRYPOINT_SH="$SCRIPT_DIR/.agent-layer/src/lib/entrypoint.sh"
if [[ ! -f "$ENTRYPOINT_SH" ]]; then
  ENTRYPOINT_SH="$SCRIPT_DIR/src/lib/entrypoint.sh"
fi
if [[ ! -f "$ENTRYPOINT_SH" ]]; then
  ENTRYPOINT_SH="$SCRIPT_DIR/../src/lib/entrypoint.sh"
fi
if [[ ! -f "$ENTRYPOINT_SH" ]]; then
  echo "ERROR: Missing src/lib/entrypoint.sh (expected near .agent-layer/)." >&2
  exit 2
fi
# shellcheck disable=SC1090
source "$ENTRYPOINT_SH"
resolve_entrypoint_root || exit $?

ROOT="$WORKING_ROOT"
cd "$ROOT"

# Warn when .agent-layer is pinned to an older tag (local tags only).
warn_if_outdated_tag() {
  if ! command -v git > /dev/null 2>&1; then
    return 0
  fi
  if ! git -C "$ROOT/.agent-layer" rev-parse --is-inside-work-tree > /dev/null 2>&1; then
    return 0
  fi

  local current_tag latest_tag
  current_tag="$(git -C "$ROOT/.agent-layer" describe --tags --exact-match 2> /dev/null || true)"
  if [[ -z "$current_tag" ]]; then
    return 0
  fi

  latest_tag="$(git -C "$ROOT/.agent-layer" tag --list --sort=-v:refname | head -n 1)"
  if [[ -z "$latest_tag" || "$current_tag" == "$latest_tag" ]]; then
    return 0
  fi

  echo "warning: .agent-layer is on tag $current_tag; latest local tag is $latest_tag." >&2
  echo "warning: Run '.agent-layer/agent-layer-install.sh --upgrade' from the working repo root to upgrade." >&2
}

warn_if_outdated_tag

SYNC_ARGS=""
if [[ "${1:-}" == "codex" || "$(basename "${1:-}")" == "codex" ]]; then
  SYNC_ARGS="--codex"
  export AGENTLAYER_RUN_CODEX=1
fi

command -v node > /dev/null 2>&1 || {
  echo "ERROR: Node.js is required (node not found). Install Node, then re-run." >&2
  exit 2
}

# Option A (default): sync every run, load only .agent-layer/.env, then exec.
if [[ -n "$SYNC_ARGS" ]]; then
  node .agent-layer/src/sync/sync.mjs "$SYNC_ARGS"
else
  node .agent-layer/src/sync/sync.mjs
fi

exec "$ROOT/.agent-layer/with-env.sh" "$@"

# Option B: env-only (no sync).
# exec "$ROOT/.agent-layer/with-env.sh" "$@"

# Option C: sync-only (no env).
# exec node .agent-layer/src/sync/sync.mjs "$@"

# Option D: sync check + regen if stale, then env-only.
# node .agent-layer/src/sync/sync.mjs --check || node .agent-layer/src/sync/sync.mjs
# exec "$ROOT/.agent-layer/with-env.sh" "$@"

# Option E: sync every run, load .agent-layer/.env + .env, then exec.
# node .agent-layer/src/sync/sync.mjs
# exec "$ROOT/.agent-layer/with-env.sh" --project-env "$@"

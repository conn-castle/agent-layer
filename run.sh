#!/usr/bin/env bash
set -euo pipefail

# .agent-layer/run.sh
# Internal runner for ./al (root resolution + sync/env execution).

# Resolve the repo root using the shared entrypoint helper.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENTRYPOINT_SH="$SCRIPT_DIR/src/lib/entrypoint.sh"
if [[ ! -f "$ENTRYPOINT_SH" ]]; then
  echo "ERROR: Missing src/lib/entrypoint.sh (expected near .agent-layer/)." >&2
  exit 2
fi
# shellcheck disable=SC1090
source "$ENTRYPOINT_SH"
resolve_entrypoint_root || exit $?

# Work from the repo root so relative paths are stable.
ROOT="$WORKING_ROOT"
cd "$ROOT"

# Parse internal mode flags (used by the commented options in ./al).
mode="sync-env"
project_env="0"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --env-only)
      mode="env-only"
      shift
      ;;
    --sync-only)
      mode="sync-only"
      shift
      ;;
    --check-env)
      mode="check-env"
      shift
      ;;
    --project-env)
      project_env="1"
      shift
      ;;
    --)
      shift
      break
      ;;
    *)
      break
      ;;
  esac
done

# Build the sync command; when launching Codex, pass --codex for enforcement.
SYNC_CMD=(node "$AGENTLAYER_ROOT/src/sync/sync.mjs")
if [[ "${1:-}" == "codex" || "$(basename "${1:-}")" == "codex" ]]; then
  SYNC_CMD+=(--codex)
  export AGENTLAYER_RUN_CODEX=1
fi

# Decide which stages to run for the selected mode.
need_sync="0"
need_env="0"
case "$mode" in
  env-only)
    need_env="1"
    ;;
  sync-only)
    need_sync="1"
    ;;
  check-env)
    need_sync="1"
    need_env="1"
    ;;
  sync-env | *)
    need_sync="1"
    need_env="1"
    ;;
esac

# Run sync if requested (and ensure Node is available).
if [[ "$need_sync" == "1" ]]; then
  command -v node > /dev/null 2>&1 || {
    echo "ERROR: Node.js is required (node not found). Install Node, then re-run." >&2
    exit 2
  }
  if [[ "$mode" == "check-env" ]]; then
    "${SYNC_CMD[@]}" --check || "${SYNC_CMD[@]}"
  else
    "${SYNC_CMD[@]}"
  fi
fi

# Run the CLI with agent-layer env (and optional project env).
if [[ "$need_env" == "1" ]]; then
  if [[ "$project_env" == "1" ]]; then
    exec "$AGENTLAYER_ROOT/with-env.sh" --project-env "$@"
  else
    exec "$AGENTLAYER_ROOT/with-env.sh" "$@"
  fi
fi

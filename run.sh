#!/usr/bin/env bash
set -euo pipefail

# .agent-layer/run.sh
# Internal runner for ./al (root resolution + sync/env execution).

# Resolve the repo root using the shared entrypoint helper.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
ENTRYPOINT_SH="$SCRIPT_DIR/src/lib/entrypoint.sh"
if [[ ! -f "$ENTRYPOINT_SH" ]]; then
  echo "ERROR: Missing src/lib/entrypoint.sh (expected near .agent-layer/)." >&2
  exit 2
fi
# shellcheck disable=SC1090
source "$ENTRYPOINT_SH"

# Parse root flags plus internal mode flags (used by the commented options in ./al).
parent_root=""
use_temp_parent_root="0"
mode="sync-env"
project_env="0"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --parent-root)
      shift
      if [[ $# -eq 0 || -z "${1:-}" ]]; then
        echo "ERROR: --parent-root requires a path." >&2
        exit 2
      fi
      parent_root="$1"
      shift
      ;;
    --parent-root=*)
      parent_root="${1#*=}"
      if [[ -z "$parent_root" ]]; then
        echo "ERROR: --parent-root requires a path." >&2
        exit 2
      fi
      shift
      ;;
    --temp-parent-root)
      use_temp_parent_root="1"
      shift
      ;;
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

ROOTS_PARENT_ROOT="$parent_root" ROOTS_USE_TEMP_PARENT_ROOT="$use_temp_parent_root" resolve_entrypoint_root || exit $?

if [[ "$TEMP_PARENT_ROOT_CREATED" == "1" ]]; then
  # shellcheck disable=SC2153
  trap '[[ "${PARENT_ROOT_KEEP_TEMP:-0}" == "1" ]] || rm -rf "$PARENT_ROOT"' EXIT INT TERM
fi

# Work from the repo root so relative paths are stable.
ROOT="$PARENT_ROOT"
cd "$ROOT"

# Build the sync command; when launching Codex, pass --codex for enforcement.
SYNC_CMD=(node "$AGENT_LAYER_ROOT/src/sync/sync.mjs")
if [[ "${1:-}" == "codex" || "$(basename "${1:-}")" == "codex" ]]; then
  SYNC_CMD+=(--codex)
  export AGENT_LAYER_RUN_CODEX=1
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
    AGENT_LAYER_SYNC_ROOTS=1 "${SYNC_CMD[@]}" --check || AGENT_LAYER_SYNC_ROOTS=1 "${SYNC_CMD[@]}"
  else
    AGENT_LAYER_SYNC_ROOTS=1 "${SYNC_CMD[@]}"
  fi
fi

# Run the CLI with agent-layer env (and optional project env).
if [[ "$need_env" == "1" ]]; then
  if [[ "$project_env" == "1" ]]; then
    if [[ "$TEMP_PARENT_ROOT_CREATED" == "1" ]]; then
      "$AGENT_LAYER_ROOT/with-env.sh" --project-env "$@"
      exit $?
    fi
    exec "$AGENT_LAYER_ROOT/with-env.sh" --project-env "$@"
  else
    if [[ "$TEMP_PARENT_ROOT_CREATED" == "1" ]]; then
      "$AGENT_LAYER_ROOT/with-env.sh" "$@"
      exit $?
    fi
    exec "$AGENT_LAYER_ROOT/with-env.sh" "$@"
  fi
fi

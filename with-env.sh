#!/usr/bin/env bash
set -euo pipefail

# Repo-local helper to load .agent-layer env into the environment, then exec the command.
# Usage:
#   ./.agent-layer/with-env.sh claude
#   ./.agent-layer/with-env.sh codex
#   ./.agent-layer/with-env.sh --project-env gemini

# Resolve the entrypoint helper to locate the repo root.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
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

INCLUDE_PROJECT_ENV=0
parent_root=""
use_temp_parent_root="0"

# Parse CLI flags and validate required arguments.
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
    --project-env)
      INCLUDE_PROJECT_ENV=1
      shift
      ;;
    --help | -h)
      echo "Usage: ./.agent-layer/with-env.sh [--project-env] [--parent-root <path>] [--temp-parent-root] <command> [args...]"
      exit 0
      ;;
    --)
      shift
      break
      ;;
    *) break ;;
  esac
done

if [[ $# -lt 1 ]]; then
  echo "Usage: ./.agent-layer/with-env.sh [--project-env] [--parent-root <path>] [--temp-parent-root] <command> [args...]"
  exit 2
fi

# shellcheck disable=SC1090
source "$ENTRYPOINT_SH"
if [[ -n "$parent_root" || "$use_temp_parent_root" == "1" || -z "${PARENT_ROOT:-}" || -z "${AGENT_LAYER_ROOT:-}" ]]; then
  ROOTS_PARENT_ROOT="$parent_root" ROOTS_USE_TEMP_PARENT_ROOT="$use_temp_parent_root" resolve_entrypoint_root || exit $?
fi

if [[ "${TEMP_PARENT_ROOT_CREATED:-0}" == "1" ]]; then
  # shellcheck disable=SC2153
  trap '[[ "${PARENT_ROOT_KEEP_TEMP:-0}" == "1" ]] || rm -rf "$PARENT_ROOT"' EXIT INT TERM
fi

# Load agent-layer .env if present.
AGENT_ENV="$AGENT_LAYER_ROOT/.env"

if [[ -f "$AGENT_ENV" ]]; then
  set -a
  # shellcheck disable=SC1090
  source "$AGENT_ENV"
  set +a
fi

# Optionally load the project .env after the agent-layer env.
PROJECT_ENV="$PARENT_ROOT/.env"
if [[ "$INCLUDE_PROJECT_ENV" -eq 1 && -f "$PROJECT_ENV" && "$PROJECT_ENV" != "$AGENT_ENV" ]]; then
  set -a
  # shellcheck disable=SC1090
  source "$PROJECT_ENV"
  set +a
fi

# Ensure CODEX_HOME points at the repo-local .codex when running Codex.
if [[ "${AGENT_LAYER_RUN_CODEX:-}" == "1" && -z "${CODEX_HOME:-}" ]]; then
  export CODEX_HOME="$PARENT_ROOT/.codex"
fi

# Execute the requested command with the loaded environment.
if [[ "${TEMP_PARENT_ROOT_CREATED:-0}" == "1" ]]; then
  "$@"
  exit $?
fi

exec "$@"

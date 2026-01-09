#!/usr/bin/env bash
set -euo pipefail

# Repo-local helper to load .agent-layer env into the environment, then exec the command.
# Usage:
#   ./.agent-layer/with-env.sh claude
#   ./.agent-layer/with-env.sh codex
#   ./.agent-layer/with-env.sh --project-env gemini

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

AGENTLAYER_ROOT="$WORKING_ROOT/.agent-layer"
# Keep the caller's working directory; use WORKING_ROOT only for env file paths.

INCLUDE_PROJECT_ENV=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --project-env)
      INCLUDE_PROJECT_ENV=1
      shift
      ;;
    --help | -h)
      echo "Usage: ./.agent-layer/with-env.sh [--project-env] <command> [args...]"
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
  echo "Usage: ./.agent-layer/with-env.sh [--project-env] <command> [args...]"
  exit 2
fi

AGENT_ENV="$AGENTLAYER_ROOT/.env"

if [[ -f "$AGENT_ENV" ]]; then
  set -a
  # shellcheck disable=SC1090
  source "$AGENT_ENV"
  set +a
fi

PROJECT_ENV="$WORKING_ROOT/.env"
if [[ "$INCLUDE_PROJECT_ENV" -eq 1 && -f "$PROJECT_ENV" && "$PROJECT_ENV" != "$AGENT_ENV" ]]; then
  set -a
  # shellcheck disable=SC1090
  source "$PROJECT_ENV"
  set +a
fi

exec "$@"

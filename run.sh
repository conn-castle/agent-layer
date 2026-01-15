#!/usr/bin/env bash
set -euo pipefail

# .agent-layer/run.sh
# Internal runner for ./al and .agent-layer/agent-layer (root resolution + sync/env execution).

# Resolve the repo root using the shared entrypoint helper.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
ENTRYPOINT_SH="$SCRIPT_DIR/src/lib/entrypoint.sh"
if [[ ! -f "$ENTRYPOINT_SH" ]]; then
  echo "ERROR: Missing src/lib/entrypoint.sh (expected near .agent-layer/)." >&2
  exit 2
fi
# shellcheck disable=SC1090
source "$ENTRYPOINT_SH"

# Parse root flags plus internal mode flags (used by the launcher options in .agent-layer/agent-layer).
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
    --sync)
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

# Detect agent commands for opt-in + defaults.
agent_cmd=""
if [[ $# -gt 0 ]]; then
  agent_candidate="$(basename -- "${1:-}")"
  case "$agent_candidate" in
    gemini | claude | codex)
      agent_cmd="$agent_candidate"
      ;;
  esac
fi

# Build the sync command; add --codex later if needed.
SYNC_CMD=(node "$AGENT_LAYER_ROOT/src/sync/sync.mjs")

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

# Resolve agent opt-in + defaults (only when launching a CLI).
agent_enabled=""
agent_default_args=()
if [[ -n "$agent_cmd" && "$need_env" == "1" ]]; then
  command -v node > /dev/null 2>&1 || {
    echo "ERROR: Node.js is required (node not found). Install Node, then re-run." >&2
    exit 2
  }
  if ! agent_info="$(node "$AGENT_LAYER_ROOT/src/lib/agent-config.mjs" --print-shell "$agent_cmd")"; then
    exit $?
  fi
  while IFS= read -r line; do
    case "$line" in
      enabled=*)
        agent_enabled="${line#enabled=}"
        ;;
      defaultArg=*)
        agent_default_args+=("${line#defaultArg=}")
        ;;
    esac
  done <<< "$agent_info"
  if [[ "$agent_enabled" != "true" ]]; then
    cat << EOF >&2
ERROR: ${agent_cmd} is disabled in .agent-layer/config/agents.json.

Enable it, then re-run:
  ./al --sync
  # or: ./al ${agent_cmd}
EOF
    exit 2
  fi
fi

# Pass --codex when launching Codex and export the codex run flag.
if [[ "$agent_cmd" == "codex" ]]; then
  export AGENT_LAYER_RUN_CODEX=1
  if [[ "$need_sync" == "1" ]]; then
    SYNC_CMD+=(--codex)
  fi
fi

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
  final_args=("$@")
  if [[ -n "$agent_cmd" && ${#agent_default_args[@]} -gt 0 ]]; then
    appended=()
    i=0
    while [[ $i -lt ${#agent_default_args[@]} ]]; do
      token="${agent_default_args[$i]}"
      if [[ "$token" == --* ]]; then
        flag="${token%%=*}"
        has_flag="0"
        if [[ ${#final_args[@]} -gt 1 ]]; then
          for user_arg in "${final_args[@]:1}"; do
            if [[ "$user_arg" == "$flag" || "$user_arg" == "$flag="* ]]; then
              has_flag="1"
              break
            fi
          done
        fi
        if [[ "$has_flag" == "1" ]]; then
          if [[ "$token" != *=* ]]; then
            next="${agent_default_args[$((i + 1))]:-}"
            if [[ -n "$next" && "$next" != --* ]]; then
              i=$((i + 1))
            fi
          fi
        else
          appended+=("$token")
          if [[ "$token" != *=* ]]; then
            next="${agent_default_args[$((i + 1))]:-}"
            if [[ -n "$next" && "$next" != --* ]]; then
              appended+=("$next")
              i=$((i + 1))
            fi
          fi
        fi
      else
        appended+=("$token")
      fi
      i=$((i + 1))
    done
    final_args+=("${appended[@]}")
  fi

  if [[ "$project_env" == "1" ]]; then
    if [[ "$TEMP_PARENT_ROOT_CREATED" == "1" ]]; then
      "$AGENT_LAYER_ROOT/with-env.sh" --project-env "${final_args[@]}"
      exit $?
    fi
    exec "$AGENT_LAYER_ROOT/with-env.sh" --project-env "${final_args[@]}"
  else
    if [[ "$TEMP_PARENT_ROOT_CREATED" == "1" ]]; then
      "$AGENT_LAYER_ROOT/with-env.sh" "${final_args[@]}"
      exit $?
    fi
    exec "$AGENT_LAYER_ROOT/with-env.sh" "${final_args[@]}"
  fi
fi

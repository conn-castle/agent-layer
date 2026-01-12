#!/usr/bin/env bash

# Root discovery helpers for agent-layer scripts.

ROOTS_HELPER_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEMP_WORK_ROOT_HELPER="$ROOTS_HELPER_DIR/temp-work-root.sh"
ROOT_SEARCH_MAX_DEPTH=50 # Avoid infinite ancestor scans on unusual mount layouts.

normalize_root_path() {
  local dir="$1"
  if [[ -z "$dir" ]]; then
    printf "%s" "$dir"
    return 0
  fi
  # macOS can emit // in some root paths; normalize to avoid endless loops.
  while [[ "$dir" == "//"* ]]; do
    dir="${dir#"/"}"
  done
  printf "%s" "$dir"
}

find_agentlayer_root() {
  local dir="$1" parent dir_norm parent_norm
  if [[ -z "$dir" ]]; then
    return 1
  fi
  dir="$(cd "$dir" && pwd)"
  for ((i = 0; i < ROOT_SEARCH_MAX_DEPTH; i++)); do
    if [[ "$(basename "$dir")" == ".agent-layer" ]]; then
      printf "%s" "$dir"
      return 0
    fi
    parent="$(cd "$dir/.." && pwd)"
    dir_norm="$(normalize_root_path "$dir")"
    parent_norm="$(normalize_root_path "$parent")"
    if [[ "$parent_norm" == "$dir_norm" ]]; then
      break
    fi
    dir="$parent"
  done
  return 1
}

find_agentlayer_repo_root() {
  local dir="$1" parent dir_norm parent_norm
  if [[ -z "$dir" ]]; then
    return 1
  fi
  dir="$(cd "$dir" && pwd)"
  for ((i = 0; i < ROOT_SEARCH_MAX_DEPTH; i++)); do
    if [[ -f "$dir/src/sync/sync.mjs" ]]; then
      printf "%s" "$dir"
      return 0
    fi
    parent="$(cd "$dir/.." && pwd)"
    dir_norm="$(normalize_root_path "$dir")"
    parent_norm="$(normalize_root_path "$parent")"
    if [[ "$parent_norm" == "$dir_norm" ]]; then
      break
    fi
    dir="$parent"
  done
  return 1
}

find_working_root() {
  local dir="$1" parent dir_norm parent_norm
  if [[ -z "$dir" ]]; then
    return 1
  fi
  dir="$(cd "$dir" && pwd)"
  for ((i = 0; i < ROOT_SEARCH_MAX_DEPTH; i++)); do
    if [[ -d "$dir/.agent-layer" ]]; then
      printf "%s" "$dir"
      return 0
    fi
    parent="$(cd "$dir/.." && pwd)"
    dir_norm="$(normalize_root_path "$dir")"
    parent_norm="$(normalize_root_path "$parent")"
    if [[ "$parent_norm" == "$dir_norm" ]]; then
      break
    fi
    dir="$parent"
  done
  return 1
}

resolve_working_root() {
  local start
  for start in "$@"; do
    if [[ -z "$start" ]]; then
      continue
    fi
    local root
    root="$(find_working_root "$start" || true)"
    if [[ -n "$root" ]]; then
      printf "%s" "$root"
      return 0
    fi
  done
  return 1
}

roots_die() {
  local msg="$1"
  if declare -F die > /dev/null 2>&1; then
    die "$msg"
  else
    printf "ERROR: %s\n" "$msg" >&2
    return 2
  fi
}

resolve_roots() {
  if [[ -z "${SCRIPT_DIR:-}" ]]; then
    roots_die "SCRIPT_DIR must be set before calling resolve_roots."
    return 2
  fi

  local allow_consumer_work_root="${ROOTS_ALLOW_CONSUMER_WORK_ROOT:-0}"
  local default_temp="${ROOTS_DEFAULT_TEMP_WORK_ROOT:-0}"
  local require_work_root="${ROOTS_REQUIRE_WORK_ROOT_IN_AGENT_LAYER:-0}"
  local work_root="${WORK_ROOT:-}"
  local use_temp="${USE_TEMP_WORK_ROOT:-0}"

  if [[ "$use_temp" == "1" && -n "$work_root" ]]; then
    roots_die "Choose one of --work-root or --temp-work-root."
    return 2
  fi

  TEMP_WORK_ROOT_CREATED="0"
  export TEMP_WORK_ROOT_CREATED

  local agentlayer_root
  agentlayer_root="$(find_agentlayer_root "$SCRIPT_DIR" || true)"
  if [[ -n "$agentlayer_root" ]]; then
    IS_CONSUMER_LAYOUT="1"
    if [[ "$use_temp" == "1" ]]; then
      roots_die "--temp-work-root is only supported in the agent-layer repo."
      return 2
    fi
    if [[ -n "$work_root" && "$allow_consumer_work_root" != "1" ]]; then
      roots_die "--work-root is not supported in this layout."
      return 2
    fi
    if [[ -n "$work_root" ]]; then
      if [[ ! -d "$work_root" ]]; then
        roots_die "--work-root does not exist: $work_root"
        return 2
      fi
      work_root="$(cd "$work_root" && pwd)"
      if [[ ! -d "$work_root/.agent-layer" ]]; then
        roots_die "--work-root must contain a .agent-layer directory: $work_root"
        return 2
      fi
      WORKING_ROOT="$work_root"
      AGENTLAYER_ROOT="$work_root/.agent-layer"
    else
      WORKING_ROOT="$(cd "$agentlayer_root/.." && pwd)"
      AGENTLAYER_ROOT="$agentlayer_root"
    fi
  else
    IS_CONSUMER_LAYOUT="0"
    agentlayer_root="$(find_agentlayer_repo_root "$SCRIPT_DIR" || true)"
    if [[ -z "$agentlayer_root" ]]; then
      roots_die "Missing agent-layer repo root (expected src/sync/sync.mjs)."
      return 2
    fi
    AGENTLAYER_ROOT="$agentlayer_root"

    if [[ "$use_temp" == "1" || ( "$default_temp" == "1" && -z "$work_root" ) ]]; then
      if [[ -f "$TEMP_WORK_ROOT_HELPER" ]]; then
        # shellcheck disable=SC1090
        source "$TEMP_WORK_ROOT_HELPER"
      fi
      if ! declare -F make_temp_work_root > /dev/null 2>&1; then
        roots_die "Missing src/lib/temp-work-root.sh (expected in the agent-layer repo)."
        return 2
      fi
      if [[ -n "${TEMP_WORK_ROOT_PREFIX:-}" ]]; then
        work_root="$(make_temp_work_root "$AGENTLAYER_ROOT" "$TEMP_WORK_ROOT_PREFIX")"
      else
        work_root="$(make_temp_work_root "$AGENTLAYER_ROOT")"
      fi
      if [[ -z "$work_root" || ! -d "$work_root" ]]; then
        roots_die "Failed to create a temporary work root."
        return 2
      fi
      TEMP_WORK_ROOT_CREATED="1"
    elif [[ -n "$work_root" ]]; then
      if [[ ! -d "$work_root" ]]; then
        roots_die "--work-root does not exist: $work_root"
        return 2
      fi
      work_root="$(cd "$work_root" && pwd)"
    else
      if [[ "$require_work_root" == "1" ]]; then
        roots_die "Missing .agent-layer/ directory in this path or any parent. Re-run with --work-root <path> (or use --temp-work-root in the agent-layer repo)."
        return 2
      fi
      work_root="$AGENTLAYER_ROOT"
    fi
    WORKING_ROOT="$work_root"
  fi

  export WORKING_ROOT AGENTLAYER_ROOT IS_CONSUMER_LAYOUT TEMP_WORK_ROOT_CREATED
  return 0
}

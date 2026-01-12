#!/usr/bin/env bash

# Temp parent root creation helper (spec-compliant).
# Exposes make_temp_parent_root for use by parent-root.sh.

TEMP_PARENT_ROOT_FAILED_DIR=""

make_temp_parent_root() {
  local agent_layer_root="$1"
  local temp_dir=""
  local base_dir=""
  local has_mktemp="0"

  if [[ -z "$agent_layer_root" || ! -d "$agent_layer_root" ]]; then
    return 2
  fi

  agent_layer_root="$(cd "$agent_layer_root" && pwd -P)"

  if command -v mktemp > /dev/null 2>&1; then
    has_mktemp="1"
  fi

  if [[ "$has_mktemp" == "1" ]]; then
    base_dir="${TMPDIR:-/tmp}"
    temp_dir="$(mktemp -d "${base_dir%/}/agent-layer-temp-parent-root.XXXXXX" 2> /dev/null || true)"

    if [[ -z "$temp_dir" || ! -d "$temp_dir" ]]; then
      mkdir -p "$agent_layer_root/tmp" 2> /dev/null || true
      temp_dir="$(mktemp -d "$agent_layer_root/tmp/agent-layer-temp-parent-root.XXXXXX" 2> /dev/null || true)"
    fi

    if [[ -z "$temp_dir" || ! -d "$temp_dir" ]]; then
      return 2
    fi
  else
    mkdir -p "$agent_layer_root/tmp" 2> /dev/null || true
    temp_dir="$agent_layer_root/tmp/agent-layer-temp-parent-root.$$"
    if ! mkdir -p "$temp_dir" 2> /dev/null; then
      return 2
    fi
  fi

  if ! ln -s "$agent_layer_root" "$temp_dir/.agent-layer" 2> /dev/null; then
    # shellcheck disable=SC2034
    TEMP_PARENT_ROOT_FAILED_DIR="$temp_dir"
    rm -rf "$temp_dir"
    return 3
  fi

  printf "%s" "$temp_dir"
  return 0
}

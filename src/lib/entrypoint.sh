#!/usr/bin/env bash
set -euo pipefail

resolve_entrypoint_root() {
  local script_dir
  script_dir="$(cd "$(dirname "${BASH_SOURCE[1]}")" && pwd)"
  local paths_sh
  paths_sh="$script_dir/.agent-layer/src/lib/paths.sh"
  if [[ ! -f "$paths_sh" ]]; then
    paths_sh="$script_dir/src/lib/paths.sh"
  fi
  if [[ ! -f "$paths_sh" ]]; then
    paths_sh="$script_dir/../src/lib/paths.sh"
  fi
  if [[ ! -f "$paths_sh" ]]; then
    echo "ERROR: Missing src/lib/paths.sh (expected near .agent-layer/)." >&2
    return 2
  fi
  # shellcheck disable=SC1090
  source "$paths_sh"

  local working_root
  working_root="$(resolve_working_root "$script_dir" "$PWD" || true)"
  if [[ -z "$working_root" ]]; then
    echo "ERROR: Missing .agent-layer/ directory in this path or any parent." >&2
    return 2
  fi

  WORKING_ROOT="$working_root"
  AGENTLAYER_ROOT="$working_root/.agent-layer"
  export WORKING_ROOT AGENTLAYER_ROOT
  return 0
}

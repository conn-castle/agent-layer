#!/usr/bin/env bash
set -euo pipefail

# Shared entrypoint resolution for agent-layer shell scripts.
# Call resolve_entrypoint_root to populate WORKING_ROOT and AGENTLAYER_ROOT.

resolve_entrypoint_root() {
  # Resolve the caller's directory and locate the nearest .agent-layer root.
  # On success, exports WORKING_ROOT and AGENTLAYER_ROOT; returns 0 on success, 2 on failure.
  local script_dir
  script_dir="$(cd "$(dirname "${BASH_SOURCE[1]}")" && pwd)"
  local discover_sh
  discover_sh="$script_dir/.agent-layer/src/lib/discover-root.sh"
  if [[ ! -f "$discover_sh" ]]; then
    discover_sh="$script_dir/src/lib/discover-root.sh"
  fi
  if [[ ! -f "$discover_sh" ]]; then
    discover_sh="$script_dir/../src/lib/discover-root.sh"
  fi
  if [[ ! -f "$discover_sh" ]]; then
    echo "ERROR: Missing src/lib/discover-root.sh (expected near .agent-layer/)." >&2
    return 2
  fi
  # shellcheck disable=SC1090
  source "$discover_sh"

  local working_root
  working_root="$(resolve_working_root "$script_dir" "$PWD" || true)"
  if [[ -z "$working_root" ]]; then
    local repo_root
    repo_root="$(find_agentlayer_repo_root "$script_dir" || true)"
    if [[ -z "$repo_root" ]]; then
      echo "ERROR: Missing .agent-layer/ directory in this path or any parent." >&2
      return 2
    fi
    WORKING_ROOT="$repo_root"
    AGENTLAYER_ROOT="$repo_root"
    export WORKING_ROOT AGENTLAYER_ROOT
    return 0
  fi

  WORKING_ROOT="$working_root"
  AGENTLAYER_ROOT="$working_root/.agent-layer"
  export WORKING_ROOT AGENTLAYER_ROOT
  return 0
}

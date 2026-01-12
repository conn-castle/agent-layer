#!/usr/bin/env bash
set -euo pipefail

# Shared entrypoint resolution for agent-layer shell scripts.
# Call resolve_entrypoint_root to populate PARENT_ROOT and AGENT_LAYER_ROOT.

resolve_entrypoint_root() {
  local entry_dir
  entry_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
  local parent_root_sh
  parent_root_sh="$entry_dir/parent-root.sh"

  if [[ ! -f "$parent_root_sh" ]]; then
    echo "ERROR: Missing src/lib/parent-root.sh (expected near .agent-layer/)." >&2
    return 2
  fi

  # shellcheck disable=SC1090
  source "$parent_root_sh"
  resolve_parent_root || return $?
}

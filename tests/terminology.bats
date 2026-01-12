#!/usr/bin/env bats

# Terminology consistency checks.
# Load shared helpers for temp roots and stub binaries.
load "helpers.bash"

# Test: legacy root terminology has been removed (excluding plan/README).
@test "Terminology: legacy root names are gone" {
  if ! command -v rg > /dev/null 2>&1; then
    skip "rg not available"
  fi

  run rg -n \
    -g '!**/README.md' \
    -g '!**/plan.md' \
    -g '!**/tests/terminology.bats' \
    -g '!**/tmp/**' \
    -e 'WORKING_ROOT' \
    -e 'work-root' \
    -e 'work_root' \
    -e 'AGENTLAYER_ROOT' \
    -e 'AGENTLAYER_SYNC_ROOTS' \
    -e 'discover-root.sh' \
    -e 'temp-work-root.sh' \
    -e 'find_working_root' \
    "$AGENT_LAYER_ROOT"

  if [[ "$status" -eq 0 ]]; then
    printf "%s\n" "$output"
    return 1
  fi
  [ "$status" -eq 1 ]
}

#!/usr/bin/env bats

# Tests for working root discovery helpers.
# Load shared helpers for temp roots and stub binaries.
load "helpers.bash"

# Test: resolve_working_root finds .agent-layer from working root
@test "resolve_working_root finds .agent-layer from working root" {
  local root
  root="$(create_working_root)"

  ROOT="$root" AGENTLAYER_ROOT="$AGENTLAYER_ROOT" run bash -c \
    'source "$AGENTLAYER_ROOT/src/lib/discover-root.sh"; resolve_working_root "$ROOT"'

  [ "$status" -eq 0 ]
  [ "$output" = "$root" ]
  rm -rf "$root"
}

# Test: resolve_working_root fails when .agent-layer is missing from ancestors
@test "resolve_working_root fails when .agent-layer is missing from ancestors" {
  local root
  root="/"

  ROOT="$root" AGENTLAYER_ROOT="$AGENTLAYER_ROOT" run bash -c \
    'source "$AGENTLAYER_ROOT/src/lib/discover-root.sh"; resolve_working_root "$ROOT"'

  [ "$status" -ne 0 ]
}

# Test: make_temp_work_root creates a .agent-layer symlink.
@test "make_temp_work_root creates .agent-layer symlink" {
  AGENTLAYER_ROOT="$AGENTLAYER_ROOT" run bash -c '
    set -euo pipefail
    source "$AGENTLAYER_ROOT/src/lib/temp-work-root.sh"
    dir="$(make_temp_work_root "$AGENTLAYER_ROOT")"
    test -L "$dir/.agent-layer"
    test "$(cd "$dir/.agent-layer" && pwd -P)" = "$(cd "$AGENTLAYER_ROOT" && pwd -P)"
    rm -rf "$dir"
  '

  [ "$status" -eq 0 ]
}

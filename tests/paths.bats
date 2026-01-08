#!/usr/bin/env bats

load "helpers.bash"

@test "resolve_working_root finds .agentlayer from working root" {
  local root
  root="$(create_working_root)"

  ROOT="$root" AGENTLAYER_ROOT="$AGENTLAYER_ROOT" run bash -c \
    'source "$AGENTLAYER_ROOT/lib/paths.sh"; resolve_working_root "$ROOT"'

  [ "$status" -eq 0 ]
  [ "$output" = "$root" ]
  rm -rf "$root"
}

@test "resolve_working_root fails when .agentlayer is missing" {
  local root
  root="$(make_tmp_dir)"

  ROOT="$root" AGENTLAYER_ROOT="$AGENTLAYER_ROOT" run bash -c \
    'source "$AGENTLAYER_ROOT/lib/paths.sh"; resolve_working_root "$ROOT"'

  [ "$status" -ne 0 ]
  rm -rf "$root"
}

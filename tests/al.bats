#!/usr/bin/env bats

load "helpers.bash"

@test "al uses its script dir when PWD points at another working repo" {
  local root_a root_b stub_bin output
  root_a="$(create_working_root)"
  root_b="$(create_working_root)"

  ln -s "$root_a/.agentlayer/al" "$root_a/al"
  stub_bin="$(create_stub_node "$root_a")"

  output="$(cd "$root_b/sub/dir" && PATH="$stub_bin:$PATH" "$root_a/al" pwd)"
  status=$?
  [ "$status" -eq 0 ]
  [ "$output" = "$root_a" ]

  rm -rf "$root_a" "$root_b"
}

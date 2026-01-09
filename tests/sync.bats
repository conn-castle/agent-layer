#!/usr/bin/env bats

load "helpers.bash"

@test "sync generates Codex config and instructions" {
  local root
  root="$(create_working_root)"

  run node "$root/.agent-layer/sync/sync.mjs"
  [ "$status" -eq 0 ]

  [ -f "$root/.codex/config.toml" ]
  [ -f "$root/.codex/AGENTS.md" ]
  grep -q '^\[mcp_servers\.' "$root/.codex/config.toml"
  grep -q 'GENERATED FILE' "$root/.codex/AGENTS.md"

  rm -rf "$root"
}

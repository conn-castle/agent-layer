#!/usr/bin/env bats

# Integration tests for end-to-end workflows.
# Load shared helpers for temp roots and stub binaries.
load "helpers.bash"

# Helper: write a stub command that exits successfully.
write_stub_cmd() {
  local bin="$1" name="$2"
  cat >"$bin/$name" <<'CMD'
#!/usr/bin/env bash
exit 0
CMD
  chmod +x "$bin/$name"
}

# Helper: write a stub command that logs its args to a file.
write_stub_logger() {
  local bin="$1" name="$2" log_path="$3"
  cat >"$bin/$name" <<EOF
#!/usr/bin/env bash
printf "%s\\n" "\$@" >> "$log_path"
exit 0
EOF
  chmod +x "$bin/$name"
}

# Helper: create a minimal dev repo layout for tests/run.sh.
create_dev_repo() {
  local root="$1"
  mkdir -p "$root/tests" "$root/src/lib" "$root/src/sync"
  cp "$AGENT_LAYER_ROOT/tests/run.sh" "$root/tests/run.sh"
  cp "$AGENT_LAYER_ROOT/src/lib/parent-root.sh" "$root/src/lib/parent-root.sh"
  cp "$AGENT_LAYER_ROOT/src/lib/temp-parent-root.sh" "$root/src/lib/temp-parent-root.sh"
  : >"$root/src/sync/sync.mjs"
  chmod +x "$root/tests/run.sh"
}

# Test: bootstrap -> setup -> al -> tests
@test "Integration: bootstrap -> al -> tests" {
  local root stub_bin bash_bin node_log npm_log bats_log
  root="$(create_isolated_parent_root)"
  stub_bin="$root/stub-bin"
  bash_bin="$(command -v bash)"
  node_log="$root/node.log"
  npm_log="$root/npm.log"
  bats_log="$root/bats.log"

  mkdir -p "$stub_bin"
  write_stub_logger "$stub_bin" "node" "$node_log"
  write_stub_logger "$stub_bin" "npm" "$npm_log"
  write_stub_logger "$stub_bin" "bats" "$bats_log"
  write_stub_cmd "$stub_bin" "git"
  write_stub_cmd "$stub_bin" "rg"
  write_stub_cmd "$stub_bin" "shfmt"
  write_stub_cmd "$stub_bin" "shellcheck"
  write_stub_cmd "$stub_bin" "ruby"
  write_stub_cmd "$stub_bin" "bashcov"

  mkdir -p "$root/.agent-layer/node_modules/.bin"
  cat >"$root/.agent-layer/node_modules/.bin/prettier" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
  chmod +x "$root/.agent-layer/node_modules/.bin/prettier"

  mkdir -p "$root/.agent-layer/src/mcp/agent-layer-prompts"
  printf "{}\n" >"$root/.agent-layer/src/mcp/agent-layer-prompts/package.json"

  cp "$AGENT_LAYER_ROOT/tests/run.sh" "$root/.agent-layer/tests/run.sh"
  chmod +x "$root/.agent-layer/tests/run.sh"

  run "$bash_bin" -c "cd '$root' && PATH='$stub_bin:/usr/bin:/bin' '$root/.agent-layer/dev/bootstrap.sh' --yes --parent-root '$root'"
  [ "$status" -eq 0 ]

  run rg -n "sync.mjs" "$node_log"
  [ "$status" -eq 0 ]
  run rg -n "^install$" "$npm_log"
  [ "$status" -eq 0 ]

  run "$bash_bin" -c "cd '$root' && PATH='$stub_bin:/usr/bin:/bin' '$root/.agent-layer/al' echo ok"
  [ "$status" -eq 0 ]
  [ "$output" = "ok" ]

  run "$bash_bin" -c "cd '$root' && PATH='$stub_bin:/usr/bin:/bin' BATS_BIN='bats' '$root/.agent-layer/tests/run.sh'"
  [ "$status" -eq 0 ]
  run rg -n "/tests" "$bats_log"
  [ "$status" -eq 0 ]

  rm -rf "$root"
}

# Test: CI simulation runs tests via temp parent root in dev repo layout.
@test "Integration: CI runs tests/run.sh --temp-parent-root" {
  local base tool_root stub_bin bash_bin bats_log
  base="$(make_tmp_dir)"
  tool_root="$base/agent-layer"
  stub_bin="$tool_root/stub-bin"
  bash_bin="$(command -v bash)"
  bats_log="$tool_root/bats.log"

  mkdir -p "$tool_root" "$stub_bin"
  create_dev_repo "$tool_root"

  write_stub_cmd "$stub_bin" "git"
  write_stub_cmd "$stub_bin" "node"
  write_stub_cmd "$stub_bin" "rg"
  write_stub_cmd "$stub_bin" "shfmt"
  write_stub_cmd "$stub_bin" "shellcheck"
  write_stub_cmd "$stub_bin" "ruby"
  write_stub_cmd "$stub_bin" "bashcov"
  write_stub_cmd "$stub_bin" "prettier"
  write_stub_logger "$stub_bin" "bats" "$bats_log"

  run "$bash_bin" -c "cd '$tool_root' && PATH='$stub_bin:/usr/bin:/bin' BATS_BIN='bats' '$tool_root/tests/run.sh' --temp-parent-root"
  [ "$status" -eq 0 ]
  run rg -n "/tests" "$bats_log"
  [ "$status" -eq 0 ]

  rm -rf "$base"
}

#!/usr/bin/env bats

# Tests for tests/run.sh parent-root behavior.
# Load shared helpers for temp roots and stub binaries.
load "helpers.bash"

teardown() {
  cleanup_test_temp_dirs
}

# Helper: stub the external tools required by tests/run.sh.
write_stub_tools() {
  local bin="$1"
  mkdir -p "$bin"
  for cmd in git rg shfmt shellcheck bats prettier; do
    cat >"$bin/$cmd" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
    chmod +x "$bin/$cmd"
  done
}

# Helper: create a minimal agent-layer repo layout for tests/run.sh.
create_tool_repo() {
  local root="$1"
  mkdir -p "$root/tests" "$root/src/lib" "$root/src/sync"
  cp "$AGENT_LAYER_ROOT/tests/run.sh" "$root/tests/run.sh"
  cp "$AGENT_LAYER_ROOT/src/lib/roots.mjs" "$root/src/lib/roots.mjs"
  cp "$AGENT_LAYER_ROOT/src/lib/env.mjs" "$root/src/lib/env.mjs"
  cp "$AGENT_LAYER_ROOT/src/sync/utils.mjs" "$root/src/sync/utils.mjs"
  : >"$root/src/sync/sync.mjs"
  chmod +x "$root/tests/run.sh"
}

# Test: tests/run.sh fails without --parent-root in agent-layer repo layout
@test "tests/run.sh requires --parent-root when no .agent-layer exists" {
  local root tool_root bash_bin
  root="$(make_tmp_dir)"
  tool_root="$root/agent-layer"
  bash_bin="$(command -v bash)"

  mkdir -p "$tool_root"
  create_tool_repo "$tool_root"

  run "$bash_bin" -c "cd '$tool_root' && '$tool_root/tests/run.sh' 2>&1"
  [ "$status" -ne 0 ]
  [[ "$output" == *"Running from agent-layer repo requires explicit parent root configuration."* ]]
  [[ "$output" == *"--temp-parent-root"* ]]

  rm -rf "$root"
}

# Test: tests/run.sh succeeds with --parent-root in agent-layer repo layout
@test "tests/run.sh accepts --parent-root without .agent-layer in agent-layer repo layout" {
  local tool_root parent_root stub_bin bash_bin base node_bin
  base="$(make_tmp_dir)"
  tool_root="$base/agent-layer"
  parent_root="$(make_tmp_dir)"
  stub_bin="$parent_root/stub-bin"
  bash_bin="$(command -v bash)"
  node_bin="$(dirname "$(command -v node)")"

  mkdir -p "$tool_root"
  create_tool_repo "$tool_root"
  write_stub_tools "$stub_bin"
  ln -s "$tool_root" "$parent_root/.agent-layer"

  run "$bash_bin" -c "cd '$tool_root' && PATH='$stub_bin:$node_bin:/usr/bin:/bin' BATS_BIN='bats' '$tool_root/tests/run.sh' --parent-root '$parent_root' --setup-only"
  [ "$status" -eq 0 ]
  [[ "$output" == *"Setup validated successfully"* ]]
  [[ "$output" == *"PARENT_ROOT=$parent_root"* ]]

  rm -rf "$base" "$parent_root"
}

# Test: tests/run.sh can create a temp parent root in agent-layer repo layout
@test "tests/run.sh accepts --temp-parent-root in agent-layer repo layout" {
  local tool_root stub_bin bash_bin base node_bin
  base="$(make_tmp_dir)"
  tool_root="$base/agent-layer"
  stub_bin="$tool_root/stub-bin"
  bash_bin="$(command -v bash)"
  node_bin="$(dirname "$(command -v node)")"

  mkdir -p "$tool_root"
  create_tool_repo "$tool_root"
  write_stub_tools "$stub_bin"

  run "$bash_bin" -c "cd '$tool_root' && PATH='$stub_bin:$node_bin:/usr/bin:/bin' BATS_BIN='bats' '$tool_root/tests/run.sh' --temp-parent-root --setup-only"
  [ "$status" -eq 0 ]
  [[ "$output" == *"Setup validated successfully"* ]]

  rm -rf "$base"
}

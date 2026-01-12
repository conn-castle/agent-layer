#!/usr/bin/env bats

# Tests for tests/run.sh work-root behavior.
# Load shared helpers for temp roots and stub binaries.
load "helpers.bash"

# Helper: stub the external tools required by tests/run.sh.
write_stub_tools() {
  local bin="$1"
  mkdir -p "$bin"
  for cmd in git node rg shfmt shellcheck bats prettier; do
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
  cp "$AGENTLAYER_ROOT/tests/run.sh" "$root/tests/run.sh"
  cp "$AGENTLAYER_ROOT/src/lib/discover-root.sh" "$root/src/lib/discover-root.sh"
  cp "$AGENTLAYER_ROOT/src/lib/temp-work-root.sh" "$root/src/lib/temp-work-root.sh"
  : >"$root/src/sync/sync.mjs"
  chmod +x "$root/tests/run.sh"
}

# Test: tests/run.sh fails without --work-root in agent-layer repo layout
@test "tests/run.sh requires --work-root when no .agent-layer exists" {
  local root bash_bin
  root="$(make_tmp_dir)"
  bash_bin="$(command -v bash)"

  create_tool_repo "$root"

  run "$bash_bin" -c "cd '$root' && '$root/tests/run.sh' 2>&1"
  [ "$status" -ne 0 ]
  [[ "$output" == *"Missing .agent-layer/"* ]]
  [[ "$output" == *"--work-root"* ]]

  rm -rf "$root"
}

# Test: tests/run.sh succeeds with --work-root in agent-layer repo layout
@test "tests/run.sh accepts --work-root without .agent-layer in agent-layer repo layout" {
  local tool_root work_root stub_bin bash_bin
  tool_root="$(make_tmp_dir)"
  work_root="$(make_tmp_dir)"
  stub_bin="$work_root/stub-bin"
  bash_bin="$(command -v bash)"

  create_tool_repo "$tool_root"
  write_stub_tools "$stub_bin"

  run "$bash_bin" -c "cd '$tool_root' && PATH='$stub_bin:/usr/bin:/bin' BATS_BIN='bats' '$tool_root/tests/run.sh' --work-root '$work_root'"
  [ "$status" -eq 0 ]

  rm -rf "$tool_root" "$work_root"
}

# Test: tests/run.sh can create a temp work root in agent-layer repo layout
@test "tests/run.sh accepts --temp-work-root in agent-layer repo layout" {
  local tool_root stub_bin bash_bin
  tool_root="$(make_tmp_dir)"
  stub_bin="$tool_root/stub-bin"
  bash_bin="$(command -v bash)"

  create_tool_repo "$tool_root"
  write_stub_tools "$stub_bin"

  run "$bash_bin" -c "cd '$tool_root' && PATH='$stub_bin:/usr/bin:/bin' BATS_BIN='bats' '$tool_root/tests/run.sh' --temp-work-root"
  [ "$status" -eq 0 ]

  rm -rf "$tool_root"
}

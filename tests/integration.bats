#!/usr/bin/env bats

# Integration tests for end-to-end workflows.
# Load shared helpers for temp roots and stub binaries.
load "helpers.bash"

teardown() {
  cleanup_test_temp_dirs
}

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
  cp "$AGENT_LAYER_ROOT/src/lib/roots.mjs" "$root/src/lib/roots.mjs"
  cp "$AGENT_LAYER_ROOT/src/lib/env.mjs" "$root/src/lib/env.mjs"
  cp "$AGENT_LAYER_ROOT/src/sync/utils.mjs" "$root/src/sync/utils.mjs"
  : >"$root/src/sync/sync.mjs"
  chmod +x "$root/tests/run.sh"
}

# Test: bootstrap runs sync and npm install, then launcher works
@test "Integration: bootstrap runs sync and launches agent" {
  local root stub_bin bash_bin npm_log sync_log node_bin git_bin rg_bin
  root="$(create_isolated_parent_root)"
  stub_bin="$root/stub-bin"
  bash_bin="$(command -v bash)"
  node_bin="$(dirname "$(command -v node)")"
  git_bin="$(dirname "$(command -v git)")"
  rg_bin="$(dirname "$(command -v rg)")"
  npm_log="$root/npm.log"
  sync_log="$root/sync.log"

  mkdir -p "$stub_bin"
  write_stub_logger "$stub_bin" "npm" "$npm_log"
  write_stub_cmd "$stub_bin" "shfmt"
  write_stub_cmd "$stub_bin" "shellcheck"

  # Run bootstrap (uses stub sync.mjs for SYNC_LOG)
  run "$bash_bin" -c "cd '$root' && PATH='$stub_bin:$node_bin:$git_bin:$rg_bin:/usr/bin:/bin' SYNC_LOG='$sync_log' '$root/.agent-layer/dev/bootstrap.sh' --yes --parent-root '$root'"
  [ "$status" -eq 0 ]

  # Verify sync was called
  [ -f "$sync_log" ]

  # Verify npm install was called
  run rg -n "^install$" "$npm_log"
  [ "$status" -eq 0 ]

  # Verify agent launcher works
  run "$bash_bin" -c "cd '$root' && PATH='$stub_bin:$node_bin:$git_bin:$rg_bin:/usr/bin:/bin' '$root/.agent-layer/agent-layer' echo ok"
  [ "$status" -eq 0 ]
  [ "$output" = "ok" ]

  rm -rf "$root"
}

# Test: CI can invoke tests/run.sh with --temp-parent-root in dev repo layout.
@test "Integration: CI runs tests/run.sh --temp-parent-root" {
  local base tool_root stub_bin bash_bin node_bin
  base="$(make_tmp_dir)"
  tool_root="$base/agent-layer"
  stub_bin="$tool_root/stub-bin"
  bash_bin="$(command -v bash)"
  node_bin="$(dirname "$(command -v node)")"

  mkdir -p "$tool_root" "$stub_bin"
  create_dev_repo "$tool_root"

  write_stub_cmd "$stub_bin" "git"
  write_stub_cmd "$stub_bin" "rg"
  write_stub_cmd "$stub_bin" "shfmt"
  write_stub_cmd "$stub_bin" "shellcheck"
  write_stub_cmd "$stub_bin" "prettier"
  write_stub_cmd "$stub_bin" "bats"

  run "$bash_bin" -c "cd '$tool_root' && PATH='$stub_bin:$node_bin:/usr/bin:/bin' BATS_BIN='bats' '$tool_root/tests/run.sh' --temp-parent-root --setup-only"
  [ "$status" -eq 0 ]
  [[ "$output" == *"Setup validated successfully"* ]]

  rm -rf "$base"
}

#!/usr/bin/env bats

# Tests for setup behavior via ./al --setup.
# Load shared helpers for temp roots and stub binaries.
load "helpers.bash"

teardown() {
  cleanup_test_temp_dirs
}

# Helper: write a stub command that exits successfully.
write_stub_cmd() {
  local bin="$1" name="$2"
  cat > "$bin/$name" << 'CMD'
#!/usr/bin/env bash
exit 0
CMD
  chmod +x "$bin/$name"
}

# Helper: write a stub command that logs arguments to a file.
write_stub_logger() {
  local bin="$1" name="$2" log_path="$3"
  cat > "$bin/$name" << EOF
#!/usr/bin/env bash
printf "%s\\n" "\$@" >> "$log_path"
exit 0
EOF
  chmod +x "$bin/$name"
}

# Test: ./al --setup fails when node is missing
@test "al --setup fails when node is missing" {
  local root stub_bin bash_bin
  root="$(create_isolated_parent_root)"
  stub_bin="$root/stub-bin"
  bash_bin="$(command -v bash)"

  mkdir -p "$stub_bin"
  ln -s "$bash_bin" "$stub_bin/bash"
  ln -s "$(command -v env)" "$stub_bin/env"
  ln -s "$(command -v basename)" "$stub_bin/basename"
  ln -s "$(command -v cat)" "$stub_bin/cat"
  ln -s "$(command -v dirname)" "$stub_bin/dirname"
  write_stub_cmd "$stub_bin" "npm"
  write_stub_cmd "$stub_bin" "git"

  run -127 "$bash_bin" -c "PATH='$stub_bin' '$root/.agent-layer/agent-layer' --setup 2>&1"
  [[ "$output" == *"node"* ]]

  rm -rf "$root"
}

# Test: ./al --setup runs sync and npm install with --skip-checks
@test "al --setup runs sync and npm install with --skip-checks" {
  local root stub_bin npm_log sync_log
  root="$(create_isolated_parent_root)"
  stub_bin="$root/stub-bin"
  npm_log="$root/npm.log"
  sync_log="$root/sync.log"

  mkdir -p "$stub_bin"
  ln -s "$(command -v bash)" "$stub_bin/bash"
  write_stub_logger "$stub_bin" "npm" "$npm_log"
  write_stub_cmd "$stub_bin" "git"

  mkdir -p "$root/.agent-layer/src/mcp/agent-layer-prompts"
  printf "{}\n" > "$root/.agent-layer/src/mcp/agent-layer-prompts/package.json"

  run bash -c "cd '$root' && PATH='$stub_bin:$PATH' SYNC_LOG='$sync_log' '$root/.agent-layer/agent-layer' --setup --skip-checks"
  [ "$status" -eq 0 ]
  [[ "$output" == *"Skipping sync check"* ]]

  [ -f "$sync_log" ]

  run rg -n "^install$" "$npm_log"
  [ "$status" -eq 0 ]

  rm -rf "$root"
}

# Test: ./al --setup prints required vs optional steps
@test "al --setup prints required vs optional steps" {
  local root stub_bin
  root="$(create_isolated_parent_root)"
  stub_bin="$root/stub-bin"

  mkdir -p "$stub_bin"
  ln -s "$(command -v bash)" "$stub_bin/bash"
  write_stub_cmd "$stub_bin" "npm"
  write_stub_cmd "$stub_bin" "git"

  mkdir -p "$root/.agent-layer/src/mcp/agent-layer-prompts"
  printf "{}\n" > "$root/.agent-layer/src/mcp/agent-layer-prompts/package.json"

  run bash -c "cd '$root' && PATH='$stub_bin:$PATH' '$root/.agent-layer/agent-layer' --setup --skip-checks"
  [ "$status" -eq 0 ]
  [[ "$output" == *"Required manual steps"* ]]
  [[ "$output" == *"Create/fill .agent-layer/.env"* ]]
  [[ "$output" == *"Review MCP servers: .agent-layer/config/mcp-servers.json"* ]]
  [[ "$output" == *"Optional customization"* ]]
  [[ "$output" == *"./al --sync"* ]]

  rm -rf "$root"
}

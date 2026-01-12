#!/usr/bin/env bats

# Tests for the setup script behavior.
# Load shared helpers for temp roots and stub binaries.
load "helpers.bash"

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

# Test: setup.sh fails when node is missing
@test "setup.sh fails when node is missing" {
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

  run "$bash_bin" -c "PATH='$stub_bin' '$bash_bin' '$root/.agent-layer/setup.sh' 2>&1"
  [ "$status" -ne 0 ]
  [[ "$output" == *"Node.js is required"* ]]

  rm -rf "$root"
}

# Test: setup.sh runs sync and npm install with --skip-checks
@test "setup.sh runs sync and npm install with --skip-checks" {
  local root stub_bin bash_bin node_log npm_log
  root="$(create_isolated_parent_root)"
  stub_bin="$root/stub-bin"
  bash_bin="$(command -v bash)"
  node_log="$root/node.log"
  npm_log="$root/npm.log"

  mkdir -p "$stub_bin"
  ln -s "$bash_bin" "$stub_bin/bash"
  ln -s "$(command -v env)" "$stub_bin/env"
  ln -s "$(command -v basename)" "$stub_bin/basename"
  ln -s "$(command -v cat)" "$stub_bin/cat"
  ln -s "$(command -v dirname)" "$stub_bin/dirname"
  write_stub_logger "$stub_bin" "node" "$node_log"
  write_stub_logger "$stub_bin" "npm" "$npm_log"
  write_stub_cmd "$stub_bin" "git"

  mkdir -p "$root/.agent-layer/src/mcp/agent-layer-prompts"
  printf "{}\n" > "$root/.agent-layer/src/mcp/agent-layer-prompts/package.json"

  run "$bash_bin" -c "cd '$root' && PATH='$stub_bin' '$bash_bin' '$root/.agent-layer/setup.sh' --skip-checks"
  [ "$status" -eq 0 ]
  [[ "$output" == *"Skipping sync check"* ]]

  run rg -n "sync.mjs" "$node_log"
  [ "$status" -eq 0 ]

  run rg -n "^install$" "$npm_log"
  [ "$status" -eq 0 ]

  rm -rf "$root"
}

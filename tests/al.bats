#!/usr/bin/env bats

# Tests for the repo-local launcher behavior in ./al.
# Load shared helpers for temp roots and stub binaries.
load "helpers.bash"

# Test: al uses its script dir when PWD points at another parent repo
@test "al uses its script dir when PWD points at another parent repo" {
  local root_a root_b stub_bin output
  root_a="$(create_parent_root)"
  root_b="$(create_parent_root)"

  ln -s "$root_a/.agent-layer/al" "$root_a/al"
  stub_bin="$(create_stub_node "$root_a")"

  output="$(cd "$root_b/sub/dir" && PATH="$stub_bin:$PATH" "$root_a/al" --parent-root "$root_a" pwd)"
  status=$?
  [ "$status" -eq 0 ]
  [ "$output" = "$root_a" ]

  rm -rf "$root_a" "$root_b"
}

# Test: al prefers .agent-layer paths when a root src/lib/parent-root.sh exists
@test "al prefers .agent-layer paths when a root src/lib/parent-root.sh exists" {
  local root stub_bin output
  root="$(create_parent_root)"

  ln -s "$root/.agent-layer/al" "$root/al"
  mkdir -p "$root/src/lib"
  cat > "$root/src/lib/parent-root.sh" << 'EOF'
resolve_parent_root() {
  return 1
}
EOF

  stub_bin="$(create_stub_node "$root")"

  output="$(cd "$root/sub/dir" && PATH="$stub_bin:$PATH" "$root/al" --parent-root "$root" pwd)"
  status=$?
  [ "$status" -eq 0 ]
  [ "$output" = "$root" ]

  rm -rf "$root"
}

# Test: al sets CODEX_HOME when unset
@test "al sets CODEX_HOME when unset" {
  local root stub_bin output
  root="$(create_isolated_parent_root)"
  mkdir -p "$root/.codex"
  stub_bin="$(create_stub_tools "$root")"
  cat > "$stub_bin/codex" << 'EOF'
#!/usr/bin/env bash
printf "%s" "${CODEX_HOME:-}"
EOF
  chmod +x "$stub_bin/codex"

  output="$(cd "$root/sub/dir" && PATH="$stub_bin:$PATH" CODEX_HOME= "$root/.agent-layer/al" codex)"
  status=$?
  [ "$status" -eq 0 ]
  [ "$output" = "$root/.codex" ]

  rm -rf "$root"
}

# Test: al does not warn when CODEX_HOME already matches repo-local
@test "al does not warn when CODEX_HOME already matches repo-local" {
  local root stub_bin output status
  root="$(create_isolated_parent_root)"
  mkdir -p "$root/.codex"
  stub_bin="$(create_stub_tools "$root")"
  cat > "$stub_bin/codex" << 'EOF'
#!/usr/bin/env bash
printf "%s" "${CODEX_HOME:-}"
EOF
  chmod +x "$stub_bin/codex"

  output="$(cd "$root/sub/dir" && PATH="$stub_bin:$PATH" CODEX_HOME="$root/.codex" \
    "$root/.agent-layer/al" codex 2>&1)"
  status=$?
  [ "$status" -eq 0 ]
  [ "$output" = "$root/.codex" ]

  rm -rf "$root"
}

# Test: al accepts CODEX_HOME when it resolves to repo-local via symlink
@test "al accepts CODEX_HOME when it resolves to repo-local via symlink" {
  local root stub_bin output
  root="$(create_isolated_parent_root)"
  mkdir -p "$root/.codex"
  stub_bin="$(create_stub_tools "$root")"
  cat > "$stub_bin/codex" << 'EOF'
#!/usr/bin/env bash
printf "%s" "${CODEX_HOME:-}"
EOF
  chmod +x "$stub_bin/codex"

  ln -s "$root/.codex" "$root/.codex-link"

  output="$(cd "$root/sub/dir" && PATH="$stub_bin:$PATH" CODEX_HOME="$root/.codex-link" \
    "$root/.agent-layer/al" codex 2>&1)"
  status=$?
  [ "$status" -eq 0 ]
  [ "$output" = "$root/.codex-link" ]

  rm -rf "$root"
}

# Test: al passes --codex to sync when running codex
@test "al passes --codex to sync when running codex" {
  local root stub_bin output node_args
  root="$(create_isolated_parent_root)"
  mkdir -p "$root/.codex"
  stub_bin="$root/stub-bin"
  mkdir -p "$stub_bin"
  node_args="$root/node-args.txt"
  cat > "$stub_bin/node" << 'EOF'
#!/usr/bin/env bash
printf "%s\n" "$@" > "$NODE_ARGS_LOG"
exit 0
EOF
  cat > "$stub_bin/codex" << 'EOF'
#!/usr/bin/env bash
exit 0
EOF
  chmod +x "$stub_bin/node" "$stub_bin/codex"

  run bash -c "cd \"$root/sub/dir\" && PATH=\"$stub_bin:$PATH\" NODE_ARGS_LOG=\"$node_args\" \"$root/.agent-layer/al\" codex"
  [ "$status" -eq 0 ]

  run rg -n -- "--codex" "$node_args"
  [ "$status" -eq 0 ]

  rm -rf "$root"
}

# Test: al fails when node is missing
@test "al fails when node is missing" {
  local root stub_bin output bash_bin
  root="$(create_isolated_parent_root)"
  stub_bin="$root/stub-bin"
  bash_bin="$(command -v bash)"
  mkdir -p "$stub_bin"
  ln -s "$bash_bin" "$stub_bin/bash"
  ln -s "$(command -v basename)" "$stub_bin/basename"
  ln -s "$(command -v dirname)" "$stub_bin/dirname"

  run "$bash_bin" -c "cd '$root/sub/dir' && PATH='$stub_bin' '$bash_bin' '$root/.agent-layer/al' pwd 2>&1"
  [ "$status" -ne 0 ]
  [[ "$output" == *"Node.js is required"* ]]

  rm -rf "$root"
}

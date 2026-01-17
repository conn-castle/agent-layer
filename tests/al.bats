#!/usr/bin/env bats

# Tests for the repo-local launcher behavior in ./al.
# Load shared helpers for temp roots and stub binaries.
load "helpers.bash"

teardown() {
  cleanup_test_temp_dirs
}

# Test: al uses its script dir when PWD points at another parent repo
@test "al uses its script dir when PWD points at another parent repo" {
  local root_a root_b stub_bin output
  root_a="$(create_parent_root)"
  root_b="$(create_parent_root)"

  ln -s "$root_a/.agent-layer/agent-layer" "$root_a/al"
  stub_bin="$root_a/stub-bin"
  mkdir -p "$stub_bin"
  cat >"$stub_bin/print-root" <<'EOF'
#!/usr/bin/env bash
printf "%s" "${PARENT_ROOT:-}"
EOF
  chmod +x "$stub_bin/print-root"

  output="$(cd "$root_b/sub/dir" && PATH="$stub_bin:$PATH" "$root_a/al" --no-sync --parent-root "$root_a" print-root)"
  status=$?
  [ "$status" -eq 0 ]
  [ "$output" = "$root_a" ]

  rm -rf "$root_a" "$root_b"
}

# Test: al prints the current version with --version
@test "al prints the current version with --version" {
  local root output status
  root="$(create_isolated_parent_root)"

  git -C "$root/.agent-layer" init -q
  git -C "$root/.agent-layer" config user.email "test@example.com"
  git -C "$root/.agent-layer" config user.name "Test User"
  git -C "$root/.agent-layer" add .
  git -C "$root/.agent-layer" commit -m "init" -q
  git -C "$root/.agent-layer" tag v0.1.0

  output="$("$root/.agent-layer/agent-layer" --version)"
  status=$?
  [ "$status" -eq 0 ]
  [ "$output" = "v0.1.0" ]

  rm -rf "$root"
}

# Test: al sets CODEX_HOME when unset
@test "al sets CODEX_HOME when unset" {
  local root stub_bin output
  root="$(create_isolated_parent_root)"
  mkdir -p "$root/.codex"
  stub_bin="$root/stub-bin"
  mkdir -p "$stub_bin"
  cat > "$stub_bin/codex" << 'EOF'
#!/usr/bin/env bash
printf "%s" "${CODEX_HOME:-}"
EOF
  chmod +x "$stub_bin/codex"

  output="$(cd "$root/sub/dir" && PATH="$stub_bin:$PATH" CODEX_HOME= "$root/.agent-layer/agent-layer" codex)"
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
  stub_bin="$root/stub-bin"
  mkdir -p "$stub_bin"
  cat > "$stub_bin/codex" << 'EOF'
#!/usr/bin/env bash
printf "%s" "${CODEX_HOME:-}"
EOF
  chmod +x "$stub_bin/codex"

  output="$(cd "$root/sub/dir" && PATH="$stub_bin:$PATH" CODEX_HOME="$root/.codex" \
    "$root/.agent-layer/agent-layer" codex 2>&1)"
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
  stub_bin="$root/stub-bin"
  mkdir -p "$stub_bin"
  cat > "$stub_bin/codex" << 'EOF'
#!/usr/bin/env bash
printf "%s" "${CODEX_HOME:-}"
EOF
  chmod +x "$stub_bin/codex"

  ln -s "$root/.codex" "$root/.codex-link"

  output="$(cd "$root/sub/dir" && PATH="$stub_bin:$PATH" CODEX_HOME="$root/.codex-link" \
    "$root/.agent-layer/agent-layer" codex 2>&1)"
  status=$?
  [ "$status" -eq 0 ]
  [ "$output" = "$root/.codex-link" ]

  rm -rf "$root"
}

# Test: al warns when CODEX_HOME points elsewhere
@test "al warns when CODEX_HOME points outside repo-local" {
  local root stub_bin output status
  root="$(create_isolated_parent_root)"
  mkdir -p "$root/.codex" "$root/alt-codex"
  stub_bin="$root/stub-bin"
  mkdir -p "$stub_bin"
  cat > "$stub_bin/codex" << 'EOF'
#!/usr/bin/env bash
printf "%s" "${CODEX_HOME:-}"
EOF
  chmod +x "$stub_bin/codex"

  output="$(cd "$root/sub/dir" && PATH="$stub_bin:$PATH" CODEX_HOME="$root/alt-codex" \
    "$root/.agent-layer/agent-layer" codex 2>&1)"
  status=$?
  [ "$status" -eq 0 ]
  [[ "$output" == *"CODEX_HOME is set to $root/alt-codex"* ]]
  [[ "$output" == *"expected $root/.codex"* ]]

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

  run -127 "$bash_bin" -c "cd '$root/sub/dir' && PATH='$stub_bin' '$bash_bin' '$root/.agent-layer/agent-layer' pwd 2>&1"
  [[ "$output" == *"node"* ]]

  rm -rf "$root"
}

#!/usr/bin/env bats

load "helpers.bash"

@test "al uses its script dir when PWD points at another working repo" {
  local root_a root_b stub_bin output
  root_a="$(create_working_root)"
  root_b="$(create_working_root)"

  ln -s "$root_a/.agent-layer/al" "$root_a/al"
  stub_bin="$(create_stub_node "$root_a")"

  output="$(cd "$root_b/sub/dir" && PATH="$stub_bin:$PATH" "$root_a/al" pwd)"
  status=$?
  [ "$status" -eq 0 ]
  [ "$output" = "$root_a" ]

  rm -rf "$root_a" "$root_b"
}

@test "al prefers .agent-layer paths when a root src/lib/paths.sh exists" {
  local root stub_bin output
  root="$(create_working_root)"

  ln -s "$root/.agent-layer/al" "$root/al"
  mkdir -p "$root/src/lib"
  cat >"$root/src/lib/paths.sh" <<'EOF'
resolve_working_root() {
  return 1
}
EOF

  stub_bin="$(create_stub_node "$root")"

  output="$(cd "$root/sub/dir" && PATH="$stub_bin:$PATH" "$root/al" pwd)"
  status=$?
  [ "$status" -eq 0 ]
  [ "$output" = "$root" ]

  rm -rf "$root"
}

@test "al sets CODEX_HOME when unset" {
  local root stub_bin output
  root="$(create_isolated_working_root)"
  stub_bin="$(create_stub_tools "$root")"
  cat >"$stub_bin/codex" <<'EOF'
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

@test "al does not warn when CODEX_HOME already matches repo-local" {
  local root stub_bin output status
  root="$(create_isolated_working_root)"
  stub_bin="$(create_stub_tools "$root")"
  cat >"$stub_bin/codex" <<'EOF'
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

@test "al preserves CODEX_HOME when already set" {
  local root stub_bin output
  root="$(create_isolated_working_root)"
  stub_bin="$(create_stub_tools "$root")"
  cat >"$stub_bin/codex" <<'EOF'
#!/usr/bin/env bash
printf "%s" "${CODEX_HOME:-}"
EOF
  chmod +x "$stub_bin/codex"

  output="$(cd "$root/sub/dir" && PATH="$stub_bin:$PATH" CODEX_HOME="/tmp/custom-codex" \
    "$root/.agent-layer/al" codex 2>&1)"
  status=$?
  [ "$status" -eq 0 ]
  [[ "$output" == *"warning: CODEX_HOME already set to '/tmp/custom-codex'; leaving it unchanged."* ]]
  [[ "$output" == *"/tmp/custom-codex" ]]

  rm -rf "$root"
}

@test "al fails when node is missing" {
  local root stub_bin output bash_bin
  root="$(create_isolated_working_root)"
  stub_bin="$root/stub-bin"
  bash_bin="$(command -v bash)"
  mkdir -p "$stub_bin"
  ln -s "$(command -v basename)" "$stub_bin/basename"
  ln -s "$(command -v dirname)" "$stub_bin/dirname"

  run bash -c "cd '$root/sub/dir' && PATH='$stub_bin' '$bash_bin' '$root/.agent-layer/al' pwd 2>&1"
  [ "$status" -ne 0 ]
  [[ "$output" == *"Node.js is required"* ]]

  rm -rf "$root"
}

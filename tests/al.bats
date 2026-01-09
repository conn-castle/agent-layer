#!/usr/bin/env bats

load "helpers.bash"

@test "al uses its script dir when PWD points at another working repo" {
  local root_a root_b stub_bin output
  root_a="$(create_working_root)"
  root_b="$(create_working_root)"

  ln -s "$root_a/.agentlayer/al" "$root_a/al"
  stub_bin="$(create_stub_node "$root_a")"

  output="$(cd "$root_b/sub/dir" && PATH="$stub_bin:$PATH" "$root_a/al" pwd)"
  status=$?
  [ "$status" -eq 0 ]
  [ "$output" = "$root_a" ]

  rm -rf "$root_a" "$root_b"
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

  output="$(cd "$root/sub/dir" && PATH="$stub_bin:$PATH" "$root/.agentlayer/al" codex)"
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
    "$root/.agentlayer/al" codex 2>/dev/null)"
  status=$?
  [ "$status" -eq 0 ]
  [ "$output" = "/tmp/custom-codex" ]

  rm -rf "$root"
}

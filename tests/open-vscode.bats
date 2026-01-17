#!/usr/bin/env bats

# Tests for the VS Code launcher wrapper.
# Load shared helpers for temp roots and stub binaries.
load "helpers.bash"

teardown() {
  cleanup_test_temp_dirs
}

# Test: ./al --open-vscode sets CODEX_HOME and launches the parent root
@test "al --open-vscode sets CODEX_HOME and launches the parent root" {
  local root stub_bin node_bin
  root="$(create_parent_root)"
  mkdir -p "$root/.codex"
  stub_bin="$root/stub-bin"
  node_bin="$(dirname "$(command -v node)")"
  mkdir -p "$stub_bin"

  cat >"$stub_bin/code" <<'EOF'
#!/usr/bin/env bash
echo "$CODEX_HOME|$1"
EOF
  chmod +x "$stub_bin/code"

  # Unset CODEX_HOME to test default behavior; resolve root for macOS /var vs /private/var
  local real_root
  real_root="$(cd "$root" && pwd -P)"
  CODEX_HOME= PATH="$stub_bin:$node_bin:/bin:/usr/bin" run "$root/.agent-layer/agent-layer" --open-vscode --parent-root "$root"

  [ "$status" -eq 0 ]
  [ "$output" = "$real_root/.codex|$real_root" ]

  rm -rf "$root"
}

# Test: ./al --open-vscode warns and does not override CODEX_HOME
@test "al --open-vscode warns and does not override CODEX_HOME" {
  local root stub_bin output status node_bin
  root="$(create_parent_root)"
  mkdir -p "$root/.codex" "$root/alt-codex"
  stub_bin="$root/stub-bin"
  node_bin="$(dirname "$(command -v node)")"
  mkdir -p "$stub_bin"

  cat >"$stub_bin/code" <<'EOF'
#!/usr/bin/env bash
echo "$CODEX_HOME|$1"
EOF
  chmod +x "$stub_bin/code"

  output="$(PATH="$stub_bin:$node_bin:/bin:/usr/bin" CODEX_HOME="$root/alt-codex" \
    "$root/.agent-layer/agent-layer" --open-vscode --parent-root "$root" 2>&1)"
  status=$?

  [ "$status" -eq 0 ]
  [[ "$output" == *"CODEX_HOME is set to $root/alt-codex"* ]]
  [[ "$output" == *"expected $root/.codex"* ]]
  [[ "$output" == *"$root/alt-codex|$root" ]]

  rm -rf "$root"
}

# Test: ./al --open-vscode does not auto-close Terminal
@test "al --open-vscode does not auto-close Terminal" {
  local root stub_bin marker node_bin
  root="$(create_parent_root)"
  mkdir -p "$root/.codex"
  stub_bin="$root/stub-bin"
  node_bin="$(dirname "$(command -v node)")"
  mkdir -p "$stub_bin"
  marker="$root/osascript-called"

  cat >"$stub_bin/code" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
  cat >"$stub_bin/osascript" <<'EOF'
#!/usr/bin/env bash
cat >/dev/null
if [[ -n "${OSASCRIPT_MARKER:-}" ]]; then
  echo "called" > "$OSASCRIPT_MARKER"
fi
EOF
  chmod +x "$stub_bin/code" "$stub_bin/osascript"

  TERM_PROGRAM="Apple_Terminal" OSASCRIPT_MARKER="$marker" \
    PATH="$stub_bin:$node_bin:/bin:/usr/bin" run "$root/.agent-layer/agent-layer" --open-vscode --parent-root "$root"

  [ "$status" -eq 0 ]
  [ ! -f "$marker" ]

  rm -rf "$root"
}

# Test: ./al --open-vscode fails when code CLI is missing
@test "al --open-vscode fails when code CLI is missing" {
  local root node_bin
  root="$(create_parent_root)"
  node_bin="$(dirname "$(command -v node)")"
  mkdir -p "$root/.codex"

  PATH="$node_bin:/bin:/usr/bin" run "$root/.agent-layer/agent-layer" --open-vscode --parent-root "$root"

  [ "$status" -ne 0 ]
  [[ "$output" == *"code"* ]]

  rm -rf "$root"
}

#!/usr/bin/env bats

# Tests for the VS Code launcher wrapper.
# Load shared helpers for temp roots and stub binaries.
load "helpers.bash"

# Test: open-vscode.command sets CODEX_HOME and launches the parent root
@test "open-vscode.command sets CODEX_HOME and launches the parent root" {
  local root stub_bin
  root="$(create_parent_root)"
  mkdir -p "$root/.codex"
  stub_bin="$root/stub-bin"
  mkdir -p "$stub_bin"

  cat >"$stub_bin/code" <<'EOF'
#!/usr/bin/env bash
echo "$CODEX_HOME|$1"
EOF
  chmod +x "$stub_bin/code"

  PATH="$stub_bin:/bin:/usr/bin" run "$root/.agent-layer/open-vscode.command"

  [ "$status" -eq 0 ]
  [ "$output" = "$root/.codex|$root" ]

  rm -rf "$root"
}

# Test: open-vscode.command auto-closes Terminal by default when osascript is available
@test "open-vscode.command auto-closes Terminal by default when osascript is available" {
  local root stub_bin marker
  root="$(create_parent_root)"
  mkdir -p "$root/.codex"
  stub_bin="$root/stub-bin"
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
    PATH="$stub_bin:/bin:/usr/bin" run "$root/.agent-layer/open-vscode.command"

  [ "$status" -eq 0 ]
  [ -f "$marker" ]

  rm -rf "$root"
}

# Test: open-vscode.command skips auto-close when OPEN_VSCODE_NO_CLOSE is set
@test "open-vscode.command skips auto-close when OPEN_VSCODE_NO_CLOSE is set" {
  local root stub_bin marker
  root="$(create_parent_root)"
  mkdir -p "$root/.codex"
  stub_bin="$root/stub-bin"
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

  TERM_PROGRAM="Apple_Terminal" OPEN_VSCODE_NO_CLOSE=1 OSASCRIPT_MARKER="$marker" \
    PATH="$stub_bin:/bin:/usr/bin" run "$root/.agent-layer/open-vscode.command"

  [ "$status" -eq 0 ]
  [ ! -f "$marker" ]

  rm -rf "$root"
}

# Test: open-vscode.command fails when code CLI is missing
@test "open-vscode.command fails when code CLI is missing" {
  local root
  root="$(create_parent_root)"
  mkdir -p "$root/.codex"

  PATH="/bin:/usr/bin" run "$root/.agent-layer/open-vscode.command"

  [ "$status" -ne 0 ]
  [[ "$output" == *"code"* ]]

  rm -rf "$root"
}

#!/usr/bin/env bats

# Tests for parent root resolution and temp root helpers.
# Load shared helpers for temp roots and stub binaries.
load "helpers.bash"

realpath_dir() {
  (cd "$1" && pwd -P)
}

# Spec 1: Consumer repo discovery succeeds
@test "Spec 1: Consumer repo discovery succeeds" {
  local parent_root agent_layer_root expected_parent expected_agent script
  parent_root="$(make_tmp_dir)"
  agent_layer_root="$parent_root/.agent-layer"
  create_agent_layer_root "$agent_layer_root"

  expected_parent="$(realpath_dir "$parent_root")"
  expected_agent="$(realpath_dir "$agent_layer_root")"

  script="$(
    multiline \
      "set -euo pipefail" \
      "source \"$agent_layer_root/src/lib/parent-root.sh\"" \
      "resolve_parent_root" \
      "printf \"PARENT_ROOT=%s\n\" \"\$PARENT_ROOT\"" \
      "printf \"AGENT_LAYER_ROOT=%s\n\" \"\$AGENT_LAYER_ROOT\"" \
      "printf \"TEMP_PARENT_ROOT_CREATED=%s\n\" \"\$TEMP_PARENT_ROOT_CREATED\"" \
      "printf \"IS_CONSUMER_LAYOUT=%s\n\" \"\$IS_CONSUMER_LAYOUT\""
  )"

  run bash -c "$script"
  [ "$status" -eq 0 ]
  [[ "$output" == *"PARENT_ROOT=$expected_parent"* ]]
  [[ "$output" == *"AGENT_LAYER_ROOT=$expected_agent"* ]]
  [[ "$output" == *"TEMP_PARENT_ROOT_CREATED=0"* ]]
  [[ "$output" == *"IS_CONSUMER_LAYOUT=1"* ]]

  rm -rf "$parent_root"
}

# Spec 1 Edge: Symlinks resolved correctly in discovery
@test "Spec 1 Edge: Symlinks resolved correctly in discovery" {
  local real_parent link_parent agent_layer_root expected_parent expected_agent script
  real_parent="$(make_tmp_dir)"
  agent_layer_root="$real_parent/.agent-layer"
  create_agent_layer_root "$agent_layer_root"

  link_parent="$(make_tmp_dir)"
  rm -rf "$link_parent"
  ln -s "$real_parent" "$link_parent"

  expected_parent="$(realpath_dir "$real_parent")"
  expected_agent="$(realpath_dir "$agent_layer_root")"

  script="$(
    multiline \
      "set -euo pipefail" \
      "source \"$link_parent/.agent-layer/src/lib/parent-root.sh\"" \
      "resolve_parent_root" \
      "printf \"PARENT_ROOT=%s\n\" \"\$PARENT_ROOT\"" \
      "printf \"AGENT_LAYER_ROOT=%s\n\" \"\$AGENT_LAYER_ROOT\""
  )"

  run bash -c "$script"
  [ "$status" -eq 0 ]
  [[ "$output" == *"PARENT_ROOT=$expected_parent"* ]]
  [[ "$output" == *"AGENT_LAYER_ROOT=$expected_agent"* ]]

  rm -rf "$real_parent" "$link_parent"
}

# Spec 1 Error: Discovery consistency check fails
@test "Spec 1 Error: Discovery consistency check fails" {
  local parent_root agent_layer_root script expected expected_parent expected_agent expected_configured
  parent_root="$(make_tmp_dir)"
  agent_layer_root="$parent_root/.agent-layer"
  create_agent_layer_root "$agent_layer_root"
  mkdir -p "$parent_root/not-agent-layer"

  expected_parent="$(realpath_dir "$parent_root")"
  expected_agent="$(realpath_dir "$parent_root/not-agent-layer")"
  expected_configured="$(realpath_dir "$agent_layer_root")"

  script="$(
    multiline \
      "source \"$agent_layer_root/src/lib/parent-root.sh\"" \
      "AGENT_LAYER_ROOT=\"$parent_root/not-agent-layer\"" \
      "resolve_discovered_parent_root"
  )"

  run bash -c "$script"
  [ "$status" -ne 0 ]

  expected="$(
    multiline \
      "ERROR: Parent root .agent-layer/ does not match script location." \
      "" \
      "Resolved script location: $expected_agent" \
      "Resolved parent config:   $expected_configured" \
      "" \
      "These must point to the same location. You are running scripts from one" \
      "agent-layer installation but trying to configure a different one." \
      "" \
      "Fix:" \
      "  - Use scripts from ${expected_parent}/.agent-layer/" \
      "  - Or adjust --parent-root to match script location"
  )"
  [[ "$output" == "$expected" ]]

  rm -rf "$parent_root"
}

# Spec 1 Edge: Parent root may be read-only
@test "Spec 1 Edge: Parent root may be read-only" {
  local parent_root agent_layer_root script
  parent_root="$(make_tmp_dir)"
  agent_layer_root="$parent_root/.agent-layer"
  create_agent_layer_root "$agent_layer_root"

  chmod 555 "$parent_root"

  script="$(
    multiline \
      "set -euo pipefail" \
      "source \"$agent_layer_root/src/lib/parent-root.sh\"" \
      "resolve_parent_root" \
      "printf \"PARENT_ROOT=%s\n\" \"\$PARENT_ROOT\""
  )"

  run bash -c "$script"
  [ "$status" -eq 0 ]

  chmod 755 "$parent_root"
  rm -rf "$parent_root"
}

# Spec 1 Error: Agent layer root is filesystem root
@test "Spec 1 Error: Agent layer root is filesystem root" {
  local script expected
  script="$(
    multiline \
      'source "$AGENT_LAYER_ROOT/src/lib/parent-root.sh"' \
      'ROOTS_AGENT_LAYER_ROOT_OVERRIDE="/" resolve_parent_root'
  )"

  run bash -c "$script"
  [ "$status" -ne 0 ]

  expected="$(
    multiline \
      'ERROR: Agent layer root is the filesystem root (/).' \
      '' \
      'This is invalid. The agent layer root must be a directory (e.g., .agent-layer)' \
      'inside a parent repo.' \
      '' \
      'Fix:' \
      '  - Reinstall agent-layer in a valid subdirectory'
  )"
  [[ "$output" == "$expected" ]]
}

# Spec 1 Edge: Parent root may be filesystem root when agent layer is /.agent-layer
@test "Spec 1 Edge: Parent root may be filesystem root" {
  if [[ ! -w "/" ]]; then
    skip "Filesystem root is not writable"
  fi

  local created="0" script
  if [[ ! -d "/.agent-layer" ]]; then
    mkdir "/.agent-layer"
    created="1"
  fi

  script="$(
    multiline \
      'source "$AGENT_LAYER_ROOT/src/lib/parent-root.sh"' \
      'ROOTS_AGENT_LAYER_ROOT_OVERRIDE="/.agent-layer" resolve_parent_root' \
      'printf "PARENT_ROOT=%s\n" "$PARENT_ROOT"'
  )"

  run bash -c "$script"
  [ "$status" -eq 0 ]
  [ "$output" = "PARENT_ROOT=/" ]

  if [[ "$created" == "1" ]]; then
    rmdir "/.agent-layer"
  fi
}

# Spec Edge: Agent layer root override must exist
@test "Spec Edge: Agent layer root override must exist" {
  local script expected
  script="$(
    multiline \
      'source "$AGENT_LAYER_ROOT/src/lib/parent-root.sh"' \
      'ROOTS_AGENT_LAYER_ROOT_OVERRIDE="/path/does-not-exist" resolve_parent_root'
  )"

  run bash -c "$script"
  [ "$status" -ne 0 ]

  expected="ERROR: Agent layer root override does not exist: /path/does-not-exist"
  [[ "$output" == "$expected" ]]
}

# Spec 2: Explicit parent root via --parent-root resolves relative paths
@test "Spec 2: Explicit parent root via --parent-root resolves relative paths" {
  local base parent_root agent_layer_root expected_parent script
  base="$(make_tmp_dir)"
  parent_root="$base/parent-root"
  agent_layer_root="$parent_root/.agent-layer"
  mkdir -p "$parent_root"
  create_agent_layer_root "$agent_layer_root"

  expected_parent="$(realpath_dir "$parent_root")"

  script="$(
    multiline \
      "set -euo pipefail" \
      "cd \"$base\"" \
      "source \"$agent_layer_root/src/lib/parent-root.sh\"" \
      "ROOTS_PARENT_ROOT=\"parent-root\" resolve_parent_root" \
      "printf \"PARENT_ROOT=%s\n\" \"\$PARENT_ROOT\""
  )"

  run bash -c "$script"
  [ "$status" -eq 0 ]
  [[ "$output" == *"PARENT_ROOT=$expected_parent"* ]]

  rm -rf "$base"
}

# Spec 2 Edge: Relative parent root from filesystem root
@test "Spec 2 Edge: Relative parent root from filesystem root" {
  local base parent_root agent_layer_root expected_parent relative_path script
  base="$(make_tmp_dir)"
  parent_root="$base/parent-root"
  agent_layer_root="$parent_root/.agent-layer"
  mkdir -p "$parent_root"
  create_agent_layer_root "$agent_layer_root"

  expected_parent="$(realpath_dir "$parent_root")"
  relative_path="${expected_parent#/}"

  script="$(
    multiline \
      "set -euo pipefail" \
      "cd /" \
      "source \"$agent_layer_root/src/lib/parent-root.sh\"" \
      "ROOTS_PARENT_ROOT=\"$relative_path\" resolve_parent_root" \
      "printf \"PARENT_ROOT=%s\n\" \"\$PARENT_ROOT\""
  )"

  run bash -c "$script"
  [ "$status" -eq 0 ]
  [[ "$output" == *"PARENT_ROOT=$expected_parent"* ]]

  rm -rf "$base"
}

# Spec 2 Edge: Explicit parent root supports trailing slashes
@test "Spec 2 Edge: Explicit parent root supports trailing slashes" {
  local base parent_root agent_layer_root expected_parent script
  base="$(make_tmp_dir)"
  parent_root="$base/parent-root"
  agent_layer_root="$parent_root/.agent-layer"
  mkdir -p "$parent_root"
  create_agent_layer_root "$agent_layer_root"

  expected_parent="$(realpath_dir "$parent_root")"

  script="$(
    multiline \
      "set -euo pipefail" \
      "source \"$agent_layer_root/src/lib/parent-root.sh\"" \
      "ROOTS_PARENT_ROOT=\"$parent_root/\" resolve_parent_root" \
      "printf \"PARENT_ROOT=%s\n\" \"\$PARENT_ROOT\""
  )"

  run bash -c "$script"
  [ "$status" -eq 0 ]
  [[ "$output" == *"PARENT_ROOT=$expected_parent"* ]]

  rm -rf "$base"
}

# Spec 2 Edge: Explicit parent root accepts symlink paths
@test "Spec 2 Edge: Explicit parent root accepts symlink paths" {
  local base real_parent link_parent agent_layer_root expected_parent script
  base="$(make_tmp_dir)"
  real_parent="$base/real-parent"
  link_parent="$base/link-parent"
  agent_layer_root="$real_parent/.agent-layer"
  mkdir -p "$real_parent"
  create_agent_layer_root "$agent_layer_root"
  ln -s "$real_parent" "$link_parent"

  expected_parent="$(realpath_dir "$real_parent")"

  script="$(
    multiline \
      "set -euo pipefail" \
      "source \"$agent_layer_root/src/lib/parent-root.sh\"" \
      "ROOTS_PARENT_ROOT=\"$link_parent\" resolve_parent_root" \
      "printf \"PARENT_ROOT=%s\n\" \"\$PARENT_ROOT\""
  )"

  run bash -c "$script"
  [ "$status" -eq 0 ]
  [[ "$output" == *"PARENT_ROOT=$expected_parent"* ]]

  rm -rf "$base"
}

# Spec 2: PARENT_ROOT from .env resolves relative to agent layer root
@test "Spec 2: PARENT_ROOT from .env resolves relative to agent layer root" {
  local parent_root agent_layer_root expected_parent script
  parent_root="$(make_tmp_dir)"
  agent_layer_root="$parent_root/.agent-layer"
  create_agent_layer_root "$agent_layer_root"

  printf "# comment\nPARENT_ROOT=..\nSECRET_SHOULD_NOT_LEAK=1\n" > "$agent_layer_root/.env"

  expected_parent="$(realpath_dir "$parent_root")"

  script="$(
    multiline \
      "set -euo pipefail" \
      "source \"$agent_layer_root/src/lib/parent-root.sh\"" \
      "resolve_parent_root" \
      "printf \"PARENT_ROOT=%s\n\" \"\$PARENT_ROOT\"" \
      "printf \"SECRET=%s\n\" \"\${SECRET_SHOULD_NOT_LEAK:-}\""
  )"

  run bash -c "$script"
  [ "$status" -eq 0 ]
  [[ "$output" == *"PARENT_ROOT=$expected_parent"* ]]
  [[ "$output" == *"SECRET="* ]]
  [[ "$output" != *"SECRET=1"* ]]

  rm -rf "$parent_root"
}

# Spec 2: PARENT_ROOT from .env supports quoted values
@test "Spec 2: PARENT_ROOT from .env supports quoted values" {
  local parent_root agent_layer_root expected_parent script
  parent_root="$(make_tmp_dir)"
  agent_layer_root="$parent_root/.agent-layer"
  create_agent_layer_root "$agent_layer_root"

  printf 'PARENT_ROOT=".."\n' > "$agent_layer_root/.env"

  expected_parent="$(realpath_dir "$parent_root")"

  script="$(
    multiline \
      "set -euo pipefail" \
      "source \"$agent_layer_root/src/lib/parent-root.sh\"" \
      "resolve_parent_root" \
      "printf \"PARENT_ROOT=%s\n\" \"\$PARENT_ROOT\""
  )"

  run bash -c "$script"
  [ "$status" -eq 0 ]
  [[ "$output" == *"PARENT_ROOT=$expected_parent"* ]]

  rm -rf "$parent_root"
}

# Spec 2 Error: Parent root path does not exist
@test "Spec 2 Error: Parent root path does not exist" {
  local parent_root agent_layer_root missing expected script
  parent_root="$(make_tmp_dir)"
  agent_layer_root="$parent_root/.agent-layer"
  create_agent_layer_root "$agent_layer_root"

  missing="$parent_root/does-not-exist"

  script="$(
    multiline \
      "source \"$agent_layer_root/src/lib/parent-root.sh\"" \
      "ROOTS_PARENT_ROOT=\"$missing\" resolve_parent_root"
  )"

  run bash -c "$script"
  [ "$status" -ne 0 ]

  expected="$(
    multiline \
      "ERROR: Parent root path does not exist: $missing" \
      "" \
      "Source: --parent-root flag" \
      "" \
      "Fix:" \
      "  - Create the directory, or" \
      "  - Use a different path, or" \
      "  - Use temp parent root for testing: --temp-parent-root"
  )"
  [[ "$output" == "$expected" ]]

  rm -rf "$parent_root"
}

# Spec 2 Error: Parent root path does not exist (from .env)
@test "Spec 2 Error: Parent root path does not exist (from .env)" {
  local parent_root agent_layer_root missing expected script
  parent_root="$(make_tmp_dir)"
  agent_layer_root="$parent_root/.agent-layer"
  create_agent_layer_root "$agent_layer_root"

  missing="$parent_root/does-not-exist"
  printf "PARENT_ROOT=$missing\n" > "$agent_layer_root/.env"

  script="$(
    multiline \
      "source \"$agent_layer_root/src/lib/parent-root.sh\"" \
      "resolve_parent_root"
  )"

  run bash -c "$script"
  [ "$status" -ne 0 ]

  expected="$(
    multiline \
      "ERROR: Parent root path does not exist: $missing" \
      "" \
      "Source: PARENT_ROOT in .env" \
      "" \
      "Fix:" \
      "  - Create the directory, or" \
      "  - Use a different path, or" \
      "  - Use temp parent root for testing: --temp-parent-root"
  )"
  [[ "$output" == "$expected" ]]

  rm -rf "$parent_root"
}

# Spec 2 Error: Parent root missing .agent-layer
@test "Spec 2 Error: Parent root missing .agent-layer" {
  local parent_root agent_layer_root missing_agent expected script
  parent_root="$(make_tmp_dir)"
  agent_layer_root="$parent_root/.agent-layer"
  create_agent_layer_root "$agent_layer_root"

  missing_agent="$(make_tmp_dir)"

  script="$(
    multiline \
      "source \"$agent_layer_root/src/lib/parent-root.sh\"" \
      "ROOTS_PARENT_ROOT=\"$missing_agent\" resolve_parent_root"
  )"

  run bash -c "$script"
  [ "$status" -ne 0 ]

  expected="$(
    multiline \
      "ERROR: Parent root must contain .agent-layer/ (dir or symlink): $(realpath_dir \"$missing_agent\")" \
      "" \
      "Found directory but no .agent-layer/ inside." \
      "" \
      "Fix:" \
      "  - Install agent-layer in that directory" \
      "  - Use a different path that contains .agent-layer/" \
      "  - Use temp parent root for testing: --temp-parent-root"
  )"
  [[ "$output" == "$expected" ]]

  rm -rf "$parent_root" "$missing_agent"
}

# Spec 2 Error: Parent root .agent-layer is a file
@test "Spec 2 Error: Parent root .agent-layer is a file" {
  local parent_root agent_layer_root file_parent expected script
  parent_root="$(make_tmp_dir)"
  agent_layer_root="$parent_root/.agent-layer"
  create_agent_layer_root "$agent_layer_root"

  file_parent="$(make_tmp_dir)"
  printf "not-a-dir\n" > "$file_parent/.agent-layer"

  script="$(
    multiline \
      "source \"$agent_layer_root/src/lib/parent-root.sh\"" \
      "ROOTS_PARENT_ROOT=\"$file_parent\" resolve_parent_root"
  )"

  run bash -c "$script"
  [ "$status" -ne 0 ]

  expected="$(
    multiline \
      "ERROR: Parent root must contain .agent-layer/ (dir or symlink): $(realpath_dir \"$file_parent\")" \
      "" \
      "Found directory but no .agent-layer/ inside." \
      "" \
      "Fix:" \
      "  - Install agent-layer in that directory" \
      "  - Use a different path that contains .agent-layer/" \
      "  - Use temp parent root for testing: --temp-parent-root"
  )"
  [[ "$output" == "$expected" ]]

  rm -rf "$parent_root" "$file_parent"
}

# Spec 2 Error: Parent root consistency check fails
@test "Spec 2 Error: Parent root consistency check fails" {
  local agent_layer_root real_parent fake_parent expected script
  real_parent="$(make_tmp_dir)"
  agent_layer_root="$real_parent/.agent-layer"
  create_agent_layer_root "$agent_layer_root"

  fake_parent="$(make_tmp_dir)"
  mkdir -p "$fake_parent/.agent-layer"

  script="$(
    multiline \
      "source \"$agent_layer_root/src/lib/parent-root.sh\"" \
      "ROOTS_PARENT_ROOT=\"$fake_parent\" resolve_parent_root"
  )"

  run bash -c "$script"
  [ "$status" -ne 0 ]

  expected="$(
    multiline \
      "ERROR: Parent root .agent-layer/ does not match script location." \
      "" \
      "Resolved script location: $(realpath_dir \"$agent_layer_root\")" \
      "Resolved parent config:   $(realpath_dir \"$fake_parent/.agent-layer\")" \
      "" \
      "These must point to the same location. You are running scripts from one" \
      "agent-layer installation but trying to configure a different one." \
      "" \
      "Fix:" \
      "  - Use scripts from $(realpath_dir \"$fake_parent\")/.agent-layer/" \
      "  - Or adjust --parent-root to match script location"
  )"
  [[ "$output" == "$expected" ]]

  rm -rf "$real_parent" "$fake_parent"
}

# Spec 2 Error: Malformed .env PARENT_ROOT
@test "Spec 2 Error: Malformed .env PARENT_ROOT" {
  local parent_root agent_layer_root expected script
  parent_root="$(make_tmp_dir)"
  agent_layer_root="$parent_root/.agent-layer"
  create_agent_layer_root "$agent_layer_root"

  printf "PARENT_ROOT=\n" > "$agent_layer_root/.env"

  script="$(
    multiline \
      "source \"$agent_layer_root/src/lib/parent-root.sh\"" \
      "resolve_parent_root"
  )"

  run bash -c "$script"
  [ "$status" -ne 0 ]

  expected="$(
    multiline \
      "ERROR: Invalid PARENT_ROOT entry in $agent_layer_root/.env" \
      "" \
      "Line: PARENT_ROOT=" \
      "" \
      "Fix:" \
      "  - Use PARENT_ROOT=<path>"
  )"
  [[ "$output" == "$expected" ]]

  rm -rf "$parent_root"
}

# Spec 2 Error: Malformed .env PARENT_ROOT (spaces around '=')
@test "Spec 2 Error: Malformed .env PARENT_ROOT with spaces" {
  local parent_root agent_layer_root expected script
  parent_root="$(make_tmp_dir)"
  agent_layer_root="$parent_root/.agent-layer"
  create_agent_layer_root "$agent_layer_root"

  printf "PARENT_ROOT = /tmp\n" > "$agent_layer_root/.env"

  script="$(
    multiline \
      "source \"$agent_layer_root/src/lib/parent-root.sh\"" \
      "resolve_parent_root"
  )"

  run bash -c "$script"
  [ "$status" -ne 0 ]

  expected="$(
    multiline \
      "ERROR: Invalid PARENT_ROOT entry in $agent_layer_root/.env" \
      "" \
      "Line: PARENT_ROOT = /tmp" \
      "" \
      "Fix:" \
      "  - Use PARENT_ROOT=<path>" \
      "  - Use simple KEY=value pairs (no spaces around '=')"
  )"
  [[ "$output" == "$expected" ]]

  rm -rf "$parent_root"
}

# Spec 2 Error: Malformed .env PARENT_ROOT (unmatched quotes)
@test "Spec 2 Error: Malformed .env PARENT_ROOT with unmatched quotes" {
  local parent_root agent_layer_root expected script
  parent_root="$(make_tmp_dir)"
  agent_layer_root="$parent_root/.agent-layer"
  create_agent_layer_root "$agent_layer_root"

  printf 'PARENT_ROOT="/tmp\n' > "$agent_layer_root/.env"

  script="$(
    multiline \
      "source \"$agent_layer_root/src/lib/parent-root.sh\"" \
      "resolve_parent_root"
  )"

  run bash -c "$script"
  [ "$status" -ne 0 ]

  expected="$(
    multiline \
      "ERROR: Invalid PARENT_ROOT entry in $agent_layer_root/.env" \
      "" \
      "Line: PARENT_ROOT=\"/tmp" \
      "" \
      "Fix:" \
      "  - Remove unmatched quotes" \
      "  - Use simple KEY=value pairs"
  )"
  [[ "$output" == "$expected" ]]

  rm -rf "$parent_root"
}

# Spec 2 Error: Duplicate PARENT_ROOT entries
@test "Spec 2 Error: Duplicate PARENT_ROOT entries" {
  local parent_root agent_layer_root expected script
  parent_root="$(make_tmp_dir)"
  agent_layer_root="$parent_root/.agent-layer"
  create_agent_layer_root "$agent_layer_root"

  cat > "$agent_layer_root/.env" << 'EOF'
PARENT_ROOT=/one
PARENT_ROOT=/two
EOF

  script="$(
    multiline \
      "source \"$agent_layer_root/src/lib/parent-root.sh\"" \
      "resolve_parent_root"
  )"

  run bash -c "$script"
  [ "$status" -ne 0 ]

  expected="$(
    multiline \
      "ERROR: Multiple PARENT_ROOT entries found in $agent_layer_root/.env" \
      "" \
      "Fix:" \
      "  - Keep only one PARENT_ROOT entry"
  )"
  [[ "$output" == "$expected" ]]

  rm -rf "$parent_root"
}

# Spec 2: .env without PARENT_ROOT is ignored
@test "Spec 2: .env without PARENT_ROOT is ignored" {
  local parent_root agent_layer_root script expected
  parent_root="$(make_tmp_dir)"
  agent_layer_root="$parent_root/.agent-layer"
  create_agent_layer_root "$agent_layer_root"

  printf "OTHER_VAR=1\n" > "$agent_layer_root/.env"

  expected="$(realpath_dir "$parent_root")"

  script="$(
    multiline \
      "set -euo pipefail" \
      "source \"$agent_layer_root/src/lib/parent-root.sh\"" \
      "resolve_parent_root" \
      "printf \"PARENT_ROOT=%s\n\" \"\$PARENT_ROOT\""
  )"

  run bash -c "$script"
  [ "$status" -eq 0 ]
  [[ "$output" == *"PARENT_ROOT=$expected"* ]]

  rm -rf "$parent_root"
}

# Spec Conflict: --parent-root and --temp-parent-root together
@test "Spec Conflict: --parent-root and --temp-parent-root together" {
  local parent_root agent_layer_root expected script
  parent_root="$(make_tmp_dir)"
  agent_layer_root="$parent_root/.agent-layer"
  create_agent_layer_root "$agent_layer_root"

  script="$(
    multiline \
      "source \"$agent_layer_root/src/lib/parent-root.sh\"" \
      "ROOTS_PARENT_ROOT=\"$parent_root\" ROOTS_USE_TEMP_PARENT_ROOT=1 resolve_parent_root"
  )"

  run bash -c "$script"
  [ "$status" -ne 0 ]

  expected="$(
    multiline \
      'ERROR: Conflicting flags: --parent-root and --temp-parent-root' \
      '' \
      'You provided both flags but they are mutually exclusive.' \
      'Choose one:' \
      '  - Use --parent-root <path> for explicit parent root' \
      '  - Use --temp-parent-root to create temporary parent root'
  )"
  [[ "$output" == "$expected" ]]

  rm -rf "$parent_root"
}

# Spec 3 Error: Missing temp-parent-root helper
@test "Spec 3 Error: Missing temp-parent-root helper" {
  local parent_root agent_layer_root expected script
  parent_root="$(make_tmp_dir)"
  agent_layer_root="$parent_root/.agent-layer"
  create_agent_layer_root "$agent_layer_root"

  script="$(
    multiline \
      "source \"$agent_layer_root/src/lib/parent-root.sh\"" \
      "TEMP_PARENT_ROOT_HELPER=\"$agent_layer_root/missing-temp-parent-root.sh\" ROOTS_USE_TEMP_PARENT_ROOT=1 resolve_parent_root"
  )"

  run bash -c "$script"
  [ "$status" -ne 0 ]

  expected="ERROR: Missing src/lib/temp-parent-root.sh (expected in the agent-layer root)."
  [[ "$output" == "$expected" ]]

  rm -rf "$parent_root"
}

# Spec 3 Edge: Temp parent root requires agent layer root
@test "Spec 3 Edge: Temp parent root requires agent layer root" {
  local script
  script="$(
    multiline \
      'source "$AGENT_LAYER_ROOT/src/lib/temp-parent-root.sh"' \
      'status=0' \
      'make_temp_parent_root "" || status=$?' \
      'printf "status=%s\n" "$status"' \
      'exit "$status"'
  )"

  run bash -c "$script"
  [ "$status" -eq 2 ]
  [ "$output" = "status=2" ]
}

# Spec 3: Temp parent root creates symlink and flags
@test "Spec 3: Temp parent root creates symlink and flags" {
  local parent_root agent_layer_root script temp_root
  parent_root="$(make_tmp_dir)"
  agent_layer_root="$parent_root/.agent-layer"
  create_agent_layer_root "$agent_layer_root"

  script="$(
    multiline \
      "set -euo pipefail" \
      "source \"$agent_layer_root/src/lib/parent-root.sh\"" \
      "ROOTS_USE_TEMP_PARENT_ROOT=1 resolve_parent_root" \
      "printf \"PARENT_ROOT=%s\n\" \"\$PARENT_ROOT\"" \
      "printf \"TEMP_PARENT_ROOT_CREATED=%s\n\" \"\$TEMP_PARENT_ROOT_CREATED\""
  )"

  run bash -c "$script"
  [ "$status" -eq 0 ]
  [[ "$output" == *"TEMP_PARENT_ROOT_CREATED=1"* ]]

  temp_root="$(printf "%s\n" "$output" | sed -n "s/^PARENT_ROOT=//p")"
  [ -d "$temp_root" ]
  [ -L "$temp_root/.agent-layer" ]
  [ "$(cd "$temp_root/.agent-layer" && pwd -P)" = "$(realpath_dir "$agent_layer_root")" ]

  rm -rf "$temp_root" "$parent_root"
}

# Spec 3 Edge: Temp parent root works when TMPDIR is unset
@test "Spec 3 Edge: Temp parent root works when TMPDIR is unset" {
  local parent_root agent_layer_root script temp_root
  parent_root="$(make_tmp_dir)"
  agent_layer_root="$parent_root/.agent-layer"
  create_agent_layer_root "$agent_layer_root"

  script="$(
    multiline \
      "set -euo pipefail" \
      "source \"$agent_layer_root/src/lib/parent-root.sh\"" \
      "TMPDIR=\"\" ROOTS_USE_TEMP_PARENT_ROOT=1 resolve_parent_root" \
      "printf \"PARENT_ROOT=%s\n\" \"\$PARENT_ROOT\"" \
      "printf \"TEMP_PARENT_ROOT_CREATED=%s\n\" \"\$TEMP_PARENT_ROOT_CREATED\""
  )"

  run bash -c "$script"
  [ "$status" -eq 0 ]
  [[ "$output" == *"TEMP_PARENT_ROOT_CREATED=1"* ]]

  temp_root="$(printf "%s\n" "$output" | sed -n "s/^PARENT_ROOT=//p")"
  [ -d "$temp_root" ]

  rm -rf "$temp_root" "$parent_root"
}

# Spec Edge: Multiple temp parent roots do not collide
@test "Spec Edge: Multiple temp parent roots do not collide" {
  local parent_root agent_layer_root script root_one root_two
  parent_root="$(make_tmp_dir)"
  agent_layer_root="$parent_root/.agent-layer"
  create_agent_layer_root "$agent_layer_root"

  script="$(
    multiline \
      "set -euo pipefail" \
      "source \"$agent_layer_root/src/lib/parent-root.sh\"" \
      "ROOTS_USE_TEMP_PARENT_ROOT=1 resolve_parent_root" \
      "printf \"%s\\n\" \"\$PARENT_ROOT\""
  )"

  root_one="$(bash -c "$script")"
  root_two="$(bash -c "$script")"

  [ "$root_one" != "$root_two" ]

  rm -rf "$root_one" "$root_two" "$parent_root"
}

# Spec 3: Temp parent root falls back when TMPDIR is invalid
@test "Spec 3: Temp parent root falls back when TMPDIR is invalid" {
  local parent_root agent_layer_root temp_root script
  parent_root="$(make_tmp_dir)"
  agent_layer_root="$parent_root/.agent-layer"
  create_agent_layer_root "$agent_layer_root"

  script="$(
    multiline \
      "set -euo pipefail" \
      "source \"$agent_layer_root/src/lib/parent-root.sh\"" \
      "TMPDIR=\"$parent_root/does-not-exist\" ROOTS_USE_TEMP_PARENT_ROOT=1 resolve_parent_root" \
      "printf \"PARENT_ROOT=%s\n\" \"\$PARENT_ROOT\""
  )"

  run bash -c "$script"
  [ "$status" -eq 0 ]

  temp_root="$(printf "%s\n" "$output" | sed -n "s/^PARENT_ROOT=//p")"
  [[ "$temp_root" == "$(realpath_dir "$agent_layer_root")/tmp/agent-layer-temp-parent-root."* ]]

  rm -rf "$temp_root" "$parent_root"
}

# Spec 3 Edge: Temp parent root falls back when TMPDIR is not writable
@test "Spec 3 Edge: Temp parent root falls back when TMPDIR is not writable" {
  local parent_root agent_layer_root temp_root script unwritable
  parent_root="$(make_tmp_dir)"
  agent_layer_root="$parent_root/.agent-layer"
  create_agent_layer_root "$agent_layer_root"

  unwritable="$parent_root/unwritable"
  mkdir -p "$unwritable"
  chmod 555 "$unwritable"

  script="$(
    multiline \
      "set -euo pipefail" \
      "source \"$agent_layer_root/src/lib/parent-root.sh\"" \
      "TMPDIR=\"$unwritable\" ROOTS_USE_TEMP_PARENT_ROOT=1 resolve_parent_root" \
      "printf \"PARENT_ROOT=%s\n\" \"\$PARENT_ROOT\""
  )"

  run bash -c "$script"
  [ "$status" -eq 0 ]

  temp_root="$(printf "%s\n" "$output" | sed -n "s/^PARENT_ROOT=//p")"
  [[ "$temp_root" == "$(realpath_dir "$agent_layer_root")/tmp/agent-layer-temp-parent-root."* ]]

  chmod 755 "$unwritable"
  rm -rf "$temp_root" "$parent_root"
}

# Spec 3 Edge: Temp parent root without mktemp uses manual path
@test "Spec 3 Edge: Temp parent root without mktemp uses manual path" {
  local parent_root agent_layer_root stub_bin temp_root script
  parent_root="$(make_tmp_dir)"
  agent_layer_root="$parent_root/.agent-layer"
  create_agent_layer_root "$agent_layer_root"

  stub_bin="$parent_root/stub-bin"
  mkdir -p "$stub_bin"
  cat > "$stub_bin/ln" << 'EOF'
#!/bin/bash
/bin/ln "$@"
EOF
  cat > "$stub_bin/basename" << 'EOF'
#!/bin/bash
/usr/bin/basename "$@"
EOF
  cat > "$stub_bin/mkdir" << 'EOF'
#!/bin/bash
/bin/mkdir "$@"
EOF
  cat > "$stub_bin/rm" << 'EOF'
#!/bin/bash
/bin/rm "$@"
EOF
  chmod +x "$stub_bin/ln" "$stub_bin/mkdir" "$stub_bin/basename" "$stub_bin/rm"

  script="$(
    multiline \
      "set -euo pipefail" \
      "source \"$agent_layer_root/src/lib/parent-root.sh\"" \
      "PATH=\"$stub_bin\" ROOTS_USE_TEMP_PARENT_ROOT=1 resolve_parent_root" \
      "printf \"PARENT_ROOT=%s\\n\" \"\$PARENT_ROOT\""
  )"

  run bash -c "$script"
  [ "$status" -eq 0 ]

  temp_root="$(printf "%s\n" "$output" | sed -n "s/^PARENT_ROOT=//p")"
  [[ "$temp_root" == "$(realpath_dir "$agent_layer_root")/tmp/agent-layer-temp-parent-root."* ]]
  [ -d "$temp_root" ]

  rm -rf "$temp_root" "$parent_root"
}

# Spec 3 Error: Temp parent root manual mkdir fails
@test "Spec 3 Error: Temp parent root manual mkdir fails" {
  local parent_root agent_layer_root stub_bin script
  parent_root="$(make_tmp_dir)"
  agent_layer_root="$parent_root/.agent-layer"
  create_agent_layer_root "$agent_layer_root"

  stub_bin="$parent_root/stub-bin"
  mkdir -p "$stub_bin"
  cat > "$stub_bin/mkdir" << 'EOF'
#!/usr/bin/env bash
exit 1
EOF
  chmod +x "$stub_bin/mkdir"

  script="$(
    multiline \
      "source \"$agent_layer_root/src/lib/temp-parent-root.sh\"" \
      "status=0" \
      "PATH=\"$stub_bin\" make_temp_parent_root \"$agent_layer_root\" || status=\$?" \
      "printf \"status=%s\n\" \"\$status\"" \
      "exit \"\$status\""
  )"

  run bash -c "$script"
  [ "$status" -eq 2 ]
  [ "$output" = "status=2" ]

  rm -rf "$parent_root"
}

# Spec 3 Error: Temp parent root creation fails
@test "Spec 3 Error: Temp parent root creation fails" {
  local parent_root agent_layer_root stub_bin expected script
  parent_root="$(make_tmp_dir)"
  agent_layer_root="$parent_root/.agent-layer"
  create_agent_layer_root "$agent_layer_root"

  stub_bin="$parent_root/stub-bin"
  mkdir -p "$stub_bin"
  cat > "$stub_bin/mktemp" << 'EOF'
#!/usr/bin/env bash
exit 1
EOF
  chmod +x "$stub_bin/mktemp"

  script="$(
    multiline \
      "source \"$agent_layer_root/src/lib/parent-root.sh\"" \
      "PATH=\"$stub_bin:/usr/bin:/bin\" ROOTS_USE_TEMP_PARENT_ROOT=1 resolve_parent_root"
  )"

  run bash -c "$script"
  [ "$status" -ne 0 ]

  expected="$(
    multiline \
      "ERROR: Failed to create temporary parent root directory." \
      "" \
      "Attempted:" \
      "  1. ${TMPDIR:-/tmp}/agent-layer-temp-parent-root.XXXXXX" \
      "  2. $(realpath_dir \"$agent_layer_root\")/tmp/agent-layer-temp-parent-root.XXXXXX" \
      "  3. Manual creation (if mktemp unavailable)" \
      "" \
      "Possible causes:" \
      "  - Disk full (check: df -h)" \
      "  - No write permission to temp directories" \
      "  - \$TMPDIR points to non-existent location" \
      "" \
      "Fix:" \
      "  - Free disk space" \
      "  - Set TMPDIR to writable location: export TMPDIR=/writable/path" \
      "  - Use explicit parent root instead: --parent-root <path>"
  )"
  [[ "$output" == "$expected" ]]

  rm -rf "$parent_root"
}

# Spec 3 Error: Temp parent root symlink creation fails
@test "Spec 3 Error: Temp parent root symlink creation fails" {
  local parent_root agent_layer_root stub_bin script expected temp_root
  parent_root="$(make_tmp_dir)"
  agent_layer_root="$parent_root/.agent-layer"
  create_agent_layer_root "$agent_layer_root"

  stub_bin="$parent_root/stub-bin"
  mkdir -p "$stub_bin"
  temp_root="$(realpath_dir "$parent_root")/temp-parent-root"
  cat > "$stub_bin/mktemp" << EOF
#!/usr/bin/env bash
mkdir -p "$temp_root"
printf "%s" "$temp_root"
EOF
  cat > "$stub_bin/ln" << 'EOF'
#!/usr/bin/env bash
exit 1
EOF
  chmod +x "$stub_bin/mktemp" "$stub_bin/ln"

  script="$(
    multiline \
      "source \"$agent_layer_root/src/lib/parent-root.sh\"" \
      "PATH=\"$stub_bin:/usr/bin:/bin\" ROOTS_USE_TEMP_PARENT_ROOT=1 resolve_parent_root"
  )"

  run bash -c "$script"
  [ "$status" -ne 0 ]
  expected="$(
    multiline \
      "ERROR: Failed to create .agent-layer symlink in temp parent root." \
      "" \
      "Path: $temp_root/.agent-layer -> $(realpath_dir \"$agent_layer_root\")" \
      "" \
      "Possible causes:" \
      "  - Filesystem doesn't support symlinks (e.g., FAT32, some network mounts)" \
      "  - Path already exists at $temp_root/.agent-layer" \
      "  - Permission denied" \
      "" \
      "Fix:" \
      "  - Use filesystem that supports symlinks (ext4, APFS, HFS+)" \
      "  - Or use explicit parent root: --parent-root <path>"
  )"
  [[ "$output" == "$expected" ]]

  rm -rf "$parent_root"
}

# Spec 3 Edge: PARENT_ROOT_KEEP_TEMP=1 retains temp root
@test "Spec 3 Edge: PARENT_ROOT_KEEP_TEMP=1 retains temp root" {
  local parent_root agent_layer_root temp_root script
  parent_root="$(make_tmp_dir)"
  agent_layer_root="$parent_root/.agent-layer"
  create_agent_layer_root "$agent_layer_root"

  script="$(
    multiline \
      "set -euo pipefail" \
      "source \"$agent_layer_root/src/lib/parent-root.sh\"" \
      "ROOTS_USE_TEMP_PARENT_ROOT=1 resolve_parent_root" \
      "trap '[[ \"\${PARENT_ROOT_KEEP_TEMP:-0}\" == \"1\" ]] || rm -rf \"\$PARENT_ROOT\"' EXIT INT TERM" \
      "printf \"%s\\n\" \"\$PARENT_ROOT\""
  )"

  temp_root="$(PARENT_ROOT_KEEP_TEMP=1 bash -c "$script")"
  [ -d "$temp_root" ]

  rm -rf "$temp_root" "$parent_root"
}

# Spec 3 Edge: Temp parent root cleaned on interrupt
@test "Spec 3 Edge: Temp parent root cleaned on interrupt" {
  local parent_root agent_layer_root script_path state_file pid temp_root
  parent_root="$(make_tmp_dir)"
  agent_layer_root="$parent_root/.agent-layer"
  create_agent_layer_root "$agent_layer_root"

  state_file="$parent_root/state.txt"
  script_path="$parent_root/run-temp.sh"
  cat > "$script_path" << EOF
#!/usr/bin/env bash
set -euo pipefail
source "$agent_layer_root/src/lib/parent-root.sh"
ROOTS_USE_TEMP_PARENT_ROOT=1 resolve_parent_root
trap '[[ "\${PARENT_ROOT_KEEP_TEMP:-0}" == "1" ]] || rm -rf "\$PARENT_ROOT"' EXIT INT TERM
printf "%s\\n" "\$PARENT_ROOT" > "$state_file"
sleep 5
EOF
  chmod +x "$script_path"

  "$script_path" &
  pid=$!

  for _ in {1..20}; do
    [[ -f "$state_file" ]] && break
    sleep 0.1
  done

  temp_root="$(cat "$state_file")"
  kill -INT "$pid"
  wait "$pid" || true

  [ ! -d "$temp_root" ]

  rm -rf "$parent_root"
}

# Spec 4: Dev repo requires explicit parent root configuration
@test "Spec 4: Dev repo requires explicit parent root configuration" {
  local base agent_layer_root script expected
  base="$(make_tmp_dir)"
  agent_layer_root="$base/agent-layer"
  mkdir -p "$agent_layer_root"
  create_agent_layer_root "$agent_layer_root"

  script="$(
    multiline \
      "source \"$agent_layer_root/src/lib/parent-root.sh\"" \
      "resolve_parent_root"
  )"

  run bash -c "$script"
  [ "$status" -ne 0 ]

  expected="$(
    multiline \
      "ERROR: Running from agent-layer repo requires explicit parent root configuration." \
      "" \
      "Context: Agent-layer development repo" \
      "" \
      "The agent-layer repo cannot auto-discover a parent root because it doesn't have" \
      "\".agent-layer\" as its directory name. You must explicitly specify how to set up" \
      "the test environment." \
      "" \
      "Options (choose one):" \
      "  1. Use temporary parent root (recommended for testing/CI):" \
      "     ./setup.sh --temp-parent-root" \
      "     ./tests/run.sh --temp-parent-root" \
      "" \
      "  2. Specify explicit parent root (if you have a test consumer repo):" \
      "     # NOTE: The test repo must have a symlink .agent-layer -> <this-repo>" \
      "     ./setup.sh --parent-root /path/to/test-repo" \
      "     ./tests/run.sh --parent-root /path/to/test-repo" \
      "" \
      "  3. Set PARENT_ROOT in $(realpath_dir \"$agent_layer_root\")/.env for persistent config:" \
      "     echo \"PARENT_ROOT=/path/to/test-repo\" > .env"
  )"
  [[ "$output" == "$expected" ]]

  rm -rf "$base"
}

# Spec 4: Renamed agent layer directory cannot discover parent root
@test "Spec 4: Renamed agent layer directory cannot discover parent root" {
  local base agent_layer_root script expected
  base="$(make_tmp_dir)"
  agent_layer_root="$base/custom-layer"
  mkdir -p "$agent_layer_root"
  create_agent_layer_root "$agent_layer_root"

  script="$(
    multiline \
      "source \"$agent_layer_root/src/lib/parent-root.sh\"" \
      "resolve_parent_root"
  )"

  run bash -c "$script"
  [ "$status" -ne 0 ]

  expected="$(
    multiline \
      "ERROR: Cannot discover parent root - agent layer directory name is not \".agent-layer\"" \
      "" \
      "Current name: custom-layer" \
      "Expected: .agent-layer" \
      "" \
      "Discovery is only allowed when the agent layer root is named \".agent-layer\"." \
      "If you renamed it, discovery will not work." \
      "" \
      "Options:" \
      "  1. Rename directory to .agent-layer (if this is an installed agent layer)" \
      "  2. Use explicit parent root: --parent-root <path>" \
      "  3. Use temp parent root: --temp-parent-root" \
      "  4. Set PARENT_ROOT in $(realpath_dir \"$agent_layer_root\")/.env"
  )"
  [[ "$output" == "$expected" ]]

  rm -rf "$base"
}

# Spec Precedence: --parent-root overrides .env
@test "Spec Precedence: --parent-root overrides .env" {
  local base agent_layer_root parent_root env_root script
  base="$(make_tmp_dir)"
  agent_layer_root="$base/.agent-layer"
  create_agent_layer_root "$agent_layer_root"

  env_root="$(make_tmp_dir)"
  ln -s "$agent_layer_root" "$env_root/.agent-layer"
  printf "PARENT_ROOT=$env_root\n" > "$agent_layer_root/.env"

  parent_root="$(make_tmp_dir)"
  ln -s "$agent_layer_root" "$parent_root/.agent-layer"

  script="$(
    multiline \
      "set -euo pipefail" \
      "source \"$agent_layer_root/src/lib/parent-root.sh\"" \
      "ROOTS_PARENT_ROOT=\"$parent_root\" resolve_parent_root" \
      "printf \"PARENT_ROOT=%s\n\" \"\$PARENT_ROOT\""
  )"

  run bash -c "$script"
  [ "$status" -eq 0 ]
  [[ "$output" == *"PARENT_ROOT=$(realpath_dir "$parent_root")"* ]]

  rm -rf "$base" "$env_root" "$parent_root"
}

# Spec Precedence: --temp-parent-root overrides .env
@test "Spec Precedence: --temp-parent-root overrides .env" {
  local base agent_layer_root env_root script temp_root
  base="$(make_tmp_dir)"
  agent_layer_root="$base/.agent-layer"
  create_agent_layer_root "$agent_layer_root"

  env_root="$(make_tmp_dir)"
  ln -s "$agent_layer_root" "$env_root/.agent-layer"
  printf "PARENT_ROOT=$env_root\n" > "$agent_layer_root/.env"

  script="$(
    multiline \
      "set -euo pipefail" \
      "source \"$agent_layer_root/src/lib/parent-root.sh\"" \
      "ROOTS_USE_TEMP_PARENT_ROOT=1 resolve_parent_root" \
      "printf \"PARENT_ROOT=%s\n\" \"\$PARENT_ROOT\""
  )"

  run bash -c "$script"
  [ "$status" -eq 0 ]
  temp_root="$(printf "%s\n" "$output" | sed -n "s/^PARENT_ROOT=//p")"
  [ "$temp_root" != "$(realpath_dir "$env_root")" ]

  rm -rf "$temp_root" "$base" "$env_root"
}

# Spec Precedence: .env overrides discovery
@test "Spec Precedence: .env overrides discovery" {
  local base agent_layer_root env_root script
  base="$(make_tmp_dir)"
  agent_layer_root="$base/.agent-layer"
  create_agent_layer_root "$agent_layer_root"

  env_root="$(make_tmp_dir)"
  ln -s "$agent_layer_root" "$env_root/.agent-layer"
  printf "PARENT_ROOT=$env_root\n" > "$agent_layer_root/.env"

  script="$(
    multiline \
      "set -euo pipefail" \
      "source \"$agent_layer_root/src/lib/parent-root.sh\"" \
      "resolve_parent_root" \
      "printf \"PARENT_ROOT=%s\n\" \"\$PARENT_ROOT\""
  )"

  run bash -c "$script"
  [ "$status" -eq 0 ]
  [[ "$output" == *"PARENT_ROOT=$(realpath_dir "$env_root")"* ]]

  rm -rf "$base" "$env_root"
}

# Spec Edge: Multiple .agent-layer entries in ancestry
@test "Spec Edge: Multiple .agent-layer entries in ancestry" {
  local outer inner agent_layer_root script
  outer="$(make_tmp_dir)"
  inner="$outer/inner"
  mkdir -p "$inner"
  agent_layer_root="$inner/.agent-layer"
  create_agent_layer_root "$agent_layer_root"

  ln -s "$agent_layer_root" "$outer/.agent-layer"

  script="$(
    multiline \
      "set -euo pipefail" \
      "source \"$agent_layer_root/src/lib/parent-root.sh\"" \
      "resolve_parent_root" \
      "printf \"PARENT_ROOT=%s\n\" \"\$PARENT_ROOT\""
  )"

  run bash -c "$script"
  [ "$status" -eq 0 ]
  [[ "$output" == *"PARENT_ROOT=$(realpath_dir "$inner")"* ]]

  rm -rf "$outer"
}

# Spec Edge: Paths with spaces are handled
@test "Spec Edge: Paths with spaces are handled" {
  local base parent_root agent_layer_root script expected
  base="$(make_tmp_dir)"
  parent_root="$base/parent root with spaces"
  mkdir -p "$parent_root"
  agent_layer_root="$parent_root/.agent-layer"
  create_agent_layer_root "$agent_layer_root"

  expected="$(realpath_dir "$parent_root")"

  script="$(
    multiline \
      "set -euo pipefail" \
      "source \"$agent_layer_root/src/lib/parent-root.sh\"" \
      "ROOTS_PARENT_ROOT=\"$parent_root\" resolve_parent_root" \
      "printf \"PARENT_ROOT=%s\n\" \"\$PARENT_ROOT\""
  )"

  run bash -c "$script"
  [ "$status" -eq 0 ]
  [[ "$output" == *"PARENT_ROOT=$expected"* ]]

  rm -rf "$base"
}

# Spec Edge: Paths with unicode characters are handled
@test "Spec Edge: Paths with unicode characters are handled" {
  local base parent_root agent_layer_root script expected
  base="$(make_tmp_dir)"
  parent_root="$base/parent-root-über"
  mkdir -p "$parent_root"
  agent_layer_root="$parent_root/.agent-layer"
  create_agent_layer_root "$agent_layer_root"

  expected="$(realpath_dir "$parent_root")"

  script="$(
    multiline \
      "set -euo pipefail" \
      "source \"$agent_layer_root/src/lib/parent-root.sh\"" \
      "ROOTS_PARENT_ROOT=\"$parent_root\" resolve_parent_root" \
      "printf \"PARENT_ROOT=%s\n\" \"\$PARENT_ROOT\""
  )"

  run bash -c "$script"
  [ "$status" -eq 0 ]
  [[ "$output" == *"PARENT_ROOT=$expected"* ]]

  rm -rf "$base"
}

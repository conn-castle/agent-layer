#!/usr/bin/env bats

# Tests for entrypoint helper error handling.
# Load shared helpers for temp roots and stub binaries.
load "helpers.bash"

# Helper: build a wrapper script that calls resolve_entrypoint_root.
write_wrapper() {
  local dir="$1"
  cat >"$dir/entrypoint-wrapper.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1090
source "$SCRIPT_DIR/src/lib/entrypoint.sh"
resolve_entrypoint_root
EOF
  chmod +x "$dir/entrypoint-wrapper.sh"
}

# Test: entrypoint.sh fails when discover-root.sh is missing
@test "entrypoint.sh fails when discover-root.sh is missing" {
  local root script_dir bash_bin
  root="$(make_tmp_dir)"
  script_dir="$root/scripts"
  bash_bin="$(command -v bash)"

  mkdir -p "$script_dir/src/lib"
  cp "$AGENTLAYER_ROOT/src/lib/entrypoint.sh" "$script_dir/src/lib/entrypoint.sh"
  write_wrapper "$script_dir"

  run "$bash_bin" -c "cd '$root' && '$script_dir/entrypoint-wrapper.sh' 2>&1"
  [ "$status" -ne 0 ]
  [[ "$output" == *"Missing src/lib/discover-root.sh"* ]]

  rm -rf "$root"
}

# Test: entrypoint.sh fails when .agent-layer is missing
@test "entrypoint.sh fails when .agent-layer is missing" {
  local root script_dir bash_bin
  root="$(make_tmp_dir)"
  script_dir="$root/scripts"
  bash_bin="$(command -v bash)"

  mkdir -p "$script_dir/src/lib"
  cp "$AGENTLAYER_ROOT/src/lib/entrypoint.sh" "$script_dir/src/lib/entrypoint.sh"
  cp "$AGENTLAYER_ROOT/src/lib/discover-root.sh" "$script_dir/src/lib/discover-root.sh"
  write_wrapper "$script_dir"

  run "$bash_bin" -c "cd '$root' && '$script_dir/entrypoint-wrapper.sh' 2>&1"
  [ "$status" -ne 0 ]
  [[ "$output" == *"Missing .agent-layer/ directory"* ]]

  rm -rf "$root"
}

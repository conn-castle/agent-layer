#!/usr/bin/env bats

# Tests for git hook behavior.
# Load shared helpers for temp roots and stub binaries.
load "helpers.bash"

teardown() {
  cleanup_test_temp_dirs
}

# Test: pre-commit hook runs the test suite
@test "pre-commit hook runs the test suite" {
  local root bash_bin
  root="$(create_isolated_parent_root)"
  bash_bin="$(command -v bash)"

  git -C "$root" init -q

  cat >"$root/.agent-layer/tests/run.sh" <<'EOF'
#!/usr/bin/env bash
echo "ran $*"
EOF
  chmod +x "$root/.agent-layer/tests/run.sh"

  run "$bash_bin" -c "cd '$root' && '$root/.agent-layer/.githooks/pre-commit'"
  [ "$status" -eq 0 ]
  [[ "$output" == *"ran --parent-root"* ]]

  rm -rf "$root"
}

# Test: pre-commit hook runs tests in agent-layer repo layout
@test "pre-commit hook runs tests in agent-layer repo layout" {
  local root bash_bin
  root="$(make_tmp_dir)"
  bash_bin="$(command -v bash)"

  mkdir -p "$root/.githooks" "$root/tests" "$root/tmp"
  cp "$AGENT_LAYER_ROOT/.githooks/pre-commit" "$root/.githooks/pre-commit"
  chmod +x "$root/.githooks/pre-commit"

  cat >"$root/tests/run.sh" <<'EOF'
#!/usr/bin/env bash
echo "ran-agent-layer $*"
EOF
  chmod +x "$root/tests/run.sh"

  git -C "$root" init -q

  run "$bash_bin" -c "cd '$root' && '$root/.githooks/pre-commit'"
  [ "$status" -eq 0 ]
  [[ "$output" == *"ran-agent-layer --temp-parent-root"* ]]

  rm -rf "$root"
}

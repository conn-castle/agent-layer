#!/usr/bin/env bash
# Non-zero mock exit code propagates through al claude with a clear error message.

run_scenario_error_propagation() {
  section "Error propagation"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  assert_exit_zero_in "$repo_dir" "al init --no-wizard" al init --no-wizard

  # Install mock that exits with code 1
  install_mock_claude "$repo_dir" 1

  # Capture output to verify error message
  local error_output rc=0
  error_output=$(cd "$repo_dir" && al claude 2>&1) || rc=$?
  if [[ $rc -eq 1 ]]; then
    pass "al claude exits 1 on mock failure"
  elif [[ $rc -ne 0 ]]; then
    fail "al claude exited with code $rc (expected 1)"
  else
    fail "al claude should have propagated mock failure, but got exit 0"
  fi

  # Verify the error message describes what happened
  assert_output_contains "$error_output" "exited with error" \
    "error message says claude exited with error"
  assert_output_not_contains "$error_output" "panic" \
    "no panic in error output"

  # Verify mock was still called (the error is from mock's exit code, not a pre-launch failure)
  assert_claude_mock_called "$MOCK_CLAUDE_LOG"

  # Verify mock received env vars even on failure path
  assert_claude_mock_env "$MOCK_CLAUDE_LOG" "AL_RUN_DIR"

  # ---- Verify exit code flattening (Issue exit-code-flatten): al claude
  # always exits 1 regardless of the subprocess exit code. Cobra wraps all
  # errors and main.go calls os.Exit(1). The subprocess's actual exit code
  # is lost because *exec.ExitError is never type-asserted.
  # When Issue exit-code-flatten is fixed, update this to expect 42.
  reset_mock_claude_log
  export MOCK_CLAUDE_EXIT_CODE=42

  local rc42=0
  (cd "$repo_dir" && al claude >/dev/null 2>&1) || rc42=$?
  if [[ $rc42 -eq 1 ]]; then
    pass "al claude flattens exit code 42 to 1 (Issue exit-code-flatten)"
  else
    fail "al claude should flatten exit code 42 to 1, got $rc42 (has exit-code-flatten been fixed? update this test)"
  fi

  # Reset to default
  export MOCK_CLAUDE_EXIT_CODE=0

  cleanup_scenario_dir "$repo_dir"
}

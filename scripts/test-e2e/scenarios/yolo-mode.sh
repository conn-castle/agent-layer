#!/usr/bin/env bash
# Scenario 060: YOLO mode passes --dangerously-skip-permissions to mock
# and prints the yolo acknowledgement.

run_scenario_yolo_mode() {
  section "YOLO mode"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  assert_exit_zero_in "$repo_dir" "al init --no-wizard" al init --no-wizard

  assert_exit_zero_in "$repo_dir" "al wizard --profile yolo.toml --yes" \
    al wizard --profile "$E2E_FIXTURE_DIR/profiles/yolo.toml" --yes

  # Verify config actually has yolo mode set
  assert_file_contains "$repo_dir/.agent-layer/config.toml" 'mode = "yolo"' \
    "config.toml has yolo approvals mode"

  install_mock_claude "$repo_dir"

  # Capture al claude output to check for yolo warning message
  local claude_output rc=0
  claude_output=$(cd "$repo_dir" && al claude 2>&1) || rc=$?
  if [[ $rc -eq 0 ]]; then
    pass "al claude (yolo)"
  else
    fail "al claude (yolo) (exit code: $rc)"
  fi

  # Verify the yolo acknowledgement was printed
  assert_output_contains "$claude_output" "[yolo]" \
    "al claude output contains [yolo] acknowledgement"
  assert_output_contains "$claude_output" "permission prompts disabled" \
    "al claude output explains yolo disables permissions"

  assert_claude_mock_called "$MOCK_CLAUDE_LOG"
  assert_claude_mock_has_arg "$MOCK_CLAUDE_LOG" "--dangerously-skip-permissions"

  # Verify mock did NOT receive model flag (not configured)
  assert_claude_mock_lacks_arg "$MOCK_CLAUDE_LOG" "--model"

  # Verify mock received env vars
  assert_claude_mock_env "$MOCK_CLAUDE_LOG" "AL_RUN_DIR"

  cleanup_scenario_dir "$repo_dir"
}

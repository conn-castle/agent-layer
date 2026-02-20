#!/usr/bin/env bash
# Scenario 012: Running al init on an already-initialized repo fails with
# a clear error message (not a silent overwrite).

run_scenario_init_already_initialized() {
  section "Init already initialized"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  # First init should succeed
  assert_exit_zero_in "$repo_dir" "al init --no-wizard (first)" al init --no-wizard

  assert_file_exists "$repo_dir/.agent-layer/config.toml" "config.toml exists after first init"

  # Second init should fail with a clear error
  local output rc=0
  output=$(cd "$repo_dir" && al init --no-wizard 2>&1) || rc=$?
  if [[ $rc -ne 0 ]]; then
    pass "al init --no-wizard (second) exits nonzero"
  else
    fail "al init --no-wizard (second) should fail but got exit 0"
  fi

  # Verify the error message is actionable
  assert_output_contains "$output" "already initialized" \
    "error says already initialized"
  assert_output_contains "$output" "al upgrade" \
    "error suggests al upgrade"

  # Verify the original config was NOT overwritten (still has content)
  assert_file_contains "$repo_dir/.agent-layer/config.toml" "[approvals]" \
    "config.toml still has original content after failed second init"

  cleanup_scenario_dir "$repo_dir"
}

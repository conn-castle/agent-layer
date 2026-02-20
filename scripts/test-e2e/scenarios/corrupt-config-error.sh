#!/usr/bin/env bash
# Corrupt config.toml gives a helpful error, not a crash.

run_scenario_corrupt_config_error() {
  section "Corrupt config error"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  assert_exit_zero_in "$repo_dir" "al init --no-wizard" al init --no-wizard

  # Overwrite config.toml with garbage
  echo "THIS IS NOT VALID TOML {{{{" > "$repo_dir/.agent-layer/config.toml"

  install_mock_claude "$repo_dir"

  # al claude should fail with a helpful error (not a panic/crash)
  local output rc=0
  output=$(cd "$repo_dir" && al claude 2>&1) || rc=$?
  if [[ $rc -ne 0 ]]; then
    pass "al claude exits nonzero with corrupt config"
  else
    fail "al claude should fail with corrupt config, but got exit 0"
  fi

  # Verify the error identifies the config problem
  assert_output_contains "$output" "invalid config" \
    "error message says invalid config"
  assert_output_contains "$output" "config.toml" \
    "error message references config.toml"
  assert_output_not_contains "$output" "panic" \
    "no panic in output"
  assert_output_not_contains "$output" "runtime error" \
    "no runtime error in output"

  # al sync should also fail with the same config error
  local sync_output sync_rc=0
  sync_output=$(cd "$repo_dir" && al sync 2>&1) || sync_rc=$?
  if [[ $sync_rc -ne 0 ]]; then
    pass "al sync exits nonzero with corrupt config"
  else
    fail "al sync should fail with corrupt config, but got exit 0"
  fi

  assert_output_contains "$sync_output" "invalid config" \
    "sync error says invalid config"
  assert_output_not_contains "$sync_output" "panic" \
    "no panic in sync output"

  # al doctor should still run (uses lenient config loading).
  # Doctor should report a config failure with specific messaging.
  # Doctor exits non-zero when it finds [FAIL] items — this is correct
  # health-check behavior (signals "there are problems" to scripts/CI).
  local doctor_output doctor_rc=0
  doctor_output=$(cd "$repo_dir" && al doctor 2>&1) || doctor_rc=$?
  if [[ $doctor_rc -ne 0 ]]; then
    pass "al doctor exits nonzero with corrupt config (health check found failures)"
  else
    fail "al doctor should exit nonzero when config is corrupt (found [FAIL] items)"
  fi
  assert_output_not_contains "$doctor_output" "panic" \
    "no panic in doctor output"
  assert_output_contains "$doctor_output" "Checking Agent Layer health" \
    "doctor still runs health check with corrupt config"
  # Doctor should detect the corrupt config and report [FAIL]
  assert_output_contains "$doctor_output" "[FAIL]" \
    "doctor reports [FAIL] for corrupt config"
  assert_output_contains "$doctor_output" "Failed to load configuration" \
    "doctor says failed to load configuration"

  # al wizard --profile --yes silently overwrites the corrupt config with the
  # profile file (it reads existing config as raw bytes for diff, never parses
  # it as TOML). This is by design: profile mode is a forceful replacement.
  # Note: wizard without --profile requires an interactive terminal.
  local wizard_output wizard_rc=0
  wizard_output=$(cd "$repo_dir" && al wizard --profile "$E2E_DEFAULTS_TOML" --yes 2>&1) || wizard_rc=$?
  if [[ $wizard_rc -eq 0 ]]; then
    pass "al wizard --profile --yes overwrites corrupt config with profile"
  else
    fail "al wizard --profile --yes should overwrite corrupt config (exit code: $wizard_rc)"
  fi
  assert_output_not_contains "$wizard_output" "panic" \
    "no panic in wizard output"

  # Wizard should have created a backup of the corrupt config
  assert_file_exists "$repo_dir/.agent-layer/config.toml.bak" \
    "wizard created backup of corrupt config"

  # The overwritten config should now be valid TOML (matches profile)
  assert_file_contains "$repo_dir/.agent-layer/config.toml" "[approvals]" \
    "config.toml is valid after wizard overwrite (has [approvals] section)"

  # NOTE: The wizard does NOT detect or warn about config corruption.
  # It reads the existing config as raw bytes for diff preview only.
  # The word "config" appears in wizard output only because of the diff
  # preview header, not because corruption was detected.
  assert_output_contains "$wizard_output" "Wizard completed" \
    "wizard completed successfully after overwriting corrupt config"

  # After overwrite, config should be valid and al claude should work
  local post_wizard_output post_wizard_rc=0
  post_wizard_output=$(cd "$repo_dir" && al claude 2>&1) || post_wizard_rc=$?
  if [[ $post_wizard_rc -eq 0 ]]; then
    pass "al claude works after wizard overwrites corrupt config"
  else
    fail "al claude should work after wizard overwrite (exit code: $post_wizard_rc)"
  fi

  cleanup_scenario_dir "$repo_dir"
}

#!/usr/bin/env bash
# Scenario: Wizard --profile can recover from various broken states:
# - missing config.toml (not just corrupt)
# - missing .agent-layer directory structure
# This tests the resilience of the wizard recovery path.

run_scenario_wizard_recovery() {
  section "Wizard recovery"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  assert_exit_zero_in "$repo_dir" "al init --no-wizard" al init --no-wizard

  # ---- Test 1: Delete config.toml, wizard --profile recovers ----
  rm -f "$repo_dir/.agent-layer/config.toml"
  if [[ -f "$repo_dir/.agent-layer/config.toml" ]]; then
    fail "config.toml should be deleted for test"
    return
  fi

  local wizard_output wizard_rc=0
  wizard_output=$(cd "$repo_dir" && al wizard --profile "$E2E_DEFAULTS_TOML" --yes 2>&1) || wizard_rc=$?
  if [[ $wizard_rc -eq 0 ]]; then
    pass "wizard --profile recovers from missing config.toml"
  else
    fail "wizard --profile should recover from missing config.toml (exit code: $wizard_rc)"
    echo "  output (first 5 lines):"
    echo "$wizard_output" | head -5 | sed 's/^/    /'
  fi

  assert_output_not_contains "$wizard_output" "panic" \
    "no panic during wizard recovery"
  assert_file_exists "$repo_dir/.agent-layer/config.toml" \
    "config.toml restored by wizard"
  assert_file_contains "$repo_dir/.agent-layer/config.toml" "[approvals]" \
    "restored config.toml has [approvals] section"

  # ---- Test 2: al sync works after recovery ----
  assert_exit_zero_in "$repo_dir" "al sync after wizard recovery" al sync

  # ---- Test 3: al claude works after recovery ----
  install_mock_claude "$repo_dir"

  local claude_output claude_rc=0
  claude_output=$(cd "$repo_dir" && al claude 2>&1) || claude_rc=$?
  if [[ $claude_rc -eq 0 ]]; then
    pass "al claude works after wizard recovery"
  else
    fail "al claude should work after wizard recovery (exit code: $claude_rc)"
    echo "  output (first 5 lines):"
    echo "$claude_output" | head -5 | sed 's/^/    /'
  fi

  assert_claude_mock_called "$MOCK_CLAUDE_LOG"
  assert_generated_artifacts "$repo_dir"

  cleanup_scenario_dir "$repo_dir"
}

#!/usr/bin/env bash
# Upgrade from oldest version, wizard with defaults, al claude works.

run_scenario_upgrade_wizard_defaults() {
  section "Upgrade + wizard defaults + al claude"

  if skip_if_no_oldest_binary; then return; fi
  E2E_UPGRADE_SCENARIO_COUNT=$((E2E_UPGRADE_SCENARIO_COUNT + 1))

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  setup_old_version_via_binary "$repo_dir" "$E2E_OLDEST_BINARY"
  assert_al_version_content "$repo_dir" "$E2E_OLDEST_VERSION"

  local upgrade_output upgrade_rc=0
  upgrade_output=$(cd "$repo_dir" && al upgrade --yes --apply-managed-updates --apply-memory-updates --apply-deletions 2>&1) || upgrade_rc=$?
  if [[ $upgrade_rc -eq 0 ]]; then
    pass "al upgrade from $E2E_OLDEST_VERSION"
  else
    fail "al upgrade from $E2E_OLDEST_VERSION (exit code: $upgrade_rc)"
    echo "  output (first 10 lines):"
    echo "$upgrade_output" | head -10 | sed 's/^/    /'
  fi
  assert_no_crash_markers "$upgrade_output" "no crash markers in upgrade defaults output"
  assert_output_contains "$upgrade_output" "Created upgrade snapshot" \
    "upgrade defaults output mentions snapshot creation"
  assert_output_contains "$upgrade_output" "Running sync" \
    "upgrade defaults output says sync ran"
  assert_output_contains "$upgrade_output" "Upgrade successful." \
    "upgrade defaults output says successful"
  assert_al_version_content "$repo_dir" "$AL_E2E_VERSION_NO_V"

  # Capture wizard output to verify it ran successfully
  local wizard_output rc=0
  wizard_output=$(cd "$repo_dir" && al wizard --profile "$E2E_DEFAULTS_TOML" --yes 2>&1) || rc=$?
  if [[ $rc -eq 0 ]]; then
    pass "al wizard defaults after upgrade"
  else
    fail "al wizard defaults after upgrade (exit code: $rc)"
    echo "  output (first 5 lines):"
    echo "$wizard_output" | head -5 | sed 's/^/    /'
  fi

  assert_no_crash_markers "$wizard_output" "no crash markers in wizard output after upgrade"
  assert_output_contains "$wizard_output" "Running sync" \
    "wizard output says sync ran after upgrade"
  assert_output_contains "$wizard_output" "Wizard completed" \
    "wizard output says completed after upgrade"

  # Verify config is valid after upgrade + wizard
  assert_file_contains "$repo_dir/.agent-layer/config.toml" 'mode = "all"' \
    "config.toml has default approvals mode after upgrade+wizard"

  install_mock_claude "$repo_dir"

  assert_exit_zero_in "$repo_dir" "al claude after upgrade + wizard" al claude

  assert_claude_mock_called "$MOCK_CLAUDE_LOG"
  assert_claude_mock_env_non_empty "$MOCK_CLAUDE_LOG" "AL_RUN_DIR"
  assert_claude_mock_env_non_empty "$MOCK_CLAUDE_LOG" "AL_RUN_ID"
  assert_generated_artifacts "$repo_dir"

  # Verify CLAUDE.md has real instruction content after full pipeline
  assert_file_contains "$repo_dir/CLAUDE.md" "BEGIN: 00_rules.md" \
    "CLAUDE.md has instruction blocks after upgrade+wizard"

  cleanup_scenario_dir "$repo_dir"
}

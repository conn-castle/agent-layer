#!/usr/bin/env bash
# Upgrade from oldest version, wizard with defaults, al claude works.

run_scenario_upgrade_wizard_defaults() {
  section "Upgrade + wizard defaults + al claude"

  if skip_if_no_oldest_binary; then return; fi
  E2E_UPGRADE_SCENARIO_COUNT=$((E2E_UPGRADE_SCENARIO_COUNT + 1))

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  setup_old_version_via_binary "$repo_dir" "$E2E_OLDEST_BINARY"

  assert_exit_zero_in "$repo_dir" "al upgrade from $E2E_OLDEST_VERSION" \
    al upgrade --yes --apply-managed-updates --apply-memory-updates --apply-deletions

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
  assert_output_contains "$wizard_output" "Wizard completed" \
    "wizard output says completed after upgrade"

  # Verify config is valid after upgrade + wizard
  assert_file_contains "$repo_dir/.agent-layer/config.toml" 'mode = "all"' \
    "config.toml has default approvals mode after upgrade+wizard"

  install_mock_claude "$repo_dir"

  assert_exit_zero_in "$repo_dir" "al claude after upgrade + wizard" al claude

  assert_claude_mock_called "$MOCK_CLAUDE_LOG"
  assert_claude_mock_env "$MOCK_CLAUDE_LOG" "AL_RUN_DIR"
  assert_claude_mock_env "$MOCK_CLAUDE_LOG" "AL_RUN_ID"
  assert_generated_artifacts "$repo_dir"

  # Verify CLAUDE.md has real instruction content after full pipeline
  assert_file_contains "$repo_dir/CLAUDE.md" "BEGIN: 00_base.md" \
    "CLAUDE.md has instruction blocks after upgrade+wizard"

  cleanup_scenario_dir "$repo_dir"
}

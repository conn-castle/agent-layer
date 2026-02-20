#!/usr/bin/env bash
# Upgrade from v0.8.0 (oldest supported), al claude works.

run_scenario_upgrade_claude() {
  section "Upgrade from old version + al claude"

  if skip_if_no_oldest_binary; then return; fi
  E2E_UPGRADE_SCENARIO_COUNT=$((E2E_UPGRADE_SCENARIO_COUNT + 1))

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  setup_old_version_via_binary "$repo_dir" "$E2E_OLDEST_BINARY"
  assert_al_version_content "$repo_dir" "$E2E_OLDEST_VERSION"

  # Capture upgrade output to verify it prints expected messages
  local upgrade_output rc=0
  upgrade_output=$(cd "$repo_dir" && al upgrade --yes --apply-managed-updates --apply-memory-updates --apply-deletions 2>&1) || rc=$?
  if [[ $rc -eq 0 ]]; then
    pass "al upgrade from $E2E_OLDEST_VERSION"
  else
    fail "al upgrade from $E2E_OLDEST_VERSION (exit code: $rc)"
    echo "  output (first 10 lines):"
    echo "$upgrade_output" | head -10 | sed 's/^/    /'
  fi

  assert_no_crash_markers "$upgrade_output" "no crash markers in upgrade output"

  # Upgrade should create a snapshot
  assert_output_contains "$upgrade_output" "Created upgrade snapshot" \
    "upgrade output mentions snapshot creation"

  assert_al_version_content "$repo_dir" "$AL_E2E_VERSION_NO_V"

  # Verify upgrade actually updated template files (not just version)
  # docs/agent-layer/ should have been created by upgrade (memory templates)
  assert_dir_exists "$repo_dir/docs/agent-layer" \
    "upgrade created docs/agent-layer/ memory directory"
  assert_file_exists "$repo_dir/docs/agent-layer/COMMANDS.md" \
    "upgrade created docs/agent-layer/COMMANDS.md"

  install_mock_claude "$repo_dir"

  assert_exit_zero_in "$repo_dir" "al claude after upgrade from $E2E_OLDEST_VERSION" al claude

  assert_claude_mock_called "$MOCK_CLAUDE_LOG"
  assert_claude_mock_env "$MOCK_CLAUDE_LOG" "AL_RUN_DIR"
  assert_claude_mock_env "$MOCK_CLAUDE_LOG" "AL_RUN_ID"
  assert_generated_artifacts "$repo_dir"

  # Verify instruction files exist and have current template content
  assert_file_contains "$repo_dir/.agent-layer/instructions/00_base.md" \
    "Guiding Principles" "upgraded instructions/00_base.md has Guiding Principles"

  cleanup_scenario_dir "$repo_dir"
}

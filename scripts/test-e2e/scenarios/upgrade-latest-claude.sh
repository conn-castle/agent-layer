#!/usr/bin/env bash
# Upgrade from the latest published release to the current code, al claude works.
# This catches regressions introduced since the last release.

run_scenario_upgrade_latest_claude() {
  section "Upgrade from latest release + al claude"

  if skip_if_no_latest_binary; then return; fi
  E2E_UPGRADE_SCENARIO_COUNT=$((E2E_UPGRADE_SCENARIO_COUNT + 1))

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  setup_old_version_via_binary "$repo_dir" "$E2E_LATEST_BINARY"
  assert_al_version_content "$repo_dir" "$E2E_LATEST_VERSION"

  # Capture upgrade output
  local upgrade_output rc=0
  upgrade_output=$(cd "$repo_dir" && al upgrade --yes --apply-managed-updates --apply-memory-updates --apply-deletions 2>&1) || rc=$?
  if [[ $rc -eq 0 ]]; then
    pass "al upgrade from $E2E_LATEST_VERSION (latest release)"
  else
    fail "al upgrade from $E2E_LATEST_VERSION (latest release) (exit code: $rc)"
    echo "  output (first 10 lines):"
    echo "$upgrade_output" | head -10 | sed 's/^/    /'
  fi

  assert_no_crash_markers "$upgrade_output" "no crash markers in latest-release upgrade output"

  # Upgrade should create a snapshot
  assert_output_contains "$upgrade_output" "Created upgrade snapshot" \
    "latest-release upgrade output mentions snapshot creation"

  # Verify snapshot file actually exists
  local snapshot_id
  snapshot_id="$(get_latest_snapshot_id "$repo_dir")"
  if [[ -n "$snapshot_id" ]]; then
    pass "latest-release upgrade created snapshot file: $snapshot_id"
  else
    fail "latest-release upgrade should have created a snapshot file"
  fi

  assert_al_version_content "$repo_dir" "$AL_E2E_VERSION_NO_V"

  install_mock_claude "$repo_dir"

  assert_exit_zero_in "$repo_dir" "al claude after upgrade from latest release" al claude

  assert_claude_mock_called "$MOCK_CLAUDE_LOG"
  assert_claude_mock_env "$MOCK_CLAUDE_LOG" "AL_RUN_DIR"
  assert_claude_mock_env "$MOCK_CLAUDE_LOG" "AL_RUN_ID"
  assert_generated_artifacts "$repo_dir"

  cleanup_scenario_dir "$repo_dir"
}

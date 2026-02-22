#!/usr/bin/env bash
# Upgrade with zero-byte unknown file in .agent-layer/tmp.

run_scenario_upgrade_empty_unknown() {
  section "Upgrade with zero-byte unknown file"

  if skip_if_no_oldest_binary; then return; fi
  E2E_UPGRADE_SCENARIO_COUNT=$((E2E_UPGRADE_SCENARIO_COUNT + 1))

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  setup_old_version_via_binary "$repo_dir" "$E2E_OLDEST_BINARY"
  assert_al_version_content "$repo_dir" "$E2E_OLDEST_VERSION"

  # Create an empty file in .agent-layer/tmp - this should be captured in snapshot
  # and should not cause validation error even though it's empty.
  mkdir -p "$repo_dir/.agent-layer/tmp"
  touch "$repo_dir/.agent-layer/tmp/empty-unknown.txt"

  # Capture upgrade output to verify it prints expected messages
  local upgrade_output rc=0
  upgrade_output=$(cd "$repo_dir" && al upgrade --yes --apply-managed-updates --apply-memory-updates --apply-deletions 2>&1) || rc=$?
  if [[ $rc -eq 0 ]]; then
    pass "al upgrade with zero-byte unknown file"
  else
    fail "al upgrade with zero-byte unknown file (exit code: $rc)"
    echo "  output (first 20 lines):"
    echo "$upgrade_output" | head -20 | sed 's/^/    /'
  fi

  assert_no_crash_markers "$upgrade_output" "no crash markers in upgrade output"

  # Upgrade should create a snapshot
  assert_output_contains "$upgrade_output" "Created upgrade snapshot" \
    "upgrade output mentions snapshot creation"

  assert_al_version_content "$repo_dir" "$AL_E2E_VERSION_NO_V"

  cleanup_scenario_dir "$repo_dir"
}

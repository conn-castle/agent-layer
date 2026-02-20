#!/usr/bin/env bash
# Upgrade then rollback restores prior state.

run_scenario_upgrade_rollback() {
  section "Upgrade + rollback restores state"

  if skip_if_no_oldest_binary; then return; fi
  E2E_UPGRADE_SCENARIO_COUNT=$((E2E_UPGRADE_SCENARIO_COUNT + 1))

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  setup_old_version_via_binary "$repo_dir" "$E2E_OLDEST_BINARY"
  assert_al_version_content "$repo_dir" "$E2E_OLDEST_VERSION"

  # Snapshot entire .agent-layer/ state before upgrade (config, version, env,
  # instructions, slash-commands) — not just config.toml.
  local pre_snapshot="$E2E_TMP_ROOT/rollback-pre-upgrade.txt"
  _snapshot_agent_layer_state "$repo_dir" > "$pre_snapshot"

  assert_exit_zero_in "$repo_dir" "al upgrade from $E2E_OLDEST_VERSION (for rollback)" \
    al upgrade --yes --apply-managed-updates --apply-memory-updates --apply-deletions

  assert_al_version_content "$repo_dir" "$AL_E2E_VERSION_NO_V"

  # Grab the snapshot ID created by upgrade
  local snapshot_id
  snapshot_id="$(get_latest_snapshot_id "$repo_dir")"
  if [[ -z "$snapshot_id" ]]; then
    fail "no upgrade snapshot found after upgrade"
    cleanup_scenario_dir "$repo_dir"
    return
  fi
  pass "upgrade created snapshot: $snapshot_id"

  # Rollback
  assert_exit_zero_in "$repo_dir" "al upgrade rollback $snapshot_id" \
    al upgrade rollback "$snapshot_id"

  # Verify version was restored
  assert_al_version_content "$repo_dir" "$E2E_OLDEST_VERSION"

  # Verify entire .agent-layer/ state matches pre-upgrade snapshot
  assert_agent_layer_state_unchanged "$repo_dir" "$pre_snapshot" \
    ".agent-layer/ state fully restored after rollback"

  # Verify init structure still intact
  assert_al_init_structure "$repo_dir"

  cleanup_scenario_dir "$repo_dir"
}

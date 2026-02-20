#!/usr/bin/env bash
# Scenario 090: Running al claude twice produces identical sync outputs.

run_scenario_idempotency() {
  section "Idempotency"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  assert_exit_zero_in "$repo_dir" "al init --no-wizard" al init --no-wizard

  install_mock_claude "$repo_dir"

  # First run
  assert_exit_zero_in "$repo_dir" "al claude (first run)" al claude

  assert_claude_mock_called "$MOCK_CLAUDE_LOG"

  # Verify files have real content before snapshot (guards against empty-file bugs)
  assert_file_contains "$repo_dir/CLAUDE.md" "Guiding Principles" \
    "CLAUDE.md has content before idempotency snapshot"
  assert_file_contains "$repo_dir/.mcp.json" '"mcpServers"' \
    ".mcp.json has content before idempotency snapshot"

  # Snapshot ALL state after first run: sync outputs + core .agent-layer/ files
  local all_state_snapshot="$E2E_TMP_ROOT/idempotency-all-state.txt"
  _snapshot_all_state "$repo_dir" > "$all_state_snapshot"

  # Also keep the sync-only snapshot for the existing assertion
  E2E_SNAPSHOT_FILE="$E2E_TMP_ROOT/idempotency-snapshot.txt"
  export E2E_SNAPSHOT_FILE
  _snapshot_sync_outputs "$repo_dir" > "$E2E_SNAPSHOT_FILE"

  # Reset mock log for second run
  reset_mock_claude_log

  # Second run
  assert_exit_zero_in "$repo_dir" "al claude (second run)" al claude

  assert_claude_mock_called "$MOCK_CLAUDE_LOG"

  # Compare: sync outputs should be identical
  assert_generated_files_unchanged "$repo_dir" "sync outputs unchanged after second run"

  # Compare: ALL state (including config.toml, al.version, .env) should be identical
  assert_all_state_unchanged "$repo_dir" "$all_state_snapshot" \
    "all state (sync outputs + .agent-layer/) unchanged after second run"

  cleanup_scenario_dir "$repo_dir"
}
